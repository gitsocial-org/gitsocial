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

func TestDetectStackEdges_LinearStack(t *testing.T) {
	prs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "main", HeadBranch: "auth-middleware"},
		{ExternalID: "2", Number: 2, State: "open", BaseBranch: "auth-middleware", HeadBranch: "auth-routes"},
		{ExternalID: "3", Number: 3, State: "open", BaseBranch: "auth-routes", HeadBranch: "auth-tests"},
	}
	edges := detectStackEdges(prs)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d: %+v", len(edges), edges)
	}
	// PR2 depends on PR1
	if edges[0].ChildExternalID != "2" || edges[0].ParentExternalID != "1" {
		t.Errorf("edge[0] = %+v, want child=2 parent=1", edges[0])
	}
	// PR3 depends on PR2
	if edges[1].ChildExternalID != "3" || edges[1].ParentExternalID != "2" {
		t.Errorf("edge[1] = %+v, want child=3 parent=2", edges[1])
	}
}

func TestDetectStackEdges_PlatformAgnostic(t *testing.T) {
	// Same branch topology imported from GitLab should yield identical edges.
	// ExternalID format differs per platform (GitHub: PR number, GitLab: MR IID) but
	// detectStackEdges only cares about the branch structure.
	gitlabPRs := []ImportPR{
		{ExternalID: "101", Number: 101, State: "open", BaseBranch: "main", HeadBranch: "feature-a"},
		{ExternalID: "102", Number: 102, State: "open", BaseBranch: "feature-a", HeadBranch: "feature-b"},
	}
	githubPRs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "main", HeadBranch: "feature-a"},
		{ExternalID: "2", Number: 2, State: "open", BaseBranch: "feature-a", HeadBranch: "feature-b"},
	}
	gitlabEdges := detectStackEdges(gitlabPRs)
	githubEdges := detectStackEdges(githubPRs)
	if len(gitlabEdges) != 1 || len(githubEdges) != 1 {
		t.Fatalf("both platforms should produce 1 edge each, got gitlab=%d github=%d", len(gitlabEdges), len(githubEdges))
	}
	if gitlabEdges[0].ChildExternalID != "102" || gitlabEdges[0].ParentExternalID != "101" {
		t.Errorf("gitlab edge = %+v", gitlabEdges[0])
	}
	if githubEdges[0].ChildExternalID != "2" || githubEdges[0].ParentExternalID != "1" {
		t.Errorf("github edge = %+v", githubEdges[0])
	}
}

func TestDetectStackEdges_ExcludesForks(t *testing.T) {
	prs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "main", HeadBranch: "feature-a"},
		{ExternalID: "2", Number: 2, State: "open", BaseBranch: "feature-a", HeadBranch: "feature-b", HeadRepo: "https://gitlab.com/fork/repo"},
	}
	edges := detectStackEdges(prs)
	if len(edges) != 0 {
		t.Errorf("fork PR should not produce stack edge, got %+v", edges)
	}
}

func TestDetectStackEdges_ExcludesClosed(t *testing.T) {
	prs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "main", HeadBranch: "feature-a"},
		{ExternalID: "2", Number: 2, State: "closed", BaseBranch: "feature-a", HeadBranch: "feature-b"},
		{ExternalID: "3", Number: 3, State: "merged", BaseBranch: "feature-a", HeadBranch: "feature-c"},
	}
	edges := detectStackEdges(prs)
	if len(edges) != 0 {
		t.Errorf("closed/merged PRs should not produce stack edges, got %+v", edges)
	}
}

func TestDetectStackEdges_NoSelfReference(t *testing.T) {
	// A PR whose base matches its own head (degenerate/invalid data) must not self-link.
	prs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "feature-a", HeadBranch: "feature-a"},
	}
	edges := detectStackEdges(prs)
	if len(edges) != 0 {
		t.Errorf("self-referencing PR should not produce edge, got %+v", edges)
	}
}

func TestDetectStackEdges_Empty(t *testing.T) {
	if edges := detectStackEdges(nil); edges != nil {
		t.Errorf("nil input should return nil, got %+v", edges)
	}
	if edges := detectStackEdges([]ImportPR{}); edges != nil {
		t.Errorf("empty input should return nil, got %+v", edges)
	}
}

func TestDetectStackEdges_NoMatch(t *testing.T) {
	// Two PRs targeting main independently — no stack relationship.
	prs := []ImportPR{
		{ExternalID: "1", Number: 1, State: "open", BaseBranch: "main", HeadBranch: "feature-a"},
		{ExternalID: "2", Number: 2, State: "open", BaseBranch: "main", HeadBranch: "feature-b"},
	}
	edges := detectStackEdges(prs)
	if len(edges) != 0 {
		t.Errorf("independent PRs should not produce edges, got %+v", edges)
	}
}
