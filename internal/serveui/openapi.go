package serveui

import (
	"strings"
	"sync"

	"rommeta/internal/appinfo"
	"rommeta/internal/catalogstate"
	"rommeta/internal/curation"
	"rommeta/internal/httpdocs"
	"rommeta/internal/librarysettings"
)

var browserSpecOnce sync.Once
var browserSpec any

func browserOpenAPI() any {
	browserSpecOnce.Do(func() {
		s := httpdocs.NewSchemas()
		str := httpdocs.Schema{"type": "string"}
		platform := httpdocs.Query("platform", "Exact platform folder; CLI aliases are fallback.", true, str)
		readFailure := httpdocs.Response("Read or parse failure.", s.Ref(errorResponse{}))
		op := func(summary, description string, parameters []httpdocs.Schema, response httpdocs.Schema) httpdocs.Schema {
			if parameters == nil {
				parameters = []httpdocs.Schema{}
			}
			return httpdocs.Schema{"summary": summary, "description": description, "parameters": parameters, "responses": httpdocs.Schema{"200": httpdocs.Response("Success", response), "400": httpdocs.Response("Invalid parameters", s.Ref(errorResponse{})), "404": httpdocs.Response("Resource not found", s.Ref(errorResponse{})), "500": readFailure}}
		}
		paths := httpdocs.Schema{
			"/api":           httpdocs.Schema{"get": op("Endpoint index", "Discover browser endpoints; the shared server adds consumer API v1 routes.", nil, httpdocs.Schema{"type": "object", "additionalProperties": true})},
			"/api/platforms": httpdocs.Schema{"get": op("List platform folders and game counts", "Directory enumeration only; does not read every game document.", nil, s.Ref([]platformItem{}))},
			"/api/games":     httpdocs.Schema{"get": op("List names or game summaries", "Use names=1 for fast folder discovery. Repeat game up to 32 times for bounded summaries. Without either, reads every game in the platform. Per-card errors use summary_error.", []httpdocs.Schema{platform, httpdocs.Query("names", "Use 1 for names only.", false, httpdocs.Schema{"type": "string", "enum": []string{"1"}}), httpdocs.Schema{"name": "game", "in": "query", "description": "Folders to summarize (repeat the query parameter).", "style": "form", "explode": true, "schema": httpdocs.Schema{"type": "array", "maxItems": 32, "items": str}}}, s.Ref(gamesResponse{}))},
			"/api/game":      httpdocs.Schema{"get": op("Read a game card", "Stored game fields over the source overview, full catalog YAML, original evidence and local media URLs. Reads only the selected card; no export policy is applied.", []httpdocs.Schema{platform, httpdocs.Query("game", "Exact game folder.", true, str)}, s.Ref(gameResponse{}))},
			"/api/stats":     httpdocs.Schema{"get": op("Scan catalog statistics", "Expensive metadata/media inventory scan; no writable acquisition cache or media payload reads. Omit platform to scan the whole catalog. Cancellation stops between bounded game batches after in-flight filesystem calls. stream=1 emits progress frames followed by one result or error frame; after streaming begins, errors retain HTTP 200.", []httpdocs.Schema{httpdocs.Query("platform", "Optional exact platform folder.", false, str), httpdocs.Query("stream", "1 enables newline-delimited progress/result/error frames.", false, httpdocs.Schema{"type": "string", "enum": []string{"1"}})}, s.Ref(catalogstate.Status{}))},
			"/media":         httpdocs.Schema{"get": httpdocs.Schema{"summary": "Read local media", "description": "Use a path returned by /api/game. Paths outside catalog media are rejected. Opening a packed video may materialize it; videos are opened only on explicit play.", "parameters": []httpdocs.Schema{httpdocs.Query("path", "Catalog-relative media path.", true, str)}, "responses": httpdocs.Schema{"200": httpdocs.Schema{"description": "Media bytes", "content": httpdocs.Schema{"application/octet-stream": httpdocs.Schema{"schema": httpdocs.Schema{"type": "string", "format": "binary"}}}}, "400": httpdocs.Response("Invalid media path", s.Ref(errorResponse{})), "404": httpdocs.Response("Media not found", s.Ref(errorResponse{}))}}},
		}
		paths["/api/fingerprints"] = httpdocs.Schema{"get": op("Read registered file fingerprints", "Read-only paged records for one named card. No writable cache, ROM reads or verification. Optional release/variant filters are exclusive; q searches file names and hashes. limit defaults to 100, maximum 200; offset starts at zero. Missing fingerprint documents return an empty page.", []httpdocs.Schema{httpdocs.Query("platform", "Catalog platform.", true, str), httpdocs.Query("game", "Catalog game folder.", true, str), httpdocs.Query("release", "Exact official release.", false, str), httpdocs.Query("variant", "Exact derived variant key.", false, str), httpdocs.Query("q", "Filename or hash search, up to 256 characters.", false, str), httpdocs.Query("offset", "Zero-based result offset.", false, str), httpdocs.Query("limit", "Page size, 1..200.", false, str)}, s.Ref(fingerprintsResponse{}))}
		preview := op("Preview catalog export selection", "Read-only computation for at most 32 explicitly named games. Returns a neutral profile YAML and selected fields/media descriptors. include_raw adds native XML templates and media paths with placeholder ROM inputs for named cards. media_roles explicitly selects frontend-supported types. artwork_preference is auto, original or reconstructed and ranks artwork type inside regional priority. Per-card media_reasons explain shared selector results without opening payloads. launchbox_regions ranks new LaunchBox primary versions and the preferred catalog release when no Edition is explicit. Existing native main launches remain unchanged; CLI selection depends on actual ROMs. With no games, returns only the profile. Does not match ROMs, open media payloads, save files or apply native export.", nil, s.Ref(exportPreviewResponse{}))
		preview["requestBody"] = httpdocs.Schema{"required": true, "content": httpdocs.Schema{"application/json": httpdocs.Schema{"schema": s.Ref(exportPreviewRequest{})}}}
		paths["/api/export-preview"] = httpdocs.Schema{"post": preview}
		paths["/api/export-options"] = httpdocs.Schema{"get": op("Read export source defaults", "Catalog-configured metadata/artwork/video source order, registered source namespaces, canonical region choices and frontend-supported media roles for the visual chooser. No game/media scan. Omitted settings use defaults; explicit empty arrays disable automatic providers while saved catalog overrides still apply.", nil, s.Ref(exportSourceOptions{}))}
		for path, item := range paths {
			method := "get"
			if path == "/api/export-preview" {
				method = "post"
			}
			get := item.(httpdocs.Schema)[method].(httpdocs.Schema)
			get["operationId"] = "browser" + strings.NewReplacer("/", "_", "-", "_").Replace(path)
		}

		paths["/api/catalog"] = httpdocs.Schema{"get": op("Catalog editing capability", "Reports whether serve --edit enables catalog actions; no writes.", nil, httpdocs.Schema{"type": "object", "additionalProperties": true})}
		editGet := op("Read editable card and source records", "Returns library fields, exact source record targets, named-source registry, current SHA256 revision, preserved YAML and browser profile.", []httpdocs.Schema{platform, httpdocs.Query("game", "Exact game folder.", true, str)}, s.Ref(editorCard{}))
		addWrite := func(path, summary, description string, request any) {
			action := op(summary, description, nil, httpdocs.Schema{"type": "object", "additionalProperties": true})
			action["operationId"] = "browser" + strings.NewReplacer("/", "_", "-", "_").Replace(path) + "_post"
			action["requestBody"] = httpdocs.Schema{"required": true, "content": httpdocs.Schema{"application/json": httpdocs.Schema{"schema": s.Ref(request)}}}
			responses := action["responses"].(httpdocs.Schema)
			responses["403"] = httpdocs.Response("Editing disabled or cross-origin action.", s.Ref(errorResponse{}))
			responses["409"] = httpdocs.Response("Stale input, expired preview, conflicting data or busy writer.", s.Ref(errorResponse{}))
			paths[path] = httpdocs.Schema{"post": action}
		}
		addWrite("/api/catalog/curation", "Create or rename a named source", "Explicit name and current registry revision required. Stable source IDs keep saved profile priorities valid. Does not copy metadata or enable a source.", curationRequest{})
		paths["/api/catalog/curation"].(httpdocs.Schema)["get"] = op("Read named sources", "Editable mode required; library-wide names and stable source IDs.", nil, s.Ref(curation.Registry{}))
		addWrite("/api/catalog/settings", "Save library settings", "Editable mode and current section revision required. Updates only submitted supported fields under the shared catalog writer lock; preserves other YAML and credentials. Saving does not start imports/downloads.", librarysettings.Request{})
		paths["/api/catalog/settings"].(httpdocs.Schema)["get"] = op("Read library settings", "Editable mode required. Source priorities, acquisition, connection options, fold regions, platform mappings and media policies. Credential values are never returned.", nil, s.Ref([]librarysettings.Section{}))
		paths["/api/operations"] = httpdocs.Schema{"get": op("List server operations", "Session-local DAT plans, running imports and results. Closing the browser never cancels accepted work; reconnect using this list. Records expire after 24 hours or restart.", nil, s.Ref([]importOperation{}))}
		addWrite("/api/catalog/import-plan", "Prepare uploaded DAT", "Select an uploaded ZIP member and optional platform. Returns the same server-owned operation; no card writes. Repeated running requests do not start another worker.", importAction{})
		addWrite("/api/catalog/import-apply", "Import reviewed DAT", "Requires the current ready revision and unchanged library/settings. Starts one server-owned import under the shared CLI writer lock. Repeated accepted commands return the same operation.", importAction{})
		addWrite("/api/catalog/import-dismiss", "Dismiss completed or pending import", "Removes the receipt and any uploaded input. Running operations cannot be dismissed.", importAction{})
		upload := op("Upload DAT", "Upload one ordinary XML DAT or ZIP (256 MiB, DAT members at most 512 MiB expanded). Requires editable mode and same-origin access. A server operation prepares the plan; multi-DAT ZIPs require member selection. MAME uses the classified CLI workflow.", nil, s.Ref(importOperation{}))
		upload["requestBody"] = httpdocs.Schema{"required": true, "content": httpdocs.Schema{"multipart/form-data": httpdocs.Schema{"schema": httpdocs.Schema{"type": "object", "required": []string{"file"}, "properties": httpdocs.Schema{"file": httpdocs.Schema{"type": "string", "format": "binary"}}}}}}
		paths["/api/catalog/import-upload"] = httpdocs.Schema{"post": upload}
		for _, path := range []string{"/api/catalog/import-upload", "/api/catalog/import-plan", "/api/catalog/import-apply"} {
			responses := paths[path].(httpdocs.Schema)["post"].(httpdocs.Schema)["responses"].(httpdocs.Schema)
			delete(responses, "200")
			responses["202"] = httpdocs.Response("Accepted operation snapshot; poll GET /api/operations.", s.Ref(importOperation{}))
		}
		addWrite("/api/catalog/edit", "Save card and source fields", "Requires serve --edit, JSON and same-origin requests. Revision covers the entire stored card. Send edits with exact targets and changed fields for one atomic save; legacy fields/target is a single edit. Null removes a field. New manual targets require a registered source ID and optional existing release scope. Unsubmitted values are preserved. saved=true remains success if index refresh reports a warning.", editRequest{})
		paths["/api/catalog/edit"].(httpdocs.Schema)["get"] = editGet
		addWrite("/api/catalog/edit-preview", "Preview local card corrections", "Read-only preview against the current revision. Does not save or acquire provider data.", editRequest{})
		addWrite("/api/catalog/fold-plan", "Build a platform fold plan", "Uses the same fold rules and catalog region priorities as CLI. Optional games selects 2..12 exact same-platform cards for explicit identity review. Reports ready/review/blocked groups; no mutations. stream=1 returns NDJSON progress and one result/error. Plans expire after 30 minutes, maximum four retained plans; MAME may use temporary prepared files.", foldPlanRequest{})
		addWrite("/api/catalog/fold-preview", "Inspect or correct a group", "Omit draft to inspect the current prepared result without resetting edits. A draft selects candidate folders, primary/local fields and optional owner confirmation. Explicit confirmation can resolve identity/content doubts; data/file conflicts remain blocking. Omitted folders preserve current membership. Returns a new group revision after changes.", foldPreviewRequest{})
		addWrite("/api/catalog/fold-apply", "Apply selected reviewed groups", "Decisions contain exact group IDs, current revisions and merge/separate actions. All selected inputs are rechecked before writes. Uses shared writer lock, existing platform staging and MAME binding-aware apply. Every apply attempt consumes the plan; errors may report already completed ordinary groups before a MAME failure. With continue=true, a successful apply returns next_plan with a fresh ID and revalidated unchanged drafts, or continuation_error without undoing saved work. Recovery locations are returned. Existing backups are retained; no new snapshots.", foldApplyRequest{})
		addWrite("/api/catalog/fold-discard", "Discard a plan", "Releases the in-memory plan and task-owned MAME staging without changing catalog data.", struct {
			Plan string `json:"plan"`
		}{})

		stats := paths["/api/stats"].(httpdocs.Schema)["get"].(httpdocs.Schema)["responses"].(httpdocs.Schema)["200"].(httpdocs.Schema)["content"].(httpdocs.Schema)
		stats["application/x-ndjson"] = httpdocs.Schema{"schema": httpdocs.Schema{"type": "string"}, "example": "{\"progress\":{\"Stage\":\"checking media\",\"Current\":1}}\n{\"result\":{\"GameCount\":1}}\n"}
		browserSpec = httpdocs.Document("RomMeta browser API", appinfo.Version, "Local catalog browser, read-only by default; serve --edit enables reviewed catalog changes. No authentication; keep on loopback or a trusted network. The serve runtime composes these browser routes with the portable /v1 consumer API and one OpenAPI document.", paths, s, false)
	})
	return browserSpec
}

// OpenAPI describes the browser routes for composition into the shared server.
func OpenAPI() any { return browserOpenAPI() }
