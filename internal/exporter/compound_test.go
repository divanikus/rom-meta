package exporter

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCompoundObservationRequiresEveryMemberOfOneRelease(t *testing.T) {
	options, game, _ := fixture(t)
	a, b := []byte("first synthetic chip"), []byte("second synthetic chip")
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {size: %d, sha1: '%x'}\n  - release: Example (USA)\n    file: {size: %d, sha1: '%x'}\n", len(a), sha1.Sum(a), len(b), sha1.Sum(b))))
	putZip(t, filepath.Join(options.ROMRoot, "Composite.zip"), map[string][]byte{"a.z64": a, "b.z64": b})
	observed, err := ObserveInput(options.ROMRoot, "n64", "Composite.zip")
	if err != nil || !ValidObservation(observed) || len(observed.Payload.Components) != 1 {
		t.Fatal(observed, err)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("Nintendo - Nintendo 64")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.Resolve([]ObservedInput{observed})
	if err != nil || plan.Records[0].Status != "matched" {
		t.Fatal(plan, err)
	}
	changed := observed
	changed.Payload.Components = append([]PayloadProof(nil), observed.Payload.Components...)
	changed.Payload.Components[0].SHA1 = observed.Payload.SHA1
	plan, err = resolver.Resolve([]ObservedInput{changed})
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("unaccounted member accepted", plan, err)
	}
	// Existing offline exporters keep their explicitly single-payload behavior.
	if _, _, err = hashROM(filepath.Join(options.ROMRoot, "Composite.zip"), "n64"); err == nil {
		t.Fatal("legacy reader changed")
	}
}

func TestGameBindingOptInIsUnverifiedAndRejectsConflictingNames(t *testing.T) {
	options, game, _ := fixture(t)
	name := "Example (USA) (Storefront).z64"
	put(t, filepath.Join(options.ROMRoot, name), []byte("unknown synthetic bytes"))
	observed, err := ObserveInput(options.ROMRoot, "n64", name)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewLookupCatalog(options.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := catalog.Open("Nintendo - Nintendo 64")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.Resolve([]ObservedInput{observed})
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("default no longer exact", err)
	}
	plan, err = resolver.ResolveBindings([]ObservedInput{observed}, true)
	if err != nil || plan.Records[0].MatchedBy != "game-name" || plan.Records[0].Release != "" || plan.Records[0].MetadataRelease == "" {
		t.Fatal(plan, err)
	}
	observed.ROM = "translations/en/" + name
	plan, err = resolver.ResolveBindings([]ObservedInput{observed}, true)
	if err != nil || plan.Records[0].Status == "matched" {
		t.Fatal("deferred translation rebound", err)
	}
	other := filepath.Join(filepath.Dir(game), "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Example\nreleases: {Different: {}}\n"))
	if _, err = os.Stat(game); err != nil {
		t.Fatal(err)
	}
	resolver, err = catalog.Open("Nintendo - Nintendo 64")
	if err != nil {
		t.Fatal(err)
	}
	observed.ROM = name
	plan, err = resolver.ResolveBindings([]ObservedInput{observed}, true)
	if err != nil || plan.Records[0].Status != "ambiguous" {
		t.Fatal("ambiguous title bound", plan, err)
	}
}
