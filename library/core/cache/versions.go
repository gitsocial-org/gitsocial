// versions.go - Message versioning, edit tracking, and canonical resolution
package cache

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type Version struct {
	EditRepoURL      string
	EditHash         string
	EditBranch       string
	CanonicalRepoURL string
	CanonicalHash    string
	CanonicalBranch  string
	IsRetracted      bool
	Timestamp        time.Time
}

type LatestVersionResult struct {
	RepoURL     string
	Hash        string
	Branch      string
	IsRetracted bool
	HasEdits    bool
}

type ResolveResult struct {
	RepoURL string
	Hash    string
	Branch  string
}

type LatestContentResult struct {
	Message  string
	HasEdits bool
}

// ProcessVersionFromHeader extracts edits/retracted fields from a parsed message header,
// resolves the canonical coordinates, and inserts a version record.
// No-op if the message has no edits field.
func ProcessVersionFromHeader(msg *protocol.Message, commitHash, repoURL, branch string) {
	if msg == nil {
		return
	}
	editsRef := msg.Header.Fields["edits"]
	isRetracted := msg.Header.Fields["retracted"] == "true"
	if editsRef == "" {
		return
	}
	canonical := protocol.ResolveRefWithDefaults(editsRef, repoURL, branch)
	if canonical.Hash == "" {
		return
	}
	_ = InsertVersion(repoURL, commitHash, branch, canonical.RepoURL, canonical.Hash, canonical.Branch, isRetracted)
}

// InsertVersion stores an edit relationship between commits.
func InsertVersion(editRepoURL, editHash, editBranch, canonicalRepoURL, canonicalHash, canonicalBranch string, isRetracted bool) error {
	return ExecLocked(func(db *sql.DB) error {
		// Validate canonical commit exists
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
			canonicalRepoURL, canonicalHash, canonicalBranch).Scan(&count); err != nil {
			return fmt.Errorf("version insert: failed to check canonical: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("version insert: canonical commit not found: %s#%s@%s", canonicalRepoURL, canonicalHash, canonicalBranch)
		}
		retracted := 0
		if isRetracted {
			retracted = 1
		}
		_, err := db.Exec(`
			INSERT OR REPLACE INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			editRepoURL, editHash, editBranch, canonicalRepoURL, canonicalHash, canonicalBranch, retracted)
		return err
	})
}

// GetLatestVersion returns the latest version of a commit.
// If no edits exist, returns the canonical commit info with HasEdits=false.
func GetLatestVersion(canonicalRepoURL, canonicalHash, canonicalBranch string) (LatestVersionResult, error) {
	return QueryLocked(func(db *sql.DB) (LatestVersionResult, error) {
		var latestRepoURL, latestHash, latestBranch string
		var isRetracted int

		err := db.QueryRow(`
			SELECT v.edit_repo_url, v.edit_hash, v.edit_branch, v.is_retracted
			FROM core_commits_version v
			JOIN core_commits c ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			ORDER BY c.timestamp DESC, v.edit_hash DESC
			LIMIT 1`,
			canonicalRepoURL, canonicalHash, canonicalBranch,
		).Scan(&latestRepoURL, &latestHash, &latestBranch, &isRetracted)

		if err == sql.ErrNoRows {
			return LatestVersionResult{RepoURL: canonicalRepoURL, Hash: canonicalHash, Branch: canonicalBranch, IsRetracted: false, HasEdits: false}, nil
		}
		if err != nil {
			return LatestVersionResult{}, err
		}

		return LatestVersionResult{RepoURL: latestRepoURL, Hash: latestHash, Branch: latestBranch, IsRetracted: isRetracted == 1, HasEdits: true}, nil
	})
}

// GetVersionHistory returns all versions of a commit ordered by timestamp DESC.
// First item is latest, last is canonical.
func GetVersionHistory(canonicalRepoURL, canonicalHash, canonicalBranch string) ([]Version, error) {
	return QueryLocked(func(db *sql.DB) ([]Version, error) {
		rows, err := db.Query(`
			SELECT v.edit_repo_url, v.edit_hash, v.edit_branch, v.canonical_repo_url, v.canonical_hash, v.canonical_branch,
			       v.is_retracted, c.timestamp
			FROM core_commits_version v
			JOIN core_commits c ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			ORDER BY c.timestamp DESC, v.edit_hash DESC`,
			canonicalRepoURL, canonicalHash, canonicalBranch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var versions []Version
		for rows.Next() {
			var v Version
			var isRetracted int
			var ts string
			if err := rows.Scan(&v.EditRepoURL, &v.EditHash, &v.EditBranch, &v.CanonicalRepoURL, &v.CanonicalHash, &v.CanonicalBranch, &isRetracted, &ts); err != nil {
				return nil, err
			}
			v.IsRetracted = isRetracted == 1
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				v.Timestamp = t
			}
			versions = append(versions, v)
		}
		return versions, rows.Err()
	})
}

// HasEdits returns true if the canonical commit has any edits.
func HasEdits(canonicalRepoURL, canonicalHash, canonicalBranch string) (bool, error) {
	return QueryLocked(func(db *sql.DB) (bool, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM core_commits_version
			WHERE canonical_repo_url = ? AND canonical_hash = ? AND canonical_branch = ?`,
			canonicalRepoURL, canonicalHash, canonicalBranch).Scan(&count)
		return count > 0, err
	})
}

