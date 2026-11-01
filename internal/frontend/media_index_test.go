package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaDirectoriesAreIndexedOncePerPlan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "art")
	put(t, root, "art/First-01.png", "one")
	put(t, root, "art/Second-02.png", "two")
	p := &Plan{}
	if ok, err := p.hasArt(dir, []string{"First"}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// Renaming the directory makes a repeated directory walk observably wrong.
	// A new plan sees current disk contents; this plan uses its initial index.
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if ok, err := p.hasArt(dir, []string{"Second"}); err != nil || !ok {
		t.Fatal("art directory reread", ok, err)
	}
	p = &Plan{}
	if ok, err := p.esdeMediaPresent(filepath.Join(dir+"-moved", "First-01.jpg")); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := os.Rename(dir+"-moved", dir); err != nil {
		t.Fatal(err)
	}
	if ok, err := p.esdeMediaPresent(filepath.Join(dir+"-moved", "Second-02.webp")); err != nil || !ok {
		t.Fatal("ES-DE directory reread", ok, err)
	}
	pkg := t.TempDir()
	relative := "Images/" + platform + "/Box - Front"
	put(t, pkg, relative+"/"+a+"-01.png", "a")
	put(t, pkg, relative+"/"+b+"-01.png", "b")
	p = &Plan{Package: pkg}
	if got, err := p.packageMedia(relative, a); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if err := os.Remove(filepath.Join(pkg, filepath.FromSlash(relative), b+"-01.png")); err != nil {
		t.Fatal(err)
	}
	if got, err := p.packageMedia(relative, b); err != nil || len(got) != 1 {
		t.Fatal("incoming media directory reread", got, err)
	}
}
func BenchmarkArtworkLookup(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 256; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Game %d-01.png", i)), []byte("art"), 0644); err != nil {
			b.Fatal(err)
		}
	}
	for _, indexed := range []bool{false, true} {
		b.Run(map[bool]string{false: "directory-per-card", true: "shared-index"}[indexed], func(b *testing.B) {
			p := &Plan{}
			if _, err := p.hasArt(dir, []string{"Game 128"}); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !indexed {
					p.artDirs = nil
				}
				if ok, err := p.hasArt(dir, []string{"Game 128"}); err != nil || !ok {
					b.Fatal(ok, err)
				}
			}
		})
	}
}
