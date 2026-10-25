package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

func TestNativeProjectionAcceptsFullSystemAndLargeTransport(t *testing.T) {
	sha1 := strings.Repeat("a", 40)
	card := catalogapi.Card{
		Game:         catalogapi.Document{YAML: "releases:\n  Game (USA): {}\n"},
		Fingerprints: catalogapi.Document{YAML: "items:\n  - id: no-intro.1\n    release: Game (USA)\n    source: {kind: dat, name: No-Intro}\n    file:\n      size: 4\n      sha1: " + sha1 + "\n      crc32: 01234567\n"},
	}
	selection := catalogapi.Selection{SchemaVersion: 1, Cards: map[string]catalogapi.Card{"card": card}}
	// A realistic transport can exceed 64 MiB through many source documents.
	// This unselected card exercises decoding without making YAML parsing costly.
	selection.Cards["large-source"] = catalogapi.Card{Game: catalogapi.Document{YAML: strings.Repeat("x", (64<<20)+1)}}
	for i := 0; i < 1005; i++ {
		record := exporter.Record{ROM: fmt.Sprintf("Game %04d.gg", i), Game: "card", Release: "Game (USA)", Status: "matched", MatchedBy: "hash", Size: 4, SHA1: sha1}
		proof := exporter.PayloadProof{Size: 4, SHA1: sha1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567"}
		selection.Inputs = append(selection.Inputs, catalogapi.Input{Record: record, Payload: proof})
	}
	root := t.TempDir()
	in, err := os.Create(filepath.Join(root, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := json.NewEncoder(in).Encode(map[string]any{"selection": selection, "before": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	info, _ := in.Stat()
	if info.Size() <= 64<<20 {
		t.Fatal("fixture did not cross original response bound")
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(root, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	originalIn, originalOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	defer func() { os.Stdin, os.Stdout = originalIn, originalOut }()
	if err := runRomMProject(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		ROM       string `json:"rom"`
		Providers struct {
			Verification struct {
				Flags map[string]bool `json:"flags"`
			} `json:"verification"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(out).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1005 || entries[1004].ROM != "Game 1004.gg" || !entries[1004].Providers.Verification.Flags["nointro_match"] {
		t.Fatal("lost final entry or its exact No-Intro verification")
	}
	// Removing the size bound must not allow concatenated/ignored input documents.
	if _, err := in.WriteAt([]byte("{}"), info.Size()); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := runRomMProject(nil); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatal("accepted trailing JSON", err)
	}
}

func TestProjectionRetainsROMWhenOneProviderIsAmbiguous(t *testing.T) {
	sha1 := strings.Repeat("a", 40)
	valid := catalogapi.Card{Game: catalogapi.Document{YAML: "releases:\n  R: {}\n"}, Fingerprints: catalogapi.Document{YAML: "items: []\n"}}
	conflict := valid
	conflict.Game.YAML = "releases:\n  R: {}\n  Other: {}\nsources:\n  launchbox:\n    matches:\n      - {release: R, database_id: 1}\n      - {release: Other, database_id: 2}\n"
	proof := exporter.PayloadProof{Size: 4, SHA1: sha1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567"}
	selection := catalogapi.Selection{SchemaVersion: 1, Cards: map[string]catalogapi.Card{"valid": valid, "conflict": conflict}}
	for _, game := range []string{"valid", "conflict"} {
		selection.Inputs = append(selection.Inputs, catalogapi.Input{Record: exporter.Record{ROM: game + ".gg", Game: game, Release: "R", Status: "matched", MatchedBy: "hash", Size: 4, SHA1: sha1}, Payload: proof})
	}
	run := func(args []string) ([]map[string]any, error) {
		t.Helper()
		dir := t.TempDir()
		in, err := os.Create(filepath.Join(dir, "in.json"))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if err = json.NewEncoder(in).Encode(map[string]any{"selection": selection, "before": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		_, _ = in.Seek(0, 0)
		out, err := os.Create(filepath.Join(dir, "out.json"))
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		oldIn, oldOut := os.Stdin, os.Stdout
		os.Stdin, os.Stdout = in, out
		defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
		err = runRomMProject(args)
		if err != nil {
			return nil, err
		}
		_, _ = out.Seek(0, 0)
		var output []map[string]any
		if err = json.NewDecoder(out).Decode(&output); err != nil {
			t.Fatal(err)
		}
		return output, nil
	}
	if output, err := run(nil); err != nil || len(output) != 2 || output[1]["providers"] == nil {
		t.Fatal("release-scoped identity was discarded", output, err)
	}
	conflict.Game.YAML = strings.ReplaceAll(conflict.Game.YAML, "release: Other", "release: R")
	selection.Cards["conflict"] = conflict
	output, err := run([]string{"--report-conflicts"})
	if err != nil || len(output) != 2 || output[0]["conflict"] != nil || output[1]["conflict"] != nil || output[1]["providers"] == nil {
		t.Fatal(output, err)
	}
	data, _ := json.Marshal(output[1]["providers"])
	if !strings.Contains(string(data), `"provider":"launchbox"`) || strings.Contains(string(data), `"launchbox":`) {
		t.Fatal("ambiguous slot was not omitted/reported", string(data))
	}
	selection.Inputs[1].Payload.SHA1 = strings.Repeat("f", 40)
	if _, err = run([]string{"--report-conflicts"}); err == nil || !strings.Contains(err.Error(), "proof incomplete") {
		t.Fatal("payload error became a skip", err)
	}
}
