package hasheous

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rommeta/internal/catalogcache"
	"rommeta/internal/fingerprintselect"
	"rommeta/internal/sourceyaml"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

// ImportOptions controls Hasheous hash lookup enrichment.
type ImportOptions struct {
	DataRoot       string
	Platform       string
	Apply          bool
	ForceOverwrite bool
	// SkipWithoutEvidence leaves unqueryable cards untouched during automatic fetch.
	SkipWithoutEvidence bool
	Limit               int
	Concurrency         int
	Client              *http.Client
	Progress            ProgressFunc
}

// ProgressFunc receives progress updates during applied Hasheous lookups.
type ProgressFunc func(ImportProgress)

// ImportProgress describes Hasheous lookup progress.
type ImportProgress struct {
	Current  int
	Total    int
	Stage    string
	Game     string
	Queries  int
	Matches  int
	Worker   int
	Workers  int
	Request  int
	Requests int
}

// ImportResult describes a dry-run or applied Hasheous enrichment.
type ImportResult struct {
	DataRoot                    string
	APIBaseURL                  string
	PlatformCount               int
	GameBundleCount             int
	SelectedGameCount           int
	SkippedByLimitCount         int
	PlannedLookupCount          int
	QueriedLookupCount          int
	MatchedGameCount            int
	GameFileCount               int
	NotFoundGameCount           int
	WrittenFileCount            int
	SkippedExistingCount        int
	SkippedWithoutEvidenceCount int
	FailedLookupCount           int
	Failures                    []ImportFailure
	RateLimitPerSecond          int
	Concurrency                 int
	Limit                       int
	Apply                       bool
}

// ImportFailure is a failed Hasheous lookup that did not stop the whole run.
type ImportFailure struct {
	Game  string
	Query string
	Error string
}

type catalogGame struct {
	MAMEReleases   []catalogcache.Release
	Platform       string
	Path           string
	Title          string
	DefaultRelease string
	Sources        []string
	Fingerprints   []catalogcache.Fingerprint
}

type sourceSettings struct {
	APIBaseURL         string
	RateLimitPerSecond int
	Concurrency        int
}

type lookupInput struct {
	MAMESet string `json:"-"`
	Release string `json:"-"`
	Name    string `json:"-"`
	Label   string `json:"-"`
	CRC     string `json:"CRC,omitempty"`
	MD5     string `json:"MD5,omitempty"`
	SHA1    string `json:"SHA1,omitempty"`
	SHA256  string `json:"SHA256,omitempty"`
}

type lookupResponse struct {
	ID         any                            `json:"id"`
	Name       string                         `json:"name"`
	Signatures map[string][]signatureResponse `json:"signatures"`
	Metadata   []metadataResponse             `json:"metadata"`
}

type signatureResponse struct {
	Game signatureGame `json:"game"`
	ROM  signatureROM  `json:"rom"`
}

type signatureGame struct {
	Description string `json:"description"`
	System      string `json:"system"`
	ID          any    `json:"id"`
	Name        string `json:"name"`
}

type signatureROM struct {
	Name            string `json:"name"`
	Size            any    `json:"size"`
	CRC             string `json:"crc"`
	MD5             string `json:"md5"`
	SHA1            string `json:"sha1"`
	SHA256          string `json:"sha256"`
	SignatureSource string `json:"signatureSource"`
}

type metadataResponse struct {
	ObjectType  string `json:"objectType"`
	ID          string `json:"id"`
	ImmutableID string `json:"immutableId"`
	Status      string `json:"status"`
	MatchMethod string `json:"matchMethod"`
	Source      string `json:"source"`
	Link        string `json:"link"`
}

type matchedGame struct {
	Release             string
	Title               string
	HasheousID          string
	MatchedQuery        string
	Score               int
	IGDBID              int
	TheGamesDBID        string
	RetroAchievementsID string
	SignatureSources    []string
	References          map[string]string
}

type lookupRequest struct {
	Release string
	Inputs  []lookupInput
	Label   string
}

