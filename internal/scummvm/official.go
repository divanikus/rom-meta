package scummvm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
)

const officialRepo = "https://github.com/scummvm/scummvm-web"
const officialHead = "https://api.github.com/repos/scummvm/scummvm-web/commits/master"
const officialRaw = "https://raw.githubusercontent.com/scummvm/scummvm-web/"

var officialFiles = []string{"games.yaml", "compatibility.yaml", "versions.yaml", "engines.yaml"}
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var snapshotPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type OfficialSource struct {
	URL        string            `json:"url"`
	Revision   string            `json:"revision"`
	Release    string            `json:"release"`
	Excluded   int               `json:"excluded_entries"`
	FileSHA256 map[string]string `json:"file_sha256"`
}

type snapshotManifest struct {
	Version  string          `json:"version"`
	SHA256   string          `json:"list_sha256"`
	Command  string          `json:"command"`
	Count    int             `json:"listed_games"`
	Official *OfficialSource `json:"official,omitempty"`
}

type snapshotFile struct {
	name     string
	contents []byte
}

func loadOfficial(options ListOptions) (List, error) {
	if options.Offline {
		return loadOfficialCache(options)
	}
	revision := options.Revision
	if revision != "" && !revisionPattern.MatchString(revision) {
		return List{}, fmt.Errorf("--revision must be a full lowercase 40-character ScummVM website commit SHA")
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if revision == "" {
		var err error
		revision, err = resolveOfficialRevision(ctx, client)
		if err != nil {
			return List{}, err
		}
	}
	files := map[string][]byte{}
	for _, name := range officialFiles {
		contents, err := officialGET(ctx, client, officialRaw+revision+"/data/en/"+name)
		if err != nil {
			return List{}, err
		}
		files[name] = contents
	}
	return parseOfficial(files, revision, options.Release, time.Now())
}

func officialGET(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "RomMeta")
	switch url {
	case officialRefs:
		request.Header.Set("Accept", officialRefsMediaType)
	case officialHead:
		request.Header.Set("Accept", "application/vnd.github+json")
	default:
		request.Header.Set("Accept", "application/octet-stream")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download official ScummVM data: %w; --offline reuses a previously applied official snapshot", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official ScummVM data returned HTTP %d for %s", response.StatusCode, url)
	}
	if url == officialRefs {
		mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || mediaType != officialRefsMediaType {
			return nil, fmt.Errorf("official ScummVM Git refs returned an unexpected content type")
		}
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxListBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxListBytes {
		return nil, fmt.Errorf("official ScummVM response exceeds 16 MiB")
	}
	return contents, nil
}

type websiteGame struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Engine string `yaml:"engine_id"`
}
type websiteEngine struct {
	ID      string `yaml:"id"`
	Enabled bool   `yaml:"enabled"`
}
type websiteVersion struct {
	ID   string `yaml:"id"`
	Date string `yaml:"release_date"`
}
type websiteCompatibility struct {
	ID      string `yaml:"id"`
	Support string `yaml:"support"`
	Since   string `yaml:"since_version"`
}

