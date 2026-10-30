package serveui

import (
	"strings"
	"sync"

	"rommeta/internal/appinfo"
	"rommeta/internal/catalogstate"
	"rommeta/internal/httpdocs"
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
		preview := op("Preview catalog export selection", "Read-only computation for at most 32 explicitly named games. Returns a neutral profile YAML and selected fields/media descriptors. With no games, returns only the profile. Does not match ROMs, open media payloads, save files or apply native export.", nil, s.Ref(exportPreviewResponse{}))
		preview["requestBody"] = httpdocs.Schema{"required": true, "content": httpdocs.Schema{"application/json": httpdocs.Schema{"schema": s.Ref(exportPreviewRequest{})}}}
		paths["/api/export-preview"] = httpdocs.Schema{"post": preview}
		paths["/api/export-options"] = httpdocs.Schema{"get": op("Read export source defaults", "Catalog-configured metadata/artwork/video source order for the visual chooser. No game/media scan. Omitted settings use defaults; explicit empty arrays disable automatic providers while saved catalog overrides still apply.", nil, s.Ref(exportSourceOptions{}))}
		for path, item := range paths {
			method := "get"
			if path == "/api/export-preview" {
				method = "post"
			}
			get := item.(httpdocs.Schema)[method].(httpdocs.Schema)
			get["operationId"] = "browser" + strings.NewReplacer("/", "_", "-", "_").Replace(path)
		}
		stats := paths["/api/stats"].(httpdocs.Schema)["get"].(httpdocs.Schema)["responses"].(httpdocs.Schema)["200"].(httpdocs.Schema)["content"].(httpdocs.Schema)
		stats["application/x-ndjson"] = httpdocs.Schema{"schema": httpdocs.Schema{"type": "string"}, "example": "{\"progress\":{\"Stage\":\"checking media\",\"Current\":1}}\n{\"result\":{\"GameCount\":1}}\n"}
		browserSpec = httpdocs.Document("RomMeta browser API", appinfo.Version, "Read-only local catalog browser. No authentication; keep on loopback or a trusted network. The serve runtime composes these browser routes with the portable /v1 consumer API and one OpenAPI document.", paths, s, false)
	})
	return browserSpec
}

// OpenAPI describes the browser routes for composition into the shared server.
func OpenAPI() any { return browserOpenAPI() }
