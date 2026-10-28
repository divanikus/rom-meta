package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaNamesUsesRealDirectoryBeforeRegistryDefault(t *testing.T) {
	for _, input := range []string{"Nintendo - Wii U", "wiiu"} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			game := filepath.Join(root, "platforms", "Nintendo - Wii U", "Example")
			media := filepath.Join(game, "media")
			if err := os.MkdirAll(media, 0755); err != nil {
				t.Fatal(err)
			}
			body := `title: Example
releases:
  Example (USA): {}
sources:
  launchbox:
    matches:
      - release: Example (USA)
        media:
          box_front:
            - url: https://example.invalid/front.jpg
              region: USA
              source_type: Box - Front
`
			if err := os.WriteFile(filepath.Join(game, "game.yaml"), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(media, "box-front.usa.launchbox.jpg")
			if err := os.WriteFile(old, []byte("image"), 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"--data-root", root, "--platform", input}
			if err := runCatalogMediaNames(args); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(old); err != nil {
				t.Fatal("dry run modified file", err)
			}
			if err := runCatalogMediaNames(append(args, "--apply")); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(media, "box-front.usa.variant-original.launchbox.jpg"))
			if err != nil || string(b) != "image" {
				t.Fatal("real Wii U directory was skipped", err)
			}
			if _, err := os.Stat(old); !os.IsNotExist(err) {
				t.Fatal("legacy file remains", err)
			}
		})
	}
}

func TestMediaNamesDistinguishesDiscAndDigitalAliases(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Nintendo - Wii U", "Nintendo - Wii U (Digital) (CDN)"} {
		if err := os.MkdirAll(filepath.Join(root, "platforms", name), 0755); err != nil {
			t.Fatal(err)
		}
		if got, err := catalogPlatformDirectory(root, name); err != nil || got != name {
			t.Fatal(got, err)
		}
	}
	for alias, want := range map[string]string{"wiiu": "Nintendo - Wii U", "wiiueshop": "Nintendo - Wii U (Digital) (CDN)"} {
		if got, err := catalogPlatformDirectory(root, alias); err != nil || got != want {
			t.Fatal(alias, got, err)
		}
	}
	if _, err := catalogPlatformDirectory(root, "missing-system"); err == nil {
		t.Fatal("unknown platform silently accepted")
	}
}
