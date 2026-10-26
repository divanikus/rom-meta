# Operator workflow: import, enrich, fold, export

Examples use Game Gear; substitute your paths/platform. Keep catalog, source ROMs
and output separate. The normal workflow needs no server, local API token or
manual hash/review plan. Use `rom-meta help <command>` or `--help` for flags.

## 1. Initialize and import a DAT

```shell
rom-meta init "<catalog>"
rom-meta dat plan "<platform.dat-or-zip>"
rom-meta dat import "<platform.dat-or-zip>" --data-root "<catalog>"
rom-meta dat import "<platform.dat-or-zip>" --data-root "<catalog>" --apply
```

`dat plan` identifies the dataset/platform and ZIP members. Use `--member` for
multi-DAT archives or `--platform` when detection needs a name. Logiqx XML from TOSEC retains its own authority; ClrMamePro text requires
conversion to XML first. Import previews
changes; `--apply` writes release/checksum records, preserving existing metadata/
media. It does not copy ROMs. Unresolved identities stop mutation with a report.

## 2. Enrich metadata, then inspect coverage

```shell
rom-meta sources list --data-root "<catalog>"
rom-meta fetch --data-root "<catalog>" --platform gamegear --source launchbox --type metadata --limit 10
rom-meta fetch --data-root "<catalog>" --platform gamegear --source launchbox --type metadata --limit 10 --apply
rom-meta sources coverage --data-root "<catalog>" --platform gamegear
```

LaunchBox needs no account. First fetch may download its snapshot; `--limit`
limits cards, not snapshot bytes. Configured snapshots are reused. Preview may
query/download/cache source data but does not write provider stanzas. Review
match/not-found counts before apply; repeats skip saved work.

Remove `--limit` to continue. Omit `--source` for configured applicability/order;
configure credentials for providers that need them. Use `--type artwork` or
`--type video` for media. Missing matches remain gaps, not guessed identities or
reasons for bulk overwrite.

## 3. Fold duplicate regional cards with explicit review

```shell
rom-meta catalog fold --data-root "<catalog>" --platform gamegear --details
rom-meta catalog fold --data-root "<catalog>" --platform gamegear --interactive
```

Preview shows candidates, evidence, retained card and held conflicts. Interactive
choices: `yes` merges; `no` saves rejection of that evidence; `skip`/Enter defers;
`quit` stops. Writes print backup/recovery paths. No candidates is valid: DAT
import may already group the releases. `--apply` is a separate automatic bulk path
that still respects held conflicts. Distinct products must remain separate.

Partitions, release reassignment and explicit primaries use advanced reviewed
JSON plans. They are not required for ordinary whole-group fold. Source aliases
alone do not justify an incorrect match.

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

The [operator regression](https://github.com/divanikus/rom-meta/blob/main/cmd/rom-meta/operator_workflow_test.go) tests this
loop offline with synthetic DAT/provider data, including resume and preservation.
