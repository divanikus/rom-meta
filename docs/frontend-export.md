# Export and enrich a frontend

Use one command for both built-in targets, ES-DE and LaunchBox. It adds missing
games and enriches existing entries using a prepared local catalog or the same
catalog API. ROMs and catalogs remain unchanged. Choose the destination and
inspect the plan; add `--apply` to write it with backups.

```powershell
rom-meta export --target launchbox --platform nes --data-root "D:\Catalog" --rom-root "D:\ROMs\nes" --destination "D:\LaunchBox"
rom-meta export --target launchbox --platform nes --data-root "D:\Catalog" --rom-root "D:\ROMs\nes" --destination "D:\LaunchBox" --apply

rom-meta export --target es-de --platform nes --data-root "D:\Catalog" --rom-root "D:\ROMs\nes" --destination "D:\ES-DE"
rom-meta export --target es-de --platform nes --data-root "D:\Catalog" --rom-root "D:\ROMs\nes" --destination "D:\ES-DE" --apply
```

For LaunchBox the destination is the installation containing `Data/Platforms`.
For ES-DE it is the application data/home directory containing `settings`,
`gamelists` and normally `downloaded_media`, rather than necessarily the executable
directory. Close the target frontend before applying. Process checks run on
Windows for both targets and on Linux for ES-DE.

## Common options and behavior

| Option | Both targets |
| --- | --- |
| `--target es-de` or `--target launchbox` | Select the native adapter |
| `--data-root <catalog>` or `--api <origin>` | Select metadata/media source |
| `--rom-root <directory>` | Existing launchable ROM inputs |
| `--platform <code/name/alias>` | One platform ROM directory |
| `--platforms nes,snes,psx` or `--all` | Same selectors across short or long platform folder names |
| `--destination <directory>` | Add games and enrich the installed frontend |
| `--profile <yaml>` | Shared selection priorities; adapter-specific preferences remain optional |
| `--match <text>`, `--limit <count>` | Bound the observed input selection; avoid partial disc sets |
| `--report <new.json>` | Optional new machine-readable destination plan |
| `--apply` | Apply; omission always means preview |
| `--out <new directory>` | Optional portable package instead of a destination update |
| `--package <existing directory>` | Optional prepared package as the source for a destination update |

The same destination operation creates new entries and fills missing metadata,
versions and media on existing ones. Nonempty metadata and existing media are
preserved. Favorites, play history, emulator overrides, main launch choices,
unknown native fields and unrelated entries survive. Each adapter uses native
identity/launch evidence; title similarity never identifies an existing card.
Conflicts refuse application before downloads or library writes.

Preview prints `add`, `enrich`, `unchanged` and `conflict` with fields, versions,
media, playlists and file counts. It creates no staging directory, copies no media
and downloads no API assets. `--report` explicitly writes only the requested new
JSON report; it contains private paths. Apply prepares validated files internally,
rechecks the destination, then installs them. No manual package/export/import step
is required. An unchanged apply skips preparation and asset downloads, writes no
files and creates no backup.

Replaced files are backed up under `<destination>/Backups/RomMeta/<run>/`.
`changes.json` lists roots, relative paths, new files and expected checksums.
Assets are prepared before XML publication. This is not a multi-file transaction;
a partial I/O failure may require reviewed manual recovery. No automatic restore
overwrites later edits. Do not interrupt an active frontend save.

See [platform selection](platform-selection.md) for the unified code/name/alias list,
folder discovery and the distinction between name recognition and ROM evidence.

## Local, API and batch sources

Change only the source option for a network catalog:

```shell
rom-meta export --target es-de --api "http://catalog-host:8787" --platforms nes,snes,psx --rom-root "/games/roms" --destination "/home/player/ES-DE"
rom-meta export --target es-de --api "http://catalog-host:8787" --platforms nes,snes,psx --rom-root "/games/roms" --destination "/home/player/ES-DE" --apply
```

Use `ROMMETA_API_TOKEN` if the server requires it. ROM bytes remain on the client.
A batch uses one shared destination plan; source reading, provenance and native
preservation rules match single-system and local mode. See [input coverage](export-scenarios.md)
and [API transport](offline-export.md#export-from-a-remote-catalog).

## Optional portable packages

Use `--out` when the result needs transport, HTML review or separate installation:

```powershell
rom-meta export --target launchbox --platform nes --data-root "D:\Catalog" --rom-root "D:\ROMs\nes" --out "D:\Exports\nes" --apply
rom-meta export --target launchbox --package "D:\Exports\nes" --destination "D:\LaunchBox"
rom-meta export --target launchbox --package "D:\Exports\nes" --destination "D:\LaunchBox" --apply
```

ES-DE uses the same options with `--target es-de`. `--destination` and `--out` are
mutually exclusive. `--package` replaces catalog/ROM/profile/filter inputs; use the
package's recorded selection and native target. Existing package outputs are
never overwritten. Packages retain preview/report/manifests and may contain
private absolute source paths. Source paths must remain usable on the target
machine; packaging does not relocate or copy ROMs.

The older `launchbox merge --package ... --launchbox-root ...` command is a
compatibility alias for this same pipeline. New workflows use `export`.

## Native differences

- [LaunchBox](launchbox-export.md) groups catalog games into cards with Additional
  Versions and edition-specific M3Us. Existing IDs/main launches remain. Ambiguous
  old-card folds and custom native media-folder overrides require separate review.
- [ES-DE](offline-export.md) merges gamelists by launch path, retaining folder,
  favorite/history and alternative-emulator data. Its media path comes from
  `settings/es_settings.xml` (or legacy `es_settings.xml`), defaulting to
  `<destination>/downloaded_media`. `--media-root` can explicitly select another
  path, including a new external directory. Existing alternate image formats are
  retained. Configured ROMDirectory/custom-system paths must agree with the source.
  Unresolved path variables and legacy gamelists beside ROMs are rejected rather
  than guessed. Settings themselves are never modified.

[RomM](romm-server-importer.md) remains a separate server-side integration with
its own runtime and setup; it is not a built-in filesystem export target. This
shared native-adapter interface applies to the two built-in exporters. It does
not add emulator installation, ROM conversion, BIOS or gameplay verification.
