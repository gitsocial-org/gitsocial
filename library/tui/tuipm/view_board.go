// board.go - Kanban board view for PM issues
package tuipm

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// BoardView displays issues in a kanban board layout.
type BoardView struct {
	workdir         string
	repoURL         string
	width           int
	height          int
	board           pm.BoardView
	prefs           pm.UserPrefs
	loaded          bool
	selectedCol     int
	selectedRow     int
	scrollOffset    int
	quickCreateMode bool
	quickInput      textinput.Model
	zonePrefix      string
	lastClickCol    int
	lastClickRow    int
}

// NewBoardView creates a new board view.
func NewBoardView(workdir string) *BoardView {
	input := textinput.New()
	input.Placeholder = "Issue subject..."
	input.CharLimit = 200
	input.Prompt = "New issue: "
	tuicore.StyleTextInput(&input, tuicore.Title, tuicore.Normal, tuicore.Dim)
	return &BoardView{
		workdir:      workdir,
		quickInput:   input,
		zonePrefix:   zone.NewPrefix(),
		lastClickCol: -1,
		lastClickRow: -1,
	}
}

// SetSize sets the view dimensions.
func (v *BoardView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// Activate loads the board data.
func (v *BoardView) Activate(state *tuicore.State) tea.Cmd {
	v.quickCreateMode = false
	v.quickInput.SetValue("")
	return v.loadBoard()
}

func (v *BoardView) loadBoard() tea.Cmd {
	workdir := v.workdir
	return func() tea.Msg {
		result := pm.GetBoardView(workdir)
		if !result.Success {
			return BoardLoadedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
		}
		branch := gitmsg.GetExtBranch(workdir, "pm")
		unpushed, _ := git.GetUnpushedCommits(workdir, branch)
		for ci := range result.Data.Columns {
			for ii := range result.Data.Columns[ci].Issues {
				ref := protocol.ParseRef(result.Data.Columns[ci].Issues[ii].ID)
				if _, ok := unpushed[ref.Value]; ok {
					result.Data.Columns[ci].Issues[ii].IsUnpushed = true
				}
			}
		}
		// Get repo URL for prefs
		repoURL := workdir
		prefs := pm.GetUserPrefs(repoURL)
		return BoardLoadedMsg{Board: result.Data, RepoURL: repoURL, Prefs: prefs}
	}
}

// BoardLoadedMsg signals that the board has been loaded.
type BoardLoadedMsg struct {
	Board   pm.BoardView
	RepoURL string
	Prefs   pm.UserPrefs
	Err     error
}

// Update handles messages.
func (v *BoardView) Update(msg tea.Msg, state *tuicore.State) tea.Cmd {
	switch msg := msg.(type) {
	case BoardLoadedMsg:
		if msg.Err == nil {
			v.board = msg.Board
			v.repoURL = msg.RepoURL
			v.prefs = msg.Prefs
			v.loaded = true
		}
		return nil

	case IssueCreatedMsg:
		v.quickCreateMode = false
		v.quickInput.SetValue("")
		if msg.Err == nil {
			return v.loadBoard()
		}
		return nil

	case tea.KeyPressMsg:
		if v.quickCreateMode {
			return v.handleQuickCreateKey(msg, state)
		}
		return v.handleKey(msg, state)
	case tea.MouseMsg:
		return v.handleMouse(msg)
	}

	// Update text input when in quick create mode
	if v.quickCreateMode {
		var cmd tea.Cmd
		v.quickInput, cmd = v.quickInput.Update(msg)
		return cmd
	}
	return nil
}

// IsInputActive returns true when text input is active.
func (v *BoardView) IsInputActive() bool {
	return v.quickCreateMode
}

func (v *BoardView) handleQuickCreateKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	switch msg.String() {
	case "enter":
		subject := strings.TrimSpace(v.quickInput.Value())
		if subject == "" {
			v.quickCreateMode = false
			return nil
		}
		workdir := v.workdir
		return func() tea.Msg {
			result := pm.CreateIssue(workdir, subject, "", pm.CreateIssueOptions{})
			if !result.Success {
				return IssueCreatedMsg{Err: fmt.Errorf("%s", result.Error.Message)}
			}
			return IssueCreatedMsg{Issue: result.Data}
		}
	case "esc":
		v.quickCreateMode = false
		v.quickInput.SetValue("")
		return nil
	}
	// Let textinput handle other keys
	var cmd tea.Cmd
	v.quickInput, cmd = v.quickInput.Update(msg)
	return cmd
}

