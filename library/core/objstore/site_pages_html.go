// site_pages_html.go - templates and styling for the static HTML pages: the
// shared head (meta/OG/canonical/PE hooks), the item/list/front templates, the
// two-layer CSS (tiny inline base + pages.css), and the presentation builders
// (chips, meta lines, paragraphs, description extraction).
//
// Everything renders through html/template so every subject/body/author/header
// value — all attacker-controlled — is context-escaped; nothing ever passes
// through template.HTML. The visual spec is the prototype set in .local/lite/.

package objstore

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

const (
	// sitePagesCSSKey is the pages' shared stylesheet, their only subresource.
	sitePagesCSSKey = "pages.css"
	// sitePagesFrontKey is the front page's bucket key. Since the M8 entry flip
	// the generated front page (README + timeline + PE hooks + gs-upgrade.js) IS
	// index.html — the pages maintainer owns index.html whenever the page layer
	// is effective, and uploadSiteFiles owns the embedded shell index.html only
	// when it is not. The old timeline.html key was retired on the flip (it never
	// deployed to production, so URLs-are-forever does not bind).
	sitePagesFrontKey = "index.html"
	// sitePagesLegacyFrontKey is the pre-flip front-page key, swept on every push
	// so a bucket first pushed by a pre-M8 binary (which wrote timeline.html) does
	// not keep serving a stale duplicate front page.
	sitePagesLegacyFrontKey = "timeline.html"
	// sitePagesUpgradeKey is the shell's page-entry boot asset (gs-upgrade.js),
	// referenced defer by every generated page; uploaded with the other shell
	// assets by uploadSiteFiles.
	sitePagesUpgradeKey = "gs-upgrade.js"
	// sitePagesSitemapKey is the sitemap entry point: a single <urlset> until the
	// URL count exceeds one part, then a <sitemapindex> over the parts.
	sitePagesSitemapKey = "sitemap.xml"
	// sitePagesSitemapHeadKey is index mode's mutable newest part; the numbered
	// sitemap-<n>.xml parts are sealed (full, long-cached).
	sitePagesSitemapHeadKey = "sitemap-head.xml"
	// sitePagesRobotsKey is the crawler policy file.
	sitePagesRobotsKey = "robots.txt"
	// sitePagesFeedKey is the Atom 1.0 feed of the newest top-level items, one
	// more crawl-surface artifact in the sitemap/robots class.
	sitePagesFeedKey = "feed.xml"
	// siteFeedBodyMax caps one feed entry's raw body bytes before paragraph
	// rendering (a cut appends a truncation marker, like the README cap).
	siteFeedBodyMax = 4 * 1024
	// sitePageDescriptionLen bounds the meta/OG description (~160 chars).
	sitePageDescriptionLen = 160
)

// sitePageMaxReplies caps a thread's inlined replies; the rest truncate into an
// explicit "N more replies" marker (the app shows the full thread). A var so
// tests can lower it.
var sitePageMaxReplies = 100

// sitePageMaxThreadBytes caps a thread's total inlined body bytes (~200 KB). A
// var so tests can lower it.
var sitePageMaxThreadBytes = 200 * 1024

// siteSitemapPartSize bounds one sitemap file's URL count (the protocol caps a
// sitemap at 50K URLs; ~40K leaves headroom). A positive
// GITSOCIAL_SITE_SITEMAP_PART overrides it so tests exercise index mode without
// generating 40K pages.
var siteSitemapPartSize = siteSitemapPartSizeFromEnv()

// siteSitemapPartSizeFromEnv returns the sitemap part size, honoring a positive
// GITSOCIAL_SITE_SITEMAP_PART override, else the 40000 default.
func siteSitemapPartSizeFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_SITEMAP_PART"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 40000
}

// sitePagesInlineCSS is the tiny per-page base layer (body width/margins, font
// stack, light+dark palette, link color) inlined into every page so a saved or
// curl'ed copy reads decently and a failed pages.css fetch degrades gracefully.
// Kept deliberately tiny: changing it means a pagesVersion bump and full regen,
// while a pages.css change is one PUT.
const sitePagesInlineCSS = `body{max-width:44rem;margin:0 auto;padding:1.5rem 1rem 3rem;background:#f8eed5;color:#1a1a1a;font:19px/1.55 Georgia,serif}a{color:#008787}@media (prefers-color-scheme:dark){body{background:#02041b;color:#c9d1d9}a{color:#00d7d7}}`

