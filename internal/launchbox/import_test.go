package launchbox

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/sourceyaml"
	"rommeta/internal/taskprogress"
)

func TestImportMatchesNoIntroTitleShape(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Legend of Zelda, The - Ocarina of Time")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "The Legend of Zelda: Ocarina of Time"

releases:
  "Legend of Zelda, The - Ocarina of Time (USA)":
    title: "The Legend of Zelda: Ocarina of Time"
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>42</DatabaseID>
    <Name>The Legend of Zelda: Ocarina of Time</Name>
    <Platform>Nintendo 64</Platform>
    <ReleaseDate>1998-11-23T00:00:00-08:00</ReleaseDate>
    <Developer>Nintendo</Developer>
    <Publisher>Nintendo</Publisher>
    <Genres>Action; Adventure</Genres>
    <MaxPlayers>1</MaxPlayers>
    <CommunityRating>4.8</CommunityRating>
    <Overview>Hero of Time.</Overview>
  </Game>
  <GameAlternateName>
    <DatabaseID>42</DatabaseID>
    <AlternateName>Zelda 64</AlternateName>
    <Region>Europe</Region>
  </GameAlternateName>
  <GameImage>
    <DatabaseID>42</DatabaseID>
    <Type>Box - Front</Type>
    <Region>North America</Region>
    <FileName>games/zelda/front.png</FileName>
  </GameImage>
  <GameImage>
    <DatabaseID>42</DatabaseID>
    <Type>Box - Front</Type>
    <Region>Japan</Region>
    <FileName>games/zelda/front-jp.png</FileName>
  </GameImage>
  <GameImage>
    <DatabaseID>42</DatabaseID>
    <Type>Clear Logo</Type>
    <FileName>games/zelda/logo.png</FileName>
  </GameImage>
</LaunchBox>`,
	})

	var progressStages []string
	dryRun, err := Import(ImportOptions{
		DataRoot: dataRoot,
		ZipPath:  zipPath,
		Progress: func(snapshot taskprogress.Snapshot) {
			progressStages = appendUnique(progressStages, snapshot.Stage)
		},
	})
	if err != nil {
		t.Fatalf("Import dry run returned error: %v", err)
	}
	if dryRun.ReleaseCount != 1 || dryRun.MatchedReleaseCount != 1 || dryRun.WrittenFileCount != 0 {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}
	for _, stage := range []string{"scanning catalog", "reading catalog", "reading platform list", "reading metadata", "matching"} {
		if !containsString(progressStages, stage) {
			t.Fatalf("progress did not include %q: %#v", stage, progressStages)
		}
	}
	if containsString(progressStages, "checking existing metadata") {
		t.Fatalf("LaunchBox should not run a separate existing-metadata scan: %#v", progressStages)
	}

	var cachedProgressStages []string
	result, err := Import(ImportOptions{
		DataRoot: dataRoot,
		ZipPath:  zipPath,
		Apply:    true,
		Progress: func(snapshot taskprogress.Snapshot) {
			cachedProgressStages = appendUnique(cachedProgressStages, snapshot.Stage)
		},
	})
	if err != nil {
		t.Fatalf("Import apply returned error: %v", err)
	}
	if result.WrittenFileCount != 1 {
		t.Fatalf("expected one written file, got %#v", result)
	}
	if !containsString(cachedProgressStages, "reading cache") {
		t.Fatalf("expected second pass to read LaunchBox snapshot cache, got %#v", cachedProgressStages)
	}
	if containsString(cachedProgressStages, "reading metadata") {
		t.Fatalf("expected cached second pass not to read Metadata.xml, got %#v", cachedProgressStages)
	}
	second, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath, Apply: true})
	if err != nil {
		t.Fatalf("second Import apply returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 1 {
		t.Fatalf("expected existing source metadata to be skipped on second apply, got %#v", second)
	}
	forced, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath, Apply: true, ForceOverwrite: true})
	if err != nil {
		t.Fatalf("force Import apply returned error: %v", err)
	}
	if forced.WrittenFileCount != 1 || forced.SkippedExistingCount != 0 {
		t.Fatalf("expected force overwrite to write existing source metadata, got %#v", forced)
	}

	contents := readFileForTest(t, filepath.Join(gamePath, "game.yaml"))
	for _, expected := range []string{
		"launchbox:",
		"database_id: \"42\"",
		"title: \"The Legend of Zelda: Ocarina of Time\"",
		"release_date: \"1998-11-23\"",
		"genres: [\"Action\", \"Adventure\"]",
		"alternate_titles: [\"Zelda 64\"]",
		"titles:",
		"- title: \"Zelda 64\"",
		"region: \"Europe\"",
		"media:",
		"box_front:",
		"- url: \"https://images.launchbox-app.com/games/zelda/front.png\"",
		"region: \"North America\"",
		"source_file: \"games/zelda/front.png\"",
		"- url: \"https://images.launchbox-app.com/games/zelda/front-jp.png\"",
		"region: \"Japan\"",
		"logo:",
		"- url: \"https://images.launchbox-app.com/games/zelda/logo.png\"",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("game.yaml does not contain %q:\n%s", expected, contents)
		}
	}
	if strings.Contains(contents, "gamesdb.launchbox-app.com/games/details/42") {
		t.Fatalf("game.yaml should not derive a web page URL from LaunchBox DatabaseID:\n%s", contents)
	}
}

func TestImportApplyWritesLaunchBoxGamesIncrementally(t *testing.T) {
	dataRoot := t.TempDir()
	alphaPath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Alpha Test")
	writeFileForTest(t, filepath.Join(alphaPath, "game.yaml"), `title: "Alpha Test"

releases:
  "Alpha Test (USA)":
`)
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Beta Test", "game.yaml"), `title: "Beta Test"

releases:
  "Beta Test (USA)":
`)
	missingPath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Zulu Missing")
	writeFileForTest(t, filepath.Join(missingPath, "game.yaml"), `title: "Zulu Missing"

releases:
  "Zulu Missing (USA)":
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>1</DatabaseID>
    <Name>Alpha Test</Name>
    <Platform>Nintendo 64</Platform>
  </Game>
  <Game>
    <DatabaseID>2</DatabaseID>
    <Name>Beta Test</Name>
    <Platform>Nintendo 64</Platform>
  </Game>
</LaunchBox>`,
	})

	checked := false
	var stages []string
	result, err := Import(ImportOptions{
		DataRoot: dataRoot,
		ZipPath:  zipPath,
		Apply:    true,
		Progress: func(snapshot taskprogress.Snapshot) {
			stages = appendUnique(stages, snapshot.Stage)
			if snapshot.Stage == "matching" && strings.Contains(snapshot.Item, "Beta Test") {
				checked = true
				contents := readFileForTest(t, filepath.Join(alphaPath, "game.yaml"))
				if !strings.Contains(contents, "  launchbox:") {
					t.Fatalf("first game was not written before matching the second game:\n%s", contents)
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if !checked {
		t.Fatalf("progress never reached the second game")
	}
	if containsString(stages, "checking existing metadata") {
		t.Fatalf("LaunchBox should not run a separate existing-metadata scan: %#v", stages)
	}
	if result.WrittenFileCount != 3 || result.MatchedReleaseCount != 2 || result.NotFoundGameCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	missing := readFileForTest(t, filepath.Join(missingPath, "game.yaml"))
	if !strings.Contains(missing, "  launchbox:") || !strings.Contains(missing, "status: not_found") || !strings.Contains(missing, "checked_at:") {
		t.Fatalf("missing game was not marked as not_found:\n%s", missing)
	}
	second, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath, Apply: true})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 3 {
		t.Fatalf("expected matched and not_found games to be skipped on second import, got %#v", second)
	}
}

func TestImportDoesNotMatchPunctuationOnlyTitleSuffix(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Sega - Mega CD & Sega CD", "Sonic CD")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Sonic CD"

releases:
  "Sonic CD (USA)":
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>142663</DatabaseID>
    <Name>Sonic CD++</Name>
    <Platform>Sega CD</Platform>
  </Game>
</LaunchBox>`,
	})

	result, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedReleaseCount != 0 || result.NotFoundGameCount != 1 {
		t.Fatalf("Sonic CD++ should not match Sonic CD, got %#v", result)
	}
}

