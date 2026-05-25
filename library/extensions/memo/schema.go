// schema.go - Memo extension database schema
package memo

import "github.com/gitsocial-org/gitsocial/library/core/cache"

func init() {
	cache.RegisterSchema("memo", schema)
}

// memo_items tags commits as memos so core/search and the resolved view can
// join cleanly. There are no memo-specific fields here: tiering is a function
// of repo_url + branch, and metadata (priority, expires, topic) lives in core
// labels.
const schema = `
-- Extension: Memo items (marker table; metadata lives in core labels)
CREATE TABLE IF NOT EXISTS memo_items (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'memo',
    PRIMARY KEY (repo_url, hash, branch),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);

-- Extension: Memo resolved view (unified read interface).
--
-- Callers that read this view get effective fields (post-edit, post-origin)
-- via the COALESCE-backed effective_* columns on core_commits, but they are
-- still responsible for filtering edit / retracted / stale rows:
--   WHERE NOT is_edit_commit AND NOT is_retracted AND stale_since IS NULL
-- The Go-side helpers in items.go apply these by default; raw SQL written
-- against the view must add them explicitly.
DROP VIEW IF EXISTS memo_items_resolved;
CREATE VIEW memo_items_resolved AS
SELECT
    c.repo_url,
    c.hash,
    c.branch,
    c.effective_message AS resolved_message,
    c.message AS original_message,
    c.edits,
    c.is_retracted,
    c.has_edits,
    c.is_edit_commit,
    c.effective_author_name AS author_name,
    c.effective_author_email AS author_email,
    c.effective_timestamp AS timestamp,
    c.is_virtual,
    c.stale_since,
    c.labels,
    m.type
FROM core_commits c
INNER JOIN memo_items m ON c.repo_url = m.repo_url AND c.hash = m.hash AND c.branch = m.branch;
`
