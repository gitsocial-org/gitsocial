// util_diffrow_adapter.go - Bridges tuicore (Chroma + color constants) and
// the package-pure diff row model. DiffViewCore calls DefaultDiffPalette()
// and DefaultHighlight() to feed BuildPlan without the diff package
// reaching back into tuicore.
package tuicore

import (
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore/diff"
)

// init configures the diff renderer for the host terminal's color profile.
// On ≤ANSI profiles (4-bit / 16 colors and below) the muted DiffAddedBg /
// DiffRemovedBg / DiffFileHeaderBg hex values all collapse to indistinct
// "dark gray" via lipgloss's nearest-color downgrade. Suppress LineBG in
// those cases so the diff stays legible via FG + sign alone.
func init() {
	if colorprofile.Detect(os.Stdout, os.Environ()) <= colorprofile.ANSI {
		diff.SetSuppressLineBG(true)
	}
}

// DefaultDiffPalette returns a diff.Palette populated from the tuicore
// color constants.
func DefaultDiffPalette() diff.Palette {
	return diff.Palette{
		AddedFG:            DiffAdded,
		RemovedFG:          DiffRemoved,
		AddedBG:            DiffAddedBg,
		RemovedBG:          DiffRemovedBg,
		IntraLineAddedBG:   DiffIntraLineAddedBg,
		IntraLineRemovedBG: DiffIntraLineRemovedBg,
		FileHeaderBG:       DiffFileHeaderBg,
		HunkHeaderFG:       DiffHunkHeader,
		LineNumFG:          DiffLineNum,
		TextSecondary:      TextSecondary,
	}
}

// DefaultHighlight returns a diff.Highlight closure that tokenizes via
// Chroma and emits one Cell per token. Falls back to a single plain-text
// cell on tokenize error or unknown language. `path` is mapped to a
// chroma language via DetectLanguageFromPath.
//
// The closure memoizes the resolved lexer per path: chroma's filename
// matching walks every registered lexer's glob patterns via filepath.Match
// — without this cache, a 10k-line file becomes 10k pattern-match scans.
func DefaultHighlight() diff.Highlight {
	lexerCache := make(map[string]chroma.Lexer)
	return func(line, path string) []diff.Cell {
		if line == "" {
			return nil
		}
		lexer, ok := lexerCache[path]
		if !ok {
			lexer = resolveLexer(DetectLanguageFromPath(path))
			lexerCache[path] = lexer
		}
		tokens, err := chroma.Tokenise(lexer, nil, line)
		if err != nil {
			return []diff.Cell{{Text: line}}
		}
		var cells []diff.Cell
		for _, tok := range tokens {
			// Some chroma lexers (e.g. Rust's CommentSingle) append a trailing
			// "\n" to the token value even when the input had none. A newline
			// inside a styled cell breaks lipgloss rendering: the terminal
			// processes the \n mid-row, the padding lands on the next visual
			// line, and the row's BG visibly cuts off short of the right edge.
			value := strings.ReplaceAll(tok.Value, "\n", "")
			if value == "" {
				continue
			}
			entry := chromaStyle.Get(tok.Type)
			cell := diff.Cell{Text: value}
			//nolint:misspell // chroma exposes the British spelling
			if entry.Colour.IsSet() {
				cell.FG = entry.Colour.String()
			}
			// Chroma styles paint comments dim by design (e.g. monokai's
			// #75715E), which collapses contrast on the dark Diff{Added,
			// Removed}Bg rows. Override comment cells with a lighter muted
			// gray so comment text stays readable on diff backgrounds.
			if tok.Type.InCategory(chroma.Comment) {
				cell.FG = DiffCommentFG
			}
			if entry.Bold == chroma.Yes {
				cell.Bold = true
			}
			if entry.Italic == chroma.Yes {
				cell.Italic = true
			}
			if entry.Underline == chroma.Yes {
				cell.Underline = true
			}
			cells = append(cells, cell)
		}
		if len(cells) == 0 {
			return []diff.Cell{{Text: line}}
		}
		return cells
	}
}
