# Contributing

AI coding agents should start with [AGENTS.md](AGENTS.md) for repository rules,
verification and data boundaries.

Use the Go toolchain specified in `go.mod`. Keep implementation in `cmd/` and
`internal/`; frontend adapters read the shared catalog model. Add focused behavior
tests and update the relevant user guide. Format Go with `gofmt`.

```shell
go test ./...
go vet ./...
python -m unittest discover -s scripts -p 'test_*.py'
python -m unittest discover -s examples/romm-importer -p 'test_*.py'
```

Run full checks from a clean source checkout. Go also scans Git-ignored scratch
directories under a module; keep temporary Go experiments in dot-prefixed
directories or outside the source tree.

The separate variant preparation helper is covered by `go test ./scripts/prepare-variants`.
Its export regressions use only synthetic catalog/payload/media fixtures.

The RomM tests use synthetic/fake runtime objects; a live database is not needed.
Image normalization tests need Pillow; the importer runtime supplies it. Python
helpers use Python 3.10 or newer. Catalogs, ROMs/BIOS, downloaded media, credentials,
private paths and generated plans/receipts belong outside Git. Fixtures must be
synthetic or clearly redistributable. Never include a populated environment file.

A Windows build updates `bin/rom-meta.exe`; Linux uses `bin/rom-meta`. Keep output
separate from input ROM/catalog trees. Do not run concurrent catalog writers.

To build release ZIPs from a clean committed tree in PowerShell:

```powershell
./scripts/build-release.ps1 -Version 0.4.0
```

To build a standalone variant-helper package for Windows x64:

```powershell
./scripts/build-release.ps1 -Version 0.4.0 -Program prepare-variants -Platform windows-amd64
```

This creates a ZIP and SHA256SUMS under
`bin/releases/prepare-variants/<version>/`, including the executable, quick-start
text, variant guide and dependency licenses. It needs a clean committed checkout
and does not create a tag, upload a release or deploy anything. Omit `-Platform`
to build Windows amd64 and Linux amd64/arm64 helper packages. Main CLI-only packaging is
available with `-Program rom-meta`; `-Platform` can also narrow its target list.
The default `-Program all` creates separate main CLI and helper ZIP assets for
each selected platform, listed together in the root `SHA256SUMS`.

The default script writes separate main CLI/helper Windows amd64 and Linux
amd64/arm64 archives plus SHA256SUMS under ignored
`bin/releases/`, including selected user docs/examples and dependency licenses.
It does not create a Git tag, upload artifacts or deploy services. Test extracted
archives and verify embedded version/checksums before publishing. Public file
selection is explicit in `packaging/public-files.json`; the preparation helper
copies selected tracked files without `.git` into a new directory.

## Web UI source layout

`internal/serveui/operations.go` owns the shared operation registry and lifecycle;
import, fetch and export handlers own their workflow-specific execution.
`assets.go` embeds and serves the browser assets. Merge and card editing have
separate source files, as do export setup, match review and profile editing. They
are concatenated into the existing `/catalog-edit.js` and `/export.js` responses
in a fixed order; there is no extra frontend build step. Browser tests load the
same bundles, and HTTP tests cover their routes and initialization order.
