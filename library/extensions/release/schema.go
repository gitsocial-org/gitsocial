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

-- Extension: Release resolved view (unified read interface)
DROP VIEW IF EXISTS release_items_resolved;
CREATE VIEW release_items_resolved AS
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
    COALESCE(le.tag, p.tag) as tag,
    COALESCE(le.version, p.version) as version,
    COALESCE(le.prerelease, p.prerelease) as prerelease,
    COALESCE(le.artifacts, p.artifacts) as artifacts,
    COALESCE(le.artifact_url, p.artifact_url) as artifact_url,
    COALESCE(le.checksums, p.checksums) as checksums,
    COALESCE(le.signed_by, p.signed_by) as signed_by,
    COALESCE(le.sbom, p.sbom) as sbom,
    COALESCE(si.comments, 0) as comments
FROM core_commits_resolved r
INNER JOIN release_items p ON r.repo_url = p.repo_url AND r.hash = p.hash AND r.branch = p.branch
LEFT JOIN social_interactions si ON r.repo_url = si.repo_url AND r.hash = si.hash AND r.branch = si.branch
LEFT JOIN (
    SELECT v.canonical_repo_url, v.canonical_hash, v.canonical_branch,
           pe.tag, pe.version, pe.prerelease, pe.artifacts, pe.artifact_url,
           pe.checksums, pe.signed_by, pe.sbom,
           ROW_NUMBER() OVER (
               PARTITION BY v.canonical_repo_url, v.canonical_hash, v.canonical_branch
               ORDER BY e.timestamp DESC
           ) as rn
    FROM core_commits_version v
    JOIN core_commits e ON v.edit_repo_url = e.repo_url AND v.edit_hash = e.hash AND v.edit_branch = e.branch
    JOIN release_items pe ON v.edit_repo_url = pe.repo_url AND v.edit_hash = pe.hash AND v.edit_branch = pe.branch
) le ON le.canonical_repo_url = r.repo_url
    AND le.canonical_hash = r.hash
    AND le.canonical_branch = r.branch
    AND le.rn = 1;

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
