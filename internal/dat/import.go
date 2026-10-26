package dat

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/contentidentity"
	"rommeta/internal/regions"
	"rommeta/internal/releasepolicy"
)

// ImportOptions controls DAT-to-catalog generation.
type ImportOptions struct {
	DataRoot               string
	PlatformOverride       string
	MemberPath             string
	Apply                  bool
	RegionPriority         []string
	RegionGroups           map[string][]string
	PlatformRegionPriority map[string][]string
	Progress               ProgressFunc
}

// ImportChanges summarizes how an import affects an existing platform.
type ImportChanges struct {
	Action     string
	Added      int
	Reconciled int
	Conflicts  []string
}

// ImportResult describes a dry-run or applied DAT import.
type ImportResult struct {
	Plan             Plan
	DataRoot         string
	PlatformPath     string
	GameBundleCount  int
	ReleaseCount     int
	FingerprintCount int
	SampleGames      []GameSample
	WrittenFileCount int
	Apply            bool
	Changes          ImportChanges
}

// ProgressFunc receives DAT import progress updates.
type ProgressFunc func(ImportProgress)

// ImportProgress describes the current DAT import stage.
type ImportProgress struct {
	Stage   string
	Current int
	Total   int
	Item    string
}

type datafile struct {
	format      string
	header      header
	games       []datGame
	romCount    int
	hashCounts  HashCounts
	diskCount   int
	sampleCount int
}

type datGame struct {
	Name         string
	Description  string
	SourceFile   string
	Year         string
	Manufacturer string
	SampleOf     string
	DriverStatus string
	Unofficial   bool
	Incomplete   bool
	Disks        []datROM
	DeviceRefs   []string
	ID           string
	CloneOf      string
	ROMOf        string
	Serial       string
	Version      string
	IsMachine    bool
	IsBIOS       bool
	IsDevice     bool
	Runnable     string
	IsMechanical bool
	ROMs         []datROM
	ROMCount     int
	ROMSpoolPath string
}

type datROM struct {
	Name   string
	Size   string
	CRC32  string
	MD5    string
	SHA1   string
	SHA256 string
	Status string
	Serial string
}

type gameBundle struct {
	Title    string
	Folder   string
	Releases []datGame
}

// Import reads a DAT/XML file or ZIP and optionally writes a local portable catalog.
func Import(path string, options ImportOptions) (ImportResult, error) {
	inputPath, err := filepath.Abs(path)
	if err != nil {
		return ImportResult{}, err
	}
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err = filepath.Abs(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}

	reportProgress(options.Progress, ImportProgress{Stage: "opening", Item: filepath.Base(inputPath)})
	opened, err := openDAT(inputPath, options.MemberPath)
	if err != nil {
		return ImportResult{}, err
	}
	if opened.MemberPath != "" {
		reportProgress(options.Progress, ImportProgress{Stage: "opening", Item: opened.MemberPath})
	}
	reportProgress(options.Progress, ImportProgress{Stage: "parsing", Item: firstNonEmpty(opened.MemberPath, filepath.Base(inputPath))})
	runtimePath := filepath.Join(dataRoot, ".rommeta")
	if err := catalogfs.MkdirAll(runtimePath, 0o755); err != nil {
		_ = opened.Close()
		return ImportResult{}, err
	}
	spoolPath, err := os.MkdirTemp(runtimePath, "dat-import-")
	if err != nil {
		_ = opened.Close()
		return ImportResult{}, err
	}
	defer catalogfs.RemoveAll(spoolPath)
	parsed, err := parseDatafile(opened.Reader, spoolPath)
	closeErr := opened.Close()
	if err != nil {
		return ImportResult{}, err
	}
	if closeErr != nil {
		return ImportResult{}, closeErr
	}

	reportProgress(options.Progress, ImportProgress{Stage: "planning", Item: firstNonEmpty(parsed.header.Name, parsed.header.Description, filepath.Base(inputPath))})
	plan := planFromDatafile(inputPath, opened, parsed, PlanOptions{PlatformOverride: options.PlatformOverride, MemberPath: options.MemberPath, SampleLimit: 8})
	platformFolder := safePathName(plan.PlatformName)
	if platformFolder == "" {
		return ImportResult{}, fmt.Errorf("could not infer a safe platform folder name")
	}

	importGames := importableGames(parsed.games, plan)
	if isPlanOnlyImport(plan) && options.Apply {
		return ImportResult{}, fmt.Errorf("arcade or machine-style DAT/listxml import is currently plan-only; run dat plan and review machine, BIOS, device, CHD, and sample counts before catalog generation")
	}

	reportProgress(options.Progress, ImportProgress{Stage: "bundling", Current: len(importGames), Total: len(importGames), Item: plan.PlatformName})
	bundles := bundleGames(importGames)
	fingerprintCount := 0
	for _, game := range importGames {
		fingerprintCount += gameROMCount(game)
	}

	platformPath := filepath.Join(dataRoot, "platforms", platformFolder)
	merge, err := planPlatformMerge(platformPath, plan, bundles, options.Progress)
	if err != nil {
		return ImportResult{}, err
	}
	if merge.normalized != nil {
		bundles = merge.normalized
	}
	result := ImportResult{
		Plan:             plan,
		DataRoot:         dataRoot,
		PlatformPath:     platformPath,
		GameBundleCount:  len(bundles),
		ReleaseCount:     len(importGames),
		FingerprintCount: fingerprintCount,
		SampleGames:      sampleImportGames(importGames, 8),
		Apply:            options.Apply,
		Changes:          merge.changes,
	}
	if !options.Apply {
		return result, nil
	}
	if len(merge.changes.Conflicts) > 0 {
		return result, mergeConflictError(merge.changes.Conflicts)
	}

	reportProgress(options.Progress, ImportProgress{Stage: "writing", Current: 0, Total: len(bundles), Item: platformFolder})
	writes, err := writeImportedPlatform(platformPath, plan, bundles, options, merge)
	if err != nil {
		return ImportResult{}, err
	}
	result.WrittenFileCount = writes
	return result, nil
}

