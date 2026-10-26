package catalogapi

import (
	"strings"
	"sync"

	"rommeta/internal/appinfo"
	"rommeta/internal/exporter"
	"rommeta/internal/httpdocs"
)

var apiSpecs [4]struct {
	once  sync.Once
	value any
}

// OpenAPI returns the contract for the actual server mode, without catalog data.
func OpenAPI(lookup, protected bool) any {
	index := 0
	if lookup {
		index = 1
	}
	if protected {
		index += 2
	}
	entry := &apiSpecs[index]
	entry.once.Do(func() {
		schemas := httpdocs.NewSchemas()
		selection := schemas.Ref(Selection{})
		str := httpdocs.Schema{"type": "string"}
		text := func(description string) httpdocs.Schema {
			return httpdocs.Schema{"description": description, "content": httpdocs.Schema{"text/plain": httpdocs.Schema{"schema": str}}}
		}
		errors := func() httpdocs.Schema {
			return httpdocs.Schema{"400": text("Invalid request, unknown field, trailing JSON or query parameters"), "401": text("Missing/invalid bearer token"), "405": text("Unsupported method"), "409": text("Source drift or lookup unavailable; finish catalog writes before retrying")}
		}
		paths := httpdocs.Schema{}
		health := httpdocs.Schema{"type": "object", "properties": httpdocs.Schema{"schema_version": httpdocs.Schema{"type": "integer", "enum": []int{1}}, "mode": httpdocs.Schema{"type": "string", "enum": []string{"catalog"}}, "export_schema_version": httpdocs.Schema{"type": "integer", "enum": []int{1}}, "platforms": schemas.Ref([]exporter.LookupPlatform{})}, "required": []string{"schema_version", "mode", "export_schema_version", "platforms"}}
		addGet := func(path, summary, description string, result httpdocs.Schema) {
			responses := errors()
			responses["200"] = httpdocs.Response("Success", result)
			op := httpdocs.Schema{"summary": summary, "description": description, "responses": responses}
			head := httpdocs.Schema{}
			for key, value := range op {
				head[key] = value
			}
			paths[path] = httpdocs.Schema{"get": op, "head": head}
		}
		if lookup {
			addGet("/v1/health", "Check API availability", "Last published capability view. Does not wait for a lookup or assert source freshness.", health)
			addGet("/v1/platforms", "List platform capabilities", "Refreshes completed CLI changes. Unsupported readers include explicit reasons.", health)
			request := schemas.Ref(LookupRequest{})
			schema := schemas.Types["LookupRequest"].(httpdocs.Schema)
			props := schema["properties"].(httpdocs.Schema)
			schema["additionalProperties"] = false
			if disc, ok := schemas.Types["DiscObservation"].(httpdocs.Schema); ok {
				dp := disc["properties"].(httpdocs.Schema)
				dp["kind"] = httpdocs.Schema{"type": "string", "enum": []string{"chd-header-stat-v1"}}
				files := dp["files"].(httpdocs.Schema)
				files["minItems"] = 1
				files["maxItems"] = 32
			}
			if file, ok := schemas.Types["DiscFile"].(httpdocs.Schema); ok {
				file["properties"].(httpdocs.Schema)["header"] = httpdocs.Schema{"type": "string", "pattern": "^[0-9a-f]{248}$", "description": "Actual 124-byte CHD v5 header as lowercase hex; not a whole-file digest."}
			}
			props["platform"] = httpdocs.Schema{"type": "string", "minLength": 1}
			props["inputs"].(httpdocs.Schema)["nullable"] = false
			props["media"] = httpdocs.Schema{"type": "string", "enum": []string{"", "covers", "none"}, "description": "Empty: inventory all selected media; covers: selected covers only; none: no asset bytes. Large media may take minutes to hash."}
			responses := errors()
			responses["200"] = httpdocs.Response("Selection, coverage, pinned cards/assets and provenance", selection)
			paths["/v1/lookup"] = httpdocs.Schema{"post": httpdocs.Schema{"summary": "Resolve client-observed ROM inputs", "description": "Read-only; request body limited to 32 MiB. ROM identities are never opened as server paths. Exact cartridge proofs must agree with one release. game_binding explicitly permits unverified game-name metadata. Typed CHD header/stat observations never prove an optical dump. Run catalog preparation and imports sequentially. Selected documents/assets retain SHA checks; no response deadline during large media hashing.", "requestBody": httpdocs.Schema{"required": true, "content": httpdocs.Schema{"application/json": httpdocs.Schema{"schema": request, "example": httpdocs.Schema{"platform": "Sega - Game Gear", "inputs": []any{}, "media": "none"}}}}, "responses": responses}}
			exportRequest := schemas.Ref(ExportRequest{})
			exportResponse := schemas.Ref(ExportResponse{})
			titles := schemas.Types["TitlePolicy"].(httpdocs.Schema)
			titles["additionalProperties"] = false
			tp := titles["properties"].(httpdocs.Schema)
			tp["mode"] = httpdocs.Schema{"type": "string", "enum": []string{"regional", "release", "filename"}, "default": "regional", "description": "Edition-local title, full authority release name, or client filename stem. Never the common game-card title."}
			tp["labels"] = httpdocs.Schema{"type": "string", "enum": []string{"keep", "drop"}, "default": "keep", "description": "Preserve or remove opaque balanced square-bracket blocks; no interpretation of their contents."}
			tp["variants"] = httpdocs.Schema{"type": "string", "enum": []string{"none", "auto", "release", "filename"}, "default": "auto", "description": "Disambiguation over the complete visible selection of one system. Auto: region, then revision/language when needed, then input names."}
			schemas.Types["TitleContext"].(httpdocs.Schema)["description"] = "Unformatted edition title data. Clients must finalize titles over the complete system after collecting all API batches; a batch alone cannot determine all visible variants."
			schemas.Types["ExportRequest"].(httpdocs.Schema)["additionalProperties"] = false
			ep := schemas.Types["ExportRequest"].(httpdocs.Schema)["properties"].(httpdocs.Schema)
			ep["inputs"].(httpdocs.Schema)["maxItems"] = 1000
			ep["inputs"].(httpdocs.Schema)["nullable"] = false
			ep["profile"].(httpdocs.Schema)["description"] = "Optional YAML policy bytes parsed against catalog defaults; never a server filename"
			ep["profile"].(httpdocs.Schema)["maxLength"] = 1 << 20
			observed := schemas.Types["ExportInput"].(httpdocs.Schema)["properties"].(httpdocs.Schema)
			observed["kind"] = httpdocs.Schema{"type": "string", "enum": []string{"payload", "arcade", "playlist", "serial", "unsupported"}}
			observed["rom"] = httpdocs.Schema{"type": "string", "minLength": 1, "maxLength": 8192, "description": "Safe relative client identity; never opened on the server"}
			observed["parts"].(httpdocs.Schema)["maxItems"] = 32
			observed["serial"].(httpdocs.Schema)["maxLength"] = 9
			observed["reason"].(httpdocs.Schema)["maxLength"] = 4096
			assetProperties := schemas.Types["ExportAsset"].(httpdocs.Schema)["properties"].(httpdocs.Schema)
			assetProperties["sha256"] = httpdocs.Schema{"type": "string", "pattern": "^[0-9a-f]{64}$"}
			assetProperties["uri"] = httpdocs.Schema{"type": "string", "pattern": "^/v1/assets/[0-9a-f]{64}$"}
			assetProperties["size"].(httpdocs.Schema)["minimum"] = 0

			exportResponses := errors()
			exportResponses["409"] = httpdocs.Response("Export unavailable or source changed; snapshot_required requires --arcade-snapshot", schemas.Ref(ExportError{}))
			exportResponses["200"] = httpdocs.Response("ES-DE projection, revision and selected SHA-bound assets", exportResponse)
			paths["/v1/export"] = httpdocs.Schema{"post": httpdocs.Schema{"summary": "Project client ROM observations to ES-DE metadata", "description": "Read-only catalog operation, same readers/binding confidence as local export including disc images, directory playlists, PS3 descriptors and saved MAME set names. No ROM upload or server-side client paths. Request <=32 MiB, <=1000 launches; clients batch without a total collection ceiling. Re-finalize title_context across the complete system after collecting all batches. Revision pins catalog/profile across batches. Selected media only; no ROM selection/rebuilding or frontend installation.", "requestBody": httpdocs.Schema{"required": true, "content": httpdocs.Schema{"application/json": httpdocs.Schema{"schema": exportRequest}}}, "responses": exportResponses}}
		} else {
			snapshotHealth := httpdocs.Schema{"type": "object", "properties": httpdocs.Schema{"schema_version": httpdocs.Schema{"type": "integer"}, "inputs": httpdocs.Schema{"type": "integer"}, "cards": httpdocs.Schema{"type": "integer"}, "digest": str, "coverage": schemas.Ref(Coverage{})}, "required": []string{"schema_version", "inputs", "cards", "digest", "coverage"}}
			addGet("/v1/health", "Check configured snapshot", "Counts and digest for the fixed selection.", snapshotHealth)
			addGet("/v1/selection", "Read configured selection", "Rechecks the pinned source snapshot. Review and restart after external source changes.", selection)
		}
		asset := httpdocs.Schema{"summary": "Read a selected SHA-bound asset", "description": "ID/URI comes from a selection response. HEAD and Range are supported. Source drift returns 409; unknown IDs return 404.", "parameters": []httpdocs.Schema{{"name": "id", "in": "path", "required": true, "schema": str}, {"name": "Range", "in": "header", "required": false, "schema": str, "example": "bytes=0-15"}}, "responses": httpdocs.Schema{"200": httpdocs.Schema{"description": "Asset bytes", "headers": httpdocs.Schema{"ETag": httpdocs.Schema{"schema": str}}, "content": httpdocs.Schema{"application/octet-stream": httpdocs.Schema{"schema": httpdocs.Schema{"type": "string", "format": "binary"}}}}, "206": httpdocs.Schema{"description": "Requested byte range"}, "401": text("Missing/invalid bearer token"), "404": text("Unknown asset ID"), "409": text("Asset changed"), "416": text("Unsatisfiable range")}}
		assetHead := httpdocs.Schema{}
		for key, value := range asset {
			assetHead[key] = value
		}
		paths["/v1/assets/{id}"] = httpdocs.Schema{"get": asset, "head": assetHead}
		for path, item := range paths {
			for method, raw := range item.(httpdocs.Schema) {
				raw.(httpdocs.Schema)["operationId"] = method + strings.NewReplacer("/", "_", "{", "", "}", "").Replace(path)
			}
		}
		description := "Catalog metadata and assets are read-only. No query parameters. Optional indexes are disposable; normal CLI writes refresh catalog mode automatically. External archive edits require reviewed restart. CHD observations are game metadata only."
		if protected {
			description += " Bearer protection is enabled; only documentation is public."
		} else {
			description += " No token is configured; the shared server may supply optional access control."
		}
		entry.value = httpdocs.Document("RomMeta consumer API v1", appinfo.Version, description, paths, schemas, protected)
	})
	return entry.value
}
