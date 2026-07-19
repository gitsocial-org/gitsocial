# Static Site

Once enabled, `gitsocial push` publishes a complete, browsable website of your repository (timeline, issues and boards, pull requests, releases, code, search, analytics) into the same S3-compatible bucket that hosts the repo, alongside the repo data — plus an optional crawlable HTML page layer (see [HTML pages](#html-pages)).

The site is plain static files read directly by the browser: no server, no build step, no dependencies, and nothing for visitors to install. Anyone with the bucket's public URL can follow the project without a forge account, and the site stays current automatically because every later `gitsocial push` refreshes the data it reads.

> Transport: [S3.md](S3.md) owns the remote helper, URL normalization, [bucket layout](S3.md#bucket-layout), ref-update modes, cache policy, and environment variables.

## Publish

The site is **off by default** and enabled per repo by the `publish` guard, stored in the site config so the decision travels with the repo:

```bash
gitsocial remote add s3://<endpoint>/<bucket>/<prefix>   # once: bucket as a remote (see S3.md)
gitsocial config site set publish true                   # master switch for the static site
gitsocial push                                           # publish repo data + the browsable website
```

`publish` gates everything the site consists of — the shell, `refs.json`, the pm/site config artifacts, the items/bodies/code indexes, and the HTML pages. Unset or false, `gitsocial push` and plain `git push` move repo data only; a bucket that already carries a site (created before the guard existed, or by another clone) is left untouched with a one-line hint naming the config to set. Because the guard lives in the pushed config ref (`refs/gitmsg/core/config`), a plain `git push` that carries it maintains — and on a fresh bucket creates — the site too.

`--no-site` skips the site step for one push, and `git config gitsocial.pushSite false` opts a machine out persistently. Both remain force-offs on top of the guard, never enablers: `site.publish=true` is the only thing that turns the site on.

`gitsocial site push [remote]` remains the **explicit site refresh**: it re-derives the whole site without pushing data, and is the "catch up now" command right after enabling the guards on an already-pushed repo — nothing is re-pushed, since every artifact derives from bucket + local state under the existing budgets. The remote defaults to the [push remote](S3.md#push-remote-resolution).

The bucket (or its public domain, e.g. r2.dev or a custom domain on Cloudflare R2) must allow public reads. Once the bucket carries the site, every subsequent push maintains the data artifacts it reads, and a push from a binary carrying a newer embedded site re-uploads the shell itself (tracked by `.gitsocial/site/version`), so the page keeps working without a manual refresh.

## HTML pages

A second guard adds a crawlable, no-JS HTML layer at the prefix root — real pages for every item, readable by any crawler or text browser and unfurling with OG cards:

```bash
gitsocial config site set pages true
gitsocial config site set url "https://example.com/"    # absolute base: canonicals, OG and the sitemap need it
```

Effective only when all three hold: `publish=true`, `pages=true`, and a valid `url`. Every push then maintains, next to `index.html`:

- `i/<shorthash>.html` — one page per top-level gitmsg item, its thread inlined: replies in timestamp order, resolved edits with an "edited" marker, tombstones for retractions, PR review-state chips and `file:line` feedback anchors, release artifact blocks. Threads cap at ~100 replies / ~200 KB with an explicit "N more replies" marker.
- `issues/ prs/ posts/ releases/ memos/` — per-type list pages: a mutable `index.html` head plus immutable sealed `<n>.html` pages (100 entries each), chained "older →". Milestones and sprints fold into `issues`.
- `index.html` — the **generated front page** (the newest items interleaved with code commits — code links into the app, so object count scales with items, never history — then the default branch's README as escaped text, ~8 KB cap). Since the entry flip (M8), the pages maintainer OWNS `index.html` while the page layer is effective; when the page layer is off, `index.html` is the embedded SPA shell instead (see [Progressive enhancement](#progressive-enhancement-the-pages-are-the-site)). The pre-flip `timeline.html` key is retired and swept on every push.
- `pages.css` — the shared stylesheet; each page also inlines a tiny base style so a saved or curl'ed copy reads decently on its own.
- `sitemap.xml` + `robots.txt` — the site root plus every item page with `<lastmod>` from its latest activity; past ~40K URLs the sitemap becomes an index over immutable `sitemap-<n>.xml` parts plus a rewritten `sitemap-head.xml`.
- `feed.xml` — an Atom 1.0 feed of the newest ~50 non-retracted top-level items (memo excluded, code commits absent — same interleave as the front page): title, canonical link, author, type category, created/updated times, plus the item's own body (subject stripped, replies excluded, ~4 KB cap) as escaped-HTML content. Every generated page's `<head>` carries the autodiscovery `<link rel="alternate" type="application/atom+xml">`. Each type directory additionally gets its own `<dir>/feed.xml` mirroring its list page (memos included), advertised by a second autodiscovery link on that type's list pages.

Pages are a projection of the push's own index artifacts (never a second git walk), rendered as escaped plain text — no markdown or highlighting; the SPA remains the rich surface. Maintenance is incremental: a reply regenerates only its thread's page, the affected list heads, the front page, the sitemap and the feed. First-time generation is budgeted (~5000 pages per push, `GITSOCIAL_SITE_PAGES_BUDGET` override) and resumes across pushes — a partial set is a valid newest-first prefix, and list pages and the sitemap claim only what exists. Setting `pages false` (or removing `url`) deletes the whole page layer on the next push and **restores the embedded SPA shell at `index.html`** (index.html is dual-owned, never deleted). Machine state lives at `.gitsocial/site/pages.json`; the page keys are part of the [reserved root namespace](S3.md#bucket-layout).

### Progressive enhancement: the pages ARE the site

Every generated page is a complete, crawlable, no-JS-readable HTML document — and the SPA is an enhancement layer that upgrades it in place. Each page references `gs-upgrade.js` with `defer` and carries three boot hooks: a `<meta name="gs-route">` route (the shell's `parseRoute` grammar), a `data-base` attribute on the `<div id="gs-page">` mount, and the mount itself. On load, `gs-upgrade.js` resolves the artifact base (the `data-base` attribute, or the `?base=`/`?repo=` cross-bucket override), injects the app chrome plus the shared `pages-app.css` (extracted from the shell's inline styles so both the shell and an upgraded page render the same look), loads `gs-core`/`gs-render`/`gs-app` relative to that base, and boots the app on the page's route. A `location.hash` deep-link WINS over the page's `gs-route` meta, so a shared `#/…` link or a timeline code-commit link opens its target on any page it lands on. Post-boot navigation `pushState`s clean page URLs for routes that have pages (items, type lists, the front page) and keeps hash routes for app-only surfaces (search, board, analytics, code, compare); reloads always work because every page URL is a real bucket object. The boot is inert on failure: the reader (`gs-core`/`gs-render`) loads before the body is ever replaced, so a broken or partial upgrade leaves the static, readable page fully intact.

**Dual-mode `index.html` ownership.** `index.html` is the generated front page while the page layer is effective, the embedded shell otherwise. `uploadSiteFiles`/`ensureSiteShell` always ship the shell first; when pages are effective, the pages pass then overwrites `index.html` with the generated front page (`rebuildSitePages` runs after `uploadSiteFiles`, and even its no-op-tips branch reclaims `index.html` — so a shell-version bump that re-ships the shell can never strand `index.html` as the shell). A shared item's copy-link/share affordance in the booted app hands out the page URL (`{site.url}i/<short>.html`) when the site config carries a valid `url` and `pages` is on, else the in-app hash URL.

## Customization

The site can be branded per repo. Values are stored in the `site` sub-object of the core config (`refs/gitmsg/core/config`) and published to the bucket as `.gitsocial/site/site-config.json` on the next push. Set them with the CLI or in the TUI under Configuration → Site:

```bash
gitsocial config site set title "My Project"
gitsocial config site set accent "#0a7"                # strict #rgb / #rrggbb hex
gitsocial config site set accentDark "#0dd"            # optional dark-mode accent
gitsocial config site set favicon @path/to/icon.png    # or a data: URI directly
gitsocial config site list
```

| Field | Validation |
|-------|------------|
| `title` | plain string, trimmed, ≤ 200 chars |
| `accent`, `accentDark` | strict `#rgb` / `#rrggbb` hex |
| `favicon` | `data:image/png|webp|svg+xml` URI, ≤ 32 KB (the CLI converts an `@path` for you) |
| `url` | absolute `https://` base URL (`http://` only for localhost), no query/fragment, trailing slash normalized, ≤ 500 chars |
| `description` | plain string, trimmed, ≤ 300 chars (front-page meta/OG description) |
| `publish`, `pages` | `true` / `false`, both default false — the [site](#publish) and [HTML page](#html-pages) guards |

Both the writer and the reader validate every field with the same rules, so a bad config never breaks the page; if nothing survives validation the artifact is deleted and the site falls back to its defaults. The `.gitsocial/site/assets/` prefix is reserved for future binary assets; nothing reads or writes it today.

## How it works

The whole site lives in `core/objstore/site/`, embedded in the binary via `SiteFiles`: `index.html`, the reader JS (`gs-core.js` / `gs-render.js` / `gs-app.js`), the page-entry boot layer `gs-upgrade.js`, and the extracted app stylesheet `pages-app.css` (linked by both the shell and the generated pages so an upgraded page renders the same look), which reads the [bucket layout](S3.md#bucket-layout) directly and re-implements gitmsg message parsing in JS. Layout or protocol changes must touch it too, and editing a file under `core/objstore/site/` requires rebuilding the binary before `site push`. `SiteFiles` ships every file under `site/` recursively (subdirectories included), so `uploadSiteFiles` publishes the whole shell — including the syntax-highlighting grammars under `site/grammars/`.

Syntax highlighting is Prism (`prism.js` bundles the common grammars: go/js/ts/json/yaml/bash/markdown/markup/css/diff). Every other language ships as its own `site/grammars/prism-<lang>.js` file (regenerate with `scripts_gen_grammars.sh` from the vendored `prismcomp/` 1.30.0 build); the reader lazy-loads one on first use (`ensureGrammar` in `gs-render.js`), loading any dependency chain first (e.g. `cpp`→`c`), caching per session, and upgrading the already-rendered plain text in place. A code block renders un-highlighted immediately and never blocks on the fetch; a missing grammar file stays plain text. This replaces the old push-time tree scan that published a per-repo `prism-extra.js` bundle — `site push` (and any shell re-upload) now deletes that obsolete key best-effort.

Pushes maintain a set of read-optimized data artifacts under `.gitsocial/site/`: `refs.json` (ref discovery without bucket listing), and per-extension items/bodies indexes in an append-only sharded layout (immutable brotli-compressed shards plus a small mutable head and manifest). Items shards carry metadata and subjects only; full message bodies live in a separate corpus loaded on demand. A single **code items index** (`.gitsocial/site/items/code/`) mirrors that layout for plain (non-gitmsg) commits: one merged, deduped corpus across every code branch, each entry attributed to a branch (the default branch when the commit is reachable from it, else the first code branch that reached it), so the timeline sources its interleaved code commits from the index instead of walking loose objects. The code corpus is **metadata-only — no bodies corpus**: a code card shows subject + author/time/hash, and the full commit message is only needed on the detail view (which hydrates the loose object), so bodies would bloat the bucket for content the timeline never renders. Maintenance is incremental: a push appends the new commits, repairs an interrupted write, or, on very large branches, bootstraps the index over multiple pushes (a partial index is always a valid newest-first prefix, so the timeline works from the first push). A code-branch force-push/rebase that drops indexed commits is repaired by rebuilding the affected shards (the corpus is defined as "reachable from any current code tip"). Artifacts carry a format version (gitmsg items/bodies: 4; the code corpus: 5, whose entries also carry each commit's parent shas so the repository graph renders its DAG from the index). A reader that sees an unknown version — or no code index at all (an older bucket) — ignores the artifacts and falls back to a bounded loose-object walk; a push onto a v4-code-corpus bucket re-seals the code index at v5 (the schema version salts the shard keys).

Almost every surface is served from the index, never a per-object history walk: the timeline, the default-branch log, all the list tabs (issues/board/milestones/sprints/prs/releases/memos), search (gitmsg items plus plain code commits — code matches at subject/author level, mirroring the core search's coverage; the code corpus deliberately carries no message bodies), item and merged-PR detail (a merged PR's merge-base/merge-head short shas resolve by prefix over the code index rather than a base-branch walk), analytics, and the commit graph (the v5 code corpus carries parent shas, so the DAG renders from the index; on an indexed bucket the graph covers code branches only — `gitmsg/*` data branches appear only under the loose-walk fallback for pre-v5 buckets) all read the index slice plus the rendered slice. Graph rows are decorated `git log --decorate` style from already-fetched data: live branch-tip chips (the default branch marked), lightweight-tag chips from `refs.json` (annotated tags point at their tag object, which never matches a row — no peel fetches), and dimmed historical chips for merged-PR head branches (recovered from merged-PR headers in the review index's eager set, linked to the PR detail; branches merged without a PR stay unlabeled). Only the surfaces that need topology the index cannot answer walk loose objects deliberately, bounded: compare and a non-default-branch log (index attribution can't answer reachability off the default line), plus code/file browsing (tree objects are an inherent per-navigation cost). A request-budget test suite (`sitetest/verify_request_budget.js`) pins a per-route fetch ceiling on the fully-indexed fixture so a regression into a walk fails CI.

Visitor cost stays flat as history grows: opening the site downloads the fixed 130 KB shell plus the newest slice of the index, under half a megabyte even at 100K commits. Everything else loads on demand; the timeline fetches 50 items at a time as you scroll, deeper search shows its download size before fetching (about 2.4 MB of full-text at 100K commits), and loads are guarded (progress checks, a boot watchdog) so a broken or partial bucket surfaces an error instead of an eternal "Loading…". On the bucket, collaboration data costs about 1 KB per message, and code roughly 2-7x its packed clone size (objects are stored individually, without packfile delta compression); none of that reaches the visitor. A 100K-commit index bootstraps over two `site push` runs, and the timeline is already servable after the first.

## Local development & testing

The site can be served by the disk-backed local S3 server used for the transport (`library/core/objstore/locals3`; see [S3.md § Local development](S3.md#local-development)), so a locally built site is browsable exactly as it would be from a real bucket.

`library/core/objstore/sitetest` is the headless test harness: `fixture.sh` builds a fixture bucket (guards enabled; thread-demo also carries the HTML page layer), `serve.js` serves it with real bucket cache/`Content-Encoding` headers, and `runner.js` drives the browser-side suites (writer/reader parity, interrupted and partial-bootstrap pushes, the feature verifiers under `verify_*.js`, and the HTML page layer via `verify_html_pages.js`). The `GITSOCIAL_SITE_SHARD_COUNT` / `GITSOCIAL_SITE_WALK_BUDGET` / `GITSOCIAL_SITE_PAGES_BUDGET` / `GITSOCIAL_SITE_SITEMAP_PART` overrides shrink shard sizes, walk/page budgets and the sitemap part size so bootstrap and sharding paths are exercised on small fixtures.
