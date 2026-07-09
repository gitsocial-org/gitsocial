// verify_icons_fallback.js - GSIcons absent (old bucket copies): icon calls
// return null and every call site falls back to its text glyph. NOTE: does not
// require icons.js, so window.GSIcons is never set.
require("./shim.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, findTag, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out){ out=out||[]; for(const c of node._children||[]){ if(c&&c.nodeType===1){ if(c._cls&&c._cls.has(cls)) out.push(c); findClass(c,cls,out);} } return out; }
async function run(h){ setHash(h); await GS.route(global.__ctx); await wait(700); }
(async () => {
  ok("GSIcons is absent in this process", typeof global.window.GSIcons === "undefined");
  ok("iconEl returns null without GSIcons", GS.iconEl("folder") === null);
  ok("icon returns null without GSIcons", GS.icon("main.go") === null);
  global.__ctx = GS.newContext((process.env.GS_SITE_ORIGIN||"http://localhost:8000")+"/gitsocial/");
  await wait(2500); // drain init home route
  // Use the code root (carries files like go.mod/README.md) so file rows are
  // present to prove the file-type icon fallback.
  setHash("#/code"); await GS.route(global.__ctx); await wait(700);
  const rows = findClass(viewNode, "tree-row");
  ok("tree still renders rows without icons.js", rows.length >= 3, "rows=" + rows.length);
  // The in-place tree adds a structural expand chevron on directory rows from the
  // bundled CHEVRON_SVG path (a trusted asset, independent of icons.js), so a
  // fallback tree legitimately carries chevron svgs/gs-icon wrappers. What must
  // degrade to glyphs is the file-TYPE icon set (icons.js): no .tree-icon carries
  // a gs-icon wrapper, and every svg present is a structural chevron.
  // ROUND UPDATE: directory rows no longer carry a folder icon at all (the
  // rotating chevron alone signals a folder), so .tree-icon spans now appear
  // ONLY on file rows and the 📁 folder text glyph is gone; 📄 stays the file
  // fallback.
  const treeIcons = findClass(viewNode, "tree-icon");
  ok("file rows carry a fallback file-type icon (tree-icon present, no gs-icon)", treeIcons.length >= 1 && treeIcons.every((n) => !(n._cls && n._cls.has("gs-icon"))));
  ok("only structural chevron svgs present (file-type icons absent)", findTag(viewNode, "svg").every((s) => s._parent && s._parent._cls && s._parent._cls.has("tree-chevron")));
  ok("file rows fall back to the 📄 text glyph; dir rows show no folder glyph", /📄/.test(textOf(viewNode)) && !/📁/.test(textOf(viewNode)));
  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error("THREW:", e); process.exit(1); });
