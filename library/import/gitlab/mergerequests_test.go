// mergerequests_test.go - FetchReview against the fake GitLab merge request endpoints
package gitlab

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// reviewMRsJSON holds a draft, a merged fork request, a closed request and a bot request.
const reviewMRsJSON = `[
	{"iid":10,"title":"Add widget","description":"Desc","state":"opened","draft":true,
	 "source_branch":"feature/widget","target_branch":"main",
	 "source_project_id":7,"target_project_id":7,"sha":"abc123",
	 "diff_refs":{"base_sha":"base010"},"labels":["review","p1"],
	 "author":{"username":"alice"},"reviewers":[{"username":"bob"},{"username":"dave"}],
	 "created_at":"2024-06-15T12:00:00Z"},
	{"iid":11,"title":"Fork work","description":"","state":"merged",
	 "source_branch":"fix","target_branch":"main",
	 "source_project_id":8,"target_project_id":7,"sha":"def456",
	 "merge_commit_sha":"merge789","diff_refs":{"base_sha":"base111"},
	 "author":{"username":"bob"},"merged_by":{"username":"carol"},
	 "merged_at":"2024-07-01T10:00:00Z","created_at":"2024-06-10T12:00:00Z"},
	{"iid":12,"title":"Closed one","description":"","state":"closed",
	 "source_branch":"old","target_branch":"main",
	 "source_project_id":7,"target_project_id":7,"sha":"ghi789",
	 "author":{"username":"dave"},"closed_by":{"username":"alice"},
	 "closed_at":"2024-07-02T10:00:00Z","created_at":"2024-06-11T12:00:00Z"},
	{"iid":13,"title":"Bot request","description":"","state":"opened",
	 "source_branch":"deps","target_branch":"main",
	 "source_project_id":7,"target_project_id":7,
	 "author":{"username":"gitlab-bot"},"created_at":"2024-06-12T12:00:00Z"}
]`

// reviewRoutes serves the merge request, note, project and user endpoints FetchReview walks.
func reviewRoutes(t *testing.T, mrs http.HandlerFunc) http.HandlerFunc {
	return routed(t,
		glRoute{"/notes", jsonRoute(`[]`)},
		glRoute{"/merge_requests", mrs},
		glRoute{"/projects/8", jsonRoute(`{"web_url":"https://gitlab.example.com/forker/widgets"}`)},
		glRoute{"/users", usersRoute()},
	)
}

// mrPageJSON builds a one-request page for the pagination tests.
func mrPageJSON(iid int, createdAt string) string {
	return fmt.Sprintf(`{"iid":%d,"title":"MR %d","description":"","state":"opened",
		"source_branch":"b%d","target_branch":"main","source_project_id":7,"target_project_id":7,
		"author":{"username":"alice"},"created_at":%q}`, iid, iid, iid, createdAt)
}

