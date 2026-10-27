// site_pages_html.go - page templates, the inlined CSS base and the presentation builders

package site

import (
	"bytes"
	"cmp"
	"fmt"
	"hash/fnv"
	"html/template"
	"maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
<link rel="preload" as="style" href="{{.Base}}{{shellDir}}pages-full.css" onload="this.onload=null;this.rel='stylesheet'">
<noscript><link rel="stylesheet" href="{{.Base}}{{shellDir}}pages-full.css"></noscript>
{{shellTag}}
<script defer src="{{.Base}}{{shellDir}}gs-upgrade.js"></script>
</head>
<body>
<div id="gs-page" data-base="{{.Base}}">
{{end}}{{define "foot"}}</div>
</body>
</html>
{{end}}{{define "sidebar"}}<aside class="page-nav">
<input type="checkbox" class="page-nav-toggle" id="page-nav-toggle" aria-label="Open navigation">
<div class="nav-header"><label for="page-nav-toggle" class="nav-hamburger" aria-label="Open navigation"><svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M3 6h18M3 12h18M3 18h18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></label>{{if .RepoIcon}}<img class="repo-icon" src="{{.RepoIcon}}" alt="">{{end}}<a class="repo-title" href="{{.Base}}index.html">{{.SiteTitle}}</a></div>
<nav class="nav-list">{{range .Nav}}{{if .Section}}<div class="nav-group"><div class="nav-section">{{.Section}}</div>{{end}}{{range .Links}}<a href="{{.Href}}"{{if .Current}} class="active"{{end}}><span class="nav-icon">{{.Glyph}}</span>{{.Label}}</a>{{end}}{{if .Section}}</div>{{end}}{{end}}</nav>
<div class="nav-footer"><a class="foot-brand" href="https://gitsocial.org"><svg class="logo-small" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg><span>Built with GitSocial</span></a></div>
</aside>
{{end}}{{define "chip"}}{{if .Parts}}<span class="{{chipClass .}}">{{with chipLead .}}{{.}} {{end}}{{range $i, $p := .Parts}}{{if $i}} → {{end}}{{template "link" $p}}{{end}}</span>{{else if .Href}}<a class="{{chipClass .}}" href="{{.Href}}"{{if .Title}} title="{{.Title}}"{{end}}{{if .Rel}} rel="{{.Rel}}"{{end}}>{{with chipLead .}}{{.}} {{end}}{{.Label}}</a>{{else}}<span class="{{chipClass .}}"{{if .Title}} title="{{.Title}}"{{end}}>{{with chipLead .}}{{.}} {{end}}{{.Label}}</span>{{end}}{{end}}{{define "chiprow"}}{{if .}}<div class="card-chips">{{range .}}{{template "chip" .}} {{end}}</div>
{{end}}{{end}}{{define "detailhead"}}<div class="card-head"><h1 class="subject">{{if .HeadingHref}}<a href="{{.HeadingHref}}">{{.Heading}}</a>{{else}}{{.Heading}}{{end}}</h1></div>{{end}}{{define "link"}}{{if .Href}}<a{{if .Class}} class="{{.Class}}"{{end}} href="{{.Href}}"{{if .Title}} title="{{.Title}}"{{end}}{{if .Rel}} rel="{{.Rel}}"{{end}}>{{.Text}}</a>{{else}}{{.Text}}{{end}}{{end}}{{define "bits"}}{{range $i, $b := .}}{{if $i}}{{if $b.Sep}}{{$b.Sep}}{{else}} · {{end}}{{end}}{{if $b.Href}}{{template "link" $b}}{{else if $b.Class}}<span class="{{$b.Class}}"{{if $b.Title}} title="{{$b.Title}}"{{end}}>{{$b.Text}}</span>{{else}}{{$b.Text}}{{end}}{{end}}{{end}}{{define "paras"}}{{range .}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{range $l}}{{template "link" .}}{{end}}{{end}}</p>
{{end}}{{end}}{{define "body"}}{{range .}}{{if .Notes}}<dl class="release-notes">{{range .Notes}}<dt><a class="hash" href="{{.Href}}">{{.Hash}}</a></dt><dd>{{.Text}}</dd>{{end}}</dl>
{{else}}<p>{{range $i, $l := .Lines}}{{if $i}}<br>{{end}}{{range $l}}{{template "link" .}}{{end}}{{end}}</p>
{{end}}{{end}}{{end}}{{define "rows"}}{{if .}}<dl>{{range .}}<dt>{{.Key}}</dt><dd>{{if gt (len .Bits) 1}}<span>{{end}}{{range $i, $b := .Bits}}{{if $i}}, {{end}}{{template "link" $b}}{{end}}{{if gt (len .Bits) 1}}</span>{{end}}</dd>{{end}}</dl>
{{end}}{{end}}{{define "assetchip"}}{{if .Chip}}<span class="chip chip-ref">⏏ {{.Chip}}</span>{{end}}{{end}}{{define "assetrow"}}{{if .Href}}<a class="asset-row" href="{{.Href}}" rel="noopener"><span class="mono selectable">{{.Name}}</span>{{template "assetchip" .}}</a>{{else}}<div class="asset-row"><span class="mono selectable">{{.Name}}</span>{{template "assetchip" .}}</div>{{end}}{{end}}{{define "assets"}}<div class="assets"><div class="assets-head">Assets</div>
{{if .Artifacts}}<div class="asset-list">{{range .Artifacts}}{{template "assetrow" .}}{{end}}</div>
{{end}}{{if .Extra}}<div class="asset-list">{{range .Extra}}{{template "assetrow" .}}{{end}}</div>
{{end}}{{if .SignedBy}}<div class="asset-signed"><span class="chip chip-prov chip-signed" title="{{.SignedBy}}">✓ signed</span> <span class="mono selectable">{{.SignedBy}}</span></div>
{{end}}</div>
{{end}}{{define "diffhead"}}<span class="diff-status s-{{.Status}}">{{.Letter}}</span><span class="mono diff-path">{{.Path}}</span>{{if .Generated}}<span class="chip">generated</span>{{end}}<span class="diff-counts mono">{{if .Binary}}binary{{else}}<span class="cnt-add">+{{.Adds}}</span><span class="cnt-del">-{{.Dels}}</span>{{end}}</span>{{end}}{{define "diff"}}<div class="diff-section"><div class="diff-head"><span class="subject">Files changed{{if not .Notice}} ({{.Count}}){{end}}</span></div>
{{if .Notice}}<div class="notice">{{.Notice}}</div>
{{end}}{{range .Files}}{{if .Generated}}<details class="diff-file"><summary class="diff-file-head">{{template "diffhead" .}}</summary>{{else}}<div class="diff-file"><div class="diff-file-head">{{template "diffhead" .}}</div>{{end}}
{{if .Hunks}}<div class="diff-file-body"><div class="diff-body unified">{{range .Hunks}}<div class="diff-hunk-head mono">{{.Head}}</div>
{{range .Lines}}<div class="diff-line {{.Op}}"><span class="dl-num mono">{{.OldN}}</span><span class="dl-num mono">{{.NewN}}</span><span class="dl-sign mono">{{.Sign}}</span><span class="dl-text mono">{{.Text}}</span></div>
{{end}}{{end}}</div></div>
{{end}}{{if .Generated}}</details>{{else}}</div>{{end}}
{{end}}{{if .Omitted}}<div class="notice">{{.Omitted}} more not shown.</div>
{{end}}{{if .More}}<div class="notice">The page shows the first {{lineCap}} changed lines. <a href="{{.AppHref}}">The app shows the rest.</a></div>
{{end}}</div>
{{end}}{{define "avatar"}}{{with .Avatar}}<span class="avatar {{.Class}}" title="{{.Title}}">{{.Initials}}</span>{{end}}{{end}}{{define "glyph"}}{{if .Glyph}}<span class="type-glyph{{if .GlyphClass}} {{.GlyphClass}}{{end}}"{{if .GlyphTitle}} title="{{.GlyphTitle}}"{{end}}>{{.Glyph}}</span>{{end}}{{end}}{{define "hometoggle"}}<summary class="home-toggle" aria-label="Show all"><span class="gs-icon chevron"><svg fill="none" viewBox="0 0 16 16" aria-hidden="true"><path stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" d="m3.5 6 4.5 4.5L12.5 6"/></svg></span></summary>
{{end}}{{define "filerow"}}<a class="home-row" href="{{.Href}}">{{.Icon}}<span class="home-row-subject">{{.Name}}</span></a>
{{end}}{{define "entries"}}{{range .}}<div class="card"{{if .ID}} id="{{.ID}}"{{end}}>{{template "avatar" .}}{{template "glyph" .}}{{if or .Meta .BodyOnly}}<span class="meta">{{template "bits" .Meta}}{{if .Chip}} · <span class="marker">{{.Chip.Label}}</span>{{end}}</span>
{{end}}{{template "chiprow" .Chips}}{{if .BodyOnly}}{{if .Text}}<div class="body body-clamp">{{.Text}}</div>
{{end}}{{else}}<div class="card-head"><a class="subject" href="{{.Href}}">{{.Title}}</a></div>
{{if .Excerpt}}<div class="excerpt"><div class="body body-clamp">{{.Excerpt}}</div></div>
{{end}}{{end}}</div>
{{end}}{{end}}{{define "item"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}{{template "detailhead" .}}
{{end}}<div class="detail-meta">{{template "avatar" .}}<span class="meta">{{template "bits" .Meta}}{{range .MetaChips}} {{template "chip" .}}{{end}}</span>{{if .Chips}}<div class="card-chips">{{range .Chips}}{{template "chip" .}} {{end}}</div>{{end}}</div>

{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "rows" .Rows}}{{template "body" .Body}}{{end}}{{with .Quote}}<div class="{{.Variant}}"><div class="reply-quote-head meta">{{template "avatar" .}}{{template "glyph" .}}{{template "bits" .Meta}}</div><div class="body reply-quote-body body-clamp">{{template "paras" .Paras}}</div></div>
{{end}}{{with .Assets}}{{template "assets" .}}{{end}}{{with .Diff}}{{template "diff" .}}{{end}}{{if .Replies}}<div class="thread"><div class="thread-head">Comments ({{len .Replies}})</div>
{{range .Replies}}{{if .Depth}}<div class="comment-row"><div class="thread-rail">{{range $i := .Rail}}<span class="rail-guide"></span>{{end}}</div>{{end}}<div class="card {{.Variant}}">
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "avatar" .}}{{template "glyph" .}}<p class="meta">{{template "bits" .Meta}}</p>
{{template "chiprow" .Chips}}<div class="body body-clamp">{{template "paras" .Paras}}</div>{{end}}</div>{{if .Depth}}</div>{{end}}
{{end}}</div>
{{end}}{{if .Omitted}}<p class="notice">{{.Omitted}} more replies not shown.</p>
{{end}}<footer><a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "list"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