func writeImportedPlatform(platformPath string, plan Plan, bundles []gameBundle, options ImportOptions, merge platformMerge) (int, error) {
	return publishImportedPlatform(platformPath, func(stagePath string) (int, error) {
		if err := copyPlatformSnapshot(platformPath, stagePath); err != nil {
			return 0, err
		}
		return writeImportFiles(stagePath, platformPath, plan, bundles, options, merge)
	}, os.Rename)
}

func writeImportFiles(targetPath, existingPath string, plan Plan, bundles []gameBundle, options ImportOptions, merge platformMerge) (int, error) {
	if err := catalogfs.MkdirAll(targetPath, 0o755); err != nil {
		return 0, err
	}
	writes := 0
	if err := writeFile(filepath.Join(targetPath, "_catalog.yaml"), renderCatalogYAML(plan)); err != nil {
		return 0, err
	}
	writes++
	platformYAML := renderPlatformYAML(plan)
	if plan.ArcadeSnapshot != "" {
		if existing, err := catalogfs.ReadFile(filepath.Join(existingPath, "_platform.yaml")); err == nil {
			platformYAML, err = mergeArcadePlatformYAML(string(existing), platformYAML)
			if err != nil {
				return 0, err
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	if err := writeFile(filepath.Join(targetPath, "_platform.yaml"), platformYAML); err != nil {
		return 0, err
	}
	writes++
	bundles = coalesceReconciledBundles(bundles, merge)
	for index, bundle := range bundles {
		reportProgress(options.Progress, ImportProgress{Stage: "writing", Current: index + 1, Total: len(bundles), Item: bundle.Title})
		folder := merge.existingFolder(bundle.Folder)
		gamePath := filepath.Join(targetPath, folder)
		if err := catalogfs.MkdirAll(gamePath, 0o755); err != nil {
			return 0, err
		}
		generated := renderGameYAML(plan, bundle, options)
		existingGamePath := filepath.Join(existingPath, folder, "game.yaml")
		if existing, err := catalogfs.ReadFile(existingGamePath); err == nil {
			generated, err = mergeImportedGameYAML(string(existing), generated, plan, options)
			if err != nil {
				return 0, fmt.Errorf("merge %s: %w", existingGamePath, err)
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		if err := writeFile(filepath.Join(gamePath, "game.yaml"), generated); err != nil {
			return 0, err
		}
		writes++
		fingerprints, err := renderFingerprintsYAML(plan, bundle)
		if err != nil {
			return 0, err
		}
		existingFingerprintPath := filepath.Join(existingPath, folder, "fingerprints.yaml")
		if existing, err := catalogfs.ReadFile(existingFingerprintPath); err == nil {
			fingerprints, err = mergeImportedFingerprintsYAML(string(existing), fingerprints)
			if err != nil {
				return 0, fmt.Errorf("merge %s: %w", existingFingerprintPath, err)
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		if err := writeFile(filepath.Join(gamePath, "fingerprints.yaml"), fingerprints); err != nil {
			return 0, err
		}
		writes++
	}
	return writes, nil
}

func copyPlatformSnapshot(source, target string) error {
	info, err := catalogfs.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("platform path is not a directory: %s", source)
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return catalogfs.MkdirAll(target, 0o755)
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return catalogfs.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("cannot stage non-regular catalog file: %s", path)
		}
		if err := catalogfs.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := catalogfs.Link(path, destination); err == nil {
			return nil
		}
		contents, err := catalogfs.ReadFile(path)
		if err != nil {
			return err
		}
		return catalogfs.WriteFile(destination, contents, 0o644)
	})
}

func reportProgress(progress ProgressFunc, snapshot ImportProgress) {
	if progress != nil {
		progress(snapshot)
	}
}

func parseDatafile(reader io.Reader, spoolPath string) (datafile, error) {
	decoder := xml.NewDecoder(reader)
	var result datafile
	var currentGame *datGame
	var inHeader bool
	var headerField string
	var gameField string
	var rootSeen bool
	gameIndex := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return datafile{}, err
		}

		switch token := token.(type) {
		case xml.StartElement:
			name := token.Name.Local
			if !rootSeen {
				rootSeen = true
				result.format = formatFromRoot(name)
				if result.format == "mame-listxml" {
					result.header.Name = "MAME"
					result.header.Version = attr(token.Attr, "build")
					result.header.Author = "MAME"
				}
			}
			switch name {
			case "header":
				inHeader = true
			case "name", "description", "version", "author":
				if inHeader {
					headerField = name
				} else if currentGame != nil && (name == "version" || name == "description") {
					gameField = name
				}
			case "year", "manufacturer", "serial":
				if currentGame != nil && !inHeader {
					gameField = name
				}
			case "game", "machine":
				game := datGame{
					Name:         attr(token.Attr, "name"),
					ID:           attr(token.Attr, "id"),
					CloneOf:      firstNonEmpty(attr(token.Attr, "cloneofid"), attr(token.Attr, "cloneof")),
					ROMOf:        attr(token.Attr, "romof"),
					SampleOf:     attr(token.Attr, "sampleof"),
					SourceFile:   attr(token.Attr, "sourcefile"),
					IsMachine:    name == "machine",
					IsBIOS:       attrYes(token.Attr, "isbios"),
					IsDevice:     attrYes(token.Attr, "isdevice"),
					Runnable:     attr(token.Attr, "runnable"),
					IsMechanical: attrYes(token.Attr, "ismechanical") || attrYes(token.Attr, "mechanical"),
				}
				if game.IsMachine && game.ID == "" {
					game.ID = game.Name
				}
				result.games = append(result.games, game)
				currentGame = &result.games[len(result.games)-1]
			case "rom":
				result.romCount++
				if attr(token.Attr, "crc") != "" {
					result.hashCounts.CRC32++
				}
				if attr(token.Attr, "md5") != "" {
					result.hashCounts.MD5++
				}
				if attr(token.Attr, "sha1") != "" {
					result.hashCounts.SHA1++
				}
				if attr(token.Attr, "sha256") != "" {
					result.hashCounts.SHA256++
				}
				if currentGame == nil {
					continue
				}
				currentGame.ROMs = append(currentGame.ROMs, datROM{
					Name:   attr(token.Attr, "name"),
					Size:   attr(token.Attr, "size"),
					CRC32:  strings.ToLower(attr(token.Attr, "crc")),
					MD5:    strings.ToLower(attr(token.Attr, "md5")),
					SHA1:   strings.ToLower(attr(token.Attr, "sha1")),
					SHA256: strings.ToLower(attr(token.Attr, "sha256")),
					Status: attr(token.Attr, "status"),
					Serial: attr(token.Attr, "serial"),
				})
			case "disk":
				result.diskCount++
				if currentGame != nil {
					currentGame.Disks = append(currentGame.Disks, datROM{Name: attr(token.Attr, "name"), SHA1: attr(token.Attr, "sha1"), Status: attr(token.Attr, "status")})
				}
			case "device_ref":
				if currentGame != nil {
					currentGame.DeviceRefs = append(currentGame.DeviceRefs, attr(token.Attr, "name"))
				}
			case "driver":
				if currentGame != nil {
					currentGame.DriverStatus = attr(token.Attr, "status")
					currentGame.Unofficial = attrYes(token.Attr, "unofficial")
					currentGame.Incomplete = attrYes(token.Attr, "incomplete")
				}
			case "sample":
				result.sampleCount++
			}
		case xml.CharData:
			value := strings.TrimSpace(string(token))
			if value == "" {
				continue
			}
			if headerField == "" {
				if currentGame == nil || gameField == "" {
					continue
				}
				switch gameField {
				case "description":
					currentGame.Description += string(token)
				case "year":
					currentGame.Year += value
				case "manufacturer":
					currentGame.Manufacturer += string(token)
				case "serial":
					currentGame.Serial += value
				case "version":
					currentGame.Version += value
				}
				continue
			}
			switch headerField {
			case "name":
				result.header.Name += value
			case "description":
				result.header.Description += value
			case "version":
				result.header.Version += value
			case "author":
				result.header.Author += value
			}
		case xml.EndElement:
			switch token.Name.Local {
			case "header":
				inHeader = false
			case "name", "description", "version", "author":
				headerField = ""
				if token.Name.Local == "version" || token.Name.Local == "description" {
					gameField = ""
				}
			case "year", "manufacturer", "serial":
				gameField = ""
			case "game", "machine":
				if currentGame != nil {
					currentGame.ROMCount = len(currentGame.ROMs)
					if len(currentGame.ROMs) > 0 && spoolPath != "" {
						gameIndex++
						path := filepath.Join(spoolPath, fmt.Sprintf("%08d.json", gameIndex))
						contents, err := json.Marshal(currentGame.ROMs)
						if err != nil {
							return datafile{}, err
						}
						if err := catalogfs.WriteFile(path, contents, 0o600); err != nil {
							return datafile{}, err
						}
						currentGame.ROMSpoolPath = path
						currentGame.ROMs = nil
					}
				}
				currentGame = nil
				gameField = ""
			}
		}
	}
	if result.format == "" {
		result.format = "xml"
	}
	return result, nil
}

func gameROMCount(game datGame) int {
	if game.ROMCount > 0 {
		return game.ROMCount
	}
	return len(game.ROMs)
}

func gameROMs(game datGame) ([]datROM, error) {
	if game.ROMSpoolPath == "" {
		return game.ROMs, nil
	}
	contents, err := catalogfs.ReadFile(game.ROMSpoolPath)
	if err != nil {
		return nil, err
	}
	var roms []datROM
	if err := json.Unmarshal(contents, &roms); err != nil {
		return nil, err
	}
	return roms, nil
}

func planFromDatafile(inputPath string, opened openedDAT, parsed datafile, options PlanOptions) Plan {
	format := parsed.format
	dataset := firstNonEmpty(parsed.header.Name, parsed.header.Description, defaultDatasetForFormat(format), datasetNameFromPath(opened.MemberPath), datasetNameFromPath(inputPath))
	platform, layoutHint := inferPlatform(dataset)
	authority := inferAuthority(dataset, parsed.header.Author, format, inputPath, opened.MemberPath)
	if isArcadeDataset(dataset, authority, format) && strings.TrimSpace(options.PlatformOverride) == "" {
		platform = "arcade"
		layoutHint = ""
	}
	if strings.TrimSpace(options.PlatformOverride) != "" {
		platform = strings.TrimSpace(options.PlatformOverride)
	}
	if platform == "" {
		platform = datasetNameFromPath(inputPath)
	}

	hashCounts := parsed.hashCounts
	cloneLinks := 0
	romDependencies := 0
	roms := parsed.romCount
	machines := 0
	playable := 0
	bios := 0
	devices := 0
	mechanical := 0
	var sample []GameSample
	for _, game := range parsed.games {
		if game.ROMOf != "" {
			romDependencies++
		}
		if game.CloneOf != "" {
			cloneLinks++
		}
		if len(sample) < options.SampleLimit {
			sample = append(sample, GameSample{
				Name:        game.Name,
				CloneOf:     game.CloneOf,
				ROMOf:       game.ROMOf,
				Description: game.Description,
				BIOS:        game.IsBIOS,
				Device:      game.IsDevice,
				Runnable:    game.Runnable,
				Mechanical:  game.IsMechanical,
			})
		}
		if game.IsMachine {
			machines++
			if game.IsBIOS {
				bios++
			}
			if game.IsDevice {
				devices++
			}
			if game.IsMechanical {
				mechanical++
			}
			if isPlayableMachine(GameSample{BIOS: game.IsBIOS, Device: game.IsDevice, Runnable: game.Runnable}) {
				playable++
			}
		}
	}

	return Plan{
		InputPath:          inputPath,
		Container:          opened.Container,
		MemberPath:         opened.MemberPath,
		Format:             format,
		Authority:          authority,
		DatasetName:        dataset,
		PlatformName:       platform,
		PlatformOverride:   strings.TrimSpace(options.PlatformOverride) != "",
		LayoutHint:         layoutHint,
		Version:            parsed.header.Version,
		Author:             parsed.header.Author,
		GameCount:          len(parsed.games),
		MachineCount:       machines,
		PlayableCount:      playable,
		BIOSCount:          bios,
		DeviceCount:        devices,
		MechanicalCount:    mechanical,
		ROMCount:           roms,
		DiskCount:          parsed.diskCount,
		SampleCount:        parsed.sampleCount,
		CloneLinkCount:     cloneLinks,
		ROMDependencyCount: romDependencies,
		HashCounts:         hashCounts,
		SampleGames:        sample,
	}
}

func isArcadePlan(plan Plan) bool {
	return isArcadeDataset(plan.DatasetName, plan.Authority, plan.Format)
}

func isPlanOnlyImport(plan Plan) bool {
	return isArcadePlan(plan) || plan.MachineCount > 0
}

func importableGames(games []datGame, plan Plan) []datGame {
	planOnly := isPlanOnlyImport(plan)
	var filtered []datGame
	for _, game := range games {
		if isIgnoredImportGame(game) {
			continue
		}
		if planOnly && (game.IsDevice || strings.EqualFold(game.Runnable, "no")) {
			continue
		}
		filtered = append(filtered, game)
	}
	return filtered
}

func sampleImportGames(games []datGame, limit int) []GameSample {
	if limit <= 0 {
		return nil
	}
	samples := make([]GameSample, 0, minInt(len(games), limit))
	for _, game := range games {
		samples = append(samples, GameSample{
			Name:        game.Name,
			CloneOf:     game.CloneOf,
			ROMOf:       game.ROMOf,
			Description: game.Description,
			BIOS:        game.IsBIOS,
			Device:      game.IsDevice,
			Runnable:    game.Runnable,
			Mechanical:  game.IsMechanical,
		})
		if len(samples) >= limit {
			break
		}
	}
	return samples
}

func isIgnoredImportGame(game datGame) bool {
	return game.IsBIOS || catalogrules.IsBIOSName(game.Name)
}

func authorityKey(plan Plan) string {
	if strings.TrimSpace(plan.Authority) != "" {
		return strings.TrimSpace(plan.Authority)
	}
	return "no-intro"
}

func authorityDisplayName(plan Plan) string {
	switch authorityKey(plan) {
	case "redump":
		return "Redump"
	case "mame":
		return "MAME"
	case "fbneo":
		return "FBNeo"
	case "no-intro":
		return "No-Intro"
	case "tosec":
		return "TOSEC"
	default:
		return authorityKey(plan)
	}
}

func bundleGames(games []datGame) []gameBundle {
	byID := map[string]datGame{}
	referencedIDs := map[string]bool{}
	for _, game := range games {
		if game.ID != "" {
			byID[game.ID] = game
		}
	}
	for _, game := range games {
		if parentID := groupParentID(game); parentID != "" {
			referencedIDs[rootCloneID(parentID, byID)] = true
		}
	}

	grouped := map[string][]datGame{}
	groupTitles := map[string]string{}
	for _, game := range games {
		title, content := contentidentity.Analyze(game.Name)
		key := "title:" + title
		if parentID := groupParentID(game); parentID != "" {
			rootID := rootCloneID(parentID, byID)
			key = "clone:" + rootID
			if parent, ok := byID[rootID]; ok {
				if parentTitle, parentContent := contentidentity.Analyze(parent.Name); parentContent.Key() == content.Key() {
					title = parentTitle
				}
			}
		} else if game.ID != "" && referencedIDs[game.ID] {
			key = "clone:" + game.ID
			title, _ = contentidentity.Analyze(game.Name)
		}
		if title == "" {
			title = game.Name
		}
		key += "\x00" + content.Key()
		if previous := groupTitles[key]; previous == "" || title < previous {
			groupTitles[key] = title
		}
		grouped[key] = append(grouped[key], game)
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		leftTitle := groupTitles[keys[i]]
		rightTitle := groupTitles[keys[j]]
		if leftTitle == rightTitle {
			return keys[i] < keys[j]
		}
		return leftTitle < rightTitle
	})

	usedFolders := map[string]bool{}
	bundles := make([]gameBundle, 0, len(keys))
	for _, key := range keys {
		title := groupTitles[key]
		releases := grouped[key]
		sort.Slice(releases, func(i, j int) bool {
			return releases[i].Name < releases[j].Name
		})
		_, content := contentidentity.Analyze(releases[0].Name)
		folderTitle := title
		if content.Kind != "retail" && !content.Development() && content.Component == "" {
			label := content.Kind
			if content.Variant != "" {
				label += ", " + content.Variant
			}
			folderTitle += " (" + strings.ToUpper(label[:1]) + label[1:] + ")"
		}
		folder := uniqueBundleFolderName(folderTitle, releases, usedFolders, false)
		bundles = append(bundles, gameBundle{Title: title, Folder: folder, Releases: releases})
	}
	return bundles
}

func hasFolderDiscriminator(releases []datGame) bool {
	return len(folderDiscriminatorTokens(releases)) > 0
}

func groupParentID(game datGame) string {
	return game.CloneOf
}

func rootCloneID(id string, games map[string]datGame) string {
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return id
		}
		seen[id] = true
		game, ok := games[id]
		parentID := groupParentID(game)
		if !ok || parentID == "" {
			return id
		}
		id = parentID
	}
	return id
}

