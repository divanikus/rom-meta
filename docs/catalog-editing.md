# Edit cards and review folds in the browser

Start a complete catalog server with explicit write access:

```shell
rom-meta serve --data-root /catalog --edit
```

Without `--edit`, the browser remains read-only. Containers need a writable
catalog mount and filesystem permissions for the service user. On Linux, packed
writes retain the original ZIP owner/group: run the editor as the catalog owner
and resolve mixed metadata ownership explicitly before enabling writes. Fixed ROM-selection
servers do not support editing. Existing optional API authentication also protects
editor actions; enabling editing does not introduce accounts or change the token.

## Import into a new library

Initialize the library once, then start the same CLI server:

```shell
rom-meta init "<catalog>"
rom-meta serve --data-root "<catalog>" --edit
```

**Library / Operations / Export / Settings** separate browsing, background work,
export-profile preparation and library defaults. Platform review/statistics live
beside the selected platform; the edit pencil lives on the card. **API** and
**API docs** remain in the header across views and open in a separate tab.
Settings edits library configuration, as described below.

1. Choose **Import your first DAT** in an empty library or the **+** beside
   **Platforms**. Operations offers a new import action when viewing an existing
   operation; the upload screen itself has just its upload/prepare action.
2. Upload an ordinary XML DAT (`.dat` or `.xml`) or ZIP. For multiple DATs in a
   ZIP, choose the member. The optional platform name overrides detection.
3. Review the prepared platform, card/release/fingerprint counts and sample
   releases. **Prepare plan** refreshes the preview after changing options.
4. **Import into library** applies the reviewed plan. **Open platform** takes you
   to the saved cards; the platform count refreshes automatically.

No-Intro, Redump and compatible Logiqx XML use the ordinary CLI importer, including
library region/alias policy and preservation of existing metadata. No ROMs are
uploaded or copied. ClrMamePro text needs conversion to XML; classified MAME
listxml remains a CLI workflow. The CLI remains fully usable without a server.

After the upload completes, planning and import belong to the server. Closing the
browser does not cancel them. Reopen **Operations** to recover the same current
progress or result; reconnecting never resubmits an import. Keep the page open
during the file upload itself. Accepted Apply retries are idempotent. Only one
catalog writer runs at a time, including CLI edits; changed catalog/configuration
requires a fresh plan.

Operations are scoped to this running server, shared by its authorized clients.
There is no account-specific history or restart/reboot recovery. Inactive records
expire after 24 hours or dismissal. Uploads are limited to 256 MiB, XML members
to 512 MiB expanded, and the server retains at most 32 operations/uploads.
Successful imports, dismissed uploads and expired records clean up their inputs.
An interrupted server can leave its `.rommeta/web-import-*` upload directories;
remove those only after ensuring their owning server is stopped. A running import
cannot be cancelled/dismissed in this first workflow.

## Edit library settings

**Settings** saves the actual library YAML used by the CLI:

| Section | Scope |
| --- | --- |
| Source priorities | Metadata, artwork and video order; enables/disables automatic source selection. Export profiles can override these defaults. |
| Enrichment | Source order for automatic metadata/artwork/video acquisition and parallel-job count. Saving does not start a fetch. |
| Source connections | Configured provider addresses, snapshot paths/URLs, request limits, OAuth endpoints and archive tools where applicable. Credentials show availability only. |
| Folding & regions | Preferred release regions, regional aliases/groups and per-platform overrides. Applies to newly prepared DAT/fold plans. |
| Platform names & mappings | Local aliases and provider names/IDs in `config/platforms.yaml`; extends the built-in registry. Legacy enrichment aliases have their own section. |
| Media policies | Source policy/attribution records in `source-policies.yaml`. |

