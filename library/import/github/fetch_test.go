// fetch_test.go - Adapter fetch paths driven by canned gh output
package github

import (
	"strings"
	"testing"
	"time"

	importpkg "github.com/gitsocial-org/gitsocial/library/import"
)

// ghArg reports whether any argument contains the given substring.
func ghArg(args []string, want string) bool {
	for _, a := range args {
		if strings.Contains(a, want) {
			return true
		}
	}
	return false
}

const (
	pmMilestonesJSON = `[{"title":"v1.0","state":"open","description":"First milestone",
		"dueOn":"2024-12-31T00:00:00Z","number":1,"creator":{"login":"alice"},
		"created_at":"2024-06-01T00:00:00Z"}]`

	pmIssuesJSON = `[
		{"number":1,"title":"First issue","body":"Blocked by #2","state":"OPEN",
		 "author":{"login":"alice","name":"Alice"},"labels":[{"name":"bug"}],
		 "assignees":[{"login":"bob"}],"milestone":{"title":"v1.0"},
		 "createdAt":"2024-06-15T12:00:00Z"},
		{"number":2,"title":"Second issue","body":"","state":"CLOSED",
		 "author":{"login":"bob","name":"Bob"},"createdAt":"2024-06-16T12:00:00Z",
		 "closedAt":"2024-07-01T09:30:00Z"},
		{"number":3,"title":"Bot noise","body":"","state":"OPEN",
		 "author":{"login":"dependabot[bot]"},"createdAt":"2024-06-17T12:00:00Z"}
	]`

	pmClosedByJSON = `{"data":{"repository":{"i2":{"timelineItems":{"nodes":[{"actor":{"login":"alice"}}]}}}}}`

	pmCommentsJSON = `{"data":{"repository":{"c1":{"comments":{"nodes":[
		{"databaseId":555,"body":"Me too","author":{"login":"bob","name":"Bob"},
		 "createdAt":"2024-06-18T08:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}}`
)

// ghUserProfile answers a users/<login> lookup for the fixtures above.
func ghUserProfile(args []string) (ghResponse, bool) {
	switch {
	case ghArg(args, "users/alice"):
		return ghResponse{stdout: `{"name":"Alice Example","email":"alice@example.com"}`}, true
	case ghArg(args, "users/bob"):
		return ghResponse{stdout: `{"name":"Bob Example"}`}, true
	case ghArg(args, "users/"):
		return ghResponse{stdout: `{}`}, true
	}
	return ghResponse{}, false
}

