package openvgdb

import "database/sql"

// The provider snapshot is the authoritative platform list and its own cache.
func readPlatformNames(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT DISTINCT systemName FROM SYSTEMS ORDER BY systemName")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
