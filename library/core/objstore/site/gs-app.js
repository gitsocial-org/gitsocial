// gs-app.js - boot layer: router dispatch, nav wiring (highlight, sidebar tree
// slot, memos reveal), init/hashchange, and object-URL lifecycle. Extends the
// shared GS namespace defined by gs-core.js and gs-render.js.

if (typeof module !== "undefined" && module.exports) { require("./gs-core.js"); require("./gs-render.js"); }
(function () {
  const root = (typeof globalThis !== "undefined") ? globalThis : (typeof window !== "undefined" ? window : this);
  const NS = root.GS || (root.GS = {});
  const { COMMIT_VIEW, deriveBase, loadExtItemsAll, loadExtItemsWindow, loadInteractionCounts, countsFor, loadManifest, loadSiteCustomization, loadTimelineWindow, mdSlug, newContext, parseRoute, readRefMode, PR_STATES, analyticsView, autoScrollListView, boardView, branchLogView, branchesView, compareView, graphView, codeView, comingSoon, commitDetail, configView, el, filteredListView, focusSearchInput, focusTreeSearch, highlightNav, homeView, issuesBody, milestonesBody, sprintsBody, itemDetail, listDetailView, listsView, memoCard, pagedListView, prCard, releaseCard, renderList, revokeObjectUrls, searchIconEl, searchView, setView, tagsView, tagDetail, timelineCard, treeOrBlob, updateCodeSidebar } = NS;

  // pendingTreeFocus defers focusing the file-tree search until after a Code
  // route renders (when the magnifier is clicked from a non-code view).
  let pendingTreeFocus = false;

  // WATCHDOG_MS bounds how long a route may sit on "Loading…" before the boot
  // watchdog surfaces a visible error (a large real repo can take several seconds
  // to hydrate its first window, so this is generous — it only trips on a genuine
  // stall, never a slow-but-progressing load).
  const WATCHDOG_MS = 30000;

  // scrollToAnchor scrolls to a rendered markdown heading (md- slugged id),
  // retrying briefly because markdown panes can fill in after the view mounts.
  function scrollToAnchor(slug, tries) {
    const t = document.getElementById("md-" + mdSlug(slug));
    if (t && t.scrollIntoView) { t.scrollIntoView(); return; }
    if (tries > 0 && typeof setTimeout === "function") setTimeout(() => scrollToAnchor(slug, tries - 1), 200);
  }

  async function route(ctx) {
    const r = parseRoute(location.hash);
    if (r.canonical && r.canonical !== location.hash) { location.replace(r.canonical); return; }
    // Remember the last in-app hash so a detail page's "back" returns to where the
    // user came from (a board/milestone/sprint/search list), not a fixed default.
    // Updated AFTER a detail render reads it (see recordRoute at the end).
    ctx.backFrom = ctx.lastHash || "";
    let activeTab = "";
    if (r.type === "index") activeTab = r.tab;
    else if (r.type === "commit" && COMMIT_VIEW[r.branch]) activeTab = COMMIT_VIEW[r.branch].tab;
    else if (r.type === "home") activeTab = "home";
    else if (r.type === "code" || r.type === "file") activeTab = "code";
    else if (r.type === "branches" || r.type === "branch" || r.type === "compare") activeTab = "branches";
    else if (r.type === "graph") activeTab = "graph";
    else if (r.type === "tags" || r.type === "tag") activeTab = "tags";
    else if (r.type === "analytics") activeTab = "analytics";
    else if (r.type === "board") activeTab = "board";
    else if (r.type === "search") activeTab = "search";
    else if (r.type === "lists" || r.type === "list") activeTab = "lists";
    else if (r.type === "config") activeTab = "config";
    highlightNav(activeTab);
    revokeObjectUrls();
    setView([el("div", { class: "loading" }, ["Loading…"])]);
    // Boot watchdog: a route whose async work never settles (an unsettled promise
    // or a wedged walk) would otherwise leave the page on "Loading…" forever with
    // NO console error. This bounds that failure: if the view is still the loading
    // placeholder after WATCHDOG_MS, surface a visible, actionable error and log
    // it, so a hang is never silent. `settled` is flipped by every real setView
    // below (routeSettled), so a slow-but-valid load that DID paint never trips it.
    let settled = false;
    const watchdog = (typeof setTimeout === "function") ? setTimeout(() => {
      if (settled) return;
      try { if (typeof console !== "undefined" && console.error) console.error("gitsocial: route " + location.hash + " did not render within " + (WATCHDOG_MS / 1000) + "s (possible stalled load)"); } catch (e) { /* no console */ }
      setView([el("div", { class: "err" }, [
        "This view is taking unusually long to load and may have stalled. Reload the " +
        "page; if it persists, the bucket's data or a specific item may be malformed.",
      ])]);
    }, WATCHDOG_MS) : null;
    // routeSettled marks the route resolved and clears the watchdog — called on
    // every render exit (success, handled error, or the catch below).
    const routeSettled = () => { settled = true; if (watchdog && typeof clearTimeout === "function") clearTimeout(watchdog); };
    try {
      if (ctx.refMode === undefined) ctx.refMode = await readRefMode(ctx.base);
      if (ctx.refMode && ctx.refMode !== "etag") {
        if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
        if (!ctx.manifest) {
          setView([el("div", { class: "err" }, [
            "This bucket uses \"" + ctx.refMode + "\" ref mode, whose refs the static " +
            "reader can only resolve through the refs manifest — and this bucket has " +
            "none yet. Push with a current gitsocial, or run `gitsocial site push`, " +
            "to publish it.",
          ])]);
          return;
        }
      }
      if (r.type === "index" && r.tab === "timeline") {
        const [first, counts] = await Promise.all([loadTimelineWindow(ctx, false), loadInteractionCounts(ctx)]);
        setView(autoScrollListView(first,
          (items, box) => box.replaceChildren(...renderList(items, (it) => timelineCard(it, countsFor(counts, it.commit.short)), "No activity in this repository yet.")),
          () => loadTimelineWindow(ctx, true)));
      } else if (r.type === "index" && r.tab === "memos") {
        const first = await loadExtItemsWindow(ctx, "memo", false);
        setView(pagedListView(first,
          (items, box) => box.replaceChildren(...renderList(items, memoCard, "No memos in this repository.")),
          () => loadExtItemsWindow(ctx, "memo", true)));
      } else if (r.type === "index" && r.tab === "issues") {
        // Full pm set (metadata-only, cheap and refreshed on every push) so the
        // state-filter counts are exact on first paint; the list paginates client-side.
        const [pm, counts] = await Promise.all([loadExtItemsAll(ctx, "pm"), loadInteractionCounts(ctx)]);
        setView(issuesBody(pm, counts));
      } else if (r.type === "index" && r.tab === "milestones") {
        setView(milestonesBody(await loadExtItemsAll(ctx, "pm")));
      } else if (r.type === "index" && r.tab === "sprints") {
        setView(sprintsBody(await loadExtItemsAll(ctx, "pm")));
      } else if (r.type === "index" && r.tab === "prs") {
        const [all, counts] = await Promise.all([loadExtItemsAll(ctx, "review"), loadInteractionCounts(ctx)]);
        setView(filteredListView(all.filter((i) => (i.header.type || "") === "pull-request"), (it) => prCard(it, countsFor(counts, it.commit.short)), "prs", PR_STATES, "No pull requests in this repository."));
      } else if (r.type === "index" && r.tab === "releases") {
        const relsOf = (items) => items.filter((i) => (i.header.type || "") === "release");
        const first = await loadExtItemsWindow(ctx, "release", false);
        setView(pagedListView(first,
          (items, box) => box.replaceChildren(...renderList(relsOf(items), releaseCard, "No releases in this repository.")),
          () => loadExtItemsWindow(ctx, "release", true)));
      } else if (r.type === "commit") {
        setView(COMMIT_VIEW[r.branch] ? await itemDetail(ctx, r.hash, r.branch) : await commitDetail(ctx, r.hash));
      } else if (r.type === "home") {
        setView(await homeView(ctx));
        if (r.anchor) scrollToAnchor(r.anchor, 10);
      } else if (r.type === "analytics") {
        setView(await analyticsView(ctx));
      } else if (r.type === "board") {
        setView(await boardView(ctx));
      } else if (r.type === "search") {
        setView(searchView(ctx, r.q));
      } else if (r.type === "lists") {
        setView(await listsView(ctx));
      } else if (r.type === "list") {
        setView(await listDetailView(ctx, r.id));
      } else if (r.type === "config") {
        setView(await configView(ctx));
      } else if (r.type === "branches") {
        setView(await branchesView(ctx));
      } else if (r.type === "branch") {
        setView(await branchLogView(ctx, r.name));
      } else if (r.type === "compare") {
        setView(await compareView(ctx, r.base, r.head));
      } else if (r.type === "graph") {
        setView(await graphView(ctx));
      } else if (r.type === "tags") {
        setView(await tagsView(ctx));
      } else if (r.type === "tag") {
        setView(await tagDetail(ctx, r.name));
      } else if (r.type === "code") {
        setView(await codeView(ctx));
      } else if (r.type === "file") {
        setView(await treeOrBlob(ctx, r.path, r.branch, r.line, r.lineEnd));
        if (r.anchor) scrollToAnchor(r.anchor, 10);
      } else if (r.type === "reserved") {
        setView(comingSoon(r));
      } else {
        setView([el("div", { class: "empty" }, ["Not found."])]);
      }
    } catch (err) {
      if (err && err.forbidden) {
        // A 401/403 anywhere in the boot probes (HEAD, ref-mode, manifest) or a
        // walk means the bucket's public read is denied — surface it as one clear
        // page rather than an empty/"not found" view (a missing object is 404 and
        // stays quiet).
        setView([el("div", { class: "err" }, [
          "This bucket's public access appears to be disabled (the server returned " +
          "403 Forbidden). A gitsocial static site is served from the bucket's public " +
          "web endpoint with anonymous reads enabled — check the bucket's public-access " +
          "or website configuration.",
        ])]);
        return;
      }
      setView([el("div", { class: "err" }, ["Error: " + err.message])]);
    } finally {
      // Any exit from the render block (a real view, a handled error, an early
      // return, or a thrown+caught error) means the route resolved: clear the
      // watchdog so it can never fire over a page that already painted.
      routeSettled();
    }
    // Record this hash as the "came from" for the NEXT navigation's back link.
    // Detail routes are excluded so back never bounces detail→detail.
    if (r.type !== "commit" && r.type !== "tag") ctx.lastHash = location.hash;
    // Content is rendered (cache warmed); reflect the code-context sidebar tree.
    await updateCodeSidebar(ctx, r);
    // Honor a deferred magnifier focus once the Code tree (and its search) exist.
    if (pendingTreeFocus && r.type === "code") { pendingTreeFocus = false; focusTreeSearch(); }
  }

  // activateNavSearch reveals/focuses the file-tree search from the Code nav
  // magnifier. In a code/dir context the tree (and its input) is already mounted,
  // so focus it directly; elsewhere navigate to Code first and focus on render.
  function activateNavSearch(ev) {
    if (ev) { if (ev.preventDefault) ev.preventDefault(); if (ev.stopPropagation) ev.stopPropagation(); }
    const r = parseRoute(location.hash);
    if (r.type === "code" || r.type === "file") { focusTreeSearch(); return; }
    pendingTreeFocus = true;
    location.hash = "#/code";
  }

  // wireNavSearch fills the Code nav item's magnifier (trusted SVG) and wires its
  // click/keyboard to activateNavSearch. It focuses the existing tree search
  // input rather than adding a second search box.
  function wireNavSearch() {
    const btn = typeof document !== "undefined" && document.getElementById ? document.getElementById("nav-code-search") : null;
    if (!btn) return;
    const icon = searchIconEl && searchIconEl();
    if (icon) btn.replaceChildren(icon); else btn.textContent = "⌕";
    btn.addEventListener("click", activateNavSearch);
    btn.addEventListener("keydown", (ev) => { if (ev.key === "Enter" || ev.key === " ") activateNavSearch(ev); });
  }

  // isTypingTarget reports whether an event target is a text-entry control, so
  // the `/` shortcut does not steal a slash the user is typing into a field.
  function isTypingTarget(t) {
    if (!t || !t.tagName) return false;
    const tag = String(t.tagName).toLowerCase();
    return tag === "input" || tag === "textarea" || tag === "select" || t.isContentEditable === true;
  }

  // onGlobalKey handles the `/` shortcut: focus the item search from anywhere
  // (navigating to #/search first when elsewhere), unless the user is typing in a
  // field. Mirrors the TUI's global `/` to Search.
  function onGlobalKey(ev) {
    if (ev.key !== "/" || ev.metaKey || ev.ctrlKey || ev.altKey) return;
    if (isTypingTarget(ev.target)) return;
    if (ev.preventDefault) ev.preventDefault();
    if (parseRoute(location.hash).type === "search") { if (focusSearchInput) focusSearchInput(); return; }
    location.hash = "#/search";
  }

  // repoTitle derives a display name from the served directory.
  function repoTitle(base) {
    try {
      const u = new URL(base);
      const segs = u.pathname.split("/").filter(Boolean);
      return segs.length ? segs[segs.length - 1] : (u.hostname || "repository");
    } catch { return "repository"; }
  }

  // HEX_RE / FAVICON_RE mirror the writer's strict validation (site_customization.go):
  // an accent must be #rgb/#rrggbb; a favicon must be an allowed image data URI.
  // Invalid values are ignored so a bad config never breaks the page.
  const HEX_RE = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;
  const FAVICON_RE = /^data:image\/(png|webp|svg\+xml)[;,]/;

  // setDocTitle applies a title to the tab and header, textContent only.
  function setDocTitle(name) {
    if (typeof document === "undefined" || !name) return;
    document.title = name;
    const title = document.getElementById("repo-title");
    if (title) title.textContent = name;
    const mobileTitle = document.getElementById("mobile-title");
    if (mobileTitle) mobileTitle.textContent = name;
  }

  // applyAccent overrides the --link accent token. The theme toggle sets a
  // body.{light,dark}-mode class whose --link wins over an inline :root style, so
  // a validated hex is injected via a scoped <style> (literals only, never
  // interpolated user text) that overrides :root, both theme classes, and the
  // dark media query. accentDark, when valid, tints the dark theme separately.
  function applyAccent(accent, accentDark) {
    if (typeof document === "undefined") return;
    const light = HEX_RE.test(accent || "") ? accent : "";
    const dark = HEX_RE.test(accentDark || "") ? accentDark : (light || "");
    if (!light && !dark) return;
    let style = document.getElementById("gs-site-accent");
    if (!style) { style = document.createElement("style"); style.setAttribute("id", "gs-site-accent"); document.head.appendChild(style); }
    const rules = [];
    if (light) { rules.push(":root{--link:" + light + "}"); rules.push("body.light-mode{--link:" + light + "}"); }
    if (dark) { rules.push("body.dark-mode{--link:" + dark + "}"); rules.push("@media (prefers-color-scheme: dark){:root{--link:" + dark + "}}"); }
    style.textContent = rules.join("\n");
  }

  // applyFavicon points the <link rel=icon> at a validated data URI (href only).
  function applyFavicon(favicon) {
    if (typeof document === "undefined" || !FAVICON_RE.test(favicon || "")) return;
    let link = document.querySelector("link[rel=icon]");
    if (!link) { link = document.createElement("link"); link.setAttribute("rel", "icon"); document.head.appendChild(link); }
    link.setAttribute("href", favicon);
  }

  // applySiteCustomization fetches the push-published site-config.json and applies
  // its title/accent/accentDark/favicon overrides with strict validation; each
  // field is applied only when valid, and any absence/error leaves the default.
  async function applySiteCustomization(ctx, fallbackName) {
    let cfg = null;
    try { cfg = await loadSiteCustomization(ctx); } catch { cfg = null; }
    if (cfg && typeof cfg.title === "string" && cfg.title.trim()) setDocTitle(cfg.title.trim());
    else setDocTitle(fallbackName);
    if (cfg) { applyAccent(cfg.accent, cfg.accentDark); applyFavicon(cfg.favicon); }
  }

  async function init() {
    const ctx = newContext(deriveBase(location));
    const name = repoTitle(ctx.base);
    // The document/tab title names the browsed project, not the static "gitsocial"
    // shell placeholder — derived from the same served-directory name as the chrome.
    // A pushed site customization (title/accent/favicon) overrides these defaults.
    setDocTitle(name);
    applySiteCustomization(ctx, name);
    wireNavSearch();
    const run = () => route(ctx);
    window.addEventListener("hashchange", run);
    if (typeof document !== "undefined" && document.addEventListener) document.addEventListener("keydown", onGlobalKey);
    run();
  }

  Object.assign(NS, { route, init, applySiteCustomization, applyAccent, applyFavicon, setDocTitle });
  if (typeof module !== "undefined" && module.exports) module.exports = NS;
})();
if (typeof document !== "undefined") (typeof globalThis !== "undefined" ? globalThis : this).GS.init();
