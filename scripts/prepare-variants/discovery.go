package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type discoveryOptions struct {
	DAT      []string `json:"dat,omitempty"`
	TEN      []string `json:"ten,omitempty"`
	GA       string   `json:"ga,omitempty"`
	RHA      string   `json:"rha,omitempty"`
	HackHash string   `json:"hackhash,omitempty"`
}
type reviewDecision struct {
	Platform string   `json:"platform,omitempty"`
	Game     string   `json:"game,omitempty"`
	NewGame  *newGame `json:"new_game,omitempty"`
	Exclude  bool     `json:"exclude,omitempty"`
}
type discoveryRow struct {
	ID             string              `json:"id"`
	Platform       string              `json:"platform"`
	SourcePlatform string              `json:"source_platform"`
	Source         string              `json:"source"`
	Name           string              `json:"name"`
	Member         string              `json:"member,omitempty"`
	Languages      []string            `json:"languages,omitempty"`
	File           fingerprint         `json:"file"`
	Status         string              `json:"status"`
	Reason         string              `json:"reason"`
	Candidates     []*gameChoice       `json:"candidates,omitempty"`
	Target         *gameChoice         `json:"target,omitempty"`
	Parent         string              `json:"source_parent,omitempty"`
	Links          []map[string]any    `json:"relationship_evidence,omitempty"`
	Published      *publishedReference `json:"published,omitempty"`
	Kind           string              `json:"kind,omitempty"`
	Title          string              `json:"title,omitempty"`
	Group          string              `json:"group,omitempty"`
	Version        string              `json:"version,omitempty"`
}
type discoveryReport struct {
	SchemaVersion int                `json:"schema_version"`
	Counts        map[string]int     `json:"counts"`
	Rows          []*discoveryRow    `json:"rows"`
	SourceHolds   []map[string]any   `json:"source_holds,omitempty"`
	Sources       []discoveredSource `json:"sources,omitempty"`
	IgnoredFiles  int                `json:"ignored_metadata_files"`
}

