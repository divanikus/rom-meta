package gamelist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"rommeta/internal/catalogfs"
)

func publishCards(root string, cards map[string]*card, updates map[string][]byte, in *input, progress func(string, int, int), beforePublish func(string) error) (stage string, err error) {
	runRoot := filepath.Join(root, ".rommeta", "imports")
	if err = os.MkdirAll(runRoot, 0700); err != nil {
		return
	}
	stage, err = os.MkdirTemp(runRoot, "es-de-")
	if err != nil {
		return
	}
	staged := map[string]map[string]string{}
	games := make([]string, 0, len(updates))
	for game := range updates {
		games = append(games, game)
	}
	sort.Strings(games)
	renames := map[string]map[string]string{}
	for i, game := range games {
		if progress != nil {
			progress("staging "+game, i+1, len(games))
		}
		c := cards[game]
		rel, _ := filepath.Rel(root, game)
		dir := filepath.Join(stage, "stage", rel)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return
		}
		original, e := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
		if e != nil {
			return stage, e
		}
		if !bytes.Equal(original, c.original) {
			return stage, fmt.Errorf("catalog changed during planning: %s", game)
		}
		backup := filepath.Join(stage, "backups", rel, "game.yaml")
		if err = os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
			return
		}
		if err = os.WriteFile(backup, c.original, 0600); err != nil {
			return
		}
		metadata := filepath.Join(dir, "game.yaml")
		if err = os.WriteFile(metadata, updates[game], 0600); err != nil {
			return
		}
		files := map[string]string{"game.yaml": metadata}
		for dest, a := range c.media {
			var src io.ReadCloser
			if a.catalog {
				src, e = catalogfs.Open(a.input)
			} else if in != nil {
				src, e = in.open(a.input)
			} else {
				e = fmt.Errorf("missing media input")
			}
			if e != nil {
				return stage, e
			}
			file, e := os.CreateTemp(dir, "asset-")
			if e != nil {
				src.Close()
				return stage, e
			}
			h := sha256.New()
			n, e := io.Copy(io.MultiWriter(file, h), io.LimitReader(src, 512<<20+1))
			src.Close()
			ce := file.Close()
			if e != nil {
				return stage, e
			}
			if ce != nil {
				return stage, ce
			}
			if n != a.size || hex.EncodeToString(h.Sum(nil)) != a.digest {
				return stage, fmt.Errorf("source media changed during staging")
			}
			files[dest] = file.Name()
		}
		staged[game] = files
		if len(c.renames) > 0 {
			renames[filepath.ToSlash(rel)] = c.renames
		}
	}
	receipt, e := json.MarshalIndent(renames, "", "  ")
	if e != nil {
		return stage, e
	}
	if err = os.WriteFile(filepath.Join(stage, "media-renames.json"), receipt, 0600); err != nil {
		return
	}
	if beforePublish != nil {
		if err = beforePublish(stage); err != nil {
			return
		}
	}
	for i, game := range games {
		if progress != nil {
			progress("publishing "+game, i+1, len(games))
		}
		original, e := catalogfs.ReadFile(filepath.Join(game, "game.yaml"))
		if e != nil {
			return stage, e
		}
		if !bytes.Equal(original, cards[game].original) {
			return stage, fmt.Errorf("catalog changed before publication: %s", game)
		}
		if e = publishFiles(game, staged[game], cards[game].remove); e != nil {
			return stage, fmt.Errorf("retained stage and backups %s: %w", stage, e)
		}
	}
	err = os.RemoveAll(filepath.Join(stage, "stage"))
	return
}
