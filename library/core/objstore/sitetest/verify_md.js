// verify_md.js - GFM parser units + sanitizer/markdown/fullscreen shim tests.
// A minimal DOM + DOMParser shim lets the browser-only sanitizer and renderer
// run headlessly; the parser (parseInline/parseMarkdown) is pure and needs no DOM.

// ---------- DOM shim ----------
function mkEl(tag) {
  return {
    nodeType: 1, tagName: (tag || "").toUpperCase(), _attrs: new Map(), _children: [],
    style: {}, _cls: new Set(), _handlers: {}, _parent: null,
    get attributes() { return Array.from(this._attrs, ([name, value]) => ({ name, value })); },
    get childNodes() { return this._children; },
    get children() { return this._children.filter((c) => c && c.nodeType === 1); },
    setAttribute(k, v) { this._attrs.set(k, String(v)); },
    getAttribute(k) { return this._attrs.has(k) ? this._attrs.get(k) : null; },
    hasAttribute(k) { return this._attrs.has(k); },
    removeAttribute(k) { this._attrs.delete(k); },
    set className(v) { this._cls = new Set(String(v).split(/\s+/).filter(Boolean)); },
    get className() { return Array.from(this._cls).join(" "); },
    classList: {
      add() {}, remove() {}, toggle() {}, contains() { return false; },
    },
    append(...cs) { for (const c of cs) { const n = norm(c); if (n && typeof n === "object") n._parent = this; this._children.push(n); } },
    prepend(...cs) { this._children.unshift(...cs.map(norm)); },
    replaceChildren(...cs) { this._children = cs.map(norm); },
    remove() { if (this._parent) { const i = this._parent._children.indexOf(this); if (i >= 0) this._parent._children.splice(i, 1); } },
    cloneNode() { const n = mkEl(this.tagName); n._attrs = new Map(this._attrs); n._children = this._children.map((c) => (c && c.cloneNode ? c.cloneNode(true) : c)); return n; },
    addEventListener(ev, fn) { (this._handlers[ev] = this._handlers[ev] || []).push(fn); },
    removeEventListener() {},
    scrollIntoView() {},
    querySelectorAll(sel) { const out = []; collect(this, sel, out); return out; },
    set textContent(v) { this._children = [{ nodeType: 3, nodeValue: String(v) }]; },
    get textContent() { return textOf(this); },
    set innerHTML(v) { this._html = String(v); },
  };
}
function norm(c) { return (typeof c === "string" || typeof c === "number") ? { nodeType: 3, nodeValue: String(c) } : c; }
function textOf(n) {
  if (n == null) return "";
  if (typeof n === "string") return n;
  if (n.nodeType === 3) return n.nodeValue || "";
  let s = ""; for (const c of n._children || []) s += textOf(c); return s;
}
function matchSel(node, sel) {
  const m = /^([a-zA-Z0-9]*)(?:\[([^\]=]+)(?:=["']?([^\]"']*)["']?)?\])?$/.exec(sel.trim());
  if (!m) return false;
  if (m[1] && node.tagName.toLowerCase() !== m[1].toLowerCase()) return false;
  if (m[2]) { if (!node.hasAttribute(m[2])) return false; if (m[3] != null && m[3] !== "" && node.getAttribute(m[2]) !== m[3]) return false; }
  return true;
}
function collect(node, sel, out) {
  for (const c of node._children || []) { if (c && c.nodeType === 1) { if (matchSel(c, sel)) out.push(c); collect(c, sel, out); } }
}

