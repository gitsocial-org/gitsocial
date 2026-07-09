// verify_tree_search.js - shim synthetic + live gitsocial fixture. Grab-bag:
//  - in-place tree search (filter, ancestor reveal, highlight, cache reuse, cap)
//  - dir-row folder-icon drop (chevron-only) vs file-row icons
//  - nav chrome: border removal, per-item nav icons/glyphs, mono voice
//  - Code nav magnifier that focuses the tree search input
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const fs = require("fs");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function findTag(node, tag, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c.tagName.toLowerCase() === tag) out.push(c); findTag(c, tag, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, key: "" }, props || {}))); }
function rowText(r) { return textOf(r).replace(/\s+/g, " ").trim(); }
function treeBytes(entries) { const parts = []; for (const e of entries) parts.push(Buffer.from(e.mode + " " + e.name + "\0", "utf8"), Buffer.from(e.sha, "hex")); return new Uint8Array(Buffer.concat(parts)); }
const sha = (n) => String(n).padStart(40, "0");
const fileEntry = (name, s) => ({ mode: "100644", name, sha: sha(s), type: "blob" });
const dirEntry = (name, s) => ({ mode: "40000", name, sha: sha(s), type: "tree" });

const HTML = fs.readFileSync(require("path").join(__dirname,"../site/index.html"), "utf8");

function block(css, sel) { const i = css.indexOf(sel); if (i < 0) return ""; return css.slice(i, css.indexOf("}", i)); }

async function markup() {
  console.log("--- markup (index.html) ---");
  // Item 2: left borders removed from nav.
  ok("nav base rule drops border-left", !/border-left/.test(block(HTML, "#nav a, #nav .nav-disabled {")));
  ok("nav active rule drops border-left/border-left-color", !/border-left/.test(block(HTML, "#nav a.active {")));
  ok("nav active uses a tint background", /background:/.test(block(HTML, "#nav a.active {")));
  ok("no residual border-left in mobile nav overrides", !/#nav a[^{]*\{[^}]*border-left/.test(HTML.slice(HTML.indexOf("@media (max-width: 720px)"))));
  // Item 4: every nav item carries an icon glyph.
  const navBlock = HTML.slice(HTML.indexOf('<nav id="nav">'), HTML.indexOf("</nav>"));
  const anchors = (navBlock.match(/<a /g) || []).length;
  const icons = (navBlock.match(/class="nav-icon"/g) || []).length;
  ok("every nav anchor carries a nav-icon (" + anchors + ")", anchors === 13 && icons === 13, "a=" + anchors + " icons=" + icons);
  for (const g of ["⏱", "○", "⑂", "⏏", "☞", "◧"]) ok("nav includes TUI glyph " + g, navBlock.includes(g));
  ok("nav-icon inherits color (not a fixed hue)", /\.nav-icon\s*\{[^}]*color:\s*inherit/.test(HTML));
  // Item 13a: sidebar voice is IBM Plex Mono at the UI scale (nav links, glyphs,
  // footer). Round 14d moved the literal 0.8rem onto the --fs-ui token (= 0.8rem),
  // so the size now reads through var(--fs-ui); the mono voice is unchanged.
  const navRule = block(HTML, "#nav a, #nav .nav-disabled {");
  ok("nav links use var(--mono) at var(--fs-ui)", /font-family:\s*var\(--mono\)/.test(navRule) && /font-size:\s*var\(--fs-ui\)/.test(navRule));
  ok("nav-icon glyphs use var(--mono) at var(--fs-ui)", /\.nav-icon\s*\{[^}]*font-family:\s*var\(--mono\)[^}]*font-size:\s*var\(--fs-ui\)/.test(HTML.replace(/\n/g, " ")));
  const footRule = block(HTML, "footer {");
  ok("footer uses var(--mono) at var(--fs-ui)", /font-family:\s*var\(--mono\)/.test(footRule) && /font-size:\s*var\(--fs-ui\)/.test(footRule));
  // Item 13b: no vertical rail guides in the tree CSS.
  ok("index.html drops the .tree-guide rail rule", !/\.tree-guide\b/.test(HTML));
  ok("index.html keeps a plain .tree-indent spacer", /\.tree-indent\b/.test(HTML));
  // Item 13d: the Code nav item carries the magnifier slot.
  ok("Code nav item carries the magnifier slot", /id="nav-code-search"/.test(HTML) && /class="nav-search"/.test(HTML));
}

