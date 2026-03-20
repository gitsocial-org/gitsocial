// util_register.go - PM extension view and message handler registration
package tuipm

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func init() {
	// Register view metadata for PM paths (enables ESC→GoBack and correct keybinding context)
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/board", Context: tuicore.PMBoard, Title: "Board", Icon: "▦", NavItemID: "pm.board"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/issues", Context: tuicore.PMIssues, Title: "Issues", Icon: "○", NavItemID: "pm.issues", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/issue", Context: tuicore.PMIssueDetail, Title: "Issue Detail", Icon: "○", NavItemID: "pm.issues", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/issue/history", Context: tuicore.PMIssueHistory, Title: "Issue History", Icon: "○", NavItemID: "pm.issues", Component: "VersionPicker"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/new-issue", Context: tuicore.PMIssueDetail, Title: "New Issue", Icon: "○", NavItemID: "pm.issues", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/edit-issue", Context: tuicore.PMIssueDetail, Title: "Edit Issue", Icon: "○", NavItemID: "pm.issues", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/milestones", Context: tuicore.PMMilestones, Title: "Milestones", Icon: "◇", NavItemID: "pm.milestones", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/milestone", Context: tuicore.PMMilestoneDetail, Title: "Milestone Detail", Icon: "◇", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/milestone/history", Context: tuicore.PMMilestoneHistory, Title: "Milestone History", Icon: "◇", NavItemID: "pm.milestones", Component: "VersionPicker"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/new-milestone", Context: tuicore.PMMilestoneDetail, Title: "New Milestone", Icon: "◇", NavItemID: "pm.milestones", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/edit-milestone", Context: tuicore.PMMilestoneDetail, Title: "Edit Milestone", Icon: "◇", NavItemID: "pm.milestones", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/sprints", Context: tuicore.PMSprints, Title: "Sprints", Icon: "⟳", NavItemID: "pm.sprints", Component: "CardList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/sprint", Context: tuicore.PMSprintDetail, Title: "Sprint Detail", Icon: "⟳", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/sprint/history", Context: tuicore.PMSprintHistory, Title: "Sprint History", Icon: "⟳", NavItemID: "pm.sprints", Component: "VersionPicker"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/new-sprint", Context: tuicore.PMSprintDetail, Title: "New Sprint", Icon: "⟳", NavItemID: "pm.sprints", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/edit-sprint", Context: tuicore.PMSprintDetail, Title: "Edit Sprint", Icon: "⟳", NavItemID: "pm.sprints", Component: "SectionList"})
	tuicore.RegisterViewMeta(tuicore.ViewMeta{Path: "/pm/config", Context: tuicore.PMConfig, Title: "PM Config", Icon: "▢"})

	tuicore.RegisterMessageHandler(handlePMMessages)
	// Register nav targets for PM types
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "pm", Type: "issue"},
		func(id string) tuicore.Location { return tuicore.LocPMIssueDetail(id) },
	)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "pm", Type: "milestone"},
		func(id string) tuicore.Location { return tuicore.LocPMMilestoneDetail(id) },
	)
	tuicore.RegisterNavTarget(
		tuicore.ItemType{Extension: "pm", Type: "sprint"},
		func(id string) tuicore.Location { return tuicore.LocPMSprintDetail(id) },
	)

	// Register card renderers for PM types
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "pm", Type: "issue"},
		issueCardRenderer,
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "pm", Type: "milestone"},
		milestoneCardRenderer,
	)
	tuicore.RegisterCardRenderer(
		tuicore.ItemType{Extension: "pm", Type: "sprint"},
		sprintCardRenderer,
	)
	for _, t := range []string{"issue-assigned", "issue-closed", "issue-reopened", "fork-issue"} {
		tuicore.RegisterCardRenderer(
			tuicore.ItemType{Extension: "pm", Type: t},
			pmNotificationCardRenderer,
		)
	}
	for _, t := range []string{"issue-closed", "issue-reopened"} {
		tuicore.RegisterNavTarget(
			tuicore.ItemType{Extension: "pm", Type: t},
			stateChangeIssueNavTarget,
		)
	}
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "pm", Type: "issue"},
		issueDimmedChecker,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "pm", Type: "milestone"},
		milestoneDimmedChecker,
	)
	tuicore.RegisterDimmedChecker(
		tuicore.ItemType{Extension: "pm", Type: "sprint"},
		sprintDimmedChecker,
	)

}

