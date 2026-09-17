// unit_render_code.js - shim-rendered units for the code surfaces, over an
// object store seeded in the read context: the file tree and its search, the
// blob view, and the diff a commit detail renders in both view modes.
require("./shim.js");
const GS = require("../assets/gs-render.js");
const { textOf, findTag } = global.__shim;
let pass = 0, fail = 0;
function eq(a, b, msg) { if (JSON.stringify(a) === JSON.stringify(b)) { pass++; } else { fail++; console.log("FAIL", msg, "got", JSON.stringify(a), "want", JSON.stringify(b)); } }
function ok(c, msg, extra) { if (c) { pass++; } else { fail++; console.log("FAIL", msg, extra == null ? "" : extra); } }
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { for (const fn of (node && node._handlers && node._handlers[ev]) || []) fn(Object.assign({ preventDefault() {}, stopPropagation() {}, key: "", shiftKey: false, target: { closest() { return null; } } }, props || {})); }
const tick = (ms) => new Promise((r) => setTimeout(r, ms || 5));

// Nothing is served: every bucket read misses, so only the seeded objects answer.
global.fetch = async () => ({ ok: false, status: 404, headers: { get: () => null }, text: async () => "", arrayBuffer: async () => new ArrayBuffer(0) });

const enc = new TextEncoder();
const sha = (s) => (s + "0".repeat(40)).slice(0, 40);
// treeBody encodes tree entries the way git stores them, so parseTree reads them back.
function treeBody(entries) {
  const parts = [];
  for (const e of entries) {
    parts.push(enc.encode(e.mode + " " + e.name + "\0"));
    const b = new Uint8Array(20);
    for (let i = 0; i < 20; i++) b[i] = parseInt(e.sha.slice(i * 2, i * 2 + 2), 16);
    parts.push(b);
  }
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}
const ctx = { base: "http://seeded/", objects: new Map(), refMisses: new Set(), treeExpanded: new Set(), walks: {}, packs: { names: [], packed: null, maps: new Map(), idx: new Map(), size: new Map(), windows: new Map(), lastHit: null } };
const put = (s, type, body) => ctx.objects.set(sha(s), { type, body: typeof body === "string" ? enc.encode(body) : body });

const middle = Array.from({ length: 18 }, (_, i) => "line " + (i + 2)).join("\n");
const OLD_GO = "package old\n" + middle + "\nfunc last() {}\n";
const NEW_GO = "package new\n" + middle + "\nfunc last() { return }\n";
put("b1", "blob", OLD_GO);
put("b2", "blob", NEW_GO);
put("b3", "blob", "# Title\n\nA paragraph.\n");
put("b4", "blob", "deep text\n");
put("b5", "blob", "leaf text\n");
put("bb", "blob", new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3]));
put("b6", "blob", "package lib\n");
put("b7", "blob", "../README.md");
put("b8", "blob", (() => { const b = new Uint8Array(2048); b[7] = 0; b[8] = 0x41; return b; })());
put("b9", "blob", "version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\nsize 12345678\n");
put("a7", "tree", treeBody([{ mode: "100644", name: "leaf.md", sha: sha("b5") }]));
put("a9", "tree", treeBody([{ mode: "40000", name: "nested", sha: sha("a7") }, { mode: "100644", name: "deep.txt", sha: sha("b4") }]));
put("a6", "tree", treeBody([
  { mode: "100644", name: "util.go", sha: sha("b6") },
  { mode: "100644", name: "data.bin", sha: sha("b8") },
  { mode: "100644", name: "model.bin", sha: sha("b9") },
  { mode: "120000", name: "LINK.md", sha: sha("b7") },
  { mode: "160000", name: "vendor", sha: sha("ab") },
]));
const rootEntries = (goSha) => treeBody([
  { mode: "40000", name: "docs", sha: sha("a9") },
  { mode: "40000", name: "lib", sha: sha("a6") },
  { mode: "100644", name: "README.md", sha: sha("b3") },
  { mode: "100644", name: "logo.png", sha: sha("bb") },
  { mode: "100644", name: "main.go", sha: goSha },
]);
put("a1", "tree", rootEntries(sha("b2")));
put("a0", "tree", rootEntries(sha("b1")));
put("c0", "commit", "tree " + sha("a0") + "\nauthor Ada <ada@example.com> 1750000000 +0000\n\nRoot commit\n");
put("c1", "commit", "tree " + sha("a1") + "\nparent " + sha("c0") + "\nauthor Ada <ada@example.com> 1750000100 +0000\n\nRewrite the last line\n\nThe body of the commit.\n");
ctx.head = Promise.resolve({ branch: "refs/heads/trunk", sha: sha("c1") });
ctx.manifest = Promise.resolve({ "refs/heads/trunk": sha("c1") });

