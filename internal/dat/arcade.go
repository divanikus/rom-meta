package dat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
)

type ArcadeOptions struct {
	ImportOptions
	ClassificationPath string
	Offline            bool
	Client             *http.Client
	RoutingPath        string
	Sets               []string
	Fold               bool
	ReportPath         string
}
type ArcadeBinding struct {
	Family   string `json:"family,omitempty" yaml:"family,omitempty"`
	Hardware string `json:"hardware,omitempty" yaml:"hardware,omitempty"`
	Platform string `json:"platform" yaml:"platform"`
	Game     string `json:"game" yaml:"game"`
	Release  string `json:"release" yaml:"release"`
}
type ArcadeEntry struct {
	Family           string `json:"family,omitempty"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	Category         string `json:"category,omitempty"`
	Platform         string `json:"platform,omitempty"`
	Hardware         string `json:"hardware,omitempty"`
	HardwareEvidence string `json:"hardware_evidence,omitempty"`
	Game             string `json:"game,omitempty"`
	CloneOf          string `json:"cloneof,omitempty"`
	ROMOf            string `json:"romof,omitempty"`
	FoldReview       string `json:"fold_review,omitempty"`
	Selected         bool   `json:"selected"`
}
type ArcadePlatformResult struct {
	Platform string        `json:"platform"`
	Kind     string        `json:"kind"`
	Games    int           `json:"games"`
	Releases int           `json:"releases"`
	Changes  ImportChanges `json:"changes"`
	Applied  bool          `json:"applied"`
}
type ArcadeFamilyResult struct {
	Family   string `json:"family"`
	Kind     string `json:"kind"`
	Games    int    `json:"games"`
	Releases int    `json:"releases"`
}
type ArcadeResult struct {
	Classification ArcadeClassificationSource `json:"classification"`
	Families       []ArcadeFamilyResult       `json:"families"`
	Version        string                     `json:"version"`
	Build          string                     `json:"build"`
	Snapshot       string                     `json:"snapshot"`
	Fold           bool                       `json:"fold"`
	Counts         map[string]int             `json:"counts"`
	Platforms      []ArcadePlatformResult     `json:"platforms"`
	Entries        []ArcadeEntry              `json:"entries"`
	Bindings       map[string]ArcadeBinding   `json:"bindings"`
	Titles         map[string]string          `json:"titles,omitempty"`
	Conflicts      []string                   `json:"conflicts,omitempty"`
	Applied        bool                       `json:"applied"`
}

// ImportArcade plans the entire source, then optionally applies the selected
// eligible sets using the existing additive platform writer.
func ImportArcade(path string, options ArcadeOptions) (result ArcadeResult, err error) {
	result.Counts = map[string]int{}
	result.Bindings = map[string]ArcadeBinding{}
	result.Titles = map[string]string{}
	result.Fold = options.Fold
	if strings.TrimSpace(options.DataRoot) == "" {
		return result, fmt.Errorf("--data-root is required")
	}
	root, err := filepath.Abs(options.DataRoot)
	if err != nil {
		return result, err
	}
	options.DataRoot = root
	if options.ReportPath != "" {
		report, e := filepath.Abs(options.ReportPath)
		if e != nil {
			return result, e
		}
		// Reports must never replace inputs or any existing file.
		if _, e = catalogfs.Lstat(report); e == nil {
			return result, fmt.Errorf("report already exists: %s", report)
		} else if !os.IsNotExist(e) {
			return result, e
		}
		if info, e := catalogfs.Stat(filepath.Dir(report)); e != nil {
			return result, e
		} else if !info.IsDir() {
			return result, fmt.Errorf("report parent is not a directory")
		}
		defer func() {
			data, e := json.MarshalIndent(result, "", "  ")
			if e == nil {
				e = writeArcadeNewFile(report, append(data, '\n'))
			}
			if e != nil {
				if err == nil {
					err = e
				} else {
					err = fmt.Errorf("%w; report: %v", err, e)
				}
			}
		}()
	}
	sourceHash, err := arcadeFileHash(path)
	if err != nil {
		return result, err
	}
	reportProgress(options.Progress, ImportProgress{Stage: "parsing", Item: filepath.Base(path)})
	opened, err := openDAT(path, options.MemberPath)
	if err != nil {
		return result, err
	}
	parsed, err := parseDatafile(opened.Reader, "")
	closeErr := opened.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	if parsed.format != "mame-listxml" {
		if inferAuthority(parsed.header.Name, parsed.header.Author, parsed.format, path, opened.MemberPath) == "fbneo" {
			return result, fmt.Errorf("FBNeo DAT detected: FBNeo catalog bindings are not implemented yet; MAME classifications cannot be applied to an FBNeo version")
		}
		return result, fmt.Errorf("classified arcade import requires official MAME listxml; distribution DATs and Software Lists are not supported")
	}
	result.Build = parsed.header.Version
	result.Version = arcadeVersionRE.FindString(result.Build)
	if result.Version == "" {
		return result, fmt.Errorf("MAME build has no supported version: %q", result.Build)
	}
	result.Classification, err = resolveArcadeClassification(result.Version, sourceHash, options)
	if err != nil {
		return result, err
	}
	options.ClassificationPath = result.Classification.Path
	categoryHash := result.Classification.SHA256
	classification, err := readArcadeClassification(options.ClassificationPath, result.Version)
	if err != nil {
		return result, err
	}
	for file, want := range map[string]string{path: sourceHash, options.ClassificationPath: categoryHash} {
		have, e := arcadeFileHash(file)
		if e != nil {
			return result, e
		}
		if have != want {
			return result, fmt.Errorf("input changed while being parsed: %s", file)
		}
	}
	routing, err := loadArcadeRouting(options.RoutingPath)
	if err != nil {
		return result, err
	}
	rules, err := json.MarshalIndent(routing, "", "  ")
	if err != nil {
		return result, err
	}
	rulesHash := arcadeBytesHash(rules)
	result.Snapshot = result.Version + "-" + arcadeBytesHash([]byte(sourceHash + "\n" + categoryHash + "\n" + rulesHash))[:12]
	index := map[string]datGame{}
	for _, g := range parsed.games {
		if !g.IsMachine || !arcadeShortNameRE.MatchString(g.Name) {
			return result, fmt.Errorf("invalid MAME machine identity %q", g.Name)
		}
		if _, exists := index[g.Name]; exists {
			return result, fmt.Errorf("duplicate MAME machine %s", g.Name)
		}
		if strings.TrimSpace(g.Description) == "" {
			return result, fmt.Errorf("missing MAME description for %s", g.Name)
		}
		index[g.Name] = g
	}
	requested := map[string]bool{}
	for _, name := range options.Sets {
		name = strings.TrimSpace(name)
		if _, ok := index[name]; !ok {
			return result, fmt.Errorf("requested set not found: %s", name)
		}
		requested[name] = true
	}
	byFamily := map[string][]datGame{}
	families := map[string]ArcadeFamily{}
	entryIndex := map[string]int{}
	hardware := map[string]string{}
	for _, name := range sortedArcadeKeys(index) {
		g := index[name]
		decision, reason := classifyArcade(g, classification)
		entry := ArcadeEntry{Name: name, Title: g.Description, Decision: decision, Reason: reason, Category: classification.categories[name], CloneOf: g.CloneOf, ROMOf: g.ROMOf}
		if decision == "eligible" {
			board, evidence := routeArcade(g, index, routing)
			entry.Family, entry.HardwareEvidence = arcadeCatalogPlatform(board, routing), evidence
			if entry.Family != "" {
				entry.Platform = routing.CatalogPlatform
				definition := routing.Platforms[entry.Family]
				families[name] = ArcadeFamily{Platform: entry.Family, Kind: definition.Kind, NamingEvidence: definition.NamingEvidence}
			}
			if routing.Platforms[board].Kind == "hardware" {
				entry.Hardware = board
				hardware[name] = board
			}
			if entry.Platform == "" {
				entry.Decision = "review"
				entry.Reason = entry.HardwareEvidence
			}
		}
		result.Counts[entry.Decision]++
		entry.Selected = entry.Decision == "eligible" && (len(requested) == 0 || requested[name])
		if entry.Selected {
			result.Counts["selected"]++
		} else if requested[name] {
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("requested %s is %s: %s", name, entry.Decision, entry.Reason))
		}
		entryIndex[name] = len(result.Entries)
		result.Entries = append(result.Entries, entry)
	}
	projectArcadeBootlegFamilies(result.Entries, index, entryIndex, families)
	for _, entry := range result.Entries {
		if entry.Decision == "eligible" {
			byFamily[entry.Family] = append(byFamily[entry.Family], index[entry.Name])
		}
	}
	plan := planFromDatafile(path, opened, parsed, PlanOptions{})
	plan.ArcadeSnapshot = result.Snapshot
	plan.ArcadeHardware = hardware
	plan.ArcadeFamilies = families
	plan.PlatformName = routing.CatalogPlatform
	plan.ArcadePlatformKind = "collection"
	plan.ArcadeNamingEvidence = "RomMeta common arcade catalog; hardware families are recorded on game cards"
	// Compute identities/folders from the complete eligible source. Selecting
	// releases must not change collision suffixes or a confirmed clone's card.
	var allBundles []gameBundle
	notes := map[string]string{}
	for _, family := range sortedArcadeKeys(byFamily) {
		grouped, review := arcadeBundleGamesWithReference(byFamily[family], options.Fold, hardware, routing, index)
		allBundles = append(allBundles, grouped...)
		for name, reason := range review {
			notes[name] = reason
		}
	}
	labelArcadeHardwareCollisions(allBundles, families)
	assignSharedArcadeFolders(allBundles)
	var bundles []gameBundle
	familyCounts := map[string]ArcadeFamilyResult{}
	for _, full := range allBundles {
		selected := gameBundle{Title: full.Title, Folder: full.Folder}
		for _, release := range full.Releases {
			if result.Entries[entryIndex[release.Name]].Selected {
				selected.Releases = append(selected.Releases, release)
			}
		}
		if len(selected.Releases) == 0 {
			continue
		}
		bundles = append(bundles, selected)
		family := families[selected.Releases[0].Name]
		count := familyCounts[family.Platform]
		count.Family, count.Kind = family.Platform, family.Kind
		count.Games++
		count.Releases += len(selected.Releases)
		familyCounts[family.Platform] = count
	}
	merge, e := planPlatformMerge(filepath.Join(root, "platforms", plan.PlatformName), plan, bundles, options.Progress)
	if e != nil {
		return result, e
	}
	result.Conflicts = append(result.Conflicts, merge.changes.Conflicts...)
	for _, bundle := range bundles {
		folder := merge.existingFolder(bundle.Folder)
		result.Titles[folder] = bundle.Title
		for _, release := range bundle.Releases {
			b := ArcadeBinding{Platform: plan.PlatformName, Family: families[release.Name].Platform, Game: folder, Release: release.Name, Hardware: hardware[release.Name]}
			if !arcadeSafeBinding(b) {
				return result, fmt.Errorf("unsafe generated binding for %s", release.Name)
			}
			result.Bindings[release.Name] = b
			entry := &result.Entries[entryIndex[release.Name]]
			entry.Game, entry.FoldReview = folder, notes[release.Name]
			if entry.FoldReview != "" {
				result.Counts["fold_review"]++
			}
		}
		if len(bundle.Releases) > 1 {
			result.Counts["fold_groups"]++
			result.Counts["folded_releases"] += len(bundle.Releases) - 1
		}
	}
	result.Counts["games"] = len(bundles)
	for _, family := range sortedArcadeKeys(familyCounts) {
		result.Families = append(result.Families, familyCounts[family])
	}
	if len(bundles) > 0 {
		result.Platforms = append(result.Platforms, ArcadePlatformResult{Platform: plan.PlatformName, Kind: plan.ArcadePlatformKind, Games: len(bundles), Releases: result.Counts["selected"], Changes: merge.changes})
	}
	sourceDir := filepath.Join(root, "bindings", "romsets", "mame", result.Snapshot)
	existing, e := readArcadeBindings(filepath.Join(sourceDir, "catalog.yaml"))
	if e != nil {
		return result, e
	}
	for name, b := range result.Bindings {
		if previous, ok := existing[name]; ok && previous != b {
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("%s changes an accepted snapshot binding; platform/card moves require review", name))
		}
	}
	previousConflicts, e := arcadePreviousBindingConflicts(root, result.Snapshot, result.Bindings)
	if e != nil {
		return result, e
	}
	result.Conflicts = append(result.Conflicts, previousConflicts...)
	if len(result.Conflicts) > 0 && options.Apply {
		return result, fmt.Errorf("%d arcade import conflicts; no catalog files were changed", len(result.Conflicts))
	}
	if !options.Apply {
		return result, nil
	}
	if len(bundles) == 0 {
		return result, fmt.Errorf("no eligible selected arcade sets")
	}
	if err = persistArcadeSource(sourceDir, path, options.ClassificationPath, sourceHash, categoryHash, rules, plan, result); err != nil {
		return result, err
	}
	// Save the intended mapping before platform writes, so a failed binding-file
	// write still leaves a reviewable recovery record next to the source.
	if err = atomicfile.WriteFile(filepath.Join(sourceDir, "catalog-planned.yaml"), []byte(arcadeYAML(map[string]any{"sets": result.Bindings})), 0o644); err != nil {
		return result, err
	}
	if _, e = writeImportedPlatform(filepath.Join(root, "platforms", plan.PlatformName), plan, bundles, options.ImportOptions, merge); e != nil {
		return result, fmt.Errorf("arcade catalog import failed: %w", e)
	}
	result.Platforms[0].Applied = true
	for name, b := range result.Bindings {
		existing[name] = b
	}
	if e = atomicfile.WriteFile(filepath.Join(sourceDir, "catalog.yaml"), []byte(arcadeYAML(map[string]any{"sets": existing})), 0o644); e != nil {
		return result, fmt.Errorf("Arcade was written but catalog binding update failed; recover from catalog-planned.yaml: %w", e)
	}
	result.Applied = true
	return result, nil
}

func readArcadeBindings(path string) (map[string]ArcadeBinding, error) {
	result := map[string]ArcadeBinding{}
	f, err := catalogfs.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()
	var doc struct {
		Sets map[string]ArcadeBinding `yaml:"sets"`
	}
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if err = d.Decode(&doc); err != nil {
		return result, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("binding file must contain one YAML document")
	}
	for name, b := range doc.Sets {
		if !arcadeShortNameRE.MatchString(name) || !arcadeSafeBinding(b) {
			return result, fmt.Errorf("invalid binding for %q", name)
		}
		result[name] = b
	}
	return result, nil
}

func persistArcadeSource(dir, source, classification, sourceHash, categoryHash string, rules []byte, plan Plan, result ArcadeResult) error {
	ext := ".xml"
	if strings.EqualFold(filepath.Ext(source), ".zip") {
		ext = ".zip"
	}
	sourceName := "mame" + ext
	metadata := []byte(arcadeYAML(map[string]any{"schema_version": 1, "source": "mame", "build": result.Build, "snapshot": result.Snapshot,
		"file": sourceName, "sha256": sourceHash, "member": plan.MemberPath, "machine_count": plan.MachineCount,
		"classifications": map[string]string{"file": "classifications.zip", "sha256": categoryHash},
		"routing":         map[string]string{"file": "routing.json", "sha256": arcadeBytesHash(rules)}}))
	if _, err := catalogfs.Stat(dir); err == nil {
		for name, want := range map[string]string{sourceName: sourceHash, "classifications.zip": categoryHash, "routing.json": arcadeBytesHash(rules), "_source.yaml": arcadeBytesHash(metadata)} {
			hash, e := arcadeFileHash(filepath.Join(dir, name))
			if e != nil {
				return e
			}
			if hash != want {
				return fmt.Errorf("existing snapshot file differs: %s", name)
			}
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(dir)
	if err := catalogfs.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".mame-source-")
	if err != nil {
		return err
	}
	defer catalogfs.RemoveAll(stage)
	for _, input := range []struct{ from, to, hash string }{{source, sourceName, sourceHash}, {classification, "classifications.zip", categoryHash}} {
		in, e := catalogfs.Open(input.from)
		if e != nil {
			return e
		}
		out, e := os.Create(filepath.Join(stage, input.to))
		if e != nil {
			in.Close()
			return e
		}
		h := sha256.New()
		_, e = io.Copy(io.MultiWriter(out, h), in)
		inErr := in.Close()
		outErr := out.Close()
		if e != nil {
			return e
		}
		if inErr != nil {
			return inErr
		}
		if outErr != nil {
			return outErr
		}
		if hex.EncodeToString(h.Sum(nil)) != input.hash {
			return fmt.Errorf("input changed during import: %s", input.from)
		}
	}
	if err = catalogfs.WriteFile(filepath.Join(stage, "routing.json"), rules, 0o644); err != nil {
		return err
	}
	if err = catalogfs.WriteFile(filepath.Join(stage, "_source.yaml"), metadata, 0o644); err != nil {
		return err
	}
	return catalogfs.Rename(stage, dir)
}

func arcadeFileHash(path string) (string, error) {
	f, err := catalogfs.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func arcadeBytesHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func writeArcadeNewFile(path string, data []byte) error {
	f, err := catalogfs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
