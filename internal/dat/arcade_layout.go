package dat

import (
	"strings"
	"unicode/utf16"
)

// ArcadeFamily is semantic platform membership, independent of storage folders.
// Collections such as Other explicitly distinguish unknown/mixed hardware.
type ArcadeFamily struct {
	Platform       string `json:"platform" yaml:"platform"`
	Kind           string `json:"kind" yaml:"kind"`
	NamingEvidence string `json:"naming_evidence" yaml:"naming_evidence"`
}

// A short set ID is a last-resort path discriminator, not a hardware label.
// Use the already reviewed family when equal display titles span known boards.
func labelArcadeHardwareCollisions(bundles []gameBundle, families map[string]ArcadeFamily) {
	byTitle := map[string][]int{}
	for i, b := range bundles {
		byTitle[strings.ToLower(safePathName(b.Title))] = append(byTitle[strings.ToLower(safePathName(b.Title))], i)
	}
	for _, indexes := range byTitle {
		if len(indexes) < 2 {
			continue
		}
		for _, i := range indexes {
			b := &bundles[i]
			family := families[b.Releases[0].Name]
			label := ""
			if family.Kind == "hardware" {
				label = strings.TrimPrefix(family.Platform, "Arcade - ")
			} else {
				// Unknown boards remain unknown. Maker/year describe provenance,
				// not a guessed hardware family; prefer the actual MAME parent.
				var chosen *datGame
				for j := range b.Releases {
					g := &b.Releases[j]
					if arcadeSpecial(*g) || g.Manufacturer == "" || g.Year == "" {
						continue
					}
					if chosen == nil || g.CloneOf == "" {
						chosen = g
					}
					if g.CloneOf == "" {
						break
					}
				}
				if chosen != nil {
					label = chosen.Manufacturer
					if len(chosen.Year) == 4 && strings.Trim(chosen.Year, "0123456789") == "" {
						label += ", " + chosen.Year
					}
				}
			}
			if label != "" {
				b.Title += " (" + label + ")"
				b.Folder = safePathName(b.Title)
			}
		}
	}
}

// All titles share one directory now. Resolve every case-insensitive collision
// against the full eligible source, not the selected subset or insertion order.
func assignSharedArcadeFolders(bundles []gameBundle) {
	counts := map[string]int{}
	used := map[string]bool{}
	for _, b := range bundles {
		counts[strings.ToLower(b.Folder)]++
	}
	for _, b := range bundles {
		if counts[strings.ToLower(b.Folder)] == 1 {
			used[strings.ToLower(b.Folder)] = true
		}
	}
	for i := range bundles {
		b := &bundles[i]
		if counts[strings.ToLower(b.Folder)] < 2 {
			continue
		}
		base := []rune(b.Folder)
		for len(utf16.Encode(base)) > 160 {
			base = base[:len(base)-1]
		}
		folder := strings.TrimSpace(string(base)) + " [" + b.Releases[0].Name + "]"
		for used[strings.ToLower(folder)] {
			folder += "-"
		}
		b.Folder = folder
		used[strings.ToLower(folder)] = true
	}
}
