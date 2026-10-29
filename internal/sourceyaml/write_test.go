package sourceyaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataTemporaryFilenameIsNotHidden(t *testing.T) {
	dir := t.TempDir()
	file, err := os.CreateTemp(dir, metadataTempPattern)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(file.Name())
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".tmp") {
		t.Fatalf("unsafe metadata temporary name: %q", name)
	}
}

func TestWriteSourceMetadataCreatesAndOverwritesWithoutTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.yaml")
	for _, value := range []string{"first", "second"} {
		if err := WriteSourceMetadata(path, "test", "    value: "+value+"\n"); err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(contents), "value: "+value) {
			t.Fatalf("source write contents=%q, error=%v", contents, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != "game.yaml" {
			t.Fatalf("unexpected files after write: %v, error=%v", entries, err)
		}
	}
}

func TestWriteSourceMetadataPreservesOtherCatalogBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.yaml")
	original := "# human note\ntitle: Game\nmanual:\n  note: keep\nsources:\n  igdb:\n    status: not_found\n  launchbox:\n    value: keep\nreleases:\n  Game: {}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteSourceMetadata(path, "igdb", "    matches: 1\n"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, wanted := range []string{"# human note", "manual:\n  note: keep", "  launchbox:\n    value: keep", "  igdb:\n    matches: 1", "releases:\n  Game: {}"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("missing %q:\n%s", wanted, text)
		}
	}
}
