# Manage your library in the browser

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

**Library / Operations / Settings** separate browsing, background work and
library defaults. **Operations → Add job → Export** opens destination, ROM and
profile preparation; there is no separate Export navigation tab. Read-only servers
retain **Preview export** in Operations. Check platforms in the sidebar,
then use the **Fetch metadata**, **Merge** or **Statistics** buttons above the
platform list. These icon buttons have hover tooltips and accessible names.
The action strip stays in that column and shows the selection
count. Without checked platforms it uses the currently browsed platform.
Statistics requires one platform. **Edit card** lives on the card. **API** and
**API docs** remain in the header across views and open in a separate tab.
Settings edits library configuration, as described below. The sun/moon icon in the upper-right corner
chooses a neutral dark or light palette, remembered by this browser.

1. Choose **Import your first DAT** in an empty library or the **+** beside
   **Platforms**, or **Operations → Add job → Import DAT**. The same menu also
   offers Fetch metadata and Export when the library has platforms.
2. Upload an ordinary XML DAT (`.dat` or `.xml`) or ZIP. For multiple DATs in a
   ZIP, choose the member. The optional platform name overrides detection.
3. Review the prepared platform, card/release/fingerprint counts and sample
   releases. **Prepare plan** refreshes the preview after changing options.
4. **Import into library** applies the reviewed plan. The completed result offers
   **Fetch metadata** as the next step: it opens source selection with the imported
   platform already checked. Choose sources and review/start the new job there;
   opening the wizard starts no downloads. **Open platform** takes you to the
   saved cards, dismisses the completed import receipt, and refreshes counts.

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

Finished imports, metadata fetches and exports offer **Dismiss result** and a
matching **×** in the upper-right corner of the result card. Successful results
highlight the bottom button in green. Both controls remove only the operation
from the list; saved cards, downloaded media and exported files stay in place.
Failed or incomplete results retain their retry actions. Running tasks have no
close button; **Remove from queue** remains a separate action.

Operations are scoped to this running server, shared by its authorized clients.
There is no account-specific history or restart/reboot recovery. Inactive records
expire after 24 hours or dismissal. Uploads are limited to 256 MiB, XML members
to 512 MiB expanded, and the server retains at most 32 operations/uploads.
Successful imports, dismissed uploads and expired records clean up their inputs.
An interrupted server can leave its `.rommeta/web-import-*` upload directories;
remove those only after ensuring their owning server is stopped. A running import
cannot be cancelled/dismissed in this first workflow.

## Fetch metadata

Check one or several platforms in the sidebar (or **Select all**), then choose
**Fetch metadata** in the same column. The wizard receives that selection
and lets you adjust it before choosing sources, data types and parallel workers.
**Operations → Add job → Fetch metadata** opens the same wizard.

1. Select sources and data types: metadata, artwork and video. Defaults come from
   the library's acquisition settings. Unavailable choices explain missing
   configuration or credentials. Platform support is checked automatically
   against the source's list when its task starts; a missing built-in mapping
   does not disable it. Unresolved identities skip without changing cards;
   explicit names/IDs in Settings → Platform mappings can resolve them.
2. **Review tasks** prepares the selected jobs without contacting providers.
   Review the platform and source/type list, then choose **Start fetching**.
   While another operation is active or queued, this button becomes **Enqueue**;
   the accepted task starts automatically when its turn arrives. The label follows
   Operations updates, including when the previous job finishes.
   Optional parallel workers (1–32) apply within each task; provider limits still
   apply. The default uses library/provider settings. Effective workers appear
   beside each task; metadata databases may require serial processing.
3. **Operations** colors status labels consistently in the operation list and
   task stages: green for complete, blue for active/ready, amber for queued or
   attention needed, red for failure, and gray for waiting/skipped stages.
   Text labels remain visible. It shows stages in a separate list and progress/results for the
   selected task. Downloads show active workers, transferred bytes, discovered
   files and pending/failed counts; totals can grow while scanning. Queued tasks
   start automatically in FIFO order when other web/CLI catalog writers finish.
   Closing the browser preserves the queue and active work; reconnecting never
   launches it twice.
4. Review each source's result and choose **Open platform**. **Prepare another
   pass** creates a new plan when you want to retry incomplete work.

The browser and CLI use the same provider routines, library configuration, rate
limits, cached snapshots and existing-data checks. Metadata runs before artwork
and video. Successful work is retained if another task fails; later independent
sources continue. ScreenScraper quota exhaustion skips its remaining tasks.
Hasheous provides identity links from ROM hashes, rather than descriptions.
Artwork can use existing source links without repeating metadata acquisition.