<div class="heading-row"><h1>{{.Heading}}</h1><span class="view-count">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</span></div>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="empty">{{.Empty}}</p>
{{end}}<footer>{{if .NewerHref}}<a href="{{.NewerHref}}">← newer</a> {{end}}{{if .OlderHref}}<a href="{{.OlderHref}}">older →</a> {{end}}<a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "file"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{if .Heading}}<h1>{{.Heading}}</h1>
{{end}}<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .HTML}}{{.HTML}}{{else}}<pre>{{.Pre}}</pre>
{{end}}{{if .Truncated}}<p class="notice">Truncated. The full file is in the repository.</p>
{{end}}<footer><a href="{{.Chrome.Base}}f/index.html">← files</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "front"}}{{template "head" .Chrome}}{{template "sidebar" .Chrome}}

{{with .Home}}<div class="home-section"><div class="home-head"><a class="chip" href="{{.BranchHref}}">⎇ {{.Branch}}</a>{{with .Latest}} <a class="home-commit" href="{{.Href}}"><span class="home-row-subject">{{.Subject}}</span> <span class="meta">{{template "bits" .Meta}}</span></a>{{end}}</div>
{{if .Files}}<div class="home-files">{{range .Files}}{{template "filerow" .}}{{end}}{{if .MoreFiles}}<details class="home-more">{{template "hometoggle"}}{{range .MoreFiles}}{{template "filerow" .}}{{end}}</details><div class="home-fade"></div>{{end}}</div>
{{end}}</div>
{{end}}{{with .Home}}{{if .Readme}}<section class="readme">
{{.Readme.HTML}}{{if .Readme.Truncated}}<p class="notice">Truncated. The full file is in the repository.</p>
{{end}}</section>
{{end}}{{end}}<footer>{{range .Chrome.Nav}}{{range .Links}}{{if not (or .Current .App)}}<a href="{{.Href}}">{{.Label}}</a> {{end}}{{end}}{{end}}</footer>
{{template "foot"}}{{end}}`

// sitePageTemplates is the parsed page template set, with the core CSS and the boot script spliced in.
// sitePagesShellDir is the versioned shell directory this binary's pages reference, fixed per build like the inlined core CSS.
var sitePagesShellDir = sitePagesReadShellDir()

// sitePagesReadShellDir derives the shell directory from the embedded assets; a broken embed panics at init, like the core CSS read.
func sitePagesReadShellDir() string {
	version, err := siteVersion()
	if err != nil {
		panic("site: hash embedded assets for the pages' shell dir: " + err.Error())
	}
	return shellDirFor(version)
}

// sitePagesShellScript announces the shell directory to the boot scripts; a
// prebuilt tag, so the JS escaper cannot respell the path and every consumer
// sees the same bytes.
var sitePagesShellScript = template.HTML(`<script>window.GS_SHELL="` + sitePagesShellDir + `";</script>`)

var sitePageTemplates = template.Must(template.New("pages").Funcs(template.FuncMap{
	"shellDir":  func() string { return sitePagesShellDir },
	"shellTag":  func() template.HTML { return sitePagesShellScript },
	"lineCap":   func() int { return sitePRDiffLineCap },
	"chipClass": sitePageChipClass,
	"chipLead":  sitePageChipLead,
}).Parse(
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

// sitePageChip is one chip: a span, a link when it has an href, or the two linked sides of a branch pair.
type sitePageChip struct {
	Kind         string // state, count, reference, facet, date or provenance; "" on a head chip, whose class carries its look
	Class, Label string
	Word, Icon   string // the relation word and the glyph before the label
	Href, Rel    string
	Title        string
	Parts        []sitePageBit // the two sides of a pull request's branch or tip pair (siteBranchPart, siteTipPart)
}

// sitePageBit is one meta-row or body bit: plain text, or a classed span carrying a title, or a link.
type sitePageBit struct {
	Class string // "" renders the text bare, as the page layer's own further bits do
	Text  string
	Title string // title attribute on a classed span or link
	Href  string // set on the hash bit and on a linked author bit, which render as a link
	Rel   string // rel attribute of a link: "nofollow ugc" on a URL in a body, "noopener" on a cross-repository link
	Sep   string // the separator before the bit in a meta row; "" is the row's own " · "
}

// sitePageNoteRow is one commit a release body names: the short hash, the link to its commit, and the message beside it.
type sitePageNoteRow struct{ Hash, Href, Text string }

// sitePageRow is one definition row of an item page: a header key and the bits of its value.
type sitePageRow struct {
	Key  string
	Bits []sitePageBit
}

// sitePageBodyBlock is one block of an item body: prose lines of bits, or the commit rows a release lists.
type sitePageBodyBlock struct {
	Lines [][]sitePageBit
	Notes []sitePageNoteRow
}

// sitePageAsset is one release asset row: its name, its download link when the release gives it one, and the label of its reference chip.
type sitePageAsset struct{ Name, Href, Chip string }

// sitePageAssets is a release's asset block: the artifacts, then the checksums and the SBOM, then the signing key.
type sitePageAssets struct {
	Artifacts []sitePageAsset
	Extra     []sitePageAsset
	SignedBy  string
}

// siteAvatar is the initials circle that leads a card with an author line.
type siteAvatar struct{ Class, Initials, Title string }

// sitePageReply is one thread reply, rendered as the app's comment or feedback card.
type sitePageReply struct {
	Variant    string         // card variant classes ("comment", "feedback verdict-approved")
	Avatar     *siteAvatar    // nil on a feedback card
	Chips      []sitePageChip // a feedback's verdict, suggestion mark and anchor, the chip row before its text
	Glyph      string
	GlyphClass string
	GlyphTitle string
	Depth      int
	Meta       []sitePageBit
	Paras      [][][]sitePageBit
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
	Subject     string
	Heading     string
	HeadingHref string         // a release head links its tag page (siteHeadingHref)
	MetaChips   []sitePageChip // the state pill and the marker chips that end the meta line (sitePageHeadChips)
	Chips       []sitePageChip // the chip row under the meta line, as on a card: the version of a release, then siteItemChips
	Avatar      *siteAvatar    // the author's face, the column of the meta block (sitePageAvatar)
	Meta        []sitePageBit
	Body        []sitePageBodyBlock
	Rows        []sitePageRow // the header fields no other part of the page carries (siteItemPageRows)
	Tomb        string
	Quote       *sitePageReply  // the quote box of a reply, a quote or a repost, nil when its message quotes nothing (buildSiteQuote)
	Assets      *sitePageAssets // a release's asset block, nil on every other type
	Diff        *sitePageDiff   // a pull request's Files changed section, nil on every other type
	Replies     []sitePageReply
	Omitted     int
}

// sitePageNavLink is one sidebar entry: the app's nav item, pointed at this
// site's own generated page (Current takes the app's .active treatment) or,
// for a destination with no page, at its route in the shell (App).
type sitePageNavLink struct {
	Href    string
	Label   string
	Glyph   string
	Current bool
	App     bool // the footer lists the page links only
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
	Avatar     *siteAvatar // nil on a row with no author line
	Glyph      string      // leading type glyph ("" — this type has none)
	GlyphClass string      // its tint class (tg-open/tg-closed/tg-merged, else tg-<class type>)
	GlyphTitle string      // the glyph's title attribute (sitePageGlyphTitle)
	Chip       *sitePageChip
	Chips      []sitePageChip // the chip row before the title or the text: the identity chips, the reply count, then siteItemChips
	Href       string
	Title      string
	BodyOnly   bool   // no head: the marker leads the meta row, and the text follows it (siteRowHead)
	Text       string // a body-only row's own text, the first line of its content (pageRowText)
	Excerpt    string // a titled row's excerpt, under the clamp with no toggle, since the row links its page (pageRowExcerpt)
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
	Branch     string           // default branch name
	BranchHref string           // app link to the branch's log
	Latest     *siteFrontCommit // default branch tip (nil when unreadable)
	Files      []siteFrontFile  // the root entries the code block shows, directories first
	MoreFiles  []siteFrontFile  // the root entries behind its chevron
	Readme     *siteFrontReadme
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
	Href string        // app link to the file/directory view
	Icon template.HTML // the type icon the app's treeIcon gives the row
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

// sitePageList describes one type directory: extension, bucket dir, label, shell route, sidebar label and glyph.
type sitePageList struct {
	Ext      string
	Dir      string
	Label    string
	Route    string
	NavLabel string
	Glyph    string
}

// sitePageLists orders the five type directories; the routes match gs-core.js parseRoute's INDEX_TABS.
var sitePageLists = []sitePageList{
	{Ext: "pm", Dir: "issues", Label: "issues", Route: "/issues", NavLabel: "Issues", Glyph: "☐"},
	{Ext: "review", Dir: "prs", Label: "pull requests", Route: "/prs", NavLabel: "Pull Requests", Glyph: "△"},
	{Ext: "social", Dir: "posts", Label: "posts", Route: "/timeline", NavLabel: "Timeline", Glyph: "⏱"},
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

// sitePageParaLines splits an item body into paragraphs of lines.
func sitePageParaLines(text string) [][]string {
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

// sitePageParas splits an item body into paragraphs of lines of bits, each ref and URL a link; the template escapes every bit, so no markup reaches the page (TestSitePages_bodyStaysEscaped).
func sitePageParas(text string, link func(ref string) string) [][][]sitePageBit {
	blocks := sitePageParaLines(text)
	paras := make([][][]sitePageBit, 0, len(blocks))
	for _, lines := range blocks {
		paras = append(paras, sitePageLineBits(lines, link))
	}
	return paras
}

// sitePageLineBits turns each line into bits: text, a ref anchor as the app renders it (TestParityPageBodyRefs), and a URL anchor with rel="nofollow ugc" (TestSitePages_externalURLNofollow).
func sitePageLineBits(lines []string, link func(ref string) string) [][]sitePageBit {
	out := make([][]sitePageBit, 0, len(lines))
	for _, line := range lines {
		var spans []protocol.RefSpan
		for _, u := range protocol.FindURLs(line) {
			url := strings.TrimRight(u.Ref, ".,;:!?")
			spans = append(spans, protocol.RefSpan{Start: u.Start, End: u.Start + len(url), Ref: url})
		}
		spans = append(spans, protocol.FindRefs(line)...)
		sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
		var bits []sitePageBit
		last := 0
		for _, s := range spans {
			if s.Start < last {
				continue
			}
			if s.Start > last {
				bits = append(bits, sitePageTextBit(line[last:s.Start]))
			}
			if strings.HasPrefix(s.Ref, "#") {
				bits = append(bits, sitePageBit{Class: "hash", Text: s.Ref, Href: link(s.Ref)})
			} else {
				bits = append(bits, sitePageBit{Text: s.Ref, Href: s.Ref, Rel: "nofollow ugc"})
			}
			last = s.End
		}
		if last < len(line) || len(bits) == 0 {
			bits = append(bits, sitePageTextBit(line[last:]))
		}
		out = append(out, bits)
	}
	return out
}

// sitePageRefHref resolves a body ref to its href: a commit ref to a root item in titles opens its page, any other ref the route of the app (TestSitePages_refHrefTargets).
func sitePageRefHref(site sitePageSite, titles map[string]string) func(ref string) string {
	return func(ref string) string {
		if hash := pageLocalCommitHash(ref); strings.HasPrefix(ref, "#commit:") && len(hash) == 12 && titles[hash] != "" {
			return "../i/" + hash + ".html"
		}
		return sitePageAppURL(site, ref[1:])
	}
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
	"milestone": "◇", "sprint": "◷", "pull-request": "△", "feedback": "↩",
	"release": "⏏", "memo": "☞", "commit": "•",
}

// sitePageGlyph returns a type's glyph and tint class (gs-render.js typeGlyphEl); a state-bearing type is tinted by its state.
func sitePageGlyph(itemType, state string) (glyph, class string) {
	glyph = sitePageTypeGlyph[itemType]
	switch {
	case itemType == "issue":
		glyph = "☐"
		if state == "closed" || state == "completed" {
			glyph = "☑"
		} else if strings.HasPrefix(state, "cancel") {
			glyph = "☒"
		}
	case itemType == "milestone" && (state == "closed" || strings.HasPrefix(state, "cancel")):
		glyph = "◆"
	case itemType == "pull-request" && state == "merged":
		glyph = "▲"
	case itemType == "sprint" && state == "active":
		glyph = "◶"
	case itemType == "sprint" && state == "completed":
		glyph = "◵"
	case itemType == "sprint" && strings.HasPrefix(state, "cancel"):
		glyph = "⊘"
	}
	class = itemType
	if itemType == "issue" || itemType == "pull-request" || itemType == "milestone" || itemType == "sprint" {
		class = sitePageStateClass(state)
	}
	return glyph, "tg-" + class
}

// sitePageGlyphTitle returns a glyph's title attribute: the item type, plus its state on a state-bearing type.
func sitePageGlyphTitle(itemType, state string) string {
	if itemType != "issue" && itemType != "pull-request" && itemType != "milestone" && itemType != "sprint" {
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

// sitePageHeadChips splits a page's head chips by slot: the pills that end the meta line, open included, and the version that leads the chip row. Mirrors pageHeadChips in gs-core.js.
func sitePageHeadChips(it *sitePageItem, head string) (pills, identity []sitePageChip) {
	version := siteReleaseVersionChip(pageItemField(it, "version"), head)
	for _, c := range siteHeadChips(it, head) {
		if c.Label == version {
			identity = append(identity, c)
		} else {
			pills = append(pills, c)
		}
	}
	return pills, identity
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

// siteAvatarHues is the count of avatar hue classes, av-0 to av-11. Mirrors AVATAR_HUES in gs-render.js.
const siteAvatarHues = 12

// siteAvatarInitials gives the first code point of the first two words of the name, else of the email's local part, else "?". Mirrors avatarInitials in gs-render.js.
func siteAvatarInitials(name, email string) string {
	letter := func(word string) string {
		r, _ := utf8.DecodeRuneInString(word)
		return strings.ToUpper(string(r))
	}
	// A login shown as @login gives its letter, not the @.
	words := strings.Fields(strings.TrimPrefix(strings.TrimSpace(name), "@"))
	if len(words) > 0 {
		var b strings.Builder
		for _, word := range words[:min(len(words), 2)] {
			b.WriteString(letter(word))
		}
		return b.String()
	}
	if local, _, _ := strings.Cut(strings.TrimSpace(email), "@"); local != "" {
		return letter(local)
	}
	return "?"
}

// siteAvatarHue returns FNV-1a 32 of the lowercase email, else of the name, modulo siteAvatarHues. Mirrors avatarHue in gs-render.js.
func siteAvatarHue(name, email string) int {
	key := strings.TrimSpace(email)
	if key == "" {
		key = strings.TrimSpace(name)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(key))) // a hash write never fails
	return int(hash.Sum32() % siteAvatarHues)
}

// sitePageAvatar builds an author's initials circle, its hue class from the email. Mirrors authorAvatar in gs-render.js.
func sitePageAvatar(name, email string) *siteAvatar {
	return &siteAvatar{Class: "av-" + strconv.Itoa(siteAvatarHue(name, email)), Initials: siteAvatarInitials(name, email), Title: sitePageAuthorLabel(name, email)}
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

// sitePageAuthorLink builds the author bit linked to the app's author search when the email is known. Mirrors authorEl in gs-render.js.
func sitePageAuthorLink(m *sitePageMsg) sitePageBit {
	bit := sitePageAuthorBit(m)
	if _, email := pageDisplayAuthor(m); email != "" {
		bit.Href = sitePageSearchHref("author", email)
	}
	return bit
}

// sitePageSearchHref builds the search route of the app with one facet prefilled; every page that carries it is one directory under the shell. Mirrors searchHref in gs-render.js.
func sitePageSearchHref(facet, value string) string {
	return "../index.html#/search/" + strings.ReplaceAll(url.QueryEscape(facet+":"+value), "+", "%20")
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

// sitePageAdoptedBit names the repository an adopted copy came from, linked when it is browsable; ok is false for any other item.
func sitePageAdoptedBit(it *sitePageItem) (sitePageBit, bool) {
	adopts := pageHeaderField(it.Msg, "adopts")
	if adopts == "" {
		return sitePageBit{}, false
	}
	repo := protocol.ParseRef(adopts).Repository
	bit := sitePageBit{Class: "adopted", Text: "adopted from " + repo}
	if strings.HasPrefix(repo, "https://") {
		bit.Href = repo
	}
	return bit, true
}

// sitePageEditedBit builds the edited marker: the edit's precise time in its title, and the editor when it is not the author.
func sitePageEditedBit(it *sitePageItem) sitePageBit {
	text := "edited"
	if editor := sitePageEditorName(it); editor != "" {
		text = "edited by " + editor
	}
	return sitePageBit{Class: "edited", Text: text, Title: sitePagePreciseTime(pageEffectiveTime(it.Resolved))}
}

// siteBranchPart builds one side of a pull request's branch pair: a branch of this repository linked to its log, a browsable fork branch as owner/repo:branch linked to its URL, anything else as written. Mirrors branchPart in gs-core.js.
func siteBranchPart(field string) sitePageBit {
	repo, rest, found := strings.Cut(field, "#")
	if !found {
		repo, rest = "", field
	}
	name, isBranch := strings.CutPrefix(rest, "branch:")
	if !isBranch {
		name = ""
	}
	if repo == "" && name != "" {
		return sitePageBit{Text: name, Href: "../index.html#branch:" + name}
	}
	if strings.HasPrefix(repo, "https://") {
		segs := strings.FieldsFunc(strings.TrimSuffix(repo, ".git"), func(r rune) bool { return r == '/' })
		owner := strings.Join(segs[max(len(segs)-2, 0):], "/")
		return sitePageBit{Text: owner + ":" + name, Href: field, Rel: "noopener", Title: field}
	}
	if field == "" {
		return sitePageTextBit("?")
	}
	return sitePageTextBit(field)
}

// sitePageAnyRefHashRe and sitePageCommitHashRe pull the hash out of a ref of any type and out of a commit ref. Mirror anyRefHash and refHash in gs-core.js.
var (
	sitePageAnyRefHashRe = regexp.MustCompile(`[#:]([0-9a-f]{7,40})(?:@|$)`)
	sitePageCommitHashRe = regexp.MustCompile(`commit:([0-9a-f]{7,40})`)
)

