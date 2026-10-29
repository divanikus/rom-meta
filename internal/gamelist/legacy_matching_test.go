package gamelist_test

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/gamelist"
)

func rewriteLaunch(t *testing.T, o gamelist.Options, launch string) {
	t.Helper()
	xml, err := os.ReadFile(o.Input)
	if err != nil {
		t.Fatal(err)
	}
	xml = bytes.ReplaceAll(xml, []byte("Different label (USA).zip"), []byte(launch))
	if err = os.WriteFile(o.Input, xml, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyNamesRetainReleaseConstraints(t *testing.T) {
	tests := []struct {
		name, launch       string
		releases           []string
		matched, ambiguous int
	}{
		{"country", "Example (U) [!].zip", []string{"Example (USA)"}, 1, 0},
		{"country-set", "Example (JU) [!].zip", []string{"Example (USA, Japan)"}, 1, 0},
		{"omitted-language", "Example (E) [!].zip", []string{"Example (Europe) (En,Fr)"}, 1, 0},
		{"explicit-language", "Example (E) (Fr) [!].zip", []string{"Example (Europe) (En)", "Example (Europe) (Fr)"}, 1, 0},
		{"language-conflict", "Example (E) (De) [!].zip", []string{"Example (Europe) (En,Fr)"}, 0, 0},
		{"language-ambiguity", "Example (E) [!].zip", []string{"Example (Europe) (En)", "Example (Europe) (Fr)"}, 0, 1},
		{"compatibility-annotation", "Example (U) [!].zip", []string{"Example (USA) (En,Fr) (GB Compatible)"}, 1, 0},
		{"edition", "Example (U) [!].zip", []string{"Example (USA) (Rumble Version)"}, 0, 0},
		{"revision", "Example (U) (Rev 1) [!].zip", []string{"Example (USA)", "Example (USA) (Rev 1)"}, 1, 0},
		{"revision-not-guessed", "Example (U) (V1.1) [!].zip", []string{"Example (USA)", "Example (USA) (Rev 1)"}, 0, 0},
		{"region-conflict", "Example (E) [!].zip", []string{"Example (USA)"}, 0, 0},
		{"demo", "Example (U) (Demo) [!].zip", []string{"Example (USA)"}, 0, 0},
		{"hack-folder", "Hack/Example (U) [!].zip", []string{"Example (USA)"}, 0, 0},
		{"translation", "Example (U) [T+En].zip", []string{"Example (USA)"}, 0, 0},
		{"bad-dump", "Example (U) [b1].zip", []string{"Example (USA)"}, 0, 0},
		{"pirate", "Example (U) [p1].zip", []string{"Example (USA)"}, 0, 0},
		{"significant-plus", "Example+ (U) [!].zip", []string{"Example (USA)"}, 0, 0},
	}
	for _, packed := range []bool{false, true} {
		for _, tc := range tests {
			t.Run(fmt.Sprint(packed)+"/"+tc.name, func(t *testing.T) {
				o, game := fixture(t, false)
				body := "title: Example\nreleases:\n"
				for _, release := range tc.releases {
					body += fmt.Sprintf("  %q: {title: Example}\n", release)
				}
				put(t, filepath.Join(game, "game.yaml"), body)
				rewriteLaunch(t, o, tc.launch)
				fingerprint, _ := os.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				if packed {
					catalogfstest.Pack(t, filepath.Dir(game))
				}
				o.Apply = true
				result, err := gamelist.Import(o)
				if err != nil || result.Matched != tc.matched || result.Ambiguous != tc.ambiguous {
					t.Fatal(result, err)
				}
				if tc.matched > 0 && result.Observations[0].MatchMethod != "legacy-release-name" {
					t.Fatal(result.Observations)
				}
				after, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				if !bytes.Equal(fingerprint, after) {
					t.Fatal("name association changed fingerprints")
				}
				repeat, err := gamelist.Import(o)
				if err != nil || repeat.Updated != 0 || repeat.MediaAdded != 0 || repeat.MediaRenamed != 0 {
					t.Fatal(repeat, err)
				}
			})
		}
	}
}

func TestLegacyNameAndReportedHashConflict(t *testing.T) {
	o, game := fixture(t, false)
	rewriteLaunch(t, o, "Example (U) [!].zip")
	hash := strings.Repeat("a", 32)
	xml, _ := os.ReadFile(o.Input)
	os.WriteFile(o.Input, bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte("<md5>"+hash+"</md5>")), 0644)
	other := filepath.Join(filepath.Dir(game), "Other")
	put(t, filepath.Join(other, "game.yaml"), "title: Other\nreleases:\n  Other (USA): {title: Other}\n")
	put(t, filepath.Join(other, "fingerprints.yaml"), "items:\n  - release: Other (USA)\n    file: {name: other.n64, size: 1, md5: "+hash+"}\n")
	o.MatchMD5 = true
	result, err := gamelist.Import(o)
	if err != nil || result.Matched != 0 || result.HashReview != 1 || result.Observations[0].MD5Status != "name-conflict" {
		t.Fatal(result, err)
	}
}

func nesFixture(t *testing.T, packed, zipped bool) (gamelist.Options, string, string, []byte) {
	t.Helper()
	o, oldGame := fixture(t, false)
	platform := filepath.Join(o.DataRoot, "platforms", "Nintendo - Nintendo Entertainment System")
	if err := os.Rename(filepath.Dir(oldGame), platform); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(platform, "Example")
	put(t, filepath.Join(platform, "_platform.yaml"), "name: Nintendo - Nintendo Entertainment System\n")
	header := []byte{'N', 'E', 'S', 0x1a, 1, 1, 0, 8, 0x10, 0, 0x77, 0x07, 0, 0, 0, 0}
	payload := bytes.Repeat([]byte{0x5a}, 24<<10)
	canonical := append(append([]byte(nil), header...), payload...)
	legacy := append([]byte{'N', 'E', 'S', 0x1a, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, payload...)
	hash := fmt.Sprintf("%x", md5.Sum(canonical))
	put(t, filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: example.nes, size: %d, md5: %s}\n", len(canonical), hash))
	rewriteLaunch(t, o, "Renamed.zip")
	xml, _ := os.ReadFile(o.Input)
	xml = bytes.ReplaceAll(xml, []byte("<system>n64</system>"), []byte("<system>nes</system>"))
	xml = bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte(fmt.Sprintf("<md5>%x</md5>", md5.Sum(legacy))))
	os.WriteFile(o.Input, xml, 0644)
	root := t.TempDir()
	file := filepath.Join(root, "renamed.nes")
	if zipped {
		file = filepath.Join(root, "renamed.zip")
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, err := z.Create("inside.nes")
		if err != nil {
			t.Fatal(err)
		}
		f.Write(canonical)
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(file, b.Bytes(), 0644)
	} else {
		os.WriteFile(file, canonical, 0644)
	}
	if packed {
		catalogfstest.Pack(t, platform)
	}
	o.MatchMD5 = true
	return o, game, file, canonical
}

func TestVerifiedLocalNESHeaderAssociationIsOptionalAndIdempotent(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, zipped := range []bool{false, true} {
			t.Run(fmt.Sprint(packed, zipped), func(t *testing.T) {
				o, game, file, _ := nesFixture(t, packed, zipped)
				beforeROM, _ := os.ReadFile(file)
				beforeFP, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				unassisted, err := gamelist.Import(o)
				if err != nil || unassisted.Matched != 0 {
					t.Fatal(unassisted, err)
				}
				o.NESROMRoot = filepath.Dir(file)
				o.Apply = true
				result, err := gamelist.Import(o)
				if err != nil || result.Matched != 1 || result.HashMatched != 1 || result.Observations[0].MatchMethod != "reported-md5-legacy-nes-header" {
					t.Fatal(result, err)
				}
				afterFP, _ := catalogfs.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				afterROM, _ := os.ReadFile(file)
				if !bytes.Equal(beforeFP, afterFP) || !bytes.Equal(beforeROM, afterROM) {
					t.Fatal("association changed ROM or fingerprints")
				}
				repeat, err := gamelist.Import(o)
				if err != nil || repeat.Updated != 0 || repeat.MediaAdded != 0 || repeat.MediaRenamed != 0 {
					t.Fatal(repeat, err)
				}
			})
		}
	}
}

