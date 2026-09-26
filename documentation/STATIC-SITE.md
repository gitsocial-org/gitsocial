# Static Site

`gitsocial mirror` and `gitsocial push` build a browsable website of the repository in a bucket, as static files the browser reads directly: timeline, issues and boards, pull requests, releases, code, search, analytics.

[Mirror](#mirror) · [Publish](#publish) · [Customization](#customization) · [HTML pages](#html-pages) · [Design](#design) · [Testing](#testing) · [Reference](#reference)

## Mirror

For a project hosted on a forge, one command clones it, imports its issues, pull requests, releases and discussions, pushes data and code, and builds the site:

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix> --url https://your-domain/
gitsocial mirror                       # later, from the workspace: refresh
```

- `--url` is the site's public URL and turns the [HTML pages](#html-pages) on.
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
- A rebuild from a newer binary re-uploads the shell, the app's own files.
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
| `description` | `"One sentence for search results"` | plain string, trimmed, up to 300 characters |
| `accent`, `accentDark` | `"#0a7"` | `#rgb` or `#rrggbb` |
| `favicon` | `favicon.png` | shown beside the sidebar title: a key relative to the site root (upload with `gitsocial remote put`) or an absolute `https://` URL, up to 500 characters |
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

Crawlable pages that read without JS. With JS on, a page boots into the app in place.

```bash
gitsocial config site set pages true
gitsocial config site set url "https://example.com/"   # absolute base for canonicals, OG tags and the sitemap
```

Effective when `publish`, `pages` and a valid `url` are all set. Every push then maintains:

- One page per top-level item, its thread inlined up to a cap.
- A list page per type, paged oldest-first with `older →` and `← newer` links. Milestones and sprints fold into `issues`.
- The commits list of the default branch, one row per commit with no diffs. A row is citable as `commits/<n>.html#c-<sha12>`.
- A file page for each prose document on the default branch, at `f/<path>.html`: markdown (`.md`, `.markdown`, `.mdown`, `.mdx`) and the extensionless convention documents (LICENSE, CONTRIBUTING, CHANGELOG and their siblings). Documents under a dotdir or a vendored, test or fixture directory are left out, and so are submodules, symlinks, the root README and paths with a space or one of `@ : # ? %`. `filesInclude` and `filesExclude` override the selection.
- The front page: one section with the branch, the latest commit and the root files, two of them shown and the rest behind a chevron, then the README.
- `sitemap.xml` with every indexable page, and Atom feeds: `feed.xml` and one per type directory, memos and commits excluded.

What a page shows:

- Item bodies render as escaped plain text. Only the README and file pages, the bucket owner's own content, render as markdown, with raw HTML rebuilt against an allowlist.
- A repo-relative image in the README degrades to its alt text, since a bucket has no path to serve it from.
- Retraction tombstones and very short file pages carry `noindex,follow`, so links to them keep working.
- Every `<title>` is unique, and every description is prose with the markdown stripped.

The first push on a large repository can take several pushes to write every page; `GITSOCIAL_SITE_PAGES_BUDGET` caps the item pages one push writes ([S3.md](S3.md#environment-variables)). Setting `pages false` or removing `url` deletes the pages on the next push.

## Design

The site is the app shell, the pre-rendered pages and the two stylesheets. Five rules hold across all three.

1. A document first, an app second. Every page reads without JS. The app adds to what the reader sees and never replaces it under them.
2. One vocabulary. The page layer and the app render a component from the same class names and the same tokens. Two markups for one component is a defect.
3. Content sets the tone. Body text is serif; chrome, code and metadata are small mono. Nothing is decorative.
4. Every rule holds on any repository: no README, no issues, a tree of thousands of files, an `.mdx` documentation site.
5. Fail in place. A missing asset or a failed fetch shows a one-line notice where the content would be, never a blank and never an endless spinner.

The tokens are declared once in `pages-core.css`; `site_pages_tokens_test.go` fails on a spacing value off the scale or a token declared elsewhere. `sitetest/parity_fixtures.json` pins the markup and wording the page layer and the app share. A visual change ships with its golden and baseline update in the same commit.

## Testing

```bash
scripts/site-test.sh                                              # the browser battery
go test -tags sitetest -timeout 30m ./library/core/site/      # the same from go test; skipped without node
bin/locals3 -root <dir>                                           # serve a pushed site locally, see S3.md
GS_STYLES_UPDATE=1 node library/core/site/sitetest/verify_styles.js   # recapture the style baselines
go test -tags sitetest -run TestSiteShapeGoldens ./library/core/site/ -update   # regenerate the goldens
```

- The harness is `library/core/site/sitetest/`. The battery runs at release; nothing in `go test ./...` covers the browser side.
- Every suite but `verify_styles.js` runs under a DOM shim. That one drives a real Chrome, compares computed styles and structure on ten routes in both themes to `sitetest/styles/`, and checks that the main routes fit a 390 px viewport.
- `shapes.sh` builds six repo-shape fixtures (a non-`main` default branch, an `.mdx` docs site, an empty repository, code only, a 6,000-file tree, binary and LFS objects), and `TestSiteShapeGoldens` compares their screenshots at 1280 and 390 px to `sitetest/goldens/`.
- Chrome resolves through `chrome.js`, a `CHROME` override or a list of absolute paths. Without Chrome the style suite and the goldens skip; `release.sh` preflights the resolver so a release cannot skip them.

## Reference

### Page keys

At the prefix root, next to the shell; the cache classes are defined in [S3.md](S3.md#cache-policy):

| Key | Content | Cache |
|---|---|---|
| `index.html` | front page while the page layer is on, else the shell | no-cache |
| `i/<short>.html` | one page per top-level item | no-cache |
| `issues/`, `prs/`, `posts/`, `releases/`, `memos/` | `index.html` head plus sealed `<n>.html` | head no-cache, sealed immutable |
| `commits/index.html`, `commits/<n>.html` | the default-branch commit list | no-cache |
| `f/<path>.html`, `f/index.html` | file pages and their list | no-cache |
| `sitemap.xml`, `sitemap-head.xml`, `sitemap-<n>.xml` | the sitemap, an index over parts when it is large | head no-cache, parts immutable |
| `robots.txt` | `Allow: /` and the sitemap location | no-cache |
| `feed.xml`, `<dir>/feed.xml` | Atom feeds | no-cache |

Item pages and sealed list pages are rewritten only when `sitePagesVersion` in `site_pages.go` changes; the commit that bumps it says why. Everything else is rewritten on every push.

### Artifacts

Under `.gitsocial/site/`, read by the app in place of object walks, together with the bucket's ref list at `.gitsocial/refs.json` ([S3.md](S3.md#keys)):

| Key | Content |
|---|---|
| `version` | shell version marker, a hash of the raw assets |
| `items/<ext>/` | per-extension metadata index: sealed shards, a mutable head, a manifest; an adopted copy's entry also names its original author |
| `bodies/<ext>/` | message bodies, loaded on demand |
| `items/code/` | one index of plain commits across code branches, with parent shas and no bodies |
| `pages.json` | page-layer manifest: schema version, consumed tips, bootstrap cursor, list and commits partitions, the sidebar's item counts |
| `site-config.json` | the customization values |
| `pm-config.json` | the resolved PM board |
| `stats.json` | a stats blob written by the CLI from the workdir |
| `push-state` | skip digest for push-time maintenance |

### Invariants

The shell and the pages:

- `pages-core.css` is inlined into every page head, so it carries no `url()` and a change to it bumps `sitePagesVersion`. `pages-full.css` loads after first paint. The same two sheets govern the app, so the page and the booted app cannot drift.
- A configured accent is stamped per push as a `:root` override after the inlined core, so the shell version comes from the binary alone.
- `index.html` has two owners: the page layer while it is effective, the shell otherwise. Every effective rebuild reclaims it.
- The page layer and the app render markdown with one grammar, ported between JS and Go and asserted equal.
- An adopted copy (GITMSG.md §1.5) shows its original author and the repository it came from on both renderers. A cross-repository edit is a proposal, so neither lists it.
- File discovery reads the default branch's tree from the pusher's local odb. A tree it cannot read carries the published set forward.
- A changed default branch rewrites every page.
- The sidebar counts are the app's alone. Branches and tags come from `refs.json`; commits and the item counts come from `pages.json`, which carries the item counts only after a complete pass. An item count is the open work, the rows its list opens on: Issues, Pull Requests, Milestones and Sprints open on their Open filter. A generated page's sidebar carries no count, since a sealed page is not rewritten when a count moves.

Commits pages:

- Rows come from the code index, not a second git walk, so the list, the timeline and the front page agree. The pagination is in `pages.json`, so the app's `/commits` route renders the rows the page shows.
- The default branch is not append-only, so sealed commits pages are `no-cache` and re-derived after a rebase or force-push. Every pass finds the recorded frontier sha in the current list with the same number of rows below it; either check failing re-derives the chain.

Page entry:

- A page hands the app its route in `<meta name="gs-route">` and its base in `data-base` on `#gs-page`. `gs-upgrade.js` loads the shell from that base.
- The page's head script marks `<html>` with `gs-boot` and stamps the stored theme before the body is parsed. The front page is not cloaked, since its served document is already what the home route shows.
- Every step of the takeover is reversible: a shell asset that fails or hangs, a throw during boot, or a route that does not settle restores the served page and its URL.
- A `location.hash` naming a route wins over the page's own route; a bare anchor stays on the page.

Index maintenance:

- Each extension keeps two append-only corpora, `items/<ext>/` and `bodies/<ext>/`: content-hash-keyed sealed shards, a mutable head and a manifest.
- Artifacts are built from objects the push uploaded before any ref moved, so they never name a commit a reader cannot resolve.
- One rebuild writes bodies shards, items shards, bodies head, items head, bodies manifest, items manifest, then the cursor. The manifests are the only commit points.
- A document at another schema version reads as absent, and the reader falls back to a bounded object walk.
- A rebuild takes one action: no-op, append, repair, bootstrap or backfill. Append owns the newest end and backfill the oldest. Once an items manifest exists, the one reset is a manifest tip unreachable from the pushed tip.
- `items/code/` takes its tip as a digest over the sorted branch tips, attributes each commit to a branch by the reader's own rule, and repairs rather than appends when the tip changes.

### Push-time maintenance

Every ref-moving push to an s3 remote runs the site rebuild as its share of the upkeep pass ([S3.md](S3.md#push-maintenance)), through the `site.PostPushMaintenance` hook the CLI gives the remote helper.

- `.gitsocial/site/push-state` records the last full pass: the shell version, a digest over the ref listing and the page layer's state. A matching marker skips the pass.
- The marker is stamped only at the end of a full pass, and withheld while a bootstrap or the page layer still owes work.
- The site walks read commits from the pusher's local odb and fall back to the bucket for an object it lacks.
