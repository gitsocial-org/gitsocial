// issues_test.go - FetchPM against the fake GitLab REST and GraphQL endpoints
package gitlab

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// pmMilestonesJSON holds an active milestone with a due date, a closed one, and one with an unparsable date.
const pmMilestonesJSON = `[
	{"id":11,"title":"v1.0","description":"First milestone","state":"active",
	 "due_date":"2024-12-31","created_at":"2024-06-01T00:00:00Z"},
	{"id":12,"title":"v0.9","description":"Shipped","state":"closed",
	 "due_date":null,"created_at":"2024-01-01T00:00:00Z"},
	{"id":13,"title":"v2.0","description":"","state":"active",
	 "due_date":"not-a-date","created_at":"2024-06-02T00:00:00Z"}
]`

// pmIssuesJSON holds an open issue, a closed one with a closer, and a bot issue.
const pmIssuesJSON = `[
	{"iid":1,"title":"First issue","description":"Body one","state":"opened",
	 "labels":["bug","p1"],"author":{"username":"alice","name":"Alice"},
	 "assignees":[{"username":"bob"},{"username":"dave"}],
	 "milestone":{"title":"v1.0"},"created_at":"2024-06-15T12:00:00Z"},
	{"iid":2,"title":"Second issue","description":"","state":"closed",
	 "author":{"username":"bob","name":"Bob"},"closed_by":{"username":"carol"},
	 "created_at":"2024-06-16T12:00:00Z","closed_at":"2024-07-01T09:30:00Z"},
	{"iid":3,"title":"Bot noise","description":"","state":"opened",
	 "author":{"username":"renovate[bot]"},"created_at":"2024-06-17T12:00:00Z"}
]`

// pmGraphQLJSON gives issue 1 one blocking and one blocked-by link.
const pmGraphQLJSON = `{"data":{"project":{"issues":{
	"nodes":[{"iid":"1","blockedByIssues":{"nodes":[{"iid":"2"}]},
	          "blockingIssues":{"nodes":[{"iid":"3"}]}}],
	"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`

// issueLinksRoute answers the per-issue links endpoint from a table keyed by issue IID.
func issueLinksRoute(byIID map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.EscapedPath(), "/")
		iid := ""
		if len(parts) >= 2 {
			iid = parts[len(parts)-2]
		}
		body, ok := byIID[iid]
		if !ok {
			body = `[]`
		}
		_, _ = io.WriteString(w, body)
	}
}

// pmRoutes serves the milestone, issue, note, user, link and GraphQL endpoints FetchPM walks.
func pmRoutes(t *testing.T, issues http.HandlerFunc, graphql http.HandlerFunc, links map[string]string) http.HandlerFunc {
	return routed(t,
		glRoute{"/notes", jsonRoute(`[]`)},
		glRoute{"/links", issueLinksRoute(links)},
		glRoute{"/milestones", jsonRoute(pmMilestonesJSON)},
		glRoute{"/issues", issues},
		glRoute{"/users", usersRoute()},
		glRoute{"/api/graphql", graphql},
	)
}

// issuePageJSON builds a one-issue page for the pagination tests.
func issuePageJSON(iid int, createdAt string) string {
	return fmt.Sprintf(`{"iid":%d,"title":"Issue %d","description":"","state":"opened",
		"author":{"username":"alice"},"created_at":%q}`, iid, iid, createdAt)
}

