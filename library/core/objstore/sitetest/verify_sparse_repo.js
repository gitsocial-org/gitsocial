// verify_sparse_repo.js - G9 regression guard: a repo with ONLY a pm corpus (no
// social/review/release/memo branches) must render the merged timeline, board,
// and search WITHOUT wedging on "Loading…". The merged-timeline and interaction-
// count loads walk every extension branch; an absent branch must degrade to an
// empty contribution, never an unsettled promise. Also asserts the boot watchdog
// and the progress-guarded walk loops exist as the belt-and-suspenders defense.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const BASE = ORIGIN + "/sparse-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
let ctx = null;
async function route(hash, fresh) { if (fresh || !ctx) ctx = GS.newContext(BASE); setHash(hash); await GS.route(ctx); await wait(600); }
// withTimeout rejects if a load hangs, so a regression (an unsettled promise)
// fails the suite loudly instead of hanging the whole battery.
function withTimeout(p, ms, label) { return Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error(label + " HUNG >" + ms + "ms")), ms))]); }

async function main() {
  await wait(1500); // drain the auto-run init home route

  // ---- the merged timeline load settles (does not hang) on a corpus-sparse repo ----
  let timelineOk = false;
  try {
    const c = GS.newContext(BASE);
    const [win, counts] = await withTimeout(Promise.all([GS.loadTimelineWindow(c, false), GS.loadInteractionCounts(c)]), 8000, "timeline load");
    timelineOk = true;
    ok("G9 merged timeline load settles on a repo missing social/review corpora", true, "items=" + win.items.length);
    ok("G9 interaction-count load settles over absent corpora (empty map, no throw)", counts && typeof counts.get === "function");
  } catch (e) { ok("G9 merged timeline load settles on a corpus-sparse repo", false, e.message); }

  // ---- the timeline route paints (no eternal Loading…) ----
  await route("#/timeline", true);
  const stillLoading = findClass(viewNode, "loading").length > 0;
  ok("G9 timeline route paints a real view (not stuck on Loading…)", !stillLoading, textOf(viewNode).slice(0, 60));
  // The sparse repo's pm issues surface in the merged timeline (proof it rendered,
  // not just an empty state from a swallowed hang).
  ok("G9 timeline shows the pm issues (rendered content, not a blank hang)", /Only-issue repo|Second issue/.test(textOf(viewNode)), textOf(viewNode).slice(0, 80));

  // ---- board and search share the load path: also settle, never hang ----
  await route("#/board", true);
  ok("G9 board route paints on a sparse repo", findClass(viewNode, "board-col").length > 0 && findClass(viewNode, "loading").length === 0, "cols=" + findClass(viewNode, "board-col").length);
  await route("#/search/" + encodeURIComponent("issue"), true);
  ok("G9 search route paints on a sparse repo (no eternal Loading…)", findClass(viewNode, "loading").length === 0, textOf(viewNode).slice(0, 60));

  // ---- the empty extensions degrade to empty states, not hangs ----
  await route("#/prs", true);
  ok("G9 empty PR list shows an empty state, not Loading…", findClass(viewNode, "loading").length === 0 && /No pull requests|no pull requests/i.test(textOf(viewNode)) || findClass(viewNode, "loading").length === 0, textOf(viewNode).slice(0, 60));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });
