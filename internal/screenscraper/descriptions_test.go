package screenscraper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

func TestImportDescriptionsWithNonLFLineBreaksAndResume(t *testing.T) {
	descriptions := map[string]string{
		"A New Game":  "First paragraph.\rSecond paragraph.\r\rLast paragraph.",
		"B Next Game": "First paragraph.\u2028Second paragraph.\u2029Third paragraph.\u0085Last paragraph.",
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systemesListe.php" {
			_, _ = w.Write([]byte(`{"response":{"systemes":[{"id":14,"nom":"Nintendo 64"}]}}`))
			return
		}
		requests++
		title := r.URL.Query().Get("romnom")
		description, ok := descriptions[title]
		if !ok {
			http.Error(w, "unexpected title", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{"jeu": map[string]any{
				"id": 1234, "nom": title,
				"synopsis": []map[string]string{{"langue": "en", "text": description}, {"langue": "fr", "text": "Texte suivant."}},
				"medias":   []map[string]string{{"type": "box-2D", "url": "https://example.test/cover.jpg"}},
			}},
		})
	}))
	defer server.Close()
	root := t.TempDir()
	writeSourcesConfig(t, root, server.URL)
	writeSecrets(t, root, map[string]string{"username": "user", "password": "pass", "dev_id": "dev", "dev_password": "devpass"})
	originals := map[string]string{}
	for title := range descriptions {
		originals[title] = fmt.Sprintf(`title: %q
default_release: %q
manual:
  note: Keep this.
releases:
  %q:
    title: %q
sources:
  launchbox:
    matches:
      - database_id: "existing"
        title: %q
`, title, title+" (USA)", title+" (USA)", title, title)
		writeGame(t, root, "Nintendo - Nintendo 64", title, originals[title])
	}
	result, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if result.WrittenFileCount != 2 || result.MatchedGameCount != 2 || requests != 2 {
		t.Fatalf("unexpected import result: %#v, requests=%d", result, requests)
	}
	saved := map[string][]byte{}
	for title, description := range descriptions {
		path := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", title, "game.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		saved[path] = data
		doc, err := catalogyaml.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		source := catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), "screenscraper")
		match := catalogyaml.Value(source, "matches").Content[0]
		expected := strings.NewReplacer("\r", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n").Replace(description)
		if got := catalogyaml.Value(catalogyaml.Value(match, "descriptions"), "en").Value; got != expected {
			t.Fatalf("description changed: got %q, want %q", got, expected)
		}
		var before, after map[string]any
		if err := yaml.Unmarshal([]byte(originals[title]), &before); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(data, &after); err != nil {
			t.Fatal(err)
		}
		delete(after["sources"].(map[string]any), "screenscraper")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("import changed releases, manual fields or another source")
		}
	}
	resumed, err := Import(ImportOptions{DataRoot: root, Apply: true, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || resumed.SkippedExistingCount != 2 || resumed.WrittenFileCount != 0 {
		t.Fatalf("resume repeated saved lookups: %#v, requests=%d", resumed, requests)
	}
	for path, before := range saved {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("resume modified an already saved card")
		}
	}
}