func TestFetchPM(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), jsonRoute(pmGraphQLJSON), map[string]string{
		"1": `[{"iid":4,"link_type":"relates_to"},{"iid":5,"link_type":"blocks"}]`,
		"2": `[{"iid":1,"link_type":"is_blocked_by"}]`,
	}))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}

	if len(plan.Milestones) != 3 {
		t.Fatalf("milestones = %d, want 3", len(plan.Milestones))
	}
	first := plan.Milestones[0]
	if first.ExternalID != "v1.0" || first.Number != 11 || first.Title != "v1.0" || first.Body != "First milestone" {
		t.Errorf("milestone 1 = %+v", first)
	}
	if first.State != "open" {
		t.Errorf("milestone 1 state = %q, want open", first.State)
	}
	if first.DueDate == nil || !first.DueDate.Equal(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("milestone 1 due date = %v", first.DueDate)
	}
	if !first.CreatedAt.Equal(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("milestone 1 created = %v", first.CreatedAt)
	}
	if plan.Milestones[1].State != "closed" || plan.Milestones[1].DueDate != nil {
		t.Errorf("milestone 2 = %+v, want closed with no due date", plan.Milestones[1])
	}
	if plan.Milestones[2].DueDate != nil {
		t.Errorf("milestone 3 due date = %v, want nil for an unparsable date", plan.Milestones[2].DueDate)
	}

	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the bot issue)", plan.Filtered)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(plan.Issues))
	}
	open := plan.Issues[0]
	if open.ExternalID != "1" || open.Number != 1 || open.Title != "First issue" || open.Body != "Body one" {
		t.Errorf("issue 1 = %+v", open)
	}
	if open.State != "open" || open.MilestoneID != "v1.0" {
		t.Errorf("issue 1 state = %q, milestone = %q", open.State, open.MilestoneID)
	}
	if len(open.Labels) != 2 || open.Labels[0] != "bug" || open.Labels[1] != "p1" {
		t.Errorf("issue 1 labels = %v", open.Labels)
	}
	if open.AuthorName != "Alice Example" || open.AuthorEmail != "alice@example.com" {
		t.Errorf("issue 1 author = %q / %q, want the profile lookup", open.AuthorName, open.AuthorEmail)
	}
	if !open.CreatedAt.Equal(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("issue 1 created = %v", open.CreatedAt)
	}
	wantAssignees := []string{"bob@commit.example.com", "dave@users.noreply." + server.host()}
	if len(open.Assignees) != 2 || open.Assignees[0] != wantAssignees[0] || open.Assignees[1] != wantAssignees[1] {
		t.Errorf("issue 1 assignees = %v, want %v", open.Assignees, wantAssignees)
	}

	closed := plan.Issues[1]
	if closed.State != "closed" || !closed.ClosedAt.Equal(time.Date(2024, 7, 1, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("issue 2 state = %q, closed = %v", closed.State, closed.ClosedAt)
	}
	if closed.ClosedByName != "Carol Example" || closed.ClosedByEmail != "carol@private.example.com" {
		t.Errorf("issue 2 closed-by = %q / %q", closed.ClosedByName, closed.ClosedByEmail)
	}

	// GraphQL supplies the blocking links, REST adds relates_to on top.
	if len(open.BlocksIDs) != 1 || open.BlocksIDs[0] != "3" {
		t.Errorf("issue 1 BlocksIDs = %v, want the GraphQL link", open.BlocksIDs)
	}
	if len(open.BlockedByIDs) != 1 || open.BlockedByIDs[0] != "2" {
		t.Errorf("issue 1 BlockedByIDs = %v, want the GraphQL link", open.BlockedByIDs)
	}
	if len(open.RelatedIDs) != 1 || open.RelatedIDs[0] != "4" {
		t.Errorf("issue 1 RelatedIDs = %v, want the REST link", open.RelatedIDs)
	}
	if len(closed.BlockedByIDs) != 1 || closed.BlockedByIDs[0] != "1" {
		t.Errorf("issue 2 BlockedByIDs = %v, want REST to fill the GraphQL gap", closed.BlockedByIDs)
	}
}

func TestFetchPM_ResolvesUsersOncePerUsername(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchPM(importpkg.FetchOptions{}); err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	seen := map[string]int{}
	for _, r := range server.all("/users") {
		seen[r.query.Get("username")]++
	}
	for username, n := range seen {
		if n != 1 {
			t.Errorf("looked up %q %d times, want 1 (the profile cache)", username, n)
		}
	}
	if seen["bob"] != 1 {
		t.Errorf("bob lookups = %d, want 1 across author and assignee", seen["bob"])
	}
}

