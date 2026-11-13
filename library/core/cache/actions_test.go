// actions_test.go - Tests for the timeline action of a commit
package cache

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"
)

const actionRepo = "https://github.com/user/repo"

// actionCommit builds a commit of the pm branch at the given minute with the given header.
func actionCommit(hash string, minute int, header string) Commit {
	return Commit{
		Hash:      hash,
		RepoURL:   actionRepo,
		Branch:    "gitmsg/pm",
		Message:   "Subject\n\nGitMsg: ext=\"pm\"; " + header + "; v=\"0.1.0\"",
		Timestamp: time.Date(2025, 10, 21, 12, minute, 0, 0, time.UTC),
	}
}

// reviewCommit builds a commit of the review branch at the given minute with the given header.
func reviewCommit(hash string, minute int, header string) Commit {
	c := actionCommit(hash, minute, header)
	c.Branch = "gitmsg/review"
	return c
}

// actionOf reads the action of a hash, "" for none.
func actionOf(t *testing.T, hash string) string {
	t.Helper()
	action, err := QueryLocked(func(db *sql.DB) (sql.NullString, error) {
		var a sql.NullString
		err := db.QueryRow(`SELECT action FROM core_commits WHERE repo_url = ? AND hash = ?`, actionRepo, hash).Scan(&a)
		return a, err
	})
	if err != nil {
		t.Fatalf("read action of %s: %v", hash, err)
	}
	return action.String
}

// issueHistory is an issue that is closed, edited in text, reopened, closed again and assigned.
func issueHistory() []Commit {
	const edits = `edits="#commit:a55000000001@gitmsg/pm"`
	return []Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"`),
		actionCommit("ed0000000001", 1, `type="issue"; `+edits+`; state="closed"`),
		actionCommit("ed0000000002", 2, `type="issue"; `+edits+`; state="closed"; labels="kind/bug"`),
		actionCommit("ed0000000003", 3, `type="issue"; `+edits+`; state="open"`),
		actionCommit("ed0000000004", 4, `type="issue"; `+edits+`; state="closed"`),
		actionCommit("ed0000000005", 5, `type="issue"; `+edits+`; state="closed"; assignees="a@example.com"`),
	}
}

// checkIssueHistory compares the action of each commit of issueHistory with the expected value.
func checkIssueHistory(t *testing.T, order string) {
	t.Helper()
	for hash, want := range map[string]string{
		"a55000000001": "", "ed0000000001": "closed", "ed0000000002": "", "ed0000000003": "reopened", "ed0000000004": "closed", "ed0000000005": "assigned",
	} {
		if got := actionOf(t, hash); got != want {
			t.Errorf("%s: action of %s = %q, want %q", order, hash, got, want)
		}
	}
}

