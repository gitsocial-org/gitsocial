// view_code.go - Code view: the tree of a branch and a file with line numbers, read-only
package tuisocial

import (
	"fmt"
	"os"
	pathpkg "path"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/storage"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// codeTabWidth is the width a tab takes in a rendered file.
const codeTabWidth = 4

// codeView shows the tree of a branch at a path, or one file of it; Enter descends into a directory or opens a file.
type codeView struct {
	url          string
	name         string
	isWorkspace  bool
	repoDir      string
	branch       string
	path         string
	lineStart    int
	lineEnd      int
	entries      []git.TreeEntry
	lines        []string
	language     string
	isFile       bool
	binary       bool
	lfsSize      int64
	size         int64
	cursor       int
	lastClickIdx int
	scroll       int
	loading      bool
	err          error
	zonePrefix   string
}

// Bindings returns keybindings for the code view.
func (v *codeView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	return []tuicore.Binding{
		{Key: "enter", Label: "open", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "backspace", Label: "parent", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "j", Label: "down", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "k", Label: "up", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "ctrl+d", Label: "half-page down", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "ctrl+u", Label: "half-page up", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
		{Key: "g/G", Label: "top/bottom", Contexts: []tuicore.Context{tuicore.Code}, Handler: noop},
	}
}

// newCodeView creates a new code view.
func newCodeView() *codeView {
	return &codeView{lastClickIdx: -1, zonePrefix: zone.NewPrefix()}
}

// Activate resolves the repository directory and the branch the location names, then reads the tree or the file at its path.
func (v *codeView) Activate(state *tuicore.State) tea.Cmd {
	loc := state.Router.Location()
	v.url, v.name, v.repoDir, v.isWorkspace = localRepo(state, loc.Param("url"))
	v.branch = loc.Param("branch")
	v.path = strings.Trim(loc.Param("path"), "/")
	v.lineStart, _ = strconv.Atoi(loc.Param("line"))
	v.lineEnd, _ = strconv.Atoi(loc.Param("lineEnd"))
	if v.lineEnd < v.lineStart {
		v.lineEnd = v.lineStart
	}
	v.entries, v.lines, v.isFile, v.err = nil, nil, false, nil
	v.cursor, v.scroll, v.lastClickIdx = 0, 0, -1
	v.loading = true
	repoDir, branch, path, isWorkspace := v.repoDir, v.branch, v.path, v.isWorkspace
	return func() tea.Msg {
		if err := requireLocalClone(repoDir); err != nil {
			return codeLoadedMsg{Err: err}
		}
		if branch == "" {
			branch = defaultCodeBranch(repoDir, isWorkspace)
		}
		if branch == "" {
			return codeLoadedMsg{Err: fmt.Errorf("no default branch in the local clone")}
		}
		kind, err := git.ObjectType(repoDir, branch, path)
		if err != nil {
			return codeLoadedMsg{Err: err}
		}
		msg := codeLoadedMsg{Type: kind, Branch: branch}
		switch kind {
		case "tree":
			msg.Entries, msg.Err = git.ListTree(repoDir, branch, path)
		case "blob":
			msg.Content, msg.Err = git.GetFileContent(repoDir, branch, path)
		default:
			msg.Err = fmt.Errorf("%s is a %s, not a file or a directory", path, kind)
		}
		return msg
	}
}

// localRepo resolves a location's url to the workspace or to the local clone of a followed repository: the url to keep, the display name, the git directory and whether it is the workspace.
func localRepo(state *tuicore.State, url string) (keepURL, name, repoDir string, isWorkspace bool) {
	originURL := git.GetOriginURL(state.Workdir)
	if url == "" || protocol.NormalizeURL(url) == protocol.NormalizeURL(originURL) {
		return "", "My Repository", state.Workdir, true
	}
	return url, protocol.GetDisplayName(url), storage.GetStorageDir(state.CacheDir, protocol.NormalizeURL(url)), false
}

// requireLocalClone fails when a repository has no directory to read.
func requireLocalClone(repoDir string) error {
	if _, err := os.Stat(repoDir); err != nil {
		return fmt.Errorf("no local clone of this repository: follow it to read its code")
	}
	return nil
}

// defaultCodeBranch is the default branch by the home rule for the workspace, else what the clone says, else the checked-out branch.
func defaultCodeBranch(repoDir string, isWorkspace bool) string {
	if isWorkspace {
		if branch, err := fetch.DefaultBranch(repoDir); err == nil && branch != "" {
			return branch
		}
	}
	if branch, err := git.GetDefaultBranch(repoDir); err == nil && branch != "" {
		return branch
	}
	branch, _ := git.GetCurrentBranch(repoDir)
	return branch
}

// Update handles messages and returns commands.
func (v *codeView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if v.loading {
			return nil
		}
		return v.handleMouse(msg, state)
	case tea.KeyPressMsg:
		return v.handleKey(msg, state)
	case codeLoadedMsg:
		v.loading = false
		if msg.Err != nil {
			v.err = msg.Err
			state.SetMessage(msg.Err.Error(), tuicore.MessageTypeError)
			return nil
		}
		v.isFile = msg.Type == "blob"
		v.branch = msg.Branch
		v.entries = msg.Entries
		if v.isFile {
			v.loadFile(msg.Content, state)
		}
	}
	return nil
}

// loadFile keeps the lines of a file, its language and its kind, and scrolls a line range into view.
func (v *codeView) loadFile(content string, state *tuicore.State) {
	v.size = int64(len(content))
	content = strings.TrimSuffix(content, "\n")
	_, v.lfsSize, _ = git.ParseLFSPointer([]byte(content))
	v.binary = v.lfsSize == 0 && git.IsBinaryContent(content)
	v.lines = strings.Split(content, "\n")
	v.language = tuicore.DetectLanguageFromPath(v.path)
	if v.lineStart > 0 {
		v.scroll = max(0, v.lineStart-1-max(0, state.InnerHeight()-3)/3)
	}
}

// rowCount returns the number of tree rows: the parent row when the path is not the root, and one per entry.
func (v *codeView) rowCount() int {
	return len(v.entries) + v.parentRows()
}

// parentRows is 1 when the tree shows a row for the parent directory.
func (v *codeView) parentRows() int {
	if v.path != "" {
		return 1
	}
	return 0
}

// moveCursor moves the tree cursor by delta and keeps it on a row.
func (v *codeView) moveCursor(delta int) {
	if next := v.cursor + delta; next >= 0 && next < v.rowCount() {
		v.cursor = next
	}
}

// scrollBy moves the file by delta lines and keeps the window on the file.
func (v *codeView) scrollBy(delta int) {
	v.scroll = min(max(0, v.scroll+delta), max(0, len(v.lines)-1))
}

// handleMouse processes mouse input: a click selects a tree row, a second click opens it, and the wheel moves either mode.
func (v *codeView) handleMouse(msg tea.MouseMsg, state *tuicore.State) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		if v.isFile {
			return nil
		}
		idx := tuicore.ZoneClicked(msg, v.rowCount(), v.zonePrefix)
		if idx >= 0 {
			if idx == v.lastClickIdx && idx == v.cursor {
				v.lastClickIdx = -1
				return v.openSelected(state)
			}
			v.cursor = idx
			v.lastClickIdx = idx
		}
	case tea.MouseWheelMsg:
		delta := 1
		if msg.Mouse().Button == tea.MouseWheelUp {
			delta = -1
		}
		if v.isFile {
			v.scrollBy(3 * delta)
		} else {
			v.moveCursor(delta)
		}
	}
	return nil
}

