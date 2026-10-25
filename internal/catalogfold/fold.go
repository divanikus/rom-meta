package catalogfold

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogrules"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/contentidentity"
	"rommeta/internal/releasepolicy"
	"rommeta/internal/taskprogress"
)

const minimumStrongMatchScore = 140

// Options controls post-metadata catalog folding.
type Options struct {
	DataRoot               string
	Platform               string
	Match                  string
	Apply                  bool
	EditionsOnly           bool
	ReviewPlan             string
	Review                 ReviewFunc
	RegionPriority         []string
	PlatformRegionPriority map[string][]string
	RegionGroups           map[string][]string
	Progress               taskprogress.Func
	OnPlatformStart        func(index, total int, platform string)
	OnPlatformResult       func(PlatformResult)
}

// ReviewFunc asks a caller how to handle one fold candidate.
type ReviewFunc func(ReviewPrompt) (ReviewDecision, error)

// ReviewPrompt describes one candidate shown during interactive review.
type ReviewPrompt struct {
	Index int
	Total int
	Group Group
}

// ReviewDecision is the selected action for one interactive fold candidate.
type ReviewDecision int

const (
	ReviewSkip ReviewDecision = iota
	ReviewApply
	ReviewIgnoreEvidence
	ReviewQuit
)

// Result describes a dry-run or applied catalog fold pass.
type Result struct {
	DataRoot            string
	PlatformFilter      string
	ReviewPlan          string
	Match               string
	PlatformCount       int
	GameCount           int
	IdentityCount       int
	CandidateGroupCount int
	AppliedGroupCount   int
	SkippedGroupCount   int
	MergedGameCount     int
	RenamedGameCount    int
	Backups             []string
	ReleaseCount        int
	FingerprintCount    int
	MovedFileCount      int
	ReviewedGroupCount  int
	IgnoredGroupCount   int
	IgnoredFileCount    int
	HeldGroupCount      int
	ReviewRequiredCount int
	ReviewSkippedCount  int
	Groups              []Group
	PlatformResults     []PlatformResult
	Apply               bool
	Interactive         bool
	ReviewStopped       bool
	RegionPriority      []string
}

// PlatformResult describes fold work for one platform.
type PlatformResult struct {
	Platform            string
	GameCount           int
	IdentityCount       int
	CandidateGroupCount int
	AppliedGroupCount   int
	SkippedGroupCount   int
	MergedGameCount     int
	RenamedGameCount    int
	Backups             []string
	ReleaseCount        int
	FingerprintCount    int
	MovedFileCount      int
	ReviewedGroupCount  int
	IgnoredGroupCount   int
	IgnoredFileCount    int
	HeldGroupCount      int
	ReviewRequiredCount int
	ReviewSkippedCount  int
	ReviewStopped       bool
}

// Group is one proposed or applied game-bundle merge.
type Group struct {
	Platform          string
	Keep              GameRef
	TargetFolder      string
	Merge             []GameRef
	Evidence          []Evidence
	ReleaseCount      int
	FingerprintCount  int
	MovedFileCount    int
	IgnoreFileCount   int
	Applied           bool
	Ignored           bool
	Skipped           bool
	SkipReason        string
	ReviewReason      string
	DevelopmentParent string
	IdentityReview    *ReviewedIdentity
	HoldReason        string
	ReviewRequired    string
	ReviewAccepted    bool
	FileChanges       []FileChange
}

// GameRef identifies one catalog game bundle.
type GameRef struct {
	Folder       string
	Title        string
	ReleaseCount int
	SourceCount  int
	Regions      []string
}

// Evidence explains why games can be folded together.
type Evidence struct {
	Source string
	Field  string
	Value  string
	URL    string
	Games  []string
}

type catalogGame struct {
	ContentKey           string
	Content              []contentidentity.Profile
	HasRomsetBindings    bool
	Platform             string
	Folder               string
	Path                 string
	Title                string
	DefaultRelease       string
	Releases             []block
	SourceBlocks         map[string]sourceBlock
	SourceCount          int
	Evidence             []Evidence
	IgnoredEvidence      []string
	Relationships        []sourceRelationship
	RelationshipIDs      []Evidence
	EditionBase          bool
	Aliases              []string
	AlternateTitles      []string
	Regions              []string
	SourceAliases        []sourceAlias
	AliasHoldReasons     []string
	RedumpSerials        []string
	IncompleteSerials    bool
	PS3Serials           []string
	IncompletePS3Serials bool
	FingerprintItems     []string
	FingerprintCount     int
}

type block struct {
	Key  string
	Text string
}

type sourceBlock struct {
	Name        string
	Raw         string
	HasMatches  bool
	MatchBlocks []string
}