type fingerprintCandidate struct {
	input       lookupInput
	fingerprint catalogcache.Fingerprint
	order       int
	score       int
}

const (
	defaultAPIBaseURL   = "https://hasheous.org/api/v1"
	defaultRateLimitRPS = 1
	maxReleaseLookups   = 8
)

// Import looks up catalog game fingerprints in Hasheous and optionally writes
// source-specific identity matches into game.yaml.
func Import(options ImportOptions) (ImportResult, error) {
	dataRoot := strings.TrimSpace(options.DataRoot)
	if dataRoot == "" {
		dataRoot = "."
	}
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}

	reportStage(options.Progress, options.Apply, "loading config", 0, 0, filepath.Join(dataRoot, "config", "sources.yaml"))
	config, err := store.LoadSources(dataRoot)
	if err != nil {
		return ImportResult{}, err
	}
	settings := hasheousSettings(config)
	if options.Concurrency > 0 {
		settings.Concurrency = options.Concurrency
	}

	result := ImportResult{
		DataRoot:           dataRoot,
		APIBaseURL:         settings.APIBaseURL,
		RateLimitPerSecond: settings.RateLimitPerSecond,
		Concurrency:        settings.Concurrency,
		Apply:              options.Apply,
		Limit:              options.Limit,
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	reportStage(options.Progress, options.Apply, "opening catalog cache", 0, 0, catalogcache.Path(dataRoot))
	pipeline := newImportPipeline(options, settings, client, result)
	platformCount, gameCount, scanErr := streamCatalogGames(dataRoot, options.Platform, "hasheous", options.ForceOverwrite, pipeline.catalogProgress, pipeline.submit)
	result, err = pipeline.finish(scanErr)
	result.PlatformCount = platformCount
	result.GameBundleCount = gameCount
	return result, err
}

func reportProgress(progress ProgressFunc, apply bool, current, total int, game string, queries, matches int) {
	if !apply || progress == nil {
		return
	}
	progress(ImportProgress{
		Current: current,
		Total:   total,
		Game:    game,
		Queries: queries,
		Matches: matches,
	})
}

func reportStage(progress ProgressFunc, apply bool, stage string, current, total int, label string) {
	if !apply || progress == nil {
		return
	}
	progress(ImportProgress{
		Current: current,
		Total:   total,
		Stage:   stage,
		Game:    label,
	})
}

func hasheousSettings(config store.SourcesConfig) sourceSettings {
	settings := sourceSettings{
		APIBaseURL:         defaultAPIBaseURL,
		RateLimitPerSecond: defaultRateLimitRPS,
		Concurrency:        1,
	}
	if config.FetchConcurrency > 0 {
		settings.Concurrency = config.FetchConcurrency
	}
	for _, source := range config.Sources {
		if source.Name != "hasheous" {
			continue
		}
		if source.APIBaseURL != "" {
			settings.APIBaseURL = strings.TrimRight(source.APIBaseURL, "/")
		}
		if source.RateLimitPerSecond > 0 {
			settings.RateLimitPerSecond = source.RateLimitPerSecond
		}
		if source.FetchConcurrency > 0 {
			settings.Concurrency = source.FetchConcurrency
		}
		break
	}
	settings.APIBaseURL = strings.TrimRight(settings.APIBaseURL, "/")
	return settings
}

type lookupProgressFunc func(stage string, current, total int)

