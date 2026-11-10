// desktop_test.go - Tests for the new unread items of a fetch and the setting that turns desktop messages off.
package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/core/site"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// isolateDesktopSetting points the personal repository at a temp dir and clears the variable.
func isolateDesktopSetting(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(t.TempDir(), "personal"))
	t.Setenv(desktopEnv, "")
}

// recordSends returns a sender that records the body of each message, and the list it fills.
func recordSends() (func([]notifications.DesktopMessage) error, *[]string) {
	var sent []string
	return func(messages []notifications.DesktopMessage) error {
		for _, m := range messages {
			sent = append(sent, m.Body)
		}
		return nil
	}, &sent
}

// failingProviderMarker is the file in the git dir of a workspace that makes the test provider fail.
const failingProviderMarker = "fail-notifications"

// failingProvider fails for a workspace that has the marker file.
type failingProvider struct{}

// init registers the provider that TestFetch_snapshotErrorSendsNothing turns on with the marker.
func init() {
	notifications.RegisterProvider("test-failing", failingProvider{})
}

// GetNotifications fails when the workspace has the marker file.
func (failingProvider) GetNotifications(workdir string, _ notifications.Filter) ([]notifications.Notification, error) {
	if _, err := os.Stat(filepath.Join(workdir, ".git", failingProviderMarker)); err == nil {
		return nil, errors.New("provider unavailable")
	}
	return nil, nil
}

// GetUnreadCount returns no count.
func (failingProvider) GetUnreadCount(string) (int, error) {
	return 0, nil
}

// mentionWorkspace returns a workspace whose registered fork mentions the workspace user.
func mentionWorkspace(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("real git")
	}
	testutil.OpenTempCache(t, "")
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	workdir := testutil.CopyRepo(t, template)
	forkDir := t.TempDir()
	if _, err := git.ExecGit(forkDir, []string{"init", "--bare"}); err != nil {
		t.Fatalf("init fork: %v", err)
	}
	for _, cfg := range [][]string{{"user.name", "Ada"}, {"user.email", "ada@example.com"}} {
		if _, err := git.ExecGit(forkDir, []string{"config", cfg[0], cfg[1]}); err != nil {
			t.Fatalf("configure fork: %v", err)
		}
	}
	message := "ping @" + git.GetUserEmail(workdir) + "\n\n" + `GitMsg: ext="social"; type="post"; v="0.1.0"`
	if _, err := git.CreateCommitOnBranch(forkDir, "gitmsg/social", message); err != nil {
		t.Fatalf("create fork post: %v", err)
	}
	if err := gitmsg.AddFork(workdir, forkDir); err != nil {
		t.Fatalf("add fork: %v", err)
	}
	if before, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}); len(before) != 0 {
		t.Fatalf("unread before the fetch = %d, want 0", len(before))
	}
	return workdir
}

// fetchNewUnread runs the fetch sequence and returns its new unread items.
func fetchNewUnread(t *testing.T, workdir string) []notifications.Notification {
	t.Helper()
	_, _, newUnread := Fetch(workdir, t.TempDir(), FetchOptions{})
	return newUnread
}

func TestDesktopEnabled_envOverridesKey(t *testing.T) {
	isolateDesktopSetting(t)
	if !DesktopEnabled() {
		t.Error("DesktopEnabled() = false with no key and no variable, want the default of on")
	}
	if err := settings.NewManager().Write("notifications.desktop", "false"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if DesktopEnabled() {
		t.Error("DesktopEnabled() = true with the key off")
	}
	t.Setenv(desktopEnv, "true")
	if !DesktopEnabled() {
		t.Error("DesktopEnabled() = false with the key off and the variable on, want the variable to win")
	}
	if err := settings.NewManager().Write("notifications.desktop", "true"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Setenv(desktopEnv, "false")
	if DesktopEnabled() {
		t.Error("DesktopEnabled() = true with the key on and the variable off, want the variable to win")
	}
}

func TestFetch_newUnreadInResult(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := mentionWorkspace(t)
	newUnread := fetchNewUnread(t, workdir)
	if len(newUnread) != 1 || newUnread[0].Type != "mention" || newUnread[0].Actor.Name != "Ada" {
		t.Fatalf("new unread = %+v, want the one mention by Ada", newUnread)
	}
	send, sent := recordSends()
	notifyNew(workdir, t.TempDir(), "", newUnread, send, http.DefaultClient)
	if len(*sent) != 1 {
		t.Errorf("sent %q with the setting on, want one message", *sent)
	}
}

func TestFetch_desktopOffSendsNothing(t *testing.T) {
	isolateDesktopSetting(t)
	if err := settings.NewManager().Write("notifications.desktop", "false"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	send, sent := recordSends()
	workdir := mentionWorkspace(t)
	newUnread := fetchNewUnread(t, workdir)
	if len(newUnread) != 0 {
		t.Errorf("the result carries %d items with the setting off, want none because no snapshot runs", len(newUnread))
	}
	if after, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}); len(after) == 0 {
		t.Fatal("the fetch brought no unread mention, the fixture is broken")
	}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { posts.Add(1) }))
	defer server.Close()
	notifyNew(workdir, t.TempDir(), "", newUnread, send, server.Client())
	if len(*sent) != 0 || posts.Load() != 0 {
		t.Errorf("sent %d desktop messages and %d webhook posts with the setting off and no webhook, want none", len(*sent), posts.Load())
	}
	if err := settings.NewManager().Write("notifications.webhook", server.URL); err != nil {
		t.Fatalf("write webhook: %v", err)
	}
	after, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true})
	notifyNew(workdir, t.TempDir(), "", after, send, server.Client())
	if len(*sent) != 0 || int(posts.Load()) != len(after) {
		t.Errorf("sent %d desktop messages and %d webhook posts with a webhook and desktop off, want 0 and %d", len(*sent), posts.Load(), len(after))
	}
}