// Fold finds game bundles that share strong source IDs and optionally merges them.
func Fold(options Options) (Result, error) {
	root := strings.TrimSpace(options.DataRoot)
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		DataRoot:       root,
		ReviewPlan:     options.ReviewPlan,
		PlatformFilter: strings.TrimSpace(options.Platform),
		Match:          strings.TrimSpace(options.Match),
		Apply:          options.Apply,
		Interactive:    options.Review != nil,
		RegionPriority: normalizedRegionPriority(options.RegionPriority),
	}
	defaultRegionRules := newRegionRules(result.RegionPriority, options.RegionGroups)
	platformRegionRules := newPlatformRegionRules(options.PlatformRegionPriority, options.RegionGroups)

	var platforms []string
	var reviewedGroups map[string][]Group
	var reviewPlan ReviewPlan
	if options.ReviewPlan != "" {
		if options.Platform != "" || options.Match != "" || options.EditionsOnly || options.Review != nil {
			return Result{}, fmt.Errorf("review plan cannot be combined with platform, match, editions-only or interactive review")
		}
		reviewPlan, err = readReviewPlan(options.ReviewPlan)
		if err == nil {
			platforms, reviewedGroups, err = prepareReviewPlan(root, reviewPlan, options, defaultRegionRules, platformRegionRules)
		}
	} else {
		platforms, err = catalogPlatforms(root, result.PlatformFilter, options.Progress)
	}
	if err != nil {
		return Result{}, err
	}
	result.PlatformCount = len(platforms)

	stopReview := false
	for platformIndex, platform := range platforms {
		if options.OnPlatformStart != nil {
			options.OnPlatformStart(platformIndex+1, len(platforms), platform)
		}
		groups := reviewedGroups[platform]
		var mame *mameFoldPlan
		if reviewedGroups == nil && platform == "Arcade" {
			mame, err = prepareMAMEFold(root, options)
			if err != nil {
				return Result{}, err
			}
			defer mame.close()
		}
		gameCount := 0
		if reviewedGroups != nil {
			for _, group := range groups {
				gameCount += 1 + len(group.Merge)
			}
		} else {
			games, err := readPlatformCatalog(root, platform, options.Progress)
			if err != nil {
				return Result{}, err
			}
			gameCount = len(games)
			if mame != nil {
				unbound := games[:0]
				for _, game := range games {
					if !game.HasRomsetBindings {
						unbound = append(unbound, game)
					}
				}
				games = unbound
			}
			options.Progress.Update("matching", 0, gameCount, "building candidate groups")
			games = withEditionEvidence(games, options.EditionsOnly)
			if !options.EditionsOnly {
				games, err = withPS3DiscFamilies(games)
				if err != nil {
					return Result{}, err
				}
				games = withRegionalAliasEvidence(games)
				games = withXboxSerialHints(games)
			}
			groups = filterGroups(buildGroups(games, defaultRegionRules, platformRegionRules), result.Match)
			options.Progress.Update("matching", gameCount, gameCount, fmt.Sprintf("%d candidate groups", len(groups)))
		}
		platformResult := PlatformResult{Platform: platform, GameCount: gameCount}
		result.GameCount += gameCount
		platformResult.CandidateGroupCount = len(groups)
		for index := range groups {
			group := &groups[index]
			if group.HoldReason != "" {
				platformResult.HeldGroupCount++
			} else {
				options.Progress.Update("checking files", index+1, len(groups), platform+" / "+group.Keep.Folder)
				changes, reason, err := planGroupFiles(root, *group)
				if err != nil {
					return Result{}, err
				}
				group.FileChanges = changes
				if reason != "" {
					group.HoldReason = reason
					platformResult.HeldGroupCount++
				}
			}
			if group.HoldReason == "" && group.ReviewRequired != "" {
				platformResult.ReviewRequiredCount++
			}
			platformResult.IdentityCount += len(group.Evidence)
			platformResult.MergedGameCount += len(group.Merge)
			platformResult.ReleaseCount += group.ReleaseCount
			platformResult.FingerprintCount += group.FingerprintCount
		}
		mutationRoot := root
		var transaction *platformTransaction
		startTransaction := func() error {
			if transaction != nil {
				return nil
			}
			transaction, err = beginPlatformTransaction(root, platform)
			if err != nil {
				return err
			}
			mutationRoot = transaction.root
			if reviewedGroups != nil {
				if err := verifyReviewedPlatform(mutationRoot, platform, reviewPlan); err != nil {
					return err
				}
			}
			return nil
		}
		// Interactive skip/quit and blocked groups must not copy an entire
		// platform before the user has accepted any mutation.
		defer func() {
			if transaction != nil {
				transaction.abort()
			}
		}()
		if options.Apply && platformResult.HeldGroupCount+platformResult.ReviewRequiredCount < len(groups) {
			if err := startTransaction(); err != nil {
				return Result{}, err
			}
		}

		switch {
		case options.Review != nil:
			for index := range groups {
				decision, err := options.Review(ReviewPrompt{Index: index + 1, Total: len(groups), Group: groups[index]})
				if err != nil {
					return Result{}, err
				}
				if decision == ReviewQuit {
					platformResult.ReviewStopped = true
					result.ReviewStopped = true
					stopReview = true
					break
				}
				platformResult.ReviewedGroupCount++
				switch decision {
				case ReviewApply:
					if groups[index].HoldReason == "" {
						if err := startTransaction(); err != nil {
							return Result{}, err
						}
					}
					groups[index].ReviewAccepted = true
					options.Progress.Update("applying", index+1, len(groups), groups[index].Platform+" / "+groups[index].Keep.Folder)
					if err := applyGroup(mutationRoot, &groups[index], rulesForPlatform(defaultRegionRules, platformRegionRules, groups[index].Platform)); err != nil {
						return Result{}, err
					}
					if groups[index].Skipped {
						platformResult.SkippedGroupCount++
					} else {
						platformResult.AppliedGroupCount++
						platformResult.MovedFileCount += groups[index].MovedFileCount
					}
				case ReviewIgnoreEvidence:
					if err := startTransaction(); err != nil {
						return Result{}, err
					}
					options.Progress.Update("writing ignore", index+1, len(groups), groups[index].Platform+" / "+groups[index].Keep.Folder)
					if err := ignoreGroupEvidence(mutationRoot, &groups[index]); err != nil {
						return Result{}, err
					}
					platformResult.IgnoredGroupCount++
					platformResult.IgnoredFileCount += groups[index].IgnoreFileCount
				default:
					platformResult.ReviewSkippedCount++
				}
			}
		case options.Apply:
			for index := range groups {
				options.Progress.Update("applying", index+1, len(groups), groups[index].Platform+" / "+groups[index].Keep.Folder)
				if err := applyGroup(mutationRoot, &groups[index], rulesForPlatform(defaultRegionRules, platformRegionRules, groups[index].Platform)); err != nil {
					return Result{}, err
				}
				if groups[index].Skipped {
					platformResult.SkippedGroupCount++
				} else {
					platformResult.AppliedGroupCount++
					platformResult.MovedFileCount += groups[index].MovedFileCount
				}
			}
		}
		if transaction != nil && (platformResult.AppliedGroupCount > 0 || platformResult.IgnoredGroupCount > 0) {
			options.Progress.Update("committing", 1, 1, platform)
			if reviewedGroups != nil {
				if err := verifyReviewedPlatform(root, platform, reviewPlan); err != nil {
					return Result{}, err
				}
			}
			if err := transaction.commit(); err != nil {
				return Result{}, err
			}
			platformResult.Backups = append(platformResult.Backups, transaction.backupPath)
		}
		if err := mame.finish(root, options, &platformResult, &groups); err != nil {
			return Result{}, err
		}

		result.IdentityCount += platformResult.IdentityCount
		result.CandidateGroupCount += platformResult.CandidateGroupCount
		result.MergedGameCount += platformResult.MergedGameCount
		result.RenamedGameCount += platformResult.RenamedGameCount
		result.Backups = append(result.Backups, platformResult.Backups...)
		result.ReleaseCount += platformResult.ReleaseCount
		result.FingerprintCount += platformResult.FingerprintCount
		result.AppliedGroupCount += platformResult.AppliedGroupCount
		result.SkippedGroupCount += platformResult.SkippedGroupCount
		result.MovedFileCount += platformResult.MovedFileCount
		result.ReviewedGroupCount += platformResult.ReviewedGroupCount
		result.IgnoredGroupCount += platformResult.IgnoredGroupCount
		result.IgnoredFileCount += platformResult.IgnoredFileCount
		result.HeldGroupCount += platformResult.HeldGroupCount
		result.ReviewRequiredCount += platformResult.ReviewRequiredCount
		result.ReviewSkippedCount += platformResult.ReviewSkippedCount
		result.Groups = append(result.Groups, groups...)
		result.PlatformResults = append(result.PlatformResults, platformResult)
		if options.OnPlatformResult != nil {
			options.OnPlatformResult(platformResult)
		}
		if stopReview {
			break
		}
	}
	return result, nil
}

