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
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { viewNode, textOf, setHash } = global.__shim;
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
function done() { console.log("\n" + pass + " passed, " + fail + " failed"); process.exit(fail ? 1 : 0); }

// Delay every fetch whose URL contains `slowMark` by `delayMs`, so the route that
// reads that bucket settles strictly after a concurrently-started fast route.
const realFetch = global.fetch;
let slowMark = null, delayMs = 0;
global.fetch = async function (url, opts) {
  if (slowMark && typeof url === "string" && url.indexOf(slowMark) !== -1 && delayMs) await wait(delayMs);
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
  slowMark = "/merged-demo/"; delayMs = 200;
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

  done();
}
main().catch((e) => { console.error(e); process.exit(1); });
