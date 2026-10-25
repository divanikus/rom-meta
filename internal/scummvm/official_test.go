package scummvm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func officialFixture() map[string][]byte {
	return map[string][]byte{
		"versions.yaml": []byte(`- {id: DEV, release_date: "March 16, 2118"}
- {id: 2.9.0rc1, release_date: "January 1, 2024"}
- {id: 2.9.0, release_date: "January 2, 2024"}
- {id: 2.10.0, release_date: "January 2, 2024"}
- {id: 99.0.0, release_date: "January 1, 2099"}
`),
		"engines.yaml": []byte(`- {id: scumm, enabled: true}
- {id: sci, enabled: true}
- {id: ags, enabled: true}
- {id: disabled, enabled: false}
`),
		"games.yaml": []byte(`- {id: "scumm:tentacle", name: "Day of the Tentacle", engine_id: scumm}
- {id: "sci:one", name: "Regression", engine_id: sci}
- {id: "scumm:future", name: "Future game", engine_id: scumm}
- {id: "scumm:bug", name: "Playable with bugs", engine_id: scumm}
- {id: "ags:ags", name: "AGS games", engine_id: ags}
- {id: "disabled:one", name: "Disabled game", engine_id: disabled}
`),
		"compatibility.yaml": []byte(`- {id: "scumm:tentacle", support: broken, since_version: 2.9.0rc1}
- {id: "scumm:tentacle", support: excellent, since_version: 2.10.0}
- {id: "sci:one", support: good, since_version: 2.9.0}
- {id: "sci:one", support: broken, since_version: 2.10.0}
- {id: "scumm:future", support: excellent, since_version: DEV}
- {id: "scumm:bug", support: bugged, since_version: 2.9.0}
- {id: "ags:ags", support: good, since_version: 2.9.0}
- {id: "disabled:one", support: excellent, since_version: 2.9.0}
`),
	}
}

type officialTransport func(*http.Request) (*http.Response, error)

func (fn officialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func fixtureClient(t *testing.T, requests *int) *http.Client {
	t.Helper()
	files := officialFixture()
	return &http.Client{Transport: officialTransport(func(r *http.Request) (*http.Response, error) {
		*requests++
		if r.Header.Get("User-Agent") != "RomMeta" {
			t.Error("missing user agent")
		}
		var contents []byte
		if r.URL.String() == officialRefs {
			contents = []byte(officialRefFixture())
		} else if r.URL.String() == officialHead {
			contents = []byte(`{"sha":"` + fixtureRevision + `"}`)
		} else {
			prefix := officialRaw + fixtureRevision + "/data/en/"
			if !strings.HasPrefix(r.URL.String(), prefix) {
				t.Fatalf("unpinned or nonofficial URL: %s", r.URL)
			}
			name := strings.TrimPrefix(r.URL.String(), prefix)
			var ok bool
			contents, ok = files[name]
			if !ok {
				t.Fatalf("unexpected source file %s", name)
			}
		}
		header := http.Header{}
		if r.URL.String() == officialRefs {
			header.Set("Content-Type", officialRefsMediaType)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(contents)), Header: header}, nil
	})}
}

