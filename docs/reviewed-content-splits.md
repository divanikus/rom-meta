# Reviewed content splits

Use `rom-meta catalog split --help` for the command contract. The default is a
read-only preflight; `--apply` activates the complete staged platform and retains
its original directory under `backups/`. Stop catalog writers before applying.
No remote metadata/media requests or ROM writes are performed.

A version 1 JSON plan names one platform and explicit groups. Each group has the
original folder, `game_sha256`, `fingerprints_sha256`, a review reason, two or more
parts, a complete extra-file mapping, and optional source routes. Every part has
a unique safe folder, title, opaque `content_key`, and nonempty exact release
names. Exactly one part retains the original folder. All releases must appear
exactly once; every fingerprint must still name one of them. Versioned/derived
and MAME bindings require a separate adapter and are rejected here.

Every file except the two root YAML documents needs its relative path, size,
`mtime_ns`, explicit target folder/path pairs and reason. `sha256` is optional.
Multiple destinations preserve deliberately shared artwork. Unplanned files,
stale inputs, unsafe paths, links, existing targets and collisions fail before
mutation. Media basenames/layouts are retained by the reviewed mapping; a label
in a filename alone does not prove ownership. An identity review uses exact release
directories and actual bytes matched to saved provider media hashes, retaining ambiguous original files on
the original card. Do not treat the opaque directory immediately after `_fold`
as a release directory.

Exact-scoped provider matches follow release ownership. Multi-release records
are copied into each owning partition with filtered release references; every
other payload field is unchanged. Every original release association survives. An optional
`source_routes` entry names source, zero-based `match_index` and destination
folder for a reviewed unscoped record. It cannot contradict an exact release
scope. Unresolved records remain on the original card. Only release-reference fields are projected; other provider payloads are
unchanged. YAML anchors that cannot survive the partition fail preflight. Unknown root metadata remains on the
original card; source attributes accompany moved records. Existing defaults
survive when their release remains; other defaults use configured region policy.

`fold.content_key` persists the reviewed boundary. Different nonempty keys stop
ordinary fold approval modes from recombining partitions; equal keys alone cannot
justify merging. DAT refresh retains this fold metadata. The broader classifier
still protects known roles and unknown qualifiers, so a key is no override for
other content holds. Keep the frozen plan, independent originals and integrity
receipts privately. Catalogs, downloaded media and private reports are not Git
artifacts. Native platform backups can share hard links with current media.

Installing this command does not automatically split any historical cards. Do not replay a
consumed plan. Exact-release `_fold` export provenance is still unfinished.

Explicit identity review allows same-game expanded editions to share a card. An edition label, new features or
different provider IDs alone do not justify splitting. Keep independent games and
different multicart contents separate; preserve exact provider applicability when
joining an original and expanded edition.

DAT refresh protects saved authority-ID/name ownership across reviewed keys.
It replays the partition only when every release has a unique exact
saved name and no conflicting saved ID. Unknown additions or conflicting anchors
remain review cases, even when a changed DAT ID hides a weaker exact-name match.
Shared bytes alone do not establish ownership.

A single game's demos/promos/betas/prototypes are release variants and should not be split merely by role, region or build. A separately reviewed
`development_parent` fold can retire those former stage keys while preserving
true commercial/product boundaries. See [reviewed fold plans](reviewed-fold-plans.md).

Do not split a GOTY/complete edition's included DLC installer from its game disc.
A generic Bonus/Special Disc label is insufficient proof of another product;
check whether it is a development release, package component, independent addon
or multi-game compilation. Required install/play discs stay together. Exact
same-content regional editions stay together even when their commercial names
differ. Structured provider `releases` entries (such as OpenVGDB ROM records) are
projected whole, preserving their nested hashes and metadata.
