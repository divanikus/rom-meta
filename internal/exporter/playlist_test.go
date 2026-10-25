package exporter

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaylistDirectoryIsOneLaunchAndRevalidatesMembers(t *testing.T) {
	options, _, _ := fixture(t)
	a, b := []byte("first synthetic disc"), []byte("second synthetic disc")
	card := addPlatform(t, options, "psx", "Disc Game (USA) (Disc 1)", a, "Disc Game (USA) (Disc 1).chd")
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Disc Game\nreleases:\n  Disc Game (USA) (Disc 1): {regions: [USA]}\n  Disc Game (USA) (Disc 2): {regions: [USA]}\n"))
	options.Platform = "psx"
	directory := filepath.Join(options.ROMRoot, "Disc Game (USA).m3u")
	put(t, filepath.Join(directory, "Disc Game (USA).m3u"), []byte("Disc Game (USA) (Disc 1).chd\nDisc Game (USA) (Disc 2).chd\n"))
	put(t, filepath.Join(directory, "Disc Game (USA) (Disc 1).chd"), a)
	put(t, filepath.Join(directory, "Disc Game (USA) (Disc 2).chd"), b)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 1 || plan.NameMatches != 1 || len(plan.Records[0].Playlist) != 2 {
		t.Fatalf("playlist: %+v", plan)
	}
	if plan.Records[0].ROM != "Disc Game (USA).m3u" || plan.Records[0].LaunchFile != "Disc Game (USA).m3u/Disc Game (USA).m3u" {
		t.Fatal("wrong directory launch")
	}
	if !strings.HasSuffix(plan.Records[0].Media["cover"].OutputPath, "covers/Disc Game (USA).m3u.png") {
		t.Fatal("playlist media naming")
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(options.Out, "gamelists", "psx", "gamelist.xml"))
	var list xmlList
	if err := xml.Unmarshal(body, &list); err != nil || len(list.Games) != 1 || list.Games[0].Path != "./Disc Game (USA).m3u" {
		t.Fatal("wrong playlist XML")
	}
	options.Out = filepath.Join(filepath.Dir(options.Out), "changed")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(directory, "Disc Game (USA) (Disc 2).chd"), []byte("changed member"))
	if err := Apply(plan, nil); err == nil {
		t.Fatal("changed playlist member accepted")
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("failed playlist published")
	}
}

func TestPlaylistRejectsEscapeMissingAndURL(t *testing.T) {
	for _, contents := range []string{"../outside.chd\n", "missing.chd\n", "https://invalid/file.chd\n"} {
		options, _, _ := fixture(t)
		options.Platform = "psx"
		addPlatform(t, options, "psx", "Disc Game (USA)", []byte("disc"), "Disc Game (USA).chd")
		put(t, filepath.Join(options.ROMRoot, "outside.chd"), []byte("outside"))
		put(t, filepath.Join(options.ROMRoot, "Game.m3u", "Game.m3u"), []byte(contents))
		plan, err := Build(options)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, record := range plan.Records {
			if record.Representation == "playlist-directory" {
				found = true
				if record.Status != "unsupported" {
					t.Fatal("invalid playlist accepted")
				}
			}
		}
		if !found {
			t.Fatal("missing playlist record")
		}
	}
}
