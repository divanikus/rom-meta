package catalogedit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
)

const registerGame = "# Game note\ntitle: Orbit\ndefault_release: USA\nreleases:\n  USA:\n    title: Orbit\n    regions: [USA]\n    authority: {entry_name: USA}\n    custom: preserve\n  Japan: {}\nvariants:\n  Translation: {kind: translation, based_on: USA}\nsources:\n  service:\n    releases: [USA]\n    matches:\n      - release: USA\n        releases: [{release: USA, title: Orbit}]\n        authority: {entry_name: USA}\n"
const registerFP = "# Fingerprint note\ncustom: retained\nitems:\n  - id: original\n    release: USA\n    file: {name: Orbit.bin, size: 12, crc32: '1234abcd', custom: keep}\n    source: {name: DAT, entry_name: USA}\n  - release: Japan\n    file: {name: Japan.bin, size: 4, sha1: '0123456789012345678901234567890123456789'}\n"

func registerFixture(t *testing.T, packed bool) (string, string, Card, Register) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "platforms", "Console", "Orbit")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	for name, data := range map[string]string{"game.yaml": registerGame, "fingerprints.yaml": registerFP} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(data), 0640); e != nil {
			t.Fatal(e)
		}
	}
	if packed {
		catalogfstest.Pack(t, filepath.Dir(dir))
	}
	card, e := Read(root, "Console", "Orbit")
	if e != nil {
		t.Fatal(e)
	}
	reg, e := ReadRegister(root, "Console", "Orbit", []byte(card.YAML))
	if e != nil {
		t.Fatal(e)
	}
	return root, dir, card, reg
}
func TestRegisterCRUDLooseAndPacked(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root, dir, card, reg := registerFixture(t, packed)
			patch := RegisterPatch{Releases: []ReleaseEdit{{Original: "USA", Name: "Europe", Fields: Patch{"regions": []any{"Europe"}}}, {Name: "World", Fields: Patch{"title": "World edition"}}}, Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"file": map[string]any{"name": "Changed.bin", "size": float64(25), "crc32": "abcdef01"}}}, {Index: 1, Delete: true}, {Index: -1, Fields: Patch{"release": "World", "file": map[string]any{"name": "New.bin", "size": float64(3), "md5": strings.Repeat("a", 32)}}}}}
			prepared, e := PrepareDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: patch})
			if e != nil {
				t.Fatal(e)
			}
			unchanged, _ := Read(root, "Console", "Orbit")
			if unchanged.Revision != card.Revision {
				t.Fatal("preview wrote game")
			}
			oldFP, _ := catalogfs.ReadFile(filepath.Join(dir, "fingerprints.yaml"))
			if string(oldFP) != registerFP {
				t.Fatal("preview wrote fingerprints")
			}
			saved, e := SaveDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: patch})
			if e != nil {
				t.Fatal(e)
			}
			if prepared.Card.YAML != saved.Card.YAML || len(saved.Register.Fingerprints) != 2 {
				t.Fatal("wrong saved documents")
			}
			for _, want := range []string{"default_release: Europe", "based_on: Europe", "release: Europe", "entry_name: USA", "custom: preserve", "# Game note"} {
				if !strings.Contains(saved.Card.YAML, want) {
					t.Fatalf("missing %s: %s", want, saved.Card.YAML)
				}
			}
			for _, want := range []string{"release: Europe", "custom: keep", "custom: retained", "entry_name: USA", "# Fingerprint note", "id: original"} {
				if !strings.Contains(saved.Register.YAML, want) {
					t.Fatalf("missing %s: %s", want, saved.Register.YAML)
				}
			}
			if _, e = SaveDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: patch}); e == nil {
				t.Fatal("accepted stale card")
			}
			if _, e = SaveDocuments(root, "Console", "Orbit", saved.Card.Revision, nil, DocumentEdit{Revision: reg.Revision}); e == nil {
				t.Fatal("accepted stale fingerprints")
			}
			noOp, e := SaveDocuments(root, "Console", "Orbit", saved.Card.Revision, nil, DocumentEdit{Revision: saved.Register.Revision})
			if e != nil || noOp.Card.Revision != saved.Card.Revision || noOp.Register.Revision != saved.Register.Revision {
				t.Fatal("no-op changed documents", e)
			}
		})
	}
}
func TestRegisterValidationKeepsBothDocuments(t *testing.T) {
	badSize := float64(1.5)
	cases := []RegisterPatch{
		{Releases: []ReleaseEdit{{Original: "USA", Name: "Japan"}}},
		{Releases: []ReleaseEdit{{Original: "USA", Name: ""}}},
		{Releases: []ReleaseEdit{{Original: "USA", Name: "USA", Fields: Patch{"release_date": "2026-02-30"}}}},
		{Releases: []ReleaseEdit{{Original: "USA", Delete: true}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"release": "Missing"}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"variant": "Translation"}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"file": map[string]any{"crc32": "oops"}}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"file": map[string]any{"size": badSize}}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"file": map[string]any{"size": float64(-1)}}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Fields: Patch{"file": map[string]any{"crc32": nil}}}}},
		{Fingerprints: []FingerprintEdit{{Index: 0, Delete: true}, {Index: 0, Delete: true}}},
		{Fingerprints: []FingerprintEdit{{Index: -1, Fields: Patch{"release": "USA", "file": map[string]any{"name": "new", "size": float64(1), "crc32": "12345678"}}}, {Index: 2, Delete: true}}},
	}
	for i, patch := range cases {
		root, dir, card, reg := registerFixture(t, false)
		_, e := SaveDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: patch})
		if e == nil {
			t.Fatalf("case %d accepted", i)
		}
		for name, want := range map[string]string{"game.yaml": registerGame, "fingerprints.yaml": registerFP} {
			b, _ := os.ReadFile(filepath.Join(dir, name))
			if string(b) != want {
				t.Fatalf("case %d wrote %s", i, name)
			}
		}
	}
}
func TestRegisterDeleteAndReassignInSameDraft(t *testing.T) {
	root, _, card, reg := registerFixture(t, false)
	next := "USA"
	patch := RegisterPatch{Releases: []ReleaseEdit{{Original: "Japan", Delete: true}}, DefaultRelease: &next, Fingerprints: []FingerprintEdit{{Index: 1, Fields: Patch{"release": "USA"}}}}
	saved, e := SaveDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: patch})
	if e != nil {
		t.Fatal(e)
	}
	if len(saved.Register.Releases) != 1 || saved.Register.Fingerprints[1].Fields["release"] != "USA" {
		t.Fatal(saved.Register)
	}
}
func TestRegisterMissingFileAndLegacyRoms(t *testing.T) {
	root, dir, card, _ := registerFixture(t, false)
	if e := os.Remove(filepath.Join(dir, "fingerprints.yaml")); e != nil {
		t.Fatal(e)
	}
	reg, e := ReadRegister(root, "Console", "Orbit", []byte(card.YAML))
	if e != nil {
		t.Fatal(e)
	}
	req := DocumentEdit{Revision: reg.Revision, Patch: RegisterPatch{Fingerprints: []FingerprintEdit{{Index: -1, Fields: Patch{"release": "USA", "file": map[string]any{"name": "new", "size": float64(0), "crc32": "00000000"}}}}}}
	result, e := SaveDocuments(root, "Console", "Orbit", card.Revision, nil, req)
	if e != nil || len(result.Register.Fingerprints) != 1 {
		t.Fatal(e)
	}
	_, fp, e := ApplyRegister([]byte(registerGame), []byte(strings.Replace(registerFP, "items:", "roms:", 1)), RegisterPatch{Fingerprints: []FingerprintEdit{{Index: 1, Delete: true}}})
	if e != nil || !strings.Contains(string(fp), "roms:") || strings.Contains(string(fp), "items:") {
		t.Fatal(string(fp), e)
	}
}
func TestRegisterLooseRollbackOnSecondWrite(t *testing.T) {
	root, dir, card, reg := registerFixture(t, false)
	p, e := PrepareDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: RegisterPatch{Releases: []ReleaseEdit{{Original: "USA", Name: "Europe"}}}})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	e = writeDocuments(p, func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("injected second rename failure")
		}
		return os.Rename(from, to)
	})
	if e == nil || calls != 2 {
		t.Fatal("expected failure", e, calls)
	}
	for name, want := range map[string]string{"game.yaml": registerGame, "fingerprints.yaml": registerFP} {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		if string(b) != want {
			t.Fatalf("rollback lost %s", name)
		}
	}
	temps, _ := filepath.Glob(filepath.Join(dir, ".rommeta-edit-*"))
	if len(temps) != 0 {
		t.Fatal(temps)
	}
}
func TestRegisterDraftMaterialization(t *testing.T) {
	root, _, card, reg := registerFixture(t, false)
	p, e := PrepareDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, Patch: RegisterPatch{Releases: []ReleaseEdit{{Original: "USA", Name: "Europe"}}}})
	if e != nil {
		t.Fatal(e)
	}
	raw := p.Card.YAML
	fp := p.Register.YAML
	saved, e := SaveDocuments(root, "Console", "Orbit", card.Revision, []Edit{{Target: Target{Kind: "raw"}, YAML: &raw}}, DocumentEdit{Revision: reg.Revision, YAML: &fp, Patch: RegisterPatch{Releases: []ReleaseEdit{{Original: "Europe", Name: "World"}}}})
	if e != nil {
		t.Fatal(e)
	}
	doc, _ := catalogyaml.Parse([]byte(saved.Card.YAML))
	if catalogyaml.Scalar(catalogyaml.Value(doc.Root(), "default_release")) != "World" {
		t.Fatal(saved.Card.YAML)
	}
}

