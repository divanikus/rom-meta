package launchbox

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchAssetsWritesPortablePlan(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Star Fox 64")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Star Fox 64"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"

sources:
  launchbox:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "https://images.launchbox-app.com/games/starfox/front.png"
              region: "North America"
`)

	planPath := filepath.Join(dataRoot, ".rommeta", "runs", "launchbox-assets.aria2")
	result, err := FetchAssets(AssetOptions{
		DataRoot:   dataRoot,
		PlanPath:   planPath,
		PlanFormat: "aria2",
	})
	if err != nil {
		t.Fatalf("FetchAssets returned error: %v", err)
	}
	if result.CandidateCount != 1 || result.PlannedDownloadCount != 1 || result.PlanItemCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}

	plan := readFileForTest(t, planPath)
	for _, expected := range []string{
		"https://images.launchbox-app.com/games/starfox/front.png",
		"dir=platforms/Nintendo - Nintendo 64/Star Fox 64/media",
		"out=box-front.north-america.launchbox.png",
	} {
		if !strings.Contains(plan, expected) {
			t.Fatalf("plan does not contain %q:\n%s", expected, plan)
		}
	}
	if strings.Contains(plan, dataRoot) || strings.Contains(plan, `\`) {
		t.Fatalf("plan should contain relative POSIX paths only:\n%s", plan)
	}
}

func TestFetchAssetsKeepsDifferentReleaseMediaSeparate(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Contra")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Contra"

releases:
  "Contra (USA)":
    title: "Contra"
  "Probotector (Europe)":
    title: "Probotector"

sources:
  launchbox:
    matches:
      - release: "Contra (USA)"
        media:
          box_front:
            - url: "https://images.launchbox-app.com/games/contra/front-us.jpg"
              region: "USA"
      - release: "Probotector (Europe)"
        media:
          box_front:
            - url: "https://images.launchbox-app.com/games/contra/front-eu.jpg"
              region: "Europe"
`)

	planPath := filepath.Join(dataRoot, ".rommeta", "runs", "launchbox-assets.aria2")
	result, err := FetchAssets(AssetOptions{
		DataRoot:   dataRoot,
		PlanPath:   planPath,
		PlanFormat: "aria2",
	})
	if err != nil {
		t.Fatalf("FetchAssets returned error: %v", err)
	}
	if result.CandidateCount != 2 || result.PlannedDownloadCount != 2 || result.PlanItemCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}

	plan := readFileForTest(t, planPath)
	for _, expected := range []string{
		"dir=platforms/Nintendo - Nintendo 64/Contra/media",
		"dir=platforms/Nintendo - Nintendo 64/Contra/media/Probotector (Europe)",
		"out=box-front.usa.launchbox.jpg",
		"out=box-front.europe.launchbox.jpg",
	} {
		if !strings.Contains(plan, expected) {
			t.Fatalf("plan does not contain %q:\n%s", expected, plan)
		}
	}
	if strings.Contains(plan, "Contra (USA)") {
		t.Fatalf("ordinary regional release should not create a media subfolder:\n%s", plan)
	}
	if strings.Contains(plan, dataRoot) || strings.Contains(plan, `\`) {
		t.Fatalf("plan should contain relative POSIX paths only:\n%s", plan)
	}
}

func TestFetchAssetsPlansNumberedCandidatesFromFirst(t *testing.T) {
	dataRoot := t.TempDir()
	gamePath := filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "Paper Mario")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_catalog.yaml"), "platform: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(dataRoot, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), "name: \"Nintendo - Nintendo 64\"\n")
	writeFileForTest(t, filepath.Join(gamePath, "game.yaml"), `title: "Paper Mario"

releases:
  "Paper Mario (USA)":
    title: "Paper Mario"

sources:
  launchbox:
    matches:
      - release: "Paper Mario (USA)"
        media:
          banner:
            - url: "https://images.launchbox-app.com/games/paper/banner-a.jpg"
            - url: "https://images.launchbox-app.com/games/paper/banner-b.jpg"
`)

	planPath := filepath.Join(dataRoot, ".rommeta", "runs", "launchbox-assets.aria2")
	result, err := FetchAssets(AssetOptions{
		DataRoot:   dataRoot,
		PlanPath:   planPath,
		PlanFormat: "aria2",
	})
	if err != nil {
		t.Fatalf("FetchAssets returned error: %v", err)
	}
	if result.CandidateCount != 2 || result.PlannedDownloadCount != 2 || result.PlanItemCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}

	plan := readFileForTest(t, planPath)
	for _, expected := range []string{
		"out=banner-01.launchbox.jpg",
		"out=banner-02.launchbox.jpg",
	} {
		if !strings.Contains(plan, expected) {
			t.Fatalf("plan does not contain %q:\n%s", expected, plan)
		}
	}
}

func TestFetchAssetsWritesRootSourcePolicyOnApply(t *testing.T) {
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
  launchbox:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "`+server.URL+`/front.jpg"
`)

	result, err := FetchAssets(AssetOptions{DataRoot: dataRoot, Apply: true})
	if err != nil {
		t.Fatalf("FetchAssets returned error: %v", err)
	}
	if result.DownloadedFileCount != 1 || result.FailedDownloadCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	policies := readFileForTest(t, filepath.Join(dataRoot, "source-policies.yaml"))
	if !strings.Contains(policies, "launchbox:") {
		t.Fatalf("source-policies.yaml does not contain launchbox source:\n%s", policies)
	}
	if _, err := os.Stat(filepath.Join(gamePath, "media", "rights.yaml")); !os.IsNotExist(err) {
		t.Fatalf("media/rights.yaml should not be created, stat err=%v", err)
	}
}

func TestGroupedPlusArtworkKeepsReleaseOwnership(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Nintendo - Nintendo DS", "Synth")
	writeFileForTest(t, filepath.Join(game, "game.yaml"), `title: Synth
releases:
  "Synth (USA)": {}
  "Synth+ (USA)": {}
  "Synth+ (Japan)": {}
sources:
  launchbox:
    matches:
      - releases: ["Synth (USA)"]
        media:
          box_front:
            - url: https://example.test/base.jpg
      - releases: ["Synth+ (USA)", "Synth+ (Japan)"]
        media:
          box_front:
            - url: https://example.test/plus.jpg
              region: World
`)
	plan := filepath.Join(root, "plan.aria2")
	result, err := FetchAssets(AssetOptions{DataRoot: root, PlanPath: plan, PlanFormat: "aria2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CandidateCount != 3 {
		t.Fatalf("lost edition ownership: %#v", result)
	}
	text := readFileForTest(t, plan)
	for _, release := range []string{"Synth+ (USA)", "Synth+ (Japan)"} {
		if !strings.Contains(text, "dir=platforms/Nintendo - Nintendo DS/Synth/media/"+release) {
			t.Fatalf("missing %s: %s", release, text)
		}
	}
}
