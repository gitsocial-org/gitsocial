// site_markdown_test.go - the front page's markdown renderer: the grammar
// construct by construct, the raw-HTML allowlist against hostile input, and a
// truncated document still rendering well-formed HTML.

package objstore

import (
	"regexp"
	"strings"
	"testing"
)

// mdTestContext is the reference context the front page renders a README with.
var mdTestContext = siteMarkdownContext{AppBase: "https://example.com/index.html#", Branch: "main"}

// renderMD renders markdown with the test context.
func renderMD(t *testing.T, text string) string {
	t.Helper()
	return renderSiteMarkdown(text, mdTestContext)
}

// TestSiteMarkdown_Grammar walks every construct a README uses: each case pins
// what the renderer must emit AND what must not survive as literal source, which
// is the whole point of pre-rendering (the front page used to serve `## About`
// and `<div align="center">` as the text a crawler indexes).
func TestSiteMarkdown_Grammar(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		want   []string
		absent []string
	}{
		{
			name:   "atx headings carry anchor ids",
			src:    "# Title\n\n## About\n\n###### Deep\n",
			want:   []string{`<h1 id="md-title">Title</h1>`, `<h2 id="md-about">About</h2>`, `<h6 id="md-deep">Deep</h6>`},
			absent: []string{"# Title", "## About"},
		},
		{
			name: "duplicate heading slugs are deduplicated",
			src:  "## About\n\n## About\n",
			want: []string{`id="md-about"`, `id="md-about-1"`},
		},
		{
			name: "setext headings",
			src:  "Big\n===\n\nSmall\n---\n",
			want: []string{`<h1 id="md-big">Big</h1>`, `<h2 id="md-small">Small</h2>`},
		},
		{
			name:   "paragraph with emphasis, strong, strike and inline code",
			src:    "plain *em* **strong** ~~gone~~ `x = 1` end\n",
			want:   []string{"<p>plain <em>em</em> <strong>strong</strong> <del>gone</del> <code>x = 1</code> end</p>"},
			absent: []string{"**strong**", "~~gone~~"},
		},
		{
			name:   "nested bullet list",
			src:    "- first\n  - nested\n- second\n",
			want:   []string{"<ul>\n<li>first\n<ul>\n<li>nested</li>\n</ul>\n</li>\n<li>second</li>\n</ul>"},
			absent: []string{"- first"},
		},
		{
			name: "ordered list",
			src:  "1. one\n2. two\n",
			want: []string{"<ol>\n<li>one</li>\n<li>two</li>\n</ol>"},
		},
		{
			name: "task list",
			src:  "- [x] done\n- [ ] todo\n",
			want: []string{`<li class="task"><input type="checkbox" disabled checked>done</li>`, `<li class="task"><input type="checkbox" disabled>todo</li>`},
		},
		{
			name:   "fenced code is escaped, never markup",
			src:    "```go\nif a < b && c[\"k\"] { }\n```\n",
			want:   []string{`<pre class="codeblock"><code>if a &lt; b &amp;&amp; c[&#34;k&#34;] { }</code></pre>`},
			absent: []string{"```"},
		},
		{
			name:   "blockquote nests its own blocks",
			src:    "> quoted **line**\n>\n> - item\n",
			want:   []string{"<blockquote>\n<p>quoted <strong>line</strong></p>", "<li>item</li>", "</blockquote>"},
			absent: []string{"&gt; quoted"},
		},
		{
			name: "thematic break",
			src:  "before\n\n---\n\nafter\n",
			want: []string{"<hr>"},
		},
		{
			name: "table with alignment",
			src:  "| A | B |\n|:--|--:|\n| 1 | 2 |\n",
			want: []string{`<th align="left">A</th>`, `<th align="right">B</th>`, `<td align="left">1</td>`, `<td align="right">2</td>`},
		},
		{
			name:   "absolute link, autolink and bare url",
			src:    "[text](https://example.org/a) <https://example.org/b> https://example.org/c\n",
			want:   []string{`<a href="https://example.org/a">text</a>`, `<a href="https://example.org/b">https://example.org/b</a>`, `<a href="https://example.org/c">https://example.org/c</a>`},
			absent: []string{"[text]("},
		},
		{
			name: "in-page anchor resolves onto this renderer's heading ids",
			src:  "## About\n\n[jump](#about)\n",
			want: []string{`<h2 id="md-about">About</h2>`, `<a href="#md-about">jump</a>`},
		},
		{
			name: "repo-relative link becomes the app's file route",
			src:  "[spec](specs/GITMSG.md#2-lists) and [plain](./docs/A.md)\n",
			want: []string{`<a href="https://example.com/index.html#file:specs/GITMSG.md@main:2-lists">spec</a>`, `<a href="https://example.com/index.html#file:docs/A.md@main">plain</a>`},
		},
		{
			name: "absolute https image keeps its src and attributes",
			src:  `<img src="https://img.example.com/badge.svg" alt="badge" width="120">` + "\n",
			want: []string{`<img src="https://img.example.com/badge.svg" alt="badge" width="120">`},
		},
		{
			name:   "markdown image, absolute",
			src:    "![alt text](https://img.example.com/a.png)\n",
			want:   []string{`<img src="https://img.example.com/a.png" alt="alt text">`},
			absent: []string{"![alt text]"},
		},
		{
			name:   "repo-relative image degrades to its alt text, never a src",
			src:    "![Project logo](docs/logo.svg)\n",
			want:   []string{"<p>Project logo</p>"},
			absent: []string{"<img", "docs/logo.svg"},
		},
		{
			name:   "repo-relative image tag degrades to its alt text",
			src:    `<img src="documentation/images/icon.svg" width="120" alt="Icon">` + "\n",
			want:   []string{"Icon"},
			absent: []string{"<img", "documentation/images/icon.svg"},
		},
		{
			name:   "hero div wraps the markdown blocks that follow it",
			src:    "<div align=\"center\">\n\n# Project\n\n*tagline*\n\n</div>\n",
			want:   []string{`<div align="center">`, `<h1 id="md-project">Project</h1>`, "<p><em>tagline</em></p>", "</div>"},
			absent: []string{"&lt;div align="},
		},
		{
			name: "inline raw html on a paragraph line stays inline",
			src:  "a <b>bold</b> and <kbd>Esc</kbd> here\n",
			want: []string{"<p>a <b>bold</b> and <kbd>Esc</kbd> here</p>"},
		},
		{
			name: "details/summary survive the allowlist",
			src:  "<details open>\n\ninner text\n\n</details>\n",
			want: []string{`<details open="">`, "<p>inner text</p>", "</details>"},
		},
		{
			name:   "html comments are dropped",
			src:    "<!-- hidden note -->\n\nvisible\n",
			absent: []string{"hidden note", "<!--"},
			want:   []string{"<p>visible</p>"},
		},
		{
			name: "backslash escapes stay literal",
			src:  "\\*not em\\*\n",
			want: []string{"<p>*not em*</p>"},
		},
		{
			name: "text is escaped, never interpreted",
			src:  "5 < 6 & \"quoted\"\n",
			want: []string{"<p>5 &lt; 6 &amp; &#34;quoted&#34;</p>"},
		},
		{
			name: "an unknown tag unwraps to its children",
			src:  "<article><p>kept</p></article>\n",
			want: []string{"<p>kept</p>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderMD(t, tc.src)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("unwanted %q in:\n%s", absent, got)
				}
			}
		})
	}
}

