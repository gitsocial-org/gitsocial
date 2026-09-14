// releases_test.go - FetchReleases and FetchSocial against the fake GitLab endpoints
package gitlab

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// releasesJSON holds a release with assets, one with no name, and one with alternate asset names.
const releasesJSON = `[
	{"tag_name":"v1.2.0","name":"Widgets 1.2.0","description":"Notes",
	 "released_at":"2024-06-15T12:00:00Z","author":{"username":"alice"},
	 "assets":{"links":[
		{"name":"widgets_linux_amd64.tar.gz","url":"https://example.com/a"},
		{"name":"checksums.txt","url":"https://example.com/b"},
		{"name":"widgets.spdx.json","url":"https://example.com/c"}]}},
	{"tag_name":"v1.1.0","name":"","description":"",
	 "released_at":"2024-05-01T12:00:00Z","author":{"username":"bob"},
	 "assets":{"links":[]}},
	{"tag_name":"v1.0.0","name":"Widgets 1.0.0","description":"",
	 "released_at":"2024-04-01T12:00:00Z","author":{"username":"dave"},
	 "assets":{"links":[
		{"name":"widgets.sha256","url":"https://example.com/d"},
		{"name":"bom.cdx.xml","url":"https://example.com/e"}]}}
]`

// releaseRoutes serves the release and user endpoints FetchReleases walks.
func releaseRoutes(t *testing.T, releases http.HandlerFunc) http.HandlerFunc {
	return routed(t,
		glRoute{"/releases", releases},
		glRoute{"/users", usersRoute()},
	)
}

// releasePageJSON builds a one-release page for the pagination tests.
func releasePageJSON(tag, releasedAt string) string {
	return fmt.Sprintf(`{"tag_name":%q,"name":%q,"description":"","released_at":%q,
		"author":{"username":"alice"},"assets":{"links":[]}}`, tag, tag, releasedAt)
}

