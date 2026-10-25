package dat

import "testing"

func TestArcadeTechnicalRootsAreScopedAndRetainContent(t *testing.T) {
	games := []datGame{
		{Name: "cart", Description: "Example (Japan, Rev A)", SourceFile: "sega/naomi.cpp"},
		{Name: "disc", Description: "Example (Export) (GDS-0011)", SourceFile: "sega/naomi.cpp"},
		{Name: "otherboard", Description: "Example (GDS-0011)", SourceFile: "sega/naomi.cpp"},
		{Name: "medal", Description: "Example (Japan, medal)", SourceFile: "sega/naomi.cpp"},
		{Name: "named", Description: "Example (Japan) Version B", SourceFile: "sega/naomi.cpp"},
		{Name: "fdha", Description: "Dance Dance Revolution SuperNOVA (FDH:A:A:A:2006071300)", SourceFile: "konami/kpython2.cpp"},
		{Name: "fdhe", Description: "Dancing Stage SuperNOVA (FDH:E:A:A:2006072500)", SourceFile: "konami/kpython2.cpp"},
		{Name: "gdju", Description: "Dance Dance Revolution SuperNOVA 2 (GDJ:U:A:A:2007100800)", SourceFile: "konami/kpython2.cpp"},
		{Name: "unrelated1", Description: "Same Name (World)", SourceFile: "capcom/cps1.cpp"},
		{Name: "unrelated2", Description: "Same Name (Japan)", SourceFile: "capcom/cps1.cpp"},
	}
	hw := map[string]string{}
	for _, g := range games {
		hw[g.Name] = "Arcade - Sega - Naomi"
	}
	hw["otherboard"] = "Arcade - Sega - Naomi 2"
	for _, id := range []string{"fdha", "fdhe", "gdju"} {
		hw[id] = "Arcade - Konami - Python 2"
	}
	r := defaultArcadeRouting()
	bundles, _ := arcadeBundleGamesWithHardware(games, true, hw, r)
	member := map[string]string{}
	for _, b := range bundles {
		for _, g := range b.Releases {
			member[g.Name] = b.Folder
		}
	}
	if len(bundles) != 8 || member["cart"] != member["disc"] || member["fdha"] != member["fdhe"] {
		t.Fatal(member)
	}
	for _, pair := range [][2]string{{"cart", "otherboard"}, {"cart", "medal"}, {"cart", "named"}, {"fdha", "gdju"}, {"unrelated1", "unrelated2"}} {
		if member[pair[0]] == member[pair[1]] {
			t.Fatal("unsafe root fold", pair)
		}
	}
	if b, _ := arcadeBundleGamesWithHardware(games, false, hw, r); len(b) != len(games) {
		t.Fatal("no-fold ignored")
	}
}

func TestArcadeVariantRevisionsAndQualifierBoundaries(t *testing.T) {
	for _, q := range []string{"SV Version", "TV Version", "CV Version", "uncensored", "medal", "older hardware"} {
		if arcadeTechnicalQualifier(q) {
			t.Fatal("content/hardware qualifier erased", q)
		}
	}
	for _, q := range []string{"World, GNB2/VER.A alt", "Italy, set 2, checksum 629B", "FD1094 317-0181a", "World / USA + Canada / Korea / Hong Kong / Taiwan"} {
		if !arcadeTechnicalQualifier(q) {
			t.Fatal("technical qualifier retained", q)
		}
	}
	for _, tc := range []struct{ title, want string }{{"Game (World, uncensored)", "Game (uncensored)"}, {"Game (Japan 900802, medal)", "Game (medal)"}, {"Game (version 1.0, checksum ABCDEF, older hardware)", "Game (older hardware)"}} {
		if got := arcadeCardTitle(datGame{Description: tc.title}); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	g := []datGame{
		{Name: "base", Description: "Original", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu"}},
		{Name: "a", CloneOf: "base", Description: "Derivative (set 1)", Manufacturer: "hack (Maker)", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu"}},
		{Name: "b", CloneOf: "base", Description: "Derivative (set 2)", Manufacturer: "hack (Maker)", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu"}},
		{Name: "c", CloneOf: "base", Description: "Original (rev 1, hack)", Manufacturer: "hack", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu"}},
		{Name: "d", CloneOf: "base", Description: "Original (rev 2, hack)", Manufacturer: "hack", SourceFile: "driver.cpp", DeviceRefs: []string{"cpu"}},
	}
	b, _ := arcadeBundleGames(g, true)
	if len(b) != 4 {
		t.Fatal("derivative revisions or unnamed patches incorrectly grouped", b)
	}
	for _, bundle := range b {
		if bundle.Title == "Derivative" && len(bundle.Releases) != 2 {
			t.Fatal(bundle)
		}
	}
}