// The website's history is cumulative: select the newest compatibility record
// not later than the requested release, using official release dates.
func parseOfficial(files map[string][]byte, revision, release string, now time.Time) (List, error) {
	var games []websiteGame
	var engines []websiteEngine
	var versions []websiteVersion
	var history []websiteCompatibility
	for _, data := range []struct {
		name   string
		target any
	}{
		{"games.yaml", &games}, {"engines.yaml", &engines},
		{"versions.yaml", &versions}, {"compatibility.yaml", &history},
	} {
		if len(files[data.name]) == 0 {
			return List{}, fmt.Errorf("missing official %s", data.name)
		}
		if err := yaml.Unmarshal(files[data.name], data.target); err != nil {
			return List{}, fmt.Errorf("parse official %s: %w", data.name, err)
		}
	}
	dates := map[string]time.Time{}
	stable := map[string]bool{}
	latest := ""
	for _, version := range versions {
		if version.ID == "DEV" {
			continue
		}
		_, versionErr := numericVersion(version.ID)
		stable[version.ID] = versionErr == nil
		date, err := time.Parse("January 2, 2006", version.Date)
		if err != nil {
			return List{}, fmt.Errorf("invalid official release date for %s", version.ID)
		}
		if _, exists := dates[version.ID]; exists {
			return List{}, fmt.Errorf("duplicate official release %s", version.ID)
		}
		dates[version.ID] = date
		if stable[version.ID] && !date.After(now.UTC()) && (latest == "" || laterVersion(version.ID, latest, dates)) {
			latest = version.ID
		}
	}
	if release == "" || release == "latest" {
		release = latest
	}
	selectedDate, ok := dates[release]
	if !ok || !stable[release] || selectedDate.After(now.UTC()) {
		return List{}, fmt.Errorf("unknown or unreleased stable ScummVM release %q", release)
	}
	enabled := map[string]bool{}
	for _, engine := range engines {
		if !engineID.MatchString(engine.ID) {
			return List{}, fmt.Errorf("invalid official engine ID %q", engine.ID)
		}
		if _, exists := enabled[engine.ID]; exists {
			return List{}, fmt.Errorf("duplicate official engine %s", engine.ID)
		}
		enabled[engine.ID] = engine.Enabled
	}
	byID := map[string]websiteGame{}
	for _, game := range games {
		if !qualifiedID.MatchString(game.ID) || len(game.ID) > 100 || strings.TrimSpace(game.Name) == "" ||
			strings.ContainsAny(game.Name, "\r\n\t") || strings.SplitN(game.ID, ":", 2)[0] != game.Engine {
			return List{}, fmt.Errorf("invalid official game %q", game.ID)
		}
		if _, exists := byID[game.ID]; exists {
			return List{}, fmt.Errorf("duplicate official game %s", game.ID)
		}
		byID[game.ID] = game
	}
	states := map[string]websiteCompatibility{}
	for _, state := range history {
		switch state.Support {
		case "good", "excellent", "bugged", "broken", "untested":
		default:
			return List{}, fmt.Errorf("unknown official support level %q for %s", state.Support, state.ID)
		}
		if state.Since == "DEV" {
			continue
		}
		date, exists := dates[state.Since]
		if !exists {
			return List{}, fmt.Errorf("unknown official compatibility release %s", state.Since)
		}
		if date.After(selectedDate) || (date.Equal(selectedDate) && laterVersion(state.Since, release, dates)) {
			continue
		}
		old, exists := states[state.ID]
		if !exists || laterVersion(state.Since, old.Since, dates) {
			states[state.ID] = state
		} else if state.Since == old.Since && state.Support != old.Support {
			return List{}, fmt.Errorf("conflicting official compatibility for %s at %s", state.ID, state.Since)
		}
	}
	result := List{Version: "ScummVM " + release + "; website " + revision, Official: &OfficialSource{
		URL: officialRepo, Revision: revision, Release: release, FileSHA256: map[string]string{},
	}, SourceFiles: files}
	for id, state := range states {
		game, exists := byID[id]
		if !exists {
			return List{}, fmt.Errorf("official compatibility references missing game %s", id)
		}
		if !enabled[game.Engine] || state.Support == "broken" || state.Support == "untested" {
			result.Official.Excluded++
			continue
		}
		result.Games = append(result.Games, Game{ID: id, Title: game.Name, Support: state.Support})
	}
	if len(result.Games) == 0 {
		return List{}, fmt.Errorf("official ScummVM release %s has no supported games", release)
	}
	sort.Slice(result.Games, func(i, j int) bool { return result.Games[i].ID < result.Games[j].ID })
	var normalized strings.Builder
	for _, game := range result.Games {
		fmt.Fprintf(&normalized, "%s %s\n", game.ID, game.Title)
	}
	result.Contents = []byte(normalized.String())
	for _, name := range officialFiles {
		sum := sha256.Sum256(files[name])
		result.Official.FileSHA256[name] = hex.EncodeToString(sum[:])
	}
	return result, nil
}

