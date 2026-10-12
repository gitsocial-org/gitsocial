// fork_pulls.go - The open pull requests of registered forks: their heads for the fork fetch and the site seam
package review

import (
	"fmt"
	"slices"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/site"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
)

// openForkPulls returns the open pull requests of the registered forks whose base is this repository, the set pr list shows.
func openForkPulls(workdir string) ([]PullRequest, error) {
	wsURL := gitmsg.ResolveRepoURL(workdir)
	forks := gitmsg.GetForks(workdir)
	if wsURL == "" || len(forks) == 0 {
		return nil, nil
	}
	res := GetPullRequestsWithForks(wsURL, gitmsg.GetExtBranch(workdir, "review"), forks, []string{string(PRStateOpen)}, "", 0)
	if !res.Success {
		return nil, fmt.Errorf("list fork pull requests: %s", res.Error.Message)
	}
	pulls := make([]PullRequest, 0, len(res.Data))
	for _, pr := range res.Data {
		if pr.Repository != wsURL {
			pulls = append(pulls, pr)
		}
	}
	return pulls, nil
}

// forkHeadBranch returns the head branch of a fork pull request when the head is on the fork itself, "" otherwise.
func forkHeadBranch(pr PullRequest) string {
	parsed := protocol.ParseRef(pr.Head)
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" || (parsed.Repository != "" && parsed.Repository != pr.Repository) {
		return ""
	}
	return parsed.Value
}

// forkHeadRef is the ref a fetched fork head is at in the fork repository.
func forkHeadRef(forkURL, branch string) string {
	return fetch.ForkCodeRefPrefix + fetch.URLHash(forkURL) + "/" + branch
}

// forkHeadSHA returns the full sha of a fetched fork head, "" when the fork repository does not have it.
func forkHeadSHA(forkDir, forkURL, branch string) string {
	out, err := git.ExecGit(forkDir, []string{"rev-parse", "--verify", "--quiet", forkHeadRef(forkURL, branch)})
	if err != nil {
		return ""
	}
	return out.Stdout
}

// OpenForkHeads returns the head branches of the open fork pull requests per fork, the code branches the fork fetch adds.
func OpenForkHeads(workdir string) map[string][]string {
	pulls, err := openForkPulls(workdir)
	if err != nil {
		log.Debug("open fork heads", "error", err)
		return nil
	}
	heads := map[string][]string{}
	for _, pr := range pulls {
		if branch := forkHeadBranch(pr); branch != "" && !slices.Contains(heads[pr.Repository], branch) {
			heads[pr.Repository] = append(heads[pr.Repository], branch)
		}
	}
	return heads
}

// FetchMissingForkHeads fetches the head of each open fork pull request that the fork repository does not have yet, one fetch per fork.
func FetchMissingForkHeads(workdir, cacheDir string) {
	pulls, err := openForkPulls(workdir)
	if err != nil || len(pulls) == 0 {
		return
	}
	forkDir, err := storage.EnsureForkRepository(cacheDir, gitmsg.ResolveRepoURL(workdir))
	if err != nil {
		log.Debug("fork repository", "error", err)
		return
	}
	// The fork repository borrows the workspace objects, so the fetch brings only the commits the workspace lacks.
	if err := storage.SetAlternate(forkDir, workdir); err != nil {
		log.Debug("fork repo alternate failed", "dir", forkDir, "error", err)
	}
	addresses := gitmsg.ForkAddresses(workdir)
	missing := map[string][]string{}
	for _, pr := range pulls {
		branch := forkHeadBranch(pr)
		if branch == "" || slices.Contains(missing[pr.Repository], branch) || forkHeadSHA(forkDir, pr.Repository, branch) != "" {
			continue
		}
		missing[pr.Repository] = append(missing[pr.Repository], branch)
	}
	for forkURL, branches := range missing {
		if err := fetch.FetchForkBranches(forkDir, forkURL, fetch.ForkAddress(addresses, forkURL), branches); err != nil {
			log.Debug("fetch fork heads", "fork", forkURL, "error", err)
		}
	}
}

// OpenForkPulls returns the open fork pull requests for the site: the fork, the fork repository, the fetched head and the review commits in version order.
func OpenForkPulls(workdir, cacheDir string) ([]site.ForkPull, error) {
	pulls, err := openForkPulls(workdir)
	if err != nil || len(pulls) == 0 {
		return nil, err
	}
	wsURL := gitmsg.ResolveRepoURL(workdir)
	forkDir, err := storage.EnsureForkRepository(cacheDir, wsURL)
	if err != nil {
		return nil, fmt.Errorf("fork repository: %w", err)
	}
	out := make([]site.ForkPull, 0, len(pulls))
	for _, pr := range pulls {
		versions := GetPRVersions(pr.ID, wsURL)
		if !versions.Success {
			return nil, fmt.Errorf("versions of %s: %s", pr.ID, versions.Error.Message)
		}
		commits := make([]string, len(versions.Data))
		for i, v := range versions.Data {
			commits[i] = v.CommitHash
		}
		head := ""
		if branch := forkHeadBranch(pr); branch != "" {
			head = forkHeadSHA(forkDir, pr.Repository, branch)
		}
		out = append(out, site.ForkPull{Repo: pr.Repository, Dir: forkDir, Head: head, Commits: commits})
	}
	return out, nil
}
