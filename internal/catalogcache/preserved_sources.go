package catalogcache

// Preserve snapshot tables while invalidating catalog rows that formerly omitted
// owner fetch controls. The covering resume index keeps ordinary repeats cheap.
func (c *cache) migratePreservedSources() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present int
	if err = tx.QueryRow("SELECT count(*) FROM pragma_table_info('catalog_games') WHERE name = 'preserved_sources_json'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		if _, err = tx.Exec("ALTER TABLE catalog_games ADD COLUMN preserved_sources_json TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return err
		}
	}
	for _, query := range []string{"DROP INDEX IF EXISTS idx_catalog_games_resume", "UPDATE catalog_games SET size = -2", "UPDATE catalog_cache_meta SET schema_version = 5 WHERE id = 1"} {
		if _, err = tx.Exec(query); err != nil {
			return err
		}
	}
	return tx.Commit()
}