func lookupBestMatches(ctx context.Context, client *http.Client, limiter *rateLimiter, settings sourceSettings, game catalogGame, requests []lookupRequest, progress lookupProgressFunc) ([]matchedGame, int, error) {
	queries := 0
	seen := map[string]bool{}
	var retryableErr error
	for index, request := range requests {
		responses, err := lookupHashes(ctx, client, limiter, settings, request.Inputs, func(stage string) {
			if stage == "querying" {
				queries++
			}
			if progress != nil {
				progress(stage, index+1, len(requests))
			}
		})
		if err != nil {
			if isRateLimitStop(err) {
				return nil, queries, err
			}
			if isNotFoundError(err) {
				continue
			}
			if isRetryableLookupError(err) {
				if retryableErr == nil {
					retryableErr = err
				}
				continue
			}
			return nil, queries, err
		}
		if len(request.Inputs) > 0 && request.Inputs[0].MAMESet != "" {
			responses = unambiguousMAMEResponses(responses, request.Inputs)
		}
		for _, response := range responses {
			match := matchFromResponse(response, game, request.Inputs)
			if match.Score == 0 {
				continue
			}
			if match.Release == "" {
				match.Release = request.Release
			}
			key := strings.Join([]string{match.HasheousID, strconv.Itoa(match.IGDBID), match.TheGamesDBID, match.RetroAchievementsID}, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			return []matchedGame{match}, queries, nil
		}
	}
	if retryableErr != nil {
		return nil, queries, retryableErr
	}
	return nil, queries, nil
}

func lookupHashesOnce(ctx context.Context, client *http.Client, limiter *rateLimiter, settings sourceSettings, inputs []lookupInput, progress func(string)) ([]lookupResponse, error) {
	body, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	endpoint := settings.APIBaseURL + "/Lookup/ByHash?returnAllSources=true&returnFields=Signatures,Metadata"
	if len(inputs) > 0 && inputs[0].MAMESet != "" {
		endpoint += "&returnSources=MAMEArcade"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Hasheous lookup request could not be created: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	if progress != nil {
		progress("rate limit")
	}
	if err := limiter.waitWithProgress(ctx, progress); err != nil {
		return nil, err
	}
	if progress != nil {
		progress("querying")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Hasheous lookup request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("Hasheous lookup response could not be read: %w", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, notFoundError{message: strings.TrimSpace(string(responseBody))}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpStatusError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(string(responseBody)),
			RetryAfter: strings.TrimSpace(response.Header.Get("Retry-After")),
		}
	}
	trimmed := bytes.TrimSpace(responseBody)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if bytes.HasPrefix(trimmed, []byte("[")) {
		var responses []lookupResponse
		if err := json.Unmarshal(trimmed, &responses); err != nil {
			return nil, fmt.Errorf("Hasheous lookup response could not be decoded: %w", err)
		}
		return responses, nil
	}
	var responseObject lookupResponse
	if err := json.Unmarshal(trimmed, &responseObject); err != nil {
		return nil, fmt.Errorf("Hasheous lookup response could not be decoded: %w", err)
	}
	return []lookupResponse{responseObject}, nil
}

func matchFromResponse(response lookupResponse, game catalogGame, inputs []lookupInput) matchedGame {
	if len(game.MAMEReleases) > 0 {
		var ok bool
		response, ok = verifiedMAMEResponse(response, inputs)
		if !ok {
			return matchedGame{}
		}
	}
	signatureName := signatureTitle(response)
	responseName := strings.TrimSpace(response.Name)
	trustMetadata := metadataLinksTrustworthy(responseName, signatureName)
	match := matchedGame{
		Release:          firstNonEmpty(matchedRelease(response, inputs), game.DefaultRelease, game.Title),
		Title:            firstNonEmpty(usableTitle(signatureName), usableTitle(responseName)),
		HasheousID:       anyString(response.ID),
		MatchedQuery:     matchedQuery(response, inputs),
		SignatureSources: signatureSources(response),
		References:       map[string]string{},
		Score:            180,
	}
	for _, metadata := range response.Metadata {
		if !trustMetadata {
			continue
		}
		if !metadataIsMapped(metadata) {
			continue
		}
		source := normalizedMetadataSource(metadata.Source)
		id := firstNonEmpty(metadata.ID, metadata.ImmutableID)
		switch source {
		case "igdb":
			if parsed, err := strconv.Atoi(id); err == nil && parsed > 0 {
				match.IGDBID = parsed
				match.Score = 220
			}
		case "thegamesdb":
			match.TheGamesDBID = id
		case "retroachievements":
			match.RetroAchievementsID = id
		}
		if metadata.Link != "" {
			match.References[source] = metadata.Link
		}
	}
	if match.HasheousID == "" && match.Title == "" && len(match.SignatureSources) == 0 && match.IGDBID == 0 && match.TheGamesDBID == "" && match.RetroAchievementsID == "" {
		return matchedGame{}
	}
	if match.MatchedQuery == "" {
		match.MatchedQuery = inputLabels(inputs)
	}
	return match
}

func metadataLinksTrustworthy(responseName, signatureName string) bool {
	responseName = strings.TrimSpace(responseName)
	signatureName = strings.TrimSpace(signatureName)
	if isServiceTitle(responseName) || isServiceTitle(signatureName) {
		return false
	}
	if responseName == "" || signatureName == "" {
		return true
	}
	return titlesOverlap(responseName, signatureName)
}

func fingerprintLookupRequests(game catalogGame) []lookupRequest {
	if len(game.MAMEReleases) > 0 {
		return mameLookupRequests(game)
	}
	type releaseGroup struct {
		release    string
		candidates []fingerprintCandidate
		seenHash   map[string]bool
		priority   int
		order      int
	}
	releaseCounts := map[string]int{}
	for _, fingerprint := range game.Fingerprints {
		releaseCounts[fingerprint.Release]++
	}
	groups := map[string]*releaseGroup{}
	var orderedGroups []*releaseGroup
	for index, fingerprint := range game.Fingerprints {
		if fingerprint.PayloadKind != "" {
			continue
		}
		input := lookupInputFromFingerprint(game, fingerprint)
		if !inputHasHash(input) {
			continue
		}
		release := strings.TrimSpace(fingerprint.Release)
		key := strings.ToLower(release)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(fingerprint.Name))
		}
		group := groups[key]
		if group == nil {
			group = &releaseGroup{
				release:  release,
				seenHash: map[string]bool{},
				order:    index,
			}
			groups[key] = group
			orderedGroups = append(orderedGroups, group)
		}
		hashKey := inputHashKey(input)
		if hashKey == "" || group.seenHash[hashKey] {
			continue
		}
		group.seenHash[hashKey] = true
		priority := fingerprintselect.Priority(fingerprint, game.DefaultRelease, releaseCounts[fingerprint.Release])
		group.candidates = append(group.candidates, fingerprintCandidate{
			input:       input,
			fingerprint: fingerprint,
			order:       index,
			score:       priority,
		})
		if len(group.candidates) == 1 || priority > group.priority {
			group.priority = priority
		}
	}
	sort.SliceStable(orderedGroups, func(i, j int) bool {
		if orderedGroups[i].priority != orderedGroups[j].priority {
			return orderedGroups[i].priority > orderedGroups[j].priority
		}
		return orderedGroups[i].order < orderedGroups[j].order
	})
	requests := make([]lookupRequest, 0, len(orderedGroups))
	for _, group := range orderedGroups {
		inputs := representativeLookupInputs(group.candidates)
		if len(inputs) == 0 {
			continue
		}
		requests = append(requests, lookupRequest{
			Release: group.release,
			Inputs:  inputs,
			Label:   requestLabel(group.release, inputs),
		})
		if len(requests) >= maxReleaseLookups {
			break
		}
	}
	return requests
}

