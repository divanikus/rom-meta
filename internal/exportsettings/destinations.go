package exportsettings

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"rommeta/internal/exporter"
	"strings"
)

type Destination struct {
	ID          string               `json:"id" yaml:"id"`
	Name        string               `json:"name" yaml:"name"`
	Target      string               `json:"target" yaml:"target"`
	Path        string               `json:"path" yaml:"path"`
	MediaRoot   string               `json:"media_root,omitempty" yaml:"media_root,omitempty"`
	ROMRoot     string               `json:"rom_root,omitempty" yaml:"rom_root,omitempty"`
	Platform    string               `json:"platform,omitempty" yaml:"platform,omitempty"`
	All         bool                 `json:"all,omitempty" yaml:"all,omitempty"`
	ProfileID   string               `json:"profile_id,omitempty" yaml:"profile_id,omitempty"`
	ProfileYAML string               `json:"profile_yaml,omitempty" yaml:"profile_yaml,omitempty"`
	ProfileName string               `json:"profile_name,omitempty" yaml:"profile_name,omitempty"`
	Extra       map[string]yaml.Node `json:"-" yaml:",inline"`
}
type DestinationsDocument struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Destinations  []Destination        `yaml:"destinations"`
	Extra         map[string]yaml.Node `yaml:",inline"`
}

func ValidateDestination(d *Destination) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 120 {
		return fmt.Errorf("give this destination a name (up to 120 characters)")
	}
	if d.Target != "es-de" && d.Target != "launchbox" {
		return fmt.Errorf("choose ES-DE or LaunchBox")
	}
	d.Path = strings.TrimSpace(d.Path)
	d.ROMRoot = strings.TrimSpace(d.ROMRoot)
	d.MediaRoot = strings.TrimSpace(d.MediaRoot)
	for _, path := range []string{d.Path, d.ROMRoot, d.MediaRoot} {
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("use absolute paths on the machine running this command or serve")
		}
		if strings.ContainsRune(path, 0) {
			return fmt.Errorf("invalid path")
		}
	}
	if d.Path == "" {
		return fmt.Errorf("choose a destination folder")
	}
	if d.MediaRoot != "" && d.Target != "es-de" {
		return fmt.Errorf("a separate media folder is only supported for ES-DE")
	}
	if len(d.ProfileYAML) > 1<<20 {
		return fmt.Errorf("profile must be at most 1 MiB")
	}
	if d.ProfileYAML != "" {
		if err := exporter.ValidateExportProfile(d.ProfileYAML); err != nil {
			return fmt.Errorf("profile: %w", err)
		}
	}
	if len(d.ProfileName) > 120 {
		return fmt.Errorf("profile name is too long")
	}
	return nil
}
