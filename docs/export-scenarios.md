# Export scenarios

A prepared local catalog is the common source. Acquiring that catalog is separate
from generating frontend output. This matrix describes supported inputs, generated output and identity rules.

## User paths

| Scenario | Entry point | Output / evidence | Status in 0.1.0 |
| --- | --- | --- | --- |
| One existing ROM system directory | `export --target es-de --platform gamegear` | New gamelist/media package; hash/name/game confidence and omissions in manifest | Implemented, experimental frontend projection |
| Several ES-DE system folders | `export --all` or `--platforms n64,snes,psx,arcade` | One staged multi-system package; skipped systems reported; no partial final output on failure | Implemented |
| ES-DE files with a remote catalog | `export --api <origin>` with `--platform`, `--platforms` or `--all` | Same readers, evidence and package as local mode; client hashes ROMs, server supplies selected metadata/media | Implemented; no ROM upload/catalog share |
| Reuse source/language/region/art preferences | `--profile` | Deterministic selected metadata/media with provenance; no provider network calls | Implemented |
| Enrich already scanned RomM games | [Server importer](../examples/romm-importer/README.md) `plan`, `apply`, `check` | Native fields/provider slots and supported Resources; generated parts/receipts; fill-only default | Experimental, pinned RomM 5.4.0 runtime with native DB access |
| Integrate a different consumer | `serve` `/v1/*` and `/docs` | Metadata/proof/asset API; consumer must implement its own mapping | API implemented; no additional built-in frontend exporter |

The RomM server importer needs native runtime/DB handlers and Resources access.
A website login or stock edit-API token alone does not configure this example.

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

Disc archives, CUE/GDI, loose M3U and component tracks are not general launch
readers. Unmatched/ambiguous files stay in reports. ES-DE export does not copy or
rename source ROMs, perform 1G1R filtering or write an installed frontend.

## RomM example coverage

Exact raw/single-payload-ZIP cartridge recognition covers the nine API reader
families above. Catalog mode also permits typed CHD v5 single games and ordinary
same-edition multidisc folders (2..32 numbered discs), with limited header/size/
mtime observations. These provide **unverified game metadata**, not a DAT match.
Other optical formats and ambiguous/mixed disc folders are reported unsupported.

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
| ES-DE recognition and selection | [Exporter tests](../internal/exporter/): hash/name ambiguity, raw/ZIP, CHD/ISO, playlists, modern inputs, exact MAME snapshot, profiles, provenance, unchanged ROMs, unsafe paths, stale sources and output collisions |
| Network ES-DE parity | [Client tests](../internal/exportclient/): every local reader, raw/ZIP, packed storage, profile bytes, playlists, serials, MAME, >1,000 inputs, asset deduplication, drift/cancellation/auth-origin rejection |
| Batch publication | `batch_test.go`: cartridge/disc/arcade mixture, skips and rejection without partial output |
| Loose/packed catalog equivalence | `packed_test.go`: same export values and media for supported source packs |
| Human CLI loop | [Operator workflow test](../cmd/rom-meta/operator_workflow_test.go): actual import/fetch-resume/interactive-fold/export with synthetic DAT/provider data |
| RomM mapping and importer | [Shared projection tests](../internal/romm/) and [example tests](../examples/romm-importer/): fake-native preservation, batch >1000 inputs, proof/preflight, stale/partial/no-op, cover normalization/refresh, CHD observations and media receipts |

The tests use synthetic fixtures to check recognition, projection, transport and
preservation rules. They do not validate emulator compatibility or gameplay.
New RomM versions need runtime/schema review before use. Further frontend targets,
arbitrary templates and broader disc/arcade proof remain unsupported.