func representativeLookupInputs(candidates []fingerprintCandidate) []lookupInput {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return []lookupInput{candidates[0].input}
	}

	if hasExtension(candidates, "cue") {
		if selected := selectLikelyDataByExtension(candidates, "bin"); len(selected) > 0 {
			return selected[:1]
		}
	}
	if hasAnyExtension(candidates, "gdi", "toc", "ccd") {
		if selected := selectLikelyDataByExtension(candidates, "bin", "raw", "img", "iso", "chd"); len(selected) > 0 {
			return selected[:1]
		}
	}
	if hasAnyExtension(candidates, "iso", "img", "ima", "chd") {
		if selected := selectBestByExtension(candidates, "iso", "img", "ima", "chd"); len(selected) > 0 {
			return selected[:1]
		}
	}

	sorted := append([]fingerprintCandidate{}, candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].score != sorted[j].score {
			return sorted[i].score > sorted[j].score
		}
		return sorted[i].order < sorted[j].order
	})
	if len(sorted) > 5 {
		sorted = sorted[:5]
	}
	inputs := make([]lookupInput, 0, len(sorted))
	for _, candidate := range sorted {
		inputs = append(inputs, candidate.input)
	}
	return inputs
}

func selectLikelyDataByExtension(candidates []fingerprintCandidate, extensions ...string) []lookupInput {
	selected := candidatesByExtension(candidates, extensions...)
	// In cue/bin disc sets, Track 01 can be a shared warning/audio track.
	// DAT file size is the best local signal for choosing a data-like track.
	sort.SliceStable(selected, func(i, j int) bool {
		leftSize := selected[i].fingerprint.Size
		rightSize := selected[j].fingerprint.Size
		if leftSize != rightSize {
			return leftSize > rightSize
		}
		if selected[i].score != selected[j].score {
			return selected[i].score > selected[j].score
		}
		return selected[i].order < selected[j].order
	})
	return lookupInputsFromCandidates(selected)
}

