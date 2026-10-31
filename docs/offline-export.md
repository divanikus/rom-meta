# ES-DE export

Generate gamelists, selected media and reports for existing ROMs. Local mode reads
a prepared catalog offline; `--api` uses that catalog through `serve`. Neither
mode modifies ROMs/catalogs. ES-DE/emulator integration remains experimental.

For native LaunchBox XML/media, use the [LaunchBox exporter](launchbox-export.md).

## Quick start: existing ES-DE ROMs

Use the [shared export/enrichment interface](frontend-export.md). Prepare the
catalog once, then point at the actual system ROM directory and ES-DE application
data/home directory:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --destination "/home/player/ES-DE"
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --destination "/home/player/ES-DE" --apply
```

Review recognition, coverage and `add`/`enrich`/`unchanged`/`conflict` actions.
Preview creates no output and copies/downloads no media. Close ES-DE before apply.
New entries are added; empty metadata and missing media are filled. Existing
nonempty fields, favorites, play counts, hidden flags, emulator overrides,
folder entries, unknown fields and unrelated entries remain. The native
`alternativeEmulator` sibling beside `gameList` is retained. Replaced files are
backed up under `<destination>/Backups/RomMeta`; unchanged repeats write nothing.
ROMs and catalog files stay unchanged.

`--destination` identifies ES-DE's data directory, normally containing `settings`,
`gamelists` and `downloaded_media`. The media path is read from modern
`settings/es_settings.xml` or legacy `es_settings.xml`; absent/empty MediaDirectory
uses `<destination>/downloaded_media`. Use `--media-root <directory>` for an
explicit external media location or unresolved media-path variables. Settings
are not changed. ROMDirectory and selected custom-system paths, when configured,
must agree with `--rom-root`; unresolved ROM path variables and legacy gamelists
beside ROMs are rejected. See the [official directory/settings guide](https://gitlab.com/es-de/emulationstation-de/-/blob/master/USERGUIDE.md#installation-and-first-startup).

Use `--match` or `--limit` for a bounded sample; omit them for a full system.
Copy [the profile](../examples/export-es-de-library.yaml) for metadata/media
priorities. Profiles do not fetch missing provider content.

For multiple immediate system folders use the same destination:

```shell
rom-meta export --target es-de --platforms nes,snes,megadrive --data-root "/path/to/catalog" --rom-root "/games/roms" --destination "/home/player/ES-DE" --apply
```

`--all` discovers supported immediate folders. Custom nested ROM paths need a
separate `--platform` invocation, using the same export/enrichment workflow.
Related disc/digital catalogs are searched together for one hardware system,
including API mode. Ambiguities and unsupported readers remain explicit.

### Optional package review or transport

To produce a portable package instead, replace `--destination` with
`--out <new-directory>`. Apply creates `preview.html`, reports, a manifest,
`gamelists/<system>/gamelist.xml` and selected `downloaded_media/<system>/` files.
Existing outputs are never overwritten. To install it, use the same command:

```shell
rom-meta export --target es-de --package "/exports/nes" --destination "/home/player/ES-DE"
rom-meta export --target es-de --package "/exports/nes" --destination "/home/player/ES-DE" --apply
```

Do not replace an existing gamelist with a package XML by copying; native state
is preserved by the same destination adapter as direct export. Source ROM paths
must still correspond to the configured frontend system paths.

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
rom-meta export --api "http://192.168.1.10:8787" --platform nes --rom-root "/games/roms/nes" --destination "/home/player/ES-DE"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --destination "/home/player/ES-DE"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --destination "/home/player/ES-DE" --apply
```

`--platforms nes,gba,psx,mame` selects immediate folders by code, full name or alias.
`--api` excludes `--data-root`. `--list-platforms --api <origin>` lists client reader
support and server catalog availability; server catalog contents
determine availability. Custom roots still need separate `--platform` exports.

