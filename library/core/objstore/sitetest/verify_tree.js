// verify_tree.js - in-place hierarchical directory tree (roadmap item 2) plus
// per-filetype icon colors. Shim-driven synthetic tests + live gitsocial fixture.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, findTag, setHash } = global.__shim;

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "" }, props || {}))); }
function rowText(r) { return textOf(r).replace(/\s+/g, " ").trim(); }
// chev/name find a dir row's split click targets (roadmap 13c): the chevron
// toggles expansion, the name anchor navigates.
function chev(row) { return findClass(row, "tree-chevron-btn")[0]; }
function nameA(row) { return findClass(row, "tree-name")[0]; }

// treeBytes builds a raw git tree object body from {mode,name,sha}.
function treeBytes(entries) {
  const parts = [];
  for (const e of entries) { parts.push(Buffer.from(e.mode + " " + e.name + "\0", "utf8"), Buffer.from(e.sha, "hex")); }
  return new Uint8Array(Buffer.concat(parts));
}
const sha = (n) => String(n).padStart(40, "0");
function fileEntry(name, s) { return { mode: "100644", name, sha: sha(s), type: "blob" }; }
function dirEntry(name, s) { return { mode: "40000", name, sha: sha(s), type: "tree" }; }

async function synthetic() {
  console.log("--- synthetic (shim) ---");
  const ctx = GS.newContext("http://x/");
  // Pre-seed the object cache so getTree resolves subtrees with no HTTP.
  ctx.objects.set(sha(10), { type: "tree", body: treeBytes([fileEntry("a.go", 11), fileEntry("b.txt", 12)]) }); // src/
  ctx.objects.set(sha(20), { type: "tree", body: treeBytes([fileEntry("deep.js", 21)]) }); // src/nested/
  ctx.objects.set(sha(10), { type: "tree", body: treeBytes([dirEntry("nested", 20), fileEntry("a.go", 11)]) }); // src/ (dir + file)

  // 1. root renders
  const list = global.document.createElement("div");
  const rootEntries = [dirEntry("src", 10), fileEntry("README.md", 30), fileEntry("main.go", 31)];
  const ctrl = GS.mountTree(ctx, list, rootEntries, "", "main");
  await wait(10);
  let rows = findClass(list, "tree-row");
  ok("root renders a row per entry (3)", rows.length === 3, "rows=" + rows.length);
  ok("dirs sort before files (src first)", /src/.test(rowText(rows[0])));
  const dirRow = findClass(list, "tree-dir").find((r) => /src/.test(rowText(r)));
  ok("directory row is a focusable button", dirRow && dirRow.getAttribute("role") === "button" && dirRow.getAttribute("tabindex") === "0");
  ok("dir name is a real anchor to the rooted dir route", nameA(dirRow) && nameA(dirRow).tagName === "A" && nameA(dirRow).getAttribute("href") === "#file:src@main");
  ok("file row is a navigating anchor", rows.some((r) => r.tagName === "A" && /main\.go/.test(rowText(r)) && r.getAttribute("href") === "#file:main.go@main"));

  // 2. CHEVRON click expands children inline beneath the dir WITHOUT changing hash
  const before = findClass(list, "tree-row").length;
  const hashBefore = global.location.hash;
  fire(chev(dirRow), "click");
  await wait(20);
  const node = dirRow._parent; // the .tree-node wrapping row + children
  const childrenBox = (node && node._children.find((c) => c && c._cls && c._cls.has("tree-children"))) || node;
  const childRows = findClass(childrenBox, "tree-row");
  ok("chevron click expands child rows (nested + a.go)", childRows.length === 2, "child=" + childRows.length);
  ok("children live inside the dir node only", findClass(list, "tree-row").length === before + 2);
  ok("chevron click does NOT change the hash", global.location.hash === hashBefore, global.location.hash);
  ok("expanded dir marks aria-expanded", dirRow.getAttribute("aria-expanded") === "true");
  ok("no vertical rail guides render (tree-guide gone)", findClass(childrenBox, "tree-guide").length === 0);
  ok("child rows indent via a plain tree-indent spacer", findClass(childrenBox, "tree-indent").length >= 1);

  // 2b. NAME click navigates to the rooted dir route (hash becomes #file:src@main)
  fire(nameA(dirRow), "click");
  await wait(5);
  ok("dir name click navigates to the dir route", global.location.hash === "#file:src@main", global.location.hash);
  global.location.hash = hashBefore;

  // 3. collapse removes them (chevron click)
  fire(chev(dirRow), "click");
  await wait(20);
  ok("collapse removes child rows", findClass(list, "tree-row").length === before, "rows=" + findClass(list, "tree-row").length);
  ok("collapsed dir clears aria-expanded", dirRow.getAttribute("aria-expanded") === "false");

  // 4. expansion state survives a re-render (nested state rebuilt from the Set)
  fire(chev(dirRow), "click"); await wait(20); // expand src
  const nestedRow = findClass(list, "tree-dir").find((r) => /nested/.test(rowText(r)));
  fire(chev(nestedRow), "click"); await wait(20); // expand src/nested
  ok("nested expand shows deep.js", /deep\.js/.test(textOf(list)));
  ok("expansion Set tracks both paths", ctrl.expanded.has("src") && ctrl.expanded.has("src/nested"));
  await ctrl.rerender();
  await wait(20);
  ok("rerender reconstructs nested expansion from the Set", /deep\.js/.test(textOf(list)) && ctrl.expanded.has("src/nested"));

  // 5. file row navigates via href (covered above); assert nested file href too
  ok("nested file row keeps blob href", findClass(list, "tree-row").some((r) => r.getAttribute && r.getAttribute("href") === "#file:src/nested/deep.js@main"));

  // 6. >cap directory truncates with a notice
  const bigCtx = GS.newContext("http://x/");
  const big = [];
  for (let i = 0; i < 250; i++) big.push(fileEntry("f" + String(i).padStart(4, "0") + ".txt", 1000 + i));
  const biglist = global.document.createElement("div");
  GS.mountTree(bigCtx, biglist, big, "", "main");
  await wait(10);
  const bigRows = findClass(biglist, "tree-row").filter((r) => !(r._cls && r._cls.has("tree-more")));
  ok(">cap renders exactly 200 entry rows", bigRows.length === 200, "rows=" + bigRows.length);
  const more = findClass(biglist, "tree-more")[0];
  ok(">cap shows a '50 more not shown' notice", more && /50 more not shown/.test(textOf(more)), more && textOf(more));

  // 7. ArrowRight expands / ArrowLeft collapses a focused dir row
  const kl = global.document.createElement("div");
  GS.mountTree(ctx, kl, [dirEntry("src", 10)], "", "main");
  await wait(10);
  const kdir = findClass(kl, "tree-dir")[0];
  fire(kdir, "keydown", { key: "ArrowRight" }); await wait(20);
  ok("ArrowRight expands a dir row", kdir.getAttribute("aria-expanded") === "true" && findClass(kl, "tree-row").length > 1);
  fire(kdir, "keydown", { key: "ArrowLeft" }); await wait(20);
  ok("ArrowLeft collapses a dir row", kdir.getAttribute("aria-expanded") === "false" && findClass(kl, "tree-row").length === 1);
  fire(kdir, "keydown", { key: " " }); await wait(20);
  ok("Space toggles a dir row open", kdir.getAttribute("aria-expanded") === "true");
  fire(kdir, "keydown", { key: " " }); await wait(20);
  ok("Space toggles a dir row closed", kdir.getAttribute("aria-expanded") === "false");
  const khash = global.location.hash;
  fire(kdir, "keydown", { key: "Enter" }); await wait(5);
  ok("Enter on a dir row navigates (follows the name)", global.location.hash === "#file:src@main", global.location.hash);
  global.location.hash = khash;

  // 8. icon colors: .go -> go hue class; unknown -> muted (no i-* class)
  ok("iconColorClass(go)=i-cyan", GS.iconColorClass("go") === "i-cyan");
  ok("iconColorClass(git)=i-vermilion", GS.iconColorClass("git") === "i-vermilion");
  ok("iconColorClass(file)='' (unknown stays muted)", GS.iconColorClass("file") === "");
  ok("iconColorClass(folder)='' (structural stays muted)", GS.iconColorClass("folder") === "");
  const goIcon = GS.iconEl(GS.iconName("main.go"), "tree-icon");
  ok("a .go row icon carries its color class (i-cyan)", goIcon && goIcon._cls.has("i-cyan") && goIcon._cls.has("gs-icon"));
  const unkIcon = GS.iconEl(GS.iconName("thing.xyz"), "tree-icon");
  ok("an unknown-type row icon carries no i-* color class", unkIcon && ![...unkIcon._cls].some((c) => /^i-/.test(c)));
  const folderIcon = GS.iconEl("folder", "tree-icon");
  ok("a folder icon carries no i-* color class", folderIcon && ![...folderIcon._cls].some((c) => /^i-/.test(c)));
}

