// provider_test.go - Tests for the text fields of the memo notification provider
package memo

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

func TestNotificationText_fields(t *testing.T) {
	setupTestDB(t)
	freshHome(t)
	const repoURL, memo, comment = "https://github.com/test/text-memo", "3e4000000001", "3e4000000002"
	insertMemoTestCommit(t, repoURL, memo, MemoBranch, "me@x.com")
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: comment, RepoURL: repoURL, Branch: "gitmsg/social", AuthorName: "Other", AuthorEmail: "other@x.com",
		Message: "Looks good\n\nThe rest", Timestamp: time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := social.InsertSocialItem(social.SocialItem{RepoURL: repoURL, Hash: comment, Branch: "gitmsg/social", Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL), OriginalHash: cache.ToNullString(memo), OriginalBranch: cache.ToNullString(MemoBranch)}); err != nil {
		t.Fatalf("InsertSocialItem() error = %v", err)
	}
	got, err := getMemoCommentNotifications("me@x.com", notifications.Filter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("getMemoCommentNotifications() = %d, %v, want 1", len(got), err)
	}
	if n := got[0]; n.Subject != "memo" || n.Excerpt != "Looks good" || n.Phrase != "" || n.Glyph != "↩" {
		t.Errorf("memo-comment = %q, %q, %q, %q, want the memo as the subject and the comment as the excerpt", n.Subject, n.Excerpt, n.Phrase, n.Glyph)
	}
	dir := initTestRepo(t)
	inheritedURL := protocol.NormalizeURL("https://example.com/org-policies.git")
	if r := AddInherit(dir, inheritedURL); !r.Success {
		t.Fatalf("AddInherit: %s", r.Error.Message)
	}
	const policy = "3e4000000003"
	message := buildMemoContent("Encrypt at rest", "Each store", CreateMemoOptions{Labels: []string{"priority/critical"}}, "")
	if err := cache.InsertCommits([]cache.Commit{{Hash: policy, RepoURL: inheritedURL, Branch: MemoBranch, AuthorName: "Bot", AuthorEmail: "bot@x.com", Message: message, Timestamp: time.Now()}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := InsertMemoItem(MemoItem{RepoURL: inheritedURL, Hash: policy, Branch: MemoBranch, Type: "memo"}); err != nil {
		t.Fatalf("InsertMemoItem() error = %v", err)
	}
	policies, err := getInheritedPolicyNotifications(dir, "me@x.com", notifications.Filter{})
	if err != nil || len(policies) != 1 {
		t.Fatalf("getInheritedPolicyNotifications() = %d, %v, want 1", len(policies), err)
	}
	if n := policies[0]; n.Subject != "Encrypt at rest" || n.Excerpt != "" || n.Glyph != "☞" {
		t.Errorf("inherited-policy = %q, %q, %q, want the memo as the subject and the memo glyph", n.Subject, n.Excerpt, n.Glyph)
	}
}
