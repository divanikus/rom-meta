package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
)

// SourcesConfig is a parsed config/sources.yaml file.
type SourcesConfig struct {
	Root             string
	Path             string
	Secrets          SecretsConfig
	Sources          []Source
	Fetch            map[string][]string
	FetchConcurrency int
	PlatformAliases  map[string]string
}

// Source is one configured metadata or media source.
type Source struct {
	Name                  string
	Enabled               bool
	Kind                  string
	CredentialType        string
	Credentials           []CredentialRef
	CredentialEnvVars     []string
	CredentialSecrets     []string
	Levels                map[string]bool
	SnapshotPath          string
	SnapshotURL           string
	OAuthTokenURL         string
	OAuthGrantType        string
	APIBaseURL            string
	RateLimitPerSecond    int
	MaxConcurrentRequests int
	FetchConcurrency      int
	ArchiveExtractor      string
}

var sourceProvides = map[string][]string{
	"emumovies":     {"artwork", "video"},
	"hasheous":      {"identity"},
	"igdb":          {"metadata", "artwork"},
	"launchbox":     {"metadata", "artwork"},
	"openvgdb":      {"metadata", "artwork"},
	"screenscraper": {"metadata", "artwork"},
}

// SourceProvidesCapability reports whether a known source provides one
// semantic capability. Unknown sources are left to the caller's policy.
func SourceProvidesCapability(name, capability string) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	for _, value := range SourceProvides(name) {
		if value == capability {
			return true
		}
	}
	return false
}

// SourceIsIdentityOnly reports whether a source can identify a game but does
// not provide descriptive metadata.
func SourceIsIdentityOnly(name string) bool {
	return SourceProvidesCapability(name, "identity") && !SourceProvidesCapability(name, "metadata")
}

var sourceParallelFetch = map[string]bool{
	"emumovies":     true,
	"hasheous":      true,
	"igdb":          true,
	"launchbox":     true,
	"openvgdb":      true,
	"screenscraper": false,
}

var sourceRequiredCredentials = map[string][]string{
	"emumovies":     {"username", "password"},
	"igdb":          {"client_id", "client_secret"},
	"screenscraper": {"username", "password", "dev_id", "dev_password"},
}

// SourceProvides returns the source capabilities implemented by RomMeta.
func SourceProvides(name string) []string {
	provides := sourceProvides[strings.ToLower(strings.TrimSpace(name))]
	if len(provides) == 0 {
		return nil
	}
	result := make([]string, len(provides))
	copy(result, provides)
	return result
}

// SourceRequiredCredentials returns the credential fields RomMeta needs for a known source.
func SourceRequiredCredentials(name string) []string {
	credentials := sourceRequiredCredentials[strings.ToLower(strings.TrimSpace(name))]
	if len(credentials) == 0 {
		return nil
	}
	result := make([]string, len(credentials))
	copy(result, credentials)
	return result
}

// SourceSupportsParallelFetch reports whether a source supports parallel fetch work.
func SourceSupportsParallelFetch(name string) bool {
	return sourceParallelFetch[strings.ToLower(strings.TrimSpace(name))]
}

// CredentialRef names one credential field and how it is resolved.
type CredentialRef struct {
	Name string
	Ref  string
}

// SecretsConfig is a parsed local config/secrets.yaml file.
type SecretsConfig struct {
	Path   string
	Exists bool
	Values map[string]map[string]string
	Err    error
}

// CredentialStatusItem identifies a present or missing credential without its value.
type CredentialStatusItem struct {
	Kind string
	Name string
}

