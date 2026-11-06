// Package exportsettings shares saved export choices between serve and the CLI.
package exportsettings

import (
	"crypto/sha256"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"rommeta/internal/catalogyaml"
	"strings"
)

type SavedProfile struct {
	ID          string               `json:"id" yaml:"id"`
	Name        string               `json:"name" yaml:"name"`
	Target      string               `json:"target" yaml:"target"`
	ProfileYAML string               `json:"profile_yaml" yaml:"profile_yaml"`
	Extra       map[string]yaml.Node `json:"-" yaml:",inline"`
}
type ProfilesDocument struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Profiles      []SavedProfile       `yaml:"profiles"`
	Extra         map[string]yaml.Node `yaml:",inline"`
}

func File(root, name string) (string, error) {
	if name != "export-destinations.yaml" && name != "export-profiles.yaml" {
		return "", fmt.Errorf("unknown export settings file")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "config", name)
	for p := path; p != root; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("export configuration cannot use symlinks")
		}
	}
	return path, nil
}
func read(root, name string, doc any) (string, os.FileMode, error) {
	path, err := File(root, name)
	if err != nil {
		return "", 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", 0, err
	}
	mode := os.FileMode(0600)
	if len(data) > 0 {
		if len(data) > 2<<20 {
			return "", 0, fmt.Errorf("export configuration exceeds 2 MiB")
		}
		if _, err = catalogyaml.Parse(data); err != nil {
			return "", 0, err
		}
		if err = yaml.Unmarshal(data, doc); err != nil {
			return "", 0, err
		}
		if info, e := os.Stat(path); e == nil {
			mode = info.Mode().Perm()
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), mode, nil
}
func ReadDestinations(root string) (DestinationsDocument, string, os.FileMode, error) {
	doc := DestinationsDocument{SchemaVersion: 1, Destinations: []Destination{}}
	revision, mode, err := read(root, "export-destinations.yaml", &doc)
	if err == nil && doc.SchemaVersion != 1 {
		err = fmt.Errorf("unsupported export destinations schema")
	}
	if doc.Destinations == nil {
		doc.Destinations = []Destination{}
	}
	return doc, revision, mode, err
}
func ReadProfiles(root string) (ProfilesDocument, string, os.FileMode, error) {
	doc := ProfilesDocument{SchemaVersion: 1, Profiles: []SavedProfile{}}
	revision, mode, err := read(root, "export-profiles.yaml", &doc)
	if err == nil && doc.SchemaVersion != 1 {
		err = fmt.Errorf("unsupported export profiles schema")
	}
	if doc.Profiles == nil {
		doc.Profiles = []SavedProfile{}
	}
	return doc, revision, mode, err
}
func FindDestination(root, name string) (Destination, error) {
	doc, _, _, err := ReadDestinations(root)
	if err != nil {
		return Destination{}, err
	}
	var found []Destination
	for _, d := range doc.Destinations {
		if d.ID == name || strings.EqualFold(d.Name, strings.TrimSpace(name)) {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		return Destination{}, fmt.Errorf("saved destination %q: expected one match, found %d; use --list-destinations", name, len(found))
	}
	return found[0], nil
}
func FindProfile(root, name string) (SavedProfile, error) {
	doc, _, _, err := ReadProfiles(root)
	if err != nil {
		return SavedProfile{}, err
	}
	var found []SavedProfile
	for _, p := range doc.Profiles {
		if p.ID == name || strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		return SavedProfile{}, fmt.Errorf("saved profile %q: expected one match, found %d; use --list-profiles", name, len(found))
	}
	return found[0], nil
}
func ResolveDestinationProfile(root string, d *Destination) error {
	if d.ProfileID == "" {
		return nil
	}
	doc, _, _, err := ReadProfiles(root)
	if err != nil {
		return err
	}
	for _, p := range doc.Profiles {
		if p.ID == d.ProfileID {
			if p.Target != d.Target {
				return fmt.Errorf("saved profile %q uses %s, destination uses %s", p.Name, p.Target, d.Target)
			}
			d.ProfileYAML = p.ProfileYAML
			d.ProfileName = p.Name
			return nil
		}
	}
	return fmt.Errorf("saved profile for destination %q no longer exists; choose another profile", d.Name)
}
