# LaunchBox export

Generate one native LaunchBox card per matched catalog game. Regional releases,
revisions, prepared translations and hacks become **Additional Versions** under
that card, including its primary launch. Grouping uses catalog game identity,
never similar display titles. Unknown/ambiguous inputs remain independent cards.

## Preview and create

Run the client on the Windows machine that will launch the games, using paths
available there. ROMs and catalogs are never copied or changed.

```powershell
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --profile "D:\Profiles\launchbox.yaml" --out "D:\Exports\n64-check"
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --profile "D:\Profiles\launchbox.yaml" --out "D:\Exports\n64-check" --apply
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

Preview creates no package. Apply creates a new directory and refuses existing
output. Inspect `preview.html`, `report.txt` and `manifest.json`: they show groups,
all versions, primary decisions, omissions and media provenance. Manifests include
private absolute paths. Input totals remain ROM counts rather than card counts.

## Network and batch export

Use `--api "http://catalog-host:8787"` instead of `--data-root`. Recognition and
asset validation remain shared. The client removes only the `launchbox` rendering
section before sending profile bytes, allowing existing version-1 catalog APIs;
it restores that policy locally before rendering. New API responses include the
catalog title; older responses fall back to the primary regional display title.

```powershell
rom-meta export --target launchbox --api "http://catalog-host:8787" --platforms nes,snes,psx --rom-root "D:\Games" --profile "D:\Profiles\launchbox.yaml" --out "D:\Exports\library-check" --apply
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

## Package and installation

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

Close LaunchBox/Big Box and back up Data. For new platforms copy platform XML and
the Images, Videos, Manuals and Playlists trees into matching installation folders.
Use the Platform sidebar if category grouping hides new platforms. Custom media
roots need corresponding destination adjustments.

Do not replace existing platform XML: favorites, statistics, manual metadata,
other games and version bindings need a deliberate merge. No generic in-place
merger is provided. The first ungrouped exporter used input-path game IDs; grouped
cards now use native platform plus catalog game key. Primary/version selection
and moving ROMs no longer change a known card's ID. Version IDs follow launch
paths/members; renaming catalog keys still changes card identity. Review migration
from old output instead of assuming the IDs or game counts are unchanged.

Emulators are unassigned on cards and versions. Configure a platform default or
review every version's emulator; changing just the main card may leave alternate
versions unassigned. Export installs no emulators, cores or BIOS.

## ScummVM

[ScummVM cards/media](scummvm.md) remain metadata preparation. Launch export needs
real folders, detection and reviewed bindings. The retired built-in integration
is not used. See the
[official guide](https://feedback.launchbox-app.com/en/help/articles/7897312-importing-and-managing-scummvm-games).
