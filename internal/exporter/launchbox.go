package exporter

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"path/filepath"
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

// launchBoxID is stable for a catalog card regardless of its primary version.
func launchBoxID(plan *Plan, record Record) string {
	if record.Status == "matched" && record.Game != "" {
		return launchBoxUUID(launchBoxPlatform(plan.System) + "\ncard:" + record.Game)
	}
	return launchBoxUUID(launchBoxPlatform(plan.System) + "\ninput:" + launchBoxAbsolute(plan, record))
}

func launchBoxUUID(identity string) string {
	namespace := []byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	h := sha1.New()
	h.Write(namespace)
	h.Write([]byte("rommeta:launchbox:" + identity))
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
	plan.LaunchBox = nil
	for i := range plan.Records {
		record := &plan.Records[i]
		if !ValidInputPath(record.ROM) || !ValidInputPath(launchBoxFile(*record)) {
			return fmt.Errorf("invalid launch path")
		}
		for role, media := range record.Media {
			path, err := ExportOutputPath(plan.System, *record, role, filepath.Ext(media.SourcePath))
			if err != nil {
				return err
			}
			media.OutputPath = path
			record.Media[role] = media
		}
	}
	if target == "launchbox" {
		projection, err := buildLaunchBoxProjection(plan)
		if err != nil {
			return err
		}
		plan.LaunchBox = projection
		primary := map[int]string{}
		for _, group := range projection.Groups {
			for _, version := range group.Versions {
				if version.ID == group.Primary {
					primary[version.Records[0]] = group.ID
				}
			}
		}
		for i := range plan.Records {
			record := &plan.Records[i]
			for role, media := range record.Media {
				platform := launchBoxPlatform(plan.System)
				ext := filepath.Ext(media.SourcePath)
				path := filepath.Join("VersionMedia", platform, launchBoxVersionID(plan, *record), role+ext)
				if id, ok := primary[i]; ok {
					name := id + "-01" + ext
					switch role {
					case "video":
						path = filepath.Join("Videos", platform, name)
					case "manual":
						path = filepath.Join("Manuals", platform, name)
					default:
						path = filepath.Join("Images", platform, launchBoxImages[role], name)
					}
				}
				media.OutputPath = filepath.ToSlash(path)
				record.Media[role] = media
			}
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
	XMLName  xml.Name               `xml:"LaunchBox"`
	Games    []launchBoxGame        `xml:"Game"`
	Versions []launchBoxApplication `xml:"AdditionalApplication"`
}

// RenderLaunchBox writes native library records; database IDs are deliberately
// omitted because provider snapshot IDs are not verified local library bindings.
func RenderLaunchBox(plan *Plan) ([]byte, error) {
	projection, err := buildLaunchBoxProjection(plan)
	if err != nil {
		return nil, err
	}
	list := launchBoxList{Games: []launchBoxGame{}, Versions: []launchBoxApplication{}}
	for _, group := range projection.Groups {
		for priority, version := range group.Versions {
			r := plan.Records[version.Records[0]]
			get := func(key string) string { return r.Fields[key].Value }
			date := ""
			if parsed, err := time.Parse("2006-01-02", get("release_date")); err == nil {
				date = parsed.Format("2006-01-02")
			}
			region := ""
			if r.TitleContext != nil {
				region = strings.Join(r.TitleContext.Regions, "; ")
			}
			list.Versions = append(list.Versions, launchBoxApplication{
				ID: version.ID, GameID: group.ID, Name: version.Name, Version: version.Name,
				ApplicationPath: version.Path, Section: "Version", UseEmulator: true,
				EmulatorID: "00000000-0000-0000-0000-000000000000", Installed: true,
				Priority: priority, Region: region, ReleaseDate: date, Developer: get("developer"), Publisher: get("publisher"),
			})
			if version.ID != group.Primary {
				continue
			}
			list.Games = append(list.Games, launchBoxGame{
				ID: group.ID, Title: group.Title, ApplicationPath: version.Path,
				Platform: launchBoxPlatform(plan.System),
				Emulator: "00000000-0000-0000-0000-000000000000", Installed: true,
				Notes: get("description"), ReleaseDate: date, Developer: get("developer"),
				Publisher: get("publisher"), MaxPlayers: launchBoxPlayers(get("players")),
				Genre: strings.ReplaceAll(get("genres"), " / ", "; "), Version: version.Name,
				VideoPath: r.Media["video"].OutputPath, ManualPath: r.Media["manual"].OutputPath,
			})
		}
	}
	body, err := xml.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func launchBoxInstructions() string {
	return `RomMeta LaunchBox staging package (local/personal)

Review preview.html, report.txt and manifest.json. Matched catalog games become
one native card; every playable release, translation and hack is an Additional
Version, including the primary launch. Unknown/ambiguous files stay independent.
The profile's launchbox kinds/regions/languages lists select the primary version.
Card metadata and native artwork come from that primary input. Alternate selected
media remain in VersionMedia for review, outside LaunchBox's automatic image scan.

Close LaunchBox and Big Box; back up Data before installation.
For NEW platforms, copy Data/Platforms/*.xml into LaunchBox/Data/Platforms/.
Copy Images, Videos, Manuals and Playlists into their matching installation folders.
Do not overwrite an existing platform XML: it contains other games, owner fields,
favorites, statistics, emulator choices and version bindings. In-place merging
is not implemented. Card IDs now follow catalog identity rather than individual
launch paths; migration from the first ungrouped exporter requires deliberate review.
Use the Platform sidebar filter if a category view does not show new platforms.
Custom media-folder overrides need adjusted destinations.

Emulators remain unassigned on both the card and all versions. Configure a
platform default or review every version's emulator before playing.
ROMs/catalogs are never copied or changed. Supplied files and M3Us use original
absolute paths. Generated M3Us live in Playlists and reference absolute disc paths;
copy that folder too and keep network mappings available.
Each M3U belongs to one region/revision/modified version. Versions deliberately
have no global Disc number: automatic LaunchBox M3U generation could mix editions.
The selected emulator receives an existing M3U. Disc control and save compatibility
still need actual emulator verification.

Official catalog disc names differing only by one Disc/Disk token may form a
generated M3U when selected disc numbers are exactly 1..N (2..32), without gaps or
duplicates; any declared total must agree. Other qualifiers remain significant.
This checks the observed sequence, not full catalog/set completeness. Review
warnings, especially when filtering/limiting inputs. Unassembled numbered discs
remain separate versions and are never fed to automatic global M3U generation.
Prepared translated/hacked multidisc sets require an explicit supplied M3U.
Directory-style supplied M3Us preserve their original member order. Ordinary flat
M3U, CUE/BIN, GDI and PBP retain the shared reader limits.

manifest.json retains every input/proof, selected media, grouping and primary
decision, including references to all source members. No new ROM identity is
invented. Unsupported inputs and unbound arcade sets are omitted from native XML.
No provider fallback requests, emulator/BIOS install or ScummVM launches are added.

https://feedback.launchbox-app.com/en/help/articles/8972270-multi-disc-games-and-m3u-playlists
`
}
