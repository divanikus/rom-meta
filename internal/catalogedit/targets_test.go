package catalogedit

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/curation"
	"rommeta/internal/sourceyaml"
	"strings"
	"testing"
)

const targetFixture = `title: Example
releases:
  Example (USA): {}
  Example (Japan): {}
custom: keep
sources:
  launchbox:
    status: matched
    matches:
      - database_id: '10'
        title: First
        developer: Provider one # keep comment
      - database_id: '20'
        title: Second
        releases:
          - release: Example (Japan)
            publisher: Original
`

func TestEditRecordsAtomicScopedAndRefresh(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "platforms", "Console", "Example")
			if e := os.MkdirAll(dir, 0755); e != nil {
				t.Fatal(e)
			}
			file := filepath.Join(dir, "game.yaml")
			if e := os.WriteFile(file, []byte(targetFixture), 0644); e != nil {
				t.Fatal(e)
			}
			if packed {
				catalogfstest.Pack(t, filepath.Dir(dir))
			}
			registry, _ := curation.Load(root)
			registry, a, e := curation.Save(root, registry.Revision, "", "Short")
			if e != nil {
				t.Fatal(e)
			}
			_, b, e := curation.Save(root, registry.Revision, "", "Long")
			if e != nil {
				t.Fatal(e)
			}
			card, e := Read(root, "Console", "Example")
			if e != nil {
				t.Fatal(e)
			}
			zero := 0
			edits := []Edit{
				{Target: Target{Kind: "source", Source: "launchbox", Match: 0}, Fields: Patch{"developer": "Corrected"}},
				{Target: Target{Kind: "source", Source: "launchbox", Match: 1, ReleaseIndex: &zero}, Fields: Patch{"publisher": "Nested"}},
				{Target: Target{Kind: "manual", Source: a.ID, Match: -1}, Fields: Patch{"descriptions": map[string]any{"en": "Short", "ru": "Кратко"}}},
				{Target: Target{Kind: "manual", Source: b.ID, Match: -1, Scope: "Example (Japan)"}, Fields: Patch{"developer": "My Japanese studio"}},
			}
			saved, e := SaveEdits(root, "Console", "Example", card.Revision, edits)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(saved.YAML, "# keep comment") || !strings.Contains(saved.YAML, "custom: keep") || !strings.Contains(saved.YAML, "database_id: '20'") {
				t.Fatal(saved.YAML)
			}
			records, releases, e := Records([]byte(saved.YAML))
			if e != nil || len(releases) != 2 {
				t.Fatal(e, releases)
			}
			for _, r := range records {
				if r.Target.Source == a.ID && r.Target.Match == 0 {
					if len(r.Fields) != 1 {
						t.Fatal("variant copied unrelated fields", r)
					}
				}
			}
			bad := []Edit{{Target: Target{Kind: "source", Source: "launchbox", Match: 0}, Fields: Patch{"developer": "Must not save"}}, {Target: Target{Kind: "source", Source: "launchbox", Match: 99}, Fields: Patch{"publisher": "Bad"}}}
			if _, e = SaveEdits(root, "Console", "Example", saved.Revision, bad); e == nil {
				t.Fatal("invalid last edit accepted")
			}
			unchanged, _ := Read(root, "Console", "Example")
			if unchanged.Revision != saved.Revision {
				t.Fatal("partial write")
			}
			if _, e = SaveEdits(root, "Console", "Example", card.Revision, edits); e == nil {
				t.Fatal("stale accepted")
			}
			if e = sourceyaml.WriteSourceMetadata(file, "launchbox", "    matches:\n      - title: Refreshed\n"); e != nil {
				t.Fatal(e)
			}
			refreshed, _ := Read(root, "Console", "Example")
			for _, expected := range []string{a.ID, b.ID, "Short", "My Japanese studio"} {
				if !strings.Contains(refreshed.YAML, expected) {
					t.Fatal(refreshed.YAML)
				}
			}
		})
	}
}
func TestInvalidSourcePatches(t *testing.T) {
	target := Target{Kind: "source", Source: "launchbox", Match: 0}
	for _, patch := range []Patch{{"matches": []any{}}, {"__proto__": "x"}, {"descriptions": "wrong"}, {"descriptions": map[string]any{"en": 3}}, {"genres": "x"}, {"title": true}, {"releases": []any{3}}} {
		if _, e := ApplyTarget([]byte(targetFixture), target, patch); e == nil {
			t.Fatal("accepted", patch)
		}
	}
	if _, e := ApplyTarget([]byte(targetFixture), Target{Kind: "manual", Source: "manual.test", Scope: "Unknown"}, Patch{"developer": "x"}); e == nil {
		t.Fatal("invalid scope")
	}
}
