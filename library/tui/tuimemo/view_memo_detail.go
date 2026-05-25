// view_memo_detail.go - Memo detail view with sectionList (memo card + comments)
package tuimemo

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
	"github.com/gitsocial-org/gitsocial/library/tui/tuisocial"
)

// MemoDetailView displays a single memo plus its comments, mirroring the post
// detail layout so navigation, search, and key handling stay consistent.
type MemoDetailView struct {
	workdir      string
	workspaceURL string
	userEmail    string
	width        int
	height       int
	loaded       bool
	memo         *memo.Memo
	comments     []social.Post
	showEmail    bool
	showRaw      bool
	confirm      tuicore.ConfirmDialog
	sectionList  *tuicore.SectionList
	sourceIndex  int
	sourceTotal  int
}

// ShowRawView toggles between rendered body and the full commit message.
func (v *MemoDetailView) ShowRawView() tea.Cmd {
	v.showRaw = !v.showRaw
	v.buildSections()
	return func() tea.Msg { return nil }
}

// NewMemoDetailView creates a new memo detail view.
func NewMemoDetailView(workdir string) *MemoDetailView {
	return &MemoDetailView{
		workdir:      workdir,
		userEmail:    git.GetUserEmail(workdir),
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
		sectionList:  tuicore.NewSectionList(),
	}
}

// Title mirrors the post detail header: icon, author, full timestamp, scope, ref.
func (v *MemoDetailView) Title() string {
	if v.memo == nil {
		return "Memo"
	}
	m := v.memo
	name := m.Author.Name
	if name == "" {
		name = "Anonymous"
	}
	if v.showEmail && m.Author.Email != "" {
		name += " <" + m.Author.Email + ">"
	}
	title := "☞  " + name
	if !m.Timestamp.IsZero() {
		title += " · " + tuicore.FormatFullTime(m.Timestamp)
	}
	if m.Tier != "" {
		title += " · [" + string(m.Tier) + "]"
	}
	isWorkspace := v.workspaceURL != "" && m.Repository == v.workspaceURL
	if ref := tuicore.BuildRef(m.ID, m.Repository, m.Branch, isWorkspace); ref != "" {
		title += " · " + ref
	}
	return title
}

// HeaderInfo returns position info for left/right list navigation.
func (v *MemoDetailView) HeaderInfo() (int, string) {
	if v.sourceTotal > 0 {
		return v.sourceIndex + 1, fmt.Sprintf("%d", v.sourceTotal)
	}
	return 0, ""
}

// SetSize sets the panel dimensions.
func (v *MemoDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.sectionList.SetSize(w, h-2)
}

