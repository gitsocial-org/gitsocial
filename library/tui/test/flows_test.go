// flows_test.go - Multi-step flows that drive the keys of one view
package test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// TestFlows drives multi-step key sequences through one view at a time.
func TestFlows(t *testing.T) {
	fullTierOnly(t)
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	t.Run("PRDetailOpenAndMerged", func(t *testing.T) {
		h.Navigate("/review/prs")
		h.SendKey("enter")
		out := renderedAfterLoad(h, []string{f.PRSubject, "LGTM!"})
		for _, want := range []string{f.PRSubject, "Reviews (1)", "LGTM!", "1/1"} {
			assertContains(t, out, want)
		}
		h.SendKeys("right", "left", "[", "]")
		if got := h.CurrentPath(); got != "/review/pr" {
			t.Fatalf("source and stack keys at the ends: path = %q, want /review/pr", got)
		}
		h.SendKeys("j", "G", "pgup", "pgdown", "k", "g")
		h.SetSize(80, 12)
		h.SendKeys("j", "j", "j", "k", "k", "k", "end", "home")
		h.SetSize(120, 40)
		h.SendKey("v")
		assertContains(t, h.Rendered(), "Author: Alice <alice@example.com>")
		h.SendKey("v")
		h.SendKeys("/", "L", "G", "T", "M", "enter")
		assertContains(t, h.Rendered(), "1/1  n:next")
		h.SendKeys("n", "N", "esc")
		h.SendKey("M")
		assertContains(t, h.Rendered(), "Merge strategy?")
		h.SendKey("esc")
		h.SendKey("C")
		assertContains(t, h.Rendered(), "Close this pull request? [y/n]")
		h.SendKey("n")
		h.SendKey("a")
		assertContains(t, h.Rendered(), "Action?")
		h.SendKey("x")
		assertContains(t, h.Rendered(), "Retract this pull request? [y/n]")
		h.SendKey("n")
		h.SendKeys("a", "s")
		assertContains(t, h.Rendered(), "1 commits behind main. Sync?")
		h.SendKey("esc")
		h.SendKeys("G", "s")
		if got := h.CurrentPath(); got != "/review/pr" {
			t.Fatalf("s on a review with no suggestion: path = %q, want /review/pr", got)
		}
		h.SendKey("r")
		if got := h.CurrentPath(); got != "/review/feedback" {
			t.Errorf("r on the pull request: path = %q, want /review/feedback", got)
		}
		h.SendKey("esc")
		h.SendKeys("G", "enter")
		if got := h.CurrentPath(); got != "/social/detail" {
			t.Errorf("enter on the review: path = %q, want /social/detail", got)
		}
		res := review.GetPullRequests(gitmsg.ResolveRepoURL(f.Workdir), gitmsg.GetExtBranch(f.Workdir, "review"), []string{string(review.PRStateMerged)}, "", 10)
		if !res.Success || len(res.Data) == 0 {
			t.Fatalf("no merged pull request in the fixture")
		}
		loc := tuicore.LocReviewPRDetail(res.Data[0].ID)
		assertRendersItem(t, h, loc, "Add theme toggle component", "merged", "Merged by")
		out = rendered(h)
		for _, hidden := range []string{"M:merge", "C:close", "r:review"} {
			if strings.Contains(out, hidden) {
				t.Errorf("a merged pull request shows %q in its footer", hidden)
			}
		}
		h.SendKey("a")
		assertContains(t, h.Rendered(), "Action?  [x]retract  esc")
		h.SendKey("esc")
	})
	t.Run("PRDetailCommitsAndSuggestion", func(t *testing.T) {
		iso := SetupFixture(t)
		head := seedPRChange(t, iso.Workdir, iso.PRID)
		fb := review.CreateFeedback(iso.Workdir, "Shorter line.\n\n```suggestion\nline five changed\n```", review.CreateFeedbackOptions{
			PullRequest: iso.PRID, Commit: head, File: "long.txt", NewLine: 5, Suggestion: true,
		})
		if !fb.Success {
			t.Fatalf("CreateFeedback: %s", fb.Error.Message)
		}
		reply := social.CreateComment(iso.Workdir, fb.Data.ID, "Agreed on the suggestion", nil)
		if !reply.Success {
			t.Fatalf("CreateComment on the feedback: %s", reply.Error.Message)
		}
		comment := social.CreateComment(iso.Workdir, iso.PRID, "Ship it after the fix", nil)
		if !comment.Success {
			t.Fatalf("CreateComment on the pull request: %s", comment.Error.Message)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		focus := func(id string) tuicore.Location {
			loc := tuicore.LocReviewPRDetail(iso.PRID)
			loc.Params["focusID"] = id
			return loc
		}
		assertRendersItem(t, ih, focus(fb.Data.ID), "Commits (1)", "Change long file", "Suggestion:", "+ line five changed", ">    5 line five")
		ih.SendKey("s")
		data, err := os.ReadFile(filepath.Join(iso.Workdir, "long.txt"))
		if err != nil {
			t.Fatalf("read long.txt: %v", err)
		}
		if !strings.Contains(string(data), "line five changed") {
			t.Errorf("s on the suggestion did not apply it:\n%s", data)
		}
		for _, target := range []struct{ id, text string }{{comment.Data.ID, "Ship it after the fix"}, {reply.Data.ID, "Agreed on the suggestion"}} {
			assertRendersItem(t, ih, focus(target.id), target.text)
			ih.SendKey("enter")
			if got := ih.CurrentPath(); got != "/social/detail" {
				t.Fatalf("enter on the focused comment: path = %q, want /social/detail", got)
			}
			assertContains(t, ih.Rendered(), target.text)
		}
		ih.NavigateTo(tuicore.LocReviewPRDetail(iso.PRID))
		ih.SendKeys("g", "j", "enter")
		if got := ih.CurrentPath(); got != "/review/diff" {
			t.Fatalf("enter on the commit: path = %q, want /review/diff", got)
		}
		assertContains(t, ih.Rendered(), "long.txt")
	})
	// Two edits of one pull request in the same second tie on timestamp, so each edit of this flow waits for the next second.
	t.Run("PRDetailStackActions", func(t *testing.T) {
		iso := SetupFixture(t)
		childID := setupStack(t, iso)
		pushToLocalBare(t, iso.Workdir)
		gitOut(t, iso.Workdir, "commit", "--allow-empty", "-m", "Advance main")
		ih := New(t, iso.Workdir, iso.CacheDir)
		loc := tuicore.LocReviewPRDetail(iso.PRID)
		assertRendersItem(t, ih, loc, "Stack (2)", "Stacked: add dark mode toggle UI", "2 commits behind main")
		ih.SendKey("]")
		assertRendersItem(t, ih, tuicore.LocReviewPRDetail(childID), "Depends on")
		ih.SendKey("[")
		assertRendersItem(t, ih, loc, f.PRSubject, "Stack (2)")
		ih.SendKeys("a", "t")
		assertContains(t, ih.Rendered(), "Stack operation?")
		ih.SendKey("s")
		assertContains(t, ih.Rendered(), "Synced tips for")
		ih.SendKeys("a", "t", "r")
		assertContains(t, ih.Rendered(), "no commits to rebase")
		ih.SendKeys("a", "u")
		mainTip := strings.TrimSpace(gitOut(t, iso.Workdir, "rev-parse", "--short=12", "main"))
		assertRendersItem(t, ih, loc, "main · #"+mainTip)
		waitNextSecond()
		ih.SendKeys("a", "d")
		assertRendersItem(t, ih, loc, "draft")
		ih.SendKey("j")
		if strings.Contains(rendered(ih), "M:merge") {
			t.Error("the footer of a draft offers M:merge")
		}
		ih.SendKey("a")
		assertContains(t, ih.Rendered(), "[d]mark ready")
		ih.SendKey("d")
		ih.SendKeys("a", "s")
		assertContains(t, ih.Rendered(), "2 commits behind main. Sync?")
		ih.SendKey("r")
		assertContains(t, ih.Rendered(), "no commits to rebase")
	})
	t.Run("PRDetailMergeCloseRetract", func(t *testing.T) {
		iso := SetupFixture(t)
		childID := setupStack(t, iso)
		pushToLocalBare(t, iso.Workdir)
		ih := New(t, iso.Workdir, iso.CacheDir)
		loc := tuicore.LocReviewPRDetail(iso.PRID)
		assertRendersItem(t, ih, loc, f.PRSubject)
		ih.SendKeys("M", "m")
		assertRendersItem(t, ih, loc, "Merged by", "[merged]")
		childLoc := tuicore.LocReviewPRDetail(childID)
		assertRendersItem(t, ih, childLoc, "Stacked: add dark mode toggle UI")
		waitNextSecond()
		ih.SendKeys("C", "y")
		assertRendersItem(t, ih, childLoc, "Closed by", "[closed]")
		waitNextSecond()
		ih.SendKeys("a", "x")
		assertContains(t, ih.Rendered(), "Retract this pull request? [y/n]")
		ih.SendKey("y")
		assertContains(t, ih.Rendered(), "Pull request retracted")
	})
	t.Run("ReviewDiffKeys", func(t *testing.T) {
		iso := SetupFixture(t)
		seedPRChange(t, iso.Workdir, iso.PRID)
		ih := New(t, iso.Workdir, iso.CacheDir)
		loc := tuicore.LocReviewDiff(iso.PRID)
		assertRendersItem(t, ih, loc, "alpha.txt", "long.txt")
		ih.SendKey("enter")
		assertContains(t, ih.Rendered(), "alpha body")
		ih.SendKeys("enter", "E")
		assertContains(t, ih.Rendered(), "line fifty")
		ih.SendKeys("]", "]", "]")
		if got := cursorLine(rendered(ih)); !strings.Contains(got, "line u") {
			t.Errorf("after three ] the cursor is on %q, want the first row of the second long.txt hunk", got)
		}
		ih.SendKeys("[", "[", "tab", "shift+tab", "pgdown", "pgup", "G", "g", "j", "k", "e", "e")
		ih.SendKey("v")
		assertContains(t, ih.Rendered(), "line e xxxx")
		ih.SendKeys("right", "left", "w")
		ih.SendKey("v")
		if strings.Contains(rendered(ih), "GitSocial") {
			t.Error("the fullscreen mode still shows the navigation panel")
		}
		ih.SendKey("esc")
		assertContains(t, ih.Rendered(), "GitSocial")
		ih.SendKeys("v", "v", "w")
		ih.SendKeys("/", "#", "backspace", "f", "i", "f", "t", "y", "enter")
		assertContains(t, ih.Rendered(), "1/1  n:next")
		ih.SendKeys("n", "N", "esc")
		if got := ih.CurrentPath(); got != "/review/diff" {
			t.Fatalf("esc on the search results: path = %q, want /review/diff", got)
		}
		if strings.Contains(rendered(ih), "n:next") {
			t.Error("esc leaves the search footer")
		}
		ih.SetSize(50, 20)
		assertContains(t, ih.Rendered(), "Files: 2")
		ih.SetSize(200, 50)
		assertContains(t, ih.Rendered(), "line fifty")
		ih.SendKeys("g", "]", "c")
		if got := ih.CurrentPath(); got != "/review/feedback" {
			t.Errorf("c on a diff line: path = %q, want /review/feedback", got)
		}
	})
	t.Run("PostThreadKeys", func(t *testing.T) {
		assertRendersItem(t, h, tuicore.LocDetail(f.PostID), f.EditedContent, f.QuoteContent, f.CommentContent)
		h.SendKeys("j", "j", "end", "k", "home", "ctrl+d", "ctrl+u", "pgdown", "pgup")
		h.SendKeys("/", "G", "r", "e", "a", "t")
		assertContains(t, h.Rendered(), "1/1")
		h.SendKeys("enter", "n", "N", "x", "/", "esc")
		h.SendKeys("/", "enter", "/", "z", "z", "enter", "n", "N", "/", "backspace", "backspace", "esc")
		h.SendKeys("/", "G", "enter", "esc")
		if strings.Contains(rendered(h), "1/1") {
			t.Error("the search footer stays after esc")
		}
		h.SendKey("v")
		assertContains(t, h.Rendered(), "Author: Alice <alice@example.com>")
		h.SendKeys("v", ";", ";", ",", ",", ",", "esc")
		h.SendKeys(";", "enter")
		if got := h.CurrentPath(); got != "/search" {
			t.Errorf("enter on the first link of the post: path = %q, want /search", got)
		}
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKeys("end", "enter")
		assertContains(t, rendered(h), "↩  ⇡  Alice")
		assertContains(t, rendered(h), f.CommentContent)
		h.SendKeys("k", "enter")
		assertContains(t, rendered(h), "•  ⇡  Alice")
		h.SendKey("d")
		if got := h.CurrentPath(); got != "/diff" {
			t.Errorf("d on a workspace post: path = %q, want /diff", got)
		}
		h.NavigateTo(tuicore.LocDetail(f.PostID))
		h.SendKey("r")
		if got := h.CurrentPath(); got != "/social/repository" {
			t.Errorf("r on a workspace post: path = %q, want /social/repository", got)
		}
	})
	t.Run("PostTitlesAndSources", func(t *testing.T) {
		for _, item := range []struct{ id, title string }{
			{f.IssueID, "○  ⇡  Alice"}, {f.MilestoneID, "◇  ⇡  Alice"}, {f.SprintID, "◷  ⇡  Alice"},
			{f.PRID, "⑂  ⇡  Alice"}, {f.ReleaseID, "⏏  ⇡  Alice"}, {f.MemoID, "☞  ⇡  Alice"},
		} {
			assertRendersItem(t, h, tuicore.LocDetail(item.id), item.title)
		}
		res := social.GetPosts(f.Workdir, "timeline", nil)
		if !res.Success {
			t.Fatalf("GetPosts timeline: %s", res.Error.Message)
		}
		ids := map[social.PostType]string{}
		for _, p := range res.Data {
			ids[p.Type] = p.ID
		}
		if ids[social.PostTypeRepost] == "" || ids[social.PostTypeQuote] == "" {
			t.Fatal("no repost or quote in the timeline")
		}
		assertRendersItem(t, h, tuicore.LocDetail(ids[social.PostTypeRepost]), "↻  ⇡  Alice", f.EditedContent)
		assertRendersItem(t, h, tuicore.LocDetail(ids[social.PostTypeQuote]), "↻  ⇡  Alice", f.QuoteContent)
		h.Navigate("/social/detail")
		assertContains(t, rendered(h), "Error: no post ID in location")
		assertRendersItem(t, h, tuicore.LocDetail("https://github.com/user/repo#commit:000000000000@gitmsg/social"), "Error:")
		h.Navigate("/search")
		h.SendKeys("G", "r", "e", "a", "t", "enter", "enter")
		assertContains(t, rendered(h), "↩  ⇡  Alice")
		h.SendKeys("/", "esc", "esc")
		if got := h.CurrentPath(); got != "/social/detail" {
			t.Errorf("esc on a search highlight: path = %q, want /social/detail", got)
		}
		h.Navigate("/social/timeline")
		h.SendKeys("enter", "right")
		if got := h.CurrentPath(); got != "/social/detail" {
			t.Fatalf("right from the first timeline item: path = %q, want /social/detail", got)
		}
		assertContains(t, rendered(h), "2/")
		h.SendKeys("right", "left", "d")
		assertContains(t, rendered(h), "2/")
		h.SendKey("r")
		if got := h.CurrentPath(); got != "/social/repository" {
			t.Errorf("r on a fork post: path = %q, want /social/repository", got)
		}
	})
	t.Run("PostRetractAccept", func(t *testing.T) {
		iso := SetupFixture(t)
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocDetail(iso.PostID), f.EditedContent)
		ih.SendKey("X")
		assertContains(t, ih.Rendered(), "Retract this post? [y/n]")
		ih.SendKey("y")
		if strings.Contains(rendered(ih), "[y/n]") {
			t.Error("y leaves the retract confirm open")
		}
		assertRendersItem(t, ih, tuicore.LocDetail(iso.PostID), "Error: post not found in thread")
	})
	// A file:// URL under a temporary directory keeps the first fetch of an added repository offline.
	t.Run("ListPickerCreateAndDelete", func(t *testing.T) {
		iso := SetupFixture(t)
		ih := New(t, iso.Workdir, iso.CacheDir)
		ih.Navigate("/lists")
		assertContains(t, rendered(ih), "(memo-inherits)")
		ih.SendKeys("n", "esc", "n", "enter")
		assertContains(t, rendered(ih), "name is required")
		ih.SendKeys(strings.Split("Reading List", "")...)
		ih.SendKeys("enter", "B", "a", "d", "!", "enter")
		assertContains(t, rendered(ih), "invalid id")
		ih.SendKeys("backspace", "backspace", "backspace", "backspace", "enter", "enter")
		out := rendered(ih)
		for _, want := range []string{"Created list: Reading List (reading-list)", "Reading List (reading-list)"} {
			assertContains(t, out, want)
		}
		ih.SendKeys("n")
		ih.SendKeys(strings.Split("Later", "")...)
		ih.SendKeys("enter", "l", "a", "t", "e", "r", "enter", "enter")
		assertContains(t, rendered(ih), "Later (later)")
		ih.SendKeys("n")
		ih.SendKeys(strings.Split("Later", "")...)
		ih.SendKeys("enter", "enter", "enter")
		assertContains(t, rendered(ih), "list 'later' already exists")
		ih.SendKeys("j", "k", "k", "j", "j", "j", "j", "enter")
		if got := ih.CurrentPath(); got != "/social/list" {
			t.Fatalf("enter on a list: path = %q, want /social/list", got)
		}
		ih.SendKey("esc")
		ih.SendKey("D")
		assertContains(t, rendered(ih), "Delete list 'later'? [y/n]")
		ih.SendKeys("n", "p")
		assertContains(t, rendered(ih), "Push to")
		ih.SendKeys("n", "D", "y")
		if strings.Contains(rendered(ih), "(later)") {
			t.Error("the deleted list is still in the picker")
		}
		offline := "file://" + filepath.Join(t.TempDir(), "carol")
		ih.NavigateTo(tuicore.LocListsWithRepo(offline))
		assertContains(t, rendered(ih), "Adding: file://")
		ih.SendKeys("j", "enter")
		assertRendersItem(t, ih, tuicore.LocListRepos("reading-list"), "carol")
		ih.Navigate("/lists")
		ih.SendKey("esc")
		if got := ih.CurrentPath(); got == "/lists" {
			t.Error("esc on the picker stays on /lists")
		}
	})
	// A file:// URL under a temporary directory keeps the first fetch of an added repository offline.
	t.Run("ListReposAddAndRemove", func(t *testing.T) {
		iso := SetupFixture(t)
		if res := social.CreateList(iso.Workdir, "reading", "Reading"); !res.Success {
			t.Fatalf("CreateList: %s", res.Error.Message)
		}
		for _, url := range []string{"https://github.com/carol/atlas", "https://github.com/carol/zephyr"} {
			if res := social.AddRepositoryToList(iso.Workdir, "reading", url, "main", false); !res.Success {
				t.Fatalf("AddRepositoryToList %s: %s", url, res.Error.Message)
			}
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocListRepos("reading"), "☷  Reading", "github.com/carol/atlas", "github.com/carol/zephyr")
		ih.SendKeys("j", "j", "k", "k", "enter")
		if got := ih.CurrentPath(); got != "/social/repository" {
			t.Fatalf("enter on a repository: path = %q, want /social/repository", got)
		}
		ih.SendKey("esc")
		ih.SendKey("x")
		assertContains(t, rendered(ih), "Remove 'atlas' from list? [y/n]")
		ih.SendKeys("n", "j", "x", "y")
		out := rendered(ih)
		assertContains(t, out, "Removed zephyr from list")
		if strings.Contains(out, "github.com/carol/zephyr") {
			t.Error("the removed repository is still in the list")
		}
		ih.SendKey("a")
		ih.SendKeys(strings.Split("https://github.com/carol/atlas *", "")...)
		ih.SendKeys("enter", "enter")
		assertContains(t, rendered(ih), "Already in Reading: https://github.com/carol/atlas")
		ih.SendKey("p")
		assertContains(t, rendered(ih), "Push to")
		ih.SendKey("n")
		ih.SendKeys("a", "enter")
		assertContains(t, rendered(ih), "repository URL is required")
		ih.SendKey("esc")
		assertContains(t, rendered(ih), "a:add repository")
		offline := "file://" + filepath.Join(t.TempDir(), "carol", "quill")
		ih.SendKey("a")
		ih.SendKeys(strings.Split(offline+" *", "")...)
		ih.SendKeys("enter", "enter")
		assertContains(t, rendered(ih), "quill")
		ext := cache.ExternalRepoList{RepoURL: iso.ForkURL, ListID: "picks", Name: "Picks", Repositories: []cache.ListRepository{{RepoURL: "https://github.com/carol/zephyr", Branch: "main"}}}
		if err := cache.InsertExternalRepoList(ext); err != nil {
			t.Fatalf("InsertExternalRepoList: %v", err)
		}
		assertRendersItem(t, ih, tuicore.LocExternalListRepos(iso.ForkURL, "picks"), "☷  Picks · repo", "github.com/carol/zephyr")
		ih.SendKeys("a", "x")
		out = rendered(ih)
		for _, hidden := range []string{"a:add repository", "[y/n]", "Repository *"} {
			if strings.Contains(out, hidden) {
				t.Errorf("an external list shows %q after a and x", hidden)
			}
		}
	})
	t.Run("IssueDetailCardRows", func(t *testing.T) {
		iso := SetupFixture(t)
		repoURL, branch := gitmsg.ResolveRepoURL(iso.Workdir), gitmsg.GetExtBranch(iso.Workdir, "pm")
		closed := pm.GetIssues(repoURL, branch, []string{string(pm.StateClosed)}, "", 10)
		dropped := pm.GetIssues(repoURL, branch, []string{string(pm.StateCancelled)}, "", 10)
		if !closed.Success || len(closed.Data) == 0 || !dropped.Success || len(dropped.Data) == 0 {
			t.Fatal("no closed or dropped issue in the fixture")
		}
		due := time.Date(2030, 3, 4, 0, 0, 0, 0, time.UTC)
		child := pm.CreateIssue(iso.Workdir, "Theme tokens", "Define the color tokens first.", pm.CreateIssueOptions{
			Parent: iso.IssueID, Milestone: iso.MilestoneID, Sprint: iso.SprintID, Due: &due,
			Assignees: []string{"alice@example.com", "carol@example.com"},
			Blocks:    []string{dropped.Data[0].ID}, BlockedBy: []string{closed.Data[0].ID}, Related: []string{iso.IssueID, dropped.Data[0].ID},
			Origin: &protocol.Origin{AuthorName: "Dana", Platform: "github", Time: "2025-05-06T10:00:00Z", URL: "https://github.com/user/repo/issues/7"},
			Labels: []pm.Label{{Scope: "priority", Value: "high"}, {Scope: "kind", Value: "bug"}},
		})
		if !child.Success {
			t.Fatalf("CreateIssue child: %s", child.Error.Message)
		}
		done := pm.CreateIssue(iso.Workdir, "Theme audit", "", pm.CreateIssueOptions{Parent: iso.IssueID, State: pm.StateClosed})
		if !done.Success {
			t.Fatalf("CreateIssue closed child: %s", done.Error.Message)
		}
		comment := social.CreateComment(iso.Workdir, child.Data.ID, "Tokens land first", nil)
		if !comment.Success {
			t.Fatalf("CreateComment: %s", comment.Error.Message)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocPMIssueDetail(child.Data.ID), "Parent", f.IssueSubject, f.MilestoneTitle, f.SprintTitle,
			"Mar 4, 2030", "Alice  alice@example.com", "carol@example.com", "Blocked by", "Add keyboard shortcuts [closed]", "Related", "priority/high", "kind/bug", "Comments (1)", "Dana · via Github", "○  Theme tokens · Dana")
		ih.SendKey("m")
		if got := ih.CurrentPath(); got != "/pm/milestone" {
			t.Errorf("m on the issue: path = %q, want /pm/milestone", got)
		}
		ih.SendKey("esc")
		ih.SendKey("s")
		if got := ih.CurrentPath(); got != "/pm/sprint" {
			t.Errorf("s on the issue: path = %q, want /pm/sprint", got)
		}
		ih.SendKey("esc")
		ih.SendKeys(";", ";", ";", ";", "enter")
		assertContains(t, rendered(ih), "Sub-issues (1 open, 1 closed)")
		ih.SendKeys("/", "t", "o", "k")
		assertContains(t, rendered(ih), "1/")
		ih.SendKeys("enter", "esc", "j", "enter")
		if got := ih.CurrentPath(); got != "/pm/issue" {
			t.Fatalf("enter on a sub-issue: path = %q, want /pm/issue", got)
		}
		assertContains(t, rendered(ih), "Theme audit")
		assertRendersItem(t, ih, tuicore.LocPMIssueDetail(dropped.Data[0].ID), "canceled")
		loc := tuicore.LocPMIssueDetail(child.Data.ID)
		loc.Params["focusID"] = comment.Data.ID
		assertRendersItem(t, ih, loc, "Tokens land first")
		ih.SendKey("enter")
		if got := ih.CurrentPath(); got != "/social/detail" {
			t.Errorf("enter on the focused comment: path = %q, want /social/detail", got)
		}
	})
	t.Run("IssueDetailCloseAndRetract", func(t *testing.T) {
		iso := SetupFixture(t)
		ih := New(t, iso.Workdir, iso.CacheDir)
		loc := tuicore.LocPMIssueDetail(iso.IssueID)
		assertRendersItem(t, ih, loc, f.IssueSubject, "open")
		ih.SendKey("C")
		assertContains(t, rendered(ih), "Close this issue? [y/n]")
		ih.SendKey("y")
		out := rendered(ih)
		assertContains(t, out, "Issue closed")
		assertContains(t, out, "closed")
		if strings.Contains(out, "C:close") {
			t.Error("the footer of a closed issue offers C:close")
		}
		ih.SendKey("h")
		if got := ih.CurrentPath(); got != "/pm/issue/history" {
			t.Fatalf("h on an edited issue: path = %q, want /pm/issue/history", got)
		}
		ih.SendKey("esc")
		waitNextSecond()
		ih.SendKey("X")
		assertContains(t, rendered(ih), "Retract this issue? [y/n]")
		ih.SendKey("y")
		if got := ih.CurrentPath(); got == "/pm/issue" {
			t.Errorf("an applied retraction stays on /pm/issue")
		}
		assertRendersItem(t, ih, loc, "Issue not found")
	})
	// The fork repository at ForkDir publishes two issues of its own, which the sync caches under the registered fork.
	t.Run("IssueDetailAdoptForkIssue", func(t *testing.T) {
		iso := SetupFixture(t)
		adopt := pm.CreateIssue(iso.ForkDir, "Fork crash on start", "", pm.CreateIssueOptions{})
		closeAdopt := pm.CreateIssue(iso.ForkDir, "Fork typo in help", "", pm.CreateIssueOptions{})
		if !adopt.Success || !closeAdopt.Success {
			t.Fatal("CreateIssue on the fork failed")
		}
		if err := client.SyncWorkspace(iso.ForkDir); err != nil {
			t.Fatalf("sync the fork: %v", err)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocPMIssueDetail(adopt.Data.ID), "Fork crash on start", "A:adopt")
		if strings.Contains(rendered(ih), "n:sub-issue") {
			t.Error("the footer of a fork issue offers n:sub-issue")
		}
		ih.SendKey("C")
		assertContains(t, rendered(ih), "Adopt and close this issue from")
		ih.SendKeys("n", "X")
		assertContains(t, rendered(ih), "Propose retracting this issue on")
		ih.SendKeys("n", "A")
		assertContains(t, rendered(ih), "Adopt this issue from")
		ih.SendKey("y")
		out := renderedAfterLoad(ih, []string{"Adopted from"})
		assertContains(t, out, "Adopted from")
		assertContains(t, out, "Fork crash on start")
		ih.SendKeys(";", "esc")
		assertRendersItem(t, ih, tuicore.LocPMIssueDetail(closeAdopt.Data.ID), "Fork typo in help")
		ih.SendKeys("C", "y")
		out = renderedAfterLoad(ih, []string{"Adopted from"})
		assertContains(t, out, "Adopted from")
		assertContains(t, out, "closed")
		assertRendersItem(t, ih, tuicore.LocPMIssueDetail(adopt.Data.ID), "Fork crash on start")
		ih.SendKeys("X", "y")
		assertContains(t, rendered(ih), "Retraction proposed")
	})
	// The priority label keeps a label on the issue through its move to Done; a second move of one issue waits for the next second.
	t.Run("BoardMoves", func(t *testing.T) {
		iso := SetupFixture(t)
		labels := []pm.Label{{Scope: "priority", Value: "high"}}
		if res := pm.UpdateIssue(iso.Workdir, iso.IssueID, pm.UpdateIssueOptions{Labels: &labels}); !res.Success {
			t.Fatalf("UpdateIssue: %s", res.Error.Message)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		ih.Navigate("/pm/board")
		out := renderedAfterLoad(ih, []string{"Backlog (2)"})
		for _, want := range []string{"Backlog (2)", "In Progress (0", "Done (1)"} {
			assertContains(t, out, want)
		}
		ih.SendKeys("<", "right", ">", "right", "right", "right", "left", "left", "left", "down", "end", "up", "home", "down")
		waitNextSecond()
		ih.SendKey(">")
		out = rendered(ih)
		assertContains(t, out, "Backlog (1)")
		assertContains(t, out, "In Progress (1")
		waitNextSecond()
		ih.SendKey(">")
		assertContains(t, rendered(ih), "Review (1")
		waitNextSecond()
		ih.SendKey(">")
		out = rendered(ih)
		assertContains(t, out, "Review (0")
		assertContains(t, out, "Done (2)")
		waitNextSecond()
		ih.SendKey("<")
		out = rendered(ih)
		assertContains(t, out, "Review (1")
		assertContains(t, out, "Done (1)")
		columns := []pm.ColumnConfig{
			{Name: "Bare", Filter: "triage"}, {Name: "Either", Filter: "status:a,status:b"}, {Name: "Loose", Filter: "loose"},
			{Name: "Shut", Filter: "state:closed"}, {Name: "Blank", Filter: "status:"},
		}
		if err := pm.SetBoardConfig(iso.Workdir, pm.BoardConfig{ID: "triage", Name: "Triage", Columns: columns}); err != nil {
			t.Fatalf("SetBoardConfig: %v", err)
		}
		ih.SendKey("r")
		assertContains(t, rendered(ih), "Either (0)")
		for _, keys := range [][]string{{"left", "left", ">"}, {"right", "right", "right", "<"}, {">"}} {
			ih.SendKeys(keys...)
			assertContains(t, rendered(ih), "Move failed: destination column filter must be field:value")
		}
		ih.SendKeys("left", ">", "x", "right", "enter")
		if got := ih.CurrentPath(); got != "/pm/issue" {
			t.Fatalf("enter on a board cell: path = %q, want /pm/issue", got)
		}
	})
	t.Run("BoardFiltersAndScroll", func(t *testing.T) {
		iso := SetupFixture(t)
		for _, subject := range []string{"Fork one", "Fork two"} {
			opts := pm.CreateIssueOptions{Labels: []pm.Label{{Scope: "priority", Value: "low"}, {Scope: "kind", Value: "chore"}}}
			if subject == "Fork one" {
				opts.Assignees = []string{"alice@example.com"}
			}
			if res := pm.CreateIssue(iso.ForkDir, subject, "", opts); !res.Success {
				t.Fatalf("CreateIssue on the fork: %s", res.Error.Message)
			}
		}
		for i := 0; i < 6; i++ {
			if res := pm.CreateIssue(iso.Workdir, "Filler "+string(rune('a'+i)), "", pm.CreateIssueOptions{}); !res.Success {
				t.Fatalf("CreateIssue: %s", res.Error.Message)
			}
		}
		if err := client.SyncWorkspace(iso.ForkDir); err != nil {
			t.Fatalf("sync the fork: %v", err)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		ih.Navigate("/pm/board")
		out := renderedAfterLoad(ih, []string{"Backlog (10)"})
		assertContains(t, out, "Backlog (10)")
		ih.SendKey("m")
		out = rendered(ih)
		assertContains(t, out, "Mine")
		assertContains(t, out, "Backlog (9)")
		ih.SendKey("m")
		for _, lane := range []string{"grouped by priority", "grouped by kind", "grouped by assignees", "grouped by author"} {
			ih.SendKey("s")
			assertContains(t, rendered(ih), lane)
		}
		assertContains(t, rendered(ih), "▾ Bob")
		ih.SendKey("s")
		ih.SetSize(120, 10)
		ih.SendKeys("end", "up", "home", "down", "down", "down", "down", "down", "down", "down")
		assertContains(t, rendered(ih), "Backlog (10)")
		ih.SendKey("enter")
		if got := ih.CurrentPath(); got != "/pm/issue" {
			t.Fatalf("enter on the board: path = %q, want /pm/issue", got)
		}
		ih.SetSize(120, 40)
		ih.SendKeys("right", "left")
		assertContains(t, rendered(ih), "/10")
		ih.SendKeys("esc", "K")
		if got := ih.CurrentPath(); got != "/config/forks" {
			t.Errorf("K on the board: path = %q, want /config/forks", got)
		}
	})
	t.Run("ForksRemoveAndAdd", func(t *testing.T) {
		iso := SetupFixture(t)
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocForks, "Forks (1)", "github.com/bob/repo")
		ih.SendKeys("x", "n", "x")
		assertContains(t, rendered(ih), "Remove fork 'repo'? [y/n]")
		ih.SendKey("y")
		out := rendered(ih)
		for _, want := range []string{"Fork removed: repo", "No forks registered", "Forks (0)"} {
			assertContains(t, out, want)
		}
		ih.SendKeys("x", "pgdown", "enter", "a", "enter")
		assertContains(t, rendered(ih), "URL is required")
		ih.SendKey("esc")
		assertContains(t, rendered(ih), "a:add fork")
		bare := filepath.Join(t.TempDir(), "carol", "quill")
		if err := os.MkdirAll(bare, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", bare, err)
		}
		gitOut(t, bare, "init", "--bare")
		ih.SendKey("a")
		ih.SendKeys(strings.Split("file://"+bare, "")...)
		ih.SendKeys("enter", "enter")
		out = rendered(ih)
		for _, want := range []string{"Fork added:", "a list may fit better", "Forks (1)", "file:///"} {
			assertContains(t, out, want)
		}
		ih.SendKey("a")
		ih.SendKeys(strings.Split("ftp://example.com/x", "")...)
		ih.SendKeys("enter", "enter")
		assertContains(t, rendered(ih), "invalid fork URL")
		if forks := gitmsg.GetForks(iso.Workdir); len(forks) != 1 || !strings.HasSuffix(forks[0], "carol/quill") {
			t.Errorf("forks after the add = %v, want the one file:// fork", forks)
		}
	})
	t.Run("ForksSortSearchAndRemove", func(t *testing.T) {
		iso := SetupFixture(t)
		for _, url := range []string{"https://github.com/carol/atlas", "https://github.com/dave/beacon", "https://github.com/erin/zephyr"} {
			if err := gitmsg.AddFork(iso.Workdir, url); err != nil {
				t.Fatalf("AddFork %s: %v", url, err)
			}
		}
		fetched := sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
		bob, err := cache.GetRepository(iso.ForkURL)
		if err != nil {
			bob = &cache.Repository{URL: iso.ForkURL, Branch: "main"}
		}
		bob.LastFetch = fetched
		for _, repo := range []cache.Repository{*bob, {URL: "https://github.com/dave/beacon", Branch: "main", LastFetch: fetched}, {URL: "https://github.com/erin/zephyr", Branch: "main", LastFetch: sql.NullString{String: "yesterday", Valid: true}}} {
			if err := cache.InsertRepository(repo); err != nil {
				t.Fatalf("InsertRepository %s: %v", repo.URL, err)
			}
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		assertRendersItem(t, ih, tuicore.LocForks, "Forks (4)", "URL ↓", "github.com/erin/zephyr")
		ih.SendKeys("j", "j", "k", "end", "home", "pgdown", "pgup", "ctrl+d", "ctrl+u", "down", "up")
		ih.SetSize(120, 8)
		ih.SendKey("end")
		assertContains(t, rendered(ih), "erin/zephyr")
		ih.SendKey("home")
		assertContains(t, rendered(ih), "bob/repo")
		ih.SetSize(120, 6)
		assertContains(t, rendered(ih), "Forks (4)")
		ih.SetSize(120, 40)
		for _, key := range []string{"f", "c"} {
			ih.SendKey("v")
			assertContains(t, rendered(ih), "Sort by:  [a]name  [f]fetched  [c]commits")
			ih.SendKey(key)
			if strings.Contains(rendered(ih), "URL ↓") {
				t.Errorf("v then %s keeps the sort on the URL", key)
			}
		}
		ih.SendKeys("v", "a")
		assertContains(t, rendered(ih), "URL ↓")
		ih.SendKeys("/", "e", "down", "down", "up")
		assertContains(t, rendered(ih), "Forks (3/4)")
		ih.SendKeys("backspace", "d", "a", "v")
		out := rendered(ih)
		assertContains(t, out, "Forks (1/4)")
		if strings.Contains(out, "carol/atlas") {
			t.Error("the search for dav still lists carol/atlas")
		}
		ih.SendKeys("down", "up", "esc")
		assertContains(t, rendered(ih), "Forks (4)")
		ih.SendKeys("/", "z", "z", "enter")
		out = rendered(ih)
		assertContains(t, out, "No matches")
		assertContains(t, out, "Forks (0/4)")
		ih.SendKeys("/", "backspace", "backspace", "b", "o", "b", "enter")
		if got := ih.CurrentPath(); got != "/social/repository" {
			t.Fatalf("enter on the searched fork: path = %q, want /social/repository", got)
		}
		ih.SendKey("esc")
		ih.SendKeys("end", "x")
		assertContains(t, rendered(ih), "Remove fork 'zephyr'? [y/n]")
		ih.SendKey("y")
		out = rendered(ih)
		assertContains(t, out, "Fork removed: zephyr")
		assertContains(t, out, "Forks (3)")
		if strings.Contains(out, "erin/zephyr") {
			t.Error("the removed fork is still in the list")
		}
	})
	// x stays out of this flow: on the shared harness it deletes a cached repository that later tests read.
	t.Run("CacheConfirmsDismissed", func(t *testing.T) {
		h.Navigate("/cache")
		out := renderedAfterLoad(h, []string{"Database"})
		for _, want := range []string{"Memory", "Database", "Repositories", "Forks"} {
			assertContains(t, out, want)
		}
		for _, step := range []struct{ key, prompt, dismiss string }{
			{"D", "Delete database? y/n", "n"}, {"X", "Delete repositories? y/n", "esc"}, {"F", "Delete forks? y/n", "N"}, {"C", "Delete all cache? y/n", "n"},
		} {
			h.SendKey(step.key)
			assertContains(t, rendered(h), step.prompt)
			h.SendKeys("z", step.dismiss)
			if strings.Contains(rendered(h), "y/n") {
				t.Errorf("%s then %s leaves the confirm open", step.key, step.dismiss)
			}
		}
		h.SendKey("r")
		assertContains(t, rendered(h), "Database")
		if got := h.CurrentPath(); got != "/cache" {
			t.Errorf("r on the cache: path = %q, want /cache", got)
		}
	})
	// The cache clears close the database of the copy, so this flow ends after them.
	t.Run("CacheDeleteAndClear", func(t *testing.T) {
		iso := SetupFixture(t)
		bare := func(parent, name string) string {
			dir := filepath.Join(iso.CacheDir, parent, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			gitOut(t, dir, "init", "--bare")
			return dir
		}
		atlas := bare("repositories", "carol-atlas")
		if err := os.WriteFile(filepath.Join(atlas, "pad"), make([]byte, 1<<17), 0o600); err != nil {
			t.Fatalf("write pad: %v", err)
		}
		home := bare("repositories", "user-repo")
		bare("forks", "bob-"+strings.Repeat("r", 72))
		fetched := sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
		for _, repo := range []cache.Repository{{URL: "https://github.com/carol/atlas", Branch: "main", StoragePath: atlas, LastFetch: fetched}, {URL: gitmsg.ResolveRepoURL(iso.Workdir), Branch: "main", StoragePath: home}} {
			if err := cache.InsertRepository(repo); err != nil {
				t.Fatalf("InsertRepository %s: %v", repo.URL, err)
			}
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		ih.Navigate("/cache")
		out := renderedAfterLoad(ih, []string{"carol-atlas"})
		for _, want := range []string{"carol-atlas (", "user-repo (", "(works", "2 repos,", "1 forks (", "rrrr"} {
			assertContains(t, out, want)
		}
		ih.SendKeys("j", "k", "pgdown", "pgup", "end", "home", "G", "g", "down", "up")
		ih.SetSize(120, 12)
		ih.SendKey("end")
		assertContains(t, rendered(ih), "rrrr")
		ih.SendKey("home")
		assertContains(t, rendered(ih), "carol-atlas")
		ih.SendKey("end")
		assertContains(t, rendered(ih), "rrrr")
		ih.SetSize(120, 40)
		assertContains(t, rendered(ih), "Memory")
		ih.SendKeys("home", "j", "x")
		assertContains(t, rendered(ih), "Error: Cannot delete workspace repository")
		ih.SetSize(40, 40)
		assertContains(t, rendered(ih), "...l-atlas (0)")
		ih.SetSize(120, 40)
		ih.SendKeys("k", "x")
		out = rendered(ih)
		assertContains(t, out, "1 repos,")
		for _, gone := range []string{"carol-atlas", "Error:"} {
			if strings.Contains(out, gone) {
				t.Errorf("after the delete the view still shows %q", gone)
			}
		}
		ih.SendKeys("end", "F")
		assertContains(t, rendered(ih), "Delete forks? y/n")
		ih.SendKey("y")
		assertContains(t, rendered(ih), "Repopulate cache? y/n")
		ih.SendKeys("z", "n")
		out = rendered(ih)
		assertContains(t, out, "0 forks (")
		if strings.Contains(out, "Repopulate") {
			t.Error("n leaves the repopulate prompt open")
		}
		ih.SendKeys("X", "y", "n", "r")
		out = rendered(ih)
		assertContains(t, out, "0 repos, 0 commits")
		if strings.Contains(out, "(works") {
			t.Error("the workspace row stays after the repositories clear")
		}
		ih.SendKeys("D", "y")
		assertContains(t, rendered(ih), "Repopulate cache? y/n")
		if _, err := os.Stat(filepath.Join(iso.CacheDir, "cache.db")); !os.IsNotExist(err) {
			t.Errorf("cache.db after the database clear: err = %v, want not exist", err)
		}
		ih.SendKeys("n", "C")
		assertContains(t, rendered(ih), "Delete all cache? y/n")
		ih.SendKey("y")
		assertContains(t, rendered(ih), "Repopulate cache? y/n")
		ih.SendKeys("n", "x", "end", "j")
		out = rendered(ih)
		assertContains(t, out, "0 forks (")
		if strings.Contains(out, "Error:") {
			t.Error("x on an empty cache shows an error")
		}
	})
	t.Run("NavPanelWalkAndCommit", func(t *testing.T) {
		nh := New(t, f.Workdir, f.CacheDir)
		nh.Navigate("/notifications")
		nh.SendKey("tab")
		seen := map[string]bool{}
		for i := 0; i < 60 && nh.CurrentPath() != "/errorlog"; i++ {
			nh.SendKey("down")
			seen[nh.CurrentPath()] = true
		}
		for _, want := range []string{
			"/analytics", "/social/repository/branches", "/social/repository/tags", "/lists", "/social/list", "/social/followers", "/social/explore",
			"/pm/issues", "/pm/milestones", "/review/prs", "/release/list", "/memo/inherited", "/memo/personal", "/memo/session", "/memo/list",
			"/config/forks", "/pm/config", "/config/site", "/config/identity", "/cache", "/settings", "/errorlog",
		} {
			if !seen[want] {
				t.Errorf("the cursor walk does not preview %s", want)
			}
		}
		nh.SendKeys("k", "up", "j", "pgdown", "pgup")
		if got := nh.CurrentPath(); got != "/settings" {
			t.Fatalf("k, up, j from the error log: path = %q, want /settings", got)
		}
		nh.SendKeys("enter", "down")
		if got := nh.CurrentPath(); got != "/settings" {
			t.Fatalf("down after enter: path = %q, want /settings", got)
		}
		nh.SendKeys("tab", "down")
		if got := nh.CurrentPath(); got != "/errorlog" {
			t.Fatalf("tab then down: path = %q, want /errorlog", got)
		}
		nh.SendKeys("shift+tab", "up")
		if got := nh.CurrentPath(); got != "/errorlog" {
			t.Fatalf("up after shift+tab: path = %q, want /errorlog", got)
		}
		nh.SendKeys("tab", "up", "esc", "down")
		if got := nh.CurrentPath(); got != "/settings" {
			t.Fatalf("up, esc, down on the panel: path = %q, want /settings", got)
		}
		nh.SendKey("%")
		if got := nh.CurrentPath(); got != "/analytics" {
			t.Errorf("%% after the panel: path = %q, want /analytics", got)
		}
		assertContains(t, rendered(nh), "Error Log")
	})
	// Search previews in input mode, so its esc goes back and returns the focus to the content panel.
	t.Run("NavPanelJumpsAndFold", func(t *testing.T) {
		nh := New(t, f.Workdir, f.CacheDir)
		nh.SendKeys("tab", "5")
		for _, step := range []struct{ key, path string }{
			{"2", "/pm/board"}, {"3", "/review/prs"}, {"4", "/release/list"}, {"1", "/social/timeline"}, {"k", "/analytics"}, {"k", "/notifications"}, {"end", "/errorlog"}, {"home", "/search"}, {"esc", "/social/timeline"},
		} {
			nh.SendKey(step.key)
			if got := nh.CurrentPath(); got != step.path {
				t.Fatalf("after %s: path = %q, want %q", step.key, got, step.path)
			}
		}
		nh.SendKey("down")
		if got := nh.CurrentPath(); got != "/social/timeline" {
			t.Fatalf("down after esc: path = %q, want /social/timeline", got)
		}
		nh.SetSize(80, 24)
		nh.SendKeys("tab", "6", "down", "down")
		if got := nh.CurrentPath(); got != "/memo/personal" {
			t.Fatalf("6, down, down at 80x24: path = %q, want /memo/personal", got)
		}
		out := nh.Rendered()
		assertFrameFits(t, out, 80, 24)
		for _, want := range []string{"GitSocial", "Personal", "Session"} {
			assertContains(t, out, want)
		}
		nh.SendKeys("esc", "esc")
		nh.Navigate("/help")
		nh.SendKeys("tab", "down")
		if got := nh.CurrentPath(); got != "/search" {
			t.Fatalf("down from a route with no panel item: path = %q, want /search", got)
		}
		nh.SendKey("esc")
		nh.SetSize(120, 40)
		if got := nh.CurrentPath(); got == "/search" {
			t.Errorf("esc on the previewed search stays on /search")
		}
		assertContains(t, rendered(nh), "Analytics")
	})
	t.Run("NotificationOpensEditedIssue", func(t *testing.T) {
		h.Navigate("/notifications")
		assertContains(t, renderedAfterLoad(h, []string{"edited your item"}), "Bob edited your item")
		h.SendKey("enter")
		if got := h.CurrentPath(); got != "/pm/issue" {
			t.Fatalf("enter on the edit notification: path = %q, want /pm/issue", got)
		}
		assertContains(t, renderedAfterLoad(h, []string{f.IssueSubject}), f.IssueSubject)
	})
	t.Run("ReleaseArtifactExportFails", func(t *testing.T) {
		h.NavigateTo(tuicore.LocReleaseDetail(f.ReleaseID))
		renderedAfterLoad(h, []string{"app-linux-x64.tar.gz"})
		h.SendKeys(";", ";", "enter")
		assertContains(t, rendered(h), "artifact app-linux-x64.tar.gz not found in 1.0.0")
		if got := h.CurrentPath(); got != "/release/detail" {
			t.Errorf("enter on an artifact: path = %q, want /release/detail", got)
		}
		h.SendKey("j")
		if strings.Contains(rendered(h), "not found in 1.0.0") {
			t.Error("a key leaves the export error on the status line")
		}
	})
	t.Run("DiffFullscreenHidesNav", func(t *testing.T) {
		h.NavigateTo(tuicore.LocCommitDiff(strings.TrimSpace(gitOut(t, f.Workdir, "rev-parse", "HEAD"))))
		assertContains(t, rendered(h), "GitSocial")
		for _, step := range []struct {
			keys  []string
			shown bool
		}{{[]string{"v", "v"}, false}, {[]string{"esc"}, true}, {[]string{"v"}, false}, {[]string{"v"}, true}} {
			h.SendKeys(step.keys...)
			if got := strings.Contains(rendered(h), "GitSocial"); got != step.shown {
				t.Fatalf("after %v: nav panel shown = %v, want %v", step.keys, got, step.shown)
			}
			if got := h.CurrentPath(); got != "/diff" {
				t.Fatalf("after %v: path = %q, want /diff", step.keys, got)
			}
		}
		h.SendKey("esc")
		if got := h.CurrentPath(); got == "/diff" {
			t.Error("esc in the split diff stays on /diff")
		}
	})
	t.Run("RefLinksResolve", func(t *testing.T) {
		iso := SetupFixture(t)
		refs := make([]string, 0, 14)
		for _, id := range []string{iso.IssueID, iso.PRID, iso.MilestoneID, iso.SprintID, iso.MemoID, iso.PostID, iso.ReleaseID} {
			refs = append(refs, "#commit:"+protocol.ParseRef(id).Value)
		}
		head := strings.TrimSpace(gitOut(t, iso.Workdir, "rev-parse", "--short=12", "dark-mode"))
		refs = append(refs, "#commit:"+head, "#tag:"+iso.ReleaseTag, "#tag:v9.9.9", "#branch:main", "#list:reading", "#file:README.md@main", "#commit:0123456789ab")
		post := social.CreatePost(iso.Workdir, "Refs "+strings.Join(refs, " "), nil)
		if !post.Success {
			t.Fatalf("CreatePost: %s", post.Error.Message)
		}
		ih := New(t, iso.Workdir, iso.CacheDir)
		seen := map[string]bool{}
		for i := 2; i <= len(refs)+1; i++ {
			ih.NavigateTo(tuicore.LocDetail(post.Data.ID))
			for j := 0; j < i; j++ {
				ih.SendKey(";")
			}
			ih.SendKey("enter")
			seen[ih.CurrentPath()] = true
		}
		for _, want := range []string{
			"/pm/issue", "/review/pr", "/pm/milestone", "/pm/sprint", "/memo/detail", "/social/detail", "/release/detail",
			"/diff", "/social/repository", "/social/list", "/social/repository/code",
		} {
			if !seen[want] {
				t.Errorf("no ref link opens %s, opened %v", want, seen)
			}
		}
	})
	t.Run("FetchModeChoiceAndBusyKeys", func(t *testing.T) {
		iso := SetupFixture(t)
		bare := t.TempDir()
		gitOut(t, iso.Workdir, "clone", "--bare", "-q", iso.Workdir, bare)
		gitOut(t, iso.Workdir, "remote", "set-url", "origin", bare)
		ih := New(t, iso.Workdir, iso.CacheDir)
		ih.Navigate("/cache")
		ih.SendKeys("F", "y", "y")
		prompt := stripANSI(ih.model.Host().State().ChoicePrompt)
		for _, want := range []string{"Workspace fetch mode?", "[d]efault + gitmsg", "branches)"} {
			assertContains(t, prompt, want)
		}
		ih.SendKey("z")
		assertContains(t, stripANSI(ih.model.Host().State().ChoicePrompt), "Workspace fetch mode?")
		ih.SendKey("d")
		if got := ih.model.Host().State().ChoicePrompt; got != "" {
			t.Fatalf("d leaves the fetch mode choice open: %q", got)
		}
		assertContains(t, rendered(ih), "Fetching...")
		ih.SendKeys("f", "F", "y", "y")
		ih.Navigate("/social/timeline")
		ih.SendKey("I")
		ih.NavigateTo(tuicore.LocReleaseDetail(iso.ReleaseID))
		ih.SendKey("L")
		ih.NavigateTo(tuicore.LocConfig("social"))
		ih.SendKey("p")
		out := rendered(ih)
		assertContains(t, out, "Fetching...")
		for _, absent := range []string{"Workspace fetch mode?", "Push to ", "Import", "Pushing"} {
			if strings.Contains(out, absent) {
				t.Errorf("a key during the fetch opens %q:\n%s", absent, out)
			}
		}
		jh := New(t, iso.Workdir, iso.CacheDir)
		jh.SendKey("f")
		out = rendered(jh)
		assertContains(t, out, "Fetching...")
		if strings.Contains(out, "Workspace fetch mode?") {
			t.Error("a saved fetch mode asks again")
		}
		lone := CloneFixture(t, iso.Workdir)
		gitOut(t, lone, "remote", "remove", "origin")
		kh := New(t, lone, iso.CacheDir)
		kh.SendKey("f")
		assertContains(t, rendered(kh), "Fetching...")
	})
	t.Run("PushRemotePicker", func(t *testing.T) {
		workdir := CloneFixture(t, f.Workdir)
		for _, r := range [][2]string{{"r2", "s3://r2.example.com/bucket/repo"}, {"backup", "s3://backup.example.com/bucket/repo"}} {
			gitOut(t, workdir, "remote", "add", r[0], r[1])
		}
		ph := New(t, workdir, f.CacheDir)
		ph.NavigateTo(tuicore.LocConfig("social"))
		ph.SendKeys("p", "z", "9")
		prompt := stripANSI(ph.model.Host().State().ChoicePrompt)
		for _, want := range []string{"Push to which remote? (enter=backup, D=set default)", "[1] backup", "[2] r2", "[D] set default"} {
			assertContains(t, prompt, want)
		}
		ph.SendKey("esc")
		if got := ph.model.Host().State().ChoicePrompt; got != "" {
			t.Fatalf("esc leaves the remote picker open: %q", got)
		}
		ph.SendKeys("p", "enter", "n")
		if got := ph.CurrentPath(); got != "/config" {
			t.Errorf("the picker moves the view: path = %q, want /config", got)
		}
	})
}

// seedPRChange gives the pull request head two hunks in long.txt and a new file, records the tips and returns the head.
func seedPRChange(t *testing.T, workdir, prID string) string {
	t.Helper()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line " + string(rune('a'+i%26)) + " " + strings.Repeat("x", i%7)
	}
	writeLines := func(name string, content []string) {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(strings.Join(content, "\n")+"\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeLines("long.txt", lines)
	gitOut(t, workdir, "add", "long.txt")
	if _, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Add long file"}); err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}
	gitOut(t, workdir, "branch", "-f", "dark-mode", "HEAD")
	gitOut(t, workdir, "checkout", "dark-mode")
	lines[4] = "line five"
	lines[49] = "line fifty"
	writeLines("long.txt", lines)
	writeLines("alpha.txt", []string{"alpha body"})
	gitOut(t, workdir, "add", "long.txt", "alpha.txt")
	head, err := git.CreateCommit(workdir, git.CommitOptions{Message: "Change long file"})
	if err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}
	gitOut(t, workdir, "checkout", "main")
	if res := review.UpdatePRTips(workdir, prID); !res.Success {
		t.Fatalf("UpdatePRTips: %s", res.Error.Message)
	}
	return head
}

// pushToLocalBare points the push URL of origin at a local bare repository, so a merge publishes nothing.
func pushToLocalBare(t *testing.T, workdir string) {
	t.Helper()
	bare := t.TempDir()
	gitOut(t, bare, "init", "--bare")
	gitOut(t, workdir, "config", "remote.origin.pushurl", bare)
}

// gitOut runs one git command and returns its output, failing the test on an error.
func gitOut(t *testing.T, workdir string, args ...string) string {
	t.Helper()
	res, err := git.ExecGit(workdir, args)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return res.Stdout
}

// waitNextSecond sleeps into the next wall-clock second, so the next commit's timestamp follows the last one.
func waitNextSecond() {
	now := time.Now()
	time.Sleep(now.Truncate(time.Second).Add(time.Second).Sub(now))
}
