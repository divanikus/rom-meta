package main

import "testing"

func TestGamelistOptions(t *testing.T) {
	o, report, help, err := parseGamelistOptions([]string{"pack.zip", "--collection", "sample", "--language=ru", "--member", "n64/gamelist.xml", "--report", "report.json", "--apply"})
	if err != nil || help || !o.Apply || o.Collection != "sample" || o.Language != "ru" || report != "report.json" {
		t.Fatalf("%+v %s %v %v", o, report, help, err)
	}
	for _, args := range [][]string{{"pack.zip"}, {"--collection"}, {"pack.zip", "other.zip", "--collection", "sample", "--language", "ru"}} {
		if _, _, _, err := parseGamelistOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestGamelistHashAndRoleOptions(t *testing.T) {
	o, _, _, err := parseGamelistOptions([]string{"input.xml", "--collection", "sample", "--language", "en", "--match-md5", "--media-role", "thumbnail=screenshot", "--media-role=image=ignore"})
	if err != nil || !o.MatchMD5 || o.MediaRoles["thumbnail"] != "screenshot" || o.MediaRoles["image"] != "ignore" {
		t.Fatal(o, err)
	}
	for _, args := range [][]string{{"--media-role", "image"}, {"--media-role", "image=ignore", "--media-role", "image=screenshot"}} {
		if _, _, _, err := parseGamelistOptions(args); err == nil {
			t.Fatal(args)
		}
	}
}

func TestGamelistNESROMRootOption(t *testing.T) {
	o, _, _, err := parseGamelistOptions([]string{"input.xml", "--collection", "sample", "--language", "en", "--match-md5", "--nes-rom-root", "existing-roms"})
	if err != nil || !o.MatchMD5 || o.NESROMRoot != "existing-roms" {
		t.Fatal(o, err)
	}
}
