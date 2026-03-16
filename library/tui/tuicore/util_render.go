// util_render.go - Rendering utilities: styles, time formatting, row rendering, and markdown/math processing
package tuicore

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// StyleTextInput applies prompt, text, and placeholder styles to a textinput.
func StyleTextInput(input *textinput.Model, prompt, text, placeholder lipgloss.Style) {
	s := input.Styles()
	s.Focused.Prompt = prompt
	s.Focused.Text = text
	s.Focused.Placeholder = placeholder
	s.Blurred.Prompt = prompt
	s.Blurred.Text = text
	s.Blurred.Placeholder = placeholder
	input.SetStyles(s)
}

// Content panel padding (applied globally by host.renderFrame)
const (
	ContentPaddingTop    = 1 // blank lines above content
	ContentPaddingLeft   = 2 // spaces before content
	ContentPaddingRight  = 3 // spaces after content
	ContentPaddingBottom = 0 // lines below content (before footer)
)

var (
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(IdentityFollowing))

	TitleSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityFollowing)).
			Background(lipgloss.Color(BgSelected))

	Selected = lipgloss.NewStyle().
			Background(lipgloss.Color(BgSelected)).
			Foreground(lipgloss.Color(TextPrimary))

	Normal = lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextNormal))

	NormalSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextNormal)).
			Background(lipgloss.Color(BgSelected))

	Dim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextSecondary))

	DimSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary)).
			Background(lipgloss.Color(BgSelected))

	Bold = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(TextPrimary))

	MeTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(IdentityMe))

	MutedMeTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityMeMuted))

	MutedTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityMuted))

	MutualTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityMutual))

	MutedMutualTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(IdentityMutualMuted))

	OwnRepoTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityOwnRepo))

	MutedOwnRepoTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(IdentityOwnRepoMuted))

	AssignedTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(IdentityAssigned))

	MutedAssignedTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(IdentityAssignedMuted))

	Error = lipgloss.NewStyle().
		Foreground(lipgloss.Color(StatusError))

	Highlight = lipgloss.NewStyle().
			Background(lipgloss.Color(AccentHighlight)).
			Foreground(lipgloss.Color("0"))

	Retracted = lipgloss.NewStyle().
			Foreground(lipgloss.Color(TextSecondary))

	RetractedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color(BorderWarning))

	ListIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color(IdentityMeMuted))

	ListIndicatorSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color(IdentityMeMuted)).
				Background(lipgloss.Color(BgSelected))

	Doc = lipgloss.NewStyle().Margin(1, 2)
)

// AuthorStyle returns MeTitle when authorEmail matches userEmail, otherwise base.
func AuthorStyle(authorEmail, userEmail string, base lipgloss.Style) lipgloss.Style {
	if userEmail != "" && authorEmail != "" && strings.EqualFold(authorEmail, userEmail) {
		return MeTitle
	}
	return base
}

// FormatTime formats a timestamp as relative time.
func FormatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	mins := int(diff.Minutes())
	hours := int(diff.Hours())
	days := int(diff.Hours() / 24)

	if mins < 1 {
		return "just now"
	}
	if mins < 60 {
		return fmt.Sprintf("%dm ago", mins)
	}
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	if days < 7 {
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("Jan 2, 2006")
}

// FormatFullTime formats a timestamp with date, time, and timezone.
func FormatFullTime(t time.Time) string {
	return t.Local().Format("Jan 2, 2006 15:04 MST")
}

// RowStyles contains reusable styles for key-value layouts
type RowStyles struct {
	Label    lipgloss.Style // dim, fixed width
	Value    lipgloss.Style // normal color
	Action   lipgloss.Style // dim, for [key] hints
	Selected lipgloss.Style // background highlight
	Header   lipgloss.Style // bold, colored section headers
	Dim      lipgloss.Style // dim text for secondary info
}

// DefaultRowStyles returns standard styles for key-value layouts.
func DefaultRowStyles() RowStyles {
	return RowStyles{
		Label:    lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary)).Width(20),
		Value:    lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary)),
		Action:   lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary)),
		Selected: lipgloss.NewStyle().Background(lipgloss.Color(BgSelected)).Foreground(lipgloss.Color(TextPrimary)),
		Header:   lipgloss.NewStyle().Foreground(lipgloss.Color(TextPrimary)).Bold(true),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color(TextSecondary)),
	}
}

