// schema.go - Social extension database schema
package social

import "github.com/gitsocial-org/gitsocial/core/cache"

func init() {
	cache.RegisterSchema("social", schema)
}

const schema = `
-- Extension: Social items
CREATE TABLE IF NOT EXISTS social_items (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    type TEXT NOT NULL,
    original_repo_url TEXT,
    original_hash TEXT,
    original_branch TEXT,
    reply_to_repo_url TEXT,
    reply_to_hash TEXT,
    reply_to_branch TEXT,
    PRIMARY KEY (repo_url, hash, branch),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_social_type ON social_items(type);
CREATE INDEX IF NOT EXISTS idx_social_original ON social_items(original_repo_url, original_hash, original_branch);
CREATE INDEX IF NOT EXISTS idx_social_reply_to ON social_items(reply_to_repo_url, reply_to_hash, reply_to_branch);

-- Extension: Social interactions (comment/repost/quote counts)
CREATE TABLE IF NOT EXISTS social_interactions (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    comments INTEGER DEFAULT 0,
    reposts INTEGER DEFAULT 0,
    quotes INTEGER DEFAULT 0,
    PRIMARY KEY (repo_url, hash, branch)
);

-- Extension: Social resolved view (unified read interface).
-- Projects core_commits' generated effective_* columns under the legacy
-- output names (resolved_message, author_name, etc.) so consumers don't change.
DROP VIEW IF EXISTS social_items_resolved;
CREATE VIEW social_items_resolved AS
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
    COALESCE(s.type, 'post') as type,
    s.original_repo_url,
    s.original_hash,
    s.original_branch,
    s.reply_to_repo_url,
    s.reply_to_hash,
    s.reply_to_branch,
    COALESCE(i.comments, 0) as comments,
    COALESCE(i.reposts, 0) as reposts,
    COALESCE(i.quotes, 0) as quotes
FROM core_commits c
LEFT JOIN social_items s ON c.repo_url = s.repo_url AND c.hash = s.hash AND c.branch = s.branch
LEFT JOIN social_interactions i ON c.repo_url = i.repo_url AND c.hash = i.hash AND c.branch = i.branch;

-- Extension: Social notifications read state (legacy, migrated to core_notification_reads)
CREATE TABLE IF NOT EXISTS social_notification_reads (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    read_at TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);

-- Migration: copy social_notification_reads to core_notification_reads
INSERT OR IGNORE INTO core_notification_reads (repo_url, hash, branch, read_at)
SELECT repo_url, hash, branch, read_at FROM social_notification_reads;

-- Extension: Social followers (tracks which repos follow a workspace)
CREATE TABLE IF NOT EXISTS social_followers (
    repo_url TEXT NOT NULL,
    workspace_url TEXT NOT NULL,
    detected_at TEXT,
    list_id TEXT,
    commit_hash TEXT,
    PRIMARY KEY (repo_url, workspace_url)
);
CREATE INDEX IF NOT EXISTS idx_social_followers_workspace ON social_followers(workspace_url);

-- Extension: Social repo lists (cached lists from external repositories)
CREATE TABLE IF NOT EXISTS social_repo_lists (
    repo_url TEXT NOT NULL,
    list_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT DEFAULT '0.1.0',
    commit_hash TEXT,
    cached_at TEXT NOT NULL,
    PRIMARY KEY (repo_url, list_id)
);
CREATE INDEX IF NOT EXISTS idx_social_repo_lists_repo ON social_repo_lists(repo_url);

CREATE TABLE IF NOT EXISTS social_repo_list_repositories (
    owner_repo_url TEXT NOT NULL,
    list_id TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    branch TEXT DEFAULT 'main',
    PRIMARY KEY (owner_repo_url, list_id, repo_url),
    FOREIGN KEY (owner_repo_url, list_id) REFERENCES social_repo_lists(repo_url, list_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_social_repo_list_repos_list ON social_repo_list_repositories(owner_repo_url, list_id);
`
