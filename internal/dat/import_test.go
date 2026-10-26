package dat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
)

func TestImportDryRunDoesNotWriteCatalog(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if result.Apply {
		t.Fatalf("expected dry-run result")
	}
	if result.GameBundleCount != 1 {
		t.Fatalf("expected one clone-root bundle, got %d", result.GameBundleCount)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "platforms")); !os.IsNotExist(err) {
		t.Fatalf("dry run should not write platforms, stat err=%v", err)
	}
}

func TestImportReadsSelectedDATZipMember(t *testing.T) {
	datPath := writeTempDATZip(t, "multi-system.zip", map[string]string{
		"arcade.dat": sampleMAMEClrmameXML,
		"ps.dat":     sampleRedumpDAT,
	})
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot, MemberPath: "ps.dat"})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if result.Plan.MemberPath != "ps.dat" {
		t.Fatalf("unexpected member path: %q", result.Plan.MemberPath)
	}
	if result.Plan.Authority != "redump" {
		t.Fatalf("unexpected authority: %q", result.Plan.Authority)
	}
	if result.GameBundleCount != 1 || result.ReleaseCount != 2 || result.FingerprintCount != 3 {
		t.Fatalf("unexpected counts: bundles=%d releases=%d fingerprints=%d", result.GameBundleCount, result.ReleaseCount, result.FingerprintCount)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "platforms")); !os.IsNotExist(err) {
		t.Fatalf("dry run should not write platforms, stat err=%v", err)
	}
}

func TestImportArcadeDryRunFiltersSpecialMachines(t *testing.T) {
	datPath := writeTempDAT(t, "mame.xml", sampleMAMEListXML)
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if result.Plan.Authority != "mame" {
		t.Fatalf("unexpected authority: %q", result.Plan.Authority)
	}
	if result.Plan.MachineCount != 4 || result.Plan.BIOSCount != 1 || result.Plan.DeviceCount != 1 {
		t.Fatalf("unexpected machine counts: machines=%d bios=%d devices=%d", result.Plan.MachineCount, result.Plan.BIOSCount, result.Plan.DeviceCount)
	}
	if result.ReleaseCount != 2 {
		t.Fatalf("expected only playable parent/clone release candidates, got %d", result.ReleaseCount)
	}
	if result.GameBundleCount != 1 {
		t.Fatalf("expected one parent/clone bundle, got %d", result.GameBundleCount)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "platforms")); !os.IsNotExist(err) {
		t.Fatalf("dry run should not write platforms, stat err=%v", err)
	}
}

func TestImportArcadeApplyIsPlanOnly(t *testing.T) {
	datPath := writeTempDAT(t, "mame.xml", sampleMAMEListXML)
	dataRoot := t.TempDir()

	_, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true})
	if err == nil {
		t.Fatalf("expected arcade apply to be blocked")
	}
	if !strings.Contains(err.Error(), "plan-only") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "platforms")); !os.IsNotExist(err) {
		t.Fatalf("blocked apply should not write platforms, stat err=%v", err)
	}
}

func TestImportApplyWritesValidPlatformShape(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if !result.Apply {
		t.Fatalf("expected applied result")
	}
	if result.WrittenFileCount == 0 {
		t.Fatalf("expected written files")
	}
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"))
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"))
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "fingerprints.yaml"))
	mustNotExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media"))

	issues, err := catalog.Validate(dataRoot)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("expected generated platform data to validate, got %#v", issues)
	}
}

func TestImportMergePreservesEnrichedGameFields(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()
	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml")
	existing := readText(t, gamePath)
	existing += `
aliases: ["Starwing"]

fold:
  ignore_evidence:
    - "igdb.igdb_id=42"

sources:
  igdb:
    matches:
      - igdb_id: 42
`
	if err := os.WriteFile(gamePath, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("merge Import returned error: %v", err)
	}
	merged := readText(t, gamePath)
	for _, expected := range []string{`aliases: ["Starwing"]`, `fold:`, `igdb.igdb_id=42`, `sources:`, `igdb_id: 42`} {
		if !strings.Contains(merged, expected) {
			t.Fatalf("merged game.yaml missing %q:\n%s", expected, merged)
		}
	}
}