// sitePagesCSS is the full look (chips, sections, thread styling, lists, nav,
// pre), served once as pages.css and shared by every page.
const sitePagesCSS = `h1{font-size:1.45rem;line-height:1.25;margin:.3rem 0 .2rem}
nav,.meta,.chip,pre,code,footer{font-family:'SF Mono',Consolas,monospace}
nav,footer{font-size:.72rem}
nav a,nav b,footer a{margin-right:.9rem}
.meta{font-size:.75rem;color:#6f6552}
.chip{font-size:.7rem;padding:0 .35rem;border:1px solid;border-radius:9px;white-space:nowrap}
.open{color:#1f9d55}
.closed{color:#8957e5}
.merged{color:#8250df}
.prerel{color:#bf8700}
.code,.draft{color:#6f6552}
.approve{color:#1f9d55}
.changes{color:#cf222e}
.tomb{color:#6f6552;font-style:italic}
section{border-top:1px solid #d8cbaa;margin-top:1.4rem;padding-top:.9rem}
p{margin:.7rem 0}
pre{font-size:.72rem;line-height:1.45;overflow-x:auto;background:#f2e5c6;padding:.7rem;border:1px solid #d8cbaa}
ol.items{list-style:none;padding:0;margin:1rem 0}
ol.items li{border-top:1px solid #d8cbaa;padding:.55rem 0}
footer{margin-top:2.2rem;border-top:1px solid #d8cbaa;padding-top:.8rem}
@media (prefers-color-scheme:dark){.meta,.code,.draft,.tomb{color:#7d8590}pre{background:#0a0d26;border-color:#1e2445}section,ol.items li,footer{border-color:#1e2445}}
`

// sitePageTemplateText is the full template set. The shared "head" stamps the
// common metadata plus the PE hooks (gs-route meta, the #gs-page mount div with
// its data-base attribute) — inert in v1, adopted by the M7 upgrade boot. The
// @BASE@ placeholder is spliced with the inline CSS constant before parsing so
// both layers emit from this file's constants.
const sitePageTemplateText = `{{define "head"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' https: data: blob:; font-src 'self'; connect-src 'self' https:; object-src 'none'; base-uri 'none'; form-action 'none'">
<title>{{.Title}}</title>
<meta name="description" content="{{.Description}}">
<link rel="canonical" href="{{.Canonical}}">
<meta property="og:title" content="{{.OGTitle}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:site_name" content="{{.SiteTitle}}">
<meta property="og:url" content="{{.Canonical}}">
<meta name="twitter:card" content="summary">
<link rel="alternate" type="application/atom+xml" title="{{.SiteTitle}}" href="{{.Feed}}">
{{if .TypeFeed}}<link rel="alternate" type="application/atom+xml" title="{{.TypeFeedTitle}}" href="{{.TypeFeed}}">
{{end}}<meta name="gs-route" content="{{.Route}}">
<style>@BASE@</style>
<link rel="stylesheet" href="{{.Base}}pages.css">
<script defer src="{{.Base}}gs-upgrade.js"></script>
</head>
<body>
<div id="gs-page" data-base="{{.Base}}">
{{end}}{{define "foot"}}</div>
</body>
</html>
{{end}}{{define "metaline"}}<p class="meta">{{if .Chip}}<span class="chip {{.Chip.Class}}">{{.Chip.Label}}</span> {{end}}{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</p>{{end}}{{define "paras"}}{{range .}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>
{{end}}{{end}}{{define "entries"}}<ol class="items">
{{range .}}<li>{{if .Chip}}<span class="chip {{.Chip.Class}}">{{.Chip.Label}}</span> {{end}}<a href="{{.Href}}">{{.Title}}</a><br>
<span class="meta">{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</span></li>
{{end}}</ol>
{{end}}{{define "item"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a> <a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a></nav>

<h1>{{.Subject}}</h1>
{{template "metaline" .}}
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "paras" .Paras}}{{end}}{{range .Sections}}<section>
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "metaline" .}}
{{if .Pre}}<pre>{{.Pre}}</pre>
{{end}}{{template "paras" .Paras}}{{end}}</section>
{{end}}{{if .Omitted}}<section><p class="meta">{{.Omitted}} more replies in the thread</p></section>
{{end}}<footer><a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "list"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a> <a href="{{.Chrome.Base}}index.html">home</a>{{range .Nav}} {{if .Current}}<b>{{.Label}}</b>{{else}}<a href="{{.Href}}">{{.Label}}</a>{{end}}{{end}}</nav>

<h1>{{.Heading}}</h1>
<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="meta">nothing here yet</p>
{{end}}<footer>{{if .NewerHref}}<a href="{{.NewerHref}}">← newer</a> {{end}}{{if .OlderHref}}<a href="{{.OlderHref}}">older →</a> {{end}}<a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "front"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a>{{range .Nav}} <a href="{{.Href}}">{{.Label}}</a>{{end}}</nav>

<h1>{{.Heading}}</h1>
<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="meta">nothing here yet</p>
{{end}}{{if .Readme}}<section><p class="meta">README</p>
{{template "paras" .Readme.Paras}}{{if .Readme.Truncated}}<p class="meta">… truncated — full README in the repository</p>
{{end}}</section>
{{end}}<footer>{{range .Nav}}<a href="{{.Href}}">{{.Label}}</a> {{end}}</footer>
{{template "foot"}}{{end}}`

