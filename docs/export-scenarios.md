# Export scenarios

A prepared local catalog is the common source. Acquiring that catalog is separate
from generating frontend output. This matrix describes supported inputs, generated output and identity rules.

## User paths

| Scenario | Entry point | Output / evidence | Status |
| --- | --- | --- | --- |
| One existing ROM system directory | `export --target es-de --platform gamegear --destination <ES-DE-data>` | Add games and fill empty native metadata/media with backups; optional `--out` package | Implemented, experimental frontend projection |
| Several ES-DE system folders | `export --all` or `--platforms n64,snes,psx,arcade` | One shared destination plan for selected systems; optional portable package; skipped systems reported | Implemented |
| ES-DE files with a remote catalog | `export --api <origin>` with `--platform`, `--platforms` or `--all` | Same readers, evidence and package as local mode; client hashes ROMs, server supplies selected metadata/media | Implemented; no ROM upload/catalog share |
| LaunchBox with a local or remote catalog | [export --target launchbox](launchbox-export.md), single or batch | One card per catalog game, native Additional Versions, primary preferences and edition-specific M3Us | Implemented through the same `export --destination` interface; optional portable package; configure emulators separately |
| Reuse source/language/region/art preferences | `--profile` | Deterministic selected metadata/media with provenance; no provider network calls | Implemented |
| Enrich already scanned RomM games | [Server importer](../examples/romm-importer/README.md) `plan`, `apply`, `check` | Native fields/provider slots and supported Resources; generated parts/receipts; fill-only default | Experimental, pinned RomM 5.4.1 runtime with native DB access |
| Integrate a different consumer | `serve` `/v1/*` and `/docs` | Metadata/proof/asset API; consumer must implement its own mapping | API implemented; custom consumer mapping required |

The RomM server importer needs native runtime/DB handlers and Resources access.
A website login or stock edit-API token alone does not configure this example.

## Generic ROM-set coverage

Both built-in targets default to [common ROM-set observation](romset-export.md)
for catalog platforms, including new headers. IBM PC/ScummVM remain excluded.
Raw/single-payload ZIP proofs, opaque container/set names and installed-directory
stat snapshots keep distinct confidence. Native emulator setup remains separate.

The table below describes established specialized readers, not a platform allowlist.

## ES-DE input coverage

Run `rom-meta export --list-systems` for the compiled list; aliases are not extra
readers. Detailed rules and installation are in [offline export](offline-export.md).

| Family / ES-DE IDs | Accepted launch input | Identity boundary |
| --- | --- | --- |
| Nine cartridge families: n64, nes, snes, gb, gbc, gba, gamegear, mastersystem, megadrive/genesis | Supported raw extensions; ZIP with exactly one supported payload, up to 512 MiB | All known hashes must agree; weaker exact normalized release/game names are reported separately; no byte-order/header conversion |
| 3do, psx, ps2, dreamcast, saturn, segacd, pcenginecd | Standalone CHD or ISO | CHD header evidence is not Redump/whole-container integrity; matching representation hashes or names have distinct confidence |
| psx, saturn, dreamcast | `<Game>.m3u/` directory with same-name inner playlist and relative CHD/ISO files | Every member must resolve uniquely to one catalog card; first member supplies metadata; members rechecked |
| ps3 | ISO; small `.ps3` serial descriptor | Descriptor needs explicit unique catalog title; no PSN serial authority or disc proof |
| xbox / xbox360 | ISO; Xbox also XISO | No decryption/conversion, firmware or emulator readiness check |
| arcade / mame | ZIP or 7z short set names | Exact saved MAME snapshot binding; no component, parent/BIOS/sample/CHD completeness or input-version proof |

Disc archives, CUE/GDI and other containers may be retained as generic catalog
references; this does not validate dependencies or make source sets ready to launch. Unmatched/ambiguous files stay in reports. ES-DE export does not copy or
rename source ROMs, perform 1G1R filtering or configure an emulator.

## RomM example coverage

Exact raw/single-payload-ZIP cartridge recognition covers the nine API reader
families above. Catalog mode also permits typed CHD v5 single games and ordinary
same-edition multidisc folders (2..32 numbered discs), with limited header/size/
mtime observations. These provide **unverified game metadata**, not a DAT match.
Other regular files/folders use complete unique catalog release names with stat
observations, including RVZ/WUX, installed games, MAME sets and other cartridge
archives. Regions/revisions/labels are retained; this is not payload verification.
MAME additionally requires current source authority/snapshot evidence. Missing
catalog systems and unknown/ambiguous names remain explicit skips. These metadata
capabilities do not extend the ES-DE launch-reader table.

The example fills supported blank metadata/provider slots, covers, shared artwork,
screenshots, video and actual manuals where available. Native ownership, locks,
existing media and library/user bindings are protected; unsupported roles and
alternate descriptions remain provenance. Normal import is fill-only. Explicit
cover refresh and identity repair are separate reviewed operations. There is no
whole-collection file ceiling; generated parts bound lookup/apply work. See the
[operator guide](romm-import-guide.md) for repeat checks and partial-failure handling.

## Automated tests

| Area | Coverage |
| --- | --- |
| ES-DE recognition and selection | [Exporter tests](https://github.com/divanikus/rom-meta/tree/main/internal/exporter/): hash/name ambiguity, raw/ZIP, CHD/ISO, playlists, modern inputs, exact MAME snapshot, profiles, provenance, unchanged ROMs, unsafe paths, stale sources and output collisions |
| Network ES-DE parity | [Client tests](https://github.com/divanikus/rom-meta/tree/main/internal/exportclient/): every local reader, raw/ZIP, packed storage, profile bytes, playlists, serials, MAME, >1,000 inputs, asset deduplication, drift/cancellation/auth-origin rejection |
| Batch publication | `batch_test.go`: cartridge/disc/arcade mixture, skips and rejection without partial output |
| Loose/packed catalog equivalence | `packed_test.go`: same export values and media for supported source packs |
| Human CLI loop | [Operator workflow test](https://github.com/divanikus/rom-meta/blob/main/cmd/rom-meta/operator_workflow_test.go): actual import/fetch-resume/interactive-fold/export with synthetic DAT/provider data |
| RomM mapping and importer | [Shared projection tests](https://github.com/divanikus/rom-meta/tree/main/internal/romm/) and [example tests](https://github.com/divanikus/rom-meta/tree/main/examples/romm-importer/): fake-native preservation, batch >1000 inputs, proof/preflight, stale/partial/no-op, cover normalization/refresh, CHD observations and media receipts |

The tests use synthetic fixtures to check recognition, projection, transport and
preservation rules. They do not validate emulator compatibility or gameplay.
New RomM versions need runtime/schema review before use. Other frontend targets,
arbitrary templates and broader disc/arcade proof remain unsupported.
