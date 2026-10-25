# Catalog storage

New catalogs use loose YAML and media files. Existing marked platforms can use
per-game metadata and source/type media ZIPs; both layouts can coexist. Public
0.1.0 has no bulk storage conversion command. Read logical paths through RomMeta;
do not add a marker to a loose platform without constructing its complete packs.

```text
platforms/<platform>/
  catalog-storage.yaml     # version: 1; layout: game-source-zip; archive: torrentzip
  _platform.yaml
  _catalog.yaml
  <game>/
    metadata.zip           # game.yaml, fingerprints.yaml, other metadata sidecars
    artwork.launchbox.zip
    artwork.screenscraper.zip
    video.emumovies.zip
    media.<source>.zip     # other registered media
```

Members are game-relative, such as `game.yaml` or `media/Release/box-front.usa.png`.
YAML/API/export paths remain logical game paths, not archive member URLs. Packed
updates rewrite the affected pack in deterministic TorrentZip form; selected
stored media supports seek/Range, and deflated media retains validation on read.
Indexes are disposable and do not replace YAML source evidence.

Unmarked platforms remain loose. An explicit `catalog-conversion.pending` marker
blocks a partially prepared platform; automatic discovery excludes `.old`,
`.old.*` and `.converting` roots. Keep recovery backups/snapshots separately and
check changed data before restoring one logical file or platform.

Only Store/Deflate members with safe, unambiguous paths are supported. Encrypted,
symlink, duplicate/case-colliding or loose/packed-colliding entries fail explicitly.
Standard ZIP tools can inspect packs; stop catalog writers before external edits,
then restart the server so external changes are visible. A content edit invalidates
previous archive hashes/torrent pieces even when output is deterministic.

Dropping a source-specific media pack does not remove that provider's YAML evidence
or grant redistribution rights. Review source policy separately. Existing acquisition,
fold, browse/API and export commands support both layouts; catalogs and archives
are user data and are not included in the source repository or release binaries.
