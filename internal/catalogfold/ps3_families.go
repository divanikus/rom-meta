package catalogfold

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/platformnames"
	"rommeta/internal/releasepolicy"
)

//go:embed ps3_families.yaml
var ps3FamilyData []byte

type ps3DiscFamily struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Source   string   `yaml:"source"`
	Reviewed string   `yaml:"reviewed"`
	Notes    string   `yaml:"notes"`
	Serials  []string `yaml:"serials"`
}

var ps3DiscSerial = regexp.MustCompile(`^B[CL][AEHJKU]S[0-9]{5}$|^BLJM[0-9]{5}$`)
var ps3FamilyID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var builtinPS3Families = sync.OnceValues(func() (map[string]ps3DiscFamily, error) {
	return parsePS3Families(ps3FamilyData)
})

func parsePS3Families(contents []byte) (map[string]ps3DiscFamily, error) {
	var document struct {
		Version  int             `yaml:"version"`
		Families []ps3DiscFamily `yaml:"families"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("PS3 identity registry must contain one YAML document")
	}
	if document.Version != 1 || len(document.Families) == 0 {
		return nil, fmt.Errorf("invalid PS3 identity registry version or empty families")
	}
	result := map[string]ps3DiscFamily{}
	ids := map[string]bool{}
	for _, family := range document.Families {
		source, err := url.Parse(family.Source)
		_, dateErr := time.Parse("2006-01-02", family.Reviewed)
		if !ps3FamilyID.MatchString(family.ID) || ids[family.ID] || strings.TrimSpace(family.Title) == "" ||
			strings.TrimSpace(family.Notes) == "" || dateErr != nil || err != nil || source.Scheme != "https" ||
			source.Host == "" || source.User != nil || len(family.Serials) == 0 {
			return nil, fmt.Errorf("invalid or duplicate PS3 identity %q", family.ID)
		}
		ids[family.ID] = true
		for _, serial := range family.Serials {
			if !ps3DiscSerial.MatchString(serial) {
				return nil, fmt.Errorf("invalid PS3 disc serial %q", serial)
			}
			if previous, exists := result[serial]; exists {
				return nil, fmt.Errorf("PS3 serial %s repeated in %s and %s", serial, previous.ID, family.ID)
			}
			result[serial] = family
		}
	}
	return result, nil
}

// Preserve every serial/release boundary. Unknown suffixes and partially known
// cards must not acquire a full-game identity from one recognized component.
func collectPS3Serials(game *catalogGame, name string, release *yaml.Node) {
	authority := catalogyaml.Value(release, "authority")
	if catalogyaml.Scalar(catalogyaml.Value(authority, "source")) != "redump" ||
		releasepolicy.Preference(name, nil, nil) < 0 {
		game.IncompletePS3Serials = true
		return
	}
	for _, raw := range strings.Split(catalogyaml.Scalar(catalogyaml.Value(authority, "serial")), ",") {
		serial := strings.ToUpper(strings.TrimSpace(raw))
		serial = strings.NewReplacer("-", "", " ", "").Replace(serial)
		if !ps3DiscSerial.MatchString(serial) {
			game.IncompletePS3Serials = true
			return
		}
		game.PS3Serials = appendUniqueStrings(game.PS3Serials, serial)
	}
}

func withPS3DiscFamilies(games []catalogGame) ([]catalogGame, error) {
	registry, err := builtinPS3Families()
	if err != nil {
		return nil, fmt.Errorf("PS3 identity registry: %w", err)
	}
	platforms := platformnames.DefaultRegistry()
	for i := range games {
		game := &games[i]
		if platforms.ResolveCLI(game.Platform) != "Sony - PlayStation 3" || game.HasRomsetBindings ||
			game.IncompletePS3Serials || len(game.PS3Serials) == 0 || len(game.Releases) == 0 || len(game.IgnoredEvidence) != 0 {
			continue
		}
		var family ps3DiscFamily
		complete := true
		for _, serial := range game.PS3Serials {
			found, ok := registry[serial]
			if !ok || (family.ID != "" && family.ID != found.ID) {
				complete = false
				break
			}
			family = found
		}
		if complete {
			game.Evidence = append(game.Evidence, Evidence{Source: "ps3", Field: "disc_family", Value: family.ID, URL: family.Source})
		}
	}
	return games, nil
}

func isPS3DiscFamily(e Evidence) bool {
	return e.Source == "ps3" && e.Field == "disc_family"
}

// A provider/alias bridge must not pull an unverified third card, another
// registered game, or a content edition into an otherwise exact serial group.
func ps3FamilyGroupHoldReason(games []catalogGame, indexes []int) string {
	family := ""
	for _, index := range indexes {
		for _, evidence := range games[index].Evidence {
			if isPS3DiscFamily(evidence) {
				if family != "" && family != evidence.Value {
					return "conflicting PS3 disc families"
				}
				family = evidence.Value
			}
		}
	}
	if family == "" {
		return ""
	}
	for _, index := range indexes {
		covered := false
		for _, evidence := range games[index].Evidence {
			covered = covered || (isPS3DiscFamily(evidence) && evidence.Value == family)
		}
		if !covered {
			return "PS3 disc family does not cover every member of the connected group"
		}
	}
	return strings.Join(groupDirectIDConflicts(games, indexes), "; ")
}
