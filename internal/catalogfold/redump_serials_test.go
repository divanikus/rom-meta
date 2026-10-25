package catalogfold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSerialGame(t *testing.T, root, platform, title, region, serial, authority, extra, sources string) string {
	t.Helper()
	if authority == "" {
		authority = "redump"
	}
	body := fmt.Sprintf("title: %q\n%sreleases:\n  %q:\n    title: %q\n    regions: [%q]\n    authority:\n      source: %q\n      serial: %q\n", title, extra, title+" ("+region+")", title, region, authority, serial)
	if sources != "" {
		body += "sources:\n" + sources
	}
	path := filepath.Join(root, "platforms", platform, title)
	writeFoldFile(t, filepath.Join(path, "game.yaml"), body)
	writeFoldFile(t, filepath.Join(path, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: %q\n    sha1: %q\n", title+" ("+region+")", title))
	return path
}

func serialProvider(id string) string {
	return fmt.Sprintf("  launchbox:\n    matches:\n      - database_id: %q\n        match_score: 146\n", id)
}

func TestXboxSerialDiscoveryNeverAutomaticallyMerges(t *testing.T) {
	for _, names := range [][2]string{
		{"007 - Quantum of Solace", "007 - Kvant Miloserdiya"},
		{"2010 FIFA World Cup South Africa", "2010 FIFA World Cup - Minami Africa Taikai"},
		{"Final Fantasy XIII-2", "Final Fantasy XIII-2 - Additional Content Selection"},
		{"Bejeweled 3", "Plants vs. Zombies"},
	} {
		t.Run(names[1], func(t *testing.T) {
			root := t.TempDir()
			a := writeSerialGame(t, root, "Microsoft - Xbox 360", names[0], "USA", "EA-2276", "", "", "")
			b := writeSerialGame(t, root, "Microsoft - Xbox 360", names[1], "Japan", "ea2276", "", "", "")
			beforeA, beforeB := readFoldFile(t, filepath.Join(a, "game.yaml")), readFoldFile(t, filepath.Join(b, "game.yaml"))
			result, err := Fold(Options{DataRoot: root, Apply: true})
			if err != nil || result.CandidateGroupCount != 1 || result.ReviewRequiredCount != 1 || result.AppliedGroupCount != 0 || result.SkippedGroupCount != 1 || len(result.Backups) != 0 {
				t.Fatalf("%v %+v", err, result)
			}
			g := result.Groups[0]
			if g.ReviewRequired == "" || len(g.Evidence) != 1 || !isXboxSerialHint(g.Evidence[0]) || g.Evidence[0].Value != "EA-2276" {
				t.Fatalf("missing hint: %+v", g)
			}
			if readFoldFile(t, filepath.Join(a, "game.yaml")) != beforeA || readFoldFile(t, filepath.Join(b, "game.yaml")) != beforeB {
				t.Fatal("automatic apply changed an unverified game")
			}
		})
	}
}

func TestXboxSerialDiscoveryBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, platform, serial, authority, extra string
		editions                                 bool
	}{
		{name: "other hardware", platform: "Sony - PlayStation 3"},
		{name: "other DAT authority", authority: "no-intro"},
		{name: "missing serial", serial: "missing"},
		{name: "malformed serial", serial: "EA-2276 extra"},
		{name: "multiple serials", serial: "EA-2276, EA-2277"},
		{name: "other Xbox generation", serial: "EA-1276"},
		{name: "ignored hint", extra: "fold:\n  ignore_evidence: ['redump.xbox360_serial=EA-2276']\n"},
		{name: "previous provider rejection", extra: "fold:\n  ignore_evidence: ['launchbox.database_id=10']\n"},
		{name: "edition-only", editions: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			platform := tt.platform
			if platform == "" {
				platform = "Microsoft - Xbox 360"
			}
			serial := tt.serial
			if serial == "" {
				serial = "EA-2276"
			}
			if serial == "missing" {
				serial = ""
			}
			writeSerialGame(t, root, platform, "English Game", "USA", "EA-2276", "", "", "")
			writeSerialGame(t, root, platform, "Localized Game", "Japan", serial, tt.authority, tt.extra, "")
			r, e := Fold(Options{DataRoot: root, EditionsOnly: tt.editions})
			if e != nil || r.CandidateGroupCount != 0 {
				t.Fatalf("%v %+v", e, r)
			}
		})
	}
	t.Run("MAME and mixed releases", func(t *testing.T) {
		for _, extra := range []string{"    mame:\n      snapshot: mame-0.289\n", "  Other:\n    authority:\n      source: redump\n      serial: EA-2277\n", "  Other:\n    title: Unknown\n"} {
			root := t.TempDir()
			writeSerialGame(t, root, "Xbox 360", "English Game", "USA", "EA-2276", "", "", "")
			p := writeSerialGame(t, root, "Xbox 360", "Localized Game", "Japan", "EA-2276", "", "", "")
			writeFoldFile(t, filepath.Join(p, "game.yaml"), readFoldFile(t, filepath.Join(p, "game.yaml"))+extra)
			r, e := Fold(Options{DataRoot: root})
			if e != nil || r.CandidateGroupCount != 0 {
				t.Fatalf("%s: %v %+v", extra, e, r)
			}
		}
	})
}

func TestXboxSerialReviewPreservesDataAndMedia(t *testing.T) {
	root := t.TempDir()
	platform := "Microsoft - Xbox 360"
	a := writeSerialGame(t, root, platform, "English Game", "USA", "AV-2047", "", "aliases: [Older Title]\n", serialProvider("10"))
	b := writeSerialGame(t, root, platform, "Localized Game", "Russia", "AV-2047", "", "", "  hasheous:\n    matches:\n      - hasheous_id: '12'\n        title: Localized Game\n")
	writeFoldFile(t, filepath.Join(a, "media", "box.jpg"), "English artwork")
	writeFoldFile(t, filepath.Join(b, "media", "box.jpg"), "Russian artwork")
	called := 0
	r, e := Fold(Options{DataRoot: root, Review: func(p ReviewPrompt) (ReviewDecision, error) {
		called++
		if p.Group.ReviewRequired == "" || p.Group.HoldReason != "" || len(p.Group.FileChanges) != 1 || p.Group.FileChanges[0].Action != "relocate" {
			t.Fatalf("unreviewable group: %+v", p.Group)
		}
		return ReviewApply, nil
	}})
	if e != nil || called != 1 || r.AppliedGroupCount != 1 || len(r.Backups) != 1 {
		t.Fatalf("%v %+v", e, r)
	}
	for _, want := range []string{"Older Title", "Localized Game (Russia)", "hasheous_id: '12'", "database_id:"} {
		if !strings.Contains(readFoldFile(t, filepath.Join(a, "game.yaml")), want) {
			t.Fatal("missing " + want)
		}
	}
	if !strings.Contains(readFoldFile(t, filepath.Join(a, "fingerprints.yaml")), "Localized Game (Russia)") {
		t.Fatal("lost fingerprint")
	}
	for path, want := range map[string]string{filepath.Join(a, "media", "box.jpg"): "English artwork", filepath.Join(a, "media", "_fold", "Localized Game", "box.jpg"): "Russian artwork", filepath.Join(r.Backups[0], "Localized Game", "media", "box.jpg"): "Russian artwork"} {
		if got := readFoldFile(t, path); got != want {
			t.Fatal(path, got)
		}
	}
	if _, e = os.Stat(b); !os.IsNotExist(e) {
		t.Fatal("localized directory survived", e)
	}
	again, e := Fold(Options{DataRoot: root, Apply: true})
	if e != nil || again.CandidateGroupCount != 0 {
		t.Fatal(e, again)
	}
}

