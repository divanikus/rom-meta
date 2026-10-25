package igdb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/providerrefs"
	"rommeta/internal/store"
)

type linkReview struct {
	SchemaVersion int               `json:"schema_version"`
	Platform      string            `json:"platform"`
	PlatformID    int               `json:"platform_id"`
	Entries       []linkReviewEntry `json:"entries"`
}
type linkReviewEntry struct {
	Game           string   `json:"game"`
	ExpectedSHA256 string   `json:"expected_sha256"`
	FromID         int64    `json:"from_id"`
	Action         string   `json:"action"`
	ToID           int64    `json:"to_id,omitempty"`
	Reason         string   `json:"reason"`
	Evidence       []string `json:"evidence"`
}

func (e linkReviewEntry) decision() providerrefs.IGDBDecision {
	return providerrefs.IGDBDecision{Source: "hasheous", FromID: e.FromID, Action: e.Action, ToID: e.ToID, Reason: e.Reason, Evidence: e.Evidence}
}
func (e linkReviewEntry) target() int64 {
	if e.Action == "suppress" {
		return 0
	}
	if e.Action == "replace" {
		return e.ToID
	}
	return e.FromID
}
func shaBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

var linkSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,399}$`)

func loadLinkReview(options ImportOptions) (string, linkReview, error) {
	root, e := filepath.Abs(options.DataRoot)
	if e != nil {
		return "", linkReview{}, e
	}
	if options.LinkReview == "" || options.ForceOverwrite || options.RefreshRelations || options.Limit != 0 {
		return "", linkReview{}, fmt.Errorf("--refresh-links requires --link-review, without force-overwrite, refresh-relations or limit")
	}
	f, e := catalogfs.Open(options.LinkReview)
	if e != nil {
		return "", linkReview{}, e
	}
	defer f.Close()
	var review linkReview
	dec := json.NewDecoder(io.LimitReader(f, 4<<20))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&review); e != nil {
		return "", review, e
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return "", review, fmt.Errorf("review must contain exactly one JSON object")
	}
	if review.SchemaVersion != 1 || review.PlatformID < 1 || review.Platform == "" || options.Platform != review.Platform || len(review.Entries) < 1 || len(review.Entries) > 500 {
		return "", review, fmt.Errorf("review schema/platform/count differs from explicit --platform")
	}
	seen := map[string]bool{}
	for _, entry := range review.Entries {
		parts := strings.Split(entry.Game, "/")
		if len(parts) != 3 || parts[0] != "platforms" || parts[1] != review.Platform || parts[2] == "" || parts[2] == "." || parts[2] == ".." || strings.ContainsAny(entry.Game, `\:`) || seen[entry.Game] {
			return "", review, fmt.Errorf("invalid or duplicate reviewed card path: %s", entry.Game)
		}
		seen[entry.Game] = true
		h, err := hex.DecodeString(entry.ExpectedSHA256)
		if err != nil || len(h) != 32 || entry.FromID < 1 {
			return "", review, fmt.Errorf("invalid reviewed SHA/ID: %s", entry.Game)
		}
		decision := entry.decision()
		if entry.Action == "confirm" {
			if entry.ToID != 0 {
				return "", review, fmt.Errorf("confirm cannot replace ID")
			}
			decision.Action = "suppress"
		}
		if err = decision.Validate(); err != nil {
			return "", review, err
		}
	}
	return root, review, nil
}

func linkReferences(doc *catalogyaml.Document) ([]providerrefs.IGDBDecision, error) {
	var rows []providerrefs.IGDBDecision
	node := catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(doc.Root(), "manual"), "provider_references"), "igdb")
	if node != nil {
		if e := node.Decode(&rows); e != nil {
			return nil, e
		}
	}
	seen := map[int64]bool{}
	for _, d := range rows {
		if e := d.Validate(); e != nil {
			return nil, e
		}
		if seen[d.FromID] {
			return nil, fmt.Errorf("duplicate IGDB reference decision")
		}
		seen[d.FromID] = true
	}
	return rows, nil
}
func linkState(data []byte, entry linkReviewEntry, platformID int) (bool, error) {
	doc, e := catalogyaml.Parse(data)
	if e != nil {
		return false, e
	}
	hasheous := catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(doc.Root(), "sources"), "hasheous"), "matches")
	var refs []map[string]any
	if hasheous != nil {
		if e = hasheous.Decode(&refs); e != nil {
			return false, e
		}
	}
	found := false
	for _, m := range refs {
		if m["igdb_id"] == nil {
			continue
		}
		n, err := strconv.ParseInt(fmt.Sprint(m["igdb_id"]), 10, 64)
		if err != nil || n < 1 {
			continue
		}
		if n != entry.FromID {
			return false, fmt.Errorf("review from_id disagrees with Hasheous card references")
		}
		found = true
	}
	if !found {
		return false, fmt.Errorf("review from_id absent from Hasheous card")
	}
	decisions, e := linkReferences(doc)
	if e != nil {
		return false, e
	}
	decisionDone := entry.Action == "confirm"
	for _, d := range decisions {
		if d.FromID == entry.FromID {
			if !reflect.DeepEqual(d, entry.decision()) {
				return false, fmt.Errorf("existing curated IGDB decision differs")
			}
			decisionDone = true
		}
	}
	target := entry.target()
	complete := true
	ownCount := 0
	for _, m := range relationMatches(doc) {
		if relationID(m) == 0 {
			continue
		}
		ownCount++
		if int64(relationID(m)) != target {
			return false, fmt.Errorf("own IGDB snapshot differs from reviewed target; preserve it")
		}
		var ids []int
		if node := catalogyaml.Value(m, "platform_ids"); node != nil {
			_ = node.Decode(&ids)
		}
		platformOK := false
		for _, id := range ids {
			platformOK = platformOK || id == platformID
		}
		slug := catalogyaml.Scalar(catalogyaml.Value(m, "slug"))
		page := catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(m, "references"), "page"))
		complete = complete && (catalogyaml.Scalar(catalogyaml.Value(m, "capture")) == "identity-links" && linkSlug.MatchString(slug) && page == "https://www.igdb.com/games/"+slug && platformOK)
	}
	return complete && (target == 0 || ownCount > 0) && decisionDone, nil
}
func updateLinks(data []byte, entry linkReviewEntry, game apiGame) ([]byte, error) {
	doc, e := catalogyaml.Parse(data)
	if e != nil {
		return nil, e
	}
	if entry.Action != "confirm" {
		rows, e := linkReferences(doc)
		if e != nil {
			return nil, e
		}
		found := false
		for _, d := range rows {
			found = found || d.FromID == entry.FromID
		}
		if !found {
			rows = append(rows, entry.decision())
			manual, e := catalogyaml.EnsureMapping(doc.Root(), "manual")
			if e != nil {
				return nil, e
			}
			refs, e := catalogyaml.EnsureMapping(manual, "provider_references")
			if e != nil {
				return nil, e
			}
			node := &yaml.Node{}
			if e = node.Encode(rows); e != nil {
				return nil, e
			}
			if e = catalogyaml.Set(refs, "igdb", node); e != nil {
				return nil, e
			}
		}
	}
	if entry.target() != 0 {
		sources, e := catalogyaml.EnsureMapping(doc.Root(), "sources")
		if e != nil {
			return nil, e
		}
		source, e := catalogyaml.EnsureMapping(sources, "igdb")
		if e != nil {
			return nil, e
		}
		matches := catalogyaml.Value(source, "matches")
		if matches == nil {
			matches = &yaml.Node{Kind: yaml.SequenceNode}
			_ = catalogyaml.Set(source, "matches", matches)
		}
		if matches.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("IGDB matches must be a sequence")
		}
		chosen := []*yaml.Node{}
		for _, m := range matches.Content {
			if relationID(m) > 0 {
				chosen = append(chosen, m)
			}
		}
		if len(chosen) == 0 {
			m := &yaml.Node{Kind: yaml.MappingNode}
			matches.Content = append(matches.Content, m)
			chosen = append(chosen, m)
		}
		names := []string{}
		ids := []int{}
		for _, p := range game.Platforms {
			names = append(names, p.Name)
			ids = append(ids, p.ID)
		}
		for _, m := range chosen {
			fields := []struct {
				key   string
				value any
			}{{"igdb_id", strconv.Itoa(game.ID)}, {"title", game.Name}, {"slug", game.Slug}, {"platforms", names}, {"platform_ids", ids}, {"capture", "identity-links"}}
			for _, field := range fields {
				key, value := field.key, field.value
				if old := catalogyaml.Value(m, key); old != nil {
					if key == "slug" && catalogyaml.Scalar(old) != game.Slug {
						return nil, fmt.Errorf("existing slug differs from official ID")
					}
					continue
				}
				node := &yaml.Node{}
				if e = node.Encode(value); e != nil {
					return nil, e
				}
				if e = catalogyaml.Set(m, key, node); e != nil {
					return nil, e
				}
			}
			refs, e := catalogyaml.EnsureMapping(m, "references")
			if e != nil {
				return nil, e
			}
			if old := catalogyaml.Value(refs, "page"); old != nil && catalogyaml.Scalar(old) != game.URL {
				return nil, fmt.Errorf("existing page differs from official ID")
			}
			if e = catalogyaml.Set(refs, "page", &yaml.Node{Kind: yaml.ScalarNode, Value: game.URL}); e != nil {
				return nil, e
			}
		}
		if catalogyaml.Scalar(catalogyaml.Value(source, "status")) == "not_found" {
			_ = catalogyaml.Set(source, "status", &yaml.Node{Kind: yaml.ScalarNode, Value: "identity_only"})
		}
	}
	return doc.Marshal()
}

// refreshLinks queries reviewed IDs only. All source bindings and candidates are
// validated before the first write. It never performs title search or media fetch.
func refreshLinks(options ImportOptions) (ImportResult, error) {
	root, review, e := loadLinkReview(options)
	result := ImportResult{DataRoot: root, Apply: options.Apply, PlatformCount: 1}
	if e != nil {
		return result, e
	}
	type work struct {
		entry         linkReviewEntry
		path          string
		before, after []byte
	}
	pending := []work{}
	ids := map[int]bool{}
	for _, entry := range review.Entries {
		path := filepath.Join(root, filepath.FromSlash(entry.Game), "game.yaml")
		resolved, err := catalogfs.EvalSymlinks(path)
		if err != nil {
			return result, err
		}
		absRoot, err := catalogfs.EvalSymlinks(root)
		if err != nil {
			return result, err
		}
		relative, err := filepath.Rel(absRoot, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return result, fmt.Errorf("reviewed card escapes data-root")
		}
		data, err := catalogfs.ReadFile(path)
		if err != nil {
			return result, err
		}
		done, err := linkState(data, entry, review.PlatformID)
		if err != nil {
			return result, fmt.Errorf("%s: %w", entry.Game, err)
		}
		if done {
			result.SkippedExistingCount++
			result.LinkRows = append(result.LinkRows, entry.Game+" | unchanged")
			continue
		}
		if shaBytes(data) != strings.ToLower(entry.ExpectedSHA256) {
			return result, fmt.Errorf("%s: source SHA differs; prepare a fresh reviewed input", entry.Game)
		}
		pending = append(pending, work{entry: entry, path: path, before: data})
		if entry.target() > 0 {
			ids[int(entry.target())] = true
		}
	}
	result.GameBundleCount = len(review.Entries)
	result.SelectedGameCount = len(pending)
	records := map[int]apiGame{}
	if len(ids) > 0 {
		cfg, err := store.LoadSources(root)
		if err != nil {
			return result, err
		}
		settings, err := igdbSettings(cfg)
		if err != nil {
			return result, err
		}
		client := options.Client
		if client == nil {
			client = &http.Client{Timeout: 60 * time.Second}
		}
		token, err := ensureAccessToken(client, root, settings)
		if err != nil {
			return result, err
		}
		cache, err := openQueryCache(root, false)
		if err != nil {
			return result, err
		}
		defer cache.close()
		ordered := []int{}
		for id := range ids {
			ordered = append(ordered, id)
		}
		sort.Ints(ordered)
		games, cached, err := queryLinks(client, cache, newRateLimiter(settings.RateLimitPerSecond), settings, token, ordered)
		if err != nil {
			return result, err
		}
		if cached {
			result.CachedSearchCount++
		} else {
			result.QueriedSearchCount++
		}
		for _, game := range games {
			if !ids[game.ID] {
				return result, fmt.Errorf("IGDB returned an unrequested ID")
			}
			if _, exists := records[game.ID]; exists {
				return result, fmt.Errorf("IGDB returned duplicate ID")
			}
			records[game.ID] = game
		}
	}
	for i := range pending {
		item := &pending[i]
		target := item.entry.target()
		game := records[int(target)]
		if target > 0 {
			platformOK := false
			for _, p := range game.Platforms {
				platformOK = platformOK || p.ID == review.PlatformID && platformMatches(review.Platform, []apiNamed{p}, []string{p.Name}, options.PlatformRegistry)
			}
			if int64(game.ID) != target || !platformOK || !linkSlug.MatchString(game.Slug) || game.Name == "" || game.URL != "https://www.igdb.com/games/"+game.Slug {
				return result, fmt.Errorf("%s: official ID/platform/slug/page failed review", item.entry.Game)
			}
		}
		item.after, e = updateLinks(item.before, item.entry, game)
		if e != nil {
			return result, fmt.Errorf("%s: %w", item.entry.Game, e)
		}
		complete, err := linkState(item.after, item.entry, review.PlatformID)
		if err != nil || !complete {
			return result, fmt.Errorf("%s: incomplete identity-link projection: %v", item.entry.Game, err)
		}
		result.LinkRows = append(result.LinkRows, fmt.Sprintf("%s | %s | %d -> %d | %s", item.entry.Game, item.entry.Action, item.entry.FromID, target, game.Slug))
	}
	// Recheck the complete write set before the first mutation; each atomic write
	// also rechecks its input. This is resumable, not a multi-file transaction.
	for _, item := range pending {
		data, err := catalogfs.ReadFile(item.path)
		if err != nil || shaBytes(data) != shaBytes(item.before) {
			return result, fmt.Errorf("source changed before first write")
		}
	}
	if options.Apply {
		for _, item := range pending {
			data, err := catalogfs.ReadFile(item.path)
			if err != nil || shaBytes(data) != shaBytes(item.before) {
				return result, fmt.Errorf("source changed before atomic write")
			}
			info, err := catalogfs.Stat(item.path)
			if err != nil {
				return result, err
			}
			if err = atomicfile.WriteFile(item.path, item.after, info.Mode().Perm()); err != nil {
				return result, err
			}
			result.WrittenFileCount++
		}
	}
	return result, nil
}

func queryLinks(client *http.Client, cache *queryCache, limiter *rateLimiter, settings sourceSettings, token accessToken, ids []int) ([]apiGame, bool, error) {
	values := []string{}
	for _, id := range ids {
		values = append(values, strconv.Itoa(id))
	}
	query := strings.Join(values, ",")
	key := cacheKey("identity-links-v1", settings.APIBaseURL, query)
	if cache != nil && !cache.bypass {
		var raw string
		err := cache.db.QueryRow("SELECT response_json FROM igdb_query_cache WHERE cache_key = ?", key).Scan(&raw)
		if err == nil {
			var games []apiGame
			if json.Unmarshal([]byte(raw), &games) == nil {
				return games, true, nil
			}
		}
	}
	limiter.wait()
	body := "fields id,name,slug,url,platforms.id,platforms.name; where id = (" + query + "); limit 500;"
	req, e := http.NewRequest(http.MethodPost, strings.TrimRight(settings.APIBaseURL, "/")+"/games", strings.NewReader(body))
	if e != nil {
		return nil, false, e
	}
	req.Header.Set("Client-ID", settings.ClientID)
	req.Header.Set("Authorization", "Bearer "+token.Value)
	req.Header.Set("Content-Type", "text/plain")
	response, e := client.Do(req)
	if e != nil {
		return nil, false, fmt.Errorf("IGDB identity-link request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("IGDB identity-link request returned HTTP %d", response.StatusCode)
	}
	var games []apiGame
	if e = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&games); e != nil {
		return nil, false, e
	}
	if cache != nil {
		if e = cache.storeGames(key, settings.APIBaseURL, "identity-links:"+query, games); e != nil {
			return nil, false, e
		}
	}
	return games, false, nil
}
