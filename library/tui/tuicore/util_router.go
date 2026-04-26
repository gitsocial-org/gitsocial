// util_router.go - Navigation infrastructure: routing, locations, and registry
package tuicore

import (
	"fmt"
	"sort"
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

// LocPMIssuesRepo creates a location for PM issues in a specific repository.
func LocPMIssuesRepo(repoURL, branch string) Location {
	params := map[string]string{"url": repoURL}
	if branch != "" {
		params["branch"] = branch
	}
	return Location{Path: "/pm/issues", Params: params}
}

// LocPMIssueDetail creates a location for a PM issue detail view.
func LocPMIssueDetail(issueID string) Location {
	return Location{Path: "/pm/issue", Params: map[string]string{"issueID": issueID}}
}

// LocPMNewIssue creates a location for the new issue form.
var LocPMNewIssue = Location{Path: "/pm/new-issue"}

// LocPMEditIssue creates a location for editing an issue.
func LocPMEditIssue(issueID string) Location {
	return Location{Path: "/pm/edit-issue", Params: map[string]string{"issueID": issueID}}
}

// LocPMIssueHistory creates a location for an issue's edit history.
func LocPMIssueHistory(issueID string) Location {
	return Location{Path: "/pm/issue/history", Params: map[string]string{"issueID": issueID}}
}

// LocPMConfig creates a location for PM configuration.
var LocPMConfig = Location{Path: "/pm/config"}

// LocPMMilestones creates a location for the PM milestones list.
var LocPMMilestones = Location{Path: "/pm/milestones"}

// LocPMMilestonesRepo creates a location for PM milestones in a specific repository.
func LocPMMilestonesRepo(repoURL, branch string) Location {
	params := map[string]string{"url": repoURL}
	if branch != "" {
		params["branch"] = branch
	}
	return Location{Path: "/pm/milestones", Params: params}
}

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

// LocPMSprints creates a location for the PM sprints list.
var LocPMSprints = Location{Path: "/pm/sprints"}

// LocPMSprintsRepo creates a location for PM sprints in a specific repository.
func LocPMSprintsRepo(repoURL, branch string) Location {
	params := map[string]string{"url": repoURL}
	if branch != "" {
		params["branch"] = branch
	}
	return Location{Path: "/pm/sprints", Params: params}
}

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

// LocReviewPRs creates a location for the review PR list view.
var LocReviewPRs = Location{Path: "/review/prs"}

// LocForks creates a location for the forks management view.
var LocForks = Location{Path: "/config/forks"}

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
		ext := r.location.Params["extension"]
		switch ext {
		case "social":
			return "config.social"
		case "pm":
			return "config.pm"
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

// NavItem represents a navigation entry in the sidebar
type NavItem struct {
	ID      string // Unique identifier (e.g., "social", "social.timeline")
	Label   string // Display label
	Icon    string // Icon prefix (e.g., "")
	Parent  string // Parent ID ("" for top-level)
	Order   int    // Sort order within parent
	Enabled bool   // Whether item is implemented
}

// IsTopLevel returns true if this item has no parent.
func (n NavItem) IsTopLevel() bool {
	return n.Parent == ""
}

// NavRegistry manages navigation items with tree structure
type NavRegistry struct {
	items   []NavItem
	dynamic map[string][]NavItem // parentID -> dynamic children
	hidden  map[string]bool      // domain IDs hidden by user settings
	version int                  // incremented on mutations for cache invalidation
}

// NewNavRegistry creates a new navigation registry.
func NewNavRegistry() *NavRegistry {
	return &NavRegistry{
		items:   make([]NavItem, 0),
		dynamic: make(map[string][]NavItem),
		hidden:  make(map[string]bool),
	}
}

// SetHidden controls whether a top-level domain is hidden from navigation.
func (r *NavRegistry) SetHidden(domain string, hide bool) {
	if hide {
		r.hidden[domain] = true
	} else {
		delete(r.hidden, domain)
	}
	r.version++
}

// IsHidden returns true if a domain is hidden.
func (r *NavRegistry) IsHidden(domain string) bool {
	return r.hidden[domain]
}

// Register adds a static navigation item.
func (r *NavRegistry) Register(item NavItem) {
	r.items = append(r.items, item)
	r.version++
}

// RegisterDynamic replaces dynamic children under a parent for runtime items.
func (r *NavRegistry) RegisterDynamic(parentID string, items []NavItem) {
	r.dynamic[parentID] = items
	r.version++
}

// ClearDynamic removes all dynamic children under a parent.
func (r *NavRegistry) ClearDynamic(parentID string) {
	delete(r.dynamic, parentID)
	r.version++
}

// Version returns the current registry version for cache invalidation.
func (r *NavRegistry) Version() int {
	return r.version
}

// Get returns a navigation item by ID.
func (r *NavRegistry) Get(id string) *NavItem {
	for i := range r.items {
		if r.items[i].ID == id {
			return &r.items[i]
		}
	}
	for _, children := range r.dynamic {
		for i := range children {
			if children[i].ID == id {
				return &children[i]
			}
		}
	}
	return nil
}

// GetTopLevel returns all top-level items sorted by Order, excluding hidden domains.
func (r *NavRegistry) GetTopLevel() []NavItem {
	var result []NavItem
	for _, item := range r.items {
		if item.Parent == "" && !r.hidden[item.ID] {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

// GetChildren returns direct children of a parent, sorted by Order. Returns nil for hidden parents.
func (r *NavRegistry) GetChildren(parentID string) []NavItem {
	if r.hidden[parentID] {
		return nil
	}
	var result []NavItem
	// Static children
	for _, item := range r.items {
		if item.Parent == parentID {
			result = append(result, item)
		}
	}
	// Dynamic children
	if dyn, ok := r.dynamic[parentID]; ok {
		result = append(result, dyn...)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

// HasChildren returns true if the item has any children.
func (r *NavRegistry) HasChildren(id string) bool {
	if r.hidden[id] {
		return false
	}
	for _, item := range r.items {
		if item.Parent == id {
			return true
		}
	}
	if dyn, ok := r.dynamic[id]; ok && len(dyn) > 0 {
		return true
	}
	return false
}

// RegisterCoreNavItems registers the core navigation structure.
func RegisterCoreNavItems(r *NavRegistry) {
	// Unimplemented extensions (placeholders)
	// PM is registered by extensions/pm/nav.go
	// Release is registered by extensions/release/nav.go
	// Review is registered by extensions/review/nav.go
	r.Register(NavItem{ID: "cicd", Label: "CI/CD", Icon: "⚒", Order: 4, Enabled: false})
	r.Register(NavItem{ID: "infra", Label: "Infrastructure", Icon: "⛫", Order: 5, Enabled: false})
	r.Register(NavItem{ID: "ops", Label: "Operations", Icon: "⎈", Order: 6, Enabled: false})
	r.Register(NavItem{ID: "security", Label: "Security", Icon: "⛨", Order: 7, Enabled: false})
	r.Register(NavItem{ID: "portfolio", Label: "Portfolio", Icon: "⧉", Order: 8, Enabled: false})
	r.Register(NavItem{ID: "dm", Label: "DM", Icon: "✉", Order: 9, Enabled: false})

	// Config domain with sub-items
	r.Register(NavItem{ID: "config", Label: "Configuration", Icon: "⚙", Order: 10, Enabled: true})
	r.Register(NavItem{ID: "config.core", Label: "Core", Icon: "※", Parent: "config", Order: 0, Enabled: false})
	r.Register(NavItem{ID: "config.identity", Label: "Identity", Icon: "⚿", Parent: "config", Order: 1, Enabled: true})
	r.Register(NavItem{ID: "config.forks", Label: "Forks", Icon: "⑂", Parent: "config", Order: 2, Enabled: true})
	r.Register(NavItem{ID: "config.social", Label: "Social", Icon: "⌘", Parent: "config", Order: 3, Enabled: false})
	r.Register(NavItem{ID: "config.pm", Label: "PM", Icon: "▢", Parent: "config", Order: 4, Enabled: true})

	// Cache and Settings
	r.Register(NavItem{ID: "cache", Label: "Cache", Icon: "⛁", Order: 11, Enabled: true})
	r.Register(NavItem{ID: "settings", Label: "Settings", Icon: "⌨", Order: 12, Enabled: true})
}