// ---------- minimal HTML parser for DOMParser shim ----------
const VOID = new Set(["br", "hr", "img", "source", "col", "input", "wbr", "area", "meta", "link", "base"]);
function parseAttrs(str) {
  const attrs = [];
  const re = /([a-zA-Z_:][\w:.-]*)(?:\s*=\s*("([^"]*)"|'([^']*)'|[^\s"'>]+))?/g;
  let m;
  while ((m = re.exec(str)) !== null) {
    const name = m[1];
    let val = m[3] != null ? m[3] : m[4] != null ? m[4] : (m[2] != null ? m[2] : "");
    attrs.push({ name, value: val });
  }
  return attrs;
}
function parseHTML(html) {
  const root = mkEl("body");
  const stack = [root];
  const re = /<!--[\s\S]*?-->|<\/([a-zA-Z][\w-]*)\s*>|<([a-zA-Z][\w-]*)((?:[^<>"']|"[^"]*"|'[^']*')*?)(\/?)>|([^<]+)/g;
  let m;
  while ((m = re.exec(html)) !== null) {
    if (m[0].startsWith("<!--")) continue;
    const cur = stack[stack.length - 1];
    if (m[1]) { // close tag
      for (let i = stack.length - 1; i > 0; i--) { if (stack[i].tagName.toLowerCase() === m[1].toLowerCase()) { stack.length = i; break; } }
    } else if (m[2]) { // open tag
      const node = mkEl(m[2]);
      for (const a of parseAttrs(m[3] || "")) node._attrs.set(a.name, a.value);
      cur._children.push(node); node._parent = cur;
      if (!m[4] && !VOID.has(m[2].toLowerCase())) stack.push(node);
    } else if (m[5]) { // text
      cur._children.push({ nodeType: 3, nodeValue: m[5] });
    }
  }
  return root;
}

const docHandlers = {};
global.document = {
  createElement: (t) => mkEl(t),
  createTextNode: (v) => ({ nodeType: 3, nodeValue: String(v) }),
  getElementById: () => mkEl("div"),
  querySelectorAll: () => [],
  addEventListener: (ev, fn) => { (docHandlers[ev] = docHandlers[ev] || []).push(fn); },
  removeEventListener: (ev, fn) => { if (docHandlers[ev]) docHandlers[ev] = docHandlers[ev].filter((f) => f !== fn); },
  body: mkEl("body"),
};
global.window = { addEventListener() {}, matchMedia: () => ({ matches: false }) };
global.location = { hash: "", pathname: "/gitsocial/", origin: (process.env.GS_SITE_ORIGIN||"http://localhost:8000"), search: "" };
global.DOMParser = function () {};
global.DOMParser.prototype.parseFromString = function (html) { return { body: parseHTML(html) }; };
let objCounter = 0;
global.URL.createObjectURL = () => "blob:mock/" + (++objCounter);
global.URL.revokeObjectURL = () => {};
global.Blob = function (parts, opts) { this.type = opts && opts.type; };

const GS = require("../site/gs-app.js");

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const attrOf = (n, k) => (n && n.getAttribute ? n.getAttribute(k) : null);
function findTag(node, tag, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTag(c, tag, out); } } return out; }
function insideTag(node, tag) { for (let p = node && node._parent; p; p = p._parent) { if (p.tagName && p.tagName.toLowerCase() === tag) return true; } return false; }

// ===================== A. Parser units (pure, no DOM) =====================
(function () {
  // strikethrough + bold + em + code inline
  const sp = GS.parseInline("a ~~gone~~ **b** _i_ `c`");
  ok("inline strike token", sp.some((s) => s.type === "strike"), JSON.stringify(sp.map((s) => s.type)));
  ok("inline strong+em+code tokens", sp.some((s) => s.type === "strong") && sp.some((s) => s.type === "em") && sp.some((s) => s.type === "code"));
  // image + link
  const img = GS.parseInline("![alt text](documentation/x.png)");
  ok("inline image token", img[0].type === "image" && img[0].alt === "alt text" && img[0].src === "documentation/x.png", JSON.stringify(img[0]));
  const lnk = GS.parseInline("[t](https://ex.com)");
  ok("inline link token", lnk[0].type === "link" && lnk[0].href === "https://ex.com");
  // autolink <url> and bare url
  ok("autolink <url>", GS.parseInline("<https://a.b/c>")[0].type === "link");
  ok("bare url autolink", GS.parseInline("see https://a.b/c end").some((s) => s.type === "link" && s.href === "https://a.b/c"));
  // raw inline html
  const rh = GS.parseInline("press <kbd>Ctrl</kbd> now");
  ok("inline rawhtml token", rh.some((s) => s.type === "rawhtml" && /kbd/.test(s.value)), JSON.stringify(rh.map((s) => s.type)));

  // table
  const tbl = GS.parseMarkdown("| A | B |\n|---|:-:|\n| 1 | 2 |\n");
  ok("table block parsed", tbl[0].type === "table" && tbl[0].headers.length === 2 && tbl[0].rows.length === 1, JSON.stringify(tbl[0] && tbl[0].type));
  ok("table alignment center", tbl[0].aligns[1] === "center", JSON.stringify(tbl[0].aligns));
  // task list
  const tl = GS.parseMarkdown("- [x] done\n- [ ] todo\n");
  ok("task list items", tl[0].type === "list" && tl[0].items[0].task === true && tl[0].items[1].task === false, JSON.stringify(tl[0].items.map((i) => i.task)));
  // nested list
  const nl = GS.parseMarkdown("- a\n  - a1\n  - a2\n- b\n");
  ok("nested list child attached", nl[0].items[0].children.length === 1 && nl[0].items[0].children[0].items.length === 2, JSON.stringify(nl[0].items.map((i) => i.children.length)));
  // blockquote
  const bq = GS.parseMarkdown("> quoted **b**\n> more\n");
  ok("blockquote block parsed", bq[0].type === "blockquote" && bq[0].blocks.length >= 1, JSON.stringify(bq[0] && bq[0].type));
  // html blocks
  const hb = GS.parseMarkdown('<div align="center">\n\n<img src="a.png" width="10">\n\ntext\n\n</div>\n');
  const types = hb.map((b) => b.type);
  ok("html open/close wrappers recognized", types.includes("htmlopen") && types.includes("htmlclose") && types.includes("html"), JSON.stringify(types));
  const openBlk = hb.find((b) => b.type === "htmlopen");
  ok("htmlopen keeps align attr", /align="center"/.test(openBlk.open) && openBlk.tag === "div", openBlk.open);
  ok("no raw <div paragraph leaks", !hb.some((b) => b.type === "paragraph" && b.spans.some((s) => s.type === "text" && s.value.includes("<div"))));
})();

// ===================== A2. Comments, setext, thematic breaks, escapes, HTML flow =====================
(function () {
  // html comments stripped inline (single- and multi-line; paragraph lines join with \n)
  const ic = GS.parseInline("a <!-- gone --> b");
  ok("inline html comment stripped", !ic.map((s) => s.value || "").join("").includes("<!--") && ic.map((s) => s.value || "").join("").includes("b"), JSON.stringify(ic));
  ok("unterminated comment eats rest", GS.parseInline("a <!-- gone").map((s) => s.value).join("") === "a ");
  const co = GS.parseMarkdown("<!-- LOGO -->\n\ntext\n");
  ok("comment-only line emits no block", co.length === 1 && co[0].type === "paragraph", JSON.stringify(co.map((b) => b.type)));
  const mc = GS.parseMarkdown("x <!-- a\nb --> y\n");
  ok("multi-line comment stripped in paragraph", mc.length === 1 && !JSON.stringify(mc).includes("<!--") && JSON.stringify(mc).includes("y"), JSON.stringify(mc));
  const fc = GS.parseMarkdown("```\n<!-- keep -->\n```\n");
  ok("comment in code fence stays literal", fc[0].type === "code" && fc[0].text.includes("<!-- keep -->"), JSON.stringify(fc[0]));
  // setext headings + thematic breaks (--- under text is setext h2, standalone is a rule)
  const sx = GS.parseMarkdown("Title\n===\n\nSub\n---\n");
  ok("setext h1 + h2", sx.length === 2 && sx[0].type === "heading" && sx[0].level === 1 && sx[1].type === "heading" && sx[1].level === 2, JSON.stringify(sx.map((b) => [b.type, b.level])));
  const th = GS.parseMarkdown("a\n\n---\n\nb\n");
  ok("standalone --- is thematic break", th.length === 3 && th[1].type === "thematic", JSON.stringify(th.map((b) => b.type)));
  ok("*** is thematic break", GS.parseMarkdown("***\n")[0].type === "thematic");
  // backslash escapes for punctuation
  const be = GS.parseInline("\\*not em\\*");
  ok("backslash escapes punctuation", be.length === 1 && be[0].value === "*not em*", JSON.stringify(be));
  // inline-tag lines join the paragraph (CommonMark inline flow; the link-row shape)
  const lr = GS.parseMarkdown('intro\n<a href="#a">A</a>\n·\n<a href="#b">B</a>\n');
  ok("inline-tag lines join paragraph", lr.length === 1 && lr[0].type === "paragraph" && lr[0].spans.filter((s) => s.type === "rawhtml").length === 2, JSON.stringify(lr.map((b) => b.type)));
  // htmlclose carries its tag for matching pops
  ok("htmlclose carries tag", GS.parseMarkdown("</div>\n")[0].tag === "div");
  // a lone <img> between blank lines is still a standalone html block
  const si = GS.parseMarkdown('before\n\n<img src="x.png">\n\nafter\n');
  ok("lone img stays standalone html block", si.length === 3 && si[1].type === "html" && /img/.test(si[1].raw), JSON.stringify(si.map((b) => b.type)));
})();

// ===================== B. Sanitizer whitelist walk (synthetic inert) =====================
const iel = (tag, attrs, kids) => ({ nodeType: 1, tagName: tag.toUpperCase(), attributes: attrs || [], childNodes: kids || [] });
const itext = (v) => ({ nodeType: 3, nodeValue: v });
const body = (kids) => ({ nodeType: 1, tagName: "BODY", attributes: [], childNodes: kids });

(function () {
  // script dropped entirely (subtree gone)
  let out = GS.sanitizeInert(body([iel("script", [], [itext("alert(1)")]), itext("safe")]), {});
  ok("script tag stripped subtree", !out.some((n) => n.tagName === "SCRIPT") && out.map(textOf).join("") === "safe", JSON.stringify(out.map((n) => n.tagName || n.nodeValue)));
  // iframe dropped
  out = GS.sanitizeInert(body([iel("iframe", [{ name: "src", value: "https://evil" }], [])]), {});
  ok("iframe stripped", out.length === 0);
  // onclick + style attrs stripped, tag kept
  out = GS.sanitizeInert(body([iel("div", [{ name: "onclick", value: "x()" }, { name: "style", value: "color:red" }, { name: "align", value: "center" }], [itext("hi")])]), {});
  ok("onclick/style stripped, align survives", out[0].tagName === "DIV" && attrOf(out[0], "onclick") === null && attrOf(out[0], "style") === null && attrOf(out[0], "align") === "center", JSON.stringify(out[0].attributes));
  // javascript: href dropped
  out = GS.sanitizeInert(body([iel("a", [{ name: "href", value: "javascript:alert(1)" }], [itext("x")])]), {});
  ok("javascript: href dropped", out[0].tagName === "A" && attrOf(out[0], "href") === null, JSON.stringify(out[0].attributes));
  // https href survives
  out = GS.sanitizeInert(body([iel("a", [{ name: "href", value: "https://ok.com" }], [itext("x")])]), {});
  ok("https href survives", attrOf(out[0], "href") === "https://ok.com");
  // javascript: src dropped on img
  out = GS.sanitizeInert(body([iel("img", [{ name: "src", value: "javascript:alert(1)" }, { name: "alt", value: "a" }], [])]), {});
  ok("javascript: img src dropped, alt kept", attrOf(out[0], "src") === null && attrOf(out[0], "alt") === "a", JSON.stringify(out[0].attributes));
  // absolute https img src survives
  out = GS.sanitizeInert(body([iel("img", [{ name: "src", value: "https://img.shields.io/x.svg" }], [])]), {});
  ok("https img src survives (badge parity)", attrOf(out[0], "src") === "https://img.shields.io/x.svg");
  // relative img → data-gs-src deferred marker
  out = GS.sanitizeInert(body([iel("img", [{ name: "src", value: "documentation/images/x.png" }], [])]), { ctx: {}, branch: "main", dir: "" });
  ok("relative img → data-gs-src marker", attrOf(out[0], "data-gs-src") === "documentation/images/x.png" && attrOf(out[0], "data-gs-branch") === "main" && attrOf(out[0], "src") === null, JSON.stringify(out[0].attributes));
  // unknown tag unwrapped to children
  out = GS.sanitizeInert(body([iel("marquee", [], [itext("keep"), iel("b", [], [itext("bold")])])]), {});
  ok("unknown tag unwrapped to children", !out.some((n) => n.tagName === "MARQUEE") && out.some((n) => n.tagName === "B") && out.map(textOf).join("").includes("keep"), JSON.stringify(out.map((n) => n.tagName || n.nodeValue)));
  // picture degrades to its img
  out = GS.sanitizeInert(body([iel("picture", [], [iel("source", [{ name: "src", value: "https://a/dark.png" }], []), iel("img", [{ name: "src", value: "https://a/light.png" }], [])])]), {});
  ok("picture degrades to img (source dropped)", out.length === 1 && out[0].tagName === "IMG" && attrOf(out[0], "src") === "https://a/light.png", JSON.stringify(out.map((n) => n.tagName)));
  // div align=center wrapping img (README shape)
  out = GS.sanitizeInert(body([iel("div", [{ name: "align", value: "center" }], [iel("img", [{ name: "src", value: "documentation/images/gitsocial-icon.png" }, { name: "width", value: "120" }], [])])]), { ctx: {}, branch: "main", dir: "" });
  const innerImg = findTag(out[0], "img")[0];
  ok("README div>img: div align kept, img width kept, relative deferred", out[0].tagName === "DIV" && attrOf(out[0], "align") === "center" && innerImg && attrOf(innerImg, "width") === "120" && attrOf(innerImg, "data-gs-src") === "documentation/images/gitsocial-icon.png", JSON.stringify({ div: out[0].attributes, img: innerImg && innerImg.attributes }));
})();

// ===================== C. renderMarkdown end-to-end (DOMParser shim) =====================
(function () {
  const md = '<div align="center">\n\n# Title\n\n<img src="documentation/images/icon.png" width="120">\n\n*tagline*\n\n</div>\n\n' +
    "| A | B |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n\n~~struck~~ and https://ex.com\n\n```go\nfunc main() {}\n```\n";
  const root = GS.renderMarkdown(md, { ctx: {}, branch: "main", dir: "" });
  const txt = textOf(root);
  const divs = findTag(root, "div").filter((d) => d.getAttribute("align") === "center");
  ok("md: centered div survives", divs.length === 1, "divs=" + divs.length);
  const imgs = findTag(root, "img");
  // resolveImages (fire-and-forget) strips data-gs-src as it resolves; the
  // branch/dir markers remain and prove the relative image path engaged.
  ok("md: relative img routed through in-bucket resolution", imgs.length === 1 && attrOf(imgs[0], "data-gs-branch") === "main" && !/^https/.test(attrOf(imgs[0], "src") || ""), JSON.stringify(imgs[0] && imgs[0].attributes));
  ok("md: table rendered", findTag(root, "table").length === 1 && findTag(root, "th").length === 2);
  ok("md: task checkboxes rendered", findTag(root, "input").length === 2);
  ok("md: strikethrough rendered as <del>", findTag(root, "del").length >= 1);
  ok("md: fenced code preserved", findTag(root, "pre").length === 1 && /func main/.test(txt));
  ok("md: no escaped <div text leaks", !txt.includes("<div"), txt.slice(0, 80));
})();

// ===================== C2. Hero-header README top end-to-end (upstream shape, verbatim) =====================
(function () {
  // Real-world README opener: leading comment, <h1> wrapping an unclosed <p>,
  // a centered <p> whose tagline + <br/>-separated link row must stay one
  // paragraph, and a stray trailing </p>. GitHub semantics: comment invisible,
  // link row inline, tagline NOT inside the h1.
  const md = [
    "<!-- LOGO -->",
    "<h1>",
    '<p align="center">',
    '  <img src="https://github.com/user-attachments/assets/fe853809-ba8b-400b-83ab-a9a0da25be8a" alt="Logo" width="128">',
    "  <br>Ghostty",
    "</h1>",
    '  <p align="center">',
    "    Fast, native, feature-rich terminal emulator pushing modern features.",
    "    <br />",
    "    A native GUI or embeddable library via <code>libghostty</code>.",
    "    <br />",
    '    <a href="#about">About</a>',
    "    ·",
    '    <a href="https://ghostty.org/download">Download</a>',
    "    ·",
    '    <a href="https://ghostty.org/docs">Documentation</a>',
    "    ·",
    '    <a href="CONTRIBUTING.md">Contributing</a>',
    "    ·",
    '    <a href="HACKING.md">Developing</a>',
    "  </p>",
    "</p>",
    "",
    "## About",
    "",
  ].join("\n");
  const root = GS.renderMarkdown(md, {});
  const txt = textOf(root);
  ok("hero: comment stripped", !txt.includes("<!--") && !txt.includes("LOGO"), txt.slice(0, 60));
  const h1s = findTag(root, "h1");
  ok("hero: single h1 holds only the logo line", h1s.length === 1 && textOf(h1s[0]).includes("Ghostty") && !textOf(h1s[0]).includes("Fast"), h1s[0] && textOf(h1s[0]));
  const anchors = findTag(root, "a");
  ok("hero: five anchors rendered", anchors.length === 5, "count=" + anchors.length);
  const rowP = anchors[0] && anchors[0]._parent;
  ok("hero: link row inline in one paragraph", !!rowP && rowP.tagName === "P" && anchors.every((a) => a._parent === rowP) && textOf(rowP).includes("·"), rowP && rowP.tagName);
  ok("hero: tagline under p wrapper, not h1", !!rowP && textOf(rowP).includes("Fast, native") && !insideTag(rowP, "h1"));
  ok("hero: About heading follows", findTag(root, "h2").some((h) => textOf(h).includes("About")));

  // mismatched close recovery: an unmatched </span> must not pop the <div>
  const rec = GS.renderMarkdown('<div align="center">\n\n</span>\n\ninside\n\n</div>\n\noutside\n', {});
  const div = findTag(rec, "div").find((d) => d.getAttribute("align") === "center");
  ok("recovery: unmatched close ignored", !!div && textOf(div).includes("inside") && !textOf(div).includes("outside"), div && textOf(div));

  // setext + thematic + escapes end-to-end
  const st = GS.renderMarkdown("Big\n===\n\nSmall\n-----\n\n---\n\n\\*lit\\*\n", {});
  ok("setext h1/h2 rendered", findTag(st, "h1").length === 1 && findTag(st, "h2").length === 1);
  ok("thematic break renders hr", findTag(st, "hr").length === 1);
  ok("escaped * renders literal, no em", textOf(st).includes("*lit*") && findTag(st, "em").length === 0, textOf(st));
})();

// ===================== D. Live README render (real fixture) =====================
async function liveReadme() {
  const gs = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  const head = await GS.resolveHead((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  const rm = await GS.resolvePath(gs, head.sha, "README.md");
  const obj = await GS.getObject(gs, rm.sha);
  const text = new TextDecoder().decode(obj.body);
  const root = GS.renderMarkdown(text, { ctx: gs, branch: "main", dir: "" });
  const txt = textOf(root);
  const centered = findTag(root, "div").filter((d) => d.getAttribute("align") === "center");
  ok("README: <div align=center> survives", centered.length >= 1, "count=" + centered.length);
  const allImgs = findTag(root, "img");
  const badge = allImgs.find((i) => /^https:\/\//.test(i.getAttribute("src") || ""));
  ok("README: badge img keeps absolute https src", !!badge, "imgs=" + allImgs.length);
  ok("README: no escaped <div text visible", !txt.includes("<div") && !txt.includes("&lt;div"), txt.slice(0, 100));
  ok("README: About heading rendered", findTag(root, "h2").some((h) => textOf(h).includes("About")));
  // resolveImages builds in-bucket object URLs for the relative icon + demo.gif.
  await new Promise((r) => setTimeout(r, 400));
  const relResolved = allImgs.filter((i) => /^blob:/.test(i.getAttribute("src") || ""));
  ok("README: relative img(s) resolved to object URL", relResolved.length >= 1, "resolved=" + relResolved.length + " of " + allImgs.length);
  ok("README: icon img resolved (dir + branch markers)", allImgs.some((i) => i.getAttribute("data-gs-branch") === "main" && /^blob:/.test(i.getAttribute("src") || "")));
}

// ===================== E. Fullscreen overlay shim =====================
(function () {
  const target = mkEl("div"); target.append("content");
  global.document.body._children = [];
  GS.openFullscreen(target);
  const overlay = global.document.body._children.find((c) => c && c._cls && c._cls.has("fs-overlay"));
  ok("fullscreen: overlay appended to body", !!overlay && global.document.body.style.overflow === "hidden");
  // find close button and click
  const closeBtn = findTag(overlay, "button").find((b) => b._cls && b._cls.has("fs-close"));
  ok("fullscreen: has close button + cloned content", !!closeBtn && textOf(overlay).includes("content"));
  (closeBtn._handlers.click || []).forEach((fn) => fn({}));
  ok("fullscreen: close removes overlay + restores scroll", !global.document.body._children.includes(overlay) && global.document.body.style.overflow === "");
  // Escape closes too
  GS.openFullscreen(target);
  const ov2 = global.document.body._children.find((c) => c && c._cls && c._cls.has("fs-overlay"));
  (docHandlers.keydown || []).forEach((fn) => fn({ key: "Escape" }));
  ok("fullscreen: Escape removes overlay", !global.document.body._children.includes(ov2) && global.document.body.style.overflow === "");
})();

liveReadme().then(() => {
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}).catch((e) => { console.error("THREW:", e); process.exit(1); });
