# MAME Arcade Import

The supported path is `rom-meta dat import`. It reads official MAME
listxml, obtains version-matched Progetto-SNAPS classification data, distributes
eligible games into `platforms/Arcade/<game>/` and optionally folds confirmed
revisions. Each card retains its logical platform in `arcade.platform`.

## Preview And Apply

Initialize a separate catalog root, then provide the official XML (or ZIP).
An existing catalog does not need to be initialized again. Run from its root;
`--data-root` is optional and overrides the current directory:

```shell
rom-meta init <catalog-root>
cd <catalog-root>
rom-meta dat import <mame.xml-or-zip> --report <new-plan.json>
rom-meta dat import <mame.xml-or-zip> --sets sf2,sf2ua,mslug,mslug2,kinst --report <new-apply.json> --apply
rom-meta validate .
```

MAME is detected from the selected XML root, including XML stored as a `.dat`
member inside a ZIP; filenames do not select the engine. No-Intro/Redump continue
through the ordinary importer. `dat arcade` remains a compatibility alias with
the same current-directory default and flags. The shared MAME catalog is always
`Arcade`; an unrelated `--platform` override is rejected. MAME-only options
are rejected for ordinary DATs instead of being silently ignored.

## Classification Acquisition

The DAT build selects an exact Progetto-SNAPS category version; it is never
replaced with the newest or nearest version. Acquisition order:

1. An explicit `--classifications <zip>`, if supplied.
2. A verified durable snapshot for the same input file SHA256 and MAME version.
3. The validated cache under
   `<data-root>/.rommeta/cache/arcade/mame/<version>/`.
4. Download the versioned archive over HTTPS from Progetto-SNAPS.

`--offline` disables step 4. A repeat import works offline from its durable
snapshot even after the disposable cache is removed. Conflicting saved
classification hashes require an explicit choice; a corrupt snapshot/cache
produces an error rather than silently changing the input.

The first preview may download and write support data to the disposable cache;
it does not create game cards or durable bindings. Only `--apply` changes
the catalog. Downloads have a timeout and size limit, are validated for ZIP
structure, required INIs and matching version, and are published with a SHA256
manifest only after validation. SHA256 pins downloaded bytes locally; it is not
an upstream signature. A missing archive, incompatible format or network error
leaves the catalog unchanged and explains the explicit-file alternative.

Console output and the JSON report include the actual path, version, SHA256
and acquisition origin (explicit, snapshot, cache or download); downloaded/cached
data also identifies its source URL. Nothing is embedded in the binary or bundled
in the distribution. The raw classification ZIP is still retained in applied
snapshots, without changing their content-based identity.

FBNeo has its own DAT authority and versioning. FBNeo remains guarded against apply and never enters MAME classification
acquisition; its applied binding path is not implemented. It never requests a MAME category archive using an FBNeo version.
Distribution DATs and Software Lists remain separate unsupported apply inputs.

## Selection And Folding

Report files must not exist; their parent directory must exist. The report
contains every source machine's classification and reason, selection flag,
hardware evidence, proposed card, fold review reason and selected set bindings.
`--sets` uses exact comma-separated MAME short names. Selecting an excluded,
unresolved or nonexistent set prevents apply. Grouping and folder names are computed
from all eligible source sets before selecting releases: importing a clone alone
does not change its card identity or import its unselected parent. Omitted selection
means all eligible sets, so review the full distribution before a broad apply. `--member` chooses
the XML member when a ZIP has multiple candidates.

`--fold` is enabled by default; `--no-fold` creates separate cards. Automatic
fold follows eligible MAME `cloneof` ancestry within one logical arcade platform;
regional titles and technical qualifiers need not match. Both driver names must
be known. Different drivers require the same known board or reviewed boards
explicitly grouped into the same catalog family, or an exact source-reviewed
clone link (ADR 0062); titles cannot bypass this guard.
Device-list differences alone do not split same-driver protection/development
releases. Neither a
shared driver nor BIOS/ROM dependencies (`romof`) establish game identity.

