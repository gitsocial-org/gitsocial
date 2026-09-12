// util_router.go - Navigation infrastructure: routing and locations
package tuicore

import (
	"fmt"
	"strings"
)

// Location represents a navigation destination
type Location struct {
	Path   string            // e.g., "/social/timeline", "/social/detail"
	Params map[string]string // e.g., {"postID": "abc123"}
}

// Router manages navigation state with browser-like history
type Router struct {
	location Location
	history  []Location
}

// NewRouter creates a router with initial location.
func NewRouter(initial Location) *Router {
	return &Router{
		location: initial,
		history:  make([]Location, 0),
	}
}

// Location returns current location.
func (r *Router) Location() Location {
	return r.location
}

// Push navigates to a new location, adding current to history.
func (r *Router) Push(loc Location) {
	r.history = append(r.history, r.location)
	r.location = loc
}

// Replace navigates without adding to history.
func (r *Router) Replace(loc Location) {
	r.location = loc
}

// Back returns to previous location, returns false if no history.
func (r *Router) Back() bool {
	if len(r.history) == 0 {
		return false
	}
	r.location = r.history[len(r.history)-1]
	r.history = r.history[:len(r.history)-1]
	return true
}

// Common locations
var (
	LocTimeline      = Location{Path: "/social/timeline"}
	LocSearch        = Location{Path: "/search"}
	LocNotifications = Location{Path: "/notifications"}
	LocSettings      = Location{Path: "/settings"}
	LocCache         = Location{Path: "/cache"}
	LocMyRepo        = Location{Path: "/social/repository"}
	LocLists         = Location{Path: "/lists"}
	LocAnalytics     = Location{Path: "/analytics"}
	LocHelp          = Location{Path: "/help"}
	LocErrorLog      = Location{Path: "/errorlog"}
)

// LocAnalyticsRepo creates a location for repository-scoped analytics.
func LocAnalyticsRepo(repoURL string) Location {
	return Location{Path: "/analytics", Params: map[string]string{"url": repoURL}}
}

// LocDetail creates a location for a post detail view.
func LocDetail(postID string) Location {
	return Location{Path: "/social/detail", Params: map[string]string{"postID": postID}}
}

// LocSocialPostForm creates a location for the social post form. mode is
// "new"/"comment"/"quote"/"edit"; targetID is required for comment/quote/edit.
func LocSocialPostForm(mode, targetID string) Location {
	params := map[string]string{"mode": mode}
	if targetID != "" {
		params["targetID"] = targetID
	}
	return Location{Path: "/social/post-form", Params: params}
}

// LocRepository creates a location for a repository view.
func LocRepository(url, branch string) Location {
	params := map[string]string{"url": url}
	if branch != "" {
		params["branch"] = branch
	}
	return Location{Path: "/social/repository", Params: params}
}

// LocList creates a location for a list posts view.
func LocList(listID string) Location {
	return Location{Path: "/social/list", Params: map[string]string{"listID": listID}}
}

// LocListsWithRepo creates a location for lists containing a repository.
func LocListsWithRepo(repoURL string) Location {
	return Location{Path: "/lists", Params: map[string]string{"repoURL": repoURL}}
}

// LocExternalList creates a location for an external list view.
func LocExternalList(ownerRepoURL, listID string) Location {
	return Location{Path: "/social/list", Params: map[string]string{
		"listID": listID,
		"owner":  ownerRepoURL,
	}}
}

// LocListRepos creates a location for a list repositories view.
func LocListRepos(listID string) Location {
	return Location{Path: "/social/list/repos", Params: map[string]string{"listID": listID}}
}

// LocExternalListRepos creates a location for external list repositories.
func LocExternalListRepos(ownerRepoURL, listID string) Location {
	return Location{Path: "/social/list/repos", Params: map[string]string{
		"listID": listID,
		"owner":  ownerRepoURL,
	}}
}

// LocExplore is the repository discovery (browse all known repos) view.
var LocExplore = Location{Path: "/social/explore"}

// LocExploreRelated creates a location listing repositories related to url.
// Served by the same /social/explore view; the `related` param switches modes.
func LocExploreRelated(url string) Location {
	return Location{Path: "/social/explore", Params: map[string]string{"related": url}}
}

// LocFollowers lists the workspaces that follow this workspace.
var LocFollowers = Location{Path: "/social/followers"}

