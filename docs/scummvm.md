# ScummVM game cards and media

Create a catalog without a DAT, game files or an installed ScummVM. RomMeta gets
the game list from structured data in the official ScummVM website repository;
existing providers supply descriptions, artwork and video.

## Prepare

Build/update RomMeta using the README instructions. Online import needs HTTPS
access to github.com and raw.githubusercontent.com; this public source needs no
account or token. RomMeta reads the public Git ref advertisement to resolve
master, using the GitHub REST API only if Git ref discovery fails.
RomMeta does not install or run ScummVM in the default workflow.

Set your real paths once in PowerShell (reuse your existing data root):

```powershell
$RomMeta = "C:\Tools\RomMeta\rom-meta.exe"
$DataRoot = "D:\RomMetaData"

& $RomMeta init $DataRoot
& $RomMeta sources list --data-root $DataRoot
```

Initialization preserves existing config unless --force-config is explicitly
used. Keep source credentials in config/secrets.yaml under the data root.
Use secrets.example.yaml as a template; preserve an existing secrets file.

- LaunchBox needs no account for its public Metadata.zip and artwork URLs.
  The first metadata fetch downloads the snapshot if no local copy exists.
- ScreenScraper needs username, password, dev_id and dev_password. Without
  available credentials, use LaunchBox alone and add ScreenScraper later.
- EmuMovies uses your account's username/password and accessible FTP service.
  Available quality and coverage depend on that service. Personal-media policies
  remain in effect.

The embedded registry selects dedicated ScummVM sections, including ScreenScraper
system 123. It does not guess a PC/DOS/Amiga equivalent when a provider has no
ScummVM card. IGDB/OpenVGDB are not automatic fallback providers for this workflow.

## GitHub API HTTP 403

RomMeta resolves the official master revision with public Git HTTPS ref discovery,
without a Git installation, clone or API token. The GitHub REST API is a fallback;
an anonymous shared egress quota can produce HTTP 403 there.

If Git ref discovery fails, the REST endpoint remains a fallback. If both
channels fail, the error reports both failures and suggests --revision
<full-commit-sha> to bypass discovery, or --offline for a previously applied
snapshot. These options do not bypass access checks on the pinned raw files.
Raw-file failures stop the import; no moving-branch or stale-cache fallback
silently changes its source. All four raw files still come from one commit.

## Preview and create cards

```powershell
& $RomMeta scummvm import --data-root $DataRoot
& $RomMeta scummvm import --data-root $DataRoot --apply
```

Default import chooses the newest released stable version in the official
versions data. It reads games.yaml, compatibility.yaml, versions.yaml and
engines.yaml from one pinned commit of scummvm/scummvm-web. For each game it
selects the latest compatibility record no later than that release. Enabled
games with good, excellent or bugged support are eligible; broken/untested and
disabled-engine entries are excluded. Known generic engine categories are
filtered separately.

This is the official website's curated coverage, not a complete inventory of
every detectable fan game supported by every engine. The summary shows the
release, source commit, exclusion counts, selected games and planned changes.
Preview fetches public source data but creates no catalog/cache files.

Use --release 2026.4.0 to choose a particular stable release. Add
--revision <full-commit-sha> to both preview and apply to use exactly the same
website data; otherwise a later online invocation can see upstream updates.
Optional --engine scumm,sci selects a smaller first catalog. Remove that filter
later to add remaining engines. Unknown engine IDs are errors.

Cards are stored under platforms/ScummVM/ in folders named after the game.
Only titles that collide after filename sanitation and case folding get a
qualified-ID suffix. Collision decisions use the full concrete source list,
including when --engine selects a subset. The
scummvm provenance mapping records engine/game IDs, source title, version,
source URL/commit, support level and normalized-list SHA256. Identical titles
with different IDs remain separate; known generic categories are skipped.
The current catalog model needs releases, so each card has one title-based
metadata anchor. It does not claim a physical edition, language, region, ROM
hash or tested local launch. No fingerprints are invented.

## Collect metadata, artwork and video

Without --source, fetch automatically checks the sources in config/sources.yaml
against ScummVM. Metadata runs before artwork, then video. Preview and apply:

```powershell
& $RomMeta fetch --data-root $DataRoot --platform scummvm
& $RomMeta fetch --data-root $DataRoot --platform scummvm --apply
```

For a staged first pass, use --type metadata, --type artwork, then --type video;
add --limit 10 where supported. The limit applies separately to each source/type/
platform task. Remove it to continue. LaunchBox, ScreenScraper and EmuMovies are
eligible if listed in fetch.<type>. IGDB/OpenVGDB are skipped for lack of a
dedicated platform mapping. Hasheous is skipped if cards have no usable ROM
lookup evidence. Each skip prints its reason without provider requests or new
not_found records. See [automatic selection](fetch.md) for registry overrides.

A newly initialized configuration uses LaunchBox/OpenVGDB/Hasheous metadata,
LaunchBox/OpenVGDB artwork and no video. Add ScreenScraper and EmuMovies to the
appropriate fetch lists after configuring their accounts. Existing configured
lists are preserved; automatic selection does not add providers to them.
Applicable providers still require valid credentials. EmuMovies preview lists
remote directories but does not download videos.

