// verify_upgrade_boot.js - the page-entry boot contract (gs-upgrade.js):
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
//   3. The REAL boot(), run against a fake page whose assets resolve from a
//      fixture map (mkPage): WHEN the static content comes back or goes away is
//      ordering, not a pure function. A JS visitor never sees content that is
//      about to change — the page's own head script hides the static content
//      before the body paints — and the takeover then arrives in TWO steps: the
//      app's chrome as soon as pages-app.css can style it, and the first view
//      into that chrome's content slot once it has settled. So an entry reads
//      blank-loading → chrome → content, with the content transition still
//      happening exactly once and the chrome not moving across it. Both steps are
//      reversible, and each way the boot can fail (a 404 or a hang on any shell
//      asset, a throw, a route that never settles) puts the readable, styled
//      static page back on screen AND takes the chrome back off it.
require("./shim.js");
// prismFetches counts bucket requests for prism.js. The tokenizer is no longer
// part of the boot — the reader fetches it the first time something actually
// asks to be highlighted — so "which routes pay for it" is now an assertable
// property, and it is counted from here (before gs-app's own boot route runs) so
// nothing slips in uncounted.
let prismFetches = 0;
const realFetch = global.fetch;
global.fetch = (url, opts) => { if (/\/prism\.js(?:\?|$)/.test(String(url))) prismFetches++; return realFetch(url, opts); };
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
// findClass collects the rendered nodes carrying a class, for reading what the
// booted view actually painted.
function findClass(node, cls, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (c._cls && c._cls.has(cls)) out.push(c); findClass(c, cls, out); } } return out; }
// findTag collects the rendered nodes whose tag name matches, for comparing the
// app's rendered structure against the served page's.
function findTag(node, re, out) { out = out || []; for (const c of (node && node._children) || []) { if (c && c.nodeType === 1) { if (re.test((c.tagName || "").toLowerCase())) out.push(c); findTag(c, re, out); } } return out; }
// unesc turns the template's escaped text back into what the app renders, so
// page text compares against rendered text rather than against entities.
function unesc(s) { return String(s).replace(/&#34;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&"); }
// pageText strips a served page's markup down to its visible text, so a static
// page can be compared against what the app renders rather than against markup.
function pageText(html) { return unesc(html.replace(/<[^>]+>/g, " ")).replace(/\s+/g, " "); }

// The page-entry reveal handshake. gs-upgrade holds the page on its loading
// state and installs window.__gsOnFirstView; gs-app calls it once the first route
// has settled INCLUDING the home view's deferred recent-activity fill, which is
// what makes the app appear once and FINISHED rather than filling in in pieces
// while the visitor watches. Requiring gs-app boots it (the shim defines a
// document), so the callback is installed HERE, synchronously, before that first
// route can settle; what it recorded is asserted further down.
let revealCalls = 0, revealCards = -1;
global.window.__gsOnFirstView = () => { revealCalls++; revealCards = findClass(global.__shim.viewNode, "card").length; };

// bootLike drives the app the way gs-upgrade would after loading the assets: set
// the boot hash (entryFor: a real location.hash wins over the meta route,
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

// ASSET_MS models per-request latency. The boot's steps have to be ordered in
// TIME, not collapsed into one microtask flush, or "what was on screen while
// this request was in flight" has no meaning, and that question is the whole
// subject of the reveal contract.
const ASSET_MS = 5;

// LOADING is what the served page paints while `gs-boot` is on <html>: the
// inline rules hide #gs-page and put the app's own loading treatment in its
// place. The suite models it as the visible text, which is what a reader sees.
const LOADING = "Loading…";
// CHROME marks the app's own frame being on screen — the nav and the two-panel
// layout, revealed as soon as pages-app.css governs the page and long before the
// first view's data has been read. Its own text is fixed and says nothing about
// how far the boot has got, so the suite models a chrome frame as this marker
// plus the state of the content slot, which is the part that changes.
const CHROME = "[chrome]";
// APP_VIEW stands in for the first view's rendered content: what gs-app puts in
// #view before it signals. It has to be distinguishable from the loading state,
// because "the slot was already filled but still showed loading" is exactly the
// property the `gs-loading` class exists to provide.
const APP_VIEW = "APP VIEW";
const CHROME_LOADING = CHROME + " " + LOADING;
const CHROME_VIEW = CHROME + " " + APP_VIEW;

// mkPage builds a served static page plus the document/window/history globals
// gs-upgrade reads, so the REAL boot() runs headless. Each shell asset resolves
// after ASSET_MS out of `serves` (an explicit false is a 404), and every request
// SAMPLES what the body is showing, so a test reads the visitor's sequence of
// visible states instead of inferring it from the source. activate()/restore()
// bracket the global swap so the shim's own DOM survives the block.
//
// The page starts in the state the browser reaches it in: the inline head script
// has already run, so documentElement carries the boot class and the static
// content is hidden behind it. `visible()` resolves that class the way the
// stylesheet does, which is what lets the suite assert that no state in the
// sequence is the static content — and that a failed boot brings it back.
function mkPage(opts) {
  const mk = global.__shim.mkEl;
  const shimDoc = global.document, shimWin = global.window, shimHist = global.history;
  const serves = opts.serves || {};
  const body = mk("body");
  // The static page: a #gs-page mount carrying the page's own readable content,
  // and its own styling layer in the head (what reveal drops).
  const mount = mk("div");
  mount.setAttribute("id", "gs-page");
  mount.setAttribute("data-base", opts.dataBase);
  mount.append(opts.content);
  body.appendChild(mount);
  const meta = mk("meta");
  meta.setAttribute("name", "gs-route");
  meta.setAttribute("content", opts.metaRoute);
  const head = mk("head");
  const pageStyle = mk("style");
  head._children.push(pageStyle);
  pageStyle._parent = head;
  // documentElement models <html> and its two class flags, which between them are
  // the whole hide mechanism. The page's inline head script adds "gs-boot" before
  // the body is parsed; gs-upgrade swaps it for "gs-loading" when the chrome
  // takes the screen (pages-app.css hangs the content slot's loading treatment
  // off that one), and drops "gs-loading" when the first view lands. A restore
  // clears both. Nothing else in the boot touches them.
  // opts.cloaked === false models the entry the conditional boot script now
  // produces on the front page: the served content is already what this route
  // renders, so nothing is hidden and the visitor is reading the page while the
  // shell is still downloading.
  const htmlClasses = new Set(opts.cloaked === false ? [] : ["gs-boot"]);
  const documentElement = {
    classList: { add: (c) => htmlClasses.add(c), remove: (c) => htmlClasses.delete(c), contains: (c) => htmlClasses.has(c) },
  };
  const booting = () => htmlClasses.has("gs-boot");
  const viewLoading = () => htmlClasses.has("gs-loading");
  // findIn walks for the first element the predicate accepts. The staged chrome
  // is parsed out of gs-upgrade's CHROME constant, so its id/class land in the
  // shim's attribute map rather than its class set — match on attributes.
  const findIn = (n, pred) => {
    for (const c of (n && n._children) || []) {
      if (c && c.nodeType === 1) { if (pred(c)) return c; const d = findIn(c, pred); if (d) return d; }
    }
    return null;
  };
  const shellNode = () => findIn(body, (n) => /(^|\s)shell(\s|$)/.test(n.getAttribute("class") || ""));
  const viewNode = () => findIn(body, (n) => n.getAttribute("id") === "view");
  // chromeUp: the app's frame is on screen. stageChrome appends the chrome's top
  // level nodes to the body at display:none and the reveal unhides them, so the
  // shell's own display is the honest read — and it stays honest after a restore,
  // which puts them back to hidden.
  const chromeUp = () => { const s = shellNode(); return !!s && s.style.display !== "none"; };
  // visible: what a reader would see. Three shapes, in the order the boot can
  // produce them: the served page's own loading line while `gs-boot` is set, the
  // app's frame (plus whatever its content slot is showing) once the chrome is
  // revealed, and the static content whenever it is neither hidden by the boot
  // class nor by the reveal. The static text is appended rather than replaced so
  // "the chrome came up but the static content is showing under it" cannot hide.
  const staticShown = () => (mount.style.display === "none" ? "" : global.__shim.textOf(mount).replace(/\s+/g, " ").trim());
  const visible = () => {
    if (booting()) return LOADING;
    const stat = staticShown();
    if (!chromeUp()) return stat;
    const slot = viewLoading() ? LOADING : global.__shim.textOf(viewNode()).replace(/\s+/g, " ").trim();
    return CHROME + " " + slot + (stat ? " | " + stat : "");
  };
  const samples = [];
  // Each sample also records location.hash, which is the only channel gs-app's
  // init reads: the sample taken as gs-app.js is requested is therefore the route
  // the app is about to boot, before syncURL rewrites the URL to a clean page URL.
  const sample = (tag) => samples.push({ tag: tag, visible: visible(), hash: state.hash });
  // events is the interleaving of requests and settles, recorded separately
  // because that interleaving IS the difference between loading the shell in
  // parallel and loading it serially. A serial boot reads request/settle/request/
  // settle…; a parallel one requests everything before the first byte arrives.
  // Each request also carries what the boot put on the element (rel for links,
  // async for scripts), since async=false is what buys parallel download without
  // giving up execution order.
  const events = [];
  // The network. A script/link append is a request: record it, then (after the
  // flight) sample what the page was showing and settle it.
  head.appendChild = (n) => {
    head._children.push(n);
    const name = String(n.src || n.href || "").split("/").pop();
    events.push({ ev: "request", name: name, rel: n.rel || "", async: n.async });
    setTimeout(() => {
      events.push({ ev: "settle", name: name });
      sample(name);
      if (serves[name] === false) { if (n.onerror) n.onerror(); return; }
      if (n.onload) n.onload();
    }, ASSET_MS);
    return n;
  };
  const state = { href: opts.url + (opts.hash || ""), hash: opts.hash || "", search: "" };
  const apply = (u) => {
    const abs = String(u).charAt(0) === "#" ? state.href.split("#")[0] + u : String(u);
    const hi = abs.indexOf("#");
    const rest = hi >= 0 ? abs.slice(0, hi) : abs;
    const qi = rest.indexOf("?");
    state.href = abs; state.hash = hi >= 0 ? abs.slice(hi) : ""; state.search = qi >= 0 ? rest.slice(qi) : "";
  };
  const loc = {
    get href() { return state.href; }, get hash() { return state.hash; }, get search() { return state.search; },
    get pathname() { try { return new URL(state.href).pathname; } catch (e) { return "/"; } },
    set hash(h) { apply(h.charAt(0) === "#" ? h : "#" + h); },
  };
  const doc = {
    head: head, body: body, documentElement: documentElement,
    createElement: (t) => mk(t),
    createTextNode: (v) => shimDoc.createTextNode(v),
    getElementById: (id) => (id === "gs-page" ? mount : shimDoc.getElementById(id)),
    // Two selectors reach here from the boot: the gs-route meta, and the static
    // styling layer reveal captures. wireChrome's ".nav" falls through to the
    // shim document, whose null is the no-op that chrome wiring is built for.
    querySelector: (sel) => (/gs-route/.test(sel) ? meta : shimDoc.querySelector(sel)),
    querySelectorAll: (sel) => (/stylesheet/.test(sel) ? [pageStyle] : shimDoc.querySelectorAll(sel)),
    addEventListener: () => {},
  };
  const win = { location: loc, addEventListener: () => {}, matchMedia: () => ({ matches: false }) };
  // throwAfterChrome injects a failure into the window write the boot makes right
  // after the chrome goes up (installing the first-view handshake). It is the
  // stand-in for "a throw anywhere in boot()", placed in the window that now
  // matters most: the app's frame is already on screen, so boot()'s catch has a
  // real takeover to unwind rather than nothing at all.
  if (opts.throwAfterChrome) {
    Object.defineProperty(win, "__gsOnFirstView", {
      configurable: true, get: () => undefined,
      set: () => { throw new Error("handshake install failed"); },
    });
  }
  return {
    win: win, visible: visible, sample: sample, samples: samples, events: events,
    booting: booting, viewLoading: viewLoading, chromeUp: chromeUp,
    // fillView models gs-app rendering its first view into the slot. It runs
    // BEFORE the handshake, as the app does, so the suite can assert that a
    // filled slot still reads as loading until the signal.
    fillView: (t) => { const v = viewNode(); if (v) v.textContent = t; return !!v; },
    // states is the deduped sequence of visible states: the measurement.
    states: () => samples.map((s) => s.visible).filter((v, i, a) => i === 0 || v !== a[i - 1]),
    appCSS: () => head._children.find((n) => /pages-app\.css$/.test(String(n.href || ""))) || null,
    // styledByPage: the served page's own sheet is GOVERNING. The reveal suspends
    // it by media rather than detaching it, because a boot that fails after the
    // chrome is up has to hand the page back exactly as served; only the final
    // reveal, once restoring is off the table, drops it from the head for good.
    styledByPage: () => head._children.indexOf(pageStyle) >= 0 && pageStyle.media !== "not all",
    activate: () => {
      global.document = doc; global.window = win;
      global.history = { replaceState: (_s, _t, u) => apply(u), pushState: (_s, _t, u) => apply(u) };
    },
    restore: () => { global.document = shimDoc; global.window = shimWin; global.history = shimHist; },
  };
}

// runBoot drives the real boot() over a page and reports how it settled. The
// first-view signal is delivered ONLY when the boot resolved, because gs-app.js
// is what delivers it: a boot that never loaded the app never gets one, and
// faking it would hide exactly the failure this suite is here to pin.
async function runBoot(page) {
  page.activate();
  UP._resetSync();
  page.sample("entry");
  let err = null;
  try {
    try { await UP.boot(); } catch (e) { err = e; }
    page.sample("app loaded");
    const beforeSignal = page.visible();
    // gs-app renders into #view and THEN signals. Model both, in that order: the
    // slot's loading treatment is what keeps that render off screen until the
    // signal, and a model that only signalled would never test it.
    if (!err) page.fillView(APP_VIEW);
    page.sample("app rendered");
    const beforeReveal = page.visible();
    if (!err && typeof page.win.__gsOnFirstView === "function") page.win.__gsOnFirstView();
    page.sample("first view settled");
    return { err: err, beforeSignal: beforeSignal, beforeReveal: beforeReveal, after: page.visible() };
  } finally { page.restore(); }
}

// duringLoad is what the page showed while each shell asset was in flight.
function duringLoad(page) { return page.samples.filter((s) => /\.(js|css)$/.test(s.tag)).map((s) => s.visible); }
// shownAt is what the page was showing as the named asset settled, keyed by name
// rather than by position: the chrome reveal falls between two settles of the one
// parallel batch, so which side of it an asset lands on is the assertion, and an
// index would pin the batch's internal ordering instead.
function shownAt(page, name) { return page.samples.filter((s) => s.tag === name).map((s) => s.visible); }

// bootHash is location.hash at the moment gs-app.js was requested: the route the
// app boots on, read before syncURL rewrites the URL into a clean page URL.
function bootHash(page) { const s = page.samples.find((x) => x.tag === "gs-app.js"); return s ? s.hash : "(never requested)"; }

async function main() {
  console.log("--- The boot reveal handshake (one visual transition) ---");
  // FIRST, before this suite drives any route of its own: the boot's first route
  // is the one gs-app auto-ran on require, and its rendered view stays mounted
  // only until the next route replaces it. The recorder installed at load time
  // captured that reveal.
  // Its assertion pins the reveal contract: the landing is complete when the app
  // becomes visible, and it does NOT wait for recent activity. That section is
  // below the fold and costs roughly two thirds of home's fetches, so waiting
  // for it held the loading state about 1.9s longer for nothing the visitor
  // could see. It fills in a beat later, as it does in the plain shell.
  {
    for (let i = 0; i < 60 && revealCalls === 0; i++) await wait(50);
    ok("the app signalled the page-entry reveal", revealCalls === 1, "calls=" + revealCalls);
    ok("the reveal did not wait for the recent-activity section", revealCards === 0, "cards at reveal=" + revealCards);
    // That last one is an observation of a race the section lost, not the rule:
    // it holds only because the fill needs a network round trip. The RULE is
    // that signalFirstView awaits ctx.viewSettled and the home view publishes
    // none, so the section can never be part of what the reveal waits for.
    // Asserted on a context of this suite's own, which leaves the boot's
    // rendered view (read above) untouched.
    const homeCtx = GS.newContext(base);
    await GS.homeView(homeCtx);
    ok("because the home view publishes no settle promise for the reveal to await",
      homeCtx.viewSettled === undefined, "viewSettled=" + typeof homeCtx.viewSettled);
    ok("the app cleared the callback so no later route re-reveals", global.window.__gsOnFirstView === null);
  }

  console.log("\n--- Prism is loaded by the render, not by the boot ---");
  // 12 kB of tokenizer used to be fetched on every page entry, including the many
  // routes with no code on them. It now loads on the first render that actually
  // highlights something. Driven FIRST, while nothing has highlighted yet, so
  // "not loaded" is a fact about the boot rather than an accident of ordering.
  {
    const prismIn = () => !!(global.window.Prism && global.window.Prism.tokenize);
    ok("the boot finished with no tokenizer in the page", !prismIn() && prismFetches === 0, "loaded=" + prismIn() + " fetches=" + prismFetches);
    // A route with no code on it: the branch list is rows of refs, no markdown,
    // no blobs, no diffs. It must not pay for the tokenizer.
    const before = prismFetches;
    setHash("#/branches");
    await GS.route(GS.newContext(base));
    await wait(200);
    ok("a route with no code on it never fetches prism.js", prismFetches === before, "fetches=" + (prismFetches - before));
    ok("and leaves the page with no tokenizer at all", !prismIn(), "loaded=" + prismIn());

    // A route that DOES highlight: a source blob. The pane renders immediately
    // with whatever is loaded (plain), and the load it kicks off is registered on
    // highlightsSettled — the same wait the page-entry reveal takes, which is what
    // keeps a first view from appearing as plain code that highlights a beat
    // later. Awaiting it here is exactly what the reveal does.
    setHash("#file:hello.py@main");
    await GS.route(GS.newContext(base));
    await GS.highlightsSettled();
    await wait(50);
    ok("a route with code fetches prism.js exactly once", prismFetches === before + 1, "fetches=" + (prismFetches - before));
    ok("the tokenizer is in the page after that route", prismIn(), "loaded=" + prismIn());
    const tokens = findClass(global.__shim.viewNode, "token");
    ok("the blob is highlighted by the time the reveal wait resolves", tokens.length > 0, "token spans=" + tokens.length);

    // Cached from here: a second highlighting render reuses the loaded tokenizer
    // and highlights synchronously, with no second fetch.
    const parent = GS.el("code", {}, []);
    GS.highlightTo(parent, "const x = 1;\n", "javascript");
    ok("a later block highlights synchronously from the loaded tokenizer", findClass(parent, "token").length > 0, "token spans=" + findClass(parent, "token").length);
    ok("and costs no second prism.js fetch", prismFetches === before + 1, "fetches=" + (prismFetches - before));
  }

  console.log("\n--- Pure route↔page-URL mapping (pushState targets) ---");
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

  console.log("\n--- The commits list is a page route, anchors and all ---");
  // The commits list is the one route family whose ROWS are addressable, so the
  // grammar carries the anchor as a first-class suffix and the page mapping has
  // to split page from place: the page is the object to load, the anchor is where
  // in it to land. Without the split the citable URL a commits page hands out
  // (commits/7.html#c-<sha>) is flattened to the page on the first sync.
  ok("commits head/sealed pages map to their objects", UP.pageURLForHash(base, "/commits") === base + "commits/index.html" && UP.pageURLForHash(base, "/commits/7") === base + "commits/7.html");
  ok("an anchored commits route still maps to the PAGE", UP.pageURLForHash(base, "/commits/7:c-abcdef012345") === base + "commits/7.html");
  ok("and the anchor is split off separately", UP.routeAnchor("/commits/7:c-abcdef012345") === "#c-abcdef012345" && UP.routeAnchor("/commits/7") === "" && UP.routeAnchor("/issues") === "");
  ok("commits pages map back to their routes", UP.hashForPath(base, base + "commits/index.html") === "#/commits" && UP.hashForPath(base, base + "commits/12.html") === "#/commits/12");
  ok("the commits grammar rejects a non-numeric page", GS.parseRoute("#/commits/x").type === "notfound");

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

  console.log("\n--- The hide is JS-gated: the served document is never hidden ---");
  // A page hidden in the markup or by an unconditional CSS rule, then revealed by
  // script, is what search engines treat as cloaking, and it takes the no-JS
  // contract this whole layer exists for with it. So the served bytes hide
  // NOTHING: every hide rule is qualified by a class only a script can add, and
  // the mount carries no hiding attribute or inline style of its own.
  {
    const head = front.text.slice(0, front.text.indexOf("</head>"));
    const hideRules = (head.match(/[^{}]*\{[^{}]*display:none[^{}]*\}/g) || []);
    ok("every display:none rule in the page's own CSS is class-gated", hideRules.length > 0 && hideRules.every((r) => /html\.gs-boot\s/.test(r)), JSON.stringify(hideRules));
    ok("the mount is served plain (no hidden attribute, no inline display)", /<div id="gs-page" data-base="[^"]*">/.test(front.text) && !/id="gs-page"[^>]*(hidden|style=)/.test(front.text));
    ok("the served <html> carries no boot class of its own", /<html lang="en">/.test(front.text));
    // The hide has to beat first paint, so it cannot wait for the deferred
    // gs-upgrade.js: it is an inline script in the head, before the body.
    const bootScript = /<script>\(function\(d,w\)\{[^<]*gs-boot[^<]*\}\)\(document,window\)<\/script>/.exec(head);
    ok("an inline head script is what adds the class (synchronous, pre-body)", !!bootScript, "head tail=" + head.slice(-200));
    ok("that script runs before the body, and before the deferred upgrade", head.indexOf("gs-boot") < head.indexOf("gs-upgrade.js"));
    // And it stands its own failsafe down only for a gs-upgrade.js that actually
    // ran: an upgrade script that 404s or fails to parse leaves nobody else to
    // un-hide the page, which is the one way this mechanism could strand a reader.
    ok("the script un-hides itself if the upgrade never takes ownership", !!bootScript && /__gsBooting/.test(bootScript[0]) && /addEventListener\("load"/.test(bootScript[0]) && /setTimeout\(u,\d+\)/.test(bootScript[0]), bootScript && bootScript[0]);
    // The loading line is the app's own treatment (muted, centered, generous
    // padding) so the boot and the app that follows read as one design.
    ok("the loading state is painted by the same class, not by markup", /html\.gs-boot body::before\{content:"Loading…";[^}]*text-align:center/.test(head), "head=" + head.slice(head.indexOf("<style>"), head.indexOf("</style>")).slice(0, 400));
    // The whole point of the no-JS contract: the page still reads.
    ok("the served page is complete without JS (content in the body as served)", /<h1>/.test(front.text) && /Recent activity/.test(front.text));

    // The cloak is conditional, so the condition is worth RUNNING rather than
    // reading. The script is executed here against a stub document/window for
    // each entry shape, because the decision it makes is the difference between
    // a front page that paints its finished content immediately and one that
    // holds a loading line until ~149 KB of shell arrives.
    const cloakDecision = (route, hash) => {
      const src = bootScript[0].replace(/^<script>/, "").replace(/<\/script>$/, "");
      const classes = new Set();
      const d = {
        documentElement: { classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c) } },
        querySelector: (sel) => (/gs-route/.test(sel) ? { getAttribute: () => route } : null),
      };
      const w = { location: { hash }, addEventListener: () => {}, __gsBooting: false };
      new Function("document", "window", "setTimeout", src)(d, w, () => 0);
      return classes.has("gs-boot");
    };
    ok("the front page entered plainly is NOT cloaked", cloakDecision("/", "") === false);
    ok("a bare row/heading anchor on the front page is NOT cloaked", cloakDecision("/", "#md-about") === false && cloakDecision("/", "#quick-start") === false);
    ok("the home route spelled as a fragment is NOT cloaked", cloakDecision("/", "#/") === false);
    ok("a deep link to another route IS cloaked", cloakDecision("/", "#/issues") === true && cloakDecision("/", "#/commits/2") === true);
    ok("a ref-shaped deep link IS cloaked", cloakDecision("/", "#commit:abcdef012345@main") === true && cloakDecision("/", "#file:README.md@main") === true);
    // Every other generated page still cloaks unconditionally: its content is a
    // page of the corpus, and the app replaces it with a route's own rendering.
    ok("a non-front page is cloaked however it is entered", cloakDecision("/issues", "") === true && cloakDecision("/commits", "#c-abcdef012345") === true && cloakDecision("", "") === true);
  }

  console.log("\n--- The chrome-first reveal's other half lives in pages-app.css ---");
  // The boot reveals the chrome as soon as pages-app.css governs the page and
  // marks <html> with `gs-loading`; that class does nothing on its own. The rules
  // that hold the content slot on a loading line — and hide whatever gs-app has
  // already rendered into it — are in the app stylesheet, a different file, so a
  // rename or a cleanup there would silently turn the split reveal into a slot
  // that fills in while the visitor watches. Pin the pair together.
  {
    const css = await get(base + "pages-app.css");
    ok("pages-app.css served", css.status === 200);
    ok("the class hides whatever the app rendered into the slot", /html\.gs-loading\s+#view\s*>\s*\*\s*\{[^}]*display:\s*none/.test(css.text), "css=" + css.text.slice(0, 120));
    ok("and paints the same loading treatment in its place", /html\.gs-loading\s+#view::before\s*\{[^}]*content:\s*"Loading…"/.test(css.text), "css=" + css.text.slice(0, 120));
    // It is scoped to the class, so the plain shell (which never sets it) and
    // every post-boot navigation are untouched: this is a boot state, not a style.
    ok("nothing hides #view unconditionally", !/(^|\n)\s*#view\s*>\s*\*\s*\{[^}]*display:\s*none/.test(css.text));
    // No cloaking risk from this half: the served static page links pages.css,
    // never pages-app.css, so these rules only exist once the boot has fetched
    // the app stylesheet — which is script-gated by construction.
    ok("the served page does not load the app stylesheet at all", !/pages-app\.css/.test(front.text));

    // The front page is not hidden during the boot any more: it stays on screen
    // while the shell downloads and is then swapped for the app's render of the
    // SAME content. So every metric the two sheets disagree on is a visible jump
    // at that swap — the column moving, or the text re-wrapping under a different
    // size or leading. The pages' inlined base therefore mirrors the shell's
    // numbers, and these assertions are what keep the two in step: change a token
    // in pages-app.css and the page layer has to follow it here.
    const headCSS = front.text.slice(front.text.indexOf("<style>"), front.text.indexOf("</style>"));
    const token = (name) => (new RegExp("--" + name + ":\\s*([^;]+);").exec(css.text) || [])[1];
    const metrics = [
      ["nav column width", token("nav-w"), /#gs-page>nav\{[^}]*width:\s*([0-9.]+px)/.exec(headCSS)],
      ["shell max width", token("shell-max"), /#gs-page\{[^}]*max-width:\s*([0-9.]+px)/.exec(headCSS)],
      ["shell padding", token("shell-pad"), /#gs-page\{[^}]*padding:\s*([0-9.]+rem)/.exec(headCSS)],
      ["column gap", token("nav-gap"), /#gs-page\{[^}]*padding:[^;}]*\+\s*([0-9.]+rem)\)/.exec(headCSS)],
      ["body size", token("fs-body"), /body\{[^}]*font:\s*([0-9.]+px)\//.exec(headCSS)],
    ];
    for (const [label, appValue, pageMatch] of metrics) {
      ok("the pages' " + label + " is the shell's (" + appValue + ")", !!appValue && !!pageMatch && pageMatch[1] === appValue.trim(), "app=" + appValue + " page=" + (pageMatch && pageMatch[1]));
    }
    // Leading is written into the same shorthand on both sides, so it is matched
    // as a pair with the size rather than as a token.
    const appLeading = /font:\s*var\(--fs-body\)\/([0-9.]+)/.exec(css.text);
    const pageLeading = /body\{[^}]*font:\s*[0-9.]+px\/([0-9.]+)/.exec(headCSS);
    ok("the pages' body leading is the shell's", !!appLeading && !!pageLeading && appLeading[1] === pageLeading[1], "app=" + (appLeading && appLeading[1]) + " page=" + (pageLeading && pageLeading[1]));
    // And the single-column fallback flips at the same width, or a desktop fix
    // just moves the jump to mobile.
    const appBP = /@media\s*\(max-width:\s*([0-9]+px)\)/.exec(css.text);
    const pageBP = /@media\s*\(max-width:\s*([0-9]+px)\)\{#gs-page\{[^}]*\}#gs-page>nav\{position:static/.exec(headCSS);
    ok("both collapse to one column at the same breakpoint", !!appBP && !!pageBP && appBP[1] === pageBP[1], "app=" + (appBP && appBP[1]) + " page=" + (pageBP && pageBP[1]));
    // The nav is the sidebar's column rather than a spacer standing in for it,
    // so nothing has to appear at the swap. It is positioned INTO a gutter the
    // content reserves as padding, never laid out as a row of the content's own
    // flow: a nav that participates in that flow sizes a row to the whole link
    // stack and leaves a tall empty band under the first heading.
    ok("the page's own nav occupies the sidebar gutter, out of flow", /#gs-page>nav\{position:absolute;left:1\.25rem/.test(headCSS) && /#gs-page\{[^}]*padding:[^;}]*calc\(/.test(headCSS), headCSS.slice(0, 240));
    ok("the nav cannot size a row of the content flow", !/#gs-page>nav\{[^}]*grid-row/.test(headCSS), headCSS.slice(0, 240));
    // Matching numbers are not enough: they have to be measured the same way.
    // pages-app.css resets box-sizing globally, so the shell's max-width includes
    // its padding. Without the same reset the page's identical 1012px would bound
    // the CONTENT box, put the reserved gutter outside it, and leave the served
    // page 284px wider and centered 142px to the app's left, with the text
    // wrapping at a different measure. Same declarations, different box model.
    ok("the app stylesheet resets the box model globally", /\*\s*\{[^}]*box-sizing:\s*border-box/.test(css.text));
    ok("the pages' box model is the same reset", /\*\{box-sizing:border-box\}/.test(headCSS), headCSS.slice(0, 120));
  }

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

  console.log("\n--- Front page ↔ booted home view agree (no first-load swap) ---");
  // index.html is dual-owned: the static front page paints first and the app's
  // home render replaces that body in place. The two must therefore SHOW THE
  // SAME THING — everything the home view paints has to be on the static page
  // already, and the page must carry nothing the home view drops. A regression
  // here is visible as a flash: one page painted, then swapped for another.
  {
    await bootLike(base, "/", null);
    const view = global.__shim.viewNode;
    const strip = findClass(view, "meta-strip")[0] || { _children: [] };
    const chips = findClass(strip, "chip").map((n) => global.__shim.textOf(n).trim());
    const short = (findClass(strip, "hash")[0] || {}) && global.__shim.textOf(findClass(strip, "hash")[0] || null).trim();
    const subject = global.__shim.textOf(findClass(strip, "meta")[0] || null).split(" · ")[0].trim();
    // The listing collapses past HOME_FILE_LIMIT: only the rows the app leaves
    // visible (plus its "Show all N" control) are part of first paint.
    const shown = findClass(view, "tree-row").filter((r) => r.style.display !== "none").map((r) => global.__shim.textOf(r).trim());
    const more = global.__shim.textOf(findClass(view, "show-more-label")[0] || null).trim();
    const front = await get(base + "index.html");
    const text = pageText(front.text);
    ok("home view rendered a meta strip to compare", chips.length >= 2 && !!short && !!subject, "chips=" + JSON.stringify(chips));
    ok("front page carries the home view's branch chips", chips.every((c) => text.includes(c)), "chips=" + JSON.stringify(chips));
    ok("front page carries the home view's latest commit", text.includes(subject) && text.includes(short), "subject=" + subject + " short=" + short);
    ok("front page lists the same visible root files", shown.length > 0 && shown.every((n) => text.includes(n)), "files=" + JSON.stringify(shown));
    ok("front page carries the same collapse control", !more || text.includes(more), "more=" + more);
    // The README is PRE-RENDERED into the served page (site_markdown.go, a port
    // of the reader's own grammar), so the boot no longer rewrites markup into
    // prose — the one thing the upgrade still adds here is images it can resolve
    // out of the object store. Both halves are asserted: the served section
    // carries the structure the app renders, and none of the source it came from.
    const md = findClass(view, "markdown")[0];
    const appHeadings = findTag(md, /^h[1-6]$/).map((h) => global.__shim.textOf(h).trim()).filter(Boolean);
    const readmeStart = front.text.indexOf('<p class="meta">README</p>');
    const readme = readmeStart < 0 ? "" : front.text.slice(readmeStart, front.text.indexOf("<h2>Recent activity</h2>"));
    ok("home view renders the README as markdown", !!md && appHeadings.length > 0, "headings=" + JSON.stringify(appHeadings));
    ok("front page carries the same headings as real heading elements",
      !!readme && appHeadings.every((h) => new RegExp("<h[1-6][^>]*>" + h.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "</h[1-6]>").test(readme)),
      "headings=" + JSON.stringify(appHeadings) + " section=" + readme.slice(0, 200));
    ok("front page carries the README's rendered structure (hero, list, anchored heading)",
      /<div align="center">/.test(readme) && /<ul>\s*<li>/.test(readme) && /<h2 id="md-about">/.test(readme) && /<a href="#md-about">/.test(readme),
      "section=" + readme.slice(0, 300));
    ok("front page carries an absolute image as an image, and no unresolvable src",
      /<img src="https:\/\/[^"]+"/.test(readme) && !/<img(?![^>]*src="https:)/.test(readme), "section=" + readme.slice(0, 300));
    ok("front page carries no markdown or markup source as text",
      !/&lt;div align=/.test(readme) && !/## About/.test(readme) && !/&gt; /.test(readme), "section=" + readme.slice(0, 300));
    // Recent activity closes both surfaces. The rows ARE the guarantee here: the
    // same items, in the same order, in the same card shape with the same type
    // glyph, or the upgrade swaps one list for another (the original bug, moved
    // down the page). Item hrefs are the crawlable item pages — the reason the
    // section links pages rather than app routes. The section fills in after the
    // landing's own fetches, so wait for the rows rather than assuming a settle time.
    for (let i = 0; i < 40 && findClass(view, "card").length === 0; i++) await wait(50);
    const painted = findClass(view, "card").map((c) => ({
      glyph: global.__shim.textOf(findClass(c, "type-glyph")[0] || null).trim(),
      type: global.__shim.textOf(findClass(c, "chip")[0] || null).trim(),
      subject: global.__shim.textOf(findClass(c, "subject")[0] || null).trim(),
    }));
    const section = front.text.slice(front.text.indexOf("<h2>Recent activity</h2>"));
    // The chip is optional: a code row drops it (its glyph already says commit),
    // so the glyph CLASS is what identifies a row's kind here, not a display label.
    const rows = [...section.matchAll(/<div class="card"><div class="card-head">(?:<span class="type-glyph (tg-[a-z-]+)" title="[^"]*">([^<]*)<\/span> )?(?:<span class="chip">([^<]*)<\/span> )?<a class="subject" href="([^"]+)">([^<]*)<\/a>/g)]
      .map((m) => ({ glyphClass: m[1] || "", glyph: m[2] || "", type: unesc(m[3] || ""), href: m[4], subject: unesc(m[5]) }));
    ok("home view paints recent-activity rows", painted.length > 0, "painted=" + painted.length);
    ok("front page carries the recent-activity section after the README", front.text.indexOf("<h2>Recent activity</h2>") > front.text.indexOf("README"));
    ok("front page lists the same activity rows in the same order", rows.length === painted.length && rows.every((r, i) => r.type === painted[i].type && r.subject === painted[i].subject),
      "page=" + JSON.stringify(rows.map((r) => r.type + ":" + r.subject)) + " app=" + JSON.stringify(painted.map((r) => r.type + ":" + r.subject)));
    // The section is capped at the same round ten on both sides (HOME_ACTIVITY_LIMIT
    // / sitePagesHomeActivity): a summary below the README, not a scrolling log.
    ok("both surfaces cap the section at ten rows", rows.length === 10 && painted.length === 10, "page=" + rows.length + " app=" + painted.length);
    // Glyphs are plain text on both sides, so the no-JS page carries the exact
    // characters the app paints — the card treatment survives the upgrade whole.
    ok("every row carries a type glyph on both surfaces", rows.every((r) => r.glyph) && painted.every((p) => p.glyph), "page=" + JSON.stringify(rows.map((r) => r.glyph)) + " app=" + JSON.stringify(painted.map((p) => p.glyph)));
    ok("the glyphs agree row for row", rows.every((r, i) => r.glyph === painted[i].glyph), "page=" + JSON.stringify(rows.map((r) => r.glyph)) + " app=" + JSON.stringify(painted.map((p) => p.glyph)));
    ok("state-bearing rows carry a tinted glyph class", rows.filter((r) => r.type === "issue" || r.type === "pull request").every((r) => /^tg-(open|closed|merged)$/.test(r.glyphClass)), "classes=" + JSON.stringify(rows.map((r) => r.type + ":" + r.glyphClass)));
    // Code commits interleave with the items on both surfaces. They are the rows
    // that keep the section honest on a repo whose gitmsg corpus is mostly one
    // extension, so the merge (not just the item set) is what must agree.
    const codeRows = rows.filter((r) => r.glyphClass === "tg-commit");
    const itemRows = rows.filter((r) => r.glyphClass !== "tg-commit");
    ok("the merge interleaves code commits", codeRows.length > 0 && rows.length > codeRows.length, "code=" + codeRows.length + " of " + rows.length);
    ok("code rows carry the commit glyph", codeRows.every((r) => r.glyph === "◦" && r.glyphClass === "tg-commit"), "glyphs=" + JSON.stringify(codeRows.map((r) => r.glyph + "/" + r.glyphClass)));
    // A code row is the commit card: glyph, subject, sha, and no chip repeating
    // what the glyph's own title already says. Item rows keep their label.
    ok("code rows carry no chip repeating the glyph", codeRows.every((r) => r.type === ""), "chips=" + JSON.stringify(codeRows.map((r) => r.type)));
    ok("code rows carry the commit's short sha in the meta",
      codeRows.every((r) => new RegExp(" · [0-9a-f]{12}</span>").test(section.slice(section.indexOf(r.href)))),
      "hrefs=" + JSON.stringify(codeRows.map((r) => r.href)));
    ok("item rows keep their type chip", itemRows.every((r) => r.type !== ""), "chips=" + JSON.stringify(itemRows.map((r) => r.type)));
    ok("item rows link to their crawlable item page", itemRows.every((r) => /^\.\/i\/[0-9a-f]{12}\.html$/.test(r.href)), "hrefs=" + JSON.stringify(itemRows.map((r) => r.href)));
    // A code commit has no page of its own, so its row deep-links into the app on
    // the same route the app's own row uses.
    ok("code rows deep-link into the app", codeRows.every((r) => /index\.html#commit:[0-9a-f]{7,40}@/.test(r.href)), "hrefs=" + JSON.stringify(codeRows.map((r) => r.href)));
    // The section closes with the same "See more" control on both surfaces: a real
    // crawlable object on the page (the posts archive IS the page for /timeline),
    // the app's own timeline route once upgraded.
    const appMore = findClass(view, "show-more").filter((n) => /See more/.test(global.__shim.textOf(n)));
    ok("home view closes the section with a See more control", appMore.length === 1 && appMore[0].getAttribute("href") === "#/timeline", "n=" + appMore.length + " href=" + (appMore[0] && appMore[0].getAttribute("href")));
    ok("front page closes the section with the same affordance over a crawlable page", /<a class="show-more" href="\.\/posts\/index\.html"><span class="show-more-icon">⌄<\/span><span class="show-more-label">See more<\/span><\/a>/.test(section), "tail=" + section.slice(-220));
    ok("that page is the one the app maps back to /timeline", UP.hashForPath(base, base + "posts/index.html") === "#/timeline");
  }

  console.log("\n--- Commits page ↔ booted /commits agree (row for row) ---");
  // The commits list exists so code commits have indexable content and a citable
  // URL. Both halves of that only hold if the generated page and the app's own
  // render of the SAME route are the same list: same rows, same order, same
  // count, same subject/author/date/sha, same row ids. A divergence here is the
  // upgrade visibly swapping one changelog for another.
  {
    const page = await get(base + "commits/index.html");
    ok("commits/index.html served + readable without JS", page.status === 200 && /<h1>commits<\/h1>/.test(page.text));
    ok("commits page carries gs-route(/commits) + data-base(../) + upgrade script", /name="gs-route" content="\/commits"/.test(page.text) && /data-base="\.\.\/"/.test(page.text) && /<script defer src="\.\.\/gs-upgrade\.js">/.test(page.text));
    const rows = [...page.text.matchAll(/<div class="card" id="(c-[0-9a-f]{12})"><div class="card-head"><a class="subject" href="([^"]+)">([\s\S]*?)<\/a><\/div>\s*<span class="meta">([^<]*)<\/span><\/div>/g)]
      .map((m) => ({ id: m[1], href: m[2], subject: unesc(m[3]), meta: unesc(m[4]) }));
    ok("commits page lists rows", rows.length > 0, "rows=" + rows.length);
    // Every row is a citable place: an id a URL can name, and a subject linking
    // into the app's commit view (the rich surface a commit already has).
    ok("every row carries its c-<sha12> anchor id", rows.every((r) => r.id === "c-" + r.meta.split(" · ").pop()), JSON.stringify(rows.map((r) => r.id)));
    ok("every row's subject links the app's commit view on the default branch", rows.every((r) => /^\.\.\/index\.html#commit:[0-9a-f]{12}@/.test(r.href)), JSON.stringify(rows.map((r) => r.href)));
    ok("every row's meta is author · date · sha", rows.every((r) => /^.+ · \d{4}-\d{2}-\d{2} · [0-9a-f]{12}$/.test(r.meta)), JSON.stringify(rows.map((r) => r.meta)));
    // The nav is what makes the list reachable from every other crawlable page —
    // and on the commits list itself the nav marks it current (bold, unlinked).
    ok("the commits list is in every other page's nav", /href="\.\.\/commits\/index\.html"/.test((await get(base + "posts/index.html")).text) && /href="\.\/commits\/index\.html"/.test((await get(base + "index.html")).text));
    ok("the commits page's own nav marks it current", /<b>commits<\/b>/.test(page.text) && !/href="\.\.\/commits\/index\.html"/.test(page.text));

    await bootLike(base, "/commits", null);
    const view = global.__shim.viewNode;
    const painted = findClass(view, "card").map((c) => ({
      id: c.getAttribute("id"),
      subject: global.__shim.textOf(findClass(c, "subject")[0] || null).trim(),
      meta: global.__shim.textOf(findClass(c, "meta")[0] || null).replace(/\s+/g, " ").trim(),
    }));
    ok("the app paints the same number of rows", painted.length === rows.length, "page=" + rows.length + " app=" + painted.length);
    // The commits list is a list of code commits, so its rows are the app's
    // code-commit card, the same one the merged timeline and the branch log
    // paint. Rendering them as bare text instead is what made this list read as
    // a different product from the timeline showing the very same commits.
    const cards = findClass(view, "card");
    ok("commits rows are the app's commit card (glyph + linked subject)",
      cards.every((c) => findClass(c, "tg-commit").length === 1 && (findClass(c, "subject")[0] || {}).tagName === "A"),
      JSON.stringify(cards.map((c) => (findClass(c, "tg-commit").length ? "glyph" : "no-glyph") + "/" + ((findClass(c, "subject")[0] || {}).tagName || "none"))));
    ok("the rows agree row for row (id, subject, author/date/sha)",
      rows.every((r, i) => painted[i] && painted[i].id === r.id && painted[i].subject === r.subject && painted[i].meta === r.meta),
      "page=" + JSON.stringify(rows.map((r) => r.id + "|" + r.subject + "|" + r.meta)) + " app=" + JSON.stringify(painted.map((p) => p.id + "|" + p.subject + "|" + p.meta)));
    // The head's own meta line is part of the same agreement: the total is what
    // the writer published, not what the reader happened to drain.
    const pageMeta = /<p class="meta">([^<]*)<\/p>/.exec(page.text.slice(page.text.indexOf("<h1>commits</h1>")));
    const appMeta = global.__shim.textOf(findClass(view, "meta")[0] || null).trim();
    ok("both surfaces head the list with the same count/branch/order line", !!pageMeta && unesc(pageMeta[1]).trim() === appMeta, "page=" + (pageMeta && pageMeta[1]) + " app=" + appMeta);

    console.log("\n--- A row anchor survives the upgrade (the citable URL) ---");
    const anchor = rows[0] && rows[0].id;
    // 1. Entry: the served page scrolled to #c-<sha>, and the boot must turn that
    //    into the page's own route CARRYING the anchor rather than dropping it as
    //    an unroutable fragment (which is what left the app at the top of the list).
    const pg = mkPage({ url: base + "commits/index.html", hash: "#" + anchor, dataBase: "../", metaRoute: "/commits", content: global.__shim.mkEl("div") });
    pg.activate();
    const entry = UP.entryFor();
    pg.restore();
    ok("a bare row anchor boots the page's own route WITH the anchor", entry.route === "/commits:" + anchor && entry.ownPage === true, JSON.stringify(entry));
    // 2. Render: that route paints the list and the anchored row is in it.
    const anchoredView = await bootLike(base, "/commits:" + anchor, null);
    ok("the anchored route renders the same list", findClass(global.__shim.viewNode, "card").length === rows.length, "view=" + anchoredView.slice(0, 60));
    ok("the anchored row is present to scroll to", findClass(global.__shim.viewNode, "card").some((c) => c.getAttribute("id") === anchor));
    // 3. URL: the citable address is what the visitor is left holding.
    const savedW = global.window, savedH = global.history, savedL = global.location;
    const win = mkBrowser(base + "commits/index.html#/commits:" + anchor);
    UP._resetSync();
    UP.wireNav(base);
    UP.syncURL(base);
    ok("the URL settles back on the citable page+anchor", win.href() === base + "commits/index.html#" + anchor, "href=" + win.href());
    global.window = savedW; global.history = savedH; global.location = savedL;
  }

  console.log("\n--- The branch log keeps its own row treatment ---");
  // The commits list and the branch log render the same commit through one card
  // builder, and the commits list overrides three of its defaults: the citable
  // row id, the generated page's absolute date, and the sha12 the page links.
  // Those overrides are opt-in, and this is the assertion that says so: the
  // branch log is the surface with no page to agree with, so it keeps the
  // relative time every other card in the app uses and takes no row anchor.
  // Without this, folding the two builders together lets the commits list's
  // treatment leak across a whole surface with nothing to catch it.
  {
    await bootLike(base, "branch:main", null);
    const cards = findClass(global.__shim.viewNode, "card");
    ok("the branch log paints commit rows", cards.length > 0, "cards=" + cards.length);
    ok("branch log rows show relative time, not the page's absolute date",
      cards.every((c) => findClass(c, "reltime").length === 1),
      JSON.stringify(cards.map((c) => global.__shim.textOf(findClass(c, "meta")[0] || null).replace(/\s+/g, " ").trim())));
    ok("branch log rows take no citable row id (only the commits list has one)",
      cards.every((c) => !c.getAttribute("id")),
      JSON.stringify(cards.map((c) => c.getAttribute("id"))));
  }

  console.log("\n--- Hash deep-link wins over the page's meta route ---");
  // An item page (meta = the issue) receiving a #/prs deep-link must boot /prs,
  // not the issue — a code-commit/legacy shared link landing on any page works.
  const deepView = await bootLike(base, "commit:" + short + "@gitmsg/pm", "#/prs");
  ok("hash route overrides the meta route", /pull request|No pull requests|Expand notes/i.test(deepView), "view=" + deepView.slice(0, 80));

  console.log("\n--- Broken upgrade (404) leaves the readable page intact ---");
  // Simulate the upgrade never loading: the static item page's HTML is complete
  // and readable on its own (the app is pure enhancement). A missing gs-upgrade.js
  // 404s and run() never fires, so nothing sets __gsBooting and the page's own
  // inline failsafe takes the boot class back off — the crawlable content stays.
  const bad = await get(base + "i/" + short + ".html");
  ok("item page reads standalone (subject + body present)", bad.status === 200 && /<h1>/.test(bad.text) && /class="meta"/.test(bad.text));
  const missing = await get(base + "does-not-exist-gs-upgrade.js");
  ok("a 404'd asset is a real 404 (upgrade never boots, page stays)", missing.status === 404);

  // Every route driven since the handshake block is a post-boot navigation, and a
  // navigation must never re-run the reveal: the app is already on screen, and a
  // second reveal would re-strip styles and re-hide nothing.
  ok("the routes driven since the boot signalled no second reveal", revealCalls === 1, "calls=" + revealCalls);

  console.log("\n--- Guards-off bucket serves the static shell at index.html ---");
  const shell = await get(origin + "/" + OTHER + "/index.html");
  ok("guards-off index.html is the SPA shell (no gs-route)", shell.status === 200 && !/name="gs-route"/.test(shell.text) && /id="view"/.test(shell.text));

  // The real boot, driven last: it swaps global.document/window/history, so it
  // runs after every fixture-driven route above has had its say.
  console.log("\n--- The boot's visible states: the page's own route ---");
  const HOST = "FRONT PAGE STATIC CONTENT";
  const ITEM_ROUTE = "commit:abcdef012345@gitmsg/pm";
  const ITEM = "ITEM abcdef012345 STATIC BODY";
  const COMMIT_LINK = "#commit:" + "ef40".repeat(10) + "@main";
  {
    // The page's gs-route is what boots, so the static content IS this route's
    // content — and it is STILL not shown, because it is about to be replaced by
    // the same content in different typography and (on the front page) with the
    // README actually rendered. The visitor gets one loading state and then the
    // finished app, once.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST });
    const r = await runBoot(page);
    ok("matching route: the boot completed", !r.err, "err=" + (r.err && r.err.message));
    // The shell batch is fetched onto the page's own loading line: nothing can be
    // styled until pages-app.css lands, so the chrome cannot precede it and every
    // asset up to and including it settles onto a blank page.
    ok("matching route: the whole shell batch loads on the page's own loading line", duringLoad(page).length === 6 && ["icons.js", "gs-core.js", "gs-render.js", "pages-app.css"].every((n) => shownAt(page, n)[0] === LOADING),
      JSON.stringify(duringLoad(page)));
    // And the chrome is up for BOTH gs-app.js touches — the preload that lands in
    // the same batch and the script that actually runs it. That gap is the whole
    // point of the split reveal: it is where the visitor stops looking at a blank
    // page, and it opens before the app has been asked for, let alone any data.
    ok("matching route: the chrome is up before gs-app.js is ever fetched", shownAt(page, "gs-app.js").length === 2 && shownAt(page, "gs-app.js").every((v) => v === CHROME_LOADING),
      JSON.stringify(shownAt(page, "gs-app.js")));
    ok("matching route: no state in the whole boot is the static content", !page.states().includes(HOST), JSON.stringify(page.states()));

    // The uncloaked entry: the front page reached without a deep link. The
    // served document already IS this route's finished content, so the visitor
    // reads it from first paint and the boot must not disturb it. That inverts
    // the split reveal above: putting the chrome up early here would replace a
    // readable page with a nav over a loading line, so the two steps collapse
    // into the single swap revealApp performs when the app is actually ready.
    {
      const plain = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST, cloaked: false });
      const pr = await runBoot(plain);
      ok("uncloaked: the boot completed", !pr.err, "err=" + (pr.err && pr.err.message));
      ok("uncloaked: no state is ever the loading line", !plain.states().includes(LOADING), JSON.stringify(plain.states()));
      ok("uncloaked: the served content is what the visitor reads from the start", plain.states()[0] === HOST, JSON.stringify(plain.states()));
      // The whole shell downloads while the finished page is on screen, which is
      // the entire point: on a slow link the visitor is reading, not waiting.
      ok("uncloaked: the whole shell batch loads with the page still readable",
        ["icons.js", "gs-core.js", "gs-render.js", "pages-app.css", "gs-app.js"].every((n) => shownAt(plain, n).every((v) => v === HOST)),
        JSON.stringify(["icons.js", "gs-core.js", "gs-render.js", "pages-app.css", "gs-app.js"].map((n) => n + "=" + JSON.stringify(shownAt(plain, n)))));
      // Exactly one visual change, and it is the finished app arriving. No state
      // may show the chrome over a loading slot, and none may show the chrome
      // and the static content at once.
      ok("uncloaked: the chrome never goes up before the app is ready", !plain.states().some((s) => s === CHROME_LOADING || (s.startsWith(CHROME) && s.includes(HOST))), JSON.stringify(plain.states()));
      ok("uncloaked: the entry is exactly served-page then app", plain.states().length === 2 && plain.states()[1].startsWith(CHROME), JSON.stringify(plain.states()));
    }

    // The shell is ONE round trip, not one per file. Every asset is requested
    // before any of them has arrived; only gs-app.js is held back, and only
    // because it auto-runs init() and so must not execute until the chrome is
    // staged and the route seeded. Loaded serially this was six round trips on
    // every page entry, paid by every visitor and scaling with nothing.
    const reqs = page.events.filter((e) => e.ev === "request");
    const firstSettle = page.events.findIndex((e) => e.ev === "settle");
    ok("the whole shell is requested before the first byte of it arrives", firstSettle === 5 && page.events.slice(0, 5).every((e) => e.ev === "request"),
      "firstSettle=" + firstSettle + " head=" + JSON.stringify(page.events.slice(0, 6).map((e) => e.ev + ":" + e.name)));
    ok("the batch is icons + the reader + the app stylesheet", JSON.stringify(reqs.slice(0, 4).map((e) => e.name)) === JSON.stringify(["icons.js", "gs-core.js", "gs-render.js", "pages-app.css"]),
      JSON.stringify(reqs.map((e) => e.name + (e.rel ? "(" + e.rel + ")" : ""))));
    // async=false is the whole trick: parallel download, insertion-order
    // execution, so gs-core still runs before gs-render with no serialization.
    ok("every injected script is async=false (parallel download, ordered execution)", reqs.filter((e) => /\.js$/.test(e.name) && !e.rel).every((e) => e.async === false),
      JSON.stringify(reqs.map((e) => e.name + ":" + e.async)));
    ok("gs-app.js is warmed by a preload in that same batch", reqs[4] && reqs[4].name === "gs-app.js" && reqs[4].rel === "preload", JSON.stringify(reqs[4]));
    ok("and only executed after the shell settled (the chrome is staged by then)", reqs[5] && reqs[5].name === "gs-app.js" && reqs[5].rel === "" && page.events.indexOf(reqs[5]) > firstSettle,
      JSON.stringify(reqs.map((e) => e.name + (e.rel ? "(" + e.rel + ")" : ""))));
    ok("prism.js is not in the boot at all", !reqs.some((e) => e.name === "prism.js"), JSON.stringify(reqs.map((e) => e.name)));
    ok("matching route: the content slot is still loading once the app is loaded", r.beforeSignal === CHROME_LOADING, "visible=" + r.beforeSignal);
    // The property the `gs-loading` class buys: gs-app has already rendered its
    // first view into the slot here, and the visitor is still looking at the
    // loading line. Without it the view would appear the moment it was written,
    // mid-assembly and before its highlights, which is the second transition the
    // handshake exists to prevent.
    ok("matching route: a slot already filled by the app still reads as loading", r.beforeReveal === CHROME_LOADING, "visible=" + r.beforeReveal);
    // Two visual steps, and exactly two: the chrome arrives, then the content
    // lands in it. The static content is in neither, and the chrome does not
    // change between them.
    ok("matching route: the entry is chrome-then-content, once each", JSON.stringify(page.states()) === JSON.stringify([LOADING, CHROME_LOADING, CHROME_VIEW]), JSON.stringify(page.states()));
    ok("matching route: the app stylesheet goes live with the chrome, and the page's own sheet stops governing", page.appCSS() && page.appCSS().media === "all" && !page.styledByPage());
    ok("matching route: the boot marker is cleared when the chrome takes the screen", !page.booting());
    ok("matching route: the loading marker is cleared when the content lands", !page.viewLoading());
  }

  console.log("\n--- The boot's visible states: a deep link to another route ---");
  {
    // A code commit has no page by design, so its link is a hash route onto
    // index.html. index.html's own content is another page's here, and it is
    // hidden for the same reason the matching page's is: it is not what the
    // visitor asked for and it is about to be replaced.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", hash: COMMIT_LINK, dataBase: "./", content: HOST });
    const r = await runBoot(page);
    ok("mismatched route: the boot completed", !r.err, "err=" + (r.err && r.err.message));
    ok("mismatched route: the host page's content is never shown", !page.states().includes(HOST) && r.beforeSignal === CHROME_LOADING, JSON.stringify(page.states()));
    ok("mismatched route: the app stylesheet is live from the chrome reveal on", page.appCSS() && page.appCSS().media === "all" && !page.styledByPage());
    ok("mismatched route: the same chrome-then-content sequence as a matching entry", JSON.stringify(page.states()) === JSON.stringify([LOADING, CHROME_LOADING, CHROME_VIEW]) && r.after !== r.beforeSignal, JSON.stringify(page.states()));
  }

  console.log("\n--- Matching and mismatched entries are now the same boot ---");
  // The old contract branched here: a page booting its own route held the static
  // content for the whole load, a deep link dropped it early for a placeholder.
  // Both now show the loading state and then the finished app, so an entry's
  // sequence no longer depends on where the visitor came from.
  {
    // An item page receiving another item's deep link: its own subject and body
    // would otherwise sit on screen while a different item loads.
    const page = mkPage({ url: base + "i/abcdef012345.html", metaRoute: ITEM_ROUTE, hash: "#commit:0123456789ab@gitmsg/review", dataBase: "../", content: ITEM });
    const r = await runBoot(page);
    ok("item page + another item's deep link never shows the host item", !page.states().includes(ITEM) && r.beforeSignal === CHROME_LOADING, JSON.stringify(page.states()));
  }
  {
    // A shared link to the page's OWN item resolves to the same route the meta
    // names, and gets exactly the same sequence.
    const page = mkPage({ url: base + "i/abcdef012345.html", metaRoute: ITEM_ROUTE, hash: "#" + ITEM_ROUTE, dataBase: "../", content: ITEM });
    const r = await runBoot(page);
    ok("a deep link to the page's own route gets the same two steps", r.beforeSignal === CHROME_LOADING && page.states().length === 3 && !page.states().includes(ITEM), JSON.stringify(page.states()));
  }

  console.log("\n--- An ordinary anchor is not a route ---");
  {
    // A plain HTML anchor into a page's own markup (a "#c-<sha>" commits-list row,
    // a "#reply-…" on an item page) is not a deep link. Booted verbatim it becomes
    // parseRoute's bare-fragment case, which is the HOME view plus an anchor: the
    // visitor asked for a row on this page and would get the README instead.
    const page = mkPage({ url: base + "i/abcdef012345.html", metaRoute: ITEM_ROUTE, hash: "#c-ef408bf738a2", dataBase: "../", content: ITEM });
    const r = await runBoot(page);
    ok("an unroutable anchor falls back to the page's own route", bootHash(page) === "#" + ITEM_ROUTE, "hash=" + bootHash(page));
    ok("an anchor entry still gets the chrome-then-content boot", r.beforeSignal === CHROME_LOADING && page.states().length === 3, "visible=" + r.beforeSignal + " states=" + page.states().length);
  }
  {
    // Off the grammar entirely: same fallback, no notfound view.
    const page = mkPage({ url: base + "i/abcdef012345.html", metaRoute: ITEM_ROUTE, hash: "#nope:whatever", dataBase: "../", content: ITEM });
    const r = await runBoot(page);
    ok("an off-grammar fragment falls back to the page's own route", bootHash(page) === "#" + ITEM_ROUTE, "hash=" + bootHash(page));
    ok("an off-grammar fragment keeps the same two steps", r.beforeSignal === CHROME_LOADING && page.states().length === 3, "visible=" + r.beforeSignal);
  }
  {
    // The exception: the home view's README is what heading anchors are IN, so on
    // a page whose own route is home the fragment rides along untouched and the
    // app scrolls to the heading after it renders.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", hash: "#quick-start", dataBase: "./", content: HOST });
    const r = await runBoot(page);
    ok("a README heading anchor on the home page is left in the URL", bootHash(page) === "#quick-start", "hash=" + bootHash(page));
    ok("a README heading anchor keeps the same two steps", r.beforeSignal === CHROME_LOADING && page.states().length === 3, "visible=" + r.beforeSignal);
  }
  {
    // A file route carries an anchor of its own (#file:<path>@<branch>:<slug>) and
    // IS a destination: the anchor test must not swallow it.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", hash: "#file:README.md@main:intro", dataBase: "./", content: HOST });
    const r = await runBoot(page);
    ok("a file route with a heading anchor is still a real deep link", bootHash(page) === "#file:README.md@main:intro" && r.beforeSignal === CHROME_LOADING, "hash=" + bootHash(page) + " visible=" + r.beforeSignal);
  }

  console.log("\n--- A failed boot restores the served page, never a blank one ---");
  // Hiding the static content is what makes the loading state possible, and it is
  // also the one thing that could strand a visitor: a spinner with nothing behind
  // it is strictly worse than the old hold. So the hide is reversible and EVERY
  // failure path un-does it. Revealing the chrome early raises the stakes rather
  // than changing them: from that point a restore has to take the app's frame
  // back OFF the page as well, or the visitor is left with a dead nav — links
  // that are app routes nothing will ever answer — sitting over the served
  // document. Each case below asserts the same four things: the boot rejected,
  // the static content is back on screen, NO chrome is left behind, and the page
  // is still styled by its own sheet (the app stylesheet never stayed live).
  {
    // gs-app.js is the last asset and the only one fetched after the chrome is
    // revealed, so its failure is the deepest: the takeover is on screen and has
    // to be unwound completely, in front of the visitor.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", hash: COMMIT_LINK, dataBase: "./", content: HOST, serves: { "gs-app.js": false } });
    const r = await runBoot(page);
    ok("mismatched route + failed app: the boot rejected", !!r.err && /gs-app\.js/.test(r.err.message), "err=" + (r.err && r.err.message));
    ok("mismatched route + failed app: the static page is back on screen, readable", page.visible() === HOST && !page.booting(), "visible=" + JSON.stringify(page.visible()));
    ok("mismatched route + failed app: the chrome came back off with it", !page.chromeUp() && !page.viewLoading(), "chromeUp=" + page.chromeUp() + " viewLoading=" + page.viewLoading());
    ok("mismatched route + failed app: the chrome showed, the content never did (chrome → static)", JSON.stringify(page.states()) === JSON.stringify([LOADING, CHROME_LOADING, HOST]), JSON.stringify(page.states()));
    ok("mismatched route + failed app: the page keeps its own styling, the app sheet goes inert again", page.styledByPage() && page.appCSS() && page.appCSS().media === "not all");
  }
  {
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST, serves: { "gs-app.js": false } });
    const r = await runBoot(page);
    ok("matching route + failed app: the readable static page comes back, chrome and all", !!r.err && page.visible() === HOST && page.styledByPage() && !page.chromeUp(), JSON.stringify(page.states()));
  }
  {
    // A throw rather than a failed fetch, injected at the window write the boot
    // makes immediately after revealing the chrome. Nothing 404s here: this is
    // boot()'s catch doing the unwinding, with the frame already painted.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST, throwAfterChrome: true });
    const r = await runBoot(page);
    ok("a throw after the chrome is up: the boot rejected", !!r.err, "err=" + (r.err && r.err.message));
    ok("a throw after the chrome is up: the static page is back, styled, with no chrome", page.visible() === HOST && page.styledByPage() && !page.chromeUp() && !page.booting() && !page.viewLoading(), "visible=" + JSON.stringify(page.visible()));
    ok("a throw after the chrome is up: the app stylesheet is inert again", page.appCSS() && page.appCSS().media === "not all", "media=" + (page.appCSS() && page.appCSS().media));
  }
  // Loading the shell as one batch means a failure can land while its siblings are
  // still in flight. Each one must abort the takeover BEFORE anything is staged —
  // the chrome reveal is gated on the whole batch, so none of these ever paints a
  // frame — and hand back the readable, styled static page exactly as served.
  for (const dead of ["gs-core.js", "gs-render.js", "pages-app.css"]) {
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST, serves: { [dead]: false } });
    const r = await runBoot(page);
    ok("a failed " + dead + " restores the readable static page", !!r.err && page.visible() === HOST && !page.booting(), "err=" + (r.err && r.err.message) + " states=" + JSON.stringify(page.states()));
    ok("a failed " + dead + " never makes the app stylesheet live", page.styledByPage() && (!page.appCSS() || page.appCSS().media === "not all"), "media=" + (page.appCSS() && page.appCSS().media));
    // The gate: an unstyled nav is a worse flash than the blank page it would
    // replace, so the chrome must never reach the screen when the batch it is
    // gated on did not complete. pages-app.css is the load-bearing member here.
    ok("a failed " + dead + " never puts the chrome on screen", !page.chromeUp() && !page.states().includes(CHROME_LOADING), "states=" + JSON.stringify(page.states()));
  }
  {
    // icons.js is the one optional member of the batch: a decorative enhancer
    // whose absence must not cost the visitor the upgrade.
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST, serves: { "icons.js": false } });
    const r = await runBoot(page);
    ok("a failed icons.js still completes the upgrade", !r.err && page.states().length === 3 && !page.states().includes(HOST), "err=" + (r.err && r.err.message) + " states=" + JSON.stringify(page.states()));
  }
  {
    // A route that never settles: gs-app.js loads, so nothing rejects, but the
    // first-view signal never comes. This is now the failure the split reveal
    // makes most vivid — the chrome IS on screen, and staying there is the one
    // outcome that would strand the visitor for good: a nav whose every link
    // routes into an app that never arrived, over a document it is hiding. The
    // give-up watchdog restores instead, chrome and all.
    UP._setBootMaxMs(60);
    const page = mkPage({ url: base + "index.html", metaRoute: "/", dataBase: "./", content: HOST });
    page.activate();
    UP._resetSync();
    page.sample("entry");
    let wderr = null;
    try { await UP.boot(); } catch (e) { wderr = e; }
    page.sample("app loaded");
    const chromeBefore = page.chromeUp();
    await wait(120); // outlive the watchdog without ever signalling a first view
    page.sample("watchdog");
    const visibleAfter = page.visible();
    const stillBooting = page.booting();
    const chromeAfter = page.chromeUp();
    // The late signal a wedged route might eventually deliver must find the latch
    // taken, or the restore would be swapped away again seconds later.
    if (typeof page.win.__gsOnFirstView === "function") page.win.__gsOnFirstView();
    page.sample("late signal");
    const afterLate = page.visible();
    page.restore();
    UP._setBootMaxMs(10000);
    ok("a route that never settles: the boot itself did not reject", !wderr, "err=" + (wderr && wderr.message));
    ok("a route that never settles: the chrome WAS on screen while it waited", chromeBefore === true);
    ok("a route that never settles: the static page is restored, readable and styled", visibleAfter === HOST && !stillBooting && page.styledByPage(), "visible=" + JSON.stringify(visibleAfter));
    ok("a route that never settles: the watchdog took the chrome down with it", chromeAfter === false && !page.viewLoading(), "chromeUp=" + chromeAfter + " viewLoading=" + page.viewLoading());
    ok("a route that never settles: the app stylesheet does not stay live", page.appCSS() && page.appCSS().media === "not all", "media=" + (page.appCSS() && page.appCSS().media));
    ok("a late first-view signal cannot swap the restored page away", afterLate === HOST, "visible=" + JSON.stringify(afterLate));
  }

  console.log("\n" + pass + " passed, " + fail + " failed");
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error("THREW:", e); process.exit(1); });