func TestFetchReview(t *testing.T) {
	server := newGLServer(t, reviewRoutes(t, jsonRoute(reviewMRsJSON)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the bot request)", plan.Filtered)
	}
	if len(plan.PRs) != 3 {
		t.Fatalf("PRs = %d, want 3", len(plan.PRs))
	}

	draft := plan.PRs[0]
	if draft.ExternalID != "10" || draft.Number != 10 || draft.Title != "Add widget" || draft.Body != "Desc" {
		t.Errorf("MR 10 = %+v", draft)
	}
	if draft.State != "open" || !draft.IsDraft {
		t.Errorf("MR 10 state = %q, draft = %v", draft.State, draft.IsDraft)
	}
	if draft.BaseBranch != "main" || draft.HeadBranch != "feature/widget" || draft.HeadSHA != "abc123" {
		t.Errorf("MR 10 branches = %q..%q at %q", draft.BaseBranch, draft.HeadBranch, draft.HeadSHA)
	}
	if len(draft.Labels) != 2 || draft.Labels[0] != "review" {
		t.Errorf("MR 10 labels = %v", draft.Labels)
	}
	wantReviewers := []string{"bob@commit.example.com", "dave@users.noreply." + server.host()}
	if len(draft.Reviewers) != 2 || draft.Reviewers[0] != wantReviewers[0] || draft.Reviewers[1] != wantReviewers[1] {
		t.Errorf("MR 10 reviewers = %v, want %v", draft.Reviewers, wantReviewers)
	}
	if draft.AuthorName != "Alice Example" || draft.AuthorEmail != "alice@example.com" {
		t.Errorf("MR 10 author = %q / %q", draft.AuthorName, draft.AuthorEmail)
	}
	if !draft.CreatedAt.Equal(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("MR 10 created = %v", draft.CreatedAt)
	}
	// A same-project request carries the forge's base too, so its diff pins to it.
	if draft.HeadRepo != "" || draft.BaseSHA != "base010" {
		t.Errorf("MR 10 head repo = %q, base = %q, want no fork and base010", draft.HeadRepo, draft.BaseSHA)
	}

	fork := plan.PRs[1]
	if fork.State != "merged" || fork.MergeCommit != "merge789" {
		t.Errorf("MR 11 state = %q, merge commit = %q", fork.State, fork.MergeCommit)
	}
	if fork.MergedByName != "Carol Example" || fork.MergedByEmail != "carol@private.example.com" {
		t.Errorf("MR 11 merged-by = %q / %q", fork.MergedByName, fork.MergedByEmail)
	}
	if !fork.MergedAt.Equal(time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("MR 11 merged = %v", fork.MergedAt)
	}
	if fork.HeadRepo != "https://gitlab.example.com/forker/widgets" || fork.BaseSHA != "base111" {
		t.Errorf("MR 11 head repo = %q, base = %q", fork.HeadRepo, fork.BaseSHA)
	}
	if fork.Labels == nil || len(fork.Labels) != 0 {
		t.Errorf("MR 11 labels = %v, want an empty slice rather than nil", fork.Labels)
	}

	closed := plan.PRs[2]
	if closed.State != "closed" || !closed.ClosedAt.Equal(time.Date(2024, 7, 2, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("MR 12 state = %q, closed = %v", closed.State, closed.ClosedAt)
	}
	if closed.ClosedByName != "Alice Example" || closed.ClosedByEmail != "alice@example.com" {
		t.Errorf("MR 12 closed-by = %q / %q", closed.ClosedByName, closed.ClosedByEmail)
	}

	if len(plan.Forks) != 1 || plan.Forks[0] != "https://gitlab.example.com/forker/widgets" {
		t.Errorf("Forks = %v, want the one source project", plan.Forks)
	}
	if got := server.count("/projects/8"); got != 1 {
		t.Errorf("fork project lookups = %d, want 1", got)
	}
}

func TestFetchReview_SkipsForkWhenProjectLookupFails(t *testing.T) {
	const forkMR = `[{"iid":11,"title":"Fork work","description":"","state":"opened",
		"source_branch":"fix","target_branch":"main","source_project_id":8,"target_project_id":7,
		"diff_refs":{"base_sha":"base111"},
		"author":{"username":"alice"},"created_at":"2024-06-10T12:00:00Z"}]`
	server := newGLServer(t, routed(t,
		glRoute{"/notes", jsonRoute(`[]`)},
		glRoute{"/merge_requests", jsonRoute(forkMR)},
		glRoute{"/projects/8", statusRoute(http.StatusNotFound, `{"message":"404 Project Not Found"}`)},
		glRoute{"/users", usersRoute()},
	))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.Forks) != 0 {
		t.Errorf("Forks = %v, want none when the project is unreadable", plan.Forks)
	}
	if plan.PRs[0].HeadRepo != "" {
		t.Errorf("head repo = %q, want empty", plan.PRs[0].HeadRepo)
	}
	// The fork base still lands: it comes from the merge request, not the project lookup.
	if plan.PRs[0].BaseSHA != "base111" {
		t.Errorf("base = %q, want base111", plan.PRs[0].BaseSHA)
	}
}

func TestFetchReview_TreatsUnknownStateAsOpen(t *testing.T) {
	const lockedMR = `[{"iid":20,"title":"Locked","description":"","state":"locked",
		"source_branch":"lock","target_branch":"main","source_project_id":7,"target_project_id":7,
		"author":{"username":"alice"},"created_at":"2024-06-10T12:00:00Z"}]`
	server := newGLServer(t, reviewRoutes(t, jsonRoute(lockedMR)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if plan.PRs[0].State != "open" {
		t.Errorf("state = %q, want open for a state GitSocial has no name for", plan.PRs[0].State)
	}
}

func TestFetchReview_FollowsNextPageHeader(t *testing.T) {
	pages := pagedRoute(
		"["+mrPageJSON(1, "2024-06-15T12:00:00Z")+","+mrPageJSON(2, "2024-06-14T12:00:00Z")+"]",
		"["+mrPageJSON(3, "2024-06-13T12:00:00Z")+"]",
	)
	server := newGLServer(t, reviewRoutes(t, pages))
	adapter := newTestAdapter(server)

	var progress int
	plan, err := adapter.FetchReview(importpkg.FetchOptions{OnFetchProgress: func(n int) { progress = n }})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.PRs) != 3 {
		t.Fatalf("PRs = %d, want 3 across both pages", len(plan.PRs))
	}
	if progress != 3 {
		t.Errorf("progress = %d, want 3", progress)
	}
	lists := server.all("/merge_requests")
	if len(lists) != 2 || lists[1].query.Get("page") != "2" {
		t.Errorf("merge request requests = %d, second page query = %q", len(lists), lists[1].query.Get("page"))
	}
}

func TestFetchReview_LimitStopsPaging(t *testing.T) {
	pages := pagedRoute(
		"["+mrPageJSON(1, "2024-06-15T12:00:00Z")+","+mrPageJSON(2, "2024-06-14T12:00:00Z")+"]",
		"["+mrPageJSON(3, "2024-06-13T12:00:00Z")+","+mrPageJSON(4, "2024-06-12T12:00:00Z")+"]",
		"["+mrPageJSON(5, "2024-06-11T12:00:00Z")+"]",
	)
	server := newGLServer(t, reviewRoutes(t, pages))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.PRs) != 3 {
		t.Fatalf("PRs = %d, want the 3 the limit allows", len(plan.PRs))
	}
	if got := server.count("/merge_requests"); got != 2 {
		t.Errorf("merge request requests = %d, want 2 (paging stops at the limit)", got)
	}
}

