// items.go - Memo item cache queries and inserts
package memo

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// MemoItem is a memo as stored in / read from the cache.
type MemoItem struct {
	RepoURL     string
	Hash        string
	Branch      string
	Type        string
	Labels      sql.NullString
	Origin      *protocol.Origin
	Content     string
	AuthorName  string
	AuthorEmail string
	Timestamp   time.Time
	EditOf      sql.NullString
	IsRetracted bool
	IsEdited    bool
	IsVirtual   bool
	IsStale     bool
}

// Memo doesn't fit cache.ResolvedSelect: it tracks stale_since and has no
// comments/has_proposed columns, so it composes the shared snippets directly.
const baseSelectFromView = `
	SELECT ` + cache.ResolvedCommonColumns + `,
	       v.type, v.labels,
	       ` + cache.ResolvedFlagColumns + `, v.stale_since
	FROM memo_items_resolved v
`

// MemoQuery configures GetMemos filtering.
type MemoQuery struct {
	RepoURL            string
	Branch             string
	WorkspaceURL       string   // anchor for tier ordering (priority 2 — project)
	PersonalURL        string   // anchor for tier ordering (priority 1 — personal)
	InheritedURLs      []string // anchor for tier ordering (priority 3 — inherited)
	TierRepoURLs       []string // when non-nil, restricts results to these repo_urls
	SessionID          string   // when set, only this session's session-tier rows surface
	IncludeAllSessions bool     // when true, every session-tier row surfaces
	Tier               Tier     // when set, restricts to one tier
	Labels             []string // AND-of-labels filter
	Cursor             string
	Limit              int
	IncludeExpired     bool
	OnlyExpired        bool
}

// InsertMemoItem upserts a memo marker row in the cache.
func InsertMemoItem(item MemoItem) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		t := item.Type
		if t == "" {
			t = "memo"
		}
		_, err := db.Exec(`
			INSERT INTO memo_items (repo_url, hash, branch, type)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET type = excluded.type`,
			item.RepoURL, item.Hash, item.Branch, t,
		)
		return err
	})
}

