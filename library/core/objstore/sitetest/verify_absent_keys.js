// verify_absent_keys.js - a key the page can already tell is not there must not
// be asked for over and over. A miss is not free: a real bucket answers 404 with
// a full HTML error body (~27 KB on R2), so a handful of speculative probes cost
// more than the page they are part of.
//
// Three cases, each with a source of truth the page already holds:
//   - an extension branch absent from .gitsocial/site/refs.json. The manifest is
//     the fast path and NOT proof of absence — it is written best-effort by
//     whichever pusher last succeeded, so treating its silence as authoritative
//     would make a whole extension branch read as empty forever, with no error
//     anywhere. So an omitted refname is probed live, and the bound is that the
//     probe happens ONCE per context rather than once per route,
//   - a pack map shard for a sha the caller already knows is a tree or a blob
//     (the map indexes commits and tags only, and is written sparsely),
//   - /favicon.ico at the ORIGIN root, which a generated page provokes by
//     declaring no icon — and under a bucket prefix that key is outside the site
//     altogether, so no push could ever make it resolve.
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const { setHash } = global.__shim;
const ORIGIN = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const TD = ORIGIN + "/thread-demo/";
const PACKED = ORIGIN + "/packed-demo/";
let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

const realFetch = global.fetch;
let log = [];
global.fetch = async (url, opts) => {
  const res = await realFetch(url, opts);
  log.push({ url: String(url).replace(ORIGIN, ""), status: res.status, method: (opts && opts.method) || "GET" });
  return res;
};
const mark = () => log.length;
const since = (at) => log.slice(at);
const hits = (rows, re) => rows.filter((r) => re.test(r.url));
// probesFor counts the requests that ended at exactly this refname's key.
const probesFor = (rows, ref) => hits(rows, new RegExp(ref.replace(/\//g, "\\/") + "$")).length;

// drain waits for the route in flight to stop fetching, rather than sleeping a
// fixed span. Every count below is read off the log, so a late fetch that lands
// after a fixed sleep silently lands inside the NEXT measurement instead — on a
// loaded machine that turns "refs.json read exactly once" into a coin flip.
async function drain(quietMs, maxMs) {
  const quiet = quietMs || 400, deadline = Date.now() + (maxMs || 15000);
  let seen = -1, still = Date.now();
  while (Date.now() < deadline) {
    if (log.length !== seen) { seen = log.length; still = Date.now(); }
    else if (Date.now() - still >= quiet) return;
    await wait(50);
  }
}

async function main() {
  await drain(); // the auto-run init home route

  // ---- 1. an ext branch the manifest omits costs one probe per context ----
  const listed = JSON.parse(await GS.fetchText(TD, ".gitsocial/site/refs.json"));
  const absent = Object.values(GS.EXT_BRANCHES).filter((r) => !listed[r]);
  const present = Object.values(GS.EXT_BRANCHES).filter((r) => listed[r]);
  ok("the fixture has an extension branch its manifest omits", absent.length > 0, "all " + present.length + " listed");

  const ctx = GS.newContext(TD);
  setHash("#/");
  let at = mark();
  await GS.route(ctx);
  await drain();
  const home = since(at);
  // The well-known extension branches are asked for on every route, so the cost
  // that matters is per refname, not per ask: one live probe establishes the
  // miss and the context remembers it.
  const overProbed = absent.filter((ref) => probesFor(home, ref) > 1);
  ok("a manifest-omitted branch is probed at most once on the route that first asks for it",
    overProbed.length === 0, overProbed.map((ref) => ref + " x" + probesFor(home, ref)).join(", "));
  ok("home reads the refs manifest exactly once",
    hits(home, /refs\.json/).length === 1, hits(home, /refs\.json/).length + " reads");
  // And the probe asks for one bit, so it must not pay for a body. An object
  // store answers a missing key with a full error document (R2 serves ~27 KB),
  // which on a repo missing two well-known branches was ~54 KB per session
  // against a page whose own transfer is ~18 KB. HEAD answers the same question
  // for nothing.
  const probeReqs = absent.flatMap((ref) => home.filter((r) => r.url.endsWith(ref)));
  ok("the absence probe is issued, and every one of them is a HEAD",
    probeReqs.length > 0 && probeReqs.every((r) => r.method === "HEAD"),
    JSON.stringify(probeReqs.map((r) => r.method + " " + r.url + " -> " + r.status)));
  ok("no absent branch is ever fetched for its body",
    !home.some((r) => r.status === 404 && r.method === "GET" && absent.some((ref) => r.url.endsWith(ref))),
    JSON.stringify(home.filter((r) => r.status === 404).map((r) => r.method + " " + r.url)));
  // The half that used to be paid per route: a second route on the same context
  // re-asks for the same branches and must issue nothing at all for them.
  setHash("#/timeline");
  at = mark();
  await GS.route(ctx);
  await drain();
  const second = since(at);
  const reprobed = absent.filter((ref) => probesFor(second, ref) > 0);
  ok("a second route on the same context re-probes none of them",
    reprobed.length === 0, reprobed.map((ref) => ref + " x" + probesFor(second, ref)).join(", "));
  ok("the route leaves the manifest body on the context for the freshness watch",
    typeof ctx.manifestText === "string", typeof ctx.manifestText);
  at = mark();
  await GS.manifestFor(ctx);
  ok("re-reading the manifest off a warm context fetches nothing",
    hits(since(at), /refs\.json/).length === 0, since(at).map((r) => r.url).join(" | "));

  // A listed branch still resolves, so remembering misses never turned into a
  // shortcut that broke discovery.
  const tip = present.length ? await GS.refTip(GS.newContext(TD), present[0]) : null;
  ok("a listed extension branch still resolves to its tip", /^[0-9a-f]{40}$/.test(tip || ""), String(tip));

  // A bucket with no manifest at all must keep probing: nothing else lists refs.
  const bare = GS.newContext(ORIGIN + "/refdelta-demo/");
  at = mark();
  await GS.refTip(bare, "refs/heads/main");
  ok("a bucket with no manifest still probes the live ref key",
    hits(since(at), /refs\/heads\/main$/).length === 1, since(at).map((r) => r.url).join(" | "));

  // ---- 2. a tree or blob read consults no pack map shard ----
  const packedCtx = GS.newContext(PACKED);
  const head = await GS.resolveRef(PACKED, "refs/heads/main");
  await GS.getObject(packedCtx, head); // the commit itself legitimately uses the map
  at = mark();
  const node = await GS.resolvePath(packedCtx, head, "notes.txt");
  const blob = node && await GS.getContentObject(packedCtx, node.sha);
  const content = since(at);
  ok("the blob read really happened", !!(blob && blob.type === "blob" && blob.body.length > 0), node ? node.type : "no node");
  ok("walking a tree to a blob consults no pack map shard",
    hits(content, /packmap/).length === 0, hits(content, /packmap/).map((r) => r.status + " " + r.url).join(" | "));
  ok("nothing in a content read 404s", content.every((r) => r.status < 400),
    content.filter((r) => r.status >= 400).map((r) => r.status + " " + r.url).join(" | "));

  // The map is genuinely sparse, so a shard the reader skips is often not there
  // to be found — which is what makes such a probe a 404 rather than a small 200.
  let missing = 0;
  for (let i = 0; i < 256; i++) {
    const name = i.toString(16).padStart(2, "0");
    if ((await GS.fetchBytes(PACKED, ".gitsocial/packmap/" + name + ".json")) === null) missing++;
  }
  ok("the pack map is sparse (" + missing + " of 256 prefixes have no shard)", missing > 0);

  // ---- 3. every generated page declares an icon that needs no bucket key ----
  const pages = ["index.html", "issues/index.html"];
  const itemPage = (await GS.fetchText(TD, "sitemap.xml") || "").match(/<loc>[^<]*\/(i\/[^<]+\.html)<\/loc>/);
  if (itemPage) pages.push(itemPage[1]);
  ok("found a front page, a list page and an item page to check", pages.length === 3, pages.join(", "));
  for (const key of pages) {
    const html = await GS.fetchText(TD, key);
    const icon = (html || "").match(/<link rel="icon" href="([^"]*)"/);
    ok(key + " declares an icon", !!icon, (html || "").slice(0, 60));
    ok(key + " icon needs no bucket key (a data: URI)", !!icon && /^data:image\//.test(icon[1]), icon ? icon[1].slice(0, 32) : "none");
    ok(key + " references no favicon.ico", !/favicon\.ico/.test(html || ""));
  }

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });
