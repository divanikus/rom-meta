package sourceassets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/taskprogress"
)

func TestFetchPlansIGDBMediaWithRelativePaths(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
sources:
  igdb:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "https://images.igdb.com/igdb/image/upload/t_cover_big/co1234.jpg"
              region: "USA"
              source_id: "co1234"
          screenshot:
            - url: "https://images.igdb.com/igdb/image/upload/t_screenshot_big/sc1234.jpg"
              source_id: "sc1234"
`)
	planPath := filepath.Join(root, ".rommeta", "runs", "igdb-assets.aria2")

	var progressStages []string
	result, err := Fetch(Options{
		DataRoot:   root,
		Source:     "igdb",
		PlanPath:   planPath,
		PlanFormat: "aria2",
		Progress: func(snapshot taskprogress.Snapshot) {
			progressStages = appendUniqueString(progressStages, snapshot.Stage)
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if result.CandidateCount != 2 || result.PlannedDownloadCount != 2 || result.PlanItemCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	for _, stage := range []string{"scanning catalog", "planning downloads", "writing plan"} {
		if !stringSliceContains(progressStages, stage) {
			t.Fatalf("progress did not include %q: %#v", stage, progressStages)
		}
	}
	if stringSliceContains(progressStages, "reading media URLs") || stringSliceContains(progressStages, "planning") {
		t.Fatalf("progress should not expose a separate URL-reading/planning pass: %#v", progressStages)
	}
	plan := readFile(t, planPath)
	for _, expected := range []string{
		"https://images.igdb.com/igdb/image/upload/t_cover_big/co1234.jpg",
		"dir=platforms/Nintendo - Nintendo 64/Star Fox 64/media",
		"out=box-front.usa.igdb.jpg",
		"out=screenshot.igdb.jpg",
	} {
		if !strings.Contains(plan, expected) {
			t.Fatalf("plan does not contain %q:\n%s", expected, plan)
		}
	}
	if strings.Contains(plan, `\`) || strings.Contains(plan, root) {
		t.Fatalf("plan should use relative POSIX paths:\n%s", plan)
	}
}

func TestDownloadLabelOmitsPlatform(t *testing.T) {
	label := downloadLabel(assetCandidate{
		Platform: "Nintendo - Nintendo Entertainment System",
		Game:     "10-Yard Fight",
		Release:  "10-Yard Fight (USA)",
		Target:   filepath.Join("root", "media", "box-front.launchbox.jpg"),
	})

	if strings.Contains(label, "Nintendo - Nintendo Entertainment System") {
		t.Fatalf("download label should not repeat the platform: %q", label)
	}
	if label != "10-Yard Fight / 10-Yard Fight (USA) / box-front.launchbox.jpg" {
		t.Fatalf("unexpected download label: %q", label)
	}
}

func TestDownloadAssetErrorIncludesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary outage", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := downloadAsset(server.URL+"/box.jpg", server.URL+"/box.jpg", filepath.Join(t.TempDir(), "box.jpg"), nil, "box.jpg", 1, 1)
	if err == nil {
		t.Fatalf("expected download error")
	}
	if !strings.Contains(err.Error(), server.URL+"/box.jpg") || !strings.Contains(err.Error(), "500 Internal Server Error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadAssetErrorUsesPublicSanitizedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary outage", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := downloadAsset(server.URL+"/box.jpg?media=box&softname=RomMeta&ssid=leaked-user&sspassword=leaked-pass", server.URL+"/box.jpg?media=box", filepath.Join(t.TempDir(), "box.jpg"), nil, "box.jpg", 1, 1)
	if err == nil {
		t.Fatalf("expected download error")
	}
	if !strings.Contains(err.Error(), server.URL+"/box.jpg?media=box") || strings.Contains(err.Error(), "leaked") || strings.Contains(err.Error(), "softname") || strings.Contains(err.Error(), "ssid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchPlansSanitizeScreenScraperMediaURLs(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, ".rommeta", "runs", "screenscraper-assets.jsonl")
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
sources:
  screenscraper:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "https://cdn.example.test/front.jpg?media=box&softname=RomMeta&ssid=leaked-user&sspassword=leaked-pass&devid=leaked-dev&devpassword=leaked-devpass"
              region: "Jp"
`)

	result, err := Fetch(Options{
		DataRoot:   root,
		Source:     "screenscraper",
		PlanPath:   planPath,
		PlanFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.PlanItemCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	plan := readFile(t, planPath)
	if !strings.Contains(plan, "https://cdn.example.test/front.jpg?media=box") {
		t.Fatalf("plan missing sanitized URL:\n%s", plan)
	}
	if !strings.Contains(plan, `"region":"Japan"`) || strings.Contains(plan, `"region":"Jp"`) {
		t.Fatalf("plan missing normalized region:\n%s", plan)
	}
	for _, secret := range []string{"softname", "ssid", "sspassword", "devid", "devpassword", "leaked"} {
		if strings.Contains(plan, secret) {
			t.Fatalf("plan leaked %q:\n%s", secret, plan)
		}
	}
}

func TestFetchApplySignsScreenScraperMediaURLOnlyForRequest(t *testing.T) {
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RawQuery
		query := r.URL.Query()
		for key, want := range map[string]string{
			"media":       "box",
			"ssid":        "user",
			"sspassword":  "pass",
			"devid":       "dev",
			"devpassword": "devpass",
		} {
			if got := query.Get(key); got != want {
				t.Fatalf("query %s = %q, want %q (raw %s)", key, got, want, r.URL.RawQuery)
			}
		}
		if got := query.Get("softname"); got != "" {
			t.Fatalf("media download query should not include softname, got %q (raw %s)", got, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()

	root := t.TempDir()
	writeScreenScraperConfig(t, root)
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
sources:
  screenscraper:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "`+server.URL+`/front.jpg?media=box&softname=RomMeta&ssid=leaked-user&sspassword=leaked-pass"
`)

	result, err := Fetch(Options{
		DataRoot: root,
		Source:   "screenscraper",
		Apply:    true,
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.DownloadedFileCount != 1 || strings.Contains(requested, "leaked") {
		t.Fatalf("unexpected result/request: result=%#v query=%s", result, requested)
	}
	target := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.screenscraper.jpg")
	if contents := readFile(t, target); contents != "image" {
		t.Fatalf("unexpected downloaded contents: %q", contents)
	}
}

func appendUniqueString(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestFetchKeepsRegionalReleaseMediaGameWideWhenURLsDiffer(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Contra", `title: "Contra"
releases:
  "Contra (Europe)":
    title: "Contra"
  "Contra (USA)":
    title: "Contra"
sources:
  igdb:
    matches:
      - release: "Contra (Europe)"
        media:
          box_front:
            - url: "https://images.example.test/contra-eu.jpg"
              region: "Europe"
      - release: "Contra (USA)"
        media:
          box_front:
            - url: "https://images.example.test/contra-us.jpg"
              region: "USA"
`)
	planPath := filepath.Join(root, ".rommeta", "runs", "igdb-assets.jsonl")

	result, err := Fetch(Options{DataRoot: root, Source: "igdb", PlanPath: planPath, PlanFormat: "jsonl"})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if result.PlannedDownloadCount != 2 {
		t.Fatalf("unexpected planned count: %#v", result)
	}
	plan := readFile(t, planPath)
	if !strings.Contains(plan, `"target":"platforms/Nintendo - Nintendo 64/Contra/media/box-front-01.europe.igdb.jpg"`) {
		t.Fatalf("missing Europe game-wide target:\n%s", plan)
	}
	if !strings.Contains(plan, `"target":"platforms/Nintendo - Nintendo 64/Contra/media/box-front-02.usa.igdb.jpg"`) {
		t.Fatalf("missing USA game-wide target:\n%s", plan)
	}
	if strings.Contains(plan, "Contra (Europe)") || strings.Contains(plan, "Contra (USA)") {
		t.Fatalf("regional releases should not create media subfolders:\n%s", plan)
	}
}

func TestFetchCollapsesIdenticalURLsToGameWideMedia(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Contra", `title: "Contra"
releases:
  "Contra (Europe)":
    title: "Contra"
  "Contra (USA)":
    title: "Contra"
sources:
  igdb:
    matches:
      - release: "Contra (Europe)"
        media:
          box_front:
            - url: "https://images.example.test/contra.jpg"
      - release: "Contra (USA)"
        media:
          box_front:
            - url: "https://images.example.test/contra.jpg"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Contra"), "igdb")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected one collapsed candidate, got %#v", candidates)
	}
	if strings.Contains(candidates[0].Target, "Contra (USA)") || strings.Contains(candidates[0].Target, "Contra (Europe)") {
		t.Fatalf("expected game-wide target, got %q", candidates[0].Target)
	}
	if filepath.Base(candidates[0].Target) != "box-front.igdb.jpg" {
		t.Fatalf("unexpected target: %q", candidates[0].Target)
	}
}

func TestFetchKeepsSingleReleaseMultipleMediaCandidatesGameWide(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Sega - 32X", "BC Racers", `title: "BC Racers"
releases:
  "BC Racers (USA)":
    title: "BC Racers"
sources:
  launchbox:
    matches:
      - release: "BC Racers (USA)"
        media:
          box_front:
            - url: "https://images.example.test/bc-racers-front.jpg"
              region: "North America"
            - url: "https://images.example.test/bc-racers-front-reconstructed.jpg"
              region: "World"
          screenshot:
            - url: "https://images.example.test/bc-racers-shot-1.png"
            - url: "https://images.example.test/bc-racers-shot-2.png"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Sega - 32X", "BC Racers"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 4 {
		t.Fatalf("expected four candidates, got %#v", candidates)
	}
	targets := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Release != "" {
			t.Fatalf("single-release media candidates should be game-wide, got %#v", candidate)
		}
		if strings.Contains(candidate.Target, "BC Racers (USA)") {
			t.Fatalf("single-release media should not write release folders, got %q", candidate.Target)
		}
		targets = append(targets, candidate.Target)
	}
	text := strings.Join(targets, "\n")
	for _, expected := range []string{
		"box-front-01.north-america.launchbox.jpg",
		"box-front-02.world.launchbox.jpg",
		"screenshot-01.launchbox.png",
		"screenshot-02.launchbox.png",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing target %q in:\n%s", expected, text)
		}
	}
}