// TestAction_orderOfArrival: the action of each edit is the same when the versions arrive in one batch, one at a time, or in reverse order.
func TestAction_orderOfArrival(t *testing.T) {
	history := issueHistory()
	setupTestDB(t)
	if err := InsertCommits(history); err != nil {
		t.Fatalf("insert batch: %v", err)
	}
	checkIssueHistory(t, "one batch")

	setupTestDB(t)
	for _, c := range history {
		if err := InsertCommits([]Commit{c}); err != nil {
			t.Fatalf("insert %s: %v", c.Hash, err)
		}
	}
	checkIssueHistory(t, "in order")

	setupTestDB(t)
	for i := len(history) - 1; i >= 0; i-- {
		if err := InsertCommits([]Commit{history[i]}); err != nil {
			t.Fatalf("insert %s: %v", history[i].Hash, err)
		}
	}
	if _, err := ReconcileVersions(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	checkIssueHistory(t, "reverse order")
}

// TestAction_textEditHasNone: an edit that changes only text or labels has no action.
func TestAction_textEditHasNone(t *testing.T) {
	setupTestDB(t)
	const edits = `edits="#commit:a55000000001@gitmsg/pm"`
	if err := InsertCommits([]Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"`),
		actionCommit("ed0000000001", 1, `type="issue"; `+edits+`; state="open"; labels="kind/bug"`),
		actionCommit("ed0000000002", 2, `type="issue"; `+edits),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for _, hash := range []string{"a55000000001", "ed0000000001", "ed0000000002"} {
		if got := actionOf(t, hash); got != "" {
			t.Errorf("action of %s = %q, want none", hash, got)
		}
	}
}

// checkActions inserts the commits in one batch and compares the action of each hash with the expected value.
func checkActions(t *testing.T, commits []Commit, want map[string]string) {
	t.Helper()
	setupTestDB(t)
	if err := InsertCommits(commits); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for hash, action := range want {
		if got := actionOf(t, hash); got != action {
			t.Errorf("action of %s = %q, want %q", hash, got, action)
		}
	}
}

// TestAction_assigned: an edit whose assignees gain an email is assigned, compared with no case, and an edit with the same list is not.
func TestAction_assigned(t *testing.T) {
	const edits = `edits="#commit:a55000000001@gitmsg/pm"`
	checkActions(t, []Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"`),
		actionCommit("ed0000000001", 1, `type="issue"; `+edits+`; state="open"; assignees="a@example.com"`),
		actionCommit("ed0000000002", 2, `type="issue"; `+edits+`; state="open"; assignees="b@example.com"`),
		actionCommit("ed0000000003", 3, `type="issue"; `+edits+`; state="open"; assignees="B@Example.com, c@example.com"`),
		actionCommit("ed0000000004", 4, `type="issue"; `+edits+`; state="open"; assignees="c@example.com,b@example.com"; labels="kind/bug"`),
	}, map[string]string{"a55000000001": "", "ed0000000001": "assigned", "ed0000000002": "assigned", "ed0000000003": "assigned", "ed0000000004": ""})
}

// TestAction_reviewRequested: an edit whose reviewers gain an email is review-requested.
func TestAction_reviewRequested(t *testing.T) {
	const edits = `edits="#commit:d00000000001@gitmsg/review"`
	checkActions(t, []Commit{
		reviewCommit("d00000000001", 0, `type="pull-request"; state="open"`),
		reviewCommit("de0000000001", 1, `type="pull-request"; `+edits+`; state="open"; reviewers="b@example.com"`),
		reviewCommit("de0000000002", 2, `type="pull-request"; `+edits+`; state="open"; reviewers="b@example.com,c@example.com"`),
		reviewCommit("de0000000003", 3, `type="pull-request"; `+edits+`; state="open"; reviewers="b@example.com,c@example.com"`),
	}, map[string]string{"d00000000001": "", "de0000000001": "review-requested", "de0000000002": "review-requested", "de0000000003": ""})
}

// TestAction_stateChangeWinsOverAssign: an edit that changes the state and assigns has the state action, and a ready mark wins over a review request.
func TestAction_stateChangeWinsOverAssign(t *testing.T) {
	const edits = `edits="#commit:a55000000001@gitmsg/pm"`
	checkActions(t, []Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"`),
		actionCommit("ed0000000001", 1, `type="issue"; `+edits+`; state="closed"; assignees="a@example.com"`),
		reviewCommit("d00000000001", 2, `type="pull-request"; state="open"; draft="true"`),
		reviewCommit("de0000000001", 3, `type="pull-request"; edits="#commit:d00000000001@gitmsg/review"; state="open"; reviewers="b@example.com"`),
	}, map[string]string{"ed0000000001": "closed", "de0000000001": "ready"})
}

// TestAction_unassignAndFirstVersionHaveNone: a first version with assignees, a removal and an edit with no assignees have no action, and the next edit with the field is assigned again.
func TestAction_unassignAndFirstVersionHaveNone(t *testing.T) {
	const edits = `edits="#commit:a55000000001@gitmsg/pm"`
	checkActions(t, []Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"; assignees="a@example.com,b@example.com"`),
		actionCommit("ed0000000001", 1, `type="issue"; `+edits+`; state="open"; assignees="a@example.com"`),
		actionCommit("ed0000000002", 2, `type="issue"; `+edits+`; state="open"`),
		actionCommit("ed0000000003", 3, `type="issue"; `+edits+`; state="open"; assignees="a@example.com"`),
	}, map[string]string{"a55000000001": "", "ed0000000001": "", "ed0000000002": "", "ed0000000003": "assigned"})
}