// TestSiteMarkdown_Hostile pins the sanitizer. The README is the bucket owner's
// own content, so this is hygiene rather than an adversarial boundary, but the
// renderer must be correct on its own: the pages carry a strict CSP and it is
// never the thing that makes the output safe.
func TestSiteMarkdown_Hostile(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		absent []string
		want   []string
	}{
		{
			name:   "script element and its body vanish",
			src:    "<script>alert('xss')</script>\n",
			absent: []string{"<script", "alert("},
		},
		{
			name:   "script opened as a block wrapper carries nothing executable",
			src:    "<script>\n\nalert('xss')\n\n</script>\n\nafter\n",
			absent: []string{"<script", "</script>"},
			want:   []string{"<p>after</p>"},
		},
		{
			name:   "event handlers are not in the attribute allowlist",
			src:    `<img src="https://e.com/a.png" onerror="alert(1)" alt="a">` + "\n",
			absent: []string{"onerror", "alert(1)"},
			want:   []string{`<img src="https://e.com/a.png" alt="a">`},
		},
		{
			name:   "event handler on an allowed container is dropped",
			src:    "<div onclick=\"steal()\" align=\"center\">\n\nhi\n\n</div>\n",
			absent: []string{"onclick", "steal()"},
			want:   []string{`<div align="center">`},
		},
		{
			name:   "javascript: link target is dropped",
			src:    "[click](javascript:alert(1))\n",
			absent: []string{"javascript:"},
			want:   []string{"<a>click</a>"},
		},
		{
			name:   "javascript: href in raw html is dropped",
			src:    `<a href="javascript:alert(1)">click</a>` + "\n",
			absent: []string{"javascript:"},
			want:   []string{"<a>click</a>"},
		},
		{
			name:   "entity-obfuscated javascript: href is dropped",
			src:    `<a href="java&#115;cript:alert(1)">click</a>` + "\n",
			absent: []string{"javascript:", "java&#115;cript", "alert(1)"},
			// The positive half is what makes this case discriminate: the value has
			// to be entity-DECODED before the scheme is judged, or the link merely
			// resolves somewhere else instead of being refused.
			want: []string{"<a>click</a>"},
		},
		{
			name:   "javascript: and data: image sources never become a src",
			src:    "![a](javascript:alert(1))\n\n![b](data:text/html,<script>alert(1)</script>)\n",
			absent: []string{"javascript:", "data:text/html", "<script"},
		},
		{
			name:   "iframe, object and style vanish subtree and all",
			src:    "<iframe src=\"https://evil.example\"></iframe>\n\n<object data=\"x.swf\"></object>\n\n<style>body{display:none}</style>\n",
			absent: []string{"<iframe", "<object", "<style", "evil.example", "display:none"},
		},
		{
			name:   "svg with an onload handler vanishes",
			src:    `<svg onload="alert(1)"><circle r="10"/></svg>` + "\n",
			absent: []string{"<svg", "onload", "alert(1)"},
		},
		{
			name:   "a style attribute has no way through",
			src:    "<div style=\"position:fixed;top:0\">\n\nx\n\n</div>\n",
			absent: []string{"style="},
			want:   []string{"<div>"},
		},
		{
			name:   "a form and its inputs vanish",
			src:    "<form action=\"https://evil.example\"><input name=\"pw\"></form>\n",
			absent: []string{"<form", "<input name", "evil.example"},
		},
		{
			name:   "an unclosed script wrapper cannot leak an executable tag",
			src:    "text\n\n<script>\n\nalert(1)\n",
			absent: []string{"<script"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderMD(t, tc.src)
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("unwanted %q in:\n%s", absent, got)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
			assertBalancedHTML(t, got)
		})
	}
}

