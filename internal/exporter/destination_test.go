package exporter

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"rommeta/internal/frontend"
	"rommeta/internal/frontenddata"
	"testing"
)

func decodedNative(t *testing.T, body []byte) []frontenddata.Entry {
	t.Helper()
	var doc struct {
		Entries []struct {
			XMLName xml.Name
			Fields  []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		} `xml:",any"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	result := []frontenddata.Entry{}
	for _, e := range doc.Entries {
		fields := map[string]string{}
		for _, f := range e.Fields {
			fields[f.XMLName.Local] = f.Text
		}
		result = append(result, frontenddata.Entry{Name: e.XMLName.Local, Fields: fields})
	}
	return result
}
func TestTypedDestinationMatchesPackageFieldsAndRepeats(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			options, _, payload := fixture(t)
			options.Target = target
			put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
			put(t, filepath.Join(options.ROMRoot, "two.z64"), payload)
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			library, files, err := DestinationData([]*Plan{plan})
			if err != nil {
				t.Fatal(err)
			}
			var xmlBytes []byte
			if target == "launchbox" {
				xmlBytes, err = RenderLaunchBox(plan)
			} else {
				xmlBytes, err = RenderGamelist(plan)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(library.Native[outputXMLPath(plan)], decodedNative(t, xmlBytes)) {
				t.Fatal("typed fields differ from native package contract")
			}
			source := &frontend.Source{Library: library, Files: map[string]frontend.SourceFile{}, Check: func() error { return CheckInputs(plan) }}
			for name, f := range files {
				if filepath.Ext(name) == ".xml" || name == "manifest.json" {
					t.Fatal("intermediate serialization", name)
				}
				source.Files[name] = frontend.SourceFile{Data: f.Data, Open: f.Open, Size: f.Size, SHA256: f.SHA256}
			}
			if err = Apply(plan, nil); err != nil {
				t.Fatal(err)
			}
			var changes []frontend.Change
			var native []frontenddata.Entry
			for _, direct := range []bool{false, true} {
				destination := t.TempDir()
				if target == "launchbox" {
					if err = os.MkdirAll(filepath.Join(destination, "Data", "Platforms"), 0755); err != nil {
						t.Fatal(err)
					}
				}
				opts := frontend.Options{Target: target, PackageRoot: plan.Out, Destination: destination}
				if direct {
					opts.Source = source
					opts.PackageRoot = filepath.Join(t.TempDir(), "not-created")
				}
				merge, err := frontend.Build(opts)
				if err != nil {
					t.Fatal(err)
				}
				if direct && !reflect.DeepEqual(changes, merge.Changes) {
					t.Fatal("typed/package plan difference", changes, merge.Changes)
				}
				changes = merge.Changes
				if err = merge.Apply(); err != nil {
					t.Fatal(err)
				}
				body, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(outputXMLPath(plan))))
				if err != nil {
					t.Fatal(err)
				}
				got := decodedNative(t, body)
				if direct && !reflect.DeepEqual(native, got) {
					t.Fatal("typed/package native output differs")
				}
				native = got
				repeat, err := frontend.Build(opts)
				if err != nil {
					t.Fatal(err)
				}
				if len(repeat.Files) != 0 {
					t.Fatal("repeat changed destination", repeat.Files)
				}
			}
		})
	}
}
