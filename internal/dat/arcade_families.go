package dat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

// Folder families are local catalog choices. Keep the reviewed board routes
// intact: belonging to a family never asserts ROM or machine compatibility.
func groupArcadeFamilies(r *ArcadeRouting) {
	r.Families = map[string]string{}
	family := func(name, source string, boards ...string) {
		target := "Arcade - " + name
		r.Platforms[target] = ArcadePlatform{
			Kind:           "hardware",
			NamingEvidence: "RomMeta catalog family grouping; board distinctions retained; https://github.com/mamedev/mame/blob/mame0289/src/mame/" + source,
		}
		for _, board := range boards {
			r.Families["Arcade - "+board] = target
		}
	}
	family("Sega - System 16", "sega/segas16b.cpp", "Sega - System 16A", "Sega - System 16B", "Sega - System 16C")
	family("Sega - System 32", "sega/segas32.cpp", "Sega - System 32", "Sega - Multi 32")
	family("Namco - NA", "namco/namcona1.cpp", "Namco - NA-1", "Namco - NA-2")
	family("Namco - NB", "namco/namconb1.cpp", "Namco - NB-1", "Namco - NB-2")
	family("Namco - System 22", "namco/namcos22.cpp", "Namco - System 22", "Namco - Super System 22")
	family("Namco - System 23", "namco/namcos23.cpp", "Namco - System 23", "Namco - Super System 23", "Namco - System 23 Evolution 2")
	family("Irem - M72", "irem/m72.cpp", "Irem - M72", "Irem - M81", "Irem - M82", "Irem - M84", "Irem - M85")
}

func arcadeCatalogPlatform(board string, r ArcadeRouting) string {
	if family := r.Families[board]; family != "" {
		return family
	}
	return board
}

func validateArcadeFamilies(r ArcadeRouting) error {
	for board, family := range r.Families {
		from, fromOK := r.Platforms[board]
		to, toOK := r.Platforms[family]
		if !fromOK || !toOK {
			return fmt.Errorf("family mapping refers to undefined board or platform: %s -> %s", board, family)
		}
		if from.Kind != "hardware" || to.Kind != "hardware" {
			return fmt.Errorf("family mapping requires hardware, not a collection: %s -> %s", board, family)
		}
		if next := r.Families[family]; next != "" && next != family {
			return fmt.Errorf("family mapping must be one level (no chains or cycles): %s -> %s", board, family)
		}
	}
	return nil
}

// Different drivers may describe revisions of one reviewed family. Unknown
// hardware, collections and merely sharing a manufacturer do not relax folding.
func sameArcadeFoldFamily(a, b string, r ArcadeRouting) bool {
	return a != "" && b != "" && a != b &&
		(r.Families[a] != "" || r.Families[b] != "") &&
		arcadeCatalogPlatform(a, r) == arcadeCatalogPlatform(b, r)
}

// A new routing hash must not silently duplicate a previously imported set
// in another platform. Relocation must eventually update every old binding.
func arcadePreviousBindingConflicts(root, current string, selected map[string]ArcadeBinding) ([]string, error) {
	base := filepath.Join(root, "bindings", "romsets", "mame")
	snapshots, err := catalogfs.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, snapshot := range snapshots {
		if !snapshot.IsDir() || snapshot.Name() == current || strings.HasPrefix(snapshot.Name(), ".") {
			continue
		}
		previous, err := readArcadeBindings(filepath.Join(base, snapshot.Name(), "catalog.yaml"))
		if err != nil {
			return nil, err
		}
		for _, name := range sortedArcadeKeys(selected) {
			next := selected[name]
			old, exists := previous[name]
			if exists && (old.Platform != next.Platform || old.Game != next.Game || old.Release != next.Release) {
				conflicts = append(conflicts, fmt.Sprintf("%s already bound in snapshot %s to %s/%s; platform/card moves require review", name, snapshot.Name(), old.Platform, old.Game))
			}
		}
	}
	return conflicts, nil
}
