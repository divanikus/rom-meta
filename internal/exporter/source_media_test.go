package exporter

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
)

func sourceMediaFixture(t testing.TB, packed bool) (*LookupCatalog, string, []ExportInput, Profile) {
	t.Helper()
	root, roms, staging := t.TempDir(), t.TempDir(), t.TempDir()
	platform := filepath.Join(root, "platforms", "n64")
	gamePath := filepath.Join(platform, "Game")
	write := func(name string, body []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(platform, "_platform.yaml"), []byte("name: Nintendo - Nintendo 64\n"))
	var card, fingerprints strings.Builder
	card.WriteString("title: Game\ndefault_release: Game (Rev 0)\nreleases:\n")
	fingerprints.WriteString("items:\n")
	dirs := []string{"", "Parent", "Child"}
	for i := 0; i < 8; i++ {
		release := fmt.Sprintf("Game (Rev %d)", i)
		fmt.Fprintf(&card, "  %q: {}\n", release)
		dirs = append(dirs, release)
		payload := []byte(fmt.Sprintf("synthetic media selection release %d", i))
		fmt.Fprintf(&fingerprints, "  - release: %q\n    file: {size: %d, sha1: '%x'}\n", release, len(payload), sha1.Sum(payload))
		write(filepath.Join(roms, fmt.Sprintf("release-%d.z64", i)), payload)
	}
	card.WriteString("variants:\n  Parent: {kind: translation, based_on: Game (Rev 0), languages: [en]}\n  Child: {kind: bugfix, based_on: Parent}\n")
	payload := []byte("synthetic media selection variant")
	fmt.Fprintf(&fingerprints, "  - variant: Child\n    file: {size: %d, sha1: '%x'}\n", len(payload), sha1.Sum(payload))
	for i := 0; i < 8; i++ {
		write(filepath.Join(roms, fmt.Sprintf("variant-%d.z64", i)), payload)
	}
	files := map[string]string{}
	add := func(name string, body []byte) {
		dest := filepath.Join(gamePath, filepath.FromSlash(name))
		if packed {
			dest = filepath.Join(staging, filepath.FromSlash(name))
		}
		write(dest, body)
		files[filepath.ToSlash(name)] = dest
	}
	add("game.yaml", []byte(card.String()))
	add("fingerprints.yaml", []byte(fingerprints.String()))
	for _, dir := range dirs {
		for i := 0; i < 8; i++ {
			add(filepath.ToSlash(filepath.Join("media", dir, fmt.Sprintf("box-front-%02d.local.png", i+1))), []byte("synthetic artwork"))
		}
	}
	if packed {
		if err := catalogfs.PutFiles(gamePath, files, nil); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(platform, catalogfs.Marker), []byte(catalogfs.MarkerContent))
	}
	catalog, err := NewLookupCatalog(root, "")
	if err != nil {
		t.Fatal(err)
	}
	catalog.CacheDir = "-"
	inputs, err := ObserveExport(Options{ROMRoot: roms, System: "n64"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := ExportProfile("", "schema_version: 1\nselection:\n  media:\n    cover: {sources: [local], formats: [png], regions: ['*'], scope: release}\n")
	if err != nil {
		t.Fatal(err)
	}
	return catalog, gamePath, inputs, profile
}

func BenchmarkSourceMediaSelection(b *testing.B) {
	for _, packed := range []bool{false, true} {
		b.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(b *testing.B) {
			catalog, _, inputs, profile := sourceMediaFixture(b, packed)
			resolver, err := catalog.OpenExport("n64", "n64", "")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				plan, err := resolver.Resolve(inputs, profile)
				if err != nil || plan.Matched != 16 {
					b.Fatal(plan, err)
				}
			}
		})
	}
}

func TestSourceMediaReleaseAndVariantOwnership(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			catalog, _, inputs, profile := sourceMediaFixture(t, packed)
			resolver, err := catalog.OpenExport("n64", "n64", "")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := resolver.Resolve(inputs, profile)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Records) != len(inputs) {
				t.Fatal("missing records", len(plan.Records))
			}
			for i, r := range plan.Records {
				owner := fmt.Sprintf("Game (Rev %d)", i)
				if strings.HasPrefix(r.ROM, "variant-") {
					owner = "Child"
				}
				want := "platforms/n64/Game/media/" + owner + "/box-front-01.local.png"
				if r.Media["cover"].SourcePath != want {
					t.Fatalf("%s: %+v", r.ROM, r.Media)
				}
				if owner == "Child" && r.Media["cover"].FromVariant != "Child" {
					t.Fatal("variant ownership lost", r.Media)
				}
			}
		})
	}
}

