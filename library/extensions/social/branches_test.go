// branches_test.go - The branch of a repository scope filters the posts, and the branch list of the cache counts what the scope shows
package social

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// hashesOf returns the commit hashes of a post result.
func hashesOf(t *testing.T, result Result[[]Post]) map[string]bool {
	t.Helper()
	if !result.Success {
		t.Fatalf("GetPosts() failed: %s", result.Error.Message)
	}
	hashes := make(map[string]bool, len(result.Data))
	for _, p := range result.Data {
		hashes[p.Display.CommitHash] = true
	}
	return hashes
}

// TestGetPosts_workspaceBranchScope checks that repository:workspace@<branch> reads that branch of the workspace only, and that the scope with no branch reads every branch.
func TestGetPosts_workspaceBranchScope(t *testing.T) {
	t.Parallel()
	workdir := cloneFixture(t)
	post := CreatePost(workdir, "On the social branch", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	onMain, err := git.CreateCommit(workdir, git.CommitOptions{Message: "A plain commit", AllowEmpty: true})
	if err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	fullSync(t, workdir)
	postHash := protocol.ParseRef(post.Data.ID).Value

	all := hashesOf(t, GetPosts(workdir, "repository:workspace", nil))
	if !all[postHash] || !all[onMain] {
		t.Errorf("the scope with no branch misses a post: social=%v main=%v", all[postHash], all[onMain])
	}
	social := hashesOf(t, GetPosts(workdir, "repository:workspace@gitmsg/social", nil))
	if !social[postHash] || social[onMain] {
		t.Errorf("the social branch scope: social=%v main=%v, want the social post only", social[postHash], social[onMain])
	}
	main := hashesOf(t, GetPosts(workdir, "repository:my@main", nil))
	if main[postHash] || !main[onMain] {
		t.Errorf("the main scope: social=%v main=%v, want the main commit only", main[postHash], main[onMain])
	}
	if n := CountRepository(workdir, "", "gitmsg/social", true); n != len(social) {
		t.Errorf("CountRepository(gitmsg/social) = %d, want %d", n, len(social))
	}
	if n := CountRepository(workdir, "", "", true); n != len(all) {
		t.Errorf("CountRepository() = %d, want %d", n, len(all))
	}
}

// TestGetPosts_remoteBranchScopeHidesOtherBranches checks that a remote scope with a branch leaves the gitmsg/social posts of the remote out, which the Repository view avoids by sending no branch.
func TestGetPosts_remoteBranchScopeHidesOtherBranches(t *testing.T) {
	t.Parallel()
	repo := "https://example.com/followed/" + t.Name()
	if err := cache.InsertCommits([]cache.Commit{
		{Hash: "main00000001", RepoURL: repo, Branch: "main", Message: "Add the index", Timestamp: time.Now().UTC()},
		{Hash: "post00000001", RepoURL: repo, Branch: "gitmsg/social", Message: "Hello\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	workdir := cloneFixture(t)
	onMain := hashesOf(t, GetPosts(workdir, "repository:"+repo+"@main", nil))
	if onMain["post00000001"] || !onMain["main00000001"] {
		t.Errorf("the main scope: social=%v main=%v, want main only", onMain["post00000001"], onMain["main00000001"])
	}
	all := hashesOf(t, GetPosts(workdir, "repository:"+repo, nil))
	if !all["post00000001"] || !all["main00000001"] {
		t.Errorf("the scope with no branch: social=%v main=%v, want both", all["post00000001"], all["main00000001"])
	}
}

// TestGetRepositoryBranches checks that each branch count equals the count of the Repository view on that branch: the default branch holds each commit it reaches, and a feature branch only its own.
func TestGetRepositoryBranches(t *testing.T) {
	t.Parallel()
	workdir := cloneFixture(t)
	if post := CreatePost(workdir, "Hello", nil); !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "On main", AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	if _, err := git.ExecGit(workdir, []string{"checkout", "-q", "-b", "feature/x"}); err != nil {
		t.Fatalf("checkout feature/x: %v", err)
	}
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "On the feature", AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	if _, err := git.ExecGit(workdir, []string{"checkout", "-q", "main"}); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	fullSync(t, workdir)

	branches, err := cache.GetRepositoryBranches(gitmsg.ResolveRepoURL(workdir))
	if err != nil {
		t.Fatalf("GetRepositoryBranches() error = %v", err)
	}
	counts := make(map[string]int, len(branches))
	total := 0
	for _, b := range branches {
		counts[b.Name] = b.Commits
		total += b.Commits
		if n := CountRepository(workdir, "", b.Name, true); n != b.Commits {
			t.Errorf("%s: branch count %d, the Repository view counts %d", b.Name, b.Commits, n)
		}
	}
	if counts["main"] != 2 || counts["feature/x"] != 1 || counts["gitmsg/social"] != 1 {
		t.Errorf("counts = %v, want main 2, feature/x 1, gitmsg/social 1", counts)
	}
	if n := CountRepository(workdir, "", "", true); n != total {
		t.Errorf("CountRepository() = %d, the branch counts sum to %d", n, total)
	}
}
