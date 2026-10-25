package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// Validate durable snapshot hashes and the catalog projection without assuming
// that a ROM inventory exists or that emulation is working.
func validateArcadeBindings(root string, issues *[]Issue) {
	base := filepath.Join(root, "bindings", "romsets", "mame")
	snapshots, err := catalogfs.ReadDir(base)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		addIssue(root, issues, base, err.Error())
		return
	}
	for _, snapshot := range snapshots {
		if !snapshot.IsDir() || strings.HasPrefix(snapshot.Name(), ".") {
			continue
		}
		dir := filepath.Join(base, snapshot.Name())
		sourcePath := filepath.Join(dir, "_source.yaml")
		source, err := readArcadeDocument(sourcePath)
		if err != nil {
			addIssue(root, issues, sourcePath, err.Error())
			continue
		}
		if catalogyaml.Scalar(catalogyaml.Value(source, "snapshot")) != snapshot.Name() {
			addIssue(root, issues, sourcePath, "snapshot identity differs from directory")
		}
		files := []*yaml.Node{source, catalogyaml.Value(source, "classifications"), catalogyaml.Value(source, "routing")}
		for _, file := range files {
			name := catalogyaml.Scalar(catalogyaml.Value(file, "file"))
			expected := catalogyaml.Scalar(catalogyaml.Value(file, "sha256"))
			if name == "" || !isSafePathName(name) || filepath.Base(name) != name || name == "." || name == ".." {
				addIssue(root, issues, sourcePath, "invalid snapshot file name")
				continue
			}
			f, err := catalogfs.Open(filepath.Join(dir, name))
			if err != nil {
				addIssue(root, issues, sourcePath, err.Error())
				continue
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				addIssue(root, issues, sourcePath, err.Error())
				continue
			}
			if closeErr != nil {
				addIssue(root, issues, sourcePath, closeErr.Error())
				continue
			}
			if expected != hex.EncodeToString(h.Sum(nil)) {
				addIssue(root, issues, sourcePath, "snapshot SHA256 mismatch: "+name)
			}
		}
		bindingPath := filepath.Join(dir, "catalog.yaml")
		bindingDoc, err := readArcadeDocument(bindingPath)
		if err != nil {
			addIssue(root, issues, bindingPath, err.Error())
			continue
		}
		sets := catalogyaml.Value(bindingDoc, "sets")
		if sets == nil || sets.Kind != yaml.MappingNode {
			addIssue(root, issues, bindingPath, "missing sets mapping")
			continue
		}
		checked := map[string]bool{}
		err = catalogyaml.Mapping(sets, func(name string, node *yaml.Node) error {
			platform := catalogyaml.Scalar(catalogyaml.Value(node, "platform"))
			folder := catalogyaml.Scalar(catalogyaml.Value(node, "game"))
			release := catalogyaml.Scalar(catalogyaml.Value(node, "release"))
			if (platform != "Arcade" && !strings.HasPrefix(platform, "Arcade - ")) || !safeArcadeComponent(platform) || !safeArcadeComponent(folder) || !safeArcadeComponent(release) {
				addIssue(root, issues, bindingPath, "invalid catalog path for "+name)
				return nil
			}
			gamePath := filepath.Join(root, "platforms", platform, folder, "game.yaml")
			game, err := readArcadeDocument(gamePath)
			if err != nil {
				addIssue(root, issues, bindingPath, err.Error())
				return nil
			}
			releaseNode := catalogyaml.Value(catalogyaml.Value(game, "releases"), release)
			if releaseNode == nil {
				addIssue(root, issues, bindingPath, fmt.Sprintf("%s refers to unknown release %s", name, release))
			}
			hardware := catalogyaml.Scalar(catalogyaml.Value(node, "hardware"))
			if hardware != "" && (!strings.HasPrefix(hardware, "Arcade - ") || !safeArcadeComponent(hardware)) {
				addIssue(root, issues, bindingPath, "invalid hardware name for "+name)
			}
			family := catalogyaml.Scalar(catalogyaml.Value(node, "family"))
			if platform == "Arcade" || family != "" {
				arcade := catalogyaml.Value(game, "arcade")
				if !strings.HasPrefix(family, "Arcade - ") || !safeArcadeComponent(family) ||
					catalogyaml.Scalar(catalogyaml.Value(arcade, "platform")) != family {
					addIssue(root, issues, bindingPath, "card arcade platform disagrees with snapshot binding: "+name)
				}
				kind := catalogyaml.Scalar(catalogyaml.Value(arcade, "kind"))
				if (kind != "hardware" && kind != "collection") || catalogyaml.Scalar(catalogyaml.Value(arcade, "naming_evidence")) == "" {
					addIssue(root, issues, gamePath, "missing arcade platform kind or naming evidence")
				}
			}
			mame := catalogyaml.Value(releaseNode, "mame")
			// Older snapshots retain their own board evidence when a newer one
			// refreshes this release. Legacy files may omit hardware on both sides.
			if catalogyaml.Scalar(catalogyaml.Value(mame, "snapshot")) == snapshot.Name() &&
				catalogyaml.Scalar(catalogyaml.Value(mame, "hardware")) != hardware {
				addIssue(root, issues, bindingPath, "release hardware disagrees with snapshot binding: "+name)
			}
			card := platform + "/" + folder
			if checked[card] {
				return nil
			}
			checked[card] = true
			fpPath := filepath.Join(filepath.Dir(gamePath), "fingerprints.yaml")
			fp, err := readArcadeDocument(fpPath)
			if err != nil {
				addIssue(root, issues, fpPath, err.Error())
				return nil
			}
			return catalogyaml.Sequence(catalogyaml.Value(fp, "items"), func(item *yaml.Node) error {
				src := catalogyaml.Value(item, "source")
				id := catalogyaml.Scalar(catalogyaml.Value(src, "snapshot"))
				if id == "" {
					return nil
				}
				if !safeArcadeComponent(id) {
					addIssue(root, issues, fpPath, "invalid fingerprint snapshot")
					return nil
				}
				if _, err := catalogfs.Stat(filepath.Join(base, id, "_source.yaml")); err != nil {
					addIssue(root, issues, fpPath, "fingerprint snapshot is missing: "+id)
				}
				if id != snapshot.Name() {
					return nil
				}
				entry := catalogyaml.Scalar(catalogyaml.Value(src, "entry_name"))
				target := catalogyaml.Value(sets, entry)
				if target == nil || catalogyaml.Scalar(catalogyaml.Value(target, "platform")) != platform ||
					catalogyaml.Scalar(catalogyaml.Value(target, "game")) != folder ||
					catalogyaml.Scalar(catalogyaml.Value(target, "release")) != catalogyaml.Scalar(catalogyaml.Value(item, "release")) {
					addIssue(root, issues, fpPath, "fingerprint disagrees with snapshot binding: "+entry)
				}
				kind := catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(item, "file"), "kind"))
				if kind != "rom-component" && kind != "chd-disk" {
					addIssue(root, issues, fpPath, "invalid arcade fingerprint payload kind")
				}
				return nil
			})
		})
		if err != nil {
			addIssue(root, issues, bindingPath, err.Error())
		}
	}
}
func safeArcadeComponent(name string) bool {
	return name != "" && name != "." && name != ".." && isSafePathName(name) && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}
func readArcadeDocument(path string) (*yaml.Node, error) {
	data, err := catalogfs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, err
	}
	return document.Root(), nil
}