Ordinary bootlegs matching the parent or an official regional alias in the same
eligible clone group can join the same card when hardware is compatible. CPS2 Phoenix Edition and explicitly
identified protection-removal hacks are technical releases. Prototype, beta, demo
and test labels also belong to releases; initial defaults still prefer retail
versions. Full descriptions and provenance remain unchanged on releases.

Cabinet labels such as `(deluxe)`, `(standard)` and `(upright)` do not create
separate cards. Recognized cabinet/revision/product-code qualifiers are removed
from initial card titles. Explicit content editions, other hacks, unknown renamed
bootlegs, incomplete/unofficial content without supported identity, unresolved
parents and cycles remain held. Piranha/Abscam remain reviewed separate derivatives.
These are conservative boundaries rather than exhaustive semantic classification.

Versions for distinct hardware platforms remain separate, as do independent game
roots: 1943 Kai and Mark II are not folded into 1943. The Atari Alpha One prototypes
belong to Major Havoc in MAME; the Vision Electronics Alpha One is unrelated.
Existing reviewed revisions of a logical board family can still share a card.

A same-title bootleg with unmapped hardware may inherit the eligible parent's
known browsing family. This is not permission to fold across driver files;
its exact board stays unknown. Explicit routing, known different platforms and
ineligible parents are never overridden. Mega-Tech ports remain separate because
their platform and source identities are distinct.

When MAME assigns several branches directly to one parent, compatible sibling
roots can share a separate card. Siblings require complete eligible ancestry and
a known common board/family, or matching nonempty driver/device profiles. They
must preserve content boundaries and unfamiliar qualifiers. Renamed bootlegs also
need the same derivative title and manufacturer, ignoring standalone numeric set
labels, unless a scoped source review establishes an equivalent maker branch
(currently sf2b/sf2b2; source metadata is retained). For example, 1991 Spikes has two releases; older Aero Fighters includes
aerofgtb/aerofgtc/sonicwi separately from the newer-hardware parent and its bootlegs.
Device profiles are supporting evidence, not exact board identification.

The initial card title comes from the accepted parent with recognized technical
suffixes removed, including supported slash-separated cabinet lists such as
`deluxe/upright` and the `Export` market label. These remain in full release
descriptions; Export does not assign a guessed region. Action Fighter's nine board/control variants become one card;
Adders and Ladders v2.0/v2.1 become another. Full descriptions, short names,
ROM/CHD evidence and board details remain on individual releases.

The default release follows configured region priorities using the description,
not the short name. A later import preserves an existing valid default, curated
card title, additional release fields, attached source metadata and media.
Platform display names and extra provider-mapping fields also survive refresh.
Family-specific region priorities take precedence over the general Arcade policy.

## Scope And Routing

Classification happens before hardware routing or folding. BIOS/devices are
support records. Home/other systems, non-runnable entries, mechanical machines
and deferred categories do not become game cards. Missing or contradictory
classification is held for review. Positive video-game genres include quiz and
tabletop; preliminary emulation alone does not exclude a game. Console-derived
arcades such as PlayChoice-10, VS. System, Mega Play and Mega-Tech remain eligible.

Hardware identification and catalog folders are separate. Reviewed hardware rules
project eligible sets to logical families inside the shared physical Arcade catalog.
Unmapped eligible games carry `arcade.platform: Arcade - Other`
and `kind: collection`; they still live directly under `platforms/Arcade/`. Conflicting
driver/BIOS rules, missing dependencies or dependency cycles require review.

`--platform-map <json>` adds or replaces registry rules. Explicit set rules take
precedence, followed by reviewed `source_sets` entries scoped to one driver.
BIOS and whole-driver rules otherwise must agree. Scoped board exceptions still
check missing/cyclic ROM dependencies. Overrides are preserved
with the source snapshot. Names must start with `Arcade - ` and every platform
must declare `kind` (`hardware` or `collection`) and `naming_evidence`.