func hasExtension(candidates []fingerprintCandidate, extension string) bool {
	for _, candidate := range candidates {
		if fingerprintExtension(candidate.fingerprint) == extension {
			return true
		}
	}
	return false
}

func hasAnyExtension(candidates []fingerprintCandidate, extensions ...string) bool {
	for _, candidate := range candidates {
		current := fingerprintExtension(candidate.fingerprint)
		for _, extension := range extensions {
			if current == extension {
				return true
			}
		}
	}
	return false
}

func selectBestByExtension(candidates []fingerprintCandidate, extensions ...string) []lookupInput {
	selected := candidatesByExtension(candidates, extensions...)
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].score != selected[j].score {
			return selected[i].score > selected[j].score
		}
		return selected[i].order < selected[j].order
	})
	return lookupInputsFromCandidates(selected)
}

func candidatesByExtension(candidates []fingerprintCandidate, extensions ...string) []fingerprintCandidate {
	var selected []fingerprintCandidate
	for _, candidate := range candidates {
		current := fingerprintExtension(candidate.fingerprint)
		for _, extension := range extensions {
			if current == extension {
				selected = append(selected, candidate)
				break
			}
		}
	}
	return selected
}

func lookupInputsFromCandidates(candidates []fingerprintCandidate) []lookupInput {
	inputs := make([]lookupInput, 0, len(candidates))
	for _, candidate := range candidates {
		inputs = append(inputs, candidate.input)
	}
	return inputs
}

func fingerprintExtension(fingerprint catalogcache.Fingerprint) string {
	format := strings.ToLower(strings.TrimSpace(fingerprint.Format))
	if format != "" {
		return strings.TrimPrefix(format, ".")
	}
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(fingerprint.Name), "."))
}

func lookupInputFromFingerprint(game catalogGame, fingerprint catalogcache.Fingerprint) lookupInput {
	release := strings.TrimSpace(fingerprint.Release)
	queryTerm := firstNonEmpty(release, strings.TrimSpace(fingerprint.Name), game.DefaultRelease, game.Title)
	input := lookupInput{
		Release: release,
		Name:    strings.TrimSpace(fingerprint.Name),
		Label:   firstHashLabel(fingerprint),
		CRC:     cleanHash(fingerprint.CRC32, 8),
		MD5:     cleanHash(fingerprint.MD5, 32),
		SHA1:    cleanHash(fingerprint.SHA1, 40),
		SHA256:  cleanHash(fingerprint.SHA256, 64),
	}
	if input.Label == "" {
		input.Label = stripTags(queryTerm)
	}
	return input
}

func cleanHash(value string, length int) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != length {
		return ""
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return ""
	}
	return value
}

func inputHasHash(input lookupInput) bool {
	return input.CRC != "" || input.MD5 != "" || input.SHA1 != "" || input.SHA256 != ""
}

