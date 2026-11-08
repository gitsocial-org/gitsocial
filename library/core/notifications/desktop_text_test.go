// desktop_text_test.go - Tests for the new unread items of a fetch and the text of their desktop messages.
package notifications

import (
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

func TestDesktopMessages_summaryOverThree(t *testing.T) {
	three := []Notification{unread("a"), unread("b"), unread("c")}
	bodies := DesktopBodies(three)
	if len(bodies) != 3 {
		t.Fatalf("three items gave %d messages, want 3", len(bodies))
	}
	if bodies[0] != "Alice mentioned you · owner/repo" {
		t.Errorf("body = %q", bodies[0])
	}
	five := []Notification{unread("a"), unread("b"), unread("c"), unread("d"), unread("e")}
	if got := DesktopBodies(five); len(got) != 1 || got[0] != "5 new notifications" {
		t.Errorf("five items gave %q, want one summary", got)
	}
	unknown := DesktopBodies([]Notification{{Type: "new-kind"}})
	if len(unknown) != 1 || !strings.Contains(unknown[0], "new-kind") {
		t.Errorf("an unmapped type gave %q, want the type name", unknown)
	}
	branch := DesktopBodies([]Notification{{Type: "head-advanced", RepoURL: "https://example.com/owner/repo"}})
	if len(branch) != 1 || branch[0] != "The head branch of a pull request advanced · owner/repo" {
		t.Errorf("a type with no actor gave %q", branch)
	}
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

func TestDesktopBodies_everyDocumentedType(t *testing.T) {
	types := documentedTypes(t)
	if len(types) < 20 {
		t.Fatalf("the guide lists %d types, want the full table", len(types))
	}
	for _, typ := range types {
		body := DesktopBodies([]Notification{{Type: typ, Actor: Actor{Name: "Alice"}}})
		if len(body) != 1 || strings.HasSuffix(body[0], " "+typ) || body[0] == typ {
			t.Errorf("type %s gave %q, want a sentence", typ, body)
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