// sitePageAnyRefHash returns the 12-character hash a relation field names, "" when it names none.
func sitePageAnyRefHash(ref string) string {
	m := sitePageAnyRefHashRe.FindStringSubmatch(ref)
	if m == nil {
		m = sitePageCommitHashRe.FindStringSubmatch(ref)
	}
	if m == nil {
		return ""
	}
	return m[1][:min(len(m[1]), 12)]
}

// sitePageBotLoginRe and sitePageBotDomainRe match an automation in either half of the origin author email. Mirror BOT_LOGINS and BOT_DOMAINS in gs-core.js.
var (
	sitePageBotLoginRe  = regexp.MustCompile(`(?i)^(app/.+|.+\[bot\]|dependabot|dependabot-preview|vercel|netlify|claassistant|cla-bot|github-actions|codecov|codecov-commenter|renovate|renovate-bot|sonarcloud|sonarqubecloud|greptileai|coderabbitai|copilot|copilot-pull-request-reviewer|semantic-release-bot|allcontributors|imgbot|snyk-bot|mergify)$`)
	sitePageBotDomainRe = regexp.MustCompile(`(?i)(^|\.)(coderabbit\.ai|dependabot\.com|renovateapp\.com|greptile\.com|codecov\.io|mergify\.com)$`)
	sitePageGitHubIDRe  = regexp.MustCompile(`^\d+\+`)
)

