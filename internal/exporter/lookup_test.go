package exporter

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestObserveNativeRawZIPAndContainment(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "Game.gg")
	if err := os.WriteFile(raw, []byte("synthetic ROM"), 0600); err != nil {
		t.Fatal(err)
	}
	observed, err := ObserveInput(root, "gamegear", "Game.gg")
	if err != nil || !ValidObservation(observed) || observed.Payload.Member != "" {
		t.Fatal(observed, err)
	}
	archive := filepath.Join(root, "Game.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("Nested/Game.gg")
	_, _ = w.Write([]byte("synthetic ROM"))
	_ = z.Close()
	_ = f.Close()
	zipped, err := ObserveInput(root, "gamegear", "Game.zip")
	if err != nil || !ValidObservation(zipped) || zipped.Payload.SHA1 != observed.Payload.SHA1 || zipped.Outer.SHA256 == zipped.Payload.SHA256 {
		t.Fatal(zipped, err)
	}
	for _, tc := range [][2]string{{"gamegear", "../Game.gg"}, {"gamegear", "/Game.gg"}, {"gamegear", "Game.7z"}, {"ps2", "Game.gg"}} {
		if _, err := ObserveInput(root, tc[0], tc[1]); err == nil {
			t.Fatal(tc)
		}
	}
	if err := os.Symlink(raw, filepath.Join(root, "Link.gg")); err == nil {
		if _, err = ObserveInput(root, "gamegear", "Link.gg"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	f, err = os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z = zip.NewWriter(f)
	for _, name := range []string{"One.gg", "Two.gg"} {
		w, _ = z.Create(name)
		_, _ = w.Write([]byte("ROM"))
	}
	_ = z.Close()
	_ = f.Close()
	if _, err = ObserveInput(root, "gamegear", "Game.zip"); err == nil {
		t.Fatal("ambiguous archive accepted")
	}
}
