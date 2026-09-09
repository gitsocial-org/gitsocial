// gs-render.js - DOM rendering: el and the sanitizer, markdown, cards, views and icon builders on the GS namespace

if (typeof module !== "undefined" && module.exports) require("./gs-core.js");
(function () {
  const root = (typeof globalThis !== "undefined") ? globalThis : (typeof window !== "undefined" ? window : this);
  const NS = root.GS || (root.GS = {});
  const { COMMIT_VIEW, CONCURRENCY, DETAIL_WALK_CAP, THREAD_MAX_DEPTH, WALK_CAP, activityBuckets, anchorFeedback, buildBoard, buildHunks, buildIssueHierarchy, commitRef, compareRef, resolveCompareRef, commitTree, diffLines, diffTrees, effectiveAuthor, effectiveAuthorEmail, effectiveTime, embeddedRefs, fileDiff, findItemDeep, headFor, flattenThread, getObject, getContentObject, getTree, groupPM, groupThread, hashEq, headBranchName, hunkLineKeys, hydrateItems, iconColorClass, iconName, intraLine, isBinary, isBodyOnly, itemLabels, itemSubject, stripLinkRefDefs, listBranches, listTags, peelTag, listMemberRef, loadAnalyticsData, loadHomeActivity, loadSiteStats, loadBranchLogWindow, loadCommitsPage, loadCompareCommitsWindow, loadGraphWindow, assignGraphLanes, loadExtConfig, loadExtItems, loadExtItemsAll, loadExtItemsUpTo, loadForks, loadListDetail, loadListsSummary, loadSearchWindow, manifestFor, forkRefNames, loadSiteConfig, loadSiteCustomization, loadInteractionCounts, countsFor, fullSearchBytes, mergeBase, resolveMergeBase, parseBranchField, parseCommit, parseMarkdown, parentRef, parentQuote, pmParentHash, pmProgress, prFeedback, quotedRefFor, refBranch, refHash, refRepoUrl, refTip, releaseAssets, resolveAncestors, resolveHead, resolvePath, resolveShortShaFromIndex, reviewSummary, searchItemsFaceted, stateCounts, typeGlyph, suggestionBody, topItemAuthors, walkHistory, parseRoute, SWIMLANE_FIELDS, SWIMLANE_LABELS, swimlaneValue, swimlaneOrder, groupBySwimlane, swimlaneLabel } = NS;

  // BACK_ROUTES are the route types a detail page's back link may return to; detail routes are excluded.
  const BACK_ROUTES = { index: 1, board: 1, search: 1, home: 1, branches: 1, tags: 1, lists: 1, list: 1, analytics: 1, code: 1 };

  // detailBackHref returns the in-app list route the reader came from, else the view's default.
  function detailBackHref(ctx, defaultHash) {
    const from = ctx && ctx.backFrom;
    if (from) {
      const r = parseRoute(from);
      if (r && BACK_ROUTES[r.type]) return from;
    }
    return defaultHash;
  }

  // ---- Rendering (browser only; never invoked from Node) ----

  // relTime formats a unix timestamp as a coarse "N ago" string.
  function relTime(unixSeconds) {
    const diff = Date.now() / 1000 - unixSeconds;
    const units = [["y", 31536000], ["mo", 2592000], ["d", 86400], ["h", 3600], ["m", 60]];
    for (const [label, secs] of units) {
      const n = Math.floor(diff / secs);
      if (n >= 1) return n + label + " ago";
    }
    return "just now";
  }

  // tzAbbrev returns the reader's short timezone name, or a UTC offset when Intl is unavailable.
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

  // preciseTime formats a unix timestamp as a local "YYYY-MM-DD HH:MM TZ" string.
  function preciseTime(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    if (isNaN(d.getTime())) return "";
    const p = (n) => String(n).padStart(2, "0");
    const stamp = d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
    return stamp + " " + tzAbbrev(d);
  }

  // timeEl returns a relative-time span with the precise local time as its title.
  function timeEl(unixSeconds) {
    return el("span", { class: "reltime", title: preciseTime(unixSeconds) }, [relTime(unixSeconds)]);
  }

  // authorEl returns an author span with the email as its title when it differs from the label.
  function authorEl(name, email) {
    const label = name || email || "unknown";
    const attrs = { class: "author" };
    if (email && email !== label) attrs.title = email;
    return el("span", attrs, [label]);
  }

  // commitAuthorEl renders a commit's author: origin provenance first, then the git author, never the committer.
  function commitAuthorEl(c) {
    const header = (c && c.gitmsg) || null;
    return authorEl(effectiveAuthor(c, header), effectiveAuthorEmail(c, header));
  }

  // el creates an element with attributes and appended children.
  function el(tag, attrs, children) {
    const node = document.createElement(tag);
    if (attrs) for (const k in attrs) {
      if (k === "class") node.className = attrs[k];
      else node.setAttribute(k, attrs[k]);
    }
    for (const c of children || []) node.append(c);
    return node;
  }

  // gsIcons returns the vendored icon set, or null when icons.js is absent.
  function gsIcons() {
    if (typeof window !== "undefined" && window.GSIcons) return window.GSIcons;
    if (typeof GSIcons !== "undefined") return GSIcons;
    return null;
  }

  const iconTemplates = new Map();
  // iconTemplate parses a vendored SVG once through DOMParser and caches the node; null when unknown.
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

  // iconEl clones a cached icon template into a themed span, or null so callers fall back to text.
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

  // EXT_LANG maps a file extension to its Prism grammar; non-base grammars lazy-load from grammars/.
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
    jsx: "jsx", tsx: "tsx", scss: "scss", less: "less",
    m: "objectivec", mm: "objectivec",
    groovy: "groovy", gradle: "groovy", scala: "scala", sbt: "scala",
    dart: "dart", graphql: "graphql", gql: "graphql",
    ps1: "powershell", psm1: "powershell", psd1: "powershell",
    tf: "hcl", tfvars: "hcl", hcl: "hcl",
    ex: "elixir", exs: "elixir", mk: "makefile", cmake: "cmake",
    hs: "haskell", clj: "clojure", cljs: "clojure", cljc: "clojure", edn: "clojure",
    erl: "erlang", hrl: "erlang", pl: "perl", pm: "perl",
    r: "r", jl: "julia", ml: "ocaml", mli: "ocaml",
    fs: "fsharp", fsi: "fsharp", fsx: "fsharp",
    zig: "zig", nim: "nim", cr: "crystal", sol: "solidity",
    tex: "latex", sty: "latex", bat: "batch", cmd: "batch",
    vim: "vim", vue: "markup", svelte: "markup",
  };
  // BASENAME_LANG maps exact filenames to a grammar; consulted before EXT_LANG.
  const BASENAME_LANG = {
    Dockerfile: "docker", Makefile: "makefile", GNUmakefile: "makefile",
    makefile: "makefile", "CMakeLists.txt": "cmake", Jenkinsfile: "groovy",
    Gemfile: "ruby", Rakefile: "ruby", "nginx.conf": "nginx",
    ".vimrc": "vim", vimrc: "vim",
  };
  // FENCE_LANG maps a Markdown fence tag or alias to its Prism grammar.
  const FENCE_LANG = {
    go: "go", golang: "go", js: "javascript", javascript: "javascript",
    mjs: "javascript", jsx: "jsx", ts: "typescript", typescript: "typescript",
    tsx: "tsx", json: "json", yaml: "yaml", yml: "yaml", sh: "bash",
    bash: "bash", shell: "bash", zsh: "bash", console: "bash", md: "markdown",
    markdown: "markdown", html: "markup", htm: "markup", xml: "markup", css: "css", diff: "diff",
    py: "python", python: "python", rs: "rust", rust: "rust",
    c: "c", cpp: "cpp", "c++": "cpp", cxx: "cpp",
    java: "java", sql: "sql", rb: "ruby", ruby: "ruby",
    kt: "kotlin", kotlin: "kotlin", swift: "swift",
    toml: "toml", php: "php", cs: "csharp", csharp: "csharp", "c#": "csharp",
    ini: "ini", proto: "protobuf", protobuf: "protobuf", lua: "lua",
    docker: "docker", dockerfile: "docker",
    scss: "scss", less: "less", dart: "dart",
    objc: "objectivec", objectivec: "objectivec",
    groovy: "groovy", gradle: "groovy", scala: "scala",
    graphql: "graphql", gql: "graphql", hcl: "hcl", terraform: "hcl",
    powershell: "powershell", ps1: "powershell", elixir: "elixir",
    make: "makefile", makefile: "makefile", cmake: "cmake",
    haskell: "haskell", hs: "haskell", clojure: "clojure", clj: "clojure",
    erlang: "erlang", erl: "erlang", perl: "perl", pl: "perl",
    r: "r", julia: "julia", jl: "julia", ocaml: "ocaml", fsharp: "fsharp",
    zig: "zig", nim: "nim", crystal: "crystal",
    solidity: "solidity", sol: "solidity", nginx: "nginx",
    latex: "latex", tex: "latex", matlab: "matlab",
    batch: "batch", bat: "batch", cmd: "batch", vim: "vim",
    vue: "markup", svelte: "markup",
  };

  // BASE_GRAMMARS ship inside prism.js and are never fetched as a grammar file.
  const BASE_GRAMMARS = {
    markup: 1, css: 1, clike: 1, javascript: 1, typescript: 1, json: 1,
    yaml: 1, bash: 1, go: 1, markdown: 1, diff: 1,
  };
  // GRAMMAR_DEPS lists the lazy grammars each lazy grammar extends; dependencies load first.
  const GRAMMAR_DEPS = {
    cpp: ["c"], tsx: ["jsx"], objectivec: ["c"], scala: ["java"], crystal: ["ruby"],
  };

  let grammarBase = "";
  // setGrammarBase records the bucket base URL grammar files load from; "" disables the loader.
  function setGrammarBase(base) { grammarBase = base || ""; }

  // grammarState caches one load Promise per grammar name.
  const grammarState = new Map();

  // fetchAsset GETs a shell asset relative to grammarBase; null on any failure.
  async function fetchAsset(rel) {
    if (!grammarBase || typeof fetch !== "function") return null;
    try {
      const res = await fetch(grammarBase + rel);
      if (!res || !res.ok) return null;
      return await res.text();
    } catch (e) { return null; }
  }

  // fetchGrammarText GETs grammars/prism-<name>.js relative to grammarBase.
  function fetchGrammarText(name) { return fetchAsset("grammars/prism-" + name + ".js"); }

  // prismState caches the one-shot prism.js load.
  let prismState = null;

  // ensurePrism fetches and evaluates prism.js on demand; false when unavailable.
  function ensurePrism() {
    if (getPrism()) return Promise.resolve(true);
    if (prismState) return prismState;
    prismState = (async () => {
      const src = await fetchAsset("prism.js");
      if (src === null) return false;
      try {
        if (typeof window !== "undefined") {
          window.Prism = window.Prism || {};
          // Set before evaluation: prism-core reads Prism.manual as it initializes.
          window.Prism.manual = true;
        }
        // Evaluated rather than appended as a script tag: the CSP allows connect-src https: but not script-src https:.
        // eslint-disable-next-line no-new-func
        new Function(src)();
      } catch (e) { return false; }
      return !!getPrism();
    })();
    return prismState;
  }

  // prismPending holds upgrades waiting on prism.js itself, never on a lazy grammar; the reveal awaits them.
  const prismPending = new Set();

  // PRISM_DEADLINE_MS bounds every wait on the tokenizer; a stalled fetch degrades to plain text.
  const PRISM_DEADLINE_MS = 2000;

  // withPrismDeadline resolves when p settles or the deadline passes, and never rejects.
  function withPrismDeadline(p) {
    const done = Promise.resolve(p).then(() => undefined, () => undefined);
    if (typeof setTimeout !== "function") return done;
    return new Promise((resolve) => {
      const timer = setTimeout(resolve, PRISM_DEADLINE_MS);
      done.then(() => { clearTimeout(timer); resolve(); });
    });
  }

  // highlightsSettled resolves once every pending prism upgrade re-rendered or the deadline passed.
  function highlightsSettled() {
    if (!prismPending.size) return Promise.resolve();
    return withPrismDeadline(Promise.all(Array.from(prismPending)));
  }

  // prismUpgrade resolves true when highlighting for lang improved on the synchronous render.
  async function prismUpgrade(lang) {
    if (!lang) return false;
    const had = !!getPrism();
    if (!had && !(await ensurePrism())) return false;
    const P = getPrism();
    if (!P) return false;
    const loaded = () => !!(P.languages && P.languages[lang]);
    // Prism just arrived with a base grammar, so the plain render is stale.
    if (loaded()) return !had;
    // A base grammar that is somehow not registered has no file to fetch.
    if (BASE_GRAMMARS[lang]) return false;
    return await ensureGrammar(lang);
  }

  // trackUpgrade runs an upgrade and re-render, registering only the prism.js phase on prismPending.
  function trackUpgrade(lang, rerender) {
    const tracked = !getPrism();
    const done = prismUpgrade(lang).then((ok) => { if (ok) rerender(); }).catch(() => {});
    if (!tracked) return;
    const phase = (!lang || BASE_GRAMMARS[lang]) ? done : ensurePrism().then(() => undefined, () => undefined);
    prismPending.add(phase);
    phase.then(() => prismPending.delete(phase));
  }

  // evalGrammar runs a fetched grammar component with Prism in scope; true when the language registered.
  function evalGrammar(name, src) {
    const P = getPrism();
    if (!P || !src) return false;
    try {
      // eslint-disable-next-line no-new-func
      new Function("Prism", src)(P);
    } catch (e) { return false; }
    return !!(P.languages && P.languages[name]);
  }

  // ensureGrammar loads a grammar and its dependencies into Prism.languages, resolving whether it is available.
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

  // lazyHighlight renders now, then re-renders in place once the missing tokenizer or grammar loads.
  function lazyHighlight(parent, lang, render) {
    render();
    if (!lang || !parent) return;
    trackUpgrade(lang, () => { parent.replaceChildren(); render(); });
  }

  // langForPath returns the Prism grammar for a path; an exact basename wins over the extension.
  function langForPath(path) {
    const p = path || "";
    const base = p.slice(p.lastIndexOf("/") + 1);
    if (BASENAME_LANG[base]) return BASENAME_LANG[base];
    const dot = p.lastIndexOf(".");
    if (dot < 0) return null;
    return EXT_LANG[p.slice(dot + 1).toLowerCase()] || null;
  }

  // langForFence maps a Markdown fence's info string to a Prism grammar name.
  function langForFence(tag) {
    return FENCE_LANG[(tag || "").toLowerCase().split(/\s+/)[0]] || null;
  }

  // getPrism returns the loaded Prism global, or null.
  function getPrism() {
    if (typeof window !== "undefined" && window.Prism && window.Prism.tokenize) return window.Prism;
    if (typeof Prism !== "undefined" && Prism.tokenize) return Prism;
    return null;
  }

  // tokenLeaves flattens a Prism token stream into { text, cls } leaves.
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

  // highlightNow appends highlighted DOM for code using only loaded grammars, else a text node.
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

  // highlightTo appends highlighted DOM for code, upgrading in place when the grammar loads.
  function highlightTo(parent, code, lang) {
    lazyHighlight(parent, lang, () => highlightNow(parent, code, lang));
    return parent;
  }

  // linesFor splits a tokenization into one { text, cls } segment array per source line.
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

  // highlightLines returns per-line segments for a blob using only loaded grammars.
  function highlightLines(code, lang) {
    return linesFor(code, lang);
  }

  // appendSegments fills a container with { text, cls } segments as spans and text nodes.
  function appendSegments(container, segs) {
    for (const s of segs) {
      if (s.cls) container.append(el("span", { class: s.cls }, [s.text]));
      else container.append(document.createTextNode(s.text));
    }
    return container;
  }

  // metaRow renders an item's author, time and hash link with its edit chips.
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

  // stateChip renders a state pill; each state needs a solid background class for its white text.
  function stateChip(state) {
    const map = {
      open: "open", closed: "closed", merged: "merged",
      canceled: "canceled", cancelled: "canceled", completed: "completed",
      active: "active", planned: "planned",
    };
    const cls = map[state] || "unknown";
    return el("span", { class: "chip state " + cls }, [state || "?"]);
  }

  // originChip returns an "↗ platform" badge for imported content, else null.
  function originChip(header) {
    const h = header || {};
    if (!h["origin-platform"] && !h["origin-url"] && !h["origin-author-name"]) return null;
    const label = h["origin-platform"] || "imported";
    return el("span", { class: "chip chip-origin", title: h["origin-url"] || label }, ["↗ " + label]);
  }

  // BOT_LOGINS and BOT_DOMAINS match an automation in either half of the origin author email.
  const BOT_LOGINS = /^(app\/.+|.+\[bot\]|dependabot|dependabot-preview|vercel|netlify|claassistant|cla-bot|github-actions|codecov|codecov-commenter|renovate|renovate-bot|sonarcloud|sonarqubecloud|greptileai|coderabbitai|copilot|copilot-pull-request-reviewer|semantic-release-bot|allcontributors|imgbot|snyk-bot|mergify)$/i;
  const BOT_DOMAINS = /(^|\.)(coderabbit\.ai|dependabot\.com|renovateapp\.com|greptile\.com|codecov\.io|mergify\.com)$/i;

  // botChip marks an item whose origin author is an automation, else null; the login drops GitHub's numeric id prefix.
  function botChip(header) {
    const h = header || {};
    const email = (h["origin-author-email"] || "").trim().toLowerCase();
    const at = email.indexOf("@");
    const login = (at > 0 ? email.slice(0, at) : email).replace(/^\d+\+/, "");
    const domain = at > 0 ? email.slice(at + 1) : "";
    const name = (h["origin-author-name"] || "").trim().toLowerCase();
    const isBot = (login && BOT_LOGINS.test(login)) || (domain && BOT_DOMAINS.test(domain)) || name.endsWith("[bot]");
    return isBot ? el("span", { class: "chip chip-bot", title: h["origin-author-email"] || "automated author" }, ["⚙ bot"]) : null;
  }

  // headerChips builds the chips a card shows from its header and optional interaction counts.
  function headerChips(header, counts) {
    const h = header || {};
    const chips = [];
    const rc = retractedChip(h);
    if (rc) chips.push(rc);
    const bc = botChip(h);
    if (bc) chips.push(bc);
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

  // assigneeLabel shortens an assignee email to its local part.
  function assigneeLabel(a) {
    const at = a.indexOf("@");
    return at > 0 ? a.slice(0, at) : a;
  }

  // appendChipRow appends a chip row to a card when there are chips.
  function appendChipRow(card, chips) {
    if (chips && chips.length) card.append(el("div", { class: "card-chips" }, chips));
  }

  // retractedChip returns a "retracted" chip when the header marks the item retracted, else null.
  function retractedChip(header) {
    return (header && header.retracted === "true") ? el("span", { class: "chip chip-retracted" }, ["retracted"]) : null;
  }

  // typeGlyphEl renders the type glyph; a state-bearing glyph is tinted and names its state in the title.
  function typeGlyphEl(item, ext) {
    const g = typeGlyph(item, ext);
    if (!g) return null;
    const h = item.header || {};
    const t = h.type || ext;
    const stateful = t === "issue" || t === "pull-request";
    const mod = stateful ? glyphStateClass(h.state) : t;
    return el("span", { class: "type-glyph tg-" + mod, title: stateful ? t + " · " + (h.state || "open") : t }, [g]);
  }
  // glyphStateClass maps an item state to its glyph tint class; unknown states are open.
  function glyphStateClass(state) {
    if (state === "merged") return "merged";
    if (state === "closed" || state === "canceled" || state === "cancelled" || state === "completed") return "closed";
    return "open";
  }
  // prependGlyph puts the type glyph before a card head, or before a headless card's meta row with .meta-lead.
  function prependGlyph(head, item, ext) {
    const g = typeGlyphEl(item, ext);
    if (!g) return;
    head.prepend(g);
    if (head.classList && head.classList.contains("meta")) head.classList.add("meta-lead");
  }

  // subjectBody splits content into its first line and the rest after dropping link reference definitions.
  function subjectBody(content) {
    const text = stripLinkRefDefs(content || "");
    const nl = text.indexOf("\n");
    return nl < 0 ? [text, ""] : [text.slice(0, nl), text.slice(nl + 1).trim()];
  }

  // cardNav makes a whole card navigate to its detail route, sparing inner links and active selections.
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

  // renderList maps items to cards, or renders an empty notice.
  function renderList(items, mapItem, emptyText) {
    if (!items.length) return [el("div", { class: "empty" }, [emptyText])];
    return items.map(mapItem);
  }

  // pagedListView renders a walk-backed list with a "Load more" control while the walk is truncated.
  function pagedListView(initial, drawBody, loadMore) {
    const wrap = el("div", {}, []);
    const body = el("div", {}, []);
    wrap.append(body);
    let moreWrap = null;
    // draw fills the body and refreshes the Load more control.
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

  // autoScrollListView renders a walk-backed list advancing on a sentinel observer; wrap.__loadNext advances one window.
  function autoScrollListView(initial, drawBody, loadMore) {
    const wrap = el("div", {}, []);
    const body = el("div", {}, []);
    const sentinel = el("div", { class: "scroll-sentinel", "aria-hidden": "true" }, []);
    wrap.append(body, sentinel);
    let truncated = !!initial.truncated;
    let loading = false;
    let observer = null;
    drawBody(initial.items, body);
    // advance loads the next window once, re-observing the sentinel after.
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

  // clampNode wraps a node in a CSS clamp with a Show more toggle, polling for layout before measuring.
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

  // clampedBody renders a card body as markdown under the clamp, matching the detail page render.
  function clampedBody(text) {
    return clampNode(el("div", { class: "body body-md" }, [renderCommitBody(text)]));
  }

  // replyQuoteBlock renders the excerpt a reply answers from its own commit, else null.
  function replyQuoteBlock(item, dimmed) {
    const q = parentQuote(item);
    if (!q) return null;
    const box = el("div", { class: "reply-quote" + (dimmed ? " dimmed" : "") }, []);
    const who = el("div", { class: "reply-quote-head meta meta-lead" }, [authorEl(q.author || "unknown", q.email)]);
    if (q.time) who.append(" · ", timeEl(Math.floor(Date.parse(q.time) / 1000) || 0));
    const h = refHash(q.ref);
    const sameRepo = h && !refRepoUrl(q.ref);
    if (sameRepo) who.append(" · ", el("a", { class: "hash", href: commitRef(h, refBranch(q.ref)) }, [h.slice(0, 12)]));
    box.append(who);
    box.append(clampNode(el("div", { class: "body reply-quote-body" }, [q.quoted])));
    // Navigates to the parent, so propagation stops before the outer cardNav.
    if (sameRepo) {
      box.classList.add("clickable");
      box.setAttribute("title", "Open what this replies to");
      box.addEventListener("click", (ev) => {
        if (ev.target && ev.target.closest && ev.target.closest("a, button")) return;
        const sel = typeof window !== "undefined" && window.getSelection ? window.getSelection() : null;
        if (sel && !sel.isCollapsed) return;
        ev.stopPropagation();
        location.hash = commitRef(h, refBranch(q.ref));
      });
    }
    return box;
  }

  // socialCard renders a post, comment, quote or repost card.
  function socialCard(item, counts) {
    const [subject, body] = subjectBody(item.content);
    const type = (item.header && item.header.type) || "post";
    const card = el("div", { class: "card" }, []);
    const meta = metaRow(item, "gitmsg/social");
    prependGlyph(meta, item, "social");
    card.append(el("div", {}, [meta]));
    const text = subject + (body ? "\n" + body : "");
    if (text) card.append(clampedBody(text));
    const quote = type === "comment" || type === "quote" || type === "repost"
      ? replyQuoteBlock(item, type !== "repost") : null;
    if (quote) card.append(quote);
    appendChipRow(card, headerChips(item.header, counts));
    return cardNav(card, item.commit.hash, "gitmsg/social");
  }

  // issueCard renders an issue card; subCount adds an "n sub" chip.
  function issueCard(item, subCount, counts) {
    const subject = itemSubject(item);
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"]),
    ]);
    prependGlyph(head, item, "pm");
    if (subCount) head.append(el("span", { class: "chip pm-sub-chip" }, [subCount + " sub"]));
    card.append(head);
    card.append(metaRow(item, "gitmsg/pm"));
    appendChipRow(card, headerChips(item.header, counts));
    return cardNav(card, item.commit.hash, "gitmsg/pm");
  }

  // prCard renders a pull request card with its head to base flow.
  function prCard(item, counts) {
    const subject = itemSubject(item);
    const h = item.header || {};
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
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

  // releaseCard renders a release card with tag, version, prerelease and asset chips.
  function releaseCard(item) {
    const [subject, body] = subjectBody(item.content);
    const h = item.header || {};
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, []);
    prependGlyph(head, item, "release");
    head.append(el("a", { class: "subject", href: commitRef(item.commit.hash, "gitmsg/release") }, [h.tag || subject || h.version || "(release)"]));
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

  // memoCard renders a memo card.
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

  // timelineCard dispatches a merged-timeline item to the card for its extension.
  function timelineCard(item, counts) {
    if (item._ext === "code") return commitCard(item.commit, item._branch || "", { chip: true });
    if (item._ext === "pm") return issueCard(item, 0, counts);
    if (item._ext === "review") return prCard(item, counts);
    if (item._ext === "release") return releaseCard(item);
    return socialCard(item, counts);
  }

  // versionLabel names a version by its position: original, v<N>, current.
  function versionLabel(i, total) {
    if (i === total - 1) return "current";
    if (i === 0) return "original";
    return "v" + (i + 1);
  }

  // versionMetaRow renders a version's meta row with that version's own state pill.
  function versionMetaRow(v, branch) {
    const when = v.effectiveTime || (v.commit && v.commit.authorTime);
    const row = el("span", { class: "meta" }, [authorEl(v.author || "unknown", effectiveAuthorEmail(v.commit, v.header)), " · ", timeEl(when), " · "]);
    row.append(el("a", { class: "hash", href: commitRef(v.commit.hash, branch) }, [v.commit.short]));
    if (v.header && v.header.state) row.append(stateChip(v.header.state));
    if (v.edited) row.append(el("span", { class: "chip" }, ["edited"]));
    if (v.editorName) row.append(el("span", { class: "chip" }, ["edited by " + v.editorName]));
    return row;
  }

  // VERSION_DELTA_SKIP are header keys a version delta never reports.
  const VERSION_DELTA_SKIP = { v: 1, edits: 1 };

  // headerDelta lists the header fields that differ between two versions as {key, from, to}.
  function headerDelta(prev, cur) {
    const a = (prev && prev.header) || {}, b = (cur && cur.header) || {};
    const out = [];
    for (const key of Object.keys(Object.assign({}, a, b)).sort()) {
      if (VERSION_DELTA_SKIP[key]) continue;
      const from = a[key] === undefined ? "" : String(a[key]);
      const to = b[key] === undefined ? "" : String(b[key]);
      if (from !== to) out.push({ key, from, to });
    }
    return out;
  }

  // headerDeltaPanel renders header deltas as "key: from → to" rows, with state as pills.
  function headerDeltaPanel(deltas) {
    const side = (key, val) => val === "" ? el("span", { class: "version-delta-none" }, ["—"])
      : key === "state" ? stateChip(val)
        : el("span", { class: "version-delta-val mono" }, [val]);
    const panel = el("div", { class: "version-header-delta" }, []);
    for (const d of deltas) {
      panel.append(el("div", { class: "version-delta-row" }, [
        el("span", { class: "version-delta-key mono" }, [d.key]),
        side(d.key, d.from), el("span", { class: "version-delta-arrow" }, ["→"]), side(d.key, d.to),
      ]));
    }
    return panel;
  }

  // versionDiffPanel renders the header deltas, then a unified body diff, between two versions.
  function versionDiffPanel(prev, cur) {
    const deltas = headerDelta(prev, cur);
    const panel = el("div", { class: "version-diff" }, []);
    if (deltas.length) panel.append(headerDeltaPanel(deltas));
    const ops = diffLines(prev.content || "", cur.content || "");
    if (ops === null) {
      panel.append(el("div", { class: "notice" }, ["Versions too large to diff."]));
      return panel;
    }
    const hunks = buildHunks(ops, 3);
    if (hunks.length) panel.append(renderHunksUnified(hunks, null));
    else panel.append(el("div", { class: "empty" }, [deltas.length ? "The message itself is unchanged." : "No changes."]));
    return panel;
  }

  // versionRowStateChip returns a state pill only on the original and on versions that changed it.
  function versionRowStateChip(versions, i) {
    const state = (versions[i].header || {}).state;
    if (!state) return null;
    if (i > 0 && (versions[i - 1].header || {}).state === state) return null;
    return stateChip(state);
  }

  // versionHistorySection renders the newest-first version picker with per-row diff toggles.
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
      const sc = versionRowStateChip(versions, i);
      if (sc) meta.append(sc);
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

  // trailerValue renders one trailer value; a same-repo original or reply-to ref links to its commit.
  function trailerValue(key, val) {
    if ((key === "original" || key === "reply-to") && !refRepoUrl(val)) {
      const h = refHash(val);
      if (h) return el("a", { href: commitRef(h, refBranch(val)) }, [val]);
    }
    return val;
  }

  // shareURL returns the item's page URL when the site config enables pages, else the in-app hash URL.
  function shareURL(ctx, short, branch) {
    const cfg = ctx && ctx.siteCustomization;
    if (cfg && cfg.pages === "true" && typeof cfg.url === "string" && /^https?:\/\//.test(cfg.url)) {
      const base = cfg.url.endsWith("/") ? cfg.url : cfg.url + "/";
      return base + "i/" + short + ".html";
    }
    const base = (ctx && ctx.base) || "";
    return base + commitRef(short, branch);
  }

  // shareControl renders a Copy link button that resolves shareURL at click time.
  function shareControl(ctx, short, branch) {
    const btn = el("button", { class: "share-link", type: "button", title: "Copy a link to this item" }, ["Copy link"]);
    btn.addEventListener("click", async () => {
      let url = shareURL(ctx, short, branch);
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

  // detailView renders an item detail page, with a version picker when the item has edits.
  function detailView(item, kind, skipKeys, ctx) {
    const skip = skipKeys || [];
    const versions = (item.versions && item.versions.length) ? item.versions
      : [{ commit: item.commit, header: item.header, content: item.content, rawMessage: item.rawMessage, author: item.author, editorName: item.editorName, edited: item.edited, effectiveTime: item.effectiveTime }];
    const sel = { idx: versions.length - 1 };
    const bodyOnly = isBodyOnly(item, (COMMIT_VIEW[kind.branch] || {}).ext);
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("div", { class: "detail-topbar" }, [
      el("a", { class: "back", href: detailBackHref(ctx, "#/" + kind.tab) }, ["← back"]),
      shareControl(ctx, item.commit.short, kind.branch),
    ]));
    const subjectEl = bodyOnly ? null : el("div", { class: "subject" }, []);
    if (subjectEl) wrap.append(subjectEl);
    const metaSlot = el("div", { class: "detail-meta" }, []);
    wrap.append(metaSlot);
    const bodyPane = el("div", {}, []);
    wrap.append(bodyPane);
    const dl = el("dl", {}, []);
    wrap.append(dl);
    // paint repaints the subject, meta, body and header fields for the selected version.
    function paint() {
      const v = versions[sel.idx];
      const [subject, body] = subjectBody(v.content);
      if (subjectEl) subjectEl.textContent = subject || "(untitled)";
      const cb = commitBody(bodyOnly ? v.content : body, v.rawMessage);
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

  // resolveCommitRouteSha expands a short route sha through the index, then a bounded walk; getObject needs full shas.
  async function resolveCommitRouteSha(ctx, hash, branch) {
    if (/^[0-9a-f]{40}$/.test(hash)) return hash;
    const indexed = await resolveShortShaFromIndex(ctx, hash);
    if (indexed) return indexed;
    const name = branch || headBranchName(await headFor(ctx));
    if (!name) return null;
    const live = await refTip(ctx, "refs/heads/" + name);
    if (!live) return null;
    if (live.startsWith(hash)) return live;
    const commits = await walkHistory(ctx, live, DETAIL_WALK_CAP);
    return (commits.find((c) => c.hash.startsWith(hash)) || {}).hash || null;
  }

  // commitDetail renders a raw commit's detail page with its changes.
  async function commitDetail(ctx, hash, branch) {
    const sha = await resolveCommitRouteSha(ctx, hash, branch);
    const obj = sha ? await getObject(ctx, sha) : null;
    if (!obj || obj.type !== "commit") return [el("div", { class: "err" }, ["Commit not found: " + hash])];
    const c = parseCommit(sha, obj.body);
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

  // IMG_MIME maps displayable image extensions to MIME types; SVG rides through img so scripts stay inert.
  const IMG_MIME = {
    png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif",
    webp: "image/webp", svg: "image/svg+xml",
  };
  // imageExt returns a path's displayable image extension, or null.
  function imageExt(path) {
    const d = (path || "").lastIndexOf(".");
    const ext = d >= 0 ? path.slice(d + 1).toLowerCase() : "";
    return IMG_MIME[ext] ? ext : null;
  }
  // imageMime returns a path's image MIME type, or null.
  function imageMime(path) { const e = imageExt(path); return e ? IMG_MIME[e] : null; }

  // VIDEO_MIME maps inline-playable video extensions to their MIME type.
  const VIDEO_MIME = { mp4: "video/mp4", webm: "video/webm" };
  // videoMime returns a path's video MIME type, or null.
  function videoMime(path) {
    const d = (path || "").lastIndexOf(".");
    const ext = d >= 0 ? path.slice(d + 1).toLowerCase() : "";
    return VIDEO_MIME[ext] || null;
  }

  // liveObjectUrls holds the ephemeral object URLs revoked on the next route.
  let liveObjectUrls = [];
  // trackObjectUrl registers an object URL for revocation on route change.
  function trackObjectUrl(u) { liveObjectUrls.push(u); }
  // revokeObjectUrls revokes every tracked object URL.
  function revokeObjectUrls() {
    for (const u of liveObjectUrls) { try { URL.revokeObjectURL(u); } catch (e) { /* noop */ } }
    liveObjectUrls = [];
  }

  // imageUrlCache maps branch + "\0" + path to { tip, url }, url being the in-flight Promise.
  const imageUrlCache = new Map();

  // joinPath resolves a relative path against the document's directory, honoring ./, ../ and a leading /.
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
  const IMG_BLOB_CAP = 8388608;

  // blobObjectUrl returns a cached object URL for an in-bucket image, or null; a passed tip skips refTip.
  async function blobObjectUrl(ctx, path, branch, tip) {
    if (!tip) tip = await refTip(ctx, "refs/heads/" + branch);
    if (!tip) return null;
    const key = branch + "\0" + path;
    const cached = imageUrlCache.get(key);
    if (cached) {
      if (cached.tip === tip) return cached.url;
      cached.url.then((u) => { if (u) { try { URL.revokeObjectURL(u); } catch (e) { /* noop */ } } }, () => { /* noop */ });
    }
    const url = (async () => {
      const node = await resolvePath(ctx, tip, path);
      if (!node || node.type !== "blob") return null;
      const mime = imageMime(path);
      if (!mime) return null;
      const obj = await getContentObject(ctx, node.sha);
      if (!obj || obj.body.length > IMG_BLOB_CAP) return null;
      return URL.createObjectURL(new Blob([obj.body], { type: mime }));
    })();
    imageUrlCache.set(key, { tip, url });
    // A rejection is never kept, so a transient fetch error does not pin the image broken.
    url.catch(() => { const e = imageUrlCache.get(key); if (e && e.url === url) imageUrlCache.delete(key); });
    return url;
  }

  // bytesObjectUrl wraps already-inflated blob bytes in a tracked object URL, or null.
  function bytesObjectUrl(bytes, path) {
    const mime = imageMime(path);
    if (!mime || bytes.length > IMG_BLOB_CAP) return null;
    const u = URL.createObjectURL(new Blob([bytes], { type: mime }));
    trackObjectUrl(u);
    return u;
  }

  // resolveImages turns the sanitizer's relative img data-gs-src markers into in-bucket object URLs.
  async function resolveImages(container, ctx, tip) {
    if (!ctx || !container || !container.querySelectorAll) return;
    for (const img of Array.from(container.querySelectorAll("img[data-gs-src]"))) {
      const src = img.getAttribute("data-gs-src");
      const branch = img.getAttribute("data-gs-branch") || "";
      const dir = img.getAttribute("data-gs-dir") || "";
      img.removeAttribute("data-gs-src");
      try { const u = await blobObjectUrl(ctx, joinPath(dir, src), branch, tip); if (u) img.setAttribute("src", u); }
      catch (e) { /* leave alt text */ }
    }
  }

  // ---- Sanitizer: inert-parse then whitelist-rebuild a clean DOM tree ----

  // SANITIZE_TAGS are rebuilt clean, SANITIZE_DROP vanish with their subtrees, other tags unwrap to their children.
  const SANITIZE_TAGS = new Set(["div", "span", "p", "br", "hr", "a", "img", "b", "strong", "i", "em", "code", "pre", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "colgroup", "col", "details", "summary", "center", "sup", "sub", "kbd", "del", "s", "strike", "blockquote", "mark"]);
  const SANITIZE_DROP = new Set(["script", "style", "iframe", "object", "embed", "link", "meta", "noscript", "template", "svg", "math", "form", "input", "button", "textarea", "select", "title", "head", "base", "frame", "frameset", "applet"]);
  const SANITIZE_ATTRS = new Set(["align", "alt", "title", "width", "height", "src", "href", "open"]);

  // hrefOk accepts absolute web, mailto, in-page and root-relative hrefs.
  function hrefOk(v) { return /^(https?:|mailto:|#|\/)/i.test(v || ""); }

  // relativeHref resolves a bare-relative href to the in-site file route, with a fragment as a :slug suffix.
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

  // applyImgSrc keeps an absolute https src, defers a relative path to resolveImages, and drops every other scheme.
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

  // nodeChildren returns a node's child nodes as an array.
  function nodeChildren(n) { return Array.from((n && n.childNodes) || []); }
  // nodeAttrs returns a node's attributes as an array.
  function nodeAttrs(n) { return Array.from((n && n.attributes) || []); }

  // sanitizeChildren appends a whitelisted rebuild of a node's children to out.
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

  // sanitizeInert rebuilds a clean node array from an already-parsed inert node.
  function sanitizeInert(inertBody, mdctx) {
    const container = document.createElement("div");
    sanitizeChildren(inertBody, container, mdctx);
    return Array.from(container.childNodes || []);
  }

  // sanitizeHtml parses untrusted HTML into an inert document and rebuilds it against the whitelist.
  function sanitizeHtml(html, mdctx) {
    let body;
    try { body = new DOMParser().parseFromString(String(html), "text/html").body; }
    catch (e) { return [document.createTextNode(String(html))]; }
    return sanitizeInert(body, mdctx);
  }

  // makeImage builds an img for a markdown image, gated like a sanitized one.
  function makeImage(src, alt, mdctx) {
    const img = el("img", {}, []);
    applyImgSrc(img, src, alt || "", mdctx);
    return img;
  }

  // ---- Fullscreen overlay (pure presentation, no fetches) ----

  // openFullscreen shows a node in an overlay; opts.live moves the real node and restores it on close.
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
    // shut closes the overlay and restores a live node.
    function shut() { overlay.remove(); document.body.style.overflow = ""; document.removeEventListener("keydown", onKey); if (restore) restore(); }
    // onKey closes on Escape.
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

  // renderInline turns inline spans into DOM nodes; rawhtml spans pass through the sanitizer.
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
        // A single newline is a hard br, as in a GitHub comment.
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
      // The md- prefix keeps heading anchor ids clear of app ids.
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

  // renderBlocksInto renders blocks into a parent, nesting later blocks inside sanitized htmlopen wrappers.
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
        // Pop to the nearest matching wrapper; an unmatched close is ignored.
        for (let d = stack.length - 1; d > 0; d--) {
          if (stack[d].tag === block.tag) { stack.length = d; break; }
        }
      } else if (block.type === "html") { for (const n of sanitizeHtml(block.raw, mdctx)) cur().append(n); }
      else renderMdBlock(block, cur(), mdctx);
    }
  }

  // renderMarkdown builds a sanitized DOM subtree from markdown; mdctx carries { ctx, branch, dir, tip }.
  function renderMarkdown(text, mdctx) {
    mdctx = mdctx || {};
    if (!mdctx.slugs) mdctx.slugs = new Set();
    const root = el("div", { class: "markdown" }, []);
    renderBlocksInto(root, parseMarkdown(text), mdctx);
    if (mdctx.ctx) resolveImages(root, mdctx.ctx, mdctx.tip);
    wireInPageAnchors(root);
    return root;
  }

  // wireInPageAnchors scrolls plain #fragment links to their md- heading and pushes a shareable URL.
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

  // renderCommitBody renders a commit body as markdown with hard breaks and no relative image resolution.
  function renderCommitBody(body) {
    return renderMarkdown(body || "", { hardBreaks: true });
  }

  // rawToggle builds the Raw toggle button switching between a rendered and a verbatim pane.
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

  // commitBody builds a detail body with a Raw toggle, returning { modes, pane }; Raw is the full verbatim message.
  function commitBody(body, rawMessage) {
    const pane = el("div", {}, []);
    const btn = rawToggle(
      () => pane.replaceChildren(renderCommitBody(body)),
      () => pane.replaceChildren(el("div", { class: "body raw-body" }, [rawMessage || ""])));
    const modes = el("div", { class: "view-modes body-modes" }, [btn]);
    return { modes, pane };
  }

  // breadcrumb renders a path as the root plus one link per segment.
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

  // treeIcon builds a tree-row icon for an entry, falling back to a text glyph.
  function treeIcon(entry, open) {
    let key, glyph;
    if (!entry) { key = "folder"; glyph = "📁"; }
    else if (entry.type === "tree") { key = open ? "folder-open" : "folder"; glyph = open ? "📂" : "📁"; }
    else if (entry.type === "commit") { key = "git"; glyph = "📁"; }
    else { key = entry.mode === "120000" ? "symlink" : iconName(entry.name); glyph = "📄"; }
    return iconEl(key, "tree-icon") || el("span", { class: "tree-icon" }, [glyph]);
  }

  // TREE_ENTRY_CAP bounds the entries one directory level renders.
  const TREE_ENTRY_CAP = 200;

  // TREE_SEARCH_CAP bounds the one-time full-tree walk behind the in-place search.
  const TREE_SEARCH_CAP = 3000;

  // highlightName wraps each case-insensitive match of q in a mark.
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

  // treeChevron builds a directory expand caret, or a text caret without DOMParser.
  function treeChevron(open) {
    const c = chevronEl("down");
    if (c) { c.className = "gs-icon chevron tree-chevron" + (open ? " open" : ""); return c; }
    return el("span", { class: "tree-chevron" + (open ? " open" : "") }, [open ? "▾" : "▸"]);
  }

  // mountTree renders a lazily-expanding directory tree into listNode; returns { expanded, rerender, setFilter, buildIndex }.
  function mountTree(ctx, listNode, rootEntries, rootPath, branch, opts) {
    opts = opts || {};
    const expanded = opts.expanded || new Set();
    const activePath = opts.activePath || "";

    // indent builds a depth-sized spacer at a row's left.
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

    // dirNode builds a directory row: the chevron and row toggle, the name navigates, keys Enter, Space and arrows.
    function dirNode(entry, childPath, depth) {
      const childrenEl = el("div", { class: "tree-children" }, []);
      const dirCls = "tree-row tree-dir" + (childPath === activePath ? " tree-active" : "");
      const row = el("div", { class: dirCls, role: "button", tabindex: "0", "aria-expanded": "false" }, []);
      const node = el("div", { class: "tree-node" }, [row, childrenEl]);
      let open = false;
      const navigate = () => { if (typeof location !== "undefined") location.hash = fileRef(childPath, branch); };
      const paint = () => {
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
      // openDir expands the directory, fetching its tree.
      async function openDir() {
        if (open) return;
        open = true; expanded.add(childPath); paint();
        const kids = (await getTree(ctx, entry.sha)) || [];
        await renderLevel(childrenEl, kids, childPath, depth + 1);
      }
      // closeDir collapses the directory.
      function closeDir() {
        if (!open) return;
        open = false; expanded.delete(childPath);
        childrenEl.replaceChildren();
        paint();
      }
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

    // renderLevel fills a container with one capped directory level, reopening directories in the expansion Set.
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

    // fullIndex caches the one-time recursive walk under the root.
    let fullIndex = null;

    // buildIndex walks the whole tree once, collecting { path, name, type } up to TREE_SEARCH_CAP.
    async function buildIndex() {
      if (fullIndex) return fullIndex;
      const all = [];
      const state = { truncated: false };
      // walk recurses one directory level into all.
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

    // renderFiltered draws only visible paths, with forceOpen directories expanded and matches marked.
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

    // setFilter shows matches with their ancestor chain expanded; an empty query restores the tree.
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

  // lastTreeSearch is the most recently mounted tree search input.
  let lastTreeSearch = null;

  // focusTreeSearch focuses the mounted tree search input, returning whether it did.
  function focusTreeSearch() {
    if (lastTreeSearch && lastTreeSearch.focus) { lastTreeSearch.focus(); return true; }
    return false;
  }

  // treeView renders a breadcrumb and search input above the interactive tree rooted at path.
  function treeView(ctx, entries, path, branch) {
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(breadcrumb(path, branch));
    const listNode = el("div", { class: "tree-list" }, []);
    if (!entries.length) { listNode.append(el("div", { class: "empty" }, ["Empty directory."])); wrap.append(listNode); return [wrap]; }
    const ctrl = mountTree(ctx, listNode, entries, path, branch, { expanded: ctx.treeExpanded });
    wrap.__tree = ctrl;
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

  // blobAnchor is the last clicked blob line, so a shift-click extends a range.
  let blobAnchor = null;

  // rawBlobPane builds the line-numbered blob body with lazy highlighting; returns { code, firstHl }.
  function rawBlobPane(textStr, path, branch, line, lineEnd) {
    const from = line || 0, to = lineEnd || line || 0;
    const lang = langForPath(path);
    blobAnchor = line || null;
    const code = el("div", { class: "blob" }, []);
    let firstHl = null;
    // build is re-run when a grammar loads; firstHl is the first highlighted row.
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
    trackUpgrade(lang, build);
    return { code, firstHl };
  }

  // blobView renders a file: images and videos inline before the binary sniff, else text with a Raw toggle for markdown.
  function blobView(bytes, path, branch, line, lineEnd, ctx, tip) {
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
    const vmime = videoMime(path);
    if (vmime) {
      if (bytes.length <= IMG_BLOB_CAP) {
        const u = URL.createObjectURL(new Blob([bytes], { type: vmime }));
        trackObjectUrl(u);
        wrap.append(el("div", { class: "blob-img" }, [el("video", { src: u, controls: "", style: "max-width:100%" }, [])]));
      } else {
        wrap.append(el("div", { class: "notice" }, ["Video too large to play inline (over " + humanSize(IMG_BLOB_CAP) + ")."]));
      }
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
        () => pane.replaceChildren(renderMarkdown(textStr, { ctx, branch, dir, tip })),
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

  // getDiffMode reads the unified|split view mode from localStorage; unified is the default.
  function getDiffMode() {
    try { return localStorage.getItem("diffview") === "split" ? "split" : "unified"; } catch { return "unified"; }
  }
  // setDiffMode persists the diff view mode.
  function setDiffMode(m) { try { localStorage.setItem("diffview", m); } catch { /* private mode */ } }

  // diffStatusLabel maps a file status to its A, D or M letter.
  function diffStatusLabel(s) { return s === "added" ? "A" : s === "deleted" ? "D" : "M"; }

  // hunkHeadText formats a hunk's @@ header.
  function hunkHeadText(h) {
    return "@@ -" + h.oldStart + "," + h.oldCount + " +" + h.newStart + "," + h.newCount + " @@";
  }

  // pairIntra maps paired del/add lines within a hunk to their intra-line { prefix, mid, suffix } split.
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

  // renderDiffText fills a diff line container, marking an intra-line middle with markCls.
  function renderDiffText(container, lineText, lang, intra, markCls) {
    if (!intra) return highlightTo(container, lineText, lang);
    highlightTo(container, intra.prefix, lang);
    container.append(highlightTo(el("mark", { class: markCls }, []), intra.mid, lang));
    highlightTo(container, intra.suffix, lang);
    return container;
  }

  // hunkSeparator builds the "N unchanged lines" divider that reveals the skipped rows on click.
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

  // unifiedRow builds one unified-diff line row.
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

  // appendLineFeedback appends the feedback cards anchored to a diff line, deduplicated across its keys.
  function appendLineFeedback(box, l, fbCtx) {
    if (!fbCtx || !fbCtx.byKey) return;
    const seen = new Set();
    for (const k of hunkLineKeys(l)) {
      const list = fbCtx.byKey.get(k);
      if (!list) continue;
      for (const fb of list) { if (seen.has(fb)) continue; seen.add(fb); box.append(feedbackRow(fb)); }
    }
  }

  // renderHunksUnified renders hunks as a unified diff, with inline feedback when fbCtx is passed.
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

  // splitRows pairs a hunk's lines into two-column rows, deletes against the following adds.
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

  // splitCells appends one split row's four grid cells; one grid per hunk body keeps both sides height-locked.
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

  // renderHunksSplit renders hunks as a four-column grid, old left and new right, with inline feedback.
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

  // suggestionBlock renders a suggestion body as a code panel with an "applies to L<n>" note.
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

  // feedbackCard renders one PR feedback: verdict icon, author, time and body.
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

  // feedbackRow wraps a feedback card as a full-width diff row.
  function feedbackRow(fb) { return el("div", { class: "diff-feedback" }, [feedbackCard(fb)]); }

  // offscreenBlock renders feedback whose anchored line is not in the rendered hunks.
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

  // diffSection renders a capped, collapsible changed-file list with a unified|split toggle; diffs load on first expand.
  function diffSection(ctx, entries, title, caveats, fileFeedback) {
    fileFeedback = fileFeedback || [];
    const shown = entries.slice(0, DIFF_FILE_CAP);
    const extra = entries.length - shown.length;
    // A truncated scan has no exact count, so the header reads "N+".
    const scanTruncated = !!entries.truncated;
    const countLabel = scanTruncated ? DIFF_FILE_CAP + "+" : String(entries.length);
    const wrap = el("div", { class: "diff-section" }, []);
    const head = el("div", { class: "diff-head" }, [el("span", { class: "subject" }, [title + " (" + countLabel + ")"])]);
    for (const c of caveats || []) head.append(el("span", { class: "chip caveat" }, [c]));
    let mode = getDiffMode();
    const modeBtn = el("button", { class: "diff-btn mode-toggle", type: "button" }, []);
    const expandBtn = el("button", { class: "diff-btn expand-toggle", type: "button" }, []);
    const fsBtn = el("button", { class: "diff-btn", type: "button", title: "Fullscreen changes", "aria-label": "Fullscreen changes" }, ["⤢"]);
    head.append(el("div", { class: "diff-controls" }, [expandBtn, modeBtn, fsBtn]));
    wrap.append(head);
    const files = [];
    // refreshModeBtn labels the toggle with the mode a click switches to.
    function refreshModeBtn() {
      const target = mode === "unified" ? "split" : "unified";
      modeBtn.textContent = "⇄ " + (target === "split" ? "Split" : "Unified");
      modeBtn.setAttribute("title", "Switch to " + target + " view");
      modeBtn.setAttribute("aria-label", "Switch to " + target + " view");
    }
    // refreshExpandBtn labels the toggle from the per-file state.
    function refreshExpandBtn() {
      if (!files.length) { expandBtn.style.display = "none"; return; }
      const anyCollapsed = files.some((f) => !f.expanded);
      expandBtn.textContent = anyCollapsed ? "⊞ Expand all" : "⊟ Collapse all";
    }
    // apply repaints every expanded file in the current mode.
    function apply() {
      refreshModeBtn();
      for (const f of files) if (f.expanded && f.model) f.renderBody();
    }
    modeBtn.addEventListener("click", () => {
      mode = mode === "unified" ? "split" : "unified";
      setDiffMode(mode);
      apply();
    });
    // expandAll expands the collapsed files with CONCURRENCY workers.
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

  // resolveTipCommit resolves a PR tip to a full sha here, preferring the recorded short tip over the live one.
  async function resolveTipCommit(ctx, field, short) {
    const { url, name } = parseBranchField(field);
    if (url) return { status: "foreign" };
    if (!name) return { status: "unknown" };
    const live = await refTip(ctx, "refs/heads/" + name);
    if (!live) return { status: "absent" };
    if (short && live.startsWith(short)) return { status: "ok", sha: live, exact: true };
    if (short) {
      // The index answers first; the bounded walk covers shas the index lacks.
      const indexed = await resolveShortShaFromIndex(ctx, short);
      if (indexed) return { status: "ok", sha: indexed, exact: true };
      const commits = await walkHistory(ctx, live, DETAIL_WALK_CAP);
      const hit = commits.find((c) => c.hash.startsWith(short));
      if (hit) return { status: "ok", sha: hit.hash, exact: true };
    }
    return { status: "ok", sha: live, exact: false };
  }

  // resolveMergedRefs resolves a merged PR's merge-base and merge-head shorts to full shas, or null.
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

  // prDiffSection builds a PR's Files changed section from the merge range, else the resolved tips, or null.
  async function prDiffSection(ctx, header, fileFeedback) {
    if (!header) return null;
    // The merge range comes first: it stays on the base branch after the head is deleted.
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
    const mb = await resolveMergeBase(ctx, headR.sha, baseR.sha, DETAIL_WALK_CAP);
    let leftTree = baseTree;
    if (mb) { leftTree = await commitTree(ctx, mb) || baseTree; }
    else caveats.push("raw two-tip diff");
    if (!headR.exact) caveats.push("head branch advanced");
    if (!baseR.exact && !mb) caveats.push("base branch advanced");
    const entries = await diffTrees(ctx, leftTree, headTree);
    return diffSection(ctx, entries, "Files changed", caveats, fileFeedback);
  }

  // reviewSummarySection renders the review counts, status chip, reviewer chips and verdict feedback, or null.
  function reviewSummarySection(summary, verdictFeedback) {
    const hasAny = summary.reviewers.length || summary.approved || summary.changesRequested || summary.pending;
    if (!hasAny) return null;
    const wrap = el("div", { class: "review-summary" }, []);
    wrap.append(el("div", { class: "review-summary-head mono" }, ["Reviews"]));
    const strip = el("div", { class: "review-summary-line" }, [
      el("span", { class: "meta" }, [summary.approved + " approved · " + summary.changesRequested + " changes requested · " + summary.pending + " pending"]),
    ]);
    if (summary.isApproved) strip.append(el("span", { class: "chip state open" }, ["ready to merge"]));
    else if (summary.isBlocked) strip.append(el("span", { class: "chip state closed" }, ["changes requested"]));
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

  // commitChangesSection builds a commit's Changes section against its first parent.
  async function commitChangesSection(ctx, commit) {
    let parentTree = null;
    if (commit.parents.length) parentTree = await commitTree(ctx, commit.parents[0]);
    const entries = await diffTrees(ctx, parentTree, commit.tree);
    const caveats = commit.parents.length > 1 ? ["vs first parent"] : [];
    const title = commit.parents.length === 0 ? "Changes (root commit)" : "Changes";
    return diffSection(ctx, entries, title, caveats);
  }

  // setView replaces the #view contents.
  function setView(nodes) {
    const view = document.getElementById("view");
    view.replaceChildren(...nodes);
  }

  // highlightNav marks the active nav tab.
  function highlightNav(tab) {
    for (const a of document.querySelectorAll("#nav a[data-nav]")) {
      a.classList.toggle("active", a.getAttribute("data-nav") === tab);
    }
  }

  // ---- PM rendering (browser only) ----

  const RELEASE_ASSET_KEYS = ["artifacts", "artifact-url", "checksums", "sbom", "signed-by"];
  const ISSUE_STATES = [{ key: "all", label: "All" }, { key: "open", label: "Open" }, { key: "closed", label: "Closed" }];
  const PR_STATES = [{ key: "all", label: "All" }, { key: "open", label: "Open" }, { key: "merged", label: "Merged" }, { key: "closed", label: "Closed" }];
  // filterState is the per-tab state filter selection, kept out of the route.
  const filterState = { issues: "all", prs: "all" };

  // assetRow renders one asset entry: a link when it has a gated href, else selectable mono text.
  function assetRow(name, href, tag) {
    const kids = [el("span", { class: "mono selectable" }, [name])];
    if (tag) kids.push(el("span", { class: "chip" }, [tag]));
    if (href && /^(https?:|\/)/i.test(href)) {
      const a = el("a", { class: "asset-row", href, rel: "noopener" }, kids);
      return a;
    }
    return el("div", { class: "asset-row" }, kids);
  }

  // releaseAssetsSection renders a release's artifacts, checksums, SBOM and signing key, or null.
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

  // embeddedBlock renders the cross-repo context a commit embeds for one reference.
  function embeddedBlock(e) {
    const box = el("div", { class: "embedded" }, []);
    box.append(el("div", { class: "embedded-from meta" }, ["from " + e.url]));
    if (e.quoted) box.append(el("div", { class: "embedded-quote" }, [e.quoted]));
    const who = el("div", { class: "meta" }, [authorEl(e.author || "unknown", e.email)]);
    if (e.time) who.append(" · " + e.time);
    box.append(who);
    return box;
  }

  // commentCard renders one thread comment; clamp gives the meta-first clamped layout, nav makes it clickable.
  function commentCard(item, branch, clamp, nav) {
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
    return nav ? cardNav(card, item.commit.hash, branch || "gitmsg/social") : card;
  }

  // commentRow places depth rail guides to a comment card's left.
  function commentRow(item, depth, branch, clamp, nav) {
    if (depth <= 0) return commentCard(item, branch, clamp, nav);
    const rail = el("div", { class: "thread-rail" }, []);
    for (let i = 0; i < depth; i++) rail.append(el("span", { class: "rail-guide" }, []));
    return el("div", { class: "comment-row" }, [rail, commentCard(item, branch, clamp, nav)]);
  }

  // threadSection renders a comment thread as a flat chronological list with rail guides, or null.
  function threadSection(thread) {
    const flat = flattenThread(thread);
    if (!flat.length) return null;
    const wrap = el("div", { class: "thread" }, []);
    wrap.append(el("div", { class: "thread-head mono" }, ["Comments (" + flat.length + ")"]));
    for (const row of flat) wrap.append(commentRow(row.comment, row.depth));
    return wrap;
  }

  // itemThreadSection loads the same-repo comments up to DETAIL_WALK_CAP and groups them under the item.
  async function itemThreadSection(ctx, item) {
    const social = await loadExtItemsUpTo(ctx, "social", DETAIL_WALK_CAP);
    const comments = social.filter((i) => i.header && i.header.original);
    const thread = groupThread(item.commit.short, comments);
    await hydrateItems(ctx, flattenThread(thread).map((r) => r.comment));
    return threadSection(thread);
  }

  // quotedFallbackBlock renders the item's quoted excerpt for an unresolvable same-repo parent, or null.
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

  // replyContextSection renders a reply's ancestor chain root-first above its permalink, or null.
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
      wrap.append(commentRow(chain[i].item, Math.min(i, THREAD_MAX_DEPTH), chain[i].branch, true, true));
    }
    return wrap;
  }

  // enrichDetail appends a producer's section after first paint, so no bounded walk gates it; failures are swallowed.
  function enrichDetail(root, producer, place) {
    Promise.resolve().then(producer).then((node) => {
      if (node) (place ? place(node) : root.append(node));
    }).catch(() => { /* enrichment is best-effort; the base detail already painted */ });
  }

  // itemDetail paints the base detail as soon as the item resolves, then enriches sections in the background.
  async function itemDetail(ctx, hash, branch) {
    const cv = COMMIT_VIEW[branch];
    const onProgress = (visited) => setView([el("div", { class: "loading" }, ["Searching history… (" + visited + " commits scanned)"])]);
    const { item, items } = await findItemDeep(ctx, cv.ext, hash, onProgress);
    if (!item) return [el("div", { class: "err" }, [cv.label + " not found."])];
    const skip = cv.ext === "release" ? RELEASE_ASSET_KEYS : [];
    const nodes = detailView(item, { tab: cv.tab, branch }, skip, ctx);
    const root = nodes[0];
    for (const e of embeddedRefs(item.commit, item.header)) root.append(embeddedBlock(e));
    if (cv.ext === "release") {
      const sec = releaseAssetsSection(item.header);
      if (sec) root.append(sec);
    }
    enrichDetail(root, () => replyContextSection(ctx, item, branch),
      (node) => root.insertBefore(node, root.children[1] || null));
    if (cv.ext === "pm") {
      enrichDetail(root, async () => {
        const wrap = el("div", {}, []);
        for (const extra of await pmDetailExtras(ctx, item, items)) wrap.append(extra);
        return wrap.childNodes.length ? wrap : null;
      });
    }
    if (branch === "gitmsg/review" && (item.header.type || "") === "pull-request") {
      enrichDetail(root, async () => {
        const fb = prFeedback(items, item.commit.short);
        await hydrateItems(ctx, fb.all);
        const summary = reviewSummary(fb.all, item.header.reviewers || "");
        return reviewSummarySection(summary, fb.nonFile);
      });
      enrichDetail(root, async () => {
        const file = prFeedback(items, item.commit.short).file;
        await hydrateItems(ctx, file);
        return prDiffSection(ctx, item.header, file);
      });
    }
    // The social walk can be large, so the thread enriches last.
    enrichDetail(root, () => itemThreadSection(ctx, item));
    return nodes;
  }

  // filteredListView renders a state chip bar with exact counts above a client-paginated list.
  function filteredListView(items, cardFn, tab, states, emptyText) {
    const counts = stateCounts(items);
    const PAGE = 100;
    const bar = el("div", { class: "filter-bar" }, []);
    const listBox = el("div", {}, []);
    const outer = el("div", {}, [bar, listBox]);
    const chips = [];
    let moreWrap = null;
    // render applies the selected state filter.
    function render() {
      const sel = filterState[tab] || "all";
      const shown = sel === "all" ? items : items.filter((it) => ((it.header && it.header.state) || "open") === sel);
      let page = 1;
      // paint draws the current page with a Load more for the rest.
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

  // pmGroupCard renders a milestone or sprint with its chips and member issues.
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

  // issuesBody renders the state-filtered issue list as a node array.
  function issuesBody(pmItems, counts) {
    const g = groupPM(pmItems);
    const hier = buildIssueHierarchy(g.issues);
    const card = (it) => issueCard(it, (hier.childrenOf.get(it.commit.short) || []).length, countsFor(counts, it.commit.short));
    return filteredListView(g.issues, card, "issues", ISSUE_STATES, "No issues in this repository.");
  }

  // versionKey extracts a milestone's leading dotted version as numbers, or null.
  function versionKey(item) {
    const m = /(\d+(?:\.\d+)+|\d+)/.exec(itemSubject(item) || "");
    return m ? m[1].split(".").map(Number) : null;
  }
  // compareVersionDesc orders milestones by version, highest first; unversioned ones fall to the end.
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

  // dedupePmGroups merges duplicate imports of a milestone or sprint into [{ item, members }] sorted by cmp.
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

  // milestonesBody renders one deduped card per milestone, highest version first.
  function milestonesBody(pmItems) {
    const g = groupPM(pmItems);
    const groups = dedupePmGroups(g.milestones, g.byMilestone, compareVersionDesc);
    if (!groups.length) return [el("div", { class: "empty" }, ["No milestones in this repository."])];
    return groups.map((x) => pmGroupCard(x.item, x.members, "milestone"));
  }
  // sprintsBody renders one deduped card per sprint, newest first.
  function sprintsBody(pmItems) {
    const g = groupPM(pmItems);
    const groups = dedupePmGroups(g.sprints, g.bySprint);
    if (!groups.length) return [el("div", { class: "empty" }, ["No sprints in this repository."])];
    return groups.map((x) => pmGroupCard(x.item, x.members, "sprint"));
  }

  // ---- PM board / milestone-sprint detail / sub-issues ----

  // progressBar renders an "n closed of m" line with a filled bar.
  function progressBar(closed, total) {
    const pct = total ? Math.round((closed / total) * 100) : 0;
    const wrap = el("div", { class: "pm-progress" }, []);
    wrap.append(el("div", { class: "pm-bar" }, [el("div", { class: "pm-bar-fill", style: "width:" + pct + "%" }, [])]));
    wrap.append(el("span", { class: "pm-progress-label mono" }, [total ? (closed + " closed of " + total) : "no issues"]));
    return wrap;
  }

  // issueMemberRow renders a state chip and linked subject for a member issue.
  function issueMemberRow(item) {
    const subject = itemSubject(item);
    return el("div", { class: "pm-member" }, [
      stateChip((item.header && item.header.state) || "open"), " ",
      el("a", { href: commitRef(item.commit.hash, "gitmsg/pm") }, [subject || "(untitled)"]),
    ]);
  }

  // pmMembersSection renders a milestone's or sprint's member list with a count and progress bar.
  function pmMembersSection(headLabel, members) {
    const wrap = el("div", { class: "pm-members" }, []);
    wrap.append(el("div", { class: "pm-members-head mono" }, [headLabel + " (" + members.length + ")"]));
    const p = pmProgress(members);
    wrap.append(progressBar(p.closed, p.total));
    for (const m of members) wrap.append(issueMemberRow(m));
    return wrap;
  }

  // subIssuesSection renders an issue's direct children with a progress bar, or null.
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

  // pmRelChip renders a labelled parent, milestone or sprint link, falling back to the bare hash.
  function pmRelChip(labelText, target, hash) {
    const subject = target ? (subjectBody(target.content)[0] || "(untitled)") : hash;
    const row = el("div", { class: "pm-rel" }, [
      el("span", { class: "pm-rel-label mono" }, [labelText]),
      el("a", { class: "pm-rel-link", href: commitRef((target && target.commit.hash) || hash, "gitmsg/pm") }, [subject]),
    ]);
    if (target && target.header && target.header.state) row.append(stateChip(target.header.state));
    return row;
  }

  // pmDetailExtras builds the member, relationship and sub-issue sections for a pm item detail.
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

  // boardCard renders the compact issue card used in board columns.
  function boardCard(item) {
    const subject = itemSubject(item);
    const card = el("div", { class: "card board-card" }, []);
    // Glyph and subject share one flex child so a narrow column wraps the text, not the glyph.
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

  // BOARD_ITEM_CAP bounds the cards a column cell shows before "show N more".
  const BOARD_ITEM_CAP = 7;

  // boardColumnEl renders one board column with its header, count, WIP and capped cards.
  function boardColumnEl(col, issues, state, cellKey, onChange) {
    const column = el("div", { class: "board-col" + (state.collapsedCols.has(col.name) ? " board-col-collapsed" : "") }, []);
    const head = el("div", { class: "board-col-head mono" }, []);
    const caret = el("button", { class: "board-col-toggle", type: "button", "aria-label": "Collapse column" }, [state.collapsedCols.has(col.name) ? "▸" : "▾"]);
    caret.addEventListener("click", (e) => { e.stopPropagation(); state.toggleCol(col.name); onChange(); });
    head.append(caret, el("span", { class: "board-col-name" }, [col.name + " " + issues.length + (col.wip ? " / " + col.wip : "")]));
    if (col.wip && issues.length > col.wip) head.append(el("span", { class: "chip board-wip-over" }, ["over WIP"]));
    const hide = el("button", { class: "board-col-hide", type: "button", "aria-label": "Hide column", title: "Hide column" }, ["✕"]);
    hide.addEventListener("click", (e) => { e.stopPropagation(); state.hideCol(col.name); onChange(); });
    head.append(hide);
    column.append(head);
    if (state.collapsedCols.has(col.name)) return column;
    if (!issues.length) { column.append(el("div", { class: "board-empty mono" }, ["—"])); return column; }
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

  // boardGrid renders the visible columns, each from its own issues or the lane subset laneOf gives.
  function boardGrid(columns, laneOf, state, keyPrefix, onChange) {
    const grid = el("div", { class: "board" }, []);
    for (const col of columns) {
      if (state.hiddenCols.has(col.name)) continue;
      const issues = laneOf ? (laneOf.get(col.name) || []) : col.issues;
      grid.append(boardColumnEl(col, issues, state, keyPrefix + "\x00" + col.name, onChange));
    }
    return grid;
  }

  // newBoardState builds the session-only board UI state.
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

  // boardBody renders the kanban board with a group-by swimlane control and session-only column state.
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
    // draw rebuilds the controls and the flat or laned board.
    function draw() {
      controls.replaceChildren(el("span", { class: "board-groupby-label mono" }, ["Group by"]));
      for (const f of SWIMLANE_FIELDS) {
        const chip = el("button", { class: "filter-chip" + (f === field ? " active" : ""), type: "button" }, [SWIMLANE_LABELS[f]]);
        chip.addEventListener("click", () => { field = f; draw(); });
        controls.append(chip);
      }
      for (const col of board.columns) {
        if (!state.hiddenCols.has(col.name)) continue;
        const restore = el("button", { class: "filter-chip board-col-restore", type: "button" }, ["+ " + col.name]);
        restore.addEventListener("click", () => { state.showCol(col.name); draw(); });
        controls.append(restore);
      }
      if (!field) { body.replaceChildren(boardGrid(board.columns, null, state, "flat", rerender)); return; }
      const lanes = swimlaneOrder(issues, field);
      const sections = [el("div", { class: "board-groupby-indicator mono" }, ["grouped by " + field])];
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
        // A lane whose members all sit in hidden columns would render empty.
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

  // boardView loads the full pm set and the board config and renders the board.
  async function boardView(ctx) {
    const issuesOf = (items) => items.filter((i) => ((i.header && i.header.type) || "issue") === "issue");
    const [all, config] = await Promise.all([loadExtItemsAll(ctx, "pm"), loadSiteConfig(ctx)]);
    return [boardBody(issuesOf(all), config)];
  }

  // highlightFrag wraps the first case-insensitive match of query in a mark, returning children for el.
  function highlightFrag(str, query) {
    const s = str || "";
    const q = (query || "").trim();
    if (!q) return [s];
    const idx = s.toLowerCase().indexOf(q.toLowerCase());
    if (idx < 0) return [s];
    return [s.slice(0, idx), el("mark", { class: "search-mark" }, [s.slice(idx, idx + q.length)]), s.slice(idx + q.length)];
  }

  // searchSnippet returns a highlighted excerpt when the match is in the body or labels, not the subject.
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

  // searchResultCard renders one search hit with its glyph, highlighted subject, meta row and snippet.
  function searchResultCard(item, group, query) {
    const subject = itemSubject(item);
    const h = item.header || {};
    const card = el("div", { class: "card search-result" }, []);
    const head = el("div", { class: "card-head" }, []);
    prependGlyph(head, item, group.ext);
    head.append(el("a", { class: "subject", href: commitRef(item.commit.hash, group.branch) }, highlightFrag(subject || "(untitled)", query)));
    card.append(head);
    card.append(metaRow(item, group.branch));
    const snip = searchSnippet(item, query);
    if (snip) card.append(snip);
    return cardNav(card, item.commit.hash, group.branch);
  }

  // searchResults renders the flat recency lane, or per-extension sections when grouped.
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

  // searchHelp renders the pre-query help and a scope note for the current tier.
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

  // searchInputEl is the mounted search box the "/" shortcut focuses.
  let searchInputEl = null;
  // focusSearchInput focuses the current search box if one is mounted.
  function focusSearchInput() { if (searchInputEl && searchInputEl.focus) { searchInputEl.focus(); return true; } return false; }

  // humanBytes formats a byte count as a compact KB or MB string.
  function humanBytes(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + " MB";
    if (n >= 1024) return Math.round(n / 1024) + " KB";
    return n + " B";
  }

  // searchView renders the tiered item search synchronously; the corpus loads lane by lane and fills the results.
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
    const filters = { type: new Set(), state: new Set(), author: new Set(), label: new Set() };
    let facetExpanded = {};
    let grouped = false;
    const FACET_UI = [["type", "Type"], ["state", "State"], ["author", "Author"], ["label", "Labels"]];
    const FACET_CAP = 8;
    // facetChip renders one value chip whose click toggles the field's selection.
    function facetChip(field, b) {
      const chip = el("button", { class: "facet-chip" + (b.selected ? " selected" : ""), type: "button" }, [b.value, el("span", { class: "facet-count" }, [String(b.count)])]);
      chip.addEventListener("click", () => {
        const set = filters[field];
        if (set.has(b.value)) set.delete(b.value); else set.add(b.value);
        draw();
      });
      return chip;
    }
    // renderFacets builds the facet chip rows, capping each field at FACET_CAP with a "+N more" expander.
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
    // tierButton loads a deeper search window on click and redraws.
    function tierButton(label, busyLabel, extend, full, older) {
      const btn = el("button", { class: "load-more", type: "button" }, [label]);
      btn.addEventListener("click", async () => {
        btn.disabled = true; btn.textContent = busyLabel;
        try { corpus = await loadSearchWindow(ctx, extend, full, older); } catch (e) { /* keep prior corpus */ }
        draw();
      });
      return btn;
    }
    // renderDeeper shows the full-index affordance while the settled corpus is incomplete.
    function renderDeeper() {
      if (deeperWrap) { deeperWrap.remove(); deeperWrap = null; }
      // Coverage flags are final only once the corpus settled.
      if (!corpus || corpus.loading) return;
      const kids = [];
      const incomplete = corpus.truncated || ((corpus.light || corpus.hasOlder) && !corpus.full);
      if (incomplete) {
        const note = corpus.partial
          ? "Currently searching recent items by subject, author, and labels. Loads every message body across all history to match full text (coverage is limited to the bootstrapped prefix)."
          : "Currently searching recent items by subject, author, and labels. Loads every message body across all history to match full text.";
        kids.push(el("div", { class: "search-tier-note" }, [note]));
        const fullBtn = tierButton("Load full search index", "Loading full search index…", true, true, true);
        kids.push(fullBtn);
        fullSearchBytes(ctx).then((bytes) => { if (bytes > 0 && !fullBtn.disabled) fullBtn.textContent = "Load full search index (" + humanBytes(bytes) + ")"; }).catch(() => {});
      }
      if (!kids.length) return;
      deeperWrap = el("div", { class: "load-more-wrap" }, kids);
      wrap.append(deeperWrap);
    }
    const HYDRATE_MATCHES = 25;
    let hydrateToken = 0;
    // hydrateMatches back-fills the top hollow results' bodies and redraws once; a superseded completion is dropped.
    function hydrateMatches(res) {
      const targets = (res.flat || []).slice(0, HYDRATE_MATCHES).map((f) => f.item).filter((it) => it.commit && it.commit.hollow);
      if (!targets.length) return;
      const token = ++hydrateToken;
      hydrateItems(ctx, targets).then(() => { if (token === hydrateToken) draw(); }).catch(() => {});
    }
    // laneProgress is the "searching N of M" suffix while corpus lanes resolve.
    function laneProgress() {
      if (!corpus || !corpus.loading) return "";
      return " · searching " + corpus.loading.done + " of " + corpus.loading.total + " sections…";
    }
    // draw renders the results for the current query, filters and corpus.
    function draw() {
      if (!corpus) { results.replaceChildren(el("div", { class: "loading" }, ["Loading…"])); return; }
      const res = searchItemsFaceted(input.value || "", corpus.perExt, filters);
      // No facets means no query or filter: show the scope help.
      if (!Object.keys(res.facets).length) {
        status.textContent = laneProgress().replace(/^ · /, "");
        results.replaceChildren(searchHelp(corpus));
        renderDeeper();
        return;
      }
      const partial = corpus.truncated || corpus.light || corpus.hasOlder;
      status.textContent = res.total + (res.total === 1 ? " result" : " results") + (partial && !corpus.full && !corpus.loading ? " in loaded items" : "") + laneProgress();
      const nodes = [];
      const facetBox = renderFacets(res);
      if (facetBox) nodes.push(facetBox);
      const groupBtn = el("button", { class: "facet-chip" + (grouped ? " selected" : ""), type: "button" }, ["Group by type"]);
      groupBtn.addEventListener("click", () => { grouped = !grouped; draw(); });
      nodes.push(el("div", { class: "facet-row search-sort" }, [groupBtn]));
      for (const n of searchResults(res, res.terms, grouped)) nodes.push(n);
      results.replaceChildren(...nodes);
      renderDeeper();
      hydrateMatches(res);
    }
    input.addEventListener("input", () => { if (debounce) clearTimeout(debounce); debounce = setTimeout(draw, 150); });
    input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") { input.value = ""; draw(); } });
    draw();
    // Each resolved lane redraws over the growing perExt; the final await settles the coverage flags.
    (async () => {
      try {
        corpus = await loadSearchWindow(ctx, false, false, false, (perExt, done, total) => {
          corpus = { perExt, truncated: false, light: true, hasOlder: false, partial: false, full: false, loading: { done, total } };
          draw();
        });
      } catch (e) { corpus = { perExt: {}, truncated: false, light: false, full: false }; }
      draw();
    })();
    if (input.focus) setTimeout(() => input.focus(), 0);
    return [wrap];
  }

  // KIND_LABELS names each chart series; GRANS is the granularity toggle order.
  const KIND_LABELS = { commits: "commits", posts: "posts", issues: "issues", prs: "PRs", releases: "releases", memos: "memos" };
  const GRANS = ["weekly", "monthly", "yearly"];
  const GRAN_NOUN = { weekly: "week", monthly: "month", yearly: "year" };

  // analyticsSummary renders the stat grid: total, per-kind totals and the most active period.
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

  // chartFilter renders the series filter chips, doubling as the legend.
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

  // granularityToggle renders the weekly, monthly and yearly buttons.
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

  // yAxis renders the fixed count axis with peak, midpoint and zero ticks.
  function yAxis(max) {
    const ticks = max > 1 ? [max, Math.round(max / 2), 0] : [Math.max(max, 1), 0];
    const col = el("div", { class: "activity-yaxis mono" }, []);
    for (const t of ticks) col.append(el("div", { class: "activity-ytick" }, [compactNum(t)]));
    return col;
  }

  // stackedBars renders one stacked column per period, sized to its share of the peak.
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

  // authorSearchHref builds a #/search link with the author facet prefilled by email, else name.
  function authorSearchHref(a) {
    const token = a.email || a.name || "";
    return "#/search/" + encodeURIComponent("author:" + token);
  }

  // analyticsAuthors renders the top authors with a live filter and an autoscroll window of PAGE rows.
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
    // The list is its own scroll container, so the observer roots on it.
    const sentinel = el("div", { class: "scroll-sentinel", "aria-hidden": "true" }, []);
    let shown = PAGE;
    let observer = null;
    // matches filters authors by the name or email query.
    function matches() {
      const q = (filter.value || "").trim().toLowerCase();
      if (!q) return authors;
      return authors.filter((a) => (a.name || "").toLowerCase().indexOf(q) !== -1 || (a.email || "").toLowerCase().indexOf(q) !== -1);
    }
    // draw renders the shown window of matching authors.
    function draw() {
      const all = matches();
      const rows = all.slice(0, shown);
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
    // advance reveals PAGE more rows.
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

  // analyticsView renders repo facts, the summary grid, the activity chart and the top authors.
  async function analyticsView(ctx) {
    const head = await headFor(ctx);
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
    if (data.partial) wrap.append(el("div", { class: "search-tier-note" }, [
      "Showing recent activity only: this bucket's item index is missing or still building, so analytics cover the most recent items rather than all history. Push with a current gitsocial (or run `gitsocial push --site-only`) to index the full history.",
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
    const commitEntries = (stats && Array.isArray(stats.commitTimes)) ? stats.commitTimes.map((t) => ({ kind: "commits", time: t })) : [];
    const chartKinds = commitEntries.length ? ["commits"].concat(data.kinds) : data.kinds.slice();
    const chartEntries = commitEntries.length ? data.entries.concat(commitEntries) : data.entries;
    let gran = "monthly";
    let selected = "all";
    // draw re-buckets the loaded data at the current granularity and series and repaints.
    function draw() {
      const itemBuckets = activityBuckets(data.entries, gran, data.kinds).buckets;
      const mostActive = itemBuckets.reduce((best, b) => (b.total > (best ? best.total : -1) ? b : best), null);
      summarySlot.replaceChildren(analyticsSummary(data, mostActive));
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
      // Deferred until the chart is laid out; scrollWidth is 0 before then.
      setTimeout(() => { chart.scrollLeft = chart.scrollWidth; }, 0);
    }
    draw();
    return [wrap];
  }

  // ---- Lists (#/lists overview + #list:<ext>/<name> detail) ----

  // listMemberRow renders one list member: a link when local, labeled mono text when foreign.
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

  // listCardNav makes a list card navigate to its #list: route, sparing inner links and selections.
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

  // listsView renders one card per list linking to its detail.
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

  // listDetailView renders one list's metadata and resolved members.
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

  // prefRow renders a labelled preference button showing the current value.
  function prefRow(labelText, getValue, onToggle) {
    const btn = el("button", { class: "pref-btn", type: "button" }, [getValue()]);
    btn.addEventListener("click", () => { onToggle(); btn.textContent = getValue(); });
    return el("div", { class: "pref-row" }, [el("span", { class: "pref-label mono" }, [labelText]), btn]);
  }

  // readerPrefsSection renders the reader preferences, driving the same localStorage keys as the header controls.
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

  // siteConfigSection renders the pushed site customization read-only, or null when none was published.
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

  // repoConfigSection renders each extension's in-bucket config JSON read-only.
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

  // FORKS_CAP is how many forks the config page shows before an expand control.
  const FORKS_CAP = 10;

  // forksSection lists the registered forks, capped, with a Load all control; null when none.
  async function forksSection(ctx) {
    // Only the displayed cap is hydrated up front; the count comes from the manifest alone.
    const total = forkRefNames(await manifestFor(ctx)).length;
    if (!total) return null;
    let forks = await loadForks(ctx, FORKS_CAP);
    if (!forks.length) return null;
    const wrap = el("div", { class: "config-section" }, []);
    const head = el("div", { class: "config-head mono" }, ["Forks (" + total + ")"]);
    wrap.append(head);
    const forkRow = (f) => {
      const row = el("div", { class: "tree-row" }, []);
      const shown = f.url.replace(/^https?:\/\//, "");
      if (/^https?:\/\//.test(f.url)) row.append(el("a", { class: "mono", href: f.url }, [shown]));
      else row.append(el("span", { class: "mono selectable" }, [shown]));
      return row;
    };
    if (total <= FORKS_CAP) {
      const list = el("div", { class: "tree-list" }, []);
      for (const f of forks) list.append(forkRow(f));
      wrap.append(list);
      return wrap;
    }
    // The expanded state is entered only when the bulk load completed in full.
    let expanded = false, loaded = false;
    const list = el("div", { class: "tree-list contrib-scroll" }, []);
    const filter = el("input", { class: "contrib-filter", type: "text", placeholder: "Filter forks…", "aria-label": "Filter forks", autocomplete: "off", spellcheck: "false" }, []);
    filter.style.display = "none";
    const toggle = el("button", { class: "load-more" }, ["Load all " + total + " forks"]);
    const note = el("div", { class: "meta" }, []);
    note.style.display = "none";
    const draw = () => {
      const q = (filter.value || "").trim().toLowerCase();
      const base = expanded ? forks : forks.slice(0, FORKS_CAP);
      const rows = q ? base.filter((f) => f.url.toLowerCase().indexOf(q) !== -1) : base;
      list.replaceChildren();
      if (!rows.length) { list.append(el("div", { class: "empty" }, ["No forks match “" + filter.value + "”."])); return; }
      for (const f of rows) list.append(forkRow(f));
    };
    toggle.addEventListener("click", async () => {
      note.style.display = "none";
      if (!loaded) {
        toggle.disabled = true;
        toggle.textContent = "Loading " + total + " forks…";
        let all = null;
        try { all = await loadForks(ctx); } catch (e) { all = null; }
        toggle.disabled = false;
        if (all && all.length > forks.length) forks = all;
        if (!all || all.failed) {
          // Failed or partial: stay capped and honest, keep the retry.
          toggle.textContent = "Load all " + total + " forks";
          note.textContent = all
            ? "Could not load " + all.failed + " of " + total + " forks (the host may be rate limiting). Try again to fetch the rest."
            : "Loading the fork list failed (the host may be rate limiting). Try again.";
          note.style.display = "";
          draw();
          return;
        }
        loaded = true;
      }
      expanded = !expanded;
      toggle.textContent = expanded ? "Show top " + FORKS_CAP : "Show all " + forks.length + " forks";
      filter.style.display = expanded ? "" : "none";
      list.classList.toggle("contrib-scroll", expanded);
      if (!expanded) filter.value = "";
      draw();
    });
    filter.addEventListener("input", draw);
    wrap.append(filter, list, note, toggle);
    list.classList.remove("contrib-scroll");
    draw();
    return wrap;
  }

  // configView renders the config page; its sections load concurrently and fail independently, except a 403.
  async function configView(ctx) {
    const wrap = el("div", { class: "detail config-view" }, []);
    wrap.append(el("div", { class: "subject" }, ["Configuration"]));
    wrap.append(readerPrefsSection());
    const guard = (p) => p.catch((e) => {
      if (e && e.forbidden) throw e;
      return el("div", { class: "config-section" }, [el("div", { class: "meta" }, ["This section failed to load."])]);
    });
    const [site, forks, repo] = await Promise.all([
      guard(siteConfigSection(ctx)),
      guard(forksSection(ctx)),
      guard(repoConfigSection(ctx)),
    ]);
    if (site) wrap.append(site);
    if (forks) wrap.append(forks);
    wrap.append(repo);
    return [wrap];
  }

  // LIST_HEADINGS is each list route's heading, the nav label verbatim.
  const LIST_HEADINGS = { issues: "Issues", milestones: "Milestones", sprints: "Sprints", prs: "Pull Requests", timeline: "Timeline", releases: "Releases", memos: "Memos", commits: "Commits" };

  // listHeading renders a list route's h1.
  function listHeading(tab) {
    return el("h1", {}, [LIST_HEADINGS[tab]]);
  }

  // countHead renders a view's total-count line.
  function countHead(n, singular, plural) {
    return el("div", { class: "view-count" }, [n + " " + (n === 1 ? singular : plural || singular + "s")]);
  }

  // branchesView lists the repo's branches with the default marked.
  async function branchesView(ctx) {
    const { branches, defaultBranch } = await listBranches(ctx);
    if (!branches.length) return [el("div", { class: "empty" }, ["No branches in this repository."])];
    const nodes = [countHead(branches.length, "branch", "branches")];
    if (defaultBranch) nodes.push(el("div", { class: "page-actions" }, [
      el("a", { class: "action-link", href: compareRef(defaultBranch, "") }, ["⇄ Compare branches"]),
    ]));
    for (const b of branches) {
      const card = el("div", { class: "card" }, []);
      const head = el("div", { class: "card-head" }, [el("a", { class: "subject mono", href: "#branch:" + b.name }, [b.name])]);
      if (b.isDefault) head.append(el("span", { class: "chip" }, ["default"]));
      if (defaultBranch && b.name !== defaultBranch) head.append(el("a", { class: "hash compare-link", href: compareRef(defaultBranch, b.name) }, ["compare"]));
      card.append(head);
      nodes.push(card);
    }
    return nodes;
  }

  // tagsView renders one card per tag linking to its commit detail.
  async function tagsView(ctx) {
    const tags = await listTags(ctx);
    if (!tags.length) return [el("div", { class: "empty" }, ["No tags in this repository."])];
    return [countHead(tags.length, "tag")].concat(tags.map((t) => {
      const card = el("div", { class: "card" }, []);
      const head = el("div", { class: "card-head" }, [
        el("a", { class: "subject mono", href: "#tag:" + t.name }, [t.name]),
        el("a", { class: "hash", href: "#tag:" + t.name }, [t.sha.slice(0, 12)]),
      ]);
      card.append(head);
      return cardTagNav(card, t.name);
    }));
  }

  // cardTagNav makes a tag card navigate to its #tag: route, sparing inner links and selections.
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

  // parseTagger parses a tagger line into { name, email, time }, or null.
  function parseTagger(tagger) {
    const m = /^(.*) <([^>]*)> (\d+) /.exec(tagger || "");
    return m ? { name: m[1], email: m[2], time: parseInt(m[3], 10) } : null;
  }

  // tagDetail renders a tag page: header, commits since the previous tag, and the three-dot diff against it.
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

    if (prevCommit && prevCommit !== peeled.commit) {
      const mb = await resolveMergeBase(ctx, peeled.commit, prevCommit, DETAIL_WALK_CAP);
      const headTree = await commitTree(ctx, peeled.commit);
      const baseTree = await commitTree(ctx, mb || prevCommit);
      if (headTree && baseTree) {
        const entries = await diffTrees(ctx, baseTree, headTree);
        wrap.append(diffSection(ctx, entries, "Files changed since " + prev.name, mb ? [] : ["no common ancestor — raw two-dot diff"]));
      }
    }
    return [wrap];
  }

  // commitMemberRow renders a short hash and linked subject for a commit on the tag page.
  function commitMemberRow(c) {
    return el("div", { class: "pm-member" }, [
      el("a", { class: "hash mono", href: commitRef(c.hash, "") }, [c.short]),
      el("a", { href: commitRef(c.hash, "") }, [subjectBody(c.content)[0] || "(no message)"]),
    ]);
  }

  // tagCommitsSection lists the commits a tag introduces over the previous tag, paged; the oldest tag lists its history.
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

  // commitCard renders the one code-commit card; id, time and refSha keep parity with the generated commits page.
  function commitCard(c, name, opts) {
    const o = opts || {};
    const ref = commitRef(o.refSha || c.hash, name);
    const card = el("div", o.id ? { class: "card", id: o.id } : { class: "card" }, []);
    card.append(el("div", { class: "card-head" }, [
      el("span", { class: "type-glyph tg-commit", title: "commit" }, ["◦"]),
      el("a", { class: "subject", href: ref }, [subjectBody(c.content)[0] || "(no message)"]),
    ]));
    const meta = el("span", { class: "meta" }, [
      commitAuthorEl(c), " · ", o.time || timeEl(c.authorTime), " · ",
      el("a", { class: "hash", href: ref }, [c.short]),
    ]);
    if (o.chip && name) meta.append(el("span", { class: "chip" }, [name]));
    card.append(meta);
    return cardNav(card, c.hash, name);
  }

  // utcDate formats a unix time as the page layer's UTC YYYY-MM-DD.
  function utcDate(unixSeconds) {
    if (!unixSeconds) return "";
    const d = new Date(unixSeconds * 1000);
    return isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10);
  }

  // commitsView renders one commits page, row for row the generated page for the same route.
  async function commitsView(ctx, page) {
    const r = await loadCommitsPage(ctx, page);
    const wrap = el("div", { class: "detail" }, []);
    const bits = r.page > 0
      ? [r.rows.length + " commits", r.branch, "older page " + r.page]
      : [r.total + " commits", r.branch, "newest first"];
    wrap.append(el("div", { class: "meta" }, [bits.filter(Boolean).join(" · ")]));
    if (!r.rows.length) {
      wrap.append(el("div", { class: "empty" }, [r.missing ? "No such commits page." : "No commits on the default branch."]));
      return [listHeading("commits"), wrap];
    }
    for (const c of r.rows) wrap.append(commitCard(c, r.branch, { id: "c-" + c.short, time: utcDate(c.authorTime), refSha: c.short }));
    const links = [];
    if (r.page > 0) links.push(el("a", { class: "action-link", href: r.page === r.sealed ? "#/commits" : "#/commits/" + (r.page + 1) }, ["← newer"]));
    const older = r.page > 0 ? r.page - 1 : r.sealed;
    if (older > 0) links.push(el("a", { class: "action-link", href: "#/commits/" + older }, ["older →"]));
    if (links.length) wrap.append(el("div", { class: "page-actions" }, links));
    return [listHeading("commits"), wrap];
  }

  // branchLogView renders a branch's paged commit log.
  async function branchLogView(ctx, name) {
    const first = await loadBranchLogWindow(ctx, name, false);
    if (!first.tip) return [el("div", { class: "err" }, ["Branch not found: " + name])];
    const wrap = el("div", { class: "detail" }, []);
    wrap.append(el("div", { class: "subject" }, [name]));
    const actions = el("div", { class: "page-actions" }, [
      el("a", { class: "action-link", href: fileRef("", name) }, ["browse files →"]),
    ]);
    const { defaultBranch } = await listBranches(ctx);
    if (defaultBranch && defaultBranch !== name) actions.append(el("a", { class: "action-link", href: compareRef(defaultBranch, name) }, ["⇄ compare with " + defaultBranch]));
    wrap.append(actions);
    if (!first.items.length) { wrap.append(el("div", { class: "empty" }, ["No commits on this branch."])); return [wrap]; }
    for (const n of pagedListView(first,
      (commits, box) => box.replaceChildren(...commits.map((c) => commitCard(c, name))),
      () => loadBranchLogWindow(ctx, name, true))) wrap.append(n);
    return [wrap];
  }

  // comparePicker builds a base or head ref select over branches and tags; changing it navigates.
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

  // compareView renders the compare page: pickers, head-side commits since the merge-base, and the three-dot diff.
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
    const mb = await resolveMergeBase(ctx, headR.sha, baseR.sha, DETAIL_WALK_CAP);
    const headTree = await commitTree(ctx, headR.sha);
    const baseTree = await commitTree(ctx, baseR.sha);
    if (!headTree || !baseTree) { wrap.append(el("div", { class: "err" }, ["A ref's commit objects are missing from this bucket."])); return [wrap]; }
    const caveats = [];
    let leftTree = baseTree;
    if (mb) leftTree = await commitTree(ctx, mb) || baseTree;
    else caveats.push("no common ancestor — raw two-dot diff");
    const excludeFrom = mb || baseR.sha;
    const first = await loadCompareCommitsWindow(ctx, excludeFrom, headR.sha, false);
    const commitsWrap = el("div", { class: "compare-commits" }, []);
    commitsWrap.append(el("div", { class: "diff-head" }, [el("span", { class: "subject" }, ["Commits"])]));
    if (!first.items.length) commitsWrap.append(el("div", { class: "empty" }, ["No commits on head that base lacks (head is behind or level with base)."]));
    else for (const n of pagedListView(first,
      (commits, box) => box.replaceChildren(...commits.map((c) => commitCard(c, headR.kind === "branch" ? headName : ""))),
      () => loadCompareCommitsWindow(ctx, excludeFrom, headR.sha, true))) commitsWrap.append(n);
    wrap.append(commitsWrap);
    const entries = await diffTrees(ctx, leftTree, headTree);
    wrap.append(diffSection(ctx, entries, "Files changed", caveats));
    return [wrap];
  }

  const SVG_NS = "http://www.w3.org/2000/svg";
  // svgEl builds an SVG-namespaced element; createElement would assign the HTML namespace and never render.
  function svgEl(tag, attrs, children) {
    const node = document.createElementNS(SVG_NS, tag);
    // SVGElement.className is read-only, so class goes through setAttribute and is mirrored onto classList.
    if (attrs) for (const k in attrs) {
      node.setAttribute(k, attrs[k]);
      if (k === "class" && node.classList) for (const c of String(attrs[k]).split(/\s+/).filter(Boolean)) node.classList.add(c);
    }
    for (const c of children || []) node.append(c);
    return node;
  }

  // GRAPH_LANE_W, GRAPH_ROW_H and GRAPH_DOT_R set the gutter geometry.
  const GRAPH_LANE_W = 18, GRAPH_ROW_H = 40, GRAPH_DOT_R = 4;
  // GRAPH_LANE_VARS pairs each lane's theme token with its light-theme fallback.
  const GRAPH_LANE_VARS = [
    ["--link", "#008787"], ["--closed", "#8957e5"], ["--open", "#1f9d55"],
    ["--warn", "#bf8700"], ["--danger", "#cf222e"], ["--i-blue", "#1a85d4"],
    ["--i-vermilion", "#d5512f"], ["--i-indigo", "#693acf"],
  ];
  // graphLaneColors resolves the lane palette from the body's computed custom properties.
  function graphLaneColors() {
    let cs = null;
    try {
      if (typeof getComputedStyle === "function" && typeof document !== "undefined" && document.body) cs = getComputedStyle(document.body);
    } catch (e) { cs = null; }
    return GRAPH_LANE_VARS.map(([name, fallback]) => {
      let v = "";
      if (cs) { try { v = String(cs.getPropertyValue(name) || "").trim(); } catch (e) { v = ""; } }
      return v || fallback;
    });
  }

  // buildGraphGutter draws the lane gutter as one SVG: a dot per commit and a line to each loaded parent.
  function buildGraphGutter(rows, laneCount) {
    const laneColors = graphLaneColors();
    const graphLaneColor = (lane) => laneColors[lane % laneColors.length];
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

  // GRAPH_ROW_CHIPS caps a row's decorations; the rest fold into one "+N" chip.
  const GRAPH_ROW_CHIPS = 3;
  // graphRefChips returns a row's branch tip, tag and merged-PR chips, capped at GRAPH_ROW_CHIPS.
  function graphRefChips(hash, decor) {
    if (!decor) return [];
    const chips = [];
    const push = (name, node) => chips.push({ name, node });
    const live = decor.tips[hash] || [];
    for (const name of live) push(name, el("span", { class: "chip branch-tip" + (name === decor.defaultBranch ? " default" : ""), title: name }, [name]));
    for (const name of decor.tags[hash] || []) push(name, el("a", { class: "chip tag-tip", href: "#tag:" + name, title: name }, [name]));
    for (const m of decor.merged || []) {
      if (!hash.startsWith(m.short) || live.includes(m.name)) continue;
      push(m.name, m.prSha
        ? el("a", { class: "chip branch-tip merged-branch", href: commitRef(m.prSha, "gitmsg/review"), title: m.name + " (merged pull request)" }, [m.name])
        : el("span", { class: "chip branch-tip merged-branch", title: m.name + " (merged pull request)" }, [m.name]));
    }
    if (chips.length <= GRAPH_ROW_CHIPS) return chips.map((c) => c.node);
    const rest = chips.slice(GRAPH_ROW_CHIPS).map((c) => c.name);
    return chips.slice(0, GRAPH_ROW_CHIPS).map((c) => c.node)
      .concat(el("span", { class: "chip branch-tip", title: rest.join("\n") }, ["+" + rest.length]));
  }

  // graphRowText builds a graph row's text column: chips, hash, subject, author and date.
  function graphRowText(r, decor) {
    const c = r.commit;
    const row = el("div", { class: "graph-row-text" }, []);
    for (const chip of graphRefChips(c.hash, decor)) row.append(chip);
    row.append(el("a", { class: "hash mono", href: commitRef(c.hash, "") }, [c.short]));
    row.append(el("a", { class: "graph-subject", href: commitRef(c.hash, "") }, [subjectBody(c.content)[0] || "(no message)"]));
    row.append(el("span", { class: "meta graph-meta" }, [
      commitAuthorEl(c), " · ", timeEl(c.authorTime),
    ]));
    return row;
  }

  // graphBody renders the SVG gutter beside GRAPH_ROW_H-tall text rows.
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

  // graphView renders the multi-branch commit graph over the newest window with a Load more.
  async function graphView(ctx) {
    let data = await loadGraphWindow(ctx, false);
    const wrap = el("div", { class: "detail graph" }, []);
    wrap.append(el("div", { class: "subject" }, ["Commit graph"]));
    if (!data.commits.length) { wrap.append(el("div", { class: "empty" }, ["No commits in this repository."])); return [wrap]; }
    // Lanes shift as off-window parents arrive, so a load-more redraws the whole graph.
    const scroll = el("div", { class: "graph-scroll" }, []);
    let moreWrap = null;
    // render lays out lanes over the loaded commits and refreshes Load more.
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
    // A file view awaits the tokenizer, bounded, so the pane renders highlighted the first time.
    const prismReady = langForPath(path) ? ensurePrism() : null;
    const obj = await getContentObject(ctx, node.sha);
    if (!obj) return [el("div", { class: "err" }, ["Object not found."])];
    if (prismReady) await withPrismDeadline(prismReady);
    return blobView(obj.body, path, branch, line, lineEnd, ctx, tip);
  }

  // codeView renders the root tree of the default branch.
  async function codeView(ctx) {
    const head = await headFor(ctx);
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

  // HOME_FILE_LIMIT is how many root entries Home shows before the Show all control.
  const HOME_FILE_LIMIT = 3;

  // CHEVRON_SVG holds the trusted inline chevron glyphs, parsed like the icon set.
  const CHEVRON_SVG = {
    down: "<svg fill=\"none\" viewBox=\"0 0 16 16\"><path stroke=\"currentColor\" stroke-width=\"1.6\" stroke-linecap=\"round\" stroke-linejoin=\"round\" d=\"m3.5 6 4.5 4.5L12.5 6\"/></svg>",
    up: "<svg fill=\"none\" viewBox=\"0 0 16 16\"><path stroke=\"currentColor\" stroke-width=\"1.6\" stroke-linecap=\"round\" stroke-linejoin=\"round\" d=\"m3.5 10 4.5-4.5L12.5 10\"/></svg>",
  };
  const chevronTemplates = new Map();

  // chevronEl clones one chevron glyph into a themed span, or null without DOMParser.
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

  // SEARCH_SVG is the trusted magnifier glyph for the Code nav item.
  const SEARCH_SVG = "<svg fill=\"none\" viewBox=\"0 0 16 16\"><circle cx=\"7\" cy=\"7\" r=\"4.25\" stroke=\"currentColor\" stroke-width=\"1.5\"/><path stroke=\"currentColor\" stroke-width=\"1.5\" stroke-linecap=\"round\" d=\"m10.5 10.5 3 3\"/></svg>";
  let searchTemplate;

  // searchIconEl clones the magnifier into a themed span, or null without DOMParser.
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

  // homeFileList renders the root entries, collapsing past HOME_FILE_LIMIT behind a chevron toggle.
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

  // homeActivityRow renders a recent-activity card from index metadata, mirroring the static front page's row.
  function homeActivityRow(item) {
    const branch = item._branch || "";
    const code = item._ext === "code";
    const card = el("div", { class: "card" }, []);
    const head = el("div", { class: "card-head" }, [
      el("a", { class: "subject", href: commitRef(item.commit.hash, branch) }, [itemSubject(item)]),
    ]);
    if (code) head.prepend(el("span", { class: "type-glyph tg-commit", title: "commit" }, ["◦"]));
    else prependGlyph(head, item, item._ext);
    card.append(head);
    const meta = el("span", { class: "meta" }, [item.author || "", " · ", timeEl(item.effectiveTime)]);
    if (code) meta.append(" · ", el("a", { class: "hash", href: commitRef(item.commit.hash, branch) }, [item.commit.short]));
    card.append(meta);
    return cardNav(card, item.commit.hash, branch);
  }

  // homeActivityMore renders the trailing link to the full timeline.
  function homeActivityMore() {
    const glyph = el("span", { class: "show-more-icon" }, [chevronEl("down") || document.createTextNode("⌄")]);
    return el("a", { class: "show-more", href: "#/timeline" }, [glyph, el("span", { class: "show-more-label" }, ["See more"])]);
  }

  // homeView renders the landing: metadata strip, root files, README and recent activity.
  async function homeView(ctx) {
    const head = await headFor(ctx);
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
    const site = await loadSiteCustomization(ctx);
    if (site && typeof site.description === "string" && site.description.trim()) wrap.append(el("p", { class: "meta" }, [site.description.trim()]));
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
      const obj = await getContentObject(ctx, readme.sha);
      if (obj) wrap.append(renderMarkdown(new TextDecoder().decode(obj.body), { ctx, branch, dir: "", tip: head.sha }));
    }
    const activity = el("div", { class: "home-activity" }, []);
    wrap.append(activity);
    // Not awaited and not the settle promise: the section lands below the fold after first paint.
    loadHomeActivity(ctx).then((items) => {
      if (!items.length) return;
      activity.append(el("h2", { class: "home-activity-head" }, ["Recent activity"]));
      for (const it of items) activity.append(homeActivityRow(it));
      activity.append(homeActivityMore());
    }).catch(() => { /* the landing stands on its own */ });
    return [wrap];
  }

  // codeSidebarTarget maps a code or file route to the sidebar tree's active path, else null.
  function codeSidebarTarget(r) {
    if (!r) return null;
    if (r.type === "code") return { path: "", branch: null };
    if (r.type === "file") return { path: r.path || "", branch: r.branch || null };
    return null;
  }

  // updateCodeSidebar fills or clears the sidebar file tree for the route; any failure clears the slot.
  async function updateCodeSidebar(ctx, r) {
    const slot = typeof document !== "undefined" && document.getElementById ? document.getElementById("nav-tree-slot") : null;
    if (!slot) return;
    const target = codeSidebarTarget(r);
    if (!target) { slot.replaceChildren(); return; }
    try {
      const head = await headFor(ctx);
      const branch = target.branch || headBranchName(head);
      if (!branch) { slot.replaceChildren(); return; }
      const tip = await refTip(ctx, "refs/heads/" + branch);
      if (!tip) { slot.replaceChildren(); return; }
      const rootNode = await resolvePath(ctx, tip, "");
      const rootEntries = rootNode && (await getTree(ctx, rootNode.sha));
      if (!rootEntries) { slot.replaceChildren(); return; }
      // Ancestors auto-expand so the active row is visible.
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


  Object.assign(NS, { LIST_HEADINGS, listHeading, analyticsView, mdSlug, authorEl, commitAuthorEl, countHead, autoScrollListView, boardView, boardBody, branchLogView, branchesView, commitsView, compareView, ensureGrammar, ensurePrism, highlightsSettled, setGrammarBase, highlightTo, langForPath, langForFence, graphView, codeSidebarTarget, codeView, commitDetail, configView, el, filteredListView, focusSearchInput, focusTreeSearch, highlightNav, homeView, icon, iconEl, issuesBody, milestonesBody, sprintsBody, itemDetail, listDetailView, listsView, memoCard, metaRow, mountTree, openFullscreen, pagedListView, prCard, PR_STATES, releaseCard, renderInline, renderList, renderMarkdown, revokeObjectUrls, sanitizeInert, searchIconEl, searchView, setView, tagsView, tagDetail, timelineCard, treeOrBlob, updateCodeSidebar });
  if (typeof module !== "undefined" && module.exports) module.exports = NS;
})();
