package dat

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogfs"
)

// PlanOptions controls read-only DAT planning.
type PlanOptions struct {
	PlatformOverride string
	MemberPath       string
	SampleLimit      int
}

// Plan describes what a DAT import would use before any catalog writes happen.
type Plan struct {
	InputPath            string
	Container            string
	MemberPath           string
	Format               string
	Authority            string
	DatasetName          string
	PlatformName         string
	PlatformOverride     bool
	LayoutHint           string
	Version              string
	Author               string
	GameCount            int
	MachineCount         int
	PlayableCount        int
	BIOSCount            int
	DeviceCount          int
	MechanicalCount      int
	ROMCount             int
	DiskCount            int
	SampleCount          int
	CloneLinkCount       int
	ROMDependencyCount   int
	ArcadeSnapshot       string
	ArcadePlatformKind   string
	ArcadeHardware       map[string]string
	ArcadeFamilies       map[string]ArcadeFamily
	ArcadeNamingEvidence string
	HashCounts           HashCounts
	SampleGames          []GameSample
}

// HashCounts records how much fingerprint data is available in the DAT.
type HashCounts struct {
	CRC32  int
	MD5    int
	SHA1   int
	SHA256 int
}

// GameSample is a small release sample for human-readable plans.
type GameSample struct {
	Name        string
	Description string
	CloneOf     string
	ROMOf       string
	BIOS        bool
	Device      bool
	Runnable    string
	Mechanical  bool
}

type header struct {
	Name        string
	Description string
	Version     string
	Author      string
}

type rawPlan struct {
	format             string
	header             header
	gameCount          int
	machineCount       int
	playableCount      int
	biosCount          int
	deviceCount        int
	mechanicalCount    int
	romCount           int
	diskCount          int
	sampleCount        int
	cloneLinkCount     int
	romDependencyCount int
	hashCounts         HashCounts
	sampleGames        []GameSample
}

// BuildPlan reads a DAT/XML file or ZIP containing a DAT/XML file and returns a
// read-only import plan.
func BuildPlan(path string, options PlanOptions) (Plan, error) {
	if options.SampleLimit < 0 {
		return Plan{}, fmt.Errorf("sample limit must be non-negative")
	}

	inputPath, err := filepath.Abs(path)
	if err != nil {
		return Plan{}, err
	}

	opened, err := openDAT(inputPath, options.MemberPath)
	if err != nil {
		return Plan{}, err
	}
	defer opened.Close()

	raw, err := parse(opened.Reader, options.SampleLimit)
	if err != nil {
		return Plan{}, err
	}

	dataset := firstNonEmpty(raw.header.Name, raw.header.Description, defaultDatasetForFormat(raw.format), datasetNameFromPath(opened.MemberPath), datasetNameFromPath(inputPath))
	platform, layoutHint := inferPlatform(dataset)
	authority := inferAuthority(dataset, raw.header.Author, raw.format, inputPath, opened.MemberPath)
	if isArcadeDataset(dataset, authority, raw.format) && strings.TrimSpace(options.PlatformOverride) == "" {
		platform = "arcade"
		layoutHint = ""
	}
	if strings.TrimSpace(options.PlatformOverride) != "" {
		platform = strings.TrimSpace(options.PlatformOverride)
	}
	if platform == "" {
		platform = datasetNameFromPath(inputPath)
	}

	return Plan{
		InputPath:          inputPath,
		Container:          opened.Container,
		MemberPath:         opened.MemberPath,
		Format:             raw.format,
		Authority:          authority,
		DatasetName:        dataset,
		PlatformName:       platform,
		PlatformOverride:   strings.TrimSpace(options.PlatformOverride) != "",
		LayoutHint:         layoutHint,
		Version:            raw.header.Version,
		Author:             raw.header.Author,
		GameCount:          raw.gameCount,
		MachineCount:       raw.machineCount,
		PlayableCount:      raw.playableCount,
		BIOSCount:          raw.biosCount,
		DeviceCount:        raw.deviceCount,
		MechanicalCount:    raw.mechanicalCount,
		ROMCount:           raw.romCount,
		DiskCount:          raw.diskCount,
		SampleCount:        raw.sampleCount,
		CloneLinkCount:     raw.cloneLinkCount,
		ROMDependencyCount: raw.romDependencyCount,
		HashCounts:         raw.hashCounts,
		SampleGames:        raw.sampleGames,
	}, nil
}

