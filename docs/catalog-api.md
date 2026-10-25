# Read-only catalog API

`rom-meta serve` hosts the browser, prepared offline metadata API and local media on one address, independently
of RomM or any other frontend. It reads the existing portable catalog and resolver;
it adds no authoritative database, provider acquisition or catalog payload write.
A separate
[RomM server importer](romm-server-importer.md) and the built-in
[ES-DE network exporter](offline-export.md#export-from-a-remote-catalog) are consumers.

## Serve a complete catalog for native-inventory lookups

```shell
rom-meta serve --data-root /catalog
```

Catalog lookup is the default. No ROM mount, token or explicit profile is required.
Default bind is `127.0.0.1:8787`; `--addr` changes it. Optional `--profile` overrides
source priorities from `sources.yaml`. Startup reads platform headers/profile, not
the full media library. Browser `/`, API discovery `/api`, Swagger `/docs` and
OpenAPI `/openapi.json` share the same process. See optional protection below.
Catalog lookup uses no `--all` flag; `serve` is the only server command. `GET /v1/platforms` (also health) returns
`schema_version: 1`, `mode: catalog` and actual catalog folder names with `system`,
`supported`, optional `reason` and `game_binding`. The nine cartridge families
retain exact payload readers. CHD-capable disc systems additionally advertise
`game_binding: true`: bounded container observations permit game metadata only,
not optical/track verification. These fields describe the stricter `/v1/lookup`
contract. `export_schema_version: 1` and each platform's `export_systems` advertise
the separate ES-DE projection capabilities, including modern systems and MAME.

**Read-only** `POST /v1/lookup` accepts:

```json
{"platform":"Sega - Game Gear","inputs":[{"rom":"retail/Example.zip","outer":{"size":123,"sha256":"<64 hex>"},"payload":{"size":256,"sha1":"<40 hex>","sha256":"<64 hex>","md5":"<32 hex>","crc32":"<8 hex>","member":"Example.gg"}}]}
```

The response uses the existing Selection contract (including empty exact results
with coverage reasons). `rom` is an opaque relative input identity; it is never
opened on the server. Exact cartridge fingerprints must agree with one catalog release. Explicit
`game_binding: true` additionally permits a unique title/authority alias, never a
DAT verification. Native IDs, client filesystem roots and writes are absent.
The client must independently establish and recheck the relationship between the
actual outer file and payload; this endpoint alone does not prove ROM ownership.

Requests are bounded to 32 MiB; the importer automatically splits work (default
100 native games per request), with no total collection ceiling. The API caches
one active metadata index and stat-bound selected asset hashes in memory. Selection
responses are rebuilt with pinned source document checks; assets remain SHA-bound.
Catalog/profile changes are picked up as described below. Selected artwork is hashed lazily on lookup;
a request can take minutes and has no five-minute response deadline. Memory/cache
size grows with selected metadata/assets, not with all unowned catalog media.
`/v1/health` reads the last published capability view without waiting for a long
lookup or media hash; it does not assert source freshness or lookup completion.

Source/API errors fail planning; they are not silently converted into unsupported
platform skips. GET/HEAD asset delivery remains unchanged.

## ES-DE projection from client observations

`POST /v1/export` projects existing client files with the same implementation as
local `export`. It accepts `platform` (actual catalog folder), `system` (advertised
ES-DE ID), `inputs`, optional YAML `profile`, `catalog_snapshot` and `revision`.
The built-in `export --api` client constructs and validates these observations;
use `/docs` or `/openapi.json` for the full request/response schemas.

Inputs carry a safe relative ROM identity and `kind`: `payload`, `arcade`,
`playlist`, `serial` or `unsupported`. Payloads use whole-file hashes, or the
single selected cartridge ZIP member's hashes. Playlists carry at most 32 ordered
CHD/ISO members; PS3 descriptors carry a bounded serial observation. No client
path is opened on the server and no ROM payload is uploaded. Names/hashes are
caller observations; weak name/arcade binding never becomes payload proof.

The response contains `schema_version`, catalog/profile `revision`, an export
`plan` and selected `assets` keyed by logical catalog path. Each asset has an
opaque same-origin URI, byte size and SHA-256. It contains no server filesystem
root. Clients pin the revision across requests, derive output paths locally and
verify downloads; the endpoint performs no frontend installation or catalog edit.

The request limit is 32 MiB / 1,000 launches, with profile bytes limited to 1 MiB.
The CLI batches 100 launches without a total collection ceiling. Invalid JSON,
unknown fields and oversized requests return 400. Unavailable projection or source
drift returns 409 JSON `{code, message}`; `snapshot_required` identifies unresolved
MAME snapshot selection, while other conflicts use `export_unavailable`.
The stricter RomM `/v1/lookup` contract and its proof limits remain unchanged.

Selected deflated packed media can use the OS temporary directory for seekable
reads. The process keeps up to 1 GiB / eight idle members, plus active readers.
Provide enough temporary disk space; a small container tmpfs can fail even when
catalog and client disks have space. Stored video members seek in place.

## Optional platform index

Catalog lookup keeps a disposable recognition index at
`platforms/<platform>/_index.sqlite`. It stores normalized names, fingerprints and
document digests, without full YAML cards, media or absolute paths. The file can
travel with a platform; it is optional and can be omitted from filtered exports.

The first lookup builds a missing index. Later platform loads enumerate game
folders and check physical metadata file size/mtime/layout (persisted timestamps compare at 100 ns precision for Linux ↔ Windows/SMB portability); only new or changed
bundles are read and parsed, and deleted games are removed from the index.
Unchanged packed games do not need metadata ZIP directory/content reads for this
pass. Full cards are loaded for selected games; source documents are independently
SHA-checked before returning a selection. API logs report cached/rebuilt games.

Index writes are auxiliary cache writes and do not change catalog payloads.
Unwritable storage or an unusable database falls back to source reads; malformed
rows rebuild individually. Delete `_index.sqlite` to recreate a damaged index on
the next platform load. Stat freshness does not detect
equal-size edits deliberately preserving physical mtime; rebuild the index after
such edits. Selected source-document SHA checks remain in place.

For a read-only catalog mount, copy in a prebuilt index, or mount a writable cache
directory and add `--lookup-cache-dir /cache`. This uses separate, catalog-scoped
platform files there. Use `--lookup-cache-dir -` to disable persisted indexes.
The catalog never requires write access for lookup. One active recognition index
remains in memory; returning to another platform reuses its persisted summaries.

## Automatic updates

Normal RomMeta CLI writes maintain the indexes automatically. A command collects
touched platforms and refreshes each index once at completion, even if a partial
write returns an error. Unchanged bundles reuse existing summaries; changed/new
games are parsed and removed games disappear. This includes metadata ingestion,
media updates, game moves and deletions. No separate indexing script is needed.
The writer reports an index persistence failure and a nonzero exit status without
undoing payloads that were already written.

After maintenance the command atomically replaces `<data-root>/_catalog-revision`.
Catalog-mode `/v1/lookup`, `/v1/export` and `/v1/platforms` requests notice that small marker,
reload headers/profile and clear the old resolver/file caches. They also notice
ordinary edits to the configured profile and sources configuration. The next
lookup uses the updated platform index; normal updates need no API restart.
`/v1/health` and asset requests do not reload the catalog.

Run catalog editing/acquisition and RomM imports sequentially. This personal
utility does not coordinate concurrent writers and imports. Existing source/media
drift checks remain. External ZIP/YAML edits do not publish a revision: finish
them and restart the API. If a process is killed before command finalization,
rerun its resumable operation or restart the API after reviewing the catalog.
Fixed-selection mode below still requires a reviewed restart.

The marker and platform indexes are disposable state; ZIP/YAML remains the
source of truth. RO API mounts can read indexes maintained by the normal writable
CLI.

## CHD game observations

Catalog-mode lookups accept an explicitly typed optional `disc` observation with
`kind: chd-header-stat-v1`, `directory` and ordered `files`. Each file contains its
original basename, physical size, nanosecond mtime and the actual 124-byte CHD v5
header encoded as hex. `outer.size` is the sum of container sizes;
`outer.sha256` and payload hashes are empty. Header bytes, embedded CHD hashes and
stat facts are never labeled as whole-file SHA256 or compared to Redump tracks.
The native `romm inspect-inputs` command generates this transport automatically.
Upgrade the API, CLI and importer together.

This reader accepts standalone self-contained CHD v5 files and ordinary folders
of 2..32 CHDs numbered Disc 1..N. A folder must have one edition stem, no extra
files, no missing/duplicate numbers and no additional disc listed for that same
catalog edition. Each member title must resolve to the same unique catalog card;
compacted folder labels do not supply identity. Unsupported, mixed, incomplete or
ambiguous inputs receive an explicit skip. Renamed translations stay deferred.
Every resulting binding is `game-name`, with no proven release or dump flags.
Metadata/media selection continues through the existing profile and provenance.

Before apply the importer compares the native member paths/sizes, exact captured
headers, file sizes and mtimes again. It never hashes or decompresses the complete
CHDs. This is an ordinary immutable-library drift guard, **not an integrity
check**: payload edits retaining size, mtime and header cannot be detected. Run a
separate CHD/dump verification when that evidence is actually required. Existing
raw/ZIP cartridge verification and other frontend exports retain their defaults.

## Optional access protection

The default local server requires no authentication. Set `ROMMETA_API_TOKEN` only
when shared protection is wanted; a nonempty value must have at least 32 characters
or startup fails. Pass it through environment/secret storage, never a URL/argument.
When configured it applies uniformly to browser, metadata, health and media routes.
Existing consumers use `Authorization: Bearer <token>`. A browser can use HTTP Basic:
username `rommeta`, password the same token. This is one service secret; there are
no user accounts, registration, roles or sessions. Documentation/static assets
remain public so Swagger can acquire credentials. Use TLS outside a trusted network.
Existing deployed tokens remain valid; no running deployment is changed by this
local CLI update. Consumers own hostnames, ports and secret storage.

## Fixed-selection mode

### Start a system snapshot

Build the ordinary CLI as described in README and supply both `--rom-root` and
`--platform` to select the fixed snapshot instead of catalog lookup:

```shell
rom-meta serve --data-root /catalog --rom-root /selection --platform gamegear
```

This selects **one immutable configured API selection**; browser inspection remains
available on the same address. Without those selection flags, catalog lookup is
the default. By default it includes all candidates: `--limit 0`
(or omit the flag). A positive `--limit` is an optional sample size, with no fixed
1000-file ceiling. Negative limits are rejected.
Only exact single-file payload matches are included. By default, an unsupported,
unmatched, ambiguous or name-only input aborts snapshot creation. Explicit
`--skip-unmatched` keeps the exact matches and records every rejected candidate's
relative path, status and reason in digest-bound `coverage`; it never assigns a
translation/aftermarket file to a similar retail title. `coverage.considered`
counts supported-reader candidates inside the configured roots and limit, not
all filesystem files. Exclude support/firmware roots from a games-only service.
Use `--limit 20` only for a limited sample; omit it for the complete configured
folder. Review considered/included/skipped coverage rather than claiming that every
file was recognized. The consumer and its Go projection also accept more than
1000 inputs and responses larger than 64 MiB. Snapshots/plans are still materialized
in memory: available RAM, media staging space and preparation time depend on the
selected system, rather than a hidden fixed count/response limit.
`--match` can constrain a configured ROM path. Changing inputs/profile/catalog
requires a reviewed restart; client requests cannot choose filesystem roots.

Startup reuses the shared resolver's actual payload size/SHA1/SHA256/MD5/CRC32 and
ZIP member identity. Raw-ROM proof is not reread just to construct the API response;
ZIP containers additionally receive an outer-file SHA256. Trusted regular ROM
inputs are bounded to 32 GiB independently of the 512 MiB asset bound. Existing
reader/platform limits still apply. Game Gear also accepts its own catalog-proven
SMS-mode `.sms` dumps, raw or single-payload ZIP, without a Master System lookup.
This does not establish arbitrary optical,
raw-track, multidisc or full disc verification support.

Wait for an **authenticated** `/v1/health` response after changing the API scope.
An App/container being RUNNING does not mean its immutable snapshot is ready:
initial enumeration and selected-media hashing happen before HTTP starts. The
whole Game Gear preparation reads about 7 GiB once and can take minutes. Follow
startup logs and wait for readiness; do not create a restart loop that repeats
those reads. A later request does not hash every video again.

Only media belonging to the selected cards is inventoried/hashed, including its
ordinary registered release folders; `_fold`, arbitrary trees and symlinks are
excluded. The API never copies the full media collection. Twenty selected games
can still have substantial art/video, so review that selection's byte count.

## Contract v1

| Route | Response |
| --- | --- |
| `GET /v1/health` | Contract version, input/card counts and snapshot digest |
| `GET /v1/selection` | Portable resolved inputs, policy, original source documents, fingerprints and registered media refs |
| `GET /v1/assets/{id}` | Exactly one registered local asset; HEAD/Range supported |

In fixed-selection mode only GET/HEAD are accepted. No mutation, arbitrary path, query-token, credentials
endpoint or external-provider proxy exists. A snapshot contains no RomM-native
DTO, database identity, output directory or ES-DE package layout.

`inputs` retain the portable resolver record, outer file size/SHA256 and complete
payload proof. `cards` contain raw parsed source data **and exact UTF-8 YAML** with
the original document size/SHA256. Lossless YAML preserves leading-zero checksums
and other lexical values that generic JSON number conversion cannot preserve.
Clients can use parsed data for ordinary display and lossless source for typed
provider/authority interpretation. No source credentials/config files are served.

Every asset has canonical kind, real source, region/language/variant, relative
catalog path, size, SHA256 and an opaque URI. Provenance is distinct from the
consumer's chosen role. The URI is registered from the configured selection;
it cannot be turned into an arbitrary filesystem reader.

The snapshot digest pins its exact content. Small source documents are rehashed
on selection reads. ROM/asset requests use captured file identity, size and mtime
checks, with containment/open-file checks; they do not hash whole disc images or
videos for every metadata request/seek. These stat checks assume an immutable
read-only source snapshot and do not detect adversarial same-size/same-mtime edits.
Consumers must verify actual input/download digests before applying changes.
Detected drift returns 409; refresh only after reviewing source changes.

## Container build

Cross-build a static Linux binary to `bin/linux-amd64/rom-meta`, then use
`docker/Dockerfile.api`. The image runs as UID/GID 568 and needs only read-only
catalog mount, optional profile/selected ROM mounts and an optional token environment. It has
no RomM DB or Resources access. Optional index updates require a writable cache
mount, or can be disabled; a prebuilt platform index can be read from the catalog.
The scratch image also needs a writable temporary directory for selected deflated
members. For example, mount `--tmpfs /tmp:rw,nosuid,size=2g`; budget memory for that
tmpfs or use a disk-backed temporary mount. The image contains no default /tmp.
The minimal `.dockerignore` excludes historical
scratch, catalogs, private env files and Git from the prebuilt-binary build context.

The first real deployment is operated by the consumer project, independently of
its catalog-installed RomM App. A fixed-snapshot API restart does not change RomM.
A general index/query API and multiple configured collections remain follow-ups,
not an excuse to hash all downloaded media on startup.

## Cover style evidence

Cover assets and selected media expose `style` / `style_evidence` and an exact
bound provider `source_type` where available. Saved LaunchBox front categories and
ScreenScraper box-2D are bound through existing target/legacy naming projection;
other/missing/ambiguous assets remain unknown. No ratio/pixel/style guess, image
mutation or foreign provider attribution is implied. Cover style and regional tie
preference are explicit profile opt-ins; default and other bundled export behavior
remain unchanged. See [the selection and reviewed refresh guide](cover-refresh.md).

## OpenAPI and interactive documentation

`/openapi.json` is the common OpenAPI 3.0.3 document and `/docs` is embedded Swagger
UI. It combines browser routes with active consumer API v1 routes: catalog mode
includes lookup/platforms; fixed-selection mode includes selection. Both document
asset HEAD/Range. DTO schemas come from actual JSON structs, including embedded
fields, nullable collections, selection/coverage/provenance and typed observations.
Endpoint constraints/examples describe semantics beyond Go types.

Try it out executes real requests against this same server origin. No second
service, API URL or CORS setup is needed. Optional security declarations reflect
whether a token is configured. In protected mode Authorize accepts a bearer token
without `Bearer`, or HTTP Basic credentials. Authorization remains in page memory.
Swagger UI 5.34.0/assets/license/notice are vendored; no CDN or external validator is
contacted.

## Browser statistics reference

The header links to `/api` discovery and `/docs` Swagger. Stats supports optional
`platform` and `stream=1`: newline-delimited `progress` frames followed by `result`
or `error`. Once streaming starts, failures use an error frame under HTTP 200.
Browser Stats scans the selected platform by default; a full catalog scan is
explicit and may be expensive. Cancellation stops between bounded game batches after in-flight filesystem calls; no writable
acquisition cache or media payload scan is involved.
