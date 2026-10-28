package platformnames

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	_ "embed"

	"rommeta/internal/catalogfs"
)

const (
	SourceEmuMovies     = "emumovies"
	SourceIGDB          = "igdb"
	SourceLaunchBox     = "launchbox"
	SourceOpenVGDB      = "openvgdb"
	SourceScreenScraper = "screenscraper"
)

//go:embed aliases.yaml
var defaultAliasYAML string

type aliasSet struct {
	Keys            []string
	Display         string
	EmuMovies       string
	IGDB            string
	LaunchBox       string
	OpenVGDB        string
	ScreenScraper   string
	ScreenScraperID string
}

type aliasEntry struct {
	Match string
	Alias aliasSet
}

// SourceRef describes how one source names or identifies a platform.
type SourceRef struct {
	Name  string
	Names []string
	ID    string
}

// Platform describes one canonical catalog platform name.
type Platform struct {
	Name    string
	Aliases []string
	Sources map[string]SourceRef
}

// Registry resolves user-facing and source-specific platform names.
type Registry struct {
	platforms map[string]Platform
}

var aliases = loadAliases(defaultAliasYAML)
var cliAliases = loadCLIAliases(defaultAliasYAML)
var defaultRegistry = registryFromAliasEntries(parseAliasEntries(defaultAliasYAML))

// DefaultRegistry returns the embedded platform registry.
func DefaultRegistry() Registry {
	return defaultRegistry.Clone()
}

// LoadRegistry returns the embedded registry with optional local
// config/platforms.yaml overrides applied.
func LoadRegistry(dataRoot string) (Registry, bool, error) {
	registry := DefaultRegistry()
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return Registry{}, false, err
	}
	local, ok, err := LoadRegistryFile(filepath.Join(root, "config", "platforms.yaml"))
	if err != nil {
		return Registry{}, false, err
	}
	if ok {
		registry.Merge(local)
	}
	return registry, ok, nil
}

// LoadRegistryFile reads a local platform registry override file. Missing files
// are accepted and reported as not loaded.
func LoadRegistryFile(path string) (Registry, bool, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{}, false, nil
		}
		return Registry{}, false, err
	}
	registry, err := ParseRegistryYAML(string(contents))
	if err != nil {
		return Registry{}, false, err
	}
	return registry, true, nil
}

// NewRegistry returns an empty platform registry.
func NewRegistry() Registry {
	return Registry{platforms: map[string]Platform{}}
}

// Clone returns an independent copy of the registry.
func (r Registry) Clone() Registry {
	clone := NewRegistry()
	for _, platform := range r.Platforms() {
		clone.setPlatform(platform)
	}
	return clone
}

// Merge overlays another registry on top of this registry.
func (r *Registry) Merge(other Registry) {
	if r.platforms == nil {
		r.platforms = map[string]Platform{}
	}
	for _, platform := range other.Platforms() {
		existing := r.ensurePlatform(platform.Name)
		existing.Aliases = appendUniqueStrings(existing.Aliases, platform.Aliases...)
		if existing.Sources == nil {
			existing.Sources = map[string]SourceRef{}
		}
		for source, ref := range platform.Sources {
			existing.Sources[sourceKey(source)] = ref
		}
		r.setPlatform(existing)
	}
}

// MergeAliases adds simple local aliases that resolve to canonical platform
// names or to other known aliases.
func (r *Registry) MergeAliases(local map[string]string) {
	for alias, target := range local {
		canonical := r.ResolveCLI(target)
		if strings.TrimSpace(canonical) == "" {
			continue
		}
		platform := r.ensurePlatform(canonical)
		platform.Aliases = appendUniqueString(platform.Aliases, alias)
		r.setPlatform(platform)
	}
}

// ResolveCLI returns the canonical catalog platform name for user input.
func (r Registry) ResolveCLI(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if platform, ok := r.lookup(name); ok {
		return platform.Name
	}
	return name
}

// ForSource returns the preferred source-specific platform name.
func (r Registry) ForSource(name, source string) string {
	canonical := r.ResolveCLI(name)
	if platform, ok := r.lookup(canonical); ok {
		if ref, ok := platform.Sources[sourceKey(source)]; ok {
			if ref.Name != "" {
				return ref.Name
			}
			if len(ref.Names) > 0 {
				return ref.Names[0]
			}
		}
	}
	return Display(canonical)
}

