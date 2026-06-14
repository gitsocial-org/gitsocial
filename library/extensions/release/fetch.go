// fetch.go - Release extension fetch wrapper over core/fetch
package release

import (
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
)

// FetchRepository fetches release data from a remote repository.
func FetchRepository(cacheDir, repoURL, branch string) fetch.Result {
	if branch == "" {
		branch = "gitmsg/release"
	}
	return fetch.FetchRepository(cacheDir, repoURL, branch, "", Processors(), nil)
}

// Processors returns the commit processors for the release extension.
func Processors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processReleaseCommit}
}

// BackfillSpec describes how the post-fetch backfill detects release commits
// whose release_items row is missing.
func BackfillSpec() fetch.ExtBackfillSpec {
	return fetch.ExtBackfillSpec{Extension: "release", ItemsTable: "release_items"}
}