// issueCardRenderer renders a PM issue to a Card.
func issueCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case issueItemData:
		return IssueToCardWithOptions(d.Issue, IssueToCardOptions{ShowEmail: d.ShowEmail, UserEmail: d.UserEmail, ContributorNames: d.ContributorNames})
	case pm.Issue:
		return IssueToCard(d)
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid issue"}}
}

// milestoneCardRenderer renders a PM milestone to a Card.
func milestoneCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case milestoneItemData:
		return MilestoneToCardWithOptions(d.Milestone, MilestoneToCardOptions{UserEmail: d.UserEmail, ShowEmail: d.ShowEmail, WorkspaceURL: d.WorkspaceURL})
	case pm.Milestone:
		return MilestoneToCard(d)
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid milestone"}}
}

// sprintCardRenderer renders a PM sprint to a Card.
func sprintCardRenderer(data any, resolver tuicore.ItemResolver) tuicore.Card {
	switch d := data.(type) {
	case sprintItemData:
		return SprintToCardWithOptions(d.Sprint, SprintToCardOptions{UserEmail: d.UserEmail, ShowEmail: d.ShowEmail, WorkspaceURL: d.WorkspaceURL})
	case pm.Sprint:
		return SprintToCard(d)
	}
	return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid sprint"}}
}

// issueDimmedChecker checks if an issue should be dimmed.
func issueDimmedChecker(data any) bool {
	switch d := data.(type) {
	case issueItemData:
		return d.Issue.State == pm.StateClosed || d.Issue.State == pm.StateCancelled
	case pm.Issue:
		return d.State == pm.StateClosed || d.State == pm.StateCancelled
	}
	return false
}

// milestoneDimmedChecker checks if a milestone should be dimmed.
func milestoneDimmedChecker(data any) bool {
	milestone, ok := data.(pm.Milestone)
	if !ok {
		return false
	}
	return milestone.State == pm.StateClosed || milestone.State == pm.StateCancelled
}

// sprintDimmedChecker checks if a sprint should be dimmed.
func sprintDimmedChecker(data any) bool {
	sprint, ok := data.(pm.Sprint)
	if !ok {
		return false
	}
	return sprint.State == pm.SprintStateCompleted || sprint.State == pm.SprintStateCancelled
}

// pmNotificationCardRenderer renders a PM notification (e.g. issue-assigned) to a Card.
func pmNotificationCardRenderer(data any, _ tuicore.ItemResolver) tuicore.Card {
	pn, ok := data.(pm.PMNotification)
	if !ok {
		return tuicore.Card{Header: tuicore.CardHeader{Title: "Invalid notification"}}
	}
	badge := "assigned you"
	switch pn.Type {
	case "fork-issue":
		badge = "fork issue"
	case "issue-closed":
		badge = "closed"
	case "issue-reopened":
		badge = "reopened"
	}
	var subtitleParts []tuicore.HeaderPart
	subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(pn.Timestamp)})
	if pn.RepoURL != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: protocol.GetFullDisplayName(pn.RepoURL)})
	}
	if pn.State != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: pn.State})
	}
	return tuicore.Card{
		Header: tuicore.CardHeader{
			Icon:     "○",
			Title:    pn.ActorName,
			Subtitle: subtitleParts,
			Badge:    badge,
		},
		Content: tuicore.CardContent{Text: pn.Subject},
	}
}

// stateChangeIssueNavTarget navigates a state-change notification to the canonical issue detail.
func stateChangeIssueNavTarget(id string) tuicore.Location {
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return tuicore.LocPMIssueDetail(id)
	}
	canonRepo, canonHash, canonBranch, err := cache.ResolveToCanonical(parsed.Repository, parsed.Value, parsed.Branch)
	if err != nil || canonHash == parsed.Value {
		return tuicore.LocPMIssueDetail(id)
	}
	issueRef := protocol.CreateRef(protocol.RefTypeCommit, canonHash, canonRepo, canonBranch)
	return tuicore.LocPMIssueDetail(issueRef)
}

