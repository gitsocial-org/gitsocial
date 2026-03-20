// util_card_render.go - Card rendering functions
package tuicore

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var (
	// Package-level glamour renderers (no word wrap - done via lipgloss)
	markdownRenderer      *glamour.TermRenderer
	mutedMarkdownRenderer *glamour.TermRenderer
	boldMarkdownRenderer  *glamour.TermRenderer
	// Matches email autolinks like <user@domain.com> to escape them before glamour
	emailAutolinkRe = regexp.MustCompile(`<([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})>`)
	// Matches fenced code blocks (``` or ~~~)
	codeBlockRe = regexp.MustCompile("(?s)(?:```(\\w*)\\n(.*?)```|~~~(\\w*)\\n(.*?)~~~)")
	// Matches bare URLs (not inside markdown link syntax or HTML attributes)
	urlRe = regexp.MustCompile(`https?://[^\s)>\]"]+`)
	// Matches markdown links [text](http-url) with capture groups
	mdLinkExtractRe = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`)
	// Matches markdown images ![alt](url) with optional {attrs} suffix (GitLab/Kramdown)
	mdImageExtractRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)(?:\{[^}]*\})?`)
	// Matches HTML <img> tags (self-closing or not)
	htmlImgRe    = regexp.MustCompile(`<img\s[^>]*?>`)
	htmlImgSrcRe = regexp.MustCompile(`src=["']([^"']+)["']`)
	htmlImgAltRe = regexp.MustCompile(`alt=["']([^"']+)["']`)
	// Email styling
	emailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(AccentEmail))
	// Known image file extensions
	imageExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".svg": true, ".webp": true, ".bmp": true, ".tiff": true, ".ico": true,
	}
)

// glamourCache caches glamour.Render() output keyed by renderer variant + input text.
var glamourCache = struct {
	entries map[string]string
}{entries: make(map[string]string, 256)}

// cachedGlamourRender calls renderer.Render with caching. variant identifies the renderer (e.g. "n", "m", "b").
func cachedGlamourRender(renderer *glamour.TermRenderer, variant byte, input string) (string, error) {
	key := string(variant) + input
	if cached, ok := glamourCache.entries[key]; ok {
		return cached, nil
	}
	rendered, err := renderer.Render(input)
	if err != nil {
		return "", err
	}
	if len(glamourCache.entries) >= 256 {
		glamourCache.entries = make(map[string]string, 256)
	}
	glamourCache.entries[key] = rendered
	return rendered, nil
}

const codePlaceholderPrefix = "\x00CODE"
const urlPlaceholderPrefix = "\x00URL"
const mdLinkPlaceholderPrefix = "\x00MDLINK"
const mdImagePlaceholderPrefix = "\x00MDIMG"

type codeBlock struct {
	language string
	code     string
}

type mdLink struct {
	text string
	url  string
}

type mdImage struct {
	alt string
	url string
}

// ExtractCodeBlocks extracts fenced code blocks from content and replaces them with placeholders.
func ExtractCodeBlocks(content string) (string, []codeBlock) {
	var blocks []codeBlock
	result := codeBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := codeBlockRe.FindStringSubmatch(match)
		var lang, code string
		if subs[1] != "" || subs[2] != "" {
			lang = subs[1]
			code = subs[2]
		} else {
			lang = subs[3]
			code = subs[4]
		}
		idx := len(blocks)
		blocks = append(blocks, codeBlock{language: lang, code: code})
		return fmt.Sprintf("\n%s%d\x00\n", codePlaceholderPrefix, idx)
	})
	return result, blocks
}

// RestoreCodeBlocks replaces code block placeholders with Chroma-highlighted output.
func RestoreCodeBlocks(content string, blocks []codeBlock, dimmed bool) string {
	for i, block := range blocks {
		placeholder := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, i)
		highlighted := HighlightCode(strings.TrimRight(block.code, "\n"), block.language, dimmed)
		content = strings.Replace(content, placeholder, highlighted, 1)
	}
	return content
}

// ExtractMarkdownLinks extracts [text](http-url) patterns and replaces them with placeholders.
// Must run BEFORE ExtractURLs so bare URL extraction doesn't eat URLs inside markdown links.
func ExtractMarkdownLinks(content string) (string, []mdLink) {
	var links []mdLink
	result := mdLinkExtractRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := mdLinkExtractRe.FindStringSubmatch(match)
		idx := len(links)
		links = append(links, mdLink{text: subs[1], url: subs[2]})
		return fmt.Sprintf("%s%d\x00", mdLinkPlaceholderPrefix, idx)
	})
	return result, links
}

