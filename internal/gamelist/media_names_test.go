package gamelist_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
	"rommeta/internal/gamelist"
)

func TestReadableSingleAndMixedFormatVariants(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			o, game := fixture(t, packed)
			o.Apply = true
			if _, err := gamelist.Import(o); err != nil {
				t.Fatal(err)
			}
			first, err := exporter.InventoryMedia(o.DataRoot, []exporter.Record{{Game: "platforms/Nintendo - Nintendo 64/Example"}})
			if err != nil || len(first) != 3 {
				t.Fatal(first, err)
			}
			for _, a := range first {
				n := path.Base(a.Path)
				if strings.Contains(n, "unspecified") || strings.Contains(n, ".und.") || strings.Contains(n, "-01.") {
					t.Fatal(n)
				}
			}
			// A second format is an additional variant, not another "-01" in its own extension.
			o.MediaRoles = map[string]string{"image": "box-front"}
			if _, err := gamelist.Import(o); err != nil {
				t.Fatal(err)
			}
			media, err := exporter.InventoryMedia(o.DataRoot, []exporter.Record{{Game: "platforms/Nintendo - Nintendo 64/Example"}})
			if err != nil {
				t.Fatal(err)
			}
			covers := []string{}
			for _, a := range media {
				if a.Kind == "box-front" {
					covers = append(covers, path.Base(a.Path))
				}
			}
			if len(covers) != 2 || covers[0] == covers[1] {
				t.Fatal(covers)
			}
			for _, name := range covers {
				if strings.Contains(name, "unspecified") || strings.Contains(name, ".und.") {
					t.Fatal(name)
				}
			}
			r, err := gamelist.Import(o)
			if err != nil || r.Updated != 0 || r.MediaAdded != 0 {
				t.Fatal(r, err)
			}
			_ = game
		})
	}
}

func TestMigrateLegacyMediaPreservesBytesReferencesAndRepeat(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			o, game := fixture(t, packed)
			o.Apply = true
			if _, err := gamelist.Import(o); err != nil {
				t.Fatal(err)
			}
			originalFP, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
			body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			inventory, err := exporter.InventoryMedia(o.DataRoot, []exporter.Record{{Game: "platforms/Nintendo - Nintendo 64/Example"}})
			if err != nil {
				t.Fatal(err)
			}
			oldCover := ""
			for _, a := range inventory {
				full := filepath.Join(o.DataRoot, filepath.FromSlash(a.Path))
				content, err := catalogfs.ReadFile(full)
				if err != nil {
					t.Fatal(err)
				}
				role := a.Kind
				if role == "screenshot" {
					role = "screenshot-01"
				}
				// The legacy scheme uses twelve hex characters.
				h := fmt.Sprintf("%x", sha256.Sum256(content))
				old := role + ".unspecified.und." + h[:12] + ".gamelist.sample" + path.Ext(a.Path)
				legacy := filepath.Join(filepath.Dir(full), old)
				if err := catalogfs.Rename(full, legacy); err != nil {
					t.Fatal(err)
				}
				rel, _ := filepath.Rel(game, full)
				newRel, _ := filepath.Rel(game, legacy)
				body = bytes.ReplaceAll(body, []byte(filepath.ToSlash(rel)), []byte(filepath.ToSlash(newRel)))
				if a.Kind == "box-front" {
					oldCover = filepath.ToSlash(newRel)
				}
			}
			// Retained content is a real second variant; both owner reference forms follow its rename.
			data := []byte("another synthetic cover")
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			extra := path.Join(path.Dir(oldCover), "box-front.unspecified.und."+hash[:12]+".gamelist.sample.jpg")
			if err := catalogfs.WriteFile(filepath.Join(game, filepath.FromSlash(extra)), data, 0644); err != nil {
				t.Fatal(err)
			}
			body = append(body, []byte(fmt.Sprintf("owner_pin: %q\nowner_catalog_pin: %q\n", extra, "platforms/Nintendo - Nintendo 64/Example/"+extra))...)
			if err := catalogfs.WriteFile(filepath.Join(game, "game.yaml"), body, 0644); err != nil {
				t.Fatal(err)
			}
			opts := gamelist.MediaNamesOptions{DataRoot: o.DataRoot, Collection: o.Collection}
			preview, err := gamelist.NameMedia(opts)
			if err != nil || preview.Cards != 1 || preview.Renamed != 4 {
				t.Fatal(preview, err)
			}
			unchanged, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if !bytes.Equal(body, unchanged) {
				t.Fatal("preview changed YAML")
			}
			opts.Apply = true
			applied, err := gamelist.NameMedia(opts)
			if err != nil || applied.Renamed != preview.Renamed {
				t.Fatal(applied, err)
			}
			after, err := gamelist.NameMedia(opts)
			if err != nil || after.Renamed != 0 || after.Cards != 0 {
				t.Fatal(after, err)
			}
			gameRel := "platforms/Nintendo - Nintendo 64/Example"
			for old, hash := range preview.Inventory[gameRel] {
				dest := preview.Changes[gameRel][old]
				if dest == "" {
					dest = old
				}
				if after.Inventory[gameRel][dest] != hash {
					t.Fatalf("changed content: %s -> %s", old, dest)
				}
				if strings.Contains(dest, ".unspecified.") || strings.Contains(dest, ".und.") {
					t.Fatal(dest)
				}
			}
			for old, dest := range preview.Changes[gameRel] {
				if old == dest {
					t.Fatal("identity rename")
				}
				if _, err := catalogfs.Stat(filepath.Join(game, filepath.FromSlash(old))); !os.IsNotExist(err) {
					t.Fatal(old, err)
				}
			}
			body, _ = catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if bytes.Contains(body, []byte("unspecified.und")) || !bytes.Contains(body, []byte("owner_pin:")) || !bytes.Contains(body, []byte("source_file: media/boxart/front.jpg")) {
				t.Fatal(string(body))
			}
			fp, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
			if !bytes.Equal(originalFP, fp) {
				t.Fatal("fingerprints changed")
			}
			r, err := gamelist.Import(o)
			if err != nil || r.Updated != 0 || r.MediaAdded != 0 || r.MediaRenamed != 0 {
				t.Fatal(r, err)
			}
		})
	}
}