// sitePageIsBotAuthor reports whether an origin author is an automation; the login drops GitHub's numeric id prefix. Mirrors isBotAuthor in gs-core.js.
func sitePageIsBotAuthor(h map[string]string) bool {
	login, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(h["origin-author-email"])), "@")
	login = sitePageGitHubIDRe.ReplaceAllString(login, "")
	name := strings.ToLower(strings.TrimSpace(h["origin-author-name"]))
	return (login != "" && sitePageBotLoginRe.MatchString(login)) || (domain != "" && sitePageBotDomainRe.MatchString(domain)) || strings.HasSuffix(name, "[bot]")
}

// siteChipKindClass maps a chip kind to its class; a value class such as chip-label rides beside it. Mirrors CHIP_KIND_CLASS in gs-core.js.
var siteChipKindClass = map[string]string{"state": "state", "count": "count chip-count", "reference": "chip-ref", "facet": "chip-facet", "date": "chip-date", "provenance": "chip-prov"}

// sitePageChipClass lists a chip's classes: chip, the class of its kind, then its value class. Mirrors chipClass in gs-core.js.
func sitePageChipClass(c sitePageChip) string {
	classes := []string{"chip"}
	for _, class := range []string{siteChipKindClass[c.Kind], c.Class} {
		if class != "" {
			classes = append(classes, class)
		}
	}
	return strings.Join(classes, " ")
}

// sitePageChipLead words what comes before a chip's label: the relation word, then the glyph. Mirrors chipLead in gs-core.js.
func sitePageChipLead(c sitePageChip) string {
	return strings.TrimSpace(c.Word + " " + c.Icon)
}

// siteItemRefs are the relation fields a chip names, in row order: the key, the word before the glyph, the glyph of the target, "" for the glyph of the type its branch holds, and whether a card caps it. Mirrors ITEM_REFS in gs-core.js.
var siteItemRefs = []struct {
	Key, Word, Icon string
	Capped          bool
}{
	{"original", "", "", false}, {"reply-to", "", "↩", false}, {"pull-request", "", "△", false}, {"milestone", "", "◇", false}, {"sprint", "", "◷", false}, {"parent", "", "☐", false},
	{"closes", "closes", "☐", true}, {"refs", "refs", "☐", true},
	{"blocks", "blocks", "☐", true}, {"blocked-by", "blocked by", "☐", true}, {"related", "related", "☐", true},
}

// siteRefChipCap is the chips a card shows per capped relation before its "+n" chip. Mirrors REF_CHIP_CAP in gs-core.js.
const siteRefChipCap = 3

// siteRefGlyph names the glyph of a ref's target type, else of the type its branch holds, an issue for a pm ref. Mirrors refGlyph in gs-core.js.
func siteRefGlyph(ref, typ string) string {
	ext := "pm"
	if _, branch, ok := strings.Cut(ref, "@"); ok && strings.HasPrefix(branch, "gitmsg/") {
		ext = strings.TrimPrefix(branch, "gitmsg/")
	}
	typ = cmp.Or(typ, sitePageDefaultTypes[ext])
	if typ == "issue" {
		return "☐"
	}
	return sitePageTypeGlyph[typ]
}

// siteRelationChips lists the reference chips of one relation field: a ref of this repository links its route and names its target's subject, else its short hash, and takes the glyph of its target's type from types; a browsable cross-repository ref links its URL; a value that is not refs alone is one chip as written. more caps a card's list and links its "+n" chip. Mirrors relationChips in gs-core.js.
func siteRelationChips(word, icon, val string, capped bool, subjects, types map[string]string, more string) []sitePageChip {
	refs := sitePageListBits(val, sitePageTextBit)
	if len(refs) == 0 {
		return nil
	}
	for _, r := range refs {
		if !sitePageHeaderRefRe.MatchString(r.Text) {
			return []sitePageChip{{Kind: "reference", Word: word, Icon: icon, Label: val}}
		}
	}
	shown := refs
	if more != "" && capped && len(refs) > siteRefChipCap {
		shown = refs[:siteRefChipCap]
	}
	chips := make([]sitePageChip, 0, len(shown)+1)
	for _, r := range shown {
		chip := sitePageChip{Kind: "reference", Word: word, Label: r.Text, Title: r.Text}
		repo, _, _ := strings.Cut(r.Text, "#")
		typ := ""
		if repo == "" {
			chip.Href = "../index.html" + r.Text
		} else if strings.HasPrefix(repo, "https://") {
			chip.Href, chip.Rel = r.Text, "noopener"
		}
		if m := sitePageCommitHashRe.FindStringSubmatch(r.Text); m != nil {
			hash := m[1][:min(len(m[1]), 12)]
			chip.Label = hash
			if subject := subjects[hash]; repo == "" && subject != "" {
				chip.Label = subject
			}
			if repo == "" {
				typ = types[hash]
			}
		}
		chip.Icon = cmp.Or(icon, siteRefGlyph(r.Text, typ))
		chips = append(chips, chip)
	}
	if len(shown) < len(refs) {
		chips = append(chips, sitePageChip{Kind: "reference", Label: "+" + strconv.Itoa(len(refs)-len(shown)), Href: more})
	}
	return chips
}

