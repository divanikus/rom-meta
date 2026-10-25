package launchbox

import (
	"path/filepath"
	"testing"
)

func TestArcadeMAMENamesAndHardwareRouting(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "Metadata.zip")
	writeZipForTest(t, zipPath, map[string]string{"Mame.xml": `<LaunchBox><MameFile><FileName>sonicwi2</FileName><Name>Aero Fighters 2 / Sonic Wings 2</Name></MameFile><MameFile><FileName>boot</FileName><Name>Aero Fighters 2</Name><IsBootleg>true</IsBootleg></MameFile></LaunchBox>`})
	names, err := readMAMENames(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]map[string][]releaseMetadata{"Arcade": {}, "SNK Neo Geo MVS": {}}
	for platform, id := range map[string]string{"Arcade": "1", "SNK Neo Geo MVS": "2"} {
		for _, key := range titleMatchKeys("Aero Fighters 2") {
			index[platform][key] = []releaseMetadata{{Title: "Aero Fighters 2", DatabaseID: id, Platform: platform}}
		}
	}
	game := catalogGame{Platform: "Arcade", ArcadePlatform: "Arcade - SNK - Neo Geo", Title: "Other game"}
	release := catalogRelease{Name: "local", MAMESet: "sonicwi2", Title: "Aero Fighters 2 / Sonic Wings 2 (NGM-075)"}
	m, ok := matchArcadeMetadata(index, game, release, names)
	if !ok || m.DatabaseID != "2" {
		t.Fatalf("wrong hardware: %#v %v", m, ok)
	}
	delete(index, "SNK Neo Geo MVS")
	m, ok = matchArcadeMetadata(index, game, release, names)
	if !ok || m.DatabaseID != "1" {
		t.Fatalf("Arcade fallback: %#v %v", m, ok)
	}
	release.MAMESet = "boot"
	if _, ok := matchArcadeMetadata(index, game, release, names); ok {
		t.Fatal("bootleg borrowed original metadata")
	}
	release = catalogRelease{Name: "absent", MAMESet: "absent", Title: "Another name"}
	game.Title = "Aero Fighters 2"
	if _, ok := matchArcadeMetadata(index, game, release, names); ok {
		t.Fatal("unproven card-title inheritance")
	}
}

func TestArcadePortDoesNotBorrowOriginalMetadata(t *testing.T) {
	index := map[string][]releaseMetadata{}
	for _, m := range []releaseMetadata{{DatabaseID: "original", Title: "After Burner"}, {DatabaseID: "port", Title: "After Burner (Mega-Tech)"}} {
		for _, key := range titleMatchKeys(m.Title) {
			index[key] = append(index[key], m)
		}
	}
	for _, tc := range []struct{ set, title, id string }{
		{"aburner", "After Burner (VER 1.2, deluxe - upright)", "original"},
		{"mt_aftrb", "After Burner (Mega-Tech, SMS based)", "port"},
	} {
		m, ok := matchMetadata(index, catalogRelease{MAMESet: tc.set, Name: tc.set, Title: tc.title, MAMETitle: "After Burner"}, "After Burner")
		if !ok || m.DatabaseID != tc.id {
			t.Fatalf("%s got %#v, %v", tc.set, m, ok)
		}
	}
}