// RowStylesWithValueWidth returns styles with a custom value width.
func RowStylesWithValueWidth(width int) RowStyles {
	s := DefaultRowStyles()
	s.Value = s.Value.Width(width)
	return s
}

// RowStylesWithWidths returns styles with custom label and value widths.
func RowStylesWithWidths(labelWidth, valueWidth int) RowStyles {
	s := DefaultRowStyles()
	s.Label = s.Label.Width(labelWidth)
	if valueWidth > 0 {
		s.Value = s.Value.Width(valueWidth)
	}
	return s
}

// RenderRow renders a label-value pair with optional action.
func RenderRow(s RowStyles, label, value, action string, selected bool) string {
	if selected {
		return s.Selected.Render(fmt.Sprintf("▸ %s  %s", s.Label.Render(label), s.Value.Render(value)))
	}
	if action != "" {
		return fmt.Sprintf("  %s  %s  %s", s.Label.Render(label), s.Value.Render(value), s.Action.Render(action))
	}
	return fmt.Sprintf("  %s  %s", s.Label.Render(label), s.Value.Render(value))
}

// RenderEditRow renders a row in edit mode with text input.
func RenderEditRow(s RowStyles, label, inputView string) string {
	return fmt.Sprintf("  %s  %s", s.Label.Render(label), inputView)
}

// RenderHeader renders a section header.
func RenderHeader(s RowStyles, text string) string {
	return s.Header.Render(text)
}

var (
	// (?s) enables dotall mode so . matches newlines
	mathBlockRe  = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	mathInlineRe = regexp.MustCompile(`\$([^$\n]+?)\$`)
)

const (
	mathPlaceholderPrefix = "\x00MATH"
	mathBoundaryMarker    = "\x01"
)

type replacement struct {
	latex   string
	unicode string
}

var sortedReplacements []replacement

func init() {
	// Collect all replacements
	for k, v := range greekLetters {
		sortedReplacements = append(sortedReplacements, replacement{k, v})
	}
	for k, v := range operators {
		sortedReplacements = append(sortedReplacements, replacement{k, v})
	}
	for k, v := range relations {
		sortedReplacements = append(sortedReplacements, replacement{k, v})
	}
	for k, v := range arrows {
		sortedReplacements = append(sortedReplacements, replacement{k, v})
	}
	for k, v := range misc {
		sortedReplacements = append(sortedReplacements, replacement{k, v})
	}

	// Sort by length descending (longest first to avoid partial matches)
	sort.Slice(sortedReplacements, func(i, j int) bool {
		return len(sortedReplacements[i].latex) > len(sortedReplacements[j].latex)
	})
}

// RenderMath processes content, replacing math blocks with Unicode.
func RenderMath(content string) string {
	// Process block math ($$...$$)
	content = mathBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		latex := strings.TrimPrefix(strings.TrimSuffix(match, "$$"), "$$")
		latex = strings.TrimSpace(latex)
		return unicodeFallback(latex)
	})

	// Process inline math ($...$)
	content = mathInlineRe.ReplaceAllStringFunc(content, func(match string) string {
		latex := strings.TrimPrefix(strings.TrimSuffix(match, "$"), "$")
		latex = strings.TrimSpace(latex)
		return unicodeFallback(latex)
	})

	return content
}

// RenderMathWithPlaceholders converts math and returns placeholders for Glamour.
func RenderMathWithPlaceholders(content string) (string, []string) {
	var extracted []string
	m := mathBoundaryMarker

	// Process block math - extract and replace with placeholder (paragraph breaks for Glamour)
	content = mathBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		latex := strings.TrimPrefix(strings.TrimSuffix(match, "$$"), "$$")
		rendered := m + unicodeFallback(strings.TrimSpace(latex)) + m
		idx := len(extracted)
		extracted = append(extracted, rendered)
		return fmt.Sprintf("\n\n%s%d\x00\n\n", mathPlaceholderPrefix, idx)
	})

	// Process inline math - extract and replace with placeholder (no paragraph breaks)
	content = mathInlineRe.ReplaceAllStringFunc(content, func(match string) string {
		latex := strings.TrimPrefix(strings.TrimSuffix(match, "$"), "$")
		rendered := m + unicodeFallback(strings.TrimSpace(latex)) + m
		idx := len(extracted)
		extracted = append(extracted, rendered)
		return fmt.Sprintf("%s%d\x00", mathPlaceholderPrefix, idx)
	})

	return content, extracted
}