func TestFreshMixedFormatsGetDistinctOrdinals(t *testing.T) {
	o, game := fixture(t, false)
	o.Apply = true
	o.MediaRoles = map[string]string{"image": "box-front"}
	if _, err := gamelist.Import(o); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"box-front-01.gamelist.sample.png", "box-front-02.gamelist.sample.jpg"} {
		if _, err := catalogfs.Stat(filepath.Join(game, "media", "Example (USA)", name)); err != nil {
			t.Fatal(name, err)
		}
	}
}
func TestMigrationRejectsDirectoryCollisionWithoutWrites(t *testing.T) {
	o, game := fixture(t, false)
	o.Apply = true
	if _, err := gamelist.Import(o); err != nil {
		t.Fatal(err)
	}
	clean := filepath.Join(game, "media", "Example (USA)", "gameplay-video.gamelist.sample.mp4")
	data, _ := catalogfs.ReadFile(clean)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	legacy := filepath.Join(filepath.Dir(clean), "gameplay-video.unspecified.und."+hash[:12]+".gamelist.sample.mp4")
	if err := catalogfs.Rename(clean, legacy); err != nil {
		t.Fatal(err)
	}
	body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	body = bytes.ReplaceAll(body, []byte(path.Base(clean)), []byte(path.Base(legacy)))
	if err := catalogfs.WriteFile(filepath.Join(game, "game.yaml"), body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(clean, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := gamelist.NameMedia(gamelist.MediaNamesOptions{DataRoot: o.DataRoot, Collection: "sample", Apply: true}); err == nil {
		t.Fatal("directory collision accepted")
	}
	after, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	if !bytes.Equal(body, after) {
		t.Fatal("collision changed metadata")
	}
	remaining, _ := catalogfs.ReadFile(legacy)
	if !bytes.Equal(remaining, data) {
		t.Fatal("collision lost source")
	}
}
