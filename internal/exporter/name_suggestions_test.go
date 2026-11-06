package exporter

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestTranslationNameSuggestionsAreNotBindings(t *testing.T) {
	root := t.TempDir()
	one := &game{path: filepath.Join(root, "platforms", "pcengine", "Sky Quest"), name: "Sky Quest"}
	two := &game{path: filepath.Join(root, "platforms", "pcengine", "Other Quest"), name: "Other Quest"}
	index := &catalogIndex{}
	index.addExportName("Sky Quest (Japan)", one, true)
	index.addExportName("Quest of Sky", one, false)
	index.addExportName("Other Quest", two, true)
	index.addExportName("Sky Quest", two, false) // Broad provider alias cannot outrank a canonical title.
	for _, tc := range []struct {
		name, member string
		want         int
	}{
		{"translations/en/Sky Quest (Japan) [T-En by Team v1.0].zip", "Sky Quest (Japan).pce", 1},
		{"Sky Quest (Japan) [T+Rus].pce", "", 1},
		{"translations/en/Quest of Sky (Japan).pce", "", 1},
		{"translations/en/Sky Quest.pce", "Other Quest.pce", 2},
		{"translations/en/Sky Quest II.pce", "", 0},
		{"translations/en/Sky.pce", "", 0},
		{"Sky Quest.pce", "", 0},
		{"support/docs/Collection [T-En] Docs.zip", "Sky Quest (Japan).pce", 0},
		{"translations/en/Sky Quest.zip", "Sky Quest (Japan).md", 0},
		{"translations/en/Sky Quest (Japan).ips", "", 0},
		{"translations/en/Sky Quest.zip", "docs/Sky Quest (Japan).pce", 0},
	} {
		t.Run(tc.name+tc.member, func(t *testing.T) {
			record := Record{ROM: tc.name, Member: tc.member, Status: "unmatched", SHA1: "observed-hash"}
			suggestTranslationNames(root, index, &record)
			if len(record.Suggestions) != tc.want {
				t.Fatalf("%+v", record)
			}
			if record.Game != "" || record.Release != "" || record.Variant != "" || record.MetadataRelease != "" || record.MatchedBy != "" || len(record.Fields) != 0 || record.SHA1 != "observed-hash" {
				t.Fatal("suggestion strengthened identity", record)
			}
			if tc.want > 0 && record.Status != "ambiguous" {
				t.Fatal(record)
			}
		})
	}
}

func TestTranslationExportSuggestionsAndStrictLookup(t *testing.T) {
	o, _, original := fixture(t)
	input := "translations/en/Example (Japan) [T-En by Team v1.0].zip"
	putZip(t, filepath.Join(o.ROMRoot, filepath.FromSlash(input)), map[string][]byte{"Example (Japan).z64": []byte("synthetic modified payload")})
	// An exact prepared hash still wins, even with a translated-looking filename.
	exact := "translations/en/Renamed [T-En].z64"
	put(t, filepath.Join(o.ROMRoot, filepath.FromSlash(exact)), original)
	local, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	var proposed Record
	for _, r := range local.Records {
		if r.ROM == input {
			proposed = r
		}
		if r.ROM == exact && (r.Status != "matched" || r.MatchedBy != "hash" || len(r.Suggestions) != 0) {
			t.Fatal("exact hash weakened", r)
		}
	}
	if proposed.Status != "ambiguous" || len(proposed.Suggestions) != 1 || proposed.Game != "" {
		t.Fatalf("%+v", proposed)
	}
	catalog, err := NewLookupCatalog(o.DataRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := catalog.OpenExport(local.Platform, local.System, "")
	if err != nil {
		t.Fatal(err)
	}
	o.System = local.System
	inputs, err := ObserveExport(o)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := remote.Resolve(inputs, catalog.Profile)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, r := range exported.Records {
		if r.ROM == input {
			seen = true
			if !reflect.DeepEqual(r.Suggestions, proposed.Suggestions) || r.Status != "ambiguous" || r.Game != "" {
				t.Fatal("API export differs", r)
			}
		}
	}
	if !seen {
		t.Fatal("API export omitted translation")
	}
	strict, err := catalog.Open(local.Platform)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ObserveInput(o.ROMRoot, local.System, input)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := strict.ResolveBindings([]ObservedInput{proof}, true)
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Records[0].Status == "matched" || len(lookup.Records[0].Suggestions) != 0 {
		t.Fatal("consumer lookup relaxed", lookup.Records[0])
	}
}