func numericVersion(value string) ([3]int, error) {
	var version [3]int
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version, fmt.Errorf("invalid official release %q", value)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version, fmt.Errorf("invalid official release %q", value)
		}
		version[i] = n
	}
	return version, nil
}

func laterVersion(left, right string, dates map[string]time.Time) bool {
	if !dates[left].Equal(dates[right]) {
		return dates[left].After(dates[right])
	}
	l, leftErr := numericVersion(left)
	r, rightErr := numericVersion(right)
	if leftErr != nil || rightErr != nil {
		if (leftErr == nil) != (rightErr == nil) {
			return leftErr == nil
		}
		return left > right
	}
	for i := range l {
		if l[i] != r[i] {
			return l[i] > r[i]
		}
	}
	return false
}

func officialPointer(root string) string {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	return filepath.Join(root, ".rommeta", "sources", "scummvm", "latest-official.txt")
}

func loadOfficialCache(options ListOptions) (List, error) {
	pointer := officialPointer(options.CacheRoot)
	contents, err := catalogfs.ReadFile(pointer)
	if err != nil {
		return List{}, fmt.Errorf("no readable official ScummVM snapshot: %w; run online with --apply first", err)
	}
	key := strings.TrimSpace(string(contents))
	if !snapshotPattern.MatchString(key) {
		return List{}, fmt.Errorf("invalid official ScummVM cache pointer")
	}
	path := filepath.Join(filepath.Dir(pointer), key)
	contents, err = catalogfs.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return List{}, err
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return List{}, err
	}
	source := manifest.Official
	if source == nil || source.URL != officialRepo || !revisionPattern.MatchString(source.Revision) {
		return List{}, fmt.Errorf("invalid cached official ScummVM provenance")
	}
	if options.Release != "" && options.Release != "latest" && options.Release != source.Release {
		return List{}, fmt.Errorf("cached release is %s; run online to get %s", source.Release, options.Release)
	}
	if options.Revision != "" && options.Revision != source.Revision {
		return List{}, fmt.Errorf("cached website revision does not match --revision")
	}
	files := map[string][]byte{}
	for _, name := range officialFiles {
		contents, err := catalogfs.ReadFile(filepath.Join(path, name))
		if err != nil {
			return List{}, err
		}
		hash := sha256.Sum256(contents)
		if hex.EncodeToString(hash[:]) != source.FileSHA256[name] {
			return List{}, fmt.Errorf("cached ScummVM %s checksum differs", name)
		}
		files[name] = contents
	}
	list, err := parseOfficial(files, source.Revision, source.Release, time.Now())
	if err != nil {
		return List{}, err
	}
	hash := sha256.Sum256(list.Contents)
	if list.Version != manifest.Version || hex.EncodeToString(hash[:]) != manifest.SHA256 ||
		manifest.Count != len(list.Games) || snapshotKey(list.Version, manifest.SHA256) != key {
		return List{}, fmt.Errorf("cached official ScummVM manifest differs")
	}
	saved, err := catalogfs.ReadFile(filepath.Join(path, "games.txt"))
	if err != nil || !bytes.Equal(saved, list.Contents) {
		return List{}, fmt.Errorf("cached ScummVM normalized game list differs")
	}
	return list, nil
}

func snapshotKey(version, digest string) string {
	hash := sha256.Sum256([]byte(version + "\n" + digest))
	return hex.EncodeToString(hash[:])
}

func snapshotFiles(list List, digest string) ([]snapshotFile, error) {
	command := "scummvm --list-games"
	if list.Official != nil {
		command = "official website compatibility data"
	}
	contents, err := json.MarshalIndent(snapshotManifest{list.Version, digest, command, len(list.Games), list.Official}, "", "  ")
	if err != nil {
		return nil, err
	}
	files := []snapshotFile{{"games.txt", list.Contents}, {"manifest.json", append(contents, '\n')}}
	if list.Official != nil {
		for _, name := range officialFiles {
			files = append(files, snapshotFile{name, list.SourceFiles[name]})
		}
	}
	return files, nil
}
