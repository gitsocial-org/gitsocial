// verify_chrome.js - app chrome + layout against the live legacy buckets. Grab-bag:
//  - one-liner footer
//  - sidebar header row order (title, theme, collapse)
//  - floating fullscreen overlay close + Escape
//  - GitHub-familiar Home (root file listing, README, Show-all)
//  - contributor block relocated from Home to Analytics
//  - whole-card clickable timeline cards (selection-aware)
//  - single-button diff expand/collapse + unified/split mode toggles
//  - font-size token scale + breadcrumb tail icon
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const fs = require("fs");
const { viewNode, textOf, findTag, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out){ out=out||[]; for(const c of node._children||[]){ if(c&&c.nodeType===1){ if(c._cls&&c._cls.has(cls)) out.push(c); findClass(c,cls,out);} } return out; }
function hasClassDeep(node, cls){ return findClass(node, cls).length>0; }
function fire(node, ev, props){ (node&&node._handlers&&node._handlers[ev]||[]).forEach(fn=>fn(Object.assign({preventDefault(){},stopPropagation(){},key:""},props||{}))); }
async function run(hash){ setHash(hash); await GS.route(global.__ctx); await wait(700); }

const HTML = fs.readFileSync(require("path").join(__dirname,"../site/index.html"), "utf8") + "\n" + fs.readFileSync(require("path").join(__dirname,"../site/pages-app.css"), "utf8");

