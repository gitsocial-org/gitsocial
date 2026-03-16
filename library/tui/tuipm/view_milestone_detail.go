// milestone_detail.go - Milestone detail view with progress, issues, and comments
package tuipm

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// MilestoneDetailView displays a single milestone with issues and comments.
type MilestoneDetailView struct {
	workdir      string
	width        int
	height       int
	milestoneID  string
	milestone    *pm.Milestone
	issues       []pm.Issue
	comments     []social.Post
	loaded       bool
	userEmail    string
	showEmail    bool
	workspaceURL string
	focusID      string
	showRaw      bool
	confirm      tuicore.ConfirmDialog
	sectionList  *tuicore.SectionList
	sourceIndex  int
	sourceTotal  int
}

// NewMilestoneDetailView creates a new milestone detail view.
func NewMilestoneDetailView(workdir string) *MilestoneDetailView {
	return &MilestoneDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *MilestoneDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the milestone details.
func (v *MilestoneDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.confirm.Reset()
	v.milestoneID = state.Router.Location().Param("milestoneID")
	v.focusID = state.Router.Location().Param("focusID")
	v.loaded = false
	v.milestone = nil
	v.issues = nil
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
	return v.loadMilestone()
}

func (v *MilestoneDetailView) loadMilestone() tea.Cmd {
	milestoneID := v.milestoneID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.GetMilestone(milestoneID)
		if !result.Success {
			return MilestoneDetailLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		branch := gitmsg.GetExtBranch(workdir, "pm")
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		ref := protocol.ParseRef(result.Data.ID)
		if _, ok := unpushed[ref.Value]; ok {
			result.Data.IsUnpushed = true
		}
		issueResult := pm.GetMilestoneIssues(result.Data.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
		var issues []pm.Issue
		if issueResult.Success {
			issues = issueResult.Data
		}
		commentsResult := pm.GetItemComments(result.Data.ID, "")
		var comments []social.Post
		if commentsResult.Success {
			comments = commentsResult.Data
		}
		return MilestoneDetailLoadedMsg{Milestone: result.Data, Issues: issues, Comments: comments}
	}
}

// MilestoneDetailLoadedMsg signals milestone details loaded.
type MilestoneDetailLoadedMsg struct {
	Milestone pm.Milestone
	Issues    []pm.Issue
	Comments  []social.Post
	Err       error
}

// Update handles messages.
func (v *MilestoneDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case MilestoneDetailLoadedMsg:
		v.loaded = true
		if msg.Err == nil {
			v.milestone = &msg.Milestone
			v.issues = msg.Issues
			for i := range msg.Comments {
				if msg.Comments[i].Repository == v.workspaceURL {
					msg.Comments[i].Display.IsWorkspacePost = true
				}
			}
			v.comments = msg.Comments
			v.buildSections()
			if v.focusID != "" {
				for i, c := range v.comments {
					if c.ID == v.focusID {
						v.sectionList.SetSelected(1 + len(v.issues) + i)
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
			case "left":
				return v.navigateSource(state, -1)
			case "right":
				return v.navigateSource(state, 1)
			case "e":
				if v.milestone != nil {
					milestoneID := v.milestone.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMEditMilestone(milestoneID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "h":
				if v.milestone != nil && v.milestone.IsEdited {
					milestoneID := v.milestone.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMMilestoneHistory(milestoneID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "c":
				if v.milestone != nil {
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{
							Mode:     "comment",
							TargetID: v.milestone.ID,
						}
					}
				}
			case "C":
				if v.milestone != nil && v.milestone.State == pm.StateOpen {
					return v.closeMilestone()
				}
			case "X":
				if v.milestone != nil {
					v.confirm.Show("Retract this milestone?", false, func() tea.Cmd { return v.doRetract() })
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

// navigateSource navigates to adjacent items in the source list.
func (v *MilestoneDetailView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocPMMilestoneDetail}
	}
}

// IsInputActive returns true when confirmation or search input is active.
func (v *MilestoneDetailView) IsInputActive() bool {
	return v.confirm.IsActive() || v.sectionList.IsInputActive()
}

func (v *MilestoneDetailView) buildSections() {
	var sections []tuicore.Section
	// Hero section (no label) — the milestone card
	ms := v.milestone
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderMilestoneCard(ms, width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return ms.Title + " " + ms.Body },
			Links: func() []tuicore.CardLink {
				var links []tuicore.CardLink
				if ms.Origin != nil && ms.Origin.URL != "" {
					links = append(links, tuicore.CardLink{Label: "Source", Location: tuicore.Location{Path: ms.Origin.URL}})
				}
				links = append(links, tuicore.ExtractContentLinks(ms.Body, ms.Repository, "")...)
				return links
			},
		}},
	})
	// Issues section
	if len(v.issues) > 0 {
		label := fmt.Sprintf(" Linked Issues (%d)", len(v.issues))
		items := make([]tuicore.SectionItem, 0, len(v.issues))
		for _, issue := range v.issues {
			issue := issue
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, _ *tuicore.AnchorCollector) []string {
					return v.renderIssueRow(issue, width, selected, searchQuery)
				},
				SearchText: func() string { return issue.Subject },
				OnActivate: func() tea.Cmd {
					issueID := issue.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMIssueDetail(issueID),
							Action:   tuicore.NavPush,
						}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
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
					commentID := comment.ID
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

func (v *MilestoneDetailView) closeMilestone() tea.Cmd {
	milestoneID := v.milestone.ID
	return func() tea.Msg {
		result := pm.CloseMilestone("", milestoneID)
		if !result.Success {
			return MilestoneDetailLoadedMsg{Err: fmt.Errorf("close failed: %s", result.Error.Message)}
		}
		return MilestoneDetailLoadedMsg{Milestone: result.Data, Issues: nil, Comments: nil}
	}
}

func (v *MilestoneDetailView) doRetract() tea.Cmd {
	milestoneID := v.milestone.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.RetractMilestone(workdir, milestoneID)
		if !result.Success {
			return MilestoneRetractedMsg{ID: milestoneID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return MilestoneRetractedMsg{ID: milestoneID}
	}
}

// Render renders the milestone detail view.
func (v *MilestoneDetailView) Render(state *tuicore.State) string {
	if v.milestone != nil && v.milestone.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading milestone..."
	} else if v.milestone == nil {
		content = tuicore.Dim.Render("  Milestone not found")
	} else {
		content = v.sectionList.View()
	}
	exclude := map[string]bool{}
	if v.milestone == nil || !v.milestone.IsEdited {
		exclude["h"] = true
	}
	if v.milestone == nil || v.milestone.State != pm.StateOpen {
		exclude["D"] = true
	}
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.PMMilestoneDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

func (v *MilestoneDetailView) renderMilestoneCard(ms *pm.Milestone, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	var lines []string
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	title := ms.Title
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, selectionBar+tuicore.Bold.Render(title))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	styles := tuicore.RowStylesWithWidths(14, 0)
	stateStr := string(ms.State)
	switch ms.State {
	case pm.StateOpen:
		stateStr = tuicore.Title.Render("open")
	case pm.StateClosed:
		stateStr = tuicore.Dim.Render("closed")
	case pm.StateCancelled:
		stateStr = tuicore.Dim.Render("canceled")
	}
	lines = append(lines, selectionBar+styles.Label.Render("State")+stateStr)
	lines = append(lines, tuicore.RenderOriginRows(ms.Origin, styles, selectionBar, anchors, v.showEmail)...)
	if ms.Due != nil {
		lines = append(lines, selectionBar+styles.Label.Render("Due")+styles.Value.Render(ms.Due.Format("Jan 2, 2006")))
	}
	progressBar := tuicore.RenderProgressBar(ms.ClosedCount, ms.IssueCount, 16)
	lines = append(lines, selectionBar+styles.Label.Render("Progress")+styles.Value.Render(progressBar))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if v.showRaw {
		lines = append(lines, tuicore.RenderCommitMessage(ms.ID, selectionBar, width-3)...)
	} else if ms.Body != "" {
		for _, line := range strings.Split(tuicore.RenderMarkdownWithAnchors(ms.Body, width-3, anchors), "\n") {
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

func (v *MilestoneDetailView) renderIssueRow(issue pm.Issue, width int, selected bool, searchQuery string) []string {
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	stateIcon := "○"
	if issue.State == pm.StateClosed {
		stateIcon = "●"
	}
	var labelStr string
	if len(issue.Labels) > 0 {
		var labels []string
		for _, l := range issue.Labels {
			if l.Scope != "" {
				labels = append(labels, l.Scope+"/"+l.Value)
			} else {
				labels = append(labels, l.Value)
			}
		}
		labelStr = tuicore.Dim.Render("  " + strings.Join(labels, " "))
	}
	subjectWidth := width - 6 - len(labelStr)
	subject := tuicore.TruncateToWidth(issue.Subject, subjectWidth)
	if searchQuery != "" {
		subject = tuicore.HighlightInText(subject, searchQuery)
	}
	line := fmt.Sprintf("%s%s %s%s", selectionBar, stateIcon, subject, labelStr)
	return []string{line}
}

// ShowRawView toggles between rendered body and full commit message.
func (v *MilestoneDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return func() tea.Msg { return nil }
}

// Title returns the view title.
func (v *MilestoneDetailView) Title() string {
	if v.milestone == nil {
		return "◇  Milestone"
	}
	author := v.milestone.Author.Name
	if v.showEmail && v.milestone.Author.Email != "" {
		author += " <" + v.milestone.Author.Email + ">"
	}
	if v.milestone.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(v.milestone.Origin, v.showEmail); a != "" {
			author = a
		}
	}
	timestamp := tuicore.FormatTime(v.milestone.Timestamp)
	if v.milestone.Origin != nil && v.milestone.Origin.Time != "" {
		timestamp = tuicore.FormatOriginTime(v.milestone.Origin.Time)
	}
	id := protocol.FormatShortRef(v.milestone.ID, v.workspaceURL)
	icon := "◇"
	if v.milestone.IsUnpushed {
		icon += "  ⇡"
	}
	return fmt.Sprintf("%s  %s · %s · %s · %s", icon, tuicore.TruncateToWidth(v.milestone.Title, 40), author, timestamp, id)
}

// HeaderInfo returns position info.
func (v *MilestoneDetailView) HeaderInfo() (position, total int) {
	return 0, 0
}

// Bindings returns keybindings for this view.
func (v *MilestoneDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: tuicore.RawViewHandler},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "C", Label: "close", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMMilestoneDetail}, Handler: push},
	}
}

// ViewName returns the view identifier.
func (v *MilestoneDetailView) ViewName() string {
	return "pm.milestone_detail"
}