// ResolveForSource returns the source platform name when it can be matched
// against a source-provided platform list.
func (r Registry) ResolveForSource(name, source string, available []string) string {
	if resolved, ok := r.MatchForSource(name, source, available); ok {
		return resolved
	}
	return r.ForSource(name, source)
}

// MatchForSource checks registry candidates against source names.
func (r Registry) MatchForSource(name, source string, available []string) (string, bool) {
	byKey := map[string]string{}
	ambiguous := map[string]bool{}
	for _, value := range available {
		value = strings.TrimSpace(value)
		lookupKey := key(value)
		if lookupKey == "" {
			continue
		}
		if existing, ok := byKey[lookupKey]; ok && existing != value {
			ambiguous[lookupKey] = true
			continue
		}
		byKey[lookupKey] = value
	}
	for _, candidate := range r.CandidatesForSource(name, source) {
		lookupKey := key(candidate)
		if lookupKey == "" || ambiguous[lookupKey] {
			continue
		}
		if value, ok := byKey[lookupKey]; ok {
			return value, true
		}
	}
	return "", false
}

// CandidatesForSource returns platform-name candidates in lookup priority.
func (r Registry) CandidatesForSource(name, source string) []string {
	canonical := r.ResolveCLI(name)
	var candidates []string
	if platform, ok := r.lookup(canonical); ok {
		if ref, ok := platform.Sources[sourceKey(source)]; ok {
			candidates = appendUniqueString(candidates, ref.Name)
			candidates = appendUniqueStrings(candidates, ref.Names...)
		}
		for _, ref := range platform.Sources {
			candidates = appendUniqueString(candidates, ref.Name)
			candidates = appendUniqueStrings(candidates, ref.Names...)
		}
		candidates = appendUniqueStrings(candidates, platform.Aliases...)
	}
	candidates = appendUniqueString(candidates, ForSource(canonical, source))
	candidates = appendUniqueString(candidates, Display(canonical))
	candidates = appendUniqueString(candidates, stripKnownPrefix(canonical))
	candidates = appendUniqueString(candidates, canonical)
	candidates = appendUniqueString(candidates, name)
	return candidates
}

// SourceID returns a source-specific platform ID when configured.
func (r Registry) SourceID(name, source string) (string, bool) {
	canonical := r.ResolveCLI(name)
	platform, ok := r.lookup(canonical)
	if !ok {
		return "", false
	}
	ref, ok := platform.Sources[sourceKey(source)]
	if !ok || strings.TrimSpace(ref.ID) == "" {
		return "", false
	}
	return strings.TrimSpace(ref.ID), true
}

// Entry returns one registry platform.
func (r Registry) Entry(name string) (Platform, bool) {
	return r.lookup(r.ResolveCLI(name))
}

// Platforms returns all registry platforms sorted by canonical name.
func (r Registry) Platforms() []Platform {
	values := make([]Platform, 0, len(r.platforms))
	for _, platform := range r.platforms {
		values = append(values, clonePlatform(platform))
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Name < values[j].Name
	})
	return values
}

// Display returns the friendly platform name used when no source-specific
// naming convention is required.
func Display(name string) string {
	if alias, ok := aliases[key(name)]; ok && alias.Display != "" {
		return alias.Display
	}
	return stripKnownPrefix(name)
}

// ResolveCLI returns the canonical catalog platform name for a user-facing CLI
// platform argument. Unknown values are returned unchanged so explicit overrides
// remain possible.
func ResolveCLI(name string) string {
	return defaultRegistry.ResolveCLI(name)
}

// ResolveCLIWithAliases returns the canonical catalog platform name for a
// user-facing CLI platform argument, using local aliases before the embedded
// convention table.
func ResolveCLIWithAliases(name string, localAliases map[string]string) string {
	registry := DefaultRegistry()
	registry.MergeAliases(localAliases)
	return registry.ResolveCLI(name)
}

// ForSource returns the platform name convention expected by a source.
func ForSource(name, source string) string {
	canonical := defaultRegistry.ResolveCLI(name)
	if _, ok := defaultRegistry.lookup(canonical); ok {
		return defaultRegistry.ForSource(name, source)
	}
	if alias, ok := aliases[key(name)]; ok {
		if sourceName := alias.nameForSource(source); sourceName != "" {
			return sourceName
		}
	}
	return defaultRegistry.ForSource(name, source)
}

