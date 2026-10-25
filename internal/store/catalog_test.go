package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadCatalogConfigReadsFoldRules(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "catalog.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(`schema_version: 0

fold:
  region_priority: [japan, usa, europe]
  region_groups:
    japan: [Japan, JP, JPN, JAP, JA]
    europe:
      - Europe
      - Germany
      - France
  platforms:
    segacd:
      region_priority:
        - usa
        - japan
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	config, err := LoadCatalogConfig(root)
	if err != nil {
		t.Fatalf("LoadCatalogConfig returned error: %v", err)
	}

	if !reflect.DeepEqual(config.Fold.RegionPriority, []string{"japan", "usa", "europe"}) {
		t.Fatalf("RegionPriority = %#v", config.Fold.RegionPriority)
	}
	if !reflect.DeepEqual(config.Fold.RegionGroups["japan"], []string{"Japan", "JP", "JPN", "JAP", "JA"}) {
		t.Fatalf("Japan group = %#v", config.Fold.RegionGroups["japan"])
	}
	if !reflect.DeepEqual(config.Fold.RegionGroups["europe"], []string{"Europe", "Germany", "France"}) {
		t.Fatalf("Europe group = %#v", config.Fold.RegionGroups["europe"])
	}
	if !reflect.DeepEqual(config.Fold.Platforms["segacd"].RegionPriority, []string{"usa", "japan"}) {
		t.Fatalf("platform priority = %#v", config.Fold.Platforms["segacd"].RegionPriority)
	}
}

func TestLoadCatalogConfigAcceptsMissingFile(t *testing.T) {
	config, err := LoadCatalogConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadCatalogConfig returned error: %v", err)
	}
	if !reflect.DeepEqual(config.Fold.RegionPriority, []string{"usa", "world", "europe", "japan", "asia", "russia"}) {
		t.Fatalf("default RegionPriority = %#v", config.Fold.RegionPriority)
	}
}
