// items.go - Social item queries and cache operations
package social

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type SocialItem struct {
	RepoURL         string
	Hash            string
	Type            string
	OriginalRepoURL sql.NullString
	OriginalHash    sql.NullString
	OriginalBranch  sql.NullString
	ReplyToRepoURL  sql.NullString
	ReplyToHash     sql.NullString
	ReplyToBranch   sql.NullString
	// Derived from core_commits via JOIN
	Origin      *protocol.Origin
	Branch      string
	Content     string
	AuthorName  string
	AuthorEmail string
	EditorName  string
	EditorEmail string
	Timestamp   time.Time
	EditOf      sql.NullString
	IsRetracted bool
	IsEdited    bool
	IsVirtual   bool
	IsStale     bool
	// Derived from social_interactions
	Comments int
	Reposts  int
	Quotes   int
	// Derived from social_followers
	FollowsWorkspace bool
	// Parsed from GitMsg-Ref for cross-extension navigation
	OriginalExtension string
	OriginalType      string
	// Parsed from GitMsg header (the item's own ext/type/state, not referenced item)
	HeaderExt   string
	HeaderType  string
	HeaderState string
}

// baseSelectFromView is the standard SELECT for querying social_items_resolved.
// All social queries use this as the base, with additional WHERE/ORDER/LIMIT clauses.
const baseSelectFromView = `
	SELECT v.repo_url, v.hash, v.branch,
	       v.author_name, v.author_email, v.resolved_message, v.original_message, v.timestamp,
	       v.type,
	       v.original_repo_url, v.original_hash, v.original_branch,
	       v.reply_to_repo_url, v.reply_to_hash, v.reply_to_branch,
	       v.edits, v.comments, v.reposts, v.quotes,
	       v.is_virtual, v.is_retracted, v.has_edits,
	       v.stale_since,
	       v.editor_name, v.editor_email,
	       (sf.repo_url IS NOT NULL) as follows_workspace
	FROM social_items_resolved v
	LEFT JOIN social_followers sf ON v.repo_url = sf.repo_url AND sf.workspace_url = ?
`

// baseDirectSelect bypasses the social_items_resolved view, joining
// core_commits directly. Resolved-state fields (resolved_message,
// is_retracted, has_edits) are read from inline columns on core_commits, kept
// in sync by applyEditToCanonical on every edit insert + reconcile pass.
// Column order matches scanResolvedRows/scanResolvedRow.
// Caller binds: workspace_url (for sf join), then WHERE params.
const baseDirectSelect = `
	SELECT c.repo_url, c.hash, c.branch,
	       COALESCE(c.origin_author_name, c.author_name),
	       COALESCE(c.origin_author_email, c.author_email),
	       COALESCE(c.resolved_message, c.message),
	       c.message,
	       COALESCE(c.origin_time, c.timestamp),
	       COALESCE(s.type, 'post'),
	       s.original_repo_url, s.original_hash, s.original_branch,
	       s.reply_to_repo_url, s.reply_to_hash, s.reply_to_branch,
	       c.edits,
	       COALESCE(i.comments, 0), COALESCE(i.reposts, 0), COALESCE(i.quotes, 0),
	       c.is_virtual,
	       c.is_retracted,
	       c.has_edits,
	       c.stale_since,
	       c.resolved_editor_name, c.resolved_editor_email,
	       (sf.repo_url IS NOT NULL)
	FROM core_commits c
	LEFT JOIN social_items s ON c.repo_url = s.repo_url AND c.hash = s.hash AND c.branch = s.branch
	LEFT JOIN social_interactions i ON c.repo_url = i.repo_url AND c.hash = i.hash AND c.branch = i.branch
	LEFT JOIN social_followers sf ON c.repo_url = sf.repo_url AND sf.workspace_url = ?
`