// IsEdit returns true if this commit is an edit (not a canonical).
func IsEdit(repoURL, hash, branch string) (bool, error) {
	return QueryLocked(func(db *sql.DB) (bool, error) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM core_commits_version
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			repoURL, hash, branch).Scan(&count)
		return count > 0, err
	})
}

// GetCanonical returns the canonical commit info if this is an edit.
// Returns nil if the commit is not an edit.
func GetCanonical(repoURL, hash, branch string) (*Version, error) {
	return QueryLocked(func(db *sql.DB) (*Version, error) {
		var v Version
		var isRetracted int
		err := db.QueryRow(`
			SELECT edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted
			FROM core_commits_version
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			repoURL, hash, branch).Scan(&v.EditRepoURL, &v.EditHash, &v.EditBranch, &v.CanonicalRepoURL, &v.CanonicalHash, &v.CanonicalBranch, &isRetracted)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		v.IsRetracted = isRetracted == 1
		return &v, nil
	})
}

// ResolveToCanonical follows the edit chain to find the canonical version.
// If the commit is already canonical, returns the same repo/hash/branch.
func ResolveToCanonical(repoURL, hash, branch string) (string, string, string, error) {
	result, err := QueryLocked(func(db *sql.DB) (ResolveResult, error) {
		var canonicalRepoURL, canonicalHash, canonicalBranch string
		err := db.QueryRow(`
			SELECT canonical_repo_url, canonical_hash, canonical_branch
			FROM core_commits_version
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			repoURL, hash, branch).Scan(&canonicalRepoURL, &canonicalHash, &canonicalBranch)
		if err == sql.ErrNoRows {
			return ResolveResult{RepoURL: repoURL, Hash: hash, Branch: branch}, nil
		}
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{RepoURL: canonicalRepoURL, Hash: canonicalHash, Branch: canonicalBranch}, nil
	})
	return result.RepoURL, result.Hash, result.Branch, err
}

// ResolveRefToCanonical resolves a ref string to its canonical version.
// If the ref points to an edit, returns the canonical ref.
// If resolution fails or ref is already canonical, returns the original ref.
func ResolveRefToCanonical(refString string) string {
	parsed := protocol.ParseRef(refString)
	if parsed.Value == "" {
		return refString
	}
	canonicalRepoURL, canonicalHash, canonicalBranch, err := ResolveToCanonical(parsed.Repository, parsed.Value, parsed.Branch)
	if err != nil || canonicalHash == "" {
		return refString
	}
	if canonicalRepoURL == "" {
		canonicalRepoURL = parsed.Repository
	}
	if canonicalBranch == "" {
		canonicalBranch = parsed.Branch
	}
	return protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, canonicalRepoURL, canonicalBranch)
}