func TestLocalNESHeaderAssociationRequiresVerifiedWholeFile(t *testing.T) {
	for _, mode := range []string{"changed-payload", "wrong-size", "extended-mapper", "extended-size", "derivative", "variant", "multi-rom"} {
		t.Run(mode, func(t *testing.T) {
			o, game, file, body := nesFixture(t, false, false)
			o.NESROMRoot = filepath.Dir(file)
			if mode == "changed-payload" {
				body[len(body)-1] ^= 1
			}
			if mode == "extended-mapper" || mode == "extended-size" {
				offset := 8
				if mode == "extended-size" {
					offset = 9
				}
				body[offset] = 1
				put(t, filepath.Join(game, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: example.nes, size: %d, md5: %x}\n", len(body), md5.Sum(body)))
			}
			if mode == "wrong-size" {
				text, _ := os.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				put(t, filepath.Join(game, "fingerprints.yaml"), strings.Replace(string(text), fmt.Sprint(len(body)), fmt.Sprint(len(body)+1), 1))
			}
			if mode == "derivative" {
				xml, _ := os.ReadFile(o.Input)
				os.WriteFile(o.Input, bytes.ReplaceAll(xml, []byte("Renamed.zip"), []byte("Renamed [p1].zip")), 0644)
			}
			if mode == "variant" {
				doc, _ := os.ReadFile(filepath.Join(game, "game.yaml"))
				put(t, filepath.Join(game, "game.yaml"), string(doc)+"variants:\n  Modified: {kind: translation, based_on: Example (USA)}\n")
				text, _ := os.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				put(t, filepath.Join(game, "fingerprints.yaml"), strings.Replace(string(text), "release: Example (USA)", "variant: Modified", 1))
			}
			os.WriteFile(file, body, 0644)
			if mode == "multi-rom" {
				var b bytes.Buffer
				z := zip.NewWriter(&b)
				for _, name := range []string{"one.nes", "two.nes"} {
					f, _ := z.Create(name)
					f.Write(body)
				}
				z.Close()
				os.Remove(file)
				os.WriteFile(filepath.Join(o.NESROMRoot, "multi.zip"), b.Bytes(), 0644)
			}
			result, err := gamelist.Import(o)
			if err != nil || result.Matched != 0 {
				t.Fatal(result, err)
			}
			if (mode == "variant" || mode == "derivative") && result.HashReview != 1 {
				t.Fatal(result)
			}
		})
	}
	o, _, _, _ := nesFixture(t, false, false)
	o.NESROMRoot = t.TempDir()
	o.MatchMD5 = false
	if _, err := gamelist.Import(o); err == nil {
		t.Fatal("NES root accepted without MD5")
	}
	o.MatchMD5 = true
	o.Platform = "Nintendo - Nintendo 64"
	if _, err := gamelist.Import(o); err == nil {
		t.Fatal("NES root accepted for another platform")
	}
}

func TestReportedMD5AndNamesKeepDistinctEvidenceMethods(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		o, game := fixture(t, false)
		hash := strings.Repeat("c", 32)
		put(t, filepath.Join(game, "fingerprints.yaml"), "items:\n  - release: Example (USA)\n    file: {name: example.n64, size: 1, md5: "+hash+"}\n")
		if legacy {
			rewriteLaunch(t, o, "Example (U) [!].zip")
		}
		xml, _ := os.ReadFile(o.Input)
		os.WriteFile(o.Input, bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte("<md5>"+hash+"</md5>")), 0644)
		o.MatchMD5 = true
		result, err := gamelist.Import(o)
		want := "reported-md5-and-name"
		if legacy {
			want = "reported-md5-and-legacy-name"
		}
		if err != nil || result.Matched != 1 || result.Observations[0].MatchMethod != want {
			t.Fatal(result, err)
		}
	}
}

func TestReportedMD5MustRespectAmbiguousLegacyNameCandidates(t *testing.T) {
	for _, mode := range []string{"in-set", "outside", "collision"} {
		t.Run(mode, func(t *testing.T) {
			o, game := fixture(t, false)
			rewriteLaunch(t, o, "Example (E) [!].zip")
			put(t, filepath.Join(game, "game.yaml"), "title: Example\nreleases:\n  Example (Europe) (En): {title: Example}\n  Example (Europe) (Fr): {title: Example}\n")
			a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
			if mode == "collision" {
				b = a
			}
			put(t, filepath.Join(game, "fingerprints.yaml"), "items:\n  - release: Example (Europe) (En)\n    file: {name: en.n64, size: 1, md5: "+a+"}\n  - release: Example (Europe) (Fr)\n    file: {name: fr.n64, size: 1, md5: "+b+"}\n")
			hash := a
			if mode == "outside" {
				hash = strings.Repeat("c", 32)
				other := filepath.Join(filepath.Dir(game), "Other")
				put(t, filepath.Join(other, "game.yaml"), "title: Other\nreleases:\n  Other (USA): {title: Other}\n")
				put(t, filepath.Join(other, "fingerprints.yaml"), "items:\n  - release: Other (USA)\n    file: {name: other.n64, size: 1, md5: "+hash+"}\n")
			}
			xml, _ := os.ReadFile(o.Input)
			os.WriteFile(o.Input, bytes.ReplaceAll(xml, []byte("<md5>unverified</md5>"), []byte("<md5>"+hash+"</md5>")), 0644)
			o.MatchMD5 = true
			result, err := gamelist.Import(o)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "in-set":
				if result.Matched != 1 || result.Observations[0].Release != "Example (Europe) (En)" {
					t.Fatal(result)
				}
			case "outside":
				if result.Matched != 0 || result.HashReview != 1 {
					t.Fatal(result)
				}
			case "collision":
				if result.Matched != 0 || result.Ambiguous != 1 {
					t.Fatal(result)
				}
			}
		})
	}
}

