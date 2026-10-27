# RomMeta

RomMeta prepares a local metadata catalog for ROM collections. Import DAT release
and checksum data, fetch provider metadata and artwork, review regional game
bundles, then generate frontend metadata from the prepared catalog locally or over
your network.
Catalog data stays in readable YAML with source evidence and explicit overrides.
Prepared [translations and hacks](docs/catalog-variants.md) can inherit metadata
from their original game, with an optional exact release, while retaining their
own fingerprints.

![RomMeta web browser showing a game card with cover artwork, metadata and alternate titles](docs/images/catalog-game-card.png)

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

See [source setup](docs/source-setup.md) for accounts/credentials and
[fetch selection](docs/fetch.md) for platform applicability and matching.

## Export targets

| Target | Integration | Output |
| --- | --- | --- |
| ES-DE | Built-in `export --target es-de`, with a local catalog or `--api` | Gamelists, selected media, preview and provenance reports |
| RomM | Separate [server importer example](examples/romm-importer/README.md) | Metadata and media for already-scanned games in the pinned RomM runtime |

See [supported input formats and export scenarios](docs/export-scenarios.md).
The [HTTP API](#http-api) also exposes catalog data and assets to other consumers.

## Install

Extract a Windows **amd64** or Linux **amd64/arm64** archive from
[Releases](https://github.com/divanikus/rom-meta/releases) and verify `SHA256SUMS`.
On Linux, run `chmod +x ./rom-meta`. Add the executable's directory to `PATH`.
Check installation with `rom-meta --version` and `rom-meta --help`.

To build from source, use the Go version in [go.mod](go.mod):

```shell
git clone https://github.com/divanikus/rom-meta.git
cd rom-meta
go build -o bin/rom-meta ./cmd/rom-meta
```

On Windows use `-o bin/rom-meta.exe`. Prebuilt binaries do not require Go.

## Import, enrich and review

Keep the catalog outside the source checkout and ROM directories. Replace example
paths with your own; start with one platform and a small sample.

```shell
rom-meta init "/path/to/catalog"
rom-meta dat plan "/path/to/platform.dat-or-zip"
rom-meta dat import "/path/to/platform.dat-or-zip" --data-root "/path/to/catalog"
rom-meta dat import "/path/to/platform.dat-or-zip" --data-root "/path/to/catalog" --apply
rom-meta sources list --data-root "/path/to/catalog"
```

Follow [source setup](docs/source-setup.md) for accounts/credentials and
`config/sources.yaml`. Then preview enrichment, apply and review folds:

```shell
rom-meta fetch --data-root "/path/to/catalog" --platform gamegear --source launchbox --type metadata --limit 10
rom-meta fetch --data-root "/path/to/catalog" --platform gamegear --source launchbox --type metadata --limit 10 --apply
rom-meta catalog fold --data-root "/path/to/catalog" --platform gamegear --details
rom-meta catalog fold --data-root "/path/to/catalog" --platform gamegear --interactive
rom-meta validate "/path/to/catalog"
```

Repeat fetch to resume; run catalog writers sequentially. See the
[operator workflow](docs/cli-workflow.md) for recovery and review, or
`rom-meta help <command>` for flags.

## Browse

```shell
rom-meta serve --data-root "/path/to/catalog"
```

Open http://127.0.0.1:8787 for the read-only browser. Local browsing requires no
token or profile. Stats starts with one platform; full-catalog scans are explicit,
show progress and support cancellation. HDD/network scans can take minutes.
Video loads on playback.

## HTTP API

The same `serve` exposes catalog metadata and media:

| Route | Purpose |
| --- | --- |
| `/api` | Endpoint index and browser API |
| `/v1/*` | Consumer lookup, export and assets |
| `/docs` | Interactive Swagger UI |
| `/openapi.json` | OpenAPI specification |

[API reference](docs/catalog-api.md) covers schemas and optional shared-token access.

## Export to a frontend

**ES-DE:** generate metadata for your existing ROM files from a prepared catalog:

```shell
rom-meta export --target es-de --platform nes --data-root "/path/to/catalog" --rom-root "/path/to/roms/nes" --out "/path/to/new-package"
```

Review the printed recognition/coverage summary, then repeat with `--apply`.
The new package contains `preview.html`, reports, `gamelists/nes/gamelist.xml`
and selected `downloaded_media/nes/`. ROMs and catalog stay unchanged.
[Profiles](examples/export-es-de-library.yaml) control
[display names](docs/offline-export.md#display-names), languages, sources and media.
Use `--all` or `--platforms` against a parent ROM directory for multiple systems.

For a remote catalog, run the server there:

```shell
rom-meta serve --data-root "/path/to/catalog" --addr "192.168.1.10:8787"
```

On the ES-DE machine:

```shell
rom-meta export --api "http://192.168.1.10:8787" --all --rom-root "/games/roms" --out "/exports/new-package"
```

Review, then repeat with `--apply`. Local and network export share readers and
confidence rules, including MAME. ROM bytes stay on the client; no catalog share
is needed. See the [ES-DE guide](docs/offline-export.md#quick-start-existing-es-de-roms)
and [network details](docs/offline-export.md#export-from-a-remote-catalog).

Close ES-DE before installation. Back up existing metadata and merge by launch
path to preserve favorites, play statistics and emulator overrides. The CLI
creates packages; installation/merge is manual.

**RomM:** the [server importer example](examples/romm-importer/README.md) fills
already-scanned games through native RomM 5.4.1 handlers and Resources. It requires
administrator setup and is version-specific and experimental.
See [export scenarios](docs/export-scenarios.md) for formats, confidence and gaps.
ES-DE is the only built-in `export --target`.

## Boundaries

- Preview catalog changes before `--apply` or interactive approval. Back up broad
  identity changes; advanced split/partition review uses hash-bound plans.
- Reports distinguish exact hashes, weaker names and game-only matches. CHD
  observations and MAME set names do not prove dump validity or gameplay.
- Unsupported formats and ambiguities are reported. ROM conversion/organization,
  emulator setup and additional frontend templates are outside 0.1.0.
- Provider terms still govern downloaded metadata/media and generated packages.

## Documentation and development

- [Documentation index](docs/README.md)
- [0.1.0 release notes](docs/releases/0.1.0.md)
- [Contributing and verification](CONTRIBUTING.md)

RomMeta code is licensed under [MIT](LICENSE). Third-party notices accompany
binary releases; vendored Swagger UI retains its own license and notices.