// RestoreMarkdownLinks replaces markdown link placeholders with styled OSC 8 terminal hyperlinks.
func RestoreMarkdownLinks(content string, links []mdLink, anchors *AnchorCollector) string {
	for i, link := range links {
		placeholder := fmt.Sprintf("%s%d\x00", mdLinkPlaceholderPrefix, i)
		replacement := anchors.MarkLink(link.text, link.url, Location{Path: link.url})
		content = strings.Replace(content, placeholder, replacement, 1)
	}
	return content
}

// ConvertHTMLImages converts HTML <img> tags to markdown ![alt](src) syntax.
func ConvertHTMLImages(text string) string {
	return htmlImgRe.ReplaceAllStringFunc(text, func(match string) string {
		srcMatch := htmlImgSrcRe.FindStringSubmatch(match)
		if len(srcMatch) < 2 {
			return match
		}
		alt := ""
		if altMatch := htmlImgAltRe.FindStringSubmatch(match); len(altMatch) >= 2 {
			alt = altMatch[1]
		}
		return fmt.Sprintf("![%s](%s)", alt, srcMatch[1])
	})
}

// ExtractMarkdownImages extracts ![alt](url) patterns and replaces them with placeholders.
// Must run BEFORE ExtractMarkdownLinks so link extraction doesn't eat the [alt](url) inside images.
func ExtractMarkdownImages(content string) (string, []mdImage) {
	var images []mdImage
	result := mdImageExtractRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := mdImageExtractRe.FindStringSubmatch(match)
		idx := len(images)
		images = append(images, mdImage{alt: subs[1], url: subs[2]})
		return fmt.Sprintf("%s%d\x00", mdImagePlaceholderPrefix, idx)
	})
	return result, images
}

// RestoreMarkdownImages replaces image placeholders with styled [IMAGE: alt] indicators.
func RestoreMarkdownImages(content string, images []mdImage, anchors *AnchorCollector) string {
	for i, img := range images {
		placeholder := fmt.Sprintf("%s%d\x00", mdImagePlaceholderPrefix, i)
		label := "IMAGE"
		if img.alt != "" {
			label = "IMAGE: " + img.alt
		}
		replacement := "\x1b[38;5;" + AccentImage + "m[" + label + "]\x1b[39m"
		replacement = fmt.Sprintf("\x1b]8;;%s\x07%s\x1b]8;;\x07", img.url, replacement)
		if anchors != nil {
			loc := Location{Path: img.url}
			replacement = anchors.Mark(replacement, loc)
		}
		content = strings.Replace(content, placeholder, replacement, 1)
	}
	return content
}

// IsImageURL checks if a URL path ends with a known image extension (case-insensitive).
func IsImageURL(url string) bool {
	u := strings.SplitN(url, "?", 2)[0]
	u = strings.SplitN(u, "#", 2)[0]
	ext := strings.ToLower(path.Ext(u))
	return imageExtensions[ext]
}

// ExtractURLs extracts bare URLs from content and replaces them with placeholders.
// URLs inside markdown link syntax [text](url) are skipped to avoid breaking glamour rendering.
func ExtractURLs(content string) (string, []string) {
	mdSpans := mdLinkExtractRe.FindAllStringIndex(content, -1)
	urlMatches := urlRe.FindAllStringIndex(content, -1)
	if len(urlMatches) == 0 {
		return content, nil
	}
	isInMdLink := func(start, end int) bool {
		for _, span := range mdSpans {
			if start >= span[0] && end <= span[1] {
				return true
			}
		}
		return false
	}
	// Replace from end to preserve earlier offsets
	var urls []string
	var toReplace []struct{ start, end, idx int }
	for _, m := range urlMatches {
		if isInMdLink(m[0], m[1]) {
			continue
		}
		idx := len(urls)
		urls = append(urls, content[m[0]:m[1]])
		toReplace = append(toReplace, struct{ start, end, idx int }{m[0], m[1], idx})
	}
	for i := len(toReplace) - 1; i >= 0; i-- {
		r := toReplace[i]
		placeholder := fmt.Sprintf("%s%d\x00", urlPlaceholderPrefix, r.idx)
		content = content[:r.start] + placeholder + content[r.end:]
	}
	return content, urls
}

