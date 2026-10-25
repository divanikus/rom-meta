# Import offline metadata into RomM

RomM, `rom-meta` and the separate importer must be installed, with their one-time
connections configured. The importer runs in its prepared RomM 5.4.0 environment;
a website login alone does not configure it. An administrator sets native database,
library, Resources and token access once; see [setup](romm-server-importer.md).
Your computer does not need access to RomM's Resources folder.

## 1. Scan games in RomM

Add your ROM folders in RomM and run its ordinary scan. The importer adds metadata
and media to those existing games. It does not add every game in the larger offline
metadata catalog to your library.

To see the actual names and aliases, run in the importer's prepared environment:

```shell
python importer.py platforms
```

## 2. Start rom-meta once for the whole metadata catalog

Set the configured token through your secret/environment file, then run:

```shell
rom-meta serve --data-root "<metadata catalog>" --profile "<export-romm-api.yaml>" --addr 0.0.0.0:8787
```

Use the supplied [profile](../examples/export-romm-api.yaml). Leave the service
running through the import. No system ROM folder or platform is selected on this
side; you do not change this command when switching systems. The API reads only
the offline catalog. Its address must be reachable from the importer on the
configured private network.

## 3. Prepare an import

For every scanned platform and game in RomM:

```shell
python importer.py plan --all --api http://<rom-meta-host>:8787 --out plan.json
```

Or select one complete name/alias from `platforms`:

```shell
python importer.py plan --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --out plan.json
```

For a batch of four, repeat the same option; the order is preserved:

```shell
python importer.py plan --platform "Sega Master System/Mark III" --platform "Game Boy" --platform "Game Boy Color" --platform "Sega Mega Drive/Genesis" --api http://<rom-meta-host>:8787 --out first-four.json
```

All games of just those systems are considered, without a sample limit. Apply
uses the commands below, substituting `first-four.json`. For a repeat, use the
same four platform names with `check`. This is four
sequential systems, not four simultaneous conversion/download jobs. Duplicate
aliases for the same native platform are rejected before creating a plan.

IDs and paths are automatic. Read the printed per-platform report: exact matches,
unsupported systems/formats and unrecognized files have separate reasons.
Provider IDs first respect explicit release scope. An ambiguous provider is
reported and omitted while other available metadata is retained. No ID is picked
to force grouping; transport,
source-document and payload/proof errors still stop planning. Renamed
translations never inherit retail identity or verification by title similarity.

You do not write JSON. `plan.json` and its `plan-parts` folder are generated
outputs; keep them together. The report shows proposed changes and media space.
Default processing includes all games, with no 1000-file ceiling. For a deliberate
sample, add `--limit 20` (per supported platform). Progress is printed while reading
ROMs, looking up metadata and projecting native changes. Selected images/videos
can still require substantial reading time and disk space.

Exact catalog lookup supports Game Gear, Master System, Mega Drive, NES, SNES,
Game Boy, Game Boy Color, Game Boy Advance and Nintendo 64 in their supported
cartridge formats. CHD-capable optical systems also support **unverified game
metadata** for standalone CHD v5 files and ordinary same-edition multidisc folders.
The importer keeps the existing RomM folder card and every disc file. It reads
only headers, sizes and mtimes; this does not establish Redump or CHD integrity.
Mixed/missing/ambiguous disc inputs and other unsupported formats are reported.

For a small optical selection, filter by filename:

```shell
python importer.py plan --platform "<optical-platform>" --match "<filename-fragment>" --api http://<rom-meta-host>:8787 --out optical-plan.json
```

Repeat `--match` for filename substrings (case-insensitive OR). Filtering happens
before optional `--limit`; the report distinguishes deliberate exclusions from
unsupported/unmatched games. Use the same filters with `check`, `cover-baseline`
or `refresh`. Omitting them selects the whole platform as before. Continue with
the ordinary apply commands below, substituting `optical-plan.json`. Platform
names and filesystem aliases may differ between the two utilities: one exact
native system/name alias must identify a unique catalog capability; ambiguity
stops rather than choosing a platform. See [optical evidence](catalog-api.md#chd-game-observations)
for the intentionally limited checks.

## 4. Check and import

After reviewing the report:

```shell
python importer.py apply --plan plan.json
python importer.py apply --plan plan.json --apply --out result.json
```

The first command checks without native writes. The second stages/downloads media,
checks the real input proofs and imports one generated part at a time. Ordinary
fill-only behavior preserves existing owner metadata, covers and locks. Do not
scan/edit selected games or update the source catalog during apply. Reload RomM
and check a few game pages afterwards.

`result.json` references automatically generated per-part receipts. If a part fails,
processing stops and completed parts remain recorded. Correct the reported problem
and make a fresh plan with new output names; already imported data is preserved.
For an ordinary repeat check, use the same selection with `check`:

```shell
python importer.py check --all --api http://<rom-meta-host>:8787 --out check.json
```

Replace `--all` with the same repeated `--platform` names to check just that batch.
An unchanged import reports zero changes and zero proposed media bytes. This
read-only command compares metadata and stored media references without reading
existing Resources files again. Exact ROM identity checks remain. It does not
audit media existence/content; downloaded bytes were checked during import. Its
report cannot be applied. Use `plan` before any new write; normal planning/apply
still capture and recheck complete native resource baselines.

## 5. Add games or metadata later

Scan added games in RomM. Restart rom-meta after changing its catalog or profile,
then repeat steps 3-4 with new filenames. The same API configuration handles every
supported platform. No manual per-platform API restart or ID/path list is needed.

The previous fixed-selection API and explicit native ID/path commands remain
available for existing scripts and scoped repair/cover review operations. Those
advanced repair/review flags are not part of ordinary batch fill-only import.

## Correct grouping and refresh cover choices without reimporting other media

Upgrade the API and importer together. Before changing the explicit cover profile,
generate a baseline using the current profile:

```shell
python importer.py cover-baseline --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --out covers-before.json
```

Then use the supplied region-first profile and restart only rom-meta. Prepare and
apply the update:

```shell
python importer.py refresh --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --cover-baseline covers-before.json --out update.json
python importer.py apply --plan update.json --apply --out update-result.json
```

Use repeated platform names or `--all` on both baseline/refresh commands. Review
the printed quality report: source identity conflicts remain held, while game-only
bindings are explicitly unverified. Apply stages temporary files and checks the
generated plan once just before writing, rereading only changed ROMs; a separate
dry run is optional. Existing covers change only when their source choice
differs and the current unlocked cover still matches the baseline. Old cover bytes
remain; other images, videos and manuals are not reread or imported. No JSON authoring
is required. Repeat `refresh` with the same baseline and a new output filename to
check for zero changes and zero media bytes.

For large repeat updates, add `--reuse-plan previous-update.json` (or a previous
generated import/check/baseline) to `refresh`. Unchanged ID/name/size observations
are reused for the draft; newly scanned games are read normally. Changed targets
are always reread before writing, so this option cannot declare an unverified ROM
verified. Keep the previous parent and its parts together.
