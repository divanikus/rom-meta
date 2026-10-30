package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type identityMap struct {
	Schema int               `json:"schema_version"`
	Cards  map[string]string `json:"cards"`
}
type native struct {
	index  int
	node   element
	fields map[string]string
}

var guidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const statePath = "Data/RomMeta.launchbox.json"
const zeroGUID = "00000000-0000-0000-0000-000000000000"

func id(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func keyPath(root, s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\\", "/")
	if s == "" {
		return ""
	}
	if !path.IsAbs(s) && !(len(s) > 2 && s[1] == ':') {
		s = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(s)))
	}
	clean := path.Clean(s)
	if strings.HasPrefix(s, "//") {
		clean = "/" + clean
	}
	return strings.ToLower(clean)
}
func records(doc *document) (map[string]native, []native, error) {
	games := map[string]native{}
	var apps []native
	appIDs := map[string]bool{}
	for i, e := range doc.children {
		if e.name != "Game" && e.name != "AdditionalApplication" {
			continue
		}
		f, err := e.fields()
		if err != nil {
			return nil, nil, err
		}
		n := native{i, e, f}
		field := "ID"
		if e.name == "AdditionalApplication" {
			field = "Id"
		}
		key := id(f[field])
		if !guidPattern.MatchString(key) {
			return nil, nil, fmt.Errorf("invalid %s.%s", e.name, field)
		}
		if e.name == "Game" {
			if _, ok := games[key]; ok {
				return nil, nil, fmt.Errorf("duplicate game ID %s", key)
			}
			games[key] = n
		} else {
			if appIDs[key] {
				return nil, nil, fmt.Errorf("duplicate version/app ID %s", key)
			}
			appIDs[key] = true
			apps = append(apps, n)
		}
	}
	for _, app := range apps {
		if _, ok := games[id(app.fields["GameID"])]; !ok {
			return nil, nil, fmt.Errorf("orphan additional app %s", app.fields["Id"])
		}
	}
	return games, apps, nil
}
func empty(field, value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || field == "ReleaseDate" && strings.HasPrefix(value, "0001-01-01") || field == "MaxPlayers" && value == "0"
}

var metadataFields = []string{"Title", "Notes", "ReleaseDate", "Developer", "Publisher", "Genre", "MaxPlayers"}
var versionFields = []string{"Name", "Version", "Region", "ReleaseDate", "Developer", "Publisher"}

