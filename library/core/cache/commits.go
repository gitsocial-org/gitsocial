// commits.go - Commit storage, retrieval, and synchronization with cache
package cache

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
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
	SignerKey   *string
}

// VirtualCommit is the snapshot payload for a placeholder core_commits row
// created from a GitMsg-Ref trailer on a GitMsg-typed message (see GITMSG.md
// §1.3). All fields come from the snapshot itself.
type VirtualCommit struct {
	RepoURL     string
	Hash        string
	Branch      string
	AuthorName  string
	AuthorEmail string
	Message     string
	Timestamp   time.Time
}

// UpsertVirtualCommit inserts a placeholder commit under the caller's write lock when the repository has no row of its hash, and reports whether it did.
func UpsertVirtualCommit(db *sql.DB, vc VirtualCommit) (bool, error) {
	if vc.RepoURL == "" || vc.Hash == "" || vc.Branch == "" {
		return false, fmt.Errorf("upsert virtual commit: repo/hash/branch required")
	}
	res, err := db.Exec(`
		INSERT INTO core_commits
		(repo_url, hash, branch, author_name, author_email, message, timestamp, is_virtual)
		SELECT ?, ?, ?, ?, ?, ?, ?, 1
		WHERE NOT EXISTS (SELECT 1 FROM core_commits WHERE repo_url = ? AND hash = ?)`,
		vc.RepoURL,
		vc.Hash,
		vc.Branch,
		vc.AuthorName,
		vc.AuthorEmail,
		vc.Message,
		vc.Timestamp.Format(time.RFC3339),
		vc.RepoURL,
		vc.Hash,
	)
	if err != nil {
		return false, fmt.Errorf("upsert virtual commit: %w", err)
	}
	inserted, _ := res.RowsAffected() // a driver without the count reads as no insert
	return inserted == 1, nil
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
// the commit's own core_commits row, which takes over a virtual row of the
// same key, its FTS row (when not an edit), and the edit→canonical link in
// core_commits_version (when the canonical is already in the cache). All
// denormalized resolved-state on canonical rows (resolved_message, has_edits,
// is_retracted, labels, FTS, mutable extension columns) is written by
// applyEditToCanonical, called once per affected canonical at the end of the
// loop, which includes each new row of a known canonical: see versions.go.
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
	// A fetched commit takes over its virtual row; a real row is left as it is, so the statement returns no rowid for it.
	commitStmt, err := tx.Prepare(`
		INSERT INTO core_commits (
			repo_url, hash, branch, author_name, author_email, message, timestamp,
			origin_time, edits, labels, fetched_at,
			origin_author_name, origin_author_email, signer_key,
			is_edit_commit
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
			author_name = excluded.author_name, author_email = excluded.author_email,
			message = excluded.message, timestamp = excluded.timestamp,
			origin_time = excluded.origin_time, edits = excluded.edits, labels = excluded.labels,
			fetched_at = excluded.fetched_at,
			origin_author_name = excluded.origin_author_name, origin_author_email = excluded.origin_author_email,
			signer_key = excluded.signer_key, is_edit_commit = excluded.is_edit_commit, is_virtual = 0
		WHERE core_commits.is_virtual = 1
		RETURNING rowid`)
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

	ftsStmt, err := tx.Prepare(`INSERT INTO core_fts(rowid, content, author) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts statement: %w", err)
	}
	defer ftsStmt.Close()

	canonicals := make(map[[2]string]bool)
	var inserted [][2]string

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

		// signer_key: an empty string is confirmed unsigned; NULL means the git lookup failed at insert, and the backfill retries it.
		var rowid int64
		err := commitStmt.QueryRow(repoURL, c.Hash, branch, c.AuthorName, c.AuthorEmail, c.Message, ts, originTime, edits, labels, now, originAuthorName, originAuthorEmail, c.SignerKey, isEditCommit).Scan(&rowid)
		written := err == nil
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("insert commit %s: %w", c.Hash, err)
		}

		// Populate normalized core_labels rows from the comma-string. For
		// canonicals this is the authored value; for edits the canonical's
		// row is later refreshed by applyEditToCanonical with the edit's
		// labels. Empty/nil labels means we DELETE existing rows and INSERT
		// none; for a row that was already there the rebuild is a no-op write.
		labelsStr := ""
		if labels != nil {
			labelsStr = *labels
		}
		if err := RebuildCSVLinkingTable(tx, "core_labels", "label", repoURL, c.Hash, branch, labelsStr); err != nil {
			return fmt.Errorf("rebuild core_labels for %s: %w", c.Hash, err)
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
				if err := tx.QueryRow(`SELECT 1 FROM core_commits WHERE repo_url = ? AND hash = ? LIMIT 1`,
					canonicalRepoURL, parsed.Value).Scan(&exists); err == nil {
					retracted := 0
					if isRetracted {
						retracted = 1
					}
					if _, err := versionStmt.Exec(repoURL, c.Hash, branch, canonicalRepoURL, parsed.Value, canonicalBranch, retracted); err != nil {
						return fmt.Errorf("insert version record for %s: %w", c.Hash, err)
					}
					canonicals[[2]string{canonicalRepoURL, parsed.Value}] = true
				}
			}
		}

		if written {
			inserted = append(inserted, [2]string{repoURL, c.Hash})
		}

		// Insert into FTS5 for non-edit commits. Edit commits don't get their
		// own FTS row; their content reaches FTS via the canonical's row,
		// which applyEditToCanonical refreshes below. A real row that was
		// already there has its FTS row; a virtual row never had one.
		if isEditCommit == 0 && written {
			_, _ = ftsStmt.Exec(rowid, c.Message, resolvedAuthorName+" "+resolvedAuthorEmail)
		}
	}

	if err := addKnownCanonicals(tx, inserted, canonicals); err != nil {
		return err
	}
	// Apply each affected canonical once. If multiple edits in this
	// batch target the same canonical, applyEditToCanonical picks the latest
	// by timestamp, so per-canonical work doesn't scale with edit count.
	for k := range canonicals {
		if err := applyEditToCanonical(tx, k[0], k[1]); err != nil {
			return fmt.Errorf("apply edit to canonical %s/%s: %w", k[0], k[1], err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// addKnownCanonicals adds each inserted pair that already has a version row to the canonicals, in one query on the canonical index.
func addKnownCanonicals(tx *sql.Tx, inserted [][2]string, canonicals map[[2]string]bool) error {
	if len(inserted) == 0 {
		return nil
	}
	args := make([]interface{}, 0, 2*len(inserted))
	for _, pair := range inserted {
		args = append(args, pair[0], pair[1])
	}
	rows, err := tx.Query(`SELECT DISTINCT canonical_repo_url, canonical_hash FROM core_commits_version
		WHERE (canonical_repo_url, canonical_hash) IN (VALUES `+strings.TrimSuffix(strings.Repeat("(?, ?),", len(inserted)), ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("find known canonicals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return fmt.Errorf("scan known canonical: %w", err)
		}
		canonicals[pair] = true
	}
	return rows.Err()
}

// CountCommitsByBranch returns the number of non-virtual cached commits per
// branch, keyed by git refname (e.g. "gitmsg/social", "main"). A before/after
// snapshot across a fetch yields the per-extension count of what was fetched.
func CountCommitsByBranch() (map[string]int, error) {
	return QueryLocked(func(db *sql.DB) (map[string]int, error) {
		rows, err := db.Query(`SELECT branch, COUNT(*) FROM core_commits WHERE is_virtual = 0 GROUP BY branch`)
		if err != nil {
			return nil, fmt.Errorf("count commits by branch: %w", err)
		}
		defer rows.Close()
		out := make(map[string]int)
		for rows.Next() {
			var branch string
			var n int
			if err := rows.Scan(&branch, &n); err != nil {
				return nil, fmt.Errorf("scan branch count: %w", err)
			}
			out[branch] = n
		}
		return out, rows.Err()
	})
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

// FilterUnfetchedCommitsByRepo returns the hashes with no fetched row under any branch of the repository; a virtual row does not count.
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
		query := `SELECT hash FROM core_commits WHERE repo_url = ? AND is_virtual = 0 AND hash IN (` + placeholders + `)`
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

// MarkCommitsStaleByHome marks each row stale whose branch is not the home of its commit, and live again when it is; a read marker follows a commit to its new home, and the rows of the settled branches are not read.
func MarkCommitsStaleByHome(repoURL string, homes map[string]string, settled []string) (int, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	return QueryLocked(func(db *sql.DB) (int, error) {
		branches, err := unsettledBranches(db, repoURL, settled)
		if err != nil || len(branches) == 0 {
			return 0, err
		}
		// branch IN (...) seeks idx_core_commits_repo_branch; NOT IN read every row of the repository.
		args := []interface{}{repoURL}
		for _, branch := range branches {
			args = append(args, branch)
		}
		rows, err := db.Query(`SELECT hash, branch, stale_since FROM core_commits WHERE repo_url = ? AND is_virtual = 0
			AND branch IN (`+strings.TrimSuffix(strings.Repeat("?,", len(branches)), ",")+`)`, args...)
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
			live := homes[hash] == branch
			if !live && !staleSince.Valid {
				toStale = append(toStale, commitKey{hash, branch})
			} else if live && staleSince.Valid {
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
			home := homes[k.hash]
			if home == "" {
				continue
			}
			if _, err := db.Exec(
				`INSERT OR IGNORE INTO core_notification_reads (repo_url, hash, branch, read_at)
				 SELECT repo_url, hash, ?, read_at FROM core_notification_reads WHERE repo_url = ? AND hash = ? AND branch = ?`,
				home, repoURL, k.hash, k.branch); err != nil {
				return 0, fmt.Errorf("move read marker %s: %w", k.hash, err)
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

// unsettledBranches returns the branches the repository has rows under, less the settled ones, from a skip-ahead scan of the branch index.
func unsettledBranches(db *sql.DB, repoURL string, settled []string) ([]string, error) {
	skip := make(map[string]bool, len(settled))
	for _, branch := range settled {
		skip[branch] = true
	}
	rows, err := db.Query(`SELECT DISTINCT branch FROM core_commits WHERE repo_url = ?`, repoURL)
	if err != nil {
		return nil, fmt.Errorf("query branches for stale check: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var branch string
		if err := rows.Scan(&branch); err != nil {
			return nil, fmt.Errorf("scan branch for stale check: %w", err)
		}
		if !skip[branch] {
			out = append(out, branch)
		}
	}
	return out, rows.Err()
}

// ResetRepositoryData deletes the commits and extension rows of a repository, when its follow mode changes or a memo session is collected; the repository itself and its list memberships stay.
func ResetRepositoryData(repoURL string) error {
	repoURL = protocol.NormalizeURL(repoURL)
	return ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := deleteRepoRows(tx, repoURL, repoRowDeletes); err != nil {
			return err
		}
		return tx.Commit()
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
	Extension string // "review", "pm", "release", "social", "memo"
	Type      string // extension-specific type (e.g. "pull-request", "issue", "post")
	RepoURL   string
	Hash      string
	Branch    string
}

// HashPrefixMatch returns a WHERE term on a hash column that seeks an index, and its arguments: equality for a 12-character hash, else a range over the lowercase hex prefix; an empty prefix matches no hash.
func HashPrefixMatch(column, prefix string) (string, []interface{}) {
	prefix = strings.ToLower(prefix)
	if len(prefix) == 12 || prefix == "" {
		return column + " = ?", []interface{}{prefix}
	}
	// "g" sorts after every hex digit, so the range holds each hash that starts with the prefix.
	return column + " >= ? AND " + column + " < ?", []interface{}{prefix, prefix + "g"}
}

// DetectExtension returns the extension rows of a hash from the raw tables, the live and fetched row first.
func DetectExtension(hash string) ([]ExtensionHit, error) {
	if !isHexString(hash) {
		return nil, fmt.Errorf("detect extension: invalid hash")
	}
	return QueryLocked(func(db *sql.DB) ([]ExtensionHit, error) {
		cond, condArgs := HashPrefixMatch("hash", hash)
		query := `SELECT h.ext, h.type, h.repo_url, h.hash, h.branch FROM (
			SELECT 0 as rank, 'review' as ext, type, repo_url, hash, branch FROM review_items WHERE ` + cond + `
			UNION ALL
			SELECT 1, 'pm', type, repo_url, hash, branch FROM pm_items WHERE ` + cond + `
			UNION ALL
			SELECT 2, 'release', tag, repo_url, hash, branch FROM release_items WHERE ` + cond + `
			UNION ALL
			SELECT 3, 'social', type, repo_url, hash, branch FROM social_items WHERE ` + cond + `
			UNION ALL
			SELECT 4, 'memo', type, repo_url, hash, branch FROM memo_items WHERE ` + cond + `
		) h
		LEFT JOIN core_commits c ON c.repo_url = h.repo_url AND c.hash = h.hash AND c.branch = h.branch
		ORDER BY c.repo_url IS NULL, ` + LiveFirstOrder("c") + `, h.rank, h.repo_url, h.branch`
		args := make([]interface{}, 0, 5*len(condArgs))
		for range 5 {
			args = append(args, condArgs...)
		}
		rows, err := db.Query(query, args...)
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
		cond, condArgs := HashPrefixMatch("hash", hashPrefix)
		args := append(append([]interface{}{repoURL}, condArgs...), branch)
		err := db.QueryRow(`SELECT hash, repo_url, branch, author_name, author_email, message, timestamp FROM core_commits WHERE repo_url = ? AND `+cond+` AND branch = ? LIMIT 1`,
			args...).Scan(&c.Hash, &c.RepoURL, &c.Branch, &c.AuthorName, &c.AuthorEmail, &c.Message, &ts)
		if err != nil {
			return Commit{}, fmt.Errorf("get commit: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			c.Timestamp = t
		}
		return c, nil
	})
}

// GetCommitOnAnyBranch returns a cached commit by repo URL and hash prefix, the live row before a stale one.
func GetCommitOnAnyBranch(repoURL, hashPrefix string) (Commit, error) {
	repoURL = protocol.NormalizeURL(repoURL)
	if !isHexString(hashPrefix) {
		return Commit{}, fmt.Errorf("get commit: invalid hash prefix")
	}
	return QueryLocked(func(db *sql.DB) (Commit, error) {
		var c Commit
		var ts string
		cond, condArgs := HashPrefixMatch("c.hash", hashPrefix)
		args := append([]interface{}{repoURL}, condArgs...)
		err := db.QueryRow(`SELECT c.hash, c.repo_url, c.branch, c.author_name, c.author_email, c.message, c.timestamp FROM core_commits c WHERE c.repo_url = ? AND `+cond+` ORDER BY `+LiveFirstOrder("c")+`, c.branch LIMIT 1`,
			args...).Scan(&c.Hash, &c.RepoURL, &c.Branch, &c.AuthorName, &c.AuthorEmail, &c.Message, &ts)
		if err != nil {
			return Commit{}, fmt.Errorf("get commit: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			c.Timestamp = t
		}
		return c, nil
	})
}

// FilterUnfetchedCommits returns the hashes with no fetched row under the branch; a virtual row does not count, so the fetch takes it over.
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
		query := `SELECT hash FROM core_commits WHERE repo_url = ? AND branch = ? AND is_virtual = 0 AND hash IN (` + placeholders + `)`
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