func inputHashKey(input lookupInput) string {
	return strings.Join([]string{input.CRC, input.MD5, input.SHA1, input.SHA256}, "/")
}

func firstHashLabel(fingerprint catalogcache.Fingerprint) string {
	for _, item := range []struct {
		name  string
		value string
		size  int
	}{
		{name: "md5", value: fingerprint.MD5, size: 32},
		{name: "sha1", value: fingerprint.SHA1, size: 40},
		{name: "sha256", value: fingerprint.SHA256, size: 64},
		{name: "crc", value: fingerprint.CRC32, size: 8},
	} {
		hash := cleanHash(item.value, item.size)
		if hash == "" {
			continue
		}
		if len(hash) > 8 {
			hash = hash[:8]
		}
		return item.name + ":" + hash
	}
	return ""
}

func matchedRelease(response lookupResponse, inputs []lookupInput) string {
	for _, signatures := range response.Signatures {
		for _, signature := range signatures {
			if input := inputForSignature(signature.ROM, inputs); input.Release != "" {
				return input.Release
			}
		}
	}
	return ""
}

func matchedQuery(response lookupResponse, inputs []lookupInput) string {
	for _, signatures := range response.Signatures {
		for _, signature := range signatures {
			if input := inputForSignature(signature.ROM, inputs); input.Label != "" {
				return input.Label
			}
		}
	}
	return ""
}

func inputForSignature(rom signatureROM, inputs []lookupInput) lookupInput {
	for _, input := range inputs {
		if hashesOverlap(input, rom) {
			return input
		}
	}
	return lookupInput{}
}

func hashesOverlap(input lookupInput, rom signatureROM) bool {
	return (input.CRC != "" && strings.EqualFold(input.CRC, rom.CRC)) ||
		(input.MD5 != "" && strings.EqualFold(input.MD5, rom.MD5)) ||
		(input.SHA1 != "" && strings.EqualFold(input.SHA1, rom.SHA1)) ||
		(input.SHA256 != "" && strings.EqualFold(input.SHA256, rom.SHA256))
}

