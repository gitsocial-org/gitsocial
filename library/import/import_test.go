// import_test.go - Tests for pure helpers in import pipeline
package importpkg

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
)

func TestBuildOrigin(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	got := buildOrigin("Alice", "alice@example.com", ts, "github", "https://github.com/user/repo", "issues/42")
	if got == nil {
		t.Fatal("buildOrigin returned nil")
	}
	if got.AuthorName != "Alice" {
		t.Errorf("AuthorName = %q", got.AuthorName)
	}
	if got.AuthorEmail != "alice@example.com" {
		t.Errorf("AuthorEmail = %q", got.AuthorEmail)
	}
	if got.Platform != "github" {
		t.Errorf("Platform = %q", got.Platform)
	}
	if got.Time != "2024-06-15T12:00:00Z" {
		t.Errorf("Time = %q", got.Time)
	}
	if got.URL != "https://github.com/user/repo/issues/42" {
		t.Errorf("URL = %q", got.URL)
	}
}

func TestBuildOrigin_AllEmpty(t *testing.T) {
	got := buildOrigin("", "", time.Time{}, "github", "", "")
	if got != nil {
		t.Errorf("buildOrigin with all empty fields should return nil, got %+v", got)
	}
}

func TestBuildOrigin_ZeroTime(t *testing.T) {
	got := buildOrigin("Alice", "alice@example.com", time.Time{}, "github", "", "")
	if got == nil {
		t.Fatal("buildOrigin returned nil")
	}
	if got.Time != "" {
		t.Errorf("Time should be empty for zero time, got %q", got.Time)
	}
}

func TestBuildOrigin_EmptyPath(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	got := buildOrigin("Bob", "bob@example.com", ts, "gitlab", "https://gitlab.com/org/repo", "")
	if got == nil {
		t.Fatal("buildOrigin returned nil")
	}
	if got.URL != "" {
		t.Errorf("URL should be empty when path is empty, got %q", got.URL)
	}
}

func TestBuildStateChangeOrigin(t *testing.T) {
	ts := time.Date(2024, 7, 1, 8, 0, 0, 0, time.UTC)
	got := buildStateChangeOrigin("Bob", "bob@example.com", ts, "github", "https://github.com/user/repo", "pull/10")
	if got == nil {
		t.Fatal("buildStateChangeOrigin returned nil")
	}
	if got.AuthorName != "Bob" || got.AuthorEmail != "bob@example.com" {
		t.Errorf("author mismatch: %q %q", got.AuthorName, got.AuthorEmail)
	}
	if got.URL != "https://github.com/user/repo/pull/10" {
		t.Errorf("URL = %q", got.URL)
	}
}

func TestBuildStateChangeOrigin_AllEmpty(t *testing.T) {
	got := buildStateChangeOrigin("", "", time.Time{}, "github", "", "")
	if got != nil {
		t.Errorf("buildStateChangeOrigin with all empty should return nil, got %+v", got)
	}
}

func TestToPMLabels(t *testing.T) {
	input := []string{"kind/bug", "area/networking", "unscoped"}
	got := toPMLabels(input)
	want := []pm.Label{
		{Scope: "kind", Value: "bug"},
		{Scope: "area", Value: "networking"},
		{Value: "unscoped"},
	}
	if len(got) != len(want) {
		t.Fatalf("toPMLabels len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("toPMLabels[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestToPMLabels_Empty(t *testing.T) {
	got := toPMLabels(nil)
	if len(got) != 0 {
		t.Errorf("toPMLabels(nil) = %v, want empty", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"hello world this is long", 10, "hello w..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}
	for _, c := range cases {
		got := truncate(c.input, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
	}
}

func TestStatsTotal(t *testing.T) {
	s := Stats{Milestones: 2, Issues: 5, Releases: 1, Forks: 3, PRs: 4, Posts: 10, Comments: 20}
	if got := s.Total(); got != 45 {
		t.Errorf("Total() = %d, want 45", got)
	}
}

func TestParseEmailMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emails.txt")
	content := "# comment\nalice=alice@example.com\n\nbob=bob@example.com\n"
	os.WriteFile(path, []byte(content), 0644)
	got, err := ParseEmailMap(path)
	if err != nil {
		t.Fatalf("ParseEmailMap: %v", err)
	}
	if got["alice"] != "alice@example.com" {
		t.Errorf("alice = %q", got["alice"])
	}
	if got["bob"] != "bob@example.com" {
		t.Errorf("bob = %q", got["bob"])
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestParseEmailMap_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	os.WriteFile(path, []byte("no-equals-sign\n"), 0644)
	_, err := ParseEmailMap(path)
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestParseEmailMap_EmptyValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	os.WriteFile(path, []byte("alice=\n"), 0644)
	_, err := ParseEmailMap(path)
	if err == nil {
		t.Error("expected error for empty email")
	}
}

func TestParseEmailMap_FileNotFound(t *testing.T) {
	_, err := ParseEmailMap("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