func TestFetchPM(t *testing.T) {
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		switch {
		case ghArg(args, "/milestones"):
			return ghResponse{stdout: pmMilestonesJSON}
		case len(args) > 1 && args[0] == "issue" && args[1] == "list":
			return ghResponse{stdout: pmIssuesJSON}
		case ghArg(args, "comments(first:"):
			return ghResponse{stdout: pmCommentsJSON}
		case ghArg(args, "graphql"):
			return ghResponse{stdout: pmClosedByJSON}
		}
		t.Errorf("unrouted gh call: %v", args)
		return ghResponse{stdout: "[]"}
	})

	var progress int
	plan, err := adapter.FetchPM(importpkg.FetchOptions{
		SkipBots:        true,
		OnFetchProgress: func(n int) { progress = n },
	})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if progress != 3 {
		t.Errorf("progress = %d, want 3 (raw issues seen)", progress)
	}

	if len(plan.Milestones) != 1 {
		t.Fatalf("milestones = %d, want 1", len(plan.Milestones))
	}
	ms := plan.Milestones[0]
	if ms.ExternalID != "v1.0" || ms.Number != 1 || ms.Title != "v1.0" || ms.Body != "First milestone" {
		t.Errorf("milestone = %+v", ms)
	}
	if ms.State != "open" || ms.AuthorName != "Alice Example" || ms.AuthorEmail != "alice@example.com" {
		t.Errorf("milestone author/state = %+v", ms)
	}
	if ms.DueDate == nil || !ms.DueDate.Equal(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("milestone DueDate = %v", ms.DueDate)
	}

	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the bot issue)", plan.Filtered)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(plan.Issues))
	}
	first := plan.Issues[0]
	if first.ExternalID != "1" || first.Number != 1 || first.Title != "First issue" {
		t.Errorf("issue 1 = %+v", first)
	}
	if first.State != "open" || first.MilestoneID != "v1.0" {
		t.Errorf("issue 1 state = %q, milestone = %q", first.State, first.MilestoneID)
	}
	if len(first.Labels) != 1 || first.Labels[0] != "bug" {
		t.Errorf("issue 1 labels = %v", first.Labels)
	}
	// Bob's profile carries no email, so the noreply address stands in.
	if len(first.Assignees) != 1 || first.Assignees[0] != "bob@users.noreply.github.com" {
		t.Errorf("issue 1 assignees = %v", first.Assignees)
	}
	if len(first.RelatedIDs) != 1 || first.RelatedIDs[0] != "2" {
		t.Errorf("issue 1 RelatedIDs = %v, want the #2 body reference", first.RelatedIDs)
	}

	second := plan.Issues[1]
	if second.State != "closed" || second.ClosedAt.IsZero() {
		t.Errorf("issue 2 state = %q, closedAt = %v", second.State, second.ClosedAt)
	}
	if second.ClosedByName != "Alice Example" || second.ClosedByEmail != "alice@example.com" {
		t.Errorf("issue 2 closed-by = %q / %q", second.ClosedByName, second.ClosedByEmail)
	}

	if len(plan.Comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(plan.Comments))
	}
	comment := plan.Comments[0]
	if comment.ExternalID != "555" || comment.PostID != "1" || comment.Content != "Me too" {
		t.Errorf("comment = %+v", comment)
	}
	if comment.AuthorName != "Bob Example" {
		t.Errorf("comment author = %q", comment.AuthorName)
	}
}

func TestFetchPM_SkipsMappedExternalIDs(t *testing.T) {
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		switch {
		case ghArg(args, "/milestones"):
			return ghResponse{stdout: pmMilestonesJSON}
		case len(args) > 1 && args[0] == "issue" && args[1] == "list":
			return ghResponse{stdout: pmIssuesJSON}
		}
		return ghResponse{stdout: `{"data":{"repository":{}}}`}
	})

	plan, err := adapter.FetchPM(importpkg.FetchOptions{
		SkipExternalIDs: map[string]bool{"issue:1": true, "milestone:v1.0": true},
	})
	if err != nil {
		t.Fatalf("FetchPM() error = %v", err)
	}
	if len(plan.Milestones) != 0 {
		t.Errorf("milestones = %+v, want none (already mapped)", plan.Milestones)
	}
	for _, issue := range plan.Issues {
		if issue.ExternalID == "1" {
			t.Error("issue 1 was fetched despite being mapped")
		}
	}
	// Mapped items are skipped, not counted as filtered by --since/--skip-bots.
	if plan.Filtered != 0 {
		t.Errorf("Filtered = %d, want 0", plan.Filtered)
	}
}

func TestFetchPM_PropagatesFetchError(t *testing.T) {
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func([]string) ghResponse {
		return ghResponse{stderr: "gh: Not Found (HTTP 404)", exitCode: 1}
	})
	if _, err := adapter.FetchPM(importpkg.FetchOptions{}); err == nil {
		t.Fatal("FetchPM() error = nil, want failure")
	}
}

