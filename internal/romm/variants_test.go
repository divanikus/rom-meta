package romm

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

func derivativeFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "catalog")
	roms := filepath.Join(base, "roms")
	payload := []byte("synthetic translated payload")
	data := fmt.Sprintf("title: Original\ndefault_release: Original (USA)\nreleases:\n  Original (USA): {}\n  Original (Japan): {regions: [Japan]}\nvariants:\n  English:\n    kind: translation\n    title: Translated Adventure\n    based_on: Original (Japan)\n    languages: [en]\n    group: Test team\n    version: '1.0'\n  Fixed:\n    kind: fix\n    based_on: English\nsources:\n  igdb:\n    matches:\n      - {release: Original (USA), igdb_id: 10, slug: wrong-default, developer: Wrong developer}\n      - release: Original (Japan)\n        igdb_id: 20\n        slug: original-game\n        developer: Original developer\n        release_date: '1991-05-06'\n        descriptions: {en: Inherited Japanese-release prose}\n  hasheous:\n    matches:\n      - release: Original (Japan)\n        hasheous_id: 30\n        matched_query: sha1:%x\n        signatures: [NoIntros, Redump, RetroAchievements]\n", sha1.Sum(payload))
	put := func(name string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	game := filepath.Join(root, "platforms", "Sega - Game Gear", "Game")
	put(filepath.Join(root, "platforms", "Sega - Game Gear", "_platform.yaml"), []byte("name: Sega - Game Gear\n"))
	put(filepath.Join(game, "game.yaml"), []byte(data))
	put(filepath.Join(game, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - variant: Fixed\n    source: {kind: dat, name: No-Intro}\n    file:\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum(payload))))
	put(filepath.Join(roms, "translations", "Completely renamed.gg"), payload)
	return root, roms
}