// RestoreURLs replaces URL placeholders with styled, zone-marked URLs (when anchors != nil) or plain styled URLs.
func RestoreURLs(content string, urls []string, anchors *AnchorCollector) string {
	for i, u := range urls {
		placeholder := fmt.Sprintf("%s%d\x00", urlPlaceholderPrefix, i)
		replacement := anchors.MarkLink(u, u, Location{Path: u})
		content = strings.Replace(content, placeholder, replacement, 1)
	}
	return content
}

// ExtractContentLinks extracts markdown images, markdown links, and bare URLs from text and returns them as CardLinks.
// When repoURL and branch are provided, relative paths are resolved to raw file URLs.
func ExtractContentLinks(text, repoURL, branch string) []CardLink {
	text = ConvertHTMLImages(text)
	imgMatches := mdImageExtractRe.FindAllStringSubmatch(text, -1)
	links := make([]CardLink, 0, len(imgMatches))
	for _, match := range imgMatches {
		label := "IMAGE"
		if match[1] != "" {
			label = "IMAGE: " + match[1]
		}
		links = append(links, CardLink{Label: label, Location: Location{Path: resolveContentURL(match[2], repoURL, branch)}})
	}
	// Remove images from text so link extraction doesn't double-count
	textWithoutImages := mdImageExtractRe.ReplaceAllString(text, "")
	matches := mdLinkExtractRe.FindAllStringSubmatch(textWithoutImages, -1)
	for _, match := range matches {
		links = append(links, CardLink{Label: match[1], Location: Location{Path: resolveContentURL(match[2], repoURL, branch)}})
	}
	_, urls := ExtractURLs(textWithoutImages)
	for _, u := range urls {
		links = append(links, CardLink{Label: u, Location: Location{Path: u}})
	}
	return links
}

// resolveContentURL resolves a relative path to a full URL using the repo's raw file URL.
func resolveContentURL(rawURL, repoURL, branch string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if repoURL == "" {
		return rawURL
	}
	// Platform upload paths (e.g. /uploads/hash/file on GitLab) resolve using stored project ID.
	if strings.HasPrefix(rawURL, "/uploads/") {
		normalized := protocol.NormalizeURL(repoURL)
		if protocol.DetectHost(repoURL) == protocol.HostGitLab {
			if pid, err := cache.GetRepositoryMeta(normalized, "platform_project_id"); err == nil && pid != "" {
				parsed, _ := url.Parse(normalized)
				return parsed.Scheme + "://" + parsed.Host + "/-/project/" + pid + rawURL
			}
		}
		return normalized + rawURL
	}
	return protocol.FileURL(repoURL, branch, rawURL)
}

// ResolveContentURLs rewrites relative URLs in markdown images and links to absolute raw file URLs.
func ResolveContentURLs(text, repoURL, branch string) string {
	if repoURL == "" {
		return text
	}
	text = ConvertHTMLImages(text)
	// Resolve images: ![alt](relative) → ![alt](https://...)
	text = mdImageExtractRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := mdImageExtractRe.FindStringSubmatch(match)
		resolved := resolveContentURL(subs[2], repoURL, branch)
		if resolved == subs[2] {
			return match
		}
		// Preserve the full match but with resolved URL (drop {attrs} suffix since it's consumed by regex)
		return fmt.Sprintf("![%s](%s)", subs[1], resolved)
	})
	// Resolve links: [text](relative) → [text](https://...)
	text = mdLinkExtractRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := mdLinkExtractRe.FindStringSubmatch(match)
		resolved := resolveContentURL(subs[2], repoURL, branch)
		if resolved == subs[2] {
			return match
		}
		return fmt.Sprintf("[%s](%s)", subs[1], resolved)
	})
	return text
}

// cardRenderer implements CardRenderer
type cardRenderer struct{}

// RenderCard renders a card to a string.
func (r cardRenderer) RenderCard(card Card, opts CardOptions) string {
	return RenderCard(card, opts)
}

// CardHeight calculates the height in lines for a card.
func (r cardRenderer) CardHeight(card Card, opts CardOptions) int {
	return CardHeight(card, opts)
}

func init() {
	markdownRenderer, _ = glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(0),
		glamour.WithStylesFromJSONBytes([]byte(`{"document":{"margin":0}}`)),
	)
	mutedMarkdownRenderer, _ = glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(0),
		glamour.WithStylesFromJSONBytes([]byte(`{"document":{"margin":0,"color":"245"},"paragraph":{"color":"245"},"code_block":{"color":"245"},"link":{"color":"245"},"link_text":{"color":"245"}}`)),
	)
	boldMarkdownRenderer, _ = glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(0),
		glamour.WithStylesFromJSONBytes([]byte(`{"document":{"margin":0}}`)),
	)
	// Register as the default card renderer
	DefaultCardRenderer = cardRenderer{}
}

