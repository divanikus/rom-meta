# RomM integration

The supported experimental path is the separate
[server importer example](../examples/romm-importer/README.md), with its own pinned
RomM runtime, setup and tests. Follow the [operator guide](romm-import-guide.md).
It enriches games already scanned in RomM from the independent RomMeta API.

RomMeta's internal `romm inspect-inputs` and `romm project-snapshot` JSON commands
are the protocol used by the server example. Use the example's own plan/apply/check
commands for import; the Go protocol commands are not interactive import tasks.