func TestRegisterRemovalReassignsKnownReferencesOnly(t *testing.T) {
	game, fp, e := ApplyRegister([]byte(registerGame), []byte(registerFP), RegisterPatch{Releases: []ReleaseEdit{{Original: "USA", Delete: true, Replacement: "Japan"}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"default_release: Japan", "based_on: Japan", "release: Japan", "entry_name: USA"} {
		if !strings.Contains(string(game), want) {
			t.Fatal(want, string(game))
		}
	}
	if !strings.Contains(string(fp), "release: Japan") || !strings.Contains(string(fp), "entry_name: USA") {
		t.Fatal(string(fp))
	}
	_, _, e = ApplyRegister([]byte(registerGame), []byte(registerFP), RegisterPatch{Releases: []ReleaseEdit{{Original: "USA", Delete: true, Replacement: "Missing"}}})
	if e == nil {
		t.Fatal("accepted missing replacement")
	}
}
func TestRegisterRejectsMalformedMaterializedFingerprints(t *testing.T) {
	root, _, card, reg := registerFixture(t, false)
	for _, bad := range []string{strings.Replace(registerFP, "1234abcd", "not-a-hash", 1), strings.Replace(registerFP, "size: 12", "size: 9999999999999999999999999999", 1), strings.Replace(registerFP, "name: Orbit.bin", "name: Orbit\u0000.bin", 1)} {
		_, e := SaveDocuments(root, "Console", "Orbit", card.Revision, nil, DocumentEdit{Revision: reg.Revision, YAML: &bad})
		if e == nil {
			t.Fatal("accepted invalid draft")
		}
	}
}
