# Prepare translations and hacks

The separate `scripts/prepare-variants` helper converts explicit relationships
into ordinary [variant and fingerprint YAML](catalog-variants.md). The compiled
`prepare-variants.exe` runs as a standalone console program on Windows x64, without
Go or a source checkout. Normal offline exports consume prepared variants directly.
The helper remains separate from the core CLI.

Source-first discovery resolves published relationships and unique catalog names
into game links, with a terminal review queue for uncertain cases. The older
collection mode can also prepare exact game/base keys. Staging checks modified
output identities and catalog preconditions. The helper uses local metadata
snapshots; it does not download sources or select fuzzy title matches.

Variants can link only to their containing game: omit `based_on` and use a
reviewed original-game association. Exact release/variant parents remain optional.
See [game-only catalog records](catalog-variants.md#catalog-records).

## Run the standalone helper

Extract a `prepare-variants-<version>-windows-amd64.zip` helper package into a
folder and open PowerShell there:

```powershell
.\prepare-variants.exe --help
```

The package includes the executable, `README.txt`, this guide, build information
and dependency licenses. Follow the source download/setup steps below, then run:

```powershell
.\prepare-variants.exe --catalog "D:\RomMetaWork\catalog" --sources "D:\RomMetaWork\sources" --discover "D:\RomMetaWork\review"
.\prepare-variants.exe --review "D:\RomMetaWork\review"
.\prepare-variants.exe --workspace "D:\RomMetaWork\review" --out "D:\RomMetaWork\prepared"
```

Replace the example paths with your folders. The last command previews; add
`--apply` to create staging after review. Source discovery needs neither local
ROMs nor Go. It is a terminal program; launching it from PowerShell keeps the
output visible. Linux builds use `./prepare-variants` after `chmod +x`.

The shell examples below use `prepare-variants` as the command name. In the
extracted Windows folder, use `.\prepare-variants.exe`; on Linux use
`./prepare-variants`, or add the executable directory to PATH.

Standalone helper packages are built separately from the main CLI archives.
Existing published 0.1.0 main CLI assets are unchanged. If a helper archive is not
available with a release, developers can build one from a clean checkout using
[the packaging instructions](../CONTRIBUTING.md). To build just a local Windows
executable with the Go toolchain from `go.mod`:

```shell
go build -trimpath -o bin/prepare-variants.exe ./scripts/prepare-variants
```

Go is required for that build, not for running the resulting executable.

## Download the source metadata

Start with a prepared catalog of original games, including their descriptions and
artwork if you want those during export. Normal No-Intro/Redump imports describe
original releases; the additional translation DATs below describe modified outputs.
Keep downloaded metadata in one dedicated `--sources` folder, separate from ROMs,
the catalog and the review/staging folders.

For English translations, open [Omni-Games DAT files / OmniScope](https://omni-games.info/datfiles.php),
filter the Group or DAT name for `[T-En] Collection`, and choose your systems:

| Your system | Collection to select | Browse the collection |
| --- | --- | --- |
| NES / Famicom | Nintendo - Famicom [T-En] Collection | [NES](https://omni-games.info/omnidat_details.php?datid=DAT-C86E4ED2) |
| PS1 | Sony - PlayStation [T-En] Collection | [PS1](https://omni-games.info/omnidat_details.php?datid=DAT-87068337) |
| PS2 | Sony - PlayStation 2 [T-En] Collection | [PS2](https://omni-games.info/omnidat_details.php?datid=DAT-3DCF30C6) |
| Saturn | Sega - Saturn [T-En] Collection | [Saturn](https://omni-games.info/omnidat_details.php?datid=DAT-A81D3E21) |

Other systems use the same collection group. Open the selected collection in
OmniScope and generate/download its full XML DAT, keeping the original collection
structure when offered. Do not prune to one translation per game or apply region
filters when building a general variant library. Keep optical `disk` identities
in their original representation. Save the resulting `.dat`/`.xml` file, or a ZIP
containing those files, anywhere under `--sources`. ZIP DATs are read without
extraction. Export dates do not establish patch release dates. DATVault is an
alternative for users who already have it; it is not required for the public path.

`T-En` means this publisher's English translation collection, not a universal
name for translations. The helper reads compatible XML collections labelled
`[T-Ru]`, `[T-Fr]`, `[T-Pt-BR]`, etc. as well. These labels use language codes;
the language is not guessed from the downloaded filename. For another language,
use a publisher's compatible translation DAT with a declared language. This does
not imply a maintained public equivalent of every T-En collection in every
language. Ordinary retail DATs and other unidentified layouts are not silently
treated as English translations.

### Optional sources that help identify originals

These supplement the output DATs. Place them in the same sources tree; normal
discovery detects them without adapter flags. Nothing is downloaded automatically.

| Source | What to obtain | How it is connected |
| --- | --- | --- |
| [Gaming Alexandria Frontend Project](https://www.gamingalexandria.com/wp/frontend-project/) | Under NES, **Nintendo - Famicom [GA] [T-En] Collection**, choose **Download DAT**. Use the full collection; the 1G1R version removes alternatives. | Its complete original names help with renamed English outputs. XML DATs and ZIP DATs are recognized by their header. It can also supply the output list itself. |
| [Romhack Archive](https://github.com/videogame-archive/romhack-archive) | The repository's `database/**/romhack.json` metadata, retaining the `database/platform/base-ROM/output-ROM/romhack.json` hierarchy. The CSV export is not this adapter's input. | The original-game relationship comes from the hierarchy and matching modified-output hashes. Patches are not read. |
| [HackHash](https://hackhash.darkblood.uk/) | The [detailed JSON export](https://hackhash.darkblood.uk/api/entries/export?format=detailed), using `format=detailed`. Standard XML/minimal JSON exports lack the detailed `baseRom` relation. See the [publisher's export implementation](https://github.com/darkblood159/HackHash/blob/main/src/app/api/entries/export/route.ts). | Detailed `machines[].details.baseRom` relationships and output fingerprints are recognized from JSON contents, regardless of filename. |
| Romhack Plaza | No compatible source-first snapshot adapter in this helper yet. | An API key alone does not connect Plaza; acquisition needs a separate adapter. Do not put credentials in the sources tree. |

For Romhack Archive, downloading/extracting the repository ZIP is sufficient if
you retain the hierarchy. With Git, the following downloads only its metadata
working tree and enables long paths on Windows:

```shell
git clone --depth 1 --filter=blob:none --no-checkout https://github.com/videogame-archive/romhack-archive.git "/snapshots/romhack-archive"
git -C "/snapshots/romhack-archive" config core.longpaths true
git -C "/snapshots/romhack-archive" sparse-checkout set --no-cone "/database/**/romhack.json"
git -C "/snapshots/romhack-archive" checkout
```

Only the extracted Romhack Archive metadata tree is consumed; a repository ZIP
is not automatically expanded. You can save the HackHash export in a browser or
use this public metadata request (use `curl.exe` in Windows PowerShell if `curl`
is an alias). A direct request can receive HTTP 403 from the service; use the
publisher's browser export interface in that case, or omit this optional source:

```shell
curl --fail --location "https://hackhash.darkblood.uk/api/entries/export?format=detailed" --output "/snapshots/hackhash-detailed.json"
```

For example, this entire tree connects through one `--sources /snapshots`:

```text
snapshots/
  translations/       # downloaded XML DATs or ZIPs of DATs; filenames are arbitrary
  gaming-alexandria/  # optional full GA translation DAT
  romhack-archive/    # optional extracted metadata checkout
    database/...
  hackhash-detailed.json  # optional detailed export
```

Metadata files/XML members are bounded to 32 MiB and ZIP containers to 256 MiB
for automatic inspection. Oversized automatic inputs are reported and skipped;
an explicitly selected oversized input fails clearly.

Sources improve only outputs with matching published identities. Installing an
additional source does not guarantee a relationship for every translation or
add missing descriptions/artwork to original cards.

## Discover and review a source library without ROMs

Start with a read-only catalog snapshot containing original games and locally
obtained translation XML DAT files (see downloads above). No ROM directory, API key or exact patch version is
required. Run the standalone helper:

```shell
prepare-variants --catalog "/catalog-snapshot" --sources "/snapshots" --discover "/work/translation-review"
prepare-variants --review "/work/translation-review"
prepare-variants --workspace "/work/translation-review" --out "/work/variant-catalog" --media artwork
```

Discovery searches `--sources` recursively for supported XML DATs and ZIP
members, identifies systems/languages, and connects known optional sources.
New files in this folder are picked up on refresh without further questions.
Missing catalog platforms are skipped with an explanation in `report.json`;
ambiguities remain explicit. Discovery writes analysis JSON in a new, empty
workspace; it does not write catalog YAML. The last command previews only
ready links. Repeat it with `--apply` to create the separate sparse catalog.
Unresolved rows remain in the workspace and in the manifest's `holds`; they do
not block ready links. An empty ready set reports an explicit error. Publication,
metadata fetching and frontend/RomM import remain separate normal workflows.

Discovery follows these rules:

- A translation DAT output supplies its own published raw size/SHA1 or typed CHD combined
  SHA1. Original/base hashes never become modified-output hashes.
- Optional Gaming Alexandria, Romhack Archive and HackHash metadata are detected
  automatically. `--ga`, `--rha` and `--hackhash` remain explicit overrides.
  An exact modified-output match supplies an explicit parent assertion;
  published strong base hashes can select its original game. Conflicting or
  unresolved explicit relationships remain for review.
- Otherwise, complete names before a translation label in the output/container are
  compared with catalog release names, game titles and aliases. Provider aliases
  require a matching stored platform. Multiple releases of one game collapse to
  that game. Multiple games, conflicting labels and unmatched names require review.
  Normalization preserves word/number boundaries; there is no fuzzy auto-selection.
- The platform registry plus a small publisher platform-label adapter handles names
  such as Famicom/NES and Super Famicom/SNES. Missing catalog platforms are
  reported and skipped; ambiguous identities fail explicitly.
  `config/platforms.yaml` can supply local aliases.
- Existing output ownership is reported, not duplicated. Ownership conflicts
  remain for review and staging also checks untouched games on selected platforms.
- Whole multi-payload ROM groups remain `unsupported`. No member is promoted to
  a full-set fingerprint. Documentation, BIOS and nodump rows are skipped.
  Each published optical `disk` remains an independent typed CHD identity.

The report lists detected sources, languages, skipped platforms and ignored
metadata, and distinguishes `ready`, `review`, `already-owned`, `unsupported` and
`excluded`. It records source names, relationship evidence, candidates and the
selected original. Name-based automatic links are catalog/source assertions;
use `--review-all` with `--review` to inspect or correct automatic choices too.
This does not assert an exact input pressing, patch revision or set completeness.

The terminal accepts:

| Input | Action |
| --- | --- |
| A candidate number | Select that existing game |
| `f text` | Search names within the source platform; results still need selection |
| `b Game folder` | Select an existing exact catalog game key |
| `b psx/Game folder` | Explicitly select a game on another catalog platform |
| `n Original title` | Prepare a new original metadata anchor, with an unspecified edition |
| `n psx/Original title` | Prepare that anchor on another existing platform |
| `s` | Leave this row for later |
| `x` | Persistently exclude the output |
| `a` | Clear its saved decision and return to automatic discovery |
| `q` or end of input | Save progress and finish |

Decisions are written after each selection and reused by output fingerprint,
independent of filenames and DAT content revisions. Changed output identities
need a new decision. `--review-all` also lets you reconsider excluded rows.
A new original card contains only the supplied title, an explicitly unspecified
metadata anchor and provenance; it invents no retail fingerprint, region, serial,
description or artwork. Enrich it through normal catalog preparation before
expecting those fields during export.

The workspace contains `decisions.json`, `report.json`, `links.json` and
`.variant-discovery.json`. Keep `decisions.json` as reusable data; no per-game code
or document SHA256 editing is needed. Run `--discover /work/translation-review`
again to refresh analysis with the same configured sources/catalog. Source paths
and catalog root are workstation settings. Default folder discovery includes new
metadata files on refresh. To narrow output DATs explicitly, repeat `--dat` with
a relative XML/ZIP file or directory, for example `--dat translations/ps1.dat`.
Other source adapters are still found automatically. The old `--ten` flag and
existing workspaces remain supported for compatibility; new commands use
`--dat` or automatic selection. Changing the explicit selection/roots needs a
new workspace, to which you can copy `decisions.json` before discovery resumes.
Generated links/report edits are detected and preserved rather than overwritten.

Staging checks pinned DAT/source bytes and selected catalog document hashes;
after changing sources, decisions or original cards, refresh discovery and use a
new staging destination. An identical stage repeats unchanged. Avoid concurrent
writers against a workspace or source snapshot. An interrupted review retains its
last saved choices; if a process was terminated, remove `.discovery-lock` only
after checking no helper is running. If interruption or manual edits leave the
generated files inconsistent, retain the old workspace, create a new one and copy
its decisions before refreshing. Local ROM observation, remote downloads/Plaza,
automatic metadata enrichment, whole-set adapters and RomM writes are outside this
source-first increment. The existing local collection mode remains available.

## Advanced: prepare explicit published-DAT relationships

A local translation XML DAT supplies published output identities; your reviewed map
supplies the original game. The DAT alone does not identify that game. Add `dat`
to an ordinary relationship entry, naming the exact `rom` or `disk` output:

```json
{
  "schema_version": 1,
  "entries": [{
    "platform": "Sony - PlayStation",
    "game": "Original Game",
    "variant": {
      "key": "Name from the source",
      "title": "Translated Game",
      "kind": "translation",
      "languages": ["en"]
    },
    "dat": {"path": "ps1-ten.xml", "name": "Exact disk name from DAT"},
    "evidence": {
      "relationship": "reviewed",
      "links": [{"source": "Reviewed project mapping", "entry": "Translation record", "parent": "Original Game"}]
    },
    "expected_documents": {
      "game.yaml": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "fingerprints.yaml": "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
    }
  }]
}
```

Replace placeholder names/digests with the reviewed bundle and current document
SHA256 values. Variant keys are arbitrary identifiers; no translation-name
vocabulary is required. `dat.path` is relative to `--sources`. The DAT header must
match the entry's platform, or an explicit `dat.source_platform` publisher label
when the reviewed target uses another platform name. Optional `dat.member` names
a safe relative XML member inside a ZIP; the whole ZIP is pinned by SHA256. Its content is pinned by SHA256 automatically; an
existing snapshot pin must agree. The exact output name supplies `file`; supplied
file constraints must agree with that output. Keep a separate original-game
source link: an output hash does not itself prove a parent.

Use the normal staging command below, omitting `--rom-root` and `inputs` when
preparing only published identities. Raw `rom` records supply positive size and
SHA1 with optional MD5/CRC32. Optical `disk` SHA1 becomes `chd-combined-sha1`,
without claiming compressed-file size or raw-disc hashes. No local game is needed.
Underscore-prefixed documentation/BIOS containers and multi-ROM groups are not imported by
this adapter. Missing/duplicate output names, platform/hash conflicts and stale
source pins fail explicitly.

Optional `inputs` still verify local files; `--rom-root` is then required. A CHD
input checks its header/stat identity, without full-image hashing or an integrity
claim. Game-only relationships use `reviewed`; release-name/base-hash evidence
still requires an exact `based_on`. Ordinary offline export recognizes a prepared
variant by its own fingerprint, even after the file is renamed.

## Advanced: collect relationships from local ROMs

Source collection is an optional external mode of the same helper. Supply one or
more local metadata sources; the command scans only the selected platforms'
`translations/` subdirectories and builds the ordinary relationship JSON:

```shell
prepare-variants --catalog "/catalog-snapshot" --rom-root "/roms" --sources "/snapshots" --ga "ga.dat" --rha "rha" --hackhash "hackhash-detailed.json" --platforms nes,snes,ngpc --collect-links "/work/links.json" --report "/work/source-preview.json"
```

Preview writes only the requested report. Repeat with `--apply` and a new report
path to save `links.json`, then use the staging command below. Omit source flags
you do not need. Source paths are relative to `--sources`; RHA's root contains
`database/`. `--subdir` changes the selected ROM subdirectory. All output paths
must be new and separate from catalog, ROM and source trees.

| Adapter | Relationship used |
| --- | --- |
| `--ga` | Gaming Alexandria Famicom DAT: the complete original name before a well-formed T-En label; a source naming assertion |
| `--rha` | Romhack Archive `database/platform/base-ROM/output-ROM/romhack.json` hierarchy, with modified hashes and structured patch credits |
| `--hackhash` | HackHash detailed JSON `machines[].details.baseRom`; strong published base hashes take precedence only when consistent with the source name |

See the publishers' [GA page](https://www.gamingalexandria.com/wp/frontend-project/),
[RHA schema](https://github.com/videogame-archive/romhack-archive/blob/main/docs/contributor-guide.md)
and [HackHash project](https://github.com/darkblood159/HackHash). Use the detailed
HackHash export with `baseRom`; the minimal DAT export lacks that relationship.
RHA `info.translatedTitle` is a title-screen boolean, not a translated name.
Its optional `info.name`, patch metadata and original source hierarchy are kept
distinct. On Windows, retain the upstream hierarchy and use a checkout/extractor
that supports long paths.

Matching uses observed modified size/SHA1, with available MD5/CRC32 constraints.
The parent must select one same-platform official release by full name/alias or
published strong base hashes. Literal qualified names take precedence; optional
punctuation handling keeps word boundaries and qualifiers: I, II cannot become III.
No default release, bare title or edit-distance fallback is used. Source names
remain assertions, not independent verification of the original bytes.

The report separates held input files and malformed source rows. Missing or
ambiguous parents, conflicting hashes and already-owned outputs are held.
Damaged labels are reported without guessing repairs. Current collection handles
raw cartridge files/single-payload ZIPs; CHD, ISO, RVZ and unsupported archives
require representation-specific preparation. Merely collecting a source does
not add a reader or optical parent adapter. Every source file is pinned by SHA256,
duplicate JSON keys fail and staged application rechecks its preconditions.

Collection makes fresh proposals and never replaces prior reviewed maps or live
catalog entries. Unknown source fields can remain in their pinned snapshots;
source extraction/parent accuracy is separate from successful catalog resolution.

## Preview and stage

Run from the checkout root. Keep the source catalog, ROMs, pinned snapshots,
relationship JSON and staging directory outside Git:

```shell
prepare-variants --catalog "/catalog" --rom-root "/roms" --sources "/snapshots" --links "/work/links.json" --out "/work/variant-catalog" --report "/work/preview.json"
```

Review the held queue, errors, file count and copy size. Repeat with `--apply`
and a new report path to create the staging catalog. Preview writes only an
explicitly requested report. Errors in proposed entries block the entire apply;
deliberately held entries are retained in reports and never imported.

The output is a **sparse catalog containing selected bundles**, with copied
platform headers and source policies. Packed source bundles are read logically
and become loose files in staging. Configuration, secrets, indexes and source
revision markers are excluded. Unknown YAML fields and existing variants are
preserved; duplicate/merged YAML keys and aliases require manual preparation.

`--media artwork` is the default: copy image files, omitting video and manuals.
Use `--media none` for a metadata-only check or `--media all` for every local
asset. The preview estimates the selected copy size before writing.

The helper validates staging with the same catalog validator before publishing
the output directory. A failed temporary stage remains available for diagnosis.
One identical `--apply` repeat verifies the saved receipt and file hashes and
returns `unchanged`. Changed or unrelated output directories are refused.

This helper writes only staging. Keep any eventual production merge separate,
coordinate it with catalog writers and recheck the current catalog before merging.
Do not replace newer production bundles with an older sparse staging copy.

## Relationship JSON

This is the helper's versioned input, not a core import command. One entry defines
one modified output identity and may list several input paths with the same
bytes. An entry with `dat` can instead use the published identity without inputs. Paths are relative to `--rom-root`, starting with the exact platform
folder. Both catalog SHA256 values describe the original source documents before
any entry in this plan is added.

The example hashes and size are placeholders; replace them with actual values:

```json
{
  "schema_version": 1,
  "entries": [
    {
      "platform": "Nintendo - Nintendo Entertainment System",
      "game": "Example Quest",
      "variant": {
        "key": "English 1.0",
        "title": "Translated Quest",
        "kind": "translation",
        "based_on": "Example Quest (Japan)",
        "languages": ["en"],
        "group": "Example team",
        "version": "1.0"
      },
      "file": {
        "name": "Translated Quest.nes",
        "size": 123456,
        "sha1": "0123456789abcdef0123456789abcdef01234567"
      },
      "evidence": {
        "relationship": "source-release-name",
        "links": [
          {
            "source": "Prepared source snapshot",
            "entry": "Translation record",
            "parent": "Example Quest (Japan)"
          }
        ]
      },
      "expected_documents": {
        "game.yaml": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        "fingerprints.yaml": "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
      },
      "inputs": [
        {
          "path": "Nintendo - Nintendo Entertainment System/translations/Renamed.zip",
          "member": "Renamed member.nes"
        }
      ]
    }
  ],
  "holds": [
    {"entry": "Unresolved record", "reason": "Exact base release unknown"}
  ]
}
```

Payloads currently supported by the helper are raw regular files and named ZIP
members, with a positive size and SHA1 and/or SHA256. Optional MD5 and CRC32 must
also agree. Omit `member` for a raw file. Optional input `size` and `mtime_ns`
bind the outer file's saved stat signature. Archives are read without extraction;
UTF-8 and legacy CP437 member names use the same decoder as catalog ZIPs and
export observations.
Published T-En `disk` records and optional standalone CHD inputs use the explicit
CHD representation above. Other disc formats and multi-payload sets need separate
preparation adapters; their hashes are not reinterpreted.

Top-level `snapshots` can pin local source files with `path`, `sha256` and
optional `url`; `path` is relative to `--sources`. Source files are checked
before preparation and again before publishing staging. Supplied source links
are copied into variant/fingerprint provenance; adapters should include snapshot
references and preserve their raw parent assertions. When a link declares
`snapshot` and `snapshot_sha256`, both must agree with a pinned snapshot.
A supplied variant title also becomes an explicit metadata-only name alias;
payload matching still requires the modified hash.

Allowed relationship evidence:

- `source-release-name`: the source names the parent. Optional
  `evidence.normalization` records explicit formatting handling. Provenance
  states that original ROM bytes are not proved by this name assertion.
- `source-base-hash`: a source link includes `base` with SHA1 and/or SHA256,
  optionally MD5/CRC32. Those hashes must agree with the exact official base
  release's fingerprint. This verifies the supplied source claim against the
  catalog; it does not read or reconstruct an unmodified ROM.
- `reviewed`: an explicit reviewed game or release relationship; preserve the
  decision's reference in its source link. Omit `based_on` for a game-only link.

Unknown top-level/entry fields and duplicate JSON keys fail. Names, parents,
strong hashes, input changes, conflicting ownership across selected platforms,
variant graphs and final catalog contents are checked. Source-specific extraction
and approval of relationships remain the adapter/reviewer's responsibility.

## Verify export

Use the ordinary CLI against staging and the selected ROM files:

```shell
rom-meta export --target es-de --platform nes --data-root "/work/variant-catalog" --rom-root "/roms/Nintendo - Nintendo Entertainment System" --out "/work/variant-export"
```

Review, then repeat with `--apply` into a new package. Inspect translated names,
launch paths, inherited descriptions and media, including provenance in
`manifest.json`. A metadata-only staging catalog intentionally has no media.
The existing [reader limits](export-scenarios.md#es-de-input-coverage) still apply;
preparing a relationship does not add a platform launch reader.
