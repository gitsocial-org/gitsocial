// desktop_text_test.go - Tests for the text of the desktop messages.
package notifications

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// unread returns one unread mention on a commit.
func unread(hash string) Notification {
	return Notification{RepoURL: "https://example.com/owner/repo", Hash: hash, Branch: "gitmsg/social", Type: "mention", Actor: Actor{Name: "Alice"}}
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

func TestDesktopMessages_oneMessagePerItem(t *testing.T) {
	three := []Notification{unread("a"), unread("b"), unread("c")}
	messages := DesktopMessages(three, func(n Notification) string { return "https://site.example/" + n.Hash }, "https://site.example/")
	if len(messages) != 3 {
		t.Fatalf("three items gave %d messages, want 3", len(messages))
	}
	want := DesktopMessage{Title: "Alice mentioned you", Body: "owner/repo", URL: "https://site.example/a", Type: "mention", Repo: "https://example.com/owner/repo", Hash: "a", Ref: "https://example.com/owner/repo#commit:a@gitmsg/social"}
	if messages[0] != want {
		t.Errorf("message = %+v, want %+v", messages[0], want)
	}
	unknown := bodies([]Notification{{Type: "new-kind"}})
	if len(unknown) != 1 || unknown[0] != "" {
		t.Errorf("an unmapped type with no parts gave the body %q, want none", unknown)
	}
	if title, _ := MessageText(Notification{Type: "new-kind", Actor: Actor{Name: "Alice"}}); title != "Alice new-kind" {
		t.Errorf("an unmapped type gave the title %q, want the type name", title)
	}
}

func TestDesktopMessages_summaryListsTitles(t *testing.T) {
	items := []Notification{unread("a"), unread("b"), unread("c"), unread("d"), unread("e")}
	items[1].Glyph, items[1].Subject = "↩", "Not in the summary"
	summary := DesktopMessages(items, noURL, "https://site.example/")
	want := DesktopMessage{Title: "5 new notifications", Body: "Alice mentioned you\n↩ Alice mentioned you\nAlice mentioned you\nand 2 more", URL: "https://site.example/"}
	if len(summary) != 1 || summary[0] != want {
		t.Errorf("five items gave %+v, want %+v", summary, want)
	}
}

// textCase is one case of the notificationTexts fixture: the inputs of the text function and its title and body.
type textCase struct {
	Name, Type, Actor, Repo, Subject, Excerpt, Phrase, Glyph, Title, Body string
}

// parityFixture returns the notification text parts of the fixture that the site carrier is checked against.
func parityFixture(t *testing.T) (cases []textCase, phrases map[string]string) {
	t.Helper()
	data, err := os.ReadFile("../site/sitetest/parity_fixtures.json")
	if err != nil {
		t.Fatalf("read the parity fixture: %v", err)
	}
	var fixture struct {
		NotificationTexts []textCase        `json:"notificationTexts"`
		NewItemPhrases    map[string]string `json:"newItemPhrases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse the parity fixture: %v", err)
	}
	return fixture.NotificationTexts, fixture.NewItemPhrases
}

// fixtureNotification returns the notification of the inputs of one fixture case.
func fixtureNotification(c textCase) Notification {
	n := Notification{Type: c.Type, Actor: Actor{Name: c.Actor}, Subject: c.Subject, Excerpt: c.Excerpt, Phrase: c.Phrase, Glyph: c.Glyph}
	if c.Repo != "" {
		n.RepoURL = "https://example.com/" + c.Repo
	}
	return n
}

// documentedTypes returns the notification types of the table under the Types heading of the guide.
func documentedTypes(t *testing.T) []string {
	t.Helper()
	guide, err := os.ReadFile("../../../documentation/NOTIFICATIONS.md")
	if err != nil {
		t.Fatalf("read the guide: %v", err)
	}
	_, section, ok := strings.Cut(string(guide), "\n## Types\n")
	if !ok {
		t.Fatal("the guide has no Types heading")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var types []string
	for _, line := range strings.Split(section, "\n") {
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
	cases, _ := parityFixture(t)
	covered := make(map[string]bool, len(cases))
	for _, c := range cases {
		covered[c.Type] = true
		if title, body := MessageText(fixtureNotification(c)); title != c.Title || body != c.Body {
			t.Errorf("%s gave %q, %q, want %q, %q", c.Name, title, body, c.Title, c.Body)
		}
	}
	types := documentedTypes(t)
	if len(types) != 28 {
		t.Errorf("the types table of the guide has %d types, want 28", len(types))
	}
	for _, typ := range types {
		if !covered[typ] {
			t.Errorf("type %s is not in the fixture", typ)
		}
		if typeWords(typ) == typ {
			t.Errorf("type %s has no sentence and no verb", typ)
		}
	}
}

func TestNewItemPhrase_parity(t *testing.T) {
	_, phrases := parityFixture(t)
	if len(phrases) == 0 {
		t.Fatal("no new item phrases in the parity fixture")
	}
	for kind, want := range phrases {
		if got := NewItemPhrase(kind); got != want {
			t.Errorf("NewItemPhrase(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestNotificationText_missingParts(t *testing.T) {
	base := Notification{Type: "comment", Actor: Actor{Name: "Alice"}, RepoURL: "https://example.com/owner/repo", Glyph: "↩"}
	cases := []struct {
		name, subject, excerpt, wantBody string
	}{
		{"no excerpt and no subject", "", "", "owner/repo"},
		{"a subject only", "Release notes", "", "Release notes\nowner/repo"},
		{"an excerpt only", "", "Nice", "Nice\nowner/repo"},
		{"an excerpt of control characters only", "Release notes", "\x1b\x07 \u202E", "Release notes\nowner/repo"},
	}
	for _, c := range cases {
		n := base
		n.Subject, n.Excerpt = c.subject, c.excerpt
		title, body := MessageText(n)
		if body != c.wantBody {
			t.Errorf("%s: body = %q, want %q", c.name, body, c.wantBody)
		}
		if title != "↩ Alice commented" {
			t.Errorf("%s: title = %q, want the glyph, the actor and the verb only", c.name, title)
		}
	}
	if title, body := MessageText(Notification{Type: "follow", Actor: Actor{Name: "owner/repo"}, RepoURL: "https://example.com/owner/repo", Subject: "x"}); title != "owner/repo added your repository to a list" || body != "" {
		t.Errorf("a follow gave %q, %q, want the repository as the actor and no body", title, body)
	}
}

func TestNotificationText_titleLimit(t *testing.T) {
	longest := 0
	for _, words := range []map[string]string{typeSentences, typeVerbs} {
		for _, w := range words {
			longest = max(longest, utf8.RuneCountInString(w))
		}
	}
	limit := utf8.RuneCountInString("↩ ") + actorLimit + 1 + longest
	actor := strings.Repeat("A", 200) + "\n" + strings.Repeat("B", 200)
	for _, words := range []map[string]string{typeSentences, typeVerbs} {
		for typ := range words {
			title, _ := MessageText(Notification{Type: typ, Actor: Actor{Name: actor}, Glyph: "↩", Subject: strings.Repeat("S", 200), Excerpt: strings.Repeat("E", 200)})
			if got := utf8.RuneCountInString(title); got > limit {
				t.Errorf("%s: title has %d code points, want at most %d", typ, got, limit)
			}
			if strings.ContainsAny(title, "SEB") {
				t.Errorf("%s: title %q holds a subject, an excerpt or a second line", typ, title)
			}
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

// textFields returns the text fields of the notifications of a provider, by commit hash.
func textFields(t *testing.T, p Provider, workdir string) map[string][4]string {
	t.Helper()
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	out := make(map[string][4]string, len(items))
	for _, n := range items {
		out[n.Hash] = [4]string{n.Subject, n.Excerpt, n.Phrase, n.Glyph}
	}
	return out
}

func TestNotificationText_fields(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t) // alice@example.com
	const repoURL, otherURL = "https://github.com/x/y", "https://github.com/z/fork"
	comment := protocol.FormatMessage("Ping @alice@example.com about this\n\nThe rest", protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "comment"}}, nil)
	if err := cache.ExecLocked(func(db *sql.DB) error {
		for _, c := range [][3]string{{"ae0000000001", "gitmsg/social", comment}, {"ae0000000002", "main", "Ask @alice@example.com\n\nbody"}} {
			if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp) VALUES (?, ?, ?, 'Bob', 'bob@example.com', ?, '2025-10-21T12:00:00Z')`, repoURL, c[0], c[1], c[2]); err != nil {
				return err
			}
			if _, err := db.Exec(`INSERT INTO core_mentions (repo_url, hash, branch, email) VALUES (?, ?, ?, 'alice@example.com')`, repoURL, c[0], c[1]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed mentions: %v", err)
	}
	mentions := textFields(t, &mentionProvider{}, workdir)
	if got, want := mentions["ae0000000001"], [4]string{"", "Ping @alice@example.com about this", "", "↩"}; got != want {
		t.Errorf("mention in a comment = %q, want %q", got, want)
	}
	if got, want := mentions["ae0000000002"], [4]string{"", "Ask @alice@example.com", "", ""}; got != want {
		t.Errorf("mention in a code commit = %q, want %q", got, want)
	}
	seedTarget(t, repoURL, "a55e00000001", "gitmsg/pm", false)
	seedTrailer(t, repoURL, "b0b000000001", "main", "a55e00000001", "gitmsg/pm")
	if got, want := textFields(t, &trailerProvider{}, workdir)["b0b000000001"], [4]string{"target", "fix", "", ""}; got != want {
		t.Errorf("reference = %q, want the item as the subject and the fix as the excerpt", got)
	}
	now := time.Now()
	seedEdit(t, "ca0000000001", "alice@example.com", "ed0000000001", "Bob", "bob@example.com", now, false)
	seedEdit(t, "ca0000000002", "alice@example.com", "ed0000000002", "Bob", "bob@example.com", now, true)
	proposal := protocol.FormatMessage("my version", protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post", "edits": repoURL + "#commit:ca0000000001@main"}}, nil)
	if err := cache.InsertCommits([]cache.Commit{{RepoURL: otherURL, Hash: "ed0000000003", Branch: "gitmsg/social", AuthorName: "Carol", AuthorEmail: "carol@example.com", Message: proposal, Timestamp: now.Add(2 * time.Minute)}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	edits := textFields(t, &editProvider{}, workdir)
	for hash, want := range map[string][4]string{
		"ed0000000001": {"canonical msg", "", "", "•"},
		"ed0000000002": {"canonical msg", "", "retracted your item", "•"},
		"ed0000000003": {"canonical msg", "", "proposed an edit to your item", "•"},
	} {
		if got := edits[hash]; got != want {
			t.Errorf("edit %s = %q, want %q", hash, got, want)
		}
	}
}
