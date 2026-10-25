package screenscraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogcache"
)

func TestImportDryRunPlansSearchesWithoutCredentials(t *testing.T) {
	root := t.TempDir()
	writeSourcesConfig(t, root, "https://screenscraper.example.test")
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)

	result, err := Import(ImportOptions{DataRoot: root, Limit: 1})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if result.SelectedGameCount != 1 || result.PlannedSearchCount == 0 || result.QueriedSearchCount != 0 {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	if result.APIBaseURL != "https://screenscraper.example.test" {
		t.Fatalf("unexpected API base URL: %q", result.APIBaseURL)
	}
}

func TestImportApplyWritesScreenScraperMetadata(t *testing.T) {
	var seenQuery string
	gameRequestCount := 0
	systemRequestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systemesListe.php" {
			systemRequestCount++
			query := r.URL.Query()
			for _, key := range []string{"devid", "devpassword", "softname", "ssid", "sspassword"} {
				if query.Get(key) == "" {
					t.Fatalf("missing query key %s in %s", key, r.URL.RawQuery)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`))
			return
		}
		gameRequestCount++
		if r.URL.Path != "/jeuInfos.php" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		for _, key := range []string{"devid", "devpassword", "softname", "ssid", "sspassword", "systemeid", "romnom"} {
			if query.Get(key) == "" {
				t.Fatalf("missing query key %s in %s", key, r.URL.RawQuery)
			}
		}
		if query.Get("softname") != "RomMeta 0.0.0-dev" {
			t.Fatalf("unexpected softname: %q", query.Get("softname"))
		}
		if query.Get("systemeid") != "14" {
			t.Fatalf("unexpected systemeid: %s", query.Get("systemeid"))
		}
		seenQuery = query.Get("romnom")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "jeu": {
      "id": 1234,
      "noms": [
        {"region": "jp", "text": "Lylat Wars"},
        {"region": "us", "text": "Star Fox 64"}
      ],
      "dates": [
        {"region": "us", "text": "1997-07-01"}
      ],
      "developpeur": {"text": "Nintendo EAD"},
      "editeur": {"text": "Nintendo"},
      "genres": [{"text": "Rail Shooter"}],
      "joueurs": {"text": "1-4"},
      "note": {"text": "18"},
      "synopsis": [
        {"langue": "fr", "text": "Pilotez l'Arwing."},
        {"langue": "en", "text": "Pilot the Arwing."}
      ],
      "medias": [
        {"type": "box-2D", "region": "us", "url": "https://cdn.example.test/starfox-box.png?media=box&softname=RomMeta&ssid=leaked-user&sspassword=leaked-pass&devid=leaked-dev&devpassword=leaked-devpass"},
        {"type": "ss", "url": "https://cdn.example.test/starfox-shot1.png"},
        {"type": "ss", "url": "https://cdn.example.test/starfox-shot2.png"},
        {"type": "video", "url": "https://cdn.example.test/starfox.mp4"}
      ]
    }
  }
}`))
	}))
	defer server.Close()

	root := t.TempDir()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{
		"username":     "user",
		"password":     "pass",
		"dev_id":       "dev",
		"dev_password": "devpass",
	})
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)

	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}

	if seenQuery == "" {
		t.Fatalf("server was not queried")
	}
	if result.MatchedGameCount != 1 || result.WrittenFileCount != 1 || result.FailedSearchCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if gameRequestCount != 1 || systemRequestCount != 1 {
		t.Fatalf("expected one game request and one system request, got games=%d systems=%d", gameRequestCount, systemRequestCount)
	}
	gameYAML := readFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"))
	for _, expected := range []string{
		"  screenscraper:",
		`screenscraper_id: "1234"`,
		`title: "Star Fox 64"`,
		`release_date: "1997-07-01"`,
		`developer: "Nintendo EAD"`,
		`publisher: "Nintendo"`,
		`genres: ["Rail Shooter"]`,
		`players: "1-4"`,
		`titles:`,
		`- title: "Lylat Wars"`,
		`region: "Japan"`,
		`- title: "Star Fox 64"`,
		`region: "USA"`,
		`descriptions:`,
		`en: |-`,
		`Pilot the Arwing.`,
		`fr: |-`,
		`Pilotez l'Arwing.`,
		`media:`,
		`box_front:`,
		`- url: "https://cdn.example.test/starfox-box.png?media=box"`,
		`region: "USA"`,
		`screenshot:`,
		`- url: "https://cdn.example.test/starfox-shot1.png"`,
		`- url: "https://cdn.example.test/starfox-shot2.png"`,
	} {
		if !strings.Contains(gameYAML, expected) {
			t.Fatalf("game.yaml does not contain %q:\n%s", expected, gameYAML)
		}
	}
	if strings.Contains(gameYAML, "starfox.mp4") {
		t.Fatalf("video URL should not be written by metadata/artwork pass:\n%s", gameYAML)
	}
	for _, secret := range []string{"softname", "ssid", "sspassword", "devid", "devpassword", "leaked"} {
		if strings.Contains(gameYAML, secret) {
			t.Fatalf("game.yaml leaked %q:\n%s", secret, gameYAML)
		}
	}

	second, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 1 || second.QueriedSearchCount != 0 {
		t.Fatalf("expected existing ScreenScraper metadata to be skipped, got %#v", second)
	}
	if gameRequestCount != 1 || systemRequestCount != 1 {
		t.Fatalf("second import should not query ScreenScraper, got games=%d systems=%d", gameRequestCount, systemRequestCount)
	}

	forced, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1, ForceOverwrite: true})
	if err != nil {
		t.Fatalf("force Import returned error: %v", err)
	}
	if forced.WrittenFileCount != 1 || forced.SkippedExistingCount != 0 || forced.QueriedSearchCount == 0 {
		t.Fatalf("expected force overwrite to query and write ScreenScraper metadata, got %#v", forced)
	}
	if gameRequestCount != 2 || systemRequestCount != 2 {
		t.Fatalf("force import should query ScreenScraper again, got games=%d systems=%d", gameRequestCount, systemRequestCount)
	}
}

