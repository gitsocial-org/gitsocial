// view_interdiff.go - Range-diff view comparing PR versions
package tuireview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// InterdiffView displays range-diff output between two PR versions.
type InterdiffView struct {
	workdir       string
	workspaceURL  string
	prID          string
	width         int
	height        int
	loaded        bool
	versions      []review.PRVersion
	fromVersion   int
	toVersion     int
	renderedLines []string
	cursor        int
	scroll        int
	errMsg        string
}

// NewInterdiffView creates a new interdiff view.
func NewInterdiffView(workdir string) *InterdiffView {
	return &InterdiffView{
		workdir:      workdir,
		workspaceURL: gitmsg.ResolveRepoURL(workdir),
	}
}

// SetSize sets the view dimensions.
func (v *InterdiffView) SetSize(w, h int) {
	v.width = w
	v.height = h - 3
}

type interdiffLoadedMsg struct {
	versions []review.PRVersion
	diff     string
	from     int
	to       int
	err      string
}

// Activate loads versions and range-diff when view becomes active.
func (v *InterdiffView) Activate(state *tuicore.State) tea.Cmd {
	prID := state.Router.Location().Param("prID")
	if prID == "" {
		return nil
	}
	v.prID = prID
	v.loaded = false
	v.errMsg = ""
	v.cursor = 0
	v.scroll = 0
	workdir := v.workdir
	workspaceURL := v.workspaceURL
	return func() tea.Msg {
		vRes := review.GetPRVersions(prID, workspaceURL)
		if !vRes.Success {
			return interdiffLoadedMsg{err: vRes.Error.Message}
		}
		versions := vRes.Data
		if len(versions) < 2 {
			return interdiffLoadedMsg{versions: versions, err: "Need at least 2 versions for interdiff"}
		}
		from := len(versions) - 2
		to := len(versions) - 1
		dRes := review.ComparePRVersions(workdir, prID, versions[from].Number, versions[to].Number)
		if !dRes.Success {
			return interdiffLoadedMsg{versions: versions, from: from, to: to, err: dRes.Error.Message}
		}
		return interdiffLoadedMsg{versions: versions, diff: dRes.Data, from: from, to: to}
	}
}

// Deactivate is called when the view is hidden.
func (v *InterdiffView) Deactivate() {}

// Update handles messages.
func (v *InterdiffView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case interdiffLoadedMsg:
		v.loaded = true
		v.versions = msg.versions
		v.fromVersion = msg.from
		v.toVersion = msg.to
		v.errMsg = msg.err
		if msg.err == "" {
			v.buildRenderedLines(msg.diff)
		} else {
			v.renderedLines = nil
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.renderedLines)-1 {
				v.cursor++
				v.ensureVisible()
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				v.ensureVisible()
			}
		case "g", "home":
			v.cursor = 0
			v.scroll = 0
		case "G", "end":
			if len(v.renderedLines) > 0 {
				v.cursor = len(v.renderedLines) - 1
				v.ensureVisible()
			}
		case "]":
			return v.cycleVersions(1)
		case "[":
			return v.cycleVersions(-1)
		}
	}
	return nil
}

// cycleVersions shifts the version pair and reloads.
func (v *InterdiffView) cycleVersions(dir int) tea.Cmd {
	if len(v.versions) < 2 {
		return nil
	}
	newFrom := v.fromVersion + dir
	newTo := v.toVersion + dir
	if newFrom < 0 || newTo >= len(v.versions) {
		return nil
	}
	v.loaded = false
	v.errMsg = ""
	from, to := newFrom, newTo
	workdir := v.workdir
	versions := v.versions
	prID := v.prID
	return func() tea.Msg {
		dRes := review.ComparePRVersions(workdir, prID, versions[from].Number, versions[to].Number)
		if !dRes.Success {
			return interdiffLoadedMsg{versions: versions, from: from, to: to, err: dRes.Error.Message}
		}
		return interdiffLoadedMsg{versions: versions, diff: dRes.Data, from: from, to: to}
	}
}