```json
{
  "version": 1,
  "platforms": {
    "Arcade - Example - Board": {
      "kind": "hardware",
      "naming_evidence": "Local reviewed hardware reference"
    }
  },
  "sources": {"example/board.cpp": "Arcade - Example - Board"},
  "bios": {},
  "sets": {}
}
```

Use `source_sets` for a mixed driver rather than routing its entire contents:

```json
{
  "version": 1,
  "source_sets": {
    "sega/system1.cpp": {
      "wboy": "Arcade - Sega - System 1",
      "wboysys2": "Arcade - Sega - System 2"
    }
  }
}
```

Optional `families` maps a hardware label to its logical platform, after all
hardware/dependency checks. Targets must be declared hardware platforms with naming
evidence. Collections, chains and cycles are invalid. Built-in groups are:

| Logical platform in each card | Hardware labels grouped |
| --- | --- |
| Sega - System 16 | System 16A, 16B, 16C |
| Sega - System 32 | System 32, Multi 32 |
| Namco - NA | NA-1, NA-2 |
| Namco - NB | NB-1, NB-2 |
| Namco - System 22 | System 22, Super System 22 |
| Namco - System 23 | System 23, Super System 23, System 23 Evolution 2 |
| Irem - M72 | M72, M81, M82, M84, M85 |

All names carry the `Arcade - ` prefix. These are RomMeta browsing/grouping choices,
not claims that every board is a minor revision or that ROMs are interchangeable.
CPS-1/2/3, Neo Geo/Hyper Neo Geo 64, Naomi/Naomi 2/Atomiswave and other unmapped
families retain separate platforms. System 22.5 remains separate from System 23.

For example, the built-in projection includes:

```json
{
  "version": 1,
  "families": {
    "Arcade - Namco - NA-1": "Arcade - Namco - NA",
    "Arcade - Namco - NA-2": "Arcade - Namco - NA"
  }
}
```

To opt a board out, override its family with its own full hardware name. As with
other routing changes, this changes the snapshot hash and does not migrate an
existing catalog. Board routing rules refer to hardware labels; families are
metadata and fold boundaries. The effective routing includes `catalog_platform:
"Arcade"`. This layout value is fixed and hashed with the other rules.

Unknown sets in that driver stay unresolved unless another verified rule applies.
A matching short name in a different driver does not reuse the scoped rule.
Changing the effective registry changes the source snapshot ID. A previous
catalog with cards under Other needs explicit relocation review; use a separate
catalog root while refining routing.

Example names above are placeholders. A driver can emulate more than one board;
verify membership before assigning its entire contents to a hardware platform.
MAME is authoritative for set names and full descriptions within its snapshot.
Hardware names have separate provenance; `Classics` and `Other` are local
collection choices. Provider aliases and game display names are separate curation.

## Durable Data

```text
<data-root>/
  platforms/
    Arcade/
      _platform.yaml
      _catalog.yaml
      Street Fighter II - The World Warrior/
        game.yaml
        fingerprints.yaml
  bindings/romsets/mame/<version>-<digest>/
    _source.yaml
    mame.zip
    classifications.zip
    routing.json
    catalog.yaml
    catalog-planned.yaml
```

Card metadata separates platform membership from storage:

```yaml
arcade:
  platform: Arcade - Namco - NA
  kind: hardware
  naming_evidence: "RomMeta catalog family grouping; pinned MAME hardware notes"
title: Emeraldia
releases:
  emeralda:
    title: Emeraldia (World)
    mame:
      hardware: Arcade - Namco - NA-2
  emeraldaj:
    title: Emeraldia (Japan)
    mame:
      hardware: Arcade - Namco - NA-1
```

This excerpt omits ordinary authority/snapshot/region fields. `arcade.platform`
is the logical family, while each release retains its exact routed board. `kind`
marks real hardware versus mixed/unknown collections. Optional local fields in
the `arcade` block survive repeat imports. Existing files remain separate
`game.yaml` and `fingerprints.yaml`; this change does not consolidate YAML.

