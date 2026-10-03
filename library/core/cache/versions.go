// versions.go - Message versioning, edit tracking, and canonical resolution
package cache

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// sqlExecutor is the subset of *sql.DB / *sql.Tx that applyEditToCanonical
// uses, so the same implementation can be invoked from inside an in-flight
// transaction (insertCommitsTxn) or a top-level ExecLocked call.
type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
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

// sameRepoEdits joins each same-repo edit of a canonical to its rows, aliased c, and to an optional join; the query takes the canonical's repository and hash.
func sameRepoEdits(join string) string {
	return `FROM core_commits_version v
	JOIN core_commits c ON c.repo_url = v.edit_repo_url AND c.hash = v.edit_hash ` + join + `
	WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.edit_repo_url = v.canonical_repo_url`
}

// latestEditFirst orders sameRepoEdits by the edit's time, then its live row, then the hash.
var latestEditFirst = ` ORDER BY c.timestamp DESC, ` + LiveFirstOrder("c") + `, v.edit_hash DESC LIMIT 1`

// applyEditToCanonical writes the resolved state of a canonical to each row of its hash; each field comes from the latest same-repo edit that carries it, else from the canonical.
func applyEditToCanonical(tx sqlExecutor, canonicalRepoURL, canonicalHash string) error {
	var editRepoURL, editHash, editBranch string
	var editMessage, editAuthorName, editAuthorEmail string
	var editIsRetracted int
	err := tx.QueryRow(`
		SELECT v.edit_repo_url, v.edit_hash, c.branch,
		       c.message, v.is_retracted,
		       COALESCE(c.origin_author_name, c.author_name),
		       COALESCE(c.origin_author_email, c.author_email)
		`+sameRepoEdits("")+latestEditFirst,
		canonicalRepoURL, canonicalHash,
	).Scan(&editRepoURL, &editHash, &editBranch, &editMessage, &editIsRetracted,
		&editAuthorName, &editAuthorEmail)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("apply edit: find latest edit for %s/%s: %w", canonicalRepoURL, canonicalHash, err)
	}

	var resolvedAuthorName, resolvedAuthorEmail, canonicalMessage string
	if err := tx.QueryRow(`
		SELECT COALESCE(c.origin_author_name, c.author_name),
		       COALESCE(c.origin_author_email, c.author_email), c.message
		FROM core_commits c
		WHERE c.repo_url = ? AND c.hash = ?
		ORDER BY `+LiveFirstOrder("c")+`, c.branch LIMIT 1`,
		canonicalRepoURL, canonicalHash,
	).Scan(&resolvedAuthorName, &resolvedAuthorEmail, &canonicalMessage); err != nil {
		return fmt.Errorf("apply edit: read canonical author: %w", err)
	}
	var editorNameArg, editorEmailArg interface{}
	if !strings.EqualFold(strings.TrimSpace(editAuthorEmail), strings.TrimSpace(resolvedAuthorEmail)) {
		editorNameArg = editAuthorName
		editorEmailArg = editAuthorEmail
	}

	var labelsArg interface{}
	var editLabels string
	err = tx.QueryRow(`SELECT c.labels `+sameRepoEdits("")+` AND c.labels IS NOT NULL`+latestEditFirst,
		canonicalRepoURL, canonicalHash).Scan(&editLabels)
	switch {
	case err == nil:
		labelsArg = editLabels
	case err != sql.ErrNoRows:
		return fmt.Errorf("apply edit: find latest labels: %w", err)
	default:
		if msg := protocol.ParseMessage(canonicalMessage); msg != nil && msg.Header.Fields["labels"] != "" {
			labelsArg = msg.Header.Fields["labels"]
		}
	}
	if _, err := tx.Exec(`
		UPDATE core_commits
		SET has_edits = 1,
		    resolved_message = ?,
		    resolved_editor_name = ?,
		    resolved_editor_email = ?,
		    resolved_edit_repo_url = ?,
		    resolved_edit_hash = ?,
		    resolved_edit_branch = ?,
		    is_retracted = ?,
		    labels = ?
		WHERE repo_url = ? AND hash = ?`,
		editMessage, editorNameArg, editorEmailArg,
		editRepoURL, editHash, editBranch,
		editIsRetracted, labelsArg,
		canonicalRepoURL, canonicalHash,
	); err != nil {
		return fmt.Errorf("apply edit: update canonical: %w", err)
	}

	var rowids []int64
	labelsByBranch := map[string]string{}
	res, err := tx.Query(`SELECT rowid, branch, COALESCE(labels, '') FROM core_commits WHERE repo_url = ? AND hash = ?`,
		canonicalRepoURL, canonicalHash)
	if err != nil {
		return fmt.Errorf("apply edit: read canonical rows: %w", err)
	}
	for res.Next() {
		var rowid int64
		var branch, labels string
		if err := res.Scan(&rowid, &branch, &labels); err != nil {
			res.Close()
			return fmt.Errorf("apply edit: scan canonical row: %w", err)
		}
		rowids = append(rowids, rowid)
		labelsByBranch[branch] = labels
	}
	res.Close()
	if err := res.Err(); err != nil {
		return fmt.Errorf("apply edit: read canonical rows: %w", err)
	}
	for branch, labels := range labelsByBranch {
		if err := RebuildCSVLinkingTable(tx, "core_labels", "label",
			canonicalRepoURL, canonicalHash, branch, labels); err != nil {
			return fmt.Errorf("apply edit: rebuild core_labels: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE core_commits SET is_edit_commit = 1 WHERE repo_url = ? AND hash = ?`,
		editRepoURL, editHash,
	); err != nil {
		return fmt.Errorf("apply edit: mark edit row: %w", err)
	}

	for _, ext := range editableExtensionTables {
		if err := propagateExtensionColumns(tx, ext.table, ext.cols, ext.csvLinks, canonicalRepoURL, canonicalHash); err != nil {
			return err
		}
	}

	// core_fts is contentless, so each row of the canonical is refreshed by a delete and an insert.
	for _, rowid := range rowids {
		if _, err := tx.Exec(`DELETE FROM core_fts WHERE rowid = ?`, rowid); err != nil {
			return fmt.Errorf("apply edit: delete canonical fts: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO core_fts(rowid, content, author) VALUES (?, ?, ?)`,
			rowid, editMessage, resolvedAuthorName+" "+resolvedAuthorEmail,
		); err != nil {
			return fmt.Errorf("apply edit: insert canonical fts: %w", err)
		}
	}
	return nil
}

// propagateExtensionColumns copies an extension's mutable columns from the latest same-repo edit that has a row in its table to each row of the canonical, and rebuilds the linking tables of those rows.
func propagateExtensionColumns(tx sqlExecutor, table, cols string, links []csvLinkSpec, canonicalRepoURL, canonicalHash string) error {
	// Any error skips the table: no edit has a row, or the extension's schema is not registered.
	var editRepoURL, editHash, sourceBranch string
	if err := tx.QueryRow(`SELECT x.repo_url, x.hash, x.branch `+
		sameRepoEdits(`JOIN `+table+` x ON x.repo_url = c.repo_url AND x.hash = c.hash AND x.branch = c.branch`)+latestEditFirst,
		canonicalRepoURL, canonicalHash,
	).Scan(&editRepoURL, &editHash, &sourceBranch); err != nil {
		return nil
	}
	// The source row exists, so the row-value SET cannot NULL the canonical's columns.
	if _, err := tx.Exec(`UPDATE `+table+` SET (`+cols+`) =
		(SELECT `+cols+` FROM `+table+`
		 WHERE repo_url = ? AND hash = ? AND branch = ?)
		WHERE repo_url = ? AND hash = ?`,
		editRepoURL, editHash, sourceBranch,
		canonicalRepoURL, canonicalHash,
	); err != nil {
		return fmt.Errorf("apply edit: propagate %s fields: %w", table, err)
	}
	for _, link := range links {
		if err := rebuildExtensionLinks(tx, table, link, canonicalRepoURL, canonicalHash); err != nil {
			return err
		}
	}
	return nil
}

// rebuildExtensionLinks rebuilds one linking table for each extension row of a hash from its CSV column.
func rebuildExtensionLinks(tx sqlExecutor, table string, link csvLinkSpec, repoURL, hash string) error {
	res, err := tx.Query(`SELECT branch, COALESCE(`+link.col+`, '') FROM `+table+` WHERE repo_url = ? AND hash = ?`, repoURL, hash)
	if err != nil {
		return fmt.Errorf("apply edit: read canonical %s.%s: %w", table, link.col, err)
	}
	values := map[string]string{}
	for res.Next() {
		var branch, csv string
		if err := res.Scan(&branch, &csv); err != nil {
			res.Close()
			return fmt.Errorf("apply edit: scan canonical %s.%s: %w", table, link.col, err)
		}
		values[branch] = csv
	}
	res.Close()
	if err := res.Err(); err != nil {
		return fmt.Errorf("apply edit: read canonical %s.%s: %w", table, link.col, err)
	}
	for branch, csv := range values {
		if err := RebuildCSVLinkingTable(tx, link.linkTable, link.valueCol, repoURL, hash, branch, csv); err != nil {
			return fmt.Errorf("apply edit: rebuild %s: %w", link.linkTable, err)
		}
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

	// A same-repo mirror edit that accepts a cross-repo proposal derives the
	// acceptance (proposal coords -> this mirror) into core_edit_acceptances on
	// every fetch path (GITMSG.md §1.5). This clears the proposer's pending marker
	// without a separate published marker and gives accept idempotency.
	if acceptsRef := msg.Header.Fields["accepts"]; acceptsRef != "" {
		if p := protocol.ParseRef(acceptsRef); p.Value != "" {
			_ = RecordAcceptance(protocol.NormalizeURL(p.Repository), p.Value, p.Branch)
		}
	}
}

// InsertVersion stores an edit relationship between commits and applies the
// edit's state to the canonical via the unified writer.
func InsertVersion(editRepoURL, editHash, editBranch, canonicalRepoURL, canonicalHash, canonicalBranch string, isRetracted bool) error {
	return ExecLocked(func(db *sql.DB) error {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE repo_url = ? AND hash = ?`,
			canonicalRepoURL, canonicalHash).Scan(&count); err != nil {
			return fmt.Errorf("version insert: check canonical: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("version insert: canonical commit not found: %s#%s", canonicalRepoURL, canonicalHash)
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
		return applyEditToCanonical(db, canonicalRepoURL, canonicalHash)
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
		seen := make(map[[2]string]bool, len(edits))
		for _, e := range edits {
			var canonical [2]string
			err := db.QueryRow(`SELECT canonical_repo_url, canonical_hash
				FROM core_commits_version
				WHERE edit_repo_url = ? AND edit_hash = ? LIMIT 1`,
				e.RepoURL, e.Hash).Scan(&canonical[0], &canonical[1])
			if err != nil || seen[canonical] {
				continue
			}
			seen[canonical] = true
			_ = applyEditToCanonical(db, canonical[0], canonical[1])
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
			  AND v.edit_repo_url = v.canonical_repo_url
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
			  AND v.edit_repo_url = v.canonical_repo_url
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

	// Phase 2: Write version records and apply each affected canonical once.
	// Multiple edits targeting the same canonical fold into a single
	// applyEditToCanonical call (which already picks the latest by timestamp).
	created := 0
	err = ExecLocked(func(db *sql.DB) error {
		canonicals := make(map[[2]string]bool, len(pending))
		for _, p := range pending {
			var exists int
			if err := db.QueryRow(`SELECT 1 FROM core_commits WHERE repo_url = ? AND hash = ? LIMIT 1`,
				p.canonicalRepoURL, p.canonicalHash).Scan(&exists); err != nil {
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
				canonicals[[2]string{p.canonicalRepoURL, p.canonicalHash}] = true
			}
		}
		for k := range canonicals {
			if err := applyEditToCanonical(db, k[0], k[1]); err != nil {
				return fmt.Errorf("reconcile: %w", err)
			}
		}
		return nil
	})
	return created, err
}