func renderCatalogYAML(plan Plan) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "title: %s\n", yamlString(displayPlatformName(plan.PlatformName)))
	fmt.Fprintf(&b, "platform: %s\n", yamlString(plan.PlatformName))
	fmt.Fprintf(&b, "generated_from:\n")
	fmt.Fprintf(&b, "  source: %s\n", yamlString(authorityKey(plan)))
	fmt.Fprintf(&b, "  dataset: %s\n", yamlString(plan.DatasetName))
	if plan.Version != "" {
		fmt.Fprintf(&b, "  version: %s\n", yamlString(plan.Version))
	}
	if plan.LayoutHint != "" {
		fmt.Fprintf(&b, "  layout_hint: %s\n", yamlString(plan.LayoutHint))
	}
	fmt.Fprintf(&b, "notes:\n")
	fmt.Fprintf(&b, "  - Generated from DAT import. Keep human edits reviewable before regenerating.\n")
	return b.String()
}

func renderPlatformYAML(plan Plan) string {
	if plan.ArcadeSnapshot != "" {
		return renderArcadePlatform(plan)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "name: %s\n", yamlString(plan.PlatformName))
	fmt.Fprintf(&b, "display_name: %s\n", yamlString(displayPlatformName(plan.PlatformName)))
	fmt.Fprintf(&b, "authority:\n")
	fmt.Fprintf(&b, "  source: %s\n", yamlString(authorityKey(plan)))
	fmt.Fprintf(&b, "  system: %s\n", yamlString(plan.PlatformName))
	if plan.LayoutHint != "" {
		fmt.Fprintf(&b, "rom_layout: %s\n", yamlString(plan.LayoutHint))
	}
	fmt.Fprintf(&b, "notes:\n")
	fmt.Fprintf(&b, "  - Folder names preserve authority names except for path-unsafe characters.\n")
	return b.String()
}

