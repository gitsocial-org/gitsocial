// stack.go - Stacked PR query and traversal functions
package review

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// StackEntry represents a PR in a stack with its position.
type StackEntry struct {
	PullRequest PullRequest
	Position    int
}

// GetStack reconstructs the full stack from any member PR by walking depends-on
// chains in both directions. Returns entries ordered bottom-up (root first).
func GetStack(prRef string) Result[[]StackEntry] {
	startResult := GetPR(prRef)
	if !startResult.Success {
		return result.Err[[]StackEntry](startResult.Error.Code, startResult.Error.Message)
	}
	start := startResult.Data

	// Walk down to find the root (PR with no depends-on or all merged deps)
	root := start
	visited := map[string]bool{root.ID: true}
	for len(root.DependsOn) > 0 {
		depRef := qualifyRefWithRepo(root.DependsOn[0], root.Repository)
		depResult := GetPR(depRef)
		if !depResult.Success {
			break
		}
		dep := depResult.Data
		if visited[dep.ID] {
			break
		}
		visited[dep.ID] = true
		root = dep
	}

	// Walk up from root, collecting the stack
	var entries []StackEntry
	entries = append(entries, StackEntry{PullRequest: root, Position: 0})
	visited = map[string]bool{root.ID: true}

	current := root
	for {
		dependents := GetDependents(current.Repository, current.Branch, extractRefHash(current.ID))
		var next *PullRequest
		for _, dep := range dependents {
			if !visited[dep.ID] {
				next = &dep
				break
			}
		}
		if next == nil {
			break
		}
		visited[next.ID] = true
		entries = append(entries, StackEntry{PullRequest: *next, Position: len(entries)})
		current = *next
	}

	if len(entries) <= 1 {
		return result.Err[[]StackEntry]("NOT_A_STACK", "pull request is not part of a stack")
	}

	return result.Ok(entries)
}

// FindPRByHead finds open PRs whose head branch matches the given branch ref.
// Used for auto-detecting stack relationships when creating a new PR.
func FindPRByHead(headRef string) []PullRequest {
	if headRef == "" {
		return nil
	}
	items, err := cache.QueryLocked(func(db *sql.DB) ([]ReviewItem, error) {
		query := baseSelectFromView + `
			WHERE v.type = 'pull-request'
			  AND v.state = 'open'
			  AND v.head = ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		rows, err := db.Query(query, headRef)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			result = append(result, *item)
		}
		return result, rows.Err()
	})
	if err != nil {
		return nil
	}
	prs := make([]PullRequest, 0, len(items))
	for _, item := range items {
		prs = append(prs, ReviewItemToPullRequest(item))
	}
	return prs
}

// RebaseStack rebases all PRs above the given PR in the stack.
// For each dependent, rebases its head branch onto its base branch and updates tips.
// Returns the list of updated PRs or an error if a rebase fails.
func RebaseStack(workdir, prRef string) Result[[]PullRequest] {
	startResult := GetPR(prRef)
	if !startResult.Success {
		return result.Err[[]PullRequest](startResult.Error.Code, startResult.Error.Message)
	}
	start := startResult.Data
	if start.State != PRStateOpen {
		return result.Err[[]PullRequest]("INVALID_STATE", "cannot rebase stack: PR is "+string(start.State))
	}

	// Collect the upward chain from this PR
	var chain []PullRequest
	current := start
	visited := map[string]bool{current.ID: true}
	for {
		dependents := GetDependents(current.Repository, current.Branch, extractRefHash(current.ID))
		var next *PullRequest
		for _, dep := range dependents {
			if !visited[dep.ID] && dep.State == PRStateOpen {
				next = &dep
				break
			}
		}
		if next == nil {
			break
		}
		visited[next.ID] = true
		chain = append(chain, *next)
		current = *next
	}

	if len(chain) == 0 {
		return result.Err[[]PullRequest]("NO_DEPENDENTS", "no open dependents to rebase")
	}

	var updated []PullRequest
	for _, dep := range chain {
		syncResult := SyncPRBranch(workdir, dep.ID, "rebase")
		if !syncResult.Success {
			return result.Err[[]PullRequest]("REBASE_FAILED",
				fmt.Sprintf("rebase failed on \"%s\": %s", dep.Subject, syncResult.Error.Message))
		}
		updated = append(updated, syncResult.Data)
	}

	return result.Ok(updated)
}

// SyncStackTips updates base-tip/head-tip for all open PRs in the stack.
func SyncStackTips(workdir, prRef string) Result[[]PullRequest] {
	stackResult := GetStack(prRef)
	if !stackResult.Success {
		return result.Err[[]PullRequest](stackResult.Error.Code, stackResult.Error.Message)
	}

	var updated []PullRequest
	for _, entry := range stackResult.Data {
		if entry.PullRequest.State != PRStateOpen {
			continue
		}
		tipResult := UpdatePRTips(workdir, entry.PullRequest.ID)
		if !tipResult.Success {
			continue
		}
		updated = append(updated, tipResult.Data)
	}

	return result.Ok(updated)
}

// qualifyRefWithRepo ensures a ref has a repository component by filling from defaultRepo if missing.
// Used to resolve workspace-local depends-on refs against the containing PR's repository.
func qualifyRefWithRepo(ref, defaultRepo string) string {
	if defaultRepo == "" {
		return ref
	}
	parsed := protocol.ParseRef(ref)
	if parsed.Repository != "" {
		return ref
	}
	return protocol.CreateRef(parsed.Type, parsed.Value, defaultRepo, parsed.Branch)
}

// GetDependents finds all PRs that depend on the given PR (by hash match in depends_on field).
func GetDependents(repoURL, branch, hash string) []PullRequest {
	if hash == "" {
		return nil
	}
	items, err := cache.QueryLocked(func(db *sql.DB) ([]ReviewItem, error) {
		query := baseSelectFromView + `
			WHERE v.type = 'pull-request'
			  AND v.depends_on LIKE ?
			  AND NOT v.is_edit_commit AND NOT v.is_retracted`
		rows, err := db.Query(query, "%"+hash+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []ReviewItem
		for rows.Next() {
			item, err := scanResolvedRows(rows)
			if err != nil {
				return nil, err
			}
			result = append(result, *item)
		}
		return result, rows.Err()
	})
	if err != nil {
		return nil
	}
	prs := make([]PullRequest, 0, len(items))
	for _, item := range items {
		pr := ReviewItemToPullRequest(item)
		// Verify the hash actually appears in depends-on (LIKE match may be too broad)
		for _, dep := range pr.DependsOn {
			if strings.Contains(dep, hash) {
				prs = append(prs, pr)
				break
			}
		}
	}
	return prs
}
