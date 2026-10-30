// Package exportclient builds frontend output from local ROMs and a remote catalog.
package exportclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
	"rommeta/internal/platformnames"
)

type Options struct {
	exporter.BatchOptions
	API, Token string
	HTTP       *http.Client
}

type Job struct {
	Batch    *exporter.BatchPlan
	Single   bool
	client   *client
	assets   map[string]catalogapi.ExportAsset
	stamps   map[string]os.FileInfo
	progress func(string, string, int, int)
}

type client struct {
	origin, token string
	http          *http.Client
	revision      string
}

func newClient(origin, token string, provided *http.Client) (*client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("--api must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if token != "" && len(token) < 32 {
		return nil, fmt.Errorf("ROMMETA_API_TOKEN, when set, needs at least 32 characters")
	}
	h := &http.Client{}
	if provided != nil {
		*h = *provided
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("API redirects are not followed") }
	return &client{origin: strings.TrimSuffix(origin, "/"), token: token, http: h}, nil
}

type apiError struct {
	status int
	catalogapi.ExportError
}

func (e *apiError) Error() string { return fmt.Sprintf("catalog API HTTP %d: %s", e.status, e.Message) }

func (c *client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var data io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if len(encoded) > 32<<20 {
			return nil, fmt.Errorf("export request exceeds 32 MiB")
		}
		data = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, data)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog API request failed: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		result := &apiError{status: response.StatusCode}
		if json.Unmarshal(message, &result.ExportError) != nil || result.Message == "" {
			result.Message = strings.TrimSpace(string(message))
		}
		return nil, result
	}
	return response, nil
}
func (c *client) json(ctx context.Context, method, path string, body, result any) error {
	response, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	limited := &io.LimitedReader{R: response.Body, N: (64 << 20) + 1}
	decoder := json.NewDecoder(limited)
	if err = decoder.Decode(result); err != nil {
		return fmt.Errorf("invalid catalog response: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || limited.N == 0 {
		return fmt.Errorf("catalog response has trailing or excessive JSON")
	}
	return nil
}

func real(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func outputPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := absolute
	tail := []string{}
	for {
		_, err = os.Lstat(parent)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		tail = append(tail, filepath.Base(parent))
		parent = next
	}
	parent, err = real(parent)
	if err != nil {
		return "", err
	}
	for i := len(tail) - 1; i >= 0; i-- {
		parent = filepath.Join(parent, tail[i])
	}
	return parent, nil
}

func (c *client) platforms(ctx context.Context) (*exporter.PlatformSelector, error) {
	var capabilities struct {
		SchemaVersion int                       `json:"schema_version"`
		ExportVersion int                       `json:"export_schema_version"`
		Mode          string                    `json:"mode"`
		Platforms     []exporter.LookupPlatform `json:"platforms"`
	}
	if err := c.json(ctx, "GET", "/v1/platforms", nil, &capabilities); err != nil {
		return nil, err
	}
	if capabilities.SchemaVersion != 1 || capabilities.ExportVersion != 1 || capabilities.Mode != "catalog" || capabilities.Platforms == nil {
		return nil, fmt.Errorf("API must serve catalog mode with network export capabilities")
	}
	return exporter.NewPlatformSelector(platformnames.DefaultRegistry(), capabilities.Platforms), nil
}

// ListPlatforms reads capabilities only; it never observes ROMs or fetches assets.
func ListPlatforms(ctx context.Context, options Options) (*exporter.PlatformSelector, error) {
	c, err := newClient(options.API, options.Token, options.HTTP)
	if err != nil {
		return nil, err
	}
	return c.platforms(ctx)
}

func Build(ctx context.Context, options Options) (*Job, error) {
	target, err := exporter.NormalizeTarget(options.Target)
	if err != nil {
		return nil, err
	}
	modes := 0
	if options.Platform != "" {
		modes++
	}
	if options.All {
		modes++
	}
	if len(options.Systems) > 0 {
		modes++
	}
	if modes != 1 || options.ROMRoot == "" || options.Out == "" || options.Limit < 0 {
		return nil, fmt.Errorf("network export needs --rom-root, --out and exactly one of --platform, --platforms or --all")
	}
	c, err := newClient(options.API, options.Token, options.HTTP)
	if err != nil {
		return nil, err
	}
	root, err := real(options.ROMRoot)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("ROM root must be a directory")
	}
	out, err := outputPath(options.Out)
	if err != nil {
		return nil, err
	}
	if within(root, out) || within(out, root) {
		return nil, fmt.Errorf("output must be outside ROM trees")
	}
	selector, err := c.platforms(ctx)
	if err != nil {
		return nil, err
	}
	profile := ""
	if options.ProfilePath != "" {
		data, err := os.ReadFile(options.ProfilePath)
		if err != nil {
			return nil, err
		}
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("export profile exceeds 1 MiB")
		}
		profile = string(data)
		if err = exporter.ValidateExportProfile(profile); err != nil {
			return nil, err
		}
	}
	profile, launchBoxPolicy, err := exporter.ClientExportProfile(profile)
	if err != nil {
		return nil, err
	}
	batch := &exporter.BatchPlan{SchemaVersion: 1, Target: target, ROMRoot: root, Out: out, Plans: []*exporter.Plan{}, Skipped: []exporter.SkippedSystem{}}
	job := &Job{Batch: batch, Single: options.Platform != "", client: c, assets: map[string]catalogapi.ExportAsset{}, stamps: map[string]os.FileInfo{}, progress: options.Progress}
	var selectedFolders []exporter.FolderSelection
	if job.Single {
		selection, catalog, err := selector.Single(options.Platform, root)
		if err != nil {
			return nil, err
		}
		selectedFolders = []exporter.FolderSelection{{Selection: selection, Catalog: catalog}}
	} else {
		selectedFolders, batch.Skipped, err = selector.Folders(root, options.Systems, options.All)
		if err != nil {
			return nil, err
		}
	}
	for _, chosen := range selectedFolders {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		selection := chosen.Selection
		system, folder := selection.System, selection.Folder
		selected := struct{ platform, system string }{chosen.Catalog, system}
		skip := func(reason string) {
			batch.Skipped = append(batch.Skipped, exporter.SkippedSystem{System: filepath.Base(folder), Reason: reason})
		}
		child := options.Options
		child.Context = ctx
		child.ROMRoot, child.System = folder, system
		if options.Progress != nil {
			child.Progress = func(stage, name string, current, total int) {
				options.Progress(system+" / "+stage, name, current, total)
			}
		}
		observations, err := exporter.ObserveExport(child)
		if err != nil {
			if !job.Single && exporter.IsEmptyExport(err) {
				skip(err.Error())
				continue
			}
			return nil, err
		}
		for _, input := range observations {
			if err = job.remember(folder, input); err != nil {
				return nil, err
			}
		}
		plan := &exporter.Plan{PlatformSelection: selection, SchemaVersion: 1, Target: "es-de", Platform: selected.platform, System: system, ROMRoot: folder, Out: out, SourceAPI: c.origin, Records: []exporter.Record{}}
		for offset := 0; offset < len(observations); offset += 100 {
			end := offset + 100
			if end > len(observations) {
				end = len(observations)
			}
			if options.Progress != nil {
				options.Progress(system+" / API", selected.platform, end, len(observations))
			}
			request := catalogapi.ExportRequest{Platform: selected.platform, System: system, Inputs: observations[offset:end], ArcadeSnapshot: options.ArcadeSnapshot, Profile: profile, Revision: c.revision}
			var response catalogapi.ExportResponse
			if err = c.json(ctx, "POST", "/v1/export", request, &response); err != nil {
				var apiErr *apiError
				if !job.Single && offset == 0 && errors.As(err, &apiErr) && apiErr.status == http.StatusConflict && apiErr.Code == "snapshot_required" {
					skip("MAME snapshot selection required; use --arcade-snapshot")
					plan = nil
					break
				}
				return nil, err
			}
			if response.SchemaVersion != 1 || response.Revision == "" || response.Plan == nil || response.Plan.Platform != selected.platform || response.Plan.System != system || response.Plan.Target != "es-de" || len(response.Plan.Records) != end-offset {
				return nil, fmt.Errorf("API export response does not match selected inputs")
			}
			if c.revision != "" && c.revision != response.Revision {
				return nil, fmt.Errorf("catalog changed between export requests")
			}
			c.revision = response.Revision
			if offset == 0 {
				plan.Profile = response.Plan.Profile
			} else if !reflect.DeepEqual(plan.Profile, response.Plan.Profile) {
				return nil, fmt.Errorf("export policy changed between batches")
			}
			for i, record := range response.Plan.Records {
				if err = validateRecord(observations[offset+i], &record, system, response.Assets); err != nil {
					return nil, err
				}
				plan.Records = append(plan.Records, record)
				for _, media := range record.Media {
					asset := response.Assets[media.SourcePath]
					if old, ok := job.assets[media.SourcePath]; ok && old != asset {
						return nil, fmt.Errorf("selected source asset changed during export")
					}
					job.assets[media.SourcePath] = asset
				}
			}
		}
		if plan != nil {
			plan.Profile.LaunchBox = launchBoxPolicy
			if err = exporter.FinalizeExport(plan); err != nil {
				return nil, err
			}
			if err = exporter.ProjectTarget(plan, target); err != nil {
				return nil, err
			}
			batch.Plans = append(batch.Plans, plan)
		}
	}
	return job, nil
}

