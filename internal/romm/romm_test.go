package romm

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rommeta/internal/exporter"
)

type fakeAPI struct {
	server          *httptest.Server
	rows            map[int]map[string]json.RawMessage
	platform        Platform
	put             int
	manualPost      int
	manualData      []byte
	requests        int
	badField        bool
	inventory       []ROM
	redirect        string
	enabledProvider string
}

func raw(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
func (f *fakeAPI) close()           { f.server.Close() }
func newFake(t *testing.T, rows ...map[string]json.RawMessage) *fakeAPI {
	t.Helper()
	f := &fakeAPI{rows: map[int]map[string]json.RawMessage{}, platform: Platform{7, "Sega - Game Gear"}}
	for _, row := range rows {
		r, e := decodeROM(row)
		if e != nil {
			t.Fatal(e)
		}
		f.rows[r.ID] = row
		f.inventory = append(f.inventory, r)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests++
		if r.Header.Get("Authorization") != "Bearer secret-never-print" {
			http.Error(w, "bad auth", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if f.redirect != "" {
			http.Redirect(w, r, f.redirect, 302)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/heartbeat":
			sources := map[string]bool{"IGDB_API_ENABLED": false, "SS_API_ENABLED": false, "LAUNCHBOX_API_ENABLED": false, "HASHEOUS_API_ENABLED": false, "ANY_SOURCE_ENABLED": true}
			if f.enabledProvider != "" {
				sources[f.enabledProvider] = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"SYSTEM": map[string]string{"VERSION": "5.4.0"}, "METADATA_SOURCES": sources})
		case r.Method == "GET" && r.URL.Path == "/api/platforms":
			_ = json.NewEncoder(w).Encode([]Platform{f.platform})
		case r.Method == "GET" && r.URL.Path == "/api/roms":
			if r.URL.Query().Get("platform_ids") != "7" {
				t.Error("missing platform scope")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": f.inventory, "total": len(f.inventory)})
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/roms/") && strings.HasSuffix(r.URL.Path, "/manuals"):
			id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/roms/"), "/manuals"))
			row, ok := f.rows[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			f.manualPost++
			name := r.Header.Get("X-Upload-Filename")
			if !strings.HasPrefix(name, "rommeta-manual.") {
				t.Error("manual upload header missing")
			}
			if e := r.ParseMultipartForm(128 << 10); e != nil {
				t.Error(e)
				http.Error(w, "invalid multipart", 400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, e := r.FormFile(name)
			if e != nil {
				t.Error("manual dynamic multipart field missing", e)
				http.Error(w, "missing file", 400)
				return
			}
			if header.Filename != name || len(r.MultipartForm.File) != 1 {
				t.Error("invalid manual filename/form")
			}
			f.manualData, _ = io.ReadAll(file)
			file.Close()
			row["path_manual"] = raw(fmt.Sprintf("roms/7/%d/manual/%d%s", id, id, filepath.Ext(name)))
			row["has_manual"] = raw(true)
			var locked []string
			_ = json.Unmarshal(row["locked_fields"], &locked)
			locked = append(locked, "url_manual")
			row["locked_fields"] = raw(locked)
			row["updated_at"] = raw("manual-uploaded")
			w.WriteHeader(201)
		case strings.HasPrefix(r.URL.Path, "/api/roms/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/roms/"))
			row, ok := f.rows[id]
			if !ok {
				http.Error(w, "not found", 404)
				return
			}
			if r.Method == "PUT" {
				f.put++
				if e := r.ParseMultipartForm(128 << 10); e != nil {
					t.Error(e)
					http.Error(w, "bad multipart", 400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				for key, values := range r.MultipartForm.Value {
					switch key {
					case "name", "summary":
						row[key] = raw(values[0])
					case "raw_manual_metadata", "raw_igdb_metadata", "raw_ss_metadata", "raw_launchbox_metadata", "raw_hasheous_metadata":
						var metadata map[string]json.RawMessage
						if e := json.Unmarshal([]byte(values[0]), &metadata); e != nil {
							t.Error(e)
						}
						row[strings.TrimPrefix(key, "raw_")] = raw(metadata)
					case "igdb_id", "ss_id", "launchbox_id", "hasheous_id", "tgdb_id":
						n, _ := strconv.Atoi(values[0])
						row[key] = raw(n)
					default:
						f.badField = true
						t.Errorf("unexpected API field %s", key)
					}
				}
				if file, header, e := r.FormFile("artwork"); e == nil {
					data, _ := io.ReadAll(file)
					file.Close()
					if len(data) == 0 || header.Filename == "" {
						t.Error("empty artwork")
					}
					row["path_cover_large"] = raw("roms/7/1/cover/big.jpg")
					row["path_cover_small"] = raw("roms/7/1/cover/small.jpg")
					row["locked_fields"] = raw([]string{"url_cover"})
				}
				row["updated_at"] = raw("updated")
			}
			_ = json.NewEncoder(w).Encode(row)
		default:
			http.NotFound(w, r)
		}
	}))
	return f
}
func beforeRow(id int, relative string, size int64) map[string]json.RawMessage {
	return map[string]json.RawMessage{"id": raw(id), "platform_id": raw(7), "platform_fs_slug": raw("Sega - Game Gear"), "fs_path": raw("roms/Sega - Game Gear/retail"), "fs_name": raw(relative), "fs_size_bytes": raw(size), "fs_name_no_ext": raw(strings.TrimSuffix(relative, filepath.Ext(relative))), "fs_name_no_tags": raw("Example"), "name": raw("Example"), "summary": raw(""), "manual_metadata": raw(map[string]any{}), "path_cover_small": raw(""), "path_cover_large": raw(""), "url_cover": raw(""), "locked_fields": raw([]string{}), "updated_at": raw("baseline"), "igdb_id": raw(nil)}
}
func sourceFixture(t *testing.T, count int) *exporter.Plan {
	t.Helper()
	root := t.TempDir()
	catalog := filepath.Join(root, "catalog")
	romRoot := filepath.Join(root, "roms")
	if e := os.MkdirAll(catalog, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(romRoot, 0700); e != nil {
		t.Fatal(e)
	}
	media := []byte("synthetic image bytes")
	if e := os.WriteFile(filepath.Join(catalog, "box.jpg"), media, 0600); e != nil {
		t.Fatal(e)
	}
	plan := &exporter.Plan{DataRoot: catalog, ROMRoot: romRoot, Profile: exporter.Profile{Selection: exporter.Selection{Media: map[string]exporter.Rule{"cover": {}}}}}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("Example %d.gg", i+1)
		payload := []byte(fmt.Sprintf("synthetic ROM %d", i))
		if e := os.WriteFile(filepath.Join(romRoot, name), payload, 0600); e != nil {
			t.Fatal(e)
		}
		h := sha1.Sum(payload)
		plan.Records = append(plan.Records, exporter.Record{ROM: name, Status: "matched", MatchedBy: "hash", SHA1: hex.EncodeToString(h[:]), Size: int64(len(payload)), Game: "platforms/Example/Game", Release: "Example (USA)", Fields: map[string]exporter.Value{"title": {Value: "Catalog title", Source: "catalog"}, "description": {Value: "Offline prose", Source: "launchbox", Language: "en"}, "genres": {Value: "Action / Puzzle", Source: "launchbox"}, "developer": {Value: "Original studio", Source: "local"}, "release_date": {Value: "1993", Source: "launchbox"}}, Media: map[string]exporter.Media{"cover": {SourcePath: "box.jpg", Size: int64(len(media)), Source: "launchbox"}}})
	}
	return plan
}
func buildFixture(t *testing.T, count int) (*fakeAPI, *Client, *Plan) {
	t.Helper()
	source := sourceFixture(t, count)
	var rows []map[string]json.RawMessage
	for i, r := range source.Records {
		info, e := os.Stat(filepath.Join(source.ROMRoot, r.ROM))
		if e != nil {
			t.Fatal(e)
		}
		rows = append(rows, beforeRow(i+1, r.ROM, info.Size()))
	}
	api := newFake(t, rows...)
	t.Cleanup(api.close)
	client, e := NewClient(api.server.URL, "secret-never-print")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail")
	if e != nil {
		t.Fatal(e)
	}
	return api, client, plan
}
func TestPlanExactPathAndNoServerWrites(t *testing.T) {
	api, client, plan := buildFixture(t, 2)
	if api.put != 0 || len(plan.Entries) != 2 || plan.CoverBytes != 42 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if e := Preflight(context.Background(), client, plan); e != nil {
		t.Fatal(e)
	}
	if api.put != 0 {
		t.Fatal("preflight wrote server")
	}
	if _, ok := plan.Entries[0].Form["raw_manual_metadata"]; !ok {
		t.Fatal("manual metadata omitted")
	}
	if strings.Contains(plan.Entries[0].Form["raw_manual_metadata"], "first_release_date") {
		t.Fatal("partial year invented a complete date")
	}
	b, _ := json.Marshal(plan)
	if bytes.Contains(b, []byte("secret-never-print")) {
		t.Fatal("token leaked to manifest")
	}
	entries, e := os.ReadDir(plan.DataRoot)
	if e != nil || len(entries) != 1 {
		t.Fatal("planning copied media")
	}
}
func TestExistingMetadataAndCoverPreserved(t *testing.T) {
	source := sourceFixture(t, 1)
	row := beforeRow(1, source.Records[0].ROM, source.Records[0].Size)
	row["name"] = raw("Owner title")
	row["summary"] = raw("Owner prose")
	row["manual_metadata"] = raw(map[string]any{"genres": []string{"Owner genre"}, "developers": []string{"Owner studio"}, "alternative_names": []string{"Owner alias"}})
	row["path_cover_large"] = raw("owner-cover.png")
	api := newFake(t, row)
	defer api.close()
	client, _ := NewClient(api.server.URL, "secret-never-print")
	plan, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail")
	if e != nil {
		t.Fatal(e)
	}
	entry := plan.Entries[0]
	if len(entry.Form) != 0 || entry.Cover != nil {
		t.Fatalf("owner data would change: %+v", entry.Form)
	}
}
func TestRejectInventoryMismatch(t *testing.T) {
	for _, kind := range []string{"case", "size", "duplicate", "platform"} {
		t.Run(kind, func(t *testing.T) {
			source := sourceFixture(t, 1)
			row := beforeRow(1, source.Records[0].ROM, source.Records[0].Size)
			switch kind {
			case "case":
				row["fs_name"] = raw(strings.ToLower(source.Records[0].ROM))
			case "size":
				row["fs_size_bytes"] = raw(999)
			case "platform":
				row["platform_id"] = raw(99)
			}
			api := newFake(t, row)
			defer api.close()
			if kind == "duplicate" {
				copy := api.inventory[0]
				copy.ID = 2
				api.inventory = append(api.inventory, copy)
			}
			client, _ := NewClient(api.server.URL, "secret-never-print")
			if _, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail"); e == nil {
				t.Fatal("bad inventory accepted")
			}
			if api.put != 0 {
				t.Fatal("bad planning wrote server")
			}
		})
	}
}
func TestEntireSelectionPreflightBeforeAnyWrite(t *testing.T) {
	api, client, plan := buildFixture(t, 2)
	api.rows[2]["summary"] = raw("Concurrent owner edit")
	e := Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "receipt.json"), nil, false)
	if e == nil || api.put != 0 {
		t.Fatalf("stale second item allowed writes: %v / %d", e, api.put)
	}
}
func TestChangedLocalCoverAborts(t *testing.T) {
	api, client, plan := buildFixture(t, 1)
	if e := os.WriteFile(plan.Entries[0].Cover.Path, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "receipt.json"), nil, false); e == nil || api.put != 0 {
		t.Fatal("changed cover submitted")
	}
}
func TestSuccessfulApplyUsesOnlyAllowedMultipartFields(t *testing.T) {
	api, client, plan := buildFixture(t, 2)
	before, e := os.ReadFile(plan.Entries[0].Input.Path)
	if e != nil {
		t.Fatal(e)
	}
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	if e = Apply(context.Background(), client, plan, receipt, nil, false); e != nil {
		t.Fatal(e)
	}
	if api.put != 2 || api.badField {
		t.Fatal("wrong edits")
	}
	after, _ := os.ReadFile(plan.Entries[0].Input.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("ROM changed")
	}
	var metadata map[string]any
	_ = json.Unmarshal(api.rows[1]["manual_metadata"], &metadata)
	if !reflect.DeepEqual(metadata["genres"], []any{"Action", "Puzzle"}) {
		t.Fatalf("wrong genres: %v", metadata)
	}
	data, _ := os.ReadFile(receipt)
	if !bytes.Contains(data, []byte("applied")) || bytes.Contains(data, []byte("secret-never-print")) {
		t.Fatal("bad receipt")
	}
	if e = Preflight(context.Background(), client, plan); e == nil {
		t.Fatal("old applied plan reusable")
	}
}
func TestPlanFilesCannotOverwrite(t *testing.T) {
	_, _, plan := buildFixture(t, 1)
	name := filepath.Join(t.TempDir(), "plan.json")
	if e := WriteNew(name, plan); e != nil {
		t.Fatal(e)
	}
	if e := WriteNew(name, plan); e == nil {
		t.Fatal("overwrote plan")
	}
	copy, e := ReadPlan(name)
	if e != nil || !reflect.DeepEqual(plan, copy) {
		t.Fatalf("plan round trip: %v", e)
	}
}
func TestCredentialsNotRedirectedOrPrinted(t *testing.T) {
	row := beforeRow(1, "Example.gg", 3)
	api := newFake(t, row)
	defer api.close()
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	api.redirect = foreign.URL
	client, _ := NewClient(api.server.URL, "secret-never-print")
	_, e := client.Detail(context.Background(), 1)
	if e == nil || foreignCalls != 0 || strings.Contains(e.Error(), "secret-never-print") {
		t.Fatal("unsafe redirect or error")
	}
	for _, origin := range []string{"http://example.invalid", "https://user:pass@example.invalid", "https://example.invalid/?token=x"} {
		if _, e := NewClient(origin, "secret-never-print"); e == nil {
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
}
func TestRejectExtraMediaAndWeakIdentity(t *testing.T) {
	source := sourceFixture(t, 1)
	api := newFake(t, beforeRow(1, source.Records[0].ROM, source.Records[0].Size))
	defer api.close()
	client, _ := NewClient(api.server.URL, "secret-never-print")
	source.Profile.Selection.Media["video"] = exporter.Rule{}
	if _, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail"); e == nil {
		t.Fatal("video enabled")
	}
	delete(source.Profile.Selection.Media, "video")
	source.Records[0].MatchedBy = "name"
	if _, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail"); e == nil {
		t.Fatal("weak name accepted")
	}
}

func TestPayloadChangedAfterRecognitionRejected(t *testing.T) {
	source := sourceFixture(t, 1)
	record := source.Records[0]
	if e := os.WriteFile(filepath.Join(source.ROMRoot, record.ROM), bytes.Repeat([]byte("X"), int(record.Size)), 0600); e != nil {
		t.Fatal(e)
	}
	api := newFake(t, beforeRow(1, record.ROM, record.Size))
	defer api.close()
	client, _ := NewClient(api.server.URL, "secret-never-print")
	if _, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail"); e == nil {
		t.Fatal("stale recognition accepted")
	}
}
func TestLockedFieldsAndUnknownManualSchemaPreserved(t *testing.T) {
	record := sourceFixture(t, 1).Records[0]
	before := beforeRow(1, record.ROM, record.Size)
	before["locked_fields"] = raw([]string{"name", "summary", "url_cover"})
	before["manual_metadata"] = raw(map[string]any{"future_owner_field": "preserve"})
	form := proposedForm(record, before)
	if len(form) != 0 || coverWritable(before) {
		t.Fatal("locked or unknown owner fields would change")
	}
}
func TestPlanInputCannotBeSubstituted(t *testing.T) {
	api, client, plan := buildFixture(t, 1)
	replacement := filepath.Join(plan.ROMRoot, "another.gg")
	data, _ := os.ReadFile(plan.Entries[0].Input.Path)
	if e := os.WriteFile(replacement, data, 0600); e != nil {
		t.Fatal(e)
	}
	state, e := fileState(plan.ROMRoot, replacement, 512<<20)
	if e != nil {
		t.Fatal(e)
	}
	plan.Entries[0].Input = state
	if e = Preflight(context.Background(), client, plan); e == nil || api.put != 0 {
		t.Fatal("substituted source accepted")
	}
}

