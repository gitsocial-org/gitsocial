// util_syntax.go - Unified syntax highlighting using Chroma
package tuicore

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var (
	chromaFormatter chroma.Formatter
	chromaStyle     *chroma.Style
	chromaDimStyle  *chroma.Style

	// highlightLineCache caches HighlightLine results keyed by language+dimmed+content.
	highlightLineCache = make(map[string]string, 4096)
	// highlightCodeCache caches HighlightCode results keyed by language+dimmed+content.
	highlightCodeCache = make(map[string]string, 256)
)

func init() {
	chromaFormatter = formatters.TTY256
	// Build theme-dependent state for the default (dark) background. The TUI
	// detects the real terminal background and applies display.theme in Run();
	// doing it there rather than here keeps package init from issuing a blocking
	// terminal query, which hangs headless test binaries that import tuicore.
	refreshThemeState()
}

// refreshThemeState rebuilds every piece of theme-dependent render state from
// the current DarkBackground. Called after detection and on SetDarkBackground.
func refreshThemeState() {
	selectChromaStyle()
	buildChromaDimStyle()
	buildMarkdownRenderers()
	FocusedLinkMarker = focusedLinkMarker()
}

// selectChromaStyle picks the syntax highlighting theme matching DarkBackground.
func selectChromaStyle() {
	if DarkBackground {
		chromaStyle = styles.Get("monokai")
	} else {
		chromaStyle = styles.Get("github")
	}
}

// buildChromaDimStyle builds the dimmed syntax style for stale/retracted code:
// grays that read as low-contrast against the current background (darker on
// dark, lighter on light), preserving the relative dimming hierarchy.
func buildChromaDimStyle() {
	dim := pickThemeColor(grayDimDark, grayDimLight)
	builder := chroma.NewStyleBuilder("dimmed")
	builder.Add(chroma.Background, dim)
	builder.Add(chroma.Text, dim)
	builder.Add(chroma.Keyword, dim)
	builder.Add(chroma.KeywordType, dim)
	builder.Add(chroma.NameFunction, dim)
	builder.Add(chroma.LiteralString, dim)
	builder.Add(chroma.LiteralNumber, dim)
	builder.Add(chroma.Comment, dim)
	builder.Add(chroma.Operator, dim)
	builder.Add(chroma.Punctuation, dim)
	chromaDimStyle, _ = builder.Build()
	if chromaDimStyle == nil {
		chromaDimStyle = chromaStyle
	}
}

// SetDarkBackground overrides the theme background (from the display.theme
// setting) and refreshes theme-dependent syntax state. Call before the first
// render; a no-op when the value is unchanged.
func SetDarkBackground(dark bool) {
	if dark == DarkBackground {
		return
	}
	DarkBackground = dark
	refreshThemeState()
	clear(highlightLineCache)
	clear(highlightCodeCache)
	clear(glamourCache.entries)
}

// HighlightCode highlights a full code block with syntax coloring.
func HighlightCode(code, language string, dimmed bool) string {
	d := byte('0')
	if dimmed {
		d = '1'
	}
	key := language + "\x00" + string(d) + "\x00" + code
	if cached, ok := highlightCodeCache[key]; ok {
		return cached
	}
	lexer := resolveLexer(language)
	style := chromaStyle
	if dimmed {
		style = chromaDimStyle
	}
	tokens, err := chroma.Tokenise(lexer, nil, code)
	if err != nil {
		return code
	}
	var buf strings.Builder
	err = chromaFormatter.Format(&buf, style, chroma.Literator(tokens...))
	if err != nil {
		return code
	}
	result := buf.String()
	if len(highlightCodeCache) >= 256 {
		highlightCodeCache = make(map[string]string, 256)
	}
	highlightCodeCache[key] = result
	return result
}

// HighlightLine highlights a single line of code for use in diffs.
func HighlightLine(line, language string, dimmed bool) string {
	d := byte('0')
	if dimmed {
		d = '1'
	}
	key := language + "\x00" + string(d) + "\x00" + line
	if cached, ok := highlightLineCache[key]; ok {
		return cached
	}
	lexer := resolveLexer(language)
	style := chromaStyle
	if dimmed {
		style = chromaDimStyle
	}
	tokens, err := chroma.Tokenise(lexer, nil, line)
	if err != nil {
		return line
	}
	var buf strings.Builder
	err = chromaFormatter.Format(&buf, style, chroma.Literator(tokens...))
	if err != nil {
		return line
	}
	// Strip all newlines — chroma may place \n before ANSI reset codes
	result := strings.ReplaceAll(buf.String(), "\n", "")
	if len(highlightLineCache) >= 4096 {
		highlightLineCache = make(map[string]string, 4096)
	}
	highlightLineCache[key] = result
	return result
}

// DetectLanguage detects the programming language from a filename.
func DetectLanguage(filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		return ""
	}
	config := lexer.Config()
	if config == nil {
		return ""
	}
	return strings.ToLower(config.Name)
}

// resolveLexer returns a lexer for the given language, falling back to plaintext.
func resolveLexer(language string) chroma.Lexer {
	if language != "" {
		lexer := lexers.Get(language)
		if lexer != nil {
			return chroma.Coalesce(lexer)
		}
	}
	return chroma.Coalesce(lexers.Fallback)
}

// DetectLanguageFromPath detects language from a file path (uses extension).
func DetectLanguageFromPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return DetectLanguage(path)
	}
	lang := DetectLanguage("file" + ext)
	if lang == "diff" {
		return ""
	}
	return lang
}
