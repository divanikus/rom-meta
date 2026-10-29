package gamelist_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exportclient"
	"rommeta/internal/exporter"
	"rommeta/internal/gamelist"
)

func TestReportedMD5AssociatesRenamedFileWithoutROM(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			o, game := fixture(t, false)
			hash := fmt.Sprintf("%x", md5.Sum([]byte("synthetic payload")))
			put(t, filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: example.z64, size: 17, md5: %s}\n", hash))
			xml, _ := os.ReadFile(o.Input)
			xml = bytes.ReplaceAll(xml, []byte("Different label (USA).zip"), []byte("Renamed.zip"))
			xml = bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte("<md5>"+strings.ToUpper(hash)+"</md5>"))
			os.WriteFile(o.Input, xml, 0644)
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			old, err := gamelist.Import(o)
			if err != nil || old.Matched != 0 {
				t.Fatal(old, err)
			}
			o.MatchMD5 = true
			o.Apply = true
			r, err := gamelist.Import(o)
			if err != nil || r.Matched != 1 || r.HashMatched != 1 || r.Observations[0].MatchMethod != "reported-md5" {
				t.Fatal(r, err)
			}
			body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			if !bytes.Contains(body, []byte("match_method: reported-md5")) {
				t.Fatal(string(body))
			}
			repeat, err := gamelist.Import(o)
			if err != nil || repeat.Updated != 0 || repeat.MediaAdded != 0 {
				t.Fatal(repeat, err)
			}
		})
	}
}
func TestReportedMD5HoldsConflictsVariantsAndComponents(t *testing.T) {
	hash := strings.Repeat("a", 32)
	for _, mode := range []string{"conflict", "variant", "component", "multi-file", "ambiguous", "derivative", "variant-folder"} {
		t.Run(mode, func(t *testing.T) {
			o, game := fixture(t, false)
			o.MatchMD5 = true
			xml, _ := os.ReadFile(o.Input)
			name := "Renamed.zip"
			if mode == "conflict" {
				name = "Different label (USA).zip"
			}
			if mode == "derivative" {
				name = "Renamed [T+Rus].zip"
			}
			if mode == "variant-folder" {
				name = "Variants/Renamed.zip"
			}
			xml = bytes.ReplaceAll(xml, []byte("Different label (USA).zip"), []byte(name))
			xml = bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte("<md5>"+hash+"</md5>"))
			os.WriteFile(o.Input, xml, 0644)
			release := "Example (USA)"
			extra := ""
			kind := ""
			if mode == "conflict" {
				release = "Other (USA)"
				body, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
				put(t, filepath.Join(filepath.Dir(game), "Other", "game.yaml"), strings.ReplaceAll(strings.ReplaceAll(string(body), "Example (USA)", "Other (USA)"), "    aliases: [Different label (USA)]\n", ""))
				game = filepath.Join(filepath.Dir(game), "Other")
			}
			entry := "release: " + release
			if mode == "variant" {
				body, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
				put(t, filepath.Join(game, "game.yaml"), string(body)+"variants:\n  Modified: {kind: hack, based_on: Example (USA)}\n")
				entry = "variant: Modified"
			}
			if mode == "component" {
				kind = ", kind: mame-rom"
			}
			if mode == "multi-file" {
				extra = "  - release: Example (USA)\n    file: {name: other.bin, size: 1, md5: " + strings.Repeat("b", 32) + "}\n"
			}
			put(t, filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - %s\n    file: {name: sample.bin, size: 17, md5: %s%s}\n%s", entry, hash, kind, extra))
			if mode == "ambiguous" {
				body, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
				other := filepath.Join(filepath.Dir(game), "Duplicate")
				put(t, filepath.Join(other, "game.yaml"), string(body))
				fp, _ := os.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				put(t, filepath.Join(other, "fingerprints.yaml"), string(fp))
			}
			r, err := gamelist.Import(o)
			if err != nil || r.Matched != 0 {
				t.Fatal(r, err)
			}
			if mode == "ambiguous" && r.Ambiguous != 1 {
				t.Fatal(r)
			}
			if (mode == "conflict" || mode == "variant" || mode == "derivative" || mode == "variant-folder") && r.HashReview != 1 {
				t.Fatal(r)
			}
		})
	}
}
func TestExplicitMediaTagRole(t *testing.T) {
	o, game := fixture(t, false)
	o.MediaRoles = map[string]string{"thumbnail": "screenshot", "image": "ignore"}
	o.Apply = true
	r, err := gamelist.Import(o)
	if err != nil || r.MediaAdded != 2 {
		t.Fatal(r, err)
	}
	body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	if bytes.Contains(body, []byte("box_front:")) || !bytes.Contains(body, []byte("screenshot-02")) {
		t.Fatal(string(body))
	}
	o.MediaRoles["unknown"] = "box-front"
	if _, err = gamelist.Import(o); err == nil {
		t.Fatal("unknown tag accepted")
	}
}
func orderedImages(t *testing.T) ([]byte, []byte) {
	t.Helper()
	var images [][]byte
	for i := 0; i < 12; i++ {
		im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		im.SetNRGBA(0, 0, color.NRGBA{R: uint8(i), G: 64, B: 128, A: 255})
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		images = append(images, append([]byte(nil), b.Bytes()...))
	}
	sort.Slice(images, func(i, j int) bool {
		return fmt.Sprintf("%x", sha256.Sum256(images[i])) < fmt.Sprintf("%x", sha256.Sum256(images[j]))
	})
	return images[0], images[len(images)-1]
}
func TestReplacementMediaLocalAndAPIExport(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "catalog")
			platform := filepath.Join(root, "platforms", "Sega - Game Gear")
			game := filepath.Join(platform, "Example")
			roms := filepath.Join(base, "roms")
			put(t, filepath.Join(platform, "_platform.yaml"), "name: Sega - Game Gear\n")
			put(t, filepath.Join(platform, "_catalog.yaml"), "name: Synthetic\n")
			put(t, filepath.Join(game, "game.yaml"), "title: Example\ndefault_release: Example (USA)\nreleases:\n  Example (USA): {title: Example, regions: [USA]}\n")
			payload := []byte("synthetic Game Gear payload")
			put(t, filepath.Join(roms, "Example (USA).gg"), string(payload))
			put(t, filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: example.gg, size: %d, sha1: %x}\n", len(payload), sha1.Sum(payload)))
			if packed {
				catalogfstest.Pack(t, platform)
			}
			input := filepath.Join(base, "input", "gamelist.xml")
			cover := filepath.Join(base, "input", "media", "cover.png")
			shot := filepath.Join(base, "input", "media", "screen.png")
			video := filepath.Join(base, "input", "media", "clip.mp4")
			put(t, input, `<gameList><provider><system>gamegear</system></provider><game><path>./Example (USA).gg</path><desc>Synthetic prose</desc><thumbnail>./media/cover.png</thumbnail><image>./media/screen.png</image><video>./media/clip.mp4</video></game></gameList>`)
			old, new := orderedImages(t)
			put(t, cover, string(old))
			put(t, shot, string(old))
			put(t, video, string(old))
			o := gamelist.Options{Input: input, DataRoot: root, Collection: "sample", Language: "en", Apply: true}
			if _, err := gamelist.Import(o); err != nil {
				t.Fatal(err)
			}
			oldAssets, err := exporter.InventoryMedia(root, []exporter.Record{{Game: "platforms/Sega - Game Gear/Example"}})
			if err != nil || len(oldAssets) != 3 {
				t.Fatal(oldAssets, err)
			}
			put(t, cover, string(new))
			put(t, shot, string(new))
			put(t, video, string(new))
			changed, err := gamelist.Import(o)
			if err != nil || changed.MediaAdded != 3 {
				t.Fatal(changed, err)
			}
			repeat, err := gamelist.Import(o)
			if err != nil || repeat.Updated != 0 || repeat.MediaAdded != 0 {
				t.Fatal(repeat, err)
			}
			profile := filepath.Join(base, "profile.yaml")
			put(t, profile, "schema_version: 1\ntarget: es-de\nselection:\n  description: {languages: [en], sources: [gamelist.sample]}\n  media:\n    cover: {sources: [gamelist.sample]}\n    screenshot: {sources: [gamelist.sample]}\n    video: {sources: [gamelist.sample]}\n")
			local, err := exporter.Build(exporter.Options{DataRoot: root, ROMRoot: roms, Platform: "gamegear", ProfilePath: profile, Out: filepath.Join(base, "local")})
			if err != nil {
				t.Fatal(err)
			}
			if err = exporter.Apply(local, nil); err != nil {
				t.Fatal(err)
			}
			api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(api)
			defer server.Close()
			job, err := exportclient.Build(context.Background(), exportclient.Options{API: server.URL, BatchOptions: exporter.BatchOptions{Options: exporter.Options{ROMRoot: roms, Platform: "gamegear", ProfilePath: profile, Out: filepath.Join(base, "remote")}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = job.Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, role := range []string{"cover", "screenshot", "video"} {
				m := local.Records[0].Media[role]
				if m.Source != "gamelist.sample" {
					t.Fatal(m)
				}
				a, err := os.ReadFile(filepath.Join(local.Out, filepath.FromSlash(m.OutputPath)))
				if err != nil || !bytes.Equal(a, new) {
					t.Fatalf("stale local %s: %v", role, err)
				}
				b, err := os.ReadFile(filepath.Join(job.Batch.Out, filepath.FromSlash(m.OutputPath)))
				if err != nil || !bytes.Equal(a, b) {
					t.Fatalf("stale API %s: %v", role, err)
				}
			}
			active, err := exporter.InventoryMedia(root, local.Records)
			if err != nil || len(active) != 3 {
				t.Fatal(active, err)
			}
			for _, oldAsset := range oldAssets {
				if _, err = catalogfs.Stat(filepath.Join(root, filepath.FromSlash(oldAsset.Path))); err != nil {
					t.Fatal("recovery asset deleted", err)
				}
			}
			// An explicit owner pin can still choose retained recovery content.
			oldCover := ""
			for _, a := range oldAssets {
				if a.Kind == "box-front" {
					oldCover = a.Path
				}
			}
			body, _ := os.ReadFile(profile)
			gameRel := "platforms/Sega - Game Gear/Example"
			relative := strings.TrimPrefix(oldCover, gameRel+"/")
			put(t, profile, string(body)+fmt.Sprintf("media_overrides:\n  - game: %q\n    release: Example (USA)\n    role: cover\n    path: %q\n", gameRel, relative))
			pinned, err := exporter.Build(exporter.Options{DataRoot: root, ROMRoot: roms, Platform: "gamegear", ProfilePath: profile, Out: filepath.Join(base, "pinned")})
			if err != nil || pinned.Records[0].Media["cover"].SourcePath != oldCover {
				t.Fatal(pinned, err)
			}
			available, err := exporter.InventoryMedia(root, pinned.Records)
			if err != nil || len(available) != 4 {
				t.Fatal(available, err)
			}
			// Another language's distinct observation may keep old assets active.
			o.Origin = "another-language/gamelist.xml"
			o.Language = "fr"
			put(t, cover, string(old))
			put(t, shot, string(old))
			put(t, video, string(old))
			if _, err = gamelist.Import(o); err != nil {
				t.Fatal(err)
			}
			available, err = exporter.InventoryMedia(root, local.Records)
			if err != nil || len(available) != 6 {
				t.Fatal(available, err)
			}
			docBytes, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
			doc, _ := catalogyaml.Parse(docBytes)
			refs, err := catalogyaml.GamelistMediaReferences(doc.Root())
			if err != nil || len(refs["gamelist.sample"]) != 6 {
				t.Fatal(refs, err)
			}
		})
	}
}