// Matches whitespace and ANSI codes between consecutive math markers
var consecutiveMathRe = regexp.MustCompile("\x01(?:\\s|\x1b\\[[0-9;]*m)*\x01")

// RestoreMath replaces placeholders with rendered math.
func RestoreMath(content string, extracted []string) string {
	m := mathBoundaryMarker
	for i, math := range extracted {
		placeholder := fmt.Sprintf("%s%d\x00", mathPlaceholderPrefix, i)
		content = strings.Replace(content, placeholder, math, 1)
	}
	// Collapse consecutive math (marker + whitespace/ANSI + marker -> marker\nmarker)
	content = consecutiveMathRe.ReplaceAllString(content, m+"\n"+m)
	// Remove markers
	content = strings.ReplaceAll(content, m, "")
	return content
}

// unicodeFallback converts LaTeX to Unicode symbols.
func unicodeFallback(content string) string {
	// Handle \frac{a}{b} -> (a)/(b) with nested brace support
	content = convertFrac(content)

	// Handle \sqrt{x} -> sqrt(x) with nested brace support
	content = convertSqrt(content)

	// Handle \binom{n}{k} -> C(n,k)
	content = convertBinom(content)

	// Handle \vec{x} -> x with combining arrow
	content = convertVec(content)

	// Handle \begin{matrix}...\end{matrix} -> [a b; c d]
	content = convertMatrix(content)

	// Apply sorted symbol replacements (longest first)
	for _, r := range sortedReplacements {
		content = strings.ReplaceAll(content, r.latex, r.unicode)
	}

	content = convertSuperscripts(content)
	content = convertSubscripts(content)
	return content
}

// extractBraceContent extracts content within balanced braces starting at pos.
func extractBraceContent(s string, pos int) (string, int) {
	if pos >= len(s) || s[pos] != '{' {
		return "", -1
	}
	depth := 0
	start := pos + 1
	for i := pos; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start:i], i + 1
			}
		}
	}
	return "", -1
}

// convertFrac handles \frac{num}{den} with nested braces.
func convertFrac(content string) string {
	for {
		idx := strings.Index(content, `\frac{`)
		if idx == -1 {
			break
		}
		numStart := idx + 5 // position of first {
		num, afterNum := extractBraceContent(content, numStart)
		if afterNum == -1 {
			break
		}
		den, afterDen := extractBraceContent(content, afterNum)
		if afterDen == -1 {
			break
		}
		// Recursively process numerator and denominator
		num = unicodeFallback(num)
		den = unicodeFallback(den)
		replacement := "(" + num + ")/(" + den + ")"
		content = content[:idx] + replacement + content[afterDen:]
	}
	return content
}

// convertSqrt handles \sqrt{x} with nested braces.
func convertSqrt(content string) string {
	for {
		idx := strings.Index(content, `\sqrt{`)
		if idx == -1 {
			break
		}
		argStart := idx + 5 // position of {
		arg, afterArg := extractBraceContent(content, argStart)
		if afterArg == -1 {
			break
		}
		// Recursively process argument
		arg = unicodeFallback(arg)
		replacement := "\u221A(" + arg + ")"
		content = content[:idx] + replacement + content[afterArg:]
	}
	return content
}

// convertBinom handles \binom{n}{k} conversion to C(n,k).
func convertBinom(content string) string {
	for {
		idx := strings.Index(content, `\binom{`)
		if idx == -1 {
			break
		}
		nStart := idx + 6 // position of first {
		n, afterN := extractBraceContent(content, nStart)
		if afterN == -1 {
			break
		}
		k, afterK := extractBraceContent(content, afterN)
		if afterK == -1 {
			break
		}
		// Recursively process n and k
		n = unicodeFallback(n)
		k = unicodeFallback(k)
		replacement := "C(" + n + "," + k + ")"
		content = content[:idx] + replacement + content[afterK:]
	}
	return content
}

// convertVec handles \vec{x} with combining right arrow above.
func convertVec(content string) string {
	for {
		idx := strings.Index(content, `\vec{`)
		if idx == -1 {
			break
		}
		argStart := idx + 4 // position of {
		arg, afterArg := extractBraceContent(content, argStart)
		if afterArg == -1 {
			break
		}
		// Recursively process argument and append combining right arrow above (U+20D7)
		arg = unicodeFallback(arg)
		replacement := arg + "\u20D7"
		content = content[:idx] + replacement + content[afterArg:]
	}
	return content
}

// convertMatrix handles \begin{matrix}...\end{matrix}.
func convertMatrix(content string) string {
	beginTag := `\begin{matrix}`
	endTag := `\end{matrix}`
	for {
		startIdx := strings.Index(content, beginTag)
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(content[startIdx:], endTag)
		if endIdx == -1 {
			break
		}
		endIdx += startIdx
		// Extract matrix content
		matrixContent := content[startIdx+len(beginTag) : endIdx]
		matrixContent = strings.TrimSpace(matrixContent)
		// Parse rows (separated by \\) and columns (separated by &)
		rows := strings.Split(matrixContent, `\\`)
		var rowStrs []string
		for _, row := range rows {
			row = strings.TrimSpace(row)
			if row == "" {
				continue
			}
			cols := strings.Split(row, "&")
			colStrs := make([]string, 0, len(cols))
			for _, col := range cols {
				col = strings.TrimSpace(col)
				col = unicodeFallback(col)
				colStrs = append(colStrs, col)
			}
			rowStrs = append(rowStrs, strings.Join(colStrs, " "))
		}
		replacement := "[" + strings.Join(rowStrs, "; ") + "]"
		content = content[:startIdx] + replacement + content[endIdx+len(endTag):]
	}
	return content
}

var greekLetters = map[string]string{
	`\alpha`:   "\u03B1",
	`\beta`:    "\u03B2",
	`\gamma`:   "\u03B3",
	`\delta`:   "\u03B4",
	`\epsilon`: "\u03B5",
	`\zeta`:    "\u03B6",
	`\eta`:     "\u03B7",
	`\theta`:   "\u03B8",
	`\iota`:    "\u03B9",
	`\kappa`:   "\u03BA",
	`\lambda`:  "\u03BB",
	`\mu`:      "\u03BC",
	`\nu`:      "\u03BD",
	`\xi`:      "\u03BE",
	`\pi`:      "\u03C0",
	`\rho`:     "\u03C1",
	`\sigma`:   "\u03C3",
	`\tau`:     "\u03C4",
	`\upsilon`: "\u03C5",
	`\phi`:     "\u03C6",
	`\chi`:     "\u03C7",
	`\psi`:     "\u03C8",
	`\omega`:   "\u03C9",
	`\Alpha`:   "\u0391",
	`\Beta`:    "\u0392",
	`\Gamma`:   "\u0393",
	`\Delta`:   "\u0394",
	`\Theta`:   "\u0398",
	`\Lambda`:  "\u039B",
	`\Xi`:      "\u039E",
	`\Pi`:      "\u03A0",
	`\Sigma`:   "\u03A3",
	`\Phi`:     "\u03A6",
	`\Psi`:     "\u03A8",
	`\Omega`:   "\u03A9",
}

var operators = map[string]string{
	`\sum`:    "\u2211",
	`\prod`:   "\u220F",
	`\int`:    "\u222B",
	`\oint`:   "\u222E",
	`\sqrt`:   "\u221A",
	`\cbrt`:   "\u221B",
	`\pm`:     "\u00B1",
	`\mp`:     "\u2213",
	`\times`:  "\u00D7",
	`\div`:    "\u00F7",
	`\cdot`:   "\u00B7",
	`\circ`:   "\u2218",
	`\bullet`: "\u2022",
	`\cap`:    "\u2229",
	`\cup`:    "\u222A",
	`\wedge`:  "\u2227",
	`\vee`:    "\u2228",
	`\oplus`:  "\u2295",
	`\otimes`: "\u2297",
}

