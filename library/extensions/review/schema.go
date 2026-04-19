// schema.go - Review extension database schema
package review

import (
	"database/sql"

	"github.com/gitsocial-org/gitsocial/core/cache"
)

func init() {
	cache.RegisterSchema("review", schema)
	cache.RegisterMigration(func(db *sql.DB) {
		_, _ = db.Exec(`ALTER TABLE review_items ADD COLUMN draft INTEGER DEFAULT 0`)
	})
	cache.RegisterMigration(func(db *sql.DB) {
		_, _ = db.Exec(`ALTER TABLE review_items ADD COLUMN depends_on TEXT`)
	})
}

const schema = `
-- Extension: Review items (pull requests and feedback)
CREATE TABLE IF NOT EXISTS review_items (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    type TEXT NOT NULL,
    state TEXT,
    draft INTEGER DEFAULT 0,
    base TEXT,
    base_tip TEXT,
    head TEXT,
    head_tip TEXT,
    closes TEXT,
    reviewers TEXT,
    pull_request_repo_url TEXT,
    pull_request_hash TEXT,
    pull_request_branch TEXT,
    commit_ref TEXT,
    file TEXT,
    old_line INTEGER,
    new_line INTEGER,
    old_line_end INTEGER,
    new_line_end INTEGER,
    review_state TEXT,
    suggestion INTEGER DEFAULT 0,
    PRIMARY KEY (repo_url, hash, branch),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_review_type ON review_items(type);
CREATE INDEX IF NOT EXISTS idx_review_state ON review_items(state);
CREATE INDEX IF NOT EXISTS idx_review_pr ON review_items(pull_request_repo_url, pull_request_hash, pull_request_branch);
CREATE INDEX IF NOT EXISTS idx_review_file ON review_items(file);

-- Extension: Review resolved view (unified read interface)
-- Mutable fields (state, draft, reviewers, etc.) are maintained on the canonical's
-- raw row by syncExtensionFields at edit time, so no ROW_NUMBER subquery is needed.
DROP VIEW IF EXISTS review_items_resolved;
CREATE VIEW review_items_resolved AS
SELECT
    r.repo_url,
    r.hash,
    r.branch,
    r.resolved_message,
    r.original_message,
    r.edits,
    r.is_retracted,
    r.has_edits,
    r.is_edit_commit,
    r.author_name,
    r.author_email,
    r.timestamp,
    r.is_virtual,
    r.stale_since,
    p.type,
    p.state,
    p.draft,
    p.base,
    p.base_tip,
    p.head,
    p.head_tip,
    p.depends_on,
    p.closes,
    p.reviewers,
    p.pull_request_repo_url,
    p.pull_request_hash,
    p.pull_request_branch,
    p.commit_ref,
    p.file,
    p.old_line,
    p.new_line,
    p.old_line_end,
    p.new_line_end,
    p.review_state,
    p.suggestion,
    r.labels,
    COALESCE(si.comments, 0) as comments
FROM core_commits_resolved r
INNER JOIN review_items p ON r.repo_url = p.repo_url AND r.hash = p.hash AND r.branch = p.branch
LEFT JOIN social_interactions si ON r.repo_url = si.repo_url AND r.hash = si.hash AND r.branch = si.branch;
`
