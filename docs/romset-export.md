# Generic ROM-set export

Both built-in adapters export platforms from the catalog through a common ROM-set
observer. There is no platform allowlist to extend in Go. A new `_platform.yaml`
header supplies the platform identity; the registry supplies names, aliases and
preferred codes when available. Otherwise RomMeta generates a normalized code
from the canonical header. Local configuration can improve naming without enabling
a new reader. See [platform selection](platform-selection.md).

IBM PC and ScummVM are explicit exclusions from this workflow. Their metadata
catalogs remain usable. MAME retains saved snapshot/set-name binding, PS3 retains
its serial descriptors, and established input profiles preserve existing IDs and
stronger readers. These are exceptions/specializations of the common pipeline.

## Input evidence

| Representation | Common handling | Identity claim |
| --- | --- | --- |
| Ordinary binary ROM, including unfamiliar extensions | Stream hashes up to 512 MiB and search the selected catalog | Exact registered fingerprints when all checks agree; weaker names remain separate |
| ZIP with one ordinary payload up to 512 MiB | Inspect safe member names and hash the payload; ignore documentation sidecars | Payload evidence, never an inferred cross-platform match |
| Multi-file ZIP, disc/package archive, or larger ZIP payload | Keep the original archive as one set; no extraction | Complete unique catalog release/variant name plus file stat; unverified contents |
| Compressed optical images and large files, e.g. RVZ/WUX | Keep the existing file and record stat facts | Complete unique catalog name; no synthetic DAT hash |
| Installed-game layout or named multi-component directory | One input with a bounded sorted membership/stat snapshot | Complete directory/release name; no dump or dependency proof |
| Directory-style M3U with CHD/ISO members | Existing bounded relative playlist handler, shared across platforms | Every member must bind to the same game/edition; qualifiers and order survive |
| Saved MAME set | Existing snapshot-aware handler | Exact set name; no component completeness proof |

A directory identifies its hardware through the registry, never through an
extension or archive member. Format handling happens after platform selection.
Names preserve regions, revisions, language and derivative qualifiers. A prepared
translation/hack retains its own identity and inheritance. Ambiguity remains held;
no random ZIP member, base title or provider artwork proves a different edition.

Standard category directories such as retail, aftermarket and translations are
traversed. Recognized installed layouts and named multi-component sets are grouped;
ordinary standalone disc subdirectories continue to expose their discs. Metadata,
artwork, keys and firmware sidecars are excluded where their role is unambiguous.
Unmatched opaque/source sets remain in reports rather than producing speculative
native cards. Unknown or unsupported payload details remain visible in the plan.

## Exported library versus launch readiness

LaunchBox groups recognized versions into one catalog-game card. Multiple catalog
datasets with one native platform label contribute to one generated platform XML;
card identity is retained and duplicate IDs remain errors. Source archives
and unprepared installed directories are Additional Versions labelled `[source
set]`; they are not marked installed and do not inherit a parent emulator. A
normal launch representation ranks before a source set when selecting a new main
version. Descriptions explain preparation needs. Existing main launches, owner
fields, IDs, emulator choices and statistics retain the ordinary preservation rules.

ES-DE uses native folder entries for grouped source directories. The exporter
writes metadata and references existing inputs; it does not extract, install,
convert or rename ROMs, fabricate launch wrappers, or configure emulators.
ES-DE must have a native system definition for the selected code and actual ROM
path. An unfamiliar platform may need a native/custom-system definition even
though its catalog export needs no RomMeta code change. Frontend and emulator
format support is separate from catalog coverage.

Both local and API application recheck source-set membership/stat snapshots.
New/deleted/changed members and symlinks fail freshness checks. Stat evidence does
not detect edits which deliberately preserve both size and modification time.
Hash-bound inputs retain their ordinary content checks.

## Commands and API compatibility

```shell
rom-meta platforms list --data-root /catalog --rom-root /games/roms
rom-meta export --target launchbox --data-root /catalog --rom-root /games/roms --all --destination /LaunchBox
rom-meta export --target es-de --api http://catalog-host:8787 --rom-root /games/roms --all --destination /home/player/ES-DE
```

Preview is the default. Use `--limit` per platform for a bounded first inspection,
then review the full desired selection before `--apply`. Packages remain optional.

Catalog capability responses advertise optional `export_mode: romset-v1` and
export reader codes. Requests can carry `kind: catalog` with the existing bounded
`CatalogObservation` stat DTO. Responses keep `catalog-name` confidence and the
original snapshot; clients reject conversion into payload/hash evidence. This is
an additive version-1 extension. Older servers retain their advertised coverage
and existing readers; update the server to enable the generic formats/platforms.
A CLI rebuild does not redeploy a running catalog API.