// handleKey processes keyboard input for the tree and for a file.
func (v *codeView) handleKey(msg tea.KeyPressMsg, state *tuicore.State) tea.Cmd {
	half := max(1, (state.InnerHeight()-3)/2)
	switch msg.String() {
	case "j", "down":
		if v.isFile {
			v.scrollBy(1)
		} else {
			v.moveCursor(1)
		}
	case "k", "up":
		if v.isFile {
			v.scrollBy(-1)
		} else {
			v.moveCursor(-1)
		}
	case "ctrl+d", "pgdown":
		if v.isFile {
			v.scrollBy(half)
		} else {
			v.moveCursor(half)
		}
	case "ctrl+u", "pgup":
		if v.isFile {
			v.scrollBy(-half)
		} else {
			v.moveCursor(-half)
		}
	case "g", "home":
		v.scroll, v.cursor = 0, 0
	case "G", "end":
		v.scroll = max(0, len(v.lines)-1)
		v.cursor = max(0, v.rowCount()-1)
	case "enter":
		return v.openSelected(state)
	case "backspace", "left":
		return v.openParent()
	}
	return nil
}

// openSelected pushes the location of the entry under the cursor; the parent row goes up instead.
func (v *codeView) openSelected(state *tuicore.State) tea.Cmd {
	if v.loading || v.isFile {
		return nil
	}
	if v.parentRows() == 1 && v.cursor == 0 {
		return v.openParent()
	}
	idx := v.cursor - v.parentRows()
	if idx < 0 || idx >= len(v.entries) {
		return nil
	}
	entry := v.entries[idx]
	if entry.Type == "commit" {
		state.SetMessage(entry.Name+" is a submodule: its code is in another repository", tuicore.MessageTypeWarning)
		return nil
	}
	loc := tuicore.LocRepoCode(v.url, v.branch, pathpkg.Join(v.path, entry.Name), 0, 0)
	return func() tea.Msg { return tuicore.NavigateMsg{Location: loc, Action: tuicore.NavPush} }
}

