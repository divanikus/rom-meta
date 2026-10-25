# Reviewed IGDB identity links

Use this narrow workflow when a card already has a Hasheous IGDB reference but
no reliable IGDB page link. A foreign ID may name another port or another product:
review the official title, aliases and platform first. This command never searches
by name, fetches descriptions/ratings/dates or downloads media. Normal fetch and
normal RomM import remain unchanged and fill-only.

## Review a bounded selection

Keep the review outside Git, for example `<data-root>/.rommeta/reviews/links.json`.
Use a canonical physical platform name and the official expected platform ID.
Each `game` is the relative card directory; `expected_sha256` is the current
`game.yaml` SHA256. A review has 1 to 500 unique cards, all from that platform.

```json
{
  "schema_version": 1,
  "platform": "Example Platform",
  "platform_id": 123,
  "entries": [
    {
      "game": "platforms/Example Platform/Example Card",
      "expected_sha256": "replace-with-64-hex-current-game-yaml-sha256",
      "from_id": 456,
      "action": "confirm",
      "reason": "Official exact platform/product verified",
      "evidence": ["https://www.igdb.com/games/verified-source-slug"]
    }
  ]
}
```

Actions are `confirm` (retain ID), `replace` (also requires the explicitly reviewed
`to_id`) and `suppress` (no verified target; do not guess). `from_id` must agree
with every nonempty Hasheous IGDB reference in the card. Conflicting own IGDB
snapshots remain protected. Evidence is a public HTTPS page, never a token URL.
For Windows, `(Get-FileHash -Algorithm SHA256 -LiteralPath $gameYaml).Hash.ToLower()`
computes the input hash; Linux `sha256sum` is equivalent.

## Refresh only these known identities

Use existing IGDB credentials in `config/secrets.yaml` or the configured environment
secret references. Existing OAuth token handling is reused; never put credentials
in arguments or the review. From a machine with catalog access and IGDB connectivity:

```shell
rom-meta fetch --data-root /path/to/metadata --platform 'Example Platform' --source igdb --type metadata --refresh-links --link-review /path/to/links.json
rom-meta fetch --data-root /path/to/metadata --platform 'Example Platform' --source igdb --type metadata --refresh-links --link-review /path/to/links.json --apply
```

The first command may perform a minimal authenticated official ID lookup and
write its private token/query cache, but does not change catalog YAML. Read its
card/action/old/new/slug table. The distinct link cache cannot replace a full
metadata response. The second command validates every source hash and all target
ID/name/slug/page/platform pairs before the first atomic source write. Back up
selected YAML files before apply. This is resumable per-file mutation, not a
multi-file transaction. A partial failure reports an error and needs a fresh review
for remaining changed inputs.

Saved matches contain only ID/title/slug/page/platform names/IDs and
`capture: identity-links`. A previous `not_found` status becomes `identity_only`;
its historical fields and every other provider/owner field remain. No regional
release or payload verification is inferred from an ID lookup. `replace`/`suppress`
are durable curated `manual.provider_references.igdb` decisions with source
`hasheous`, old/new IDs, reason and evidence; raw Hasheous snapshots/signatures
stay intact. The mapper retains those rejected foreign IDs as reference-only
provenance instead of injecting them again. See ADR 0092.

Repeat the exact command after success: identical completed links/decisions are
pure no-ops, without querying IGDB, even though the original before-SHA is stale.
That exception authorizes no new write. A different proposed change requires an
updated review/current SHA. A read-only API snapshot pins old YAML, so explicitly
restart that configured API after reviewing the source diff; wait for authenticated
health after startup hashing, then create a fresh consumer plan.

## Correct an already inherited native ID

Source curation alone does not authorize overwriting an existing RomM identity.
The [server importer](romm-server-importer.md) has a separate opt-in:

```shell
docker compose --env-file deployment.env -f compose.yaml --profile tools run --rm importer plan --api http://rommeta-api:8788 --platform-id 49 --romm-rom-root 'roms/Sega - Game Gear' --repair-igdb-references --out /work/igdb-plan.json
```

A correction requires the exact reviewed old ID, empty native slug and own IGDB
metadata, no IGDB/slug locks, and a verified direct replacement snapshot (or no
active IGDB slot for suppression). It cannot replace another owner ID or populated
metadata. Read each `REVIEWED IGDB` line and the manifest's before-state/decision.
A clear writes `igdb_id: null`; a replacement writes the reviewed ID and source
slug. This can legitimately split groups previously merged by a wrong ID.
Native Hasheous IDs, proof, covers, dates and all unrelated owner data are preserved.

Use the ordinary `apply --plan ...` preflight, then `--apply --out <new-receipt>`
and a fresh plan with the same explicit repair flag. Successful repeat has zero
changes and zero media bytes. An old applied plan is stale; do not replay it.
Do not use global force-overwrite or a title-based substitute for this workflow.
