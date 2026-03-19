// util_footer.go - Footer rendering with keybinding hints and status messages
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary)).
			Background(lipgloss.Color(BgFooter)).
			Padding(0, 1)

	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(BorderFocused)).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextNormal))

	// Global keys that appear dimmed in footer, in display order
	// "/" and "@" are shown in sidebar instead (Search [/], Notifications [@])
	globalKeyOrder = []string{"`", "f", "tab", "q", "?"}
	globalKeys     = map[string]bool{"`": true, "f": true, "tab": true, "q": true, "?": true}

	// Hidden keys - functional but not shown in footer
	// "/" and "@" are shown in sidebar; extension keys (T, B, etc.) are highlighted in sidebar
	// Navigation aliases (j/k, ctrl+d/u, up/down, enter, home/end) are documented in ? help
	hiddenKeys = map[string]bool{"esc": true, "/": true, "@": true, "%": true, "!": true, "T": true, "B": true, "P": true, "R": true, "A": true, "S": true, "D": true, "O": true, "left": true, "right": true, "home": true, "end": true, "j": true, "k": true, "ctrl+d": true, "ctrl+u": true, "up": true, "down": true, "enter": true}
)

// RenderSyncingFooter renders the syncing progress footer
func RenderSyncingFooter(width int) string {
	return footerStyle.Width(width).Render(keyStyle.Render("Syncing workspace..."))
}

// RenderLoadingFooter renders a subtle loading indicator in the footer
func RenderLoadingFooter(width int) string {
	return footerStyle.Width(width).Render(Dim.Render("Loading..."))
}

// RenderFetchingFooter renders the fetching progress footer with dynamic info
func RenderFetchingFooter(repos, lists, width int) string {
	content := keyStyle.Render("Fetching...") + "  " +
		labelStyle.Render(fmt.Sprintf("%d repos from %d lists", repos, lists))
	return footerStyle.Width(width).Render(content)
}

// RenderPushingFooter renders the pushing progress footer
func RenderPushingFooter(remote string, width int) string {
	content := "Pushing..."
	if remote != "" {
		content = "Pushing to " + remote + "..."
	}
	return footerStyle.Width(width).Render(keyStyle.Render(content))
}

// RenderSavingFooter renders the saving progress footer
func RenderSavingFooter(width int) string {
	return footerStyle.Width(width).Render(keyStyle.Render("Saving..."))
}

// RenderRetractingFooter renders the retracting progress footer
func RenderRetractingFooter(width int) string {
	return footerStyle.Width(width).Render(keyStyle.Render("Retracting..."))
}

// RenderMessageFooter renders a status message in the footer with appropriate color
func RenderMessageFooter(message string, msgType MessageType, width int) string {
	var color string
	switch msgType {
	case MessageTypeSuccess:
		color = StatusSuccess
	case MessageTypeWarning:
		color = StatusWarning
	case MessageTypeError:
		color = StatusError
	default:
		color = TextNormal
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	content := style.Render(message)
	return footerStyle.Width(width).Render(content)
}

// RenderChoicePromptFooter renders a pre-rendered choice prompt in the footer.
func RenderChoicePromptFooter(prompt string, width int) string {
	return footerStyle.Width(width).Render(prompt)
}

// RenderFooter renders footer with keybindings from registry.
// Local bindings appear first, followed by global keys in fixed order.
// Pass nil for exclude to show all keys.
func RenderFooter(registry *Registry, ctx Context, width int, exclude map[string]bool) string {
	return renderFooterInner(registry, ctx, width, exclude, nil)
}

// RenderFooterInclude renders footer like RenderFooter but force-shows keys in include
// that would normally be hidden by hiddenKeys.
func RenderFooterInclude(registry *Registry, ctx Context, width int, exclude, include map[string]bool) string {
	return renderFooterInner(registry, ctx, width, exclude, include)
}

// renderFooterInner is the shared implementation for RenderFooter and RenderFooterInclude.
func renderFooterInner(registry *Registry, ctx Context, width int, exclude, include map[string]bool) string {
	bindings := registry.ForContext(ctx)
	bindingMap := make(map[string]Binding)
	for _, b := range bindings {
		bindingMap[b.Key] = b
	}
	isHidden := func(key string) bool {
		if include != nil && include[key] {
			return false
		}
		return hiddenKeys[key]
	}
	var parts []string
	// First: local bindings (non-global, non-hidden)
	for _, b := range bindings {
		if isHidden(b.Key) || globalKeys[b.Key] {
			continue
		}
		if exclude != nil && exclude[b.Key] {
			continue
		}
		part := keyStyle.Render(b.Key) + ":" + labelStyle.Render(b.Label)
		parts = append(parts, part)
	}
	// Then: global keys in fixed order (dimmed)
	for _, key := range globalKeyOrder {
		if exclude != nil && exclude[key] {
			continue
		}
		if b, ok := bindingMap[key]; ok {
			part := keyStyle.Render(b.Key) + ":" + Dim.Render(b.Label)
			parts = append(parts, part)
		}
	}
	content := strings.Join(parts, "  ")
	return footerStyle.Width(width).Render(content)
}

// RenderFooterWithPosition renders footer with position indicator.
// Local bindings appear first, followed by global keys in fixed order.
func RenderFooterWithPosition(registry *Registry, ctx Context, width, current, total int, exclude map[string]bool) string {
	bindings := registry.ForContext(ctx)
	bindingMap := make(map[string]Binding)
	for _, b := range bindings {
		bindingMap[b.Key] = b
	}
	var parts []string
	if total > 0 {
		pos := fmt.Sprintf("%d/%d ", current, total)
		parts = append(parts, labelStyle.Render(pos))
	}
	// First: local bindings (non-global, non-hidden)
	for _, b := range bindings {
		if hiddenKeys[b.Key] || globalKeys[b.Key] {
			continue
		}
		if exclude != nil && exclude[b.Key] {
			continue
		}
		part := keyStyle.Render(b.Key) + ":" + labelStyle.Render(b.Label)
		parts = append(parts, part)
	}
	// Then: global keys in fixed order (dimmed)
	for _, key := range globalKeyOrder {
		if exclude != nil && exclude[key] {
			continue
		}
		if b, ok := bindingMap[key]; ok {
			part := keyStyle.Render(b.Key) + ":" + Dim.Render(b.Label)
			parts = append(parts, part)
		}
	}
	content := strings.Join(parts, "  ")
	return footerStyle.Width(width).Render(content)
}
