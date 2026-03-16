// fetch.go - PM extension fetch wrapper over core/fetch
package pm

import (
	"github.com/gitsocial-org/gitsocial/core/fetch"
)

// FetchRepository fetches PM data from a remote repository.
func FetchRepository(cacheDir, repoURL, branch string) fetch.Result {
	if branch == "" {
		branch = "gitmsg/pm"
	}
	return fetch.FetchRepository(cacheDir, repoURL, branch, "", Processors(), nil)
}

// Processors returns the commit processors for the PM extension.
func Processors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processPMCommit}
}
