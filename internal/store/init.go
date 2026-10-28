package store

import (
	"fmt"
	"os"
	"path/filepath"

	"rommeta/internal/catalogfs"
)

// InitOptions controls data-root initialization.
type InitOptions struct {
	ForceConfig bool
}

// InitResult describes the initialized data root.
type InitResult struct {
	Root                     string
	PlatformsPath            string
	ConfigPath               string
	RuntimePath              string
	SourcesConfigPath        string
	CatalogConfigPath        string
	SecretsExamplePath       string
	SourcePoliciesPath       string
	CreatedDirectoryCount    int
	SourcesConfigCreated     bool
	SourcesConfigOverwritten bool
	CatalogConfigCreated     bool
	SecretsExampleCreated    bool
	SourcePoliciesCreated    bool
}

// Init creates the standard RomMeta data-root layout.
func Init(root string, options InitOptions) (InitResult, error) {
	if root == "" {
		return InitResult{}, fmt.Errorf("data root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return InitResult{}, err
	}

	result := InitResult{
		Root:               absRoot,
		PlatformsPath:      filepath.Join(absRoot, "platforms"),
		ConfigPath:         filepath.Join(absRoot, "config"),
		RuntimePath:        filepath.Join(absRoot, ".rommeta"),
		SourcesConfigPath:  filepath.Join(absRoot, "config", "sources.yaml"),
		CatalogConfigPath:  filepath.Join(absRoot, "config", "catalog.yaml"),
		SecretsExamplePath: filepath.Join(absRoot, "config", "secrets.example.yaml"),
		SourcePoliciesPath: SourcePoliciesPath(absRoot),
	}

	for _, dir := range []string{
		result.PlatformsPath,
		result.ConfigPath,
		filepath.Join(result.RuntimePath, "cache"),
		filepath.Join(result.RuntimePath, "downloads"),
		filepath.Join(result.RuntimePath, "indexes"),
		filepath.Join(result.RuntimePath, "runs"),
		filepath.Join(result.RuntimePath, "sources"),
	} {
		created, err := mkdirIfMissing(dir)
		if err != nil {
			return InitResult{}, err
		}
		if created {
			result.CreatedDirectoryCount++
		}
	}

	if _, err := catalogfs.Stat(result.SourcePoliciesPath); err == nil {
		// Keep user edits.
	} else if err != nil && os.IsNotExist(err) {
		if err := catalogfs.WriteFile(result.SourcePoliciesPath, []byte(defaultSourcePoliciesConfig), 0o644); err != nil {
			return InitResult{}, err
		}
		result.SourcePoliciesCreated = true
	} else if err != nil {
		return InitResult{}, err
	}

	if _, err := catalogfs.Stat(result.SecretsExamplePath); err == nil {
		// Keep user edits.
	} else if err != nil && os.IsNotExist(err) {
		if err := catalogfs.WriteFile(result.SecretsExamplePath, []byte(defaultSecretsExampleConfig), 0o644); err != nil {
			return InitResult{}, err
		}
		result.SecretsExampleCreated = true
	} else if err != nil {
		return InitResult{}, err
	}

	if _, err := catalogfs.Stat(result.CatalogConfigPath); err == nil {
		// Keep user edits.
	} else if err != nil && os.IsNotExist(err) {
		if err := catalogfs.WriteFile(result.CatalogConfigPath, []byte(defaultCatalogConfig), 0o644); err != nil {
			return InitResult{}, err
		}
		result.CatalogConfigCreated = true
	} else if err != nil {
		return InitResult{}, err
	}

	if _, err := catalogfs.Stat(result.SourcesConfigPath); err == nil && !options.ForceConfig {
		return result, nil
	} else if err != nil && !os.IsNotExist(err) {
		return InitResult{}, err
	}

	if err := catalogfs.WriteFile(result.SourcesConfigPath, []byte(defaultSourcesConfig), 0o644); err != nil {
		return InitResult{}, err
	}
	if options.ForceConfig {
		result.SourcesConfigOverwritten = true
	} else {
		result.SourcesConfigCreated = true
	}
	return result, nil
}

func mkdirIfMissing(path string) (bool, error) {
	if info, err := catalogfs.Stat(path); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s exists and is not a directory", path)
		}
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return true, catalogfs.MkdirAll(path, 0o755)
}

const defaultSourcesConfig = `schema_version: 0

# Source configuration is shareable only while it contains no secrets.
# Keep credential values in local config/secrets.yaml, ignored local files, or an OS keychain.
sources:
  launchbox:
    snapshot:
      type: file
      path: .rommeta/sources/launchbox/Metadata.zip
      url: https://gamesdb.launchbox-app.com/Metadata.zip

  screenscraper:
    concurrency: 1
    credentials:
      type: secrets
      username: username
      password: password
      dev_id: dev_id
      dev_password: dev_password
    api:
      base_url: https://api.screenscraper.fr/api2
      rate_limit_per_second: 1

  emumovies:
    concurrency: 2
    credentials:
      type: secrets
      username: username
      password: password
    api:
      base_url: ftps://files.emumovies.com
    tools:
      archive_extractor: auto

  igdb:
    credentials:
      type: secrets
      client_id: client_id
      client_secret: client_secret
    oauth:
      token_url: https://id.twitch.tv/oauth2/token
      grant_type: client_credentials
    api:
      base_url: https://api.igdb.com/v4
      rate_limit_per_second: 4
      max_concurrent_requests: 8

  hasheous:
    api:
      base_url: https://hasheous.org/api/v1
      rate_limit_per_second: 1

  openvgdb:
    snapshot:
      type: file
      path: .rommeta/sources/openvgdb/openvgdb.sqlite
      url: https://github.com/OpenVGDB/OpenVGDB/releases/download/v29.0/openvgdb.zip

fetch:
  concurrency: 4

  metadata:
    - launchbox
    - openvgdb
    - hasheous
  artwork:
    - launchbox
    - openvgdb
  video:

priority:
  identity:
    - no-intro
    - redump
    - mame
  fingerprints:
    - no-intro
    - redump
    - mame
    - openvgdb
  grouping:
    - launchbox
    - screenscraper
    - openvgdb
    - igdb
  metadata:
    - launchbox
    - screenscraper
    - openvgdb
    - igdb
  artwork:
    - emumovies
    - launchbox
    - screenscraper
    - igdb
  video:
    - emumovies
    - screenscraper

rules:
  explicit_overrides: highest-when-present
  authority_fields:
    - identity
    - fingerprints
  fallback_mode: first-useful
  overwrite_mode: never-without-review
  keep_candidates: true
`

const defaultSecretsExampleConfig = `schema_version: 0

# Copy this file to config/secrets.yaml and fill only the sources you use.
# config/secrets.yaml is local-only and must not be committed.
sources:
  screenscraper:
    username: ""
    password: ""
    dev_id: ""
    dev_password: ""

  emumovies:
    username: ""
    password: ""

  igdb:
    client_id: ""
    client_secret: ""
`

const defaultSourcePoliciesConfig = `schema_version: 0

default:
  media:
    redistributable: false
    scope: review

sources:
  launchbox:
    media:
      redistributable: false
      scope: review
      note: "Imported from LaunchBox Games Database media URLs; verify rights before sharing."

  openvgdb:
    media:
      redistributable: false
      scope: review
      note: "Imported from OpenVGDB media URLs; verify rights before sharing."

  igdb:
    media:
      redistributable: false
      scope: review
      note: "Imported from IGDB media URLs; verify rights before sharing."

  emumovies:
    media:
      redistributable: false
      scope: personal
      note: "Imported from EmuMovies account downloads; do not include in shareable packages."
`