func renderGameYAML(plan Plan, bundle gameBundle, options ImportOptions) string {
	if plan.ArcadeSnapshot != "" {
		return renderArcadeGame(plan, bundle, options)
	}
	priority := options.RegionPriority
	if platformPriority := options.PlatformRegionPriority[plan.PlatformName]; len(platformPriority) > 0 {
		priority = platformPriority
	}
	defaultRelease := chooseDefaultRelease(bundle.Releases, priority, options.RegionGroups)
	var b bytes.Buffer
	fmt.Fprintf(&b, "title: %s\n", yamlString(bundle.Title))
	if defaultRelease != "" {
		fmt.Fprintf(&b, "default_release: %s\n", yamlString(safePathName(defaultRelease)))
	}
	fmt.Fprintf(&b, "\nreleases:\n")
	for _, release := range bundle.Releases {
		key := safePathName(release.Name)
		fmt.Fprintf(&b, "  %s:\n", yamlString(key))
		fmt.Fprintf(&b, "    title: %s\n", yamlString(baseTitle(release.Name)))
		regions := releaseRegions(release.Name)
		if len(regions) > 0 {
			fmt.Fprintf(&b, "    regions: %s\n", yamlStringList(regions))
		}
		languages := releaseLanguages(release.Name)
		if len(languages) > 0 {
			fmt.Fprintf(&b, "    languages: %s\n", yamlStringList(languages))
		}
		tags := releaseTags(release.Name)
		if len(tags) > 0 {
			fmt.Fprintf(&b, "    tags: %s\n", yamlStringList(tags))
		}
		fmt.Fprintf(&b, "    authority:\n")
		fmt.Fprintf(&b, "      source: %s\n", yamlString(authorityKey(plan)))
		fmt.Fprintf(&b, "      entry_name: %s\n", yamlString(release.Name))
		if release.ID != "" {
			fmt.Fprintf(&b, "      entry_id: %s\n", yamlString(release.ID))
		}
		if release.CloneOf != "" {
			fmt.Fprintf(&b, "      cloneof_id: %s\n", yamlString(release.CloneOf))
		}
		if release.Serial != "" {
			fmt.Fprintf(&b, "      serial: %s\n", yamlString(release.Serial))
		}
		if release.Version != "" {
			fmt.Fprintf(&b, "      version: %s\n", yamlString(release.Version))
		}
	}
	_ = plan
	return b.String()
}

