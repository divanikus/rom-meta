package exporter

import (
	"reflect"
	"strings"
	"testing"
)

func TestSharedExportProfileTargetMarkers(t *testing.T) {
	baseline, err := loadProfileReader("", strings.NewReader("schema_version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"", "target: es-de\n", "target: launchbox\n", "target: ' LAUNCHBOX '\n"} {
		p, err := loadProfileReader("", strings.NewReader("schema_version: 1\n"+marker))
		if err != nil || !reflect.DeepEqual(p, baseline) {
			t.Fatalf("%q: %+v, %v", marker, p, err)
		}
		wire, _, err := ClientExportProfile("schema_version: 1\n" + marker)
		if err != nil {
			t.Fatal(err)
		}
		if wire != "schema_version: 1\n"+marker {
			t.Fatalf("rewrote shared profile: %s", wire)
		}
	}
	for _, body := range []string{"schema_version: 2\n", "target: unsupported\n", "target: [launchbox]\n", "unknown: true\n"} {
		if err := ValidateExportProfile(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestClientProfilePreservesMergedRenderingOptions(t *testing.T) {
	input := "schema_version: 1\n<<: {target: launchbox, launchbox: {kinds: [translation, original]}}\nselection:\n  description: {languages: [ru, en]}\n"
	wire, policy, err := ClientExportProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	if wire != input || policy.Kinds[0] != "translation" {
		t.Fatal(wire, policy)
	}
	server, err := ExportProfile("", wire)
	if err != nil || !reflect.DeepEqual(server.LaunchBox, policy) || server.Selection.Description.Languages[0] != "ru" {
		t.Fatal(server, err)
	}
}
