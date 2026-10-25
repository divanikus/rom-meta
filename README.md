# RomMeta

RomMeta prepares a local metadata catalog for ROM collections. Import DAT release
and checksum data, fetch provider metadata and artwork, review regional game
bundles, then generate frontend metadata from the prepared catalog locally or over
your network.
Catalog data stays in readable YAML with source evidence and explicit overrides.

**0.1.0 is pre-stable.** Command names and catalog formats may change before 1.0.
RomMeta includes no ROMs, BIOS, provider database or downloaded artwork.

## Catalog inputs

| Catalog | Supported input |
| --- | --- |
| No-Intro | XML DAT files, including DAT members in ZIP archives; release attributes and ROM checksums |
| Redump | XML DAT files, including DAT members in ZIP archives; disc releases and file checksums |
| [MAME](docs/arcade-import.md) | Official `listxml` XML/ZIP input with version-matched classifications, set components and parent/clone relationships |
| [ScummVM](docs/scummvm.md) | Structured official game list imported as game cards; no DAT or game files required |

## Metadata and media sources

These providers enrich catalog cards through `fetch`:

| Source | Fetch types |
| --- | --- |
| LaunchBox | `metadata`, `artwork` |
| OpenVGDB | `metadata`, `artwork` |
| IGDB | `metadata`, `artwork` |
| ScreenScraper | `metadata`, `artwork` |
| Hasheous | `metadata`, using ROM identity |
| EmuMovies | `artwork`, `video` |

Coverage depends on the platform and provider access. See the
[fetch guide](docs/fetch.md) for configuration and matching rules.

## Export targets

| Target | Integration | Output |
| --- | --- | --- |
| ES-DE | Built-in `export --target es-de`, with a local catalog or `--api` | Gamelists, selected media, preview and provenance reports |
| RomM | Separate [server importer example](examples/romm-importer/README.md) | Metadata and media for already-scanned games in the pinned RomM runtime |

