// resolved.go - Shared SELECT/scan scaffolding for extension *_items_resolved views
package cache

import (
	"database/sql"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// ResolvedCommonColumns is the commit-level column prefix shared by every
// extension's resolved-view SELECT. Expects the view aliased as v.
const ResolvedCommonColumns = `v.repo_url, v.hash, v.branch,
       v.author_name, v.author_email, v.resolved_message, v.original_message, v.timestamp`

// ResolvedFlagColumns are the resolved-state flag columns shared by every
// extension's resolved-view SELECT. Expects the view aliased as v.
const ResolvedFlagColumns = `v.edits, v.is_virtual, v.is_retracted, v.has_edits`

// HasProposedColumn returns the expression (aliased has_proposed) computing
// whether an item has a pending cross-repo edit proposal: an edit from another
// repo with neither an acceptance nor a decline recorded. Single source of
// truth for the proposed-edit marker semantics. itemAlias is the table alias
// carrying repo_url/hash/branch.
func HasProposedColumn(itemAlias string) string {
	return `EXISTS(SELECT 1 FROM core_commits_version cve
              WHERE cve.canonical_repo_url = ` + itemAlias + `.repo_url AND cve.canonical_hash = ` + itemAlias + `.hash
                AND cve.canonical_branch = ` + itemAlias + `.branch
                AND cve.edit_repo_url != cve.canonical_repo_url
                AND NOT EXISTS (SELECT 1 FROM core_edit_acceptances d WHERE d.edit_repo_url = cve.edit_repo_url AND d.edit_hash = cve.edit_hash AND d.edit_branch = cve.edit_branch)
                AND NOT EXISTS (SELECT 1 FROM core_edit_declines dd WHERE dd.edit_repo_url = cve.edit_repo_url AND dd.edit_hash = cve.edit_hash AND dd.edit_branch = cve.edit_branch)) AS has_proposed`
}

// ResolvedSelect builds the standard resolved-view SELECT used by extensions:
// common commit columns, the extension's own columns, flag columns, comments,
// and the has_proposed marker, FROM the given view aliased as v. Rows produced
// by this query are scanned with ScanResolved.
func ResolvedSelect(view, extColumns string) string {
	return `
	SELECT ` + ResolvedCommonColumns + `,
	       ` + extColumns + `,
	       ` + ResolvedFlagColumns + `,
	       v.comments,
	       ` + HasProposedColumn("v") + `
	FROM ` + view + ` v
`
}

// RowScanner abstracts *sql.Row and *sql.Rows so one scan function serves
// single-row and multi-row queries.
type RowScanner interface {
	Scan(dest ...any) error
}

// ResolvedMeta carries the commit-level fields of a row produced by
// ResolvedSelect. Message and OriginalMessage hold the raw values for
// extensions that parse them differently; Content, Origin and Timestamp hold
// the standard interpretation.
type ResolvedMeta struct {
	RepoURL         string
	Hash            string
	Branch          string
	AuthorName      string
	AuthorEmail     string
	Message         sql.NullString // raw resolved_message
	OriginalMessage sql.NullString // raw original_message
	Content         string         // clean content extracted from Message
	Origin          *protocol.Origin
	Timestamp       time.Time
	EditOf          sql.NullString
	IsVirtual       bool
	IsRetracted     bool
	IsEdited        bool
	HasProposed     bool
	Comments        int
}

// ScanResolved scans a row produced by ResolvedSelect: the shared commit
// columns wrap the extension's own columns, which land in extDest in order.
func ScanResolved(s RowScanner, extDest ...any) (*ResolvedMeta, error) {
	var m ResolvedMeta
	var ts sql.NullString
	var isVirtual, isRetracted, hasEdits, hasProposed int
	dest := make([]any, 0, 14+len(extDest))
	dest = append(dest, &m.RepoURL, &m.Hash, &m.Branch,
		&m.AuthorName, &m.AuthorEmail, &m.Message, &m.OriginalMessage, &ts)
	dest = append(dest, extDest...)
	dest = append(dest, &m.EditOf, &isVirtual, &isRetracted, &hasEdits, &m.Comments, &hasProposed)
	if err := s.Scan(dest...); err != nil {
		return nil, err
	}
	if m.Message.Valid {
		m.Content = protocol.ExtractCleanContent(m.Message.String)
	}
	if m.OriginalMessage.Valid {
		if msg := protocol.ParseMessage(m.OriginalMessage.String); msg != nil {
			m.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		m.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	m.IsVirtual = isVirtual == 1
	m.IsRetracted = isRetracted == 1
	m.IsEdited = hasEdits == 1
	m.HasProposed = hasProposed == 1
	return &m, nil
}
