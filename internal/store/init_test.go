package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesDataRootLayout(t *testing.T) {
	root := t.TempDir()

	result, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	for _, path := range []string{
		result.PlatformsPath,
		result.ConfigPath,
		filepath.Join(result.RuntimePath, "cache"),
		filepath.Join(result.RuntimePath, "downloads"),
		filepath.Join(result.RuntimePath, "indexes"),
		filepath.Join(result.RuntimePath, "runs"),
		filepath.Join(result.RuntimePath, "sources"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", path)
		}
	}

	contents := readFile(t, result.SourcesConfigPath)
	if !strings.Contains(contents, "type: secrets") {
		t.Fatalf("sources config does not use local secrets credentials")
	}
	if !strings.Contains(contents, "client_secret: client_secret") {
		t.Fatalf("sources config does not contain IGDB secret key template")
	}
	if !strings.Contains(contents, "https://gamesdb.launchbox-app.com/Metadata.zip") {
		t.Fatalf("sources config does not contain LaunchBox metadata snapshot URL")
	}
	catalog := readFile(t, result.CatalogConfigPath)
	if !strings.Contains(catalog, "region_priority: [usa, world, europe, japan, asia, russia]") {
		t.Fatalf("catalog config does not contain default fold priority:\n%s", catalog)
	}
	if !strings.Contains(catalog, "japan: [Japan, JP, JPN, JAP, JA]") {
		t.Fatalf("catalog config does not contain region aliases:\n%s", catalog)
	}
	if !strings.Contains(catalog, "asia: [Asia, China, PRC, CN, CHN") || !strings.Contains(catalog, "russia: [Russia, Russian Federation, RU, RUS]") {
		t.Fatalf("catalog config does not contain Asia/Russia region aliases:\n%s", catalog)
	}
	secrets := readFile(t, result.SecretsExamplePath)
	if !strings.Contains(secrets, "config/secrets.yaml") || !strings.Contains(secrets, "igdb:") || !strings.Contains(secrets, "client_secret:") {
		t.Fatalf("secrets example does not contain expected template:\n%s", secrets)
	}
	policies := readFile(t, result.SourcePoliciesPath)
	if !strings.Contains(policies, "launchbox:") || !strings.Contains(policies, "openvgdb:") || !strings.Contains(policies, "emumovies:") {
		t.Fatalf("source policies do not contain expected source defaults:\n%s", policies)
	}
}

func TestInitDoesNotOverwriteSourcesConfigByDefault(t *testing.T) {
	root := t.TempDir()
	first, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(first.SourcesConfigPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	second, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}

	if second.SourcesConfigCreated || second.SourcesConfigOverwritten {
		t.Fatalf("expected sources config to be kept")
	}
	if got := readFile(t, first.SourcesConfigPath); got != "custom: true\n" {
		t.Fatalf("sources config was overwritten: %q", got)
	}
}

func TestInitDoesNotOverwriteCatalogConfig(t *testing.T) {
	root := t.TempDir()
	first, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(first.CatalogConfigPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	second, err := Init(root, InitOptions{ForceConfig: true})
	if err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}

	if second.CatalogConfigCreated {
		t.Fatalf("expected catalog config to be kept")
	}
	if got := readFile(t, first.CatalogConfigPath); got != "custom: true\n" {
		t.Fatalf("catalog config was overwritten: %q", got)
	}
}

func TestInitCanForceSourcesConfig(t *testing.T) {
	root := t.TempDir()
	first, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(first.SourcesConfigPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	second, err := Init(root, InitOptions{ForceConfig: true})
	if err != nil {
		t.Fatalf("forced Init returned error: %v", err)
	}

	if !second.SourcesConfigOverwritten {
		t.Fatalf("expected sources config to be overwritten")
	}
	if got := readFile(t, first.SourcesConfigPath); !strings.Contains(got, "sources:") {
		t.Fatalf("sources config was not regenerated: %q", got)
	}
}

func TestInitDoesNotOverwriteSourcePolicies(t *testing.T) {
	root := t.TempDir()
	first, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(first.SourcePoliciesPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	second, err := Init(root, InitOptions{ForceConfig: true})
	if err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}
	if second.SourcePoliciesCreated {
		t.Fatalf("expected source policies to be kept")
	}
	if got := readFile(t, first.SourcePoliciesPath); got != "custom: true\n" {
		t.Fatalf("source policies were overwritten: %q", got)
	}
}

func TestInitDoesNotOverwriteSecretsExample(t *testing.T) {
	root := t.TempDir()
	first, err := Init(root, InitOptions{})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(first.SecretsExamplePath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	second, err := Init(root, InitOptions{ForceConfig: true})
	if err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}
	if second.SecretsExampleCreated {
		t.Fatalf("expected secrets example to be kept")
	}
	if got := readFile(t, first.SecretsExamplePath); got != "custom: true\n" {
		t.Fatalf("secrets example was overwritten: %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	return string(contents)
}
