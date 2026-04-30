// versions.go - Message versioning, edit tracking, and canonical resolution
package cache

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// sqlExecutor is the subset of *sql.DB / *sql.Tx that applyEditToCanonical
// uses, so the same implementation can be invoked from inside an in-flight
// transaction (insertCommitsTxn) or a top-level ExecLocked call.
type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// csvLinkSpec describes a CSV column on an extension table that has a
// normalized linking-table sibling (e.g., pm_items.assignees → pm_assignees).
// applyEditToCanonical rebuilds the linking-table rows for the canonical
// after propagating the column from the edit's row.
type csvLinkSpec struct {
	col       string // column on the extension table (e.g. "assignees")
	linkTable string // linking table name (e.g. "pm_assignees")
	valueCol  string // value column on the linking table (e.g. "email")
}

// editableExtensionTables enumerates the per-extension column lists that
// applyEditToCanonical propagates from an edit's row to the canonical's row.
// Listed once here so the column inventory has a single source of truth.
var editableExtensionTables = []struct {
	table, cols string
	csvLinks    []csvLinkSpec
}{
	{
		table: "review_items",
		cols:  "state, draft, base, base_tip, head, head_tip, depends_on, closes, reviewers",
		csvLinks: []csvLinkSpec{
			{col: "reviewers", linkTable: "review_reviewers", valueCol: "email"},
		},
	},
	{
		table: "pm_items",
		cols:  "state, assignees, due, start_date, end_date, milestone_repo_url, milestone_hash, milestone_branch, sprint_repo_url, sprint_hash, sprint_branch, parent_repo_url, parent_hash, parent_branch, root_repo_url, root_hash, root_branch",
		csvLinks: []csvLinkSpec{
			{col: "assignees", linkTable: "pm_assignees", valueCol: "email"},
		},
	},
	{
		table: "release_items",
		cols:  "tag, version, prerelease, artifacts, artifact_url, checksums, signed_by, sbom",
	},
}

