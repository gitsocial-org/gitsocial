// fetch.go - PM extension fetch wrapper over core/fetch
package pm

import (
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
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

// BackfillSpec describes how the post-fetch backfill detects PM commits
// whose pm_items row is missing.
func BackfillSpec() fetch.ExtBackfillSpec {
	return fetch.ExtBackfillSpec{Extension: "pm", ItemsTable: "pm_items"}
}