// RenderCard renders a Card with the given options using a unified compositional approach.
// The card's Nested slice determines what embedded content is shown (parents, originals, etc.)
// MaxLines: -1 = unlimited, 0 = default (5), >0 = specific limit
func RenderCard(card Card, opts CardOptions) string {
	if opts.MaxLines == 0 {
		opts.MaxLines = 5
	} else if opts.MaxLines < 0 {
		opts.MaxLines = 10000 // effectively unlimited
	}
	if card.Header.IsRetracted {
		opts.Retracted = true
	}
	if card.Header.IsStale {
		opts.Dimmed = true
	}

	selectionBar := " "
	if opts.Selected {
		selectionBar = Title.Render("▏")
	}

	// Compute icon padding for content alignment
	iconPad := ""
	if card.Header.Icon != "" {
		iconWidth := AnsiWidth(card.Header.Icon) + 2 // icon + 2 spaces
		iconPad = strings.Repeat(" ", iconWidth)
		if opts.WrapWidth > iconWidth {
			opts.WrapWidth -= iconWidth
		}
	}

	var str strings.Builder

	// 1. Header
	str.WriteString(opts.Indent)
	str.WriteString(renderHeader(card.Header, selectionBar, opts))

	// 2. Nested cards with Position="before" (e.g., parent context for comments)
	for _, nested := range card.Nested {
		if nested.Position == "before" {
			str.WriteString("\n")
			str.WriteString(renderNestedCard(nested, selectionBar, opts.Width))
		}
	}

	// 3. Content
	if opts.CommitMessage != "" {
		str.WriteString(renderContent(CardContent{Text: opts.CommitMessage}, selectionBar, iconPad, CardOptions{Raw: true, WrapWidth: opts.WrapWidth, MaxLines: opts.MaxLines}))
	} else if card.Content.Text != "" {
		str.WriteString(renderContent(card.Content, selectionBar, iconPad, opts))
	}

	// 4. Stats (before nested to avoid confusion with nested card stats)
	if opts.ShowStats && len(card.Stats) > 0 {
		str.WriteString("\n")
		str.WriteString(opts.Indent)
		str.WriteString(selectionBar)
		str.WriteString(iconPad)
		str.WriteString(renderStats(card, opts))
	}

	// 5. Nested cards with Position="after" (e.g., quoted/reposted original)
	for _, nested := range card.Nested {
		if nested.Position == "after" {
			str.WriteString("\n")
			str.WriteString(selectionBar)
			str.WriteString("\n")
			str.WriteString(renderNestedCard(nested, selectionBar, opts.Width))
		}
	}

	// 6. Separator
	if opts.Separator && opts.Width > 2 {
		str.WriteString("\n\n ")
		str.WriteString(Dim.Render(strings.Repeat("─", opts.Width-1)))
		str.WriteString("\n")
	}

	return str.String()
}

// renderHeader renders the card header line
func renderHeader(header CardHeader, selectionBar string, opts CardOptions) string {
	var str strings.Builder
	str.WriteString(selectionBar)
	if header.Icon != "" {
		str.WriteString(Dim.Render(header.Icon))
		str.WriteString("  ")
	}
	if header.IsRetracted {
		str.WriteString(RetractedBadge.Render("[RETRACTED]"))
		str.WriteString(" ")
	}
	titleText := header.TitleStyle(opts.Dimmed).Render(header.Title)
	if header.TitleLink != nil {
		titleText = opts.Anchors.Mark(titleText, *header.TitleLink)
	}
	str.WriteString(titleText)

	rest := ""
	if header.Badge != "" {
		rest += " " + header.Badge
	}
	if header.IsEdited {
		rest += " ✎"
	}
	if rest != "" {
		str.WriteString(Dim.Render(rest))
	}

	// Render structured subtitle parts, truncating to fit within width
	usedWidth := AnsiWidth(str.String())
	for _, part := range header.Subtitle {
		partWidth := 3 + AnsiWidth(part.Text) // " · " + text
		if opts.Width > 0 && usedWidth+partWidth > opts.Width {
			break
		}
		str.WriteString(Dim.Render(" · "))
		text := part.Text
		if part.Link != nil {
			text = Dim.Render(text)
			text = opts.Anchors.Mark(text, *part.Link)
		} else {
			text = Dim.Render(text)
		}
		str.WriteString(text)
		usedWidth += partWidth
	}

	return str.String()
}

// renderStats renders the stats line with optional zone marking for linked stats.
func renderStats(card Card, opts CardOptions) string {
	parts := make([]string, 0, len(card.Stats))
	for _, stat := range card.Stats {
		text := stat.Text
		if stat.Link != nil {
			text = Dim.Render(text)
			text = opts.Anchors.Mark(text, *stat.Link)
		} else {
			text = Dim.Render(text)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, Dim.Render(" · "))
}

// RenderMarkdown renders text with markdown formatting, math, email colorization, and word wrapping.
func RenderMarkdown(text string, wrapWidth int) string {
	text = strings.TrimSpace(text)
	text = escapeEmailAutolinks(text)
	textWithCodePlaceholders, codeBlocks := ExtractCodeBlocks(text)
	contentWithPlaceholders, extractedMath := RenderMathWithPlaceholders(textWithCodePlaceholders)
	rendered, err := cachedGlamourRender(markdownRenderer, 'n', contentWithPlaceholders)
	if err != nil {
		text = RenderMath(text)
	} else {
		text = RestoreMath(rendered, extractedMath)
		text = strings.TrimSpace(text)
	}
	text = RestoreCodeBlocks(text, codeBlocks, false)
	text = colorizeEmails(text)
	if wrapWidth > 0 {
		text = lipgloss.NewStyle().Width(wrapWidth).Render(text)
	}
	return text
}

// RenderMarkdownWithAnchors renders markdown text with link extraction and anchor marking for tab navigation.
// Use this instead of RenderMarkdown when body content needs clickable/focusable links.
func RenderMarkdownWithAnchors(text string, wrapWidth int, anchors *AnchorCollector) string {
	text = strings.TrimSpace(text)
	text = ConvertHTMLImages(text)
	textNoImages, mdImages := ExtractMarkdownImages(text)
	textNoLinks, mdLinks := ExtractMarkdownLinks(textNoImages)
	textNoURLs, urls := ExtractURLs(textNoLinks)
	return renderMarkdownWithURLs(textNoURLs, wrapWidth, mdImages, mdLinks, urls, anchors)
}

// renderMarkdownWithURLs renders markdown text with pre-extracted images, markdown links, and bare URLs restored after glamour.
func renderMarkdownWithURLs(text string, wrapWidth int, mdImages []mdImage, mdLinks []mdLink, urls []string, anchors *AnchorCollector) string {
	text = strings.TrimSpace(text)
	text = escapeEmailAutolinks(text)
	textWithCodePlaceholders, codeBlocks := ExtractCodeBlocks(text)
	contentWithPlaceholders, extractedMath := RenderMathWithPlaceholders(textWithCodePlaceholders)
	rendered, err := cachedGlamourRender(markdownRenderer, 'n', contentWithPlaceholders)
	if err != nil {
		text = RenderMath(text)
	} else {
		text = RestoreMath(rendered, extractedMath)
		text = strings.TrimSpace(text)
	}
	text = RestoreCodeBlocks(text, codeBlocks, false)
	text = colorizeEmails(text)
	text = RestoreMarkdownImages(text, mdImages, anchors)
	text = RestoreMarkdownLinks(text, mdLinks, anchors)
	text = RestoreURLs(text, urls, anchors)
	if wrapWidth > 0 {
		text = lipgloss.NewStyle().Width(wrapWidth).Render(text)
	}
	return text
}

// renderContent renders the card content with optional truncation
func renderContent(content CardContent, selectionBar string, iconPad string, opts CardOptions) string {
	var str strings.Builder

	text := strings.TrimSpace(content.Text)

	// Extract images, markdown links, and bare URLs before rendering so glamour doesn't mangle them
	var extractedMdImages []mdImage
	var extractedMdLinks []mdLink
	var extractedURLs []string
	if !opts.Raw {
		text = ConvertHTMLImages(text)
		text, extractedMdImages = ExtractMarkdownImages(text)
		text, extractedMdLinks = ExtractMarkdownLinks(text)
		text, extractedURLs = ExtractURLs(text)
	}

	if !opts.Raw {
		if opts.Markdown {
			if opts.Dimmed || opts.Bold {
				text = escapeEmailAutolinks(text)
				textWithCodePlaceholders, codeBlocks := ExtractCodeBlocks(text)
				contentWithPlaceholders, extractedMath := RenderMathWithPlaceholders(textWithCodePlaceholders)
				renderer := mutedMarkdownRenderer
				variant := byte('m')
				if opts.Bold {
					renderer = boldMarkdownRenderer
					variant = 'b'
				}
				rendered, err := cachedGlamourRender(renderer, variant, contentWithPlaceholders)
				if err != nil {
					text = RenderMath(text)
				} else {
					text = RestoreMath(rendered, extractedMath)
					text = strings.TrimSpace(text)
				}
				text = RestoreCodeBlocks(text, codeBlocks, opts.Dimmed)
				text = colorizeEmails(text)
				text = RestoreMarkdownImages(text, extractedMdImages, opts.Anchors)
				text = RestoreMarkdownLinks(text, extractedMdLinks, opts.Anchors)
				text = RestoreURLs(text, extractedURLs, opts.Anchors)
				if opts.WrapWidth > 0 {
					text = lipgloss.NewStyle().Width(opts.WrapWidth).Render(text)
				}
			} else {
				text = renderMarkdownWithURLs(text, opts.WrapWidth, extractedMdImages, extractedMdLinks, extractedURLs, opts.Anchors)
			}
		} else {
			text = RenderMath(text)
			text = RestoreMarkdownImages(text, extractedMdImages, opts.Anchors)
			text = RestoreMarkdownLinks(text, extractedMdLinks, opts.Anchors)
			text = RestoreURLs(text, extractedURLs, opts.Anchors)
			if opts.WrapWidth > 0 {
				text = lipgloss.NewStyle().Width(opts.WrapWidth).Render(text)
			}
		}
	} else if opts.WrapWidth > 0 {
		text = lipgloss.NewStyle().Width(opts.WrapWidth).Render(text)
	}

	// Highlight search text
	if opts.HighlightText != "" {
		text = HighlightInText(text, opts.HighlightText)
	}

	var contentLines []string
	showMore := false

	if opts.MaxLines == 1 {
		// Single line: flatten content
		text = strings.ReplaceAll(text, "\n", " ")
		if len(text) > opts.Width-10 && opts.Width > 10 {
			text = TruncateToWidth(text, opts.Width-10)
		}
		contentLines = []string{text}
	} else {
		contentLines = strings.Split(text, "\n")
		if len(contentLines) > opts.MaxLines {
			contentLines = contentLines[:opts.MaxLines]
			showMore = true
		}
	}

	for _, line := range contentLines {
		str.WriteString("\n")
		str.WriteString(opts.Indent)
		str.WriteString(selectionBar)
		str.WriteString(iconPad)
		if opts.Retracted {
			str.WriteString(Retracted.Render(line))
		} else if opts.Dimmed && !opts.Markdown {
			// Only apply dim styling if not using markdown renderer (which handles its own styling)
			str.WriteString(Dim.Render(line))
		} else if opts.Bold && !opts.Markdown {
			str.WriteString(Bold.Render(line))
		} else {
			str.WriteString(line)
		}
	}

	if showMore {
		str.WriteString("\n")
		str.WriteString(opts.Indent)
		str.WriteString(selectionBar)
		str.WriteString(iconPad)
		if opts.Retracted {
			str.WriteString(Retracted.Render("···"))
		} else {
			str.WriteString("···")
		}
	}

	return str.String()
}

// escapeEmailAutolinks escapes <email@domain> patterns to prevent CommonMark autolink parsing.
// Replaces <email> with \<email\> so glamour renders it as literal angle brackets + email.
func escapeEmailAutolinks(text string) string {
	return emailAutolinkRe.ReplaceAllString(text, `\<$1\>`)
}

// colorizeEmails adds color to <email@domain> patterns in rendered text.
func colorizeEmails(text string) string {
	return emailAutolinkRe.ReplaceAllStringFunc(text, func(match string) string {
		email := emailAutolinkRe.FindStringSubmatch(match)[1]
		return "<" + emailStyle.Render(email) + ">"
	})
}

// searchPatternCache caches the last compiled search pattern to avoid
// recompilation on every render call. Only one search is active at a time
// in the single-threaded bubbletea event loop.
var searchPatternCache struct {
	query   string
	pattern *regexp.Regexp
}

// ExtractSearchTerms extracts the actual search terms from a query, removing scope prefixes.
// E.g., "repository:url hello world" -> "hello world"
func ExtractSearchTerms(query string) string {
	prefixes := []string{"repository:", "repo:", "list:", "author:", "type:", "hash:", "commit:", "after:", "before:"}
	result := query
	for _, prefix := range prefixes {
		for {
			idx := strings.Index(strings.ToLower(result), prefix)
			if idx == -1 {
				break
			}
			end := idx + len(prefix)
			for end < len(result) && result[end] != ' ' {
				end++
			}
			result = result[:idx] + result[end:]
		}
	}
	return strings.TrimSpace(result)
}

// CompileSearchPattern compiles a case-insensitive search pattern, caching the result.
func CompileSearchPattern(query string) *regexp.Regexp {
	if query == "" {
		return nil
	}
	if searchPatternCache.query == query {
		return searchPatternCache.pattern
	}
	searchPatternCache.query = query
	searchPatternCache.pattern = regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
	return searchPatternCache.pattern
}

// HighlightInText highlights all occurrences of query in text (case-insensitive).
func HighlightInText(text, query string) string {
	pattern := CompileSearchPattern(query)
	if pattern == nil {
		return text
	}
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		return Highlight.Render(match)
	})
}

