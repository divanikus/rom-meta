package dat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type arcadeClassificationTransport func(*http.Request) (*http.Response, error)

func (f arcadeClassificationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func arcadeNoNetwork(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: arcadeClassificationTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request: %s", r.URL)
		return nil, errors.New("network disabled")
	})}
}

func arcadeClassificationResponse(body []byte, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
}

func TestArcadeClassificationAutomaticDownloadCacheAndSnapshot(t *testing.T) {
	input, c := arcadeFixture(t)
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	explicit, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, ClassificationPath: c, Fold: true, Client: arcadeNoNetwork(t)})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Fold: true}
	options.Client = &http.Client{Transport: arcadeClassificationTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "www.progettosnaps.net" ||
			r.URL.Query().Get("tipo") != "category" || r.URL.Query().Get("file") != "/support/packs/pS_category_289.zip" {
			t.Fatalf("unexpected download request: %s %s", r.Method, r.URL)
		}
		return arcadeClassificationResponse(data, 200), nil
	})}
	plan, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || plan.Classification.Origin != "download" || plan.Classification.URL == "" {
		t.Fatalf("download provenance: %#v; requests=%d", plan.Classification, calls)
	}
	if plan.Snapshot != explicit.Snapshot || !reflect.DeepEqual(plan.Bindings, explicit.Bindings) || !reflect.DeepEqual(plan.Counts, explicit.Counts) {
		t.Fatal("automatic classification changed import semantics")
	}
	for _, name := range []string{"platforms", "bindings"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("preview wrote %s", name)
		}
	}
	options.Client = arcadeNoNetwork(t)
	options.Offline = true
	cached, err := ImportArcade(input, options)
	if err != nil || cached.Snapshot != plan.Snapshot || cached.Classification.Origin != "cache" {
		t.Fatalf("offline cache: %v %#v", err, cached.Classification)
	}
	options.Apply = true
	applied, err := ImportArcade(input, options)
	if err != nil || !applied.Applied {
		t.Fatalf("cached apply: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".rommeta")); err != nil {
		t.Fatal(err)
	}
	options.Apply = false
	saved, err := ImportArcade(input, options)
	if err != nil || saved.Snapshot != plan.Snapshot || saved.Classification.Origin != "snapshot" {
		t.Fatalf("offline snapshot recovery: %v %#v", err, saved.Classification)
	}
	// A valid accepted snapshot also takes precedence over a broken disposable cache.
	cacheDir := arcadeClassificationCacheDir(root, "0.289")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportArcade(input, options); err != nil {
		t.Fatal(err)
	}
}

func TestArcadeClassificationFailuresDoNotPublishCache(t *testing.T) {
	input, c := arcadeFixture(t)
	good, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := filepath.Join(t.TempDir(), "mismatch.zip")
	arcadeTestZIP(t, mismatch, map[string]string{"mame.ini": ";; MAME.ini 0.288\n[ROOT_FOLDER]\nsf2\n"})
	wrong, err := os.ReadFile(mismatch)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"offline", "network", "404", "html", "truncated", "wrong version", "oversized header", "oversized stream", "http redirect"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, Fold: true, Offline: scenario == "offline"}
			options.Client = &http.Client{Transport: arcadeClassificationTransport(func(r *http.Request) (*http.Response, error) {
				switch scenario {
				case "offline":
					t.Fatal("offline requested network")
				case "network":
					return nil, errors.New("connection unavailable")
				case "404":
					return arcadeClassificationResponse(nil, 404), nil
				case "html":
					return arcadeClassificationResponse([]byte("<html>unavailable</html>"), 200), nil
				case "truncated":
					return arcadeClassificationResponse(good[:len(good)/2], 200), nil
				case "wrong version":
					return arcadeClassificationResponse(wrong, 200), nil
				case "oversized header":
					response := arcadeClassificationResponse(nil, 200)
					response.ContentLength = maxArcadeClassificationDownload + 1
					return response, nil
				case "oversized stream":
					response := arcadeClassificationResponse(bytes.Repeat([]byte("x"), maxArcadeClassificationDownload+1), 200)
					response.ContentLength = -1
					return response, nil
				case "http redirect":
					response := arcadeClassificationResponse(nil, 302)
					response.Header.Set("Location", "http://example.test/data")
					return response, nil
				}
				return nil, errors.New("unexpected request")
			})}
			if _, err := ImportArcade(input, options); err == nil || !strings.Contains(err.Error(), "--classifications") {
				t.Fatalf("missing actionable error: %v", err)
			}
			for _, path := range []string{arcadeClassificationCacheDir(root, "0.289"), filepath.Join(root, "platforms"), filepath.Join(root, "bindings")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("failure published %s", path)
				}
			}
			stages, err := filepath.Glob(filepath.Join(root, ".rommeta", "cache", "arcade", "mame", ".classification-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("temporary downloads left behind: %v %v", stages, err)
			}
		})
	}
}