// sitePageTemplates is the parsed page template set, with the inline base CSS
// spliced in (a constant, never user content).
var sitePageTemplates = template.Must(template.New("pages").Parse(strings.ReplaceAll(sitePageTemplateText, "@BASE@", sitePagesInlineCSS)))

// sitePageChrome is the shared head/shell data every page stamps.
type sitePageChrome struct {
	Title         string // full <title> (subject · site title)
	Description   string // meta/OG description, whitespace-collapsed, ~160 chars
	OGTitle       string // og:title (the bare subject)
	SiteTitle     string
	Canonical     string // absolute self URL from site.url
	Route         string // gs-route content, in the shell's parseRoute grammar
	Base          string // relative path from this page to the site root ("./" or "../")
	Feed          string // absolute feed.xml URL for the autodiscovery link (a relative href breaks after gs-upgrade.js hash-rewrites the location)
	TypeFeed      string // absolute <dir>/feed.xml URL — a second autodiscovery link on a type's list pages ("" elsewhere)
	TypeFeedTitle string // the type feed link's distinct display title ("<label> · <site title>")
}

// sitePageChip is one state/type chip.
type sitePageChip struct{ Class, Label string }

// sitePageSection is one thread section on an item page: a reply, a tombstone
// line, or the release artifacts block.
type sitePageSection struct {
	Chip  *sitePageChip
	Meta  []string
	Paras [][]string
	Pre   string
	Tomb  string
}

// siteItemPageData feeds the "item" template.
type siteItemPageData struct {
	Chrome    sitePageChrome
	ListDir   string
	ListLabel string
	Subject   string
	Chip      *sitePageChip
	Meta      []string
	Paras     [][]string
	Tomb      string
	Sections  []sitePageSection
	Omitted   int
}

// sitePageNavLink is one type-list nav entry (Current renders bold, unlinked).
type sitePageNavLink struct {
	Href    string
	Label   string
	Current bool
}

// sitePageListEntry is one row on a list or front page.
type sitePageListEntry struct {
	Chip  *sitePageChip
	Href  string
	Title string
	Meta  []string
}

// siteListPageData feeds the "list" and "front" templates.
type siteListPageData struct {
	Chrome    sitePageChrome
	Nav       []sitePageNavLink
	Heading   string
	MetaBits  []string
	Entries   []sitePageListEntry
	NewerHref string
	OlderHref string
	Readme    *siteFrontReadme // front page only
}

// siteFrontReadme is the front page's README section: the default branch's
// README.md as escaped-text paragraphs, capped with a truncation marker.
type siteFrontReadme struct {
	Paras     [][]string
	Truncated bool
}

// sitePageSite is the resolved site identity every page stamps.
type sitePageSite struct {
	Title       string
	URL         string // normalized site.url (trailing slash)
	Description string
}

// sitePageList describes one type directory: source extension, bucket dir,
// display label, and the shell route its pages map to.
type sitePageList struct {
	Ext   string
	Dir   string
	Label string
	Route string
}

// sitePageLists orders the five type directories. Milestones and sprints fold
// into issues; the posts list routes to the shell's /timeline tab (the shell
// has no posts-only surface). Routes match gs-core.js parseRoute's INDEX_TABS.
var sitePageLists = []sitePageList{
	{Ext: "pm", Dir: "issues", Label: "issues", Route: "/issues"},
	{Ext: "review", Dir: "prs", Label: "prs", Route: "/prs"},
	{Ext: "social", Dir: "posts", Label: "posts", Route: "/timeline"},
	{Ext: "release", Dir: "releases", Label: "releases", Route: "/releases"},
	{Ext: "memo", Dir: "memos", Label: "memos", Route: "/memos"},
}

// renderSitePage executes one page template into bytes.
func renderSitePage(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := sitePageTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render %s page: %w", name, err)
	}
	return buf.Bytes(), nil
}

// sitePageAppURL builds the in-app hash URL on the front page (index.html) for a
// shell route — used by the front page's code-commit rows (code commits get no
// pages, so they deep-link into the app, which gs-upgrade.js boots with the hash
// winning over the page's own gs-route).
func sitePageAppURL(site sitePageSite, route string) string {
	return site.URL + "index.html#" + route
}

// sitePageDate formats a unix timestamp as the pages' date form (UTC).
func sitePageDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

// sitePageParas splits escaped-text content into paragraphs of lines
// (\n\n → <p>, \n → <br> in the template).
func sitePageParas(text string) [][]string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r", ""))
	if text == "" {
		return nil
	}
	var paras [][]string
	for _, block := range strings.Split(text, "\n\n") {
		block = strings.Trim(block, "\n")
		if block == "" {
			continue
		}
		paras = append(paras, strings.Split(block, "\n"))
	}
	return paras
}

