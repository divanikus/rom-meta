package exporter

import (
	"archive/zip"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataCartridgeRawAndRenamedZIPMatchExactRelease(t *testing.T) {
	for system, spec := range metadataCartridges {
		t.Run(system, func(t *testing.T) {
			root := t.TempDir()
			catalogRoot := filepath.Join(root, "catalog")
			roms := filepath.Join(root, "roms")
			platform := filepath.Join(catalogRoot, "platforms", spec.Platform)
			game := filepath.Join(platform, "Example")
			release := "Example (USA) (Beta 1)"
			payload := []byte("synthetic cartridge payload")
			put(t, filepath.Join(platform, "_platform.yaml"), []byte("name: "+spec.Platform+"\n"))
			put(t, filepath.Join(game, "game.yaml"), []byte("title: Example\ndefault_release: "+release+"\nreleases:\n  "+release+": {title: Example, regions: [USA]}\n"))
			put(t, filepath.Join(game, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: %s\n    file: {size: %d, sha1: '%x'}\n", release, len(payload), sha1.Sum(payload))))
			raw := "Renamed (USA) (Beta) (1994-08-22)" + spec.Extensions[0]
			put(t, filepath.Join(roms, raw), payload)
			file, err := os.Create(filepath.Join(roms, "Different name.zip"))
			if err != nil {
				t.Fatal(err)
			}
			archive := zip.NewWriter(file)
			member, err := archive.Create("nested/Dated prototype" + spec.Extensions[0])
			if err != nil {
				t.Fatal(err)
			}
			_, _ = member.Write(payload)
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			catalog, err := NewLookupCatalog(catalogRoot, "")
			if err != nil {
				t.Fatal(err)
			}
			cap := catalog.Platforms[0]
			if !cap.Supported || cap.System != system || len(cap.ExportSystems) == 0 || !cap.NameBinding || !LookupSystem(system) {
				t.Fatalf("incorrect metadata capability: %#v", cap)
			}
			if _, err := systemFor(spec.Platform, system); err != nil {
				t.Fatal("generic export did not cover existing cartridge metadata reader", err)
			}
			resolver, err := catalog.Open(spec.Platform)
			if err != nil {
				t.Fatal(err)
			}
			var observations []ObservedInput
			for _, name := range []string{raw, "Different name.zip"} {
				input, err := ObserveInput(roms, system, name)
				if err != nil || !ValidObservation(input) {
					t.Fatalf("input=%#v err=%v", input, err)
				}
				observations = append(observations, input)
			}
			plan, err := resolver.Resolve(observations)
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range plan.Records {
				if record.Status != "matched" || record.MatchedBy != "hash" || record.Release != release || record.SHA1 != fmt.Sprintf("%x", sha1.Sum(payload)) || record.Payload == nil {
					t.Fatalf("not an exact hash match: %#v", record)
				}
			}
			put(t, filepath.Join(roms, "Example (USA) (Beta) (1994-10-02)"+spec.Extensions[0]), []byte("unknown bytes"))
			unknown, err := ObserveInput(roms, system, "Example (USA) (Beta) (1994-10-02)"+spec.Extensions[0])
			if err != nil {
				t.Fatal(err)
			}
			unmatched, err := resolver.ResolveBindings([]ObservedInput{unknown}, true)
			if err != nil || unmatched.Records[0].Status == "matched" {
				t.Fatalf("invented a match: %#v %v", unmatched, err)
			}
		})
	}
}