func fill(existing, incoming map[string]string, allowed []string) map[string]string {
	result := map[string]string{}
	for _, key := range allowed {
		if empty(key, existing[key]) && !empty(key, incoming[key]) {
			result[key] = incoming[key]
		}
	}
	return result
}
func orderedGames(games map[string]native) []string {
	keys := make([]string, 0, len(games))
	for k := range games {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildLaunchBox preserves native card identity and owner curation.
func buildLaunchBox(p *Plan) error {
	root, pkg := p.Root, p.Package
	if info, err := os.Stat(filepath.Join(root, "Data", "Platforms")); err != nil || !info.IsDir() {
		return fmt.Errorf("LaunchBox destination must contain Data/Platforms")
	}
	var body []byte
	var err error
	state := identityMap{Schema: 1, Cards: map[string]string{}}
	body, err = p.read(root, statePath, true)
	if err != nil {
		return err
	}
	if body != nil {
		if err = json.Unmarshal(body, &state); err != nil {
			return err
		}
		if state.Schema != 1 || state.Cards == nil {
			return fmt.Errorf("unsupported LaunchBox identity map")
		}
	}
	folders, err := p.read(root, "Data/Platforms.xml", true)
	if err != nil {
		return err
	}
	entries, err := p.packageEntries("Data/Platforms")
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".xml") {
			continue
		}
		relative := "Data/Platforms/" + entry.Name()
		body, err = p.read(pkg, relative, false)
		if err != nil {
			return err
		}
		incoming, err := parse(body, "LaunchBox")
		if err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		games, apps, err := records(incoming)
		if err != nil {
			return err
		}
		if len(games) == 0 {
			continue
		}
		count += len(games)
		platform := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		for _, game := range games {
			if game.fields["Platform"] != platform {
				return fmt.Errorf("platform XML/name mismatch")
			}
		}
		if err = checkFolders(folders, platform); err != nil {
			return err
		}
		old, err := p.read(root, relative, true)
		if err != nil {
			return err
		}
		if old == nil {
			old = []byte("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<LaunchBox>\n</LaunchBox>\n")
		}
		library, err := parse(old, "LaunchBox")
		if err != nil {
			return err
		}
		existing, versions, err := records(library)
		if err != nil {
			return err
		}
		replacements := map[int][]byte{}
		var additions [][]byte
		paths := map[string]map[string]bool{}
		addPath := func(raw, game string) {
			for _, key := range []string{keyPath(root, raw), p.launchIdentity(raw, false)} {
				if key != "" {
					if paths[key] == nil {
						paths[key] = map[string]bool{}
					}
					paths[key][game] = true
				}
			}
		}
		for game, n := range existing {
			addPath(n.fields["ApplicationPath"], game)
		}
		for _, n := range versions {
			if n.fields["Section"] == "Version" {
				addPath(n.fields["ApplicationPath"], id(n.fields["GameID"]))
			}
		}
		targets := map[string]string{}
		problems := map[string]string{}
		used := map[string][]string{}
		for _, game := range orderedGames(games) {
			candidates := map[string]bool{}
			if _, ok := existing[game]; ok {
				candidates[game] = true
			}
			if mapped := state.Cards[platform+"/"+game]; mapped != "" {
				mapped = id(mapped)
				if _, ok := existing[mapped]; !ok {
					problems[game] = "previously linked native card is missing"
				} else {
					candidates[mapped] = true
				}
			}
			observe := func(raw string) {
				for _, launch := range []string{keyPath(root, raw), p.launchIdentity(raw, true)} {
					for key := range paths[launch] {
						candidates[key] = true
					}
				}
			}
			observe(games[game].fields["ApplicationPath"])
			for _, v := range apps {
				if id(v.fields["GameID"]) == game {
					observe(v.fields["ApplicationPath"])
				}
			}
			if len(candidates) > 1 {
				problems[game] = "launch paths/IDs point to multiple existing cards; consolidate them in LaunchBox first"
				continue
			}
			target := game
			for candidate := range candidates {
				target = candidate
			}
			targets[game] = target
			used[target] = append(used[target], game)
		}
		for _, group := range used {
			if len(group) > 1 {
				for _, game := range group {
					problems[game] = "multiple catalog games point to one existing card"
				}
			}
		}
		// Check all version IDs/paths before planning any part of a card.
		for game, target := range targets {
			for _, v := range apps {
				if id(v.fields["GameID"]) != game {
					continue
				}
				if v.fields["Section"] != "Version" {
					return fmt.Errorf("package contains a non-version additional app")
				}
				matches := 0
				for _, oldVersion := range versions {
					if id(oldVersion.fields["GameID"]) == target && oldVersion.fields["Section"] == "Version" && p.sameLaunch(oldVersion.fields["ApplicationPath"], v.fields["ApplicationPath"]) {
						matches++
					}
					if id(v.fields["Id"]) == id(oldVersion.fields["Id"]) && (id(oldVersion.fields["GameID"]) != target || oldVersion.fields["Section"] != "Version" || !p.sameLaunch(oldVersion.fields["ApplicationPath"], v.fields["ApplicationPath"])) {
						problems[game] = "an existing additional app/version has a conflicting ID or launch path"
					}
				}
				if matches > 1 {
					problems[game] = "multiple existing versions have the same launch path"
				}
			}
		}
		for _, game := range orderedGames(games) {
			g := games[game]
			target := targets[game]
			change := Change{Platform: platform, Title: g.fields["Title"], ID: target, Action: "unchanged"}
			if reason := problems[game]; reason != "" {
				change.Action = "conflict"
				p.Conflicts = append(p.Conflicts, platform+" / "+change.Title+": "+reason)
				p.Changes = append(p.Changes, change)
				continue
			}
			firstFile := len(p.files)
			oldGame, found := existing[target]
			for _, oldVersion := range versions {
				if id(oldVersion.fields["GameID"]) == target && strings.TrimSpace(oldVersion.fields["Disc"]) != "" && oldVersion.fields["Disc"] != "0" {
					p.Warnings = append(p.Warnings, platform+" / "+change.Title+": preserved existing Disc values; review LaunchBox automatic M3U generation before using mixed editions")
					break
				}
			}
			fields := g.fields
			updates := map[string]string{}
			if found {
				fields = oldGame.fields
				updates = fill(fields, g.fields, metadataFields)
			} else {
				change.Action = "add"
				updates["ID"] = target
			}
			if found && keyPath(root, fields["ApplicationPath"]) == keyPath(root, g.fields["ApplicationPath"]) && empty("Version", fields["Version"]) && g.fields["Version"] != "" {
				updates["Version"] = g.fields["Version"]
			}
			mediaExisting := fields
			if !found {
				mediaExisting = map[string]string{}
			}
			addedMedia, mediaFields, err := p.media(platform, game, target, mediaExisting, g.fields)
			if err != nil {
				return err
			}
			change.Media = addedMedia
			for key, value := range mediaFields {
				updates[key] = value
			}
			if !found {
				if err = p.playlist(g.fields["ApplicationPath"]); err != nil {
					return err
				}
			}
			for key := range updates {
				if key != "ID" {
					change.Fields = append(change.Fields, key)
				}
			}
			sort.Strings(change.Fields)
			if found {
				if len(updates) > 0 {
					raw, err := setFields(oldGame.node, updates)
					if err != nil {
						return err
					}
					replacements[oldGame.index] = raw
				}
			} else {
				raw, err := setFields(g.node, updates)
				if err != nil {
					return err
				}
				additions = append(additions, raw)
			}
			for _, v := range apps {
				if id(v.fields["GameID"]) != game {
					continue
				}
				var matching *native
				for i := range versions {
					oldVersion := &versions[i]
					if id(oldVersion.fields["GameID"]) == target && oldVersion.fields["Section"] == "Version" && p.sameLaunch(oldVersion.fields["ApplicationPath"], v.fields["ApplicationPath"]) {
						matching = oldVersion
						break
					}
				}
				if matching != nil {
					if keyPath(root, matching.fields["ApplicationPath"]) == keyPath(root, v.fields["ApplicationPath"]) {
						if err = p.playlist(v.fields["ApplicationPath"]); err != nil {
							return err
						}
					}
					patch := fill(matching.fields, v.fields, versionFields)
					if len(patch) > 0 {
						for key := range patch {
							change.Fields = append(change.Fields, "Version: "+v.fields["Name"]+" / "+key)
						}
						raw, err := setFields(matching.node, patch)
						if err != nil {
							return err
						}
						replacements[matching.index] = raw
					}
					continue
				}
				patch := map[string]string{"GameID": target}
				// Reuse a working primary M3U when the ordered disc paths agree.
				if found && p.sameLaunch(fields["ApplicationPath"], v.fields["ApplicationPath"]) {
					patch["ApplicationPath"] = fields["ApplicationPath"]
					if keyPath(root, fields["ApplicationPath"]) == keyPath(root, v.fields["ApplicationPath"]) {
						if err = p.playlist(v.fields["ApplicationPath"]); err != nil {
							return err
						}
					}
				} else if err = p.playlist(v.fields["ApplicationPath"]); err != nil {
					return err
				}
				if found && v.fields["UseEmulator"] != "false" && guidPattern.MatchString(fields["Emulator"]) && id(fields["Emulator"]) != zeroGUID {
					patch["EmulatorId"] = fields["Emulator"]
					patch["UseEmulator"] = "true"
				}
				raw, err := setFields(v.node, patch)
				if err != nil {
					return err
				}
				additions = append(additions, raw)
				change.Versions++
			}
			for _, file := range p.files[firstFile:] {
				if strings.HasPrefix(file.path, "Playlists/") {
					change.Playlists++
				}
			}
			if found && (change.Playlists > 0 || len(change.Fields) > 0 || change.Versions > 0 || change.Media > 0) {
				change.Action = "enrich"
			}
			sort.Strings(change.Fields)
			state.Cards[platform+"/"+game] = target
			p.Changes = append(p.Changes, change)
		}
		result := library.edit(replacements, additions)
		if !bytes.Equal(result, old) {
			if _, err = p.write(relative, result, ""); err != nil {
				return err
			}
		}
	}
	if count == 0 {
		return fmt.Errorf("package contains no LaunchBox game cards")
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if _, err = p.write(statePath, append(data, '\n'), ""); err != nil {
		return err
	}
	return nil
}

func checkFolders(body []byte, platform string) error {
	if body == nil {
		return nil
	}
	doc, err := parse(body, "LaunchBox")
	if err != nil {
		return err
	}
	for _, e := range doc.children {
		if e.name != "PlatformFolder" {
			continue
		}
		f, err := e.fields()
		if err != nil {
			return err
		}
		if !strings.EqualFold(f["Platform"], platform) {
			continue
		}
		expected := ""
		if imageTypes[f["MediaType"]] {
			expected = "Images/" + platform + "/" + f["MediaType"]
		} else if f["MediaType"] == "Video" {
			expected = "Videos/" + platform
		} else if f["MediaType"] == "Manual" {
			expected = "Manuals/" + platform
		}
		folder := strings.Trim(strings.ReplaceAll(f["FolderPath"], "\\", "/"), "/")
		if expected != "" && folder != "" && !strings.EqualFold(folder, expected) {
			return fmt.Errorf("custom %s folder for %s is unsupported by merge: %s", f["MediaType"], platform, f["FolderPath"])
		}
	}
	return nil
}