// sitePageDescription extracts a meta/OG description: the text (falling back
// to the subject) whitespace-collapsed and truncated to ~160 chars.
func sitePageDescription(text, fallback string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if collapsed == "" {
		collapsed = strings.Join(strings.Fields(fallback), " ")
	}
	runes := []rune(collapsed)
	if len(runes) > sitePageDescriptionLen {
		return strings.TrimSpace(string(runes[:sitePageDescriptionLen])) + "…"
	}
	return collapsed
}

// sitePageTypeLabel maps an item type to its display label.
func sitePageTypeLabel(t string) string {
	if t == "pull-request" {
		return "pull request"
	}
	return t
}

// sitePageStateClass maps a workflow state to its chip color class.
func sitePageStateClass(state string) string {
	switch state {
	case "closed", "completed", "canceled":
		return "closed"
	case "merged":
		return "merged"
	default:
		return "open"
	}
}

// sitePageItemChip returns an item's leading state chip (nil when its type
// carries none), matching the prototype chip vocabulary.
func sitePageItemChip(it *sitePageItem) *sitePageChip {
	if it.Retracted {
		return &sitePageChip{Class: "code", Label: "retracted"}
	}
	switch pageItemType(it) {
	case "issue", "milestone", "sprint":
		state := pageItemField(it, "state")
		if state == "" {
			state = "open"
		}
		return &sitePageChip{Class: sitePageStateClass(state), Label: state}
	case "pull-request":
		if pageItemField(it, "draft") == "true" {
			return &sitePageChip{Class: "draft", Label: "draft"}
		}
		state := pageItemField(it, "state")
		if state == "" {
			state = "open"
		}
		return &sitePageChip{Class: sitePageStateClass(state), Label: state}
	case "release":
		if pageItemField(it, "prerelease") == "true" {
			return &sitePageChip{Class: "prerel", Label: "pre-release"}
		}
	}
	return nil
}

// sitePageAuthorBit formats a message's author meta bit ("name <email>").
func sitePageAuthorBit(m *sitePageMsg) string {
	name, email := pageDisplayAuthor(m)
	if email != "" {
		if name == "" {
			return "<" + email + ">"
		}
		return name + " <" + email + ">"
	}
	return name
}

// sitePageBaseHead formats a PR's "head → base" branch pair from its header
// refs (cross-fork sides keep their repository prefix).
func sitePageBaseHead(it *sitePageItem) string {
	format := func(ref string) string {
		if ref == "" {
			return ""
		}
		p := protocol.ParseRef(ref)
		if p.Repository != "" {
			return p.Repository + "#" + p.Value
		}
		return p.Value
	}
	base, head := format(pageItemField(it, "base")), format(pageItemField(it, "head"))
	if base == "" && head == "" {
		return ""
	}
	return head + " → " + base
}

// siteItemPageMeta builds an item page's meta-line bits (type, type extras,
// author, date, markers, and the item's short ref).
func siteItemPageMeta(it *sitePageItem) []string {
	t := pageItemType(it)
	bits := []string{sitePageTypeLabel(t)}
	switch t {
	case "pull-request":
		if bh := sitePageBaseHead(it); bh != "" {
			bits = append(bits, bh)
		}
	case "release":
		if tag := pageItemField(it, "tag"); tag != "" {
			bits = append(bits, "tag "+tag)
		}
	case "milestone":
		if due := pageItemField(it, "due"); due != "" {
			bits = append(bits, "due "+due)
		}
	case "sprint":
		if start, end := pageItemField(it, "start"), pageItemField(it, "end"); start != "" || end != "" {
			bits = append(bits, start+" → "+end)
		}
	}
	bits = append(bits, sitePageAuthorBit(it.Msg), sitePageDate(pageEffectiveTime(it.Msg)))
	if t == "release" && pageItemField(it, "signed-by") != "" {
		bits = append(bits, "signed")
	}
	if it.Edited && !it.Retracted {
		bits = append(bits, "edited")
	}
	return append(bits, "#commit:"+it.Msg.Short)
}

// sitePageFeedbackChip returns a feedback reply's review-state chip (nil for a
// plain comment).
func sitePageFeedbackChip(r *sitePageItem) *sitePageChip {
	switch pageItemField(r, "review-state") {
	case "approved":
		return &sitePageChip{Class: "approve", Label: "approved"}
	case "changes-requested":
		return &sitePageChip{Class: "changes", Label: "changes requested"}
	}
	return nil
}

// sitePageFeedbackAnchor formats a line-anchored feedback's "file:line" bit
// ("" when the feedback is not file-anchored).
func sitePageFeedbackAnchor(r *sitePageItem) string {
	file := pageItemField(r, "file")
	if file == "" {
		return ""
	}
	line, end := pageItemField(r, "new-line"), pageItemField(r, "new-line-end")
	if line == "" {
		line, end = pageItemField(r, "old-line"), pageItemField(r, "old-line-end")
	}
	if line == "" {
		return file
	}
	if end != "" && end != line {
		return file + ":" + line + "-" + end
	}
	return file + ":" + line
}

