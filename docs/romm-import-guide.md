# Import offline metadata into RomM

Use the prepared RomM 5.4.1 importer environment. An administrator configures
native DB, library, Resources and token access once; see [setup](romm-server-importer.md).
A website login is insufficient. Your computer needs no Resources share.

## 1. Scan games in RomM

Scan your ROM folders in RomM first. The importer enriches those existing games,
not every catalog card. List native platform names/aliases in its environment:

```shell
python importer.py platforms
```

## 2. Start rom-meta once for the whole metadata catalog

Supply the token via private environment, then run:

```shell
rom-meta serve --data-root "<metadata catalog>" --profile "<export-romm-api.yaml>" --addr 0.0.0.0:8787
```

Use [the profile](../examples/export-romm-api.yaml) and an address reachable by
the importer. Leave this catalog-only service running across platforms; no
source-side system ROM folder is needed.

## 3. Prepare an import

All scanned platforms:

```shell
python importer.py plan --all --api http://<rom-meta-host>:8787 --out plan.json
```

One native name/alias:

```shell
python importer.py plan --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --out plan.json
```

An ordered selection (duplicate aliases for one platform fail):

```shell
python importer.py plan --platform "Sega Master System/Mark III" --platform "Game Boy" --platform "Game Boy Color" --platform "Sega Mega Drive/Genesis" --api http://<rom-meta-host>:8787 --out first-four.json
```

Systems run sequentially. IDs/paths and bounded `plan-parts` are generated;
keep parent, parts and receipts together. There is no collection ceiling;
`--limit 20` deliberately samples each supported platform. Review changes,
gaps/ambiguities and media space; hashing/selected media can take time.

Provider IDs respect release scope. Ambiguous providers are omitted while other
metadata remains. Fingerprint sizes accept YAML integers or decimal strings;
invalid byte counts and transport/document/proof errors stop planning. Renamed
translations never inherit retail identity by title similarity.

Exact cartridge readers cover nine API families. CHD headers and complete catalog
release names extend metadata lookup to other files and installed folders,
including MAME sets, RVZ, WUX and other cartridge archives. These matches remain
**unverified**; no payload, set-version or emulator compatibility is inferred.
All available catalog systems participate in `--all`; absent source systems and
ambiguous/unknown names are reported. Native files and manual state are preserved.
See [catalog name observations](catalog-api.md#catalog-names-for-other-formats).

To narrow an optical selection:

```shell
python importer.py plan --platform "<optical-platform>" --match "<filename-fragment>" --api http://<rom-meta-host>:8787 --out optical-plan.json
```

Repeated `--match` means case-insensitive OR, before `--limit`. Use the same filters
with `check`, `cover-baseline` or `refresh`; exclusions are reported separately.
Native names/aliases must resolve to one catalog capability or planning stops.
See [optical evidence](catalog-api.md#chd-game-observations).

## 4. Check and import

After review:

```shell
python importer.py apply --plan plan.json
python importer.py apply --plan plan.json --apply --out result.json
```

First apply preflights without writes; `--apply` stages/verifies media and actual
input proofs, then imports generated parts. Fill-only preserves owner data,
covers and locks. Do not scan/edit selected games or mutate the source during
apply. Reload RomM and inspect game pages.

Receipts record completed parts. After failure, fix the cause and create a fresh
plan with new filenames; completed imports remain. For a read-only repeat:

```shell
python importer.py check --all --api http://<rom-meta-host>:8787 --out check.json
```

Use the same platform/filter selection. Unchanged imports report zero changes/
media bytes. `check` compares metadata/stored references and exact ROM proofs,
without rereading Resources, so it is not a media-content/existence audit. Its
report cannot be applied. New writes need `plan` with full resource-baseline checks.

## 5. Add games or metadata later

Scan new games and repeat steps 3–4 with new output names. Normal CLI catalog/profile
changes refresh the API automatically; external catalog edits need a restart.
The same service handles all supported systems. Fixed-selection/native-ID commands
remain available for scoped advanced operations; they are not ordinary fill-only import.

## Correct grouping and refresh cover choices without reimporting other media

Upgrade API/importer together. Capture the baseline **before** changing cover policy:

```shell
python importer.py cover-baseline --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --out covers-before.json
```

Use the supplied region-first profile, then restart only rom-meta and prepare/apply:

```shell
python importer.py refresh --platform "Sega Game Gear" --api http://<rom-meta-host>:8787 --cover-baseline covers-before.json --out update.json
python importer.py apply --plan update.json --apply --out update-result.json
```

Use matching repeated platforms or `--all` on baseline/refresh. Review the quality
report: conflicts stay held; game-only matches remain unverified. Apply stages
files and checks the plan immediately before writes, rereading changed ROMs; a
separate dry run is optional.

A cover changes only if source choice differs and the current unlocked pair
matches the baseline. Old bytes remain; other artwork/video/manuals are not
reread/imported. No JSON authoring is needed. Repeat `refresh` with the same
baseline and new output name to confirm zero changes/media bytes.

`--reuse-plan previous-update.json` (also prior generated import/check/baseline)
reuses unchanged ID/name/size observations for a draft; new games are read normally.
Changed targets are always reread before writing, so reuse cannot create proof.
Keep the previous parent/parts together.
