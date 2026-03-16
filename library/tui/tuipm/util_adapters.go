// util_adapters.go - Adapters bridging pm types to tuicore interfaces
package tuipm

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// IssueToCardOptions configures how an Issue is converted to a Card
type IssueToCardOptions struct {
	ShowEmail        bool
	UserEmail        string
	ContributorNames map[string]string
}

// issueItemData wraps an issue with user context for card rendering
type issueItemData struct {
	Issue            pm.Issue
	ShowEmail        bool
	UserEmail        string
	ContributorNames map[string]string
}

// IssueToCard converts a pm.Issue to a Card.
func IssueToCard(issue pm.Issue) tuicore.Card {
	return IssueToCardWithOptions(issue, IssueToCardOptions{})
}

// IssueToCardWithOptions converts a pm.Issue to a subject-first Card.
func IssueToCardWithOptions(issue pm.Issue, opts IssueToCardOptions) tuicore.Card {
	badge := ""
	switch issue.State {
	case pm.StateClosed:
		badge = "closed"
	case pm.StateCancelled:
		badge = "canceled"
	}
	isAssigned := false
	var stats []tuicore.CardStat
	for _, a := range issue.Assignees {
		if opts.UserEmail != "" && strings.EqualFold(a, opts.UserEmail) {
			isAssigned = true
			stats = append(stats, tuicore.CardStat{Text: "☛  me"})
		} else {
			label := a
			if opts.ContributorNames != nil {
				if name, ok := opts.ContributorNames[a]; ok {
					label = name
				}
			}
			stats = append(stats, tuicore.CardStat{Text: "☛  " + label})
		}
	}
	if issue.Due != nil {
		stats = append(stats, tuicore.CardStat{Text: "due: " + issue.Due.Format("Jan 2")})
	}
	for _, l := range issue.Labels {
		if l.Scope != "" {
			stats = append(stats, tuicore.CardStat{Text: l.Scope + "/" + l.Value})
		} else {
			stats = append(stats, tuicore.CardStat{Text: l.Value})
		}
	}
	if issue.Comments > 0 {
		loc := tuicore.LocPMIssueDetail(issue.ID)
		stats = append(stats, tuicore.CardStat{Text: fmt.Sprintf("↩ %d", issue.Comments), Link: &loc})
	}
	icon := "○"
	if issue.State == pm.StateClosed || issue.State == pm.StateCancelled {
		icon = "●"
	}
	if issue.IsUnpushed {
		if badge != "" {
			badge += " · ⇡"
		} else {
			badge = "⇡"
		}
	}
	// Build subtitle with origin info when present
	var subtitleParts []tuicore.HeaderPart
	if issue.Origin != nil {
		if b := tuicore.FormatOriginBadge(issue.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
		if a := tuicore.FormatOriginAuthorDisplay(issue.Origin, opts.ShowEmail); a != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: a})
		}
		if t := tuicore.FormatOriginTime(issue.Origin.Time); t != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: t})
		}
	}
	if ref := tuicore.BuildRef(issue.ID, issue.Repository, issue.Branch, false); ref != "" {
		loc := tuicore.LocRepository(issue.Repository, issue.Branch)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: &loc})
	}
	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       issue.Subject,
			Subtitle:    subtitleParts,
			Badge:       badge,
			Icon:        icon,
			IsAssigned:  isAssigned,
			IsEdited:    issue.IsEdited,
			IsRetracted: issue.IsRetracted,
		},
		Stats: stats,
	}
	return card
}

// IssuesToItems converts []pm.Issue to []tuicore.DisplayItem using universal Item
func IssuesToItems(issues []pm.Issue, userEmail string, contributorNames map[string]string, showEmail bool) []tuicore.DisplayItem {
	items := make([]tuicore.DisplayItem, len(issues))
	for i, issue := range issues {
		items[i] = tuicore.NewItem(issue.ID, "pm", "issue", issue.Timestamp, issueItemData{
			Issue:            issue,
			ShowEmail:        showEmail,
			UserEmail:        userEmail,
			ContributorNames: contributorNames,
		})
	}
	return items
}

// ItemToIssue extracts pm.Issue from a DisplayItem
func ItemToIssue(item tuicore.DisplayItem) (pm.Issue, bool) {
	if ui, ok := item.(tuicore.Item); ok {
		if d, ok := ui.Data.(issueItemData); ok {
			return d.Issue, true
		}
		if issue, ok := ui.Data.(pm.Issue); ok {
			return issue, true
		}
	}
	return pm.Issue{}, false
}

