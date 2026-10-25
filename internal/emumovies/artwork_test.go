package emumovies

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchArtworkPlansPackageArchivesRelativeTargets(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()
	planPath := filepath.Join(root, ".rommeta", "runs", "emumovies-artwork.jsonl")

	result, err := FetchArtwork(ArtworkOptions{
		DataRoot:   root,
		FTPClient:  fake,
		PlanPath:   planPath,
		PlanFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("FetchArtwork returned error: %v", err)
	}
	if result.PackageCandidateCount != 4 || result.NeedsExtractorPackageCount != 2 || result.SelectedPackageCount != 2 || result.PlannedPackageCount != 2 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	if result.NeedsExtractorRARCount != 2 || result.NeedsExtractor7zCount != 0 {
		t.Fatalf("unexpected archive extractor breakdown: %#v", result)
	}

	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile plan failed: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, `"target":".rommeta/sources/emumovies/packages/Nintendo - Nintendo 64/Nintendo 64 (Logos)(No-Intro 1G1R)(EM 4.0).zip"`) {
		t.Fatalf("plan target was not relative POSIX package path:\n%s", text)
	}
	if strings.Contains(text, `D:\`) || strings.Contains(text, `\\`) {
		t.Fatalf("plan should not contain absolute Windows paths:\n%s", text)
	}
	if !strings.Contains(text, `ftps://files.emumovies.com/Official/Artwork/Nintendo%2064/Nintendo%2064%20%28Logos%29%28No-Intro%201G1R%29%28EM%204.0%29.zip`) {
		t.Fatalf("plan did not contain escaped FTP URL:\n%s", text)
	}
	if len(fake.downloads) != 0 {
		t.Fatalf("dry run should not download: %#v", fake.downloads)
	}
}

func TestFetchArtworkFiltersMediaType(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()
	planPath := filepath.Join(root, ".rommeta", "runs", "emumovies-box-front.jsonl")

	result, err := FetchArtwork(ArtworkOptions{
		DataRoot:   root,
		FTPClient:  fake,
		MediaTypes: []string{"box_front"},
		PlanPath:   planPath,
		PlanFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("FetchArtwork returned error: %v", err)
	}
	if result.SelectedPackageCount != 1 || result.PlannedPackageCount != 1 {
		t.Fatalf("expected one selected box-front package: %#v", result)
	}
	if len(result.MediaTypes) != 1 || result.MediaTypes[0] != "box-front" {
		t.Fatalf("unexpected media type filter: %#v", result.MediaTypes)
	}

	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile plan failed: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, `"media_type":"box-front"`) || !strings.Contains(text, "2D Boxes") {
		t.Fatalf("plan does not contain box-front package:\n%s", text)
	}
	if strings.Contains(text, "Logos") {
		t.Fatalf("plan should not contain unfiltered logo package:\n%s", text)
	}
}

func TestFetchArtworkFindsPlatformDirectoryFromRegistryCandidates(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	mustWrite(t, filepath.Join(root, "platforms", "NEC - PC Engine - TurboGrafx-16", "Bonk's Adventure", "game.yaml"), `title: "Bonk's Adventure"
default_release: "Bonk's Adventure (USA)"

releases:
  "Bonk's Adventure (USA)":
    title: "Bonk's Adventure"
    regions: ["USA"]
`)
	fake := newFakeArtworkFTP()
	fake.lists["Official/Artwork"] = []FTPEntry{
		{Name: "NEC PC Engine - Turbografx 16", IsDir: true},
	}
	fake.lists["Official/Artwork/NEC PC Engine - Turbografx 16"] = []FTPEntry{
		{Name: "NEC PC Engine - Turbografx 16 (Logos)(EM 4.0).zip", Size: 1024},
	}
	planPath := filepath.Join(root, ".rommeta", "runs", "emumovies-pce-artwork.jsonl")

	result, err := FetchArtwork(ArtworkOptions{
		DataRoot:   root,
		FTPClient:  fake,
		Platform:   "NEC - PC Engine - TurboGrafx-16",
		PlanPath:   planPath,
		PlanFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("FetchArtwork returned error: %v", err)
	}
	if result.FTPDirectory != "Official/Artwork/NEC PC Engine - Turbografx 16" || result.PlannedPackageCount != 1 {
		t.Fatalf("expected PCE artwork directory fallback, got %#v", result)
	}
	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile plan failed: %v", err)
	}
	if !strings.Contains(string(contents), `"source_path":"Official/Artwork/NEC PC Engine - Turbografx 16/NEC PC Engine - Turbografx 16 (Logos)(EM 4.0).zip"`) {
		t.Fatalf("plan did not use actual artwork directory:\n%s", contents)
	}
}

