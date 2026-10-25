package dat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
)

// These cases have shared source files, BIOS or clone parents but distinct boards
// in the mame0289 hardware notes. Broad driver/parent inference gets them wrong.
func TestArcadeHardwareSharedDriverBoundaries(t *testing.T) {
	r, err := loadArcadeRouting("")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, source, romof, want string }{
		{"wboy", "sega/system1.cpp", "", "Sega - System 1"},
		{"wboysys2", "sega/system1.cpp", "wboy", "Sega - System 2"},
		{"alien3", "sega/segas32.cpp", "", "Sega - System 32"},
		{"orunners", "sega/segas32.cpp", "", "Sega - Multi 32"},
		{"fantzn2x", "sega/segas16b.cpp", "", "Sega - System 16C"},
		{"dbreed", "irem/m72.cpp", "", "Irem - M81"},
		{"dbreedm72", "irem/m72.cpp", "dbreed", "Irem - M72"},
		{"hharryu", "irem/m72.cpp", "hharry", "Irem - M84"},
		{"dkgensanm82", "irem/m72.cpp", "hharry", "Irem - M82"},
		{"poundfor", "irem/m72.cpp", "", "Irem - M85"},
		{"loht_ms", "irem/m72.cpp", "loht", "Other"},
		{"emeralda", "namco/namcona1.cpp", "", "Namco - NA-2"},
		{"emeraldaj", "namco/namcona1.cpp", "emeralda", "Namco - NA-1"},
		{"ridgerac", "namco/namcos22.cpp", "", "Namco - System 22"},
		{"propcycl", "namco/namcos22.cpp", "", "Namco - Super System 22"},
		{"rapidrvr", "namco/namcos23.cpp", "", "Namco - System 22.5"},
		{"timecrs2", "namco/namcos23.cpp", "", "Namco - System 23"},
		{"timecrs2v4a", "namco/namcos23.cpp", "timecrs2", "Namco - Super System 23"},
		{"crszone", "namco/namcos23.cpp", "", "Namco - System 23 Evolution 2"},
		{"tekken4", "namco/namcops2.cpp", "sys246", "Namco - System 246"},
		{"tekken51", "namco/namcops2.cpp", "sys256", "Namco - System 256"},
		{"timecrs4", "namco/namcops2.cpp", "sys256", "Namco - Super System 256"},
		{"sfex", "sony/zn.cpp", "coh1000c", "Capcom - ZN-1"},
		{"sfex2", "sony/zn.cpp", "coh3002c", "Capcom - ZN-2"},
		{"raystorm", "sony/zn.cpp", "coh1000t", "Taito - FX-1"},
		{"doapp", "sony/zn.cpp", "coh1002m", "Tecmo - TPS"},
		{"mtetrisc", "nintendo/aleck64.cpp", "aleck64", "Seta - E90"},
		{"11beat", "nintendo/aleck64.cpp", "aleck64", "Seta - Aleck64"},
		{"narc", "williams/midyunit.cpp", "", "Midway - Z-Unit"},
		{"smashtv", "williams/midyunit.cpp", "", "Midway - Y-Unit"},
		{"wg3dh", "williams/seattle.cpp", "", "Atari - Phoenix"},
		{"sfrush", "williams/seattle.cpp", "", "Atari - Flagstaff"},
		{"blitz", "williams/seattle.cpp", "", "Midway - Seattle"},
		{"area51", "atari/jaguar.cpp", "", "Atari - CoJag"},
		{"jaguar", "atari/jaguar.cpp", "", "Other"},
		{"raiden3", "taito/taitotx.cpp", "", "Taito - Type X"},
		{"chasehq2", "taito/taitotx.cpp", "", "Taito - Type X2"},
		{"wontmuch", "taito/taitotx.cpp", "", "Other"},
		{"ddp2", "igs/pgm.cpp", "", "IGS - PGM"},
		{"sfex", "future/different_board.cpp", "", "Other"},
		{"unknownnewset", "sega/system1.cpp", "", "Other"},
	}
	index := map[string]datGame{}
	for _, c := range cases {
		index[c.name] = datGame{Name: c.name, SourceFile: c.source, ROMOf: c.romof}
		if c.romof != "" {
			if _, ok := index[c.romof]; !ok {
				index[c.romof] = datGame{Name: c.romof}
			}
		}
	}
	for _, c := range cases {
		t.Run(c.source+"/"+c.name, func(t *testing.T) {
			g := datGame{Name: c.name, SourceFile: c.source, ROMOf: c.romof}
			got, _ := routeArcade(g, index, r)
			if got != "Arcade - "+c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
	// XML/override paths can use Windows separators; known source identity survives.
	g := datGame{Name: "wboysys2", SourceFile: `sega\system1.cpp`}
	if got, _ := routeArcade(g, index, r); got != "Arcade - Sega - System 2" {
		t.Fatal(got)
	}
}

func TestArcadeSourceSetOverrideAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routing.json")
	data := `{"version":1,"source_sets":{"sega/system1.cpp":{"wboy":"Arcade - Other"}}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := loadArcadeRouting(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := routeArcade(datGame{Name: "wboy", SourceFile: "sega/system1.cpp"}, nil, r); got != "Arcade - Other" {
		t.Fatal(got)
	}
	// Merging one override must preserve the other source-scoped decisions.
	if got, _ := routeArcade(datGame{Name: "wboysys2", SourceFile: "sega/system1.cpp"}, nil, r); got != "Arcade - Sega - System 2" {
		t.Fatal(got)
	}
	r.Sets["wboysys2"] = "Arcade - Other"
	if got, _ := routeArcade(datGame{Name: "wboysys2", SourceFile: "sega/system1.cpp"}, nil, r); got != "Arcade - Other" {
		t.Fatal("explicit override lost", got)
	}
	for _, invalid := range []string{
		`{"version":1,"source_sets":{"x.cpp":{"test":"missing"}}}`,
		`{"version":1,"source_sets":{"":{"test":"Arcade - Other"}}}`,
		`{"version":1,"source_sets":{"x.cpp":{"../escape":"Arcade - Other"}}}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadArcadeRouting(path); err == nil {
			t.Fatal("accepted invalid source_sets")
		}
	}
}

