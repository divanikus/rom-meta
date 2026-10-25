package catalogfold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPS3RegistryValidation(t *testing.T) {
	if _, err := builtinPS3Families(); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []struct{ old, next string }{
		{"version: 1", "version: 2"},
		{"BLES00407", "BLES00406"},
		{"BLES00407", "SLUS21813"},
		{"id: fifa-world-cup-2010", "id: quantum-of-solace"},
		{"reviewed:", "unchecked:"},
		{"https://serialstation.com", "http://serialstation.com"},
	} {
		bad := strings.Replace(string(ps3FamilyData), replacement.old, replacement.next, 1)
		if _, err := parsePS3Families([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid registry: %+v", replacement)
		}
	}
	if _, err := parsePS3Families(append(append([]byte{}, ps3FamilyData...), []byte("\n---\nversion: 1\n")...)); err == nil {
		t.Fatal("accepted extra YAML document")
	}
}

func TestPS3DiscFamiliesUseSerialsAndPreserveEnrichment(t *testing.T) {
	root := t.TempDir()
	platform := "Sony - PlayStation 3"
	names := []string{"007 - Quantum of Solace", "007 - Kvant Miloserdiya", "007 - Nagusame no Houshuu"}
	serials := []string{"BLUS-30198", "bles00407", "BLJM 60137"}
	regions := []string{"USA", "Russia", "Japan"}
	paths := []string{}
	for i, name := range names {
		paths = append(paths, writeSerialGame(t, root, platform, name, regions[i], serials[i], "", "", fmt.Sprintf("  hasheous:\n    matches:\n      - hasheous_id: '%d'\n        title: %q\n", 100+i, name)))
		writeFoldFile(t, filepath.Join(paths[i], "media", "box.jpg"), fmt.Sprintf("artwork %d", i))
	}
	// The scope is PS3, even if unrelated hardware has identical source fields.
	other := writeSerialGame(t, root, "Other Console", names[1], "Russia", serials[1], "", "", "")
	dry, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil || dry.CandidateGroupCount != 1 || dry.HeldGroupCount != 0 || dry.ReviewRequiredCount != 0 {
		t.Fatalf("%v %+v", err, dry)
	}
	group := dry.Groups[0]
	if len(group.Evidence) != 1 || !isPS3DiscFamily(group.Evidence[0]) || group.Evidence[0].URL == "" || group.Keep.Folder != names[0] {
		t.Fatalf("%+v", group)
	}
	result, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 || len(result.Backups) != 1 {
		t.Fatalf("%v %+v", err, result)
	}
	body := readFoldFile(t, filepath.Join(paths[0], "game.yaml"))
	for i, name := range names {
		if !strings.Contains(body, name) || !strings.Contains(body, serials[i]) || !strings.Contains(body, fmt.Sprintf("hasheous_id: '%d'", 100+i)) {
			t.Fatal("lost release/source", name, body)
		}
		if !strings.Contains(readFoldFile(t, filepath.Join(paths[0], "fingerprints.yaml")), name) {
			t.Fatal("lost fingerprint", name)
		}
		media := filepath.Join(paths[0], "media", "box.jpg")
		if i > 0 {
			media = filepath.Join(paths[0], "media", "_fold", name, "box.jpg")
		}
		if readFoldFile(t, media) != fmt.Sprintf("artwork %d", i) {
			t.Fatal("lost media", name)
		}
		if readFoldFile(t, filepath.Join(result.Backups[0], name, "media", "box.jpg")) != fmt.Sprintf("artwork %d", i) {
			t.Fatal("lost backup", name)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal(err)
	}
	again, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil || again.CandidateGroupCount != 0 {
		t.Fatal(err, again)
	}
}

func TestPS3DiscFamiliesConservativeCoverage(t *testing.T) {
	for _, tt := range []struct {
		name, platform, serial, authority, extra, releaseExtra string
		editions                                               bool
	}{
		{name: "unknown serial", serial: "BLJM99999"},
		{name: "other family", serial: "BLJM60222"},
		{name: "unknown component", serial: "BLJM60137, BLJM99999"},
		{name: "mixed known families", serial: "BLJM60137, BLJM60222"},
		{name: "empty component", serial: "BLJM60137,"},
		{name: "unverified suffix", serial: "BLJM60137#"},
		{name: "other hardware", platform: "Sony - PlayStation 2"},
		{name: "different authority", authority: "no-intro"},
		{name: "ignored registry", extra: "fold:\n  ignore_evidence: ['ps3.disc_family=quantum-of-solace']\n"},
		{name: "prior rejection", extra: "fold:\n  ignore_evidence: ['launchbox.database_id=12']\n"},
		{name: "MAME", releaseExtra: "    mame:\n      snapshot: mame-0.289\n"},
		{name: "unknown release", releaseExtra: "  Unknown:\n    title: Unknown\n"},
		{name: "editions only", editions: true},
		{name: "demo", releaseExtra: "  Localized (Japan) (Demo):\n    authority:\n      source: redump\n      serial: BLJM60137\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			platform := tt.platform
			if platform == "" {
				platform = "Sony - PlayStation 3"
			}
			serial := tt.serial
			if serial == "" {
				serial = "BLJM60137"
			}
			writeSerialGame(t, root, platform, "English", "USA", "BLUS30198", "", "", "")
			path := writeSerialGame(t, root, platform, "Localized", "Japan", serial, tt.authority, tt.extra, "")
			if tt.releaseExtra != "" {
				writeFoldFile(t, filepath.Join(path, "game.yaml"), readFoldFile(t, filepath.Join(path, "game.yaml"))+tt.releaseExtra)
			}
			r, e := Fold(Options{DataRoot: root, Apply: true, EditionsOnly: tt.editions})
			if e != nil || r.CandidateGroupCount != 0 || len(r.Backups) != 0 {
				t.Fatalf("%v %+v", e, r)
			}
		})
	}
	t.Run("comma separated same family", func(t *testing.T) {
		root := t.TempDir()
		writeSerialGame(t, root, "PS3", "English", "USA", "BLUS30198", "", "", "")
		writeSerialGame(t, root, "PS3", "Localized", "Europe", "BLES00406, BLES00411", "", "", "")
		r, e := Fold(Options{DataRoot: root})
		if e != nil || r.CandidateGroupCount != 1 {
			t.Fatal(e, r)
		}
	})
}

func TestPS3DiscFamiliesDoNotBypassHolds(t *testing.T) {
	for _, tt := range []struct{ name, extra, source, thirdSerial, thirdSource string }{
		{name: "direct IDs", source: serialProvider("11")},
		{name: "unscoped alias", source: "  launchbox:\n    matches:\n      - database_id: '10'\n        title: Localized\n        alternate_titles: [English]\n"},
		{name: "manual fields", extra: "manual_notes: keep\n"},
		{name: "unverified bridge", source: serialProvider("10"), thirdSerial: "BLJM99999", thirdSource: serialProvider("10")},
		{name: "different registered family", source: serialProvider("10"), thirdSerial: "BLJM60222", thirdSource: serialProvider("10")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			platform := "Sony - PlayStation 3"
			anchorSource := serialProvider("10")
			if tt.name == "unscoped alias" {
				anchorSource = ""
			}
			writeSerialGame(t, root, platform, "English", "USA", "BLUS30198", "", "", anchorSource)
			writeSerialGame(t, root, platform, "Localized", "Japan", "BLJM60137", "", tt.extra, tt.source)
			if tt.thirdSerial != "" {
				writeSerialGame(t, root, platform, "Third", "Europe", tt.thirdSerial, "", "", tt.thirdSource)
			}
			r, e := Fold(Options{DataRoot: root, Review: func(ReviewPrompt) (ReviewDecision, error) { return ReviewApply, nil }})
			if e != nil || r.HeldGroupCount != 1 || r.AppliedGroupCount != 0 || len(r.Backups) != 0 {
				t.Fatalf("%v %+v", e, r)
			}
		})
	}
}