// applyEditToCanonical is the single writer of denormalized resolved state.
// Given a canonical's coordinates, it picks the latest edit from
// core_commits_version + core_commits and propagates that edit's content into:
//
//   - core_commits.has_edits / resolved_message / is_retracted / labels (the
//     canonical's row)
//   - core_commits.is_edit_commit (the edit's row, defensive — insertCommitsTxn
//     already sets this on insert)
//   - mutable extension columns on review_items / pm_items / release_items
//     (canonical's row, copied from the edit's row in a single UPDATE per table)
//   - core_fts (canonical's row, replaced with the edit's content)
//
// No-op when the canonical has no edits in core_commits_version yet.
// Idempotent: repeated calls converge to the same state. All write paths
// (insertCommitsTxn, InsertVersion, ReconcileVersions, SyncEditExtensionFields)
// route through this function — no other code should write the columns above.
func applyEditToCanonical(tx sqlExecutor, canonicalRepoURL, canonicalHash, canonicalBranch string) error {
	var editRepoURL, editHash, editBranch string
	var editMessage, editAuthorName, editAuthorEmail string
	var editLabels sql.NullString
	var editIsRetracted int
	err := tx.QueryRow(`
		SELECT v.edit_repo_url, v.edit_hash, v.edit_branch,
		       c.message, c.labels, v.is_retracted,
		       COALESCE(c.origin_author_name, c.author_name),
		       COALESCE(c.origin_author_email, c.author_email)
		FROM core_commits_version v
		JOIN core_commits c
		  ON v.edit_repo_url = c.repo_url
		 AND v.edit_hash = c.hash
		 AND v.edit_branch = c.branch
		WHERE v.canonical_repo_url = ?
		  AND v.canonical_hash = ?
		  AND v.canonical_branch = ?
		ORDER BY c.timestamp DESC, v.edit_hash DESC
		LIMIT 1`,
		canonicalRepoURL, canonicalHash, canonicalBranch,
	).Scan(&editRepoURL, &editHash, &editBranch, &editMessage, &editLabels, &editIsRetracted,
		&editAuthorName, &editAuthorEmail)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("apply edit: find latest edit for %s/%s@%s: %w", canonicalRepoURL, canonicalHash, canonicalBranch, err)
	}

	var resolvedAuthorName, resolvedAuthorEmail string
	if err := tx.QueryRow(`
		SELECT COALESCE(origin_author_name, author_name),
		       COALESCE(origin_author_email, author_email)
		FROM core_commits
		WHERE repo_url = ? AND hash = ? AND branch = ?`,
		canonicalRepoURL, canonicalHash, canonicalBranch,
	).Scan(&resolvedAuthorName, &resolvedAuthorEmail); err != nil {
		return fmt.Errorf("apply edit: read canonical author: %w", err)
	}
	var editorNameArg, editorEmailArg interface{}
	if !strings.EqualFold(strings.TrimSpace(editAuthorEmail), strings.TrimSpace(resolvedAuthorEmail)) {
		editorNameArg = editAuthorName
		editorEmailArg = editAuthorEmail
	}

	var labelsArg interface{}
	if editLabels.Valid {
		labelsArg = editLabels.String
	}
	if _, err := tx.Exec(`
		UPDATE core_commits
		SET has_edits = 1,
		    resolved_message = ?,
		    resolved_editor_name = ?,
		    resolved_editor_email = ?,
		    is_retracted = ?,
		    labels = COALESCE(?, labels)
		WHERE repo_url = ? AND hash = ? AND branch = ?`,
		editMessage, editorNameArg, editorEmailArg, editIsRetracted, labelsArg,
		canonicalRepoURL, canonicalHash, canonicalBranch,
	); err != nil {
		return fmt.Errorf("apply edit: update canonical: %w", err)
	}

	// Rebuild core_labels for the canonical from its (possibly just-updated)
	// labels column. Read back rather than using editLabels directly because
	// the UPDATE used COALESCE — when the edit didn't set labels we want to
	// keep the canonical's existing rows in sync with whatever's there.
	var canonicalLabels sql.NullString
	if err := tx.QueryRow(`SELECT labels FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
		canonicalRepoURL, canonicalHash, canonicalBranch,
	).Scan(&canonicalLabels); err != nil {
		return fmt.Errorf("apply edit: read canonical labels: %w", err)
	}
	if err := RebuildCSVLinkingTable(tx, "core_labels", "label",
		canonicalRepoURL, canonicalHash, canonicalBranch, canonicalLabels.String); err != nil {
		return fmt.Errorf("apply edit: rebuild core_labels: %w", err)
	}

	if _, err := tx.Exec(`
		UPDATE core_commits SET is_edit_commit = 1
		WHERE repo_url = ? AND hash = ? AND branch = ?`,
		editRepoURL, editHash, editBranch,
	); err != nil {
		return fmt.Errorf("apply edit: mark edit row: %w", err)
	}

	for _, ext := range editableExtensionTables {
		// Skip silently on any error: sql.ErrNoRows means this extension has no
		// row for the edit (so nothing to propagate); "no such table" means the
		// extension's schema isn't registered in this DB (cache-only tests).
		// Either way, the propagation is a no-op for this table.
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM `+ext.table+`
			WHERE repo_url = ? AND hash = ? AND branch = ?`,
			editRepoURL, editHash, editBranch,
		).Scan(&exists); err != nil {
			continue
		}
		// Row-value SET copies all mutable columns in one statement.
		// Pre-checking the edit row above avoids the row-value-with-no-rows
		// gotcha (SQLite would NULL the canonical's columns).
		if _, err := tx.Exec(`UPDATE `+ext.table+` SET (`+ext.cols+`) =
			(SELECT `+ext.cols+` FROM `+ext.table+`
			 WHERE repo_url = ? AND hash = ? AND branch = ?)
			WHERE repo_url = ? AND hash = ? AND branch = ?`,
			editRepoURL, editHash, editBranch,
			canonicalRepoURL, canonicalHash, canonicalBranch,
		); err != nil {
			return fmt.Errorf("apply edit: propagate %s fields: %w", ext.table, err)
		}
		// Rebuild any CSV linking tables that mirror columns we just copied.
		for _, link := range ext.csvLinks {
			var csv sql.NullString
			if err := tx.QueryRow(`SELECT `+link.col+` FROM `+ext.table+`
				WHERE repo_url = ? AND hash = ? AND branch = ?`,
				canonicalRepoURL, canonicalHash, canonicalBranch,
			).Scan(&csv); err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				return fmt.Errorf("apply edit: read canonical %s.%s: %w", ext.table, link.col, err)
			}
			if err := RebuildCSVLinkingTable(tx, link.linkTable, link.valueCol,
				canonicalRepoURL, canonicalHash, canonicalBranch, csv.String); err != nil {
				return fmt.Errorf("apply edit: rebuild %s: %w", link.linkTable, err)
			}
		}
	}

	if _, err := tx.Exec(`DELETE FROM core_fts
		WHERE repo_url = ? AND hash = ? AND branch = ?`,
		canonicalRepoURL, canonicalHash, canonicalBranch,
	); err != nil {
		return fmt.Errorf("apply edit: delete canonical fts: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO core_fts (repo_url, hash, branch, content, author)
		VALUES (?, ?, ?, ?, ?)`,
		canonicalRepoURL, canonicalHash, canonicalBranch,
		editMessage, resolvedAuthorName+" "+resolvedAuthorEmail,
	); err != nil {
		return fmt.Errorf("apply edit: insert canonical fts: %w", err)
	}

	return nil
}

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

// InsertVersion stores an edit relationship between commits and applies the
// edit's state to the canonical via the unified writer.
func InsertVersion(editRepoURL, editHash, editBranch, canonicalRepoURL, canonicalHash, canonicalBranch string, isRetracted bool) error {
	return ExecLocked(func(db *sql.DB) error {
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
		if _, err := db.Exec(`
			INSERT OR REPLACE INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			editRepoURL, editHash, editBranch, canonicalRepoURL, canonicalHash, canonicalBranch, retracted); err != nil {
			return err
		}
		return applyEditToCanonical(db, canonicalRepoURL, canonicalHash, canonicalBranch)
	})
}