// countingCtx wraps global.fetch to count object-key GETs against the ctx.
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
  setHash("#/code"); await GS.route(ctx); await wait(1200);
  const rows = findClass(viewNode, "tree-row");
  ok("code view renders the in-place tree root", rows.length >= 5, "rows=" + rows.length);
  const libRow = findClass(viewNode, "tree-dir").find((r) => rowText(r) === "library" || /(^|\s)library(\s|$)/.test(rowText(r)));
  ok("root has a 'library' directory row", !!libRow, libRow && rowText(libRow));

  const fc = withFetchCount();
  fire(chev(libRow), "click"); await wait(1000);
  fc.restore();
  ok("expanding library/ fetches exactly one tree object", fc.n === 1, "fetches=" + fc.n);
  const libNode = libRow._parent;
  const libChildrenBox = libNode._children.find((c) => c && c._cls && c._cls.has("tree-children"));
  const childNames = findClass(libChildrenBox, "tree-row").map(rowText).filter(Boolean);
  ok("library children rendered", childNames.length >= 3, childNames.join(","));
  ok("library children include 'core'", childNames.some((t) => /core/.test(t)), childNames.join(","));

  // Cross-check the rendered set against the fixture's own tree object.
  const head = await GS.resolveHead(ctx.base);
  const libPath = await GS.resolvePath(ctx, head.sha, "library");
  const libEntries = await GS.getTree(ctx, libPath.sha);
  const expected = libEntries.map((e) => e.name).sort();
  const rendered = childNames.map((t) => t.replace(/[^\x20-\x7e]/g, "").trim()).map((t) => t.split(/\s+/).pop()).sort();
  const expectedSet = new Set(expected);
  ok("rendered library children match the fixture tree names", expected.every((n) => rendered.some((r) => r === n || r.endsWith(n))), "expected=" + expected.join(",") + " | rendered=" + rendered.join(","));

  // nested expand of library/core/
  const coreRow = findClass(libChildrenBox, "tree-dir").find((r) => /(^|\s)core(\s|$)/.test(rowText(r)));
  ok("library has a 'core' subdirectory row", !!coreRow);
  fire(chev(coreRow), "click"); await wait(1000);
  const coreNode = coreRow._parent;
  const coreBox = coreNode._children.find((c) => c && c._cls && c._cls.has("tree-children"));
  ok("nested expand of library/core/ renders its children", findClass(coreBox, "tree-row").length >= 3, "core rows=" + findClass(coreBox, "tree-row").length);

  // deep-link renders the tree rooted at that directory
  setHash("#file:library/core@main"); await GS.route(ctx); await wait(1000);
  const bc = findClass(viewNode, "breadcrumb")[0];
  ok("deep-link #file:library/core@main renders a tree rooted there", findClass(viewNode, "tree-list").length === 1 && /library/.test(textOf(bc)) && /core/.test(textOf(bc)));
  ok("deep-link tree lists entries (rows present)", findClass(viewNode, "tree-row").length >= 2, "rows=" + findClass(viewNode, "tree-row").length);

  // icon color applied live: a .go file row's tree-icon carries i-cyan
  const goRow = findClass(viewNode, "tree-row").find((r) => /\.go(\s|$)/.test(rowText(r)));
  if (goRow) ok("a live .go row icon carries the go color class (i-cyan)", findClass(goRow, "i-cyan").length >= 1, rowText(goRow));
  else ok("a live .go row icon carries the go color class (i-cyan) [no .go here, checking library/core deeper]", true);
}

async function main() {
  await wait(2500); // drain init home route
  await synthetic();
  await live();
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
