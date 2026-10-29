package exporter

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"rommeta/internal/platformnames"
)

// NormalizeTarget validates the local rendering choice. Catalog/API resolution
// and selection profiles retain the existing shared export contract.
func NormalizeTarget(target string) (string, error) {
	if target == "" {
		return "es-de", nil
	}
	if target != "es-de" && target != "launchbox" {
		return "", fmt.Errorf("export target must be es-de or launchbox")
	}
	return target, nil
}

func launchBoxPlatform(system string) string {
	return platformnames.ForSource(systems[system].Platform, platformnames.SourceLaunchBox)
}

func outputXMLPath(plan *Plan) string {
	if plan.Target == "launchbox" {
		return filepath.ToSlash(filepath.Join("Data", "Platforms", launchBoxPlatform(plan.System)+".xml"))
	}
	return filepath.ToSlash(filepath.Join("gamelists", plan.System, "gamelist.xml"))
}

// launchBoxID is a UUIDv5 over the platform and actual launch path, independent
// of title, metadata, output directory and system aliases.
func launchBoxID(plan *Plan, record Record) string {
	path := filepath.ToSlash(filepath.Clean(filepath.Join(plan.ROMRoot, filepath.FromSlash(launchBoxFile(record)))))
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	// RFC 4122 URL namespace.
	namespace := []byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	h := sha1.New()
	h.Write(namespace)
	h.Write([]byte("rommeta:launchbox:" + launchBoxPlatform(plan.System) + "\n" + path))
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func launchBoxFile(record Record) string {
	if record.LaunchFile != "" {
		return record.LaunchFile
	}
	return record.ROM
}

var launchBoxImages = map[string]string{
	"cover": "Box - Front", "back-cover": "Box - Back", "box-3d": "Box - 3D",
	"screenshot": "Screenshot - Gameplay", "title-screen": "Screenshot - Game Title",
	"logo": "Clear Logo", "fanart": "Fanart - Background",
}

// ProjectTarget changes only the frontend layout after local/API identity and
// media validation. API-supplied output paths never become LaunchBox paths.
func ProjectTarget(plan *Plan, target string) error {
	target, err := NormalizeTarget(target)
	if err != nil {
		return err
	}
	if _, ok := systems[plan.System]; !ok {
		return fmt.Errorf("unknown export system: %s", plan.System)
	}
	plan.Target = target
	for i := range plan.Records {
		record := &plan.Records[i]
		if !ValidInputPath(record.ROM) || !ValidInputPath(launchBoxFile(*record)) {
			return fmt.Errorf("invalid launch path")
		}
		for role, media := range record.Media {
			ext := filepath.Ext(media.SourcePath)
			path, err := ExportOutputPath(plan.System, *record, role, ext)
			if err != nil {
				return err
			}
			if target == "launchbox" {
				name := launchBoxID(plan, *record) + "-01" + ext
				platform := launchBoxPlatform(plan.System)
				switch role {
				case "video":
					path = filepath.ToSlash(filepath.Join("Videos", platform, name))
				case "manual":
					path = filepath.ToSlash(filepath.Join("Manuals", platform, name))
				default:
					category, ok := launchBoxImages[role]
					if !ok {
						return fmt.Errorf("unsupported LaunchBox media role: %s", role)
					}
					path = filepath.ToSlash(filepath.Join("Images", platform, category, name))
				}
			}
			media.OutputPath = path
			record.Media[role] = media
		}
	}
	return FinalizeExport(plan)
}

func launchBoxPlayers(value string) int {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) > 2 {
		return 0
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil || first < 1 {
		return 0
	}
	if len(parts) == 1 {
		return first
	}
	last, err := strconv.Atoi(parts[1])
	if err != nil || last < first {
		return 0
	}
	return last
}

type launchBoxGame struct {
	ID              string `xml:"ID"`
	Title           string `xml:"Title"`
	ApplicationPath string `xml:"ApplicationPath"`
	Platform        string `xml:"Platform"`
	Emulation       bool   `xml:"Emulation"`
	Emulator        string `xml:"Emulator"`
	Installed       bool   `xml:"Installed"`
	Notes           string `xml:"Notes,omitempty"`
	ReleaseDate     string `xml:"ReleaseDate,omitempty"`
	Developer       string `xml:"Developer,omitempty"`
	Publisher       string `xml:"Publisher,omitempty"`
	MaxPlayers      int    `xml:"MaxPlayers,omitempty"`
	Genre           string `xml:"Genre,omitempty"`
	Version         string `xml:"Version,omitempty"`
	VideoPath       string `xml:"VideoPath,omitempty"`
	ManualPath      string `xml:"ManualPath,omitempty"`
}
type launchBoxList struct {
	XMLName xml.Name        `xml:"LaunchBox"`
	Games   []launchBoxGame `xml:"Game"`
}

// RenderLaunchBox writes native library records; database IDs are deliberately
// omitted because provider snapshot IDs are not verified local library bindings.
func RenderLaunchBox(plan *Plan) ([]byte, error) {
	list := launchBoxList{Games: []launchBoxGame{}}
	seen := map[string]bool{}
	for _, r := range plan.Records {
		if r.Hidden || r.Status == "unsupported" || systems[plan.System].Arcade && r.Status != "matched" {
			continue
		}
		if !ValidInputPath(launchBoxFile(r)) {
			return nil, fmt.Errorf("invalid launch path")
		}
		get := func(key string) string { return r.Fields[key].Value }
		date := ""
		if parsed, err := time.Parse("2006-01-02", get("release_date")); err == nil {
			date = parsed.Format("2006-01-02")
		}
		id := launchBoxID(plan, r)
		if seen[id] {
			return nil, fmt.Errorf("duplicate LaunchBox launch identity")
		}
		seen[id] = true
		// Media paths are relative to the LaunchBox installation; ROM paths stay
		// absolute because the package does not copy games.
		list.Games = append(list.Games, launchBoxGame{
			ID: id, Title: get("title"),
			ApplicationPath: filepath.Join(plan.ROMRoot, filepath.FromSlash(launchBoxFile(r))),
			Platform:        launchBoxPlatform(plan.System), Emulation: true,
			Emulator: "00000000-0000-0000-0000-000000000000", Installed: true,
			Notes: get("description"), ReleaseDate: date, Developer: get("developer"),
			Publisher: get("publisher"), MaxPlayers: launchBoxPlayers(get("players")), Genre: strings.ReplaceAll(get("genres"), " / ", "; "),
			Version: r.Release, VideoPath: r.Media["video"].OutputPath, ManualPath: r.Media["manual"].OutputPath,
		})
	}
	body, err := xml.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func launchBoxInstructions() string {
	return `RomMeta LaunchBox staging package (local/personal)

Open preview.html and review report.txt and manifest.json before installation.
Close LaunchBox and Big Box. Back up Data and any media you will change.
For NEW platforms, copy Data/Platforms/*.xml into LaunchBox/Data/Platforms/.
Copy Images, Videos and Manuals into the corresponding LaunchBox directories.
Do not overwrite an existing platform XML: it may contain favorites, play history,
manual metadata, emulator overrides and other games. In-place merging/updating is
not implemented. Use a new platform or merge deliberately outside this exporter.
If a platform does not appear, create its exact name in Tools > Manage > Platforms
and select the matching Scrape As value. Existing platform folder overrides need
the same media destinations or a manual adjustment.

Emulators are left unassigned. Select/configure the appropriate emulator in
LaunchBox before playing. The package does not install emulators or BIOS.
ROMs/catalogs are not copied, renamed or changed. ApplicationPath references the
original absolute file path, so export on the Windows machine that will launch it.
Keep network drives/shares available under the same path.
Directory-style M3U inputs launch the actual inner M3U file as one library card.
Disc order stays in the original playlist; swapping requires emulator support.
Single CHD/ISO files remain separate launches; disc sets are not guessed by title.
CUE/BIN, GDI, PBP and ordinary flat M3U inputs retain the shared reader limits.

Images use the local game UUID for matching, keeping same-title releases separate.
VideoPath and ManualPath point into the installed media directories.
IDs are deterministic for the platform and launch path, independent of metadata
and package directory. Moving ROMs changes IDs. This is not an update/merge tool.
Unknown/ambiguous inputs retain filename-only metadata; unsupported inputs and
unbound arcade sets are omitted from XML and remain visible in the report.
No provider content is downloaded as an online fallback.
ScummVM catalog cards alone are not launchable game folders and are not exported.

https://feedback.launchbox-app.com/en/help/articles/8972270-multi-disc-games-and-m3u-playlists
https://feedback.launchbox-app.com/en/help/articles/7897312-importing-and-managing-scummvm-games
`
}
