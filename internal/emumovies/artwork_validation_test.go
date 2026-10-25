package emumovies

import (
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestArtworkCopyRejectsZeroFilledCachedImage(t *testing.T) {
	for _, packed := range []bool{false, true} {
		name := "loose"
		if packed {
			name = "packed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			platform := filepath.Join(root, "platforms", "Example")
			game := filepath.Join(platform, "Game")
			if e := os.MkdirAll(game, 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(game, "game.yaml"), []byte("title: Game\n"), 0644); e != nil {
				t.Fatal(e)
			}
			if packed {
				catalogfstest.Pack(t, platform)
			}
			source := filepath.Join(root, "broken.png")
			if e := os.WriteFile(source, make([]byte, 128<<10), 0644); e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(game, "media", "cart-3d.emumovies.png")
			if e := copyFile(source, target); e == nil {
				t.Fatal("zero-filled artwork was accepted")
			}
			if _, e := catalogfs.Stat(target); !os.IsNotExist(e) {
				t.Fatalf("invalid artwork published: %v", e)
			}
			if _, e := os.Stat(target + ".tmp"); !os.IsNotExist(e) {
				t.Fatalf("temporary invalid image remains: %v", e)
			}
		})
	}
}

func TestArtworkMaterializationContinuesAfterInvalidSource(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "platforms", "Example", "Game")
	if e := os.MkdirAll(game, 0755); e != nil {
		t.Fatal(e)
	}
	broken := artworkFileCandidate{Platform: "Example", Game: "Game", GamePath: game, MediaType: "logo", SourcePackage: "art.zip", SourcePath: "broken.png", Target: filepath.Join(game, "media", "broken.emumovies.png")}
	good := broken
	good.SourcePath = "good.png"
	good.Target = filepath.Join(game, "media", "good.emumovies.png")
	if e := os.MkdirAll(filepath.Dir(broken.SourcePathAbs()), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(broken.SourcePathAbs(), make([]byte, 1024), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(good.SourcePathAbs(), []byte("synthetic image"), 0644); e != nil {
		t.Fatal(e)
	}
	result, e := materializeArtworkFiles([]artworkFileCandidate{broken, good})
	if e != nil || result.invalid != 1 || result.written != 1 || len(result.failures) != 1 {
		t.Fatalf("unexpected materialization: %+v %v", result, e)
	}
	if _, e := catalogfs.Stat(broken.Target); !os.IsNotExist(e) {
		t.Fatalf("invalid source published: %v", e)
	}
	data, e := catalogfs.ReadFile(good.Target)
	if e != nil || string(data) != "synthetic image" {
		t.Fatalf("valid source not published: %q %v", data, e)
	}
}
