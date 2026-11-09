// desktop_text_test.go - Tests for the new unread items of a fetch and the text of their desktop messages.
package notifications

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// unread returns one unread mention on a commit.
func unread(hash string) Notification {
	return Notification{RepoURL: "https://example.com/owner/repo", Hash: hash, Branch: "gitmsg/social", Type: "mention", Actor: Actor{Name: "Alice"}}
}

func TestNewUnread_excludesPriorUnread(t *testing.T) {
	before := []Notification{unread("a1")}
	after := []Notification{unread("a1"), unread("b2"), unread("b2")}
	fresh := NewUnread(before, after)
	if len(fresh) != 1 || fresh[0].Hash != "b2" {
		t.Errorf("NewUnread = %+v, want only b2 once", fresh)
	}
	if got := NewUnread(after, after); len(got) != 0 {
		t.Errorf("NewUnread with no change = %+v, want none", got)
	}
}

func TestNewUnread_ignoresBranch(t *testing.T) {
	moved := unread("a1")
	moved.Branch = "main"
	if fresh := NewUnread([]Notification{unread("a1")}, []Notification{moved}); len(fresh) != 0 {
		t.Errorf("NewUnread for a row that moved branch = %+v, want none", fresh)
	}
}

// noURL gives no click target.
func noURL(Notification) string { return "" }

// bodies returns the bodies of the messages of items.
func bodies(items []Notification) []string {
	all := DesktopMessages(items, noURL, "")
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Body)
	}
	return out
}

func TestDesktopMessages_summaryOverThree(t *testing.T) {
	three := []Notification{unread("a"), unread("b"), unread("c")}
	messages := DesktopMessages(three, func(n Notification) string { return "https://site.example/" + n.Hash }, "https://site.example/")
	if len(messages) != 3 {
		t.Fatalf("three items gave %d messages, want 3", len(messages))
	}
	want := DesktopMessage{Title: "GitSocial", Body: "Alice mentioned you · owner/repo", URL: "https://site.example/a", Type: "mention", Repo: "https://example.com/owner/repo", Hash: "a"}
	if messages[0] != want {
		t.Errorf("message = %+v, want %+v", messages[0], want)
	}
	five := []Notification{unread("a"), unread("b"), unread("c"), unread("d"), unread("e")}
	summary := DesktopMessages(five, noURL, "https://site.example/")
	if len(summary) != 1 || summary[0] != (DesktopMessage{Title: "GitSocial", Body: "5 new notifications", URL: "https://site.example/"}) {
		t.Errorf("five items gave %+v, want one summary with the summary URL and no item", summary)
	}
	unknown := bodies([]Notification{{Type: "new-kind"}})
	if len(unknown) != 1 || !strings.Contains(unknown[0], "new-kind") {
		t.Errorf("an unmapped type gave %q, want the type name", unknown)
	}
	branch := bodies([]Notification{{Type: "head-advanced", RepoURL: "https://example.com/owner/repo"}})
	if len(branch) != 1 || branch[0] != "The head branch of a pull request advanced · owner/repo" {
		t.Errorf("a type with no actor gave %q", branch)
	}
}

// parityBodies returns the body of each documented type for the actor Alice on owner/repo, from the fixture that the site carrier is checked against.
func parityBodies(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../site/sitetest/parity_fixtures.json")
	if err != nil {
		t.Fatalf("read the parity fixture: %v", err)
	}
	var fixture struct {
		NotificationBodies map[string]string `json:"notificationBodies"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse the parity fixture: %v", err)
	}
	return fixture.NotificationBodies
}

// documentedTypes returns the notification types of the table in the guide.
func documentedTypes(t *testing.T) []string {
	t.Helper()
	guide, err := os.ReadFile("../../../documentation/NOTIFICATIONS.md")
	if err != nil {
		t.Fatalf("read the guide: %v", err)
	}
	var types []string
	for _, line := range strings.Split(string(guide), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		for _, cell := range strings.Split(cells[1], ",") {
			types = append(types, strings.Trim(strings.TrimSpace(cell), "`"))
		}
	}
	return types
}

func TestNotificationText_parity(t *testing.T) {
	types := documentedTypes(t)
	golden := parityBodies(t)
	if len(types) != len(golden) {
		t.Errorf("the guide lists %d types and the fixture %d, want the same set", len(types), len(golden))
	}
	for _, typ := range types {
		want, ok := golden[typ]
		if !ok {
			t.Errorf("type %s is not in the fixture", typ)
			continue
		}
		got := bodies([]Notification{{Type: typ, Actor: Actor{Name: "Alice"}, RepoURL: "https://example.com/owner/repo"}})
		if len(got) != 1 || got[0] != want {
			t.Errorf("type %s gave %q, want %q", typ, got, want)
		}
	}
}

func TestGetAllStrict_reportsProviderFailure(t *testing.T) {
	resetProviders(t)
	RegisterProvider("ok", &mockProvider{notifications: []Notification{unread("a1")}})
	RegisterProvider("broken", &mockProvider{err: errors.New("busy")})
	items, err := GetAllStrict("", Filter{})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("GetAllStrict error = %v, want the failing provider", err)
	}
	if len(items) != 1 {
		t.Errorf("GetAllStrict items = %d, want the items of the provider that worked", len(items))
	}
	if _, err := GetAll("", Filter{}); err != nil {
		t.Errorf("GetAll error = %v, want none", err)
	}
}
