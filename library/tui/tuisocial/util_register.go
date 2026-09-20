// util_register.go - Social extension view and message handler registration
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuiviews"
)

// socialHost provides social-specific item operations using universal DisplayItem interface.
type socialHost interface {
	DisplayItems() []tuicore.DisplayItem
	SetDisplayItems([]tuicore.DisplayItem)
	UpdateDisplayItem(tuicore.DisplayItem)
	RemoveDisplayItem(string)
}

// init registers the social views, card renderers, dimmed checkers, nav targets and message handler.
func init() {
	// ViewMeta (order = doc output order within social domain)
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/timeline", Context: tuicore.Timeline, Title: "Timeline", Icon: "⏱", NavItemID: "social.timeline", ShowFetch: true, Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/search", Context: tuicore.Search, Title: "Search", Icon: "⌕", NavItemID: "_search", ShowFetch: true})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/search/help", Context: tuicore.SearchHelp, Title: "Search Help", Icon: "?"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/notifications", Context: tuicore.Notifications, Title: "Notifications", Icon: "⚑", NavItemID: "_notifications", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/detail", Context: tuicore.Detail, Title: "Post Detail", Icon: "◉"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/thread", Context: tuicore.Thread, Title: "Thread", Icon: "◉"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/repository", Context: tuicore.Repository, Title: "Repository", Icon: "◉", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/my-repository", Context: tuicore.MyRepository, Title: "My Repository", Icon: "◉", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/lists", Context: tuicore.ListPicker, Title: "List Picker", Icon: "☷", NavItemID: "social.lists"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/list", Context: tuicore.ListPosts, Title: "List Posts", Icon: "☷", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/list/repos", Context: tuicore.ListRepos, Title: "List Repos", Icon: "☷"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/repository/lists", Context: tuicore.RepoLists, Title: "Repository Lists", Icon: "☷"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/explore", Context: tuicore.Explore, Title: "Explore", Icon: "➼", NavItemID: "social.explore"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/followers", Context: tuicore.Explore, Title: "My Followers", Icon: "㋡", NavItemID: "social.followers"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/history", Context: tuicore.History, Title: "History", Icon: "◉", Component: "VersionPicker"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/history/diff", Context: tuicore.HistoryDiff, Title: "Post Diff", Icon: "◉"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/post-form", Context: tuicore.Detail, Title: "Post", Icon: "•"})

	tuicore.RegisterMessageHandler(handleSocialMessages)
	// Register nav targets for social types (wildcard covers post, comment, repost, quote)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "social", Type: "*"},
		func(id string) tuicore.Location { return tuicore.LocDetail(id) },
	)
	// Follow notifications navigate to the actor's repository (handled separately in notifications view)

	// Register card renderers for social types
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "social", Type: "*"},
		socialItemToCard,
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "social", Type: "follow"},
		followNotificationToCard,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "social", Type: "*"},
		socialIsDimmed,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "social", Type: "follow"},
		followIsDimmed,
	)

}

// socialItemToCard renders any social item (post, comment, repost, quote) to a Card.
func socialItemToCard(data any, resolver tuicore.ItemResolver) tuicore.Card {
	post, ok := data.(social.Post)
	if !ok {
		return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid social item"}}
	}
	var postResolver PostResolver
	if resolver != nil {
		postResolver = func(id string) (social.Post, bool) {
			if item, ok := resolver(id); ok {
				if p, ok := itemToPost(item); ok {
					return p, true
				}
			}
			return social.Post{}, false
		}
	}
	return PostToCardWithOptions(post, postResolver, PostToCardOptions{
		UserEmail: post.Display.UserEmail,
		ShowEmail: post.Display.ShowEmail,
		Workdir:   post.Display.Workdir,
	})
}

// socialIsDimmed checks if a social item should be dimmed.
func socialIsDimmed(data any) bool {
	post, ok := data.(social.Post)
	if !ok {
		return false
	}
	return post.Display.IsUnpushed || post.Display.IsNotificationRead
}

