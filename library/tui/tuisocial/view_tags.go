// view_tags.go - Tags view: the tags of a repository with their dates, authors and commits since the previous tag
package tuisocial

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// tagRow is one tag with the commit count since the previous tag in display order; -1 when the count is unknown.
type tagRow struct {
	git.Tag
	Prev  string
	Count int
}

// tagsView lists the tags of a repository; Enter opens the release that names a tag, else the diff of its commit.
type tagsView struct {
	url          string
	name         string
	repoDir      string
	tags         []tagRow
	releases     map[string]string
	cursor       int
	lastClickIdx int
	loading      bool
	err          error
	zonePrefix   string
}

// Bindings returns keybindings for the tags view.
func (v *tagsView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "enter", Label: "open", Contexts: []tuicore.Context{tuicore.Tags}, Handler: noop},
		{Key: "c", Label: "code", Contexts: []tuicore.Context{tuicore.Tags}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.Tags}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.Tags}, Handler: noop},
	}
}

// newTagsView creates a new tags view.
func newTagsView() *tagsView {
	return &tagsView{lastClickIdx: -1, zonePrefix: zone.NewPrefix()}
}

// Activate reads the tags of the repository the location names, or of the workspace, from its local clone.
func (v *tagsView) Activate(state *tuicore.State) tea.Cmd {
	v.loading, v.err, v.tags, v.cursor, v.lastClickIdx = true, nil, nil, 0, -1
	var isWorkspace bool
	v.url, v.name, v.repoDir, isWorkspace = localRepo(state, state.Router.Location().Param("url"))
	repoDir, repoURL := v.repoDir, v.url
	if isWorkspace {
		repoURL = gitmsg.ResolveRepoURL(state.Workdir)
	}
	return func() tea.Msg {
		if err := requireLocalClone(repoDir); err != nil {
			return tagsLoadedMsg{Err: err}
		}
		tags, err := git.ListTags(repoDir)
		if err != nil {
			return tagsLoadedMsg{Err: err}
		}
		rows := make([]tagRow, len(tags))
		for i, t := range tags {
			rows[i] = tagRow{Tag: t, Count: -1}
			prev := ""
			if i+1 < len(tags) {
				prev, rows[i].Prev = tags[i+1].Commit, tags[i+1].Name
			}
			if n, err := git.CountCommits(repoDir, t.Commit, prev); err == nil {
				rows[i].Count = n
			}
		}
		releases, err := release.ReleaseTags(repoURL)
		if err != nil {
			log.Debug("read the release tags", "error", err)
		}
		return tagsLoadedMsg{Tags: rows, Releases: releases}
	}
}

// Update handles messages and returns commands.
func (v *tagsView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.loading {
			return nil
		}
		return v.handleMouse(msg)
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	case tagsLoadedMsg:
		v.loading = false
		if msg.Err != nil {
			v.err = msg.Err
			state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
			return nil
		}
		v.tags, v.releases = msg.Tags, msg.Releases
	}
	return nil
}

// moveCursor moves the cursor by delta and keeps it on a row.
func (v *tagsView) moveCursor(delta int) {
	if next := v.cursor + delta; next >= 0 && next < len(v.tags) {
		v.cursor = next
	}
}

// handleMouse processes mouse input.
func (v *tagsView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		idx := tuicore.ZoneClicked(msg, len(v.tags), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.openSelected()
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		if msg.Mouse().Button == tea.MouseWheelUp {
			v.moveCursor(-1)
		} else {
			v.moveCursor(1)
		}
	}
	return nil
}

// handleKey processes keyboard input.
func (v *tagsView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		v.moveCursor(1)
	case "k", "up":
		v.moveCursor(-1)
	case "enter":
		return v.openSelected()
	case "c":
		return v.openCode()
	}
	return nil
}

// selected returns the tag under the cursor.
func (v *tagsView) selected() (tagRow, bool) {
	if v.loading || v.cursor < 0 || v.cursor >= len(v.tags) {
		return tagRow{}, false
	}
	return v.tags[v.cursor], true
}

// openSelected pushes the release that names the tag, else the diff of the tagged commit.
func (v *tagsView) openSelected() tea.Cmd {
	tag, ok := v.selected()
	if !ok {
		return nil
	}
	loc := tuicore.LocCommitDiff("refs/tags/" + tag.Name)
	if hash := v.releaseOf(tag.Name); hash != "" {
		loc = tuicore.LocReleaseDetail(hash)
	}
	return func() tea.Msg { return tuicore.NavigateMsg{Location: loc, Action: tuicore.NavPush} }
}

// openCode pushes the code view at the tag.
func (v *tagsView) openCode() tea.Cmd {
	tag, ok := v.selected()
	if !ok {
		return nil
	}
	loc := tuicore.LocRepoCode(v.url, tag.Name, "", 0, 0)
	return func() tea.Msg { return tuicore.NavigateMsg{Location: loc, Action: tuicore.NavPush} }
}

// Render renders the tag rows to a string.
func (v *tagsView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var b strings.Builder
	switch {
	case v.loading:
		b.WriteString(tuicore.Dim.Render("Loading tags..."))
	case v.err != nil:
		b.WriteString(tuicore.Dim.Render(v.err.Error()))
	case len(v.tags) == 0:
		b.WriteString(tuicore.Dim.Render("No tags"))
	}
	for i, tag := range v.tags {
		details := []string{tag.sinceLabel(), tuicore.FormatTime(time.Unix(tag.Time, 0)), tag.Author}
		if v.releaseOf(tag.Name) != "" {
			details = append(details, "release")
		}
		line := "  " + tag.Name
		if i == v.cursor {
			line = tuicore.Title.Render("▸ " + tag.Name)
		}
		for _, detail := range details {
			if detail != "" {
				line += tuicore.Dim.Render(" · " + detail)
			}
		}
		b.WriteString(tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, i), line) + "\n")
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.Tags, nil)
	return wrapper.Render(b.String(), footer)
}

// releaseOf returns the hash of the release that names a tag, by the tag name or by the version under a v prefix.
func (v *tagsView) releaseOf(name string) string {
	if hash := v.releases[name]; hash != "" {
		return hash
	}
	return v.releases[strings.TrimPrefix(name, "v")]
}

// sinceLabel words the commit count since the previous tag; the oldest tag counts its history.
func (t tagRow) sinceLabel() string {
	switch {
	case t.Count < 0:
		return ""
	case t.Prev == "":
		return commitsLabel(t.Count)
	}
	return fmt.Sprintf("%s since %s", commitsLabel(t.Count), t.Prev)
}

// Title returns the view title for the header.
func (v *tagsView) Title() string {
	if v.name == "" {
		return "⌂  Tags"
	}
	return "⌂  " + v.name + " tags"
}
