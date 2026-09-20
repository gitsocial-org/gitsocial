// verify_route_supersede.js - a stale route must never clobber a newer view.
//
// Two route() calls run concurrently against the shared #view (the exact class of
// race the browser hits on rapid nav timeline→detail, and that the headless suites
// hit because require("gs-app.js") auto-boots init()'s "#/" route alongside a
// suite's own route). We drive it deterministically: fetch is wrapped so the FIRST
// route's requests are delayed, guaranteeing it resolves LAST and would overwrite
// the second (newer) route's already-painted detail if route() had no generation
// guard. Post-fix the stale paint is dropped; pre-fix this fails (view reverts to
// the first route's home/tree).
require("./shim.js");
require("../assets/icons.js");
const GS = require("../assets/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
function done() { console.log("\n" + pass + " passed, " + fail + " failed"); process.exit(fail ? 1 : 0); }

// Delay every fetch whose URL contains one of `slowMarks` by `delayMs`, so the route
// that reads those keys settles strictly after a concurrently-started fast route.
const realFetch = global.fetch;
let slowMarks = [], delayMs = 0, slowHits = 0;
global.fetch = async function (url, opts) {
  if (delayMs && typeof url === "string" && slowMarks.some((m) => url.indexOf(m) !== -1)) { slowHits++; await wait(delayMs); }
  return realFetch(url, opts);
};

async function main() {
  // Discover the merged PR (non-deterministic hashes per build).
  const disc = GS.newContext(origin + "/merged-demo/");
  const items = await GS.loadExtItemsAll(disc, "review");
  const pr = items.find((i) => i.header && i.header.type === "pull-request");
  ok("merged PR discovered", !!pr, "no pull-request item");
  if (!pr) return done();

  // Route A (slow, "home" of merged-demo) starts first and is delayed so it settles
  // last. Route B (fast, the PR detail) starts immediately after and paints first.
  slowMarks = ["/merged-demo/"]; delayMs = 200;
  const ctxA = GS.newContext(origin + "/merged-demo/");
  setHash("#/");
  const aDone = GS.route(ctxA); // do NOT await — let it run in the background, delayed

  // Give A a tick to claim its generation and issue its (delayed) fetches, then
  // start B. B's requests are also delayed (same bucket), but B is the newer gen.
  await wait(10);
  const ctxB = GS.newContext(origin + "/merged-demo/");
  setHash(GS.commitRef(pr.commit.hash, "gitmsg/review"));
  const bDone = GS.route(ctxB);

  await Promise.all([aDone, bDone]);
  // Let A's now-superseded terminal setView (and any late enrichment) attempt to land.
  await wait(50);

  const txt = textOf(viewNode);
  ok("newer detail view survived the stale route", txt.includes("Add a changelog") || txt.toLowerCase().includes("merged"), "view=" + JSON.stringify(txt.slice(0, 120)));
  ok("stale home/tree did not clobber the detail", !txt.includes("Showcase fixture") && !txt.includes("branches"), "clobbered by stale route :: " + JSON.stringify(txt.slice(0, 120)));

  await progressCase();
  done();
}

// progressCase is the other way a stale route reaches the shared #view: the
// "Searching history…" line itemDetail repaints while findItemDeep deepens an ext
// walk, which on a multi-shard corpus keeps ticking after the visitor clicked away.
async function progressCase() {
  const base = origin + "/thread-demo/", dir = ".gitsocial/site/items/pm/";
  // An item in the OLDEST sealed shard sits outside the index's eager set (newest
  // sealed shard plus head), so its detail route has to drain the shards between.
  const warm = GS.newContext(base);
  const mtext = await GS.fetchText(base, dir + "manifest.json");
  const manifest = mtext ? JSON.parse(mtext) : null;
  const older = manifest && manifest.shards.length > 1 ? manifest.shards.slice(0, -1).map((s) => s.key) : [];
  const oldestDoc = older.length ? JSON.parse(await GS.fetchText(base, dir + manifest.shards[0].key)) : null;
  const entry = oldestDoc ? (oldestDoc.items || []).find((e) => !/edits="/.test(e.header || "")) : null;
  ok("an item outside the pm eager set is available", !!entry && older.length > 0,
    "older shards=" + older.length + " entry=" + !!entry);
  if (!entry || !older.length) return;

  // Warm the timeline context first, so re-routing it paints without waiting on the
  // shards the search below is held on.
  setHash("#/timeline");
  await GS.route(warm);
  await wait(50);

  slowMarks = older; delayMs = 300; slowHits = 0;
  const ctxA = GS.newContext(base);
  // The short hash is what a generated item page stamps as its route, and it is the
  // form that resolves through the walk rather than one loose object read, so the
  // search really deepens.
  setHash(GS.commitRef(entry.sha.slice(0, 12), "gitmsg/pm"));
  const aDone = GS.route(ctxA); // do NOT await: the search ticks in the background
  await wait(20);

  // The Timeline nav link: it sets the hash, and the app routes on hashchange.
  setHash("#/timeline");
  await GS.route(warm);
  const route = GS.parseRoute(global.location.hash);
  ok("the nav link's hash routes to the timeline", route.type === "index" && route.tab === "timeline", "route=" + JSON.stringify(route));
  const painted = textOf(viewNode);
  ok("the timeline rendered over the item detail", painted.includes("Timeline"), "view=" + JSON.stringify(painted.slice(0, 120)));

  const hitsAtPaint = slowHits;
  await aDone;
  await wait(100);
  const after = textOf(viewNode);
  // The held shards prove the search was still running after the timeline painted,
  // so a green result here is the guard working and never a race that went missing.
  ok("the stale search was still deepening past the timeline's paint", slowHits > hitsAtPaint, "held shard reads " + hitsAtPaint + " → " + slowHits);
  ok("the superseded item search never repaints over the timeline", !after.includes("Searching history"),
    "clobbered by the stale search :: " + JSON.stringify(after.slice(0, 120)));
  ok("the timeline is still the view once the stale search ends", after.includes("Timeline"), "view=" + JSON.stringify(after.slice(0, 120)));
  slowMarks = []; delayMs = 0;
}
main().catch((e) => { console.error(e); process.exit(1); });