// EditKey identifies an edit commit for extension field syncing.
type EditKey struct {
	RepoURL, Hash, Branch string
}

// SyncEditExtensionFields re-applies each edit's content to its canonical via
// the unified writer. Callers (extension fetch hooks) invoke this after they
// insert the edit's extension row, since ProcessVersionFromHeader runs before
// extension rows exist and the canonical's extension columns therefore weren't
// propagated on the first pass.
//
// The function name is preserved for caller compatibility; the work it does
// now also includes resolved_message / is_retracted / FTS, all idempotent.
func SyncEditExtensionFields(edits []EditKey) {
	if len(edits) == 0 {
		return
	}
	_ = ExecLocked(func(db *sql.DB) error {
		type canonicalKey struct{ repoURL, hash, branch string }
		seen := make(map[canonicalKey]struct{}, len(edits))
		for _, e := range edits {
			var canonRepoURL, canonHash, canonBranch string
			err := db.QueryRow(`SELECT canonical_repo_url, canonical_hash, canonical_branch
				FROM core_commits_version
				WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
				e.RepoURL, e.Hash, e.Branch).Scan(&canonRepoURL, &canonHash, &canonBranch)
			if err != nil {
				continue
			}
			k := canonicalKey{canonRepoURL, canonHash, canonBranch}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			_ = applyEditToCanonical(db, canonRepoURL, canonHash, canonBranch)
		}
		return nil
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

	// Phase 2: Write version records and apply each affected canonical exactly
	// once. Multiple edits targeting the same canonical fold into a single
	// applyEditToCanonical call (which already picks the latest by timestamp).
	type canonicalKey struct{ repoURL, hash, branch string }
	created := 0
	err = ExecLocked(func(db *sql.DB) error {
		canonicals := make(map[canonicalKey]struct{}, len(pending))
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
				canonicals[canonicalKey{p.canonicalRepoURL, p.canonicalHash, p.canonicalBranch}] = struct{}{}
			}
		}
		for k := range canonicals {
			if err := applyEditToCanonical(db, k.repoURL, k.hash, k.branch); err != nil {
				return fmt.Errorf("reconcile: %w", err)
			}
		}
		return nil
	})
	return created, err
}