type platformTransaction struct {
	root         string
	originalPath string
	stageRoot    string
	stagePath    string
	backupPath   string
}

func beginPlatformTransaction(root, platform string) (*platformTransaction, error) {
	originalPath := filepath.Join(root, "platforms", platform)
	runtimePath := filepath.Join(root, ".rommeta")
	if err := catalogfs.MkdirAll(runtimePath, 0o755); err != nil {
		return nil, err
	}
	stageRoot, err := os.MkdirTemp(runtimePath, "fold-")
	if err != nil {
		return nil, err
	}
	transaction := &platformTransaction{
		root:         stageRoot,
		originalPath: originalPath,
		stageRoot:    stageRoot,
		stagePath:    filepath.Join(stageRoot, "platforms", platform),
	}
	if err := clonePlatformForFold(originalPath, transaction.stagePath); err != nil {
		transaction.abort()
		return nil, err
	}
	return transaction, nil
}

func (transaction *platformTransaction) commit() error {
	backupPath, err := reserveFoldBackupPath(transaction.originalPath)
	if err != nil {
		return err
	}
	if err := renameFoldDirectory(transaction.originalPath, backupPath); err != nil {
		return err
	}
	if err := renameFoldDirectory(transaction.stagePath, transaction.originalPath); err != nil {
		if rollbackErr := renameFoldDirectory(backupPath, transaction.originalPath); rollbackErr != nil {
			return fmt.Errorf("activate folded platform: %v; restore original from %s: %w", err, backupPath, rollbackErr)
		}
		return err
	}
	// Retain the original platform, including downloaded media, for recovery.
	transaction.backupPath = backupPath
	return nil
}

func (transaction *platformTransaction) abort() {
	if transaction == nil {
		return
	}
	_ = catalogfs.RemoveAll(transaction.stageRoot)
}

func clonePlatformForFold(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return catalogfs.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("cannot stage non-regular catalog file: %s", path)
		}
		if err := catalogfs.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if filepath.Base(path) != "game.yaml" && filepath.Base(path) != "fingerprints.yaml" {
			if err := catalogfs.Link(path, destination); err == nil {
				return nil
			}
		}
		return copyFoldFile(path, destination)
	})
}

func copyFoldFile(source, target string) error {
	input, err := catalogfs.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := catalogfs.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return catalogfs.Chtimes(target, info.ModTime(), info.ModTime())
}

func reserveFoldBackupPath(platformPath string) (string, error) {
	backupRoot := filepath.Join(filepath.Dir(filepath.Dir(platformPath)), "backups")
	if err := catalogfs.MkdirAll(backupRoot, 0o755); err != nil {
		return "", fmt.Errorf("create fold backup directory: %w", err)
	}
	reserved, err := os.MkdirTemp(backupRoot, filepath.Base(platformPath)+".fold-backup-")
	if err != nil {
		return "", err
	}
	if err := catalogfs.Remove(reserved); err != nil {
		return "", err
	}
	return reserved, nil
}

func catalogPlatforms(root, platformFilter string, progress taskprogress.Func) ([]string, error) {
	platformRoot := filepath.Join(root, "platforms")
	entries, err := catalogfs.ReadDir(platformRoot)
	if err != nil {
		return nil, err
	}
	var platforms []string
	for index, entry := range entries {
		progress.Update("scanning platforms", index+1, len(entries), entry.Name())
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if platformFilter != "" && entry.Name() != platformFilter {
			continue
		}
		platforms = append(platforms, entry.Name())
	}
	sort.Strings(platforms)
	return platforms, nil
}

func readPlatformCatalog(root, platform string, progress taskprogress.Func) ([]catalogGame, error) {
	platformPath := filepath.Join(root, "platforms", platform)
	entries, err := catalogfs.ReadDir(platformPath)
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || catalogrules.IsIgnoredGameFolder(entry.Name()) {
			continue
		}
		folders = append(folders, entry.Name())
	}
	sort.Strings(folders)
	games := make([]catalogGame, 0, len(folders))
	for index, folder := range folders {
		progress.Update("reading", index+1, len(folders), platform+" / "+folder)
		game, err := readGameSummary(catalogGame{
			Platform: platform,
			Folder:   folder,
			Path:     filepath.Join(platformPath, folder),
		})
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	return games, nil
}

func readGameYAML(game catalogGame) (catalogGame, error) {
	contents, err := catalogfs.ReadFile(filepath.Join(game.Path, "game.yaml"))
	if err != nil {
		return catalogGame{}, err
	}
	parsed, err := parseGameYAML(contents)
	if err != nil {
		return catalogGame{}, fmt.Errorf("parse %s: %w", filepath.Join(game.Path, "game.yaml"), err)
	}
	parsed.Platform = game.Platform
	parsed.Folder = game.Folder
	parsed.Path = game.Path
	parsed.SourceCount = len(parsed.SourceBlocks)
	return parsed, nil
}

