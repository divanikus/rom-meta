package catalogyaml

import (
	"reflect"
	"testing"
)

func TestPreservedSourcesYAMLControls(t *testing.T) {
	for _, body := range []string{
		"fetch: {preserve_sources: [IGDB, openvgdb, igdb]}",
		"defaults: &policy {preserve_sources: [IGDB, openvgdb, igdb]}\nfetch: *policy",
		"defaults: &policy {preserve_sources: [IGDB, openvgdb, igdb]}\nfetch: {<<: *policy}",
	} {
		doc, err := Parse([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		got, err := PreservedSources(doc.Root())
		if err != nil || !reflect.DeepEqual(got, []string{"igdb", "openvgdb"}) {
			t.Fatalf("%s: %#v %v", body, got, err)
		}
	}
	for _, body := range []string{"fetch: wrong", "fetch: {preserve_sources: igdb}", "fetch: {preserve_sources: null}", "fetch: {preserve_sources: [true]}", "fetch: {preserve_sources: ['']}"} {
		doc, err := Parse([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = PreservedSources(doc.Root()); err == nil {
			t.Fatalf("malformed preservation accepted: %s", body)
		}
	}
}
