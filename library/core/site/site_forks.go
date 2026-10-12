// site_forks.go - the fork pull request area: the seam the clients fill from the review extension
package site

// ForkPull is one open pull request of a registered fork: the fork URL, the fork bare repository, the head sha or "", the review commit shas.
type ForkPull struct {
	Repo, Dir, Head string
	Commits         []string
}

// ForkPullSource returns the open fork pull requests of the workspace; a nil source has none.
type ForkPullSource func() ([]ForkPull, error)
