// desktop_test.go - Tests for the delivery of the events of a fetch and the setting that turns desktop messages off.
package client

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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

// init registers the provider that TestDeliver_providerFailureSendsTheRest turns on with the marker.
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

// fetchedMention returns a workspace after a fetch that brought one unread mention and delivered nothing, as serve does.
func fetchedMention(t *testing.T) string {
	t.Helper()
	workdir := mentionWorkspace(t)
	if result, _ := Fetch(workdir, t.TempDir(), FetchOptions{}); !result.Success {
		t.Fatalf("Fetch() error = %v", result.Error)
	}
	if unread, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}); len(unread) != 1 || unread[0].Type != "mention" {
		t.Fatalf("unread after the fetch = %+v, want the one mention", unread)
	}
	return workdir
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

func TestDeliver_afterServeFetch(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	send, sent := recordSends()
	if count := deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient); count != 1 || len(*sent) != 1 {
		t.Errorf("deliver() = %d unread with %q sent, want 1 and one message", count, *sent)
	}
	if count := deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient); count != 1 || len(*sent) != 1 {
		t.Errorf("second deliver() = %d unread with %q sent, want 1 and no second message", count, *sent)
	}
}

// TestDeliver_twoProcessesSendOnce runs two deliveries on one cache at the same time, as two processes do through the claim.
func TestDeliver_twoProcessesSendOnce(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	var sends atomic.Int32
	send := func(messages []notifications.DesktopMessage) error {
		if sends.Add(int32(len(messages))) > 1 {
			return errors.New("sent twice")
		}
		return nil
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient)
		}()
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Errorf("two deliveries sent %d messages, want 1", sends.Load())
	}
}

func TestDeliver_manualClaimsNoDesktop(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	server, docs := webhookRecorder(t, func(map[string]any, int) int { return http.StatusOK })
	if err := settings.NewManager().Write("notifications.webhook", server.URL); err != nil {
		t.Fatalf("write webhook: %v", err)
	}
	send, sent := recordSends()
	deliver(workdir, t.TempDir(), "", false, send, server.Client())
	if len(*sent) != 0 || len(docs()) != 1 {
		t.Errorf("a manual delivery sent %d desktop messages and %d webhook posts, want 0 and 1", len(*sent), len(docs()))
	}
	deliver(workdir, t.TempDir(), "", true, send, server.Client())
	if len(*sent) != 0 || len(docs()) != 1 {
		t.Errorf("the next delivery sent %d desktop messages and %d webhook posts, want the event to stay claimed", len(*sent), len(docs()))
	}
}

func TestDeliver_readThenUnread(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	if err := notifications.MarkAllAsRead(workdir); err != nil {
		t.Fatalf("MarkAllAsRead() error = %v", err)
	}
	if err := notifications.MarkAllAsUnread(workdir); err != nil {
		t.Fatalf("MarkAllAsUnread() error = %v", err)
	}
	send, sent := recordSends()
	if count := deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient); count != 1 || len(*sent) != 0 {
		t.Errorf("deliver() = %d unread with %q sent, want 1 unread and no message for an event that was read", count, *sent)
	}
}

func TestDeliver_channelsOffClaimsNothing(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	if err := settings.NewManager().Write("notifications.desktop", "false"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	send, sent := recordSends()
	if count := deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient); count != 1 || len(*sent) != 0 {
		t.Errorf("deliver() = %d unread with %q sent with both channels off, want 1 and no message", count, *sent)
	}
	if err := settings.NewManager().Write("notifications.desktop", "true"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient)
	if len(*sent) != 1 {
		t.Errorf("sent %q after the setting went on, want the one event that no process claimed", *sent)
	}
}

func TestDeliver_providerFailureSendsTheRest(t *testing.T) {
	isolateDesktopSetting(t)
	workdir := fetchedMention(t)
	if err := os.WriteFile(filepath.Join(workdir, ".git", failingProviderMarker), nil, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	send, sent := recordSends()
	deliver(workdir, t.TempDir(), "", true, send, http.DefaultClient)
	if len(*sent) != 1 {
		t.Errorf("sent %q after one provider failed, want the mention of the other provider", *sent)
	}
}

func TestSendClaimed_emitterSocket(t *testing.T) {
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
	sendClaimed(workdir, t.TempDir(), socket, true, []notifications.Notification{item, follow}, server.URL, send, server.Client())
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
	sendClaimed(workdir, t.TempDir(), "", true, []notifications.Notification{item}, server.URL, send, server.Client())
	if len(sent) != 1 || sent[0].Socket != "" || sent[0].URL != "https://site.example/index.html#commit:0123456789ab" {
		t.Errorf("desktop messages with no socket = %+v, want the item URL", sent)
	}
}