// Activate (re)loads the memo and its comments for the current route.
func (v *MemoDetailView) Activate(state *tuicore.State) tea.Cmd {
	v.showEmail = state.ShowEmailOnCards
	v.workspaceURL = gitmsg.ResolveRepoURL(v.workdir)
	v.loaded = false
	v.memo = nil
	v.comments = nil
	if state.DetailSource != nil {
		v.sourceIndex = state.DetailSource.Index
		v.sourceTotal = state.DetailSource.Total
	} else {
		v.sourceIndex = 0
		v.sourceTotal = 0
	}
	id := state.Router.Location().Params["memoID"]
	if id == "" {
		return nil
	}
	workspaceURL := v.workspaceURL
	workdir := v.workdir
	return func() tea.Msg {
		res := memo.GetSingleMemo(id, workspaceURL, memo.ListInherits(workdir))
		if !res.Success {
			return memoDetailLoadedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		m := res.Data
		var comments []social.Post
		if c := memo.GetMemoComments(m.ID, workspaceURL); c.Success {
			comments = c.Data
		}
		return memoDetailLoadedMsg{memo: &m, comments: comments}
	}
}

// IsInputActive surfaces section-list search input so global keys defer.
func (v *MemoDetailView) IsInputActive() bool {
	return v.sectionList.IsInputActive()
}

// Update routes messages: load events, key dispatch, confirm dialog, sectionList.
func (v *MemoDetailView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case memoDetailLoadedMsg:
		v.loaded = true
		if msg.err != nil {
			return nil
		}
		v.memo = msg.memo
		v.comments = msg.comments
		v.buildSections()
		return nil
	case memoRetractedMsg:
		if msg.err != nil {
			return nil
		}
		return func() tea.Msg { return tuicore.NavigateMsg{Action: tuicore.NavBack} }
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
				return v.navigateSource(-1)
			case "right":
				return v.navigateSource(1)
			case "c":
				if v.memo != nil {
					id := v.memo.ID
					return func() tea.Msg {
						return tuicore.OpenEditorMsg{Mode: "comment", TargetID: id}
					}
				}
			case "e":
				if v.memo != nil {
					if v.memo.Tier == memo.TierExternal || v.memo.Tier == memo.TierInherited {
						return readOnlyMsg(state)
					}
					id := v.memo.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{
							Location: tuicore.Location{Path: "/memo/edit", Params: map[string]string{"memoID": id}},
							Action:   tuicore.NavPush,
						}
					}
				}
			case "X":
				if v.memo != nil {
					if v.memo.Tier == memo.TierExternal || v.memo.Tier == memo.TierInherited {
						return readOnlyMsg(state)
					}
					v.confirm.Show("Retract this memo?", false, func() tea.Cmd { return v.doRetract() })
					return nil
				}
			case "h":
				if v.memo != nil {
					id := v.memo.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{Location: tuicore.LocMemoHistory(id), Action: tuicore.NavPush}
					}
				}
			}
		}
	}
	if v.sectionList.IsInputActive() {
		return v.sectionList.UpdateSearchInput(msg)
	}
	return nil
}

// Bindings returns the view's keybindings.
func (v *MemoDetailView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "c", Label: "comment", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "e", Label: "edit", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "X", Label: "retract", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "h", Label: "history", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "v", Label: "raw", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: tuicore.RawViewHandler},
		{Key: "/", Label: "search", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "left", Label: "prev", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
		{Key: "right", Label: "next", Contexts: []tuicore.Context{tuicore.MemoDetail}, Handler: noop},
	}
}

// Render renders memo card + comments via the section list.
func (v *MemoDetailView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.sectionList.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())

	var content string
	if !v.loaded {
		content = "Loading memo..."
	} else if v.memo == nil {
		content = tuicore.Dim.Render("memo not found")
	} else {
		content = v.sectionList.View()
	}

	exclude := map[string]bool{}
	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else if v.sectionList.IsSearchActive() {
		footer = v.sectionList.SearchFooter(wrapper.ContentWidth())
	} else {
		footer = tuicore.RenderFooterWithPosition(state.Registry, tuicore.MemoDetail, wrapper.ContentWidth(), v.sourceIndex+1, v.sourceTotal, exclude)
	}
	return wrapper.Render(content, footer)
}

// buildSections lays out the memo card and any comments as section-list sections.
func (v *MemoDetailView) buildSections() {
	if v.memo == nil {
		return
	}
	m := v.memo
	var sections []tuicore.Section
	sections = append(sections, tuicore.Section{
		Items: []tuicore.SectionItem{{
			Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
				return v.renderMemoCard(m, width, selected, searchQuery, anchors)
			},
			SearchText: func() string { return m.Subject + " " + m.Body },
			Links: func() []tuicore.CardLink {
				return tuicore.ExtractContentLinks(m.Body, m.Repository, "")
			},
		}},
	})
	if len(v.comments) > 0 {
		label := fmt.Sprintf(" Comments (%d)", len(v.comments))
		items := make([]tuicore.SectionItem, 0, len(v.comments))
		for i, c := range v.comments {
			c := c
			isLast := i == len(v.comments)-1
			nextDepth := 0
			if !isLast {
				nextDepth = v.comments[i+1].Depth
			}
			items = append(items, tuicore.SectionItem{
				Render: func(width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
					lines := tuisocial.RenderCommentCard(c, width, selected, searchQuery, v.userEmail, v.showEmail, anchors)
					if !isLast {
						lines = append(lines, "", tuicore.RenderItemSeparator(width, nextDepth), "")
					}
					return lines
				},
				SearchText: func() string { return c.Content },
				Links: func() []tuicore.CardLink {
					card := tuisocial.PostToCardWithOptions(c, nil, tuisocial.PostToCardOptions{SkipNested: true, UserEmail: v.userEmail, ShowEmail: v.showEmail})
					return card.AllLinks()
				},
				OnActivate: func() tea.Cmd {
					id := c.ID
					return func() tea.Msg {
						return tuicore.NavigateMsg{Location: tuicore.LocDetail(id), Action: tuicore.NavPush}
					}
				},
			})
		}
		sections = append(sections, tuicore.Section{Label: label, Items: items})
	}
	v.sectionList.SetSections(sections)
}