This flow enriches all selected platforms with ordinary acquisition
semantics. It does not force-overwrite existing results. Another pass skips
completed metadata and downloaded files according to each provider's rules.
Advanced force/refresh/limit options remain available through [CLI fetch](fetch.md).
Source credentials stay on the server and are never included in operation results.

The same operation lifetime and single-writer rules as DAT import apply:
closing the browser is supported; restarting the server does not resume a running
job or restore its receipt. Saved catalog changes remain. Inactive receipts expire
after 24 hours. Waiting queue entries can be removed before execution; running
enrichment cannot be dismissed or cancelled. Catalog data updates do not make
queued acquisition plans stale. Changed source settings require a fresh review;
unavailable source/platform combinations appear as skipped tasks.

## Export on the server machine

With `serve --edit`, **Export** opens a native export setup for ES-DE or
LaunchBox. The CLI remains available through the same exporter.

1. Choose a saved destination or enter a name, frontend and absolute destination
   path. **Browse** lists folders on the machine running serve. For LaunchBox,
   choose its installation containing Data/Platforms; for ES-DE, choose its data
   folder. ES-DE optionally supports a separate media folder.
2. Choose the ROM folder and layout: one platform's ROMs, or a parent containing
   all platform folders. Select the catalog platform for the single-platform case.
   Batch mode uses the CLI folder recognition and reports skipped systems.
3. Edit **Selection profile** directly in this setup: titles, description language
   order and scope, media types, artwork preference, and source priorities per field/media role.
   Start from library settings or **Load YAML** (up to 1 MiB). Advanced profile
   YAML is editable with **Validate & use YAML**. Other profile rules are retained.
   **Artwork** selects original or reconstructed images first, with fallback when
   unavailable; video/manual rules are unchanged. **Loaded profile priorities**
   restores the image ranking from the loaded profile or library settings.
   Choose a **Saved profile**, or enter a **Profile name** and **Save profile** to
   keep it in this library. **Update profile** replaces the selected profile;
   change the name and **Save as new** to keep another variant. Profiles survive
   browser/server restarts in `config/export-profiles.yaml`.
   **Download YAML** exports a portable copy for **Load YAML** or CLI `--profile`.
   These actions include all profile rules without ROM/destination paths and
   neither require a destination nor start an export.
   **Preview in new tab** opens an independent snapshot of this exact profile;
   the original setup keeps its paths, ROM selection and edits. Change settings
   in setup and open another preview to compare. The preview is a catalog projection;
   **Prepare export plan** still checks actual ROM matches and native writes.
