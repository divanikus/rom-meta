# Read-only catalog API

`rom-meta serve` hosts a read-only browser, catalog API and local media on one
address. It uses the portable catalog without provider acquisition or a new
authoritative database. Consumers include the
[RomM importer](romm-server-importer.md) and
[ES-DE exporter](offline-export.md#export-from-a-remote-catalog).

## Serve a complete catalog for native-inventory lookups

```shell
rom-meta serve --data-root /catalog
```

Catalog mode needs no ROM mount, token, profile or `--all`. Default address:
`127.0.0.1:8787`; use `--addr` to change it. `--profile` overrides priorities from
`sources.yaml`. Startup reads platform headers/profile, not the full media library.
The browser (`/`), API index (`/api`) and documentation (`/docs`, `/openapi.json`)
share this process.

`GET /v1/platforms` returns actual catalog folders, `schema_version: 1`,
`mode: catalog`, `system`, `supported`, `aliases` and optional `reason`/
`game_binding`/`name_binding`. `name_binding` advertises metadata lookup by a
complete release name for files/folders, independently of ES-DE launch readers.
`game_binding: true` in a request explicitly permits unverified metadata.
`export_schema_version: 1` and `export_systems` separately advertise ES-DE readers,
including modern systems and MAME. `/v1/health` returns the last published view
without waiting for active work; it does not establish freshness or completion.

Related disc/digital datasets advertise `catalogs` on the hardware entry.
Lookup through that entry searches the listed datasets together; an explicit
member folder keeps its own scope. Wii/Wii U, DSiWare, PS3 PSN and PSP Minis remain
separate DATs. Cross-dataset collisions stay ambiguous. This does not add launch
readers for formats listed only under `name_binding`.

`POST /v1/lookup` accepts client observations:

```json
{"platform":"Sega - Game Gear","inputs":[{"rom":"retail/Example.zip","outer":{"size":123,"sha256":"<64 hex>"},"payload":{"size":256,"sha1":"<40 hex>","sha256":"<64 hex>","md5":"<32 hex>","crc32":"<8 hex>","member":"Example.gg"}}]}
```

The response uses the Selection contract, with coverage reasons for empty results.
`rom` is a relative identity, never a server path. Cartridge proofs must agree
with one release; explicit `game_binding: true` also permits a unique title/alias
match. The client must establish and recheck the actual outer-file/payload relation.
The API supplies neither ownership proof nor native frontend IDs.

Requests are limited to 32 MiB. The importer batches 100 native games by default,
without a collection ceiling. One metadata index and selected stat-bound asset
hashes are cached in memory; source documents remain pinned. Selected artwork is
hashed lazily, so lookups may take minutes; there is no five-minute response
limit. Memory use follows selected data, not all catalog media. Source/API errors
fail planning rather than becoming unsupported-platform skips.

## Metadata-only cartridge hashes

These systems use bounded raw/ZIP payload observations independently of launch
readers. The raw formats listed below are hashed as stored, without header
stripping, byte swapping or interleaving conversion.

| Catalog platform | `system` | Raw payload extensions |
| --- | --- | --- |
| Nintendo Satellaview | `satellaview` | `.bs` |
| Atari 2600 / 5200 | `atari2600` / `atari5200` | `.a26` / `.a52` |
| Atari Jaguar (J64) / Lynx (LYX) | `atarijaguar` / `atarilynx` | `.j64` / `.lyx` |
| WonderSwan Color | `wonderswancolor` | `.wsc` |
| MSX / MSX2 | `msx` / `msx2` | `.rom` |
| PC Engine / TurboGrafx-16 | `pcengine` | `.pce` |
| Nintendo DS | `nds` | `.nds` |
| Virtual Boy | `virtualboy` | `.vb` |
| Sega Beena | `beena` | `.bin` |
| Sega 32X | `sega32x` | `.32x`, `.bin`, `.md`, `.gen` |

Matching uses catalog size and hashes, so renamed or dated filenames can select
the exact DAT release. ZIP payloads use the same extension rules.
An unmatched payload remains unmatched even with `game_binding: true`; it is not
replaced with a title-only guess. Unsupported formats may still use the existing
explicit complete-name observation path.

These metadata observations do not add an ES-DE launch/export reader. The
`export_systems` capability remains the separate launch contract. Use matching
current builds of the server and native importer when a new observation system
is introduced.

## Catalog names for other formats

With `game_binding: true`, a `catalog` observation accepts any regular file or
folder whose complete name uniquely identifies a catalog release. No region,
revision or square-bracket label is discarded. It contains `kind: name-stat-v1`,
`directory` and sorted `files` (`name`, `size`, `mtime_ns`); `outer.sha256` and
`payload` hashes stay empty. Native consumers recheck these facts before writes.
This supports metadata for RVZ/WUX/ISO, other cartridge archives and installed
folders without inventing format or dump verification. Same-edition numbered
disc folders can use explicit member names. Translations need their own prepared variant binding.

MAME uses exact `authority.entry_name` identifiers with current `mame` source and
snapshot evidence. Underscores remain significant; internal component filenames
do not compete with a set identifier.
Set version, members, BIOS/parent/CHD completeness and gameplay remain unverified.
Missing or ambiguous source names are reported, never guessed. This capability
does not add ES-DE launch readers; `export_systems` is a separate contract.

PSP CHD metadata uses `system: psp` observations with `game_binding: true`.
A standalone 2048-byte-sector UMD header can select one Redump metadata basis
by declared raw SHA-1 and logical size, including renamed files. The response
keeps `release` empty and records `metadata_release`; it proves no payload hash
or dump integrity. Conflicts and unprepared translations stay unresolved. PSP
Minis need their separate No-Intro DAT and Minis provider mappings (LaunchBox
`Sony PSP Minis`, ScreenScraper system `172`). Enrich them separately before
combining with UMD cards. This metadata reader adds no ES-DE launch/export support.

## ES-DE projection from client observations

`POST /v1/export` uses the local exporter. Required fields: `platform` (catalog
folder), `system` (advertised ES-DE ID), `inputs`. Optional fields: YAML `profile`,
`catalog_snapshot`, `revision`. The CLI `export --api` builds these observations;
see `/docs` for full schemas.

Input `kind` is `payload`, `arcade`, `playlist`, `serial` or `unsupported`.
Payloads contain whole-file or single cartridge ZIP-member hashes. Playlists carry
up to 32 ordered CHD/ISO members; PS3 descriptors carry a bounded serial. ROM
bytes are not uploaded and client paths are never opened on the server.
Name/arcade matches remain weaker than payload proof.

The response contains `schema_version`, catalog/profile `revision`, `plan` and
selected `assets` keyed by logical catalog path. Assets expose same-origin URI,
size and SHA-256 without server filesystem roots. Clients pin the revision,
derive output paths and verify downloads. Finalize display names across all
batches of one system using `title_context`; see [display names](offline-export.md#display-names).

Limits: 32 MiB body, 1,000 launches and 1 MiB profile per request. The CLI batches
100 launches without a collection ceiling. Invalid JSON, unknown fields and
oversized requests return 400. Projection/source conflicts return 409 JSON
`{code, message}`: `snapshot_required` for unresolved MAME snapshot selection,
otherwise `export_unavailable`. `/v1/lookup` retains its stricter proof contract.

Selected deflated media uses seekable OS temporary files: up to 1 GiB/eight idle
members plus active readers. Budget temporary disk space, especially with tmpfs.
Stored video members seek in place. No frontend installation or catalog edit occurs.

## Prepared variants

The shared resolver supports explicit `variants`/`based_on` and exclusive variant
fingerprints. Records use `variant` and `variant_info`; `release` stays empty for
a derivative. `based_on` is optional: without an exact terminal base, the variant
inherits ordinary game metadata. `metadata_release` then identifies the game's
selected metadata default rather than an original-disc identity, and inherited
fields/media use `selection: game`. Explicit bases retain their existing behavior.
Inherited fields/media retain `from_release`; explicit derivative overrides use
`from_variant`. `variant_binding` in platform capabilities advertises this path.
Full named catalog association still requires `game_binding: true` and is weaker
than payload proof. See [the preparation guide](catalog-variants.md).

Export input observations may include `disc` alongside an actual CHD container
payload proof. It carries the observed header; the response preserves both and
labels any combined-hash variant association separately. Clients verify that the
server did not alter either observation. No caller ROM path is opened by the API.

## Optional platform index

`platforms/<platform>/_index.sqlite` is an optional, disposable index of normalized
names, fingerprints and document digests. It contains no full cards, media or
absolute paths and may travel with a platform or be omitted from exports.

A missing index is built on first lookup. Later loads compare metadata file
size/mtime/layout at 100 ns timestamp precision across Linux, Windows and SMB.
Only changed/new bundles are parsed; deleted games are removed. Unchanged packs
need no ZIP reads during this pass. Selected cards are loaded and independently
SHA-checked before response. Logs report cached/rebuilt games.

Index writes do not change catalog payloads. Unwritable/unusable indexes fall back
to source reads; malformed rows rebuild individually. Delete the index to rebuild
it. Stat checks cannot detect equal-size edits preserving mtime: rebuild after
such edits. Selected-document SHA checks still apply.

Read-only catalogs can use prebuilt indexes or a writable directory via
`--lookup-cache-dir /cache`; `--lookup-cache-dir -` disables persisted indexes.
Only one platform index stays in memory; returning to another reuses disk summaries.
Parsed card documents and media-style maps are released after each lookup;
compact names, fingerprints and source digests remain indexed.

## Automatic updates

Normal CLI writes refresh each touched platform index once at completion, including
partial writes that return an error. Index persistence failures produce a nonzero
exit without undoing payloads already written.

The command then atomically replaces `<data-root>/_catalog-revision`.
`/v1/lookup`, `/v1/export` and `/v1/platforms` detect it, reload headers/profile and
clear resolver/file caches. They also detect configured profile/sources edits.
Normal updates require no restart; health and asset requests do not reload.

Run catalog writers and consumer imports sequentially. External ZIP/YAML edits
require a reviewed API restart because they publish no marker. After interrupted
finalization, resume the operation or review the catalog and restart. Fixed
selections always need a reviewed restart. Markers/indexes are disposable;
ZIP/YAML remains authoritative, including on read-only API mounts.

## CHD game observations

Catalog lookup accepts `disc` with `kind: chd-header-stat-v1`, `directory` and
ordered `files`. Each file carries basename, physical size, nanosecond mtime and
the actual 124-byte CHD v5 header as hex. `outer.size` sums container sizes;
`outer.sha256` and payload hashes are empty. Header/embedded hashes and stat facts
are never treated as whole-file SHA-256 or Redump track proof. Native
`romm inspect-inputs` generates this transport; upgrade API/CLI/importer together.

Supported inputs are self-contained CHD v5 files or folders of 2..32 CHDs numbered
Disc 1..N. A folder needs one edition stem, no extra files, missing/duplicate
numbers or additional catalog discs. Every title must resolve to one card;
compacted folder labels supply no identity. Mixed, incomplete, ambiguous and
unsupported inputs are skipped. Prepared standalone translated CHDs can use an
explicit variant combined-SHA1 fingerprint; these return `variant-chd-header`
with inherited base metadata, separate from payload/Redump verification. Other bindings
are always `game-name`, without a proven release/dump. Profile/provenance still
controls metadata/media selection. Complete release names take precedence over
shared game-title aliases; they select an unverified metadata basis and keep
`release` empty. A conflicting complete name remains ambiguous.

Before apply, the importer rechecks native paths, headers, sizes and mtimes without
hashing/decompressing entire CHDs. This detects ordinary library drift, **not
integrity**: same-size/same-mtime/header edits are invisible. Verify CHDs separately
when required. Raw/ZIP cartridge and other frontend export defaults are unchanged.

## Optional access protection

Local servers require no authentication by default. Set `ROMMETA_API_TOKEN` for
shared protection; a nonempty token must be at least 32 characters. Supply it via
environment/secret storage, never a URL or argument.

Protection covers browser, metadata, health and media routes. Consumers send
`Authorization: Bearer <token>`; browsers can use HTTP Basic with username
`rommeta` and that token as password. There are no accounts, roles or sessions.
Documentation/static assets stay public so Swagger can acquire credentials.
Use TLS outside a trusted network.

## Fixed-selection mode

### Start a system snapshot

Supply both selection flags instead of catalog lookup:

```shell
rom-meta serve --data-root /catalog --rom-root /selection --platform gamegear
```

This creates one immutable API selection while retaining browser access.
`--limit 0` (default) includes all candidates; positive limits sample and negative
limits fail. `--match` filters configured ROM paths. Clients cannot select roots;
input/profile/catalog changes require a reviewed restart.

Only exact single-file payload matches are included. Unsupported, unmatched,
ambiguous or name-only candidates normally abort startup. `--skip-unmatched`
retains exact matches and records rejected paths/status/reasons in digest-bound
`coverage`. `coverage.considered` counts supported-reader candidates within the
configured roots/limit, not every file. Exclude firmware/support roots yourself.

There is no 1,000-input or 64 MiB response ceiling, but snapshots/plans reside in
memory. RAM, staging space and startup time depend on selection size.

Startup reuses payload size/SHA1/SHA256/MD5/CRC32 and ZIP member evidence; ZIPs also
receive an outer SHA-256. Regular ROM inputs are bounded to 32 GiB, independently
of the 512 MiB asset bound and existing platform limits. Game Gear accepts its own
catalog-proven SMS-mode `.sms` dumps without a Master System lookup. This does not
provide arbitrary optical, raw-track or multidisc proof.

Wait for `/v1/health` (authenticated if protected). Enumeration and selected-media
hashing complete before HTTP starts; follow logs rather than repeatedly restarting
slow preparation. Only selected cards' registered media is hashed; `_fold`,
arbitrary trees and symlinks are excluded. Review selected media bytes as well as
card counts. Later requests do not rehash every video.

## Contract v1

| Route | Response |
| --- | --- |
| `GET /v1/health` | Contract version, input/card counts and snapshot digest |
| `GET /v1/selection` | Resolved inputs, policy, original source documents, fingerprints and registered media refs |
| `GET /v1/assets/{id}` | One registered local asset; HEAD/Range supported |

Fixed-selection mode accepts GET/HEAD only. It exposes no writes, arbitrary paths,
query tokens, credentials endpoint or provider proxy. Snapshots contain no native
RomM IDs/DTOs, output directory or ES-DE layout.

`inputs` retain resolver records, outer size/SHA-256 and payload proof. `cards`
include parsed data and exact UTF-8 YAML with original size/SHA-256, preserving
leading-zero checksums and other lexical values. Source credentials/configs are
never served.

Assets expose kind, actual source, region/language/variant, relative catalog path,
size, SHA-256 and opaque URI. Provenance is separate from a consumer's chosen role;
registered URIs cannot address arbitrary files.

The digest pins the snapshot. Selection reads rehash small source documents;
ROM/assets use captured file identity, size, mtime and containment/open-file checks
instead of rehashing videos/discs on each request. These checks assume immutable
sources and miss same-size/same-mtime edits. Consumers must verify actual input
and download digests before apply. Detected drift returns 409; review changes
before refreshing.

## Container build

Cross-build a static Linux binary at `bin/linux-amd64/rom-meta`, then use
`docker/Dockerfile.api`. The image runs as UID/GID 568 with a read-only catalog,
optional profile/selected-ROM mounts and optional token environment. It needs no
RomM database or Resources access.

The image sets `GOMEMLIMIT=1536MiB` for a 2 GiB container. Override it for other
runtime limits, leaving space for non-Go memory and temporary files; see the
[Go memory-limit guide](https://go.dev/doc/gc-guide#Memory_limit).

Use prebuilt indexes, a writable cache mount or disabled persistence. Deflated
media also needs writable temporary space: the scratch image has no default
`/tmp`. For example, mount `--tmpfs /tmp:rw,nosuid,size=2g`, budgeting its memory,
or use disk-backed storage. `.dockerignore` excludes Git, catalogs and private
scratch/environment files from the prebuilt-binary build context.

## Cover style evidence

Cover assets expose `style`, `style_evidence` and bound provider `source_type` when
available. Saved LaunchBox front categories and ScreenScraper box-2D supply naming
provenance; missing/ambiguous evidence stays unknown. No pixel/ratio guessing,
image transformation or foreign attribution occurs. Style and regional tie
preference are profile opt-ins; see [cover selection](cover-refresh.md).

## OpenAPI and interactive documentation

`/openapi.json` serves OpenAPI 3.0.3; `/docs` serves embedded Swagger UI. Schemas
come from JSON structs, with constraints/examples added for endpoint semantics.
Catalog mode documents lookup/platforms/export; snapshot mode documents selection.
Both include browser routes and asset HEAD/Range.

**Try it out** sends real requests to this origin without CORS or another service.
In protected mode, **Authorize** accepts the token without `Bearer`, or HTTP Basic
credentials. Authorization stays in page memory. Swagger UI 5.34.0 and its
assets/notices are vendored; no CDN or external validator is used.

## Browser statistics reference

The browser links to `/api` and `/docs`. Stats accepts optional `platform` and
`stream=1`. Streaming emits newline-delimited `progress`, then `result` or `error`;
a failure after streaming starts uses HTTP 200 with an error frame.

Stats defaults to one platform. Full scans are explicit and may be expensive.
Cancellation stops between bounded game batches after in-flight filesystem calls.
No writable acquisition cache or media payload scan is involved.

Prepared CHD variants can enter `/v1/lookup` with explicit `game_binding: true`.
The response retains `variant-chd-header` and its bounded observation; it does
not assert raw payload integrity or original-release verification. Matching
native consumers keep derivative provenance separate from original provider data.

## Explicit release names

A release can declare `aliases: ["Alternative complete filename"]` for a reviewed
local spelling. Lookup uses these exact normalized names and retains the original
release key and DAT authority. Aliases identify an edition by name, not by hash;
`game_binding` remains required for catalog name/stat inputs. Colliding aliases
remain ambiguous, and other regions/revisions are not inferred. For example:

```yaml
releases:
  "Example (USA, Canada) (v02.00)":
    aliases: ["Example (USA) (v02.00)"]
```