func TestImportApplySearchesScreenScraperByFingerprintFirst(t *testing.T) {
	gameRequestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systemesListe.php" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`))
			return
		}
		gameRequestCount++
		if r.URL.Path != "/jeuInfos.php" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("md5"); got != "05f0e7b7b7f4d070b7db5daeb9708d68" {
			t.Fatalf("md5 = %q, want fingerprint hash (raw %s)", got, r.URL.RawQuery)
		}
		if got := query.Get("romnom"); got != "Star Fox 64 (USA).z64" {
			t.Fatalf("romnom = %q, want the original fingerprint filename with extension", got)
		}
		if got := query.Get("romtype"); got != "rom" {
			t.Fatalf("romtype = %q, want rom", got)
		}
		if got := query.Get("romtaille"); got != "33554432" {
			t.Fatalf("romtaille = %q, want fingerprint size (raw %s)", got, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"jeu":{"id":1234,"noms":[{"region":"us","text":"Star Fox 64"}]}}}`))
	}))
	defer server.Close()

	root := t.TempDir()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{
		"username":     "user",
		"password":     "pass",
		"dev_id":       "dev",
		"dev_password": "devpass",
	})
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
default_release: "Star Fox 64 (USA)"

releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)
	writeFingerprints(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `items:
  - id: "no-intro.0001"
    release: "Star Fox 64 (USA)"
    file:
      name: "Star Fox 64 (USA).z64"
      size: 33554432
      md5: 05f0e7b7b7f4d070b7db5daeb9708d68
    dump:
      format: "z64"
