package launchbox

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
)

// LaunchBox is primarily organized under Arcade, with a few hardware-specific
// sections. These are provider names, never catalog directories.
var arcadeLaunchBoxPlatforms = map[string]string{
	"Arcade - SNK - Neo Geo":   "SNK Neo Geo MVS",
	"Arcade - Sega - Naomi":    "Sega Naomi",
	"Arcade - Sega - Naomi 2":  "Sega Naomi 2",
	"Arcade - Sega - Model 1":  "Sega Model 1",
	"Arcade - Sega - Model 2":  "Sega Model 2",
	"Arcade - Sega - Model 3":  "Sega Model 3",
	"Arcade - Sega - Triforce": "Sega Triforce",
}

type mameName struct {
	FileName    string
	Name        string
	IsBootleg   bool
	IsHack      bool
	IsNonArcade bool
}

// Mame.xml is optional in old snapshots. Its names supplement matching but
// its CloneOf field is deliberately not followed across catalog cards.
func readMAMENames(zipPath string) (map[string]mameName, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	result := map[string]mameName{}
	for _, member := range archive.File {
		if !strings.EqualFold(filepath.Base(member.Name), "Mame.xml") {
			continue
		}
		reader, err := member.Open()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		decoder := xml.NewDecoder(reader)
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "MameFile" {
				continue
			}
			var entry mameName
			if err := decoder.DecodeElement(&entry, &start); err != nil {
				return nil, err
			}
			if entry.FileName != "" {
				result[entry.FileName] = entry
			}
		}
	}
	return result, nil
}

func matchArcadeMetadata(index map[string]map[string][]releaseMetadata, game catalogGame, release catalogRelease, names map[string]mameName) (releaseMetadata, bool) {
	if entry, ok := names[release.MAMESet]; ok && !entry.IsNonArcade {
		release.MAMETitle = entry.Name
		if entry.IsBootleg {
			release.Tags = append(release.Tags, "bootleg")
		}
		if entry.IsHack {
			release.Tags = append(release.Tags, "hack")
		}
	}
	if platform := arcadeLaunchBoxPlatforms[game.ArcadePlatform]; platform != "" {
		if metadata, ok := matchMetadata(index[platform], release, ""); ok {
			return metadata, true
		}
	}
	return matchMetadata(index["Arcade"], release, "")
}

// A generic original-game record is not evidence for a separate bootleg.
func arcadeCandidateAllowed(release catalogRelease, candidate releaseMetadata) bool {
	// These named ports have their own LaunchBox records inside Arcade. Base
	// title keys alone erase their hardware suffix and make the original tie.
	localTitle := looseTitleKey(release.Title)
	sourceTitle := looseTitleKey(candidate.Title)
	for _, port := range []string{"mega tech", "mega play", "playchoice"} {
		if strings.Contains(localTitle, port) != strings.Contains(sourceTitle, port) {
			return false
		}
	}
	text := strings.ToLower(strings.Join(append([]string{candidate.Title, candidate.Developer, candidate.Publisher}, candidate.AlternateTitles...), " "))
	for _, tag := range release.Tags {
		if (tag == "bootleg" || tag == "hack") && !strings.Contains(text, tag) {
			return false
		}
	}
	return true
}
