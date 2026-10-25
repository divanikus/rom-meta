package sourceaudit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/taskprogress"
)

// CoverageOptions controls source coverage reporting.
type CoverageOptions struct {
	DataRoot string
	Platform string
	Source   string
	LowScore int
	Details  bool
	Progress taskprogress.Func
}

// CoverageReport summarizes source candidate coverage across a data root.
type CoverageReport struct {
	DataRoot       string
	PlatformFilter string
	SourceFilter   string
	PlatformCount  int
	GameCount      int
	Sources        []SourceCoverage
	Details        bool
	LowScore       int
}

// SourceCoverage summarizes one source stanza across scanned games.
type SourceCoverage struct {
	Source               string
	GameCount            int
	CoveredGameCount     int
	MatchedGameCount     int
	MissingGameCount     int
	MatchCount           int
	StaleMatchCount      int
	LowConfidenceCount   int
	MissingGames         []GameRef
	StaleMatches         []StaleMatch
	LowConfidenceMatches []LowConfidenceMatch
}

// GameRef identifies one game bundle in a report.
type GameRef struct {
	Platform string
	Game     string
	Title    string
	Path     string
}

// StaleMatch is a source match whose release does not exist in the game bundle.
type StaleMatch struct {
	GameRef
	Release string
	Source  string
}

// LowConfidenceMatch is a scored source match below the configured threshold.
type LowConfidenceMatch struct {
	GameRef
	Release string
	Source  string
	Score   int
}

type catalogGame struct {
	GameRef
	Releases map[string]bool
	Sources  map[string]sourceRecord
}

type sourceRecord struct {
	Matches []sourceMatch
}

type sourceMatch struct {
	Releases []string
	Score    int
	HasScore bool
}

// Coverage scans game.yaml files and reports source stanza coverage.
func Coverage(options CoverageOptions) (CoverageReport, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return CoverageReport{}, err
	}
	if options.LowScore == 0 {
		options.LowScore = 120
	}

	sources, platformCount, gameCount, err := selectedSources(dataRoot, options.Platform, options.Source, options.Progress)
	if err != nil {
		return CoverageReport{}, err
	}
	report := CoverageReport{
		DataRoot:       dataRoot,
		PlatformFilter: strings.TrimSpace(options.Platform),
		SourceFilter:   strings.TrimSpace(options.Source),
		PlatformCount:  platformCount,
		GameCount:      gameCount,
		Details:        options.Details,
		LowScore:       options.LowScore,
	}
	coverageBySource := map[string]*SourceCoverage{}
	for _, source := range sources {
		coverageBySource[source] = &SourceCoverage{Source: source, GameCount: gameCount}
	}
	if len(sources) > 0 {
		_, _, err = walkCatalogGames(dataRoot, options.Platform, "checking coverage", options.Progress, func(game catalogGame) error {
			for _, source := range sources {
				addSourceCoverage(coverageBySource[source], game, source, options.LowScore, options.Details)
			}
			return nil
		})
		if err != nil {
			return CoverageReport{}, err
		}
	}
	for _, source := range sources {
		report.Sources = append(report.Sources, *coverageBySource[source])
	}
	return report, nil
}