func TestImportKeepsDuplicateSourceTitlesDistinct(t *testing.T) {
	dataRoot := t.TempDir()
	platform := "Sega - Mega Drive - Genesis"
	taitoPath := filepath.Join(dataRoot, "platforms", platform, "Flintstones, The")
	oceanPath := filepath.Join(dataRoot, "platforms", platform, "Flintstones, The [2]")
	writeFileForTest(t, filepath.Join(taitoPath, "game.yaml"), `title: "Flintstones, The"

releases:
  "Flintstones, The (USA) (Taito)":
    title: "Flintstones, The"
    regions: ["USA"]
`)
	writeFileForTest(t, filepath.Join(oceanPath, "game.yaml"), `title: "Flintstones, The"

releases:
  "Flintstones, The (USA) (Ocean) (Sega Channel)":
    title: "Flintstones, The"
    regions: ["USA"]
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>4666</DatabaseID>
    <Name>The Flintstones</Name>
    <Platform>Sega Genesis</Platform>
    <Developer>Taito</Developer>
    <Publisher>Taito</Publisher>
  </Game>
  <Game>
    <DatabaseID>196307</DatabaseID>
    <Name>The Flintstones (Foley Hi-tech)</Name>
    <Platform>Sega Genesis</Platform>
    <Developer>Foley Hi-Tech</Developer>
    <Publisher>Sega of America</Publisher>
  </Game>
</LaunchBox>`,
	})

	result, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedReleaseCount != 1 || result.NotFoundGameCount != 1 || result.WrittenFileCount != 2 {
		t.Fatalf("unexpected duplicate-title result: %#v", result)
	}

	taito := readFileForTest(t, filepath.Join(taitoPath, "game.yaml"))
	if !strings.Contains(taito, `database_id: "4666"`) {
		t.Fatalf("Taito release did not select the Taito LaunchBox entry:\n%s", taito)
	}
	ocean := readFileForTest(t, filepath.Join(oceanPath, "game.yaml"))
	if !strings.Contains(ocean, `status: not_found`) {
		t.Fatalf("ambiguous Ocean release should be left unmatched:\n%s", ocean)
	}
	if strings.Contains(ocean, `database_id: "4666"`) || strings.Contains(ocean, `database_id: "196307"`) {
		t.Fatalf("ambiguous Ocean release should not inherit either duplicate title entry:\n%s", ocean)
	}
}

func TestImportDoesNotMatchOfficialGameForUnofficialRelease(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Aladdin (World, Unl, Pirate)")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Aladdin"

releases:
  "Aladdin (World) (Unl) (Pirate)":
    tags: ["unlicensed", "pirate"]
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>55</DatabaseID>
    <Name>Aladdin</Name>
    <Platform>Nintendo 64</Platform>
  </Game>
</LaunchBox>`,
	})

	result, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedReleaseCount != 0 || result.NotFoundGameCount != 1 {
		t.Fatalf("official LaunchBox entry should not match unofficial release, got %#v", result)
	}
}

func TestImportDoesNotUseAlternateTitlesAsPrimaryMatch(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Sega - Mega CD & Sega CD", "Ecco the Dolphin CD")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Ecco the Dolphin CD"

releases:
  "Ecco the Dolphin CD (Japan) (Disc 1) (Ecco the Dolphin)":
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>440</DatabaseID>
    <Name>Ecco: The Tides of Time</Name>
    <Platform>Sega CD</Platform>
  </Game>
  <GameAlternateName>
    <DatabaseID>440</DatabaseID>
    <AlternateName>Ecco the Dolphin CD</AlternateName>
  </GameAlternateName>
</LaunchBox>`,
	})

	result, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedReleaseCount != 0 || result.NotFoundGameCount != 1 {
		t.Fatalf("LaunchBox alternate title should not be a primary match, got %#v", result)
	}
}