func renderFingerprintsYAML(plan Plan, bundle gameBundle) (string, error) {
	if plan.ArcadeSnapshot != "" {
		return renderArcadeFingerprints(plan, bundle)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "items:\n")
	for _, release := range bundle.Releases {
		roms, err := gameROMs(release)
		if err != nil {
			return "", fmt.Errorf("read fingerprints for %s: %w", release.Name, err)
		}
		for index, rom := range roms {
			id := release.ID
			if id == "" {
				id = safePathName(release.Name)
			}
			if len(roms) > 1 {
				id = fmt.Sprintf("%s.%d", id, index+1)
			}
			fmt.Fprintf(&b, "  - id: %s\n", yamlString(authorityKey(plan)+"."+id))
			fmt.Fprintf(&b, "    release: %s\n", yamlString(safePathName(release.Name)))
			fmt.Fprintf(&b, "    source:\n")
			fmt.Fprintf(&b, "      kind: dat\n")
			fmt.Fprintf(&b, "      name: %s\n", yamlString(authorityDisplayName(plan)))
			fmt.Fprintf(&b, "      system: %s\n", yamlString(plan.PlatformName))
			if plan.MemberPath != "" {
				fmt.Fprintf(&b, "      dat_file: %s\n", yamlString(filepath.Base(plan.MemberPath)))
			} else {
				fmt.Fprintf(&b, "      dat_file: %s\n", yamlString(filepath.Base(plan.InputPath)))
			}
			fmt.Fprintf(&b, "      entry_name: %s\n", yamlString(release.Name))
			if release.ID != "" {
				fmt.Fprintf(&b, "      entry_id: %s\n", yamlString(release.ID))
			}
			if release.CloneOf != "" {
				fmt.Fprintf(&b, "      cloneof_id: %s\n", yamlString(release.CloneOf))
			}
			fmt.Fprintf(&b, "    file:\n")
			fmt.Fprintf(&b, "      name: %s\n", yamlString(rom.Name))
			if rom.Size != "" {
				fmt.Fprintf(&b, "      size: %s\n", rom.Size)
			}
			if rom.CRC32 != "" {
				fmt.Fprintf(&b, "      crc32: %s\n", rom.CRC32)
			}
			if rom.MD5 != "" {
				fmt.Fprintf(&b, "      md5: %s\n", rom.MD5)
			}
			if rom.SHA1 != "" {
				fmt.Fprintf(&b, "      sha1: %s\n", rom.SHA1)
			}
			if rom.SHA256 != "" {
				fmt.Fprintf(&b, "      sha256: %s\n", rom.SHA256)
			}
			fmt.Fprintf(&b, "    dump:\n")
			if rom.Status != "" {
				fmt.Fprintf(&b, "      status: %s\n", yamlString(rom.Status))
			}
			if ext := strings.TrimPrefix(filepath.Ext(rom.Name), "."); ext != "" {
				fmt.Fprintf(&b, "      format: %s\n", yamlString(ext))
			}
			if plan.LayoutHint != "" {
				fmt.Fprintf(&b, "      layout: %s\n", yamlString(plan.LayoutHint))
			}
			if rom.Serial != "" {
				fmt.Fprintf(&b, "      serial: %s\n", yamlString(rom.Serial))
			}
		}
	}
	return b.String(), nil
}

