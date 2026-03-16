// issue_detail.go - Issue detail view with metadata and comments
package tuipm

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// IssueDetailView displays a single issue with metadata and comments.
type IssueDetailView struct {
	workdir          string
	width            int
	height           int
	issueID          string
	issue            *pm.Issue
	milestone        *pm.Milestone
	sprint           *pm.Sprint
	comments         []social.Post
	loaded           bool
	userEmail        string
	showEmail        bool
	workspaceURL     string
	focusID          string
	contributorNames map[string]string
	showRaw          bool
	confirm          tuicore.ConfirmDialog
	sectionList      *tuicore.SectionList
	sourceIndex      int
	sourceTotal      int
}

// NewIssueDetailView creates a new issue detail view.
func NewIssueDetailView(workdir string) *IssueDetailView {
	return &IssueDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *IssueDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the issue data.
func (v *IssueDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.confirm.Reset()
	v.issueID = state.Router.Location().Param("issueID")
	v.focusID = state.Router.Location().Param("focusID")
	v.loaded = false
	v.issue = nil
	v.milestone = nil
	v.sprint = nil
	v.comments = nil
	v.sectionList.SetSections(nil)
	if state.DetailSource != nil {
		v.sourceIndex = state.DetailSource.Index
		v.sourceTotal = state.DetailSource.Total
		if state.DetailSource.SearchQuery != "" {
			v.sectionList.SetHighlightQuery(tuicore.ExtractSearchTerms(state.DetailSource.SearchQuery))
		}
	} else {
		v.sourceIndex = 0
		v.sourceTotal = 0
		v.sectionList.SetHighlightQuery("")
	}
	return v.loadIssue()
}

func (v *IssueDetailView) loadIssue() tea.Cmd {
	issueID := v.issueID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.GetIssue(issueID)
		if !result.Success {
			return IssueDetailLoadedMsg{Err: fmt.Errorf("issue not found: %s", issueID)}
		}
		issue := result.Data
		branch := gitmsg.GetExtBranch(workdir, "pm")
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		ref := protocol.ParseRef(issue.ID)
		if _, ok := unpushed[ref.Value]; ok {
			issue.IsUnpushed = true
		}
		commentsResult := pm.GetItemComments(issue.ID, "")
		var comments []social.Post
		if commentsResult.Success {
			comments = commentsResult.Data
		}
		var milestone *pm.Milestone
		var sprint *pm.Sprint
		if issue.Milestone != nil {
			mRef := protocol.CreateRef(protocol.RefTypeCommit, issue.Milestone.Hash, issue.Milestone.RepoURL, issue.Milestone.Branch)
			if res := pm.GetMilestone(mRef); res.Success {
				milestone = &res.Data
			}
		}
		if issue.Sprint != nil {
			sRef := protocol.CreateRef(protocol.RefTypeCommit, issue.Sprint.Hash, issue.Sprint.RepoURL, issue.Sprint.Branch)
			if res := pm.GetSprint(sRef); res.Success {
				sprint = &res.Data
			}
		}
		contributorNames := buildContributorNameMap(workdir)
		return IssueDetailLoadedMsg{Issue: &issue, Comments: comments, Milestone: milestone, Sprint: sprint, ContributorNames: contributorNames}
	}
}

// IssueDetailLoadedMsg signals that the issue has been loaded.
type IssueDetailLoadedMsg struct {
	Issue            *pm.Issue
	Milestone        *pm.Milestone
	Sprint           *pm.Sprint
	Comments         []social.Post
	ContributorNames map[string]string
	Err              error
}

