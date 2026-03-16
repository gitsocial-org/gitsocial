// util_register_test.go - Tests for review card conversion and helpers
package tuireview

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/review"
)

func TestShortenBranchRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"local branch", "#branch:main", "main"},
		{"local feature", "#branch:feature/auth", "feature/auth"},
		{"remote branch", "https://github.com/user/repo#branch:main", "https://github.com/user/repo#branch:main"},
		{"empty", "", ""},
		{"not a branch ref", "#commit:abc123456789", "#commit:abc123456789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenBranchRef(tt.ref)
			if got != tt.want {
				t.Errorf("shortenBranchRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestBranchRefLocation(t *testing.T) {
	loc := branchRefLocation("#branch:main", "https://github.com/user/repo")
	if loc == nil {
		t.Fatal("branchRefLocation() returned nil for valid local branch")
	}
	if loc.Params["url"] != "https://github.com/user/repo" {
		t.Errorf("url = %q", loc.Params["url"])
	}

	loc = branchRefLocation("#branch:dev", "")
	if loc != nil {
		t.Error("branchRefLocation() should return nil when no workspace URL and local ref")
	}

	loc = branchRefLocation("", "https://github.com/user/repo")
	if loc != nil {
		t.Error("branchRefLocation() should return nil for empty ref")
	}

	loc = branchRefLocation("#commit:abc123456789", "https://github.com/user/repo")
	if loc != nil {
		t.Error("branchRefLocation() should return nil for non-branch ref")
	}
}

func TestPrDimmedChecker(t *testing.T) {
	open := review.PullRequest{State: review.PRStateOpen}
	if prDimmedChecker(open) {
		t.Error("open PR should not be dimmed")
	}

	closed := review.PullRequest{State: review.PRStateClosed}
	if !prDimmedChecker(closed) {
		t.Error("closed PR should be dimmed")
	}

	retracted := review.PullRequest{IsRetracted: true}
	if !prDimmedChecker(retracted) {
		t.Error("retracted PR should be dimmed")
	}

	wrapped := prItemData{PR: review.PullRequest{State: review.PRStateClosed}}
	if !prDimmedChecker(wrapped) {
		t.Error("wrapped closed PR should be dimmed")
	}

	if prDimmedChecker("invalid") {
		t.Error("invalid type should not be dimmed")
	}
}

func TestPRToCard_basic(t *testing.T) {
	pr := review.PullRequest{
		Subject:   "Add auth",
		Body:      "Implements OAuth",
		State:     review.PRStateOpen,
		Author:    review.Author{Name: "Alice", Email: "alice@test.com"},
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}
	card := PRToCard(pr)
	if card.Header.Icon != "⑂" {
		t.Errorf("Icon = %q, want ⑂", card.Header.Icon)
	}
	if card.Header.Title != "Add auth" {
		t.Errorf("Title = %q, want 'Add auth'", card.Header.Title)
	}
	if card.Content.Text != "Implements OAuth" {
		t.Errorf("Content.Text = %q", card.Content.Text)
	}
}

func TestPRToCard_withBranches(t *testing.T) {
	pr := review.PullRequest{
		Subject: "Feature",
		State:   review.PRStateOpen,
		Base:    "#branch:main",
		Head:    "#branch:feature/auth",
		Author:  review.Author{Name: "Dev"},
	}
	card := PRToCard(pr)
	foundBase := false
	foundHead := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "main ←" {
			foundBase = true
		}
		if p.Text == "feature/auth" {
			foundHead = true
		}
	}
	if !foundBase {
		t.Error("should include base branch in subtitle")
	}
	if !foundHead {
		t.Error("should include head branch in subtitle")
	}
}

func TestPRToCard_withReviewSummary(t *testing.T) {
	pr := review.PullRequest{
		Subject: "PR",
		State:   review.PRStateOpen,
		Author:  review.Author{Name: "Dev"},
		ReviewSummary: review.ReviewSummary{
			Approved:         2,
			ChangesRequested: 1,
		},
	}
	card := PRToCard(pr)
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "[✓2 ✗1]" {
			found = true
		}
	}
	if !found {
		t.Error("should include review summary [✓2 ✗1] in subtitle")
	}
}

func TestPRToCardWithOptions_showEmail(t *testing.T) {
	pr := review.PullRequest{
		Subject: "PR",
		State:   review.PRStateOpen,
		Author:  review.Author{Name: "Bob", Email: "bob@test.com"},
	}
	card := PRToCardWithOptions(pr, PRToCardOptions{ShowEmail: true})
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "Bob <bob@test.com>" {
			found = true
		}
	}
	if !found {
		t.Error("ShowEmail=true should include email in subtitle")
	}
}

