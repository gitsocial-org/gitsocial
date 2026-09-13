// methods_core_test.go - Tests for the core.* methods served over JSON-RPC
package rpc

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// divergedWorkdir returns a repository whose gitmsg/social branch is one commit
// ahead of and one commit behind the same branch on its bare origin.
func divergedWorkdir(t *testing.T) string {
	t.Helper()
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(template) })
	workdir := testutil.CopyRepo(t, template)
	origin := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := git.ExecGit(dir, args); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	commit := func(message string) {
		t.Helper()
		if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: message, AllowEmpty: true}); err != nil {
			t.Fatalf("CreateCommit(%q) error = %v", message, err)
		}
	}
	run(origin, "init", "--bare", "-b", "main")
	run(workdir, "remote", "add", "origin", origin)
	run(workdir, "checkout", "-b", "gitmsg/social")
	commit("social: shared base")
	run(workdir, "push", "origin", "gitmsg/social")
	commit("social: published")
	run(workdir, "push", "origin", "gitmsg/social")
	run(workdir, "reset", "--hard", "HEAD~1")
	commit("social: rewritten locally")
	return workdir
}

// twoRemoteWorkdir returns a repo with a post and two bare remotes, both configured.
func twoRemoteWorkdir(t *testing.T) (workdir string, remotes [2]string) {
	t.Helper()
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(template) })
	workdir = testutil.CopyRepo(t, template)
	for i, name := range []string{"first", "second"} {
		remotes[i] = t.TempDir()
		if err := git.EnsureBareRepo(remotes[i]); err != nil {
			t.Fatalf("EnsureBareRepo: %v", err)
		}
		if _, err := git.ExecGit(workdir, []string{"remote", "add", name, remotes[i]}); err != nil {
			t.Fatalf("remote add %s: %v", name, err)
		}
		if _, err := git.ExecGit(workdir, []string{"push", name, "main"}); err != nil {
			t.Fatalf("push main to %s: %v", name, err)
		}
	}
	if err := git.SetConfiguredPushRemotes(workdir, []string{"first", "second"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(workdir, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("CreateCommitOnBranch: %v", err)
	}
	return workdir, remotes
}

// pushServer returns a server initialized on workdir.
func pushServer(t *testing.T, workdir string) *Server {
	t.Helper()
	server := NewServer(NewRegistry(), strings.NewReader(""), io.Discard)
	RegisterCoreMethods(server, "test")
	server.session.Workdir = workdir
	server.session.Initialized = true
	return server
}

// TestCorePush_dryRunReachesEveryConfiguredRemote: the CLI's list, and a dry run sends nothing.
func TestCorePush_dryRunReachesEveryConfiguredRemote(t *testing.T) {
	testutil.OpenTempCache(t, "")
	workdir, remotes := twoRemoteWorkdir(t)
	server := pushServer(t, workdir)

	resp := server.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "core.push",
		Params:  json.RawMessage(`{"dryRun": true}`),
	})
	if resp.Error != nil {
		t.Fatalf("core.push error = %v", resp.Error)
	}
	encoded, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var served []struct {
		Push struct {
			Remote string `json:"remote"`
		} `json:"push"`
	}
	if err := json.Unmarshal(encoded, &served); err != nil {
		t.Fatalf("unmarshal result as an array: %v (%s)", err, encoded)
	}
	if len(served) != 2 {
		t.Fatalf("core.push returned %d results, want one per configured remote", len(served))
	}
	if served[0].Push.Remote != "first" || served[1].Push.Remote != "second" {
		t.Errorf("results name %q and %q, want first and second in config order", served[0].Push.Remote, served[1].Push.Remote)
	}
	for _, remote := range remotes {
		out, err := git.ExecGit(remote, []string{"branch", "--list", "gitmsg/social"})
		if err != nil {
			t.Fatalf("branch --list: %v", err)
		}
		if strings.TrimSpace(out.Stdout) != "" {
			t.Errorf("a dry run sent the post to %s: %q", remote, out.Stdout)
		}
	}
}

// TestCorePush_siteOnlyOnNonS3Fails: the sequence's own error reaches the caller.
func TestCorePush_siteOnlyOnNonS3Fails(t *testing.T) {
	testutil.OpenTempCache(t, "")
	workdir, _ := twoRemoteWorkdir(t)
	server := pushServer(t, workdir)

	resp := server.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "core.push",
		Params:  json.RawMessage(`{"remote": "first", "siteOnly": true}`),
	})
	if resp.Error == nil {
		t.Fatalf("core.push --site-only on a non-s3 remote should fail, got %v", resp.Result)
	}
	if !strings.Contains(resp.Error.Message, "is not an s3 remote") {
		t.Errorf("error message = %q, want the sequence's non-s3 message", resp.Error.Message)
	}
}

// TestCoreGetNotifications_carriesEverySource checks the RPC round trip serves
// the notification set the library reports, the divergence source included.
func TestCoreGetNotifications_carriesEverySource(t *testing.T) {
	testutil.OpenTempCache(t, "")
	workdir := divergedWorkdir(t)

	server := NewServer(NewRegistry(), strings.NewReader(""), io.Discard)
	RegisterCoreMethods(server, "test")
	server.session.Workdir = workdir
	server.session.Initialized = true

	resp := server.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "core.getNotifications",
	})
	if resp.Error != nil {
		t.Fatalf("core.getNotifications error = %v", resp.Error)
	}
	encoded, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var served []struct {
		Source string
		Type   string
	}
	if err := json.Unmarshal(encoded, &served); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	direct, err := notifications.GetAll(workdir, notifications.Filter{})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if len(served) != len(direct) {
		t.Fatalf("round trip served %d notifications, GetAll reports %d", len(served), len(direct))
	}
	for i, n := range direct {
		if served[i].Source != n.Source {
			t.Errorf("notification %d source = %q, want %q", i, served[i].Source, n.Source)
		}
	}
	found := false
	for _, n := range served {
		if n.Source == "gitmsg-divergence" && n.Type == "branch-diverged" {
			found = true
		}
	}
	if !found {
		t.Error("round trip served no gitmsg-divergence notification for a diverged branch")
	}
}
