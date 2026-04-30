// generate.go - Markdown generation from collected keybinding documentation
package tuikeydoc

import (
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

// Generate produces markdown documentation from collected domain docs.
func Generate(domains []DomainDoc) string {
	var b strings.Builder
	b.WriteString("# TUI Keybindings\n\n")
	writeSharedNavigation(&b)
	writeGlobalKeys(&b)
	for _, domain := range domains {
		writeDomain(&b, domain)
	}
	writeMouseSupport(&b)
	writeConfirmationDialogs(&b)
	return b.String()
}

// writeSharedNavigation writes the shared component navigation tables.
func writeSharedNavigation(b *strings.Builder) {
	b.WriteString("## Shared Navigation\n\n")
	writeComponentTable(b, "CardList", CardListKeys, componentUsedBy("CardList"))
	writeComponentTable(b, "SectionList", SectionListKeys, componentUsedBy("SectionList"))
	writeComponentTable(b, "VersionPicker", VersionPickerKeys, componentUsedBy("VersionPicker"))
	b.WriteString("---\n\n")
}

// writeComponentTable writes a single component's key table.
func writeComponentTable(b *strings.Builder, name string, keys []KeyDoc, usedBy string) {
	fmt.Fprintf(b, "### %s (used by: %s)\n\n", name, usedBy)
	b.WriteString("| Key | Action |\n")
	b.WriteString("|-----|--------|\n")
	for _, k := range keys {
		fmt.Fprintf(b, "| `%s` | %s |\n", k.Key, k.Label)
	}
	b.WriteString("\n")
}

// writeGlobalKeys writes the global keys section.
func writeGlobalKeys(b *strings.Builder) {
	b.WriteString("## Global Keys\n\n")
	// Core keys
	b.WriteString("### Core Keys\n\n")
	b.WriteString("| Key | Action | Scope |\n")
	b.WriteString("|-----|--------|-------|\n")
	b.WriteString("| `esc` | Go back | Everywhere except Timeline |\n")
	for _, ck := range tuicore.CoreKeys {
		fmt.Fprintf(b, "| `%s` | %s | Everywhere except %s |\n",
			ck.Key, capitalize(ck.Label), titleForContext(ck.Target))
	}
	b.WriteString("| `f` | Fetch updates | Everywhere except Detail/Thread/History |\n")
	b.WriteString("| `/` | Search | Everywhere except Search |\n")
	b.WriteString("| `tab` | Toggle nav/content focus | Global |\n")
	b.WriteString("| `q` | Quit | Global |\n")
	b.WriteString("| `?` | Help | Global |\n")
	b.WriteString("\n")
	// Extension keys
	b.WriteString("### Extension Keys (uppercase, shown in sidebar)\n\n")
	b.WriteString("| Key | Extension | Target View | Status |\n")
	b.WriteString("|-----|-----------|-------------|--------|\n")
	for _, ek := range tuicore.ExtensionKeys {
		status := "Active"
		target := titleForContext(ek.Target)
		if ek.Target == tuicore.Global {
			status = "Planned"
			target = capitalize(ek.Label)
		}
		ext := domainToExtensionName(ek.Domain)
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", ek.Key, ext, target, status)
	}
	b.WriteString("\n---\n\n")
}

// writeDomain writes all contexts in a domain.
func writeDomain(b *strings.Builder, domain DomainDoc) {
	fmt.Fprintf(b, "## %s\n\n", domain.Title)
	for _, ctx := range domain.Contexts {
		writeContext(b, ctx)
	}
	b.WriteString("---\n\n")
}

// writeContext writes a single context's key table.
func writeContext(b *strings.Builder, ctx ContextDoc) {
	fmt.Fprintf(b, "### %s\n\n", ctx.Title)
	b.WriteString("| Key | Action |\n")
	b.WriteString("|-----|--------|\n")
	if ctx.Component != "" {
		fmt.Fprintf(b, "| %s navigation | (see Shared Navigation) |\n", ctx.Component)
	}
	// Filter out keys that are global or hidden from per-view display
	globalKeys := map[string]bool{
		"q": true, "?": true,
		"T": true, "B": true, "P": true, "R": true,
		"@": true, "%": true,
	}
	for _, k := range ctx.Keys {
		if globalKeys[k.Key] {
			continue
		}
		// Skip global esc/back (but keep view-specific esc like "exit input", "exit mode")
		if k.Key == "esc" && k.Label == "back" {
			continue
		}
		// Skip global fetch binding (but keep view-specific "f" like fold hunk)
		if k.Key == "f" && k.Label == "fetch" {
			continue
		}
		// Skip "/" for CardList views since it's a global key
		if k.Key == "/" && ctx.Component == "CardList" {
			continue
		}
		fmt.Fprintf(b, "| `%s` | %s |\n", k.Key, capitalize(k.Label))
	}
	b.WriteString("\n")
}

// capitalize uppercases the first letter of a string.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// domainToExtensionName maps domain IDs to display names.
func domainToExtensionName(domain string) string {
	names := map[string]string{
		"social":    "Social",
		"pm":        "PM",
		"review":    "Review",
		"release":   "Release",
		"cicd":      "CI/CD",
		"infra":     "Infrastructure",
		"ops":       "Operations",
		"security":  "Security",
		"dm":        "DM",
		"portfolio": "Portfolio",
	}
	if n, ok := names[domain]; ok {
		return n
	}
	return domain
}

// titleForContext returns the display title for a context from ViewMeta.
func titleForContext(ctx tuicore.Context) string {
	for _, meta := range tuicore.AllViewMetas() {
		if meta.Context == ctx {
			return meta.Title
		}
	}
	return string(ctx)
}

// componentUsedBy returns a comma-separated list of view titles using a component.
func componentUsedBy(component string) string {
	seen := make(map[tuicore.Context]bool)
	var titles []string
	for _, meta := range tuicore.AllViewMetas() {
		if meta.Component == component && !seen[meta.Context] {
			seen[meta.Context] = true
			titles = append(titles, meta.Title)
		}
	}
	return strings.Join(titles, ", ")
}

// writeMouseSupport writes the mouse support section.
func writeMouseSupport(b *strings.Builder) {
	b.WriteString("## Mouse Support\n\n")
	b.WriteString("All views support mouse wheel scrolling and click-to-select/activate. CardList and SectionList views provide full mouse support including link zone clicking via the AnchorCollector system. Simple list views (List Picker, List Repos, Repository Lists, PM Config, Settings, Config) support wheel scroll and click-to-select/activate via zone marking. Board view supports column header clicks and issue selection. Cache view is action-based with no cursor, so mouse is not applicable.\n\n")
}

// writeConfirmationDialogs writes the confirmation dialogs section.
func writeConfirmationDialogs(b *strings.Builder) {
	b.WriteString("## Confirmation Dialogs\n\n")
	b.WriteString("Retract, delete, merge, close, and remove actions show a `[y/n]` confirmation prompt:\n")
	b.WriteString("- `y` / `Y` - Confirm action\n")
	b.WriteString("- `n` / `N` / `esc` - Cancel\n\n")
	b.WriteString("All confirmations use the shared `ConfirmDialog` component.\n")
}