`)

	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if gameRequestCount != 1 {
		t.Fatalf("expected one hash request, got %d", gameRequestCount)
	}
	if result.PlannedSearchCount != 2 || result.QueriedSearchCount != 1 || result.MatchedGameCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	gameYAML := readFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Star Fox 64", "game.yaml"))
	if !strings.Contains(gameYAML, `- release: "Star Fox 64 (USA)"`) {
		t.Fatalf("fingerprint match should keep the catalog release key:\n%s", gameYAML)
	}
}

func TestImportRejectsFingerprintResponseWithoutMatchingROM(t *testing.T) {
	gameRequestCount := 0
	fallbackCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systemesListe.php" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"systemes":[{"id":1,"nom":"Megadrive"}]}}`))
			return
		}
		gameRequestCount++
		if r.URL.Query().Get("md5") == "" {
			fallbackCount++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "jeu": {
      "id": 324,
      "noms": [{"region":"us","text":"The Flintstones"}],
      "roms": [
        {"rommd5":"60EA3535C8A939D26DED83BDE0F4809D","romsize":"524288"}
      ]
    }
  }
}`))
	}))
	defer server.Close()

	root := t.TempDir()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{
		"username":     "user",
		"password":     "pass",
		"dev_id":       "dev",
		"dev_password": "devpass",
	})
	writeGame(t, root, "Sega - Mega Drive - Genesis", "Flintstones, The [2]", `title: "Flintstones, The"
default_release: "Flintstones, The (USA) (Ocean) (Sega Channel)"

releases:
  "Flintstones, The (USA) (Ocean) (Sega Channel)":
    title: "Flintstones, The"
`)
	writeFingerprints(t, root, "Sega - Mega Drive - Genesis", "Flintstones, The [2]", `items:
  - id: "no-intro.flintstones-ocean"
    release: "Flintstones, The (USA) (Ocean) (Sega Channel)"
    file:
      name: "Flintstones, The (USA) (Ocean) (Sega Channel).md"
      size: 2097152
      md5: 27eb1bcbc8fe09f113d038255138d04d
    dump:
      format: "md"