// Update handles messages.
func (v *IssueDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case IssueDetailLoadedMsg:
		v.loaded = true
		if msg.Err == nil {
			v.issue = msg.Issue
			v.milestone = msg.Milestone
			v.sprint = msg.Sprint
			for i := range msg.Comments {
				if msg.Comments[i].Repository == v.workspaceURL {
					msg.Comments[i].Display.IsWorkspacePost = true
				}
			}
			v.comments = msg.Comments
			v.contributorNames = msg.ContributorNames
			v.buildSections()
			if v.focusID != "" {
				for i, c := range v.comments {
					if c.ID == v.focusID {
						v.sectionList.SetSelected(1 + i)
						break
					}
				}
				v.focusID = ""
			}
		}
		return nil
	case tea.KeyPressMsg, tea.MouseMsg:
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if handled, cmd := v.confirm.HandleKey(key.String()); handled {
				return cmd
			}
		}
		consumed, cmd := v.sectionList.Update(msg)
		if consumed {
			return cmd
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "m":
				if v.milestone != nil {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMMilestoneDetail(v.milestone.ID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "s":
				if v.sprint != nil {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMSprintDetail(v.sprint.ID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "e":
				if v.issue != nil {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMEditIssue(v.issue.ID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "h":
				if v.issue != nil && v.issue.IsEdited {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMIssueHistory(v.issue.ID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "c":
				if v.issue != nil {
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{
							Mode:     "comment",
							TargetID: v.issue.ID,
						}
					}
				}
			case "C":
				if v.issue != nil && v.issue.State == pm.StateOpen {
					return v.closeIssue()
				}
			case "left":
				return v.navigateSource(state, -1)
			case "right":
				return v.navigateSource(state, 1)
			case "X":
				if v.issue != nil {
					v.confirm.Show("Retract this issue?", false, func() tea.Cmd { return v.doRetract() })
					return nil
				}
			}
		}
	}
	if v.sectionList.IsInputActive() {
		return v.sectionList.UpdateSearchInput(msg)
	}
	return nil
}

func (v *IssueDetailView) buildSections() {
	var sections []tuicore.Section
	// Hero section (no label) — the issue card
	issue := v.issue
	milestone := v.milestone
	sprint := v.sprint
	contributorNames := v.contributorNames
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderIssueCard(issue, milestone, sprint, contributorNames, width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return issue.Subject + " " + issue.Body },
			Links: func() []tuicore.CardLink {
				var links []tuicore.CardLink
				if issue.Origin != nil && issue.Origin.URL != "" {
					links = append(links, tuicore.CardLink{Label: "Source", Location: tuicore.Location{Path: issue.Origin.URL}})
				}
				if milestone != nil {
					links = append(links, tuicore.CardLink{Label: "milestone", Location: tuicore.LocPMMilestoneDetail(milestone.ID)})
				}
				if sprint != nil {
					links = append(links, tuicore.CardLink{Label: "sprint", Location: tuicore.LocPMSprintDetail(sprint.ID)})
				}
				for _, ref := range issue.Blocks {
					refID := protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
					links = append(links, tuicore.CardLink{Label: "blocks", Location: tuicore.LocPMIssueDetail(refID)})
				}
				for _, ref := range issue.BlockedBy {
					refID := protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
					links = append(links, tuicore.CardLink{Label: "blocked-by", Location: tuicore.LocPMIssueDetail(refID)})
				}
				for _, ref := range issue.Related {
					refID := protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
					links = append(links, tuicore.CardLink{Label: "related", Location: tuicore.LocPMIssueDetail(refID)})
				}
				links = append(links, tuicore.ExtractContentLinks(issue.Body, issue.Repository, "")...)
				return links
			},
		}},
	})
	// Comments section
	if len(v.comments) > 0 {
		label := fmt.Sprintf(" Comments (%d)", len(v.comments))
		items := make([]tuicore.SectionItem, 0, len(v.comments))
		for i, comment := range v.comments {
			comment := comment
			isLast := i == len(v.comments)-1
			nextDepth := 0
			if !isLast {
				nextDepth = v.comments[i+1].Depth
			}
			commentID := comment.ID
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					lines := tuisocial.RenderCommentCard(comment, width, selected, searchQuery, v.userEmail, v.showEmail, anchors)
					if !isLast {
						lines = append(lines, "", tuicore.RenderItemSeparator(width, nextDepth), "")
					}
					return lines
				},
				SearchText: func() string { return comment.Content },
				Links: func() []tuicore.CardLink {
					card := tuisocial.PostToCardWithOptions(comment, nil, tuisocial.PostToCardOptions{SkipNested: true, UserEmail: v.userEmail, ShowEmail: v.showEmail})
					return card.AllLinks()
				},
				OnActivate: func() tea.Cmd {
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocDetail(commentID),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	v.sectionList.SetSections(sections)
}

func (v *IssueDetailView) closeIssue() tea.Cmd {
	issueID := v.issue.ID
	return tea.Sequence(
		func() tea.Msg {
			result := pm.CloseIssue("", issueID)
			if !result.Success {
				return IssueDetailLoadedMsg{Err: fmt.Errorf("close failed: %s", result.Error.Message)}
			}
			return nil
		},
		v.loadIssue(),
	)
}

func (v *IssueDetailView) doRetract() tea.Cmd {
	issueID := v.issue.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.RetractIssue(workdir, issueID)
		if !result.Success {
			return IssueRetractedMsg{ID: issueID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return IssueRetractedMsg{ID: issueID}
	}
}

// navigateSource navigates to adjacent items in the source list.
func (v *IssueDetailView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocPMIssueDetail}
	}
}

// IsInputActive returns true when confirmation or search input is active.
func (v *IssueDetailView) IsInputActive() bool {
	return v.confirm.IsActive() || v.sectionList.IsInputActive()
}

// Render renders the issue detail view.
func (v *IssueDetailView) Render(state *tuicore.State) string {
	if v.issue != nil && v.issue.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading issue..."
	} else if v.issue == nil {
		content = tuicore.Dim.Render("  Issue not found")
	} else {
		content = v.sectionList.View()
	}
	exclude := map[string]bool{}
	if v.issue == nil || !v.issue.IsEdited {
		exclude["h"] = true
	}
	if v.milestone == nil {
		exclude["m"] = true
	}
	if v.sprint == nil {
		exclude["s"] = true
	}
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.PMIssueDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

func (v *IssueDetailView) renderIssueCard(issue *pm.Issue, milestone *pm.Milestone, sprint *pm.Sprint, contributorNames map[string]string, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	var lines []string
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	title := issue.Subject
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, selectionBar+tuicore.Bold.Render(title))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	styles := tuicore.RowStylesWithWidths(14, 0)
	stateStr := string(issue.State)
	switch issue.State {
	case pm.StateOpen:
		stateStr = tuicore.Title.Render("open")
	case pm.StateClosed:
		stateStr = tuicore.Dim.Render("closed")
	case pm.StateCancelled:
		stateStr = tuicore.Dim.Render("canceled")
	}
	lines = append(lines, selectionBar+styles.Label.Render("State")+stateStr)
	lines = append(lines, tuicore.RenderOriginRows(issue.Origin, styles, selectionBar, anchors, v.showEmail)...)
	if len(issue.Assignees) > 0 {
		lines = append(lines, selectionBar+styles.Label.Render("Assignees")+styles.Value.Render(formatAssignees(issue.Assignees, contributorNames)))
	}
	if issue.Due != nil {
		lines = append(lines, selectionBar+styles.Label.Render("Due")+styles.Value.Render(issue.Due.Format("Jan 2, 2006")))
	}
	if milestone != nil {
		milestoneStr := milestone.Title
		if milestone.Due != nil {
			milestoneStr += tuicore.Dim.Render("  due " + milestone.Due.Format("Jan 2, 2006"))
		}
		parsed := protocol.ParseRef(milestone.ID)
		commitURL := protocol.CommitURL(parsed.Repository, parsed.Value)
		milestoneDisplay := anchors.MarkLink(milestoneStr, commitURL, tuicore.LocPMMilestoneDetail(milestone.ID))
		lines = append(lines, selectionBar+styles.Label.Render("Milestone")+milestoneDisplay)
	}
	if sprint != nil {
		sprintStr := sprint.Title
		sprintStr += tuicore.Dim.Render("  " + sprint.Start.Format("Jan 2") + " - " + sprint.End.Format("Jan 2, 2006"))
		parsed := protocol.ParseRef(sprint.ID)
		commitURL := protocol.CommitURL(parsed.Repository, parsed.Value)
		sprintDisplay := anchors.MarkLink(sprintStr, commitURL, tuicore.LocPMSprintDetail(sprint.ID))
		lines = append(lines, selectionBar+styles.Label.Render("Sprint")+sprintDisplay)
	}
	lines = append(lines, renderLinkRows("Blocks", issue.Blocks, selectionBar, styles, anchors)...)
	lines = append(lines, renderLinkRows("Blocked by", issue.BlockedBy, selectionBar, styles, anchors)...)
	lines = append(lines, renderLinkRows("Related", issue.Related, selectionBar, styles, anchors)...)
	if len(issue.Labels) > 0 {
		for i, l := range issue.Labels {
			label := l.Value
			if l.Scope != "" {
				label = l.Scope + "/" + l.Value
			}
			rowLabel := "Labels"
			if i > 0 {
				rowLabel = ""
			}
			lines = append(lines, selectionBar+styles.Label.Render(rowLabel)+styles.Value.Render(label))
		}
	}
	ref := protocol.ParseRef(issue.ID)
	if trailerRefs, err := cache.GetTrailerRefsTo(ref.Repository, ref.Value, ref.Branch); err == nil && len(trailerRefs) > 0 {
		for i, tr := range trailerRefs {
			rowLabel := "Referenced by"
			if i > 0 {
				rowLabel = ""
			}
			subject, _ := protocol.SplitSubjectBody(tr.Message)
			display := subject + tuicore.Dim.Render("  "+tr.Hash[:12]+"  "+tr.TrailerKey)
			lines = append(lines, selectionBar+styles.Label.Render(rowLabel)+styles.Value.Render(display))
		}
	}
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if v.showRaw {
		lines = append(lines, tuicore.RenderCommitMessage(issue.ID, selectionBar, width-3)...)
	} else if issue.Body != "" {
		for _, line := range strings.Split(tuicore.RenderMarkdownWithAnchors(issue.Body, width-3, anchors), "\n") {
			if searchQuery != "" {
				line = tuicore.HighlightInText(line, searchQuery)
			}
			lines = append(lines, selectionBar+line)
		}
	} else {
		lines = append(lines, selectionBar+tuicore.Dim.Render("No description"))
	}
	return lines
}