// renderMemoCard renders the hero memo card: title, body, labels. When the
// raw-view toggle is on, the rendered body is replaced with the full commit
// message (parsable trailer included) so users can inspect the protocol form.
// Anchors are threaded through so markdown links can register as clickable.
func (v *MemoDetailView) renderMemoCard(m *memo.Memo, width int, selected bool, searchQuery string, anchors *tuicore.AnchorCollector) []string {
	var lines []string
	bar := " "
	if selected {
		bar = tuicore.Title.Render("▏")
	}
	title := m.Subject
	if searchQuery != "" {
		title = tuicore.HighlightInText(title, searchQuery)
	}
	lines = append(lines, bar+tuicore.Bold.Render(title))
	lines = append(lines, bar+tuicore.Dim.Render(strings.Repeat("─", width-3)))
	if len(m.Labels) > 0 {
		lines = append(lines, bar+tuicore.Dim.Render(strings.Join(m.Labels, " · ")))
	}
	if m.IsEdited {
		lines = append(lines, bar+tuicore.Dim.Render("(edited)"))
	}
	if v.showRaw {
		lines = append(lines, bar)
		lines = append(lines, tuicore.RenderCommitMessage(m.ID, bar, width-3)...)
		return lines
	}
	if m.Body != "" {
		lines = append(lines, bar)
		rendered := tuicore.RenderMarkdownWithAnchors(m.Body, width-3, anchors)
		for _, line := range strings.Split(rendered, "\n") {
			if searchQuery != "" {
				line = tuicore.HighlightInText(line, searchQuery)
			}
			lines = append(lines, bar+line)
		}
	}
	return lines
}

// readOnlyMsg surfaces a "this memo is read-only" status when the user
// attempts to edit/retract an inherited- or external-tier memo.
func readOnlyMsg(state *tuicore.State) tea.Cmd {
	state.SetMessage("inherited and external memos are read-only", tuicore.MessageTypeWarning)
	return nil
}

// navigateSource moves to the prev/next memo in the source list (e.g., /memo/list).
func (v *MemoDetailView) navigateSource(offset int) tea.Cmd {
	if v.sourceTotal == 0 {
		return nil
	}
	target := v.sourceIndex + offset
	if target < 0 || target >= v.sourceTotal {
		return nil
	}
	return func() tea.Msg {
		return tuicore.SourceNavigateMsg{Offset: offset, MakeLocation: tuicore.LocMemoDetail}
	}
}

// doRetract calls memo.RetractMemo and emits a memoRetractedMsg.
func (v *MemoDetailView) doRetract() tea.Cmd {
	if v.memo == nil {
		return nil
	}
	id := v.memo.ID
	workdir := v.workdir
	return func() tea.Msg {
		res := memo.RetractMemo(workdir, id)
		if !res.Success {
			return memoRetractedMsg{err: fmt.Errorf("%s", res.Error.Message)}
		}
		return memoRetractedMsg{}
	}
}

type memoDetailLoadedMsg struct {
	memo     *memo.Memo
	comments []social.Post
	err      error
}

type memoRetractedMsg struct {
	err error
}
