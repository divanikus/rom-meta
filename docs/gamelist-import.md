# Import existing gamelists

Harvest private ES-DE/EmulationStation metadata and explicitly referenced local
images, MP4 videos and PDF manuals into an existing catalog:

```shell
rom-meta gamelist import /path/to/gamelist.xml --data-root /path/to/catalog --collection personal --language ru --report /path/to/preview.json
rom-meta gamelist import /path/to/gamelist.xml --data-root /path/to/catalog --collection personal --language ru --report /path/to/import.json --apply
```

Use `--platform <name>` when the gamelist's `provider.system` is absent or does
not resolve through the platform registry. ZIP packs are supported directly;
select `--member system/gamelist.xml` if there are multiple gamelists. The ZIP
reader opens XML and referenced media members, without extracting ROM members.

`--collection` is a lowercase identifier (letters, digits, hyphen or underscore).
It names the source `gamelist.<collection>` in metadata, media filenames and the
root source policy. The technical adapter is recorded as `importer: es-de`.
Description language is explicit and independent of the gamelist's ROM language.

Only unique complete ROM basenames, release keys, authority names and explicit
release aliases on the selected platform are associated. Matching preserves
punctuation and qualifiers; it never strips translation/hack labels to guess a
parent. Unmatched and ambiguous entries remain in the private JSON report.
Frontend IDs and reported MD5 values remain source observations; they do not
create provider IDs, releases, fingerprints, aliases or fold evidence.

Existing canonical fields, other provider stanzas and unrelated fields survive.
Repeated observations use the same source-file/path identity; `--origin` supplies
a stable relative identifier if inputs are moved or renamed. Imports from
different gamelists in the same collection coexist. Repeating an unchanged import
writes no catalog data.

Referenced media uses source-labelled, content-addressed filenames inside the
matched release's media folder. Images, thumbnails, marquees, videos, fanart and
manuals retain their source paths and checksums. Missing files are reported;
available metadata/media can still be applied, with an incomplete exit status.
Unsupported formats, including fanart playlists, are reported as warnings.
Absolute paths, traversal and source symlink escapes are rejected. Media outside
the imported XML's directory tree, remote URLs, and modern frontend media located
only through filename inference require a separate adapter.

The default source policy is `redistribution: private`. Keep input packs, media,
reports and `.rommeta/imports/` outside Git. Applied imports retain original YAML
and a receipt there; failed publication retains its prepared stage. Recover
selected original YAML through ordinary catalog tools if needed. Catalog writers
must run sequentially.

To select the imported curation in an export profile, use its full source name:

```yaml
schema_version: 1
target: es-de
selection:
  description:
    languages: [ru, en]
    sources: [gamelist.personal, screenscraper, launchbox]
  media:
    cover:
      sources: [gamelist.personal, launchbox, screenscraper]
    video:
      sources: [gamelist.personal, emumovies, screenscraper]
```

Importing does not change an existing export profile or installed frontend.
Review the report before applying and use the ordinary export preview afterward.