// LoadSources reads config/sources.yaml from a data root.
func LoadSources(root string) (SourcesConfig, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return SourcesConfig{}, err
	}
	path := filepath.Join(absRoot, "config", "sources.yaml")
	file, err := catalogfs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SourcesConfig{}, fmt.Errorf("source config not found: %s (run rom-meta init <data-root>)", path)
		}
		return SourcesConfig{}, err
	}
	defer file.Close()

	config := SourcesConfig{Root: absRoot, Path: path, Secrets: loadSecrets(absRoot), PlatformAliases: map[string]string{}}
	var current *Source
	var section string
	inSources := false
	inFetch := false
	inPlatforms := false
	inPlatformAliases := false
	fetchType := ""

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := trimSourceComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := sourceIndent(line)
		value := strings.TrimSpace(line)

		if indent == 0 {
			inSources = value == "sources:"
			inFetch = value == "fetch:"
			inPlatforms = value == "platforms:"
			inPlatformAliases = false
			current = nil
			section = ""
			fetchType = ""
			continue
		}
		if inPlatforms {
			if indent == 2 && strings.HasSuffix(value, ":") {
				inPlatformAliases = strings.TrimSuffix(value, ":") == "aliases"
				continue
			}
			if indent == 4 && inPlatformAliases {
				key, raw, ok := splitSourceKV(value)
				if !ok {
					continue
				}
				alias := parseSourceScalar(raw)
				if alias == "" {
					continue
				}
				if config.PlatformAliases == nil {
					config.PlatformAliases = map[string]string{}
				}
				config.PlatformAliases[parseSourceScalar(key)] = alias
				continue
			}
		}
		if inFetch {
			if indent == 2 {
				key, raw, ok := splitSourceKV(value)
				if !ok {
					continue
				}
				if key == "concurrency" {
					config.FetchConcurrency = parseInt(raw)
					continue
				}
				fetchType = key
				if config.Fetch == nil {
					config.Fetch = map[string][]string{}
				}
				config.Fetch[fetchType] = nil
				if strings.TrimSpace(raw) != "" {
					var sources []string
					if err := yaml.Unmarshal([]byte(raw), &sources); err != nil {
						return SourcesConfig{}, fmt.Errorf("fetch.%s must be a source list: %w", key, err)
					}
					config.Fetch[fetchType] = sources
				}
				continue
			}
			if indent == 4 && strings.HasPrefix(value, "- ") && fetchType != "" {
				source := parseSourceScalar(strings.TrimSpace(strings.TrimPrefix(value, "- ")))
				if source != "" {
					config.Fetch[fetchType] = append(config.Fetch[fetchType], source)
				}
				continue
			}
		}
		if !inSources {
			continue
		}

		if indent == 2 && strings.HasSuffix(value, ":") {
			name := strings.TrimSuffix(value, ":")
			source := Source{Name: name, Levels: map[string]bool{}}
			config.Sources = append(config.Sources, source)
			current = &config.Sources[len(config.Sources)-1]
			section = ""
			continue
		}
		if current == nil {
			continue
		}
		if indent == 4 && strings.HasSuffix(value, ":") {
			section = strings.TrimSuffix(value, ":")
			continue
		}
		key, raw, ok := splitSourceKV(value)
		if !ok {
			continue
		}
		switch {
		case indent == 4:
			section = ""
			switch key {
			case "enabled":
				current.Enabled = parseBool(raw)
			case "kind":
				current.Kind = parseSourceScalar(raw)
			case "concurrency":
				current.FetchConcurrency = parseInt(raw)
			}
		case indent == 6 && section == "credentials":
			if key == "type" {
				current.CredentialType = parseSourceScalar(raw)
				continue
			}
			ref := parseSourceScalar(raw)
			if ref == "" {
				ref = key
			}
			current.Credentials = append(current.Credentials, CredentialRef{Name: key, Ref: ref})
		case indent == 6 && (section == "levels" || section == "provides"):
			current.Levels[key] = parseBool(raw)
		case indent == 6 && section == "snapshot" && key == "path":
			current.SnapshotPath = parseSourceScalar(raw)
		case indent == 6 && section == "snapshot" && key == "url":
			current.SnapshotURL = parseSourceScalar(raw)
		case indent == 6 && section == "oauth" && key == "token_url":
			current.OAuthTokenURL = parseSourceScalar(raw)
		case indent == 6 && section == "oauth" && key == "grant_type":
			current.OAuthGrantType = parseSourceScalar(raw)
		case indent == 6 && section == "api" && key == "base_url":
			current.APIBaseURL = parseSourceScalar(raw)
		case indent == 6 && section == "api" && key == "rate_limit_per_second":
			current.RateLimitPerSecond = parseInt(raw)
		case indent == 6 && section == "api" && key == "max_concurrent_requests":
			current.MaxConcurrentRequests = parseInt(raw)
		case indent == 6 && section == "tools" && key == "archive_extractor":
			current.ArchiveExtractor = parseSourceScalar(raw)
		}
	}
	if err := scanner.Err(); err != nil {
		return SourcesConfig{}, err
	}
	for i := range config.Sources {
		finalizeSourceCredentials(&config.Sources[i])
		sort.Strings(config.Sources[i].CredentialEnvVars)
		sort.Strings(config.Sources[i].CredentialSecrets)
	}
	sort.Slice(config.Sources, func(i, j int) bool {
		return config.Sources[i].Name < config.Sources[j].Name
	})
	return config, nil
}

