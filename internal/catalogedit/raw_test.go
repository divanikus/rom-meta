package catalogedit

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"strings"
	"testing"
)

func TestRawValidationAndExactText(t *testing.T) {
	good := targetFixture + "# Owner formatting\ncustom_object: {enabled: true, score: 4}\n"
	if data, err := ValidateRaw(good); err != nil || string(data) != good {
		t.Fatal(string(data), err)
	}
	cases := []string{
		"title: A\ncustom: .nan\n", "title: A\nreleases: {Edition: {title: [Wrong]}}\n",
		"title: A\nsources:\n  provider:\n    matches: [{releases: [Missing]}]\n",
		"title: A\ntitle: B\n", "title: [Wrong]\n", "title: A\n---\ntitle: B\n",
		"title: A\nsources: []\n", "title: A\nsources:\n  provider:\n    matches: {}\n",
		"title: A\nsources:\n  provider:\n    matches: [{title: 12}]\n",
		"title: A\ndefault_release: Missing\n", "title: A\nreleases: {Edition: []}\n",
		"title: A\nsources:\n  provider:\n    matches: [{release: Missing}]\n",
		"title: A\ncustom: {a: 1, a: 2}\n", "title: A\ncustom: &a [*a]\n",
		"title: A\ncustom: !!str \u0000\n",
	}
	for _, s := range cases {
		if _, err := ValidateRaw(s); err == nil {
			t.Errorf("accepted invalid YAML: %q", s)
		}
	}
}
func TestRawSaveChecksReferencesRevisionAndPackedCards(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "platforms", "Console", "Example")
			if e := os.MkdirAll(dir, 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(targetFixture), 0640); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "fingerprints.yaml"), []byte("items:\n  - release: Example (USA)\n    file: {name: example.bin}\n"), 0644); e != nil {
				t.Fatal(e)
			}
			if packed {
				catalogfstest.Pack(t, filepath.Dir(dir))
			}
			card, e := Read(root, "Console", "Example")
			if e != nil {
				t.Fatal(e)
			}
			broken := strings.ReplaceAll(card.YAML, "Example (USA)", "Removed")
			_, e = SaveEdits(root, "Console", "Example", card.Revision, []Edit{{Target: Target{Kind: "raw"}, YAML: &broken}})
			if e == nil || !strings.Contains(e.Error(), "fingerprint") {
				t.Fatal(e)
			}
			unchanged, _ := Read(root, "Console", "Example")
			if unchanged.Revision != card.Revision {
				t.Fatal("invalid raw wrote card")
			}
			good := strings.Replace(card.YAML, "title: Example", "title: Edited", 1)
			saved, e := SaveEdits(root, "Console", "Example", card.Revision, []Edit{{Target: Target{Kind: "raw"}, YAML: &good}})
			if e != nil || saved.YAML != good {
				t.Fatal(saved, e)
			}
			_, e = SaveEdits(root, "Console", "Example", card.Revision, []Edit{{Target: Target{Kind: "raw"}, YAML: &good}})
			if e == nil {
				t.Fatal("stale revision accepted")
			}
		})
	}
}
