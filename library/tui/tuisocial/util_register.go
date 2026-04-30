// util_register.go - Social extension view and message handler registration
package tuisocial

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// SocialHost provides social-specific item operations using universal DisplayItem interface.
type SocialHost interface {
	DisplayItems() []tuicore.DisplayItem
	SetDisplayItems([]tuicore.DisplayItem)
	UpdateDisplayItem(tuicore.DisplayItem)
	RemoveDisplayItem(string)
}

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
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/social/history", Context: tuicore.History, Title: "History", Icon: "◉", Component: "VersionPicker"})

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
				if p, ok := ItemToPost(item); ok {
					return p, true
				}
			}
			return social.Post{}, false
		}
	}
	return PostToCardWithOptions(post, postResolver, PostToCardOptions{
		UserEmail: post.Display.UserEmail,
		ShowEmail: post.Display.ShowEmail,
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
	timeline := NewTimelineView(state.Workdir, state.UserEmail, state.ShowEmailOnCards)
	post := NewPostView(state.Workdir)
	post.SetUserEmail(state.UserEmail)
	showEmailFn := func() bool { return state.ShowEmailOnCards }
	search := tuicore.NewSearchView(
		state.Workdir,
		MakeSearchFunc(state.UserEmail, showEmailFn),
		MakeResolveItemFunc(state.UserEmail),
	)
	notifications := tuicore.NewNotificationsView(
		state.Workdir,
		MakeGetNotificationsFunc(state.UserEmail, showEmailFn),
		MakeMarkReadFunc(),
		MakeMarkUnreadFunc(),
		MakeResolveItemFunc(state.UserEmail),
		tuicore.WithBulkMarkFuncs(MakeMarkAllReadFunc(), MakeMarkAllUnreadFunc()),
	)
	repository := NewRepositoryView(state.Workdir)
	repository.SetUserEmail(state.UserEmail)
	listPicker := NewListPickerView(state.Workdir)
	listPosts := NewListPostsView(state.Workdir)
	listPosts.SetUserEmail(state.UserEmail)
	listPosts.SetShowEmail(state.ShowEmailOnCards)
	listRepos := NewListReposView(state.Workdir)
	history := NewHistoryView(state.Workdir)
	repoLists := NewRepoListsView(state.Workdir)
	host.AddView("/social/timeline", timeline)
	host.AddView("/social/detail", post)
	host.AddView("/search", search)
	host.AddView("/search/help", tuicore.NewSearchHelpView())
	host.AddView("/social/repository", repository)
	host.AddView("/notifications", notifications)
	host.AddView("/lists", listPicker)
	host.AddView("/social/list", listPosts)
	host.AddView("/social/list/repos", listRepos)
	host.AddView("/social/history", history)
	host.AddView("/social/repository/lists", repoLists)
}

// Message handlers

func handleSocialMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case ListsLoadedMsg:
		return handleListsLoaded(msg, ctx)
	case tuicore.NotificationsLoadedMsg:
		return handleNotificationsLoaded(msg, ctx)
	case tuicore.NotificationMarkedReadMsg:
		return handleNotificationMarkedRead(msg, ctx)
	case tuicore.NotificationMarkedUnreadMsg:
		return handleNotificationMarkedUnread(msg, ctx)
	case tuicore.NotificationsAllMarkedReadMsg:
		return handleNotificationsAllMarkedRead(msg, ctx)
	case tuicore.NotificationsAllMarkedUnreadMsg:
		return handleNotificationsAllMarkedUnread(msg, ctx)
	case FetchCompletedMsg:
		return handleFetchCompleted(msg, ctx)
	case PushCompletedMsg:
		return handlePushCompleted(msg, ctx)
	case TimelineLoadedMsg:
		return handleTimelineLoaded(msg, ctx)
	case PostCreatedMsg:
		return handlePostCreated(msg, ctx)
	case CommentCreatedMsg:
		return handleCommentCreated(msg, ctx)
	case RepostCreatedMsg:
		return handleRepostCreated(msg, ctx)
	case PostEditedMsg:
		return handlePostEdited(msg, ctx)
	case RetractStartedMsg:
		return handleRetractStarted(msg, ctx)
	case PostRetractedMsg:
		return handlePostRetracted(msg, ctx)
	case RepoAddedMsg:
		return handleRepoAdded(msg, ctx)
	case RepoFetchedAfterAddMsg:
		return handleRepoFetchedAfterAdd(msg, ctx)
	case RepoRemovedMsg:
		return handleRepoRemoved(msg, ctx)
	case ListCreatedMsg:
		return handleListCreated(msg, ctx)
	case tuicore.InteractionCountsRefreshedMsg:
		return handleInteractionCountsRefreshed(msg, ctx)
	}
	return false, nil
}

func handleListsLoaded(msg ListsLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil && msg.Lists != nil {
		social.UpdateListItems(ctx.Nav().Registry(), msg.Lists)
	}
	return true, ctx.Host().Update(msg)
}

