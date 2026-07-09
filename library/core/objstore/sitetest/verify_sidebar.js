// verify_sidebar.js - code-context sidebar file tree (scope addition): renders
// under the Code nav slot only in code/dir/blob routes, shares expansion + cache
// with the content tree, highlights the active path, survives blob-to-blob nav.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, key: "" }, props || {}))); }
function rowText(r) { return textOf(r).replace(/\s+/g, " ").trim(); }
const slot = () => global.document.getElementById("nav-tree-slot");
async function run(ctx, h) { setHash(h); await GS.route(ctx); await wait(1200); }

async function main() {
  // Pure mapping unit checks.
  ok("codeSidebarTarget(code) -> root", JSON.stringify(GS.codeSidebarTarget({ type: "code" })) === JSON.stringify({ path: "", branch: null }));
  ok("codeSidebarTarget(file) -> path", GS.codeSidebarTarget({ type: "file", path: "a/b", branch: "main" }).path === "a/b");
  ok("codeSidebarTarget(timeline index) -> null (non-code)", GS.codeSidebarTarget({ type: "index", tab: "timeline" }) === null);
  ok("codeSidebarTarget(commit) -> null", GS.codeSidebarTarget({ type: "commit" }) === null);

  const ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  global.__ctx = ctx;
  await wait(200);

  // Non-code route: sidebar slot is empty.
  await run(ctx, "#/timeline");
  ok("non-code route leaves the sidebar slot empty", findClass(slot(), "nav-tree-list").length === 0, "children=" + (slot()._children || []).length);

  // Code root: sidebar tree renders with rows.
  await run(ctx, "#/code");
  const sb = findClass(slot(), "nav-tree-list")[0];
  ok("#/code renders the sidebar file tree", !!sb && findClass(sb, "tree-row").length >= 5, sb && ("rows=" + findClass(sb, "tree-row").length));
  ok("content pane still shows its own tree (duplication is harmless, both share cache)", findClass(viewNode, "tree-list").length >= 1);

  // Expanding a dir in the sidebar updates the SHARED expansion Set + cache.
  const libRow = findClass(sb, "tree-dir").find((r) => /(^|\s)library(\s|$)/.test(rowText(r)));
  ok("sidebar has a 'library' directory row", !!libRow);
  const objBefore = ctx.objects.size;
  ok("sidebar dir name is an anchor to the rooted route", findClass(libRow, "tree-name")[0] && /^#file:library@/.test(findClass(libRow, "tree-name")[0].getAttribute("href")));
  ok("sidebar tree renders no vertical rail guides", findClass(sb, "tree-guide").length === 0);
  const sbHash = global.location.hash;
  fire(findClass(libRow, "tree-chevron-btn")[0], "click"); await wait(600);
  ok("sidebar chevron toggle does not change the hash", global.location.hash === sbHash, global.location.hash);
  ok("expanding a sidebar dir adds to the shared ctx.treeExpanded", ctx.treeExpanded.has("library"));
  const libNode = libRow._parent;
  const libBox = libNode._children.find((c) => c && c._cls && c._cls.has("tree-children"));
  ok("sidebar dir expands its children in place", findClass(libBox, "tree-row").length >= 3, "kids=" + findClass(libBox, "tree-row").length);

  // Blob route: sidebar highlights the active file and auto-expands its ancestors.
  const fc0 = ctx.objects.size;
  await run(ctx, "#file:library/core/objstore/site/app.js@main");
  const sb2 = findClass(slot(), "nav-tree-list")[0];
  ok("blob route keeps the sidebar tree", !!sb2 && findClass(sb2, "tree-row").length >= 5);
  const active = findClass(sb2, "tree-active");
  ok("sidebar highlights exactly the active file path", active.length >= 1 && active.some((r) => /app\.js/.test(rowText(r))), active.map(rowText).join("|"));
  ok("active file's ancestors are auto-expanded in the sidebar", /objstore/.test(textOf(sb2)) && /site/.test(textOf(sb2)) && ctx.treeExpanded.has("library/core/objstore"));
  ok("content pane shows the blob (app.js source), not a tree", findClass(viewNode, "blob").length >= 1 || /app\.js/.test(textOf(viewNode)));

  // Blob-to-blob navigation keeps the sidebar tree AND its expansion.
  await run(ctx, "#file:library/core/objstore/site/index.html@main");
  const sb3 = findClass(slot(), "nav-tree-list")[0];
  ok("blob-to-blob nav keeps the sidebar tree present", !!sb3 && findClass(sb3, "tree-row").length >= 5);
  ok("prior expansion survives blob-to-blob (library/core/objstore still open)", /site/.test(textOf(sb3)) && ctx.treeExpanded.has("library/core/objstore"));
  const active3 = findClass(sb3, "tree-active");
  ok("active highlight moved to the new file (index.html)", active3.some((r) => /index\.html/.test(rowText(r))), active3.map(rowText).join("|"));

  // Leaving code context clears the sidebar.
  await run(ctx, "#/issues");
  ok("leaving code context clears the sidebar slot", findClass(slot(), "nav-tree-list").length === 0);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
