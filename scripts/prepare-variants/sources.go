package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/platformnames"
)

type collectOptions struct{ Out, GA, RHA, HackHash, Platforms, Subdir string }
type sourceRecord struct {
	Platform, Parent, Name, Kind, Group, Version, Title, Problem string
	Languages                                                    []string
	Output                                                       fingerprint
	Base                                                         map[string]any
	Link                                                         map[string]any
}
type sourceSet struct {
	Records   []sourceRecord
	Snapshots []snapshot
	Holds     []map[string]any
}

func sourceFile(o options, relative string, set *sourceSet, url string) ([]byte, error) {
	name, err := regularInput(o.Sources, relative)
	if err != nil {
		return nil, err
	}
	before, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	after, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) {
		return nil, fmt.Errorf("source changed: %s", relative)
	}
	for _, s := range set.Snapshots {
		if s.Path == relative {
			return nil, fmt.Errorf("duplicate source file %s", relative)
		}
	}
	set.Snapshots = append(set.Snapshots, snapshot{Path: relative, SHA256: digest(raw), URL: url})
	return raw, nil
}
func sourceLink(source, path, hash, entry, parent string) map[string]any {
	return map[string]any{"source": source, "entry": entry, "parent": parent, "snapshot": path, "snapshot_sha256": hash}
}
func validOutput(f fingerprint) bool {
	return f.Size > 0 && validHash(f.SHA1, 20) && (f.MD5 == "" || validHash(f.MD5, 16)) && (f.CRC32 == "" || validHash(f.CRC32, 4))
}
func rejectSource(set *sourceSet, file, id, reason string) {
	set.Holds = append(set.Holds, map[string]any{"source_file": file, "entry": id, "reason": reason})
}

func loadGA(o options, file string, set *sourceSet) error {
	raw, err := sourceFile(o, file, set, "https://www.gamingalexandria.com/wp/frontend-project/")
	if err != nil {
		return err
	}
	type rom struct {
		Name string `xml:"name,attr"`
		Size int64  `xml:"size,attr"`
		SHA1 string `xml:"sha1,attr"`
		MD5  string `xml:"md5,attr"`
		CRC  string `xml:"crc,attr"`
	}
	type machine struct {
		Name string `xml:"name,attr"`
		ROMs []rom  `xml:"rom"`
	}
	var dat struct {
		XMLName xml.Name `xml:"datafile"`
		Header  struct {
			Name string `xml:"name"`
		} `xml:"header"`
		Machines []machine `xml:"machine"`
	}
	if err = xml.Unmarshal(raw, &dat); err != nil {
		return err
	}
	if len(dat.Machines) == 0 || !strings.Contains(strings.ToLower(dat.Header.Name), "famicom") {
		return fmt.Errorf("unrecognized Gaming Alexandria Famicom DAT")
	}
	for _, m := range dat.Machines {
		if len(m.ROMs) != 1 {
			rejectSource(set, file, m.Name, "requires one ROM per source entry")
			continue
		}
		rom := m.ROMs[0]
		f := fingerprint{Name: rom.Name, Size: rom.Size, SHA1: strings.ToLower(rom.SHA1), MD5: strings.ToLower(rom.MD5), CRC32: strings.ToLower(rom.CRC)}
		if !validOutput(f) {
			rejectSource(set, file, m.Name, "invalid modified payload identity")
			continue
		}
		pos := strings.Index(m.Name, " [T-En")
		parent := ""
		problem := ""
		if pos < 1 {
			problem = "source lacks a well-formed original-name / T-En boundary"
		} else {
			parent = strings.TrimSpace(m.Name[:pos])
			suffix := m.Name[pos:]
			depth := 0
			for _, r := range suffix {
				if r == '[' {
					depth++
				}
				if r == ']' {
					depth--
				}
				if depth < 0 {
					problem = "unbalanced source labels"
				}
			}
			if depth != 0 || strings.ContainsAny(parent, "[]") {
				problem = "unbalanced or pre-existing source labels"
			}
		}
		if problem != "" {
			rejectSource(set, file, m.Name, problem)
		}
		group, version := translationCredit(m.Name)
		link := sourceLink("Gaming Alexandria", file, digest(raw), m.Name, parent)
		link["parent_convention"] = "full machine name before the first well-formed T-En label"
		set.Records = append(set.Records, sourceRecord{Platform: "Nintendo - Nintendo Entertainment System", Parent: parent, Name: m.Name, Kind: "translation", Languages: []string{"en"}, Group: group, Version: version, Output: f, Link: link, Problem: problem})
	}
	return nil
}

func loadRHA(o options, dir string, set *sourceSet) error {
	if !safeRelative(dir) {
		return fmt.Errorf("unsafe RHA root")
	}
	base := filepath.Join(o.Sources, filepath.FromSlash(dir), "database")
	count := 0
	err := filepath.WalkDir(base, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in RHA source")
		}
		if d.IsDir() || d.Name() != "romhack.json" {
			return nil
		}
		rel, e := filepath.Rel(o.Sources, name)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		parts, e := filepath.Rel(base, name)
		if e != nil {
			return e
		}
		p := strings.Split(filepath.ToSlash(parts), "/")
		if len(p) != 4 {
			return fmt.Errorf("RHA path is not platform/base/output/romhack.json: %s", rel)
		}
		count++
		raw, e := sourceFile(o, rel, set, "https://github.com/videogame-archive/romhack-archive")
		if e != nil {
			return e
		}
		var r struct {
			Info struct {
				Name            string `json:"name"`
				TranslatedTitle *bool  `json:"translatedTitle"`
			} `json:"info"`
			ROM struct {
				Size  int64  `json:"size"`
				SHA1  string `json:"sha1"`
				MD5   string `json:"md5"`
				CRC32 string `json:"crc32"`
			} `json:"rom"`
			Hacks []struct {
				Name    string   `json:"name"`
				Authors []string `json:"authors"`
				Version string   `json:"version"`
				Labels  []string `json:"labels"`
				URL     string   `json:"url"`
			} `json:"hacks"`
		}
		if e = checkJSONKeys(raw); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return fmt.Errorf("%s: %w", rel, e)
		}
		f := fingerprint{Name: p[2], Size: r.ROM.Size, SHA1: strings.ToLower(r.ROM.SHA1), MD5: strings.ToLower(r.ROM.MD5), CRC32: strings.ToLower(r.ROM.CRC32)}
		if !validOutput(f) || len(r.Hacks) == 0 {
			rejectSource(set, rel, p[2], "missing ROM identity or patch metadata")
			return nil
		}
		languages := []string{}
		kind := "hack"
		var authors []string
		var versions []string
		for _, h := range r.Hacks {
			translated := false
			for _, label := range h.Labels {
				if strings.HasPrefix(label, "T-") {
					kind = "translation"
					translated = true
				}
				if lang, ok := map[string]string{"T-En": "en", "T-Ja": "ja", "T-Ru": "ru", "T-Fr": "fr", "T-De": "de", "T-Es": "es", "T-It": "it", "T-Pt": "pt", "T-Zh": "zh", "T-Ko": "ko"}[label]; ok {
					languages = appendUnique(languages, lang)
				}
			}
			if translated || len(r.Hacks) == 1 {
				for _, a := range h.Authors {
					authors = appendUnique(authors, a)
				}
				if h.Version != "" {
					versions = appendUnique(versions, h.Version)
				}
			}
		}
		parent := stripROMExtension(p[1])
		link := sourceLink("Romhack Archive", rel, digest(raw), filepath.ToSlash(parts), parent)
		link["parent_convention"] = "database/platform/base-ROM/output-ROM/romhack.json"
		link["patches"] = r.Hacks
		if r.Info.TranslatedTitle != nil {
			link["title_screen_translated"] = *r.Info.TranslatedTitle
		}
		version := ""
		if len(versions) == 1 {
			version = versions[0]
		}
		set.Records = append(set.Records, sourceRecord{Platform: p[0], Parent: parent, Name: stripROMExtension(p[2]), Title: r.Info.Name, Kind: kind, Languages: languages, Group: strings.Join(authors, ", "), Version: version, Output: f, Link: link})
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("RHA root contains no structured ROM records")
	}
	return nil
}

func loadHackHash(o options, file string, set *sourceSet, registry *platformnames.Registry) error {
	raw, err := sourceFile(o, file, set, "https://github.com/darkblood159/HackHash")
	if err != nil {
		return err
	}
	var data struct {
		Machines []struct {
			ROMName  string          `json:"romName"`
			Platform string          `json:"platform"`
			Size     json.RawMessage `json:"fileSize"`
			SHA1     string          `json:"sha1"`
			MD5      string          `json:"md5"`
			CRC32    string          `json:"crc32"`
			Details  struct {
				HackName  string         `json:"hackName"`
				Version   string         `json:"version"`
				Author    string         `json:"author"`
				Languages []string       `json:"translationLanguages"`
				Base      map[string]any `json:"baseRom"`
			} `json:"details"`
		} `json:"machines"`
	}
	if err = checkJSONKeys(raw); err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if len(data.Machines) == 0 {
		return fmt.Errorf("HackHash detailed export lacks machines")
	}
	for _, r := range data.Machines {
		sizeText := strings.Trim(string(r.Size), "\"")
		size, e := strconv.ParseInt(sizeText, 10, 64)
		f := fingerprint{Name: r.ROMName, Size: size, SHA1: strings.ToLower(r.SHA1), MD5: strings.ToLower(r.MD5), CRC32: strings.ToLower(r.CRC32)}
		if e != nil || !validOutput(f) {
			rejectSource(set, file, r.ROMName, "invalid output identity")
			continue
		}
		parent, _ := r.Details.Base["name"].(string)
		problem := ""
		basePlatform, _ := r.Details.Base["platform"].(string)
		status, _ := r.Details.Base["status"].(string)
		if parent == "" {
			problem = "missing detailed baseRom relationship"
		}
		if basePlatform != "" && registry.ResolveCLI(basePlatform) != registry.ResolveCLI(r.Platform) {
			problem = "baseRom platform differs from modified platform"
		}
		if status != "" && status != "APPROVED" {
			problem = "baseRom is not approved by source"
		}
		kind := "hack"
		if len(r.Details.Languages) > 0 {
			kind = "translation"
		}
		link := sourceLink("HackHash detailed", file, digest(raw), r.ROMName, parent)
		link["base"] = r.Details.Base
		set.Records = append(set.Records, sourceRecord{Platform: registry.ResolveCLI(r.Platform), Parent: parent, Name: stripROMExtension(r.ROMName), Title: r.Details.HackName, Kind: kind, Languages: r.Details.Languages, Group: r.Details.Author, Version: r.Details.Version, Base: r.Details.Base, Output: f, Link: link, Problem: problem})
	}
	return nil
}
func appendUnique(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
func sortSources(set *sourceSet) {
	sort.Slice(set.Snapshots, func(i, j int) bool { return set.Snapshots[i].Path < set.Snapshots[j].Path })
}