func validateRecord(input exporter.ExportInput, record *exporter.Record, system string, assets map[string]catalogapi.ExportAsset) error {
	if record.ROM != input.ROM || !exporter.ValidInputPath(record.ROM) {
		return fmt.Errorf("API changed ROM identity")
	}
	switch record.Status {
	case "matched", "unmatched", "ambiguous", "unsupported":
	default:
		return fmt.Errorf("invalid export status")
	}
	if input.Kind == "unsupported" {
		if record.Status != "unsupported" || record.SHA1 != "" || len(record.Playlist) > 0 || len(record.Media) > 0 {
			return fmt.Errorf("API changed unsupported input")
		}
	} else {
		if record.Size != input.Proof.Size || record.SHA1 != input.Proof.SHA1 || record.Member != input.Proof.Member {
			return fmt.Errorf("API changed local input proof")
		}
		switch input.Kind {
		case "payload":
			if !reflect.DeepEqual(input.Disc, record.Disc) {
				return fmt.Errorf("API changed CHD header observation")
			}
			if record.Payload == nil || !record.Payload.Equal(input.Proof) {
				return fmt.Errorf("API changed payload evidence")
			}
		case "arcade":
			if record.Status == "matched" && record.MatchedBy != "arcade-set-name" {
				return fmt.Errorf("MAME set name cannot become payload proof")
			}
		case "serial":
			if record.Status == "matched" && record.MatchedBy != "name" && record.MatchedBy != "game-name" {
				return fmt.Errorf("serial title cannot become payload proof")
			}
		}
	}
	if input.Kind == "playlist" {
		record.LaunchFile = filepath.ToSlash(filepath.Join(input.ROM, filepath.Base(input.ROM)))
		if len(record.Playlist) > len(input.Parts) || record.Status == "matched" && len(record.Playlist) != len(input.Parts) {
			return fmt.Errorf("API changed playlist membership")
		}
		for i, part := range record.Playlist {
			proof := input.Parts[i]
			if part.ROM != proof.ROM || part.Size != proof.Proof.Size || part.SHA1 != proof.Proof.SHA1 {
				return fmt.Errorf("API changed playlist member")
			}
		}
	} else if record.LaunchFile != "" || len(record.Playlist) > 0 {
		return fmt.Errorf("unexpected API playlist")
	}
	for role, media := range record.Media {
		if record.Status != "matched" || !exporter.ValidInputPath(record.Game) || !exporter.ValidInputPath(media.SourcePath) || !strings.HasPrefix(media.SourcePath, record.Game+"/") {
			return fmt.Errorf("invalid selected media source")
		}
		asset, ok := assets[media.SourcePath]
		digest, err := hex.DecodeString(asset.SHA256)
		id := strings.TrimPrefix(asset.URI, "/v1/assets/")
		if !ok || len(digest) != 32 || err != nil || len(id) != 64 || asset.URI != "/v1/assets/"+id || asset.Size != media.Size || asset.Size < 0 || media.SHA256 != asset.SHA256 {
			return fmt.Errorf("invalid SHA-bound export asset")
		}
		if _, err = hex.DecodeString(id); err != nil {
			return fmt.Errorf("invalid asset URI")
		}
		media.OutputPath, err = exporter.ExportOutputPath(system, *record, role, filepath.Ext(media.SourcePath))
		if err != nil {
			return err
		}
		record.Media[role] = media
	}
	return nil
}