func derivativeSelection(t *testing.T, root, roms, system, platform, rom string, named bool) catalogapi.Selection {
	t.Helper()
	var observed exporter.ObservedInput
	var err error
	if named {
		observed, err = exporter.ObserveCatalogInput(roms, rom)
	} else {
		observed, err = exporter.ObserveInput(roms, system, rom)
	}
	if err != nil {
		t.Fatal(err)
	}
	server, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server)
	defer srv.Close()
	body, _ := json.Marshal(catalogapi.LookupRequest{Platform: platform, Inputs: []exporter.ObservedInput{observed}, GameBinding: named || system == "psx", Media: "none"})
	response, err := http.Post(srv.URL+"/v1/lookup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	var selected catalogapi.Selection
	if err = json.NewDecoder(response.Body).Decode(&selected); err != nil {
		t.Fatal(err)
	}
	if len(selected.Inputs) != 1 {
		t.Fatalf("%+v", selected.Coverage)
	}
	return selected
}
func projectDerivative(t *testing.T, s catalogapi.Selection) (*ProviderEvidence, map[string]string) {
	t.Helper()
	item := s.Inputs[0]
	card := s.Cards[item.Record.Game]
	p := item.Payload
	ev, form, err := ProjectSnapshot(item.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), VerificationProof{Size: p.Size, SHA1: p.SHA1, SHA256: p.SHA256, MD5: p.MD5, CRC32: p.CRC32, Flags: map[string]bool{"redump_match": true}}, map[string]json.RawMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Derivative == nil || ev.Derivative.Variant != "Fixed" || ev.Derivative.MetadataRelease != "Original (Japan)" || ev.Slots["igdb"].ID != 20 || ev.Slots["igdb"].Scope != "exact-release" {
		t.Fatalf("%+v", ev)
	}
	for key, value := range ev.Verification.Flags {
		if value {
			t.Fatalf("derived flag promoted: %s", key)
		}
	}
	if ev.Verification.Authority != "" || ev.Verification.HasheousMatchIndex != nil {
		t.Fatal("inherited authority")
	}
	if form["summary"] != "Inherited Japanese-release prose" || !strings.Contains(form["name"], "Translated Adventure") {
		t.Fatal(form)
	}
	return ev, form
}
func TestVariantNativeProjectionUsesBaseAndKeepsPatchedProof(t *testing.T) {
	root, roms := derivativeFixture(t)
	selection := derivativeSelection(t, root, roms, "gamegear", "Sega - Game Gear", "translations/Completely renamed.gg", false)
	ev, _ := projectDerivative(t, selection)
	if ev.Verification.SHA1 == "" || len(ev.Derivative.Info.Ancestors) != 1 || ev.Derivative.Info.Ancestors[0].Group != "Test team" {
		t.Fatal("lost actual proof or ancestor credit")
	}
	item := selection.Inputs[0]
	card := selection.Cards[item.Record.Game]
	proof := VerificationProof{Size: item.Payload.Size, SHA1: item.Payload.SHA1, SHA256: item.Payload.SHA256, MD5: item.Payload.MD5, CRC32: item.Payload.CRC32}
	for _, kind := range []string{"wrong base", "wrong payload", "wrong credits", "dual target", "missing variant"} {
		t.Run(kind, func(t *testing.T) {
			record := item.Record
			record.VariantInfo = nil
			raw, _ := json.Marshal(item.Record)
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			fp := card.Fingerprints.YAML
			p := proof
			switch kind {
			case "wrong base":
				record.MetadataRelease = "Original (USA)"
			case "wrong payload":
				p.SHA1 = strings.Repeat("f", 40)
			case "wrong credits":
				record.VariantInfo.Group = "forged"
			case "dual target":
				fp = strings.Replace(fp, "variant: Fixed", "variant: Fixed\n    release: Original (Japan)", 1)
			case "missing variant":
				record.Variant = "Absent"
			}
			if _, _, err := ProjectSnapshot(record, []byte(card.Game.YAML), []byte(fp), p, nil); err == nil {
				t.Fatal("accepted " + kind)
			}
		})
	}
}
func TestVariantNativeNameProofStaysUnverified(t *testing.T) {
	root, roms := derivativeFixture(t)
	raw, err := os.ReadFile(filepath.Join(roms, "translations", "Completely renamed.gg"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(roms, "Fixed.gg"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := derivativeSelection(t, root, roms, "gamegear", "Sega - Game Gear", "Fixed.gg", true)
	ev, _ := projectDerivative(t, s)
	if ev.Derivative.MatchedBy != "catalog-name" || !emptyProof(ev.Verification) {
		t.Fatal("name proof became payload proof")
	}
}
func TestVariantNativeCombinedCHDStaysHeaderEvidence(t *testing.T) {
	root, roms := derivativeFixture(t)
	old := filepath.Join(root, "platforms", "Sega - Game Gear", "Game")
	game := filepath.Join(root, "platforms", "Sony - PlayStation", "Game")
	if err := os.MkdirAll(game, 0755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(old, "game.yaml"))
	if err := os.WriteFile(filepath.Join(game, "game.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(game), "_platform.yaml"), []byte("name: Sony - PlayStation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := make([]byte, 256)
	copy(h, "MComprHD")
	binary.BigEndian.PutUint32(h[8:12], 124)
	binary.BigEndian.PutUint32(h[12:16], 5)
	binary.BigEndian.PutUint64(h[32:40], 2352*10)
	binary.BigEndian.PutUint64(h[40:48], 124)
	binary.BigEndian.PutUint32(h[56:60], 2352*4)
	binary.BigEndian.PutUint32(h[60:64], 2352)
	h[64] = 1
	h[84] = 2
	fp := fmt.Sprintf("items:\n  - variant: Fixed\n    file:\n      kind: chd-combined-sha1\n      sha1: %s\n", hex.EncodeToString(h[84:104]))
	if err := os.WriteFile(filepath.Join(game, "fingerprints.yaml"), []byte(fp), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roms, "Translated.chd"), h, 0600); err != nil {
		t.Fatal(err)
	}
	s := derivativeSelection(t, root, roms, "psx", "Sony - PlayStation", "Translated.chd", false)
	ev, _ := projectDerivative(t, s)
	if ev.Derivative.MatchedBy != "variant-chd-header" || !emptyProof(ev.Verification) {
		t.Fatal("CHD header became raw proof")
	}
}

func TestGameOnlyVariantNativeProjectionUsesGameMetadata(t *testing.T) {
	for _, explicitDefault := range []bool{true, false} {
		t.Run(fmt.Sprint("default=", explicitDefault), func(t *testing.T) {
			root, roms := derivativeFixture(t)
			name := filepath.Join(root, "platforms", "Sega - Game Gear", "Game", "game.yaml")
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.Replace(string(raw), "    based_on: Original (Japan)\n", "", 1)
			data = strings.Replace(data, "developer: Wrong developer}", "developer: Game developer, descriptions: {en: Game card prose}}", 1)
			if !explicitDefault {
				data = strings.Replace(data, "default_release: Original (USA)\n", "", 1)
			}
			if err = os.WriteFile(name, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			s := derivativeSelection(t, root, roms, "gamegear", "Sega - Game Gear", "translations/Completely renamed.gg", false)
			item := s.Inputs[0]
			card := s.Cards[item.Record.Game]
			p := item.Payload
			proof := VerificationProof{Size: p.Size, SHA1: p.SHA1, SHA256: p.SHA256, MD5: p.MD5, CRC32: p.CRC32, Flags: map[string]bool{"redump_match": true}}
			ev, form, err := ProjectSnapshot(item.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), proof, nil)
			if err != nil {
				t.Fatal(err)
			}
			if item.Record.Release != "" || ev.Derivative.MetadataRelease != "Original (USA)" || ev.Derivative.Info.Ancestors[0].BasedOn != "" || ev.Slots["igdb"].ID != 10 || form["summary"] != "Game card prose" || !strings.Contains(form["name"], "Translated Adventure") {
				t.Fatalf("game metadata projection: %+v %v", ev, form)
			}
			for _, set := range ev.Verification.Flags {
				if set {
					t.Fatal("original verification inherited")
				}
			}
			if ev.Verification.Authority != "" || ev.Verification.HasheousMatchIndex != nil || ev.Verification.SHA1 != proof.SHA1 {
				t.Fatal("modified proof changed")
			}
			wrong := item.Record
			wrong.MetadataRelease = "Original (Japan)"
			if _, _, err := ProjectSnapshot(wrong, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), proof, nil); err == nil {
				t.Fatal("accepted arbitrary metadata release")
			}
		})
	}
}
