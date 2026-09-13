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
