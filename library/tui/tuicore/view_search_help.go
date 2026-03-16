// view_search_help.go - Search help view showing filter syntax and keyboard shortcuts
package tuicore

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// SearchHelpView displays search syntax documentation.
type SearchHelpView struct {
	scroll int
	lines  []string
}

// NewSearchHelpView creates a new search help view.
func NewSearchHelpView() *SearchHelpView {
	return &SearchHelpView{}
}

// Bindings returns keybindings for the search help view.
func (v *SearchHelpView) Bindings() []Binding {
	noop := func(ctx *HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []Binding{
		{Key: "j", Label: "scroll down", Contexts: []Context{SearchHelp}, Handler: noop},
		{Key: "k", Label: "scroll up", Contexts: []Context{SearchHelp}, Handler: noop},
		{Key: "?", Label: "back to search", Contexts: []Context{SearchHelp}, Handler: noop},
	}
}

// row formats a key-description pair with aligned columns.
func row(key, desc string) string {
	return fmt.Sprintf("  %s  %s", Bold.Render(fmt.Sprintf("%-20s", key)), Dim.Render(desc))
}

// Activate resets scroll and renders content when the view becomes active.
func (v *SearchHelpView) Activate(_ *State) tea.Cmd {
	v.scroll = 0
	v.lines = []string{
		"",
		Title.Render("Filters"),
		"",
		row("author:<email>", "Posts by author"),
		row("type:<type>", "post, comment, repost, quote,"),
		row("", "pr, issue, milestone, sprint, release"),
		row("repo:<url>", "Posts from repository"),
		row("hash:<prefix>", "Commit hash lookup"),
		row("list:<name>", "Posts from a specific list"),
		row("after:YYYY-MM-DD", "Posts after date"),
		row("before:YYYY-MM-DD", "Posts before date"),
		"",
		Title.Render("Shortcuts"),
		"",
		row("@alice", "Same as author:alice"),
		row("#abc123f", "Same as hash:abc123f (7+ hex chars)"),
		"",
		Title.Render("Examples"),
		"",
		row("author:alice auth", `Posts by alice containing "auth"`),
		row("type:comment", "All comments"),
		row("after:2026-01-01 bug", `Posts about "bug" since Jan 2026`),
		row("@bob type:post", "Posts (not comments) by bob"),
		row("repo:github.com/x/y", "Posts from a specific repo"),
		"",
		Title.Render("Sort"),
		"",
		"  " + Dim.Render("Results are ranked by relevance:"),
		row("Content match", "10 pts"),
		row("Author match", "5 pts"),
		row("Recency", "tiebreaker"),
		"",
		Title.Render("Navigation"),
		"",
		row("/", "Focus search input"),
		row("Enter", "Execute search / open result"),
		row("Esc", "Back to results / exit search"),
		row("Up/Down", "Navigate results"),
		row("?", "Toggle this help"),
	}
	return nil
}

// Update handles messages.
func (v *SearchHelpView) Update(msg tea.Msg, state *State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.(type) {
		case tea.MouseWheelMsg:
			m := msg.Mouse()
			if m.Button == tea.MouseWheelUp {
				v.scroll -= 3
				if v.scroll < 0 {
					v.scroll = 0
				}
			} else {
				v.scroll += 3
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			v.scroll++
		case "k", "up":
			if v.scroll > 0 {
				v.scroll--
			}
		case "ctrl+d", "pgdown":
			v.scroll += state.InnerHeight() / 2
		case "ctrl+u", "pgup":
			v.scroll -= state.InnerHeight() / 2
			if v.scroll < 0 {
				v.scroll = 0
			}
		case "home", "g":
			v.scroll = 0
		case "end", "G":
			v.scroll = 99999
		case "?", "esc":
			return func() tea.Msg {
				return NavigateMsg{Action: NavBack}
			}
		}
	}
	return nil
}

// Render renders the search help view.
func (v *SearchHelpView) Render(state *State) string {
	wrapper := NewViewWrapper(state)
	if len(v.lines) == 0 {
		footer := RenderFooter(state.Registry, SearchHelp, wrapper.ContentWidth(), nil)
		return wrapper.Render(Dim.Render("Loading..."), footer)
	}
	height := wrapper.ContentHeight()
	if v.scroll >= len(v.lines) {
		v.scroll = len(v.lines) - 1
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	end := v.scroll + height
	if end > len(v.lines) {
		end = len(v.lines)
	}
	visible := strings.Join(v.lines[v.scroll:end], "\n")
	footer := RenderFooter(state.Registry, SearchHelp, wrapper.ContentWidth(), nil)
	return wrapper.Render(visible, footer)
}
