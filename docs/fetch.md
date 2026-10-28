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

On PSP, a catalog-side No-Intro `(Minis)` collection label can match an untagged
media name. This applies to video and artwork, records `psp_minis_label` in match
evidence and keeps the original release key. Explicit source labels, regions,
other qualifiers and ambiguous owners remain significant; labels are not removed
globally. Provider coverage can still be incomplete.

Partial fetch appends matches only for successful new downloads, preserving prior
matches, custom fields and recovery evidence. Failed/zero downloads add no match;
existing files resume by presence. These rules do not retroactively verify old media.

## Progress, partial results and resume

Catalog discovery and file transfers have separate counters. Applied media fetch
shows downloaded, existing, failed and pending files, transferred bytes, average
rate and elapsed time. Pending counts discovered work; the final catalog size is
not a download percentage. Logs receive throttled totals and a final update.

Individual download or metadata lookup errors produce an `incomplete` result and
a nonzero exit status. Independent source jobs continue; the final summary counts
completed, incomplete, failed and skipped jobs. A normal `not found` is not an error.
Successful files and source matches remain saved. EmuMovies extracts complete
packages even when another package failed; incomplete multipart archives wait.

To resume, **run the same command with `--apply` again**. Existing files are
skipped; failed and unstarted downloads are attempted. This uses the normal
catalog/provider discovery pass. No retry-specific flag is needed, and existing
files are not revalidated or overwritten. Provider quotas may require waiting.

Applied media fetch writes a separate JSONL outcome report per pass under
`.rommeta/runs/`, with catalog-relative targets and credential-free source URLs.
HTTP artwork records existing, duplicate, limit-skipped, downloaded and failed
candidates; EmuMovies records existing and attempted video/archive files, including
individual archive parts. Reports retain all recorded failures even when console
details are shortened. A later pass keeps earlier reports. Reports describe
processed candidates, not unseen work after a quota stop or forced termination.

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

LaunchBox matching includes explicit catalog `aliases` within the selected
platform. Conflicting aliases and unofficial-release guards still apply.
ScreenScraper responses naming a different system are rejected even if its API
returns them for the requested system. A declared child system remains eligible.

WiiWare video matching can omit the catalog's `(WiiWare)` label only inside the
provider's exact Nintendo WiiWare directory. Wii disc videos cannot use this
fallback; regions, edition qualifiers and ambiguous owners remain checked.

Digital DATs keep their own catalog platforms: `wiidigital` selects Wii Digital
CDN, `wiiueshop` selects Wii U Digital CDN, and `ps3psn` selects PS3 PSN Content.
The `wii`, `wiiu` and `ps3` aliases select disc catalogs. Provider metadata uses
the corresponding hardware platform; EmuMovies uses WiiWare, Wii U eShop or PSN.
IGDB uses the exact provider names `Wii` and `Wii U`; local source overrides
should use those names too.
Explicit catalog/CLI aliases take precedence over shared provider platform names.

Hasheous defaults to one request per second, shared by its workers. On HTTP 429
it honors `Retry-After`, Hasheous `retryAfterSeconds` and legacy `retry_after`,
keeps a slower request pace after recovery, and stops resumably if retries fail.
Existing local `sources.hasheous.api.rate_limit_per_second` overrides remain in
force. Server profile limits can differ; see [Hasheous rate limiting](https://github.com/gaseous-project/hasheous/wiki/Rate-Limiting).

ScreenScraper's daily quota and its HTTP 431 return-tomorrow response stop that
provider's run. Incomplete searches remain resumable instead of becoming saved
negative matches. Other HTTP 431 errors retain their normal error handling.