Common order/number/text settings have controls. Region groups, platform mappings
and media-policy documents have YAML editors with validation. **Save changes**
writes the selected section; drafts survive navigation. **Discard changes**
restores the loaded values; **Reload saved** rereads a section after an external
edit. Stale revisions and active catalog writers block saving without discarding
the draft. Unknown configuration outside submitted fields and credential blocks
are preserved, including existing file modes.

Credential values remain in `config/secrets.yaml` or referenced environment
variables; the browser neither returns nor edits them. Use
[source setup](source-setup.md) to change credentials. Service bind address and
access protection remain serve startup options; export-profile preferences remain
in Export. Legacy descriptive `rules` and identity/fingerprint/grouping priority
entries are retained; this settings editor does not imply runtime behavior for
fields the current consumers do not use.

## Correct a card

In **Library**, choose **Edit card** (or a pencil beside a value). The card stays
in place and all fields of the selected record become inputs together.
**Source tabs** identify the destination: Card fields, a provider, or your named
variant. After choosing a source, **Record** appears only when it has several
records or scopes; unrelated sources are not mixed into that list. Common metadata has ordinary controls; **Other source
fields** exposes saved IDs and additional values, with JSON inputs for structured
objects. You can add or remove source fields without replacing the whole record.

Switching records retains their drafts. **Save changes** commits all edited
records of this card in one atomic write; **Discard** discards them.
**Finish editing** returns to the card. Leaving with unsaved changes asks before
discarding them. Invalid or stale saves retain the drafts. Drafts are not saved
across a page/server restart; this differs from server-owned import operations.

### Named manual variants

Choose **+ Named variant**, enter your own name, then contribute the fields you
want to curate. You can create several variants and reuse each across cards.
Creating a variant saves its name immediately; **Save changes** saves its card
fields. A variant stores only contributed values, never an implicit copy of the
resolved card. **Rename variant** changes its label while retaining its stable ID.

A variant can apply to the whole game or an existing release. Choose its **new
release record** entry to add a release-specific refinement. Specific values win
over that variant's whole-game values; other selected sources still supply missing
fields. Explicitly scoped manual descriptions never borrow from another release,
even when provider description scope is Whole game. Use per-language descriptions
for translations; shared description text applies to all languages.

Enable and order variants in **Export → Advanced → Metadata sources**, or save
library defaults in **Settings → Source priorities**. New variants start disabled.
Saved export profiles reference stable IDs, so renaming keeps their selection
valid. Title, developer, publisher, date, genres, players and descriptions use the same
resolver in the browser, CLI, ES-DE and LaunchBox.

Names are stored in `config/curation.yaml`; values are ordinary
`game.yaml` source records:

```yaml
# config/curation.yaml
schema_version: 1
sources:
  manual.0123456789abcdef01234567:
    name: Short descriptions
```

```yaml
# Inside a card's game.yaml
sources:
  manual.0123456789abcdef01234567:
    matches:
      - descriptions:
          en: A short description for this collection.
      - release: Example (Japan)
        publisher: Regional publisher
```

Use that same ID in `config/sources.yaml` under `priority.metadata`, or in an
export profile's description/field `sources` lists. The browser's **Save profile**
writes the selected IDs and order. No new CLI-only configuration mechanism is
required. Provider refresh and ordinary fold preserve these source records.

### Provider records and existing library fields

Selecting a provider record edits its actual saved fields, including IDs.
The selector distinguishes match records and nested release observations.
**Source details** edits the surrounding provider stanza (for example status),
not its matches list. Refreshing that provider may replace its block; use a named
variant when your version should survive provider refresh.

Existing explicit library/release fields retain their previous precedence.
The **Card fields** tab edits these overrides. Clear the relevant library value
to resume source selection. The library title identifies the card and is the
exception: it does not override a title selected from sources.
There is no automatic migration of earlier corrections.