4. **Save destination** stores the paths and a reference to the selected saved
   profile in `config/export-destinations.yaml`. Unsaved policy edits are stored
   inline instead. Updating a linked profile affects future exports; already
   prepared plans keep their policy snapshot. Referenced profiles cannot be
   removed until their destinations select another profile or an inline policy.
   Saved entries survive server restarts. CLI uses the same entries through
   `--destination-name` and `--profile-name`; see [saved choices](frontend-export.md#saved-profiles-and-destinations).
   Renaming updates the selected entry; choose New destination for another.
   Removing an entry removes only its settings, never frontend files.
5. **Prepare export plan** starts a background scan. Operations shows its progress,
   matching counts, native file count, game actions, conflicts and skipped systems.
   **Inputs needing attention** lists unresolved, ambiguous and game-only matches
   with filenames, reasons and candidate evidence. Unrecognized translated ROMs
   can show **Suggested by name** cards: full titles and aliases are compared
   after removing filename tags. **Accept match** beside the title confirms a
   suggestion. **Accept all with one candidate** stages every remaining unique
   suggestion across all pages, keeping existing choices and exclusions. Inputs
   with multiple candidates remain for individual review. Both actions select
   game metadata, not an exact release or translation version. **Choose game** searches cards
   in the input's platform as you type; select a release when known, or **Game only** to use
   default metadata. **Use this game** stages the choice. You can change it,
   restore the automatic match, or exclude the input. **Update plan** recalculates
   metadata, artwork and native files from the retained scan without writing or
   re-scanning ROMs. Manual choices are labelled explicitly, not hash-verified.
   Unsupported inputs still require correcting their reader/file problem.
   Choices stay with this operation after browser closure; they do not modify
   cards/fingerprints or carry over into a new export or a server restart.
   Recognized inputs are included automatically; **Download full plan** includes
   all native changes and filenames. Preparing writes no destination files.
6. Close the target frontend and choose **Apply export** after review. The server
   retains and applies this exact native plan, rechecking catalog/configuration,
   selected card documents, ROM stamps and destination files. Stale inputs require
   a new plan. Accepted Apply retries never replay writes.

A single-platform export can run alongside **Fetch metadata** for other platforms
in the same server. This applies to preparing, reviewing and applying a plan.
Aliases and related DAT datasets count as the same platform. Fetch completion on
another platform does not invalidate the export plan. Fetching the same platform
waits for an active export; starting export while that platform is being fetched
reports the conflict. Exporting all platforms, other catalog writers and a second
export still require exclusive access.

Both planning and applying continue after browser closure. Return to Operations
for progress or results; a completed repeat can report zero files to write.
A write failure reports the files completed so far. Prepare another plan against
the current destination to continue; there is no automatic rollback, hidden backup
tree, package staging or ROM copying. Native metadata, playlists and selected
missing media use the same additive rules as [CLI export](frontend-export.md).

Paths always belong to the serve host, including when the browser is elsewhere.
In a container, mount ROM folders read-only and destinations writable as needed,
and ensure the service user has access. Browser paths do not grant access to a
different computer's disks. `--edit` permits native writes within that process's
filesystem permissions; existing server access protection applies to these routes.
Browser-managed remote-catalog export and portable packages are outside this
workflow; existing CLI `--api`, `--out` and `--package` remain available.

Operation receipts and in-memory plans have the same server-session lifetime as
imports: browser reconnection is supported, server restart recovery/cancellation
is not. Inactive receipts expire after 24 hours. Saved destinations are durable;
active progress is not stored alongside frontend output.

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

In **Library**, choose **Edit card**. The fields become editable in place.
The editor initially selects a saved source contributing to the displayed card,
or the last chosen manual variant. Opening it never copies resolved values into
overrides.

**Source tabs** identify the destination: Card fields, a provider, or your named
variant. **Entry** appears only when that source has several records or scopes.
A short caption identifies what you are editing. Existing description languages
have text areas and a **× Remove translation** control; **Add a translation** adds
another. Removal changes only the selected record and takes effect on **Done**;
**Discard** cancels it. Removing the last language removes its descriptions field.
**Other fields** keeps IDs,
service details and structured values out of the common form until needed.
You can add or remove source fields without replacing the whole record.

Switching records retains their drafts. **Done** validates and saves all edited
records together, then closes the editor. **Discard** cancels all card edits and
closes it. Invalid or stale saves keep the editor and drafts open. Navigating
away with unsaved changes asks before discarding them. Drafts are not saved
across a page/server restart; this differs from server-owned import operations.

### Named manual variants

Choose **+ My variant** beside the source tabs, enter your own name,
then contribute the fields you want to curate. You can create several variants and reuse each across cards.
Creating a variant saves its name immediately; **Done** saves its card
fields. A variant stores only contributed values, never an implicit copy of the
resolved card. **Rename variant** changes its label while retaining its stable ID.

A variant can apply to the whole game or an existing release. Choose its **new
release record** entry to add a release-specific refinement. Specific values win
over that variant's whole-game values; other selected sources still supply missing
fields. Explicitly scoped manual descriptions never borrow from another release,
even when provider description scope is Whole game. Use per-language descriptions
for translations; shared description text applies to all languages.

Enable and order variants in **Export → Selection profile → Source priorities**, or save
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

Open **Export → Edit browser profile** to preview the saved projection. The
compact preview bar groups frontend, language and media choices; **Advanced**
contains source priorities. **Use this profile →** returns to export setup with
those choices. The card editor has no separate export-preview action.
Changing a source title never
renames release keys, ROMs, fingerprints, launch paths or native game/version IDs.

### Edit Raw YAML

In edit mode, **Raw** exposes the complete `game.yaml` as an editable text area.
Form changes are included when switching to Raw; returning to Overview validates
the YAML and restores source forms from that draft. **Validate YAML** changes no
catalog data. **Done** always validates again before writing.

Validation checks one mapping document, duplicate keys, known field types,
default/source release references, derivative relationships and existing
fingerprint references. Errors include the YAML line or affected field. YAML
aliases/custom tags are rejected; use explicit values. Unknown ordinary fields
and comments are retained, and a Raw-only save preserves the submitted text.
A renamed/removed release must not leave a registered fingerprint dangling.
A newly added `manual.*` source needs an existing named variant in the registry.
Invalid or stale input stays in the editor for correction; it is never written.

**Merge** and **Statistics** are in the platform action strip.
**Select for merge** adds a card to a selection. On another card,
**Compare for merge (N)** includes the open card automatically and opens their
side-by-side merge review. The count includes that card, without duplicates.
This compares game cards from the same platform, not ROM bytes or export output;
opening it does not merge anything.

### Edit releases and fingerprints

Enter **Edit card**, then open **Releases & fingerprints**. The source chooser is
replaced by release/file forms. You can also enter editing while already on this tab.

- **Add release** creates a draft definition. Edit its name, title, regions,
  languages and date; choose the card's default release separately.
- Renaming updates this card's default, source scopes, variant bases and fingerprint
  references. Original DAT/provider provenance and media paths remain unchanged.
- Inside each release, **Add file** records a filename, size in bytes, its release or an existing
  variant, and one or more CRC32/MD5/SHA1/SHA256 hashes. Expand a filename to edit
  its fingerprint; new files inherit the containing release. Existing fingerprints can
  be edited or removed. This registers an identity; it does not hash or alter ROMs.
- **Remove** marks a record for deletion; **Undo removal** restores it.
  For a referenced release, choose a surviving release in **Reassign its references
  to**, or remove/reassign the dependent records yourself. Source evidence is
  retained when its scope moves; release fields are not copied into the replacement.
- **Validate changes** checks the complete draft without writing. **Done**
  validates again, saves all card/source/release/fingerprint changes and closes
  editing. **Discard** cancels the complete draft and closes editing.
  File search and paging retain edits to other records.

Validation rejects duplicate release names, broken source/default/variant/file
references, invalid dates, fractional/negative sizes and malformed hashes.
Changed/new identities require a filename, size and at least one hash; explicitly
undumped records may omit hashes. Existing legacy records remain unchanged unless
edited. Unknown fields, source provenance and YAML comments are retained.
Forms do not create derived variants; use the existing variant workflow or Raw
for those definitions.

Both document revisions are checked, including a missing fingerprint file.
Packed catalogs replace their shared metadata ZIP atomically. Loose catalogs stage
both files before replacement and restore the originals after ordinary write errors;
they do not provide a two-file transaction across a process crash or power loss.
No persistent backup or snapshot is created. Save refreshes recognition indexes,
browser summaries and the visible fingerprint list.

## Let fold prepare the work

Select one or several platforms and choose **Merge** to review duplicates.
The server runs the existing fold rules and proposes groups, primary cards and
merged results automatically. Multiple platforms share one review and final Apply;
each group shows its platform and never merges cards across platforms.
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
cards; excluded cards stay unchanged.
**Agree & next** validates and queues the group, then opens the next unresolved
comparison. **Keep separate** queues evidence rejection; **Skip for now** leaves
the group unresolved. None of these actions writes the catalog.
The fixed upper bar shows progress and how many groups still need review;
ready groups already have a prepared decision. Skipped groups are counted separately.
Previewing a result does not advance review progress until you choose a decision.
Keep separate, Skip and Agree stay together in a fixed action bar while cards scroll.
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

For a group the automatic plan does not propose, use **Select for merge** on one
or more cards, open the last card, then **Compare for merge** (2–12 cards total).
The current card is included automatically. Manual groups need an explicit same-game
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
input files across all selected platforms before any write, applies each platform
using its existing transaction, then builds a fresh
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

Ordinary selected groups use one staging/swap per platform. MAME uses its
binding-aware transaction. This is not an all-platform atomic transaction: if a
later platform or MAME transaction fails, earlier completed changes remain saved.
Backups are optional and off by default. Check **Keep a backup of the affected
platforms** in the final review to retain originals for that apply. Manage retained
copies in **Settings → Backups**; deleting a copy requires confirmation.
See [backup management](catalog-backups.md) for CLI use and limits.
Errors report any retained recovery files; build a fresh plan for remaining cases. An apply attempt
consumes its plan even on failure. The browser requests `continue: true` after a
successful apply; the response contains `next_plan` with a new ID, or a separate
`continuation_error` alongside the successful result. Temporary originals are removed after successful application unless
`keep_backup: true` was requested. No filesystem or ZFS snapshot is created.

Single-card saves use atomic replacement and reject stale revisions. The recognition
index and browser summaries are refreshed after writes; an index refresh problem is
reported separately from a successful save. Acquisition and native export use the separate operations described above;
card/fold edits preserve ROM files.

## HTTP entry points

`GET /api/catalog` advertises editing capability. The editor uses
`GET|POST /api/catalog/edit`, `POST /api/catalog/edit-preview`,
`GET|POST /api/catalog/curation` (named-source registry), and
`POST /api/catalog/fold-plan`, `fold-preview`, `fold-apply`, `fold-discard`.
See the running server's `/docs` and `/openapi.json` for request fields.
Write actions require JSON, explicit editable mode and same-origin browser requests.
The existing `/v1` consumer/export contracts remain read-only.
