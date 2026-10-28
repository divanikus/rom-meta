package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Add standalone structured outputs only in automatic source mode. Explicit
// --dat/legacy --ten selections keep their established DAT-only output scope.
func appendPublishedDiscovery(c *discoveryCatalog, set *sourceSet, r *discoveryReport) error {
	covered := map[string]bool{}
	for _, row := range r.Rows {
		if row.File.SHA1 != "" {
			covered[identityKey(row.Platform, row.File)] = true
		}
	}
	groups := map[string][]sourceRecord{}
	platforms := map[string]string{}
	for _, s := range set.Records {
		if publishedRecordReference(s) == nil {
			continue
		}
		p, err := c.platform(s.Platform)
		if err != nil {
			if !errors.Is(err, errCatalogPlatformMissing) {
				return err
			}
			r.Counts["skipped-output-platform"]++
			continue
		}
		key := identityKey(p, s.Output)
		if covered[key] {
			continue
		}
		groups[key] = append(groups[key], s)
		platforms[key] = p
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p, records := platforms[key], groups[key]
		if err := c.load(p); err != nil {
			return err
		}
		s := records[0]
		ref := publishedRecordReference(s)
		row := &discoveryRow{ID: decisionID(p, s.Output), Platform: p, SourcePlatform: s.Platform, Source: ref.Path, Name: s.Output.Name, File: s.Output, Parent: s.Parent, Published: ref, Kind: s.Kind, Title: s.Title, Status: "review", Reason: "no unique original-game match"}
		if row.Title == "" {
			row.Title = s.Name
		}
		if row.Kind == "" {
			row.Kind = "hack"
		}
		groups, versions := []string{}, []string{}
		blocked := false
		for _, source := range records {
			for _, other := range records {
				if !outputAgrees(source.Output, other.Output) {
					row.Status = "unsupported"
					row.Reason = "published modified-output hash constraints conflict"
				}
			}
			for _, lang := range source.Languages {
				row.Languages = appendUnique(row.Languages, lang)
			}
			if source.Kind == "translation" {
				row.Kind = "translation"
			}
			if source.Group != "" {
				groups = appendUnique(groups, source.Group)
			}
			if source.Version != "" {
				versions = appendUnique(versions, source.Version)
			}
			row.Links = append(row.Links, source.Link)
			candidates, problem := sourceBaseTargets(c, p, source)
			if problem != "" || source.Problem != "" || len(candidates) != 1 {
				blocked = true
			}
			row.Candidates = append(row.Candidates, candidates...)
		}
		if len(groups) == 1 {
			row.Group = groups[0]
		}
		if len(versions) == 1 {
			row.Version = versions[0]
		}
		row.Candidates = uniqueGames(row.Candidates)
		if row.Status != "unsupported" {
			if len(row.Candidates) == 1 && !blocked {
				row.Target = row.Candidates[0]
				row.Status = "ready"
				row.Reason = "published output-to-parent relationship"
			} else {
				row.Reason = "explicit source parent is unresolved or ambiguous"
			}
		}
		// Keep the same file/relationship validation used by DAT discovery.
		probe := entry{Platform: p, Game: "probe", Variant: variant{Key: "probe", Kind: row.Kind}, File: s.Output, Published: ref, Evidence: evidence{Relationship: "reviewed", Links: []map[string]any{{"source": "probe", "entry": "probe", "parent": "probe"}}}, Expected: map[string]string{"game.yaml": strings.Repeat("a", 64), "fingerprints.yaml": strings.Repeat("a", 64)}}
		if err := validateEntry(probe); err != nil {
			row.Status = "unsupported"
			row.Reason = fmt.Sprint(err)
		}
		r.Rows = append(r.Rows, row)
	}
	return nil
}
