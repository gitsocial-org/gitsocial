// adapter_test.go - Adapter wiring: counts, metadata, authentication, hosts and retries
package gitlab

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// totalRoute answers with an X-Total header and an empty page, the shape CountItems reads.
func totalRoute(total string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if total != "" {
			w.Header().Set("X-Total", total)
		}
		_, _ = w.Write([]byte(`[]`))
	}
}

func TestPlatform(t *testing.T) {
	if got := New("acme", "widgets", AdapterOptions{}).Platform(); got != "gitlab" {
		t.Errorf("Platform() = %q, want gitlab", got)
	}
}

func TestCountItems(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"/issues", totalRoute("42")},
		glRoute{"/merge_requests", totalRoute("7")},
		glRoute{"/releases", totalRoute("3")},
	))
	adapter := newTestAdapter(server)

	counts, err := adapter.CountItems(importpkg.FetchOptions{State: "open"})
	if err != nil {
		t.Fatalf("CountItems() error = %v", err)
	}
	if counts.Issues != 42 || counts.PRs != 7 || counts.Releases != 3 {
		t.Errorf("counts = %+v, want 42/7/3", counts)
	}
	if counts.Discussions != -1 {
		t.Errorf("Discussions = %d, want -1 (GitLab has none)", counts.Discussions)
	}
	if got := server.first(t, "/issues").query.Get("per_page"); got != "1" {
		t.Errorf("per_page = %q, want 1", got)
	}
	if got := server.first(t, "/issues").query.Get("state"); got != "opened" {
		t.Errorf("issue state query = %q, want opened", got)
	}
	if got := server.first(t, "/merge_requests").query.Get("state"); got != "opened" {
		t.Errorf("merge request state query = %q, want opened", got)
	}
}

func TestCountItems_UnknownTotals(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"/issues", totalRoute("")},
		glRoute{"/merge_requests", totalRoute("many")},
		glRoute{"/releases", statusRoute(http.StatusNotFound, `{"message":"404 Not Found"}`)},
	))
	adapter := newTestAdapter(server)

	counts, err := adapter.CountItems(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("CountItems() error = %v", err)
	}
	if counts.Issues != -1 || counts.PRs != -1 || counts.Releases != -1 {
		t.Errorf("counts = %+v, want -1 for a missing, unparsable and failing total", counts)
	}
}

func TestPlatformMeta(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"%2Fwidgets", jsonRoute(`{"id":4242}`)},
	))
	adapter := newTestAdapter(server)

	meta := adapter.PlatformMeta()
	if meta["platform_project_id"] != "4242" {
		t.Errorf("meta = %v, want the numeric project ID", meta)
	}
	if adapter.PlatformMeta()["platform_project_id"] != "4242" {
		t.Error("second PlatformMeta() call lost the project ID")
	}
	if got := server.count("%2Fwidgets"); got != 1 {
		t.Errorf("project lookups = %d, want 1 (the ID is cached)", got)
	}
}

func TestPlatformMeta_UnreadableProject(t *testing.T) {
	server := newGLServer(t, routed(t,
		glRoute{"%2Fwidgets", statusRoute(http.StatusNotFound, `{"message":"404 Project Not Found"}`)},
	))
	adapter := newTestAdapter(server)

	if meta := adapter.PlatformMeta(); meta != nil {
		t.Errorf("meta = %v, want nil when the project is unreadable", meta)
	}
}

func TestSetUserEmails(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), graphqlUnavailableRoute(), nil))
	adapter := newTestAdapter(server)
	adapter.SetUserEmails(map[string]string{"alice": "alice@override.example.com"})

	plan, err := adapter.FetchPM(importpkg.FetchOptions{SkipBots: true})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if plan.Issues[0].AuthorEmail != "alice@override.example.com" {
		t.Errorf("author email = %q, want the override over the profile", plan.Issues[0].AuthorEmail)
	}
	if plan.Issues[0].AuthorName != "Alice Example" {
		t.Errorf("author name = %q, want the profile name", plan.Issues[0].AuthorName)
	}
}

