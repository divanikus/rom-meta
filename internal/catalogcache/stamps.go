package catalogcache

// Track both files independently: a newer fingerprint file must not mask a
// same-size game.yaml edit, or vice versa. -1 means fingerprints are absent.
type bundleStamp struct {
	size               int64
	mtimeNS            int64
	fingerprintSize    int64
	fingerprintMTimeNS int64
}

// Preserve unrelated snapshot tables. Existing payloads are invalidated lazily
// by the -2 sentinel, which cannot match either a real file or an absent file.
func (c *cache) migrateFileStamps() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"ALTER TABLE catalog_games ADD COLUMN fingerprint_size INTEGER NOT NULL DEFAULT -2",
		"ALTER TABLE catalog_games ADD COLUMN fingerprint_mtime_ns INTEGER NOT NULL DEFAULT 0",
		"DROP INDEX IF EXISTS idx_catalog_games_resume",
		"UPDATE catalog_cache_meta SET schema_version = 2 WHERE id = 1",
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Reparse arcade rows to populate authority IDs without touching snapshots or other platforms.
func (c *cache) migrateArcadeIdentity() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"ALTER TABLE catalog_games ADD COLUMN arcade_platform TEXT NOT NULL DEFAULT ''",
		"UPDATE catalog_games SET size = -2 WHERE platform = 'Arcade' OR platform LIKE 'Arcade - %'",
		"UPDATE catalog_cache_meta SET schema_version = 3 WHERE id = 1",
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
