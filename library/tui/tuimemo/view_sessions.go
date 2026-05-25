// view_sessions.go - Session picker/manager: list sessions, navigate to one, gc, init.
package tuimemo

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// SessionsView lists every memo session with its id, age, and remote status,
// and lets the user open, init, or gc a session. The TUI never tracks a
// "current" session — selecting a row navigates to that session's memos.
type SessionsView struct {
	workdir   string
	sessions  []memo.SessionInfo
	cursor    int
	width     int
	height    int
	loaded    bool
	input     textinput.Model
	inputMode bool
	confirm   tuicore.ConfirmDialog
}

// NewSessionsView creates a new sessions list/picker view.
func NewSessionsView(workdir string) *SessionsView {
	in := textinput.New()
	in.Placeholder = "session-id"
	in.Prompt = "+ "
	in.CharLimit = 100
	tuicore.StyleTextInput(&in, tuicore.Title, tuicore.Title, tuicore.Dim)
	return &SessionsView{workdir: workdir, input: in}
}

// Title returns the panel header.
func (v *SessionsView) Title() string {
	return fmt.Sprintf("☞  Sessions · %d", len(v.sessions))
}

// HeaderInfo returns the position indicator for the title bar.
func (v *SessionsView) HeaderInfo() (int, string) {
	if len(v.sessions) == 0 {
		return 0, ""
	}
	return v.cursor + 1, fmt.Sprintf("%d", len(v.sessions))
}

// SetSize stores panel dimensions.
func (v *SessionsView) SetSize(w, h int) { v.width, v.height = w, h }

// Activate (re)loads the session list. Cursor is preserved across navigation:
// the previous cursor is clamped to the new list bounds.
func (v *SessionsView) Activate(state *tuicore.State) tea.Cmd {
	prev := v.cursor
	res := memo.ListSessions(gitmsg.ResolveRepoURL(v.workdir))
	if res.Success {
		v.sessions = res.Data
	}
	v.cursor = prev
	if v.cursor >= len(v.sessions) {
		v.cursor = len(v.sessions) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.loaded = true
	v.inputMode = false
	v.input.SetValue("")
	v.input.Blur()
	return nil
}

// IsInputActive reports whether the new-session prompt is taking text input.
func (v *SessionsView) IsInputActive() bool { return v.inputMode }

// Bindings returns the view's keybindings.
func (v *SessionsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "enter", Label: "open", Contexts: []tuicore.Context{tuicore.MemoSessions}, Handler: noop},
		{Key: "n", Label: "new", Contexts: []tuicore.Context{tuicore.MemoSessions}, Handler: noop},
		{Key: "d", Label: "gc", Contexts: []tuicore.Context{tuicore.MemoSessions}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.MemoSessions}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.MemoSessions}, Handler: noop},
	}
}

// Update handles input mode, confirm dialog, and key dispatch.
func (v *SessionsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	if v.inputMode {
		return v.updateInput(msg)
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if handled, cmd := v.confirm.HandleKey(m.String()); handled {
			return cmd
		}
		switch m.String() {
		case "enter":
			if v.cursor < 0 || v.cursor >= len(v.sessions) {
				return nil
			}
			id := v.sessions[v.cursor].ID
			return func() tea.Msg {
				return tuicore.NavigateMsg{Location: tuicore.LocMemoSessionItems(id), Action: tuicore.NavPush}
			}
		case "n":
			v.inputMode = true
			v.input.SetValue("")
			return v.input.Focus()
		case "d", "X":
			if v.cursor < 0 || v.cursor >= len(v.sessions) {
				return nil
			}
			id := v.sessions[v.cursor].ID
			v.confirm.Show("Delete session "+id+"?", true, func() tea.Cmd { return v.doGC(id) })
			return nil
		case "j", "down":
			if v.cursor < len(v.sessions)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g", "home":
			v.cursor = 0
		case "G", "end":
			v.cursor = len(v.sessions) - 1
		}
	}
	return nil
}

