package openvgdb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchAssetsPlansDownloadsAndSkipsExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg bytes"))
	}))
	defer server.Close()

	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Star Fox 64"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"

sources:
  openvgdb:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "`+server.URL+`/front.jpg"
              region: "USA"
`)

	dryRun, err := FetchAssets(AssetOptions{DataRoot: dataRoot})
	if err != nil {
		t.Fatalf("FetchAssets dry run returned error: %v", err)
	}
	if dryRun.CandidateCount != 1 || dryRun.PlannedDownloadCount != 1 || dryRun.DownloadedFileCount != 0 {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}

	result, err := FetchAssets(AssetOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("FetchAssets apply returned error: %v", err)
	}
	if result.DownloadedFileCount != 1 || result.FailedDownloadCount != 0 {
		t.Fatalf("unexpected apply result: %#v", result)
	}
	assetPath := filepath.Join(gamePath, "media", "box-front.usa.openvgdb.jpg")
	if contents, err := os.ReadFile(assetPath); err != nil || string(contents) != "jpeg bytes" {
		t.Fatalf("unexpected downloaded asset contents=%q err=%v", contents, err)
	}
	policies := readBytes(t, filepath.Join(dataRoot, "source-policies.yaml"))
	if !strings.Contains(string(policies), "openvgdb:") {
		t.Fatalf("source-policies.yaml does not contain openvgdb source:\n%s", policies)
	}
	if _, err := os.Stat(filepath.Join(gamePath, "media", "rights.yaml")); !os.IsNotExist(err) {
		t.Fatalf("media/rights.yaml should not be created, stat err=%v", err)
	}

	second, err := FetchAssets(AssetOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("second FetchAssets apply returned error: %v", err)
	}
	if second.ExistingFileCount != 1 || second.DownloadedFileCount != 0 {
		t.Fatalf("expected existing file to be skipped, got %#v", second)
	}
}

func TestFetchAssetsHonorsLimit(t *testing.T) {
	dataRoot := t.TempDir()
	planPath := filepath.Join(t.TempDir(), "assets.aria2")
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Star Fox 64"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"

sources:
  openvgdb:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "https://example.test/front.jpg"
              region: "USA"
          box_back:
            - url: "https://example.test/back.jpg"
              region: "USA"
`)

	result, err := FetchAssets(AssetOptions{DataRoot: dataRoot, Limit: 1, PlanPath: planPath, PlanFormat: "aria2"})
	if err != nil {
		t.Fatalf("FetchAssets returned error: %v", err)
	}
	if result.CandidateCount != 2 || result.PlannedDownloadCount != 1 || result.SkippedByLimitCount != 1 {
		t.Fatalf("unexpected limited result: %#v", result)
	}
	if result.PlanItemCount != 1 || result.PlanFormat != "aria2" {
		t.Fatalf("unexpected plan result: %#v", result)
	}
	plan := readBytes(t, planPath)
	if !strings.Contains(string(plan), "https://example.test/back.jpg") && !strings.Contains(string(plan), "https://example.test/front.jpg") {
		t.Fatalf("plan does not contain a download URL:\n%s", plan)
	}
	if !strings.Contains(string(plan), "dir=") || !strings.Contains(string(plan), "out=") {
		t.Fatalf("plan does not look like aria2 input:\n%s", plan)
	}
	planText := string(plan)
	if strings.Contains(planText, dataRoot) {
		t.Fatalf("plan should use paths relative to data root, got:\n%s", planText)
	}
	if strings.Contains(planText, "\\") {
		t.Fatalf("plan should use POSIX-style path separators, got:\n%s", planText)
	}
	expectedDir := "dir=platforms/Nintendo - Nintendo 64/Star Fox 64/media"
	if !strings.Contains(planText, expectedDir) {
		t.Fatalf("plan does not contain expected relative dir %q:\n%s", expectedDir, planText)
	}
	if !strings.Contains(planText, "out=box-back.usa.openvgdb.jpg") {
		t.Fatalf("plan does not contain expected source-specific output filename:\n%s", planText)
	}

	jsonlPath := filepath.Join(t.TempDir(), "assets.jsonl")
	if _, err := FetchAssets(AssetOptions{DataRoot: dataRoot, Limit: 1, PlanPath: jsonlPath, PlanFormat: "jsonl"}); err != nil {
		t.Fatalf("FetchAssets JSONL plan returned error: %v", err)
	}
	jsonl := string(readBytes(t, jsonlPath))
	if strings.Contains(jsonl, dataRoot) || strings.Contains(jsonl, "\\") {
		t.Fatalf("JSONL plan should use relative POSIX-style paths, got:\n%s", jsonl)
	}
	expectedTarget := `"target":"platforms/Nintendo - Nintendo 64/Star Fox 64/media/box-back.usa.openvgdb.jpg"`
	if !strings.Contains(jsonl, expectedTarget) {
		t.Fatalf("JSONL plan does not contain expected target %s:\n%s", expectedTarget, jsonl)
	}
}
