// util_links.go - Link helpers the detail cards share: mentions in a subject, and the commits that cite an item
package tuicore

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// MarkMentions renders text in style with query highlighted, and each @email mention zone-marked as a link to the author search; the highlight goes on each plain segment before the mark, so it cannot split a zone marker.
func MarkMentions(text string, style lipgloss.Style, query string, anchors *AnchorCollector) string {
	spans := subjectMentions(text)
	if len(spans) == 0 {
		return style.Render(HighlightInText(text, query))
	}
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if s.Start > last {
			b.WriteString(style.Render(HighlightInText(text[last:s.Start], query)))
		}
		b.WriteString(anchors.Mark(LinkStyle(HighlightInText(text[s.Start:s.End], query)), LocFacet("author", s.Email)))
		last = s.End
	}
	if last < len(text) {
		b.WriteString(style.Render(HighlightInText(text[last:], query)))
	}
	return b.String()
}

// subjectMentions finds the mentions of a subject outside its urls, as the body path does by taking the urls out first.
func subjectMentions(text string) []protocol.MentionSpan {
	urls := urlRe.FindAllStringIndex(text, -1)
	var spans []protocol.MentionSpan
	for _, s := range protocol.FindMentions(text) {
		if !insideAny(urls, s.Start) {
			spans = append(spans, s)
		}
	}
	return spans
}

// MarkFacet renders text as a link to the search on one facet: author, assignee, reviewer or label.
func MarkFacet(anchors *AnchorCollector, text, key, value string) string {
	return anchors.Mark(LinkStyle(text), LocFacet(key, value))
}

// MentionLinks lists the mentions of text as links, in the order MarkMentions marks them.
func MentionLinks(text string) []CardLink {
	spans := subjectMentions(text)
	links := make([]CardLink, 0, len(spans))
	for _, s := range spans {
		links = append(links, CardLink{Label: text[s.Start:s.End], Location: LocFacet("author", s.Email)})
	}
	return links
}

// TrailerRefRows renders the "Referenced by" rows of an item from the refs its loader read: each citing commit as a link, with its trailer key as text.
func TrailerRefRows(refs []cache.TrailerRef, styles RowStyles, selectionBar string, anchors *AnchorCollector) []string {
	lines := make([]string, 0, len(refs))
	for i, tr := range refs {
		rowLabel := "Referenced by"
		if i > 0 {
			rowLabel = ""
		}
		subject, _ := protocol.SplitSubjectBody(tr.Message)
		link := anchors.Mark(LinkStyle(subject)+Dim.Render("  "+tr.Hash[:12]), trailerRefLocation(tr))
		lines = append(lines, selectionBar+styles.Label.Render(rowLabel)+link+Dim.Render("  "+tr.TrailerKey))
	}
	return lines
}

// TrailerRefLinks lists the commits that cite an item as links, in the order TrailerRefRows marks them.
func TrailerRefLinks(refs []cache.TrailerRef) []CardLink {
	links := make([]CardLink, 0, len(refs))
	for _, tr := range refs {
		subject, _ := protocol.SplitSubjectBody(tr.Message)
		links = append(links, CardLink{Label: subject, Location: trailerRefLocation(tr)})
	}
	return links
}

// trailerRefLocation is the target of a citing commit: its item when the cache knows one, else its diff, resolved at navigation time.
func trailerRefLocation(tr cache.TrailerRef) Location {
	return LocRef(protocol.CreateRef(protocol.RefTypeCommit, tr.Hash, tr.RepoURL, tr.Branch))
}
