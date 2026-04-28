// schema.go - PM extension database schema
package pm

import "github.com/gitsocial-org/gitsocial/core/cache"

func init() {
	cache.RegisterSchema("pm", schema)
}

const schema = `
-- Extension: PM items (issues, milestones, and sprints)
CREATE TABLE IF NOT EXISTS pm_items (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    type TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'open',
    assignees TEXT,
    due TEXT,
    start_date TEXT,
    end_date TEXT,
    milestone_repo_url TEXT,
    milestone_hash TEXT,
    milestone_branch TEXT,
    sprint_repo_url TEXT,
    sprint_hash TEXT,
    sprint_branch TEXT,
    parent_repo_url TEXT,
    parent_hash TEXT,
    parent_branch TEXT,
    root_repo_url TEXT,
    root_hash TEXT,
    root_branch TEXT,
    labels TEXT,
    PRIMARY KEY (repo_url, hash, branch),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_pm_type ON pm_items(type);
CREATE INDEX IF NOT EXISTS idx_pm_state ON pm_items(state);
CREATE INDEX IF NOT EXISTS idx_pm_labels ON pm_items(labels);
CREATE INDEX IF NOT EXISTS idx_pm_milestone ON pm_items(milestone_repo_url, milestone_hash, milestone_branch);
CREATE INDEX IF NOT EXISTS idx_pm_sprint ON pm_items(sprint_repo_url, sprint_hash, sprint_branch);
CREATE INDEX IF NOT EXISTS idx_pm_parent ON pm_items(parent_repo_url, parent_hash, parent_branch);
CREATE INDEX IF NOT EXISTS idx_pm_root ON pm_items(root_repo_url, root_hash, root_branch);

-- Extension: PM links (blocks, depends-on relationships)
CREATE TABLE IF NOT EXISTS pm_links (
    from_repo_url TEXT NOT NULL,
    from_hash TEXT NOT NULL,
    from_branch TEXT NOT NULL,
    to_repo_url TEXT NOT NULL,
    to_hash TEXT NOT NULL,
    to_branch TEXT NOT NULL,
    link_type TEXT NOT NULL,
    PRIMARY KEY (from_repo_url, from_hash, from_branch, to_repo_url, to_hash, to_branch, link_type)
);
CREATE INDEX IF NOT EXISTS idx_pm_links_to ON pm_links(to_repo_url, to_hash, to_branch);

-- Extension: PM assignees normalized for indexed search. Maintained alongside
-- pm_items.assignees (the comma-separated source-of-truth for display).
CREATE TABLE IF NOT EXISTS pm_assignees (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    email TEXT NOT NULL,
    PRIMARY KEY (repo_url, hash, branch, email),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES pm_items(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_pm_assignees_email ON pm_assignees(email);

-- Extension: PM resolved view (unified read interface).
-- Resolved-state columns live directly on core_commits; mutable extension
-- fields (state, assignees, due, etc.) are maintained on the canonical's pm_items
-- row by applyEditToCanonical at edit time, so no ROW_NUMBER subquery is needed.
DROP VIEW IF EXISTS pm_items_resolved;
CREATE VIEW pm_items_resolved AS
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
    p.type,
    p.state,
    p.assignees,
    p.due,
    p.start_date,
    p.end_date,
    p.milestone_repo_url,
    p.milestone_hash,
    p.milestone_branch,
    p.sprint_repo_url,
    p.sprint_hash,
    p.sprint_branch,
    p.parent_repo_url,
    p.parent_hash,
    p.parent_branch,
    p.root_repo_url,
    p.root_hash,
    p.root_branch,
    COALESCE(c.labels, p.labels) as labels,
    COALESCE(si.comments, 0) as comments
FROM core_commits c
INNER JOIN pm_items p ON c.repo_url = p.repo_url AND c.hash = p.hash AND c.branch = p.branch
LEFT JOIN social_interactions si ON c.repo_url = si.repo_url AND c.hash = si.hash AND c.branch = si.branch;
`
