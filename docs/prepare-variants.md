# Stage prepared variant links

The separate `scripts/prepare-variants` helper converts explicit relationships
into ordinary [variant and fingerprint YAML](catalog-variants.md). It runs from a
source checkout with the Go toolchain in `go.mod`; normal offline exports do not
need it. No additional dependency or core CLI command is installed.

A source adapter or reviewer supplies the exact game folder and `based_on` key.
The helper checks actual modified payloads and catalog preconditions. It does not
discover parents, repair source names, download sources or independently establish
that a source's parent assertion is correct.

## Preview and stage

Run from the checkout root. Keep the source catalog, ROMs, pinned snapshots,
relationship JSON and staging directory outside Git:

```shell
go run ./scripts/prepare-variants --catalog "/catalog" --rom-root "/roms" --sources "/snapshots" --links "/work/links.json" --out "/work/variant-catalog" --report "/work/preview.json"
```

Review the held queue, errors, file count and copy size. Repeat with `--apply`
and a new report path to create the staging catalog. Preview writes only an
explicitly requested report. Errors in proposed entries block the entire apply;
deliberately held entries are retained in reports and never imported.

The output is a **sparse catalog containing selected bundles**, with copied
platform headers and source policies. Packed source bundles are read logically
and become loose files in staging. Configuration, secrets, indexes and source
revision markers are excluded. Unknown YAML fields and existing variants are
preserved; duplicate/merged YAML keys and aliases require manual preparation.

`--media artwork` is the default: copy image files, omitting video and manuals.
Use `--media none` for a metadata-only check or `--media all` for every local
asset. The preview estimates the selected copy size before writing.

The helper validates staging with the same catalog validator before publishing
the output directory. A failed temporary stage remains available for diagnosis.
One identical `--apply` repeat verifies the saved receipt and file hashes and
returns `unchanged`. Changed or unrelated output directories are refused.

This helper writes only staging. Keep any eventual production merge separate,
coordinate it with catalog writers and recheck the current catalog before merging.
Do not replace newer production bundles with an older sparse staging copy.

## Relationship JSON

This is the helper's versioned input, not a core import command. One entry defines
one modified payload identity and may list several input paths with the same
bytes. Paths are relative to `--rom-root`, starting with the exact platform
folder. Both catalog SHA256 values describe the original source documents before
any entry in this plan is added.

The example hashes and size are placeholders; replace them with actual values:

```json
{
  "schema_version": 1,
  "entries": [
    {
      "platform": "Nintendo - Nintendo Entertainment System",
      "game": "Example Quest",
      "variant": {
        "key": "English 1.0",
        "title": "Translated Quest",
        "kind": "translation",
        "based_on": "Example Quest (Japan)",
        "languages": ["en"],
        "group": "Example team",
        "version": "1.0"
      },
      "file": {
        "name": "Translated Quest.nes",
        "size": 123456,
        "sha1": "0123456789abcdef0123456789abcdef01234567"
      },
      "evidence": {
        "relationship": "source-release-name",
        "links": [
          {
            "source": "Prepared source snapshot",
            "entry": "Translation record",
            "parent": "Example Quest (Japan)"
          }
        ]
      },
      "expected_documents": {
        "game.yaml": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        "fingerprints.yaml": "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
      },
      "inputs": [
        {
          "path": "Nintendo - Nintendo Entertainment System/translations/Renamed.zip",
          "member": "Renamed member.nes"
        }
      ]
    }
  ],
  "holds": [
    {"entry": "Unresolved record", "reason": "Exact base release unknown"}
  ]
}
```

Payloads currently supported by the helper are raw regular files and named ZIP
members, with a positive size and SHA1 and/or SHA256. Optional MD5 and CRC32 must
also agree. Omit `member` for a raw file. Optional input `size` and `mtime_ns`
bind the outer file's saved stat signature. Archives are read without extraction;
UTF-8 and legacy CP437 member names use the same decoder as catalog ZIPs and
export observations.
Representation-specific CHD identities and multi-payload sets need separate
preparation adapters; this helper does not reinterpret their hashes.

Top-level `snapshots` can pin local source files with `path`, `sha256` and
optional `url`; `path` is relative to `--sources`. Source files are checked
before preparation and again before publishing staging. Supplied source links
are copied into variant/fingerprint provenance; adapters should include snapshot
references and preserve their raw parent assertions. When a link declares
`snapshot` and `snapshot_sha256`, both must agree with a pinned snapshot.
A supplied variant title also becomes an explicit metadata-only name alias;
payload matching still requires the modified hash.

Allowed relationship evidence:

- `source-release-name`: the source names the parent. Optional
  `evidence.normalization` records explicit formatting handling. Provenance
  states that original ROM bytes are not proved by this name assertion.
- `source-base-hash`: a source link includes `base` with SHA1 and/or SHA256,
  optionally MD5/CRC32. Those hashes must agree with the exact official base
  release's fingerprint. This verifies the supplied source claim against the
  catalog; it does not read or reconstruct an unmodified ROM.
- `reviewed`: an explicit reviewed relationship; preserve the decision's
  reference in its source link.

Unknown top-level/entry fields and duplicate JSON keys fail. Names, parents,
strong hashes, input changes, conflicting ownership across selected bundles,
variant graphs and final catalog contents are checked. Source-specific extraction
and approval of relationships remain the adapter/reviewer's responsibility.

## Verify export

Use the ordinary CLI against staging and the selected ROM files:

```shell
rom-meta export --target es-de --platform nes --data-root "/work/variant-catalog" --rom-root "/roms/Nintendo - Nintendo Entertainment System" --out "/work/variant-export"
```

Review, then repeat with `--apply` into a new package. Inspect translated names,
launch paths, inherited descriptions and media, including provenance in
`manifest.json`. A metadata-only staging catalog intentionally has no media.
The existing [reader limits](export-scenarios.md#es-de-input-coverage) still apply;
preparing a relationship does not add a platform launch reader.
