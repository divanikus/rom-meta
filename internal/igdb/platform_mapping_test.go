package igdb

import (
	"testing"

	"rommeta/internal/platformnames"
)

func TestPlatformMappingsPreserveRelatedSystemBoundaries(t *testing.T) {
	registry := platformnames.DefaultRegistry()
	for _, tc := range []struct{ platform, sourceName, other string }{
		{"Fujitsu - FM Towns series", "FM Towns", "FM-7"},
		{"Fujitsu - FM-Towns", "FM Towns", "FM-7"},
		{"Commodore - Amiga CD32", "Amiga CD32", "Amiga"},
		{"Nintendo - Nintendo 64DD", "64DD", "Nintendo 64"},
		{"Sega - Beena", "Advanced Pico Beena", "Sega Pico"},
		{"Zeebo - Zeebo", "Zeebo", "3DO Interactive Multiplayer"},
		{"Bandai - WonderSwan Color", "WonderSwan Color", "WonderSwan"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			for _, configured := range []*platformnames.Registry{nil, &registry} {
				for _, available := range [][]string{nil, {tc.other, tc.sourceName}} {
					if !platformMatches(tc.platform, []apiNamed{{Name: tc.sourceName}}, available, configured) {
						t.Fatal("rejected candidate on the exact provider platform")
					}
					if platformMatches(tc.platform, []apiNamed{{Name: tc.other}}, available, configured) {
						t.Fatal("accepted candidate on a different platform")
					}
				}
			}
		})
	}
}