Explicit selection remains available, for example --type metadata --source
launchbox or --type artwork --source launchbox,screenscraper. Without
ScreenScraper credentials, leave it out of the fetch lists or select LaunchBox
explicitly. --source uses the existing manual connector behavior.

Repeat fetch commands without --force-overwrite to resume. Saved metadata
results (including not_found) and existing media are skipped. Missing matches
stay reported gaps. Retrying saved not_found results needs a deliberate scoped
--force-overwrite metadata run; do not force the entire library to resume.
ScreenScraper quota/retry behavior is unchanged.

## Rename folders created by the first importer

The first importer appended an engine/game ID to every folder. After the media
fetch finishes, use the ordinary CLI to remove unnecessary suffixes:

```powershell
& $RomMeta scummvm rename-folders --data-root $DataRoot --details
& $RomMeta scummvm rename-folders --data-root $DataRoot --apply
```

The preview is read-only. Apply preflights every target, saves a private JSON
mapping in .rommeta/runs/scummvm-folder-names-*.json, and renames whole bundles
on the same filesystem. Metadata, media filenames, release anchors and contents
are not rewritten or downloaded again. Real title collisions retain their
qualified IDs. Custom folder names are preserved, and occupied targets are
errors rather than merges or overwrites. Transient permission failures use a
bounded retry. A persistent rename failure is reported while independent moves
continue; the run returns an error. Repeat to resume; completed moves remain. A successful repeat plans zero changes.

Keep other catalog writers stopped during apply. Existing imports continue to
find cards by scummvm.id and preserve the renamed paths. Import itself does not
rename existing folders. Normal catalog reads follow the current directories;
the derived cache is disposable. Historical download manifests and external
path references are not rewritten; retain the rename mapping when consulting
old paths.

## Check and browse

```powershell
& $RomMeta validate $DataRoot
& $RomMeta catalog status --data-root $DataRoot --platform scummvm --details
& $RomMeta sources coverage --data-root $DataRoot --platform scummvm --details
& $RomMeta serve --data-root $DataRoot
```

Fetch summaries report matches, not-found results, planned/existing/downloaded
media and failures. Source coverage reports provider gaps; catalog status counts
local files and URLs. Inspect actual art/video through the read-only browser.
These counts do not promise every media category, artwork style or gameplay.

## Repeat, update and work offline

Re-run online import to acquire current official data, optionally with --release.
Existing cards are found by scummvm.id even if their folders were renamed.
Only importer provenance and needed official-title aliases are updated. User
titles, release keys, unknown fields, provider metadata and media survive.
IDs absent or excluded in the new source remain in the existing catalog.

Apply saves all four raw YAML files, their SHA256 values, the normalized games.txt,
selected release and full commit in the printed snapshot directory. A local
pointer identifies the last successfully applied official snapshot. After an
online apply, repeat without network:

```powershell
& $RomMeta scummvm import --data-root $DataRoot --offline
& $RomMeta scummvm import --data-root $DataRoot --offline --apply
```

Offline mode verifies raw-source/list checksums and reconstructs the selection.
It makes no network calls. Explicit --release/--revision must match the cached
snapshot; acquire another selection online first. Missing/corrupt cache is an
error, with no automatic online fallback. Preview does not save a cache.

Explicit local alternatives remain available:

```powershell
& $RomMeta scummvm import --data-root $DataRoot --scummvm "<path-to-scummvm.exe>"
& $RomMeta scummvm import --data-root $DataRoot --list-file "<snapshot>\games.txt" --version "<recorded version>"
```

The executable alternative runs --version and --list-games. Saved plain lists
need --version and accept UTF-8/BOM-marked UTF-16. These explicit local modes do
not apply website support filtering. --release, --revision and --offline belong
to the official source and cannot be combined with local input. Add --apply
after previewing. Detection DAT and JSON files are not game-list inputs.

Stop other writers before applying. Input/card conflicts are checked before
writing, and each file is replaced atomically. A filesystem failure may leave a
partial import; repeat to resume. Media folders are never copied or replaced.

## Scope and verification

The default scope is the stable release's official website data. A list entry
does not guarantee support for every language/edition. ROM recognition, game
installation and ScummVM ES-DE launch export remain outside this workflow.

Tests cover official HTTP requests and commit pinning, historical compatibility,
RC/future releases, malformed data, offline checksums, preview/apply/replay and
existing-card preservation. Synthetic providers exercise metadata, image/video
downloads and resume. Live full-list media coverage remains unmeasured.

Sources:

- [Official website data](https://github.com/scummvm/scummvm-web/tree/master/data/en)
- [ScummVM command line](https://docs.scummvm.org/en/latest/advanced_topics/command_line.html)
- [LaunchBox ScummVM](https://gamesdb.launchbox-app.com/platforms/games/143-scummvm)
- [ScreenScraper ScummVM](https://www.screenscraper.fr/systemeinfos.php?plateforme=123)
- [EmuMovies ScummVM videos](https://emumovies.com/files/file/1380-scummvm-video-snaps-pack/)