// LocConfig creates a location for the configuration view.
func LocConfig(extension string) Location {
	return Location{Path: "/config", Params: map[string]string{"extension": extension}}
}

// LocSearchQuery creates a location for search with a query.
func LocSearchQuery(query string) Location {
	return Location{Path: "/search", Params: map[string]string{"q": query}}
}

// LocHistory creates a location for a post's edit history.
func LocHistory(postID string) Location {
	return Location{Path: "/social/history", Params: map[string]string{"postID": postID}}
}

// LocHistoryDiff creates a location for a post-history version diff.
func LocHistoryDiff(postID, fromID, toID string) Location {
	return Location{Path: "/social/history/diff", Params: map[string]string{
		"postID": postID,
		"from":   fromID,
		"to":     toID,
	}}
}

// LocRepoLists creates a location for a repository's defined lists.
func LocRepoLists(repoURL string) Location {
	return Location{Path: "/social/repository/lists", Params: map[string]string{"url": repoURL}}
}

// LocCommitDiff creates a location for a generic commit diff view.
func LocCommitDiff(commit string) Location {
	return Location{Path: "/diff", Params: map[string]string{"commit": commit}}
}

// PM locations

// LocPMBoard creates a location for the PM board view.
var LocPMBoard = Location{Path: "/pm/board"}

// LocPMIssues creates a location for the PM issues list.
var LocPMIssues = Location{Path: "/pm/issues"}

// LocPMIssueDetail creates a location for a PM issue detail view.
func LocPMIssueDetail(issueID string) Location {
	return Location{Path: "/pm/issue", Params: map[string]string{"issueID": issueID}}
}

// LocPMNewIssue creates a location for the new issue form.
var LocPMNewIssue = Location{Path: "/pm/new-issue"}

// LocPMNewSubIssue creates a location for the new issue form pre-filled to
// create a sub-issue of the given parent.
func LocPMNewSubIssue(parentID string) Location {
	return Location{Path: "/pm/new-issue", Params: map[string]string{"parentID": parentID}}
}

// LocPMEditIssue creates a location for editing an issue.
func LocPMEditIssue(issueID string) Location {
	return Location{Path: "/pm/edit-issue", Params: map[string]string{"issueID": issueID}}
}

// LocPMIssueHistory creates a location for an issue's edit history.
func LocPMIssueHistory(issueID string) Location {
	return Location{Path: "/pm/issue/history", Params: map[string]string{"issueID": issueID}}
}

// LocPMIssueHistoryDiff creates a location for an issue-history version diff.
func LocPMIssueHistoryDiff(issueID, fromID, toID string) Location {
	return Location{Path: "/pm/issue/history/diff", Params: map[string]string{
		"issueID": issueID,
		"from":    fromID,
		"to":      toID,
	}}
}

// LocPMConfig creates a location for PM configuration.
var LocPMConfig = Location{Path: "/pm/config"}

// LocPMMilestones creates a location for the PM milestones list.
var LocPMMilestones = Location{Path: "/pm/milestones"}

// LocPMMilestoneDetail creates a location for a PM milestone detail view.
func LocPMMilestoneDetail(milestoneID string) Location {
	return Location{Path: "/pm/milestone", Params: map[string]string{"milestoneID": milestoneID}}
}

// LocPMNewMilestone creates a location for the new milestone form.
var LocPMNewMilestone = Location{Path: "/pm/new-milestone"}

// LocPMEditMilestone creates a location for editing a milestone.
func LocPMEditMilestone(milestoneID string) Location {
	return Location{Path: "/pm/edit-milestone", Params: map[string]string{"milestoneID": milestoneID}}
}

// LocPMMilestoneHistory creates a location for a milestone's edit history.
func LocPMMilestoneHistory(milestoneID string) Location {
	return Location{Path: "/pm/milestone/history", Params: map[string]string{"milestoneID": milestoneID}}
}

// LocPMMilestoneHistoryDiff creates a location for a milestone-history version diff.
func LocPMMilestoneHistoryDiff(milestoneID, fromID, toID string) Location {
	return Location{Path: "/pm/milestone/history/diff", Params: map[string]string{
		"milestoneID": milestoneID,
		"from":        fromID,
		"to":          toID,
	}}
}

// LocPMSprints creates a location for the PM sprints list.
var LocPMSprints = Location{Path: "/pm/sprints"}