// TestSiteMarkdown_Truncation: the front page caps the README source before
// rendering, so the renderer is routinely handed a document cut mid-structure.
// Every cut must still produce balanced markup — an unclosed <div>, <ul> or
// fence would run to the end of the page and swallow everything under it.
func TestSiteMarkdown_Truncation(t *testing.T) {
	full := "<div align=\"center\">\n\n# Title\n\n</div>\n\n## Section\n\n- one\n  - nested\n- two\n\n```go\nfunc main() {}\n```\n\n> quote\n\n<details>\n\ninner\n\n</details>\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\ntail paragraph\n"
	for limit := 1; limit <= len(full); limit++ {
		src, truncated := siteMDTruncateSource(full, limit)
		if truncated != (limit < len(full)) {
			t.Fatalf("limit %d: truncated=%v", limit, truncated)
		}
		got := renderSiteMarkdown(src, mdTestContext)
		assertBalancedHTML(t, got)
		// A cut that landed on a line boundary (the only kind the cap produces
		// once the source has any line at all) must never leave raw markup behind.
		if strings.Contains(src, "\n") && (strings.Contains(got, "&lt;div") || strings.Contains(got, "```")) {
			t.Fatalf("limit %d left raw source in the output:\n%s", limit, got)
		}
	}
}

// TestSiteMarkdown_Empty: a README with nothing renderable in it produces no
// section at all rather than an empty container.
func TestSiteMarkdown_Empty(t *testing.T) {
	for _, src := range []string{"", "   \n\n \n", "<!-- only a comment -->\n"} {
		if got := renderSiteMarkdown(src, mdTestContext); got != "" {
			t.Errorf("renderSiteMarkdown(%q) = %q, want empty", src, got)
		}
	}
}

// TestSiteMarkdown_NoReferenceContext: without a branch (a bucket whose tip the
// pusher cannot read) a relative reference resolves to nothing rather than to a
// link that goes nowhere.
func TestSiteMarkdown_NoReferenceContext(t *testing.T) {
	got := renderSiteMarkdown("[spec](specs/A.md) and [abs](https://e.com/)\n", siteMarkdownContext{})
	if !strings.Contains(got, "<a>spec</a>") {
		t.Errorf("relative link must render without an href:\n%s", got)
	}
	if !strings.Contains(got, `<a href="https://e.com/">abs</a>`) {
		t.Errorf("absolute link must still resolve:\n%s", got)
	}
}

// mdTagRE finds one tag in rendered output (the assertion below re-lexes the
// output independently of the renderer's own lexer).
var mdTagRE = regexp.MustCompile(`</?([a-zA-Z][\w-]*)[^>]*>`)

// mdVoidTags are the tags assertBalancedHTML expects no closing tag for.
var mdVoidTags = map[string]bool{"br": true, "hr": true, "img": true, "input": true, "col": true, "wbr": true, "area": true}

// assertBalancedHTML fails unless every element in the rendered output is closed
// in the order it was opened.
func assertBalancedHTML(t *testing.T, out string) {
	t.Helper()
	var stack []string
	for _, m := range mdTagRE.FindAllStringSubmatch(out, -1) {
		tag := strings.ToLower(m[1])
		switch {
		case mdVoidTags[tag] || strings.HasSuffix(m[0], "/>"):
		case strings.HasPrefix(m[0], "</"):
			if len(stack) == 0 || stack[len(stack)-1] != tag {
				t.Fatalf("unbalanced </%s> (open: %v) in:\n%s", tag, stack, out)
			}
			stack = stack[:len(stack)-1]
		default:
			stack = append(stack, tag)
		}
	}
	if len(stack) > 0 {
		t.Fatalf("unclosed elements %v in:\n%s", stack, out)
	}
}