// buildSiteReplySection renders one thread reply into its section data (a
// tombstone line when the reply was retracted).
func buildSiteReplySection(r *sitePageItem) sitePageSection {
	if r.Retracted {
		return sitePageSection{Tomb: "a reply from " + sitePageDate(pageEffectiveTime(r.Msg)) + " was retracted by its author"}
	}
	s := sitePageSection{Meta: []string{sitePageAuthorBit(r.Msg), sitePageDate(pageEffectiveTime(r.Msg))}}
	if pageMsgType(r.Msg) == "feedback" {
		s.Chip = sitePageFeedbackChip(r)
		if anchor := sitePageFeedbackAnchor(r); anchor != "" {
			s.Meta = append(s.Meta, anchor)
		}
		if pageItemField(r, "suggestion") == "true" {
			s.Meta = append(s.Meta, "suggestion")
		}
	} else if r.InReplyTo != "" {
		s.Meta = append(s.Meta, "reply to "+r.InReplyTo)
	}
	if r.Edited {
		s.Meta = append(s.Meta, "edited")
	}
	s.Paras = sitePageParas(pageItemBody(r))
	return s
}

// buildSiteReleaseArtifacts returns a release page's artifact/checksum block
// (nil when the release ships none).
func buildSiteReleaseArtifacts(it *sitePageItem) *sitePageSection {
	var lines []string
	for _, a := range strings.Split(pageItemField(it, "artifacts"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			lines = append(lines, a)
		}
	}
	if c := pageItemField(it, "checksums"); c != "" {
		lines = append(lines, c)
	}
	if s := pageItemField(it, "sbom"); s != "" {
		lines = append(lines, s)
	}
	if len(lines) == 0 {
		return nil
	}
	meta := []string{"artifacts"}
	if u := pageItemField(it, "artifact-url"); u != "" {
		meta = append(meta, u)
	}
	return &sitePageSection{Meta: meta, Pre: strings.Join(lines, "\n")}
}

// buildSiteItemPage assembles one root's full item-page data: chrome, meta
// line, escaped-text body (or tombstone), release extras, and the thread
// sections in timestamp order up to the reply/byte cap.
func buildSiteItemPage(it *sitePageItem, list sitePageList, site sitePageSite) siteItemPageData {
	route := "commit:" + it.Msg.Short + "@gitmsg/" + list.Ext
	subject, body := protocol.SplitSubjectBody(pageItemBody(it))
	d := siteItemPageData{
		ListDir:   list.Dir,
		ListLabel: list.Label,
		Subject:   subject,
		Chip:      sitePageItemChip(it),
		Meta:      siteItemPageMeta(it),
	}
	if it.Retracted {
		label := sitePageTypeLabel(pageItemType(it))
		d.Subject = "retracted " + label
		d.Tomb = "this " + label + " was retracted by its author"
		body = ""
	} else {
		d.Paras = sitePageParas(body)
	}
	d.Chrome = sitePageChrome{
		Title:       d.Subject + " · " + site.Title,
		Description: sitePageDescription(body, d.Subject),
		OGTitle:     d.Subject,
		SiteTitle:   site.Title,
		Canonical:   site.URL + "i/" + it.Msg.Short + ".html",
		Route:       route,
		Base:        "../",
		Feed:        site.URL + sitePagesFeedKey,
	}
	if pageItemType(it) == "release" {
		if s := buildSiteReleaseArtifacts(it); s != nil {
			d.Sections = append(d.Sections, *s)
		}
	}
	threadBytes := 0
	for i, r := range it.Replies {
		if i >= sitePageMaxReplies || threadBytes > sitePageMaxThreadBytes {
			d.Omitted = len(it.Replies) - i
			break
		}
		d.Sections = append(d.Sections, buildSiteReplySection(r))
		threadBytes += len(pageItemBody(r))
	}
	return d
}

// buildSiteListEntry renders one root as a list/front row. base is the page's
// relative path to the site root; defaultType suppresses the redundant type
// bit on a type's own list (an issue row on the issues list).
func buildSiteListEntry(it *sitePageItem, base, defaultType string) sitePageListEntry {
	t := pageItemType(it)
	subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
	name, _ := pageDisplayAuthor(it.Msg)
	var meta []string
	if t != defaultType {
		meta = append(meta, sitePageTypeLabel(t))
	}
	meta = append(meta, name, sitePageDate(pageEffectiveTime(it.Msg)))
	if n := len(it.Replies); n == 1 {
		meta = append(meta, "1 comment")
	} else if n > 0 || t == "issue" || t == "pull-request" {
		meta = append(meta, fmt.Sprintf("%d comments", n))
	}
	return sitePageListEntry{
		Chip:  sitePageItemChip(it),
		Href:  base + "i/" + it.Msg.Short + ".html",
		Title: subject,
		Meta:  meta,
	}
}