func TestFetchPM_UnknownUserFallsBackToNoreply(t *testing.T) {
	const issuesJSON = `[{"iid":1,"title":"Ghost","description":"","state":"opened",
		"author":{"username":"ghost"},"created_at":"2024-06-15T12:00:00Z"}]`
	server := newGLServer(t, pmRoutes(t, jsonRoute(issuesJSON), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(plan.Issues))
	}
	if plan.Issues[0].AuthorName != "@ghost" {
		t.Errorf("author name = %q, want @ghost", plan.Issues[0].AuthorName)
	}
	if want := "ghost@users.noreply." + server.host(); plan.Issues[0].AuthorEmail != want {
		t.Errorf("author email = %q, want %q", plan.Issues[0].AuthorEmail, want)
	}
}

func TestFetchPM_FallsBackToRESTLinks(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphqlUnavailableRoute(), map[string]string{
		"1": `[{"iid":4,"link_type":"relates_to"},{"iid":5,"link_type":"blocks"},{"iid":6,"link_type":"is_blocked_by"}]`,
	}))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	issue := plan.Issues[0]
	if len(issue.BlocksIDs) != 1 || issue.BlocksIDs[0] != "5" {
		t.Errorf("BlocksIDs = %v, want the REST link", issue.BlocksIDs)
	}
	if len(issue.BlockedByIDs) != 1 || issue.BlockedByIDs[0] != "6" {
		t.Errorf("BlockedByIDs = %v, want the REST link", issue.BlockedByIDs)
	}
	if len(issue.RelatedIDs) != 1 || issue.RelatedIDs[0] != "4" {
		t.Errorf("RelatedIDs = %v, want the REST link", issue.RelatedIDs)
	}
}

func TestFetchPM_IgnoresUnreadableLinks(t *testing.T) {
	links := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
	}
	server := newGLServer(t, routed(t,
		glRoute{"/notes", jsonRoute(`[]`)},
		glRoute{"/links", links},
		glRoute{"/milestones", jsonRoute(pmMilestonesJSON)},
		glRoute{"/issues", jsonRoute(pmIssuesJSON)},
		glRoute{"/users", usersRoute()},
		glRoute{"/api/graphql", graphqlUnavailableRoute()},
	))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v, want the link failure to be tolerated", err)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(plan.Issues))
	}
	if len(plan.Issues[0].RelatedIDs) != 0 || len(plan.Issues[0].BlocksIDs) != 0 {
		t.Errorf("issue 1 links = %+v, want none", plan.Issues[0])
	}
}

func TestFetchPM_GraphQLErrorEnvelopeFallsBackToREST(t *testing.T) {
	const errorEnvelope = `{"errors":[{"message":"Field 'blockingIssues' does not exist"}]}`
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), jsonRoute(errorEnvelope), map[string]string{
		"1": `[{"iid":9,"link_type":"blocks"}]`,
	}))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues[0].BlocksIDs) != 1 || plan.Issues[0].BlocksIDs[0] != "9" {
		t.Errorf("BlocksIDs = %v, want the REST link after a GraphQL error", plan.Issues[0].BlocksIDs)
	}
}

