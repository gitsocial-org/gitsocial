// verify_request_budget.js - per-route request-count ceilings on the fully
// indexed showcase fixture. Landing directly on a route with a COLD cache costs
// a bounded number of bucket fetches; this pins that budget per route so a
// regression (an accidental per-object walk where the index could answer) fails
// CI. Each route is driven on a fresh context (cold cache), its fetches counted,
// and asserted under a generous-but-honest ceiling (≈ measured × 1.5). The
// measured table prints so future readers see the real numbers.
//
// Index-fed routes (timeline, default-branch log, list tabs, item/PR detail,
// home, tags/branches, analytics, merged-PR detail, the graph) get tight
// ceilings — their cost is the index slice + the rendered slice, not a history
// walk. The deliberate walks (compare, a non-default-branch log) are bounded by
// their walk window, not the index; they get ceilings too so "bounded" is never
// "unmeasured".
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { setHash } = global.__shim;
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

// Count every bucket fetch (any URL), regardless of caching layers above — the
// honest visitor cost is the number of network requests the reader issues.
const realFetch = global.fetch;
let fetches = 0;
let inFlight = 0;
global.fetch = async (url, opts) => {
  fetches++;
  inFlight++;
  try { return await realFetch(url, opts); } finally { inFlight--; }
};

// settle waits until the route has actually STOPPED fetching: nothing in flight
// and nothing new started for a quiet span. A flat sleep measured the wrong
// thing here — several routes keep fetching after first paint (home's recent
// activity is most of home's cost and fills after the landing paints, detail
// routes enrich in the background), so whatever landed late simply went
// uncounted. That variance is downward, the dangerous direction: a regression
// that doubled a section's cost would read as a pass.
async function settle(quietMs, maxMs) {
  const quiet = quietMs || 400, deadline = Date.now() + (maxMs || 20000);
  let seen = -1, still = Date.now();
  while (Date.now() < deadline) {
    if (inFlight > 0 || fetches !== seen) { seen = fetches; still = Date.now(); }
    else if (Date.now() - still >= quiet) return;
    await wait(50);
  }
}

// measure drives one route on a FRESH cold context and returns the number of
// fetches it issued, once every fetch the route started — including the ones it
// starts after first paint — has landed.
async function measure(bucket, hash) {
  const ctx = GS.newContext(origin + "/" + bucket + "/");
  setHash(hash);
  fetches = 0;
  await GS.route(ctx);
  await settle();
  return fetches;
}

// discoverHashes pulls a representative item hash from each ext branch of a
// bucket (fixture commit hashes are non-deterministic per build), so the detail
// routes address a real item.
async function firstItem(bucket, ext, pred) {
  const ctx = GS.newContext(origin + "/" + bucket + "/");
  const items = await GS.loadExtItemsAll(ctx, ext);
  const list = pred ? items.filter(pred) : items;
  return list.length ? list[0] : null;
}

const results = [];
// run measures a route, records it for the table, and asserts the ceiling.
async function run(label, bucket, hash, ceiling) {
  const n = await measure(bucket, hash);
  results.push({ label, n, ceiling });
  ok(label + " ≤ " + ceiling + " fetches (measured " + n + ")", n <= ceiling, "measured " + n + " > ceiling " + ceiling);
}

