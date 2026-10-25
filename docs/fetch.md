# Automatic fetch source selection

Omit --source to use the ordered lists in fetch.metadata, fetch.artwork and
fetch.video in config/sources.yaml. --type limits the lists; otherwise metadata
runs before artwork, followed by video. Sources absent from these lists are not
added automatically. The legacy enabled field does not control selection.

```powershell
& $RomMeta fetch --data-root $DataRoot --platform scummvm
& $RomMeta fetch --data-root $DataRoot --platform scummvm --apply
```

Each configured source/type is checked against the selected catalog platform.
Automatic selection requires an explicit source name mapping in the embedded
platform registry or config/platforms.yaml. ScreenScraper can also use its
registered system ID. A display name or another provider's platform name is not
proof of applicability. Missing mappings produce a visible Result: skipped with
the platform, source and reason, before the connector is called. This means no
provider requests, metadata not_found writes or media downloads for that task.
A skip describes RomMeta's available mapping, not a claim that the upstream
service can never contain the game.

Hasheous uses ROM identity rather than the platform registry. Automatic fetch
checks pending cards locally using the connector's actual lookup rules: valid
whole-ROM hashes, or a MAME set identity together with a matching component
SHA1. A list-only card has neither. If none are queryable, the entire task is
skipped with a reason. In a mixed platform, queryable cards proceed while the
others stay untouched and are counted as skipped without consuming --limit.
Even --force-overwrite does not turn absent lookup evidence into not_found.
The local applicability scan may update the disposable catalog cache.

Without --platform, selection operates separately on each actual platform
directory. Source order is retained across platforms. Exact existing directory
names take precedence over aliases; aliases must resolve to one directory.
Unknown or ambiguous filters are errors. A library with no platforms or a run
with every source skipped finishes successfully with an explanation.
Automatic --limit is per source/type/platform task where supported. Normal
provider credentials, access checks, rate limits and quota handling still apply
to selected tasks. Missing credentials are errors, not absent game matches.

A real search with no accepted match still saves not_found under --apply;
existing results still resume normally. Automatic skips do not remove any
previously saved provider results. Explicit --source retains the manual path
and its existing connector behavior, including its platform matching rules.
Use it for a deliberate investigation rather than expecting automatic skips.

## Add a reviewed mapping

An unregistered platform is skipped for providers that require a mapping.
Add verified names using the existing registry override format:

```yaml
"My Catalog Platform":
  aliases: [my-system]
  sources:
    launchbox: "Actual LaunchBox Platform"
    screenscraper: { id: 999, name: "Actual ScreenScraper System" }
```

Save this as config/platforms.yaml under the data root. Use actual provider
names and IDs, not the illustrative values above. Existing embedded entries
are merged with local overrides. A source must still appear in fetch.<type>.

For ScummVM the embedded registry maps LaunchBox, ScreenScraper (system 123)
and EmuMovies. IGDB and OpenVGDB have no dedicated mapping; Hasheous normally
has no evidence on cards created from the official list. See
[the ScummVM workflow](scummvm.md) and rom-meta fetch --help.

## Reviewed identity-only IGDB links

For known foreign IDs that need official platform/slug confirmation or a reviewed
replacement/suppression, use [the scoped link workflow](igdb-links.md),
`fetch --refresh-links --link-review <JSON>`. It does not rescrape full metadata
or media and does not use global `--force-overwrite`.

## EmuMovies video directories

Video acquisition accepts only an exact normalized platform name or an explicit
registry alias before choosing HD, HQ, then SQ. Shared prefixes and suffixes are
not platform identity: Xbox 360 cannot supply Xbox, and Game Boy Color cannot
supply Game Boy. Missing exact directories remain visible skips; failed FTP
listings remain errors. Provider collection names such as PS3 Retail are explicit
aliases; PSN or hacks collections are not implicit fallbacks.

Known provider spellings use the same embedded registry and config/platforms.yaml
overrides as other fetch tasks. Quality preference does not guarantee complete
game coverage. Normal reruns preserve existing local media; changing a mapping
alone does not validate or replace files from an earlier wrong selection. Inspect
saved source paths and use a backed-up, scoped replacement when repair is needed.

Video/artwork name matching preserves edition, hardware and unknown qualifiers.
Only recognized region/language, revision/version and disc-number attributes are
ignored for the base title. A generic title or alias cannot erase an explicit
qualifier or extend a title into another edition. Known source regions require a
compatible release; regional preference cannot retarget a selected edition.
Ambiguous or incompatible candidates are skipped rather than borrowed.

A partial video fetch appends source matches only for successful new downloads.
Earlier video/artwork matches, custom source fields and recovery evidence remain.
Failed downloads create no source match; a zero-download repeat leaves metadata
unchanged. Existing files are still resumed by presence, so these rules do not
retroactively validate media acquired before an identity correction.

## Primary spellings and saved negative results

LaunchBox tries exact primary names first. If none exists, it can accept one
compatible primary name with different word boundaries, such as BattleCity /
Battle City. Compact-name collisions, incompatible editions or known regions
remain unmatched. Provider alternate names do not become primary identities.
Bounded hash lookups and newly chosen conventional defaults prefer official
releases over pirate/aftermarket variants; all-unofficial relative order remains.

A saved not_found counts as existing work during normal metadata resume. After a
matcher or snapshot correction, deliberately refresh the selected provider:

```shell
rom-meta fetch --data-root "/path/to/catalog" --platform nes --source launchbox --type metadata --force-overwrite
rom-meta fetch --data-root "/path/to/catalog" --platform nes --source launchbox --type metadata --force-overwrite --apply
```

Review the preview first. This replaces the selected provider's results, including
saved negatives; other providers and explicit catalog overrides remain. The rule
does not make headered and headerless ROM hashes interchangeable.