func (v *BoardView) handleKey(msg tea.KeyPressMsg, _ *tuicore.State) tea.Cmd {
	if !v.loaded || len(v.board.Columns) == 0 {
		return nil
	}

	switch msg.String() {
	case "left":
		if v.selectedCol > 0 {
			v.selectedCol--
			v.clampRow()
			v.adjustScroll()
		}
	case "right":
		if v.selectedCol < len(v.board.Columns)-1 {
			v.selectedCol++
			v.clampRow()
			v.adjustScroll()
		}
	case "down":
		col := v.board.Columns[v.selectedCol]
		if v.selectedRow < len(col.Issues)-1 {
			v.selectedRow++
			v.adjustScroll()
		}
	case "up":
		if v.selectedRow > 0 {
			v.selectedRow--
			v.adjustScroll()
		}
	case "home":
		v.selectedRow = 0
		v.scrollOffset = 0
	case "end":
		if len(v.board.Columns) > 0 {
			col := v.board.Columns[v.selectedCol]
			if len(col.Issues) > 0 {
				v.selectedRow = len(col.Issues) - 1
				v.adjustScroll()
			}
		}
	case "enter":
		if v.selectedCol >= 0 && v.selectedCol < len(v.board.Columns) {
			col := v.board.Columns[v.selectedCol]
			if v.selectedRow >= 0 && v.selectedRow < len(col.Issues) {
				issueID := col.Issues[v.selectedRow].ID
				selectedRow := v.selectedRow
				colLen := len(col.Issues)
				return func() tea.Msg {
					return tuicore.NavigateMsg{
						Location:    tuicore.LocPMIssueDetail(issueID),
						Action:      tuicore.NavPush,
						SourcePath:  "/pm/board",
						SourceIndex: selectedRow,
						SourceTotal: colLen,
					}
				}
			}
		}
	case "r":
		return v.loadBoard()
	case "n":
		// Quick create - inline input
		v.quickCreateMode = true
		v.quickInput.SetValue("")
		v.quickInput.Focus()
		return nil
	case "N":
		// Full create - form view
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocPMNewIssue,
				Action:   tuicore.NavPush,
			}
		}
	case "x":
		// Toggle column collapse
		if v.selectedCol >= 0 && v.selectedCol < len(v.board.Columns) {
			colName := v.board.Columns[v.selectedCol].Name
			v.prefs.ToggleColumnCollapsed(colName)
			repoURL := v.repoURL
			prefs := v.prefs
			go func() { _ = pm.SaveUserPrefs(repoURL, prefs) }()
		}
		return nil
	case "F":
		return func() tea.Msg {
			return tuicore.NavigateMsg{
				Location: tuicore.LocForks,
				Action:   tuicore.NavPush,
			}
		}
	case "s":
		// Cycle swimlane grouping
		v.prefs.CycleSwimlaneField()
		repoURL := v.repoURL
		prefs := v.prefs
		go func() { _ = pm.SaveUserPrefs(repoURL, prefs) }()
		return nil
	}
	return nil
}