// openParent replaces the location with the directory above the current path; the root has none.
func (v *codeView) openParent() tea.Cmd {
	if v.path == "" {
		return nil
	}
	parent := pathpkg.Dir(v.path)
	if parent == "." {
		parent = ""
	}
	loc := tuicore.LocRepoCode(v.url, v.branch, parent, 0, 0)
	return func() tea.Msg { return tuicore.NavigateMsg{Location: loc, Action: tuicore.NavReplace} }
}

// Render renders the tree rows or the file lines with the footer.
func (v *codeView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)
	var content string
	switch {
	case v.loading:
		content = tuicore.Dim.Render("Loading code...")
	case v.err != nil:
		content = tuicore.Dim.Render(v.err.Error())
	case v.isFile:
		content = v.renderFile(wrapper.ContentWidth(), wrapper.ContentHeight())
	default:
		content = v.renderTree()
	}
	footer := tuicore.RenderFooter(state.Registry, tuicore.Code, nil)
	return wrapper.Render(content, footer)
}

// renderTree renders the parent row, the directories and the files with their sizes.
func (v *codeView) renderTree() string {
	var b strings.Builder
	if len(v.entries) == 0 && v.path == "" {
		b.WriteString(tuicore.Dim.Render("Empty tree"))
		return b.String()
	}
	if v.parentRows() == 1 {
		b.WriteString(v.row(0, "..", ""))
	}
	for i, entry := range v.entries {
		name, detail := entry.Name, ""
		switch entry.Type {
		case "tree":
			name += "/"
		case "commit":
			detail = "submodule"
		default:
			detail = formatCodeSize(entry.Size)
		}
		b.WriteString(v.row(i+v.parentRows(), name, detail))
	}
	return b.String()
}

// row renders one selectable tree line with its dim detail.
func (v *codeView) row(index int, name, detail string) string {
	line := "  " + name
	if index == v.cursor {
		line = tuicore.Title.Render("▸ " + name)
	}
	if detail != "" {
		line += tuicore.Dim.Render(" · " + detail)
	}
	return tuicore.MarkZone(tuicore.ZoneID(v.zonePrefix, index), line) + "\n"
}

// renderFile renders the visible lines with their numbers; a line of the requested range takes the selected style.
func (v *codeView) renderFile(width, height int) string {
	switch {
	case v.lfsSize > 0:
		return tuicore.Dim.Render("Git LFS file · " + formatCodeSize(v.lfsSize) + " · the content is in LFS, not in this view")
	case v.binary:
		return tuicore.Dim.Render("Binary file · " + formatCodeSize(v.size))
	}
	gutter := len(strconv.Itoa(len(v.lines)))
	textWidth := max(8, width-gutter-2)
	v.scroll = min(v.scroll, max(0, len(v.lines)-1))
	end := min(len(v.lines), v.scroll+max(1, height))
	var b strings.Builder
	for i := v.scroll; i < end; i++ {
		number := fmt.Sprintf("%*d", gutter, i+1)
		text := fitCodeLine(v.lines[i], textWidth)
		if i+1 >= v.lineStart && i+1 <= v.lineEnd {
			b.WriteString(tuicore.NormalSelected.Render(number + "  " + text))
		} else {
			b.WriteString(tuicore.Dim.Render(number) + "  " + tuicore.HighlightLine(text, v.language, false))
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// fitCodeLine expands tabs and cuts a line to the width, by rune.
func fitCodeLine(line string, width int) string {
	line = strings.ReplaceAll(line, "\t", strings.Repeat(" ", codeTabWidth))
	runes := []rune(line)
	if len(runes) > width {
		return string(runes[:width-1]) + "…"
	}
	return line
}

// formatCodeSize words a byte count, with bytes below one kilobyte.
func formatCodeSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return cache.FormatBytes(n)
}

// Title returns the view title for the header: the repository, the branch and the path.
func (v *codeView) Title() string {
	title := "▤  " + v.name
	if v.branch != "" {
		title += " · " + v.branch
	}
	if v.path != "" {
		title += " · " + v.path
	}
	return tuicore.TruncateToWidth(title, 70)
}
