package screenscraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestImportContinuesAfterTimeoutAndRetriesOnResume(t *testing.T) {
	const platform = "Nintendo - Nintendo 64"
	const hash = "00000403000000000000000000000000"
	root := t.TempDir()
	writeSourcesConfig(t, root, "https://screenscraper.example.test")
	writeSecrets(t, root, map[string]string{
		"username": "test-user", "password": "test-password",
		"dev_id": "test-dev-id", "dev_password": "test-dev-password",
	})
	for _, title := range []string{"Family Kid", "Star Fox 64"} {
		writeGame(t, root, platform, title, fmt.Sprintf("title: %q\n", title))
	}
	writeFingerprints(t, root, platform, "Family Kid", `items:
  - id: "fixture.0001"
    release: "Family Kid"
    file:
      name: "Family Kid.nes"
      size: 524304
      md5: "`+hash+`"
`)
	failedPath := filepath.Join(root, "platforms", platform, "Family Kid", "game.yaml")
	before := readFile(t, failedPath)
	fail := true
	requests := map[string]int{}
	client := &http.Client{
		Timeout: 20 * time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/systemesListe.php" {
				return testResponse(http.StatusOK, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
			}
			title := strings.TrimSuffix(request.URL.Query().Get("romnom"), ".nes")
			requests[title]++
			if title == "Family Kid" {
				if got := request.URL.Query().Get("md5"); got != hash {
					t.Fatalf("md5 = %q, want %q", got, hash)
				}
				if fail {
					<-request.Context().Done()
					return nil, request.Context().Err()
				}
			}
			return testResponse(http.StatusOK, fmt.Sprintf(`{"response":{"jeu":{"id":1234,"noms":[{"region":"us","text":%q}]}}}`, title)), nil
		}),
	}

	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
	if err != nil {
		t.Fatalf("a game timeout must not abort Import: %v", err)
	}
	if result.FailedSearchCount != 1 || result.WrittenFileCount != 1 || result.MatchedGameCount != 1 || result.NotFoundGameCount != 0 {
		t.Fatalf("unexpected first result: %#v", result)
	}
	if got := readFile(t, failedPath); got != before {
		t.Fatalf("failed game must remain unchanged for resume:\n%s", got)
	}
	if requests["Family Kid"] != 2 || requests["Star Fox 64"] != 1 {
		t.Fatalf("both games must be attempted: %#v", requests)
	}
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Error, "deadline exceeded") {
		t.Fatalf("expected timeout in failure summary: %#v", result.Failures)
	}
	if strings.Contains(result.Failures[0].Error, "test-password") || strings.Contains(result.Failures[0].Error, "test-dev-password") {
		t.Fatal("failure summary contains credentials")
	}

	fail = false
	resumed, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if resumed.FailedSearchCount != 0 || resumed.WrittenFileCount != 1 || resumed.MatchedGameCount != 1 || resumed.SkippedExistingCount != 1 {
		t.Fatalf("unexpected resumed result: %#v", resumed)
	}
	if requests["Family Kid"] != 3 || requests["Star Fox 64"] != 1 {
		t.Fatalf("resume must retry only the failed game: %#v", requests)
	}
	if !strings.Contains(readFile(t, failedPath), "screenscraper:") {
		t.Fatal("resumed game metadata was not saved")
	}
}

func TestScreenScraperRequestErrorsOmitCredentialsAndPreserveTimeout(t *testing.T) {
	settings := sourceSettings{
		APIBaseURL: "https://screenscraper.example.test",
		Username:   "private-user", Password: "private-password",
		DevID: "private-dev", DevPassword: "private-dev-password",
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		// Include a nested URL error as well as the wrapper supplied by http.Client.
		return nil, &url.Error{Op: "Get", URL: request.URL.String(), Err: context.DeadlineExceeded}
	})}
	for _, endpoint := range []string{"platform list", "search"} {
		t.Run(endpoint, func(t *testing.T) {
			var err error
			if endpoint == "platform list" {
				_, err = querySystems(client, settings)
			} else {
				_, _, err = queryGameWithFingerprint(client, settings, "14", screenScraperQuery{
					Term: "Login Quest", MD5: "00000401000000000000000000000000", Size: 42,
				})
			}
			if err == nil || !strings.Contains(err.Error(), endpoint+" request failed") {
				t.Fatalf("expected contextual request error, got %v", err)
			}
			for _, secret := range []string{"private-", "ssid=", "sspassword=", "devid=", "devpassword="} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("request error leaks %q: %v", secret, err)
				}
			}
			var timeout net.Error
			if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("timeout identity was lost: %v", err)
			}
			if isFatalSearchError(err) || isNotFoundError(err) {
				t.Fatalf("transport failure must remain retryable: %v", err)
			}
		})
	}
}

func TestScreenScraperClassifiesServerResponseInsteadOfRequestText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		fatal    bool
		notFound bool
	}{
		{"unauthorized", 401, "access denied", true, false},
		{"forbidden", 403, "access denied", true, false},
		{"text authentication failure", 200, "Erreur de login : identifiants invalides", true, false},
		{"server failure", 500, "upstream returned 401", false, false},
		{"unavailable login service", 503, "login service not found", false, false},
		{"rate limit", 429, "retry after 403 seconds", false, false},
		{"daily quota", 430, "quota exceeded", true, false},
		{"game absent", 404, "jeu introuvable", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testResponse(tc.status, tc.body), nil
			})}
			_, _, err := queryGame(client, sourceSettings{APIBaseURL: "https://screenscraper.example.test"}, "14", "Login 403")
			if err == nil {
				t.Fatal("expected server error")
			}
			if got := isFatalSearchError(err); got != tc.fatal {
				t.Errorf("fatal = %t, want %t: %v", got, tc.fatal, err)
			}
			if got := isNotFoundError(err); got != tc.notFound {
				t.Errorf("not found = %t, want %t: %v", got, tc.notFound, err)
			}
		})
	}
}