// LocPMSprintDetail creates a location for a PM sprint detail view.
func LocPMSprintDetail(sprintID string) Location {
	return Location{Path: "/pm/sprint", Params: map[string]string{"sprintID": sprintID}}
}

// LocPMNewSprint creates a location for the new sprint form.
var LocPMNewSprint = Location{Path: "/pm/new-sprint"}

// LocPMEditSprint creates a location for editing a sprint.
func LocPMEditSprint(sprintID string) Location {
	return Location{Path: "/pm/edit-sprint", Params: map[string]string{"sprintID": sprintID}}
}

// LocPMSprintHistory creates a location for a sprint's edit history.
func LocPMSprintHistory(sprintID string) Location {
	return Location{Path: "/pm/sprint/history", Params: map[string]string{"sprintID": sprintID}}
}

// LocPMSprintHistoryDiff creates a location for a sprint-history version diff.
func LocPMSprintHistoryDiff(sprintID, fromID, toID string) Location {
	return Location{Path: "/pm/sprint/history/diff", Params: map[string]string{
		"sprintID": sprintID,
		"from":     fromID,
		"to":       toID,
	}}
}

// LocReleaseList creates a location for the release list view.
var LocReleaseList = Location{Path: "/release/list"}

// LocReleaseDetail creates a location for a release detail view.
func LocReleaseDetail(releaseID string) Location {
	return Location{Path: "/release/detail", Params: map[string]string{"releaseID": releaseID}}
}

// LocReleaseNew creates a location for the new release form.
var LocReleaseNew = Location{Path: "/release/new"}

// LocReleaseEdit creates a location for editing a release.
func LocReleaseEdit(releaseID string) Location {
	return Location{Path: "/release/edit", Params: map[string]string{"releaseID": releaseID}}
}

// LocReleaseSBOM creates a location for the release SBOM view.
func LocReleaseSBOM(releaseID string) Location {
	return Location{Path: "/release/sbom", Params: map[string]string{"releaseID": releaseID}}
}

// LocReleaseHistory creates a location for a release's edit history.
func LocReleaseHistory(releaseID string) Location {
	return Location{Path: "/release/history", Params: map[string]string{"releaseID": releaseID}}
}

// LocReleaseHistoryDiff creates a location for a release-history version diff.
func LocReleaseHistoryDiff(releaseID, fromID, toID string) Location {
	return Location{Path: "/release/history/diff", Params: map[string]string{
		"releaseID": releaseID,
		"from":      fromID,
		"to":        toID,
	}}
}

// LocReviewPRs creates a location for the review PR list view.
var LocReviewPRs = Location{Path: "/review/prs"}

// LocForks creates a location for the forks management view.
var LocForks = Location{Path: "/config/forks"}

// LocSite creates a location for the site customization view.
var LocSite = Location{Path: "/config/site"}

// LocMemoList creates a location for the merged memo list view.
var LocMemoList = Location{Path: "/memo/list"}

// LocMemoProject creates a location for the project-tier memo list view.
var LocMemoProject = Location{Path: "/memo/project"}

// LocMemoInherited creates a location for the inherited (binding) memo list view.
var LocMemoInherited = Location{Path: "/memo/inherited"}

// LocMemoPersonal creates a location for the personal-tier memo list view.
var LocMemoPersonal = Location{Path: "/memo/personal"}

// LocMemoSession creates a location for the session-picker view (list of sessions).
var LocMemoSession = Location{Path: "/memo/session"}

// LocMemoSessionItems creates a location for a specific session's memos.
func LocMemoSessionItems(sessionID string) Location {
	return Location{Path: "/memo/session/items", Params: map[string]string{"sessionID": sessionID}}
}

// LocMemoInherits creates a location for the inherits-management view (the
// list of trusted memo source URLs, not their memos).
var LocMemoInherits = Location{Path: "/memo/inherits"}

// LocMemoDetail creates a location for a memo detail view.
func LocMemoDetail(memoID string) Location {
	return Location{Path: "/memo/detail", Params: map[string]string{"memoID": memoID}}
}

// LocMemoNew creates a location for the new memo form. An optional tier
// ("session" | "personal" | "project") seeds the form's default tier.
func LocMemoNew(tier string) Location {
	params := map[string]string{}
	if tier != "" {
		params["tier"] = tier
	}
	return Location{Path: "/memo/new", Params: params}
}

