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

## Correct a card

In **Library**, open a card and choose **Edit card**. Edit its title, aliases,
alternate names, descriptions (including language-specific text), release date,
developer, publisher, genres or players. **Preview changes** shows the resulting
card; **Save card** writes that preview. Further edits require a new preview.

Saved fields take priority over provider values. **Use sources** removes a local
override. Empty text returns to source selection; it is not a forced-blank rule.
The title remains required. The editor retains unknown YAML, original provider
observations, release identities, variants, fingerprints and media. Source refresh
does not replace these local game-level corrections. Advanced structural fields
remain inspectable in the original catalog documents.

## Let fold prepare the work

Select a platform and choose **Review duplicates**. The server runs the existing
fold rules and proposes groups, primary cards and merged results automatically.
The overview has three columns with counts and local cover previews:

- **Ready to merge**: identity, content, files and saved fields pass validation.
  These groups start selected. Inspect any sample, then apply them together without
  opening every comparison. Recognized revision/pressing labels such as RE and Alt
  do not by themselves require owner confirmation; provider identity still must agree.
- **Your review**: confirm which cards belong together despite identity,
  edition or provider-ID doubts, or choose between differing saved field values.
- **Conflicts**: unreadable files, conflicting release payloads, colliding
  non-media files or incompatible unknown fields prevent a lossless merge.
  Choose a different subset or resolve the reported data/access problem.
  Identity confirmation cannot discard conflicting data.

Each group shows up to four member covers and names; **Compare** opens all
candidates. Missing or inaccessible artwork has a placeholder and does not change
merge readiness. **Select all** and **Apply ready groups** remain above the ready
column while its list scrolls. On narrow screens, scroll sideways between columns.
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
**Confirm & merge selected** confirms and saves in one action when data checks
pass. A changed selection needs fresh confirmation. Confirmation is the owner's
catalog decision, not new provider evidence. Real data conflicts keep apply
unavailable until resolved. Editable saved-field differences can be resolved
through the result editor before previewing again.

Candidate cards keep a readable width in a horizontal scroll area; the result
stays beside them on wider screens and below on narrow screens. Focus the card
area to scroll with the keyboard, or use its scrollbars/touch gestures.
**Compare** shows candidate cards, highlighted differences, cover art, releases,
source observations, media and registered fingerprints. Accept the suggested
primary, choose another eligible member, or correct the result. Previewed decisions
stay in the plan when returning to its list. **Apply this merge** handles one group;
**Apply ready groups** handles the selected ready groups. Platform counts and the
current list refresh after completed changes, including partial completion.
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
consumes its plan even on failure. Recovery folders use the existing fold behavior;
no filesystem or ZFS snapshot is requested.

Single-card saves use atomic replacement and reject stale revisions. The recognition
index and browser summaries are refreshed after writes; an index refresh problem is
reported separately from a successful save. The editor does not download media,
edit ROM files, import profiles or apply frontend exports.

## HTTP entry points

`GET /api/catalog` advertises editing capability. The editor uses
`GET|POST /api/catalog/edit`, `POST /api/catalog/edit-preview`, and
`POST /api/catalog/fold-plan`, `fold-preview`, `fold-apply`, `fold-discard`.
See the running server's `/docs` and `/openapi.json` for request fields.
Write actions require JSON, explicit editable mode and same-origin browser requests.
The existing `/v1` consumer/export contracts remain read-only.
