package main

import (
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/scummvm"
)

func TestScummVMCommandCreatesCatalogAndReplays(t *testing.T) {
	root := filepath.Join(t.TempDir(), "catalog")
	list := filepath.Join(t.TempDir(), "games.txt")
	if err := os.WriteFile(list, []byte("scumm:tentacle Day of the Tentacle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "--data-root", root, "--list-file", list, "--version", "ScummVM test"}
	if err := runScummVM(args); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("preview wrote catalog")
	}
	if err := runScummVM(append(args, "--apply")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "platforms", scummvm.Platform))
	if err != nil || len(entries) != 3 {
		t.Fatalf("command failed to create platform: %v %v", entries, err)
	}
	if err := runScummVM(append(args, "--apply")); err != nil {
		t.Fatal(err)
	}
}

func TestScummVMImportOptionErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--unknown"}, {"--data-root"}, {"--engine", "--apply"}, {"--scummvm="},
		{"--scummvm", "scummvm", "--list-file", "games.txt"},
	} {
		if _, _, err := parseScummVMImportOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	options, help, err := parseScummVMImportOptions([]string{"--data-root=example", "--engine=scumm,sci", "--apply"})
	if err != nil || help || options.DataRoot != "example" || len(options.Engines) != 1 || !options.Apply {
		t.Fatalf("incorrect options: %+v %v", options, err)
	}
	if err := runScummVM([]string{"unknown"}); err == nil {
		t.Fatal("unknown subcommand accepted")
	}
	for _, args := range [][]string{nil, {"--help"}, {"import", "--help"}} {
		if err := runScummVM(args); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScummVMOfficialSourceOptions(t *testing.T) {
	options, help, err := parseScummVMImportOptions([]string{"--release=2026.4.0", "--revision", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--offline", "--engine", "scumm"})
	if err != nil || help || options.Release != "2026.4.0" || !options.Offline || options.Revision == "" {
		t.Fatalf("official options not parsed: %+v %v", options, err)
	}
	for _, args := range [][]string{{"--release"}, {"--revision="}} {
		if _, _, err := parseScummVMImportOptions(args); err == nil {
			t.Fatalf("accepted missing value: %v", args)
		}
	}
}

func TestScummVMRenameFoldersCommand(t *testing.T) {
	root := t.TempDir()
	if err := runScummVM([]string{"rename-folders", "--data-root", root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".rommeta")); !os.IsNotExist(err) {
		t.Fatal("rename preview wrote runtime state")
	}
	for _, args := range [][]string{{"rename-folders", "--unknown"}, {"rename-folders", "extra"}} {
		if err := runScummVM(args); err == nil {
			t.Fatalf("accepted invalid rename command: %v", args)
		}
	}
	if err := runScummVM([]string{"rename-folders", "--help"}); err != nil {
		t.Fatal(err)
	}
}
