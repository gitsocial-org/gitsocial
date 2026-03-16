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

-- Extension: PM resolved view (unified read interface)
-- Resolves PM metadata from latest edit, falling back to canonical (mirrors social pattern)
DROP VIEW IF EXISTS pm_items_resolved;
CREATE VIEW pm_items_resolved AS
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
    COALESCE(le.assignees, p.assignees) as assignees,
    COALESCE(le.due, p.due) as due,
    COALESCE(le.start_date, p.start_date) as start_date,
    COALESCE(le.end_date, p.end_date) as end_date,
    COALESCE(le.milestone_repo_url, p.milestone_repo_url) as milestone_repo_url,
    COALESCE(le.milestone_hash, p.milestone_hash) as milestone_hash,
    COALESCE(le.milestone_branch, p.milestone_branch) as milestone_branch,
    COALESCE(le.sprint_repo_url, p.sprint_repo_url) as sprint_repo_url,
    COALESCE(le.sprint_hash, p.sprint_hash) as sprint_hash,
    COALESCE(le.sprint_branch, p.sprint_branch) as sprint_branch,
    COALESCE(le.parent_repo_url, p.parent_repo_url) as parent_repo_url,
    COALESCE(le.parent_hash, p.parent_hash) as parent_hash,
    COALESCE(le.parent_branch, p.parent_branch) as parent_branch,
    COALESCE(le.root_repo_url, p.root_repo_url) as root_repo_url,
    COALESCE(le.root_hash, p.root_hash) as root_hash,
    COALESCE(le.root_branch, p.root_branch) as root_branch,
    COALESCE(r.labels, p.labels) as labels,
    COALESCE(si.comments, 0) as comments
FROM core_commits_resolved r
INNER JOIN pm_items p ON r.repo_url = p.repo_url AND r.hash = p.hash AND r.branch = p.branch
LEFT JOIN social_interactions si ON r.repo_url = si.repo_url AND r.hash = si.hash AND r.branch = si.branch
LEFT JOIN (
    SELECT v.canonical_repo_url, v.canonical_hash, v.canonical_branch,
           pe.type, pe.state, pe.assignees, pe.due, pe.start_date, pe.end_date,
           pe.milestone_repo_url, pe.milestone_hash, pe.milestone_branch,
           pe.sprint_repo_url, pe.sprint_hash, pe.sprint_branch,
           pe.parent_repo_url, pe.parent_hash, pe.parent_branch,
           pe.root_repo_url, pe.root_hash, pe.root_branch,
           ROW_NUMBER() OVER (
               PARTITION BY v.canonical_repo_url, v.canonical_hash, v.canonical_branch
               ORDER BY e.timestamp DESC
           ) as rn
    FROM core_commits_version v
    JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
    JOIN pm_items pe ON v.edit_repo_url = pe.repo_url AND v.edit_hash = pe.hash AND v.edit_branch = pe.branch
) le ON le.canonical_repo_url = r.repo_url
    AND le.canonical_hash = r.hash
    AND le.canonical_branch = r.branch
    AND le.rn = 1;
`
