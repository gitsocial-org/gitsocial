// lists.go - List metadata and repository membership operations
package cache

import (
	"database/sql"
	"strings"
	"time"
)

type CachedList struct {
	ID           string
	Name         string
	Source       sql.NullString
	Version      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Workdir      string
	Repositories []ListRepository
}

type ListRepository struct {
	ListID  string
	RepoURL string
	Branch  string
	AddedAt time.Time
}

// InsertList stores a list with all its repositories.
func InsertList(list CachedList) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO core_lists (id, name, source, version, created_at, updated_at, workdir)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		list.ID,
		list.Name,
		list.Source,
		list.Version,
		list.CreatedAt.Format(time.RFC3339),
		list.UpdatedAt.Format(time.RFC3339),
		list.Workdir,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM core_list_repositories WHERE list_id = ?", list.ID)
	if err != nil {
		return err
	}

	for _, repo := range list.Repositories {
		_, err = tx.Exec(`
			INSERT OR REPLACE INTO core_list_repositories (list_id, repo_url, branch, added_at)
			VALUES (?, ?, ?, ?)`,
			list.ID,
			repo.RepoURL,
			repo.Branch,
			repo.AddedAt.Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetLists returns all lists for a workdir with their repositories.
func GetLists(workdir string) ([]CachedList, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	query := `SELECT id, name, source, version, created_at, updated_at, workdir FROM core_lists`
	var args []interface{}
	if workdir != "" {
		query += " WHERE workdir = ?"
		args = append(args, workdir)
	}
	query += " ORDER BY name"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CachedList
	var listIDs []string
	listIndex := make(map[string]int)

	for rows.Next() {
		var list CachedList
		var createdAt, updatedAt string

		err := rows.Scan(
			&list.ID,
			&list.Name,
			&list.Source,
			&list.Version,
			&createdAt,
			&updatedAt,
			&list.Workdir,
		)
		if err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			list.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			list.UpdatedAt = t
		}

		listIndex[list.ID] = len(lists)
		listIDs = append(listIDs, list.ID)
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(listIDs) == 0 {
		return lists, nil
	}

	placeholders := strings.Repeat("?,", len(listIDs))
	placeholders = placeholders[:len(placeholders)-1]
	repoQuery := `SELECT list_id, repo_url, branch, added_at FROM core_list_repositories WHERE list_id IN (` + placeholders + `)`

	repoArgs := make([]interface{}, len(listIDs))
	for i, id := range listIDs {
		repoArgs[i] = id
	}

	repoRows, err := db.Query(repoQuery, repoArgs...)
	if err != nil {
		return nil, err
	}
	defer repoRows.Close()

	for repoRows.Next() {
		var repo ListRepository
		var addedAt string
		if err := repoRows.Scan(&repo.ListID, &repo.RepoURL, &repo.Branch, &addedAt); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, addedAt); err == nil {
			repo.AddedAt = t
		}
		if idx, ok := listIndex[repo.ListID]; ok {
			lists[idx].Repositories = append(lists[idx].Repositories, repo)
		}
	}

	return lists, repoRows.Err()
}

// AddRepositoryToList adds a repository to a list.
func AddRepositoryToList(listID, repoURL, branch string) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO core_list_repositories (list_id, repo_url, branch, added_at)
		VALUES (?, ?, ?, ?)`,
		listID, repoURL, branch, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	_, err = db.Exec("UPDATE core_lists SET updated_at = ? WHERE id = ?",
		time.Now().Format(time.RFC3339), listID)
	return err
}

// RemoveRepositoryFromList removes a repository from a list.
func RemoveRepositoryFromList(listID, repoURL string) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	_, err := db.Exec("DELETE FROM core_list_repositories WHERE list_id = ? AND repo_url = ?",
		listID, repoURL)
	if err != nil {
		return err
	}

	_, err = db.Exec("UPDATE core_lists SET updated_at = ? WHERE id = ?",
		time.Now().Format(time.RFC3339), listID)
	return err
}

// GetListIDs returns all list IDs for a workdir.
func GetListIDs(workdir string) ([]string, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	query := "SELECT id FROM core_lists"
	var args []interface{}
	if workdir != "" {
		query += " WHERE workdir = ?"
		args = append(args, workdir)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
