package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// Issue is a validation problem tied to a repository path.
type Issue struct {
	Path    string
	Message string
}

// ValidateOptions controls optional validation reporting.
type ValidateOptions struct {
	Progress ProgressFunc
}

// ProgressFunc receives validation progress updates.
type ProgressFunc func(ValidateProgress)

// ValidateProgress describes the current validation position.
type ValidateProgress struct {
	Stage    string
	Current  int
	Total    int
	Platform string
	Game     string
	Issues   int
}

// Validate checks the portable metadata platform layout rooted at root.
func Validate(root string) ([]Issue, error) {
	return ValidateWithOptions(root, ValidateOptions{})
}

// ValidateWithOptions checks the portable metadata platform layout rooted at
// root and optionally reports progress while walking large catalogs.
func ValidateWithOptions(root string, options ValidateOptions) ([]Issue, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	reportProgress(options.Progress, ValidateProgress{Stage: "scanning", Game: root})
	platformRoot := filepath.Join(root, "platforms")
	info, err := catalogfs.Stat(platformRoot)
	if err != nil {
		return nil, fmt.Errorf("stat platforms: %w", err)
	}
	if !info.IsDir() {
		return []Issue{{Path: rel(root, platformRoot), Message: "platforms is not a directory"}}, nil
	}

	var issues []Issue
	policies := loadSourcePolicies(root)
	platforms, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	totalGames := 0
	for _, platform := range platforms {
		if !platform.IsDir() || isHidden(platform.Name()) {
			continue
		}
		totalGames += countPlatformGames(filepath.Join(platformRoot, platform.Name()))
	}
	reportProgress(options.Progress, ValidateProgress{Stage: "validating", Total: totalGames})
	processedGames := 0
	for _, platform := range platforms {
		if !platform.IsDir() || isHidden(platform.Name()) {
			continue
		}
		validatePlatform(root, filepath.Join(platformRoot, platform.Name()), policies, &issues, options.Progress, &processedGames, totalGames)
	}
	validateArcadeBindings(root, &issues)
	return issues, nil
}

func countPlatformGames(platformPath string) int {
	games, err := catalogfs.ReadDir(platformPath)
	if err != nil {
		return 0
	}
	count := 0
	for _, game := range games {
		if game.IsDir() && !strings.HasPrefix(game.Name(), "_") && !isHidden(game.Name()) {
			count++
		}
	}
	return count
}

func validatePlatform(root, platformPath string, policies sourcePolicies, issues *[]Issue, progress ProgressFunc, processedGames *int, totalGames int) {
	name := filepath.Base(platformPath)
	if !isSafePathName(name) {
		addIssue(root, issues, platformPath, "platform folder has characters unsafe for common filesystems")
	}
	requireFile(root, issues, filepath.Join(platformPath, "_catalog.yaml"))
	requireFile(root, issues, filepath.Join(platformPath, "_platform.yaml"))

	games, err := catalogfs.ReadDir(platformPath)
	if err != nil {
		addIssue(root, issues, platformPath, err.Error())
		return
	}
	for _, game := range games {
		if !game.IsDir() || strings.HasPrefix(game.Name(), "_") || isHidden(game.Name()) {
			continue
		}
		(*processedGames)++
		reportProgress(progress, ValidateProgress{
			Stage:    "validating",
			Current:  *processedGames,
			Total:    totalGames,
			Platform: name,
			Game:     game.Name(),
			Issues:   len(*issues),
		})
		validateGame(root, filepath.Join(platformPath, game.Name()), policies, issues)
	}
}

func reportProgress(progress ProgressFunc, snapshot ValidateProgress) {
	if progress != nil {
		progress(snapshot)
	}
}

func validateGame(root, gamePath string, policies sourcePolicies, issues *[]Issue) {
	name := filepath.Base(gamePath)
	if !isSafePathName(name) {
		addIssue(root, issues, gamePath, "game folder has characters unsafe for common filesystems")
	}

	gameYAML := filepath.Join(gamePath, "game.yaml")
	requireFile(root, issues, gameYAML)
	game, err := parseGame(gameYAML)
	if err != nil {
		addIssue(root, issues, gameYAML, err.Error())
		return
	}
	if game.Title == "" {
		addIssue(root, issues, gameYAML, "missing top-level title")
	}
	if len(game.Releases) == 0 {
		addIssue(root, issues, gameYAML, "missing releases")
	}
	if game.DefaultRelease != "" && !game.Releases[game.DefaultRelease] {
		addIssue(root, issues, gameYAML, "default_release is not defined in releases")
	}
	for release := range game.Releases {
		if !isSafePathName(release) {
			addIssue(root, issues, gameYAML, fmt.Sprintf("release %q has characters unsafe for common filesystems", release))
		}
	}
	for _, reference := range game.SourceReleaseRefs {
		if reference.Release != "" && !game.Releases[reference.Release] {
			addIssue(root, issues, gameYAML, fmt.Sprintf("source %q references unknown release %q", reference.Source, reference.Release))
		}
	}

	mediaOwners := map[string]bool{}
	for name := range game.Releases {
		mediaOwners[name] = true
	}
	for name := range game.Variants {
		mediaOwners[name] = true
		if !isSafePathName(name) {
			addIssue(root, issues, gameYAML, fmt.Sprintf("variant %q has characters unsafe for common filesystems", name))
		}
	}
	validateMedia(root, filepath.Join(gamePath, "media"), mediaOwners, policies, issues)
	validateFingerprints(root, filepath.Join(gamePath, "fingerprints.yaml"), game.Root, issues)
}