async function main() {
  const TD = "thread-demo";

  // Discover detail-route hashes on thread-demo.
  const issue = await firstItem(TD, "pm", (i) => (i.header.type || "issue") === "issue");
  const pr = await firstItem(TD, "review", (i) => (i.header.type || "") === "pull-request");
  const rel = await firstItem(TD, "release");
  const post = await firstItem(TD, "social");
  const memo = await firstItem(TD, "memo");
  const mergedPR = await firstItem("merged-demo", "review", (i) => (i.header.type || "") === "pull-request");

  // Every detail route below is guarded by this check, so a ceiling can never be
  // dropped by an item that failed to be discovered.
  ok("discovered thread-demo item hashes", !!(issue && pr && post && memo),
    "issue=" + !!issue + " pr=" + !!pr + " post=" + !!post + " memo=" + !!memo);
  ok("discovered merged-demo PR", !!mergedPR);
  if (!issue || !pr || !post || !memo || !mergedPR) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }
  // Releases are the one type thread-demo deliberately does NOT carry (the
  // release extension is left uninitialized there, which verify_pages.js pins).
  // Its ceiling therefore has nothing to measure, and that state is ASSERTED
  // rather than swallowed by a bare `if`: a ceiling silently skipped is a
  // ceiling that stopped guarding anything, and if the fixture ever gains a
  // release corpus this turns red until the ceiling is wired in with it.
  ok("the release ceiling is skipped only because the fixture carries no releases", rel === null, "found a release item");

  // Ceilings were set from a MEASURE run (see the printed table): ≈ measured ×
  // 1.5, rounded up to a round number. Index-fed routes first.
  //
  // home's ceiling moved 30 → 45 when the front page gained its Recent activity
  // section: the four ext indexes cost ~18 and the code index ~3, a deliberate
  // and understood increase, not drift. It was then raised again to absorb
  // run-to-run variance, which was an artifact of the measurement — the section
  // fills AFTER first paint, and a flat settle window counted however much of it
  // happened to land in time. Now that measure() waits for the route to stop
  // fetching, the count is stable and the ceiling is back near it. This drives
  // the app directly (the plain-shell path); a page entry behaves the same way,
  // since its reveal deliberately does NOT wait for the section. A real bucket
  // pays ~23: this fixture forces 4-entry shards (GITSOCIAL_SITE_SHARD_COUNT),
  // so its tiny branches drain older shards that 4000-entry production shards
  // never touch.
  await run("home", TD, "#/", 35);
  await run("timeline", TD, "#/timeline", 85);
  await run("issues", TD, "#/issues", 60);
  await run("prs list", TD, "#/prs", 40);
  await run("releases list", TD, "#/releases", 40);
  await run("milestones", TD, "#/milestones", 40);
  await run("sprints", TD, "#/sprints", 40);
  await run("memos", TD, "#/memos", 40);
  await run("board", TD, "#/board", 60);
  await run("lists", TD, "#/lists", 30);
  await run("tags", TD, "#/tags", 20);
  await run("branches", TD, "#/branches", 20);
  await run("default-branch log", TD, "branch:main", 25);
  // The commits list is metadata-only by construction: the page layer's published
  // partition (one small doc) plus the code index slice the requested page needs.
  // On this fixture the corpus is one shard, so the ceiling cannot tell a bounded
  // read from a full drain — what it does guard is the regression that would
  // actually hurt, a per-commit loose-object hydration behind the rows.
  await run("commits list", TD, "#/commits", 15);
  await run("analytics", TD, "#/analytics", 60);
  await run("config", TD, "#/config", 30);
  await run("search (empty)", TD, "#/search", 85);

  // Detail routes (include background enrichment settle).
  await run("issue detail", TD, GS.commitRef(issue.commit.hash, "gitmsg/pm"), 40);
  await run("pr detail", TD, GS.commitRef(pr.commit.hash, "gitmsg/review"), 40);
  if (rel) await run("release detail", TD, GS.commitRef(rel.commit.hash, "gitmsg/release"), 30);
  await run("post detail", TD, GS.commitRef(post.commit.hash, "gitmsg/social"), 30);
  await run("memo detail", TD, GS.commitRef(memo.commit.hash, "gitmsg/memo"), 30);
  // The whole point of Task 1: merged-PR detail resolves its short shas from the
  // code index, NOT a ~775-GET base-branch walk. Post-fix ceiling.
  await run("merged-PR detail", "merged-demo", GS.commitRef(mergedPR.commit.hash, "gitmsg/review"), 40);

  // The graph is index-fed since the v5 code corpus (entries carry parents):
  // manifest + eager shard/head + refs, no per-commit walk, plus the review
  // index's eager set (3 fetches) for the merged-PR branch decorations.
  await run("graph (indexed)", TD, "#/graph", 15);

  // Deliberate walks — bounded by their walk window, measured and capped.
  await run("compare (walk)", TD, "#/compare:main...feature%2Fnotes-expand", 40);
  await run("non-default-branch log (walk)", TD, "branch:feature/notes-expand", 30);

  // Print the measured table so future readers see real numbers.
  console.log("\n  route                              fetches  ceiling");
  for (const r of results) console.log("  " + r.label.padEnd(34) + String(r.n).padStart(5) + "  " + String(r.ceiling).padStart(7));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });
