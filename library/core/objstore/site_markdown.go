// site_markdown.go - the markdown renderer the page layer uses for the front
// page's README: a Go port of the reader's own grammar, emitting an HTML string
// where the reader builds DOM nodes.
//
// The grammar is deliberately not designed here — it is transcribed from the
// three functions the booted app renders the same README with, so the served
// document and the app agree block for block: parseMarkdown / parseInline
// (site/gs-core.js), and renderInline / renderBlocksInto / sanitizeInert
// (site/gs-render.js). Covered: ATX and setext headings, paragraphs, fenced
// code, nested and task lists, GFM tables, blockquotes, thematic breaks, links,
// images, emphasis, strong, strikethrough, inline code, autolinks, and the raw
// HTML allowlist READMEs rely on.
//
// Two deliberate departures from the reader, both forced by what a static page
// can do:
//
//   - A repo-relative image has no src a bucket can serve (the bucket is keyed
//     by sha, not by path; the reader resolves those by fetching the blob and
//     building a blob: URL), so it degrades to its alt text instead of emitting
//     a src that would 404. Only an absolute https src becomes a real <img>.
//   - In-page anchors are rewritten to the heading ids this renderer stamps
//     (md-<slug>, the reader's own scheme), so a README's own table of contents
//     works with no script running.
//
// Provenance note: this renders the BUCKET OWNER'S OWN README, so the allowlist
// is hygiene (never execute what someone pasted into their own README), not an
// adversarial boundary. Item bodies are third-party content under a stricter
// policy and are NOT rendered through here.

package objstore

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Block kinds a parsed markdown document is made of.
const (
	siteMDParagraph = "paragraph"
	siteMDHeading   = "heading"
	siteMDCode      = "code"
	siteMDList      = "list"
	siteMDQuote     = "blockquote"
	siteMDTable     = "table"
	siteMDThematic  = "thematic"
	siteMDHTML      = "html"      // a self-contained raw HTML line
	siteMDHTMLOpen  = "htmlopen"  // an opening tag on its own line: following blocks nest inside
	siteMDHTMLClose = "htmlclose" // its closing tag
)

// Inline span kinds.
const (
	siteMDText    = "text"
	siteMDInCode  = "code"
	siteMDStrong  = "strong"
	siteMDEm      = "em"
	siteMDStrike  = "strike"
	siteMDImage   = "image"
	siteMDLink    = "link"
	siteMDRawHTML = "rawhtml"
)

// siteMDBlock is one parsed block. The fields are a union over the block kinds
// (the reader's block objects, transcribed), not a per-kind type each.
type siteMDBlock struct {
	Kind    string
	Level   int            // heading level
	Spans   []siteMDSpan   // heading, paragraph
	Text    string         // fenced code body
	Ordered bool           // list
	Items   []siteMDItem   // list
	Blocks  []siteMDBlock  // blockquote
	Headers [][]siteMDSpan // table header cells
	Aligns  []string       // table column alignment
	Rows    [][][]siteMDSpan
	Tag     string // htmlopen/htmlclose tag name
	Raw     string // html/htmlopen source
}

// siteMDItem is one list item: its inline content, an optional task checkbox
// state, and any nested lists that hang off it.
type siteMDItem struct {
	Spans    []siteMDSpan
	Task     string // "" none, " " unchecked, "x" checked
	Children []siteMDBlock
}

// siteMDSpan is one inline span.
type siteMDSpan struct {
	Kind  string
	Value string       // text, inline code, raw HTML source
	Spans []siteMDSpan // strong/em/strike/link content
	Src   string       // image
	Alt   string       // image
	Href  string       // link
}

// siteMarkdownContext is what a relative reference resolves against: the app's
// hash-URL base (site.URL + "index.html#") and the branch the document lives on.
// Both empty means relative references simply do not resolve.
type siteMarkdownContext struct {
	AppBase string
	Branch  string
}

// siteMDRender carries one document's render state: the reference context and
// the heading slugs already used (ids stay unique within the document).
type siteMDRender struct {
	ctx   siteMarkdownContext
	slugs map[string]bool
}

var (
	siteMDFenceRE     = regexp.MustCompile("^\\s*```")
	siteMDCloseTagRE  = regexp.MustCompile(`^</([a-zA-Z][\w-]*)\s*>$`)
	siteMDOpenTagRE   = regexp.MustCompile(`^<([a-zA-Z][\w-]*)((?:\s[^<>]*)?)>$`)
	siteMDHTMLLineRE  = regexp.MustCompile(`^<[a-zA-Z][\w-]*(\s[^<>]*)?/?>`)
	siteMDLeadTagRE   = regexp.MustCompile(`^<(/?)([a-zA-Z][\w-]*)`)
	siteMDHeadingRE   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	siteMDHeadTrailRE = regexp.MustCompile(`\s+#+\s*$`)
	siteMDQuoteRE     = regexp.MustCompile(`^\s*>`)
	siteMDBulletRE    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	siteMDItemRE      = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$`)
	siteMDOrderedRE   = regexp.MustCompile(`^\s*\d+[.)]\s+`)
	siteMDSetextRE    = regexp.MustCompile(`^ {0,3}(=+|-+) *$`)
	siteMDTaskRE      = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	siteMDSepCellRE   = regexp.MustCompile(`^:?-+:?$`)
	siteMDAutolinkRE  = regexp.MustCompile(`^<((?:https?://|mailto:)[^>\s]+)>`)
	siteMDInTagRE     = regexp.MustCompile(`^<(/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s[^<>]*)?)(/?)>`)
	siteMDBareURLRE   = regexp.MustCompile(`^https?://[^\s<>)]+`)
	siteMDSchemeRE    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	siteMDHrefOKRE    = regexp.MustCompile(`(?i)^(https?:|mailto:|#|/)`)
	siteMDSlugDropRE  = regexp.MustCompile(`[^\w\s-]`)
	siteMDSlugSpaceRE = regexp.MustCompile(`\s+`)
)

// siteMDVoidTags carry no closing tag, so line- and inline-level detection
// treats them as standalone elements, never wrappers (gs-core.js VOID_HTML).
var siteMDVoidTags = map[string]bool{"br": true, "hr": true, "img": true, "source": true, "col": true, "input": true, "wbr": true, "area": true}

// siteMDInlineTags are the inline-level tags whose line continues the current
// paragraph instead of breaking it (gs-core.js INLINE_HTML).
var siteMDInlineTags = map[string]bool{
	"a": true, "b": true, "i": true, "em": true, "strong": true, "code": true, "kbd": true,
	"sup": true, "sub": true, "span": true, "img": true, "br": true, "del": true, "s": true,
	"strike": true, "mark": true, "small": true, "picture": true, "input": true,
}

// siteMDAllowTags is the sanitizer's element allowlist (gs-render.js
// SANITIZE_TAGS): these are rebuilt clean, everything else is dropped or
// unwrapped.
var siteMDAllowTags = map[string]bool{
	"div": true, "span": true, "p": true, "br": true, "hr": true, "a": true, "img": true,
	"b": true, "strong": true, "i": true, "em": true, "code": true, "pre": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "table": true, "thead": true, "tbody": true,
	"tfoot": true, "tr": true, "td": true, "th": true, "caption": true, "colgroup": true,
	"col": true, "details": true, "summary": true, "center": true, "sup": true, "sub": true,
	"kbd": true, "del": true, "s": true, "strike": true, "blockquote": true, "mark": true,
}

// siteMDDropTags vanish subtree and all: nothing executable, loadable or
// document-level ever reaches the output (gs-render.js SANITIZE_DROP).
var siteMDDropTags = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true, "link": true,
	"meta": true, "noscript": true, "template": true, "svg": true, "math": true, "form": true,
	"input": true, "button": true, "textarea": true, "select": true, "title": true, "head": true,
	"base": true, "frame": true, "frameset": true, "applet": true,
}

// siteMDAllowAttrs is the attribute allowlist (gs-render.js SANITIZE_ATTRS).
// Event handlers and style are absent by construction, which is what makes an
// onerror= or onclick= impossible to carry through.
var siteMDAllowAttrs = map[string]bool{
	"align": true, "alt": true, "title": true, "width": true, "height": true,
	"src": true, "href": true, "open": true,
}

// renderSiteMarkdown renders markdown to the page layer's HTML. Empty when the
// document carries no blocks, so a caller can skip the section entirely.
func renderSiteMarkdown(text string, ctx siteMarkdownContext) string {
	blocks := parseSiteMarkdown(text)
	if len(blocks) == 0 {
		return ""
	}
	r := &siteMDRender{ctx: ctx, slugs: map[string]bool{}}
	var b strings.Builder
	b.WriteString("<div class=\"markdown\">\n")
	writeSiteMDBlocks(&b, blocks, r)
	b.WriteString("</div>\n")
	return b.String()
}

// siteMDTruncateSource caps markdown SOURCE at max bytes, reporting whether it
// cut. The cut is pulled back to the last line boundary so the renderer is never
// handed half a line: every block rule is line-based, so a whole-line cut can
// only ever leave a structure unclosed (which the renderer closes), where a
// mid-line cut could leave half a tag with nothing to do but escape it back into
// the visible markup this layer exists to remove.
func siteMDTruncateSource(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	text = strings.ToValidUTF8(text[:max], "")
	if cut := strings.LastIndexByte(text, '\n'); cut > 0 {
		text = text[:cut]
	}
	return text, true
}

// ---- Block parsing (gs-core.js parseMarkdown) ----

// parseSiteMarkdown parses text into a block list. Raw HTML is captured
// verbatim as html/htmlopen/htmlclose blocks for the sanitizer; every other
// block is plain data the writer turns into markup it generated itself.
func parseSiteMarkdown(text string) []siteMDBlock {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	var blocks []siteMDBlock
	for i := 0; i < len(lines); {
		line := lines[i]
		if siteMDFenceRE.MatchString(line) {
			// The fence's info string is parsed away and dropped: it only selects a
			// highlighter grammar, and the static page ships no tokenizer (the
			// booted app highlights the same block in place).
			var body []string
			for i++; i < len(lines) && !siteMDFenceRE.MatchString(lines[i]); i++ {
				body = append(body, lines[i])
			}
			i++
			blocks = append(blocks, siteMDBlock{Kind: siteMDCode, Text: strings.Join(body, "\n")})
			continue
		}
		t := strings.TrimSpace(line)
		if m := siteMDCloseTagRE.FindStringSubmatch(t); m != nil {
			blocks = append(blocks, siteMDBlock{Kind: siteMDHTMLClose, Tag: strings.ToLower(m[1])})
			i++
			continue
		}
		if m := siteMDOpenTagRE.FindStringSubmatch(t); m != nil && !strings.HasSuffix(m[2], "/") && !siteMDVoidTags[strings.ToLower(m[1])] {
			blocks = append(blocks, siteMDBlock{Kind: siteMDHTMLOpen, Tag: strings.ToLower(m[1]), Raw: t})
			i++
			continue
		}
		if siteMDHTMLLineRE.MatchString(t) {
			blocks = append(blocks, siteMDBlock{Kind: siteMDHTML, Raw: t})
			i++
			continue
		}
		if m := siteMDHeadingRE.FindStringSubmatch(line); m != nil {
			content := strings.TrimSpace(siteMDHeadTrailRE.ReplaceAllString(m[2], ""))
			blocks = append(blocks, siteMDBlock{Kind: siteMDHeading, Level: len(m[1]), Spans: parseSiteMDInline(content)})
			i++
			continue
		}
		if siteMDQuoteRE.MatchString(line) {
			var buf []string
			for ; i < len(lines) && siteMDQuoteRE.MatchString(lines[i]); i++ {
				buf = append(buf, siteMDStripQuote(lines[i]))
			}
			blocks = append(blocks, siteMDBlock{Kind: siteMDQuote, Blocks: parseSiteMarkdown(strings.Join(buf, "\n"))})
			continue
		}
		if strings.Contains(line, "|") && i+1 < len(lines) && siteMDIsTableSeparator(lines[i+1]) {
			block := siteMDBlock{Kind: siteMDTable}
			for _, cell := range siteMDSplitRow(line) {
				block.Headers = append(block.Headers, parseSiteMDInline(cell))
			}
			for _, cell := range siteMDSplitRow(lines[i+1]) {
				block.Aligns = append(block.Aligns, siteMDCellAlign(cell))
			}
			for i += 2; i < len(lines) && strings.TrimSpace(lines[i]) != "" && strings.Contains(lines[i], "|"); i++ {
				var row [][]siteMDSpan
				for _, cell := range siteMDSplitRow(lines[i]) {
					row = append(row, parseSiteMDInline(cell))
				}
				block.Rows = append(block.Rows, row)
			}
			blocks = append(blocks, block)
			continue
		}
		if siteMDIsThematicBreak(line) {
			blocks = append(blocks, siteMDBlock{Kind: siteMDThematic})
			i++
			continue
		}
		if siteMDBulletRE.MatchString(line) {
			block, next := parseSiteMDList(lines, i)
			blocks = append(blocks, block)
			i = next
			continue
		}
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		var para []string
		setext := 0
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !siteMDFenceRE.MatchString(lines[i]) &&
			!siteMDHeadingRE.MatchString(lines[i]) && !siteMDBulletRE.MatchString(lines[i]) &&
			!siteMDQuoteRE.MatchString(lines[i]) && !siteMDBreaksParagraph(strings.TrimSpace(lines[i])) {
			if len(para) > 0 {
				if su := siteMDSetextRE.FindStringSubmatch(lines[i]); su != nil {
					setext = 2
					if su[1][0] == '=' {
						setext = 1
					}
					i++
					break
				}
			}
			if siteMDIsThematicBreak(lines[i]) {
				break
			}
			para = append(para, lines[i])
			i++
		}
		if len(para) == 0 {
			// Progress guard: a line that breaks a paragraph but matched no block
			// rule (a malformed or cut-off tag, a stray closing tag with trailing
			// text) is consumed as the escaped text it is. Without it the line
			// would be re-examined forever.
			para = append(para, lines[i])
			i++
		}
		spans := parseSiteMDInline(strings.Join(para, "\n"))
		switch {
		case setext > 0:
			blocks = append(blocks, siteMDBlock{Kind: siteMDHeading, Level: setext, Spans: spans})
		case len(spans) > 0:
			blocks = append(blocks, siteMDBlock{Kind: siteMDParagraph, Spans: spans})
		}
	}
	return blocks
}

// siteMDStripQuote removes one blockquote marker (and the space after it) from
// a line, the way the reader's `line.replace(/^\s*>\s?/, "")` does.
func siteMDStripQuote(line string) string {
	rest := strings.TrimLeft(line, " \t")
	rest = strings.TrimPrefix(rest, ">")
	return strings.TrimPrefix(rest, " ")
}

// siteMDIndentWidth counts a line's leading whitespace (a tab counts as two),
// which is what decides list nesting.
func siteMDIndentWidth(line string) int {
	n := 0
	for _, c := range line {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 2
		default:
			return n
		}
	}
	return n
}

// parseSiteMDList consumes an indentation-delimited list starting at `start`,
// returning the block and the line after it. Deeper-indented items attach to the
// preceding item as nested lists; a blank line ends the list (tight lists only).
func parseSiteMDList(lines []string, start int) (siteMDBlock, int) {
	base := siteMDIndentWidth(lines[start])
	block := siteMDBlock{Kind: siteMDList, Ordered: siteMDOrderedRE.MatchString(lines[start])}
	i := start
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		m := siteMDItemRE.FindStringSubmatch(line)
		if m == nil {
			break
		}
		ind := siteMDIndentWidth(line)
		if ind < base {
			break
		}
		if ind > base {
			sub, next := parseSiteMDList(lines, i)
			if len(block.Items) > 0 {
				last := &block.Items[len(block.Items)-1]
				last.Children = append(last.Children, sub)
			}
			i = next
			continue
		}
		item := siteMDItem{}
		content := m[2]
		if tm := siteMDTaskRE.FindStringSubmatch(content); tm != nil {
			item.Task, content = strings.ToLower(tm[1]), tm[2]
		}
		item.Spans = parseSiteMDInline(content)
		block.Items = append(block.Items, item)
		i++
	}
	return block, i
}

// siteMDSplitRow splits a table row into trimmed cells, dropping the optional
// leading and trailing pipes.
func siteMDSplitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	cells := strings.Split(s, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// siteMDIsTableSeparator recognizes a GFM delimiter row (---, :--, :-:, --:).
func siteMDIsTableSeparator(line string) bool {
	if !strings.Contains(line, "|") && !strings.Contains(line, "-") {
		return false
	}
	cells := siteMDSplitRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !siteMDSepCellRE.MatchString(c) {
			return false
		}
	}
	return true
}

// siteMDCellAlign maps a delimiter cell to its column alignment.
func siteMDCellAlign(cell string) string {
	l, r := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
	switch {
	case l && r:
		return "center"
	case r:
		return "right"
	case l:
		return "left"
	}
	return ""
}

// siteMDIsThematicBreak recognizes a *** / --- / ___ rule line (3+ markers,
// spaces allowed between). A `---` directly under paragraph text is a setext h2
// instead, which parseSiteMarkdown checks first.
func siteMDIsThematicBreak(line string) bool {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 || body == "" {
		return false
	}
	marker := body[0]
	if marker != '-' && marker != '*' && marker != '_' {
		return false
	}
	count := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case marker:
			count++
		case ' ':
		default:
			return false
		}
	}
	return count >= 3
}

// siteMDBreaksParagraph reports whether a trimmed `<`-leading line ends the
// current paragraph: closing tags and block-level tags do, an inline-level tag
// joins the paragraph and flows through the inline parser's raw-HTML handling.
func siteMDBreaksParagraph(t string) bool {
	m := siteMDLeadTagRE.FindStringSubmatch(t)
	if m == nil {
		return false
	}
	return m[1] == "/" || !siteMDInlineTags[strings.ToLower(m[2])]
}

// ---- Inline parsing (gs-core.js parseInline) ----

// siteMDMatchDelim returns the index of the delimiter closing the one at
// `start`, counting nesting, or -1. This is what lets a [![alt](img)](link)
// badge parse: the outer ] and ) match past the inner image's.
func siteMDMatchDelim(text string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// siteMDIsPunct reports whether a byte is one of the ASCII punctuation
// characters a backslash may escape.
func siteMDIsPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') || (c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

// parseSiteMDInline tokenizes text into inline spans: code, images, links,
// autolinks, bare URLs, strong, strikethrough, emphasis, a raw-HTML subset
// captured verbatim for the sanitizer, and plain text.
func parseSiteMDInline(text string) []siteMDSpan {
	var spans []siteMDSpan
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			spans = append(spans, siteMDSpan{Kind: siteMDText, Value: buf.String()})
			buf.Reset()
		}
	}
	for i := 0; i < len(text); {
		ch := text[i]
		if ch == '\\' && i+1 < len(text) && siteMDIsPunct(text[i+1]) {
			buf.WriteByte(text[i+1])
			i += 2
			continue
		}
		if ch == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDInCode, Value: text[i+1 : i+1+end]})
				i += end + 2
				continue
			}
		}
		if ch == '!' && i+1 < len(text) && text[i+1] == '[' {
			if close := siteMDMatchDelim(text, i+1, '[', ']'); close > i && close+1 < len(text) && text[close+1] == '(' {
				if paren := siteMDMatchDelim(text, close+1, '(', ')'); paren > close {
					flush()
					spans = append(spans, siteMDSpan{Kind: siteMDImage, Alt: text[i+2 : close], Src: strings.TrimSpace(text[close+2 : paren])})
					i = paren + 1
					continue
				}
			}
		}
		if ch == '[' {
			if close := siteMDMatchDelim(text, i, '[', ']'); close > i && close+1 < len(text) && text[close+1] == '(' {
				if paren := siteMDMatchDelim(text, close+1, '(', ')'); paren > close {
					flush()
					spans = append(spans, siteMDSpan{Kind: siteMDLink, Spans: parseSiteMDInline(text[i+1 : close]), Href: strings.TrimSpace(text[close+2 : paren])})
					i = paren + 1
					continue
				}
			}
		}
		if ch == '<' {
			if strings.HasPrefix(text[i:], "<!--") {
				flush()
				if end := strings.Index(text[i+4:], "-->"); end >= 0 {
					i += 4 + end + 3
				} else {
					i = len(text)
				}
				continue
			}
			if auto := siteMDAutolinkRE.FindStringSubmatch(text[i:]); auto != nil {
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDLink, Spans: []siteMDSpan{{Kind: siteMDText, Value: auto[1]}}, Href: auto[1]})
				i += len(auto[0])
				continue
			}
			if tagM := siteMDInTagRE.FindStringSubmatch(text[i:]); tagM != nil {
				tag, whole := strings.ToLower(tagM[2]), tagM[0]
				flush()
				if tagM[1] == "/" || tagM[4] == "/" || siteMDVoidTags[tag] {
					spans = append(spans, siteMDSpan{Kind: siteMDRawHTML, Value: whole})
					i += len(whole)
					continue
				}
				closeTag := "</" + tag + ">"
				if end := strings.Index(text[i+len(whole):], closeTag); end >= 0 {
					stop := i + len(whole) + end + len(closeTag)
					spans = append(spans, siteMDSpan{Kind: siteMDRawHTML, Value: text[i:stop]})
					i = stop
					continue
				}
				spans = append(spans, siteMDSpan{Kind: siteMDRawHTML, Value: whole})
				i += len(whole)
				continue
			}
		}
		if ch == '*' && i+1 < len(text) && text[i+1] == '*' {
			if end := strings.Index(text[i+2:], "**"); end >= 0 {
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDStrong, Spans: parseSiteMDInline(text[i+2 : i+2+end])})
				i += end + 4
				continue
			}
		}
		if ch == '~' && i+1 < len(text) && text[i+1] == '~' {
			if end := strings.Index(text[i+2:], "~~"); end >= 0 {
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDStrike, Spans: parseSiteMDInline(text[i+2 : i+2+end])})
				i += end + 4
				continue
			}
		}
		if (ch == '*' || ch == '_') && i+1 < len(text) && text[i+1] != ch && text[i+1] != ' ' {
			if end := strings.IndexByte(text[i+1:], ch); end >= 0 {
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDEm, Spans: parseSiteMDInline(text[i+1 : i+1+end])})
				i += end + 2
				continue
			}
		}
		if ch == 'h' {
			if m := siteMDBareURLRE.FindString(text[i:]); m != "" {
				url := strings.TrimRight(m, ".,;:!?")
				flush()
				spans = append(spans, siteMDSpan{Kind: siteMDLink, Spans: []siteMDSpan{{Kind: siteMDText, Value: url}}, Href: url})
				i += len(url)
				continue
			}
		}
		buf.WriteByte(ch)
		i++
	}
	flush()
	return spans
}

// ---- Reference gating ----

// siteMDSlug builds the reader's GitHub-style anchor slug from heading text.
func siteMDSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = siteMDSlugDropRE.ReplaceAllString(s, "")
	return siteMDSlugSpaceRE.ReplaceAllString(s, "-")
}

// siteMDJoinPath normalizes a relative path the way the reader's joinPath does
// (drops "./", resolves ".." and empty segments).
func siteMDJoinPath(rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimPrefix(rel, "/")
	var parts []string
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// siteMDHref gates a link target. An in-page anchor is rewritten onto this
// renderer's own heading ids so a README's table of contents works with no
// script running; absolute web, mailto and root-relative targets pass through;
// a bare-relative path resolves to the app's file route (the only surface that
// can show a repo file). "" means the link renders without an href, exactly as
// the reader renders one it cannot resolve.
func siteMDHref(raw string, ctx siteMarkdownContext) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "#") {
		slug := siteMDSlug(raw[1:])
		if slug == "" {
			return "#"
		}
		return "#md-" + slug
	}
	if siteMDHrefOKRE.MatchString(raw) {
		return raw
	}
	if ctx.AppBase == "" || ctx.Branch == "" || siteMDSchemeRE.MatchString(raw) || strings.HasPrefix(raw, "//") {
		return ""
	}
	path, frag, _ := strings.Cut(raw, "#")
	path, _, _ = strings.Cut(path, "?")
	if path == "" {
		return ""
	}
	ref := ctx.AppBase + "file:" + siteMDJoinPath(path) + "@" + ctx.Branch
	if slug := siteMDSlug(frag); slug != "" {
		ref += ":" + slug
	}
	return ref
}

// siteMDImageHTML renders an image. An absolute https src is a real <img> (badge
// rows and shields render exactly as they do in the app); anything else is
// repo-relative or an unusable scheme, and a static page has no src for it — the
// bucket serves objects by sha, not by path — so it DEGRADES TO ITS ALT TEXT
// rather than emitting a src that would 404 or reach off-site. The booted app
// re-renders the same README and resolves those images from the object store,
// which is the one remaining thing the upgrade adds to this section.
// Every emitted image is loading="lazy": on a deep-link entry the boot hides
// this content within milliseconds, and lazy images inside a display:none
// subtree are never fetched — without it the preload scanner pulled a README
// hero image (often hundreds of KB, third-party) on every route, competing
// with the shell batch; a no-JS reader still gets the image on scroll.
func siteMDImageHTML(src, alt, extraAttrs string) string {
	if !strings.HasPrefix(strings.ToLower(src), "https://") {
		return html.EscapeString(alt)
	}
	out := `<img src="` + html.EscapeString(src) + `"`
	if alt != "" {
		out += ` alt="` + html.EscapeString(alt) + `"`
	}
	return out + extraAttrs + ` loading="lazy">`
}

