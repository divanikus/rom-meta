# Prefer a flat front and refresh reviewed native covers

For the current generated multi-platform metadata/cover workflow, use
[the import guide](romm-import-guide.md#correct-grouping-and-refresh-cover-choices-without-reimporting-other-media)
for the normal generated workflow. The explicit per-cover review commands below
are available for advanced narrow operations.

The explicit `examples/export-romm-api.yaml` cover policy first matches the release
region, then prefers a known flat front over an unknown/front-plus-spine image. Other export defaults and bundled
profiles retain their default selection; flat preference is opt-in. Style is separate from original/reconstructed/
fanart quality. Exact saved downloader target/legacy paths bind LaunchBox front
categories and ScreenScraper box-2D. EmuMovies and IGDB covers remain unknown;
provider name, filename shape or a narrow aspect ratio cannot establish style.
Categories can still contain misclassified pixels: review the selected art.

```yaml
selection:
  media:
    cover:
      order: [region, region-preference, style, variant, source, scope, format]
      styles: [flat-front, unknown, front-spine]
      variants: [original, unknown, reconstructed, fanart]
```

The optional `region-preference` criterion must follow `region` in `order`.
Only inside a matching multi-region `$release` tier does it use the explicit
configured region preference to break ties before filename ordinal: `[usa, japan, europe, '*']` can prefer USA
over Brazil when both belong to that release. A Japan-only release still prefers
Japan over nonrelease USA; there is no global USA-first rule. The other existing
scope/region/source/format priorities remain. Without `region-preference`,
`region` retains its original affinity-only tie behavior, even when styles are
configured. Regional preference can also be enabled without styles. Default
media keeps `[region, source, format]` and implicit scope priority; an explicit
custom order remains authoritative. Existing
`media_overrides` still wins as a deliberate exact-release file choice. The current
classifier does not label unknown artwork as front-spine; both are honest fallback
values, never image recognition. The API/selection manifest retains `style`,
`style_evidence` and bound `source_type` for review. Source bytes are not renamed,
cropped, stretched or edited. Regional square covers stay square. Native PNG
thumbnails may shrink proportionally, retaining natural aspect ratio.

## Human baseline, selection preview, apply and repeat

Existing native imports remain fill-only. To deliberately refresh current covers,
first run this **before changing the source profile or restarting the API** from
the configured NAS deployment directory. Upgrade the separate importer job first
if necessary; the API may still serve the old accepted selection.

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --api http://rommeta-api:8787 --platform 'Sega Game Gear' --out /work/covers-before-plan.json --export-cover-review /work/cover-review.json
```

The read-only helper exports the selected old **cover asset** SHA256, native large/
small paths, actual derivative SHA256/size and the hashed before-plan reference.
This is a current baseline, **not proof of adapter ownership**. Compare it with
accepted import plans/receipts and owner choices; remove entries you have not
explicitly reviewed. No hand-assembled per-ROM JSON is required. Occupied remote
URLs, explicit cover-path locks and missing/incomplete cover pairs are excluded.
Source files and native rows are untouched by this command.

Back up the API profile, use the explicit cover policy in `examples/export-romm-api.yaml`, then
restart only the separate read-only API. Wait for authenticated health after
snapshot hashing. Do not restart/patch the native RomM App or change library ACLs.

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --api http://rommeta-api:8787 --platform 'Sega Game Gear' --refresh-covers /work/cover-review.json --out /work/covers-plan.json
```

Read every `REVIEWED COVER` line: old/new source asset SHA, provider, style/evidence,
new paths and byte budget. The private manifest includes actual old-cover proof and
native before-state. For a cover-only operation, confirm every changed `patch`
contains only `path_cover_l`/`path_cover_s` and every new file is a cover derivative.
This flag adds reviewed cover replacement to the ordinary fill-only planner; it
does not silently disable other blank metadata fields. Hold unexpected metadata
changes for a separate reviewed import. Inspect the chosen source images as well
as the labels: provider category and region are not pixel-level guarantees.
Only exact reviewed current cover paths and SHA256/size authorize replacement.
The custom-cover `url_cover` lock is bypassed only for this explicit reviewed
operation; owner path locks and all unrelated fields remain protected. Same old
source bytes are a no-op. Nothing implicitly authorizes a changed owner cover.

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/covers-plan.json
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer apply --plan /work/covers-plan.json --apply --out /work/covers-receipt.json
```

Complete preflight runs before any write. Every unique new source is staged and
SHA/size checked, native derivatives prepared and preflight repeated before the
first publication. New ordinary per-ROM files live in
`roms/<platform>/<id>/cover/rommeta-<new-source-sha>/big.png` and `small.png`.
Existing cover bytes stay untouched for selective recovery. The reviewed cover-only
plan changes only native cover paths; the receipt retains source/style/old/new
proof and outcomes.
No scan, rematch, bulk metadata overwrite or client Resources share is involved.

Create a fresh same-scope plan using the same `--refresh-covers` review and a new
`--out` filename: successful repeat has zero changes/media bytes. Already selected
deterministic new paths are a pure no-op, never authority to overwrite later owner
byte edits there. Another cover path/old-byte change requires a fresh explicit
review. Keep the old covers until recovery evidence is no longer needed; this
command does not delete them. Do not replay a stale applied plan.

Selective recovery restores the old two native paths from the private review;
old bytes remain in Resources. Preserve unrelated new metadata/owner edits, rather
than restoring a whole database. Use the matching native runtime/administrator
workflow for this exceptional recovery, then verify the chosen images and fresh
plan. The ordinary [server job setup](romm-server-importer.md) applies.