func TestFetchReleases(t *testing.T) {
	const releasesJSON = `[
		{"tag_name":"v1.2.0","name":"Widgets 1.2.0","body":"Notes","prerelease":false,
		 "draft":false,"created_at":"2024-06-15T12:00:00Z","author":{"login":"alice"},
		 "assets":[{"name":"widgets_linux_amd64.tar.gz"},{"name":"checksums.txt"},
		           {"name":"widgets.spdx.json"}]},
		{"tag_name":"v1.3.0-rc1","name":"","body":"","prerelease":true,"draft":false,
		 "created_at":"2024-06-20T12:00:00Z","author":{"login":"bob"},"assets":[]},
		{"tag_name":"v9.9.9","name":"Draft","body":"","prerelease":false,"draft":true,
		 "created_at":"2024-06-21T12:00:00Z","author":{"login":"alice"},"assets":[]}
	]`
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		if ghArg(args, "/releases") {
			return ghResponse{stdout: releasesJSON}
		}
		t.Errorf("unrouted gh call: %v", args)
		return ghResponse{stdout: "[]"}
	})

	plan, err := adapter.FetchReleases(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if len(plan.Releases) != 2 {
		t.Fatalf("releases = %d, want 2 (the draft is dropped)", len(plan.Releases))
	}
	first := plan.Releases[0]
	if first.ExternalID != "v1.2.0" || first.Tag != "v1.2.0" || first.Version != "1.2.0" {
		t.Errorf("release = %+v", first)
	}
	if first.Name != "Widgets 1.2.0" || first.Body != "Notes" || first.Prerelease {
		t.Errorf("release name/body/prerelease = %+v", first)
	}
	if first.Checksums != "checksums.txt" || first.SBOM != "widgets.spdx.json" {
		t.Errorf("release checksums = %q, sbom = %q", first.Checksums, first.SBOM)
	}
	if len(first.Artifacts) != 3 {
		t.Errorf("artifacts = %v, want all three assets", first.Artifacts)
	}
	if first.ArtifactURL != "https://github.com/acme/widgets/releases/download/v1.2.0" {
		t.Errorf("ArtifactURL = %q", first.ArtifactURL)
	}
	if first.AuthorName != "Alice Example" || first.AuthorEmail != "alice@example.com" {
		t.Errorf("release author = %q / %q", first.AuthorName, first.AuthorEmail)
	}
	// A release with no name falls back to its tag.
	if plan.Releases[1].Name != "v1.3.0-rc1" || !plan.Releases[1].Prerelease {
		t.Errorf("prerelease = %+v", plan.Releases[1])
	}
}

func TestFetchReleases_SinceFilterAndSkip(t *testing.T) {
	const releasesJSON = `[
		{"tag_name":"v1.0.0","name":"Old","created_at":"2023-01-01T00:00:00Z","author":{"login":"alice"}},
		{"tag_name":"v2.0.0","name":"New","created_at":"2024-06-15T12:00:00Z","author":{"login":"alice"}},
		{"tag_name":"v3.0.0","name":"Mapped","created_at":"2024-06-16T12:00:00Z","author":{"login":"alice"}}
	]`
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		return ghResponse{stdout: releasesJSON}
	})

	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	plan, err := adapter.FetchReleases(importpkg.FetchOptions{
		Since:           &since,
		SkipExternalIDs: map[string]bool{"release:v3.0.0": true},
	})
	if err != nil {
		t.Fatalf("FetchReleases() error = %v", err)
	}
	if len(plan.Releases) != 1 || plan.Releases[0].Tag != "v2.0.0" {
		t.Fatalf("releases = %+v, want only v2.0.0", plan.Releases)
	}
	if plan.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1 (the pre-Since release)", plan.Filtered)
	}
}

