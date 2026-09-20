// branch_test.go - Edits and retractions land on the workspace's social branch
package social

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// commitsOn returns the hashes git lists on a branch.
func commitsOn(t *testing.T, workdir, branch string) map[string]bool {
	t.Helper()
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{Branch: branch})
	if err != nil {
		t.Fatalf("GetCommits(%s) error = %v", branch, err)
	}
	hashes := make(map[string]bool, len(commits))
	for _, c := range commits {
		hashes[c.Hash] = true
	}
	return hashes
}

// TestEditOnSocialBranch checks GITMSG.md 1.5: an edit lands on the branch of the post it edits.
func TestEditOnSocialBranch(t *testing.T) {
	t.Parallel()

	// A blog workspace writes its social branch, main, for posts, edits and retractions.
	t.Run("blogWorkspaceOnMain", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		if err := gitmsg.WriteExtConfig(workdir, "social", map[string]interface{}{"branch": "main"}); err != nil {
			t.Fatalf("WriteExtConfig() error = %v", err)
		}
		file := filepath.Join(workdir, "index.md")
		if err := os.WriteFile(file, []byte("the blog\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Add the index"}); err != nil {
			t.Fatalf("CreateCommit() error = %v", err)
		}

		post := CreatePost(workdir, "Hello from the blog", nil)
		if !post.Success {
			t.Fatalf("CreatePost() failed: %s", post.Error.Message)
		}
		if post.Data.Branch != "main" {
			t.Errorf("post branch = %q, want main", post.Data.Branch)
		}

		edit := EditPost(workdir, post.Data.ID, "Hello from the blog, revised", nil)
		if !edit.Success {
			t.Fatalf("EditPost() failed: %s", edit.Error.Message)
		}
		if edit.Data.Branch != "main" {
			t.Errorf("edit branch = %q, want main", edit.Data.Branch)
		}
		onMain := commitsOn(t, workdir, "main")
		if !onMain[protocol.ParseRef(post.Data.ID).Value] || !onMain[protocol.ParseRef(edit.Data.ID).Value] {
			t.Error("the post and its edit are not both on main")
		}

		before := len(onMain)
		if r := RetractPost(workdir, post.Data.ID); !r.Success {
			t.Fatalf("RetractPost() failed: %s", r.Error.Message)
		}
		if after := len(commitsOn(t, workdir, "main")); after != before+1 {
			t.Errorf("main holds %d commits after the retraction, want %d", after, before+1)
		}

		if _, err := os.Stat(file); err != nil {
			t.Errorf("the working tree lost %s: %v", file, err)
		}
		diff, err := git.ExecGit(workdir, []string{"diff", "--name-only"})
		if err != nil || diff.Stdout != "" {
			t.Errorf("the working tree differs from the index: %q (%v)", diff.Stdout, err)
		}
	})

	// A default workspace writes gitmsg/social, so a commit on main is not its to edit.
	t.Run("defaultWorkspaceRefusesMain", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		hash, err := git.CreateCommit(workdir, git.CommitOptions{Message: "A plain commit", AllowEmpty: true})
		if err != nil {
			t.Fatalf("CreateCommit() error = %v", err)
		}
		if err := syncWorkspace(workdir); err != nil {
			t.Fatalf("syncWorkspace() error = %v", err)
		}

		ref := protocol.CreateRef(protocol.RefTypeCommit, hash, gitmsg.ResolveRepoURL(workdir), "main")
		result := EditPost(workdir, ref, "Rewritten", nil)
		if result.Success {
			t.Fatal("EditPost() should refuse a commit that is not on the social branch")
		}
		if result.Error.Code != "INVALID_TARGET" {
			t.Errorf("error code = %q, want INVALID_TARGET", result.Error.Code)
		}
	})
}
