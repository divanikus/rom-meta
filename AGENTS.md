# Agent instructions

RomMeta is a Go CLI for preparing portable ROM metadata catalogs and exporting
metadata/media for files supplied by the user. Keep catalog preparation, identity
matching and frontend projection separate.

## Before editing

- Inspect `git status` and preserve unrelated or unfinished user changes.
- Read [README.md](README.md), [CONTRIBUTING.md](CONTRIBUTING.md) and the guide for
  the affected workflow in [docs/README.md](docs/README.md).
- Read relevant local conventions, context, backlog and decisions when present.
  In particular, read `docs/conventions.md` before editing if that file exists.
  A public checkout does not require private planning documents or other checkouts.
- Check the implementation and tests before relying on historical notes. Keep
  changes scoped; prefer existing patterns and the smallest coherent fix.

## Code and behavior

- Keep CLI parsing/output in `cmd/` and reusable behavior in `internal/`.
  Source adapters retain provider evidence; export adapters project the shared
  catalog model. See [the metadata model](docs/portable-metadata.md).
- Preserve unknown YAML fields, explicit owner overrides and source provenance.
  Display formatting must not change ROM binding, hashes, launch paths or media
  ownership. Do not strengthen identity claims beyond the evidence available.
- Keep local export and API export behavior aligned. Changes affecting batching,
  playlists or MAME need tests for the relevant path; see
  [export](docs/offline-export.md) and [API](docs/catalog-api.md) contracts.
- Catalog mutations default to preview/dry run and require explicit `--apply`.
  Keep generated output separate from input ROM/catalog trees. Do not run
  concurrent writers against the same catalog.
- Prefer proportionate safeguards and existing recovery mechanisms. Do not add
  infrastructure, new dependencies or broad refactors unrelated to the task.

## Verification

Use the Go toolchain specified in `go.mod` and format changed Go files with
`gofmt`. Add focused regression tests when behavior changes. Run the relevant
package tests during iteration, then `go test ./...` and `go vet ./...` for Go
behavior changes before handoff.

For Python helper changes, run the applicable tests from
[CONTRIBUTING.md](CONTRIBUTING.md), including the RomM example's tests when that
example changes. Synthetic runtime tests do not prove a live frontend install.
For documentation-only changes, check the diff, links, commands and file selection;
a full application test run is not required.

Windows builds use `bin/rom-meta.exe`; Linux builds use `bin/rom-meta`. If an
output executable is locked, report it rather than stopping an active job or
inventing another executable name.

## Long operations and questions

- Complete safe automatic work before asking for user input. Resolve deterministic
  cases from existing configuration and conventions.
- Prefer existing plan-first/resumable workflows and useful progress output.
  Batch non-urgent questions after a plan, scan or resumable checkpoint.
- Ask immediately only when continuing is genuinely blocked, destructive, would
  expose private data, needs unavailable credentials or would spend significant
  resources without a useful plan. Do not interrupt active imports/downloads
  merely to commit documentation or ask an avoidable question.

## Public data and documentation

- Never commit secrets, populated environment files, ROMs/BIOS, downloaded media,
  provider databases, generated catalogs, caches, binaries or private plans/logs.
  Fixtures must be synthetic or clearly redistributable.
- Public guides describe capabilities, commands, formats and limitations. Keep
  private-library inventories, internal trial results, installation/device details
  and private paths out of public docs, release notes and examples.
- Update the affected user guide and examples when behavior changes. Keep README
  focused on introduction and useful workflows, rather than a change ledger.
- Documentation defaults to English. Conversation can follow the user's language.
- `packaging/public-files.json` defines the public file selection. Keep this file,
  CONTRIBUTING and the selected source/docs consistent; do not copy an entire
  private working tree into a public repository.

## Git and handoff

Follow the user's commit/push instructions and any applicable local workflow.
Stage only reviewed task files. Do not force-push, rewrite shared history, create
release tags, publish releases or deploy services without authorization. A normal
source change is not authorization for production catalog or frontend mutations.

Report what changed, why, how it was checked, remaining limits, commit/push status
and any unrelated local changes. Update existing local handoff documents when
applicable; do not create private-library reports in the public documentation.