func TestFetchPM_PagesTheGraphQLLinkQuery(t *testing.T) {
	page1 := `{"data":{"project":{"issues":{
		"nodes":[{"iid":"1","blockedByIssues":{"nodes":[]},"blockingIssues":{"nodes":[{"iid":"7"}]}}],
		"pageInfo":{"hasNextPage":true,"endCursor":"cursor-2"}}}}}`
	page2 := `{"data":{"project":{"issues":{
		"nodes":[{"iid":"2","blockedByIssues":{"nodes":[{"iid":"8"}]},"blockingIssues":{"nodes":[]}}],
		"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`
	calls := 0
	graphql := func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, page1)
			return
		}
		_, _ = io.WriteString(w, page2)
	}
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphql, nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if got := server.count("/api/graphql"); got != 2 {
		t.Fatalf("graphql calls = %d, want 2", got)
	}
	posts := server.all("/api/graphql")
	if posts[0].method != http.MethodPost {
		t.Errorf("graphql method = %q, want POST", posts[0].method)
	}
	if !strings.Contains(posts[0].body, `"fullPath":"acme/widgets"`) {
		t.Errorf("graphql body = %q, want the project path variable", posts[0].body)
	}
	if !strings.Contains(posts[1].body, `"after":"cursor-2"`) {
		t.Errorf("second graphql body = %q, want the end cursor", posts[1].body)
	}
	if len(plan.Issues[0].BlocksIDs) != 1 || plan.Issues[0].BlocksIDs[0] != "7" {
		t.Errorf("issue 1 BlocksIDs = %v, want the first page link", plan.Issues[0].BlocksIDs)
	}
	if len(plan.Issues[1].BlockedByIDs) != 1 || plan.Issues[1].BlockedByIDs[0] != "8" {
		t.Errorf("issue 2 BlockedByIDs = %v, want the second page link", plan.Issues[1].BlockedByIDs)
	}
}

func TestFetchPM_FollowsNextPageHeader(t *testing.T) {
	pages := pagedRoute(
		"["+issuePageJSON(1, "2024-06-15T12:00:00Z")+","+issuePageJSON(2, "2024-06-14T12:00:00Z")+"]",
		"["+issuePageJSON(3, "2024-06-13T12:00:00Z")+","+issuePageJSON(4, "2024-06-12T12:00:00Z")+"]",
	)
	server := newGLServer(t, pmRoutes(t, pages, graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	var progress int
	plan, err := adapter.FetchPM(importpkg.FetchOptions{OnFetchProgress: func(n int) { progress = n }})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues) != 4 {
		t.Fatalf("issues = %d, want 4 across both pages", len(plan.Issues))
	}
	if progress != 4 {
		t.Errorf("progress = %d, want 4", progress)
	}
	lists := server.all("/issues")
	if len(lists) != 2 {
		t.Fatalf("issue list requests = %d, want 2", len(lists))
	}
	if got := lists[0].query.Get("page"); got != "" {
		t.Errorf("first page query page = %q, want unset", got)
	}
	if got := lists[1].query.Get("page"); got != "2" {
		t.Errorf("second page query page = %q, want 2 from X-Next-Page", got)
	}
	if got := lists[0].query.Get("per_page"); got != "100" {
		t.Errorf("per_page = %q, want 100", got)
	}
}

func TestFetchPM_FollowsTheKeysetLinkHeader(t *testing.T) {
	pages := keysetRoute("projects/acme%2Fwidgets/issues",
		"["+issuePageJSON(1, "2024-06-15T12:00:00Z")+"]",
		"["+issuePageJSON(2, "2024-06-14T12:00:00Z")+"]",
	)
	server := newGLServer(t, pmRoutes(t, pages, graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("issues = %d, want 2 across both pages", len(plan.Issues))
	}
	lists := server.all("/issues")
	if len(lists) != 2 {
		t.Fatalf("issue list requests = %d, want 2", len(lists))
	}
	if got := lists[1].query.Get("cursor"); got != "2" {
		t.Errorf("second request cursor = %q, want 2 from the Link header", got)
	}
	if got := lists[1].query.Get("pagination"); got != "keyset" {
		t.Errorf("second request pagination = %q, want keyset", got)
	}
}

func TestFetchPM_LimitStopsPaging(t *testing.T) {
	pages := pagedRoute(
		"["+issuePageJSON(1, "2024-06-15T12:00:00Z")+","+issuePageJSON(2, "2024-06-14T12:00:00Z")+"]",
		"["+issuePageJSON(3, "2024-06-13T12:00:00Z")+","+issuePageJSON(4, "2024-06-12T12:00:00Z")+"]",
		"["+issuePageJSON(5, "2024-06-11T12:00:00Z")+"]",
	)
	server := newGLServer(t, pmRoutes(t, pages, graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues) != 3 {
		t.Fatalf("issues = %d, want the 3 the limit allows", len(plan.Issues))
	}
	if got := server.count("/issues"); got != 2 {
		t.Errorf("issue list requests = %d, want 2 (paging stops at the limit)", got)
	}
	if plan.Issues[2].Number != 3 {
		t.Errorf("last issue = %d, want 3 (the newest three)", plan.Issues[2].Number)
	}
}

func TestFetchPM_FiltersBySince(t *testing.T) {
	since := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC)
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{Since: &since})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the issue before --since)", plan.Filtered)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(plan.Issues))
	}
	for _, issue := range plan.Issues {
		if issue.Number == 1 {
			t.Error("issue 1 survived the --since filter")
		}
	}
}

