// version_picker.go - Reusable version history picker component
package tuicore

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// VersionItem is implemented by extension-specific version types.
type VersionItem interface {
	GetID() string
	GetTimestamp() time.Time
	GetEditOf() string
	IsRetracted() bool
	RenderListEntry(index, total int, label string, selected bool, width int) string
	RenderDetail(width int) string
}

// VersionPicker is a reusable component for browsing edit history.
type VersionPicker struct {
	items      []VersionItem
	cursor     int
	viewport   viewport.Model
	detailMode bool
	loading    bool
	width      int
	height     int
	zonePrefix string
}

// NewVersionPicker creates a new version picker.
func NewVersionPicker() *VersionPicker {
	return &VersionPicker{
		viewport:   viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		zonePrefix: zone.NewPrefix(),
	}
}

// SetSize updates the picker dimensions.
func (p *VersionPicker) SetSize(width, height int) {
	p.width = width
	p.height = height
	p.viewport.SetWidth(width)
	p.viewport.SetHeight(height - 3) // -3 for footer
}

// SetLoading sets the loading state.
func (p *VersionPicker) SetLoading(loading bool) {
	p.loading = loading
	if loading {
		p.items = nil
		p.cursor = 0
		p.detailMode = false
	}
}

// SetItems sets the version items and refreshes display.
func (p *VersionPicker) SetItems(items []VersionItem) {
	p.items = items
	p.loading = false
	if len(items) > 0 {
		p.viewport.SetContent(p.renderList())
	}
}

// Items returns the current items.
func (p *VersionPicker) Items() []VersionItem {
	return p.items
}

// IsLoading returns the loading state.
func (p *VersionPicker) IsLoading() bool {
	return p.loading
}

// IsDetailMode returns whether detail mode is active.
func (p *VersionPicker) IsDetailMode() bool {
	return p.detailMode
}

// Cursor returns the current cursor position.
func (p *VersionPicker) Cursor() int {
	return p.cursor
}

// SelectedItem returns the currently selected item.
func (p *VersionPicker) SelectedItem() VersionItem {
	if p.cursor >= 0 && p.cursor < len(p.items) {
		return p.items[p.cursor]
	}
	return nil
}

// HandleMouse processes mouse input, returns true if handled.
func (p *VersionPicker) HandleMouse(msg tea.MouseMsg) (handled bool, cmd tea.Cmd) {
	if len(p.items) == 0 {
		return false, nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m := msg.Mouse()
		if m.Button == tea.MouseWheelUp {
			if p.detailMode {
				p.viewport.HalfPageUp()
			} else if p.cursor > 0 {
				p.cursor--
				p.viewport.SetContent(p.renderList())
			}
		} else {
			if p.detailMode {
				p.viewport.HalfPageDown()
			} else if p.cursor < len(p.items)-1 {
				p.cursor++
				p.viewport.SetContent(p.renderList())
			}
		}
		return true, nil
	case tea.MouseClickMsg:
		if p.detailMode {
			return true, nil
		}
		idx := ZoneClicked(msg, len(p.items), p.zonePrefix)
		if idx < 0 {
			return true, nil
		}
		if idx == p.cursor {
			p.detailMode = true
			p.viewport.SetContent(p.renderDetail())
			p.viewport.GotoTop()
			return true, nil
		}
		p.cursor = idx
		p.viewport.SetContent(p.renderList())
		return true, nil
	}
	return false, nil
}

// HandleKey processes keyboard input, returns true if handled.
func (p *VersionPicker) HandleKey(key string) (handled bool, cmd tea.Cmd) {
	if len(p.items) == 0 {
		if key == "esc" {
			return true, func() tea.Msg {
				return NavigateMsg{Action: NavBack}
			}
		}
		return false, nil
	}

	if p.detailMode {
		return p.handleDetailKey(key)
	}
	return p.handleListKey(key)
}

