package catalogfold

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFoldDryRunFindsSharedLaunchBoxIdentity(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega CD & Sega CD"
	writeFoldGame(t, root, platform, "Dragon's Lair (Japan)", "Dragon's Lair (Japan)", "Dragon's Lair", "100")
	writeFoldGame(t, root, platform, "Dragon's Lair (USA)", "Dragon's Lair (USA)", "Dragon's Lair", "100")

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 1 {
		t.Fatalf("CandidateGroupCount = %d, want 1", result.CandidateGroupCount)
	}
	group := result.Groups[0]
	if group.Keep.Folder != "Dragon's Lair (USA)" {
		t.Fatalf("Keep.Folder = %q, want USA release to win", group.Keep.Folder)
	}
	if len(group.Merge) != 1 || group.Merge[0].Folder != "Dragon's Lair (Japan)" {
		t.Fatalf("unexpected merge refs: %#v", group.Merge)
	}
	if len(group.Evidence) != 1 || group.Evidence[0].Source != "launchbox" || group.Evidence[0].Value != "100" {
		t.Fatalf("unexpected evidence: %#v", group.Evidence)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", platform, "Dragon's Lair (Japan)")); err != nil {
		t.Fatalf("dry run should not remove merge folder: %v", err)
	}
}

func TestFoldMatchFiltersCandidateGroups(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	writeFoldGame(t, root, platform, "Adventures of Lolo", "Adventures of Lolo (USA)", "Adventures of Lolo", "100")
	writeFoldGame(t, root, platform, "Adventures of Lolo (Japan)", "Adventures of Lolo (Japan) (En)", "Adventures of Lolo", "100")
	writeFoldGame(t, root, platform, "Aladdin", "Aladdin (USA)", "Aladdin", "200")
	writeFoldGame(t, root, platform, "Aladdin (World) (Unl)", "Aladdin (World) (Unl)", "Aladdin", "200")

	result, err := Fold(Options{DataRoot: root, Platform: platform, Match: "Lolo"})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.Match != "Lolo" {
		t.Fatalf("Match = %q, want Lolo", result.Match)
	}
	if result.CandidateGroupCount != 1 || len(result.Groups) != 1 {
		t.Fatalf("filtered groups = %d/%d, want 1/1: %#v", result.CandidateGroupCount, len(result.Groups), result.Groups)
	}
	if result.Groups[0].Keep.Folder != "Adventures of Lolo" {
		t.Fatalf("unexpected kept group: %#v", result.Groups[0])
	}
}

func TestFoldDoesNotMergeAcrossPlatforms(t *testing.T) {
	root := t.TempDir()
	nes := "Nintendo - Nintendo Entertainment System"
	snes := "Nintendo - Super Nintendo Entertainment System"
	writeFoldGame(t, root, nes, "Game One (USA)", "Game One (USA)", "Game One", "100")
	writeFoldGame(t, root, snes, "Game One (USA)", "Game One (USA)", "Game One", "100")

	result, err := Fold(Options{DataRoot: root})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.PlatformCount != 2 || result.GameCount != 2 {
		t.Fatalf("platform/game counts = %d/%d, want 2/2", result.PlatformCount, result.GameCount)
	}
	if result.CandidateGroupCount != 0 {
		t.Fatalf("cross-platform shared identity should not fold, got %#v", result.Groups)
	}
	if len(result.PlatformResults) != 2 {
		t.Fatalf("PlatformResults length = %d, want 2", len(result.PlatformResults))
	}
	for _, platform := range result.PlatformResults {
		if platform.CandidateGroupCount != 0 {
			t.Fatalf("platform %s has %d candidate groups, want 0", platform.Platform, platform.CandidateGroupCount)
		}
	}
}