// renderLinkRows renders metadata rows for issue links (blocks, blocked-by, related).
func renderLinkRows(label string, refs []pm.IssueRef, selectionBar string, styles tuicore.RowStyles, anchors *tuicore.AnchorCollector) []string {
	if len(refs) == 0 {
		return nil
	}
	var lines []string
	for i, ref := range refs {
		rowLabel := label
		if i > 0 {
			rowLabel = ""
		}
		refID := protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch)
		display := protocol.FormatShortRef(refID, "")
		// Try to load the issue subject for a friendlier display
		if item, err := pm.GetPMItem(ref.RepoURL, ref.Hash, ref.Branch); err == nil {
			subject, _ := protocol.SplitSubjectBody(protocol.ExtractCleanContent(item.Content))
			if subject != "" {
				stateIndicator := ""
				if item.State == string(pm.StateClosed) {
					stateIndicator = tuicore.Dim.Render(" [closed]")
				}
				display = subject + stateIndicator + tuicore.Dim.Render("  "+protocol.FormatShortRef(refID, ""))
			}
		}
		commitURL := protocol.CommitURL(ref.RepoURL, ref.Hash)
		styledDisplay := anchors.MarkLink(display, commitURL, tuicore.LocPMIssueDetail(refID))
		lines = append(lines, selectionBar+styles.Label.Render(rowLabel)+styledDisplay)
	}
	return lines
}

