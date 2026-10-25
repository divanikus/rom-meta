// Package romm applies a bounded offline catalog projection through RomM's public API.
package romm

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
)

const maxResponse = 32 << 20
const maxCover = 64 << 20

type Client struct {
	base, token string
	http        *http.Client
}

func NewClient(server, token string) (*Client, error) {
	u, e := url.Parse(server)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server must be an origin URL without credentials, path, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, fmt.Errorf("RomM requires HTTPS (HTTP is allowed only on loopback)")
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("set ROMM_TOKEN to a RomM bearer token")
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), token: token, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) request(ctx context.Context, method, route, contentType string, body io.Reader, out any, uploadName ...string) error {
	req, e := http.NewRequestWithContext(ctx, method, c.base+"/api"+route, body)
	if e != nil {
		return fmt.Errorf("build RomM request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if len(uploadName) > 0 {
		req.Header.Set("X-Upload-Filename", uploadName[0])
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, e := c.http.Do(req)
	if e != nil {
		return fmt.Errorf("RomM %s request failed (network/TLS/timeout)", method)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("RomM %s returned HTTP %d", method, response.StatusCode)
	}
	payload, e := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if e != nil || len(payload) > maxResponse {
		return fmt.Errorf("RomM response unreadable or too large")
	}
	if out != nil {
		if e = json.Unmarshal(payload, out); e != nil {
			return fmt.Errorf("invalid RomM JSON response")
		}
	}
	return nil
}

type Platform struct {
	ID     int    `json:"id"`
	FSSlug string `json:"fs_slug"`
}
type ROM struct {
	ID             int    `json:"id"`
	PlatformID     int    `json:"platform_id"`
	PlatformFSSlug string `json:"platform_fs_slug"`
	FSPath         string `json:"fs_path"`
	FSName         string `json:"fs_name"`
	FSSize         int64  `json:"fs_size_bytes"`
}
type FileState struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Entry struct {
	Providers  *ProviderEvidence          `json:"providers,omitempty"`
	Manual     *FileState                 `json:"manual,omitempty"`
	DateRepair *DateRepair                `json:"date_repair,omitempty"`
	ROM        ROM                        `json:"romm_rom"`
	Source     exporter.Record            `json:"source"`
	Input      FileState                  `json:"input"`
	Cover      *FileState                 `json:"cover,omitempty"`
	Before     map[string]json.RawMessage `json:"before"`
	BeforeHash string                     `json:"before_hash"`
	Form       map[string]string          `json:"form"`
}
type Skip struct {
	ROM    string `json:"rom"`
	Reason string `json:"reason"`
}
type Plan struct {
	AvailableMedia []CatalogAsset   `json:"available_media,omitempty"`
	Operation      string           `json:"operation,omitempty"`
	SchemaVersion  int              `json:"schema_version"`
	Target         string           `json:"target"`
	Server         string           `json:"server"`
	DataRoot       string           `json:"data_root"`
	ROMRoot        string           `json:"rom_root"`
	Platform       Platform         `json:"platform"`
	Prefix         string           `json:"romm_rom_root"`
	Entries        []Entry          `json:"entries"`
	Skipped        []Skip           `json:"skipped"`
	ManualBytes    int64            `json:"manual_bytes"`
	CoverBytes     int64            `json:"cover_bytes"`
	Profile        exporter.Profile `json:"selection_profile"`
}

func (c *Client) Detail(ctx context.Context, id int) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	e := c.request(ctx, http.MethodGet, fmt.Sprintf("/roms/%d", id), "", nil, &out)
	return out, e
}
func decodeROM(raw map[string]json.RawMessage) (ROM, error) {
	b, e := json.Marshal(raw)
	if e != nil {
		return ROM{}, e
	}
	var r ROM
	e = json.Unmarshal(b, &r)
	return r, e
}
func (c *Client) inventory(ctx context.Context, platform Platform) ([]ROM, error) {
	var all []ROM
	for offset := 0; offset < 50000; {
		var page struct {
			Items []ROM `json:"items"`
			Total *int  `json:"total"`
		}
		query := fmt.Sprintf("/roms?platform_ids=%d&limit=1000&offset=%d&with_char_index=false&with_filter_values=false&with_rom_id_index=false&order_by=id", platform.ID, offset)
		if e := c.request(ctx, http.MethodGet, query, "", nil, &page); e != nil {
			return nil, e
		}
		for _, r := range page.Items {
			if r.PlatformID != platform.ID || r.PlatformFSSlug != platform.FSSlug {
				return nil, fmt.Errorf("inventory returned a different platform")
			}
		}
		all = append(all, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || (page.Total != nil && offset >= *page.Total) {
			return all, nil
		}
	}
	return nil, fmt.Errorf("inventory exceeds the bounded adapter limit")
}
func cleanRelative(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, ":") {
		return "", fmt.Errorf("expected a clean relative POSIX path")
	}
	return value, nil
}
func fileState(root, name string, limit int64) (FileState, error) {
	realRoot, e := catalogfs.EvalSymlinks(root)
	if e != nil {
		return FileState{}, e
	}
	info, e := catalogfs.Lstat(name)
	if e != nil || !info.Mode().IsRegular() {
		return FileState{}, fmt.Errorf("source is not a regular file")
	}
	real, e := catalogfs.EvalSymlinks(name)
	if e != nil {
		return FileState{}, e
	}
	rel, e := filepath.Rel(realRoot, real)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return FileState{}, fmt.Errorf("source escapes its input root")
	}
	if limit > 0 && info.Size() > limit {
		return FileState{}, fmt.Errorf("source exceeds adapter size limit")
	}
	f, e := catalogfs.Open(real)
	if e != nil {
		return FileState{}, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	if e != nil || n != info.Size() {
		return FileState{}, fmt.Errorf("source changed while reading")
	}
	after, e := f.Stat()
	if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return FileState{}, fmt.Errorf("source changed while reading")
	}
	return FileState{Path: real, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func rawString(m map[string]json.RawMessage, key string) string {
	var v string
	_ = json.Unmarshal(m[key], &v)
	return v
}
func nonemptyRaw(v json.RawMessage) bool {
	s := strings.TrimSpace(string(v))
	return s != "" && s != "null" && s != "[]" && s != "{}" && s != "\"\""
}
func snapshotHash(m map[string]json.RawMessage) string {
	// updated_at catches concurrent library edits; per-user state is deliberately excluded.
	keys := []string{"id", "platform_id", "platform_fs_slug", "fs_path", "fs_name", "fs_size_bytes", "name", "name_sort_key", "summary", "manual_metadata", "path_cover_small", "path_cover_large", "url_cover", "locked_fields", "updated_at", "igdb_id", "ss_id", "launchbox_id", "moby_id", "hasheous_id", "gamelist_id", "libretro_id", "path_manual", "url_manual", "has_manual", "tgdb_id", "ra_id", "igdb_metadata", "ss_metadata", "launchbox_metadata", "hasheous_metadata"}
	selected := map[string]json.RawMessage{}
	for _, key := range keys {
		if v, ok := m[key]; ok {
			selected[key] = v
		}
	}
	b, _ := json.Marshal(selected)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func proposedForm(r exporter.Record, before map[string]json.RawMessage) map[string]string {
	form := map[string]string{}
	name := rawString(before, "name")
	title := r.Fields["title"].Value
	if !fieldLocked(before, "name") && title != "" && title != name && (name == "" || name == rawString(before, "fs_name") || name == rawString(before, "fs_name_no_ext") || name == rawString(before, "fs_name_no_tags")) {
		form["name"] = title
	}
	if text := r.Fields["description"].Value; !fieldLocked(before, "summary") && text != "" && strings.TrimSpace(rawString(before, "summary")) == "" {
		form["summary"] = text
	}
	metadata := map[string]json.RawMessage{}
	_ = json.Unmarshal(before["manual_metadata"], &metadata)
	if metadata == nil {
		metadata = map[string]json.RawMessage{}
	}
	if !knownManualFields(metadata) {
		return form
	}
	changed := false
	for source, target := range map[string]string{"developer": "developers", "publisher": "publishers", "genres": "genres"} {
		text := r.Fields[source].Value
		if text == "" || nonemptyRaw(metadata[target]) {
			continue
		}
		values := []string{text}
		if source == "genres" {
			values = strings.Split(text, " / ")
		}
		metadata[target], _ = json.Marshal(values)
		changed = true
	}
	if text := r.Fields["release_date"].Value; text != "" && !nonemptyRaw(metadata["first_release_date"]) {
		if date, e := time.Parse("2006-01-02", text); e == nil {
			metadata["first_release_date"], _ = json.Marshal(date.UnixMilli())
			changed = true
		}
	}
	if changed {
		b, _ := json.Marshal(metadata)
		form["raw_manual_metadata"] = string(b)
	}
	return form
}
func coverWritable(before map[string]json.RawMessage) bool {
	for _, k := range []string{"path_cover_small", "path_cover_large", "url_cover"} {
		if strings.TrimSpace(rawString(before, k)) != "" {
			return false
		}
	}
	var locked []string
	_ = json.Unmarshal(before["locked_fields"], &locked)
	for _, k := range locked {
		if k == "url_cover" {
			return false
		}
	}
	return true
}
func sameROM(a, b ROM) bool { return a == b }

// Build reads the API and existing files; it never writes the server or copies media.
func Build(ctx context.Context, c *Client, source *exporter.Plan, fsSlug, prefix string) (*Plan, error) {
	if _, e := cleanRelative(prefix); e != nil {
		return nil, e
	}
	for role := range source.Profile.Selection.Media {
		if role != "cover" && role != "manual" {
			return nil, fmt.Errorf("RomM stock API profile permits only cover and manual media roles; other roles need a supported transfer strategy")
		}
	}
	var platforms []Platform
	if e := c.request(ctx, http.MethodGet, "/platforms", "", nil, &platforms); e != nil {
		return nil, e
	}
	var selected []Platform
	for _, p := range platforms {
		if p.FSSlug == fsSlug {
			selected = append(selected, p)
		}
	}
	if len(selected) != 1 || selected[0].ID < 1 {
		return nil, fmt.Errorf("expected exactly one RomM platform with the requested fs_slug")
	}
	platform := selected[0]
	inventory, e := c.inventory(ctx, platform)
	if e != nil {
		return nil, e
	}
	byPath := map[string][]ROM{}
	ids := map[int]bool{}
	for _, r := range inventory {
		if r.ID < 1 || ids[r.ID] {
			return nil, fmt.Errorf("invalid or duplicate ROM inventory id")
		}
		ids[r.ID] = true
		if _, e := cleanRelative(r.FSPath); e != nil {
			return nil, fmt.Errorf("invalid inventory path")
		}
		if strings.ContainsAny(r.FSName, "/\\") || r.FSName == "" {
			return nil, fmt.Errorf("invalid inventory filename")
		}
		key := path.Join(r.FSPath, r.FSName)
		byPath[key] = append(byPath[key], r)
	}
	plan := &Plan{SchemaVersion: 1, Target: "romm", Server: c.base, DataRoot: source.DataRoot, ROMRoot: source.ROMRoot, Platform: platform, Prefix: prefix, Profile: source.Profile, Entries: []Entry{}, Skipped: []Skip{}}
	used := map[int]bool{}
	for _, record := range source.Records {
		if record.Status != "matched" || record.MatchedBy != "hash" || record.Hidden || len(record.Playlist) > 0 {
			plan.Skipped = append(plan.Skipped, Skip{record.ROM, "pilot requires an unambiguous single-file hash match"})
			continue
		}
		rel, e := cleanRelative(filepath.ToSlash(record.ROM))
		if e != nil {
			return nil, e
		}
		matches := byPath[path.Join(prefix, rel)]
		if len(matches) != 1 {
			return nil, fmt.Errorf("ROM has no unique exact inventory path: %s", rel)
		}
		rom := matches[0]
		if used[rom.ID] {
			return nil, fmt.Errorf("multiple inputs map to one RomM ROM")
		}
		used[rom.ID] = true
		input, e := sourceRecordState(source.ROMRoot, record)
		if e != nil {
			return nil, fmt.Errorf("ROM input %s: %w", rel, e)
		}
		if input.Size != rom.FSSize {
			return nil, fmt.Errorf("ROM size differs from inventory: %s", rel)
		}
		before, e := c.Detail(ctx, rom.ID)
		if e != nil {
			return nil, e
		}
		detail, e := decodeROM(before)
		if e != nil || !sameROM(rom, detail) {
			return nil, fmt.Errorf("ROM inventory changed during planning")
		}
		entry := Entry{ROM: rom, Source: record, Input: input, Before: before, BeforeHash: snapshotHash(before), Form: proposedForm(record, before)}
		// Manifest provenance keeps selected catalog values and source media; the API receives no provider IDs.
		if media, ok := record.Media["cover"]; ok && coverWritable(before) {
			cover, e := fileState(source.DataRoot, filepath.Join(source.DataRoot, filepath.FromSlash(media.SourcePath)), maxCover)
			if e != nil {
				return nil, fmt.Errorf("cover for %s: %w", rel, e)
			}
			if cover.Size != media.Size {
				return nil, fmt.Errorf("cover size changed after selection")
			}
			entry.Cover = &cover
			plan.CoverBytes += cover.Size
		}
		if media, ok := record.Media["manual"]; ok && manualWritable(before) {
			manual, e := fileState(source.DataRoot, filepath.Join(source.DataRoot, filepath.FromSlash(media.SourcePath)), maxManual)
			if e != nil {
				return nil, e
			}
			if manual.Size != media.Size {
				return nil, fmt.Errorf("manual size changed after selection")
			}
			if _, e = manualExtension(manual.Path); e != nil {
				return nil, e
			}
			entry.Manual = &manual
			plan.ManualBytes += manual.Size
		}
		plan.Entries = append(plan.Entries, entry)
	}
	if len(plan.Entries) == 0 {
		return nil, fmt.Errorf("no eligible ROMs for the RomM pilot")
	}
	return plan, nil
}

// WriteNew preserves prior manifests/receipts and uses private file permissions.
func WriteNew(name string, value any) error {
	f, e := catalogfs.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if e = encoder.Encode(value); e != nil {
		return e
	}
	return f.Sync()
}
func ReadPlan(name string) (*Plan, error) {
	f, e := catalogfs.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 64<<20))
	decoder.DisallowUnknownFields()
	var p Plan
	if e = decoder.Decode(&p); e != nil {
		return nil, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("plan must contain one JSON document")
	}
	if p.SchemaVersion != 1 || p.Target != "romm" || len(p.Entries) == 0 && p.Operation != repairDateUnits || len(p.Entries) > 1000 {
		return nil, fmt.Errorf("unsupported or empty RomM plan")
	}
	return &p, nil
}
func verifyFile(root string, want FileState, limit int64) error {
	got, e := fileState(root, want.Path, limit)
	if e != nil {
		return e
	}
	if got != want {
		return fmt.Errorf("planned source changed")
	}
	return nil
}
func Preflight(ctx context.Context, c *Client, p *Plan) error {
	if p.Server != c.base || p.SchemaVersion != 1 || p.Target != "romm" || p.Platform.ID < 1 || p.Operation != "" && p.Operation != repairDateUnits && p.Operation != providerImport {
		return fmt.Errorf("invalid plan or server mismatch")
	}
	if _, e := cleanRelative(p.Prefix); e != nil {
		return e
	}
	if p.Operation == providerImport {
		if e := c.offlineProviders(ctx); e != nil {
			return e
		}
	}
	seen := map[int]bool{}
	for _, entry := range p.Entries {
		if seen[entry.ROM.ID] || entry.ROM.ID < 1 || entry.ROM.PlatformID != p.Platform.ID || entry.ROM.PlatformFSSlug != p.Platform.FSSlug {
			return fmt.Errorf("invalid or duplicate planned inventory identity")
		}
		seen[entry.ROM.ID] = true
		rel, e := cleanRelative(filepath.ToSlash(entry.Source.ROM))
		if e != nil {
			return e
		}
		if path.Join(entry.ROM.FSPath, entry.ROM.FSName) != path.Join(p.Prefix, rel) || entry.Input.Size != entry.ROM.FSSize {
			return fmt.Errorf("planned inventory mapping differs")
		}
		expected, expectedError := expectedForm(p, entry)
		if expectedError != nil || entry.Source.Status != "matched" || entry.Source.MatchedBy != "hash" || !reflect.DeepEqual(entry.Form, expected) || snapshotHash(entry.Before) != entry.BeforeHash {
			return fmt.Errorf("plan metadata or baseline is inconsistent")
		}
		current, e := c.Detail(ctx, entry.ROM.ID)
		if e != nil {
			return e
		}
		rom, e := decodeROM(current)
		if e != nil || rom != entry.ROM || snapshotHash(current) != entry.BeforeHash {
			return fmt.Errorf("RomM ROM %d changed since planning; create a fresh plan", entry.ROM.ID)
		}
		expectedInput, pathErr := catalogfs.EvalSymlinks(filepath.Join(p.ROMRoot, filepath.FromSlash(rel)))
		if pathErr != nil || expectedInput != entry.Input.Path {
			return fmt.Errorf("planned input path differs from its ROM identity")
		}
		if e = verifyFile(p.ROMRoot, entry.Input, 512<<20); e != nil {
			return fmt.Errorf("ROM %d: %w", entry.ROM.ID, e)
		}
		if entry.Manual != nil {
			media, hasManual := entry.Source.Media["manual"]
			expectedManual, pathError := catalogfs.EvalSymlinks(filepath.Join(p.DataRoot, filepath.FromSlash(media.SourcePath)))
			if !hasManual || pathError != nil || expectedManual != entry.Manual.Path || media.Size != entry.Manual.Size || !manualWritable(entry.Before) {
				return fmt.Errorf("manual would replace owner data or differ from source selection")
			}
			if _, e := manualExtension(entry.Manual.Path); e != nil {
				return e
			}
			if e := verifyFile(p.DataRoot, *entry.Manual, maxManual); e != nil {
				return e
			}
		}
		if entry.Cover != nil {
			media, hasCover := entry.Source.Media["cover"]
			expectedCover, pathErr := catalogfs.EvalSymlinks(filepath.Join(p.DataRoot, filepath.FromSlash(media.SourcePath)))
			if !hasCover || pathErr != nil || expectedCover != entry.Cover.Path || media.Size != entry.Cover.Size {
				return fmt.Errorf("planned cover differs from selected media")
			}
			if !coverWritable(entry.Before) {
				return fmt.Errorf("cover would replace an existing or locked cover")
			}
			if e = verifyFile(p.DataRoot, *entry.Cover, maxCover); e != nil {
				return fmt.Errorf("cover %d: %w", entry.ROM.ID, e)
			}
		}
	}
	return nil
}
func (c *Client) update(ctx context.Context, entry Entry) (map[string]json.RawMessage, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(entry.Form))
	for key := range entry.Form {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowedEditField(key, entry.Providers != nil) {
			return nil, fmt.Errorf("unsupported edit field")
		}
		if e := w.WriteField(key, entry.Form[key]); e != nil {
			return nil, e
		}
	}
	if entry.Cover != nil {
		file, e := catalogfs.Open(entry.Cover.Path)
		if e != nil {
			return nil, e
		}
		defer file.Close()
		part, e := w.CreateFormFile("artwork", filepath.Base(entry.Cover.Path))
		if e != nil {
			return nil, e
		}
		digest := sha256.New()
		n, copyError := io.Copy(io.MultiWriter(part, digest), io.LimitReader(file, maxCover+1))
		if copyError != nil || n != entry.Cover.Size || hex.EncodeToString(digest.Sum(nil)) != entry.Cover.SHA256 {
			return nil, fmt.Errorf("cover source changed during upload")
		}
	}
	if e := w.Close(); e != nil {
		return nil, e
	}
	var after map[string]json.RawMessage
	e := c.request(ctx, http.MethodPut, fmt.Sprintf("/roms/%d", entry.ROM.ID), w.FormDataContentType(), &buf, &after)
	return after, e
}

