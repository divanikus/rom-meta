package openvgdb

import (
	"archive/zip"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/taskprogress"

	_ "modernc.org/sqlite"
)

func TestImportOpenVGDBWritesSourceMetadata(t *testing.T) {
	dataRoot := t.TempDir()
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"), `title: "Star Fox 64"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"

sources:
  launchbox:
    title: "Existing LaunchBox candidate"
`)
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "fingerprints.yaml"), `items:
  - release: "Star Fox 64 (USA)"
`)
	dbPath := createOpenVGDBFixture(t)

	dryRun, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath})
	if err != nil {
		t.Fatalf("dry run Import returned error: %v", err)
	}
	if dryRun.MatchedReleaseCount != 1 || dryRun.WrittenFileCount != 0 {
		t.Fatalf("unexpected dry run result: %#v", dryRun)
	}

	result, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.WrittenFileCount != 1 {
		t.Fatalf("expected one written file, got %#v", result)
	}
	second, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath, Apply: true})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 1 {
		t.Fatalf("expected existing OpenVGDB source metadata to be skipped, got %#v", second)
	}
	forced, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath, Apply: true, ForceOverwrite: true})
	if err != nil {
		t.Fatalf("force Import returned error: %v", err)
	}
	if forced.WrittenFileCount != 1 || forced.SkippedExistingCount != 0 {
		t.Fatalf("expected force overwrite to write existing OpenVGDB source metadata, got %#v", forced)
	}
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml")
	contents, err := os.ReadFile(gamePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(contents), "launchbox:") {
		t.Fatalf("game file did not preserve existing source stanza:\n%s", contents)
	}
	if !strings.Contains(string(contents), "openvgdb:") {
		t.Fatalf("game file did not contain OpenVGDB source stanza:\n%s", contents)
	}
	if !strings.Contains(string(contents), "A rail shooter in space.") {
		t.Fatalf("game file did not contain description:\n%s", contents)
	}
	if !strings.Contains(string(contents), "release_date: \"1997-07-01\"") {
		t.Fatalf("game file did not contain normalized release date:\n%s", contents)
	}
	if !strings.Contains(string(contents), "box_front") {
		t.Fatalf("game file did not contain media URL:\n%s", contents)
	}
}

func TestRenderMetadataUsesCompactReleaseOverrides(t *testing.T) {
	body := renderMetadata([]matchedRelease{
		{
			Release: "Doom (Europe)",
			Metadata: releaseMetadata{
				Title:         "Doom",
				Description:   "Common description.",
				Developer:     "id Software",
				CoverFrontURL: "https://images.example.test/doom-eu.jpg",
				Region:        "Europe",
				ROMFileName:   "doom-eu.bin",
			},
		},
		{
			Release: "Doom (USA)",
			Metadata: releaseMetadata{
				Title:         "Doom",
				Description:   "Common description.",
				Developer:     "id Software",
				CoverFrontURL: "https://images.example.test/doom-us.jpg",
				Region:        "USA",
				ROMFileName:   "doom-us.bin",
			},
		},
	})

	for _, expected := range []string{
		`- title: "Doom"`,
		`developer: "id Software"`,
		`releases:`,
		`- release: "Doom (Europe)"`,
		`- release: "Doom (USA)"`,
		`https://images.example.test/doom-eu.jpg`,
		`https://images.example.test/doom-us.jpg`,
		`file_name: "doom-eu.bin"`,
		`file_name: "doom-us.bin"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metadata did not contain %q:\n%s", expected, body)
		}
	}
	if strings.Count(body, "descriptions:") != 1 || strings.Count(body, "media:") != 2 || strings.Count(body, "openvgdb_rom:") != 2 {
		t.Fatalf("metadata should share common fields and keep release overrides separate:\n%s", body)
	}
}

func TestImportOpenVGDBMatchesByFingerprintBeforeName(t *testing.T) {
	dataRoot := t.TempDir()
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Regional Alias", "game.yaml"), `title: "Regional Alias"

releases:
  "Regional Alias (USA)":
`)
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Regional Alias", "fingerprints.yaml"), `items:
  - release: "Regional Alias (USA)"
    file:
      name: "Regional Alias (USA).n64"
      crc32: "363e7ee8"
      md5: "fecdcbbcd1a178a2f4b29b43facf9f39"
      sha1: "cff7fb46e79d35f727e4e20cc4c13492ecd2a2d7"
`)
	dbPath := createOpenVGDBFixture(t)

	result, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath, Apply: true})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedReleaseCount != 1 || result.NotFoundGameCount != 0 || result.WrittenFileCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	contents, err := os.ReadFile(filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Regional Alias", "game.yaml"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	for _, expected := range []string{
		`release: "Regional Alias (USA)"`,
		`title: "Star Fox 64"`,
		`crc32: 363e7ee8`,
	} {
		if !strings.Contains(string(contents), expected) {
			t.Fatalf("fingerprint match did not write %q:\n%s", expected, contents)
		}
	}
}

func TestImportApplyWritesOpenVGDBGamesIncrementally(t *testing.T) {
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
	dbPath := createOpenVGDBFixture(t)

	checked := false
	var stages []string
	result, err := Import(ImportOptions{
		DataRoot: dataRoot,
		DBPath:   dbPath,
		Apply:    true,
		Progress: func(snapshot taskprogress.Snapshot) {
			stages = appendUniqueString(stages, snapshot.Stage)
			if snapshot.Stage == "matching" && strings.Contains(snapshot.Item, "Beta Test") {
				checked = true
				contents, err := os.ReadFile(filepath.Join(alphaPath, "game.yaml"))
				if err != nil {
					t.Fatalf("ReadFile failed: %v", err)
				}
				if !strings.Contains(string(contents), "  openvgdb:") {
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
	for _, expected := range []string{"scanning catalog", "reading catalog", "opening database", "matching"} {
		if !containsString(stages, expected) {
			t.Fatalf("expected progress stage %q in %#v", expected, stages)
		}
	}
	if containsString(stages, "checking existing metadata") {
		t.Fatalf("OpenVGDB should not run a separate existing-metadata scan: %#v", stages)
	}
	if result.WrittenFileCount != 3 || result.MatchedReleaseCount != 2 || result.NotFoundGameCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	missing, err := os.ReadFile(filepath.Join(missingPath, "game.yaml"))
	if err != nil {
		t.Fatalf("ReadFile missing failed: %v", err)
	}
	if !strings.Contains(string(missing), "  openvgdb:") || !strings.Contains(string(missing), "status: not_found") || !strings.Contains(string(missing), "checked_at:") {
		t.Fatalf("missing game was not marked as not_found:\n%s", missing)
	}
	second, err := Import(ImportOptions{DataRoot: dataRoot, DBPath: dbPath, Apply: true})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 3 {
		t.Fatalf("expected matched and not_found games to be skipped on second import, got %#v", second)
	}
}

func TestEnsureSnapshotDownloadsIntoRuntimeState(t *testing.T) {
	dbPath := createOpenVGDBFixture(t)
	zipBytes := zipFile(t, "openvgdb.sqlite", readBytes(t, dbPath))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()

	root := t.TempDir()
	result, err := ensureSnapshot(root, server.URL)
	if err != nil {
		t.Fatalf("ensureSnapshot returned error: %v", err)
	}
	if !result.Downloaded {
		t.Fatalf("expected snapshot to be downloaded")
	}
	if _, err := os.Stat(filepath.Join(root, ".rommeta", "sources", "openvgdb", "openvgdb.sqlite")); err != nil {
		t.Fatalf("expected runtime sqlite snapshot: %v", err)
	}

	second, err := ensureSnapshot(root, server.URL)
	if err != nil {
		t.Fatalf("second ensureSnapshot returned error: %v", err)
	}
	if second.Downloaded {
		t.Fatalf("expected existing snapshot to be kept")
	}
}

func createOpenVGDBFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openvgdb.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("Open sqlite failed: %v", err)
	}
	defer db.Close()
	statements := []string{
		`create table SYSTEMS (systemID integer, systemName text, systemShortName text)`,
		`create table ROMs (romID integer, systemID integer, romHashCRC text, romHashMD5 text, romHashSHA1 text, romFileName text, romExtensionlessFileName text)`,
		`create table RELEASES (releaseID integer, romID integer, releaseTitleName text, TEMPregionLocalizedName text, releaseCoverFront text, releaseCoverBack text, releaseCoverCart text, releaseCoverDisc text, releaseDescription text, releaseDeveloper text, releasePublisher text, releaseGenre text, releaseDate text, releaseReferenceURL text, releaseReferenceImageURL text)`,
		`insert into SYSTEMS values (23, 'Nintendo 64', 'N64')`,
		`insert into ROMs values (1, 23, '363E7EE8', 'FECDCBBCD1A178A2F4B29B43FACF9F39', 'CFF7FB46E79D35F727E4E20CC4C13492ECD2A2D7', 'Star Fox 64 (USA).n64', 'Star Fox 64 (USA)')`,
		`insert into RELEASES values (10, 1, 'Star Fox 64', 'USA', 'https://example.test/front.jpg', '', '', '', 'A rail shooter in space.', 'Nintendo', 'Nintendo', 'Action,Shooter,Rail', 'Jul1, 1997', 'https://example.test/game', 'https://example.test/images')`,
		`insert into ROMs values (2, 23, '11111111', '11111111111111111111111111111111', '1111111111111111111111111111111111111111', 'Alpha Test (USA).n64', 'Alpha Test (USA)')`,
		`insert into RELEASES values (20, 2, 'Alpha Test', 'USA', '', '', '', '', 'First test game.', 'Dev', 'Pub', 'Action', '1997', '', '')`,
		`insert into ROMs values (3, 23, '22222222', '22222222222222222222222222222222', '2222222222222222222222222222222222222222', 'Beta Test (USA).n64', 'Beta Test (USA)')`,
		`insert into RELEASES values (30, 3, 'Beta Test', 'USA', '', '', '', '', 'Second test game.', 'Dev', 'Pub', 'Action', '1998', '', '')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("Exec failed for %q: %v", statement, err)
		}
	}
	return path
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	return contents
}

func zipFile(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "snapshot.zip")
	file, err := os.Create(tmp)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	writer := zip.NewWriter(file)
	member, err := writer.Create(name)
	if err != nil {
		t.Fatalf("Create zip member failed: %v", err)
	}
	if _, err := member.Write(contents); err != nil {
		t.Fatalf("Write zip member failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close zip writer failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close zip file failed: %v", err)
	}
	return readBytes(t, tmp)
}
