// util_register.go - Review extension view and message handler registration
package tuireview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func init() {
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/prs", Context: tuicore.ReviewPRs, Title: "Pull Requests", Icon: "⑂", NavItemID: "review.prs", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/pr", Context: tuicore.ReviewPRDetail, Title: "PR Detail", Icon: "⑂", NavItemID: "review.prs", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/new-pr", Context: tuicore.ReviewPRDetail, Title: "New Pull Request", Icon: "⑂", NavItemID: "review.prs", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/edit-pr", Context: tuicore.ReviewPRDetail, Title: "Edit Pull Request", Icon: "⑂", NavItemID: "review.prs", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/feedback", Context: tuicore.ReviewPRDetail, Title: "Feedback", Icon: "⑂", NavItemID: "review.prs", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/pr/history", Context: tuicore.ReviewPRHistory, Title: "PR History", Icon: "⑂", NavItemID: "review.prs", Component: "VersionPicker"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/diff", Context: tuicore.ReviewDiff, Title: "Files Changed", Icon: "⑂", NavItemID: "review.prs"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/review/pr/interdiff", Context: tuicore.ReviewInterdiff, Title: "Interdiff", Icon: "⑂", NavItemID: "review.prs"})
	tuicore.RegisterMessageHandler(handleReviewMessages)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "review", Type: "pull-request"},
		func(id string) tuicore.Location { return tuicore.LocReviewPRDetail(id) },
	)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "review", Type: "feedback"},
		func(id string) tuicore.Location {
			item, err := review.GetReviewItemByRef(id, "")
			if err != nil || !item.PullRequestHash.Valid {
				return tuicore.LocDetail(id)
			}
			prRef := protocol.CreateRef(protocol.RefTypeCommit,
				item.PullRequestHash.String, item.PullRequestRepoURL.String, item.PullRequestBranch.String)
			loc := tuicore.LocReviewPRDetail(prRef)
			loc.Params["focusID"] = id
			return loc
		},
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "review", Type: "pull-request"},
		prCardRenderer,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "review", Type: "pull-request"},
		prDimmedChecker,
	)
	// Review notification card renderers
	for _, t := range []string{"fork-pr", "feedback", "approved", "changes-requested", "review-requested", "pr-merged", "pr-closed", "pr-ready"} {
		tuicore.RegisterCardRenderer(
			tuicore.ItemType{Extension: "review", Type: t},
			reviewNotificationCardRenderer,
		)
	}
	// Review notification nav targets
	for _, t := range []string{"fork-pr", "review-requested"} {
		tuicore.RegisterNavTarget(
			tuicore.ItemType{Extension: "review", Type: t},
			func(id string) tuicore.Location { return tuicore.LocReviewPRDetail(id) },
		)
	}
	for _, t := range []string{"feedback", "approved", "changes-requested"} {
		tuicore.RegisterNavTarget(
			tuicore.ItemType{Extension: "review", Type: t},
			feedbackNavTarget,
		)
	}
	for _, t := range []string{"pr-merged", "pr-closed", "pr-ready"} {
		tuicore.RegisterNavTarget(
			tuicore.ItemType{Extension: "review", Type: t},
			stateChangeNavTarget,
		)
	}
}

// prItemData wraps a pull request with display context for card rendering
type prItemData struct {
	PR           review.PullRequest
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// prCardRenderer renders a pull request to a Card.
func prCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case prItemData:
		return PRToCardWithOptions(d.PR, PRToCardOptions{ShowEmail: d.ShowEmail, UserEmail: d.UserEmail, WorkspaceURL: d.WorkspaceURL})
	case review.PullRequest:
		return PRToCard(d)
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid pull request"}}
}

// prDimmedChecker checks if a pull request should be dimmed.
func prDimmedChecker(data any) bool {
	switch d := data.(type) {
	case prItemData:
		return d.PR.IsRetracted || d.PR.State == review.PRStateClosed
	case review.PullRequest:
		return d.IsRetracted || d.State == review.PRStateClosed
	}
	return false
}