func TestChangedRolesRetireOnlyOwnedReferences(t *testing.T) {
	o, game := fixture(t, false)
	o.Apply = true
	if _, err := gamelist.Import(o); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(filepath.Dir(o.Input), "media", "boxart", "front.jpg"))
	missing, err := gamelist.Import(o)
	if err != nil || missing.MissingMedia != 1 || missing.Updated != 0 {
		t.Fatal(missing, err)
	}
	// Ignoring a tag intentionally retires its old imported reference even when
	// its source file is absent; missing optional inputs otherwise retain it.
	o.MediaRoles = map[string]string{"thumbnail": "ignore"}
	changed, err := gamelist.Import(o)
	if err != nil || changed.Updated != 1 {
		t.Fatal(changed, err)
	}
	assets, err := exporter.InventoryMedia(o.DataRoot, []exporter.Record{{Game: "platforms/Nintendo - Nintendo 64/Example"}})
	if err != nil || len(assets) != 2 {
		t.Fatal(assets, err)
	}
	body, _ := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	if bytes.Contains(body, []byte("box_front:")) {
		t.Fatal(string(body))
	}
	// Other observations can retain that old cover, and owner candidates survive.
	doc, _ := catalogyaml.Parse(body)
	source := catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), "gamelist.sample")
	matches := catalogyaml.Value(source, "matches")
	owner, _ := catalogyaml.Parse([]byte("media: {box_front: [{url: media/owner.gamelist.sample.png}]}\n"))
	media := catalogyaml.Value(owner.Root(), "media")
	catalogyaml.Set(matches.Content[0], "media", media)
	updated, _ := doc.Marshal()
	catalogfs.WriteFile(filepath.Join(game, "game.yaml"), updated, 0644)
	if _, err = gamelist.Import(o); err != nil {
		t.Fatal(err)
	}
	body, _ = catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
	if !bytes.Contains(body, []byte("owner.gamelist.sample.png")) {
		t.Fatal("owner candidate lost")
	}
}