func TestArcadeHardwareSplitApplyKeepsCloneOnItsBoard(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "mame.zip")
	classes := filepath.Join(dir, "category.zip")
	xml := `<mame build="0.289 (fixture)">
 <machine name="wboy" sourcefile="sega/system1.cpp"><description>Wonder Boy (Japan)</description><rom name="one" size="1" crc="12345678"/></machine>
 <machine name="wboysys2" sourcefile="sega/system1.cpp" cloneof="wboy" romof="wboy"><description>Wonder Boy (USA)</description><rom name="two" size="1" crc="23456789"/></machine>
 <machine name="area51" sourcefile="atari/jaguar.cpp"><description>Area 51</description><rom name="three" size="1" crc="34567890"/></machine>
 <machine name="jaguar" sourcefile="atari/jaguar.cpp"><description>Jaguar</description></machine>
 </mame>`
	arcadeTestZIP(t, input, map[string]string{"mame.xml": xml})
	arcadeTestZIP(t, classes, map[string]string{
		"mame.ini":     ";; MAME.ini 0.289\n[ROOT_FOLDER]\nwboy\nwboysys2\narea51\n",
		"mess.ini":     ";; MESS.ini 0.289\n[ROOT_FOLDER]\njaguar\n",
		"category.ini": ";; Category.ini 0.289\n[Platform / Run Jump]\nwboy\nwboysys2\n[Shooter / Gun]\narea51\n[Game Console / Home]\njaguar\n",
	})
	root := filepath.Join(dir, "catalog")
	r, err := ImportArcade(input, ArcadeOptions{ImportOptions: ImportOptions{DataRoot: root, Apply: true}, ClassificationPath: classes, Fold: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["selected"] != 3 || r.Counts["games"] != 3 || r.Counts["excluded"] != 1 {
		t.Fatal(r.Counts)
	}
	if r.Bindings["wboy"].Family == r.Bindings["wboysys2"].Family || r.Bindings["wboy"].Game == r.Bindings["wboysys2"].Game {
		t.Fatal("converted clone lost hardware boundary")
	}
	if _, ok := r.Bindings["jaguar"]; ok {
		t.Fatal("home console imported from arcade driver")
	}
	stored, err := os.ReadFile(filepath.Join(root, "bindings", "romsets", "mame", r.Snapshot, "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rules ArcadeRouting
	if err = json.Unmarshal(stored, &rules); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules.SourceSets["sega/system1.cpp"]["wboysys2"], "System 2") {
		t.Fatal("scoped rules not preserved")
	}
	issues, err := catalog.Validate(root)
	if err != nil || len(issues) > 0 {
		t.Fatalf("validation: %v %v", err, issues)
	}
}

func TestArcadeScopedHardwareStillChecksDependencies(t *testing.T) {
	r := defaultArcadeRouting()
	for _, parent := range []string{"missing", "wboysys2"} {
		g := datGame{Name: "wboysys2", SourceFile: "sega/system1.cpp", ROMOf: parent}
		if got, reason := routeArcade(g, map[string]datGame{g.Name: g}, r); got != "" || reason == "" {
			t.Fatalf("invalid dependency bypassed: %s %s", got, reason)
		}
	}
}