// buildSiteCodeEntry renders one code commit as a front-page row linking into
// the app (code commits get no pages; object count must not scale with
// history).
func buildSiteCodeEntry(e siteMetaEntry, site sitePageSite) sitePageListEntry {
	short := e.SHA
	if len(short) > 12 {
		short = short[:12]
	}
	meta := []string{short[:min(8, len(short))]}
	if e.Branch != "" {
		meta = append(meta, e.Branch)
	}
	meta = append(meta, e.Author, sitePageDate(e.TS))
	return sitePageListEntry{
		Chip:  &sitePageChip{Class: "code", Label: "commit"},
		Href:  sitePageAppURL(site, "commit:"+short+"@"+e.Branch),
		Title: e.Subject,
		Meta:  meta,
	}
}

// sitePageNav builds the type-list nav links for a list/front page. base is
// the page's relative path to the site root; current bolds that list's link.
func sitePageNav(base, current string) []sitePageNavLink {
	nav := make([]sitePageNavLink, 0, len(sitePageLists))
	for _, l := range sitePageLists {
		nav = append(nav, sitePageNavLink{Href: base + l.Dir + "/index.html", Label: l.Label, Current: l.Dir == current})
	}
	return nav
}

// siteXMLEscaper escapes text/attribute content for the sitemap XML (locs are
// derived from site.url, whose path may carry XML-special characters).
var siteXMLEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// siteSitemapEntry is one sitemap URL: its absolute location, its last activity
// (W3C date), and the creation sort key that keeps part membership stable.
type siteSitemapEntry struct {
	loc     string
	lastmod string
	ts      int64
	sha     string
}

// buildSiteSitemapEntries collects the sitemap URL set: the site root first,
// then every generated item page with <lastmod> = the item's latest activity
// (root, resolved edit, or newest reply). Item entries sort ascending by
// creation (time, sha) — creation never changes, so appends land at the tail
// and sealed part membership stays stable.
func buildSiteSitemapEntries(roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) []siteSitemapEntry {
	var items []siteSitemapEntry
	var newest int64
	for _, list := range sitePageLists {
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			last := sitePageLastActivity(it)
			if last > newest {
				newest = last
			}
			items = append(items, siteSitemapEntry{
				loc:     site.URL + "i/" + it.Msg.Short + ".html",
				lastmod: sitePageDate(last),
				ts:      pageEffectiveTime(it.Msg),
				sha:     it.Msg.SHA,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ts != items[j].ts {
			return items[i].ts < items[j].ts
		}
		return items[i].sha < items[j].sha
	})
	return append([]siteSitemapEntry{{loc: site.URL, lastmod: sitePageDate(newest)}}, items...)
}

// sitePageLastActivity returns an item's latest activity time: its creation,
// its resolved edit, or its newest reply.
func sitePageLastActivity(it *sitePageItem) int64 {
	last := pageEffectiveTime(it.Msg)
	if it.Edited && it.Resolved.TS > last {
		last = it.Resolved.TS
	}
	for _, r := range it.Replies {
		if t := pageEffectiveTime(r.Msg); t > last {
			last = t
		}
	}
	return last
}

// renderSiteURLSet renders one <urlset> sitemap document.
func renderSiteURLSet(entries []siteSitemapEntry) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, e := range entries {
		b.WriteString("<url><loc>" + siteXMLEscaper.Replace(e.loc) + "</loc>")
		if e.lastmod != "" {
			b.WriteString("<lastmod>" + e.lastmod + "</lastmod>")
		}
		b.WriteString("</url>\n")
	}
	b.WriteString("</urlset>\n")
	return []byte(b.String())
}

// renderSiteSitemapIndex renders the <sitemapindex> document over the parts.
func renderSiteSitemapIndex(parts []siteSitemapEntry) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, p := range parts {
		b.WriteString("<sitemap><loc>" + siteXMLEscaper.Replace(p.loc) + "</loc>")
		if p.lastmod != "" {
			b.WriteString("<lastmod>" + p.lastmod + "</lastmod>")
		}
		b.WriteString("</sitemap>\n")
	}
	b.WriteString("</sitemapindex>\n")
	return []byte(b.String())
}

// writeSiteSitemap writes the crawl map for the generated pages: a single
// sitemap.xml until the URL count exceeds one part, then a sitemap index over
// sealed numbered parts (full, immutable, long-cached — appends only ever grow
// the tail) plus the rewritten sitemap-head.xml newest part.
func writeSiteSitemap(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) error {
	entries := buildSiteSitemapEntries(roots, done, site)
	if len(entries) <= siteSitemapPartSize {
		return putSiteText(client, prefix+sitePagesSitemapKey, "application/xml", renderSiteURLSet(entries))
	}
	sealed := (len(entries) - 1) / siteSitemapPartSize
	index := make([]siteSitemapEntry, 0, sealed+1)
	for n := 1; n <= sealed; n++ {
		part := entries[(n-1)*siteSitemapPartSize : n*siteSitemapPartSize]
		key := fmt.Sprintf("sitemap-%d.xml", n)
		if err := putSiteText(client, prefix+key, "application/xml", renderSiteURLSet(part)); err != nil {
			return err
		}
		index = append(index, siteSitemapEntry{loc: site.URL + key, lastmod: newestLastmod(part)})
	}
	head := entries[sealed*siteSitemapPartSize:]
	if err := putSiteText(client, prefix+sitePagesSitemapHeadKey, "application/xml", renderSiteURLSet(head)); err != nil {
		return err
	}
	index = append(index, siteSitemapEntry{loc: site.URL + sitePagesSitemapHeadKey, lastmod: newestLastmod(head)})
	return putSiteText(client, prefix+sitePagesSitemapKey, "application/xml", renderSiteSitemapIndex(index))
}

