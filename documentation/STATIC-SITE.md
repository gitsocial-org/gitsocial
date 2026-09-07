# Static Site

`gitsocial mirror` and `gitsocial push` publish a browsable website of the repository into a bucket, as static files the browser reads directly: timeline, issues and boards, pull requests, releases, code, search, analytics.

[Mirror](#mirror) · [Publish](#publish) · [Customization](#customization) · [HTML pages](#html-pages) · [Testing](#testing) · [Reference](#reference)

## Mirror

For a project hosted on a forge, one command clones it, imports its issues, pull requests, releases and discussions, and publishes data, code and the site:

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix> --url https://your-domain/
gitsocial mirror                       # later, from the workspace: refresh
```

- `--url` is the site's public address and turns the [HTML pages](#html-pages) on; the other flags are in [CLI.md](CLI.md#gitsocial-mirror).
- Re-running refreshes. It is safe from cron, and a crashed run resumes.
- Creating the bucket, allowing public reads and attaching the domain are provider dashboard steps; `--dry-run` prints the checklist.

## Publish

```bash
gitsocial remote add s3://<endpoint>/<bucket>/<prefix>   # once, see S3.md
gitsocial config site set publish true
gitsocial push
```

- `publish` is off by default. It lives in the pushed config ref, so a plain `git push` that carries it maintains the site too.
- `--no-site` skips the site for one push. `git config gitsocial.pushSite false` opts a machine out. Neither turns the site on.
- `gitsocial push --site-only [remote...]` rebuilds the site without pushing data. It fails when `publish` is off or the remote is not s3.
- The site needs public reads on the bucket or the domain in front of it. A private bucket still works as a remote, without a site.
- A push from a newer binary re-uploads the [shell](#shell), the app's own files.
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

Only `url`, `publish` and `pages` can differ per remote; the other keys travel with the repo. An override lives in local git config as `remote.<name>.gitsocial-site-<key>`, regenerates that remote's site in full on the next push, and applies to pushes by remote name, not by URL.

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

Effective when `publish`, `pages` and a valid `url` are all set. Every push then maintains the [page keys](#page-keys):

- An item page inlines its thread: replies in time order, edits resolved with an "edited" marker, tombstones for retractions, review chips and `file:line` feedback on pull requests, artifact blocks on releases. A thread caps at about 100 replies or 200 KB with a "N more replies" marker.
- A list page holds 100 entries: a mutable `index.html` head and sealed `<n>.html` pages, each linking to the older one. Milestones and sprints fold into `issues`.
- The commits list covers the default branch, one row per commit, no diffs, no per-commit page. Each row has an id, so `commits/<n>.html#c-<sha12>` is a citable URL. Sealed commits pages are re-derived after a rebase or force-push.
- A file page renders one prose document on the default branch at `f/<path>.html` (`f/specs/GITMSG.html`). A document qualifies when:
  - it is markdown (`.md`, `.markdown`, `.mdown`, `.mdx`) or an extensionless convention document (LICENSE, NOTICE, AUTHORS, CONTRIBUTING, CHANGELOG and their siblings, shown preformatted);
  - it is not under a dotdir, `node_modules/`, `vendor/`, `testdata/`, `third_party/`, `fixtures/`, `golden/`, `corpus/`, `snapshots/` or `__snapshots__/`, and is not a submodule, a symlink or the root README;
  - its path has no space and none of `@ : # ? %`.
- File pages follow the tree: a document that leaves it loses its page, one under 100 words stays out of the sitemap, and any renders whole up to 256 KB. `filesInclude` and `filesExclude` override the selection rule.
- The front page: the default branch and its tip commit, the root file listing, the README rendered from up to 8 KB of source, and the newest 10 entries across items and code commits. Item rows link to their pages; commit rows link into the app.
- `sitemap.xml` lists the front page, every indexable item page, non-empty list pages, the commits pages and the file pages, each with `lastmod`. Not listed: retracted items, empty lists, file pages under the word floor.
- `feed.xml` is Atom 1.0 with the newest 50 non-retracted top-level items, memos excluded. Each type directory has its own `feed.xml`.

The README is rendered with the app's own markdown grammar (`site_markdown.go` is a port of it), so the page and the app agree block for block; raw HTML is rebuilt against an allowlist. Two differences from the app: repo-relative images degrade to their alt text, and in-page anchors are rewritten to the rendered heading ids.

Rules that hold on every page:

- Every `<title>` is unique (a shared subject gets the item's date, then its short ref) and every `<meta name="description">` is prose with the markdown syntax stripped.
- Retraction tombstones and file pages under the word floor carry `noindex,follow`. The page stays, so existing links keep working.
- Item bodies render as escaped plain text. Only the README and file pages, the bucket owner's own content, go through the markdown renderer.
- First-time generation is budgeted at 5,000 pages per push and resumes on the next push: item pages, then file pages, then commits pages.
- Setting `pages false` or removing `url` deletes the page layer on the next push and restores the shell at `index.html`.

Known divergence: the app renders markdown for `.md` and `.markdown` only, so an `.mdx` page reads as prose before the boot and as source after it.

## Testing

```bash
scripts/site-test.sh                                              # the browser battery
go test -tags sitetest -timeout 30m ./library/core/objstore/      # the same from go test; skipped without node
GS_SITE_LEGACY_ORIGIN=http://localhost:8000 scripts/site-test.sh  # plus the legacy tier
bin/locals3 -root <dir>                                           # serve a pushed site locally, see S3.md
```

The harness is `library/core/objstore/sitetest/`: `fixture.sh` builds the fixture buckets, `serve.js` serves them with real cache headers and `Range` support, `runner.js` runs the suites. Fixture-size overrides are in [S3.md](S3.md#environment-variables). The battery runs at release; nothing in `go test ./...` covers the browser side.

## Reference

### Shell

`core/objstore/site/`, embedded in the binary and uploaded whenever the shell version changes:

| File | Role |
|---|---|
| `index.html` | the app shell, and the front page while the page layer is off |
| `gs-core.js`, `gs-render.js`, `gs-app.js` | the app |
| `gs-upgrade.js` | boots the app from a generated page; a failed boot restores the static page |
| `pages-core.css` | tokens, theme gates, reset, page structure; inlined into every generated page |
| `pages-full.css` | the component vocabulary and the webfonts |
| `prism.js`, `grammars/` | the base grammars and 47 lazy-loaded ones |
| `icons.js`, `fonts/` | file-type icons; EB Garamond and IBM Plex Mono |

`.js`, `.css`, `.html` and `.json` under `site/` upload brotli-compressed with `Content-Encoding: br`. Generated HTML, the sitemap, `robots.txt` and the feeds upload plain. Git objects never carry an encoding.

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

Item pages and sealed list pages are rewritten only when `sitePagesVersion` in `site_pages.go` changes, so a change to their head or markup bumps it. Everything else is rewritten on every push.

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