var relations = map[string]string{
	`\leq`:      "\u2264",
	`\le`:       "\u2264",
	`\geq`:      "\u2265",
	`\ge`:       "\u2265",
	`\neq`:      "\u2260",
	`\ne`:       "\u2260",
	`\approx`:   "\u2248",
	`\equiv`:    "\u2261",
	`\sim`:      "\u223C",
	`\simeq`:    "\u2243",
	`\cong`:     "\u2245",
	`\propto`:   "\u221D",
	`\subset`:   "\u2282",
	`\supset`:   "\u2283",
	`\subseteq`: "\u2286",
	`\supseteq`: "\u2287",
	`\in`:       "\u2208",
	`\notin`:    "\u2209",
	`\ni`:       "\u220B",
	`\perp`:     "\u22A5",
	`\parallel`: "\u2225",
}

var arrows = map[string]string{
	`\rightarrow`:     "\u2192",
	`\to`:             "\u2192",
	`\leftarrow`:      "\u2190",
	`\leftrightarrow`: "\u2194",
	`\Rightarrow`:     "\u21D2",
	`\Leftarrow`:      "\u21D0",
	`\Leftrightarrow`: "\u21D4",
	`\uparrow`:        "\u2191",
	`\downarrow`:      "\u2193",
	`\mapsto`:         "\u21A6",
	`\hookrightarrow`: "\u21AA",
	`\hookleftarrow`:  "\u21A9",
}

var misc = map[string]string{
	// Math functions (stay as text)
	`\lim`: "lim",
	`\sin`: "sin",
	`\cos`: "cos",
	`\tan`: "tan",
	`\log`: "log",
	`\ln`:  "ln",
	`\exp`: "exp",
	`\max`: "max",
	`\min`: "min",
	// Symbols
	`\infty`:      "\u221E",
	`\partial`:    "\u2202",
	`\nabla`:      "\u2207",
	`\forall`:     "\u2200",
	`\exists`:     "\u2203",
	`\nexists`:    "\u2204",
	`\emptyset`:   "\u2205",
	`\varnothing`: "\u2205",
	`\therefore`:  "\u2234",
	`\because`:    "\u2235",
	`\ldots`:      "\u2026",
	`\cdots`:      "\u22EF",
	`\vdots`:      "\u22EE",
	`\ddots`:      "\u22F1",
	`\aleph`:      "\u2135",
	`\Re`:         "\u211C",
	`\Im`:         "\u2111",
	`\wp`:         "\u2118",
	`\ell`:        "\u2113",
	`\hbar`:       "\u210F",
	`\deg`:        "\u00B0",
	`\angle`:      "\u2220",
	`\triangle`:   "\u25B3",
	`\square`:     "\u25A1",
	`\diamond`:    "\u25C7",
	`\star`:       "\u2605",
	`\dagger`:     "\u2020",
	`\ddagger`:    "\u2021",
	`\checkmark`:  "\u2713",
}

var superscripts = map[rune]rune{
	'0': '\u2070',
	'1': '\u00B9',
	'2': '\u00B2',
	'3': '\u00B3',
	'4': '\u2074',
	'5': '\u2075',
	'6': '\u2076',
	'7': '\u2077',
	'8': '\u2078',
	'9': '\u2079',
	'+': '\u207A',
	'-': '\u207B',
	'=': '\u207C',
	'(': '\u207D',
	')': '\u207E',
	'n': '\u207F',
	'i': '\u2071',
	'x': '\u02E3',
}

var subscripts = map[rune]rune{
	'0': '\u2080',
	'1': '\u2081',
	'2': '\u2082',
	'3': '\u2083',
	'4': '\u2084',
	'5': '\u2085',
	'6': '\u2086',
	'7': '\u2087',
	'8': '\u2088',
	'9': '\u2089',
	'+': '\u208A',
	'-': '\u208B',
	'=': '\u208C',
	'(': '\u208D',
	')': '\u208E',
	'a': '\u2090',
	'e': '\u2091',
	'o': '\u2092',
	'x': '\u2093',
	'i': '\u1D62',
	'j': '\u2C7C',
	'n': '\u2099',
}

