# Import existing gamelists

Harvest private ES-DE/EmulationStation metadata and explicitly referenced local
images, MP4 videos and PDF manuals into an existing catalog:

```shell
rom-meta gamelist import /path/to/gamelist.xml --data-root /path/to/catalog --collection personal --language ru --report /path/to/preview.json
rom-meta gamelist import /path/to/gamelist.xml --data-root /path/to/catalog --collection personal --language ru --report /path/to/import.json --apply
```

Use `--platform <name>` when the gamelist's `provider.system` is absent or does
not resolve through the platform registry. ZIP packs are supported directly;
select `--member system/gamelist.xml` if there are multiple gamelists. The ZIP
reader opens XML and referenced media members, without extracting ROM members.

`--collection` is a lowercase identifier (letters, digits, hyphen or underscore).
It names the source `gamelist.<collection>` in metadata, media filenames and the
root source policy. The technical adapter is recorded as `importer: es-de`.
Description language is explicit and independent of the gamelist's ROM language.

Complete ROM basenames, release keys, authority names and explicit release aliases
are tried first on the selected platform. A guarded legacy fallback recognizes
region notation such as `(U)`, `(E)` and `(UE)`, plus the `[!]` dump marker. It
preserves the complete title, punctuation, region set, revision, edition and
unknown qualifiers. Catalog language-only and `GB Compatible` annotations may be
absent in an older list; if that leaves multiple releases, the entry stays ambiguous.
Explicit source language annotations remain constraints. Translation/hack paths and
altered-dump flags do not use this fallback. No guessed aliases are written.
Unmatched and ambiguous entries remain in the private JSON report.
Frontend IDs and reported MD5 values remain source observations; they do not
create provider IDs, releases, fingerprints, aliases or fold evidence.

Add `--match-md5` to try source-reported MD5 before names. Only existing
single-file catalog fingerprints on the selected platform are eligible; component
and multi-file set hashes are excluded. Unique exact names can disambiguate equal
hash targets. Name/hash conflicts, known derived variants and variant/mod/translation-labelled
paths requiring a parent decision remain in the report. An unmatched hash falls
back to names: some lists hash the complete ZIP, others the contained ROM.
Use repeated `--md5-review-path <relative-path-or-prefix>` flags for sections
known to retain unreliable hashes, for example `--md5-review-path review`.
Matching those paths stays in review rather than silently accepting a base hash.
The association retains `reported-md5` provenance and does not verify a ROM or
create a fingerprint. Reports expose hash methods and review status. Ordinary
name/MD5 association opens no ROM data.

For an NES list reporting older iNES headers, optionally add
`--nes-rom-root <existing-local-nes-directory>` together with `--match-md5`.
This scans caller-provided raw `.nes` and single-ROM ZIP files. A local file must
first match an eligible registered whole-file MD5 and size. The importer then
computes an older header representation in memory and records
`reported-md5-legacy-nes-header` evidence. Extended mapper/size and nonstandard
console fields are skipped; collisions, variants and derivative paths keep their
review rules. The comparison is a metadata association, not a new verified dump
or a claim that header hardware fields are equivalent. Source-reported MD5 stays
unchanged. No ROM, fingerprint or alias is written, downloaded or converted.
The existing [iNES](https://www.nesdev.org/wiki/INES) and
[NES 2.0](https://www.nesdev.org/wiki/NES_2.0) header layouts explain why full-file
checksums can differ while the ROM payload is the same.

Existing canonical fields, other provider stanzas and unrelated fields survive.
Repeated observations use the same source-file/path identity; `--origin` supplies
a stable relative identifier if inputs are moved or renamed. Imports from
different gamelists in the same collection coexist. Repeating an unchanged import
writes no catalog data.

Media tags have explicit adapter defaults:

| XML tag | Default catalog role |
| --- | --- |
| `image` | Screenshot; a `boxart`/`covers` directory selects box front |
| `thumbnail` | Box front |
| `marquee` | Logo |
| `video` | Gameplay video (MP4) |
| `fanart` | Fanart image |
| `manual` | Manual (PDF) |

These are adapter conventions; a frontend list can use different meanings.
Override them with repeated `--media-role <tag=role>` flags, for example
`--media-role thumbnail=screenshot --media-role image=ignore`. Supported roles
are `screenshot`, `box-front`, `logo`, `gameplay-video`, `fanart`, `manual` and
`ignore`. Role changes/removals retire only this importer's references in the
current observation. Owner candidates survive. A missing or unsupported file
keeps an earlier successful reference while reporting the gap.

Referenced media uses readable source-labelled names inside the matched release's
media folder: `logo.gamelist.personal.png`, `gameplay-video.gamelist.personal.mp4`
and `screenshot.gamelist.personal.png`. SHA-256 and original paths stay in YAML;
unknown region/language placeholders and checksum suffixes are omitted. Multiple
distinct files for a role/release/collection receive ordinal suffixes such as
`logo-01.gamelist.personal.png` and `logo-02.gamelist.personal.jpg`. Ordinals span
formats; an already published readable name stays stable as alternatives arrive.
Retained older content also counts as a distinct variant. Images, thumbnails,
marquees, videos, fanart and manuals retain their source paths and checksums. Missing files are reported;
available metadata/media can still be applied, with an incomplete exit status.
Unsupported formats, including fanart playlists, are reported as warnings.
Absolute paths, traversal and source symlink escapes are rejected. Media outside
the imported XML's directory tree, remote URLs, and modern frontend media located
only through filename inference require a separate adapter.

After replacement, automatic browser/export/API selection uses current
references across all observations in the collection. Older media
files remain available for recovery but are not automatic candidates once
unreferenced. Another observation/language/collection can retain them; an explicit
export `media_overrides` pin may also select a retained file intentionally.

The default source policy is `redistribution: private`. Keep input packs, media,
reports and `.rommeta/imports/` outside Git. Applied imports retain original YAML
and a receipt there; failed publication retains its prepared stage. Recover
selected original YAML through ordinary catalog tools if needed. Catalog writers
must run sequentially.

To select the imported curation in an export profile, use its full source name:

```yaml
schema_version: 1
target: es-de
selection:
  description:
    languages: [ru, en]
    sources: [gamelist.personal, screenscraper, launchbox]
  media:
    cover:
      sources: [gamelist.personal, launchbox, screenscraper]
    video:
      sources: [gamelist.personal, emumovies, screenscraper]
```

Importing does not change an existing export profile or installed frontend.
Review the report before applying and use the ordinary export preview afterward.

## Rename previously imported media

Preview and apply the same naming rules without the original packs:

```shell
rom-meta gamelist media-names --data-root /path/to/catalog --collection personal --report /path/to/names-preview.json
rom-meta gamelist media-names --data-root /path/to/catalog --collection personal --report /path/to/names-applied.json --apply
```

An optional `--platform` limits this operation to one exact catalog folder.
Run catalog writers sequentially. The migration verifies content, handles loose
and packed storage, updates game-relative/catalog-relative YAML references and
keeps original `source_file`/`source_path` provenance. Other source namespaces remain
unchanged. Identical content in the same role/format can share one destination.
Original YAML and a rename receipt stay under `.rommeta/imports`; the private report
includes the filename/checksum inventory. Repeating after completion changes
nothing. Review external export-profile pins against the rename report, because
profiles outside the catalog cannot be discovered automatically.
