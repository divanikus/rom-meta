# Operator workflow: import, enrich, fold, export

These are ordinary CLI operations. Running an HTTP server is optional; it is useful
for inspecting results or serving consumers. No local API token, accounts, JSON
review plan or manual hash calculation is required for the normal workflow below.
Use `rom-meta help <command>` (including `help catalog fold`) or `--help` for flags.

Examples use a Game Gear catalog and one ROM system directory. Replace quoted
paths/platforms with your own. Keep catalog, source ROMs and output folders separate.

## 1. Initialize and import a DAT

```shell
rom-meta init "<catalog>"
rom-meta dat plan "<platform.dat-or-zip>"
rom-meta dat import "<platform.dat-or-zip>" --data-root "<catalog>"
rom-meta dat import "<platform.dat-or-zip>" --data-root "<catalog>" --apply
```

`dat plan` identifies the dataset/platform and ZIP members. Use `--member` when an
archive contains several DATs, or `--platform` if detection needs an explicit name.
The first import command previews additions/reconciliation; `--apply` writes the
catalog. It imports release/checksum records, not ROM payload files. Existing
metadata/media is preserved. Unresolved identities stop mutation with a report.

## 2. Enrich metadata, then inspect coverage

```shell
rom-meta sources list --data-root "<catalog>"
rom-meta fetch --data-root "<catalog>" --platform gamegear --source launchbox --type metadata --limit 10
rom-meta fetch --data-root "<catalog>" --platform gamegear --source launchbox --type metadata --limit 10 --apply
rom-meta sources coverage --data-root "<catalog>" --platform gamegear
```

LaunchBox metadata needs no provider account. The first fetch may download its
snapshot; `--limit` limits selected cards, not snapshot download size. An existing
configured local snapshot is reused. Preview may download/query/cache source data
but does not write provider stanzas into game cards. Review the matched/not-found
counts, then apply. Repeating an applied command resumes/skips saved results.

Remove `--limit` after a small pass. Omit `--source` to use configured provider
order/applicability; set provider credentials once in local configuration for
sources that require them. Artwork/video use the same `fetch` interface with
`--type artwork` / `--type video`; inspect source policy and real coverage. `not_found`
is an observation, not a reason to force a guessed match or run a bulk overwrite.

## 3. Fold duplicate regional cards with explicit review

```shell
rom-meta catalog fold --data-root "<catalog>" --platform gamegear --details
rom-meta catalog fold --data-root "<catalog>" --platform gamegear --interactive
```

The first command shows candidates, evidence, proposed retained card and held
conflicts without merging. Interactive review accepts `yes` to merge, `no` to save
rejection of the displayed evidence, `skip` (or Enter) to leave it undecided, and
`quit` to stop review. Backups/recovery paths are printed when data is written.
No candidates is a valid result: DAT import already groups ordinary regional
releases when it has enough evidence. Do not merge distinct products merely to
reduce card count. `--apply` is the separate bulk path for automatically eligible
groups and does not bypass held identity conflicts.

Complex historical mixtures needing partitions, release reassignment or an explicit
primary remain an advanced reviewed-plan workflow. Those cases still need technical
review/JSON; this is a known usability gap, not a requirement of ordinary whole-group
fold. Source aliases are evidence, not permission to accept a wrong provider match.

## 4. Preview and create a frontend package

```shell
rom-meta validate "<catalog>"
rom-meta export --target es-de --platform gamegear --data-root "<catalog>" --rom-root "<one-system-ROM-directory>" --out "<new-output-directory>"
rom-meta export --target es-de --platform gamegear --data-root "<catalog>" --rom-root "<one-system-ROM-directory>" --out "<new-output-directory>" --apply
```

Export is offline and reports exact/name matches, ambiguity, unsupported inputs and
text/media coverage. Preview creates no output. Apply creates a new package with
preview/report/XML; it never replaces an existing output or modifies source ROMs.
`--profile` is optional; use a reusable profile for language/source/media priorities.
For an ES-DE root containing system subfolders use `--all` or `--platforms` instead
of `--platform`. Reader support and game-only evidence remain explicit limits.
For a catalog on another machine, replace `--data-root` with `--api <origin>`;
run the same `serve` there and keep ROMs on the client. See the
[network walkthrough](offline-export.md#export-from-a-remote-catalog).

## Optional: inspect the result through HTTP

```shell
rom-meta serve --data-root "<catalog>"
```

One address serves the browser, `/api/*`, `/v1/*`, `/docs` and `/openapi.json`.
No profile or token is required. Default bind is `127.0.0.1:8787`. Metadata writes
remain the CLI commands above. Run catalog preparation and consumer imports
sequentially. See [catalog API](catalog-api.md) for optional access protection and
consumer setup, and [offline export](offline-export.md) for consumers.

The automated operator regression executes this complete path using a synthetic
DAT, a local LaunchBox snapshot and generated bytes, including actual interactive
fold, resume, metadata propagation, preserved fingerprints and unchanged ROMs.
It blocks outbound HTTP, so successful enrichment proves the offline path.