// ResolveForSource returns the source platform name when it can be matched
// against a source-provided platform list. It falls back to the local
// convention when the list is empty or does not contain a unique match.
func ResolveForSource(name, source string, available []string) string {
	return defaultRegistry.ResolveForSource(name, source, available)
}

// MatchForSource checks local platform naming candidates against source names.
func MatchForSource(name, source string, available []string) (string, bool) {
	return defaultRegistry.MatchForSource(name, source, available)
}

// CandidatesForSource returns platform-name candidates in lookup priority.
func CandidatesForSource(name, source string) []string {
	return defaultRegistry.CandidatesForSource(name, source)
}

// ParseRegistryYAML reads the compact config/platforms.yaml registry format.
func ParseRegistryYAML(data string) (Registry, error) {
	registry := NewRegistry()
	var current string
	inAliases := false
	inSources := false
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		raw := trimRegistryComment(scanner.Text())
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := countIndent(raw)
		line := strings.TrimSpace(raw)
		if indent == 0 {
			if !strings.HasSuffix(line, ":") {
				return Registry{}, fmt.Errorf("invalid platform entry %q", line)
			}
			current = parseAliasScalar(strings.TrimSpace(strings.TrimSuffix(line, ":")))
			if current == "" {
				return Registry{}, fmt.Errorf("empty platform entry")
			}
			registry.ensurePlatform(current)
			inAliases = false
			inSources = false
			continue
		}
		if current == "" {
			continue
		}
		if indent == 2 {
			field, rawValue, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			field = strings.TrimSpace(field)
			rawValue = strings.TrimSpace(rawValue)
			inAliases = field == "aliases"
			inSources = field == "sources"
			if field == "aliases" && rawValue != "" {
				registry.addAliases(current, parseAliasList(rawValue))
			}
			continue
		}
		if indent == 4 && inAliases && strings.HasPrefix(line, "- ") {
			registry.addAliases(current, []string{parseAliasScalar(strings.TrimSpace(strings.TrimPrefix(line, "- ")))})
			continue
		}
		if indent == 4 && inSources {
			source, rawValue, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			source = strings.TrimSpace(source)
			rawValue = strings.TrimSpace(rawValue)
			ref := parseSourceRef(rawValue)
			if source != "" && !ref.empty() {
				registry.setSource(current, source, ref)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func loadAliases(data string) map[string]aliasSet {
	entries := parseAliasEntries(data)
	result := map[string]aliasSet{}
	for _, entry := range entries {
		setAlias(result, entry.Match, entry.Alias, true)
		setAlias(result, entry.Alias.Display, entry.Alias, true)
	}
	for _, entry := range entries {
		for _, value := range entry.Alias.sourceNames() {
			setAlias(result, value, entry.Alias, false)
		}
	}
	return result
}

func loadCLIAliases(data string) map[string]string {
	entries := parseAliasEntries(data)
	result := map[string]string{}
	for _, entry := range entries {
		canonical := canonicalPlatformName(entry.Match)
		setCLIAlias(result, entry.Match, canonical)
		setCLIAlias(result, canonical, canonical)
		for _, value := range entry.Alias.Keys {
			setCLIAlias(result, value, canonical)
		}
	}
	return result
}

func parseAliasEntries(data string) []aliasEntry {
	var entries []aliasEntry
	var match string
	var current aliasSet
	flush := func() {
		if strings.TrimSpace(match) == "" {
			return
		}
		entries = append(entries, aliasEntry{Match: match, Alias: current})
		match = ""
		current = aliasSet{}
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "aliases:" {
			continue
		}
		if strings.HasPrefix(line, "- match:") {
			flush()
			match = parseAliasScalar(strings.TrimSpace(strings.TrimPrefix(line, "- match:")))
			continue
		}
		if match == "" {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(field) {
		case "keys":
			current.Keys = parseAliasList(value)
		case "display":
			current.Display = parseAliasScalar(value)
		case "emumovies":
			current.EmuMovies = parseAliasScalar(value)
		case "igdb":
			current.IGDB = parseAliasScalar(value)
		case "launchbox":
			current.LaunchBox = parseAliasScalar(value)
		case "openvgdb":
			current.OpenVGDB = parseAliasScalar(value)
		case "screenscraper":
			current.ScreenScraper = parseAliasScalar(value)
		case "screenscraper_id":
			current.ScreenScraperID = parseAliasScalar(value)
		}
	}
	flush()
	return entries
}

func setAlias(result map[string]aliasSet, value string, alias aliasSet, replace bool) {
	aliasKey := key(value)
	if aliasKey == "" {
		return
	}
	if _, ok := result[aliasKey]; ok && !replace {
		return
	}
	result[aliasKey] = alias
}

func setCLIAlias(result map[string]string, value, canonical string) {
	aliasKey := key(value)
	canonical = strings.TrimSpace(canonical)
	if aliasKey == "" || canonical == "" {
		return
	}
	if existing, ok := result[aliasKey]; ok && existing != canonical {
		return
	}
	result[aliasKey] = canonical
}

func (a aliasSet) sourceNames() []string {
	return []string{
		a.EmuMovies,
		a.IGDB,
		a.LaunchBox,
		a.OpenVGDB,
		a.ScreenScraper,
	}
}

func (a aliasSet) nameForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case SourceEmuMovies:
		return a.EmuMovies
	case SourceIGDB:
		return a.IGDB
	case SourceLaunchBox:
		return a.LaunchBox
	case SourceOpenVGDB:
		return a.OpenVGDB
	case SourceScreenScraper:
		return a.ScreenScraper
	default:
		return ""
	}
}

func registryFromAliasEntries(entries []aliasEntry) Registry {
	registry := NewRegistry()
	for _, entry := range entries {
		canonical := canonicalPlatformName(entry.Match)
		platform := registry.ensurePlatform(canonical)
		platform.Aliases = appendUniqueStrings(platform.Aliases, entry.Alias.Keys...)
		platform.Aliases = appendUniqueString(platform.Aliases, entry.Alias.Display)
		if entry.Match != canonical {
			platform.Aliases = appendUniqueString(platform.Aliases, entry.Match)
		}
		if platform.Sources == nil {
			platform.Sources = map[string]SourceRef{}
		}
		setRegistrySource(platform.Sources, SourceEmuMovies, entry.Alias.EmuMovies)
		setRegistrySource(platform.Sources, SourceIGDB, entry.Alias.IGDB)
		setRegistrySource(platform.Sources, SourceLaunchBox, entry.Alias.LaunchBox)
		setRegistrySource(platform.Sources, SourceOpenVGDB, entry.Alias.OpenVGDB)
		setRegistrySourceRef(platform.Sources, SourceScreenScraper, SourceRef{Name: entry.Alias.ScreenScraper, ID: entry.Alias.ScreenScraperID})
		registry.setPlatform(platform)
	}
	return registry
}

func setRegistrySource(sources map[string]SourceRef, source, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	setRegistrySourceRef(sources, source, SourceRef{Name: name})
}

func setRegistrySourceRef(sources map[string]SourceRef, source string, ref SourceRef) {
	ref.Name = strings.TrimSpace(ref.Name)
	ref.ID = strings.TrimSpace(ref.ID)
	if ref.empty() {
		return
	}
	sources[sourceKey(source)] = ref
}

func (r *Registry) ensurePlatform(name string) Platform {
	if r.platforms == nil {
		r.platforms = map[string]Platform{}
	}
	name = strings.TrimSpace(name)
	if platform, ok := r.platforms[key(name)]; ok {
		return clonePlatform(platform)
	}
	return Platform{Name: name, Sources: map[string]SourceRef{}}
}

func (r *Registry) setPlatform(platform Platform) {
	if r.platforms == nil {
		r.platforms = map[string]Platform{}
	}
	platform.Name = strings.TrimSpace(platform.Name)
	if platform.Name == "" {
		return
	}
	if platform.Sources == nil {
		platform.Sources = map[string]SourceRef{}
	}
	r.platforms[key(platform.Name)] = clonePlatform(platform)
}

func (r Registry) lookup(name string) (Platform, bool) {
	lookupKey := key(name)
	if lookupKey == "" {
		return Platform{}, false
	}
	if platform, ok := r.platforms[lookupKey]; ok {
		return clonePlatform(platform), true
	}
	// Explicit catalog aliases outrank provider names shared by several datasets.
	for _, sourceNames := range []bool{false, true} {
		var matched Platform
		found := false
		for _, platform := range r.platforms {
			matches := false
			if !sourceNames {
				for _, alias := range platform.Aliases {
					if key(alias) == lookupKey {
						matches = true
						break
					}
				}
			} else {
				for _, ref := range platform.Sources {
					if key(ref.Name) == lookupKey {
						matches = true
						break
					}
					for _, name := range ref.Names {
						if key(name) == lookupKey {
							matches = true
							break
						}
					}
					if matches {
						break
					}
				}
			}
			if !matches {
				continue
			}
			if found && key(matched.Name) != key(platform.Name) {
				return Platform{}, false
			}
			matched = platform
			found = true
		}
		if found {
			return clonePlatform(matched), true
		}
	}
	return Platform{}, false
}

func (r *Registry) addAliases(platform string, aliases []string) {
	entry := r.ensurePlatform(platform)
	entry.Aliases = appendUniqueStrings(entry.Aliases, aliases...)
	r.setPlatform(entry)
}

func (r *Registry) setSource(platform, source string, ref SourceRef) {
	entry := r.ensurePlatform(platform)
	if entry.Sources == nil {
		entry.Sources = map[string]SourceRef{}
	}
	entry.Sources[sourceKey(source)] = ref
	r.setPlatform(entry)
}

func clonePlatform(platform Platform) Platform {
	clone := Platform{
		Name:    platform.Name,
		Aliases: append([]string(nil), platform.Aliases...),
		Sources: map[string]SourceRef{},
	}
	for source, ref := range platform.Sources {
		clone.Sources[source] = SourceRef{
			Name:  ref.Name,
			Names: append([]string(nil), ref.Names...),
			ID:    ref.ID,
		}
	}
	return clone
}

func parseSourceRef(value string) SourceRef {
	value = strings.TrimSpace(value)
	if value == "" {
		return SourceRef{}
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		fields := parseInlineMap(value)
		ref := SourceRef{
			Name: parseAliasScalar(fields["name"]),
			ID:   parseAliasScalar(fields["id"]),
		}
		ref.Names = parseAliasList(fields["names"])
		return ref
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		names := parseAliasList(value)
		ref := SourceRef{Names: names}
		if len(names) > 0 {
			ref.Name = names[0]
		}
		return ref
	}
	return SourceRef{Name: parseAliasScalar(value)}
}

func (r SourceRef) empty() bool {
	return strings.TrimSpace(r.Name) == "" && strings.TrimSpace(r.ID) == "" && len(r.Names) == 0
}

func parseInlineMap(value string) map[string]string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "{")
	value = strings.TrimSuffix(value, "}")
	result := map[string]string{}
	for _, part := range splitInlineList(value) {
		key, raw, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(raw)
	}
	return result
}

func parseAliasList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	var result []string
	for _, part := range splitInlineList(value) {
		part = parseAliasScalar(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitInlineList(value string) []string {
	var result []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	depth := 0
	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			b.WriteRune(r)
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteRune(r)
		case (r == '[' || r == '{') && !inSingle && !inDouble:
			depth++
			b.WriteRune(r)
		case (r == ']' || r == '}') && !inSingle && !inDouble:
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case r == ',' && !inSingle && !inDouble && depth == 0:
			result = append(result, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	result = append(result, strings.TrimSpace(b.String()))
	return result
}

func trimRegistryComment(line string) string {
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

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func parseAliasScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
		value = strings.ReplaceAll(value, `\"`, `"`)
		value = strings.ReplaceAll(value, `\\`, `\`)
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	return strings.TrimSpace(value)
}

func canonicalPlatformName(name string) string {
	platform := strings.TrimSpace(name)
	for {
		start := strings.LastIndex(platform, "(")
		end := strings.LastIndex(platform, ")")
		if start < 0 || end != len(platform)-1 || start > end {
			break
		}
		value := strings.TrimSpace(platform[start+1 : end])
		if !isLayoutQualifier(value) {
			break
		}
		platform = strings.TrimSpace(platform[:start])
	}
	return platform
}

func isLayoutQualifier(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bigendian", "byte-swapped", "byteswapped", "littleendian", "decrypted", "encrypted", "headered", "unheadered":
		return true
	default:
		return false
	}
}

func stripKnownPrefix(name string) string {
	for _, prefix := range []string{"Atari - ", "Bandai - ", "Microsoft - ", "NEC - ", "Nintendo - ", "Panasonic - ", "Sega - ", "SNK - ", "Sony - "} {
		name = strings.TrimPrefix(name, prefix)
	}
	return name
}

func appendUniqueString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func appendUniqueStrings(values []string, next ...string) []string {
	for _, value := range next {
		values = appendUniqueString(values, value)
	}
	return values
}

func sourceKey(source string) string {
	return strings.ToLower(strings.TrimSpace(source))
}

func key(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "&", " and ")
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
