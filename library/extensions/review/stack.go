// stack.go - Stacked PR query and traversal functions
package review

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// StackEntry represents a PR in a stack with its position.
type StackEntry struct {
	PullRequest PullRequest
	Position    int
}

// GetStack reconstructs a stack from any member, root first.
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
		dependents := getDependents(extractRefHash(current.ID))
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

// FindPRByHead finds the open pull requests whose head is the given branch ref.
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
			item, err := scanResolvedRow(rows)
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

// RebaseStack rebases every pull request above the given one, stopping at the first failure.
func RebaseStack(workdir, prRef string) Result[[]PullRequest] {
	startResult := GetPR(prRef)
	if !startResult.Success {
		return result.Err[[]PullRequest](startResult.Error.Code, startResult.Error.Message)
	}
	start := startResult.Data
	if start.State != PRStateOpen {
		return result.Err[[]PullRequest]("INVALID_STATE", "cannot rebase stack: pull request is "+string(start.State))
	}

	// Collect the upward chain from this PR
	var chain []PullRequest
	current := start
	visited := map[string]bool{current.ID: true}
	for {
		dependents := getDependents(extractRefHash(current.ID))
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

// qualifyRefWithRepo fills a ref's missing repository from defaultRepo.
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

// getDependents finds the pull requests whose depends-on names the given hash.
func getDependents(hash string) []PullRequest {
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
			item, err := scanResolvedRow(rows)
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
	// A copy and the original it adopts both carry the depends-on, so the original collapses into its copy.
	adopted := map[string]bool{}
	for _, item := range items {
		if ref := protocol.ParseRef(item.Adopts); ref.Type == protocol.RefTypeCommit && ref.Value != "" {
			adopted[adoptedPRKey(ref.Repository, ref.Value)] = true
		}
	}
	prs := make([]PullRequest, 0, len(items))
	for _, item := range items {
		if adopted[adoptedPRKey(item.RepoURL, item.Hash)] {
			continue
		}
		pr := ReviewItemToPullRequest(item)
		// The LIKE match is broad, so each candidate is checked against the parsed refs.
		for _, dep := range pr.DependsOn {
			if strings.Contains(dep, hash) {
				prs = append(prs, pr)
				break
			}
		}
	}
	return prs
}