func decisionID(p string, f fingerprint) string {
	return digest([]byte(fmt.Sprintf("%s\x00%s\x00%s", identityKey(p, f), strings.ToLower(f.MD5), strings.ToLower(f.CRC32))))
}
func outputAgrees(a, b fingerprint) bool {
	if a.Kind != b.Kind || (a.Kind == "" && a.Size != b.Size) {
		return false
	}
	for _, pair := range [][2]string{{a.SHA1, b.SHA1}, {a.SHA256, b.SHA256}, {a.MD5, b.MD5}, {a.CRC32, b.CRC32}} {
		if pair[0] != "" && pair[1] != "" && !strings.EqualFold(pair[0], pair[1]) {
			return false
		}
	}
	return true
}
func outputParent(s string) string {
	s = stripROMExtension(s)
	loc := translationTagRE.FindStringSubmatchIndex(s)
	if loc == nil || loc[0] < 1 {
		return ""
	}
	i := loc[0]
	p := strings.TrimSpace(s[:i])
	if strings.ContainsAny(p, "[]") {
		return ""
	}
	return p
}
func sourceBaseTargets(c *discoveryCatalog, p string, s sourceRecord) ([]*gameChoice, string) {
	named, _ := c.match(p, s.Parent)
	strong := false
	for _, k := range []string{"sha1", "sha256"} {
		v, _ := s.Base[k].(string)
		strong = strong || v != ""
	}
	if !strong {
		return named, ""
	}
	out := []*gameChoice{}
	for _, owners := range c.owners {
		for _, o := range owners {
			if o.Game.Platform != p || o.Release == "" || o.File.Kind != "" {
				continue
			}
			agree := true
			for k, got := range map[string]string{"sha1": o.File.SHA1, "sha256": o.File.SHA256, "md5": o.File.MD5, "crc32": o.File.CRC32} {
				want, _ := s.Base[k].(string)
				if want != "" && !strings.EqualFold(want, got) {
					agree = false
				}
			}
			if agree {
				out = append(out, o.Game)
			}
		}
	}
	out = uniqueGames(out)
	if len(out) == 0 {
		return nil, "published base fingerprint has no original-game match"
	}
	if len(named) > 0 {
		overlap := false
		for _, a := range named {
			for _, b := range out {
				overlap = overlap || gameID(a.Platform, a.Game) == gameID(b.Platform, b.Game)
			}
		}
		if !overlap {
			return nil, "published base and parent name disagree"
		}
	}
	return out, ""
}
func discover(o options, d discoveryOptions, decisions map[string]reviewDecision) (*manifest, *discoveryReport, *discoveryCatalog, error) {
	inventory, err := inspectDiscoverySources(o, d)
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := openDiscoveryCatalog(o)
	if err != nil {
		return nil, nil, nil, err
	}
	m := &manifest{SchemaVersion: 1, Entries: []entry{}}
	report := &discoveryReport{SchemaVersion: 1, Counts: map[string]int{}, Rows: []*discoveryRow{}}
	set := &sourceSet{}
	for _, ref := range inventory.GA {
		if err = loadGAReference(o, ref, set); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, dir := range inventory.RHA {
		if err = loadRHA(o, dir, set); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, file := range inventory.HackHash {
		if err = loadHackHash(o, file, set, &c.registry); err != nil {
			return nil, nil, nil, err
		}
	}
	report.Sources = inventory.Inputs
	report.IgnoredFiles = inventory.Ignored
	progress(o, "Sources: %d translation DATs, %d Gaming Alexandria, %d Romhack Archive, %d HackHash; %d metadata files ignored", len(inventory.DAT), len(inventory.GA), len(inventory.RHA), len(inventory.HackHash), inventory.Ignored)

	m.Snapshots = append(m.Snapshots, set.Snapshots...)
	report.SourceHolds = set.Holds
	records := map[string][]sourceRecord{}
	for _, s := range set.Records {
		p, e := c.platform(s.Platform)
		if e != nil {
			continue
		}
		records[identityKey(p, s.Output)] = append(records[identityKey(p, s.Output)], s)
	}
	seen := map[string]*discoveryRow{}
	for _, ref := range inventory.DAT {
		path := ref.Path
		src, err := readTranslationDAT(o, ref, m)
		if err != nil {
			return nil, nil, nil, err
		}
		sourcePlatform := src.prefix
		p, err := c.platform(src.platform)
		if err != nil {
			if !errors.Is(err, errCatalogPlatformMissing) {
				return nil, nil, nil, err
			}
			report.Counts["skipped-platform"]++
			for i := range report.Sources {
				input := &report.Sources[i]
				if input.Path == ref.Path && input.Member == ref.Member && input.Type == "translation-dat" {
					input.Status = "skipped"
					input.Reason = "platform is absent from the catalog"
				}
			}
			continue
		}
		if err = c.load(p); err != nil {
			return nil, nil, nil, err
		}
		progress(o, "Discovering published outputs: %s (%d groups)", p, len(src.machines))
		for _, machine := range src.machines {
			if strings.HasPrefix(machine.Name, "_") || strings.HasPrefix(strings.ToUpper(machine.Name), "[BIOS]") {
				report.Counts["skipped-group"]++
				continue
			}
			if len(machine.ROMs) > 1 || (len(machine.ROMs) > 0 && len(machine.Disks) > 0) {
				report.Rows = append(report.Rows, &discoveryRow{Platform: p, SourcePlatform: sourcePlatform, Source: path, Name: machine.Name, Status: "unsupported", Reason: "whole multi-payload set requires a set adapter"})
				continue
			}
			outputs := append([]tenOutput{}, machine.ROMs...)
			outputs = append(outputs, machine.Disks...)
			for _, raw := range outputs {
				if raw.Status == "nodump" {
					report.Counts["nodump"]++
					continue
				}
				f, ok := src.outputs[raw.Name]
				if !ok {
					continue
				}
				row := &discoveryRow{ID: decisionID(p, f), Platform: p, SourcePlatform: sourcePlatform, Source: path, Name: raw.Name, Member: ref.Member, Languages: src.languages, File: f, Status: "review", Reason: "no unique original-game match", Parent: outputParent(raw.Name)}
				langs := translationLanguages(raw.Name)
				if len(langs) == 0 {
					langs = translationLanguages(machine.Name)
				}
				if len(langs) > 0 {
					row.Languages = langs
				}
				if row.Parent == "" {
					row.Parent = outputParent(machine.Name)
				}
				if old := seen[row.ID]; old != nil {
					for _, lang := range row.Languages {
						old.Languages = appendUnique(old.Languages, lang)
					}
					if old.Name != row.Name && !(old.Status == "ready" && old.Reason == "published output-to-parent relationship") {
						old.Status = "review"
						old.Reason = "same output has different source labels; choose its original explicitly"
					}
					continue
				}
				seen[row.ID] = row
				report.Rows = append(report.Rows, row)
				probe := entry{Platform: p, Game: "probe", Variant: variant{Key: "probe", Kind: "translation"}, File: f, Evidence: evidence{Relationship: "reviewed", Links: []map[string]any{{"source": "probe", "entry": "probe", "parent": "probe"}}}, Expected: map[string]string{"game.yaml": strings.Repeat("a", 64), "fingerprints.yaml": strings.Repeat("a", 64)}, DAT: &datReference{Path: path, Name: raw.Name}}
				if err := validateEntry(probe); err != nil {
					row.Status = "unsupported"
					row.Reason = err.Error()
					continue
				}
				choiceReason := ""
				blocked := false
				explicit := false
				for _, s := range records[identityKey(p, f)] {
					explicit = true
					if !outputAgrees(f, s.Output) || s.Problem != "" {
						blocked = true
						row.Reason = "source relationship or output constraints conflict"
						continue
					}
					gs, problem := sourceBaseTargets(c, p, s)
					if problem != "" || len(gs) != 1 {
						blocked = true
						row.Reason = "explicit source parent is unresolved or ambiguous"
					}
					row.Candidates = append(row.Candidates, gs...)
					row.Links = append(row.Links, s.Link)
					choiceReason = "published output-to-parent relationship"
				}
				if !explicit {
					row.Candidates, choiceReason = c.match(p, row.Parent)
					if row.Parent == "" {
						row.Candidates = nil
					}
					row.Links = []map[string]any{sourceLink("Translation DAT name convention", path, src.sha256, row.Name, row.Parent)}
					if parent := outputParent(machine.Name); parent != "" && parent != row.Parent {
						gs, _ := c.match(p, parent)
						row.Candidates = append(row.Candidates, gs...)
						row.Links = append(row.Links, sourceLink("Translation DAT container name convention", path, src.sha256, machine.Name, parent))
					}
				}
				row.Candidates = uniqueGames(row.Candidates)
				if len(row.Candidates) == 1 && !blocked {
					row.Target = row.Candidates[0]
					row.Status = "ready"
					row.Reason = choiceReason
				} else if len(row.Candidates) > 1 {
					row.Reason = "multiple original-game cards match"
				}
			}
		}
	}
	if len(d.DAT) == 0 && len(d.TEN) == 0 {
		if err := appendPublishedDiscovery(c, set, report); err != nil {
			return nil, nil, nil, err
		}
	}
	// Finalize after duplicate labels have been seen. A persistent reviewer choice
	// has precedence over naming/source suggestions, but never over hash ownership.
	for _, row := range report.Rows {
		if row.Status == "unsupported" {
			continue
		}
		if dec, ok := decisions[row.ID]; ok {
			row.Target = nil
			row.Status = "review"
			row.Reason = "saved original-game target is unavailable"
			if dec.Exclude {
				row.Status = "excluded"
				row.Reason = "excluded by reviewer"
				continue
			}
			p, err := c.platform(dec.Platform)
			if err == nil {
				err = c.load(p)
			}
			if err == nil {
				target := c.games[gameID(p, dec.Game)]
				if dec.NewGame != nil {
					e := entry{Platform: p, Game: dec.Game, NewGame: dec.NewGame}
					if safeName(dec.Game) && !strings.HasPrefix(dec.Game, "_") && !strings.HasPrefix(dec.Game, ".") && strings.TrimSpace(dec.NewGame.Title) != "" && checkNewGame(o, e) == nil {
						target = &gameChoice{Platform: p, Game: dec.Game, Title: dec.NewGame.Title, NewGame: dec.NewGame, expected: map[string]string{"game.yaml": "", "fingerprints.yaml": ""}}
					}
				}
				if target != nil {
					row.Target = target
					row.Status = "ready"
					row.Reason = "saved reviewer choice"
					row.Links = []map[string]any{{"source": "Reviewer decision", "entry": row.ID, "parent": gameID(p, dec.Game), "method": "original-game selection"}}
				}
			}
		}
		owners := c.outputOwners(row.Platform, row.File)
		// Cross-platform choices also check the destination's existing owners.
		if row.Target != nil && row.Target.Platform != row.Platform {
			owners = append(append([]outputOwner{}, owners...), c.outputOwners(row.Target.Platform, row.File)...)
		}
		if len(owners) > 0 {
			row.Status = "already-owned"
			row.Reason = "output already has a catalog owner"
			row.Candidates = nil
			for _, own := range owners {
				row.Candidates = append(row.Candidates, own.Game)
				if !outputAgrees(row.File, own.File) || (row.Target != nil && gameID(own.Game.Platform, own.Game.Game) != gameID(row.Target.Platform, row.Target.Game)) {
					row.Status = "review"
					row.Reason = "existing output ownership or constraints conflict"
				}
			}
			row.Candidates = uniqueGames(row.Candidates)
		}
		if row.Status != "ready" {
			continue
		}
		g := row.Target
		key := safeTitleKey(stripROMExtension(row.Name))
		if len(key) > 140 {
			key = "Translation output"
			if row.Published != nil {
				key = "Variant output"
			}
		}
		key += " [" + row.ID[:12] + "]"
		group, version := translationCredit(row.Name)
		for _, link := range row.Links {
			if parent, _ := link["parent"].(string); parent == "" {
				link["parent"] = gameID(g.Platform, g.Game)
			}
		}
		e := entry{Platform: g.Platform, Game: g.Game, NewGame: g.NewGame, Variant: variant{Key: key, Title: stripROMExtension(row.Name), Kind: "translation", Languages: row.Languages, Group: group, Version: version}, File: row.File, DAT: &datReference{Path: row.Source, Member: row.Member, Name: row.Name, SourcePlatform: row.SourcePlatform}, Evidence: evidence{Relationship: "reviewed", Links: row.Links, Normalization: []string{row.Reason}}, Expected: g.expected}
		if row.Published != nil {
			e.DAT = nil
			e.Published = row.Published
			e.Variant.Kind = row.Kind
			e.Variant.Title = row.Title
			e.Variant.Group = row.Group
			e.Variant.Version = row.Version
		}
		if err := validateEntry(e); err != nil {
			return nil, nil, nil, err
		}
		m.Entries = append(m.Entries, e)
	}
	for _, row := range report.Rows {
		if row.Status == "review" || row.Status == "unsupported" {
			b, err := json.Marshal(row)
			if err != nil {
				return nil, nil, nil, err
			}
			m.Holds = append(m.Holds, b)
		}
		report.Counts[row.Status]++
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		a, b := report.Rows[i], report.Rows[j]
		return a.Platform+"/"+a.Name < b.Platform+"/"+b.Name
	})
	sort.Slice(m.Entries, func(i, j int) bool {
		a, b := m.Entries[i], m.Entries[j]
		return gameID(a.Platform, a.Game)+a.Variant.Key < gameID(b.Platform, b.Game)+b.Variant.Key
	})
	if err := c.fresh(); err != nil {
		return nil, nil, nil, err
	}
	if err := checkSnapshots(m, o); err != nil {
		return nil, nil, nil, err
	}
	return m, report, c, nil
}
