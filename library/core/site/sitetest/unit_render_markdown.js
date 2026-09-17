// unit_render_markdown.js - shim-rendered units for the markdown renderer:
// tables, lists, raw-HTML sanitizing, relative link and image resolution,
// in-page heading anchors, and the fullscreen overlay a code block opens.
require("./shim.js");
const GS = require("../assets/gs-render.js");
const { textOf, findTag } = global.__shim;
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { for (const fn of (node && node._handlers && node._handlers[ev]) || []) fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", target: { closest() { return null; } } }, props || {})); }
const href = (n) => n.getAttribute("href");

// The bucket is never read here: no mdctx.ctx means images stay at their alt text.
console.log("=== tables ===");
const table = GS.renderMarkdown("| left | right | plain |\n|:--|--:|---|\n| 1 | 2 | 3 |\n| 4 | 5 | 6 |\n");
eq(findTag(table, "table").length, 1, "one table");
eq(findTag(table, "th").map(textOf), ["left", "right", "plain"], "header cells in order");
eq(findTag(table, "th").map((n) => n.getAttribute("align")), ["left", "right", null], "per-column alignment, none for a plain column");
eq(findTag(table, "tr").length, 3, "a header row and two body rows");
eq(findTag(table, "td").map(textOf), ["1", "2", "3", "4", "5", "6"], "body cells in row order");
eq(findTag(table, "td").map((n) => n.getAttribute("align")), ["left", "right", null, "left", "right", null], "body cells inherit the column alignment");

console.log("=== lists and inline spans ===");
const lists = GS.renderMarkdown("- [x] done\n- [ ] todo\n  - nested\n\n1. one\n2. two\n\n`code` **bold** _em_ ~~gone~~\n");
eq(findTag(lists, "ul").length, 2, "an outer and a nested bullet list");
eq(findTag(lists, "ol").length, 1, "one ordered list");
eq(findTag(lists, "input").map((n) => n.getAttribute("checked")), ["", null], "the checked task box carries checked, the open one does not");
ok(findTag(lists, "input").every((n) => n.getAttribute("disabled") === ""), "task boxes are disabled");
eq(findTag(lists, "li").map(textOf).slice(0, 3), ["done", "todonested", "nested"], "list item text, nested item inside its parent");
eq([findTag(lists, "code").map(textOf), findTag(lists, "strong").map(textOf), findTag(lists, "em").map(textOf), findTag(lists, "del").map(textOf)],
  [["code"], ["bold"], ["em"], ["gone"]], "inline code, strong, em and strike");

console.log("=== raw HTML is rebuilt against the whitelist ===");
const raw = GS.renderMarkdown('<div align="center"><script>alert(1)</script><b onclick="x()">safe</b><a href="javascript:evil()">bad</a><a href="https://example.com">good</a></div>\n');
eq(findTag(raw, "script").length, 0, "script is dropped with its subtree");
eq(textOf(raw).indexOf("alert"), -1, "the dropped script leaves no text behind");
eq(findTag(raw, "b").map(textOf), ["safe"], "a whitelisted tag is rebuilt");
eq(findTag(raw, "b")[0].getAttribute("onclick"), null, "an event handler attribute does not survive");
eq(findTag(raw, "div")[0].getAttribute("align"), "center", "a whitelisted attribute survives");
eq(findTag(raw, "a").map(href), [null, "https://example.com"], "a javascript: href is dropped, an https one is kept");
const unwrapped = GS.renderMarkdown("<section><p>kept</p></section>\n");
eq(findTag(unwrapped, "section").length, 0, "an unlisted tag unwraps");
eq(findTag(unwrapped, "p").map(textOf), ["kept"], "its children are kept");
eq(GS.sanitizeInert(new DOMParser().parseFromString("<em>a</em><style>b{}</style>", "text/html").body, null).map((n) => n.tagName), ["EM"], "sanitizeInert rebuilds a parsed body and drops style");

console.log("=== relative links and images resolve against the document ===");
const rel = GS.renderMarkdown("[doc](sub/GUIDE.md#the-rules)\n\n[up](../TOP.md)\n\n[scheme](ftp://host/f)\n\n![logo](img/logo.png)\n\n![remote](https://cdn.example/x.png)\n\n![bad](ftp://host/x.png)\n",
  { branch: "trunk", dir: "documentation/nested", ctx: null });
eq(findTag(rel, "a").map(href),
  ["#file:documentation/nested/sub/GUIDE.md@trunk:the-rules", "#file:documentation/TOP.md@trunk", null],
  "a relative link becomes a file route with the fragment as a slug, .. climbs, an unknown scheme is dropped");
eq(findTag(rel, "img").map((n) => [n.getAttribute("src"), n.getAttribute("alt")]),
  [[null, "logo"], ["https://cdn.example/x.png", "remote"], [null, "bad"]],
  "a relative image has no src without a bucket, an https one is kept, another scheme is dropped");

console.log("=== heading ids and in-page anchors ===");
const heads = GS.renderMarkdown("# The Rules!\n\n## The Rules!\n\n[jump](#the-rules)\n");
eq([findTag(heads, "h1")[0].getAttribute("id"), findTag(heads, "h2")[0].getAttribute("id")], ["md-the-rules", "md-the-rules-1"], "a repeated heading slug is suffixed");
eq(GS.mdSlug("The Rules!"), "the-rules", "mdSlug strips punctuation and hyphenates");
global.location.hash = "#file:documentation/GUIDE.md@trunk";
const pushed = [];
global.history = { pushState: (_s, _t, url) => pushed.push(url) };
global.__lastFocused = null;
fire(findTag(heads, "a")[0], "click");
eq(pushed, ["#file:documentation/GUIDE.md@trunk:the-rules"], "an in-page anchor on a file route pushes the page's own route plus the slug");
global.location.hash = "#/";
fire(findTag(heads, "a")[0], "click");
eq(pushed[1], "#the-rules", "off a file route the plain fragment is pushed");

console.log("=== code blocks and the fullscreen overlay ===");
const code = GS.renderMarkdown("```go\nfunc main() {}\n```\n");
eq(findTag(code, "pre").map((n) => n.className), ["codeblock"], "a fence renders one code block");
eq(findTag(code, "code").map(textOf), ["func main() {}"], "the fence body renders as text without a tokenizer");
const fsBtn = findTag(code, "button").find((b) => b._cls.has("fs-btn"));
ok(!!fsBtn, "a code block carries a fullscreen button");
fire(fsBtn, "click");
let overlays = () => document.body._children.filter((c) => c.nodeType === 1 && c._cls && c._cls.has("fs-overlay"));
eq(overlays().length, 1, "the button opens one overlay on the body");
eq(document.body.style.overflow, "hidden", "the overlay locks body scrolling");
ok(textOf(overlays()[0]).indexOf("func main() {}") !== -1, "the overlay shows a copy of the code");
fire(findClass(overlays()[0], "fs-close")[0], "click");
eq(overlays().length, 0, "the close button removes the overlay");
eq(document.body.style.overflow, "", "closing restores body scrolling");

const host = GS.el("div", {}, []);
const live = GS.el("p", {}, ["live node"]);
const after = GS.el("p", {}, ["sibling"]);
host.append(live, after);
GS.openFullscreen(live, { live: true });
eq(host._children.map(textOf), ["sibling"], "a live node moves out of its parent into the overlay");
for (const fn of global.__shim.docHandlers.keydown || []) fn({ key: "Escape" });
eq(host._children.map(textOf), ["live node", "sibling"], "Escape closes the overlay and puts the live node back before its sibling");
eq(overlays().length, 0, "Escape removed the overlay");
GS.openFullscreen(null);
eq(overlays().length, 0, "a missing node opens nothing");

console.log("\n" + pass + " passed, " + fail + " failed");
process.exit(fail ? 1 : 0);
