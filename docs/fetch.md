# Automatic fetch source selection

Set up accounts, keys and secret/env mappings with [source setup](source-setup.md).

Omit `--source` to use `fetch.metadata`, `fetch.artwork` and `fetch.video` from
`config/sources.yaml`, in that order. `--type` selects lists. Unlisted sources are
not added; legacy `enabled` does not control selection.

```powershell
& $RomMeta fetch --data-root $DataRoot --platform scummvm
& $RomMeta fetch --data-root $DataRoot --platform scummvm --apply
```

Automatic selection requires a source mapping in the embedded registry or
`config/platforms.yaml`; ScreenScraper can use its registered system ID. Display
names and another provider's names are insufficient. Missing mappings print a
skip before any provider call or `not_found` write. This means RomMeta lacks a
mapping, not that the upstream source has no game.

Hasheous instead checks pending cards for valid whole-ROM hashes or MAME set
identity plus matching component SHA1. Without evidence, cards are skipped without
consuming `--limit`, even with `--force-overwrite`; queryable cards still proceed.
This local scan may update the disposable catalog cache.

Without `--platform`, selection runs per actual platform directory in source
order. Exact directories precede unique aliases; unknown/ambiguous filters fail.
Empty/all-skipped runs succeed with an explanation. `--limit` is per source/type/
platform task where supported. Credentials, rate limits and quotas still apply;
missing credentials are errors.

Applied searches with no accepted match save `not_found`; automatic skips preserve
existing results. Explicit `--source` uses manual connector matching and bypasses
automatic applicability skips.

## Add a reviewed mapping

Add verified provider names/IDs in `<data-root>/config/platforms.yaml`:

```yaml
"My Catalog Platform":
  aliases: [my-system]
  sources:
    launchbox: "Actual LaunchBox Platform"
    screenscraper: { id: 999, name: "Actual ScreenScraper System" }
```

Replace illustrative values with actual source identities. Overrides merge into
the embedded registry; the source must still appear in `fetch.<type>`.

ScummVM maps LaunchBox, ScreenScraper (123) and EmuMovies. IGDB/OpenVGDB lack a
dedicated mapping; list-created cards normally lack Hasheous evidence. See the
[ScummVM workflow](scummvm.md) and `rom-meta fetch --help`.

## Reviewed identity-only IGDB links

For known foreign IDs that need official platform/slug confirmation or a reviewed
replacement/suppression, use [the scoped link workflow](igdb-links.md),
`fetch --refresh-links --link-review <JSON>`. It does not rescrape full metadata
or media and does not use global `--force-overwrite`.

## EmuMovies video directories

Video requires an exact normalized platform name or explicit alias before choosing
HD, HQ, then SQ. Shared affixes do not establish identity: Xbox 360 cannot supply
Xbox, nor Game Boy Color supply Game Boy. Missing directories skip; failed FTP
listings error. PS3 Retail is an explicit alias; PSN/hacks are not implicit fallbacks.

Mappings use the registry and `config/platforms.yaml`. Quality preference does
not guarantee coverage. Changing a mapping does not validate/replace earlier
files; review saved paths and make a backed-up scoped replacement when needed.

Name matching retains edition, hardware and unknown qualifiers, stripping only
recognized region/language, revision/version and disc attributes from the base.
Generic aliases cannot erase qualifiers or cross editions. Known source regions
need compatible releases; regional preference cannot retarget an edition.
Ambiguous/incompatible candidates skip.

Partial fetch appends matches only for successful new downloads, preserving prior
matches, custom fields and recovery evidence. Failed/zero downloads add no match;
existing files resume by presence. These rules do not retroactively verify old media.

## Primary spellings and saved negative results

LaunchBox tries exact primary names, then one compatible word-boundary variant
such as BattleCity/Battle City. Collisions, incompatible editions and known
regions stay unmatched; alternate names are not primary identities. Bounded hash
lookup and new defaults prefer official releases, retaining all-unofficial order.

Saved `not_found` results are skipped on normal resume. After a matcher/snapshot
fix, deliberately refresh the affected provider:

```shell
rom-meta fetch --data-root "/path/to/catalog" --platform nes --source launchbox --type metadata --force-overwrite
rom-meta fetch --data-root "/path/to/catalog" --platform nes --source launchbox --type metadata --force-overwrite --apply
```

Review before apply. This replaces selected-provider results, including negatives;
other providers and explicit overrides remain. Headered/headerless hashes still
represent different bytes.
