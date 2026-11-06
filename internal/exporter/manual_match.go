package exporter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogyaml"
)

// ManualMatch is a user's selection for one retained input, not hash evidence.
type ManualMatch struct {
	Game         string `json:"game"`
	Release      string `json:"release,omitempty"`
	GameRevision string `json:"game_revision"`
}
type ManualTarget struct {
	Game         string   `json:"game"`
	Title        string   `json:"title"`
	GameRevision string   `json:"game_revision"`
	Releases     []string `json:"releases"`
}

func CanManuallyMatch(r Record) bool {
	return r.InputGuard != nil && !r.Hidden && !r.SourceOnly &&
		(r.Status == "unmatched" || r.Status == "ambiguous" || r.MatchedBy == "game-name" || r.MatchedBy == "manual")
}
func ManualMatchPlatforms(p *Plan) ([]string, error) {
	if p.SourceAPI != "" {
		return nil, fmt.Errorf("manual selection requires a local catalog")
	}
	selector, err := LoadPlatformSelector(p.DataRoot)
	if err != nil {
		return nil, err
	}
	return selector.Scope(p.Platform), nil
}
func manualGame(p *Plan, name string) (*game, error) {
	parts := strings.Split(name, "/")
	if !ValidInputPath(name) || len(parts) != 3 || parts[0] != "platforms" || strings.HasPrefix(parts[2], ".") || strings.HasPrefix(parts[2], "_") {
		return nil, fmt.Errorf("select a catalog game from this platform")
	}
	platforms, err := ManualMatchPlatforms(p)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, platform := range platforms {
		if platform == parts[1] {
			allowed = true
		}
	}
	if !allowed {
		return nil, fmt.Errorf("selected game belongs to another platform")
	}
	root, err := realPath(p.DataRoot)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	actual, err := realPath(path)
	if err != nil {
		return nil, err
	}
	// A catalog alias cannot redirect a manual selection into another platform.
	if !within(filepath.Join(root, "platforms", parts[1]), actual) {
		return nil, fmt.Errorf("selected game escapes its platform")
	}
	doc, digest, err := lookupDocument(root, filepath.Join(path, "game.yaml"))
	if err != nil {
		return nil, err
	}
	g := &game{path: path, name: field(doc, "title"), root: doc, documentDigest: digest, context: p.Context}
	if g.name == "" {
		g.name = parts[2]
	}
	return g, nil
}
func ReadManualTarget(p *Plan, name string) (ManualTarget, error) {
	g, err := manualGame(p, name)
	if err != nil {
		return ManualTarget{}, err
	}
	result := ManualTarget{Game: name, Title: g.name, GameRevision: g.documentDigest, Releases: []string{}}
	releases := catalogyaml.Value(g.root, "releases")
	if releases != nil {
		for i := 0; i+1 < len(releases.Content); i += 2 {
			result.Releases = append(result.Releases, releases.Content[i].Value)
		}
	}
	sort.Strings(result.Releases)
	return result, nil
}
func ValidateManualMatch(p *Plan, r Record, choice ManualMatch) error {
	if !CanManuallyMatch(r) {
		return fmt.Errorf("this input cannot be manually assigned")
	}
	if err := r.InputGuard.Check(p.Context); err != nil {
		return err
	}
	g, err := manualGame(p, choice.Game)
	if err != nil {
		return err
	}
	return validateManualGame(g, choice)
}
func validateManualGame(g *game, choice ManualMatch) error {
	if choice.GameRevision == "" || choice.GameRevision != g.documentDigest {
		return fmt.Errorf("selected card changed; choose it again")
	}
	if choice.Release != "" && catalogyaml.Value(catalogyaml.Value(g.root, "releases"), choice.Release) == nil {
		return fmt.Errorf("selected release does not belong to this card")
	}
	return nil
}

// BindManual preserves ROM observations and guards. It changes only the metadata
// association in this export and never writes fingerprints or library documents.
func BindManual(p *Plan, r Record, choice ManualMatch) (Record, error) {
	if !CanManuallyMatch(r) {
		return Record{}, fmt.Errorf("this input cannot be manually assigned")
	}
	if err := r.InputGuard.Check(p.Context); err != nil {
		return Record{}, err
	}
	g, err := manualGame(p, choice.Game)
	if err != nil {
		return Record{}, err
	}
	if err = validateManualGame(g, choice); err != nil {
		return Record{}, err
	}
	r.Variant, r.VariantInfo, r.TitleContext, r.SelectedTitle = "", nil, nil, nil
	r.DiscVariants, r.Candidates = nil, nil
	r.Warnings = append([]string(nil), r.Warnings...)
	r.Status, r.MatchedBy, r.Reason = "matched", "manual", "Selected by you for this export; not verified by fingerprint"
	if err = attachRecord(p.DataRoot, g, choice.Release, &r, p.Profile, p.System); err != nil {
		return Record{}, err
	}
	return r, nil
}