func TestPRToCard_withOriginalAuthor(t *testing.T) {
	origAuthor := review.Author{Name: "Original"}
	pr := review.PullRequest{
		Subject:        "Fork PR",
		State:          review.PRStateOpen,
		Author:         review.Author{Name: "Forker"},
		OriginalAuthor: &origAuthor,
	}
	card := PRToCard(pr)
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "Original" {
			found = true
		}
	}
	if !found {
		t.Error("should use OriginalAuthor when present")
	}
}

func TestFeedbackToCard_approved(t *testing.T) {
	fb := review.Feedback{
		Content:     "LGTM",
		ReviewState: review.ReviewStateApproved,
		Author:      review.Author{Name: "Reviewer", Email: "rev@test.com"},
		Timestamp:   time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}
	card := FeedbackToCard(fb, "", true, false)
	if card.Header.Icon != "✓" {
		t.Errorf("Icon = %q, want ✓ for approved", card.Header.Icon)
	}
	if card.Header.Badge != "approved" {
		t.Errorf("Badge = %q, want 'approved'", card.Header.Badge)
	}
}

func TestFeedbackToCard_changesRequested(t *testing.T) {
	fb := review.Feedback{
		Content:     "Needs work",
		ReviewState: review.ReviewStateChangesRequested,
		Author:      review.Author{Name: "Reviewer"},
	}
	card := FeedbackToCard(fb, "", true, false)
	if card.Header.Icon != "✗" {
		t.Errorf("Icon = %q, want ✗", card.Header.Icon)
	}
	if card.Header.Badge != "requested changes" {
		t.Errorf("Badge = %q, want 'requested changes'", card.Header.Badge)
	}
}

func TestFeedbackToCard_suggestion(t *testing.T) {
	fb := review.Feedback{
		Suggestion:  true,
		ReviewState: review.ReviewStateApproved,
		Author:      review.Author{Name: "Dev"},
	}
	card := FeedbackToCard(fb, "", true, false)
	if card.Header.Badge != "approved [suggestion]" {
		t.Errorf("Badge = %q, want 'approved [suggestion]'", card.Header.Badge)
	}

	fb2 := review.Feedback{
		Suggestion: true,
		Author:     review.Author{Name: "Dev"},
	}
	card2 := FeedbackToCard(fb2, "", true, false)
	if card2.Header.Badge != "[suggestion]" {
		t.Errorf("Badge = %q, want '[suggestion]'", card2.Header.Badge)
	}
}

func TestFeedbackToCard_withFileLocation(t *testing.T) {
	fb := review.Feedback{
		Content: "Fix this",
		File:    "main.go",
		NewLine: 42,
		Author:  review.Author{Name: "Rev"},
	}
	card := FeedbackToCard(fb, "", true, false)
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "main.go:42" {
			found = true
		}
	}
	if !found {
		t.Error("should include file:line in subtitle")
	}
}

func TestFeedbackToCard_withFileRange(t *testing.T) {
	fb := review.Feedback{
		Content:    "Fix this block",
		File:       "main.go",
		NewLine:    10,
		NewLineEnd: 20,
		Author:     review.Author{Name: "Rev"},
	}
	card := FeedbackToCard(fb, "", true, false)
	found := false
	for _, p := range card.Header.Subtitle {
		if p.Text == "main.go:10-20" {
			found = true
		}
	}
	if !found {
		t.Error("should include file:line-end in subtitle")
	}
}

func TestFeedbackToCard_isMe(t *testing.T) {
	fb := review.Feedback{
		Author: review.Author{Name: "Me", Email: "me@test.com"},
	}
	card := FeedbackToCard(fb, "me@test.com", true, false)
	if !card.Header.IsMe {
		t.Error("IsMe should be true when emails match")
	}

	card2 := FeedbackToCard(fb, "other@test.com", true, false)
	if card2.Header.IsMe {
		t.Error("IsMe should be false when emails differ")
	}
}

