# Regional content identity

DAT import and catalog fold share the content classifier in
`internal/contentidentity`. It contains no game-title, publisher or provider-ID
exception table. MAME's versioned set grouping remains a separate adapter.

## Two requirements for a merge

A merge needs both compatible release content and confirmed identity on the
same platform. Discovery links alone do not satisfy either requirement.

Content classification removes recognized region, language, retail revision,
numbered disc/side, technical serial and verified-dump qualifiers. Demo/trial,
beta/prototype, sample, preview/debug and promotional stages are distinct releases
inside the same confirmed game, including different regions and build dates.
Their names, tags, build detail and fingerprints remain distinct. Addons,
expansions, updates, independently issued supplements and commercial editions retain
product boundaries. Explicit complete-edition DLC installers and required install/
play discs remain components of their owning edition. Unknown or generic bonus
labels do not prove a separate product. Network-test stages are early releases;
free-form test-related commercial names remain conservative.
Unknown qualifiers and free-form demo-disc issue subtitles remain conservative.
A title word such as `Demo Racer` does not classify a release as a demo.
Saved release tags, explicit card labels and scoped provider content types also
constrain existing cards. Unknown product mixtures or missing releases require
review; a retail/demo/beta mixture alone is valid (ADR 0088).

For ordinary provider-based fold, every pair in the proposed group needs:

- A shared eligible direct ID and the same normalized title; or
- agreement from two independent providers through direct IDs or anchored aliases;
  or an anchored alias with an explicit matching release region; or
- complete coverage by the existing independently reviewed PS3 serial family.

One provider's plain alias or broad ID with different titles is discovery only.
Conflicting direct IDs, ambiguous aliases, wrong alias regions and saved
keep-separate evidence remain holds. IGDB records without platform scope and
Hasheous bridge IDs cannot prove identity. A chain A–B–C cannot merge when A–C
has no direct confirmation. An ambiguous component is held as a whole rather
than partitioned by an arbitrary traversal order.

`edition_of`, `expanded_edition_of`, remake/remaster and expansion relationships
are content boundaries. `port_of` and shared edition-family ancestry are only
discovery evidence. `--editions-only` inspects those families; it does not permit
merging different content. Xbox product serials remain discovery hints requiring
interactive review whenever independent regional proof is missing.

## Import and refresh

Fresh DAT grouping partitions title/clone families by content profile. A clone
parent can include its confirmed development releases; different supplements and
commercial editions remain separate.
Unknown qualifiers remain visible instead of being stripped as if they were
regions. No implicit translated-title dictionary or fuzzy match is added.

Refresh retains existing paths using persisted authority identity and complete
release fingerprint signatures. Several incoming regional bundles may reuse one
existing card only when each contains an exact previously saved authority name
and compatible content is confirmed. When every incoming name is already owned
by a destination with a reviewed key, refresh preserves that exact ownership
without reinterpreting its reviewed regional labels. Unknown new releases remain
subject to the conservative content/identity checks. Unconfirmed many-to-one matches still abort the import.
An exact DAT anchor inherits the compatible card's descriptive provider content
type; DAT's lack of that field does not turn an unchanged edition into retail.
All incoming bundles for one destination are combined before rendering it once.
This prevents the later bundle from overwriting releases/fingerprints written
for the earlier one. Existing omitted releases and custom release, authority and
fingerprint fields survive; fresh DAT-owned values retain their normal priority.

## Preservation and limits

Automatic fold, interactive approval and ordinary hash-bound plans enforce the
same product/edition boundary. Explicit `identity_review` (ADR 0089) can resolve
independently proved regional content equivalence and obsolete keys; it requires
complete releases, previous-key acknowledgements, evidence URLs and fresh hashes. Review plans approve membership/identity. An
explicit `development_parent` can retire former release-stage keys only for
reviewed development members; it cannot join another commercial card. Plans fail preflight before mutation; ordinary apply
rechecks the actual staged content and identity before writing.

The existing platform transaction, complete release/fingerprint renderer and
media collision handling remain in use. Identical media coalesce only after byte
verification; distinct same-name media remain under `media/_fold/`. Backups keep
the original platform. See [reviewed plans](reviewed-fold-plans.md).

Metadata does not prove undocumented changes to songs, costumes or other game
content. Missing or contradictory evidence requires review, and unlabelled
source mistakes can still require correction. This policy prevents known classes
of false merges; it is not automatic semantic analysis of ROM payloads.
No live-library migration or automatic split of historical mixed cards is part
of installing the fix. Exact-release `_fold` export provenance remains separate.

Regression tests cover title-independent classification, order-independent DAT
partitions, broad IDs, two-provider aliases, unscoped/bridge evidence, transitive
chains, all approval modes, and the actual import → fold → repeat-import cycle.
That cycle compares decoded complete releases/fingerprints/provider metadata and
SHA256 of media, including differing and identical same-path files.

## Repeat import of reviewed ownership

A DAT family crossing different reviewed keys can be partitioned by exact saved
authority names when every incoming release has exactly one existing owner and
no authority ID points to another card. This reuses prior decisions without
removing keys. Unknown additions, ambiguous names and conflicting IDs keep the
whole unresolved family on review before mutation. Exact known-name refresh can
retain production/part qualifiers within a confirmed card; explicit commercial
edition and product differences remain guards. No stripped-title ownership is
invented. Import/fold/reimport regressions check this behavior.

An exact saved-authority refresh of a nonempty reviewed key also retains explicitly
reviewed regional edition equivalence. This does not admit unknown additions or
conflicting authority IDs.