func TestFetchReleases(t *testing.T) {
	server := newGLServer(t, releaseRoutes(t, jsonRoute(releasesJSON)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReleases(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if len(plan.Releases) != 3 {
		t.Fatalf("releases = %d, want 3", len(plan.Releases))
	}

	latest := plan.Releases[0]
	if latest.ExternalID != "v1.2.0" || latest.Tag != "v1.2.0" || latest.Version != "1.2.0" {
		t.Errorf("release 1 ids = %+v", latest)
	}
	if latest.Name != "Widgets 1.2.0" || latest.Body != "Notes" {
		t.Errorf("release 1 name = %q, body = %q", latest.Name, latest.Body)
	}
	if !latest.CreatedAt.Equal(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("release 1 created = %v", latest.CreatedAt)
	}
	if latest.AuthorName != "Alice Example" || latest.AuthorEmail != "alice@example.com" {
		t.Errorf("release 1 author = %q / %q", latest.AuthorName, latest.AuthorEmail)
	}
	if len(latest.Artifacts) != 3 || latest.Artifacts[0] != "widgets_linux_amd64.tar.gz" {
		t.Errorf("release 1 artifacts = %v, want every asset link name", latest.Artifacts)
	}
	if latest.Checksums != "checksums.txt" || latest.SBOM != "widgets.spdx.json" {
		t.Errorf("release 1 checksums = %q, sbom = %q", latest.Checksums, latest.SBOM)
	}
	if want := server.server.URL + "/acme/widgets/-/releases/v1.2.0"; latest.ArtifactURL != want {
		t.Errorf("release 1 artifact URL = %q, want %q", latest.ArtifactURL, want)
	}

	// A release with no name falls back to its tag.
	if plan.Releases[1].Name != "v1.1.0" {
		t.Errorf("release 2 name = %q, want the tag", plan.Releases[1].Name)
	}
	if len(plan.Releases[1].Artifacts) != 0 {
		t.Errorf("release 2 artifacts = %v, want none", plan.Releases[1].Artifacts)
	}
	if plan.Releases[2].Checksums != "widgets.sha256" || plan.Releases[2].SBOM != "bom.cdx.xml" {
		t.Errorf("release 3 checksums = %q, sbom = %q", plan.Releases[2].Checksums, plan.Releases[2].SBOM)
	}
}

func TestFetchReleases_FollowsNextPageHeader(t *testing.T) {
	pages := pagedRoute(
		"["+releasePageJSON("v2.0.0", "2024-06-15T12:00:00Z")+"]",
		"["+releasePageJSON("v1.9.0", "2024-05-15T12:00:00Z")+"]",
	)
	server := newGLServer(t, releaseRoutes(t, pages))
	adapter := newTestAdapter(server)

	var progress int
	plan, err := adapter.FetchReleases(importpkg.FetchOptions{OnFetchProgress: func(n int) { progress = n }})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if len(plan.Releases) != 2 {
		t.Fatalf("releases = %d, want 2 across both pages", len(plan.Releases))
	}
	if progress != 2 {
		t.Errorf("progress = %d, want 2", progress)
	}
	lists := server.all("/releases")
	if len(lists) != 2 || lists[1].query.Get("page") != "2" {
		t.Errorf("release requests = %d, second page query = %q", len(lists), lists[1].query.Get("page"))
	}
	if got := lists[0].query.Get("order_by"); got != "released_at" {
		t.Errorf("order_by = %q, want released_at", got)
	}
}

func TestFetchReleases_LimitStopsPaging(t *testing.T) {
	pages := pagedRoute(
		"["+releasePageJSON("v3.0.0", "2024-06-15T12:00:00Z")+"]",
		"["+releasePageJSON("v2.0.0", "2024-05-15T12:00:00Z")+"]",
		"["+releasePageJSON("v1.0.0", "2024-04-15T12:00:00Z")+"]",
	)
	server := newGLServer(t, releaseRoutes(t, pages))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReleases(importpkg.FetchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if len(plan.Releases) != 2 {
		t.Fatalf("releases = %d, want the 2 the limit allows", len(plan.Releases))
	}
	if got := server.count("/releases"); got != 2 {
		t.Errorf("release requests = %d, want 2 (paging stops at the limit)", got)
	}
}

func TestFetchReleases_FiltersBySinceAndMapping(t *testing.T) {
	since := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	server := newGLServer(t, releaseRoutes(t, jsonRoute(releasesJSON)))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchReleases(importpkg.FetchOptions{
		Since:           &since,
		SkipExternalIDs: map[string]bool{"release:v1.2.0": true},
	})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the release before --since)", plan.Filtered)
	}
	if len(plan.Releases) != 1 || plan.Releases[0].Tag != "v1.1.0" {
		t.Errorf("releases = %+v, want only v1.1.0", plan.Releases)
	}
}

func TestFetchReleases_PropagatesError(t *testing.T) {
	server := newGLServer(t, releaseRoutes(t, statusRoute(http.StatusNotFound, `{"message":"404 Project Not Found"}`)))
	adapter := newTestAdapter(server)

	_, err := adapter.FetchReleases(importpkg.FetchOptions{})
	if err == nil {
		t.Fatal("FetchReleases() error = nil, want the not-found failure")
	}
	if !strings.Contains(err.Error(), "fetch releases") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the fetch context and the status", err)
	}
}

func TestFetchReleases_RejectsMalformedBody(t *testing.T) {
	server := newGLServer(t, releaseRoutes(t, jsonRoute(`[{"tag_name":]`)))
	adapter := newTestAdapter(server)

	if _, err := adapter.FetchReleases(importpkg.FetchOptions{}); err == nil {
		t.Fatal("FetchReleases() error = nil, want a decode failure")
	}
}

func TestFetchSocial(t *testing.T) {
	server := newGLServer(t, routed(t))
	adapter := newTestAdapter(server)

	plan, err := adapter.FetchSocial(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchSocial() error = %v", err)
	}
	if len(plan.Posts) != 0 || len(plan.Comments) != 0 || plan.Filtered != 0 {
		t.Errorf("plan = %+v, want an empty plan (GitLab has no discussions)", plan)
	}
	if len(server.requests()) != 0 {
		t.Errorf("requests = %v, want none", server.requests())
	}
}