`)

	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1})
	if err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if result.MatchedGameCount != 0 || result.NotFoundGameCount != 1 || result.WrittenFileCount != 1 {
		t.Fatalf("fingerprint mismatch should be written as not_found, got %#v", result)
	}
	if gameRequestCount != 1 || fallbackCount != 0 {
		t.Fatalf("fingerprint mismatch should not fall back to title search, requests=%d fallback=%d", gameRequestCount, fallbackCount)
	}
	gameYAML := readFile(t, filepath.Join(root, "platforms", "Sega - Mega Drive - Genesis", "Flintstones, The [2]", "game.yaml"))
	if !strings.Contains(gameYAML, "status: not_found") || strings.Contains(gameYAML, "screenscraper_id") {
		t.Fatalf("game.yaml should contain a not_found marker only:\n%s", gameYAML)
	}
}

func TestFingerprintQueriesSkipMultiFileDiscFingerprints(t *testing.T) {
	game := catalogGame{
		Title:          "What Is X'Eye Multi Entertainment System",
		DefaultRelease: "What Is X'Eye Multi Entertainment System (USA)",
		Fingerprints: []catalogcache.Fingerprint{
			{Release: "What Is X'Eye Multi Entertainment System (USA)", Name: "What Is X'Eye Multi Entertainment System (USA).cue", Size: 1125, MD5: "11111111111111111111111111111111", Format: "cue"},
			{Release: "What Is X'Eye Multi Entertainment System (USA)", Name: "What Is X'Eye Multi Entertainment System (USA) (Track 1).bin", Size: 49316736, MD5: "22222222222222222222222222222222", Format: "bin"},
			{Release: "What Is X'Eye Multi Entertainment System (USA)", Name: "What Is X'Eye Multi Entertainment System (USA) (Track 2).bin", Size: 6738480, MD5: "33333333333333333333333333333333", Format: "bin"},
		},
	}

	queries := fingerprintQueries(game)

	if len(queries) != 0 {
		t.Fatalf("expected multi-file disc fingerprints to be skipped, got %#v", queries)
	}
}

func TestSearchTermsForUnofficialGameAvoidCleanTitle(t *testing.T) {
	game := catalogGame{
		Title:          "Aladdin",
		DefaultRelease: "Aladdin (World) (Unl) (Pirate)",
		Releases:       []string{"Aladdin (World) (Unl) (Pirate)"},
		UnofficialOnly: true,
	}

	terms := searchTerms(game)

	if contains(terms, "Aladdin") {
		t.Fatalf("unofficial game should not search the clean official title: %#v", terms)
	}
	if !contains(terms, "Aladdin (World) (Unl) (Pirate)") {
		t.Fatalf("expected tagged release search term: %#v", terms)
	}
}

func TestScoreCandidateRejectsOfficialForUnofficialGame(t *testing.T) {
	game := catalogGame{
		Title:          "Aladdin",
		DefaultRelease: "Aladdin (World) (Unl) (Pirate)",
		Releases:       []string{"Aladdin (World) (Unl) (Pirate)"},
		UnofficialOnly: true,
	}
	official := gameMetadata{
		Title: "Aladdin",
	}
	unofficial := gameMetadata{
		Title:           "Aladdin",
		AlternateTitles: []string{"Aladdin (Unl)"},
	}

	if score := scoreCandidate(official, game, searchTerms(game)); score != 0 {
		t.Fatalf("expected official candidate to be rejected, got score %d", score)
	}
	if score := scoreCandidate(unofficial, game, searchTerms(game)); score == 0 {
		t.Fatalf("expected tagged candidate to match")
	}
}

func TestImportApplyWritesScreenScraperGamesIncrementally(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/systemesListe.php" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`))
			return
		}
		if r.URL.Path != "/jeuInfos.php" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query().Get("romnom")
		if strings.Contains(query, "Zulu Missing") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{}}`))
			return
		}
		title := "Alpha Test"
		id := "1"
		if strings.Contains(query, "Beta") {
			title = "Beta Test"
			id = "2"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "jeu": {
      "id": ` + id + `,
      "nom": "` + title + `",
      "noms": [{"region": "us", "text": "` + title + `"}]
    }
  }
}`))
	}))
	defer server.Close()

	root := t.TempDir()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{
		"username":     "user",
		"password":     "pass",
		"dev_id":       "dev",
		"dev_password": "devpass",
	})
	gamePaths := map[string]string{
		"Alpha Test": filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Alpha Test", "game.yaml"),
		"Beta Test":  filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Beta Test", "game.yaml"),
	}
	writeGame(t, root, "Nintendo - Nintendo 64", "Alpha Test", `title: "Alpha Test"
default_release: "Alpha Test (USA)"

releases:
  "Alpha Test (USA)":
    title: "Alpha Test"
`)
	writeGame(t, root, "Nintendo - Nintendo 64", "Beta Test", `title: "Beta Test"
default_release: "Beta Test (USA)"

releases:
  "Beta Test (USA)":
    title: "Beta Test"
`)
	missingPath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Zulu Missing", "game.yaml")
	writeGame(t, root, "Nintendo - Nintendo 64", "Zulu Missing", `title: "Zulu Missing"
default_release: "Zulu Missing (USA)"

releases:
  "Zulu Missing (USA)":
    title: "Zulu Missing"
`)

	checked := false
	firstPath := ""
	var stages []string
	result, err := Import(ImportOptions{
		DataRoot: root,
		Apply:    true,
		Progress: func(progress ImportProgress) {
			if progress.Stage != "" {
				stages = appendUniqueString(stages, progress.Stage)
				return
			}
			if progress.Current == 1 {
				for title, path := range gamePaths {
					if strings.Contains(progress.Game, title) {
						firstPath = path
						break
					}
				}
			}
			if progress.Current == 2 && progress.Matches > 0 {
				checked = true
				if firstPath == "" {
					t.Fatalf("first progress item was not captured")
				}
				contents := readFile(t, firstPath)
				if !strings.Contains(contents, "  screenscraper:") {
					t.Fatalf("first game was not written before searching the second game:\n%s", contents)
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
	for _, expected := range []string{"loading config", "opening catalog cache", "scanning catalog", "reading catalog", "checking credentials", "loading platform list"} {
		if !contains(stages, expected) {
			t.Fatalf("expected progress stage %q in %#v", expected, stages)
		}
	}
	if contains(stages, "checking existing metadata") {
		t.Fatalf("ScreenScraper should not run a separate existing-metadata scan, stages=%#v", stages)
	}
	if result.WrittenFileCount != 3 || result.MatchedGameCount != 2 || result.NotFoundGameCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	missing := readFile(t, missingPath)
	if !strings.Contains(missing, "  screenscraper:") || !strings.Contains(missing, "status: not_found") || !strings.Contains(missing, "checked_at:") {
		t.Fatalf("missing game was not marked as not_found:\n%s", missing)
	}
	second, err := Import(ImportOptions{DataRoot: root, Apply: true})
	if err != nil {
		t.Fatalf("second Import returned error: %v", err)
	}
	if second.WrittenFileCount != 0 || second.SkippedExistingCount != 3 || second.QueriedSearchCount != 0 {
		t.Fatalf("expected matched and not_found games to be skipped on second import, got %#v", second)
	}
}

func TestImportApplyRequiresScreenScraperUserCredentials(t *testing.T) {
	root := t.TempDir()
	writeSourcesConfig(t, root, "https://screenscraper.example.test")
	writeSecrets(t, root, map[string]string{
		"username": "user",
	})
	writeGame(t, root, "Nintendo - Nintendo 64", "Star Fox 64", `title: "Star Fox 64"
releases:
  "Star Fox 64 (USA)":
    title: "Star Fox 64"
`)

	_, err := Import(ImportOptions{DataRoot: root, Apply: true, Limit: 1})
	if err == nil {
		t.Fatalf("expected missing credentials error")
	}
	if !strings.Contains(err.Error(), "sources.screenscraper.password") || !strings.Contains(err.Error(), "sources.screenscraper.dev_id") || !strings.Contains(err.Error(), "sources.screenscraper.dev_password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryGameExplainsDeveloperCredentialRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Erreur de login : Vérifier vos identifiants développeur !", http.StatusForbidden)
	}))
	defer server.Close()

	_, _, err := queryGame(server.Client(), sourceSettings{
		Username:   "user",
		Password:   "pass",
		SoftName:   "RomMeta",
		APIBaseURL: server.URL,
	}, "14", "Star Fox 64")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "ordinary user credentials alone were rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScreenScraperRequestsExplainUnavailableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	settings := sourceSettings{
		Username:    "user",
		Password:    "pass",
		DevID:       "dev",
		DevPassword: "devpass",
		SoftName:    "RomMeta",
		APIBaseURL:  url,
	}

	if _, err := querySystems(server.Client(), settings); err == nil || !strings.Contains(err.Error(), "ScreenScraper platform list request failed") {
		t.Fatalf("unexpected platform list error: %v", err)
	}
	if _, _, err := queryGame(server.Client(), settings, "14", "Star Fox 64"); err == nil || !strings.Contains(err.Error(), "ScreenScraper search request failed") {
		t.Fatalf("unexpected search error: %v", err)
	}
}