func selectedSources(dataRoot, platformFilter, source string, progress taskprogress.Func) ([]string, int, int, error) {
	source = strings.TrimSpace(source)
	if source != "" {
		platformCount, gameCount, err := walkCatalogGames(dataRoot, platformFilter, "counting games", progress, nil)
		return []string{source}, platformCount, gameCount, err
	}
	found := map[string]bool{}
	platformCount, gameCount, err := walkCatalogGames(dataRoot, platformFilter, "finding sources", progress, func(game catalogGame) error {
		for name := range game.Sources {
			found[name] = true
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	var sources []string
	for name := range found {
		sources = append(sources, name)
	}
	sort.Strings(sources)
	return sources, platformCount, gameCount, nil
}

func addSourceCoverage(result *SourceCoverage, game catalogGame, source string, lowScore int, details bool) {
	record, ok := game.Sources[source]
	if !ok {
		result.MissingGameCount++
		if details {
			result.MissingGames = append(result.MissingGames, game.GameRef)
		}
		return
	}

	result.CoveredGameCount++
	if len(record.Matches) > 0 {
		result.MatchedGameCount++
	}
	result.MatchCount += len(record.Matches)
	for _, match := range record.Matches {
		for _, release := range match.Releases {
			if release == "" || game.Releases[release] {
				continue
			}
			result.StaleMatchCount++
			if details {
				result.StaleMatches = append(result.StaleMatches, StaleMatch{
					GameRef: game.GameRef,
					Release: release,
					Source:  source,
				})
			}
		}
		if match.HasScore && match.Score < lowScore {
			result.LowConfidenceCount++
			if details {
				result.LowConfidenceMatches = append(result.LowConfidenceMatches, LowConfidenceMatch{
					GameRef: game.GameRef,
					Release: firstNonEmpty(match.Releases...),
					Source:  source,
					Score:   match.Score,
				})
			}
		}
	}
}

func walkCatalogGames(dataRoot, platformFilter, stage string, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	platformRoot := filepath.Join(dataRoot, "platforms")
	platforms, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return 0, 0, err
	}

	platformCount := 0
	totalGames := 0
	type platformEntry struct {
		name    string
		path    string
		entries []os.DirEntry
	}
	var selectedPlatforms []platformEntry
	for _, platform := range platforms {
		if !platform.IsDir() || strings.HasPrefix(platform.Name(), ".") {
			continue
		}
		if platformFilter != "" && platform.Name() != platformFilter {
			continue
		}
		platformCount++
		platformPath := filepath.Join(platformRoot, platform.Name())
		entries, err := catalogfs.ReadDir(platformPath)
		if err != nil {
			return 0, 0, err
		}
		selectedPlatforms = append(selectedPlatforms, platformEntry{name: platform.Name(), path: platformPath, entries: entries})
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || catalogrules.IsIgnoredGameFolder(entry.Name()) {
				continue
			}
			totalGames++
		}
	}

	current := 0
	for _, platform := range selectedPlatforms {
		for _, entry := range platform.entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || catalogrules.IsIgnoredGameFolder(entry.Name()) {
				continue
			}
			current++
			label := platform.name + " / " + entry.Name()
			progress.Update(stage, current, totalGames, label)
			if fn == nil {
				continue
			}
			gamePath := filepath.Join(platform.path, entry.Name())
			game, err := readGameYAML(filepath.Join(gamePath, "game.yaml"))
			if err != nil {
				return 0, 0, err
			}
			game.Platform = platform.name
			game.Game = entry.Name()
			game.Path = gamePath
			if err := fn(game); err != nil {
				return 0, 0, err
			}
		}
	}
	return platformCount, totalGames, nil
}

func parseMatchField(value string, match *sourceMatch) {
	key, raw, ok := splitKV(value)
	if !ok {
		return
	}
	switch key {
	case "release":
		match.Releases = appendUniqueString(match.Releases, parseScalar(raw))
	case "releases":
		for _, release := range parseInlineList(raw) {
			match.Releases = appendUniqueString(match.Releases, release)
		}
	case "match_score":
		score, err := strconv.Atoi(parseScalar(raw))
		if err == nil {
			match.Score = score
			match.HasScore = true
		}
	}
}

func splitKV(value string) (string, string, bool) {
	key, raw, ok := strings.Cut(value, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(raw), true
}

func trimComment(line string) string {
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

func parseScalar(value string) string {
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

func parseInlineList(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil
	}
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return nil
	}
	var result []string
	for _, part := range splitInlineList(value) {
		parsed := parseScalar(part)
		if parsed != "" {
			result = appendUniqueString(result, parsed)
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
		case r == ',' && !inSingle && !inDouble:
			result = append(result, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	result = append(result, strings.TrimSpace(b.String()))
	return result
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func CoveragePercent(covered, total int) string {
	if total == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(covered)*100/float64(total))
}
