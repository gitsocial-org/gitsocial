// timeline_actions_test.go - Tests for the action entries of the timeline
package social

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

const actionsWorkspace = "https://github.com/ws/actions"

// actionsCommit builds a commit of the workspace by the given author, on the given branch and second.
func actionsCommit(hash, branch, email string, second int, subject, header string) cache.Commit {
	return cache.Commit{
		Hash: hash, RepoURL: actionsWorkspace, Branch: branch, AuthorName: email, AuthorEmail: email,
		Message:   subject + "\n\nGitMsg: " + header + "; v=\"0.1.0\"",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC).Add(time.Duration(second) * time.Second),
	}
}

// timelineActions returns the workspace timeline as "hash action" pairs, newest first.
func timelineActions(t *testing.T) []string {
	t.Helper()
	items, err := getTimeline(nil, actionsWorkspace, actionsWorkspace, nil, 50, "")
	if err != nil {
		t.Fatalf("getTimeline() error = %v", err)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Hash+" "+item.Action)
	}
	if count, err := getTimelineCount(nil, actionsWorkspace, nil); err != nil || count != len(items) {
		t.Errorf("getTimelineCount() = %d, %v, want %d", count, err, len(items))
	}
	return out
}

// sameEntries compares two timelines in order.
func sameEntries(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTimeline_actionEntries: the timeline has the item at its creation time and each action at its own time, and a text edit adds no entry.
func TestTimeline_actionEntries(t *testing.T) {
	setupTestDB(t)
	const issueEdits = `ext="pm"; type="issue"; edits="#commit:a55000000001@gitmsg/pm"`
	if err := cache.InsertCommits([]cache.Commit{
		actionsCommit("a55000000001", "gitmsg/pm", "a@example.com", 0, "Fix the build", `ext="pm"; type="issue"; state="open"`),
		actionsCommit("ed0000000001", "gitmsg/pm", "a@example.com", 10, "Fix the build now", issueEdits+`; state="open"`),
		actionsCommit("ed0000000002", "gitmsg/pm", "b@example.com", 20, "Fix the build now", issueEdits+`; state="closed"`),
		actionsCommit("feedbac00001", "gitmsg/review", "b@example.com", 30, "Looks right", `ext="review"; type="feedback"; review-state="approved"`),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	want := []string{"feedbac00001 approved", "ed0000000002 closed", "a55000000001 "}
	if got := timelineActions(t); !sameEntries(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	items, _ := getTimeline(nil, actionsWorkspace, actionsWorkspace, nil, 50, "")
	closed := timelinePost(items[1])
	if closed.ActionSubject != "Fix the build now" || closed.Author.Email != "b@example.com" || closed.HeaderType != "issue" {
		t.Errorf("closed entry = subject %q by %q of type %q, want the subject of its version, its own author and the issue type",
			closed.ActionSubject, closed.Author.Email, closed.HeaderType)
	}
	if want := time.Date(2025, 10, 21, 12, 0, 20, 0, time.UTC); !closed.Timestamp.Equal(want) {
		t.Errorf("closed entry time = %v, want the time of the action %v", closed.Timestamp, want)
	}
}

// TestTimeline_excludesStaleAction: a stale row of an action is in no timeline.
func TestTimeline_excludesStaleAction(t *testing.T) {
	setupTestDB(t)
	if err := cache.InsertCommits([]cache.Commit{
		actionsCommit("a55000000001", "gitmsg/pm", "a@example.com", 0, "Fix the build", `ext="pm"; type="issue"; state="open"`),
		actionsCommit("ed0000000001", "gitmsg/pm", "a@example.com", 10, "Fix the build", `ext="pm"; type="issue"; edits="#commit:a55000000001@gitmsg/pm"; state="closed"`),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE hash = 'ed0000000001'`)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	if got, want := timelineActions(t), []string{"a55000000001 "}; !sameEntries(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}
}

// TestTimeline_excludesActionOfRetractedItem: the actions of an item leave the timeline with the item when it is retracted.
func TestTimeline_excludesActionOfRetractedItem(t *testing.T) {
	setupTestDB(t)
	const edits = `ext="pm"; type="issue"; edits="#commit:a55000000001@gitmsg/pm"`
	if err := cache.InsertCommits([]cache.Commit{
		actionsCommit("a55000000001", "gitmsg/pm", "a@example.com", 0, "Fix the build", `ext="pm"; type="issue"; state="open"`),
		actionsCommit("ed0000000001", "gitmsg/pm", "a@example.com", 10, "Fix the build", edits+`; state="closed"`),
		actionsCommit("ed0000000002", "gitmsg/pm", "a@example.com", 20, "Fix the build", edits+`; state="closed"; retracted="true"`),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got := timelineActions(t); len(got) != 0 {
		t.Errorf("timeline = %v, want no entry for a retracted item", got)
	}
}

// TestTimeline_mergeHidesIssueClose: a merge that closes an issue gives one entry that names the issue, and a close by another author, of another issue or more than 60 seconds later gives its own entry.
func TestTimeline_mergeHidesIssueClose(t *testing.T) {
	setupTestDB(t)
	issue := func(hash string, second int) cache.Commit {
		return actionsCommit(hash, "gitmsg/pm", "a@example.com", second, "Issue "+hash, `ext="pm"; type="issue"; state="open"`)
	}
	closeOf := func(hash, target, email string, second int) cache.Commit {
		return actionsCommit(hash, "gitmsg/pm", email, second, "Issue "+target, `ext="pm"; type="issue"; edits="#commit:`+target+`@gitmsg/pm"; state="closed"`)
	}
	const prHeader = `ext="review"; type="pull-request"; closes="#commit:a55000000001@gitmsg/pm,#commit:a55000000002@gitmsg/pm"`
	if err := cache.InsertCommits([]cache.Commit{
		issue("a55000000001", 0), issue("a55000000002", 1), issue("a55000000003", 2),
		actionsCommit("d00000000001", "gitmsg/review", "a@example.com", 3, "Add the fix", prHeader+`; state="open"`),
		actionsCommit("de0000000001", "gitmsg/review", "a@example.com", 100, "Add the fix", prHeader+`; edits="#commit:d00000000001@gitmsg/review"; state="merged"`),
		closeOf("ed0000000001", "a55000000001", "a@example.com", 101),
		closeOf("ed0000000002", "a55000000002", "b@example.com", 102),
		closeOf("ed0000000003", "a55000000003", "a@example.com", 103),
		actionsCommit("ed0000000004", "gitmsg/pm", "a@example.com", 104, "Issue a55000000001", `ext="pm"; type="issue"; edits="#commit:a55000000001@gitmsg/pm"; state="open"`),
		closeOf("ed0000000005", "a55000000001", "a@example.com", 161),
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	want := []string{
		"ed0000000005 closed", "ed0000000004 reopened", "ed0000000003 closed", "ed0000000002 closed", "de0000000001 merged",
		"d00000000001 ", "a55000000003 ", "a55000000002 ", "a55000000001 ",
	}
	if got := timelineActions(t); !sameEntries(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	items, _ := getTimeline(nil, actionsWorkspace, actionsWorkspace, nil, 50, "")
	if merged := timelinePost(items[4]); len(merged.Closes) != 2 || merged.Closes[0] != "#commit:a55000000001@gitmsg/pm" {
		t.Errorf("merged entry closes = %v, want the two issues of the pull request", merged.Closes)
	}
}
