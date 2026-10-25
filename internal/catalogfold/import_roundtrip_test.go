package catalogfold

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/dat"
)

func contentDAT(t *testing.T, names ...string) string {
	t.Helper()
	text := `<datafile><header><name>Sony - PlayStation 2</name><author>redump.org</author></header>`
	for i, name := range names {
		text += fmt.Sprintf(`<game name="%s"><rom name="track.bin" size="1" sha1="%040d"/></game>`, name, i+1)
	}
	path := filepath.Join(t.TempDir(), "content.dat")
	writeFoldFile(t, path, text+`</datafile>`)
	return path
}
func readContentMap(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(readFoldFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func writeContentMap(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	err := encoder.Encode(doc)
	raw := buffer.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}
func contentFiles(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		result[rel] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestImportFoldReimportPreservesRegionalReleasesAndMedia(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit (USA)", "Tsuki (Japan)", "Orbit (Europe) (Demo)", "Orbit (USA) (Promo)", "Tsuki (Japan) (Beta) (2001-01-01)", "Tsuki (Japan) (Beta) (2001-02-03)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	first, err := dat.Import(path, options)
	if err != nil || first.GameBundleCount != 2 {
		t.Fatal(err, first)
	}
	platform := filepath.Join(root, "platforms", "Sony - PlayStation 2")
	expectedReleases := map[string]any{}
	expectedFP := map[string]any{}
	for _, folder := range []string{"Orbit", "Tsuki"} {
		gamePath := filepath.Join(platform, folder, "game.yaml")
		doc := readContentMap(t, gamePath)
		releases := doc["releases"].(map[string]any)
		for key, value := range releases {
			release := value.(map[string]any)
			release["curated_note"] = "keep " + folder
			release["authority"].(map[string]any)["curated_reference"] = "reference " + folder
			expectedReleases[key] = value
		}
		if folder == "Orbit" {
			doc["sources"] = map[string]any{"launchbox": map[string]any{"matches": []any{map[string]any{"database_id": "10", "title": "Orbit", "titles": []any{map[string]any{"title": "Tsuki", "region": "Japan"}}}}}}
		}
		writeContentMap(t, gamePath, doc)
		fpPath := filepath.Join(platform, folder, "fingerprints.yaml")
		fps := readContentMap(t, fpPath)
		for _, item := range fps["items"].([]any) {
			value := item.(map[string]any)
			value["curated_note"] = "hash " + folder
			expectedFP[value["id"].(string)] = value
		}
		writeContentMap(t, fpPath, fps)
		if err = os.MkdirAll(filepath.Join(platform, folder, "media"), 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"cover.jpg": folder + " artwork", "same.jpg": "identical artwork"} {
			if err = os.WriteFile(filepath.Join(platform, folder, "media", name), []byte(value), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	folded, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || folded.AppliedGroupCount != 1 {
		t.Fatal(err, folded)
	}
	kept := filepath.Join(platform, folded.Groups[0].Keep.Folder)
	// Provider taxonomy absent from DAT must survive exact-authority refresh too.
	annotated := readContentMap(t, filepath.Join(kept, "game.yaml"))
	annotated["sources"].(map[string]any)["launchbox"].(map[string]any)["matches"].([]any)[0].(map[string]any)["game_type"] = "Expanded Game"
	writeContentMap(t, filepath.Join(kept, "game.yaml"), annotated)
	mediaBefore := contentFiles(t, filepath.Join(kept, "media"))
	sourcesBefore := readContentMap(t, filepath.Join(kept, "game.yaml"))["sources"]
	if len(mediaBefore) != 3 {
		t.Fatal("differing or identical artwork was lost", mediaBefore)
	}
	for i := 0; i < 2; i++ {
		result, err := dat.Import(path, options)
		if err != nil || result.Changes.Reconciled != 2 || result.Changes.Added != 0 {
			t.Fatal(err, result)
		}
		game := readContentMap(t, filepath.Join(kept, "game.yaml"))
		if !reflect.DeepEqual(expectedReleases, game["releases"]) {
			t.Fatal("release payload changed", game["releases"])
		}
		fps := readContentMap(t, filepath.Join(kept, "fingerprints.yaml"))
		actual := map[string]any{}
		for _, item := range fps["items"].([]any) {
			value := item.(map[string]any)
			actual[value["id"].(string)] = value
		}
		if !reflect.DeepEqual(expectedFP, actual) {
			t.Fatal("fingerprints changed", actual)
		}
		if !reflect.DeepEqual(mediaBefore, contentFiles(t, filepath.Join(kept, "media"))) {
			t.Fatal("media changed during import")
		}
		if !reflect.DeepEqual(sourcesBefore, game["sources"]) {
			t.Fatal("provider metadata lost")
		}
	}
	extended := contentDAT(t, append(names, "Tsuki (Europe)")...)
	if result, err := dat.Import(extended, options); err != nil || len(result.Changes.Conflicts) != 0 {
		t.Fatal(err, result)
	}
	game := readContentMap(t, filepath.Join(kept, "game.yaml"))
	if len(game["releases"].(map[string]any)) != len(names)+1 {
		t.Fatal("new regional release overwritten")
	}
	again, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || again.CandidateGroupCount != 0 {
		t.Fatal(err, again)
	}
}
func TestImportRefusesMixedLegacyCardWithoutWrites(t *testing.T) {
	root := t.TempDir()
	path := contentDAT(t, "Orbit (USA)")
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	if _, err := dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	platform := filepath.Join(root, "platforms", "Sony - PlayStation 2")
	gamePath := filepath.Join(platform, "Orbit", "game.yaml")
	doc := readContentMap(t, gamePath)
	doc["releases"].(map[string]any)["Orbit (Japan) (DLC)"] = map[string]any{"tags": []any{"dlc"}}
	writeContentMap(t, gamePath, doc)
	before := contentFiles(t, platform)
	result, err := dat.Import(path, options)
	if err == nil || len(result.Changes.Conflicts) != 1 || !strings.Contains(result.Changes.Conflicts[0], "mixed product") {
		t.Fatal(err, result)
	}
	if !reflect.DeepEqual(before, contentFiles(t, platform)) {
		t.Fatal("rejected import modified files")
	}
}

func TestReimportReplaysKnownReviewedEditionOwnership(t *testing.T) {
	root := t.TempDir()
	names := []string{"Orbit (USA)", "Orbit (Japan)"}
	path := contentDAT(t, names...)
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	if _, err := dat.Import(path, options); err != nil {
		t.Fatal(err)
	}
	platform := filepath.Join(root, "platforms", "Sony - PlayStation 2")
	old := filepath.Join(platform, "Orbit")
	game := readContentMap(t, filepath.Join(old, "game.yaml"))
	fps := readContentMap(t, filepath.Join(old, "fingerprints.yaml"))
	for i, folder := range []string{"Orbit", "Reviewed Edition"} {
		name := names[i]
		part := map[string]any{"title": "Orbit", "releases": map[string]any{name: game["releases"].(map[string]any)[name]}, "fold": map[string]any{"content_key": folder}}
		writeFoldFile(t, filepath.Join(platform, folder, "game.yaml"), "title: placeholder\n")
		writeContentMap(t, filepath.Join(platform, folder, "game.yaml"), part)
		var items []any
		for _, item := range fps["items"].([]any) {
			if item.(map[string]any)["release"] == name {
				items = append(items, item)
			}
		}
		writeFoldFile(t, filepath.Join(platform, folder, "fingerprints.yaml"), "items: []\n")
		writeContentMap(t, filepath.Join(platform, folder, "fingerprints.yaml"), map[string]any{"items": items})
		writeFoldFile(t, filepath.Join(platform, folder, "media", "cover.jpg"), folder+" artwork")
	}
	media := map[string]map[string][32]byte{}
	for _, folder := range []string{"Orbit", "Reviewed Edition"} {
		media[folder] = contentFiles(t, filepath.Join(platform, folder, "media"))
	}
	for pass := 0; pass < 2; pass++ {
		result, err := dat.Import(path, options)
		if err != nil || result.Changes.Reconciled != 2 || result.Changes.Added != 0 {
			t.Fatal(err, result)
		}
		for i, folder := range []string{"Orbit", "Reviewed Edition"} {
			current := readContentMap(t, filepath.Join(platform, folder, "game.yaml"))
			if current["fold"].(map[string]any)["content_key"] != folder || len(current["releases"].(map[string]any)) != 1 || !reflect.DeepEqual(current["releases"].(map[string]any)[names[i]], game["releases"].(map[string]any)[names[i]]) || !reflect.DeepEqual(media[folder], contentFiles(t, filepath.Join(platform, folder, "media"))) {
				t.Fatal("reviewed ownership or media lost", current)
			}
		}
	}
	before := contentFiles(t, platform)
	result, err := dat.Import(contentDAT(t, append(names, "Orbit (Europe)")...), options)
	if err == nil || len(result.Changes.Conflicts) == 0 || !reflect.DeepEqual(before, contentFiles(t, platform)) {
		t.Fatal("unowned new release guessed across reviewed editions", err, result)
	}
}

func TestReimportKeepsReviewedDevelopmentManufacturingDetails(t *testing.T) {
	root := t.TempDir()
	path := contentDAT(t, "Orbit (Japan) (Disc 1) (Part One) (1M)", "Orbit (Japan) (Demo) (2MM1)")
	options := dat.ImportOptions{DataRoot: root, Apply: true}
	first, err := dat.Import(path, options)
	if err != nil || first.GameBundleCount != 2 {
		t.Fatal(err, first)
	}
	platform := filepath.Join(root, "platforms", "Sony - PlayStation 2")
	entries, err := os.ReadDir(platform)
	if err != nil {
		t.Fatal(err)
	}
	var folders []string
	parent := ""
	for _, e := range entries {
		if e.IsDir() {
			folders = append(folders, e.Name())
			g := readContentMap(t, filepath.Join(platform, e.Name(), "game.yaml"))
			for name := range g["releases"].(map[string]any) {
				if !strings.Contains(name, "(Demo)") {
					parent = e.Name()
				}
			}
		}
	}
	group := reviewedFixture(t, root, "Sony - PlayStation 2", folders...)
	group.DevelopmentParent = parent
	if _, err = Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, group), Apply: true}); err != nil {
		t.Fatal(err)
	}
	before := readContentMap(t, filepath.Join(platform, parent, "game.yaml"))["releases"]
	result, err := dat.Import(path, options)
	if err != nil || result.Changes.Reconciled != 2 || !reflect.DeepEqual(before, readContentMap(t, filepath.Join(platform, parent, "game.yaml"))["releases"]) {
		t.Fatal("known manufacturing variants split on refresh", err, result)
	}
}