// MilestoneToCardOptions configures how a Milestone is converted to a Card
type MilestoneToCardOptions struct {
	FullTime     bool
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// MilestoneToCard converts a pm.Milestone to a Card.
func MilestoneToCard(milestone pm.Milestone) tuicore.Card {
	return MilestoneToCardWithOptions(milestone, MilestoneToCardOptions{})
}

// MilestoneToCardWithOptions converts a pm.Milestone to a Card with configuration options.
func MilestoneToCardWithOptions(milestone pm.Milestone, opts MilestoneToCardOptions) tuicore.Card {
	name := milestone.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if opts.ShowEmail && milestone.Author.Email != "" {
		name += " <" + milestone.Author.Email + ">"
	}
	if milestone.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(milestone.Origin, opts.ShowEmail); a != "" {
			name = a
		}
	}
	var subtitleParts []tuicore.HeaderPart
	if milestone.Origin != nil {
		if b := tuicore.FormatOriginBadge(milestone.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
	}
	if milestone.Origin != nil && milestone.Origin.Time != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatOriginTime(milestone.Origin.Time)})
	} else if opts.FullTime {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatFullTime(milestone.Timestamp)})
	} else {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(milestone.Timestamp)})
	}
	if ref := tuicore.BuildRef(milestone.ID, milestone.Repository, milestone.Branch, false); ref != "" {
		loc := tuicore.LocRepository(milestone.Repository, milestone.Branch)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: &loc})
	}
	badge := ""
	switch milestone.State {
	case pm.StateClosed:
		badge = "closed"
	case pm.StateCancelled:
		badge = "canceled"
	}
	var stats []tuicore.CardStat
	if milestone.Due != nil {
		stats = append(stats, tuicore.CardStat{Text: "due: " + milestone.Due.Format("2006-01-02")})
	}
	if milestone.IssueCount > 0 {
		stats = append(stats, tuicore.CardStat{Text: fmt.Sprintf("%d/%d issues", milestone.ClosedCount, milestone.IssueCount)})
	}
	if milestone.IsUnpushed {
		if badge != "" {
			badge += " · ⇡"
		} else {
			badge = "⇡"
		}
	}
	var titleLink *tuicore.Location
	milestoneEmail := milestone.Author.Email
	if milestone.Origin != nil && milestone.Origin.AuthorEmail != "" {
		milestoneEmail = milestone.Origin.AuthorEmail
	}
	if milestoneEmail != "" {
		loc := tuicore.LocSearchQuery("author:" + milestoneEmail)
		titleLink = &loc
	}
	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       name,
			TitleLink:   titleLink,
			Subtitle:    subtitleParts,
			Badge:       badge,
			Icon:        "◇",
			IsEdited:    milestone.IsEdited,
			IsRetracted: milestone.IsRetracted,
		},
		Content: tuicore.CardContent{
			Text: milestone.Title,
		},
		Stats: stats,
	}
	if opts.UserEmail != "" && strings.EqualFold(milestoneEmail, opts.UserEmail) {
		card.Header.IsMe = true
	}
	if opts.WorkspaceURL != "" && milestone.Repository == opts.WorkspaceURL {
		card.Header.IsOwnRepo = true
	}
	return card
}

// milestoneItemData wraps a milestone with display context for card rendering
type milestoneItemData struct {
	Milestone    pm.Milestone
	UserEmail    string
	ShowEmail    bool
	WorkspaceURL string
}

// MilestonesToItems converts []pm.Milestone to []tuicore.DisplayItem using universal Item
func MilestonesToItems(milestones []pm.Milestone, userEmail string, showEmail bool, workspaceURL string) []tuicore.DisplayItem {
	items := make([]tuicore.DisplayItem, len(milestones))
	for i, m := range milestones {
		items[i] = tuicore.NewItem(m.ID, "pm", "milestone", m.Timestamp, milestoneItemData{
			Milestone:    m,
			UserEmail:    userEmail,
			ShowEmail:    showEmail,
			WorkspaceURL: workspaceURL,
		})
	}
	return items
}

// ItemToMilestone extracts pm.Milestone from a DisplayItem
func ItemToMilestone(item tuicore.DisplayItem) (pm.Milestone, bool) {
	if ui, ok := item.(tuicore.Item); ok {
		if d, ok := ui.Data.(milestoneItemData); ok {
			return d.Milestone, true
		}
		if m, ok := ui.Data.(pm.Milestone); ok {
			return m, true
		}
	}
	return pm.Milestone{}, false
}

// SprintToCardOptions configures how a Sprint is converted to a Card
type SprintToCardOptions struct {
	FullTime     bool
	ShowEmail    bool
	UserEmail    string
	WorkspaceURL string
}

