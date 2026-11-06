package platformnames

import (
	"fmt"
	"sort"
	"strings"
)

// ResolutionError means a provider list was read successfully, but did not
// establish a unique platform identity. It is not a network or game-match error.
type ResolutionError struct {
	Source    string
	Platform  string
	Ambiguous bool
}

func (e *ResolutionError) Error() string {
	reason := "No matching platform"
	if e.Ambiguous {
		reason = "Several matching platforms"
	}
	return fmt.Sprintf("%s in %s's platform list for %q. Review this source in Settings > Platform mappings (config/platforms.yaml). No game searches or not_found records were made for this platform.", reason, e.Source, e.Platform)
}

// Discover matches provider names without assuming that a missing built-in
// mapping means unsupported. An explicit source name takes precedence and may
// not silently fall back to another system. Hardware qualifiers stay significant.
func (r Registry) Discover(name, source string, available []string) (string, error) {
	candidates := r.CandidatesForSource(name, source)
	if platform, ok := r.Entry(name); ok {
		ref := platform.Sources[sourceKey(source)]
		if ref.Name != "" || len(ref.Names) > 0 {
			candidates = appendUniqueStrings(nil, append([]string{ref.Name}, ref.Names...)...)
		}
	}
	matches := map[string]bool{}
	for _, candidate := range candidates {
		for _, value := range available {
			if discoveryKey(candidate) != "" && discoveryKey(candidate) == discoveryKey(value) {
				matches[value] = true
			}
		}
	}
	if len(matches) == 1 {
		for value := range matches {
			return value, nil
		}
	}
	return "", &ResolutionError{Source: source, Platform: name, Ambiguous: len(matches) > 1}
}

func discoveryKey(name string) string {
	return strings.ReplaceAll(key(strings.ReplaceAll(name, "+", " plus ")), " ", "")
}

// DiscoverForSource uses the caller's overrides, or the embedded aliases.
func DiscoverForSource(name, source string, available []string, registry *Registry) (string, error) {
	if registry == nil {
		registry = &defaultRegistry
	}
	return registry.Discover(name, source, available)
}

// WithSourceName keeps an automatically resolved provider name local to this
// operation; it never rewrites the owner's platform configuration.
func (r Registry) WithSourceName(name, source, resolved string) Registry {
	next := r.Clone()
	next.setSource(r.ResolveCLI(name), source, SourceRef{Name: resolved})
	return next
}

// discoveryCandidates is deterministic even when other providers supply aliases.
func discoveryCandidates(refs map[string]SourceRef) []string {
	sources := make([]string, 0, len(refs))
	for source := range refs {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	var result []string
	for _, source := range sources {
		ref := refs[source]
		result = appendUniqueString(result, ref.Name)
		result = appendUniqueStrings(result, ref.Names...)
	}
	return result
}

// A DAT's "Manufacturer - System" convention does not require a manufacturer
// allowlist. Only the separator is removed; suffixes such as Color, CD or DD stay.
func datSystemName(name string) string {
	_, system, ok := strings.Cut(name, " - ")
	if ok {
		return strings.TrimSpace(system)
	}
	return ""
}
