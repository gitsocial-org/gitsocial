// verify_memos.js - Memos tab against the showcase fixture: an empty bucket
// (other-demo, no memo branch) renders the empty state with the nav item still
// shown; the showcase bucket has three project-tier memos that render as cards
// with label chips and a working detail route.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { textOf } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/" + (process.env.GS_SITE_BUCKET || "thread-demo") + "/";
const EMPTY = ORIGIN + "/" + (process.env.GS_SITE_BUCKET_EMPTY || "other-demo") + "/";
let pass = 0, fail = 0; const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
function findClass(node, cls, out) { out = out || []; for (const c of node._children || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
async function main() {
  // A bucket without a memo branch resolves no tip and renders the empty state;
  // the nav item stays present either way.
  const ctxEmpty = GS.newContext(EMPTY);
  const tipEmpty = await GS.refTip(ctxEmpty, GS.EXT_BRANCHES.memo);
  ok("empty path: bucket has NO memo branch (tab still shown, empty state)", !tipEmpty, "tip=" + tipEmpty);
  const rEmpty = await GS.loadExtItemsWindow(ctxEmpty, "memo", false);
  ok("empty path: bucket loads zero memos (renders 'No memos.')", rEmpty.items.length === 0, "n=" + rEmpty.items.length);
  // the showcase bucket HAS memos.
  const ctx = GS.newContext(BASE);
  const tip = await GS.refTip(ctx, GS.EXT_BRANCHES.memo);
  ok("showcase memo branch resolves (list populated)", !!tip, "tip=" + tip);
  const memos = await GS.loadExtItems(ctx, "memo");
  ok("showcase has 3 memos", memos.length === 3, "count=" + memos.length);
  const cards = memos.map(GS.memoCard);
  const chips = cards.flatMap((c) => findClass(c, "chip").map((x) => textOf(x)));
  const glyphs = cards.flatMap((c) => findClass(c, "type-glyph").map((x) => textOf(x)));
  ok("rendered memo cards carry the memo type glyph", glyphs.filter((x) => x === "☞").length === 3, glyphs.join(","));
  ok("rendered memo cards carry label chips", chips.includes("kind/policy") && chips.includes("area/objstore"), chips.join(","));
  ok("memo subjects link to @gitmsg/memo detail", cards.every((c) => findClass(c, "subject").some((s) => (s.getAttribute("href") || "").includes("@gitmsg/memo"))));
  // Let the unawaited boot route (home, fired on require) land first, so its
  // render cannot clobber the detail view below (item loads are index-fast now).
  await new Promise((r) => setTimeout(r, 600));
  global.__ctx = ctx; global.__shim.setHash("#commit:" + tip.slice(0, 12) + "@gitmsg/memo");
  await GS.route(ctx);
  const vt = textOf(global.__shim.viewNode);
  ok("memo detail route renders (subject present)", /Effective author rule|Cache invalidation|loose objects/.test(vt), vt.slice(0, 80));
  console.log("\n" + pass + " passed, " + fail + " failed"); process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