func TestZIPMemberFingerprintAndOuterInventorySize(t *testing.T) {
	source := sourceFixture(t, 1)
	record := source.Records[0]
	payload, e := os.ReadFile(filepath.Join(source.ROMRoot, record.ROM))
	if e != nil {
		t.Fatal(e)
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	member, e := archive.Create("Example.gg")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = member.Write(payload)
	if e = archive.Close(); e != nil {
		t.Fatal(e)
	}
	record.ROM = "Example (USA).zip"
	record.Member = "Example.gg"
	source.Records[0] = record
	if e = os.WriteFile(filepath.Join(source.ROMRoot, record.ROM), buffer.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	api := newFake(t, beforeRow(1, record.ROM, int64(buffer.Len())))
	defer api.close()
	client, _ := NewClient(api.server.URL, "secret-never-print")
	plan, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail")
	if e != nil {
		t.Fatal(e)
	}
	if plan.Entries[0].Input.Size == record.Size || plan.Entries[0].Input.Size != int64(buffer.Len()) {
		t.Fatal("payload and outer-file sizes conflated")
	}
	if e = Preflight(context.Background(), client, plan); e != nil {
		t.Fatal(e)
	}
}

func TestUnexposedLocksRequireExplicitAcknowledgement(t *testing.T) {
	api, client, plan := buildFixture(t, 1)
	delete(api.rows[1], "locked_fields")
	delete(plan.Entries[0].Before, "locked_fields")
	plan.Entries[0].BeforeHash = snapshotHash(plan.Entries[0].Before)
	if !HasUnexposedLocks(plan) {
		t.Fatal("missing locks treated as known empty")
	}
	if e := Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "receipt.json"), nil, false); e == nil || api.put != 0 {
		t.Fatal("unknown locks submitted without review")
	}
	if e := Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "reviewed.json"), nil, true); e != nil {
		t.Fatal(e)
	}
}