func (j *Job) remember(root string, input exporter.ExportInput) error {
	if input.Kind == "unsupported" {
		return nil
	}
	paths := []string{input.ROM}
	if input.Kind == "playlist" {
		paths[0] = filepath.ToSlash(filepath.Join(input.ROM, filepath.Base(input.ROM)))
		for _, part := range input.Parts {
			paths = append(paths, part.ROM)
		}
	}
	for _, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("ROM input changed during observation")
		}
		j.stamps[path] = info
	}
	return nil
}
func (j *Job) fresh() error {
	for path, before := range j.stamps {
		now, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !now.Mode().IsRegular() || !os.SameFile(before, now) || before.Size() != now.Size() || !before.ModTime().Equal(now.ModTime()) {
			return fmt.Errorf("ROM changed since planning: %s", filepath.Base(path))
		}
	}
	return nil
}

// Apply streams only selected media into a temporary cache and reuses the normal
// staged writer. It never installs a frontend or changes input files.
func (j *Job) Apply(ctx context.Context) error {
	if len(j.Batch.Plans) == 0 {
		return fmt.Errorf("no systems with selected inputs; no package created")
	}
	if _, err := os.Lstat(j.Batch.Out); err == nil {
		return fmt.Errorf("output already exists; choose a new directory")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := j.fresh(); err != nil {
		return err
	}
	cache, err := os.MkdirTemp("", "rommeta-export-media-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cache)
	paths := make([]string, 0, len(j.assets))
	for path := range j.assets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	mediaPaths := map[string]string{}
	downloaded := map[string]int64{}
	for i, path := range paths {
		if err = ctx.Err(); err != nil {
			return err
		}
		asset := j.assets[path]
		if j.progress != nil {
			j.progress("download", path, i+1, len(paths))
		}
		if size, ok := downloaded[asset.SHA256]; ok {
			if size != asset.Size {
				return fmt.Errorf("inconsistent asset size")
			}
		} else {
			response, err := j.client.request(ctx, "GET", asset.URI, nil)
			if err != nil {
				return err
			}
			file, err := os.OpenFile(filepath.Join(cache, asset.SHA256), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				response.Body.Close()
				return err
			}
			hash := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, asset.Size+1))
			closeErr := file.Close()
			response.Body.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if n != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
				return fmt.Errorf("asset size/SHA256 differs from export plan")
			}
			downloaded[asset.SHA256] = asset.Size
		}
		mediaPaths[path] = asset.SHA256
	}
	if err = j.fresh(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for _, plan := range j.Batch.Plans {
		plan.MediaRoot, plan.MediaPaths, plan.Context = cache, mediaPaths, ctx
	}
	if j.Single {
		return exporter.Apply(j.Batch.Plans[0], j.progress)
	}
	return exporter.ApplyBatch(j.Batch, j.progress)
}
