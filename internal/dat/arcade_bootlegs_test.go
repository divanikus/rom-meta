package dat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalog"
)

func TestArcadeBootlegIdentityBoundaries(t *testing.T) {
	parent := datGame{Name: "game", Description: "Game (World)", SourceFile: "capcom/cps2.cpp"}
	clone := datGame{Name: "gamed", Description: "Game (USA Phoenix Edition) (bootleg)", SourceFile: "capcom/cps2.cpp", CloneOf: "game", Manufacturer: "bootleg"}
	for _, tc := range []struct {
		name   string
		modify func(*datGame)
		want   bool
	}{
		{"phoenix", func(g *datGame) {}, true},
		{"unofficial copy", func(g *datGame) { g.Unofficial = true }, true},
		{"hack", func(g *datGame) { g.Description = "Game (bootleg hack)" }, false},
		{"renamed", func(g *datGame) { g.Description = "Another Game (bootleg)" }, false},
		{"prototype", func(g *datGame) { g.Description = "Game (US prototype, bootleg)" }, true},
		{"incomplete", func(g *datGame) { g.Incomplete = true }, false},
		{"speed-up", func(g *datGame) { g.Description = "Game (bootleg, speed-up)" }, false},
		{"other driver edition", func(g *datGame) { g.SourceFile = "other.cpp" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := clone
			tc.modify(&g)
			if got := arcadeSameTitleBootleg(g, parent); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
		})
	}
	if !arcadeSpecial(clone) {
		t.Fatal("bootleg lost default-release penalty/provenance")
	}
}

func TestArcadeBootlegFamilyProjectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, childTitle, childFamily, childEvidence, parentFamily string
		parentEligible                                             bool
		want                                                       string
	}{
		{"unknown board", "Game (bootleg)", "Arcade - Other", "unmapped hardware; explicit fallback collection", "Arcade - Sega - System 18", true, "Arcade - Sega - System 18"},
		{"explicit Other", "Game (bootleg)", "Arcade - Other", "explicit set rule", "Arcade - Sega - System 18", true, "Arcade - Other"},
		{"known different platform", "Game (bootleg)", "Arcade - Sega - Mega-Tech", "source file sega/megatech.cpp", "Arcade - Sega - System 18", true, "Arcade - Sega - Mega-Tech"},
		{"no eligible parent", "Game (bootleg)", "Arcade - Other", "unmapped hardware; explicit fallback collection", "Arcade - Sega - System 18", false, "Arcade - Other"},
		{"renamed", "Another Game (bootleg)", "Arcade - Other", "unmapped hardware; explicit fallback collection", "Arcade - Sega - System 18", true, "Arcade - Other"},
		{"hack", "Game (bootleg hack)", "Arcade - Other", "unmapped hardware; explicit fallback collection", "Arcade - Sega - System 18", true, "Arcade - Other"},
		{"port without bootleg", "Game (Mega-Tech)", "Arcade - Other", "unmapped hardware; explicit fallback collection", "Arcade - Sega - System 18", true, "Arcade - Other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []ArcadeEntry{{Name: "child", Decision: "eligible", Family: tc.childFamily, HardwareEvidence: tc.childEvidence}}
			families := map[string]ArcadeFamily{"child": {Platform: tc.childFamily, Kind: "collection"}}
			if tc.parentEligible {
				families["parent"] = ArcadeFamily{Platform: tc.parentFamily, Kind: "hardware"}
			}
			index := map[string]datGame{"parent": {Name: "parent", Description: "Game (World)", SourceFile: "official.cpp"}, "child": {Name: "child", CloneOf: "parent", Description: tc.childTitle, SourceFile: "bootleg.cpp"}}
			projectArcadeBootlegFamilies(entries, index, map[string]int{"child": 0}, families)
			if families["child"].Platform != tc.want || entries[0].Family != tc.want || entries[0].Hardware != "" {
				t.Fatalf("families=%v entries=%v", families, entries)
			}
		})
	}
}

func TestArcadeAlienBootlegsImport(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "category.zip")
	var xml, names strings.Builder
	xml.WriteString(`<mame build="0.289 (fixture)"><machine name="megatech" isbios="yes"><description>Mega-Tech BIOS</description></machine><machine name="home"><description>Home computer</description></machine>`)
	add := func(name, parent, romof, source, title, manufacturer string) {
		fmt.Fprintf(&xml, `<machine name="%s" cloneof="%s" romof="%s" sourcefile="%s"><description>%s</description><manufacturer>%s</manufacturer><device_ref name="fixture_board"/><rom name="%s.bin" size="1" crc="12345678"/></machine>`, name, parent, romof, source, title, manufacturer, name)
		fmt.Fprintln(&names, name)
	}
	add("astorm", "", "", "sega/segas18.cpp", "Alien Storm (World, 2 Players) (FD1094 317-0154)", "Sega")
	for _, v := range []struct{ name, title, source string }{
		{"astormbl", "Alien Storm (bootleg, set 1)", "sega/segas18_astormbl.cpp"},
		{"astormb2", "Alien Storm (bootleg, set 2)", "sega/segas18_astormbl.cpp"},
		{"astorm3d", "Alien Storm (World, 3 Players) (bootleg of FD1094 317-0148 set)", "sega/segas18.cpp"},
		{"astormjd", "Alien Storm (Japan, 2 Players) (bootleg of FD1094 317-0146 set)", "sega/segas18.cpp"},
		{"astormud", "Alien Storm (US, 3 Players) (bootleg of FD1094 317-0147 set)", "sega/segas18.cpp"},
	} {
		add(v.name, "astorm", "astorm", v.source, v.title, "bootleg")
	}
	add("mt_astrm", "", "megatech", "sega/megatech.cpp", "Alien Storm (Mega-Tech)", "Sega")
	add("aliensyn", "", "", "sega/segas16b.cpp", "Alien Syndrome (set 4, System 16B, unprotected)", "Sega")
	add("mt_asyn", "", "megatech", "sega/megatech.cpp", "Alien Syndrome (Mega-Tech, SMS based)", "Sega")
	add("avsp", "", "", "capcom/cps2.cpp", "Alien vs. Predator (Europe 940520)", "Capcom")
	add("avspd", "avsp", "avsp", "capcom/cps2.cpp", "Alien vs. Predator (Europe 940520 Phoenix Edition) (bootleg)", "bootleg")
	xml.WriteString(`</mame>`)
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml.String()})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\nmegatech\n" + names.String(),
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Fighter / 2.5D]\n" + names.String() + "[System / BIOS]\nmegatech\n[Computer / Home]\nhome\n",
	})
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true, RegionPriority: []string{"USA", "World", "Europe", "Japan"}}, ClassificationPath: classes, Fold: true}
	r, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["games"] != 6 || len(r.Bindings) != 11 {
		t.Fatal(r.Counts)
	}
	for _, n := range []string{"astormbl", "astormb2", "astorm3d", "astormjd", "astormud"} {
		want := "Alien Storm"
		if n == "astormbl" || n == "astormb2" {
			want = "Alien Storm (bootleg)"
		}
		if r.Bindings[n].Game != want || r.Bindings[n].Family != r.Bindings["astorm"].Family {
			t.Fatal(n, r.Bindings[n])
		}
	}
	if r.Bindings["astormbl"].Hardware != "" || r.Bindings["astorm"].Hardware == "" {
		t.Fatal("unmapped bootleg board falsely identified", r.Bindings)
	}
	if r.Bindings["avspd"].Game != r.Bindings["avsp"].Game || r.Bindings["mt_astrm"].Game == r.Bindings["astorm"].Game || r.Bindings["mt_asyn"].Game == r.Bindings["aliensyn"].Game {
		t.Fatal("copy/port boundary lost", r.Bindings)
	}
	for _, group := range []struct {
		name     string
		releases int
	}{{"astorm", 4}, {"avsp", 2}} {
		b := r.Bindings[group.name]
		data, err := os.ReadFile(filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			DefaultRelease string `yaml:"default_release"`
			Releases       map[string]struct {
				Title string
				MAME  struct{ Manufacturer string }
			}
		}
		if err = yaml.Unmarshal(data, &card); err != nil {
			t.Fatal(err)
		}
		if card.DefaultRelease != group.name || len(card.Releases) != group.releases {
			t.Fatalf("default/release loss: %+v", card)
		}
		if group.name == "avsp" && (!strings.Contains(card.Releases["avspd"].Title, "Phoenix Edition") || card.Releases["avspd"].MAME.Manufacturer != "bootleg") {
			t.Fatal("provenance lost", card)
		}
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validate %v %v", err, issues)
	}
	repeat, err := ImportArcade(input, options)
	if err != nil || len(repeat.Conflicts) != 0 {
		t.Fatalf("repeat %v %v", err, repeat.Conflicts)
	}
	options.DataRoot = t.TempDir()
	options.Sets = []string{"astormbl", "astormb2", "avspd"}
	subset, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	for n, b := range subset.Bindings {
		if b != r.Bindings[n] {
			t.Fatal("subset identity changed", n, b)
		}
	}
	options.DataRoot = t.TempDir()
	options.Sets = nil
	options.Fold = false
	separate, err := ImportArcade(input, options)
	if err != nil || separate.Counts["games"] != 11 {
		t.Fatalf("no-fold: %v %v", err, separate.Counts)
	}
}
