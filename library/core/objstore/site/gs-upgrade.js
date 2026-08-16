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
// A JS visitor is never shown content that is about to change. The page's own
// inline head script (site_pages_html.go) puts `.gs-boot` on <html> before the
// body is parsed, which hides the static content and paints the loading line, so
// the boot begins on a loading state rather than on a page the visitor starts
// reading and loses two seconds later.
//
// The takeover then reveals in TWO steps, because the two halves of the app
// become available at very different times. The chrome (nav + layout) is a
// constant in this file and pages-full.css lands in the parallel batch (the
// shared base, pages-core.css, is already live in the served page's own
// head), so the
// site's own frame can be on screen long before any data has been read; the
// first view has to wait for the bucket. So revealChrome puts the frame up as
// soon as the stylesheet governs the page, with the content slot holding a
// loading treatment, and revealApp fills that slot once the first view has
// settled. The visitor sees the site for most of the wait instead of a blank
// page, and because the chrome is laid out from the first of those steps,
// nothing moves when the content lands. Every entry works this way — a page
// booting its own route and a deep link to somewhere else alike — so the real
// content still appears once, final, and nothing changes under the reader.
//
// Bootstrapping on first visit is the accepted cost. The alternative, holding
// the static page until the app is ready, meant the body was replaced under
// someone mid-sentence, in different typography, and on the front page the held
// content was partly wrong besides (raw markdown where the app renders it).
//
// Failure is still inert, but now by RESTORING rather than by doing nothing:
// every step of the takeover is reversible, and every path that cannot finish it
// — a 404 or a hang on any shell asset, a throw anywhere in boot(), a route that
// never settles — puts the complete, styled static page back on screen. Since
// the chrome can now be up before the boot is safe, restoring means taking the
// app's frame back off the page too, not just un-hiding the content: a dead nav
// whose links are app routes that will never resolve, sitting over the served
// document, is its own kind of stranding. A page that reads without JS must keep
// reading when the upgrade fails, and the one thing this layer must never leave
// behind is a blank page, or a frame, with nothing coming. The `settled` latch
// below is what makes that safe in both directions.