func (v *BoardView) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !v.loaded || len(v.board.Columns) == 0 {
		return nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			if v.selectedRow > 0 {
				v.selectedRow--
				v.adjustScroll()
			}
		} else {
			if v.selectedCol < len(v.board.Columns) {
				col := v.board.Columns[v.selectedCol]
				if v.selectedRow < len(col.Issues)-1 {
					v.selectedRow++
					v.adjustScroll()
				}
			}
		}
		return nil
	case tea.MouseClickMsg:
		// Check header clicks
		for col := range v.board.Columns {
			zid := fmt.Sprintf("%sh%d", v.zonePrefix, col)
			if zone.Get(zid).InBounds(msg) {
				v.selectedCol = col
				v.clampRow()
				v.lastClickCol = -1
				v.lastClickRow = -1
				return nil
			}
		}
		// Check issue cell clicks
		for col := range v.board.Columns {
			for row := range v.board.Columns[col].Issues {
				zid := fmt.Sprintf("%s%d_%d", v.zonePrefix, col, row)
				if zone.Get(zid).InBounds(msg) {
					if v.lastClickCol == col && v.lastClickRow == row {
						// Second click on same cell — open detail
						v.lastClickCol = -1
						v.lastClickRow = -1
						issueID := v.board.Columns[col].Issues[row].ID
						return func() tea.Msg {
							return tuicore.NavigateMsg{
								Location: tuicore.LocPMIssueDetail(issueID),
								Action:   tuicore.NavPush,
							}
						}
					}
					v.selectedCol = col
					v.selectedRow = row
					v.lastClickCol = col
					v.lastClickRow = row
					v.adjustScroll()
					return nil
				}
			}
		}
	}
	return nil
}

// adjustScroll ensures the selected row is visible.
func (v *BoardView) adjustScroll() {
	if v.selectedRow < v.scrollOffset {
		v.scrollOffset = v.selectedRow
	}
	visibleHeight := v.height - 1
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	if v.selectedRow >= v.scrollOffset+visibleHeight {
		v.scrollOffset = v.selectedRow - visibleHeight + 1
	}
}

func (v *BoardView) clampRow() {
	if v.selectedCol >= len(v.board.Columns) {
		return
	}
	col := v.board.Columns[v.selectedCol]
	if v.selectedRow >= len(col.Issues) {
		v.selectedRow = len(col.Issues) - 1
	}
	if v.selectedRow < 0 {
		v.selectedRow = 0
	}
}

// Render renders the board view.
func (v *BoardView) Render(state *tuicore.State) string {
	wrapper := tuicore.NewViewWrapper(state)

	var content string
	if !v.loaded {
		content = "Loading board..."
	} else if len(v.board.Columns) == 0 {
		content = "No columns configured"
	} else {
		// Reserve space for quick create input if active
		boardHeight := wrapper.ContentHeight()
		if v.quickCreateMode {
			boardHeight -= 2 // Input line + spacing
		}
		content = v.renderBoard(wrapper.ContentWidth(), boardHeight)
		if v.quickCreateMode {
			v.quickInput.SetWidth(wrapper.ContentWidth() - 15)
			content += "\n" + v.quickInput.View()
		}
	}

	var footer string
	if v.quickCreateMode {
		footer = tuicore.Dim.Render("enter:create  esc:cancel")
	} else {
		footer = tuicore.RenderFooter(state.Registry, tuicore.PMBoard, wrapper.ContentWidth(), nil)
	}
	return wrapper.Render(content, footer)
}

