package exporter

import (
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
	"rommeta/internal/medianames"
	"sort"
	"strconv"
	"strings"
)

type CatalogAsset struct {
	Style         string `json:"style,omitempty"`
	StyleEvidence string `json:"style_evidence,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	Path          string `json:"path"`
	Game          string `json:"game"`
	Kind          string `json:"kind"`
	Source        string `json:"source"`
	Region        string `json:"region,omitempty"`
	Language      string `json:"language,omitempty"`
	Variant       string `json:"variant,omitempty"`
	Size          int64  `json:"size"`
}

// InventoryAvailableMedia describes local availability only. It never treats an
// unsupported role as an upload, touches a server filesystem or starts a fetch.
func InventoryMedia(root string, records []Record) ([]CatalogAsset, error) {
	seen := map[string]bool{}
	assets := []CatalogAsset{}
	for _, record := range records {
		game := record.Game
		if seen[game] {
			continue
		}
		seen[game] = true
		var doc struct {
			Releases map[string]any `yaml:"releases"`
		}
		gameRoot := filepath.Join(root, filepath.FromSlash(game))
		content, e := catalogfs.ReadFile(filepath.Join(gameRoot, "game.yaml"))
		if e == nil {
			e = yaml.Unmarshal(content, &doc)
		}
		if e != nil {
			return nil, e
		}
		styles, e := savedMediaStyles(gameRoot)
		if e != nil {
			return nil, e
		}
		mediaRoot := filepath.Join(gameRoot, "media")
		e = catalogfs.WalkDir(mediaRoot, func(name string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if name != mediaRoot {
					rel, e := filepath.Rel(mediaRoot, name)
					if e != nil {
						return e
					}
					if _, ok := doc.Releases[rel]; !ok {
						return fs.SkipDir
					}
				}
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			rel, e := filepath.Rel(root, name)
			if e != nil {
				return e
			}
			variant, parts := medianames.SplitVariant(strings.Split(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), "."))
			kind := parts[0]
			if dash := strings.LastIndex(kind, "-"); dash >= 0 {
				if _, e := strconv.Atoi(kind[dash+1:]); e == nil {
					kind = kind[:dash]
				}
			}
			source, region, lang := "local", "", ""
			if len(parts) > 1 {
				source = parts[len(parts)-1]
			}
			if len(parts) > 2 {
				region = parts[1]
			}
			if len(parts) > 3 {
				lang = parts[2]
			}
			style := mediaStyle{}
			if kind == "box-front" {
				style = assetStyle(styles, name)
			}
			assets = append(assets, CatalogAsset{Style: style.style, StyleEvidence: style.evidence, SourceType: style.sourceType, Path: filepath.ToSlash(rel), Game: game, Kind: kind, Source: source, Region: region, Language: lang, Variant: variant, Size: info.Size()})
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })

	return assets, nil
}