func readGameSummary(game catalogGame) (catalogGame, error) {
	parsed, err := readGameYAML(game)
	if err != nil {
		return catalogGame{}, err
	}
	fingerprintCount, err := countFingerprintItems(filepath.Join(game.Path, "fingerprints.yaml"))
	if err != nil {
		return catalogGame{}, err
	}
	parsed.FingerprintCount = fingerprintCount
	parsed.SourceBlocks = nil
	return parsed, nil
}

func readGameFull(game catalogGame) (catalogGame, error) {
	parsed, err := readGameYAML(game)
	if err != nil {
		return catalogGame{}, err
	}
	items, err := readFingerprintItems(filepath.Join(game.Path, "fingerprints.yaml"))
	if err != nil {
		return catalogGame{}, err
	}
	parsed.FingerprintItems = items
	parsed.FingerprintCount = len(items)
	return parsed, nil
}

func countFingerprintItems(path string) (int, error) {
	items, err := readFingerprintSequence(path)
	if err != nil || items == nil {
		return 0, err
	}
	return len(items.Content), nil
}

func buildGroups(games []catalogGame, defaultRegions regionRules, platformRegions map[string]regionRules) []Group {
	parent := make([]int, len(games))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	identityGames := map[string][]int{}
	identityEvidence := map[string]Evidence{}
	for index, game := range games {
		for _, evidence := range game.Evidence {
			key := evidenceKey(evidence)
			identityGames[key] = appendUniqueIndex(identityGames[key], index)
			record := identityEvidence[key]
			if record.Source == "" {
				record = evidence
			}
			record.Games = appendUniqueString(record.Games, game.Folder)
			identityEvidence[key] = record
		}
	}
	for _, indexes := range identityGames {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes[1:] {
			union(indexes[0], index)
		}
	}

	groupIndexes := map[int][]int{}
	for index := range games {
		root := find(index)
		groupIndexes[root] = append(groupIndexes[root], index)
	}
	var groups []Group
	for _, indexes := range groupIndexes {
		if len(indexes) < 2 {
			continue
		}
		sort.Slice(indexes, func(i, j int) bool {
			return games[indexes[i]].Folder < games[indexes[j]].Folder
		})
		regions := rulesForPlatform(defaultRegions, platformRegions, games[indexes[0]].Platform)
		keepIndex := chooseKeep(games, indexes, regions)
		group := Group{
			Platform: games[keepIndex].Platform,
			Keep:     gameRef(games[keepIndex]),
		}
		for _, index := range indexes {
			if index == keepIndex {
				continue
			}
			group.Merge = append(group.Merge, gameRef(games[index]))
			group.ReleaseCount += len(games[index].Releases)
			group.FingerprintCount += games[index].FingerprintCount
		}
		group.Evidence = groupEvidence(games, indexes, identityGames, identityEvidence)
		group.HoldReason = identityGroupHoldReason(games, indexes)
		if reason := ps3FamilyGroupHoldReason(games, indexes); reason != "" {
			group.HoldReason = strings.Trim(strings.Join([]string{group.HoldReason, reason}, "; "), "; ")
		}
		if serialHintsRequired(games, indexes) {
			group.ReviewRequired = "shared Xbox 360 Redump serial is only a discovery hint; confirm regional titles, edition and content with --interactive"
			if conflicts := groupDirectIDConflicts(games, indexes); len(conflicts) > 0 {
				group.HoldReason = strings.Trim(strings.Join(append([]string{group.HoldReason}, conflicts...), "; "), "; ")
			}
		}
		sort.Slice(group.Evidence, func(i, j int) bool {
			a, b := group.Evidence[i], group.Evidence[j]
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			if a.Field != b.Field {
				return a.Field < b.Field
			}
			return a.Value < b.Value
		})
		if groupHasNumberedCollisionFolder(group) {
			continue
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Platform != groups[j].Platform {
			return groups[i].Platform < groups[j].Platform
		}
		return groups[i].Keep.Folder < groups[j].Keep.Folder
	})
	return groups
}

func groupHasNumberedCollisionFolder(group Group) bool {
	if hasNumberedCollisionSuffix(group.Keep.Folder) {
		return true
	}
	for _, ref := range group.Merge {
		if hasNumberedCollisionSuffix(ref.Folder) {
			return true
		}
	}
	return false
}