func chooseDefaultRelease(releases []datGame, priority []string, groups map[string][]string) string {
	names := make([]string, 0, len(releases))
	for _, release := range releases {
		names = append(names, release.Name)
	}
	return releasepolicy.Choose(names, priority, groups)
}

func baseTitle(name string) string {
	title := strings.TrimSpace(name)
	for {
		trimmed := stripTrailingNameField(title, "(", ")")
		if trimmed != title {
			title = trimmed
			continue
		}
		trimmed = stripTrailingNameField(title, "[", "]")
		if trimmed == title {
			break
		}
		title = trimmed
	}
	return title
}

func stripTrailingNameField(name, open, close string) string {
	name = strings.TrimSpace(name)
	start := strings.LastIndex(name, open)
	end := strings.LastIndex(name, close)
	if start < 0 || end != len(name)-len(close) || start > end {
		return name
	}
	return strings.TrimSpace(name[:start])
}

func releaseRegions(name string) []string {
	var regions []string
	for _, qualifier := range trailingQualifiers(name) {
		for _, part := range splitQualifier(qualifier) {
			if isRegion(part) {
				regions = append(regions, canonicalRegion(part))
			}
		}
	}
	return uniqueStrings(regions)
}

func releaseLanguages(name string) []string {
	var languages []string
	for _, qualifier := range trailingQualifiers(name) {
		for _, part := range splitQualifier(qualifier) {
			if isLanguage(part) {
				languages = append(languages, strings.ToLower(part))
			}
		}
	}
	return uniqueStrings(languages)
}

