# Retained backups

Successful merges and reviewed splits keep no backup by default. In the final
web review, check **Keep a backup of the affected platforms** to retain the
originals for that apply. This choice starts unchecked each time.

CLI opt-in:

```shell
rom-meta catalog fold --data-root /catalog --platform sega-cd --apply --keep-backup
rom-meta catalog split --data-root /catalog --review-plan split.json --apply --keep-backup
```

Without this option, temporary originals are removed after successful
activation. If application or rollback fails, the error reports any retained
recovery files. Existing backups are never automatically removed.

**Settings → Backups** lists retained platform and MAME backups, their logical
size, file count and folder modification time. **Delete…** asks for confirmation
and removes only that backup. Current library cards and shared hard-linked media
remain intact. Modification time is not necessarily the date of the merge;
logical size can exceed extra disk usage because media may be hard-linked.
Automatic expiry and restoration are not provided.

The same manager is available from CLI:

```shell
rom-meta catalog backups --data-root /catalog
rom-meta catalog backups --data-root /catalog --delete "Console.fold-backup-123"
# Review the entry, then confirm:
rom-meta catalog backups --data-root /catalog --delete "Console.fold-backup-123" --apply
```

The manager recognizes RomMeta platform/MAME directories directly under
`backups/`, including those created by earlier versions. It rejects symlinks,
changed revisions and deletion while a catalog writer is active. Other folders,
snapshots and incomplete temporary staging directories are outside this manager.
Do not delete recovery files from a failed operation until the catalog has been
checked.
