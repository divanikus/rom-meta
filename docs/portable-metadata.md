# Portable Metadata

## Purpose

Portable metadata is the shared foundation for the ROM Manager family of tools.

The first goal is not to manage a specific ROM set. The first goal is to build a local, portable, human-readable platform library: games, releases, titles, regional variants, media, source references, and enough recognition data to later connect the library to ROM sets and local files.

## Source Of Truth

The source of truth is a file-based metadata repository, not a database.

Generated databases, search indexes, and frontend exports may be used for speed and convenience, but they must be disposable. A generated index should be rebuildable from the human-readable catalog files and local assets.

Preferred source-of-truth properties:

- Human-readable and hand-editable.
- Organized by platform.
- Organized by game bundle inside each platform.
- Independent from any one ROM set, frontend, emulator, or scraper.
- Friendly to validation, diffs, review, backup, and migration.
- Explicit about user curation decisions and source provenance when those decisions matter.

## Packed platform storage

Optional marked platforms with per-game metadata and source/type media TorrentZips. Logical paths
and source-policy rules below are unchanged; loose and packed platforms can coexist.
See the [storage guide](catalog-storage.md) for markers, editing and API behavior.

## Layout

The logical layout below is also the physical layout of legacy unmarked
platforms. In marked platforms, the files inside each game folder are ZIP members
with the same game-relative paths.

```text
platforms/
  Nintendo - Nintendo 64/
    _catalog.yaml
    _platform.yaml

    Star Fox 64/
      game.yaml
      fingerprints.yaml
      media/
        screenshot-01.png
        box-front.usa.openvgdb.jpg
        gameplay-video.usa.emumovies.mp4
        Star Fox 64 (USA)/
          box-front.png
          logo.png
        Lylat Wars (Europe)/
          box-front.png
          logo.png

    Jet Force Gemini/
      game.yaml
      media/
        Jet Force Gemini (USA)/
          box-front.png
        Star Twins (Japan)/
          box-front.png

generated/
  indexes/
  exports/

cache/
  sources/

source-policies.yaml

bindings/
  romsets/
  inventories/
```

Notes:

- `platforms/<platform>/` is the portable, user-facing platform.
- `platforms/<platform>/<game>/` is the per-game bundle.
- There is no extra `entries/` layer unless a later need appears.
- `_catalog.yaml` and `_platform.yaml` are reserved for platform-level metadata and catalog defaults.
- `generated/` is disposable output.
- `cache/` is for raw external downloads or source snapshots that can be rebuilt or refreshed.
- Source-specific media stays near the media it describes and uses a source suffix before the extension, such as `box-front.openvgdb.jpg` or `gameplay-video.emumovies.mp4`.
- `source-policies.yaml` records source-level redistribution defaults and exceptions for the local media tree.
- `bindings/` is reserved for later ROM-set and local-inventory mapping. It is not part of the first catalog MVP.

## Convention Over Configuration

The catalog should prefer layout and naming conventions over verbose YAML.

YAML should record intent, decisions, and exceptions. It should not list every media path or repeat identity that can be derived from paths and keys.

Filesystem naming:

- Platform folder names should use the selected authority system name, with only filesystem-invalid characters sanitized. For example, No-Intro `Nintendo - Nintendo 64` stays `Nintendo - Nintendo 64`.
- Game folder names should use the selected catalog display title, usually based on the preferred authority release title for the folded game, again with only filesystem-invalid characters sanitized.
- DAT-derived optical bundles partition explicit content roles and retain unknown edition qualifiers. Trial folders include their role and dated build discriminator when present. Ordinary regions, languages and retail revisions remain release metadata. See [regional content identity](regional-content-identity.md) and ADR 0086; earlier permissive role/edition folding is superseded.
- Release keys should use the selected authority release entry name, with only filesystem-invalid characters sanitized. For example, No-Intro `Star Fox 64 (USA)` stays `Star Fox 64 (USA)`.
- MAME set names are already compact stable identifiers and can be used directly where they are the authority key.
- Path sanitization should preserve case, spaces, parentheses, commas, apostrophes, and ordinary hyphens. It should only replace or remove path separators, control characters, characters invalid on common filesystems, and unsafe edge cases such as trailing dots or spaces on Windows.

Derived identity:

- Platform ID comes from `platforms/<platform>/`.
- Game ID comes from `platforms/<platform>/<game>/`, for example `Nintendo - Nintendo 64.Star Fox 64`.
- Release IDs come from keys in `game.yaml`.
- Game-wide media comes from `media/`.
- Curated release-specific media comes from `media/<release-id>/` when that release actually has release-specific media.
- Source-specific candidate media comes from the same game-wide or release-specific media folders, with the source encoded as a filename suffix: `media/<release-id>/box-front.openvgdb.jpg`.
- Fold preserves distinct same-name files under `media/_fold/<original-card-folder>/`, retaining their basenames and original game-wide or release-specific layout. `_fold` is a reserved preservation namespace, not a release ID. Repeated folds can preserve an earlier `_fold` subtree inside another original-card folder; all files still follow the normal filename and source-policy checks.
- Empty media directories should not be created. Absence of `media/` or `media/<release-id>/` means no media has been attached at that level yet.
- Source-level media policies come from root `source-policies.yaml`.

Validation should catch unknown release media folders, unknown release keys, unknown media type filenames, source suffixes missing from `source-policies.yaml`, malformed release keys, missing required fields, and ambiguous conventions.

Dates:

- Source importers should normalize release dates to ISO-style calendar strings before writing `game.yaml`.
- Full dates use `YYYY-MM-DD`.
- Month-only dates use `YYYY-MM`.
- Year-only dates use `YYYY`.
- Importers should not invent missing precision, such as turning a year-only source date into January 1.

## Game And Release Model

A game bundle represents one folded library item: the thing a user should usually see as one game card.

A release represents a regional, language, title, build or revision-level form of
one library game. Confirmed demos/betas/prototypes stay with their game.
Reviewed same-game expanded editions may also share the card while retaining
their own provider identities and media (ADR 0100). Independent games, separate
supplements and compilations with different contents remain separate. Grouping
is a curation decision; changed features or provider IDs alone do not establish
different games. Regional names such as `Contra`, `Gryzor`, and `Probotector` can
therefore remain releases of one game.

Minimal direction for `game.yaml`:

```yaml
title: Star Fox 64
default_release: "Star Fox 64 (USA)"

releases:
  "Star Fox 64 (USA)":
    title: Star Fox 64
    regions: [US]
    languages: [en]
    authority:
      source: no-intro
      entry_name: Star Fox 64 (USA)

  "Lylat Wars (Europe)":
    title: Lylat Wars
    regions: [EU]
    languages: [en]
    authority:
      source: no-intro
      entry_name: Lylat Wars (Europe)

  "Star Fox 64 (Japan)":
    title: Star Fox 64
    regions: [JP]
    languages: [ja]
    authority:
      source: no-intro
      entry_name: Star Fox 64 (Japan)

sources:
  openvgdb:
    matches:
      - release: "Star Fox 64 (USA)"
        title: "Star Fox 64"
        region: "USA"
        genres: ["Action", "Shooter", "Rail"]
```

Rules:

- The top-level `title` is the platform display title, not an objective universal title.
- Official regional titles belong on releases.
- Aliases should be added only when a useful recognition title is not already present as the display title or a release title.
- Source-specific candidates live under `sources.<source>` in the same `game.yaml`; they preserve provenance but do not automatically become canonical fields.
- Aggressive auto-folding is out of scope for the first MVP.
- Explicitly known same-game regional titles should be supported from the beginning.

## Derived Variants

Fan translations, ROM hacks, restorations, bugfixes, and similar derived works use
an optional `variants` mapping inside the original game bundle. They are not
official releases and should not become unrelated game folders.

`variant` is the common entity; its `kind` field distinguishes `translation`,
`hack`, `restoration`, `bugfix`, `mod`, and future categories. A linked variant has
a `based_on` reference to an official release or another variant in the same game
bundle. See ADR 0040 for the complete compatibility and fingerprint rules.

Manual fold corrections live beside the game they correct:

```yaml
fold:
  ignore_evidence:
    - "screenscraper.screenscraper_id=752"
```

`fold.ignore_evidence` disables only the listed source identity as folding evidence for that game. It is meant for reviewed source metadata mistakes, not for hiding metadata from browsing or export.

`fold.content_key` is an opaque reviewed content identity written by an explicit
[content split](reviewed-content-splits.md). Different nonempty keys prevent a
future fold across that boundary; equal keys do not establish identity. Existing
fold metadata survives repeat DAT imports.

## Titles

There is no single objective canonical title for every folded game.

The catalog display title is a practical curation choice for the platform catalog. It should generally prefer the most recognizable English or international title when that is useful for the user, but official regional titles must remain available on releases.

When title choice is non-obvious, record a short note or provenance field rather than encoding too much policy into the schema.

## Media

Media should be stored by convention.

Game-wide media:

```text
media/screenshot-01.png
media/gameplay-01.png
```

Release-specific media:

```text
media/Star Fox 64 (USA)/box-front.png
media/Star Fox 64 (USA)/logo.png
media/Lylat Wars (Europe)/box-front.png
```

Source-specific media:

```text
media/box-front.launchbox.jpg
media/logo.launchbox.png
media/gameplay-video.usa.emumovies.mp4
media/box-front.usa.openvgdb.jpg
```

Initial media type names should stay small and predictable. Image media may use `.png`, `.jpg`, `.jpeg`, or `.webp`; examples use `.png` unless a source download naturally provides another extension.

- `box-front.<image>`
- `box-back.<image>`
- `box-spine.<image>`
- `box-full.<image>`
- `box-3d.<image>`
- `logo.<image>`
- `banner.<image>`
- `screenshot-01.<image>`
- `gameplay-01.<image>`
- `title-screen.<image>`
- `fanart.<image>`
- `physical-media.<image>`
- `manual.pdf`
- `poster.<image>`
- `flyer-front.<image>`
- `video.mp4`

Source-specific candidate files add a source suffix before the extension:

```text
box-front.openvgdb.jpg
gameplay-video.usa.emumovies.mp4
```

When a source provides multiple distinct candidates for the same media type, number them from the first candidate:

```text
box-front-01.launchbox.jpg
box-front-02.launchbox.jpg
logo-01.launchbox.png
logo-02.launchbox.png
```

If every release candidate from the same source points to the literally same source media URL for a media type, the downloader should materialize it once as game-wide media. If multiple files differ only because one source has several candidates for one release, they should be numbered in the game-wide media folder. If media differs only by ordinary release region or language, keep it game-wide and encode the region or language in the filename. The downloader should use `media/<release-id>/` only when the media is tied to a real release-specific variant such as a special edition, prototype, demo, unlicensed/pirate/aftermarket/homebrew/sample release, or a release whose base title differs from the game bundle.

An explicit source artwork category is preserved as a `variant-<value>`
qualifier immediately before the source suffix:

```text
box-front-01.usa.variant-original.launchbox.jpg
box-front-02.usa.variant-reconstructed.launchbox.png
box-front-03.usa.variant-fanart.launchbox.jpg
```

The existing role/ordinal, region, language, quality and source positions are
unchanged after removing that qualifier. LaunchBox's exact Box - Front/Back,
reconstructed and fanart categories map to these values. Other or conflicting
categories remain unqualified/unknown; neither a provider name nor PNG/JPG
implies a variant. This is a source label, not image analysis or a guarantee of
case-free pixels. Manual files may use the same qualifier and `local` source.

Export selects variants by filename. Legacy unqualified names stay eligible as
unknown. `catalog media-names` can preview/apply an offline naming upgrade
from saved downloader candidate order; see [offline export](offline-export.md).
Existing legacy files are recognized by fetch and are not downloaded again just
because the naming convention changed. Exact-path curation follows renamed paths.

Exporter policy decides variant, region, source and folder priorities. Ordinal
`-01` breaks equal-priority ties.

Generated media should not become source metadata. Files such as mix images, thumbnails, resized covers, transcoded videos, Steam-style grids, or frontend-specific tiles belong under generated export output unless there is a deliberate reason to curate them as first-class local assets.

For arcade and cabinet-oriented targets, the catalog should leave room for later media names such as `arcade-marquee.png`, `screen-marquee.png`, `bezel-4x3.png`, `bezel-16x9.png`, `control-panel.png`, `cabinet-left.png`, and `cabinet-right.png`. These should be added when the first arcade/MAME examples need them rather than forced into the first N64 MVP.

## Media Provenance And Rights

Media files need enough provenance and redistribution status to split a local library into shareable and personal parts, but this should mostly be convention plus one root source policy file.

Default convention:

- `media/<release-id>/` is curated release media by convention and is materialized only when files exist for that release.
- Source-specific media is materialized in the same game-wide or release-specific folder as the curated media, but with a source suffix before the extension. Source is the suffix, for example `emumovies` in `gameplay-video.emumovies.mp4`.
- `source-policies.yaml` records whether source-specific media can be redistributed.
- `generated/` is disposable output and inherits the most restrictive input rights.
- Source-media downloaders should create or update root `source-policies.yaml` when they materialize source-specific media.

For EmuMovies, the normal path is:

```text
platforms/Nintendo - Nintendo 64/Star Fox 64/media/gameplay-video.usa.emumovies.mp4
```

This is enough to infer:

- source: `emumovies`
- platform: `Nintendo - Nintendo 64`
- game: `Star Fox 64`
- region: `USA`
- media type: `gameplay-video`

The root source policy file carries source-level sharing rules:

```yaml
schema_version: 0

default:
  media:
    redistributable: false
    scope: review

sources:
  emumovies:
    media:
      redistributable: false
      scope: personal
      license: emumovies-member-terms
```

Share/package commands should ignore or reject non-redistributable source media by default. Local export commands may opt into personal source media for the user's own devices.

## Exports And Consumers

Frontend and library outputs are generated projections from the portable catalog, local assets, and later ROM bindings. They are not the portable metadata source of truth.

Every target should be implemented as an adapter with an explicit profile:

- Input: catalog game/release metadata, local media, recognition results, and concrete ROM binding when the target needs filenames.
- Output: a target-specific staging directory, manifest, and report.
- Apply: optional and explicit, especially when writing into an existing frontend installation.

Known target categories:

- Metadata-rich file frontends, such as ES-DE, need metadata files plus target media folders. ES-DE is a good early bridge because `gamelist.xml` is widely understood, but it is not the canonical model.
- Desktop library frontends, such as LaunchBox, need careful adapter work because they have rich metadata and media categories. The first safe shape should be a staged import or update package, not direct mutation of a user's LaunchBox install.
- Handheld frontends such as muOS may consume a compact catalogue of box art, previews, splash images, and text alongside a device-specific ROM layout. Minimal targets such as MinUI may ignore most metadata and media; for them export may mostly mean ROM layout, display-safe filenames, hidden files, and collection subsets.
- Self-hosted library managers, such as RomM, Retrom, and Gaseous, are useful compatibility targets and stress tests. They should consume generated outputs or target-native adapters rather than replacing the portable catalog.

Exporters should be able to ignore unsupported fields without losing them from the catalog. A muOS or MinUI export should not delete unsupported metadata or media. An ES-DE export should not force ES-DE media naming into source YAML. A LaunchBox export should not make LaunchBox IDs mandatory for every game.

## Recognition

Recognition maps an observed thing to a catalog game and possibly a release.

Early recognition input might be:

```yaml
platform: Nintendo - Nintendo 64
title: Lylat Wars
region: EU
source: launchbox
```

Expected result:

```yaml
game_id: Nintendo - Nintendo 64.Star Fox 64
release_id: Lylat Wars (Europe)
confidence: high
reason:
  - exact release title match
  - platform match
  - region match
```

Recognition order should be explainable:

1. Exact external source ID match.
2. Known fingerprint match.
3. Known source or DAT entry name match.
4. Exact display title, release title, or alias match within platform.
5. Title plus region, year, language, publisher, or developer hints.
6. Fuzzy title candidates for human review.
7. Manual decision recorded in the source-of-truth catalog.

Fuzzy matching should propose candidates, not silently create permanent folds.

## Fingerprints

Known hashes should be supported as optional recognition data, but they are not primary game metadata.

Fingerprints describe concrete file or dump representations. They may be imported from DAT files, preservation databases, or other source manifests.

The first convention is to keep fingerprints next to `game.yaml` and point each official fingerprint at a release key. A future derived-variant fingerprint uses `variant` instead; one fingerprint item cannot identify both:

```text
platforms/Nintendo - Nintendo 64/Star Fox 64/fingerprints.yaml
```

Working shape:

```yaml
items:
  - id: no-intro.star-fox-64.usa
    release: "Star Fox 64 (USA)"
    source:
      kind: dat
      name: No-Intro
      system: Nintendo - Nintendo 64
      entry_name: Star Fox 64 (USA)
    file:
      name: Star Fox 64 (USA).z64
      size: 12582912
      crc32: "00000000"
      md5: "00000000000000000000000000000000"
      sha1: "0000000000000000000000000000000000000000"
    dump:
      status: verified
      format: z64
```

For systems with multiple file layouts, the hash context must be explicit. Nintendo 64 is an example because `.z64`, `.v64`, and `.n64` layouts can represent the same ROM content differently.

No-Intro, Redump, and MAME are the primary authorities for platform and release naming where they apply. Hash entries can still include additional source IDs, alternative names, dump notes, and derived hashes when useful for recognition.

## Naming Authorities

No-Intro, Redump, and MAME are the preferred source of truth for platform and release naming where their catalogs cover the system:

- Use No-Intro naming for cartridge, digital, and similar systems covered by No-Intro.
- Use Redump naming for optical disc systems covered by Redump.
- Use MAME naming for arcade systems and MAME-managed arcade sets.
- Prefer their platform/system names and release entry names when deriving stable platform folder names, release media folder names, release IDs, source references, and fingerprint source records. Only sanitize characters that are unsafe for paths.
- For No-Intro-style release names, initial game grouping should treat trailing filename fields such as `(Region)`, `(Languages)`, `(Version)`, `(Devstatus)`, `(Additional)`, `(Special)`, `(License)`, and `[Status]` as release qualifiers rather than game-title text.
- A small set of release-class qualifiers can still be reflected in DAT-derived game folder names for human browsing, while country/region qualifiers should not be used as folder discriminators.
- For No-Intro DATs, `cloneofid` relationships should be used as stronger initial grouping evidence than title similarity. Parent/clone groups can fold regional titles whose base title differs before the filename fields.
- Keep the user-facing game display title as a catalog curation choice.
- Store additional official titles, regional titles, scraper titles, frontend titles, and aliases as metadata, not as competing filesystem layouts.
- Store known hashes as recognition/fingerprint data, with source provenance and explicit hash context.

If No-Intro, Redump, or MAME do not cover a platform well, record the chosen naming authority in the platform catalog or a later ADR before building a full catalog for that platform.

## ROM Sets And Inventories

The first portable metadata catalog should not be tied to a ROM set.

ROM-set bindings and local file inventories are later layers that connect observed files, DAT entries, archive members, checksums, and local paths to catalog games and releases.

Future binding examples:

- No-Intro entry to release.
- Redump disc to release.
- Local server file to known fingerprint.
- Frontend-specific export item to preferred release.

Private local paths and inventories should remain outside committed public metadata unless deliberately stored in a private location.

## Tooling Direction

The first implementation should be a Go CLI-first core.

CLI is expected to cover most early workflows:

```text
rom-meta validate
rom-meta build-index
rom-meta recognize --platform n64 --title "Lylat Wars" --region EU
rom-meta import-source <source> --platform n64
rom-meta report
rom-meta export --target <target> --platform n64
```

Desktop and web interfaces may become separate applications later. They should consume the same file-based catalogs and generated indexes rather than becoming a separate source of truth.

The early Go package boundaries should stay boring and workflow-shaped:

```text
cmd/rom-meta/
internal/catalog/
internal/recognition/
internal/sources/
internal/media/
internal/export/
```

## MAME And Arcade

MAME is the primary arcade set/description and technical source authority.
The implemented layout is `platforms/Arcade/<readable game>/`, with the existing
`game.yaml` and `fingerprints.yaml` files. Logical platform membership is
explicit in `game.yaml`:

```yaml
arcade:
  platform: Arcade - SNK - Neo Geo
  kind: hardware
  naming_evidence: "Reviewed MAME hardware mapping"
```

Each release separately retains `mame.hardware`, plus snapshot and dependency
evidence. Other/mixed classifications use `kind: collection`. Families partition
folding even though all cards share storage. Versioned raw inputs/routing and set
bindings live under `bindings/romsets/mame/<snapshot>/`. Bindings distinguish
physical `platform: Arcade` from logical `family`. Provider-specific aliases and
future exports must preserve that distinction. See [the workflow](arcade-import.md)
for the shared physical layout.

## Source Edition Relationships

A source match may contain relationships to other records in the same source:

```yaml
relationships:
  - type: expanded_edition_of
    target_id: "100"
    source_field: parent_game
```

Keep the original source ID and all release-specific metadata. The relationship
is evidence for grouping, not a replacement identity. edition_of,
expanded_edition_of and port_of can propose a same-platform fold; remakes,
remasters, expansions, DLC and generic related_to links remain separate.
An empty list means the source relationship lookup completed without a parent.
See `fetch --help` / `catalog fold --help` for refresh and review.

Reviewed subgroups and explicit full-game primary eligibility can be applied with
[reviewed fold plans](reviewed-fold-plans.md); this does not add catalog fields.

### Game cards and development releases

A card can include retail, regional demos, promotional builds, betas/prototypes
and their dated builds when parent game identity is confirmed. Release
names, tags, fingerprints and scoped source/media evidence remain distinct. A
release-stage difference does not establish another game. Ordinary defaults prefer
retail; explicitly selecting an early build retains that exact release. Multi-game
compilation products, addons and content-different commercial editions need their
own identity decisions. Reviewed former stage keys can be retired only through
the hash-bound `development_parent` fold described in reviewed-fold-plans.md.

## Reviewed foreign provider references

`manual.provider_references.igdb` is a list of curated Hasheous-reference decisions:
`source: hasheous`, `from_id`, `action: suppress|replace`, optional replacement
`to_id`, `reason` and public HTTPS `evidence` URLs. Keep raw source IDs/signatures.
This is card-level game identity, never an exact-release verification claim.
Own provider snapshots still own their identity; contradictory own IDs remain
errors. The [reviewed link workflow](igdb-links.md) records minimal official
identity snapshots separately from descriptions/ratings/media.