func releaseTags(name string) []string {
	var tags []string
	for _, qualifier := range trailingQualifiers(name) {
		for _, part := range splitQualifier(qualifier) {
			lower := strings.ToLower(strings.TrimSpace(part))
			switch {
			case strings.Contains(lower, "pirate"):
				tags = append(tags, "pirate")
			case lower == "unl" || strings.Contains(lower, "unlicensed"):
				tags = append(tags, "unlicensed")
			case strings.Contains(lower, "aftermarket"):
				tags = append(tags, "aftermarket")
			case strings.Contains(lower, "homebrew"):
				tags = append(tags, "homebrew")
			case strings.Contains(lower, "beta"):
				tags = append(tags, "beta")
			case strings.Contains(lower, "proto"):
				tags = append(tags, "proto")
			case strings.Contains(lower, "rev"):
				tags = append(tags, "revision")
			case strings.Contains(lower, "demo"):
				tags = append(tags, "demo")
			case strings.Contains(lower, "sample"):
				tags = append(tags, "sample")
			}
		}
	}
	return uniqueStrings(tags)
}

func trailingQualifiers(name string) []string {
	var qualifiers []string
	title := strings.TrimSpace(name)
	for {
		start := strings.LastIndex(title, "(")
		end := strings.LastIndex(title, ")")
		if start < 0 || end != len(title)-1 || start > end {
			break
		}
		qualifier := strings.TrimSpace(title[start+1 : end])
		qualifiers = append([]string{qualifier}, qualifiers...)
		title = strings.TrimSpace(title[:start])
	}
	return qualifiers
}

