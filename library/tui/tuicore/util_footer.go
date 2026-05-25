// util_footer.go - Footer content builders (keybinding hints + status messages).
// The BgFooter background bar is applied centrally by ViewWrapper.Render, so
// every builder here returns plain styled content; never wrap with footerStyle
// directly from a builder.
package tuicore

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	// Every footer-side style includes BgFooter as its background. lipgloss
	// wraps each rendered span with an ANSI reset (`ESC[0m`) at the end —
	// that reset clears the *outer* footerStyle bg for everything after the
	// span. To keep the bar continuous, each inner span (and the plain
	// separator strings between them) re-establishes bg explicitly.
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary)).
			Background(lipgloss.Color(BgFooter)).
			Padding(0, 1, 0, 3)

	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(BorderFocused)).
			Background(lipgloss.Color(BgFooter)).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextNormal)).
			Background(lipgloss.Color(BgFooter))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary)).
			Background(lipgloss.Color(BgFooter))

	// sepStyle wraps plain separator strings (":" between key/label, "  "
	// between bindings) so they paint bg too.
	sepStyle = lipgloss.NewStyle().Background(lipgloss.Color(BgFooter))

	// Global keys that appear dimmed in footer, in display order.
	// "/" and "@" are shown in sidebar instead (Search [/], Notifications [@]).
	globalKeyOrder = []string{"tab", "f", "q", "?"}
	globalKeys     = map[string]bool{"tab": true, "f": true, "q": true, "?": true}

	// Hidden keys - functional but not shown in footer.
	// "/" and "@" are shown in sidebar; extension keys (S, P, R, V, M, Y, ...) are highlighted in sidebar.
	// Navigation aliases (j/k, ctrl+d/u, up/down, enter, home/end) are documented in ? help.
	hiddenKeys = map[string]bool{"esc": true, "/": true, "@": true, "%": true, "!": true, "S": true, "P": true, "R": true, "V": true, "M": true, "Y": true, "A": true, "D": true, "O": true, "left": true, "right": true, "home": true, "end": true, "j": true, "k": true, "ctrl+d": true, "ctrl+u": true, "up": true, "down": true, "enter": true, "shift+tab": true}
)

// kv renders a "key:label" pair with bg-aware styling on every span,
// including the colon between key and label, so the bar background
// survives lipgloss's per-span reset codes.
func kv(key, label string, dim bool) string {
	labelS := labelStyle
	if dim {
		labelS = dimStyle
	}
	return keyStyle.Render(key) + sepStyle.Render(":") + labelS.Render(label)
}

// joinFooter joins styled footer parts with a bg-aware two-space separator
// so the bar background stays continuous through lipgloss resets.
func joinFooter(parts []string) string {
	return strings.Join(parts, sepStyle.Render("  "))
}

// RenderSyncingFooter renders the syncing progress message.
func RenderSyncingFooter() string {
	return keyStyle.Render("Syncing workspace...")
}

// RenderBackgroundSyncFooter renders a dim indicator while the post-startup
// background goroutine continues processing older commits and verifying
// identity bindings. The timeline is already interactive at this point.
func RenderBackgroundSyncFooter() string {
	return dimStyle.Render("Background sync in progress...")
}

// RenderLoadingFooter renders a subtle loading indicator.
func RenderLoadingFooter() string {
	return dimStyle.Render("Loading...")
}

// RenderFetchingFooter renders the fetching progress with dynamic info.
func RenderFetchingFooter(repos, lists int) string {
	return keyStyle.Render("Fetching...") + sepStyle.Render("  ") +
		labelStyle.Render(fmt.Sprintf("%d repos from %d lists", repos, lists))
}

// RenderPushingFooter renders the pushing progress message.
func RenderPushingFooter(remote string) string {
	content := "Pushing..."
	if remote != "" {
		content = "Pushing to " + remote + "..."
	}
	return keyStyle.Render(content)
}

// RenderSavingFooter renders the saving progress message.
func RenderSavingFooter() string {
	return keyStyle.Render("Saving...")
}

// RenderRetractingFooter renders the retracting progress message.
func RenderRetractingFooter() string {
	return keyStyle.Render("Retracting...")
}

// RenderMessageFooter renders a status message with appropriate color.
func RenderMessageFooter(message string, msgType MessageType) string {
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
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Background(lipgloss.Color(BgFooter)).
		Render(message)
}

// RenderFooter renders the keybinding hints for a view from the registry.
// Local bindings appear first, followed by global keys in fixed order.
// Pass nil for exclude to show all keys.
func RenderFooter(registry *Registry, ctx Context, exclude map[string]bool) string {
	return renderFooterInner(registry, ctx, exclude, nil)
}

// RenderFooterInclude renders footer like RenderFooter but force-shows keys in include
// that would normally be hidden by hiddenKeys.
func RenderFooterInclude(registry *Registry, ctx Context, exclude, include map[string]bool) string {
	return renderFooterInner(registry, ctx, exclude, include)
}

// renderFooterInner is the shared implementation for RenderFooter and RenderFooterInclude.
func renderFooterInner(registry *Registry, ctx Context, exclude, include map[string]bool) string {
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
	// First: local bindings (non-global, non-hidden).
	for _, b := range bindings {
		if isHidden(b.Key) || globalKeys[b.Key] {
			continue
		}
		if exclude != nil && exclude[b.Key] {
			continue
		}
		parts = append(parts, kv(b.Key, b.Label, false))
	}
	// Then: global keys in fixed order (dimmed).
	for _, key := range globalKeyOrder {
		if exclude != nil && exclude[key] {
			continue
		}
		if b, ok := bindingMap[key]; ok {
			parts = append(parts, kv(b.Key, b.Label, true))
		}
	}
	return joinFooter(parts)
}

// RenderFooterWithPosition renders the keybinding hints for a view prefixed
// with a position indicator (current/total). Detail views are the only
// callers, and their footers carry many local bindings — to reduce clutter
// the global `q` (quit) hint is suppressed here. Users learn `q` once from
// list views; `?` (help) stays visible as the help signpost.
func RenderFooterWithPosition(registry *Registry, ctx Context, current, total int, exclude map[string]bool) string {
	bindings := registry.ForContext(ctx)
	bindingMap := make(map[string]Binding)
	for _, b := range bindings {
		bindingMap[b.Key] = b
	}
	var parts []string
	if total > 0 {
		parts = append(parts, labelStyle.Render(fmt.Sprintf("%d/%d ", current, total)))
	}
	// First: local bindings (non-global, non-hidden).
	for _, b := range bindings {
		if hiddenKeys[b.Key] || globalKeys[b.Key] {
			continue
		}
		if exclude != nil && exclude[b.Key] {
			continue
		}
		parts = append(parts, kv(b.Key, b.Label, false))
	}
	// Then: global keys in fixed order (dimmed), minus `q`.
	for _, key := range globalKeyOrder {
		if key == "q" {
			continue
		}
		if exclude != nil && exclude[key] {
			continue
		}
		if b, ok := bindingMap[key]; ok {
			parts = append(parts, kv(b.Key, b.Label, true))
		}
	}
	return joinFooter(parts)
}
