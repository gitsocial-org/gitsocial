// site_pages_html.go - templates and styling for the static HTML pages: the
// shared head (meta/OG/canonical/PE hooks), the item/list/front templates, the
// two-layer CSS (tiny inline base + pages.css), and the presentation builders
// (chips, meta lines, paragraphs, description extraction).
//
// Everything renders through html/template so every subject/body/author/header
// value — all attacker-controlled — is context-escaped. Exactly two values are
// typed, and both are narrow by construction:
//
//   - The favicon href (template.URL, since html/template rewrites any data: URI
//     in a URL attribute to a failsafe): either this package's own shell
//     constant or a data URI already narrowed to an image type by
//     ValidSiteFavicon.
//   - The front page's README (template.HTML, siteFrontReadme.HTML): markup
//     produced by THIS package's renderer, site_markdown.go, which builds every
//     tag itself from a parsed block/span tree and escapes every text node,
//     attribute value and code body with html.EscapeString. Raw HTML embedded in
//     the source is never passed through: it is lexed and REBUILT against an
//     element/attribute allowlist that has no event handlers, no style, no
//     script/iframe/object, and no image or link target that is not an absolute
//     https/mailto/in-page/app reference. The tier matters: this is the bucket
//     owner's own repo README, so the allowlist is hygiene, not an adversarial
//     boundary. ITEM BODIES ARE THIRD-PARTY AND STAY ESCAPED PLAIN TEXT
//     (sitePageParas) — do not route them through here without redoing that
//     threat model.
//
// The visual spec is the app's own sheet, site/pages-app.css (plus the
// doctrines in its comments): the generated pages converge toward the app's
// treatment, never the other way. (The .local/lite/ prototypes this layer
// started from are superseded — see .local/lite/DEPRECATED.md.)

package objstore

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
)

