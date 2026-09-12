// util_theme.go - Theme state: the background, the renderers built from it, and the caches it fills
package tuicore

import (
	"fmt"
	"regexp"

	"charm.land/glamour/v2"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// theme holds every value that follows the terminal background, with the render caches a swap clears.
type theme struct {
	dark              bool
	markdown          *glamour.TermRenderer
	mutedMarkdown     *glamour.TermRenderer
	chromaStyle       *chroma.Style
	chromaDimStyle    *chroma.Style
	focusedLinkMarker string
	glamourCache      map[string]string
	lineCache         map[string]string
	codeCache         map[string]string
	searchQuery       string
	searchPattern     *regexp.Regexp
}

// currentTheme is the one piece of theme state; SetDarkBackground swaps it whole.
var currentTheme = newTheme(true)

// newTheme builds the renderers and empty caches for the given background.
func newTheme(dark bool) *theme {
	t := &theme{
		dark:              dark,
		chromaStyle:       chromaStyleFor(dark),
		focusedLinkMarker: focusedLinkMarkerFor(dark),
		glamourCache:      make(map[string]string, 256),
		lineCache:         make(map[string]string, 4096),
		codeCache:         make(map[string]string, 256),
	}
	t.chromaDimStyle = chromaDimStyleFor(dark, t.chromaStyle)
	t.markdown, t.mutedMarkdown = markdownRenderersFor(dark)
	return t
}

// SetDarkBackground applies the display.theme background, rebuilding the renderers and clearing the caches.
func SetDarkBackground(dark bool) {
	if dark == currentTheme.dark {
		return
	}
	currentTheme = newTheme(dark)
}

// pickFor returns the dark or light string for the given background.
func pickFor(dark bool, darkStr, lightStr string) string {
	if dark {
		return darkStr
	}
	return lightStr
}

// chromaStyleFor picks the syntax highlighting theme matching the background.
func chromaStyleFor(dark bool) *chroma.Style {
	if dark {
		return styles.Get("monokai")
	}
	return styles.Get("github")
}

// chromaDimStyleFor builds the dimmed syntax style for stale and retracted code: one low-contrast gray.
func chromaDimStyleFor(dark bool, fallback *chroma.Style) *chroma.Style {
	dim := pickFor(dark, grayDimDark, grayDimLight)
	builder := chroma.NewStyleBuilder("dimmed")
	for _, token := range []chroma.TokenType{
		chroma.Background, chroma.Text, chroma.Keyword, chroma.KeywordType, chroma.NameFunction,
		chroma.LiteralString, chroma.LiteralNumber, chroma.Comment, chroma.Operator, chroma.Punctuation,
	} {
		builder.Add(token, dim)
	}
	built, err := builder.Build()
	if err != nil || built == nil {
		return fallback
	}
	return built
}

// markdownRenderersFor builds the normal and muted glamour renderers for the background.
func markdownRenderersFor(dark bool) (normal, muted *glamour.TermRenderer) {
	style := "light"
	if dark {
		style = "dark"
	}
	// The body color is pinned so dark keeps its brighter paragraph and light stays near-black.
	body := pickFor(dark, grayPrimaryDark, grayPrimaryLight)
	bodyJSON := fmt.Sprintf(`{"document":{"margin":0,"color":%q},"paragraph":{"color":%q}}`, body, body)
	normal, _ = glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(0),
		glamour.WithStylesFromJSONBytes([]byte(bodyJSON)),
	)
	mutedColor := pickFor(dark, graySecondaryDark, graySecondaryLight)
	mutedJSON := fmt.Sprintf(`{"document":{"margin":0,"color":%q},"paragraph":{"color":%q},"code_block":{"color":%q},"link":{"color":%q},"link_text":{"color":%q}}`,
		mutedColor, mutedColor, mutedColor, mutedColor, mutedColor)
	muted, _ = glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(0),
		glamour.WithStylesFromJSONBytes([]byte(mutedJSON)),
	)
	return normal, muted
}

// focusedLinkMarkerFor builds the focused-link prefix: bold, underline and a theme-aware highlight background.
func focusedLinkMarkerFor(dark bool) string {
	return "\x1b[1;4;48;5;" + pickFor(dark, graySelectedDark, graySelectedLight) + "m"
}
