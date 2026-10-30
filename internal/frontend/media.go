package frontend

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var imageTypes = map[string]bool{"Box - Front": true, "Box - Back": true, "Box - 3D": true, "Clear Logo": true, "Screenshot - Gameplay": true, "Screenshot - Game Title": true, "Fanart - Background": true}
var imageNumber = regexp.MustCompile(`-\d+$`)

func imageName(s string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, s))
}
func hasArt(dir string, aliases []string) (bool, error) {
	found := false
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
		for _, alias := range aliases {
			if alias != "" && base == imageName(alias) {
				found = true
			}
		}
		return nil
	})
	return found, err
}
func (p *Plan) media(platform, sourceID, targetID string, existing, incoming map[string]string) (int, map[string]string, error) {
	added := 0
	fields := map[string]string{}
	kinds := make([]string, 0, len(imageTypes))
	for kind := range imageTypes {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		relative := "Images/" + platform + "/" + kind
		entries, err := p.packageEntries(relative)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, nil, err
		}
		targetDir, err := safeJoin(p.Root, relative)
		if err != nil {
			return 0, nil, err
		}
		present, err := hasArt(targetDir, []string{targetID, existing["Title"], existing["LaunchBoxDbId"]})
		if err != nil {
			return 0, nil, err
		}
		if present {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), sourceID+"-") {
				continue
			}
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
		old, err := digest(name)
		if err != nil {
			return 0, nil, err
		}
		input, err := p.packageProof(source)
		if err != nil {
			return 0, nil, err
		}
		if old.exists && old != input {
			p.Warnings = append(p.Warnings, "Preserved existing media: "+target)
			p.guards[name] = old
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
