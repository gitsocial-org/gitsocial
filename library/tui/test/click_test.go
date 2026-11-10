// click_test.go - A click on a desktop message, sent through Update as the click socket sends it
package test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// clickFixture opens the feed on a harness and returns it with the one notification of the fixture, unread.
func clickFixture(t *testing.T, f *Fixture) (*Harness, notifications.Notification) {
	t.Helper()
	all, err := notifications.GetAll(f.Workdir, notifications.Filter{})
	if err != nil || len(all) != 1 || all[0].IsRead {
		t.Fatalf("fixture notifications = %+v, %v, want one unread", all, err)
	}
	h := New(t, f.Workdir, f.CacheDir)
	h.Navigate("/notifications")
	assertContains(t, renderedAfterLoad(h, []string{"edited your item"}), "Notifications (1)")
	return h, all[0]
}

// click sends a click on ref that arrived at, and drains its commands.
func click(h *Harness, ref string, at time.Time) {
	h.sendMsg(tuicore.ClickMsg{Ref: ref, At: at})
}

// isRead reports whether the cache has the notification of a repository and a hash read.
func isRead(t *testing.T, workdir string, n notifications.Notification) bool {
	t.Helper()
	all, err := notifications.GetAll(workdir, notifications.Filter{})
	if err != nil {
		t.Fatalf("get notifications: %v", err)
	}
	for _, other := range all {
		if other.RepoURL == n.RepoURL && other.Hash == n.Hash {
			return other.IsRead
		}
	}
	t.Fatalf("notification %s is not in the cache", n.Hash)
	return false
}

// feedRowRead reports whether the first row of the feed on screen is read.
func feedRowRead(t *testing.T, h *Harness) bool {
	t.Helper()
	meta := reflect.ValueOf(h.CurrentView()).Elem().FieldByName("meta")
	if !meta.IsValid() || meta.Len() == 0 {
		t.Fatalf("the view at %s has no feed rows", h.CurrentPath())
	}
	return meta.Index(0).FieldByName("IsRead").Bool()
}

// assertClickMatchesFeed clicks ref, then checks the route, the read marks and the count, and that Enter on the feed row opens the same location.
func assertClickMatchesFeed(t *testing.T, f *Fixture, ref string) {
	t.Helper()
	h, n := clickFixture(t, f)
	click(h, ref, time.Now())
	clicked := h.model.Router().Location()
	if clicked.Path != "/pm/issue" {
		t.Fatalf("click on the edit notification: path = %q, want /pm/issue", clicked.Path)
	}
	if !isRead(t, f.Workdir, n) {
		t.Error("the cache has the clicked notification unread")
	}
	if out := rendered(h); strings.Contains(out, "Notifications (1)") {
		t.Error("the nav panel still counts the clicked notification")
	}
	h.SendKey("esc")
	if got := h.CurrentPath(); got != "/notifications" {
		t.Fatalf("esc after the click: path = %q, want the feed", got)
	}
	if !feedRowRead(t, h) {
		t.Error("the feed shows the clicked notification unread")
	}
	h.SendKey("enter")
	if got := h.model.Router().Location(); !reflect.DeepEqual(got, clicked) {
		t.Errorf("enter on the feed row opens %+v, want the location of the click %+v", got, clicked)
	}
}

func TestClick_sameTargetAsFeed(t *testing.T) {
	f := SetupFixture(t)
	n, _ := notifications.GetAll(f.Workdir, notifications.Filter{})
	if len(n) != 1 {
		t.Fatalf("fixture notifications = %+v, want one", n)
	}
	assertClickMatchesFeed(t, f, protocol.CreateRef(protocol.RefTypeCommit, n[0].Hash, n[0].RepoURL, n[0].Branch))
}

func TestClick_movedBranch(t *testing.T) {
	f := SetupFixture(t)
	n, _ := notifications.GetAll(f.Workdir, notifications.Filter{})
	if len(n) != 1 || n[0].Branch == "main" {
		t.Fatalf("fixture notifications = %+v, want one off main", n)
	}
	assertClickMatchesFeed(t, f, protocol.CreateRef(protocol.RefTypeCommit, n[0].Hash, n[0].RepoURL, "main"))
}

func TestClick_guardKeepsView(t *testing.T) {
	f := SetupFixture(t)
	h, n := clickFixture(t, f)
	ref := protocol.CreateRef(protocol.RefTypeCommit, n.Hash, n.RepoURL, n.Branch)
	h.model.Host().State().ChoicePrompt = "Push to origin?"
	click(h, ref, time.Now())
	if got := h.CurrentPath(); got != "/notifications" {
		t.Errorf("click with a choice dialog open: path = %q, want the feed", got)
	}
	h.model.Host().State().ChoicePrompt = ""
	h.Navigate("/search")
	if input, ok := h.CurrentView().(tuicore.InputHandler); !ok || !input.IsInputActive() {
		t.Fatal("the search view has no active input, the guard is not under test")
	}
	click(h, ref, time.Now())
	if got := h.CurrentPath(); got != "/search" {
		t.Errorf("click with an active input: path = %q, want /search", got)
	}
	if isRead(t, f.Workdir, n) {
		t.Error("a click that the guard stopped marked the notification read")
	}
	assertContains(t, rendered(h), "Notifications (1)")
}

func TestClick_duringFetch(t *testing.T) {
	f := SetupFixture(t)
	h, n := clickFixture(t, f)
	h.model.SetFetching(true)
	click(h, protocol.CreateRef(protocol.RefTypeCommit, n.Hash, n.RepoURL, n.Branch), time.Now())
	if got := h.CurrentPath(); got != "/pm/issue" {
		t.Errorf("click during a fetch: path = %q, want /pm/issue", got)
	}
	if !isRead(t, f.Workdir, n) {
		t.Error("a click during a fetch left the notification unread")
	}
}

func TestClick_expiredIsDropped(t *testing.T) {
	f := getFixture(t)
	h, n := clickFixture(t, f)
	click(h, protocol.CreateRef(protocol.RefTypeCommit, n.Hash, n.RepoURL, n.Branch), time.Now().Add(-3*time.Second))
	if got := h.CurrentPath(); got != "/notifications" {
		t.Errorf("click older than 2 s: path = %q, want the feed", got)
	}
	if isRead(t, f.Workdir, n) {
		t.Error("a click older than 2 s marked the notification read")
	}
	if feedRowRead(t, h) {
		t.Error("a click older than 2 s marked the feed row read")
	}
	click(h, "", time.Now().Add(-3*time.Second))
	if got := h.CurrentPath(); got != "/notifications" {
		t.Errorf("click of a summary older than 2 s: path = %q, want the feed", got)
	}
}

func TestClick_refFallback(t *testing.T) {
	f := getFixture(t)
	h, n := clickFixture(t, f)
	click(h, f.ReleaseID, time.Now())
	if got := h.CurrentPath(); got != "/release/detail" {
		t.Errorf("click on a ref with no notification: path = %q, want /release/detail", got)
	}
	if isRead(t, f.Workdir, n) {
		t.Error("a click on another ref marked the notification read")
	}
	h.Navigate("/social/timeline")
	click(h, "", time.Now())
	if got := h.CurrentPath(); got != "/notifications" {
		t.Errorf("click on a summary: path = %q, want the feed", got)
	}
}
