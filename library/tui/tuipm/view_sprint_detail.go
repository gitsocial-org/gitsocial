// sprint_detail.go - Sprint detail view with progress, backlog, and comments
package tuipm

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/social"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/tui/tuisocial"
)

// SprintDetailView displays a single sprint with backlog and comments.
type SprintDetailView struct {
	workdir      string
	width        int
	height       int
	sprintID     string
	sprint       *pm.Sprint
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

// NewSprintDetailView creates a new sprint detail view.
func NewSprintDetailView(workdir string) *SprintDetailView {
	return &SprintDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// SetSize sets the view dimensions.
func (v *SprintDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
	v.sectionList.SetSize(w, h-3)
}

// Activate loads the sprint details.
func (v *SprintDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.confirm.Reset()
	v.sprintID = state.Router.Location().Param("sprintID")
	v.focusID = state.Router.Location().Param("focusID")
	v.loaded = false
	v.sprint = nil
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
	return v.loadSprint()
}

func (v *SprintDetailView) loadSprint() tea.Cmd {
	sprintID := v.sprintID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.GetSprint(sprintID)
		if !result.Success {
			return SprintDetailLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		branch := gitmsg.GetExtBranch(workdir, "pm")
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		ref := protocol.ParseRef(result.Data.ID)
		if _, ok := unpushed[ref.Value]; ok {
			result.Data.IsUnpushed = true
		}
		issueResult := pm.GetSprintIssues(result.Data.ID, []string{string(pm.StateOpen), string(pm.StateClosed)})
		var issues []pm.Issue
		if issueResult.Success {
			issues = issueResult.Data
		}
		commentsResult := pm.GetItemComments(result.Data.ID, "")
		var comments []social.Post
		if commentsResult.Success {
			comments = commentsResult.Data
		}
		return SprintDetailLoadedMsg{Sprint: result.Data, Issues: issues, Comments: comments}
	}
}

// SprintDetailLoadedMsg signals sprint details loaded.
type SprintDetailLoadedMsg struct {
	Sprint   pm.Sprint
	Issues   []pm.Issue
	Comments []social.Post
	Err      error
}

// Update handles messages.
func (v *SprintDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case SprintDetailLoadedMsg:
		v.loaded = true
		if msg.Err == nil {
			v.sprint = &msg.Sprint
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
				if v.sprint != nil {
					sprintID := v.sprint.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMEditSprint(sprintID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "h":
				if v.sprint != nil && v.sprint.IsEdited {
					sprintID := v.sprint.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.LocPMSprintHistory(sprintID),
							Action:   tuicore.NavPush,
						}
					}
				}
			case "c":
				if v.sprint != nil {
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{
							Mode:     "comment",
							TargetID: v.sprint.ID,
						}
					}
				}
			case "D":
				if v.sprint != nil && v.sprint.State == pm.SprintStateActive {
					return v.completeSprint()
				}
			case "X":
				if v.sprint != nil {
					v.confirm.Show("Retract this sprint?", false, func() tea.Cmd { return v.doRetract() })
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
func (v *SprintDetailView) navigateSource(state *tuicore.State, offset int) tea.Cmd {
	if state.DetailSource == nil {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocPMSprintDetail}
	}
}

// IsInputActive returns true when confirmation or search input is active.
func (v *SprintDetailView) IsInputActive() bool {
	return v.confirm.IsActive() || v.sectionList.IsInputActive()
}

func (v *SprintDetailView) buildSections() {
	var sections []tuicore.Section
	// Hero section (no label) — the sprint card
	sp := v.sprint
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderSprintCard(sp, width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return sp.Title + " " + sp.Body },
			Links: func() []tuicore.CardLink {
				var links []tuicore.CardLink
				if sp.Origin != nil && sp.Origin.URL != "" {
					links = append(links, tuicore.CardLink{Label: "Source", Location: tuicore.Location{Path: sp.Origin.URL}})
				}
				links = append(links, tuicore.ExtractContentLinks(sp.Body, sp.Repository, "")...)
				return links
			},
		}},
	})
	// Backlog section
	if len(v.issues) > 0 {
		label := fmt.Sprintf(" Sprint Backlog (%d)", len(v.issues))
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

func (v *SprintDetailView) completeSprint() tea.Cmd {
	sprintID := v.sprint.ID
	return func() tea.Msg {
		result := pm.CompleteSprint("", sprintID)
		if !result.Success {
			return SprintDetailLoadedMsg{Err: fmt.Errorf("complete failed: %s", result.Error.Message)}
		}
		return SprintDetailLoadedMsg{Sprint: result.Data, Issues: nil, Comments: nil}
	}
}

func (v *SprintDetailView) doRetract() tea.Cmd {
	sprintID := v.sprint.ID
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.RetractSprint(workdir, sprintID)
		if !result.Success {
			return SprintRetractedMsg{ID: sprintID, Err: fmt.Errorf("%s", result.Error.Message)}
		}
		return SprintRetractedMsg{ID: sprintID}
	}
}