// newestLastmod returns a part's newest lastmod (W3C dates compare lexically).
func newestLastmod(entries []siteSitemapEntry) string {
	newest := ""
	for _, e := range entries {
		if e.lastmod > newest {
			newest = e.lastmod
		}
	}
	return newest
}

// writeSiteRobots writes robots.txt: allow everything and point at the sitemap.
// Deliberately no Disallow for .gitsocial/ — a crawler's renderer needs the
// shards for the SPA surfaces, and loose objects are never linked.
func writeSiteRobots(client *Client, prefix string, site sitePageSite) error {
	body := "User-agent: *\nAllow: /\nSitemap: " + site.URL + "sitemap.xml\n"
	return putSiteText(client, prefix+sitePagesRobotsKey, "text/plain; charset=utf-8", []byte(body))
}

// siteFeedEntry is one Atom entry projected from a top-level item: its title,
// canonical page URL (also its stable <id>), latest activity and creation
// times, display author, item-type category term, and body HTML.
type siteFeedEntry struct {
	title     string
	href      string
	updated   int64
	published int64
	author    string
	term      string
	content   string // escaped <p>/<br> HTML of the item's own body ("" = no content element)
}

// selectSiteFeedItems picks one feed's item set: the newest sitePagesFrontSize
// non-retracted top-level items of the given extensions (code commits absent —
// they have no item page to link), newest-first by (effective time, sha)
// exactly like the front page's interleave. Selection runs before body
// attachment so bodies are never fetched for items the cap drops.
func selectSiteFeedItems(roots map[string][]*sitePageItem, done map[string]int, exts []string) []*sitePageItem {
	var items []*sitePageItem
	for _, ext := range exts {
		for _, it := range roots[ext][:done[ext]] {
			if !it.Retracted {
				items = append(items, it)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		ti, tj := pageEffectiveTime(items[i].Msg), pageEffectiveTime(items[j].Msg)
		if ti != tj {
			return ti > tj
		}
		return items[i].Msg.SHA > items[j].Msg.SHA
	})
	if len(items) > sitePagesFrontSize {
		items = items[:sitePagesFrontSize]
	}
	return items
}

// siteFeedContentHTML renders an item's own body (subject stripped, replies
// excluded) as escaped <p>/<br> HTML for the entry's content element, capped
// at siteFeedBodyMax with a truncation marker paragraph; "" when the item has
// no body beyond its subject.
func siteFeedContentHTML(it *sitePageItem) string {
	_, body := protocol.SplitSubjectBody(pageItemBody(it))
	truncated := false
	if len(body) > siteFeedBodyMax {
		body, truncated = strings.ToValidUTF8(body[:siteFeedBodyMax], ""), true
	}
	paras := sitePageParas(body)
	if paras == nil {
		return ""
	}
	var b strings.Builder
	for _, para := range paras {
		b.WriteString("<p>")
		for i, line := range para {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(siteXMLEscaper.Replace(line))
		}
		b.WriteString("</p>")
	}
	if truncated {
		b.WriteString("<p>… truncated</p>")
	}
	return b.String()
}

// buildSiteFeedEntries projects the selected (body-attached) items into Atom
// entries.
func buildSiteFeedEntries(items []*sitePageItem, site sitePageSite) []siteFeedEntry {
	entries := make([]siteFeedEntry, 0, len(items))
	for _, it := range items {
		subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
		if subject == "" {
			subject = sitePageTypeLabel(pageItemType(it))
		}
		name, _ := pageDisplayAuthor(it.Msg)
		entries = append(entries, siteFeedEntry{
			title:     subject,
			href:      site.URL + "i/" + it.Msg.Short + ".html",
			updated:   sitePageLastActivity(it),
			published: pageEffectiveTime(it.Msg),
			author:    name,
			term:      pageItemType(it),
			content:   siteFeedContentHTML(it),
		})
	}
	return entries
}

// siteFeedHead is one feed document's identity block: the main feed carries
// the site's, a type feed its list's.
type siteFeedHead struct {
	id       string
	title    string
	subtitle string // omitted when empty
	self     string
	alt      string
}

// renderSiteFeed renders one Atom 1.0 feed document. The feed's <updated> is the
// newest entry's activity (epoch when there are no entries — deterministic, never
// wall clock); every text/attribute value is XML-escaped.
func renderSiteFeed(entries []siteFeedEntry, head siteFeedHead) []byte {
	esc := siteXMLEscaper.Replace
	rfc3339 := func(ts int64) string { return time.Unix(ts, 0).UTC().Format(time.RFC3339) }
	var newest int64
	for _, e := range entries {
		if e.updated > newest {
			newest = e.updated
		}
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<feed xmlns=\"http://www.w3.org/2005/Atom\">\n")
	b.WriteString("<id>" + esc(head.id) + "</id>\n")
	b.WriteString("<title>" + esc(head.title) + "</title>\n")
	if head.subtitle != "" {
		b.WriteString("<subtitle>" + esc(head.subtitle) + "</subtitle>\n")
	}
	b.WriteString("<updated>" + rfc3339(newest) + "</updated>\n")
	b.WriteString("<link rel=\"self\" href=\"" + esc(head.self) + "\"/>\n")
	b.WriteString("<link rel=\"alternate\" href=\"" + esc(head.alt) + "\"/>\n")
	for _, e := range entries {
		b.WriteString("<entry>\n")
		b.WriteString("<title>" + esc(e.title) + "</title>\n")
		b.WriteString("<id>" + esc(e.href) + "</id>\n")
		b.WriteString("<link rel=\"alternate\" href=\"" + esc(e.href) + "\"/>\n")
		b.WriteString("<updated>" + rfc3339(e.updated) + "</updated>\n")
		b.WriteString("<published>" + rfc3339(e.published) + "</published>\n")
		b.WriteString("<author><name>" + esc(e.author) + "</name></author>\n")
		b.WriteString("<category term=\"" + esc(e.term) + "\"/>\n")
		if e.content != "" {
			// Escaped HTML inside escaped XML: the content is HTML markup whose
			// text was already escaped, XML-escaped again as element chardata.
			b.WriteString("<content type=\"html\">" + esc(e.content) + "</content>\n")
		}
		b.WriteString("</entry>\n")
	}
	b.WriteString("</feed>\n")
	return []byte(b.String())
}

// putSiteFeed fetches the selected items' missing root bodies (a no-op on the
// full-regen path where the corpus is already loaded, a few head-first GETs on
// the metadata-only incremental path) and uploads one rendered feed document
// (uncompressed, like the sitemap and robots).
func putSiteFeed(client *Client, prefix, key string, items []*sitePageItem, head siteFeedHead, site sitePageSite) error {
	if err := attachRootBodies(client, prefix, items); err != nil {
		return fmt.Errorf("feed bodies %s: %w", key, err)
	}
	return putSiteText(client, prefix+key, "application/atom+xml; charset=utf-8", renderSiteFeed(buildSiteFeedEntries(items, site), head))
}

// writeSiteFeed writes the main Atom feed: the front page's item interleave
// (memo excluded), identified as the site itself.
func writeSiteFeed(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) error {
	exts := make([]string, 0, len(sitePageLists))
	for _, list := range sitePageLists {
		if list.Ext != "memo" {
			exts = append(exts, list.Ext)
		}
	}
	head := siteFeedHead{id: site.URL, title: site.Title, subtitle: site.Description, self: site.URL + sitePagesFeedKey, alt: site.URL}
	return putSiteFeed(client, prefix, sitePagesFeedKey, selectSiteFeedItems(roots, done, exts), head, site)
}

// siteTypeFeedKey is a type directory's feed bucket key.
func siteTypeFeedKey(list sitePageList) string {
	return list.Dir + "/" + sitePagesFeedKey
}

// siteTypeFeedTitle words a type feed's display title, distinct from the main
// feed's so reader pickers tell them apart.
func siteTypeFeedTitle(list sitePageList, site sitePageSite) string {
	return list.Label + " · " + site.Title
}

// writeSiteTypeFeeds writes the per-type Atom feeds: every type directory's
// feed mirrors its list page the way the main feed mirrors the front page
// (memos included here — only the main feed's interleave excludes them). dirs
// (nil = every dir) limits the incremental pass to the type directories whose
// entries changed, matching writeSiteTypeLists' gating.
func writeSiteTypeFeeds(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, dirs map[string]bool) error {
	for _, list := range sitePageLists {
		if dirs != nil && !dirs[list.Dir] {
			continue
		}
		head := siteFeedHead{
			id:    site.URL + siteTypeFeedKey(list),
			title: siteTypeFeedTitle(list, site),
			self:  site.URL + siteTypeFeedKey(list),
			alt:   site.URL + list.Dir + "/index.html",
		}
		if err := putSiteFeed(client, prefix, siteTypeFeedKey(list), selectSiteFeedItems(roots, done, []string{list.Ext}), head, site); err != nil {
			return err
		}
	}
	return nil
}
