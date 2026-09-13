// site_pages_html.go - page templates, the inlined CSS base and the presentation builders

package site

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

const (
	// sitePagesLegacyCSSKey is the retired generated stylesheet, kept for the disable sweep.
	sitePagesLegacyCSSKey = "pages.css"
	// sitePagesFrontKey is the front page's bucket key; the page layer owns it whenever it is effective.
	sitePagesFrontKey = "index.html"
	// sitePagesLegacyFrontKey is the pre-flip front-page key, swept on every push.
	sitePagesLegacyFrontKey = "timeline.html"
	// sitePagesUpgradeKey is the page-entry boot asset every generated page defers.
	sitePagesUpgradeKey = "gs-upgrade.js"
	// sitePagesSitemapKey is the sitemap entry point: one urlset, or an index over the parts.
	sitePagesSitemapKey = "sitemap.xml"
	// sitePagesSitemapHeadKey is index mode's mutable newest part.
	sitePagesSitemapHeadKey = "sitemap-head.xml"
	// sitePagesRobotsKey is the crawler policy file.
	sitePagesRobotsKey = "robots.txt"
	// sitePagesFeedKey is the Atom 1.0 feed of the newest top-level items.
	sitePagesFeedKey = "feed.xml"
	// siteFeedBodyMax caps one feed entry's raw body bytes before paragraph rendering.
	siteFeedBodyMax = 4 * 1024
	// sitePageDescriptionLen bounds the meta/OG description (~160 chars).
	sitePageDescriptionLen = 160
	// sitePageRobotsNoIndex is the meta robots value a tombstone carries.
	sitePageRobotsNoIndex = "noindex,follow"
)

// sitePageMaxReplies caps a thread's inlined replies; the rest truncate into a marker. A var so tests can lower it.
var sitePageMaxReplies = 100

// sitePageMaxThreadBytes caps a thread's total inlined body bytes. A var so tests can lower it.
var sitePageMaxThreadBytes = 200 * 1024

// siteSitemapPartSize bounds one sitemap file's URL count.
var siteSitemapPartSize = siteSitemapPartSizeFromEnv()

// siteSitemapPartSizeFromEnv returns the sitemap part size, honoring GITSOCIAL_SITE_SITEMAP_PART.
func siteSitemapPartSizeFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_SITEMAP_PART"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 40000
}

// sitePagesCoreCSS is the embedded site/pages-core.css, comments stripped, inlined into every page's head.
var sitePagesCoreCSS = sitePagesReadCoreCSS()

// sitePagesReadCoreCSS reads the embedded site/pages-core.css with its comments stripped; a broken embed panics at init.
func sitePagesReadCoreCSS() string {
	data, err := siteFiles.ReadFile("assets/pages-core.css")
	if err != nil {
		panic("site: read embedded assets/pages-core.css for the pages' inlined base: " + err.Error())
	}
	return strings.TrimSpace(sitePagesStripCSSComments(string(data)))
}

// sitePagesStripCSSComments removes /* … */ spans so the inlined copy carries declarations only.
func sitePagesStripCSSComments(css string) string {
	var b strings.Builder
	for {
		i := strings.Index(css, "/*")
		if i < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:i])
		j := strings.Index(css[i+2:], "*/")
		if j < 0 {
			return b.String()
		}
		css = css[i+2+j+2:]
	}
}

// sitePagesBootScript is the inline head script every page carries: it stamps the stored theme and cloaks a page the app is about to replace.
const sitePagesBootScript = `<script>(function(d,w){var e=d.documentElement;` +
	`try{var t=w.localStorage.getItem("theme");if(t==="dark-mode"||t==="light-mode")e.classList.add(t)}catch(x){}` +
	`var m=d.querySelector('meta[name="gs-route"]');var r=m?m.getAttribute("content"):"";` +
	`var h=w.location.hash||"";var deep=/^#\/.+/.test(h)||/^#[a-z][a-z-]*:/.test(h);` +
	`if(r!=="/"||deep)e.classList.add("gs-boot");` +
	`function u(){if(!w.__gsBooting)e.classList.remove("gs-boot")}` +
	`setTimeout(u,10000);w.addEventListener("load",u)})(document,window)</script>`

// sitePagesAccentCSS renders the per-push accent override stamped after the inlined core; readSiteCustomization has already reduced cfg to hex colors.
func sitePagesAccentCSS(cfg siteCustomization) template.CSS {
	light, dark := "", ""
	if cfg.Accent != "" {
		light, dark = cfg.Accent, cfg.Accent
	}
	if cfg.AccentDark != "" {
		dark = cfg.AccentDark
	}
	if light == "" && dark == "" {
		return ""
	}
	var decls []string
	if light != "" {
		decls = append(decls, "--pl-link:"+light)
	}
	if dark != "" {
		decls = append(decls, "--pd-link:"+dark)
	}
	return template.CSS(":root{" + strings.Join(decls, ";") + "}")
}

