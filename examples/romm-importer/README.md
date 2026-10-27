# RomM server importer example

A separate one-shot Python consumer of the RomMeta HTTP API. It enriches games
already scanned in **RomM 5.4.1** using native handlers/models, DB/Redis and Resources.
It does not patch the installed application, scan ROMs, fetch providers or move ROM
files. This is a version-specific administrator example, not a general RomM plugin
or stock edit-API client. The Go CLI still supplies JSON input/projection helpers.

## Files and requirements

- `importer.py`: plan/apply/check, batches, receipts and explicit refresh.
- `Dockerfile`: digest-pinned RomM 5.4.1 with matching RomMeta.
- Compose/env examples: native mounts, network and user configuration.
- `test_importer*.py`: fake-native tests without a live DB.

Use matching API/CLI/importer revisions. Other native runtimes/schemas fail
preflight; review before upgrading. Preflight checks the actual server heartbeat
before native DB access. The image includes Python/Pillow/RomM.
The job needs DB/Redis, read-only config/ROMs, writable Resources/work and the
native UID/GID. Check ownership instead of recursively changing the library.
Back up native DB/Resources before the first write.

## Prepared translations and other variants

Use matching current API/CLI/importer builds with a prepared
[variant catalog](../../docs/catalog-variants.md). The projector validates
`variants`/`based_on` against source YAML, checks the modified payload's own
fingerprints, and selects provider metadata using the terminal original release.
A different catalog default does not substitute for that base.

Plans retain `providers.derivative` with variant identity, base, evidence level,
language and modification credits. Original provider identifiers refer to the
underlying game; they are not newly assigned translation identities.
Translation date/version/group never replace original developer or release date.
Unsupported native credit/language fields remain explicit provenance.

Patched payload matches, exact prepared names and declared CHD combined-header
matches retain separate evidence. Header/name association requires
`game_binding: true`; neither asserts payload integrity or original-ROM
verification. No No-Intro/Redump/Hasheous signature flags inherit from the base.
Existing owner fields, provider data and locks follow ordinary fill-only rules.

For numbered CHD directories, every observed member must have a declared variant
identity in the same source game. Plans retain `disc_variants` under the derivative
provenance and use the first member's metadata basis. Each member's source credits
and base remain independent; the group does not assert a coherent patch edition
or complete playable disc set. Unknown or conflicting members prevent an update.

Variant-owned media precedes its parent variants and original base. Changes are
still planned/applied through the normal importer workflow; preparing or testing
variants does not deploy an image, modify a live catalog or start another writer.

## 1. Build the job image

From the repository root, using the Go version in `go.mod`:

```shell
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/linux-amd64/rom-meta ./cmd/rom-meta
docker build -f examples/romm-importer/Dockerfile -t rommeta-romm-importer:local .
```

On Windows set GOOS/GOARCH/CGO_ENABLED in PowerShell, or place the extracted Linux
binary at `bin/linux-amd64/rom-meta`. Build from the repository root for COPY paths.
Catalogs, credentials and native library are not copied into the image.

## 2. Configure the server job

In a separate deployment directory, copy the Compose/env examples to `compose.yaml`
and `deployment.env`. Set native config/ROM/Resources/work paths, network and
UID/GID; make work writable by that user. This defines only the importer job.

Create `private/romm.env` with the installation's actual values:

```dotenv
ROMM_DB_DRIVER=postgresql
DB_HOST=existing-db-service
DB_PORT=5432
DB_USER=existing-native-user
DB_PASSWD=replace-with-current-password
DB_NAME=existing-native-database
REDIS_HOST=existing-redis-service
REDIS_PORT=6379
REDIS_PASSWORD=replace-with-current-password
ROMM_BASE_PATH=/romm
ROMM_SERVER_URL=http://romm:8080
```

Use the native server origin reachable from the job for `ROMM_SERVER_URL`
(default `http://romm:8080`); its heartbeat needs no catalog token. Use actual
driver/ports; an absent Redis password may be empty. Put the service's
shared `ROMMETA_API_TOKEN` (at least 32 characters) in `private/api.env`. This job
requires a token even though local browsing does not. Keep env files restricted
and server-side, with raw values and no shell quotes/comments. Compose needs
`env_file` `format: raw` to prevent password interpolation. Validate without
printing expanded secrets:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools config --quiet
```

## 3. Start the catalog API

Run matching `serve` separately with its private token environment. Containerized
catalog/profile mounts can be read-only:

```shell
rom-meta serve --data-root /catalog --profile /config/export-romm-api.yaml --addr 0.0.0.0:8787
```

Use [the profile](../export-romm-api.yaml) and a network reachable by the job
(`rommeta-api` below); use HTTPS across trust boundaries. Catalog mode needs no
ROM mount or per-platform restart. Normal CLI changes refresh automatically;
restart after external catalog edits. See [API limits](../../docs/catalog-api.md).

## 4. Plan, review, apply and repeat

From the deployment directory:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer platforms
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --platform 'Sega Game Gear' --api http://rommeta-api:8787 --out /work/plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/plan.json --apply --out /work/result.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer check --platform 'Sega Game Gear' --api http://rommeta-api:8787 --out /work/check.json
```

Review fields, gaps/ambiguities and media budget. First apply is read-only;
`--apply` stages/verifies media, rechecks source/native state and publishes
sequentially. Use new output names. Do not scan/edit selected games or mutate
the source during apply. `check` is a read-only repeat report, not an apply plan
or integrity scan.

Use `--all`, repeated `--platform`, filename `--match` or per-platform `--limit`.
Bounded parts are generated without a collection ceiling; keep parent, parts and
receipts together outside Git. After partial failure, fix the cause and create a
fresh fill-only plan. Use receipts/before-values for scoped recovery; do not
replay stale plans or restore a whole DB to undo one row.

See [batch/refresh operations](../../docs/romm-import-guide.md),
[cover review](../../docs/cover-refresh.md) and [identity links](../../docs/igdb-links.md).

## Mapping and limits

- Exact cartridge proof covers nine API families. Other regular files/folders
  use complete catalog release names and stat observations, including MAME,
  RVZ/WUX and other cartridge archives. CHD headers and same-edition disc names
  also provide unverified metadata. No dump/set-version proof is inferred.
  `--all` reports missing source systems and unresolved inputs.
- Imports fill blank supported fields/provider slots and Resources, preserving
  owner data, locks, existing media and library/user bindings. Unsupported roles
  and alternate descriptions remain provenance.
- Source-proven IDs/slugs differ from inferred names. Ambiguities never justify
  guessed identity, grouping or verification.
- Assets/native baselines are rechecked; receipts hash new files. Budget staging
  and derivative space. Publication is sequential, not one DB/filesystem transaction.
- New covers use source-hash subdirectories; unbound legacy files stay intact.
  Generated PNG covers omit embedded ICC profiles; source assets stay unchanged.
- Cover refresh/identity repair require separate review. Retain original bytes and
  receipts. Synthetic tests do not establish gameplay or live manual transfer.

## Tests

From the source repository, with Python 3.10+ and Pillow:

```shell
python -m unittest discover -s examples/romm-importer -p 'test_*.py'
go test ./internal/romm ./cmd/rom-meta
```

The tests exercise fake-native preservation, batch/no-op/partial failures, proof
rechecks, stale plans, image normalization/refresh and CHD observations. They do
not require credentials, a running RomM installation or access to a personal library.
