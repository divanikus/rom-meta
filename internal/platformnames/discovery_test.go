package platformnames

import (
	"errors"
	"testing"
)

func TestDiscoverProviderPlatformsWithoutEmbeddedEntry(t *testing.T) {
	r := DefaultRegistry()
	for _, tc := range []struct{ catalog, provider string }{
		{"Funtech - Super Acan", "Super A'Can"},
		{"Future Vendor - New System", "New System"},
		{"Future Vendor - New System CD", "Future Vendor New System CD"},
	} {
		got, err := r.Discover(tc.catalog, SourceIGDB, []string{"Other System", tc.provider})
		if err != nil || got != tc.provider {
			t.Fatalf("%s: %q, %v", tc.catalog, got, err)
		}
	}
}
func TestDiscoveryPreservesBoundariesAndExplicitOverrides(t *testing.T) {
	r := DefaultRegistry()
	for _, tc := range []struct {
		catalog   string
		available []string
		ambiguous bool
	}{
		{"Nintendo - Game Boy", []string{"Game Boy Color"}, false},
		{"Nintendo - Nintendo 64DD", []string{"Nintendo 64"}, false},
		{"Future - New System CD", []string{"New System"}, false},
		{"Future - New System+", []string{"New System"}, false},
		{"Future - New System", []string{"New System", "New-System"}, true},
		{"Future - New System", []string{"Future New System", "New System"}, true},
	} {
		_, err := r.Discover(tc.catalog, SourceIGDB, tc.available)
		var unresolved *ResolutionError
		if !errors.As(err, &unresolved) || unresolved.Ambiguous != tc.ambiguous {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	local, err := ParseRegistryYAML("Future - New System:\n  sources:\n    igdb: Reviewed Name\n")
	if err != nil {
		t.Fatal(err)
	}
	r.Merge(local)
	got, err := r.Discover("Future - New System", SourceIGDB, []string{"New System", "Reviewed Name"})
	if err != nil || got != "Reviewed Name" {
		t.Fatal(got, err)
	}
	if _, err := r.Discover("Future - New System", SourceIGDB, []string{"New System"}); err == nil {
		t.Fatal("overrode missing explicit identity")
	}
}