func TestFetchKeepsSingleScopedCandidateReleaseSpecificWhenGameHasMultipleReleases(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Sega - 32X", "Example Game", `title: "Example Game"
releases:
  "Example Game (USA)":
    title: "Example Game"
  "Example Game - Special Edition (USA)":
    title: "Example Game - Special Edition"
sources:
  launchbox:
    matches:
      - release: "Example Game - Special Edition (USA)"
        media:
          box_front:
            - url: "https://images.example.test/example-special-front.jpg"
              region: "USA"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Sega - 32X", "Example Game"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %#v", candidates)
	}
	if candidates[0].Release != "Example Game - Special Edition (USA)" {
		t.Fatalf("expected release-specific candidate, got %#v", candidates[0])
	}
	expected := filepath.Join(root, "platforms", "Sega - 32X", "Example Game", "media", "Example Game - Special Edition (USA)", "box-front.usa.launchbox.jpg")
	if candidates[0].Target != expected {
		t.Fatalf("unexpected target: got %q want %q", candidates[0].Target, expected)
	}
}

func TestFetchNumbersReleaseSpecificMediaPerTargetFolder(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "NEC - PC Engine - TurboGrafx-16", "Final Soldier", `title: "Final Soldier"
releases:
  "Final Soldier (Japan) (En)":
    title: "Final Soldier"
  "Final Soldier - Special Version (Japan) (En)":
    title: "Final Soldier - Special Version"
sources:
  launchbox:
    matches:
      - release: "Final Soldier (Japan) (En)"
        media:
          box_front:
            - url: "https://images.example.test/final-soldier-jp.jpg"
              region: "Japan"
            - url: "https://images.example.test/final-soldier-na.jpg"
              region: "North America"
      - release: "Final Soldier - Special Version (Japan) (En)"
        media:
          box_3d:
            - url: "https://images.example.test/final-soldier-special-3d.png"
              region: "North America"
          box_front:
            - url: "https://images.example.test/final-soldier-special-jp.png"
              region: "Japan"
            - url: "https://images.example.test/final-soldier-special-na.png"
              region: "North America"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "NEC - PC Engine - TurboGrafx-16", "Final Soldier"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 5 {
		t.Fatalf("expected five candidates, got %#v", candidates)
	}
	targets := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		targets = append(targets, candidate.Target)
	}
	text := strings.Join(targets, "\n")
	for _, expected := range []string{
		"media" + string(filepath.Separator) + "box-front-01.japan.launchbox.jpg",
		"media" + string(filepath.Separator) + "box-front-02.north-america.launchbox.jpg",
		"media" + string(filepath.Separator) + "Final Soldier - Special Version (Japan) (En)" + string(filepath.Separator) + "box-3d.north-america.launchbox.png",
		"media" + string(filepath.Separator) + "Final Soldier - Special Version (Japan) (En)" + string(filepath.Separator) + "box-front-01.japan.launchbox.png",
		"media" + string(filepath.Separator) + "Final Soldier - Special Version (Japan) (En)" + string(filepath.Separator) + "box-front-02.north-america.launchbox.png",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing target %q in:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Special Version (Japan) (En)"+string(filepath.Separator)+"box-front-03") {
		t.Fatalf("release-specific media should be numbered per target folder:\n%s", text)
	}
}

func TestFetchDoesNotDeduplicateIdenticalURLsAcrossGameFolders(t *testing.T) {
	root := t.TempDir()
	for _, game := range []string{"Space Harrier", "Space Harrier (Beta)"} {
		writeGame(t, root, "Sega - 32X", game, `title: "`+game+`"
releases:
  "`+game+`":
    title: "Space Harrier"
sources:
  launchbox:
    matches:
      - release: "`+game+`"
        media:
          box_front:
            - url: "https://images.example.test/space-harrier-front.jpg"
`)
	}
	planPath := filepath.Join(root, ".rommeta", "runs", "launchbox-assets.jsonl")

	result, err := Fetch(Options{DataRoot: root, Source: "launchbox", PlanPath: planPath, PlanFormat: "jsonl"})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if result.CandidateCount != 2 || result.PlannedDownloadCount != 2 || result.DuplicateURLCount != 0 || result.PlanItemCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	plan := readFile(t, planPath)
	if got := strings.Count(plan, "https://images.example.test/space-harrier-front.jpg"); got != 2 {
		t.Fatalf("expected same URL to be planned once per game folder, got %d occurrences:\n%s", got, plan)
	}
}

func TestFetchDoesNotDedupeSameURLAcrossMediaTypes(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Sega - 32X", "Space Harrier", `title: "Space Harrier"
releases:
  "Space Harrier":
    title: "Space Harrier"
sources:
  launchbox:
    matches:
      - release: "Space Harrier"
        media:
          box_front:
            - url: "https://images.example.test/shared-image.jpg"
          title_screen:
            - url: "https://images.example.test/shared-image.jpg"
`)
	planPath := filepath.Join(root, ".rommeta", "runs", "launchbox-assets.jsonl")

	result, err := Fetch(Options{DataRoot: root, Source: "launchbox", PlanPath: planPath, PlanFormat: "jsonl"})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if result.CandidateCount != 2 || result.PlannedDownloadCount != 2 || result.DuplicateURLCount != 0 || result.PlanItemCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	plan := readFile(t, planPath)
	if !strings.Contains(plan, `"media_type":"box_front"`) || !strings.Contains(plan, `"media_type":"title_screen"`) {
		t.Fatalf("plan should keep separate media type rows:\n%s", plan)
	}
}

func TestFetchReadsGroupedReleaseMedia(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Contra", `title: "Contra"
releases:
  "Contra (Europe)":
    title: "Contra"
  "Contra (USA)":
    title: "Contra"
sources:
  launchbox:
    matches:
      - releases: ["Contra (Europe)", "Contra (USA)"]
        media:
          box_front:
            - url: "https://images.example.test/contra.jpg"
              region: "World"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Contra"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected one collapsed candidate, got %#v", candidates)
	}
	if filepath.Base(candidates[0].Target) != "box-front.world.launchbox.jpg" {
		t.Fatalf("unexpected target: %q", candidates[0].Target)
	}
}