**Library title**, aliases and alternate names describe the catalog card.
In **Regional title** mode, provider/manual titles participate in metadata source
priorities, with the edition-local title as fallback. CLI profiles can override
`selection.fields.title.sources` independently. Missing titles fall through to
the next enabled source. Full release and filename modes remain explicit naming
choices. Prepared derivative titles retain their own identity; ordinary version
disambiguation still adds region/revision/language where needed. LaunchBox uses
the selected primary version's source title for its parent game.

**Preview export** shows the saved projection. Changing a source title never
renames release keys, ROMs, fingerprints, launch paths or native game/version IDs.

### Edit Raw YAML

In edit mode, **Raw** exposes the complete `game.yaml` as an editable text area.
Form changes are included when switching to Raw; returning to Overview validates
the YAML and restores source forms from that draft. **Validate YAML** changes no
catalog data. **Save changes** always validates again before one atomic write.

Validation checks one mapping document, duplicate keys, known field types,
default/source release references, derivative relationships and existing
fingerprint references. Errors include the YAML line or affected field. YAML
aliases/custom tags are rejected; use explicit values. Unknown ordinary fields
and comments are retained, and a Raw-only save preserves the submitted text.
A renamed/removed release must not leave a registered fingerprint dangling.
A newly added `manual.*` source needs an existing named variant in the registry.
Invalid or stale input stays in the editor for correction; it is never written.

Review duplicates and Statistics are in **Platform actions** in the Platforms
pane. **Compare (N)** appears beside card comparison controls after adding cards,
and becomes available with at least two selected cards.

## Let fold prepare the work

Select a platform and choose **Review duplicates**. The server runs the existing
fold rules and proposes groups, primary cards and merged results automatically.
The overview has three columns with counts and local cover previews:

- **Ready to merge**: identity, content, files and saved fields pass validation.
  These groups start selected for the final batch. Inspect any sample without
  opening every comparison. Recognized revision/pressing labels such as RE and Alt
  do not by themselves require owner confirmation; provider identity still must agree.
- **Your review**: confirm which cards belong together despite identity,
  edition or provider-ID doubts, or choose between differing saved field values.
- **Conflicts**: unreadable files, conflicting release payloads, colliding
  non-media files or incompatible unknown fields prevent a lossless merge.
  Choose a different subset or resolve the reported data/access problem.
  Identity confirmation cannot discard conflicting data.

Each group shows one cover from its suggested primary, a title and the card count.
In **Your review** and **Conflicts**, click anywhere on the card or focus it and
press Enter/Space to open Compare. Ready cards keep separate Include and Compare
controls so selecting a batch does not navigate away. All member covers and names
are shown inside Compare. Missing or inaccessible artwork has a placeholder and
does not change merge readiness. **Select all** controls the ready batch; **Review decisions** opens its final summary. On narrow screens, scroll sideways between columns.
A confirmed review appears in the ready column when you return to the overview.

These states describe rules and current evidence, not a confidence percentage.
Matching a title alone or missing provider metadata does not establish identity.
An unreadable candidate is reported as blocked while the remaining ordinary groups
are planned. Incomplete catalog scans and MAME binding preparation still fail
explicitly. A plan with an incomplete file inspection cannot be reused after an
access repair; build a new one. Applying ordinary groups still uses a whole-platform
transaction, so all files required for that copy must be readable.

**Compare** opens all candidates. Use **Include in merge** to select at least two
cards; excluded cards stay unchanged. For identity doubts, **Confirm selection**
records your same-game decision and prepares the group for batch apply.
**Agree & next** validates and queues the group, then opens the next unresolved
comparison. **Keep separate** queues evidence rejection; **Skip for now** leaves
the group unresolved. None of these actions writes the catalog.
A changed selection needs fresh confirmation. Confirmation is the owner's
catalog decision, not new provider evidence. Real data conflicts keep apply
unavailable until resolved. Editable saved-field differences can be resolved
through the result editor before previewing again.