// renderBoard renders the kanban board content.
func (v *BoardView) renderBoard(width, height int) string {
	colCount := len(v.board.Columns)
	// Count collapsed columns and calculate width for expanded ones
	collapsedCount := 0
	for _, col := range v.board.Columns {
		if v.prefs.IsColumnCollapsed(col.Name) {
			collapsedCount++
		}
	}
	expandedCount := colCount - collapsedCount
	// Width = total - separators - collapsed columns width
	separatorWidth := (colCount - 1) * 3
	collapsedWidth := collapsedCount * 4
	availableWidth := width - separatorWidth - collapsedWidth
	colWidth := 15
	if expandedCount > 0 {
		colWidth = availableWidth / expandedCount
		if colWidth < 15 {
			colWidth = 15
		}
	}

	var lines []string

	// Header row with swimlane indicator
	headers := make([]string, 0, len(v.board.Columns))
	for i, col := range v.board.Columns {
		count := len(col.Issues)
		isCollapsed := v.prefs.IsColumnCollapsed(col.Name)
		wip := v.prefs.GetWIPOverride(col.Name)
		if wip == nil {
			wip = col.WIP
		}
		var header string
		collapseIndicator := ""
		if isCollapsed {
			collapseIndicator = "▸ "
		}
		if wip != nil {
			header = fmt.Sprintf(" %s%s (%d/%d)", collapseIndicator, col.Name, count, *wip)
		} else {
			header = fmt.Sprintf(" %s%s (%d)", collapseIndicator, col.Name, count)
		}
		thisColWidth := colWidth
		if isCollapsed {
			thisColWidth = 4
		}
		if len(header) > thisColWidth-2 {
			header = header[:thisColWidth-3] + "…"
		}
		style := lipgloss.NewStyle().Width(thisColWidth).Bold(i == v.selectedCol)
		if wip != nil && count > *wip {
			style = style.Foreground(lipgloss.Color("196"))
		}
		zid := fmt.Sprintf("%sh%d", v.zonePrefix, i)
		headers = append(headers, zone.Mark(zid, style.Render(header)))
	}
	lines = append(lines, strings.Join(headers, " │ "))

	// Swimlane indicator line
	if v.prefs.SwimlaneField != "" {
		indicator := fmt.Sprintf("─── grouped by %s ", v.prefs.SwimlaneField)
		indicator += strings.Repeat("─", width-len(indicator))
		lines = append(lines, tuicore.Dim.Render(indicator))
	} else {
		lines = append(lines, strings.Repeat("─", width))
	}

	// Issue rows (subtract header + separator)
	availableHeight := height - 2
	if availableHeight < 1 {
		availableHeight = 1
	}

	if v.prefs.SwimlaneField != "" {
		lines = append(lines, v.renderWithSwimlanes(colWidth, availableHeight, width)...)
	} else {
		lines = append(lines, v.renderWithoutSwimlanes(colWidth, availableHeight)...)
	}

	return strings.Join(lines, "\n")
}

// renderWithoutSwimlanes renders issues without swimlane grouping.
func (v *BoardView) renderWithoutSwimlanes(colWidth, availableHeight int) []string {
	var lines []string
	for row := 0; row < availableHeight; row++ {
		dataRow := v.scrollOffset + row
		var cells []string
		for colIdx, col := range v.board.Columns {
			isCollapsed := v.prefs.IsColumnCollapsed(col.Name)
			thisColWidth := colWidth
			if isCollapsed {
				thisColWidth = 4
			}
			if isCollapsed {
				cells = append(cells, strings.Repeat(" ", thisColWidth))
			} else if dataRow < len(col.Issues) {
				issue := col.Issues[dataRow]
				zid := fmt.Sprintf("%s%d_%d", v.zonePrefix, colIdx, dataRow)
				cells = append(cells, zone.Mark(zid, v.renderIssueCell(issue, thisColWidth, colIdx == v.selectedCol && dataRow == v.selectedRow)))
			} else {
				cells = append(cells, strings.Repeat(" ", thisColWidth))
			}
		}
		lines = append(lines, strings.Join(cells, " │ "))
	}
	return lines
}

// renderWithSwimlanes renders issues grouped by swimlane field.
func (v *BoardView) renderWithSwimlanes(colWidth, availableHeight, totalWidth int) []string {
	swimlanes := v.getSwimlaneOrder()
	grouped := v.groupIssuesBySwimlane(swimlanes)

	// Build per-column issue index maps: for each column, map issue hash to its
	// flat index within col.Issues so zone IDs and selection match keyboard nav.
	colIssueIndex := make([]map[string]int, len(v.board.Columns))
	for colIdx, col := range v.board.Columns {
		colIssueIndex[colIdx] = make(map[string]int, len(col.Issues))
		for i, issue := range col.Issues {
			colIssueIndex[colIdx][issue.ID] = i
		}
	}

	var lines []string
	rowCount := 0

	for _, lane := range swimlanes {
		if rowCount >= availableHeight {
			break
		}
		hasIssues := false
		for _, col := range v.board.Columns {
			if len(grouped[col.Name][lane]) > 0 {
				hasIssues = true
				break
			}
		}
		if !hasIssues {
			continue
		}

		isCollapsed := v.prefs.IsSwimlaneCollapsed(lane)
		laneLabel := lane
		if laneLabel == "" {
			laneLabel = "(none)"
		}

		collapseIcon := "▾"
		if isCollapsed {
			collapseIcon = "▸"
		}
		header := fmt.Sprintf("─ %s %s ", collapseIcon, laneLabel)
		header += strings.Repeat("─", totalWidth-len(header))
		lines = append(lines, tuicore.Dim.Render(header))
		rowCount++

		if isCollapsed {
			continue
		}

		maxInLane := 0
		for _, col := range v.board.Columns {
			if len(grouped[col.Name][lane]) > maxInLane {
				maxInLane = len(grouped[col.Name][lane])
			}
		}

		for laneRow := 0; laneRow < maxInLane && rowCount < availableHeight; laneRow++ {
			var cells []string
			for colIdx, col := range v.board.Columns {
				isColCollapsed := v.prefs.IsColumnCollapsed(col.Name)
				thisColWidth := colWidth
				if isColCollapsed {
					thisColWidth = 4
				}
				if isColCollapsed {
					cells = append(cells, strings.Repeat(" ", thisColWidth))
				} else if laneRow < len(grouped[col.Name][lane]) {
					issue := grouped[col.Name][lane][laneRow]
					flatIdx := colIssueIndex[colIdx][issue.ID]
					isSelected := colIdx == v.selectedCol && flatIdx == v.selectedRow
					zid := fmt.Sprintf("%s%d_%d", v.zonePrefix, colIdx, flatIdx)
					cells = append(cells, zone.Mark(zid, v.renderIssueCell(issue, thisColWidth, isSelected)))
				} else {
					cells = append(cells, strings.Repeat(" ", thisColWidth))
				}
			}
			lines = append(lines, strings.Join(cells, " │ "))
			rowCount++
		}
	}

	for rowCount < availableHeight {
		cells := make([]string, 0, len(v.board.Columns))
		for _, col := range v.board.Columns {
			isCollapsed := v.prefs.IsColumnCollapsed(col.Name)
			thisColWidth := colWidth
			if isCollapsed {
				thisColWidth = 4
			}
			cells = append(cells, strings.Repeat(" ", thisColWidth))
		}
		lines = append(lines, strings.Join(cells, " │ "))
		rowCount++
	}

	return lines
}

// renderIssueCell renders a single issue cell.
func (v *BoardView) renderIssueCell(issue pm.Issue, width int, isSelected bool) string {
	stateIcon := "○"
	if issue.State == pm.StateClosed {
		stateIcon = "●"
	}
	cell := fmt.Sprintf(" %s %s", stateIcon, issue.Subject)
	if len(cell) > width-2 {
		cell = cell[:width-3] + "…"
	}
	style := lipgloss.NewStyle().Width(width)
	if isSelected {
		style = style.Reverse(true)
	}
	return style.Render(cell)
}