func TestOfficialDefaultAcquisitionAndReleaseSelection(t *testing.T) {
	requests := 0
	list, err := Load(ListOptions{Client: fixtureClient(t, &requests)})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 5 || list.Official.Release != "2.10.0" || len(list.Games) != 3 || list.Official.Excluded != 2 {
		t.Fatalf("unexpected official list: requests=%d %+v %+v", requests, list, list.Official)
	}
	if list.Games[2].ID != "scumm:tentacle" || list.Games[2].Support != "excellent" {
		t.Fatalf("wrong latest compatibility state: %+v", list.Games)
	}
	old, err := parseOfficial(officialFixture(), fixtureRevision, "2.9.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ids := string(old.Contents)
	if strings.Contains(ids, "scumm:tentacle") || !strings.Contains(ids, "sci:one") || strings.Contains(ids, "scumm:future") {
		t.Fatalf("historical release not respected: %s", ids)
	}
	for _, release := range []string{"DEV", "99.0.0", "2.9.0rc1", "missing"} {
		if _, err := parseOfficial(officialFixture(), fixtureRevision, release, time.Now()); err == nil {
			t.Fatalf("accepted unsupported stable selection %s", release)
		}
	}
}

func TestOfficialImportPreviewApplyOfflineAndIntegrity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "catalog")
	requests := 0
	options := ImportOptions{DataRoot: root, ListOptions: ListOptions{Client: fixtureClient(t, &requests)}}
	preview, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Added != 2 || preview.SkippedGeneric != 1 {
		t.Fatalf("wrong preview: %+v", preview)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("online preview wrote to disk")
	}
	options.Apply = true
	result, err := Import(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(append([]string{}, officialFiles...), "games.txt", "manifest.json") {
		if _, err := os.Stat(filepath.Join(result.SnapshotPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	cards, err := readExisting(filepath.Join(root, "platforms", Platform))
	if err != nil {
		t.Fatal(err)
	}
	card := string(cards["scumm:tentacle"].contents)
	for _, fact := range []string{"source_url: " + officialRepo, fixtureRevision, "support: excellent"} {
		if !strings.Contains(card, fact) {
			t.Fatalf("missing source fact %s: %s", fact, card)
		}
	}
	networkCalls := requests
	options.Offline = true
	options.Client = &http.Client{Transport: officialTransport(func(*http.Request) (*http.Response, error) { t.Fatal("offline used network"); return nil, nil })}
	options.Run = nil
	repeat, err := Import(options)
	if err != nil || repeat.WrittenFiles != 0 || repeat.Unchanged != 2 || requests != networkCalls {
		t.Fatalf("offline repeat differs: %+v %v", repeat, err)
	}
	changed := filepath.Join(result.SnapshotPath, "games.yaml")
	if err := os.WriteFile(changed, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(options); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("corrupt source accepted: %v", err)
	}
}

func TestOfficialSourceFailuresAndInputConflicts(t *testing.T) {
	for _, options := range []ListOptions{
		{Executable: "scummvm", Offline: true}, {ListFile: "games.txt", Version: "test", Release: "2.9.0"},
		{Version: "test"}, {Revision: "master"},
	} {
		if _, err := Load(options); err == nil {
			t.Fatalf("accepted conflicting/invalid options: %+v", options)
		}
	}
	if _, err := Load(ListOptions{Offline: true, CacheRoot: t.TempDir()}); err == nil {
		t.Fatal("missing offline snapshot accepted")
	}
	client := &http.Client{Transport: officialTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
	})}
	if _, err := Load(ListOptions{Client: client}); err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("HTTP failure hidden: %v", err)
	}
	for _, mutate := range []func(map[string][]byte){
		func(f map[string][]byte) { f["games.yaml"] = []byte("bad yaml: [") },
		func(f map[string][]byte) {
			f["compatibility.yaml"] = append(f["compatibility.yaml"], []byte("\n- {id: 'scumm:tentacle', support: unknown, since_version: 2.10.0}")...)
		},
		func(f map[string][]byte) {
			f["compatibility.yaml"] = append(f["compatibility.yaml"], []byte("\n- {id: 'scumm:tentacle', support: broken, since_version: 2.10.0}")...)
		},
		func(f map[string][]byte) { delete(f, "engines.yaml") },
	} {
		files := officialFixture()
		mutate(files)
		if _, err := parseOfficial(files, fixtureRevision, "", time.Now()); err == nil {
			t.Fatal("malformed or contradictory official data accepted")
		}
	}
}

func TestOfficialPinnedRevisionSkipsHeadRequest(t *testing.T) {
	requests := 0
	list, err := Load(ListOptions{Revision: fixtureRevision, Release: "2.9.0", Client: fixtureClient(t, &requests)})
	if err != nil || requests != 4 || list.Official.Release != "2.9.0" {
		t.Fatalf("pinned load: %d %+v %v", requests, list, err)
	}
	// Runner input stays explicit and is never selected by the online default.
	_, err = Load(ListOptions{Executable: "missing", Run: func(context.Context, string, ...string) ([]byte, error) { return nil, fmt.Errorf("missing") }})
	if err == nil || !strings.Contains(err.Error(), "--scummvm") {
		t.Fatalf("local mode changed: %v", err)
	}
}