// reviewNotificationCardRenderer renders a review notification to a Card.
func reviewNotificationCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	rn, ok := data.(review.ReviewNotification)
	if !ok {
		return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid notification"}}
	}
	icon := "⑂"
	badge := ""
	content := rn.PRSubject
	switch rn.Type {
	case "fork-pr":
		icon = "⑂"
		badge = "fork PR"
		content = rn.PRSubject
	case "approved":
		icon = "✓"
		badge = "approved"
		content = rn.Content
	case "changes-requested":
		icon = "✗"
		badge = "changes requested"
		content = rn.Content
	case "feedback":
		icon = "↩"
		badge = "feedback"
		content = rn.Content
	case "review-requested":
		icon = "⑂"
		badge = "review requested"
		content = rn.PRSubject
	case "pr-merged":
		icon = "⑂"
		badge = "merged"
		content = rn.PRSubject
	case "pr-closed":
		icon = "⑂"
		badge = "closed"
		content = rn.PRSubject
	case "pr-ready":
		icon = "⑂"
		badge = "ready for review"
		content = rn.PRSubject
	}
	var subtitleParts []tuicore.HeaderPart
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(rn.Timestamp)})
	if rn.RepoURL != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: protocol.GetFullDisplayName(rn.RepoURL)})
	}
	return tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:     icon,
			Title:    rn.ActorName,
			Subtitle: subtitleParts,
			Badge:    badge,
		},
		Content:      tuicore.CardContent{Text: content},
		ContentLinks: tuicore.ExtractContentLinks(content, rn.RepoURL, ""),
	}
}

// feedbackNavTarget navigates a feedback notification to the parent PR detail.
func feedbackNavTarget(id string) tuicore.Location {
	item, err := review.GetReviewItemByRef(id, "")
	if err != nil || !item.PullRequestHash.Valid {
		return tuicore.LocDetail(id)
	}
	prRef := protocol.CreateRef(protocol.RefTypeCommit,
		item.PullRequestHash.String, item.PullRequestRepoURL.String, item.PullRequestBranch.String)
	loc := tuicore.LocReviewPRDetail(prRef)
	loc.Params["focusID"] = id
	return loc
}

// stateChangeNavTarget navigates a state-change notification to the canonical PR detail.
func stateChangeNavTarget(id string) tuicore.Location {
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return tuicore.LocReviewPRDetail(id)
	}
	canonRepo, canonHash, canonBranch, err := cache.ResolveToCanonical(parsed.Repository, parsed.Value, parsed.Branch)
	if err != nil || canonHash == parsed.Value {
		return tuicore.LocReviewPRDetail(id)
	}
	prRef := protocol.CreateRef(protocol.RefTypeCommit, canonHash, canonRepo, canonBranch)
	return tuicore.LocReviewPRDetail(prRef)
}

// PRToCardOptions configures how a PullRequest is converted to a Card
type PRToCardOptions struct {
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// PRToCard converts a PullRequest to a Card for display.
func PRToCard(pr review.PullRequest) tuicore.Card {
	return PRToCardWithOptions(pr, PRToCardOptions{})
}

// PRToCardWithOptions converts a PullRequest to a Card with configuration options.
func PRToCardWithOptions(pr review.PullRequest, opts PRToCardOptions) tuicore.Card {
	icon := "⑂"
	stateStr := string(pr.State)
	if pr.IsDraft {
		stateStr = "draft"
	}

	cardAuthor := pr.Author
	cardTime := pr.Timestamp
	if pr.OriginalAuthor != nil {
		cardAuthor = *pr.OriginalAuthor
		if !pr.OriginalTime.IsZero() {
			cardTime = pr.OriginalTime
		}
	}
	authorName := cardAuthor.Name
	if opts.ShowEmail && cardAuthor.Email != "" {
		authorName += " <" + cardAuthor.Email + ">"
	}
	if pr.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(pr.Origin, opts.ShowEmail); a != "" {
			authorName = a
		}
	}
	var subtitleParts []tuicore.HeaderPart
	if pr.Origin != nil {
		if b := tuicore.FormatOriginBadge(pr.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
	}
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: authorName})
	if pr.Origin != nil && pr.Origin.Time != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatOriginTime(pr.Origin.Time)})
	} else if !cardTime.IsZero() {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(cardTime)})
	}
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: stateStr})

	baseShort := shortenBranchRef(pr.Base)
	headShort := shortenBranchRef(pr.Head)
	if baseShort != "" && headShort != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: baseShort + " ←"})
		headPart := tuicore.HeaderPart{Text: headShort}
		parsed := protocol.ParseRef(pr.Head)
		if parsed.Repository != "" {
			loc := tuicore.LocRepository(parsed.Repository, parsed.Value)
			headPart.Link = &loc
		}
		subtitleParts = append(subtitleParts, headPart)
	}

	if pr.Comments > 0 {
		loc := tuicore.LocReviewPRDetail(pr.ID)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{
			Text: fmt.Sprintf("↩ %d", pr.Comments),
			Link: &loc,
		})
	}

	// Add review summary to subtitle
	summary := pr.ReviewSummary
	if summary.Approved > 0 || summary.ChangesRequested > 0 {
		parts := []string{}
		if summary.Approved > 0 {
			parts = append(parts, fmt.Sprintf("✓%d", summary.Approved))
		}
		if summary.ChangesRequested > 0 {
			parts = append(parts, fmt.Sprintf("✗%d", summary.ChangesRequested))
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: fmt.Sprintf("[%s]", strings.Join(parts, " "))})
	}
	prOriginURL := ""
	if pr.Origin != nil {
		prOriginURL = pr.Origin.URL
	}
	isWorkspace := opts.WorkspaceURL != "" && pr.Repository == opts.WorkspaceURL
	if ref := tuicore.BuildRef(pr.ID, pr.Repository, pr.Branch, isWorkspace); ref != "" {
		var refLink *tuicore.Location
		if !isWorkspace && pr.Repository != "" {
			loc := tuicore.LocRepository(pr.Repository, pr.Branch)
			refLink = &loc
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: refLink})
	}

	if prOriginURL != "" {
		loc := tuicore.Location{Path: prOriginURL}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: "↗", Link: &loc})
	}

	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:     icon,
			Title:    pr.Subject,
			Subtitle: subtitleParts,
		},
		Content:      tuicore.CardContent{Text: pr.Body},
		ContentLinks: tuicore.ExtractContentLinks(pr.Body, pr.Repository, ""),
	}
	prEmail := cardAuthor.Email
	if pr.Origin != nil && pr.Origin.AuthorEmail != "" {
		prEmail = pr.Origin.AuthorEmail
	}
	if opts.UserEmail != "" && strings.EqualFold(prEmail, opts.UserEmail) {
		card.Header.IsMe = true
	}
	if opts.WorkspaceURL != "" && pr.Repository == opts.WorkspaceURL {
		card.Header.IsOwnRepo = true
	}
	return card
}