// siteDueOverdue reports a due date before today on an item that is open, or has no state; today is the UTC date. Mirrors dueOverdue in gs-core.js.
func siteDueOverdue(due, state string) bool {
	if len(due) < 10 || !siteDateRe.MatchString(due[:10]) || (state != "" && state != "open") {
		return false
	}
	return due[:10] < time.Now().UTC().Format(time.DateOnly)
}

// siteDateRe matches a YYYY-MM-DD date.
var siteDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// siteTipPart builds one side of a pull request's tip pair: a hash by its short, linked to its commit on the branch of this repository it points into, else the value as written, "?" when absent. Mirrors tipPart in gs-core.js.
func siteTipPart(key string, h map[string]string) sitePageBit {
	val := h[key]
	if !sitePageTipRe.MatchString(val) {
		return sitePageTextBit(cmp.Or(val, "?"))
	}
	return sitePageBit{Text: val[:min(len(val), 12)], Href: "../index.html#commit:" + val + "@" + sitePageTipBranch(key, h)}
}

// siteItemChips lists an item's chip row in kind order: the sprint dates first; the issue count, none at zero, and the asset count; the branch pair, the tip pair, stacked and the relations; the due date; priority, assignees, reviewers and labels; origin and bot. subjects names a reference target by its short hash and types its type, more is the page a card's "+n" chip opens, "" on a page, which shows every chip, and issues the member issues of a milestone or sprint entry. Mirrors itemChips in gs-core.js.
func siteItemChips(it *sitePageItem, subjects, types map[string]string, more string, issues int) []sitePageChip {
	h := sitePageItemHeader(it)
	t := pageItemType(it)
	var chips []sitePageChip
	if start, end := h["start"], h["end"]; t == "sprint" && (start != "" || end != "") {
		chips = append(chips, sitePageChip{Kind: "date", Label: cmp.Or(start, "?") + " → " + cmp.Or(end, "?")})
	}
	if issues > 0 {
		chips = append(chips, sitePageChip{Kind: "count", Icon: "☐", Label: siteCountLabel(issues, "issue", "issues")})
	}
	if t == "release" {
		if label := siteReleaseAssetLabel(h["artifacts"]); label != "" {
			chips = append(chips, sitePageChip{Kind: "count", Icon: sitePageTypeGlyph["release"], Label: label})
		}
	}
	if t == "pull-request" && (h["head"] != "" || h["base"] != "") {
		head, base := siteBranchPart(h["head"]), siteBranchPart(h["base"])
		chips = append(chips, sitePageChip{Kind: "reference", Icon: "⎇", Label: head.Text + " → " + base.Text, Parts: []sitePageBit{head, base}})
	}
	if t == "pull-request" && (h["base-tip"] != "" || h["head-tip"] != "") {
		head, base := siteTipPart("head-tip", h), siteTipPart("base-tip", h)
		chips = append(chips, sitePageChip{Kind: "reference", Icon: sitePageTypeGlyph["commit"], Label: head.Text + " → " + base.Text, Parts: []sitePageBit{head, base}})
	}
	if deps := h["depends-on"]; deps != "" {
		chip := sitePageChip{Kind: "reference", Icon: sitePageTypeGlyph["pull-request"], Label: "stacked"}
		if under := sitePageAnyRefHash(strings.Split(deps, ",")[0]); under != "" {
			chip.Href = "../index.html#commit:" + under + "@gitmsg/review"
		}
		chips = append(chips, chip)
	}
	for _, rel := range siteItemRefs {
		if val := h[rel.Key]; val != "" {
			chips = append(chips, siteRelationChips(rel.Word, rel.Icon, val, rel.Capped, subjects, types, more)...)
		}
	}
	if due := h["due"]; due != "" {
		class := "chip-due"
		if siteDueOverdue(due, h["state"]) {
			class += " overdue"
		}
		chips = append(chips, sitePageChip{Kind: "date", Class: class, Label: "due " + due})
	}
	labels := sitePageListBits(h["labels"], sitePageTextBit)
	for _, l := range labels {
		if level, ok := strings.CutPrefix(l.Text, "priority/"); ok {
			chips = append(chips, sitePageChip{Kind: "facet", Class: "chip-priority prio-" + level, Label: level, Href: sitePageSearchHref("label", l.Text)})
		}
	}
	for _, a := range sitePageListBits(h["assignees"], sitePageTextBit) {
		local, _, _ := strings.Cut(a.Text, "@")
		chips = append(chips, sitePageChip{Kind: "facet", Class: "chip-assignee", Icon: "☛", Label: cmp.Or(local, a.Text), Href: sitePageSearchHref("assignee", a.Text)})
	}
	for _, r := range sitePageListBits(h["reviewers"], sitePageTextBit) {
		local, _, _ := strings.Cut(r.Text, "@")
		chips = append(chips, sitePageChip{Kind: "facet", Icon: "✎", Label: cmp.Or(local, r.Text), Href: sitePageSearchHref("reviewer", r.Text)})
	}
	for _, l := range labels {
		if !strings.HasPrefix(l.Text, "priority/") && !strings.HasPrefix(l.Text, "status/") {
			chips = append(chips, sitePageChip{Kind: "facet", Class: "chip-label", Label: l.Text, Href: sitePageSearchHref("label", l.Text)})
		}
	}
	if h["origin-platform"] != "" || h["origin-url"] != "" || h["origin-author-name"] != "" {
		platform, u := cmp.Or(h["origin-platform"], "imported"), h["origin-url"]
		chip := sitePageChip{Kind: "provenance", Class: "chip-origin", Icon: "↗", Label: platform, Title: cmp.Or(u, platform)}
		if lower := strings.ToLower(u); strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:") {
			chip.Href, chip.Rel = u, "noopener"
		}
		chips = append(chips, chip)
	}
	if sitePageIsBotAuthor(h) {
		chips = append(chips, sitePageChip{Kind: "provenance", Class: "chip-bot", Icon: "⚙", Label: "bot", Title: cmp.Or(h["origin-author-email"], "automated author")})
	}
	return chips
}

// siteItemPageMeta builds an item page's meta-line bits: the author, the glyph, the time and the hash, then the edited and adopted markers.
func siteItemPageMeta(it *sitePageItem) []sitePageBit {
	t := pageItemType(it)
	// The page has no mark column, so the face leads the line and the glyph follows the name, as versionMetaRow in gs-render.js does.
	state := pageItemField(it, "state")
	glyph, glyphClass := sitePageGlyph(t, state)
	bits := []sitePageBit{
		sitePageAuthorLink(it.Msg),
		{Class: "type-glyph " + glyphClass, Text: glyph, Title: sitePageGlyphTitle(t, state), Sep: " "},
		sitePageTimeBit(pageEffectiveTime(it.Msg)),
		sitePageHashBit(it.Msg.Short, it.Msg.Short+".html"),
	}
	if it.Edited && !it.Retracted {
		bits = append(bits, sitePageEditedBit(it))
	}
	if bit, ok := sitePageAdoptedBit(it); ok {
		bits = append(bits, bit)
	}
	return bits
}

// sitePageTrailerSkip are the header fields another part of an item page carries: the route, the chips of the head, the meta row and the chip row; the edited marker carries edits and the thread root. Mirrors TRAILER_SKIP in gs-render.js.
var sitePageTrailerSkip = map[string]bool{
	"v": true, "ext": true, "type": true, "state": true, "draft": true, "retracted": true, "tag": true, "version": true, "prerelease": true, "adopts": true, "labels": true,
	"base": true, "head": true, "base-tip": true, "head-tip": true, "depends-on": true, "due": true, "start": true, "end": true, "assignees": true, "reviewers": true,
	"milestone": true, "sprint": true, "parent": true, "closes": true, "refs": true, "blocks": true, "blocked-by": true, "related": true,
	"original": true, "reply-to": true, "pull-request": true, "edits": true, "root": true,
	"review-state": true, "file": true, "old-line": true, "new-line": true, "old-line-end": true, "new-line-end": true,
	"origin-author-name": true, "origin-author-email": true, "origin-time": true, "origin-platform": true, "origin-url": true,
}

