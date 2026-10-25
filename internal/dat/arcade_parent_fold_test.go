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

func TestArcadeParentIdentityAndExceptions(t *testing.T) {
	for _, tc := range []struct {
		name, parent, clone string
		separate            bool
	}{
		{"regional rename", "Puck Man (Japan)", "Pac-Man (USA)", false},
		{"board and controls", "Action Fighter (FD1089A 317-0018)", "Action Fighter (System 16B, unprotected, analog controls)", false},
		{"version and hardware", "Adders and Ladders (v2.1) (MPU4 Video)", "Adders and Ladders (v2.0) (MPU4 Video)", false},
		{"bootleg", "Game (World)", "Game (Japan, bootleg)", false},
		{"renamed bootleg", "Game", "Different Game (bootleg)", true},
		{"bootleg with content changes", "Game", "Game (bootleg, speed-up)", true},
		{"hack", "Game (World)", "Other Title (hack)", true},
		{"regional beta", "Dragon's Lair (US rev. F2)", "Dragon's Lair (US Beta 1, Pioneer PR-7820)", false},
		{"evaluation", "Raiden Fighters (World)", "Raiden Fighters (Evaluation Software For Show, Japan)", false},
		{"prototype", "Game", "Game (Japan, prototype)", false},
		{"test version", "Game", "Game (Italy, test version)", false},
		{"letter revision", "Game (version I)", "Game (version X)", false},
		{"named version", "Master Boy (Spain)", "Master Boy Version II (Spain)", true},
		{"olympic", "Master Boy (Spain)", "Master Boy Olympic (Spain)", true},
		{"demo", "Game", "Game (US demo)", false},
		{"location test", "Game", "Game (Japan, location test)", false},
		{"edition", "Donkey Kong (US set 1)", "Donkey Kong (Pauline Edition)", true},
		{"same edition", "Game (World, Black Edition)", "Game (Japan, Black Edition)", false},
		{"cabinet", "Game (deluxe, Rev A)", "Game (upright)", false},
		{"named deluxe edition", "Game", "Game Deluxe", true},
		{"protection hack", "Game (World)", "Game (Japan, no protection hack)", false},
		{"kit", "Donkey Kong (US set 1)", "Donkey Kong (hard kit)", true},
		{"questions", "Trivia (Questions Series 8)", "Trivia (Questions Series 10)", true},
		{"alternate questions", "Trivia", "Trivia (alt questions)", true},
		{"beta in actual title", "Beta Mission (World)", "Beta Mission (Japan)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			games := []datGame{
				{Name: "parent", Description: tc.parent, SourceFile: "driver.cpp"},
				{Name: "clone", Description: tc.clone, SourceFile: "driver.cpp", CloneOf: "parent"},
			}
			bundles, notes := arcadeBundleGames(games, true)
			if (len(bundles) == 2) != tc.separate {
				t.Fatalf("bundles=%+v notes=%v", bundles, notes)
			}
			if tc.separate && notes["clone"] == "" {
				t.Fatal("missing review reason")
			}
			if !tc.separate && (len(bundles) != 1 || len(bundles[0].Releases) != 2) {
				t.Fatal(bundles)
			}
		})
	}
	games := []datGame{
		{Name: "parent", Description: "Puck Man (Japan)", SourceFile: "pacman.cpp"},
		{Name: "clone", Description: "Pac-Man (USA)", SourceFile: "pacman.cpp", CloneOf: "parent"},
		{Name: "child", Description: "Pac-Man (USA, set 2)", SourceFile: "pacman.cpp", CloneOf: "clone"},
		{Name: "other", Description: "Another Game", SourceFile: "pacman.cpp", ROMOf: "parent"},
	}
	groups, _ := arcadeBundleGames(games, true)
	if len(groups) != 2 {
		t.Fatal("driver/romof used as game identity", groups)
	}
	for _, g := range groups {
		if g.Title == "Puck Man" && len(g.Releases) != 3 {
			t.Fatal("clone ancestry lost", g)
		}
	}
	groups, _ = arcadeBundleGames(games, false)
	if len(groups) != 4 {
		t.Fatal("no-fold ignored", groups)
	}
}

func TestArcadeTechnicalDisplayTitles(t *testing.T) {
	for input, want := range map[string]string{
		"Action Fighter (FD1089A 317-0018)":                                 "Action Fighter",
		"Action Fighter (System 16B, FD1089B 317-unknown, analog controls)": "Action Fighter",
		"Adders and Ladders (v2.1) (MPU4 Video)":                            "Adders and Ladders",
		"Game (v2.1) (unrecognized cabinet)":                                "Game (unrecognized cabinet)",
		"Game (World, Black Edition)":                                       "Game (World, Black Edition)",
		"Trivia (Questions Series 8)":                                       "Trivia (Questions Series 8)",
		"Game (deluxe, Rev A)":                                              "Game",
		"Game (standard)":                                                   "Game",
		"Game (upright)":                                                    "Game",
		"Game (NGM-007 ~ NGH-007)":                                          "Game",
		"Game (Revenge)":                                                    "Game (Revenge)",
		"Game (strange suffix) (v1.0)":                                      "Game (strange suffix)",
	} {
		if got := arcadeBaseTitle(input); got != want {
			t.Errorf("%q -> %q, want %q", input, got, want)
		}
	}
}

