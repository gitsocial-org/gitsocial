// comments_test.go - Tests for GraphQL comment query builders
package github

import (
	"strings"
	"testing"
	"time"
)

func TestBuildItemCommentsQuery(t *testing.T) {
	query := buildItemCommentsQuery("octocat", "hello", "issue", []int{1, 42})
	for _, want := range []string{
		`repository(owner: "octocat", name: "hello")`,
		"c1: issue(number: 1)",
		"c42: issue(number: 42)",
		"comments(first: 50)",
		"databaseId",
		"pageInfo { hasNextPage endCursor }",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
}

func TestBuildItemCommentsQuery_PullRequestField(t *testing.T) {
	query := buildItemCommentsQuery("octocat", "hello", "pullRequest", []int{7})
	if !strings.Contains(query, "c7: pullRequest(number: 7)") {
		t.Errorf("query missing pullRequest alias:\n%s", query)
	}
}

func TestBuildMoreItemCommentsQuery(t *testing.T) {
	query := buildMoreItemCommentsQuery("octocat", "hello", "issue", 5, "CURSOR123")
	for _, want := range []string{
		`item: issue(number: 5)`,
		`comments(first: 100, after: "CURSOR123")`,
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
}

func TestItemCommentExternalID(t *testing.T) {
	ts := time.Date(2024, 3, 5, 9, 30, 0, 0, time.UTC)
	withID := ghItemComment{DatabaseID: 123456789, CreatedAt: ts}
	if got := itemCommentExternalID(42, withID); got != "123456789" {
		t.Errorf("with databaseId = %q, want 123456789", got)
	}
	withoutID := ghItemComment{CreatedAt: ts}
	if got := itemCommentExternalID(42, withoutID); got != "42-20240305T093000" {
		t.Errorf("fallback = %q, want 42-20240305T093000", got)
	}
}