// renderNestedCard renders an embedded card (parent, original, etc.)
func renderNestedCard(nested NestedCard, selectionBar string, width int) string {
	maxLines := nested.MaxLines
	if maxLines == 0 {
		maxLines = 5
	}

	padding := "   "
	nestedIconPadWidth := 0
	nestedIconPad := ""
	if nested.Card.Header.Icon != "" {
		nestedIconPadWidth = AnsiWidth(nested.Card.Header.Icon) + 2
		nestedIconPad = strings.Repeat(" ", nestedIconPadWidth)
	}
	var str strings.Builder

	// Header
	str.WriteString(selectionBar)
	str.WriteString(padding)
	if nested.Card.Header.Icon != "" {
		str.WriteString(Dim.Render(nested.Card.Header.Icon))
		str.WriteString("  ")
	}
	str.WriteString(nested.Card.Header.TitleStyle(nested.Dimmed).Render(nested.Card.Header.Title))
	rest := ""
	if nested.Card.Header.Badge != "" {
		rest += " " + nested.Card.Header.Badge
	}
	str.WriteString(Dim.Render(rest))
	for _, part := range nested.Card.Header.Subtitle {
		str.WriteString(Dim.Render(" · "))
		str.WriteString(Dim.Render(part.Text))
	}

	// Content (trimmed)
	content := strings.TrimSpace(nested.Card.Content.Text)
	content = RenderMath(content)
	// Word wrap to fit within panel: subtract selectionBar(1) + padding(3) + iconPad
	wrapWidth := width - 4 - nestedIconPadWidth
	if wrapWidth > 0 {
		content = lipgloss.NewStyle().Width(wrapWidth).Render(content)
	}
	contentLines := strings.Split(content, "\n")
	truncated := false
	if len(contentLines) > maxLines {
		contentLines = contentLines[:maxLines]
		truncated = true
	}

	for _, line := range contentLines {
		str.WriteString("\n")
		str.WriteString(selectionBar)
		str.WriteString(padding)
		str.WriteString(nestedIconPad)
		if nested.Dimmed {
			str.WriteString(Dim.Render(line))
		} else {
			str.WriteString(line)
		}
	}

	if truncated {
		str.WriteString("\n")
		str.WriteString(selectionBar)
		str.WriteString(padding)
		str.WriteString(nestedIconPad)
		if nested.Dimmed {
			str.WriteString(Dim.Render("···"))
		} else {
			str.WriteString("···")
		}
	}

	// Stats
	if len(nested.Card.Stats) > 0 {
		str.WriteString("\n")
		str.WriteString(selectionBar)
		str.WriteString(padding)
		str.WriteString(nestedIconPad)
		statTexts := make([]string, len(nested.Card.Stats))
		for i, s := range nested.Card.Stats {
			statTexts[i] = s.Text
		}
		str.WriteString(Dim.Render(strings.Join(statTexts, " · ")))
	}

	return str.String()
}