Candidate cards keep a readable width in a horizontal scroll area; the result
stays beside them on wider screens and below on narrow screens. Focus the card
area to scroll with the keyboard, or use its scrollbars/touch gestures.
**Compare** shows candidate cards, highlighted differences, cover art, releases,
source observations, media and registered fingerprints. Accept the suggested
primary, choose another eligible member, or correct the result. Previewed decisions
stay in the plan when returning to its list. **Review decisions** lists the exact
merge and separation decisions, plus the unresolved/skipped count.
**Apply N decisions** saves the selected batch in one operation.
Platform counts and the current list refresh after completed changes, including partial completion.
**Keep separate** records
rejection of the displayed fold evidence, not a permanent ban on every future
identity signal.

For a pair the automatic plan does not propose, use **Add to comparison** on 2–12
cards from one platform, then **Compare**. Manual groups need an explicit same-game
confirmation. The recorded decision contains exact members, document hashes,
previous content keys and release names under `fold.owner_reviews`; all original
provider observations remain. No external evidence link is required. This explicit
owner choice can cross automatic identity/content boundaries, while automatic
fold and strict CLI reviewed plans retain their existing rules.

MAME uses its accepted snapshot and hardware/set bindings. Its primary and result
follow that adapter; edit individual cards before rebuilding to change saved fields.
Exact selected MAME groups can apply while unrelated conflicting groups remain held.
Manual provider-based regrouping of MAME cards is not offered.

Review navigation reuses the current prepared plan and prefetches the next
comparison. There is no platform transaction or full replan between decisions.
The current comparison remains visible during a short validation read; no full
loading overlay replaces it. Queued choices are held for this page session,
and closing the workspace asks before discarding them.

Only the final apply writes catalog data. It rechecks the chosen revisions and
input files, applies the batch using the existing transaction, then builds a fresh
remaining plan. The completion screen appears only when no groups remain.
Unresolved/skipped groups remain available afterward. If saving succeeds but
preparing the remaining plan fails, **Continue review** reloads remaining work
without replaying a completed write.

## Plan lifetime and writes

Planning and previewing do not change catalog cards. Plans belong to the current
server session, expire after 30 minutes, and are discarded on closing the workspace;
at most four can be retained. Restarting the service requires a fresh plan. No
background job or permanent plan registry is introduced.

Apply rechecks the metadata hashes, file inventory stamps and preview revision.
Changed cards require a new plan. File stamps detect ordinary filesystem changes;
same-size/same-time edits by external programs are outside that check. Updated CLI
catalog writers and the editor share `.rommeta/catalog-write.lock`; older tools and
manual YAML edits do not participate. A leftover lock is never stolen: remove it
only after verifying that its writer has stopped.

Ordinary selected groups use one existing platform staging/swap. MAME uses its
binding-aware transaction. Recovery paths are shown after apply. If an ordinary
Arcade batch completes and a subsequent MAME batch fails, completed work and recovery
locations are reported; build a fresh plan for remaining cases. An apply attempt
consumes its plan even on failure. The browser requests `continue: true` after a
successful apply; the response contains `next_plan` with a new ID, or a separate
`continuation_error` alongside the successful result. Recovery folders use the existing fold behavior;
no filesystem or ZFS snapshot is requested.

Single-card saves use atomic replacement and reject stale revisions. The recognition
index and browser summaries are refreshed after writes; an index refresh problem is
reported separately from a successful save. The editor does not download media,
edit ROM files, import profiles or apply frontend exports.

## HTTP entry points

`GET /api/catalog` advertises editing capability. The editor uses
`GET|POST /api/catalog/edit`, `POST /api/catalog/edit-preview`,
`GET|POST /api/catalog/curation` (named-source registry), and
`POST /api/catalog/fold-plan`, `fold-preview`, `fold-apply`, `fold-discard`.
See the running server's `/docs` and `/openapi.json` for request fields.
Write actions require JSON, explicit editable mode and same-origin browser requests.
The existing `/v1` consumer/export contracts remain read-only.
