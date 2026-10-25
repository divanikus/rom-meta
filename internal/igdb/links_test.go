package igdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func linkFixture(t *testing.T, wrongPlatform bool) (ImportOptions, []string, *int) {
	t.Helper()
	root := t.TempDir()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"access_token":"synthetic","token_type":"bearer","expires_in":3600}`)
			return
		}
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/games" || string(body) != "fields id,name,slug,url,platforms.id,platforms.name; where id = (10,21); limit 500;" || r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Errorf("unexpected link request: %s %s", r.URL.Path, body)
		}
		platform := 35
		if wrongPlatform {
			platform = 1
		}
		fmt.Fprintf(w, `[{"id":10,"name":"Exact One","slug":"exact-one","url":"https://www.igdb.com/games/exact-one","platforms":[{"id":35,"name":"Game Gear"}]},{"id":21,"name":"Exact Port","slug":"exact-port","url":"https://www.igdb.com/games/exact-port","platforms":[{"id":%d,"name":"Game Gear"}]}]`, platform)
	}))
	t.Cleanup(server.Close)
	writeText(t, filepath.Join(root, "config", "sources.yaml"), `sources:
  igdb:
    credentials:
      type: secrets
      client_id: client_id
      client_secret: client_secret
    oauth:
      token_url: `+server.URL+`/token
      grant_type: client_credentials
    api:
      base_url: `+server.URL+`
      rate_limit_per_second: 1000
`)
	writeText(t, filepath.Join(root, "config", "secrets.yaml"), "sources:\n  igdb:\n    client_id: synthetic-client\n    client_secret: synthetic-secret\n")
	review := linkReview{SchemaVersion: 1, Platform: "Sega - Game Gear", PlatformID: 35}
	paths := []string{}
	for i, action := range []string{"confirm", "replace", "suppress"} {
		game := fmt.Sprintf("platforms/Sega - Game Gear/Card%d", i)
		path := filepath.Join(root, filepath.FromSlash(game), "game.yaml")
		data := fmt.Sprintf("title: Owner\ncustom: {keep: true}\nmanual: {description: Owner prose}\nsources:\n  igdb: {status: not_found, checked_at: original}\n  hasheous:\n    matches: [{hasheous_id: 42, igdb_id: %d, signatures: [NoIntros], matched_query: 'md5:12345678'}]\n  launchbox: {matches: [{database_id: 50}]}\n", (i+1)*10)
		writeText(t, path, data)
		paths = append(paths, path)
		entry := linkReviewEntry{Game: game, ExpectedSHA256: shaBytes([]byte(data)), FromID: int64((i + 1) * 10), Action: action, Reason: "Reviewed official identity", Evidence: []string{"https://www.igdb.com/games/exact-port"}}
		if action == "replace" {
			entry.ToID = 21
		}
		review.Entries = append(review.Entries, entry)
	}
	reviewPath := filepath.Join(root, "review.json")
	data, _ := json.Marshal(review)
	writeText(t, reviewPath, string(data))
	return ImportOptions{DataRoot: root, Platform: review.Platform, RefreshLinks: true, LinkReview: reviewPath, Client: server.Client()}, paths, &calls
}
func TestLinkRefreshMinimalQueryDryRunApplyAndOfflineRepeat(t *testing.T) {
	options, paths, calls := linkFixture(t, false)
	original := [][]byte{}
	for _, path := range paths {
		b, _ := os.ReadFile(path)
		original = append(original, b)
	}
	result, err := Import(options)
	if err != nil || result.WrittenFileCount != 0 || *calls != 1 {
		t.Fatal(result, err, *calls)
	}
	for i, path := range paths {
		b, _ := os.ReadFile(path)
		if string(b) != string(original[i]) {
			t.Fatal("dryrun changed catalog")
		}
	}
	options.Apply = true
	result, err = Import(options)
	if err != nil || result.WrittenFileCount != 3 || result.CachedSearchCount != 1 || *calls != 1 {
		t.Fatal(result, err, *calls)
	}
	written := [][]byte{}
	for i, path := range paths {
		b, _ := os.ReadFile(path)
		written = append(written, b)
		var before, after map[string]any
		_ = yaml.Unmarshal(original[i], &before)
		_ = yaml.Unmarshal(b, &after)
		bs := before["sources"].(map[string]any)
		as := after["sources"].(map[string]any)
		if !reflect.DeepEqual(bs["hasheous"], as["hasheous"]) || !reflect.DeepEqual(bs["launchbox"], as["launchbox"]) || before["title"] != after["title"] || !reflect.DeepEqual(before["custom"], after["custom"]) {
			t.Fatal("modified protected source or owner fields")
		}
		igdb := as["igdb"].(map[string]any)
		if i < 2 {
			matches := igdb["matches"].([]any)
			m := matches[0].(map[string]any)
			if len(m) != 7 || m["capture"] != "identity-links" || igdb["status"] != "identity_only" {
				t.Fatal(m)
			}
		} else if !reflect.DeepEqual(bs["igdb"], igdb) {
			t.Fatal("suppress fabricated provider snapshot")
		}
	}
	// Original expected SHA is now stale. Identical complete output is a pure no-op,
	// without credentials/network/cache access, not authority for another mutation.
	_ = os.Remove(filepath.Join(options.DataRoot, "config", "secrets.yaml"))
	result, err = Import(options)
	if err != nil || result.WrittenFileCount != 0 || result.SkippedExistingCount != 3 || *calls != 1 {
		t.Fatal(result, err, *calls)
	}
	for i, path := range paths {
		b, _ := os.ReadFile(path)
		if string(b) != string(written[i]) {
			t.Fatal("repeat changed bytes")
		}
	}
}
func TestLinkRefreshRejectsAggregateWrongPlatformOrSourceDrift(t *testing.T) {
	for _, wrongPlatform := range []bool{true, false} {
		t.Run(fmt.Sprint(wrongPlatform), func(t *testing.T) {
			options, paths, _ := linkFixture(t, wrongPlatform)
			options.Apply = true
			if !wrongPlatform {
				f, _ := os.OpenFile(paths[2], os.O_APPEND|os.O_WRONLY, 0600)
				_, _ = f.WriteString("extra: changed\n")
				f.Close()
			}
			originals := [][]byte{}
			for _, p := range paths {
				b, _ := os.ReadFile(p)
				originals = append(originals, b)
			}
			if _, err := Import(options); err == nil {
				t.Fatal("unsafe review accepted")
			}
			for i, p := range paths {
				b, _ := os.ReadFile(p)
				if string(b) != string(originals[i]) {
					t.Fatal("partial publication before aggregate validation")
				}
			}
		})
	}
}
func TestLinkReviewRejectsTraversalUnknownFieldsAndUnsafeDecisions(t *testing.T) {
	options, _, _ := linkFixture(t, false)
	data, _ := os.ReadFile(options.LinkReview)
	for _, body := range []string{strings.Replace(string(data), "platforms/Sega - Game Gear/Card0", "platforms/Sega - Game Gear/..", 1), strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"force":true`, 1), strings.Replace(string(data), `"to_id":21`, `"to_id":20`, 1)} {
		writeText(t, options.LinkReview, body)
		if _, err := Import(options); err == nil {
			t.Fatal("invalid review accepted", body)
		}
	}
}
