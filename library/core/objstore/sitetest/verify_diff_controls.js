// verify_diff_controls.js - drives the real route() through the shim. Grab-bag:
//  - diff bulk controls (expand/collapse all, unified/split mode, fullscreen)
//  - file-type icons (iconName units, tree/breadcrumb/diff-file svg icons)
//  - commit-message markdown rendering (headings, code, lists, multi-line)
require("./shim.js");
require("../site/icons.js"); // sets window.GSIcons
const GS = require("../site/gs-app.js");
const { viewNode, textOf, findTag, setHash } = global.__shim;

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function run(hash) { setHash(hash); await GS.route(global.__ctx); await wait(600); }
function fire(node, ev, props) { (node && node._handlers && node._handlers[ev] || []).forEach((fn) => fn(Object.assign({ preventDefault() {}, stopPropagation() {}, shiftKey: false, key: "" }, props || {}))); }
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
function btn(label) { return findTag(viewNode, "button").find((b) => textOf(b).trim() === label); }

async function main() {
  await wait(2500); // drain the auto-run init() home route so it can't clobber our setView
  // ---- Item 2: iconName unit tests ----
  const cases = [
    ["main.go", null, "go"], ["app.js", null, "js"], ["x.tsx", null, "ts"],
    ["c.json", null, "json"], ["c.yaml", null, "yaml"], ["r.md", null, "md"],
    ["Dockerfile", null, "docker"], ["Makefile", null, "gear"], [".gitignore", null, "git"],
    ["a.rs", null, "rust"], ["a.py", null, "py"], ["x.svg", null, "svg"],
    ["img.png", null, "image"], ["a.zip", null, "zip"], ["thing.xyz", null, "file"],
    ["noext", null, "file"], ["x", "tree", "folder"], ["x", "tree-open", "folder-open"],
    ["x", "commit", "git"], ["x", "symlink", "symlink"], ["package.json", null, "npm"],
  ];
  for (const [name, kind, want] of cases) ok("iconName(" + name + (kind ? "," + kind : "") + ")=" + want, GS.iconName(name, kind) === want, GS.iconName(name, kind));

  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");

  // ---- Item 2: tree rows carry svg icons; breadcrumb carries no file icon ----
  await run("#/code");
  const treeSvgs = findTag(viewNode, "svg");
  ok("code tree rows carry svg icons", treeSvgs.length >= 3, "svgs=" + treeSvgs.length);
  ok("code breadcrumb carries no file icon (bc-icon removed)", findClass(findClass(viewNode, "breadcrumb")[0] || viewNode, "bc-icon").length === 0);
  const gsIconSpans = findClass(viewNode, "gs-icon");
  ok("tree rows use gs-icon wrapper spans", gsIconSpans.length >= 3, "gs-icon=" + gsIconSpans.length);

  await run("#file:go.mod@main");
  ok("blob breadcrumb carries no file icon (bc-icon removed)", findClass(findClass(viewNode, "breadcrumb")[0] || viewNode, "bc-icon").length === 0);

  // ---- Item 1 + Item 2 on a 6-file commit ----
  await run("#commit:e898f386804bb1579cc976faa0609dd2deadbafd@main");
  const files0 = findClass(viewNode, "diff-file");
  ok("commit changes lists 6 files", files0.length === 6, "files=" + files0.length);
  ok("diff file rows carry file-type icons", findClass(viewNode, "diff-file-icon").length === 6, "icons=" + findClass(viewNode, "diff-file-icon").length);
  const bodies0 = findClass(viewNode, "diff-file-body");
  const collapsed0 = bodies0.filter((b) => b.style.display === "none").length;
  ok("6-file commit does NOT auto-expand (all collapsed)", collapsed0 === 6, "collapsed=" + collapsed0);

  // Single expand/collapse toggle (⊞ Expand all / ⊟ Collapse all glyph labels)
  // and single mode toggle (⇄ <target mode>), per the current diff controls.
  const expandToggle = () => findClass(viewNode, "expand-toggle")[0];
  const modeToggle = () => findClass(viewNode, "mode-toggle")[0];
  ok("Expand-all toggle present (glyph label)", !!expandToggle() && textOf(expandToggle()).includes("Expand all"), textOf(expandToggle() || {}));
  ok("mode toggle present (⇄ target)", !!modeToggle() && /⇄/.test(textOf(modeToggle())), textOf(modeToggle() || {}));
  ok("fullscreen-section control present", !!btn("⤢"));

  fire(expandToggle(), "click");
  await wait(1500);
  const bodiesE = findClass(viewNode, "diff-file-body");
  const shown = bodiesE.filter((b) => b.style.display !== "none").length;
  const withContent = bodiesE.filter((b) => findClass(b, "diff-line").length > 0 || findClass(b, "notice").length > 0 || findClass(b, "empty").length > 0).length;
  ok("Expand all opens all 6 file bodies", shown === 6, "shown=" + shown);
  ok("Expand all fetched+rendered all 6 diffs", withContent === 6, "content=" + withContent);
  ok("toggle label flips to Collapse all after expand", textOf(expandToggle()).includes("Collapse all"), textOf(expandToggle() || {}));

  // Mode toggle re-renders expanded files as split, then back to unified
  fire(modeToggle(), "click");
  await wait(200);
  ok("mode toggle applies split globally (diff-body.split present)", findClass(viewNode, "split").length >= 1, "split=" + findClass(viewNode, "split").length);
  fire(modeToggle(), "click");
  await wait(200);
  ok("mode toggle restores unified body", findClass(viewNode, "unified").length >= 1);

  // Collapse all via the same toggle (now labeled Collapse all)
  fire(expandToggle(), "click");
  await wait(50);
  const bodiesC = findClass(viewNode, "diff-file-body").filter((b) => b.style.display === "none").length;
  ok("Collapse all collapses all 6 bodies", bodiesC === 6, "collapsed=" + bodiesC);

  // Fullscreen whole section
  const before = (global.document.body._children || []).length;
  fire(btn("⤢"), "click");
  await wait(50);
  const overlays = findClass(global.document.body, "fs-overlay");
  ok("fullscreen opens an overlay on document.body", overlays.length === 1, "overlays=" + overlays.length);
  const closeBtn = findClass(overlays[0] || global.document.body, "fs-close")[0];
  fire(closeBtn, "click");
  await wait(50);
  ok("fullscreen close removes the overlay", findClass(global.document.body, "fs-overlay").length === 0);

  // ---- Item 3: commit-message markdown (thread-demo) ----
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/thread-demo/");
  await run("#commit:387bdba91d0b@gitmsg/social");
  const mds = findClass(viewNode, "markdown");
  ok("markdown post detail renders a .markdown body", mds.length >= 1, "markdown=" + mds.length);
  ok("markdown body renders a body heading (<h2> Release notes)", findTag(viewNode, "h2").some((h) => textOf(h).includes("Release notes")));
  ok("markdown body renders a fenced code block", findClass(viewNode, "codeblock").length >= 1);
  ok("markdown body renders a list", findTag(viewNode, "li").length >= 2, "li=" + findTag(viewNode, "li").length);
  ok("markdown body renders bold text", findTag(viewNode, "strong").length >= 1);

  await run("#commit:3ef580d8c046@gitmsg/social");
  const t = textOf(viewNode);
  const brs = findTag(viewNode, "br").length;
  ok("plain multi-line post preserves all three lines", t.includes("first line") && t.includes("second line") && t.includes("third line"));
  ok("plain multi-line post keeps line breaks as <br> (no paragraph collapse)", brs >= 1, "br=" + brs);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
