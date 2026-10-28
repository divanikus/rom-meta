package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogFoldUsesRealWiiUDirectory(t *testing.T) {
	for _, input := range []string{"Nintendo - Wii U", "wiiu", "nintendo - wii u"} {
		for _, apply := range []bool{false, true} {
			t.Run(input+map[bool]string{false: "/preview", true: "/apply"}[apply], func(t *testing.T) {
				root := t.TempDir()
				for _, folder := range []string{"Example USA", "Example Japan"} {
					dir := filepath.Join(root, "platforms", "Nintendo - Wii U", folder)
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					body := "title: Example\nreleases:\n  " + folder + ": {}\nsources:\n  launchbox:\n    matches:\n      - database_id: '123'\n        title: Example\n"
					if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(body), 0644); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"--data-root", root, "--platform", input, "--details"}
				if apply {
					args = append(args, "--apply")
				}
				if err := runCatalogFold(args); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(filepath.Join(root, "platforms", "Nintendo - Wii U"))
				if err != nil {
					t.Fatal(err)
				}
				want := 2
				if apply {
					want = 1
				}
				if len(entries) != want {
					t.Fatalf("got %d cards, want %d; real Wii U catalog may have been skipped", len(entries), want)
				}
			})
		}
	}
}

func TestCatalogFoldDistinguishesDiscAndDigitalPlatforms(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Nintendo - Wii U", "Nintendo - Wii U (Digital) (CDN)"} {
		if err := os.MkdirAll(filepath.Join(root, "platforms", name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ input, want string }{{"missing-system", "no catalog platform directory"}} {
		if err := runCatalogFold([]string{"--data-root", root, "--platform=" + tc.input}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want %s", tc.input, err, tc.want)
		}
	}
	for _, name := range []string{"Nintendo - Wii U", "Nintendo - Wii U (Digital) (CDN)", "wiiu", "wiiueshop"} {
		if err := runCatalogFold([]string{"--data-root", root, "--platform", name}); err != nil {
			t.Fatal(err)
		}
	}
}
