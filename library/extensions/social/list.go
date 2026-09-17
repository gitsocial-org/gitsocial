// list.go - List CRUD operations and repository management
package social

import (
	"regexp"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

var listIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

const socialExtension = "social"

// GetLists retrieves all social lists from the workspace.
func GetLists(workdir string) Result[[]List] {
	names, err := gitmsg.EnumerateLists(workdir, socialExtension)
	if err != nil {
		return failureWithDetails[[]List]("GIT_ERROR", "enumerate lists", err)
	}

	lists := make([]List, 0, len(names))
	for _, name := range names {
		data, err := gitmsg.ReadList(workdir, socialExtension, name)
		if err != nil || data == nil {
			continue
		}
		lists = append(lists, listDataToList(*data))
	}

	return success(lists)
}

// GetList retrieves a single list by its ID.
func GetList(workdir, listID string) Result[*List] {
	data, err := gitmsg.ReadList(workdir, socialExtension, listID)
	if err != nil {
		return failureWithDetails[*List]("GIT_ERROR", "read list", err)
	}

	if data == nil {
		return success[*List](nil)
	}

	list := listDataToList(*data)
	return success(&list)
}

// CreateList creates a new empty list with the given ID and name.
func CreateList(workdir, listID, name string) Result[List] {
	if !listIDPattern.MatchString(listID) {
		return failure[List]("INVALID_LIST_ID", "list ID must match [a-zA-Z0-9_-]{1,40}")
	}

	existing, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if existing != nil {
		return failure[List]("LIST_EXISTS", "list '"+listID+"' already exists")
	}

	if name == "" {
		name = listID
	}

	data := gitmsg.ListData{
		Version:      "0.1.0",
		ID:           listID,
		Name:         name,
		Repositories: []string{},
	}

	if err := gitmsg.WriteList(workdir, socialExtension, listID, data); err != nil {
		return failureWithDetails[List]("GIT_ERROR", "create list", err)
	}

	return success(listDataToList(data))
}

// DeleteList removes a list by its ID.
func DeleteList(workdir, listID string) Result[struct{}] {
	existing, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if existing == nil {
		return failure[struct{}]("LIST_NOT_FOUND", "list '"+listID+"' not found")
	}

	if err := gitmsg.DeleteList(workdir, socialExtension, listID); err != nil {
		return failureWithDetails[struct{}]("GIT_ERROR", "delete list", err)
	}

	return success(struct{}{})
}

// AddRepositoryToList adds a repository to a list and returns the saved ref; allBranches stores "*".
func AddRepositoryToList(workdir, listID, repoURL, branch string, allBranches bool) Result[string] {
	data, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if data == nil {
		return failure[string]("LIST_NOT_FOUND", "list '"+listID+"' not found")
	}

	// The member ref keeps the address, the spelling git is handed; the cache keeps the identity.
	address := strings.TrimSpace(repoURL)
	identity := protocol.NormalizeURL(address)
	if allBranches {
		branch = "*"
	} else if branch == "" {
		branch = git.GetRemoteDefaultBranch(workdir, address)
	}
	repoRef := address + "#branch:" + branch

	for _, repo := range data.Repositories {
		if protocol.ParseRepositoryID(repo).Repository == identity {
			return failure[string]("REPOSITORY_EXISTS", "repository already in the list: use --all-branches to follow every branch")
		}
	}

	if err := gitmsg.AddListMember(workdir, socialExtension, listID, repoRef); err != nil {
		return failureWithDetails[string]("GIT_ERROR", "update list", err)
	}

	// Sync to cache for immediate visibility
	if err := cache.AddRepositoryToList(listID, identity, branch); err != nil {
		log.Warn("cache sync for list add failed", "list", listID, "repo", identity, "error", err)
	}

	return success(repoRef)
}

// RemoveRepositoryFromList removes a repository from a list.
func RemoveRepositoryFromList(workdir, listID, repoURL string) Result[struct{}] {
	data, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if data == nil {
		return failure[struct{}]("LIST_NOT_FOUND", "list '"+listID+"' not found")
	}

	// Any spelling removes the member: both sides compare as identities, and a branch in the argument picks that member.
	target := protocol.ParseRepositoryID(repoURL)
	_, wantBranch, _ := strings.Cut(repoURL, "#branch:")
	var foundRef string
	for _, repo := range data.Repositories {
		member := protocol.ParseRepositoryID(repo)
		if member.Repository == target.Repository && (wantBranch == "" || member.Branch == wantBranch) {
			foundRef = repo
			break
		}
	}
	if foundRef == "" {
		return failure[struct{}]("REPOSITORY_NOT_FOUND", "repository not in the list")
	}

	if err := gitmsg.RemoveListMember(workdir, socialExtension, listID, foundRef); err != nil {
		return failureWithDetails[struct{}]("GIT_ERROR", "update list", err)
	}

	return success(struct{}{})
}

// listDataToList converts gitmsg.ListData to the social List type.
func listDataToList(data gitmsg.ListData) List {
	return List{
		ID:           data.ID,
		Name:         data.Name,
		Version:      data.Version,
		Repositories: data.Repositories,
	}
}