// sitePageTemplateText is the full template set; @CORE@ and @BOOT@ are spliced in before parsing.
const sitePageTemplateText = `{{define "head"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' https: data: blob:; media-src 'self' blob:; font-src 'self'; connect-src 'self' https:; object-src 'none'; base-uri 'none'; form-action 'none'">
<title>{{.Title}}</title>
{{if .Icon}}<link rel="icon" href="{{.Icon}}">
{{end}}<meta name="description" content="{{.Description}}">
<link rel="canonical" href="{{.Canonical}}">
{{if .Robots}}<meta name="robots" content="{{.Robots}}">
{{end}}<meta property="og:title" content="{{.OGTitle}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:site_name" content="{{.SiteTitle}}">
<meta property="og:url" content="{{.Canonical}}">
{{if .Image}}<meta property="og:image" content="{{.Image}}">
<meta name="twitter:image" content="{{.Image}}">
<meta name="twitter:card" content="summary_large_image">
{{else}}<meta name="twitter:card" content="summary">
{{end}}<link rel="alternate" type="application/atom+xml" title="{{.SiteTitle}}" href="{{.Feed}}">
{{if .TypeFeed}}<link rel="alternate" type="application/atom+xml" title="{{.TypeFeedTitle}}" href="{{.TypeFeed}}">
{{end}}<meta name="gs-route" content="{{.Route}}">
<style data-gs-core>@CORE@</style>
{{if .AccentCSS}}<style data-gs-core>{{.AccentCSS}}</style>
{{end}}@BOOT@
<link rel="preload" as="style" href="{{.Base}}pages-full.css" onload="this.onload=null;this.rel='stylesheet'">
<noscript><link rel="stylesheet" href="{{.Base}}pages-full.css"></noscript>
<script defer src="{{.Base}}gs-upgrade.js"></script>
</head>
<body>
<div id="gs-page" data-base="{{.Base}}">
{{end}}{{define "foot"}}</div>
</body>
</html>
{{end}}{{define "sidebar"}}<aside class="page-nav">
<div class="nav-header"><a class="repo-title" href="{{.Base}}index.html">{{.SiteTitle}}</a></div>
<nav class="nav-list">{{range .Nav}}{{if .Section}}<div class="nav-group"><div class="nav-section">{{.Section}}</div>{{end}}{{range .Links}}<a href="{{.Href}}"{{if .Current}} class="active"{{end}}><span class="nav-icon">{{.Glyph}}</span>{{.Label}}</a>{{end}}{{if .Section}}</div>{{end}}{{end}}</nav>
<div class="nav-footer"><a class="foot-brand" href="https://gitsocial.org"><svg class="logo-small" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg><span>Built with GitSocial</span></a></div>
</aside>
{{end}}{{define "chip"}}<span class="chip{{if .Class}} {{.Class}}{{end}}">{{.Label}}</span>{{end}}{{define "detailhead"}}<div class="card-head"><h1 class="subject">{{.Heading}}</h1>{{range .Chips}} {{template "chip" .}}{{end}}</div>{{end}}{{define "metaline"}}<p class="meta">{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</p>{{end}}{{define "paras"}}{{range .}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>
{{end}}{{end}}{{define "entries"}}{{range .}}<div class="card"{{if .ID}} id="{{.ID}}"{{end}}><div class="card-head">{{if .Glyph}}<span class="type-glyph {{.GlyphClass}}" title="{{.GlyphTitle}}">{{.Glyph}}</span> {{end}}{{if .Chip}}{{template "chip" .Chip}} {{end}}<a class="subject" href="{{.Href}}">{{.Title}}</a>{{range .TailChips}} {{template "chip" .}}{{end}}</div>
<span class="meta">{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</span></div>
{{end}}{{end}}{{define "item"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}{{template "detailhead" .}}
{{end}}<div class="detail-meta"><span class="meta">{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</span></div>
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "paras" .Paras}}{{end}}{{with .Artifacts}}<section>
{{template "metaline" .}}
{{if .Pre}}<pre>{{.Pre}}</pre>
{{end}}{{template "paras" .Paras}}</section>
{{end}}{{if .Replies}}<div class="thread"><div class="thread-head mono">Comments ({{len .Replies}})</div>
{{range .Replies}}{{if .Depth}}<div class="comment-row"><div class="thread-rail">{{range $i := .Rail}}<span class="rail-guide"></span>{{end}}</div>{{end}}<div class="card {{.Variant}}">
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}<p class="meta meta-lead">{{if .Glyph}}<span class="type-glyph {{.GlyphClass}}" title="{{.GlyphTitle}}">{{.Glyph}}</span> {{end}}{{range .Chips}}{{template "chip" .}} {{end}}{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{template "paras" .Paras}}{{end}}</div>{{if .Depth}}</div>{{end}}
{{end}}</div>
{{end}}{{if .Omitted}}<section><p class="meta">… truncated — {{.Omitted}} more replies in the thread</p></section>
{{end}}<footer><a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "list"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

<h1>{{.Heading}}</h1>
<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="meta">nothing here yet</p>
{{end}}<footer>{{if .NewerHref}}<a href="{{.NewerHref}}">← newer</a> {{end}}{{if .OlderHref}}<a href="{{.OlderHref}}">older →</a> {{end}}<a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "file"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}<h1>{{.Heading}}</h1>
{{end}}<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .HTML}}{{.HTML}}{{else}}<pre>{{.Pre}}</pre>
{{end}}{{if .Truncated}}<p class="meta">… truncated — full file in the repository</p>
{{end}}<footer><a href="{{.Chrome.Base}}f/index.html">← files</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "front"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Description}}<p class="meta">{{.Description}}</p>
{{end}}{{with .Home}}{{if .Branch}}<p class="meta"><span class="chip">{{.Branch}}</span> <a class="chip" href="{{.BranchesHref}}">{{.Branches}}</a>{{with .Latest}} {{.Subject}} · {{.Date}} · <a href="{{.Href}}">{{.Short}}</a>{{end}}</p>
{{end}}{{if .Files}}<ul class="files">
{{range .Files}}<li><a href="{{.Href}}">{{.Name}}</a></li>
{{end}}</ul>
{{if .MoreHref}}<p class="meta"><a href="{{.MoreHref}}">{{.MoreLabel}}</a></p>
{{end}}{{end}}{{if .Readme}}<section><p class="meta">README</p>
{{.Readme.HTML}}{{if .Readme.Truncated}}<p class="meta">… truncated — full README in the repository</p>
{{end}}</section>
{{end}}{{end}}{{if .Activity}}<div class="home-activity"><h2 class="home-activity-head">Recent activity</h2>
{{template "entries" .Activity}}{{if .ActivityMoreHref}}<a class="show-more" href="{{.ActivityMoreHref}}"><span class="show-more-icon"><span class="gs-icon chevron"><svg fill="none" viewBox="0 0 16 16" aria-hidden="true"><path stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" d="m3.5 6 4.5 4.5L12.5 6"/></svg></span></span><span class="show-more-label">{{.ActivityMoreLabel}}</span></a>
{{end}}</div>
{{end}}<footer>{{range .Chrome.Nav}}{{range .Links}}{{if not .Current}}<a href="{{.Href}}">{{.Label}}</a> {{end}}{{end}}{{end}}</footer>
{{template "foot"}}{{end}}`