type Result struct {
	ManualStatus string                     `json:"manual_status,omitempty"`
	ID           int                        `json:"id"`
	Status       string                     `json:"status"`
	After        map[string]json.RawMessage `json:"after,omitempty"`
}
type Receipt struct {
	SchemaVersion int        `json:"schema_version"`
	Target        string     `json:"target"`
	Started       time.Time  `json:"started"`
	Finished      *time.Time `json:"finished,omitempty"`
	Results       []Result   `json:"results"`
	Error         string     `json:"error,omitempty"`
}

// Apply preflights the entire selection, then checks each baseline again just before its edit.
// The server has no conditional-update API; concurrent edits must remain paused during apply.
func Apply(ctx context.Context, c *Client, p *Plan, receiptPath string, progress func(int, int, int), allowUnexposedLocks bool) error {
	if HasUnexposedLocks(p) && !allowUnexposedLocks {
		return fmt.Errorf("RomM API does not expose field locks; apply requires --allow-unexposed-locks after independent review")
	}
	if e := Preflight(ctx, c, p); e != nil {
		return e
	}
	f, e := catalogfs.OpenFile(receiptPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	receipt := Receipt{SchemaVersion: 1, Target: "romm", Started: time.Now().UTC(), Results: []Result{}}
	save := func() error {
		if _, e := f.Seek(0, 0); e != nil {
			return e
		}
		if e := f.Truncate(0); e != nil {
			return e
		}
		encoder := json.NewEncoder(f)
		encoder.SetIndent("", "  ")
		if e := encoder.Encode(receipt); e != nil {
			return e
		}
		return f.Sync()
	}
	if e = save(); e != nil {
		return e
	}
	for i, entry := range p.Entries {
		if progress != nil {
			progress(i+1, len(p.Entries), entry.ROM.ID)
		}
		if len(entry.Form) == 0 && entry.Cover == nil && entry.Manual == nil {
			receipt.Results = append(receipt.Results, Result{ID: entry.ROM.ID, Status: "unchanged"})
			if e = save(); e != nil {
				return e
			}
			continue
		}
		if p.Operation == providerImport {
			if _, sourceError := expectedProviderForm(p, entry); sourceError != nil {
				receipt.Error = "provider source changed before its edit"
				_ = save()
				return fmt.Errorf("ROM %d: %s", entry.ROM.ID, receipt.Error)
			}
			if sourceError := verifyFile(p.ROMRoot, entry.Input, 512<<20); sourceError != nil {
				receipt.Error = "ROM source changed before its edit"
				_ = save()
				return fmt.Errorf("ROM %d: %s", entry.ROM.ID, receipt.Error)
			}
		}
		current, readErr := c.Detail(ctx, entry.ROM.ID)
		if readErr != nil {
			e = readErr
		} else if snapshotHash(current) != entry.BeforeHash {
			e = fmt.Errorf("RomM ROM %d changed before its edit", entry.ROM.ID)
		} else {
			var after map[string]json.RawMessage
			manualStatus := ""
			if len(entry.Form) > 0 || entry.Cover != nil {
				after, e = c.update(ctx, entry)
			} else {
				after = current
			}
			if e == nil {
				rom, decodeErr := decodeROM(after)
				if decodeErr != nil || rom != entry.ROM {
					e = fmt.Errorf("updated ROM response changed inventory identity")
				}
			}
			if e == nil {
				for key, value := range entry.Form {
					if (key == "name" || key == "summary") && rawString(after, key) != value {
						e = fmt.Errorf("RomM did not retain submitted %s", key)
					}
				}
				if manual, submitted := entry.Form["raw_manual_metadata"]; submitted && !sameJSON([]byte(manual), after["manual_metadata"]) {
					e = fmt.Errorf("RomM did not retain submitted manual metadata")
				}
				if e == nil {
					e = verifyProviderResponse(entry, after)
				}
				if entry.Cover != nil && (rawString(after, "path_cover_large") == "" || rawString(after, "path_cover_small") == "") {
					e = fmt.Errorf("RomM did not retain the submitted cover")
				}
			}
			if e == nil && entry.Manual != nil {
				beforeManual := after
				latest, readError := c.Detail(ctx, entry.ROM.ID)
				if readError != nil {
					e = readError
				} else if snapshotHash(latest) != snapshotHash(beforeManual) || !manualWritable(latest) {
					e = fmt.Errorf("manual baseline changed before upload")
				}
				if e == nil {
					manualStatus = "not-confirmed"
					e = c.uploadManual(ctx, entry)
					if e == nil {
						manualStatus = "posted"
						confirmed, readError := c.Detail(ctx, entry.ROM.ID)
						if readError != nil {
							e = readError
						} else {
							after = confirmed
							e = verifyManualResponse(entry, beforeManual, after)
							if e == nil {
								manualStatus = "confirmed"
							}
						}
					}
				}
			}
			status := "applied"
			if e != nil {
				status = "response-not-confirmed"
			}
			receipt.Results = append(receipt.Results, Result{ID: entry.ROM.ID, Status: status, After: after, ManualStatus: manualStatus})
		}
		if e != nil {
			receipt.Error = e.Error()
			_ = save()
			return e
		}
		if e = save(); e != nil {
			return e
		}
	}
	now := time.Now().UTC()
	receipt.Finished = &now
	return save()
}

func fieldLocked(before map[string]json.RawMessage, key string) bool {
	var locked []string
	_ = json.Unmarshal(before["locked_fields"], &locked)
	for _, value := range locked {
		if value == key {
			return true
		}
	}
	return false
}
func knownManualFields(metadata map[string]json.RawMessage) bool {
	allowed := map[string]bool{"genres": true, "franchises": true, "companies": true, "publishers": true, "developers": true, "game_modes": true, "age_ratings": true, "first_release_date": true, "youtube_video_id": true, "alternative_names": true}
	for key := range metadata {
		if !allowed[key] {
			return false
		}
	}
	return true
}
func sourceRecordState(root string, record exporter.Record) (FileState, error) {
	state, e := fileState(root, filepath.Join(root, filepath.FromSlash(record.ROM)), 512<<20)
	if e != nil {
		return FileState{}, e
	}
	var reader io.ReadCloser
	if record.Member != "" {
		archive, e := zip.OpenReader(state.Path)
		if e != nil {
			return FileState{}, e
		}
		defer archive.Close()
		var selected *zip.File
		for _, member := range archive.File {
			if member.Name == record.Member {
				if selected != nil {
					return FileState{}, fmt.Errorf("duplicate selected archive member")
				}
				selected = member
			}
		}
		if selected == nil || selected.UncompressedSize64 > 512<<20 {
			return FileState{}, fmt.Errorf("selected archive member missing or too large")
		}
		reader, e = selected.Open()
		if e != nil {
			return FileState{}, e
		}
	} else {
		reader, e = catalogfs.Open(state.Path)
		if e != nil {
			return FileState{}, e
		}
	}
	defer reader.Close()
	h := sha1.New()
	n, e := io.Copy(h, io.LimitReader(reader, (512<<20)+1))
	if e != nil || n != record.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), record.SHA1) {
		return FileState{}, fmt.Errorf("ROM payload changed after catalog recognition")
	}
	return state, nil
}

// HasUnexposedLocks reports an API capability gap, not an empty lock list.
func HasUnexposedLocks(p *Plan) bool {
	for _, entry := range p.Entries {
		if _, ok := entry.Before["locked_fields"]; !ok {
			return true
		}
	}
	return false
}
