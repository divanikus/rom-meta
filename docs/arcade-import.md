# MAME Arcade Import

The supported path is `rom-meta dat import`. It reads official MAME
listxml, obtains version-matched Progetto-SNAPS classification data, distributes
eligible games into `platforms/Arcade/<game>/` and optionally folds confirmed
revisions. Each card retains its logical platform in `arcade.platform`.

## Preview And Apply

Initialize a separate catalog once. Run from its root, or set `--data-root`:

```shell
rom-meta init <catalog-root>
cd <catalog-root>
rom-meta dat import <mame.xml-or-zip> --report <new-plan.json>
rom-meta dat import <mame.xml-or-zip> --sets sf2,sf2ua,mslug,mslug2,kinst --report <new-apply.json> --apply
rom-meta validate .
```

The XML root selects MAME, including `.dat` members inside ZIPs; filenames do not.
No-Intro/Redump use the ordinary importer. `dat arcade` is a compatibility alias.
MAME always uses `Arcade`; unrelated `--platform` overrides fail. MAME-only options
also fail on ordinary DATs.

## Classification Acquisition

The DAT build requires an exact Progetto-SNAPS version, never the nearest/latest.
Acquisition order:

1. Explicit `--classifications <zip>`.
2. Verified durable snapshot matching input SHA256 and MAME version.
3. Validated `.rommeta/cache/arcade/mame/<version>/` under the data root.
4. Versioned HTTPS download from Progetto-SNAPS.

`--offline` disables download; applied snapshots survive cache deletion.
Conflicting snapshot hashes need explicit selection. Corrupt snapshots/cache fail
rather than silently selecting other bytes.

Preview may download/cache support data but creates no cards or durable bindings.
Apply retains the raw classification ZIP. Downloads have timeout/size limits and
ZIP/required-INI/version validation before SHA256-manifest publication. SHA256 is
a local byte pin, not an upstream signature. Failure leaves the catalog unchanged
and reports the explicit-file alternative.

Console/JSON reports show path, version, hash and origin; cached/downloaded data
also includes its URL. Classification data is not bundled with RomMeta.
FBNeo has separate versioning, no applied binding path and no MAME classification
requests. Distribution DATs and Software Lists are unsupported apply inputs.

## Selection And Folding

Reports require a new filename and existing parent directory. They include every
machine's classification/reason, selection, hardware, proposed card, fold review
and bindings. `--sets` accepts exact comma-separated short names; excluded,
unresolved or absent sets block apply. `--member` selects ZIP XML.

Grouping/naming uses all eligible sets before selection, so clone-only imports
retain stable card identity without importing the parent. Omit `--sets` for all
eligible sets; review before broad apply.

`--fold` defaults on; `--no-fold` creates separate cards. Automatic fold follows
eligible `cloneof` ancestry within one logical platform, regardless of regional
names/technical qualifiers. Both drivers must be known. Different drivers need a
known shared board, reviewed family, or exact source-reviewed clone link; titles
cannot bypass this. Device differences alone do not split same-driver protection/
development releases. Shared drivers and `romof`/BIOS dependencies alone prove no identity.

Compatible ordinary bootlegs matching a parent/regional alias can join its card.
CPS2 Phoenix/protection-removal hacks are technical releases; demos, prototypes,
betas and tests stay releases with retail preferred by default. Descriptions and
provenance remain intact. Cabinet/revision/product-code labels do not create cards.

Explicit content editions, other hacks, unknown renamed bootlegs, unsupported
unofficial content, unresolved parents and cycles remain held. Piranha/Abscam
remain separate derivatives. Distinct hardware and independent roots stay separate
(e.g. 1943 Kai/Mark II versus 1943). Atari Alpha One belongs to Major Havoc;
Vision Electronics Alpha One does not. These rules are conservative, not exhaustive.

Same-title bootlegs with unmapped hardware may inherit an eligible parent's
browsing family, but not its exact board or cross-driver fold permission. Explicit
routing, known different platforms and ineligible parents still win. Mega-Tech
ports remain distinct.

Compatible sibling roots can form a separate card when ancestry is complete and
eligible, with a shared board/family or matching nonempty driver/device profiles.
Content boundaries and unfamiliar qualifiers remain. Renamed bootlegs also need
matching derivative title/manufacturer (ignoring numeric set labels), unless an
exact scoped source review permits the maker branch, currently sf2b/sf2b2.
For example, aerofgtb/aerofgtc/sonicwi remain separate from their newer-hardware
parent. Device profiles support grouping without proving an exact board.

Initial titles use the accepted parent minus recognized technical suffixes,
including cabinet lists (`deluxe/upright`) and `Export`. Full releases retain them;
Export supplies no guessed region. Defaults use description-based region priority,
with family priorities before general Arcade. Refresh preserves valid defaults,
curated titles, extra release fields, sources/media and platform provider mappings.

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

After import, fetch normally from the data root; no extra fold/reimport is needed:

```powershell
rom-meta fetch --platform Arcade --apply
```

Configured metadata/artwork/video run in order. `--type metadata,artwork` restricts
types. Repeat to resume; normal continuation needs no `--force-overwrite`.
ScreenScraper uses existing user/developer credentials and its child-arcade systems.
The first run upgrades/reparses disposable cache rows for MAME IDs without changing layout.

EmuMovies uses configured FTP/extractor; artwork is package-based, so `--limit`
limits packages, not extracted images. Preview selected packages with `--plan <path>`.
ScreenScraper/EmuMovies require exact imported set identity. LaunchBox uses optional
`Mame.xml` names and family sections, falling back to Arcade. Named console-derived
ports cannot borrow ordinary arcade records; bootleg/hack matches need source
evidence. Shared descriptive IDs do not authorize hardware-distinct folds.

Hasheous checks imported component SHA1s in `MAMEArcade`, requiring exact returned
set/system/SHA1 and preferring larger components over BIOS-only evidence. These
are not ZIP hashes. IGDB independently validates Hasheous hints against platform,
regional names, ports and unofficial variants. OpenVGDB requires exact `<set>.zip`
in its Arcade section. See `fetch --help` for provider options.