// FeedbackToCard converts a review.Feedback to a Card for display.
func FeedbackToCard(fb review.Feedback, userEmail string, isWorkspace bool, showEmail bool) tuicore.Card {
	icon := "↩"
	badge := ""
	switch fb.ReviewState {
	case review.ReviewStateApproved:
		icon = "✓"
		badge = "approved"
	case review.ReviewStateChangesRequested:
		icon = "✗"
		badge = "requested changes"
	}
	if fb.Suggestion {
		if badge != "" {
			badge += " [suggestion]"
		} else {
			badge = "[suggestion]"
		}
	}
	var subtitleParts []tuicore.HeaderPart
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(fb.Timestamp)})
	if fb.Comments > 0 {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: fmt.Sprintf("↩ %d", fb.Comments)})
	}
	if fb.File != "" {
		location := fb.File
		if fb.NewLine > 0 {
			location += fmt.Sprintf(":%d", fb.NewLine)
			if fb.NewLineEnd > 0 {
				location += fmt.Sprintf("-%d", fb.NewLineEnd)
			}
		} else if fb.OldLine > 0 {
			location += fmt.Sprintf(":%d (old)", fb.OldLine)
			if fb.OldLineEnd > 0 {
				location += fmt.Sprintf("-%d", fb.OldLineEnd)
			}
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: location})
	}
	if ref := tuicore.BuildRef(fb.ID, fb.Repository, fb.Branch, isWorkspace); ref != "" {
		var refLink *tuicore.Location
		if !isWorkspace && fb.Repository != "" {
			loc := tuicore.LocRepository(fb.Repository, fb.Branch)
			refLink = &loc
		}
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: refLink})
	}
	name := fb.Author.Name
	if showEmail && fb.Author.Email != "" {
		name += " <" + fb.Author.Email + ">"
	}
	var titleLink *tuicore.Location
	if fb.Author.Email != "" {
		loc := tuicore.LocSearchQuery("author:" + fb.Author.Email)
		titleLink = &loc
	}
	return tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:        icon,
			Title:       name,
			TitleLink:   titleLink,
			Subtitle:    subtitleParts,
			Badge:       badge,
			IsEdited:    fb.IsEdited,
			IsRetracted: fb.IsRetracted,
			IsMe:        userEmail != "" && strings.EqualFold(fb.Author.Email, userEmail),
			IsOwnRepo:   isWorkspace,
		},
		Content:      tuicore.CardContent{Text: fb.Content},
		ContentLinks: tuicore.ExtractContentLinks(fb.Content, fb.Repository, ""),
	}
}

