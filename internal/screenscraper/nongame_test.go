package screenscraper

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportRejectsNonGameBucketAndCanUseAnotherRelease(t *testing.T) {
	for _, laterMatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("later_match_%t", laterMatch), func(t *testing.T) {
			root := t.TempDir()
			writeSourcesConfig(t, root, "https://example.test")
			writeSecrets(t, root, map[string]string{"username": "test-user", "password": "test-pass", "dev_id": "test-dev", "dev_password": "test-dev-pass"})
			writeGame(t, root, "Nintendo - Nintendo 64", "Demo Game", "title: Demo Game\ndefault_release: Demo Game (World)\n")
			fingerprints := `items:
  - release: "Demo Game (World)"
    file:
      name: "Demo Game (World).z64"
      size: 1024
      md5: "11111111111111111111111111111111"
`
			if laterMatch {
				fingerprints += `  - release: "Demo Game (USA)"
    file:
      name: "Demo Game (USA).z64"
      size: 1024
      md5: "22222222222222222222222222222222"
`
			}
			writeFingerprints(t, root, "Nintendo - Nintendo 64", "Demo Game", fingerprints)
			queries := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/systemesListe.php" {
					return testResponse(200, `{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`), nil
				}
				queries++
				md5 := r.URL.Query().Get("md5")
				if md5 == "" {
					t.Fatal("do not use title fallbacks after a non-game fingerprint response")
				}
				title := "ZZZ(notgame):#NONGAME"
				id := 99999
				if md5 == strings.Repeat("2", 32) {
					title = "Demo Game"
					id = 42
				}
				return testResponse(200, fmt.Sprintf(`{"response":{"jeu":{"id":%d,"noms":[{"region":"us","text":%q}],"roms":[{"rommd5":%q,"romsize":1024}]}}}`, id, title, md5)), nil
			})}
			result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: client})
			if err != nil {
				t.Fatal(err)
			}
			if result.FailedSearchCount != 0 || result.WrittenFileCount != 1 {
				t.Fatalf("result=%+v", result)
			}
			got := readFile(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Demo Game", "game.yaml"))
			if strings.Contains(got, "NONGAME") || strings.Contains(got, "99999") {
				t.Fatalf("service bucket must not become an identity: %s", got)
			}
			if laterMatch {
				if result.MatchedGameCount != 1 || result.NotFoundGameCount != 0 || queries != 2 {
					t.Fatalf("real alternative was lost: %+v, queries=%d", result, queries)
				}
			} else if result.NotFoundGameCount != 1 || result.MatchedGameCount != 0 || queries != 1 || !strings.Contains(got, "status: not_found") {
				t.Fatalf("bucket should give no usable match: %+v, queries=%d", result, queries)
			}
		})
	}
}

func TestNonGameMetadataRecognition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata gameMetadata
		want     bool
	}{
		{"bucket", gameMetadata{Title: "ZZZ(notgame):#NONGAME"}, true},
		{"alternate", gameMetadata{Title: "Demo Game", AlternateTitles: []string{"zzz(NOTGAME):#NONGAME"}}, true},
		{"short", gameMetadata{Title: " #NONGAME "}, true},
		{"ordinary_word", gameMetadata{Title: "This Is Notgame Adventure"}, false},
		{"real_demo", gameMetadata{Title: "Demo Game (Demo) (Unl)"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNonGameMetadata(tc.metadata); got != tc.want {
				t.Fatalf("isNonGameMetadata(%+v)=%t, want %t", tc.metadata, got, tc.want)
			}
		})
	}
}