// ShowRawView toggles between rendered body and full commit message.
func (v *IssueDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return func() tea.Msg { return nil }
}

// Title returns the view title.
func (v *IssueDetailView) Title() string {
	if v.issue == nil {
		return "○  Issue"
	}
	author := v.issue.Author.Name
	if v.showEmail && v.issue.Author.Email != "" {
		author += " <" + v.issue.Author.Email + ">"
	}
	if v.issue.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(v.issue.Origin, v.showEmail); a != "" {
			author = a
		}
	}
	timestamp := tuicore.FormatTime(v.issue.Timestamp)
	if v.issue.Origin != nil && v.issue.Origin.Time != "" {
		timestamp = tuicore.FormatOriginTime(v.issue.Origin.Time)
	}
	id := protocol.FormatShortRef(v.issue.ID, v.workspaceURL)
	icon := "○"
	if v.issue.IsUnpushed {
		icon += "  ⇡"
	}
	return fmt.Sprintf("%s  %s · %s · %s · %s", icon, tuicore.TruncateToWidth(v.issue.Subject, 40), author, timestamp, id)
}

// Bindings returns keybindings for this view.
func (v *IssueDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "m", Label: "milestone", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "s", Label: "sprint", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: tuicore.RawViewHandler},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "C", Label: "close", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMIssueDetail}, Handler: push},
	}
}

// ViewName returns the view identifier.
func (v *IssueDetailView) ViewName() string {
	return "pm.issue_detail"
}

// buildContributorNameMap builds an email-to-name map from all cached commits.
func buildContributorNameMap(workdir string) map[string]string {
	all, err := cache.GetAllContributors()
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(all))
	for _, c := range all {
		if c.Name != "" {
			m[c.Email] = c.Name
		}
	}
	repoURL := gitmsg.ResolveRepoURL(workdir)
	if repo, err := cache.GetContributors(repoURL); err == nil {
		for _, c := range repo {
			if c.Name != "" {
				m[c.Email] = c.Name
			}
		}
	}
	return m
}

// formatAssignees formats assignee emails with names when available.
func formatAssignees(assignees []string, nameMap map[string]string) string {
	parts := make([]string, len(assignees))
	for i, email := range assignees {
		if nameMap != nil {
			if name, ok := nameMap[email]; ok {
				parts[i] = name + "  " + tuicore.Dim.Render(email)
				continue
			}
		}
		parts[i] = email
	}
	return strings.Join(parts, ", ")
}
