// commits.go - Commit storage, retrieval, and synchronization with cache
package cache

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type Commit struct {
	Hash        string
	RepoURL     string
	Branch      string
	AuthorName  string
	AuthorEmail string
	Message     string
	Timestamp   time.Time
	FetchedAt   time.Time
	SignerKey   string
}

// commitInsertBatchSize bounds how many commits go into a single InsertCommits
// orchestration unit. The background workspace sync reports progress per batch,
// so this number governs the granularity of progress updates.
const commitInsertBatchSize = 10000

// commitTxnSize bounds how many commits go into a single SQL transaction (and
// therefore how long the cache write lock is held). Smaller transactions cost a
// little more BEGIN/COMMIT overhead in aggregate, but they shorten the worst
// case wait for concurrent readers (UI renders during kernel-scale background
// sync) by ~10×.
const commitTxnSize = 1000

// InsertCommits batch inserts commits and populates version records for edits.
// Inputs larger than commitInsertBatchSize are processed in chunks; each chunk
// is further split into commitTxnSize transactions so the write lock releases
// between transactions and concurrent readers can interleave.
func InsertCommits(commits []Commit) error {
	if len(commits) == 0 {
		return nil
	}
	for start := 0; start < len(commits); start += commitInsertBatchSize {
		end := start + commitInsertBatchSize
		if end > len(commits) {
			end = len(commits)
		}
		if err := insertCommitsBatch(commits[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// insertCommitsBatch processes one orchestration chunk by splitting it into
// per-transaction slices of at most commitTxnSize. Each slice acquires the
// write lock independently so readers can interleave between transactions.
func insertCommitsBatch(commits []Commit) error {
	for start := 0; start < len(commits); start += commitTxnSize {
		end := start + commitTxnSize
		if end > len(commits) {
			end = len(commits)
		}
		if err := insertCommitsTxn(commits[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// insertCommitsTxn inserts up to commitTxnSize commits in a single transaction.
//
// This function only writes rows that belong to the commits being inserted:
// the commit's own core_commits row, its FTS row (when not an edit), and the
// edit→canonical link in core_commits_version (when the canonical is already
// in the cache). All denormalized resolved-state on canonical rows
// (resolved_message, has_edits, is_retracted, labels, FTS, mutable extension
// columns) is written by applyEditToCanonical, called once per affected
// canonical at the end of the loop. This keeps the canonical-update logic in
// one place — see versions.go.
func insertCommitsTxn(commits []Commit) error {
	db := dbPtr.Load()
	if db == nil {
		return ErrNotOpen
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	commitStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO core_commits (
			repo_url, hash, branch, author_name, author_email, message, timestamp,
			origin_time, edits, labels, fetched_at,
			origin_author_name, origin_author_email, signer_key,
			is_edit_commit
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare commit statement: %w", err)
	}
	defer commitStmt.Close()

	versionStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO core_commits_version (edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare version statement: %w", err)
	}
	defer versionStmt.Close()

	ftsStmt, err := tx.Prepare(`INSERT INTO core_fts (repo_url, hash, branch, content, author) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts statement: %w", err)
	}
	defer ftsStmt.Close()

	type canonicalKey struct{ repoURL, hash, branch string }
	canonicals := make(map[canonicalKey]struct{})

	for _, c := range commits {
		branch := c.Branch
		if branch == "" {
			branch = "main"
		}
		repoURL := protocol.NormalizeURL(c.RepoURL)

		// Extract edits, retracted, origin-time, origin-author, and labels fields from GitMsg header
		var edits *string
		var originTime *string
		var labels *string
		var originAuthorName *string
		var originAuthorEmail *string
		var isRetracted bool
		if msg := protocol.ParseMessage(c.Message); msg != nil {
			if e := msg.Header.Fields["edits"]; e != "" {
				edits = &e
			}
			if ot := msg.Header.Fields["origin-time"]; ot != "" {
				originTime = &ot
			}
			if l := msg.Header.Fields["labels"]; l != "" {
				labels = &l
			}
			if name := msg.Header.Fields["origin-author-name"]; name != "" {
				originAuthorName = &name
			}
			if email := msg.Header.Fields["origin-author-email"]; email != "" {
				originAuthorEmail = &email
			} else if email := msg.Header.Fields["origin-author"]; email != "" {
				originAuthorEmail = &email
			}
			isRetracted = msg.Header.Fields["retracted"] == "true"
		}

		ts := c.Timestamp.UTC().Format(time.RFC3339)
		// Resolved author/email/timestamp (COALESCE origin over git) — used for FTS author column.
		resolvedAuthorName := c.AuthorName
		if originAuthorName != nil {
			resolvedAuthorName = *originAuthorName
		}
		resolvedAuthorEmail := c.AuthorEmail
		if originAuthorEmail != nil {
			resolvedAuthorEmail = *originAuthorEmail
		}

		isEditCommit := 0
		// Determine whether this is an edit commit by parsing edits trailer.
		// Even if the canonical isn't yet in the cache, we know this commit edits something.
		if edits != nil && *edits != "" {
			isEditCommit = 1
		}

		// Always store signer_key as a string (empty for unsigned). NULL is
		// reserved for legacy rows fetched before signer extraction shipped;
		// the backfill scans NULL rows only.
		if _, err := commitStmt.Exec(repoURL, c.Hash, branch, c.AuthorName, c.AuthorEmail, c.Message, ts, originTime, edits, labels, now, originAuthorName, originAuthorEmail, c.SignerKey, isEditCommit); err != nil {
			return fmt.Errorf("insert commit %s: %w", c.Hash, err)
		}

		// Populate version-table link if this is an edit and canonical exists.
		// Edits whose canonical isn't in cache yet are picked up later by
		// ReconcileVersions. Either way, the canonical's resolved-state is
		// applied below via applyEditToCanonical, not inline.
		if edits != nil && *edits != "" {
			parsed := protocol.ParseRef(*edits)
			if parsed.Value != "" {
				canonicalRepoURL := parsed.Repository
				if canonicalRepoURL == "" {
					canonicalRepoURL = repoURL
				}
				canonicalBranch := parsed.Branch
				if canonicalBranch == "" {
					canonicalBranch = branch
				}
				var exists int
				if err := tx.QueryRow(`SELECT 1 FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
					canonicalRepoURL, parsed.Value, canonicalBranch).Scan(&exists); err == nil {
					retracted := 0
					if isRetracted {
						retracted = 1
					}
					if _, err := versionStmt.Exec(repoURL, c.Hash, branch, canonicalRepoURL, parsed.Value, canonicalBranch, retracted); err != nil {
						return fmt.Errorf("insert version record for %s: %w", c.Hash, err)
					}
					canonicals[canonicalKey{canonicalRepoURL, parsed.Value, canonicalBranch}] = struct{}{}
				}
			}
		}

		// Insert into FTS5 for non-edit commits. Edit commits don't get their
		// own FTS row; their content reaches FTS via the canonical's row,
		// which applyEditToCanonical refreshes below.
		if isEditCommit == 0 {
			_, _ = ftsStmt.Exec(repoURL, c.Hash, branch,
				c.Message, resolvedAuthorName+" "+resolvedAuthorEmail)
		}
	}

	// Apply each affected canonical exactly once. If multiple edits in this
	// batch target the same canonical, applyEditToCanonical picks the latest
	// by timestamp, so per-canonical work doesn't scale with edit count.
	for k := range canonicals {
		if err := applyEditToCanonical(tx, k.repoURL, k.hash, k.branch); err != nil {
			return fmt.Errorf("apply edit to canonical %s/%s@%s: %w", k.repoURL, k.hash, k.branch, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// MarkCommitsStale marks cached commits as stale if they no longer exist in the live branch.
// Commits present in liveHashes but marked stale are un-staled (e.g., undo of rebase).
// Returns the count of newly stale commits.
func MarkCommitsStale(repoURL, branch string, liveHashes map[string]bool) (int, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	return QueryLocked(func(db *sql.DB) (int, error) {
		rows, err := db.Query(
			`SELECT hash, stale_since FROM core_commits WHERE repo_url = ? AND branch = ? AND is_virtual = 0`,
			repoURL, branch)
		if err != nil {
			return 0, fmt.Errorf("query commits for stale check: %w", err)
		}
		defer rows.Close()

		var toStale, toUnstale []string
		for rows.Next() {
			var hash string
			var staleSince sql.NullString
			if err := rows.Scan(&hash, &staleSince); err != nil {
				return 0, fmt.Errorf("scan commit for stale check: %w", err)
			}
			if !liveHashes[hash] && !staleSince.Valid {
				toStale = append(toStale, hash)
			} else if liveHashes[hash] && staleSince.Valid {
				toUnstale = append(toUnstale, hash)
			}
		}
		if err := rows.Err(); err != nil {
			return 0, err
		}

		now := time.Now().UTC().Format(time.RFC3339)
		for _, hash := range toStale {
			if _, err := db.Exec(
				`UPDATE core_commits SET stale_since = ? WHERE repo_url = ? AND hash = ? AND branch = ?`,
				now, repoURL, hash, branch); err != nil {
				return 0, fmt.Errorf("mark stale %s: %w", hash, err)
			}
		}
		for _, hash := range toUnstale {
			if _, err := db.Exec(
				`UPDATE core_commits SET stale_since = NULL WHERE repo_url = ? AND hash = ? AND branch = ?`,
				repoURL, hash, branch); err != nil {
				return 0, fmt.Errorf("unstale %s: %w", hash, err)
			}
		}
		return len(toStale), nil
	})
}

// Contributor represents a unique author from cached commits.
type Contributor struct {
	Name  string
	Email string
}

// GetContributors returns distinct authors for a repository, ordered by most recent activity.
//
// SQLite's "bare column" rule: when SELECT pairs a non-aggregated column with
// MAX()/MIN(), the bare column comes from the row that produced the max/min.
// This avoids the self-join the previous implementation needed and runs in
// hundreds of milliseconds (vs ~30s) on a 1.4M-row commits table.
func GetContributors(repoURL string) ([]Contributor, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	return QueryLocked(func(db *sql.DB) ([]Contributor, error) {
		rows, err := db.Query(`
			SELECT author_name, author_email, MAX(timestamp) AS latest
			FROM core_commits
			WHERE author_email != '' AND repo_url = ?
			GROUP BY author_email
			ORDER BY latest DESC`, repoURL)
		if err != nil {
			return nil, fmt.Errorf("query contributors: %w", err)
		}
		defer rows.Close()
		var contributors []Contributor
		for rows.Next() {
			var c Contributor
			var latest string
			if err := rows.Scan(&c.Name, &c.Email, &latest); err != nil {
				return nil, fmt.Errorf("scan contributor: %w", err)
			}
			contributors = append(contributors, c)
		}
		return contributors, rows.Err()
	})
}

// GetAllContributors returns distinct authors across all repositories, ordered by most recent activity.
func GetAllContributors() ([]Contributor, error) {
	return QueryLocked(func(db *sql.DB) ([]Contributor, error) {
		rows, err := db.Query(`
			SELECT author_name, author_email, MAX(timestamp) AS latest
			FROM core_commits
			WHERE author_email != ''
			GROUP BY author_email
			ORDER BY latest DESC`)
		if err != nil {
			return nil, fmt.Errorf("query all contributors: %w", err)
		}
		defer rows.Close()
		var contributors []Contributor
		for rows.Next() {
			var c Contributor
			var latest string
			if err := rows.Scan(&c.Name, &c.Email, &latest); err != nil {
				return nil, fmt.Errorf("scan contributor: %w", err)
			}
			contributors = append(contributors, c)
		}
		return contributors, rows.Err()
	})
}

// hashFilterBatchSize bounds how many hashes are passed in a single SQL IN clause.
// SQLite's SQLITE_LIMIT_VARIABLE_NUMBER defaults to 32766; we stay well under it
// to keep query plans manageable on huge repos (e.g. linux-kernel-scale 1M+ commits).
const hashFilterBatchSize = 5000

// FilterUnfetchedCommitsByRepo returns hashes not yet in the cache for any branch of this repo.
func FilterUnfetchedCommitsByRepo(repoURL string, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	normURL := protocol.NormalizeURL(repoURL)
	fetched := make(map[string]bool, len(hashes))
	for start := 0; start < len(hashes); start += hashFilterBatchSize {
		end := start + hashFilterBatchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[start:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = placeholders[:len(placeholders)-1]
		query := `SELECT hash FROM core_commits WHERE repo_url = ? AND hash IN (` + placeholders + `)`
		args := make([]interface{}, 0, len(batch)+1)
		args = append(args, normURL)
		for _, h := range batch {
			args = append(args, h)
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				rows.Close()
				return nil, err
			}
			fetched[hash] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	var unfetched []string
	for _, hash := range hashes {
		if !fetched[hash] {
			unfetched = append(unfetched, hash)
		}
	}
	return unfetched, nil
}

// MarkCommitsStaleByRepo marks cached commits as stale across all branches of a repo.
func MarkCommitsStaleByRepo(repoURL string, liveHashes map[string]bool) (int, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	return QueryLocked(func(db *sql.DB) (int, error) {
		rows, err := db.Query(
			`SELECT hash, branch, stale_since FROM core_commits WHERE repo_url = ? AND is_virtual = 0`,
			repoURL)
		if err != nil {
			return 0, fmt.Errorf("query commits for stale check: %w", err)
		}
		defer rows.Close()
		type commitKey struct{ hash, branch string }
		var toStale, toUnstale []commitKey
		for rows.Next() {
			var hash, branch string
			var staleSince sql.NullString
			if err := rows.Scan(&hash, &branch, &staleSince); err != nil {
				return 0, fmt.Errorf("scan commit for stale check: %w", err)
			}
			if !liveHashes[hash] && !staleSince.Valid {
				toStale = append(toStale, commitKey{hash, branch})
			} else if liveHashes[hash] && staleSince.Valid {
				toUnstale = append(toUnstale, commitKey{hash, branch})
			}
		}
		if err := rows.Err(); err != nil {
			return 0, err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for _, k := range toStale {
			if _, err := db.Exec(
				`UPDATE core_commits SET stale_since = ? WHERE repo_url = ? AND hash = ? AND branch = ?`,
				now, repoURL, k.hash, k.branch); err != nil {
				return 0, fmt.Errorf("mark stale %s: %w", k.hash, err)
			}
		}
		for _, k := range toUnstale {
			if _, err := db.Exec(
				`UPDATE core_commits SET stale_since = NULL WHERE repo_url = ? AND hash = ? AND branch = ?`,
				repoURL, k.hash, k.branch); err != nil {
				return 0, fmt.Errorf("unstale %s: %w", k.hash, err)
			}
		}
		return len(toStale), nil
	})
}

// ResetRepositoryData deletes all commits and extension items for a repo.
// Used when switching between specific branch and * following mode.
func ResetRepositoryData(repoURL string) error {
	repoURL = protocol.NormalizeURL(repoURL)
	return ExecLocked(func(db *sql.DB) error {
		tables := []string{
			"social_items", "social_interactions",
			"pm_items", "release_items", "review_items",
			"core_commits_version", "core_mentions",
			"core_notification_reads", "core_commits",
		}
		_, _ = db.Exec(`DELETE FROM core_fts WHERE repo_url = ?`, repoURL)
		for _, table := range tables {
			if _, err := db.Exec(`DELETE FROM `+table+` WHERE repo_url = ?`, repoURL); err != nil {
				// Table may not exist if extension not loaded — skip
				continue
			}
		}
		return nil
	})
}

// isHexString returns true if s contains only hex characters.
func isHexString(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}

// EscapeLike escapes SQL LIKE wildcards in user input.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// ExtensionHit identifies which extension table owns a commit hash, with its full primary key.
type ExtensionHit struct {
	Extension string // "review", "pm", "release", "social"
	Type      string // extension-specific type (e.g. "pull-request", "issue", "post")
	RepoURL   string
	Hash      string
	Branch    string
}

// DetectExtension returns which extension table(s) contain a given hash.
// Uses direct index lookups on raw tables — no resolved views.
func DetectExtension(hash string) ([]ExtensionHit, error) {
	if !isHexString(hash) {
		return nil, fmt.Errorf("detect extension: invalid hash")
	}
	return QueryLocked(func(db *sql.DB) ([]ExtensionHit, error) {
		var cond string
		var arg string
		if len(hash) == 12 {
			cond = "hash = ?"
			arg = hash
		} else {
			cond = "hash LIKE ? ESCAPE '\\'"
			arg = EscapeLike(hash) + "%"
		}
		query := `SELECT ext, type, repo_url, hash, branch FROM (
			SELECT 'review' as ext, type, repo_url, hash, branch FROM review_items WHERE ` + cond + `
			UNION ALL
			SELECT 'pm', type, repo_url, hash, branch FROM pm_items WHERE ` + cond + `
			UNION ALL
			SELECT 'release', tag, repo_url, hash, branch FROM release_items WHERE ` + cond + `
			UNION ALL
			SELECT 'social', type, repo_url, hash, branch FROM social_items WHERE ` + cond + `
		)`
		rows, err := db.Query(query, arg, arg, arg, arg)
		if err != nil {
			return nil, fmt.Errorf("detect extension: %w", err)
		}
		defer rows.Close()
		var hits []ExtensionHit
		for rows.Next() {
			var h ExtensionHit
			if rows.Scan(&h.Extension, &h.Type, &h.RepoURL, &h.Hash, &h.Branch) == nil {
				hits = append(hits, h)
			}
		}
		return hits, rows.Err()
	})
}

// GetCommit returns a cached commit by repo URL, hash prefix, and branch.
func GetCommit(repoURL, hashPrefix, branch string) (Commit, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	if !isHexString(hashPrefix) {
		return Commit{}, fmt.Errorf("get commit: invalid hash prefix")
	}
	return QueryLocked(func(db *sql.DB) (Commit, error) {
		var c Commit
		var ts string
		err := db.QueryRow(`SELECT hash, repo_url, branch, author_name, author_email, message, timestamp FROM core_commits WHERE repo_url = ? AND hash LIKE ? AND branch = ? LIMIT 1`,
			repoURL, hashPrefix+"%", branch).Scan(&c.Hash, &c.RepoURL, &c.Branch, &c.AuthorName, &c.AuthorEmail, &c.Message, &ts)
		if err != nil {
			return Commit{}, fmt.Errorf("get commit: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			c.Timestamp = t
		}
		return c, nil
	})
}

// FilterUnfetchedCommits returns hashes that are not yet in the cache.
func FilterUnfetchedCommits(repoURL, branch string, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	db := dbPtr.Load()
	if db == nil {
		return nil, ErrNotOpen
	}
	normURL := protocol.NormalizeURL(repoURL)
	fetched := make(map[string]bool, len(hashes))
	for start := 0; start < len(hashes); start += hashFilterBatchSize {
		end := start + hashFilterBatchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[start:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = placeholders[:len(placeholders)-1]
		query := `SELECT hash FROM core_commits WHERE repo_url = ? AND branch = ? AND hash IN (` + placeholders + `)`
		args := make([]interface{}, 0, len(batch)+2)
		args = append(args, normURL, branch)
		for _, h := range batch {
			args = append(args, h)
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				rows.Close()
				return nil, err
			}
			fetched[hash] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	var unfetched []string
	for _, hash := range hashes {
		if !fetched[hash] {
			unfetched = append(unfetched, hash)
		}
	}
	return unfetched, nil
}