func (v *InterdiffView) ensureVisible() {
	viewH := v.height
	if viewH < 1 {
		viewH = 1
	}
	if v.cursor < v.scroll {
		v.scroll = v.cursor
	} else if v.cursor >= v.scroll+viewH {
		v.scroll = v.cursor - viewH + 1
	}
}

// buildRenderedLines parses range-diff output and colorizes it.
func (v *InterdiffView) buildRenderedLines(raw string) {
	v.renderedLines = nil
	lines := strings.Split(raw, "\n")
	equalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.TextSecondary))
	modifiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.StatusWarning))
	addedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.DiffAdded))
	removedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.DiffRemoved))
	hunkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tuicore.DiffHunkHeader))
	for _, line := range lines {
		if line == "" {
			v.renderedLines = append(v.renderedLines, "")
			continue
		}
		// Range-diff output uses prefixes at start or after commit summary indent
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case strings.HasPrefix(trimmed, "@@"):
			v.renderedLines = append(v.renderedLines, "  "+hunkStyle.Render(line))
		case strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "+++"):
			v.renderedLines = append(v.renderedLines, "  "+addedStyle.Render(line))
		case strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "---"):
			v.renderedLines = append(v.renderedLines, "  "+removedStyle.Render(line))
		case strings.HasPrefix(line, "="):
			v.renderedLines = append(v.renderedLines, "  "+equalStyle.Render(line))
		case strings.HasPrefix(line, "!"):
			v.renderedLines = append(v.renderedLines, "  "+modifiedStyle.Render(line))
		default:
			v.renderedLines = append(v.renderedLines, "  "+line)
		}
	}
}

// Render renders the interdiff view.
func (v *InterdiffView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	if !v.loaded {
		content = tuicore.Dim.Render("  Loading interdiff...")
	} else if v.errMsg != "" {
		content = tuicore.Dim.Render("  " + v.errMsg)
	} else if len(v.renderedLines) == 0 {
		content = tuicore.Dim.Render("  No differences between versions")
	} else {
		viewH := v.height
		if viewH < 1 {
			viewH = 1
		}
		end := v.scroll + viewH
		if end > len(v.renderedLines) {
			end = len(v.renderedLines)
		}
		visible := v.renderedLines[v.scroll:end]
		var lines []string
		for i, line := range visible {
			lineIdx := v.scroll + i
			if lineIdx == v.cursor {
				lines = append(lines, tuicore.Title.Render("▏")+line)
			} else {
				lines = append(lines, " "+line)
			}
		}
		content = strings.Join(lines, "\n")
	}
	footer := v.renderFooter(wrapper.ContentWidth())
	return wrapper.Render(content, footer)
}

func (v *InterdiffView) renderFooter(_ int) string {
	parts := make([]string, 0, 3)
	parts = append(parts, "[/]:versions")
	parts = append(parts, "j/k:scroll")
	parts = append(parts, "g/G:top/bottom")
	legend := "= unchanged  ! modified  + added  - removed"
	return tuicore.Dim.Render(strings.Join(parts, "  ") + "  " + legend)
}

// IsInputActive returns false.
func (v *InterdiffView) IsInputActive() bool {
	return false
}

// Title returns the view title.
func (v *InterdiffView) Title() string {
	if !v.loaded || len(v.versions) < 2 {
		return "⑂  Interdiff"
	}
	fromLabel := v.versions[v.fromVersion].Label
	toLabel := v.versions[v.toVersion].Label
	return fmt.Sprintf("⑂  Interdiff · %s → %s", fromLabel, toLabel)
}

// Bindings returns keybindings for this view.
func (v *InterdiffView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "[", Label: "prev version", Contexts: []tuicore.Context{tuicore.ReviewInterdiff}, Handler: noop},
		{Key: "]", Label: "next version", Contexts: []tuicore.Context{tuicore.ReviewInterdiff}, Handler: noop},
	}
}
