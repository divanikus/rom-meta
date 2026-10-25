package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSourcesReadsGeneratedConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, InitOptions{}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}

	if len(config.Sources) != 6 {
		t.Fatalf("expected 6 sources, got %d", len(config.Sources))
	}
	rawConfig, err := os.ReadFile(filepath.Join(root, "config", "sources.yaml"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	for _, unwanted := range []string{"enabled:", "kind:", "levels:", "provides:"} {
		if strings.Contains(string(rawConfig), unwanted) {
			t.Fatalf("generated sources.yaml should not contain %q:\n%s", unwanted, string(rawConfig))
		}
	}

	launchbox := findSource(t, config, "launchbox")
	if launchbox.SnapshotURL == "" || launchbox.SnapshotPath == "" {
		t.Fatalf("expected launchbox snapshot config: %#v", launchbox)
	}
	if len(launchbox.CredentialEnvVars) != 0 {
		t.Fatalf("launchbox should not require credential envs: %#v", launchbox.CredentialEnvVars)
	}
	emumovies := findSource(t, config, "emumovies")
	if emumovies.APIBaseURL != "ftps://files.emumovies.com" {
		t.Fatalf("unexpected emumovies API base URL: %q", emumovies.APIBaseURL)
	}
	if emumovies.ArchiveExtractor != "auto" {
		t.Fatalf("unexpected emumovies archive extractor: %q", emumovies.ArchiveExtractor)
	}
	if len(emumovies.CredentialEnvVars) != 0 {
		t.Fatalf("emumovies should not use env credentials by default: %#v", emumovies.CredentialEnvVars)
	}
	if len(emumovies.CredentialSecrets) != 2 {
		t.Fatalf("unexpected emumovies credential secrets: %#v", emumovies.CredentialSecrets)
	}
	screenscraper := findSource(t, config, "screenscraper")
	if len(screenscraper.CredentialSecrets) != 4 {
		t.Fatalf("unexpected screenscraper credential secrets: %#v", screenscraper.CredentialSecrets)
	}
	igdb := findSource(t, config, "igdb")
	if len(igdb.CredentialEnvVars) != 0 {
		t.Fatalf("igdb should not use env credentials by default: %#v", igdb.CredentialEnvVars)
	}
	if len(igdb.CredentialSecrets) != 2 {
		t.Fatalf("unexpected igdb credential secrets: %#v", igdb.CredentialSecrets)
	}
	if igdb.OAuthTokenURL != "https://id.twitch.tv/oauth2/token" {
		t.Fatalf("unexpected igdb token URL: %q", igdb.OAuthTokenURL)
	}
	if igdb.OAuthGrantType != "client_credentials" {
		t.Fatalf("unexpected igdb grant type: %q", igdb.OAuthGrantType)
	}
	if igdb.APIBaseURL != "https://api.igdb.com/v4" {
		t.Fatalf("unexpected igdb API base URL: %q", igdb.APIBaseURL)
	}
	if igdb.RateLimitPerSecond != 4 || igdb.MaxConcurrentRequests != 8 {
		t.Fatalf("unexpected igdb API limits: rps=%d concurrency=%d", igdb.RateLimitPerSecond, igdb.MaxConcurrentRequests)
	}
	hasheous := findSource(t, config, "hasheous")
	if hasheous.APIBaseURL != "https://hasheous.org/api/v1" {
		t.Fatalf("unexpected hasheous API base URL: %q", hasheous.APIBaseURL)
	}
	if hasheous.RateLimitPerSecond != 8 {
		t.Fatalf("unexpected hasheous rate limit: %d", hasheous.RateLimitPerSecond)
	}
	if len(hasheous.CredentialSecrets) != 0 || len(hasheous.CredentialEnvVars) != 0 {
		t.Fatalf("hasheous should not require credentials: %#v %#v", hasheous.CredentialSecrets, hasheous.CredentialEnvVars)
	}
	openvgdb := findSource(t, config, "openvgdb")
	if openvgdb.SnapshotURL == "" {
		t.Fatalf("expected openvgdb snapshot URL")
	}
	if got := config.Fetch["metadata"]; len(got) != 3 || got[0] != "launchbox" || got[1] != "openvgdb" || got[2] != "hasheous" {
		t.Fatalf("unexpected metadata fetch defaults: %#v", got)
	}
	if got := config.Fetch["artwork"]; len(got) != 2 || got[0] != "launchbox" || got[1] != "openvgdb" {
		t.Fatalf("unexpected artwork fetch defaults: %#v", got)
	}
	if config.FetchConcurrency != 4 {
		t.Fatalf("unexpected fetch concurrency defaults: %#v", config.FetchConcurrency)
	}
	if config.PlatformAliases == nil {
		t.Fatalf("expected generated config to include platform aliases map")
	}
	if screenscraper.FetchConcurrency != 1 {
		t.Fatalf("unexpected screenscraper concurrency: %d", screenscraper.FetchConcurrency)
	}
	if emumovies.FetchConcurrency != 2 {
		t.Fatalf("unexpected emumovies concurrency: %d", emumovies.FetchConcurrency)
	}
}

func TestLoadSourcesReadsPlatformAliases(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "sources.yaml"), []byte(`platforms:
  aliases:
    segacd: "Sega - Mega CD & Sega CD"
    "pce cd": pcenginecd
sources:
  launchbox:
fetch:
  metadata:
    - launchbox
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}

	if config.PlatformAliases["segacd"] != "Sega - Mega CD & Sega CD" {
		t.Fatalf("unexpected segacd alias: %#v", config.PlatformAliases)
	}
	if config.PlatformAliases["pce cd"] != "pcenginecd" {
		t.Fatalf("unexpected quoted pce cd alias: %#v", config.PlatformAliases)
	}
}

func TestCredentialEnvStatusDoesNotRevealValues(t *testing.T) {
	t.Setenv("ROMMETA_TEST_USER", "secret-user")
	source := Source{CredentialEnvVars: []string{"ROMMETA_TEST_USER", "ROMMETA_TEST_PASSWORD"}}

	present, missing := CredentialEnvStatus(source)

	if len(present) != 1 || present[0] != "ROMMETA_TEST_USER" {
		t.Fatalf("unexpected present envs: %#v", present)
	}
	if len(missing) != 1 || missing[0] != "ROMMETA_TEST_PASSWORD" {
		t.Fatalf("unexpected missing envs: %#v", missing)
	}
}

func TestCredentialStatusReadsLocalSecretsWithoutValues(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, InitOptions{}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "secrets.yaml"), []byte(`sources:
  igdb:
    client_id: "secret-client"
    client_secret: ""
`), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}
	igdb := findSource(t, config, "igdb")
	present, missing := CredentialStatus(config, igdb)

	if len(present) != 1 || present[0].Kind != "secret" || present[0].Name != "sources.igdb.client_id" {
		t.Fatalf("unexpected present secrets: %#v", present)
	}
	if len(missing) != 1 || missing[0].Name != "sources.igdb.client_secret" {
		t.Fatalf("unexpected missing secrets: %#v", missing)
	}
	if value, ok := CredentialValue(config, igdb, "client_id"); !ok || value != "secret-client" {
		t.Fatalf("unexpected credential value lookup: value=%q ok=%t", value, ok)
	}
}

func TestLoadSourcesAddsKnownSourceCredentialRefs(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "sources.yaml"), []byte(`sources:
  screenscraper:
    credentials:
      type: secrets
      username: username
      password: password
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}
	source := findSource(t, config, "screenscraper")

	for _, expected := range []string{"username", "password", "dev_id", "dev_password"} {
		if !contains(source.CredentialSecrets, expected) {
			t.Fatalf("expected screenscraper credential %q in %#v", expected, source.CredentialSecrets)
		}
	}
}

