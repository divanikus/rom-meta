# Prepared translations and other derived variants

A prepared catalog can recognize translated or modified files through their own
fingerprints and export the original game's metadata/media. Keep a derivative
inside the original game bundle. The external preparation tool or user supplies
the relationship; RomMeta does not search for a parent from an unfamiliar title.

## Catalog records

Use optional `variants` in `game.yaml`, separate from official `releases`:

```yaml
title: Example Quest
default_release: Example Quest (USA)
releases:
  Example Quest (USA):
    title: Example Quest
    regions: [USA]
  Example Quest (Japan):
    title: Example Quest Japan
    regions: [Japan]
    languages: [ja]

variants:
  English 1.0:
    title: Example Quest Translated
    kind: translation
    based_on: Example Quest (Japan)
    languages: [en]
    group: Example Translation Team
    version: '1.0'
    source:
      name: prepared-map
      entry_name: Source translation record
      relationship: reviewed-source-parent
```

Each variant requires a nonempty `kind`. If the exact original release is unknown,
omit `based_on`: the containing bundle supplies the original-game relationship.
For example, this translation needs no original disc revision or patch version:

```yaml
variants:
  English translation:
    title: Example Quest Translated
    kind: translation
    languages: [en]
```

When known, `based_on` can name an exact official release or another variant in
the same bundle. A chain may end at an official release or a game-only variant.
A supplied `based_on` must be a nonempty string; omit it instead of using null or
an empty value. Missing named references, cycles and overlapping release/variant
keys are errors.
Variant keys must be safe catalog/media folder names. Unknown provenance fields
are preserved. Default release selection stays within official releases.

In `fingerprints.yaml`, each modified artifact references `variant`; each
official artifact continues to reference `release`. Never put both on one item
or attach a patched hash to an official release:

```yaml
items:
  - variant: English 1.0
    source:
      name: prepared-map
      entry_name: Source translation record
    file:
      name: Example Quest Translated.nes
      size: 123456
      sha1: '0123456789abcdef0123456789abcdef01234567'
```

The size/hash above is illustrative; use the actual modified payload's values.
Raw or ZIP payload fingerprints need a positive size and SHA1 or SHA256.
Optional MD5/CRC32 constraints must also agree. ZIP hashes describe extracted
game members, not the archive. Recognition survives file/member renaming.

## CHD identities

For a prepared translated CHD, declare the representation explicitly:

```yaml
items:
  - variant: English 1.0
    file:
      kind: chd-combined-sha1
      name: Example Quest Translated.chd
      sha1: '0123456789abcdef0123456789abcdef01234567'
```

This is the declared CHD v5 combined SHA1 at header bytes 84..103, not the raw-data
SHA1, compressed-file SHA1 or a Redump track digest. Optional `size` is the
declared logical size, not compressed file length. Do not put raw/file MD5,
SHA256 or CRC32 on this representation.

Standalone translated CHDs can resolve by this identity. The manifest labels
the result `variant-chd-header` and retains the actual observation; it makes no
decompression, payload-integrity or Redump verification claim. Existing M3U
exports can combine numbered discs from one prepared translation family; mixed
patch versions or official/variant combinations stay unsupported. Normal ES-DE CHD
export also retains its existing compressed-file observation. Metadata lookup
from header/stat observations requires explicit `game_binding: true`.

## Inheritance and display

When a chain names an exact terminal release, that release selects existing
provider metadata and media using the export profile. A different `default_release`
is not substituted. Without an exact terminal release, export uses ordinary game
metadata selection: a valid `default_release`, or the conventional default when
none is configured. This selects metadata, not the historical disc that was patched.

Description, original release date, developer, publisher, genres and players
inherit through this selection; a variant's explicitly supplied fields override
it. Parent variant overrides apply before child overrides. The translated payload
keeps its own fingerprints regardless of whether the exact base is known.

The translated title and declared language remain separate from the base region
and language. Translator/group and modification version are credits; they do not
replace original developer or release date. If a translation date is retained in
source evidence, do not write it as the original game's `release_date`.

Eligible media in `media/<variant-key>/` overrides inherited media of the same
role. Parent variant media can supply a fallback before the official base.
No base media needs duplication in the catalog. Generated assets use the actual
launch path, including renamed files and subfolders.

Export records expose `variant` and `variant_info` separately from the official
`release` field. `metadata_release` identifies the selected metadata release;
for a game-only relationship it is the game's metadata default, not an asserted
original input. In that case inherited fields/media use `selection: game` and
retain the actual `from_release` when applicable. Exact-base inheritance retains
`selection: base`; derivative overrides use `from_variant`. An absent
`variant_info.based_on` means that variant is linked only to the game. Variant info retains
ancestor modification credits for chained derivatives. Language/title formatting is
the existing `titles` policy. Its `variants` option controls display
disambiguation, not the catalog's derivative model.

## Workflow and limits

The separate [preparation helper](prepare-variants.md) can turn explicit reviewed
relationships into a new sparse catalog, with payload/digest checks, a held queue
and an identical no-op repeat. It leaves the source catalog untouched.

Prepare the explicit relationship and modified fingerprints outside the source
tree, then validate and preview with ordinary commands:

```shell
rom-meta validate "/path/to/catalog"
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/games/roms/nes" --out "/exports/nes-variants"
```

Review the result, then repeat export with `--apply` to a new package. No dedicated
parent-search or variant-DAT importer is provided. Source-specific naming repairs
and ambiguous parents remain preparation/review work.

Prepared variants use the existing supported ROM readers; this does not add
general RVZ/7z/track readers. Exact named catalog observations can provide weaker
metadata-only association with `game_binding: true`; the response preserves that
evidence level. An unknown translated payload must not silently become a retail
release merely because its name resembles one. Ordinary payload export requires
the modified fingerprint; an exact variant filename does not bypass a hash mismatch.

Lookup indexes/cache summaries include variants and rebuild old derived entries.
Normal catalog writers refresh indexes/revision markers. After external YAML/ZIP
edits, follow the existing [catalog update rules](catalog-api.md#automatic-updates).
Folding preserves variant blocks and bases and holds conflicting definitions.
Reviewed content splitting still refuses bundles containing variants.

Local and network export share the resolver. Use matching current client/server
builds and consumers that retain the variant/proof fields. Native frontend
installation, source-link discovery and automatic production catalog import are
separate workflows. Validate actual frontend launch/media behavior independently.