func handleNotificationsLoaded(msg tuicore.NotificationsLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
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

func handleNotificationMarkedRead(msg tuicore.NotificationMarkedReadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil {
		ctx.Nav().SetUnreadCount(msg.UnreadCount)
	}
	return true, ctx.Host().Update(msg)
}

func handleNotificationMarkedUnread(msg tuicore.NotificationMarkedUnreadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil {
		ctx.Nav().SetUnreadCount(msg.UnreadCount)
	}
	return true, ctx.Host().Update(msg)
}

func handleNotificationsAllMarkedRead(msg tuicore.NotificationsAllMarkedReadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Nav().SetUnreadCount(0)
	return true, ctx.Host().Update(msg)
}

func handleNotificationsAllMarkedUnread(msg tuicore.NotificationsAllMarkedUnreadMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Nav().SetUnreadCount(msg.UnreadCount)
	return true, ctx.Host().Update(msg)
}

func handleFetchCompleted(msg FetchCompletedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.SetFetching(false)
	if msg.Err != nil {
		errMsg := fmt.Sprintf("Fetch failed: %s", msg.Err)
		ctx.Host().SetMessage(errMsg, tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, errMsg, "fetch")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	ctx.Host().SetFetchStatus(time.Now(), msg.Stats.Posts)
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
	} else if msg.Stats.Posts > 0 {
		msgCmd = ctx.Host().SetMessageWithTimeout(fmt.Sprintf("Fetched %d new posts", msg.Stats.Posts), tuicore.MessageTypeSuccess, 5*time.Second)
	} else {
		msgCmd = ctx.Host().SetMessageWithTimeout("Already up to date", tuicore.MessageTypeSuccess, 5*time.Second)
	}
	return true, tea.Batch(ctx.Host().RefreshView(), ctx.LoadUnreadCount(), msgCmd)
}

func handlePushCompleted(msg PushCompletedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.SetPushing(false)
	if msg.Err != nil {
		errMsg := fmt.Sprintf("Push failed: %s", msg.Err)
		ctx.Host().SetMessage(errMsg, tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, errMsg, "push")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	var msgCmd tea.Cmd
	if msg.Commits == 0 && msg.Refs == 0 {
		msgCmd = ctx.Host().SetMessageWithTimeout("Nothing to push", tuicore.MessageTypeSuccess, 5*time.Second)
	} else {
		msgCmd = ctx.Host().SetMessageWithTimeout(fmt.Sprintf("Pushed %d commits, %d refs", msg.Commits, msg.Refs), tuicore.MessageTypeSuccess, 5*time.Second)
	}
	return true, tea.Batch(ctx.Host().RefreshView(), ctx.LoadUnpushedCount(), msgCmd)
}

func handleTimelineLoaded(msg TimelineLoadedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == nil && !msg.Append {
		if sh, ok := ctx.Host().(SocialHost); ok {
			state := ctx.Host().State()
			items := PostsToItems(msg.Posts, state.UserEmail, state.ShowEmailOnCards)
			sh.SetDisplayItems(items)
		}
	}
	return true, ctx.Host().Update(msg)
}

func handlePostCreated(msg PostCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if sh, ok := ctx.Host().(SocialHost); ok {
		state := ctx.Host().State()
		post := msg.Post
		post.Display.UserEmail = state.UserEmail
		post.Display.ShowEmail = state.ShowEmailOnCards
		newItem := tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post)
		items := append([]tuicore.DisplayItem{newItem}, sh.DisplayItems()...)
		sh.SetDisplayItems(items)
	}
	return true, nil
}

func handleCommentCreated(msg CommentCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if sh, ok := ctx.Host().(SocialHost); ok {
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

func handleRepostCreated(msg RepostCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if sh, ok := ctx.Host().(SocialHost); ok {
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
	return true, tea.Batch(cmds...)
}

func handlePostEdited(msg PostEditedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Host().SetSaving(false)
	if sh, ok := ctx.Host().(SocialHost); ok {
		canonicalID := msg.Post.EditOf
		if canonicalID == "" {
			canonicalID = msg.Post.ID
		}
		result := social.GetPosts(ctx.Workdir(), "post:"+canonicalID, nil)
		if result.Success && len(result.Data) > 0 {
			state := ctx.Host().State()
			post := result.Data[0]
			post.Display.UserEmail = state.UserEmail
			post.Display.ShowEmail = state.ShowEmailOnCards
			updatedItem := tuicore.NewItem(post.ID, "social", string(post.Type), post.Timestamp, post)
			sh.UpdateDisplayItem(updatedItem)
		}
	}
	return true, ctx.Host().RefreshView()
}

func handleRetractStarted(_ RetractStartedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Host().SetRetracting(true)
	return true, nil
}

func handlePostRetracted(msg PostRetractedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	ctx.Host().SetRetracting(false)
	if msg.Err == nil {
		if sh, ok := ctx.Host().(SocialHost); ok {
			sh.RemoveDisplayItem(msg.PostID)
		}
	}
	return true, nil
}

func handleRepoAdded(msg RepoAddedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		if msg.Err.Error() == "Repository already in list" {
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

func handleRepoRemoved(msg RepoRemovedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	var msgCmd tea.Cmd
	if msg.Err == nil {
		msgCmd = ctx.Host().SetMessageWithTimeout(fmt.Sprintf("Removed %s from list", protocol.GetDisplayName(msg.RepoURL)), tuicore.MessageTypeSuccess, 5*time.Second)
	}
	cmd := ctx.Host().Update(msg)
	return true, tea.Batch(cmd, msgCmd)
}

func handleListCreated(msg ListCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
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

func handleInteractionCountsRefreshed(msg tuicore.InteractionCountsRefreshedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		return true, nil
	}
	// Forward to views to update their display
	return true, ctx.Host().Update(msg)
}

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
