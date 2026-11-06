package screenscraper

import (
	"errors"
	"rommeta/internal/platformnames"
	"testing"
)

func TestDiscoverSystemUsesProviderAliasesAndIDs(t *testing.T) {
	systems := []screenScraperSystem{
		{ID: "1", Name: "New System", Names: []string{"New System", "new-system"}},
		{ID: "2", Name: "New System CD"},
	}
	id, err := discoverSystemID("Future Vendor - New System", systems, nil)
	if err != nil || id != "1" {
		t.Fatal(id, err)
	}
	systems = append(systems, screenScraperSystem{ID: "3", Name: "New-System"})
	_, err = discoverSystemID("Future Vendor - New System", systems, nil)
	var unresolved *platformnames.ResolutionError
	if !errors.As(err, &unresolved) || !unresolved.Ambiguous {
		t.Fatal(err)
	}
	registry, err := platformnames.ParseRegistryYAML("Future Vendor - New System:\n  sources:\n    screenscraper: { id: 3 }\n")
	if err != nil {
		t.Fatal(err)
	}
	id, err = discoverSystemID("Future Vendor - New System", systems, &registry)
	if err != nil || id != "3" {
		t.Fatal(id, err)
	}
	_, err = discoverSystemID("Future Vendor - New System", systems[:2], &registry)
	if !errors.As(err, &unresolved) {
		t.Fatal("absent explicit ID accepted", err)
	}
}
