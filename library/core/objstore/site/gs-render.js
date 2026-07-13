// gs-render.js - DOM rendering layer: el() and the sanitizer, markdown-to-DOM,
// cards, metaRow, views (home/tree/blob/branches/analytics/memos/detail/diff/
// thread/fullscreen), and the icon DOM builders. Extends the shared GS namespace
// defined by gs-core.js.

if (typeof module !== "undefined" && module.exports) require("./gs-core.js");
(function () {
  const root = (typeof globalThis !== "undefined") ? globalThis : (typeof window !== "undefined" ? window : this);
  const NS = root.GS || (root.GS = {});
  const { COMMIT_VIEW, CONCURRENCY, DETAIL_WALK_CAP, THREAD_MAX_DEPTH, WALK_CAP, activityBuckets, anchorFeedback, buildBoard, buildHunks, buildIssueHierarchy, commitRef, compareRef, resolveCompareRef, commitTree, diffLines, diffTrees, effectiveAuthor, effectiveAuthorEmail, effectiveTime, embeddedRefs, fileDiff, findItemDeep, flattenThread, getObject, getTree, groupPM, groupThread, hashEq, headBranchName, hunkLineKeys, hydrateItems, iconColorClass, iconName, intraLine, isBinary, itemLabels, itemSubject, listBranches, listTags, peelTag, listMemberRef, loadAnalyticsData, loadSiteStats, loadBranchLogWindow, loadCompareCommitsWindow, loadGraphWindow, assignGraphLanes, loadExtConfig, loadExtItems, loadExtItemsAll, loadExtItemsUpTo, loadForks, loadListDetail, loadListsSummary, loadSearchWindow, loadSiteConfig, loadSiteCustomization, loadInteractionCounts, countsFor, fullSearchBytes, mergeBase, parseBranchField, parseCommit, parseMarkdown, parentRef, pmParentHash, pmProgress, prFeedback, quotedRefFor, refBranch, refHash, refRepoUrl, refTip, releaseAssets, resolveAncestors, resolveHead, resolvePath, resolveShortShaFromIndex, reviewSummary, searchItemsFaceted, stateCounts, typeGlyph, suggestionBody, topItemAuthors, walkHistory, parseRoute, SWIMLANE_FIELDS, SWIMLANE_LABELS, swimlaneValue, swimlaneOrder, groupBySwimlane, swimlaneLabel } = NS;

  // BACK_ROUTES are the in-app route types a detail page's "back" may return to
  // (a list/board/search the user came from). Detail routes (commit/tag) are
  // excluded so back never bounces detail→detail.
  const BACK_ROUTES = { index: 1, board: 1, search: 1, home: 1, branches: 1, tags: 1, lists: 1, list: 1, analytics: 1, code: 1 };

  // detailBackHref returns a detail page's back-link target: the route the user
  // navigated FROM (ctx.backFrom) when it was an in-app list/board/search route,
  // else the view's own default. This makes "back" return to a board/milestone/
  // sprint/search when the user came from one, not always the fixed index tab.
  function detailBackHref(ctx, defaultHash) {
    const from = ctx && ctx.backFrom;
    if (from) {
      const r = parseRoute(from);
      if (r && BACK_ROUTES[r.type]) return from;
    }
    return defaultHash;
  }

  // ---- Rendering (browser only; never invoked from Node) ----

  function relTime(unixSeconds) {
    const diff = Date.now() / 1000 - unixSeconds;
    const units = [["y", 31536000], ["mo", 2592000], ["d", 86400], ["h", 3600], ["m", 60]];
    for (const [label, secs] of units) {
      const n = Math.floor(diff / secs);
      if (n >= 1) return n + label + " ago";
    }
    return "just now";
  }

  // tzAbbrev returns the reader's local timezone label for a date: the short
  // zone name ("PST", "GMT+2") via Intl when available, else a "UTC±HH:MM"
  // offset. Appended to the precise-time tooltip so an absolute time is
  // unambiguous across zones.
  function tzAbbrev(d) {
    try {
      const parts = new Intl.DateTimeFormat(undefined, { timeZoneName: "short" }).formatToParts(d);
      const tz = parts.find((x) => x.type === "timeZoneName");
      if (tz && tz.value) return tz.value;
    } catch (e) { /* Intl unavailable: fall back to numeric offset */ }
    const off = -d.getTimezoneOffset();
    const sign = off >= 0 ? "+" : "-";
    const abs = Math.abs(off);
    const p = (n) => String(n).padStart(2, "0");
    return "UTC" + sign + p(Math.floor(abs / 60)) + ":" + p(abs % 60);
  }

  // preciseTime formats a unix-seconds timestamp as a local "YYYY-MM-DD HH:MM TZ"
  // string (date + time + timezone) for the hover tooltip on relative-time spans.
  function preciseTime(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    if (isNaN(d.getTime())) return "";
    const p = (n) => String(n).padStart(2, "0");
    const stamp = d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
    return stamp + " " + tzAbbrev(d);
  }

  // timeEl returns a span showing the relative time with the precise local
  // date+time+timezone in its title attribute, so every rendered "N ago" reveals
  // the exact moment on hover. This is the single shared time helper; using it at
  // every relTime call site (cards, meta rows, threads, versions, analytics) is
  // what gives every timestamp the tooltip for free.
  function timeEl(unixSeconds) {
    return el("span", { class: "reltime", title: preciseTime(unixSeconds) }, [relTime(unixSeconds)]);
  }

  // authorEl returns a span showing an author's display name with their email in
  // its title attribute, so hovering any rendered author name reveals the email.
  // The single shared author helper: routing every author render through it gives
  // every card/detail/thread the tooltip for free. The tooltip is omitted when no
  // email is known or the name already IS the email (no redundant hover).
  function authorEl(name, email) {
    const label = name || email || "unknown";
    const attrs = { class: "author" };
    if (email && email !== label) attrs.title = email;
    return el("span", attrs, [label]);
  }

  // commitAuthorEl renders the author for a raw git commit (graph, branch log,
  // compare list, commit detail), preferring the origin author when the commit
  // carries GitMsg origin-* provenance (imported content) and otherwise the git
  // `author` line — never the committer (parseCommit never reads committer). This
  // is the single authorship helper those views share, matching effectiveAuthor
  // for gitmsg item cards.
  function commitAuthorEl(c) {
    const header = (c && c.gitmsg) || null;
    return authorEl(effectiveAuthor(c, header), effectiveAuthorEmail(c, header));
  }

  function el(tag, attrs, children) {
    const node = document.createElement(tag);
    if (attrs) for (const k in attrs) {
      if (k === "class") node.className = attrs[k];
      else node.setAttribute(k, attrs[k]);
    }
    for (const c of children || []) node.append(c);
    return node;
  }

  // gsIcons returns the vendored icon set (window.GSIcons from icons.js), or
  // null when it is absent (old bucket copies, Node without the shim global).
  function gsIcons() {
    if (typeof window !== "undefined" && window.GSIcons) return window.GSIcons;
    if (typeof GSIcons !== "undefined") return GSIcons;
    return null;
  }

  // iconTemplate parses one trusted vendored SVG string once through an inert
  // DOMParser (text/html, so the HTML parser assigns the SVG namespace and the
  // clone renders) and caches the resulting node; null when the key is unknown
  // or parsing fails. The SVGs are trusted assets, never user content, so this
  // path is deliberately separate from the untrusted-HTML sanitizer.
  const iconTemplates = new Map();
  function iconTemplate(key) {
    if (iconTemplates.has(key)) return iconTemplates.get(key);
    const set = gsIcons();
    const str = set && set[key];
    let node = null;
    if (str) {
      try {
        const body = new DOMParser().parseFromString(str, "text/html").body;
        const svgs = body && body.querySelectorAll ? body.querySelectorAll("svg") : [];
        node = svgs && svgs.length ? svgs[0] : null;
      } catch (e) { node = null; }
    }
    iconTemplates.set(key, node);
    return node;
  }

  // iconEl clones a cached icon template into a themed <span>. Returns null when
  // the icon set is absent or the key is unknown, so every call site falls back
  // to its prior text glyph.
  function iconEl(key, cls) {
    const tpl = iconTemplate(key);
    if (!tpl) return null;
    const svg = tpl.cloneNode(true);
    if (svg.setAttribute) svg.setAttribute("aria-hidden", "true");
    const color = iconColorClass(key);
    return el("span", { class: "gs-icon" + (cls ? " " + cls : "") + (color ? " " + color : "") }, [svg]);
  }

  // icon resolves a filename/kind to its key then builds the element (or null).
  function icon(name, kind, cls) { return iconEl(iconName(name, kind), cls); }


  // ---- Syntax highlighting via Prism (browser only, no innerHTML) ----

  // File-extension → Prism grammar name. The base grammars (go/js/ts/json/yaml/
  // bash/markdown/markup/css/diff) ship in prism.js; the rest (python/rust/c/...)
  // are lazy-loaded from grammars/prism-<lang>.js the first time a file/block in
  // that language is rendered (see ensureGrammar), so the shell stays small and a
  // repo pays only for the languages a visitor actually opens. A missing grammar
  // file degrades to plain text.
  const EXT_LANG = {
    go: "go", js: "javascript", mjs: "javascript", ts: "typescript",
    json: "json", yaml: "yaml", yml: "yaml", sh: "bash", bash: "bash",
    md: "markdown", html: "markup", htm: "markup", xml: "markup", css: "css",
    py: "python", pyi: "python", pyw: "python",
    rs: "rust", c: "c", h: "c",
    cpp: "cpp", cc: "cpp", cxx: "cpp", hpp: "cpp", hh: "cpp",
    java: "java", sql: "sql", rb: "ruby",
    kt: "kotlin", kts: "kotlin", swift: "swift",
    toml: "toml", php: "php", cs: "csharp",
    ini: "ini", proto: "protobuf", lua: "lua",
  };
  // Markdown fence tag (and its common aliases) → Prism grammar name. Non-base
  // grammars are lazy-loaded on first use (see ensureGrammar / EXT_LANG).
  const FENCE_LANG = {
    go: "go", golang: "go", js: "javascript", javascript: "javascript",
    mjs: "javascript", jsx: "javascript", ts: "typescript", typescript: "typescript",
    tsx: "typescript", json: "json", yaml: "yaml", yml: "yaml", sh: "bash",
    bash: "bash", shell: "bash", zsh: "bash", console: "bash", md: "markdown",
    markdown: "markdown", html: "markup", htm: "markup", xml: "markup", css: "css", diff: "diff",
    py: "python", python: "python", rs: "rust", rust: "rust",
    c: "c", cpp: "cpp", "c++": "cpp", cxx: "cpp",
    java: "java", sql: "sql", rb: "ruby", ruby: "ruby",
    kt: "kotlin", kotlin: "kotlin", swift: "swift",
    toml: "toml", php: "php", cs: "csharp", csharp: "csharp", "c#": "csharp",
    ini: "ini", proto: "protobuf", protobuf: "protobuf", lua: "lua",
    docker: "docker", dockerfile: "docker",
  };

  // BASE_GRAMMARS are the grammars already bundled in prism.js (and its clike
  // base), so they are highlighted synchronously and never lazy-loaded.
  const BASE_GRAMMARS = {
    markup: 1, css: 1, clike: 1, javascript: 1, typescript: 1, json: 1,
    yaml: 1, bash: 1, go: 1, markdown: 1, diff: 1,
  };
  // GRAMMAR_DEPS lists, for each lazy-loaded grammar, the OTHER lazy-loaded
  // grammars it extends and that must therefore load first (deps before the
  // dependent). Grammars that extend only a base grammar (clike/markup/css,
  // already in prism.js) have no entry. Ported from prismGrammars in the retired
  // site_prism.go. A grammar not listed here has no lazy-load dependencies.
  const GRAMMAR_DEPS = {
    cpp: ["c"],
  };

  // grammarBase is the bucket base URL (trailing slash) grammar files are fetched
  // relative to, set once at boot by setGrammarBase(deriveBase(location)). "" when
  // unset (the loader then no-ops and highlighting stays base-only).
  let grammarBase = "";
  function setGrammarBase(base) { grammarBase = base || ""; }

  // grammarState caches per-session grammar loads: a grammar name maps to a
  // Promise that resolves true once loaded (or false on a quiet failure), so a
  // grammar file is fetched at most once and concurrent requests share it.
  const grammarState = new Map();

  // fetchGrammarText GETs grammars/prism-<name>.js relative to grammarBase and
  // returns its source text, or null on any failure (404, network, no fetch/base)
  // — every one a quiet fallback to plain text, never an error.
  async function fetchGrammarText(name) {
    if (!grammarBase || typeof fetch !== "function") return null;
    try {
      const res = await fetch(grammarBase + "grammars/prism-" + name + ".js");
      if (!res || !res.ok) return null;
      return await res.text();
    } catch (e) { return null; }
  }

  // evalGrammar runs a fetched grammar component with Prism in scope, so its
  // `Prism.languages.X = ...` / `!function(e){...}(Prism)` body registers the
  // grammar on the shared Prism global. Returns true when Prism.languages gained
  // the language, false otherwise. Grammar files are trusted vendored assets
  // (shipped in the shell), never visitor content.
  function evalGrammar(name, src) {
    const P = getPrism();
    if (!P || !src) return false;
    try {
      // eslint-disable-next-line no-new-func
      new Function("Prism", src)(P);
    } catch (e) { return false; }
    return !!(P.languages && P.languages[name]);
  }

  // ensureGrammar loads the grammar `name` (and its dependency chain, deps first)
  // into Prism.languages, returning a Promise<boolean> for whether it is now
  // available. Base grammars resolve true immediately; an already-loaded (or
  // in-flight) grammar reuses its cached Promise; a missing grammar file or a
  // failed dependency resolves false (the caller keeps plain text). Browser-only:
  // absent fetch/Prism/base it resolves whether the grammar happens to be present.
  function ensureGrammar(name) {
    const P = getPrism();
    if (!name || !P || (P.languages && P.languages[name])) return Promise.resolve(!!(P && P.languages && P.languages[name]));
    if (BASE_GRAMMARS[name]) return Promise.resolve(false);
    if (grammarState.has(name)) return grammarState.get(name);
    const load = (async () => {
      for (const dep of GRAMMAR_DEPS[name] || []) {
        if (!(await ensureGrammar(dep))) return false;
      }
      const src = await fetchGrammarText(name);
      if (src === null) return false;
      return evalGrammar(name, src);
    })();
    grammarState.set(name, load);
    return load;
  }

  // lazyHighlight renders `lang`-highlighted content into a freshly-emptied
  // parent NOW (synchronously) via `render`, then — when the grammar is not yet
  // loaded but is lazy-loadable — kicks off ensureGrammar and, on success,
  // re-renders in place so the plain text upgrades to highlighted without ever
  // blocking. A no-op re-render when the grammar fails to load (parent keeps its
  // plain text). Used by every highlight entry point so lazy loading is uniform.
  function lazyHighlight(parent, lang, render) {
    render();
    const P = getPrism();
    if (!P || !lang || !parent || (P.languages && P.languages[lang]) || BASE_GRAMMARS[lang]) return;
    ensureGrammar(lang).then((ok) => {
      if (ok && parent) { parent.replaceChildren(); render(); }
    });
  }

  // langForPath returns the Prism grammar name for a file path, or null.
  function langForPath(path) {
    const dot = (path || "").lastIndexOf(".");
    if (dot < 0) return null;
    return EXT_LANG[path.slice(dot + 1).toLowerCase()] || null;
  }

  // langForFence maps a Markdown fence's info string to a Prism grammar name.
  function langForFence(tag) {
    return FENCE_LANG[(tag || "").toLowerCase().split(/\s+/)[0]] || null;
  }

  // getPrism returns the loaded Prism global, or null when prism.js is absent
  // (Node tests, older bucket copies). typeof guards keep it ReferenceError-safe.
  function getPrism() {
    if (typeof window !== "undefined" && window.Prism && window.Prism.tokenize) return window.Prism;
    if (typeof Prism !== "undefined" && Prism.tokenize) return Prism;
    return null;
  }

  // tokenLeaves flattens a Prism token stream (strings, or {type, content,
  // alias} where content recurses) into flat { text, cls } leaves, cls being the
  // space-joined token classes accumulated down the tree ("" for plain text).
  function tokenLeaves(tokens, cls, out) {
    out = out || [];
    for (const t of tokens) {
      if (typeof t === "string") { out.push({ text: t, cls }); continue; }
      const alias = t.alias ? " " + (Array.isArray(t.alias) ? t.alias.join(" ") : t.alias) : "";
      const tcls = (cls ? cls + " " : "") + "token " + t.type + alias;
      if (typeof t.content === "string") out.push({ text: t.content, cls: tcls });
      else if (Array.isArray(t.content)) tokenLeaves(t.content, tcls, out);
      else tokenLeaves([t.content], tcls, out);
    }
    return out;
  }

  // highlightNow appends `lang`-highlighted DOM for `code` to `parent` using only
  // the grammars currently loaded (no lazy load): tokenized spans when the
  // grammar is present, else a single plain text node. The escaping invariant
  // holds (el()/text nodes, never innerHTML). Returns parent.
  function highlightNow(parent, code, lang) {
    const P = getPrism();
    const grammar = P && lang && P.languages ? P.languages[lang] : null;
    if (!P || !grammar) { parent.append(document.createTextNode(code)); return parent; }
    let leaves;
    try { leaves = tokenLeaves(P.tokenize(code, grammar), ""); }
    catch { parent.append(document.createTextNode(code)); return parent; }
    for (const leaf of leaves) {
      if (leaf.cls) parent.append(el("span", { class: leaf.cls }, [leaf.text]));
      else parent.append(document.createTextNode(leaf.text));
    }
    return parent;
  }

  // highlightTo appends syntax-highlighted DOM for `code` to `parent`, lazy-
  // loading the grammar when needed: it renders plain text now and upgrades it
  // in place once grammars/prism-<lang>.js loads (progressive enhancement, never
  // blocking; a missing grammar stays plain). Returns parent.
  function highlightTo(parent, code, lang) {
    lazyHighlight(parent, lang, () => highlightNow(parent, code, lang));
    return parent;
  }

  // linesFor splits a whole-text Prism tokenization into one { text, cls }
  // segment array per source line (splitting token leaves on newlines), or plain
  // per-line segments when the grammar is absent.
  function linesFor(code, lang) {
    const P = getPrism();
    const grammar = P && lang && P.languages ? P.languages[lang] : null;
    const raw = code.split("\n");
    if (!P || !grammar) return raw.map((t) => [{ text: t, cls: "" }]);
    let leaves;
    try { leaves = tokenLeaves(P.tokenize(code, grammar), ""); }
    catch { return raw.map((t) => [{ text: t, cls: "" }]); }
    const lines = [[]];
    for (const leaf of leaves) {
      const parts = leaf.text.split("\n");
      for (let i = 0; i < parts.length; i++) {
        if (i > 0) lines.push([]);
        if (parts[i] !== "") lines[lines.length - 1].push({ text: parts[i], cls: leaf.cls });
      }
    }
    return lines;
  }

  // highlightLines returns one { text, cls } segment array per source line for a
  // blob view, using only currently-loaded grammars. The blob view (rawBlobPane)
  // lazy-loads the grammar itself and rebuilds its rows on load, so this stays a
  // pure synchronous helper.
  function highlightLines(code, lang) {
    return linesFor(code, lang);
  }

  // appendSegments fills a container with { text, cls } segments as escaped
  // spans / text nodes.
  function appendSegments(container, segs) {
    for (const s of segs) {
      if (s.cls) container.append(el("span", { class: s.cls }, [s.text]));
      else container.append(document.createTextNode(s.text));
    }
    return container;
  }

  function metaRow(item, branch) {
    const c = item.commit;
    const when = item.effectiveTime || c.authorTime;
    const author = item.author || c.authorName || c.authorEmail || "unknown";
    const row = el("span", { class: "meta" }, [authorEl(author, effectiveAuthorEmail(c, item.header)), " · ", timeEl(when), " · "]);
    row.append(el("a", { class: "hash", href: commitRef(c.hash, branch) }, [c.short]));
    if (item.edited) row.append(el("span", { class: "chip" }, ["edited"]));
    if (item.editorName) row.append(el("span", { class: "chip" }, ["edited by " + item.editorName]));
    return row;
  }

  // stateChip renders a colored state pill. Every known state maps to a
  // background class so the chip's white text always has a solid fill behind it
  // (an unmapped state would fall back to the translucent --chip background, on
  // which white text is unreadable). Milestone/sprint lifecycle
  // states (planned/active/completed/canceled) get their own accents.
  function stateChip(state) {
    const map = {
      open: "open", closed: "closed", merged: "merged",
      canceled: "canceled", cancelled: "canceled", completed: "completed",
      active: "active", planned: "planned",
    };
    const cls = map[state] || "unknown";
    return el("span", { class: "chip state " + cls }, [state || "?"]);
  }

  // headerChips builds the enrichment chips a pm/review card shows from its
  // already-published header (labels/assignees/priority — all in the GitMsg line
  // the metadata index carries, so no corpus fetch): a priority/* label as a
  // tinted priority chip, each assignee as a "☛ name" chip, and every other
  // scoped/unscoped label (except status/priority, already shown as glyph/chip)
  // as a plain label chip. Mirrors the TUI issue/PR card stats, kept tasteful:
  // status labels drive the board column, priority gets its own accent, the rest
  // ride as muted chips. Returns an array of chip nodes (possibly empty).
  // originChip returns a "↗ platform" badge for imported content (an item whose
  // header carries origin-* provenance, GITMSG §1.9), else null. The glyph marks
  // the item as mirrored from an external platform; the title names the source.
  function originChip(header) {
    const h = header || {};
    if (!h["origin-platform"] && !h["origin-url"] && !h["origin-author-name"]) return null;
    const label = h["origin-platform"] || "imported";
    return el("span", { class: "chip chip-origin", title: h["origin-url"] || label }, ["↗ " + label]);
  }

  function headerChips(header, counts) {
    const h = header || {};
    const chips = [];
    const rc = retractedChip(h);
    if (rc) chips.push(rc);
    const oc = originChip(h);
    if (oc) chips.push(oc);
    if (h.due) chips.push(el("span", { class: "chip chip-due" }, ["due " + h.due]));
    const labels = itemLabels(h);
    for (const l of labels) if (l.scope === "priority") chips.push(el("span", { class: "chip chip-priority prio-" + l.value }, [l.value]));
    for (const a of (h.assignees || "").split(",").map((s) => s.trim()).filter(Boolean)) {
      chips.push(el("span", { class: "chip chip-assignee" }, ["☛ " + assigneeLabel(a)]));
    }
    for (const l of labels) {
      if (l.scope === "priority" || l.scope === "status") continue;
      chips.push(el("span", { class: "chip chip-label" }, [l.scope ? l.scope + "/" + l.value : l.value]));
    }
    // Interaction/review counts (from cross-branch corpora when the view supplies
    // them). Kept compact: a comment count, repost/quote counts (posts), and a
    // PR review summary (✓N ✗N). Absent counts render nothing.
    if (counts) {
      if (counts.approved || counts.changesRequested) {
        const rs = el("span", { class: "chip chip-review" }, []);
        if (counts.approved) rs.append(el("span", { class: "review-ok" }, ["✓" + counts.approved]));
        if (counts.approved && counts.changesRequested) rs.append(" ");
        if (counts.changesRequested) rs.append(el("span", { class: "review-no" }, ["✗" + counts.changesRequested]));
        chips.push(rs);
      }
      if (counts.reposts) chips.push(el("span", { class: "chip chip-count" }, ["↻ " + counts.reposts]));
      if (counts.quotes) chips.push(el("span", { class: "chip chip-count" }, ["❞ " + counts.quotes]));
      if (counts.comments) chips.push(el("span", { class: "chip chip-count" }, ["↩ " + counts.comments]));
    }
    return chips;
  }

  // assigneeLabel shortens an assignee email to its local part for a compact chip
  // (a full email overflows the card head); a non-email value renders verbatim.
  function assigneeLabel(a) {
    const at = a.indexOf("@");
    return at > 0 ? a.slice(0, at) : a;
  }

  // appendChipRow appends a wrapping chip row (`.card-chips`) to a card when the
  // chip list is non-empty, so a card without enrichment carries no extra row.
  function appendChipRow(card, chips) {
    if (chips && chips.length) card.append(el("div", { class: "card-chips" }, chips));
  }

  // retractedChip returns a "retracted" marker chip when the item's header marks
  // it retracted (derivable without a fetch), else null. Retracted items are
  // dropped from lists by resolveItems, so this only surfaces on a permalink.
  function retractedChip(header) {
    return (header && header.retracted === "true") ? el("span", { class: "chip chip-retracted" }, ["retracted"]) : null;
  }

  // typeGlyphEl renders an item's leading type glyph (matching the TUI card icons)
  // as a titled span. One consistent scheme across every view: a state-bearing item
  // (issue, pull-request) tints its glyph by state (tg-open/closed/merged); every
  // pure type glyph (post, release, milestone, sprint, memo, …) falls through to the
  // single muted --type-glyph color. Null when the type has no glyph.
  function typeGlyphEl(item, ext) {
    const g = typeGlyph(item, ext);
    if (!g) return null;
    const h = item.header || {};
    const t = h.type || ext;
    const mod = (t === "issue" || t === "pull-request") ? glyphStateClass(h.state) : t;
    return el("span", { class: "type-glyph tg-" + mod, title: t }, [g]);
  }
  // glyphStateClass maps an item state to its glyph tint class (open/closed/merged);
  // unknown or absent states default to open, matching the item-list default.
  function glyphStateClass(state) {
    if (state === "merged") return "merged";
    if (state === "closed" || state === "canceled" || state === "cancelled" || state === "completed") return "closed";
    return "open";
  }
  function prependGlyph(head, item, ext) { const g = typeGlyphEl(item, ext); if (g) head.prepend(g); }

  function subjectBody(content) {
    const nl = content.indexOf("\n");
    return nl < 0 ? [content, ""] : [content.slice(0, nl), content.slice(nl + 1).trim()];
  }

  // cardNav makes a whole item card navigate to its #commit: detail route on a
  // click anywhere in it, while leaving inner links and text selection intact: a
  // click inside an <a> keeps that link's own behavior, and a click is ignored
  // while a selection is active. Navigation sets location.hash so history/back
  // works. Keyboard users still reach the item through the inner subject/hash
  // links, so the card takes no tabindex.
  function cardNav(card, hash, branch) {
    card.className = card.className + " clickable";
    card.addEventListener("click", (e) => {
      if (e && e.target && e.target.closest && e.target.closest("a")) return;
      const sel = typeof window !== "undefined" && window.getSelection ? window.getSelection() : null;
      if (sel && !sel.isCollapsed) return;
      location.hash = commitRef(hash, branch);
    });
    return card;
  }

  function renderList(items, mapItem, emptyText) {
    if (!items.length) return [el("div", { class: "empty" }, [emptyText])];
    return items.map(mapItem);
  }

  // pagedListView renders a walk-backed list with a "Load more" control shown
  // whenever the history walk was truncated (unwalked commits remain). `initial`
  // is { items, truncated }; drawBody(items, container) fills the list from the
  // ACCUMULATED item set (filters and counts recompute from it); loadMore()
  // resolves the next { items, truncated } window — its items supersede the prior
  // array, since resolveItems re-ran over the larger commit set (so an edit whose
  // canonical was out of window stops looking like a duplicate). Returns [wrap].
  function pagedListView(initial, drawBody, loadMore) {
    const wrap = el("div", {}, []);
    const body = el("div", {}, []);
    wrap.append(body);
    let moreWrap = null;
    function draw(items, truncated) {
      drawBody(items, body);
      if (moreWrap) { moreWrap.remove(); moreWrap = null; }
      if (!truncated) return;
      const btn = el("button", { class: "load-more", type: "button" }, ["Load more"]);
      moreWrap = el("div", { class: "load-more-wrap" }, [btn]);
      btn.addEventListener("click", async () => {
        btn.disabled = true; btn.textContent = "Loading…";
        try { const next = await loadMore(); draw(next.items, next.truncated); }
        catch (e) { btn.disabled = false; btn.textContent = "Load more"; }
      });
      wrap.append(moreWrap);
    }
    draw(initial.items, initial.truncated);
    return [wrap];
  }

  // autoScrollListView renders a walk-backed list that advances by infinite
  // autoscroll instead of a button (used by the merged timeline, whose window is
  // bounded to keep body hydration small). An IntersectionObserver watches a
  // bottom sentinel and, when it nears the viewport, loads the next window and
  // appends to the ACCUMULATED set; a `loading` guard drops overlapping fires and
  // the observer disconnects once the walk is exhausted (truncated false). After
  // each load it re-observes the sentinel so a short list still filling the
  // viewport keeps advancing. `initial` is { items, truncated }; drawBody(items,
  // container) fills the list from the accumulated set (its items supersede the
  // prior array, resolveItems having re-run over the larger commit set); loadMore()
  // resolves the next { items, truncated } window. In an observer-less headless
  // environment there is no auto-advance — wrap.__loadNext() drives one window
  // advance directly (also the tests' hook). Returns [wrap].
  function autoScrollListView(initial, drawBody, loadMore) {
    const wrap = el("div", {}, []);
    const body = el("div", {}, []);
    const sentinel = el("div", { class: "scroll-sentinel", "aria-hidden": "true" }, []);
    wrap.append(body, sentinel);
    let truncated = !!initial.truncated;
    let loading = false;
    let observer = null;
    drawBody(initial.items, body);
    async function advance() {
      if (loading || !truncated) return;
      loading = true;
      try { const next = await loadMore(); truncated = !!next.truncated; drawBody(next.items, body); }
      catch (e) { /* keep truncated so a later fire retries */ }
      finally {
        loading = false;
        if (observer) {
          if (!truncated) { observer.disconnect(); observer = null; }
          else { observer.unobserve(sentinel); observer.observe(sentinel); }
        }
      }
    }
    wrap.__loadNext = advance;
    const IO = (typeof window !== "undefined" && window.IntersectionObserver) ||
      (typeof IntersectionObserver !== "undefined" ? IntersectionObserver : null);
    if (IO && truncated) {
      observer = new IO((entries) => { for (const e of entries) if (e.isIntersecting) { advance(); break; } }, { rootMargin: "600px" });
      observer.observe(sentinel);
    }
    return [wrap];
  }

  // clampNode wraps a content node in a ~10-line CSS clamp (.body-clamp:
  // max-height + mask fade) with a "Show more"/"Show less" toggle. Overflow can
  // only be measured once the node is laid out, and attachment timing varies:
  // list cards attach within a frame of build, but detail views (reply-context
  // cards) build first and attach only after further async work. So the check
  // polls one frame at a time until the node reports layout (clientHeight > 0),
  // giving up after a few seconds by removing the clamp class — content is
  // never left hidden without a toggle. A node that fits loses the clamp class
  // and gets no toggle. Expansion is an ephemeral in-DOM class flip (no route
  // change); the toggle stops propagation so it never triggers a surrounding
  // cardNav navigation. Used by list-card bodies and the permalink
  // reply-context cards; thread comments and the item's own detail body stay
  // unclamped — they never come through here.
  function clampNode(node) {
    node.classList.add("body-clamp");
    const wrap = el("div", { class: "body-clamp-wrap" }, [node]);
    const defer = (typeof requestAnimationFrame === "function") ? requestAnimationFrame : (fn) => setTimeout(fn, 0);
    const deadline = Date.now() + 4000;
    const measure = () => {
      if (!(node.clientHeight > 0)) {
        if (Date.now() < deadline) { defer(measure); return; }
        node.classList.remove("body-clamp");
        return;
      }
      if (!(node.scrollHeight > node.clientHeight + 1)) { node.classList.remove("body-clamp"); return; }
      const btn = el("button", { class: "body-clamp-toggle", type: "button" }, ["Show more"]);
      let expanded = false;
      btn.addEventListener("click", (ev) => {
        ev.stopPropagation();
        expanded = !expanded;
        if (expanded) node.classList.remove("body-clamp"); else node.classList.add("body-clamp");
        btn.textContent = expanded ? "Show less" : "Show more";
      });
      wrap.append(btn);
    };
    defer(measure);
    return wrap;
  }

  // clampedBody builds a LIST-card raw pre-wrap text body under the clamp.
  function clampedBody(text) {
    return clampNode(el("div", { class: "body" }, [text]));
  }

  function socialCard(item, counts) {
    const [subject, body] = subjectBody(item.content);
    const card = el("div", { class: "card" }, []);
    const meta = metaRow(item, "gitmsg/social");
    prependGlyph(meta, item, "social");
    card.append(el("div", {}, [meta]));
    card.append(clampedBody(subject + (body ? "\n" + body : "")));
    appendChipRow(card, headerChips(item.header, counts));
    return cardNav(card, item.commit.hash, "gitmsg/social");
  }

  // issueCard renders an issue list card. `subCount` (a parent issue's direct
  // child count) adds a "n sub" chip mirroring the TUI sub-issue indicator.
  function issueCard(item, subCount, counts) {
    const subject = itemSubject(item);
    const state = (item.header && item.header.state) || "open";
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      stateChip(state),
      el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"]),
    ]);
    prependGlyph(head, item, "pm");
    if (subCount) head.append(el("span", { class: "chip pm-sub-chip" }, [subCount + " sub"]));
    card.append(head);
    card.append(metaRow(item, "gitmsg/pm"));
    appendChipRow(card, headerChips(item.header, counts));
    return cardNav(card, item.commit.hash, "gitmsg/pm");
  }

  function prCard(item, counts) {
    const subject = itemSubject(item);
    const h = item.header || {};
    const state = h.state || "open";
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      stateChip(state),
      el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/review") }, [subject || "(untitled)"]),
    ]);
    prependGlyph(head, item, "review");
    card.append(head);
    const flow = (h.head || "?") + " → " + (h.base || "?");
    const row = metaRow(item, "gitmsg/review");
    row.append(el("span", { class: "chip" }, [flow]));
    if (h.draft === "true") row.append(el("span", { class: "chip" }, ["draft"]));
    if (h["depends-on"]) row.append(el("span", { class: "chip" }, ["stacked"]));
    card.append(row);
    appendChipRow(card, headerChips(h, counts));
    return cardNav(card, item.commit.hash, "gitmsg/review");
  }

  function releaseCard(item) {
    const [subject, body] = subjectBody(item.content);
    const h = item.header || {};
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, []);
    prependGlyph(head, item, "release");
    head.append(el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/release") }, [h.tag || subject || h.version || "(release)"]));
    // Release-specific chips stay in the head (version, prerelease, asset count);
    // the shared enrichment row (labels, origin ↗, retracted) renders below like
    // issue/PR cards.
    if (h.version) head.append(el("span", { class: "chip" }, ["v" + h.version]));
    if (h.prerelease === "true") head.append(el("span", { class: "chip pre state" }, ["prerelease"]));
    const assets = releaseAssets(h);
    if (assets.artifacts.length) head.append(el("span", { class: "chip" }, [assets.artifacts.length + (assets.artifacts.length === 1 ? " asset" : " assets")]));
    card.append(head);
    card.append(metaRow(item, "gitmsg/release"));
    appendChipRow(card, headerChips(h));
    if (subject && subject !== h.tag) card.append(clampedBody(subject + (body ? "\n" + body : "")));
    return cardNav(card, item.commit.hash, "gitmsg/release");
  }

  // memoCard renders a memo (gitmsg/memo): the subject linking to its detail, the
  // author/time meta (with edited / edited-by chips), the shared enrichment chip
  // row (labels, origin ↗, retracted — same conventions as issue/PR cards), and
  // the body.
  function memoCard(item) {
    const [subject, body] = subjectBody(item.content);
    const h = item.header || {};
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/memo") }, [subject || "(untitled)"]),
    ]);
    prependGlyph(head, item, "memo");
    card.append(head);
    card.append(metaRow(item, "gitmsg/memo"));
    appendChipRow(card, headerChips(h));
    if (body) card.append(clampedBody(body));
    return cardNav(card, item.commit.hash, "gitmsg/memo");
  }

  // timelineCard dispatches a merged-timeline item to the card matching its
  // source extension, so each entry keeps its native shape (issue / PR / release
  // / post) while interleaving by effective time.
  function timelineCard(item, counts) {
    if (item._ext === "code") return commitTimelineCard(item);
    if (item._ext === "pm") return issueCard(item, 0, counts);
    if (item._ext === "review") return prCard(item, counts);
    if (item._ext === "release") return releaseCard(item);
    return socialCard(item, counts);
  }

  // commitTimelineCard renders a plain code commit in the merged timeline: a
  // "commit" glyph, the subject, and the author/time/hash meta linking to the
  // commit detail under the branch it was reached via. It carries no GitMsg
  // header, so no state/enrichment chips and no interaction counts — reusing the
  // branch-log commit-card shape (subject + meta) for visual parity.
  function commitTimelineCard(item) {
    const c = item.commit;
    const branch = item._branch || "";
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      el("a", { class: "subject", href: commitRef(c.hash, branch) }, [subjectBody(c.content)[0] || "(no message)"]),
    ]);
    head.prepend(el("span", { class: "type-glyph tg-commit", title: "commit" }, ["◦"]));
    card.append(head);
    const meta = el("span", { class: "meta" }, [
      commitAuthorEl(c), " · ", timeEl(c.authorTime), " · ",
      el("a", { class: "hash", href: commitRef(c.hash, branch) }, [c.short]),
    ]);
    if (branch) meta.append(el("span", { class: "chip" }, [branch]));
    card.append(meta);
    return cardNav(card, c.hash, branch);
  }

  // versionLabel names a version by its position in the canonical-first list:
  // the last entry is "current", the first "original", middle entries "v<N>"
  // (mirroring the TUI VersionLabel, inverted for the ASC list).
  function versionLabel(i, total) {
    if (i === total - 1) return "current";
    if (i === 0) return "original";
    return "v" + (i + 1);
  }

  // versionMetaRow renders a version's author/time/hash meta with edited /
  // edited-by chips (metaRow's shape, from a version entry).
  function versionMetaRow(v, branch) {
    const when = v.effectiveTime || (v.commit && v.commit.authorTime);
    const row = el("span", { class: "meta" }, [authorEl(v.author || "unknown", effectiveAuthorEmail(v.commit, v.header)), " · ", timeEl(when), " · "]);
    row.append(el("a", { class: "hash", href: commitRef(v.commit.hash, branch) }, [v.commit.short]));
    if (v.edited) row.append(el("span", { class: "chip" }, ["edited"]));
    if (v.editorName) row.append(el("span", { class: "chip" }, ["edited by " + v.editorName]));
    return row;
  }

  // versionDiffPanel renders a unified text diff of two versions' message bodies
  // (previous vs current) — pure presentation over the in-memory commits, no
  // fetches. A notice when the pair is too large; empty when unchanged.
  function versionDiffPanel(prev, cur) {
    const ops = diffLines(prev.content || "", cur.content || "");
    if (ops === null) return el("div", { class: "notice" }, ["Versions too large to diff."]);
    const hunks = buildHunks(ops, 3);
    if (!hunks.length) return el("div", { class: "empty" }, ["No changes to the message."]);
    return el("div", { class: "version-diff" }, [renderHunksUnified(hunks, null)]);
  }

  // versionHistorySection renders the compact "History (N versions)" picker on an
  // item detail page (TUI VersionPicker-flavored): one row per version, newest
  // first, each with a label chip + author + relative time + short hash (and an
  // "edited by" chip when the editor differs). Clicking a row shows that version's
  // content in the main body pane via onSelect; each row above the original
  // carries a "diff to previous" toggle rendering a unified message-body diff from
  // memory (zero new fetches).
  function versionHistorySection(versions, onSelect) {
    const total = versions.length;
    const wrap = el("div", { class: "version-history" }, []);
    wrap.append(el("div", { class: "version-history-head mono" }, ["History (" + total + " versions)"]));
    const rows = [];
    for (let i = total - 1; i >= 0; i--) {
      const v = versions[i];
      const when = v.effectiveTime || (v.commit && v.commit.authorTime);
      const meta = el("span", { class: "version-row-meta" }, [
        el("span", { class: "chip version-label" }, [versionLabel(i, total)]),
        " ", authorEl(v.author || "unknown", effectiveAuthorEmail(v.commit, v.header)), " · ", timeEl(when), " · ",
        el("span", { class: "hash mono" }, [v.commit.short]),
      ]);
      if (v.editorName) meta.append(el("span", { class: "chip" }, ["edited by " + v.editorName]));
      const diffPane = el("div", { class: "version-diff-pane" }, []);
      if (i > 0) {
        const dBtn = el("button", { class: "version-diff-btn", type: "button" }, ["diff to previous"]);
        let open = false;
        dBtn.addEventListener("click", (ev) => {
          ev.stopPropagation();
          open = !open;
          if (open) { diffPane.replaceChildren(versionDiffPanel(versions[i - 1], v)); dBtn.textContent = "hide diff"; }
          else { diffPane.replaceChildren(); dBtn.textContent = "diff to previous"; }
        });
        meta.append(el("span", { class: "version-diff-ctl" }, [dBtn]));
      }
      const row = el("div", { class: "version-row", role: "button", tabindex: "0" }, [meta]);
      const select = () => { for (const r of rows) r.classList.remove("active"); row.classList.add("active"); onSelect(i); };
      row.addEventListener("click", (ev) => { if (ev.target && ev.target.closest && ev.target.closest("button")) return; select(); });
      row.addEventListener("keydown", (ev) => { if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); select(); } });
      rows.push(row);
      wrap.append(el("div", { class: "version-node" }, [row, diffPane]));
    }
    wrap.__rows = rows;
    return wrap;
  }

  // detailView renders an item detail page. When the item carries more than one
  // version (an edit chain), a "History" picker lets the reader select any
  // trailerValue renders one parsed-trailer value: a same-repo original/
  // reply-to ref links to its commit permalink so the reader can jump to the
  // parent; cross-repo values (objects not in this bucket) stay plain text.
  function trailerValue(key, val) {
    if ((key === "original" || key === "reply-to") && !refRepoUrl(val)) {
      const h = refHash(val);
      if (h) return el("a", { href: commitRef(h, refBranch(val)) }, [val]);
    }
    return val;
  }

  // shareURL returns the best shareable URL for an item: the canonical PAGE URL
  // ({site.url}i/<short>.html) when the site config carries a valid url AND the
  // HTML page layer is enabled (pages === "true") — those are the site's
  // permanent, crawlable, shared-forever URLs; otherwise the in-app hash URL
  // (origin + base path + #commit:<short>@<branch>), which always resolves. ctx
  // carries the site customization loaded once per context (loadSiteCustomization);
  // it may be absent (a bucket with no site config), in which case the hash URL
  // is used. `short` is the item's short hash, `branch` its ext data branch.
  function shareURL(ctx, short, branch) {
    const cfg = ctx && ctx.siteCustomization;
    if (cfg && cfg.pages === "true" && typeof cfg.url === "string" && /^https?:\/\//.test(cfg.url)) {
      const base = cfg.url.endsWith("/") ? cfg.url : cfg.url + "/";
      return base + "i/" + short + ".html";
    }
    // Hash URL: the served base directory plus the app hash route. ctx.base is
    // the absolute site root; strip any trailing "#..." the location may carry.
    const base = (ctx && ctx.base) || "";
    return base + commitRef(short, branch);
  }

  // shareControl renders a minimal copy-link affordance for an item detail: a
  // mono button that copies shareURL() to the clipboard and flashes "Copied".
  // The URL is resolved at click time so a site config that loads after the
  // detail renders is still honored. Clipboard failures fall back silently.
  function shareControl(ctx, short, branch) {
    const btn = el("button", { class: "share-link", type: "button", title: "Copy a link to this item" }, ["Copy link"]);
    btn.addEventListener("click", async () => {
      let url = shareURL(ctx, short, branch);
      // The site config may still be loading; a fresh read makes the page URL
      // available as soon as it lands, without blocking first paint.
      if (ctx && ctx.siteCustomization === undefined && typeof loadSiteCustomization === "function") {
        try { await loadSiteCustomization(ctx); url = shareURL(ctx, short, branch); } catch (e) { /* keep hash URL */ }
      }
      let done = false;
      try { if (navigator && navigator.clipboard && navigator.clipboard.writeText) { await navigator.clipboard.writeText(url); done = true; } } catch (e) { /* clipboard denied */ }
      btn.textContent = done ? "Copied" : url;
      setTimeout(() => { btn.textContent = "Copy link"; }, done ? 1500 : 4000);
    });
    return btn;
  }

  // version; the subject, meta, body (Rendered|Raw), and header fields all repaint
  // for the selected version. The latest version is shown by default.
  function detailView(item, kind, skipKeys, ctx) {
    const skip = skipKeys || [];
    const versions = (item.versions && item.versions.length) ? item.versions
      : [{ commit: item.commit, header: item.header, content: item.content, rawMessage: item.rawMessage, author: item.author, editorName: item.editorName, edited: item.edited, effectiveTime: item.effectiveTime }];
    const sel = { idx: versions.length - 1 };
    const wrap = el("div", { class: "detail" }, []);
    // Back link plus a copy-link/share affordance handing out the item's page URL
    // ({site.url}i/<short>.html) when the site config enables the HTML page layer,
    // else the in-app hash URL. On one row so the share control sits by the back
    // link without disturbing the meta line below.
    wrap.append(el("div", { class: "detail-topbar" }, [
      el("a", { class: "back", href: detailBackHref(ctx, "#/" + kind.tab) }, ["← back"]),
      shareControl(ctx, item.commit.short, kind.branch),
    ]));
    const subjectEl = el("div", { class: "subject" }, []);
    wrap.append(subjectEl);
    const metaSlot = el("div", { class: "detail-meta" }, []);
    wrap.append(metaSlot);
    const bodyPane = el("div", {}, []);
    wrap.append(bodyPane);
    const dl = el("dl", {}, []);
    wrap.append(dl);
    function paint() {
      const v = versions[sel.idx];
      const [subject, body] = subjectBody(v.content);
      subjectEl.textContent = subject || "(untitled)";
      const cb = commitBody(body, v.rawMessage);
      metaSlot.replaceChildren(versionMetaRow(v, kind.branch), cb.modes);
      bodyPane.replaceChildren(cb.pane);
      dl.replaceChildren();
      const h = v.header || {};
      for (const key of Object.keys(h).sort()) {
        if (key === "v" || skip.indexOf(key) !== -1) continue;
        dl.append(el("dt", {}, [key]));
        dl.append(el("dd", {}, [trailerValue(key, h[key])]));
      }
    }
    if (versions.length > 1) wrap.append(versionHistorySection(versions, (i) => { sel.idx = i; paint(); }));
    paint();
    return [wrap];
  }

  async function commitDetail(ctx, hash) {
    const obj = await getObject(ctx, hash);
    if (!obj || obj.type !== "commit") return [el("div", { class: "err" }, ["Commit not found: " + hash])];
    const c = parseCommit(hash, obj.body);
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("a", { class: "back", href: detailBackHref(ctx, "#/timeline") }, ["← back"]));
    wrap.append(el("div", { class: "subject" }, [subjectBody(c.content)[0]]));
    const meta = el("span", { class: "meta" }, [
      commitAuthorEl(c), " · ", timeEl(c.authorTime), " · ",
    ]);
    meta.append(el("span", { class: "hash" }, [c.hash]));
    const cbody = subjectBody(c.content)[1];
    const cb = commitBody(cbody, c.rawMessage);
    wrap.append(el("div", { class: "detail-meta" }, [meta, cb.modes]));
    wrap.append(cb.pane);
    if (c.gitmsg) {
      const dl = el("dl", {}, []);
      for (const key of Object.keys(c.gitmsg).sort()) {
        dl.append(el("dt", {}, [key]));
        dl.append(el("dd", {}, [trailerValue(key, c.gitmsg[key])]));
      }
      wrap.append(dl);
    }
    wrap.append(await commitChangesSection(ctx, c));
    return [wrap];
  }

  // fileRef builds a workspace-relative gitmsg file ref fragment.
  function fileRef(path, branch, line, lineEnd) {
    let s = "#file:" + path + "@" + (branch || "");
    if (line) s += ":L" + line + (lineEnd ? "-" + lineEnd : "");
    return s;
  }

  // ---- Object URLs for in-bucket images (revoked on route change) ----

  // Image extensions this reader can display, mapped to their MIME type. SVG
  // rides through <img>, so any embedded script stays inert.
  const IMG_MIME = {
    png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif",
    webp: "image/webp", svg: "image/svg+xml",
  };
  function imageExt(path) {
    const d = (path || "").lastIndexOf(".");
    const ext = d >= 0 ? path.slice(d + 1).toLowerCase() : "";
    return IMG_MIME[ext] ? ext : null;
  }
  function imageMime(path) { const e = imageExt(path); return e ? IMG_MIME[e] : null; }

  // Object URLs built for the current view; revoked wholesale on the next
  // route() so image blobs do not leak across navigations.
  let liveObjectUrls = [];
  function trackObjectUrl(u) { liveObjectUrls.push(u); }
  function revokeObjectUrls() {
    for (const u of liveObjectUrls) { try { URL.revokeObjectURL(u); } catch (e) { /* noop */ } }
    liveObjectUrls = [];
  }

  // joinPath resolves a relative markdown/HTML path against the directory the
  // document lives in, honoring ./, ../, and a leading / (repo root).
  function joinPath(dir, rel) {
    rel = (rel || "").replace(/^\.\//, "");
    let parts = (dir ? dir.split("/") : []).filter(Boolean);
    if (rel.startsWith("/")) { parts = []; rel = rel.slice(1); }
    for (const seg of rel.split("/")) {
      if (seg === "" || seg === ".") continue;
      if (seg === "..") parts.pop();
      else parts.push(seg);
    }
    return parts.join("/");
  }

  const BLOB_CAP = 1048576;
  // Images get a higher cap than text (8 MiB vs 1 MiB): a browser stream-renders
  // a multi-MB image cheaply, and README media (demo GIFs, screenshots) routinely
  // exceed 1 MiB — the text cap silently dropped them. Text stays at BLOB_CAP
  // (large text truncates/skip-diffs, a different economics).
  const IMG_BLOB_CAP = 8388608;

  // blobObjectUrl fetches an in-bucket blob and wraps it in a same-origin object
  // URL with an extension-derived MIME, capped at IMG_BLOB_CAP. Returns null when
  // the path is missing, not an image, or over the cap.
  async function blobObjectUrl(ctx, path, branch) {
    const tip = await refTip(ctx, "refs/heads/" + branch);
    if (!tip) return null;
    const node = await resolvePath(ctx, tip, path);
    if (!node || node.type !== "blob") return null;
    const mime = imageMime(path);
    if (!mime) return null;
    const obj = await getObject(ctx, node.sha);
    if (!obj || obj.body.length > IMG_BLOB_CAP) return null;
    const u = URL.createObjectURL(new Blob([obj.body], { type: mime }));
    trackObjectUrl(u);
    return u;
  }

  // bytesObjectUrl wraps already-inflated blob bytes in an object URL for the
  // standalone image blob view (bytes are in hand, no extra fetch).
  function bytesObjectUrl(bytes, path) {
    const mime = imageMime(path);
    if (!mime || bytes.length > IMG_BLOB_CAP) return null;
    const u = URL.createObjectURL(new Blob([bytes], { type: mime }));
    trackObjectUrl(u);
    return u;
  }

  // resolveImages resolves the relative <img data-gs-src> markers left by the
  // sanitizer / markdown image renderer into in-bucket object URLs. Absolute
  // https images already carry their src. Fire-and-forget after render.
  async function resolveImages(container, ctx) {
    if (!ctx || !container || !container.querySelectorAll) return;
    for (const img of Array.from(container.querySelectorAll("img[data-gs-src]"))) {
      const src = img.getAttribute("data-gs-src");
      const branch = img.getAttribute("data-gs-branch") || "";
      const dir = img.getAttribute("data-gs-dir") || "";
      img.removeAttribute("data-gs-src");
      try { const u = await blobObjectUrl(ctx, joinPath(dir, src), branch); if (u) img.setAttribute("src", u); }
      catch (e) { /* leave alt text */ }
    }
  }

  // ---- Sanitizer: inert-parse then whitelist-rebuild a clean DOM tree ----

  // The whitelist. Allowed elements are rebuilt clean; DROP tags (and their
  // subtrees) vanish; picture and any other non-whitelisted tag are unwrapped to
  // their children; <source> is dropped so <picture> degrades to its <img>.
  const SANITIZE_TAGS = new Set(["div", "span", "p", "br", "hr", "a", "img", "b", "strong", "i", "em", "code", "pre", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "colgroup", "col", "details", "summary", "center", "sup", "sub", "kbd", "del", "s", "strike", "blockquote", "mark"]);
  const SANITIZE_DROP = new Set(["script", "style", "iframe", "object", "embed", "link", "meta", "noscript", "template", "svg", "math", "form", "input", "button", "textarea", "select", "title", "head", "base", "frame", "frameset", "applet"]);
  const SANITIZE_ATTRS = new Set(["align", "alt", "title", "width", "height", "src", "href", "open"]);

  // hrefOk keeps the today's-gate href set (absolute web/mailto, in-page and
  // root-relative); bare-relative hrefs go through relativeHref instead.
  function hrefOk(v) { return /^(https?:|mailto:|#|\/)/i.test(v || ""); }

  // relativeHref resolves a bare-relative markdown/HTML href (e.g. a README's
  // specs/GITMSG.md#2-lists link) to the in-site file route, against the same
  // { branch, dir } base the relative-image resolver uses. Schemes,
  // protocol-relative, in-page, and root-relative hrefs are not its job
  // (hrefOk gates those). A fragment rides along as a heading-anchor suffix
  // (:slug); queries are dropped. Returns "" when not resolvable.
  function relativeHref(raw, mdctx) {
    if (!raw || !mdctx || !mdctx.branch) return "";
    if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(raw) || raw.startsWith("//") || raw.startsWith("#") || raw.startsWith("/")) return "";
    const hashAt = raw.indexOf("#");
    const frag = hashAt >= 0 ? raw.slice(hashAt + 1) : "";
    const path = (hashAt >= 0 ? raw.slice(0, hashAt) : raw).split("?")[0];
    if (!path) return "";
    const slug = frag ? mdSlug(frag) : "";
    return fileRef(joinPath(mdctx.dir || "", path), mdctx.branch) + (slug ? ":" + slug : "");
  }

  // applyImgSrc gates an image source: an absolute https src is kept verbatim
  // (GitHub parity for badges); a bucket-relative path becomes a data-gs-src
  // marker resolveImages turns into an object URL; every other scheme
  // (http:, data:, javascript:, protocol-relative) is dropped.
  function applyImgSrc(img, rawSrc, alt, mdctx) {
    if (alt) img.setAttribute("alt", alt);
    if (!rawSrc) return;
    if (/^https:\/\//i.test(rawSrc)) { img.setAttribute("src", rawSrc); return; }
    if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(rawSrc) || rawSrc.startsWith("//")) return;
    if (mdctx && mdctx.ctx) {
      img.setAttribute("data-gs-src", rawSrc);
      img.setAttribute("data-gs-branch", mdctx.branch || "");
      img.setAttribute("data-gs-dir", mdctx.dir || "");
    }
  }

  function nodeChildren(n) { return Array.from((n && n.childNodes) || []); }
  function nodeAttrs(n) { return Array.from((n && n.attributes) || []); }

  // sanitizeChildren walks an inert node's children and appends a clean, rebuilt
  // copy to `out`. Only whitelisted tags and attributes survive; event handlers
  // and style attributes never do (they are not in SANITIZE_ATTRS); DROP tags
  // are removed subtree-and-all; unknown tags are unwrapped to their children.
  function sanitizeChildren(parent, out, mdctx) {
    for (const child of nodeChildren(parent)) {
      const nt = child.nodeType;
      if (nt === 3) { out.append(document.createTextNode(child.nodeValue || "")); continue; }
      if (nt !== 1) continue;
      const tag = (child.tagName || "").toLowerCase();
      if (SANITIZE_DROP.has(tag) || tag === "source") continue;
      if (tag === "picture" || !SANITIZE_TAGS.has(tag)) { sanitizeChildren(child, out, mdctx); continue; }
      const clean = document.createElement(tag);
      let rawSrc = null, alt = "";
      for (const a of nodeAttrs(child)) {
        const name = (a.name || "").toLowerCase();
        if (!SANITIZE_ATTRS.has(name)) continue;
        const value = a.value == null ? "" : String(a.value);
        if (name === "href") {
          const href = hrefOk(value) ? value : relativeHref(value, mdctx);
          if (href) clean.setAttribute("href", href);
          continue;
        }
        if (name === "src") { rawSrc = value; continue; }
        if (name === "alt") { alt = value; continue; }
        clean.setAttribute(name, value);
      }
      if (tag === "img") applyImgSrc(clean, rawSrc, alt, mdctx);
      else if (alt) clean.setAttribute("alt", alt);
      sanitizeChildren(child, clean, mdctx);
      out.append(clean);
    }
    return out;
  }

  // sanitizeInert rebuilds a clean node array from an already-parsed inert node
  // (DOM-shim-testable: pass a synthetic inert body and inspect the output).
  function sanitizeInert(inertBody, mdctx) {
    const container = document.createElement("div");
    sanitizeChildren(inertBody, container, mdctx);
    return Array.from(container.childNodes || []);
  }

  // sanitizeHtml is the single HTML gate: parse untrusted HTML into an INERT
  // document (never attached, so nothing executes), then rebuild a clean node
  // array against the whitelist. Raw HTML never reaches innerHTML of the live
  // page.
  function sanitizeHtml(html, mdctx) {
    let body;
    try { body = new DOMParser().parseFromString(String(html), "text/html").body; }
    catch (e) { return [document.createTextNode(String(html))]; }
    return sanitizeInert(body, mdctx);
  }

  // makeImage builds an <img> for a markdown ![alt](src), gated like a sanitized
  // one (absolute https kept, relative deferred to resolveImages).
  function makeImage(src, alt, mdctx) {
    const img = el("img", {}, []);
    applyImgSrc(img, src, alt || "", mdctx);
    return img;
  }

  // ---- Fullscreen overlay (pure presentation, no fetches) ----

  // openFullscreen shows `node` in a full-viewport overlay with body scroll
  // locked; Escape or the close button exits. By default it shows a static clone
  // (right for a code blob / one diff file / a fenced block). With opts.live it
  // moves the real node into the overlay and restores it to its original spot on
  // close, so interactive content (the whole changes section: lazy per-file
  // expands backed by the shared model cache) stays live inside the overlay.
  function openFullscreen(node, opts) {
    if (!node) return;
    opts = opts || {};
    let inner, restore = null;
    if (opts.live && node.parentNode) {
      const parent = node.parentNode, next = node.nextSibling;
      inner = node;
      restore = () => { if (next && next.parentNode === parent) parent.insertBefore(node, next); else parent.appendChild(node); };
    } else inner = node.cloneNode ? node.cloneNode(true) : node;
    const content = el("div", { class: "fs-content" }, [inner]);
    const close = el("button", { class: "fs-close circle", type: "button", "aria-label": "Close fullscreen", title: "Close (Esc)" }, ["✕"]);
    const overlay = el("div", { class: "fs-overlay" }, [content, close]);
    function shut() { overlay.remove(); document.body.style.overflow = ""; document.removeEventListener("keydown", onKey); if (restore) restore(); }
    function onKey(ev) { if (ev.key === "Escape") shut(); }
    close.addEventListener("click", shut);
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    document.body.append(overlay);
  }

  // fullscreenBtn returns a small expand button that opens getTarget() fullscreen.
  function fullscreenBtn(getTarget) {
    const b = el("button", { class: "fs-btn", type: "button", "aria-label": "Fullscreen", title: "Fullscreen" }, ["⤢"]);
    b.addEventListener("click", (ev) => { ev.preventDefault(); ev.stopPropagation(); openFullscreen(getTarget()); });
    return b;
  }

  // wrapFullscreen wraps a content node with a positioned expand button.
  function wrapFullscreen(node) {
    return el("div", { class: "fs-wrap" }, [fullscreenBtn(() => node), node]);
  }

  // ---- Markdown rendering (browser only; raw HTML via the sanitizer) ----

  // renderInline turns inline spans into safe DOM nodes. Markdown-native spans
  // build clean elements directly; rawhtml spans flow through the sanitizer.
  function renderInline(spans, mdctx) {
    const out = [];
    for (const s of spans) {
      if (s.type === "code") out.push(el("code", {}, [s.value]));
      else if (s.type === "strong") out.push(el("strong", {}, renderInline(s.spans, mdctx)));
      else if (s.type === "em") out.push(el("em", {}, renderInline(s.spans, mdctx)));
      else if (s.type === "strike") out.push(el("del", {}, renderInline(s.spans, mdctx)));
      else if (s.type === "image") out.push(makeImage(s.src, s.alt, mdctx));
      else if (s.type === "rawhtml") for (const n of sanitizeHtml(s.value, mdctx)) out.push(n);
      else if (s.type === "link") {
        const a = el("a", {}, renderInline(s.spans, mdctx));
        const href = hrefOk(s.href) ? s.href : relativeHref(s.href, mdctx);
        if (href) a.setAttribute("href", href);
        out.push(a);
      } else if (mdctx && mdctx.hardBreaks && s.value.indexOf("\n") >= 0) {
        // GitHub comment semantics: a single newline is a hard <br>, so a plain
        // multi-line message keeps its line structure instead of collapsing.
        const segs = s.value.split("\n");
        segs.forEach((seg, i) => { if (i) out.push(el("br", {}, [])); if (seg) out.push(document.createTextNode(seg)); });
      } else out.push(document.createTextNode(s.value));
    }
    return out;
  }

  // renderMdList renders a (possibly nested, task-aware) list block.
  function renderMdList(block, mdctx) {
    const list = el(block.ordered ? "ol" : "ul", {}, []);
    for (const item of block.items) {
      const li = el("li", item.task != null ? { class: "task" } : {}, []);
      if (item.task != null) {
        const box = el("input", { type: "checkbox", disabled: "" }, []);
        if (item.task) box.setAttribute("checked", "");
        li.append(box);
      }
      for (const n of renderInline(item.spans, mdctx)) li.append(n);
      for (const child of item.children || []) li.append(renderMdList(child, mdctx));
      list.append(li);
    }
    return list;
  }

  // renderMdTable renders a GFM table with per-column alignment.
  function renderMdTable(block, mdctx) {
    const table = el("table", {}, []);
    const htr = el("tr", {}, []);
    block.headers.forEach((cell, idx) => htr.append(el("th", block.aligns[idx] ? { align: block.aligns[idx] } : {}, renderInline(cell, mdctx))));
    table.append(el("thead", {}, [htr]));
    const tbody = el("tbody", {}, []);
    for (const row of block.rows) {
      const tr = el("tr", {}, []);
      row.forEach((cell, idx) => tr.append(el("td", block.aligns[idx] ? { align: block.aligns[idx] } : {}, renderInline(cell, mdctx))));
      tbody.append(tr);
    }
    table.append(tbody);
    return table;
  }

  // mdSlug builds a GitHub-style anchor slug from heading text.
  function mdSlug(s) { return (s || "").toLowerCase().trim().replace(/[^\w\s-]/g, "").replace(/\s+/g, "-"); }

  // spanText flattens inline spans to their plain text (for heading slugs).
  function spanText(spans) {
    let t = "";
    for (const s of spans || []) t += s.value != null ? s.value : spanText(s.spans);
    return t;
  }

  // renderMdBlock renders one markdown-native block into a parent element.
  function renderMdBlock(block, parent, mdctx) {
    if (block.type === "heading") {
      const h = el("h" + block.level, {}, renderInline(block.spans, mdctx));
      // Headings get md- prefixed slug ids (dedup'd per document) so in-page
      // anchors have a scroll target; the prefix keeps them clear of app ids.
      const slug = mdSlug(spanText(block.spans));
      if (slug && mdctx.slugs) {
        let id = slug, n = 1;
        while (mdctx.slugs.has(id)) id = slug + "-" + n++;
        mdctx.slugs.add(id);
        h.setAttribute("id", "md-" + id);
      }
      parent.append(h);
    }
    else if (block.type === "thematic") parent.append(el("hr", {}, []));
    else if (block.type === "code") {
      const codeEl = highlightTo(el("code", {}, []), block.text, langForFence(block.lang));
      parent.append(wrapFullscreen(el("pre", { class: "codeblock" }, [codeEl])));
    } else if (block.type === "list") parent.append(renderMdList(block, mdctx));
    else if (block.type === "table") parent.append(renderMdTable(block, mdctx));
    else if (block.type === "blockquote") {
      const bq = el("blockquote", {}, []);
      renderBlocksInto(bq, block.blocks, mdctx);
      parent.append(bq);
    } else parent.append(el("p", {}, renderInline(block.spans, mdctx)));
  }

  // renderBlocksInto renders a block list into a parent, honoring raw-HTML
  // wrapper markers: htmlopen pushes a sanitized container that following blocks
  // (markdown or HTML) nest into, so a <div align="center"> wrapping markdown
  // renders as GitHub does. The sanitizer is the only path raw HTML takes.
  function renderBlocksInto(parent, blocks, mdctx) {
    const stack = [{ node: parent, tag: null }];
    const cur = () => stack[stack.length - 1].node;
    for (const block of blocks) {
      if (block.type === "htmlopen") {
        const nodes = sanitizeHtml(block.open + "</" + block.tag + ">", mdctx);
        const container = nodes.find((n) => typeof n.append === "function");
        if (container) { cur().append(container); stack.push({ node: container, tag: block.tag }); }
        else for (const n of nodes) cur().append(n);
      } else if (block.type === "htmlclose") {
        // Pop through to the nearest matching wrapper, discarding unclosed
        // levels in between (HTML-parser recovery); an unmatched close is ignored.
        for (let d = stack.length - 1; d > 0; d--) {
          if (stack[d].tag === block.tag) { stack.length = d; break; }
        }
      } else if (block.type === "html") { for (const n of sanitizeHtml(block.raw, mdctx)) cur().append(n); }
      else renderMdBlock(block, cur(), mdctx);
    }
  }

  // renderMarkdown builds a sanitized DOM subtree from markdown text. mdctx
  // carries { ctx, branch, dir } so relative images resolve to in-bucket blobs.
  function renderMarkdown(text, mdctx) {
    mdctx = mdctx || {};
    if (!mdctx.slugs) mdctx.slugs = new Set();
    const root = el("div", { class: "markdown" }, []);
    renderBlocksInto(root, parseMarkdown(text), mdctx);
    if (mdctx.ctx) resolveImages(root, mdctx.ctx);
    wireInPageAnchors(root);
    return root;
  }

  // wireInPageAnchors makes plain #fragment links (a document's own TOC) scroll
  // to their md- slugged heading on click instead of re-routing. The URL is
  // updated via pushState (no hashchange, no re-render) so the anchor is
  // shareable: on a file route it becomes the file's heading-anchor form
  // (#file:path@branch:slug), on home the plain #fragment; a direct load of
  // either routes back to the document + scrolls (parseRoute).
  function wireInPageAnchors(root) {
    if (!root.querySelectorAll) return;
    for (const a of Array.from(root.querySelectorAll('a[href^="#"]'))) {
      const frag = (a.getAttribute("href") || "").slice(1);
      if (!/^[A-Za-z0-9][\w.-]*$/.test(frag)) continue;
      if (typeof a.addEventListener !== "function") continue;
      a.addEventListener("click", (e) => {
        e.preventDefault();
        const slug = mdSlug(frag);
        if (typeof history !== "undefined" && history.pushState && typeof location !== "undefined") {
          const cur = parseRoute(location.hash);
          const target = cur.type === "file" ? fileRef(cur.path, cur.branch) + ":" + slug : "#" + frag;
          history.pushState(null, "", target);
        }
        const t = document.getElementById("md-" + slug);
        if (t && t.scrollIntoView) t.scrollIntoView();
      });
    }
  }

  // renderCommitBody renders a commit message body (everything after the subject
  // line) through the same Markdown pipeline the file view uses, with GitHub
  // comment hard-break semantics: a single newline becomes a <br>, so a plain
  // multi-line message keeps its line structure (no paragraph collapse) while a
  // message using real Markdown constructs renders them. Relative images are not
  // resolved against the commit tree (no mdctx.ctx), so only absolute https
  // images load here; the sanitizer path is identical to the file view.
  function renderCommitBody(body) {
    return renderMarkdown(body || "", { hardBreaks: true });
  }

  // rawToggle builds the single "Raw" toggle button used everywhere a rendered
  // view has a verbatim counterpart: unpressed shows the rendered pane, pressed
  // (.active, aria-pressed) shows the raw one. One control replaces the old
  // Rendered|Raw button pair; propagation stops so card navigation never fires.
  function rawToggle(showRendered, showRaw, initialRaw) {
    const btn = el("button", { class: "view-toggle", type: "button", "aria-pressed": "false", title: "Toggle raw view" }, ["Raw"]);
    let raw = !!initialRaw;
    const apply = () => {
      btn.classList.toggle("active", raw);
      btn.setAttribute("aria-pressed", raw ? "true" : "false");
      if (raw) showRaw(); else showRendered();
    };
    btn.addEventListener("click", (ev) => { ev.stopPropagation(); raw = !raw; apply(); });
    apply();
    return btn;
  }

  // commitBody builds a detail-page commit body with a Raw toggle, returning
  // { modes, pane } so the caller can place the small toggle on the author/time
  // meta line (like the blob-head pattern) and the pane below it. Rendered is
  // the markdown body (subject already shown above); Raw is the FULL VERBATIM
  // commit message — subject + body + the GitMsg trailer block, exactly as
  // committed — the protocol-level truth, monospace pre-wrap (mirrors the TUI's
  // raw view mode). Thread comments keep the plain rendered body, no toggle.
  function commitBody(body, rawMessage) {
    const pane = el("div", {}, []);
    const btn = rawToggle(
      () => pane.replaceChildren(renderCommitBody(body)),
      () => pane.replaceChildren(el("div", { class: "body raw-body" }, [rawMessage || ""])));
    const modes = el("div", { class: "view-modes body-modes" }, [btn]);
    return { modes, pane };
  }

  // breadcrumb renders a path as clickable segments linking into the tree (the
  // root plus one link per path segment). No leading file-type icon.
  function breadcrumb(path, branch) {
    const row = el("div", { class: "breadcrumb mono" }, []);
    const rootA = el("a", { href: fileRef("", branch) }, [branch || "root"]);
    row.append(rootA);
    const parts = (path || "").split("/").filter(Boolean);
    let acc = "";
    for (const part of parts) {
      acc = acc ? acc + "/" + part : part;
      row.append(document.createTextNode(" / "));
      row.append(el("a", { href: fileRef(acc, branch) }, [part]));
    }
    return row;
  }

  // treeIcon builds a tree-row icon for an entry (or the ".." parent), falling
  // back to the text glyph when the icon set is absent. An expanded directory
  // flips to the folder-open icon.
  function treeIcon(entry, open) {
    let key, glyph;
    if (!entry) { key = "folder"; glyph = "📁"; }
    else if (entry.type === "tree") { key = open ? "folder-open" : "folder"; glyph = open ? "📂" : "📁"; }
    else if (entry.type === "commit") { key = "git"; glyph = "📁"; }
    else { key = entry.mode === "120000" ? "symlink" : iconName(entry.name); glyph = "📄"; }
    return iconEl(key, "tree-icon") || el("span", { class: "tree-icon" }, [glyph]);
  }

  // TREE_ENTRY_CAP bounds how many entries a single directory level renders (the
  // first N, then an "M more not shown" notice), matching the other display caps
  // (WALK_CAP, DIFF_FILE_CAP). Fan-out is user-driven: expanding a directory is
  // exactly one tree-object GET, cached in ctx.objects, so re-collapse/re-expand
  // and full re-renders reuse it with no new fetch.
  const TREE_ENTRY_CAP = 200;

  // TREE_SEARCH_CAP bounds the one-time full-tree walk the in-place search does:
  // it recurses every directory under the current root exactly once (one tree
  // GET each, cached in ctx.objects), stopping at this many total entries and
  // surfacing a "search truncated" notice if hit. Worst-case fan-out for a
  // search is therefore one GET per directory in the tree, once.
  const TREE_SEARCH_CAP = 3000;

  // highlightName splits a name around case-insensitive occurrences of a query,
  // wrapping each match in a <mark> so results show which substring matched.
  function highlightName(name, q) {
    if (!q) return [document.createTextNode(name)];
    const lower = name.toLowerCase();
    const out = [];
    let i = 0;
    while (i < name.length) {
      const idx = lower.indexOf(q, i);
      if (idx < 0) { out.push(document.createTextNode(name.slice(i))); break; }
      if (idx > i) out.push(document.createTextNode(name.slice(i, idx)));
      out.push(el("mark", { class: "tree-mark" }, [name.slice(idx, idx + q.length)]));
      i = idx + q.length;
    }
    return out;
  }

  // sortTreeEntries orders entries directories-first, then alphabetical.
  function sortTreeEntries(entries) {
    const dirs = entries.filter((e) => e.type === "tree").sort((a, b) => a.name.localeCompare(b.name));
    const files = entries.filter((e) => e.type !== "tree").sort((a, b) => a.name.localeCompare(b.name));
    return dirs.concat(files);
  }

  // treeChevron builds a directory-row expand caret from the trusted CHEVRON_SVG
  // path (rotated right when collapsed, down when open, via the .open class), or
  // a plain text caret when DOMParser is unavailable.
  function treeChevron(open) {
    const c = chevronEl("down");
    if (c) { c.className = "gs-icon chevron tree-chevron" + (open ? " open" : ""); return c; }
    return el("span", { class: "tree-chevron" + (open ? " open" : "") }, [open ? "▾" : "▸"]);
  }

  // mountTree renders an in-place, lazily-expanding directory tree into listNode.
  // Directory rows expand their children inline beneath them on click (one cached
  // tree GET per expand), files navigate to the blob route. Expansion state lives
  // in a per-view Set keyed by full path, so collapsing removes a subtree's rows
  // yet a later re-expand (or a full rerender()) reconstructs it from the Set and
  // the object cache with no refetch. Returns { expanded, rerender } for tests.
  function mountTree(ctx, listNode, rootEntries, rootPath, branch, opts) {
    opts = opts || {};
    // A shared expansion Set (opts.expanded) lets the content and sidebar trees
    // track the same open directories; omitted, each mount keeps its own.
    const expanded = opts.expanded || new Set();
    // activePath marks the currently-viewed file/dir with the active tint (used
    // by the sidebar tree so the open file is visible in the hierarchy).
    const activePath = opts.activePath || "";

    // indent reserves a plain, rail-free spacer sized by depth (0.9rem per level)
    // at a row's left, so nesting reads from indentation alone (no vertical guides).
    function indent(depth) {
      const box = el("div", { class: "tree-indent" }, []);
      if (depth > 0) box.style.width = (depth * 0.9) + "rem";
      return box;
    }

    // fileRow is a navigating anchor row (Enter activates natively).
    function fileRow(entry, childPath, depth) {
      const cls = "tree-row" + (childPath === activePath ? " tree-active" : "");
      return el("a", { class: cls, href: fileRef(childPath, branch) }, [
        indent(depth), el("span", { class: "tree-chevron-spacer" }, []),
        treeIcon(entry), el("span", { class: "mono", title: entry.name }, [entry.name]),
      ]);
    }

    // dirNode builds a directory as a { node } whose row has split click targets:
    // the chevron toggles the inline children container (no navigation, no hash
    // change), while the directory NAME is a real anchor that navigates to the
    // rooted #file:<dir>@<branch> route (so middle/modified clicks open a tab).
    // Row keyboard: Enter follows the name (navigate), Space toggles expansion,
    // ArrowRight expands and ArrowLeft collapses (the cheap keyboard subset).
    function dirNode(entry, childPath, depth) {
      const childrenEl = el("div", { class: "tree-children" }, []);
      const dirCls = "tree-row tree-dir" + (childPath === activePath ? " tree-active" : "");
      const row = el("div", { class: dirCls, role: "button", tabindex: "0", "aria-expanded": "false" }, []);
      const node = el("div", { class: "tree-node" }, [row, childrenEl]);
      let open = false;
      const navigate = () => { if (typeof location !== "undefined") location.hash = fileRef(childPath, branch); };
      const paint = () => {
        // No folder icon on dir rows: the rotating chevron already signals a
        // folder. The chevron is its own toggle target; the name navigates.
        const chevBtn = el("span", {
          class: "tree-chevron-btn", role: "button", tabindex: "-1",
          "aria-label": (open ? "Collapse " : "Expand ") + entry.name,
        }, [treeChevron(open)]);
        chevBtn.addEventListener("click", (ev) => {
          ev.preventDefault(); if (ev.stopPropagation) ev.stopPropagation();
          open ? closeDir() : openDir();
        });
        const nameA = el("a", { class: "mono tree-name", href: fileRef(childPath, branch), title: entry.name, tabindex: "-1" }, [entry.name]);
        nameA.addEventListener("click", (ev) => {
          if (ev.metaKey || ev.ctrlKey || ev.shiftKey || (ev.button && ev.button !== 0)) return;
          ev.preventDefault(); if (ev.stopPropagation) ev.stopPropagation();
          navigate();
        });
        row.replaceChildren(indent(depth), chevBtn, nameA);
        row.setAttribute("aria-expanded", open ? "true" : "false");
      };
      async function openDir() {
        if (open) return;
        open = true; expanded.add(childPath); paint();
        const kids = (await getTree(ctx, entry.sha)) || [];
        await renderLevel(childrenEl, kids, childPath, depth + 1);
      }
      function closeDir() {
        if (!open) return;
        open = false; expanded.delete(childPath);
        childrenEl.replaceChildren();
        paint();
      }
      // The whole row is a toggle target: a click on the row's padding/indent
      // (anywhere but the name anchor or chevron, both of which stopPropagation)
      // expands/collapses the directory, so the full-width hover chip is fully
      // active — the name still navigates, the chevron still toggles.
      row.addEventListener("click", (ev) => {
        if (ev.target && ev.target.closest && ev.target.closest("a, .tree-chevron-btn")) return;
        open ? closeDir() : openDir();
      });
      row.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter") { ev.preventDefault(); navigate(); }
        else if (ev.key === " " || ev.key === "Spacebar") { ev.preventDefault(); open ? closeDir() : openDir(); }
        else if (ev.key === "ArrowRight" && !open) { ev.preventDefault(); openDir(); }
        else if (ev.key === "ArrowLeft" && open) { ev.preventDefault(); closeDir(); }
      });
      paint();
      return { node, openDir };
    }

    // renderLevel fills a container with one directory level (capped), then
    // auto-reopens any directory whose path is still in the expansion Set — the
    // recursion that reconstructs a saved tree shape from the cache.
    async function renderLevel(container, entries, parentPath, depth) {
      const all = sortTreeEntries(entries);
      const shown = all.slice(0, TREE_ENTRY_CAP);
      for (const e of shown) {
        const childPath = parentPath ? parentPath + "/" + e.name : e.name;
        if (e.type === "tree") {
          const d = dirNode(e, childPath, depth);
          container.append(d.node);
          if (expanded.has(childPath)) await d.openDir();
        } else {
          container.append(fileRow(e, childPath, depth));
        }
      }
      const extra = all.length - shown.length;
      if (extra > 0) container.append(el("div", { class: "tree-row tree-more mono" }, [
        indent(depth), el("span", { class: "tree-chevron-spacer" }, []),
        el("span", {}, [extra + " more not shown"]),
      ]));
    }

    // rerender rebuilds the whole tree from the root entries, honoring the Set.
    function rerender() { listNode.replaceChildren(); return renderLevel(listNode, rootEntries, rootPath, 0); }

    // ---- In-place search (hide non-matches; expand ancestors of matches) ----

    // fullIndex caches the one-time recursive walk of every directory under the
    // root (bounded at TREE_SEARCH_CAP), so repeat searches refetch nothing.
    let fullIndex = null;

    // buildIndex walks the entire tree once, collecting { path, name, type } for
    // every entry, capped at TREE_SEARCH_CAP. Each directory is one cached tree
    // GET; a second search reuses the cache with zero new GETs.
    async function buildIndex() {
      if (fullIndex) return fullIndex;
      const all = [];
      const state = { truncated: false };
      async function walk(entries, parentPath) {
        for (const e of sortTreeEntries(entries)) {
          if (all.length >= TREE_SEARCH_CAP) { state.truncated = true; return; }
          const p = parentPath ? parentPath + "/" + e.name : e.name;
          all.push({ path: p, name: e.name, type: e.type });
          if (e.type === "tree") {
            const kids = (await getTree(ctx, e.sha)) || [];
            await walk(kids, p);
            if (state.truncated) return;
          }
        }
      }
      await walk(rootEntries, rootPath);
      fullIndex = { all, truncated: state.truncated };
      return fullIndex;
    }

    // renderFiltered draws a static, pre-expanded view: only entries in
    // visiblePaths render, dirs in forceOpenPaths render expanded (revealing the
    // matches beneath), and matched substrings are marked. Non-matching siblings
    // are simply absent. The live `expanded` Set is untouched, so clearing the
    // query restores the pre-search shape via rerender().
    async function renderFiltered(container, entries, parentPath, depth, f) {
      for (const e of sortTreeEntries(entries)) {
        const childPath = parentPath ? parentPath + "/" + e.name : e.name;
        if (!f.visible.has(childPath)) continue;
        const nameSpan = el("span", { class: "mono" }, highlightName(e.name, f.q));
        if (e.type === "tree") {
          const forceOpen = f.forceOpen.has(childPath);
          const row = el("div", { class: "tree-row tree-dir", "aria-expanded": forceOpen ? "true" : "false" }, [
            indent(depth), treeChevron(forceOpen), nameSpan,
          ]);
          const childrenEl = el("div", { class: "tree-children" }, []);
          container.append(el("div", { class: "tree-node" }, [row, childrenEl]));
          if (forceOpen) {
            const kids = (await getTree(ctx, e.sha)) || [];
            await renderFiltered(childrenEl, kids, childPath, depth + 1, f);
          }
        } else {
          container.append(el("a", { class: "tree-row", href: fileRef(childPath, branch) }, [
            indent(depth), el("span", { class: "tree-chevron-spacer" }, []), treeIcon(e), nameSpan,
          ]));
        }
      }
    }

    // setFilter applies (or clears) an in-place filter. An empty query restores
    // the pre-search tree from the untouched expansion Set. Otherwise it walks
    // the full tree once, keeps every match plus its ancestor chain, auto-expands
    // those ancestors, and hides the rest.
    async function setFilter(query) {
      const q = (query || "").trim().toLowerCase();
      if (!q) return rerender();
      const { all, truncated } = await buildIndex();
      const rootDepth = rootPath ? rootPath.split("/").length : 0;
      const visible = new Set();
      const forceOpen = new Set();
      let matchCount = 0;
      for (const it of all) {
        if (!it.name.toLowerCase().includes(q)) continue;
        matchCount++;
        visible.add(it.path);
        const parts = it.path.split("/");
        for (let n = rootDepth + 1; n < parts.length; n++) {
          const anc = parts.slice(0, n).join("/");
          visible.add(anc);
          forceOpen.add(anc);
        }
      }
      listNode.replaceChildren();
      if (!matchCount) {
        listNode.append(el("div", { class: "empty" }, ["No matches for “" + query.trim() + "”."]));
        return;
      }
      await renderFiltered(listNode, rootEntries, rootPath, 0, { visible, forceOpen, q });
      if (truncated) listNode.append(el("div", { class: "notice tree-truncated" }, [
        "Search truncated at " + TREE_SEARCH_CAP + " entries; refine the query.",
      ]));
    }

    renderLevel(listNode, rootEntries, rootPath, 0);
    return { expanded, rerender, setFilter, buildIndex };
  }

  // lastTreeSearch holds the most recently mounted content-tree search input, so
  // the Code nav magnifier can focus it without a second search box.
  let lastTreeSearch = null;

  // focusTreeSearch focuses the current file-tree search input (if one is mounted
  // and focusable), returning whether it did — used by the Code nav magnifier.
  function focusTreeSearch() {
    if (lastTreeSearch && lastTreeSearch.focus) { lastTreeSearch.focus(); return true; }
    return false;
  }

  // treeView renders the in-place hierarchical tree: a breadcrumb (which carries
  // up-navigation, including "..") above the interactive listing rooted at path.
  function treeView(ctx, entries, path, branch) {
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(breadcrumb(path, branch));
    const listNode = el("div", { class: "tree-list" }, []);
    if (!entries.length) { listNode.append(el("div", { class: "empty" }, ["Empty directory."])); wrap.append(listNode); return [wrap]; }
    const ctrl = mountTree(ctx, listNode, entries, path, branch, { expanded: ctx.treeExpanded });
    wrap.__tree = ctrl;
    // Search input above the tree: debounced (150ms), Escape clears. Filtering
    // hides non-matches and auto-expands ancestors of matches (hide-non-matches
    // mode); clearing restores the pre-search expansion state.
    const input = el("input", { class: "tree-search mono", type: "text", placeholder: "Search files…", "aria-label": "Search files", spellcheck: "false" }, []);
    lastTreeSearch = input;
    let timer = null;
    input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => ctrl.setFilter(input.value), 150); });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape") { ev.preventDefault(); clearTimeout(timer); input.value = ""; ctrl.setFilter(""); }
    });
    wrap.append(el("div", { class: "tree-search-wrap" }, [input]));
    wrap.append(listNode);
    return [wrap];
  }


  // humanSize renders a byte count compactly.
  function humanSize(n) {
    if (n < 1024) return n + " B";
    if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
    return (n / 1048576).toFixed(1) + " MB";
  }

  // blobAnchor holds the last line clicked in the blob view, so a subsequent
  // shift-click extends a #file:...:L<a>-<b> range (either direction). Reset per
  // render to the loaded line so a range extends from an opened permalink.
  let blobAnchor = null;

  // rawBlobPane builds the monospace, line-numbered blob body with whole-file
  // Prism highlighting and line-number anchors (click sets #file:...:L<n>,
  // shift-click extends a range). Rows render with whatever grammar is loaded
  // now, then rebuild in place once the file's lazy-loaded grammar arrives
  // (progressive enhancement; a missing grammar stays plain). Returns
  // { code, firstHl }.
  function rawBlobPane(textStr, path, branch, line, lineEnd) {
    const from = line || 0, to = lineEnd || line || 0;
    const lang = langForPath(path);
    blobAnchor = line || null;
    const code = el("div", { class: "blob" }, []);
    let firstHl = null;
    // build fills `code` with one row per source line, highlighting with the
    // grammars loaded at call time. Re-runnable so a later grammar load upgrades
    // the whole pane; firstHl tracks the first highlighted-range row for scroll.
    const build = () => {
      code.replaceChildren();
      firstHl = null;
      highlightLines(textStr, lang).forEach((segs, idx) => {
        const n = idx + 1;
        const hl = n >= from && n <= to && from > 0;
        const row = el("div", { class: "blob-row" + (hl ? " hl" : "") }, []);
        const num = el("a", { class: "ln mono", href: fileRef(path, branch, n) }, [String(n)]);
        num.addEventListener("click", (ev) => {
          ev.preventDefault();
          let lo = n, hi = n;
          if (ev.shiftKey && blobAnchor) { lo = Math.min(blobAnchor, n); hi = Math.max(blobAnchor, n); }
          else blobAnchor = n;
          location.hash = fileRef(path, branch, lo, hi === lo ? null : hi);
        });
        row.append(num);
        row.append(appendSegments(el("span", { class: "lc mono" }, []), segs));
        code.append(row);
        if (hl && !firstHl) firstHl = row;
      });
    };
    build();
    const P = getPrism();
    if (P && lang && !(P.languages && P.languages[lang]) && !BASE_GRAMMARS[lang]) {
      ensureGrammar(lang).then((ok) => { if (ok) build(); });
    }
    return { code, firstHl };
  }

  // blobView renders a file. A known-image extension displays the blob as an
  // <img> (object URL) BEFORE the NUL-sniff, so images do not fall into the
  // binary path. Otherwise binary blobs get a note, large blobs truncate, and
  // text renders monospace with a fullscreen affordance. A .md file gets a
  // Rendered|Raw toggle (Rendered default; Raw is the line-numbered view, so
  // line permalinks apply there).
  function blobView(bytes, path, branch, line, lineEnd, ctx) {
    const wrap = el("div", { class: "detail" }, []);
    const blobMeta = el("div", { class: "meta blob-meta" }, [humanSize(bytes.length)]);
    const head = el("div", { class: "blob-head" }, [breadcrumb(path, branch), blobMeta]);
    wrap.append(head);
    if (imageExt(path)) {
      const u = bytesObjectUrl(bytes, path);
      if (u) wrap.append(el("div", { class: "blob-img" }, [el("img", { src: u, alt: path }, [])]));
      else wrap.append(el("div", { class: "notice" }, ["Image too large to display (over " + humanSize(IMG_BLOB_CAP) + ")."]));
      return [wrap];
    }
    if (isBinary(bytes)) {
      wrap.append(el("div", { class: "empty" }, ["Binary file not shown."]));
      return [wrap];
    }
    let slice = bytes, truncated = false;
    if (bytes.length > BLOB_CAP) { slice = bytes.subarray(0, BLOB_CAP); truncated = true; }
    const textStr = new TextDecoder().decode(slice);
    if (truncated) wrap.append(el("div", { class: "notice" }, ["Large file truncated to the first " + humanSize(BLOB_CAP) + "."]));
    const renderRaw = () => {
      const raw = rawBlobPane(textStr, path, branch, line, lineEnd);
      if (raw.firstHl) setTimeout(() => raw.firstHl.scrollIntoView({ block: "center" }), 0);
      return wrapFullscreen(raw.code);
    };
    if (/\.(md|markdown)$/i.test(path)) {
      const dir = path.indexOf("/") >= 0 ? path.slice(0, path.lastIndexOf("/")) : "";
      const pane = el("div", {}, []);
      const btn = rawToggle(
        () => pane.replaceChildren(renderMarkdown(textStr, { ctx, branch, dir })),
        () => pane.replaceChildren(renderRaw()),
        !!line);
      head.append(el("div", { class: "view-modes" }, [btn]));
      wrap.append(pane);
      return [wrap];
    }
    wrap.append(renderRaw());
    return [wrap];
  }

  // ---- Diff rendering (browser only) ----

  const DIFF_FILE_CAP = 100;

  // getDiffMode / setDiffMode persist the unified|split view mode across the
  // whole reader in localStorage['diffview']; unified is the default.
  function getDiffMode() {
    try { return localStorage.getItem("diffview") === "split" ? "split" : "unified"; } catch { return "unified"; }
  }
  function setDiffMode(m) { try { localStorage.setItem("diffview", m); } catch { /* private mode */ } }

  function diffStatusLabel(s) { return s === "added" ? "A" : s === "deleted" ? "D" : "M"; }

  function hunkHeadText(h) {
    return "@@ -" + h.oldStart + "," + h.oldCount + " +" + h.newStart + "," + h.newCount + " @@";
  }

  // pairIntra pairs adjacent del/add runs within a hunk's lines index-wise and
  // returns a Map from each paired line object to its intra-line split
  // ({ prefix, mid, suffix }) for word-level highlighting. Lines with no
  // opposite pair, or pairs intraLine skips (identical / >500 chars) or whose
  // side has no differing middle, are absent from the map.
  function pairIntra(lines) {
    const map = new Map();
    let i = 0;
    while (i < lines.length) {
      if (lines[i].op !== "del") { i++; continue; }
      const dels = []; while (i < lines.length && lines[i].op === "del") { dels.push(lines[i]); i++; }
      const adds = []; while (i < lines.length && lines[i].op === "add") { adds.push(lines[i]); i++; }
      const m = Math.min(dels.length, adds.length);
      for (let j = 0; j < m; j++) {
        const r = intraLine(dels[j].line, adds[j].line);
        if (!r) continue;
        if (r.delMid) map.set(dels[j], { prefix: r.prefix, mid: r.delMid, suffix: r.suffix });
        if (r.addMid) map.set(adds[j], { prefix: r.prefix, mid: r.addMid, suffix: r.suffix });
      }
    }
    return map;
  }

  // renderDiffText fills a container for one diff line: Prism syntax highlight,
  // and when an intra-line split is present the differing middle is wrapped in a
  // .dw (del) / .aw (add) mark for word-level emphasis over the base row tint.
  function renderDiffText(container, lineText, lang, intra, markCls) {
    if (!intra) return highlightTo(container, lineText, lang);
    highlightTo(container, intra.prefix, lang);
    container.append(highlightTo(el("mark", { class: markCls }, []), intra.mid, lang));
    highlightTo(container, intra.suffix, lang);
    return container;
  }

  // hunkSeparator builds the collapsed "N unchanged lines" divider shown before
  // a hunk. Clicking (or Enter/Space) reveals the skipped context lines, which
  // are already in memory (zero new fetches), by replacing the divider with the
  // rows rowsFor builds.
  function hunkSeparator(skipped, rowsFor) {
    const n = skipped.length;
    const row = el("div", { class: "diff-expand mono", role: "button", tabindex: "0" }, [
      el("span", { class: "diff-expand-icon" }, ["↕"]),
      el("span", {}, [n + (n === 1 ? " unchanged line" : " unchanged lines")]),
    ]);
    const reveal = () => { row.replaceWith.apply(row, rowsFor(skipped)); };
    row.addEventListener("click", reveal);
    row.addEventListener("keydown", (ev) => { if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); reveal(); } });
    return row;
  }

  // unifiedRow builds one unified-diff line row (also reused to reveal skipped
  // context, all rendered as eq/ctx lines).
  function unifiedRow(l, lang, intra) {
    const cls = l.op === "add" ? "add" : l.op === "del" ? "del" : "ctx";
    const text = renderDiffText(el("span", { class: "dl-text mono" }, []), l.line, lang, intra, l.op === "del" ? "dw" : "aw");
    return el("div", { class: "diff-line " + cls }, [
      el("span", { class: "dl-num mono" }, [l.oldN ? String(l.oldN) : ""]),
      el("span", { class: "dl-num mono" }, [l.newN ? String(l.newN) : ""]),
      el("span", { class: "dl-sign mono" }, [l.op === "add" ? "+" : l.op === "del" ? "-" : " "]),
      text,
    ]);
  }

  // appendLineFeedback appends inline PR-feedback card rows after a diff line row
  // when fbCtx.byKey holds feedback anchored to that line (matched by the line's
  // new-side then old-side key). Deduped so a feedback attaching to a context
  // line's shared keys renders once.
  function appendLineFeedback(box, l, fbCtx) {
    if (!fbCtx || !fbCtx.byKey) return;
    const seen = new Set();
    for (const k of hunkLineKeys(l)) {
      const list = fbCtx.byKey.get(k);
      if (!list) continue;
      for (const fb of list) { if (seen.has(fb)) continue; seen.add(fb); box.append(feedbackRow(fb)); }
    }
  }

  // renderHunksUnified renders hunks as a single-column unified diff, with a
  // collapsed-context divider before each hunk that has skipped lines. When
  // fbCtx is passed, inline feedback cards render beneath their anchored lines.
  function renderHunksUnified(hunks, lang, fbCtx) {
    const box = el("div", { class: "diff-body unified" }, []);
    const ctxRows = (lines) => lines.map((l) => unifiedRow(l, lang, null));
    for (const h of hunks) {
      if (h.skipped && h.skipped.length) box.append(hunkSeparator(h.skipped, ctxRows));
      box.append(el("div", { class: "diff-hunk-head mono" }, [hunkHeadText(h)]));
      const intra = pairIntra(h.lines);
      for (const l of h.lines) { box.append(unifiedRow(l, lang, intra.get(l))); appendLineFeedback(box, l, fbCtx); }
    }
    return box;
  }

  // splitRows pairs a hunk's lines into two-column rows: equal lines fill both
  // sides; a run of deletes lines up against the following run of adds, extras
  // spilling to one side only.
  function splitRows(lines) {
    const rows = [];
    let i = 0;
    while (i < lines.length) {
      if (lines[i].op === "eq") { rows.push({ left: lines[i], right: lines[i], kind: "eq" }); i++; continue; }
      const dels = [], adds = [];
      while (i < lines.length && lines[i].op === "del") { dels.push(lines[i]); i++; }
      while (i < lines.length && lines[i].op === "add") { adds.push(lines[i]); i++; }
      const m = Math.max(dels.length, adds.length);
      for (let j = 0; j < m; j++) rows.push({ left: dels[j] || null, right: adds[j] || null, kind: "chg" });
    }
    return rows;
  }

  // splitCells emits the four grid cells (leftNum, leftCode, rightNum, rightCode)
  // for one split row into `into`. A single grid over the whole hunk body keeps
  // the four columns aligned and each row's two sides height-locked when one
  // wraps. intra maps a line object to its word-level split.
  function splitCells(into, r, lang, intra) {
    const cell = (entry, side, markCls) => {
      const sideCls = side === "left" ? "ds-left" : "ds-right";
      if (!entry) {
        into.append(el("span", { class: "ds-num ds-empty mono " + sideCls }, []));
        into.append(el("span", { class: "ds-code ds-empty mono " + sideCls }, []));
        return;
      }
      const cls = entry.op === "add" ? "add" : entry.op === "del" ? "del" : "ctx";
      const num = side === "left" ? entry.oldN : entry.newN;
      into.append(el("span", { class: "ds-num mono " + sideCls + " " + cls }, [num ? String(num) : ""]));
      into.append(renderDiffText(el("span", { class: "ds-code mono " + sideCls + " " + cls }, []), entry.line, lang, intra.get(entry), markCls));
    };
    cell(r.left, "left", "dw");
    cell(r.right, "right", "aw");
  }

  // renderHunksSplit renders hunks as one CSS grid (num/code/num/code), old on
  // the left and new on the right, with hunk heads and collapsed-context
  // dividers spanning all four columns. When fbCtx is passed, inline feedback
  // cards render (full-width) after their anchored row.
  function renderHunksSplit(hunks, lang, fbCtx) {
    const box = el("div", { class: "diff-body split" }, []);
    const ctxRows = (lines) => {
      const frag = [];
      for (const l of lines) { const g = el("div", { class: "ds-contents" }, []); splitCells(g, { left: l, right: l, kind: "eq" }, lang, new Map()); frag.push(g); }
      return frag;
    };
    for (const h of hunks) {
      if (h.skipped && h.skipped.length) box.append(hunkSeparator(h.skipped, ctxRows));
      box.append(el("div", { class: "diff-hunk-head ds-full mono" }, [hunkHeadText(h)]));
      const intra = pairIntra(h.lines);
      for (const r of splitRows(h.lines)) {
        const g = el("div", { class: "ds-contents" }, []); splitCells(g, r, lang, intra); box.append(g);
        if (r.right) appendLineFeedback(box, r.right, fbCtx);
        if (r.left && r.left !== r.right) appendLineFeedback(box, r.left, fbCtx);
      }
    }
    return box;
  }

  // ---- PR review feedback (inline comments on the diff) ----

  // suggestionBlock renders a suggestion feedback body as a mono code panel with
  // an "applies to L<n>" note derived from the feedback's line ref (range aware).
  function suggestionBlock(fb) {
    const h = fb.header || {};
    const line = h["new-line"] || h["old-line"] || "";
    const end = h["new-line-end"] || h["old-line-end"] || "";
    const label = line ? ("applies to L" + line + (end && end !== line ? "-" + end : "")) : "suggestion";
    const box = el("div", { class: "suggestion" }, []);
    box.append(el("div", { class: "suggestion-head mono" }, [label]));
    box.append(el("pre", { class: "codeblock suggestion-code" }, [el("code", {}, [suggestionBody(fb.content)])]));
    return box;
  }

  // feedbackCard renders one PR feedback: a verdict icon (✓ approved / ✗ changes
  // requested / ↩ comment), the effective author + relative time, and the body
  // (suggestion bodies as a code panel, otherwise the markdown comment body).
  function feedbackCard(fb) {
    const h = fb.header || {};
    const state = h["review-state"];
    const icon = state === "approved" ? "✓" : state === "changes-requested" ? "✗" : "↩";
    const card = el("div", { class: "fb-card" + (state ? " fb-" + state : "") }, []);
    const when = fb.effectiveTime || (fb.commit && fb.commit.authorTime);
    card.append(el("div", { class: "fb-head" }, [
      el("span", { class: "fb-icon" }, [icon]), " ",
      el("span", { class: "fb-author" }, [authorEl(fb.author || "unknown", effectiveAuthorEmail(fb.commit, fb.header))]),
      el("span", { class: "meta" }, [" · ", timeEl(when)]),
    ]));
    if (h.suggestion === "true") card.append(suggestionBlock(fb));
    else if (fb.content) card.append(renderCommitBody(fb.content));
    else card.append(el("div", { class: "body" }, ["(no content)"]));
    return card;
  }

  // feedbackRow wraps a feedback card as a full-width diff row (spans all split
  // columns; flows inline in unified mode).
  function feedbackRow(fb) { return el("div", { class: "diff-feedback" }, [feedbackCard(fb)]); }

  // offscreenBlock renders feedback whose anchored line is not present in the
  // rendered hunks (outdated or context-collapsed), each with its line ref.
  function offscreenBlock(fbList) {
    const box = el("div", { class: "fb-offscreen" }, []);
    box.append(el("div", { class: "fb-offscreen-head mono" }, ["Comments not on visible lines"]));
    for (const fb of fbList) {
      const h = fb.header || {};
      const line = h["new-line"] || h["old-line"] || "?";
      box.append(el("div", { class: "fb-offscreen-item" }, [el("span", { class: "meta" }, ["L" + line + " · "]), feedbackCard(fb)]));
    }
    return box;
  }

  // diffSection renders a changed-file list (capped at DIFF_FILE_CAP) with a
  // unified|split view toggle above it. Each file is collapsible; its blob pair
  // is fetched and diffed lazily on first expand, and auto-expanded when the
  // list is small (<= 5). The computed per-file model is cached so toggling the
  // view mode re-renders without re-fetching.
  function diffSection(ctx, entries, title, caveats, fileFeedback) {
    fileFeedback = fileFeedback || [];
    const shown = entries.slice(0, DIFF_FILE_CAP);
    const extra = entries.length - shown.length;
    // diffTrees bounded its recursion (over DIFF_FILE_CAP + margin changed paths):
    // the true count is unknown, so the header reads "N+" and the notice says the
    // list was truncated rather than an exact "M more not shown".
    const scanTruncated = !!entries.truncated;
    const countLabel = scanTruncated ? DIFF_FILE_CAP + "+" : String(entries.length);
    const wrap = el("div", { class: "diff-section" }, []);
    const head = el("div", { class: "diff-head" }, [el("span", { class: "subject" }, [title + " (" + countLabel + ")"])]);
    for (const c of caveats || []) head.append(el("span", { class: "chip caveat" }, [c]));
    let mode = getDiffMode();
    // Single mode toggle: the label names the mode a click switches TO, with a
    // swap glyph so it reads as an action, never as the current state.
    const modeBtn = el("button", { class: "diff-btn mode-toggle", type: "button" }, []);
    // Single expand/collapse toggle: the label is recomputed from the actual
    // per-file state after every change (bulk or manual), so it always names an
    // action that does something.
    const expandBtn = el("button", { class: "diff-btn expand-toggle", type: "button" }, []);
    const fsBtn = el("button", { class: "diff-btn", type: "button", title: "Fullscreen changes", "aria-label": "Fullscreen changes" }, ["⤢"]);
    head.append(el("div", { class: "diff-controls" }, [expandBtn, modeBtn, fsBtn]));
    wrap.append(head);
    const files = [];
    function refreshModeBtn() {
      const target = mode === "unified" ? "split" : "unified";
      modeBtn.textContent = "⇄ " + (target === "split" ? "Split" : "Unified");
      modeBtn.setAttribute("title", "Switch to " + target + " view");
      modeBtn.setAttribute("aria-label", "Switch to " + target + " view");
    }
    function refreshExpandBtn() {
      if (!files.length) { expandBtn.style.display = "none"; return; }
      const anyCollapsed = files.some((f) => !f.expanded);
      expandBtn.textContent = anyCollapsed ? "⊞ Expand all" : "⊟ Collapse all";
    }
    function apply() {
      refreshModeBtn();
      for (const f of files) if (f.expanded && f.model) f.renderBody();
    }
    modeBtn.addEventListener("click", () => {
      mode = mode === "unified" ? "split" : "unified";
      setDiffMode(mode);
      apply();
    });
    // Expand all fetches the displayed files' diffs concurrently, bounded by the
    // shared CONCURRENCY limit, reusing each file's cached model (already-open
    // files are skipped). Only the shown (<= DIFF_FILE_CAP) files are touched.
    async function expandAll() {
      const pending = files.filter((f) => !f.expanded);
      let idx = 0;
      const worker = async () => { while (idx < pending.length) { const f = pending[idx++]; await f.expand(); } };
      await Promise.all(Array.from({ length: Math.min(CONCURRENCY, pending.length) || 1 }, worker));
    }
    expandBtn.addEventListener("click", () => {
      if (files.some((f) => !f.expanded)) expandAll();
      else for (const f of files) f.collapse();
    });
    fsBtn.addEventListener("click", (ev) => { ev.preventDefault(); openFullscreen(wrap, { live: true }); });
    const autoExpand = shown.length <= 5;
    for (const entry of shown) {
      const counts = el("span", { class: "diff-counts mono" }, []);
      const body = el("div", { class: "diff-file-body" }, []);
      const fileIcon = iconEl(iconName(entry.path), "diff-file-icon");
      const entryFb = fileFeedback.filter((fb) => fb.header && fb.header.file === entry.path);
      const fhead = el("div", { class: "diff-file-head" }, [
        el("span", { class: "diff-status s-" + entry.status }, [diffStatusLabel(entry.status)]),
        ...(fileIcon ? [fileIcon] : []),
        el("span", { class: "mono diff-path" }, [entry.path]),
        ...(entryFb.length ? [el("span", { class: "chip fb-count" }, [entryFb.length + (entryFb.length === 1 ? " comment" : " comments")])] : []),
        counts,
        fullscreenBtn(() => body),
      ]);
      body.style.display = "none";
      const lang = langForPath(entry.path);
      const f = { expanded: false, model: null };
      f.renderBody = () => {
        if (!f.model) return;
        if (f.model.binary) { body.replaceChildren(el("div", { class: "notice" }, ["Binary file changed."])); return; }
        if (f.model.tooLarge) {
          const anyway = el("button", { class: "load-more", type: "button" }, ["Diff anyway"]);
          anyway.addEventListener("click", async () => {
            anyway.disabled = true; anyway.textContent = "Diffing…";
            f.model = await fileDiff(ctx, entry, true);
            counts.replaceChildren(el("span", { class: "cnt-add" }, ["+" + f.model.adds]), el("span", { class: "cnt-del" }, ["-" + f.model.dels]));
            f.renderBody();
          });
          body.replaceChildren(el("div", { class: "notice" }, ["File too large to diff. ", anyway]));
          return;
        }
        if (!f.model.hunks.length) {
          const kids = [el("div", { class: "empty" }, ["No line changes."])];
          if (entryFb.length) kids.push(offscreenBlock(entryFb));
          body.replaceChildren(...kids);
          return;
        }
        const anchor = entryFb.length ? anchorFeedback(entryFb, f.model.hunks) : { byKey: new Map(), offscreen: [] };
        const diffBody = mode === "split" ? renderHunksSplit(f.model.hunks, lang, { byKey: anchor.byKey }) : renderHunksUnified(f.model.hunks, lang, { byKey: anchor.byKey });
        const kids = [diffBody];
        if (anchor.offscreen.length) kids.push(offscreenBlock(anchor.offscreen));
        body.replaceChildren(...kids);
      };
      f.expand = async () => {
        f.expanded = true; body.style.display = ""; fhead.classList.add("open");
        refreshExpandBtn();
        if (!f.model) {
          body.replaceChildren(el("div", { class: "loading" }, ["Loading diff…"]));
          f.model = await fileDiff(ctx, entry);
          if (f.model.binary) counts.textContent = "binary";
          else if (f.model.tooLarge) counts.textContent = "large";
          else counts.replaceChildren(el("span", { class: "cnt-add" }, ["+" + f.model.adds]), el("span", { class: "cnt-del" }, ["-" + f.model.dels]));
        }
        f.renderBody();
      };
      f.collapse = () => { f.expanded = false; body.style.display = "none"; fhead.classList.remove("open"); refreshExpandBtn(); };
      fhead.addEventListener("click", () => { f.expanded ? f.collapse() : f.expand(); });
      files.push(f);
      wrap.append(el("div", { class: "diff-file" }, [fhead, body]));
      if (autoExpand) f.expand();
    }
    apply();
    refreshExpandBtn();
    if (scanTruncated) wrap.append(el("div", { class: "notice" }, ["Over " + DIFF_FILE_CAP + " files changed; list truncated."]));
    else if (extra > 0) wrap.append(el("div", { class: "notice" }, [extra + " more not shown."]));
    return wrap;
  }

  // resolveTipCommit resolves a PR tip to a full commit sha in this bucket. It
  // resolves the branch ref, prefers the exact recorded short tip (matching the
  // live tip or found in a bounded walk), and otherwise falls back to the live
  // tip flagged non-exact. Foreign or absent branches yield a status the caller
  // renders as "tips not present in this bucket".
  async function resolveTipCommit(ctx, field, short) {
    const { url, name } = parseBranchField(field);
    if (url) return { status: "foreign" };
    if (!name) return { status: "unknown" };
    const live = await refTip(ctx, "refs/heads/" + name);
    if (!live) return { status: "absent" };
    if (short && live.startsWith(short)) return { status: "ok", sha: live, exact: true };
    if (short) {
      // Resolve the recorded tip (which can sit far behind the live tip on a
      // long-running branch) by prefix match over the code items index first —
      // the branch's commits are in the merged code corpus — falling back to a
      // bounded loose walk only when the index can't answer (absent/non-v4, or a
      // sha predating the bootstrap).
      const indexed = await resolveShortShaFromIndex(ctx, short);
      if (indexed) return { status: "ok", sha: indexed, exact: true };
      const commits = await walkHistory(ctx, live, DETAIL_WALK_CAP);
      const hit = commits.find((c) => c.hash.startsWith(short));
      if (hit) return { status: "ok", sha: hit.hash, exact: true };
    }
    return { status: "ok", sha: live, exact: false };
  }

  // resolveMergedRefs resolves a merged PR's merge-base / merge-head short shas
  // to full commit shas present in this bucket. Both are reachable from the PR's
  // base branch: an imported merge stores the merge commit and its first parent
  // (both on the base line), a native merge stores the fork-point base and the
  // head tip (reachable as the merge commit's second parent). Resolves each short
  // sha by prefix match over the code items index (full sha per code commit,
  // draining shards newest-first) — the index removes the ~hundreds of loose GETs
  // the base-branch walk used to cost. Falls back to that bounded walk only for
  // a short that the index can't answer (index absent/non-v4, or the sha predates
  // the bootstrap or sits on a non-indexed object). Null when the base is foreign
  // or absent, or either sha stays unresolved. Ambiguity mirrors the walk: the
  // first (newest-first) prefix match wins, no uniqueness check.
  async function resolveMergedRefs(ctx, baseField, baseShort, headShort) {
    const { url, name } = parseBranchField(baseField);
    if (url || !name) return null;
    const live = await refTip(ctx, "refs/heads/" + name);
    if (!live) return null;
    let commits = null;
    const walk = async () => (commits || (commits = await walkHistory(ctx, live, DETAIL_WALK_CAP)));
    const find = async (short) => {
      if (live.startsWith(short)) return live;
      const indexed = await resolveShortShaFromIndex(ctx, short);
      if (indexed) return indexed;
      return ((await walk()).find((c) => c.hash.startsWith(short)) || {}).hash || null;
    };
    const baseSha = await find(baseShort), headSha = await find(headShort);
    return baseSha && headSha ? { baseSha, headSha } : null;
  }

  // prDiffSection builds the "Files changed" section for a PR. It resolves both
  // tips in this bucket, attempts a bounded merge-base for three-dot semantics,
  // falls back to a raw two-tip diff (with a caveat) when no common ancestor is
  // reachable, and returns a "tips not present" note when either tip is foreign
  // or absent. Returns null when the header lacks tip fields.
  async function prDiffSection(ctx, header, fileFeedback) {
    if (!header) return null;
    // Merged PRs: prefer the durable merge-base..merge-head range (parity with
    // the TUI's resolveMergedDiff). Those commits sit on the base branch even
    // when the head branch is a foreign fork or was deleted after merge, so a
    // merged fork/squash PR still diffs where base-tip/head-tip cannot. Fall
    // through to the tip path when the range can't be resolved in this bucket.
    if ((header.state || "") === "merged" && header["merge-base"] && header["merge-head"]) {
      const m = await resolveMergedRefs(ctx, header.base, header["merge-base"], header["merge-head"]);
      if (m) {
        const baseTree = await commitTree(ctx, m.baseSha);
        const headTree = await commitTree(ctx, m.headSha);
        if (baseTree && headTree) {
          const entries = await diffTrees(ctx, baseTree, headTree);
          return diffSection(ctx, entries, "Files changed", [], fileFeedback);
        }
      }
    }
    if (!header["head-tip"] || !header["base-tip"]) return null;
    const headR = await resolveTipCommit(ctx, header.head, header["head-tip"]);
    const baseR = await resolveTipCommit(ctx, header.base, header["base-tip"]);
    if (headR.status !== "ok" || baseR.status !== "ok") {
      const note = el("div", { class: "diff-section" }, [
        el("div", { class: "diff-head" }, [el("span", { class: "subject" }, ["Files changed"])]),
        el("div", { class: "notice" }, ["The base or head tips are not present in this bucket (a foreign fork or an unfetched branch), so no diff can be shown here."]),
      ]);
      return note;
    }
    const headTree = await commitTree(ctx, headR.sha);
    const baseTree = await commitTree(ctx, baseR.sha);
    if (!headTree || !baseTree) return el("div", { class: "diff-section" }, [el("div", { class: "notice" }, ["Tip commit objects are missing from this bucket."])]);
    const caveats = [];
    // Deep merge-base budget so a deep or stacked PR still gets a true three-dot
    // diff instead of falling back to the raw two-tip diff where reachable.
    const mb = await mergeBase(ctx, headR.sha, baseR.sha, DETAIL_WALK_CAP);
    let leftTree = baseTree;
    if (mb) { leftTree = await commitTree(ctx, mb) || baseTree; }
    else caveats.push("raw two-tip diff");
    if (!headR.exact) caveats.push("head branch advanced");
    if (!baseR.exact && !mb) caveats.push("base branch advanced");
    const entries = await diffTrees(ctx, leftTree, headTree);
    return diffSection(ctx, entries, "Files changed", caveats, fileFeedback);
  }

  // reviewSummarySection renders the PR review strip (GetReviewSummary parity): a
  // count line (approved / changes-requested / pending) with a Ready-to-merge or
  // Changes-requested status chip, per-reviewer chips (latest verdict, else
  // commented), and any verdict/general review feedback bodies. Null when a PR has
  // no review activity or reviewers.
  function reviewSummarySection(summary, verdictFeedback) {
    const hasAny = summary.reviewers.length || summary.approved || summary.changesRequested || summary.pending;
    if (!hasAny) return null;
    const wrap = el("div", { class: "review-summary" }, []);
    wrap.append(el("div", { class: "review-summary-head mono" }, ["Reviews"]));
    const strip = el("div", { class: "review-summary-line" }, [
      el("span", { class: "meta" }, [summary.approved + " approved · " + summary.changesRequested + " changes requested · " + summary.pending + " pending"]),
    ]);
    if (summary.isApproved) strip.append(el("span", { class: "chip state open" }, ["Ready to merge"]));
    else if (summary.isBlocked) strip.append(el("span", { class: "chip state closed" }, ["Changes requested"]));
    wrap.append(strip);
    if (summary.reviewers.length) {
      const chips = el("div", { class: "review-chips" }, []);
      for (const r of summary.reviewers) {
        const icon = r.state === "approved" ? "✓ " : r.state === "changes-requested" ? "✗ " : "↩ ";
        chips.append(el("span", { class: "chip reviewer-chip fb-" + r.state }, [icon + r.name + " · " + r.state]));
      }
      wrap.append(chips);
    }
    for (const fb of verdictFeedback || []) {
      if (!fb.content && !(fb.header && fb.header["review-state"])) continue;
      wrap.append(feedbackCard(fb));
    }
    return wrap;
  }

  // commitChangesSection builds the "Changes" section for a commit: the diff of
  // its tree against its first parent (empty tree for a root commit; first
  // parent labeled for a merge).
  async function commitChangesSection(ctx, commit) {
    let parentTree = null;
    if (commit.parents.length) parentTree = await commitTree(ctx, commit.parents[0]);
    const entries = await diffTrees(ctx, parentTree, commit.tree);
    const caveats = commit.parents.length > 1 ? ["vs first parent"] : [];
    const title = commit.parents.length === 0 ? "Changes (root commit)" : "Changes";
    return diffSection(ctx, entries, title, caveats);
  }

  function setView(nodes) {
    const view = document.getElementById("view");
    view.replaceChildren(...nodes);
  }

  function highlightNav(tab) {
    for (const a of document.querySelectorAll("#nav a[data-nav]")) {
      a.classList.toggle("active", a.getAttribute("data-nav") === tab);
    }
  }

  // comingSoon renders the placeholder for reserved routes (branch/tag/file/
  // list, and the friendly /tree and /code tabs) that are not available yet.
  // It still echoes the parsed target so the ref is legible.
  function comingSoon(r) {
    const labels = { branch: "Branch view", tag: "Tag view", file: "File view", list: "List view" };
    const name = r.type === "reserved" ? (r.what === "code" ? "Code browser" : "Tree browser") : (labels[r.type] || "This view");
    const target = r.name || r.path || r.id || "";
    const text = name + " is not available yet." + (target ? " (" + target + ")" : "");
    return [el("div", { class: "empty" }, [text])];
  }

  // ---- PM rendering (browser only) ----

  const RELEASE_ASSET_KEYS = ["artifacts", "artifact-url", "checksums", "sbom", "signed-by"];
  const ISSUE_STATES = [{ key: "all", label: "All" }, { key: "open", label: "Open" }, { key: "closed", label: "Closed" }];
  const PR_STATES = [{ key: "all", label: "All" }, { key: "open", label: "Open" }, { key: "merged", label: "Merged" }, { key: "closed", label: "Closed" }];
  // In-memory per-tab filter selection (persists across navigations within a
  // session; deliberately not in the fragment, so the route grammar is intact).
  const filterState = { issues: "all", prs: "all" };

  // assetRow renders one artifact/checksums/sbom entry: an external link when a
  // gated href is present, else selectable mono text (git-stored, unlinkable
  // in a static reader).
  function assetRow(name, href, tag) {
    const kids = [el("span", { class: "mono selectable" }, [name])];
    if (tag) kids.push(el("span", { class: "chip" }, [tag]));
    if (href && /^(https?:|\/)/i.test(href)) {
      const a = el("a", { class: "asset-row", href, rel: "noopener" }, kids);
      return a;
    }
    return el("div", { class: "asset-row" }, kids);
  }

  // releaseAssetsSection renders a release's artifacts, checksums, SBOM, and
  // signing key. Returns null when the release carries no asset fields.
  function releaseAssetsSection(header) {
    const a = releaseAssets(header);
    if (!a.artifacts.length && !a.checksums && !a.sbom && !a.signedBy) return null;
    const wrap = el("div", { class: "assets" }, []);
    wrap.append(el("div", { class: "assets-head mono" }, ["Assets"]));
    if (a.artifacts.length) {
      const list = el("div", { class: "asset-list" }, []);
      for (const art of a.artifacts) list.append(assetRow(art.name, art.href));
      wrap.append(list);
    }
    const extra = el("div", { class: "asset-list" }, []);
    if (a.checksums) extra.append(assetRow(a.checksums.name, a.checksums.href, "checksums"));
    if (a.sbom) extra.append(assetRow(a.sbom.name, a.sbom.href, "SBOM"));
    if (extra.children.length) wrap.append(extra);
    if (a.signedBy) wrap.append(el("div", { class: "asset-signed" }, [
      el("span", { class: "meta" }, ["signed-by "]), el("span", { class: "mono selectable" }, [a.signedBy]),
    ]));
    return wrap;
  }

  // embeddedBlock renders the cross-repo context a commit embeds for one
  // reference: a muted "from <repo>" line, the quoted origin excerpt, and the
  // origin author/time — the local view of a thread that cannot be fetched.
  function embeddedBlock(e) {
    const box = el("div", { class: "embedded" }, []);
    box.append(el("div", { class: "embedded-from meta" }, ["from " + e.url]));
    if (e.quoted) box.append(el("div", { class: "embedded-quote" }, [e.quoted]));
    const who = el("div", { class: "meta" }, [authorEl(e.author || "unknown", e.email)]);
    if (e.time) who.append(" · " + e.time);
    box.append(who);
    return box;
  }

  // commentCard renders one thread comment card (content, author/time meta, and
  // any cross-repo embedded context the comment itself carries). Indentation is
  // applied by commentRow, not here. `branch` is the data branch the item's
  // permalink points at (default gitmsg/social, the comment branch). `clamp`
  // puts the content under the ~10-line clamp with the meta line (author/time
  // + Raw toggle) ABOVE it, detail-page style (reply-context ancestors);
  // thread comments stay plain rendered content-first, no chrome.
  function commentCard(item, branch, clamp) {
    const card = el("div", { class: "card comment" }, []);
    const content = item.content ? renderCommitBody(item.content) : el("div", { class: "body" }, ["(no content)"]);
    const meta = metaRow(item, branch || "gitmsg/social");
    prependGlyph(meta, item, "social");
    if (clamp) {
      const pane = el("div", {}, [content]);
      const modes = el("div", { class: "view-modes" }, [rawToggle(
        () => pane.replaceChildren(content),
        () => pane.replaceChildren(el("div", { class: "body raw-body" }, [item.rawMessage || ""])))]);
      card.append(el("div", { class: "detail-meta" }, [meta, modes]), clampNode(pane));
    } else {
      card.append(content, meta);
    }
    for (const e of embeddedRefs(item.commit, item.header)) card.append(embeddedBlock(e));
    return card;
  }

  // commentRow places `depth` rail guides to a comment's left so the reply
  // hierarchy reads as continuous vertical connector lines (the TUI's indent),
  // then the comment card. Depth 0 renders the bare card, no rail.
  function commentRow(item, depth, branch, clamp) {
    if (depth <= 0) return commentCard(item, branch, clamp);
    const rail = el("div", { class: "thread-rail" }, []);
    for (let i = 0; i < depth; i++) rail.append(el("span", { class: "rail-guide" }, []));
    return el("div", { class: "comment-row" }, [rail, commentCard(item, branch, clamp)]);
  }

  // threadSection renders a grouped comment thread as a flat, chronological list
  // whose reply hierarchy is drawn with per-depth rail guides (true nesting up to
  // THREAD_MAX_DEPTH, then indent-capped). Returns null for an empty thread.
  function threadSection(thread) {
    const flat = flattenThread(thread);
    if (!flat.length) return null;
    const wrap = el("div", { class: "thread" }, []);
    wrap.append(el("div", { class: "thread-head mono" }, ["Comments (" + flat.length + ")"]));
    for (const row of flat) wrap.append(commentRow(row.comment, row.depth));
    return wrap;
  }

  // itemThreadSection loads same-repo social comments and groups them under the
  // item. The social walk is deepened to DETAIL_WALK_CAP so comments older than
  // the first window still attach (bounded, and cached on ctx for the session).
  async function itemThreadSection(ctx, item) {
    const social = await loadExtItemsUpTo(ctx, "social", DETAIL_WALK_CAP);
    const comments = social.filter((i) => i.header && i.header.original);
    const thread = groupThread(item.commit.short, comments);
    // The walk seeds comment bodies from the metadata index (body-less); fetch
    // the loose objects for the comments actually shown in this thread.
    await hydrateItems(ctx, flattenThread(thread).map((r) => r.comment));
    return threadSection(thread);
  }

  // quotedFallbackBlock renders the item's own GitMsg-Ref quoted excerpt for a
  // same-repo parent that could not be resolved in-bucket — the embedded block
  // style, with the ref linked so the reader can still try the permalink. Null
  // when the commit carries no excerpt for that ref.
  function quotedFallbackBlock(item, ref) {
    const q = quotedRefFor(item.commit, ref);
    if (!q || !q.quoted) return null;
    const box = el("div", { class: "embedded" }, []);
    const h = refHash(ref);
    const from = el("div", { class: "embedded-from meta" }, ["in reply to "]);
    from.append(h ? el("a", { href: commitRef(h, refBranch(ref)) }, [ref]) : ref);
    box.append(from);
    box.append(clampNode(el("div", { class: "embedded-quote" }, [q.quoted])));
    const who = el("div", { class: "meta" }, [authorEl(q.author || "unknown", q.email)]);
    if (q.time) who.append(" · " + q.time);
    box.append(who);
    return box;
  }

  // replyContextSection renders the same-repo parent context above a reply's
  // permalink: the resolved ancestor chain root-first as thread comment cards
  // (rail-indented like the thread view), preceded — when the deepest reachable
  // ancestor's own parent is unresolvable — by the commit's quoted excerpt for
  // that ref. Null when the item is not a same-repo reply or no context can be
  // shown. Resolution failures (missing object, cap, 403) degrade to the
  // excerpt fallback rather than failing the already-resolved detail view.
  async function replyContextSection(ctx, item, branch) {
    if (!parentRef(item.header)) return null;
    let chain = [], missing = null;
    try {
      const r = await resolveAncestors(ctx, item, branch);
      chain = r.chain; missing = r.missing;
    } catch (e) { missing = parentRef(item.header); }
    const fallback = missing ? quotedFallbackBlock(item, missing) : null;
    if (!chain.length && !fallback) return null;
    // Resolved ancestors come from the metadata index (body-less); fetch bodies.
    await hydrateItems(ctx, chain.map((c) => c.item));
    const wrap = el("div", { class: "thread reply-context" }, []);
    wrap.append(el("div", { class: "thread-head mono" }, ["In reply to"]));
    if (fallback) wrap.append(fallback);
    for (let i = 0; i < chain.length; i++) {
      wrap.append(commentRow(chain[i].item, Math.min(i, THREAD_MAX_DEPTH), chain[i].branch, true));
    }
    return wrap;
  }

  // enrichDetail runs one background enrichment step and appends its section to
  // the already-painted detail. Producer returns a node (or null); place is an
  // optional inserter (default append). A step's own bounded walk (a PR diff's
  // merge-base search, a thread's social walk) thus runs AFTER first paint, never
  // gating it — the timeline-bug fix applied to the detail routes: a stale/absent
  // index degrades to bounded background work, never an eternal "Loading…".
  // Failures (missing object, cap, transient 403/429 on a bounded walk) are
  // swallowed so one slow section never blanks the resolved detail.
  function enrichDetail(root, producer, place) {
    Promise.resolve().then(producer).then((node) => {
      if (node) (place ? place(node) : root.append(node));
    }).catch(() => { /* enrichment is best-effort; the base detail already painted */ });
  }

  // itemDetail resolves the item matching hash — deepening the history walk up to
  // DETAIL_WALK_CAP so an old permalink past the first window still resolves (a
  // "searching history" note shows while it deepens). The commit itself is one
  // loose GET: it paints the base detail (subject, body, meta, header, embedded
  // cross-repo context, release assets) as soon as the item resolves, then
  // enriches progressively in the BACKGROUND — same-repo reply context (parent
  // chain above the item), pm relations/members, the PR review summary and
  // files-changed diff, and the comment thread. Each enrichment carries its own
  // bounded walk, so none of them gates first paint (the class of hang that left
  // a merged PR's detail on "Loading…" behind a 2000-commit merge-base walk).
  async function itemDetail(ctx, hash, branch) {
    const cv = COMMIT_VIEW[branch];
    const onProgress = (visited) => setView([el("div", { class: "loading" }, ["Searching history… (" + visited + " commits scanned)"])]);
    const { item, items } = await findItemDeep(ctx, cv.ext, hash, onProgress);
    if (!item) return [el("div", { class: "err" }, [cv.label + " not found."])];
    const skip = cv.ext === "release" ? RELEASE_ASSET_KEYS : [];
    const nodes = detailView(item, { tab: cv.tab, branch }, skip, ctx);
    const root = nodes[0];
    // Base paint (synchronous, item already hydrated): embedded cross-repo
    // context and release assets cost no walk, so they land with first paint.
    for (const e of embeddedRefs(item.commit, item.header)) root.append(embeddedBlock(e));
    if (cv.ext === "release") {
      const sec = releaseAssetsSection(item.header);
      if (sec) root.append(sec);
    }
    // Same-repo reply context (ancestor chain, or the quoted-excerpt fallback)
    // renders above the item so a bare permalink of a reply still reads as one;
    // its findRefItem walks are bounded but backgrounded off first paint.
    enrichDetail(root, () => replyContextSection(ctx, item, branch),
      (node) => root.insertBefore(node, root.children[1] || null));
    if (cv.ext === "pm") {
      // Milestone/sprint member lists + progress, or an issue's parent/milestone/
      // sprint links and sub-issue list, from the already-resolved pm set.
      enrichDetail(root, async () => {
        const wrap = el("div", {}, []);
        for (const extra of await pmDetailExtras(ctx, item, items)) wrap.append(extra);
        return wrap.childNodes.length ? wrap : null;
      });
    }
    if (branch === "gitmsg/review" && (item.header.type || "") === "pull-request") {
      // Feedback rides gitmsg/review alongside the PR; findItemDeep already walked
      // it (feedback is newer than its PR, so it is in the accumulated set). The
      // review summary and the files-changed diff (whose merge-base / tip search
      // is a bounded but potentially large loose walk) enrich in the background.
      enrichDetail(root, async () => {
        const fb = prFeedback(items, item.commit.short);
        // Feedback records come body-less from the metadata index; fetch their
        // loose objects so verdicts, authors, and comment bodies render.
        await hydrateItems(ctx, fb.all);
        const summary = reviewSummary(fb.all, item.header.reviewers || "");
        return reviewSummarySection(summary, fb.nonFile);
      });
      enrichDetail(root, async () => {
        const file = prFeedback(items, item.commit.short).file;
        // Per-line feedback bodies come body-less from the index; hydrate them so
        // the diff's anchored comments render.
        await hydrateItems(ctx, file);
        return prDiffSection(ctx, item.header, file);
      });
    }
    // Comment thread: the social walk is bounded (DETAIL_WALK_CAP) but can be a
    // large loose walk on an index-absent social branch, so it enriches last.
    enrichDetail(root, () => itemThreadSection(ctx, item));
    return nodes;
  }

  // filteredListView renders a state-filter chip bar (with per-state counts) above
  // a client-side-filtered, client-paginated item list. Counts are exact — over
  // the full `items` (the caller passes the complete metadata set, not a window),
  // so they are correct on first paint. The selection persists in-memory per tab;
  // clicking a chip re-renders in place, and "Load more" reveals the next page
  // from memory (no refetch) so a large list stays light in the DOM.
  function filteredListView(items, cardFn, tab, states, emptyText) {
    const counts = stateCounts(items);
    const PAGE = 100;
    const bar = el("div", { class: "filter-bar" }, []);
    const listBox = el("div", {}, []);
    const outer = el("div", {}, [bar, listBox]);
    const chips = [];
    let moreWrap = null;
    function render() {
      const sel = filterState[tab] || "all";
      const shown = sel === "all" ? items : items.filter((it) => ((it.header && it.header.state) || "open") === sel);
      let page = 1;
      function paint() {
        listBox.replaceChildren(...renderList(shown.slice(0, page * PAGE), cardFn, emptyText));
        if (moreWrap) { moreWrap.remove(); moreWrap = null; }
        const remaining = shown.length - page * PAGE;
        if (remaining > 0) {
          const btn = el("button", { class: "load-more", type: "button" }, ["Load more (" + remaining + ")"]);
          btn.addEventListener("click", () => { page++; paint(); });
          moreWrap = el("div", { class: "load-more-wrap" }, [btn]);
          outer.append(moreWrap);
        }
      }
      paint();
      for (const c of chips) c.classList.toggle("active", c._key === sel);
    }
    for (const s of states) {
      const n = s.key === "all" ? counts.total : (counts.byState[s.key] || 0);
      const chip = el("button", { class: "filter-chip", type: "button" }, [s.label + " " + n]);
      chip._key = s.key;
      chip.addEventListener("click", () => { filterState[tab] = s.key; render(); });
      chips.push(chip);
      bar.append(chip);
    }
    render();
    return [outer];
  }

  // pmGroupCard renders one milestone or sprint with its state/date chips and
  // the issues bucketed under it.
  function pmGroupCard(item, members, kind) {
    const subject = itemSubject(item);
    const h = item.header || {};
    const card = el("div", { class: "card pm-group" }, []);
    const head = el("div", { class: "card-head" }, [
      el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"]),
    ]);
    prependGlyph(head, { header: { type: kind } }, "pm");
    if (h.state) head.append(stateChip(h.state));
    card.append(head);
    const dates = [];
    if (h.due) dates.push("due " + h.due);
    if (h.start) dates.push(h.start + " → " + (h.end || "?"));
    dates.push((members.length) + (members.length === 1 ? " issue" : " issues"));
    card.append(el("div", { class: "meta" }, [dates.join(" · ")]));
    for (const m of members) {
      const row = el("div", { class: "pm-member" }, [
        stateChip((m.header && m.header.state) || "open"), " ",
        el("a", { href: commitRef(m.commit.hash, "gitmsg/pm") }, [itemSubject(m) || "(untitled)"]),
      ]);
      card.append(row);
    }
    return card;
  }

  // issuesBody renders the state-filtered issue list as a node array (so the paged
  // list can redraw it from a growing pm item set on "Load more"). Milestones and
  // sprints have their own nav pages, so they are no longer embedded here.
  function issuesBody(pmItems, counts) {
    const g = groupPM(pmItems);
    const hier = buildIssueHierarchy(g.issues);
    const card = (it) => issueCard(it, (hier.childrenOf.get(it.commit.short) || []).length, countsFor(counts, it.commit.short));
    return filteredListView(g.issues, card, "issues", ISSUE_STATES, "No issues in this repository.");
  }

  // issuesView renders the milestones/sprints subsections above a state-filtered
  // issue list.
  function issuesView(pmItems) {
    return [el("div", {}, issuesBody(pmItems))];
  }
  // versionKey extracts a milestone's leading dotted-number version ("1.4.0" ->
  // [1,4,0], "1.0 Public Release" -> [1,0]); null when the subject carries none.
  function versionKey(item) {
    const m = /(\d+(?:\.\d+)+|\d+)/.exec(itemSubject(item) || "");
    return m ? m[1].split(".").map(Number) : null;
  }
  // compareVersionDesc orders milestone entries by version, highest first; entries
  // without a version fall to the end (newest-created among themselves).
  function compareVersionDesc(a, b) {
    const va = versionKey(a.item), vb = versionKey(b.item);
    if (!va && !vb) return (b.item.effectiveTime || 0) - (a.item.effectiveTime || 0);
    if (!va) return 1;
    if (!vb) return -1;
    for (let i = 0; i < Math.max(va.length, vb.length); i++) {
      const d = (vb[i] || 0) - (va[i] || 0);
      if (d) return d;
    }
    return 0;
  }

  // dedupePmGroups collapses milestone/sprint items that are duplicate imports of
  // the same upstream object (same origin-url, else same subject) into one entry,
  // merging the issues bucketed under every duplicate and keeping the newest commit
  // as the representative. Returns [{ item, members }] sorted by `cmp` (default
  // newest-created first).
  function dedupePmGroups(groupItems, byHash, cmp) {
    const byKey = new Map();
    for (const it of groupItems) {
      const key = (it.header && it.header["origin-url"]) || itemSubject(it) || it.commit.hash;
      let e = byKey.get(key);
      if (!e) { e = { item: it, hashes: [] }; byKey.set(key, e); }
      else if ((it.effectiveTime || 0) > (e.item.effectiveTime || 0)) e.item = it;
      e.hashes.push(it.commit.short);
    }
    const out = [];
    for (const e of byKey.values()) {
      const seen = new Set(), members = [];
      for (const hash of e.hashes) for (const m of (byHash.get(hash) || [])) {
        if (!seen.has(m.commit.hash)) { seen.add(m.commit.hash); members.push(m); }
      }
      out.push({ item: e.item, members });
    }
    out.sort(cmp || ((a, b) => (b.item.effectiveTime || 0) - (a.item.effectiveTime || 0)));
    return out;
  }

  // milestonesBody / sprintsBody render the standalone PM milestone / sprint list
  // views (their own nav destinations): one deduped card per upstream group, newest
  // first, with its issues merged across duplicate imports; empty-state when none.
  function milestonesBody(pmItems) {
    const g = groupPM(pmItems);
    const groups = dedupePmGroups(g.milestones, g.byMilestone, compareVersionDesc);
    if (!groups.length) return [el("div", { class: "empty" }, ["No milestones in this repository."])];
    return groups.map((x) => pmGroupCard(x.item, x.members, "milestone"));
  }
  function sprintsBody(pmItems) {
    const g = groupPM(pmItems);
    const groups = dedupePmGroups(g.sprints, g.bySprint);
    if (!groups.length) return [el("div", { class: "empty" }, ["No sprints in this repository."])];
    return groups.map((x) => pmGroupCard(x.item, x.members, "sprint"));
  }

  // ---- PM board / milestone-sprint detail / sub-issues ----

  // progressBar renders a compact "n closed of m" completion line with a simple
  // filled bar (TUI RenderProgressBar parity), using theme tokens. total 0 shows
  // a neutral "no issues" note.
  function progressBar(closed, total) {
    const pct = total ? Math.round((closed / total) * 100) : 0;
    const wrap = el("div", { class: "pm-progress" }, []);
    wrap.append(el("div", { class: "pm-bar" }, [el("div", { class: "pm-bar-fill", style: "width:" + pct + "%" }, [])]));
    wrap.append(el("span", { class: "pm-progress-label mono" }, [total ? (closed + " closed of " + total) : "no issues"]));
    return wrap;
  }

  // issueMemberRow is a compact state-chip + linked-subject row for an issue
  // listed under a milestone, sprint, or parent (the .pm-member style).
  function issueMemberRow(item) {
    const subject = itemSubject(item);
    return el("div", { class: "pm-member" }, [
      stateChip((item.header && item.header.state) || "open"), " ",
      el("a", { href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"]),
    ]);
  }

  // pmMembersSection renders a milestone's "Linked Issues" or a sprint's "Sprint
  // Backlog" member list with a header count and a closed-count progress bar
  // (mirroring the TUI milestone/sprint detail sections).
  function pmMembersSection(headLabel, members) {
    const wrap = el("div", { class: "pm-members" }, []);
    wrap.append(el("div", { class: "pm-members-head mono" }, [headLabel + " (" + members.length + ")"]));
    const p = pmProgress(members);
    wrap.append(progressBar(p.closed, p.total));
    for (const m of members) wrap.append(issueMemberRow(m));
    return wrap;
  }

  // subIssuesSection renders an issue's direct children (GITPM 1.7) under a
  // "Sub-issues (n open, n closed)" header with a closed-count progress bar,
  // matching the TUI sub-issue section. Null when the issue has no children.
  function subIssuesSection(children) {
    if (!children.length) return null;
    const p = pmProgress(children);
    const open = p.total - p.closed;
    const wrap = el("div", { class: "pm-subissues" }, []);
    wrap.append(el("div", { class: "pm-subissues-head mono" }, ["Sub-issues (" + open + " open, " + p.closed + " closed)"]));
    wrap.append(progressBar(p.closed, p.total));
    for (const c of children) wrap.append(issueMemberRow(c));
    return wrap;
  }

  // pmRelChip renders a labelled relationship link (parent / milestone / sprint)
  // on an issue detail: the target's subject linking to its detail page, plus a
  // state chip when the target is loaded. Falls back to the bare ref hash when the
  // target is not in the walked set (the link still deep-resolves on navigation).
  function pmRelChip(labelText, target, hash) {
    const subject = target ? (subjectBody(target.content)[0] || "(untitled)") : hash;
    const row = el("div", { class: "pm-rel" }, [
      el("span", { class: "pm-rel-label mono" }, [labelText]),
      el("a", { class: "pm-rel-link", href: commitRef((target && target.commit.hash) || hash, "gitmsg/pm") }, [subject]),
    ]);
    if (target && target.header && target.header.state) row.append(stateChip(target.header.state));
    return row;
  }

  // pmDetailExtras builds the pm-specific sections appended to an item detail
  // (issue/milestone/sprint) from the already-resolved pm item set: milestones and
  // sprints get their member list + progress; an issue gets its parent/milestone/
  // sprint relationship links and its sub-issue list + progress. Grouping runs off
  // header fields (present in the metadata index); the few related/member items
  // whose subjects render are body-hydrated before building the cards.
  async function pmDetailExtras(ctx, item, items) {
    const type = (item.header && item.header.type) || "issue";
    const issues = items.filter((i) => ((i.header && i.header.type) || "issue") === "issue");
    const out = [];
    if (type === "milestone") {
      const g = groupPM(items);
      const members = g.byMilestone.get(item.commit.short) || [];
      await hydrateItems(ctx, members);
      out.push(pmMembersSection("Linked Issues", members));
    } else if (type === "sprint") {
      const g = groupPM(items);
      const members = g.bySprint.get(item.commit.short) || [];
      await hydrateItems(ctx, members);
      out.push(pmMembersSection("Sprint Backlog", members));
    } else {
      const find = (h) => { if (!h) return null; for (const i of items) if (hashEq(i.commit.short, h)) return i; return null; };
      const ph = pmParentHash(item.header);
      const msh = refHash((item.header && item.header.milestone) || "");
      const sph = refHash((item.header && item.header.sprint) || "");
      const hier0 = buildIssueHierarchy(issues);
      const subs0 = hier0.childrenOf.get(item.commit.short) || [];
      await hydrateItems(ctx, [find(ph), find(msh), find(sph)].concat(subs0).filter(Boolean));
      const rels = el("div", { class: "pm-rels" }, []);
      if (ph) rels.append(pmRelChip("parent", find(ph), ph));
      if (msh) rels.append(pmRelChip("milestone", find(msh), msh));
      if (sph) rels.append(pmRelChip("sprint", find(sph), sph));
      if (rels.childNodes && rels.childNodes.length) out.push(rels);
      const sub = subIssuesSection(subs0);
      if (sub) out.push(sub);
    }
    return out.filter(Boolean);
  }

  // boardCard is the compact issue card used in board columns: linked subject plus
  // any scoped labels (status/priority/kind chips).
  function boardCard(item) {
    const subject = itemSubject(item);
    const card = el("div", { class: "card board-card" }, []);
    // Glyph + subject share ONE flex child (a .board-card-title line) so a narrow
    // 15rem board column wraps the subject TEXT under itself without ever pushing
    // the glyph onto its own row (the flex-wrap card-head would otherwise split
    // the glyph from a long unbreakable subject). G8.
    const titleLine = el("span", { class: "board-card-title" }, [el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"])]);
    const g = typeGlyphEl(item, "pm");
    if (g) titleLine.prepend(g);
    const head = el("div", { class: "card-head" }, [titleLine]);
    card.append(head);
    const labels = itemLabels(item.header);
    if (labels.length) {
      const row = el("div", { class: "board-card-labels" }, []);
      for (const l of labels) row.append(el("span", { class: "chip" }, [l.scope ? l.scope + "/" + l.value : l.value]));
      card.append(row);
    }
    return cardNav(card, item.commit.hash, "gitmsg/pm");
  }

  // BOARD_ITEM_CAP bounds how many cards a single column-cell shows before a
  // "show N more" control expands it, so a long column (or a busy swimlane cell)
  // never pushes the following lanes far down the page.
  const BOARD_ITEM_CAP = 7;

  // boardColumnEl renders one board column (header + count + WIP + capped cards)
  // for a given issue subset — reused by the flat board and each swimlane lane.
  // `state` carries the session-only board UI state (collapsed columns, hidden
  // columns, per-cell expand keys). A collapsed or hidden column renders only its
  // slim header; otherwise the cell shows up to BOARD_ITEM_CAP cards with a "show N
  // more" control (keyed by `cellKey` so each swimlane cell expands independently).
  function boardColumnEl(col, issues, state, cellKey, onChange) {
    const column = el("div", { class: "board-col" + (state.collapsedCols.has(col.name) ? " board-col-collapsed" : "") }, []);
    const head = el("div", { class: "board-col-head mono" }, []);
    // A caret toggles this column collapsed (slim header only) for the whole board.
    const caret = el("button", { class: "board-col-toggle", type: "button", "aria-label": "Collapse column" }, [state.collapsedCols.has(col.name) ? "▸" : "▾"]);
    caret.addEventListener("click", (e) => { e.stopPropagation(); state.toggleCol(col.name); onChange(); });
    head.append(caret, el("span", { class: "board-col-name" }, [col.name + " " + issues.length + (col.wip ? " / " + col.wip : "")]));
    if (col.wip && issues.length > col.wip) head.append(el("span", { class: "chip board-wip-over" }, ["over WIP"]));
    // A hide control removes the column from the board (restored from the controls
    // bar); useful for a Done column that accumulates over time.
    const hide = el("button", { class: "board-col-hide", type: "button", "aria-label": "Hide column", title: "Hide column" }, ["✕"]);
    hide.addEventListener("click", (e) => { e.stopPropagation(); state.hideCol(col.name); onChange(); });
    head.append(hide);
    column.append(head);
    if (state.collapsedCols.has(col.name)) return column;
    if (!issues.length) { column.append(el("div", { class: "board-empty mono" }, ["—"])); return column; }
    // Done-like columns (state:closed filter) start capped even when other columns
    // aren't expanded, since they grow unbounded; the expand key still overrides.
    const expanded = state.expandedCells.has(cellKey);
    const shown = expanded ? issues : issues.slice(0, BOARD_ITEM_CAP);
    for (const it of shown) column.append(boardCard(it));
    if (issues.length > BOARD_ITEM_CAP) {
      const more = el("button", { class: "board-more load-more", type: "button" }, [expanded ? "show less" : "show " + (issues.length - BOARD_ITEM_CAP) + " more"]);
      more.addEventListener("click", (e) => { e.stopPropagation(); state.toggleCell(cellKey); onChange(); });
      column.append(more);
    }
    return column;
  }

  // boardGrid renders the columns × issues layout for a set of columns, each
  // column drawing from its own bucketed issues (col.issues), or a per-lane subset
  // when `laneOf` maps a column name to its lane members. Hidden columns are
  // skipped here (restored from the controls bar). `keyPrefix` namespaces each
  // cell's expand key so the flat board and every swimlane cell page independently.
  function boardGrid(columns, laneOf, state, keyPrefix, onChange) {
    const grid = el("div", { class: "board" }, []);
    for (const col of columns) {
      if (state.hiddenCols.has(col.name)) continue;
      const issues = laneOf ? (laneOf.get(col.name) || []) : col.issues;
      grid.append(boardColumnEl(col, issues, state, keyPrefix + "\x00" + col.name, onChange));
    }
    return grid;
  }

  // newBoardState builds the session-only board UI state: collapsed columns,
  // hidden columns, collapsed swimlanes, and expanded per-cell keys. Not persisted
  // (mirrors the TUI's in-session UserPrefs), reset on every fresh board render.
  function newBoardState() {
    const s = {
      collapsedCols: new Set(), hiddenCols: new Set(), collapsedLanes: new Set(), expandedCells: new Set(),
      toggleCol: (n) => { s.collapsedCols.has(n) ? s.collapsedCols.delete(n) : s.collapsedCols.add(n); },
      hideCol: (n) => s.hiddenCols.add(n),
      showCol: (n) => s.hiddenCols.delete(n),
      toggleLane: (l) => { s.collapsedLanes.has(l) ? s.collapsedLanes.delete(l) : s.collapsedLanes.add(l); },
      toggleCell: (k) => { s.expandedCells.has(k) ? s.expandedCells.delete(k) : s.expandedCells.add(k); },
    };
    return s;
  }

  // boardBody renders the kanban board (#/board) with a "group by" swimlane
  // control. Columns come from the repo's resolved config; the group-by
  // cycles none/priority/kind/assignees/author, defaulting to the board
  // config's defaultSwimlane, with a session-only override. When a field is set,
  // issues are grouped into collapsible lane bands (each spanning the columns) in
  // the TUI's lane order, with a lane index for jumping and per-column-cell item
  // caps. Column collapse/hide and lane collapse are session-only (G7).
  function boardBody(issues, config) {
    const board = buildBoard(issues, config);
    const state = newBoardState();
    const outer = el("div", { class: "board-view" }, []);
    const controls = el("div", { class: "board-controls" }, []);
    outer.append(controls);
    const body = el("div", {}, []);
    outer.append(body);
    let field = SWIMLANE_FIELDS.indexOf(board.defaultSwimlane) >= 0 ? board.defaultSwimlane : "";
    const rerender = () => draw();
    function draw() {
      // Group-by control: a labeled row of selectable field chips.
      controls.replaceChildren(el("span", { class: "board-groupby-label mono" }, ["Group by"]));
      for (const f of SWIMLANE_FIELDS) {
        const chip = el("button", { class: "filter-chip" + (f === field ? " active" : ""), type: "button" }, [SWIMLANE_LABELS[f]]);
        chip.addEventListener("click", () => { field = f; draw(); });
        controls.append(chip);
      }
      // Hidden-column restore control: one chip per hidden column brings it back.
      for (const col of board.columns) {
        if (!state.hiddenCols.has(col.name)) continue;
        const restore = el("button", { class: "filter-chip board-col-restore", type: "button" }, ["+ " + col.name]);
        restore.addEventListener("click", () => { state.showCol(col.name); draw(); });
        controls.append(restore);
      }
      if (!field) { body.replaceChildren(boardGrid(board.columns, null, state, "flat", rerender)); return; }
      const lanes = swimlaneOrder(issues, field);
      const sections = [el("div", { class: "board-groupby-indicator mono" }, ["grouped by " + field])];
      // Lane index: a compact list of lane labels + counts that scrolls to a lane
      // on click, so long lanes don't bury the ones after them.
      const laneMembers = new Map();
      const laneEls = new Map();
      for (const lane of lanes) {
        const laneOf = new Map();
        let any = 0, visibleAny = 0;
        for (const col of board.columns) {
          const members = groupBySwimlane(col.issues, field, [lane]).get(lane) || [];
          laneOf.set(col.name, members);
          any += members.length;
          if (!state.hiddenCols.has(col.name)) visibleAny += members.length;
        }
        // Skip a lane whose members all live in hidden columns (e.g. every issue
        // of a lane sits in a hidden Done column): its grid would render empty.
        if (visibleAny) laneMembers.set(lane, { laneOf, any });
      }
      if (laneMembers.size > 1) {
        const index = el("div", { class: "board-lane-index mono" }, []);
        for (const [lane, m] of laneMembers) {
          const link = el("button", { class: "board-lane-jump", type: "button" }, [swimlaneLabel(lane) + " (" + m.any + ")"]);
          link.addEventListener("click", () => { const t = laneEls.get(lane); if (t && t.scrollIntoView) t.scrollIntoView({ behavior: "smooth", block: "start" }); });
          index.append(link);
        }
        sections.push(index);
      }
      for (const [lane, m] of laneMembers) {
        const collapsed = state.collapsedLanes.has(lane);
        const section = el("div", { class: "board-lane" + (collapsed ? " board-lane-collapsed" : "") }, []);
        laneEls.set(lane, section);
        const laneHead = el("div", { class: "board-lane-head mono", role: "button", tabindex: "0" }, [
          el("span", { class: "board-lane-caret" }, [collapsed ? "▸" : "▾"]),
          el("span", {}, [swimlaneLabel(lane) + " (" + m.any + ")"]),
        ]);
        laneHead.addEventListener("click", () => { state.toggleLane(lane); draw(); });
        section.append(laneHead);
        if (!collapsed) section.append(boardGrid(board.columns, m.laneOf, state, "lane:" + lane, rerender));
        sections.push(section);
      }
      body.replaceChildren(...sections);
    }
    draw();
    return outer;
  }

  // boardView renders the pm board (#/board): a client-side kanban regroup of the
  // walked issue set, with a "Load more" that deepens the pm walk and recounts the
  // columns. Worst-case fan-out: the same bounded pm walk the Issues tab runs.
  async function boardView(ctx) {
    const issuesOf = (items) => items.filter((i) => ((i.header && i.header.type) || "issue") === "issue");
    // Full pm set (metadata-only, cheap) so every column's count is exact, not
    // limited to a first window. The board columns come from the repo's resolved
    // config (framework or custom), falling back to the kanban default.
    const [all, config] = await Promise.all([loadExtItemsAll(ctx, "pm"), loadSiteConfig(ctx)]);
    return [boardBody(issuesOf(all), config)];
  }

  // highlightFrag splits text around the first case-insensitive occurrence of the
  // query, wrapping the match in a <mark> (existing search-mark styling). Returns
  // a child array for el(); the plain string when there is no match.
  function highlightFrag(str, query) {
    const s = str || "";
    const q = (query || "").trim();
    if (!q) return [s];
    const idx = s.toLowerCase().indexOf(q.toLowerCase());
    if (idx < 0) return [s];
    return [s.slice(0, idx), el("mark", { class: "search-mark" }, [s.slice(idx, idx + q.length)]), s.slice(idx + q.length)];
  }

  // searchSnippet returns a small highlighted excerpt when the query matched the
  // item body or its labels rather than the subject (so the reader sees where it
  // hit); null when the subject already carries the match, or when the match
  // came from a body-less light-tier field (author) with nothing to excerpt.
  function searchSnippet(item, query) {
    const q = (query || "").trim().toLowerCase();
    if (!q) return null;
    const subject = itemSubject(item);
    if (subject.toLowerCase().indexOf(q) !== -1) return null;
    let hay = item.content || "", idx = hay.toLowerCase().indexOf(q);
    if (idx === -1) {
      const lbl = (item.header && item.header.labels) || "";
      if (lbl.toLowerCase().indexOf(q) !== -1) { hay = "labels: " + lbl; idx = hay.toLowerCase().indexOf(q); }
    }
    if (idx === -1) return null;
    const start = Math.max(0, idx - 32), end = Math.min(hay.length, idx + q.length + 32);
    const frag = (start > 0 ? "…" : "") + hay.slice(start, end).replace(/\n+/g, " ") + (end < hay.length ? "…" : "");
    return el("div", { class: "search-snippet meta" }, highlightFrag(frag, query));
  }

  // searchResultCard renders one grouped search hit: a state chip (issues/PRs),
  // the highlighted subject (the metadata-index subject for a body-less
  // light-tier hit) linking to the item detail, a meta row, and a snippet when
  // the match was in the body/labels.
  function searchResultCard(item, group, query) {
    const subject = itemSubject(item);
    const h = item.header || {};
    const card = el("div", { class: "card search-result" }, []);
    const head = el("div", { class: "card-head" }, []);
    prependGlyph(head, item, group.ext);
    if (h.state) head.append(stateChip(h.state));
    head.append(el("a", { class: "subject", href: commitRef(item.commit.hash, group.branch) }, highlightFrag(subject || "(untitled)", query)));
    card.append(head);
    card.append(metaRow(item, group.branch));
    const snip = searchSnippet(item, query);
    if (snip) card.append(snip);
    return cardNav(card, item.commit.hash, group.branch);
  }

  // searchResults renders the result list: by default the flat recency-ordered
  // lane (each card carries its type glyph, so type stays visible without the
  // headers); `grouped` switches to the per-extension sections with match
  // counts. The "no results" empty state names the query.
  function searchResults(res, query, grouped) {
    if (!res.total) {
      const shown = query || res.query;
      return [el("div", { class: "empty" }, [shown ? "No results for “" + shown + "”." : "No items match the selected filters."])];
    }
    const out = [];
    if (grouped) {
      for (const g of res.groups) {
        out.push(el("div", { class: "search-group-head mono" }, [g.label + " (" + g.count + ")"]));
        for (const it of g.items) out.push(searchResultCard(it, g, query));
      }
    } else {
      for (const f of res.flat) out.push(searchResultCard(f.item, f.group, query));
    }
    return out;
  }

  // searchHelp is the pre-query help / scope note: what search covers and an
  // honest statement of the current tier — light (subjects/authors/labels, with
  // the full-text affordance), truncated fallback walks (the deeper affordance),
  // or complete coverage.
  function searchHelp(corpus) {
    const box = el("div", { class: "search-help empty" }, []);
    box.append(el("div", {}, ["Search loaded issues, pull requests, posts, releases, memos, and code commits by subject, content, author, or labels."]));
    box.append(el("div", { class: "search-help-scope" }, ["Filter with type:issue, state:open, author:alice, label:bug, or @alice — then refine with the facet chips."]));
    box.append(el("div", { class: "search-help-scope" }, ["Find by commit with hash:abc1234 (or a bare 7-40 hex hash), and narrow by date with after:2026-01-01 or before:2026-12-31."]));
    let scope = "All history is loaded; search covers every item in this bucket (code commits match by subject and author).";
    if (corpus.truncated || corpus.light || corpus.hasOlder) scope = "Recent items are searched by subject, author, and labels; use Load full search index to cover all history and match message bodies (code commits stay subject-level).";
    box.append(el("div", { class: "search-help-scope" }, [scope]));
    return box;
  }

  // searchInputEl holds the live search box so the `/` shortcut can focus it
  // without a second control (mirrors the tree-search focus pattern).
  let searchInputEl = null;
  // focusSearchInput focuses the current search box if one is mounted.
  function focusSearchInput() { if (searchInputEl && searchInputEl.focus) { searchInputEl.focus(); return true; } return false; }

  // searchView renders the in-bucket item search (#/search): a debounced query
  // input over the already-walked items of every extension, results recent-first
  // across all types (a toggle restores per-extension sections), and an honest scope
  // humanBytes formats a byte count as a compact KB/MB string (one decimal for MB).
  function humanBytes(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + " MB";
    if (n >= 1024) return Math.round(n / 1024) + " KB";
    return n + " B";
  }

  // note. Search is tiered: the light tier answers immediately from the metadata
  // index (subjects/authors/labels); a "Load full search index" affordance fetches
  // the bodies corpus and re-runs the query over complete message text. Buckets
  // without artifacts keep the "Search deeper" affordance that advances every
  // extension's walk one window and re-runs the query. Escape clears; the input
  // auto-focuses on mount. Returns synchronously (the corpus loads async and fills
  // the results), so the box is focusable immediately.
  function searchView(ctx, initialQuery) {
    const wrap = el("div", { class: "search-view" }, []);
    wrap.append(el("a", { class: "back", href: "#/" }, ["← back"]));
    const input = el("input", { class: "search-input", type: "text", placeholder: "Search issues, PRs, posts, releases, memos, commits…", "aria-label": "Search items", autocomplete: "off", spellcheck: "false" }, []);
    if (initialQuery) input.value = initialQuery;
    wrap.append(input);
    searchInputEl = input;
    const status = el("div", { class: "search-status meta" }, []);
    wrap.append(status);
    const results = el("div", { class: "search-results" }, []);
    wrap.append(results);
    let corpus = null, deeperWrap = null, debounce = null;
    // Facet state: clicked-chip selections per field (unioned with any typed
    // filters in the query box) and which facets the user expanded past the cap.
    const filters = { type: new Set(), state: new Set(), author: new Set(), label: new Set() };
    let facetExpanded = {};
    let grouped = false;
    const FACET_UI = [["type", "Type"], ["state", "State"], ["author", "Author"], ["label", "Labels"]];
    const FACET_CAP = 8;
    // facetChip renders one clickable value chip with its drill-down count; a
    // click toggles the field's selection and redraws.
    function facetChip(field, b) {
      const chip = el("button", { class: "facet-chip" + (b.selected ? " selected" : ""), type: "button" }, [b.value, el("span", { class: "facet-count" }, [String(b.count)])]);
      chip.addEventListener("click", () => {
        const set = filters[field];
        if (set.has(b.value)) set.delete(b.value); else set.add(b.value);
        draw();
      });
      return chip;
    }
    // renderFacets builds the Type/State/Author/Labels chip rows above results. A
    // field shows only when it has ≥2 distinct values (or a live selection), and
    // caps its chips with a "+N more" expander so a long author/label tail stays
    // one line until asked for.
    function renderFacets(res) {
      const rows = [];
      for (const [key, label] of FACET_UI) {
        const buckets = res.facets[key] || [];
        if (buckets.length < 2 && !buckets.some((b) => b.selected)) continue;
        const shown = facetExpanded[key] ? buckets : buckets.slice(0, FACET_CAP);
        const row = el("div", { class: "facet-row" }, [el("span", { class: "facet-label" }, [label])]);
        for (const b of shown) row.append(facetChip(key, b));
        if (buckets.length > shown.length) {
          const more = el("button", { class: "facet-more", type: "button" }, ["+" + (buckets.length - shown.length) + " more"]);
          more.addEventListener("click", () => { facetExpanded[key] = true; draw(); });
          row.append(more);
        }
        rows.push(row);
      }
      return rows.length ? el("div", { class: "search-facets" }, rows) : null;
    }
    function tierButton(label, busyLabel, extend, full, older) {
      const btn = el("button", { class: "load-more", type: "button" }, [label]);
      btn.addEventListener("click", async () => {
        btn.disabled = true; btn.textContent = busyLabel;
        try { corpus = await loadSearchWindow(ctx, extend, full, older); } catch (e) { /* keep prior corpus */ }
        draw();
      });
      return btn;
    }
    function renderDeeper() {
      if (deeperWrap) { deeperWrap.remove(); deeperWrap = null; }
      if (!corpus) return;
      const kids = [];
      // One affordance loads everything the current corpus is missing: the bodies
      // corpus fetches every shard (whole-history full text) for indexed
      // extensions, and the same click advances any loose-object fallback walk for
      // an extension without artifacts. It reappears until nothing remains.
      const incomplete = corpus.truncated || ((corpus.light || corpus.hasOlder) && !corpus.full);
      if (incomplete) {
        const note = corpus.partial
          ? "Currently searching recent items by subject, author, and labels. Loads every message body across all history to match full text (coverage is limited to the bootstrapped prefix)."
          : "Currently searching recent items by subject, author, and labels. Loads every message body across all history to match full text.";
        kids.push(el("div", { class: "search-tier-note" }, [note]));
        const fullBtn = tierButton("Load full search index", "Loading full search index…", true, true, true);
        kids.push(fullBtn);
        // Append the download size once known (from the loaded metadata indexes),
        // without blocking the button's appearance; skip if already clicked.
        fullSearchBytes(ctx).then((bytes) => { if (bytes > 0 && !fullBtn.disabled) fullBtn.textContent = "Load full search index (" + humanBytes(bytes) + ")"; }).catch(() => {});
      }
      if (!kids.length) return;
      deeperWrap = el("div", { class: "load-more-wrap" }, kids);
      wrap.append(deeperWrap);
    }
    function draw() {
      if (!corpus) { results.replaceChildren(el("div", { class: "loading" }, ["Loading…"])); return; }
      const res = searchItemsFaceted(input.value || "", corpus.perExt, filters);
      // No query, no typed filter, no chip: idle → the scope help, no facets.
      if (!Object.keys(res.facets).length) {
        status.textContent = "";
        results.replaceChildren(searchHelp(corpus));
        renderDeeper();
        return;
      }
      const partial = corpus.truncated || corpus.light || corpus.hasOlder;
      status.textContent = res.total + (res.total === 1 ? " result" : " results") + (partial && !corpus.full ? " in loaded items" : "");
      const nodes = [];
      const facetBox = renderFacets(res);
      if (facetBox) nodes.push(facetBox);
      // Results are recent-first across all types by default; the toggle
      // restores the per-extension sections.
      const groupBtn = el("button", { class: "facet-chip" + (grouped ? " selected" : ""), type: "button" }, ["Group by type"]);
      groupBtn.addEventListener("click", () => { grouped = !grouped; draw(); });
      nodes.push(el("div", { class: "facet-row search-sort" }, [groupBtn]));
      for (const n of searchResults(res, res.terms, grouped)) nodes.push(n);
      results.replaceChildren(...nodes);
      renderDeeper();
    }
    input.addEventListener("input", () => { if (debounce) clearTimeout(debounce); debounce = setTimeout(draw, 150); });
    input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") { input.value = ""; draw(); } });
    draw();
    (async () => { try { corpus = await loadSearchWindow(ctx, false); } catch (e) { corpus = { perExt: {}, truncated: false, light: false, full: false }; } draw(); })();
    if (input.focus) setTimeout(() => input.focus(), 0);
    return [wrap];
  }

  // Analytics series labels + the granularity nouns/order. KIND_LABELS names each
  // extension series in the summary/legend; GRANS is the toggle order (default
  // monthly). The activity chart renders the full history at a fixed ~12-periods-
  // per-view width (never squished) and scrolls horizontally for older periods.
  const KIND_LABELS = { commits: "commits", posts: "posts", issues: "issues", prs: "PRs", releases: "releases", memos: "memos" };
  const GRANS = ["weekly", "monthly", "yearly"];
  const GRAN_NOUN = { weekly: "week", monthly: "month", yearly: "year" };

  // analyticsSummary renders the headline stat grid: total items, the per-kind
  // totals, and the most active period at the current granularity (mostActive may
  // be null when there is no activity).
  function analyticsSummary(data, mostActive) {
    const wrap = el("div", { class: "analytics-section" }, []);
    wrap.append(el("div", { class: "contrib-head mono" }, ["Summary"]));
    const grid = el("div", { class: "stat-grid" }, []);
    const cell = (label, value, valueClass) => grid.append(el("div", { class: "stat-cell" }, [
      el("div", { class: valueClass || "stat-value" }, [String(value)]),
      el("div", { class: "stat-label mono" }, [label]),
    ]));
    cell("total items", data.total);
    for (const k of data.kinds) cell(KIND_LABELS[k] || k, data.perKind[k] || 0);
    if (mostActive) cell("most active", mostActive.label, "stat-value-text mono");
    wrap.append(grid);
    return wrap;
  }

  // chartFilter renders the activity chart's series filter: an "all" chip plus one
  // per kind (each with its color swatch), doubling as the legend. Clicking a chip
  // shows just that series; "all" stacks every series.
  function chartFilter(kinds, selected, onPick) {
    const row = el("div", { class: "chart-filter mono" }, []);
    const chip = (key, label, swatchKind) => {
      const b = el("button", { class: "filter-chip" + (key === selected ? " active" : ""), type: "button" }, []);
      if (swatchKind) b.append(el("span", { class: "legend-swatch akind-" + swatchKind }, []));
      b.append(label);
      b.addEventListener("click", () => onPick(key));
      return b;
    };
    row.append(chip("all", "all", null));
    for (const k of kinds) row.append(chip(k, KIND_LABELS[k] || k, k));
    return row;
  }

  // granularityToggle renders the Weekly/Monthly/Yearly buttons; clicking one
  // calls onPick(gran), which re-buckets the already-loaded data in memory (no
  // refetch). The current granularity's button is marked active.
  function granularityToggle(current, onPick) {
    const row = el("div", { class: "gran-toggle" }, []);
    for (const g of GRANS) {
      const btn = el("button", { class: "gran-btn" + (g === current ? " active" : ""), type: "button" }, [g]);
      btn.addEventListener("click", () => onPick(g));
      row.append(btn);
    }
    return row;
  }

  // compactNum formats a count-axis tick compactly (1234 -> "1.2k", 16000 -> "16k").
  function compactNum(n) {
    if (n >= 1000) { const k = n / 1000; return (k >= 10 ? Math.round(k) : Math.round(k * 10) / 10) + "k"; }
    return String(n);
  }

  // yAxis renders the activity chart's fixed count axis — peak, midpoint, and zero
  // ticks aligned to the bar area. It sits left of the scrolling bars and stays put.
  function yAxis(max) {
    const ticks = max > 1 ? [max, Math.round(max / 2), 0] : [Math.max(max, 1), 0];
    const col = el("div", { class: "activity-yaxis mono" }, []);
    for (const t of ticks) col.append(el("div", { class: "activity-ytick" }, [compactNum(t)]));
    return col;
  }

  // stackedBars renders the activity chart (CSS only, no chart lib): one column
  // per period, a full-height stack sized to that period's share of the peak
  // total, split into per-kind segments (color via akind-<kind>). Exact totals on
  // column hover, per-kind counts on segment hover.
  function stackedBars(model, kinds) {
    const chart = el("div", { class: "activity-chart stacked" }, []);
    for (const b of model.buckets) {
      const barH = model.max ? Math.round((b.total / model.max) * 100) : 0;
      const bar = el("div", { class: "activity-stack", style: "height:" + barH + "%" }, []);
      for (const k of kinds) {
        const c = b.counts[k] || 0;
        if (!c) continue;
        const segH = b.total ? (c / b.total) * 100 : 0;
        bar.append(el("div", { class: "activity-seg akind-" + k, style: "height:" + segH + "%", title: (KIND_LABELS[k] || k) + ": " + c }, []));
      }
      chart.append(el("div", { class: "activity-col", title: b.label + ": " + b.total + (b.total === 1 ? " item" : " items") }, [
        el("div", { class: "activity-bar-wrap" }, [bar]),
        el("div", { class: "activity-label mono" }, [b.short || b.label]),
      ]));
    }
    return chart;
  }

  // analyticsAuthors renders the top-authors ranking by item count (across every
  // extension), each with its share of all items, its count, and its email on
  // hover (via authorEl). Item authorship uses effective/origin authors (imported
  // content is attributed to the real upstream author), unlike a git-commit count.
  // authorSearchHref builds a #/search deep-link with the author facet prefilled
  // by EMAIL (tightest match) when known, else the display name. The search view
  // initializes from the route query and executes it, so the link lands on the
  // author's items already filtered.
  function authorSearchHref(a) {
    const token = a.email || a.name || "";
    return "#/search/" + encodeURIComponent("author:" + token);
  }

  // analyticsAuthors renders the top authors by item count in a scrollable
  // container: a live name/email filter over the FULL ranked list on the heading
  // line, each author name linking to the prefilled search page, and an infinite
  // autoscroll window (mirrors autoScrollListView) that reveals PAGE more rows
  // each time a bottom sentinel nears the viewport, so the list keeps loading past
  // the first page as the user scrolls instead of capping. The count label
  // reflects what is shown vs the total distinct authors so it is not misleading.
  function analyticsAuthors(authors, total) {
    const PAGE = 50;
    const wrap = el("div", { class: "contrib" }, []);
    const head = el("div", { class: "contrib-head mono" }, []);
    const label = el("span", {}, []);
    const filter = el("input", { class: "contrib-filter", type: "text", placeholder: "Filter authors…", "aria-label": "Filter authors", autocomplete: "off", spellcheck: "false" }, []);
    head.append(label, filter);
    wrap.append(head);
    const list = el("div", { class: "contrib-list contrib-scroll" }, []);
    wrap.append(list);
    // The sentinel lives INSIDE the scrolling list (the list is its own overflow
    // container, not the page), so the observer roots on `list` to fire as it nears
    // the list's own bottom; it is re-appended after the rows on every draw.
    const sentinel = el("div", { class: "scroll-sentinel", "aria-hidden": "true" }, []);
    let shown = PAGE;
    let observer = null;
    function matches() {
      const q = (filter.value || "").trim().toLowerCase();
      if (!q) return authors;
      return authors.filter((a) => (a.name || "").toLowerCase().indexOf(q) !== -1 || (a.email || "").toLowerCase().indexOf(q) !== -1);
    }
    function draw() {
      const all = matches();
      const rows = all.slice(0, shown);
      // Count label: shown/total while the window (or a filter) hides rows, so
      // the count never overstates the list; plain total once everything shows.
      label.textContent = rows.length < authors.length ? "Authors " + rows.length + "/" + authors.length : "Authors " + authors.length;
      list.replaceChildren();
      if (!rows.length) { list.append(el("div", { class: "empty" }, ["No authors match “" + filter.value + "”."])); return; }
      for (const a of rows) {
        const pct = total ? Math.round((a.count / total) * 100) : 0;
        list.append(el("div", { class: "contrib-row" }, [
          el("span", { class: "contrib-name" }, [el("a", { href: authorSearchHref(a), title: a.email || a.name }, [a.name])]),
          el("span", { class: "contrib-meta" }, [el("span", { class: "contrib-pct" }, [pct + "%"]), el("span", { class: "chip" }, [String(a.count)])]),
        ]));
      }
      if (rows.length < all.length) { list.append(sentinel); if (observer) { observer.unobserve(sentinel); observer.observe(sentinel); } }
    }
    function advance() {
      if (shown >= matches().length) return;
      shown += PAGE;
      draw();
    }
    wrap.__loadNext = advance;
    filter.addEventListener("input", () => { shown = PAGE; draw(); });
    const IO = (typeof window !== "undefined" && window.IntersectionObserver) ||
      (typeof IntersectionObserver !== "undefined" ? IntersectionObserver : null);
    if (IO) observer = new IO((entries) => { for (const e of entries) if (e.isIntersecting) { advance(); break; } }, { root: list, rootMargin: "200px" });
    draw();
    return wrap;
  }

  // analyticsView is the dedicated stats page. It loads the FULL per-extension
  // item metadata (uncapped, body-free — timestamps/authors/types all live in the
  // metadata index, so no loose-object hydration and no 200-item ceiling), then
  // renders: repo facts (default branch, branch count, latest release); a summary
  // stat grid (total + per-kind + most active period); an interactive activity
  // chart broken down by extension with a Weekly/Monthly/Yearly granularity toggle
  // (default monthly) that re-buckets the already-loaded data in memory; and the
  // top authors by item count. Worst-case fan-out: one full walk per data branch,
  // free on an index-seeded bucket (the frontier is already exhausted).
  async function analyticsView(ctx) {
    const head = await resolveHead(ctx.base);
    const branch = headBranchName(head);
    const { branches } = await listBranches(ctx);
    const wrap = el("div", { class: "detail analytics-view" }, []);
    wrap.append(el("div", { class: "subject" }, ["Analytics"]));
    const data = await loadAnalyticsData(ctx);
    const stats = await loadSiteStats(ctx);
    const facts = el("div", { class: "meta-strip" }, []);
    if (branch) facts.append(el("span", { class: "chip" }, [branch]));
    facts.append(el("a", { class: "chip", href: "#/branches" }, [branches.length + (branches.length === 1 ? " branch" : " branches")]));
    if (stats && typeof stats.commits === "number") {
      const n = String(stats.commits).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
      facts.append(el("span", { class: "chip" }, [n + (stats.commits === 1 ? " commit" : " commits")]));
    }
    if (data.latestRelease) facts.append(el("span", { class: "chip" }, ["latest " + data.latestRelease]));
    wrap.append(facts);
    // On an index-absent or stale-manifest bucket each extension's set is bounded
    // to its most-recent commits (no unbounded loose walk), so the aggregates
    // reflect recent activity only — say so, in the search view's voice, rather
    // than presenting a partial total as complete.
    if (data.partial) wrap.append(el("div", { class: "search-tier-note" }, [
      "Showing recent activity only: this bucket's item index is missing or still building, so analytics cover the most recent items rather than all history. Push with a current gitsocial (or run `gitsocial site push`) to index the full history.",
    ]));
    if (!data.total) { wrap.append(el("div", { class: "empty" }, ["No activity to analyze yet."])); return [wrap]; }
    const authors = topItemAuthors(data.entries);
    const summarySlot = el("div", {}, []);
    wrap.append(summarySlot);
    const chartSec = el("div", { class: "analytics-section" }, []);
    const heading = el("div", { class: "contrib-head mono" }, []);
    const toggleSlot = el("div", {}, []);
    chartSec.append(el("div", { class: "analytics-chart-head" }, [heading, toggleSlot]));
    const filterSlot = el("div", {}, []);
    chartSec.append(filterSlot);
    const chartSlot = el("div", {}, []);
    chartSec.append(chartSlot);
    wrap.append(chartSec);
    wrap.append(analyticsAuthors(authors, data.total));
    // Chart series: the gitsocial item kinds plus a "commits" series from the
    // push-computed commit times (when present). Filter chips pick "all" (every
    // series, stacked) or a single series; the granularity toggle re-buckets.
    const commitEntries = (stats && Array.isArray(stats.commitTimes)) ? stats.commitTimes.map((t) => ({ kind: "commits", time: t })) : [];
    const chartKinds = commitEntries.length ? ["commits"].concat(data.kinds) : data.kinds.slice();
    const chartEntries = commitEntries.length ? data.entries.concat(commitEntries) : data.entries;
    let gran = "monthly";
    let selected = "all";
    function draw() {
      // Summary's "most active" is over items only, at the current granularity.
      const itemBuckets = activityBuckets(data.entries, gran, data.kinds).buckets;
      const mostActive = itemBuckets.reduce((best, b) => (b.total > (best ? best.total : -1) ? b : best), null);
      summarySlot.replaceChildren(analyticsSummary(data, mostActive));
      // Chart: all series stacked, or a single filtered series.
      const kinds = selected === "all" ? chartKinds : [selected];
      const entries = selected === "all" ? chartEntries : chartEntries.filter((e) => e.kind === selected);
      const full = activityBuckets(entries, gran, kinds);
      const buckets = full.buckets;
      let max = 0;
      for (const b of buckets) if (b.total > max) max = b.total;
      const noun = GRAN_NOUN[gran];
      heading.textContent = "Activity (" + buckets.length + " " + noun + (buckets.length === 1 ? "" : "s") + ")";
      filterSlot.replaceChildren(chartFilter(chartKinds, selected, (k) => { selected = k; draw(); }));
      toggleSlot.replaceChildren(granularityToggle(gran, (g) => { gran = g; draw(); }));
      const chart = stackedBars({ buckets, max }, kinds);
      chartSlot.replaceChildren(el("div", { class: "activity-plot" }, [yAxis(max), chart]));
      // Start scrolled to the most recent (rightmost) periods; deferred until the
      // chart is mounted and laid out (scrollWidth is 0 before then).
      setTimeout(() => { chart.scrollLeft = chart.scrollWidth; }, 0);
    }
    draw();
    return [wrap];
  }

  // ---- Lists (#/lists overview + #list:<ext>/<name> detail) ----

  // listMemberRow renders one list member ref: a workspace-relative member links
  // into this bucket (its ref route); a foreign-repo member renders as labeled
  // mono text (its objects live in another bucket the reader cannot fetch).
  function listMemberRow(member) {
    const m = listMemberRef(member);
    const row = el("div", { class: "tree-row" }, []);
    if (m.local) {
      const href = m.ref.charAt(0) === "#" ? m.ref : "#" + m.ref;
      row.append(el("a", { class: "mono", href }, [m.ref]));
    } else {
      row.append(el("span", { class: "chip" }, ["repo"]));
      row.append(el("span", { class: "mono selectable" }, [m.ref]));
    }
    return row;
  }

  // listCardNav makes a lists-overview card navigate to its #list: detail route
  // on a click anywhere (inner anchors and selections excepted).
  function listCardNav(card, id) {
    card.className += " clickable";
    card.addEventListener("click", (e) => {
      if (e.target && e.target.closest && e.target.closest("a")) return;
      const sel = typeof window !== "undefined" && window.getSelection ? window.getSelection() : null;
      if (sel && !sel.isCollapsed) return;
      location.hash = "#list:" + id;
    });
    return card;
  }

  // listsView renders the #/lists overview: one card per list (name, extension,
  // member count, version) linking to its detail. Lists are discovered from the
  // refs manifest, so a bucket with none shows the standard empty state.
  async function listsView(ctx) {
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("div", { class: "subject" }, ["Lists"]));
    const lists = await loadListsSummary(ctx);
    if (!lists.length) { wrap.append(el("div", { class: "empty" }, ["No lists in this repository."])); return [wrap]; }
    for (const l of lists) {
      const name = (l.meta && l.meta.name) || l.name;
      const card = el("div", { class: "card" }, []);
      card.append(el("div", {}, [
        el("a", { class: "subject", href: "#list:" + l.id }, [name]), " ",
        el("span", { class: "chip" }, [l.ext]),
      ]));
      const meta = [l.count + (l.count === 1 ? " member" : " members")];
      if (l.meta && l.meta.version) meta.push("v" + l.meta.version);
      card.append(el("div", { class: "meta" }, [meta.join(" · ")]));
      wrap.append(listCardNav(card, l.id));
    }
    return [wrap];
  }

  // listDetailView renders one list (#list:<ext>/<name>): its metadata and its
  // resolved members (workspace-relative as links, foreign repos as labeled text).
  async function listDetailView(ctx, id) {
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("a", { class: "back", href: "#/lists" }, ["← back"]));
    const detail = await loadListDetail(ctx, id);
    if (!detail) { wrap.append(el("div", { class: "empty" }, ["List not found."])); return [wrap]; }
    const name = (detail.meta && detail.meta.name) || detail.name;
    wrap.append(el("div", { class: "subject" }, [name]));
    const meta = [detail.ext, detail.members.length + (detail.members.length === 1 ? " member" : " members")];
    if (detail.meta && detail.meta.version) meta.push("v" + detail.meta.version);
    wrap.append(el("div", { class: "meta" }, [meta.join(" · ")]));
    if (!detail.members.length) { wrap.append(el("div", { class: "empty" }, ["No members in this list."])); return [wrap]; }
    const listBox = el("div", { class: "tree-list" }, []);
    for (const m of detail.members) listBox.append(listMemberRow(m));
    wrap.append(listBox);
    return [wrap];
  }

  // ---- Configuration (#/config) ----

  // prefRow renders one reader-preference control: a label and a button whose
  // text is the current value; clicking runs onToggle and refreshes the label.
  function prefRow(labelText, getValue, onToggle) {
    const btn = el("button", { class: "pref-btn", type: "button" }, [getValue()]);
    btn.addEventListener("click", () => { onToggle(); btn.textContent = getValue(); });
    return el("div", { class: "pref-row" }, [el("span", { class: "pref-label mono" }, [labelText]), btn]);
  }

  // readerPrefsSection is the client-side reader preferences surface: theme,
  // layout width, sidebar collapse, and diff view mode, each reading/writing the
  // same localStorage key the header/inline controls use (theme/layout/
  // navCollapsed/diffview) so the two surfaces stay in sync. Theme/width/collapse
  // reuse the existing header buttons (keeping their icon state consistent);
  // diffview is written directly (it has no header control).
  function readerPrefsSection() {
    const wrap = el("div", { class: "config-section" }, []);
    wrap.append(el("div", { class: "config-head mono" }, ["Reader preferences"]));
    const body = document.body;
    const clickEl = (id) => { const e = document.getElementById(id); if (e) e.click(); };
    const diffMode = () => { try { return localStorage.getItem("diffview") === "split" ? "split" : "unified"; } catch (e) { return "unified"; } };
    wrap.append(prefRow("Theme",
      () => body.classList.contains("dark-mode") ? "dark" : "light",
      () => clickEl("theme-toggle")));
    wrap.append(prefRow("Layout width",
      () => body.classList.contains("wide") ? "full width" : "fixed",
      () => clickEl("width-toggle")));
    wrap.append(prefRow("Sidebar (desktop)",
      () => body.classList.contains("nav-collapsed") ? "collapsed" : "expanded",
      () => body.classList.contains("nav-collapsed") ? clickEl("nav-handle") : clickEl("nav-collapse")));
    wrap.append(prefRow("Diff view",
      diffMode,
      () => { const next = diffMode() === "split" ? "unified" : "split"; try { localStorage.setItem("diffview", next); } catch (e) { /* private mode */ } }));
    return wrap;
  }

  // siteConfigSection renders the pushed static-site customization
  // (.gitsocial/site/site-config.json: title/accent/accentDark/favicon), read-only
  // — the same fields applied to the tab title, accent token, and favicon. Null
  // when the bucket carries no customization (the reader keeps its defaults), so
  // the config page shows this section only when a site config was published.
  async function siteConfigSection(ctx) {
    const cfg = await loadSiteCustomization(ctx);
    if (!cfg || typeof cfg !== "object") return null;
    const rows = [];
    if (typeof cfg.title === "string" && cfg.title.trim()) rows.push(["title", el("span", {}, [cfg.title.trim()])]);
    const swatch = (hex) => el("span", { class: "config-swatch mono" }, [el("span", { class: "config-swatch-dot", style: "background:" + hex }, []), hex]);
    if (typeof cfg.accent === "string" && cfg.accent) rows.push(["accent", swatch(cfg.accent)]);
    if (typeof cfg.accentDark === "string" && cfg.accentDark) rows.push(["accentDark", swatch(cfg.accentDark)]);
    if (typeof cfg.favicon === "string" && /^data:image\//.test(cfg.favicon)) rows.push(["favicon", el("img", { class: "config-favicon", src: cfg.favicon, alt: "favicon" }, [])]);
    if (!rows.length) return null;
    const wrap = el("div", { class: "config-section" }, []);
    wrap.append(el("div", { class: "config-head mono" }, ["Site"]));
    const card = el("div", { class: "card config-ext" }, []);
    const dl = el("dl", {}, []);
    for (const [k, v] of rows) { dl.append(el("dt", {}, [k])); dl.append(el("dd", {}, [v])); }
    card.append(dl);
    wrap.append(card);
    return wrap;
  }

  // repoConfigSection renders each extension's in-bucket refs/gitmsg/<ext>/config
  // JSON (read-only) as formatted key/value; an absent config shows "defaults".
  async function repoConfigSection(ctx) {
    const wrap = el("div", { class: "config-section" }, []);
    wrap.append(el("div", { class: "config-head mono" }, ["Repository configuration"]));
    for (const ext of ["social", "pm", "review", "release", "memo"]) {
      const cfg = await loadExtConfig(ctx, ext);
      const card = el("div", { class: "card config-ext" }, []);
      card.append(el("div", { class: "config-ext-head mono" }, [ext]));
      if (!cfg || !Object.keys(cfg).length) {
        card.append(el("div", { class: "meta" }, ["defaults"]));
      } else {
        const dl = el("dl", {}, []);
        for (const k of Object.keys(cfg).sort()) { dl.append(el("dt", {}, [k])); dl.append(el("dd", {}, [String(cfg[k])])); }
        card.append(dl);
      }
      wrap.append(card);
    }
    return wrap;
  }

  // forksSection lists the repo's registered forks (mirroring the TUI Config →
  // Forks view), read from the fork refs in the bucket manifest. Each row shows
  // the fork's repo URL (scheme stripped, like the TUI), linked for http(s) URLs.
  // The TUI's commit-count / last-fetch columns are cache-derived and unavailable
  // to a browser reader, so only URLs show. Null when the bucket has no forks.
  // FORKS_CAP is how many forks the config page shows before an expand control:
  // a repo with hundreds of forks (they arrive most-recently-updated first) must
  // not dump the whole list into the page.
  const FORKS_CAP = 10;

  async function forksSection(ctx) {
    const forks = await loadForks(ctx);
    if (!forks.length) return null;
    const wrap = el("div", { class: "config-section" }, []);
    const head = el("div", { class: "config-head mono" }, ["Forks (" + forks.length + ")"]);
    wrap.append(head);
    const forkRow = (f) => {
      const row = el("div", { class: "tree-row" }, []);
      const shown = f.url.replace(/^https?:\/\//, "");
      if (/^https?:\/\//.test(f.url)) row.append(el("a", { class: "mono", href: f.url }, [shown]));
      else row.append(el("span", { class: "mono selectable" }, [shown]));
      return row;
    };
    // Under the cap: the plain list, no controls (the common case).
    if (forks.length <= FORKS_CAP) {
      const list = el("div", { class: "tree-list" }, []);
      for (const f of forks) list.append(forkRow(f));
      wrap.append(list);
      return wrap;
    }
    // Over the cap: show the top FORKS_CAP (most recently updated) with a "Show
    // all N" toggle; expanding reveals a filter input over a scrollable list,
    // consistent with the analytics top-authors surface.
    let expanded = false;
    const list = el("div", { class: "tree-list contrib-scroll" }, []);
    const filter = el("input", { class: "contrib-filter", type: "text", placeholder: "Filter forks…", "aria-label": "Filter forks", autocomplete: "off", spellcheck: "false" }, []);
    filter.style.display = "none";
    const toggle = el("button", { class: "load-more" }, ["Show all " + forks.length + " forks"]);
    const draw = () => {
      const q = (filter.value || "").trim().toLowerCase();
      const base = expanded ? forks : forks.slice(0, FORKS_CAP);
      const rows = q ? base.filter((f) => f.url.toLowerCase().indexOf(q) !== -1) : base;
      list.replaceChildren();
      if (!rows.length) { list.append(el("div", { class: "empty" }, ["No forks match “" + filter.value + "”."])); return; }
      for (const f of rows) list.append(forkRow(f));
    };
    toggle.addEventListener("click", () => {
      expanded = !expanded;
      toggle.textContent = expanded ? "Show top " + FORKS_CAP : "Show all " + forks.length + " forks";
      filter.style.display = expanded ? "" : "none";
      list.classList.toggle("contrib-scroll", expanded);
      if (!expanded) filter.value = "";
      draw();
    });
    wrap.append(filter, list, toggle);
    // Start collapsed: no scroll region, no filter, just the top FORKS_CAP.
    list.classList.remove("contrib-scroll");
    draw();
    return wrap;
  }

  // configView renders the #/config page: the client-side reader preferences
  // (a second surface over the header toggles), the registered forks (when any),
  // and the read-only repository configuration (each extension's in-bucket config
  // JSON).
  async function configView(ctx) {
    const wrap = el("div", { class: "detail config-view" }, []);
    wrap.append(el("div", { class: "subject" }, ["Configuration"]));
    wrap.append(readerPrefsSection());
    const site = await siteConfigSection(ctx);
    if (site) wrap.append(site);
    const forks = await forksSection(ctx);
    if (forks) wrap.append(forks);
    wrap.append(await repoConfigSection(ctx));
    return [wrap];
  }

  // branchesView lists the repo's branches with the default marked.
  async function branchesView(ctx) {
    const { branches, defaultBranch } = await listBranches(ctx);
    if (!branches.length) return [el("div", { class: "empty" }, ["No branches in this repository."])];
    const nodes = [];
    // A page-level compare affordance opens the compare picker (default branch as
    // the base) so the user reaches it without hand-writing a route.
    if (defaultBranch) nodes.push(el("div", { class: "page-actions" }, [
      el("a", { class: "action-link", href: compareRef(defaultBranch, "") }, ["⇄ Compare branches"]),
    ]));
    for (const b of branches) {
      const card = el("div", { class: "card" }, []);
      const head = el("div", { class: "card-head" }, [el("a", { class: "subject mono", href: "#branch:" + b.name }, [b.name])]);
      if (b.isDefault) head.append(el("span", { class: "chip" }, ["default"]));
      // Per-branch compare: base = default branch, head = this branch (the common
      // "what's on this branch vs main" question); a self-compare on the default
      // branch is dropped (nothing to compare).
      if (defaultBranch && b.name !== defaultBranch) head.append(el("a", { class: "hash compare-link", href: compareRef(defaultBranch, b.name) }, ["compare"]));
      card.append(head);
      nodes.push(card);
    }
    return nodes;
  }

  // tagsView renders the tags page (#/tags): every tag in the refs manifest
  // (refs/tags/*), each a card linking to its resolved commit detail (#tag:<name>)
  // plus the short target sha. Empty state consistent with the repository
  // phrasing when no tags were pushed (or the manifest is absent).
  async function tagsView(ctx) {
    const tags = await listTags(ctx);
    if (!tags.length) return [el("div", { class: "empty" }, ["No tags in this repository."])];
    return tags.map((t) => {
      const card = el("div", { class: "card" }, []);
      const head = el("div", { class: "card-head" }, [
        el("a", { class: "subject mono", href: "#tag:" + t.name }, [t.name]),
        el("a", { class: "hash", href: "#tag:" + t.name }, [t.sha.slice(0, 12)]),
      ]);
      card.append(head);
      return cardTagNav(card, t.name);
    });
  }

  // cardTagNav makes a whole tag card navigate to its #tag:<name> route on a
  // click anywhere in it (inner anchors and active selections excepted).
  function cardTagNav(card, name) {
    card.className += " clickable";
    card.addEventListener("click", (e) => {
      if (e && e.target && e.target.closest && e.target.closest("a")) return;
      const sel = typeof window !== "undefined" && window.getSelection ? window.getSelection() : null;
      if (sel && !sel.isCollapsed) return;
      location.hash = "#tag:" + name;
    });
    return card;
  }

  // parseTagger parses a raw tag-object tagger line value ("Name <email> ts tz")
  // into { name, email, time }; null when absent or unparseable (lightweight
  // tags have no tag object, so no tagger).
  function parseTagger(tagger) {
    const m = /^(.*) <([^>]*)> (\d+) /.exec(tagger || "");
    return m ? { name: m[1], email: m[2], time: parseInt(m[3], 10) } : null;
  }

  // tagDetail renders the tag page (#tag:<name>) milestone-shaped: a tag-centric
  // header (name, signed chip, annotation message, tagger — or the tagged
  // commit's author for lightweight tags — and a link row to the tagged commit),
  // then the commits the tag introduces over the previous tag (version order,
  // the changelog neighbor), then the file diff against that previous tag —
  // three-dot semantics like the compare page, not the tagged commit's own
  // diff. An annotated tag is peeled to its commit (peelTag chases the tag
  // object's `object` line); a lightweight tag points straight at the commit.
  // Not found when the tag is absent from the manifest or unreachable.
  async function tagDetail(ctx, name) {
    const tags = await listTags(ctx);
    const t = tags.find((x) => x.name === name);
    if (!t) return [el("div", { class: "err" }, ["Tag not found: " + name])];
    const peeled = await peelTag(ctx, t.sha);
    if (!peeled.commit) return [el("div", { class: "err" }, ["Tag target unreachable: " + name])];
    const cobj = await getObject(ctx, peeled.commit);
    const c = cobj && cobj.type === "commit" ? parseCommit(peeled.commit, cobj.body) : null;

    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("a", { class: "back", href: detailBackHref(ctx, "#/tags") }, ["← back"]));
    const subject = el("div", { class: "subject" }, [name]);
    // The PGP/SSH signature block is stripped from the annotation (peelTag);
    // a small unobtrusive chip stands in for it rather than dumping the armor.
    if (peeled.signed) subject.append(" ", el("span", { class: "chip chip-signed" }, ["✓ signed"]));
    wrap.append(subject);
    const meta = el("span", { class: "meta" }, []);
    const tagger = parseTagger(peeled.tagger);
    if (tagger) meta.append(authorEl(tagger.name, tagger.email), " · ", timeEl(tagger.time), " · ");
    else if (c) meta.append(commitAuthorEl(c), " · ", timeEl(c.authorTime), " · ");
    const cLink = el("a", { class: "hash", href: commitRef(peeled.commit, "") }, [peeled.commit.slice(0, 12)]);
    meta.append("tagged commit ", cLink);
    if (c) meta.append(" ", el("a", { href: commitRef(peeled.commit, "") }, [subjectBody(c.content)[0] || ""]));
    wrap.append(el("div", { class: "detail-meta" }, [meta]));
    if (peeled.message) wrap.append(el("div", { class: "tag-annotation" }, [el("div", { class: "body" }, [peeled.message])]));

    const idx = tags.findIndex((x) => x.name === name);
    const prev = idx >= 0 ? tags[idx + 1] : undefined;
    const prevCommit = prev ? (await peelTag(ctx, prev.sha)).commit : null;
    if (prevCommit) wrap.append(el("div", { class: "page-actions" }, [
      el("a", { class: "action-link", href: compareRef(prev.name, name) }, ["⇄ compare with " + prev.name]),
    ]));
    wrap.append(await tagCommitsSection(ctx, prev, prevCommit, peeled.commit));

    // File diff against the previous tag, three-dot like compareView: merge-base
    // (normally the previous tag itself on linear history) vs this tag's tree.
    // Skipped for the oldest tag (nothing to diff against) and for a previous
    // tag on the same commit (empty by definition).
    if (prevCommit && prevCommit !== peeled.commit) {
      const mb = await mergeBase(ctx, peeled.commit, prevCommit, DETAIL_WALK_CAP);
      const headTree = await commitTree(ctx, peeled.commit);
      const baseTree = await commitTree(ctx, mb || prevCommit);
      if (headTree && baseTree) {
        const entries = await diffTrees(ctx, baseTree, headTree);
        wrap.append(diffSection(ctx, entries, "Files changed since " + prev.name, mb ? [] : ["no common ancestor — raw two-dot diff"]));
      }
    }
    return [wrap];
  }

  // commitMemberRow is a compact short-hash + linked-subject one-liner for a
  // commit listed on the tag page — the milestone member-row style
  // (issueMemberRow), with the mono hash standing in the state chip's slot.
  function commitMemberRow(c) {
    return el("div", { class: "pm-member" }, [
      el("a", { class: "hash mono", href: commitRef(c.hash, "") }, [c.short]),
      el("a", { href: commitRef(c.hash, "") }, [subjectBody(c.content)[0] || "(no message)"]),
    ]);
  }

  // tagCommitsSection lists the commits a tag introduces over the previous tag:
  // commits reachable from the tag's commit but not from the previous tag's,
  // newest-first, paged, as milestone-style member one-liners under a counted
  // "Commits since <prev> (n)" header (the count grows with each loaded window;
  // the Load more control itself signals a deeper history). The oldest tag — or
  // one whose previous tag can't be peeled to a commit — lists the tag's full
  // history instead.
  async function tagCommitsSection(ctx, prev, prevCommit, commit) {
    const countEl = el("span", {}, ["0"]);
    const label = prevCommit ? "Commits since " + prev.name : "Commits";
    const head = el("div", { class: "pm-members-head mono" }, [label + " (", countEl, ")"]);
    const wrap = el("div", { class: "pm-members" }, [head]);
    const first = await loadCompareCommitsWindow(ctx, prevCommit || "", commit, false);
    if (!first.items.length) wrap.append(el("div", { class: "empty" }, [prevCommit ? "No commits since " + prev.name + "." : "No commits."]));
    else for (const n of pagedListView(first,
      (commits, box) => {
        countEl.textContent = String(commits.length);
        box.replaceChildren(...commits.map(commitMemberRow));
      },
      () => loadCompareCommitsWindow(ctx, prevCommit || "", commit, true))) wrap.append(n);
    return wrap;
  }

  // branchLogCard renders one commit row for the branch log (subject + author/
  // time meta + hash link), whole-card navigable to the commit detail.
  function branchLogCard(c, name) {
    const card = el("div", { class: "card" }, []);
    card.append(el("div", { class: "subject" }, [subjectBody(c.content)[0] || "(no message)"]));
    const meta = el("span", { class: "meta" }, [
      commitAuthorEl(c), " · ", timeEl(c.authorTime), " · ",
    ]);
    meta.append(el("a", { class: "hash", href: commitRef(c.hash, name) }, [c.short]));
    card.append(meta);
    return cardNav(card, c.hash, name);
  }

  // branchLogView renders a branch's commit log, paged: the first WALK_CAP window
  // with a "Load more" control that walks the next window when the history runs
  // deeper.
  async function branchLogView(ctx, name) {
    const first = await loadBranchLogWindow(ctx, name, false);
    if (!first.tip) return [el("div", { class: "err" }, ["Branch not found: " + name])];
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("div", { class: "subject" }, [name]));
    const actions = el("div", { class: "page-actions" }, [
      el("a", { class: "action-link", href: fileRef("", name) }, ["browse files →"]),
    ]);
    // Compare this branch against the default branch (base = default, head = this).
    const { defaultBranch } = await listBranches(ctx);
    if (defaultBranch && defaultBranch !== name) actions.append(el("a", { class: "action-link", href: compareRef(defaultBranch, name) }, ["⇄ compare with " + defaultBranch]));
    wrap.append(actions);
    if (!first.items.length) { wrap.append(el("div", { class: "empty" }, ["No commits on this branch."])); return [wrap]; }
    for (const n of pagedListView(first,
      (commits, box) => box.replaceChildren(...commits.map((c) => branchLogCard(c, name))),
      () => loadBranchLogWindow(ctx, name, true))) wrap.append(n);
    return [wrap];
  }

  // comparePicker builds one labeled base/head ref selector: an <optgroup>ed
  // <select> of the repo's branches then tags, preselecting `current`. Changing
  // it navigates to the compare route with the other side held fixed. A ref that
  // is neither a branch nor a tag (a stale/foreign name) still shows as the
  // selected option so the picker reflects the URL.
  function comparePicker(label, current, branches, tags, otherSide, isBase) {
    const sel = el("select", { class: "compare-select mono", "aria-label": label }, []);
    const optFor = (name) => el("option", Object.assign({ value: name }, name === current ? { selected: "selected" } : {}), [name]);
    if (branches.length) {
      const g = el("optgroup", { label: "Branches" }, branches.map((b) => optFor(b.name)));
      sel.append(g);
    }
    if (tags.length) {
      const g = el("optgroup", { label: "Tags" }, tags.map((t) => optFor(t.name)));
      sel.append(g);
    }
    if (current && !branches.some((b) => b.name === current) && !tags.some((t) => t.name === current)) {
      sel.append(el("option", { value: current, selected: "selected" }, [current + " (unknown)"]));
    }
    sel.addEventListener("change", () => {
      const val = sel.value;
      location.hash = isBase ? compareRef(val, otherSide) : compareRef(otherSide, val);
    });
    return el("label", { class: "compare-field" }, [el("span", { class: "meta" }, [label]), sel]);
  }

  // compareView renders the branch/tag compare page (#/compare:<base>...<head>).
  // It offers base/head pickers (branches + tags), resolves both to commits in
  // this bucket, and shows GitHub-style three-dot semantics: the file diff is
  // base=merge-base(base,head) vs head (reusing the commit diff renderer), and a
  // commit list of the head-side commits since the merge-base. Same-ref compares
  // are an empty state; unrelated histories fall back to a two-dot diff with a
  // caveat; missing refs surface a clear message. The pickers always render so
  // the user can pick even when a side is blank or unresolved.
  async function compareView(ctx, baseName, headName) {
    const { branches, defaultBranch } = await listBranches(ctx);
    const tags = await listTags(ctx);
    if (!baseName && defaultBranch) baseName = defaultBranch;
    const wrap = el("div", { class: "detail compare" }, []);
    wrap.append(el("div", { class: "subject" }, ["Compare"]));
    const pickers = el("div", { class: "compare-pickers" }, [
      comparePicker("base", baseName, branches, tags, headName, true),
      el("span", { class: "compare-dots meta" }, ["..."]),
      comparePicker("head", headName, branches, tags, baseName, false),
    ]);
    wrap.append(pickers);
    if (!baseName || !headName) {
      wrap.append(el("div", { class: "empty" }, ["Choose a base and a head ref to compare."]));
      return [wrap];
    }
    const baseR = await resolveCompareRef(ctx, baseName);
    const headR = await resolveCompareRef(ctx, headName);
    if (!baseR) { wrap.append(el("div", { class: "err" }, ["Base ref not found in this bucket: " + baseName])); return [wrap]; }
    if (!headR) { wrap.append(el("div", { class: "err" }, ["Head ref not found in this bucket: " + headName])); return [wrap]; }
    if (baseR.sha === headR.sha) {
      wrap.append(el("div", { class: "empty" }, ["These refs point at the same commit; there is nothing to compare."]));
      return [wrap];
    }
    // Three-dot semantics: diff the merge-base against head. No common ancestor
    // (unrelated histories) falls back to a raw two-dot diff with a caveat.
    const mb = await mergeBase(ctx, headR.sha, baseR.sha, DETAIL_WALK_CAP);
    const headTree = await commitTree(ctx, headR.sha);
    const baseTree = await commitTree(ctx, baseR.sha);
    if (!headTree || !baseTree) { wrap.append(el("div", { class: "err" }, ["A ref's commit objects are missing from this bucket."])); return [wrap]; }
    const caveats = [];
    let leftTree = baseTree;
    if (mb) leftTree = await commitTree(ctx, mb) || baseTree;
    else caveats.push("no common ancestor — raw two-dot diff");
    // Head-side commit list (commits head has since the merge-base, or since base
    // when there is no merge base), paged. Rendered above the file diff like a PR.
    const excludeFrom = mb || baseR.sha;
    const first = await loadCompareCommitsWindow(ctx, excludeFrom, headR.sha, false);
    const commitsWrap = el("div", { class: "compare-commits" }, []);
    commitsWrap.append(el("div", { class: "diff-head" }, [el("span", { class: "subject" }, ["Commits"])]));
    if (!first.items.length) commitsWrap.append(el("div", { class: "empty" }, ["No commits on head that base lacks (head is behind or level with base)."]));
    else for (const n of pagedListView(first,
      (commits, box) => box.replaceChildren(...commits.map((c) => branchLogCard(c, headR.kind === "branch" ? headName : ""))),
      () => loadCompareCommitsWindow(ctx, excludeFrom, headR.sha, true))) commitsWrap.append(n);
    wrap.append(commitsWrap);
    const entries = await diffTrees(ctx, leftTree, headTree);
    wrap.append(diffSection(ctx, entries, "Files changed", caveats));
    return [wrap];
  }

  // svgEl builds an SVG-namespaced element (document.createElement assigns the
  // HTML namespace, so SVG children never render — createElementNS is required).
  const SVG_NS = "http://www.w3.org/2000/svg";
  function svgEl(tag, attrs, children) {
    const node = document.createElementNS(SVG_NS, tag);
    // SVGElement.className is a read-only SVGAnimatedString, so set the class via
    // setAttribute; mirror it onto classList so headless class lookups (_cls) see
    // it too. Every other attribute is a plain setAttribute.
    if (attrs) for (const k in attrs) {
      node.setAttribute(k, attrs[k]);
      if (k === "class" && node.classList) for (const c of String(attrs[k]).split(/\s+/).filter(Boolean)) node.classList.add(c);
    }
    for (const c of children || []) node.append(c);
    return node;
  }

  // GRAPH_LANE_W / GRAPH_ROW_H / GRAPH_DOT_R set the graph gutter geometry: lane
  // horizontal pitch, per-row height, and the commit dot radius.
  const GRAPH_LANE_W = 18, GRAPH_ROW_H = 40, GRAPH_DOT_R = 4;
  // GRAPH_LANE_COLORS cycles per-lane colors (theme-agnostic hues legible on both
  // parchment and dark backgrounds); lane index mod length picks the color.
  const GRAPH_LANE_COLORS = ["#008787", "#8957e5", "#1f9d55", "#bf8700", "#cf222e", "#1a85d4", "#d5512f", "#693acf"];
  function graphLaneColor(lane) { return GRAPH_LANE_COLORS[lane % GRAPH_LANE_COLORS.length]; }

  // buildGraphGutter draws the lane gutter for the assigned rows as one inline
  // SVG: a colored dot per commit at its lane, and a line from each commit down to
  // each loaded parent's lane (a straight drop when the parent stays in-lane, an
  // elbow when it moves — a fork/merge). Edges to unloaded parents (past the
  // window) are omitted (the lane simply ends). Returns the <svg>.
  function buildGraphGutter(rows, laneCount) {
    const width = Math.max(1, laneCount) * GRAPH_LANE_W;
    const height = rows.length * GRAPH_ROW_H;
    const svg = svgEl("svg", { class: "graph-gutter", width: String(width), height: String(height), viewBox: "0 0 " + width + " " + height, "aria-hidden": "true" }, []);
    const cx = (lane) => lane * GRAPH_LANE_W + GRAPH_LANE_W / 2;
    const cy = (row) => row * GRAPH_ROW_H + GRAPH_ROW_H / 2;
    const rowOf = new Map();
    rows.forEach((r, i) => rowOf.set(r.commit.hash, i));
    // Edges first (under the dots).
    rows.forEach((r, i) => {
      for (const p of r.parents) {
        if (!p.present) continue;
        const pj = rowOf.get(p.sha);
        if (pj == null) continue;
        const x1 = cx(r.lane), y1 = cy(i), x2 = cx(p.lane), y2 = cy(pj);
        const color = graphLaneColor(x1 === x2 ? r.lane : p.lane);
        let d;
        if (x1 === x2) d = "M" + x1 + " " + y1 + " L" + x2 + " " + y2;
        else {
          // Elbow: drop, curve across near the parent row, then to the parent.
          const my = y2 - GRAPH_ROW_H / 2;
          d = "M" + x1 + " " + y1 + " L" + x1 + " " + my + " C" + x1 + " " + y2 + " " + x2 + " " + my + " " + x2 + " " + y2;
        }
        svg.append(svgEl("path", { d, fill: "none", stroke: color, "stroke-width": "2" }, []));
      }
    });
    rows.forEach((r, i) => {
      svg.append(svgEl("circle", { cx: String(cx(r.lane)), cy: String(cy(i)), r: String(GRAPH_DOT_R), fill: graphLaneColor(r.lane), stroke: "var(--bg)", "stroke-width": "1.5" }, []));
    });
    return svg;
  }

  // graphRefChips returns the ref decoration chips for one graph row (git log
  // --decorate style): live code-branch tips (the default branch as the solid
  // `default` variant), tags (lightweight only — an annotated tag's object sha
  // never matches a commit row; see loadGraphDecorations), and merged-PR
  // head-branch chips prefix-matched on the recorded merge-head/head-tip short
  // shas — rendered dashed/dimmed since the ref is historical, linked to the PR
  // detail when the canonical PR sha is known, and suppressed when the same
  // name is already on the row as a live tip.
  function graphRefChips(hash, decor) {
    if (!decor) return [];
    const chips = [];
    const live = decor.tips[hash] || [];
    for (const name of live) chips.push(el("span", { class: "chip branch-tip" + (name === decor.defaultBranch ? " default" : "") }, [name]));
    for (const name of decor.tags[hash] || []) chips.push(el("span", { class: "chip tag-tip" }, [name]));
    for (const m of decor.merged || []) {
      if (!hash.startsWith(m.short) || live.includes(m.name)) continue;
      chips.push(m.prSha
        ? el("a", { class: "chip branch-tip merged-branch", href: commitRef(m.prSha, "gitmsg/review"), title: "merged pull request" }, [m.name])
        : el("span", { class: "chip branch-tip merged-branch", title: "merged pull request" }, [m.name]));
    }
    return chips;
  }

  // graphRowText builds the text column for one graph row: ref decoration chips
  // (branch tips / tags / merged-PR branches), short hash (commit link),
  // subject, author, date.
  function graphRowText(r, decor) {
    const c = r.commit;
    const row = el("div", { class: "graph-row-text" }, []);
    for (const chip of graphRefChips(c.hash, decor)) row.append(chip);
    row.append(el("a", { class: "hash mono", href: commitRef(c.hash, "") }, [c.short]));
    row.append(el("span", { class: "graph-subject" }, [subjectBody(c.content)[0] || "(no message)"]));
    row.append(el("span", { class: "meta graph-meta" }, [
      commitAuthorEl(c), " · ", timeEl(c.authorTime),
    ]));
    return row;
  }

  // graphBody renders the assigned rows: an SVG lane gutter beside a stacked list
  // of per-row text lines (fixed GRAPH_ROW_H each so they align with the gutter).
  function graphBody(rows, laneCount, decor) {
    const gutter = buildGraphGutter(rows, laneCount);
    const textCol = el("div", { class: "graph-text-col" }, []);
    for (const r of rows) {
      const line = el("div", { class: "graph-line" }, [graphRowText(r, decor)]);
      line.style.height = GRAPH_ROW_H + "px";
      textCol.append(line);
    }
    return el("div", { class: "graph-body" }, [el("div", { class: "graph-gutter-wrap" }, [gutter]), textCol]);
  }

  // graphView renders the repository commit DAG (#/graph): a multi-branch,
  // time-ordered commit graph over the newest window of history (GRAPH_WINDOW
  // commits across all branch heads), with a "Load more" that walks the next
  // window and re-lays the lanes over the grown set. The lane gutter is inline SVG
  // (colored lane lines, fork/merge elbows, dots); each row shows ref decoration
  // chips (branch tips, tags, merged-PR branches — graphRefChips), the short
  // hash (commit link), subject, author, date. Empty when the repository has no
  // commits.
  async function graphView(ctx) {
    let data = await loadGraphWindow(ctx, false);
    const wrap = el("div", { class: "detail graph" }, []);
    wrap.append(el("div", { class: "subject" }, ["Commit graph"]));
    if (!data.commits.length) { wrap.append(el("div", { class: "empty" }, ["No commits in this repository."])); return [wrap]; }
    // The gutter+text are re-rendered from scratch on each window (lanes shift as
    // parents that were off-window become present), so a load-more redraws the
    // whole graph rather than appending — correct over clever, per the brief. The
    // horizontal-scroll wrapper keeps the lane gutter usable on narrow screens.
    const scroll = el("div", { class: "graph-scroll" }, []);
    let moreWrap = null;
    function render() {
      const { rows, laneCount } = assignGraphLanes(data.commits);
      scroll.replaceChildren(graphBody(rows, laneCount, data.decor));
      if (moreWrap) { moreWrap.remove(); moreWrap = null; }
      if (!data.truncated) return;
      const btn = el("button", { class: "load-more", type: "button" }, ["Load more"]);
      moreWrap = el("div", { class: "load-more-wrap" }, [btn]);
      btn.addEventListener("click", async () => {
        btn.disabled = true; btn.textContent = "Loading…";
        try { data = await loadGraphWindow(ctx, true); render(); }
        catch (e) { btn.disabled = false; btn.textContent = "Load more"; }
      });
      wrap.append(moreWrap);
    }
    wrap.append(scroll);
    render();
    return [wrap];
  }

  // treeOrBlob resolves a path on a branch and renders a directory or file.
  async function treeOrBlob(ctx, path, branch, line, lineEnd) {
    const tip = await refTip(ctx, "refs/heads/" + branch);
    if (!tip) return [el("div", { class: "err" }, ["Branch not found: " + branch])];
    const node = await resolvePath(ctx, tip, path);
    if (!node) return [el("div", { class: "err" }, ["Path not found: " + path])];
    if (node.type === "tree") {
      const entries = await getTree(ctx, node.sha);
      return treeView(ctx, entries || [], path, branch);
    }
    const obj = await getObject(ctx, node.sha);
    if (!obj) return [el("div", { class: "err" }, ["Object not found."])];
    return blobView(obj.body, path, branch, line, lineEnd, ctx);
  }

  // codeView renders the root tree of the default branch.
  async function codeView(ctx) {
    const head = await resolveHead(ctx.base);
    const branch = headBranchName(head);
    if (!branch || !head.sha) return [el("div", { class: "err" }, ["No default branch to browse."])];
    const entries = await getTree(ctx, (await resolvePath(ctx, head.sha, "")).sha);
    return treeView(ctx, entries || [], "", branch);
  }

  // findReadme locates a README entry in a set of root-tree entries.
  function findReadme(entries) {
    const names = ["readme.md", "readme", "readme.markdown", "readme.txt"];
    for (const want of names) {
      const e = entries.find((x) => x.type !== "tree" && x.name.toLowerCase() === want);
      if (e) return e;
    }
    return null;
  }

  // homeView is the repo landing page: README (minimal Markdown) above a
  // metadata strip (default branch, latest commit, branch count).
  const HOME_FILE_LIMIT = 3;

  // CHEVRON_SVG holds the two inline chevron glyphs (down = expand, up =
  // collapse) drawn to match the vendored icons' 16x16 viewBox. They are
  // trusted static assets, so they parse through the same inert DOMParser path
  // as the icon set, never the untrusted-HTML sanitizer.
  const CHEVRON_SVG = {
    down: "<svg fill=\"none\" viewBox=\"0 0 16 16\"><path stroke=\"currentColor\" stroke-width=\"1.6\" stroke-linecap=\"round\" stroke-linejoin=\"round\" d=\"m3.5 6 4.5 4.5L12.5 6\"/></svg>",
    up: "<svg fill=\"none\" viewBox=\"0 0 16 16\"><path stroke=\"currentColor\" stroke-width=\"1.6\" stroke-linecap=\"round\" stroke-linejoin=\"round\" d=\"m3.5 10 4.5-4.5L12.5 10\"/></svg>",
  };
  const chevronTemplates = new Map();

  // chevronEl clones one chevron glyph into a themed span (iconEl's output
  // shape), or null when DOMParser is unavailable so callers can fall back.
  function chevronEl(dir) {
    if (!chevronTemplates.has(dir)) {
      let node = null;
      try {
        const body = new DOMParser().parseFromString(CHEVRON_SVG[dir], "text/html").body;
        const svgs = body && body.querySelectorAll ? body.querySelectorAll("svg") : [];
        node = svgs && svgs.length ? svgs[0] : null;
      } catch (e) { node = null; }
      chevronTemplates.set(dir, node);
    }
    const tpl = chevronTemplates.get(dir);
    if (!tpl) return null;
    const svg = tpl.cloneNode(true);
    if (svg.setAttribute) svg.setAttribute("aria-hidden", "true");
    return el("span", { class: "gs-icon chevron" }, [svg]);
  }

  // SEARCH_SVG is the Code nav item's magnifier glyph (16x16, currentColor),
  // parsed through the same trusted DOMParser template path as the icon set and
  // chevrons, never the untrusted-HTML sanitizer.
  const SEARCH_SVG = "<svg fill=\"none\" viewBox=\"0 0 16 16\"><circle cx=\"7\" cy=\"7\" r=\"4.25\" stroke=\"currentColor\" stroke-width=\"1.5\"/><path stroke=\"currentColor\" stroke-width=\"1.5\" stroke-linecap=\"round\" d=\"m10.5 10.5 3 3\"/></svg>";
  let searchTemplate;

  // searchIconEl clones the magnifier into a themed span (iconEl's output shape),
  // or null when DOMParser is unavailable so the caller can fall back to a glyph.
  function searchIconEl() {
    if (searchTemplate === undefined) {
      let node = null;
      try {
        const body = new DOMParser().parseFromString(SEARCH_SVG, "text/html").body;
        const svgs = body && body.querySelectorAll ? body.querySelectorAll("svg") : [];
        node = svgs && svgs.length ? svgs[0] : null;
      } catch (e) { node = null; }
      searchTemplate = node;
    }
    if (!searchTemplate) return null;
    const svg = searchTemplate.cloneNode(true);
    if (svg.setAttribute) svg.setAttribute("aria-hidden", "true");
    return el("span", { class: "gs-icon nav-search-icon" }, [svg]);
  }

  // homeFileList renders the root entries (directories first, then files) as a
  // GitHub-style file listing on Home. A long listing collapses to the first
  // HOME_FILE_LIMIT rows behind a centered chevron control (down = "Show all N",
  // up = "Show less"), with a gradient fade over the last visible row so the
  // truncation reads. The fade layer takes no pointer events, so the visible
  // rows stay clickable through it.
  function homeFileList(entries, branch) {
    const dirs = entries.filter((e) => e.type === "tree").sort((a, b) => a.name.localeCompare(b.name));
    const files = entries.filter((e) => e.type !== "tree").sort((a, b) => a.name.localeCompare(b.name));
    const all = dirs.concat(files);
    const box = el("div", {}, []);
    const listNode = el("div", { class: "tree-list home-list" }, []);
    const rows = all.map((e) => el("a", { class: "tree-row", href: fileRef(e.name, branch) }, [
      treeIcon(e), el("span", { class: "mono" }, [e.name]),
    ]));
    for (const r of rows) listNode.append(r);
    box.append(listNode);
    if (all.length <= HOME_FILE_LIMIT) return box;
    const fade = el("div", { class: "tree-fade" }, []);
    const glyph = el("span", { class: "show-more-icon" }, []);
    const label = el("span", { class: "show-more-label" }, []);
    const toggle = el("button", { class: "show-more", type: "button" }, [glyph, label]);
    let expanded = false;
    const apply = () => {
      rows.forEach((r, i) => { r.style.display = expanded || i < HOME_FILE_LIMIT ? "" : "none"; });
      glyph.replaceChildren(chevronEl(expanded ? "up" : "down") || document.createTextNode(expanded ? "⌃" : "⌄"));
      label.textContent = expanded ? "Show less" : "Show all " + all.length;
      if (expanded) fade.remove();
      else listNode.append(fade);
    };
    toggle.addEventListener("click", () => { expanded = !expanded; apply(); });
    apply();
    box.append(toggle);
    return box;
  }

  // homeView is the GitHub-familiar repo landing: a metadata strip (branch,
  // branch count, latest commit), the root file listing (directories first), and
  // the rendered README below it when present. The commit-count/contributor
  // summary lives on its own Analytics page (analyticsView), not here.
  async function homeView(ctx) {
    const head = await resolveHead(ctx.base);
    const branch = headBranchName(head);
    const wrap = el("div", { class: "detail" }, []);
    const { branches } = await listBranches(ctx);
    if (!branch || !head.sha) {
      wrap.append(el("div", { class: "empty" }, ["No default branch found."]));
      return [wrap];
    }
    const root = await resolvePath(ctx, head.sha, "");
    const entries = (root && await getTree(ctx, root.sha)) || [];
    const readme = findReadme(entries);
    const commitObj = await getObject(ctx, head.sha);
    const latest = commitObj && commitObj.type === "commit" ? parseCommit(head.sha, commitObj.body) : null;
    const strip = el("div", { class: "meta-strip" }, []);
    strip.append(el("span", { class: "chip" }, [branch]));
    strip.append(el("a", { class: "chip", href: "#/branches" }, [branches.length + (branches.length === 1 ? " branch" : " branches")]));
    if (latest) {
      const m = el("span", { class: "meta" }, [subjectBody(latest.content)[0] + " · ", timeEl(latest.authorTime), " · "]);
      m.append(el("a", { class: "hash", href: commitRef(latest.hash, branch) }, [latest.short]));
      strip.append(m);
    }
    wrap.append(strip);
    if (entries.length) wrap.append(homeFileList(entries, branch));
    if (readme) {
      const obj = await getObject(ctx, readme.sha);
      if (obj) wrap.append(renderMarkdown(new TextDecoder().decode(obj.body), { ctx, branch, dir: "" }));
    }
    return [wrap];
  }

  // codeSidebarTarget maps a route to the sidebar file tree's active repo path,
  // or null when the route is not a code-browsing context (so every other route
  // keeps the plain nav). #/code and directory routes highlight a directory;
  // blob routes highlight the open file.
  function codeSidebarTarget(r) {
    if (!r) return null;
    if (r.type === "code") return { path: "", branch: null };
    if (r.type === "file") return { path: r.path || "", branch: r.branch || null };
    return null;
  }

  // updateCodeSidebar fills (or clears) #nav-tree-slot with a repo file tree
  // whenever the route is a code/dir/blob context, so files stay navigable from
  // the sidebar without walking back through breadcrumbs (the GitHub code-view
  // file panel). It shares ctx.treeExpanded and the ctx object cache with the
  // content-pane tree, so expanding is reflected across both and it issues no
  // GETs beyond the ancestor trees resolvePath already warmed for the content.
  // The active path is highlighted and its ancestors auto-expanded. Any failure
  // just clears the slot; it never disturbs the content route.
  async function updateCodeSidebar(ctx, r) {
    const slot = typeof document !== "undefined" && document.getElementById ? document.getElementById("nav-tree-slot") : null;
    if (!slot) return;
    const target = codeSidebarTarget(r);
    if (!target) { slot.replaceChildren(); return; }
    try {
      const head = await resolveHead(ctx.base);
      const branch = target.branch || headBranchName(head);
      if (!branch) { slot.replaceChildren(); return; }
      const tip = await refTip(ctx, "refs/heads/" + branch);
      if (!tip) { slot.replaceChildren(); return; }
      const rootNode = await resolvePath(ctx, tip, "");
      const rootEntries = rootNode && (await getTree(ctx, rootNode.sha));
      if (!rootEntries) { slot.replaceChildren(); return; }
      // Auto-expand the active path's ancestor directories (and the target dir
      // itself) so the active row is visible in the sidebar hierarchy.
      const parts = target.path ? target.path.split("/") : [];
      for (let n = 1; n < parts.length; n++) ctx.treeExpanded.add(parts.slice(0, n).join("/"));
      if (parts.length) {
        const node = await resolvePath(ctx, tip, target.path);
        if (node && node.type === "tree") ctx.treeExpanded.add(target.path);
      }
      const listNode = el("div", { class: "tree-list nav-tree-list" }, []);
      mountTree(ctx, listNode, rootEntries, "", branch, { expanded: ctx.treeExpanded, activePath: target.path });
      slot.replaceChildren(listNode);
    } catch (e) { slot.replaceChildren(); }
  }


  Object.assign(NS, { analyticsView, mdSlug, analyticsAuthors, authorEl, commitAuthorEl, autoScrollListView, boardView, boardBody, detailBackHref, branchLogView, branchesView, compareView, ensureGrammar, setGrammarBase, highlightTo, graphView, codeSidebarTarget, codeView, comingSoon, commitDetail, configView, el, filteredListView, focusSearchInput, focusTreeSearch, fullscreenBtn, highlightNav, homeView, hrefOk, icon, iconEl, imageExt, issuesBody, issuesView, milestonesBody, sprintsBody, itemDetail, joinPath, listDetailView, listsView, memoCard, metaRow, mountTree, openFullscreen, pagedListView, prCard, PR_STATES, preciseTime, releaseCard, renderCommitBody, renderInline, renderList, renderMarkdown, revokeObjectUrls, sanitizeHtml, sanitizeInert, searchIconEl, searchView, setView, tagsView, tagDetail, timelineCard, treeOrBlob, updateCodeSidebar });
  if (typeof module !== "undefined" && module.exports) module.exports = NS;
})();
