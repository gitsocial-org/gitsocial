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
    COALESCE(le.type, p.type) as type,
    COALESCE(le.state, p.state) as state,
    COALESCE(le.draft, p.draft) as draft,
    COALESCE(le.base, p.base) as base,
    COALESCE(le.base_tip, p.base_tip) as base_tip,
    COALESCE(le.head, p.head) as head,
    COALESCE(le.head_tip, p.head_tip) as head_tip,
    COALESCE(le.depends_on, p.depends_on) as depends_on,
    COALESCE(le.closes, p.closes) as closes,
    COALESCE(le.reviewers, p.reviewers) as reviewers,
    COALESCE(le.pull_request_repo_url, p.pull_request_repo_url) as pull_request_repo_url,
    COALESCE(le.pull_request_hash, p.pull_request_hash) as pull_request_hash,
    COALESCE(le.pull_request_branch, p.pull_request_branch) as pull_request_branch,
    COALESCE(le.commit_ref, p.commit_ref) as commit_ref,
    COALESCE(le.file, p.file) as file,
    COALESCE(le.old_line, p.old_line) as old_line,
    COALESCE(le.new_line, p.new_line) as new_line,
    COALESCE(le.old_line_end, p.old_line_end) as old_line_end,
    COALESCE(le.new_line_end, p.new_line_end) as new_line_end,
    COALESCE(le.review_state, p.review_state) as review_state,
    COALESCE(le.suggestion, p.suggestion) as suggestion,
    r.labels,
    COALESCE(si.comments, 0) as comments
FROM core_commits_resolved r
INNER JOIN review_items p ON r.repo_url = p.repo_url AND r.hash = p.hash AND r.branch = p.branch
LEFT JOIN social_interactions si ON r.repo_url = si.repo_url AND r.hash = si.hash AND r.branch = si.branch
LEFT JOIN (
    SELECT v.canonical_repo_url, v.canonical_hash, v.canonical_branch,
           pe.type, pe.state, pe.draft, pe.base, pe.base_tip, pe.head, pe.head_tip, pe.depends_on, pe.closes, pe.reviewers,
           pe.pull_request_repo_url, pe.pull_request_hash, pe.pull_request_branch,
           pe.commit_ref, pe.file, pe.old_line, pe.new_line, pe.old_line_end, pe.new_line_end,
           pe.review_state, pe.suggestion,
           ROW_NUMBER() OVER (
               PARTITION BY v.canonical_repo_url, v.canonical_hash, v.canonical_branch
               ORDER BY e.timestamp DESC
           ) as rn
    FROM core_commits_version v
    JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
    JOIN review_items pe ON v.edit_repo_url = pe.repo_url AND v.edit_hash = pe.hash AND v.edit_branch = pe.branch
) le ON le.canonical_repo_url = r.repo_url
    AND le.canonical_hash = r.hash
    AND le.canonical_branch = r.branch
    AND le.rn = 1;
`
