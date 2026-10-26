# Source setup

Configure acquisition sources here. Local browsing/export needs no provider credentials.
Paths below are relative to your catalog, not the source checkout.

## Requirements

| Source | Credentials / prerequisites |
| --- | --- |
| LaunchBox | No account/key. Public Metadata.zip is downloaded automatically if absent. |
| OpenVGDB | No account/key. Its database snapshot is downloaded automatically if absent. |
| Hasheous | No credentials for the configured public endpoint. Cards need usable ROM hashes or supported MAME set/component evidence. |
| IGDB | `client_id`, `client_secret` from a Twitch **Confidential** application; Twitch account with 2FA. See [IGDB setup](https://api-docs.igdb.com/#account-creation). RomMeta obtains/caches the OAuth token automatically. |
| ScreenScraper | `username`, `password`, `dev_id`, `dev_password`. RomMeta requires all four and bundles no developer pair. Request authorized developer access through the ScreenScraper forum; see [API access](https://www.screenscraper.fr/webapi2.php). An ordinary account alone is insufficient. |
| EmuMovies | `username`, `password` for an account with FTP access. Use the credentials shown under **FTP Login**; see [FTP FAQ](https://emumovies.com/faq/2-questions-about-the-ftp/). Access/quality depends on the account. |

EmuMovies uses FTPS. RAR/7z artwork packages need `7z`/`7zz`/`7za` (RAR also accepts
`unrar`/`rar`) in `PATH`, or an explicit
`sources.emumovies.tools.archive_extractor` path in `config/sources.yaml`.
ZIP packages and direct video files do not need this extractor.

## Configure

1. Run `rom-meta init "<catalog>"` if needed; existing config is preserved.
2. Copy `config/secrets.example.yaml` to `config/secrets.yaml`, preserving an existing
   secrets file. Fill only providers you use; omit the other sections.

```yaml
schema_version: 0
sources:
  igdb:
    client_id: "your-twitch-client-id"
    client_secret: "your-twitch-client-secret"
  screenscraper:
    username: "your-screenscraper-login"
    password: "your-screenscraper-password"
    dev_id: "your-authorized-developer-id"
    dev_password: "your-authorized-developer-password"
  emumovies:
    username: "your-ftp-login"
    password: "your-ftp-password"
```

Keep this file private and out of Git. Default `sources.yaml` already maps these
fields with `credentials.type: secrets`; do not put their values in that file.

3. Edit `fetch` lists in `config/sources.yaml`. This example enables every connector;
   remove sources you have not configured. Block and inline source lists are supported:

```yaml
fetch:
  metadata:
    - launchbox
    - openvgdb
    - hasheous
    - igdb
    - screenscraper
  artwork:
    - launchbox
    - openvgdb
    - igdb
    - screenscraper
    - emumovies
  video:
    - emumovies
```

New catalogs default to LaunchBox/OpenVGDB/Hasheous metadata, LaunchBox/OpenVGDB
artwork and no video. Supplying credentials does not enable a source automatically.
Platform applicability and ordering are covered in [fetch selection](fetch.md).

## Check

```shell
rom-meta sources list --data-root "<catalog>"
rom-meta fetch --data-root "<catalog>" --platform nes --source igdb --type metadata --limit 1
```

`sources list` reports missing fields without printing values; it does not validate
remote credentials. Preview a provider you configured before adding `--apply`.
Preview may contact providers/cache data. `--limit` limits cards, not initial
LaunchBox/OpenVGDB snapshot downloads. Missing credentials are errors; account
quotas and provider availability still apply.

## Environment variables

To use environment values, replace only `sources.igdb.credentials` in
`config/sources.yaml`, for example:

```yaml
credentials:
  type: env
  client_id: ROMMETA_IGDB_CLIENT_ID
  client_secret: ROMMETA_IGDB_CLIENT_SECRET
```

Set those variables for the process running RomMeta. Names are explicit references;
arbitrary environment variables do not override the default secrets-file mapping.
Other providers use the same mechanism with their required field names.

`ROMMETA_API_TOKEN` is separate: it protects your own `serve`/consumer connection,
not provider access. See [API protection](catalog-api.md#optional-access-protection).
