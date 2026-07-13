// verify_upgrade_boot.js - the M7/M8 page-entry boot contract (gs-upgrade.js):
// the generated pages ARE the site and the app is the enhancement layer. This
// suite pins the boot contract WITHOUT a real browser (the shim has no script
// loader), by combining two levels:
//
//   1. gs-upgrade's pure route↔page-URL mapping (pageURLForHash / hashForPath):
//      item/type-list/front routes map to real page URLs (pushState targets);
//      app-only surfaces (search/board/analytics/code/compare/…) return null so
//      the app keeps its hash route.
//   2. The served fixture: every generated page carries the boot hooks the
//      upgrade layer reads (gs-route meta + data-base + gs-upgrade.js defer), and
//      DRIVING the app on a page's own meta route (and on a hash-over-meta
//      deep-link) renders the right view — proving a page entry boots the app
//      onto the right route, hash winning over meta. A broken upgrade (a 404'd
//      gs-upgrade.js) leaves the static page's readable HTML intact (curl level).
require("./shim.js");
require("../site/icons.js");
const GS = require("../site/gs-app.js");
const UP = require("../site/gs-upgrade.js");
const { setHash } = global.__shim;
const origin = process.env.GS_SITE_ORIGIN || "http://localhost:8000";
const TD = process.env.GS_SITE_BUCKET || "thread-demo";
const OTHER = process.env.GS_SITE_BUCKET_EMPTY || "other-demo";
const base = origin + "/" + TD + "/";
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

let pass = 0, fail = 0;
const ok = (n, c, e) => { (c ? pass++ : fail++); console.log((c ? "PASS " : "FAIL ") + n + (!c && e ? " :: " + e : "")); };

async function get(url) { const res = await fetch(url); return { status: res.status, text: res.status === 200 ? await res.text() : "" }; }
// viewText renders #view and returns its text, for asserting the booted route.
function viewText() { return global.__shim.textOf(global.__shim.viewNode); }