func TestRenderMetadataGroupsIdenticalReleaseMatches(t *testing.T) {
	body := renderMetadata([]matchedRelease{
		{
			Release: "Contra (Europe)",
			Metadata: releaseMetadata{
				DatabaseID: "100",
				Title:      "Contra",
				MediaURLs: map[string][]sourceyaml.MediaCandidate{
					"box_front": {{URL: "https://images.example.test/contra.jpg"}},
				},
			},
		},
		{
			Release: "Contra (USA)",
			Metadata: releaseMetadata{
				DatabaseID: "100",
				Title:      "Contra",
				MediaURLs: map[string][]sourceyaml.MediaCandidate{
					"box_front": {{URL: "https://images.example.test/contra.jpg"}},
				},
			},
		},
	})

	if !strings.Contains(body, `- releases: ["Contra (Europe)", "Contra (USA)"]`) {
		t.Fatalf("metadata was not grouped by releases:\n%s", body)
	}
	if strings.Count(body, "database_id:") != 1 || strings.Count(body, "media:") != 1 {
		t.Fatalf("metadata payload should be rendered once:\n%s", body)
	}
}

func TestRenderMetadataUsesCompactReleaseOverrides(t *testing.T) {
	body := renderMetadata([]matchedRelease{
		{
			Release: "Doom (Europe)",
			Metadata: releaseMetadata{
				DatabaseID:  "100",
				Title:       "Doom",
				Description: "Common description.",
				MediaURLs: map[string][]sourceyaml.MediaCandidate{
					"box_front": {{URL: "https://images.example.test/doom-eu.jpg", Region: "Europe"}},
				},
			},
		},
		{
			Release: "Doom (USA)",
			Metadata: releaseMetadata{
				DatabaseID:  "100",
				Title:       "Doom",
				Description: "Common description.",
				MediaURLs: map[string][]sourceyaml.MediaCandidate{
					"box_front": {{URL: "https://images.example.test/doom-us.jpg", Region: "USA"}},
				},
			},
		},
	})

	for _, expected := range []string{
		`- database_id: "100"`,
		`title: "Doom"`,
		`releases:`,
		`- release: "Doom (Europe)"`,
		`- release: "Doom (USA)"`,
		`https://images.example.test/doom-eu.jpg`,
		`https://images.example.test/doom-us.jpg`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metadata did not contain %q:\n%s", expected, body)
		}
	}
	if strings.Count(body, "database_id:") != 1 || strings.Count(body, "descriptions:") != 1 || strings.Count(body, "media:") != 2 {
		t.Fatalf("metadata should share common fields and keep release media separate:\n%s", body)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestImportResolvesPlatformFromSnapshotNames(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "NEC - PC Engine - TurboGrafx-16", "Bonk's Adventure")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Bonk's Adventure"

releases:
  "Bonk's Adventure (USA)":
`)

	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{
		"Metadata.xml": `<LaunchBox>
  <Game>
    <DatabaseID>101</DatabaseID>
    <Name>Bonk's Adventure</Name>
    <Platform>TurboGrafx-16</Platform>
  </Game>
</LaunchBox>`,
	})

	result, err := Import(ImportOptions{DataRoot: dataRoot, ZipPath: zipPath})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.ReleaseCount != 1 || result.MatchedReleaseCount != 1 {
		t.Fatalf("expected resolved platform match, got %#v", result)
	}
}

func writeFileForTest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	return string(contents)
}

func writeZipForTest(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, contents := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("zip Create failed: %v", err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatalf("zip Write failed: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip Close failed: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile zip failed: %v", err)
	}
}
