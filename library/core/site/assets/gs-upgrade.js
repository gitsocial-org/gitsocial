// gs-upgrade.js - page entry: upgrades a generated static page into the app in place, and restores the page when the boot cannot finish

(function () {
  // BOOT_CLASS is the served page's boot marker, removed here; LOADING_CLASS then holds the app's content slot on its loading treatment.
  var BOOT_CLASS = "gs-boot";

  var LOADING_CLASS = "gs-loading";

  // settled is the boot's one-shot latch: one of revealApp and restoreStatic acts, and the other becomes a no-op.
  var settled = false;

  // undoChrome reverses revealChrome, so a restore takes the app's frame off the page along with the content swap.
  var undoChrome = null;

  var entryHref = null;

  // restoreStatic puts the served page and its entry URL back on screen, and drops every trace of the app.
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

  // resolveBase returns the absolute artifact base: the ?base=/?repo= override, else the mount's data-base resolved against the page URL.
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

  // parsedRoute runs a fragment through gs-core's parseRoute, or returns null before that script has loaded.
  function parsedRoute(frag) {
    var ns = (typeof GS !== "undefined" && GS) || null;
    return (ns && typeof ns.parseRoute === "function") ? (ns.parseRoute(frag) || null) : null;
  }

  // entryFor picks the route to boot: a location.hash deep link wins when the fragment names a route, else the page's own gs-route meta.
  function entryFor() {
    var meta = metaRoute();
    var frag = (window.location.hash || "").replace(/^#/, "");
    if (frag === "") return { route: meta, ownPage: true };
    var r = parsedRoute(frag);
    if (!r) return { route: frag, ownPage: frag === meta };
    if (r.type === "notfound") return { route: meta, ownPage: true };
    if (r.type === "home" && r.anchor) {
      var m = parsedRoute(meta);
      if (m && m.type === "home") return { route: frag, ownPage: true };
      // A file route names a heading without the md- prefix of its id, so the app's own scroll finds it.
      var suffix = (m && m.type === "file") ? r.anchor.replace(/^md-/, "") : r.anchor;
      // Ask the grammar whether the page's own route takes the anchor as a suffix, so a shared commits row survives the upgrade.
      var combined = meta + ":" + suffix;
      var c = parsedRoute(combined);
      if (c && c.type !== "notfound" && c.anchor === suffix) return { route: combined, ownPage: true };
      return { route: meta, ownPage: true };
    }
    return { route: frag, ownPage: frag === meta };
  }

  // CHROME is the nav and content shell the app renders into, kept in sync with index.html's body.
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
    '    <nav id="nav" class="nav-list">',
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
    '        <a href="#/code" data-nav="code" id="nav-code"><span class="nav-icon">❯</span>Code<span id="nav-code-search" class="nav-search" role="button" tabindex="0" aria-label="Search files" title="Search files"></span></a>',
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
    '  <main id="view" class="content"><div class="loading"><span class="spinner" role="status" aria-label="Loading"></span></div></main>',
    '</div>',
  ].join("\n");

  // stageChrome appends the chrome hidden, beside the static content, and returns the nodes reveal unhides.
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

  // ADOPT_ROUTE matches the routes whose served page is the first view: an item page and a type list page.
  var ADOPT_ROUTE = /^(?:commit:[0-9a-f]{7,40}@gitmsg\/(?:pm|review|social|release|memo)|\/issues|\/prs|\/timeline|\/releases|\/memos)$/;

  // adoptServed moves the served content, all of #gs-page but its sidebar, into the staged #view; null when the entry is not the page's own or the page has no such content.
  function adoptServed(entry, chromeNodes) {
    if (!entry.ownPage || entry.route !== metaRoute() || !ADOPT_ROUTE.test(entry.route)) return null;
    var mount = document.getElementById("gs-page");
    var view = null;
    for (var i = 0; i < chromeNodes.length && !view; i++) view = chromeNodes[i].querySelector('[id="view"]');
    if (!mount || !view) return null;
    var nodes = [], content = false;
    var kids = [].slice.call(mount.childNodes);
    for (var k = 0; k < kids.length; k++) {
      var n = kids[k];
      if (n.nodeType === 1 && /^aside$/i.test(n.tagName) && /(^|\s)page-nav(\s|$)/.test(n.getAttribute("class") || "")) continue;
      if (n.nodeType === 1 || /\S/.test(n.nodeValue || "")) content = true;
      nodes.push(n);
    }
    if (!content) return null;
    while (view.firstChild) view.removeChild(view.firstChild);
    // An item takes the .detail wrapper of the app's item view, so the app's detail rules style it; a restore moves the nodes out of it.
    var slot = view;
    if (entry.route.indexOf("commit:") === 0) { slot = document.createElement("div"); slot.className = "detail"; view.appendChild(slot); }
    for (var m = 0; m < nodes.length; m++) slot.appendChild(nodes[m]);
    return { mount: mount, nodes: nodes };
  }

  // BOOT_MAX_MS bounds the shell-asset phase, from the first request to gs-app.js having executed; expiring restores the served page.
  var BOOT_MAX_MS = 10000;

  // APP_MAX_MS takes the deadline over once gs-app.js runs, outlasting its own route watchdog so a route making progress is not torn down.
  var APP_MAX_MS = 35000;

  // wireChrome re-attaches index.html's inline behaviors: the theme toggle, sidebar collapse, width toggle and mobile drawer.
  function wireChrome() {
    var body = document.body;
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
    (function () {
      var collapse = document.getElementById("nav-collapse");
      var handle = document.getElementById("nav-handle");
      function apply(c) { body.classList.toggle("nav-collapsed", c); try { localStorage.setItem("navCollapsed", c ? "1" : "0"); } catch (e) { /* private */ } }
      var saved = "0"; try { saved = localStorage.getItem("navCollapsed") || "0"; } catch (e) { /* private */ }
      body.classList.toggle("nav-collapsed", saved === "1");
      if (collapse) collapse.addEventListener("click", function () { apply(true); });
      if (handle) handle.addEventListener("click", function () { apply(false); });
    })();
    (function () {
      var toggle = document.getElementById("width-toggle");
      var saved = "fixed"; try { saved = localStorage.getItem("layout") || "fixed"; } catch (e) { /* private */ }
      body.classList.toggle("wide", saved === "wide");
      if (toggle) toggle.addEventListener("click", function () { var wide = !body.classList.contains("wide"); body.classList.toggle("wide", wide); try { localStorage.setItem("layout", wide ? "wide" : "fixed"); } catch (e) { /* private */ } });
    })();
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

  // loadScript appends a base-relative script with async false, so injected scripts download in parallel and execute in insertion order.
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

  // shellURL joins the served base, the versioned shell directory the page announces, and one asset name.
  function shellURL(base, name) { return base + (window.GS_SHELL || "") + name; }

  // preloadScript warms gs-app.js into the cache without running it, since executing it auto-runs init().
  function preloadScript(src) {
    try {
      var link = document.createElement("link");
      link.rel = "preload";
      link.as = "script";
      link.href = src;
      document.head.appendChild(link);
    } catch (e) { /* no preload support — the ordinary load below still works */ }
  }

  // loadStylesheet appends a base-relative sheet and resolves once it loads; media keeps it inert until reveal flips it to "all".
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

  // pageURLForHash maps a hash fragment to the page URL it corresponds to, or null for an app-only route that keeps its hash.
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
    var f = FILE_ROUTE.exec(frag);
    var pages = (typeof window !== "undefined" && window.__gsFilePages) || null;
    if (f && f[4] === undefined && pages && f[3] === pages.branch && Object.prototype.hasOwnProperty.call(pages.keys, f[2])) return base + pages.keys[f[2]];
    return null;
  }

  // COMMITS_ROUTE mirrors gs-core's /commits[/<n>][:<anchor>] grammar: the page is the object, the anchor the place in it.
  var COMMITS_ROUTE = /^\/commits(?:\/(\d+))?(?::([A-Za-z0-9][\w.-]*))?$/;

  // FILE_ROUTE mirrors gs-core's file:<path>@<branch>[:<suffix>] grammar; a suffix is a line range or a heading anchor.
  var FILE_ROUTE = /^(file:([^@]*)@([^:]*))(?::(.*))?$/;

  // routeParts splits a route fragment into its page route and its in-page anchor: a commits row, or a file heading as its md- id.
  function routeParts(frag) {
    frag = String(frag || "");
    var c = COMMITS_ROUTE.exec(frag);
    if (c && c[2]) return { route: frag.slice(0, frag.length - c[2].length - 1), anchor: "#" + c[2] };
    var f = FILE_ROUTE.exec(frag);
    if (f && f[4] !== undefined && /^[A-Za-z0-9][\w.-]*$/.test(f[4]) && !/^L\d+(?:-\d+)?$/.test(f[4])) return { route: f[1], anchor: "#md-" + f[4] };
    return { route: frag, anchor: "" };
  }

  // routeAnchor returns a route fragment's trailing "#<anchor>", and routeWithoutAnchor is its complement.
  function routeAnchor(frag) { return routeParts(frag).anchor; }
  function routeWithoutAnchor(frag) { return routeParts(frag).route; }

  // overrideQuery returns the leading "?…" of the ?base=/?repo= params, which must survive every URL rewrite.
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

  // entryURLForHash builds the normalized entry URL for an app-only route, preserving any bucket override.
  function entryURLForHash(base, frag, search) {
    return base + "index.html" + overrideQuery(search) + "#" + frag;
  }

  // hashForPath maps a served page URL back to the hash the app routes on; null for an item page, which a popstate reloads instead.
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

  // pathRel returns a served URL's path relative to base, or null when it is not under base.
  function pathRel(base, href) {
    var noHash = String(href).split("#")[0].split("?")[0];
    if (noHash.indexOf(base) !== 0) return null;
    return noHash.slice(base.length);
  }

  var firstSync = true;
  // servedRel is the path the document was served at, read on the first sync; only there does the gs-route meta name the current page.
  var servedRel = null;
  // lastHref is the URL after the last sync, so a popstate can tell whether the browser fires hashchange itself.
  var lastHref = "";
  // syncURL reflects the rendered route into history and records the URL it left.
  function syncURL(base) {
    reflectURL(base);
    lastHref = window.location.href;
  }

  // reflectURL writes the clean page URL when the route has a page, else the normalized entry URL; the hash change that drove it already made the history entry, so every write is a replace.
  function reflectURL(base) {
    var frag = (window.location.hash || "").replace(/^#/, "");
    // An empty hash means the URL is already the reflection: a page URL whose route hash a sync stripped.
    if (frag === "") { firstSync = false; return; }
    var q = overrideQuery(window.location.search);
    var anchor = routeAnchor(frag);
    var route = routeWithoutAnchor(frag);
    // Keep the current page when it is itself a valid page for this route; only the residual entry hash is stripped.
    var curRel = pathRel(base, window.location.href);
    if (firstSync) servedRel = curRel;
    var meta = metaRoute();
    var ownPage = curRel !== null && curRel === servedRel && meta !== "" && route === meta;
    if (curRel !== null && (ownPage || hashForPath(base, base + curRel) === "#" + route)) {
      var keepURL = base + curRel + q + anchor;
      if (curRel !== "" && (window.location.pathname || "").length && window.location.href !== keepURL) {
        try { history.replaceState({ gs: 1 }, "", keepURL); } catch (e) { /* history off */ }
      }
      firstSync = false;
      return;
    }
    var pageURL = pageURLForHash(base, route);
    if (!pageURL) {
      var entryURL = entryURLForHash(base, frag, window.location.search);
      if (window.location.href !== entryURL) {
        try { history.replaceState({ gs: 1 }, "", entryURL); } catch (e) { /* history unavailable — stay on the hash URL */ }
      }
      firstSync = false;
      return;
    }
    var target = pageURL + q + anchor;
    if (window.location.href === target) { firstSync = false; return; }
    try { history.replaceState({ gs: 1 }, "", target); } catch (e) { /* history unavailable — stay on the hash URL */ }
    firstSync = false;
  }

  // fireHashchange dispatches the event the app routes on, for a pop the browser reports with popstate alone.
  function fireHashchange() {
    try { window.dispatchEvent(typeof HashChangeEvent === "function" ? new HashChangeEvent("hashchange") : new Event("hashchange")); } catch (e) { /* no event constructor — the hash still names the route on the next change */ }
  }

  // wireNav re-renders the app on popstate by deriving the hash from the URL; a URL with no derivable hash reloads.
  function wireNav(base) {
    window.addEventListener("popstate", function () {
      var href = window.location.href;
      var frag = (window.location.hash || "").replace(/^#/, "");
      var h = hashForPath(base, href);
      // A row anchor on a page URL is not a route, so re-drive that page's own route with the anchor as the grammar's suffix.
      var anchored = frag !== "" && h && /^[A-Za-z0-9][\w.-]*$/.test(frag) && COMMITS_ROUTE.test(h.slice(1) + ":" + frag);
      // The browser fires hashchange only when the path is unchanged; a pop between two page URLs gets the event here.
      var browserFires = href.split("#")[0] === lastHref.split("#")[0] && frag !== (lastHref.split("#")[1] || "");
      lastHref = href;
      if (frag !== "" && !anchored) { if (!browserFires) fireHashchange(); return; }
      if (!h) { window.location.reload(); return; }
      // A replace fires hashchange and keeps the pop's history position; a hash assignment would push and drop the forward entries.
      window.location.replace(anchored ? h + ":" + frag : h);
    });
    window.addEventListener("hashchange", function () { syncURL(base); });
  }

  // wireFilePages installs the hook gs-app calls once window.__gsFilePages is set, which reflects the current route in place.
  function wireFilePages(base) {
    window.__gsOnFilePages = function () { syncURL(base); };
  }

  // boot runs the takeover and restores the served page on any failure, re-throwing so the caller can report it.
  async function boot() {
    try {
      await upgrade();
    } catch (err) {
      restoreStatic();
      throw err;
    }
  }

  // upgrade resolves the base and route, stages the chrome, loads the shell assets and lets gs-app.js init() render.
  async function upgrade() {
    var base = resolveBase();
    window.__gsBase = base;
    try { entryHref = window.location.href; } catch (e) { entryHref = null; }
    var giveUp = (typeof setTimeout === "function") ? setTimeout(restoreStatic, BOOT_MAX_MS) : null;
    var shellLoad = Promise.all([
      loadScript(shellURL(base, "icons.js")).catch(function (e) { /* icons optional */ }),
      loadScript(shellURL(base, "gs-core.js")),
      loadScript(shellURL(base, "gs-render.js")),
    ]);
    var cssLoad = loadStylesheet(shellURL(base, "pages-full.css"), "not all");
    cssLoad.catch(function (e) { /* delivered at the await below */ });
    // A page without the inlined data-gs-core base predates the core/full split, so the core sheet is fetched alongside.
    var coreLoad = null;
    try {
      if (!document.querySelector("style[data-gs-core]")) coreLoad = loadStylesheet(shellURL(base, "pages-core.css"), "not all");
    } catch (e) { /* shimmed DOM — the inlined core is the normal case */ }
    if (coreLoad) coreLoad.catch(function (e) { /* delivered at the await below */ });
    preloadScript(shellURL(base, "gs-app.js"));
    await shellLoad;
    // Resolve the entry now: entryFor asks gs-core's parseRoute, which exists only once the batch above has run.
    var entry = entryFor();
    var route = entry.route;
    var appCSS = await cssLoad;
    var coreCSS = coreLoad ? await coreLoad : null;
    // Capture what the reveal suspends: the static page's own styles and body. The inlined data-gs-core base stays live across the swap.
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
    var cloaked = false;
    try { cloaked = document.documentElement.classList.contains(BOOT_CLASS); } catch (e) { /* shimmed DOM */ }
    var adopted = cloaked ? adoptServed(entry, chromeNodes) : null;
    // revealChrome puts the app's frame up once the stylesheet governs the page, suspending the static layer reversibly.
    function revealChrome() {
      if (undoChrome) return;
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
        if (adopted) for (var a = 0; a < adopted.nodes.length; a++) adopted.mount.appendChild(adopted.nodes[a]);
        // Hide, rather than remove, everything the takeover added, so a late gs-app still finds a #view to render into.
        var kids = [].slice.call(document.body.childNodes);
        for (var m = 0; m < kids.length; m++) {
          if (kids[m].nodeType === 1 && staticBody.indexOf(kids[m]) < 0) kids[m].style.display = "none";
        }
      };
    }
    // revealApp drops the loading treatment, takes the latch, and drops the suspended static layer for good.
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
    // The chrome goes up now, unless the page was left visible: there the two steps collapse and revealApp does both.
    if (cloaked) revealChrome();
    // An adopted page is its own first view: it shows with the chrome, before any bucket read, and the route's data redraws it.
    if (adopted) { try { document.documentElement.classList.remove(LOADING_CLASS); } catch (e) { /* shimmed DOM */ } }
    // The handshake gs-app.js honors once its first route has settled, a view's deferred section included.
    window.__gsOnFirstView = revealApp;
    // gs-app.js keeps the served <title> on the page's own entry, titles every other route, and paints no spinner over an adopted page.
    window.__gsPageEntry = { ownPage: entry.ownPage, adopted: !!adopted };
    // Seed the route: location.hash is the only channel gs-app's init reads.
    if (route && ("#" + route) !== window.location.hash) {
      try { history.replaceState(null, "", "#" + route); } catch (e) { window.location.hash = route; }
    }
    // gs-app.js loads last and auto-runs init(), so the chrome must be staged and the route seeded first.
    await loadScript(shellURL(base, "gs-app.js"));
    if (giveUp !== null) { try { clearTimeout(giveUp); } catch (e) { /* no clearTimeout */ } }
    if (typeof setTimeout === "function") setTimeout(restoreStatic, APP_MAX_MS);
    // Wire the URL reflection after gs-app: hashchange listeners fire in registration order, and the app must read the hash first.
    wireNav(base);
    wireFilePages(base);
    syncURL(base);
  }

  function run() {
    boot().catch(function (err) {
      try { if (console && console.error) console.error("gitsocial: page upgrade failed:", err && err.message); } catch (e) { /* no console */ }
    });
  }

  // Under CommonJS the file is a pure library for the sitetest boot suite: it exports these helpers and does not auto-boot.
  if (typeof module !== "undefined" && module.exports) {
    module.exports = {
      pageURLForHash: pageURLForHash, hashForPath: hashForPath,
      entryURLForHash: entryURLForHash, overrideQuery: overrideQuery,
      routeAnchor: routeAnchor, entryFor: entryFor,
      syncURL: syncURL, wireNav: wireNav, wireFilePages: wireFilePages, boot: boot,
      // _resetSync returns the module to its just-loaded state, so a suite can drive one boot after another.
      _resetSync: function () { firstSync = true; servedRel = null; lastHref = ""; settled = false; undoChrome = null; entryHref = null; },
      // _setBootMaxMs shortens both give-up watchdogs so a suite need not wait them out; Node only.
      _setBootMaxMs: function (ms) { BOOT_MAX_MS = ms; APP_MAX_MS = ms; },
    };
    return;
  }
  if (typeof document === "undefined" || typeof window === "undefined") return;
  // Take ownership of the hide the page's inline script put in place; its own failsafe stands down when it sees this flag.
  window.__gsBooting = true;
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", run);
  else run();
})();