// followNotificationToCard renders a follow notification to a Card.
func followNotificationToCard(data any, _ tuicore.ItemResolver) tuicore.Card {
	n, ok := data.(social.Notification)
	if !ok {
		return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid notification"}}
	}
	repoLoc := tuicore.LocRepository(n.ActorRepo, "")
	return tuicore.Card{
		Header: tuicore.CardHeader{
			Title:     n.Actor.Name,
			TitleLink: &repoLoc,
			Subtitle: []tuicore.HeaderPart{
				{Text: tuicore.FormatTime(n.Timestamp)},
				{Text: n.ActorRepo, Link: &repoLoc},
			},
			Badge: "followed you",
			Icon:  "•",
		},
		Content: tuicore.CardContent{
			Text: n.ActorRepo,
		},
	}
}

// followIsDimmed checks if a follow notification should be dimmed.
func followIsDimmed(data any) bool {
	n, ok := data.(social.Notification)
	if !ok {
		return false
	}
	return n.IsRead
}

// Register registers all social views with the host.
func Register(host tuicore.ViewHost) {
	state := host.State()
	timeline := newTimelineView(state.Workdir, state.UserEmail, state.ShowEmailOnCards)
	post := newPostView(state.Workdir)
	post.setUserEmail(state.UserEmail)
	showEmailFn := func() bool { return state.ShowEmailOnCards }
	search := tuiviews.NewSearchView(
		state.Workdir,
		makeSearchFunc(state.UserEmail, showEmailFn),
		makeResolveItemFunc(state.UserEmail),
	)
	notifications := tuiviews.NewNotificationsView(
		state.Workdir,
		makeGetNotificationsFunc(state.UserEmail, showEmailFn),
		makeMarkReadFunc(),
		makeMarkUnreadFunc(),
		makeResolveItemFunc(state.UserEmail),
		tuiviews.WithBulkMarkFuncs(makeMarkAllReadFunc(), makeMarkAllUnreadFunc()),
	)
	repository := newRepositoryView(state.Workdir)
	repository.setUserEmail(state.UserEmail)
	listPicker := newListPickerView(state.Workdir)
	listPosts := newListPostsView(state.Workdir)
	listPosts.setUserEmail(state.UserEmail)
	listPosts.setShowEmail(state.ShowEmailOnCards)
	listRepos := newListReposView(state.Workdir)
	history := newHistoryView(state.Workdir)
	historyDiff := newPostHistoryDiffView(state.Workdir)
	repoLists := newRepoListsView(state.Workdir)
	host.AddView("/social/timeline", timeline)
	host.AddView("/social/detail", post)
	host.AddView("/search", search)
	host.AddView("/search/help", tuiviews.NewSearchHelpView())
	host.AddView("/social/repository", repository)
	host.AddView("/notifications", notifications)
	host.AddView("/lists", listPicker)
	host.AddView("/social/list", listPosts)
	host.AddView("/social/list/repos", listRepos)
	host.AddView("/social/history", history)
	host.AddView("/social/history/diff", historyDiff)
	host.AddView("/social/repository/lists", repoLists)
	host.AddView("/social/post-form", newPostFormView(state.Workdir))
	explore := newExploreView(state.Workdir)
	host.AddView("/social/explore", explore)
	host.AddView("/social/followers", explore)
}

// Message handlers

func handleSocialMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case postSubmittedMsg:
		return handlePostSubmitted(msg, ctx)
	case ListsLoadedMsg:
		return handleListsLoaded(msg, ctx)
	case tuiviews.NotificationsLoadedMsg:
		return handleNotificationsLoaded(msg, ctx)
	case tuiviews.NotificationMarkedReadMsg:
		return handleNotificationMarkedRead(msg, ctx)
	case tuiviews.NotificationMarkedUnreadMsg:
		return handleNotificationMarkedUnread(msg, ctx)
	case tuiviews.NotificationsAllMarkedReadMsg:
		return handleNotificationsAllMarkedRead(msg, ctx)
	case tuiviews.NotificationsAllMarkedUnreadMsg:
		return handleNotificationsAllMarkedUnread(msg, ctx)
	case FetchCompletedMsg:
		return handleFetchCompleted(msg, ctx)
	case PushCompletedMsg:
		return handlePushCompleted(msg, ctx)
	case TimelineLoadedMsg:
		return handleTimelineLoaded(msg, ctx)
	case commentCreatedMsg:
		return handleCommentCreated(msg, ctx)
	case retractStartedMsg:
		return handleRetractStarted(msg, ctx)
	case postRetractedMsg:
		return handlePostRetracted(msg, ctx)
	case repoAddedMsg:
		return handleRepoAdded(msg, ctx)
	case RepoFetchedAfterAddMsg:
		return handleRepoFetchedAfterAdd(msg, ctx)
	case repoRemovedMsg:
		return handleRepoRemoved(msg, ctx)
	case listCreatedMsg:
		return handleListCreated(msg, ctx)
	case tuicore.InteractionCountsRefreshedMsg:
		return handleInteractionCountsRefreshed(msg, ctx)
	}
	return false, nil
}

// handlePostSubmitted navigates back to the source on success; keeps the form
// mounted on error so the user can fix input and resubmit. Comment mode pops
// back to the source view (which may be a non-social detail view like a
// release or issue) so the new comment appears in context without assuming
// the target ID resolves as a social post.
func handlePostSubmitted(msg postSubmittedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		// Pass through so the form clears its submitting state and the user can retry
		return false, nil
	}
	verb := "Posted"
	switch msg.Mode {
	case postFormComment:
		verb = "Commented"
	case postFormQuote:
		verb = "Quoted"
	case postFormEdit:
		verb = "Edited"
	}
	statusCmd := ctx.Host().SetMessageWithTimeout(verb, tuicore.MessageTypeSuccess, 5*time.Second)
	if msg.Mode == postFormComment {
		return true, tea.Batch(statusCmd, func() tea.Msg {
			return tuicore.NavigateMsg{Action: tuicore.NavBack}
		})
	}
	// For quote/edit land on the parent post; for new posts land on the
	// freshly-created post.
	target := msg.Post.ID
	if msg.Mode != postFormNew && msg.TargetID != "" {
		target = msg.TargetID
	}
	return true, tea.Batch(statusCmd, func() tea.Msg {
		return tuicore.NavigateMsg{Location: tuicore.LocDetail(target), Action: tuicore.NavReplace}
	})
}

// handleListsLoaded refreshes the list entries in the nav panel and passes the message on.
func handleListsLoaded(msg ListsLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil && msg.Lists != nil {
		social.UpdateListItems(ctx.Nav().Registry(), msg.Lists)
	}
	return true, ctx.Host().Update(msg)
}

// handleNotificationsLoaded recounts the unread badge from the loaded notifications.
func handleNotificationsLoaded(msg tuiviews.NotificationsLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil {
		unread := 0
		for _, n := range msg.Result.Meta {
			if !n.IsRead {
				unread++
			}
		}
		ctx.Nav().SetUnreadCount(unread)
	}
	return true, ctx.Host().Update(msg)
}

// handleNotificationMarkedRead updates the unread badge after one notification is read.
func handleNotificationMarkedRead(msg tuiviews.NotificationMarkedReadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil {
		ctx.Nav().SetUnreadCount(msg.UnreadCount)
	}
	return true, ctx.Host().Update(msg)
}

// handleNotificationMarkedUnread updates the unread badge after one notification is unread again.
func handleNotificationMarkedUnread(msg tuiviews.NotificationMarkedUnreadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil {
		ctx.Nav().SetUnreadCount(msg.UnreadCount)
	}
	return true, ctx.Host().Update(msg)
}