// LocMemoHistory creates a location for a memo's edit-history view.
func LocMemoHistory(memoID string) Location {
	return Location{Path: "/memo/history", Params: map[string]string{"memoID": memoID}}
}

// LocMemoHistoryDiff creates a location for a memo-history version diff.
func LocMemoHistoryDiff(memoID, fromID, toID string) Location {
	return Location{Path: "/memo/history/diff", Params: map[string]string{
		"memoID": memoID,
		"from":   fromID,
		"to":     toID,
	}}
}

// LocIdentity creates a location for the identity management view.
var LocIdentity = Location{Path: "/config/identity"}

// LocReviewPRDetail creates a location for a review PR detail view.
func LocReviewPRDetail(prID string) Location {
	return Location{Path: "/review/pr", Params: map[string]string{"prID": prID}}
}

// LocReviewNewPR creates a location for the new PR form.
var LocReviewNewPR = Location{Path: "/review/new-pr"}

// LocReviewEditPR creates a location for editing a PR.
func LocReviewEditPR(prID string) Location {
	return Location{Path: "/review/edit-pr", Params: map[string]string{"prID": prID}}
}

// LocReviewFeedback creates a location for the feedback form with optional pre-selected state.
func LocReviewFeedback(prID, state string) Location {
	return Location{Path: "/review/feedback", Params: map[string]string{"prID": prID, "state": state}}
}

// LocReviewPRHistory creates a location for a PR's edit history.
func LocReviewPRHistory(prID string) Location {
	return Location{Path: "/review/pr/history", Params: map[string]string{"prID": prID}}
}

// LocReviewPRHistoryDiff creates a location for a PR-history version diff.
func LocReviewPRHistoryDiff(prID, fromID, toID string) Location {
	return Location{Path: "/review/pr/history/diff", Params: map[string]string{
		"prID": prID,
		"from": fromID,
		"to":   toID,
	}}
}

// LocReviewInterdiff creates a location for the interdiff (range-diff) view.
func LocReviewInterdiff(prID string) Location {
	return Location{Path: "/review/pr/interdiff", Params: map[string]string{"prID": prID}}
}

// LocReviewDiff creates a location for the files changed diff view.
func LocReviewDiff(prID string) Location {
	return Location{Path: "/review/diff", Params: map[string]string{"prID": prID}}
}

// LocReviewDiffCommit creates a location for the diff of a single commit within a PR.
func LocReviewDiffCommit(prID, commit string) Location {
	return Location{Path: "/review/diff", Params: map[string]string{"prID": prID, "commit": commit}}
}

// LocReviewFeedbackInline creates a location for inline feedback from the diff view.
func LocReviewFeedbackInline(prID, file string, oldLine, newLine int, commit string) Location {
	return Location{Path: "/review/feedback", Params: map[string]string{
		"prID":    prID,
		"state":   "",
		"file":    file,
		"oldLine": fmt.Sprintf("%d", oldLine),
		"newLine": fmt.Sprintf("%d", newLine),
		"commit":  commit,
	}}
}

// configNavExtensions are the extensions whose config view has its own nav item.
var configNavExtensions = map[string]bool{"social": true, "pm": true, "release": true, "review": true, "memo": true}

// NavItemID derives the nav panel selection from location.
func (r *Router) NavItemID() string {
	path := r.location.Path
	// Special cases with dynamic nav IDs based on params
	switch path {
	case "/social/repository":
		if _, hasURL := r.location.Params["url"]; hasURL {
			return "social.timeline" // external repo from timeline
		}
		return "social.myrepo"
	case "/social/list", "/social/list/repos":
		if listID, ok := r.location.Params["listID"]; ok {
			return "social.lists." + listID
		}
		return "social.timeline"
	case "/config":
		if ext := r.location.Params["extension"]; configNavExtensions[ext] {
			return "config." + ext
		}
		return "config.core"
	}
	// Use registry for all other paths
	return GetNavItemIDForPath(path)
}

// Param returns a location parameter or empty string.
func (l Location) Param(key string) string {
	if l.Params == nil {
		return ""
	}
	return l.Params[key]
}

// Is checks if location matches a path.
func (l Location) Is(path string) bool {
	return l.Path == path
}

// HasPrefix checks if location path starts with prefix.
func (l Location) HasPrefix(prefix string) bool {
	return strings.HasPrefix(l.Path, prefix)
}
