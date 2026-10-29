package sourceyaml

import (
	"bytes"
	"errors"
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

func TestWriterCannotReplacePreservedSource(t *testing.T) {
	p := filepath.Join(t.TempDir(), "game.yaml")
	before := []byte("title: Game\nfetch: {preserve_sources: [igdb]}\nsources: {igdb: {status: not_found}, launchbox: {status: not_found}}\n")
	if err := os.WriteFile(p, before, 0600); err != nil {
		t.Fatal(err)
	}
	err := WriteSourceMetadata(p, "igdb", "    matches: [{igdb_id: 99}]\n")
	if !errors.Is(err, ErrSourcePreserved) {
		t.Fatalf("protected write accepted: %v", err)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("protected bytes changed")
	}
	if err := WriteSourceMetadata(p, "launchbox", "    status: refreshed\n"); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(p)
	if !bytes.Contains(after, []byte("preserve_sources")) || !bytes.Contains(after, []byte("igdb:")) {
		t.Fatal("unrelated refresh discarded controls")
	}
}
