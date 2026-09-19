// util_footer.go - Footer content builders (keybinding hints + status messages).
// The bgFooter background bar is applied centrally by viewWrapper.Render, so
// every builder here returns plain styled content; never wrap with footerStyle
// directly from a builder.
package tuicore

import (
	"fmt"
	colorlib "image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var (
	// Every footer-side style includes bgFooter as its background. lipgloss
	// wraps each rendered span with an ANSI reset (`ESC[0m`) at the end —
	// that reset clears the *outer* footerStyle bg for everything after the
	// span. To keep the bar continuous, each inner span (and the plain
	// separator strings between them) re-establishes bg explicitly.
	footerStyle = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Background(bgFooter).
			Padding(0, 1, 0, 3)

	// KeyStyle renders a key glyph; view packages reuse it for inline key hints.
	KeyStyle = lipgloss.NewStyle().
			Foreground(BorderFocused).
			Background(bgFooter).
			Bold(true)

	// LabelStyle renders the label after a key glyph.
	LabelStyle = lipgloss.NewStyle().
			Foreground(TextNormal).
			Background(bgFooter)

	dimStyle = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Background(bgFooter)

	// sepStyle wraps plain separator strings (":" between key/label, "  "
	// between bindings) so they paint bg too.
	sepStyle = lipgloss.NewStyle().Background(bgFooter)

	// Global keys that appear dimmed in footer, in display order.
	// "/" and "@" are shown in sidebar instead (Search [/], Notifications [@]).
	// "I" is registered only on top-level extension list contexts, so it
	// surfaces dimmed there and is absent elsewhere.
	globalKeyOrder = []string{"tab", "f", "I", "q", "?"}
	globalKeys     = map[string]bool{"tab": true, "f": true, "I": true, "q": true, "?": true}

	// Hidden keys - functional but not shown in footer.
	// "/" and "@" are shown in sidebar; extension keys (S, P, R, V, M) are highlighted in sidebar.
	// Navigation aliases (j/k, ctrl+d/u, up/down, enter, home/end) are documented in ? help.
	hiddenKeys = map[string]bool{"esc": true, "/": true, "@": true, "%": true, "!": true, "S": true, "P": true, "R": true, "V": true, "M": true, "A": true, "D": true, "left": true, "right": true, "home": true, "end": true, "j": true, "k": true, "ctrl+d": true, "ctrl+u": true, "up": true, "down": true, "enter": true, "shift+tab": true}
)

// kv renders a "key:label" pair with bg-aware styling on every span,
// including the colon between key and label, so the bar background
// survives lipgloss's per-span reset codes.
func kv(key, label string, dim bool) string {
	labelS := LabelStyle
	if dim {
		labelS = dimStyle
	}
	return KeyStyle.Render(key) + sepStyle.Render(":") + labelS.Render(label)
}

// joinFooter joins styled footer parts with a bg-aware two-space separator
// so the bar background stays continuous through lipgloss resets.
func joinFooter(parts []string) string {
	return strings.Join(parts, sepStyle.Render("  "))
}

// renderSyncingFooter renders the syncing progress message.
func renderSyncingFooter() string {
	return KeyStyle.Render("Syncing workspace...")
}

// renderBackgroundSyncFooter renders a dim indicator while the post-startup
// background goroutine continues processing older commits and verifying
// identity bindings. The timeline is already interactive at this point.
func renderBackgroundSyncFooter() string {
	return dimStyle.Render("Background sync in progress...")
}

// RenderLoadingFooter renders a subtle loading indicator.
func RenderLoadingFooter() string {
	return dimStyle.Render("Loading...")
}

// renderFetchingFooter renders the fetching progress with dynamic info.
func renderFetchingFooter(repos, lists int) string {
	return KeyStyle.Render("Fetching...") + sepStyle.Render("  ") +
		LabelStyle.Render(fmt.Sprintf("%d repos from %d lists", repos, lists))
}

// importSpinnerFrames cycles for the animated glyph next to the import header.
var importSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderImportingFooter renders the import progress message with an animated
// spinner glyph. The glyph is derived from wall-clock time so it advances on
// every render — callers still need to drive periodic re-renders (e.g. via a
// ticker) for the animation to be visible.
func renderImportingFooter(repoURL, phase, detail string) string {
	frame := int(time.Now().UnixMilli()/100) % len(importSpinnerFrames)
	glyph := importSpinnerFrames[frame]
	head := "Importing"
	if repoURL != "" {
		head = "Importing from " + repoURL
	}
	result := KeyStyle.Render(glyph+" "+head) + sepStyle.Render("...")
	if phase != "" {
		result += sepStyle.Render("  ") + LabelStyle.Render(phase)
	}
	if detail != "" {
		result += sepStyle.Render(" ") + dimStyle.Render("("+detail+")")
	}
	return result
}

// renderPushingFooter renders the pushing progress message.
func renderPushingFooter(remote string) string {
	content := "Pushing..."
	if remote != "" {
		content = "Pushing to " + remote + "..."
	}
	return KeyStyle.Render(content)
}

// renderSavingFooter renders the saving progress message.
func renderSavingFooter() string {
	return KeyStyle.Render("Saving...")
}

// renderRetractingFooter renders the retracting progress message.
func renderRetractingFooter() string {
	return KeyStyle.Render("Retracting...")
}

// RenderMessageFooter renders a status message with appropriate color.
func RenderMessageFooter(message string, msgType MessageType) string {
	var color colorlib.Color
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
		Foreground(color).
		Background(bgFooter).
		Render(message)
}

// renderFooterInner renders a position prefix, the local bindings, then the global keys dimmed.
func renderFooterInner(registry *Registry, ctx Context, exclude, include map[string]bool, position string, skipQuit bool) string {
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
	if position != "" {
		parts = append(parts, LabelStyle.Render(position))
	}
	for _, b := range bindings {
		if isHidden(b.Key) || globalKeys[b.Key] {
			continue
		}
		if exclude != nil && exclude[b.Key] {
			continue
		}
		parts = append(parts, kv(b.Key, b.Label, false))
	}
	for _, key := range globalKeyOrder {
		if skipQuit && key == "q" {
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

// RenderFooter renders the keybinding hints for a view from the registry.
// Local bindings appear first, followed by global keys in fixed order.
// Pass nil for exclude to show all keys.
func RenderFooter(registry *Registry, ctx Context, exclude map[string]bool) string {
	return renderFooterInner(registry, ctx, exclude, nil, "", false)
}

// RenderFooterInclude renders footer like RenderFooter but force-shows keys in include
// that would normally be hidden by hiddenKeys.
func RenderFooterInclude(registry *Registry, ctx Context, exclude, include map[string]bool) string {
	return renderFooterInner(registry, ctx, exclude, include, "", false)
}

// RenderFooterWithPosition renders the keybinding hints for a view prefixed
// with a position indicator (current/total). Detail views are the only
// callers, and their footers carry many local bindings — to reduce clutter
// the global `q` (quit) hint is suppressed here. Users learn `q` once from
// list views; `?` (help) stays visible as the help signpost.
// Keys in `include` are force-shown although hiddenKeys would suppress them
// (e.g. view actions bound to letters reserved for global sidebar shortcuts,
// like M:merge); pass nil to show none.
func RenderFooterWithPosition(registry *Registry, ctx Context, current, total int, exclude, include map[string]bool) string {
	position := ""
	if total > 0 {
		position = fmt.Sprintf("%d/%d ", current, total)
	}
	return renderFooterInner(registry, ctx, exclude, include, position, true)
}
