// social_test.go - FetchSocial decoding of GitHub discussions from canned gh output
package github

import (
	"fmt"
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// socialDiscussionsJSON is one page: a general discussion with a comment and a reply, an announcement, and a bot post.
const socialDiscussionsJSON = `{"data":{"repository":{"discussions":{
	"nodes":[
		{"number":1,"title":"Welcome","body":"Say hello here.",
		 "author":{"login":"alice","name":"Alice GraphQL","email":"alice@example.com"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"body":"Hello","author":{"login":"bob","name":"Bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[{"body":"Nested reply","author":{"login":"alice"},
			  "createdAt":"2024-06-15T14:00:00Z"}]}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":"Y3Vyc29yOnYyOpHOAAAAAQ=="}}},
		{"number":2,"title":"Roadmap","body":"Q3 plans.",
		 "author":{"login":"carol","name":"Carol GraphQL"},
		 "category":{"name":"Announcements","slug":"announcements"},
		 "createdAt":"2024-06-16T12:00:00Z",
		 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
		{"number":3,"title":"Bot notice","body":"Automated.",
		 "author":{"login":"dependabot[bot]"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-17T12:00:00Z",
		 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],
	"pageInfo":{"hasNextPage":false,"endCursor":"Y3Vyc29yOnYyOpHOAAAAAw=="}}}}}`

const socialEmptyJSON = `{"data":{"repository":{"discussions":{
	"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

// discussionPage builds one discussions page holding a single commentless node.
func discussionPage(number int, title, login, createdAt, nextCursor string) string {
	pageInfo := `{"hasNextPage":false,"endCursor":null}`
	if nextCursor != "" {
		pageInfo = fmt.Sprintf(`{"hasNextPage":true,"endCursor":%q}`, nextCursor)
	}
	return fmt.Sprintf(`{"data":{"repository":{"discussions":{
		"nodes":[{"number":%d,"title":%q,"body":"Body.",
		 "author":{"login":%q},"category":{"name":"General","slug":"general"},
		 "createdAt":%q,
		 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":%s}}}}`, number, title, login, createdAt, pageInfo)
}

// findComment returns the planned comment with the given external ID.
func findComment(plan *importpkg.SocialPlan, externalID string) (importpkg.ImportComment, bool) {
	for _, c := range plan.Comments {
		if c.ExternalID == externalID {
			return c, true
		}
	}
	return importpkg.ImportComment{}, false
}

// graphqlQueries returns the query argument of every recorded graphql call.
func graphqlQueries(rec *ghRecorder) []string {
	var queries []string
	for _, call := range rec.calls {
		for _, arg := range call {
			if strings.HasPrefix(arg, "query=") {
				queries = append(queries, arg)
			}
		}
	}
	return queries
}

// socialRoutes answers user lookups from the shared profiles and graphql calls from one responder.
func socialRoutes(t *testing.T, graphql func(args []string) ghResponse) *ghRecorder {
	t.Helper()
	return fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		if ghArg(args, "graphql") {
			return graphql(args)
		}
		t.Errorf("unrouted gh call: %v", args)
		return ghResponse{stdout: "{}"}
	})
}

func TestFetchSocial(t *testing.T) {
	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: socialDiscussionsJSON}
	})

	var progress int
	plan, err := adapter.FetchSocial(importpkg.FetchOptions{
		SkipBots:        true,
		OnFetchProgress: func(n int) { progress = n },
	})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if progress != 3 {
		t.Errorf("progress = %d, want 3 (raw discussions seen)", progress)
	}
	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the bot discussion)", plan.Filtered)
	}
	if len(plan.Posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(plan.Posts))
	}

	first := plan.Posts[0]
	if first.ExternalID != "1" {
		t.Errorf("post 1 ExternalID = %q", first.ExternalID)
	}
	if first.Content != "# Welcome\n\nSay hello here." {
		t.Errorf("post 1 Content = %q, want the title as a heading above the body", first.Content)
	}
	if first.AuthorName != "Alice GraphQL" || first.AuthorEmail != "alice@example.com" {
		t.Errorf("post 1 author = %q / %q", first.AuthorName, first.AuthorEmail)
	}
	if !first.CreatedAt.Equal(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("post 1 CreatedAt = %v", first.CreatedAt)
	}

	// A payload name without an email still costs a lookup, and an unresolvable login falls back to @login.
	second := plan.Posts[1]
	if second.AuthorName != "@carol" || second.AuthorEmail != "carol@users.noreply.github.com" {
		t.Errorf("post 2 author = %q / %q", second.AuthorName, second.AuthorEmail)
	}

	comment, ok := findComment(plan, "1-20240615T130000")
	if !ok {
		t.Fatalf("comments = %+v, want the discussion comment", plan.Comments)
	}
	if comment.PostID != "1" {
		t.Errorf("comment post = %q, want the discussion number", comment.PostID)
	}
	if comment.Content != "Hello" {
		t.Errorf("comment Content = %q", comment.Content)
	}
	if comment.AuthorName != "Bob Example" || comment.AuthorEmail != "bob@users.noreply.github.com" {
		t.Errorf("comment author = %q / %q", comment.AuthorName, comment.AuthorEmail)
	}
	if !comment.CreatedAt.Equal(time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("comment CreatedAt = %v", comment.CreatedAt)
	}

	queries := graphqlQueries(rec)
	if len(queries) != 1 {
		t.Errorf("graphql calls = %d, want 1 (one page, no comment pagination)", len(queries))
	}
	if !strings.Contains(queries[0], `discussions(first: 100`) {
		t.Errorf("query = %q, want a first page of 100", queries[0])
	}
}

