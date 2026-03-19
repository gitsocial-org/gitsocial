// sync_tips.go - Persisted sync tips for workspace sync short-circuiting
package cache

import "database/sql"

// GetSyncTip returns the persisted sync tip for the given key.
func GetSyncTip(key string) (string, error) {
	tips, err := QueryLocked(func(db *sql.DB) ([]string, error) {
		var tip string
		err := db.QueryRow("SELECT tip FROM core_sync_tips WHERE key = ?", key).Scan(&tip)
		if err != nil {
			return nil, err
		}
		return []string{tip}, nil
	})
	if err != nil || len(tips) == 0 {
		return "", err
	}
	return tips[0], nil
}

// SetSyncTip persists a sync tip for the given key.
func SetSyncTip(key, tip string) error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("INSERT OR REPLACE INTO core_sync_tips (key, tip) VALUES (?, ?)", key, tip)
		return err
	})
}