func TestArcadeClassificationCorruptCacheAndExplicitOverride(t *testing.T) {
	input, c := arcadeFixture(t)
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Client: &http.Client{Transport: arcadeClassificationTransport(func(*http.Request) (*http.Response, error) {
		return arcadeClassificationResponse(data, 200), nil
	})}}
	plan, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	// Change the bytes while keeping a readable ZIP, so the digest matters.
	changed := append(append([]byte{}, data...), '\n')
	if err := os.WriteFile(plan.Classification.Path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	options.Client = arcadeNoNetwork(t)
	if _, err := ImportArcade(input, options); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("corrupt cache accepted: %v", err)
	}
	options.ClassificationPath = c
	if _, err := ImportArcade(input, options); err != nil {
		t.Fatalf("explicit override blocked by cache: %v", err)
	}
}

func TestArcadeClassificationVersionIsolation(t *testing.T) {
	root := t.TempDir()
	_, c := arcadeFixture(t)
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: arcadeClassificationTransport(func(*http.Request) (*http.Response, error) {
		return arcadeClassificationResponse(data, 200), nil
	})}
	if _, err := resolveArcadeClassification("0.289", "", ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Client: client}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.288", "0.290"} {
		if _, err := resolveArcadeClassification(version, "", ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Offline: true, Client: arcadeNoNetwork(t)}); err == nil {
			t.Fatal("another version was silently substituted")
		}
	}
	for _, version := range []string{"", "0.289/../../escape", "1.0.0.02"} {
		if _, err := resolveArcadeClassification(version, "", ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Client: arcadeNoNetwork(t)}); err == nil {
			t.Fatal("invalid version accepted")
		}
	}
}

func TestArcadeUnsupportedAuthoritiesNeverDownload(t *testing.T) {
	for _, input := range []struct{ name, xml, want string }{
		{"fbneo", `<datafile><header><name>FinalBurn Neo - Arcade Games</name><version>1.0.0.02</version><author>FinalBurn Neo</author></header></datafile>`, "FBNeo DAT detected"},
		{"distribution", `<datafile><header><name>MAME 0.289 ROMs (merged)</name></header></datafile>`, "official MAME listxml"},
		{"software", `<softwarelist name="nes"/>`, "official MAME listxml"},
	} {
		t.Run(input.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), input.name+".xml")
			if err := os.WriteFile(path, []byte(input.xml), 0o644); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(t.TempDir(), "untouched")
			_, err := ImportArcade(path, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Client: arcadeNoNetwork(t)})
			if err == nil || !strings.Contains(err.Error(), input.want) {
				t.Fatalf("authority error: %v", err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("unsupported input created cache or catalog")
			}
		})
	}
}

func TestArcadeSavedClassificationAmbiguityAndHash(t *testing.T) {
	_, c := arcadeFixture(t)
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	sourceHash := strings.Repeat("a", 64)
	base := filepath.Join(root, "bindings", "romsets", "mame")
	for i, suffix := range []string{"one", "two"} {
		dir := filepath.Join(base, "0.289-"+suffix)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		contents := append(append([]byte{}, data...), bytes.Repeat([]byte("\n"), i)...)
		if err := os.WriteFile(filepath.Join(dir, "classifications.zip"), contents, 0o644); err != nil {
			t.Fatal(err)
		}
		doc := "source: mame\nsha256: " + sourceHash + "\nclassifications:\n  file: classifications.zip\n  sha256: " + arcadeBytesHash(contents) + "\n"
		if err := os.WriteFile(filepath.Join(dir, "_source.yaml"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root}, Client: arcadeNoNetwork(t)}
	if _, err := resolveArcadeClassification("0.289", sourceHash, options); err == nil || !strings.Contains(err.Error(), "multiple saved") {
		t.Fatalf("ambiguity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "0.289-one", "classifications.zip"), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveArcadeClassification("0.289", sourceHash, options); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("snapshot hash: %v", err)
	}
	options.ClassificationPath = c
	if _, err := resolveArcadeClassification("0.289", sourceHash, options); err != nil {
		t.Fatal(err)
	}
}

func TestArcadeClassificationCacheManifest(t *testing.T) {
	_, c := arcadeFixture(t)
	data, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "classifications.zip"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, meta := range []arcadeClassificationCache{
		{Version: "0.288", URL: arcadeClassificationURL("0.289"), SHA256: arcadeBytesHash(data)},
		{Version: "0.289", URL: "https://example.test/unknown", SHA256: arcadeBytesHash(data)},
		{Version: "0.289", URL: arcadeClassificationURL("0.289")},
	} {
		b, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, "source.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readArcadeClassificationCache(dir, "0.289", arcadeClassificationURL("0.289")); err == nil {
			t.Fatal("untrusted cache metadata accepted")
		}
	}
}
