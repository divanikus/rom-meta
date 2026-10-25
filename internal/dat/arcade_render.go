package dat

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/releasepolicy"
)

func arcadeYAML(value any) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		panic(err)
	} // All callers use only scalar/map/slice values.
	return string(data)
}

func renderArcadePlatform(plan Plan) string {
	return arcadeYAML(map[string]any{
		"name": plan.PlatformName, "display_name": plan.PlatformName, "category": "Arcade",
		"kind":      plan.ArcadePlatformKind,
		"naming":    map[string]any{"evidence": plan.ArcadeNamingEvidence},
		"authority": map[string]any{"source": "mame", "scope": "set identities and descriptions"},
	})
}

func renderArcadeGame(plan Plan, bundle gameBundle, options ImportOptions) string {
	family := plan.ArcadeFamilies[bundle.Releases[0].Name]
	priority := options.RegionPriority
	if p := options.PlatformRegionPriority[plan.PlatformName]; len(p) > 0 {
		priority = p
	}
	if p := options.PlatformRegionPriority[family.Platform]; len(p) > 0 {
		priority = p
	}
	releases := map[string]any{}
	best, bestScore := "", -100000
	for _, g := range bundle.Releases {
		region := arcadeRegion(g.Description)
		score := releasepolicy.Preference("Arcade ("+region+")", priority, options.RegionGroups)
		if arcadeSpecial(g) {
			score -= 1000
		}
		if score > bestScore || (score == bestScore && (best == "" || g.Name < best)) {
			best, bestScore = g.Name, score
		}
		authority := map[string]any{"source": "mame", "entry_name": g.Name, "entry_id": g.Name}
		if g.CloneOf != "" {
			authority["cloneof_id"] = g.CloneOf
		}
		release := map[string]any{
			"title":     g.Description,
			"authority": authority,
			"mame":      map[string]any{"snapshot": plan.ArcadeSnapshot, "cloneof": g.CloneOf, "romof": g.ROMOf, "sampleof": g.SampleOf, "sourcefile": g.SourceFile, "year": g.Year, "manufacturer": g.Manufacturer, "status": g.DriverStatus},
		}
		if board := plan.ArcadeHardware[g.Name]; board != "" {
			release["mame"].(map[string]any)["hardware"] = board
		}
		if region != "" {
			release["regions"] = []string{region}
		}
		releases[g.Name] = release
	}
	return arcadeYAML(map[string]any{"title": bundle.Title, "arcade": family, "default_release": best, "releases": releases})
}

func renderArcadeFingerprints(plan Plan, bundle gameBundle) (string, error) {
	items := []any{}
	for _, g := range bundle.Releases {
		roms, err := gameROMs(g)
		if err != nil {
			return "", err
		}
		for _, group := range []struct {
			kind     string
			payloads []datROM
		}{{"rom-component", roms}, {"chd-disk", g.Disks}} {
			for i, p := range group.payloads {
				file := map[string]any{"name": p.Name, "kind": group.kind}
				if p.Size != "" {
					file["size"] = p.Size
				}
				for k, v := range map[string]string{"crc32": p.CRC32, "md5": p.MD5, "sha1": p.SHA1, "sha256": p.SHA256} {
					if v != "" {
						file[k] = v
					}
				}
				source := map[string]any{"kind": "dat", "name": "MAME", "system": plan.PlatformName,
					"snapshot": plan.ArcadeSnapshot, "entry_name": g.Name, "entry_id": g.Name,
					"dat_file": filepath.Base(firstNonEmpty(plan.MemberPath, plan.InputPath))}
				item := map[string]any{"id": fmt.Sprintf("mame.%s.%s.%s.%d", plan.ArcadeSnapshot, g.Name, group.kind, i+1), "release": g.Name, "source": source, "file": file}
				if p.Status != "" {
					item["dump"] = map[string]string{"status": p.Status}
				}
				items = append(items, item)
			}
		}
	}
	return arcadeYAML(map[string]any{"items": items}), nil
}

func sortedArcadeKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func arcadeSafeBinding(b ArcadeBinding) bool {
	return (b.Hardware == "" || (strings.HasPrefix(b.Hardware, "Arcade - ") && b.Hardware == safePathName(b.Hardware))) &&
		(b.Platform == "Arcade" || strings.HasPrefix(b.Platform, "Arcade - ")) && b.Platform == safePathName(b.Platform) &&
		(b.Family == "" || (strings.HasPrefix(b.Family, "Arcade - ") && b.Family == safePathName(b.Family))) &&
		(b.Platform != "Arcade" || b.Family != "") &&
		b.Game != "" && b.Game == safePathName(b.Game) && b.Game != "." &&
		!strings.HasPrefix(b.Game, ".") && !strings.HasPrefix(b.Game, "_") &&
		arcadeShortNameRE.MatchString(b.Release)
}

// Import owns platform routing/provenance, while display names, provider mappings
// and other local platform fields survive a repeated snapshot import.
func mergeArcadePlatformYAML(existing, generated string) (string, error) {
	old, err := catalogyaml.Parse([]byte(existing))
	if err != nil {
		return "", err
	}
	next, err := catalogyaml.Parse([]byte(generated))
	if err != nil {
		return "", err
	}
	if err = catalogyaml.Mapping(old.Root(), func(field string, value *yaml.Node) error {
		if field == "display_name" || catalogyaml.Value(next.Root(), field) == nil {
			return catalogyaml.Set(next.Root(), field, value)
		}
		return nil
	}); err != nil {
		return "", err
	}
	data, err := next.Marshal()
	return string(data), err
}
