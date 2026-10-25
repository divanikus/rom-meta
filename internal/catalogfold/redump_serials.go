package catalogfold

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/platformnames"
)

var xbox360SerialPattern = regexp.MustCompile(`^[A-Z0-9]{2}-?2[0-9]{3}$`)

// Redump's Xbox 360 serial identifies a product family, not necessarily a game.
// Keep it out of provider matching and require review whenever it adds a link.
func collectRedumpSerial(game *catalogGame, release *yaml.Node) {
	authority := catalogyaml.Value(release, "authority")
	serial := strings.ToUpper(strings.TrimSpace(catalogyaml.Scalar(catalogyaml.Value(authority, "serial"))))
	if catalogyaml.Scalar(catalogyaml.Value(authority, "source")) != "redump" || !xbox360SerialPattern.MatchString(serial) {
		game.IncompleteSerials = true
		return
	}
	serial = strings.ReplaceAll(serial, "-", "")
	serial = serial[:2] + "-" + serial[2:]
	game.RedumpSerials = appendUniqueStrings(game.RedumpSerials, serial)
}

func isXboxSerialHint(e Evidence) bool {
	return e.Source == "redump" && e.Field == "xbox360_serial"
}

func withXboxSerialHints(games []catalogGame) []catalogGame {
	registry := platformnames.DefaultRegistry()
	for i := range games {
		game := &games[i]
		if game.HasRomsetBindings || game.IncompleteSerials || len(game.RedumpSerials) != 1 || len(game.Releases) == 0 ||
			registry.ResolveCLI(game.Platform) != "Microsoft - Xbox 360" {
			continue
		}
		// A new discovery path must not bypass an existing keep-separate decision,
		// including one recorded against a different provider's identity.
		if len(game.IgnoredEvidence) != 0 {
			continue
		}
		game.Evidence = append(game.Evidence, Evidence{Source: "redump", Field: "xbox360_serial", Value: game.RedumpSerials[0]})
	}
	return games
}

// A hint does not downgrade an already connected provider-ID/alias group. When
// it joins otherwise separate components, the entire proposed merge needs review.
// Alias ambiguity and provider conflicts remain independent, non-overridable holds.
func serialHintsRequired(games []catalogGame, indexes []int) bool {
	hasHint := false
	for _, i := range indexes {
		for _, e := range games[i].Evidence {
			hasHint = hasHint || isXboxSerialHint(e)
		}
	}
	if !hasHint || len(indexes) < 2 {
		return false
	}
	for i, left := range indexes {
		for _, right := range indexes[i+1:] {
			if !regionalPairProof(games[left], games[right], false) {
				return true
			}
		}
	}
	connected := map[int]bool{indexes[0]: true}
	keys := map[string]bool{}
	addKeys := func(i int) {
		for _, e := range games[i].Evidence {
			if !isXboxSerialHint(e) {
				keys[evidenceKey(e)] = true
			}
		}
	}
	addKeys(indexes[0])
	for {
		changed := false
		for _, i := range indexes {
			if connected[i] {
				continue
			}
			for _, e := range games[i].Evidence {
				if !isXboxSerialHint(e) && keys[evidenceKey(e)] {
					connected[i] = true
					addKeys(i)
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	return len(connected) != len(indexes)
}