func TestXboxSerialConflictsAndExistingIdentityGroups(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		id                    string
		transitive            bool
		wantApplied, wantHeld int
	}{
		{name: "single identity with different titles needs serial review", id: "10"},
		{name: "different direct identities are held", id: "20", wantHeld: 1},
		{name: "transitive conflict is held", transitive: true, wantHeld: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			platform := "Microsoft - Xbox 360"
			writeSerialGame(t, root, platform, "English Game", "USA", "EA-2276", "", "", serialProvider("10"))
			src := ""
			if tt.id != "" {
				src = serialProvider(tt.id)
			}
			if tt.transitive {
				src = "  igdb:\n    matches:\n      - igdb_id: '33'\n        platforms: ['Xbox 360']\n"
			}
			writeSerialGame(t, root, platform, "Localized Game", "Japan", "EA-2276", "", "", src)
			if tt.transitive {
				writeSerialGame(t, root, platform, "Third Game", "Europe", "EA-2777", "", "", src+serialProvider("20"))
			}
			r, e := Fold(Options{DataRoot: root, Apply: true})
			if e != nil || r.CandidateGroupCount != 1 || r.AppliedGroupCount != tt.wantApplied || r.HeldGroupCount != tt.wantHeld {
				t.Fatalf("%v %+v", e, r)
			}
			if tt.wantHeld > 0 {
				r, e = Fold(Options{DataRoot: root, Review: func(ReviewPrompt) (ReviewDecision, error) { return ReviewApply, nil }})
				if e != nil || r.AppliedGroupCount != 0 || !strings.Contains(r.Groups[0].HoldReason, "conflicting launchbox IDs") {
					t.Fatalf("conflict bypassed: %v %+v", e, r)
				}
			}
		})
	}
}

func TestRejectSerialBridgeRetainsEstablishedIdentities(t *testing.T) {
	root := t.TempDir()
	platform := "Microsoft - Xbox 360"
	for _, v := range []struct{ title, region, id string }{{"English Game", "USA", "10"}, {"German Game", "Germany", "10"}, {"Russian Game", "Russia", ""}} {
		src := ""
		if v.id != "" {
			src = serialProvider(v.id)
		}
		writeSerialGame(t, root, platform, v.title, v.region, "AV-2047", "", "", src)
	}
	r, e := Fold(Options{DataRoot: root, Review: func(p ReviewPrompt) (ReviewDecision, error) {
		if p.Group.ReviewRequired == "" {
			t.Fatal("missing review gate")
		}
		return ReviewIgnoreEvidence, nil
	}})
	if e != nil || r.IgnoredGroupCount != 1 {
		t.Fatal(e, r)
	}
	text := readFoldFile(t, filepath.Join(root, "platforms", platform, "German Game", "game.yaml"))
	if !strings.Contains(text, "redump.xbox360_serial=AV-2047") || strings.Contains(text, "launchbox.database_id=10") {
		t.Fatal(text)
	}
	next, e := Fold(Options{DataRoot: root})
	if e != nil || next.CandidateGroupCount != 1 || next.ReviewRequiredCount != 0 || len(next.Groups[0].Merge) != 1 {
		t.Fatal(e, next)
	}
}

func TestSerialReviewCannotOverrideFileOrAliasHolds(t *testing.T) {
	for _, tt := range []struct {
		name, extra, source string
		want                string
	}{
		{name: "unowned canonical data", extra: "custom_notes: retain\n", want: "unsupported canonical field"},
		{name: "unconfirmed alias", source: "  launchbox:\n    matches:\n      - database_id: '10'\n        title: English Game\n        alternate_titles: [Localized Game]\n", want: "no matching explicit release region"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeSerialGame(t, root, "Xbox 360", "English Game", "USA", "EA-2276", "", "", tt.source)
			writeSerialGame(t, root, "Xbox 360", "Localized Game", "Japan", "EA-2276", "", tt.extra, "")
			r, e := Fold(Options{DataRoot: root, Review: func(ReviewPrompt) (ReviewDecision, error) { return ReviewApply, nil }})
			if e != nil || r.AppliedGroupCount != 0 || r.HeldGroupCount != 1 || !strings.Contains(r.Groups[0].HoldReason, tt.want) {
				t.Fatalf("%v %+v", e, r)
			}
		})
	}
}
