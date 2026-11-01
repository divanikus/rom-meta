package exporter

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
)

func TestLocalExportReusesAndMaintainsPreparedIndex(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			options, game, payload := fixture(t)
			put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
			platform := filepath.Dir(game)
			if packed {
				catalogfstest.Pack(t, platform)
			}
			before := catalogPayloadHashes(t, options.DataRoot)
			rebuilt := 0
			options.Progress = func(stage, name string, current, total int) {
				if stage == "reindexing" {
					rebuilt++
				}
			}
			cold, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if rebuilt != 1 {
				t.Fatal("missing index not visibly rebuilt", rebuilt)
			}
			name := filepath.Join(platform, "_index.sqlite")
			if _, err := os.Stat(name); err != nil {
				t.Fatal("index was not saved", err)
			}
			rebuilt = 0
			warm, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if rebuilt != 0 || !reflect.DeepEqual(cold.Records, warm.Records) {
				t.Fatal("warm export reparsed or changed results", rebuilt)
			}
			if !reflect.DeepEqual(before, catalogPayloadHashes(t, options.DataRoot)) {
				t.Fatal("catalog payloads changed")
			}
			// A saved old summary format is upgraded once, then reused normally.
			db, err := sql.Open("sqlite", name)
			if err != nil {
				t.Fatal(err)
			}
			var body []byte
			if err := db.QueryRow("SELECT payload FROM lookup_bundles_v1 WHERE folder='Example'").Scan(&body); err != nil {
				db.Close()
				t.Fatal(err)
			}
			var entry lookupEntry
			if err := json.Unmarshal(body, &entry); err != nil {
				db.Close()
				t.Fatal(err)
			}
			entry.SchemaVersion = 2
			body, err = json.Marshal(entry)
			if err != nil {
				db.Close()
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			if _, err := db.Exec("UPDATE lookup_bundles_v1 SET payload=?,checksum=? WHERE folder='Example'", body, fmt.Sprintf("%x", sum)); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			rebuilt = 0
			if _, err := Build(options); err != nil || rebuilt != 1 {
				t.Fatal("old index format not upgraded", err, rebuilt)
			}
			rebuilt = 0
			if _, err := Build(options); err != nil || rebuilt != 0 {
				t.Fatal("upgraded index not reused", err, rebuilt)
			}
			// Only a changed card is reparsed; selected metadata still comes from YAML.
			data, err := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := catalogfs.WriteFile(filepath.Join(game, "game.yaml"), append(data, []byte("\ndeveloper: Updated\n")...), 0600); err != nil {
				t.Fatal(err)
			}
			rebuilt = 0
			changed, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if rebuilt != 1 || changed.Records[0].Fields["developer"].Value != "Updated" {
				t.Fatal("changed card not refreshed", rebuilt)
			}
			rebuilt = 0
			if _, err := Build(options); err != nil || rebuilt != 0 {
				t.Fatal("changed summary was not saved", err, rebuilt)
			}
		})
	}
}

func TestLocalExportDoesNotParseUnselectedCachedCards(t *testing.T) {
	options, game, payload := fixture(t)
	platform := filepath.Dir(game)
	indexWriteGame(t, platform, "Unused", "Unused title", 50)
	put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
	if _, err := Build(options); err != nil {
		t.Fatal(err)
	}
	// A same-stamp malformed unselected document demonstrates that warm export
	// does not parse every YAML again. Selected cards still need their digest.
	name := filepath.Join(platform, "Unused", "game.yaml")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = '['
	put(t, name, data)
	if err := os.Chtimes(name, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	plan, err := Build(options)
	if err != nil || plan.Matched != 1 {
		t.Fatal("unselected card reparsed", err)
	}
	name = filepath.Join(game, "game.yaml")
	data, err = os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = '['
	put(t, name, data)
	if err := os.Chtimes(name, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(options); err == nil {
		t.Fatal("selected changed document accepted")
	}
}