// GetMemoItem retrieves a single memo item by composite key.
func GetMemoItem(repoURL, hash, branch string) (*MemoItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*MemoItem, error) {
		query := baseSelectFromView + `
			WHERE v.repo_url = ? AND v.hash = ? AND v.branch = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		row := db.QueryRow(query, repoURL, hash, branch)
		return scanResolvedRow(row)
	})
}

// GetMemoItemByRef looks up a memo by ref string (full ref, workspace-relative,
// or bare hash prefix). Tilde-form local URLs (`local:~/...`) are accepted —
// they're expanded to the absolute form before the cache lookup so JSON-exported
// IDs round-trip cleanly.
func GetMemoItemByRef(refStr, defaultRepoURL string) (*MemoItem, error) {
	parsed := protocol.ParseRef(refStr)
	if strings.Contains(refStr, "#") || strings.Contains(refStr, "://") {
		ref := protocol.ResolveRefWithDefaults(refStr, defaultRepoURL, MemoBranch)
		if ref.Hash == "" {
			return nil, sql.ErrNoRows
		}
		return GetMemoItem(ExpandLocalURL(ref.RepoURL), ref.Hash, ref.Branch)
	}
	hash := parsed.Value
	if hash == "" {
		hash = refStr
	}
	return GetMemoItemByHashPrefix(hash)
}

// GetMemoItemByHashPrefix retrieves a memo by hash prefix.
func GetMemoItemByHashPrefix(hashPrefix string) (*MemoItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*MemoItem, error) {
		query := baseSelectFromView + `
			WHERE v.hash LIKE ? AND NOT v.is_edit_commit AND NOT v.is_retracted
			ORDER BY v.timestamp DESC LIMIT 1`
		row := db.QueryRow(query, cache.EscapeLike(hashPrefix)+"%")
		return scanResolvedRow(row)
	})
}

// GetMemoItems queries memo items with the given filters, ordered by tier
// priority then recency.
func GetMemoItems(q MemoQuery) ([]MemoItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]MemoItem, error) {
		var args []interface{}
		var where []string

		if q.RepoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, q.RepoURL)
		}
		if q.Branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, q.Branch)
		}
		if q.Cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, q.Cursor)
		}

		// Tier scope: caller can either pass a list of tier repo URLs or a
		// single Tier. Both narrow which repo_urls surface.
		if len(q.TierRepoURLs) > 0 {
			placeholders := strings.Repeat("?,", len(q.TierRepoURLs))
			placeholders = placeholders[:len(placeholders)-1]
			where = append(where, "v.repo_url IN ("+placeholders+")")
			for _, u := range q.TierRepoURLs {
				args = append(args, u)
			}
		}

		// External-tier scope: exclude every known local/workspace/inherited
		// repo. The remaining rows are by definition external.
		if q.Tier == TierExternal {
			excludes := []string{}
			if q.WorkspaceURL != "" {
				excludes = append(excludes, q.WorkspaceURL)
			}
			if q.PersonalURL != "" {
				excludes = append(excludes, q.PersonalURL)
			}
			excludes = append(excludes, q.InheritedURLs...)
			if len(excludes) > 0 {
				placeholders := strings.Repeat("?,", len(excludes))
				placeholders = placeholders[:len(placeholders)-1]
				where = append(where, "v.repo_url NOT IN ("+placeholders+")")
				for _, u := range excludes {
					args = append(args, u)
				}
			}
			where = append(where, "v.repo_url NOT LIKE ?")
			args = append(args, "local:"+sessionDirPrefix())
		}

		// Session-id filter: by default only the current session's session-tier
		// rows surface; IncludeAllSessions opts in to every session.
		if !q.IncludeAllSessions && q.SessionID != "" {
			currentSessionURL := ""
			if path, err := SessionRepoPath(q.SessionID); err == nil {
				currentSessionURL = LocalRepoURL(path)
			}
			where = append(where, `(
				v.repo_url NOT LIKE ?
				OR v.repo_url = ?
			)`)
			args = append(args, "local:"+sessionDirPrefix(), currentSessionURL)
		}

		// Expiry derived from labels: rows with `expires/<date>` past today
		// are filtered unless explicitly included or solely requested.
		// Comparison is lexical on the YYYY-MM-DD prefix, valid because ISO 8601
		// dates sort chronologically.
		today := time.Now().UTC().Format("2006-01-02")
		expiresExpr := `EXISTS (SELECT 1 FROM core_labels el WHERE
			el.repo_url = v.repo_url AND el.hash = v.hash AND el.branch = v.branch
			AND el.label LIKE 'expires/%'
			AND substr(el.label, 9, 10) < ?)`
		if q.OnlyExpired {
			where = append(where, expiresExpr)
			args = append(args, today)
		} else if !q.IncludeExpired {
			where = append(where, "NOT "+expiresExpr)
			args = append(args, today)
		}

		// Labels: every requested label must be present (AND semantics).
		for _, lbl := range q.Labels {
			lbl = strings.TrimSpace(lbl)
			if lbl == "" {
				continue
			}
			where = append(where,
				"EXISTS (SELECT 1 FROM core_labels cl WHERE cl.repo_url = v.repo_url AND cl.hash = v.hash AND cl.branch = v.branch AND cl.label = ?)")
			args = append(args, lbl)
		}

		where = append(where, "NOT v.is_edit_commit", "NOT v.is_retracted", "v.stale_since IS NULL")

		// Tier-aware ORDER BY anchored on the optional tier URL hints.
		orderClauses := []string{}
		var orderArgs []interface{}
		tierCase := buildTierPriorityCase(q.PersonalURL, q.WorkspaceURL, q.InheritedURLs, &orderArgs)
		if tierCase != "" {
			orderClauses = append(orderClauses, tierCase+" ASC")
		}
		// Within a tier, prefer higher priority labels first, then recency.
		orderClauses = append(orderClauses,
			`CASE
				WHEN ',' || COALESCE(v.labels, '') || ',' LIKE '%,priority/critical,%' THEN 0
				WHEN ',' || COALESCE(v.labels, '') || ',' LIKE '%,priority/high,%' THEN 1
				WHEN ',' || COALESCE(v.labels, '') || ',' LIKE '%,priority/normal,%' THEN 2
				WHEN ',' || COALESCE(v.labels, '') || ',' LIKE '%,priority/low,%' THEN 3
				ELSE 2
			END ASC`,
			"v.timestamp DESC",
		)

		sqlQuery := baseSelectFromView + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + strings.Join(orderClauses, ", ")
		args = append(args, orderArgs...)
		if q.Limit > 0 {
			sqlQuery += " LIMIT ?"
			args = append(args, q.Limit)
		}

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []MemoItem
		for rows.Next() {
			item, err := scanResolvedRow(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// buildTierPriorityCase emits an ORDER BY CASE that ranks rows by tier
// retrieval order: session(0), personal(1), project(2), inherited(3),
// external(4). Sessions are detected by repo_url path layout (under the
// session dir); personal/project use exact URL matches; inherited matches
// the supplied URL set.
func buildTierPriorityCase(personalURL, workspaceURL string, inheritedURLs []string, args *[]interface{}) string {
	parts := []string{"WHEN v.repo_url LIKE ? THEN 0"}
	*args = append(*args, "local:"+sessionDirPrefix())
	if personalURL != "" {
		parts = append(parts, "WHEN v.repo_url = ? THEN 1")
		*args = append(*args, personalURL)
	}
	if workspaceURL != "" {
		parts = append(parts, "WHEN v.repo_url = ? THEN 2")
		*args = append(*args, workspaceURL)
	}
	if len(inheritedURLs) > 0 {
		placeholders := strings.Repeat("?,", len(inheritedURLs))
		placeholders = placeholders[:len(placeholders)-1]
		parts = append(parts, "WHEN v.repo_url IN ("+placeholders+") THEN 3")
		for _, u := range inheritedURLs {
			*args = append(*args, u)
		}
	}
	return "CASE " + strings.Join(parts, " ") + " ELSE 4 END"
}

// sessionDirPrefix returns the LIKE-pattern prefix that matches every
// `local:<sessionDir>/...` repo_url. Trailing slash plus % is appended by
// callers to avoid matching sibling personal repos.
func sessionDirPrefix() string {
	dir, err := SessionDir()
	if err != nil {
		return ""
	}
	return strings.TrimRight(dir, "/") + "/%"
}

// GetMemos returns memos matching the given query as a Result.
func GetMemos(q MemoQuery) Result[[]Memo] {
	items, err := GetMemoItems(q)
	if err != nil {
		return result.Err[[]Memo]("QUERY_FAILED", err.Error())
	}
	memos := make([]Memo, len(items))
	for i, item := range items {
		memos[i] = MemoItemToMemo(item, q.WorkspaceURL, q.InheritedURLs)
	}
	return result.Ok(memos)
}

// GetSingleMemo retrieves a single memo by reference.
func GetSingleMemo(memoRef, workspaceURL string, inheritedURLs []string) Result[Memo] {
	item, err := GetMemoItemByRef(memoRef, workspaceURL)
	if err != nil {
		return result.Err[Memo]("NOT_FOUND", "memo not found: "+memoRef)
	}
	return result.Ok(MemoItemToMemo(*item, workspaceURL, inheritedURLs))
}

// MemoItemToMemo converts a MemoItem into the public Memo shape.
func MemoItemToMemo(item MemoItem, workspaceURL string, inheritedURLs []string) Memo {
	subject, body := protocol.SplitSubjectBody(item.Content)
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)
	var labels []string
	if item.Labels.Valid && item.Labels.String != "" {
		for _, l := range strings.Split(item.Labels.String, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				labels = append(labels, l)
			}
		}
		sort.Strings(labels)
	}
	return Memo{
		ID:          id,
		Repository:  item.RepoURL,
		Branch:      item.Branch,
		Tier:        TierForRepoURL(item.RepoURL, workspaceURL, inheritedURLs),
		Author:      Author{Name: item.AuthorName, Email: item.AuthorEmail},
		Timestamp:   item.Timestamp,
		Subject:     subject,
		Body:        body,
		Labels:      labels,
		IsEdited:    item.IsEdited,
		IsRetracted: item.IsRetracted,
		IsVirtual:   item.IsVirtual,
		IsStale:     item.IsStale,
		Origin:      item.Origin,
	}
}

// scanResolvedRow scans a baseSelectFromView row (single- or multi-row query).
func scanResolvedRow(s cache.RowScanner) (*MemoItem, error) {
	var item MemoItem
	var ts, message, originalMessage, staleSince sql.NullString
	var isVirtual, isRetracted, hasEdits int
	err := s.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type, &item.Labels,
		&item.EditOf, &isVirtual, &isRetracted, &hasEdits, &staleSince,
	)
	if err != nil {
		return nil, err
	}
	populateFromMessage(&item, message, originalMessage, ts)
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	item.IsStale = staleSince.Valid
	return &item, nil
}

func populateFromMessage(item *MemoItem, message, originalMessage, ts sql.NullString) {
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
}