func TestFetchPM_SkipsMappedExternalIDs(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{
		SkipExternalIDs: map[string]bool{"issue:1": true, "milestone:v1.0": true},
	})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	for _, m := range plan.Milestones {
		if m.ExternalID == "v1.0" {
			t.Error("milestone v1.0 was planned despite being mapped")
		}
	}
	for _, issue := range plan.Issues {
		if issue.ExternalID == "1" {
			t.Error("issue 1 was planned despite being mapped")
		}
	}
	// Mapped items are skipped, not counted as filtered by --since or --skip-bots.
	if plan.Filtered != 0 {
		t.Errorf("Filtered = %d, want 0", plan.Filtered)
	}
}

func TestFetchPM_MapsStateToTheGitLabName(t *testing.T) {
	cases := []struct{ state, want string }{
		{"open", "opened"},
		{"closed", "closed"},
		{"all", "all"},
		{"", "all"},
		{"merged", "all"},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			server := newGLServer(t, pmRoutes(t, jsonRoute(`[]`), graphqlUnavailableRoute(), nil))
			adapter := newTestAdapter(server)
			if _, err := adapter.FetchPM(importpkg.FetchOptions{State: c.state}); err != nil {
				t.Fatalf("FetchPM() error = %v", err)
			}
			if got := server.first(t, "/issues").query.Get("state"); got != c.want {
				t.Errorf("state query = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFetchPM_EmptyProject(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(`[]`), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Issues) != 0 || plan.Filtered != 0 {
		t.Errorf("plan = %+v, want no issues", plan)
	}
	if got := server.count("/links"); got != 0 {
		t.Errorf("link requests = %d, want none", got)
	}
}

func TestFetchPM_PropagatesMilestoneError(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"/milestones", statusRoute(http.StatusNotFound, `{"message":"404 Project Not Found"}`)},
	))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchPM() error = nil, want the milestone failure")
	}
	if !strings.Contains(err.Error(), "fetch milestones") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the milestone context and the status", err)
	}
}

func TestFetchPM_PropagatesIssueError(t *testing.T) {
	server := newGLServer(t, pmRoutes(t,
		statusRoute(http.StatusUnauthorized, `{"message":"401 Unauthorized"}`),
		graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchPM() error = nil, want the unauthorized failure")
	}
	if !strings.Contains(err.Error(), "fetch issues") || !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want the issue context and the status", err)
	}
}

func TestFetchPM_ReturnsAPageFailure(t *testing.T) {
	first := "[" + issuePageJSON(1, "2024-06-15T12:00:00Z") + "]"
	server := newGLServer(t, pmRoutes(t, failingPageRoute(first), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchPM() error = nil, want the second page failure")
	}
	if !strings.Contains(err.Error(), "fetch issues") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the issue context and the status", err)
	}
}

func TestFetchPM_RejectsMalformedBody(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(`{"message":"not an array"}`), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchPM(importpkg.FetchOptions{}); err == nil {
		t.Fatal("FetchPM() error = nil, want a decode failure")
	}
}

func TestFetchPM_RejectsTruncatedPage(t *testing.T) {
	truncated := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"iid":1,"title":"Half a page"`)
	}
	server := newGLServer(t, pmRoutes(t, truncated, graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchPM(importpkg.FetchOptions{}); err == nil {
		t.Fatal("FetchPM() error = nil, want a decode failure on the cut body")
	}
}
