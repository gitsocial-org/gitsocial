# Static Site Design

What the site in a bucket looks like and what a visual change must satisfy; [STATIC-SITE.md](STATIC-SITE.md) says how that site is built and served.

[Principles](#principles) · [Tokens](#tokens) · [Layout](#layout) · [Components](#components) · [States](#states) · [Repo-shape rules](#repo-shape-rules) · [Fixtures and visual tests](#fixtures-and-visual-tests) · [Change rules](#change-rules)

## Principles

The site is the app shell, the pre-rendered pages and the two stylesheets. Five rules hold across all three.

1. A document first, an app second. Every page reads without JS. The app adds to what the reader sees and never replaces it under them.
2. One vocabulary. The page layer and the app render a component from the same class names and the same tokens. Two markups for one component is a defect.
3. Content sets the tone. Body text is serif; chrome, code and metadata are small mono. Nothing is decorative.
4. Every rule holds on any repository: no README, no issues, a tree of thousands of files, an `.mdx` documentation site.
5. Fail in place. A missing asset or a failed fetch shows a one-line notice where the content would be, never a blank and never an endless spinner.

## Tokens

Declared once in `pages-core.css`, consumed by both stylesheets and both renderers.

| Group | Tokens | Rule |
|---|---|---|
| Type | `--fs-h1` 2.25rem, `--fs-h2` 2rem, `--fs-h3` 1.75rem, `--fs-h4` 1.5rem, `--fs-body` 21px with `--lh-body` 1.2, `--fs-md` 0.95rem, `--fs-ui` 0.8rem, `--fs-code` 0.85rem, `--fs-dense` 0.72rem | no `font-size` literal outside these |
| Fonts | `--serif` EB Garamond, Georgia; `--mono` IBM Plex Mono | a page that has not loaded `pages-full.css` reads in Georgia at the same size |
| Palette | `--bg`, `--text`, `--link`, `--card`, from the light set `--pl-*` or the dark set `--pd-*`; file-type hues `--i-*` | no hex outside `:root` |
| State colors | `--open` #1f9d55, `--closed` #8957e5, `--merged` #8250df, `--warn` #bf8700, `--danger` #cf222e | one meaning per color, everywhere |
| Derived | `--muted`, `--line`, `--panel`, `--chip`, `--code-bg`, `--btn`; tints `--link-t1..3`, `--open-t1..3`, `--warn-t1..3`, `--danger-t1..3` | computed with `color-mix` on `body`, never restated |
| Radius | `--r-pill` 999px, `--r-panel` 10px, `--r-ctl` 6px | marks under 4px stay literal |
| Spacing | `--sp-1` 0.2rem, `--sp-2` 0.4rem, `--sp-3` 0.6rem, `--sp-4` 0.9rem, `--sp-5` 1.2rem, `--sp-6` 2.5rem | every padding, margin, gap and offset on the nearest step; `TestSitePagesSpacingOnTheScale` holds it |
| Padding pairs | `--pad-panel` 0.75rem 0.9rem, `--pad-row` 0.4rem 0.75rem, `--pad-ctl` 0.2rem 0.6rem | built from the spacing scale |
| Layout | `--nav-w` 220px, `--shell-max` 1012px, `--shell-pad` 1.25rem, `--nav-gap` 1.5rem; one breakpoint at 720px | the breakpoint is shared by both stylesheets |
| Theme | the system preference decides; a stored choice stamps `.dark-mode` or `.light-mode` on `html` for a page and on `body` for the app | a stored choice outranks the system preference |

Off the spacing scale: a mark under 4px, and an `em` value, which scales with its own text. A value off the scale gets a row here.

A card is the one surface with a palette entry of its own rather than a mix off the page: `--pl-card` is #fff8e5, a lift off the light parchment, and `--pd-card` stays the dark panel mix. Every other surface keeps its derived token.

## Layout

- Desktop: a sidebar and a content column inside one shell, padding included, so both surfaces put the content at the same x. A width toggle switches the shell between fixed and full.
- Sidebar order: repo title, theme toggle, width toggle, collapse. Then Search, Home, the sections Social (Timeline, Lists), PM (Board, Issues, Milestones, Sprints), Repository (Pull Requests, Code, Commits, Branches, Graph, Tags), then Releases, Memos, Analytics, Configuration, and the brand credit pinned at the bottom.
- A generated page carries the same sidebar with only the destinations that have pages: Home, the type lists, Commits, and Files when the repository has documents.
- At the breakpoint and under: a top bar with a hamburger and the title, and the sidebar as a slide-over drawer. Without JS the sidebar is a wrapped row above the content. Nothing is hidden.
- The boot swap replaces `#gs-page` with the app's render at the same width and x. The chrome appears first, the content fills in, and nothing moves when it lands.

Every surface is a fragment route on `index.html`, parsed by `parseRoute` in `gs-core.js`. A route the page layer pre-renders names the page kind that serves it, and its key is in the [page keys](STATIC-SITE.md#page-keys).

| Route | Fragment | Page |
|---|---|---|
| `home` | `#/`, or a bare `#<anchor>` into the README | the `front` page |
| `index` | `#/timeline`, `#/issues`, `#/prs`, `#/releases`, `#/memos`, `#/milestones`, `#/sprints` | a `list` page per type directory |
| `commits` | `#/commits`, `#/commits/<n>`, `#/commits:<anchor>` | a `list` page |
| `commit` | `#commit:<hash>@<branch>` | an `item` page for a gitmsg item, the app alone for a code commit |
| `file` | `#file:<path>@<branch>` | a `file` page |
| `code` | `#/code`, `#/tree` | the app, and where `f/index.html` boots |
| `compare` | `#/compare:<base>...<head>`, `#compare:<base>...<head>` | the app |
| `branch` | `#branch:<name>` | the app |
| `tag` | `#tag:<name>` | the app |
| `list` | `#list:<id>` | the app |
| `branches` | `#/branches` | the app |
| `tags` | `#/tags` | the app |
| `graph` | `#/graph` | the app |
| `board` | `#/board` | the app |
| `search` | `#/search/<query>` | the app |
| `lists` | `#/lists` | the app |
| `analytics` | `#/analytics` | the app |
| `config` | `#/config` | the app |
| `notfound` | a fragment that parses as none of the above | the app |

## Components

One builder per component in JS and one template in Go. "Both" means the page layer and the app render it; "app" means the app alone.

| Component | Classes | Renderers | Notes |
|---|---|---|---|
| Sidebar | `.nav` (app), `.page-nav` (pages), `.nav-group`, `.nav-section`, `.nav-icon`, `.nav-tree-slot` (app), `.nav-footer` | both | every section shows on every repository, and a list reached from it shows its empty state; the title is `site.title`, else the bucket name; the app's file tree scrolls inside `.nav-tree-slot`, so no nav row runs under the pinned credit |
| Card | `.card > .card-head > .type-glyph + .chip + a.subject`, then `.meta`, then a chip row; a trailing chip slot after the subject | both | every list row is a card: items, commits, releases, board cards, search results, recent activity; the app builds them all from `card` in `gs-render.js`; its ground is `--card`; the head's one slot carries the retracted marker, and a body-only card, which has no head, leads its meta row with it and stands on its body, of which a row shows the first line |
| Feedback card | `.card.feedback`, the verdict on `.verdict-<state>` as a 3px left border and on a chip | both | approved or changes-requested; the file and line anchor ride a plain chip, dropped inline under the line they anchor; padding, radius and background come from `.card` |
| Chip | `.chip` plus one variant class, built by `chipEl` in JS and the `chip` template in Go | both | mono, `--fs-ui`, pill radius, tint fills from the token scale; a chip never carries the edited marker |
| Chip variants, both | `.state.<state>` through the one state-class rule (open, closed, merged, completed, active, planned, canceled, unknown), `.pre.state` ("prerelease"), `.chip-retracted` ("retracted"), `.verdict-<state>` with the hyphen read as a space | both | the plain chip carries a version, a branch name, a file anchor and "draft" |
| Chip variants, app | `.chip-count`, `.chip-label`, `.chip-assignee`, `.chip-priority.prio-<level>`, `.chip-due`, `.chip-origin`, `.chip-bot`, `.chip-review`, `.pm-sub-chip`, `.chip-signed`, `.version-label`, `.caveat`, `.board-wip-over`, `.reviewer-chip`, `.branch-tip`, `.tag-tip`, `.merged-branch`, `.filter-chip`, `.facet-chip`, and the `.card-chips` row | app | a new variant gets its entry here first |
| Type glyph | `.type-glyph.tg-<type>` | both | plain characters, so a page without JS carries the same mark; the type is the header's, else the extension's default, and it is tinted by state on issues and pull requests |
| Meta row | `.meta` holding `span.author`, `span.reltime`, `a.hash` in that order, separated by a middle dot; inside `.detail-meta` on a detail page | both | mono and muted; the author is the display name with the email in `title`, "unknown" when both are empty |
| Meta row bits | the precise stamp in the time's `title`; the relative time in the app and the `YYYY-MM-DD` UTC date on a page; the hash as the 12-character short form linking to the item | both | the page layer's type label, `head → base`, "due", the sprint range and "signed" follow the hash |
| List page | `h1`, cards, `.filter-chip` filters, and `.load-more` in the app where a page carries its sealed-page links | both | the heading is the nav label verbatim, and so are the `<title>`, the description and the feed title |
| Detail page | `.card-head > h1.subject` plus the head's one chip slot, then `.detail-meta`, `.body`, the thread, `.version-row` history, `.asset-list` on releases, diff and review sections on pull requests; the app wraps it in `.detail` | both | the state, draft, prerelease, retracted and version chips ride the head's one slot, never the meta line; a body-only type promotes no first line and heads with the meta row alone; the app's raw toggle and copy-link control share the top bar's one `.page-actions` row |
| Release head | the tag as the subject, then one version chip | both | the chip is dropped when the head already names the version |
| Release row | the release head over a meta row of the author, the date and the asset count | both | the count stands where every other row links its hash, and goes when the release names no artifact |
| Release notes | `dl.release-notes` with a `dt` and a `dd` per commit | both | a release body block whose every line reads `<hash> <message>` renders as these rows: the hash a mono link to its commit route, the message beside it; they take the trailer treatment, and every other block of the body stays prose |
| Release assets | `.assets > .assets-head`, then an `.asset-list` of `.asset-row` artifacts and a second holding the checksums and the SBOM on a chip, then `.asset-signed` | both | a row links its name when `artifact-url` gives it an `https:` or root-relative target, else the name stands as selectable mono text |
| Thread | comment cards in time order under a `Comments (N)` heading, one rail per depth level | both | a reply follows the one it answers, siblings run oldest first, depth caps at four; the type glyph leads a comment's meta row; a missing parent falls back to a quote |
| Trailers | `.detail dl` with a `dt` and `dd` per field | app | mono, muted, `--fs-ui`; it carries the header fields no other component on the page shows, so the route's `ext` and `type`, the head's `state`, `draft`, `retracted`, `tag`, `version` and `prerelease`, and the meta row's `origin-author-name`, `origin-author-email` and `origin-time` stay out; `origin-platform` and `origin-url` fold into one `origin` row |
| Markdown | `.markdown`, headings with `md-` ids, lists, tables, fences, images, blockquotes | both | one grammar, ported between JS and Go, asserted equal |
| Code | tree (`.tree-row`, `.tree-node`, chevrons, tree search), blob (highlighted, raw pane, images, video), diff (`.diff-section`, unified or split, inline feedback) | app | a blob the view labels rather than renders carries its one sentence in `.empty` under the breadcrumb, with the submodule's full sha in the label's `title` |
| Board | columns, WIP indicator, collapsed columns, group-by | app | a board card is chrome: mono at `--fs-ui`, line-height 1 |
| Search | input, scope help, tier note, snippets, result cards | app | |
| Notice | `.notice` for degraded content, `.empty`, `.loading`, `.err` | `.notice` and `.empty` both; `.loading` and `.err` app | one sentence in place of the content; the wording is in [States](#states) |
| Controls | `.action-link`, `.back`, `.page-actions`, `.view-modes`, `.view-toggle`, `.share-link`, `.load-more` | both | mono, `--r-ctl`, `--btn` surface; a surface's controls sit on one `.page-actions` row, never on two |

`sitetest/parity_fixtures.json` pins the shapes both renderers share: `siteHeadChips` and `headChips`, `siteHeadSubject` and `headSubject`, the card skeleton, the meta row skeleton, the front page's truncation wording, the list labels and the empty sentences. `verify_styles.js` pins the app's own elements.

## States

Every component defines these where they apply. The wording is fixed, so it reads the same on every page.

| State | Treatment |
|---|---|
| Empty | one sentence in `.empty`, both renderers: "No <items> in this repository." with the list's noun (issues, pull requests, releases, posts, memos, milestones, sprints, lists, branches, tags, commits), "No activity in this repository yet." on the timeline, "No results for “<query>”." on a search; sentence case, a period, never an empty container |
| Loading | "Loading…" in `.loading`, app only, only where content will land; "Loading diff…" on a diff expand is the one variant; the page layer's boot cover (`html.gs-boot body::before`) carries the same word and the same tokens, pinned by `verify_upgrade_boot.js` |
| Error | one sentence in `.err`, in place, naming what failed, with the rest of the page usable: "This section failed to load.", "Branch not found: <name>", "Object not found."; app only, since a page has no read-time failure; the three bucket-level notices (a stalled view, no refs manifest, a 403) carry the fix in the sentence |
| Not found | "Branch not found: <name>", "Object not found.", one shape for every kind |
| Retracted | a tombstone: "retracted <type>" as the subject, `.chip-retracted`, no body |
| Edited | an "edited" bit in the meta row after the hash, with the edit's precise time in `title`; "edited by <name>" when the editor is not the author; both renderers, and never a chip |
| Stale | a commit no longer on its branch: dimmed text, no chip |
| Truncated | one sentence in `.notice` as the last row, both renderers: "N more not shown." for a list, tree or diff, "N more replies not shown." for a thread, "Truncated. The full file is in the repository." for a file or README, "Search truncated at N entries; refine the query." for a search; a `.load-more` or `.show-more` control below it where the app can expand, a link to the app route where a page cannot; a truncation whose control already names the total, as the front page's root listing does with "Show all N", carries that control alone and no sentence |
| Retry | an app fetch retries 429, 5xx and header timeouts with backoff, then shows the error state |

## Repo-shape rules

Each rule has a fixture that checks it, in [Fixtures and visual tests](#fixtures-and-visual-tests).

| Shape | Rule |
|---|---|
| No README | the front page shows the branch strip, the file list and recent activity, with no README block and no placeholder |
| README with hero HTML | the front page has no heading of its own, the README is the document and its headings stand as written; the repo title is the `<title>`, the sidebar and the mobile bar |
| Default branch not `main` | every "default branch" read uses `HEAD` from `refs.json`; nothing assumes a name |
| An extension with no items | its sidebar section stays and its list page shows the empty state |
| Code only, no gitmsg branches | every section stays and every list shows its empty state |
| Large tree, 5,000 files or more | the tree caps its rows with "show N more" and offers the tree search; no full expansion |
| Binary, LFS pointer, submodule, symlink | the blob view labels the object and renders nothing else: "Binary file, 1.2 MB", "Git LFS pointer", "Submodule at <sha>", "Symlink to <path>" |
| Markdown flavours | the page layer and the app agree on which files render as prose, and both drop a prose document's YAML front matter, and an `.mdx` document's import, export and standalone JSX blocks, before rendering what is left |
| Non-Latin or right-to-left text | body text inherits its direction, the chrome stays left-to-right, and truncation is by character, never by byte |
| Long history, 100,000 commits | everything is served from the index; a screen never walks more than `WALK_CAP` commits |
| Many branches or tags, 1,000 or more | branch and tag lists page, and graph chips collapse to "+N" |

## Fixtures and visual tests

Each shape above has one fixture bucket in the [repo-shape goldens](STATIC-SITE.md#repo-shape-goldens). A shape with no fixture is not a rule yet, and a new shape gets its fixture and its row in the same branch.

The tokens, the components and their states are checked by `verify_styles.js`, one of the [browser suites](STATIC-SITE.md#testing). Both suites need Chrome.

`verify_styles.js` also holds the phone rule: at 390 px the front page, a list page, an item page and the code route stay inside the viewport, in the served document and in the app it boots into. Both suites set the viewport through `cdp.js`, since a headless window stops at the platform's own floor.

## Change rules

- A new component, state or route is added here first, with its empty and error states, then built in both renderers. `site_docs_conformance_test.go` holds the chip variants, the route table and the version entry.
- A new chip variant, token or off-scale value gets its row here first.
- No literal font size, color or spacing outside the tokens. `site_pages_tokens_test.go` holds the line over both stylesheets, `verify_upgrade_boot.js` over the served sheets.
- Any markup the page layer renders, the app renders from the same class names, and the battery asserts it.
- A visual change ships with its golden and baseline update in the same commit.
- A `sitePagesVersion` bump gets an entry in [STATIC-SITE.md](STATIC-SITE.md#page-keys) and its reason in the commit body. Bumps are batched.
- UI strings follow [STYLE.md](STYLE.md): short, no em-dash.