// TestAction_proposalAndRetractionHaveNone: an edit from another repository and a retraction have no action, and neither is the version before the next edit.
func TestAction_proposalAndRetractionHaveNone(t *testing.T) {
	setupTestDB(t)
	const edits = `edits="` + actionRepo + `#commit:a55000000001@gitmsg/pm"`
	proposal := actionCommit("b00000000001", 1, `type="issue"; `+edits+`; state="closed"`)
	proposal.RepoURL = "https://github.com/other/fork"
	if err := InsertCommits([]Commit{
		actionCommit("a55000000001", 0, `type="issue"; state="open"`),
		proposal,
		actionCommit("c00000000001", 2, `type="issue"; `+edits+`; state="closed"; retracted="true"`),
		actionCommit("ed0000000001", 3, `type="issue"; `+edits+`; state="closed"`),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got := actionOf(t, "c00000000001"); got != "" {
		t.Errorf("action of the retraction = %q, want none", got)
	}
	if got := actionOf(t, "ed0000000001"); got != "closed" {
		t.Errorf("action of the edit after them = %q, want closed against the first version", got)
	}
	got, err := QueryLocked(func(db *sql.DB) (sql.NullString, error) {
		var a sql.NullString
		err := db.QueryRow(`SELECT action FROM core_commits WHERE hash = 'b00000000001'`).Scan(&a)
		return a, err
	})
	if err != nil || got.Valid {
		t.Errorf("action of the proposal = %q (err %v), want none", got.String, err)
	}
}

// TestAction_pullRequestAndReview: a merge, a draft that becomes ready and a review each have their action, and a review comment has none.
func TestAction_pullRequestAndReview(t *testing.T) {
	setupTestDB(t)
	const edits = `edits="#commit:d00000000001@gitmsg/review"`
	if err := InsertCommits([]Commit{
		reviewCommit("d00000000001", 0, `type="pull-request"; state="open"; draft="true"`),
		reviewCommit("de0000000001", 1, `type="pull-request"; `+edits+`; state="open"`),
		reviewCommit("feedbac00001", 2, `type="feedback"; review-state="approved"`),
		reviewCommit("feedbac00002", 3, `type="feedback"; file="a.go"; new-line="3"`),
		reviewCommit("feedbac00003", 4, `type="feedback"; edits="#commit:feedbac00001@gitmsg/review"; review-state="approved"`),
		reviewCommit("feedbac00004", 5, `type="feedback"; edits="#commit:feedbac00001@gitmsg/review"; review-state="changes-requested"`),
		reviewCommit("de0000000002", 6, `type="pull-request"; `+edits+`; state="merged"`),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for hash, want := range map[string]string{
		"d00000000001": "", "de0000000001": "ready", "de0000000002": "merged",
		"feedbac00001": "approved", "feedbac00002": "", "feedbac00003": "", "feedbac00004": "changes-requested",
	} {
		if got := actionOf(t, hash); got != want {
			t.Errorf("action of %s = %q, want %q", hash, got, want)
		}
	}
}

// TestParityActions: headerAction gives the action that the site app gives for each shared fixture.
func TestParityActions(t *testing.T) {
	raw, err := os.ReadFile("../site/sitetest/action_fixtures.json")
	if err != nil {
		t.Fatalf("read the fixtures: %v", err)
	}
	var fixtures struct {
		Cases []struct {
			Name   string            `json:"name"`
			Prev   map[string]string `json:"prev"`
			Cur    map[string]string `json:"cur"`
			Expect string            `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("parse the fixtures: %v", err)
	}
	if len(fixtures.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, c := range fixtures.Cases {
		if got := headerAction(c.Prev, c.Cur); got != c.Expect {
			t.Errorf("%s: headerAction() = %q, want %q", c.Name, got, c.Expect)
		}
	}
}