// SprintToCard converts a pm.Sprint to a Card.
func SprintToCard(sprint pm.Sprint) tuicore.Card {
	return SprintToCardWithOptions(sprint, SprintToCardOptions{})
}

// SprintToCardWithOptions converts a pm.Sprint to a Card with configuration options.
func SprintToCardWithOptions(sprint pm.Sprint, opts SprintToCardOptions) tuicore.Card {
	name := sprint.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if opts.ShowEmail && sprint.Author.Email != "" {
		name += " <" + sprint.Author.Email + ">"
	}
	if sprint.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(sprint.Origin, opts.ShowEmail); a != "" {
			name = a
		}
	}
	var subtitleParts []tuicore.HeaderPart
	if sprint.Origin != nil {
		if b := tuicore.FormatOriginBadge(sprint.Origin); b != "" {
			subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: b})
		}
	}
	if sprint.Origin != nil && sprint.Origin.Time != "" {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatOriginTime(sprint.Origin.Time)})
	} else if opts.FullTime {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatFullTime(sprint.Timestamp)})
	} else {
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: tuicore.FormatTime(sprint.Timestamp)})
	}
	if ref := tuicore.BuildRef(sprint.ID, sprint.Repository, sprint.Branch, false); ref != "" {
		loc := tuicore.LocRepository(sprint.Repository, sprint.Branch)
		subtitleParts = append(subtitleParts, tuicore.HeaderPart{Text: ref, Link: &loc})
	}
	badge := ""
	switch sprint.State {
	case pm.SprintStateActive:
		badge = "active"
	case pm.SprintStateCompleted:
		badge = "completed"
	case pm.SprintStateCancelled:
		badge = "canceled"
	}
	var stats []tuicore.CardStat
	dateRange := fmt.Sprintf("%s - %s", sprint.Start.Format("Jan 2, 2006"), sprint.End.Format("Jan 2, 2006"))
	stats = append(stats, tuicore.CardStat{Text: dateRange})
	if sprint.IssueCount > 0 {
		stats = append(stats, tuicore.CardStat{Text: fmt.Sprintf("%d/%d issues", sprint.ClosedCount, sprint.IssueCount)})
	}
	if sprint.IsUnpushed {
		if badge != "" {
			badge += " · ⇡"
		} else {
			badge = "⇡"
		}
	}
	var titleLink *tuicore.Location
	sprintEmail := sprint.Author.Email
	if sprint.Origin != nil && sprint.Origin.AuthorEmail != "" {
		sprintEmail = sprint.Origin.AuthorEmail
	}
	if sprintEmail != "" {
		loc := tuicore.LocSearchQuery("author:" + sprintEmail)
		titleLink = &loc
	}
	card := tuicore.Card{
		Header: tuicore.CardHeader{
			Title:       name,
			TitleLink:   titleLink,
			Subtitle:    subtitleParts,
			Badge:       badge,
			Icon:        "◷",
			IsEdited:    sprint.IsEdited,
			IsRetracted: sprint.IsRetracted,
		},
		Content: tuicore.CardContent{
			Text: sprint.Title,
		},
		Stats: stats,
	}
	if opts.UserEmail != "" && strings.EqualFold(sprintEmail, opts.UserEmail) {
		card.Header.IsMe = true
	}
	if opts.WorkspaceURL != "" && sprint.Repository == opts.WorkspaceURL {
		card.Header.IsOwnRepo = true
	}
	return card
}

// sprintItemData wraps a sprint with display context for card rendering
type sprintItemData struct {
	Sprint       pm.Sprint
	UserEmail    string
	ShowEmail    bool
	WorkspaceURL string
}

// SprintsToItems converts []pm.Sprint to []tuicore.DisplayItem using universal Item
func SprintsToItems(sprints []pm.Sprint, userEmail string, showEmail bool, workspaceURL string) []tuicore.DisplayItem {
	items := make([]tuicore.DisplayItem, len(sprints))
	for i, s := range sprints {
		items[i] = tuicore.NewItem(s.ID, "pm", "sprint", s.Timestamp, sprintItemData{
			Sprint:       s,
			UserEmail:    userEmail,
			ShowEmail:    showEmail,
			WorkspaceURL: workspaceURL,
		})
	}
	return items
}

// ItemToSprint extracts pm.Sprint from a DisplayItem
func ItemToSprint(item tuicore.DisplayItem) (pm.Sprint, bool) {
	if ui, ok := item.(tuicore.Item); ok {
		if d, ok := ui.Data.(sprintItemData); ok {
			return d.Sprint, true
		}
		if s, ok := ui.Data.(pm.Sprint); ok {
			return s, true
		}
	}
	return pm.Sprint{}, false
}