func TestImportMergeStagesCatalogAndPreservesMedia(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()
	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64")
	mediaPath := filepath.Join(gamePath, "media", "box-front.launchbox.jpg")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(mediaPath, []byte("image"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	existing := readText(t, filepath.Join(gamePath, "game.yaml")) + "\nsources:\n  launchbox:\n    matches:\n      - database_id: 1\n"
	if err := os.WriteFile(filepath.Join(gamePath, "game.yaml"), []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("merge Import returned error: %v", err)
	}
	if contents := readText(t, mediaPath); contents != "image" {
		t.Fatalf("media contents = %q", contents)
	}
	merged := readText(t, filepath.Join(gamePath, "game.yaml"))
	if !strings.Contains(merged, "database_id: 1") {
		t.Fatalf("merge import lost source metadata:\n%s", merged)
	}
	entries, err := os.ReadDir(filepath.Join(dataRoot, "platforms"))
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".import-") || strings.Contains(entry.Name(), ".backup-") {
			t.Fatalf("merge import left staging entry %q", entry.Name())
		}
	}
}

func TestImportMergeKeepsUnmentionedEnrichedBundle(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()
	platformPath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64")
	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}
	legacyPath := filepath.Join(platformPath, "Curated Legacy", "game.yaml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	legacy := "title: Curated Legacy\n\nreleases:\n  Curated Legacy:\n\nsources:\n  manual:\n    matches:\n      - title: Curated Legacy\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("merge Import returned error: %v", err)
	}
	if contents := readText(t, legacyPath); contents != legacy {
		t.Fatalf("merge changed unmentioned enriched game:\n%s", contents)
	}
}

func TestImportMergeKeepsUnmentionedDATOnlyBundle(t *testing.T) {
	initialDAT := writeTempDAT(t, "Nintendo - Nintendo 64.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Nintendo - Nintendo 64</name><author>No-Intro</author></header>
  <game name="Game One (USA)" id="one">
    <rom name="Game One (USA).z64" size="1" crc="00000001"/>
  </game>
  <game name="Game Two (USA)" id="two">
    <rom name="Game Two (USA).z64" size="1" crc="00000002"/>
  </game>
</datafile>`)
	updatedDAT := writeTempDAT(t, "Nintendo - Nintendo 64 updated.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Nintendo - Nintendo 64</name><author>No-Intro</author></header>
  <game name="Game One (USA)" id="one">
    <rom name="Game One (USA).z64" size="1" crc="00000001"/>
  </game>
</datafile>`)
	dataRoot := t.TempDir()
	if _, err := Import(initialDAT, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}

	plan, err := Import(updatedDAT, ImportOptions{DataRoot: dataRoot})
	if err != nil {
		t.Fatalf("merge plan returned error: %v", err)
	}
	if plan.Changes.Action != "merge" || plan.Changes.Reconciled != 1 || plan.Changes.Added != 0 {
		t.Fatalf("unexpected merge changes: %#v", plan.Changes)
	}
	if _, err := Import(updatedDAT, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("merge Import returned error: %v", err)
	}
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Game One", "game.yaml"))
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Game Two", "game.yaml"))
}

func TestImportMergeAcceptsDifferentDatasetForSamePlatform(t *testing.T) {
	initialDAT := writeTempDAT(t, "Nintendo - Nintendo 64.dat", sampleDAT)
	otherDAT := writeTempDAT(t, "Other Dataset.dat", strings.ReplaceAll(
		sampleDAT,
		"Nintendo - Nintendo 64 (BigEndian)",
		"Other Curated Dataset",
	))
	dataRoot := t.TempDir()
	if _, err := Import(initialDAT, ImportOptions{DataRoot: dataRoot, Apply: true}); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}

	result, err := Import(otherDAT, ImportOptions{
		DataRoot:         dataRoot,
		PlatformOverride: "Nintendo - Nintendo 64",
		Apply:            true,
	})
	if err != nil {
		t.Fatalf("merge Import returned error: %v", err)
	}
	if result.Changes.Action != "merge" || result.Changes.Reconciled != 1 {
		t.Fatalf("unexpected merge changes: %#v", result.Changes)
	}
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"))
	catalogYAML := readText(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"))
	if !strings.Contains(catalogYAML, `dataset: "Other Curated Dataset"`) {
		t.Fatalf("catalog provenance was not updated:\n%s", catalogYAML)
	}
}

func TestImportMergeExpandsOneGameOneReleaseDATWithoutLosingData(t *testing.T) {
	oneReleaseDAT := writeTempDAT(t, "Test Platform 1G1R.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Test Platform 1G1R</name><author>No-Intro</author></header>
  <game name="Alpha (USA)" id="alpha-us">
    <rom name="Alpha (USA).rom" size="1" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
</datafile>`)
	fullDAT := writeTempDAT(t, "Test Platform Full.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Test Platform Full</name><author>No-Intro</author></header>
  <game name="Alpha (USA)" id="alpha-us">
    <rom name="Alpha (USA).rom" size="1" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
  <game name="Alpha (Japan)" id="alpha-jp" cloneofid="alpha-us">
    <rom name="Alpha (Japan).rom" size="1" crc="00000002" sha1="0000000000000000000000000000000000000002"/>
  </game>
</datafile>`)
	dataRoot := t.TempDir()
	options := ImportOptions{DataRoot: dataRoot, PlatformOverride: "Test Platform", Apply: true}
	if _, err := Import(oneReleaseDAT, options); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}
	gamePath := filepath.Join(dataRoot, "platforms", "Test Platform", "Alpha")
	gameYAMLPath := filepath.Join(gamePath, "game.yaml")
	existing := readText(t, gameYAMLPath) + "\nsources:\n  manual:\n    matches:\n      - title: Alpha\n"
	if err := os.WriteFile(gameYAMLPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	mediaPath := filepath.Join(gamePath, "media", "box-front.manual.png")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(mediaPath, []byte("image"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	result, err := Import(fullDAT, options)
	if err != nil {
		t.Fatalf("full Import returned error: %v", err)
	}
	if result.Changes.Action != "merge" || result.Changes.Reconciled != 1 || result.Changes.Added != 0 {
		t.Fatalf("unexpected full merge changes: %#v", result.Changes)
	}
	mergedGame := readText(t, gameYAMLPath)
	for _, expected := range []string{`"Alpha (USA)"`, `"Alpha (Japan)"`, "sources:", "title: Alpha"} {
		if !strings.Contains(mergedGame, expected) {
			t.Fatalf("merged game.yaml missing %q:\n%s", expected, mergedGame)
		}
	}
	mergedFingerprints := readText(t, filepath.Join(gamePath, "fingerprints.yaml"))
	for _, expected := range []string{"no-intro.alpha-us", "no-intro.alpha-jp"} {
		if !strings.Contains(mergedFingerprints, expected) {
			t.Fatalf("merged fingerprints missing %q:\n%s", expected, mergedFingerprints)
		}
	}
	if contents := readText(t, mediaPath); contents != "image" {
		t.Fatalf("media contents = %q", contents)
	}

	if _, err := Import(oneReleaseDAT, options); err != nil {
		t.Fatalf("later partial Import returned error: %v", err)
	}
	if mergedGame = readText(t, gameYAMLPath); !strings.Contains(mergedGame, `"Alpha (Japan)"`) {
		t.Fatalf("later partial DAT removed the full-set release:\n%s", mergedGame)
	}
}

func TestImportMergeCollectsAmbiguousGroupingAfterFullScan(t *testing.T) {
	separateDAT := writeTempDAT(t, "Separate.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Grouping Test</name><author>No-Intro</author></header>
  <game name="Alpha (USA)" id="alpha">
    <rom name="Alpha.rom" size="1" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
  <game name="Beta (USA)" id="beta">
    <rom name="Beta.rom" size="1" crc="00000002" sha1="0000000000000000000000000000000000000002"/>
  </game>
</datafile>`)
	groupedDAT := writeTempDAT(t, "Grouped.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Grouping Test Full</name><author>No-Intro</author></header>
  <game name="Alpha (USA)" id="alpha">
    <rom name="Alpha.rom" size="1" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
  <game name="Beta (USA)" id="beta" cloneofid="alpha">
    <rom name="Beta.rom" size="1" crc="00000002" sha1="0000000000000000000000000000000000000002"/>
  </game>
  <game name="Gamma (USA)" id="gamma">
    <rom name="Gamma.rom" size="1" crc="00000003" sha1="0000000000000000000000000000000000000003"/>
  </game>
</datafile>`)
	dataRoot := t.TempDir()
	options := ImportOptions{DataRoot: dataRoot, PlatformOverride: "Grouping Test", Apply: true}
	if _, err := Import(separateDAT, options); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}

	plan, err := Import(groupedDAT, ImportOptions{DataRoot: dataRoot, PlatformOverride: "Grouping Test"})
	if err != nil {
		t.Fatalf("merge plan returned error: %v", err)
	}
	if plan.Changes.Added != 1 || plan.Changes.Reconciled != 0 || len(plan.Changes.Conflicts) != 1 {
		t.Fatalf("unexpected merge plan: %#v", plan.Changes)
	}
	for _, expected := range []string{"Alpha", "Beta"} {
		if !strings.Contains(plan.Changes.Conflicts[0], expected) {
			t.Fatalf("unresolved match does not mention %q: %s", expected, plan.Changes.Conflicts[0])
		}
	}

	_, err = Import(groupedDAT, options)
	if err == nil || !strings.Contains(err.Error(), "unresolved match") {
		t.Fatalf("expected final unresolved-match error, got %v", err)
	}
	mustExist(t, filepath.Join(dataRoot, "platforms", "Grouping Test", "Alpha", "game.yaml"))
	mustExist(t, filepath.Join(dataRoot, "platforms", "Grouping Test", "Beta", "game.yaml"))
	mustNotExist(t, filepath.Join(dataRoot, "platforms", "Grouping Test", "Gamma"))
}

func TestImportMergeMatchesRenamedEntryByCompleteReleaseFingerprint(t *testing.T) {
	oldDAT := writeTempDAT(t, "Old Names.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Hash Test Old</name><author>No-Intro</author></header>
  <game name="Old Name (USA)" id="old-id">
    <rom name="Old Name.rom" size="1024" crc="12345678" sha1="1111111111111111111111111111111111111111" sha256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"/>
  </game>
</datafile>`)
	newDAT := writeTempDAT(t, "Community Names.dat", `<?xml version="1.0"?>
<datafile>
  <header><name>Hash Test New</name><author>Community</author></header>
  <game name="New Name (USA)" id="new-id">
    <rom name="New Name.rom" size="1024" crc="12345678" sha1="1111111111111111111111111111111111111111"/>
  </game>
</datafile>`)
	dataRoot := t.TempDir()
	options := ImportOptions{DataRoot: dataRoot, PlatformOverride: "Hash Test", Apply: true}
	if _, err := Import(oldDAT, options); err != nil {
		t.Fatalf("initial Import returned error: %v", err)
	}
	result, err := Import(newDAT, options)
	if err != nil {
		t.Fatalf("hash merge returned error: %v", err)
	}
	if result.Changes.Reconciled != 1 || result.Changes.Added != 0 || len(result.Changes.Conflicts) != 0 {
		t.Fatalf("unexpected hash merge result: %#v", result.Changes)
	}
	oldFolder := filepath.Join(dataRoot, "platforms", "Hash Test", "Old Name")
	mustExist(t, filepath.Join(oldFolder, "game.yaml"))
	mustNotExist(t, filepath.Join(dataRoot, "platforms", "Hash Test", "New Name"))
	gameYAML := readText(t, filepath.Join(oldFolder, "game.yaml"))
	for _, expected := range []string{`"Old Name (USA)"`, `"New Name (USA)"`} {
		if !strings.Contains(gameYAML, expected) {
			t.Fatalf("hash-merged game missing %q:\n%s", expected, gameYAML)
		}
	}
}

func TestImportSkipsNamedBIOSGames(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", `<?xml version="1.0"?>
<datafile>
  <header>
    <name>Nintendo - Nintendo 64 (BigEndian)</name>
    <author>No-Intro</author>
  </header>
  <game name="[BIOS] Nintendo 64 - PIF (Japan, USA)" id="bios-pif">
    <rom name="[BIOS] Nintendo 64 - PIF (Japan, USA).z64" size="2048" crc="00000001" md5="00000000000000000000000000000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
  <game name="Star Fox 64 (USA)" id="starfox">
    <rom name="Star Fox 64 (USA).z64" size="12582912" crc="b1fcaa9c" md5="caf9a78db13ee00002ff63a3c0c5eabb" sha1="d8b1088520f7c5f81433292a9258c1184afa1457"/>
  </game>
</datafile>`)
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.GameBundleCount != 1 || result.ReleaseCount != 1 || result.FingerprintCount != 1 {
		t.Fatalf("unexpected filtered counts: bundles=%d releases=%d fingerprints=%d", result.GameBundleCount, result.ReleaseCount, result.FingerprintCount)
	}
	if len(result.SampleGames) != 1 || result.SampleGames[0].Name != "Star Fox 64 (USA)" {
		t.Fatalf("expected sample games to use filtered import entries, got %#v", result.SampleGames)
	}
	mustExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"))
	mustNotExist(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "[BIOS] Nintendo 64 - PIF", "game.yaml"))
}

func TestImportReportsProgress(t *testing.T) {
	datPath := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)
	dataRoot := t.TempDir()

	var snapshots []ImportProgress
	_, err := Import(datPath, ImportOptions{
		DataRoot: dataRoot,
		Apply:    true,
		Progress: func(snapshot ImportProgress) {
			snapshots = append(snapshots, snapshot)
		},
	})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	for _, stage := range []string{"opening", "parsing", "planning", "bundling", "writing"} {
		if !hasImportProgressStage(snapshots, stage) {
			t.Fatalf("missing progress stage %q in %#v", stage, snapshots)
		}
	}
	last := snapshots[len(snapshots)-1]
	if last.Stage != "writing" || last.Current != 1 || last.Total != 1 || last.Item != "Star Fox 64" {
		t.Fatalf("unexpected final progress snapshot: %#v", last)
	}
}

func TestParseDatafileSpoolsROMPayloadsPerEntry(t *testing.T) {
	spool := t.TempDir()
	parsed, err := parseDatafile(strings.NewReader(`<?xml version="1.0"?>
<datafile>
  <header><name>Nintendo - Nintendo 64</name></header>
  <game name="Game One (USA)" id="one">
    <rom name="one.z64" size="1" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </game>
  <game name="Game Two (Japan)" id="two">
    <rom name="two-a.z64" size="2" crc="00000002"/>
    <rom name="two-b.z64" size="3" crc="00000003"/>
  </game>
</datafile>`), spool)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.romCount != 3 || parsed.hashCounts.CRC32 != 3 || parsed.hashCounts.SHA1 != 1 {
		t.Fatalf("unexpected counters: roms=%d hashes=%#v", parsed.romCount, parsed.hashCounts)
	}
	for _, game := range parsed.games {
		if len(game.ROMs) != 0 || game.ROMSpoolPath == "" {
			t.Fatalf("ROM payload remained in memory: %#v", game)
		}
		roms, err := gameROMs(game)
		if err != nil {
			t.Fatal(err)
		}
		if len(roms) != game.ROMCount {
			t.Fatalf("restored %d ROMs, want %d", len(roms), game.ROMCount)
		}
	}
}

func TestImportRedumpWritesRedumpProvenance(t *testing.T) {
	datPath := writeTempDAT(t, "Sony - PlayStation - Datfile.dat", sampleRedumpDAT)
	dataRoot := t.TempDir()

	result, err := Import(datPath, ImportOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if result.Plan.Authority != "redump" {
		t.Fatalf("unexpected authority: %q", result.Plan.Authority)
	}
	if result.GameBundleCount != 1 {
		t.Fatalf("expected multi-disc releases to fold into one bundle, got %d", result.GameBundleCount)
	}
	if result.ReleaseCount != 2 || result.FingerprintCount != 3 {
		t.Fatalf("unexpected counts: releases=%d fingerprints=%d", result.ReleaseCount, result.FingerprintCount)
	}

	gameYAML := readText(t, filepath.Join(dataRoot, "platforms", "Sony - PlayStation", "Chrono Cross", "game.yaml"))
	if !strings.Contains(gameYAML, `source: "redump"`) {
		t.Fatalf("expected Redump release authority in game.yaml:\n%s", gameYAML)
	}
	if !strings.Contains(gameYAML, `serial: "SLUS-01041"`) || !strings.Contains(gameYAML, `version: "Rev 1"`) {
		t.Fatalf("expected Redump release serial/version in game.yaml:\n%s", gameYAML)
	}
	if strings.Contains(gameYAML, `serial: ""`) || strings.Contains(gameYAML, `version: ""`) {
		t.Fatalf("missing Redump serial/version variants should not render empty fields:\n%s", gameYAML)
	}
	fingerprintsYAML := readText(t, filepath.Join(dataRoot, "platforms", "Sony - PlayStation", "Chrono Cross", "fingerprints.yaml"))
	if !strings.Contains(fingerprintsYAML, `id: "redump.Chrono Cross (USA, Canada) (Disc 1).1"`) {
		t.Fatalf("expected Redump fingerprint id in fingerprints.yaml:\n%s", fingerprintsYAML)
	}
	if !strings.Contains(fingerprintsYAML, `name: "Redump"`) {
		t.Fatalf("expected Redump source name in fingerprints.yaml:\n%s", fingerprintsYAML)
	}
}

func hasImportProgressStage(snapshots []ImportProgress, stage string) bool {
	for _, snapshot := range snapshots {
		if snapshot.Stage == stage {
			return true
		}
	}
	return false
}

func TestBaseTitleStripsNoIntroFields(t *testing.T) {
	tests := map[string]string{
		"Star Fox 64 (USA) (Rev A)":                                                      "Star Fox 64",
		"007 - The World Is Not Enough (Europe) (En,Fr,De)":                              "007 - The World Is Not Enough",
		"40 Winks (USA) (Proto) (2000-01-10)":                                            "40 Winks",
		"40 Winks (Europe) (En,Fr,Es) (Piko Interactive)":                                "40 Winks",
		"40 Winks (USA) (Piko Interactive)":                                              "40 Winks",
		"Worms Armageddon (USA) (En,Fr,Es) (Limited Run Games)":                          "Worms Armageddon",
		"WWF No Mercy (USA) (Beta) (2000-06-xx)":                                         "WWF No Mercy",
		"WWF No Mercy (USA) (Beta) (2000-08-xx)":                                         "WWF No Mercy",
		"WWF No Mercy (USA) (Beta) [b]":                                                  "WWF No Mercy",
		"Legend of Zelda, The - Majora's Mask (USA) (Zelda Collection)":                  "Legend of Zelda, The - Majora's Mask",
		"Legend of Zelda, The - Majora's Mask (Europe) (En,Fr,De,Es) (Rev 1) (Debug)":    "Legend of Zelda, The - Majora's Mask",
		"Legend of Zelda, The - Ocarina of Time (Europe) (Beta) (2003-02-13) (GameCube)": "Legend of Zelda, The - Ocarina of Time",
		"Legend of Zelda, The - Ocarina of Time - Master Quest (USA) (GameCube)":         "Legend of Zelda, The - Ocarina of Time - Master Quest",
		"[BIOS] Nintendo 64 - PIF (Japan, USA)":                                          "[BIOS] Nintendo 64 - PIF",
		"64 de Hakken!! Tamagotchi - Minna de Tamagotchi World":                          "64 de Hakken!! Tamagotchi - Minna de Tamagotchi World",
	}
	for input, expected := range tests {
		if actual := baseTitle(input); actual != expected {
			t.Fatalf("baseTitle(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestReleaseRegionsUseObservedDATRegionLabels(t *testing.T) {
	regions := releaseRegions("Regional Game (Portugal, UK, Russia) (En,Ja,Zh,Pt)")
	want := []string{"Portugal", "UK", "Russia"}
	if len(regions) != len(want) {
		t.Fatalf("releaseRegions returned %#v, want %#v", regions, want)
	}
	for index := range want {
		if regions[index] != want[index] {
			t.Fatalf("releaseRegions returned %#v, want %#v", regions, want)
		}
	}
}

func TestBundleGamesRetainsUnknownDistributionQualifiers(t *testing.T) {
	bundles := bundleGames([]datGame{
		{Name: "40 Winks (Europe) (En,Fr,Es) (Piko Interactive)"},
		{Name: "40 Winks (USA) (Piko Interactive)"},
		{Name: "Worms Armageddon (USA) (En,Fr,Es) (Limited Run Games)"},
		{Name: "Worms Armageddon (Europe)"},
		{Name: "WWF No Mercy (USA)"},
		{Name: "WWF No Mercy (USA) (Beta) (2000-06-xx)"},
		{Name: "WWF No Mercy (USA) (Beta) (2000-08-xx)"},
		{Name: "Legend of Zelda, The - Majora's Mask (Europe) (Zelda Collection)"},
		{Name: "Legend of Zelda, The - Majora's Mask (USA) (Zelda Collection)"},
		{Name: "Legend of Zelda, The - Ocarina of Time (Europe) (GameCube)"},
		{Name: "Legend of Zelda, The - Ocarina of Time - Master Quest (USA) (GameCube)"},
	})

	if len(bundles) != 7 {
		t.Fatalf("expected separate unknown editions with development releases inside games, got %d", len(bundles))
	}
	expected := map[string]int{
		"40 Winks (Piko Interactive)":                                      2,
		"Worms Armageddon":                                                 1,
		"Worms Armageddon (Limited Run Games)":                             1,
		"Legend of Zelda, The - Majora's Mask (Zelda Collection)":          2,
		"Legend of Zelda, The - Ocarina of Time (GameCube)":                1,
		"Legend of Zelda, The - Ocarina of Time - Master Quest (GameCube)": 1,
		"WWF No Mercy": 3,
	}
	for _, bundle := range bundles {
		if len(bundle.Releases) != expected[bundle.Title] {
			t.Fatalf("unexpected bundle: %+v", bundle)
		}
	}

}

func TestBundleGamesGroupsCloneOfIDs(t *testing.T) {
	bundles := bundleGames([]datGame{
		{Name: "Legend of Zelda, The - Ocarina of Time (USA)", ID: "0375"},
		{Name: "Zelda no Densetsu - Toki no Ocarina (Japan)", ID: "0877", CloneOf: "0375"},
		{Name: "Legend of Zelda, The - Ocarina of Time - Master Quest (Europe) (GameCube)", ID: "0931"},
		{Name: "Zelda no Densetsu - Toki no Ocarina GC Ura (Japan) (GameCube)", ID: "0932", CloneOf: "0931"},
	})

	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d: %#v", len(bundles), bundles)
	}
	expected := map[string]int{
		"Legend of Zelda, The - Ocarina of Time":                           2,
		"Legend of Zelda, The - Ocarina of Time - Master Quest (GameCube)": 2,
	}
	for _, bundle := range bundles {
		count, ok := expected[bundle.Title]
		if !ok {
			t.Fatalf("unexpected bundle title %q in %#v", bundle.Title, bundles)
		}
		if len(bundle.Releases) != count {
			t.Fatalf("bundle %q has %d releases, want %d", bundle.Title, len(bundle.Releases), count)
		}
	}
}

func TestBundleGamesNamesVisibleReleaseFlags(t *testing.T) {
	bundles := bundleGames([]datGame{
		{Name: "Aladdin (World)", ID: "1000"},
		{Name: "Aladdin (World) (Unl) (Pirate)", ID: "1001", CloneOf: "pirate-root"},
		{Name: "Combined Flag (World, Unl, Pirate)"},
		{Name: "Dev Build (USA) (Proto)"},
		{Name: "Store Demo (USA) (Demo)"},
		{Name: "Home Project (World) (Homebrew)"},
		{Name: "Sample Cart (Japan) (Sample)"},
		{Name: "Noisy Build (USA) (Beta) (2000-06-xx)"},
	})

	folders := map[string]string{}
	for _, bundle := range bundles {
		folders[bundle.Folder] = bundle.Title
	}
	for _, expected := range []string{
		"Aladdin",
		"Aladdin (Unl) (Pirate)",
		"Combined Flag (World, Unl, Pirate)",
		"Dev Build",
		"Store Demo",
		"Home Project (Homebrew)",
		"Sample Cart",
		"Noisy Build",
	} {
		if _, ok := folders[expected]; !ok {
			t.Fatalf("expected folder %q in %#v", expected, bundles)
		}
	}
	for _, unexpected := range []string{
		"Aladdin [2]",
		"Noisy Build (Beta)",
	} {
		if _, ok := folders[unexpected]; ok {
			t.Fatalf("did not expect fallback folder %q in %#v", unexpected, bundles)
		}
	}
}

func TestBundleGamesDoesNotUseRegionsAsFolderDiscriminators(t *testing.T) {
	bundles := bundleGames([]datGame{
		{Name: "Adventures of Lolo (Europe)", ID: "0058"},
		{Name: "Adventures of Lolo (USA)", ID: "0060", CloneOf: "0058"},
		{Name: "Adventures of Lolo (Japan) (En)", ID: "0059"},
	})

	folders := map[string]string{}
	for _, bundle := range bundles {
		folders[bundle.Folder] = bundle.Title
	}
	for _, expected := range []string{
		"Adventures of Lolo",
		"Adventures of Lolo [2]",
	} {
		if _, ok := folders[expected]; !ok {
			t.Fatalf("expected folder %q in %#v", expected, bundles)
		}
	}
	if _, ok := folders["Adventures of Lolo (Japan)"]; ok {
		t.Fatalf("did not expect region folder discriminator in %#v", bundles)
	}
}

func TestBundleGamesKeepsDevelopmentReleasesWithCloneParent(t *testing.T) {
	games := []datGame{
		{Name: "Retail Game (Japan)", ID: "0001"},
		{Name: "Retail Game (USA) (Proto)", ID: "0002", CloneOf: "0001"},
		{Name: "Regional Name (Europe) (Beta) (2001-01-01)", ID: "0003", CloneOf: "0001"},
		{Name: "Retail Game (USA) (Demo)", ID: "0004"},
		{Name: "Retail Game (Japan) (Demo)", ID: "0005", CloneOf: "0001"},
	}
	// Authority clone roots and an unlinked same-title family remain distinct;
	// known parentage groups regional titles, not arbitrary words in a name.
	bundles := bundleGames(games)
	if len(bundles) != 2 {
		t.Fatal(bundles)
	}
	for _, bundle := range bundles {
		if len(bundle.Releases) == 4 && chooseDefaultRelease(bundle.Releases, nil, nil) != "Retail Game (Japan)" {
			t.Fatal("selected an early build", bundle)
		}
	}
	unknown := bundleGames([]datGame{{Name: "Retail Game (USA)"}, {Name: "Different Project (USA) (Proto)"}})
	if len(unknown) != 2 {
		t.Fatal("unknown parent inferred", unknown)
	}
}

func TestBundleGamesDoesNotFoldROMDependencies(t *testing.T) {
	bundles := bundleGames([]datGame{
		{Name: "Parent Game (World)", ID: "0001"},
		{Name: "Regional Title (Japan)", ID: "0002", ROMOf: "0001"},
	})
	if len(bundles) != 2 {
		t.Fatalf("ROM dependency folded unrelated titles: %#v", bundles)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat err=%v", path, err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s failed: %v", path, err)
	}
	return string(contents)
}

func TestImportTOSECXMLKeepsAuthorityInsteadOfNoIntro(t *testing.T) {
	input := `<datafile><header><name>Sharp - X68000</name><author>TOSEC</author><version>2026-10-07</version></header><game name="Example (1991)(Publisher)"><rom name="Example.dim" size="4" crc="12345678" sha1="0123456789012345678901234567890123456789"/></game></datafile>`
	path := writeTempDAT(t, "games.dat", input)
	root := t.TempDir()
	result, err := Import(path, ImportOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Authority != "tosec" {
		t.Fatalf("TOSEC received %q authority", result.Plan.Authority)
	}
	found := false
	err = filepath.WalkDir(filepath.Join(root, "platforms"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() != "fingerprints.yaml" {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		found = true
		if !strings.Contains(string(data), "TOSEC") || strings.Contains(string(data), "No-Intro") {
			t.Fatalf("wrong fingerprint provenance: %s", data)
		}
		return nil
	})
	if err != nil || !found {
		t.Fatal("missing fingerprints", err)
	}
}