func signatureTitle(response lookupResponse) string {
	var serviceTitle string
	sources := make([]string, 0, len(response.Signatures))
	for source := range response.Signatures {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	// Source names provide a stable tie-break, not a new authority ranking.
	for _, source := range sources {
		for _, signature := range response.Signatures[source] {
			title := strings.TrimSpace(signature.Game.Name)
			if isServiceTitle(title) {
				serviceTitle = firstNonEmpty(serviceTitle, title)
			} else if title != "" {
				return title
			}
		}
	}
	// Retain evidence of an all-service response for the metadata trust check;
	// usableTitle prevents this fallback from becoming a persisted title.
	return serviceTitle
}

func isServiceTitle(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	return title == "#nongame" || strings.HasPrefix(title, "zzz(notgame):")
}

func usableTitle(title string) string {
	if isServiceTitle(title) {
		return ""
	}
	return strings.TrimSpace(title)
}

func signatureSources(response lookupResponse) []string {
	var sources []string
	for source, signatures := range response.Signatures {
		if len(signatures) == 0 {
			continue
		}
		sources = appendUnique(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func metadataIsMapped(metadata metadataResponse) bool {
	status := strings.ToLower(strings.TrimSpace(metadata.Status))
	if status != "" && status != "mapped" {
		return false
	}
	return firstNonEmpty(metadata.ID, metadata.ImmutableID) != ""
}

func normalizedMetadataSource(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "igdb":
		return "igdb"
	case "thegamesdb", "tgdb":
		return "thegamesdb"
	case "retroachievements", "ra":
		return "retroachievements"
	default:
		return value
	}
}

func inputLabels(inputs []lookupInput) string {
	labels := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if input.Label != "" {
			labels = append(labels, input.Label)
		}
	}
	if len(labels) > 3 {
		labels = append(labels[:3], "...")
	}
	return strings.Join(labels, ", ")
}

func requestLabel(release string, inputs []lookupInput) string {
	parts := []string{}
	if strings.TrimSpace(release) != "" {
		parts = append(parts, strings.TrimSpace(release))
	}
	if len(inputs) == 1 {
		if inputs[0].Label != "" {
			parts = append(parts, inputs[0].Label)
		}
	} else if len(inputs) > 1 {
		parts = append(parts, fmt.Sprintf("%d files", len(inputs)))
	}
	return strings.Join(parts, " / ")
}

func requestLabels(requests []lookupRequest) string {
	labels := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.Label != "" {
			labels = append(labels, request.Label)
		}
	}
	if len(labels) > 3 {
		labels = append(labels[:3], "...")
	}
	return strings.Join(labels, ", ")
}

func streamCatalogGames(dataRoot, platform, source string, forceOverwrite bool, progress taskprogress.Func, fn func(catalogGame) error) (int, int, error) {
	return catalogcache.ForEachGameWithOptions(dataRoot, catalogcache.MetadataReadOptions(platformFilters(platform), source, forceOverwrite), progress, func(cached catalogcache.Game) error {
		return fn(catalogGameFromCached(cached))
	})
}

// HasLookupEvidence reports whether the connector can construct a real lookup.
// Component fingerprints require MAME set identity and a valid component SHA1.
func HasLookupEvidence(game catalogcache.Game) bool {
	return len(fingerprintLookupRequests(catalogGameFromCached(game))) > 0
}

func catalogGameFromCached(cached catalogcache.Game) catalogGame {
	game := catalogGame{
		Platform:       cached.Platform,
		Path:           cached.Path,
		Title:          cached.Title,
		DefaultRelease: cached.DefaultRelease,
		Sources:        cached.Sources,
		Fingerprints:   cached.Fingerprints,
	}
	for _, release := range cached.Releases {
		if release.MAMESet != "" {
			game.MAMEReleases = append(game.MAMEReleases, release)
		}
	}
	return game
}

func platformFilters(platform string) []string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return []string{platform}
}

type rateLimiter struct {
	mu        sync.Mutex
	delay     time.Duration
	last      time.Time
	notBefore time.Time
}

func newRateLimiter(rps int) *rateLimiter {
	if rps <= 0 {
		rps = defaultRateLimitRPS
	}
	return &rateLimiter{delay: time.Second / time.Duration(rps)}
}

func (limiter *rateLimiter) pause(delay time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if until := time.Now().Add(delay); until.After(limiter.notBefore) {
		limiter.notBefore = until
	}
}

// backoff lowers the shared dispatch rate as well as delaying retries. Keeping
// the slower pace after recovery prevents workers from immediately re-exhausting
// the same server window.
func (limiter *rateLimiter) backoff(delay time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.delay = min(time.Minute, max(time.Second, limiter.delay*2))
	if until := time.Now().Add(delay); until.After(limiter.notBefore) {
		limiter.notBefore = until
	}
}

func (limiter *rateLimiter) wait(ctx context.Context) error {
	return limiter.waitWithProgress(ctx, nil)
}