func TestScreenScraperSystemIDUsesPlatformAliases(t *testing.T) {
	for _, tc := range []struct {
		platform string
		id       string
	}{
		{"Nintendo - Nintendo 64", "14"},
		{"Nintendo - Super Nintendo Entertainment System", "4"},
		{"NEC - PC Engine - TurboGrafx-16", "31"},
		{"NEC - PC Engine CD & TurboGrafx CD", "114"},
	} {
		id, ok := screenScraperSystemID(tc.platform, nil, nil)
		if !ok || id != tc.id {
			t.Fatalf("screenScraperSystemID(%q) = %q, %t; want %q, true", tc.platform, id, ok, tc.id)
		}
	}
}

func TestScreenScraperSystemIDUsesFetchedPlatformList(t *testing.T) {
	systems := []screenScraperSystem{
		{ID: "20", Name: "Mega CD", Names: []string{"Mega CD", "Sega CD"}},
		{ID: "21", Name: "Megadrive", Names: []string{"Megadrive", "Genesis", "Sega Genesis"}},
		{ID: "99", Name: "Snes - Super Mario World Hacks", Names: []string{"snes", "Super Nintendo Entertainment System"}},
		{ID: "4", Name: "Super Nintendo", Names: []string{"Super Nintendo", "Super Famicom", "SNES"}},
	}
	id, ok := screenScraperSystemID("Sega - Mega CD & Sega CD", systems, nil)
	if !ok || id != "20" {
		t.Fatalf("screenScraperSystemID with systems = %q, %t; want 20, true", id, ok)
	}
	id, ok = screenScraperSystemID("Nintendo - Super Nintendo Entertainment System", systems, nil)
	if !ok || id != "4" {
		t.Fatalf("screenScraperSystemID with systems = %q, %t; want 4, true", id, ok)
	}
}