func TestFetchDeduplicatesGroupedReleaseMediaByURL(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Sega - 32X", "Doom", `title: "Doom"
releases:
  "Doom (Europe)":
    title: "Doom"
  "Doom (USA)":
    title: "Doom"
sources:
  launchbox:
    matches:
      - releases: ["Doom (Europe)", "Doom (USA)"]
        media:
          box_front:
            - url: "https://images.example.test/doom-eu.jpg"
              region: "Europe"
            - url: "https://images.example.test/doom-us.jpg"
              region: "USA"
          screenshot:
            - url: "https://images.example.test/doom-shot.png"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Sega - 32X", "Doom"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 3 {
		t.Fatalf("expected grouped release media to collapse to three URL candidates, got %#v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Release != "" {
			t.Fatalf("grouped release media should become game-wide candidates, got %#v", candidate)
		}
		if strings.Contains(candidate.Target, "Doom (Europe)") || strings.Contains(candidate.Target, "Doom (USA)") {
			t.Fatalf("grouped release media should not write release folders, got %q", candidate.Target)
		}
	}
	targets := strings.Join([]string{candidates[0].Target, candidates[1].Target, candidates[2].Target}, "\n")
	for _, expected := range []string{
		"box-front-01.europe.launchbox.jpg",
		"box-front-02.usa.launchbox.jpg",
		"screenshot.launchbox.png",
	} {
		if !strings.Contains(targets, expected) {
			t.Fatalf("missing target %q in:\n%s", expected, targets)
		}
	}
}

