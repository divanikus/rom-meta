package main

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogcache"
)

type noWorkflowNetwork struct{}

func (noWorkflowNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("operator fixture must remain offline")
}

// Follow the real commands as a person would, including review/preview/apply and
// repeat/resume. No provider accounts, private catalog, plan JSON or hash editing.
func TestHumanImportEnrichFoldExportWorkflow(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "catalog")
	roms := filepath.Join(root, "roms")
	if err := os.MkdirAll(roms, 0700); err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = noWorkflowNetwork{}
	defer func() { http.DefaultTransport = originalTransport }()
	run := func(args ...string) string {
		t.Helper()
		code, out := commandOutput(t, args...)
		if code != 0 {
			t.Fatalf("%v exit %d\n%s", args, code, out)
		}
		return out
	}
	run("init", data)
	names := []string{"Operator Game - The Quest (USA)", "Operator Game, The Quest (Japan)"}
	payloads := [][]byte{bytes.Repeat([]byte{1}, 256), bytes.Repeat([]byte{2}, 256)}
	var dat strings.Builder
	dat.WriteString(`<datafile><header><name>Sega - Game Gear</name></header>`)
	for i, name := range names {
		payload := payloads[i]
		if err := os.WriteFile(filepath.Join(roms, name+".gg"), payload, 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&dat, `<game name="%s"><description>%s</description><rom name="%s.gg" size="%d" crc="%08x" md5="%x" sha1="%x"/></game>`, name, name, name, len(payload), crc32.ChecksumIEEE(payload), md5.Sum(payload), sha1.Sum(payload))
	}
	dat.WriteString(`</datafile>`)
	datPath := filepath.Join(root, "games.dat")
	if err := os.WriteFile(datPath, []byte(dat.String()), 0600); err != nil {
		t.Fatal(err)
	}
	run("dat", "plan", datPath)
	run("dat", "import", datPath, "--data-root", data)
	if entries, _ := os.ReadDir(filepath.Join(data, "platforms")); len(entries) != 0 {
		t.Fatal("preview wrote platforms")
	}
	run("dat", "import", datPath, "--data-root", data, "--apply")
	platform := filepath.Join(data, "platforms", "Sega - Game Gear")
	folders := func() []string {
		t.Helper()
		entries, err := os.ReadDir(platform)
		if err != nil {
			t.Fatal(err)
		}
		var result []string
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), "_") && !strings.HasPrefix(entry.Name(), ".") {
				result = append(result, entry.Name())
			}
		}
		return result
	}
	if got := folders(); len(got) != 2 {
		t.Fatalf("need two imported regional cards: %v", got)
	}
	snapshot := filepath.Join(data, ".rommeta", "sources", "launchbox", "Metadata.zip")
	if err := os.MkdirAll(filepath.Dir(snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	member, err := zw.Create("Metadata.xml")
	if err != nil {
		t.Fatal(err)
	}
	body := `<LaunchBox><Game><DatabaseID>42</DatabaseID><Name>Operator Game: The Quest</Name><Platform>Sega Game Gear</Platform><Developer>Example Studio</Developer><Overview>Offline operator fixture.</Overview></Game></LaunchBox>`
	if _, err := member.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	run("sources", "list", "--data-root", data)
	fetch := []string{"fetch", "--data-root", data, "--platform", "gamegear", "--source", "launchbox", "--type", "metadata"}
	run(fetch...)
	for _, folder := range folders() {
		g, _, err := catalogcache.ReadGameDocument(data, "Sega - Game Gear", folder)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.SourceStats) != 0 {
			t.Fatal("fetch preview wrote metadata")
		}
	}
	run(append(fetch, "--apply")...)
	for _, folder := range folders() {
		g, _, err := catalogcache.ReadGameDocument(data, "Sega - Game Gear", folder)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.SourceStats) != 1 || g.SourceStats[0].MatchCount == 0 {
			t.Fatalf("unmatched fixture %s: %+v", folder, g.SourceStats)
		}
	}
	run(append(fetch, "--apply")...) // Normal resume must work with no network.
	preview := run("catalog", "fold", "--data-root", data, "--platform", "gamegear", "--details")
	if len(folders()) != 2 {
		t.Fatal("fold preview mutated cards")
	}
	input, err := os.CreateTemp(root, "answers")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previousInput := os.Stdin
	os.Stdin = input
	review := func() string {
		defer func() { os.Stdin = previousInput; input.Close() }()
		return run("catalog", "fold", "--data-root", data, "--platform", "gamegear", "--interactive")
	}()
	if got := folders(); len(got) != 1 {
		t.Fatalf("ordinary interactive fold did not merge regional cards: %v\nPREVIEW:\n%s\nREVIEW:\n%s", got, preview, review)
	}
	game, err := catalogcache.ReadGameBundle(data, "Sega - Game Gear", folders()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Releases) != 2 || len(game.Fingerprints) != 2 {
		t.Fatalf("fold lost releases/proofs: %+v", game)
	}
	run("validate", data)
	output := filepath.Join(root, "export")
	export := []string{"export", "--target", "es-de", "--data-root", data, "--platform", "gamegear", "--rom-root", roms, "--out", output}
	run(export...)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("export preview wrote output", err)
	}
	run(append(export, "--apply")...)
	var list []byte
	err = filepath.WalkDir(output, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == "gamelist.xml" {
			list, err = os.ReadFile(path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var games struct {
		Games []struct {
			Path        string `xml:"path"`
			Description string `xml:"desc"`
		} `xml:"game"`
	}
	if err := xml.Unmarshal(list, &games); err != nil {
		t.Fatal(err)
	}
	if len(games.Games) != 2 {
		t.Fatal(string(list))
	}
	for _, g := range games.Games {
		if g.Description != "Offline operator fixture." {
			t.Fatal("metadata missing from export", string(list))
		}
	}
	for i, name := range names {
		actual, err := os.ReadFile(filepath.Join(roms, name+".gg"))
		if err != nil || !bytes.Equal(actual, payloads[i]) {
			t.Fatal("source ROM changed", name, err)
		}
	}
}
