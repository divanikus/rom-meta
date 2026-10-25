package exporter

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Synthetic process-index reload, with OS filesystem caches left untouched.
// This compares repeated metadata reads with stat validation plus SQLite summaries.
func BenchmarkLookupPlatformIndex(b *testing.B) {
	root := b.TempDir()
	platform := filepath.Join(root, "platforms", "Example")
	if err := os.MkdirAll(platform, 0700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(platform, "catalog-storage.yaml"), []byte("version: 1\nlayout: game-source-zip\narchive: torrentzip\n"), 0600); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		path := filepath.Join(platform, fmt.Sprintf("Game%03d", i))
		if err := os.MkdirAll(path, 0700); err != nil {
			b.Fatal(err)
		}
		f, err := os.Create(filepath.Join(path, "metadata.zip"))
		if err != nil {
			b.Fatal(err)
		}
		z := zip.NewWriter(f)
		docs := map[string]string{
			"game.yaml":         fmt.Sprintf("title: Game %d\nreleases:\n  Game %d (USA):\n    regions: [USA]\n", i, i),
			"fingerprints.yaml": fmt.Sprintf("items:\n  - release: Game %d (USA)\n    file: {name: Game%d.gg, size: %d, sha1: '%040x'}\n", i, i, i+1, i+1),
		}
		for name, data := range docs {
			w, e := z.Create(name)
			if e != nil {
				b.Fatal(e)
			}
			if _, e = w.Write([]byte(data)); e != nil {
				b.Fatal(e)
			}
		}
		if err = z.Close(); err != nil {
			b.Fatal(err)
		}
		if err = f.Close(); err != nil {
			b.Fatal(err)
		}
	}
	for _, mode := range []string{"source", "index"} {
		b.Run(mode, func(b *testing.B) {
			c := &LookupCatalog{Root: root, Platforms: []LookupPlatform{{Platform: "Example", Supported: true, System: "gamegear"}}}
			if mode == "source" {
				c.CacheDir = "-"
			}
			if _, err := c.Open("Example"); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := c.Open("Example")
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(r.IndexStats.Rebuilt), "rebuilt-games/op")
			}
		})
	}
}
