package dat

import (
	"fmt"
	"path/filepath"
	"rommeta/internal/catalog"
	"strings"
	"testing"
)

func TestArcadeImportBranchesAcrossFamiliesAndEditions(t *testing.T) {
	rows := []struct{ name, parent, source, title string }{
		{"mk", "", "williams/midtunit.cpp", "Mortal Kombat (rev 5.0 T-Unit 03/19/93)"},
		{"mkla1", "mk", "williams/midyunit.cpp", "Mortal Kombat (rev 1.0 08/09/92)"},
		{"mkla4", "mk", "williams/midyunit.cpp", "Mortal Kombat (rev 4.0 09/28/92)"},
		{"mkprot9", "mk", "williams/midyunit.cpp", "Mortal Kombat (prototype, rev 9.0 07/28/92)"},
		{"megaman", "", "capcom/cps1.cpp", "Mega Man: The Power Battle (CPS1, USA 951006)"},
		{"mmancp2u", "megaman", "capcom/cps2.cpp", "Mega Man: The Power Battle (CPS2, USA 951006, SAMPLE Version)"},
		{"mmancp2ur2", "megaman", "capcom/cps2.cpp", "Mega Man: The Power Battle (CPS2, USA 950925, SAMPLE Version)"},
		{"ssf2", "", "capcom/cps2.cpp", "Super Street Fighter II: The New Challengers (World 931005)"},
		{"ssf2tb", "ssf2", "capcom/cps2.cpp", "Super Street Fighter II: The Tournament Battle (World 931119)"},
		{"ssf2tbh", "ssf2", "capcom/cps2.cpp", "Super Street Fighter II: The Tournament Battle (Hispanic 931005)"},
		{"ssf2tbd", "ssf2", "capcom/cps2.cpp", "Super Street Fighter II: The Tournament Battle (World 931119 Phoenix Edition) (bootleg)"},
		{"ssf2t", "", "capcom/cps2.cpp", "Super Street Fighter II Turbo (World 940223)"},
		{"ssf2xj", "ssf2t", "capcom/cps2.cpp", "Super Street Fighter II X: Grand Master Challenge (Japan 940311)"},
		{"ssf2xjr1r", "ssf2t", "capcom/cps2.cpp", "Super Street Fighter II X: Grand Master Challenge (Japan 940223 rent version)"},
		{"ssf2xjr1d", "ssf2t", "capcom/cps2.cpp", "Super Street Fighter II X: Grand Master Challenge (Japan 940223 Phoenix Edition) (bootleg)"},
		{"tetris", "", "sega/segas16a.cpp", "Tetris (set 4, Japan, System 16A)"},
		{"tetrist", "tetris", "taito/taito_b.cpp", "Tetris (Japan, Taito B-System, Nastar Conversion Kit)"},
		{"tetrista", "tetris", "taito/taito_b.cpp", "Tetris (Japan, Taito B-System, Master of Weapon Conversion Kit)"},
	}
	dir := t.TempDir()
	var xml, names strings.Builder
	xml.WriteString(`<mame build="0.289 (fixture)"><machine name="home"><description>Home computer</description></machine>`)
	for _, r := range rows {
		fmt.Fprintf(&xml, `<machine name="%s" cloneof="%s" sourcefile="%s"><description>%s</description><rom name="%s.bin" size="1" crc="12345678"/></machine>`, r.name, r.parent, r.source, r.title, r.name)
		fmt.Fprintln(&names, r.name)
	}
	xml.WriteString(`</mame>`)
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "classes.zip")
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml.String()})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\n" + names.String(),
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Fighter / Versus]\n" + names.String() + "[Computer / Home]\nhome\n",
	})
	opts := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: t.TempDir(), Apply: true}, ClassificationPath: classes, Fold: true}
	r, err := ImportArcade(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["games"] != 9 {
		t.Fatal(r.Counts, r.Bindings)
	}
	for _, group := range [][]string{{"mkla1", "mkla4", "mkprot9"}, {"mmancp2u", "mmancp2ur2"}, {"ssf2tb", "ssf2tbh", "ssf2tbd"}, {"ssf2t", "ssf2xj", "ssf2xjr1r", "ssf2xjr1d"}, {"tetrist", "tetrista"}} {
		for _, name := range group {
			if r.Bindings[name].Game != r.Bindings[group[0]].Game {
				t.Fatal("unfolded branch", group, r.Bindings)
			}
		}
	}
	for _, pair := range [][2]string{{"mk", "mkla1"}, {"megaman", "mmancp2u"}, {"ssf2", "ssf2tb"}, {"ssf2tb", "ssf2t"}, {"tetris", "tetrist"}} {
		if r.Bindings[pair[0]].Game == r.Bindings[pair[1]].Game {
			t.Fatal("crossed boundary", pair)
		}
	}
	if r.Bindings["mk"].Game != "Mortal Kombat (Midway - T-Unit)" || r.Bindings["mmancp2u"].Game != "Mega Man - The Power Battle (Capcom - CPS-2)" {
		t.Fatal(r.Bindings)
	}
	if issues, err := catalog.Validate(opts.DataRoot); err != nil || len(issues) > 0 {
		t.Fatal(err, issues)
	}
	opts.DataRoot = t.TempDir()
	opts.Sets = []string{"mkprot9", "ssf2tbd", "mmancp2ur2"}
	sub, err := ImportArcade(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range sub.Bindings {
		if b != r.Bindings[name] {
			t.Fatal("subset drift", name, b, r.Bindings[name])
		}
	}
}

func TestArcadeReviewedIndependentRoots(t *testing.T) {
	g := []datGame{
		{Name: "initd", SourceFile: "sega/naomi.cpp", Description: "Initial D Arcade Stage (Japan, Rev B) (GDS-0020B)"},
		{Name: "initdexp", SourceFile: "sega/naomi.cpp", Description: "Initial D Arcade Stage (Export, Rev A) (GDS-0025A)"},
		{Name: "initdexpo", CloneOf: "initdexp", SourceFile: "sega/naomi.cpp", Description: "Initial D Arcade Stage (Export) (GDS-0025)"},
		{Name: "other", SourceFile: "sega/naomi.cpp", Description: "Initial D Arcade Stage (GDS-0025A)"},
		{Name: "jdredd", SourceFile: "sony/zn.cpp", Description: "Judge Dredd"},
		{Name: "jdreddp", SourceFile: "williams/midtunit.cpp", Description: "Judge Dredd (location test)"},
	}
	b, _ := arcadeBundleGames(g, true)
	if len(b) != 4 {
		t.Fatal(b)
	}
	g[1].SourceFile = "other.cpp"
	b, _ = arcadeBundleGames(g, true)
	if len(b) != 6 {
		t.Fatal("review overrode driver", b)
	}
	if g[1].CloneOf != "" {
		t.Fatal("source cloneof changed")
	}
}