// GetLatestContent returns the message content of the latest version.
// Resolves to canonical first, then finds latest edit's content.
func GetLatestContent(repoURL, hash, branch string) (string, bool, error) {
	result, err := QueryLocked(func(db *sql.DB) (LatestContentResult, error) {
		// First resolve to canonical if this is an edit
		canonicalRepoURL, canonicalHash, canonicalBranch := repoURL, hash, branch
		err := db.QueryRow(`
			SELECT canonical_repo_url, canonical_hash, canonical_branch
			FROM core_commits_version
			WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
			repoURL, hash, branch).Scan(&canonicalRepoURL, &canonicalHash, &canonicalBranch)
		if err != nil && err != sql.ErrNoRows {
			return LatestContentResult{}, err
		}

		// Find latest edit's content
		var latestMessage string
		err = db.QueryRow(`
			SELECT c.message
			FROM core_commits_version v
			JOIN core_commits c ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			ORDER BY c.timestamp DESC, v.edit_hash DESC
			LIMIT 1`,
			canonicalRepoURL, canonicalHash, canonicalBranch).Scan(&latestMessage)

		if err == sql.ErrNoRows {
			// No edits, get canonical content
			err = db.QueryRow(`
				SELECT message FROM core_commits
				WHERE repo_url = ? AND hash = ? AND branch = ?`,
				canonicalRepoURL, canonicalHash, canonicalBranch).Scan(&latestMessage)
			return LatestContentResult{Message: latestMessage, HasEdits: false}, err
		}
		if err != nil {
			return LatestContentResult{}, err
		}

		return LatestContentResult{Message: latestMessage, HasEdits: true}, nil
	})
	return result.Message, result.HasEdits, err
}

// ReconcileVersions populates missing version records for commits with edits field.
// This handles cases where edits were fetched before their canonicals.
// Returns the number of version records created.
func ReconcileVersions() (int, error) {
	type pendingVersion struct {
		editRepoURL      string
		editHash         string
		editBranch       string
		canonicalRepoURL string
		canonicalHash    string
		canonicalBranch  string
		isRetracted      bool
	}

	// Phase 1: Read pending edits under read lock
	pending, err := QueryLocked(func(db *sql.DB) ([]pendingVersion, error) {
		rows, err := db.Query(`
			SELECT c.repo_url, c.hash, c.branch, c.edits, c.message
			FROM core_commits c
			WHERE c.edits IS NOT NULL AND c.edits != ''
			  AND NOT EXISTS (
			      SELECT 1 FROM core_commits_version v
			      WHERE v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			  )`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var result []pendingVersion
		for rows.Next() {
			var repoURL, hash, branch, edits, message string
			if err := rows.Scan(&repoURL, &hash, &branch, &edits, &message); err != nil {
				return nil, err
			}
			parsed := protocol.ParseRef(edits)
			if parsed.Value == "" {
				continue
			}
			canonicalRepoURL := parsed.Repository
			if canonicalRepoURL == "" {
				canonicalRepoURL = repoURL
			}
			canonicalBranch := parsed.Branch
			if canonicalBranch == "" {
				canonicalBranch = branch
			}
			isRetracted := false
			if msg := protocol.ParseMessage(message); msg != nil {
				isRetracted = msg.Header.Fields["retracted"] == "true"
			}
			result = append(result, pendingVersion{
				editRepoURL:      repoURL,
				editHash:         hash,
				editBranch:       branch,
				canonicalRepoURL: canonicalRepoURL,
				canonicalHash:    parsed.Value,
				canonicalBranch:  canonicalBranch,
				isRetracted:      isRetracted,
			})
		}
		return result, rows.Err()
	})
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	// Phase 2: Write version records under write lock
	created := 0
	err = ExecLocked(func(db *sql.DB) error {
		for _, p := range pending {
			var exists int
			if err := db.QueryRow(`SELECT 1 FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
				p.canonicalRepoURL, p.canonicalHash, p.canonicalBranch).Scan(&exists); err != nil {
				continue
			}
			retracted := 0
			if p.isRetracted {
				retracted = 1
			}
			if _, err := db.Exec(`
				INSERT OR IGNORE INTO core_commits_version
				(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				p.editRepoURL, p.editHash, p.editBranch, p.canonicalRepoURL, p.canonicalHash, p.canonicalBranch, retracted); err == nil {
				created++
			}
		}
		return nil
	})
	return created, err
}