func TestFetchArtworkApplyDownloadsExtractsAndMaterializes(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()

	result, err := FetchArtwork(ArtworkOptions{
		DataRoot:  root,
		FTPClient: fake,
		Apply:     true,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("FetchArtwork returned error: %v", err)
	}
	if result.DownloadedPackageCount != 1 || result.ExtractedPackageCount != 1 || result.ExtractedFileCount != 2 {
		t.Fatalf("expected one downloaded/extracted package: result=%#v downloads=%#v", result, fake.downloads)
	}
	if result.IndexedArtworkFileCount != 2 || result.MatchedArtworkFileCount != 1 || result.UnmatchedArtworkFileCount != 1 || result.WrittenArtworkFileCount != 1 {
		t.Fatalf("unexpected artwork indexing/materialization counts: %#v", result)
	}
	if result.UnmatchedManifestPath == "" {
		t.Fatalf("expected unmatched artwork report path")
	}
	unmatched, err := os.ReadFile(result.UnmatchedManifestPath)
	if err != nil {
		t.Fatalf("ReadFile unmatched report failed: %v", err)
	}
	unmatchedText := string(unmatched)
	if !strings.Contains(unmatchedText, "\"file_name\":\"Unknown Game (USA).png\"") || !strings.Contains(unmatchedText, "\"package_target\":\".rommeta/sources/emumovies/packages/Nintendo - Nintendo 64/Nintendo 64 (Logos)(No-Intro 1G1R)(EM 4.0).zip\"") {
		t.Fatalf("unexpected unmatched report contents:\n%s", unmatchedText)
	}
	if strings.Contains(unmatchedText, "ftps://") || strings.Contains(unmatchedText, "\\") {
		t.Fatalf("unmatched report should avoid URLs and Windows paths:\n%s", unmatchedText)
	}

	target := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "GoldenEye 007", "media", "logo.usa.emumovies.png")
	if data, err := os.ReadFile(target); err != nil || string(data) != "logo" {
		t.Fatalf("unexpected materialized artwork: data=%q err=%v", data, err)
	}

	policy := filepath.Join(root, "source-policies.yaml")
	if contents, err := os.ReadFile(policy); err != nil || !strings.Contains(string(contents), "emumovies:") {
		t.Fatalf("expected source policy to be present: err=%v contents=%q", err, contents)
	}
}

func TestFetchArtworkApplyUsesExistingDownloadedPackage(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()
	existingPackage := filepath.Join(root, ".rommeta", "sources", "emumovies", "packages", "Nintendo - Nintendo 64", "Nintendo 64 (Logos)(No-Intro 1G1R)(EM 4.0).zip")
	if err := writeTestZip(existingPackage, map[string]string{
		"GoldenEye 007 (USA).png": "logo",
	}); err != nil {
		t.Fatalf("writeTestZip failed: %v", err)
	}

	result, err := FetchArtwork(ArtworkOptions{
		DataRoot:   root,
		FTPClient:  fake,
		MediaTypes: []string{"logo"},
		Apply:      true,
	})
	if err != nil {
		t.Fatalf("FetchArtwork returned error: %v", err)
	}
	if result.ExistingPackageCount != 1 || result.DownloadedPackageCount != 0 || len(fake.downloads) != 0 {
		t.Fatalf("expected existing package to be reused without download: result=%#v downloads=%#v", result, fake.downloads)
	}
	if result.ExtractedPackageCount != 1 || result.WrittenArtworkFileCount != 1 {
		t.Fatalf("expected existing package to be extracted and materialized: %#v", result)
	}
}

func TestArtworkPackagesSelectsMultipartRARWhenExtractorAvailable(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()
	games, _, err := readCatalogGames(root, "")
	if err != nil {
		t.Fatalf("readCatalogGames returned error: %v", err)
	}

	packages, _, _, candidateCount, needsExtractorCount, needsExtractorByType, err := artworkPackages(fake, sourceSettings{
		FTPHost:   "files.emumovies.com",
		URLScheme: "ftps",
	}, archiveExtractor{Path: "7z", Kind: "7z"}, root, games)
	if err != nil {
		t.Fatalf("artworkPackages returned error: %v", err)
	}
	if candidateCount != 4 || needsExtractorCount != 0 || needsExtractorByType["rar"] != 0 || len(packages) != 3 {
		t.Fatalf("unexpected package selection: candidates=%d needsExtractor=%d byType=%#v packages=%#v", candidateCount, needsExtractorCount, needsExtractorByType, packages)
	}
	var back artworkPackage
	for _, pkg := range packages {
		if pkg.MediaType == "box-back" {
			back = pkg
			break
		}
	}
	if back.Name != "Nintendo 64 (2D Boxes-Back)(No-Intro 1G1R)(EM 4.0).part1.rar" || back.ArchiveType != "rar" || len(back.Parts) != 2 {
		t.Fatalf("expected multipart RAR back package, got %#v", back)
	}
	if back.Parts[0].Name != "Nintendo 64 (2D Boxes-Back)(No-Intro 1G1R)(EM 4.0).part1.rar" || back.Parts[1].Name != "Nintendo 64 (2D Boxes-Back)(No-Intro 1G1R)(EM 4.0).part2.rar" {
		t.Fatalf("unexpected multipart order: %#v", back.Parts)
	}
}

