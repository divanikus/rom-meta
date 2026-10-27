package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/platformnames"
)

type collectionReport struct {
	SchemaVersion  int              `json:"schema_version"`
	Status         string           `json:"status"`
	SourceRecords  int              `json:"source_records"`
	SourceHolds    []map[string]any `json:"source_holds,omitempty"`
	Inputs         int              `json:"inputs"`
	Prepared       int              `json:"prepared"`
	Held           int              `json:"held"`
	Entries        []map[string]any `json:"entries"`
	ManifestSHA256 string           `json:"manifest_sha256,omitempty"`
	Error          string           `json:"error,omitempty"`
}

func guardCollection(o options, c collectOptions) error {
	if o.Catalog == "" || o.ROMRoot == "" || o.Sources == "" || c.Out == "" || c.Platforms == "" {
		return fmt.Errorf("collection needs --catalog, --rom-root, --sources, --platforms and --collect-links")
	}
	if o.Links != "" || o.Out != "" {
		return fmt.Errorf("collect links separately from --links/--out staging")
	}
	if c.GA == "" && c.RHA == "" && c.HackHash == "" {
		return fmt.Errorf("select --ga, --rha or --hackhash metadata")
	}
	out, err := resolved(c.Out)
	if err != nil {
		return err
	}
	for _, root := range []string{o.Catalog, o.ROMRoot, o.Sources} {
		p, err := resolved(root)
		if err != nil {
			return err
		}
		if within(p, out) || within(out, p) {
			return fmt.Errorf("collection output overlaps input")
		}
	}
	if _, err = os.Lstat(out); !os.IsNotExist(err) {
		return fmt.Errorf("collection output must be new")
	}
	if o.Report != "" {
		guard := o
		guard.Out = c.Out
		if _, err = reportPath(guard); err != nil {
			return err
		}
	}
	return nil
}
func collectLinks(o options, c collectOptions) (m *manifest, r *collectionReport, failure error) {
	if err := guardCollection(o, c); err != nil {
		return nil, nil, err
	}
	r = &collectionReport{SchemaVersion: 1, Status: "preview"}
	defer func() {
		if failure != nil {
			r.Status = "blocked"
			r.Error = failure.Error()
		}
	}()
	registry, _, err := platformnames.LoadRegistry(o.Catalog)
	if err != nil {
		return nil, r, err
	}
	platforms := []string{}
	for _, p := range strings.Split(c.Platforms, ",") {
		p = registry.ResolveCLI(strings.TrimSpace(p))
		if !safeName(p) {
			return nil, r, fmt.Errorf("unsafe selected platform")
		}
		platforms = appendUnique(platforms, p)
	}
	sort.Strings(platforms)
	set := &sourceSet{}
	if c.GA != "" {
		if err = loadGA(o, c.GA, set); err != nil {
			return nil, r, err
		}
	}
	if c.RHA != "" {
		if err = loadRHA(o, c.RHA, set); err != nil {
			return nil, r, err
		}
	}
	if c.HackHash != "" {
		if err = loadHackHash(o, c.HackHash, set, &registry); err != nil {
			return nil, r, err
		}
	}
	sortSources(set)
	r.SourceRecords = len(set.Records)
	r.SourceHolds = set.Holds
	progress(o, "Source records: %d; source rows held: %d", len(set.Records), len(set.Holds))
	parents, err := catalogParents(o, platforms)
	if err != nil {
		return nil, r, err
	}
	observed, err := collectInputs(o, platforms, c.Subdir)
	if err != nil {
		return nil, r, err
	}
	r.Inputs = len(observed)
	sources := map[string][]sourceRecord{}
	for _, s := range set.Records {
		sources[sourceIdentity(s.Platform, s.Output)] = append(sources[sourceIdentity(s.Platform, s.Output)], s)
	}
	m = &manifest{SchemaVersion: 1, Snapshots: set.Snapshots}
	accepted := map[string]int{}
	for number, in := range observed {
		if number%50 == 0 {
			progress(o, "Resolving observed translations: %d/%d", number, len(observed))
		}
		row := map[string]any{"platform": in.Platform, "file": in.Relative}
		hold := func(reason string) {
			row["status"] = reason
			m.Holds = append(m.Holds, mustJSON(row))
			r.Held++
			r.Entries = append(r.Entries, row)
		}
		if in.Problem != "" {
			row["detail"] = in.Problem
			hold("unsupported-input")
			continue
		}
		identity := sourceIdentity(in.Platform, in.File)
		matches := sources[identity]
		if len(matches) == 0 {
			hold("no-source-output-relationship")
			continue
		}
		if parents.Owned[identity] {
			hold("output-already-owned-by-catalog")
			continue
		}
		targets := []parentTarget{}
		links := []map[string]any{}
		methods := []string{}
		relationship := "source-release-name"
		blocked := ""
		langs := []string{}
		for _, source := range matches {
			if source.Problem != "" {
				blocked = source.Problem
				break
			}
			if source.Output.MD5 != "" && !strings.EqualFold(source.Output.MD5, in.File.MD5) || source.Output.CRC32 != "" && !strings.EqualFold(source.Output.CRC32, in.File.CRC32) {
				blocked = "published output digests disagree"
				break
			}
			candidates, method, err := parents.parent(source)
			if err != nil {
				blocked = err.Error()
				break
			}
			if len(candidates) != 1 {
				blocked = "source parent does not select one exact release"
				break
			}
			targets = append(targets, candidates[0])
			links = append(links, source.Link)
			if method == "source-base-hash" {
				relationship = "source-base-hash"
			} else {
				methods = appendUnique(methods, method)
			}
			for _, lang := range source.Languages {
				langs = appendUnique(langs, lang)
			}
		}
		if blocked != "" {
			row["detail"] = blocked
			hold("parent-review")
			continue
		}
		targets = uniqueTargets(targets)
		if len(targets) != 1 {
			hold("conflicting-source-parents")
			continue
		}
		t := targets[0]
		s := matches[0]
		title := s.Title
		if title == "" {
			title = readableName(in.File.Name)
		}
		kind := s.Kind
		v := variant{Key: kind + "-" + in.File.SHA1, Kind: kind, Title: title, BasedOn: t.Release, Languages: langs, Group: s.Group, Version: s.Version}
		expected := map[string]string{}
		for _, f := range t.Bundle.Files {
			expected[filepath.Base(f.Path)] = f.SourceSHA256
		}
		e := entry{Platform: in.Platform, Game: t.Bundle.Game, Variant: v, File: in.File,
			Evidence: evidence{Relationship: relationship, Links: links, Normalization: methods}, Expected: expected, Inputs: []input{in.Input}}
		if err = validateEntry(e); err != nil {
			row["detail"] = err.Error()
			hold("invalid-prepared-entry")
			continue
		}
		if old, ok := accepted[identity]; ok {
			existing := &m.Entries[old]
			if existing.Game != e.Game || existing.Variant.BasedOn != e.Variant.BasedOn {
				return nil, r, fmt.Errorf("same output selected different parents")
			}
			existing.Inputs = append(existing.Inputs, in.Input)
		} else {
			accepted[identity] = len(m.Entries)
			m.Entries = append(m.Entries, e)
		}
		row["status"] = "prepared"
		row["game"] = t.Bundle.Game
		row["based_on"] = t.Release
		row["relationship"] = relationship
		row["normalization"] = methods
		r.Prepared++
		r.Entries = append(r.Entries, row)
	}
	// Check the captured source/documents and input stats again before publishing a map.
	if err = checkSnapshots(m, o); err != nil {
		return nil, r, err
	}
	preconditions := &report{Files: parents.Files}
	inputs := []checkedInput{}
	for _, in := range observed {
		p, err := regularInput(o.ROMRoot, in.Relative)
		if err != nil {
			return nil, r, err
		}
		inputs = append(inputs, checkedInput{p, in.Info})
	}
	if err = checkPreconditions(o, preconditions, inputs); err != nil {
		return nil, r, err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, r, err
	}
	raw = append(raw, '\n')
	r.ManifestSHA256 = digest(raw)
	if o.Apply {
		if len(m.Entries) == 0 {
			return nil, r, fmt.Errorf("no prepared relationships; see held report")
		}
		if err = writeNewJSON(c.Out, m); err != nil {
			return nil, r, err
		}
		r.Status = "collected"
	}
	return m, r, nil
}
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func writeNewJSON(name string, value any) error {
	// Parent directory is explicit; never create it inside an input tree.
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(value)
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}
func collectCLI(o options, c collectOptions) error {
	_, r, err := collectLinks(o, c)
	if r != nil {
		if o.Report != "" {
			if e := writeNewJSON(o.Report, r); e != nil {
				return e
			}
		}
		fmt.Printf("%s: %d inputs, %d prepared, %d held; %d source records / %d held source rows\n", r.Status, r.Inputs, r.Prepared, r.Held, r.SourceRecords, len(r.SourceHolds))
	}
	return err
}