func splitQualifier(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '+'
	})
}

func isRegion(value string) bool {
	_, ok := regions.NormalizeDATDisplay(value)
	return ok
}

func canonicalRegion(value string) string {
	if normalized, ok := regions.NormalizeDATDisplay(value); ok {
		return normalized
	}
	return strings.TrimSpace(value)
}

func isLanguage(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ar", "da", "de", "el", "en", "es", "fi", "fr", "it", "ja", "ko", "nl", "no", "pl", "pt", "ru", "sv", "zh":
		return true
	default:
		return false
	}
}

func displayPlatformName(name string) string {
	name = strings.TrimPrefix(name, "Nintendo - ")
	name = strings.TrimPrefix(name, "Sony - ")
	name = strings.TrimPrefix(name, "Sega - ")
	return name
}

func uniqueBundleFolderName(title string, releases []datGame, used map[string]bool, preferDiscriminator bool) string {
	base := safePathName(title)
	if !preferDiscriminator && !used[base] {
		used[base] = true
		return base
	}
	for _, suffix := range folderDiscriminatorCandidates(releases) {
		candidate := safePathName(base + " (" + suffix + ")")
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s [%d]", base, index)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func folderDiscriminatorCandidates(releases []datGame) []string {
	var candidates []string
	tokens := folderDiscriminatorTokens(releases)
	if len(tokens) == 0 {
		return candidates
	}
	candidates = appendFolderDiscriminatorCandidate(candidates, tokens)
	for _, release := range releases {
		candidates = appendFolderDiscriminatorCandidate(candidates, folderDiscriminatorTokens([]datGame{release}))
	}
	return candidates
}

func appendFolderDiscriminatorCandidate(candidates []string, tokens []string) []string {
	if len(tokens) == 0 {
		return candidates
	}
	candidate := strings.Join(tokens, ", ")
	for _, existing := range candidates {
		if existing == candidate {
			return candidates
		}
	}
	return append(candidates, candidate)
}

func folderDiscriminatorTokens(releases []datGame) []string {
	var tokens []string
	for _, release := range releases {
		tags := folderVisibleReleaseTags(release.Name)
		if len(tags) == 0 {
			return nil
		}
		for _, tag := range tags {
			tokens = appendUniqueDiscriminator(tokens, folderTagDisplayName(tag))
		}
	}
	return tokens
}

func folderVisibleReleaseTags(name string) []string {
	var tags []string
	for _, tag := range releaseTags(name) {
		switch tag {
		case "unlicensed", "pirate", "aftermarket", "homebrew", "beta", "proto", "demo", "sample":
			tags = append(tags, tag)
		}
	}
	return tags
}

func folderTagDisplayName(tag string) string {
	switch tag {
	case "unlicensed":
		return "Unl"
	case "pirate":
		return "Pirate"
	case "aftermarket":
		return "Aftermarket"
	case "homebrew":
		return "Homebrew"
	case "beta":
		return "Beta"
	case "proto":
		return "Proto"
	case "revision":
		return "Rev"
	case "demo":
		return "Demo"
	case "sample":
		return "Sample"
	default:
		return strings.TrimSpace(tag)
	}
}

func appendUniqueDiscriminator(tokens []string, token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return tokens
	}
	for _, existing := range tokens {
		if strings.EqualFold(existing, token) {
			return tokens
		}
	}
	return append(tokens, token)
}

func safePathName(name string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range strings.TrimSpace(name) {
		unsafe := r < 32 || strings.ContainsRune(`<>:"/\|?*`, r)
		if unsafe {
			if !lastSpace {
				b.WriteString(" - ")
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = unicode.IsSpace(r)
	}
	result := strings.TrimSpace(b.String())
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	result = strings.TrimRight(result, " .")
	if result == "" || result == "." || result == ".." {
		return "Untitled"
	}
	return result
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func yamlStringList(values []string) string {
	var quoted []string
	for _, value := range values {
		quoted = append(quoted, yamlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeFile(path, contents string) error {
	return atomicfile.WriteFile(path, []byte(contents), 0o644)
}

// Several regional DAT names may already belong to one reviewed catalog card.
// Render that destination once so later incoming bundles cannot overwrite earlier ones.
func coalesceReconciledBundles(bundles []gameBundle, merge platformMerge) []gameBundle {
	var out []gameBundle
	byFolder := map[string]int{}
	for _, bundle := range bundles {
		folder := merge.existingFolder(bundle.Folder)
		if index, ok := byFolder[folder]; ok {
			out[index].Releases = append(out[index].Releases, bundle.Releases...)
			continue
		}
		byFolder[folder] = len(out)
		out = append(out, bundle)
	}
	return out
}
