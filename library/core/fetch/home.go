// home.go - The home branch of a workspace commit: the walked refs, the branch order and each branch's own commits
package fetch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

const (
	localRefPrefix  = "refs/heads/"
	originRefPrefix = "refs/remotes/origin/"
	contentPrefix   = "gitmsg/"
)

// homeBranch is a logical branch with the local and origin refs that carry it.
type homeBranch struct {
	name string
	refs []string
	tips []string
}

// listHomeRefs returns the walked refs, one line each with the name, the tip and the symref: the gate string of a sync.
func listHomeRefs(workdir string) (string, error) {
	result, err := git.ExecGit(workdir, []string{
		"for-each-ref", "--format=%(refname) %(objectname) %(symref)", localRefPrefix, originRefPrefix,
	})
	if err != nil {
		return "", fmt.Errorf("list branch refs: %w", err)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// parseHomeRefs splits a gate string into the stable branches in home order (the default branch, then the content branches by name) and the code branches by name.
func parseHomeRefs(gate string) (stable, code []homeBranch) {
	byName := make(map[string]*homeBranch)
	originHead := ""
	for _, line := range strings.Split(gate, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref, tip := fields[0], fields[1]
		if ref == originRefPrefix+"HEAD" {
			if len(fields) > 2 {
				originHead = strings.TrimPrefix(fields[2], originRefPrefix)
			}
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(ref, localRefPrefix), originRefPrefix)
		branch := byName[name]
		if branch == nil {
			branch = &homeBranch{name: name}
			byName[name] = branch
		}
		branch.refs = append(branch.refs, ref)
		branch.tips = append(branch.tips, tip)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	defaultBranch := defaultHomeBranch(names, byName, originHead)
	if defaultBranch != "" {
		stable = append(stable, *byName[defaultBranch])
	}
	for _, name := range names {
		switch {
		case name == defaultBranch:
		case strings.HasPrefix(name, contentPrefix):
			stable = append(stable, *byName[name])
		default:
			code = append(code, *byName[name])
		}
	}
	return stable, code
}

// defaultHomeBranch names the default branch from the refs alone: origin's HEAD, main, master, then the first code branch by name.
func defaultHomeBranch(names []string, byName map[string]*homeBranch, originHead string) string {
	for _, name := range []string{originHead, "main", "master"} {
		if byName[name] != nil {
			return name
		}
	}
	for _, name := range names {
		if !strings.HasPrefix(name, contentPrefix) {
			return name
		}
	}
	return ""
}

// branchRefs returns the refs of the given branches.
func branchRefs(branches []homeBranch) []string {
	var refs []string
	for _, branch := range branches {
		refs = append(refs, branch.refs...)
	}
	return refs
}

// stableHashes returns, per stable branch, the commits it reaches that no earlier stable branch does and that are after its tips in since.
func stableHashes(workdir string, stable []homeBranch, since map[string][]string, limit int) ([][]string, error) {
	lists := make([][]string, len(stable))
	for i, branch := range stable {
		exclude := append(branchRefs(stable[:i]), since[branch.name]...)
		hashes, err := git.GetCommitHashes(workdir, branch.refs, exclude, limit)
		if err != nil {
			return nil, err
		}
		lists[i] = hashes
	}
	return lists, nil
}

// stableSince returns the last sync's tips of each stable branch when the stable branches are the same and each only grew; nil asks for the full path.
func stableSince(workdir, lastGate string, stable []homeBranch) map[string][]string {
	last, _ := parseHomeRefs(lastGate)
	if len(last) != len(stable) {
		return nil
	}
	since := make(map[string][]string, len(stable))
	for i, branch := range stable {
		if last[i].name != branch.name {
			return nil
		}
		since[branch.name] = last[i].tips
		if strings.Join(last[i].tips, " ") == strings.Join(branch.tips, " ") {
			continue
		}
		// An old tip that the branch no longer reaches is a rewrite, and its rows need the full check.
		args := append([]string{"rev-list", "--max-count=1"}, last[i].tips...)
		for _, ref := range branch.refs {
			args = append(args, "^"+ref)
		}
		if result, err := git.ExecGit(workdir, args); err != nil || strings.TrimSpace(result.Stdout) != "" {
			return nil
		}
	}
	return since
}

// codeHomes puts the unmerged code branches ancestor first and by name, and returns each one's own commits, newest first.
func codeHomes(workdir string, stable, code []homeBranch) ([]homeBranch, [][]string, error) {
	graph, err := git.GetCommitGraph(workdir, branchRefs(code), branchRefs(stable))
	if err != nil {
		return nil, nil, err
	}
	parents := make(map[string][]string, len(graph))
	for _, node := range graph {
		parents[node[0]] = node[2:]
	}
	// A branch with no tip in the region is merged into a stable branch and has no commit of its own.
	var unmerged []homeBranch
	for _, branch := range code {
		var tips []string
		for _, tip := range branch.tips {
			if _, ok := parents[tip]; ok {
				tips = append(tips, tip)
			}
		}
		if len(tips) > 0 {
			unmerged = append(unmerged, homeBranch{name: branch.name, refs: branch.refs, tips: tips})
		}
	}
	ordered := ancestorFirst(unmerged, parents)
	home := make(map[string]int, len(graph))
	for i, branch := range ordered {
		walkRegion(branch.tips, parents, func(hash string) bool {
			if _, taken := home[hash]; taken {
				return false
			}
			home[hash] = i
			return true
		})
	}
	lists := make([][]string, len(ordered))
	for _, node := range graph {
		if i, ok := home[node[0]]; ok && len(node) < 4 {
			lists[i] = append(lists[i], node[1])
		}
	}
	return ordered, lists, nil
}

// walkRegion visits each commit of the region that the tips reach, and goes past a commit only when visit accepts it.
func walkRegion(tips []string, parents map[string][]string, visit func(hash string) bool) {
	stack := append([]string(nil), tips...)
	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		next, inRegion := parents[hash]
		if !inRegion || !visit(hash) {
			continue
		}
		stack = append(stack, next...)
	}
}

// ancestorFirst sorts branches so that one whose tips another reaches comes before it, and unrelated ones by name.
func ancestorFirst(branches []homeBranch, parents map[string][]string) []homeBranch {
	// within[i][j] reports that branch j reaches every tip of branch i.
	within := make([][]bool, len(branches))
	for i := range within {
		within[i] = make([]bool, len(branches))
	}
	for j, branch := range branches {
		seen := make(map[string]bool)
		walkRegion(branch.tips, parents, func(hash string) bool {
			if seen[hash] {
				return false
			}
			seen[hash] = true
			return true
		})
		for i, other := range branches {
			within[i][j] = allIn(other.tips, seen)
		}
	}
	waiting := make([]int, len(branches))
	for j := range branches {
		for i := range branches {
			if i != j && within[i][j] && !within[j][i] {
				waiting[j]++
			}
		}
	}
	placed := make([]bool, len(branches))
	sorted := make([]homeBranch, 0, len(branches))
	for len(sorted) < len(branches) {
		next := -1
		for j := range branches {
			if !placed[j] && (waiting[j] == 0 || next < 0) {
				next = j
				if waiting[j] == 0 {
					break
				}
			}
		}
		placed[next] = true
		sorted = append(sorted, branches[next])
		for j := range branches {
			if !placed[j] && within[next][j] && !within[j][next] {
				waiting[j]--
			}
		}
	}
	return sorted
}

// allIn reports whether every hash is in the set.
func allIn(hashes []string, set map[string]bool) bool {
	for _, hash := range hashes {
		if !set[hash] {
			return false
		}
	}
	return true
}