// sitePageReleaseSkip are the header fields the asset block of a release carries. Mirrors RELEASE_DETAIL_SKIP in gs-render.js.
var sitePageReleaseSkip = map[string]bool{"artifacts": true, "artifact-url": true, "checksums": true, "sbom": true, "signed-by": true}

// sitePageTipKeys are the header fields that carry a bare commit hash. Mirrors TIP_KEYS in gs-render.js.
var sitePageTipKeys = map[string]bool{"base-tip": true, "head-tip": true, "merge-base": true, "merge-head": true, "commit": true}

// sitePageFacetKeys maps a header field of emails or labels to the search facet that lists the items carrying one. Mirrors FACET_KEYS in gs-render.js.
var sitePageFacetKeys = map[string]string{"assignees": "assignee", "reviewers": "reviewer", "labels": "label"}

// sitePageHeaderRefRe matches one header ref, of this repository or qualified by one. Mirrors HEADER_REF_RE in gs-render.js.
var sitePageHeaderRefRe = regexp.MustCompile(`^[^#\s]*#(?:commit|branch|tag|file|list):\S+$`)

// sitePageTipRe matches a bare commit hash in a tip field.
var sitePageTipRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// sitePageOriginKeys are the provenance fields fixed at import (GITMSG 1.9), which an edit leaves as the canonical set them.
var sitePageOriginKeys = []string{"origin-author-name", "origin-author-email", "origin-platform", "origin-time", "origin-url"}

// sitePageItemHeader merges an item's header fields: the canonical's, then the latest version's over them, the origin fields the canonical's alone. Mirrors resolveItems in gs-core.js.
func sitePageItemHeader(it *sitePageItem) map[string]string {
	fields := map[string]string{}
	for _, m := range []*sitePageMsg{it.Msg, it.Resolved} {
		if m != nil && m.Header != nil {
			maps.Copy(fields, m.Header.Fields)
		}
	}
	for _, key := range sitePageOriginKeys {
		if v := pageHeaderField(it.Msg, key); v != "" {
			fields[key] = v
		} else {
			delete(fields, key)
		}
	}
	return fields
}

// sitePageRefLink links one header ref: a ref of this repository to its route, a browsable cross-repository ref to its URL as written; any other value stays text. Mirrors refLink in gs-render.js.
func sitePageRefLink(ref string) sitePageBit {
	if !sitePageHeaderRefRe.MatchString(ref) {
		return sitePageTextBit(ref)
	}
	repo, _, _ := strings.Cut(ref, "#")
	if repo == "" {
		return sitePageBit{Text: ref, Href: "../index.html" + ref}
	}
	if strings.HasPrefix(repo, "https://") {
		return sitePageBit{Text: ref, Href: ref, Rel: "noopener"}
	}
	return sitePageTextBit(ref)
}

// sitePageListBits renders a comma-separated header value as bits, each entry through link. Mirrors listValue in gs-render.js.
func sitePageListBits(val string, link func(string) sitePageBit) []sitePageBit {
	var bits []sitePageBit
	for _, part := range strings.Split(val, ",") {
		if part = strings.TrimSpace(part); part != "" {
			bits = append(bits, link(part))
		}
	}
	return bits
}

// sitePageRefListBits links each ref of a comma-separated list; a value that is not refs alone stays as written. Mirrors refListValue in gs-render.js.
func sitePageRefListBits(val string) []sitePageBit {
	for _, part := range strings.Split(val, ",") {
		if !sitePageHeaderRefRe.MatchString(strings.TrimSpace(part)) {
			return []sitePageBit{sitePageTextBit(val)}
		}
	}
	return sitePageListBits(val, sitePageRefLink)
}

// sitePageTipBranch names the branch of this repository a tip field points into: the head for head-tip, the base for the other tips; "" for a fork branch or a feedback's commit. Mirrors tipBranch in gs-render.js.
func sitePageTipBranch(key string, h map[string]string) string {
	if key == "commit" {
		return ""
	}
	field := h["base"]
	if key == "head-tip" {
		field = h["head"]
	}
	p := protocol.ParseRef(field)
	if p.Repository != "" || p.Type != protocol.RefTypeBranch {
		return ""
	}
	return p.Value
}

// sitePageTrailerBits renders one trailer value: a tip to its commit on its branch, an email or label to its search facet, each ref of a list to its route. Mirrors trailerValue in gs-render.js.
func sitePageTrailerBits(key, val string, h map[string]string) []sitePageBit {
	if sitePageTipKeys[key] {
		if sitePageTipRe.MatchString(val) {
			return []sitePageBit{{Text: val, Href: "../index.html#commit:" + val + "@" + sitePageTipBranch(key, h)}}
		}
		return []sitePageBit{sitePageTextBit(val)}
	}
	if facet := sitePageFacetKeys[key]; facet != "" {
		return sitePageListBits(val, func(v string) sitePageBit { return sitePageBit{Text: v, Href: sitePageSearchHref(facet, v)} })
	}
	return sitePageRefListBits(val)
}

// siteItemPageRows lists an item page's definition rows in key order: each header field no other part of the page carries. Mirrors trailerRows in gs-render.js, pinned by TestParityItemPageMeta.
func siteItemPageRows(it *sitePageItem) []sitePageRow {
	h := sitePageItemHeader(it)
	values := map[string][]sitePageBit{}
	for key, val := range h {
		if sitePageTrailerSkip[key] || (it.Msg.Ext == "release" && sitePageReleaseSkip[key]) {
			continue
		}
		values[key] = sitePageTrailerBits(key, val, h)
	}
	rows := make([]sitePageRow, 0, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		rows = append(rows, sitePageRow{Key: key, Bits: values[key]})
	}
	return rows
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

// sitePageFeedbackFileRef builds the app route a feedback's file chip opens: its new lines on the head branch of the pull request, its old lines on the base; "" when the branch is not one of this repository. Mirrors feedbackFileRef in gs-render.js.
func sitePageFeedbackFileRef(r, pr *sitePageItem) string {
	file := pageItemField(r, "file")
	if file == "" || pr == nil || pageItemType(pr) != "pull-request" {
		return ""
	}
	side, line, end := "base", "old-line", "old-line-end"
	if pageItemField(r, "new-line") != "" || pageItemField(r, "old-line") == "" {
		side, line, end = "head", "new-line", "new-line-end"
	}
	p := protocol.ParseRef(pageItemField(pr, side))
	if p.Repository != "" || p.Type != protocol.RefTypeBranch || p.Value == "" {
		return ""
	}
	ref := "#file:" + file + "@" + p.Value
	if n := pageItemField(r, line); n != "" {
		ref += ":L" + n
		if e := pageItemField(r, end); e != "" && e != n {
			ref += "-" + e
		}
	}
	return "../index.html" + ref
}

// buildSiteReply renders one thread reply as a comment or feedback card, or a tombstone line when it was retracted; root is the item the reply is under, and link resolves the refs of its body.
func buildSiteReply(r, root *sitePageItem, link func(ref string) string) sitePageReply {
	if r.Retracted {
		return sitePageReply{Variant: "comment", Depth: r.Depth, Tomb: "a reply from " + sitePageDate(pageEffectiveTime(r.Msg)) + " was retracted by its author"}
	}
	t := pageMsgType(r.Msg)
	glyph, glyphClass := sitePageGlyph(t, "")
	s := sitePageReply{
		Variant:    "comment",
		Avatar:     sitePageAvatar(pageDisplayAuthor(r.Msg)),
		Depth:      r.Depth,
		Glyph:      glyph,
		GlyphClass: glyphClass,
		GlyphTitle: t,
		Meta:       []sitePageBit{sitePageAuthorLink(r.Msg), sitePageTimeBit(pageEffectiveTime(r.Msg))},
	}
	// A comment's author line carries its hash, as metaRow in gs-render.js does; a feedback card's does not.
	if t != "feedback" {
		s.Meta = append(s.Meta, sitePageHashBit(r.Msg.Short, "../index.html#commit:"+r.Msg.Short+"@gitmsg/"+r.Msg.Ext))
	}
	if r.Edited {
		s.Meta = append(s.Meta, sitePageEditedBit(r))
	}
	if t == "feedback" {
		s.Variant, s.Avatar = "feedback", nil
		if verdict := sitePageFeedbackVerdict(r); verdict != "" {
			s.Variant += " verdict-" + verdict
			s.Chips = append(s.Chips, sitePageChip{Kind: "state", Class: "verdict-" + verdict, Label: strings.ReplaceAll(verdict, "-", " ")})
		}
		if pageItemField(r, "suggestion") == "true" {
			s.Chips = append(s.Chips, sitePageChip{Kind: "state", Class: "chip-suggestion", Label: "suggestion"})
		}
		if anchor := sitePageFeedbackAnchor(r); anchor != "" {
			s.Chips = append(s.Chips, sitePageChip{Kind: "reference", Label: anchor, Href: sitePageFeedbackFileRef(r, root)})
		}
	}
	s.Paras = sitePageParas(pageItemBody(r), link)
	return s
}

// siteQuotedText reads the quoted lines of a GitMsg-Ref, each without its quote marker, up to the first line that is not quoted. Mirrors parseRefs in gs-core.js.
func siteQuotedText(metadata string) string {
	var lines []string
	for _, line := range strings.Split(metadata, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimLeft(line, " "), ">")
		if !ok {
			break
		}
		lines = append(lines, strings.TrimPrefix(rest, " "))
	}
	return strings.Join(lines, "\n")
}

