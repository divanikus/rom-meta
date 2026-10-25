package catalogyaml

import (
	"strings"
	"testing"
)

func TestPreserveUnknownTopLevelReplacesGeneratedFields(t *testing.T) {
	existing := `# curated catalog
title: "Old title"
default_release: "Old (USA)"

releases:
  "Old (USA)":
    title: "Old"

aliases: ["Human alias"]

fold:
  ignore_evidence:
    - "igdb.igdb_id=42"

sources:
  igdb:
    matches:
      - igdb_id: 42
`
	generated := `title: "New title"
default_release: "New (USA)"

releases:
  "New (USA)":
    title: "New"
`

	updated, err := PreserveUnknownTopLevel(existing, generated, "title", "default_release", "releases")
	if err != nil {
		t.Fatalf("PreserveUnknownTopLevel returned error: %v", err)
	}
	for _, expected := range []string{`title: "New title"`, `"New (USA)":`, `aliases: ["Human alias"]`, `fold:`, `sources:`, `igdb_id: 42`} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated YAML missing %q:\n%s", expected, updated)
		}
	}
	for _, unwanted := range []string{`title: "Old title"`, `"Old (USA)":`} {
		if strings.Contains(updated, unwanted) {
			t.Fatalf("updated YAML retained %q:\n%s", unwanted, updated)
		}
	}
}

func TestPreserveUnknownTopLevelReturnsGeneratedForEmptyInput(t *testing.T) {
	updated, err := PreserveUnknownTopLevel("", "title: Test\n", "title")
	if err != nil {
		t.Fatalf("PreserveUnknownTopLevel returned error: %v", err)
	}
	if updated != "title: Test\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestHasAnyTopLevel(t *testing.T) {
	found, err := HasAnyTopLevel("title: Test\n\nsources:\n  igdb:\n", "fold", "sources")
	if err != nil {
		t.Fatalf("HasAnyTopLevel returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected sources to be found")
	}
}