// CardHeight returns the height in lines for a card with given options.
// This unified calculation mirrors the RenderCard logic.
func CardHeight(card Card, opts CardOptions) int {
	if opts.MaxLines == 0 {
		opts.MaxLines = 5
	}

	height := 1 // header

	// Nested cards with Position="before"
	for _, nested := range card.Nested {
		if nested.Position == "before" {
			height += nestedCardHeight(nested)
		}
	}

	// Content
	if card.Content.Text != "" {
		height += cardContentHeight(card.Content, opts)
	}

	// Stats (before nested to match RenderCard)
	if opts.ShowStats && len(card.Stats) > 0 {
		height++
	}

	// Nested cards with Position="after" (includes blank line before each)
	for _, nested := range card.Nested {
		if nested.Position == "after" {
			height += 1 + nestedCardHeight(nested) // +1 for blank line
		}
	}

	// Separator (blank line + separator + blank line)
	if opts.Separator {
		height += 3
	}

	return height
}

// cardContentHeight calculates the height of content with truncation.
func cardContentHeight(content CardContent, opts CardOptions) int {
	text := strings.TrimSpace(content.Text)
	contentLines := strings.Split(text, "\n")
	numLines := len(contentLines)
	truncated := false
	if numLines > opts.MaxLines {
		numLines = opts.MaxLines
		truncated = true
	}
	height := numLines
	if truncated {
		height++ // "···" indicator
	}
	return height
}