func hasNumberedCollisionSuffix(folder string) bool {
	folder = strings.TrimSpace(folder)
	if !strings.HasSuffix(folder, "]") {
		return false
	}
	open := strings.LastIndex(folder, "[")
	if open < 0 || open >= len(folder)-2 {
		return false
	}
	value := strings.TrimSpace(folder[open+1 : len(folder)-1])
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func groupEvidence(games []catalogGame, indexes []int, identityGames map[string][]int, identityEvidence map[string]Evidence) []Evidence {
	inGroup := map[int]bool{}
	for _, index := range indexes {
		inGroup[index] = true
	}
	seen := map[string]bool{}
	var result []Evidence
	for _, index := range indexes {
		for _, evidence := range games[index].Evidence {
			key := evidenceKey(evidence)
			if seen[key] {
				continue
			}
			connected := 0
			for _, candidate := range identityGames[key] {
				if inGroup[candidate] {
					connected++
				}
			}
			if connected < 2 {
				continue
			}
			seen[key] = true
			record := identityEvidence[key]
			sort.Strings(record.Games)
			result = append(result, record)
		}
	}
	return result
}

func filterGroups(groups []Group, match string) []Group {
	match = strings.ToLower(strings.TrimSpace(match))
	if match == "" {
		return groups
	}
	filtered := make([]Group, 0, len(groups))
	for _, group := range groups {
		if groupMatches(group, match) {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func groupMatches(group Group, match string) bool {
	for _, value := range []string{
		group.Platform,
		group.Keep.Folder,
		group.Keep.Title,
	} {
		if strings.Contains(strings.ToLower(value), match) {
			return true
		}
	}
	for _, candidate := range group.Merge {
		if strings.Contains(strings.ToLower(candidate.Folder), match) || strings.Contains(strings.ToLower(candidate.Title), match) {
			return true
		}
	}
	for _, evidence := range group.Evidence {
		if strings.Contains(strings.ToLower(evidence.Value), match) {
			return true
		}
		for _, game := range evidence.Games {
			if strings.Contains(strings.ToLower(game), match) {
				return true
			}
		}
	}
	return false
}

func chooseKeep(games []catalogGame, indexes []int, regions regionRules) int {
	best := indexes[0]
	bestPreference, bestScore := keepScore(games[best], regions)
	for _, index := range indexes[1:] {
		preference, score := keepScore(games[index], regions)
		if preference > bestPreference || preference == bestPreference &&
			(games[index].EditionBase && !games[best].EditionBase || games[index].EditionBase == games[best].EditionBase &&
				(score > bestScore || score == bestScore && games[index].Folder < games[best].Folder)) {
			best = index
			bestPreference, bestScore = preference, score
		}
	}
	return best
}

// The best conventional release decides the region before metadata or bundle
// size can break a tie. Revisions and discs do not cast additional region votes.
func keepScore(game catalogGame, regions regionRules) (preference, score int) {
	preference = math.MinInt
	if release := chooseDefaultRelease(game.Releases, regions); release != "" {
		preference = releasepolicy.Preference(release, regions.priority, regions.groups)
	}
	identities := 0
	for _, evidence := range game.Evidence {
		if !isXboxSerialHint(evidence) && !isPS3DiscFamily(evidence) {
			identities++
		}
	}
	score = identities*20 + game.SourceCount*10 + len(game.Releases) + folderKeepBonus(game.Folder)
	return preference, score
}

func folderKeepBonus(folder string) int {
	switch {
	case isPlainKeepFolder(folder):
		return 80
	case strings.Contains(folder, "["):
		return -100
	default:
		return 0
	}
}

func isPlainKeepFolder(folder string) bool {
	return !strings.Contains(folder, "(") && !strings.Contains(folder, "[") && trailingPlusCount(folder) == 0
}

func trailingPlusCount(title string) int {
	title = strings.TrimSpace(title)
	count := 0
	for strings.HasSuffix(title, "+") {
		count++
		title = strings.TrimSpace(strings.TrimSuffix(title, "+"))
	}
	return count
}

type regionRules struct {
	priority []string
	groups   map[string][]string
}

func newRegionRules(priority []string, groups map[string][]string) regionRules {
	normalizedPriority := normalizedRegionPriority(priority)
	normalizedGroups := defaultRegionGroups()
	for group, aliases := range groups {
		group = normalizeRegionName(group)
		if group == "" {
			continue
		}
		normalizedGroups[group] = appendUniqueStrings(normalizedGroups[group], group)
		for _, alias := range aliases {
			normalized := normalizeRegionName(alias)
			if normalized != "" {
				normalizedGroups[group] = appendUniqueStrings(normalizedGroups[group], normalized)
			}
		}
	}
	return regionRules{priority: normalizedPriority, groups: normalizedGroups}
}

func newPlatformRegionRules(priorities map[string][]string, groups map[string][]string) map[string]regionRules {
	if len(priorities) == 0 {
		return nil
	}
	result := map[string]regionRules{}
	for platform, priority := range priorities {
		platform = strings.TrimSpace(platform)
		if platform == "" || len(priority) == 0 {
			continue
		}
		result[platform] = newRegionRules(priority, groups)
	}
	return result
}

func rulesForPlatform(defaultRegions regionRules, platformRegions map[string]regionRules, platform string) regionRules {
	if rules, ok := platformRegions[platform]; ok {
		return rules
	}
	return defaultRegions
}

func normalizedRegionPriority(values []string) []string {
	if len(values) == 0 {
		values = defaultRegionPriority()
	}
	var result []string
	for _, value := range values {
		normalized := normalizeRegionName(value)
		if normalized != "" {
			result = appendUniqueStrings(result, normalized)
		}
	}
	if len(result) == 0 {
		return defaultRegionPriority()
	}
	return result
}

func defaultRegionPriority() []string {
	return []string{"usa", "world", "europe", "japan", "asia", "russia"}
}

func defaultRegionGroups() map[string][]string {
	return map[string][]string{
		"usa": {
			"usa",
			"u s a",
			"us",
			"u s",
			"united states",
			"united states of america",
			"north america",
		},
		"world": {
			"world",
			"international",
		},
		"europe": {
			"europe",
			"eu",
			"european union",
			"germany",
			"deutschland",
			"france",
			"spain",
			"italy",
			"netherlands",
			"sweden",
			"denmark",
			"finland",
			"norway",
			"portugal",
			"united kingdom",
			"uk",
			"gb",
			"great britain",
			"australia",
		},
		"japan": {
			"japan",
			"jp",
			"jpn",
			"jap",
			"ja",
		},
		"asia": {
			"asia",
			"asian",
			"china",
			"people's republic of china",
			"prc",
			"cn",
			"chn",
			"zh",
			"hong kong",
			"hk",
			"hkg",
			"taiwan",
			"tw",
			"twn",
			"republic of china",
			"korea",
			"kr",
			"kor",
			"south korea",
			"republic of korea",
			"singapore",
		},
		"russia": {
			"russia",
			"russian federation",
			"ru",
			"rus",
		},
	}
}

func normalizeRegionName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ".", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.Join(strings.Fields(value), " ")
	switch value {
	case "u s", "u s a", "united states of america":
		return "usa"
	case "us":
		return "usa"
	case "jp", "jpn", "jap", "ja":
		return "japan"
	case "eu":
		return "europe"
	case "uk", "gb", "great britain":
		return "united kingdom"
	case "cn", "chn", "prc":
		return "china"
	case "hk", "hkg":
		return "hong kong"
	case "tw", "twn":
		return "taiwan"
	case "kr", "kor":
		return "korea"
	case "ru", "rus":
		return "russia"
	default:
		return value
	}
}

func appendUniqueStrings(values []string, next string) []string {
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

func gameRef(game catalogGame) GameRef {
	return GameRef{
		Folder:       game.Folder,
		Title:        firstNonEmpty(game.Title, game.Folder),
		ReleaseCount: len(game.Releases),
		SourceCount:  game.SourceCount,
		Regions:      append([]string(nil), game.Regions...),
	}
}

func applyGroup(root string, group *Group, regions regionRules) error {
	if group.HoldReason != "" {
		group.Skipped = true
		group.SkipReason = group.HoldReason
		return nil
	}
	if group.ReviewRequired != "" && !group.ReviewAccepted {
		group.Skipped = true
		group.SkipReason = group.ReviewRequired
		return nil
	}
	platformPath := filepath.Join(root, "platforms", group.Platform)
	keepPath := filepath.Join(platformPath, group.Keep.Folder)
	if err := ensureGamePath(platformPath, keepPath); err != nil {
		return err
	}
	games := make([]catalogGame, 0, len(group.Merge)+1)
	keep, err := readGameFull(catalogGame{Platform: group.Platform, Folder: group.Keep.Folder, Path: keepPath})
	if err != nil {
		return err
	}
	games = append(games, keep)
	for _, ref := range group.Merge {
		path := filepath.Join(platformPath, ref.Folder)
		if err := ensureGamePath(platformPath, path); err != nil {
			return err
		}
		game, err := readGameFull(catalogGame{Platform: group.Platform, Folder: ref.Folder, Path: path})
		if err != nil {
			return err
		}
		games = append(games, game)
	}
	for _, game := range games {
		if game.HasRomsetBindings {
			group.Skipped = true
			group.SkipReason = "provider-ID groups cannot change versioned ROM-set bindings; ordinary catalog fold uses accepted MAME grouping automatically"
			return nil
		}
	}
	if group.IdentityReview != nil && strings.TrimSpace(group.ReviewReason) == "" {
		group.Skipped = true
		group.SkipReason = "content identity review requires an explicit reviewed plan reason"
		return nil
	}
	games, reason := reviewedIdentityGames(games, group.DevelopmentParent, group.IdentityReview)
	if reason != "" {
		group.Skipped = true
		group.SkipReason = reason
		return nil
	}
	if group.ReviewReason == "" {
		checked := append([]catalogGame(nil), games...)
		checked = withEditionEvidence(checked, false)
		checked, err = withPS3DiscFamilies(checked)
		if err != nil {
			return err
		}
		checked = withRegionalAliasEvidence(checked)
		checked = withXboxSerialHints(checked)
		indexes := make([]int, len(checked))
		for i := range indexes {
			indexes[i] = i
		}
		reason := identityGroupHoldReason(checked, indexes)
		if reason == "" {
			reason = ps3FamilyGroupHoldReason(checked, indexes)
		}
		if reason == "" && serialHintsRequired(checked, indexes) && !group.ReviewAccepted {
			reason = "shared Xbox 360 serial needs regional identity review"
		}
		if reason != "" {
			group.Skipped = true
			group.SkipReason = reason
			return nil
		}
	}
	changes, reason, err := planGroupFiles(root, *group)
	if err != nil {
		return err
	} else if reason != "" {
		group.Skipped = true
		group.SkipReason = reason
		return nil
	}
	group.FileChanges = changes

	if err := catalogfs.WriteFile(filepath.Join(keepPath, "game.yaml"), []byte(renderMergedGameYAML(games, regions)), 0o644); err != nil {
		return err
	}
	if err := catalogfs.WriteFile(filepath.Join(keepPath, "fingerprints.yaml"), []byte(renderMergedFingerprints(games)), 0o644); err != nil {
		return err
	}
	if err := applyFileChanges(platformPath, keepPath, changes); err != nil {
		return err
	}
	for _, change := range changes {
		if change.Action != "deduplicate" {
			group.MovedFileCount++
		}
	}
	for _, game := range games[1:] {
		if err := catalogfs.RemoveAll(game.Path); err != nil {
			return err
		}
	}
	group.Applied = true
	return nil
}

func ignoreGroupEvidence(root string, group *Group) error {
	evidence := group.Evidence
	if group.ReviewRequired != "" {
		// Reject the proposed serial bridge without discarding the established
		// identities connecting already verified regional members.
		evidence = nil
		for _, item := range group.Evidence {
			if isXboxSerialHint(item) {
				evidence = append(evidence, item)
			}
		}
	}
	tokens := evidenceTokens(evidence)
	if len(tokens) == 0 {
		group.Skipped = true
		group.SkipReason = "no evidence tokens to ignore"
		return nil
	}
	platformPath := filepath.Join(root, "platforms", group.Platform)
	for _, ref := range group.Merge {
		gamePath := filepath.Join(platformPath, ref.Folder)
		if err := ensureGamePath(platformPath, gamePath); err != nil {
			return err
		}
		path := filepath.Join(gamePath, "game.yaml")
		contents, err := catalogfs.ReadFile(path)
		if err != nil {
			return err
		}
		updated, changed := upsertFoldIgnoredEvidence(string(contents), tokens)
		if !changed {
			continue
		}
		if err := catalogfs.WriteFile(path, []byte(updated), 0o644); err != nil {
			return err
		}
		group.IgnoreFileCount++
	}
	group.Ignored = true
	return nil
}

func evidenceTokens(evidence []Evidence) []string {
	var tokens []string
	for _, item := range evidence {
		tokens = appendEvidenceTokens(tokens, evidenceToken(item))
	}
	sort.Strings(tokens)
	return tokens
}

func upsertFoldIgnoredEvidence(contents string, tokens []string) (string, bool) {
	document, err := catalogyaml.Parse([]byte(contents))
	if err != nil {
		return contents, false
	}
	fold, err := catalogyaml.EnsureMapping(document.Root(), "fold")
	if err != nil {
		return contents, false
	}
	existing := catalogyaml.Strings(catalogyaml.Value(fold, "ignore_evidence"))
	merged := appendEvidenceTokens(existing, tokens...)
	if sameEvidenceTokens(existing, merged) {
		return contents, false
	}
	if err := catalogyaml.Set(fold, "ignore_evidence", catalogyaml.StringSequence(merged)); err != nil {
		return contents, false
	}
	updated, err := document.Marshal()
	if err != nil {
		return contents, false
	}
	return string(updated), true
}

func sameEvidenceTokens(a, b []string) bool {
	left := appendEvidenceTokens(nil, a...)
	right := appendEvidenceTokens(nil, b...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func ensureGamePath(platformPath, gamePath string) error {
	platformPath, err := filepath.Abs(platformPath)
	if err != nil {
		return err
	}
	gamePath, err = filepath.Abs(gamePath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(platformPath, gamePath)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to modify path outside platform root: %s", gamePath)
	}
	if strings.Contains(rel, string(filepath.Separator)) {
		return fmt.Errorf("refusing to modify nested path instead of a game folder: %s", gamePath)
	}
	return nil
}

func renderMergedGameYAML(games []catalogGame, regions regionRules) string {
	keep := games[0]
	var releases []block
	seenRelease := map[string]bool{}
	for _, game := range games {
		for _, release := range game.Releases {
			if seenRelease[release.Key] {
				continue
			}
			seenRelease[release.Key] = true
			releases = append(releases, release)
		}
	}
	sort.SliceStable(releases, func(i, j int) bool { return releases[i].Key < releases[j].Key })

	var b bytes.Buffer
	fmt.Fprintf(&b, "title: %s\n", yamlString(firstNonEmpty(keep.Title, keep.Folder)))
	// The kept bundle already has the best release preference in the group.
	// Resolve equal-region title variants inside that bundle so its title and
	// default release do not come from different original folders.
	defaultRelease := chooseDefaultRelease(keep.Releases, regions)
	allDefault := chooseDefaultRelease(releases, regions)
	_, keptProfile := contentidentity.Analyze(defaultRelease)
	_, allProfile := contentidentity.Analyze(allDefault)
	if defaultRelease == "" || (keptProfile.Development() && !allProfile.Development() && allProfile.Kind == "retail") {
		defaultRelease = allDefault
	}
	if defaultRelease != "" {
		fmt.Fprintf(&b, "default_release: %s\n", yamlString(defaultRelease))
	}
	for _, field := range []string{"aliases", "alternate_titles"} {
		var values []string
		for _, game := range games {
			aliases := game.Aliases
			if field == "alternate_titles" {
				aliases = game.AlternateTitles
			}
			for _, alias := range aliases {
				values = appendUniqueString(values, alias)
			}
		}
		if len(values) > 0 {
			fmt.Fprintf(&b, "%s:\n", field)
			for _, value := range values {
				fmt.Fprintf(&b, "  - %s\n", yamlString(value))
			}
		}
	}
	contentKey := ""
	for _, game := range games {
		if game.ContentKey != "" {
			contentKey = game.ContentKey
			break
		}
	}
	ignoredEvidence := mergedIgnoredEvidence(games)
	if contentKey != "" || len(ignoredEvidence) > 0 {
		fmt.Fprintf(&b, "\nfold:\n")
		if contentKey != "" {
			fmt.Fprintf(&b, "  content_key: %s\n", yamlString(contentKey))
		}
		if len(ignoredEvidence) > 0 {
			fmt.Fprintf(&b, "  ignore_evidence:\n")
			for _, token := range ignoredEvidence {
				fmt.Fprintf(&b, "    - %s\n", yamlString(token))
			}
		}
	}
	fmt.Fprintf(&b, "\nreleases:\n")
	for _, release := range releases {
		b.WriteString(release.Text)
	}
	blocks := mergeSourceBlocks(games)
	if len(blocks) > 0 {
		fmt.Fprintf(&b, "\nsources:\n")
		for _, block := range blocks {
			b.WriteString(block.Raw)
		}
	}
	return b.String()
}

func mergedIgnoredEvidence(games []catalogGame) []string {
	seen := map[string]bool{}
	var result []string
	for _, game := range games {
		for _, token := range game.IgnoredEvidence {
			normalized := normalizeEvidenceToken(token)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	sort.Strings(result)
	return result
}

func chooseDefaultRelease(releases []block, regions regionRules) string {
	names := make([]string, 0, len(releases))
	for _, release := range releases {
		names = append(names, release.Key)
	}
	return releasepolicy.Choose(names, regions.priority, regions.groups)
}

func mergeSourceBlocks(games []catalogGame) []sourceBlock {
	bySource := map[string][]sourceBlock{}
	for _, game := range games {
		for name, block := range game.SourceBlocks {
			bySource[name] = append(bySource[name], block)
		}
	}
	var names []string
	for name := range bySource {
		names = append(names, name)
	}
	sort.Strings(names)
	var result []sourceBlock
	for _, name := range names {
		blocks := bySource[name]
		var matches []string
		for _, block := range blocks {
			matches = append(matches, block.MatchBlocks...)
		}
		if len(matches) == 0 {
			result = append(result, blocks[0])
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "  %s:\n", yamlStringIfNeeded(name))
		fmt.Fprintf(&b, "    matches:\n")
		seen := map[string]bool{}
		for _, match := range matches {
			key := strings.TrimSpace(match)
			if seen[key] {
				continue
			}
			seen[key] = true
			b.WriteString(match)
		}
		result = append(result, sourceBlock{Name: name, Raw: b.String(), HasMatches: true, MatchBlocks: matches})
	}
	return result
}

func renderMergedFingerprints(games []catalogGame) string {
	var b strings.Builder
	fmt.Fprintf(&b, "items:\n")
	seen := map[string]bool{}
	for _, game := range games {
		for _, item := range game.FingerprintItems {
			key := strings.TrimSpace(item)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			b.WriteString(item)
		}
	}
	return b.String()
}

func appendEvidenceTokens(values []string, next ...string) []string {
	for _, value := range next {
		token := normalizeEvidenceToken(value)
		if token == "" {
			continue
		}
		values = appendUniqueString(values, token)
	}
	return values
}

func filterIgnoredEvidence(evidence []Evidence, ignored []string) []Evidence {
	if len(evidence) == 0 || len(ignored) == 0 {
		return evidence
	}
	ignoredSet := map[string]bool{}
	for _, token := range ignored {
		normalized := normalizeEvidenceToken(token)
		if normalized != "" {
			ignoredSet[normalized] = true
		}
	}
	filtered := make([]Evidence, 0, len(evidence))
	for _, item := range evidence {
		if ignoredSet[normalizeEvidenceToken(evidenceToken(item))] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func sourceEvidenceFromMatch(source string, lines []string, gameTitle string) []Evidence {
	score, hasScore := matchScore(lines)
	if hasScore && score < minimumStrongMatchScore {
		return nil
	}
	if source == "screenscraper" && !screenScraperMatchCanFold(lines, gameTitle) {
		return nil
	}
	fields := matchScalarFields(lines)
	var result []Evidence
	for _, field := range foldEvidenceFields(source) {
		value := fields[field]
		if value == "" {
			continue
		}
		result = append(result, Evidence{Source: source, Field: field, Value: value, URL: evidenceURL(source, field, value, fields)})
	}
	return result
}

func matchScalarFields(lines []string) map[string]string {
	fields := map[string]string{}
	for _, raw := range lines {
		line := trimComment(raw)
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		key, rawValue, ok := splitKV(value)
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "- "))
		if key == "" {
			continue
		}
		parsedValue := parseScalar(rawValue)
		if parsedValue == "" {
			continue
		}
		fields[key] = parsedValue
	}
	return fields
}

func foldEvidenceFields(source string) []string {
	switch source {
	case "launchbox":
		return []string{"database_id"}
	case "screenscraper":
		return []string{"screenscraper_id"}
	case "igdb":
		return []string{"igdb_id"}
	default:
		return nil
	}
}

func evidenceURL(source, field, value string, fields map[string]string) string {
	if page := strings.TrimSpace(fields["page"]); strings.HasPrefix(page, "https://") || strings.HasPrefix(page, "http://") {
		return page
	}
	switch {
	case source == "igdb" && field == "igdb_id" && strings.TrimSpace(fields["slug"]) != "":
		return "https://www.igdb.com/games/" + url.PathEscape(strings.TrimSpace(fields["slug"]))
	case source == "screenscraper" && field == "screenscraper_id":
		return "https://www.screenscraper.fr/gameinfos.php?gameid=" + url.QueryEscape(value)
	default:
		return ""
	}
}

func screenScraperMatchCanFold(lines []string, gameTitle string) bool {
	titles := matchTitles(lines)
	if len(titles) == 0 || hasNonGameTitle(titles) {
		return false
	}
	return titlesIncludeCatalogTitle(titles, gameTitle)
}

func matchTitles(lines []string) []string {
	var titles []string
	for _, raw := range lines {
		line := trimComment(raw)
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		key, rawValue, ok := splitKV(value)
		key = strings.TrimSpace(strings.TrimPrefix(key, "- "))
		if !ok || key != "title" {
			continue
		}
		title := parseScalar(rawValue)
		if title != "" {
			titles = appendUniqueString(titles, title)
		}
	}
	return titles
}

func hasNonGameTitle(titles []string) bool {
	for _, title := range titles {
		normalized := strings.ToLower(strings.TrimSpace(title))
		if strings.Contains(normalized, "#nongame") || strings.Contains(normalized, "notgame") {
			return true
		}
	}
	return false
}

func titlesIncludeCatalogTitle(titles []string, gameTitle string) bool {
	catalog := normalizeFoldTitle(gameTitle)
	if catalog == "" {
		return false
	}
	for _, title := range titles {
		normalized := normalizeFoldTitle(title)
		if normalized == "" {
			continue
		}
		if normalized == catalog || strings.Contains(normalized, catalog) || strings.Contains(catalog, normalized) {
			return true
		}
	}
	return false
}

func normalizeFoldTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	space := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func matchScore(lines []string) (int, bool) {
	for _, raw := range lines {
		line := trimComment(raw)
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		key, rawValue, ok := splitKV(value)
		if !ok || key != "match_score" {
			continue
		}
		score, err := strconv.Atoi(parseScalar(rawValue))
		if err == nil {
			return score, true
		}
	}
	return 0, false
}

func readFingerprintItems(path string) ([]string, error) {
	sequence, err := readFingerprintSequence(path)
	if err != nil || sequence == nil {
		return nil, err
	}
	var items []string
	for _, item := range sequence.Content {
		text, err := renderSequenceItem(item, 2)
		if err != nil {
			return nil, err
		}
		items = append(items, text)
	}
	return items, nil
}

func readFingerprintSequence(path string) (*yaml.Node, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	root, err := literalFoldYAML(document.Root())
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	items := catalogyaml.Value(root, "items")
	if items == nil || items.Kind == yaml.ScalarNode && items.Tag == "!!null" {
		return nil, nil
	}
	if items.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("fingerprint items must be a sequence: %s", path)
	}
	for _, item := range items.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("fingerprint item must be a mapping: %s", path)
		}
	}
	return items, nil
}

func blockEnd(lines []string, start, indent int) int {
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(trimComment(lines[i])) == "" {
			continue
		}
		if countIndent(lines[i]) <= indent {
			return i
		}
	}
	return len(lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func evidenceKey(e Evidence) string {
	return e.Source + "\x00" + e.Field + "\x00" + e.Value
}

func evidenceToken(e Evidence) string {
	return e.Source + "." + e.Field + "=" + e.Value
}

func normalizeEvidenceToken(raw string) string {
	raw = parseScalar(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	left, right, ok := strings.Cut(raw, "=")
	if !ok {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	sourceField := strings.ToLower(strings.TrimSpace(left))
	value := parseScalar(strings.TrimSpace(right))
	if sourceField == "" || value == "" {
		return ""
	}
	return sourceField + "=" + strings.TrimSpace(value)
}

func appendUniqueIndex(values []int, next int) []int {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
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

func yamlString(value string) string {
	return strconv.Quote(value)
}

func yamlStringIfNeeded(value string) string {
	if strings.ContainsAny(value, " :#\"'") {
		return yamlString(value)
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
