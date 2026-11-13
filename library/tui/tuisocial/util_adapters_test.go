// util_adapters_test.go - Tests for social item type routing
package tuisocial

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiviews"
)

func TestPostItem_ItemType_RegularPost(t *testing.T) {
	post := social.Post{
		ID:   "post123",
		Type: social.PostTypePost,
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	itemType := item.ItemType()
	if itemType.Extension != "social" {
		t.Errorf("Expected extension 'social', got '%s'", itemType.Extension)
	}
	if itemType.Type != "post" {
		t.Errorf("Expected type 'post', got '%s'", itemType.Type)
	}
}

func TestPostItem_ItemType_Comment(t *testing.T) {
	post := social.Post{
		ID:   "comment123",
		Type: social.PostTypeComment,
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	itemType := item.ItemType()
	if itemType.Extension != "social" {
		t.Errorf("Expected extension 'social', got '%s'", itemType.Extension)
	}
	if itemType.Type != "comment" {
		t.Errorf("Expected type 'comment', got '%s'", itemType.Type)
	}
}

func TestPostItem_ItemType_CommentOnPMIssue(t *testing.T) {
	post := social.Post{
		ID:                "comment456",
		Type:              social.PostTypeComment,
		OriginalExtension: "pm",
		OriginalType:      "issue",
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	item.OriginalExt = post.OriginalExtension
	item.OriginalType = post.OriginalType
	itemType := item.ItemType()
	if itemType.Extension != "pm" {
		t.Errorf("Expected extension 'pm' for comment on PM issue, got '%s'", itemType.Extension)
	}
	if itemType.Type != "issue" {
		t.Errorf("Expected type 'issue' for comment on PM issue, got '%s'", itemType.Type)
	}
}

func TestPostItem_ItemType_CommentOnPMMilestone(t *testing.T) {
	post := social.Post{
		ID:                "comment789",
		Type:              social.PostTypeComment,
		OriginalExtension: "pm",
		OriginalType:      "milestone",
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	item.OriginalExt = post.OriginalExtension
	item.OriginalType = post.OriginalType
	itemType := item.ItemType()
	if itemType.Extension != "pm" {
		t.Errorf("Expected extension 'pm', got '%s'", itemType.Extension)
	}
	if itemType.Type != "milestone" {
		t.Errorf("Expected type 'milestone', got '%s'", itemType.Type)
	}
}

func TestPostItem_ItemType_Repost(t *testing.T) {
	// Reposts should always navigate to social detail, even if original is from another extension
	post := social.Post{
		ID:                "repost123",
		Type:              social.PostTypeRepost,
		OriginalExtension: "pm", // This shouldn't matter for reposts
		OriginalType:      "issue",
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	// Don't set OriginalExt for reposts - they navigate to the repost itself
	itemType := item.ItemType()
	// Reposts are social items - they navigate to the repost, not the original
	if itemType.Extension != "social" {
		t.Errorf("Expected extension 'social' for repost, got '%s'", itemType.Extension)
	}
	if itemType.Type != "repost" {
		t.Errorf("Expected type 'repost', got '%s'", itemType.Type)
	}
}

func TestPostItem_ItemType_Quote(t *testing.T) {
	post := social.Post{
		ID:   "quote123",
		Type: social.PostTypeQuote,
	}
	item := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	itemType := item.ItemType()
	if itemType.Extension != "social" {
		t.Errorf("Expected extension 'social' for quote, got '%s'", itemType.Extension)
	}
	if itemType.Type != "quote" {
		t.Errorf("Expected type 'quote', got '%s'", itemType.Type)
	}
}

func TestFollowNotificationItem_ItemType(t *testing.T) {
	notification := social.Notification{
		ID:   "follow123",
		Type: social.NotificationTypeFollow,
	}
	item := tuicore.NewItem(notification.ID, "social", "follow", time.Now(), notification)
	itemType := item.ItemType()
	if itemType.Extension != "social" {
		t.Errorf("Expected extension 'social', got '%s'", itemType.Extension)
	}
	if itemType.Type != "follow" {
		t.Errorf("Expected type 'follow', got '%s'", itemType.Type)
	}
}

func TestNavTargetIntegration(t *testing.T) {
	// Test that the registered nav targets work correctly

	// Social post -> /social/detail
	post := social.Post{ID: "abc123", Type: social.PostTypePost}
	socialItem := tuicore.NewItem(post.ID, "social", string(post.Type), time.Now(), post)
	loc := tuicore.GetNavTarget(socialItem)
	if loc.Path != "/social/detail" {
		t.Errorf("Social post should navigate to /social/detail, got %s", loc.Path)
	}

	// Comment on PM issue -> should use PM nav target (if registered)
	// Note: PM targets are registered in tuipm init(), so we test the ItemType here
	pmComment := social.Post{
		ID:                "def456",
		Type:              social.PostTypeComment,
		OriginalExtension: "pm",
		OriginalType:      "issue",
	}
	pmItem := tuicore.NewItem(pmComment.ID, "social", string(pmComment.Type), time.Now(), pmComment)
	pmItem.OriginalExt = pmComment.OriginalExtension
	pmItem.OriginalType = pmComment.OriginalType
	itemType := pmItem.ItemType()
	if itemType.Extension != "pm" || itemType.Type != "issue" {
		t.Errorf("Comment on PM issue should have ItemType {pm, issue}, got {%s, %s}",
			itemType.Extension, itemType.Type)
	}
}

// activityRowWorkdir is the workdir for which activityProvider gives its event.
const activityRowWorkdir = "activity-row-test"

// activityProvider gives one activity event of an assign for activityRowWorkdir.
type activityProvider struct{}

// init registers the provider that TestActivityRow_timelineTarget reads.
func init() {
	notifications.RegisterProvider("test-activity", activityProvider{})
}

// GetNotifications returns the activity event of an assign of an issue.
func (activityProvider) GetNotifications(workdir string, _ notifications.Filter) ([]notifications.Notification, error) {
	if workdir != activityRowWorkdir {
		return nil, nil
	}
	post := activityAssignPost()
	return []notifications.Notification{{
		RepoURL: post.Repository, Hash: "ed0000000001", Branch: post.Branch, Type: "activity", Source: "social",
		Timestamp: post.Timestamp, Item: social.Notification{ID: post.ID, Type: social.NotificationTypeActivity, Item: &post},
	}}, nil
}

// activityAssignPost returns the timeline post of an assign of an issue.
func activityAssignPost() social.Post {
	return social.Post{
		ID: "https://github.com/a/b#commit:ed0000000001@gitmsg/pm", Repository: "https://github.com/a/b", Branch: "gitmsg/pm",
		Type: social.PostTypePost, HeaderExt: "pm", HeaderType: "issue", Action: "assigned",
		EditOf: "#commit:a55000000001@gitmsg/pm", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}
}

// TestActivityRow_timelineTarget checks that the activity row of an assign opens what the timeline row opens: the issue, not the edit commit.
func TestActivityRow_timelineTarget(t *testing.T) {
	result, err := makeGetNotificationsFunc("", func() bool { return false })(activityRowWorkdir, false)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("notifications = %+v, %v, want the one activity row", result.Items, err)
	}
	got := tuiviews.NotificationTarget(result.Meta[0], result.Items[0])
	want := tuicore.GetNavTarget(postsToItems([]social.Post{activityAssignPost()}, "", false, "")[0])
	if !reflect.DeepEqual(got, want) {
		t.Errorf("activity row target = %+v, want the target of the timeline row %+v", got, want)
	}
	if issue := "https://github.com/a/b#commit:a55000000001@gitmsg/pm"; !strings.Contains(fmt.Sprint(got.Params), issue) {
		t.Errorf("activity row target = %+v, want the issue %s", got, issue)
	}
}
