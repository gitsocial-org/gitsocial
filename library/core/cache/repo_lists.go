// repo_lists.go - External repository list caching and queries
package cache

import (
	"time"
)

type ExternalRepoList struct {
	RepoURL      string
	ListID       string
	Name         string
	Version      string
	CommitHash   string
	CachedAt     time.Time
	Repositories []ListRepository
}

// InsertExternalRepoList stores a list from an external repository.
func InsertExternalRepoList(list ExternalRepoList) error {
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
		INSERT OR REPLACE INTO social_repo_lists (repo_url, list_id, name, version, commit_hash, cached_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		list.RepoURL,
		list.ListID,
		list.Name,
		list.Version,
		list.CommitHash,
		list.CachedAt.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM social_repo_list_repositories WHERE owner_repo_url = ? AND list_id = ?",
		list.RepoURL, list.ListID)
	if err != nil {
		return err
	}

	for _, repo := range list.Repositories {
		_, err = tx.Exec(`
			INSERT OR REPLACE INTO social_repo_list_repositories (owner_repo_url, list_id, repo_url, branch)
			VALUES (?, ?, ?, ?)`,
			list.RepoURL,
			list.ListID,
			repo.RepoURL,
			repo.Branch,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetExternalRepoLists returns all lists defined by a repository.
func GetExternalRepoLists(repoURL string) ([]ExternalRepoList, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	rows, err := db.Query(`
		SELECT repo_url, list_id, name, version, commit_hash, cached_at
		FROM social_repo_lists WHERE repo_url = ?
		ORDER BY name`, repoURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []ExternalRepoList
	listIndex := make(map[string]int)

	for rows.Next() {
		var list ExternalRepoList
		var cachedAt string
		var commitHash *string

		err := rows.Scan(
			&list.RepoURL,
			&list.ListID,
			&list.Name,
			&list.Version,
			&commitHash,
			&cachedAt,
		)
		if err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, cachedAt); err == nil {
			list.CachedAt = t
		}
		if commitHash != nil {
			list.CommitHash = *commitHash
		}

		key := list.RepoURL + ":" + list.ListID
		listIndex[key] = len(lists)
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(lists) == 0 {
		return lists, nil
	}

	repoQuery := `SELECT owner_repo_url, list_id, repo_url, branch
		FROM social_repo_list_repositories
		WHERE owner_repo_url = ?`

	repoRows, err := db.Query(repoQuery, repoURL)
	if err != nil {
		return nil, err
	}
	defer repoRows.Close()

	for repoRows.Next() {
		var ownerURL, listID, repoURLVal, branch string
		if err := repoRows.Scan(&ownerURL, &listID, &repoURLVal, &branch); err != nil {
			return nil, err
		}
		key := ownerURL + ":" + listID
		if idx, ok := listIndex[key]; ok {
			lists[idx].Repositories = append(lists[idx].Repositories, ListRepository{
				ListID:  listID,
				RepoURL: repoURLVal,
				Branch:  branch,
			})
		}
	}

	return lists, repoRows.Err()
}

// GetExternalRepoListCount returns the number of lists from a repository.
func GetExternalRepoListCount(repoURL string) (int, error) {
	db := dbPtr.Load()
	if db == nil {
		return 0, ErrNotOpen
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM social_repo_lists WHERE repo_url = ?`, repoURL).Scan(&count)
	return count, err
}

// DeleteExternalRepoLists removes all cached lists from a repository.
func DeleteExternalRepoLists(repoURL string) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	_, err := db.Exec("DELETE FROM social_repo_lists WHERE repo_url = ?", repoURL)
	return err
}
