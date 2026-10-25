package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
	"rommeta/internal/dat"
)

func writeDATDispatchZIP(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	for name, value := range entries {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func datDispatchFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.zip")
	writeDATDispatchZIP(t, source, map[string]string{
		"a-home.dat": `<datafile><header><name>Nintendo - Nintendo 64</name><author>No-Intro</author></header><game name="Example (USA)"><rom name="Example (USA).z64" size="16" crc="12345678"/></game></datafile>`,
		"selected.dat": `<mame build="0.289 (test)">
<machine name="sf2" sourcefile="capcom/cps1.cpp"><description>Street Fighter II (World)</description><rom name="sf2.bin" size="4" crc="12345678"/></machine>
<machine name="sf2ua" sourcefile="capcom/cps1.cpp" cloneof="sf2"><description>Street Fighter II (USA)</description><rom name="sf2ua.bin" size="4" crc="23456789"/></machine>
<machine name="nes"><description>NES</description></machine>
</mame>`,
	})
	category := filepath.Join(dir, "category.zip")
	writeDATDispatchZIP(t, category, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\nsf2\nsf2ua\n",
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nnes\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Fighter / Versus]\nsf2\nsf2ua\n[Game Console / Home Videogame]\nnes\n",
	})
	return source, category
}

func TestDATImportDetectsMAMEAndUsesCurrentDirectory(t *testing.T) {
	for _, command := range []string{"import", "arcade"} {
		t.Run(command, func(t *testing.T) {
			source, classifications := datDispatchFixture(t)
			root := t.TempDir()
			t.Chdir(root)
			args := []string{command, source, "--member", "selected.dat", "--classifications", classifications, "--offline", "--report", "preview.json"}
			if err := runDAT(args); err != nil {
				t.Fatal(err)
			}
			var preview dat.ArcadeResult
			b, err := os.ReadFile("preview.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(b, &preview); err != nil {
				t.Fatal(err)
			}
			if preview.Counts["games"] != 1 || preview.Counts["selected"] != 2 || preview.Counts["excluded"] != 1 {
				t.Fatalf("MAME did not use classified default fold: %#v", preview.Counts)
			}
			if _, err := os.Stat("platforms"); !os.IsNotExist(err) {
				t.Fatal("preview wrote cards")
			}
			args = []string{command, source, "--member=selected.dat", "--classifications=" + classifications, "--offline", "--apply"}
			if err := runDAT(args); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join("platforms", "Arcade", "Street Fighter II", "game.yaml")); err != nil {
				t.Fatal(err)
			}
			issues, err := catalog.Validate(root)
			if err != nil || len(issues) != 0 {
				t.Fatalf("validation: %v %v", err, issues)
			}
			// No classification flag needed for subsequent imports; use the accepted snapshot.
			if err := runDAT([]string{command, source, "--member", "selected.dat", "--offline"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDATImportMAMEExplicitRootSelectionAndNoFold(t *testing.T) {
	source, classifications := datDispatchFixture(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	target := t.TempDir()
	report := filepath.Join(target, "plan.json")
	if err := runDATImport([]string{source, "--member", "selected.dat", "--data-root", target,
		"--platform", "Arcade", "--classifications", classifications, "--offline", "--no-fold",
		"--sets", "sf2,sf2ua", "--report", report, "--apply"}); err != nil {
		t.Fatal(err)
	}
	var result dat.ArcadeResult
	b, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Counts["games"] != 2 || result.Fold {
		t.Fatalf("MAME flags ignored: %#v", result.Counts)
	}
	if _, err := os.Stat(filepath.Join(cwd, "platforms")); !os.IsNotExist(err) {
		t.Fatal("explicit root ignored")
	}
	issues, err := catalog.Validate(target)
	if err != nil || len(issues) != 0 {
		t.Fatalf("validation: %v %v", err, issues)
	}
}

func TestDATImportGenericPathRemainsAvailable(t *testing.T) {
	source, _ := datDispatchFixture(t)
	root := t.TempDir()
	t.Chdir(root)
	// Filename does not control the format; the selected member does.
	misleading := filepath.Join(t.TempDir(), "mame.zip")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(misleading, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDATImport([]string{misleading, "--member", "a-home.dat", "--offline", "--apply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Example", "game.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "platforms", "Arcade")); !os.IsNotExist(err) {
		t.Fatal("filename routed ordinary DAT to MAME")
	}
}

func TestDATImportRejectsInapplicableOptions(t *testing.T) {
	source, _ := datDispatchFixture(t)
	root := t.TempDir()
	t.Chdir(root)
	for _, args := range [][]string{
		{source, "--member", "selected.dat", "--platform", "Nintendo - Nintendo 64", "--offline", "--apply"},
		{source, "--member", "a-home.dat", "--sets", "sf2", "--apply"},
		{source, "--member", "a-home.dat", "--no-fold", "--apply"},
		{source, "--member", "selected.dat", "--data-root="},
	} {
		if err := runDATImport(args); err == nil {
			t.Fatalf("inapplicable options accepted: %v", args)
		}
	}
	if _, err := os.Stat("platforms"); !os.IsNotExist(err) {
		t.Fatal("rejected options wrote cards")
	}
	if err := runDATImport([]string{source, "--member", "selected.dat", "--offline"}); err == nil || !strings.Contains(err.Error(), "no local classifications") {
		t.Fatalf("MAME without classifications was not automatically dispatched: %v", err)
	}
}
