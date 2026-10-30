# LaunchBox export

Generate one native LaunchBox card per matched catalog game. Regional releases,
revisions, prepared translations and hacks become **Additional Versions** under
that card, including its primary launch. Grouping uses catalog game identity,
never similar display titles. Unknown/ambiguous inputs remain independent cards.

## Export and enrich the installation

Run the client on the Windows machine that will launch the games, using paths
available there. ROMs and catalogs are never copied or changed.

```powershell
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --profile "D:\Profiles\launchbox.yaml" --destination "D:\LaunchBox"
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --profile "D:\Profiles\launchbox.yaml" --destination "D:\LaunchBox" --apply
```

Copy [the example profile](../examples/export-launchbox.yaml) to set preferences.
Omit `--profile` for default metadata/media policy and an original-first default.
Ties use the complete version label and original path, so selection is repeatable.
The optional `launchbox` section ranks versions in this order:

```yaml
launchbox:
  kinds: [original, translation, hack]
  regions: [USA, Europe, Japan]
  languages: [ru, en, ja]
```

Put `translation` first to prefer translations. Unlisted values follow listed
ones. These lists rank available inputs; they do not acquire, filter or replace
ROMs. Other prepared variant kinds remain selectable. Variant names retain kind,
language and available group/version credits. Equal labels gain the source path.

The card keeps its catalog title; fields and native artwork come from the primary
input. Its ApplicationPath points to the same launch as the primary version.
Every version stores its own path, region, label, emulator setting and stable ID.
Current native records use `AdditionalApplication` with `Section=Version`.

The command uses the [shared ES-DE/LaunchBox interface](frontend-export.md).
Preview shows recognition and native changes without creating temporary output,
copying media or downloading assets. Close LaunchBox/Big Box and add `--apply` to
add cards and enrich existing ones with backups. Nonempty native metadata/main
launches remain even when the profile ranks another primary. Use `--report` for
an optional new JSON destination plan. An unchanged apply writes nothing.

For portable output, replace `--destination` with `--out <new-directory>`.
That optional package contains `preview.html`, `report.txt` and `manifest.json`
with grouping, input, primary, omission and provenance details. Manifests/reports
include private paths. Input totals remain ROM counts rather than card counts.

## Network and batch export

Use `--api "http://catalog-host:8787"` instead of `--data-root`. Recognition and
asset validation remain shared. The client removes only the `launchbox` rendering
section before sending profile bytes, allowing existing version-1 catalog APIs;
it restores that policy locally before rendering. New API responses include the
catalog title; older responses fall back to the primary regional display title.

```powershell
rom-meta export --target launchbox --api "http://catalog-host:8787" --platforms nes,snes,psx --rom-root "D:\Games" --profile "D:\Profiles\launchbox.yaml" --destination "D:\LaunchBox" --apply
```

`--platforms` and `--all` use the shared system-directory IDs. `--list-systems`
lists readers. Profiles keep their shared `target: es-de` header; the command's
`--target launchbox` selects the native layout. ES-DE retains its existing layout.

## Cartridges and multiple discs

Cartridge readers accept supported raw files and single-payload ZIPs. Disc readers
retain the [shared input limits](export-scenarios.md#es-de-input-coverage).

An existing directory-style M3U is one playable version:

```text
Game (USA).m3u/
  Game (USA).m3u
  Game (USA) (Disc 1).chd
  Game (USA) (Disc 2).chd
```

All members must bind to the same game/compatible prepared edition. The actual
inner M3U is launched and its original order remains. Several regional or modified
M3Us can be versions under one game card, each with its own disc set.

For loose official CHD/ISO files, export can create a package M3U when exact
catalog release names differ only by a single Disc/Disk token, all other
qualifiers/region/language/revision agree, and selected disc numbers are exactly
1..N (2..32). Duplicates, gaps, mixed editions and conflicting declared totals do
not assemble. Generated M3Us use disc-number order and absolute original paths.
This checks observed sequence completeness; it does not prove that the catalog
or original release contains no further discs. Review warnings and avoid limiting
a disc-set selection. Prepared modified sets need a supplied M3U.

Unassembled numbered discs remain separate selectable versions with explicit
warnings. Ordinary flat M3U, CUE/BIN, GDI and PBP remain unsupported launch inputs.

[LaunchBox's guide](https://feedback.launchbox-app.com/en/help/articles/8972270-multi-disc-games-and-m3u-playlists)
explains that automatic M3U generation gathers numbered attached versions. With
multiple regions/revisions on one card that can mix editions. This exporter
therefore leaves the global Disc field unset and hands a version-specific M3U to
the emulator. Do not enable global disc inference for manually added numbered
versions without checking the resulting set. Selecting Disc 2 is a fresh launch,
not a command to swap discs in a running emulator.

Disc changing, core/format support and save compatibility need actual emulator
checks. Keep an established M3U launch path when migrating saves. CHD header or
name evidence does not become verified Redump disc evidence.

## Optional package layout

| Package path | Purpose |
| --- | --- |
| `Data/Platforms/<platform>.xml` | Game cards and all native versions |
| `Images/<platform>/<image type>/<card UUID>-01.<ext>` | Primary-version native artwork |
| `Videos/<platform>/<card UUID>-01.<ext>` | Primary video, linked by VideoPath |
| `Manuals/<platform>/<card UUID>-01.pdf` | Primary manual, linked by ManualPath |
| `Playlists/<platform>/<version UUID>.m3u` | Generated edition-specific disc launch |
| `VersionMedia/<platform>/<input UUID>/<role>.<ext>` | Alternate selected media for review |
| Reports, preview and manifest | Grouping, inputs, evidence and installation notes |

Image roles include Box - Front/Back/3D, Clear Logo, Screenshot - Gameplay/Game
Title and Fanart - Background. Formats are copied unchanged; frontend support
needs separate verification. Alternate media stays outside the automatic image
scan to avoid mixing regional/modified artwork on the main card.

## Merge into the installed library

Direct export with `--destination` already uses the installed-library adapter.
When starting from a separately prepared package, use the same export command:

```powershell
rom-meta export --target launchbox --package "D:\Exports\library-check" --destination "D:\LaunchBox"
rom-meta export --target launchbox --package "D:\Exports\library-check" --destination "D:\LaunchBox" --apply
```

The shared destination plan lists each card as `add`, `enrich`, `unchanged` or `conflict`, with field,
version, media and changed-file counts. Optional `--report "D:\Exports\merge.json"`
writes that plan to a new JSON file, including private paths. Close LaunchBox and
Big Box before applying; on Windows the command checks their processes.

Matching uses native card IDs, previously recorded bindings, main/version launch
paths, or identical ordered absolute disc paths in available M3Us. Windows path
case and slash differences are normalized. Titles are never matching evidence.
An adopted card retains its existing native ID, including playlist references,
favorites and history. A small `Data/RomMeta.launchbox.json` map remembers that
binding when later exports contain moved ROM paths. A missing previously bound
card is a conflict rather than an instruction to recreate it.

The destination adapter fills empty title, description, release date, developer, publisher,
genre and player-count fields. It adds missing playable versions and fills their
empty metadata. Existing nonempty values, main launch path, emulator settings,
command lines, manual/version choices, custom fields and unknown XML survive.
Default year-0001 dates and zero player counts count as empty. Changing export
primary preferences does not replace an installed card's working primary launch.
New versions inherit an assigned main-card emulator; new cards remain unassigned.
Review emulator/core/BIOS settings before playing.

Missing primary artwork is installed under the retained card ID. Existing native
GUID/title/database-ID artwork in the same category is kept, including region
subfolders. Existing video/manual links and files are preserved. The adapter never
replaces media with different bytes. Custom native image/video/manual folder
overrides are rejected rather than guessed. Alternate `VersionMedia` sidecars
stay in the export package for review, outside native media discovery.

Generated M3Us are copied to the installation's `Playlists` tree only when a new
launch needs them. An equivalent existing main M3U remains the new version's
launch path too, preserving its established save identity. Source ROMs, discs,
playlists and catalog data are never rewritten or copied. Disc/core/save behavior
still needs emulator verification.

If paths or IDs point to multiple existing cards, multiple catalog games point to
one native card, or version IDs/paths conflict, apply refuses the entire merge.
Existing separate cards are not automatically deleted or folded: their history,
playlist memberships and curation need a deliberate native consolidation first.
See LaunchBox's [bulk-editing and combining guidance](https://launchbox.featurebase.app/help/articles/8922784-bulk-editing-and-auditing-a-large-launchbox-library).
The first ungrouped exporter used per-launch card IDs; exact launch/M3U evidence
allows adoption when it identifies one existing card unambiguously.

Before writing, the command rechecks the package, affected native files, selected
assets and compared M3Us. It backs up every replaced file under
`Backups/RomMeta/<run>/` and records changed/new paths in `changes.json`. Assets
are staged before publishing platform XML. This is not a multi-file transaction:
an I/O failure can require reviewed manual recovery using that backup. No automatic
restoration overwrites later edits. An unchanged repeat writes nothing and creates
no backup. The first merge can write only the identity map even when all native
records are already complete. Do not manually replace an existing platform XML with the package XML.

Use the Platform sidebar if category grouping hides a newly added platform.
After package installation the package can be archived; native launch/media paths
do not depend on it. Keep source ROM/network mappings available.

## ScummVM

[ScummVM cards/media](scummvm.md) remain metadata preparation. Launch export needs
real folders, detection and reviewed bindings. The retired built-in integration
is not used. See the
[official guide](https://feedback.launchbox-app.com/en/help/articles/7897312-importing-and-managing-scummvm-games).