func TestFetchReview_DecodesUpdatedAt(t *testing.T) {
	const editedMR = `[{"iid":20,"title":"Edited","description":"","state":"opened",
		"source_branch":"edit","target_branch":"main","source_project_id":7,"target_project_id":7,
		"author":{"username":"alice"},"created_at":"2024-06-10T12:00:00Z",
		"updated_at":"2024-07-02T08:30:00Z"}]`
	server := newGLServer(t, reviewRoutes(t, jsonRoute(editedMR)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	// An unchanged request is skipped on re-import only when its UpdatedAt reaches the mapping.
	if !plan.PRs[0].UpdatedAt.Equal(time.Date(2024, 7, 2, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("PR UpdatedAt = %v, want the platform timestamp", plan.PRs[0].UpdatedAt)
	}
}

func TestFetchReview_FiltersBySinceAndMapping(t *testing.T) {
	since := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	server := newGLServer(t, reviewRoutes(t, jsonRoute(reviewMRsJSON)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{
		Since:           &since,
		SkipExternalIDs: map[string]bool{"pr:10": true},
	})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	// MR 11 and 12 predate --since; MR 10 is mapped and never counted.
	if plan.Filtered != 2 {
		t.Errorf("Filtered = %d, want 2", plan.Filtered)
	}
	if len(plan.PRs) != 1 || plan.PRs[0].Number != 13 {
		t.Errorf("PRs = %+v, want only the bot request", plan.PRs)
	}
}

func TestFetchReview_MapsStateToTheGitLabName(t *testing.T) {
	cases := []struct{ state, want string }{
		{"open", "opened"},
		{"closed", "closed"},
		{"merged", "merged"},
		{"all", "all"},
		{"", "all"},
		{"nonsense", "all"},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			server := newGLServer(t, reviewRoutes(t, jsonRoute(`[]`)))
			adapter := newTestAdapter(server)
			if _, err := adapter.FetchReview(importpkg.FetchOptions{State: c.state}); err != nil {
				t.Fatalf("FetchReview() error = %v", err)
			}
			if got := server.first(t, "/merge_requests").query.Get("state"); got != c.want {
				t.Errorf("state query = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFetchReview_PropagatesError(t *testing.T) {
	server := newGLServer(t, reviewRoutes(t, statusRoute(http.StatusUnauthorized, `{"message":"401 Unauthorized"}`)))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchReview() error = nil, want the unauthorized failure")
	}
	if !strings.Contains(err.Error(), "fetch merge requests") || !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want the fetch context and the status", err)
	}
}

func TestFetchReview_ReturnsAPageFailure(t *testing.T) {
	server := newGLServer(t, reviewRoutes(t, failingPageRoute("["+mrPageJSON(1, "2024-06-15T12:00:00Z")+"]")))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchReview() error = nil, want the second page failure")
	}
	if !strings.Contains(err.Error(), "fetch merge requests") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the fetch context and the status", err)
	}
}

func TestFetchReview_MapsForgeTips(t *testing.T) {
	// base_sha is the merge base of the two branches, so it is an ancestor of the head.
	const sameProjectMR = `[{"iid":30,"title":"Tips","description":"","state":"opened",
		"source_branch":"work","target_branch":"main","source_project_id":7,"target_project_id":7,
		"sha":"headsha30","diff_refs":{"base_sha":"basesha30","start_sha":"startsha30"},
		"author":{"username":"alice"},"created_at":"2024-06-10T12:00:00Z"}]`
	server := newGLServer(t, reviewRoutes(t, jsonRoute(sameProjectMR)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.PRs) != 1 {
		t.Fatalf("PRs = %d, want 1", len(plan.PRs))
	}
	if plan.PRs[0].BaseSHA != "basesha30" || plan.PRs[0].HeadSHA != "headsha30" {
		t.Errorf("MR 30 tips = %q..%q, want basesha30..headsha30", plan.PRs[0].BaseSHA, plan.PRs[0].HeadSHA)
	}
}

func TestFetchReview_RejectsMalformedBody(t *testing.T) {
	server := newGLServer(t, reviewRoutes(t, jsonRoute(`{"message":"not an array"}`)))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchReview(importpkg.FetchOptions{}); err == nil {
		t.Fatal("FetchReview() error = nil, want a decode failure")
	}
}