const (
	// sitePagesCSSKey is the pages' shared stylesheet, their only subresource.
	sitePagesCSSKey = "pages.css"
	// sitePagesFrontKey is the front page's bucket key. Since the entry flip
	// the generated front page (the app's home landing + PE hooks +
	// gs-upgrade.js) IS index.html — the pages maintainer owns index.html
	// whenever the page layer is effective, and uploadSiteFiles owns the embedded
	// shell index.html only when it is not. The old timeline.html key was retired on the flip (it never
	// deployed to production, so URLs-are-forever does not bind).
	sitePagesFrontKey = "index.html"
	// sitePagesLegacyFrontKey is the pre-flip front-page key, swept on every push
	// so a bucket first pushed by an older binary (which wrote timeline.html) does
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
//
// The `.gs-boot` rules are the page's boot state, and NOTHING is hidden until a
// script adds that class to <html> (sitePagesBootScript). Hidden-by-default
// content revealed by script is cloaking and breaks the no-JS contract this
// layer exists for, so the served document is complete and visible for a
// crawler, a text browser, and anyone with scripting off. On a JS client the
// class lands in the head, before the body is parsed, so the static content
// never reaches a painted frame and the visitor sees only the loading line —
// styled to match the app's own `.loading` (muted, centered, 2.5rem of air) so
// the boot and the app that follows speak the same visual language.
//
// The geometry is the app's, not a reading column of its own, and that is the
// point: on the front page the served document stays on screen while the shell
// downloads and is then swapped for the app's render of the same content, so any
// metric the two disagree on becomes a visible jump at the swap. The shell's
// numbers (pages-app.css: --nav-w 220px, --shell-max 1012px, --shell-pad 1.25rem,
// --nav-gap 1.5rem, 20px/1.4 body, the 720px breakpoint) are mirrored here so the
// content column lands at the same width and the same x, and the page's own nav
// occupies the sidebar's column rather than a placeholder standing in for it.
// EB Garamond ships in the shell (fonts/eb-garamond.woff2) and both pages.css
// and pages-app.css declare the same face, so the two sides load the same font;
// the inline base names only Georgia because it must read decently when
// pages.css (and with it the @font-face) never arrives, so size and leading are
// matched against the shared fallback.
//
// Nav and content are siblings under #gs-page (there is no wrapper element to
// make them two columns of a grid), so the sidebar column is RESERVED as left
// padding and the nav is positioned into it, out of flow. A grid was the obvious
// shape and the wrong one: with the content's rows all implicit, `grid-row:1/-1`
// on the nav resolves -1 against an explicit grid that has no rows, so the nav
// stays in row one and makes that row as tall as the whole link stack, leaving a
// tall gap under the first heading. Out of flow, the nav cannot size a row.
//
// border-box is not cosmetic here, it is what makes the shared 1012px mean the
// same thing on both sides. pages-app.css resets it globally, so the shell's
// max-width INCLUDES its padding; without the same reset the page's max-width
// would bound the content box and the reserved gutter would be added outside it,
// leaving the served page 284px wider than the app's shell and therefore
// centered 142px to its left, with the text wrapping at a different measure.
//
// A configured accent and a stored theme choice never reach this layer: it
// keeps the stock teal and the system palette on purpose. pages.css — which
// carries the baked-in accent and the stored-theme gates — overrides both on
// load, so the hardcoded values here are the documented degraded state a page
// falls back to only when that stylesheet never arrives.
const sitePagesInlineCSS = `*{box-sizing:border-box}body{margin:0;background:#f8eed5;color:#1a1a1a;font:20px/1.4 Georgia,serif}a{color:#008787}#gs-page{position:relative;max-width:1012px;margin:0 auto;padding:1.25rem 1.25rem 2rem calc(1.25rem + 220px + 1.5rem)}#gs-page>nav{position:absolute;left:1.25rem;top:1.25rem;width:220px;display:flex;flex-direction:column;align-items:flex-start;gap:.35rem}html.gs-boot #gs-page{display:none}html.gs-boot body::before{content:"Loading…";display:block;padding:2.5rem 0;text-align:center;color:#6f6552}@media (max-width:720px){#gs-page{padding:.75rem}#gs-page>nav{position:static;width:auto;flex-direction:row;flex-wrap:wrap;gap:.9rem;margin-bottom:.6rem}}@media (prefers-color-scheme:dark){body{background:#02041b;color:#c9d1d9}a{color:#00d7d7}html.gs-boot body::before{color:#7d8590}}`

// sitePagesBootScript is the one inline script every page carries. It runs
// synchronously in the head — before the body is parsed, so before the browser
// can paint any static content — and marks the document as booting, which is
// what activates the `.gs-boot` rules above. A client that runs no scripts never
// reaches it and therefore never hides anything.
//
// It also owns the failsafe for the case gs-upgrade.js cannot: the upgrade
// script 404ing, being blocked, or failing to parse means nothing downstream is
// left to undo the hide, and a page hidden with no one to reveal it is the blank
// page this whole mechanism must never produce. So the hide un-does itself on
// the load event (defer scripts run before it, so an upgrade that was going to
// take over already has) and, as a backstop for a document whose load event
// never fires, on a timer. Both defer to __gsBooting, the flag gs-upgrade.js
// sets when it takes ownership and starts its own watchdog.
// The cloak is CONDITIONAL, and the condition is whether the app is about to
// show something this page does not already show. On the front page it is not:
// since the README is pre-rendered (site_markdown.go) the served document is the
// same landing the home route renders, so hiding it buys a loading line in place
// of a finished page and costs the visitor the whole shell download before they
// can read anything (~149 KB of the front page's ~182 KB, seconds on 4G). A
// fragment naming a different route is the exception: a deep link IS about to
// replace the content, so it cloaks as every other page does.
//
// The page's own route comes from the gs-route meta, which the head emits above
// this script. The fragment test mirrors the route grammar's two shapes (a path
// route past "/", or a <reftype>:<value> route) rather than restating parseRoute:
// anything else is a bare anchor, which rides along on the page's own route.
//
// It also stamps the app's stored theme choice on <html> before first paint:
// the shell's theme toggle persists localStorage "theme" as "light-mode"/
// "dark-mode" (site/index.html, gs-upgrade.js), and pages.css gates its dark
// palette on exactly these classes with prefers-color-scheme as the no-choice
// fallback — so a visitor who picked a theme in the app gets the pages in that
// theme with no flip at boot, and a no-JS client keeps the system theme.
const sitePagesBootScript = `<script>(function(d,w){var e=d.documentElement;` +
	`try{var t=w.localStorage.getItem("theme");if(t==="dark-mode"||t==="light-mode")e.classList.add(t)}catch(x){}` +
	`var m=d.querySelector('meta[name="gs-route"]');var r=m?m.getAttribute("content"):"";` +
	`var h=w.location.hash||"";var deep=/^#\/.+/.test(h)||/^#[a-z][a-z-]*:/.test(h);` +
	`if(r!=="/"||deep)e.classList.add("gs-boot");` +
	`function u(){if(!w.__gsBooting)e.classList.remove("gs-boot")}` +
	`setTimeout(u,10000);w.addEventListener("load",u)})(document,window)</script>`

// sitePagesDarkPalette is the pages' dark token set, stated once and spliced
// into BOTH dark gates below (the stored-choice class and the media fallback)
// so no value is maintained twice. The @ACCENT_DARK@ placeholder is the dark
// accent slot sitePagesCSSFor fills.
const sitePagesDarkPalette = `--bg:#02041b;--text:#c9d1d9;--link:@ACCENT_DARK@;--muted:#7d8590;--line:#1e2445;--panel:#080a21;--pre-bg:#0a0d26`

// sitePagesCSSTemplate is the full look (chips, sections, thread styling,
// lists, nav, pre), served once as pages.css and shared by every page. It also
// delivers the webfonts (SIL OFL 1.1) — EB Garamond as one variable file, IBM
// Plex Mono as four static weights a browser downloads only when a page renders
// them: the @font-face rules and the families live here rather than in the
// inline base so a page whose pages.css fetch fails keeps reading in the system
// fallbacks, and the shell's pages-app.css declares the identical faces — both
// sheets serve from the prefix root, so one relative URL names one font object
// and the browser fetches it once.
//
// The palette resolves through custom properties on :root so one token set
// carries three concerns at once: the configured accent is baked into --link at
// push time (sitePagesCSSFor), the dark palette exists once (@DARK@ splices
// sitePagesDarkPalette into both gates), and every derived tint follows via
// color-mix exactly as the app's tokens do. Dark is gated the way the app's own
// choice works: the boot script stamps the stored theme (localStorage "theme")
// on <html>, html.dark-mode forces dark, html.light-mode forces light, and
// prefers-color-scheme decides only when no choice is stamped
// (html:not(.light-mode)) — the no-JS fallback.
//
// The chip classes are the app's own vocabulary (pages-app.css): .chip.state
// fills, the milestone/sprint lifecycle fills (WCAG-AA solid hexes, per the
// app's comment), .chip.reviewer-chip verdict tints, .chip.chip-retracted —
// the builders below emit these, never a pages-only class.
const sitePagesCSSTemplate = `@font-face{font-family:'EB Garamond';font-style:normal;font-weight:400 800;font-display:swap;src:url('fonts/eb-garamond.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-style:normal;font-weight:400;font-display:swap;src:url('fonts/ibm-plex-mono.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-style:normal;font-weight:500;font-display:swap;src:url('fonts/ibm-plex-mono-medium.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-style:normal;font-weight:600;font-display:swap;src:url('fonts/ibm-plex-mono-semibold.woff2') format('woff2')}
@font-face{font-family:'IBM Plex Mono';font-style:normal;font-weight:700;font-display:swap;src:url('fonts/ibm-plex-mono-bold.woff2') format('woff2')}
:root{--bg:#f8eed5;--text:#1a1a1a;--link:@ACCENT@;--muted:#6f6552;--line:#d8cbaa;--panel:#f1e8cf;--pre-bg:#f2e5c6;--open:#1f9d55;--closed:#8957e5;--merged:#8250df;--warn:#bf8700;--danger:#cf222e;--chip:color-mix(in srgb,var(--text) 9%,transparent);--card-line:color-mix(in srgb,var(--text) 18%,transparent)}
html.dark-mode{@DARK@}
@media (prefers-color-scheme:dark){html:not(.light-mode){@DARK@}}
body{font-family:'EB Garamond',Georgia,serif;background:var(--bg);color:var(--text)}
a{color:var(--link)}
h1{font-size:1.6rem;line-height:1.25;margin:.3rem 0 .2rem}
h2{font-size:1.1rem;line-height:1.25;margin:.2rem 0 .4rem}
nav,.chip,pre,code,footer,.show-more{font-family:'IBM Plex Mono','SF Mono',Consolas,monospace}
nav,footer{font-size:.72rem}
nav a,nav b,footer a{margin-right:.9rem}
#gs-page>nav a,#gs-page>nav b{margin-right:0}
@media (max-width:720px){#gs-page>nav a,#gs-page>nav b{margin-right:.9rem}}
.meta{font-size:.8rem;color:var(--muted)}
.chip{display:inline-block;background:var(--chip);border-radius:999px;padding:.05rem .5rem;font-size:.8rem;font-weight:500;color:var(--muted);white-space:nowrap}
.chip.state{color:#fff}
.chip.open{background:var(--open)}
.chip.closed{background:var(--closed)}
.chip.merged{background:var(--merged)}
.chip.pre{background:var(--warn)}
.chip.active{background:#1f6feb}
.chip.completed{background:var(--open)}
.chip.planned{background:#6b7280}
.chip.canceled,.chip.unknown{background:#6e7681}
.chip.reviewer-chip{color:var(--text)}
.chip.reviewer-chip.fb-approved{background:color-mix(in srgb,var(--open) 20%,transparent)}
.chip.reviewer-chip.fb-changes-requested{background:color-mix(in srgb,var(--danger) 20%,transparent)}
.chip.chip-retracted{background:color-mix(in srgb,var(--danger) 20%,transparent);color:var(--text)}
.tomb{color:var(--muted);font-style:italic}
section{border-top:1px solid var(--line);margin-top:1.4rem;padding-top:.9rem}
p{margin:.7rem 0}
pre{font-size:.72rem;line-height:1.45;overflow-x:auto;background:var(--pre-bg);padding:.7rem;border:1px solid var(--line)}
ul.files{list-style:none;padding:0;margin:1rem 0;font-family:'IBM Plex Mono','SF Mono',Consolas,monospace;font-size:.8rem}
ul.files li{border-top:1px solid var(--line);padding:.4rem 0}
.card{background:var(--panel);border:1px solid var(--card-line);border-radius:10px;padding:.75rem .9rem;margin:0 0 .7rem;transition:border-color .12s ease,background .12s ease}
.card:hover{border-color:color-mix(in srgb,var(--link) 45%,var(--card-line));background:var(--chip)}
.card-head{display:flex;flex-wrap:wrap;align-items:baseline;gap:.3rem .4rem}
.card-head .subject{flex:1 1 0;min-width:0;font-weight:600}
.card .meta{display:block;margin-top:.25rem;margin-left:1.75rem}
.show-more{display:flex;flex-direction:column;align-items:center;gap:.1rem;width:100%;margin-top:.5rem;padding:.35rem;font-size:.8rem;color:var(--muted);text-decoration:none}
.show-more:hover{color:var(--text);text-decoration:none}
.type-glyph{display:inline-block;width:1.1em;text-align:center;line-height:1;vertical-align:middle;font-family:'IBM Plex Mono','SF Mono',Consolas,monospace;color:var(--muted)}
.type-glyph.tg-open{color:var(--open)}
.type-glyph.tg-closed{color:var(--closed)}
.type-glyph.tg-merged{color:var(--merged)}
footer{margin-top:2.2rem;border-top:1px solid var(--line);padding-top:.8rem}
.markdown{word-break:break-word}
.markdown h1,.markdown h2,.markdown h3,.markdown h4{margin:1rem 0 .5rem;line-height:1.2}
.markdown ul,.markdown ol{margin:.6rem 0 .6rem 1.4rem;padding:0}
.markdown li{margin:.2rem 0}
.markdown li.task{list-style:none;margin-left:-1.1rem}
.markdown img{max-width:100%;height:auto}
.markdown hr{border:0;border-top:1px solid var(--line)}
.markdown blockquote{margin:.6rem 0;padding:0 .8rem;border-left:3px solid var(--line);color:var(--muted)}
.markdown table{border-collapse:collapse;display:block;overflow-x:auto;max-width:100%;margin:.8rem 0;font-size:.85rem}
.markdown th,.markdown td{border:1px solid var(--line);padding:.3rem .6rem;text-align:left}
.markdown div[align=center],.markdown center{text-align:center}
`

// sitePagesCSS is the pages' stylesheet template with the dark palette spliced
// (still carrying the @ACCENT@/@ACCENT_DARK@ slots): the layer's stable
// identity, which is what the shell version hash covers — the accent is site
// DATA, filled per push by sitePagesCSSFor, never part of the binary identity.
var sitePagesCSS = strings.ReplaceAll(sitePagesCSSTemplate, "@DARK@", sitePagesDarkPalette)

// sitePagesAccent / sitePagesAccentDark are the pages' stock teal accents (the
// app's own :root/--link values), standing in when the site config sets none.
const (
	sitePagesAccent     = "#008787"
	sitePagesAccentDark = "#00d7d7"
)

// sitePagesCSSFor renders pages.css with the configured accent baked in at push
// time, mirroring the app's applyAccent (gs-app.js) exactly: a configured
// accent tints both themes, accentDark — when set — tints the dark theme
// separately, and no configuration leaves the stock teals. cfg's fields are
// already validated (readSiteCustomization drops malformed values), so the
// spliced strings are strict hex colors, never free text.
func sitePagesCSSFor(cfg siteCustomization) []byte {
	light, dark := sitePagesAccent, sitePagesAccentDark
	if cfg.Accent != "" {
		light, dark = cfg.Accent, cfg.Accent
	}
	if cfg.AccentDark != "" {
		dark = cfg.AccentDark
	}
	return []byte(strings.NewReplacer("@ACCENT@", light, "@ACCENT_DARK@", dark).Replace(sitePagesCSS))
}

// sitePageTemplateText is the full template set. The shared "head" stamps the
// common metadata plus the PE hooks (gs-route meta, the #gs-page mount div with
// its data-base attribute) — inert plain HTML until the gs-upgrade.js boot
// adopts them. The
// @BASE@ placeholder is spliced with the inline CSS constant before parsing so
// both layers emit from this file's constants.
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
<meta property="og:title" content="{{.OGTitle}}">
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
<style>@BASE@</style>
@BOOT@
<link rel="stylesheet" href="{{.Base}}pages.css">
<script defer src="{{.Base}}gs-upgrade.js"></script>
</head>
<body>
<div id="gs-page" data-base="{{.Base}}">
{{end}}{{define "foot"}}</div>
</body>
</html>
{{end}}{{define "chip"}}<span class="chip{{if .Class}} {{.Class}}{{end}}">{{.Label}}</span>{{end}}{{define "metaline"}}<p class="meta">{{if .Chip}}{{template "chip" .Chip}} {{end}}{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</p>{{end}}{{define "paras"}}{{range .}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>
{{end}}{{end}}{{define "entries"}}{{range .}}<div class="card"{{if .ID}} id="{{.ID}}"{{end}}><div class="card-head">{{if .Chip}}{{template "chip" .Chip}} {{end}}<a class="subject" href="{{.Href}}">{{.Title}}</a></div>
<span class="meta">{{range $i, $b := .Meta}}{{if $i}} · {{end}}{{$b}}{{end}}</span></div>
{{end}}{{end}}{{define "item"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a> <a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a></nav>

<h1>{{.Subject}}</h1>
{{template "metaline" .}}
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "paras" .Paras}}{{end}}{{range .Sections}}<section>
{{if .Tomb}}<p class="tomb meta">{{.Tomb}}</p>
{{else}}{{template "metaline" .}}
{{if .Pre}}<pre>{{.Pre}}</pre>
{{end}}{{template "paras" .Paras}}{{end}}</section>
{{end}}{{if .Omitted}}<section><p class="meta">… truncated — {{.Omitted}} more replies in the thread</p></section>
{{end}}<footer><a href="{{.Chrome.Base}}{{.ListDir}}/index.html">← {{.ListLabel}}</a> <a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "list"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a> <a href="{{.Chrome.Base}}index.html">home</a>{{range .Nav}} {{if .Current}}<b>{{.Label}}</b>{{else}}<a href="{{.Href}}">{{.Label}}</a>{{end}}{{end}}</nav>

<h1>{{.Heading}}</h1>
<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{if .Entries}}{{template "entries" .Entries}}{{else}}<p class="meta">nothing here yet</p>
{{end}}<footer>{{if .NewerHref}}<a href="{{.NewerHref}}">← newer</a> {{end}}{{if .OlderHref}}<a href="{{.OlderHref}}">older →</a> {{end}}<a href="{{.Chrome.Base}}index.html">home</a></footer>
{{template "foot"}}{{end}}{{define "front"}}{{template "head" .Chrome}}<nav><a href="{{.Chrome.Base}}index.html"><b>{{.Chrome.SiteTitle}}</b></a>{{range .Nav}} <a href="{{.Href}}">{{.Label}}</a>{{end}}</nav>

<h1>{{.Heading}}</h1>
{{if .MetaBits}}<p class="meta">{{range $i, $b := .MetaBits}}{{if $i}} · {{end}}{{$b}}{{end}}</p>
{{end}}{{with .Home}}{{if .Branch}}<p class="meta"><span class="chip">{{.Branch}}</span> <a class="chip" href="{{.BranchesHref}}">{{.Branches}}</a>{{with .Latest}} {{.Subject}} · {{.Date}} · <a href="{{.Href}}">{{.Short}}</a>{{end}}</p>
{{end}}{{if .Files}}<ul class="files">
{{range .Files}}<li><a href="{{.Href}}">{{.Name}}</a></li>
{{end}}</ul>
{{if .MoreHref}}<p class="meta"><a href="{{.MoreHref}}">{{.MoreLabel}}</a></p>
{{end}}{{end}}{{if .Readme}}<section><p class="meta">README</p>
{{.Readme.HTML}}{{if .Readme.Truncated}}<p class="meta">… truncated — full README in the repository</p>
{{end}}</section>
{{end}}{{end}}{{if .Activity}}<section><h2>Recent activity</h2>
{{range .Activity}}<div class="card"><div class="card-head">{{if .Glyph}}<span class="type-glyph {{.GlyphClass}}" title="{{.GlyphTitle}}">{{.Glyph}}</span> {{end}}{{if .Type}}<span class="chip">{{.Type}}</span> {{end}}<a class="subject" href="{{.Href}}">{{.Subject}}</a></div>
<span class="meta">{{.Author}} · {{.Date}}{{if .Sha}} · {{.Sha}}{{end}}</span></div>
{{end}}{{if .ActivityMoreHref}}<a class="show-more" href="{{.ActivityMoreHref}}"><span class="show-more-icon">⌄</span><span class="show-more-label">{{.ActivityMoreLabel}}</span></a>
{{end}}</section>
{{end}}<footer>{{range .Nav}}<a href="{{.Href}}">{{.Label}}</a> {{end}}</footer>
{{template "foot"}}{{end}}`

// sitePageTemplates is the parsed page template set, with the inline base CSS
// and the boot script spliced in (both constants, never user content).
var sitePageTemplates = template.Must(template.New("pages").Parse(
	strings.NewReplacer("@BASE@", sitePagesInlineCSS, "@BOOT@", sitePagesBootScript).Replace(sitePageTemplateText)))

// sitePagesInlineIconMax bounds a configured favicon the page layer inlines.
// The shell stamps its head once; the page layer stamps one head per page, so a
// large data URI is multiplied by the page count both in the bucket and on
// every visit. Past this the pages carry the shell's default mark and the
// upgrade layer swaps the configured icon in on boot, exactly as in the shell.
const sitePagesInlineIconMax = 2048

// sitePagesShellIconRe pulls the <link rel="icon"> href out of the shell.
var sitePagesShellIconRe = regexp.MustCompile(`<link rel="icon" href="([^"]*)"`)