// nestedCardHeight calculates the height of a nested card.
func nestedCardHeight(nested NestedCard) int {
	maxLines := nested.MaxLines
	if maxLines == 0 {
		maxLines = 5
	}

	content := strings.TrimSpace(nested.Card.Content.Text)
	contentLines := strings.Split(content, "\n")
	numLines := len(contentLines)
	truncated := false
	if numLines > maxLines {
		numLines = maxLines
		truncated = true
	}

	height := 1 + numLines // header + content lines
	if truncated {
		height++ // "···" indicator
	}
	if len(nested.Card.Stats) > 0 {
		height++ // stats line
	}
	return height
}

// IsLocalPath returns true if the URL is a local filesystem path (not a remote URL)
func IsLocalPath(url string) bool {
	return strings.HasPrefix(url, "/") ||
		(!strings.HasPrefix(url, "http://") &&
			!strings.HasPrefix(url, "https://") &&
			!strings.HasPrefix(url, "git@"))
}

// BuildCommitRef builds a smart commit reference string.
// For workspace items (repoURL matches workspaceURL or is local), shows just "#hash".
// For external items, shows "repo#commit:hash@branch".
func BuildCommitRef(repoURL, hash, branch, workspaceURL string) string {
	if repoURL == "" || hash == "" {
		return ""
	}
	shortHash := hash
	if len(shortHash) > 12 {
		shortHash = shortHash[:12]
	}
	isWorkspace := repoURL == workspaceURL || IsLocalPath(repoURL)
	var ref string
	if isWorkspace {
		ref = "#" + shortHash
	} else {
		ref = repoURL + "#commit:" + shortHash
	}
	if branch != "" {
		ref += "@" + branch
	}
	return ref
}

// BuildRef builds a repo reference from an item's ID, repository, and branch.
// Extracts hash from ID, formats as "repo#commit:hash@branch" (or "#hash" for local).
func BuildRef(id, repoURL, branch string, isWorkspace bool) string {
	if repoURL == "" {
		return ""
	}
	parsed := protocol.ParseRef(id)
	if parsed.Value == "" {
		return ""
	}
	hash := parsed.Value
	if len(hash) > 12 {
		hash = hash[:12]
	}
	var ref string
	if isWorkspace || IsLocalPath(repoURL) {
		ref = "#" + hash
	} else {
		ref = repoURL + "#commit:" + hash
	}
	if branch != "" {
		ref += "@" + branch
	}
	return ref
}

// TruncateToWidth truncates a string to fit within maxWidth characters
func TruncateToWidth(s string, maxWidth int) string {
	if AnsiWidth(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for i := len(runes) - 1; i >= 0; i-- {
		truncated := string(runes[:i]) + "..."
		if AnsiWidth(truncated) <= maxWidth {
			return truncated
		}
	}
	return "..."
}

// Pluralize returns the singular or plural form based on count
func Pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// Hyperlink wraps text in an OSC 8 terminal hyperlink with blue + underline styling.
// Uses raw ANSI codes (like AnchorCollector.Mark) because lipgloss Underline(true)
// enables useSpaceStyler which shatters existing ANSI escape sequences.
func Hyperlink(linkURL, text string) string {
	if linkURL == "" {
		return text
	}
	h := fmt.Sprintf("\x1b]8;;%s\x07%s\x1b]8;;\x07", linkURL, text)
	h = "\x1b[38;5;" + AccentHyperlink + ";4m" + h + "\x1b[39;24m"
	return h
}

// LinkStyle applies link color and underline without an OSC 8 hyperlink.
func LinkStyle(text string) string {
	return "\x1b[38;5;" + AccentHyperlink + ";4m" + text + "\x1b[39;24m"
}