// GetCachedCommit retrieves a commit from the cache and parses it as a social item.
func GetCachedCommit(repoURL, hash, branch string) (*SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*SocialItem, error) {
		var item SocialItem
		var ts string
		err := db.QueryRow(`
			SELECT repo_url, hash, branch, author_name, author_email, message, timestamp
			FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch).Scan(
			&item.RepoURL, &item.Hash, &item.Branch, &item.AuthorName, &item.AuthorEmail,
			&item.Content, &ts,
		)
		if err != nil {
			return nil, err
		}
		item.Type = "post"
		item.Timestamp, _ = time.Parse(time.RFC3339, ts) // RFC3339 from DB; zero value ok
		if msg := protocol.ParseMessage(item.Content); msg != nil {
			item.Type = string(GetPostType(msg))
			item.Content = msg.Content
			item.HeaderExt = msg.Header.Ext
			item.HeaderType = msg.Header.Fields["type"]
			item.HeaderState = msg.Header.Fields["state"]
		}
		return &item, nil
	})
}

// CreateVirtualSocialItem creates a placeholder item from a protocol reference.
func CreateVirtualSocialItem(ref protocol.Ref, parentRepoURL, parentBranch string) *SocialItem {
	if ref.Ext != "social" || ref.Metadata == "" {
		return nil
	}

	postType := "post"
	if t, ok := ref.Fields["type"]; ok {
		postType = t
	}

	lines := strings.Split(ref.Metadata, "\n")
	var contentLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, ">") {
			contentLines = append(contentLines, strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "))
		}
	}
	content := strings.Join(contentLines, "\n")
	if content == "" {
		return nil
	}

	// Parse the ref first to get its branch (if any)
	parsed := protocol.ParseRef(ref.Ref)
	if parsed.Type != protocol.RefTypeCommit {
		return nil
	}

	// Use parsed branch if available, otherwise use parent branch
	branch := parsed.Branch
	if branch == "" {
		branch = parentBranch
	}

	// Resolve repo URL
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = parentRepoURL
	}

	timestamp, err := time.Parse(time.RFC3339, ref.Time)
	if err != nil {
		return nil
	}

	return &SocialItem{
		RepoURL:     repoURL,
		Hash:        parsed.Value,
		Branch:      branch,
		Type:        postType,
		Content:     content,
		AuthorName:  ref.Author,
		AuthorEmail: ref.Email,
		Timestamp:   timestamp,
		IsVirtual:   true,
	}
}

// InsertSocialItems batch-inserts multiple non-virtual social items in a single transaction.
// Used by SyncWorkspaceToCache for reduced lock contention. Skips interaction count
// updates since workspace sync processes the full history (counts are rebuilt on fetch).
func InsertSocialItems(items []SocialItem) error {
	if len(items) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		stmt, err := tx.Prepare(`
			INSERT INTO social_items
			(repo_url, hash, branch, type, original_repo_url, original_hash, original_branch, reply_to_repo_url, reply_to_hash, reply_to_branch)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				original_repo_url = COALESCE(NULLIF(original_repo_url, ''), excluded.original_repo_url),
				original_hash = COALESCE(NULLIF(original_hash, ''), excluded.original_hash),
				original_branch = COALESCE(NULLIF(original_branch, ''), excluded.original_branch),
				reply_to_repo_url = COALESCE(NULLIF(reply_to_repo_url, ''), excluded.reply_to_repo_url),
				reply_to_hash = COALESCE(NULLIF(reply_to_hash, ''), excluded.reply_to_hash),
				reply_to_branch = COALESCE(NULLIF(reply_to_branch, ''), excluded.reply_to_branch)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, item := range items {
			_, err := stmt.Exec(
				item.RepoURL, item.Hash, item.Branch, item.Type,
				item.OriginalRepoURL, item.OriginalHash, item.OriginalBranch,
				item.ReplyToRepoURL, item.ReplyToHash, item.ReplyToBranch,
			)
			if err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// InsertSocialItem inserts or updates a social item in the cache database.
func InsertSocialItem(item SocialItem) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		if item.IsVirtual {
			// Insert virtual row into core_commits first
			_, err := db.Exec(`
				INSERT INTO core_commits
				(repo_url, hash, branch, author_name, author_email, message, timestamp, is_virtual)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO NOTHING`,
				item.RepoURL,
				item.Hash,
				item.Branch,
				item.AuthorName,
				item.AuthorEmail,
				item.Content,
				item.Timestamp.Format(time.RFC3339),
			)
			if err != nil {
				return err
			}
			// Insert into social_items
			_, err = db.Exec(`
				INSERT INTO social_items
				(repo_url, hash, branch, type, original_repo_url, original_hash, original_branch, reply_to_repo_url, reply_to_hash, reply_to_branch)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(repo_url, hash, branch) DO NOTHING`,
				item.RepoURL,
				item.Hash,
				item.Branch,
				item.Type,
				item.OriginalRepoURL,
				item.OriginalHash,
				item.OriginalBranch,
				item.ReplyToRepoURL,
				item.ReplyToHash,
				item.ReplyToBranch,
			)
			// Virtual items don't update interaction counts
			return err
		}

		// Check if item already exists in social_items (for interaction count updates)
		var existsInSocialItems int
		_ = db.QueryRow(`SELECT 1 FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			item.RepoURL, item.Hash, item.Branch).Scan(&existsInSocialItems)
		isNew := existsInSocialItems != 1

		// If upgrading from virtual, update core_commits
		var wasVirtual int
		if err := db.QueryRow(`SELECT is_virtual FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
			item.RepoURL, item.Hash, item.Branch).Scan(&wasVirtual); err == nil && wasVirtual == 1 {
			if _, err := db.Exec(`
				UPDATE core_commits SET is_virtual = 0, fetched_at = datetime('now')
				WHERE repo_url = ? AND hash = ? AND branch = ?`,
				item.RepoURL, item.Hash, item.Branch); err != nil {
				return err
			}
		}

		// Insert or update social_items
		_, err := db.Exec(`
			INSERT INTO social_items
			(repo_url, hash, branch, type, original_repo_url, original_hash, original_branch, reply_to_repo_url, reply_to_hash, reply_to_branch)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_url, hash, branch) DO UPDATE SET
				type = excluded.type,
				original_repo_url = COALESCE(NULLIF(original_repo_url, ''), excluded.original_repo_url),
				original_hash = COALESCE(NULLIF(original_hash, ''), excluded.original_hash),
				original_branch = COALESCE(NULLIF(original_branch, ''), excluded.original_branch),
				reply_to_repo_url = COALESCE(NULLIF(reply_to_repo_url, ''), excluded.reply_to_repo_url),
				reply_to_hash = COALESCE(NULLIF(reply_to_hash, ''), excluded.reply_to_hash),
				reply_to_branch = COALESCE(NULLIF(reply_to_branch, ''), excluded.reply_to_branch)`,
			item.RepoURL,
			item.Hash,
			item.Branch,
			item.Type,
			item.OriginalRepoURL,
			item.OriginalHash,
			item.OriginalBranch,
			item.ReplyToRepoURL,
			item.ReplyToHash,
			item.ReplyToBranch,
		)
		if err != nil {
			return err
		}

		// Only update counts for new items (first insert into social_items)
		// Edit commits should not update counts (they belong to canonical)
		isEdit := false
		if item.Hash != "" && item.RepoURL != "" && item.Branch != "" {
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM core_commits_version WHERE edit_repo_url = ? AND edit_hash = ? AND edit_branch = ?`,
				item.RepoURL, item.Hash, item.Branch).Scan(&count); err == nil {
				isEdit = count > 0
			}
		}
		if isNew && !isEdit {
			updateInteractionCounts(db, item)
		}
		return nil
	})
}

// updateInteractionCounts increments counts for original and reply_to posts.
func updateInteractionCounts(db *sql.DB, item SocialItem) {
	// Increment original (root post)
	if item.OriginalRepoURL.Valid && item.OriginalHash.Valid && item.OriginalBranch.Valid {
		switch item.Type {
		case "comment":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, comments) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET comments = comments + 1`,
				item.OriginalRepoURL.String, item.OriginalHash.String, item.OriginalBranch.String)
		case "repost":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, reposts) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET reposts = reposts + 1`,
				item.OriginalRepoURL.String, item.OriginalHash.String, item.OriginalBranch.String)
		case "quote":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, quotes) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET quotes = quotes + 1`,
				item.OriginalRepoURL.String, item.OriginalHash.String, item.OriginalBranch.String)
		}
	}

	// Increment reply_to (direct parent) if different from original
	if item.ReplyToRepoURL.Valid && item.ReplyToHash.Valid && item.ReplyToBranch.Valid {
		isDifferent := !item.OriginalRepoURL.Valid || !item.OriginalHash.Valid || !item.OriginalBranch.Valid ||
			item.ReplyToRepoURL.String != item.OriginalRepoURL.String ||
			item.ReplyToHash.String != item.OriginalHash.String ||
			item.ReplyToBranch.String != item.OriginalBranch.String
		if isDifferent {
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, comments) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET comments = comments + 1`,
				item.ReplyToRepoURL.String, item.ReplyToHash.String, item.ReplyToBranch.String)
		}
	}

	// Handle intermediate ancestors
	updateAncestorInteractions(db, item)
}

// updateAncestorInteractions walks up the parent chain from reply_to to original
// and increments interaction counts for each intermediate ancestor.
func updateAncestorInteractions(db *sql.DB, item SocialItem) {
	if !item.ReplyToRepoURL.Valid || !item.ReplyToHash.Valid || !item.ReplyToBranch.Valid ||
		!item.OriginalRepoURL.Valid || !item.OriginalHash.Valid || !item.OriginalBranch.Valid {
		return
	}
	currentRepoURL := item.ReplyToRepoURL.String
	currentHash := item.ReplyToHash.String
	currentBranch := item.ReplyToBranch.String
	originalRepoURL := item.OriginalRepoURL.String
	originalHash := item.OriginalHash.String
	originalBranch := item.OriginalBranch.String

	if currentRepoURL == originalRepoURL && currentHash == originalHash && currentBranch == originalBranch {
		return
	}

	// Walk up from reply_to's parent to original (max 50 levels to prevent cycles)
	const maxDepth = 50
	for depth := 0; depth < maxDepth; depth++ {
		var parentRepoURL, parentHash, parentBranch sql.NullString
		err := db.QueryRow(`SELECT reply_to_repo_url, reply_to_hash, reply_to_branch FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			currentRepoURL, currentHash, currentBranch).Scan(&parentRepoURL, &parentHash, &parentBranch)
		if err != nil || !parentRepoURL.Valid || !parentHash.Valid || !parentBranch.Valid {
			break
		}
		if parentRepoURL.String == originalRepoURL && parentHash.String == originalHash && parentBranch.String == originalBranch {
			break
		}
		currentRepoURL = parentRepoURL.String
		currentHash = parentHash.String
		currentBranch = parentBranch.String

		// Increment this ancestor's count
		switch item.Type {
		case "comment":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, comments) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET comments = comments + 1`,
				currentRepoURL, currentHash, currentBranch)
		case "repost":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, reposts) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET reposts = reposts + 1`,
				currentRepoURL, currentHash, currentBranch)
		case "quote":
			_, _ = db.Exec(`
				INSERT INTO social_interactions (repo_url, hash, branch, quotes) VALUES (?, ?, ?, 1)
				ON CONFLICT(repo_url, hash, branch) DO UPDATE SET quotes = quotes + 1`,
				currentRepoURL, currentHash, currentBranch)
		}
	}
}

// GetSocialItem retrieves a single social item by its composite key.
func GetSocialItem(repoURL, hash, branch string, workspaceURL string) (*SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) (*SocialItem, error) {
		query := baseSelectFromView + `
			WHERE v.repo_url = ? AND v.hash = ? AND v.branch = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		row := db.QueryRow(query, workspaceURL, repoURL, hash, branch)
		return scanResolvedRow(row)
	})
}

// GetSocialItemByRef looks up a social item by its ref string (e.g., "repo#commit:hash@branch")
func GetSocialItemByRef(refStr string, workspaceURL string) (*SocialItem, error) {
	parsed := protocol.ParseRef(refStr)
	if parsed.Type != protocol.RefTypeCommit {
		return nil, sql.ErrNoRows
	}
	ref := protocol.ResolveRefWithDefaults(refStr, workspaceURL, "main")
	if ref.Hash == "" {
		return nil, sql.ErrNoRows
	}
	return GetSocialItem(ref.RepoURL, ref.Hash, ref.Branch, workspaceURL)
}

type RepoRef struct {
	URL    string
	Branch string
}

type SocialQuery struct {
	Types            []string
	RepoURL          string
	RepoURLs         []string  // Multiple repo URLs (for external list posts)
	Repos            []RepoRef // URL+branch pairs for filtering
	Branch           string
	ListID           string
	ListIDs          []string
	WorkspaceURL     string
	OriginalRepoURL  string
	OriginalHash     string
	OriginalBranch   string
	Since            *time.Time
	Until            *time.Time
	Limit            int
	Offset           int
	Cursor           string // RFC3339 timestamp — items older than this (keyset pagination)
	ForFollowerCheck string // Workspace URL to check if repos follow (for gold color)
}

// GetSocialItems queries social items with filtering and pagination.
func GetSocialItems(q SocialQuery) ([]SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		var args []interface{}
		args = append(args, q.ForFollowerCheck)
		var where []string

		if len(q.Types) > 0 {
			ph := strings.Repeat("?,", len(q.Types))
			ph = ph[:len(ph)-1]
			where = append(where, "v.type IN ("+ph+")")
			for _, t := range q.Types {
				args = append(args, t)
			}
		}

		if q.RepoURL != "" {
			where = append(where, "v.repo_url = ?")
			args = append(args, q.RepoURL)
		}

		if len(q.RepoURLs) > 0 {
			ph := strings.Repeat("?,", len(q.RepoURLs))
			ph = ph[:len(ph)-1]
			where = append(where, "v.repo_url IN ("+ph+")")
			for _, url := range q.RepoURLs {
				args = append(args, url)
			}
		}

		if len(q.Repos) > 0 {
			var repoClauses []string
			for _, r := range q.Repos {
				if r.Branch != "" {
					repoClauses = append(repoClauses, "(v.repo_url = ? AND v.branch = ?)")
					args = append(args, r.URL, r.Branch)
				} else {
					repoClauses = append(repoClauses, "v.repo_url = ?")
					args = append(args, r.URL)
				}
			}
			where = append(where, "("+strings.Join(repoClauses, " OR ")+")")
		}

		if q.Branch != "" {
			where = append(where, "v.branch = ?")
			args = append(args, q.Branch)
		}

		if q.ListID != "" {
			where = append(where, "(v.repo_url, v.branch) IN (SELECT repo_url, branch FROM core_list_repositories WHERE list_id = ?)")
			args = append(args, q.ListID)
		}

		if len(q.ListIDs) > 0 || q.WorkspaceURL != "" {
			var orClauses []string
			if len(q.ListIDs) > 0 {
				ph := strings.Repeat("?,", len(q.ListIDs))
				ph = ph[:len(ph)-1]
				orClauses = append(orClauses, "(v.repo_url, v.branch) IN (SELECT repo_url, branch FROM core_list_repositories WHERE list_id IN ("+ph+"))")
				for _, id := range q.ListIDs {
					args = append(args, id)
				}
			}
			if q.WorkspaceURL != "" {
				orClauses = append(orClauses, "v.repo_url = ?")
				args = append(args, q.WorkspaceURL)
			}
			where = append(where, "("+strings.Join(orClauses, " OR ")+")")
		}

		if q.OriginalRepoURL != "" && q.OriginalHash != "" {
			if q.OriginalBranch != "" {
				where = append(where, "v.original_repo_url = ? AND v.original_hash = ? AND v.original_branch = ?")
				args = append(args, q.OriginalRepoURL, q.OriginalHash, q.OriginalBranch)
			} else {
				where = append(where, "v.original_repo_url = ? AND v.original_hash = ?")
				args = append(args, q.OriginalRepoURL, q.OriginalHash)
			}
		}

		if q.Since != nil {
			where = append(where, "v.timestamp >= ?")
			args = append(args, q.Since.Format(time.RFC3339))
		}

		if q.Until != nil {
			where = append(where, "v.timestamp <= ?")
			args = append(args, q.Until.Format(time.RFC3339))
		}

		if q.Cursor != "" {
			where = append(where, "v.timestamp < ?")
			args = append(args, q.Cursor)
		}

		where = append(where, "NOT v.is_edit_commit")
		where = append(where, "NOT v.is_retracted")
		where = append(where, "(v.stale_since IS NULL OR v.is_virtual = 1)")

		query := baseSelectFromView
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		query += " ORDER BY v.timestamp DESC"

		if q.Limit > 0 {
			query += " LIMIT ?"
			args = append(args, q.Limit)
		}
		if q.Offset > 0 && q.Cursor == "" {
			query += " OFFSET ?"
			args = append(args, q.Offset)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []SocialItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// GetTimeline retrieves posts from subscribed lists and workspace combined.
func GetTimeline(listIDs []string, workspaceURL string, limit int, cursor string) ([]SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		var unions []string
		var args []interface{}
		gitmsgFilter := " AND v.branch NOT LIKE 'refs/gitmsg/%'"
		editFilter := " AND NOT v.is_edit_commit AND NOT v.is_retracted AND (v.stale_since IS NULL OR v.is_virtual = 1)"
		if cursor != "" {
			editFilter += " AND v.timestamp < ?"
		}

		if len(listIDs) > 0 {
			ph := strings.Repeat("?,", len(listIDs))
			ph = ph[:len(ph)-1]
			unions = append(unions, baseSelectFromView+" WHERE v.repo_url IN (SELECT repo_url FROM core_list_repositories WHERE list_id IN ("+ph+"))"+gitmsgFilter+editFilter)
			args = append(args, workspaceURL)
			for _, id := range listIDs {
				args = append(args, id)
			}
			if cursor != "" {
				args = append(args, cursor)
			}
		}

		if workspaceURL != "" {
			unions = append(unions, baseSelectFromView+" WHERE v.repo_url = ?"+gitmsgFilter+editFilter)
			args = append(args, workspaceURL)
			args = append(args, workspaceURL)
			if cursor != "" {
				args = append(args, cursor)
			}
		}

		if len(unions) == 0 {
			return nil, nil
		}

		query := strings.Join(unions, " UNION ")
		query += " ORDER BY timestamp DESC"
		if limit > 0 {
			query += " LIMIT ?"
			args = append(args, limit)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []SocialItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// GetTimelineCount returns the total number of timeline items (without pagination).
func GetTimelineCount(listIDs []string, workspaceURL string) (int, error) {
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		var unions []string
		var args []interface{}
		gitmsgFilter := " AND v.branch NOT LIKE 'refs/gitmsg/%'"
		editFilter := " AND NOT v.is_edit_commit AND NOT v.is_retracted AND (v.stale_since IS NULL OR v.is_virtual = 1)"

		if len(listIDs) > 0 {
			ph := strings.Repeat("?,", len(listIDs))
			ph = ph[:len(ph)-1]
			unions = append(unions, "SELECT v.repo_url, v.hash, v.branch FROM social_items_resolved v WHERE v.repo_url IN (SELECT repo_url FROM core_list_repositories WHERE list_id IN ("+ph+"))"+gitmsgFilter+editFilter)
			for _, id := range listIDs {
				args = append(args, id)
			}
		}

		if workspaceURL != "" {
			unions = append(unions, "SELECT v.repo_url, v.hash, v.branch FROM social_items_resolved v WHERE v.repo_url = ?"+gitmsgFilter+editFilter)
			args = append(args, workspaceURL)
		}

		if len(unions) == 0 {
			return 0, nil
		}

		query := "SELECT COUNT(*) FROM (" + strings.Join(unions, " UNION ") + ")"
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// buildAllItemsWhere builds WHERE clause and args for social item queries.
func buildAllItemsWhere(q SocialQuery) ([]string, []interface{}) {
	var args []interface{}
	var where []string

	if q.RepoURL != "" {
		where = append(where, "v.repo_url = ?")
		args = append(args, q.RepoURL)
		where = append(where, "v.branch NOT LIKE 'refs/gitmsg/%'")
	}

	if q.Branch != "" {
		where = append(where, "v.branch = ?")
		args = append(args, q.Branch)
	}

	if len(q.RepoURLs) > 0 {
		ph := strings.Repeat("?,", len(q.RepoURLs))
		ph = ph[:len(ph)-1]
		where = append(where, "v.repo_url IN ("+ph+")")
		for _, url := range q.RepoURLs {
			args = append(args, url)
		}
	}

	if q.ListID != "" {
		where = append(where, "(v.repo_url, v.branch) IN (SELECT repo_url, branch FROM core_list_repositories WHERE list_id = ?)")
		args = append(args, q.ListID)
	}

	if len(q.ListIDs) > 0 || q.WorkspaceURL != "" {
		var orClauses []string
		if len(q.ListIDs) > 0 {
			ph := strings.Repeat("?,", len(q.ListIDs))
			ph = ph[:len(ph)-1]
			orClauses = append(orClauses, "(v.repo_url, v.branch) IN (SELECT repo_url, branch FROM core_list_repositories WHERE list_id IN ("+ph+"))")
			for _, id := range q.ListIDs {
				args = append(args, id)
			}
		}
		if q.WorkspaceURL != "" {
			orClauses = append(orClauses, "(v.repo_url = ? AND v.branch NOT LIKE 'refs/gitmsg/%')")
			args = append(args, q.WorkspaceURL)
		}
		where = append(where, "("+strings.Join(orClauses, " OR ")+")")
	}

	if q.Since != nil {
		where = append(where, "v.timestamp >= ?")
		args = append(args, q.Since.Format(time.RFC3339))
	}

	if q.Until != nil {
		where = append(where, "v.timestamp <= ?")
		args = append(args, q.Until.Format(time.RFC3339))
	}

	if q.Cursor != "" {
		where = append(where, "v.timestamp < ?")
		args = append(args, q.Cursor)
	}

	if len(q.Types) > 0 {
		ph := strings.Repeat("?,", len(q.Types))
		ph = ph[:len(ph)-1]
		where = append(where, "v.type IN ("+ph+")")
		for _, t := range q.Types {
			args = append(args, t)
		}
	}

	where = append(where, "NOT v.is_edit_commit")
	where = append(where, "NOT v.is_retracted")
	where = append(where, "(v.stale_since IS NULL OR v.is_virtual = 1)")

	return where, args
}

// GetAllItemsCount returns the total count of items matching the query (ignoring Limit/Cursor/Offset).
func GetAllItemsCount(q SocialQuery) (int, error) {
	q.Limit = 0
	q.Cursor = ""
	q.Offset = 0
	return cache.QueryLocked(func(db *sql.DB) (int, error) {
		where, args := buildAllItemsWhere(q)
		query := "SELECT COUNT(*) FROM social_items_resolved v"
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		var count int
		err := db.QueryRow(query, args...).Scan(&count)
		return count, err
	})
}

// GetAllItems retrieves all social items matching the query parameters.
func GetAllItems(q SocialQuery) ([]SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		where, whereArgs := buildAllItemsWhere(q)
		var args []interface{}
		args = append(args, q.ForFollowerCheck)
		args = append(args, whereArgs...)

		query := baseSelectFromView
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		query += " ORDER BY v.timestamp DESC"

		if q.Limit > 0 {
			query += " LIMIT ?"
			args = append(args, q.Limit)
		}
		if q.Offset > 0 && q.Cursor == "" {
			query += " OFFSET ?"
			args = append(args, q.Offset)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []SocialItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// GetThread retrieves all replies in a comment thread from a root post.
func GetThread(rootRepoURL, rootHash, rootBranch string, workspaceURL string) ([]SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		// Collect descendants (reply_to chain) and quotes/reposts (original_*=root)
		// into a single matches CTE so the outer query can drive by PK lookup.
		// Why: a previous OR between (c.PK IN descendants) and (s.original_*=?)
		// forced a full scan of core_commits — 6+ seconds on a 1M-commit cache.
		query := `
			WITH RECURSIVE descendants AS (
				SELECT ? as repo_url, ? as hash, ? as branch
				UNION
				SELECT si.repo_url, si.hash, si.branch FROM social_items si
				INNER JOIN descendants d ON si.reply_to_repo_url = d.repo_url AND si.reply_to_hash = d.hash AND si.reply_to_branch = d.branch
			), matches AS (
				SELECT repo_url, hash, branch FROM descendants
				UNION
				SELECT repo_url, hash, branch FROM social_items
				WHERE original_repo_url = ? AND original_hash = ? AND original_branch = ?
			)` + baseDirectSelect + `
			WHERE (c.repo_url, c.hash, c.branch) IN (SELECT repo_url, hash, branch FROM matches)
			   AND c.is_edit_commit = 0
			   AND c.is_retracted = 0
			ORDER BY COALESCE(c.origin_time, c.timestamp)`

		rows, err := db.Query(query, rootRepoURL, rootHash, rootBranch, rootRepoURL, rootHash, rootBranch, workspaceURL)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []SocialItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

// ResolvedVersion contains the resolved version of a post along with edit metadata
type ResolvedVersion struct {
	Item     *SocialItem
	IsEdited bool
}

// ResolveCurrentVersion finds the latest version of a post.
// Returns the resolved item (canonical with latest content) and whether the post has been edited.
func ResolveCurrentVersion(repoURL, hash, branch string, workspaceURL string) (ResolvedVersion, error) {
	// Resolve to canonical if this is an edit hash
	canonicalRepoURL, canonicalHash, canonicalBranch, err := cache.ResolveToCanonical(repoURL, hash, branch)
	if err != nil {
		return ResolvedVersion{}, err
	}

	// Get the canonical item — core_commits.effective_message exposes the latest content
	item, err := GetSocialItem(canonicalRepoURL, canonicalHash, canonicalBranch, workspaceURL)
	if err != nil {
		return ResolvedVersion{}, err
	}

	return ResolvedVersion{Item: item, IsEdited: item.IsEdited}, nil
}

// GetEditHistory returns all versions of a post (original + edits) ordered by timestamp desc.
// The first item is the latest version, the last is the original.
// Delegates to gitmsg.GetHistory since versioning is a core GitMsg feature.
func GetEditHistory(repoURL, hash, branch string, workspaceURL string) ([]SocialItem, error) {
	canonicalID := protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch)
	versions, err := gitmsg.GetHistory(canonicalID, workspaceURL)
	if err != nil {
		return nil, err
	}
	var items []SocialItem
	for _, v := range versions {
		items = append(items, SocialItem{
			RepoURL:     v.RepoURL,
			Hash:        v.CommitHash,
			Branch:      v.Branch,
			Type:        v.Type,
			Content:     v.Content,
			AuthorName:  v.AuthorName,
			AuthorEmail: v.AuthorEmail,
			Timestamp:   v.Timestamp,
			IsRetracted: v.IsRetracted,
			EditOf:      sql.NullString{String: v.EditOf, Valid: v.EditOf != ""},
		})
	}
	return items, nil
}

// GetEditHistoryPosts returns all versions of a post as Post structs.
func GetEditHistoryPosts(repoURL, hash, branch string, workspaceURL string) ([]Post, error) {
	items, err := GetEditHistory(repoURL, hash, branch, workspaceURL)
	if err != nil {
		return nil, err
	}
	var posts []Post
	for _, item := range items {
		posts = append(posts, SocialItemToPost(item))
	}
	return posts, nil
}

// extractOriginalExtType extracts the extension and type from the first GitMsg-Ref in a message.
// Used for cross-extension navigation (e.g., social comment on PM issue).
func extractOriginalExtType(rawMessage string) (ext, typ string) {
	msg := protocol.ParseMessage(rawMessage)
	if msg == nil || len(msg.References) == 0 {
		return "", ""
	}
	ref := msg.References[0]
	return ref.Ext, ref.Fields["type"]
}

// extractHeaderFields extracts the extension, type, and state from the GitMsg header.
func extractHeaderFields(rawMessage string) (ext, typ, state string) {
	msg := protocol.ParseMessage(rawMessage)
	if msg == nil {
		return "", "", ""
	}
	return msg.Header.Ext, msg.Header.Fields["type"], msg.Header.Fields["state"]
}

// scanResolvedRow scans a single row from baseSelectFromView queries.
// Column order matches baseSelectFromView constant.
func scanResolvedRow(row *sql.Row) (*SocialItem, error) {
	var item SocialItem
	var ts, message, originalMessage, staleSince, editorName, editorEmail sql.NullString
	var isVirtual, isRetracted, hasEdits, followsWorkspace int
	err := row.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type,
		&item.OriginalRepoURL, &item.OriginalHash, &item.OriginalBranch,
		&item.ReplyToRepoURL, &item.ReplyToHash, &item.ReplyToBranch,
		&item.EditOf, &item.Comments, &item.Reposts, &item.Quotes,
		&isVirtual, &isRetracted, &hasEdits, &staleSince,
		&editorName, &editorEmail, &followsWorkspace,
	)
	if err != nil {
		return nil, err
	}
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
		item.OriginalExtension, item.OriginalType = extractOriginalExtType(message.String)
		item.HeaderExt, item.HeaderType, item.HeaderState = extractHeaderFields(message.String)
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.EditorName = editorName.String
	item.EditorEmail = editorEmail.String
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	item.IsStale = staleSince.Valid
	item.FollowsWorkspace = followsWorkspace == 1
	return &item, nil
}

// scanResolvedRows scans multiple rows from baseSelectFromView queries.
func scanResolvedRows(rows *sql.Rows) (*SocialItem, error) {
	var item SocialItem
	var ts, message, originalMessage, staleSince, editorName, editorEmail sql.NullString
	var isVirtual, isRetracted, hasEdits, followsWorkspace int
	err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &message, &originalMessage, &ts,
		&item.Type,
		&item.OriginalRepoURL, &item.OriginalHash, &item.OriginalBranch,
		&item.ReplyToRepoURL, &item.ReplyToHash, &item.ReplyToBranch,
		&item.EditOf, &item.Comments, &item.Reposts, &item.Quotes,
		&isVirtual, &isRetracted, &hasEdits, &staleSince,
		&editorName, &editorEmail, &followsWorkspace,
	)
	if err != nil {
		return nil, err
	}
	if message.Valid {
		item.Content = protocol.ExtractCleanContent(message.String)
		item.OriginalExtension, item.OriginalType = extractOriginalExtType(message.String)
		item.HeaderExt, item.HeaderType, item.HeaderState = extractHeaderFields(message.String)
	}
	if originalMessage.Valid {
		if msg := protocol.ParseMessage(originalMessage.String); msg != nil {
			item.Origin = protocol.ExtractOrigin(&msg.Header)
		}
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	item.EditorName = editorName.String
	item.EditorEmail = editorEmail.String
	item.IsVirtual = isVirtual == 1
	item.IsRetracted = isRetracted == 1
	item.IsEdited = hasEdits == 1
	item.IsStale = staleSince.Valid
	item.FollowsWorkspace = followsWorkspace == 1
	return &item, nil
}

// SocialItemToPost converts a SocialItem to a Post for API responses.
func SocialItemToPost(item SocialItem) Post {
	postType := PostType(item.Type)
	if postType == "" {
		postType = PostTypePost
	}

	// Create refs from composite keys (including branch)
	originalPostID := ""
	if item.OriginalRepoURL.Valid && item.OriginalHash.Valid {
		origBranch := ""
		if item.OriginalBranch.Valid {
			origBranch = item.OriginalBranch.String
		}
		originalPostID = protocol.CreateRef(protocol.RefTypeCommit, item.OriginalHash.String, item.OriginalRepoURL.String, origBranch)
	}

	parentCommentID := ""
	if item.ReplyToRepoURL.Valid && item.ReplyToHash.Valid {
		replyBranch := ""
		if item.ReplyToBranch.Valid {
			replyBranch = item.ReplyToBranch.String
		}
		parentCommentID = protocol.CreateRef(protocol.RefTypeCommit, item.ReplyToHash.String, item.ReplyToRepoURL.String, replyBranch)
	}

	editOf := ""
	if item.EditOf.Valid {
		editOf = item.EditOf.String
	}

	content := strings.ReplaceAll(item.Content, "\r", "")
	repo := item.RepoURL
	if repo == "" {
		repo = "myrepository"
	}

	// Compute the full ref ID
	id := protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch)

	return Post{
		ID:         id,
		Repository: repo,
		Branch:     item.Branch,
		Author: Author{
			Name:  item.AuthorName,
			Email: item.AuthorEmail,
		},
		Timestamp:         item.Timestamp,
		Content:           content,
		Type:              postType,
		Source:            PostSourceExplicit,
		CleanContent:      content,
		OriginalPostID:    originalPostID,
		ParentCommentID:   parentCommentID,
		EditOf:            editOf,
		EditorName:        item.EditorName,
		EditorEmail:       item.EditorEmail,
		IsRetracted:       item.IsRetracted,
		IsEdited:          item.IsEdited,
		IsVirtual:         item.IsVirtual,
		IsStale:           item.IsStale,
		OriginalExtension: item.OriginalExtension,
		OriginalType:      item.OriginalType,
		HeaderExt:         item.HeaderExt,
		HeaderType:        item.HeaderType,
		HeaderState:       item.HeaderState,
		Origin:            item.Origin,
		Interactions: Interactions{
			Comments: item.Comments,
			Reposts:  item.Reposts,
			Quotes:   item.Quotes,
		},
		Display: Display{
			CommitHash:   item.Hash,
			TotalReposts: item.Reposts + item.Quotes,
			FollowsYou:   item.FollowsWorkspace,
		},
	}
}

// GetParentChain retrieves ancestor posts in a reply chain.
func GetParentChain(repoURL, hash, branch string, workspaceURL string) ([]SocialItem, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]SocialItem, error) {
		// CTE walks reply_to chain upward, then joins core_commits directly
		// (bypasses views to avoid materializing 1M+ rows).
		query := `
			WITH RECURSIVE ancestors AS (
				SELECT ? as repo_url, ? as hash, ? as branch, 0 as depth
				UNION ALL
				SELECT p.repo_url, p.hash, p.branch, a.depth + 1 as depth
				FROM ancestors a
				JOIN social_items ai ON ai.repo_url = a.repo_url AND ai.hash = a.hash AND ai.branch = a.branch
				JOIN core_commits p ON
					(ai.reply_to_repo_url IS NOT NULL AND p.repo_url = ai.reply_to_repo_url AND p.hash = ai.reply_to_hash AND p.branch = ai.reply_to_branch)
					OR (ai.reply_to_repo_url IS NULL AND ai.original_repo_url IS NOT NULL AND p.repo_url = ai.original_repo_url AND p.hash = ai.original_hash AND p.branch = ai.original_branch)
				WHERE a.depth < 50
			)` + baseDirectSelect + `
			WHERE (c.repo_url, c.hash, c.branch) IN (SELECT repo_url, hash, branch FROM ancestors WHERE depth > 0)
			ORDER BY (SELECT depth FROM ancestors a2 WHERE a2.repo_url = c.repo_url AND a2.hash = c.hash AND a2.branch = c.branch) DESC`

		rows, err := db.Query(query, repoURL, hash, branch, workspaceURL)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var items []SocialItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, *item)
		}
		return items, rows.Err()
	})
}

type InteractionCounts struct {
	Comments int
	Reposts  int
	Quotes   int
}

// RefreshInteractionCounts retrieves current interaction counts for a post.
func RefreshInteractionCounts(repoURL, hash, branch string) (InteractionCounts, error) {
	return cache.QueryLocked(func(db *sql.DB) (InteractionCounts, error) {
		var counts InteractionCounts
		err := db.QueryRow(`
			SELECT COALESCE(comments, 0), COALESCE(reposts, 0), COALESCE(quotes, 0)
			FROM social_interactions
			WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch).Scan(&counts.Comments, &counts.Reposts, &counts.Quotes)
		if err == sql.ErrNoRows {
			return InteractionCounts{}, nil
		}
		return counts, err
	})
}