func TestFetchReview(t *testing.T) {
	const prsJSON = `[
		{"number":7,"title":"Add widget","body":"","state":"OPEN","isDraft":true,
		 "author":{"login":"bob","name":"Bob"},"labels":[{"name":"enhancement"}],
		 "baseRefName":"main","headRefName":"feature/widget","headRefOid":"aaaabbbbccccdddd",
		 "reviewRequests":[{"login":"alice"},{"slug":"platform-team"}],
		 "createdAt":"2024-06-15T12:00:00Z"},
		{"number":8,"title":"Fork fix","body":"","state":"MERGED",
		 "author":{"login":"carol"},"baseRefName":"main","headRefName":"patch-1",
		 "headRepository":{"name":"widgets"},"headRepositoryOwner":{"login":"carol"},
		 "mergeCommit":{"oid":"1111222233334444"},"mergedBy":{"login":"alice"},
		 "mergedAt":"2024-07-01T09:30:00Z","createdAt":"2024-06-16T12:00:00Z"}
	]`
	adapter := New("acme", "widgets")
	fakeGHRoutes(t, func(args []string) ghResponse {
		if p, ok := ghUserProfile(args); ok {
			return p
		}
		if len(args) > 1 && args[0] == "pr" && args[1] == "list" {
			return ghResponse{stdout: prsJSON}
		}
		return ghResponse{stdout: `{"data":{"repository":{}}}`}
	})

	plan, err := adapter.FetchReview(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("FetchReview() error = %v", err)
	}
	if len(plan.PRs) != 2 {
		t.Fatalf("PRs = %d, want 2", len(plan.PRs))
	}
	open := plan.PRs[0]
	if open.ExternalID != "7" || open.State != "open" || !open.IsDraft {
		t.Errorf("PR 7 = %+v", open)
	}
	if open.BaseBranch != "main" || open.HeadBranch != "feature/widget" || open.HeadRepo != "" {
		t.Errorf("PR 7 branches = %+v", open)
	}
	if open.HeadSHA != "aaaabbbbccccdddd" {
		t.Errorf("PR 7 HeadSHA = %q", open.HeadSHA)
	}
	// Team review requests have no email, so only the user reviewer survives.
	if len(open.Reviewers) != 1 || open.Reviewers[0] != "alice@example.com" {
		t.Errorf("PR 7 reviewers = %v", open.Reviewers)
	}

	merged := plan.PRs[1]
	if merged.State != "merged" || merged.MergeCommit != "1111222233334444" {
		t.Errorf("PR 8 = %+v", merged)
	}
	if merged.MergedByName != "Alice Example" || merged.MergedAt.IsZero() {
		t.Errorf("PR 8 merged-by = %q at %v", merged.MergedByName, merged.MergedAt)
	}
	if merged.HeadRepo != "https://github.com/carol/widgets" {
		t.Errorf("PR 8 HeadRepo = %q, want the fork URL", merged.HeadRepo)
	}
	if len(plan.Forks) != 1 || plan.Forks[0] != "https://github.com/carol/widgets" {
		t.Errorf("Forks = %v", plan.Forks)
	}
}

func TestCountItems(t *testing.T) {
	adapter := New("acme", "widgets")
	rec := fakeGHRoutes(t, func(args []string) ghResponse {
		if ghArg(args, "graphql") {
			return ghResponse{stdout: `{"data":{"repository":{"issues":{"totalCount":12},
				"pullRequests":{"totalCount":5},"discussions":{"totalCount":3}}}}`}
		}
		return ghResponse{stdout: `[{"draft":false},{"draft":true},{"draft":false}]`}
	})

	counts, err := adapter.CountItems(importpkg.FetchOptions{})
	if err != nil {
		t.Fatalf("CountItems() error = %v", err)
	}
	if counts.Issues != 12 || counts.PRs != 5 || counts.Discussions != 3 {
		t.Errorf("counts = %+v", counts)
	}
	if counts.Releases != 2 {
		t.Errorf("Releases = %d, want 2 (drafts excluded)", counts.Releases)
	}
	if rec.callCount() != 2 {
		t.Errorf("gh calls = %d, want 2 (one GraphQL, one REST)", rec.callCount())
	}
}

func TestCountItems_MergedStateSkipsIssues(t *testing.T) {
	adapter := New("acme", "widgets")
	var query string
	fakeGHRoutes(t, func(args []string) ghResponse {
		if ghArg(args, "graphql") {
			query = strings.Join(args, " ")
			return ghResponse{stdout: `{"data":{"repository":{"pullRequests":{"totalCount":7},
				"discussions":{"totalCount":0}}}}`}
		}
		return ghResponse{stdout: `[]`}
	})

	counts, err := adapter.CountItems(importpkg.FetchOptions{State: "merged"})
	if err != nil {
		t.Fatalf("CountItems() error = %v", err)
	}
	if !strings.Contains(query, "pullRequests(states: MERGED)") {
		t.Errorf("query = %q, want a MERGED filter", query)
	}
	if strings.Contains(query, "issues") {
		t.Errorf("query = %q, want no issues field for state=merged", query)
	}
	if counts.Issues != -1 {
		t.Errorf("Issues = %d, want -1 (unknown)", counts.Issues)
	}
	if counts.PRs != 7 {
		t.Errorf("PRs = %d, want 7", counts.PRs)
	}
}