async function main() {
  console.log("=== the code route renders the default branch root ===");
  const code = (await GS.codeView(ctx))[0];
  eq(findClass(code, "breadcrumb").map(textOf), ["trunk"], "the breadcrumb names the branch at the root");
  eq(findClass(code, "tree-row").map((r) => textOf(r).replace(/[^\w./ -]/g, "").trim()), ["docs", "lib", "logo.png", "main.go", "README.md"], "directories sort before files, each alphabetical");
  eq(findClass(code, "tree-row").filter((r) => r.tagName === "A").map((r) => r.getAttribute("href")),
    ["#file:logo.png@trunk", "#file:main.go@trunk", "#file:README.md@trunk"], "a file row links to its file route");
  eq(GS.focusTreeSearch(), true, "the mounted tree search input takes focus");
  ok(global.__lastFocused === findClass(code, "tree-search")[0], "the focused node is the search box");

  console.log("=== a directory expands, collapses and navigates ===");
  const dirRow = findClass(code, "tree-dir")[0];
  eq(dirRow.getAttribute("aria-expanded"), "false", "a directory starts collapsed");
  fire(dirRow, "click");
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "true", "clicking the row expands it");
  eq(findClass(code, "tree-row").map((r) => textOf(r).replace(/[^\w./ -]/g, "").trim()).slice(0, 3), ["docs", "nested", "deep.txt"], "the expanded level lists its own entries, directories first");
  fire(dirRow, "keydown", { key: "ArrowLeft" });
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "false", "ArrowLeft collapses the directory");
  fire(dirRow, "keydown", { key: "ArrowRight" });
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "true", "ArrowRight expands it again");
  fire(dirRow, "keydown", { key: " " });
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "false", "Space toggles it shut");
  global.location.hash = "#/";
  fire(dirRow, "keydown", { key: "Enter" });
  eq(global.location.hash, "#file:docs@trunk", "Enter opens the directory's own route");
  global.location.hash = "#/";
  fire(findClass(dirRow, "tree-name")[0], "click");
  eq(global.location.hash, "#file:docs@trunk", "the directory name navigates instead of toggling");
  global.location.hash = "#/";
  fire(findClass(dirRow, "tree-name")[0], "click", { metaKey: true });
  eq(global.location.hash, "#/", "a modified click is left to the browser");
  const chevron = findClass(dirRow, "tree-chevron-btn")[0];
  eq(chevron.getAttribute("aria-label"), "Expand docs", "the chevron names what it does");
  fire(chevron, "click");
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "true", "the chevron alone expands the directory");
  eq(findClass(dirRow, "tree-chevron-btn")[0].getAttribute("aria-label"), "Collapse docs", "and its label follows the state");
  fire(findClass(dirRow, "tree-chevron-btn")[0], "click");
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "false", "a second chevron click collapses it");
  fire(dirRow, "click", { target: { closest: (s) => (s === "a, .tree-chevron-btn" ? {} : null) } });
  await tick(20);
  eq(dirRow.getAttribute("aria-expanded"), "false", "a row click that landed on the name or chevron does not toggle twice");

  console.log("=== the tree search filters in place ===");
  const searchBox = findClass(code, "tree-search")[0];
  searchBox.value = "leaf";
  fire(searchBox, "input");
  await tick(220);
  eq(findTag(code, "mark").map(textOf), ["leaf"], "typing filters the tree after the debounce");
  fire(searchBox, "keydown", { key: "Escape" });
  await tick(30);
  eq(searchBox.value, "", "Escape clears the query");
  eq(findTag(code, "mark").length, 0, "and restores the unfiltered tree");
  await code.__tree.setFilter("leaf");
  const marks = findTag(code, "mark");
  eq(marks.map(textOf), ["leaf"], "the matching part of the name is marked");
  eq(findClass(code, "tree-row").map((r) => textOf(r).replace(/[^\w./ -]/g, "").trim()), ["docs", "nested", "leaf.md"], "a match shows with its ancestor chain expanded");
  await code.__tree.setFilter("zzz");
  eq(findClass(code, "empty").map(textOf), ["No matches for “zzz”."], "a query with no match says so");
  await code.__tree.setFilter("");
  eq(findClass(code, "tree-row").length, 5, "an empty query restores the root level");
  await code.__tree.setFilter("MAIN");
  eq(findClass(code, "tree-row").map((r) => textOf(r).replace(/[^\w./ -]/g, "").trim()), ["main.go"], "the search is case-insensitive");
  await code.__tree.setFilter("");

  console.log("=== blob views ===");
  const goBlob = (await GS.treeOrBlob(ctx, "main.go", "trunk", 3, 5))[0];
  const rows = findClass(goBlob, "blob-row");
  eq(rows.length, 21, "every source line gets a row");
  eq(rows.filter((r) => r._cls.has("hl")).map((r) => textOf(findClass(r, "ln")[0])), ["3", "4", "5"], "the requested line range is highlighted");
  ok(textOf(goBlob).indexOf("package new") !== -1, "the blob shows the file contents", textOf(goBlob).slice(0, 40));
  global.location.hash = "#/";
  fire(findClass(rows[6], "ln")[0], "click");
  eq(global.location.hash, "#file:main.go@trunk:L7", "clicking a line number addresses that line");
  fire(findClass(rows[9], "ln")[0], "click", { shiftKey: true });
  eq(global.location.hash, "#file:main.go@trunk:L7-10", "a shift-click extends the anchor into a range");

  const imgBlob = (await GS.treeOrBlob(ctx, "logo.png", "trunk"))[0];
  const img = findTag(imgBlob, "img");
  eq(img.length, 1, "an image path renders an image, not a binary notice");
  ok(/^blob:/.test(img[0].getAttribute("src")), "the image reads from an object URL", img[0].getAttribute("src"));
  eq(img[0].getAttribute("alt"), "logo.png", "the image falls back to its path");
  GS.revokeObjectUrls();
  ok(true, "revoking the tracked object URLs does not throw");

  const mdBlob = (await GS.treeOrBlob(ctx, "README.md", "trunk"))[0];
  eq(findTag(mdBlob, "h1").map(textOf), ["Title"], "a markdown blob renders as prose");
  const rawBtn = findClass(mdBlob, "view-toggle")[0];
  ok(!!rawBtn, "a markdown blob offers the Raw toggle");
  fire(rawBtn, "click");
  eq(findTag(mdBlob, "h1").length, 0, "Raw drops the rendered prose");
  eq(findClass(mdBlob, "blob-row").length, 4, "Raw shows the numbered source");
  eq(rawBtn.getAttribute("aria-pressed"), "true", "the toggle reports its state");
  fire(rawBtn, "click");
  eq(findTag(mdBlob, "h1").map(textOf), ["Title"], "toggling back restores the prose");

  console.log("=== a blob view labels the object it cannot render ===");
  const labelOf = (node) => findClass(node, "empty").map(textOf);
  const binBlob = (await GS.treeOrBlob(ctx, "lib/data.bin", "trunk"))[0];
  eq(labelOf(binBlob), ["Binary file, 2.0 KB"], "a binary blob is labeled with its size");
  eq(findClass(binBlob, "blob-row").length, 0, "a labeled object renders no source lines");
  const lfsBlob = (await GS.treeOrBlob(ctx, "lib/model.bin", "trunk"))[0];
  eq(labelOf(lfsBlob), ["Git LFS pointer"], "an LFS pointer is labeled, not printed as its three lines");
  eq(findClass(lfsBlob, "blob-row").length, 0, "the pointer body stays off the page");
  const subBlob = (await GS.treeOrBlob(ctx, "lib/vendor", "trunk"))[0];
  eq(labelOf(subBlob), ["Submodule at ab0000000000"], "a submodule is labeled with the commit it pins");
  eq(findClass(subBlob, "empty")[0].getAttribute("title"), sha("ab"), "the full sha rides the label title");
  const linkBlob = (await GS.treeOrBlob(ctx, "lib/LINK.md", "trunk"))[0];
  eq(labelOf(linkBlob), ["Symlink to ../README.md"], "a symlink is labeled with its target, not rendered as prose");
  eq(findClass(linkBlob, "view-toggle").length, 0, "a symlink to markdown offers no Raw toggle");

  const dirView = (await GS.treeOrBlob(ctx, "docs", "trunk"))[0];
  eq(findClass(dirView, "breadcrumb").map(textOf), ["trunk / docs"], "a directory path renders the tree with its breadcrumb");
  eq(textOf((await GS.treeOrBlob(ctx, "nope.txt", "trunk"))[0]), "Path not found: nope.txt", "a missing path says so");
  eq(textOf((await GS.treeOrBlob(ctx, "main.go", "nobranch"))[0]), "Branch not found: nobranch", "a missing branch says so");

  console.log("=== a commit detail renders its changes ===");
  const detail = (await GS.commitDetail(ctx, sha("c1"), "trunk"))[0];
  await tick(30);
  eq(textOf(findClass(detail, "subject")[0]), "Rewrite the last line", "the commit subject heads the page");
  ok(textOf(detail).indexOf("The body of the commit.") !== -1, "the commit body renders");
  eq(findClass(detail, "diff-path").map(textOf), ["main.go"], "the one changed file is listed");
  eq(findClass(detail, "diff-status").map(textOf), ["M"], "a modified file is marked M");
  ok(textOf(findClass(detail, "diff-head")[0]).indexOf("Changes (1)") !== -1, "the section counts the changed files");
  eq(findClass(detail, "cnt-add").map(textOf), ["+2"], "the added line count shows");
  eq(findClass(detail, "cnt-del").map(textOf), ["-2"], "the deleted line count shows");
  const signs = findClass(detail, "diff-line").map((r) => textOf(findClass(r, "dl-sign")[0]) + textOf(findClass(r, "dl-text")[0]));
  ok(signs.indexOf("-package old") !== -1 && signs.indexOf("+package new") !== -1, "the changed first line shows on both sides", signs.slice(0, 6).join(" | "));
  const sep = findClass(detail, "diff-expand")[0];
  ok(!!sep, "the unchanged middle collapses into a divider");
  eq(textOf(sep).replace(/[^\w ]/g, "").trim(), "12 unchanged lines", "the divider counts the rows it hides");
  const before = findClass(detail, "diff-line").length;
  fire(sep, "keydown", { key: "Enter" });
  eq(findClass(detail, "diff-line").length, before + 12, "Enter on the divider reveals the skipped rows");
  const rawToggle = findClass(detail, "view-toggle")[0];
  fire(rawToggle, "click");
  eq(findClass(detail, "raw-body").map(textOf), ["Rewrite the last line\n\nThe body of the commit.\n"], "Raw shows the verbatim commit message");
  fire(rawToggle, "click");
  eq(findClass(detail, "raw-body").length, 0, "toggling back drops the raw pane");
  const fileHead = findClass(detail, "diff-file-head")[0];
  fire(fileHead, "click");
  await tick(10);
  eq(findClass(detail, "diff-file-body")[0].style.display, "none", "clicking a file head collapses that file");
  fire(fileHead, "click");
  await tick(20);
  eq(findClass(detail, "diff-file-body")[0].style.display, "", "clicking it again expands the file");
  fire(findClass(detail, "diff-controls")[0]._children[2], "click");
  eq(document.body._children.filter((c) => c.nodeType === 1 && c._cls && c._cls.has("fs-overlay")).length, 1, "the section's fullscreen control opens an overlay");
  fire(findClass(document.body, "fs-close")[0], "click");

  console.log("=== the diff view mode toggle ===");
  const modeBtn = findClass(detail, "mode-toggle")[0];
  eq(textOf(modeBtn), "⇄ Split", "the toggle names the mode a click switches to");
  fire(modeBtn, "click");
  await tick(10);
  ok(findClass(detail, "ds-code").length > 0, "split mode lays the diff out in side-by-side cells");
  eq(localStorage.getItem("diffview"), "split", "the chosen mode is persisted");
  eq(textOf(modeBtn), "⇄ Unified", "the toggle now names unified");
  const splitSep = findClass(detail, "diff-expand")[0];
  fire(splitSep, "click");
  ok(findClass(detail, "ds-code").length > 0, "revealing rows in split mode keeps the grid");
  fire(modeBtn, "click");
  await tick(10);
  eq(localStorage.getItem("diffview"), "unified", "switching back persists unified");

  console.log("=== expand and collapse all ===");
  const expandBtn = findClass(detail, "expand-toggle")[0];
  eq(textOf(expandBtn), "⊟ Collapse all", "a small diff opens expanded");
  fire(expandBtn, "click");
  await tick(10);
  eq(textOf(expandBtn), "⊞ Expand all", "collapsing every file flips the control");
  eq(findClass(detail, "diff-file-body")[0].style.display, "none", "a collapsed file hides its body");
  fire(expandBtn, "click");
  await tick(20);
  eq(textOf(expandBtn), "⊟ Collapse all", "expanding every file flips it back");
  eq(findClass(detail, "diff-file-body")[0].style.display, "", "the body shows again");
  ok(findClass(detail, "diff-line").length > 0, "the expanded file carries its rows");

  eq(textOf((await GS.commitDetail(ctx, sha("ee"), "trunk"))[0]), "Commit not found: " + sha("ee"), "an unknown sha says so");

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); });