// ---- Raw HTML: lex, then rebuild against the allowlist ----

// Token kinds the raw-HTML lexer emits.
const (
	siteHTMLText  = ""
	siteHTMLStart = "start"
	siteHTMLEnd   = "end"
)

// siteHTMLToken is one lexed piece of a raw HTML fragment.
type siteHTMLToken struct {
	Kind  string
	Text  string
	Tag   string
	Attrs [][2]string
	Void  bool // written self-closing
}

// siteMDOpenTag is one element the sanitizer has entered: the source tag name
// (what a closing tag matches against) and whether anything was emitted for it.
type siteMDOpenTag struct {
	name string
	emit bool
}

// siteMDIsSpace reports whether a byte is HTML tag whitespace.
func siteMDIsSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// siteMDIsTagNameByte reports whether a byte can appear in a tag name.
func siteMDIsTagNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// lexSiteHTMLTag parses one tag starting at s[0]=='<', returning the token and
// how many bytes it spans. A zero length means this is not a tag (an unfinished
// or malformed one), and the caller keeps the '<' as text.
func lexSiteHTMLTag(s string) (siteHTMLToken, int) {
	i := 1
	tk := siteHTMLToken{Kind: siteHTMLStart}
	if i < len(s) && s[i] == '/' {
		tk.Kind = siteHTMLEnd
		i++
	}
	start := i
	for i < len(s) && siteMDIsTagNameByte(s[i]) {
		i++
	}
	if i == start {
		return siteHTMLToken{}, 0
	}
	tk.Tag = strings.ToLower(s[start:i])
	for i < len(s) {
		for i < len(s) && siteMDIsSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '>' {
			return tk, i + 1
		}
		if s[i] == '/' {
			tk.Void = true
			i++
			continue
		}
		nameStart := i
		for i < len(s) && !siteMDIsSpace(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		if i == nameStart {
			i++
			continue
		}
		name, value := s[nameStart:i], ""
		for i < len(s) && siteMDIsSpace(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && siteMDIsSpace(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				quote := s[i]
				i++
				valueStart := i
				for i < len(s) && s[i] != quote {
					i++
				}
				value = s[valueStart:i]
				if i < len(s) {
					i++
				}
			} else {
				valueStart := i
				for i < len(s) && !siteMDIsSpace(s[i]) && s[i] != '>' {
					i++
				}
				value = s[valueStart:i]
			}
		}
		tk.Attrs = append(tk.Attrs, [2]string{name, value})
	}
	return siteHTMLToken{}, 0
}

// lexSiteHTML splits a raw HTML fragment into text runs, start tags and end
// tags. Comments, doctypes and processing instructions are dropped, which is
// what an inert parse leaves the reader's sanitizer to rebuild from.
func lexSiteHTML(s string) []siteHTMLToken {
	var out []siteHTMLToken
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, siteHTMLToken{Kind: siteHTMLText, Text: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			text.WriteByte(s[i])
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			flush()
			if end := strings.Index(s[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
			} else {
				i = len(s)
			}
			continue
		}
		if strings.HasPrefix(s[i:], "<!") || strings.HasPrefix(s[i:], "<?") {
			flush()
			if end := strings.IndexByte(s[i:], '>'); end >= 0 {
				i += end + 1
			} else {
				i = len(s)
			}
			continue
		}
		tk, n := lexSiteHTMLTag(s[i:])
		if n == 0 {
			text.WriteByte(s[i])
			i++
			continue
		}
		flush()
		out = append(out, tk)
		i += n
	}
	flush()
	return out
}

// cleanSiteHTMLTag rebuilds one start tag against the allowlist: allowed
// attributes only (so an event handler or a style has no way through), hrefs and
// image sources re-gated. It returns the tag's HTML and the tag name that must
// be closed later ("" for a void element or an image).
func cleanSiteHTMLTag(tk siteHTMLToken, ctx siteMarkdownContext) (string, string) {
	var attrs strings.Builder
	var src, alt string
	for _, a := range tk.Attrs {
		name := strings.ToLower(a[0])
		if !siteMDAllowAttrs[name] {
			continue
		}
		value := html.UnescapeString(a[1])
		switch name {
		case "href":
			if href := siteMDHref(value, ctx); href != "" {
				attrs.WriteString(` href="` + html.EscapeString(href) + `"`)
			}
		case "src":
			src = value
		case "alt":
			alt = value
		default:
			attrs.WriteString(" " + name + `="` + html.EscapeString(value) + `"`)
		}
	}
	if tk.Tag == "img" {
		return siteMDImageHTML(src, alt, attrs.String()), ""
	}
	if alt != "" {
		attrs.WriteString(` alt="` + html.EscapeString(alt) + `"`)
	}
	open := "<" + tk.Tag + attrs.String() + ">"
	if tk.Void || siteMDVoidTags[tk.Tag] {
		return open, ""
	}
	return open, tk.Tag
}

// sanitizeSiteHTML rebuilds a raw HTML fragment against the allowlist: dropped
// tags vanish subtree and all, unknown tags unwrap to their children, allowed
// tags are re-emitted from their parsed name and filtered attributes. The output
// is always balanced — every element this opens, it closes — so a fragment cut
// mid-structure by the README cap still yields well-formed HTML.
func sanitizeSiteHTML(raw string, ctx siteMarkdownContext) string {
	var b strings.Builder
	var open []siteMDOpenTag
	skip, depth := "", 0
	closeThrough := func(tag string) {
		for i := len(open) - 1; i >= 0; i-- {
			if open[i].name != tag {
				continue
			}
			for j := len(open) - 1; j >= i; j-- {
				if open[j].emit {
					b.WriteString("</" + open[j].name + ">")
				}
			}
			open = open[:i]
			return
		}
	}
	for _, tk := range lexSiteHTML(raw) {
		if skip != "" {
			if tk.Kind == siteHTMLStart && tk.Tag == skip && !tk.Void {
				depth++
			} else if tk.Kind == siteHTMLEnd && tk.Tag == skip {
				if depth--; depth == 0 {
					skip = ""
				}
			}
			continue
		}
		switch tk.Kind {
		case siteHTMLText:
			b.WriteString(html.EscapeString(html.UnescapeString(tk.Text)))
		case siteHTMLStart:
			void := tk.Void || siteMDVoidTags[tk.Tag]
			if siteMDDropTags[tk.Tag] || tk.Tag == "source" {
				if !void {
					skip, depth = tk.Tag, 1
				}
				continue
			}
			if tk.Tag == "picture" || !siteMDAllowTags[tk.Tag] {
				if !void {
					open = append(open, siteMDOpenTag{name: tk.Tag})
				}
				continue
			}
			openTag, closeName := cleanSiteHTMLTag(tk, ctx)
			b.WriteString(openTag)
			if closeName != "" {
				open = append(open, siteMDOpenTag{name: closeName, emit: true})
			}
		case siteHTMLEnd:
			closeThrough(tk.Tag)
		}
	}
	for j := len(open) - 1; j >= 0; j-- {
		if open[j].emit {
			b.WriteString("</" + open[j].name + ">")
		}
	}
	return b.String()
}

// sanitizeSiteHTMLWrapper rebuilds a block-level opening tag that following
// blocks nest inside (a README's <div align="center"> hero). It returns the
// emitted tag and the name to close, both "" when the element does not survive
// the allowlist — in which case the wrapper is not entered at all and its
// children render into the parent, exactly as the reader's renderBlocksInto
// behaves when the sanitizer hands it no container.
func sanitizeSiteHTMLWrapper(raw string, ctx siteMarkdownContext) (string, string) {
	for _, tk := range lexSiteHTML(raw) {
		if tk.Kind != siteHTMLStart {
			continue
		}
		if siteMDDropTags[tk.Tag] || tk.Tag == "source" || tk.Tag == "picture" || !siteMDAllowTags[tk.Tag] {
			return "", ""
		}
		return cleanSiteHTMLTag(tk, ctx)
	}
	return "", ""
}

// ---- Writing (gs-render.js renderBlocksInto / renderMdBlock / renderInline) ----

// writeSiteMDBlocks writes a block list, honoring the raw-HTML wrapper markers:
// an htmlopen pushes a sanitized container the following blocks nest into, and
// anything still open when the list ends is closed, so a document truncated
// mid-structure never emits unbalanced markup.
func writeSiteMDBlocks(b *strings.Builder, blocks []siteMDBlock, r *siteMDRender) {
	var open []string
	for _, block := range blocks {
		switch block.Kind {
		case siteMDHTMLOpen:
			tag, closeName := sanitizeSiteHTMLWrapper(block.Raw, r.ctx)
			b.WriteString(tag)
			if closeName != "" {
				b.WriteString("\n")
				open = append(open, closeName)
			}
		case siteMDHTMLClose:
			for i := len(open) - 1; i >= 0; i-- {
				if open[i] != block.Tag {
					continue
				}
				for j := len(open) - 1; j >= i; j-- {
					b.WriteString("</" + open[j] + ">\n")
				}
				open = open[:i]
				break
			}
		case siteMDHTML:
			// Nothing at all when the line does not survive the allowlist (a
			// dropped <script>, a repo-relative <img> with no alt), rather than a
			// blank line where it used to be.
			if out := sanitizeSiteHTML(block.Raw, r.ctx); out != "" {
				b.WriteString(out + "\n")
			}
		default:
			writeSiteMDBlock(b, block, r)
		}
	}
	for j := len(open) - 1; j >= 0; j-- {
		b.WriteString("</" + open[j] + ">\n")
	}
}

// writeSiteMDBlock writes one markdown-native block.
func writeSiteMDBlock(b *strings.Builder, block siteMDBlock, r *siteMDRender) {
	switch block.Kind {
	case siteMDHeading:
		level := strconv.Itoa(block.Level)
		b.WriteString("<h" + level)
		if id := siteMDHeadingID(block.Spans, r); id != "" {
			b.WriteString(` id="` + id + `"`)
		}
		b.WriteString(">")
		writeSiteMDInline(b, block.Spans, r)
		b.WriteString("</h" + level + ">\n")
	case siteMDThematic:
		b.WriteString("<hr>\n")
	case siteMDCode:
		b.WriteString(`<pre class="codeblock"><code>` + html.EscapeString(block.Text) + "</code></pre>\n")
	case siteMDList:
		writeSiteMDList(b, block, r)
	case siteMDTable:
		writeSiteMDTable(b, block, r)
	case siteMDQuote:
		b.WriteString("<blockquote>\n")
		writeSiteMDBlocks(b, block.Blocks, r)
		b.WriteString("</blockquote>\n")
	default:
		b.WriteString("<p>")
		writeSiteMDInline(b, block.Spans, r)
		b.WriteString("</p>\n")
	}
}

// siteMDHeadingID returns a heading's anchor id: the reader's md- prefixed slug,
// deduplicated within the document so every heading is addressable.
func siteMDHeadingID(spans []siteMDSpan, r *siteMDRender) string {
	slug := siteMDSlug(siteMDSpanText(spans))
	if slug == "" {
		return ""
	}
	id := slug
	for n := 1; r.slugs[id]; n++ {
		id = slug + "-" + strconv.Itoa(n)
	}
	r.slugs[id] = true
	return "md-" + id
}

// siteMDSpanText flattens inline spans to their plain text (for heading slugs).
func siteMDSpanText(spans []siteMDSpan) string {
	var b strings.Builder
	for _, s := range spans {
		if len(s.Spans) > 0 {
			b.WriteString(siteMDSpanText(s.Spans))
			continue
		}
		b.WriteString(s.Value)
	}
	return b.String()
}

// writeSiteMDList writes a (possibly nested, task-aware) list.
func writeSiteMDList(b *strings.Builder, block siteMDBlock, r *siteMDRender) {
	tag := "ul"
	if block.Ordered {
		tag = "ol"
	}
	b.WriteString("<" + tag + ">\n")
	for _, item := range block.Items {
		if item.Task == "" {
			b.WriteString("<li>")
		} else {
			b.WriteString(`<li class="task"><input type="checkbox" disabled`)
			if item.Task == "x" {
				b.WriteString(" checked")
			}
			b.WriteString(">")
		}
		writeSiteMDInline(b, item.Spans, r)
		for _, child := range item.Children {
			b.WriteString("\n")
			writeSiteMDList(b, child, r)
		}
		b.WriteString("</li>\n")
	}
	b.WriteString("</" + tag + ">\n")
}

// writeSiteMDTable writes a GFM table with per-column alignment.
func writeSiteMDTable(b *strings.Builder, block siteMDBlock, r *siteMDRender) {
	align := func(i int) string {
		if i < len(block.Aligns) && block.Aligns[i] != "" {
			return ` align="` + block.Aligns[i] + `"`
		}
		return ""
	}
	b.WriteString("<table>\n<thead>\n<tr>")
	for i, cell := range block.Headers {
		b.WriteString("<th" + align(i) + ">")
		writeSiteMDInline(b, cell, r)
		b.WriteString("</th>")
	}
	b.WriteString("</tr>\n</thead>\n<tbody>\n")
	for _, row := range block.Rows {
		b.WriteString("<tr>")
		for i, cell := range row {
			b.WriteString("<td" + align(i) + ">")
			writeSiteMDInline(b, cell, r)
			b.WriteString("</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n")
}

// writeSiteMDInline writes inline spans. Markdown-native spans emit markup this
// file generated; only a rawhtml span reaches the sanitizer, and text is always
// escaped.
func writeSiteMDInline(b *strings.Builder, spans []siteMDSpan, r *siteMDRender) {
	for _, s := range spans {
		switch s.Kind {
		case siteMDInCode:
			b.WriteString("<code>" + html.EscapeString(s.Value) + "</code>")
		case siteMDStrong:
			b.WriteString("<strong>")
			writeSiteMDInline(b, s.Spans, r)
			b.WriteString("</strong>")
		case siteMDEm:
			b.WriteString("<em>")
			writeSiteMDInline(b, s.Spans, r)
			b.WriteString("</em>")
		case siteMDStrike:
			b.WriteString("<del>")
			writeSiteMDInline(b, s.Spans, r)
			b.WriteString("</del>")
		case siteMDImage:
			b.WriteString(siteMDImageHTML(s.Src, s.Alt, ""))
		case siteMDRawHTML:
			b.WriteString(sanitizeSiteHTML(s.Value, r.ctx))
		case siteMDLink:
			if href := siteMDHref(s.Href, r.ctx); href != "" {
				b.WriteString(`<a href="` + html.EscapeString(href) + `">`)
			} else {
				b.WriteString("<a>")
			}
			writeSiteMDInline(b, s.Spans, r)
			b.WriteString("</a>")
		default:
			b.WriteString(html.EscapeString(s.Value))
		}
	}
}
