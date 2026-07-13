// gs-upgrade.js - the shell's page-entry boot mode (progressive enhancement).
//
// Every generated static page (item pages, type lists, the front page) is a
// complete, readable HTML document on its own. This script upgrades it in place
// into the full SPA: it resolves the artifact base, boots the app on the page's
// route, and hands post-boot navigation clean page URLs so reloads always work.
//
// The writer↔reader contract is exactly three hooks the page stamps (see
// site_pages_html.go): a `<meta name="gs-route">` route, a data-base attribute
// on the `<div id="gs-page">` mount, and the mount div itself. This layer reads
// those, loads the shell assets (styles + gs-core/gs-render/gs-app) relative to
// the resolved base, and lets gs-app.js's init() take over wholesale.
//
// Failure is inert by construction: a page that reads without JS must keep
// reading if the upgrade throws or an asset 404s. Everything below runs under
// try/catch plus a watchdog that only ever ADDS the app — it never blanks the
// static page, so a broken upgrade leaves the crawlable/readable content intact.

(function () {
  // metaRoute reads the page's gs-route hint (the shell's parseRoute grammar).
  function metaRoute() {
    const m = document.querySelector('meta[name="gs-route"]');
    return (m && m.getAttribute("content")) || "";
  }

  // resolveBase returns the absolute artifact base (the site root). The
  // ?base=/?repo= cross-bucket override wins (same class as the shell's
  // deriveBase — the static front-page content refers to the local bucket, but
  // the app takeover honors the override); otherwise the page's data-base
  // attribute (a relative path like "../" or "./") is resolved against the
  // page's own URL so item pages under i/ and type lists under their dirs both
  // anchor at the root.
  function resolveBase() {
    try {
      const params = new URLSearchParams(window.location.search || "");
      const override = params.get("base") || params.get("repo");
      if (override) return override.endsWith("/") ? override : override + "/";
    } catch (e) { /* malformed query — fall through to data-base */ }
    const mount = document.getElementById("gs-page");
    const rel = (mount && mount.getAttribute("data-base")) || "./";
    let abs = new URL(rel, window.location.href).href;
    if (!abs.endsWith("/")) abs += "/";
    return abs;
  }

  // routeFor picks the boot route: a location.hash deep-link WINS over the
  // page's gs-route meta (a shared/legacy #/… link, or a code-commit link from
  // the timeline, must open its target on any page it lands on); otherwise the
  // page's own route. Returned WITHOUT the leading "#" (parseRoute strips it).
  function routeFor() {
    const hash = (window.location.hash || "").replace(/^#/, "");
    if (hash !== "") return hash;
    return metaRoute();
  }

  // The chrome the app renders into: the two-panel nav + content shell, mirroring
  // index.html's <body>. The app fills #view via setView, highlights #nav
  // [data-nav] links, and fills the code sidebar slot; without this chrome the
  // app would boot into a bare #view with no navigation. Kept in sync with
  // index.html's shell (they pop into the same app).
  var CHROME = [
    '<button id="nav-handle" class="nav-handle" aria-label="Show navigation" title="Show navigation">»</button>',
    '<div id="mobile-bar" class="mobile-bar">',
    '  <button id="nav-hamburger" class="nav-hamburger" aria-label="Open navigation" aria-expanded="false" aria-controls="nav"><svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M3 6h18M3 12h18M3 18h18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></button>',
    '  <a id="mobile-title" class="mobile-title" href="#/">repository</a>',
    '</div>',
    '<div id="nav-scrim" class="nav-scrim"></div>',
    '<div class="shell">',
    '  <aside class="nav">',
    '    <div class="nav-header">',
    '      <a class="repo-title" id="repo-title" href="#/">repository</a>',
    '      <button id="theme-toggle" class="theme-icon" aria-label="Toggle dark mode" title="Toggle theme"><svg viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><g id="moon-icon"><mask id="moon-mask"><rect width="20" height="20" fill="white" /><circle cx="16" cy="7" r="10" fill="black" /></mask><circle cx="10" cy="10" r="10" fill="currentColor" mask="url(#moon-mask)" /></g><g id="sun-icon" style="display: none;"><circle cx="10" cy="10" r="3" fill="currentColor" /><path d="M10 1V3M10 17V19M19 10H17M3 10H1M16.5 3.5L15.1 4.9M4.9 15.1L3.5 16.5M16.5 16.5L15.1 15.1M4.9 4.9L3.5 3.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></g></svg></button>',
    '      <button id="width-toggle" class="nav-collapse" aria-label="Toggle layout width" title="Toggle fixed/full width">↔</button>',
    '      <button id="nav-collapse" class="nav-collapse" aria-label="Collapse navigation" title="Collapse navigation">«</button>',
    '    </div>',
    '    <nav id="nav">',
    '      <a href="#/search" data-nav="search"><span class="nav-icon">⌕</span>Search</a>',
    '      <a href="#/" data-nav="home"><span class="nav-icon">⌂</span>Home</a>',
    '      <div class="nav-group"><div class="nav-section">Social</div>',
    '        <a href="#/timeline" data-nav="timeline"><span class="nav-icon">⏱</span>Timeline</a>',
    '        <a href="#/lists" data-nav="lists"><span class="nav-icon">☷</span>Lists</a></div>',
    '      <div class="nav-group"><div class="nav-section">PM</div>',
    '        <a href="#/board" data-nav="board"><span class="nav-icon">▦</span>Board</a>',
    '        <a href="#/issues" data-nav="issues"><span class="nav-icon">○</span>Issues</a>',
    '        <a href="#/milestones" data-nav="milestones"><span class="nav-icon">◇</span>Milestones</a>',
    '        <a href="#/sprints" data-nav="sprints"><span class="nav-icon">◷</span>Sprints</a></div>',
    '      <div class="nav-group"><div class="nav-section">Repository</div>',
    '        <a href="#/prs" data-nav="prs"><span class="nav-icon">⑂</span>Pull Requests</a>',
    '        <a href="#/code" data-nav="code"><span class="nav-icon">❯</span>Code<span id="nav-code-search" class="nav-search" role="button" tabindex="0" aria-label="Search files" title="Search files"></span></a>',
    '        <div id="nav-tree-slot" class="nav-tree-slot"></div>',
    '        <a href="#/branches" data-nav="branches"><span class="nav-icon">⎇</span>Branches</a>',
    '        <a href="#/graph" data-nav="graph"><span class="nav-icon">⑃</span>Graph</a>',
    '        <a href="#/tags" data-nav="tags"><span class="nav-icon">⌗</span>Tags</a></div>',
    '      <a href="#/releases" data-nav="releases"><span class="nav-icon">⏏</span>Releases</a>',
    '      <a href="#/memos" data-nav="memos" id="nav-memos"><span class="nav-icon">☞</span>Memos</a>',
    '      <a href="#/analytics" data-nav="analytics"><span class="nav-icon">◧</span>Analytics</a>',
    '      <a href="#/config" data-nav="config"><span class="nav-icon">⚙</span>Configuration</a>',
    '    </nav>',
    '    <div class="nav-footer"><a class="foot-brand" href="https://gitsocial.org"><svg class="logo-small" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg><span>Built with GitSocial</span></a></div>',
    '  </aside>',
    '  <main id="view" class="content"><div class="loading">Loading…</div></main>',
    '</div>',
  ].join("\n");

  // wireChrome re-attaches the small inline behaviors index.html carries inline:
  // the theme toggle, sidebar collapse, width toggle, and the mobile drawer.
  // Each is self-contained and defensive (a missing element is a no-op), so a
  // partial chrome never throws.
  function wireChrome() {
    var body = document.body;
    // Theme toggle.
    (function () {
      var toggleButton = document.getElementById("theme-toggle");
      var moonIcon = document.getElementById("moon-icon");
      var sunIcon = document.getElementById("sun-icon");
      if (!toggleButton) return;
      function systemDark() { return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches; }
      function current() { var s = null; try { s = localStorage.getItem("theme"); } catch (e) { /* private */ } return s ? s.replace("-mode", "") : (systemDark() ? "dark" : "light"); }
      function icon(t) { if (moonIcon && sunIcon) { moonIcon.style.display = t === "dark" ? "none" : "block"; sunIcon.style.display = t === "dark" ? "block" : "none"; } }
      function set(t) { body.classList.remove("light-mode", "dark-mode"); body.classList.add(t + "-mode"); try { localStorage.setItem("theme", t + "-mode"); } catch (e) { /* private */ } icon(t); }
      body.classList.add(current() + "-mode"); icon(current());
      toggleButton.addEventListener("click", function () { set(current() === "dark" ? "light" : "dark"); });
    })();
    // Sidebar collapse.
    (function () {
      var collapse = document.getElementById("nav-collapse");
      var handle = document.getElementById("nav-handle");
      function apply(c) { body.classList.toggle("nav-collapsed", c); try { localStorage.setItem("navCollapsed", c ? "1" : "0"); } catch (e) { /* private */ } }
      var saved = "0"; try { saved = localStorage.getItem("navCollapsed") || "0"; } catch (e) { /* private */ }
      body.classList.toggle("nav-collapsed", saved === "1");
      if (collapse) collapse.addEventListener("click", function () { apply(true); });
      if (handle) handle.addEventListener("click", function () { apply(false); });
    })();
    // Width toggle.
    (function () {
      var toggle = document.getElementById("width-toggle");
      var saved = "fixed"; try { saved = localStorage.getItem("layout") || "fixed"; } catch (e) { /* private */ }
      body.classList.toggle("wide", saved === "wide");
      if (toggle) toggle.addEventListener("click", function () { var wide = !body.classList.contains("wide"); body.classList.toggle("wide", wide); try { localStorage.setItem("layout", wide ? "wide" : "fixed"); } catch (e) { /* private */ } });
    })();
    // Mobile drawer.
    (function () {
      var burger = document.getElementById("nav-hamburger");
      var scrim = document.getElementById("nav-scrim");
      var nav = document.querySelector(".nav");
      function open(o) { body.classList.toggle("nav-open", o); if (burger) burger.setAttribute("aria-expanded", o ? "true" : "false"); }
      if (burger) burger.addEventListener("click", function () { open(!body.classList.contains("nav-open")); });
      if (scrim) scrim.addEventListener("click", function () { open(false); });
      window.addEventListener("hashchange", function () { open(false); });
      if (nav) nav.addEventListener("click", function (e) { if (e.target.closest && e.target.closest('a[href^="#"]')) open(false); });
    })();
  }

  // loadScript appends a same-origin (base-relative) script and resolves on load
  // / rejects on error, so the boot can await each asset in order.
  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.src = src;
      s.onload = resolve;
      s.onerror = function () { reject(new Error("load " + src)); };
      document.head.appendChild(s);
    });
  }

  // Route↔page-URL mapping. Routes with a real bucket page get a clean URL after
  // boot (pushState/replaceState so reloads hit the object); app-only surfaces
  // (search, board, analytics, code, compare, branches, graph, tags, lists,
  // config, milestones, sprints) keep hash routes.
  //
  // pageURLForHash maps a hash fragment (no leading #) to the absolute page URL
  // it corresponds to, or null when the route has no page. `base` is the site
  // root (window.__gsBase). The front page is the README home view, so only the
  // home route maps to index.html; /timeline is app-only (the mixed feed has no
  // page of its own — posts/index.html is a valid page FOR it, kept by syncURL's
  // current-page check, but never a rewrite target).
  function pageURLForHash(base, frag) {
    if (frag === "" || frag === "/") return base + "index.html";
    var m = /^commit:([0-9a-f]{7,40})@gitmsg\/(pm|review|social|release|memo)$/.exec(frag);
    if (m) return base + "i/" + m[1].slice(0, 12) + ".html";
    if (frag === "/issues") return base + "issues/index.html";
    if (frag === "/prs") return base + "prs/index.html";
    if (frag === "/releases") return base + "releases/index.html";
    if (frag === "/memos") return base + "memos/index.html";
    return null;
  }

  // overrideQuery returns the leading "?…" of the ?base=/?repo= cross-bucket
  // override params, verbatim and in order, from a search string (with or without
  // the leading "?"), or "" when neither is present. These select which bucket the
  // upgraded app reads; they must survive every URL rewrite (page URLs and the
  // normalized entry alike) or a reload/share would silently drop the override.
  function overrideQuery(search) {
    try {
      var params = new URLSearchParams(String(search || ""));
      var out = [];
      params.forEach(function (v, k) {
        if (k === "base" || k === "repo") out.push(k + "=" + encodeURIComponent(v));
      });
      return out.length ? "?" + out.join("&") : "";
    } catch (e) { return ""; }
  }

  // entryURLForHash builds the normalized entry URL for an app-only route (one
  // pageURLForHash returns null for): the bucket entry `index.html` carries the
  // view as a hash, so every app-only surface has ONE URL regardless of which page
  // the app booted from. `base` is the site root, so this is correct at any depth
  // (an i/ item page, a type-list dir, or the root). Any ?base=/?repo= override is
  // preserved verbatim so a reload of the normalized URL still reads the right
  // bucket. `frag` is the hash fragment without a leading "#".
  function entryURLForHash(base, frag, search) {
    return base + "index.html" + overrideQuery(search) + "#" + frag;
  }

  // hashForPath maps a served page URL (path) back to the hash fragment the app
  // routes on, for popstate (the reverse of pageURLForHash's non-item cases; an
  // item page's hash is not recoverable from the URL alone, so a popstate onto an
  // item page reloads it — a real bucket object, so that is correct and cheap).
  // The front page (index.html) is the README home view; the social posts list
  // (posts/index.html) routes to /timeline (the shell has no posts-only tab) —
  // its own URL is a valid page for that route.
  function hashForPath(base, href) {
    var rel = pathRel(base, href);
    if (rel === null) return null;
    if (rel === "" || rel === "index.html") return "#/";
    var m = /^(issues|prs|posts|releases|memos)\/index\.html$/.exec(rel);
    if (m) return m[1] === "posts" ? "#/timeline" : "#/" + m[1];
    if (/^i\/[0-9a-f]{12}\.html$/.test(rel)) return null; // reload handles it
    return null;
  }

  // pathRel returns a served URL's path relative to base (its stripped hash and
  // query removed), or null when it is not under base.
  function pathRel(base, href) {
    var noHash = String(href).split("#")[0].split("?")[0];
    if (noHash.indexOf(base) !== 0) return null;
    return noHash.slice(base.length);
  }

  // syncURL is called after every in-app hash render and reflects the location
  // into history. If the route maps to a page URL, reflect that clean URL so a
  // reload lands on the real bucket object (first render replaces the entry URL;
  // later navigations push a back-navigable history entry). If it has NO page (an
  // app-only surface), normalize the location to the bucket entry
  // `index.html#<route>` so the view never inherits the path of whatever page the
  // app booted from (an item page → `#/milestones` would otherwise become
  // `i/…​.html#/milestones`: a bogus URL that misleads unfurlers and multiplies
  // per-page). That normalization REPLACES: the hash change that reached the
  // route already created the history entry (with the wrong path), so we only
  // correct its URL in place — one entry per navigation, back returns to the page.
  var firstSync = true;
  // duringPop suppresses a pushState while a popstate-driven hash set repaints:
  // a back/forward that landed on a clean page URL sets location.hash to re-render
  // the app, which fires hashchange → syncURL; pushing a new history entry there
  // would defeat the very back the user just pressed, so syncURL only REPLACES
  // (cleans the residual hash) during a pop.
  var duringPop = false;
  function syncURL(base) {
    var frag = (window.location.hash || "").replace(/^#/, "");
    // Every rewrite below carries the ?base=/?repo= override verbatim: the
    // override enters via index.html and hash navigation keeps the query, so a
    // rewrite that dropped it would lose the cross-bucket selection for all
    // later navigation (and reload).
    var q = overrideQuery(window.location.search);
    // If the CURRENT page is itself a valid page for this route, keep it — do
    // not rewrite it to a canonical page or the entry URL. This is what keeps
    // the social posts archive (posts/index.html) on its own URL when the app
    // renders /timeline (an app-only route otherwise), and the front page on
    // index.html for home: the visitor landed on this page and a reload/copy
    // must stay there. Only the residual entry hash is stripped.
    var curRel = pathRel(base, window.location.href);
    if (curRel !== null && hashForPath(base, base + curRel) === "#" + frag) {
      var keepURL = base + curRel + q;
      if (curRel !== "" && (window.location.pathname || "").length && window.location.href !== keepURL) {
        try { history.replaceState({ gs: 1 }, "", keepURL); } catch (e) { /* history off */ }
      }
      firstSync = false;
      return;
    }
    var pageURL = pageURLForHash(base, frag);
    if (!pageURL) {
      // App-only route: normalize the current entry to the bucket entry
      // (index.html) carrying the hash, preserving any ?base=/?repo= override.
      // Always replaceState — the hash set that reached this route already pushed
      // the (path-leaking) entry, so we rewrite it in place rather than stacking a
      // second one. A no-op when the URL is already normalized (the plain static
      // shell, or a pop that restored an already-normalized entry).
      var entryURL = entryURLForHash(base, frag, window.location.search);
      if (window.location.href !== entryURL) {
        try { history.replaceState({ gs: 1 }, "", entryURL); } catch (e) { /* history unavailable — stay on the hash URL */ }
      }
      firstSync = false;
      return;
    }
    var target = pageURL + q;
    // The page URL carries no hash; keep any residual hash off the clean URL.
    if (window.location.href === target) { firstSync = false; return; }
    try {
      if (firstSync || duringPop) history.replaceState({ gs: 1 }, "", target);
      else history.pushState({ gs: 1 }, "", target);
    } catch (e) { /* history unavailable — stay on the hash URL */ }
    firstSync = false;
  }

  // wireNav intercepts popstate so a back/forward across pushState'd page URLs
  // re-renders the app: derive the hash from the URL and set it (the app's
  // hashchange handler repaints). A URL with no derivable hash (an item page)
  // falls back to a reload — it is a real bucket object, so the static page
  // serves and re-upgrades. The hash set fires hashchange → syncURL, which must
  // replace (not push) so the pop's history position is preserved.
  function wireNav(base) {
    window.addEventListener("popstate", function () {
      if ((window.location.hash || "") !== "") return; // hash already drives the app
      var h = hashForPath(base, window.location.href);
      if (!h) { window.location.reload(); return; }
      duringPop = true;
      window.location.hash = h;
    });
    // After every hash render, reflect the clean page URL. duringPop is cleared
    // HERE (right after syncURL reads it) rather than via a timer, so the flag's
    // lifetime is exactly the pop-driven hashchange — no dependence on task
    // ordering between a setTimeout and the hashchange dispatch.
    window.addEventListener("hashchange", function () { syncURL(base); duringPop = false; });
  }

  // boot upgrades the page: resolve base + route, inject chrome + app CSS, load
  // the shell assets, and let gs-app.js init() render. The route is placed on
  // location.hash BEFORE gs-app.js loads so its auto-init picks it up; the base
  // is published as window.__gsBase so deriveBase anchors at the site root.
  async function boot() {
    var base = resolveBase();
    var route = routeFor();
    window.__gsBase = base;
    // Load the reader BEFORE touching the page: gs-core and gs-render are the
    // DOM-free/render layers and neither auto-boots. If either 404s (a broken or
    // partial upgrade) we throw here — the static, readable page is still fully
    // intact because the body has not been replaced yet. The optional enhancers
    // (icons/prism) are best-effort.
    try { await loadScript(base + "icons.js"); } catch (e) { /* icons optional */ }
    try { await loadScript(base + "prism.js"); } catch (e) { /* prism optional */ }
    await loadScript(base + "gs-core.js");
    await loadScript(base + "gs-render.js");
    // The reader is loaded: take over. Inject the full app CSS + chrome, seed the
    // route on the hash, then load gs-app.js LAST — its init() auto-runs into the
    // now-present chrome. gs-app.js is the smallest, last, most-reliable asset, so
    // the window where a load failure could leave a blanked page is minimal.
    // Drop the static page's own styling layer (the inline base <style> and its
    // pages.css link) at takeover: it is scoped to the pre-upgrade document and
    // constrains <body> (max-width/margin/padding), which pages-app.css never
    // resets — leaving it squeezes the injected app to the reading column and
    // turns the width toggle into a visual no-op.
    try {
      document.querySelectorAll('head style, head link[rel="stylesheet"]').forEach(function (n) { n.remove(); });
    } catch (e) { /* shimmed DOM — nothing to remove */ }
    var css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = base + "pages-app.css";
    document.head.appendChild(css);
    document.body.innerHTML = CHROME;
    wireChrome();
    // Seed the route: a bare route (no "#") becomes the hash the app reads. A
    // deep-link hash already present wins (routeFor) and is left untouched.
    if (route && ("#" + route) !== window.location.hash) {
      try { history.replaceState(null, "", "#" + route); } catch (e) { window.location.hash = route; }
    }
    await loadScript(base + "gs-app.js");
    // Wire the URL reflection AFTER gs-app: hashchange listeners fire in
    // registration order, and the app's handler must read location.hash before
    // syncURL rewrites it into a clean (hashless) page URL. Registered first,
    // syncURL would strip the hash out from under the app and every paged
    // navigation would render home.
    wireNav(base);
    // gs-app.js's init() ran on load and rendered the route; reflect the clean
    // page URL now (the entry replaceState).
    syncURL(base);
  }

  // Watchdog: if boot never resolves (a wedged asset load), the page has already
  // stayed readable — this only logs. The static content is never removed until
  // document.body.innerHTML replaces it inside boot(), and boot() is wrapped so a
  // throw before that point leaves the page intact; a throw AFTER it still leaves
  // the injected chrome + the app's own error surface.
  function run() {
    boot().catch(function (err) {
      try { if (console && console.error) console.error("gitsocial: page upgrade failed:", err && err.message); } catch (e) { /* no console */ }
    });
  }

  // Node-importable pure helpers (route/page-URL mapping) for the sitetest
  // upgrade-boot suite. Under CommonJS (module.exports present) the file is a
  // pure library: it exports the helpers and NEVER auto-boots, so importing it
  // in a test's shimmed DOM does not try to take over a page.
  if (typeof module !== "undefined" && module.exports) {
    // syncURL/wireNav operate on the global window/history, so a test can drive
    // the real navigation logic by injecting a fake window+history and resetting
    // the boot flags via _resetSync — no browser or duplicated push/replace rules.
    module.exports = {
      pageURLForHash: pageURLForHash, hashForPath: hashForPath,
      entryURLForHash: entryURLForHash, overrideQuery: overrideQuery,
      syncURL: syncURL, wireNav: wireNav,
      _resetSync: function () { firstSync = true; duringPop = false; },
    };
    return;
  }
  // Browser only: on page load, upgrade in place.
  if (typeof document === "undefined" || typeof window === "undefined") return;
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", run);
  else run();
})();