async function main() {
  await wait(2500); // drain init home route

  // ---- A: brand credit moved into a sidebar footer (page <footer> removed) ----
  ok("A page-wide <footer> element removed", !/<\/footer>/.test(HTML));
  const navFoot = HTML.slice(HTML.indexOf('class="nav-footer"'), HTML.indexOf("</aside>"));
  ok("A sidebar footer links https://gitsocial.org", /href="https:\/\/gitsocial\.org"/.test(navFoot));
  ok("A sidebar footer has a single credit line", (navFoot.match(/Built with gitsocial/gi) || []).length === 1);
  ok("A sidebar footer keeps the brand logo", /class="logo-small"/.test(navFoot));
  ok("A sidebar footer sits inside the nav aside (before </aside>)", HTML.indexOf('class="nav-footer"') < HTML.indexOf("</aside>") && HTML.indexOf('class="nav-footer"') > HTML.indexOf('class="nav"'));

  // ---- C: sidebar header row (theme icon BEFORE collapse, same line as title) ----
  const nh = HTML.slice(HTML.indexOf('class="nav-header"'), HTML.indexOf("</aside>"));
  const iTitle = nh.indexOf('id="repo-title"'), iTheme = nh.indexOf('id="theme-toggle"'), iCollapse = nh.indexOf('id="nav-collapse"');
  ok("C nav-header holds title, theme, collapse", iTitle >= 0 && iTheme >= 0 && iCollapse >= 0);
  ok("C order: title < theme < collapse (theme before collapse)", iTitle < iTheme && iTheme < iCollapse, iTitle + "," + iTheme + "," + iCollapse);
  ok("C theme toggle is icon-only (no theme-label)", !/theme-label/.test(HTML));
  ok("C sidebar-footer theme switch removed", !/theme-switch/.test(HTML));

  // ---- D: fullscreen overlay floating close, no fs-bar ----
  const target = GS.route ? global.document.createElement("div") : null;
  target.append("code sample");
  GS.openFullscreen(target);
  await wait(20);
  const overlays = findClass(global.document.body, "fs-overlay");
  ok("D fullscreen opens overlay", overlays.length === 1);
  const ov = overlays[0];
  ok("D overlay has no fs-bar", !hasClassDeep(ov, "fs-bar"));
  ok("D overlay has fs-content and a floating fs-close", hasClassDeep(ov, "fs-content") && hasClassDeep(ov, "fs-close"));
  ok("D fs-close is a direct child of the overlay (floating, not in a bar)", (ov._children || []).some((c) => c._cls && c._cls.has("fs-close")));
  const closeBtn = findClass(ov, "fs-close")[0];
  ok("D fs-close is a circle (circle class present)", !!closeBtn && closeBtn._cls.has("circle"));
  ok("D fs-close is X-only (no text label)", textOf(closeBtn).trim() === "✕", JSON.stringify(textOf(closeBtn)));
  fire(global.__shim.docHandlers.keydown && global.document, "keydown"); // no-op guard
  (global.__shim.docHandlers.keydown || []).forEach((fn) => fn({ key: "Escape" }));
  await wait(20);
  ok("D Escape closes the overlay", findClass(global.document.body, "fs-overlay").length === 0);

  // ---- E + B: Home GitHub-familiar (gitsocial: 16 root entries, has README) ----
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  await run("#/");
  const wrap = viewNode._children[0];
  const kids = wrap._children.filter((c) => c && c.nodeType === 1);
  const idxFiles = kids.findIndex((k) => hasClassDeep(k, "tree-list"));
  const idxReadme = kids.findIndex((k) => k._cls && k._cls.has("markdown"));
  const idxContrib = kids.findIndex((k) => k._cls && k._cls.has("contrib"));
  ok("E home lists root files (tree-list present)", idxFiles >= 0);
  ok("E home renders README below the file list", idxReadme > idxFiles, "files@" + idxFiles + " readme@" + idxReadme);
  // ROUND UPDATE: contributor block moved OFF Home to the dedicated Analytics page.
  ok("B contributor block removed from Home", idxContrib === -1, "contrib@" + idxContrib);
  ok("B Home has no commit-count text", !/commits on main/.test(textOf(wrap)));
  ok("E home NOT missing the 'No README' advisory (README present)", !/No README found/.test(textOf(wrap)));

  // Long-listing: gitsocial root = 16 > HOME_FILE_LIMIT (now 3), so a Show all
  // affordance appears and 16-3=13 rows are hidden while collapsed.
  const showAll = findTag(viewNode, "button").find((b) => /^Show all/.test(textOf(b)));
  ok("E long root listing shows a 'Show all N' affordance", !!showAll && /Show all 16/.test(textOf(showAll)), showAll && textOf(showAll));
  const hiddenBefore = findClass(viewNode, "tree-row").filter((r) => r.style.display === "none").length;
  ok("E extra rows hidden before expansion (16 - HOME_FILE_LIMIT 3 = 13)", hiddenBefore === 13, "hidden=" + hiddenBefore);
  fire(showAll, "click");
  await wait(20);
  const hiddenAfter = findClass(viewNode, "tree-row").filter((r) => r.style.display === "none").length;
  ok("E 'Show all' reveals every root entry", hiddenAfter === 0, "hidden=" + hiddenAfter);

  // ---- B: Analytics page carries the (rebuilt) top-authors ranking block ----
  await run("#/analytics");
  ok("B Analytics page shows the contributor block", hasClassDeep(viewNode, "contrib"), textOf(viewNode).slice(0, 60));
  ok("B Analytics shows the top-authors ranking", /Authors \d/.test(textOf(viewNode)));

  // ---- E: Home without README (demo-project: 1 file, no README) ----
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/demo-project/");
  await run("#/");
  ok("E home-without-README still lists root files", hasClassDeep(viewNode, "tree-list"));
  ok("E home-without-README shows NO advisory text", !/No README found/.test(textOf(viewNode)));
  ok("E home-without-README renders no markdown README block", findClass(viewNode, "markdown").length === 0);

  // ---- F: item cards are whole-card clickable (nav on padding, links intact) ----
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/demo-project/");
  await run("#/timeline");
  const card = findClass(viewNode, "clickable")[0];
  ok("F social cards are whole-card clickable", !!card && card._cls.has("card"));
  const hashLink = findClass(card, "hash")[0];
  global.__selection = { isCollapsed: true };
  global.location.hash = "#/timeline";
  fire(card, "click", { target: card });
  ok("F click on card padding navigates to #commit:", /^#commit:/.test(global.location.hash), global.location.hash);
  global.location.hash = "#/timeline";
  fire(card, "click", { target: hashLink });
  ok("F click on inner hash anchor does not trigger card nav (keeps its own href)", global.location.hash === "#/timeline", global.location.hash);
  global.__selection = { isCollapsed: false };
  global.location.hash = "#/timeline";
  fire(card, "click", { target: card });
  ok("F active text selection suppresses card nav", global.location.hash === "#/timeline", global.location.hash);
  global.__selection = { isCollapsed: true };

  // ---- G: single-button diff toggles (gitsocial commit with 6 changed files,
  // above the auto-expand threshold so everything starts collapsed) ----
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  await run("#commit:e898f386804bb1579cc976faa0609dd2deadbafd@main");
  const expT = findClass(viewNode, "expand-toggle")[0];
  const modeT = findClass(viewNode, "mode-toggle")[0];
  ok("G exactly one expand/collapse toggle", findClass(viewNode, "expand-toggle").length === 1);
  ok("G old two-button pair gone (no diff-modes, no Unified|Split pair)", findClass(viewNode, "diff-modes").length === 0 && findClass(viewNode, "diff-toggle").length === 0);
  ok("G expand toggle starts as 'Expand all'", textOf(expT) === "⊞ Expand all", textOf(expT));
  ok("G mode toggle names the TARGET mode (unified -> '⇄ Split')", textOf(modeT) === "⇄ Split", textOf(modeT));
  fire(expT, "click");
  await wait(2500);
  const openBodies = () => findClass(viewNode, "diff-file-body").filter((b) => b.style.display !== "none").length;
  ok("G click expands all 6 files", openBodies() === 6, "open=" + openBodies());
  ok("G label flips to 'Collapse all' after expanding", textOf(expT) === "⊟ Collapse all", textOf(expT));
  const fhead = findClass(viewNode, "diff-file-head")[0];
  fire(fhead, "click");
  await wait(50);
  ok("G manual collapse of one file flips label back to 'Expand all'", textOf(expT) === "⊞ Expand all", textOf(expT));
  fire(fhead, "click");
  await wait(300);
  ok("G manual re-expand of that file flips label to 'Collapse all'", textOf(expT) === "⊟ Collapse all", textOf(expT));
  fire(expT, "click");
  await wait(50);
  ok("G 'Collapse all' collapses every file", openBodies() === 0, "open=" + openBodies());
  ok("G label returns to 'Expand all'", textOf(expT) === "⊞ Expand all", textOf(expT));
  fire(modeT, "click");
  await wait(50);
  ok("G mode click switches to split (label now '⇄ Unified')", textOf(modeT) === "⇄ Unified", textOf(modeT));
  ok("G localStorage['diffview'] persisted as split", global.localStorage.getItem("diffview") === "split");
  fire(modeT, "click");
  await wait(50);
  ok("G mode click switches back (label '⇄ Split', storage unified)", textOf(modeT) === "⇄ Split" && global.localStorage.getItem("diffview") === "unified", textOf(modeT));

  // ---- H: polish round 14 — font-size token scale + breadcrumb tail icon ----
  // Token scale is defined on :root and rules resolve through it (source grep,
  // not computed style). At least nav/tree/chip/code rules must reference a token.
  ok("H --fs-* token scale defined on :root", /--fs-ui:\s*0\.8rem/.test(HTML) && /--fs-code:\s*0\.8rem/.test(HTML) && /--fs-small:/.test(HTML) && /--fs-body:/.test(HTML) && /--fs-dense:/.test(HTML));
  ok("H nav rule references a --fs token", /#nav a[^}]*font-size:\s*var\(--fs-ui\)/.test(HTML));
  ok("H tree-row rule references a --fs token", /\.tree-row\s*\{[^}]*font-size:\s*var\(--fs-code\)/.test(HTML));
  ok("H chip rule references a --fs token", /\.chip\s*\{[^}]*font-size:\s*var\(--fs-small\)/.test(HTML));
  ok("H code/blob rule references a --fs token", /\.blob\s*\{[^}]*font-size:\s*var\(--fs-code\)/.test(HTML));
  ok("H raw-body styled as a code block (mono code scale + border + panel)", /\.raw-body\s*\{[^}]*font-size:\s*var\(--fs-code\)[^}]*border:\s*1px solid var\(--line\)/.test(HTML));
  // Every remaining literal font-size must be an intentional inline `em` survivor
  // (markdown code/kbd scale with surrounding text, not the global scale).
  const litSizes = (HTML.match(/font-size:\s*[0-9.]+rem/g) || []);
  ok("H no literal rem font-sizes remain (all tokenized)", litSizes.length === 0, litSizes.join(","));
  const emSizes = (HTML.match(/font-size:\s*[0-9.]+em/g) || []);
  ok("H the two inline em survivors remain (markdown code/kbd)", emSizes.length === 2, emSizes.join(","));

  // Breadcrumb renders the path as clickable segments and carries NO file icon.
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  await run("#file:go.mod@main");
  const bc = findClass(viewNode, "breadcrumb")[0];
  const anchors = (bc._children || []).filter((c) => c && c.nodeType === 1 && c.tagName === "A");
  const tailA = anchors[anchors.length - 1];
  ok("H breadcrumb links the path segments (root + go.mod)", anchors.length >= 2 && textOf(tailA) === "go.mod", "tail=" + (tailA && textOf(tailA)));
  ok("H breadcrumb carries no file icon (bc-icon removed)", findClass(bc, "bc-icon").length === 0);

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
