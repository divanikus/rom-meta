package exporter

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogyaml"
)

func TestRegionalExportPreservesInputLabelsWithoutChangingProof(t *testing.T) {
	options, card, payload := fixture(t)
	release := "Probotector (Europe) (En,Fr)"
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Contra\nreleases:\n  "+release+": {title: Probotector, regions: [Europe], languages: [en, fr]}\n"))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: %q\n    file: {name: original.z64, size: %d, sha1: '%x'}\n", release, len(payload), sha1.Sum(payload))))
	name := "My ROM [T+Rus] [opaque&tag].z64"
	put(t, filepath.Join(options.ROMRoot, name), payload)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Records[0]
	if got := r.Fields["title"].Value; got != "Probotector [T+Rus] [opaque&tag]" {
		t.Fatalf("regional title/opaque labels: %q", got)
	}
	if r.Release != release || r.MatchedBy != "hash" || r.ROM != name {
		t.Fatalf("display policy changed input identity: %+v", r)
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	xml, err := os.ReadFile(filepath.Join(options.Out, "gamelists", "n64", "gamelist.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(xml), "Probotector [T+Rus] [opaque&amp;tag]") {
		t.Fatal("title did not reach XML safely", string(xml))
	}
}

func TestTitleProfileValidation(t *testing.T) {
	for _, body := range []string{
		"titles: {mode: regional, labels: keep, variants: auto}\n",
		"titles: {mode: release, labels: drop, variants: none}\n",
		"titles: {mode: filename, variants: filename}\n",
		"titles: {variants: release}\n",
	} {
		if err := ValidateExportProfile("schema_version: 1\ntarget: es-de\n" + body); err != nil {
			t.Errorf("valid title policy rejected: %s: %v", body, err)
		}
	}
	for _, body := range []string{
		"title_mode: catalog\n", "titles: {mode: catalog}\n", "titles: {labels: guess}\n", "titles: {variants: sometimes}\n", "titles: {template: '{title}'}\n",
	} {
		if err := ValidateExportProfile("schema_version: 1\ntarget: es-de\n" + body); err == nil {
			t.Errorf("invalid policy accepted: %s", body)
		}
	}
}

func titleRecord(t *testing.T, game, release, rom, declared string) Record {
	t.Helper()
	body := fmt.Sprintf("title: Wrong global name\nreleases:\n  %q:\n    authority: {entry_name: %q}\n", release, release)
	if declared != "" {
		body += fmt.Sprintf("    title: %q\n", declared)
	}
	doc, err := catalogyaml.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return Record{ROM: rom, Game: "platforms/Test/" + game, Release: release, MetadataRelease: release, Status: "matched", MatchedBy: "hash", TitleContext: releaseTitleContext(doc.Root(), release), Fields: map[string]Value{"title": {Value: "Wrong global name", Source: "catalog"}}}
}
func renderedTitles(t *testing.T, p TitlePolicy, records ...Record) map[string]string {
	t.Helper()
	plan := &Plan{Profile: Profile{Titles: p}, Records: records}
	if err := FinalizeExport(plan); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range plan.Records {
		out[r.ROM] = r.Fields["title"].Value
	}
	return out
}

func TestTitlePolicyRegionalVariants(t *testing.T) {
	cases := []struct {
		name string
		rows []Record
		want map[string]string
	}{
		{"regional aliases", []Record{titleRecord(t, "contra", "Contra (USA)", "a.nes", ""), titleRecord(t, "contra", "Probotector (Europe)", "b.nes", ""), titleRecord(t, "contra", "Gryzor (Japan)", "c.nes", "")}, map[string]string{"a.nes": "Contra (USA)", "b.nes": "Probotector (Europe)", "c.nes": "Gryzor (Japan)"}},
		{"revision and language", []Record{titleRecord(t, "game", "Game (USA) (En) (Rev A)", "a.bin", ""), titleRecord(t, "game", "Game (USA) (Fr) (Rev B)", "b.bin", "")}, map[string]string{"a.bin": "Game (Rev A)", "b.bin": "Game (Rev B)"}},
		{"language alone", []Record{titleRecord(t, "game", "Game (USA) (En)", "a.bin", ""), titleRecord(t, "game", "Game (USA) (Fr)", "b.bin", "")}, map[string]string{"a.bin": "Game (En)", "b.bin": "Game (Fr)"}},
		{"opaque labels already distinguish", []Record{titleRecord(t, "game", "Game (USA)", "a [T+Rus].bin", ""), titleRecord(t, "game", "Game (USA)", "b [unknown].bin", "")}, map[string]string{"a [T+Rus].bin": "Game [T+Rus]", "b [unknown].bin": "Game [unknown]"}},
		{"meaningful parentheses", []Record{titleRecord(t, "game", "Game (The Movie) (USA) (Beta)", "a.bin", "Game")}, map[string]string{"a.bin": "Game (The Movie) (Beta)"}},
		{"explicit release title", []Record{titleRecord(t, "game", "Authority Name (USA) (Rev A)", "a.bin", "My regional title"), titleRecord(t, "game", "Authority Name (USA) (Rev B)", "b.bin", "My regional title")}, map[string]string{"a.bin": "My regional title (Rev A)", "b.bin": "My regional title (Rev B)"}},
		{"MAME human description", []Record{titleRecord(t, "metal", "mslugj", "mslugj.zip", "Metal Slug (Japan)")}, map[string]string{"mslugj.zip": "Metal Slug"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderedTitles(t, TitlePolicy{}, tc.rows...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("titles: got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestTitlePolicyPresetsAndLiteralLabels(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    TitlePolicy
		want string
	}{
		{"regional", TitlePolicy{}, "Game (Special Edition) [T+Rus] [X  Y]"},
		{"release", TitlePolicy{Mode: "release", Variants: "none"}, "Game (USA) (En,Fr) (Rev A) (Special Edition) [T+Rus] [X  Y]"},
		{"filename", TitlePolicy{Mode: "filename"}, "My [T+Rus] ROM [X  Y]"},
		{"drop regional labels", TitlePolicy{Labels: "drop"}, "Game (Special Edition)"},
		{"drop filename labels", TitlePolicy{Mode: "filename", Labels: "drop"}, "My ROM"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := titleRecord(t, "game", "Game (USA) (En,Fr) (Rev A) (Special Edition)", "My [T+Rus] ROM [X  Y].bin", "")
			got := renderedTitles(t, tc.p, r)[r.ROM]
			if got != tc.want {
				t.Fatalf("%q want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		input, base string
		labels      []string
	}{
		{"Game [one] [one]", "Game", []string{"[one]", "[one]"}},
		{"Game [outer[inner]]", "Game", []string{"[outer[inner]]"}},
		{"Game [unterminated", "Game [unterminated", nil},
		{"Game [  unknown + text  ]", "Game", []string{"[  unknown + text  ]"}},
	} {
		base, labels := splitTitleLabels(tc.input)
		if base != tc.base || !reflect.DeepEqual(labels, tc.labels) {
			t.Fatalf("label extraction %q: %q %#v", tc.input, base, labels)
		}
	}
	r := titleRecord(t, "game", "Game [keep] (USA)", "Game [keep] [keep].bin", "")
	if got := renderedTitles(t, TitlePolicy{}, r)[r.ROM]; got != "Game [keep] [keep]" {
		t.Fatal("input labels lost or metadata duplicated them", got)
	}
}

func TestTitleVariantsPresetsAndFallback(t *testing.T) {
	for _, tc := range []struct {
		policy       string
		wantA, wantB string
	}{
		{"none", "Game", "Game"},
		{"auto", "Game (USA)", "Game (Europe)"},
		{"release", "Game (USA) (En)", "Game (Europe) (En,Fr)"},
		{"filename", "my-us", "my-eu"},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			a := titleRecord(t, "game", "Game (USA) (En)", "my-us.bin", "")
			b := titleRecord(t, "game", "Game (Europe) (En,Fr)", "my-eu.bin", "")
			got := renderedTitles(t, TitlePolicy{Variants: tc.policy}, a, b)
			if got[a.ROM] != tc.wantA || got[b.ROM] != tc.wantB {
				t.Fatalf("%#v", got)
			}
		})
	}
	a := titleRecord(t, "game", "Game (USA)", "first/Game.bin", "")
	b := titleRecord(t, "game", "Game (USA)", "second/Game.bin", "")
	got := renderedTitles(t, TitlePolicy{}, a, b)
	if got[a.ROM] != "first/Game" || got[b.ROM] != "second/Game" {
		t.Fatal("relative path fallback", got)
	}
	unknown := Record{ROM: "Gryzor (Japan) [T+Rus].nes", Game: a.Game, MetadataRelease: "Contra (USA)", Status: "matched", MatchedBy: "game-name", Fields: map[string]Value{"title": {Value: "Contra"}}}
	if got := renderedTitles(t, TitlePolicy{}, unknown)[unknown.ROM]; got != "Gryzor (Japan) [T+Rus]" {
		t.Fatal("default release used for unknown edition", got)
	}
}

func TestTitleFinalizationIsBatchIndependentAndPreservesIdentity(t *testing.T) {
	a := titleRecord(t, "game", "Game (USA)", "a.bin", "")
	b := titleRecord(t, "game", "Game (Europe)", "b.bin", "")
	first := &Plan{Records: []Record{a}}
	if err := FinalizeExport(first); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(first.Records)
	var wire []Record
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	want := renderedTitles(t, TitlePolicy{}, a, b)
	if got := renderedTitles(t, TitlePolicy{}, append(wire, b)...); !reflect.DeepEqual(got, want) {
		t.Fatal("batch changed titles", got, want)
	}
	if got := renderedTitles(t, TitlePolicy{}, b, a); !reflect.DeepEqual(got, want) {
		t.Fatal("input order changed titles", got, want)
	}
	if a.Game != "platforms/Test/game" || a.Release != "Game (USA)" || a.MatchedBy != "hash" {
		t.Fatal("display altered binding", a)
	}
	hidden := titleRecord(t, "game", "Game (Europe)", "hidden.bin", "")
	hidden.Hidden = true
	if got := renderedTitles(t, TitlePolicy{}, a, hidden)[a.ROM]; got != "Game" {
		t.Fatal("hidden input counted as another visible version", got)
	}
}

func TestTitlePlaylistIsOneVisibleEntry(t *testing.T) {
	r := titleRecord(t, "game", "Disc Game (USA) (Disc 1)", "Disc Game [custom].m3u", "")
	r.Representation = "playlist-directory"
	r.Release = ""
	r.Playlist = []PlaylistInput{{ROM: "disc1.chd"}, {ROM: "disc2.chd"}}
	if got := renderedTitles(t, TitlePolicy{}, r)[r.ROM]; got != "Disc Game [custom]" {
		t.Fatal("playlist title contains first disc number", got)
	}
	if got := renderedTitles(t, TitlePolicy{Mode: "release", Variants: "none"}, r)[r.ROM]; got != "Disc Game (USA) [custom]" {
		t.Fatal("full playlist title contains first disc", got)
	}
}

func TestTitleMAMEUsesSetDescriptionForBothDisplayModes(t *testing.T) {
	doc, err := catalogyaml.Parse([]byte(`title: Metal Slug world family
releases:
  mslugj:
    title: Metal Slug - Super Vehicle-001 (Japan)
    authority: {source: mame, entry_name: mslugj}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := Record{ROM: "mslugj.zip", Game: "metal", Release: "mslugj", Status: "matched", TitleContext: releaseTitleContext(doc.Root(), "mslugj")}
	for mode, want := range map[string]string{"regional": "Metal Slug - Super Vehicle-001", "release": "Metal Slug - Super Vehicle-001 (Japan)", "filename": "mslugj"} {
		if got := renderedTitles(t, TitlePolicy{Mode: mode}, r)[r.ROM]; got != want {
			t.Fatalf("%s: %q want %q", mode, got, want)
		}
	}
}
