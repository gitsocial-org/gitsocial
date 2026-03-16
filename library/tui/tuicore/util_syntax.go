// util_syntax.go - Unified syntax highlighting using Chroma
package tuicore

import (
	"path/filepath"
	"strings"

	"os"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"charm.land/lipgloss/v2"
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
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		chromaStyle = styles.Get("monokai")
	} else {
		chromaStyle = styles.Get("github")
	}
	// Dimmed style: map everything to gray
	builder := chroma.NewStyleBuilder("dimmed")
	builder.Add(chroma.Background, "#777777")
	builder.Add(chroma.Text, "#777777")
	builder.Add(chroma.Keyword, "#888888")
	builder.Add(chroma.KeywordType, "#888888")
	builder.Add(chroma.NameFunction, "#999999")
	builder.Add(chroma.LiteralString, "#777777")
	builder.Add(chroma.LiteralNumber, "#777777")
	builder.Add(chroma.Comment, "#666666")
	builder.Add(chroma.Operator, "#888888")
	builder.Add(chroma.Punctuation, "#777777")
	chromaDimStyle, _ = builder.Build()
	if chromaDimStyle == nil {
		chromaDimStyle = chromaStyle
	}
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
