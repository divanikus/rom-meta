# RomM server importer

The version-specific server integration lives in
[examples/romm-importer](../examples/romm-importer/README.md), together with Docker/
Compose files and synthetic tests. That README owns setup, permissions, runtime
requirements, commands and limitations. For normal plan/review/apply/repeat use the
[operator guide](romm-import-guide.md). This example accesses native DB/Resources;
it is not a stock-RomM-HTTP-API uploader or an installable RomM plugin.

Prepared variants use the terminal base for metadata and retain their own
identity/proof in the native plan. See the example's prepared-variants section
for hash, name/stat and CHD-header evidence; source collection remains in the
[external helper](prepare-variants.md).
