// verify_request_budget.js - per-route request-count ceilings on the fully
// indexed showcase fixture. Landing directly on a route with a COLD cache costs
// a bounded number of bucket fetches; this pins that budget per route so a
// regression (an accidental per-object walk where the index could answer) fails
// CI. Each route is driven on a fresh context (cold cache), its fetches counted,
// and asserted under a generous-but-honest ceiling (≈ measured × 1.5). The
// measured table prints so future readers see the real numbers.
//
// Index-fed routes (timeline, default-branch log, list tabs, item/PR detail,
// home, tags/branches, analytics, merged-PR detail) get tight ceilings — their
// cost is the index slice + the rendered slice, not a history walk. The
// deliberate walks (graph, compare, a non-default-branch log) are bounded by
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
global.fetch = async (url, opts) => { fetches++; return realFetch(url, opts); };

// measure drives one route on a FRESH cold context and returns the number of
// fetches it issued, after letting background enrichment (interaction counts, a
// deferred diff/thread) settle. settleMs is generous so the count is the fully
// resolved page cost, not a mid-load snapshot.
async function measure(bucket, hash, settleMs) {
  const ctx = GS.newContext(origin + "/" + bucket + "/");
  setHash(hash);
  fetches = 0;
  await GS.route(ctx);
  await wait(settleMs || 500);
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
async function run(label, bucket, hash, ceiling, settleMs) {
  const n = await measure(bucket, hash, settleMs);
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

  ok("discovered thread-demo item hashes", !!(issue && pr && post && memo),
    "issue=" + !!issue + " pr=" + !!pr + " post=" + !!post + " memo=" + !!memo);
  ok("discovered merged-demo PR", !!mergedPR);
  if (!issue || !pr || !post || !memo || !mergedPR) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }

  // Ceilings were set from a MEASURE run (see the printed table): ≈ measured ×
  // 1.5, rounded up to a round number. Index-fed routes first.
  await run("home", TD, "#/", 30);
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
  await run("analytics", TD, "#/analytics", 60);
  await run("config", TD, "#/config", 30);
  await run("search (empty)", TD, "#/search", 85);

  // Detail routes (include background enrichment settle).
  await run("issue detail", TD, GS.commitRef(issue.commit.hash, "gitmsg/pm"), 40, 700);
  await run("pr detail", TD, GS.commitRef(pr.commit.hash, "gitmsg/review"), 40, 700);
  if (rel) await run("release detail", TD, GS.commitRef(rel.commit.hash, "gitmsg/release"), 30, 700);
  await run("post detail", TD, GS.commitRef(post.commit.hash, "gitmsg/social"), 30, 700);
  await run("memo detail", TD, GS.commitRef(memo.commit.hash, "gitmsg/memo"), 30, 700);
  // The whole point of Task 1: merged-PR detail resolves its short shas from the
  // code index, NOT a ~775-GET base-branch walk. Post-fix ceiling.
  await run("merged-PR detail", "merged-demo", GS.commitRef(mergedPR.commit.hash, "gitmsg/review"), 40, 900);

  // Deliberate walks — bounded by their walk window, measured and capped.
  await run("graph (walk)", TD, "#/graph", 80);
  await run("compare (walk)", TD, "#/compare:main...feature%2Fnotes-expand", 40, 700);
  await run("non-default-branch log (walk)", TD, "branch:feature/notes-expand", 30);

  // Print the measured table so future readers see real numbers.
  console.log("\n  route                              fetches  ceiling");
  for (const r of results) console.log("  " + r.label.padEnd(34) + String(r.n).padStart(5) + "  " + String(r.ceiling).padStart(7));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });
