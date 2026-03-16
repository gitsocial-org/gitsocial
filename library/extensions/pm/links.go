// links.go - Issue link operations against pm_links table
package pm

import (
	"database/sql"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// InsertLinks writes link rows for an issue, replacing any existing links.
func InsertLinks(repoURL, hash, branch string, blocks, blockedBy, related []IssueRef) error {
	return cache.ExecLocked(func(db *sql.DB) error {
		// Delete existing links from this issue
		if _, err := db.Exec(`DELETE FROM pm_links WHERE from_repo_url = ? AND from_hash = ? AND from_branch = ?`,
			repoURL, hash, branch); err != nil {
			return err
		}
		stmt, err := db.Prepare(`INSERT OR IGNORE INTO pm_links (from_repo_url, from_hash, from_branch, to_repo_url, to_hash, to_branch, link_type) VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, ref := range blocks {
			if _, err := stmt.Exec(repoURL, hash, branch, ref.RepoURL, ref.Hash, ref.Branch, string(LinkTypeBlocks)); err != nil {
				return err
			}
		}
		for _, ref := range blockedBy {
			if _, err := stmt.Exec(repoURL, hash, branch, ref.RepoURL, ref.Hash, ref.Branch, string(LinkTypeBlockedBy)); err != nil {
				return err
			}
		}
		for _, ref := range related {
			if _, err := stmt.Exec(repoURL, hash, branch, ref.RepoURL, ref.Hash, ref.Branch, string(LinkTypeRelated)); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetLinks returns links declared by an issue (forward lookup).
func GetLinks(repoURL, hash, branch string) ([]Link, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]Link, error) {
		rows, err := db.Query(`SELECT from_repo_url, from_hash, from_branch, to_repo_url, to_hash, to_branch, link_type FROM pm_links WHERE from_repo_url = ? AND from_hash = ? AND from_branch = ?`,
			repoURL, hash, branch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanLinks(rows)
	})
}

// GetLinksTo returns links pointing to an issue (reverse lookup).
func GetLinksTo(repoURL, hash, branch string) ([]Link, error) {
	return cache.QueryLocked(func(db *sql.DB) ([]Link, error) {
		rows, err := db.Query(`SELECT from_repo_url, from_hash, from_branch, to_repo_url, to_hash, to_branch, link_type FROM pm_links WHERE to_repo_url = ? AND to_hash = ? AND to_branch = ?`,
			repoURL, hash, branch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanLinks(rows)
	})
}

func scanLinks(rows *sql.Rows) ([]Link, error) {
	var links []Link
	for rows.Next() {
		var l Link
		var linkType string
		if err := rows.Scan(&l.From.RepoURL, &l.From.Hash, &l.From.Branch, &l.To.RepoURL, &l.To.Hash, &l.To.Branch, &linkType); err != nil {
			return nil, err
		}
		l.Type = LinkType(linkType)
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetBlocking returns issues that this issue blocks (forward "blocks" links).
func GetBlocking(issueRef string) Result[[]Issue] {
	refs, err := resolveRefForLinks(issueRef)
	if err != nil {
		return result.Err[[]Issue]("INVALID_REF", err.Error())
	}
	links, err := GetLinks(refs.RepoURL, refs.Hash, refs.Branch)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	var issues []Issue
	for _, l := range links {
		if l.Type == LinkTypeBlocks {
			if issue, err := getIssueByRef(l.To); err == nil {
				issues = append(issues, issue)
			}
		}
	}
	return result.Ok(issues)
}

// GetBlockedBy returns issues that block this issue (forward "blocked-by" + reverse "blocks").
func GetBlockedBy(issueRef string) Result[[]Issue] {
	refs, err := resolveRefForLinks(issueRef)
	if err != nil {
		return result.Err[[]Issue]("INVALID_REF", err.Error())
	}
	// Forward: this issue declared blocked-by
	forward, err := GetLinks(refs.RepoURL, refs.Hash, refs.Branch)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	seen := map[string]bool{}
	var issues []Issue
	for _, l := range forward {
		if l.Type == LinkTypeBlockedBy {
			key := l.To.RepoURL + l.To.Hash + l.To.Branch
			if !seen[key] {
				seen[key] = true
				if issue, err := getIssueByRef(l.To); err == nil {
					issues = append(issues, issue)
				}
			}
		}
	}
	// Reverse: other issues declared blocks pointing to this issue
	reverse, err := GetLinksTo(refs.RepoURL, refs.Hash, refs.Branch)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	for _, l := range reverse {
		if l.Type == LinkTypeBlocks {
			key := l.From.RepoURL + l.From.Hash + l.From.Branch
			if !seen[key] {
				seen[key] = true
				if issue, err := getIssueByRef(l.From); err == nil {
					issues = append(issues, issue)
				}
			}
		}
	}
	return result.Ok(issues)
}

// GetRelated returns issues related to the given issue (both directions).
func GetRelated(issueRef string) Result[[]Issue] {
	refs, err := resolveRefForLinks(issueRef)
	if err != nil {
		return result.Err[[]Issue]("INVALID_REF", err.Error())
	}
	seen := map[string]bool{}
	var issues []Issue
	// Forward
	forward, err := GetLinks(refs.RepoURL, refs.Hash, refs.Branch)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	for _, l := range forward {
		if l.Type == LinkTypeRelated {
			key := l.To.RepoURL + l.To.Hash + l.To.Branch
			if !seen[key] {
				seen[key] = true
				if issue, err := getIssueByRef(l.To); err == nil {
					issues = append(issues, issue)
				}
			}
		}
	}
	// Reverse
	reverse, err := GetLinksTo(refs.RepoURL, refs.Hash, refs.Branch)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	for _, l := range reverse {
		if l.Type == LinkTypeRelated {
			key := l.From.RepoURL + l.From.Hash + l.From.Branch
			if !seen[key] {
				seen[key] = true
				if issue, err := getIssueByRef(l.From); err == nil {
					issues = append(issues, issue)
				}
			}
		}
	}
	return result.Ok(issues)
}

// IsBlocked returns true if any blocking issue is still open.
func IsBlocked(issueRef string) bool {
	res := GetBlockedBy(issueRef)
	if !res.Success {
		return false
	}
	for _, issue := range res.Data {
		if issue.State == StateOpen && !issue.IsRetracted {
			return true
		}
	}
	return false
}

// resolveRefForLinks parses an issue ref string into an IssueRef.
func resolveRefForLinks(issueRef string) (IssueRef, error) {
	parsed := protocol.ResolveRefWithDefaults(issueRef, "", "gitmsg/pm")
	if parsed.Hash == "" {
		return IssueRef{}, sql.ErrNoRows
	}
	return IssueRef{RepoURL: parsed.RepoURL, Hash: parsed.Hash, Branch: parsed.Branch}, nil
}

// getIssueByRef fetches a single issue by its IssueRef coordinates.
func getIssueByRef(ref IssueRef) (Issue, error) {
	item, err := GetPMItem(ref.RepoURL, ref.Hash, ref.Branch)
	if err != nil {
		return Issue{}, err
	}
	return PMItemToIssue(*item), nil
}

// parseRefList parses a comma-separated ref string into IssueRef slices.
func parseRefList(refsStr, defaultRepoURL, defaultBranch string) []IssueRef {
	if refsStr == "" {
		return nil
	}
	var refs []IssueRef
	for _, part := range splitComma(refsStr) {
		resolved := protocol.ResolveRefWithDefaults(part, defaultRepoURL, defaultBranch)
		if resolved.Hash != "" {
			refs = append(refs, IssueRef{RepoURL: resolved.RepoURL, Hash: resolved.Hash, Branch: resolved.Branch})
		}
	}
	return refs
}

// splitComma splits a comma-separated string, trimming whitespace.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}