func TestFetchReadsCompactReleaseMediaOverrides(t *testing.T) {
	root := t.TempDir()
	writeGame(t, root, "Sega - 32X", "Doom", `title: "Doom"
releases:
  "Doom (Europe)":
    title: "Doom"
  "Doom (USA)":
    title: "Doom"
sources:
  launchbox:
    matches:
      - database_id: "100"
        title: "Doom"
        releases:
          - release: "Doom (Europe)"
            media:
              box_front:
                - url: "https://images.example.test/doom-eu.jpg"
                  region: "Europe"
          - release: "Doom (USA)"
            media:
              box_front:
                - url: "https://images.example.test/doom-us.jpg"
                  region: "USA"
        media:
          screenshot:
            - url: "https://images.example.test/doom-shot.jpg"
`)

	candidates, err := readGameAssetCandidates(filepath.Join(root, "platforms", "Sega - 32X", "Doom"), "launchbox")
	if err != nil {
		t.Fatalf("readGameAssetCandidates returned error: %v", err)
	}

	if len(candidates) != 3 {
		t.Fatalf("expected three candidates, got %#v", candidates)
	}
	targets := strings.Join([]string{candidates[0].Target, candidates[1].Target, candidates[2].Target}, "\n")
	for _, expected := range []string{
		"media" + string(filepath.Separator) + "box-front-01.europe.launchbox.jpg",
		"media" + string(filepath.Separator) + "box-front-02.usa.launchbox.jpg",
		"media" + string(filepath.Separator) + "screenshot.launchbox.jpg",
	} {
		if !strings.Contains(targets, expected) {
			t.Fatalf("missing target %q in:\n%s", expected, targets)
		}
	}
	for _, unexpected := range []string{"Doom (Europe)", "Doom (USA)"} {
		if strings.Contains(targets, unexpected) {
			t.Fatalf("regional releases should not create media subfolders:\n%s", targets)
		}
	}
}