// handleNotificationsAllMarkedRead clears the unread badge.
func handleNotificationsAllMarkedRead(msg tuiviews.NotificationsAllMarkedReadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Nav().SetUnreadCount(0)
	return true, ctx.Host().Update(msg)
}

// handleNotificationsAllMarkedUnread restores the unread badge to the full count.
func handleNotificationsAllMarkedUnread(msg tuiviews.NotificationsAllMarkedUnreadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Nav().SetUnreadCount(msg.UnreadCount)
	return true, ctx.Host().Update(msg)
}

// handleFetchCompleted reports the fetch outcome, adjusts the auto-fetch back-off and refreshes the view.
func handleFetchCompleted(msg FetchCompletedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.SetFetching(false)
	if msg.Err != nil {
		errMsg := fmt.Sprintf("Fetch failed: %s", msg.Err)
		ctx.Host().SetMessage(errMsg, tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, errMsg, "fetch")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	ctx.Host().SetFetchStatus(time.Now(), msg.Stats.Items)
	if msg.Auto {
		// Drive auto-fetch back-off: reset when this cycle brought something,
		// otherwise lengthen the idle streak (capped — beyond this the interval
		// is already pinned at its ceiling).
		st := ctx.Host().State()
		switch {
		case msg.Stats.Items > 0:
			st.AutoFetchIdleStreak = 0
		case st.AutoFetchIdleStreak < 20:
			st.AutoFetchIdleStreak++
		}
	}
	if cacheStats, err := cache.GetStats(ctx.CacheDir()); err == nil {
		ctx.Nav().SetCacheSize(cache.FormatBytes(cacheStats.TotalBytes))
	}
	// Bump IdentityGeneration so card lists invalidate cached verified badges —
	// the verifier resolves new bindings as part of fetch.
	ctx.Host().State().IdentityGeneration++
	var msgCmd tea.Cmd
	if errCount := len(msg.Stats.Errors); errCount > 0 {
		warnMsg := fmt.Sprintf("Fetch complete (%d errors)", errCount)
		ctx.Host().SetMessage(warnMsg, tuicore.MessageTypeWarning)
		for _, fetchErr := range msg.Stats.Errors {
			ctx.Host().State().AddLogEntry(tuicore.LogSeverityWarn, fetchErr.Repository+": "+fetchErr.Error, "fetch")
		}
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
	} else if summary := formatFetchSummary(msg.Breakdown); summary != "" {
		msgCmd = ctx.Host().SetMessageWithTimeout(summary, tuicore.MessageTypeSuccess, 5*time.Second)
	} else if !msg.Auto {
		// Auto-fetch stays silent when there's nothing new — no toast spam.
		msgCmd = ctx.Host().SetMessageWithTimeout("Already up to date", tuicore.MessageTypeSuccess, 5*time.Second)
	}
	return true, tea.Batch(ctx.Host().RefreshView(), ctx.LoadUnreadCount(), msgCmd)
}

// fetchExtLabels maps extension keys to friendly plural labels for the fetch
// summary, in display order.
var fetchExtLabels = []struct{ key, label string }{
	{"social", "discussions"},
	{"review", "PRs"},
	{"pm", "PM"},
	{"release", "releases"},
	{"memo", "memos"},
	{"code", "commits"},
}

// formatFetchSummary turns a per-extension breakdown into a toast like
// "Fetched 3 discussions, 2 PRs". Returns "" when nothing was fetched.
func formatFetchSummary(breakdown map[string]int) string {
	var parts []string
	for _, e := range fetchExtLabels {
		if n := breakdown[e.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, e.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Fetched " + strings.Join(parts, ", ")
}

// handlePushCompleted reports the push outcome and refreshes the view and unpushed count.
func handlePushCompleted(msg PushCompletedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.SetPushing(false)
	if msg.Err != nil {
		errMsg := fmt.Sprintf("Push failed: %s", msg.Err)
		ctx.Host().SetMessage(errMsg, tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, errMsg, "push")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	text, tone := formatPushCompletion(msg)
	msgCmd := ctx.Host().SetMessageWithTimeout(text, tone, 5*time.Second)
	return true, tea.Batch(ctx.Host().RefreshView(), ctx.LoadUnpushedCount(), msgCmd)
}

// formatPushCompletion builds the completion toast, one line per remote.
func formatPushCompletion(msg PushCompletedMsg) (string, tuicore.MessageType) {
	tone := tuicore.MessageTypeSuccess
	lines := make([]string, 0, len(msg.Results))
	for _, res := range msg.Results {
		line, siteFailed := formatPushRemoteResult(res)
		if siteFailed {
			tone = tuicore.MessageTypeWarning
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "Nothing to push", tone
	}
	return strings.Join(lines, "; "), tone
}

// formatPushRemoteResult renders one remote's outcome and reports whether its site failed.
func formatPushRemoteResult(res client.Result) (string, bool) {
	p := res.Push
	commits, refs, tags := 0, 0, 0
	remote := ""
	if p != nil {
		commits, refs, tags, remote = p.Commits+p.CodeCommits, p.Refs, p.Tags, p.Remote
	}
	head := "Nothing to push"
	if commits > 0 || refs > 0 || tags > 0 {
		parts := []string{fmt.Sprintf("%d commits", commits), fmt.Sprintf("%d refs", refs)}
		if tags > 0 {
			parts = append(parts, fmt.Sprintf("%d tags", tags))
		}
		head = "Pushed " + strings.Join(parts, ", ")
	}
	if remote != "" {
		head += " to " + remote
	}
	switch {
	case res.Site.Err != nil:
		return head + " · site failed: " + res.Site.Err.Error(), true
	case res.Site.Published:
		return head + " · site published", false
	case res.Site.Skipped != "":
		return head + " · site skipped", false
	}
	return head, false
}

// handleTimelineLoaded replaces the host's display items with the loaded posts.
func handleTimelineLoaded(msg TimelineLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil && !msg.Append {
		if sh, ok := ctx.Host().(socialHost); ok {
			state := ctx.Host().State()
			items := postsToItems(msg.Posts, state.UserEmail, state.ShowEmailOnCards, state.Workdir)
			sh.SetDisplayItems(items)
		}
	}
	return true, ctx.Host().Update(msg)
}

// handleCommentCreated puts the new comment at the top of the list, opens it and recounts its targets.
func handleCommentCreated(msg commentCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if sh, ok := ctx.Host().(socialHost); ok {
		state := ctx.Host().State()
		post := msg.Post
		post.Display.UserEmail = state.UserEmail
		post.Display.ShowEmail = state.ShowEmailOnCards
		newItem := tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post)
		items := append([]tuicore.DisplayItem{newItem}, sh.DisplayItems()...)
		sh.SetDisplayItems(items)
	}
	cmds := []tea.Cmd{
		func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocDetail(msg.Post.ID),
				Action:   tuicore.NavPush,
			}
		},
	}
	if msg.Post.OriginalPostID != "" {
		cmds = append(cmds, refreshInteractionCounts(ctx.Workdir(), msg.Post.OriginalPostID))
	}
	if msg.Post.ParentCommentID != "" && msg.Post.ParentCommentID != msg.Post.OriginalPostID {
		cmds = append(cmds, refreshInteractionCounts(ctx.Workdir(), msg.Post.ParentCommentID))
	}
	return true, tea.Batch(cmds...)
}

// handleRetractStarted marks the host busy for the duration of the retraction.
func handleRetractStarted(_ retractStartedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Host().SetRetracting(true)
	return true, nil
}

// handlePostRetracted clears the busy marker and drops the retracted post from the list.
func handlePostRetracted(msg postRetractedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Host().SetRetracting(false)
	if msg.Err == nil {
		if sh, ok := ctx.Host().(socialHost); ok {
			sh.RemoveDisplayItem(msg.PostID)
		}
	}
	return true, nil
}

// handleRepoAdded reports the addition and starts the first fetch of the new repository.
func handleRepoAdded(msg repoAddedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		if strings.HasPrefix(msg.Err.Error(), "repository already in the list") {
			msgCmd := ctx.Host().SetMessageWithTimeout("Already in "+msg.ListName+": "+msg.RepoURL, tuicore.MessageTypeWarning, 5*time.Second)
			cmd := ctx.Host().Update(msg)
			return true, tea.Batch(cmd, msgCmd)
		}
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	ctx.Host().SetMessage("Fetching "+msg.RepoURL+"...", tuicore.MessageTypeNone)
	cmd := ctx.Host().Update(msg)
	fetchCmd := ctx.FetchRepo(msg.RepoURL)
	return true, tea.Batch(cmd, fetchCmd)
}

// handleRepoFetchedAfterAdd reports how much the first fetch brought in and reloads the lists and timeline.
func handleRepoFetchedAfterAdd(msg RepoFetchedAfterAddMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	var msgCmd tea.Cmd
	if msg.Err != nil {
		errMsg := "Fetch failed: " + msg.Err.Error()
		msgCmd = ctx.Host().SetMessageWithTimeout(errMsg, tuicore.MessageTypeError, 5*time.Second)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, errMsg, "fetch")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
	} else {
		msgCmd = ctx.Host().SetMessageWithTimeout(fmt.Sprintf("Fetched %d posts from %s", msg.Posts, protocol.GetDisplayName(msg.RepoURL)), tuicore.MessageTypeSuccess, 5*time.Second)
	}
	return true, tea.Batch(msgCmd, ctx.LoadLists(), ctx.RefreshTimeline(), ctx.LoadUnreadCount(), ctx.RefreshCacheSize())
}

// handleRepoRemoved reports the removal and passes the message to the view.
func handleRepoRemoved(msg repoRemovedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	var msgCmd tea.Cmd
	if msg.Err == nil {
		msgCmd = ctx.Host().SetMessageWithTimeout(fmt.Sprintf("Removed %s from list", protocol.GetDisplayName(msg.RepoURL)), tuicore.MessageTypeSuccess, 5*time.Second)
	}
	cmd := ctx.Host().Update(msg)
	return true, tea.Batch(cmd, msgCmd)
}

// handleListCreated reports the new list and reloads the list entries.
func handleListCreated(msg listCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, ctx.Host().Update(msg)
	}
	name := msg.List.Name
	if name == "" {
		name = msg.List.ID
	}
	msgCmd := ctx.Host().SetMessageWithTimeout("Created list: "+name+" ("+msg.List.ID+")", tuicore.MessageTypeSuccess, 5*time.Second)
	cmd := ctx.Host().Update(msg)
	return true, tea.Batch(cmd, msgCmd, ctx.LoadLists())
}

// handleInteractionCountsRefreshed forwards fresh counts to the views that display them.
func handleInteractionCountsRefreshed(msg tuicore.InteractionCountsRefreshedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		return true, nil // a background recount failure stays quiet; the counts refresh again on the next write
	}
	// Forward to views to update their display
	return true, ctx.Host().Update(msg)
}

// refreshInteractionCounts recounts a post's comments, reposts and quotes in the background.
func refreshInteractionCounts(workdir, postID string) tea.Cmd {
	return func() tea.Msg {
		parsed := protocol.ParseRef(postID)
		if parsed.Value == "" {
			return tuicore.InteractionCountsRefreshedMsg{
				PostID: postID,
				Err:    fmt.Errorf("invalid ref: %s", postID),
			}
		}
		branch := parsed.Branch
		if branch == "" {
			branch = gitmsg.GetExtBranch(workdir, "social")
		}
		counts, err := social.RefreshInteractionCounts(parsed.Repository, parsed.Value, branch)
		return tuicore.InteractionCountsRefreshedMsg{
			PostID:   postID,
			Comments: counts.Comments,
			Reposts:  counts.Reposts,
			Quotes:   counts.Quotes,
			Err:      err,
		}
	}
}
