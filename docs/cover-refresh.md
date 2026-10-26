# Prefer a flat front and refresh reviewed native covers

Use [generated batch refresh](romm-import-guide.md#correct-grouping-and-refresh-cover-choices-without-reimporting-other-media)
for ordinary updates; the commands below support narrow per-cover review.

`examples/export-romm-api.yaml` opts into release-region-first, then flat-front
preference. Other defaults stay unchanged. Style is independent of artwork variant.
Saved LaunchBox categories and ScreenScraper box-2D provide exact path evidence;
EmuMovies/IGDB remain unknown. Provider names, filenames and aspect ratios prove
no style; inspect the actual image.

```yaml
selection:
  media:
    cover:
      order: [region, region-preference, style, variant, source, scope, format]
      styles: [flat-front, unknown, front-spine]
      variants: [original, unknown, reconstructed, fanart]
```

Place `region-preference` after `region` to break multi-region `$release` ties
using configured country order. This never prefers nonrelease USA over release
Japan. Style and regional preference are independent opt-ins; default order stays
`[region, source, format]` with implicit scope. Explicit orders and exact-release
`media_overrides` remain authoritative. See [selection rules](offline-export.md#selection-profile).

Unknown style stays unknown, not inferred front-spine. API/manifests retain
`style`, `style_evidence` and bound `source_type`. Source images are not edited;
regional square covers stay square and native thumbnails resize proportionally.

## Human baseline, selection preview, apply and repeat

Imports normally fill blanks. For explicit cover replacement, update the importer
job if needed and capture a baseline **before changing the profile/restarting API**:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --api http://rommeta-api:8787 --platform 'Sega Game Gear' --out /work/covers-before-plan.json --export-cover-review /work/cover-review.json
```

The helper records old source-cover SHA256, native large/small paths, derivative
SHA256/size and before-plan hash without writes. This is current state, not adapter
ownership: compare accepted receipts/owner choices and remove unreviewed entries.
Occupied remote URLs, path locks and incomplete pairs are excluded.

Back up the profile, select `examples/export-romm-api.yaml`, restart only the
read-only API and wait for health (authenticated if protected). Do not patch/restart
RomM or change library ACLs.

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --api http://rommeta-api:8787 --platform 'Sega Game Gear' --refresh-covers /work/cover-review.json --out /work/covers-plan.json
```

Review each `REVIEWED COVER` line and manifest: old/new source hashes, provider,
style/evidence, paths and bytes. For cover-only work, patches must contain only
`path_cover_l`/`path_cover_s` and files only cover derivatives. The flag can still
fill blank metadata; hold unexpected fields for separate review. Source categories
and regions are not pixel guarantees.

Replacement needs exact reviewed current paths and SHA256/size. This explicit
operation may bypass `url_cover`, but not owner path locks or unrelated fields.
Identical source bytes are a no-op; later owner changes require fresh review.

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/covers-plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/covers-plan.json --apply --out /work/covers-receipt.json
```

Full preflight runs before writes. New sources stage with SHA/size checks;
derivatives prepare and preflight repeats before publication. New paths are
`roms/<platform>/<id>/cover/rommeta-<new-source-sha>/big.png` and `small.png`.
Old bytes remain; receipts record source/style/before/after outcomes. No scan,
rematch, bulk overwrite or client Resources share is needed.

A fresh same-scope plan with the same review and a new `--out` should show zero
changes/media bytes. Deterministic paths do not authorize overwriting later edits;
never replay a stale applied plan. Keep original covers/review until recovery is
no longer needed. Recovery restores only the old native pair from private review,
preserving unrelated owner changes, through the matching
[native runtime](romm-server-importer.md).