// bootLike drives the app the way gs-upgrade would after loading the assets: set
// the boot hash (routeFor: a real location.hash wins over the meta route,
// otherwise the meta route), point the context at the page's data-base (the site
// root), and run route(). Returns the rendered #view text.
async function bootLike(pageBase, metaRoute, deepHash) {
  const route = (deepHash && deepHash.replace(/^#/, "")) || metaRoute;
  setHash("#" + route);
  const ctx = GS.newContext(pageBase);
  await GS.route(ctx);
  await wait(400);
  return viewText();
}

// mkBrowser builds a minimal window+history+location so the REAL syncURL/wireNav
// (exported from gs-upgrade.js) drive a simulated bucket without a browser. It
// installs the fake on the globals gs-upgrade reads, records the history stack,
// and lets a test push a hash (an in-app nav), fire a hashchange, or pop (a
// back/forward) exactly as the browser would. url tracks the full location; the
// stack + index model back/forward.
function mkBrowser(startURL) {
  const parse = (u) => {
    const hi = u.indexOf("#");
    const hash = hi >= 0 ? u.slice(hi) : "";
    const rest = hi >= 0 ? u.slice(0, hi) : u;
    const qi = rest.indexOf("?");
    const search = qi >= 0 ? rest.slice(qi) : "";
    const path = qi >= 0 ? rest.slice(0, qi) : rest;
    let pathname = path;
    try { pathname = new URL(path).pathname; } catch (e) { /* keep raw */ }
    return { href: u, hash, search, pathname };
  };
  const state = { reloaded: false, hash: "", search: "", pathname: "", href: "" };
  const handlers = { popstate: [], hashchange: [] };
  const fire = (ev) => { for (const h of handlers[ev].slice()) h(); };
  const loc = {
    reload: () => { state.reloaded = true; },
    get href() { return state.href; }, get search() { return state.search; }, get pathname() { return state.pathname; },
    get hash() { return state.hash; },
    // Setting location.hash in a browser rewrites the URL; when the hash CHANGES
    // it also pushes a new history entry and fires hashchange (the handler relies
    // on that to re-render). Mirror both: advance the stack, then fire.
    set hash(h) {
      const b = state.href.split("#")[0]; const v = h.charAt(0) === "#" ? h : "#" + h;
      const nextHref = b + v;
      if (nextHref !== state.href) { stack.length = idx + 1; stack.push(nextHref); idx = stack.length - 1; }
      apply(nextHref);
      fire("hashchange");
    },
  };
  const apply = (u) => { const p = parse(u); state.href = p.href; state.hash = p.hash; state.search = p.search; state.pathname = p.pathname; };
  apply(startURL);
  const stack = [startURL];
  let idx = 0;
  global.window = {
    get location() { return loc; },
    addEventListener: (ev, fn) => { if (handlers[ev]) handlers[ev].push(fn); },
  };
  global.history = {
    replaceState: (_s, _t, u) => { stack[idx] = u; apply(u); },
    pushState: (_s, _t, u) => { stack.length = idx + 1; stack.push(u); idx = stack.length - 1; apply(u); },
  };
  return {
    loc, stack,
    // navigate models an in-app hash render: the app sets location.hash, which
    // fires hashchange → the wired syncURL reflects the URL into history.
    navigate: (hash) => { loc.hash = hash; },
    // back/forward move the history index and fire popstate, like the browser.
    back: () => { if (idx > 0) { idx--; apply(stack[idx]); fire("popstate"); } },
    forward: () => { if (idx < stack.length - 1) { idx++; apply(stack[idx]); fire("popstate"); } },
    reloaded: () => state.reloaded,
    href: () => state.href,
  };
}

async function main() {
  console.log("--- Pure route↔page-URL mapping (pushState targets) ---");
  ok("empty/home route maps to index.html", UP.pageURLForHash(base, "/") === base + "index.html" && UP.pageURLForHash(base, "") === base + "index.html");
  ok("item route maps to i/<short>.html", UP.pageURLForHash(base, "commit:abcdef012345@gitmsg/pm") === base + "i/abcdef012345.html");
  ok("item route truncates a long hash to 12", UP.pageURLForHash(base, "commit:abcdef0123456789@gitmsg/review") === base + "i/abcdef012345.html");
  ok("type lists map to their dir index", UP.pageURLForHash(base, "/issues") === base + "issues/index.html" && UP.pageURLForHash(base, "/prs") === base + "prs/index.html" && UP.pageURLForHash(base, "/releases") === base + "releases/index.html" && UP.pageURLForHash(base, "/memos") === base + "memos/index.html");
  // /timeline is app-only: the mixed feed has no page of its own (the front page
  // is the README home view; posts/index.html is kept for it by syncURL's
  // current-page check but is never a rewrite target).
  for (const appOnly of ["/timeline", "/search", "/board", "/analytics", "/code", "/compare:main...x", "/branches", "/graph", "/tags", "/lists", "/config", "/milestones", "/sprints"]) {
    ok("app-only route keeps hash (" + appOnly + ")", UP.pageURLForHash(base, appOnly) === null);
  }

  console.log("\n--- Reverse mapping (popstate) ---");
  ok("index.html -> #/ (home)", UP.hashForPath(base, base + "index.html") === "#/" && UP.hashForPath(base, base) === "#/");
  ok("type dir -> #/<type>", UP.hashForPath(base, base + "issues/index.html") === "#/issues");
  ok("item page -> null (reload handles it)", UP.hashForPath(base, base + "i/abcdef012345.html") === null);
  // posts/index.html routes to /timeline (no posts-only shell tab). The posts
  // archive is recognized as a valid page for /timeline, so its URL is never
  // rewritten away (a reload/copy stays on posts).
  ok("posts/index.html -> #/timeline (its own valid route)", UP.hashForPath(base, base + "posts/index.html") === "#/timeline");
  ok("a ?query/#hash on a page URL is stripped before the reverse map", UP.hashForPath(base, base + "posts/index.html?x=1#frag") === "#/timeline");

  console.log("\n--- App-only routes normalize to the bucket entry ---");
  // An app-only surface (no HTML page) has ONE URL: the bucket entry index.html
  // carrying the view as a hash, regardless of the page the app booted from.
  ok("app-only route -> <base>index.html#<route>", UP.entryURLForHash(base, "/milestones", "") === base + "index.html#/milestones");
  ok("normalization is base-anchored, not page-relative", UP.entryURLForHash(base, "/search", "") === base + "index.html#/search" && UP.entryURLForHash(base, "/board", "") === base + "index.html#/board");
  // ?base=/?repo= cross-bucket overrides ride along verbatim (order preserved,
  // values re-encoded) so a reload/share of the normalized URL still reads the
  // right bucket; unrelated query params are dropped.
  ok("override params preserved verbatim", UP.entryURLForHash(base, "/analytics", "?repo=r&base=b") === base + "index.html?repo=r&base=b#/analytics");
  ok("a lone ?base= override is preserved (re-encoded)", UP.entryURLForHash(base, "/code", "?base=https://x.com/b/") === base + "index.html?base=https%3A%2F%2Fx.com%2Fb%2F#/code");
  ok("non-override query params are dropped", UP.overrideQuery("?foo=bar&x=1") === "" && UP.entryURLForHash(base, "/graph", "?foo=bar") === base + "index.html#/graph");

  // The integration blocks below install a fake window+history (mkBrowser) on the
  // globals gs-upgrade reads; save the shim's originals and restore them after, so
  // the fixture-driven GS.route sections that follow keep the shim's DOM window.
  const savedWindow = global.window, savedHistory = global.history, savedLocation = global.location;

  console.log("\n--- syncURL/wireNav: item page → app-only route → back (history walk) ---");
  // Boot the app from a real item page URL, wire the nav, and prime the entry
  // sync — then navigate to an app-only surface and assert the URL normalizes to
  // the bucket entry (not i/<short>.html#/milestones — the production bug).
  {
    // boot() seeds the page's own route on the hash before the entry syncURL; for
    // an item page that is its commit route, whose pageURL is the item page — so
    // the entry sync keeps the item URL (residual hash stripped).
    const itemURL = base + "i/abcdef012345.html";
    const win = mkBrowser(itemURL + "#commit:abcdef012345@gitmsg/pm");
    UP._resetSync();
    UP.wireNav(base);
    UP.syncURL(base); // entry replaceState (keeps the item page URL, strips the hash)
    ok("boot on item page keeps the item URL", win.href() === itemURL, "href=" + win.href());
    win.navigate("#/milestones");
    ok("nav to app-only route normalizes to entry (not the item path)", win.href() === base + "index.html#/milestones", "href=" + win.href());
    // Back must restore the item page URL; item pages have no derivable hash, so
    // the popstate handler reloads (a real bucket object re-serves + re-upgrades).
    win.back();
    ok("back restores the item page URL", win.href() === itemURL, "href=" + win.href());
    ok("back onto an item page triggers a reload (re-upgrade)", win.reloaded() === true);
    // Forward re-normalizes to the same entry URL (no duplicate entry, no path leak).
    win.forward();
    ok("forward re-normalizes to the same entry URL", win.href() === base + "index.html#/milestones", "href=" + win.href());
  }

  console.log("\n--- syncURL: type-list page → search normalizes; same-route no-dup ---");
  {
    const listURL = base + "issues/index.html";
    const win = mkBrowser(listURL);
    UP._resetSync();
    UP.wireNav(base);
    UP.syncURL(base);
    win.navigate("#/search");
    ok("type-list page → search normalizes to entry", win.href() === base + "index.html#/search", "href=" + win.href());
    const depth = win.stack.length;
    win.navigate("#/search"); // same route again
    ok("re-navigating the same app-only route pushes no duplicate entry", win.stack.length === depth, "stack=" + win.stack.length + " expected " + depth);
  }

  console.log("\n--- syncURL: override params survive normalization ---");
  {
    // The ?base=/?repo= override enters via index.html; hash navigation keeps the
    // query, so an app-only nav from the front page must carry it onto the entry.
    const entryURL = base + "index.html?repo=r&base=b";
    const win = mkBrowser(entryURL);
    UP._resetSync();
    UP.wireNav(base);
    UP.syncURL(base); // front page entry: home → keeps index.html + query
    win.navigate("#/board");
    ok("app-only nav preserves ?base=/?repo= override", win.href() === base + "index.html?repo=r&base=b#/board", "href=" + win.href());
  }

  console.log("\n--- syncURL: plain static shell (entry page) is unchanged ---");
  {
    // On the plain shell the path is already index.html: normalizing an app-only
    // route must not push a spurious history entry (it only reconciles the hash).
    const shellURL = base + "index.html#/analytics";
    const win = mkBrowser(shellURL);
    UP._resetSync();
    UP.wireNav(base);
    const depth0 = win.stack.length;
    UP.syncURL(base);
    ok("shell app-only entry URL is unchanged", win.href() === shellURL && win.stack.length === depth0, "href=" + win.href());
  }
  global.window = savedWindow; global.history = savedHistory; global.location = savedLocation;

  console.log("\n--- Served pages carry the boot hooks ---");
  const front = await get(base + "index.html");
  ok("front page served", front.status === 200);
  ok("front carries gs-route + data-base + upgrade script", /name="gs-route" content="\/"/.test(front.text) && /data-base="\.\/"/.test(front.text) && /<script defer src="\.\/gs-upgrade\.js">/.test(front.text));

  // Discover a real item to address its page + route.
  const ctx0 = GS.newContext(base);
  const issue = (await GS.loadExtItemsAll(ctx0, "pm")).find((i) => (i.header.type || "issue") === "issue");
  ok("discovered a pm issue", !!issue);
  if (!issue) { console.log("\n" + pass + " passed, " + (fail + 1) + " failed"); process.exit(1); }
  const short = issue.commit.short;
  const itemPage = await get(base + "i/" + short + ".html");
  ok("item page served + readable without JS", itemPage.status === 200 && /<h1>/.test(itemPage.text));
  ok("item page carries gs-route (item) + data-base(../) + upgrade script", new RegExp('name="gs-route" content="commit:' + short + '@gitmsg/pm"').test(itemPage.text) && /data-base="\.\.\/"/.test(itemPage.text) && /<script defer src="\.\.\/gs-upgrade\.js">/.test(itemPage.text));

  console.log("\n--- A page entry boots the app onto its route ---");
  // Front page: meta route / → the app renders the home (README) view.
  const frontView = await bootLike(base, "/", null);
  ok("front-page meta route boots the home view", frontView.length > 0, "view=" + frontView.slice(0, 60));
  // Item page: meta route commit:<short>@gitmsg/pm → the app renders that issue.
  const itemView = await bootLike(base, "commit:" + short + "@gitmsg/pm", null);
  const subject = GS.itemSubject ? GS.itemSubject(issue) : "";
  ok("item-page meta route boots that item's detail", subject ? itemView.includes(subject) : itemView.length > 0, "view=" + itemView.slice(0, 80));

  console.log("\n--- Hash deep-link wins over the page's meta route ---");
  // An item page (meta = the issue) receiving a #/prs deep-link must boot /prs,
  // not the issue — a code-commit/legacy shared link landing on any page works.
  const deepView = await bootLike(base, "commit:" + short + "@gitmsg/pm", "#/prs");
  ok("hash route overrides the meta route", /pull request|No pull requests|Expand notes/i.test(deepView), "view=" + deepView.slice(0, 80));

  console.log("\n--- Broken upgrade (404) leaves the readable page intact ---");
  // Simulate the upgrade never loading: the static item page's HTML is complete
  // and readable on its own (the app is pure enhancement). A missing gs-upgrade.js
  // 404s and run() never fires, so the crawlable content stays.
  const bad = await get(base + "i/" + short + ".html");
  ok("item page reads standalone (subject + body present)", bad.status === 200 && /<h1>/.test(bad.text) && /class="meta"/.test(bad.text));
  const missing = await get(base + "does-not-exist-gs-upgrade.js");
  ok("a 404'd asset is a real 404 (upgrade never boots, page stays)", missing.status === 404);

  console.log("\n--- Guards-off bucket serves the static shell at index.html ---");
  const shell = await get(origin + "/" + OTHER + "/index.html");
  ok("guards-off index.html is the SPA shell (no gs-route)", shell.status === 200 && !/name="gs-route"/.test(shell.text) && /id="view"/.test(shell.text));

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
