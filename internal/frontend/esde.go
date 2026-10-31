package frontend

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type esdeManifest struct {
	System  string         `json:"system"`
	ROMRoot string         `json:"rom_root"`
	Systems []esdeManifest `json:"systems"`
	Records []struct {
		ROM   string `json:"rom"`
		Media map[string]struct {
			OutputPath string `json:"output_path"`
		} `json:"media"`
	} `json:"records"`
}

// ES-DE can place alternativeEmulator beside gameList and uses XML fragments
// for settings. Wrap only while parsing and retain every original outer byte.
func fragments(body []byte) (*document, []byte, error) {
	var prefix []byte
	trimmed := bytes.TrimLeft(body, " \r\n\t\xef\xbb\xbf")
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		end := bytes.Index(trimmed, []byte("?>"))
		if end < 0 {
			return nil, nil, fmt.Errorf("invalid XML declaration")
		}
		n := len(body) - len(trimmed) + end + 2
		prefix = body[:n]
		body = body[n:]
	}
	wrapped := append([]byte("<RomMetaFragments>"), body...)
	wrapped = append(wrapped, []byte("</RomMetaFragments>")...)
	doc, err := parse(wrapped, "RomMetaFragments")
	return doc, prefix, err
}
func unfragment(doc *document, prefix []byte, replacements map[int][]byte) []byte {
	body := doc.edit(replacements, nil)
	result := append([]byte{}, prefix...)
	return append(result, body[len("<RomMetaFragments>"):len(body)-len("</RomMetaFragments>")]...)
}
func gamelist(body []byte) (*document, *document, int, []byte, error) {
	outer, prefix, err := fragments(body)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	index := -1
	for i, e := range outer.children {
		if e.name == "gameList" {
			if index >= 0 {
				return nil, nil, 0, nil, fmt.Errorf("multiple gameList elements")
			}
			index = i
		}
	}
	if index < 0 {
		return nil, nil, 0, nil, fmt.Errorf("missing gameList")
	}
	inner, err := parse(outer.children[index].raw, "gameList")
	return outer, inner, index, prefix, err
}
func esdePath(root, raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if raw == "" {
		return ""
	}
	if !path.IsAbs(raw) && !(len(raw) > 2 && raw[1] == ':') {
		raw = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(raw)))
	}
	result := path.Clean(raw)
	if strings.HasPrefix(raw, "//") {
		result = "/" + result
	}
	if runtime.GOOS == "windows" {
		result = strings.ToLower(result)
	}
	return result
}
func esdeEntries(doc *document, romRoot string) (map[string]native, error) {
	result := map[string]native{}
	for i, e := range doc.children {
		if e.name != "game" && e.name != "folder" {
			continue
		}
		fields, err := e.fields()
		if err != nil {
			return nil, err
		}
		key := esdePath(romRoot, fields["path"])
		if key == "" {
			return nil, fmt.Errorf("gamelist entry has no path")
		}
		if _, ok := result[key]; ok {
			return nil, fmt.Errorf("duplicate existing gamelist launch path: %s", fields["path"])
		}
		result[key] = native{i, e, fields}
	}
	return result, nil
}
func expandESDE(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, value[2:])
	}
	if strings.Contains(value, "%") || !filepath.IsAbs(filepath.FromSlash(strings.ReplaceAll(value, "\\", "/"))) {
		return "", fmt.Errorf("cannot resolve configured ES-DE path %q; use an explicit --media-root for media", value)
	}
	return prospectiveRoot(filepath.FromSlash(strings.ReplaceAll(value, "\\", "/")))
}
func (p *Plan) esdeSettings() (map[string]string, error) {
	settings := map[string]string{}
	for _, name := range []string{"settings/es_settings.xml", "es_settings.xml"} {
		body, err := p.read(p.Root, name, true)
		if err != nil {
			return nil, err
		}
		if body == nil {
			continue
		}
		doc, _, err := fragments(body)
		if err != nil {
			return nil, err
		}
		for _, e := range doc.children {
			if e.name != "string" && e.name != "bool" {
				continue
			}
			var setting struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value,attr"`
			}
			if err = xml.Unmarshal(e.raw, &setting); err != nil {
				return nil, err
			}
			if setting.Name == "MediaDirectory" || setting.Name == "ROMDirectory" || setting.Name == "LegacyGamelistFileLocation" {
				if _, ok := settings[setting.Name]; ok {
					return nil, fmt.Errorf("duplicate ES-DE setting %s", setting.Name)
				}
				settings[setting.Name] = setting.Value
			}
		}
		break
	}
	if settings["LegacyGamelistFileLocation"] == "true" {
		return nil, fmt.Errorf("legacy gamelists beside ROMs are unsupported; use the ES-DE application data directory")
	}
	if p.MediaRoot == "" {
		var err error
		if settings["MediaDirectory"] != "" {
			value := settings["MediaDirectory"]
			if strings.Contains(value, "%ROMPATH%") {
				base, e := expandESDE(settings["ROMDirectory"])
				if e != nil {
					return nil, e
				}
				if base == "" {
					return nil, fmt.Errorf("MediaDirectory needs a configured ROMDirectory or explicit --media-root")
				}
				value = strings.ReplaceAll(value, "%ROMPATH%", base)
			}
			p.MediaRoot, err = expandESDE(value)
		} else {
			p.MediaRoot, err = safeJoin(p.Root, "downloaded_media")
		}
		if err != nil {
			return nil, err
		}
	}
	if inside(p.Package, p.MediaRoot) || inside(p.MediaRoot, p.Package) {
		return nil, fmt.Errorf("media and package directories must not overlap")
	}
	return settings, nil
}
func (p *Plan) validateESDEROMRoot(system, romRoot string, settings map[string]string) error {
	configured, err := expandESDE(settings["ROMDirectory"])
	if err != nil {
		return err
	}
	expected := ""
	if configured != "" {
		expected = filepath.Join(configured, system)
	}
	body, err := p.read(p.Root, "custom_systems/es_systems.xml", true)
	if err != nil {
		return err
	}
	if body != nil {
		doc, err := parse(body, "systemList")
		if err != nil {
			return err
		}
		seen := false
		for _, e := range doc.children {
			if e.name != "system" {
				continue
			}
			var entry struct {
				Names []string `xml:"name"`
				Paths []string `xml:"path"`
			}
			if err = xml.Unmarshal(e.raw, &entry); err != nil {
				return err
			}
			if len(entry.Names) != 1 {
				return fmt.Errorf("invalid custom ES-DE system name")
			}
			if entry.Names[0] != system {
				continue
			}
			if seen || len(entry.Paths) != 1 {
				return fmt.Errorf("ambiguous custom ES-DE path for %s", system)
			}
			seen = true
			value := entry.Paths[0]
			if strings.Contains(value, "%ROMPATH%") && configured == "" {
				return fmt.Errorf("custom %s path needs a configured ROMDirectory", system)
			}
			value = strings.ReplaceAll(value, "%ROMPATH%", configured)
			expected, err = expandESDE(value)
			if err != nil {
				return err
			}
		}
	}
	if expected != "" && esdePath("", expected) != esdePath("", romRoot) {
		return fmt.Errorf("%s ROM root differs from the configured ES-DE launch directory: %s", system, expected)
	}
	return nil
}
func buildESDE(p *Plan, body []byte) error {
	var manifest esdeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return err
	}
	plans := manifest.Systems
	if len(plans) == 0 {
		plans = []esdeManifest{manifest}
	}
	settings, err := p.esdeSettings()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, plan := range plans {
		if plan.System == "" || plan.ROMRoot == "" {
			return fmt.Errorf("ES-DE package needs system and ROM root")
		}
		if seen[plan.System] {
			return fmt.Errorf("duplicate package system %s", plan.System)
		}
		seen[plan.System] = true
		if _, err = safeJoin(p.Root, "gamelists/"+plan.System+"/gamelist.xml"); err != nil {
			return err
		}
		if err = p.validateESDEROMRoot(plan.System, plan.ROMRoot, settings); err != nil {
			return err
		}
		relative := "gamelists/" + plan.System + "/gamelist.xml"
		incoming, err := p.read(p.Package, relative, false)
		if err != nil {
			return err
		}
		_, newList, _, _, err := gamelist(incoming)
		if err != nil {
			return err
		}
		old, err := p.read(p.Root, relative, true)
		if err != nil {
			return err
		}
		if old == nil {
			old = []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<gameList />\n")
		}
		outer, listing, index, prefix, err := gamelist(old)
		if err != nil {
			return err
		}
		existing, err := esdeEntries(listing, plan.ROMRoot)
		if err != nil {
			return err
		}
		addedEntries, err := esdeEntries(newList, plan.ROMRoot)
		if err != nil {
			return err
		}
		replacements := map[int][]byte{}
		var additions [][]byte
		for _, e := range newList.children {
			if e.name != "game" && e.name != "folder" {
				continue
			}
			fields, err := e.fields()
			if err != nil {
				return err
			}
			key := esdePath(plan.ROMRoot, fields["path"])
			change := Change{Platform: plan.System, Title: fields["name"], ID: fields["path"], Action: "add"}
			if current, ok := existing[key]; ok {
				change.Action = "unchanged"
				if current.node.name != e.name {
					change.Action = "conflict"
					p.Conflicts = append(p.Conflicts, plan.System+" / "+change.Title+": game/folder path conflict")
				} else {
					updates := fill(current.fields, fields, []string{"name", "desc", "releasedate", "developer", "publisher", "genre", "players"})
					if len(updates) > 0 {
						for key := range updates {
							change.Fields = append(change.Fields, key)
						}
						sort.Strings(change.Fields)
						raw, err := setFields(current.node, updates)
						if err != nil {
							return err
						}
						replacements[current.index] = raw
						change.Action = "enrich"
					}
				}
			} else {
				additions = append(additions, e.raw)
			}
			p.Changes = append(p.Changes, change)
		}
		// Index changes by position: appending to p.Changes can reallocate its storage.
		positions := map[string]int{}
		for i := len(p.Changes) - len(addedEntries); i < len(p.Changes); i++ {
			positions[esdePath(plan.ROMRoot, p.Changes[i].ID)] = i
		}
		for recordIndex, record := range plan.Records {
			p.reportProgress("plan library/media", plan.System+" / "+record.ROM, recordIndex, len(plan.Records))
			i, ok := positions[esdePath(plan.ROMRoot, record.ROM)]
			if !ok || p.Changes[i].Action == "conflict" {
				continue
			}
			roles := make([]string, 0, len(record.Media))
			for role := range record.Media {
				roles = append(roles, role)
			}
			sort.Strings(roles)
			for _, role := range roles {
				output := record.Media[role].OutputPath
				prefix := "downloaded_media/" + plan.System + "/"
				if !strings.HasPrefix(output, prefix) {
					return fmt.Errorf("invalid ES-DE media output: %s", output)
				}
				relative := strings.TrimPrefix(output, "downloaded_media/")
				target, err := safeJoin(p.MediaRoot, relative)
				if err != nil {
					return err
				}
				present, err := esdeMediaPresent(target)
				if err != nil {
					return err
				}
				if present {
					continue
				}
				source, err := safeJoin(p.Package, output)
				if err != nil {
					return err
				}
				changed, err := p.writeTo(p.MediaRoot, relative, nil, source)
				if err != nil {
					return err
				}
				if changed {
					p.Changes[i].Media++
					if p.Changes[i].Action == "unchanged" {
						p.Changes[i].Action = "enrich"
					}
				}
			}
		}
		p.reportProgress("plan library/media", plan.System, len(plan.Records), len(plan.Records))
		merged := listing.edit(replacements, additions)
		result := unfragment(outer, prefix, map[int][]byte{index: merged})
		if !bytes.Equal(old, result) {
			if _, err = p.write(relative, result, ""); err != nil {
				return err
			}
		}
	}
	if len(p.Changes) == 0 {
		return fmt.Errorf("package has no supported ES-DE entries")
	}
	return nil
}
func esdeMediaPresent(target string) (bool, error) {
	entries, err := os.ReadDir(filepath.Dir(target))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stem := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidate := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if runtime.GOOS == "windows" {
			if strings.EqualFold(stem, candidate) {
				return true, nil
			}
		} else if stem == candidate {
			return true, nil
		}
	}
	return false, nil
}