func TestTokenHeader(t *testing.T) {
	server := newGLServer(t, pmRoutes(t, jsonRoute(pmIssuesJSON), jsonRoute(pmGraphQLJSON), nil))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchPM(importpkg.FetchOptions{}); err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	requests := server.requests()
	if len(requests) == 0 {
		t.Fatal("no requests recorded")
	}
	for _, r := range requests {
		if r.token != "test-token" {
			t.Errorf("%s %s carried token %q, want test-token", r.method, r.path, r.token)
		}
	}
}

func TestTokenHeader_FromEnvironment(t *testing.T) {
	cases := []struct{ name, private, want string }{
		{"GITLAB_TOKEN wins", "", "env-token"},
		{"GITLAB_PRIVATE_TOKEN as fallback", "private-token", "private-token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.private == "" {
				t.Setenv("GITLAB_TOKEN", c.want)
				t.Setenv("GITLAB_PRIVATE_TOKEN", "unused")
			} else {
				t.Setenv("GITLAB_TOKEN", "")
				t.Setenv("GITLAB_PRIVATE_TOKEN", c.private)
			}
			server := newGLServer(t, releaseRoutes(t, jsonRoute(`[]`)))
			adapter := New("acme", "widgets", AdapterOptions{BaseURL: server.server.URL})
			if _, err := adapter.FetchReleases(importpkg.FetchOptions{}); err != nil {
				t.Fatalf("FetchReleases() error = %v", err)
			}
			if got := server.first(t, "/releases").token; got != c.want {
				t.Errorf("token header = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTokenHeader_OmittedWhenUnset(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_PRIVATE_TOKEN", "")
	server := newGLServer(t, releaseRoutes(t, jsonRoute(`[]`)))
	adapter := New("acme", "widgets", AdapterOptions{BaseURL: server.server.URL})

	if _, err := adapter.FetchReleases(importpkg.FetchOptions{}); err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if got := server.first(t, "/releases").token; got != "" {
		t.Errorf("token header = %q, want none", got)
	}
}

func TestSelfHostedBaseURL(t *testing.T) {
	server := newGLServer(t, releaseRoutes(t, jsonRoute(releasesJSON)))
	adapter := New("acme", "widgets", AdapterOptions{BaseURL: server.server.URL + "/", Token: "test-token"})

	plan, err := adapter.FetchReleases(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	request := server.first(t, "/releases")
	if request.path != "/api/v4/projects/acme%2Fwidgets/releases" {
		t.Errorf("path = %q, want the escaped project path under /api/v4", request.path)
	}
	if want := server.server.URL + "/acme/widgets/-/releases/v1.2.0"; plan.Releases[0].ArtifactURL != want {
		t.Errorf("artifact URL = %q, want %q (built from the self-hosted base)", plan.Releases[0].ArtifactURL, want)
	}
	// The noreply domain follows the host, not gitlab.com.
	if want := "dave@users.noreply." + server.host(); plan.Releases[2].AuthorEmail != want {
		t.Errorf("author email = %q, want %q", plan.Releases[2].AuthorEmail, want)
	}
}

func TestRetriesServerErrors(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	attempts := 0
	flaky := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}
	server := newGLServer(t, releaseRoutes(t, flaky))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchReleases(importpkg.FetchOptions{}); err != nil {
		t.Fatalf("FetchReleases() error = %v, want the retry to succeed", err)
	}
	if got := server.count("/releases"); got != 2 {
		t.Errorf("release requests = %d, want 2 (one retry)", got)
	}
}

func TestRetriesRateLimits(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	attempts := 0
	limited := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}
	server := newGLServer(t, releaseRoutes(t, limited))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchReleases(importpkg.FetchOptions{}); err != nil {
		t.Fatalf("FetchReleases() error = %v, want the retry to succeed", err)
	}
	if got := server.count("/releases"); got != 2 {
		t.Errorf("release requests = %d, want 2 (one retry after Retry-After)", got)
	}
}

func TestGivesUpAfterRepeatedServerErrors(t *testing.T) {
	t.Parallel()
	server := newGLServer(t, releaseRoutes(t, statusRoute(http.StatusInternalServerError, "boom")))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchReleases(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchReleases() error = nil, want the exhausted retries")
	}
	if !strings.Contains(err.Error(), "after 3 retries") {
		t.Errorf("error = %v, want the retry count", err)
	}
	if got := server.count("/releases"); got != 4 {
		t.Errorf("release requests = %d, want 4 (the first try and 3 retries)", got)
	}
}
