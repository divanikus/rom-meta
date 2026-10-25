package exporter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/releasepolicy"
)

type catalogIndex struct {
	BySize      map[int64][]fingerprint
	ByName      map[string][]fingerprint
	GamesByName map[string][]*game
}

func nameKey(name string) string {
	name = filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(name))
	if nameExtension(ext) {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	// Preserve every qualifier's contents; only case, spaces and punctuation vary.
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
func addReleaseName(index *catalogIndex, name string, g *game, release string) {
	if key := nameKey(name); key != "" {
		index.ByName[key] = append(index.ByName[key], fingerprint{game: g, release: release})
	}
}
func indexNames(index *catalogIndex, g *game) {
	names := []string{g.name}
	for _, key := range []string{"aliases", "alternate_titles"} {
		names = append(names, catalogyaml.Strings(catalogyaml.Value(g.root, key))...)
	}
	for _, name := range names {
		if key := nameKey(name); key != "" {
			index.GamesByName[key] = append(index.GamesByName[key], g)
		}
	}
	_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "releases"), func(release string, node *yaml.Node) error {
		addReleaseName(index, release, g, release)
		addReleaseName(index, field(catalogyaml.Value(node, "authority"), "entry_name"), g, release)
		return nil
	})
}
func nameFallback(root string, index *catalogIndex, record *Record, profile Profile, system string) error {
	names := []string{record.ROM}
	if record.Member != "" {
		names = append(names, record.Member)
	}
	// Archive and member names must not identify different catalog games, even
	// when one names a release and the other names only a game.
	var games []*game
	gameSeen := map[string]bool{}
	for _, name := range names {
		for _, g := range index.GamesByName[nameKey(name)] {
			if !gameSeen[g.path] {
				gameSeen[g.path] = true
				games = append(games, g)
			}
		}
	}
	var matches []fingerprint
	seen := map[string]bool{}
	for _, name := range names {
		for _, candidate := range index.ByName[nameKey(name)] {
			key := candidate.game.path + "\x00" + candidate.release
			if !seen[key] {
				seen[key] = true
				matches = append(matches, candidate)
			}
		}
	}
	if len(matches) == 1 {
		for _, g := range games {
			if g.path != matches[0].game.path {
				record.Status, record.Reason = "ambiguous", "release name conflicts with catalog game name"
				record.Candidates = append(record.Candidates, matches[0].game.name+" / "+matches[0].release)
				for _, named := range games {
					if named.path != matches[0].game.path {
						record.Candidates = append(record.Candidates, named.name+" / release unknown")
					}
				}
				sort.Strings(record.Candidates)
				return nil
			}
		}
		record.Status, record.MatchedBy, record.Reason = "matched", "name", "unique normalized authority/file name; hash did not match"
		record.Warnings = append(record.Warnings, "Name binding is not a verified dump identity")
		return attachRecord(root, matches[0].game, matches[0].release, record, profile, system)
	}
	if len(matches) > 1 {
		record.Status, record.Reason = "ambiguous", "name identifies multiple releases or archive/member names conflict"
		for _, candidate := range matches {
			record.Candidates = append(record.Candidates, candidate.game.name+" / "+candidate.release)
		}
		sort.Strings(record.Candidates)
		return nil
	}

	if len(games) == 1 {
		record.Status, record.MatchedBy, record.Reason = "matched", "game-name", "unique catalog title/alias; exact release remains unknown"
		record.Warnings = append(record.Warnings, "Game-only name match: release is unknown; metadata uses the catalog default and profile display scope")
		return attachRecord(root, games[0], "", record, profile, system)
	}
	if len(games) > 1 {
		record.Status, record.Reason = "ambiguous", "title/alias identifies multiple catalog games"
		for _, g := range games {
			record.Candidates = append(record.Candidates, g.name)
		}
		sort.Strings(record.Candidates)
	} else {
		record.Reason = "no strong fingerprint or unique catalog name"
	}
	return nil
}

func attachRecord(root string, g *game, release string, record *Record, profile Profile, system string) error {
	if g.root == nil {
		doc, digest, err := lookupDocument(root, filepath.Join(g.path, "game.yaml"))
		if err != nil {
			return err
		}
		if digest != g.documentDigest {
			return fmt.Errorf("catalog changed behind cached lookup index; review and restart API")
		}
		g.root = doc
	}
	record.Game, _ = filepath.Rel(root, g.path)
	record.Game = filepath.ToSlash(record.Game)
	record.Release = release
	basis := release
	if basis == "" {
		basis = field(g.root, "default_release")
		if catalogyaml.Value(catalogyaml.Value(g.root, "releases"), basis) == nil {
			var names []string
			_ = catalogyaml.Mapping(catalogyaml.Value(g.root, "releases"), func(name string, _ *yaml.Node) error { names = append(names, name); return nil })
			basis = releasepolicy.Choose(names, nil, nil)
		}
	}
	record.MetadataRelease = basis
	record.Fields = resolveFields(g, basis, profile)
	if err := applyDescriptionFallback(g, record, profile); err != nil {
		return err
	}
	if release == "" {
		record.Fields["title"] = Value{Value: g.name, Source: "catalog"}
	}
	media, warnings, err := resolveMedia(root, g, basis, record.ROM, profile, release != "", system)
	if err != nil {
		return err
	}
	record.Media = media
	record.Warnings = append(record.Warnings, warnings...)
	return nil
}