// buildSiteQuote builds the quote box of a reply, a quote or a repost from the GitMsg-Ref its message carries for what it answers, reply-to before original; nil when the message quotes nothing. Mirrors replyQuoteBlock in gs-render.js.
func buildSiteQuote(it *sitePageItem, link func(ref string) string) *sitePageReply {
	t := pageItemType(it)
	if t != "comment" && t != "quote" && t != "repost" {
		return nil
	}
	msg := protocol.ParseMessage(it.Msg.Message)
	if msg == nil {
		return nil
	}
	for _, key := range []string{"reply-to", "original"} {
		val := pageItemField(it, key)
		if val == "" {
			continue
		}
		for _, ref := range msg.References {
			quoted := siteQuotedText(ref.Metadata)
			if quoted == "" || (ref.Ref != val && (sitePageAnyRefHash(ref.Ref) == "" || sitePageAnyRefHash(ref.Ref) != sitePageAnyRefHash(val))) {
				continue
			}
			author := &sitePageMsg{Author: ref.Author, Email: ref.Email}
			glyph, glyphClass := sitePageGlyph(ref.Fields["type"], "")
			q := &sitePageReply{Variant: "reply-quote dimmed", Avatar: sitePageAvatar(ref.Author, ref.Email), Glyph: glyph, GlyphClass: glyphClass, GlyphTitle: ref.Fields["type"], Meta: []sitePageBit{sitePageAuthorLink(author)}, Paras: sitePageParas(quoted, link)}
			if t == "repost" {
				q.Variant = "reply-quote"
			}
			if at, err := time.Parse(time.RFC3339, ref.Time); err == nil {
				q.Meta = append(q.Meta, sitePageTimeBit(at.Unix()))
			}
			if hash := pageLocalCommitHash(ref.Ref); hash != "" {
				_, branch, _ := strings.Cut(ref.Ref, "@")
				q.Meta = append(q.Meta, sitePageHashBit(hash, "../index.html#commit:"+hash+"@"+branch))
			}
			return q
		}
	}
	return nil
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

// sitePageBodyBlocks splits an item body into the blocks a page renders; notes turns a block whose every line reads "<hash> <message>" into a release's commit rows, and link resolves the refs of the prose. Mirrors itemBodyBlocks in gs-core.js.
func sitePageBodyBlocks(text string, notes bool, link func(ref string) string) []sitePageBodyBlock {
	var blocks []sitePageBodyBlock
	for _, para := range sitePageParaLines(text) {
		if rows := siteReleaseNoteRows(para, notes); rows != nil {
			blocks = append(blocks, sitePageBodyBlock{Notes: rows})
			continue
		}
		blocks = append(blocks, sitePageBodyBlock{Lines: sitePageLineBits(para, link)})
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

// siteHeadingHref links a release head to the tag page of the app when the head is its tag and the item stands; "" for every other head. Mirrors headTagLink in gs-render.js.
func siteHeadingHref(it *sitePageItem, heading string) string {
	if tag := pageItemField(it, "tag"); tag != "" && heading == tag && !it.Retracted {
		return "../index.html#tag:" + tag
	}
	return ""
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

// buildSiteItemPage assembles one root's item-page data: chrome, meta line, body or tombstone, release extras, a pull request's diff and the capped thread; titles names every root that has a page.
func buildSiteItemPage(it *sitePageItem, list sitePageList, site sitePageSite, titles, types map[string]string, src *objstore.LocalCommitSource, refs map[string]string) siteItemPageData {
	route := "commit:" + it.Msg.Short + "@gitmsg/" + list.Ext
	link := sitePageRefHref(site, titles)
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
		d.Body = sitePageBodyBlocks(body, pageItemType(it) == "release", link)
		siteLinkNoteRows(d.Body, site)
		d.Rows = siteItemPageRows(it)
	}
	pills, identity := sitePageHeadChips(it, d.Heading)
	d.MetaChips = pills
	d.Chips = append(identity, siteItemChips(it, titles, types, "", 0)...)
	d.Avatar = sitePageAvatar(pageDisplayAuthor(it.Msg))
	d.HeadingHref = siteHeadingHref(it, d.Heading)
	icon, repoIcon := sitePageIcons(site.Favicon, "../")
	d.Chrome = sitePageChrome{
		Title:       titles[it.Msg.Short] + " · " + site.Title,
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
	if !it.Retracted {
		d.Quote = buildSiteQuote(it, link)
	}
	d.Diff = buildSitePRDiff(it, site, src, refs)
	threadBytes := 0
	for i, r := range it.Replies {
		if i >= sitePageMaxReplies || threadBytes > sitePageMaxThreadBytes {
			d.Omitted = len(it.Replies) - i
			break
		}
		d.Replies = append(d.Replies, buildSiteReply(r, it, link))
		threadBytes += len(pageItemBody(r))
	}
	return d
}

// siteRowChips lists a row's head chips: the head's own chips, less an open or planned pill, the resting state of an item or a sprint. Mirrors rowChips in gs-core.js.
func siteRowChips(it *sitePageItem, head string) []sitePageChip {
	chips := siteHeadChips(it, head)
	if len(chips) > 0 && (chips[0].Class == "state open" || chips[0].Class == "state planned") {
		return chips[1:]
	}
	return chips
}

// siteRowHeadChips splits a row's head chips by slot: the pill the author line words, then the release version that leads the chip row.
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
	meta := []sitePageBit{sitePageAuthorLink(it.Msg), sitePageTimeBit(pageEffectiveTime(it.Msg)), sitePageHashBit(it.Msg.Short, href)}
	if it.Edited && !it.Retracted {
		meta = append(meta, sitePageEditedBit(it))
	}
	if bit, ok := sitePageAdoptedBit(it); ok {
		meta = append(meta, bit)
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

// siteRowHead resolves a row's head: a body-only type takes none, so the marker leads its meta row and its indexed first line stands as the card's text on both page passes (TestSitePages_incrementalEqualsFull). Mirrors socialCard in gs-render.js.
func siteRowHead(it *sitePageItem) sitePageListEntry {
	t := pageItemType(it)
	if sitePageBodyOnly(t) {
		text := pageRowText(it)
		chip, identity := siteRowHeadChips(it, text)
		return sitePageListEntry{BodyOnly: true, Chip: chip, Chips: identity, Text: text}
	}
	subject, _ := protocol.SplitSubjectBody(pageItemBody(it))
	head := siteHeadSubject(t, pageItemField(it, "tag"), pageItemField(it, "version"), subject)
	chip, identity := siteRowHeadChips(it, head)
	excerpt := pageRowExcerpt(it)
	// A release headed by its tag leads its excerpt with its subject, as releaseCard in gs-render.js does.
	if lead := siteSubjectText(subject); t == "release" && lead != "" && lead != head && !it.Retracted {
		excerpt = strings.TrimSpace(lead + "\n" + excerpt)
	}
	return sitePageListEntry{Chip: chip, Chips: identity, Title: head, Excerpt: excerpt}
}

// siteEntryCounts are the count chips of one list entry: its replies, the reposts and quotes that name it, the latest verdict of each reviewer, its sub-issues and the member issues of a milestone or sprint.
type siteEntryCounts struct{ Replies, Reposts, Quotes, Approved, ChangesRequested, Sub, Issues int }

// siteCountLabel words a count with its noun, singular for one.
func siteCountLabel(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// siteCountChips lists an entry's interaction counts as count chips of glyph and number, none for a zero count. Mirrors countChips in gs-render.js.
func siteCountChips(c siteEntryCounts) []sitePageChip {
	var chips []sitePageChip
	for _, p := range []struct {
		icon, class string
		n           int
	}{{"↩", "", c.Replies}, {"↻", "", c.Reposts}, {"❞", "", c.Quotes}, {"✓", "review-ok", c.Approved}, {"✗", "review-no", c.ChangesRequested}} {
		if p.n > 0 {
			chips = append(chips, sitePageChip{Kind: "count", Class: p.class, Icon: p.icon, Label: strconv.Itoa(p.n)})
		}
	}
	return chips
}

// siteVerdicts counts the latest verdict of each reviewer among a pull request's replies. Mirrors loadInteractionCounts in gs-core.js.
func siteVerdicts(replies []*sitePageItem) (approved, changes int) {
	latest := map[string]*sitePageItem{}
	for _, r := range replies {
		if r.Retracted || sitePageFeedbackVerdict(r) == "" {
			continue
		}
		_, email := pageDisplayAuthor(r.Msg)
		if prev := latest[strings.ToLower(email)]; prev == nil || pageEffectiveTime(r.Msg) >= pageEffectiveTime(prev.Msg) {
			latest[strings.ToLower(email)] = r
		}
	}
	for _, r := range latest {
		if sitePageFeedbackVerdict(r) == "approved" {
			approved++
		} else {
			changes++
		}
	}
	return approved, changes
}

// siteEntryTallies counts, by the short hash of each target, the reposts and quotes that name it, the sub-issues of an issue and the member issues of a milestone or sprint, from the index alone. Mirrors loadInteractionCounts and groupPM in gs-core.js.
func siteEntryTallies(roots map[string][]*sitePageItem) map[string]siteEntryCounts {
	tallies := map[string]siteEntryCounts{}
	tally := func(ref string, bump func(c *siteEntryCounts)) {
		if hash := pageLocalCommitHash(ref); hash != "" {
			c := tallies[hash]
			bump(&c)
			tallies[hash] = c
		}
	}
	for ext, rs := range roots {
		for _, it := range rs {
			if it.Retracted {
				continue
			}
			switch t := pageItemType(it); {
			case t == "repost":
				tally(pageItemField(it, "original"), func(c *siteEntryCounts) { c.Reposts++ })
			case t == "quote":
				tally(pageItemField(it, "original"), func(c *siteEntryCounts) { c.Quotes++ })
			case ext == "pm" && t == "issue":
				tally(pageItemField(it, "parent"), func(c *siteEntryCounts) { c.Sub++ })
				tally(pageItemField(it, "milestone"), func(c *siteEntryCounts) { c.Issues++ })
				tally(pageItemField(it, "sprint"), func(c *siteEntryCounts) { c.Issues++ })
			}
		}
	}
	return tallies
}

// buildSiteListEntry renders one root as a list page's row: the author line, the chip row led by the identity chips, the sub-issue count and the interaction counts, then the title or the text; subjects names the items its chips reference, and counts holds what other entries add to it.
func buildSiteListEntry(it *sitePageItem, subjects, types map[string]string, counts siteEntryCounts) sitePageListEntry {
	t := pageItemType(it)
	// A list page sits one directory below the item pages.
	href := "../i/" + it.Msg.Short + ".html"
	meta := siteRowMeta(it, href)
	// A repost's author line names the verb, as socialCard in gs-render.js does.
	if t == "repost" {
		meta = append([]sitePageBit{meta[0], {Text: "reposted", Sep: " "}}, meta[1:]...)
	}
	state := pageItemField(it, "state")
	glyph, glyphClass := sitePageGlyph(t, state)
	row := siteRowHead(it)
	row.Avatar = sitePageAvatar(pageDisplayAuthor(it.Msg))
	row.Glyph, row.GlyphClass, row.GlyphTitle = glyph, glyphClass, sitePageGlyphTitle(t, state)
	row.Href, row.Meta = href, meta
	// The sub-issue count and the interaction counts follow the identity chips, as issueCard and chipRow in gs-render.js do.
	if counts.Sub > 0 {
		row.Chips = append(row.Chips, sitePageChip{Kind: "count", Class: "pm-sub-chip", Icon: "☐", Label: strconv.Itoa(counts.Sub) + " sub"})
	}
	counts.Replies = len(it.Replies)
	counts.Approved, counts.ChangesRequested = siteVerdicts(it.Replies)
	row.Chips = append(row.Chips, siteCountChips(counts)...)
	row.Chips = append(row.Chips, siteItemChips(it, subjects, types, href, counts.Issues)...)
	// A state word stands in for the edited marker, as leadMeta in gs-render.js does.
	if row.Chip != nil {
		row.Meta = siteBitsWithout(meta, "edited")
	}
	return row
}

// siteListSubjects names each standing root of a list by the subject of its newest version from its index entry, so both page passes name a reference alike. Mirrors indexSubject in gs-core.js.
func siteListSubjects(items []*sitePageItem) map[string]string {
	subjects := make(map[string]string, len(items))
	for _, it := range items {
		if subject := siteSubjectText(it.Resolved.Subject); subject != "" && !it.Retracted {
			subjects[it.Msg.Short] = subject
		}
	}
	return subjects
}

// siteRefTypes names the type of every root by its short hash, so a reference chip shows the glyph of its target. Mirrors indexType in gs-core.js.
func siteRefTypes(roots map[string][]*sitePageItem) map[string]string {
	types := map[string]string{}
	for _, list := range sitePageLists {
		for _, it := range roots[list.Ext] {
			types[it.Msg.Short] = pageItemType(it)
		}
	}
	return types
}

// siteBitsWithout returns the bits less those of one class.
func siteBitsWithout(bits []sitePageBit, class string) []sitePageBit {
	out := bits[:0:0]
	for _, b := range bits {
		if b.Class != class {
			out = append(out, b)
		}
	}
	return out
}

// sitePageNavItem is one entry of the shell's sidebar: its section, route, glyph and label.
type sitePageNavItem struct {
	Section, Route, Glyph, Label string
}

// sitePagesShellNav is the sidebar of the embedded shell, in its order and words; the page sidebar mirrors it.
var sitePagesShellNav = sitePagesReadShellNav()

// sitePagesShellNavRe matches one nav anchor of the shell: its route, its glyph and its label.
var sitePagesShellNavRe = regexp.MustCompile(`<a href="#(/[a-z]*)" data-nav="[a-z]+"[^>]*><span class="nav-icon">([^<]*)</span>([A-Za-z ]+)`)

// sitePagesShellSectionRe matches a section label of the shell's nav.
var sitePagesShellSectionRe = regexp.MustCompile(`<div class="nav-section">([^<]*)</div>`)

// sitePagesReadShellNav reads the sidebar of the embedded shell; a shell with no nav panics at init, like the favicon read.
func sitePagesReadShellNav() []sitePageNavItem {
	data, err := siteFiles.ReadFile("assets/index.html")
	if err != nil {
		panic("site: read embedded assets/index.html for the pages' sidebar: " + err.Error())
	}
	text := string(data)
	start, end := strings.Index(text, `<nav id="nav"`), strings.Index(text, "</nav>")
	if start < 0 || end < start {
		panic(`site: no <nav id="nav"> in the embedded assets/index.html: the generated pages have no sidebar`)
	}
	var items []sitePageNavItem
	section := ""
	for _, line := range strings.Split(text[start:end], "\n") {
		line = strings.TrimSpace(line)
		if m := sitePagesShellSectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]
		} else if line == "</div>" {
			section = ""
		} else if m := sitePagesShellNavRe.FindStringSubmatch(line); m != nil {
			items = append(items, sitePageNavItem{Section: section, Route: m[1], Glyph: m[2], Label: m[3]})
		}
	}
	return items
}

// sitePageSidebar builds a page's sidebar from the shell's: a destination with a generated page links the page, and every other links its route in the shell.
func sitePageSidebar(base, current string, files bool) []sitePageNavGroup {
	dirs := map[string]string{"/": ""}
	for _, l := range append(append([]sitePageList{}, sitePageLists...), siteCommitsList) {
		dirs[l.Route] = l.Dir
	}
	if files {
		dirs[siteFilesList.Route] = siteFilesList.Dir
	}
	var groups []sitePageNavGroup
	for _, item := range sitePagesShellNav {
		link := sitePageNavLink{Label: item.Label, Glyph: item.Glyph}
		if dir, ok := dirs[item.Route]; !ok {
			link.Href, link.App = base+"index.html#"+item.Route, true
		} else if dir == "" {
			link.Href, link.Current = base+"index.html", current == ""
		} else {
			link.Href, link.Current = base+dir+"/index.html", dir == current
		}
		if n := len(groups); n == 0 || groups[n-1].Section != item.Section {
			groups = append(groups, sitePageNavGroup{Section: item.Section})
		}
		groups[len(groups)-1].Links = append(groups[len(groups)-1].Links, link)
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
	paras := sitePageParaLines(body)
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
