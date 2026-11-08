// desktop_test.go - Tests for the new unread items of a fetch and the setting that turns desktop messages off.
package client

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// isolateDesktopSetting points the personal repository at a temp dir and clears the variable.
func isolateDesktopSetting(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(t.TempDir(), "personal"))
	t.Setenv(desktopEnv, "")
}

// stubDesktopTools puts recording stubs of the OS tools first on PATH and returns the file they write.
func stubDesktopTools(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "sent")
	for _, tool := range []string{"osascript", "notify-send"} {
		script := "#!/bin/sh\necho \"$@\" >> '" + record + "'\n"
		if err := os.WriteFile(filepath.Join(dir, tool), []byte(script), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", tool, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
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
	newUnread := fetchNewUnread(t, mentionWorkspace(t))
	if len(newUnread) != 1 || newUnread[0].Type != "mention" || newUnread[0].Actor.Name != "Ada" {
		t.Fatalf("new unread = %+v, want the one mention by Ada", newUnread)
	}
	record := stubDesktopTools(t)
	NotifyDesktop(newUnread)
	if _, err := os.Stat(record); err != nil {
		t.Errorf("no desktop message sent with the setting on: %v", err)
	}
}

func TestFetch_desktopOffSendsNothing(t *testing.T) {
	isolateDesktopSetting(t)
	if err := settings.NewManager().Write("notifications.desktop", "false"); err != nil {
		t.Fatalf("write key: %v", err)
	}
	record := stubDesktopTools(t)
	workdir := mentionWorkspace(t)
	newUnread := fetchNewUnread(t, workdir)
	if len(newUnread) != 0 {
		t.Errorf("the result carries %d items with the setting off, want none because no snapshot runs", len(newUnread))
	}
	if after, _ := notifications.GetAll(workdir, notifications.Filter{UnreadOnly: true}); len(after) == 0 {
		t.Fatal("the fetch brought no unread mention, the fixture is broken")
	}
	NotifyDesktop(newUnread)
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Errorf("a desktop message was sent with the setting off")
	}
}

func TestFetch_snapshotErrorSendsNothing(t *testing.T) {
	isolateDesktopSetting(t)
	record := stubDesktopTools(t)
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
	NotifyDesktop(newUnread)
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Errorf("a desktop message was sent after a provider failed")
	}
}