(function () {
  // BOOT_CLASS is the served page's boot marker, added to <html> by the page's
  // inline head script and carrying the rules that hide the static content and
  // paint the loading line. This layer only ever REMOVES it: the hide has to be
  // in effect before the body is parsed, which is earlier than a deferred script
  // can run, so the page owns putting it on and the boot owns taking it off.
  var BOOT_CLASS = "gs-boot";

  // LOADING_CLASS is the app's half of the same idea, and it is what makes the
  // early chrome safe. Added to <html> when the chrome takes the screen and
  // removed when the first view has settled, it is the hook pages-full.css hangs
  // the content slot's loading treatment off: while it is set, #view paints one
  // loading line and hides whatever the app has rendered into it. gs-app fills
  // #view in pieces (a placeholder, then the view, then its highlights), and
  // this is what keeps the visitor from watching that happen — the slot goes
  // from loading to the finished view exactly once.
  var LOADING_CLASS = "gs-loading";

  // settled is the boot's one-shot latch. Exactly one of revealApp (the app is
  // finished and takes the screen) and restoreStatic (the boot gave up and the
  // served page comes back) may act, and the other becomes a no-op forever after.
  // Without it a watchdog restore could be swapped away again by a very late
  // first view, or a failure after the reveal could resurrect the static content
  // on top of a working app — both are the page changing under the reader, which
  // is the thing this file exists to prevent.
  //
  // revealChrome deliberately does NOT take it: the chrome going up is not the
  // boot finishing, and everything after it can still fail.
  var settled = false;

  // undoChrome is set by revealChrome to the exact reversal of that step, and
  // cleared once revealApp makes the takeover final. While it is set the app's
  // frame is on screen, which is what raises the stakes for every failure path
  // below: a restore that only un-hid the static content would leave a dead nav
  // above it, whose links are app routes that will never resolve. So restoring
  // goes through here first, and the chrome comes off the page with it.
  var undoChrome = null;

  // entryHref is the URL the page was entered with, captured before the boot
  // rewrites it (first the route seeding, then syncURL's clean page URLs). A
  // restore hands back the served document, so it owes the visitor that
  // document's address too: leaving the bar on a page the app never rendered
  // means a copy or a reload lands somewhere else entirely.
  var entryHref = null;

  // restoreStatic undoes whatever the boot has done so far, putting the complete,
  // styled static content back on screen and taking every trace of the app away.
  // This is where every failure path ends: the served document is readable on its
  // own by construction, so a boot that cannot finish owes the visitor exactly
  // that document rather than a spinner, or a nav, with nothing behind it.
  function restoreStatic() {
    if (settled) return;
    settled = true;
    if (entryHref !== null) {
      try { if (window.location.href !== entryHref) history.replaceState(null, "", entryHref); } catch (err) { /* history off — the page still reads */ }
    }
    if (undoChrome) {
      var undo = undoChrome;
      undoChrome = null;
      try { undo(); } catch (err) { /* shimmed DOM — the class removal below still un-hides */ }
    }
    try {
      var html = document.documentElement;
      html.classList.remove(BOOT_CLASS);
      html.classList.remove(LOADING_CLASS);
    } catch (err) { /* no classList — the page's own failsafe still restores */ }
  }

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

  // parsedRoute runs a fragment through the reader's route grammar (gs-core's
  // parseRoute, the authority on what is and is not a route), or returns null
  // before that script has loaded. That is why boot resolves its entry after
  // gs-core.js rather than at the top: the URL cannot be judged without it.
  function parsedRoute(frag) {
    var ns = (typeof GS !== "undefined" && GS) || null;
    return (ns && typeof ns.parseRoute === "function") ? (ns.parseRoute(frag) || null) : null;
  }

  // entryFor answers the one question the boot asks about the URL: WHICH route
  // boots. It also reports whether that route is the page's own (ownPage), which
  // no longer changes the reveal — every entry now shows the loading state and
  // then the finished app — but is still the honest reading of the URL and is
  // what the route rules below are stated in terms of.
  //
  // A location.hash deep-link WINS over the page's gs-route meta (a shared/legacy
  // #/… link, or a code-commit link from the timeline, must open its target on any
  // page it lands on), but only when the fragment NAMES a route. A fragment
  // outside the grammar ("notfound"), or a bare in-page anchor (which parseRoute
  // reports as the home view plus an anchor), addresses a place on the page you
  // already have rather than a page to go to: a "#c-<sha>" row on a commits list,
  // a "#reply-…" on an item page, a README heading. Booting those would route the
  // app to garbage, so the page's own route boots and the fragment is left as the
  // ordinary anchor the browser already scrolled to. The exception is a page whose
  // own route IS the home view, whose README those headings live in: there the
  // anchor and the page agree, so the fragment rides along and the app scrolls to
  // the heading after it renders.
  //
  // ownPage is false ONLY for a genuine deep link to somewhere else.
  // Route strings are returned WITHOUT the leading "#" (parseRoute strips it).
  function entryFor() {
    var meta = metaRoute();
    var frag = (window.location.hash || "").replace(/^#/, "");
    if (frag === "") return { route: meta, ownPage: true };
    var r = parsedRoute(frag);
    if (!r) return { route: frag, ownPage: frag === meta };
    if (r.type === "notfound") return { route: meta, ownPage: true };
    // Only parseRoute's bare-fragment rule yields home+anchor; a "file:…:slug"
    // route carries an anchor too and is a real destination, so match on both.
    if (r.type === "home" && r.anchor) {
      var m = parsedRoute(meta);
      if (m && m.type === "home") return { route: frag, ownPage: true };
      // The page's own route may take the anchor as a first-class suffix (a
      // commits list, whose rows ARE addressable). Ask the grammar rather than
      // hardcoding which routes those are: if meta + ":" + fragment parses to a
      // real route carrying exactly this anchor, boot that — which is what keeps
      // a shared `commits/7.html#c-<sha>` pointing at the row after the upgrade
      // instead of being flattened to the page.
      var combined = meta + ":" + frag;
      var c = parsedRoute(combined);
      if (c && c.type !== "notfound" && c.anchor === r.anchor) return { route: combined, ownPage: true };
      return { route: meta, ownPage: true };
    }
    return { route: frag, ownPage: frag === meta };
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
    '        <a href="#/commits" data-nav="commits"><span class="nav-icon">≡</span>Commits</a>',
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

  // stageChrome appends the app chrome to the body with every top-level node
  // hidden, NEXT TO the (already hidden) static content rather than replacing it:
  // gs-app needs #view and the nav slots in the document to render into, and the
  // static content has to stay in the DOM because a failed boot puts it back.
  // Returns the hidden element nodes so reveal can unhide them in one step.
  // Nothing visible changes here — the app stylesheet is still inert and the
  // staged chrome has no layout of its own, so the loading line is undisturbed.
  function stageChrome() {
    var holder = document.createElement("div");
    holder.innerHTML = CHROME;
    var nodes = [];
    while (holder.firstChild) {
      var n = holder.firstChild;
      holder.removeChild(n);
      if (n.nodeType === 1) { n.style.display = "none"; nodes.push(n); }
      document.body.appendChild(n);
    }
    return nodes;
  }

  // BOOT_MAX_MS bounds the SHELL-ASSET phase: from the first asset request to
  // gs-app.js having executed. Expiring means restoreStatic — the visitor gets
  // the served page back. It is armed at the top of upgrade() because a request
  // that never resolves and never rejects is otherwise a loading line with
  // nothing behind it, forever.
  //
  // Waiting is now the bad state (a loading line, not a readable page), so this
  // is much tighter than the old hold's 15s, and it matches the failsafe the page
  // itself arms for the case this script never runs at all — one number for "the
  // upgrade has had long enough".
  var BOOT_MAX_MS = 10000;

  // APP_MAX_MS takes the deadline over once gs-app.js has executed. From there
  // the wait is the app hydrating its first window, which a large real repo can
  // spend several seconds on, and tearing down an app that is MAKING PROGRESS is
  // the one restore that costs the visitor something: the finished view would
  // render into a permanently hidden #view and a reload would repeat it. So this
  // outlasts gs-app's own 30s route watchdog, whose error surface is itself held
  // behind the loading treatment until the route exits — a route wedged past
  // both still ends in the served page coming back.
  var APP_MAX_MS = 35000;

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
  // / rejects on error, so the boot can await each asset.
  //
  // async=false is the load order this boot depends on. A dynamically injected
  // script defaults to async=true (download and RUN whenever it lands), which for
  // gs-core → gs-render would run the render layer against a missing namespace.
  // Setting it false makes the browser download every injected script in
  // PARALLEL and execute them in insertion order, so the dependency chain costs
  // one round trip instead of one per file — no build step, no bundle.
  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.async = false;
      s.src = src;
      s.onload = resolve;
      s.onerror = function () { reject(new Error("load " + src)); };
      document.head.appendChild(s);
    });
  }

  // preloadScript warms a script into the browser's cache WITHOUT running it.
  // gs-app.js cannot simply join the ordered batch above: it auto-runs init() on
  // execution, which needs the chrome staged and the route seeded first. So it is
  // fetched here alongside the rest and the <script> that actually runs it, added
  // once the page is ready for it, is served from this preload rather than from a
  // second round trip. Best-effort by construction: an ignored/unsupported
  // rel=preload simply leaves the later load to fetch it as before.
  function preloadScript(src) {
    try {
      var link = document.createElement("link");
      link.rel = "preload";
      link.as = "script";
      link.href = src;
      document.head.appendChild(link);
    } catch (e) { /* no preload support — the ordinary load below still works */ }
  }

  // loadStylesheet appends a base-relative stylesheet <link> and resolves with it
  // once it is loaded (onload), so the boot can gate a style-destructive step on
  // the new sheet being ready. `media` scopes it: the boot passes "not all" so the
  // sheet is fetched and parsed but governs nothing until reveal flips it to
  // "all" — live from the start it would restyle the static page (its body font
  // and border-box reset reflow the reading column) underneath the very content a
  // failed boot has to hand back intact. Rejects on error or after a watchdog
  // timeout (onload/onerror wedged), so a failed CSS fetch aborts the takeover
  // before anything is staged and the restore returns a readable, styled page —
  // never a blanked or half-styled one.
  function loadStylesheet(href, media) {
    return new Promise(function (resolve, reject) {
      var link = document.createElement("link");
      link.rel = "stylesheet";
      if (media) link.media = media;
      link.href = href;
      var settled = false;
      var timer = setTimeout(function () { if (settled) return; settled = true; reject(new Error("timeout " + href)); }, 10000);
      link.onload = function () { if (settled) return; settled = true; clearTimeout(timer); resolve(link); };
      link.onerror = function () { if (settled) return; settled = true; clearTimeout(timer); reject(new Error("load " + href)); };
      document.head.appendChild(link);
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
    var c = COMMITS_ROUTE.exec(frag);
    if (c) return base + "commits/" + (c[1] || "index") + ".html";
    return null;
  }

  // COMMITS_ROUTE mirrors gs-core's /commits[/<n>][:<anchor>] grammar. The
  // commits list is the one route family whose ROWS are addressable (`c-<sha12>`
  // on a real page), so its route carries the anchor as a first-class suffix and
  // the page-URL mapping has to split the two: the page is the object, the anchor
  // is the place in it.
  var COMMITS_ROUTE = /^\/commits(?:\/(\d+))?(?::([A-Za-z0-9][\w.-]*))?$/;

  // routeAnchor returns a route fragment's trailing "#<anchor>" (the part that
  // addresses a place on the page rather than the page), or "" when it carries
  // none. routeWithoutAnchor is its complement.
  function routeAnchor(frag) {
    var m = COMMITS_ROUTE.exec(frag || "");
    return (m && m[2]) ? "#" + m[2] : "";
  }
  function routeWithoutAnchor(frag) {
    var a = routeAnchor(frag);
    return a ? String(frag).slice(0, String(frag).length - a.length) : String(frag || "");
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
    var c = /^commits\/(index|\d+)\.html$/.exec(rel);
    if (c) return c[1] === "index" ? "#/commits" : "#/commits/" + c[1];
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
    // A route may carry an anchor suffix (#/commits/7:c-<sha>), which addresses a
    // ROW rather than a page: it survives every rewrite below as an ordinary
    // fragment on the page URL, which is exactly the citable URL the generated
    // page hands out — so entering one and reading it back give the same address.
    var anchor = routeAnchor(frag);
    var route = routeWithoutAnchor(frag);
    // If the CURRENT page is itself a valid page for this route, keep it — do
    // not rewrite it to a canonical page or the entry URL. This is what keeps
    // the social posts archive (posts/index.html) on its own URL when the app
    // renders /timeline (an app-only route otherwise), and the front page on
    // index.html for home: the visitor landed on this page and a reload/copy
    // must stay there. Only the residual entry hash is stripped.
    var curRel = pathRel(base, window.location.href);
    if (curRel !== null && hashForPath(base, base + curRel) === "#" + route) {
      var keepURL = base + curRel + q + anchor;
      if (curRel !== "" && (window.location.pathname || "").length && window.location.href !== keepURL) {
        try { history.replaceState({ gs: 1 }, "", keepURL); } catch (e) { /* history off */ }
      }
      firstSync = false;
      return;
    }
    var pageURL = pageURLForHash(base, route);
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
    var target = pageURL + q + anchor;
    // The page URL carries no hash of its own; only a row anchor rides along.
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
      var frag = (window.location.hash || "").replace(/^#/, "");
      var h = hashForPath(base, window.location.href);
      // A row anchor on a page URL (commits/7.html#c-<sha>) is NOT a route: left
      // alone the app would keep whatever it last painted while the URL claimed a
      // commits page. Re-drive that page's own route, carrying the anchor as the
      // grammar's suffix so the app scrolls back to the row.
      var anchored = frag !== "" && h && /^[A-Za-z0-9][\w.-]*$/.test(frag) && COMMITS_ROUTE.test(h.slice(1) + ":" + frag);
      if (frag !== "" && !anchored) return; // hash already drives the app
      if (!h) { window.location.reload(); return; }
      duringPop = true;
      window.location.hash = anchored ? h + ":" + frag : h;
    });
    // After every hash render, reflect the clean page URL. duringPop is cleared
    // HERE (right after syncURL reads it) rather than via a timer, so the flag's
    // lifetime is exactly the pop-driven hashchange — no dependence on task
    // ordering between a setTimeout and the hashchange dispatch.
    window.addEventListener("hashchange", function () { syncURL(base); duringPop = false; });
  }

  // boot is the takeover plus its failure boundary. Every way upgrade() can fail
  // — a 404 or a wedged fetch on any shell asset, a bad base, a throw while the
  // chrome is staged — ends with the served page back on screen, and the error is
  // re-thrown so the caller can still say what went wrong. A throw AFTER the
  // reveal finds the latch taken, so a working app is never pulled back out from
  // under the visitor.
  async function boot() {
    try {
      await upgrade();
    } catch (err) {
      restoreStatic();
      throw err;
    }
  }

  // upgrade does the work: resolve base + route, inject chrome + app CSS, load
  // the shell assets, and let gs-app.js init() render. The route is placed on
  // location.hash BEFORE gs-app.js loads so its auto-init picks it up; the base
  // is published as window.__gsBase so deriveBase anchors at the site root.
  async function upgrade() {
    var base = resolveBase();
    window.__gsBase = base;
    try { entryHref = window.location.href; } catch (e) { entryHref = null; }
    // Arm the give-up watchdog before the first request. From here on the visitor
    // is looking at a loading line, so every way this can fail to finish — an
    // asset that never resolves and never rejects, a route that never settles —
    // has to end in the served page coming back, and only a timer catches the
    // ones that produce no event at all.
    var giveUp = (typeof setTimeout === "function") ? setTimeout(restoreStatic, BOOT_MAX_MS) : null;
    // Request EVERY shell asset now, in one batch. Nothing here depends on
    // another's bytes having arrived — only gs-core → gs-render → gs-app is a
    // real dependency, and only for EXECUTION order, which async=false preserves.
    // Loaded one await at a time this was six serial round trips on every single
    // page entry, home and deep link alike, and none of it scales with the repo:
    // it was pure fixed cost paid by every visitor.
    //
    // The reader (gs-core + gs-render) is what the takeover needs: both are
    // DOM-free/render layers and neither auto-boots, so if either 404s (a broken
    // or partial upgrade) the await below throws and boot()'s catch hands back
    // the served page, which nothing has touched yet. icons.js is an optional
    // enhancer, so its failure is swallowed rather than aborting the upgrade.
    // prism.js is deliberately NOT
    // here: gs-render fetches it on the first render that actually highlights
    // something, so a page with no code on it never pays for the tokenizer.
    var shellLoad = Promise.all([
      loadScript(base + "icons.js").catch(function (e) { /* icons optional */ }),
      loadScript(base + "gs-core.js"),
      loadScript(base + "gs-render.js"),
    ]);
    // The app stylesheet (pages-full.css — the same sheet the page already
    // links, so this resolves from cache) rides the same batch. It is still
    // awaited (and still fetched inert) at the point below where the takeover is
    // prepared — only its DOWNLOAD moves up here; its gating role at reveal is
    // unchanged. A stylesheet failure can land while the scripts are still in
    // flight, before there is an await on it; the no-op catch keeps that from
    // surfacing as an unhandled rejection. The rejection itself is still
    // delivered, below.
    var cssLoad = loadStylesheet(base + "pages-full.css", "not all");
    cssLoad.catch(function (e) { /* delivered at the await below */ });
    // The shared base (pages-core.css) is normally already active: the page
    // inlines it as its <style data-gs-core> head element, which the takeover
    // leaves in place — identical bytes govern before and after the swap. A page
    // WITHOUT that marker predates the core/full split (a long-cached sealed
    // list page booting a newer shell), and pages-full.css alone would leave
    // every var() dangling, so only then is the core sheet fetched alongside and
    // flipped live with the rest at reveal.
    var coreLoad = null;
    try {
      if (!document.querySelector("style[data-gs-core]")) coreLoad = loadStylesheet(base + "pages-core.css", "not all");
    } catch (e) { /* shimmed DOM — the inlined core is the normal case */ }
    if (coreLoad) coreLoad.catch(function (e) { /* delivered at the await below */ });
    preloadScript(base + "gs-app.js");
    await shellLoad;
    // Resolve the entry now and not earlier: entryFor asks gs-core's parseRoute
    // whether the URL's fragment is a route at all, and that grammar only exists
    // once the line above has run. Nothing has touched the location or the page
    // in the meantime, so the answer is the same one the top of upgrade would
    // have given, minus the guessing.
    var route = entryFor().route;
    // The reader is loaded: prepare the takeover WITHOUT changing anything on
    // screen. The app stylesheet was fetched inert (media "not all") — it must be
    // ready before the swap, and the sheet going live restyles whatever the
    // static layer still governs, which is a reflow of the very content a failed
    // boot has to hand back intact. If it fails or wedges, loadStylesheet rejects
    // and boot throws HERE — before anything is staged — and the restore puts the
    // readable, styled static page back untouched.
    var appCSS = await cssLoad;
    var coreCSS = coreLoad ? await coreLoad : null;
    // What the reveal will suspend: the static page's styling layer (its
    // pages-full.css link, with the media it was served with) and its content.
    // Captured NOW, before gs-app.js runs, so the takeover touches exactly these
    // and never a node the app added meanwhile (its accent <style>, a lazily
    // loaded grammar, …). The inlined shared base (and the page's accent
    // override) carry data-gs-core and are deliberately EXEMPT: they are the
    // tokens and body base the app's own sheet consumes, identical to what the
    // shell links as pages-core.css, so they stay live across the swap.
    var staticStyles = [];
    try {
      document.querySelectorAll('head style, head link[rel="stylesheet"]').forEach(function (n) {
        if (n !== appCSS && n !== coreCSS && !n.hasAttribute("data-gs-core")) staticStyles.push({ node: n, media: n.media || "" });
      });
    } catch (e) { /* shimmed DOM — nothing to suspend */ }
    var staticBody = [];
    var bodyKids = [].slice.call(document.body.childNodes);
    for (var s = 0; s < bodyKids.length; s++) if (bodyKids[s].nodeType === 1) staticBody.push(bodyKids[s]);
    var chromeNodes = stageChrome();
    wireChrome();
    // cloaked is read before anything can clear the class, and it is the whole
    // signal for which reveal shape this entry gets: a cloaked page is showing a
    // loading line and wants the chrome as soon as it can have it, an uncloaked
    // one is showing the finished page and wants nothing to move until the app
    // is ready.
    var cloaked = false;
    try { cloaked = document.documentElement.classList.contains(BOOT_CLASS); } catch (e) { /* shimmed DOM */ }
    // revealChrome is the first of the entry's two visual steps: the app's own
    // frame takes the screen while the first view is still loading. Everything it
    // needs is in hand — the chrome is a constant in this file and pages-full.css
    // has arrived — so the visitor gets the site's nav and layout for most of the
    // wait instead of a blank page with one line on it, and gets it at a size and
    // position the content will not disturb when it lands.
    //
    // It is gated on the stylesheet on purpose. Chrome painted before
    // pages-full.css governs the page is an unstyled nav, which is a worse flash
    // than the blank page it replaces, so this runs only after the awaited
    // cssLoad above — the same gate the reveal has always used.
    //
    // Everything here is SUSPENDED rather than dropped. The boot can still fail
    // from this point (gs-app.js is not loaded yet, and its route may never
    // settle), and each of those paths owes the visitor the served page back, so
    // the static layer is made inert by media and hidden by display — both
    // exactly reversible — and undoChrome is the reversal.
    function revealChrome() {
      if (undoChrome) return;
      // The page's boot state ends here and the app's begins: BOOT_CLASS hides
      // nothing once its stylesheet goes inert on the next line, and
      // LOADING_CLASS is what holds the content slot on its loading treatment.
      try {
        var html = document.documentElement;
        html.classList.remove(BOOT_CLASS);
        html.classList.add(LOADING_CLASS);
      } catch (e) { /* shimmed DOM */ }
      appCSS.media = "all";
      if (coreCSS) coreCSS.media = "all";
      for (var i = 0; i < staticStyles.length; i++) staticStyles[i].node.media = "not all";
      for (var j = 0; j < staticBody.length; j++) staticBody[j].style.display = "none";
      for (var k = 0; k < chromeNodes.length; k++) chromeNodes[k].style.display = "";
      undoChrome = function () {
        appCSS.media = "not all";
        if (coreCSS) coreCSS.media = "not all";
        for (var i2 = 0; i2 < staticStyles.length; i2++) staticStyles[i2].node.media = staticStyles[i2].media;
        for (var j2 = 0; j2 < staticBody.length; j2++) staticBody[j2].style.display = "";
        // Everything the takeover put in the body goes back off screen, and not
        // only the nodes stageChrome added: gs-app.js may be running by now and
        // may have appended one of its own (the refresh pill), and a restored
        // static page owes the visitor no leftovers from an app that is not
        // there. They are HIDDEN rather than removed so that a gs-app whose
        // route settles after the boot gave up still finds a #view to render
        // into — off screen, behind the latch, harming nothing — instead of
        // throwing on a missing mount.
        var kids = [].slice.call(document.body.childNodes);
        for (var m = 0; m < kids.length; m++) {
          if (kids[m].nodeType === 1 && staticBody.indexOf(kids[m]) < 0) kids[m].style.display = "none";
        }
      };
    }
    // revealApp is the second step and the one the visitor reads as the page
    // arriving: the content slot drops its loading treatment and the finished
    // first view appears in it. The chrome does not move — it has been on screen
    // and laid out since revealChrome — so this changes the content column and
    // nothing else. It takes the `settled` latch, which is what stops the give-up
    // watchdog from restoring the static page over an app that has already
    // arrived, and it is where the static layer stops being suspended and is
    // dropped for good.
    function revealApp() {
      if (settled) return;
      settled = true;
      revealChrome();
      undoChrome = null;
      try { document.documentElement.classList.remove(LOADING_CLASS); } catch (e) { /* shimmed DOM */ }
      for (var i = 0; i < staticStyles.length; i++) staticStyles[i].node.remove();
      for (var j = 0; j < staticBody.length; j++) {
        if (staticBody[j].parentNode === document.body) document.body.removeChild(staticBody[j]);
      }
    }
    // The chrome goes up now, before gs-app.js is even fetched: it is the whole
    // point of splitting the reveal, and it costs nothing to wait for.
    //
    // UNLESS the page was never cloaked. A page the boot script left visible (the
    // front page entered without a deep link) is already showing the finished
    // content this route renders, so putting the chrome up early would replace a
    // readable page with a nav over a loading line: the exact trade the split
    // reveal exists to avoid, run backwards. There the two steps collapse into
    // one and revealApp does both, so the visitor reads the served page until the
    // app is ready and then sees it swapped once.
    if (cloaked) revealChrome();
    // The handshake gs-app.js honors: it calls this once its first route has
    // settled, INCLUDING a view's deferred section (home's recent activity), so
    // the app's content is complete at the instant it becomes visible.
    window.__gsOnFirstView = revealApp;
    // Seed the route: location.hash is the only channel gs-app's init reads, so
    // the chosen route has to go there. A deep-link hash already present wins
    // (entryFor) and is therefore already equal, so this is a no-op for it, and so
    // it is for an anchor entryFor let ride along. It DOES overwrite a fragment
    // entryFor rejected (an anchor on a page that is not home, an off-grammar
    // fragment): the page's own route has to reach the app, and there is no second
    // channel to carry the anchor. The end state is unchanged either way, because
    // the app has no anchor for content outside the home README and replaces that
    // content wholesale; what a JS visitor no longer gets is the transient scroll
    // to the static row, since the static content is not on screen to scroll.
    if (route && ("#" + route) !== window.location.hash) {
      try { history.replaceState(null, "", "#" + route); } catch (e) { window.location.hash = route; }
    }
    // gs-app.js is loaded LAST and awaited: it auto-runs init(), so the chrome has
    // to be staged and the route seeded first, and a 404 here rejects with nothing
    // revealed, so boot()'s catch hands the visitor back the served page. Every
    // entry now falls through to the __gsOnFirstView handshake — a page booting
    // its own route and a deep link alike — so the app appears once, finished.
    await loadScript(base + "gs-app.js");
    // The shell-asset phase is over: hand the deadline to the app's own budget so
    // a first route that is slow but progressing is not discarded (see APP_MAX_MS).
    if (giveUp !== null) { try { clearTimeout(giveUp); } catch (e) { /* no clearTimeout */ } }
    if (typeof setTimeout === "function") setTimeout(restoreStatic, APP_MAX_MS);
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

  // run starts the boot and reports a failure to the console. boot() has already
  // restored the served page by the time this catch sees the error, so there is
  // nothing left to repair here.
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
    // boot itself is exported too: the reveal contract (WHEN the static content
    // is dropped, and that a failed asset load never drops it) is ordering, not a
    // pure function, so the suite drives the real boot against a fake document
    // whose loaders resolve from a fixture map rather than re-stating the rules.
    module.exports = {
      pageURLForHash: pageURLForHash, hashForPath: hashForPath,
      entryURLForHash: entryURLForHash, overrideQuery: overrideQuery,
      routeAnchor: routeAnchor, entryFor: entryFor,
      syncURL: syncURL, wireNav: wireNav, boot: boot,
      // _resetSync returns the module to its just-loaded state so a suite can
      // drive one boot after another; `settled` and `undoChrome` are both
      // one-shot by design, so without clearing them every boot after the first
      // would decline to act — and a stale undoChrome would additionally make the
      // next boot's chrome reveal a no-op while pointing at the previous page.
      _resetSync: function () { firstSync = true; duringPop = false; settled = false; undoChrome = null; entryHref = null; },
      // _setBootMaxMs shortens BOTH give-up watchdogs — the shell-asset phase and
      // the app phase it hands over to — so a suite can drive a route that never
      // settles without waiting it out in real time. Node-only: it exists nowhere
      // in the browser form of this file.
      _setBootMaxMs: function (ms) { BOOT_MAX_MS = ms; APP_MAX_MS = ms; },
    };
    return;
  }
  // Browser only: on page load, upgrade in place.
  if (typeof document === "undefined" || typeof window === "undefined") return;
  // Take ownership of the hide the page's inline script put in place. That script
  // stands its own failsafe down when it sees this flag, because from here the
  // boot's watchdog and run()'s catch own restoring the page — and they know when
  // the takeover actually finished, which a blind timer does not. A gs-upgrade.js
  // that 404s or fails to parse never gets here, so the page's failsafe stays
  // armed for exactly the case nothing else can cover.
  window.__gsBooting = true;
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", run);
  else run();
})();