// Register registers all PM views with the host.
func Register(host tuicore.ViewHost) {
	state := host.State()
	board := NewBoardView(state.Workdir)
	issues := NewIssuesView(state.Workdir)
	issueDetail := NewIssueDetailView(state.Workdir)
	issueHistory := NewIssueHistoryView(state.Workdir)
	issueForm := NewIssueFormView(state.Workdir)
	issueEditForm := NewIssueEditFormView(state.Workdir)
	config := NewConfigView(state.Workdir)
	milestones := NewMilestonesView(state.Workdir)
	milestoneDetail := NewMilestoneDetailView(state.Workdir)
	milestoneHistory := NewMilestoneHistoryView(state.Workdir)
	milestoneForm := NewMilestoneFormView(state.Workdir)
	milestoneEditForm := NewMilestoneEditFormView(state.Workdir)
	sprints := NewSprintsView(state.Workdir)
	sprintDetail := NewSprintDetailView(state.Workdir)
	sprintHistory := NewSprintHistoryView(state.Workdir)
	sprintForm := NewSprintFormView(state.Workdir)
	sprintEditForm := NewSprintEditFormView(state.Workdir)
	host.AddView("/pm/board", board)
	host.AddView("/pm/issues", issues)
	host.AddView("/pm/issue", issueDetail)
	host.AddView("/pm/issue/history", issueHistory)
	host.AddView("/pm/new-issue", issueForm)
	host.AddView("/pm/edit-issue", issueEditForm)
	host.AddView("/pm/config", config)
	host.AddView("/pm/milestones", milestones)
	host.AddView("/pm/milestone", milestoneDetail)
	host.AddView("/pm/milestone/history", milestoneHistory)
	host.AddView("/pm/new-milestone", milestoneForm)
	host.AddView("/pm/edit-milestone", milestoneEditForm)
	host.AddView("/pm/sprints", sprints)
	host.AddView("/pm/sprint", sprintDetail)
	host.AddView("/pm/sprint/history", sprintHistory)
	host.AddView("/pm/new-sprint", sprintForm)
	host.AddView("/pm/edit-sprint", sprintEditForm)
}

// Message handlers

func handlePMMessages(msg tea.Msg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case IssueCreatedMsg:
		return handleIssueCreated(msg, ctx)
	case IssueUpdatedMsg:
		return handleIssueUpdated(msg, ctx)
	case MilestoneUpdatedMsg:
		return handleMilestoneUpdated(msg, ctx)
	case SprintUpdatedMsg:
		return handleSprintUpdated(msg, ctx)
	case PMConfigSavedMsg:
		return handlePMConfigSaved(msg, ctx)
	case IssueRetractedMsg:
		return handleRetracted(msg.Err, "Issue retracted", ctx)
	case MilestoneRetractedMsg:
		return handleRetracted(msg.Err, "Milestone retracted", ctx)
	case SprintRetractedMsg:
		return handleRetracted(msg.Err, "Sprint retracted", ctx)
	}
	return false, nil
}

func handlePMConfigSaved(msg PMConfigSavedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err == "" {
		pm.UpdatePMNavItems(ctx.Nav().Registry(), ctx.Workdir())
	}
	// Pass through to ConfigView
	return false, nil
}

func handleIssueCreated(msg IssueCreatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Created: %s", msg.Issue.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	path := ctx.Router().Location().Path
	if path == "/pm/board" || path == "/pm/issues" {
		// Forward to active view so board/issues can refresh inline
		viewCmd := ctx.Host().Update(msg)
		return true, tea.Batch(msgCmd, viewCmd)
	}
	// Navigate to issue detail (board/issues will reload via Activate on back-navigation)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocPMIssueDetail(msg.Issue.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleIssueUpdated(msg IssueUpdatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.Issue.Subject),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocPMIssueDetail(msg.Issue.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleMilestoneUpdated(msg MilestoneUpdatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.Milestone.Title),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocPMMilestoneDetail(msg.Milestone.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleSprintUpdated(msg SprintUpdatedMsg, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if msg.Err != nil {
		ctx.Host().SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(
		fmt.Sprintf("Updated: %s", msg.Sprint.Title),
		tuicore.MessageTypeSuccess,
		5*time.Second,
	)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{
			Location: tuicore.LocPMSprintDetail(msg.Sprint.ID),
			Action:   tuicore.NavReplace,
		}
	})
}

func handleRetracted(err error, successMsg string, ctx tuicore.AppContext) (bool, tea.Cmd) {
	if err != nil {
		ctx.Host().SetMessage(err.Error(), tuicore.MessageTypeError)
		return true, nil
	}
	msgCmd := ctx.Host().SetMessageWithTimeout(successMsg, tuicore.MessageTypeSuccess, 5*time.Second)
	return true, tea.Batch(msgCmd, func() tea.Msg {
		return tuicore.NavigateMsg{Action: tuicore.NavBack}
	})
}
