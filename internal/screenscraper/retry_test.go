package screenscraper

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestImportPacesScreenScraperAcrossGames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourcesConfig(t, root, "https://example.test")
		path := filepath.Join(root, "config", "sources.yaml")
		config := strings.ReplaceAll(readFile(t, path), "rate_limit_per_second: 100", "rate_limit_per_second: 1")
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		writeSecrets(t, root, map[string]string{"username": "test-user", "password": "test-pass", "dev_id": "test-dev", "dev_password": "test-dev-pass"})
		for _, title := range []string{"Game A", "Game B", "Game C"} {
			writeGame(t, root, "Nintendo - Nintendo 64", title, fmt.Sprintf("title: %q\n", title))
		}
		var starts []time.Time
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/systemesListe.php" {
				return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
			}
			starts = append(starts, time.Now())
			return testResponse(200, fmt.Sprintf(`{"response":{"jeu":{"id":42,"noms":[{"region":"us","text":%q}]}}}`, r.URL.Query().Get("romnom"))), nil
		})}
		result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
		if err != nil || result.MatchedGameCount != 3 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		if len(starts) != 3 {
			t.Fatalf("unexpected requests: %v", starts)
		}
		for i := 1; i < len(starts); i++ {
			if starts[i].Sub(starts[i-1]) < time.Second {
				t.Fatalf("pacing reset between games: %v", starts)
			}
		}
	})
}

func TestImportRecoversScreenScraperTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourcesConfig(t, root, "https://example.test")
		writeSecrets(t, root, map[string]string{"username": "test-user", "password": "test-pass", "dev_id": "test-dev", "dev_password": "test-dev-pass"})
		writeGame(t, root, "Nintendo - Nintendo 64", "Game A", "title: Game A\n")
		var starts []time.Time
		var retryVisible bool
		client := &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/systemesListe.php" {
				return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
			}
			starts = append(starts, time.Now())
			if len(starts) == 1 {
				<-r.Context().Done()
				return nil, context.DeadlineExceeded
			}
			return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"us","text":"Game A"}]}}}`), nil
		})}
		result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client, Progress: func(p ImportProgress) {
			if strings.Contains(p.Stage, "retry") {
				retryVisible = true
			}
		}})
		if err != nil || result.MatchedGameCount != 1 || result.WrittenFileCount != 1 || result.FailedSearchCount != 0 || result.QueriedSearchCount != 2 {
			t.Fatalf("one transient timeout should recover: result=%+v error=%v", result, err)
		}
		if len(starts) != 2 || starts[1].Sub(starts[0]) < 3*time.Second || !retryVisible {
			t.Fatalf("retry must wait after the timeout and show progress: starts=%v visible=%t", starts, retryVisible)
		}
	})
}

func TestImportAllowsSlowScreenScraperHeaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourcesConfig(t, root, "https://example.test")
		writeSecrets(t, root, map[string]string{"username": "test-user", "password": "test-pass", "dev_id": "test-dev", "dev_password": "test-dev-pass"})
		writeGame(t, root, "Nintendo - Nintendo 64", "Game A", "title: Game A\n")
		previous := http.DefaultTransport
		defer func() { http.DefaultTransport = previous }()
		queries := 0
		http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/systemesListe.php" {
				return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
			}
			queries++
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) != 90*time.Second {
				t.Fatalf("expected a bounded 90-second request, got deadline %v", deadline)
			}
			select {
			case <-time.After(70 * time.Second):
				return testResponse(200, `{"response":{"jeu":{"id":42,"noms":[{"region":"us","text":"Game A"}]}}}`), nil
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		})
		result, err := Import(ImportOptions{DataRoot: root, Apply: true})
		if err != nil || result.MatchedGameCount != 1 || result.FailedSearchCount != 0 || queries != 1 {
			t.Fatalf("70-second response should succeed without retry: result=%+v queries=%d error=%v", result, queries, err)
		}
	})
}