func validateMedia(root, mediaPath string, releases map[string]bool, policies sourcePolicies, issues *[]Issue) {
	info, err := catalogfs.Stat(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		addIssue(root, issues, mediaPath, err.Error())
		return
	}
	if !info.IsDir() {
		addIssue(root, issues, mediaPath, "media is not a directory")
		return
	}

	sourceNames := map[string]bool{}
	validateMediaContents(root, mediaPath, releases, sourceNames, issues)

	if len(sourceNames) > 0 {
		if !policies.Exists {
			addIssue(root, issues, policies.Path, "missing required file")
			return
		}
		if policies.Err != nil {
			addIssue(root, issues, policies.Path, policies.Err.Error())
			return
		}
		for source := range sourceNames {
			if !policies.Sources[source] {
				addIssue(root, issues, policies.Path, fmt.Sprintf("missing source policy for source %q", source))
			}
		}
	}
}

// Fold preserves colliding media under _fold/<original-card>/ with its original
// game-wide or release-specific layout. The prefix is not a release identity.
func validateMediaContents(root, mediaPath string, releases map[string]bool, sourceNames map[string]bool, issues *[]Issue) {
	entries, err := catalogfs.ReadDir(mediaPath)
	if err != nil {
		addIssue(root, issues, mediaPath, err.Error())
		return
	}

	for _, entry := range entries {
		full := filepath.Join(mediaPath, entry.Name())
		if entry.IsDir() {
			if entry.Name() == "_fold" {
				validateFoldMedia(root, full, releases, sourceNames, issues)
				continue
			}
			if !releases[entry.Name()] {
				addIssue(root, issues, full, "media release folder does not match a release key")
				continue
			}
			validateMediaFiles(root, full, sourceNames, issues)
			continue
		}
		if isIgnorableMediaFile(entry.Name()) {
			continue
		}
		if !isAllowedMediaName(entry.Name()) {
			addIssue(root, issues, full, "unknown game-wide media filename")
		}
		if source := mediaSourceName(entry.Name()); source != "" {
			sourceNames[source] = true
		}
	}
}

func validateFoldMedia(root, dir string, releases map[string]bool, sourceNames map[string]bool, issues *[]Issue) {
	entries, err := catalogfs.ReadDir(dir)
	if err != nil {
		addIssue(root, issues, dir, err.Error())
		return
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		if !entry.IsDir() {
			addIssue(root, issues, full, "fold media namespace must contain original-card folders")
			continue
		}
		if !isSafePathName(entry.Name()) {
			addIssue(root, issues, full, "original-card folder has characters unsafe for common filesystems")
			continue
		}
		validateMediaContents(root, full, releases, sourceNames, issues)
	}
}

func validateMediaFiles(root, dir string, sourceNames map[string]bool, issues *[]Issue) {
	files, err := catalogfs.ReadDir(dir)
	if err != nil {
		addIssue(root, issues, dir, err.Error())
		return
	}
	for _, file := range files {
		full := filepath.Join(dir, file.Name())
		if file.IsDir() {
			addIssue(root, issues, full, "nested media directories are not part of the current convention")
			continue
		}
		if isIgnorableMediaFile(file.Name()) {
			continue
		}
		if !isAllowedMediaName(file.Name()) {
			addIssue(root, issues, full, "unknown media filename")
		}
		if source := mediaSourceName(file.Name()); source != "" {
			sourceNames[source] = true
		}
	}
}

type gameYAML struct {
	Title             string
	DefaultRelease    string
	Releases          map[string]bool
	SourceReleaseRefs []sourceReleaseRef
	Root              *yaml.Node
	Variants          map[string]bool
}

type sourceReleaseRef struct {
	Source  string
	Release string
}

