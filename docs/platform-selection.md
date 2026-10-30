# Select platforms and explain ROM folders

ES-DE and LaunchBox use one selector for local and API export. A platform has a
code, canonical name, accepted aliases, export targets/readers and separate
catalog availability. Catalog platforms use [generic ROM-set export](romset-export.md) by default, with
explicit IBM PC/ScummVM exclusions. Recognition does not promise a populated
catalog, matching ROMs or emulator compatibility.

```powershell
rom-meta platforms list --data-root "D:\Catalog"
rom-meta platforms list --data-root "D:\Catalog" --rom-root "D:\ROMs"
rom-meta platforms list --api "http://catalog-host:8787" --rom-root "D:\ROMs"
rom-meta platforms list --data-root "D:\Catalog" --full --format json
rom-meta export --list-platforms --data-root "D:\Catalog" --rom-root "D:\ROMs"
```

`export --list-systems` is a compatibility alias for the same list. Without a
catalog, listing falls back to the complete embedded registry and marks catalog
availability as not checked. With a catalog, the default list shows its platforms;
`--full` includes registry platforms absent from that catalog. JSON includes codes,
names, accepted aliases, reader/target support, catalog status and optional folder
explanations. `--format yaml` retains the editable registry configuration format.
Listing/discovery reads directory names, configuration and catalog platform headers;
it does not hash ROMs, inspect archives, index game cards or download media.

## Codes, names and aliases

These select the same Game Boy platform:

```powershell
rom-meta export --target launchbox --data-root "D:\Catalog" --platform gb --rom-root "D:\ROMs\Nintendo - Game Boy" --destination "D:\LaunchBox"
rom-meta export --target launchbox --data-root "D:\Catalog" --platform "Nintendo - Game Boy" --rom-root "D:\ROMs\Nintendo - Game Boy" --destination "D:\LaunchBox"
```

Multiple selection accepts the same values, separated by commas:

```powershell
rom-meta export --target launchbox --data-root "D:\Catalog" --platforms "gb,Nintendo - Game Boy Advance" --rom-root "D:\ROMs" --destination "D:\LaunchBox"
rom-meta export --target launchbox --data-root "D:\Catalog" --all --rom-root "D:\ROMs" --destination "D:\LaunchBox"
```

A collection may contain `gb`, `Nintendo - Game Boy`, `Nintendo - Game Boy Advance`,
`Sony - PlayStation` or other registered names. It need not use short frontend
folder IDs. `--platforms gb` finds an immediate long-named Game Boy folder;
`--all` examines every immediate visible folder. Inside each selected platform,
the ordinary reader recursively scans retail, translations and other subfolders.
This does not apply a retail-only, region or revision filter.

Preview is the default; add `--apply` after reviewing the destination plan. Change
`--target` and `--destination` for ES-DE; selection is identical. Native ES-DE ROM
configuration must still agree with the actual folder paths. See the
[shared export workflow](frontend-export.md).

## How a directory gets its platform

1. Compare the supplied selector or folder name with the shared registry. Match
   canonical names first, then explicit aliases, then provider names. Each tier
   requires one unique match. Case, punctuation and separators are normalized;
   letters and digits must match completely. There is no prefix or fuzzy match.
2. Map the selected identity to the common ROM-set observer or a specialized reader. An explicit reader ID
   such as `genesis`, `megadrive`, `arcade` or `mame` keeps that native system choice.
   Otherwise a matching folder's reader ID or the platform default is used.
3. Read catalog `_platform.yaml` headers to find the actual metadata dataset.
   A physical catalog folder can differ from the ROM folder. Existing related
   disc/digital grouping stays in effect; duplicate catalog headers remain ambiguous.
4. Only after selection does the reader inspect supported files and establish
   hash/name evidence. A directory label selects hardware; it does not verify a
   file's dump, region, revision, translation or hack.

For single selection, a recognized ancestor also identifies nested paths such as
`Nintendo - Game Boy/translations/ru`. An unrecognized generic folder such as
`Incoming` works with an explicit `--platform gb`. A known conflicting platform
name fails. A generic root containing several recognized platforms fails single
selection and directs the user to `--platforms` or `--all`.

Extensions such as `.bin`, `.iso`, `.zip` and archive member names are not used to
guess a directory's platform. Unknown or ambiguous folders in `--all` are reported
as skipped. Explicit unknown/ambiguous selectors and duplicate selections fail.
If several ROM folders identify one platform, explicit batch selection fails;
`--all` skips the conflicting folders. Use `--platform` with the exact ROM root.
Missing catalogs, explicit exclusions, ROM folders and empty selections are reported separately.
No first-match choice or automatic merge combines unrelated ROM trees or DATs.

Export previews print the chosen code/name, reader, actual ROM directory and
matching reason. Package manifests preserve this as `platform_selection`.

## Local names

Add aliases in the catalog's `config/platforms.yaml`:

```yaml
Nintendo - Game Boy:
  aliases: [My Pocket Collection]
```

The old `platform_aliases` map in `config/sources.yaml` remains supported. The list
shows only unambiguous accepted aliases. Conflicting names stay ambiguous. Codes
prefer an existing unambiguous short registry key; platforms without one use a
normalized canonical name. Consult the list rather than inventing abbreviations.

API capability responses optionally advertise canonical `name` and
`selector_aliases`. New clients still accept older version-1 capability responses
using their embedded registry and advertised catalog names. Server-only aliases
need the updated server; installing the CLI does not redeploy an existing API.