// sitePagesDefaultIcon is the shell's own favicon href, read from the embedded
// site/index.html so the generated pages and the SPA cannot drift apart. It is
// a data: URI, which is the point: a page that declares no icon makes the
// browser request /favicon.ico at the ORIGIN root, and under a bucket prefix
// that is a key the site does not own and can never serve. An inlined icon
// resolves with no request and no bucket object at all.
var sitePagesDefaultIcon = sitePagesShellIcon()

// sitePagesShellIcon extracts the shell's favicon href from the embedded shell.
// The match is byte-exact on the shell's own markup, so a reformat of that
// <link> tag panics at package init (template.Must-style) rather than shipping
// every generated page with no favicon — the silent regression to the
// /favicon.ico 404 that the icon exists to prevent, which no runtime signal
// would ever report.
func sitePagesShellIcon() template.URL {
	data, err := siteFiles.ReadFile("site/index.html")
	if err != nil {
		panic("objstore: read embedded site/index.html for the pages' default favicon: " + err.Error())
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
	Title         string // full <title> (subject · site title)
	Icon          template.URL
	Description   string // meta/OG description, whitespace-collapsed, ~160 chars
	OGTitle       string // og:title (the bare subject)
	SiteTitle     string
	Canonical     string // absolute self URL from site.url
	Route         string // gs-route content, in the shell's parseRoute grammar
	Base          string // relative path from this page to the site root ("./" or "../")
	Image         string // absolute og:image/twitter:image URL ("" = no card, twitter:card stays "summary")
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

// sitePageListEntry is one row on a list or front page. ID, when set, is the
// row's own anchor (`c-<sha12>` on the commits list), which is what gives a row
// a citable URL of its own; item rows leave it empty because the item already
// has a page.
type sitePageListEntry struct {
	ID    string
	Chip  *sitePageChip
	Href  string
	Title string
	Meta  []string
}

// siteListPageData feeds the "list" template.
type siteListPageData struct {
	Chrome    sitePageChrome
	Nav       []sitePageNavLink
	Heading   string
	MetaBits  []string
	Entries   []sitePageListEntry
	NewerHref string
	OlderHref string
}

// siteFrontPageData feeds the "front" template (index.html).
type siteFrontPageData struct {
	Chrome            sitePageChrome
	Nav               []sitePageNavLink
	Heading           string
	MetaBits          []string
	Home              *siteFrontHome
	Activity          []sitePageActivityRow
	ActivityMoreHref  string // crawlable destination for the section's trailing link ("" — no rows)
	ActivityMoreLabel string // its label, shared with the app's control (siteActivityMoreLabel)
}

// sitePageActivityRow is one row of the front page's recent-activity section:
// the metadata the items index already carries, linking to the item's own
// crawlable page. The app's homeActivity renders the same fields in the same
// order (gs-render.js homeActivityRow), as the same card — glyph, type chip,
// subject, meta — which the no-JS page can carry verbatim because the type
// glyphs are plain text characters.
type sitePageActivityRow struct {
	// Type is the row's type label, rendered as a chip on the item rows whose
	// glyph alone does not say which kind they are. A code commit leaves it
	// empty: its glyph is already the commit glyph, titled "commit", so a chip
	// reading "commit" beside it says the same thing twice, and the row reads as
	// the commit card it is everywhere else in the app.
	Type    string
	Href    string
	Subject string
	Author  string
	Date    string
	// Sha is the commit's short sha, appended to a code row's meta so the row
	// carries the commit's identity the way the commit card does. Empty on item
	// rows, which are identified by the item page they link to.
	Sha        string
	Glyph      string // leading type glyph ("" — this type has none)
	GlyphClass string // its tint class (tg-open/tg-closed/tg-merged, else tg-<class type>)
	GlyphTitle string // the glyph's title attribute: the class type (sitePageGlyphClassType)
}

// siteFrontHome is the front page's body: the repo landing the booted app
// renders on the home route (gs-render.js homeView) — the default branch strip,
// the root file listing, then the README. The two must show the same content in
// the same order: index.html is dual-owned and the page-entry upgrade replaces
// this body with the app's own home render, so any disagreement is a visible
// swap on first load.
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

// siteFrontReadme is the front page's README section: the default branch's
// README.md rendered by this package's own markdown renderer
// (site_markdown.go), capped with a truncation marker. HTML is the one typed
// value on a page body — see this file's header for what that trusts.
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
// shell route — used by the front page's branch/commit/file links (none of those
// get pages, so they deep-link into the app, which gs-upgrade.js boots with the
// hash winning over the page's own gs-route).
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

// sitePageTypeGlyph maps an item type to the compact leading glyph the app's
// cards show (gs-core.js TYPE_GLYPH). Issues vary by state and are resolved in
// sitePageGlyph. The glyphs are plain text, so the no-JS page carries exactly
// the characters the upgraded render paints.
var sitePageTypeGlyph = map[string]string{
	"post": "•", "comment": "↩", "repost": "↻", "quote": "↻",
	"milestone": "◇", "sprint": "◷", "pull-request": "⑂", "feedback": "↩",
	"release": "⏏", "memo": "☞", "commit": "◦",
}

// sitePageGlyph returns a type's glyph and its tint class, mirroring
// gs-render.js typeGlyphEl: a state-bearing type (issue, pull request) is tinted
// by state, every other type falls through to the single muted tg-<type> class.
// An unknown type has no glyph, exactly as typeGlyph returns "".
//
// The two arguments are deliberately distinct, because the JS derives them from
// different fallbacks and the static page must agree with the booted app row for
// row: itemType (the header type, else the extension's DEFAULT TYPE — typeGlyph)
// picks the character, while classType (the header type, else the extension
// NAME — typeGlyphEl's own `h.type || ext`) picks the class and the title.
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

// sitePageGlyphClassType returns the type the app tints and titles an item's
// glyph by (gs-render.js typeGlyphEl's `h.type || ext`): the item's own header
// type, else the source EXTENSION NAME — not the extension's default type, which
// is what picks the glyph character instead.
func sitePageGlyphClassType(it *sitePageItem) string {
	if t := pageHeaderField(it.Msg, "type"); t != "" {
		return t
	}
	if t := pageHeaderField(it.Resolved, "type"); t != "" {
		return t
	}
	return it.Msg.Ext
}

// siteActivityMoreLabel labels the recent-activity section's trailing link on
// both surfaces (gs-render.js homeActivityMore uses the same words).
const siteActivityMoreLabel = "See more"

// siteActivityMoreKey is that link's crawlable destination: the social posts
// archive, which is the served page for the app's /timeline route (gs-upgrade.js
// hashForPath), so the link is a real object for a crawler and the timeline view
// for a visitor whose upgrade booted.
const siteActivityMoreKey = "./posts/index.html"

// sitePageStateClass maps a workflow state to its chip color class. A cancel
// state is closed in BOTH spellings, matching gs-render.js glyphStateClass:
// GITPM.md words the milestone/sprint state with a doubled l, the issue/review
// paths with one. Matched by prefix because the repo's misspell linter rewrites
// the doubled-l literal on sight, so it cannot be written as a case here.
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

// sitePageChipStateClass maps a workflow state to the app's solid-fill chip
// class, mirroring gs-render.js stateChip's map exactly (every known state gets
// a fill so the white .chip.state text stays legible; anything else falls to
// the slate "unknown" fill). A cancel state is matched by prefix for the same
// misspell-linter reason as sitePageStateClass.
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

// sitePageItemChip returns an item's leading state chip (nil when its type
// carries none), in the app's chip class vocabulary (gs-render.js stateChip and
// the release/retracted chips), which sitePagesCSS styles to the app's
// treatment. A draft PR is the app's plain unclassed chip.
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
// plain comment). The classes are the app's reviewer-chip verdict vocabulary
// (.chip.reviewer-chip.fb-*) — its closest equivalent: the app has no bare
// approved/changes chip of its own (verdicts ride reviewer chips and .fb-card
// borders), so the pages borrow the reviewer-chip tint for the same states.
func sitePageFeedbackChip(r *sitePageItem) *sitePageChip {
	switch pageItemField(r, "review-state") {
	case "approved":
		return &sitePageChip{Class: "reviewer-chip fb-approved", Label: "approved"}
	case "changes-requested":
		return &sitePageChip{Class: "reviewer-chip fb-changes-requested", Label: "changes requested"}
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
		Image:       site.Image,
		Icon:        site.Icon,
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

// siteFrontActivityEntry pairs a rendered activity row with its sort key.
type siteFrontActivityEntry struct {
	row sitePageActivityRow
	ts  int64
	sha string
}

// buildSiteFrontActivity projects the newest top-level items across the item
// roots (memo excluded, mirroring the app's data branches) INTERLEAVED with the
// newest code commits, into the front page's recent-activity rows: subject,
// type, author and time, all of it metadata the indexes already carry, so no
// body or object is read. Without the code interleave the section reports only
// what a repo happens to publish as gitmsg items, which on a release-heavy repo
// is a months-old release list standing in for a project that commits daily.
// Item rows link to their own crawlable page; code commits have none, so those
// link into the app. The app's loadHomeActivity mirrors this selection, cap and
// order, so the upgrade re-renders the same rows.
func buildSiteFrontActivity(roots map[string][]*sitePageItem, done map[string]int, code []siteMetaEntry, site sitePageSite) []sitePageActivityRow {
	var merged []siteFrontActivityEntry
	for _, e := range code {
		short := e.SHA
		if len(short) > 12 {
			short = short[:12]
		}
		glyph, glyphClass := sitePageGlyph("commit", "commit", "")
		row := sitePageActivityRow{
			Href:       sitePageAppURL(site, "commit:"+short+"@"+e.Branch),
			Subject:    e.Subject,
			Author:     e.Author,
			Date:       sitePageDate(e.TS),
			Sha:        short,
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
			name, _ := pageDisplayAuthor(it.Msg)
			itemType := pageItemType(it)
			classType := sitePageGlyphClassType(it)
			glyph, glyphClass := sitePageGlyph(itemType, classType, pageItemField(it, "state"))
			row := sitePageActivityRow{
				Type:       sitePageTypeLabel(itemType),
				Href:       "./i/" + it.Msg.Short + ".html",
				Subject:    subject,
				Author:     name,
				Date:       sitePageDate(pageEffectiveTime(it.Msg)),
				Glyph:      glyph,
				GlyphClass: glyphClass,
				GlyphTitle: classType,
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
	rows := make([]sitePageActivityRow, 0, len(merged))
	for _, m := range merged {
		rows = append(rows, m.row)
	}
	return rows
}

// sitePageNav builds the list nav links for a list/front page. base is the
// page's relative path to the site root; current bolds that list's link. The
// commits list closes the row: it is the newest surface and the only one whose
// rows are code rather than gitmsg items, so it reads as an addendum to the five
// type dirs rather than one of them.
func sitePageNav(base, current string) []sitePageNavLink {
	nav := make([]sitePageNavLink, 0, len(sitePageLists)+1)
	for _, l := range append(append([]sitePageList{}, sitePageLists...), siteCommitsList) {
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

// buildSiteSitemapListEntries collects the type-list index pages' sitemap
// entries with <lastmod> = the type's latest item activity ("" for an empty
// type). These stay out of buildSiteSitemapEntries: their positions would shift
// as items append, so they ride the rewritten head part (or the single urlset),
// never a sealed one.
func buildSiteSitemapListEntries(roots map[string][]*sitePageItem, done map[string]int, site sitePageSite) []siteSitemapEntry {
	entries := make([]siteSitemapEntry, 0, len(sitePageLists))
	for _, list := range sitePageLists {
		var newest int64
		for _, it := range roots[list.Ext][:done[list.Ext]] {
			if t := sitePageLastActivity(it); t > newest {
				newest = t
			}
		}
		lastmod := ""
		if newest > 0 {
			lastmod = sitePageDate(newest)
		}
		entries = append(entries, siteSitemapEntry{loc: site.URL + list.Dir + "/index.html", lastmod: lastmod})
	}
	return entries
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
// the tail) plus the rewritten sitemap-head.xml newest part. The type-list
// index pages and the commits list (head + every sealed page) ride the single
// urlset / head part only (see buildSiteSitemapListEntries); the part math runs
// on root + items alone.
func writeSiteSitemap(client *Client, prefix string, roots map[string][]*sitePageItem, done map[string]int, site sitePageSite, commits *siteCommitsState) error {
	entries := buildSiteSitemapEntries(roots, done, site)
	lists := append(buildSiteSitemapListEntries(roots, done, site), buildSiteCommitsSitemapEntries(commits, site)...)
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

// selectSiteFeedItems picks one feed's item set: the newest sitePagesFeedSize
// non-retracted top-level items of the given extensions (code commits absent —
// they have no item page to link), newest-first by (effective time, sha).
// Selection runs before body attachment so bodies are never fetched for items
// the cap drops.
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
