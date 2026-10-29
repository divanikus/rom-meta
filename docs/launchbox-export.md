# LaunchBox export

Generate native LaunchBox platform XML and selected local media from existing
ROM files. Local catalogs and remote APIs use the same recognition, title and
source-selection rules. This is an experimental staging exporter.

## Preview, create and inspect

Run the client on the Windows machine that will launch the games, using a ROM
path available there. The command references ROMs without copying or changing them.

```powershell
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --out "D:\Exports\n64-check" --limit 10
rom-meta export --target launchbox --platform n64 --data-root "D:\Catalog" --rom-root "D:\Games\n64" --out "D:\Exports\n64-check" --limit 10 --apply
```

Preview writes no package. Apply creates a new directory and refuses existing
output. Review `preview.html`, `report.txt`, `manifest.json` and `README.txt`.
The manifest includes private absolute paths and media-selection provenance.

Use `--api "http://catalog-host:8787"` instead of `--data-root` for a remote catalog.
The client validates normal API observations/assets, then renders LaunchBox locally.
The existing export API needs no new endpoint or target support.

```powershell
rom-meta export --target launchbox --api "http://catalog-host:8787" --platforms nes,snes,psx --rom-root "D:\Games" --out "D:\Exports\library-check" --limit 10 --apply
```

`--platforms` and `--all` use the same system-directory IDs as ES-DE; they do not
infer arbitrary LaunchBox folder names. `--list-systems` lists available readers.
Existing selection profiles, including their `target: es-de` header, remain valid
shared policies; `--target launchbox` controls output layout. Source, language,
region preferences and title formatting stay in those profiles.

## Package and installation

| Package path | Use |
| --- | --- |
| `Data/Platforms/<LaunchBox platform>.xml` | Native game records |
| `Images/<platform>/<image type>/<game UUID>-01.<ext>` | Selected images |
| `Videos/<platform>/<game UUID>-01.<ext>` | Video, linked by VideoPath |
| `Manuals/<platform>/<game UUID>-01.pdf` | Manual, linked by ManualPath |
| Reports, preview and manifest | Review/provenance; not library data |

Image mappings include Box - Front, Box - Back, Box - 3D, Clear Logo, Screenshot -
Gameplay, Screenshot - Game Title and Fanart - Background. Selection controls
which roles are included. Formats are copied unchanged; frontend format support
needs separate verification. UUID filenames isolate equal display titles.

Close LaunchBox and Big Box; back up Data. For **new platforms**, copy platform
XML into `LaunchBox\Data\Platforms` and media into the corresponding installation
folders. If needed, create the exact platform name in Tools > Manage > Platforms
with its matching Scrape As value. Custom media-folder overrides need the
corresponding destination adjustment.

Do not replace existing platform XML: it may contain other games, owner metadata,
favorites, play statistics and emulator choices. No in-place merger is provided.
Existing platforms need a deliberate merge outside this exporter.

Emulators are left unassigned. Configure and select an emulator in LaunchBox
before playing. Export does not install emulators, cores or BIOS. Application
paths reference original absolute files; keep network mappings available.
IDs depend on platform and launch path, remaining stable when metadata or export
directory changes. Moving ROMs changes IDs. Provider snapshot IDs are not exported
as native LaunchBox DatabaseID bindings.

## Cartridges, discs and multiple discs

Coverage matches the [shared launch-reader table](export-scenarios.md#es-de-input-coverage):
supported raw cartridges and single-payload ZIPs, single CHD/ISO and modern disc
formats, directory-style playlists, and saved MAME set names.

A supported multidisc directory has a same-name inner playlist:

```text
Game (USA).m3u/
  Game (USA).m3u
  Game (USA) (Disc 1).chd
  Game (USA) (Disc 2).chd
```

The inner playlist lists relative CHD/ISO members in order. Every member must
bind uniquely to the same catalog game. Export creates one card pointing to the
actual inner M3U, preserving order and avoiding separate disc entries. Apply
rechecks the playlist and every disc.

Single files remain separate launches. Export does not guess groups by title or
create Additional Versions. Ordinary flat M3U, CUE/BIN, GDI and PBP retain shared
reader limits and are reported as unsupported. CHD container/name evidence does
not become verified Redump disc evidence. Unknown/ambiguous supported inputs have
filename-only cards; unsupported inputs and unbound arcade sets remain in reports
and are omitted from XML.

The selected emulator receives the existing M3U. Disc swapping, save identity and
core/format support require an emulator test. See
[LaunchBox's multidisc guide](https://feedback.launchbox-app.com/en/help/articles/8972270-multi-disc-games-and-m3u-playlists).

## ScummVM

RomMeta already prepares [ScummVM cards and media](scummvm.md) without game files.
Those cards do not establish a launchable folder, detected engine/game target,
release or language. Export does not create launches from catalog cards alone.

Current LaunchBox configures standalone ScummVM as an ordinary emulator and
imports game folders. A future adapter needs real folders and ScummVM detection,
then a reviewed mapping to cards. ScummVM-specific multidisc data assembly must
not assume console M3U behavior. The retired built-in UseScummVM integration is
not a basis for new entries. See the
[official ScummVM import guide](https://feedback.launchbox-app.com/en/help/articles/7897312-importing-and-managing-scummvm-games).