func TestLegacyNESHeaderCollisionsStayAmbiguous(t *testing.T) {
	o, game, file, body := nesFixture(t, false, false)
	o.NESROMRoot = filepath.Dir(file)
	otherBody := append([]byte(nil), body...)
	otherBody[10] ^= 1 // Same old-header bytes/payload, different registered NES 2.0 file.
	other := filepath.Join(filepath.Dir(game), "Other")
	put(t, filepath.Join(other, "game.yaml"), "title: Other\nreleases:\n  Other (USA): {title: Other}\n")
	put(t, filepath.Join(other, "fingerprints.yaml"), fmt.Sprintf("items:\n  - release: Other (USA)\n    file: {name: other.nes, size: %d, md5: %x}\n", len(otherBody), md5.Sum(otherBody)))
	os.WriteFile(filepath.Join(o.NESROMRoot, "other.nes"), otherBody, 0644)
	result, err := gamelist.Import(o)
	if err != nil || result.Matched != 0 || result.Ambiguous != 1 {
		t.Fatal(result, err)
	}
	xml, _ := os.ReadFile(o.Input)
	os.WriteFile(o.Input, bytes.ReplaceAll(xml, []byte("Renamed.zip"), []byte("Example (U) [!].zip")), 0644)
	resolved, err := gamelist.Import(o)
	if err != nil || resolved.Matched != 1 || resolved.Observations[0].Game == "" {
		t.Fatal(resolved, err)
	}
}

func TestOversizedLocalNESDoesNotBlockEligibleFiles(t *testing.T) {
	for _, zipped := range []bool{false, true} {
		t.Run(fmt.Sprint(zipped), func(t *testing.T) {
			o, _, file, _ := nesFixture(t, false, false)
			o.NESROMRoot = filepath.Dir(file)
			if zipped {
				var b bytes.Buffer
				z := zip.NewWriter(&b)
				w, err := z.CreateHeader(&zip.FileHeader{Name: "multicart.nes", Method: zip.Deflate})
				if err != nil {
					t.Fatal(err)
				}
				block := make([]byte, 1<<20)
				for i := 0; i < 33; i++ {
					if _, err = w.Write(block); err != nil {
						t.Fatal(err)
					}
				}
				z.Close()
				os.WriteFile(filepath.Join(o.NESROMRoot, "oversized.zip"), b.Bytes(), 0644)
			} else {
				f, err := os.Create(filepath.Join(o.NESROMRoot, "oversized.nes"))
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Truncate(33 << 20); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			result, err := gamelist.Import(o)
			if err != nil || result.Matched != 1 || result.HashMatched != 1 {
				t.Fatal(result, err)
			}
		})
	}
}
