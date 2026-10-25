# Contributing

Use the Go toolchain specified in `go.mod`. Keep implementation in `cmd/` and
`internal/`; frontend adapters read the shared catalog model. Add focused behavior
tests and update the relevant user guide. Format Go with `gofmt`.

```shell
go test ./...
go vet ./...
python -m unittest discover -s scripts -p 'test_*.py'
python -m unittest discover -s examples/romm-importer -p 'test_*.py'
```

The RomM tests use synthetic/fake runtime objects; a live database is not needed.
Image normalization tests need Pillow; the importer runtime supplies it. Python
helpers use Python 3.10 or newer. Catalogs, ROMs/BIOS, downloaded media, credentials,
private paths and generated plans/receipts belong outside Git. Fixtures must be
synthetic or clearly redistributable. Never include a populated environment file.

A Windows build updates `bin/rom-meta.exe`; Linux uses `bin/rom-meta`. Keep output
separate from input ROM/catalog trees. Do not run concurrent catalog writers.

To build release ZIPs from a clean committed tree in PowerShell:

```powershell
./scripts/build-release.ps1 -Version 0.1.0
```

The script writes Windows/Linux amd64 archives and SHA256SUMS under ignored
`bin/releases/`, including selected user docs/examples and dependency licenses.
It does not create a Git tag, upload artifacts or deploy services. Test extracted
archives and verify embedded version/checksums before publishing. Public file
selection is explicit in `packaging/public-files.json`; the preparation helper
copies selected tracked files without `.git` into a new directory.