// sitePageTemplates is the parsed page template set, with the core CSS and the boot script spliced in.
var sitePageTemplates = template.Must(template.New("pages").Parse(
	strings.NewReplacer("@CORE@", sitePagesCoreCSS, "@BOOT@", sitePagesBootScript).Replace(sitePageTemplateText)))

// sitePagesInlineIconMax bounds a configured favicon the page layer inlines; past it a page carries the shell default.
const sitePagesInlineIconMax = 2048

// sitePagesShellIconRe pulls the <link rel="icon"> href out of the shell.
var sitePagesShellIconRe = regexp.MustCompile(`<link rel="icon" href="([^"]*)"`)

// sitePagesDefaultIcon is the shell's own favicon href, a data: URI every page inlines.
var sitePagesDefaultIcon = sitePagesShellIcon()

// sitePagesShellIcon extracts the shell's favicon href from the embedded shell; no match panics at init.
func sitePagesShellIcon() template.URL {
	data, err := siteFiles.ReadFile("assets/index.html")
	if err != nil {
		panic("site: read embedded assets/index.html for the pages' default favicon: " + err.Error())
	}
	m := sitePagesShellIconRe.FindSubmatch(data)
	if m == nil {
		panic("objstore: no " + sitePagesShellIconRe.String() + " in the embedded site/index.html: the generated pages have no default favicon")
	}
	return template.URL(m[1])
}

// sitePageIcon resolves the icon a page's head declares: the configured favicon
// when it is set and small enough to repeat per page, else the shell default.
func sitePageIcon(favicon string) template.URL {
	if ValidSiteFavicon(favicon) && len(favicon) <= sitePagesInlineIconMax {
		return template.URL(favicon)
	}
	return sitePagesDefaultIcon
}

// sitePageChrome is the shared head/shell data every page stamps.
type sitePageChrome struct {
	Title         string       // full <title> (subject · site title)
	AccentCSS     template.CSS // per-push accent override stamped after the inlined core ("" — the core's stock teals govern)
	Icon          template.URL
	Description   string // meta/OG description, whitespace-collapsed, ~160 chars
	OGTitle       string // og:title (the bare subject)
	SiteTitle     string
	Canonical     string             // absolute self URL from site.url
	Robots        string             // meta robots content ("" = no tag, the indexable default)
	Route         string             // gs-route content, in the shell's parseRoute grammar
	Base          string             // relative path from this page to the site root ("./" or "../")
	Image         string             // absolute og:image/twitter:image URL ("" = no card, twitter:card stays "summary")
	Feed          string             // absolute feed.xml URL for the autodiscovery link (a relative href breaks after gs-upgrade.js hash-rewrites the location)
	TypeFeed      string             // absolute <dir>/feed.xml URL — a second autodiscovery link on a type's list pages ("" elsewhere)
	TypeFeedTitle string             // the type feed link's distinct display title ("<label> · <site title>")
	Nav           []sitePageNavGroup // the sidebar, with this page's own destination current (sitePageSidebar)
}

// sitePageChip is one state/type chip.
type sitePageChip struct{ Class, Label string }

// sitePageSection is one thread section on an item page: a reply, a tombstone
// line, or the release artifacts block.
type sitePageSection struct {
	Meta  []string
	Paras [][]string
	Pre   string
	Tomb  string
}

// sitePageReply is one thread reply, rendered as the app's comment or feedback card.
type sitePageReply struct {
	Variant    string // card variant classes ("comment", "feedback verdict-approved")
	Chips      []sitePageChip
	Glyph      string
	GlyphClass string
	GlyphTitle string
	Depth      int
	Meta       []string
	Paras      [][]string
	Tomb       string
}

// Rail returns one entry per depth level, so the template can emit the app's
// rail guides without an index loop.
func (r sitePageReply) Rail() []struct{} { return make([]struct{}, r.Depth) }

// siteItemPageData feeds the "item" template.
type siteItemPageData struct {
	Chrome    sitePageChrome
	ListDir   string
	ListLabel string
	// Subject titles the document; Heading is empty on a body-only type, whose first line is prose.
	Subject   string
	Heading   string
	Chips     []sitePageChip // the detail head's one chip slot, after the subject (siteHeadChips)
	Meta      []string
	Paras     [][]string
	Tomb      string
	Artifacts *sitePageSection // a release's artifact block, the only section with a Pre slot
	Replies   []sitePageReply
	Omitted   int
}

// sitePageNavLink is one sidebar entry: the app's nav item, pointed at this
// site's own generated page (Current takes the app's .active treatment).
type sitePageNavLink struct {
	Href    string
	Label   string
	Glyph   string
	Current bool
}