Identical cleaned titles receive readable hardware-family labels when available;
set-name suffixes remain the fallback for unresolved collisions. These suffixes
do not by themselves prove that two cards can merge. Comparison is case-insensitive;
reserved literal suffix names are not overwritten. Names are resolved against the
complete eligible source, keeping subset imports stable.

An uncompressed input is saved as `mame.xml`. The input is retained byte for byte;
ZIP member name, build and file SHA256 values are recorded in `_source.yaml`.
The snapshot suffix is the first 12 hexadecimal digits of SHA256 over the source,
classification and effective routing hashes separated by newlines. Routing changes
therefore create a different snapshot even for the same MAME release.

`catalog.yaml` maps set short names to physical `platform: Arcade`, game folder
and release key, retaining logical `family` and `hardware` for identified boards.
Reports expose `platform`, `family` and `hardware`; `releases.<set>.mame.hardware` preserves the exact routed board
inside a folded card. Collections omit this field rather than claiming a board.
Release keys remain short names, e.g. `sf2` and `sf2ua`, while full descriptions
remain in `releases.<set>.title`. The `mame` block records current snapshot,
clone/ROM/sample parents, driver, year, manufacturer and status. The raw XML
retains all version-specific ROM/disk/device/sample dependency detail, including
records that did not become cards.

Fingerprints carry `source.snapshot`, scoped IDs, and `file.kind` equal to
`rom-component` or `chd-disk`. Chip and disk hashes are excluded from ordinary
whole-game provider hash lookup. They are not ZIP hashes. Older snapshot
fingerprints and bindings remain when a newer snapshot refreshes a release.

## Refresh, Recovery And Limits

To recreate a disposable MAME-only Arcade catalog with new grouping rules,
move **both** `platforms/Arcade` and `bindings/romsets/mame` to a backup outside
the active `platforms` and `bindings/romsets` trees, then rerun ordinary
`dat import <mame.xml-or-zip>` (preview) and the same command with `--apply`.
Keep configuration, other platform catalogs and the optional classification cache.
If the cache is absent, classification downloads automatically; an explicit
`--classifications` path can also reuse the backup's saved ZIP. Moving cards alone
leaves accepted bindings to old paths and correctly blocks the regrouped import.
Do not use this recreation workflow for an enriched catalog whose curation/media
must be migrated. No separate post-import fold is needed for the new import.

The importer scans the shared Arcade catalog and reports matching/grouping conflicts
before applying. Known MAME identities cannot silently change the card's logical
family. Reports summarize one physical platform plus separate family counts. Existing matches use MAME authority identities rather than
component hashes. Omitted cards/releases remain. Snapshot files are immutable and
hash-checked; accepted bindings cannot silently change paths. Preflight checks
previous snapshots too: importing a bound set into another platform/card produces
a conflict before any catalog writes. Use a new catalog root while changing family
grouping; use ordinary catalog fold below for supported existing paths.

The shared catalog uses the existing staged additive writer in one Arcade
directory swap; the source snapshot and binding files are separate writes.
If the final directory activation fails, the complete prepared catalog remains
at the path printed in the error. An existing platform is restored; if that
rollback also fails, the error identifies the original backup as well. Partial
stages from failed writes are still removed. Retained stages are recovery copies,
not automatically resumed by a later import.
`catalog-planned.yaml` records the intended mapping before the swap. On a
binding-write failure, recover from that mapping or rerun the same input after
fixing the error. A source directory left without `catalog.yaml` needs the same
rerun/recovery. Keep a simple backup before broad imports. Other platform
directories, including independent Redump imports, are not scanned or rewritten.

`validate` checks saved source hashes, binding target releases and fingerprint
agreement with those bindings, plus current release hardware against its snapshot
binding, and card `arcade.platform` against the binding's logical `family`.
Legacy separate-platform bindings remain readable; older snapshot hardware
can differ from the current release's newer snapshot. The generic provider-ID
`catalog fold` uses accepted MAME relationships for these version-bound cards. Supported
same-snapshot MAME merges and renames update their bindings through the same command.

