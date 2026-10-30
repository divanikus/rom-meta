package exporter

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
		var legacy struct{ Target string }
		if err := yaml.Unmarshal([]byte(wire), &legacy); err != nil || legacy.Target != "" && legacy.Target != "es-de" {
			t.Fatalf("incompatible legacy profile: %s (%v)", wire, err)
		}
	}
	for _, body := range []string{"schema_version: 2\n", "target: unsupported\n", "target: [launchbox]\n", "unknown: true\n"} {
		if err := ValidateExportProfile(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestClientProfileExpandsMergedRenderingOptions(t *testing.T) {
	// A root merge is valid YAML; client rendering settings must not leak through
	// it to older APIs that only understand the shared selection policy.
	input := "schema_version: 1\n<<: {target: launchbox, launchbox: {kinds: [translation, original]}}\nselection:\n  description: {languages: [ru, en]}\n"
	wire, policy, err := ClientExportProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(wire), &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["launchbox"]; exists {
		t.Fatal("rendering policy leaked to API", wire)
	}
	if fields["target"] != "es-de" || policy.Kinds[0] != "translation" {
		t.Fatal(wire, policy)
	}
	if _, exists := fields["selection"]; !exists {
		t.Fatal("lost selection policy", wire)
	}
	if _, exists := fields["media_overrides"]; exists {
		t.Fatal("injected client defaults", wire)
	}
}
