# Static Site

`gitsocial push` publishes a complete, browsable website of your repository (timeline, issues and boards, pull requests, releases, code, search, analytics) into the same S3-compatible bucket that hosts the repo, alongside the repo data.

The site is plain static files read directly by the browser: no server, no build step, no dependencies, and nothing for visitors to install. Anyone with the bucket's public URL can follow the project without a forge account, and the site stays current automatically because every later `gitsocial push` refreshes the data it reads.

> Transport: [S3.md](S3.md) owns the remote helper, URL normalization, [bucket layout](S3.md#bucket-layout), ref-update modes, cache policy, and environment variables.

## Publish

```bash
gitsocial remote add s3://<endpoint>/<bucket>/<prefix>   # once: bucket as a remote (see S3.md)
gitsocial push                                           # publish repo data + the browsable website
```

`gitsocial push` publishes the site by default for `s3://` remotes — a bucket with no site gets one on the first push. Opt out per-push with `--no-site`, or persistently with `git config gitsocial.pushSite false`. `gitsocial site push [remote]` remains as an **explicit site refresh** (upload/re-derive the site without pushing new data); the remote defaults to the [push remote](S3.md#push-remote-resolution). See [S3.md → Push behavior](S3.md#push-behavior-publish-by-default) for the full flag set and the two site-gate rules (`gitsocial push` creates a site; plain `git push` does not).

The bucket (or its public domain, e.g. r2.dev or a custom domain on Cloudflare R2) must allow public reads. Once the bucket is site-enabled, every subsequent push maintains the data artifacts the site reads, and a push from a binary carrying a newer embedded site re-uploads the shell itself (tracked by `.gitsocial/site/version`), so the page keeps working without a manual refresh.

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

Both the writer and the reader validate every field with the same rules, so a bad config never breaks the page; if nothing survives validation the artifact is deleted and the site falls back to its defaults. The `.gitsocial/site/assets/` prefix is reserved for future binary assets; nothing reads or writes it today.

## How it works

The whole site lives in `core/objstore/site/`, embedded in the binary via `SiteFiles`: `index.html` plus the reader JS (`gs-core.js` / `gs-render.js` / `gs-app.js`), which reads the [bucket layout](S3.md#bucket-layout) directly and re-implements gitmsg message parsing in JS. Layout or protocol changes must touch it too, and editing a file under `core/objstore/site/` requires rebuilding the binary before `site push`. `SiteFiles` ships every file under `site/` recursively (subdirectories included), so `uploadSiteFiles` publishes the whole shell — including the syntax-highlighting grammars under `site/grammars/`.

Syntax highlighting is Prism (`prism.js` bundles the common grammars: go/js/ts/json/yaml/bash/markdown/markup/css/diff). Every other language ships as its own `site/grammars/prism-<lang>.js` file (regenerate with `scripts_gen_grammars.sh` from the vendored `prismcomp/` 1.30.0 build); the reader lazy-loads one on first use (`ensureGrammar` in `gs-render.js`), loading any dependency chain first (e.g. `cpp`→`c`), caching per session, and upgrading the already-rendered plain text in place. A code block renders un-highlighted immediately and never blocks on the fetch; a missing grammar file stays plain text. This replaces the old push-time tree scan that published a per-repo `prism-extra.js` bundle — `site push` (and any shell re-upload) now deletes that obsolete key best-effort.

Pushes maintain a set of read-optimized data artifacts under `.gitsocial/site/`: `refs.json` (ref discovery without bucket listing), and per-extension items/bodies indexes in an append-only sharded layout (immutable brotli-compressed shards plus a small mutable head and manifest). Items shards carry metadata and subjects only; full message bodies live in a separate corpus loaded on demand. A single **code items index** (`.gitsocial/site/items/code/`) mirrors that layout for plain (non-gitmsg) commits: one merged, deduped corpus across every code branch, each entry attributed to a branch (the default branch when the commit is reachable from it, else the first code branch that reached it), so the timeline sources its interleaved code commits from the index instead of walking loose objects. The code corpus is **metadata-only — no bodies corpus**: a code card shows subject + author/time/hash, and the full commit message is only needed on the detail view (which hydrates the loose object), so bodies would bloat the bucket for content the timeline never renders. Maintenance is incremental: a push appends the new commits, repairs an interrupted write, or, on very large branches, bootstraps the index over multiple pushes (a partial index is always a valid newest-first prefix, so the timeline works from the first push). A code-branch force-push/rebase that drops indexed commits is repaired by rebuilding the affected shards (the corpus is defined as "reachable from any current code tip"). Artifacts carry a format version (currently 4); a reader that sees any other version — or no code index at all (an older bucket) — ignores the artifacts and falls back to a bounded loose-object walk.

Almost every surface is served from the index, never a per-object history walk: the timeline, the default-branch log, all the list tabs (issues/board/milestones/sprints/prs/releases/memos), search, item and merged-PR detail (a merged PR's merge-base/merge-head short shas resolve by prefix over the code index rather than a base-branch walk), and analytics all read the index slice plus the rendered slice. Only the surfaces that need parent topology the index cannot answer walk loose objects deliberately, bounded: the commit graph, compare, and a non-default-branch log (index attribution can't answer reachability off the default line), plus code/file browsing (tree objects are an inherent per-navigation cost). A request-budget test suite (`sitetest/verify_request_budget.js`) pins a per-route fetch ceiling on the fully-indexed fixture so a regression into a walk fails CI.

Visitor cost stays flat as history grows: opening the site downloads the fixed 130 KB shell plus the newest slice of the index, under half a megabyte even at 100K commits. Everything else loads on demand; the timeline fetches 50 items at a time as you scroll, deeper search shows its download size before fetching (about 2.4 MB of full-text at 100K commits), and loads are guarded (progress checks, a boot watchdog) so a broken or partial bucket surfaces an error instead of an eternal "Loading…". On the bucket, collaboration data costs about 1 KB per message, and code roughly 2-7x its packed clone size (objects are stored individually, without packfile delta compression); none of that reaches the visitor. A 100K-commit index bootstraps over two `site push` runs, and the timeline is already servable after the first.

## Local development & testing

The site can be served by the disk-backed local S3 server used for the transport (`library/core/objstore/locals3`; see [S3.md § Local development](S3.md#local-development)), so a locally built site is browsable exactly as it would be from a real bucket.

`library/core/objstore/sitetest` is the headless test harness: `fixture.sh` builds a fixture bucket, `serve.js` serves it with real bucket cache/`Content-Encoding` headers, and `runner.js` drives the browser-side suites (writer/reader parity, interrupted and partial-bootstrap pushes, and the feature verifiers under `verify_*.js`). The `GITSOCIAL_SITE_SHARD_COUNT` / `GITSOCIAL_SITE_WALK_BUDGET` overrides shrink shard sizes and walk budgets so bootstrap paths are exercised on small fixtures.