type openedDAT struct {
	Reader     io.Reader
	Close      func() error
	Container  string
	MemberPath string
}

func openDAT(path, memberPath string) (openedDAT, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".zip" {
		if strings.TrimSpace(memberPath) != "" {
			return openedDAT{}, fmt.Errorf("--member can only be used with zip DAT containers")
		}
		file, err := catalogfs.Open(path)
		if err != nil {
			return openedDAT{}, err
		}
		return openedDAT{
			Reader:    file,
			Close:     file.Close,
			Container: "file",
		}, nil
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return openedDAT{}, err
	}
	var candidates []*zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(file.Name)
		if strings.HasSuffix(name, ".dat") || strings.HasSuffix(name, ".xml") {
			candidates = append(candidates, file)
		}
	}
	if len(candidates) == 0 {
		reader.Close()
		return openedDAT{}, fmt.Errorf("zip does not contain a .dat or .xml member")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name < candidates[j].Name
	})
	member, err := selectDATMember(candidates, memberPath)
	if err != nil {
		reader.Close()
		return openedDAT{}, err
	}
	memberReader, err := member.Open()
	if err != nil {
		reader.Close()
		return openedDAT{}, err
	}
	return openedDAT{
		Reader: memberReader,
		Close: func() error {
			memberErr := memberReader.Close()
			zipErr := reader.Close()
			if memberErr != nil {
				return memberErr
			}
			return zipErr
		},
		Container:  "zip",
		MemberPath: member.Name,
	}, nil
}

func selectDATMember(candidates []*zip.File, requested string) (*zip.File, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return candidates[0], nil
	}
	normalized := normalizeMemberPath(requested)
	for _, candidate := range candidates {
		if normalizeMemberPath(candidate.Name) == normalized {
			return candidate, nil
		}
	}
	var baseMatches []*zip.File
	for _, candidate := range candidates {
		if pathpkg.Base(normalizeMemberPath(candidate.Name)) == pathpkg.Base(normalized) {
			baseMatches = append(baseMatches, candidate)
		}
	}
	if len(baseMatches) == 1 {
		return baseMatches[0], nil
	}
	if len(baseMatches) > 1 {
		return nil, fmt.Errorf("zip member %q is ambiguous; matching DAT/XML members: %s", requested, joinMemberNames(baseMatches))
	}
	var available []string
	for _, candidate := range candidates {
		available = append(available, candidate.Name)
	}
	return nil, fmt.Errorf("zip member %q not found; available DAT/XML members: %s", requested, strings.Join(available, ", "))
}

func joinMemberNames(members []*zip.File) string {
	var names []string
	for _, member := range members {
		names = append(names, member.Name)
	}
	return strings.Join(names, ", ")
}

func normalizeMemberPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return strings.ToLower(path)
}