// Register registers all review views with the host.
func Register(host tuicore.ViewHost) {
	state := host.State()
	host.AddView("/review/prs", NewPRsView(state.Workdir))
	host.AddView("/review/pr", NewPRDetailView(state.Workdir))
	host.AddView("/review/new-pr", NewPRFormView(state.Workdir))
	host.AddView("/review/edit-pr", NewPREditFormView(state.Workdir))
	host.AddView("/review/feedback", NewFeedbackFormView(state.Workdir))
	host.AddView("/review/pr/history", NewPRHistoryView(state.Workdir))
	host.AddView("/review/diff", NewDiffView(state.Workdir))
	host.AddView("/review/pr/interdiff", NewInterdiffView(state.Workdir))
}

// PRCreatedMsg is sent when a pull request is created.
type PRCreatedMsg struct {
	PR  review.PullRequest
	Err error
}

// PRUpdatedMsg is sent when a pull request is updated.
type PRUpdatedMsg struct {
	PR  review.PullRequest
	Err error
}

// PRRetractedMsg is sent when a pull request is retracted.
type PRRetractedMsg struct {
	ID  string
	Err error
}

// FeedbackCreatedMsg is sent when feedback is created on a pull request.
type FeedbackCreatedMsg struct {
	Feedback review.Feedback
	PRID     string
	Err      error
}

func handleReviewMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case PRCreatedMsg:
		return handlePRCreated(msg, ctx)
	case PRUpdatedMsg:
		return handlePRUpdated(msg, ctx)
	case PRRetractedMsg:
		return handlePRRetracted(msg, ctx)
	case FeedbackCreatedMsg:
		return handleFeedbackCreated(msg, ctx)
	case SuggestionAppliedMsg:
		return handleSuggestionApplied(msg, ctx)
	}
	return false, nil
}

func handleSuggestionApplied(msg SuggestionAppliedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, "Apply suggestion: "+msg.Err.Error(), "review")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Suggestion applied to %s", msg.File),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, msgCmd
}

func handlePRCreated(msg PRCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, "PR create: "+msg.Err.Error(), "review")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Created: %s", msg.PR.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewPRDetail(msg.PR.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handlePRUpdated(msg PRUpdatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, "PR update: "+msg.Err.Error(), "review")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.PR.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewPRDetail(msg.PR.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handlePRRetracted(msg PRRetractedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout("Pull request retracted", tuicore.MessageTypeSuccess, 5*time.Second)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{Action: tuicore.NavBack}
	})
}

func handleFeedbackCreated(msg FeedbackCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		ctx.Host().State().AddLogEntry(tuicore.LogSeverityError, "Feedback: "+msg.Err.Error(), "review")
		ctx.Nav().SetErrorLogCount(ctx.Host().State().ErrorLogCount())
		return true, nil
	}
	label := "Feedback submitted"
	switch msg.Feedback.ReviewState {
	case review.ReviewStateApproved:
		label = "Pull request approved"
	case review.ReviewStateChangesRequested:
		label = "Changes requested"
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(label, tuicore.MessageTypeSuccess, 5*time.Second)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocReviewPRDetail(msg.PRID),
			Action:   tuicore.NavReplace,
		}
	})
}

// shortenBranchRef extracts the branch name from a local ref (#branch:main → main).
// Remote refs (url#branch:name) are returned as-is.
func shortenBranchRef(ref string) string {
	parsed := protocol.ParseRef(ref)
	if parsed.Type == protocol.RefTypeBranch && parsed.Repository == "" {
		return parsed.Value
	}
	return ref
}

// branchRefLocation resolves a branch ref to a navigable repository location.
func branchRefLocation(ref, workspaceURL string) *tuicore.Location {
	parsed := protocol.ParseRef(ref)
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return nil
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	if repoURL == "" {
		return nil
	}
	loc := tuicore.LocRepository(repoURL, parsed.Value)
	return &loc
}

// refreshCacheSize returns a command that recalculates cache size.
func refreshCacheSize(cacheDir string) tea.Cmd {
	return func() tea.Msg {
		if stats, err := cache.GetStats(cacheDir); err == nil {
			return tuicore.CacheSizeMsg{Size: cache.FormatBytes(stats.TotalBytes)}
		}
		return nil
	}
}
