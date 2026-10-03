// branch_test.go - Edits and retractions land on the workspace's social branch
package social

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
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

// blogWorkspace initializes a workspace whose social branch is main, holding the given committed files.
func blogWorkspace(t *testing.T, files []string) string {
	t.Helper()
	workdir := cloneFixture(t)
	if err := gitmsg.WriteExtConfig(workdir, "social", map[string]interface{}{"branch": "main"}); err != nil {
		t.Fatalf("WriteExtConfig() error = %v", err)
	}
	for _, name := range files {
		path := filepath.Join(workdir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Add the blog"}); err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	return workdir
}

// assertBlogTree checks that main still holds the files and the checkout is clean.
func assertBlogTree(t *testing.T, workdir, step string, files []string) {
	t.Helper()
	tree, err := git.ExecGit(workdir, []string{"ls-tree", "-r", "--name-only", "main"})
	if err != nil {
		t.Fatalf("ls-tree after the %s error = %v", step, err)
	}
	if got := strings.Fields(tree.Stdout); len(got) != len(files) {
		t.Errorf("main holds %v after the %s, want %v", got, step, files)
	}
	status, err := git.ExecGit(workdir, []string{"status", "--porcelain"})
	if err != nil || status.Stdout != "" {
		t.Errorf("git status after the %s = %q (%v), want clean", step, status.Stdout, err)
	}
}

// TestBlogBranchKeepsItsFiles checks that a message commit carries the branch tip's tree.
func TestBlogBranchKeepsItsFiles(t *testing.T) {
	t.Parallel()
	files := []string{"README.md", "posts/first.md"}
	workdir := blogWorkspace(t, files)

	post := CreatePost(workdir, "Hello from the blog", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	assertBlogTree(t, workdir, "post", files)

	edit := EditPost(workdir, post.Data.ID, "Hello from the blog, revised", nil)
	if !edit.Success {
		t.Fatalf("EditPost() failed: %s", edit.Error.Message)
	}
	assertBlogTree(t, workdir, "edit", files)

	if r := RetractPost(workdir, post.Data.ID); !r.Success {
		t.Fatalf("RetractPost() failed: %s", r.Error.Message)
	}
	assertBlogTree(t, workdir, "retraction", files)
}

// socialBranchWorkspace returns a workspace whose social branch is the code branch feature/social, started from main.
func socialBranchWorkspace(t *testing.T) string {
	t.Helper()
	workdir := cloneFixture(t)
	if _, err := git.ExecGit(workdir, []string{"branch", "feature/social", "main"}); err != nil {
		t.Fatalf("create feature/social: %v", err)
	}
	if err := gitmsg.WriteExtConfig(workdir, "social", map[string]interface{}{"branch": "feature/social"}); err != nil {
		t.Fatalf("WriteExtConfig() error = %v", err)
	}
	return workdir
}

// fullSync runs the workspace sync with this extension's batch function and fails the test on an error.
func fullSync(t *testing.T, workdir string) {
	t.Helper()
	if err := fetch.SyncWorkspace(workdir, []fetch.WorkspaceSyncFunc{SyncWorkspaceBatch}); err != nil {
		t.Fatalf("SyncWorkspace() error = %v", err)
	}
}

// TestSyncWorkspace_editSurvivesMerge checks that a post edited on a code social branch shows the edited text on its live row after the merge, in the long-lived cache and in a rebuild.
func TestSyncWorkspace_editSurvivesMerge(t *testing.T) {
	t.Parallel()
	workdir := socialBranchWorkspace(t)
	post := CreatePost(workdir, "Before the edit", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	fullSync(t, workdir)
	if edit := EditPost(workdir, post.Data.ID, "After the edit", nil); !edit.Success {
		t.Fatalf("EditPost() failed: %s", edit.Error.Message)
	}
	fullSync(t, workdir)
	if _, err := git.ExecGit(workdir, []string{"merge", "-q", "--no-ff", "-m", "Merge the social branch", "feature/social"}); err != nil {
		t.Fatalf("merge feature/social: %v", err)
	}
	fullSync(t, workdir)

	hash := protocol.ParseRef(post.Data.ID).Value
	check := func(label, dir string) {
		t.Helper()
		item, err := GetSocialItemByRef("#commit:"+hash, gitmsg.ResolveRepoURL(dir))
		if err != nil {
			t.Fatalf("%s: GetSocialItemByRef() error = %v", label, err)
		}
		if item.Branch != "main" || item.IsStale || !item.IsEdited || item.Content != "After the edit" {
			t.Errorf("%s: post on %s stale=%v edited=%v content=%q, want the edited text on the live row under main",
				label, item.Branch, item.IsStale, item.IsEdited, item.Content)
		}
	}
	check("long-lived cache", workdir)

	rebuilt := testutil.CopyRepo(t, workdir)
	if root, err := git.GetRootDir(rebuilt); err == nil && root != "" {
		rebuilt = root
	}
	fullSync(t, rebuilt)
	check("rebuild", rebuilt)
}

// TestEditPost_editTargetAfterMergeStaysOffMain checks that an edit or a retraction aimed at an edit's ID, after the social branch merged, never writes on main.
func TestEditPost_editTargetAfterMergeStaysOffMain(t *testing.T) {
	t.Parallel()
	workdir := socialBranchWorkspace(t)
	post := CreatePost(workdir, "Before the edit", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	fullSync(t, workdir)
	edit := EditPost(workdir, post.Data.ID, "After the edit", nil)
	if !edit.Success {
		t.Fatalf("EditPost() failed: %s", edit.Error.Message)
	}
	fullSync(t, workdir)
	if _, err := git.ExecGit(workdir, []string{"merge", "-q", "--no-ff", "-m", "Merge the social branch", "feature/social"}); err != nil {
		t.Fatalf("merge feature/social: %v", err)
	}
	fullSync(t, workdir)

	onMain := len(commitsOn(t, workdir, "main"))
	if again := EditPost(workdir, edit.Data.ID, "Edited again", nil); again.Success && !commitsOn(t, workdir, "feature/social")[protocol.ParseRef(again.Data.ID).Value] {
		t.Errorf("edit of an edit's ID on %s, want feature/social", again.Data.Branch)
	}
	_ = RetractPost(workdir, edit.Data.ID)
	if after := len(commitsOn(t, workdir, "main")); after != onMain {
		t.Errorf("main holds %d commits after the edit and the retraction, want %d", after, onMain)
	}
}