func TestReviewNotificationCardRenderer(t *testing.T) {
	tests := []struct {
		name     string
		notif    review.ReviewNotification
		wantIcon string
	}{
		{"fork-pr", review.ReviewNotification{Type: "fork-pr", ActorName: "User", PRSubject: "PR"}, "⑂"},
		{"approved", review.ReviewNotification{Type: "approved", ActorName: "User", Content: "LGTM"}, "✓"},
		{"changes-requested", review.ReviewNotification{Type: "changes-requested", ActorName: "User", Content: "Fix"}, "✗"},
		{"feedback", review.ReviewNotification{Type: "feedback", ActorName: "User", Content: "Note"}, "↩"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := reviewNotificationCardRenderer(tt.notif, nil)
			if card.Header.Icon != tt.wantIcon {
				t.Errorf("Icon = %q, want %q", card.Header.Icon, tt.wantIcon)
			}
		})
	}

	card := reviewNotificationCardRenderer("invalid", nil)
	if card.Header.Title != "Invalid notification" {
		t.Errorf("Title = %q for invalid type", card.Header.Title)
	}
}

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no ansi", "hello world", "hello world"},
		{"color", "\x1b[31mRed\x1b[0m", "Red"},
		{"bold", "\x1b[1mBold\x1b[0m", "Bold"},
		{"multiple", "\x1b[31m\x1b[1mBoldRed\x1b[0m", "BoldRed"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAnsi(tt.input)
			if got != tt.want {
				t.Errorf("stripAnsi(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFirstSliceVal(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"empty", nil, ""},
		{"empty slice", []string{}, ""},
		{"single", []string{"a"}, "a"},
		{"multiple", []string{"a", "b", "c"}, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstSliceVal(tt.input)
			if got != tt.want {
				t.Errorf("firstSliceVal(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildBranchOptions(t *testing.T) {
	branches := []string{"main", "develop", "feature/auth"}
	opts := buildBranchOptions(branches, nil)
	if len(opts) != 3 {
		t.Fatalf("len = %d, want 3", len(opts))
	}
	for i, b := range branches {
		if opts[i].Label != b || opts[i].Value != b {
			t.Errorf("opts[%d] = {%q, %q}, want {%q, %q}", i, opts[i].Label, opts[i].Value, b, b)
		}
	}
}

func TestBuildBranchOptionsWithForks(t *testing.T) {
	branches := []string{"main"}
	forkBranches := map[string][]string{
		"https://github.com/alice/repo": {"main", "feature/x"},
	}
	opts := buildBranchOptions(branches, forkBranches)
	if len(opts) != 3 {
		t.Fatalf("len = %d, want 3", len(opts))
	}
	if opts[0].Label != "main" || opts[0].Value != "main" {
		t.Errorf("local branch: got {%q, %q}", opts[0].Label, opts[0].Value)
	}
	foundForkMain := false
	for _, opt := range opts[1:] {
		if opt.Value == "https://github.com/alice/repo#branch:main" && opt.Label == "https://github.com/alice/repo#branch:main" {
			foundForkMain = true
		}
	}
	if !foundForkMain {
		t.Errorf("fork branch not found in options: %+v", opts)
	}
}

func TestFormatBranchLabel(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"main", "main"},
		{"#branch:main", "main"},
		{"https://github.com/alice/repo#branch:feature", "https://github.com/alice/repo#branch:feature"},
	}
	for _, tt := range tests {
		got := formatBranchLabel(tt.ref)
		if got != tt.want {
			t.Errorf("formatBranchLabel(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestBuildContributorOptions(t *testing.T) {
	contributors := []cache.Contributor{
		{Name: "Alice", Email: "alice@test.com"},
		{Name: "", Email: "anon@test.com"},
	}
	opts := buildContributorOptions(contributors)
	if len(opts) != 2 {
		t.Fatalf("len = %d, want 2", len(opts))
	}
	if opts[0].Label != "Alice <alice@test.com>" {
		t.Errorf("opts[0].Label = %q", opts[0].Label)
	}
	if opts[0].Value != "alice@test.com" {
		t.Errorf("opts[0].Value = %q", opts[0].Value)
	}
	if opts[1].Label != "anon@test.com" {
		t.Errorf("opts[1].Label = %q, want email-only for empty name", opts[1].Label)
	}
}

func TestBuildIssueOptions(t *testing.T) {
	issues := []pm.Issue{
		{ID: "id1", Subject: "Bug fix"},
		{ID: "id2", Subject: "Feature"},
	}
	opts := buildIssueOptions(issues)
	if len(opts) != 2 {
		t.Fatalf("len = %d, want 2", len(opts))
	}
	if opts[0].Label != "Bug fix" || opts[0].Value != "id1" {
		t.Errorf("opts[0] = {%q, %q}", opts[0].Label, opts[0].Value)
	}
}

func TestPrCardRenderer_invalidType(t *testing.T) {
	card := prCardRenderer("not a PR", nil)
	if card.Header.Title != "Invalid pull request" {
		t.Errorf("Title = %q, want 'Invalid pull request'", card.Header.Title)
	}
}