func TestFetch_snapshotErrorSendsNothing(t *testing.T) {
	isolateDesktopSetting(t)
	send, sent := recordSends()
	workdir := mentionWorkspace(t)
	if err := os.WriteFile(filepath.Join(workdir, ".git", failingProviderMarker), nil, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	newUnread := fetchNewUnread(t, workdir)
	if len(newUnread) != 0 {
		t.Errorf("the result carries %d items after a provider failed, want none", len(newUnread))
	}
	if after, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}); len(after) == 0 {
		t.Fatal("the fetch brought no unread mention, the fixture is broken")
	}
	notifyNew(workdir, t.TempDir(), "", newUnread, send, http.DefaultClient)
	if len(*sent) != 0 {
		t.Errorf("a desktop message was sent after a provider failed")
	}
}

func TestNotifyNew_emitterSocket(t *testing.T) {
	isolateDesktopSetting(t)
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(template) })
	workdir := testutil.CopyRepo(t, template)
	if err := site.WriteWorkspaceSiteCustomization(workdir, site.SiteCustomization{URL: "https://site.example/"}); err != nil {
		t.Fatalf("write site url: %v", err)
	}
	server, docs := webhookRecorder(t, func(map[string]any, int) int { return http.StatusOK })
	if err := settings.NewManager().Write("notifications.webhook", server.URL); err != nil {
		t.Fatalf("write webhook: %v", err)
	}
	item := notifications.Notification{RepoURL: gitmsg.ResolveRepoURL(workdir), Hash: "0123456789abcdef0123", Branch: "gitmsg/social", Type: "mention"}
	follow := notifications.Notification{RepoURL: "https://example.com/other/repo", Hash: "follow", Type: "follow"}
	var sent []notifications.DesktopMessage
	send := func(messages []notifications.DesktopMessage) error {
		sent = append(sent, messages...)
		return nil
	}
	socket := "/tmp/gitsocial/click/1-2.sock"
	notifyNew(workdir, t.TempDir(), socket, []notifications.Notification{item, follow}, send, server.Client())
	wantRefs := []string{protocol.CreateRef(protocol.RefTypeCommit, item.Hash, item.RepoURL, item.Branch), ""}
	if len(sent) != 2 {
		t.Fatalf("desktop messages = %+v, want two", sent)
	}
	for i, m := range sent {
		if m.Socket != socket || m.Ref != wantRefs[i] || m.URL != "" {
			t.Errorf("desktop message %d = socket %q, ref %q, url %q, want the socket, the ref %q and no URL", i, m.Socket, m.Ref, m.URL, wantRefs[i])
		}
	}
	posted := docs()
	if len(posted) != 2 {
		t.Fatalf("webhook posts = %v, want two", posted)
	}
	for _, doc := range posted {
		if doc["url"] == "" || doc["socket"] != nil || doc["ref"] != nil {
			t.Errorf("webhook post = %v, want the URL and no socket or ref", doc)
		}
	}
	sent = nil
	notifyNew(workdir, t.TempDir(), "", []notifications.Notification{item}, send, server.Client())
	if len(sent) != 1 || sent[0].Socket != "" || sent[0].URL != "https://site.example/index.html#commit:0123456789ab" {
		t.Errorf("desktop messages with no socket = %+v, want the item URL", sent)
	}
}