var superscriptRe = regexp.MustCompile(`\^(\{[^}]+\}|[0-9n+-])`)
var subscriptRe = regexp.MustCompile(`_(\{[^}]+\}|[0-9])`)

// convertSuperscripts converts ^{...} to Unicode superscripts.
func convertSuperscripts(content string) string {
	return superscriptRe.ReplaceAllStringFunc(content, func(match string) string {
		chars := match[1:]
		if strings.HasPrefix(chars, "{") && strings.HasSuffix(chars, "}") {
			chars = chars[1 : len(chars)-1]
		}
		var result strings.Builder
		allConverted := true
		for _, r := range chars {
			if sup, ok := superscripts[r]; ok {
				result.WriteRune(sup)
			} else {
				allConverted = false
				break
			}
		}
		if allConverted {
			return result.String()
		}
		return match
	})
}

// convertSubscripts converts _{...} to Unicode subscripts.
func convertSubscripts(content string) string {
	return subscriptRe.ReplaceAllStringFunc(content, func(match string) string {
		chars := match[1:]
		if strings.HasPrefix(chars, "{") && strings.HasSuffix(chars, "}") {
			chars = chars[1 : len(chars)-1]
		}
		var result strings.Builder
		allConverted := true
		for _, r := range chars {
			if sub, ok := subscripts[r]; ok {
				result.WriteRune(sub)
			} else {
				allConverted = false
				break
			}
		}
		if allConverted {
			return result.String()
		}
		return match
	})
}

// RenderSearchFooter renders a standard search mode footer with match count and navigation keys.
func RenderSearchFooter(width, matchIndex, matchCount int, inputMode bool, hasQuery bool) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(BorderFocused)).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TextNormal))
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TextSecondary)).
		Background(lipgloss.Color(BgFooter)).
		Padding(0, 1)
	var parts []string
	if matchCount > 0 {
		pos := fmt.Sprintf("%d/%d", matchIndex, matchCount)
		parts = append(parts, labelStyle.Render(pos))
	} else if hasQuery {
		parts = append(parts, Dim.Render("No matches"))
	}
	if inputMode {
		parts = append(parts, keyStyle.Render("enter")+":"+labelStyle.Render("done"))
	} else {
		parts = append(parts, keyStyle.Render("n")+":"+labelStyle.Render("next"))
		parts = append(parts, keyStyle.Render("N")+":"+labelStyle.Render("prev"))
		parts = append(parts, keyStyle.Render("/")+":"+labelStyle.Render("edit"))
	}
	parts = append(parts, keyStyle.Render("esc")+":"+Dim.Render("close"))
	return footerStyle.Width(width).Render(strings.Join(parts, "  "))
}

// WrapRawLines wraps the text block to fit the given width, then prepends selection bars.
func WrapRawLines(text, selectionBar string, wrapWidth int) []string {
	if wrapWidth > 0 {
		text = lipgloss.NewStyle().Width(wrapWidth).Render(text)
	}
	split := strings.Split(text, "\n")
	lines := make([]string, 0, len(split))
	for _, line := range split {
		lines = append(lines, selectionBar+line)
	}
	return lines
}

// RenderCommitMessage fetches the full commit with metadata from cache and renders it as wrapped lines.
func RenderCommitMessage(id, selectionBar string, wrapWidth int) []string {
	ref := protocol.ParseRef(id)
	if ref.Value == "" {
		return []string{selectionBar + Dim.Render("commit message unavailable")}
	}
	c, err := cache.GetCommit(ref.Repository, ref.Value, ref.Branch)
	if err != nil || c.Message == "" {
		return []string{selectionBar + Dim.Render("commit message unavailable")}
	}
	lines := make([]string, 0, 5)
	lines = append(lines, selectionBar+Dim.Render("commit "+c.Hash))
	lines = append(lines, selectionBar+Dim.Render(fmt.Sprintf("Author: %s <%s>", c.AuthorName, c.AuthorEmail)))
	lines = append(lines, selectionBar+Dim.Render("Date:   "+c.Timestamp.Format("Mon Jan 2 15:04:05 2006 -0700")))
	lines = append(lines, selectionBar)
	lines = append(lines, WrapRawLines(c.Message, selectionBar, wrapWidth)...)
	return lines
}