// Render renders the sprint detail view.
func (v *SprintDetailView) Render(state *tuicore.State) string {
	if v.sprint != nil && v.sprint.IsRetracted {
		state.BorderVariant = "warning"
	}
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = "Loading sprint..."
	} else if v.sprint == nil {
		content = tuicore.Dim.Render("  Sprint not found")
	} else {
		content = v.sectionList.View()
	}
	exclude := map[string]bool{}
	if v.sprint == nil || !v.sprint.IsEdited {
		exclude["h"] = true
	}
	if v.sprint == nil || v.sprint.State != pm.SprintStateActive {
		exclude["D"] = true
	}
	var footer string
	if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else {
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.PMSprintDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

func (v *SprintDetailView) renderSprintCard(sp *pm.Sprint, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	var lines []string
	selectionBar := " "
	if selected {
		selectionBar = tuicore.Title.Render("▏")
	}
	title := sp.Title
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, selectionBar+tuicore.Bold.Render(title))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	styles := tuicore.RowStylesWithWidths(14, 0)
	stateStr := string(sp.State)
	switch sp.State {
	case pm.SprintStatePlanned:
		stateStr = tuicore.Title.Render("planned")
	case pm.SprintStateActive:
		stateStr = tuicore.Title.Render("active")
	case pm.SprintStateCompleted:
		stateStr = tuicore.Dim.Render("completed")
	case pm.SprintStateCancelled:
		stateStr = tuicore.Dim.Render("canceled")
	}
	lines = append(lines, selectionBar+styles.Label.Render("State")+stateStr)
	lines = append(lines, tuicore.RenderOriginRows(sp.Origin, styles, selectionBar, anchors, v.showEmail)...)
	total := sp.IssueCount
	closed := sp.ClosedCount
	progressBar := tuicore.RenderProgressBar(closed, total, 16)
	lines = append(lines, selectionBar+styles.Label.Render("Progress")+styles.Value.Render(progressBar))
	if sp.State == pm.SprintStateActive {
		daysLeft := int(time.Until(sp.End).Hours() / 24)
		if daysLeft < 0 {
			daysLeft = 0
		}
		lines = append(lines, selectionBar+styles.Label.Render("Days left")+styles.Value.Render(fmt.Sprintf("%d", daysLeft)))
	}
	dateRange := fmt.Sprintf("%s - %s", sp.Start.Format("Jan 2"), sp.End.Format("Jan 2, 2006"))
	lines = append(lines, selectionBar+styles.Label.Render("Dates")+styles.Value.Render(dateRange))
	lines = append(lines, selectionBar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if v.showRaw {
		lines = append(lines, tuicore.RenderCommitMessage(sp.ID, selectionBar, width-3)...)
	} else if sp.Body != "" {
		for _, line := range strings.Split(tuicore.RenderMarkdownWithAnchors(sp.Body, width-3, anchors), "\n") {
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

func (v *SprintDetailView) renderIssueRow(issue pm.Issue, width int, selected bool, searchQuery string) []string {
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
func (v *SprintDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	return func() tea.Msg { return nil }
}

// Title returns the view title.
func (v *SprintDetailView) Title() string {
	if v.sprint == nil {
		return "◷  Sprint"
	}
	author := v.sprint.Author.Name
	if v.showEmail && v.sprint.Author.Email != "" {
		author += " <" + v.sprint.Author.Email + ">"
	}
	if v.sprint.Origin != nil {
		if a := tuicore.FormatOriginAuthorDisplay(v.sprint.Origin, v.showEmail); a != "" {
			author = a
		}
	}
	timestamp := tuicore.FormatTime(v.sprint.Timestamp)
	if v.sprint.Origin != nil && v.sprint.Origin.Time != "" {
		timestamp = tuicore.FormatOriginTime(v.sprint.Origin.Time)
	}
	id := protocol.FormatShortRef(v.sprint.ID, v.workspaceURL)
	icon := "◷"
	if v.sprint.IsUnpushed {
		icon += "  ⇡"
	}
	return fmt.Sprintf("%s  %s · %s · %s · %s", icon, tuicore.TruncateToWidth(v.sprint.Title, 40), author, timestamp, id)
}

// HeaderInfo returns position info.
func (v *SprintDetailView) HeaderInfo() (position, total int) {
	return 0, 0
}

// Bindings returns keybindings for this view.
func (v *SprintDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: tuicore.RawViewHandler},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMSprintDetail}, Handler: push},
	}
}

// ViewName returns the view identifier.
func (v *SprintDetailView) ViewName() string {
	return "pm.sprint_detail"
}