func TestScreenScraperSystemsCachesPlatformList(t *testing.T) {
	systemRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/systemesListe.php" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		systemRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "response": {
    "systemes": [
      {"id": "14", "nom": "Nintendo 64"},
      {"idsysteme": "20", "noms": {"nom_eu": "Mega CD", "nom_us": "Sega CD", "noms_commun": "Sega Mega CD,Sega CD"}}
    ]
  }
}`))
	}))
	defer server.Close()

	root := t.TempDir()
	settings := sourceSettings{
		Username:    "user",
		Password:    "pass",
		DevID:       "dev",
		DevPassword: "devpass",
		SoftName:    "RomMeta",
		APIBaseURL:  server.URL,
	}
	systems, cached, err := screenScraperSystems(server.Client(), root, settings, false)
	if err != nil {
		t.Fatalf("screenScraperSystems returned error: %v", err)
	}
	if cached || len(systems) != 2 {
		t.Fatalf("unexpected first systems result cached=%t systems=%#v", cached, systems)
	}
	id, ok := screenScraperSystemID("Sega - Mega CD & Sega CD", systems, nil)
	if !ok || id != "20" {
		t.Fatalf("cached systems do not resolve Sega CD: id=%q ok=%t systems=%#v", id, ok, systems)
	}
	systems, cached, err = screenScraperSystems(server.Client(), root, settings, false)
	if err != nil {
		t.Fatalf("second screenScraperSystems returned error: %v", err)
	}
	if !cached || len(systems) != 2 || systemRequests != 1 {
		t.Fatalf("expected cached second result, cached=%t requests=%d systems=%#v", cached, systemRequests, systems)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func writeSourcesConfig(t *testing.T, root, apiBaseURL string) {
	t.Helper()
	path := filepath.Join(root, "config", "sources.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	contents := `schema_version: 0
sources:
  screenscraper:
    enabled: false
    kind: metadata-and-media
    credentials:
      type: secrets
      username: username
      password: password
      dev_id: dev_id
      dev_password: dev_password
    api:
      base_url: "` + apiBaseURL + `"
      rate_limit_per_second: 100
    levels:
      metadata: true
      artwork: true
      video: false
fetch:
  metadata:
    - screenscraper
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func writeSecrets(t *testing.T, root string, values map[string]string) {
	t.Helper()
	path := filepath.Join(root, "config", "secrets.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	var b strings.Builder
	b.WriteString("schema_version: 0\nsources:\n  screenscraper:\n")
	for _, key := range []string{"username", "password", "dev_id", "dev_password"} {
		if value, ok := values[key]; ok {
			b.WriteString("    " + key + ": \"" + value + "\"\n")
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
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

func writeFingerprints(t *testing.T, root, platform, game, contents string) {
	t.Helper()
	path := filepath.Join(root, "platforms", platform, game, "fingerprints.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
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

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func TestFingerprintQueriesReachOriginalBeforeUnofficialDefault(t *testing.T) {
	clone := "Game (USA) (Pirate)"
	original := "Game (Japan)"
	game := catalogGame{Title: "Game", DefaultRelease: clone, Fingerprints: []catalogcache.Fingerprint{
		{Release: clone, Name: "Game.nes", Size: 100, MD5: "11111111111111111111111111111111"},
		{Release: "Game (Asia) (Pirate)", Name: "Clone.nes", Size: 100, MD5: "22222222222222222222222222222222"},
		{Release: original, Name: "Original.nes", Size: 100, MD5: "33333333333333333333333333333333"},
	}}
	queries := fingerprintQueries(game)
	if len(queries) != maxHashSearches || queries[0].Release != original {
		t.Fatalf("bounded queries starve original: %#v", queries)
	}
}
