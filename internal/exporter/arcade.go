package exporter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

var errArcadeSnapshot = errors.New("MAME catalog snapshot selection required")

type arcadeBinding struct {
	Platform string `yaml:"platform"`
	Game     string `yaml:"game"`
	Release  string `yaml:"release"`
	Family   string `yaml:"family"`
	Hardware string `yaml:"hardware"`
}
type arcadeIndex struct {
	Snapshot string
	Sets     map[string]arcadeBinding
	Games    map[string]*game
}

func loadArcadeIndex(root, snapshot string) (*arcadeIndex, error) {
	base := filepath.Join(root, "bindings", "romsets", "mame")
	if snapshot == "" {
		entries, err := catalogfs.ReadDir(base)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: no saved MAME bindings", errArcadeSnapshot)
		}
		if err != nil {
			return nil, err
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				names = append(names, entry.Name())
			}
		}
		if len(names) != 1 {
			return nil, fmt.Errorf("%w: use --arcade-snapshot; found %d saved snapshots", errArcadeSnapshot, len(names))
		}
		snapshot = names[0]
	}
	if filepath.Base(snapshot) != snapshot || snapshot == "." || snapshot == ".." || strings.ContainsAny(snapshot, "/\\") {
		return nil, fmt.Errorf("invalid arcade snapshot")
	}
	path, err := realPath(filepath.Join(base, snapshot, "catalog.yaml"))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: saved bindings for %s are missing", errArcadeSnapshot, snapshot)
	}
	if err != nil {
		return nil, err
	}
	if !within(root, path) {
		return nil, fmt.Errorf("arcade binding path escapes catalog")
	}
	f, err := catalogfs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var doc struct {
		Sets map[string]arcadeBinding `yaml:"sets"`
	}
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("MAME binding file must contain one YAML document")
	}
	return &arcadeIndex{Snapshot: snapshot, Sets: doc.Sets, Games: map[string]*game{}}, nil
}

// Set-name projection uses durable MAME bindings. It is deliberately distinct
// from payload verification and never falls back to generic game titles.
func bindArcade(root string, index *arcadeIndex, record *Record, profile Profile, system string) error {
	set := strings.TrimSuffix(filepath.Base(record.ROM), filepath.Ext(record.ROM))
	binding, ok := index.Sets[set]
	if !ok {
		record.Reason = "set short name is absent from selected MAME catalog snapshot"
		return nil
	}
	if binding.Platform != "Arcade" || binding.Release != set || filepath.Base(binding.Game) != binding.Game || binding.Game == "." || binding.Game == ".." || strings.ContainsAny(binding.Game, "/\\") {
		return fmt.Errorf("invalid saved MAME binding for %q", set)
	}
	path, err := realPath(filepath.Join(root, "platforms", binding.Platform, binding.Game))
	if err != nil {
		return nilBindingError(record, err)
	}
	if !within(root, path) {
		return fmt.Errorf("arcade card escapes catalog root")
	}
	g := index.Games[path]
	if g == nil {
		doc, digest, err := lookupDocument(root, filepath.Join(path, "game.yaml"))
		if err != nil {
			return nilBindingError(record, err)
		}
		g = &game{path: path, name: field(doc, "title"), root: doc, documentDigest: digest}
		if g.name == "" {
			g.name = binding.Game
		}
		index.Games[path] = g
	}
	release := catalogyaml.Value(catalogyaml.Value(g.root, "releases"), binding.Release)
	authority := catalogyaml.Value(release, "authority")
	mame := catalogyaml.Value(release, "mame")
	if release == nil || field(authority, "source") != "mame" || field(authority, "entry_name") != set || field(mame, "snapshot") != index.Snapshot {
		record.Status, record.Reason = "ambiguous", "saved MAME binding disagrees with current release/snapshot"
		return nil
	}
	record.Status, record.MatchedBy, record.ArcadeSnapshot = "matched", "arcade-set-name", index.Snapshot
	record.Reason = "exact short name in saved MAME catalog binding; input payload/version not verified"
	record.Warnings = append(record.Warnings, "Arcade archive members, input MAME/FBNeo version, parent/BIOS/CHD dependencies and merged/split completeness are not verified")
	for _, key := range []string{"romof", "sampleof"} {
		if dependency := field(mame, key); dependency != "" {
			record.Warnings = append(record.Warnings, key+": "+dependency+" (not checked)")
		}
	}
	return attachRecord(root, g, binding.Release, record, profile, system)
}
func nilBindingError(record *Record, err error) error {
	if os.IsNotExist(err) {
		record.Status, record.Reason = "ambiguous", "saved MAME binding references a missing card"
		return nil
	}
	return err
}
