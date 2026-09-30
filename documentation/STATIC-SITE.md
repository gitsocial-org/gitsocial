# Static Site

The static site is a website of the repository that `gitsocial mirror` and `gitsocial push` build in a bucket, as static files that the browser reads directly: timeline, issues and boards, pull requests, releases, code, search, analytics.

[Mirror](#mirror) · [Publish](#publish) · [Customization](#customization) · [HTML pages](#html-pages) · [Design](#design) · [Testing](#testing) · [Reference](#reference)

## Mirror

For a project on a forge, one command clones the project, imports its issues, pull requests, releases and discussions, pushes data and code, and builds the site:

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix> --url https://your-domain/
gitsocial mirror                       # later, from the workspace: refresh
```

- `--url` is the site's public URL and enables the [HTML pages](#html-pages).
- The remaining flags, the refresh behavior and the provider checklist are in [CLI.md](CLI.md#gitsocial-mirror).

## Publish

```bash
gitsocial remote add s3://<endpoint>/<bucket>/<prefix>   # once, see S3.md
gitsocial config site set publish true
gitsocial push
```

- `publish` is off by default. It is in the pushed config ref, so a plain `git push` that carries that ref also rebuilds the site.
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

Values are in the `site` object of the core config ref and reach the bucket as `.gitsocial/site/site-config.json` on the next push. A value that fails validation uses its default. The last four keys belong to the [HTML pages](#html-pages).

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

Only `url`, `publish` and `pages` can differ per remote; the other keys are the same for all remotes of the repository. An override is in the local git config as `remote.<name>.gitsocial-site-<key>`, rebuilds the full site of that remote on the next push, and applies to pushes by remote name, not by URL.

```bash
gitsocial config site set publish false --remote backup
gitsocial config site list --remote backup     # effective values
```

## HTML pages

HTML pages are crawlable pages that a browser shows without JavaScript; with JavaScript on, a page starts the app in the same browser tab.

```bash
gitsocial config site set pages true
gitsocial config site set url "https://example.com/"   # absolute base for canonicals, OG tags and the sitemap
```

Effective when `publish`, `pages` and a valid `url` are all set. Every push then maintains:

- One page per top-level item, with its thread inlined up to a limit.
- A list page per type, paged oldest-first with `older →` and `← newer` links. A full page holds a complete page of rows, and later pushes do not rewrite it; the newest rows are on the head page, `index.html`. Milestones and sprints are in `issues`.
- The commits list of the default branch, one row per commit with no diffs. A row is citable as `commits/<n>.html#c-<sha12>`.
- A file page for each prose document on the default branch, at `f/<path>.html`: markdown (`.md`, `.markdown`, `.mdown`, `.mdx`) and the extensionless convention documents (LICENSE, CONTRIBUTING, CHANGELOG and their siblings). The selection excludes documents under a dotdir or a vendored, test or fixture directory, and also submodules, symlinks, the root README and paths with a space or one of `@ : # ? %`. `filesInclude` and `filesExclude` change the selection.
- The front page: one section with the branch, the latest commit and the root files, two of them shown and the rest behind a chevron, then the README.
- `sitemap.xml` with every indexable page, and Atom feeds: `feed.xml` and one per type directory, memos and commits excluded.

What a page shows:

- Item bodies render as escaped plain text. Only the README and file pages, the bucket owner's own content, render as markdown, with raw HTML rebuilt against an allowlist.
- A repository-relative image in the README shows as its alt text.
- The pages of retracted items and very short file pages carry `noindex,follow`.
- Every `<title>` is unique, and every description is prose with the markdown stripped.

On a large repository, several pushes can be necessary to write every page; `GITSOCIAL_SITE_PAGES_BUDGET` limits the item pages that one push writes ([S3.md](S3.md#environment-variables)). Setting `pages false` or removing `url` deletes the pages on the next push.

## Design

The site is the app shell, the pre-rendered pages and the two stylesheets. Five rules hold across all three.

1. Every page shows its content without JavaScript; the app adds to what the reader sees and never replaces it.
2. The page layer and the app render a component from the same class names and the same tokens. Two markups for one component is a defect.
3. Body text is serif; the frame, code and metadata are small mono. Nothing is decorative.
4. Every rule holds on any repository: no README, no issues, a tree of thousands of files, an `.mdx` documentation site.
5. A missing asset or a failed fetch shows a one-line notice at the position of the content, never a blank or an endless spinner.

The tokens are declared once, in `pages-core.css`; `site_pages_tokens_test.go` fails on a spacing value off the scale or a token declared elsewhere. `sitetest/parity_fixtures.json` records the markup and wording that the page layer and the app share. A visual change includes its golden and baseline update in the same commit.

## Testing

```bash
scripts/site-test.sh                                              # the browser battery
go test -tags sitetest -timeout 30m ./library/core/site/      # the same from go test; skipped without node
bin/locals3 -root <dir>                                           # serve a pushed site locally, see S3.md
GS_STYLES_UPDATE=1 node library/core/site/sitetest/verify_styles.js   # recapture the style baselines
go test -tags sitetest -run TestSiteShapeGoldens ./library/core/site/ -update   # regenerate the goldens
```

- The harness is `library/core/site/sitetest/`. The browser suite runs at release, and no test in `go test ./...` covers the browser side.
- Every suite but `verify_styles.js` runs under a DOM shim. `verify_styles.js` drives a real Chrome, compares computed styles and structure on ten routes in both themes to `sitetest/styles/`, and checks that the main routes fit a 390 px viewport.
- `shapes.sh` builds six repository-shape fixtures: a non-`main` default branch, an `.mdx` docs site, an empty repository, code only, a 6,000-file tree, binary and LFS objects. `TestSiteShapeGoldens` compares their screenshots at 1280 and 390 px to `sitetest/goldens/`.
- `chrome.js` finds Chrome from a `CHROME` override or a list of absolute paths. Without Chrome the style suite and the goldens skip; `release.sh` fails in its first step when it cannot find Chrome.

## Reference

### Page keys

The pages are at the prefix root, next to the shell; the cache classes are in [S3.md](S3.md#cache-policy).

| Key | Content | Cache |
|---|---|---|
| `index.html` | front page while the page layer is on, otherwise the shell; its asset references carry the shell revision | no-cache |
| `.gitsocial/site/shell/<rev>/` | the shell assets of one binary version; the current and previous revisions are kept, older ones swept on a shell change | immutable |
| `i/<short>.html` | one page per top-level item | no-cache |
| `issues/`, `prs/`, `posts/`, `releases/`, `memos/` | `index.html` head plus full `<n>.html` pages | head no-cache, full pages immutable |
| `commits/index.html`, `commits/<n>.html` | the default-branch commit list | no-cache |
| `f/<path>.html`, `f/index.html` | file pages and their list | no-cache |
| `sitemap.xml`, `sitemap-head.xml`, `sitemap-<n>.xml` | the sitemap, an index over parts when it is large | head no-cache, parts immutable |
| `robots.txt` | `Allow: /` and the sitemap location | no-cache |
| `feed.xml`, `<dir>/feed.xml` | Atom feeds | no-cache |

Item pages and full list pages are rewritten only when `sitePagesVersion` in `site_pages.go` changes; the commit that bumps it says why. Everything else is rewritten on every push.

### Artifacts

The app reads the artifacts under `.gitsocial/site/`, and the bucket's ref list at `.gitsocial/refs.json` ([S3.md](S3.md#bucket-layout)), in place of object walks.

| Key | Content |
|---|---|
| `version` | shell version marker, a hash of the raw assets |
| `items/<ext>/` | per-extension metadata index: full shards, a mutable head, a manifest; an adopted copy's entry also names its original author and time |
| `bodies/<ext>/` | message bodies, loaded on demand |
| `items/code/` | one index of plain commits across code branches, with parent shas and no bodies |
| `pages.json` | page-layer manifest: schema version, consumed tips, bootstrap cursor, list and commits partitions, the sidebar's item counts |
| `site-config.json` | the customization values |
| `pm-config.json` | the resolved PM board |
| `stats.json` | a stats blob written by the CLI from the workdir |
| `tags.json` | each tag's commit, date, author, previous tag and its commit, the commit count since it, and whether its range document is written, in display order, from the local object database |
| `ranges/v1/<prev>..<commit>.json` | one range document for each pair of adjacent tag commits: the merge base, the commits since the previous tag (up to 2,000) and the files changed (up to 1,000); immutable |
| `push-state` | skip digest for push-time maintenance |

### Invariants

The shell and the pages:

- `pages-core.css` is inlined into every page head, carries no `url()`, and bumps `sitePagesVersion` when it changes. `pages-full.css` loads after first paint, and the app uses the same two stylesheets.
- Each push writes a configured accent as a `:root` override after the inlined core, so the shell version comes from the binary alone.
- `index.html` has two owners: the page layer while it is effective, the shell otherwise. Every rebuild with an effective page layer writes it again.
- The page layer and the app render markdown with one grammar, ported between JS and Go and asserted equal.
- An adopted copy (GITMSG.md §1.5) shows its original author and time, and the repository it came from, on both renderers. Neither renderer lists a cross-repository edit, which is a proposal.
- File discovery reads the default branch's tree from the local object database of the pusher and, when it cannot read the tree, keeps the published set.
- A changed default branch rewrites every page.
- The Tags page shows each tag's date and author, its commit count since the previous tag, and a release chip for a tag that a release names.
- The Tags page uses a `tags.json` entry only when its sha is the tag's sha in `refs.json`; for any other tag it reads the tag objects. It shows commit counts only when the file covers every tag and the previous tag of each entry is the next row.
- A push from a clone that does not have a tag keeps that tag's entry while its sha is the same in `refs.json`.
- The tag page reads its commits and files from the range document when the `tags.json` entry flags one for the commit of the previous row, and walks objects otherwise. A push writes the range documents before `tags.json`, and flags only the documents that it wrote or found flagged for the same pair. A shallow clone writes none.
- The version order of tags has one definition in two places, `compareSiteTagsDesc` and `compareTagsDesc`, and a hash suffix (`v<version>.<hash>`) is not part of the version. Tags that differ only by that suffix are in date order.
- When the code index does not hold both tips of a compare, a tag page or a pull request diff, the app walks commit objects as `git rev-list base..head` does, newest committer time first. The number of reads follows the range, not the history. A range longer than 2,000 reads shows as truncated, with no commit from under the base.
- Only the app shows sidebar counts: branches and tags come from `refs.json`, and commits and the item counts from `pages.json`, which carries the item counts only after a complete pass.
- An item count is the open work, the rows that its list shows first: Issues, Pull Requests, Milestones and Sprints show their Open filter first. The sidebar of a generated page carries no count.

Commits pages:

- Rows come from the code index, not a second git walk, so the list, the timeline and the front page show the same commits. The app's `/commits` route reads the pagination from `pages.json` and renders the rows that the page shows.
- Full commits pages are `no-cache` and re-derived after a rebase or force-push. The frontier is the newest row of the full pages. Every pass finds the recorded frontier sha in the current list, with the same number of rows below it; when either check fails, the pass re-derives every full page.

Page entry:

- A page hands the app its route in `<meta name="gs-route">` and its base in `data-base` on `#gs-page`. `gs-upgrade.js` loads the shell from that base.
- The page's head script sets the stored theme before the body is parsed and marks `<html>` with `gs-boot`, which hides the page during boot; the front page is not hidden.
- Every step of the app start is reversible: a shell asset that fails or hangs, a throw during boot, or a route that does not settle restores the served page and its URL.
- A `location.hash` that names a route has priority over the page's own route; a bare anchor stays on the page.

Index maintenance:

- Each extension keeps two append-only corpora, `items/<ext>/` and `bodies/<ext>/`: content-hash-keyed full shards, a mutable head and a manifest.
- Artifacts are built from objects that the push uploaded before any ref moved, so they never name a commit that a reader cannot resolve.
- One rebuild writes bodies shards, items shards, bodies head, items head, bodies manifest, items manifest, then the cursor. The manifests are the only commit points.
- A document at another schema version reads as absent, and the reader uses a bounded object walk instead.
- A rebuild takes one action: no-op, append, repair, bootstrap or backfill. Append owns the newest end and backfill the oldest. After an items manifest exists, the only reset is a manifest tip that is unreachable from the pushed tip.
- `items/code/` takes its tip as a digest over the sorted branch tips, attributes each commit to a branch by the reader's own rule, and repairs the index when the tip changes.

### Push-time maintenance

Every ref-moving push to an s3 remote runs the site rebuild as its part of push maintenance ([S3.md](S3.md#push-maintenance)), through the `site.PostPushMaintenance` hook that the CLI gives the remote helper.

- `.gitsocial/site/push-state` records the last full pass: the shell version, a digest over the ref listing and the page layer's state. A matching marker skips the pass.
- The marker is written only at the end of a full pass, and not while a bootstrap or the page layer has work left.
- The site walks read commits from the local object database of the pusher, and from the bucket when an object is not there.
