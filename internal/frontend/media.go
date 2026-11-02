package frontend

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var imageTypes = []string{"Box - 3D", "Box - Back", "Box - Front", "Clear Logo", "Fanart - Background", "Screenshot - Game Title", "Screenshot - Gameplay"}
var imageNumber = regexp.MustCompile(`-\d+$`)

func imageName(s string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, s))
}
func (p *Plan) hasArt(dir string, aliases []string) (bool, error) {
	if p.artDirs == nil {
		p.artDirs = map[string]map[string]bool{}
	}
	stems, loaded := p.artDirs[dir]
	if !loaded {
		stems = map[string]bool{}
		err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return fs.SkipDir
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("linked media path is unsupported: %s", name)
			}
			if entry.IsDir() {
				return nil
			}
			base := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			base = imageNumber.ReplaceAllString(base, "")
			stems[base] = true
			return nil
		})
		if err != nil {
			return false, err
		}
		p.artDirs[dir] = stems
	}
	for _, alias := range aliases {
		if alias != "" && stems[imageName(alias)] {
			return true, nil
		}
	}
	return false, nil
}

func (p *Plan) media(platform, sourceID, targetID string, existing, incoming map[string]string) (int, map[string]string, error) {
	added := 0
	fields := map[string]string{}
	for _, kind := range imageTypes {
		relative := "Images/" + platform + "/" + kind
		entries, err := p.packageMedia(relative, sourceID)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, nil, err
		}
		if len(entries) == 0 {
			continue
		}
		targetDir, err := safeJoin(p.Root, relative)
		if err != nil {
			return 0, nil, err
		}
		present, err := p.hasArt(targetDir, []string{targetID, existing["Title"], existing["LaunchBoxDbId"]})
		if err != nil {
			return 0, nil, err
		}
		if present {
			continue
		}
		for _, e := range entries {
			source, err := safeJoin(p.Package, relative+"/"+e.Name())
			if err != nil {
				return 0, nil, err
			}
			target := relative + "/" + targetID + e.Name()[len(sourceID):]
			changed, err := p.write(target, nil, source)
			if err != nil {
				return 0, nil, err
			}
			if changed {
				added++
			}
		}
	}
	for _, key := range []string{"VideoPath", "ManualPath"} {
		raw := incoming[key]
		if raw == "" || !empty(key, existing[key]) {
			continue
		}
		relative := strings.ReplaceAll(raw, "\\", "/")
		expected := "Videos/"
		if key == "ManualPath" {
			expected = "Manuals/"
		}
		if !strings.HasPrefix(relative, expected+platform+"/"+sourceID+"-") {
			return 0, nil, fmt.Errorf("invalid package %s", key)
		}
		source, err := safeJoin(p.Package, relative)
		if err != nil {
			return 0, nil, err
		}
		target := strings.Replace(relative, sourceID, targetID, 1)
		name, err := safeJoin(p.Root, target)
		if err != nil {
			return 0, nil, err
		}
		old, err := p.observe(name)
		if err != nil {
			return 0, nil, err
		}
		if old.exists {
			input, err := p.packageProof(source)
			if err != nil {
				return 0, nil, err
			}
			if old != input {
				p.Warnings = append(p.Warnings, "Preserved existing media: "+target)
			}
			fields[key] = target
			continue
		}
		changed, err := p.write(target, nil, source)
		if err != nil {
			return 0, nil, err
		}
		if changed {
			added++
		}
		fields[key] = target
	}
	return added, fields, nil
}
func (p *Plan) playlist(raw string) error {
	relative := strings.ReplaceAll(raw, "\\", "/")
	if !strings.HasPrefix(relative, "Playlists/") {
		return nil
	}
	source, err := safeJoin(p.Package, relative)
	if err != nil {
		return err
	}
	_, err = p.write(relative, nil, source)
	return err
}
