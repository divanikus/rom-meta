package dat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalog"
)

func TestArcadeSharedCatalogIdentityAndPartialImport(t *testing.T) {
	input, classes := arcadeFixture(t)
	root := t.TempDir()
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true, Sets: []string{"sf2ua"}}
	first, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	b := first.Bindings["sf2ua"]
	if len(first.Platforms) != 1 || first.Platforms[0].Platform != "Arcade" || b.Platform != "Arcade" || b.Family != "Arcade - Capcom - CPS-1" {
		t.Fatal(first.Platforms, b)
	}
	if len(first.Bindings) != 1 {
		t.Fatal("unselected sets imported")
	}
	dirs, err := os.ReadDir(filepath.Join(root, "platforms"))
	if err != nil || len(dirs) != 1 || dirs[0].Name() != "Arcade" {
		t.Fatal(dirs, err)
	}
	file := filepath.Join(root, "platforms", "Arcade", b.Game, "game.yaml")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Arcade   ArcadeFamily
		Releases map[string]any
	}
	if err = yaml.Unmarshal(original, &card); err != nil {
		t.Fatal(err)
	}
	if len(card.Releases) != 1 || card.Releases["sf2ua"] == nil || card.Arcade.Platform != b.Family || card.Arcade.Kind != "hardware" || card.Arcade.NamingEvidence == "" {
		t.Fatal(card)
	}
	// Local additions must survive while the importer owns platform evidence.
	edited := strings.Replace(string(original), "arcade:\n", "arcade:\n    local_note: Keep this\n", 1) + "\nuser_note: Keep this too\n"
	if edited == string(original) {
		t.Fatal("fixture edit failed")
	}
	if err = os.WriteFile(file, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	options.Sets = []string{"cross"}
	second, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	c := second.Bindings["cross"]
	if c.Game == b.Game || c.Family == b.Family || c.Platform != "Arcade" {
		t.Fatal("cross-platform title collision", b, c)
	}
	options.Sets = []string{"sf2", "sf2ua", "cross"}
	all, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if all.Bindings["sf2ua"].Game != b.Game || all.Bindings["sf2"].Game != b.Game || all.Bindings["cross"].Game != c.Game || all.Counts["games"] != 2 {
		t.Fatal("subset-dependent identity", all.Bindings)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(updated, &card); err != nil {
		t.Fatal(err)
	}
	if len(card.Releases) != 2 || !strings.Contains(string(updated), "local_note: Keep this") || !strings.Contains(string(updated), "user_note: Keep this too") {
		t.Fatal(string(updated))
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validation: %v %v", err, issues)
	}
	options.Apply = false
	again, err := ImportArcade(input, options)
	if err != nil || len(again.Conflicts) != 0 || again.Platforms[0].Changes.Added != 0 {
		t.Fatalf("repeat: %v %+v", err, again)
	}
	// Validation must see semantic membership rather than trusting the storage name.
	corrupt := strings.Replace(string(updated), "platform: "+b.Family, "platform: "+c.Family, 1)
	if corrupt == string(updated) {
		t.Fatal("family corruption fixture failed")
	}
	if err = os.WriteFile(file, []byte(corrupt), 0600); err != nil {
		t.Fatal(err)
	}
	issues, err = catalog.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "card arcade platform disagrees") {
			found = true
		}
	}
	if !found {
		t.Fatal("family corruption passed validation")
	}
}

func TestArcadeSharedFolderCollisionWithLiteralSuffix(t *testing.T) {
	bundles := []gameBundle{
		{Title: "Game", Folder: "Game", Releases: []datGame{{Name: "a"}}},
		{Title: "game", Folder: "game", Releases: []datGame{{Name: "b"}}},
		{Title: "Game [a]", Folder: "Game [a]", Releases: []datGame{{Name: "literal"}}},
	}
	assignSharedArcadeFolders(bundles)
	seen := map[string]bool{}
	for _, b := range bundles {
		if seen[strings.ToLower(b.Folder)] {
			t.Fatal("case-insensitive collision", bundles)
		}
		seen[strings.ToLower(b.Folder)] = true
	}
	if bundles[2].Folder != "Game [a]" || bundles[0].Folder != "Game [a]-" {
		t.Fatal(bundles)
	}
}

func TestArcadeSharedCatalogRejectsLayoutOverride(t *testing.T) {
	file := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(file, []byte(`{"version":1,"catalog_platform":"Arcade - Custom"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadArcadeRouting(file); err == nil {
		t.Fatal("accepted alternate storage layout")
	}
}

func TestArcadeSharedFamilyReassignmentRequiresReview(t *testing.T) {
	input, classes := arcadeFamilyFixture(t)
	root := t.TempDir()
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true, Sets: []string{"emeralda"}}
	before, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	b := before.Bindings["emeralda"]
	path := filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(t.TempDir(), "routing.json")
	if err = os.WriteFile(override, []byte(`{"version":1,"families":{"Arcade - Namco - NA-2":"Arcade - Namco - NA-2"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	options.RoutingPath = override
	result, err := ImportArcade(input, options)
	if err == nil || result.Applied {
		t.Fatal("changed family without review")
	}
	found := false
	for _, conflict := range result.Conflicts {
		if strings.Contains(conflict, "changes arcade family") {
			found = true
		}
	}
	if !found {
		t.Fatal(result.Conflicts)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatal("old card changed")
	}
}