func TestFoldReportsPlatformStartAndResult(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega CD & Sega CD"
	writeFoldGame(t, root, platform, "Dragon's Lair (Japan)", "Dragon's Lair (Japan)", "Dragon's Lair", "100")
	writeFoldGame(t, root, platform, "Dragon's Lair (USA)", "Dragon's Lair (USA)", "Dragon's Lair", "100")

	var starts []string
	var results []PlatformResult
	_, err := Fold(Options{
		DataRoot: root,
		OnPlatformStart: func(index, total int, platform string) {
			starts = append(starts, strconv.Itoa(index)+"/"+strconv.Itoa(total)+" "+platform)
		},
		OnPlatformResult: func(result PlatformResult) {
			results = append(results, result)
		},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if len(starts) != 1 || starts[0] != "1/1 "+platform {
		t.Fatalf("unexpected platform starts: %#v", starts)
	}
	if len(results) != 1 {
		t.Fatalf("result callback count = %d, want 1", len(results))
	}
	if results[0].Platform != platform || results[0].CandidateGroupCount != 1 || results[0].MergedGameCount != 1 {
		t.Fatalf("unexpected platform result: %#v", results[0])
	}
}

func TestFoldRegionPriorityCanPreferJapan(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Castlevania III - Dracula's Curse (US)"
	japan := "Akumajou Densetsu (JA)"
	writeFoldGame(t, root, platform, usa, usa, "Castlevania III", "42")
	writeFoldGame(t, root, platform, japan, japan, "Akumajou Densetsu", "42")
	addRegionalTestAlias(t, root, platform, usa, "Castlevania III", "Akumajou Densetsu", "Japan")

	result, err := Fold(Options{
		DataRoot:       root,
		Platform:       platform,
		RegionPriority: []string{"japan", "usa"},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 1 {
		t.Fatalf("CandidateGroupCount = %d, want 1", result.CandidateGroupCount)
	}
	if got := result.Groups[0].Keep.Folder; got != japan {
		t.Fatalf("Keep.Folder = %q, want Japanese alias release to win", got)
	}
}

func TestFoldRegionPriorityRecognizesAsiaAndRussiaAliases(t *testing.T) {
	tests := []struct {
		name     string
		priority []string
		want     string
	}{
		{name: "asia first", priority: []string{"asia", "russia"}, want: "Regional Game (ZH)"},
		{name: "russia first", priority: []string{"russia", "asia"}, want: "Regional Game (RU)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			platform := "Nintendo - Nintendo Entertainment System"
			china := "Regional Game (ZH)"
			russia := "Regional Game (RU)"
			writeFoldGame(t, root, platform, china, china, "Regional Game", "55")
			writeFoldGame(t, root, platform, russia, russia, "Regional Game", "55")

			result, err := Fold(Options{
				DataRoot:       root,
				Platform:       platform,
				RegionPriority: test.priority,
			})
			if err != nil {
				t.Fatalf("Fold returned error: %v", err)
			}
			if got := result.Groups[0].Keep.Folder; got != test.want {
				t.Fatalf("Keep.Folder = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFoldPlatformRegionPriorityOverridesGlobal(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega CD & Sega CD"
	usa := "Dragon's Lair (USA)"
	japan := "Dragon's Lair (Japan)"
	writeFoldGame(t, root, platform, usa, usa, "Dragon's Lair", "100")
	writeFoldGame(t, root, platform, japan, japan, "Dragon's Lair", "100")

	result, err := Fold(Options{
		DataRoot:       root,
		Platform:       platform,
		RegionPriority: []string{"usa", "japan"},
		PlatformRegionPriority: map[string][]string{
			platform: []string{"japan", "usa"},
		},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if got := result.Groups[0].Keep.Folder; got != japan {
		t.Fatalf("Keep.Folder = %q, want platform override to prefer Japan", got)
	}
}

func TestFoldApplyMergesGameYAMLFingerprintsAndMedia(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Castlevania III - Dracula's Curse (USA)"
	japan := "Akumajou Densetsu (Japan)"
	writeFoldGame(t, root, platform, usa, usa, "Castlevania III", "42")
	writeFoldGame(t, root, platform, japan, japan, "Akumajou Densetsu", "42")
	addRegionalTestAlias(t, root, platform, usa, "Castlevania III", "Akumajou Densetsu", "Japan")
	writeFoldFile(t, filepath.Join(root, "platforms", platform, usa, "media", "box-front.usa.launchbox.jpg"), "usa")
	writeFoldFile(t, filepath.Join(root, "platforms", platform, japan, "media", "box-front.japan.launchbox.jpg"), "japan")

	result, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.AppliedGroupCount != 1 || result.SkippedGroupCount != 0 {
		t.Fatalf("applied/skipped = %d/%d, want 1/0", result.AppliedGroupCount, result.SkippedGroupCount)
	}
	if result.MovedFileCount != 1 {
		t.Fatalf("MovedFileCount = %d, want 1", result.MovedFileCount)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", platform, japan)); !os.IsNotExist(err) {
		t.Fatalf("merge folder should be removed, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", platform, usa, "media", "box-front.japan.launchbox.jpg")); err != nil {
		t.Fatalf("merged media was not moved: %v", err)
	}

	gameYAML := readFoldFile(t, filepath.Join(root, "platforms", platform, usa, "game.yaml"))
	for _, expected := range []string{
		`"Akumajou Densetsu (Japan)":`,
		`"Castlevania III - Dracula's Curse (USA)":`,
		`database_id: "42"`,
		`title: "Akumajou Densetsu"`,
		`title: "Castlevania III"`,
	} {
		if !strings.Contains(gameYAML, expected) {
			t.Fatalf("merged game.yaml missing %q:\n%s", expected, gameYAML)
		}
	}

	fingerprints := readFoldFile(t, filepath.Join(root, "platforms", platform, usa, "fingerprints.yaml"))
	for _, expected := range []string{`id: "redump.Castlevania III - Dracula's Curse (USA)"`, `id: "redump.Akumajou Densetsu (Japan)"`} {
		if !strings.Contains(fingerprints, expected) {
			t.Fatalf("merged fingerprints missing %q:\n%s", expected, fingerprints)
		}
	}
}

func TestFoldApplyUsesRegionPriorityForMergedDefaultRelease(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Castlevania III - Dracula's Curse (USA)"
	japan := "Akumajou Densetsu (Japan)"
	writeFoldGame(t, root, platform, usa, usa, "Castlevania III", "42")
	writeFoldGame(t, root, platform, japan, japan, "Akumajou Densetsu", "42")
	addRegionalTestAlias(t, root, platform, usa, "Castlevania III", "Akumajou Densetsu", "Japan")

	_, err := Fold(Options{
		DataRoot:       root,
		Platform:       platform,
		Apply:          true,
		RegionPriority: []string{"japan", "usa"},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	gameYAML := readFoldFile(t, filepath.Join(root, "platforms", platform, japan, "game.yaml"))
	if !strings.Contains(gameYAML, `default_release: "Akumajou Densetsu (Japan)"`) {
		t.Fatalf("merged game.yaml did not choose Japanese default release:\n%s", gameYAML)
	}
}

func TestPlatformTransactionKeepsCatalogYAMLIndependentUntilCommit(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	gamePath := filepath.Join(root, "platforms", platform, "Sonic")
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), "title: Sonic\n")
	writeFoldFile(t, filepath.Join(gamePath, "media", "box-front.test.jpg"), "media")

	transaction, err := beginPlatformTransaction(root, platform)
	if err != nil {
		t.Fatalf("beginPlatformTransaction returned error: %v", err)
	}
	defer transaction.abort()
	stagedGame := filepath.Join(transaction.stagePath, "Sonic", "game.yaml")
	if err := os.WriteFile(stagedGame, []byte("title: Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFoldFile(t, filepath.Join(gamePath, "game.yaml")); got != "title: Sonic\n" {
		t.Fatalf("staging mutated original game.yaml: %q", got)
	}
	transaction.abort()
	if got := readFoldFile(t, filepath.Join(gamePath, "game.yaml")); got != "title: Sonic\n" {
		t.Fatalf("abort mutated original game.yaml: %q", got)
	}
}

func TestPlatformTransactionCommitSwapsCompletePlatform(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	gamePath := filepath.Join(root, "platforms", platform, "Sonic")
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), "title: Sonic\n")

	transaction, err := beginPlatformTransaction(root, platform)
	if err != nil {
		t.Fatalf("beginPlatformTransaction returned error: %v", err)
	}
	defer transaction.abort()
	if err := os.WriteFile(filepath.Join(transaction.stagePath, "Sonic", "game.yaml"), []byte("title: Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := transaction.commit(); err != nil {
		t.Fatalf("commit returned error: %v", err)
	}
	if got := readFoldFile(t, filepath.Join(gamePath, "game.yaml")); got != "title: Changed\n" {
		t.Fatalf("commit did not activate staged platform: %q", got)
	}
	if filepath.Dir(transaction.backupPath) != filepath.Join(root, "backups") ||
		!strings.HasPrefix(filepath.Base(transaction.backupPath), platform+".fold-backup-") {
		t.Fatalf("backup is not visible under data-root/backups: %q", transaction.backupPath)
	}
	if got := readFoldFile(t, filepath.Join(transaction.backupPath, "Sonic", "game.yaml")); got != "title: Sonic\n" {
		t.Fatalf("backup did not retain original metadata: %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, "platforms"))
	if err != nil || len(entries) != 1 || entries[0].Name() != platform {
		t.Fatalf("backup leaked into catalog platforms: %v, %v", entries, err)
	}

	second, err := beginPlatformTransaction(root, platform)
	if err != nil {
		t.Fatal(err)
	}
	defer second.abort()
	if err := second.commit(); err != nil {
		t.Fatal(err)
	}
	if second.backupPath == transaction.backupPath {
		t.Fatal("successive fold reused the previous backup")
	}
	if got := readFoldFile(t, filepath.Join(transaction.backupPath, "Sonic", "game.yaml")); got != "title: Sonic\n" {
		t.Fatalf("second fold changed the first backup: %q", got)
	}
}

func TestPlatformTransactionBackupFailuresPreserveOriginal(t *testing.T) {
	for _, failure := range []string{"backup root is a file", "activation fails"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			platform := "Console"
			original := filepath.Join(root, "platforms", platform, "Game", "game.yaml")
			writeFoldFile(t, original, "original")
			transaction, err := beginPlatformTransaction(root, platform)
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.abort()
			if failure == "backup root is a file" {
				writeFoldFile(t, filepath.Join(root, "backups"), "existing file")
			} else if err := os.Rename(transaction.stagePath, transaction.stagePath+"-unavailable"); err != nil {
				t.Fatal(err)
			}
			if err := transaction.commit(); err == nil {
				t.Fatal("expected commit failure")
			}
			if got := readFoldFile(t, original); got != "original" {
				t.Fatalf("commit failure lost the original: %q", got)
			}
			if failure == "backup root is a file" {
				if got := readFoldFile(t, filepath.Join(root, "backups")); got != "existing file" {
					t.Fatalf("overwrote backup destination: %q", got)
				}
			}
		})
	}
}

func TestFoldApplyPreservesMediaPathConflicts(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Worms Armageddon (USA)"
	europe := "Worms Armageddon (Europe)"
	writeFoldGame(t, root, platform, usa, usa, "Worms Armageddon", "77")
	writeFoldGame(t, root, platform, europe, europe, "Worms Armageddon", "77")
	writeFoldFile(t, filepath.Join(root, "platforms", platform, usa, "media", "box-front.launchbox.jpg"), "usa")
	writeFoldFile(t, filepath.Join(root, "platforms", platform, europe, "media", "box-front.launchbox.jpg"), "europe")

	result, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.AppliedGroupCount != 1 || result.SkippedGroupCount != 0 || len(result.Backups) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	for path, want := range map[string]string{
		filepath.Join(root, "platforms", platform, usa, "media", "box-front.launchbox.jpg"):                  "usa",
		filepath.Join(root, "platforms", platform, usa, "media", "_fold", europe, "box-front.launchbox.jpg"): "europe",
		filepath.Join(result.Backups[0], europe, "media", "box-front.launchbox.jpg"):                         "europe",
	} {
		if got := readFoldFile(t, path); got != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestFoldIgnoresLowScoreSourceIdentities(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega CD & Sega CD"
	writeFoldScoredGame(t, root, platform, "Ecco - The Tides of Time", "Ecco - The Tides of Time (USA)", "Ecco - The Tides of Time", "5379", 146)
	writeFoldScoredGame(t, root, platform, "Ecco the Dolphin CD", "Ecco the Dolphin CD (Japan)", "Ecco the Dolphin CD", "5379", 131)

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 0 {
		t.Fatalf("low-score shared identity should not fold, got %#v", result.Groups)
	}
}

func TestFoldIgnoresScreenScraperNonGameIdentities(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	writeFoldScreenScraperGame(t, root, platform, "Game Genie", "Game Genie (USA, Europe) (Program)", "Game Genie", "124835", "ZZZ(notgame):#NONGAME", []string{"ZZZ(notgame):#NONGAME"})
	writeFoldScreenScraperGame(t, root, platform, "Sega Art Tool", "Sega Art Tool (Japan) (En) (Program)", "Sega Art Tool", "124835", "ZZZ(notgame):#NONGAME", []string{"ZZZ(notgame):#NONGAME"})

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 0 {
		t.Fatalf("non-game ScreenScraper identity should not fold utility ROMs, got %#v", result.Groups)
	}
}

func TestFoldIgnoresScreenScraperIdentityWhenTitleDoesNotMatchCatalogGame(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	writeFoldScreenScraperGame(t, root, platform, "FIFA Soccer 95", "FIFA 2003 (Argentina) (Pirate)", "FIFA Soccer 95", "124", "FIFA 98 : Road to World Cup", []string{"FIFA 98 : Road to World Cup", "Fifa Road To World Cup 98"})
	writeFoldScreenScraperGame(t, root, platform, "FIFA 98 - Road to World Cup", "FIFA 98 - Road to World Cup (Europe)", "FIFA 98 - Road to World Cup", "124", "FIFA 98 : Road to World Cup", []string{"FIFA 98 : Road to World Cup", "FIFA 98 - Road to World Cup"})

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 0 {
		t.Fatalf("foreign ScreenScraper title should not fold unrelated bundles, got %#v", result.Groups)
	}
}

func TestFoldAllowsScreenScraperIdentityWhenTitleVariantsMatchCatalogGames(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	writeFoldScreenScraperGame(t, root, platform, "Sports Talk Baseball", "Sports Talk Baseball (USA)", "Sports Talk Baseball", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})
	writeFoldScreenScraperGame(t, root, platform, "Nikkan Sports Pro Yakyuu VAN", "Nikkan Sports Pro Yakyuu VAN (Japan)", "Nikkan Sports Pro Yakyuu VAN", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 1 {
		t.Fatalf("regional ScreenScraper title variants should still fold, got %#v", result.Groups)
	}
}

func TestFoldIgnoreEvidenceSkipsSourceIdentity(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	writeFoldScreenScraperGame(t, root, platform, "Sports Talk Baseball", "Sports Talk Baseball (USA)", "Sports Talk Baseball", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})
	writeFoldScreenScraperGame(t, root, platform, "Nikkan Sports Pro Yakyuu VAN", "Nikkan Sports Pro Yakyuu VAN (Japan)", "Nikkan Sports Pro Yakyuu VAN", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})
	gameYAMLPath := filepath.Join(root, "platforms", platform, "Nikkan Sports Pro Yakyuu VAN", "game.yaml")
	gameYAML := readFoldFile(t, gameYAMLPath)
	gameYAML = strings.Replace(gameYAML, "\nsources:", "\nfold:\n  ignore_evidence:\n    - \"screenscraper.screenscraper_id=752\"\n\nsources:", 1)
	writeFoldFile(t, gameYAMLPath, gameYAML)

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 0 {
		t.Fatalf("ignored evidence should not fold unrelated bundles, got %#v", result.Groups)
	}
}

func TestFoldApplyPreservesIgnoredEvidence(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Castlevania III - Dracula's Curse (USA)"
	japan := "Akumajou Densetsu (Japan)"
	writeFoldGame(t, root, platform, usa, usa, "Castlevania III", "42")
	writeFoldGame(t, root, platform, japan, japan, "Akumajou Densetsu", "42")
	addRegionalTestAlias(t, root, platform, usa, "Castlevania III", "Akumajou Densetsu", "Japan")
	gameYAMLPath := filepath.Join(root, "platforms", platform, japan, "game.yaml")
	gameYAML := readFoldFile(t, gameYAMLPath)
	gameYAML = strings.Replace(gameYAML, "\nsources:", "\nfold:\n  ignore_evidence: [\"screenscraper.screenscraper_id=752\"]\n\nsources:", 1)
	writeFoldFile(t, gameYAMLPath, gameYAML)

	_, err := Fold(Options{DataRoot: root, Platform: platform, Apply: true})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	merged := readFoldFile(t, filepath.Join(root, "platforms", platform, usa, "game.yaml"))
	if !strings.Contains(merged, `fold:`) || !strings.Contains(merged, `- "screenscraper.screenscraper_id=752"`) {
		t.Fatalf("merged game.yaml did not preserve ignored evidence:\n%s", merged)
	}
}

func TestFoldInteractiveNoWritesIgnoredEvidence(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	writeFoldScreenScraperGame(t, root, platform, "Sports Talk Baseball", "Sports Talk Baseball (USA)", "Sports Talk Baseball", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})
	writeFoldScreenScraperGame(t, root, platform, "Nikkan Sports Pro Yakyuu VAN", "Nikkan Sports Pro Yakyuu VAN (Japan)", "Nikkan Sports Pro Yakyuu VAN", "752", "Sports Talk Baseball", []string{"Sports Talk Baseball", "Nikkan Sports Pro Yakyuu Van"})

	result, err := Fold(Options{
		DataRoot: root,
		Platform: platform,
		Review: func(prompt ReviewPrompt) (ReviewDecision, error) {
			if prompt.Index != 1 || prompt.Total != 1 {
				t.Fatalf("prompt index/total = %d/%d, want 1/1", prompt.Index, prompt.Total)
			}
			return ReviewIgnoreEvidence, nil
		},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.IgnoredGroupCount != 1 || result.IgnoredFileCount != 1 {
		t.Fatalf("ignored groups/files = %d/%d, want 1/1", result.IgnoredGroupCount, result.IgnoredFileCount)
	}
	gameYAML := readFoldFile(t, filepath.Join(root, "platforms", platform, "Nikkan Sports Pro Yakyuu VAN", "game.yaml"))
	if !strings.Contains(gameYAML, `fold:`) || !strings.Contains(gameYAML, `- "screenscraper.screenscraper_id=752"`) {
		t.Fatalf("interactive no did not write ignored evidence:\n%s", gameYAML)
	}

	result, err = Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error after ignore: %v", err)
	}
	if result.CandidateGroupCount != 0 {
		t.Fatalf("ignored evidence should remove future candidate, got %#v", result.Groups)
	}
}

func TestFoldInteractiveYesAppliesGroup(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	usa := "Castlevania III - Dracula's Curse (USA)"
	japan := "Akumajou Densetsu (Japan)"
	writeFoldGame(t, root, platform, usa, usa, "Castlevania III", "42")
	writeFoldGame(t, root, platform, japan, japan, "Akumajou Densetsu", "42")
	addRegionalTestAlias(t, root, platform, usa, "Castlevania III", "Akumajou Densetsu", "Japan")

	result, err := Fold(Options{
		DataRoot: root,
		Platform: platform,
		Review: func(prompt ReviewPrompt) (ReviewDecision, error) {
			return ReviewApply, nil
		},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.AppliedGroupCount != 1 {
		t.Fatalf("AppliedGroupCount = %d, want 1", result.AppliedGroupCount)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", platform, japan)); !os.IsNotExist(err) {
		t.Fatalf("interactive yes should remove merge folder, stat error: %v", err)
	}
}

func TestFoldInteractiveQuitStopsReview(t *testing.T) {
	root := t.TempDir()
	platform := "Nintendo - Nintendo Entertainment System"
	writeFoldGame(t, root, platform, "Game One (USA)", "Game One (USA)", "Game One", "100")
	writeFoldGame(t, root, platform, "Game One (Japan)", "Game One (Japan)", "Game One", "100")
	writeFoldGame(t, root, platform, "Game Two (USA)", "Game Two (USA)", "Game Two", "200")
	writeFoldGame(t, root, platform, "Game Two (Japan)", "Game Two (Japan)", "Game Two", "200")

	reviews := 0
	result, err := Fold(Options{
		DataRoot: root,
		Platform: platform,
		Review: func(prompt ReviewPrompt) (ReviewDecision, error) {
			reviews++
			return ReviewQuit, nil
		},
	})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if reviews != 1 || !result.ReviewStopped {
		t.Fatalf("reviews/stopped = %d/%v, want 1/true", reviews, result.ReviewStopped)
	}
}

func TestSourceEvidenceIncludesDirectURLsWhenKnown(t *testing.T) {
	igdbEvidence := sourceEvidenceFromMatch("igdb", strings.Split(`      - release: "Doom (USA)"
        igdb_id: 1
        slug: doom
        match_score: 200`, "\n"), "Doom")
	if len(igdbEvidence) != 1 || igdbEvidence[0].URL != "https://www.igdb.com/games/doom" {
		t.Fatalf("unexpected IGDB evidence URL: %#v", igdbEvidence)
	}

	screenScraperEvidence := sourceEvidenceFromMatch("screenscraper", strings.Split(`      - release: "Sports Talk Baseball (USA)"
        screenscraper_id: "752"
        title: "Sports Talk Baseball"
        match_score: 200`, "\n"), "Sports Talk Baseball")
	if len(screenScraperEvidence) != 1 || screenScraperEvidence[0].URL != "https://www.screenscraper.fr/gameinfos.php?gameid=752" {
		t.Fatalf("unexpected ScreenScraper evidence URL: %#v", screenScraperEvidence)
	}
}

func TestFoldIgnoresBIOSGameFolders(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Master System - Mark III"
	writeFoldGame(t, root, platform, "[BIOS] Alex Kidd in Miracle World", "[BIOS] Alex Kidd in Miracle World", "Alex Kidd in Miracle World", "2684")
	writeFoldGame(t, root, platform, "Alex Kidd in Miracle World", "Alex Kidd in Miracle World (USA)", "Alex Kidd in Miracle World", "2684")

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.GameCount != 1 {
		t.Fatalf("GameCount = %d, want BIOS folder to be ignored", result.GameCount)
	}
	if result.CandidateGroupCount != 0 {
		t.Fatalf("BIOS folder should not create a fold candidate: %#v", result.Groups)
	}
}

func TestFoldMergesTrailingPlusVariantsBySourceIdentity(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Master System - Mark III"
	writeFoldGame(t, root, platform, "Impetus", "Impetus", "Impetus", "327977")
	writeFoldGame(t, root, platform, "Impetus+", "Impetus+", "Impetus+", "327977")

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 1 {
		t.Fatalf("CandidateGroupCount = %d, want 1", result.CandidateGroupCount)
	}
	if got := result.Groups[0].Keep.Folder; got != "Impetus" {
		t.Fatalf("Keep.Folder = %q, want base folder to win over plus variant", got)
	}
	if len(result.Groups[0].Merge) != 1 || result.Groups[0].Merge[0].Folder != "Impetus+" {
		t.Fatalf("unexpected merge refs: %#v", result.Groups[0].Merge)
	}
}

func TestFoldDoesNotAutoMergeNumberedCollisionFolders(t *testing.T) {
	root := t.TempDir()
	platform := "Sega - Master System - Mark III"
	writeFoldGame(t, root, platform, "Great Golf", "Great Golf", "Great Golf", "2810")
	writeFoldMultiSourceGame(t, root, platform, "Great Golf [2]", "Great Golf [2]", "Great Golf", "2810", "46143", "172292")

	result, err := Fold(Options{DataRoot: root, Platform: platform})
	if err != nil {
		t.Fatalf("Fold returned error: %v", err)
	}

	if result.CandidateGroupCount != 0 {
		t.Fatalf("numbered collision folder should require an explicit fold decision, got %#v", result.Groups)
	}
}

func writeFoldGame(t *testing.T, root, platform, folder, release, title, databaseID string) {
	t.Helper()
	gamePath := filepath.Join(root, "platforms", platform, folder)
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), `title: "`+title+`"
default_release: "`+release+`"

releases:
  "`+release+`":
    source: "redump"
    title: "`+title+`"

sources:
  launchbox:
    matches:
      - release: "`+release+`"
        database_id: "`+databaseID+`"
        title: "`+title+`"
`)
	writeFoldFile(t, filepath.Join(gamePath, "fingerprints.yaml"), `items:
  - id: "redump.`+release+`"
    release: "`+release+`"
    md5: "00000000000000000000000000000000"
`)
}

func writeFoldScoredGame(t *testing.T, root, platform, folder, release, title, igdbID string, score int) {
	t.Helper()
	gamePath := filepath.Join(root, "platforms", platform, folder)
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), `title: "`+title+`"
default_release: "`+release+`"

releases:
  "`+release+`":
    source: "redump"
    title: "`+title+`"

sources:
  igdb:
    matches:
      - release: "`+release+`"
        igdb_id: `+igdbID+`
        title: "`+title+`"
        match_score: `+strconv.Itoa(score)+`
`)
	writeFoldFile(t, filepath.Join(gamePath, "fingerprints.yaml"), `items:
  - id: "redump.`+release+`"
    release: "`+release+`"
    md5: "00000000000000000000000000000000"
`)
}

func writeFoldMultiSourceGame(t *testing.T, root, platform, folder, release, title, launchBoxID, igdbID, screenScraperID string) {
	t.Helper()
	gamePath := filepath.Join(root, "platforms", platform, folder)
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), `title: "`+title+`"
default_release: "`+release+`"

releases:
  "`+release+`":
    source: "redump"
    title: "`+title+`"

sources:
  launchbox:
    matches:
      - release: "`+release+`"
        database_id: "`+launchBoxID+`"
        title: "`+title+`"
  igdb:
    matches:
      - release: "`+release+`"
        igdb_id: `+igdbID+`
        title: "`+title+`"
        match_score: 180
  screenscraper:
    matches:
      - release: "`+release+`"
        screenscraper_id: `+screenScraperID+`
        title: "`+title+`"
        match_score: 180
`)
	writeFoldFile(t, filepath.Join(gamePath, "fingerprints.yaml"), `items:
  - id: "redump.`+release+`"
    release: "`+release+`"
    md5: "00000000000000000000000000000000"
`)
}

func writeFoldScreenScraperGame(t *testing.T, root, platform, folder, release, title, screenScraperID, sourceTitle string, titleVariants []string) {
	t.Helper()
	gamePath := filepath.Join(root, "platforms", platform, folder)
	var titleLines strings.Builder
	for _, variant := range titleVariants {
		titleLines.WriteString(`          - title: "` + variant + `"` + "\n")
	}
	writeFoldFile(t, filepath.Join(gamePath, "game.yaml"), `title: "`+title+`"
default_release: "`+release+`"

releases:
  "`+release+`":
    source: "no-intro"
    title: "`+title+`"

sources:
  screenscraper:
    matches:
      - release: "`+release+`"
        screenscraper_id: "`+screenScraperID+`"
        title: "`+sourceTitle+`"
        titles:
`+titleLines.String()+`        match_score: 200
`)
	writeFoldFile(t, filepath.Join(gamePath, "fingerprints.yaml"), `items:
  - id: "no-intro.`+release+`"
    release: "`+release+`"
    md5: "00000000000000000000000000000000"
`)
}

func writeFoldFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func readFoldFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	return string(contents)
}

func addRegionalTestAlias(t *testing.T, root, platform, folder, title, alias, region string) {
	t.Helper()
	path := filepath.Join(root, "platforms", platform, folder, "game.yaml")
	original := readFoldFile(t, path)
	anchor := "        title: " + fmt.Sprintf("%q", title) + "\n"
	replacement := anchor + fmt.Sprintf("        titles: [{title: %q, region: %q}]\n", alias, region)
	if !strings.Contains(original, anchor) {
		t.Fatal("test provider anchor missing")
	}
	writeFoldFile(t, path, strings.Replace(original, anchor, replacement, 1))
}
