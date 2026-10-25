package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"rommeta/internal/appinfo"
)

func commandOutput(t *testing.T, args ...string) (int, string) {
	t.Helper()
	savedArgs, savedOut, savedErr := os.Args, os.Stdout, os.Stderr
	file, err := os.CreateTemp(t.TempDir(), "cli")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	defer func() { os.Args, os.Stdout, os.Stderr = savedArgs, savedOut, savedErr }()
	os.Args = append([]string{"rom-meta"}, args...)
	os.Stdout, os.Stderr = file, file
	code := runCommand()
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(b)
}
func TestHumanCLIEntrypoints(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}, {"--help"}, {"serve", "--help"}, {"help", "fetch"}, {"help", "catalog", "fold"}, {"fetch", "--data-root", "unused", "--help"}, {"dat", "import", "unused.dat", "--help"}, {"dat", "plan", "unused.dat", "--help"}, {"catalog", "fold", "--platform", "unused", "--help"}} {
		code, out := commandOutput(t, args...)
		if code != 0 || strings.TrimSpace(out) == "" {
			t.Fatal(args, code, out)
		}
		if args[0] == "--version" || args[0] == "version" {
			if strings.TrimSpace(out) != appinfo.DisplayName() {
				t.Fatal(out)
			}
		}
	}
	for _, args := range [][]string{{"validate", "--data-root", "example"}, {"validate", "one", "two"}, {"version", "extra"}} {
		code, out := commandOutput(t, args...)
		if code != 2 || strings.TrimSpace(out) == "" {
			t.Fatal(args, code, out)
		}
	}
}