// handleListKey handles keys in list mode.
func (p *VersionPicker) handleListKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		if p.cursor < len(p.items)-1 {
			p.cursor++
			p.viewport.SetContent(p.renderList())
		}
		return true, nil
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.viewport.SetContent(p.renderList())
		}
		return true, nil
	case "g":
		p.cursor = 0
		p.viewport.SetContent(p.renderList())
		return true, nil
	case "G":
		p.cursor = len(p.items) - 1
		p.viewport.SetContent(p.renderList())
		return true, nil
	case "enter":
		p.detailMode = true
		p.viewport.SetContent(p.renderDetail())
		p.viewport.GotoTop()
		return true, nil
	case "ctrl+d", "pgdown":
		p.viewport.HalfPageDown()
		return true, nil
	case "ctrl+u", "pgup":
		p.viewport.HalfPageUp()
		return true, nil
	case "home":
		p.cursor = 0
		p.viewport.SetContent(p.renderList())
		return true, nil
	case "end":
		p.cursor = len(p.items) - 1
		p.viewport.SetContent(p.renderList())
		return true, nil
	case "esc":
		return true, func() tea.Msg {
			return NavigateMsg{Action: NavBack}
		}
	}
	return false, nil
}

// handleDetailKey handles keys in detail mode.
func (p *VersionPicker) handleDetailKey(key string) (bool, tea.Cmd) {
	switch key {
	case "esc":
		p.detailMode = false
		p.viewport.SetContent(p.renderList())
		p.viewport.GotoTop()
		return true, nil
	case "left":
		if p.cursor > 0 {
			p.cursor--
			p.viewport.SetContent(p.renderDetail())
			p.viewport.GotoTop()
		}
		return true, nil
	case "right":
		if p.cursor < len(p.items)-1 {
			p.cursor++
			p.viewport.SetContent(p.renderDetail())
			p.viewport.GotoTop()
		}
		return true, nil
	case "ctrl+d", "pgdown":
		p.viewport.HalfPageDown()
		return true, nil
	case "ctrl+u", "pgup":
		p.viewport.HalfPageUp()
		return true, nil
	}
	return false, nil
}

// Render returns the picker content.
func (p *VersionPicker) Render() string {
	if p.loading {
		return Dim.Render("Loading history...")
	}
	if len(p.items) == 0 {
		return Dim.Render("No edit history")
	}
	return p.viewport.View()
}

// renderList renders the list view.
func (p *VersionPicker) renderList() string {
	if len(p.items) == 0 {
		return "No versions found."
	}
	total := len(p.items)
	var content string
	content += Title.Render("Edit History") + "\n"
	content += Dim.Render(fmt.Sprintf("%d version(s)", total)) + "\n\n"
	for i, item := range p.items {
		label := VersionLabel(i, total, item.GetEditOf() != "")
		selected := i == p.cursor
		entry := item.RenderListEntry(i, total, label, selected, p.width)
		entryLines := strings.Split(entry, "\n")
		if len(entryLines) > 0 {
			entryLines[0] = MarkZone(ZoneID(p.zonePrefix, i), entryLines[0])
		}
		content += strings.Join(entryLines, "\n") + "\n"
	}
	return content
}

// renderDetail renders the detail view.
func (p *VersionPicker) renderDetail() string {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return ""
	}
	item := p.items[p.cursor]
	total := len(p.items)
	label := VersionLabel(p.cursor, total, item.GetEditOf() != "")
	header := Title.Render(fmt.Sprintf("Version %d of %d (%s)", total-p.cursor, total, label))
	return header + "\n\n" + item.RenderDetail(p.width)
}

// VersionLabel returns the display label for a version at index.
func VersionLabel(index, total int, hasEditOf bool) string {
	if index == 0 {
		return "current"
	}
	if index == total-1 && !hasEditOf {
		return "original"
	}
	return fmt.Sprintf("v%d", total-index)
}