func finalizeSourceCredentials(source *Source) {
	required := SourceRequiredCredentials(source.Name)
	hadCredentialConfig := source.CredentialType != "" || len(source.Credentials) > 0
	for _, credential := range required {
		if !hasCredentialRef(source.Credentials, credential) {
			source.Credentials = append(source.Credentials, CredentialRef{Name: credential, Ref: credential})
		}
	}
	if source.CredentialType == "" && len(source.Credentials) > 0 && len(required) > 0 && !hadCredentialConfig {
		source.CredentialType = "secrets"
	}
	if source.CredentialType == "" && len(source.Credentials) > 0 {
		source.CredentialType = "env"
	}
	source.CredentialEnvVars = nil
	source.CredentialSecrets = nil
	for _, credential := range source.Credentials {
		switch source.CredentialType {
		case "env":
			source.CredentialEnvVars = append(source.CredentialEnvVars, credential.Ref)
		case "secrets", "secret", "file", "yaml":
			source.CredentialSecrets = append(source.CredentialSecrets, credential.Ref)
		}
	}
}

func hasCredentialRef(credentials []CredentialRef, name string) bool {
	for _, credential := range credentials {
		if credential.Name == name {
			return true
		}
	}
	return false
}

// CredentialEnvStatus returns configured credential env vars split by presence.
func CredentialEnvStatus(source Source) (present []string, missing []string) {
	for _, name := range source.CredentialEnvVars {
		if _, ok := os.LookupEnv(name); ok {
			present = append(present, name)
		} else {
			missing = append(missing, name)
		}
	}
	return present, missing
}

// CredentialStatus returns configured credentials split by presence without values.
func CredentialStatus(config SourcesConfig, source Source) (present []CredentialStatusItem, missing []CredentialStatusItem) {
	switch source.CredentialType {
	case "", "env":
		for _, name := range source.CredentialEnvVars {
			item := CredentialStatusItem{Kind: "env", Name: name}
			if _, ok := os.LookupEnv(name); ok {
				present = append(present, item)
			} else {
				missing = append(missing, item)
			}
		}
	case "secrets", "secret", "file", "yaml":
		for _, credential := range source.Credentials {
			name := credential.Ref
			if name == "" {
				name = credential.Name
			}
			item := CredentialStatusItem{Kind: "secret", Name: fmt.Sprintf("sources.%s.%s", source.Name, name)}
			if config.Secrets.Err == nil && config.Secrets.Exists && config.Secrets.Values[source.Name][name] != "" {
				present = append(present, item)
			} else {
				missing = append(missing, item)
			}
		}
	}
	return present, missing
}

// CredentialValue returns a configured credential value without exposing it in status output.
func CredentialValue(config SourcesConfig, source Source, name string) (string, bool) {
	for _, credential := range source.Credentials {
		if credential.Name != name {
			continue
		}
		switch source.CredentialType {
		case "", "env":
			return os.LookupEnv(credential.Ref)
		case "secrets", "secret", "file", "yaml":
			if config.Secrets.Err != nil || !config.Secrets.Exists {
				return "", false
			}
			value := config.Secrets.Values[source.Name][credential.Ref]
			return value, value != ""
		}
	}
	return "", false
}

// ResolveDataRootPath resolves a possibly relative path against a data root.
func ResolveDataRootPath(root, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

func loadSecrets(root string) SecretsConfig {
	path := filepath.Join(root, "config", "secrets.yaml")
	config := SecretsConfig{Path: path, Values: map[string]map[string]string{}}
	file, err := catalogfs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config
		}
		config.Err = err
		return config
	}
	defer file.Close()
	config.Exists = true

	inSources := false
	currentSource := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := trimSourceComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := sourceIndent(line)
		value := strings.TrimSpace(line)
		if indent == 0 {
			inSources = value == "sources:"
			currentSource = ""
			continue
		}
		if !inSources {
			continue
		}
		if indent == 2 && strings.HasSuffix(value, ":") {
			currentSource = strings.TrimSuffix(value, ":")
			if config.Values[currentSource] == nil {
				config.Values[currentSource] = map[string]string{}
			}
			continue
		}
		if indent == 4 && currentSource != "" {
			key, raw, ok := splitSourceKV(value)
			if ok {
				config.Values[currentSource][key] = parseSourceScalar(raw)
			}
		}
	}
	config.Err = scanner.Err()
	return config
}

func trimSourceComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func sourceIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func splitSourceKV(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
}

func parseBool(value string) bool {
	parsed, err := strconv.ParseBool(strings.ToLower(parseSourceScalar(value)))
	return err == nil && parsed
}

func parseInt(value string) int {
	parsed, err := strconv.Atoi(parseSourceScalar(value))
	if err != nil {
		return 0
	}
	return parsed
}

func parseSourceScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}