func parse(reader io.Reader, sampleLimit int) (rawPlan, error) {
	decoder := xml.NewDecoder(reader)
	var result rawPlan
	var currentGame *GameSample
	var gameField string
	var inHeader bool
	var headerField string
	var rootSeen bool
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rawPlan{}, err
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
				} else if currentGame != nil && name == "description" {
					gameField = name
				}
			case "game", "machine":
				sample := GameSample{
					Name:       attr(token.Attr, "name"),
					BIOS:       attrYes(token.Attr, "isbios"),
					Device:     attrYes(token.Attr, "isdevice"),
					Runnable:   attr(token.Attr, "runnable"),
					Mechanical: attrYes(token.Attr, "ismechanical") || attrYes(token.Attr, "mechanical"),
				}
				sample.CloneOf = firstNonEmpty(attr(token.Attr, "cloneofid"), attr(token.Attr, "cloneof"))
				sample.ROMOf = attr(token.Attr, "romof")
				if sample.ROMOf != "" {
					result.romDependencyCount++
				}
				if sample.CloneOf != "" {
					result.cloneLinkCount++
				}
				result.gameCount++
				if name == "machine" {
					result.machineCount++
					if sample.BIOS {
						result.biosCount++
					}
					if sample.Device {
						result.deviceCount++
					}
					if sample.Mechanical {
						result.mechanicalCount++
					}
					if isPlayableMachine(sample) {
						result.playableCount++
					}
				}
				if sampleLimit == 0 {
					currentGame = nil
					break
				}
				if len(result.sampleGames) < sampleLimit {
					result.sampleGames = append(result.sampleGames, sample)
					currentGame = &result.sampleGames[len(result.sampleGames)-1]
				} else {
					currentGame = nil
				}
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
			case "disk":
				result.diskCount++
			case "sample":
				result.sampleCount++
			}
		case xml.CharData:
			value := strings.TrimSpace(string(token))
			if value == "" {
				continue
			}
			if headerField == "" {
				if currentGame != nil && gameField == "description" {
					currentGame.Description += value
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
				if token.Name.Local == "description" {
					gameField = ""
				}
			case "game", "machine":
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

func formatFromRoot(root string) string {
	switch root {
	case "datafile":
		return "clrmamepro-xml"
	case "mame":
		return "mame-listxml"
	default:
		if root == "" {
			return "xml"
		}
		return root
	}
}

func attr(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func attrYes(attrs []xml.Attr, name string) bool {
	return strings.EqualFold(attr(attrs, name), "yes")
}

func isPlayableMachine(sample GameSample) bool {
	if sample.BIOS || sample.Device {
		return false
	}
	return !strings.EqualFold(sample.Runnable, "no")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultDatasetForFormat(format string) string {
	if format == "mame-listxml" {
		return "MAME"
	}
	return ""
}

func inferAuthority(dataset, author, format string, paths ...string) string {
	if format == "mame-listxml" {
		return "mame"
	}
	// An explicit dataset/author identifies the authority before a storage path.
	// In particular a TOSEC snapshot must never inherit No-Intro verification.
	for _, text := range []string{strings.ToLower(dataset + " " + author), strings.ToLower(strings.Join(paths, " "))} {
		switch {
		case strings.Contains(text, "no-intro") || strings.Contains(text, "nointro"):
			return "no-intro"
		case strings.Contains(text, "redump"):
			return "redump"
		case strings.Contains(text, "tosec"):
			return "tosec"
		case strings.Contains(text, "fbneo") || strings.Contains(text, "finalburn") || strings.Contains(text, "final burn"):
			return "fbneo"
		case strings.Contains(text, "mame"):
			return "mame"
		}
	}
	return ""
}

func isArcadeDataset(dataset, authority, format string) bool {
	if format == "mame-listxml" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(dataset))
	switch authority {
	case "mame":
		return strings.Contains(lower, "mame roms")
	case "fbneo":
		return strings.Contains(lower, "arcade")
	default:
		return strings.HasPrefix(lower, "arcade - ")
	}
}

func datasetNameFromPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	name = strings.TrimSpace(name)
	if i := strings.Index(name, " (20"); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	return name
}

func inferPlatform(dataset string) (string, string) {
	platform := strings.TrimSpace(dataset)
	var hints []string
	for {
		start := strings.LastIndex(platform, "(")
		end := strings.LastIndex(platform, ")")
		if start < 0 || end != len(platform)-1 || start > end {
			break
		}
		value := strings.TrimSpace(platform[start+1 : end])
		if !isLayoutQualifier(value) {
			break
		}
		hints = append([]string{value}, hints...)
		platform = strings.TrimSpace(platform[:start])
	}
	return platform, strings.Join(hints, ", ")
}

func isLayoutQualifier(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bigendian", "byte-swapped", "byteswapped", "littleendian", "decrypted", "encrypted", "headered", "unheadered":
		return true
	default:
		return false
	}
}
