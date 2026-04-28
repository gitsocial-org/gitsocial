// repos.go - Repository metadata storage and fetch status tracking
package cache

import (
	"database/sql"
	"time"
)

type RepositoryFetchMeta struct {
	HasCommits       bool
	CommitCount      int
	OldestCommitTime time.Time
	NewestCommitTime time.Time
}

// GetRepositoryFetchMeta returns commit statistics for a repository.
func GetRepositoryFetchMeta(url string) (*RepositoryFetchMeta, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	meta := &RepositoryFetchMeta{}

	row := db.QueryRow(`
		SELECT COUNT(*), MIN(timestamp), MAX(timestamp)
		FROM core_commits WHERE repo_url = ?`, url)

	var minTS, maxTS sql.NullString
	if err := row.Scan(&meta.CommitCount, &minTS, &maxTS); err != nil {
		return meta, err
	}

	meta.HasCommits = meta.CommitCount > 0
	if minTS.Valid {
		if t, err := time.Parse(time.RFC3339, minTS.String); err == nil {
			meta.OldestCommitTime = t
		}
	}
	if maxTS.Valid {
		if t, err := time.Parse(time.RFC3339, maxTS.String); err == nil {
			meta.NewestCommitTime = t
		}
	}

	return meta, nil
}

// IsRepositoryInAnyList checks if a repository URL is in any list for the given workdir.
// This is the canonical way to determine if a repo is "followed".
func IsRepositoryInAnyList(url, workdir string) bool {
	db := dbPtr.Load()
	if db == nil {
		return false
	}
	var exists int
	row := db.QueryRow(`
		SELECT 1 FROM core_list_repositories lr
		JOIN core_lists l ON lr.list_id = l.id
		WHERE lr.repo_url = ? AND l.workdir = ?
		LIMIT 1`, url, workdir)
	if err := row.Scan(&exists); err != nil {
		return false
	}
	return exists == 1
}

type Repository struct {
	URL         string
	Branch      string
	StoragePath string
	LastFetch   sql.NullString
}

// InsertRepository stores or updates repository metadata.
func InsertRepository(repo Repository) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO core_repositories
		(url, branch, storage_path, last_fetch)
		VALUES (?, ?, ?, ?)`,
		repo.URL,
		repo.Branch,
		repo.StoragePath,
		repo.LastFetch,
	)
	return err
}

// GetRepository retrieves a repository by URL.
func GetRepository(url string) (*Repository, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	row := db.QueryRow(`
		SELECT url, branch, storage_path, last_fetch
		FROM core_repositories WHERE url = ?`, url)

	var repo Repository
	err := row.Scan(
		&repo.URL,
		&repo.Branch,
		&repo.StoragePath,
		&repo.LastFetch,
	)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetRepositories returns all tracked repositories.
func GetRepositories() ([]Repository, error) {
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	rows, err := db.Query(`
		SELECT url, branch, storage_path, last_fetch
		FROM core_repositories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []Repository
	for rows.Next() {
		var repo Repository
		err := rows.Scan(
			&repo.URL,
			&repo.Branch,
			&repo.StoragePath,
			&repo.LastFetch,
		)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}

	return repos, rows.Err()
}

// SetRepositoryMeta stores a key/value pair for a repository.
func SetRepositoryMeta(repoURL, key, value string) error {
	return ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT OR REPLACE INTO core_repository_meta (repo_url, key, value) VALUES (?, ?, ?)`,
			repoURL, key, value)
		return err
	})
}

// GetRepositoryMeta retrieves a metadata value for a repository.
func GetRepositoryMeta(repoURL, key string) (string, error) {
	return QueryLocked(func(db *sql.DB) (string, error) {
		var value string
		err := db.QueryRow(`SELECT value FROM core_repository_meta WHERE repo_url = ? AND key = ?`,
			repoURL, key).Scan(&value)
		if err != nil {
			return "", err
		}
		return value, nil
	})
}

// UpdateRepositoryLastFetch updates the last fetch timestamp for a repository.
func UpdateRepositoryLastFetch(url string) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	_, err := db.Exec(`
		UPDATE core_repositories SET last_fetch = ? WHERE url = ?`,
		time.Now().Format(time.RFC3339),
		url,
	)
	return err
}