func TestLoadSourcesReportsMissingConfig(t *testing.T) {
	_, err := LoadSources(t.TempDir())
	if err == nil {
		t.Fatalf("expected missing config error")
	}
}

func TestLoadSourcesReadsSnapshotPath(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "sources.yaml"), []byte(`sources:
  openvgdb:
    enabled: true
    kind: metadata
    snapshot:
      type: file
      path: "D:\Sources\openvgdb.sqlite"
      url: "https://example.test/openvgdb.zip"
    levels:
      metadata: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}
	openvgdb := findSource(t, config, "openvgdb")
	if !openvgdb.Enabled {
		t.Fatalf("expected openvgdb to be enabled")
	}
	if openvgdb.SnapshotPath != `D:\Sources\openvgdb.sqlite` {
		t.Fatalf("unexpected snapshot path: %q", openvgdb.SnapshotPath)
	}
	if openvgdb.SnapshotURL != "https://example.test/openvgdb.zip" {
		t.Fatalf("unexpected snapshot URL: %q", openvgdb.SnapshotURL)
	}
}

func TestLoadSourcesReadsProvidesSection(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "sources.yaml"), []byte(`sources:
  example:
    provides:
      metadata: true
      artwork: true
      video: false
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	config, err := LoadSources(root)
	if err != nil {
		t.Fatalf("LoadSources returned error: %v", err)
	}
	example := findSource(t, config, "example")
	if !example.Levels["metadata"] || !example.Levels["artwork"] || example.Levels["video"] {
		t.Fatalf("unexpected provides values: %#v", example.Levels)
	}
}

func TestSourceProvidesReturnsKnownCapabilities(t *testing.T) {
	provides := SourceProvides("igdb")

	if len(provides) != 2 || provides[0] != "metadata" || provides[1] != "artwork" {
		t.Fatalf("unexpected IGDB capabilities: %#v", provides)
	}

	provides[0] = "video"
	if again := SourceProvides("igdb"); again[0] != "metadata" {
		t.Fatalf("SourceProvides should return a copy, got %#v", again)
	}
	if hasheous := SourceProvides("hasheous"); len(hasheous) != 1 || hasheous[0] != "identity" {
		t.Fatalf("unexpected Hasheous capabilities: %#v", hasheous)
	}
	if !SourceIsIdentityOnly("hasheous") || SourceIsIdentityOnly("igdb") {
		t.Fatalf("unexpected identity-only classification")
	}
}

func findSource(t *testing.T, config SourcesConfig, name string) Source {
	t.Helper()
	for _, source := range config.Sources {
		if source.Name == name {
			return source
		}
	}
	t.Fatalf("source %q not found in %#v", name, config.Sources)
	return Source{}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
