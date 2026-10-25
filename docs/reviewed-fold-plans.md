# Reviewed fold plans

Use `catalog fold --review-plan <json>` for an explicitly checked partition of
same-content regional cards whose identity cannot be inferred safely from a
shared provider ID. Confirmed development releases belong inside their game.
Ordinary plans preserve commercial product/edition boundaries; explicit content
equivalence review below can resolve independently verified historical mistakes. See
[regional content identity](regional-content-identity.md). The normal fold still discovers
candidates from metadata. Review plans do not add global title aliases.

```powershell
rom-meta catalog fold --data-root <catalog> --review-plan <plan.json> --details
rom-meta catalog fold --data-root <catalog> --review-plan <plan.json> --apply
```

The first command is read-only. Keep a backup before applying, and stop other
catalog writers. The plan is exclusive of `--platform`, `--match`,
`--editions-only` and `--interactive`.

The version 1 JSON format is:

```json
{
  "version": 1,
  "groups": [
    {
      "platform": "Example Console",
      "reason": "Independent evidence confirms these regional releases have the same content.",
      "members": [
        {
          "folder": "Example Game",
          "game_sha256": "<64 hexadecimal digits from game.yaml>",
          "fingerprints_sha256": "<64 hexadecimal digits from fingerprints.yaml>"
        },
        {
          "folder": "Regional Game Title",
          "game_sha256": "<64 hexadecimal digits from game.yaml>",
          "fingerprints_sha256": "<64 hexadecimal digits from fingerprints.yaml>"
        }
      ],
      "primary_candidates": ["Example Game"]
    }
  ]
}
```

Use exact folder names, not platform aliases. Each group needs two or more
members, a reason and current SHA256 hashes for both files in every member.
Membership is explicit: unlisted cards cannot join even if they share an ID.
A card may occur only once in the plan. Keep uncertain groups out of the plan.
Correct reviewed source errors first, then calculate the hashes.

`primary_candidates` is optional. When present, only these member folders can
provide the card title and default release. Normal region/release preference
ranks the eligible candidates. Use it to select an appropriate regional display title. The complete original release entries and fingerprints still merge.

Every selected member is checked before applying any platform, and selected
hashes are checked again in each staged platform. Only one group's full metadata
is held at a time during preflight; the retained plan contains member references.
Unsupported canonical fields and conflicting duplicate release payloads fail
preflight instead of being silently discarded. The existing platform staging,
merge renderer and file conflict handling perform the actual fold. Directory swaps
retry permission failures for at most 1.5 seconds (five attempts); persistent errors
still trigger rollback, with the backup path reported if rollback also fails.

This is a one-shot reviewed input, not a durable transaction journal. Applying a
platform removes its merged members, so the original plan intentionally becomes
stale. A later-platform failure can leave earlier platforms committed. Preserve
the log and backup; make a new plan for the remaining, still-reviewed inputs or
restore from backup manually. Do not reset hashes blindly to bypass a stale-plan
error. Media paths should be regenerated after folding; ordinary fetch does so
from the current catalog.

Collection-specific membership, supplement roles and review evidence belong in
local review artifacts. The catalog schema has not changed, and building a fresh
catalog does not automatically reproduce those private decisions.

## Confirmed development-release parents

A reviewed group may set `development_parent` to one member's exact folder. This
is an explicit identity decision bound by the existing YAML SHA256 values. It
retains that member as the sole primary and uses its reviewed content key.
Every other member must contain only development releases compatible with a
recognized edition/product already present in the parent. Unknown production or
part qualifiers require this explicit reviewed identity decision and retain their
original values; ordinary automatic grouping does not ignore them. Other commercial cards, addons,
supplements and incompatible known qualifiers are rejected, including on staged
recheck. Existing parent releases are not repartitioned by this operation.
Ordinary plans and automatic/interactive folds cannot retire differing keys.

## Explicit content-equivalence review

A group may instead supply `identity_review`. It contains:

- `content_key`: `reviewed.` followed by a SHA256 hex value for the reviewed identity.
- `previous_keys`: every member folder mapped to its exact current key, including empty keys.
- `releases`: every exact member release name, once, without omissions or additions.
- `references`: nonempty absolute HTTPS URLs identifying the independent proof.

The ordinary reason and current metadata hashes remain mandatory. This is
exclusive of `development_parent`. Both preflight and staged apply reject changed
keys, missing releases or structural game/addon/supplement/compilation mixtures.
It may join independently confirmed regionally named content editions and replace
obsolete keys; original release names, roles, provider blocks and media survive.
This explicit operator decision never becomes a global title or ID exception.
Exact known-authority refresh preserves the resulting reviewed ownership; unknown
new releases remain conservative. Keep the frozen evidence and preservation proof.

The same explicit mechanism may join an original and a reviewed expanded edition of that
game. Retain edition-specific source IDs/media and confirm applicability to each
release before folding. A source family is not a reason to replace provider IDs
to force grouping in a frontend.
