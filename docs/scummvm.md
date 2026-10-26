# ScummVM game cards and media

Create a catalog without a DAT, game files or an installed ScummVM. RomMeta gets
the game list from structured data in the official ScummVM website repository;
existing providers supply descriptions, artwork and video.

## Prepare

Build/install RomMeta from README. Online import needs public HTTPS access to
GitHub/raw.githubusercontent.com, without an account, Git installation or ScummVM.
Keep credentials in the catalog's `config/secrets.yaml`; preserve existing secrets.

Set paths in PowerShell (reuse an existing data root):

```powershell
$RomMeta = "C:\Tools\RomMeta\rom-meta.exe"
$DataRoot = "D:\RomMetaData"

& $RomMeta init $DataRoot
& $RomMeta sources list --data-root $DataRoot
```

Initialization preserves config unless `--force-config` is explicit.

| Source | Requirements |
| --- | --- |
| LaunchBox | No account; first fetch may download Metadata.zip |
| ScreenScraper | username, password, dev_id, dev_password |
| EmuMovies | FTP account/service access; quality/coverage depends on access |

The registry selects dedicated ScummVM sections, including ScreenScraper 123.
It does not substitute PC/DOS/Amiga records. IGDB/OpenVGDB are not automatic fallbacks.

## GitHub API HTTP 403

RomMeta resolves `master` via public Git HTTPS ref discovery, with GitHub REST as
fallback. Anonymous REST quota may return 403. If both fail, use
`--revision <full-commit-sha>` or a previously applied snapshot with `--offline`.
Raw files must still be accessible and come from one pinned commit; failures
never silently switch to a moving branch or stale cache.

## Preview and create cards

```powershell
& $RomMeta scummvm import --data-root $DataRoot
& $RomMeta scummvm import --data-root $DataRoot --apply
```

Default import selects the newest stable release from official versions data.
`games.yaml`, `compatibility.yaml`, `versions.yaml` and `engines.yaml` come from
one scummvm/scummvm-web commit. Each game uses its latest compatibility record no
later than that release. Enabled good/excellent/bugged games are eligible;
broken/untested, disabled-engine and known generic engine categories are excluded.
This is website coverage, not every detectable fan game.

The summary shows release/commit, exclusions, selection and changes. Preview
fetches source data but saves no catalog/cache. `--release 2026.4.0` selects a
stable release; use the same explicit `--revision` on preview/apply to prevent
upstream drift. `--engine scumm,sci` narrows selection; unknown engines fail.

Cards use `platforms/ScummVM/<title>/`. Only sanitized/case-folded title collisions
get qualified-ID suffixes, computed against the full source even for engine
subsets. `scummvm` provenance records engine/game IDs, title, version, URL/commit, support
and normalized-list SHA256. Distinct IDs stay distinct. Each card's title-based
release is a metadata anchor, not a physical edition, region, language, fingerprint
or tested launch.

## Collect metadata, artwork and video

Without `--source`, configured applicable providers run metadata, artwork, then video:

```powershell
& $RomMeta fetch --data-root $DataRoot --platform scummvm
& $RomMeta fetch --data-root $DataRoot --platform scummvm --apply
```

Use `--type metadata`, `artwork` or `video` for staged passes; `--limit 10` applies
per source/type/platform where supported. LaunchBox, ScreenScraper and EmuMovies
need entries in `fetch.<type>`. IGDB/OpenVGDB lack dedicated mappings; Hasheous
skips cards without ROM evidence. Skips print reasons without requests or new
`not_found` results. See [automatic selection](fetch.md).

New config defaults to LaunchBox/OpenVGDB/Hasheous metadata, LaunchBox/OpenVGDB
artwork and no video. Add ScreenScraper/EmuMovies after configuring accounts;
automatic selection preserves existing lists. Explicit `--source launchbox` or
`--source launchbox,screenscraper` remains available. Leave unconfigured providers
out. EmuMovies preview lists directories without downloading videos.