// sitePageNavGroup is one sidebar section and its links; an empty Section
// renders its links at the sidebar's top level, as the app's do.
type sitePageNavGroup struct {
	Section string
	Links   []sitePageNavLink
}

// sitePageListEntry is one row on a list or front page; ID, when set, is the row's own anchor.
type sitePageListEntry struct {
	ID         string
	Glyph      string // leading type glyph ("" — this type has none)
	GlyphClass string // its tint class (tg-open/tg-closed/tg-merged, else tg-<class type>)
	GlyphTitle string // the glyph's title attribute (sitePageGlyphTitle)
	Chip       *sitePageChip
	TailChips  []sitePageChip // chips after the subject, the app cardHead's trailing chips
	Href       string
	Title      string
	Meta       []string
}

// siteListPageData feeds the "list" template.
type siteListPageData struct {
	Chrome    sitePageChrome
	Heading   string
	MetaBits  []string
	Entries   []sitePageListEntry
	NewerHref string
	OlderHref string
}

// siteFrontPageData feeds the "front" template (index.html).
type siteFrontPageData struct {
	Chrome            sitePageChrome
	Description       string
	Home              *siteFrontHome
	Activity          []sitePageListEntry
	ActivityMoreHref  string // crawlable destination for the section's trailing link ("" — no rows)
	ActivityMoreLabel string // its label, shared with the app's control (siteActivityMoreLabel)
}

// siteFrontHome is the front page's body: the branch strip, the root file listing, then the README, in the app's home order.
type siteFrontHome struct {
	Branch       string           // default branch name ("" — no strip, no files)
	Branches     string           // "N branches", the app's branch-count chip
	BranchesHref string           // app link behind that chip
	Latest       *siteFrontCommit // default branch tip (nil when unreadable)
	Files        []siteFrontFile  // root entries, directories first, capped
	MoreHref     string           // app link to the code browser ("" — nothing hidden)
	MoreLabel    string           // "Show all N", the app's collapse control
	Readme       *siteFrontReadme
}

// siteFrontCommit is the front page's latest-commit bit (the app's meta strip).
type siteFrontCommit struct {
	Subject string
	Date    string
	Short   string
	Href    string // app link to the commit detail
}

// siteFrontFile is one root-tree row in the front page's file listing.
type siteFrontFile struct {
	Name string
	Href string // app link to the file/directory view
}

// siteFrontReadme is the front page's README, rendered by site_markdown.go; item bodies take sitePageParas instead and stay escaped text.
type siteFrontReadme struct {
	HTML      template.HTML
	Truncated bool
}

// sitePageSite is the resolved site identity every page stamps.
type sitePageSite struct {
	Title       string
	URL         string // normalized site.url (trailing slash)
	Description string
	Image       string       // absolute og:image URL ("" = no social card)
	Icon        template.URL // favicon href every page's head declares
	AccentCSS   template.CSS // accent override every page's head inlines ("" = none configured)
	Files       bool         // the file layer has pages: every sidebar carries the Files entry
}

// sitePageList describes one type directory: extension, bucket dir, label, shell route and sidebar identity.
type sitePageList struct {
	Ext      string
	Dir      string
	Label    string
	Route    string
	NavLabel string
	Glyph    string
	Section  string // sidebar section ("" — an ungrouped top-level item)
}

// sitePageLists orders the five type directories; the routes match gs-core.js parseRoute's INDEX_TABS.
var sitePageLists = []sitePageList{
	{Ext: "pm", Dir: "issues", Label: "issues", Route: "/issues", NavLabel: "Issues", Glyph: "○", Section: "PM"},
	{Ext: "review", Dir: "prs", Label: "pull requests", Route: "/prs", NavLabel: "Pull Requests", Glyph: "⑂", Section: "Repository"},
	{Ext: "social", Dir: "posts", Label: "posts", Route: "/timeline", NavLabel: "Timeline", Glyph: "⏱", Section: "Social"},
	{Ext: "release", Dir: "releases", Label: "releases", Route: "/releases", NavLabel: "Releases", Glyph: "⏏"},
	{Ext: "memo", Dir: "memos", Label: "memos", Route: "/memos", NavLabel: "Memos", Glyph: "☞"},
}

// renderSitePage executes one page template into bytes.
func renderSitePage(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := sitePageTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render %s page: %w", name, err)
	}
	return buf.Bytes(), nil
}

// sitePageAppURL builds the in-app hash URL on the front page for a shell route.
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

// sitePageParas splits an item body into paragraphs of lines; the template escapes every line, so no markup reaches the page.
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

// sitePageDescription extracts a meta description: the text, else the subject, stripped of markdown and truncated.
func sitePageDescription(text, fallback string) string {
	collapsed := strings.Join(strings.Fields(siteMarkdownPlainText(text)), " ")
	if collapsed == "" {
		collapsed = strings.Join(strings.Fields(siteMarkdownPlainText(fallback)), " ")
	}
	runes := []rune(collapsed)
	if len(runes) > sitePageDescriptionLen {
		return strings.TrimSpace(string(runes[:sitePageDescriptionLen])) + "…"
	}
	return collapsed
}

// sitePageBodyOnly reports whether an item type renders whole, with no first line promoted to a heading. Mirrors gs-core.js BODY_ONLY_TYPES.
func sitePageBodyOnly(t string) bool {
	switch t {
	case "comment", "feedback", "repost", "quote":
		return true
	}
	return false
}

// sitePageTypeLabel maps an item type to its display label.
func sitePageTypeLabel(t string) string {
	if t == "pull-request" {
		return "pull request"
	}
	return t
}

