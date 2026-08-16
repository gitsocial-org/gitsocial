// site_pages_tokens_test.go - Drift guards for the two-sheet CSS split: the
// inlined pages-core.css base and the linked pages-full.css must stay
// consistent by construction.

package objstore

import (
	"regexp"
	"strings"
	"testing"
)

// sitePagesTestReadCSS reads one embedded stylesheet with comments stripped.
func sitePagesTestReadCSS(t *testing.T, name string) string {
	t.Helper()
	data, err := siteFiles.ReadFile("site/" + name)
	if err != nil {
		t.Fatalf("read embedded site/%s: %v", name, err)
	}
	return sitePagesStripCSSComments(string(data))
}

// TestSitePagesCoreHasNoURL pins the core sheet's hard rule: it is inlined
// into pages at arbitrary directory depths, where a relative url() would
// resolve against the page instead of the site root, so it must carry none
// (fonts, the only url() consumers, live in pages-full.css).
func TestSitePagesCoreHasNoURL(t *testing.T) {
	if strings.Contains(strings.ToLower(sitePagesTestReadCSS(t, "pages-core.css")), "url(") {
		t.Error("pages-core.css contains url(): it is inlined at arbitrary page depths where relative URLs break")
	}
	if strings.Contains(strings.ToLower(sitePagesCoreCSS), "url(") {
		t.Error("the inlined core CSS contains url()")
	}
}

// sitePagesTestVarUseRe matches a var(--name…) consumption.
var sitePagesTestVarUseRe = regexp.MustCompile(`var\(\s*(--[a-z][\w-]*)`)

// sitePagesTestVarDeclRe matches a --name: declaration.
var sitePagesTestVarDeclRe = regexp.MustCompile(`(--[a-z][\w-]*)\s*:`)

// TestSitePagesVarsDeclaredInCore asserts every custom property either sheet
// consumes is declared in pages-core.css: full declares no tokens of its own,
// so a var that core stops declaring would silently fall back to nothing.
func TestSitePagesVarsDeclaredInCore(t *testing.T) {
	core := sitePagesTestReadCSS(t, "pages-core.css")
	full := sitePagesTestReadCSS(t, "pages-full.css")
	declared := map[string]bool{}
	for _, m := range sitePagesTestVarDeclRe.FindAllStringSubmatch(core, -1) {
		declared[m[1]] = true
	}
	if !declared["--bg"] || !declared["--fs-body"] || !declared["--pl-link"] {
		t.Fatal("pages-core.css lost its :root token block")
	}
	for sheet, css := range map[string]string{"pages-core.css": core, "pages-full.css": full} {
		for _, m := range sitePagesTestVarUseRe.FindAllStringSubmatch(css, -1) {
			if !declared[m[1]] {
				t.Errorf("%s consumes %s, which pages-core.css does not declare", sheet, m[1])
			}
		}
	}
}

// sitePagesTestBreakpointRe matches a max-width media query's pixel literal.
var sitePagesTestBreakpointRe = regexp.MustCompile(`@media \(max-width: ?(\d+px)\)`)

// TestSitePagesBreakpointAgrees pins the one value the two sheets must state
// twice (a media query cannot consume a custom property): every max-width
// breakpoint in either sheet is the same literal.
func TestSitePagesBreakpointAgrees(t *testing.T) {
	core := sitePagesTestBreakpointRe.FindAllStringSubmatch(sitePagesTestReadCSS(t, "pages-core.css"), -1)
	full := sitePagesTestBreakpointRe.FindAllStringSubmatch(sitePagesTestReadCSS(t, "pages-full.css"), -1)
	if len(core) == 0 || len(full) == 0 {
		t.Fatalf("expected a max-width media query in both sheets (core %d, full %d)", len(core), len(full))
	}
	want := full[0][1]
	for _, m := range append(core, full...) {
		if m[1] != want {
			t.Errorf("breakpoint %s drifted from %s", m[1], want)
		}
	}
}

// TestSitePagesHeadWiring asserts a rendered page head embeds the core sheet
// (comment-stripped, marked data-gs-core so the boot swap keeps it live) and
// links pages-full.css at the page's own depth.
func TestSitePagesHeadWiring(t *testing.T) {
	page, err := renderSitePage("list", siteListPageData{Chrome: sitePageChrome{Title: "t", Icon: sitePagesDefaultIcon, Base: "../"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := string(page)
	if !strings.Contains(html, `<style data-gs-core>`+sitePagesCoreCSS+`</style>`) {
		t.Error("head must inline the embedded pages-core.css in a <style data-gs-core>")
	}
	if strings.Contains(sitePagesCoreCSS, "/*") {
		t.Error("the inlined core CSS must carry no comments")
	}
	if !strings.Contains(html, `<link rel="preload" as="style" href="../pages-full.css" onload="this.onload=null;this.rel='stylesheet'">`) {
		t.Error("head must async-load pages-full.css (preload + onload flip) relative to the page's base")
	}
	if !strings.Contains(html, `<noscript><link rel="stylesheet" href="../pages-full.css"></noscript>`) {
		t.Error("head must carry the no-JS blocking fallback link for pages-full.css")
	}
	if strings.Contains(html, "pages.css") && !strings.Contains(html, "pages-full.css") {
		t.Error("head still references the retired pages.css")
	}
}

// TestSitePagesAccentCSS pins the accent override's mapping to the app's
// applyAccent semantics: nothing configured emits nothing (the core's stock
// teals govern), a configured accent tints both themes, and accentDark — when
// set — tints the dark theme separately.
func TestSitePagesAccentCSS(t *testing.T) {
	cases := []struct {
		cfg  siteCustomization
		want string
	}{
		{siteCustomization{}, ""},
		{siteCustomization{Accent: "#0a7"}, ":root{--pl-link:#0a7;--pd-link:#0a7}"},
		{siteCustomization{Accent: "#0a7", AccentDark: "#00dddd"}, ":root{--pl-link:#0a7;--pd-link:#00dddd}"},
		{siteCustomization{AccentDark: "#00dddd"}, ":root{--pd-link:#00dddd}"},
	}
	for _, c := range cases {
		if got := string(sitePagesAccentCSS(c.cfg)); got != c.want {
			t.Errorf("accent %q/%q: got %q, want %q", c.cfg.Accent, c.cfg.AccentDark, got, c.want)
		}
	}
	page, err := renderSitePage("list", siteListPageData{Chrome: sitePageChrome{Title: "t", Icon: sitePagesDefaultIcon, Base: "../", AccentCSS: sitePagesAccentCSS(siteCustomization{Accent: "#0a7"})}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(page), `<style data-gs-core>:root{--pl-link:#0a7;--pd-link:#0a7}</style>`) {
		t.Error("a configured accent must be stamped after the inlined core in a <style data-gs-core>")
	}
	plain, err := renderSitePage("list", siteListPageData{Chrome: sitePageChrome{Title: "t", Icon: sitePagesDefaultIcon, Base: "../"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Count(string(plain), "<style data-gs-core>") != 1 {
		t.Error("no configured accent must mean no override style element")
	}
}