func TestArtworkPackagesPreferFullCoverageOver1G1R(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeArtworkFTP()
	fake.lists["Official/Artwork/Nintendo 64"] = append(fake.lists["Official/Artwork/Nintendo 64"],
		FTPEntry{Name: "Nintendo 64 (Logos)(EM 4.0).rar", Size: 2048},
	)
	games, _, err := readCatalogGames(root, "")
	if err != nil {
		t.Fatalf("readCatalogGames returned error: %v", err)
	}

	packages, _, _, _, _, _, err := artworkPackages(fake, sourceSettings{
		FTPHost:   "files.emumovies.com",
		URLScheme: "ftps",
	}, archiveExtractor{Path: "7z", Kind: "7z"}, root, games)
	if err != nil {
		t.Fatalf("artworkPackages returned error: %v", err)
	}

	var logo artworkPackage
	for _, pkg := range packages {
		if pkg.MediaType == "logo" {
			logo = pkg
			break
		}
	}
	if logo.Name != "Nintendo 64 (Logos)(EM 4.0).rar" {
		t.Fatalf("expected full-coverage logo package to beat 1G1R fallback, got %#v", logo)
	}
}

func TestAssignArtworkTargetsNumbersBeforeSourceSuffix(t *testing.T) {
	gamePath := filepath.Join(t.TempDir(), "platforms", "Nintendo - Nintendo 64", "GoldenEye 007")
	candidates := []artworkFileCandidate{
		{GamePath: gamePath, Release: "GoldenEye 007 (USA)", ReleaseCount: 1, MediaType: "logo", FileName: "front.png"},
		{GamePath: gamePath, Release: "GoldenEye 007 (USA)", ReleaseCount: 1, MediaType: "logo", FileName: "alt.png"},
		{GamePath: gamePath, Release: "GoldenEye 007 (USA)", ReleaseCount: 1, MediaType: "logo", FileName: "clear.png"},
	}

	assignArtworkTargets(candidates)

	expected := []string{
		filepath.Join(gamePath, "media", "logo.usa.emumovies.png"),
		filepath.Join(gamePath, "media", "logo-02.usa.emumovies.png"),
		filepath.Join(gamePath, "media", "logo-03.usa.emumovies.png"),
	}
	for index, target := range expected {
		if candidates[index].Target != target {
			t.Fatalf("unexpected target %d: got %q want %q", index, candidates[index].Target, target)
		}
	}
}

func TestAssignArtworkTargetsKeepsMultipleReleasesSeparate(t *testing.T) {
	gamePath := filepath.Join(t.TempDir(), "platforms", "Sega - 32X", "Example Game")
	candidates := []artworkFileCandidate{
		{Game: "Example Game", GamePath: gamePath, Release: "Example Game (USA)", ReleaseCount: 2, MediaType: "box-front", FileName: "front.png"},
		{Game: "Example Game", GamePath: gamePath, Release: "Example Game - Special Edition (USA)", ReleaseCount: 2, MediaType: "box-front", FileName: "special.png"},
	}

	assignArtworkTargets(candidates)

	expected := []string{
		filepath.Join(gamePath, "media", "box-front.usa.emumovies.png"),
		filepath.Join(gamePath, "media", "Example Game - Special Edition (USA)", "box-front.usa.emumovies.png"),
	}
	for index, target := range expected {
		if candidates[index].Target != target {
			t.Fatalf("unexpected target %d: got %q want %q", index, candidates[index].Target, target)
		}
	}
}

type fakeArtworkFTP struct {
	lists     map[string][]FTPEntry
	downloads []string
}

func newFakeArtworkFTP() *fakeArtworkFTP {
	return &fakeArtworkFTP{
		lists: map[string][]FTPEntry{
			"Official/Artwork/Nintendo 64": {
				{Name: "Nintendo 64 (Logos)(No-Intro 1G1R)(EM 4.0).zip", Size: 1024},
				{Name: "Nintendo 64 (2D Boxes)(No-Intro 1G1R)(EM 4.0).zip", Size: 2048},
				{Name: "Nintendo 64 (2D Boxes-Back)(No-Intro 1G1R)(EM 4.0).part1.rar", Size: 4096},
				{Name: "Nintendo 64 (2D Boxes-Back)(No-Intro 1G1R)(EM 4.0).part2.rar", Size: 4096},
				{Name: "readme.txt", Size: 100},
			},
		},
	}
}

func (fake *fakeArtworkFTP) List(path string) ([]FTPEntry, error) {
	entries, ok := fake.lists[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (fake *fakeArtworkFTP) Download(path, target string) error {
	fake.downloads = append(fake.downloads, path)
	files := map[string]string{
		"GoldenEye 007 (USA).png": "logo",
		"Unknown Game (USA).png":  "unknown",
	}
	if strings.Contains(path, "2D Boxes") {
		files = map[string]string{"GoldenEye 007 (USA).jpg": "box"}
	}
	return writeTestZip(target, files)
}

func writeTestZip(target string, files map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(out)
	for name, contents := range files {
		file, err := writer.Create(name)
		if err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
