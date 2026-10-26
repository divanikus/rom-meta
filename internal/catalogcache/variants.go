package catalogcache

// Existing derived caches must reparse rows that formerly omitted variants.
// Snapshot tables and catalog files are preserved.
func (c *cache) migrateVariants() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present int
	if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info('catalog_games') WHERE name = 'variants_json'").Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		if _, err := tx.Exec("ALTER TABLE catalog_games ADD COLUMN variants_json TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return err
		}
	}
	for _, query := range []string{
		"UPDATE catalog_games SET size = -2",
		"UPDATE catalog_cache_meta SET schema_version = 4 WHERE id = 1",
	} {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	return tx.Commit()
}