func parseGame(path string) (gameYAML, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return gameYAML{}, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return gameYAML{}, err
	}
	root := document.Root()
	variants, err := catalogyaml.Variants(root)
	if err != nil {
		return gameYAML{}, err
	}
	game := gameYAML{
		Root: root, Variants: map[string]bool{},
		Title:          catalogyaml.Scalar(catalogyaml.Value(root, "title")),
		DefaultRelease: catalogyaml.Scalar(catalogyaml.Value(root, "default_release")),
		Releases:       map[string]bool{},
	}
	for _, variant := range variants {
		game.Variants[variant.Name] = true
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "releases"), func(key string, _ *yaml.Node) error {
		game.Releases[key] = true
		return nil
	}); err != nil {
		return gameYAML{}, err
	}
	if err := catalogyaml.Mapping(catalogyaml.Value(root, "sources"), func(source string, stanza *yaml.Node) error {
		return catalogyaml.Sequence(catalogyaml.Value(stanza, "matches"), func(match *yaml.Node) error {
			if release := catalogyaml.Scalar(catalogyaml.Value(match, "release")); release != "" {
				game.SourceReleaseRefs = append(game.SourceReleaseRefs, sourceReleaseRef{Source: source, Release: release})
			}
			releases := catalogyaml.Value(match, "releases")
			for _, release := range catalogyaml.Strings(releases) {
				game.SourceReleaseRefs = append(game.SourceReleaseRefs, sourceReleaseRef{Source: source, Release: release})
			}
			if releases != nil && releases.Kind == yaml.SequenceNode {
				for _, item := range releases.Content {
					if item.Kind != yaml.MappingNode {
						continue
					}
					if release := catalogyaml.Scalar(catalogyaml.Value(item, "release")); release != "" {
						game.SourceReleaseRefs = append(game.SourceReleaseRefs, sourceReleaseRef{Source: source, Release: release})
					}
				}
			}
			return nil
		})
	}); err != nil {
		return gameYAML{}, err
	}
	return game, nil
}

type sourcePolicies struct {
	Path    string
	Exists  bool
	Sources map[string]bool
	Err     error
}

func loadSourcePolicies(root string) sourcePolicies {
	path := filepath.Join(root, "source-policies.yaml")
	policies := sourcePolicies{Path: path, Sources: map[string]bool{}}
	if _, err := catalogfs.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return policies
		}
		policies.Err = err
		return policies
	}
	policies.Exists = true
	sources, err := parseSourcePolicies(path)
	policies.Sources = sources
	policies.Err = err
	return policies
}

func parseSourcePolicies(path string) (map[string]bool, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return nil, err
	}
	sources := map[string]bool{}
	if err := catalogyaml.Mapping(catalogyaml.Value(document.Root(), "sources"), func(source string, _ *yaml.Node) error {
		sources[source] = true
		return nil
	}); err != nil {
		return nil, err
	}
	return sources, nil
}

func requireFile(root string, issues *[]Issue, path string) {
	info, err := catalogfs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			addIssue(root, issues, path, "missing required file")
			return
		}
		addIssue(root, issues, path, err.Error())
		return
	}
	if info.IsDir() {
		addIssue(root, issues, path, "expected file, got directory")
	}
}

func addIssue(root string, issues *[]Issue, path, message string) {
	*issues = append(*issues, Issue{Path: rel(root, path), Message: message})
}

func rel(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isSafePathName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.TrimRight(name, " .") != name {
		return false
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}
	return utf8.ValidString(name)
}

func isIgnorableMediaFile(name string) bool {
	switch name {
	case ".gitkeep", "README.md":
		return true
	default:
		return false
	}
}

func isAllowedMediaName(name string) bool {
	if allowedImageMediaName.MatchString(name) {
		return true
	}
	if numberedMediaName.MatchString(name) {
		return true
	}
	if exactMediaName.MatchString(name) {
		return true
	}
	return sourceSpecificMediaName.MatchString(name)
}

func mediaSourceName(name string) string {
	if match := sourceSpecificMediaName.FindStringSubmatch(name); len(match) >= 2 {
		return match[1]
	}
	for _, expression := range []*regexp.Regexp{allowedImageMediaName, numberedMediaName, exactMediaName} {
		match := expression.FindStringSubmatch(name)
		if len(match) >= 3 {
			return match[2]
		}
	}
	return ""
}

var sourceSuffix = `[a-z0-9][a-z0-9_-]*`
var sourceSpecificMediaName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(?:-[0-9]{2})?(?:\.` + sourceSuffix + `)*\.(` + sourceSuffix + `)\.(png|jpg|jpeg|webp|pdf|mp4)$`)
var allowedImageMediaName = regexp.MustCompile(`^(box-front|box-back|box-spine|box-full|box-3d|logo|banner|title-screen|fanart|background|physical-media|disc|cart-front|cart-back|cart-top|cart-3d|poster|flyer-front|flyer-back|marquee)(?:\.(` + sourceSuffix + `))?\.(png|jpg|jpeg|webp)$`)
var numberedMediaName = regexp.MustCompile(`^(screenshot|gameplay)-[0-9]{2}(?:\.(` + sourceSuffix + `))?\.(png|jpg|jpeg|webp)$`)
var exactMediaName = regexp.MustCompile(`^(manual|video)(?:\.(` + sourceSuffix + `))?\.(pdf|mp4)$`)
