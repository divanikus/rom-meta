package exporter

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func chdExportFixture(t testing.TB) (Options, string, []byte) {
	t.Helper()
	base := t.TempDir()
	root, roms := filepath.Join(base, "catalog"), filepath.Join(base, "roms")
	game := filepath.Join(root, "platforms", "Sega - Saturn", "Game")
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(filepath.Dir(game), "_platform.yaml"), []byte("name: Sega - Saturn\n"))
	write(filepath.Join(game, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA): {description: Selected USA description}\n  Game (Europe): {}\n"))
	write(filepath.Join(game, "fingerprints.yaml"), []byte("items: []\n"))
	data := syntheticCHD()
	write(filepath.Join(roms, "Game (USA).chd"), data)
	return Options{DataRoot: root, ROMRoot: roms, Out: filepath.Join(base, "export"), Platform: "saturn", System: "saturn"}, game, data
}

func TestCHDHeaderExportAndFullHashProduceSameFrontend(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			options, _, _ := chdExportFixture(t)
			options.Target = target
			// A sizeable sparse payload makes accidental whole-file work visible in benchmarks.
			f, err := os.OpenFile(filepath.Join(options.ROMRoot, "Game (USA).chd"), os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Truncate(8 << 20); err != nil {
				t.Fatal(err)
			}
			f.Close()
			fast, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			r := fast.Records[0]
			if fast.Matched != 1 || r.Release != "Game (USA)" || r.Payload != nil || r.SHA1 != "" || r.Disc == nil || len(r.HashRequired) != 0 {
				t.Fatalf("not bounded: %+v", r)
			}
			options.HashROMs = true
			full, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			if full.Records[0].Payload == nil || full.Records[0].SHA1 == "" {
				t.Fatal("explicit hashing skipped payload")
			}
			render := RenderGamelist
			if target == "launchbox" {
				render = RenderLaunchBox
			}
			a, err := render(fast)
			if err != nil {
				t.Fatal(err)
			}
			b, err := render(full)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("frontend changes with proof mode:\n%s\n%s", a, b)
			}
			if err = Apply(fast, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCHDHeaderFreshnessBeforeApply(t *testing.T) {
	for _, change := range []string{"header", "size", "mtime", "full-body"} {
		t.Run(change, func(t *testing.T) {
			options, _, data := chdExportFixture(t)
			options.HashROMs = change == "full-body"
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(options.ROMRoot, "Game (USA).chd")
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "header":
				data[84] = 9
			case "size":
				data = append(data, 1)
			case "mtime":
			case "full-body":
				data[len(data)-1] = 5
			}
			put(t, path, data)
			if change == "header" || change == "full-body" {
				if err = os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			if change == "mtime" {
				if err = os.Chtimes(path, stat.ModTime().Add(2000000000), stat.ModTime().Add(2000000000)); err != nil {
					t.Fatal(err)
				}
			}
			if err = Apply(plan, nil); err == nil {
				t.Fatal("changed CHD accepted")
			}
			if _, err = os.Stat(options.Out); !os.IsNotExist(err) {
				t.Fatal("failed export published")
			}
		})
	}
}

func TestCHDAutomaticContainerHashFallbackAndRename(t *testing.T) {
	options, game, data := chdExportFixture(t)
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: Game (USA)\n    file: {name: 'Game (USA).chd', size: %d, sha1: '%x'}\n", len(data), sha1.Sum(data))))
	if err := os.Rename(filepath.Join(options.ROMRoot, "Game (USA).chd"), filepath.Join(options.ROMRoot, "Renamed.chd")); err != nil {
		t.Fatal(err)
	}
	hashed := 0
	options.Progress = func(stage, name string, current, total int) {
		if stage == "hash ROMs" {
			hashed++
		}
	}
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Records[0]
	if r.MatchedBy != "hash" || r.Release != "Game (USA)" || r.Payload == nil || len(r.HashRequired) != 0 || hashed != 1 {
		t.Fatalf("missing fallback: %+v, reads %d", r, hashed)
	}
}

func TestCHDPlaylistOnlyHashesMemberWithContainerFingerprint(t *testing.T) {
	options, game, data := chdExportFixture(t)
	if err := os.Remove(filepath.Join(options.ROMRoot, "Game (USA).chd")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(game, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA) (Disc 1): {}\n  Game (USA) (Disc 2): {}\n"))
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: Game (USA) (Disc 1)\n    file: {name: 'Game (USA) (Disc 1).chd', size: %d, sha1: '%x'}\n", len(data), sha1.Sum(data))))
	dir := filepath.Join(options.ROMRoot, "Game.m3u")
	put(t, filepath.Join(dir, "Game.m3u"), []byte("Game (USA) (Disc 1).chd\nGame (USA) (Disc 2).chd\n"))
	put(t, filepath.Join(dir, "Game (USA) (Disc 1).chd"), data)
	put(t, filepath.Join(dir, "Game (USA) (Disc 2).chd"), append(append([]byte(nil), data...), make([]byte, 512)...))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Records[0]
	if r.Status != "matched" || len(r.Playlist) != 2 || r.Playlist[0].SHA1 == "" || r.Playlist[1].SHA1 != "" || r.Playlist[1].Disc == nil {
		t.Fatalf("wrong playlist proof: %+v", r)
	}
	if err = Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	options.Out += "-changed"
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Game (USA) (Disc 2).chd")
	stat, _ := os.Stat(path)
	changed := append(append([]byte(nil), data...), make([]byte, 512)...)
	changed[84] = 22
	put(t, path, changed)
	os.Chtimes(path, stat.ModTime(), stat.ModTime())
	if err = Apply(plan, nil); err == nil {
		t.Fatal("changed bounded playlist CHD accepted")
	}
}

func TestCHDHashRequestsAreBoundedToObservedInputs(t *testing.T) {
	options, _, _ := chdExportFixture(t)
	inputs, err := ObserveExport(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, requested := range [][]string{{"../outside.chd"}, {"unknown.chd"}, {inputs[0].ROM, inputs[0].ROM}} {
		records := []Record{{ROM: inputs[0].ROM, Status: "unmatched", HashRequired: requested}}
		if _, _, err = HashRequiredInputs(options, inputs, records); err == nil {
			t.Fatal("unsafe hash request accepted", requested)
		}
	}
}

func TestCHDAmbiguousNamesStayUnresolvedWithoutUsefulFingerprint(t *testing.T) {
	options, game, _ := chdExportFixture(t)
	other := filepath.Join(filepath.Dir(game), "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other\nreleases:\n  Game (USA): {}\n"))
	put(t, filepath.Join(other, "fingerprints.yaml"), []byte("items: []\n"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if r := plan.Records[0]; r.Status != "ambiguous" || r.Payload != nil || len(r.HashRequired) != 0 {
		t.Fatalf("guessed ambiguous name: %+v", r)
	}
}

func BenchmarkCHDExportProof(b *testing.B) {
	options, _, _ := chdExportFixture(b)
	f, err := os.OpenFile(filepath.Join(options.ROMRoot, "Game (USA).chd"), os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	if err = f.Truncate(32 << 20); err != nil {
		b.Fatal(err)
	}
	f.Close()
	for _, full := range []bool{false, true} {
		b.Run(fmt.Sprintf("full-hash=%t", full), func(b *testing.B) {
			options.HashROMs = full
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Build(options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
