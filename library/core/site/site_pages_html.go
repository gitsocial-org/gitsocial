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
	// sitePagesFrontKey is the front page's bucket key; the page layer owns it whenever it is effective.
	sitePagesFrontKey = "index.html"
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
func sitePagesAccentCSS(cfg SiteCustomization) template.CSS {
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
<div class="nav-header">{{if .RepoIcon}}<img class="repo-icon" src="{{.RepoIcon}}" alt="">{{end}}<a class="repo-title" href="{{.Base}}index.html">{{.SiteTitle}}</a></div>
<nav class="nav-list">{{range .Nav}}{{if .Section}}<div class="nav-group"><div class="nav-section">{{.Section}}</div>{{end}}{{range .Links}}<a href="{{.Href}}"{{if .Current}} class="active"{{end}}><span class="nav-icon">{{.Glyph}}</span>{{.Label}}</a>{{end}}{{if .Section}}</div>{{end}}{{end}}</nav>
<div class="nav-footer"><a class="foot-brand" href="https://gitsocial.org"><svg class="logo-small" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg><span>Built with GitSocial</span></a></div>
</aside>
{{end}}{{define "chip"}}<span class="chip{{if .Class}} {{.Class}}{{end}}">{{.Label}}</span>{{end}}{{define "detailhead"}}<div class="card-head"><h1 class="subject">{{.Heading}}</h1>{{range .Chips}} {{template "chip" .}}{{end}}</div>{{end}}{{define "bits"}}{{range $i, $b := .}}{{if $i}} · {{end}}{{if $b.Href}}<a class="{{$b.Class}}" href="{{$b.Href}}">{{$b.Text}}</a>{{else if $b.Class}}<span class="{{$b.Class}}"{{if $b.Title}} title="{{$b.Title}}"{{end}}>{{$b.Text}}</span>{{else}}{{$b.Text}}{{end}}{{end}}{{end}}{{define "paras"}}{{range .}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>
{{end}}{{end}}{{define "body"}}{{range .}}{{if .Notes}}<dl class="release-notes">{{range .Notes}}<dt><a class="hash" href="{{.Href}}">{{.Hash}}</a></dt><dd>{{.Text}}</dd>{{end}}</dl>
{{else}}<p>{{range $i, $l := .Lines}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>
{{end}}{{end}}{{end}}{{define "assetrow"}}{{if .Href}}<a class="asset-row" href="{{.Href}}" rel="noopener"><span class="mono selectable">{{.Name}}</span>{{if .Chip}}<span class="chip">{{.Chip}}</span>{{end}}</a>{{else}}<div class="asset-row"><span class="mono selectable">{{.Name}}</span>{{if .Chip}}<span class="chip">{{.Chip}}</span>{{end}}</div>{{end}}{{end}}{{define "assets"}}<div class="assets"><div class="assets-head">Assets</div>
{{if .Artifacts}}<div class="asset-list">{{range .Artifacts}}{{template "assetrow" .}}{{end}}</div>
{{end}}{{if .Extra}}<div class="asset-list">{{range .Extra}}{{template "assetrow" .}}{{end}}</div>
{{end}}{{if .SignedBy}}<div class="asset-signed"><span class="meta">signed-by </span><span class="mono selectable">{{.SignedBy}}</span></div>
{{end}}</div>
{{end}}{{define "glyph"}}{{if .Glyph}}<span class="type-glyph {{.GlyphClass}}" title="{{.GlyphTitle}}">{{.Glyph}}</span> {{end}}{{end}}{{define "hometoggle"}}<summary class="home-toggle" aria-label="Show all"><span class="gs-icon chevron"><svg fill="none" viewBox="0 0 16 16" aria-hidden="true"><path stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" d="m3.5 6 4.5 4.5L12.5 6"/></svg></span></summary>
{{end}}{{define "filerow"}}<a class="home-row" href="{{.Href}}"><span class="home-row-subject">{{.Name}}</span></a>
{{end}}{{define "entries"}}{{range .}}<div class="card"{{if .ID}} id="{{.ID}}"{{end}}>{{if .BodyOnly}}<span class="meta meta-lead">{{template "glyph" .}}{{if .Chip}}{{template "chip" .Chip}} {{end}}{{template "bits" .Meta}}</span>
{{if .Text}}<div class="body">{{.Text}}</div>
{{end}}{{else}}<div class="card-head">{{template "glyph" .}}{{if .Chip}}{{template "chip" .Chip}} {{end}}<a class="subject" href="{{.Href}}">{{.Title}}</a>{{range .TailChips}} {{template "chip" .}}{{end}}</div>
<span class="meta">{{template "bits" .Meta}}</span>{{end}}</div>
{{end}}{{end}}{{define "item"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}{{template "detailhead" .}}
{{end}}<div class="detail-meta"><span class="meta">{{template "bits" .Meta}}</span></div>
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "body" .Body}}{{end}}{{with .Assets}}{{template "assets" .}}{{end}}{{if .Replies}}<div class="thread"><div class="thread-head">Comments ({{len .Replies}})</div>
{{range .Replies}}{{if .Depth}}<div class="comment-row"><div class="thread-rail">{{range $i := .Rail}}<span class="rail-guide"></span>{{end}}</div>{{end}}<div class="card {{.Variant}}">
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}<p class="meta meta-lead">{{template "glyph" .}}{{range .Chips}}{{template "chip" .}} {{end}}{{template "bits" .Meta}}</p>
{{template "paras" .Paras}}{{end}}</div>{{if .Depth}}</div>{{end}}
{{end}}</div>
{{end}}{{if .Omitted}}<p class="notice">{{.Omitted}} more replies not shown.</p>
{{end}}<footer><a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "list"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

<h1>{{.Heading}}</h1>
<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="empty">{{.Empty}}</p>
{{end}}<footer>{{if .NewerHref}}<a href="{{.NewerHref}}">← newer</a> {{end}}{{if .OlderHref}}<a href="{{.OlderHref}}">older →</a> {{end}}<a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "file"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}<h1>{{.Heading}}</h1>
{{end}}<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .HTML}}{{.HTML}}{{else}}<pre>{{.Pre}}</pre>
{{end}}{{if .Truncated}}<p class="notice">Truncated. The full file is in the repository.</p>
{{end}}<footer><a href="{{.Chrome.Base}}f/index.html">← files</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "front"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{with .Home}}<div class="home-section"><div class="home-head"><a class="chip" href="{{.BranchHref}}">{{.Branch}}</a>{{with .Latest}} <a class="home-commit" href="{{.Href}}"><span class="home-row-subject">{{.Subject}}</span> <span class="meta">{{template "bits" .Meta}}</span></a>{{end}} <a class="chip home-branches" href="{{.BranchesHref}}">{{.Branches}}</a></div>
{{range .Files}}{{template "filerow" .}}{{end}}{{if .MoreFiles}}<details class="home-more">{{template "hometoggle"}}{{range .MoreFiles}}{{template "filerow" .}}{{end}}</details><div class="home-fade"></div>
{{end}}</div>
{{end}}{{with .Home}}{{if .Readme}}<section><p class="meta">README</p>
{{.Readme.HTML}}{{if .Readme.Truncated}}<p class="notice">Truncated. The full file is in the repository.</p>
{{end}}</section>
{{end}}{{end}}<footer>{{range .Chrome.Nav}}{{range .Links}}{{if not .Current}}<a href="{{.Href}}">{{.Label}}</a> {{end}}{{end}}{{end}}</footer>
{{template "foot"}}{{end}}`

// sitePageTemplates is the parsed page template set, with the core CSS and the boot script spliced in.
var sitePageTemplates = template.Must(template.New("pages").Parse(
	strings.NewReplacer("@CORE@", sitePagesCoreCSS, "@BOOT@", sitePagesBootScript).Replace(sitePageTemplateText)))

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

// sitePageIcons resolves a page's head icon (favicon joined to base, or the shell default) and its sidebar repo-icon ("" unless a favicon validates).
func sitePageIcons(favicon, base string) (icon, repoIcon template.URL) {
	norm, ok := NormalizeSiteImage(favicon)
	if !ok {
		return sitePagesDefaultIcon, ""
	}
	if !strings.Contains(norm, "://") {
		norm = base + norm
	}
	return template.URL(norm), template.URL(norm)
}

// sitePageChrome is the shared head/shell data every page stamps.
type sitePageChrome struct {
	Title         string       // full <title> (subject · site title)
	AccentCSS     template.CSS // per-push accent override stamped after the inlined core ("" — the core's stock accent governs)
	Icon          template.URL
	RepoIcon      template.URL // configured favicon, shown beside the sidebar title ("" — no icon, never the default logo)
	Description   string       // meta/OG description, whitespace-collapsed, ~160 chars
	OGTitle       string       // og:title (the bare subject)
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

// sitePageBit is one meta-row bit: plain text, or a classed span carrying a title, or a link.
type sitePageBit struct {
	Class string // "" renders the text bare, as the page layer's own further bits do
	Text  string
	Title string // title attribute on a classed span
	Href  string // set on the hash bit, which renders as a link
}

// sitePageNoteRow is one commit a release body names: the short hash, the link to its commit, and the message beside it.
type sitePageNoteRow struct{ Hash, Href, Text string }

// sitePageBodyBlock is one block of an item body: prose lines, or the commit rows a release lists.
type sitePageBodyBlock struct {
	Lines []string
	Notes []sitePageNoteRow
}

// sitePageAsset is one release asset row: its name, its download link when the release gives it one, and its kind chip.
type sitePageAsset struct{ Name, Href, Chip string }

// sitePageAssets is a release's asset block: the artifacts, then the checksums and the SBOM, then the signing key.
type sitePageAssets struct {
	Artifacts []sitePageAsset
	Extra     []sitePageAsset
	SignedBy  string
}

// sitePageReply is one thread reply, rendered as the app's comment or feedback card.
type sitePageReply struct {
	Variant    string // card variant classes ("comment", "feedback verdict-approved")
	Chips      []sitePageChip
	Glyph      string
	GlyphClass string
	GlyphTitle string
	Depth      int
	Meta       []sitePageBit
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
	Subject string
	Heading string
	Chips   []sitePageChip // the detail head's one chip slot, after the subject (siteHeadChips)
	Meta    []sitePageBit
	Body    []sitePageBodyBlock
	Tomb    string
	Assets  *sitePageAssets // a release's asset block, nil on every other type
	Replies []sitePageReply
	Omitted int
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
	BodyOnly   bool   // no head: the glyph and the marker lead the meta row (siteRowHead)
	Text       string // a body-only row's own text, the body's first line
	Meta       []sitePageBit
}

// siteListPageData feeds the "list" template.
type siteListPageData struct {
	Chrome    sitePageChrome
	Heading   string
	Empty     string // the sentence a list with no rows shows (sitePageEmptyText)
	MetaBits  []string
	Entries   []sitePageListEntry
	NewerHref string
	OlderHref string
}

// siteFrontPageData feeds the "front" template (index.html).
type siteFrontPageData struct {
	Chrome sitePageChrome
	Home   *siteFrontHome
}

// siteFrontHome is the front page's code block and README, in the app's home order.
type siteFrontHome struct {
	Branch       string           // default branch name
	BranchHref   string           // app link to the branch's log
	Branches     string           // "N branches", the app's branch-count chip
	BranchesHref string           // app link behind that chip
	Latest       *siteFrontCommit // default branch tip (nil when unreadable)
	Files        []siteFrontFile  // the root entries the code block shows, directories first
	MoreFiles    []siteFrontFile  // the root entries behind its chevron
	Readme       *siteFrontReadme
}

// siteFrontCommit is the default branch's tip on the front page's head line.
type siteFrontCommit struct {
	Subject string
	Href    string        // app link to the commit detail
	Meta    []sitePageBit // the author and the date
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
	Favicon     string       // configured favicon: an absolute URL or a relative bucket key ("" = unconfigured)
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

// sitePageEmptyText words a list's empty state, the sentence the app's own list renders.
func sitePageEmptyText(list sitePageList) string {
	if list.Ext == "social" {
		return "No activity in this repository yet."
	}
	return "No " + list.Label + " in this repository."
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

// sitePageGlyph returns a type's glyph and tint class (gs-render.js typeGlyphEl); a state-bearing type is tinted by its state.
func sitePageGlyph(itemType, state string) (glyph, class string) {
	if itemType == "issue" {
		glyph = "○"
		if state == "closed" || state == "canceled" || state == "completed" {
			glyph = "●"
		}
	} else {
		glyph = sitePageTypeGlyph[itemType]
	}
	class = itemType
	if itemType == "issue" || itemType == "pull-request" {
		class = sitePageStateClass(state)
	}
	return glyph, "tg-" + class
}

// sitePageGlyphTitle returns a glyph's title attribute: the item type, plus its state on a state-bearing type.
func sitePageGlyphTitle(itemType, state string) string {
	if itemType != "issue" && itemType != "pull-request" {
		return itemType
	}
	if state == "" {
		state = "open"
	}
	return itemType + " · " + state
}

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

// sitePageAuthorLabel picks a meta row's author label: the display name, else the email, else "unknown". Mirrors authorLabel in gs-core.js.
func sitePageAuthorLabel(name, email string) string {
	if name != "" {
		return name
	}
	if email != "" {
		return email
	}
	return "unknown"
}

// sitePageAuthorBit builds a message's author bit, the email in its title when it is not the label.
func sitePageAuthorBit(m *sitePageMsg) sitePageBit {
	name, email := pageDisplayAuthor(m)
	label := sitePageAuthorLabel(name, email)
	bit := sitePageBit{Class: "author", Text: label}
	if email != "" && email != label {
		bit.Title = email
	}
	return bit
}

// sitePagePreciseTime formats a unix timestamp as the meta row's title stamp, in UTC since a static page cannot age.
func sitePagePreciseTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04") + " UTC"
}

// sitePageTimeBit builds a time bit: the date, with the precise time in its title.
func sitePageTimeBit(ts int64) sitePageBit {
	return sitePageBit{Class: "reltime", Text: sitePageDate(ts), Title: sitePagePreciseTime(ts)}
}

// sitePageHashBit builds a short-ref bit linking to the commit's own page or route.
func sitePageHashBit(short, href string) sitePageBit {
	return sitePageBit{Class: "hash", Text: short, Href: href}
}

// sitePageTextBit builds one of the page layer's own further bits, which carry no element.
func sitePageTextBit(text string) sitePageBit {
	return sitePageBit{Text: text}
}

// sitePageEditorName returns the editor's display name when the latest version was written by someone other than the author, else "".
func sitePageEditorName(it *sitePageItem) string {
	if !it.Edited || it.Resolved == it.Msg {
		return ""
	}
	editName, editEmail := pageDisplayAuthor(it.Resolved)
	_, authorEmail := pageDisplayAuthor(it.Msg)
	if strings.EqualFold(strings.TrimSpace(editEmail), strings.TrimSpace(authorEmail)) {
		return ""
	}
	return sitePageAuthorLabel(editName, editEmail)
}

// sitePageEditedBit builds the edited marker: the edit's precise time in its title, and the editor when it is not the author.
func sitePageEditedBit(it *sitePageItem) sitePageBit {
	text := "edited"
	if editor := sitePageEditorName(it); editor != "" {
		text = "edited by " + editor
	}
	return sitePageBit{Class: "edited", Text: text, Title: sitePagePreciseTime(pageEffectiveTime(it.Resolved))}
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

// siteItemPageMeta builds an item page's meta-line bits: the author, time and hash skeleton, the edited marker, then the page layer's own further bits.
func siteItemPageMeta(it *sitePageItem) []sitePageBit {
	t := pageItemType(it)
	bits := []sitePageBit{
		sitePageAuthorBit(it.Msg),
		sitePageTimeBit(pageEffectiveTime(it.Msg)),
		sitePageHashBit(it.Msg.Short, it.Msg.Short+".html"),
	}
	if it.Edited && !it.Retracted {
		bits = append(bits, sitePageEditedBit(it))
	}
	bits = append(bits, sitePageTextBit(sitePageTypeLabel(t)))
	switch t {
	case "pull-request":
		if bh := sitePageBaseHead(it); bh != "" {
			bits = append(bits, sitePageTextBit(bh))
		}
	case "milestone":
		if due := pageItemField(it, "due"); due != "" {
			bits = append(bits, sitePageTextBit("due "+due))
		}
	case "sprint":
		if start, end := pageItemField(it, "start"), pageItemField(it, "end"); start != "" || end != "" {
			bits = append(bits, sitePageTextBit(start+" → "+end))
		}
	}
	if t == "release" && pageItemField(it, "signed-by") != "" {
		bits = append(bits, sitePageTextBit("signed"))
	}
	return bits
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
	glyph, glyphClass := sitePageGlyph(t, "")
	s := sitePageReply{
		Variant:    "comment",
		Depth:      r.Depth,
		Glyph:      glyph,
		GlyphClass: glyphClass,
		GlyphTitle: t,
		Meta:       []sitePageBit{sitePageAuthorBit(r.Msg), sitePageTimeBit(pageEffectiveTime(r.Msg))},
	}
	if r.Edited {
		s.Meta = append(s.Meta, sitePageEditedBit(r))
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
			s.Meta = append(s.Meta, sitePageTextBit("suggestion"))
		}
	} else if r.InReplyTo != "" {
		s.Meta = append(s.Meta, sitePageTextBit("reply to "+r.InReplyTo))
	}
	s.Paras = sitePageParas(pageItemBody(r))
	return s
}

// siteReleaseAssetHref builds an asset's download link from the release's artifact-url base, "" when it has none or the result is not a fetchable target. Mirrors assetRow's gate in gs-render.js.
func siteReleaseAssetHref(base, name string) string {
	if base == "" {
		return ""
	}
	href := base + "/" + name
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(href, "/") {
		return href
	}
	return ""
}

// buildSiteReleaseAssets returns a release page's asset block: the artifacts, then the checksums and the SBOM, then the signing key. Mirrors releaseAssetsSection in gs-render.js.
func buildSiteReleaseAssets(it *sitePageItem) *sitePageAssets {
	base := strings.TrimSuffix(pageItemField(it, "artifact-url"), "/")
	assets := &sitePageAssets{SignedBy: pageItemField(it, "signed-by")}
	for _, name := range strings.Split(pageItemField(it, "artifacts"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			assets.Artifacts = append(assets.Artifacts, sitePageAsset{Name: name, Href: siteReleaseAssetHref(base, name)})
		}
	}
	if c := pageItemField(it, "checksums"); c != "" {
		assets.Extra = append(assets.Extra, sitePageAsset{Name: c, Href: siteReleaseAssetHref(base, c), Chip: "checksums"})
	}
	if s := pageItemField(it, "sbom"); s != "" {
		assets.Extra = append(assets.Extra, sitePageAsset{Name: s, Href: siteReleaseAssetHref(base, s), Chip: "SBOM"})
	}
	if len(assets.Artifacts) == 0 && len(assets.Extra) == 0 && assets.SignedBy == "" {
		return nil
	}
	return assets
}

// siteReleaseNoteRe matches a release-note line: a commit hash, then the message beside it.
var siteReleaseNoteRe = regexp.MustCompile(`^([0-9a-f]{7,40})[ \t]+(\S.*)$`)

// sitePageBodyBlocks splits an item body into the blocks a page renders; notes turns a block whose every line reads "<hash> <message>" into a release's commit rows. Mirrors itemBodyBlocks in gs-core.js.
func sitePageBodyBlocks(text string, notes bool) []sitePageBodyBlock {
	var blocks []sitePageBodyBlock
	for _, para := range sitePageParas(text) {
		if rows := siteReleaseNoteRows(para, notes); rows != nil {
			blocks = append(blocks, sitePageBodyBlock{Notes: rows})
			continue
		}
		blocks = append(blocks, sitePageBodyBlock{Lines: para})
	}
	return blocks
}

// siteReleaseNoteRows reads one block as commit rows, nil when notes are off or any line is prose.
func siteReleaseNoteRows(lines []string, notes bool) []sitePageNoteRow {
	if !notes || len(lines) == 0 {
		return nil
	}
	rows := make([]sitePageNoteRow, 0, len(lines))
	for _, line := range lines {
		m := siteReleaseNoteRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			return nil
		}
		rows = append(rows, sitePageNoteRow{Hash: m[1], Text: strings.TrimSpace(m[2])})
	}
	return rows
}

// siteLinkNoteRows points each commit row at its route in the app, the one surface that renders a plain commit.
func siteLinkNoteRows(blocks []sitePageBodyBlock, site sitePageSite) {
	for i := range blocks {
		for j := range blocks[i].Notes {
			blocks[i].Notes[j].Href = sitePageAppURL(site, "commit:"+blocks[i].Notes[j].Hash+"@")
		}
	}
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
		d.Body = sitePageBodyBlocks(body, pageItemType(it) == "release")
		siteLinkNoteRows(d.Body, site)
	}
	d.Chips = siteHeadChips(it, d.Heading)
	icon, repoIcon := sitePageIcons(site.Favicon, "../")
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
		Icon:        icon,
		RepoIcon:    repoIcon,
		Feed:        site.URL + sitePagesFeedKey,
		Nav:         sitePageSidebar("../", list.Dir, site.Files),
	}
	if pageItemType(it) == "release" {
		d.Assets = buildSiteReleaseAssets(it)
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

// siteRowChips lists a row's head chips: the head's own chips, less the state pill a tinted glyph already carries. Mirrors rowChips in gs-core.js.
func siteRowChips(it *sitePageItem, head string) []sitePageChip {
	chips := siteHeadChips(it, head)
	if len(chips) == 0 || it.Retracted {
		return chips
	}
	switch pageItemType(it) {
	case "issue", "pull-request":
		if strings.HasPrefix(chips[0].Class, "state ") {
			return chips[1:]
		}
	}
	return chips
}

// siteRowHeadChips splits a row's head chips by slot: the pill leading the subject, then the release version trailing it.
func siteRowHeadChips(it *sitePageItem, head string) (*sitePageChip, []sitePageChip) {
	chips := siteRowChips(it, head)
	if len(chips) == 0 || chips[0].Label == siteReleaseVersionChip(pageItemField(it, "version"), head) {
		return nil, chips
	}
	return &chips[0], chips[1:]
}

// siteReleaseAssetLabel words a release row's asset count, "" when it names none. Mirrors releaseAssetLabel in gs-core.js.
func siteReleaseAssetLabel(artifacts string) string {
	n := 0
	for _, a := range strings.Split(artifacts, ",") {
		if strings.TrimSpace(a) != "" {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "1 asset"
	}
	return fmt.Sprintf("%d assets", n)
}

// siteRowMeta builds a row's meta line: the author, the date, the hash, then the edited marker.
func siteRowMeta(it *sitePageItem, href string) []sitePageBit {
	meta := []sitePageBit{sitePageAuthorBit(it.Msg), sitePageTimeBit(pageEffectiveTime(it.Msg))}
	// A release is named by its tag, so its row counts assets where another row links its hash.
	if pageItemType(it) == "release" {
		if label := siteReleaseAssetLabel(pageItemField(it, "artifacts")); label != "" {
			meta = append(meta, sitePageTextBit(label))
		}
	} else {
		meta = append(meta, sitePageHashBit(it.Msg.Short, href))
	}
	if it.Edited && !it.Retracted {
		meta = append(meta, sitePageEditedBit(it))
	}
	return meta
}

// sitePageSubjectOrPlaceholder strips a promoted first line to its words, or falls back to a placeholder.
func sitePageSubjectOrPlaceholder(subject string) string {
	if stripped := siteSubjectText(subject); stripped != "" {
		return stripped
	}
	return "(untitled)"
}

// siteRowHead resolves a row's head: a body-only type takes none, so the marker leads its meta row and its first line stands as the card's text. Mirrors socialCard in gs-render.js.
func siteRowHead(it *sitePageItem) sitePageListEntry {
	t := pageItemType(it)
	subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
	if sitePageBodyOnly(t) {
		chip, _ := siteRowHeadChips(it, subject)
		return sitePageListEntry{BodyOnly: true, Chip: chip, Text: subject}
	}
	head := siteHeadSubject(t, pageItemField(it, "tag"), pageItemField(it, "version"), subject)
	chip, tail := siteRowHeadChips(it, head)
	return sitePageListEntry{Chip: chip, TailChips: tail, Title: head}
}

// buildSiteListEntry renders one root as a list page's row; defaultType suppresses the type bit on a type's own list.
func buildSiteListEntry(it *sitePageItem, defaultType string) sitePageListEntry {
	t := pageItemType(it)
	// A list page sits one directory below the item pages.
	href := "../i/" + it.Msg.Short + ".html"
	meta := siteRowMeta(it, href)
	if t != defaultType {
		meta = append(meta, sitePageTextBit(sitePageTypeLabel(t)))
	}
	if n := len(it.Replies); n == 1 {
		meta = append(meta, sitePageTextBit("1 comment"))
	} else if n > 0 || t == "issue" || t == "pull-request" {
		meta = append(meta, sitePageTextBit(fmt.Sprintf("%d comments", n)))
	}
	state := pageItemField(it, "state")
	glyph, glyphClass := sitePageGlyph(t, state)
	row := siteRowHead(it)
	row.Glyph, row.GlyphClass, row.GlyphTitle = glyph, glyphClass, sitePageGlyphTitle(t, state)
	row.Href, row.Meta = href, meta
	return row
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
		b.WriteString("<p>Truncated. The full item is in the repository.</p>")
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
