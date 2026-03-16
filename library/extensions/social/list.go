// list.go - List CRUD operations and repository management
package social

import (
	"regexp"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var listIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

const socialExtension = "social"

// GetLists retrieves all social lists from the workspace.
func GetLists(workdir string) Result[[]List] {
	names, err := gitmsg.EnumerateLists(workdir, socialExtension)
	if err != nil {
		return FailureWithDetails[[]List]("GIT_ERROR", "Failed to enumerate lists", err)
	}

	lists := make([]List, 0, len(names))
	for _, name := range names {
		data, err := gitmsg.ReadList(workdir, socialExtension, name)
		if err != nil || data == nil {
			continue
		}
		lists = append(lists, listDataToList(*data))
	}

	return Success(lists)
}

// GetList retrieves a single list by its ID.
func GetList(workdir, listID string) Result[*List] {
	data, err := gitmsg.ReadList(workdir, socialExtension, listID)
	if err != nil {
		return FailureWithDetails[*List]("GIT_ERROR", "Failed to read list", err)
	}

	if data == nil {
		return Success[*List](nil)
	}

	list := listDataToList(*data)
	return Success(&list)
}

// CreateList creates a new empty list with the given ID and name.
func CreateList(workdir, listID, name string) Result[List] {
	if !listIDPattern.MatchString(listID) {
		return Failure[List]("INVALID_LIST_ID", "List ID must match pattern [a-zA-Z0-9_-]{1,40}")
	}

	existing, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if existing != nil {
		return Failure[List]("LIST_EXISTS", "List '"+listID+"' already exists")
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
		return FailureWithDetails[List]("GIT_ERROR", "Failed to create list", err)
	}

	return Success(listDataToList(data))
}

// DeleteList removes a list by its ID.
func DeleteList(workdir, listID string) Result[struct{}] {
	existing, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if existing == nil {
		return Failure[struct{}]("LIST_NOT_FOUND", "List '"+listID+"' not found")
	}

	if err := gitmsg.DeleteList(workdir, socialExtension, listID); err != nil {
		return FailureWithDetails[struct{}]("GIT_ERROR", "Failed to delete list", err)
	}

	return Success(struct{}{})
}

// AddRepositoryToList adds a repository to an existing list. Returns the saved repo ref.
// When allBranches is true, stores branch as "*" to follow all branches.
func AddRepositoryToList(workdir, listID, repoURL, branch string, allBranches bool) Result[string] {
	data, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if data == nil {
		return Failure[string]("LIST_NOT_FOUND", "List '"+listID+"' not found")
	}

	repoURL = protocol.NormalizeURL(repoURL)
	if allBranches {
		branch = "*"
	} else if branch == "" {
		branch = git.GetRemoteDefaultBranch(workdir, repoURL)
	}
	repoRef := repoURL + "#branch:" + branch

	for _, repo := range data.Repositories {
		if repo == repoRef || repo == repoURL {
			return Failure[string]("REPOSITORY_EXISTS", "Repository already in list")
		}
	}

	data.Repositories = append(data.Repositories, repoRef)

	if err := gitmsg.WriteList(workdir, socialExtension, listID, *data); err != nil {
		return FailureWithDetails[string]("GIT_ERROR", "Failed to update list", err)
	}

	// Sync to cache for immediate visibility
	if err := cache.AddRepositoryToList(listID, repoURL, branch); err != nil {
		log.Warn("cache sync for list add failed", "list", listID, "repo", repoURL, "error", err)
	}

	return Success(repoRef)
}

// RemoveRepositoryFromList removes a repository from a list.
func RemoveRepositoryFromList(workdir, listID, repoURL string) Result[struct{}] {
	data, _ := gitmsg.ReadList(workdir, socialExtension, listID)
	if data == nil {
		return Failure[struct{}]("LIST_NOT_FOUND", "List '"+listID+"' not found")
	}

	newRepos := make([]string, 0, len(data.Repositories))
	found := false
	for _, repo := range data.Repositories {
		if repo == repoURL || repo == repoURL+"#branch:main" {
			found = true
			continue
		}
		newRepos = append(newRepos, repo)
	}

	if !found {
		return Failure[struct{}]("REPOSITORY_NOT_FOUND", "Repository not in list")
	}

	data.Repositories = newRepos

	if err := gitmsg.WriteList(workdir, socialExtension, listID, *data); err != nil {
		return FailureWithDetails[struct{}]("GIT_ERROR", "Failed to update list", err)
	}

	return Success(struct{}{})
}

// listDataToList converts gitmsg.ListData to the social List type.
func listDataToList(data gitmsg.ListData) List {
	return List{
		ID:           data.ID,
		Name:         data.Name,
		Version:      data.Version,
		Repositories: data.Repositories,
		Source:       "",
	}
}