// getSwimlaneOrder returns ordered swimlane values based on field type.
func (v *BoardView) getSwimlaneOrder() []string {
	field := v.prefs.SwimlaneField
	// Predefined order for known fields
	switch field {
	case "priority":
		return []string{"critical", "high", "medium", "low", ""}
	case "kind":
		return []string{"bug", "feature", "task", "story", "spike", "chore", ""}
	default:
		// Collect unique values from issues
		seen := make(map[string]bool)
		var values []string
		for _, col := range v.board.Columns {
			for _, issue := range col.Issues {
				val := v.getSwimlaneValue(issue)
				if !seen[val] {
					seen[val] = true
					values = append(values, val)
				}
			}
		}
		return values
	}
}

// getSwimlaneValue extracts the swimlane field value from an issue.
func (v *BoardView) getSwimlaneValue(issue pm.Issue) string {
	field := v.prefs.SwimlaneField
	switch field {
	case "assignees":
		if len(issue.Assignees) > 0 {
			return issue.Assignees[0]
		}
		return ""
	case "author":
		if issue.Origin != nil && issue.Origin.AuthorEmail != "" {
			if issue.Origin.AuthorName != "" {
				return issue.Origin.AuthorName
			}
			return issue.Origin.AuthorEmail
		}
		if issue.Author.Name != "" {
			return issue.Author.Name
		}
		return issue.Author.Email
	case "priority", "kind":
		for _, label := range issue.Labels {
			if label.Scope == field {
				return label.Value
			}
		}
		return ""
	default:
		return ""
	}
}

// groupIssuesBySwimlane groups issues by swimlane value for each column.
func (v *BoardView) groupIssuesBySwimlane(swimlanes []string) map[string]map[string][]pm.Issue {
	result := make(map[string]map[string][]pm.Issue)
	for _, col := range v.board.Columns {
		result[col.Name] = make(map[string][]pm.Issue)
		for _, lane := range swimlanes {
			result[col.Name][lane] = nil
		}
		for _, issue := range col.Issues {
			val := v.getSwimlaneValue(issue)
			result[col.Name][val] = append(result[col.Name][val], issue)
		}
	}
	return result
}

// Title returns the view title.
func (v *BoardView) Title() string {
	framework := pm.GetPMConfig(v.workdir).Framework
	if framework == "" {
		framework = "kanban"
	}
	// Capitalize first letter
	framework = strings.ToUpper(framework[:1]) + framework[1:]
	return "▦  Boards " + tuicore.Dim.Render("· "+framework)
}

// Bindings returns keybindings for this view.
func (v *BoardView) Bindings() []tuicore.Binding {
	noop := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) { return false, nil }
	push := func(ctx *tuicore.HandlerContext) (bool, tea.Cmd) {
		if ctx.StartPush == nil {
			return false, nil
		}
		return true, ctx.StartPush()
	}
	return []tuicore.Binding{
		{Key: "n", Label: "quick create", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "N", Label: "full create", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "F", Label: "forks", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "x", Label: "collapse col", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "s", Label: "swimlanes", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "r", Label: "refresh", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "up", Label: "up", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "down", Label: "down", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "left", Label: "prev col", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "right", Label: "next col", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "home", Label: "first", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "end", Label: "last", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "enter", Label: "open issue", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: noop},
		{Key: "p", Label: "push", Contexts: []tuicore.Context{tuicore.PMBoard}, Handler: push},
	}
}

// GetItemAt returns the issue ID at the given index in the selected column.
func (v *BoardView) GetItemAt(index int) (string, bool) {
	if v.selectedCol >= 0 && v.selectedCol < len(v.board.Columns) {
		col := v.board.Columns[v.selectedCol]
		if index >= 0 && index < len(col.Issues) {
			return col.Issues[index].ID, true
		}
	}
	return "", false
}

// GetItemCount returns the number of issues in the selected column.
func (v *BoardView) GetItemCount() int {
	if v.selectedCol >= 0 && v.selectedCol < len(v.board.Columns) {
		return len(v.board.Columns[v.selectedCol].Issues)
	}
	return 0
}

// ViewName returns the view identifier.
func (v *BoardView) ViewName() string {
	return "pm.board"
}
