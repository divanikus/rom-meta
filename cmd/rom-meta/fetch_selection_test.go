package main

import (
	"testing"

	"rommeta/internal/store"
)

func TestFetchAcceptsCommaSeparatedSelections(t *testing.T) {
	for _, args := range [][]string{
		{"--type", "artwork, ", "--source", "launchbox, igdb"},
		{"--type=artwork,", "--source=launchbox,igdb"},
	} {
		root := t.TempDir()
		if _, err := store.Init(root, store.InitOptions{}); err != nil {
			t.Fatal(err)
		}
		command := append([]string{"--data-root", root}, args...)
		if err := runFetch(command); err != nil {
			t.Fatalf("comma selections rejected: %v", err)
		}
	}
}
