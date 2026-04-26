// schema.go - Release extension database schema
package release

import "github.com/gitsocial-org/gitsocial/core/cache"

func init() {
	cache.RegisterSchema("release", schema)
}

const schema = `
-- Extension: Release items
CREATE TABLE IF NOT EXISTS release_items (
    repo_url TEXT NOT NULL,
    hash TEXT NOT NULL,
    branch TEXT NOT NULL,
    tag TEXT,
    version TEXT,
    prerelease INTEGER DEFAULT 0,
    artifacts TEXT,
    artifact_url TEXT,
    checksums TEXT,
    signed_by TEXT,
    sbom TEXT,
    PRIMARY KEY (repo_url, hash, branch),
    FOREIGN KEY (repo_url, hash, branch) REFERENCES core_commits(repo_url, hash, branch)
);
CREATE INDEX IF NOT EXISTS idx_release_version ON release_items(version);
CREATE INDEX IF NOT EXISTS idx_release_tag ON release_items(tag);

-- Extension: Release resolved view (unified read interface).
-- Resolved-state columns live directly on core_commits; mutable extension
-- fields (tag, version, etc.) are maintained on the canonical's release_items row
-- by applyEditToCanonical at edit time, so no ROW_NUMBER subquery is needed.
DROP VIEW IF EXISTS release_items_resolved;
CREATE VIEW release_items_resolved AS
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
    p.tag,
    p.version,
    p.prerelease,
    p.artifacts,
    p.artifact_url,
    p.checksums,
    p.signed_by,
    p.sbom,
    COALESCE(si.comments, 0) as comments
FROM core_commits c
INNER JOIN release_items p ON c.repo_url = p.repo_url AND c.hash = p.hash AND c.branch = p.branch
LEFT JOIN social_interactions si ON c.repo_url = si.repo_url AND c.hash = si.hash AND c.branch = si.branch;

-- Extension: SBOM summary cache
CREATE TABLE IF NOT EXISTS release_sbom_cache (
    repo_url TEXT NOT NULL,
    version TEXT NOT NULL,
    format TEXT,
    packages INTEGER DEFAULT 0,
    generator TEXT,
    licenses_json TEXT,
    generated TEXT,
    items_json TEXT,
    cached_at TEXT,
    PRIMARY KEY (repo_url, version)
);
`
