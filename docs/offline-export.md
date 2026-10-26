# ES-DE export

Generate gamelists, selected media and reports for existing ROMs. Local mode reads
a prepared catalog offline; `--api` uses that catalog through `serve`. Neither
mode modifies ROMs/catalogs. ES-DE/emulator integration remains experimental.

## Quick start: existing ES-DE ROMs

Prepare/import/fetch the catalog once. Export then generates metadata for the
files you supply, without selecting or copying ROMs.

### 1. Identify the inputs and frontend paths

Find the actual system directory ES-DE launches, e.g. `/games/roms/nes`, plus its
configured gamelist and media locations. These may be on different disks; batch
mode does not infer custom nested system roots. The catalog must contain that
platform's metadata/media. Check the [input formats](export-scenarios.md#es-de-input-coverage).

### 2. Preview one system

Use a new output directory outside both catalog and ROM trees. `--limit 10`
selects a sample:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --out "/exports/nes-check" --limit 10
```

Preview creates no package. Review match confidence, ambiguous/unmatched/
unsupported inputs, description/cover coverage and selected media bytes.

### 3. Create and inspect a package

Repeat with `--apply`:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --out "/exports/nes-check" --limit 10 --apply
```

Open `preview.html`; inspect `report.txt`, `manifest.json`, `gamelists/nes/gamelist.xml`
and media. The manifest records recognition and source/language/media evidence;
`README.txt` explains installation. Check actual display in ES-DE separately.

For the full system, omit `--limit` and use a new output directory. For priorities,
copy [the library profile](../examples/export-es-de-library.yaml) and pass
`--profile "/path/to/profile.yaml"` on preview/apply. Export never fetches missing
provider content.

### 4. Install into ES-DE

Close ES-DE and back up existing metadata/media before copying reviewed output:

| Package path | Frontend destination |
| --- | --- |
| `gamelists/nes/gamelist.xml` | `<ES-DE-home>/gamelists/nes/gamelist.xml` |
| Contents of `downloaded_media/nes/` | `<configured-media-root>/nes/` |

Keep ROM names/subdirectories used by `./` launch paths. Use ES-DE's configured
locations; XML next to ROMs requires separate legacy configuration.

For existing gamelists, merge by launch path to preserve favorites, playcounts,
emulator overrides and unrelated entries. Replacing XML loses these values.
The CLI does not install or merge output. Reload ES-DE and check paths/display.

### 5. Export several systems

Use the parent ROM directory with immediate folders such as `nes/` and `snes/`:

```shell
rom-meta export --target es-de --platforms nes,snes,megadrive --data-root "/path/to/catalog" --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/three-systems"
```

Review, then repeat with `--apply`. `--all` discovers supported folders;
`--platforms` selects named ones. Use a separate `--platform` export for custom
nested system paths, including custom Xbox 360 roots.

## Prepared translations

A catalog can supply explicit `variants`/`based_on` relationships and modified
fingerprints. Export recognizes those files and inherits the base metadata/media
while preserving translated display identity and launch paths. See
[variant preparation and proof limits](catalog-variants.md).

## Export from a remote catalog

On the catalog machine, bind `serve` to its LAN address (default is loopback):

```shell
rom-meta serve --data-root "/catalog" --addr "192.168.1.10:8787"
```

On the machine holding the ES-DE ROMs:

```shell
rom-meta export --api "http://192.168.1.10:8787" --platform nes --rom-root "/games/roms/nes" --out "/exports/nes-package"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/all-package"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/all-package" --apply
```

`--platforms nes,gba,psx,mame` selects immediate folders. `--api` excludes
`--data-root`. `--list-systems` lists client readers; server catalog contents
determine availability. Custom roots still need separate `--platform` exports.

The client hashes ROMs locally and sends batches of 100 observations, never ROM
bytes. The server needs no ROM mount. It loads one selected index at a time and
hashes selected media. Preview creates no output or client media downloads.
Progress separates observation, API resolution, download and package writing.
CHD/ISO export reads full containers, unlike the RomM example's limited CHD observations.

