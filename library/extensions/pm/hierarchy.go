// hierarchy.go - Issue parent/root derivation and children queries (GITPM.md §1.7)
package pm

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// DeriveHierarchy resolves the parent and root header refs for a child issue
// from a user-supplied parent ref, per GITPM.md §1.7: a direct child of a
// top-level issue carries only root (which references that parent, no parent
// field), while a nested child carries both parent (immediate) and root
// (top-level ancestor). Returns localized "#commit:" ref strings ready for
// CreateIssueOptions/UpdateIssueOptions. An empty parentRef yields empty refs
// (clears hierarchy). selfRef, when non-empty, is the issue being edited; the
// parent must not be it nor appear anywhere in its ancestry (cycle prevention).
func DeriveHierarchy(parentRef, defaultRepoURL, selfRef string) (parent, root string, err error) {
	if strings.TrimSpace(parentRef) == "" {
		return "", "", nil
	}
	parentItem, err := GetPMItemByRef(parentRef, defaultRepoURL)
	if err != nil {
		return "", "", fmt.Errorf("parent issue not found: %s", parentRef)
	}
	if selfRef != "" {
		if selfItem, err := GetPMItemByRef(selfRef, defaultRepoURL); err == nil {
			selfKey := itemKey(selfItem.RepoURL, selfItem.Hash, selfItem.Branch)
			if itemKey(parentItem.RepoURL, parentItem.Hash, parentItem.Branch) == selfKey {
				return "", "", fmt.Errorf("an issue cannot be its own parent")
			}
			if err := ensureNoCycle(parentItem, selfKey); err != nil {
				return "", "", err
			}
		}
	}
	parentRefStr := localizedCommitRef(parentItem.RepoURL, parentItem.Hash, parentItem.Branch, defaultRepoURL)
	if parentItem.RootHash.Valid && parentItem.RootHash.String != "" {
		rootRefStr := localizedCommitRef(parentItem.RootRepoURL.String, parentItem.RootHash.String, parentItem.RootBranch.String, defaultRepoURL)
		return parentRefStr, rootRefStr, nil
	}
	// Parent is top-level: the child is a direct child carrying only root.
	return "", parentRefStr, nil
}

// localizedCommitRef builds a "#commit:" ref, stripping the repo prefix when it
// matches the workspace so serialized headers stay workspace-relative.
func localizedCommitRef(repoURL, hash, branch, defaultRepoURL string) string {
	return protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch), defaultRepoURL)
}

// itemKey composes a stable comparison key for a PM item's composite coordinates.
func itemKey(repoURL, hash, branch string) string {
	return repoURL + "\x00" + hash + "\x00" + branch
}

// ensureNoCycle walks the candidate parent's ancestry (immediate parent, then
// root) and errors if selfKey appears — preventing an edit that would make an
// issue a descendant of itself. The hierarchy is shallow, so the walk is short;
// a visited guard defends against malformed data.
func ensureNoCycle(start *PMItem, selfKey string) error {
	visited := map[string]bool{}
	cur := start
	for cur != nil {
		key := itemKey(cur.RepoURL, cur.Hash, cur.Branch)
		if key == selfKey {
			return fmt.Errorf("cannot set parent: would create a hierarchy cycle")
		}
		if visited[key] {
			return nil
		}
		visited[key] = true
		var next *PMItem
		if cur.ParentHash.Valid && cur.ParentHash.String != "" {
			next, _ = GetPMItem(cur.ParentRepoURL.String, cur.ParentHash.String, cur.ParentBranch.String)
		} else if cur.RootHash.Valid && cur.RootHash.String != "" {
			next, _ = GetPMItem(cur.RootRepoURL.String, cur.RootHash.String, cur.RootBranch.String)
		}
		cur = next
	}
	return nil
}

// childSelectColumns selects the extension and effective commit columns needed
// to build an Issue from a direct pm_items/core_commits join.
const childSelectColumns = `c.repo_url, c.hash, c.branch,
	       c.effective_author_name, c.effective_author_email, c.effective_message, c.effective_timestamp,
	       p.state, p.assignees, p.due,
	       p.milestone_repo_url, p.milestone_hash, p.milestone_branch,
	       p.sprint_repo_url, p.sprint_hash, p.sprint_branch,
	       p.parent_repo_url, p.parent_hash, p.parent_branch,
	       p.root_repo_url, p.root_hash, p.root_branch,
	       p.labels`

// GetChildIssues returns the direct children of the given issue (GITPM.md §1.7):
// issues whose parent is the issue, plus direct children of a top-level issue
// that carry only root = the issue (no parent field). Drives from pm_items
// joined to core_commits — the WHERE is highly selective on extension columns,
// so this bypasses the resolved view per the cache guidelines. Excludes edit,
// retracted, stale, and virtual commits, mirroring GetIssues filtering.
func GetChildIssues(repoURL, hash, branch string) Result[[]Issue] {
	items, err := cache.QueryLocked(func(db *sql.DB) ([]PMItem, error) {
		query := `SELECT ` + childSelectColumns + `
			FROM pm_items p
			JOIN core_commits c ON p.repo_url = c.repo_url AND p.hash = c.hash AND p.branch = c.branch
			WHERE p.type = ?
			  AND (
			    (p.parent_repo_url = ? AND p.parent_hash = ? AND p.parent_branch = ?)
			    OR (p.root_repo_url = ? AND p.root_hash = ? AND p.root_branch = ?
			        AND (p.parent_hash IS NULL OR p.parent_hash = ''))
			  )
			  AND NOT c.is_edit_commit AND NOT c.is_retracted
			  AND c.is_virtual = 0 AND c.stale_since IS NULL
			ORDER BY c.effective_timestamp ASC`
		rows, err := db.Query(query, string(ItemTypeIssue), repoURL, hash, branch, repoURL, hash, branch)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []PMItem
		for rows.Next() {
			item, err := scanChildRow(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, *item)
		}
		return out, rows.Err()
	})
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}
	issues := make([]Issue, len(items))
	for i, item := range items {
		issues[i] = PMItemToIssue(item)
	}
	return result.Ok(issues)
}

// scanChildRow scans a childSelectColumns row into a PMItem.
func scanChildRow(rows *sql.Rows) (*PMItem, error) {
	var item PMItem
	var content, ts sql.NullString
	if err := rows.Scan(
		&item.RepoURL, &item.Hash, &item.Branch,
		&item.AuthorName, &item.AuthorEmail, &content, &ts,
		&item.State, &item.Assignees, &item.Due,
		&item.MilestoneRepoURL, &item.MilestoneHash, &item.MilestoneBranch,
		&item.SprintRepoURL, &item.SprintHash, &item.SprintBranch,
		&item.ParentRepoURL, &item.ParentHash, &item.ParentBranch,
		&item.RootRepoURL, &item.RootHash, &item.RootBranch,
		&item.Labels,
	); err != nil {
		return nil, err
	}
	item.Type = string(ItemTypeIssue)
	if content.Valid {
		item.Content = protocol.ExtractCleanContent(content.String)
	}
	if ts.Valid {
		item.Timestamp, _ = time.Parse(time.RFC3339, ts.String)
	}
	return &item, nil
}