// userLookups returns the logins the recorded gh calls looked up.
func userLookups(rec *ghRecorder) []string {
	var logins []string
	for _, call := range rec.calls {
		for _, arg := range call {
			if strings.HasPrefix(arg, "users/") {
				logins = append(logins, strings.TrimPrefix(arg, "users/"))
			}
		}
	}
	return logins
}

func TestFetchSocial_AuthorProfileFromTheQuery(t *testing.T) {
	const wholeAndPartial = `{"data":{"repository":{"discussions":{
		"nodes":[
			{"number":1,"title":"Whole","body":"Body.",
			 "author":{"login":"alice","name":"Alice GraphQL","email":"alice@example.com"},
			 "category":{"name":"General","slug":"general"},
			 "createdAt":"2024-06-15T12:00:00Z",
			 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
			{"number":2,"title":"No email","body":"Body.",
			 "author":{"login":"bob","name":"Bob"},
			 "category":{"name":"General","slug":"general"},
			 "createdAt":"2024-06-16T12:00:00Z",
			 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
			{"number":3,"title":"Bare","body":"Body.",
			 "author":{"login":"carol"},
			 "category":{"name":"General","slug":"general"},
			 "createdAt":"2024-06-17T12:00:00Z",
			 "comments":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: wholeAndPartial}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Posts) != 3 {
		t.Fatalf("posts = %d, want 3", len(plan.Posts))
	}
	whole := plan.Posts[0]
	if whole.AuthorName != "Alice GraphQL" || whole.AuthorEmail != "alice@example.com" {
		t.Errorf("whole author = %q / %q, want the profile the query carried", whole.AuthorName, whole.AuthorEmail)
	}
	if plan.Posts[1].AuthorName != "Bob Example" {
		t.Errorf("author without an email = %q, want the users lookup result", plan.Posts[1].AuthorName)
	}
	if plan.Posts[2].AuthorName != "@carol" {
		t.Errorf("bare author = %q, want the users lookup result", plan.Posts[2].AuthorName)
	}
	lookups := userLookups(rec)
	if len(lookups) != 2 || lookups[0] == "alice" || lookups[1] == "alice" {
		t.Errorf("user lookups = %v, want bob and carol only", lookups)
	}
}

func TestFetchSocial_Filters(t *testing.T) {
	t.Run("category slug", func(t *testing.T) {
		adapter := New("acme", "widgets")
		socialRoutes(t, func([]string) ghResponse {
			return ghResponse{stdout: socialDiscussionsJSON}
		})
		plan, err := adapter.FetchSocial(importpkg.FetchOptions{Categories: []string{"announcements"}})
		if err != nil {
			t.Fatalf("FetchSocial() error = %v", err)
		}
		if len(plan.Posts) != 1 || plan.Posts[0].ExternalID != "2" {
			t.Fatalf("posts = %+v, want only the announcement", plan.Posts)
		}
		if plan.Filtered != 2 {
			t.Errorf("Filtered = %d, want 2 (the general discussions)", plan.Filtered)
		}
		if len(plan.Comments) != 0 {
			t.Errorf("comments = %+v, want none (their discussion was filtered)", plan.Comments)
		}
	})

	t.Run("category name does not match", func(t *testing.T) {
		adapter := New("acme", "widgets")
		socialRoutes(t, func([]string) ghResponse {
			return ghResponse{stdout: socialDiscussionsJSON}
		})
		plan, err := adapter.FetchSocial(importpkg.FetchOptions{Categories: []string{"Announcements"}})
		if err != nil {
			t.Fatalf("FetchSocial() error = %v", err)
		}
		if len(plan.Posts) != 0 {
			t.Errorf("posts = %+v, want none (the filter reads the slug)", plan.Posts)
		}
	})

	t.Run("since", func(t *testing.T) {
		adapter := New("acme", "widgets")
		socialRoutes(t, func([]string) ghResponse {
			return ghResponse{stdout: socialDiscussionsJSON}
		})
		since := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC)
		plan, err := adapter.FetchSocial(importpkg.FetchOptions{Since: &since})
		if err != nil {
			t.Fatalf("FetchSocial() error = %v", err)
		}
		if len(plan.Posts) != 2 {
			t.Fatalf("posts = %+v, want the two discussions after Since", plan.Posts)
		}
		if plan.Filtered != 1 {
			t.Errorf("Filtered = %d, want 1 (the older discussion)", plan.Filtered)
		}
	})

	t.Run("mapped ids", func(t *testing.T) {
		adapter := New("acme", "widgets")
		socialRoutes(t, func([]string) ghResponse {
			return ghResponse{stdout: socialDiscussionsJSON}
		})
		plan, err := adapter.FetchSocial(importpkg.FetchOptions{
			SkipExternalIDs: map[string]bool{"post:1": true},
		})
		if err != nil {
			t.Fatalf("FetchSocial() error = %v", err)
		}
		for _, post := range plan.Posts {
			if post.ExternalID == "1" {
				t.Error("discussion 1 was planned despite being mapped")
			}
		}
		// A mapped discussion still plans its comments, so new ones arrive without --update.
		if len(plan.Comments) != 2 {
			t.Errorf("comments = %+v, want the mapped discussion's comment and reply", plan.Comments)
		}
		// Mapped items are skipped, not counted as filtered by category or Since.
		if plan.Filtered != 0 {
			t.Errorf("Filtered = %d, want 0", plan.Filtered)
		}
	})

	t.Run("mapped comment", func(t *testing.T) {
		adapter := New("acme", "widgets")
		socialRoutes(t, func([]string) ghResponse {
			return ghResponse{stdout: socialDiscussionsJSON}
		})
		plan, err := adapter.FetchSocial(importpkg.FetchOptions{
			SkipExternalIDs: map[string]bool{"comment:1-20240615T130000": true},
		})
		if err != nil {
			t.Fatalf("FetchSocial() error = %v", err)
		}
		if len(plan.Posts) != 3 {
			t.Errorf("posts = %d, want 3", len(plan.Posts))
		}
		if _, ok := findComment(plan, "1-20240615T130000"); ok {
			t.Error("the mapped comment was planned again")
		}
	})
}

func TestFetchSocial_Empty(t *testing.T) {
	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: socialEmptyJSON}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Posts) != 0 || len(plan.Comments) != 0 || plan.Filtered != 0 {
		t.Errorf("plan = %+v, want an empty plan", plan)
	}
	if rec.callCount() != 1 {
		t.Errorf("gh calls = %d, want 1 (no user lookups)", rec.callCount())
	}
}

func TestFetchSocial_PropagatesFetchError(t *testing.T) {
	adapter := New("acme", "widgets")
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stderr: "gh: Not Found (HTTP 404)", exitCode: 1}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchSocial() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "fetch discussions") {
		t.Errorf("error = %q, want the fetch context", err)
	}
	if plan != nil {
		t.Errorf("plan = %+v, want nil on failure", plan)
	}
}

func TestFetchSocial_PaginatesDiscussions(t *testing.T) {
	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func(args []string) ghResponse {
		if ghArg(args, `after: "CURSOR1"`) {
			return ghResponse{stdout: discussionPage(2, "Second page", "bob", "2024-06-16T12:00:00Z", "")}
		}
		return ghResponse{stdout: discussionPage(1, "First page", "alice", "2024-06-15T12:00:00Z", "CURSOR1")}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Posts) != 2 {
		t.Fatalf("posts = %+v, want both pages", plan.Posts)
	}
	if plan.Posts[0].ExternalID != "1" || plan.Posts[1].ExternalID != "2" {
		t.Errorf("posts = %+v, want page order preserved", plan.Posts)
	}
	queries := graphqlQueries(rec)
	if len(queries) != 2 {
		t.Fatalf("graphql calls = %d, want 2", len(queries))
	}
	if strings.Contains(queries[0], "after:") {
		t.Errorf("first query = %q, want no cursor", queries[0])
	}
	if !strings.Contains(queries[1], `after: "CURSOR1"`) {
		t.Errorf("second query = %q, want the end cursor of page 1", queries[1])
	}
}

func TestFetchSocial_PartialPageFailureKeepsResults(t *testing.T) {
	adapter := New("acme", "widgets")
	socialRoutes(t, func(args []string) ghResponse {
		if ghArg(args, `after: "CURSOR1"`) {
			return ghResponse{stderr: "gh: Internal Server Error (HTTP 500)", exitCode: 1}
		}
		return ghResponse{stdout: discussionPage(1, "First page", "alice", "2024-06-15T12:00:00Z", "CURSOR1")}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v, want the first page kept", err)
	}
	if len(plan.Posts) != 1 || plan.Posts[0].ExternalID != "1" {
		t.Errorf("posts = %+v, want the page fetched before the failure", plan.Posts)
	}
}

func TestFetchSocial_LimitCapsPageSize(t *testing.T) {
	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: discussionPage(1, "Only one", "alice", "2024-06-15T12:00:00Z", "CURSOR1")}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{Limit: 1})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Posts) != 1 {
		t.Errorf("posts = %d, want 1", len(plan.Posts))
	}
	queries := graphqlQueries(rec)
	if len(queries) != 1 {
		t.Fatalf("graphql calls = %d, want 1 (the limit is reached)", len(queries))
	}
	if !strings.Contains(queries[0], "discussions(first: 1,") {
		t.Errorf("query = %q, want a page sized to the limit", queries[0])
	}
}

func TestFetchSocial_PaginatesComments(t *testing.T) {
	const firstPage = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":4,"title":"Long thread","body":"Start.",
		 "author":{"login":"alice"},"category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"body":"First","author":{"login":"bob"},"createdAt":"2024-06-15T13:00:00Z"}],
		  "pageInfo":{"hasNextPage":true,"endCursor":"CCUR1"}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
	const morePage = `{"data":{"repository":{"discussion":{"comments":{
		"nodes":[{"body":"Second","author":{"login":"alice"},"createdAt":"2024-06-15T14:00:00Z"}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`

	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func(args []string) ghResponse {
		if ghArg(args, "discussion(number: 4)") {
			return ghResponse{stdout: morePage}
		}
		return ghResponse{stdout: firstPage}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Comments) != 2 {
		t.Fatalf("comments = %+v, want both comment pages", plan.Comments)
	}
	if plan.Comments[0].Content != "First" || plan.Comments[1].Content != "Second" {
		t.Errorf("comments = %+v, want page order preserved", plan.Comments)
	}
	if plan.Comments[1].ExternalID != "4-20240615T140000" || plan.Comments[1].PostID != "4" {
		t.Errorf("second comment id = %q, post = %q", plan.Comments[1].ExternalID, plan.Comments[1].PostID)
	}
	queries := graphqlQueries(rec)
	if len(queries) != 2 {
		t.Fatalf("graphql calls = %d, want 2", len(queries))
	}
	if !strings.Contains(queries[1], `comments(first: 100, after: "CCUR1")`) {
		t.Errorf("comment query = %q, want the comment end cursor", queries[1])
	}
}

func TestFetchSocial_PaginatesReplies(t *testing.T) {
	const firstPage = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":6,"title":"Reply thread","body":"Start.",
		 "author":{"login":"alice"},"category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"id":"DC_1","body":"Top","author":{"login":"bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[
				{"id":"DC_2","body":"First reply","author":{"login":"bob"},
				 "createdAt":"2024-06-15T13:30:00Z"}],
			  "pageInfo":{"hasNextPage":true,"endCursor":"RCUR1"}}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
	const morePage = `{"data":{"node":{"replies":{
		"nodes":[{"id":"DC_3","body":"Second reply","author":{"login":"alice"},
		 "createdAt":"2024-06-15T14:00:00Z"}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

	adapter := New("acme", "widgets")
	rec := socialRoutes(t, func(args []string) ghResponse {
		if ghArg(args, `node(id: "DC_1")`) {
			return ghResponse{stdout: morePage}
		}
		return ghResponse{stdout: firstPage}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Comments) != 3 {
		t.Fatalf("comments = %+v, want the comment and both reply pages", plan.Comments)
	}
	top := plan.Comments[0]
	if top.ParentID != "" {
		t.Errorf("top comment ParentID = %q, want none", top.ParentID)
	}
	for _, reply := range plan.Comments[1:] {
		if reply.ParentID != top.ExternalID {
			t.Errorf("reply %q ParentID = %q, want the top comment %q", reply.Content, reply.ParentID, top.ExternalID)
		}
	}
	queries := graphqlQueries(rec)
	if len(queries) != 2 {
		t.Fatalf("graphql calls = %d, want 2", len(queries))
	}
	if !strings.Contains(queries[1], `replies(first: 100, after: "RCUR1")`) {
		t.Errorf("reply query = %q, want the reply end cursor", queries[1])
	}
}

func TestFetchSocial_CommentPageFailureKeepsFirstPage(t *testing.T) {
	const firstPage = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":5,"title":"Thread","body":"Start.",
		 "author":{"login":"alice"},"category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"body":"First","author":{"login":"bob"},"createdAt":"2024-06-15T13:00:00Z"}],
		  "pageInfo":{"hasNextPage":true,"endCursor":"CCUR1"}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

	adapter := New("acme", "widgets")
	socialRoutes(t, func(args []string) ghResponse {
		if ghArg(args, "discussion(number: 5)") {
			return ghResponse{stderr: "gh: Internal Server Error (HTTP 500)", exitCode: 1}
		}
		return ghResponse{stdout: firstPage}
	})

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Comments) != 1 || plan.Comments[0].Content != "First" {
		t.Errorf("comments = %+v, want the comments fetched before the failure", plan.Comments)
	}
}
