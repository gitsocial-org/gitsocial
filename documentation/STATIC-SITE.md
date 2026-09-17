# Static Site

`gitsocial mirror` and `gitsocial push` build a browsable website of the repository in a bucket, as static files the browser reads directly: timeline, issues and boards, pull requests, releases, code, search, analytics. [STATIC-SITE-DESIGN.md](STATIC-SITE-DESIGN.md) says what that website looks like.

[Mirror](#mirror) · [Publish](#publish) · [Customization](#customization) · [HTML pages](#html-pages) · [Testing](#testing) · [Reference](#reference)

## Mirror

For a project hosted on a forge, one command clones it, imports its issues, pull requests, releases and discussions, pushes data and code, and builds the site:

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix> --url https://your-domain/
gitsocial mirror                       # later, from the workspace: refresh
```

- `--url` is the site's public address and turns the [HTML pages](#html-pages) on.
- The remaining flags, the refresh behavior and the provider checklist are in [CLI.md](CLI.md#gitsocial-mirror).

## Publish

```bash
gitsocial remote add s3://<endpoint>/<bucket>/<prefix>   # once, see S3.md
gitsocial config site set publish true
gitsocial push
```

- `publish` is off by default. It lives in the pushed config ref, so a plain `git push` that carries it rebuilds the site too.
- `--no-site` skips the rebuild for one push. `git config gitsocial.pushSite false` opts a machine out. Neither turns the site on.
- `gitsocial push --site-only [remote...]` rebuilds the site and sends no refs. It fails when `publish` is off or the remote is not s3.
- The site needs public reads on the bucket or the domain in front of it. A private bucket still works as a remote, without a site.
- A rebuild from a newer binary re-uploads the [shell](#shell), the app's own files.
- A [thin fork bucket](S3.md#thin-fork-buckets) gets no site. `gitsocial push --full` detaches it and the site returns.

## Customization

```bash
gitsocial config site set <key> <value>    # or in the TUI under Configuration, Site
gitsocial config site list
```

Values live in the `site` object of the core config ref and reach the bucket as `.gitsocial/site/site-config.json` on the next push. A value that fails validation falls back to its default. The last four keys belong to the [HTML pages](#html-pages).

| Key | Example | Validation |
|---|---|---|
| `title` | `"My Project"` | plain string, trimmed, up to 200 characters |
| `description` | `"One sentence for the front page"` | plain string, trimmed, up to 300 characters |
| `accent`, `accentDark` | `"#0a7"` | `#rgb` or `#rrggbb` |
| `favicon` | `@icon.png` | `data:image/png`, `webp` or `svg+xml` URI, up to 32 KB; `@path` is converted |
| `image` | `og-card.png` | `og:image` for every page: a key relative to the site root (upload with `gitsocial remote put`) or an absolute `https://` URL, up to 500 characters |
| `url` | `https://example.com/` | absolute `https://` base (`http://` for localhost only), no query or fragment, up to 500 characters |
| `publish`, `pages` | `true` | `true` or `false`, both default false |
| `filesInclude`, `filesExclude` | `"internal/**"` | comma-separated repo-relative globs; `**` spans segments; a leading `/` or a `..` segment is dropped |

### Per-remote overrides

Only `url`, `publish` and `pages` can differ per remote; the other keys travel with the repo. An override lives in local git config as `remote.<name>.gitsocial-site-<key>`, rebuilds that remote's site in full on the next push, and applies to pushes by remote name, not by URL.

```bash
gitsocial config site set publish false --remote backup
gitsocial config site list --remote backup     # effective values
```

## HTML pages

Crawlable pages that read without JS: one per item, plus lists, commits, files, a front page, a sitemap and feeds. With JS on, a page boots into the app in place.

```bash
gitsocial config site set pages true
gitsocial config site set url "https://example.com/"   # absolute base for canonicals, OG tags and the sitemap
```

Effective when `publish`, `pages` and a valid `url` are all set. Every rebuild then maintains the [page keys](#page-keys):

- An item page inlines its thread, capped at about 100 replies or 200 KB.
- A list page holds 100 entries: a mutable `index.html` head and sealed `<n>.html` pages, each linking to the older one. Page 1 is the oldest. The sealed page that was newest when it sealed keeps its `← newer` link to the head after it stops being newest, and the head's `older →` chain reaches every page, so a crawler walking either direction lands on a real document. Milestones and sprints fold into `issues`.
- `index.html` is dual-owned: the page layer holds it whenever the layer is effective, the shell otherwise. Every effective rebuild reclaims it, since the same rebuild's shell upload may have written over it.
- The commits list covers the default branch, one row per commit, no diffs, no per-commit page. Each row has an id, so `commits/<n>.html#c-<sha12>` is a citable URL. Its rows come from the code index, not a second git walk, so the list, the timeline and the front page's activity agree on which commits belong to the branch.
- Sealed commits pages are re-derived after a rebase or force-push. `gitmsg/*` branches are append-only by protocol; the default branch is not.
- Every pass re-locates the recorded frontier in the current list before sealing onward. The sha must still be there, with the same number of rows below it. A sha commits to its ancestry, so a frontier still present proves the sealed region intact, and the row count catches a re-attribution that inserted rows beneath it. Either check failing re-derives the chain.
- Two contracts follow. `commits/` gets no Atom feed, since the code corpus carries no bodies to syndicate. A sealed commits page is `no-cache` rather than immutable, since a re-derived page has to be re-fetched.
- The commits pagination is published in the pages manifest and read back by both sides, so the app's `/commits` route renders the same rows the generated page shows and the boot swap moves nothing.
- A file page renders one prose document on the default branch at `f/<path>.html` (`f/specs/GITMSG.html`). A document qualifies when:
  - it is markdown (`.md`, `.markdown`, `.mdown`, `.mdx`) or an extensionless convention document (LICENSE, NOTICE, AUTHORS, CONTRIBUTING, CHANGELOG and their siblings, shown preformatted);
  - it is not under a dotdir, `node_modules/`, `vendor/`, `testdata/`, `third_party/`, `fixtures/`, `golden/`, `corpus/`, `snapshots/` or `__snapshots__/`, and is not a submodule, a symlink or the root README;
  - its path has no space and none of `@ : # ? %`.
- File pages follow the tree: a document that leaves it loses its page, one under 100 words stays out of the sitemap, and any renders whole up to 256 KB. `filesInclude` and `filesExclude` override the selection rule.
- A file page's key mirrors the repo path with the extension swapped for `.html`, which is a one-to-one map onto the `file:<path>@<branch>` route the page stamps as its boot hook. Two documents that would claim one key put the second at `<path>.html`.
- Discovery is the one place the page layer reads a git tree instead of the rebuild's own index artifacts, since those carry commits and not files. It walks the default branch from the pusher's local odb. A tree it cannot read carries the published set forward rather than reading as an empty repo.
- A changed default branch rewrites every page, since the branch is in each page's route and meta line. Each page's date comes from one history walk over the tree, not a `git log` per path.
- The front page: the site description, the default branch and its tip commit, the root file listing, the README rendered from up to 8 KB of source, and the newest 10 entries across items and code commits. Item rows link to their pages; commit rows link into the app.
- `sitemap.xml` lists the front page, every indexable item page, non-empty list pages, the commits pages and the file pages, each with `lastmod`. Not listed: retracted items, empty lists, file pages under the word floor.
- `feed.xml` is Atom 1.0 with the newest 50 non-retracted top-level items, memos excluded. Each type directory has its own `feed.xml`.

The README is rendered with the app's own markdown grammar (`site_markdown.go` is a port of it), so the page and the app agree block for block; raw HTML is rebuilt against an allowlist. Two differences from the app: repo-relative images degrade to their alt text, since a bucket serves objects by sha and has no src for a path, and in-page anchors are rewritten to the rendered heading ids so a README's own table of contents works with no script running. Every emitted image is `loading="lazy"`, which keeps a hero image out of the preload scanner's way while the shell loads. Sanitizer output is balanced, so a README the size cap cut mid-structure still yields well-formed HTML.

Rules that hold on every page:

- Every `<title>` is unique (a shared subject gets the item's date, then its short ref) and every `<meta name="description">` is prose with the markdown syntax stripped.
- Retraction tombstones and file pages under the word floor carry `noindex,follow`. The page stays, so existing links keep working.
- Item bodies render as escaped plain text. Only the README and file pages, the bucket owner's own content, go through the markdown renderer, and they are the one typed value a page body carries. Everything else on a page is context-escaped by `html/template`; the other typed value is the favicon href, which is either the shell's own constant or a data URI already narrowed to an image type.
- The markdown renderer builds every tag itself and escapes every text node, attribute value and code body. Raw HTML in the source is lexed and rebuilt against an allowlist that admits no event handler, no `style`, no `script`, `iframe` or `object`, and no image or link target that is not an absolute `https:`, `mailto:`, in-page or app reference.
- `f/index.html` is the one list page that boots into another route, the tree view.
- A first line promoted into a subject or a label is markdown-stripped first, by `siteSubjectText` in Go and its mirror `subjectText` in `gs-core.js`, pinned by `sitetest/parity_fixtures.json`. A subject that strips to nothing falls back to a placeholder, because a row's subject anchor is its only link to the item. What renders as nothing upstream is dropped before the first line is taken: HTML comments, and link reference definitions at a block start outside fenced code, which is where a bot hides its state in an imported comment body.
- The page layer and the app render the same extensions as prose. An `.mdx` document loses its import, export and standalone JSX lines first, by `siteFileStripMDX` in Go and its mirror `stripMDX` in `gs-core.js`, pinned by `sitetest/parity_fixtures.json`.
- The page's thread carries review feedback that the app routes into its review and diff sections instead, so the two counts differ on a pull request page.
- First-time generation runs item pages, then file pages, then commits pages. `GITSOCIAL_SITE_PAGES_BUDGET` caps the item pages one rebuild writes ([S3.md](S3.md#environment-variables)); the rest resume on the next push. The cap is unset by default.
- Setting `pages false` or removing `url` deletes the page layer on the next push and restores the shell at `index.html`.

## Testing

```bash
scripts/site-test.sh                                              # the browser battery
go test -tags sitetest -timeout 30m ./library/core/site/      # the same from go test; skipped without node
bin/locals3 -root <dir>                                           # serve a pushed site locally, see S3.md
```

The harness is `library/core/site/sitetest/`: `fixture.sh` and `shapes.sh` build the fixture buckets, `serve.js` serves them with real cache headers and `Range` support, `runner.js` runs the suites and `shots.js` takes the screenshots. Fixture-size overrides are in [S3.md](S3.md#environment-variables). The battery runs at release; nothing in `go test ./...` covers the browser side.

Every suite but one runs under a DOM shim that computes no styles. `verify_styles.js` is the exception: it drives a real Chrome, reads computed styles and child structure for a fixed selector list on ten routes in both themes, and compares them to the baselines in `sitetest/styles/`.

Chrome resolves through `chrome.js`: a `CHROME` override, then a candidate list of absolute paths. A bare name on PATH is not accepted. The suite skips with a notice when there is no Chrome, and `release.sh` preflights the same resolver so a release cannot ship with the gate skipped.

```bash
GS_STYLES_UPDATE=1 node library/core/site/sitetest/verify_styles.js   # recapture the baselines
```

A baseline records the distinct variants a selector renders, not whichever element is first, because a fixture rebuild reorders lists. Type classes are dropped from the structure fingerprint for the same reason; their tints still show as colours on their own variants.

### Repo-shape goldens

`shapes.sh` builds six fixture buckets, `shots.js` screenshots each fixture's routes at 1280 and 390 px in both themes, and `TestSiteShapeGoldens` compares every shot to `sitetest/goldens/`. It runs under the `sitetest` tag and skips without Chrome.

| Fixture | Shape |
|---|---|
| `src-repo` | a source tree on the `trunk` default branch, no docs directory, `.txt` files under fixture directories |
| `docs-repo` | an `.mdx` documentation site |
| `empty-repo` | one commit, no README, no extension |
| `code-only-repo` | code, branches and tags, no `gitmsg/*` branches |
| `big-tree-repo` | 6,000 generated files, 5,900 of them in one directory |
| `binary-repo` | an image, a binary blob, an LFS pointer, a submodule and a symlink |

Every fixture commit takes a date off one fixed clock and `shots.js` pins `Date.now()` through the server's `?now=`, so a golden's shas, dates and relative times are the same on every run. A `-nojs` golden is the served document with its scripts stripped. The shapes the fixtures stand for are in [STATIC-SITE-DESIGN.md](STATIC-SITE-DESIGN.md#repo-shape-rules).

```bash
go test -tags sitetest -run TestSiteShapeGoldens ./library/core/site/ -update   # regenerate the goldens
```

## Reference

### Shell

`core/site/assets/`, embedded in the binary and uploaded whenever the shell version changes:

| File | Role |
|---|---|
| `index.html` | the app shell, and the front page while the page layer is off |
| `gs-core.js`, `gs-render.js`, `gs-app.js` | the app |
| `gs-upgrade.js` | boots the app from a generated page; a failed boot restores the static page |
| `pages-core.css` | tokens, theme gates, reset, page structure; inlined into every generated page |
| `pages-full.css` | the component vocabulary and the webfonts |
| `prism.js`, `grammars/` | the base grammars and 47 lazy-loaded ones |
| `icons.js`, `fonts/` | file-type icons; EB Garamond and IBM Plex Mono |

`.js`, `.css`, `.html` and `.json` under `site/` upload brotli-compressed with `Content-Encoding: br`. Generated HTML, the sitemap, `robots.txt` and the feeds upload plain. Git objects carry no encoding.

A generated page inlines `pages-core.css` into its head, comments stripped, and links `pages-full.css` behind a preload that flips to a stylesheet on load, with a `noscript` fallback. First paint is the HTML plus the inlined bytes, and the component styling arrives after. The same two files govern the app, so the page and the booted app cannot drift.

Two rules follow. `pages-core.css` carries no `url()`, since pages sit at several directory depths where a relative URL resolves against the page. A change to it bumps `sitePagesVersion`, since every page's head carries a copy.

Type sizes, colours and spacing come from the [tokens](STATIC-SITE-DESIGN.md#tokens) `pages-core.css` declares.

A configured accent is site data, not part of the sheet. It is stamped per push as a small `:root` override after the inlined core, so the embedded sheet and the shell version hash stay the binary's own identity.

A grammar is chosen by file extension, then basename (`Dockerfile`, `Makefile`, `CMakeLists.txt`), then a fence's info string, with `markup` as the fallback. Code renders plain first and highlights when the grammar arrives.

### Page keys

At the prefix root, next to the shell; the cache classes are defined in [S3.md](S3.md#cache-policy):

| Key | Content | Cache |
|---|---|---|
| `index.html` | front page while the page layer is on, else the shell | no-cache |
| `i/<short>.html` | one page per top-level item, thread inlined | no-cache |
| `issues/`, `prs/`, `posts/`, `releases/`, `memos/` | `index.html` head plus sealed `<n>.html`, 100 entries each | head no-cache, sealed immutable |
| `commits/index.html`, `commits/<n>.html` | default-branch commit list, 100 rows each | no-cache |
| `f/<path>.html`, `f/index.html` | file pages and their list | no-cache |
| `sitemap.xml`, `sitemap-head.xml`, `sitemap-<n>.xml` | sitemap; an index over parts past about 40,000 URLs | head no-cache, parts immutable |
| `robots.txt` | `Allow: /` and the sitemap location | no-cache |
| `feed.xml`, `<dir>/feed.xml` | Atom feeds, 50 entries | no-cache |

Item pages and sealed list pages are rewritten only when `sitePagesVersion` in `site_pages.go` changes, so a change to their head or markup bumps it. Everything else is rewritten on every push. A manifest at any other version reads as absent, so a bump regenerates every page.

Each bump gets a row here, newest first:

| Version | What it rewrote |
|---|---|
| 28 | the release row's asset count in place of its hash, and a typeless item's glyph class |
| 27 | the head chips on every row, the edited marker on a list row, and the sidebar title |
| 26 | the meta row, the empty sentence, the truncation notices, and the copy of `pages-core.css` in every head |

### Page entry

A generated page hands the app three hooks: a `<meta name="gs-route">` route, a `data-base` attribute on the `<div id="gs-page">` mount, and the mount itself. `gs-upgrade.js` reads them, loads the shell relative to that base and lets `gs-app.js` render.

- The page's own head script marks `<html>` with `gs-boot` before the body is parsed, so a visitor with JS starts on a loading line rather than on content that is about to change. It also stamps the theme the app stored in `localStorage`, so a visitor who chose one gets the page in it with no flip at boot.
- The front page is the exception: its README is pre-rendered, so the served document is already what the home route shows. Cloaking it would trade a finished page for a loading line and a shell download. A fragment naming a different route cloaks as every other page does.
- The mark undoes itself on the load event, and on a 10 s timer for a document whose load event does not fire. Both defer to the flag `gs-upgrade.js` sets when it takes ownership, so an upgrade that fails to parse still leaves a readable page.
- The takeover reveals twice: the app's chrome once `pages-full.css` governs the page, the content once the first view has settled. A page entered without a deep link keeps its served content until then.
- A `location.hash` deep link wins over the page's own route when the fragment names a route. A bare in-page anchor addresses the page in hand, so the page's own route boots and the browser keeps the anchor.
- Every step of the takeover is reversible. A shell asset that 404s or hangs, a throw during boot, or a route that does not settle restores the served page, its styling and its entry URL.
- The shell-asset phase is bounded at 10 s, the app's first route at 35 s.
- After boot a route with a page of its own gets that page URL, so a reload hits the object. App-only surfaces normalize to `index.html#<route>`. A `?base=` or `?repo=` override survives every rewrite.

### Artifacts

Under `.gitsocial/site/`, read by the app in place of object walks, together with the bucket's ref list at `.gitsocial/refs.json` (see [S3.md](S3.md#keys)); a refname that list omits is probed live once per session:

| Key | Content |
|---|---|
| `version` | shell version marker, a hash of the raw assets |
| `items/<ext>/` | per-extension metadata index: immutable brotli shards, a mutable head, a manifest; format 4 |
| `bodies/<ext>/` | message bodies, loaded on demand; format 4 |
| `items/code/` | one deduped index of plain commits across code branches, with parent shas; format 5; no bodies |
| `pages.json` | page-layer manifest: schema version, consumed tips, bootstrap cursor, list and commits partitions |
| `site-config.json` | the customization values |
| `pm-config.json` | the resolved PM board |
| `stats.json` | a small stats blob written by the CLI from the workdir |
| `push-state` | skip digest for push-time site maintenance |

A first view stays under half a megabyte at 100,000 commits: the shell is about 150 KB, the timeline loads 50 items per scroll, and deep search states its download size before fetching. On a large repository the index bootstraps over several pushes.

### Index maintenance

Each extension keeps two corpora: `items/<ext>/` for metadata and `bodies/<ext>/` for the searchable message text. Both are append-only and split oldest-first into fixed groups. A full group seals into a content-hash-keyed shard, the trailing group is the mutable head, and a manifest lists the shards, the head, the branch tip at write time and the corpus's compressed size. A sealed shard's membership does not change under append, so its key is stable and a rebuild re-uploads nothing. Both corpora are built from the bucket's own objects, which are uploaded before any ref moves, so an artifact cannot name a commit a reader is unable to resolve.

One rebuild writes both corpora in a pinned order: bodies shards, items shards, bodies head, items head, bodies manifest, items manifest, and the cursor last. Manifests are the only commit points, so an interruption leaves at worst bodies ahead of items. A document at any other schema version reads as absent, and the reader falls back to a bounded object walk until a rebuild rewrites it.

A rebuild classifies the state from both manifests and both live head counts, then takes one action:

| Action | State | Work |
|---|---|---|
| no-op | both corpora at the pushed tip, head counts matching | none |
| append | both lockstepped at a common tip below the pushed tip | walk the bounded gap, extend both heads |
| repair | an items manifest is present and anything mismatches | rebuild each corpus from its own sealed shards plus a bounded tail re-walk |
| bootstrap | no items manifest | seal the newest budget segment, and leave a cursor when the budget is hit |
| backfill | a cursor is pending and the newest end is at the pushed tip | seal the next older segment and prepend it to both manifests |

- Once an items manifest exists there is no path back to a from-scratch capped walk. The one reset is a manifest tip unreachable from the pushed tip, which means history was rewritten under the artifacts.
- A bootstrap is in flight when a cursor is pending or the items manifest is marked incomplete. The two read the same way, so a lost cursor write cannot freeze the index; backfill reconstructs the cursor from the manifest's oldest sealed shard.
- Append owns the newest end and backfill the oldest, so the two do not overlap. The backfill frontier is the manifest's oldest sealed sha, not the cursor's lagging copy of it.
- A backfill walk stops at every already-indexed boundary, not just the frontier, so a merge parent reachable from two sides cannot land in two shards.
- The walk budget is a per-push cap, not a branch size, so progress reports a plain count rather than a percentage.
- A schema version past 4 salts a shard's content hash, so a version tick yields new keys instead of trusting a stale-schema object.

`items/code/` is one corpus across every code branch, with no bodies: a code card shows the subject, author, time and hash, and the detail view hydrates the object. It runs the same machine with four differences.

- Its tip is a digest over the sorted branch tips, so any branch move, addition or deletion reads as a changed tip.
- Each entry carries an attributed branch: the default branch wherever the commit is reachable from it, else the first branch in default-first name order whose walk reached it. That is the reader's own rule, so switching the timeline to the index moves no card. Entries also carry parent shas, which is how the repository graph renders without a per-commit walk.
- A commit carrying a `GitMsg:` header is walked for reachability and left out of the corpus, as the reader filters it.
- A changed corpus tip takes the repair path rather than a gap append: membership is "reachable from any current tip", so a force-push can shrink it inside a sealed shard. With no bootstrap in flight the repair re-walks every tip and reseals; content-hash keying leaves unchanged shards untouched and drops the stale ones from the manifest. Backfilled parents inherit the frontier commit's own branch.

### Push-time maintenance

Every ref-moving push to an s3 remote runs the site rebuild as its share of the upkeep pass ([S3.md](S3.md#push-maintenance)).

- The transport hands that share to a hook, `site.PostPushMaintenance`, which the CLI supplies to the remote helper. A binary that wires no hook sends the refs and leaves the rebuild to the next `gitsocial push`.

- `.gitsocial/site/push-state` records the last full pass: the shell version, a digest over the `refs/` listing etags plus HEAD's, and the page layer's state. A matching marker skips the data-derived pass in two or three round trips.
- The marker is stamped only at the end of a full pass, so a stale, missing or unreadable marker costs extra work rather than a skip. A per-remote override folds into the digest, since it moves no ref.
- The marker is withheld while an index bootstrap or the page layer still owes work no ref move signals, and when a shell upload's reclaim of `index.html` failed.
- The site walks read their commits from the pusher's local odb through one long-lived `git cat-file --batch`, not one bucket GET per commit. A push uploads objects before it moves refs and git objects are content-addressed, so a commit reachable from a bucket ref tip holds the same bytes in both stores. A local miss falls back to the bucket for that one object.

### Object reads

The app reads git objects out of the bucket itself.

- A packed commit or tag is located through the pack map at `.gitsocial/packmap/<xx>.json`, which names its byte range. One ranged GET returns a self-contained stream, since the commits pack is written at `--depth=0`.
- Trees and blobs have no map entry. Their pack index is range-read through its fanout rather than downloaded whole.
- The map covers commits and tags alone: a shard holds one 256th of every packed object, and every push rewrites all 256.
- Pack indexes, map shards and pack data windows are cached for the session. An object a bucket stores loose is read loose.
