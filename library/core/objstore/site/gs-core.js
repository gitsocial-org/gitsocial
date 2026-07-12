// gs-core.js - DOM-free core: fetch/inflate/parse of git loose objects and
// gitmsg headers, refs/manifest resolution, history walk, tree/path, diff engine,
// markdown/GFM AST, thread grouping, edit resolution, authorship, aggregation
// helpers, route parsing, icon-name mapping, and constants. Node-importable
// (module.exports = GS namespace); in the browser it defines the shared window.GS
// namespace the render and app layers extend.

(function () {
  const root = (typeof globalThis !== "undefined") ? globalThis : (typeof window !== "undefined" ? window : this);
  const NS = root.GS || (root.GS = {});

  // Well-known extension data branches.
  const EXT_BRANCHES = {
    social: "refs/heads/gitmsg/social",
    pm: "refs/heads/gitmsg/pm",
    review: "refs/heads/gitmsg/review",
    release: "refs/heads/gitmsg/release",
    memo: "refs/heads/gitmsg/memo",
  };
  const WALK_CAP = 200;
  // DETAIL_WALK_CAP bounds the deeper, single-target walks: the item-detail
  // permalink lookup, the thread-source walk, and a PR's merge-base / tip search.
  // A permalink or deep PR keeps walking history in WALK_CAP windows until the
  // target is reached or this many commits have been visited.
  const DETAIL_WALK_CAP = 2000;
  const CONCURRENCY = 6;

  // deriveBase returns the absolute directory the repo is served from. A
  // ?base=/?repo= query param overrides; otherwise it is the page's own
  // directory, so the site works under any bucket prefix.
  function deriveBase(loc) {
    const params = new URLSearchParams(loc.search || "");
    const override = params.get("base") || params.get("repo");
    if (override) return override.endsWith("/") ? override : override + "/";
    const path = loc.pathname || "/";
    const dir = path.slice(0, path.lastIndexOf("/") + 1);
    return (loc.origin || "") + dir;
  }

  // fetchBytes GETs a bucket key relative to base. A 404 means the object is
  // absent (null, so a single missing object degrades quietly). A 401/403 means
  // the bucket's public read is denied — a whole-site condition, not a missing
  // object — so it throws a `forbidden`-tagged error the app surfaces as one
  // clear page instead of an empty/"not found" view.
  async function fetchBytes(base, key) {
    // Default cache mode: honor the server's Cache-Control. Immutable loose
    // objects are served from disk with no network; mutable keys (refs, HEAD,
    // index artifacts) carry no-cache, so the browser revalidates them every
    // time (If-None-Match -> 304) and never serves a stale ref tip.
    const res = await fetch(base + key);
    if (res.status === 404) return null;
    if (res.status === 401 || res.status === 403) {
      const err = new Error("GET " + key + ": " + res.status + " forbidden");
      err.forbidden = true;
      throw err;
    }
    if (!res.ok) throw new Error("GET " + key + ": " + res.status);
    return new Uint8Array(await res.arrayBuffer());
  }

  // fetchText GETs a key and returns trimmed text; null on 404.
  async function fetchText(base, key) {
    const bytes = await fetchBytes(base, key);
    if (bytes === null) return null;
    return new TextDecoder().decode(bytes).trim();
  }

  // inflate zlib-decompresses git loose-object bytes (the 'deflate' format in
  // the compression-streams API is zlib-wrapped, which is what git writes).
  async function inflate(bytes) {
    const ds = new DecompressionStream("deflate");
    const writer = ds.writable.getWriter();
    writer.write(bytes);
    writer.close();
    return new Uint8Array(await new Response(ds.readable).arrayBuffer());
  }

  // parseLooseObject splits a decompressed object into its "<type> <size>\0"
  // header and raw body bytes.
  function parseLooseObject(raw) {
    let nul = -1;
    for (let i = 0; i < raw.length; i++) {
      if (raw[i] === 0) { nul = i; break; }
    }
    if (nul < 0) throw new Error("loose object: missing header");
    const head = new TextDecoder().decode(raw.subarray(0, nul));
    const space = head.indexOf(" ");
    return { type: head.slice(0, space), body: raw.subarray(nul + 1) };
  }

  // objectKey maps a 40-hex sha to its bucket key.
  function objectKey(sha) {
    return "objects/" + sha.slice(0, 2) + "/" + sha.slice(2);
  }

  // getObject fetches, inflates, and caches one git object.
  async function getObject(ctx, sha) {
    if (ctx.objects.has(sha)) return ctx.objects.get(sha);
    const compressed = await fetchBytes(ctx.base, objectKey(sha));
    if (compressed === null) { ctx.objects.set(sha, null); return null; }
    const obj = parseLooseObject(await inflate(compressed));
    ctx.objects.set(sha, obj);
    return obj;
  }

  // parseCommit turns a commit object into structured fields plus its clean
  // content and parsed gitmsg header.
  function parseCommit(sha, body) {
    const text = new TextDecoder().decode(body);
    const split = text.indexOf("\n\n");
    const headerBlock = split < 0 ? text : text.slice(0, split);
    const message = split < 0 ? "" : text.slice(split + 2);
    const parents = [];
    let tree = "", authorName = "", authorEmail = "", authorTime = 0;
    for (const line of headerBlock.split("\n")) {
      if (line.startsWith("tree ")) tree = line.slice(5).trim();
      else if (line.startsWith("parent ")) parents.push(line.slice(7).trim());
      else if (line.startsWith("author ")) {
        const m = /^author (.*) <([^>]*)> (\d+) /.exec(line);
        if (m) { authorName = m[1]; authorEmail = m[2]; authorTime = parseInt(m[3], 10); }
      }
    }
    return {
      hash: sha, short: sha.slice(0, 12), tree, parents,
      authorName, authorEmail, authorTime,
      content: cleanContent(message),
      rawMessage: message.replace(/\r/g, ""),
      gitmsg: parseGitmsg(message),
      refs: parseRefs(message),
    };
  }

  // cleanContent strips the gitmsg trailer block, leaving user text.
  function cleanContent(message) {
    let idx = -1;
    if (message.startsWith("GitMsg: ")) idx = 0;
    else { const i = message.indexOf("\nGitMsg: "); if (i !== -1) idx = i; }
    const content = idx === -1 ? message : message.slice(0, idx);
    return content.replace(/\r/g, "").trim();
  }

  // parseGitmsg extracts the GitMsg trailer into a flat key->value map (ext, v,
  // type, state, ...), or null when the commit carries no header.
  function parseGitmsg(message) {
    let idx = -1;
    if (message.startsWith("GitMsg: ")) idx = 0;
    else { const i = message.indexOf("\nGitMsg: "); if (i !== -1) idx = i + 1; }
    if (idx === -1) return null;
    const line = message.slice(idx).split("\n")[0];
    const header = {};
    const re = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
    let m;
    while ((m = re.exec(line)) !== null) header[m[1]] = m[2];
    if (!header.ext || !header.v) return null;
    return header;
  }

  // parseRefs extracts GitMsg-Ref trailers with their ` > `-quoted origin
  // content from a commit message. Each ref carries the embedded context a
  // commit keeps about content it references (ext, type, author, email, time,
  // ref, and the quoted excerpt), which is the only view a single-bucket reader
  // has of a cross-repo original whose remote thread it cannot fetch.
  function parseRefs(message) {
    const lines = (message || "").replace(/\r/g, "").split("\n");
    const refs = [];
    const re = /([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"/g;
    for (let i = 0; i < lines.length; i++) {
      if (!lines[i].startsWith("GitMsg-Ref:")) continue;
      const fields = {};
      re.lastIndex = 0;
      let m;
      while ((m = re.exec(lines[i])) !== null) fields[m[1]] = m[2];
      const quoted = [];
      let j = i + 1;
      while (j < lines.length && /^ *>/.test(lines[j])) { quoted.push(lines[j].replace(/^ *> ?/, "")); j++; }
      fields.quoted = quoted.join("\n");
      refs.push(fields);
      i = j - 1;
    }
    return refs;
  }

  // refRepoUrl returns the repo-url prefix of a "[url]#type:value" ref, or ""
  // for a workspace-relative ref. A non-empty URL marks a cross-repo reference
  // whose target objects are not in this bucket.
  function refRepoUrl(ref) {
    const s = ref || "";
    const h = s.indexOf("#");
    return h > 0 ? s.slice(0, h) : "";
  }

  // resolveRef reads a plain etag-mode ref key (content is "<40-hex>\n").
  async function resolveRef(base, refName) {
    const text = await fetchText(base, refName);
    if (!text) return null;
    return /^[0-9a-f]{40}$/.test(text) ? text : null;
  }

  // resolveHead reads the HEAD symref ("ref: refs/heads/<branch>\n") and
  // returns { branch, sha } for the default branch it points at.
  async function resolveHead(base) {
    const text = await fetchText(base, "HEAD");
    if (!text) return null;
    if (text.startsWith("ref:")) {
      const branch = text.slice(4).trim();
      return { branch, sha: await resolveRef(base, branch) };
    }
    return /^[0-9a-f]{40}$/.test(text) ? { branch: null, sha: text } : null;
  }

  // A resumable history walk. startWalk seeds a walk state at a tip; walkStep
  // advances it by up to `windowCap` more commits (BFS over parent pointers with
  // bounded concurrency), appending to state.commits and preserving state.frontier
  // / state.visited so a later step continues the NEXT window without refetching
  // (objects already fetched sit in ctx.objects). An empty state.frontier means
  // the history is fully walked. This underlies both the one-shot walkHistory and
  // the "Load more" / deep-lookup paging that accumulates across windows on ctx.
  function startWalk(tipSha) {
    return { visited: new Set(), frontier: [tipSha], commits: [] };
  }

  async function walkStep(ctx, state, windowCap) {
    windowCap = windowCap || WALK_CAP;
    const start = state.commits.length;
    while (state.frontier.length && state.commits.length - start < windowCap) {
      const batch = [];
      while (state.frontier.length && batch.length < CONCURRENCY) {
        const h = state.frontier.shift();
        if (h && !state.visited.has(h)) { state.visited.add(h); batch.push(h); }
      }
      const objs = await Promise.all(batch.map((h) => getObject(ctx, h)));
      const nextParents = [];
      for (let i = 0; i < batch.length; i++) {
        const obj = objs[i];
        if (!obj || obj.type !== "commit") continue;
        const commit = parseCommit(batch[i], obj.body);
        state.commits.push(commit);
        for (const p of commit.parents) if (!state.visited.has(p)) nextParents.push(p);
      }
      state.frontier = nextParents.concat(state.frontier);
    }
    return state;
  }

  // walkedCommits returns a newest-first copy of a walk state's accumulated
  // commits; list building and edit resolution re-run over this growing set.
  function walkedCommits(state) {
    return state.commits.slice().sort((a, b) => b.authorTime - a.authorTime);
  }

  // walkHistory walks parent pointers from a tip in a single window, capped,
  // deduped, returned newest-first by author time — the one-shot form used where
  // paging is not needed (branch log first window, analytics, PR tip search).
  async function walkHistory(ctx, tipSha, cap = WALK_CAP) {
    const state = startWalk(tipSha);
    await walkStep(ctx, state, cap);
    return walkedCommits(state).slice(0, cap);
  }

  // refHash pulls the 12-hex commit hash out of a gitmsg ref value
  // ("[url]#commit:<hash>@<branch>").
  function refHash(ref) {
    const m = /commit:([0-9a-f]{7,40})/.exec(ref || "");
    return m ? m[1].slice(0, 12) : null;
  }

  // parseBranchField splits a PR base/head field ("[url]#branch:<name>") into
  // its repo url ("" for workspace-relative) and branch name.
  function parseBranchField(field) {
    const s = field || "";
    const hash = s.indexOf("#");
    const url = hash > 0 ? s.slice(0, hash) : "";
    const rest = hash >= 0 ? s.slice(hash + 1) : s;
    const m = /^branch:(.+)$/.exec(rest);
    return { url, name: m ? m[1] : "" };
  }

  // effectiveTime returns an item's display/sort timestamp (unix seconds),
  // COALESCEing an imported item's `origin-time` (the real upstream publish
  // time) over the git commit's author time, mirroring the cache's
  // `effective_timestamp` generated column. Imported content (GitHub releases,
  // issues, …) is committed in a single synthetic run whose author times reflect
  // import order, not the real chronology, so sorting on author time alone puts
  // the oldest upstream item first; the origin-time fallback fixes the order.
  function effectiveTime(commit, header) {
    const ot = header && header["origin-time"];
    if (ot) { const ms = Date.parse(ot); if (!isNaN(ms)) return Math.floor(ms / 1000); }
    return (commit && commit.authorTime) || 0;
  }

  // Origin provenance fields (GITMSG §1.9). Fixed at import; MUST NOT change on
  // edit, so an edit's content override never carries these over the canonical.
  const ORIGIN_KEYS = ["origin-author-name", "origin-author-email", "origin-platform", "origin-time", "origin-url"];

  // originHandle derives an @handle from an origin author email, mirroring
  // protocol.OriginDisplayAuthor: GitHub's "id+login@users.noreply.github.com"
  // (and plain "login@users.noreply.github.com") yield "@login"; any other email
  // yields "@<local-part>".
  function originHandle(email) {
    if (!email) return "";
    if (email.endsWith("@users.noreply.github.com")) {
      let login = email.slice(0, -"@users.noreply.github.com".length);
      const plus = login.indexOf("+");
      if (plus >= 0) login = login.slice(plus + 1);
      return "@" + login;
    }
    const at = email.indexOf("@");
    return "@" + (at > 0 ? email.slice(0, at) : email);
  }

  // effectiveAuthor returns an item's display author, COALESCEing an imported
  // item's origin author (origin-author-name, else an @handle from
  // origin-author-email) over the git commit author — mirroring the cache's
  // effective_author_name generated column. Imported content (GitHub issues,
  // releases, PRs, …) is committed by the importer but carries the real upstream
  // author in origin-* fields, so display must prefer the origin author.
  function effectiveAuthor(commit, header) {
    header = header || {};
    if (header["origin-author-name"]) return header["origin-author-name"];
    const h = originHandle(header["origin-author-email"]);
    if (h) return h;
    return (commit && (commit.authorName || commit.authorEmail)) || "unknown";
  }

  // effectiveAuthorEmail returns the identity email an item is attributed to
  // (origin email over git email), so an edit's editor can be told apart from
  // the original author.
  function effectiveAuthorEmail(commit, header) {
    header = header || {};
    return header["origin-author-email"] || (commit && commit.authorEmail) || "";
  }

  // eqFold compares two strings case-insensitively after trimming.
  function eqFold(a, b) { return (a || "").trim().toLowerCase() === (b || "").trim().toLowerCase(); }

  // makeVersion builds one entry of an item's version list: the commit, its
  // resolved header at that point, its displayed content and verbatim raw
  // message, the attributed author (canonical/origin author, shared across
  // versions), an "edited by" editor name when the editor differs, and the
  // version's own real timestamp. Pure data, DOM-free.
  function makeVersion(commit, header, isEdit, author, editorName, effTime, content) {
    return { commit, header, content, rawMessage: commit.rawMessage, author, editorName, edited: isEdit, effectiveTime: effTime };
  }

  // buildVersions returns the ordered version list for one item: the canonical
  // first, then each edit chronologically (oldest edit first, so the last entry
  // is the latest/displayed version). Edit headers merge over the canonical but
  // keep its origin provenance (GITMSG §1.9); an edit's own timestamp is its real
  // edit time (edits carry no origin-time). The author is the canonical/origin
  // author for every row; edit rows carry an editorName only when the editor
  // differs. The data is already in the walked commit set, so a version list
  // grows (canonical + N edits) exactly as more history is walked in.
  function buildVersions(canon, canonHeader, edits, author) {
    const canonEmail = effectiveAuthorEmail(canon, canonHeader);
    const mergeHeader = (own) => {
      const h = Object.assign({}, canonHeader, own || {});
      for (const k of ORIGIN_KEYS) { if (canonHeader[k] !== undefined) h[k] = canonHeader[k]; else delete h[k]; }
      return h;
    };
    const out = [makeVersion(canon, Object.assign({}, canonHeader), false, author, "", effectiveTime(canon, canonHeader), canon.content)];
    for (const e of edits) {
      const editEmail = effectiveAuthorEmail(e, e.gitmsg);
      const editorName = eqFold(editEmail, canonEmail) ? "" : effectiveAuthor(e, e.gitmsg);
      const content = e.content || canon.content;
      out.push(makeVersion(e, mergeHeader(e.gitmsg), true, author, editorName, effectiveTime(e, e.gitmsg || {}), content));
    }
    return out;
  }

  // resolveItems applies same-repo edit resolution: latest edit per canonical
  // wins (overriding header fields and content), retractions drop the item.
  // Commits arrive newest-first, so it iterates oldest-first to let the latest
  // edit land last. An edit whose canonical is not reachable in this bucket
  // (rebuilt history, or a canonical under a URL-qualified origin ref) is
  // promoted to a standalone item so the latest state still renders. Items are
  // ordered newest-first by effectiveTime so imported content sorts by real
  // upstream time, not import order. Each item carries a `versions` list (the
  // full edit chain, canonical first) built from the same walked commit set, so
  // it deepens along with the walk.
  function resolveItems(commits) {
    const chron = commits.slice().reverse();
    const editsFor = new Map();
    const allEditsFor = new Map();
    const canonical = [];
    const byShort = new Set();
    for (const c of chron) byShort.add(c.short);
    for (const c of chron) {
      if (c.gitmsg && c.gitmsg.edits) {
        const t = refHash(c.gitmsg.edits);
        editsFor.set(t, c);
        if (!allEditsFor.has(t)) allEditsFor.set(t, []);
        allEditsFor.get(t).push(c);
      } else canonical.push(c);
    }
    const items = [];
    const consumed = new Set();
    for (const c of canonical) {
      const edit = editsFor.get(c.short);
      const canonHeader = c.gitmsg || {};
      const header = Object.assign({}, canonHeader);
      let content = c.content, rawMessage = c.rawMessage, edited = false, retracted = false, editorName = "";
      if (edit) {
        edited = true;
        consumed.add(c.short);
        Object.assign(header, edit.gitmsg);
        // Origin provenance is fixed at import and MUST NOT change on edit
        // (GITMSG §1.9): keep the canonical's origin author/time for display.
        for (const k of ORIGIN_KEYS) { if (canonHeader[k] !== undefined) header[k] = canonHeader[k]; else delete header[k]; }
        if (edit.gitmsg.retracted === "true") retracted = true;
        if (edit.content) content = edit.content;
        // Raw view shows the protocol truth of the commit whose content is
        // displayed — the edit commit when an edit overrides the canonical.
        rawMessage = edit.rawMessage;
        // Show "edited by" only when the editor differs from the original author.
        if (!eqFold(effectiveAuthorEmail(edit, edit.gitmsg), effectiveAuthorEmail(c, canonHeader))) {
          editorName = effectiveAuthor(edit, edit.gitmsg);
        }
      }
      if (retracted) continue;
      const author = effectiveAuthor(c, canonHeader);
      const versions = buildVersions(c, canonHeader, allEditsFor.get(c.short) || [], author);
      items.push({ commit: c, header, content, rawMessage, edited, editorName, author, effectiveTime: effectiveTime(c, canonHeader), versions });
    }
    for (const [target, edit] of editsFor) {
      if (byShort.has(target) || consumed.has(target)) continue;
      if (edit.gitmsg.retracted === "true") continue;
      const h = Object.assign({}, edit.gitmsg);
      const author = effectiveAuthor(edit, edit.gitmsg);
      // Orphan edits (canonical out of this bucket): the collected edits become
      // the version chain, the first standing in for the missing canonical.
      const orphans = allEditsFor.get(target) || [edit];
      const versions = buildVersions(orphans[0], orphans[0].gitmsg || {}, orphans.slice(1), author);
      items.push({ commit: edit, header: h, content: edit.content, rawMessage: edit.rawMessage, edited: true, editorName: "", author, effectiveTime: effectiveTime(edit, h), versions });
    }
    items.sort((a, b) => b.effectiveTime - a.effectiveTime);
    return items;
  }

  // readRefMode reads the bucket's ref-mode marker.
  async function readRefMode(base) {
    return await fetchText(base, ".gitsocial/ref-mode");
  }

  // loadManifest fetches the push-maintained refs manifest
  // (.gitsocial/site/refs.json, refname → sha); null when the bucket
  // predates it.
  async function loadManifest(base) {
    const text = await fetchText(base, ".gitsocial/site/refs.json");
    if (!text) return null;
    try { return JSON.parse(text); } catch { return null; }
  }

  // refTip resolves a ref tip: the live plain key first (authoritative in
  // etag mode, absent in generation mode), then the manifest — the only
  // source for generation-mode refs, and the discovery index for everything
  // beyond the well-known names.
  async function refTip(ctx, refName) {
    const live = await resolveRef(ctx.base, refName);
    if (live) return live;
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const sha = ctx.manifest && ctx.manifest[refName];
    return sha && /^[0-9a-f]{40}$/.test(sha) ? sha : null;
  }

  // walkStateFor returns the resumable walk cached on ctx under `key`, seeded at
  // `tip`; a changed tip (a push moved the branch) discards the stale walk. The
  // cache lets a tab revisited within a session continue where it left off (and a
  // detail deep-lookup share the deepened history with its list), while a fresh
  // page load builds a fresh ctx and so a fresh walk.
  function walkStateFor(ctx, key, tip) {
    const prev = ctx.walks[key];
    if (prev && prev.tip === tip) return prev;
    const fresh = { tip, state: startWalk(tip) };
    ctx.walks[key] = fresh;
    return fresh;
  }

  // loadItemsIndex fetches the push-maintained metadata index for one extension
  // (.gitsocial/site/items/<ext>/, version 4, brotli — decoded transparently by
  // the browser via Content-Encoding: br) once per context; null when the bucket
  // carries none or only an older-version manifest (the miss is remembered, so a
  // 404 is never refetched). Mirrors loadBodyIndexSharded: the manifest lists
  // immutable sealed shards (browser-cached across pushes) oldest-first plus the
  // no-cache head. Only the EAGER set is fetched here — the head and the newest
  // sealed shard — which covers the timeline window and recent light search; the
  // remaining older shards load on demand (loadOlderItemShards). The returned
  // index carries: tip, complete (whether the manifest covers to the branch
  // root), bodiesBytes (the full-search download size), the eager `items`
  // (newest-first), a `residentShas` set, the pending older-shard keys
  // (newest→oldest), the corpus dir, and whether every shard is already resident.
  async function loadItemsIndex(ctx, ext) {
    if (!ctx.itemsIndex) ctx.itemsIndex = {};
    if (ctx.itemsIndex[ext] !== undefined) return ctx.itemsIndex[ext];
    let idx = null;
    try {
      idx = await loadItemsIndexSharded(ctx, ext);
    } catch (e) { if (e && e.forbidden) throw e; }
    ctx.itemsIndex[ext] = idx;
    return idx;
  }

  // loadItemsIndexSharded reads the manifest, then fetches only the eager set
  // (newest sealed shard + head) and returns the index object. Null when the
  // bucket carries no manifest or an unknown version (4 is the shared gitmsg
  // schema; 5 is the code corpus whose entries also carry parents — the
  // returned index's `version` tells the graph whether the DAG is present).
  // The eager items are newest-first (head newest→oldest, then the newest
  // shard newest→oldest).
  async function loadItemsIndexSharded(ctx, ext) {
    const dir = ".gitsocial/site/items/" + ext + "/";
    const mtext = await fetchText(ctx.base, dir + "manifest.json");
    if (!mtext) return null;
    let m;
    try { m = JSON.parse(mtext); } catch { return null; }
    if (!m || (m.version !== 4 && m.version !== 5) || !/^[0-9a-f]{40}$/.test(m.tip || "") || !Array.isArray(m.shards)) return null;
    const newest = m.shards.length ? m.shards[m.shards.length - 1] : null;
    // Oldest-first ingestion order is: sealed shards (oldest→newest), then the
    // head (newest overall). The eager set is the newest sealed shard + head, so
    // assemble them oldest-first (shard THEN head) before reversing to the
    // newest-first order resolveItems expects — matching loadBodyIndexSharded.
    const eagerKeys = [];
    if (newest) eagerKeys.push(dir + newest.key);
    eagerKeys.push(dir + "head.json");
    const texts = await Promise.all(eagerKeys.map((k) => fetchText(ctx.base, k)));
    const items = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) items.push(it);
    }
    items.reverse();
    const residentShas = new Set(items.map((e) => e.sha));
    const olderShards = m.shards.slice(0, Math.max(0, m.shards.length - 1)).map((s) => dir + s.key).reverse();
    return {
      version: m.version, tip: m.tip, complete: m.complete !== false, bodiesBytes: m.bodiesBytes || 0,
      items, residentShas, olderShards, dir, allResident: olderShards.length === 0,
      olderBytes: m.shards.slice(0, Math.max(0, m.shards.length - 1)).reduce((n, s) => n + (s.bytes || 0), 0),
    };
  }

  // loadOlderItemShards fetches every not-yet-resident older shard of `ext`'s
  // metadata index (immutable, browser-cached), concatenates them oldest→newest,
  // and merges them into the loaded index (newest-first). Idempotent: once every
  // shard is resident it is a no-op. Invoked by scroll-back past the eager window
  // and by the "search older items" affordance.
  async function loadOlderItemShards(ctx, ext) {
    const idx = await loadItemsIndex(ctx, ext);
    if (!idx || idx.allResident || !idx.olderShards.length) return idx;
    const keys = idx.olderShards.slice().reverse(); // oldest→newest
    const texts = await Promise.all(keys.map((k) => fetchText(ctx.base, k)));
    const older = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) older.push(it);
    }
    older.reverse(); // newest-first
    for (const e of older) { if (!idx.residentShas.has(e.sha)) { idx.items.push(e); idx.residentShas.add(e.sha); } }
    idx.olderShards = [];
    idx.allResident = true;
    idx.olderBytes = 0;
    return idx;
  }

  // metaCommit converts one metadata-index entry into the commit-record shape
  // parseCommit produces, but body-less: only the `GitMsg:` header line is known
  // (parsed with the same code, so relations/type/state/origin-* resolve), the
  // message body/content and cross-repo ref excerpts are absent. The index's
  // subject line is kept on the record (light search + result display) but never
  // in `content`, so rendering paths that expect real content stay body-less
  // until hydration. Marked `hollow` so a view that renders the item fetches its
  // loose object and hydrates the body on demand (hydrateItem). Tree/parents are
  // absent; detail walks fetch the loose object as before.
  function metaCommit(e) {
    const header = String(e.header || "");
    return {
      hash: e.sha, short: e.sha.slice(0, 12), tree: "", parents: [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: "", rawMessage: header, subject: String(e.subject || ""),
      gitmsg: parseGitmsg(header), refs: [], hollow: true,
    };
  }

  // indexCommit converts one full-message entry (the bodies search corpus) into
  // the commit-record shape parseCommit produces, so resolveItems and search are
  // agnostic to the source. Tree/parents are absent; not hollow (the body is
  // present).
  function indexCommit(e) {
    const msg = String(e.message || "");
    return {
      hash: e.sha, short: e.sha.slice(0, 12), tree: "", parents: [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: cleanContent(msg),
      rawMessage: msg.replace(/\r/g, ""),
      gitmsg: parseGitmsg(msg),
      refs: parseRefs(msg),
    };
  }

  // hydrateCommit fills a hollow commit record (built from the metadata index)
  // from its loose object: the message body/content, cross-repo ref excerpts,
  // tree and parents. A no-op for a full record. On a missing/foreign object the
  // record is left body-less but no longer hollow (no repeated refetch).
  async function hydrateCommit(ctx, commit) {
    if (!commit || !commit.hollow) return;
    const obj = await getObject(ctx, commit.hash);
    if (!obj || obj.type !== "commit") { commit.hollow = false; return; }
    const full = parseCommit(commit.hash, obj.body);
    commit.content = full.content;
    commit.rawMessage = full.rawMessage;
    commit.refs = full.refs;
    commit.tree = full.tree;
    commit.parents = full.parents;
    if (full.gitmsg) commit.gitmsg = full.gitmsg;
    if (full.authorName) commit.authorName = full.authorName;
    if (full.authorEmail) commit.authorEmail = full.authorEmail;
    if (full.authorTime) commit.authorTime = full.authorTime;
    commit.hollow = false;
  }

  // hydrateItem fetches the bodies of an item's version commits (canonical +
  // edits, usually one) and recomputes its displayed and per-version content
  // from them — mirroring resolveItems' content selection (latest edit's body
  // over the canonical). Idempotent: once the commits are full it is a no-op.
  async function hydrateItem(ctx, item) {
    if (!item) return;
    const versions = item.versions || [];
    const commits = [];
    for (const v of versions) if (v.commit && commits.indexOf(v.commit) === -1) commits.push(v.commit);
    if (item.commit && commits.indexOf(item.commit) === -1) commits.push(item.commit);
    for (const c of commits) await hydrateCommit(ctx, c);
    const canon = versions.length ? versions[0].commit : item.commit;
    const canonContent = canon ? canon.content : "";
    for (let i = 0; i < versions.length; i++) {
      const vc = versions[i].commit;
      versions[i].content = i === 0 ? canonContent : ((vc && vc.content) || canonContent);
      if (vc) versions[i].rawMessage = vc.rawMessage;
    }
    if (versions.length) {
      const last = versions[versions.length - 1];
      item.content = last.content; item.rawMessage = last.rawMessage;
    } else if (item.commit) {
      item.content = item.commit.content; item.rawMessage = item.commit.rawMessage;
    }
  }

  // hydrateItems hydrates a set of items' bodies with bounded concurrency (the
  // handful actually about to render) — the lazy body fetch that keeps the
  // metadata index small. Full records (non-index buckets) short-circuit.
  async function hydrateItems(ctx, items) {
    const list = (items || []).filter(Boolean);
    let i = 0;
    const worker = async () => { while (i < list.length) { const it = list[i++]; await hydrateItem(ctx, it); } };
    await Promise.all(Array.from({ length: Math.min(CONCURRENCY, list.length) }, worker));
  }

  // bridgeToIndex walks from the live tip toward an index's recorded history,
  // stopping descent at indexed shas, and returns the gap commits once the
  // index tip is reached. Null when the tip is never met within WALK_CAP
  // (rewritten history, or a huge gap) — the caller falls back to a full walk.
  async function bridgeToIndex(ctx, tip, idx, known) {
    const visited = new Set();
    let frontier = [tip];
    const out = [];
    let metTip = false;
    while (frontier.length && out.length < WALK_CAP) {
      const h = frontier.shift();
      if (!h || visited.has(h)) continue;
      visited.add(h);
      if (h === idx.tip || known.has(h)) { if (h === idx.tip) metTip = true; continue; }
      const obj = await getObject(ctx, h);
      if (!obj || obj.type !== "commit") continue;
      const commit = parseCommit(h, obj.body);
      out.push(commit);
      frontier = commit.parents.concat(frontier);
    }
    return metTip && !frontier.length ? out : null;
  }

  // seedWalkFromIndex primes a fresh ext walk state from the items index's EAGER
  // set (newest shard + head): with a current index the timeline window loads
  // from a couple of JSON fetches; with an advanced live tip the gap is bridged
  // to the manifest tip first. The state is born fully walked (empty frontier)
  // ONLY when the manifest is complete AND every shard is already resident (a
  // short branch); otherwise older shards stay pending on the state (w.older) and
  // are loaded one at a time by stepExtWalk as paging/deep lookups need them,
  // with the loose-object walk as the final fallback. A missing index, or a
  // bridge that never meets it, leaves the state untouched (full walk as before).
  async function seedWalkFromIndex(ctx, ext, w) {
    const idx = await loadItemsIndex(ctx, ext);
    if (!idx || w.state.commits.length) return;
    const known = new Set(idx.residentShas);
    let gap = [];
    if (w.tip !== idx.tip) {
      gap = await bridgeToIndex(ctx, w.tip, idx, known);
      if (!gap) return;
    }
    const state = w.state;
    for (const c of gap) { state.visited.add(c.hash); state.commits.push(c); }
    for (const e of idx.items) {
      if (state.visited.has(e.sha)) continue;
      state.visited.add(e.sha);
      state.commits.push(metaCommit(e));
    }
    w.older = idx.olderShards.slice();
    w.ext = ext;
    // A complete, fully-resident index is exhausted; otherwise leave the frontier
    // empty (metaCommits carry no parents) so stepExtWalk drains older shards
    // before it would fall to a loose walk.
    state.frontier = [];
    w.indexBacked = true;
  }

  // loadNextItemShard pulls the next-older pending shard onto an index-seeded
  // walk state (its metadata entries become body-less metaCommits), returning
  // true when a shard was loaded. Older shards are consumed newest→oldest, so a
  // scroll-back or deep lookup deepens history one immutable, browser-cached
  // shard at a time.
  async function loadNextItemShard(ctx, w) {
    if (!w.older || !w.older.length) return false;
    const key = w.older.shift();
    const text = await fetchText(ctx.base, key);
    if (!text) return true;
    let doc;
    try { doc = JSON.parse(text); } catch { return true; }
    // Shard docs store their members oldest-first; push newest-first so
    // state.commits stays uniformly newest-first (matching the eager set and the
    // loose walk), keeping same-timestamp tie order stable across shard seams.
    const entries = (doc && Array.isArray(doc.items)) ? doc.items.slice().reverse() : [];
    for (const e of entries) {
      if (w.state.visited.has(e.sha)) continue;
      w.state.visited.add(e.sha);
      w.state.commits.push(metaCommit(e));
    }
    return true;
  }

  // stepExtWalk advances an ext walk state by one window: an index-seeded state
  // drains its next-older shard (cheap, immutable) before it would touch the
  // per-commit loose walk; a non-index walk steps as before. Returns whether the
  // history is exhausted (no pending shards and an empty frontier).
  async function stepExtWalk(ctx, w, cap) {
    if (w.older && w.older.length) { await loadNextItemShard(ctx, w); return; }
    if (w.state.frontier.length) await walkStep(ctx, w.state, cap);
  }

  // extWalkExhausted reports whether an ext walk has nothing left to load (no
  // pending older shards and an empty loose frontier).
  function extWalkExhausted(w) {
    return !(w.older && w.older.length) && !w.state.frontier.length;
  }

  // extSetComplete reports whether `ext`'s already-loaded walk covers its WHOLE
  // history — true for an index-seeded walk (its item set came from the metadata
  // shards, complete regardless of the loose bound) or a genuinely exhausted loose
  // walk; false when a loose walk stopped at the COUNTS_WALK_CAP bound (an
  // index-absent or stale-manifest bucket). Callers that ran loadExtItemsAll use
  // it to decide whether to note limited coverage. Returns true when the branch is
  // absent (nothing to cover). Reads the cached walk; drives no fetches.
  async function extSetComplete(ctx, ext) {
    const w = await extWalkState(ctx, ext);
    if (!w) return true;
    return !!w.indexBacked || extWalkExhausted(w);
  }

  // extWalkState returns an extension branch's resumable walk (null when the
  // branch is absent), seeding a fresh state from the items index once per
  // tip so list, detail, and thread paths share the index-backed set.
  async function extWalkState(ctx, ext) {
    const tip = await refTip(ctx, EXT_BRANCHES[ext]);
    if (!tip) return null;
    const w = walkStateFor(ctx, "ext:" + ext, tip);
    // Seed exactly once per walk: memoize the in-flight seed promise so a second
    // caller racing in (e.g. the timeline's parallel loadTimelineWindow +
    // loadInteractionCounts) awaits the SAME seed rather than proceeding over a
    // half-seeded state (the old boolean flag flipped before the await resolved).
    if (!w.seedPromise) w.seedPromise = seedWalkFromIndex(ctx, ext, w);
    await w.seedPromise;
    return w;
  }

  // withWalkLock serializes async operations that mutate one shared walk state.
  // Multiple consumers reference the same per-ext `w` (list, timeline, counts,
  // detail), and each advances it across `await` points by shifting w.older and
  // pushing into w.state — interleaving two of them corrupts progress (items are
  // dropped, or the walk wedges on "Loading…"). Every consumer runs its body
  // through this per-`w` promise chain so they execute one after another.
  function withWalkLock(w, fn) {
    const prev = w.lock || Promise.resolve();
    let release;
    w.lock = new Promise((r) => { release = r; });
    return prev.then(fn).finally(release);
  }

  // loadExtItemsWindow returns a bounded, body-hydrated render window of `ext`'s
  // resolved items. It grows a `shown` cursor by WALK_CAP per call (extend), walks
  // enough history to fill it (a no-op for an index-seeded walk, whose full item
  // set loads from the metadata index), resolves over the FULL accumulated commit
  // set (so edit/retraction resolution improves as windows deepen), then fetches
  // the loose-object bodies for ONLY the items in this window — the metadata index
  // carries no bodies. truncated marks that more items remain (unwalked history OR
  // beyond the cursor), which the "Load more" affordance reflects.
  async function loadExtItemsWindow(ctx, ext, extend) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { items: [], truncated: false };
    return withWalkLock(w, async () => {
      if (w.shown === undefined) w.shown = 0;
      if (extend) { w.shown += WALK_CAP; if (!extWalkExhausted(w)) await stepExtWalk(ctx, w, WALK_CAP); }
      else { if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP); if (w.shown < WALK_CAP) w.shown = WALK_CAP; }
      while (!extWalkExhausted(w) && w.state.commits.length < w.shown) await stepExtWalk(ctx, w, WALK_CAP);
      const items = resolveItems(walkedCommits(w.state));
      const shown = items.slice(0, w.shown);
      await hydrateItems(ctx, shown);
      return { items: shown, truncated: !extWalkExhausted(w) || items.length > w.shown };
    });
  }

  // loadExtItems returns an extension branch's resolved items, backed by the
  // resumable walk cache (at least one window). The array-returning form the
  // timeline, detail, and thread paths call; empty when the branch is absent.
  async function loadExtItems(ctx, ext) {
    return (await loadExtItemsWindow(ctx, ext, false)).items;
  }

  // loadExtItemsUpTo deepens `ext`'s walk until the history is exhausted or
  // `budget` commits have been visited, then returns the resolved items — used by
  // the detail thread-source walk so comments beyond the first window attach.
  // Index-seeded walks are already exhausted, so this costs no object fetches.
  async function loadExtItemsUpTo(ctx, ext, budget) {
    const w = await extWalkState(ctx, ext);
    if (!w) return [];
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      while (!extWalkExhausted(w) && w.state.visited.size < budget) await stepExtWalk(ctx, w, WALK_CAP);
      return resolveItems(walkedCommits(w.state));
    });
  }

  // findItemDeep resolves one item by hash within `ext`, deepening the resumable
  // walk one window at a time until the item is found or DETAIL_WALK_CAP commits
  // have been visited. A full-hash permalink additionally fetches its target
  // object directly up front, so the item resolves without deepening even when
  // no index covers it. onProgress(visited) fires before each extra window so the
  // caller can show a "searching history" note. Returns { item, items }; item is
  // null when the target is unreachable within the budget.
  async function findItemDeep(ctx, ext, hash, onProgress) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { item: null, items: [] };
    return withWalkLock(w, async () => {
      const match = (items) => items.find((i) => i.commit.hash === hash || i.commit.short === hash || i.commit.hash.startsWith(hash)) || null;
      let direct = null;
      if (/^[0-9a-f]{40}$/.test(hash) && !w.state.visited.has(hash)) {
        const obj = await getObject(ctx, hash);
        if (obj && obj.type === "commit") direct = parseCommit(hash, obj.body);
      }
      const resolved = () => {
        const commits = walkedCommits(w.state);
        if (direct && !w.state.visited.has(direct.hash)) {
          commits.push(direct);
          commits.sort((a, b) => b.authorTime - a.authorTime);
        }
        return resolveItems(commits);
      };
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      let items = resolved();
      let found = match(items);
      while (!found && !extWalkExhausted(w) && w.state.visited.size < DETAIL_WALK_CAP) {
        if (onProgress) onProgress(w.state.visited.size);
        await stepExtWalk(ctx, w, WALK_CAP);
        items = resolved();
        found = match(items);
      }
      if (found) await hydrateItem(ctx, found);
      return { item: found, items };
    });
  }

  // loadBranchLogWindow is the resumable branch-log walk: a paged commit list
  // (newest-first accumulated commits) with truncated marking unwalked history.
  // For the DEFAULT branch, when the bucket carries the v4 code items index, the
  // log is served from the index metadata (loadBranchLogIndexed) with NO
  // per-commit loose-object GET: a commit reachable from the default tip always
  // attributes to the default branch (site_code_index.go: "default wins"), so
  // entry.branch === defaultBranch is exactly the default branch's log, already
  // newest-first. Non-default branches (the index can't answer reachability for
  // them — shared ancestors attribute to default) and index-absent/non-v4 buckets
  // fall through to the loose walk unchanged.
  async function loadBranchLogWindow(ctx, name, extend) {
    const indexed = await loadBranchLogIndexed(ctx, name, extend);
    if (indexed) return indexed;
    const tip = await refTip(ctx, "refs/heads/" + name);
    if (!tip) return { tip: null, items: [], truncated: false };
    const w = walkStateFor(ctx, "branch:" + name, tip);
    if (extend || w.state.commits.length === 0) await walkStep(ctx, w.state, WALK_CAP);
    return { tip, items: walkedCommits(w.state), truncated: w.state.frontier.length > 0 };
  }

  // loadBranchLogIndexed serves the DEFAULT branch's log from the code items
  // index, or returns null so loadBranchLogWindow falls back to the loose walk.
  // Null when: the branch is not the bucket's default, or no v4 code index is
  // present. It reuses the timeline's index machinery (codeIndexWalkState +
  // loadNextCodeShard) so shard paging is not duplicated — the same immutable,
  // browser-cached shards the code timeline reads. The default branch's commits
  // are the index entries attributed to it (default wins over any feature
  // attribution), already newest-first; a `shown` cursor grows WALK_CAP per
  // extend, draining older shards until it is filled or every shard is resident,
  // so autoscroll/Load-more paging extends the log one shard page at a time. Like
  // the code timeline this simply serves the index state — an in-flight push whose
  // live default tip is ahead of the index shows the last-indexed state and the
  // next push closes the gap (the code corpus manifest tip is a synthetic digest,
  // not a real commit sha, so a live-tip-to-index bridge is not available here).
  async function loadBranchLogIndexed(ctx, name, extend) {
    const { defaultBranch } = await listBranches(ctx);
    if (!defaultBranch || name !== defaultBranch) return null;
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    const tip = await refTip(ctx, "refs/heads/" + name);
    const key = "branchLog:" + name;
    const b = ctx.walks[key] || (ctx.walks[key] = { shown: 0 });
    return withWalkLock(w, async () => {
      b.shown = extend ? b.shown + WALK_CAP : Math.max(b.shown, WALK_CAP);
      const filtered = () => w.items.filter((c) => (c._branch || "") === defaultBranch);
      // Drain older shards until the default-branch slice fills the cursor or no
      // pending shard remains (progress-guarded: a shard that adds nothing stops
      // the loop rather than spinning).
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && filtered().length < b.shown) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const items = filtered();
      const shown = items.slice(0, b.shown);
      const truncated = items.length > b.shown || (w.older && w.older.length > 0) || !w.complete;
      return { tip, items: shown, truncated };
    });
  }

  // startExcludingWalk seeds a resumable walk at `tip` whose visited set is
  // pre-seeded with every ancestor of `baseSha` (bounded by cap), so the walk
  // never descends into or emits commits reachable from base. This yields the
  // head-side-only commits of a compare (the commits `head` has that `base`
  // lacks), in the same resumable shape as startWalk so it pages with walkStep.
  async function startExcludingWalk(ctx, tip, baseSha, cap) {
    cap = cap || DETAIL_WALK_CAP;
    const visited = new Set();
    let frontier = [baseSha];
    let seen = 0;
    while (frontier.length && seen < cap) {
      const h = frontier.shift();
      if (!h || visited.has(h)) continue;
      visited.add(h); seen++;
      const obj = await getObject(ctx, h);
      if (obj && obj.type === "commit") for (const p of parseCommit(h, obj.body).parents) frontier.push(p);
    }
    // The base commit itself is excluded (its ancestors already are); seed the
    // walk at the head tip with that exclusion set as the visited frontier guard.
    return { visited, frontier: [tip], commits: [] };
  }

  // loadCompareCommitsWindow pages the head-side commits of a compare: the
  // commits reachable from `headSha` but not from `baseSha` (base's ancestors are
  // excluded up front), newest-first, WALK_CAP per window. The resumable walk is
  // cached on ctx keyed by the pair so "Load more" continues without refetching.
  async function loadCompareCommitsWindow(ctx, baseSha, headSha, extend) {
    const key = "compare:" + baseSha + ".." + headSha;
    let entry = ctx.walks[key];
    if (!entry || entry.tip !== headSha) {
      const state = await startExcludingWalk(ctx, headSha, baseSha, DETAIL_WALK_CAP);
      entry = ctx.walks[key] = { tip: headSha, state };
    }
    if (extend || entry.state.commits.length === 0) await walkStep(ctx, entry.state, WALK_CAP);
    return { items: walkedCommits(entry.state), truncated: entry.state.frontier.length > 0 };
  }

  // GRAPH_WINDOW is the number of commits the repository graph loads per window.
  const GRAPH_WINDOW = 150;

  // loadGraphDecorations gathers the graph's ref decorations (git log
  // --decorate style) once per context, from data the route already has or a
  // fixed few index fetches — never a per-commit object GET. Returns:
  //   tips — full sha -> [live code-branch names] (gitmsg/* excluded, matching
  //          the code-branches-only graph), defaultBranch — HEAD's branch (its
  //          chip renders as the solid `default` variant);
  //   tags — raw refs.json tag sha -> [tag names]. No peeling (same rule as the
  //          tags LIST, which also never fetches tag objects): a lightweight
  //          tag's sha is its commit and lands on a row; an annotated tag's sha
  //          is its TAG object and never matches, so it simply doesn't badge;
  //   merged — [{ short, name, prSha }] from merged-PR headers in the review
  //          index's RESIDENT eager set (manifest + newest shard + head — older
  //          merged PRs stay unlabeled rather than draining the corpus): the
  //          recorded merge-head / head-tip short shas mark the rows carrying a
  //          (possibly deleted) head branch's work; prSha is the canonical PR's
  //          full sha when resident, for a chip link to the PR detail.
  // Cached as a promise on ctx.walks so the indexed and loose paths, and every
  // load-more, share one load.
  async function loadGraphDecorations(ctx) {
    const key = "graphDecor";
    if (ctx.walks[key]) return ctx.walks[key];
    const load = (async () => {
      const { branches, defaultBranch } = await listBranches(ctx);
      const tips = {};
      for (const b of branches) {
        if (b.name.startsWith("gitmsg/")) continue;
        const sha = await refTip(ctx, b.ref);
        if (sha) (tips[sha] = tips[sha] || []).push(b.name);
      }
      const tags = {};
      for (const t of await listTags(ctx)) (tags[t.sha] = tags[t.sha] || []).push(t.name);
      let idx = null;
      try { idx = await loadItemsIndex(ctx, "review"); } catch (e) { if (e && e.forbidden) throw e; }
      const merged = [];
      const seen = new Set();
      for (const e of (idx ? idx.items : [])) {
        const h = parseGitmsg(String(e.header || ""));
        if (!h || h.ext !== "review" || h.type !== "pull-request" || h.state !== "merged") continue;
        const name = parseBranchField(h.head).name;
        if (!name || name.startsWith("gitmsg/")) continue;
        // The canonical PR sha (a merged state rides on an edit whose `edits`
        // names the canonical) — resolved within the resident set only.
        const canonShort = refHash(h.edits || "");
        const canon = canonShort ? idx.items.find((x) => String(x.sha || "").startsWith(canonShort)) : e;
        for (const short of [h["merge-head"], h["head-tip"]]) {
          if (!short || !/^[0-9a-f]{6,40}$/.test(short) || seen.has(short + " " + name)) continue;
          seen.add(short + " " + name);
          merged.push({ short, name, prSha: canon ? canon.sha : "" });
        }
      }
      return { tips, tags, defaultBranch, merged };
    })();
    ctx.walks[key] = load;
    return load;
  }

  // orderGraphWindow emits up to `cap` resident code-index commits in the loose
  // graph walk's order: seed the resident DAG's heads, repeatedly pop the
  // newest-authorTime frontier entry (the FIRST such entry wins a tie, so equal
  // timestamps keep a stable order), emit it, and push its resident parents.
  // The result is topological (a parent never precedes the child that reached
  // it) yet time-interleaved across branches — exactly what assignGraphLanes
  // needs. A plain ts-desc sort is NOT that: rebases preserve author dates, so
  // a linear first-parent chain can be ts-non-monotonic, and every inversion
  // split the chain into a phantom parallel lane. Heads are derived from the
  // resident set itself (entries no resident entry names as a parent) rather
  // than the live branch refs, so the graph serves the last-indexed state even
  // when a ref moved ahead of the index (matching loadBranchLogIndexed); the
  // resident set is a prefix of the writer's tip-seeded walk, so every resident
  // entry is reachable from a resident head. Parents outside the resident set
  // are not pushed — they join the frontier once an older shard drains, and the
  // emitted window's edges to them stay absent (the lane simply ends). Returns
  // { commits, more } — more is true when resident entries remain beyond the
  // cap. Deterministic and recomputed per window: draining older shards only
  // APPENDS older entries (never a new child of a resident one), so a grown
  // window keeps the previous window as its exact prefix.
  function orderGraphWindow(items, cap) {
    const byHash = new Map();
    for (const c of items) if (!byHash.has(c.hash)) byHash.set(c.hash, c);
    const isParent = new Set();
    for (const c of byHash.values()) for (const p of c.parents || []) isParent.add(p);
    const frontier = [];
    for (const c of byHash.values()) if (!isParent.has(c.hash)) frontier.push(c.hash);
    const visited = new Set();
    const commits = [];
    while (frontier.length && commits.length < cap) {
      let best = 0, bestT = -Infinity;
      for (let i = 0; i < frontier.length; i++) {
        const c = byHash.get(frontier[i]);
        const t = (c && c.authorTime) || 0;
        if (t > bestT) { bestT = t; best = i; }
      }
      const h = frontier.splice(best, 1)[0];
      if (visited.has(h)) continue;
      visited.add(h);
      const c = byHash.get(h);
      if (!c) continue;
      commits.push(c);
      for (const p of c.parents || []) if (byHash.has(p) && !visited.has(p)) frontier.push(p);
    }
    return { commits, more: frontier.some((h) => !visited.has(h)) };
  }

  // loadGraphWindowIndexed serves the repository graph from the code items index
  // when its entries carry parents (corpus v5), or returns null so
  // loadGraphWindow falls back to the loose walk (an absent index, or a v4
  // bucket pushed before parents existed). The resident index entries — shared
  // with the timeline/branch-log walk, so shards load once — are ordered by the
  // in-memory frontier-pop walk (orderGraphWindow, the loose walk's order) and
  // cut to a `shown` cursor that grows GRAPH_WINDOW per extend, draining older
  // shards until it is filled, mirroring loadBranchLogIndexed's paging. The
  // indexed graph covers CODE branches only (the corpus's membership): gitmsg/*
  // data branches, which the loose walk used to interleave, are omitted —
  // decorations are filtered to match so no label points at an absent row.
  async function loadGraphWindowIndexed(ctx, extend) {
    const w = await codeIndexWalkState(ctx);
    if (!w || !w.hasParents) return null;
    const key = "graphIndexed";
    const g = ctx.walks[key] || (ctx.walks[key] = { shown: 0 });
    const decor = await loadGraphDecorations(ctx);
    return withWalkLock(w, async () => {
      g.shown = extend ? g.shown + GRAPH_WINDOW : Math.max(g.shown, GRAPH_WINDOW);
      // Drain older shards until the window fills or no pending shard remains
      // (progress-guarded, matching loadBranchLogIndexed).
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && w.items.length < g.shown) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const ordered = orderGraphWindow(w.items, g.shown);
      const truncated = ordered.more || (w.older && w.older.length > 0) || !w.complete;
      return { commits: ordered.commits, truncated, decor };
    });
  }

  // loadGraphWindow walks the commit DAG across all branch heads, newest-first by
  // committer/author time, GRAPH_WINDOW commits per window with resumable
  // load-more. When the bucket carries a v5 code items index (entries with
  // parents) the window is served from the index with no per-commit object GET
  // (loadGraphWindowIndexed); otherwise it seeds a max-heap-like frontier from
  // every branch tip and pops the newest commit, expanding its parents, so a
  // merged multi-branch history is interleaved in time order (what a graph needs
  // for stable lane assignment). Returns { commits, truncated, decor } where
  // commits carry { hash, short, parents, authorName, authorEmail, authorTime,
  // content } and decor is the loadGraphDecorations ref map (branch tips, tags,
  // merged-PR labels) the renderer badges rows from. The walk state is cached
  // on ctx so load-more continues without refetching.
  async function loadGraphWindow(ctx, extend) {
    const indexed = await loadGraphWindowIndexed(ctx, extend);
    if (indexed) return indexed;
    const key = "graph";
    let entry = ctx.walks[key];
    if (!entry) {
      const { branches } = await listBranches(ctx);
      const seeds = [];
      for (const b of branches) {
        const sha = await refTip(ctx, b.ref);
        if (sha) seeds.push(sha);
      }
      entry = ctx.walks[key] = { state: { visited: new Set(), frontier: seeds.slice(), commits: [] } };
    }
    const decor = await loadGraphDecorations(ctx);
    if (extend || entry.state.commits.length === 0) await graphWalkStep(ctx, entry.state, GRAPH_WINDOW);
    return { commits: entry.state.commits.slice(), truncated: entry.state.frontier.length > 0, decor };
  }

  // graphWalkStep advances a graph walk by up to `windowCap` more commits. Unlike
  // the plain BFS walkStep, it always expands the NEWEST unvisited frontier commit
  // (a time-ordered priority pop), so the emitted sequence is a global newest-
  // first interleave across branches — the order a lane-assigning graph renderer
  // consumes. Fetches the popped commit, appends it, and merges its parents into
  // the frontier. Bounded concurrency is unnecessary here (one pop at a time keeps
  // the time order exact); objects already in ctx.objects cost no fetch.
  async function graphWalkStep(ctx, state, windowCap) {
    const start = state.commits.length;
    // Resolve author times for the current frontier so the newest can be popped.
    // Times are cached on the walk to avoid re-fetching when the frontier is
    // re-scanned across steps.
    state.times = state.times || new Map();
    const timeOf = async (h) => {
      if (state.times.has(h)) return state.times.get(h);
      const obj = await getObject(ctx, h);
      const t = obj && obj.type === "commit" ? parseCommit(h, obj.body).authorTime : 0;
      state.times.set(h, t);
      return t;
    };
    while (state.frontier.length && state.commits.length - start < windowCap) {
      // Drop already-visited frontier entries.
      state.frontier = state.frontier.filter((h) => h && !state.visited.has(h));
      if (!state.frontier.length) break;
      // Pick the newest frontier commit by author time.
      let best = 0, bestT = -1;
      for (let i = 0; i < state.frontier.length; i++) {
        const t = await timeOf(state.frontier[i]);
        if (t > bestT) { bestT = t; best = i; }
      }
      const h = state.frontier.splice(best, 1)[0];
      if (state.visited.has(h)) continue;
      state.visited.add(h);
      const obj = await getObject(ctx, h);
      if (!obj || obj.type !== "commit") continue;
      const c = parseCommit(h, obj.body);
      state.commits.push(c);
      // Branch attribution for the code-timeline walk: a tip is reached via its
      // own branch; every parent inherits the branch it was first reached from,
      // except the default branch always wins (a commit on main shows "main",
      // not a feature branch that happens to be walked first). Inert for the
      // graph walk (no tipBranch/reachedVia on its state).
      if (state.reachedVia) {
        const tb = state.tipBranch && state.tipBranch[h];
        // A commit that IS a branch tip is attributed to that branch (the default
        // branch wins over an already-assigned feature attribution); otherwise it
        // keeps the branch it was first reached from.
        const via = (tb === state.defaultBranch ? tb : (state.reachedVia[h] || tb)) || "";
        if (via) state.reachedVia[h] = via;
        const isDefault = via && via === state.defaultBranch;
        for (const p of c.parents) if (!(p in state.reachedVia) || isDefault) state.reachedVia[p] = via;
      }
      for (const p of c.parents) if (!state.visited.has(p)) state.frontier.push(p);
    }
    return state;
  }

  // assignGraphLanes assigns each commit (in the given newest-first order) a lane
  // index and computes the parent edges for an inline-SVG DAG render. It is the
  // standard "parent-following" lane algorithm: a set of active lanes each hold
  // the sha the lane is currently waiting to draw; a commit takes the leftmost
  // lane already waiting for it (else a new lane), then its first parent inherits
  // that lane and any additional parents (a merge) open/claim further lanes.
  // Returns { rows, laneCount } where each row is { commit, lane, parents:
  // [{ sha, lane }], present } — `present` flags a parent that is within the
  // loaded window (an edge is only drawn to a loaded parent; an edge to an
  // unloaded parent is a lane that simply ends). Deterministic and DOM-free.
  function assignGraphLanes(commits) {
    const index = new Map();
    commits.forEach((c, i) => index.set(c.hash, i));
    // lanes[i] = sha this lane is currently waiting to place, or null (free).
    const lanes = [];
    let laneCount = 0;
    const claim = (sha) => {
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === sha) return i;
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === null) { lanes[i] = sha; return i; }
      lanes.push(sha); return lanes.length - 1;
    };
    const rows = [];
    for (const c of commits) {
      // The commit's lane: a lane already waiting for it, else a fresh lane.
      let lane = -1;
      for (let i = 0; i < lanes.length; i++) if (lanes[i] === c.hash) { lane = i; break; }
      if (lane < 0) lane = claim(c.hash);
      // Free the commit's lane before re-assigning to parents so the first parent
      // can inherit exactly this lane (a straight line down the mainline).
      lanes[lane] = null;
      const parents = [];
      c.parents.forEach((p, pi) => {
        const present = index.has(p);
        let pl;
        if (pi === 0) { lanes[lane] = p; pl = lane; }
        else pl = claim(p);
        parents.push({ sha: p, lane: pl, present });
      });
      rows.push({ commit: c, lane, parents });
      for (let i = lanes.length; i > 0; i--) if (lanes[i - 1] !== null) { laneCount = Math.max(laneCount, i); break; }
      laneCount = Math.max(laneCount, lane + 1);
      for (const pr of parents) laneCount = Math.max(laneCount, pr.lane + 1);
    }
    return { rows, laneCount };
  }

  function newContext(base) {
    // treeExpanded is the directory-expansion Set shared by the content-pane
    // file tree and the code-context sidebar tree, so expanding in one is
    // reflected in the other on the next render (both key by full repo path).
    // walks caches resumable history walks per ext/branch (see walkStateFor) so
    // "Load more" and deep lookups accumulate across a session, not per view.
    return { base, objects: new Map(), treeExpanded: new Set(), walks: {} };
  }

  // ---- Trees, paths, branches (DOM-free, testable) ----

  // parseTree parses a git tree object body: repeated entries of
  // "<octal mode> <name>\0<20 raw sha bytes>". Mode is ASCII octal (no
  // padding), name is raw UTF-8 bytes, sha is 20 raw bytes rendered hex.
  // Type is derived from the mode: 40000 = tree, 160000 = gitlink, else blob.
  function parseTree(body) {
    const entries = [];
    const dec = new TextDecoder();
    let i = 0;
    while (i < body.length) {
      let sp = i;
      while (sp < body.length && body[sp] !== 0x20) sp++;
      if (sp >= body.length) break;
      const mode = dec.decode(body.subarray(i, sp));
      let nul = sp + 1;
      while (nul < body.length && body[nul] !== 0) nul++;
      const name = dec.decode(body.subarray(sp + 1, nul));
      const shaBytes = body.subarray(nul + 1, nul + 21);
      if (shaBytes.length < 20) break;
      let sha = "";
      for (let b = 0; b < 20; b++) sha += shaBytes[b].toString(16).padStart(2, "0");
      const type = mode === "40000" ? "tree" : (mode === "160000" ? "commit" : "blob");
      entries.push({ mode, name, sha, type });
      i = nul + 21;
    }
    return entries;
  }

  // getTree fetches a tree object and returns its parsed entries, or null.
  async function getTree(ctx, sha) {
    const obj = await getObject(ctx, sha);
    if (!obj || obj.type !== "tree") return null;
    return parseTree(obj.body);
  }

  // resolvePath walks tree entries level by level from a commit's root tree
  // down a "/"-separated path. Returns { type:'tree'|'blob'|'commit', sha,
  // mode } for the resolved entry, the root tree for an empty path, or null
  // when any segment is missing or descends through a non-tree.
  async function resolvePath(ctx, commitSha, path) {
    const obj = await getObject(ctx, commitSha);
    if (!obj || obj.type !== "commit") return null;
    const commit = parseCommit(commitSha, obj.body);
    let cur = { type: "tree", sha: commit.tree, mode: "40000" };
    const parts = (path || "").split("/").filter(Boolean);
    for (const part of parts) {
      if (cur.type !== "tree") return null;
      const entries = await getTree(ctx, cur.sha);
      if (!entries) return null;
      const match = entries.find((e) => e.name === part);
      if (!match) return null;
      cur = { type: match.type, sha: match.sha, mode: match.mode };
    }
    return cur;
  }

  // headBranchName strips refs/heads/ from the HEAD symref target.
  function headBranchName(head) {
    return head && head.branch ? head.branch.replace(/^refs\/heads\//, "") : null;
  }

  // listBranches enumerates branches: refs/heads/* from the manifest when
  // present, else the well-known extension branches plus HEAD's branch. The
  // default branch is HEAD's symref target.
  async function listBranches(ctx) {
    const head = await resolveHead(ctx.base);
    const defaultBranch = headBranchName(head);
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const names = new Set();
    if (ctx.manifest) {
      for (const ref of Object.keys(ctx.manifest)) {
        if (ref.startsWith("refs/heads/")) names.add(ref.slice(11));
      }
    } else {
      for (const ref of Object.values(EXT_BRANCHES)) names.add(ref.slice(11));
    }
    if (defaultBranch) names.add(defaultBranch);
    const branches = Array.from(names).sort().map((n) => ({
      name: n, ref: "refs/heads/" + n, isDefault: n === defaultBranch,
    }));
    return { defaultBranch, branches };
  }

  // peelTag resolves a ref sha to its underlying commit: a lightweight tag (or a
  // branch) points straight at a commit, so the sha is returned as-is; an
  // annotated tag points at a tag object whose body carries an `object <sha>`
  // line naming the tagged object, which is followed (chasing nested tag objects)
  // until a commit is reached. Returns { sha, commit, tagger, message } — commit
  // is the peeled commit sha (null when unreachable), tagger/message come from
  // the annotated tag object when present (empty for a lightweight tag).
  async function peelTag(ctx, sha) {
    let cur = sha, tagger = "", message = "", signed = false, guard = 0;
    while (cur && guard++ < 8) {
      const obj = await getObject(ctx, cur);
      if (!obj) return { sha, commit: null, tagger, message, signed };
      if (obj.type === "commit") return { sha, commit: cur, tagger, message, signed };
      if (obj.type !== "tag") return { sha, commit: null, tagger, message, signed };
      const text = new TextDecoder().decode(obj.body);
      const split = text.indexOf("\n\n");
      const header = split < 0 ? text : text.slice(0, split);
      if (split >= 0 && !message) {
        const raw = text.slice(split + 2).replace(/\r/g, "");
        const stripped = stripSignatureBlock(raw);
        signed = stripped.signed;
        message = stripped.text.trim();
      }
      let next = "";
      for (const line of header.split("\n")) {
        if (line.startsWith("object ")) next = line.slice(7).trim();
        else if (line.startsWith("tagger ") && !tagger) tagger = line.slice(7).trim();
      }
      cur = next;
    }
    return { sha, commit: null, tagger, message, signed };
  }

  // stripSignatureBlock removes a trailing PGP/SSH signature block from an
  // annotated tag (or signed commit) message body. Git appends the ASCII-armored
  // signature after the annotation, e.g. "-----BEGIN PGP SIGNATURE----- … -----END
  // PGP SIGNATURE-----"; it is noise for a reader. Returns { text, signed } so a
  // small "signed" note can stand in for the removed block.
  function stripSignatureBlock(body) {
    const re = /-----BEGIN (?:PGP|SSH) SIGNATURE-----[\s\S]*?-----END (?:PGP|SSH) SIGNATURE-----\s*/g;
    const signed = re.test(body);
    return { text: signed ? body.replace(re, "").replace(/\s+$/, "") : body, signed };
  }

  // listTags enumerates the bucket's tags from the refs manifest (refs/tags/*),
  // mirroring listBranches. Each entry carries the tag name and its raw ref sha
  // (a lightweight tag's commit, or an annotated tag's tag object — peeled to a
  // commit on demand by the tag detail view). Empty when the manifest is absent
  // or carries no tags (tags weren't pushed).
  async function listTags(ctx) {
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const tags = [];
    if (ctx.manifest) {
      for (const ref of Object.keys(ctx.manifest)) {
        if (!ref.startsWith("refs/tags/")) continue;
        const sha = ctx.manifest[ref];
        if (!/^[0-9a-f]{40}$/.test(sha || "")) continue;
        tags.push({ name: ref.slice(10), ref, sha });
      }
    }
    tags.sort(compareTagsDesc);
    return tags;
  }

  // tagVersionKey extracts a tag name's dotted version as a number array,
  // v-prefix aware ("v1.2.0" -> [1,2,0], "1.0-light" -> [1,0]); null when the
  // name carries no leading numeric version. A trailing pre-release suffix
  // (e.g. "-rc1") is ignored for ordering — good enough for descending display.
  function tagVersionKey(name) {
    const m = /^v?(\d+(?:\.\d+)*)/.exec(String(name || ""));
    return m ? m[1].split(".").map(Number) : null;
  }

  // compareTagsDesc orders tags version-aware, highest version first (semver-style
  // numeric compare, GitLab-like). Non-version tags fall after all versioned ones
  // and sort by name descending (a deterministic, listing-only fallback that needs
  // no per-tag object fetch). A shorter version prefix that is otherwise equal is
  // the lower version ("1.0" < "1.0.1"). When two names share the same numeric
  // version, a plain release outranks one carrying a suffix (semver: "1.0" >
  // "1.0-light"/"1.0-rc1"), then shorter-name-first as a deterministic tiebreak.
  function compareTagsDesc(a, b) {
    const va = tagVersionKey(a.name), vb = tagVersionKey(b.name);
    if (va && vb) {
      for (let i = 0; i < Math.max(va.length, vb.length); i++) {
        const d = (vb[i] || 0) - (va[i] || 0);
        if (d) return d;
      }
      const sa = tagVersionSuffix(a.name), sb = tagVersionSuffix(b.name);
      if (!sa && sb) return -1;
      if (sa && !sb) return 1;
      return a.name.localeCompare(b.name);
    }
    if (va) return -1;
    if (vb) return 1;
    return b.name.localeCompare(a.name);
  }

  // tagVersionSuffix returns the trailing text after a tag name's leading numeric
  // version ("v1.0-light" -> "-light", "v1.0" -> ""), so a plain release can be
  // ranked above a pre-release/variant of the same version.
  function tagVersionSuffix(name) {
    return String(name || "").replace(/^v?\d+(?:\.\d+)*/, "");
  }

  // resolveCompareRef resolves a compare side (a branch or tag name) to a commit
  // sha in this bucket. It prefers a branch of that name (refs/heads/<name>),
  // falls back to a tag (refs/tags/<name>, peeled through annotated tag objects
  // to a commit), and returns { sha, kind:'branch'|'tag', name } or null when the
  // name matches neither ref (or its object is unreachable).
  async function resolveCompareRef(ctx, name) {
    if (!name) return null;
    const branchTip = await refTip(ctx, "refs/heads/" + name);
    if (branchTip) return { sha: branchTip, kind: "branch", name };
    const tagSha = await refTip(ctx, "refs/tags/" + name);
    if (tagSha) {
      const peeled = await peelTag(ctx, tagSha);
      if (peeled.commit) return { sha: peeled.commit, kind: "tag", name };
    }
    return null;
  }

  // ---- Markdown (GFM subset; parse step is DOM-free and testable) ----

  // Void/self-closing HTML tags: they carry no closing tag, so line- and
  // inline-level detection treats them as standalone elements, never wrappers.
  const VOID_HTML = new Set(["br", "hr", "img", "source", "col", "input", "wbr", "area"]);

  // Inline-level HTML tags: a line leading with one of these continues the
  // current paragraph (CommonMark inline flow) instead of breaking it, so a
  // <br />-separated link row renders on one line like GitHub does.
  const INLINE_HTML = new Set(["a", "b", "i", "em", "strong", "code", "kbd", "sup", "sub", "span", "img", "br", "del", "s", "strike", "mark", "small", "picture", "input"]);

  // matchDelim returns the index of the delimiter that closes the one at
  // `start`, counting nesting, or -1. Lets [![alt](img)](link) badges parse:
  // the outer ] and ) are matched past the inner image's brackets/parens.
  function matchDelim(text, start, open, close) {
    let depth = 0;
    for (let i = start; i < text.length; i++) {
      if (text[i] === open) depth++;
      else if (text[i] === close) { depth--; if (depth === 0) return i; }
    }
    return -1;
  }

  // parseInline tokenizes a line into inline spans: code (`x`), images
  // (![a](u)), links ([t](u)), autolinks (<url> and bare https URLs), bold
  // (**x**), strikethrough (~~x~~), italic (*x* / _x_), a whitelisted raw-HTML
  // subset (<b>…</b>, <kbd>…, <sup>…, <br>, …) captured verbatim for the
  // sanitizer, and plain text. Markdown-native spans render through safe DOM
  // builders; only rawhtml spans reach the sanitizer.
  function parseInline(text) {
    const spans = [];
    let buf = "";
    const flush = () => { if (buf) { spans.push({ type: "text", value: buf }); buf = ""; } };
    for (let i = 0; i < text.length;) {
      const ch = text[i];
      if (ch === "\\" && /[!-/:-@[-`{-~]/.test(text[i + 1] || "")) { buf += text[i + 1]; i += 2; continue; }
      if (ch === "`") {
        const end = text.indexOf("`", i + 1);
        if (end > i) { flush(); spans.push({ type: "code", value: text.slice(i + 1, end) }); i = end + 1; continue; }
      }
      if (ch === "!" && text[i + 1] === "[") {
        const close = matchDelim(text, i + 1, "[", "]");
        if (close > i && text[close + 1] === "(") {
          const paren = matchDelim(text, close + 1, "(", ")");
          if (paren > close) { flush(); spans.push({ type: "image", alt: text.slice(i + 2, close), src: text.slice(close + 2, paren).trim() }); i = paren + 1; continue; }
        }
      }
      if (ch === "[") {
        const close = matchDelim(text, i, "[", "]");
        if (close > i && text[close + 1] === "(") {
          const paren = matchDelim(text, close + 1, "(", ")");
          if (paren > close) {
            flush();
            spans.push({ type: "link", spans: parseInline(text.slice(i + 1, close)), href: text.slice(close + 2, paren).trim() });
            i = paren + 1; continue;
          }
        }
      }
      if (ch === "<") {
        if (text.startsWith("<!--", i)) {
          flush();
          const end = text.indexOf("-->", i + 4);
          i = end >= 0 ? end + 3 : text.length;
          continue;
        }
        const auto = /^<((?:https?:\/\/|mailto:)[^>\s]+)>/.exec(text.slice(i));
        if (auto) { flush(); const u = auto[1]; spans.push({ type: "link", spans: [{ type: "text", value: u }], href: u }); i += auto[0].length; continue; }
        const tagM = /^<(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s[^<>]*)?)(\/?)>/.exec(text.slice(i));
        if (tagM) {
          const tag = tagM[2].toLowerCase(), whole = tagM[0];
          if (tagM[1] === "/" || tagM[4] === "/" || VOID_HTML.has(tag)) { flush(); spans.push({ type: "rawhtml", value: whole }); i += whole.length; continue; }
          const close = "</" + tag + ">";
          const end = text.indexOf(close, i + whole.length);
          if (end >= 0) { flush(); spans.push({ type: "rawhtml", value: text.slice(i, end + close.length) }); i = end + close.length; continue; }
          flush(); spans.push({ type: "rawhtml", value: whole }); i += whole.length; continue;
        }
      }
      if (ch === "*" && text[i + 1] === "*") {
        const end = text.indexOf("**", i + 2);
        if (end > i) { flush(); spans.push({ type: "strong", spans: parseInline(text.slice(i + 2, end)) }); i = end + 2; continue; }
      }
      if (ch === "~" && text[i + 1] === "~") {
        const end = text.indexOf("~~", i + 2);
        if (end > i) { flush(); spans.push({ type: "strike", spans: parseInline(text.slice(i + 2, end)) }); i = end + 2; continue; }
      }
      if ((ch === "*" || ch === "_") && text[i + 1] !== ch && text[i + 1] !== undefined && text[i + 1] !== " ") {
        const end = text.indexOf(ch, i + 1);
        if (end > i) { flush(); spans.push({ type: "em", spans: parseInline(text.slice(i + 1, end)) }); i = end + 1; continue; }
      }
      if (ch === "h" && /^https?:\/\//.test(text.slice(i))) {
        const m = /^https?:\/\/[^\s<>)]+/.exec(text.slice(i));
        if (m) { const url = m[0].replace(/[.,;:!?]+$/, ""); flush(); spans.push({ type: "link", spans: [{ type: "text", value: url }], href: url }); i += url.length; continue; }
      }
      buf += ch; i++;
    }
    flush();
    return spans;
  }

  // indentWidth counts a line's leading whitespace (a tab counts as two), used
  // to decide list nesting.
  function indentWidth(line) {
    let n = 0;
    for (const c of line) { if (c === " ") n++; else if (c === "\t") n += 2; else break; }
    return n;
  }

  // splitTableRow splits a Markdown table row into trimmed cell strings,
  // dropping the optional leading/trailing pipes.
  function splitTableRow(line) {
    let s = line.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|")) s = s.slice(0, -1);
    return s.split("|").map((c) => c.trim());
  }

  // isTableSeparator recognizes a GFM table delimiter row (---, :--, :-:, --:).
  function isTableSeparator(line) {
    if (!/\|/.test(line) && !/-/.test(line)) return false;
    const cells = splitTableRow(line);
    return cells.length > 0 && cells.every((c) => /^:?-+:?$/.test(c));
  }

  // cellAlign maps a delimiter cell to its column alignment.
  function cellAlign(cell) {
    const l = cell.startsWith(":"), r = cell.endsWith(":");
    return l && r ? "center" : r ? "right" : l ? "left" : "";
  }

  // parseList consumes an indentation-delimited list starting at `start` and
  // returns { block, next }. Items carry parsed inline spans, an optional task
  // checkbox state, and nested child lists (deeper-indented items attach to the
  // preceding item). Blank lines end the list (tight lists only).
  function parseList(lines, start) {
    const base = indentWidth(lines[start]);
    const ordered = /^\s*\d+[.)]\s+/.test(lines[start]);
    const items = [];
    let i = start;
    while (i < lines.length) {
      const line = lines[i];
      if (line.trim() === "") break;
      const m = /^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$/.exec(line);
      if (!m) break;
      const ind = indentWidth(line);
      if (ind < base) break;
      if (ind > base) {
        const sub = parseList(lines, i);
        if (items.length) items[items.length - 1].children.push(sub.block);
        i = sub.next;
        continue;
      }
      let content = m[2], task = null;
      const tm = /^\[([ xX])\]\s+(.*)$/.exec(content);
      if (tm) { task = tm[1].toLowerCase() === "x"; content = tm[2]; }
      items.push({ spans: parseInline(content), task, children: [] });
      i++;
    }
    return { block: { type: "list", ordered, items }, next: i };
  }

  // isThematicBreak recognizes a *** / --- / ___ rule line (3+ markers, spaces
  // allowed between). A `---` directly under paragraph text is a setext h2
  // instead; parseMarkdown checks that first.
  function isThematicBreak(line) {
    return /^ {0,3}([-*_])( *\1){2,} *$/.test(line);
  }

  // breaksParagraph reports whether a trimmed `<`-leading line ends the
  // current paragraph: closing tags and block-level tags do; a line leading
  // with an inline-level tag joins the paragraph and flows through
  // parseInline's rawhtml handling (CommonMark inline flow).
  function breaksParagraph(t) {
    const m = /^<(\/?)([a-zA-Z][\w-]*)/.exec(t);
    if (!m) return false;
    return m[1] === "/" || !INLINE_HTML.has(m[2].toLowerCase());
  }

  // parseMarkdown parses text into a block list: heading (ATX + setext),
  // paragraph, fenced code, list (nested, task-aware), blockquote (recursive),
  // table, thematic break, and raw HTML (htmlopen/htmlclose wrapper markers
  // and self-contained html lines). Markdown-native blocks are plain data the
  // DOM renderer turns into safe nodes; html blocks carry verbatim source for
  // the sanitizer.
  function parseMarkdown(text) {
    const lines = (text || "").replace(/\r/g, "").split("\n");
    const blocks = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      if (/^\s*```/.test(line)) {
        const lang = line.trim().slice(3).trim();
        const body = [];
        i++;
        while (i < lines.length && !/^\s*```/.test(lines[i])) { body.push(lines[i]); i++; }
        i++;
        blocks.push({ type: "code", lang, text: body.join("\n") });
        continue;
      }
      const t = line.trim();
      const closeM = /^<\/([a-zA-Z][\w-]*)\s*>$/.exec(t);
      if (closeM) { blocks.push({ type: "htmlclose", tag: closeM[1].toLowerCase() }); i++; continue; }
      const openM = /^<([a-zA-Z][\w-]*)((?:\s[^<>]*)?)>$/.exec(t);
      if (openM && !/\/$/.test(openM[2]) && !VOID_HTML.has(openM[1].toLowerCase())) {
        blocks.push({ type: "htmlopen", tag: openM[1].toLowerCase(), open: t }); i++; continue;
      }
      if (/^<[a-zA-Z][\w-]*(\s[^<>]*)?\/?>/.test(t)) { blocks.push({ type: "html", raw: t }); i++; continue; }
      const h = /^(#{1,6})\s+(.*)$/.exec(line);
      if (h) { blocks.push({ type: "heading", level: h[1].length, spans: parseInline(h[2].replace(/\s+#+\s*$/, "").trim()) }); i++; continue; }
      if (/^\s*>/.test(line)) {
        const buf = [];
        while (i < lines.length && /^\s*>/.test(lines[i])) { buf.push(lines[i].replace(/^\s*>\s?/, "")); i++; }
        blocks.push({ type: "blockquote", blocks: parseMarkdown(buf.join("\n")) });
        continue;
      }
      if (line.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
        const headers = splitTableRow(line).map(parseInline);
        const aligns = splitTableRow(lines[i + 1]).map(cellAlign);
        i += 2;
        const rows = [];
        while (i < lines.length && lines[i].trim() !== "" && lines[i].includes("|")) { rows.push(splitTableRow(lines[i]).map(parseInline)); i++; }
        blocks.push({ type: "table", headers, aligns, rows });
        continue;
      }
      if (isThematicBreak(line)) { blocks.push({ type: "thematic" }); i++; continue; }
      if (/^\s*(?:[-*+]|\d+[.)])\s+/.test(line)) {
        const lst = parseList(lines, i);
        blocks.push(lst.block); i = lst.next;
        continue;
      }
      if (line.trim() === "") { i++; continue; }
      const para = [];
      let setext = 0;
      while (i < lines.length && lines[i].trim() !== "" && !/^\s*```/.test(lines[i]) &&
             !/^#{1,6}\s+/.test(lines[i]) && !/^\s*(?:[-*+]|\d+[.)])\s+/.test(lines[i]) &&
             !/^\s*>/.test(lines[i]) && !breaksParagraph(lines[i].trim())) {
        const su = para.length ? /^ {0,3}(=+|-+) *$/.exec(lines[i]) : null;
        if (su) { setext = su[1][0] === "=" ? 1 : 2; i++; break; }
        if (isThematicBreak(lines[i])) break;
        para.push(lines[i]); i++;
      }
      const spans = parseInline(para.join("\n"));
      if (setext) blocks.push({ type: "heading", level: setext, spans });
      else if (spans.length) blocks.push({ type: "paragraph", spans });
    }
    return blocks;
  }

  // ---- Diff engine (DOM-free, testable) ----

  const MAX_DIFF_LINES = 5000;
  const DIFF_BLOB_CAP = 1048576;
  // DIFF_TREE_SCAN_CAP bounds diffTrees' recursion: it stops descending once this
  // many changed paths are collected, so a huge commit does not fetch every
  // changed subtree before the display cap (DIFF_FILE_CAP=100) applies. The margin
  // over the display cap is enough to report "over 100 files changed"; commits
  // under the cap are unaffected (out.truncated stays false).
  const DIFF_TREE_SCAN_CAP = 120;

  // splitLines splits text into lines, dropping the trailing empty element a
  // final newline produces so line counts match git's (a blob ending in "\n"
  // is N lines, not N+1). An empty string is zero lines.
  function splitLines(text) {
    if (text === "") return [];
    const lines = text.split("\n");
    if (lines.length && lines[lines.length - 1] === "") lines.pop();
    return lines;
  }

  // diffLines runs a Myers O(ND) diff over two texts and returns an ordered
  // edit script of { op: 'eq'|'add'|'del', line }. Returns null when the pair
  // is too large (> MAX_DIFF_LINES total), so callers fall back to a plain
  // "too large to diff" notice rather than an O(N*D) blow-up.
  function diffLines(aText, bText) {
    const a = splitLines(aText);
    const b = splitLines(bText);
    if (a.length + b.length > MAX_DIFF_LINES) return null;
    const N = a.length, M = b.length;
    const max = N + M;
    const offset = max;
    const v = new Array(2 * max + 1).fill(0);
    const trace = [];
    let done = false;
    for (let d = 0; d <= max && !done; d++) {
      trace.push(v.slice());
      for (let k = -d; k <= d; k += 2) {
        let x;
        if (k === -d || (k !== d && v[offset + k - 1] < v[offset + k + 1])) x = v[offset + k + 1];
        else x = v[offset + k - 1] + 1;
        let y = x - k;
        while (x < N && y < M && a[x] === b[y]) { x++; y++; }
        v[offset + k] = x;
        if (x >= N && y >= M) { done = true; break; }
      }
    }
    const ops = [];
    let x = N, y = M;
    for (let d = trace.length - 1; d >= 0; d--) {
      const vv = trace[d];
      const k = x - y;
      let prevK;
      if (k === -d || (k !== d && vv[offset + k - 1] < vv[offset + k + 1])) prevK = k + 1;
      else prevK = k - 1;
      const prevX = vv[offset + prevK];
      const prevY = prevX - prevK;
      while (x > prevX && y > prevY) { ops.push({ op: "eq", line: a[x - 1] }); x--; y--; }
      if (d > 0) {
        if (x === prevX) { ops.push({ op: "add", line: b[y - 1] }); y--; }
        else { ops.push({ op: "del", line: a[x - 1] }); x--; }
      }
    }
    ops.reverse();
    return ops;
  }

  // buildHunks groups an edit script into unified-diff hunks with `context`
  // (default 3) lines of surrounding context, merging change regions separated
  // by <= 2*context equal lines. Each hunk carries @@ header numbers and its
  // annotated lines ({ op, line, oldN, newN }).
  function buildHunks(ops, context) {
    context = context == null ? 3 : context;
    let oldN = 0, newN = 0;
    const ann = [];
    for (const o of ops) {
      if (o.op === "eq") { oldN++; newN++; ann.push({ op: "eq", line: o.line, oldN, newN }); }
      else if (o.op === "del") { oldN++; ann.push({ op: "del", line: o.line, oldN, newN: null }); }
      else { newN++; ann.push({ op: "add", line: o.line, oldN: null, newN }); }
    }
    const n = ann.length;
    const isCh = (idx) => ann[idx].op !== "eq";
    const hunks = [];
    let i = 0, prevStop = -1;
    while (i < n) {
      if (!isCh(i)) { i++; continue; }
      let regionEnd = i;
      let j = i + 1;
      while (j < n) {
        if (isCh(j)) { regionEnd = j; j++; continue; }
        let k = j;
        while (k < n && !isCh(k)) k++;
        if (k < n && (k - j) <= 2 * context) { j = k; continue; }
        break;
      }
      const start = Math.max(0, i - context);
      const stop = Math.min(n - 1, regionEnd + context);
      const lines = ann.slice(start, stop + 1);
      let oldStart = 0, oldCount = 0, newStart = 0, newCount = 0;
      for (const l of lines) {
        if (l.op !== "add") { oldCount++; if (!oldStart) oldStart = l.oldN; }
        if (l.op !== "del") { newCount++; if (!newStart) newStart = l.newN; }
      }
      const skipped = ann.slice(prevStop + 1, start);
      hunks.push({ oldStart: oldCount ? oldStart : 0, oldCount, newStart: newCount ? newStart : 0, newCount, lines, skipped });
      prevStop = stop;
      i = stop + 1;
    }
    return hunks;
  }

  // intraLine computes a char-level word-diff split for one paired del/add line:
  // the common prefix and suffix (shared by both sides by construction) and the
  // differing middle of each side. Pure presentation over diffLines output, so a
  // renderer can wrap the changed middle in a word-level mark. Returns null when
  // the lines are identical (nothing to mark) or either exceeds 500 chars (skip
  // the pair, too costly and too noisy to be useful).
  function intraLine(delLine, addLine) {
    const a = delLine == null ? "" : String(delLine);
    const b = addLine == null ? "" : String(addLine);
    if (a === b) return null;
    if (a.length > 500 || b.length > 500) return null;
    const max = Math.min(a.length, b.length);
    let p = 0;
    while (p < max && a[p] === b[p]) p++;
    let s = 0;
    while (s < max - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
    return {
      prefix: a.slice(0, p),
      delMid: a.slice(p, a.length - s),
      addMid: b.slice(p, b.length - s),
      suffix: a.slice(a.length - s),
    };
  }

  // diffTrees recursively compares two git trees (shaA / shaB, either may be
  // null for the empty tree) and returns changed paths as { path, status:
  // 'added'|'deleted'|'modified', shaA, shaB, modeA, modeB }. No rename
  // detection: a moved file surfaces as a delete plus an add. A directory
  // replaced by a file (or vice versa) expands to deletes of the old subtree
  // plus the add. The result is sorted by path.
  async function diffTrees(ctx, shaA, shaB, prefix) {
    const out = [];
    const state = { truncated: false };
    await diffCollect(ctx, shaA, shaB, prefix || "", out, state);
    out.sort((x, y) => (x.path < y.path ? -1 : x.path > y.path ? 1 : 0));
    out.truncated = state.truncated;
    return out;
  }

  // diffCollect is diffTrees' bounded recursive worker: it appends changed paths
  // to `out` and flips state.truncated (halting further descent and iteration)
  // once DIFF_TREE_SCAN_CAP paths are collected, so the tree fan-out is bounded.
  async function diffCollect(ctx, shaA, shaB, prefix, out, state) {
    if (state.truncated) return;
    const entriesA = shaA ? (await getTree(ctx, shaA)) || [] : [];
    const entriesB = shaB ? (await getTree(ctx, shaB)) || [] : [];
    const mapA = new Map(entriesA.map((e) => [e.name, e]));
    const mapB = new Map(entriesB.map((e) => [e.name, e]));
    const names = new Set();
    for (const k of mapA.keys()) names.add(k);
    for (const k of mapB.keys()) names.add(k);
    const push = (rec) => { out.push(rec); if (out.length >= DIFF_TREE_SCAN_CAP) state.truncated = true; };
    for (const name of names) {
      if (state.truncated) return;
      const a = mapA.get(name), b = mapB.get(name);
      const path = prefix ? prefix + "/" + name : name;
      const aTree = a && a.type === "tree", bTree = b && b.type === "tree";
      if (a && b) {
        if (a.sha === b.sha && a.mode === b.mode) continue;
        if (aTree && bTree) { await diffCollect(ctx, a.sha, b.sha, path, out, state); }
        else if (aTree) {
          await diffCollect(ctx, a.sha, null, path, out, state);
          if (state.truncated) return;
          push({ path, status: "added", shaA: null, shaB: b.sha, modeA: null, modeB: b.mode });
        } else if (bTree) {
          push({ path, status: "deleted", shaA: a.sha, shaB: null, modeA: a.mode, modeB: null });
          if (state.truncated) return;
          await diffCollect(ctx, null, b.sha, path, out, state);
        } else push({ path, status: "modified", shaA: a.sha, shaB: b.sha, modeA: a.mode, modeB: b.mode });
      } else if (a) {
        if (aTree) await diffCollect(ctx, a.sha, null, path, out, state);
        else push({ path, status: "deleted", shaA: a.sha, shaB: null, modeA: a.mode, modeB: null });
      } else {
        if (bTree) await diffCollect(ctx, null, b.sha, path, out, state);
        else push({ path, status: "added", shaA: null, shaB: b.sha, modeA: null, modeB: b.mode });
      }
    }
  }

  // commitTree returns a commit's root tree sha, or null.
  async function commitTree(ctx, sha) {
    const obj = await getObject(ctx, sha);
    if (!obj || obj.type !== "commit") return null;
    return parseCommit(sha, obj.body).tree;
  }

  // mergeBase walks ancestors of two commits with a shared, bounded budget
  // (cap total visited at `cap`, default WALK_CAP) over the shared object
  // cache, and returns the first common ancestor closest to base, or null when
  // none is reachable within the cap.
  async function mergeBase(ctx, headSha, baseSha, cap) {
    cap = cap || WALK_CAP;
    let visited = 0;
    const headAnc = new Set();
    let frontier = [headSha];
    while (frontier.length && visited < cap) {
      const h = frontier.shift();
      if (headAnc.has(h)) continue;
      headAnc.add(h); visited++;
      const obj = await getObject(ctx, h);
      if (obj && obj.type === "commit") for (const p of parseCommit(h, obj.body).parents) frontier.push(p);
    }
    const seen = new Set();
    frontier = [baseSha];
    while (frontier.length && visited < cap) {
      const h = frontier.shift();
      if (seen.has(h)) continue;
      seen.add(h); visited++;
      if (headAnc.has(h)) return h;
      const obj = await getObject(ctx, h);
      if (obj && obj.type === "commit") for (const p of parseCommit(h, obj.body).parents) frontier.push(p);
    }
    return null;
  }

  // fileDiff fetches the blob pair for one changed entry and produces a diff
  // model: { binary } for NUL-sniffed content, { tooLarge } for blobs over the
  // cap or line-diffs past MAX_DIFF_LINES, else { hunks, adds, dels }. Fetch is
  // lazy: callers invoke this only when a file is expanded.
  async function fileDiff(ctx, entry) {
    const aObj = entry.shaA ? await getObject(ctx, entry.shaA) : null;
    const bObj = entry.shaB ? await getObject(ctx, entry.shaB) : null;
    const aBytes = aObj ? aObj.body : new Uint8Array(0);
    const bBytes = bObj ? bObj.body : new Uint8Array(0);
    if (isBinary(aBytes) || isBinary(bBytes)) return { binary: true };
    if (aBytes.length > DIFF_BLOB_CAP || bBytes.length > DIFF_BLOB_CAP) return { tooLarge: true };
    const ops = diffLines(new TextDecoder().decode(aBytes), new TextDecoder().decode(bBytes));
    if (ops === null) return { tooLarge: true };
    let adds = 0, dels = 0;
    for (const o of ops) { if (o.op === "add") adds++; else if (o.op === "del") dels++; }
    return { hunks: buildHunks(ops, 3), adds, dels };
  }

  // ---- Routing (DOM-free, testable) ----

  // Extension items are commits on well-known data branches, so an item
  // permalink is a workspace-relative gitmsg ref (#commit:<hash>@<branch>).
  // These maps translate between a branch, its extension, and its index tab.
  const COMMIT_VIEW = {
    "gitmsg/social": { ext: "social", tab: "timeline", label: "Post" },
    "gitmsg/pm": { ext: "pm", tab: "issues", label: "Issue" },
    "gitmsg/review": { ext: "review", tab: "prs", label: "Pull request" },
    "gitmsg/release": { ext: "release", tab: "releases", label: "Release" },
    "gitmsg/memo": { ext: "memo", tab: "memos", label: "Memo" },
  };
  const TAB_BRANCH = { timeline: "gitmsg/social", issues: "gitmsg/pm", prs: "gitmsg/review", releases: "gitmsg/release", memos: "gitmsg/memo" };
  const LEGACY_BRANCH = { issue: "gitmsg/pm", pr: "gitmsg/review", release: "gitmsg/release", commit: "" };
  const INDEX_TABS = { timeline: 1, issues: 1, prs: 1, releases: 1, memos: 1, milestones: 1, sprints: 1 };

  // commitRef builds a workspace-relative gitmsg commit ref fragment.
  function commitRef(hash, branch) {
    return "#commit:" + hash + "@" + (branch || "");
  }

  // compareRef builds a compare route fragment (#/compare:<base>...<head>) with
  // each side URL-encoded so branch/tag names carrying "/" or other reserved
  // characters round-trip through parseRoute.
  function compareRef(base, head) {
    return "#/compare:" + encodeURIComponent(base || "") + "..." + encodeURIComponent(head || "");
  }

  // legacyCommit resolves a legacy detail route to its #commit: target and
  // records the canonical fragment so the router can location.replace to it.
  function legacyCommit(hash, branch) {
    const clean = hash.toLowerCase();
    return { type: "commit", hash: clean, branch, canonical: commitRef(clean, branch), legacy: true };
  }

  // parseRoute maps a location.hash fragment to a route descriptor. Two
  // families: friendly/legacy routes start with "/" (tabs and the old
  // #/issue|pr|release|commit/<hash> permalinks, which resolve to a #commit:
  // canonical); everything else is gitmsg ref grammar (<type>:<value>).
  // Fragments carry ":" "@" "/" unencoded, so parsing is positional: split the
  // reference type at the first ":", the hash/path from its branch at the first
  // "@", and (files only) a trailing ":L<n>[-<m>]" line suffix (branch names
  // cannot contain ":", so it delimits cleanly).
  function parseRoute(rawHash) {
    const frag = (rawHash || "").replace(/^#/, "");
    if (frag === "" || frag === "/") return { type: "home" };
    if (frag[0] === "/") {
      // #/compare:<base>...<head> — each side URL-encoded, so no unencoded "/"
      // survives to be mis-split by the tab parser. Parse it off the full
      // fragment before the "/"-split path grammar.
      if (frag.startsWith("/compare:")) {
        const value = frag.slice("/compare:".length);
        const dots = value.indexOf("...");
        const dec = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
        const rawBase = dots < 0 ? value : value.slice(0, dots);
        const rawHead = dots < 0 ? "" : value.slice(dots + 3);
        return { type: "compare", base: dec(rawBase), head: dec(rawHead) };
      }
      const parts = frag.slice(1).split("/");
      const head = parts[0];
      const rest = parts.slice(1).join("/");
      if (head === "" || head === "home") return { type: "home" };
      if (INDEX_TABS[head]) return { type: "index", tab: head };
      if (head === "tree" || head === "code") return { type: "code" };
      if (head === "branches") return { type: "branches" };
      if (head === "graph") return { type: "graph" };
      if (head === "tags") return { type: "tags" };
      if (head === "analytics") return { type: "analytics" };
      if (head === "board") return { type: "board" };
      if (head === "search") return { type: "search", q: rest ? decodeURIComponent(rest) : "" };
      if (head === "lists") return { type: "lists" };
      if (head === "config") return { type: "config" };
      if (rest && head in LEGACY_BRANCH) return legacyCommit(rest, LEGACY_BRANCH[head]);
      return { type: "notfound" };
    }
    // A plain fragment (#quick-start) is an in-page anchor into the home
    // README: route home and scroll to its md- slugged heading after render.
    if (/^[A-Za-z0-9][\w.-]*$/.test(frag)) return { type: "home", anchor: frag };
    const colon = frag.indexOf(":");
    if (colon <= 0) return { type: "notfound" };
    const reftype = frag.slice(0, colon);
    const value = frag.slice(colon + 1);
    if (reftype === "commit") {
      const at = value.indexOf("@");
      const hash = (at < 0 ? value : value.slice(0, at)).toLowerCase();
      const branch = at < 0 ? "" : value.slice(at + 1);
      if (!/^[0-9a-f]{7,40}$/.test(hash)) return { type: "notfound" };
      return { type: "commit", hash, branch };
    }
    if (reftype === "compare") {
      // #/compare:<base>...<head>, each side URL-encoded (branch/tag names carry
      // "/" and other reserved chars). A missing/empty side is left blank so the
      // compare page opens with its pickers for the user to fill.
      const dots = value.indexOf("...");
      const rawBase = dots < 0 ? value : value.slice(0, dots);
      const rawHead = dots < 0 ? "" : value.slice(dots + 3);
      const dec = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
      return { type: "compare", base: dec(rawBase), head: dec(rawHead) };
    }
    if (reftype === "branch") return { type: "branch", name: value };
    if (reftype === "tag") return { type: "tag", name: value };
    if (reftype === "list") return { type: "list", id: value };
    if (reftype === "file") {
      const at = value.indexOf("@");
      const path = at < 0 ? value : value.slice(0, at);
      let branch = at < 0 ? "" : value.slice(at + 1);
      // Branch names cannot contain ":", so anything after it is a suffix:
      // ":L<n>[-<m>]" is a line anchor, any other slug shape a heading anchor.
      let line = null, lineEnd = null, anchor = "";
      const lc = branch.indexOf(":");
      if (lc >= 0) {
        const suffix = branch.slice(lc + 1);
        const lm = /^L(\d+)(?:-(\d+))?$/.exec(suffix);
        if (lm) { line = parseInt(lm[1], 10); if (lm[2]) lineEnd = parseInt(lm[2], 10); }
        else if (/^[A-Za-z0-9][\w.-]*$/.test(suffix)) anchor = suffix;
        branch = branch.slice(0, lc);
      }
      const route = { type: "file", path, branch, line, lineEnd };
      if (anchor) route.anchor = anchor;
      return route;
    }
    return { type: "notfound" };
  }

  // ---- PM aggregation (DOM-free, testable) ----

  // releaseAssets turns a release header's asset fields into structured entries:
  // artifact/checksums/sbom filenames each paired with an external href derived
  // from `artifact-url` (`<artifact-url>/<name>`) when present, else null for
  // git-stored artifacts a static reader cannot link. `signed-by` rides along
  // as plain text.
  function releaseAssets(header) {
    header = header || {};
    const base = (header["artifact-url"] || "").replace(/\/$/, "");
    const href = (name) => (base ? base + "/" + name : null);
    const artifacts = (header.artifacts || "").split(",").map((s) => s.trim()).filter(Boolean).map((name) => ({ name, href: href(name) }));
    const checksums = header.checksums ? { name: header.checksums, href: href(header.checksums) } : null;
    const sbom = header.sbom ? { name: header.sbom, href: href(header.sbom) } : null;
    return { artifactUrl: base, artifacts, checksums, sbom, signedBy: header["signed-by"] || "" };
  }

  // stateCounts tallies items by their header `state` (defaulting to "open"),
  // returning a total and a per-state map for the open/closed/merged filters.
  function stateCounts(items) {
    const byState = {};
    for (const it of items) { const s = (it.header && it.header.state) || "open"; byState[s] = (byState[s] || 0) + 1; }
    return { total: items.length, byState };
  }

  // hashEq compares two short/long commit hashes by prefix (refs may be 7-40
  // hex, item shorts are 12), tolerating either being the longer.
  function hashEq(a, b) { return !!a && !!b && (a === b || a.startsWith(b) || b.startsWith(a)); }

  // THREAD_MAX_DEPTH caps the *visual* indent of a comment thread. The tree is
  // built to full logical depth from the reply-to chain; indentation stops
  // increasing past this level while tree order is preserved (the TUI caps at
  // maxThreadDepth=8 with indentPerLevel=4; the narrower web column caps sooner).
  const THREAD_MAX_DEPTH = 4;

  // threadTime returns a comment's chronological sort key: effectiveTime
  // (origin-time over git author time) so imported conversations order by real
  // upstream time, falling back to git author time for native comments.
  function threadTime(item) { return item.effectiveTime || (item.commit && item.commit.authorTime) || 0; }

  // groupThread builds a comment tree under an item: comments whose `original`
  // references the item are thread members; a member's `reply-to` naming another
  // member nests it under that member to full logical depth; otherwise (reply-to
  // the item itself, or a parent outside this thread) it is a top-level node.
  // Returns top-level nodes { comment, depth, replies:[node…] }; `depth` is the
  // render-indent level, capped at THREAD_MAX_DEPTH. Ordering is chronological by
  // effectiveTime at every level. Reference fields consulted: `original`
  // (membership) then `reply-to` (nesting), per GITSOCIAL 1.3.
  function groupThread(itemShort, comments) {
    const mine = comments.filter((c) => hashEq(refHash(c.header.original), itemShort));
    const byShort = new Map();
    for (const c of mine) byShort.set(c.commit.short, { comment: c, depth: 0, replies: [] });
    const roots = [];
    for (const c of mine) {
      const node = byShort.get(c.commit.short);
      const rt = refHash(c.header["reply-to"]);
      let parent = null;
      if (rt && !hashEq(rt, itemShort)) {
        for (const [short, n] of byShort) if (n !== node && hashEq(short, rt)) { parent = n; break; }
      }
      if (parent) parent.replies.push(node);
      else roots.push(node);
    }
    const cmp = (a, b) => threadTime(a.comment) - threadTime(b.comment);
    const seen = new Set();
    const assignDepth = (nodes, depth) => {
      nodes.sort(cmp);
      for (const n of nodes) {
        if (seen.has(n)) continue;
        seen.add(n);
        n.depth = Math.min(depth, THREAD_MAX_DEPTH);
        assignDepth(n.replies, depth + 1);
      }
    };
    assignDepth(roots, 0);
    return roots;
  }

  // flattenThread walks a thread node tree depth-first in chronological order
  // into a flat [{ comment, depth }] list (depth already indent-capped) — the
  // shape the renderer draws with per-depth rail guides, mirroring the TUI's
  // flat parents+anchor+children list with a per-post Depth.
  function flattenThread(nodes, out) {
    out = out || [];
    for (const n of nodes) { out.push({ comment: n.comment, depth: n.depth }); flattenThread(n.replies, out); }
    return out;
  }

  // TIMELINE_SPECS lists the extension data branches the merged timeline walks,
  // with an optional item-type filter. Mirrors the TUI/library timeline, which
  // reads the social_items_resolved view across data branches (every commit as an
  // item) rather than the social branch alone — so a repo with no posts but many
  // imported issues/PRs still shows a full, interleaved feed. review/release are
  // type-filtered to their reviewable/published units (line-level feedback and
  // draft-tag commits are not standalone timeline entries). Memo is excluded:
  // it is a distinct extension with its own view, not part of the social feed.
  const TIMELINE_SPECS = [
    { ext: "social", branch: "gitmsg/social", type: null },
    { ext: "pm", branch: "gitmsg/pm", type: null },
    { ext: "review", branch: "gitmsg/review", type: "pull-request" },
    { ext: "release", branch: "gitmsg/release", type: "release" },
  ];

  // TIMELINE_WINDOW caps how many merged items the timeline renders and hydrates
  // per autoscroll step. The metadata index carries no bodies, so the cost that
  // must stay bounded is body hydration (one loose-object GET per rendered item):
  // merging is metadata-only and cheap, but rendering N items fetches N bodies.
  // Only this many are rendered+hydrated at a time; the sentinel/observer advances
  // by another window as the reader scrolls. Kept well under WALK_CAP (200) so the
  // first paint is ~50 GETs, not the whole feed.
  const TIMELINE_WINDOW = 50;

  // timelineTyped applies a spec's optional item-type filter to resolved items.
  function timelineTyped(spec, items) {
    if (!spec.type) return items;
    return items.filter((it) => ((it.header && it.header.type) || "") === spec.type);
  }

  // loadTimelineItems builds the merged, newest-first timeline feed across every
  // extension data branch present in the bucket. Each item is tagged with its
  // source ext/branch so the renderer can pick the matching card. Worst-case
  // fan-out: one bounded walk (WALK_CAP each) per data branch (four), sharing the
  // ctx object cache — the same walks the per-tab views already run. Un-windowed
  // (hydrates every item), so it is used only where the full feed is wanted at
  // once (analytics/tests); the interactive route uses loadTimelineWindow.
  async function loadTimelineItems(ctx) {
    const out = [];
    for (const spec of TIMELINE_SPECS) {
      const items = await loadExtItems(ctx, spec.ext);
      for (const it of timelineTyped(spec, items)) {
        it._ext = spec.ext; it._branch = spec.branch;
        out.push(it);
      }
    }
    const code = await resolveCodeItems(ctx, WALK_CAP);
    for (const it of code.items) { it._ext = "code"; out.push(it); }
    out.sort((a, b) => b.effectiveTime - a.effectiveTime);
    return out;
  }

  // resolveExtItems returns `ext`'s resolved items metadata-only (UN-hydrated:
  // no body fetches), walking history just far enough to surface at least `need`
  // items or exhaust the branch — the k-way-merge input the timeline window sorts
  // across branches before hydrating only the slice it renders. An index-seeded
  // walk surfaces the eager set (newest shard + head) with no object fetches, and
  // deepens by pulling older metadata shards on demand; a non-index bucket walks
  // at most ~need commits (min one WALK_CAP window), matching loadExtItemsWindow's
  // bound rather than a full walk. `more` reports that unwalked history remains.
  async function resolveExtItems(ctx, ext, need) {
    const w = await extWalkState(ctx, ext);
    if (!w) return { items: [], more: false };
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      // Progress-guarded loop: each step must add commits or drain an older shard;
      // if a step makes NO forward progress (a malformed shard/manifest, or a walk
      // that can't advance), break rather than spin forever (the timeline would
      // otherwise hang on "Loading…" with no error). See stepGuard.
      let guardN = w.state.commits.length, guardShards = (w.older || []).length, stall = 0;
      while (!extWalkExhausted(w) && w.state.commits.length < need) {
        await stepExtWalk(ctx, w, WALK_CAP);
        const n = w.state.commits.length, s = (w.older || []).length;
        if (n === guardN && s === guardShards) { if (++stall >= 2) break; } else stall = 0;
        guardN = n; guardShards = s;
      }
      return { items: resolveItems(walkedCommits(w.state)), more: !extWalkExhausted(w) };
    });
  }

  // codeCommitItem wraps one plain default-branch commit as a timeline item in
  // the same shape resolveItems produces (commit / header / content /
  // effectiveTime / versions), so the merge, sort, and card dispatch treat it
  // uniformly. A code commit carries no GitMsg header (header is {}), so
  // timelineTyped's type filter, loadInteractionCounts, and edit resolution all
  // no-op for it. Its effectiveTime is the git author time (no origin-time).
  function codeCommitItem(commit, branch) {
    return {
      commit, header: {}, content: commit.content, rawMessage: commit.rawMessage,
      edited: false, editorName: "", author: effectiveAuthor(commit, null),
      effectiveTime: commit.authorTime || 0, versions: [], _code: true, _branch: branch || "",
    };
  }

  // codeMetaCommit converts one code items-index entry into a body-less commit
  // record for the timeline. Unlike a gitmsg metaCommit, a code entry carries a
  // real subject and its attributed branch but NO GitMsg header; the commit card
  // renders subject + author/time/hash only, so `content` is set to the subject
  // (subjectBody's first line) and the record is NOT marked hollow — the timeline
  // never fetches the loose object for a code card, which is exactly the ~50 GETs
  // the index removes. `branch` rides on the record so codeCommitItem links it
  // under the attributed branch route, and `parents` (v5 entries) carries the
  // DAG edges the graph renders. Detail views still fetch the loose object.
  function codeMetaCommit(e) {
    return {
      hash: e.sha, short: String(e.sha || "").slice(0, 12), tree: "",
      parents: Array.isArray(e.parents) ? e.parents : [],
      authorName: e.author || "", authorEmail: e.email || "", authorTime: e.ts || 0,
      content: String(e.subject || ""), rawMessage: String(e.subject || ""),
      subject: String(e.subject || ""), gitmsg: null, refs: [], _branch: e.branch || "",
    };
  }

  // codeWalkState returns the single resumable walk over EVERY pushed code branch
  // (refs/heads/* except the gitmsg/* data branches), seeded at all their tips at
  // once. Matching the TUI's all-branch timeline, this interleaves plain commits
  // from the default branch and every feature branch in one newest-first stream,
  // deduped by hash (a commit reachable from several branches appears once). Null
  // when the repo has no code branches (a data-only bucket). The walk records the
  // branch every tip belongs to (state.tipBranch) so a commit's card links under
  // the branch whose walk reached it first (graphWalkStep records reachedVia).
  async function codeWalkState(ctx) {
    const key = "codeTimeline";
    let w = ctx.walks[key];
    if (w) return w;
    const { branches, defaultBranch } = await listBranches(ctx);
    const code = branches.filter((b) => !b.name.startsWith("gitmsg/"));
    const seeds = [];
    const tipBranch = {};
    for (const b of code) {
      const sha = await refTip(ctx, b.ref);
      if (!sha) continue;
      if (!(sha in tipBranch)) tipBranch[sha] = b.name;
      seeds.push(sha);
    }
    if (!seeds.length) return null;
    w = ctx.walks[key] = { state: { visited: new Set(), frontier: seeds.slice(), commits: [], tipBranch, reachedVia: {}, defaultBranch } };
    return w;
  }

  // resolveCodeItems returns plain commits across all code branches as timeline
  // items (newest-first), advancing just far enough to surface at least `need`
  // items or exhaust the history, in step with the timeline window. When the
  // bucket carries the push-maintained code items index (.gitsocial/site/items/
  // code/, v4), items are sourced from it metadata-only — the newest shard + head
  // cover the first window with a couple of JSON fetches and NO per-commit
  // loose-object GET (a code card renders subject + meta, so no body hydration),
  // and older shards page in on demand exactly like the ext items. Absent (or
  // non-v4) index buckets fall back to the loose graph walk below, which is
  // unchanged so old buckets keep working. Each item is attributed to a real
  // branch (the index carries it; the loose walk derives reachedVia). `more`
  // reports unwalked/older history remains.
  async function resolveCodeItems(ctx, need) {
    const indexed = await resolveCodeItemsIndexed(ctx, need);
    if (indexed) return indexed;
    const w = await codeWalkState(ctx);
    if (!w) return { items: [], more: false };
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await graphWalkStep(ctx, w.state, WALK_CAP);
      while (w.state.frontier.length && w.state.commits.length < need) await graphWalkStep(ctx, w.state, WALK_CAP);
      const items = w.state.commits.filter((c) => !c.gitmsg).map((c) => codeCommitItem(c, w.state.reachedVia[c.hash] || w.state.tipBranch[c.hash] || ""));
      return { items, more: w.state.frontier.length > 0 };
    });
  }

  // resolveCodeItemsIndexed sources the timeline's code commits from the code
  // items index when present, returning null (no index → caller uses the loose
  // walk). It seeds a resumable index walk from the eager set (newest shard +
  // head) once, then drains older metadata shards until at least `need` items are
  // surfaced or every shard is resident — mirroring resolveExtItems' shard paging,
  // but building code items (subject-only metaCommits, no hydration). `more` is
  // true while older shards remain unloaded OR the manifest is an incomplete
  // bootstrap (older history still to be indexed).
  async function resolveCodeItemsIndexed(ctx, need) {
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    return withWalkLock(w, async () => {
      let guard = (w.older || []).length, stall = 0;
      while ((w.older && w.older.length) && w.items.length < need) {
        await loadNextCodeShard(ctx, w);
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      const items = w.items.map((c) => codeCommitItem(c, c._branch || ""));
      const more = (w.older && w.older.length > 0) || !w.complete;
      return { items, more };
    });
  }

  // codeIndexWalkState returns the resumable index-backed code walk (null when the
  // bucket carries no code items index, so the caller falls back to the loose
  // walk). Seeded once per context from the code corpus's eager set (newest shard
  // + head, newest-first), with the remaining older shards pending on w.older for
  // on-demand paging — the same shape seedWalkFromIndex builds for an extension.
  // `hasParents` marks a v5 corpus (entries carry parent shas) so the graph
  // knows the DAG is servable from the index.
  async function codeIndexWalkState(ctx) {
    const key = "codeIndex";
    if (ctx.walks[key] !== undefined) return ctx.walks[key];
    let idx = null;
    try { idx = await loadItemsIndex(ctx, "code"); } catch (e) { if (e && e.forbidden) throw e; }
    if (!idx) { ctx.walks[key] = null; return null; }
    const seen = new Set();
    const items = [];
    for (const e of idx.items) { if (!seen.has(e.sha)) { seen.add(e.sha); items.push(codeMetaCommit(e)); } }
    const w = { items, seen, older: idx.olderShards.slice(), complete: idx.complete, hasParents: idx.version >= 5 };
    ctx.walks[key] = w;
    return w;
  }

  // loadNextCodeShard pulls the next-older pending shard of the code index onto
  // the index-backed code walk (its metadata entries become subject-only
  // codeMetaCommits). Older shards are consumed newest→oldest, deepening the
  // timeline one immutable, browser-cached shard at a time. Mirrors
  // loadNextItemShard for the ext corpora.
  async function loadNextCodeShard(ctx, w) {
    if (!w.older || !w.older.length) return false;
    const keyName = w.older.shift();
    const text = await fetchText(ctx.base, keyName);
    if (!text) return true;
    let doc;
    try { doc = JSON.parse(text); } catch { return true; }
    const entries = (doc && Array.isArray(doc.items)) ? doc.items.slice().reverse() : [];
    for (const e of entries) {
      if (w.seen.has(e.sha)) continue;
      w.seen.add(e.sha);
      w.items.push(codeMetaCommit(e));
    }
    return true;
  }

  // resolveShortShaFromIndex resolves a SHORT commit sha to its full sha using
  // the code items index (every code commit's full sha), draining older shards
  // newest-first until a prefix match is found or every shard is resident.
  // Returns the full sha, or null when the index is absent or the prefix is not
  // present (the caller then falls back to a loose-object walk — the sha may
  // predate the index bootstrap's completeness or sit on a non-indexed object).
  // Ambiguity: mirrors the loose walk's `commits.find(...startsWith)` — the
  // first (newest-first) prefix match wins, no uniqueness check, matching the
  // single-target resolution the walk performs today.
  async function resolveShortShaFromIndex(ctx, short) {
    if (!short) return null;
    const w = await codeIndexWalkState(ctx);
    if (!w) return null;
    return withWalkLock(w, async () => {
      const hit = () => (w.items.find((c) => c.hash.startsWith(short)) || {}).hash || null;
      let found = hit();
      let guard = (w.older || []).length, stall = 0;
      while (!found && w.older && w.older.length) {
        await loadNextCodeShard(ctx, w);
        found = hit();
        const n = (w.older || []).length;
        if (n === guard) { if (++stall >= 2) break; } else stall = 0;
        guard = n;
      }
      return found;
    });
  }

  // loadTimelineWindow is the bounded, autoscroll-paged merged timeline. It grows
  // a `shown` cursor by TIMELINE_WINDOW per extend, merges every data branch's
  // resolved metadata (newest-first, NO body fetches) by effective time, takes the
  // true-newest `shown` across all branches, and hydrates ONLY that rendered slice
  // — the prefix carried over from the previous window is already hydrated
  // (loose-object bodies are immutable-cached and the commit records non-hollow),
  // so an advance fetches ~TIMELINE_WINDOW new bodies, not the whole feed.
  // truncated marks that more items remain (unwalked history OR beyond the cursor),
  // which the scroll sentinel uses to keep advancing. Because each branch surfaces
  // at least `shown` items (or is exhausted), the merged top-`shown` is exact.
  async function loadTimelineWindow(ctx, extend) {
    const tl = ctx.timeline || (ctx.timeline = { shown: 0 });
    tl.shown = extend ? tl.shown + TIMELINE_WINDOW : TIMELINE_WINDOW;
    const need = tl.shown;
    const merged = [];
    let more = false;
    for (const spec of TIMELINE_SPECS) {
      const r = await resolveExtItems(ctx, spec.ext, need);
      if (r.more) more = true;
      for (const it of timelineTyped(spec, r.items)) {
        it._ext = spec.ext; it._branch = spec.branch;
        merged.push(it);
      }
    }
    // Plain code commits from every pushed code branch, interleaved so the feed
    // matches the CLI/TUI all-branch timeline (which lists code commits alongside
    // posts/issues/PRs/releases). One merged, deduped walk across branch tips,
    // advanced in step with the window — not walked to the root up front.
    const code = await resolveCodeItems(ctx, need);
    if (code.more) more = true;
    for (const it of code.items) { it._ext = "code"; merged.push(it); }
    merged.sort((a, b) => b.effectiveTime - a.effectiveTime);
    const windowItems = merged.slice(0, need);
    await hydrateItems(ctx, windowItems);
    return { items: windowItems, truncated: more || merged.length > need };
  }

  // embeddedRefs returns the cross-repo context an item embeds for its own
  // `original`/`reply-to` references: for each reference carrying a repo-url
  // prefix, the matching GitMsg-Ref trailer's origin author/time and quoted
  // excerpt. Same-repo references (no url) are omitted — those resolve locally.
  function embeddedRefs(commit, header) {
    const out = [];
    const refs = (commit && commit.refs) || [];
    for (const key of ["original", "reply-to"]) {
      const val = header[key];
      if (!val) continue;
      const url = refRepoUrl(val);
      if (!url) continue;
      const want = refHash(val);
      const match = refs.find((r) => r.ref === val || (want && hashEq(refHash(r.ref), want)));
      out.push({ key, url, ref: val, author: (match && match.author) || "", email: (match && match.email) || "", time: (match && match.time) || "", type: (match && match.type) || "", quoted: (match && match.quoted) || "" });
    }
    return out;
  }

  // ANCESTOR_CAP bounds the same-repo parent chain a reply's commit permalink
  // resolves and renders above the item (root first); deeper threads truncate
  // to the nearest ancestors, consistent with the other bounded detail walks.
  const ANCESTOR_CAP = 5;

  // refBranch pulls the branch out of a gitmsg commit ref value
  // ("[url]#commit:<hash>@<branch>"), or "" when the ref carries none.
  function refBranch(ref) {
    const s = ref || "";
    const at = s.indexOf("@");
    return at < 0 ? "" : s.slice(at + 1);
  }

  // parentRef returns the same-repo ref an item names as its immediate parent:
  // `reply-to` (the direct parent) over `original` (the thread root), per
  // GITSOCIAL 1.3. Cross-repo refs return null — those stay embeddedRefs
  // territory (their objects are not in this bucket).
  function parentRef(header) {
    for (const key of ["reply-to", "original"]) {
      const val = header && header[key];
      if (val && !refRepoUrl(val)) return val;
    }
    return null;
  }

  // quotedRefFor returns the commit's own GitMsg-Ref trailer entry matching one
  // reference value — the embedded excerpt a permalink falls back to when the
  // referenced parent cannot be resolved in-bucket.
  function quotedRefFor(commit, ref) {
    const want = refHash(ref);
    const refs = (commit && commit.refs) || [];
    return refs.find((r) => r.ref === ref || (want && hashEq(refHash(r.ref), want))) || null;
  }

  // findRefItem resolves one same-repo commit ref to its item and the data
  // branch it lives on. The ref's branch is a hint, not the truth: the CLI
  // writes comment refs with the social branch even when the referenced commit
  // lives on another data branch (a comment on a pm issue carries
  // original=#commit:<issue>@gitmsg/social), so resolution is hash-driven —
  // the hinted ext's walk first, then the remaining data branches, each
  // deepened to DETAIL_WALK_CAP like the thread-source walk (bounded, cached
  // on ctx; absent branches return empty immediately).
  async function findRefItem(ctx, ref, branch) {
    const want = refHash(ref);
    if (!want) return null;
    const hint = refBranch(ref) || branch;
    const order = COMMIT_VIEW[hint] ? [hint] : [];
    for (const br of Object.keys(COMMIT_VIEW)) if (br !== order[0]) order.push(br);
    for (const br of order) {
      const items = await loadExtItemsUpTo(ctx, COMMIT_VIEW[br].ext, DETAIL_WALK_CAP);
      const it = items.find((i) => hashEq(i.commit.short, want));
      if (it) return { item: it, branch: br };
    }
    return null;
  }

  // resolveAncestors resolves an item's same-repo parent chain for the commit
  // permalink view: the immediate parent (reply-to over original), then each
  // ancestor's own parent, up to ANCESTOR_CAP. Returns { chain, missing }:
  // chain is the resolved ancestors root-first, each with the data branch it
  // was found on (for permalinks); missing is the first unresolvable ref
  // (object absent or beyond the walk caps), letting the renderer fall back
  // to the commit's own quoted excerpt. Cycle-safe via a visited set.
  async function resolveAncestors(ctx, item, branch) {
    const chain = [];
    let missing = null;
    const seen = new Set([item.commit.short]);
    let ref = parentRef(item.header);
    while (ref && chain.length < ANCESTOR_CAP) {
      const found = await findRefItem(ctx, ref, branch);
      if (!found) { missing = ref; break; }
      if (seen.has(found.item.commit.short)) break;
      seen.add(found.item.commit.short);
      chain.push(found);
      ref = parentRef(found.item.header);
    }
    chain.reverse();
    return { chain, missing };
  }

  // groupPM splits pm items by type and buckets issues under the milestone /
  // sprint they reference (via the `milestone` / `sprint` header fields, per
  // GITPM 1.3). Milestones carry state+due; sprints carry state+start+end.
  function groupPM(items) {
    const milestones = [], sprints = [], issues = [];
    for (const it of items) {
      const t = (it.header && it.header.type) || "issue";
      if (t === "milestone") milestones.push(it);
      else if (t === "sprint") sprints.push(it);
      else issues.push(it);
    }
    const bucket = (field) => {
      const map = new Map();
      for (const it of issues) {
        const h = refHash(it.header[field]);
        if (!h) continue;
        if (!map.has(h)) map.set(h, []);
        map.get(h).push(it);
      }
      return map;
    };
    return { milestones, sprints, issues, byMilestone: bucket("milestone"), bySprint: bucket("sprint") };
  }

  // itemLabels parses an item's `labels` header (comma-delimited scoped labels
  // like "kind/feature,status/in-progress", GITPM 1.2) into { scope, value }
  // entries; an unscoped label carries an empty scope.
  function itemLabels(header) {
    return ((header && header.labels) || "").split(",").map((s) => s.trim()).filter(Boolean).map((l) => {
      const i = l.indexOf("/");
      return i < 0 ? { scope: "", value: l } : { scope: l.slice(0, i), value: l.slice(i + 1) };
    });
  }

  // PM_BOARD_COLUMNS mirrors the shipped kanban framework board
  // (extensions/pm/framework.go FrameworkKanban / board.go DefaultBoardConfig):
  // Backlog (state:open), In Progress (status:in-progress, WIP 3), Review
  // (status:review, WIP 3), Done (state:closed). A static reader cannot read a
  // repo's custom pm config board, so it mirrors the kanban default the TUI uses
  // when no custom board is defined. Issue state is open|closed per spec; the
  // finer columns come from `status/*` labels actually present on issues.
  const PM_BOARD_COLUMNS = [
    { name: "Backlog", filter: "state:open", wip: 0 },
    { name: "In Progress", filter: "status:in-progress", wip: 3 },
    { name: "Review", filter: "status:review", wip: 3 },
    { name: "Done", filter: "state:closed", wip: 0 },
  ];

  // matchColumnFilter tests an issue header against a board filter expression
  // ("state:open" or "status:in-progress", comma = OR), mirroring board.go
  // matchFilter/matchSingleFilter: a `state:` filter compares the header state
  // (default open); any other key matches a scoped label of that scope/value.
  function matchColumnFilter(header, filter) {
    for (const part of filter.split(",")) {
      const p = part.trim();
      const idx = p.indexOf(":");
      if (idx < 0) continue;
      const key = p.slice(0, idx), value = p.slice(idx + 1);
      if (key === "state") { if (((header && header.state) || "open") === value) return true; continue; }
      for (const l of itemLabels(header)) if (l.scope === key && l.value === value) return true;
    }
    return false;
  }

  // matchIssueColumn returns the best-matching column index for an issue header,
  // preferring a specific label filter (status:x) over a broad state filter
  // (state:open) exactly as board.go matchIssueToColumn does; -1 when nothing
  // matches (the caller drops it into the first column).
  function matchIssueColumn(header, filters) {
    let stateMatch = -1;
    for (let i = 0; i < filters.length; i++) {
      if (!matchColumnFilter(header, filters[i])) continue;
      if (filters[i].indexOf("state:") !== 0) return i;
      if (stateMatch < 0) stateMatch = i;
    }
    return stateMatch;
  }

  // boardColumnsFrom normalizes a resolved-board config (from the push-maintained
  // .gitsocial/site/pm-config.json) into the { name, filter, wip } column shape
  // buildBoard groups against; the kanban default (PM_BOARD_COLUMNS) when the
  // config is absent, malformed, or carries no columns. `wip` is coerced to a
  // number (0 = no limit), matching the ColumnConfig `*int` on the push side.
  function boardColumnsFrom(config) {
    const swimlane = (config && config.defaultSwimlane) || "";
    const cols = config && Array.isArray(config.columns) ? config.columns : null;
    if (!cols || !cols.length) return { name: "Kanban Board", columns: PM_BOARD_COLUMNS, defaultSwimlane: swimlane };
    const columns = cols
      .filter((c) => c && c.name && c.filter)
      .map((c) => ({ name: c.name, filter: c.filter, wip: (typeof c.wip === "number" && c.wip > 0) ? c.wip : 0 }));
    if (!columns.length) return { name: "Kanban Board", columns: PM_BOARD_COLUMNS, defaultSwimlane: swimlane };
    return { name: config.name || "Board", columns, defaultSwimlane: swimlane };
  }

  // buildBoard groups resolved issue items into board columns (client-side
  // regroup of the already-walked pm set, no new fetches). `config` is the
  // repo's resolved board (framework or custom columns); the built-in kanban
  // default is used when it is absent. An unmatched issue falls into the first
  // column, matching board.go. Columns carry their WIP limit and the bucketed
  // issues so a renderer can show counts and over-WIP.
  function buildBoard(issues, config) {
    const board = boardColumnsFrom(config);
    const columns = board.columns.map((c) => ({ name: c.name, filter: c.filter, wip: c.wip || 0, issues: [] }));
    const filters = board.columns.map((c) => c.filter);
    for (const it of issues) {
      let idx = matchIssueColumn(it.header || {}, filters);
      if (idx < 0 || idx >= columns.length) idx = 0;
      columns[idx].issues.push(it);
    }
    return { name: board.name, columns, defaultSwimlane: board.defaultSwimlane || "" };
  }

  // loadSiteConfig fetches the push-maintained resolved PM board config
  // (.gitsocial/site/pm-config.json, { name, columns:[{name,filter,wip}] }) once
  // per context; null when the bucket carries none (a bucket pushed before the
  // artifact, or with no pm config) so the board falls back to the kanban
  // default. No-cache like the other mutable site keys, so a config change is
  // picked up on the next load.
  async function loadSiteConfig(ctx) {
    if (ctx.siteConfig !== undefined) return ctx.siteConfig;
    let cfg = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/pm-config.json");
    if (text) { try { cfg = JSON.parse(text); } catch { cfg = null; } }
    ctx.siteConfig = cfg;
    return cfg;
  }

  // loadSiteCustomization fetches the push-maintained site customization
  // (.gitsocial/site/site-config.json — { title?, accent?, accentDark?,
  // favicon? }) once per context; null when the bucket carries none (a bucket
  // pushed before the artifact, or with no `site` config) so the reader keeps
  // its built-in defaults. No-cache like the other mutable site keys, so a
  // customization change is picked up on the next load.
  async function loadSiteCustomization(ctx) {
    if (ctx.siteCustomization !== undefined) return ctx.siteCustomization;
    let cfg = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/site-config.json");
    if (text) { try { const p = JSON.parse(text); if (p && typeof p === "object") cfg = p; } catch { cfg = null; } }
    ctx.siteCustomization = cfg;
    return cfg;
  }

  // SWIMLANE_FIELDS are the board group-by options, mirroring pm.SwimlaneFields:
  // "" (none), priority, kind, assignees, author. The board's "group by" control
  // cycles/selects among them.
  const SWIMLANE_FIELDS = ["", "priority", "kind", "assignees", "author"];
  // SWIMLANE_LABELS names each field for the group-by control (none for "").
  const SWIMLANE_LABELS = { "": "none", priority: "priority", kind: "kind", assignees: "assignees", author: "author" };
  // Predefined lane orders for priority/kind (mirrors view_board.go
  // getSwimlaneOrder), ungrouped ("") last.
  const SWIMLANE_ORDER = {
    priority: ["critical", "high", "medium", "low", ""],
    kind: ["bug", "feature", "task", "story", "spike", "chore", ""],
  };

  // swimlaneValue extracts an issue's lane value for a group-by field, mirroring
  // view_board.go getSwimlaneValue: assignees → first assignee; author → the
  // display author (origin author over git author); priority/kind → the scoped
  // label value; "" (no field or no value) → the ungrouped lane.
  function swimlaneValue(item, field) {
    const h = item.header || {};
    if (field === "assignees") {
      const a = (h.assignees || "").split(",").map((s) => s.trim()).filter(Boolean);
      return a.length ? a[0] : "";
    }
    if (field === "author") return item.author || (item.commit && (item.commit.authorName || item.commit.authorEmail)) || "";
    if (field === "priority" || field === "kind") {
      for (const l of itemLabels(h)) if (l.scope === field) return l.value;
      return "";
    }
    return "";
  }

  // swimlaneOrder returns the ordered lane values for a field over an issue set,
  // mirroring view_board.go getSwimlaneOrder: priority/kind use their predefined
  // order (filtered to lanes actually present, ungrouped last); other fields use
  // encounter order (alphabetical for stability), ungrouped last. Empty when the
  // field is "".
  function swimlaneOrder(issues, field) {
    if (!field) return [];
    const present = new Set();
    for (const it of issues) present.add(swimlaneValue(it, field));
    if (SWIMLANE_ORDER[field]) {
      const out = SWIMLANE_ORDER[field].filter((v) => present.has(v));
      // Any present value not in the predefined order (a custom label) appended
      // alphabetically before the ungrouped lane.
      const extra = Array.from(present).filter((v) => v && SWIMLANE_ORDER[field].indexOf(v) === -1).sort();
      const hasBlank = out.indexOf("") !== -1;
      const base = out.filter((v) => v !== "").concat(extra);
      return hasBlank ? base.concat([""]) : base;
    }
    const vals = Array.from(present).filter((v) => v).sort();
    if (present.has("")) vals.push("");
    return vals;
  }

  // groupBySwimlane buckets a column's issues by lane value, returning a Map
  // lane → issues in the given lane order (empty lanes included so every column
  // aligns to the same lane rows).
  function groupBySwimlane(issues, field, lanes) {
    const map = new Map();
    for (const lane of lanes) map.set(lane, []);
    for (const it of issues) {
      const v = swimlaneValue(it, field);
      if (!map.has(v)) map.set(v, []);
      map.get(v).push(it);
    }
    return map;
  }

  // swimlaneLabel names a lane for display: the ungrouped lane reads "(none)",
  // any other value verbatim.
  function swimlaneLabel(value) { return value === "" ? "(none)" : value; }

  // pmParentHash returns an issue's immediate-parent short hash per GITPM 1.7: the
  // `parent` ref (a nested child's immediate parent) if present, else the `root`
  // ref (a direct child carries only root). Null for a top-level issue.
  function pmParentHash(header) {
    return refHash((header && header.parent) || "") || refHash((header && header.root) || "") || null;
  }

  // buildIssueHierarchy indexes parent/child relationships over a resolved issue
  // set (GITPM 1.7): childrenOf maps a parent issue's short hash to its direct
  // child items (chronological), byShort indexes issues by short. Cycle-safe by
  // construction — it is a flat parent→children index built in one pass, with no
  // recursive traversal that a cycle could trap (descendant walks that consume it
  // still carry their own visited set).
  function buildIssueHierarchy(issues) {
    const byShort = new Map();
    for (const it of issues) byShort.set(it.commit.short, it);
    const childrenOf = new Map();
    for (const it of issues) {
      const p = pmParentHash(it.header);
      if (!p) continue;
      let key = null;
      for (const s of byShort.keys()) if (hashEq(s, p)) { key = s; break; }
      if (!key || key === it.commit.short) continue;
      if (!childrenOf.has(key)) childrenOf.set(key, []);
      childrenOf.get(key).push(it);
    }
    for (const arr of childrenOf.values()) arr.sort((a, b) => a.effectiveTime - b.effectiveTime);
    return { byShort, childrenOf };
  }

  // pmProgress counts closed vs total over an item set (a milestone's / sprint's
  // members, or a parent's direct children), for the "n closed of m" progress.
  function pmProgress(items) {
    let closed = 0;
    for (const it of items) if (((it.header && it.header.state) || "open") === "closed") closed++;
    return { closed, total: items.length };
  }

  // loadInteractionCounts builds the cross-branch interaction/review tallies the
  // list cards show (TUI card-stat parity), keyed by a target item's short hash.
  // It loads the social and review item sets (body-free; index-backed and
  // exhaustive on an indexed bucket, but bounded to COUNTS_WALK_CAP loose commits
  // when an ext has no index, so a mid-push/stale bucket never stalls the timeline
  // behind an unbounded loose walk) and reduces their relations:
  //   - a social comment (has `original`) increments the target's comment count;
  //     a social reply-to a comment also counts toward the referenced item.
  //   - a social repost / quote increments the original's repost / quote count.
  //   - a review feedback referencing a PR (`pull-request` or `original`)
  //     increments its comment count and, when it carries a `review-state`
  //     verdict, the PR's approved / changes-requested tally (latest verdict per
  //     reviewer wins, mirroring reviewSummary).
  // Returns a Map short → { comments, reposts, quotes, approved, changesRequested }.
  // Counts are computed at read time from the resident corpora, so they are
  // always current (unlike a push-time count frozen into an immutable shard).
  // Cached on ctx. Empty maps degrade gracefully (a bucket with no social/review).
  async function loadInteractionCounts(ctx) {
    if (ctx.interactionCounts) return ctx.interactionCounts;
    const counts = new Map();
    const bump = (short, key) => {
      if (!short) return;
      let r = counts.get(short);
      if (!r) { r = { comments: 0, reposts: 0, quotes: 0, approved: 0, changesRequested: 0 }; counts.set(short, r); }
      r[key]++;
    };
    // anyRefHash extracts the 7-40 hex hash from a ref of ANY type
    // ("[url]#<type>:<hash>[@branch]"), not just commit: — a relation trailer can
    // carry a non-commit ref type ("#unknown:<hash>"), so the commit-only refHash
    // would miss it. Falls back to refHash's commit form.
    const anyRefHash = (ref) => {
      const m = /[#:]([0-9a-f]{7,40})(?:@|$)/.exec(ref || "");
      return m ? m[1].slice(0, 12) : refHash(ref);
    };
    const social = await loadExtItemsForCounts(ctx, "social").catch(() => []);
    for (const it of social) {
      const h = it.header || {};
      const t = it.header && it.header.type;
      const orig = anyRefHash(h.original);
      if (t === "repost") bump(orig, "reposts");
      else if (t === "quote") bump(orig, "quotes");
      else if (t === "comment" || h.original) bump(orig, "comments");
    }
    const review = await loadExtItemsForCounts(ctx, "review").catch(() => []);
    // Latest verdict per (PR short, reviewer email) so a reviewer's re-review does
    // not double-count, mirroring reviewSummary's latest-verdict-per-reviewer rule.
    const verdicts = new Map();
    for (const it of review) {
      const h = it.header || {};
      if ((h.type || "") !== "feedback") continue;
      const pr = anyRefHash(h["pull-request"]) || anyRefHash(h.original);
      if (!pr) continue;
      bump(pr, "comments");
      const rs = h["review-state"];
      if (rs !== "approved" && rs !== "changes-requested") continue;
      const email = (effectiveAuthorEmail(it.commit, h) || "").toLowerCase();
      const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
      const key = pr + "\x00" + email;
      const prev = verdicts.get(key);
      if (!prev || t >= prev.time) verdicts.set(key, { pr, state: rs, time: t });
    }
    for (const v of verdicts.values()) {
      const r = counts.get(v.pr) || (counts.set(v.pr, { comments: 0, reposts: 0, quotes: 0, approved: 0, changesRequested: 0 }), counts.get(v.pr));
      if (v.state === "approved") r.approved++; else r.changesRequested++;
    }
    ctx.interactionCounts = counts;
    return counts;
  }

  // countsFor returns the interaction/review counts for one item's short hash
  // from a loaded counts map (null when the item has none), so a card can decide
  // whether to render any count chips.
  function countsFor(counts, short) {
    return (counts && counts.get(short)) || null;
  }

  // ---- In-bucket item search (tier i: over already-walked items) ----

  // SEARCH_GROUPS orders and labels the groups the in-bucket search returns.
  // The core search (CLI/TUI) sweeps every cached commit — gitmsg items AND
  // plain code commits — so the site matches it: the gitmsg extensions search
  // their resolved items, and the Commits group searches plain code commits at
  // subject level from the code items index (the code corpus is deliberately
  // metadata-only, so full text never covers code bodies). review/release are
  // type-filtered to their standalone units (PRs, releases); pm/social/memo
  // pass all resolved items. The code group's branch is "" — a code hit links
  // to the plain commit detail route.
  const SEARCH_GROUPS = [
    { ext: "pm", label: "Issues", branch: "gitmsg/pm", type: null },
    { ext: "review", label: "Pull Requests", branch: "gitmsg/review", type: "pull-request" },
    { ext: "social", label: "Posts", branch: "gitmsg/social", type: null },
    { ext: "release", label: "Releases", branch: "gitmsg/release", type: "release" },
    { ext: "memo", label: "Memos", branch: "gitmsg/memo", type: null },
    { ext: "code", label: "Commits", branch: "", type: null },
  ];
  // SEARCH_EXTS is every gitmsg extension branch the search walks (the code
  // lane is fed separately from the code items index in buildSearchCorpus).
  const SEARCH_EXTS = ["social", "pm", "review", "release", "memo"];
  // Header fields worth matching a query against (labels/tag/type/state/version/
  // assignees), beyond the item subject/content and effective author.
  const SEARCH_HEADER_KEYS = ["labels", "tag", "version", "type", "state", "assignees"];

  // itemSubject returns an item's display subject even before hydration: the
  // first line of its content when a body is present, else the metadata-index
  // subject of the commit whose content would be displayed (latest version
  // first, falling back through earlier versions to the canonical — mirroring
  // hydrateItem's content selection). DOM-free.
  function itemSubject(item) {
    const content = (item.content || "").trim();
    if (content) { const nl = content.indexOf("\n"); return (nl < 0 ? content : content.slice(0, nl)).trim(); }
    const versions = item.versions || [];
    for (let i = versions.length - 1; i >= 0; i--) {
      const s = versions[i].commit && versions[i].commit.subject;
      if (s) return s;
    }
    return (item.commit && item.commit.subject) || "";
  }

  // searchableText builds the lowercased haystack an item is matched against: its
  // subject+body content (the index subject alone for a hollow, body-less item),
  // its effective author, and the header fields above — mirroring the TUI search
  // over content + author + extension fields. DOM-free.
  function searchableText(item) {
    const parts = [item.content || itemSubject(item), item.author || ""];
    const h = item.header || {};
    for (const k of SEARCH_HEADER_KEYS) if (h[k]) parts.push(h[k]);
    return parts.join("\n").toLowerCase();
  }

  // FACET_FIELDS are the pivots the in-bucket search exposes (mirroring the TUI's
  // group-by=state/author/type/label). repo/list are omitted (one bucket = one
  // repo); assignee/reviewer/milestone/base are dropped as low-value here.
  const FACET_FIELDS = ["type", "state", "author", "label"];
  // EXT_DEFAULT_TYPE names the Type-facet value for an item whose header carries
  // no explicit type (a plain post, a bare issue), so every item buckets.
  const EXT_DEFAULT_TYPE = { social: "post", pm: "issue", review: "pull-request", release: "release", memo: "memo", code: "commit" };
  // TYPE_ALIASES normalizes a typed `type:` token so the query box and the chips
  // share one vocabulary (type:pr === the pull-request chip).
  const TYPE_ALIASES = { pr: "pull-request", prs: "pull-request" };

  // facetType returns an item's Type-facet token (header type or the ext default).
  function facetType(item, ext) { return (item.header && item.header.type) || EXT_DEFAULT_TYPE[ext] || ext; }
  // facetState returns an item's State-facet value, or "" for stateless
  // extensions (posts, releases, memos have no open/closed workflow).
  function facetState(item, ext) { return (ext === "pm" || ext === "review") ? ((item.header && item.header.state) || "open") : ""; }
  // itemLabelStrings returns an item's raw label strings (Label-facet values).
  function itemLabelStrings(item) { return ((item.header && item.header.labels) || "").split(",").map((s) => s.trim()).filter(Boolean); }
  // authorBlob is the lowercased name+email an `author:` substring matches
  // against. It includes the EFFECTIVE (origin) email over the git commit email
  // so an `author:<email>` deep-link from analytics (which attributes imported
  // content to its upstream author's origin email) matches; the raw git email is
  // also kept so searching by the committer identity still works.
  function authorBlob(item) {
    const gitEmail = (item.commit && item.commit.authorEmail) || "";
    const effEmail = effectiveAuthorEmail(item.commit, item.header) || "";
    return ((item.author || "") + " " + effEmail + " " + gitEmail).toLowerCase();
  }

  // TYPE_GLYPH maps a gitmsg item type to the compact leading glyph the TUI cards
  // use (see tuisocial/util_adapters.go). Issues vary by state and are resolved in
  // typeGlyph; the rest are fixed.
  const TYPE_GLYPH = { post: "•", comment: "↩", repost: "↻", quote: "↻", milestone: "◇", sprint: "◷", "pull-request": "⑂", feedback: "↩", release: "⏏", memo: "☞", commit: "◦" };

  // typeGlyph returns an item's leading type glyph, matching the TUI card icons:
  // ○ open / ● closed for issues, and the fixed TYPE_GLYPH for every other type
  // (from the item's header type, else the extension default). "" when unknown.
  function typeGlyph(item, ext) {
    const h = item.header || {};
    const t = h.type || EXT_DEFAULT_TYPE[h.ext || ext] || "";
    if (t === "issue") return (h.state === "closed" || h.state === "canceled" || h.state === "completed") ? "●" : "○";
    return TYPE_GLYPH[t] || "";
  }

  // COMMIT_HASH_RE recognizes a bare commit-hash token (7-40 hex), mirroring
  // core/search/parse.go: such a token searches by hash prefix rather than as
  // free text. DATE_RE is the strict after:/before: date format (YYYY-MM-DD).
  const COMMIT_HASH_RE = /^[0-9a-fA-F]{7,40}$/;
  const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

  // dateBound parses a strict YYYY-MM-DD (UTC) to unix seconds. `end` returns the
  // inclusive END-of-day bound (23:59:59), so `before:D` includes all of day D;
  // otherwise the start-of-day, so `after:D` includes all of day D. NaN when the
  // string is not a valid strict date (mirrors core/search/parse.go rejecting it).
  function dateBound(s, end) {
    if (!DATE_RE.test(s)) return NaN;
    const ms = Date.parse(s + "T00:00:00Z");
    if (isNaN(ms)) return NaN;
    return Math.floor(ms / 1000) + (end ? 86399 : 0);
  }

  // parseSearchFilters splits a raw query into free-text `terms` and typed facet
  // selections (type:/state:/author:/label:/@author), plus hash/date predicates:
  // hash:/commit:<hex> and bare 7-40 hex tokens become `hashes` (prefix-matched
  // against the item hash), and after:/before:<YYYY-MM-DD> become inclusive
  // effectiveTime `dateFrom`/`dateTo` bounds (mirroring core/search/parse.go).
  // Only these known keys are stripped; any other `word:value` (e.g. a "build:
  // fix" subject) stays in terms so it is still matched literally.
  function parseSearchFilters(query) {
    const typed = { type: [], state: [], author: [], label: [] };
    const hashes = [];
    let dateFrom = null, dateTo = null;
    let terms = String(query || "").replace(/(\w+):(\S+)/g, (m, key, val) => {
      const k = key.toLowerCase(), v = val.toLowerCase();
      if (k === "type") { typed.type.push(TYPE_ALIASES[v] || v); return " "; }
      if (k === "state") { typed.state.push(v); return " "; }
      if (k === "author") { typed.author.push(v); return " "; }
      if (k === "label" || k === "labels") { typed.label.push(v); return " "; }
      if (k === "hash" || k === "commit") { if (COMMIT_HASH_RE.test(val)) { hashes.push(v); return " "; } return m; }
      if (k === "after") { const b = dateBound(val, false); if (!isNaN(b)) { dateFrom = b; return " "; } return m; }
      if (k === "before") { const b = dateBound(val, true); if (!isNaN(b)) { dateTo = b; return " "; } return m; }
      return m;
    }).replace(/(^|\s)@(\S+)/g, (m, pre, val) => { typed.author.push(val.toLowerCase()); return pre; });
    // A bare hex token (7-40) is a hash prefix, not free text — pull it out.
    terms = terms.split(/\s+/).filter((tok) => {
      if (COMMIT_HASH_RE.test(tok)) { hashes.push(tok.toLowerCase()); return false; }
      return true;
    }).join(" ");
    return { terms: terms.trim().toLowerCase(), typed, hashes, dateFrom, dateTo };
  }

  // itemMatchesHash reports whether an item's commit hash (full or short) matches
  // any of the query's hash prefixes.
  function itemMatchesHash(item, hashes) {
    const full = ((item.commit && item.commit.hash) || "").toLowerCase();
    const short = ((item.commit && item.commit.short) || "").toLowerCase();
    return hashes.some((h) => full.startsWith(h) || short.startsWith(h));
  }

  // itemFacetValues returns an item's value(s) for one facet field (label is
  // multi-valued; author uses the display name; type/state their token).
  function itemFacetValues(item, ext, field) {
    if (field === "label") return itemLabelStrings(item);
    if (field === "author") return [item.author || ""];
    return [field === "type" ? facetType(item, ext) : facetState(item, ext)];
  }

  // matchesFacet reports whether an item satisfies one field's active selection —
  // the clicked-chip set unioned with the typed tokens. An empty selection matches
  // all; otherwise OR within the field. Author matches by name/email substring,
  // the rest by exact token.
  function matchesFacet(item, ext, field, chip, typed) {
    if (!chip.size && !typed.length) return true;
    if (field === "author") {
      if (chip.has(item.author)) return true;
      const blob = authorBlob(item);
      return typed.some((a) => blob.indexOf(a) !== -1);
    }
    return itemFacetValues(item, ext, field).some((v) => chip.has(v) || typed.indexOf(String(v).toLowerCase()) !== -1);
  }

  // facetSelected marks a chip active when its value is in the clicked set or is
  // picked out by a typed token (so typing state:open lights the open chip).
  function facetSelected(field, value, chip, typed) {
    if (chip.has(value)) return true;
    if (field === "author") { const v = value.toLowerCase(); return typed.some((a) => v.indexOf(a) !== -1); }
    return typed.indexOf(String(value).toLowerCase()) !== -1;
  }

  // searchItemsFaceted runs the in-bucket search with faceting. `query` may carry
  // typed filters; `filters` holds the clicked-chip selections as
  // { type, state, author, label } Sets. Returns the extension-grouped results
  // (every active field applied) plus per-field facet buckets whose counts are
  // drill-down (each field counted with the OTHER fields applied), so a chip shows
  // how many results it would add. Honest over the loaded corpus. An idle query
  // (no terms, no typed, no chips) yields empty results and no facets.
  // searchRelevance ranks a matched item cheaply: a hash-prefix hit is surfaced
  // first (2), a subject match next (1), a body-only match last (0). Within a
  // rank, recency (effectiveTime) breaks ties — the sort caller applies that.
  function searchRelevance(item, terms, hashes) {
    if (hashes.length && itemMatchesHash(item, hashes)) return 2;
    if (terms && itemSubject(item).toLowerCase().indexOf(terms) !== -1) return 1;
    return 0;
  }

  function searchItemsFaceted(query, perExt, filters) {
    const f = filters || {};
    const chip = { type: f.type || new Set(), state: f.state || new Set(), author: f.author || new Set(), label: f.label || new Set() };
    const { terms, typed, hashes, dateFrom, dateTo } = parseSearchFilters(query);
    const active = !!terms || hashes.length > 0 || dateFrom !== null || dateTo !== null || FACET_FIELDS.some((k) => typed[k].length || chip[k].size);
    if (!active) return { query: "", terms: "", total: 0, groups: [], facets: {} };
    // Flatten the corpus once (with each item's ext and group), pre-filtered by
    // the free-text terms, hash prefixes, date bounds, and the group's own type
    // guard. A hash query matches on the item hash (prefix), independent of terms.
    const pool = [];
    for (const spec of SEARCH_GROUPS) {
      for (const it of (perExt[spec.ext] || [])) {
        if (spec.type && ((it.header && it.header.type) || "") !== spec.type) continue;
        if (terms && searchableText(it).indexOf(terms) === -1) continue;
        if (hashes.length && !itemMatchesHash(it, hashes)) continue;
        if (dateFrom !== null || dateTo !== null) {
          const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
          if (dateFrom !== null && t < dateFrom) continue;
          if (dateTo !== null && t > dateTo) continue;
        }
        pool.push({ it, ext: spec.ext, spec });
      }
    }
    const passes = (row, skip) => FACET_FIELDS.every((fld) => fld === skip || matchesFacet(row.it, row.ext, fld, chip[fld], typed[fld]));
    const byExt = new Map();
    let total = 0;
    for (const row of pool) {
      if (!passes(row, null)) continue;
      let g = byExt.get(row.spec.ext);
      if (!g) { g = { ext: row.spec.ext, label: row.spec.label, branch: row.spec.branch, count: 0, items: [] }; byExt.set(row.spec.ext, g); }
      g.items.push(row.it); g.count++; total++;
    }
    const groups = [];
    // Within a group, rank by relevance (hash hit, then subject over body-only),
    // then recency — a cheap relevance sort over the today-recency-only order.
    for (const spec of SEARCH_GROUPS) {
      const g = byExt.get(spec.ext);
      if (!g) continue;
      g.items.sort((a, b) => (searchRelevance(b, terms, hashes) - searchRelevance(a, terms, hashes)) || (b.effectiveTime - a.effectiveTime));
      groups.push(g);
    }
    // Per-field counts over items passing terms + every OTHER active field.
    const facets = {};
    for (const fld of FACET_FIELDS) {
      const counts = new Map();
      for (const row of pool) {
        if (!passes(row, fld)) continue;
        for (const v of itemFacetValues(row.it, row.ext, fld)) { if (v) counts.set(v, (counts.get(v) || 0) + 1); }
      }
      facets[fld] = Array.from(counts, ([value, count]) => ({ value, count, selected: facetSelected(fld, value, chip[fld], typed[fld]) }))
        .sort((a, b) => b.count - a.count || (a.value < b.value ? -1 : 1));
    }
    return { query: (query || "").trim().toLowerCase(), terms, total, groups, facets };
  }

  // searchItems is the facet-free entry point (backward-compatible shape) the
  // DOM-free unit tests use: extension-grouped hits for a plain substring query,
  // newest-first within each group; an empty query yields no groups.
  function searchItems(query, perExt) {
    const r = searchItemsFaceted(query, perExt, null);
    return { query: r.query, total: r.total, groups: r.groups };
  }

  // loadBodyIndex fetches the push-maintained search corpus for one extension
  // (version 4, brotli — decoded transparently by the browser) once per context;
  // null when the bucket carries none. This is the ONLY artifact carrying message
  // bodies, so it is loaded only when the user asks search to cover full text.
  // The corpus is append-only sharded under .gitsocial/site/bodies/<ext>/: the
  // manifest lists immutable sealed shards (browser-cached across pushes) plus
  // the no-cache head.
  async function loadBodyIndex(ctx, ext) {
    if (!ctx.bodyIndex) ctx.bodyIndex = {};
    if (ctx.bodyIndex[ext] !== undefined) return ctx.bodyIndex[ext];
    let idx = null;
    try {
      idx = await loadBodyIndexSharded(ctx, ext);
    } catch (e) { if (e && e.forbidden) throw e; }
    ctx.bodyIndex[ext] = idx;
    return idx;
  }

  // loadBodyIndexSharded assembles the sharded corpus into the { version, tip,
  // items } shape: it fetches the manifest, then all sealed shards and the head
  // in parallel, concatenates their items oldest-first (sealed shards
  // oldest→newest, then head), and reverses to newest-first — the order
  // resolveItems expects (latest edit wins). Null when the bucket carries no
  // manifest or an older version.
  async function loadBodyIndexSharded(ctx, ext) {
    const dir = ".gitsocial/site/bodies/" + ext + "/";
    const mtext = await fetchText(ctx.base, dir + "manifest.json");
    if (!mtext) return null;
    let m;
    try { m = JSON.parse(mtext); } catch { return null; }
    if (!m || m.version !== 4 || !/^[0-9a-f]{40}$/.test(m.tip || "") || !Array.isArray(m.shards)) return null;
    const keys = m.shards.map((s) => dir + s.key).concat([dir + "head.json"]);
    const texts = await Promise.all(keys.map((k) => fetchText(ctx.base, k)));
    const items = [];
    for (const t of texts) {
      if (!t) continue;
      let doc;
      try { doc = JSON.parse(t); } catch { continue; }
      if (doc && Array.isArray(doc.items)) for (const it of doc.items) items.push(it);
    }
    items.reverse();
    return { version: 4, tip: m.tip, items };
  }

  // buildSearchCorpus assembles the per-extension search sets in two tiers. The
  // light tier (default) costs no extra downloads: an extension whose history is
  // fully walked — from the always-loaded metadata index, or a short branch —
  // searches every item over subject, author, and header fields (bodies absent
  // until hydration; `light` marks that some results are body-less). The full
  // tier (full=true, an explicit user request) fetches the bodies corpus and
  // searches complete message text. An extension with neither falls back to the
  // display items walked so far, with the "Search deeper" affordance (truncated).
  // Cached on ctx; a deeper request advances only the fallback walks.
  // olderItemBytes sums the compressed size of every corpus's not-yet-resident
  // older metadata shards — including the code corpus, which the search drains
  // too — the download the "search older items" affordance incurs (the
  // light-search counterpart of fullSearchBytes). 0 once every corpus's shards
  // are resident or a bucket has no index.
  async function olderItemBytes(ctx) {
    let total = 0;
    for (const ext of SEARCH_EXTS.concat("code")) {
      const idx = await loadItemsIndex(ctx, ext);
      if (idx && !idx.allResident) total += idx.olderBytes || 0;
    }
    return total;
  }

  async function buildSearchCorpus(ctx, extend, full, searchOlder) {
    const perExt = {};
    let truncated = false, light = false, hasOlder = false, partial = false;
    for (const ext of SEARCH_EXTS) {
      if (full) {
        const bodies = await loadBodyIndex(ctx, ext);
        if (bodies) { perExt[ext] = resolveItems(bodies.items.map(indexCommit)); continue; }
      }
      // Light search covers the resident metadata (eager set + already-walked).
      // "Search older items" pulls every remaining shard first; otherwise older
      // shards stay a pending opt-in (hasOlder), and an incomplete manifest means
      // even the full older set covers only the bootstrapped prefix (partial).
      const idx = await loadItemsIndex(ctx, ext);
      if (idx && searchOlder && !idx.allResident) await loadOlderItemShards(ctx, ext);
      const w = await extWalkState(ctx, ext);
      if (w && extWalkExhausted(w)) {
        perExt[ext] = resolveItems(walkedCommits(w.state));
        if (perExt[ext].some((it) => it.commit.hollow)) light = true;
        if (w.older && w.older.length) hasOlder = true;
        if (idx && idx.complete === false) partial = true;
        continue;
      }
      const r = await loadExtItemsWindow(ctx, ext, extend);
      perExt[ext] = r.items;
      if (r.truncated) truncated = true;
    }
    // Plain code commits join at SUBJECT level from the code items index
    // (shared with the timeline/graph walks, so shards load once). The code
    // corpus is deliberately metadata-only — no bodies corpus — so the full
    // tier never upgrades these entries; in the light tier their presence marks
    // the corpus `light` (body-less results exist), and "search older" drains
    // the code shards like the extension corpora. No index (a v<4 or data-only
    // bucket): no code lane, the pre-index behavior.
    const cw = await codeIndexWalkState(ctx);
    if (cw) {
      await withWalkLock(cw, async () => {
        if (searchOlder) {
          let guard = (cw.older || []).length, stall = 0;
          while (cw.older && cw.older.length) {
            await loadNextCodeShard(ctx, cw);
            const n = (cw.older || []).length;
            if (n === guard) { if (++stall >= 2) break; } else stall = 0;
            guard = n;
          }
        }
        perExt.code = cw.items.map((c) => codeCommitItem(c, c._branch || ""));
      });
      if (perExt.code.length && !full) light = true;
      if (cw.older && cw.older.length) hasOlder = true;
      if (!cw.complete) partial = true;
    }
    ctx.searchCorpus = { perExt, truncated, light, hasOlder, partial, full: !!full, older: !!searchOlder };
    return ctx.searchCorpus;
  }

  // loadSearchWindow returns the in-bucket search corpus { perExt, truncated,
  // light, hasOlder, partial, full, older }. The first call builds the light tier
  // over resident metadata (no downloads beyond the eager set); a deeper request
  // advances any fallback walks; `searchOlder` pulls the remaining metadata shards
  // for whole-history light search; `full` fetches the bodies artifacts and
  // upgrades to full-text search. Fullness and older-coverage are sticky: once
  // upgraded, later rebuilds stay upgraded.
  async function loadSearchWindow(ctx, extend, full, searchOlder) {
    const cached = ctx.searchCorpus;
    const wantFull = !!full || !!(cached && cached.full);
    const wantOlder = !!searchOlder || !!(cached && cached.older);
    if (cached && cached.full === wantFull && cached.older === wantOlder && !(extend && cached.truncated)) return cached;
    return buildSearchCorpus(ctx, extend, wantFull, wantOlder);
  }

  // fullSearchBytes sums the compressed byte size of every extension's bodies
  // corpus, read from the already-loaded metadata index manifests (their
  // bodiesBytes) — the download the "Load full search index" affordance will
  // incur. 0 when unknown (no index, or an index predating the recorded size).
  async function fullSearchBytes(ctx) {
    let total = 0;
    for (const ext of SEARCH_EXTS) {
      const idx = await loadItemsIndex(ctx, ext);
      if (idx && idx.bodiesBytes) total += idx.bodiesBytes;
    }
    return total;
  }

  // ---- Review feedback (DOM-free, testable) ----

  // feedbackLine returns a feedback's anchor { side, line }: the new-file line
  // (preferred) else the old-file line, or null when it carries no line ref.
  function feedbackLine(header) {
    const nl = parseInt((header && header["new-line"]) || "", 10);
    const ol = parseInt((header && header["old-line"]) || "", 10);
    if (!isNaN(nl) && nl > 0) return { side: "new", line: nl };
    if (!isNaN(ol) && ol > 0) return { side: "old", line: ol };
    return null;
  }

  // feedbackAnchorKey returns the diff-line key a feedback anchors to
  // ("n<newLine>" preferred, else "o<oldLine>"), or null when it has no line ref.
  function feedbackAnchorKey(header) {
    const a = feedbackLine(header);
    return a ? (a.side === "new" ? "n" : "o") + a.line : null;
  }

  // hunkLineKeys returns the anchor keys a rendered diff line answers to: its
  // new-side and/or old-side key (a context line answers to both).
  function hunkLineKeys(l) {
    const ks = [];
    if (l.newN) ks.push("n" + l.newN);
    if (l.oldN) ks.push("o" + l.oldN);
    return ks;
  }

  // anchorFeedback partitions a file's feedback against a rendered hunk set:
  // byKey maps a diff-line anchor key to the feedback attached there, offscreen
  // lists feedback whose line is absent from every rendered hunk line (shown in a
  // separate "not on visible lines" block). Matching is new-line-first, else
  // old-line — mirroring the TUI feedback layer.
  function anchorFeedback(fbList, hunks) {
    const present = new Set();
    for (const h of hunks || []) for (const l of h.lines) for (const k of hunkLineKeys(l)) present.add(k);
    const byKey = new Map();
    const offscreen = [];
    for (const fb of fbList) {
      const key = feedbackAnchorKey(fb.header);
      if (key && present.has(key)) {
        if (!byKey.has(key)) byKey.set(key, []);
        byKey.get(key).push(fb);
      } else offscreen.push(fb);
    }
    return { byKey, offscreen };
  }

  // prFeedback selects the feedback referencing a PR (by short hash) out of a
  // resolved review item set, splitting file-anchored feedback (inline, carries
  // `file`) from the rest (verdicts and general review feedback).
  function prFeedback(reviewItems, prShort) {
    const all = reviewItems.filter((i) => i.header && i.header.type === "feedback" && hashEq(refHash(i.header["pull-request"]), prShort));
    const file = all.filter((i) => i.header.file);
    const nonFile = all.filter((i) => !i.header.file);
    return { all, file, nonFile };
  }

  // reviewSummary aggregates a PR's feedback into review state, mirroring
  // review.ComputeReviewSummary: the latest review-state per reviewer email wins;
  // approved / changesRequested count those verdicts; pending counts declared
  // reviewers with no verdict; isBlocked when any changes-requested; isApproved
  // when at least one approval, no changes-requested, and nothing pending. The
  // reviewers list carries a per-reviewer chip (latest verdict, else "commented").
  function reviewSummary(feedbackItems, reviewers) {
    const latestVerdict = new Map();
    const acted = new Map();
    for (const it of feedbackItems) {
      const email = (effectiveAuthorEmail(it.commit, it.header) || "").toLowerCase();
      const name = effectiveAuthor(it.commit, it.header);
      const t = it.effectiveTime || (it.commit && it.commit.authorTime) || 0;
      const a = acted.get(email);
      if (!a || t >= a.time) acted.set(email, { name, time: t });
      const rs = it.header && it.header["review-state"];
      if (rs !== "approved" && rs !== "changes-requested") continue;
      const prev = latestVerdict.get(email);
      if (!prev || t >= prev.time) latestVerdict.set(email, { state: rs, time: t, name });
    }
    let approved = 0, changesRequested = 0;
    for (const v of latestVerdict.values()) { if (v.state === "approved") approved++; else changesRequested++; }
    const declared = (typeof reviewers === "string" ? reviewers.split(",") : (reviewers || [])).map((s) => (s || "").trim().toLowerCase()).filter(Boolean);
    let pending = 0;
    for (const em of declared) if (!latestVerdict.has(em)) pending++;
    const chips = [];
    for (const [email, a] of acted) {
      const v = latestVerdict.get(email);
      chips.push({ email, name: a.name, state: v ? v.state : "commented", time: v ? v.time : a.time });
    }
    chips.sort((x, y) => x.time - y.time);
    return { approved, changesRequested, pending, isBlocked: changesRequested > 0, isApproved: approved > 0 && changesRequested === 0 && pending === 0, reviewers: chips };
  }

  // suggestionBody extracts the replacement text from a suggestion feedback body
  // (the ```suggestion fenced block, GITREVIEW §1.4), or the whole trimmed body
  // when no fence is present.
  function suggestionBody(content) {
    const m = /```suggestion[^\n]*\n([\s\S]*?)```/.exec(content || "");
    return m ? m[1].replace(/\n$/, "") : (content || "").trim();
  }

  // authorStats aggregates commit counts by author name (falling back to email)
  // over an already-walked commit set, descending by count then name.
  function authorStats(commits) {
    const map = new Map();
    for (const c of commits) { const name = c.authorName || c.authorEmail || "unknown"; map.set(name, (map.get(name) || 0) + 1); }
    const authors = Array.from(map, ([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count || (a.name < b.name ? -1 : 1));
    return { authors, total: commits.length };
  }

  // isBinary detects binary content: a NUL byte within the first 8000 bytes.
  function isBinary(bytes) {
    const n = Math.min(bytes.length, 8000);
    for (let i = 0; i < n; i++) if (bytes[i] === 0) return true;
    return false;
  }

  // ---- Lists (per-element refs; discovery/parsing is DOM-free, testable) ----

  // List ref layout (gitmsg/list.go): metadata at
  // refs/gitmsg/<ext>/lists/<name>/_meta (commit message is JSON), each member
  // at refs/gitmsg/<ext>/lists/<name>/items/<hash> (commit message is the full
  // member ref string, e.g. `https://x/y#branch:main`).
  const LIST_META_SUFFIX = "/_meta";
  const LIST_ITEMS_SEG = "/items/";

  // parseListRef splits a lists-namespace ref name into { ext, name, kind, hash }
  // (kind is 'meta' or 'item'); null for a ref outside any lists namespace. The
  // pre-migration single-ref layout (no /_meta) is read as the metadata ref.
  function parseListRef(ref) {
    const m = /^refs\/gitmsg\/([^/]+)\/lists\/(.+)$/.exec(ref || "");
    if (!m) return null;
    const ext = m[1], rest = m[2];
    if (rest.endsWith(LIST_META_SUFFIX)) return { ext, name: rest.slice(0, -LIST_META_SUFFIX.length), kind: "meta", hash: "" };
    const i = rest.indexOf(LIST_ITEMS_SEG);
    if (i >= 0) return { ext, name: rest.slice(0, i), kind: "item", hash: rest.slice(i + LIST_ITEMS_SEG.length) };
    if (rest.indexOf("/") < 0) return { ext, name: rest, kind: "meta", hash: "" };
    return null;
  }

  // enumerateLists groups a manifest's list refs into per-list descriptors
  // ({ ext, name, id, metaRef, itemRefs }), sorted by ext then name. The id
  // (`<ext>/<name>`) is the value carried by the `#list:` route.
  function enumerateLists(manifest) {
    if (!manifest) return [];
    const byKey = new Map();
    for (const ref of Object.keys(manifest)) {
      const p = parseListRef(ref);
      if (!p) continue;
      const key = p.ext + "\x00" + p.name;
      if (!byKey.has(key)) byKey.set(key, { ext: p.ext, name: p.name, id: p.ext + "/" + p.name, metaRef: "", itemRefs: [] });
      const rec = byKey.get(key);
      if (p.kind === "meta") rec.metaRef = ref;
      else rec.itemRefs.push(ref);
    }
    const out = Array.from(byKey.values());
    out.sort((a, b) => (a.ext < b.ext ? -1 : a.ext > b.ext ? 1 : a.name.localeCompare(b.name)));
    return out;
  }

  // listMemberRef classifies a member ref string ("[url]#type:value"):
  // { repoUrl, ref, local }. A local (workspace-relative, no url) member links
  // into this bucket; a foreign member's objects live elsewhere (labeled text).
  function listMemberRef(memberRef) {
    const url = refRepoUrl(memberRef);
    return { repoUrl: url, ref: memberRef || "", local: !url };
  }

  // jsonCommitMessage JSON-parses a commit message that is pure JSON (list _meta
  // and ext config commits, created by CreateCommitTree), returning the object or
  // null. DOM-free.
  function jsonCommitMessage(msg) {
    try { const v = JSON.parse((msg || "").trim()); return (v && typeof v === "object") ? v : null; } catch { return null; }
  }

  // loadListMeta resolves a list's _meta commit into its metadata object
  // (name/id/version); {} when the ref is absent/unparseable.
  async function loadListMeta(ctx, list) {
    if (!list.metaRef) return {};
    const sha = await refTip(ctx, list.metaRef);
    if (!sha) return {};
    const obj = await getObject(ctx, sha);
    if (!obj || obj.type !== "commit") return {};
    return jsonCommitMessage(parseCommit(sha, obj.body).content) || {};
  }

  // loadListMembers resolves a list's per-element item refs into member ref
  // strings (each item commit's message is the member ref), sorted.
  async function loadListMembers(ctx, list) {
    const out = [];
    for (const ref of list.itemRefs) {
      const sha = await refTip(ctx, ref);
      if (!sha) continue;
      const obj = await getObject(ctx, sha);
      if (!obj || obj.type !== "commit") continue;
      const member = parseCommit(sha, obj.body).content.trim();
      if (member) out.push(member);
    }
    out.sort();
    return out;
  }

  // loadListsSummary returns every list in the bucket with its metadata and
  // member count (count is the item-ref count, no per-member fetch). Empty when
  // the bucket has no manifest or no list refs.
  async function loadListsSummary(ctx) {
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const lists = enumerateLists(ctx.manifest);
    const out = [];
    for (const l of lists) out.push({ ext: l.ext, name: l.name, id: l.id, meta: await loadListMeta(ctx, l), count: l.itemRefs.length });
    return out;
  }

  // loadListDetail resolves one list by id (`<ext>/<name>`) with its resolved
  // members; null when no such list exists.
  async function loadListDetail(ctx, id) {
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const l = enumerateLists(ctx.manifest).find((x) => x.id === id);
    if (!l) return null;
    return { ext: l.ext, name: l.name, id: l.id, meta: await loadListMeta(ctx, l), members: await loadListMembers(ctx, l) };
  }

  // loadExtConfig resolves an extension's refs/gitmsg/<ext>/config commit into
  // its JSON object (the whole commit message is the config JSON); null when the
  // ref is absent/unparseable (rendered as "defaults"). Discovery is via refTip
  // (live plain key or manifest).
  async function loadExtConfig(ctx, ext) {
    const sha = await refTip(ctx, "refs/gitmsg/" + ext + "/config");
    if (!sha) return null;
    const obj = await getObject(ctx, sha);
    if (!obj || obj.type !== "commit") return null;
    return jsonCommitMessage(parseCommit(sha, obj.body).content);
  }

  // ---- Forks (refs/gitmsg/core/forks/<urlHash>, per-element refs) ----

  // FORKS_PREFIX is the ref namespace one ref per registered fork lives under
  // (CLAUDE.md "Workspace refs"). Each ref's NAME is a one-way hash of the fork
  // URL; the URL itself is the pointed-to commit's message (gitmsg/forks.go).
  const FORKS_PREFIX = "refs/gitmsg/core/forks/";

  // forkRefNames returns the manifest's fork ref names (each pointing at a valid
  // 40-hex sha), sorted — the browser's only fork-ref discovery (public buckets
  // expose no listing). DOM-free.
  function forkRefNames(manifest) {
    if (!manifest) return [];
    return Object.keys(manifest)
      .filter((r) => r.startsWith(FORKS_PREFIX) && /^[0-9a-f]{40}$/.test(manifest[r] || ""))
      .sort();
  }

  // loadForks resolves the registered forks from the refs manifest: each fork ref
  // points at a commit whose message is the normalized fork URL (the ref name is a
  // non-reversible hash, so the URL only comes from the commit) and whose author
  // time stands in for "last updated" (the fork ref moves when the fork is
  // re-registered/refetched). Returns [{ url, time }] sorted most-recently-updated
  // first, ties broken by URL for a deterministic order; empty when the bucket has
  // no manifest or no fork refs. The commit-count / last-fetch columns the TUI
  // shows are cache-derived and unavailable to a browser reader.
  async function loadForks(ctx) {
    if (ctx.manifest === undefined) ctx.manifest = await loadManifest(ctx.base);
    const refs = forkRefNames(ctx.manifest);
    const out = [];
    for (const ref of refs) {
      const sha = ctx.manifest[ref];
      const obj = await getObject(ctx, sha);
      if (!obj || obj.type !== "commit") continue;
      const c = parseCommit(sha, obj.body);
      const url = c.content.trim();
      if (url) out.push({ url, time: c.authorTime || 0 });
    }
    out.sort((a, b) => (b.time - a.time) || a.url.localeCompare(b.url));
    return out;
  }

  // ---- Analytics aggregation (DOM-free, testable) ----

  // commitsByMonth buckets a walked commit set by calendar month (YYYY-MM, UTC)
  // into contiguous buckets from the earliest to the latest commit month, with
  // the peak count — the model for the activity-over-time bar row. Empty input
  // yields { buckets: [], max: 0 }.
  function commitsByMonth(commits) {
    const map = new Map();
    let lo = Infinity, hi = -Infinity;
    for (const c of commits) {
      const t = c && c.authorTime;
      if (!t) continue;
      const d = new Date(t * 1000);
      const ym = d.getUTCFullYear() * 12 + d.getUTCMonth();
      map.set(ym, (map.get(ym) || 0) + 1);
      if (ym < lo) lo = ym;
      if (ym > hi) hi = ym;
    }
    if (!isFinite(lo)) return { buckets: [], max: 0 };
    const buckets = [];
    let max = 0;
    for (let ym = lo; ym <= hi; ym++) {
      const count = map.get(ym) || 0;
      const y = Math.floor(ym / 12), mo = (ym % 12) + 1;
      buckets.push({ month: y + "-" + String(mo).padStart(2, "0"), count });
      if (count > max) max = count;
    }
    return { buckets, max };
  }

  // extensionStats reduces the resolved item sets of every extension into the
  // per-extension counts the analytics page shows (issues open/closed, PR
  // open/merged/closed, releases, posts, memos, milestones, sprints). DOM-free.
  function extensionStats(perExt) {
    const pm = groupPM(perExt.pm || []);
    const issue = pmProgress(pm.issues);
    const prs = (perExt.review || []).filter((i) => (i.header && i.header.type) === "pull-request");
    const prState = { open: 0, merged: 0, closed: 0 };
    for (const p of prs) {
      const s = (p.header && p.header.state) || "open";
      if (s === "merged") prState.merged++;
      else if (s === "closed") prState.closed++;
      else prState.open++;
    }
    const releases = (perExt.release || []).filter((i) => (i.header && i.header.type) === "release");
    return {
      issues: { open: issue.total - issue.closed, closed: issue.closed, total: issue.total },
      milestones: pm.milestones.length, sprints: pm.sprints.length,
      prs: { open: prState.open, merged: prState.merged, closed: prState.closed, total: prs.length },
      releases: releases.length,
      posts: (perExt.social || []).length,
      memos: (perExt.memo || []).length,
    };
  }

  // latestReleaseVersion returns the newest release's version/tag label, or "".
  function latestReleaseVersion(releaseItems) {
    const rels = (releaseItems || []).filter((i) => (i.header && i.header.type) === "release");
    if (!rels.length) return "";
    const top = rels.slice().sort((a, b) => b.effectiveTime - a.effectiveTime)[0];
    const h = top.header || {};
    return h.version ? ("v" + h.version) : (h.tag || "");
  }

  // ANALYTICS_SPECS maps each extension data branch to the analytics series it
  // contributes and the item type that series counts, mirroring the timeline's
  // categorization (social→posts all, pm→issues, review→PRs, release→releases)
  // plus memos. ANALYTICS_KINDS is the ordered series list every bucket carries.
  const ANALYTICS_SPECS = [
    { ext: "social", kind: "posts", type: "" },
    { ext: "pm", kind: "issues", type: "issue" },
    { ext: "review", kind: "prs", type: "pull-request" },
    { ext: "release", kind: "releases", type: "release" },
    { ext: "memo", kind: "memos", type: "" },
  ];
  const ANALYTICS_KINDS = ANALYTICS_SPECS.map((s) => s.kind);

  // COUNTS_WALK_CAP bounds the loose-object walk an exhaustive-set loader is
  // allowed to do for ONE extension when that extension has no metadata index (or
  // a stale one that never bridges), so its items resolve only by walking loose
  // objects. The full item set is only cheap when the index is present and
  // current; without it, exhaustion degrades to one loose GET per commit —
  // hundreds to thousands of sequential GETs on a big data branch (mid-push, a
  // bucket pushed by plain git, a never-indexed ext), which stalls the view behind
  // an unbounded walk and can trip R2 rate limits. The full set is never
  // first-paint-critical: counts are a card-stat nicety, and a filter/board/
  // analytics view degrades quietly to the most recent COUNTS_WALK_CAP commits.
  // An index-backed walk is exhausted from cheap, browser-cached metadata shards
  // and ignores this cap, so an indexed bucket is unchanged (exact, exhaustive).
  const COUNTS_WALK_CAP = WALK_CAP;

  // loadExtItemsAll returns an extension's resolved items, metadata-only
  // (UN-hydrated, no body fetches) — the input to the filter/board/analytics
  // views and the interaction counts. An index-seeded walk pulls every older
  // metadata shard (immutable, browser-cached) and costs no loose-object fetches,
  // so an indexed bucket yields the COMPLETE set. On an index-absent or stale
  // bucket the walk falls to loose objects; `looseCap` bounds THAT walk to its
  // most-recent `looseCap` commits (default COUNTS_WALK_CAP) so first paint is
  // never gated on an unbounded exhaustion walk. Empty when the branch is absent.
  async function loadExtItemsAll(ctx, ext, looseCap) {
    const cap = looseCap || COUNTS_WALK_CAP;
    const w = await extWalkState(ctx, ext);
    if (!w) return [];
    return withWalkLock(w, async () => {
      if (w.state.commits.length === 0) await stepExtWalk(ctx, w, WALK_CAP);
      // Progress guard: a step that neither adds commits nor drains an older shard
      // is a stall; break rather than spin (a malformed corpus degrades to a
      // partial list, never an eternal "Loading…"). Loose-walk cap: an index-backed
      // state has an empty frontier (its steps only drain cheap older shards) so
      // w.older drives it to exhaustion regardless of the cap; a loose walk stops
      // once `cap` commits have been visited.
      let guardN = w.state.commits.length, guardShards = (w.older || []).length, stall = 0;
      while (!extWalkExhausted(w)) {
        if (!(w.older && w.older.length) && w.state.visited.size >= cap) break;
        await stepExtWalk(ctx, w, WALK_CAP);
        const n = w.state.commits.length, s = (w.older || []).length;
        if (n === guardN && s === guardShards) { if (++stall >= 2) break; } else stall = 0;
        guardN = n; guardShards = s;
      }
      return resolveItems(walkedCommits(w.state));
    });
  }

  // loadExtItemsForCounts returns an extension's resolved items for the interaction
  // tallies. A thin alias over loadExtItemsAll's default (COUNTS_WALK_CAP) loose
  // bound: exact on an indexed bucket, the most-recent cap commits otherwise.
  async function loadExtItemsForCounts(ctx, ext) {
    return loadExtItemsAll(ctx, ext);
  }

  // loadSiteStats fetches the push-computed stats blob (.gitsocial/site/stats.json)
  // — currently the default branch's regular commit count. Null when absent (a
  // bucket pushed before stats, or by a plain git push). Cached on ctx.
  async function loadSiteStats(ctx) {
    if (ctx.siteStats !== undefined) return ctx.siteStats;
    let stats = null;
    const text = await fetchText(ctx.base, ".gitsocial/site/stats.json");
    if (text) { try { stats = JSON.parse(text); } catch { stats = null; } }
    ctx.siteStats = stats;
    return stats;
  }

  // loadAnalyticsData loads every extension's item set (metadata-only) and reduces
  // it to the flat, body-free entry list the analytics view aggregates: one
  // { kind, time, author, email } per counted item. Also returns the running
  // per-kind totals, the ordered kind list, the grand total, and the latest
  // release label. The item set is COMPLETE on an index-seeded bucket (cheap
  // metadata shards, no loose fetches); on an index-absent or stale bucket each
  // branch's loose walk is bounded to its most-recent COUNTS_WALK_CAP commits (the
  // partial flag the view surfaces), never an unbounded exhaustion fan-out. Also
  // returns `partial`: true when any extension's set was capped by the loose bound,
  // so the view can note the coverage is limited to recent items.
  async function loadAnalyticsData(ctx) {
    const entries = [];
    const perKind = {};
    let latestRelease = "";
    let partial = false;
    for (const spec of ANALYTICS_SPECS) {
      const items = await loadExtItemsAll(ctx, spec.ext);
      if (!(await extSetComplete(ctx, spec.ext))) partial = true;
      if (spec.ext === "release") latestRelease = latestReleaseVersion(items);
      let count = 0;
      for (const it of items) {
        if (spec.type && ((it.header && it.header.type) || "") !== spec.type) continue;
        const c = it.commit;
        entries.push({
          kind: spec.kind,
          time: it.effectiveTime || (c && c.authorTime) || 0,
          author: it.author || (c && (c.authorName || c.authorEmail)) || "unknown",
          email: effectiveAuthorEmail(c, it.header) || "",
        });
        count++;
      }
      perKind[spec.kind] = count;
    }
    return { entries, perKind, kinds: ANALYTICS_KINDS.slice(), total: entries.length, latestRelease, partial };
  }

  // activityBuckets buckets analytics entries into contiguous periods at the
  // chosen granularity ("weekly" | "monthly" | "yearly", all UTC), each bucket
  // carrying a per-kind count map and its total, plus the peak total across
  // buckets — the model the stacked activity chart renders. Gaps between the
  // first and last active period are filled with empty buckets so the timeline is
  // continuous. Weekly buckets start Monday. Empty input yields { buckets:[], max:0 }.
  function activityBuckets(entries, gran, kinds) {
    const pad = (n) => String(n).padStart(2, "0");
    const step = gran === "weekly" ? 7 : 1;
    const idxOf = (time) => {
      if (gran === "weekly") { const day = Math.floor(time / 86400); return day - (((day % 7) + 3) % 7); }
      const d = new Date(time * 1000);
      if (gran === "yearly") return d.getUTCFullYear();
      return d.getUTCFullYear() * 12 + d.getUTCMonth();
    };
    const labelOf = (idx) => {
      if (gran === "weekly") { const d = new Date(idx * 86400000); return d.getUTCFullYear() + "-" + pad(d.getUTCMonth() + 1) + "-" + pad(d.getUTCDate()); }
      if (gran === "yearly") return String(idx);
      return Math.floor(idx / 12) + "-" + pad((idx % 12) + 1);
    };
    // shortOf is the compact under-bar axis label; weekly collapses the full
    // ISO date to "Mon D" so it fits a narrow column (the full date stays on the
    // column's hover title). Monthly/yearly are already short.
    const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
    const shortOf = (idx) => {
      if (gran !== "weekly") return labelOf(idx);
      const d = new Date(idx * 86400000);
      return MONTHS[d.getUTCMonth()] + " " + d.getUTCDate();
    };
    const zero = () => { const z = {}; for (const k of kinds) z[k] = 0; return z; };
    const map = new Map();
    let lo = Infinity, hi = -Infinity;
    for (const e of entries) {
      if (!e.time) continue;
      const idx = idxOf(e.time);
      if (!map.has(idx)) map.set(idx, zero());
      const counts = map.get(idx);
      if (counts[e.kind] === undefined) counts[e.kind] = 0;
      counts[e.kind]++;
      if (idx < lo) lo = idx;
      if (idx > hi) hi = idx;
    }
    if (!isFinite(lo)) return { buckets: [], max: 0 };
    const buckets = [];
    let max = 0;
    for (let idx = lo; idx <= hi; idx += step) {
      const counts = map.get(idx) || zero();
      let total = 0;
      for (const k of kinds) total += counts[k] || 0;
      buckets.push({ label: labelOf(idx), short: shortOf(idx), counts, total });
      if (total > max) max = total;
    }
    return { buckets, max };
  }

  // topItemAuthors ranks analytics entries by item count, keyed by email (falling
  // back to name) so an author's items merge across name spellings, descending by
  // count then name. Each row carries the display name, email (for the hover
  // tooltip), and count. DOM-free.
  function topItemAuthors(entries, limit) {
    const map = new Map();
    for (const e of entries) {
      const key = (e.email || e.author || "unknown").toLowerCase();
      let r = map.get(key);
      if (!r) { r = { name: e.author || e.email || "unknown", email: e.email || "", count: 0 }; map.set(key, r); }
      r.count++;
      if (!r.email && e.email) r.email = e.email;
    }
    const authors = Array.from(map.values()).sort((a, b) => b.count - a.count || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
    return limit ? authors.slice(0, limit) : authors;
  }


  // ---- File-type icons (vendored @pierre/vscode-icons via icons.js) ----

  // Exact-filename → icon key overrides (checked before the extension map).
  const ICON_FILENAMES = {
    dockerfile: "docker", "docker-compose.yml": "docker", "docker-compose.yaml": "docker",
    makefile: "gear", gnumakefile: "gear",
    ".gitignore": "git", ".gitattributes": "git", ".gitmodules": "git", ".gitkeep": "git",
    "package.json": "npm", "package-lock.json": "npm", ".npmrc": "npm",
    "claude.md": "claude", license: "text", "license.md": "text",
    "go.mod": "go", "go.sum": "go",
  };
  // File-extension → icon key. Keys must exist in icons.js (window.GSIcons).
  const ICON_EXT = {
    go: "go", js: "js", mjs: "js", cjs: "js", jsx: "js",
    ts: "ts", mts: "ts", cts: "ts", tsx: "ts",
    json: "json", jsonc: "json", json5: "json",
    yaml: "yaml", yml: "yaml", toml: "gear", ini: "gear", cfg: "gear", conf: "gear", env: "gear",
    md: "md", markdown: "md", mdx: "md",
    html: "html", htm: "html", xhtml: "html",
    css: "css", scss: "css", sass: "css", less: "css",
    sh: "sh", bash: "sh", zsh: "sh", fish: "sh",
    py: "py", pyi: "py", pyw: "py",
    rs: "rust", rb: "ruby", erb: "ruby",
    c: "c", h: "c", cpp: "c", cc: "c", cxx: "c", hpp: "c", hh: "c",
    swift: "swift", kt: "code", kts: "code", java: "code", xml: "code",
    php: "code", cs: "code", lua: "code", proto: "code",
    sql: "sql", db: "sql", sqlite: "sql",
    svg: "svg",
    png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", ico: "image", bmp: "image",
    zip: "zip", tar: "zip", gz: "zip", tgz: "zip", bz2: "zip", xz: "zip", "7z": "zip", rar: "zip", jar: "zip",
    txt: "text", log: "text", rst: "text",
    lock: "gear",
    ttf: "font", otf: "font", woff: "font", woff2: "font", eot: "font",
  };

  // iconName maps a filename (or a structural kind) to a GSIcons key. DOM-free
  // and Node-testable. `kind` handles the tree-entry shapes: "tree" (folder),
  // "tree-open" (expanded folder), "commit" (gitlink/submodule), "symlink"; any
  // other kind falls through to filename/extension resolution, defaulting to the
  // generic "file" icon for unknown types.
  function iconName(name, kind) {
    if (kind === "tree") return "folder";
    if (kind === "tree-open") return "folder-open";
    if (kind === "commit") return "git";
    if (kind === "symlink") return "symlink";
    const base = (name || "").split("/").pop();
    const lower = base.toLowerCase();
    if (ICON_FILENAMES[lower]) return ICON_FILENAMES[lower];
    const dot = base.lastIndexOf(".");
    if (dot >= 0) {
      const ext = lower.slice(dot + 1);
      if (ICON_EXT[ext]) return ICON_EXT[ext];
    }
    return "file";
  }

  // ICON_COLOR maps a GSIcons key to a hue class whose per-theme color is
  // defined in index.html from the Pierre @pierre/vscode-icons palette (MIT,
  // github.com/pierrecomputer/vscode-icons scripts/palette.mjs): the theme's
  // fontColor per language, palette shade 400 for the dark background and 600
  // for the light parchment (the light yellow is nudged darker for parchment
  // legibility). Keys Pierre leaves monochrome (folder/folder-open, file, json,
  // md, image, zip, sql, svg, font, text, gear, code, symlink) carry no entry
  // and inherit --muted, so structural and unknown icons stay neutral.
  const ICON_COLOR = {
    go: "i-cyan", ts: "i-cyan",
    js: "i-yellow",
    html: "i-orange", swift: "i-orange", rust: "i-orange", claude: "i-orange",
    css: "i-indigo",
    sh: "i-green",
    py: "i-blue", c: "i-blue", docker: "i-blue",
    yaml: "i-red", ruby: "i-red", npm: "i-red",
    git: "i-vermilion",
  };

  // iconColorClass returns the hue class for a resolved icon key, or "".
  function iconColorClass(key) { return ICON_COLOR[key] || ""; }

  const core = {
    deriveBase, fetchBytes, fetchText, inflate, parseLooseObject, objectKey,
    getObject, parseCommit, cleanContent, parseGitmsg, resolveRef, resolveHead,
    walkHistory, startWalk, walkStep, walkedCommits, walkStateFor, refHash, parseBranchField, resolveItems,
    buildVersions, effectiveTime, effectiveAuthor, effectiveAuthorEmail,
    feedbackLine, feedbackAnchorKey, hunkLineKeys, anchorFeedback, prFeedback,
    reviewSummary, suggestionBody,
    loadExtItems, loadExtItemsWindow, loadExtItemsUpTo, findItemDeep, loadBranchLogWindow, loadBranchLogIndexed, loadCompareCommitsWindow, loadGraphWindow, orderGraphWindow, assignGraphLanes, GRAPH_WINDOW,
    loadItemsIndex, loadOlderItemShards, olderItemBytes, loadBodyIndex, extWalkState, indexCommit, metaCommit, hydrateItem, hydrateItems,
    loadTimelineItems, loadTimelineWindow, resolveCodeItems, resolveShortShaFromIndex, readRefMode, newContext,
    loadManifest, refTip, parseRoute, commitRef, compareRef, resolveCompareRef, COMMIT_VIEW, EXT_BRANCHES, WALK_CAP, DETAIL_WALK_CAP,
    parseTree, getTree, resolvePath, listBranches, listTags, compareTagsDesc, tagVersionKey, peelTag, stripSignatureBlock, headBranchName,
    parseInline, parseMarkdown, parseList, isTableSeparator, cellAlign, splitTableRow,
    splitLines, diffLines, buildHunks, diffTrees, commitTree, mergeBase, fileDiff,
    intraLine, MAX_DIFF_LINES, DIFF_TREE_SCAN_CAP,
    parseRefs, refRepoUrl, releaseAssets, stateCounts, groupThread, flattenThread,
    THREAD_MAX_DEPTH, embeddedRefs, groupPM, authorStats, iconName, iconColorClass,
    ANCESTOR_CAP, refBranch, parentRef, quotedRefFor, resolveAncestors,
    CONCURRENCY, isBinary,
    itemLabels, buildBoard, boardColumnsFrom, loadSiteConfig, loadSiteCustomization, loadInteractionCounts, loadExtItemsForCounts, COUNTS_WALK_CAP, countsFor, matchIssueColumn, PM_BOARD_COLUMNS, pmParentHash,
    SWIMLANE_FIELDS, SWIMLANE_LABELS, swimlaneValue, swimlaneOrder, groupBySwimlane, swimlaneLabel,
    buildIssueHierarchy, pmProgress, searchItems, searchItemsFaceted, parseSearchFilters, itemMatchesHash, searchableText, itemSubject, typeGlyph, loadSearchWindow, fullSearchBytes,
    SEARCH_GROUPS, hashEq,
    parseListRef, enumerateLists, listMemberRef, jsonCommitMessage,
    loadListsSummary, loadListDetail, loadExtConfig,
    forkRefNames, loadForks,
    commitsByMonth, extensionStats, latestReleaseVersion,
    loadExtItemsAll, loadSiteStats, loadAnalyticsData, activityBuckets, topItemAuthors,
  };

  Object.assign(NS, core);
  if (typeof module !== "undefined" && module.exports) module.exports = NS;
})();