See [supported input formats and export scenarios](docs/export-scenarios.md).
The [HTTP API](#http-api) also exposes catalog data and assets to other consumers.

## Install

Download the Windows or Linux **amd64** archive from
[Releases](https://github.com/divanikus/rom-meta/releases) and extract it. Verify
the archive against the accompanying `SHA256SUMS`.
On Linux, run `chmod +x ./rom-meta`. Add its directory to `PATH`, or invoke the
executable by its full path. Windows uses `rom-meta.exe`.

To build from a source checkout, install the Go version in [go.mod](go.mod)
(currently 1.26.3 or newer):

```shell
git clone https://github.com/divanikus/rom-meta.git
cd rom-meta
go build -o bin/rom-meta ./cmd/rom-meta
```

On Windows use `-o bin/rom-meta.exe`. No Go installation is needed for prebuilt
binaries. Run `rom-meta --version` and `rom-meta --help` to check the installation.

## Import, enrich and review

Replace the quoted paths below with your own. Keep the catalog outside the source
checkout. Start with one system and a small metadata sample.

```shell
rom-meta init "/path/to/catalog"
rom-meta dat plan "/path/to/platform.dat-or-zip"
rom-meta dat import "/path/to/platform.dat-or-zip" --data-root "/path/to/catalog"
rom-meta dat import "/path/to/platform.dat-or-zip" --data-root "/path/to/catalog" --apply
rom-meta sources list --data-root "/path/to/catalog"
```

Enable sources in `config/sources.yaml` inside the catalog. Configure credentials
in `config/secrets.yaml` only for sources that require them; the generated template
shows the fields. Never put secrets in this repository.

```shell
rom-meta fetch --data-root "/path/to/catalog" --platform gamegear --source launchbox --type metadata --limit 10
rom-meta fetch --data-root "/path/to/catalog" --platform gamegear --source launchbox --type metadata --limit 10 --apply
rom-meta catalog fold --data-root "/path/to/catalog" --platform gamegear --details
rom-meta catalog fold --data-root "/path/to/catalog" --platform gamegear --interactive
rom-meta validate "/path/to/catalog"
```

Run the same fetch again to resume saved work. Run writers sequentially for one
catalog.

The [operator walkthrough](docs/cli-workflow.md) explains previews, enrichment,
interactive fold, backups and export. Use `rom-meta help <command>` for flags.

## Browse

```shell
rom-meta serve --data-root "/path/to/catalog"
```

Open http://127.0.0.1:8787 for the read-only catalog browser. No token or selection
profile is required for local browsing.

Stats starts with one platform. Entire-catalog scans are explicit, show progress
and cancel when returning to Browse. Large HDD/network catalogs can still take
minutes. Video opens on explicit playback.

## HTTP API

The same `serve` process exposes catalog lookup, metadata, identity proofs and
media assets. `/v1/export` provides the remote catalog projection used by
`export --api`; ROM files stay on the client.

- `/api`: endpoint index and browser API.
- `/v1/*`: consumer lookup, export and asset API.
- `/docs`: self-hosted interactive Swagger UI.
- `/openapi.json`: OpenAPI specification.

See the [API guide](docs/catalog-api.md) for endpoints, request/response formats
and optional shared-token access.

## Export to a frontend

**ES-DE:** enrich a ROM selection that you already have. Give RomMeta the prepared
catalog and the actual system ROM directory used by ES-DE. Local export works
offline, without `serve` or provider credentials. Start with a single NES folder:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/path/to/roms/nes" --out "/path/to/new-package"
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/path/to/roms/nes" --out "/path/to/new-package" --apply
```

The first command prints recognition/coverage without creating output. The second
creates a new package; open its `preview.html` and `report.txt`. Source ROMs and
catalog files stay unchanged. Output contains `gamelists/nes/gamelist.xml`, selected
`downloaded_media/nes/` and provenance reports. Batch mode uses `--all` or
`--platforms` against the parent ROM directory. [Profiles](examples/export-es-de-library.yaml)
choose languages, sources, regions and media; export does not choose/copy a ROM
collection for you.

For a catalog on another machine, run the same server there:

```shell
rom-meta serve --data-root "/path/to/catalog" --addr "192.168.1.10:8787"
```

On the ES-DE machine, use its actual ROM root and the server address:

```shell
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --out "/exports/new-package"
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --out "/exports/new-package" --apply
```

`--platform` and `--platforms` work with `--api` too. ROMs are read locally;
only names, hashes and bounded playlist/serial observations go to the server.
Selected metadata/media returns to the client. No catalog share or server ROM
mount is needed. Local and network export share the same readers and confidence
rules, including MAME; [network details](docs/offline-export.md#export-from-a-remote-catalog)
cover profiles, progress and failures.

Close ES-DE before installation. For a new frontend gamelist, copy the XML into
`<ES-DE-home>/gamelists/nes/` and the media into `<configured-media-root>/nes/`.
Back up existing data; merge an existing gamelist to preserve favorites, play
statistics and emulator overrides. The CLI export stops at package creation.
The [step-by-step ES-DE guide](docs/offline-export.md#quick-start-existing-es-de-roms)
covers actual paths, recognition reports, package contents and batch mode.

**RomM:** the separate [server importer example](examples/romm-importer/README.md)
fills already-scanned native games from the RomMeta API. It runs in the pinned
RomM 5.4.0 environment with native database/Resources access. It is experimental,
version-specific and requires administrator setup; it is not a stock-API upload.

See [export scenarios and coverage](docs/export-scenarios.md) for input formats,
identity confidence, output differences, tests and known gaps. ES-DE is the only
`export --target` implementation in 0.1.0.

## Boundaries

- Mutating catalog commands offer previews; explicit apply or interactive approval
  authorizes writing. Back up data before broad identity changes. Fold retains
  recovery backups; complex split/partition reviews use hash-bound plans.
- Recognition reports exact hash, weaker name or game-only evidence separately.
  CHD game observations and MAME set-name projection do not prove a valid dump.
- Unsupported inputs and ambiguities are reported. Further frontend targets,
  arbitrary disc formats, ROM conversion, emulator setup and universal gameplay
  compatibility are outside 0.1.0.
- Provider metadata/media retains its own terms. A generated frontend package is
  personal output, not permission to redistribute provider content.

## Documentation and development

- [Documentation index](docs/README.md)
- [0.1.0 release notes](docs/releases/0.1.0.md)
- [Contributing and verification](CONTRIBUTING.md)

RomMeta code is licensed under [MIT](LICENSE). Third-party notices accompany
binary releases; vendored Swagger UI retains its own license and notices.
