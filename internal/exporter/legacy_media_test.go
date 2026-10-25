package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// These golden paths use only the pre-M020 contract. This file is also run
// unchanged against that revision to distinguish compatibility from self-checks.
func TestLegacyMediaSelectionGolden(t *testing.T) {
	for _, mode := range []string{"default", "all-defaults", "custom"} {
		for _, exactRelease := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/scope-%t", mode, exactRelease), func(t *testing.T) {
				options, card, payload := regionalFixture(t, exactRelease)
				if mode != "default" {
					var body strings.Builder
					body.WriteString("selection:\n  media:\n")
					for _, role := range sortedRoles() {
						if mode == "all-defaults" {
							fmt.Fprintf(&body, "    %s: {}\n", role)
						} else {
							fmt.Fprintf(&body, "    %s:\n      scope: game\n      order: [region, source, format]\n      regions: ['$release', world, unspecified, usa, japan, europe, '*']\n      sources: [screenscraper]\n", role)
						}
					}
					profile(t, &options, body.String())
				}
				put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
				plan, err := Build(options)
				if err != nil {
					t.Fatal(err)
				}
				count := len(roles)
				if mode == "default" {
					count = 4
				}
				media := plan.Records[0].Media
				if len(media) != count {
					t.Fatal("missing legacy media", media)
				}
				for role, selected := range media {
					name := roles[role].Kind + "-01.brazil.screenscraper." + roleFormat(role)
					if exactRelease {
						name = "Example (USA)/" + roles[role].Kind + "-99.usa.screenscraper." + roleFormat(role)
					}
					want, _ := filepath.Rel(options.DataRoot, filepath.Join(card, "media", filepath.FromSlash(name)))
					if selected.SourcePath != filepath.ToSlash(want) || selected.Source != "screenscraper" || selected.Size != 6 {
						t.Fatalf("%s changed legacy golden: %+v; want %s", role, selected, want)
					}
				}
			})
		}
	}
}

func regionalFixture(t *testing.T, exactRelease bool) (Options, string, []byte) {
	t.Helper()
	options, card, payload := fixture(t)
	if err := os.RemoveAll(filepath.Join(card, "media")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Example\nreleases:\n  Example (USA): {regions: [USA, Europe, Brazil]}\n"))
	for role, mapping := range roles {
		for _, suffix := range []string{"-01.brazil", "-02.usa"} {
			put(t, filepath.Join(card, "media", mapping.Kind+suffix+".screenscraper."+roleFormat(role)), []byte("legacy"))
		}
		if exactRelease {
			put(t, filepath.Join(card, "media", "Example (USA)", mapping.Kind+"-99.usa.screenscraper."+roleFormat(role)), []byte("legacy"))
		}
	}
	return options, card, payload
}
func roleFormat(role string) string {
	if role == "video" {
		return "mp4"
	}
	if role == "manual" {
		return "pdf"
	}
	return "png"
}
func sortedRoles() []string {
	var names []string
	for role := range roles {
		names = append(names, role)
	}
	sort.Strings(names)
	return names
}