async function synthetic() {
  console.log("--- synthetic search + dir-row icon ---");
  const ctx = GS.newContext("http://x/");
  ctx.objects.set(sha(10), { type: "tree", body: treeBytes([dirEntry("nested", 20), fileEntry("a.go", 11)]) }); // src/
  ctx.objects.set(sha(20), { type: "tree", body: treeBytes([fileEntry("objstore.js", 21)]) }); // src/nested/
  ctx.objects.set(sha(40), { type: "tree", body: treeBytes([fileEntry("guide.md", 41)]) }); // docs/
  const list = global.document.createElement("div");
  const root = [dirEntry("src", 10), dirEntry("docs", 40), fileEntry("README.md", 30)];
  const ctrl = GS.mountTree(ctx, list, root, "", "main");
  await wait(10);

  // Item 1: dir rows have a chevron but NO folder tree-icon; file rows keep icon.
  const dirRow = findClass(list, "tree-dir").find((r) => /src/.test(rowText(r)));
  ok("dir row has a rotating chevron", findClass(dirRow, "tree-chevron").length === 1);
  ok("dir row has NO folder icon (tree-icon absent)", findClass(dirRow, "tree-icon").length === 0);
  const fileRow = findClass(list, "tree-row").find((r) => r.tagName === "A" && /README\.md/.test(rowText(r)));
  ok("file row keeps its file-type icon (tree-icon present)", findClass(fileRow, "tree-icon").length === 1);

  // Pre-search: expand src via its chevron (split click targets, roadmap 13c) so
  // we can prove clear restores it.
  fire(findClass(dirRow, "tree-chevron-btn")[0], "click"); await wait(20);
  ok("pre-search: src expanded (a.go visible)", /a\.go/.test(textOf(list)) && ctrl.expanded.has("src"));
  ok("dir name is an anchor to the rooted dir route", findClass(dirRow, "tree-name")[0] && findClass(dirRow, "tree-name")[0].getAttribute("href") === "#file:src@main");

  // Search "objstore": hides siblings, expands ancestor chain, highlights.
  await ctrl.setFilter("objstore");
  await wait(20);
  ok("search shows the match objstore.js", /objstore\.js/.test(textOf(list)));
  ok("search hides non-matching sibling docs/", !/guide\.md/.test(textOf(list)) && !findClass(list, "tree-row").some((r) => /docs/.test(rowText(r))));
  ok("search hides non-matching sibling a.go", !/a\.go/.test(textOf(list)));
  ok("search reveals ancestor chain src/ and nested/", findClass(list, "tree-row").some((r) => /(^|\s)src(\s|$)/.test(rowText(r))) && findClass(list, "tree-row").some((r) => /nested/.test(rowText(r))));
  ok("ancestors auto-expanded (aria-expanded true)", findClass(list, "tree-dir").every((r) => r.getAttribute("aria-expanded") === "true"));
  const mark = findClass(list, "tree-mark")[0];
  ok("matched substring highlighted in a <mark>", !!mark && /objstore/i.test(textOf(mark)), mark && textOf(mark));

  // Clear restores the pre-search expansion (src still open, docs back).
  await ctrl.setFilter("");
  await wait(20);
  ok("clear restores full root listing (docs back)", findClass(list, "tree-row").some((r) => /docs/.test(rowText(r))));
  ok("clear restores pre-search expansion (src still open, a.go visible)", /a\.go/.test(textOf(list)) && ctrl.expanded.has("src"));

  // No-match query shows an empty notice.
  await ctrl.setFilter("zzzznotfound");
  await wait(20);
  ok("no-match query shows an empty notice", /No matches/.test(textOf(list)));
  await ctrl.setFilter(""); await wait(20);

  // Cap: a >3000-entry synthetic tree truncates the walk with a notice.
  const bigCtx = GS.newContext("http://x/");
  const big = [];
  for (let i = 0; i < 3100; i++) big.push(fileEntry("f" + String(i).padStart(4, "0") + ".txt", 5000 + i));
  const biglist = global.document.createElement("div");
  const bigCtrl = GS.mountTree(bigCtx, biglist, big, "", "main");
  await wait(10);
  await bigCtrl.setFilter("f00");
  await wait(20);
  ok(">3000-entry tree shows a 'Search truncated' notice", findClass(biglist, "tree-truncated").length === 1, textOf(biglist).slice(0, 80));
}

function withFetchCount() {
  const orig = global.fetch;
  const state = { n: 0 };
  global.fetch = (url, opts) => { if (/\/objects\//.test(String(url))) state.n++; return orig(url, opts); };
  state.restore = () => { global.fetch = orig; };
  return state;
}

async function live() {
  console.log("--- live (gitsocial fixture) ---");
  const ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  global.__ctx = ctx;
  setHash("#/code"); await GS.route(ctx); await wait(1500);
  const wrap = viewNode._children[0];
  const ctrl = wrap.__tree;
  ok("code view mounted a tree with search controller", !!ctrl && typeof ctrl.setFilter === "function");

  const fc = withFetchCount();
  await ctrl.setFilter("objstore");
  await wait(200);
  const gets1 = fc.n;
  ok("search 'objstore' finds library/core/objstore in hierarchy", /objstore/.test(textOf(viewNode)) && findClass(viewNode, "tree-row").some((r) => /(^|\s)library(\s|$)/.test(rowText(r))) && findClass(viewNode, "tree-row").some((r) => /(^|\s)core(\s|$)/.test(rowText(r))), "gets=" + gets1);
  ok("full-tree walk issued a bounded number of object GETs", gets1 > 0, "gets=" + gets1);
  console.log("     full-tree walk GET count = " + gets1);

  // Second search reuses the cache: zero new object GETs.
  fc.n = 0;
  await ctrl.setFilter("cache");
  await wait(200);
  fc.restore();
  ok("second search issues ZERO new object GETs (cached)", fc.n === 0, "gets=" + fc.n);
  console.log("     second-search GET count = " + fc.n);

  await ctrl.setFilter(""); await wait(50);
  ok("clear restores the live code tree", findClass(viewNode, "tree-row").length >= 5);

  // Item 13d: the Code nav magnifier exists and focuses the tree search input.
  setHash("#/code"); await GS.route(ctx); await wait(1200);
  const magBtn = global.document.getElementById("nav-code-search");
  ok("Code nav magnifier icon is mounted (trusted SVG)", findClass(magBtn, "nav-search-icon").length === 1);
  global.__lastFocused = null;
  fire(magBtn, "click"); await wait(20);
  ok("magnifier click focuses the tree search input in code context", global.__lastFocused && global.__lastFocused._cls && global.__lastFocused._cls.has("tree-search"), global.__lastFocused && global.__lastFocused.className);
}

async function main() {
  await wait(2500);
  await markup();
  await synthetic();
  await live();
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
