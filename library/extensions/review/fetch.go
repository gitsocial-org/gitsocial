// fetch.go - Review extension fetch wrapper over core/fetch
package review

import (
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/log"
)

// FetchRepository fetches review data from a remote repository.
func FetchRepository(cacheDir, repoURL, branch string) fetch.Result {
	if branch == "" {
		branch = "gitmsg/review"
	}
	return fetch.FetchRepository(cacheDir, repoURL, branch, "", Processors(), nil)
}

// Processors returns the commit processors for the review extension.
func Processors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processReviewCommit}
}

// PostFetchHooks returns post-fetch hooks for the review extension.
// RefreshOpenPRBranches walks PRs across the workspace and registered
// forks together; the hook fires once per fetched repo but the refresh
// itself dedupes by (repo_url, branch), so concurrent fork fetches
// converge on the same observation rows without redundant work.
func PostFetchHooks() []fetch.PostFetchHook {
	return []fetch.PostFetchHook{refreshBranchObservationsHook}
}

func refreshBranchObservationsHook(workdir, _, _, _ string) {
	if err := RefreshOpenPRBranches(workdir); err != nil {
		log.Debug("refresh branch observations", "error", err)
	}
}