func TestArcadeParentFoldImportPreservesSetsAndBindings(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	input, classes := filepath.Join(dir, "mame.zip"), filepath.Join(dir, "category.zip")
	var xml, names strings.Builder
	xml.WriteString(`<mame build="0.289 (fixture)">`)
	add := func(name, parent, source, title string) {
		fmt.Fprintf(&xml, `<machine name="%s" cloneof="%s" sourcefile="%s"><description>%s</description><rom name="%s.bin" size="1" crc="12345678"/></machine>`, name, parent, source, title, name)
		fmt.Fprintln(&names, name)
	}
	add("afighter", "", "sega/segas16a.cpp", "Action Fighter (FD1089A 317-0018)")
	for i, q := range []string{"unprotected", "unprotected, analog controls", "FD1089B 317-unknown", "FD1089B 317-unknown, analog controls", "System 16B, unprotected, analog controls", "System 16B, FD1089B 317-unknown, analog controls", "System 16B, FD1089B 317-unknown", "System 16B, FD1089A 317-0018"} {
		source := "sega/segas16a.cpp"
		if strings.HasPrefix(q, "System") {
			source = "sega/segas16b.cpp"
		}
		add(fmt.Sprintf("afighter%c", 'a'+i), "afighter", source, "Action Fighter ("+q+")")
	}
	add("v4addlad", "", "barcrest/mpu4vid.cpp", "Adders and Ladders (v2.1) (MPU4 Video)")
	add("v4addlad20", "v4addlad", "barcrest/mpu4vid.cpp", "Adders and Ladders (v2.0) (MPU4 Video)")
	add("puckman", "", "pacman/pacman.cpp", "Puck Man (Japan)")
	add("pacman", "puckman", "pacman/pacman.cpp", "Pac-Man (USA)")
	xml.WriteString(`<machine name="home"><description>Home computer</description></machine></mame>`)
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml.String()})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\n" + names.String(),
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\nhome\n",
		"category.ini": ";; CATEGORY.ini 0.289\n[Puzzle / Misc.]\n" + names.String() + "[Computer / Home]\nhome\n",
	})
	options := ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true}
	r, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["games"] != 3 || len(r.Bindings) != 13 {
		t.Fatal(r.Counts)
	}
	if r.Bindings["afighter"].Game != "Action Fighter" || r.Bindings["afighterh"].Game != "Action Fighter" || r.Bindings["v4addlad20"].Game != "Adders and Ladders" || r.Bindings["pacman"].Game != r.Bindings["puckman"].Game {
		t.Fatal(r.Bindings)
	}
	for _, group := range []struct {
		set      string
		releases int
	}{{"afighter", 9}, {"v4addlad", 2}, {"puckman", 2}} {
		b := r.Bindings[group.set]
		data, err := os.ReadFile(filepath.Join(root, "platforms", b.Platform, b.Game, "game.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			Releases map[string]struct {
				Title string
				MAME  struct {
					CloneOf  string `yaml:"cloneof"`
					Hardware string
				}
			}
		}
		if err = yaml.Unmarshal(data, &card); err != nil {
			t.Fatal(err)
		}
		if len(card.Releases) != group.releases {
			t.Fatal(card)
		}
		if group.set == "afighter" && (card.Releases["afighterh"].MAME.Hardware == card.Releases["afighter"].MAME.Hardware || !strings.Contains(card.Releases["afighterh"].Title, "System 16B")) {
			t.Fatal("release/board detail lost", card)
		}
	}
	for _, file := range []string{"catalog.yaml", "catalog-planned.yaml"} {
		saved, err := readArcadeBindings(filepath.Join(root, "bindings", "romsets", "mame", r.Snapshot, file))
		if err != nil {
			t.Fatal(err)
		}
		for name, b := range r.Bindings {
			if saved[name] != b {
				t.Fatal(file, name)
			}
		}
	}
	repeat, err := ImportArcade(input, options)
	if err != nil || repeat.Counts["games"] != 3 || len(repeat.Conflicts) != 0 {
		t.Fatalf("repeat: %v %+v", err, repeat)
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validate: %v %v", err, issues)
	}
	options.DataRoot = t.TempDir()
	options.Sets = []string{"afighterh", "v4addlad20", "pacman"}
	subset, err := ImportArcade(input, options)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range subset.Bindings {
		if b != r.Bindings[name] {
			t.Fatal("subset identity drift", name, b)
		}
	}
}

func TestArcadeReviewedContentBoundary(t *testing.T) {
	games := []datGame{
		{Name: "puckman", Description: "Puck Man (Japan)", SourceFile: "pacman/pacman.cpp"},
		{Name: "pacman", Description: "Pac-Man (USA)", SourceFile: "pacman/pacman.cpp", CloneOf: "puckman"},
		{Name: "piranha", Description: "Piranha", SourceFile: "pacman/pacman.cpp", CloneOf: "puckman"},
		{Name: "piranhao", Description: "Piranha (older)", SourceFile: "pacman/pacman.cpp", CloneOf: "puckman"},
		{Name: "abscam", Description: "Abscam", SourceFile: "pacman/pacman.cpp", CloneOf: "puckman"},
	}
	bundles, notes := arcadeBundleGames(games, true)
	if len(bundles) != 4 || notes["pacman"] != "" {
		t.Fatalf("bundles=%+v notes=%v", bundles, notes)
	}
	for _, name := range []string{"piranha", "piranhao", "abscam"} {
		if notes[name] != "reviewed content variant kept separate" {
			t.Fatal(notes)
		}
	}
	if arcadeReviewedContentVariant(datGame{Name: "piranha", CloneOf: "puckman", SourceFile: "other.cpp"}) {
		t.Fatal("exception not scoped to driver")
	}
}