`--profile` sends client YAML bytes, not a server filename; otherwise server
defaults apply. If protection is enabled, set `ROMMETA_API_TOKEN` to the server's
token. Origins cannot contain credentials, paths or queries; redirects fail.
See [API access](catalog-api.md#optional-access-protection).

Remote readers match local ones, including PS3 descriptors, directory playlists
and MAME ZIP/7z. `--arcade-snapshot` selects saved server bindings; unresolved
snapshot selection skips a batch system or fails a single-system export.

Apply downloads selected assets, verifies size/SHA-256, rechecks ROMs and stages
one package. Revision/source drift, failures or cancellation abort publication.
Collections have no total ceiling. Retry failures with the same unused output
path; successful exports require a new path. Temporary downloads are not a
persistent resume cache. Installation/merge remains manual.

## Export an ES-DE ROM root

For immediate system folders (`n64/`, `snes/`, `psx/`, `arcade/`):

```shell
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory>
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory> --apply
```

Use one [shared profile](../examples/export-es-de-library.yaml) for all systems.
Choose exactly one of `--all`, `--platforms n64,snes,psx,arcade` or single-system
`--platform n64` with its actual system directory. `--list-systems` lists readers.
`--limit` applies **per system**; `--match` filters relative filenames. No 1G1R or
automatic region/revision filtering occurs.

Preview writes nothing. Apply stages every selected system before publishing one
package; later failure leaves no partial final output. Output must be new and
outside both input trees.

Discovery uses immediate non-hidden directories and `_platform.yaml` identity.
Unknown/missing/duplicate platforms, absent folders, empty selections and unresolved
MAME snapshots are reported as skips. Malformed selected data/profile or I/O errors
stop the batch. Skips generate no files; an all-skipped batch creates no package.
Escaping system-directory links are skipped; individual ROM/media symlinks are not used.

## Supported launch readers

| ES-DE folders | Inputs |
| --- | --- |
| n64 | .z64, .v64, .n64 |
| nes | .nes, .unf, .unif |
| snes | .sfc, .smc, .bin |
| gb | .gb, .dmg, .sgb, .gbx |
| gbc | .gbc, .cgb, .gb, .gbx |
| gba | .gba, .agb, .bin |
| gamegear / mastersystem | .gg / .sms, .bin, .rom |
| megadrive / genesis | .md, .gen, .bin, .smd |
| 3do, psx, ps2, dreamcast, saturn, segacd, pcenginecd | standalone .chd, .iso |
| psx, saturn, dreamcast | directory-style .m3u with same-name inner playlist and relative CHD/ISO members |
| ps3 | .iso, small .ps3 serial descriptors (explicit title binding only) |
| xbox / xbox360 | .iso; Xbox also .xiso |
| arcade / mame | .zip, .7z set-name projection through saved MAME bindings |

Cartridge raw files and single-payload ZIPs have a 512 MiB payload limit.
Standalone disc/arcade containers stream without that limit. Byte-order/header
conversion, image decryption, XISO conversion, firmware and emulator checks are
not implemented. Disc archives, CUE/GDI and loose M3U are unsupported.

Loose CHD/ISO files remain separate launches. PSX/Saturn/Dreamcast directory M3U
requires relative regular members bound uniquely to one card, and revalidates all
members. Metadata/media uses the first member without inventing an edition.
PS3 serial content is not a disc fingerprint or PSN authority. For frontend names
and wider emulator-dependent formats, see the official
[ES-DE Windows system configuration](https://gitlab.com/es-de/emulationstation-de/-/blob/master/resources/systems/windows/es_systems.xml).

## Recognition

Ordinary readers try size/SHA1/SHA256 first, then normalized authority/release/file
names and explicit titles/aliases. Every stored fingerprint hash must agree.
Normalization ignores case/spacing/punctuation, retaining qualifier contents.
Multiple candidates or conflicting archive/member names stay ambiguous; no fuzzy
or provider-title inference occurs.

| `matched_by` | Evidence |
| --- | --- |
| `hash` | Byte identity for the stated representation |
| `name` | Weaker release-name match |
| `game-name` | Game known; exact release unknown |

`metadata_release` records the game-only default (shared policy supplies a missing
default); game-only media remains game-wide. Unknown/ambiguous ordinary inputs
retain filename-only entries.

CHD uses `representation: chd-container`. Container hashes can match only recorded
CHD fingerprints, never raw-disc/Redump track hashes. Otherwise use name fallback.
Embedded digests are not raw-track proof; no decompression/integrity check occurs.
See [chdman](https://docs.mamedev.org/tools/chdman.html).

Arcade matches exact archive short names through
`bindings/romsets/mame/<snapshot>/catalog.yaml`. One snapshot auto-selects;
otherwise pass `--arcade-snapshot <id>`. Referenced authority and MAME snapshot
must agree. Records use `arcade-set-name`/`catalog_snapshot`, not payload/version
proof. Unbound/ambiguous archives appear in reports but not gamelists.

MAME projection checks no ROM components, merged/split completeness or parent/
BIOS/CHD/sample dependencies and leaves existing ROMs in place. There is no
FBNeo binding transfer/exporter.

## Package and installation

Output contains `gamelists/<system>/gamelist.xml` and `downloaded_media/<system>/`,
with media paths based on original ROM names/subdirectories. Batch root
`preview.html`, `manifest.json` and `report.txt` summarize systems/skips; detailed
sidecars and installation notes live in `reports/<system>/`. Single-system exports
keep sidecars at the root. Preview media links are relative.

Launch paths use `./`; ROMs are not copied/renamed. Source launches are rehashed
before publication and copied assets carry SHA-256. Installation is manual:
[back up and merge](#4-install-into-es-de) into ES-DE's configured locations.
Keep private reports/paths and licensed media outside Git. See ES-DE's
[gamelist reference](https://gitlab.com/es-de/emulationstation-de/-/blob/master/INSTALL.md#gamelistxml)
and [media naming guide](https://gitlab.com/es-de/emulationstation-de/-/blob/master/USERGUIDE.md#manually-copying-game-media-files).

## Display names

Set these options in `--profile` YAML; defaults are:

```yaml
titles:
  mode: regional
  labels: keep
  variants: auto
```

| Setting | Values | Behavior |
| --- | --- | --- |
| `mode` | `regional` (default) | Edition-local title; strip recognized trailing region/language/revision groups, retain meaningful parentheses. |
| `mode` | `release` | Full authority name or release key; MAME uses the full human set description. |
| `mode` | `filename` | Input launch filename without extension. |
| `labels` | `keep` (default), `drop` | Preserve/remove balanced `[...]` blocks without interpreting contents. |
| `variants` | `auto` (default) | For several inputs of one game, add differing regions; then revision/language for remaining equal names; then filename/relative path for unresolved collisions. |
| `variants` | `none` | No automatic disambiguation; duplicate names allowed. |
| `variants` | `release`, `filename` | Use full release names or filename stems for all members of multi-input game groups. |

One `Probotector (Europe) (En,Fr).nes` becomes `Probotector`. Select Contra/Gryzor
editions from the same card too, and names become `Contra (USA)`,
`Probotector (Europe)`, `Gryzor (Japan)`. `[T+Rus]` remains unchanged. For literal
filename names everywhere, use `mode: filename`, `variants: none`.

Regional mode honors explicit release titles unless they equal the legacy DAT
base; then it reconstructs from the full entry to retain meaningful parentheses.
MAME uses human set descriptions. Unknown editions, including game-only matches,
keep input names instead of borrowing the default edition's title.

Input labels retain bytes, order and repetitions. Missing member/release labels
are appended; nested brackets form one block and unmatched brackets stay text.
Labels establish no translation/hack/language identity.

Disambiguation covers one system's visible selection after `--match`/`--limit`,
including all API batches. Unselected editions and hidden playlist members do not
count. Directory `.m3u` is one entry without the first disc number; individual
discs retain numbers. Local, `--api` and `--all` share this policy; use matching
client/server builds. Display changes leave paths, binding and media unchanged.
Preview/manifest show names; XML escapes `<name>`. Arbitrary templates are unsupported.

## Selection profile

Start with [the library profile](../examples/export-es-de-library.yaml) or
[smaller example](../examples/export-n64-es-de.yaml); pass `--profile <yaml>`.
Schema 1 rejects unknown keys. Relative paths use the working directory. Defaults:
English descriptions and `priority.metadata/artwork/video` from `config/sources.yaml`,
or conventional priorities without config. Secrets are not read.

Descriptions compare language/source or source/language. Language-first checks
all providers per language. Default `scope: game` permits prose from the same
recognized card; ties prefer exact release, unscoped data, then sibling release.
Manifest records donor/source/language and `selection=game`. Use `scope: release`
for edition-specific prose. Other fields (`release_date`, developer, publisher,
genres, players) remain release-scoped with per-field source lists. Explicit local
fields win. Names use the [titles policy](#display-names).

Media rules select existing files by region/source/format and optional language;
lists replace inherited lists. Default regions: matched release, world,
unspecified. `*` permits any region. `scope: release` uses bound-release/shared
files; `scope: game` also allows registered siblings. Arbitrary nested paths and
`media/_fold` are excluded. Exact/shared/sibling preference precedes other
criteria unless `order` includes `scope`.

`region` ranks affinity, leaving multiple `$release` regions tied. Optional
`region-preference` after `region` breaks that tie using countries listed in
`regions`; other tiers get neutral rank zero. For example,
`[region, region-preference, source, format]` with
`['$release', usa, japan, europe, '*']` prefers USA over Brazil within a
USA/Europe/Brazil release, but Japan for Japan-only. This is explicit opt-in,
as is [flat-front style](cover-refresh.md).

Optional `variants: [original, unknown, reconstructed, fanart]` plus a `variant`
order criterion ranks explicit filename labels before format, e.g.
`box-front-01.usa.variant-original.launchbox.jpg`. Unqualified files are unknown.
No checksum-to-provider or pixel inference occurs; categories do not guarantee
flat/case-free artwork.

New downloads retain source categories in filenames. Upgrade existing names offline:

```powershell
rom-meta catalog media-names --data-root '<library>' --source launchbox --platform psx --details
rom-meta catalog media-names --data-root '<library>' --source launchbox --platform psx --details --apply
```

`--platform` accepts an exact directory or unique alias; omit it for all, or use
`--match` for a game-folder substring. Unknown/ambiguous filters fail. Preview is
default. Apply replays saved candidate order/layout and adds variant qualifiers;
uncategorized files and conflicting destinations stay unchanged. Repeating is a
no-op. It cannot authenticate manually changed files or recover pre-reimport
candidate order. Retain `--details` as a reverse map, update exact-path overrides
and run while other catalog/media writers are idle.

Source-free curated files are local; video accepts canonical gameplay-video and
legacy names. An omitted media map inherits cover/screenshot/logo/video;
`media: {}` disables all; explicit maps enable listed roles only. No transforms
or downloads occur. Escaping paths/output collisions fail. `_fold`, per-ROM
identity decisions, ratings, partial-date normalization and extra readers/templates
remain unsupported.

## Reviewed descriptions

Default game-scope selection needs no per-game mapping. Under strict
`scope: release`, an optional reviewed donor can fill a missing description:

~~~yaml
description_fallbacks:
  - game: platforms/Nintendo - Nintendo 64/Example
    release: Example (USA) (Rev 1)
    from_release: Example (USA)
~~~

The donor must be a named release in the same card; language/source priorities
still apply. Existing descriptions, game-only bindings, other fields and identity
stay unchanged. Duplicate choices, stale applicable donors or missing eligible
text fail. Manifest records `selection=reviewed-fallback`, `from_release`, source
and language. Review the prose itself: a correct provider binding can still have
a wrong overview.

## Reviewed artwork files

Regions control eligibility and priority. To allow other regional art after the
bound release, use `regions: ['$release', world, unspecified, usa, japan, europe, '*']`
for that role; defaults remain strict.

An exact-release pin may select another release's file in the same card:

~~~yaml
media_overrides:
  - game: platforms/Nintendo - Nintendo 64/Example
    release: Example (USA)
    role: cover
    path: media/Example (Europe)/box-front.europe.launchbox.png
~~~

Paths are game-relative. Pins override that role's scope/priority, not ROM identity.
Missing, empty, external, symlinked or unsupported files fail. Other releases and
game-only bindings ignore pins. Manifest records `selection=override`, original
path/provider labels and copied SHA-256. Review artwork and regional aliases;
automatic same-card borrowing instead uses `scope: game`.