The client observes ROMs locally and sends batches of 100 observations, never ROM
bytes. The server needs no ROM mount. It loads one selected index at a time and
hashes selected media. Preview creates no output or client media downloads.
Progress separates observation, optional hash fallback, API resolution, download
and package writing. CHD v5 uses bounded header/catalog recognition when the API
advertises `chd-header-v1`; older servers keep full hashing. ISO reads full containers.
See [CHD recognition and explicit checks](frontend-export.md#chd-recognition-and-full-file-checks).

`--profile` sends client YAML bytes, not a server filename; otherwise server
defaults apply. If protection is enabled, set `ROMMETA_API_TOKEN` to the server's
token. Origins cannot contain credentials, paths or queries; redirects fail.
See [API access](catalog-api.md#optional-access-protection).

Remote readers match local ones, including PS3 descriptors, directory playlists
and MAME ZIP/7z. `--arcade-snapshot` selects saved server bindings; unresolved
snapshot selection skips a batch system or fails a single-system export.

For `--destination`, apply validates the native plan before transfers, downloads
selected assets, verifies size/SHA-256 and ROM freshness, then merges validated
output into the frontend with backups. Preparation is internal and temporary.
An unchanged destination apply skips asset transfers. `--out` remains available
for a separate portable package and still requires a new output path. Temporary
downloads are not a persistent resume cache; failed preparation leaves the
installed metadata unchanged.

## Export an ES-DE ROM root

For immediate system folders (`n64/`, `snes/`, `psx/`, `arcade/`):

```shell
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory>
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory> --apply
```

Use one [shared profile](../examples/export-es-de-library.yaml) for all systems.
Choose exactly one of `--all`, `--platforms n64,snes,psx,arcade` or single-system
`--platform n64` with its actual system directory. `--list-platforms` shows codes,
full names, accepted aliases, export support and catalog availability. Both selection
flags accept the same values; see [folder recognition](platform-selection.md).
`--limit` applies **per system**; `--match` filters relative filenames. No 1G1R or
automatic region/revision filtering occurs.

Preview writes nothing. Apply stages every selected system before publishing one
package; later failure leaves no partial final output. Output must be new and
outside both input trees.

Discovery matches immediate visible ROM directory names against the shared registry,
including long canonical names. Catalog `_platform.yaml` headers locate metadata.
Unknown/missing/duplicate platforms, absent folders, empty selections and unresolved
MAME snapshots are reported as skips. Malformed selected data/profile or I/O errors
stop the batch. Skips generate no files; an all-skipped batch creates no package.
Escaping system-directory links are skipped; individual ROM/media symlinks are not used.

## Supported launch readers

Catalog platforms also use the [generic ROM-set observer](romset-export.md). The
following table describes established specialized input profiles and is not an
allowlist. IBM PC/ScummVM remain excluded from export.


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

CHD uses `representation: chd-container`. Normal v5 observation carries `disc`
header/stat evidence without `payload` or whole-file `sha1`. Prepared translations
can match declared combined SHA1 (`variant-chd-header`); other files use catalog
name evidence. A possible same-size CHD-container fingerprint triggers selective
full hashing, retaining stronger matches and identity conflicts. Container hashes
can match only recorded CHD fingerprints, never raw-disc/Redump track hashes.
Embedded digests are not raw-track proof; no decompression/integrity check occurs.
Use `--hash-roms` for explicit full-container inspection. Size/mtime/header checks
assume immutable ROMs; equal-size payload edits preserving mtime/header are detected
only in full-hash mode.
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
before publication and copied assets carry SHA-256. Install through the
[apply through the shared destination adapter](frontend-export.md) in ES-DE's configured locations.
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
The browser's Artwork preference writes these ordinary rules. Originals first
uses `order: [region, variant, source, format]` and
`variants: [original, unknown, reconstructed, fanart]`; Reconstructions first uses
`[reconstructed, original, unknown, fanart]`. Region remains the first criterion.
Auto leaves the existing policy unchanged. Numeric filename suffixes break ties
after the configured criteria, then the filename breaks remaining ties.

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
