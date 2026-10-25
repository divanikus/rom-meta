package screenscraper

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

func TestImportStopsOnDailyQuotaWithoutMarkingGamesNotFound(t *testing.T) {
	for _, quotaEndpoint := range []string{"/systemesListe.php", "/jeuInfos.php"} {
		t.Run(quotaEndpoint, func(t *testing.T) {
			root := t.TempDir()
			writeSourcesConfig(t, root, "https://screenscraper.example.test")
			writeSecrets(t, root, map[string]string{"username": "user", "password": "pass", "dev_id": "dev", "dev_password": "devpass"})
			originals := map[string]string{}
			for _, name := range []string{"First", "Second"} {
				content := fmt.Sprintf("title: %q\n", name)
				writeGame(t, root, "Nintendo - Nintendo 64", name, content)
				originals[filepath.Join(root, "platforms", "Nintendo - Nintendo 64", name, "game.yaml")] = content
			}
			quotaCalls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == quotaEndpoint {
					quotaCalls++
					return testResponse(430, "daily quota exceeded"), nil
				}
				if r.URL.Path == "/systemesListe.php" {
					return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
				}
				t.Fatalf("unexpected request after quota: %s", r.URL.Path)
				return nil, nil
			})}
			_, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
			if !errors.Is(err, ErrDailyQuota) || quotaCalls != 1 {
				t.Fatalf("quota did not stop import: calls=%d error=%v", quotaCalls, err)
			}
			for path, before := range originals {
				if got := readFile(t, path); got != before {
					t.Errorf("quota wrote game metadata: %s", path)
				}
			}
		})
	}
}
