// fetch.go - Review extension fetch wrapper over core/fetch
package review

import (
	"github.com/gitsocial-org/gitsocial/core/fetch"
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