func (limiter *rateLimiter) waitWithProgress(ctx context.Context, progress func(string)) error {
	if limiter == nil {
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limiter.mu.Lock()
		now := time.Now()
		next := limiter.last.Add(limiter.delay)
		cooldown := limiter.notBefore.After(now)
		if limiter.notBefore.After(next) {
			next = limiter.notBefore
		}
		sleep := next.Sub(now)
		if sleep <= 0 {
			limiter.last = now
			limiter.mu.Unlock()
			return nil
		}
		limiter.mu.Unlock()
		if progress != nil && cooldown {
			progress("server cooldown " + sleep.Round(time.Second).String())
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		// Recheck both pacing and cooldown after waking: another worker may
		// have dispatched a request or extended the server's pause.
	}
}

type notFoundError struct {
	message string
}

func (err notFoundError) Error() string {
	if err.message == "" {
		return "not found"
	}
	return err.message
}

func isNotFoundError(err error) bool {
	var target notFoundError
	return errors.As(err, &target)
}

type httpStatusError struct {
	StatusCode int
	Status     string
	Body       string
	RetryAfter string
}

func (err httpStatusError) Error() string {
	message := strings.TrimSpace(err.Body)
	if message == "" {
		message = err.Status
	}
	if err.RetryAfter != "" {
		return fmt.Sprintf("Hasheous lookup returned %s: %s (retry after %s)", err.Status, message, err.RetryAfter)
	}
	return fmt.Sprintf("Hasheous lookup returned %s: %s", err.Status, message)
}

func isRetryableLookupError(err error) bool {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func renderMetadata(matches []matchedGame) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "    matches:\n")
	for _, match := range matches {
		fmt.Fprintf(&b, "      - release: %s\n", sourceyaml.YAMLString(match.Release))
		if match.HasheousID != "" {
			fmt.Fprintf(&b, "        hasheous_id: %s\n", sourceyaml.YAMLString(match.HasheousID))
		}
		if match.Title != "" {
			fmt.Fprintf(&b, "        title: %s\n", sourceyaml.YAMLString(match.Title))
		}
		if match.IGDBID > 0 {
			fmt.Fprintf(&b, "        igdb_id: %d\n", match.IGDBID)
		}
		if match.TheGamesDBID != "" {
			fmt.Fprintf(&b, "        thegamesdb_id: %s\n", sourceyaml.YAMLString(match.TheGamesDBID))
		}
		if match.RetroAchievementsID != "" {
			fmt.Fprintf(&b, "        retroachievements_id: %s\n", sourceyaml.YAMLString(match.RetroAchievementsID))
		}
		if len(match.SignatureSources) > 0 {
			fmt.Fprintf(&b, "        signatures: %s\n", yamlStringList(match.SignatureSources))
		}
		if len(match.References) > 0 {
			fmt.Fprintf(&b, "        references:\n")
			for _, key := range sortedMapKeys(match.References) {
				fmt.Fprintf(&b, "          %s: %s\n", key, sourceyaml.YAMLString(match.References[key]))
			}
		}
		if match.MatchedQuery != "" {
			fmt.Fprintf(&b, "        matched_query: %s\n", sourceyaml.YAMLString(match.MatchedQuery))
		}
		if match.Score > 0 {
			fmt.Fprintf(&b, "        match_score: %d\n", match.Score)
		}
	}
	return b.String()
}

func renderNotFoundMetadata(reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    status: not_found\n")
	fmt.Fprintf(&b, "    checked_at: %s\n", sourceyaml.YAMLString(time.Now().UTC().Format(time.RFC3339)))
	if strings.TrimSpace(reason) != "" {
		fmt.Fprintf(&b, "    reason: %s\n", sourceyaml.YAMLString(reason))
	}
	return b.String()
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

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func yamlStringList(values []string) string {
	var quoted []string
	for _, value := range values {
		quoted = append(quoted, sourceyaml.YAMLString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func stripTags(value string) string {
	var b strings.Builder
	inTag := false
	for _, char := range value {
		switch char {
		case '(', '[':
			inTag = true
			b.WriteByte(' ')
		case ')', ']':
			inTag = false
			b.WriteByte(' ')
		default:
			if !inTag {
				b.WriteRune(char)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func titlesOverlap(left, right string) bool {
	leftKeys := titleKeys(left)
	if len(leftKeys) == 0 {
		return false
	}
	for key := range titleKeys(right) {
		if leftKeys[key] {
			return true
		}
	}
	return false
}

func titleKeys(value string) map[string]bool {
	keys := map[string]bool{}
	for _, candidate := range []string{value, stripTags(value)} {
		key := compactTitleKey(candidate)
		if key != "" {
			keys[key] = true
		}
	}
	return keys
}

func compactTitleKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			b.WriteRune(char)
		}
	}
	return b.String()
}

func appendUnique(values []string, next string) []string {
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
			return strings.TrimSpace(value)
		}
	}
	return ""
}
