package gamelist_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/gamelist"
)

func TestImportLeavesUnmatchedCardsAndMediaUntouched(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, matched := range []bool{false, true} {
			t.Run(fmt.Sprintf("packed=%t/matched=%t", packed, matched), func(t *testing.T) {
				o, game := fixture(t, false)
				other := filepath.Join(filepath.Dir(game), "Unrelated")
				name := "media/Unrelated (USA)/box-front.unspecified.und.0123456789ab.gamelist.sample.png"
				original := []byte("title: Unrelated\nreleases:\n  Unrelated (USA): {}\nsources:\n  gamelist.sample:\n    matches:\n      - media:\n          box_front: [{url: '" + name + "'}]\n")
				put(t, filepath.Join(other, "game.yaml"), string(original))
				put(t, filepath.Join(other, filepath.FromSlash(name)), "unrelated synthetic artwork")
				if packed {
					catalogfstest.Pack(t, filepath.Dir(game))
				}
				if !matched {
					put(t, o.Input, "<gameList><provider><system>Nintendo - Nintendo 64</system></provider><game><path>Unknown.z64</path></game></gameList>")
				}
				o.Apply = true
				result, err := gamelist.Import(o)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if matched {
					want = 1
				}
				if result.Matched != want || result.Updated != want || result.MediaRenamed != 0 {
					t.Fatalf("import affected unrelated cards: %+v", result)
				}
				body, err := catalogfs.ReadFile(filepath.Join(other, "game.yaml"))
				if err != nil || !bytes.Equal(body, original) {
					t.Fatalf("unrelated YAML changed: %s, %v", body, err)
				}
				media, err := catalogfs.ReadFile(filepath.Join(other, filepath.FromSlash(name)))
				if err != nil || string(media) != "unrelated synthetic artwork" {
					t.Fatal("unrelated media changed", err)
				}
				if !matched {
					if _, err := os.Stat(filepath.Join(o.DataRoot, ".rommeta")); !os.IsNotExist(err) {
						t.Fatal("unmatched import created runtime state", err)
					}
				}
			})
		}
	}
}