func TestSourceMediaSnapshotRechecksFilesAndExpiresAfterResolve(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			catalog, gamePath, all, profile := sourceMediaFixture(t, packed)
			resolver, err := catalog.OpenExport("n64", "n64", "")
			if err != nil {
				t.Fatal(err)
			}
			inputs := []ExportInput{all[0], all[0]}
			inputs[1].ROM = "second.z64"
			folder := filepath.Join(gamePath, "media", "Game (Rev 0)")
			preferred := filepath.Join(folder, "box-front-00.local.png")
			resolver.Progress = func(stage, name string, current, total int) {
				if stage == "metadata/media" && current == 1 {
					if err := catalogfs.WriteFile(preferred, []byte("new artwork"), 0644); err != nil {
						t.Fatal(err)
					}
					if err := catalogfs.Remove(filepath.Join(folder, "box-front-01.local.png")); err != nil {
						t.Fatal(err)
					}
				}
			}
			plan, err := resolver.Resolve(inputs, profile)
			if err != nil {
				t.Fatal(err)
			}
			for i, suffix := range []string{"box-front-01.local.png", "box-front-02.local.png"} {
				if !strings.HasSuffix(plan.Records[i].Media["cover"].SourcePath, suffix) {
					t.Fatal("snapshot or fresh selected-file check lost", plan.Records[i].Media)
				}
			}
			resolver.Progress = nil
			next, err := resolver.Resolve(inputs[:1], profile)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(next.Records[0].Media["cover"].SourcePath, "box-front-00.local.png") {
				t.Fatal("new file invisible on next request", next.Records[0].Media)
			}
			invalid := inputs[1]
			invalid.ROM = "../invalid.z64"
			if _, err := resolver.Resolve([]ExportInput{inputs[0], invalid}, profile); err == nil {
				t.Fatal("invalid observation accepted")
			}
			if len(resolver.index.media.files) != 0 {
				t.Fatal("failed request retained its media inventory")
			}
		})
	}
}

func TestSourceMediaMissingDirectoryAndCancellation(t *testing.T) {
	g := &game{path: t.TempDir()}
	files, err := g.sourceMedia()
	if err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
	put(t, filepath.Join(g.path, "media", "cover.png"), []byte("art"))
	files, err = g.sourceMedia()
	if err != nil || len(files) != 0 {
		t.Fatal("missing directory was enumerated again", files, err)
	}
	g.media.clear()
	files, err = g.sourceMedia()
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.context = ctx
	if _, err := g.sourceMedia(); !errors.Is(err, context.Canceled) {
		t.Fatal("cached inventory ignored cancellation", err)
	}
	other := &game{path: t.TempDir(), media: g.media}
	put(t, filepath.Join(other.path, "media", "other.png"), []byte("art"))
	files, err = other.sourceMedia()
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].path, "other.png") {
		t.Fatal("games shared the wrong inventory", files, err)
	}
}

func TestSourceMediaExpiresAfterStrictLookup(t *testing.T) {
	catalog, gamePath, exports, profile := sourceMediaFixture(t, false)
	catalog.Profile = profile
	resolver, err := catalog.Open("n64")
	if err != nil {
		t.Fatal(err)
	}
	p := exports[0].Proof
	input := ObservedInput{ROM: exports[0].ROM, Outer: FileDigest{Size: p.Size, SHA256: p.SHA256}, Payload: p}
	for _, suffix := range []string{"box-front-01.local.png", "box-front-00.local.png"} {
		if suffix == "box-front-00.local.png" {
			put(t, filepath.Join(gamePath, "media", "Game (Rev 0)", suffix), []byte("new artwork"))
		}
		plan, err := resolver.Resolve([]ObservedInput{input})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(plan.Records[0].Media["cover"].SourcePath, suffix) {
			t.Fatal(plan.Records[0].Media)
		}
	}
}

func TestSourceMediaExpiresAfterArcadeExport(t *testing.T) {
	options := Options{DataRoot: t.TempDir(), ROMRoot: t.TempDir(), System: "arcade"}
	gamePath := arcadeFixture(t, options)
	putZip(t, filepath.Join(options.ROMRoot, "game1.zip"), map[string][]byte{"one.bin": []byte("synthetic component")})
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.OpenExport("Arcade", "arcade", "test-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := ObserveExport(options)
	if err != nil {
		t.Fatal(err)
	}
	for i, suffix := range []string{"media/box-front.png", "media/game1/box-front.png"} {
		if i > 0 {
			put(t, filepath.Join(gamePath, filepath.FromSlash(suffix)), []byte("new artwork"))
		}
		plan, err := resolver.Resolve(inputs, catalog.Profile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(plan.Records[0].Media["cover"].SourcePath, suffix) {
			t.Fatal(plan.Records[0].Media)
		}
	}
}
