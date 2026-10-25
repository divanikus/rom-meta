# RomM server importer example

A separate one-shot Python consumer of the RomMeta HTTP API. It enriches games
already scanned in **RomM 5.4.0** using native handlers/models, DB/Redis and Resources.
It does not patch the installed application, scan ROMs, fetch providers or move ROM
files. This is a version-specific administrator example, not a general RomM plugin
or stock edit-API client. The Go CLI still supplies JSON input/projection helpers.

## Files and requirements

- `importer.py`: plan/apply/check, generated batches and receipts, explicit refresh.
- `Dockerfile`: official RomM 5.4.0 runtime pinned by digest, plus matching RomMeta.
- `compose.example.yaml` and `deployment.env.example`: native mounts/network/user.
- `test_importer*.py`: synthetic/fake-native regression tests; no live DB needed.

Use the source API, Go helper binary and importer from the same project revision.
A different native runtime/schema fails preflight; upgrading RomM requires reviewing
this integration first. The image supplies Python/Pillow/native RomM dependencies.
The job needs the existing native DB/Redis connection, read-only config and ROM
library, writable Resources and a separate work folder. Run it as the native RomM
UID/GID; check actual ownership, rather than recursively changing the library.
Use a backup of the native database/Resources before your first write.

## 1. Build the job image

From the repository root, with the Go version from `go.mod`:

```shell
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/linux-amd64/rom-meta ./cmd/rom-meta
docker build -f examples/romm-importer/Dockerfile -t rommeta-romm-importer:local .
```

On Windows, set GOOS/GOARCH/CGO_ENABLED through PowerShell environment variables,
or copy the extracted Linux release executable to `bin/linux-amd64/rom-meta`.
Build from the repository root so the Dockerfile COPY paths resolve. No catalog,
credentials or native library is copied into the image.

## 2. Configure the server job

Copy `compose.example.yaml` to a separate deployment directory as `compose.yaml`;
copy `deployment.env.example` as `deployment.env`. Fill the existing config/ROM/
Resources/work paths, Docker network and native UID/GID. Create the work directory
with that user's write access. The existing RomM installation remains separately
managed; this Compose file defines only the one-shot importer job.

Create `private/romm.env` there using the current installation's values:

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
```

Use the installation's actual driver/ports; an absent Redis password can be empty.
Create `private/api.env` with `ROMMETA_API_TOKEN` set to a shared token of at least
32 characters, also configured on the RomMeta service. This example requires that
token even though ordinary local `serve` browsing does not. Use raw values without
shell quotes/comments. Keep private env files server-side with restricted access.
Compose must support `env_file` `format: raw` so passwords are not interpolated.
Do not print expanded configuration containing secrets. Validate without printing it:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools config --quiet
```

## 3. Start the catalog API

Run the matching `rom-meta serve` separately, with catalog/profile read-only where
containerized. Set its shared token through the service's private environment:

```shell
rom-meta serve --data-root /catalog --profile /config/export-romm-api.yaml --addr 0.0.0.0:8787
```

Use [the example profile](../export-romm-api.yaml) and a private network reachable
from the importer (`rommeta-api` in the commands below). HTTPS is appropriate when
traffic crosses that network boundary. Catalog mode needs no source-side ROM mount
or per-platform server restart. Restart after changing catalog/profile data.
See [the API guide](../../docs/catalog-api.md) for route/evidence limits.

## 4. Plan, review, apply and repeat

Run from the deployment directory:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer platforms
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --platform 'Sega Game Gear' --api http://rommeta-api:8787 --out /work/plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/plan.json --apply --out /work/result.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer check --platform 'Sega Game Gear' --api http://rommeta-api:8787 --out /work/check.json
```

Review proposed fields, skipped/ambiguous inputs and media byte budgets before
apply. The first apply performs preflight without native writes; `--apply` stages
and verifies media, rechecks source/native state and publishes sequentially. Use
new output filenames. Do not scan/edit selected games or mutate the source during
apply. `check` is a read-only repeat report, not an apply plan or media integrity scan.

Use `--all` for all scanned platforms, repeated `--platform` for a selection,
`--match` for filename substrings and `--limit` for an explicit per-platform sample.
There is no total 1000-file ceiling. Plans generate bounded parts automatically;
keep the parent, part directory and receipts together outside Git. On partial
failure, completed parts stay recorded: fix the cause and create a fresh fill-only
plan. Do not replay a stale partially applied plan or restore an entire database
just to undo one row. Inspect receipts/before-values for a scoped native recovery.

The [operator guide](../../docs/romm-import-guide.md) covers full batches, optical
inputs and metadata/cover refresh. [Cover review](../../docs/cover-refresh.md) and
[identity links](../../docs/igdb-links.md) describe exceptional explicit changes.

## Mapping and limits

- Exact cartridge proofs support the nine catalog API reader families. Typed CHD v5
  singles and ordinary 2..32-disc same-edition folders supply unverified game
  metadata. Header/size/mtime checks are not Redump or whole-file integrity proof.
- Ordinary imports fill blank supported native fields/provider slots and media,
  retaining owner data, locks, existing covers and library/user bindings. Available
  shared artwork/screenshots/video/manuals use native Resources. Unsupported roles
  and alternative descriptions remain source provenance, not invented native fields.
- Source-proven IGDB slugs/IDs are distinct from inferred title matches. Provider
  ambiguities remain reported; they do not justify guessed grouping or verification.
- Selected source assets and exact native baselines are rechecked before writes;
  new files have receipt hashes. Work/Resources need space for staged media and
  derivative images. Native publication is sequential, not a single DB/filesystem
  transaction across the whole library.
- New import is not an overwrite policy. Cover refresh and identity repair require
  their separate generated baseline/review. Keep original bytes and receipts for
  selective recovery. Actual gameplay and live manual transfer are not established
  by the synthetic tests.

## Tests

From the source repository, with Python 3.10+ and Pillow:

```shell
python -m unittest discover -s examples/romm-importer -p 'test_*.py'
go test ./internal/romm ./cmd/rom-meta
```

The tests exercise fake-native preservation, batch/no-op/partial failures, proof
rechecks, stale plans, image normalization/refresh and CHD observations. They do
not require credentials, a running RomM installation or access to a personal library.