This path does not yet implement FBNeo bindings, Software List import, cross-version
set rename review, hardware relocation or ROM inventory. Offline ES-DE set-name projection is
described in [export scenarios](export-scenarios.md).
No service is required for construction. Enrichment uses the provider-specific
matching described below; new family filters/UI remain separate work.

Eligible records may include Naomi, Naomi 2, Chihiro, Triforce and Namco System
246/256 when present in the selected MAME snapshot. Disk records can require CHDs;
a DAT entry does not prove that the user's ROM inventory has those files or that
a game is playable. Separate Redump records are not automatically deduplicated by
title or treated as payload-compatible.

## Folding Existing Arcade Cards

New MAME imports fold compatible sets by default. Continue directly to metadata
and media collection; there is no separate repair step.

To regroup an older catalog or one imported with `--no-fold`, use ordinary fold
from the data root:

```powershell
rom-meta catalog fold --platform Arcade --details
rom-meta catalog fold --platform Arcade --apply
```

Fold uses the saved MAME source/classification/routing snapshot without network
lookups. It preserves all releases, provider matches, fingerprints and media while
updating set-to-card bindings. `--match <text>` limits complete groups. Partial
imports retain only their imported sets; names remain stable against the full
source. Cards on different hardware or with meaningful content differences stay
separate even when providers return the same ID. Already grouped cards are a no-op.

Preview changes no catalog files. Stop writers before apply; original folders and
bindings are retained under the printed backup path. Conflicting curation/media,
splits and multiple snapshots remain unsupported and cannot silently lose data.
Source-ID `--interactive`/`--review-plan` modes hold MAME groups; `--editions-only`
excludes this authority-based grouping. Unbound cards and other platforms retain
normal provider-ID folding. See `catalog fold --help`.

Prior card/check history remains in `arcade_repair.original_cards`; raw MAME
release descriptions remain intact.
Manual recovery after process/system interruption is still supported by the
original folders, binding and manifest in the backup; no crash journal is added.

## Metadata and Media

After successful MAME import, run ordinary fetch from the data root. No extra fold
or reimport is required:

```powershell
rom-meta fetch --platform Arcade --apply
```

This runs the configured metadata, artwork and video sources in their configured
order, just like other platforms. No manual source selection is required. To fetch
only metadata and artwork, add `--type metadata,artwork`. Repeat the same command
to resume; do not add `--force-overwrite` for normal continuation.
ScreenScraper needs the existing user/developer credentials. Its MAME system
includes child arcade systems, so no per-board user configuration is required.
The first run automatically upgrades the disposable catalog cache and reparses
Arcade rows to expose the imported authority IDs; it does not change card layout.

EmuMovies uses the configured FTP credentials and extractor. Its artwork downloads
packages; `--limit` limits packages, not extracted
images. A dry run with `--plan <path>` can inspect selected packages first.
ScreenScraper and EmuMovies require exact imported MAME set identity. LaunchBox
uses its optional `Mame.xml` names and arcade-family sections where available,
with the general Arcade section as fallback. Separate named Mega-Tech/Mega Play/
PlayChoice versions cannot borrow the ordinary arcade record by base title.
Bootleg/hack title matching requires corresponding source evidence. Shared source
IDs are descriptive metadata, not permission to fold hardware-distinct cards.
Hasheous queries SHA1s from imported MAME components in the `MAMEArcade` scope;
it accepts a response only with the exact returned set name, Arcade system and
matching SHA1. Larger components are preferred to avoid a BIOS-only query. These
are not ZIP hashes. IGDB consumes Hasheous IDs as hints and independently checks
the arcade platform, regional titles, named ports and unofficial variants.
OpenVGDB requires the exact authority `<set>.zip` filename in its Arcade section.
Use `fetch --help` for source-specific options.
