package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSourcePolicyCreatesRootPolicy(t *testing.T) {
	root := t.TempDir()

	path, err := EnsureSourcePolicy(root, "launchbox", "review", "LaunchBox media")
	if err != nil {
		t.Fatalf("EnsureSourcePolicy returned error: %v", err)
	}
	if path != filepath.Join(root, "source-policies.yaml") {
		t.Fatalf("unexpected policy path %q", path)
	}
	contents := readFile(t, path)
	for _, expected := range []string{
		"sources:",
		"  launchbox:",
		"scope: review",
		"LaunchBox media",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("policy file does not contain %q:\n%s", expected, contents)
		}
	}
}

func TestEnsureSourcePolicyUpsertsSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source-policies.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 0

sources:
  openvgdb:
    media:
      redistributable: false
      scope: review
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := EnsureSourcePolicy(root, "launchbox", "review", "LaunchBox media"); err != nil {
		t.Fatalf("EnsureSourcePolicy returned error: %v", err)
	}
	contents := readFile(t, path)
	if !strings.Contains(contents, "openvgdb:") || !strings.Contains(contents, "launchbox:") {
		t.Fatalf("policy file should keep openvgdb and add launchbox:\n%s", contents)
	}
}