func (v *SessionsView) updateInput(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			v.inputMode = false
			v.input.SetValue("")
			v.input.Blur()
			return nil
		case "enter":
			id := strings.TrimSpace(v.input.Value())
			v.inputMode = false
			v.input.Blur()
			if res := memo.InitSession(id, gitmsg.ResolveRepoURL(v.workdir)); res.Success {
				return func() tea.Msg {
					return tuicore.NavigateMsg{Location: tuicore.LocMemoSessionItems(res.Data), Action: tuicore.NavPush}
				}
			}
			return nil
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

func (v *SessionsView) doGC(id string) tea.Cmd {
	return func() tea.Msg {
		_ = memo.GCSession(id)
		return sessionsReloadMsg{}
	}
}

// Render renders the sessions list with metadata.
func (v *SessionsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	v.SetSize(wrapper.ContentWidth(), wrapper.ContentHeight())

	var lines []string
	if !v.loaded {
		lines = append(lines, "Loading...")
	} else if len(v.sessions) == 0 {
		lines = append(lines, tuicore.Dim.Render("(no sessions — press 'n' to create)"))
	} else {
		// Per session: top line is id + meta, followed by up to 3 subjects
		// (dimmed, truncated). The selection bar spans every line so the
		// visual selection is unambiguous.
		//
		// Column alignment: every column-2 of every line aligns to the same
		// gutter. On subject rows that gutter holds the age column (6) +
		// 2-space gap + author column (16) + 2-space gap = 26 chars before
		// the subject. The id+padding above must fill the same 26 chars so
		// the meta string starts at the same column the subject does.
		ageCol := 6
		authorCol := 16
		gutter := ageCol + 2 + authorCol + 2 // = 26
		idColWidth := gutter                 // id padded to gutter so meta aligns with subject
		for i, s := range v.sessions {
			bar := " "
			id := s.ID
			if i == v.cursor {
				bar = tuicore.Title.Render("▏")
				id = tuicore.Bold.Render(s.ID)
			}
			meta := []string{fmt.Sprintf("%d memos", s.MemoCount), memo.FormatAge(s.LastUsed)}
			if s.HasRemote {
				meta = append(meta, "remote")
			}
			pad := idColWidth - len(s.ID)
			if pad < 1 {
				pad = 1
			}
			top := fmt.Sprintf("%s   %s%s%s", bar, id, strings.Repeat(" ", pad), tuicore.Dim.Render(strings.Join(meta, " · ")))
			lines = append(lines, top)

			recents := s.RecentMemos
			if len(recents) == 0 {
				lines = append(lines, fmt.Sprintf("%s   %s", bar, tuicore.Dim.Render("(no memos)")))
			} else {
				maxSubjectWidth := v.width - 4 - gutter
				for _, rec := range recents {
					subj := strings.TrimSpace(rec.Subject)
					if subj == "" {
						continue
					}
					subj = truncateSubject(subj, maxSubjectWidth)
					age := memo.FormatAge(rec.Timestamp)
					author := truncateSubject(rec.Author, authorCol)
					lines = append(lines, fmt.Sprintf("%s   %s",
						bar,
						tuicore.Dim.Render(fmt.Sprintf("%-*s  %-*s  %s", ageCol, age, authorCol, author, subj)),
					))
				}
			}
			if i < len(v.sessions)-1 {
				lines = append(lines, "")
			}
		}
	}
	body := strings.Join(lines, "\n")

	var footer string
	if v.confirm.IsActive() {
		footer = v.confirm.Render()
	} else if v.inputMode {
		v.input.SetWidth(wrapper.ContentWidth() - 4)
		footer = v.input.View()
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.MemoSessions, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(body, footer)
}

type sessionsReloadMsg struct{}

// truncateSubject shortens a subject to the given visible width, appending an
// ellipsis when the original exceeds the budget. A non-positive max returns
// the subject unchanged so callers don't have to guard their width math.
func truncateSubject(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