// sitePageTypeGlyph maps an item type to the app's leading card glyph (gs-core.js TYPE_GLYPH).
var sitePageTypeGlyph = map[string]string{
	"post": "•", "comment": "↩", "repost": "↻", "quote": "↻",
	"milestone": "◇", "sprint": "◷", "pull-request": "⑂", "feedback": "↩",
	"release": "⏏", "memo": "☞", "commit": "◦",
}

// sitePageGlyph returns a type's glyph and tint class (gs-render.js typeGlyphEl): itemType picks the character, classType the class and title.
func sitePageGlyph(itemType, classType, state string) (glyph, class string) {
	if itemType == "issue" {
		glyph = "○"
		if state == "closed" || state == "canceled" || state == "completed" {
			glyph = "●"
		}
	} else {
		glyph = sitePageTypeGlyph[itemType]
	}
	class = classType
	if classType == "issue" || classType == "pull-request" {
		class = sitePageStateClass(state)
	}
	return glyph, "tg-" + class
}

// sitePageGlyphTitle returns a glyph's title attribute: the class type, plus its state on a state-bearing type.
func sitePageGlyphTitle(classType, state string) string {
	if classType != "issue" && classType != "pull-request" {
		return classType
	}
	if state == "" {
		state = "open"
	}
	return classType + " · " + state
}

// sitePageGlyphClassType returns the type the app tints a glyph by: the item's header type, else the extension name.
func sitePageGlyphClassType(it *sitePageItem) string {
	if t := pageHeaderField(it.Msg, "type"); t != "" {
		return t
	}
	if t := pageHeaderField(it.Resolved, "type"); t != "" {
		return t
	}
	return it.Msg.Ext
}

// siteActivityMoreLabel labels the recent-activity section's trailing link on both surfaces.
const siteActivityMoreLabel = "See more"

// siteActivityMoreKey is that link's crawlable destination, the served page for the app's /timeline route.
const siteActivityMoreKey = "./posts/index.html"

// sitePageStateClass maps a workflow state to its chip color class; a cancel state matches by prefix, since the misspell linter rewrites the doubled-l spelling.
func sitePageStateClass(state string) string {
	if strings.HasPrefix(state, "cancel") {
		return "closed"
	}
	switch state {
	case "closed", "completed":
		return "closed"
	case "merged":
		return "merged"
	default:
		return "open"
	}
}

// sitePageChipStateClass maps a workflow state to the app's solid-fill chip class (gs-render.js stateChip).
func sitePageChipStateClass(state string) string {
	if strings.HasPrefix(state, "cancel") {
		return "canceled"
	}
	switch state {
	case "open", "closed", "merged", "completed", "active", "planned":
		return state
	}
	return "unknown"
}

// sitePageItemChip returns an item's leading state chip, nil when its type carries none.
func sitePageItemChip(it *sitePageItem) *sitePageChip {
	if it.Retracted {
		return &sitePageChip{Class: "chip-retracted", Label: "retracted"}
	}
	switch pageItemType(it) {
	case "issue", "milestone", "sprint", "pull-request":
		if pageItemType(it) == "pull-request" && pageItemField(it, "draft") == "true" {
			return &sitePageChip{Label: "draft"}
		}
		state := pageItemField(it, "state")
		if state == "" {
			state = "open"
		}
		return &sitePageChip{Class: "state " + sitePageChipStateClass(state), Label: state}
	case "release":
		if pageItemField(it, "prerelease") == "true" {
			return &sitePageChip{Class: "pre state", Label: "prerelease"}
		}
	}
	return nil
}

// siteHeadSubject titles a card or detail head: a release leads with its tag, every other type with its first line. Mirrors headSubject in gs-core.js.
func siteHeadSubject(itemType, tag, version, subject string) string {
	if itemType != "release" {
		return sitePageSubjectOrPlaceholder(subject)
	}
	if tag != "" {
		return tag
	}
	if version != "" {
		return "v" + version
	}
	if stripped := siteSubjectText(subject); stripped != "" {
		return stripped
	}
	return "(release)"
}

// siteReleaseVersionChip labels a release head's version chip, "" when the head already names the version. Mirrors releaseVersionChip in gs-core.js.
func siteReleaseVersionChip(version, head string) string {
	if version == "" || head == version || head == "v"+version {
		return ""
	}
	return "v" + version
}

// siteReleaseVersionChips returns a release head's version chip as the head's trailing chip list, empty on every other type.
func siteReleaseVersionChips(it *sitePageItem, head string) []sitePageChip {
	if pageItemType(it) != "release" {
		return nil
	}
	label := siteReleaseVersionChip(pageItemField(it, "version"), head)
	if label == "" {
		return nil
	}
	return []sitePageChip{{Label: label}}
}

