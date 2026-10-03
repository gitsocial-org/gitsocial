// view_repository_test.go - The repository view reports a failed load or month fetch, and reads the branch of its location
package tuisocial

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestMain initializes the bubblezone global manager the card list requires.
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

// TestRepositoryFailuresSurface checks that a failed post load and a failed month fetch both reach the status bar.
func TestRepositoryFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"load", repositoryLoadedMsg{Err: errors.New("get posts: cache closed")}, "get posts: cache closed"},
		{"fetch", repositoryFetchedMsg{Err: errors.New("fetch range: host unreachable")}, "fetch range: host unreachable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newRepositoryView(t.TempDir())
			state := &tuicore.State{}
			view.Update(tt.msg, state)
			if state.Message != tt.want {
				t.Errorf("Message = %q, want %q", state.Message, tt.want)
			}
			if state.MessageType != tuicore.MessageTypeError {
				t.Errorf("MessageType = %v, want MessageTypeError", state.MessageType)
			}
		})
	}
}

// seedBranches fills a fresh cache with one commit of repo on each of main, gitmsg/social and gitmsg/pm, and follows the repo on main from workdir.
func seedBranches(t *testing.T, workdir, repo string) {
	t.Helper()
	testutil.OpenTempCache(t, "")
	if err := cache.InsertCommits([]cache.Commit{
		{Hash: "main00000001", RepoURL: repo, Branch: "main", Message: "Add the index", Timestamp: time.Now().UTC()},
		{Hash: "post00000001", RepoURL: repo, Branch: "gitmsg/social", Message: "Hello\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: time.Now().UTC()},
		{Hash: "issue0000001", RepoURL: repo, Branch: "gitmsg/pm", Message: "Fix the index\n\nGitMsg: ext=\"pm\"; type=\"issue\"; v=\"0.1.0\"", Timestamp: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := cache.InsertList(cache.CachedList{ID: "following", Name: "Following", Workdir: workdir,
		Repositories: []cache.ListRepository{{ListID: "following", RepoURL: repo, Branch: "main"}}}); err != nil {
		t.Fatalf("InsertList() error = %v", err)
	}
}

// runRepositoryCmd runs a command tree and feeds each message it yields into the view.
func runRepositoryCmd(view *repositoryView, state *tuicore.State, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runRepositoryCmd(view, state, c)
		}
		return
	}
	runRepositoryCmd(view, state, view.Update(msg, state))
}

// loadedHashes returns the commit hashes the view shows after it activates at loc.
func loadedHashes(view *repositoryView, workdir string, loc tuicore.Location) map[string]bool {
	state := &tuicore.State{Workdir: workdir, Router: tuicore.NewRouter(loc)}
	runRepositoryCmd(view, state, view.Activate(state))
	hashes := make(map[string]bool)
	for _, item := range view.cardlist.Items() {
		id := item.ItemID()
		if i := strings.Index(id, "#commit:"); i >= 0 {
			hashes[strings.SplitN(id[i+len("#commit:"):], "@", 2)[0]] = true
		}
	}
	return hashes
}

// TestRepositoryBranchParam checks that a location with no branch shows every branch, the gitmsg/social posts of a remote followed on main included, and that a link with a branch keeps it.
func TestRepositoryBranchParam(t *testing.T) {
	remote := "https://github.com/user/followed"
	tests := []struct {
		name       string
		url        string
		branch     string
		wantBranch string
		want       []string
		wantNot    []string
	}{
		{"remote with no branch", remote, "", allBranches, []string{"main00000001", "post00000001", "issue0000001"}, nil},
		{"issue link keeps gitmsg/pm", remote, "gitmsg/pm", "gitmsg/pm", []string{"issue0000001"}, []string{"main00000001", "post00000001"}},
		{"workspace with no branch", "", "", allBranches, []string{"main00000001", "post00000001", "issue0000001"}, nil},
		{"workspace on gitmsg/social", "", "gitmsg/social", "gitmsg/social", []string{"post00000001"}, []string{"main00000001", "issue0000001"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workdir := t.TempDir()
			repo := tt.url
			if repo == "" {
				repo = gitmsg.ResolveRepoURL(workdir)
			}
			seedBranches(t, workdir, repo)
			view := newRepositoryView(workdir)
			hashes := loadedHashes(view, workdir, tuicore.LocRepository(tt.url, tt.branch))
			if view.branch != tt.wantBranch {
				t.Errorf("branch = %q, want %q", view.branch, tt.wantBranch)
			}
			for _, h := range tt.want {
				if !hashes[h] {
					t.Errorf("the view lacks %s; shows %v", h, hashes)
				}
			}
			for _, h := range tt.wantNot {
				if hashes[h] {
					t.Errorf("the view shows %s; shows %v", h, hashes)
				}
			}
			if total := view.pag.Total(len(hashes)); total != len(tt.want) {
				t.Errorf("total = %d, want %d", total, len(tt.want))
			}
		})
	}
}

// TestRepositoryTitleBranch checks that the title names the branch, or all branches.
func TestRepositoryTitleBranch(t *testing.T) {
	view := newRepositoryView(t.TempDir())
	view.name = "user/repo"
	view.url = "https://github.com/user/repo"
	view.branch = allBranches
	if title := view.Title(); !strings.Contains(title, "all branches") {
		t.Errorf("title on every branch = %q, want all branches", title)
	}
	view.branch = "gitmsg/pm"
	if title := view.Title(); !strings.Contains(title, "#branch:gitmsg/pm") || strings.Contains(title, "all branches") {
		t.Errorf("title on a branch = %q, want the branch ref", title)
	}
}