func TestFetchApplyDownloadsAndEnsuresSourcePolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()

	root := t.TempDir()
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
sources:
  igdb:
    matches:
      - release: "Star Fox 64 (USA)"
        media:
          box_front:
            - url: "`+server.URL+`/front.jpg"
`)

	result, err := Fetch(Options{
		DataRoot:    root,
		Source:      "igdb",
		Apply:       true,
		PolicyScope: "review",
		PolicyNote:  "Imported from IGDB media URLs; verify rights before sharing.",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if result.DownloadedFileCount != 1 || result.FailedDownloadCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.ManifestPath == "" || result.ManifestItemCount != 1 {
		t.Fatalf("expected one apply manifest item, got %#v", result)
	}
	assetPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "media", "box-front.igdb.jpg")
	if contents := readFile(t, assetPath); contents != "image" {
		t.Fatalf("unexpected downloaded contents: %q", contents)
	}
	manifest := readFile(t, result.ManifestPath)
	for _, expected := range []string{
		`"status":"downloaded"`,
		`"` + server.URL + `/front.jpg"`,
		`"target":"platforms/Nintendo - Nintendo 64/Star Fox 64/media/box-front.igdb.jpg"`,
	} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("manifest does not contain %q:\n%s", expected, manifest)
		}
	}
	policies := readFile(t, filepath.Join(root, "source-policies.yaml"))
	if !strings.Contains(policies, "igdb:") || !strings.Contains(policies, "Imported from IGDB media URLs") {
		t.Fatalf("source policy missing IGDB entry:\n%s", policies)
	}
}

func writeGame(t *testing.T, root, platform, game, contents string) {
	t.Helper()
	path := filepath.Join(root, "platforms", platform, game, "game.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func writeScreenScraperConfig(t *testing.T, root string) {
	t.Helper()
	configPath := filepath.Join(root, "config", "sources.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`sources:
  screenscraper:
    credentials:
      type: secrets
      username: username
      password: password
      dev_id: dev_id
      dev_password: dev_password
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	secretsPath := filepath.Join(root, "config", "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte(`sources:
  screenscraper:
    username: "user"
    password: "pass"
    dev_id: "dev"
    dev_password: "devpass"
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", path, err)
	}
	return string(contents)
}
