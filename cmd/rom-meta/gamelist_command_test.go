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