Repeat without `--force-overwrite` to skip saved metadata (including `not_found`)
and existing media. Retry negatives only with a deliberate scoped metadata
`--force-overwrite`; quota/retry behavior still applies. Missing matches stay gaps.

## Rename folders created by the first importer

Older imports suffix every folder with engine/game ID. After fetch finishes,
preview removal of unnecessary suffixes:

```powershell
& $RomMeta scummvm rename-folders --data-root $DataRoot --details
& $RomMeta scummvm rename-folders --data-root $DataRoot --apply
```

Apply preflights targets, saves `.rommeta/runs/scummvm-folder-names-*.json` and
renames whole bundles on the same filesystem without rewriting contents or
redownloading. Collisions keep qualified IDs; custom names remain. Occupied
targets fail rather than merge/overwrite. Transient permissions retry; persistent
failures report an error while independent moves continue. Repeat resumes and
successful completion plans no changes.

Stop other writers during apply. Imports find cards by `scummvm.id`, preserving
renamed paths; import itself does not rename. Normal reads use current directories
and disposable caches. Historical manifests/external references are not rewritten;
keep the mapping for old paths.

## Check and browse

```powershell
& $RomMeta validate $DataRoot
& $RomMeta catalog status --data-root $DataRoot --platform scummvm --details
& $RomMeta sources coverage --data-root $DataRoot --platform scummvm --details
& $RomMeta serve --data-root $DataRoot
```

Fetch reports matches, gaps, media work and failures. Source coverage reports
provider gaps; catalog status counts local files/URLs. Inspect art/video in the
read-only browser; counts do not guarantee categories, style or gameplay.

## Repeat, update and work offline

Online reimport updates official data, optionally with `--release`. Cards are
found by `scummvm.id`; only provenance and necessary official-title aliases change.
User titles, release keys, unknown fields, providers/media and omitted IDs remain.

Apply saves four raw YAMLs, SHA256 values, normalized `games.txt`, release/commit
and a pointer to the last successful official snapshot. Afterwards:

```powershell
& $RomMeta scummvm import --data-root $DataRoot --offline
& $RomMeta scummvm import --data-root $DataRoot --offline --apply
```

Offline mode verifies checksums and makes no network calls. Explicit release/
revision must match that snapshot. Missing/corrupt cache fails without online
fallback; preview saves no cache.

Local alternatives:

```powershell
& $RomMeta scummvm import --data-root $DataRoot --scummvm "<path-to-scummvm.exe>"
& $RomMeta scummvm import --data-root $DataRoot --list-file "<snapshot>\games.txt" --version "<recorded version>"
```

The executable uses `--version`/`--list-games`; saved lists need `--version` and
accept UTF-8 or BOM-marked UTF-16. Local modes do not use website support filtering
and cannot combine with official `--release`, `--revision`, `--offline`. DAT/JSON
are not game-list inputs. Preview before `--apply`.

Stop other writers before apply. Input/card conflicts preflight; files replace
atomically, but filesystem failure may leave a partial import. Repeat to resume.
Media folders are never copied/replaced.

## Scope and verification

Website coverage for a stable release does not guarantee every language/edition.
ROM recognition, game installation and ScummVM ES-DE launch export are outside
this workflow.

Tests cover commit pinning, release/compatibility selection, malformed data,
offline checksums, preview/apply/replay and preservation. Synthetic providers test
media/resume; full live-source coverage is unmeasured.

Sources:

- [Official website data](https://github.com/scummvm/scummvm-web/tree/master/data/en)
- [ScummVM command line](https://docs.scummvm.org/en/latest/advanced_topics/command_line.html)
- [LaunchBox ScummVM](https://gamesdb.launchbox-app.com/platforms/games/143-scummvm)
- [ScreenScraper ScummVM](https://www.screenscraper.fr/systemeinfos.php?plateforme=123)
- [EmuMovies ScummVM videos](https://emumovies.com/files/file/1380-scummvm-video-snaps-pack/)
