// notes_test.go - Issue and merge request notes planned as comments
package gitlab

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// issueNotesJSON holds a note, a system note, and a bot note on issue 1.
const issueNotesJSON = `[
	{"id":301,"body":"Looks good","system":false,"author":{"username":"bob"},
	 "created_at":"2024-06-16T09:00:00Z"},
	{"id":302,"body":"changed the description","system":true,"author":{"username":"alice"},
	 "created_at":"2024-06-16T10:00:00Z"},
	{"id":303,"body":"Automated ping","system":false,"author":{"username":"gitlab-bot"},
	 "created_at":"2024-06-16T11:00:00Z"}
]`

// notesRoute answers the notes endpoint from a table keyed by item IID.
func notesRoute(byIID map[string]string) http.HandlerFunc {
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

// pmNotesRoutes serves the PM endpoints with the given notes table.
func pmNotesRoutes(t *testing.T, issues http.HandlerFunc, byIID map[string]string) http.HandlerFunc {
	return routed(t,
		glRoute{"/notes", notesRoute(byIID)},
		glRoute{"/links", issueLinksRoute(nil)},
		glRoute{"/milestones", jsonRoute(pmMilestonesJSON)},
		glRoute{"/issues", issues},
		glRoute{"/users", usersRoute()},
		glRoute{"/api/graphql", graphqlUnavailableRoute()},
	)
}

func TestFetchPM_PlansIssueNotes(t *testing.T) {
	server := newGLServer(t, pmNotesRoutes(t, jsonRoute(pmIssuesJSON), map[string]string{"1": issueNotesJSON}))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Comments) != 1 {
		t.Fatalf("comments = %+v, want the one note that is neither a system note nor a bot's", plan.Comments)
	}
	comment := plan.Comments[0]
	if comment.ExternalID != "301" || comment.PostID != "1" {
		t.Errorf("comment ids = %q on %q, want 301 on 1", comment.ExternalID, comment.PostID)
	}
	if comment.Content != "Looks good" {
		t.Errorf("comment content = %q", comment.Content)
	}
	if comment.AuthorName != "Bob Example" || comment.AuthorEmail != "bob@commit.example.com" {
		t.Errorf("comment author = %q / %q, want the profile lookup", comment.AuthorName, comment.AuthorEmail)
	}
	if !comment.CreatedAt.Equal(time.Date(2024, 6, 16, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("comment created = %v", comment.CreatedAt)
	}
	if comment.ParentID != "" {
		t.Errorf("comment ParentID = %q, want none: GitLab notes hang off the item", comment.ParentID)
	}
	if got := server.first(t, "/notes").query.Get("per_page"); got != "100" {
		t.Errorf("notes per_page = %q, want 100", got)
	}
}

func TestFetchPM_SkipsMappedNotes(t *testing.T) {
	server := newGLServer(t, pmNotesRoutes(t, jsonRoute(pmIssuesJSON), map[string]string{"1": issueNotesJSON}))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{
		SkipExternalIDs: map[string]bool{"issue-comment:301": true},
	})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	for _, c := range plan.Comments {
		if c.ExternalID == "301" {
			t.Error("note 301 was planned despite being mapped")
		}
	}
	if len(plan.Comments) != 1 || plan.Comments[0].ExternalID != "303" {
		t.Errorf("comments = %+v, want the bot note alone", plan.Comments)
	}
}

func TestFetchPM_ToleratesUnreadableNotes(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"/notes", statusRoute(http.StatusNotFound, `{"message":"404 Not Found"}`)},
		glRoute{"/links", issueLinksRoute(nil)},
		glRoute{"/milestones", jsonRoute(pmMilestonesJSON)},
		glRoute{"/issues", jsonRoute(pmIssuesJSON)},
		glRoute{"/users", usersRoute()},
		glRoute{"/api/graphql", graphqlUnavailableRoute()},
	))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchPM(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchPM() error = %v, want the note failure to be tolerated", err)
	}
	if len(plan.Issues) != 3 {
		t.Fatalf("issues = %d, want 3", len(plan.Issues))
	}
	if len(plan.Comments) != 0 {
		t.Errorf("comments = %+v, want none", plan.Comments)
	}
}

func TestFetchReview_PlansMergeRequestNotes(t *testing.T) {
	const mrNotesJSON = `[
		{"id":401,"body":"Please rebase","system":false,"author":{"username":"alice"},
		 "created_at":"2024-06-16T09:00:00Z"},
		{"id":402,"body":"assigned to @bob","system":true,"author":{"username":"alice"},
		 "created_at":"2024-06-16T10:00:00Z"}
	]`
	server := newGLServer(t, routed(t,
		glRoute{"/notes", notesRoute(map[string]string{"10": mrNotesJSON})},
		glRoute{"/merge_requests", jsonRoute(reviewMRsJSON)},
		glRoute{"/projects/8", jsonRoute(`{"web_url":"https://gitlab.example.com/forker/widgets"}`)},
		glRoute{"/users", usersRoute()},
	))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReview(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.Comments) != 1 {
		t.Fatalf("comments = %+v, want the one note that is not a system note", plan.Comments)
	}
	comment := plan.Comments[0]
	if comment.ExternalID != "401" || comment.PostID != "10" {
		t.Errorf("comment ids = %q on %q, want 401 on 10", comment.ExternalID, comment.PostID)
	}
	if comment.Content != "Please rebase" {
		t.Errorf("comment content = %q", comment.Content)
	}
	if comment.AuthorName != "Alice Example" || comment.AuthorEmail != "alice@example.com" {
		t.Errorf("comment author = %q / %q, want the profile lookup", comment.AuthorName, comment.AuthorEmail)
	}
}

func TestFetchReview_SkipsNotesOfMappedRequests(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"/notes", notesRoute(nil)},
		glRoute{"/merge_requests", jsonRoute(reviewMRsJSON)},
		glRoute{"/projects/8", jsonRoute(`{"web_url":"https://gitlab.example.com/forker/widgets"}`)},
		glRoute{"/users", usersRoute()},
	))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchReview(importpkg.FetchOptions{
		SkipExternalIDs: map[string]bool{"pr:10": true},
	}); err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	for _, r := range server.all("/notes") {
		if strings.Contains(r.path, "/10/notes") {
			t.Error("the mapped merge request was asked for its notes")
		}
	}
}
