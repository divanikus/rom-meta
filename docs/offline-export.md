# ES-DE export

Generate a separate local/personal package from existing ROMs and the prepared
catalog. Local mode works offline without providers or credentials; network mode
uses the same catalog through `serve`. Source ROMs/catalogs remain unchanged.
Local mode needs filesystem access to catalog/media; `--api` needs only a server
connection. Actual ES-DE/emulator consumer compatibility remains experimental.

## Quick start: existing ES-DE ROMs

The normal workflow enriches the ROM selection already on disk. Prepare/import/
fetch the catalog once, then export works offline with local catalog/media reads.
No running `serve` or provider credentials are required. The user provides existing
ROM files; RomMeta generates their metadata package without selecting/copying ROMs.

### 1. Identify the inputs and frontend paths

Use a catalog containing the matching platform and its downloaded metadata/media.
Find the actual system directory ES-DE launches, for example `/games/roms/nes`.
Also identify ES-DE's home/gamelists location and its configured downloaded-media
root; these can be on different disks. A custom system path can differ from the
ordinary immediate system folder. Export the actual directory for that system.

Start with a small system folder. Check the supported formats and identity rules
in the [input format matrix](export-scenarios.md#es-de-input-coverage).

### 2. Preview one system

Run with quoted real paths. The output directory must be new and outside both
catalog and ROM trees. The optional `--limit 10` below is a deliberate sample:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --out "/exports/nes-check" --limit 10
```

Review hash/name/game-only matches, ambiguous/unmatched/unsupported inputs,
description/cover coverage and selected media bytes. This command prints a
preview and creates no package. It selects metadata for existing ROMs; it does
not choose a best-games collection or filter regions into 1G1R.

### 3. Create and inspect a package

Repeat that selection with `--apply`:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --out "/exports/nes-check" --limit 10 --apply
```

Open `/exports/nes-check/preview.html` after creation. Inspect `report.txt`,
`manifest.json`, `gamelists/nes/gamelist.xml` and the selected media. The manifest
records recognition confidence and source/language/media evidence. The package's
README.txt explains installation. An HTML preview verifies generated output, not
how ES-DE will resolve/display it. Input ROMs and catalog remain unchanged.

To process the whole system, omit `--limit` and use another new output directory.
For explicit source/language/region priorities, copy
[the reusable profile](../examples/export-es-de-library.yaml), edit it once and
pass `--profile "/path/to/profile.yaml"` on preview and apply. No provider network
fallback fills missing local metadata/media during export.

### 4. Install into ES-DE

Close ES-DE so it does not save an older gamelist over the installed result.
For a system without an existing gamelist, back up existing frontend/media files
and copy these reviewed paths:

| Package path | Frontend destination |
| --- | --- |
| `gamelists/nes/gamelist.xml` | `<ES-DE-home>/gamelists/nes/gamelist.xml` |
| Contents of `downloaded_media/nes/` | `<configured-media-root>/nes/` |

Keep the ROM filenames/subdirectories referenced by `./` launch paths. The selected
ROM directory must actually be the one ES-DE uses for NES. Modern ES-DE uses its
configured gamelists/media locations; dropping XML next to ROMs requires a separate
legacy configuration. Check paths in the real frontend after reloading it.

If a gamelist already exists, merge entries by launch path to preserve favorites,
playcounts, emulator overrides and unrelated entries. Replacing the whole XML
loses those values. The CLI does not perform this merge/install. Use your existing frontend metadata
management process for that step; this release provides package generation.

### 5. Export several systems

Use the parent directory with immediate system folders (`nes/`, `snes/`,
`megadrive/`, etc.), rather than a single system directory:

```shell
rom-meta export --target es-de --platforms nes,snes,megadrive --data-root "/path/to/catalog" --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/three-systems"
```

Review, then repeat with `--apply`. `--all` discovers supported immediate folders;
`--platforms` selects named ones. Ordinary `--platform` accepts one actual system
ROM directory. An installation with a custom nested Xbox 360 root needs that
actual path in a separate single-system export; batch mode does not infer it.

## Export from a remote catalog

Run `serve` on the machine holding the prepared catalog. Bind its LAN address;
the default loopback address is accessible only on that machine:

```shell
rom-meta serve --data-root "/catalog" --addr "192.168.1.10:8787"
```

Run the client on the machine that owns the actual ES-DE ROM files:

```shell
rom-meta export --api "http://192.168.1.10:8787" --platform nes --rom-root "/games/roms/nes" --out "/exports/nes-package"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/all-package"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --profile "/path/to/profile.yaml" --out "/exports/all-package" --apply
```

Use `--platforms nes,gba,psx,mame` for selected immediate system folders. `--api`
and `--data-root` are mutually exclusive. `--list-systems` lists compiled client
readers; the actual server catalog determines availability. Custom frontend paths
still need their actual directory via a separate `--platform` export.

The client enumerates and hashes ROMs locally, sends observations in batches of
100 launches and receives the same projection as local export. It does not upload
ROM bytes. The server needs no ROM mount. It resolves one selected catalog index
at a time and hashes selected media; preview may take time but downloads no media
to the client and creates no output. Progress distinguishes ROM observation, API
resolution, download and package writing. Large CHD/ISO collections require full
local container reads, unlike the limited CHD observations used by the RomM example.

An optional `--profile` is read on the client and sent as YAML policy bytes; its
filename is never opened on the server. Otherwise the server's default/profile
applies. `ROMMETA_API_TOKEN` is optional; when server protection is configured,
set the same token in the client environment. API origins must have no embedded
credentials, path or query; redirects are rejected. See [API access](catalog-api.md).

All local readers are available remotely, including PS3 descriptors, directory
playlists and MAME ZIP/7z set-name binding. `--arcade-snapshot` selects saved server
MAME bindings; an absent/ambiguous snapshot is reported as a batch skip and a
single-system error. These are the same confidence limits as local export.

Apply downloads only selected media, checks size/SHA-256, rechecks local ROMs and
stages one new package. Catalog/profile revision changes across requests, source
drift, download failures or cancellation abort publication. There is no total
collection ceiling; per-request limits do not truncate a collection. A failed run
can be repeated with the same unused output path. Successful output must use a new
path on the next run. Interrupted temporary download work is not a persistent
resume cache. Installation/merge is still the separate step above.

## Export an ES-DE ROM root

For a directory containing system folders such as n64/, snes/, psx/ and arcade/:

```shell
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory>
rom-meta export --target es-de --data-root <catalog-root> --rom-root <ES-DE-ROM-root> --all --profile <profile.yaml> --out <new-output-directory> --apply
```

Copy [the reusable profile](../examples/export-es-de-library.yaml) once and reuse
it for different ROM roots. It has no game/release exceptions. Acquisition is
preparation; this command reads only the local catalog and assets. Summary/report
coverage identifies remaining source/identity gaps.

Use `--platforms n64,snes,psx,arcade` instead of `--all` to select folders.
For one system, retain `--platform n64 --rom-root <n64-system-directory>`.
These modes are mutually exclusive. `--list-systems` lists available readers.

The first command plans without writing; `--apply` stages all selected systems
together and publishes one new package after success. A later system failure
leaves no partial final package. Output must be new and outside both input trees.
`--limit` limits inputs **per system**; `--match` filters filenames relative to each
system directory. No 1G1R or automatic region/revision filtering is applied.
One shared `--profile` supplies selection preferences to all systems.

Automatic discovery examines immediate non-hidden directories. Unknown systems,
missing/duplicate catalog platforms, missing folders, empty selections and missing
or ambiguous MAME snapshot selection are reported as skipped. Malformed selected
catalog/profile data and I/O failures stop the batch. Skipped systems generate no
files. No package is written if every system is skipped. System-directory links
escaping the selected root are skipped; individual ROM/media symlinks are not used.
Actual catalog folders are discovered through _platform.yaml identity rather than
assumed from an alias/display name.

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

Cartridge ZIPs with exactly one supported platform payload are streamed with a
512 MiB payload limit. Raw cartridges have the same limit. Standalone disc and
arcade container files are streamed without that cartridge limit. Byte-order/header
conversion is not implemented. Disc ZIP/7z/RAR, CUE/GDI and loose M3U descriptors
are unsupported; component tracks are not standalone launches. Loose CHD/ISO
discs remain separate. Directory-style M3U for PSX/Saturn/Dreamcast validates
relative regular members within its game directory, requires unique member-release
bindings to one catalog card and records/revalidates every member. Metadata/media
explicitly uses the first member; no catalog edition is invented. PS3 serial
content is not a disc fingerprint or PSN authority. Image decryption, XISO
conversion, firmware checks and emulator integrity checks are not performed.
See the official
[ES-DE Windows system configuration](https://gitlab.com/es-de/emulationstation-de/-/blob/master/resources/systems/windows/es_systems.xml)
for frontend system names and its wider emulator-dependent formats.

## Recognition

Ordinary inputs use size and SHA1/SHA256 before normalized authority/release/file
names, then explicit catalog titles/aliases. All stored fingerprint hashes must
agree. Case, spaces and punctuation may vary; qualifier contents remain.
Multiple candidates and conflicting archive/member names stay ambiguous.
There is no fuzzy/provider-title identity inference.

`matched_by: hash` records byte identity for the stated representation; `name`
is weaker release-name evidence; `game-name` leaves exact release unknown.
`metadata_release` records the catalog default used for game-only display data,
with shared release policy supplying a missing default. Game-only media stays
game-wide. Ordinary unknown/ambiguous inputs retain filename-only entries.

CHD records explicitly use `representation: chd-container`. Container-byte hashes
can match only fingerprints whose recorded filename is CHD; raw-disc/Redump track
hashes cannot identify that container. Without such a fingerprint, use name
fallback. Embedded CHD digests are not used as raw-track fingerprints, and no
decompression or CHD integrity check is performed. See
[the MAME CHD tool reference](https://docs.mamedev.org/tools/chdman.html).

Arcade is a separate path: read durable
`bindings/romsets/mame/<snapshot>/catalog.yaml` and match exact archive short names.
Auto-select only when one saved snapshot exists; otherwise supply
`--arcade-snapshot <id>`. Validate the referenced card/release authority and its
catalog MAME snapshot. Record `arcade-set-name` and `catalog_snapshot`, never a
verified payload/version match. Generic title guesses are disabled; unbound or
ambiguous arcade archives stay in reports and are omitted from the gamelist.
ROM members, input MAME version, merged/split completeness and parent/BIOS/CHD/
sample dependencies are not checked. Existing dependencies/ROMs are left in place.
There is no FBNeo binding transfer or implemented FBNeo exporter.

## Package and installation

The common output contains gamelists/<system>/gamelist.xml and
downloaded_media/<system>/ media named after original ROM basenames/subdirectories.
Batch root preview.html, manifest.json and report.txt summarize all systems and
skips. Detailed per-system preview/manifest/report/installation notes live under
reports/<system>/; previews use local relative media links. Single-system exports
retain their original root sidecar layout.

ROMs are not copied or renamed. Existing ES-DE system folders stay usable with
the generated relative ./ launch paths. Review the package, preserve copies of
existing frontend metadata, then manually install gamelists and media in ES-DE's
configured locations. The exporter never writes an installed frontend or its
settings. Source launch files are rehashed before publication; copied assets carry
SHA256 digests. Keep all private paths/reports and licensed source media outside Git.
Consult the official
[gamelist reference](https://gitlab.com/es-de/emulationstation-de/-/blob/master/INSTALL.md#gamelistxml)
and [media naming guide](https://gitlab.com/es-de/emulationstation-de/-/blob/master/USERGUIDE.md#manually-copying-game-media-files).

## Selection profile

Start with [the reusable library profile](../examples/export-es-de-library.yaml)
or [the smaller example](../examples/export-n64-es-de.yaml); pass `--profile <yaml>`.
Schema 1 rejects unknown keys. Relative profile paths use the working directory.
Defaults use English descriptions and source priority.metadata/artwork/video from
config/sources.yaml; absent config uses conventional priorities. Secrets are not read.

Description criterion order is language/source or source/language. Language-first
compares every allowed provider at each language before falling back. Descriptions
default to scope: game: provider prose in the same already recognized card can
describe another regional/revision ROM. Priority ties prefer exact release,
unscoped game data, then sibling-release data. The manifest retains donor/source/
language and selection=game where applicable. Use scope: release for strict
edition-specific prose. This does not change identity or binding confidence.

Rules for release_date, developer, publisher, genres and players remain
release-scoped and accept per-field source lists. Explicit local catalog fields
win. title_mode is catalog or release.

Media rules select existing files by region/source/format and optional language.
Lists replace inherited lists. Defaults prefer matched-release regions, world,
then unspecified; * explicitly permits any-region fallback. Media defaults to
scope: release (bound-release folder and shared files). scope: game also permits
registered sibling-release folders of that same card. Arbitrary nested paths and
media/_fold are excluded. Exact/shared/sibling folder preference precedes other
criteria unless order explicitly includes scope.

The `region` criterion ranks affinity only: multiple matching `$release` regions
remain tied. Optional `region-preference`, placed after `region` in `order`, uses
the explicit countries already listed in `regions` to break ties within that tier.
Other tiers get neutral rank zero. This is independent of style and never injected
into old rules. For example, `[region, region-preference, source, format]` with
`regions: ['$release', usa, japan, europe, '*']` prefers USA over Brazil for a
USA/Europe/Brazil release while a Japan-only release still prefers Japan.

Flat-front style is also explicit opt-in; see [cover selection](cover-refresh.md).
Default media and the general bundled profiles keep their previous behavior.

Optional variants: [original, unknown, reconstructed, fanart] and a variant order
criterion select the explicit filename qualifier before file format, for example
box-front-01.usa.variant-original.launchbox.jpg. Export reads that label directly;
it does not match artwork checksums to provider records or inspect image pixels.
Unqualified legacy files remain unknown and eligible when unknown is listed.
Region/language/source priorities keep their usual meaning. Provider categories
do not guarantee a flat or case-free visual style.

New downloads preserve explicit source categories in filenames. To upgrade
already downloaded files without network access:

```powershell
rom-meta catalog media-names --data-root '<library>' --source launchbox --platform psx --details
rom-meta catalog media-names --data-root '<library>' --source launchbox --platform psx --details --apply
```

Omit --platform for the whole library, or use --match for a game folder substring.
An exact existing platform directory wins over registry aliases. Short aliases
resolve against actual catalog directories; unknown or ambiguous filters fail
explicitly rather than silently scanning an absent default directory.
The default is a dry run. This replays the saved source candidate order and
historical downloader layout, adding only the variant qualifier. Files without
an explicit source category stay unchanged; existing destinations/conflicts
are skipped without overwriting. Repeat application is a no-op. It cannot
authenticate files manually replaced or moved since download, or recover an
earlier candidate order after source reimport; leave those files unqualified
until their provenance is known. Retain --details output as the reverse rename
map and update optional exact-path media overrides if you use them. Run the
upgrade while imports, downloads and manual media edits are idle.

Curated source-free files are local. The video role
recognizes canonical gameplay-video filenames and legacy video names.

An omitted media map inherits cover/screenshot/logo/video; media: {} disables all.
An explicit map enables only listed roles. No transforms/downloads are performed.
Paths escaping inputs and output collisions are rejected. media/_fold, per-ROM
identity decisions, ratings, partial-date normalization, custom output
templates, further systems/readers and T-En/variant identity remain follow-ups.

## Reviewed descriptions

Ordinary exports use general game-scope description selection and need no
per-game mapping. For an intentionally strict scope: release policy, an optional
advanced exception can reuse a reviewed donor release:

~~~yaml
description_fallbacks:
  - game: platforms/Nintendo - Nintendo 64/Example
    release: Example (USA) (Rev 1)
    from_release: Example (USA)
~~~

Only a missing description is filled from that same card's named release.
Language/source priority still applies there. Other fields and ROM identity stay
unchanged; existing descriptions and game-only bindings do not use the choice.
Duplicate choices fail. An applicable missing donor release or missing eligible
text fails instead of silently hiding a stale decision. The manifest records
selection=reviewed-fallback, from_release, source and language.

Check content as well as eligibility. A correctly imported provider record can
contain a wrong overview. Provider priority and library curation address that
separately from regional display coverage.

## Reviewed artwork files

Region lists are eligibility lists as well as priorities. To prefer the bound
release and then allow other existing regional art, explicitly use
regions: ['$release', world, unspecified, usa, japan, europe, '*'] for each media
role that needs that fallback. Defaults remain strict.

A profile may select a reviewed file for one exact release, including a file
stored under another release of the same card:

~~~yaml
media_overrides:
  - game: platforms/Nintendo - Nintendo 64/Example
    release: Example (USA)
    role: cover
    path: media/Example (Europe)/box-front.europe.launchbox.png
~~~

The path is relative to the selected catalog game. A pin overrides automatic
scope/priority for that role; it does not bind ROMs or invent release identity.
Missing, empty, external, symlinked or unsupported files fail. Other releases and
game-only bindings do not consume the pin. The manifest records selection=override,
the original source path/provider labels and the copied SHA256. Review regional
aliases and actual artwork before using a pin. Automatic same-card borrowing is
controlled by scope: game; it does not require pins. See ADRs 0076 and 0078.