// siteHeadChips lists a head's chip slot: the item's state pill, then a release's version. Mirrors headChips in gs-core.js.
func siteHeadChips(it *sitePageItem, head string) []sitePageChip {
	var chips []sitePageChip
	if chip := sitePageItemChip(it); chip != nil {
		chips = append(chips, *chip)
	}
	if it.Retracted {
		return chips
	}
	return append(chips, siteReleaseVersionChips(it, head)...)
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

// sitePageBaseHead formats a PR's "head → base" branch pair from its header refs.
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

// siteItemPageMeta builds an item page's meta-line bits: type, extras, author, date, markers and short ref.
func siteItemPageMeta(it *sitePageItem) []string {
	t := pageItemType(it)
	bits := []string{sitePageTypeLabel(t)}
	switch t {
	case "pull-request":
		if bh := sitePageBaseHead(it); bh != "" {
			bits = append(bits, bh)
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

// sitePageFeedbackVerdict returns a feedback's review verdict, "" for a plain comment. Mirrors feedbackVerdict in gs-core.js.
func sitePageFeedbackVerdict(r *sitePageItem) string {
	switch state := pageItemField(r, "review-state"); state {
	case "approved", "changes-requested":
		return state
	}
	return ""
}

// sitePageFeedbackAnchor formats a line-anchored feedback's "file:line" chip label. Mirrors feedbackAnchorLabel in gs-core.js.
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

// buildSiteReply renders one thread reply as a comment or feedback card, or a tombstone line when it was retracted.
func buildSiteReply(r *sitePageItem) sitePageReply {
	if r.Retracted {
		return sitePageReply{Variant: "comment", Depth: r.Depth, Tomb: "a reply from " + sitePageDate(pageEffectiveTime(r.Msg)) + " was retracted by its author"}
	}
	t := pageMsgType(r.Msg)
	glyph, glyphClass := sitePageGlyph(t, t, "")
	s := sitePageReply{
		Variant:    "comment",
		Depth:      r.Depth,
		Glyph:      glyph,
		GlyphClass: glyphClass,
		GlyphTitle: t,
		Meta:       []string{sitePageAuthorBit(r.Msg), sitePageDate(pageEffectiveTime(r.Msg))},
	}
	if t == "feedback" {
		s.Variant = "feedback"
		if verdict := sitePageFeedbackVerdict(r); verdict != "" {
			s.Variant += " verdict-" + verdict
			s.Chips = append(s.Chips, sitePageChip{Class: "verdict-" + verdict, Label: strings.ReplaceAll(verdict, "-", " ")})
		}
		if anchor := sitePageFeedbackAnchor(r); anchor != "" {
			s.Chips = append(s.Chips, sitePageChip{Label: anchor})
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

// buildSiteReleaseArtifacts returns a release page's artifact and checksum block.
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

// sitePageItemSubject returns the subject an item page is titled by: its head subject, or the type and tag when retracted.
func sitePageItemSubject(it *sitePageItem) string {
	if !it.Retracted {
		subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
		return siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), subject)
	}
	subject := "retracted " + sitePageTypeLabel(pageItemType(it))
	if tag := pageItemField(it, "tag"); tag != "" {
		subject += " " + tag
	}
	return subject
}

// siteItemPageTitles resolves every root's <title> subject so no two pages share one; taken holds the titles other layers published.
func siteItemPageTitles(roots map[string][]*sitePageItem, taken map[string]bool) map[string]string {
	var items []*sitePageItem
	for _, list := range sitePageLists {
		items = append(items, roots[list.Ext]...)
	}
	subjects := make(map[string]string, len(items))
	shared, dated := map[string]int{}, map[string]int{}
	for t := range taken {
		shared[t]++
	}
	for _, it := range items {
		subject := sitePageItemSubject(it)
		subjects[it.Msg.Short] = subject
		shared[subject]++
	}
	for _, it := range items {
		if subject := subjects[it.Msg.Short]; shared[subject] > 1 {
			dated[siteItemPageDatedTitle(it, subject)]++
		}
	}
	titles := make(map[string]string, len(items))
	for _, it := range items {
		subject := subjects[it.Msg.Short]
		if shared[subject] > 1 {
			subject = siteItemPageDatedTitle(it, subject)
			if dated[subject] > 1 {
				subject += " · #commit:" + it.Msg.Short
			}
		}
		titles[it.Msg.Short] = subject
	}
	return titles
}

// siteTitleSet collects the resolved titles a layer published, for the next layer's disambiguation.
func siteTitleSet(titles map[string]string) map[string]bool {
	set := make(map[string]bool, len(titles))
	for _, t := range titles {
		set[t] = true
	}
	return set
}

// siteItemPageDatedTitle appends an item's date to a subject two pages share.
func siteItemPageDatedTitle(it *sitePageItem, subject string) string {
	date := sitePageDate(pageEffectiveTime(it.Msg))
	if date == "" {
		return subject
	}
	return subject + " · " + date
}

// buildSiteItemPage assembles one root's item-page data: chrome, meta line, body or tombstone, release extras and the capped thread.
func buildSiteItemPage(it *sitePageItem, list sitePageList, site sitePageSite, title string) siteItemPageData {
	route := "commit:" + it.Msg.Short + "@gitmsg/" + list.Ext
	subject, body := protocol.SplitSubjectBody(pageItemBody(it))
	bodyOnly := sitePageBodyOnly(pageItemType(it))
	if bodyOnly {
		body = pageItemBody(it)
	}
	d := siteItemPageData{
		ListDir:   list.Dir,
		ListLabel: list.NavLabel,
		Subject:   sitePageItemSubject(it),
		Meta:      siteItemPageMeta(it),
	}
	robots := ""
	if !bodyOnly {
		d.Heading = siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), subject)
	}
	if it.Retracted {
		// A tombstone is the page's own words, so it heads every type.
		d.Heading = d.Subject
		d.Tomb = "this " + sitePageTypeLabel(pageItemType(it)) + " was retracted by its author"
		// The page stays for links that already exist, but has nothing to index.
		robots = sitePageRobotsNoIndex
		body = ""
	} else {
		d.Paras = sitePageParas(body)
	}
	d.Chips = siteHeadChips(it, d.Heading)
	d.Chrome = sitePageChrome{
		Title:       title + " · " + site.Title,
		AccentCSS:   site.AccentCSS,
		Description: sitePageDescription(body, d.Subject),
		OGTitle:     d.Subject,
		SiteTitle:   site.Title,
		Canonical:   site.URL + "i/" + it.Msg.Short + ".html",
		Robots:      robots,
		Route:       route,
		Base:        "../",
		Image:       site.Image,
		Icon:        site.Icon,
		Feed:        site.URL + sitePagesFeedKey,
		Nav:         sitePageSidebar("../", list.Dir, site.Files),
	}
	if pageItemType(it) == "release" {
		d.Artifacts = buildSiteReleaseArtifacts(it)
	}
	threadBytes := 0
	for i, r := range it.Replies {
		if i >= sitePageMaxReplies || threadBytes > sitePageMaxThreadBytes {
			d.Omitted = len(it.Replies) - i
			break
		}
		d.Replies = append(d.Replies, buildSiteReply(r))
		threadBytes += len(pageItemBody(r))
	}
	return d
}

// sitePageListChip drops a list row's state pill on the types whose glyph is already tinted by state.
func sitePageListChip(it *sitePageItem) *sitePageChip {
	chip := sitePageItemChip(it)
	if chip == nil || it.Retracted {
		return chip
	}
	switch pageItemType(it) {
	case "issue", "pull-request":
		if strings.HasPrefix(chip.Class, "state ") {
			return nil
		}
	}
	return chip
}

// sitePageSubjectOrPlaceholder strips a promoted first line to its words, or falls back to a placeholder.
func sitePageSubjectOrPlaceholder(subject string) string {
	if stripped := siteSubjectText(subject); stripped != "" {
		return stripped
	}
	return "(untitled)"
}

// buildSiteListEntry renders one root as a list or front row; defaultType suppresses the type bit on a type's own list.
func buildSiteListEntry(it *sitePageItem, base, defaultType string) sitePageListEntry {
	t := pageItemType(it)
	subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
	subject = siteHeadSubject(t, pageItemField(it, "tag"), pageItemField(it, "version"), subject)
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
	classType := sitePageGlyphClassType(it)
	state := pageItemField(it, "state")
	glyph, glyphClass := sitePageGlyph(t, classType, state)
	return sitePageListEntry{
		Glyph:      glyph,
		GlyphClass: glyphClass,
		GlyphTitle: sitePageGlyphTitle(classType, state),
		Chip:       sitePageListChip(it),
		TailChips:  siteReleaseVersionChips(it, subject),
		Href:       base + "i/" + it.Msg.Short + ".html",
		Title:      subject,
		Meta:       meta,
	}
}

// siteFrontActivityEntry pairs a rendered activity row with its sort key.
type siteFrontActivityEntry struct {
	row sitePageListEntry
	ts  int64
	sha string
}

// buildSiteFrontActivity merges the newest items (memo excluded) with the newest code commits into the front page's activity rows.
func buildSiteFrontActivity(roots map[string][]*sitePageItem, done map[string]int, code []siteMetaEntry, site sitePageSite) []sitePageListEntry {
	var merged []siteFrontActivityEntry
	for _, e := range code {
		short := e.SHA
		if len(short) > 12 {
			short = short[:12]
		}
		glyph, glyphClass := sitePageGlyph("commit", "commit", "")
		row := sitePageListEntry{
			Href:       sitePageAppURL(site, "commit:"+short+"@"+e.Branch),
			Title:      e.Subject,
			Meta:       []string{e.Author, sitePageDate(e.TS), short},
			Glyph:      glyph,
			GlyphClass: glyphClass,
			GlyphTitle: "commit",
		}
		merged = append(merged, siteFrontActivityEntry{row: row, ts: e.TS, sha: e.SHA})
	}
	for _, list := range sitePageLists {
		if list.Ext == "memo" {
			continue
		}
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			if it.Retracted {
				continue
			}
			subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
			itemType := pageItemType(it)
			subject = siteHeadSubject(itemType, pageItemField(it, "tag"), pageItemField(it, "version"), subject)
			name, _ := pageDisplayAuthor(it.Msg)
			classType := sitePageGlyphClassType(it)
			state := pageItemField(it, "state")
			glyph, glyphClass := sitePageGlyph(itemType, classType, state)
			row := sitePageListEntry{
				Href:       "./i/" + it.Msg.Short + ".html",
				Title:      subject,
				TailChips:  siteReleaseVersionChips(it, subject),
				Meta:       []string{name, sitePageDate(pageEffectiveTime(it.Msg))},
				Glyph:      glyph,
				GlyphClass: glyphClass,
				GlyphTitle: sitePageGlyphTitle(classType, state),
			}
			merged = append(merged, siteFrontActivityEntry{row: row, ts: pageEffectiveTime(it.Msg), sha: it.Msg.SHA})
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].ts != merged[j].ts {
			return merged[i].ts > merged[j].ts
		}
		return merged[i].sha > merged[j].sha
	})
	if len(merged) > sitePagesHomeActivity {
		merged = merged[:sitePagesHomeActivity]
	}
	rows := make([]sitePageListEntry, 0, len(merged))
	for _, m := range merged {
		rows = append(rows, m.row)
	}
	return rows
}

// sitePageNavSections orders the sidebar's sections, mirroring the app's nav; the empty section closes the list.
var sitePageNavSections = []string{"Social", "PM", "Repository", ""}

// sitePageSidebar builds a page's sidebar from the app's nav, narrowed to the destinations that have a generated page.
func sitePageSidebar(base, current string, files bool) []sitePageNavGroup {
	groups := []sitePageNavGroup{{Links: []sitePageNavLink{
		{Href: base + "index.html", Label: "Home", Glyph: "⌂", Current: current == ""},
	}}}
	lists := append(append([]sitePageList{}, sitePageLists...), siteCommitsList)
	if files {
		lists = append(lists, siteFilesList)
	}
	for _, section := range sitePageNavSections {
		group := sitePageNavGroup{Section: section}
		for _, l := range lists {
			if l.Section != section {
				continue
			}
			group.Links = append(group.Links, sitePageNavLink{
				Href:    base + l.Dir + "/index.html",
				Label:   l.NavLabel,
				Glyph:   l.Glyph,
				Current: l.Dir == current,
			})
		}
		if len(group.Links) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// siteXMLEscaper escapes text and attribute content for the sitemap and feed XML.
var siteXMLEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// siteSitemapEntry is one sitemap URL: location, last activity, and the creation sort key.
type siteSitemapEntry struct {
	loc     string
	lastmod string
	ts      int64
	sha     string
}

// buildSiteSitemapEntries collects the sitemap URL set: the site root, then every item page, sorted by creation so sealed part membership holds.
func buildSiteSitemapEntries(roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) []siteSitemapEntry {
	var items []siteSitemapEntry
	var newest int64
	for _, list := range sitePageLists {
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			last := sitePageLastActivity(it)
			if last > newest {
				newest = last
			}
			if it.Retracted {
				continue
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

// buildSiteSitemapListEntries collects the type-list index pages' entries; they ride the head part, not a sealed one.
func buildSiteSitemapListEntries(roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) []siteSitemapEntry {
	entries := make([]siteSitemapEntry, 0, len(sitePageLists))
	for _, list := range sitePageLists {
		var newest int64
		listed := 0
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			if it.Retracted {
				continue
			}
			listed++
			if t := sitePageLastActivity(it); t > newest {
				newest = t
			}
		}
		if listed == 0 {
			continue
		}
		entries = append(entries, siteSitemapEntry{loc: site.URL + list.Dir + "/index.html", lastmod: sitePageDate(newest)})
	}
	return entries
}

// sitePageLastActivity returns an item's latest activity: its creation, its resolved edit, or its newest reply.
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

// writeSiteSitemap writes the crawl map: one sitemap.xml, or an index over sealed parts plus the rewritten head part.
func writeSiteSitemap(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, commits *siteCommitsState, files *siteFilesState) error {
	entries := buildSiteSitemapEntries(roots, done, site)
	lists := append(buildSiteSitemapListEntries(roots, done, site), buildSiteCommitsSitemapEntries(commits, site)...)
	lists = append(lists, buildSiteFilesSitemapEntries(files, site)...)
	if len(entries) <= siteSitemapPartSize {
		return putSiteText(client, prefix+sitePagesSitemapKey, "application/xml", renderSiteURLSet(append(entries, lists...)))
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
	head := append(entries[sealed*siteSitemapPartSize:], lists...)
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
func writeSiteRobots(client *objstore.Client, prefix string, site sitePageSite) error {
	body := "User-agent: *\nAllow: /\nSitemap: " + site.URL + "sitemap.xml\n"
	return putSiteText(client, prefix+sitePagesRobotsKey, "text/plain; charset=utf-8", []byte(body))
}

// siteFeedEntry is one Atom entry projected from a top-level item; href is also its stable id.
type siteFeedEntry struct {
	title     string
	href      string
	updated   int64
	published int64
	author    string
	term      string
	content   string // escaped <p>/<br> HTML of the item's own body ("" = no content element)
}

// selectSiteFeedItems picks one feed's newest non-retracted top-level items; it runs before body attachment, so a dropped item costs no fetch.
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
	if len(items) > sitePagesFeedSize {
		items = items[:sitePagesFeedSize]
	}
	return items
}

// siteFeedContentHTML renders an item's own body as escaped HTML for the entry's content element, capped at siteFeedBodyMax.
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

// buildSiteFeedEntries projects the selected items into Atom entries.
func buildSiteFeedEntries(items []*sitePageItem, site sitePageSite) []siteFeedEntry {
	entries := make([]siteFeedEntry, 0, len(items))
	for _, it := range items {
		subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
		if subject = siteSubjectText(subject); subject == "" {
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

// siteFeedHead is one feed document's identity block.
type siteFeedHead struct {
	id       string
	title    string
	subtitle string // omitted when empty
	self     string
	alt      string
}

// renderSiteFeed renders one Atom 1.0 feed document; <updated> is the newest entry's activity, not the wall clock.
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
			// The content is HTML whose text is already escaped, XML-escaped again as chardata.
			b.WriteString("<content type=\"html\">" + esc(e.content) + "</content>\n")
		}
		b.WriteString("</entry>\n")
	}
	b.WriteString("</feed>\n")
	return []byte(b.String())
}

// putSiteFeed fetches the selected items' missing bodies and uploads one rendered feed document.
func putSiteFeed(client *objstore.Client, prefix, key string, items []*sitePageItem, head siteFeedHead, site sitePageSite) error {
	if err := attachRootBodies(client, prefix, items); err != nil {
		return fmt.Errorf("feed bodies %s: %w", key, err)
	}
	return putSiteText(client, prefix+key, "application/atom+xml; charset=utf-8", renderSiteFeed(buildSiteFeedEntries(items, site), head))
}

// writeSiteFeed writes the main Atom feed: the front page's item set, memos excluded.
func writeSiteFeed(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) error {
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

// siteTypeFeedTitle words a type feed's display title, distinct from the main feed's.
func siteTypeFeedTitle(list sitePageList, site sitePageSite) string {
	return list.NavLabel + " · " + site.Title
}

// writeSiteTypeFeeds writes the per-type Atom feeds; dirs (nil = every dir) limits the incremental pass.
func writeSiteTypeFeeds(client *objstore.Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, dirs map[string]bool) error {
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
