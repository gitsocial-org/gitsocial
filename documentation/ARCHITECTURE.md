# GitSocial Architecture

Single Go library implementing [GITMSG.md](../specs/GITMSG.md), with thin clients: the [CLI](CLI.md) and the TUI call it directly, JSON-RPC serves it over stdio, and the [static site](STATIC-SITE.md) reads the bucket it publishes to over the [S3 remote](S3.md).

[Development](#development) · [Code Rules](#code-rules) · [Directory Structure](#directory-structure) · [Package Reference](#package-reference) · [Cache](#cache) · [TUI](#tui)

## Development

### Branching and builds

Trunk-based. `main` is the integration branch and stays linear; each feature lives on a `feature/<name>` branch, rebased on `main` and merged fast-forward.

```bash
git switch main && git pull --ff-only                    # refresh before branching and after any merge
git switch -c feature/<name>
git fetch origin && git rebase main                      # keep current with main
git switch main && git merge --ff-only feature/<name>    # integrate
git branch -d feature/<name>
```

```bash
go build -o bin/gitsocial ./cli/gitsocial     # the CLI
go build -o bin/ ./...                        # compile-check everything; mains land in bin/, never the repo root
bin/gitsocial tui
```

- `gitmsg/*` and `gitsocial` are protocol and data branches, not feature branches.
- Give parallel builds distinct output names so they do not clobber each other.
- A review fix folds into the branch commit it corrects; a fix for something already on `main` is its own commit.
- A branch that changes the cache schema runs with its own `--cache-dir`: the first binary to open the shared `~/.cache/gitsocial/cache.db` reseeds it at the new version, and older binaries then refuse it. Delete the cache to rebuild.

### Test and lint

`scripts/check.sh` is the gate, in two tiers. `--quick` runs the prose check, the import check, `go vet`, `golangci-lint` and every test except the guarded ones; the pre-push hook runs it on every push. Without `--quick` it sets `GITSOCIAL_TEST_FULL=1`, so the guarded tests run too, writes the coverage profile and checks the floors; run it before merging to `main` and at release.

```bash
scripts/check.sh --quick                    # the push tier
scripts/check.sh                            # the full tier
git config core.hooksPath scripts/hooks     # install the pre-push hook, once per clone
```

[TESTING.md](TESTING.md) holds the six stages and what fails each, the guarded tests, the coverage ratchet and its child-process credit, the environment variables and the artifact paths. Every baseline is a ratchet: `--update` accepts a count that fell.

### Design notes

A branch that changes `core/objstore`, `core/site`, `core/gitmsg` or `core/cache`, or touches consistency, storage layout or a protocol surface, adds three steps to the branch flow above.

- Before code: a design note, approved. Half a page in `.local/design/<feature>.md` while the branch is open: invariants, each naming its test; who writes and reads each artifact, under what guard; accepted failure modes and their repair; out of scope.
- Once the push tier passes: one medium review of the branch against the note. Triage every finding by cause before fixing any: fix, accept and record in the commit body, or defer to an issue. No second full review.
- On merge the note goes; each invariant lives on as its test, a row in the owning doc's reference tables, and a one-line comment where the code enforces it.

A change to what the site looks like takes [STATIC-SITE-DESIGN.md](STATIC-SITE-DESIGN.md) as its note: the rule lands there first, and it stays after the merge.

Go back to the note instead of another fix when a function is about to be rewritten a second time, a finding is a consequence of a decision, three findings share a cause, or a fix needs a concept the guide does not describe.

### Definition of done

A change is done when every line below holds; a review checks them in order.

- The prose, help text and comments follow [STYLE.md](STYLE.md); every comment is one line, and the reason for the change is in the commit body.
- The quick tier is green on the branch and the full tier before the fast-forward to `main`.
- Every client the feature reaches is updated in the same branch: CLI, TUI, RPC and the site do not learn about a feature at different times.
- The guide says what the reader types or sees, the reference table carries the values, and a spec change lands in `specs/` first.
- A consistency-sensitive change has its design note, its one review and its triage recorded, and the note is gone at merge; a site change uses [STATIC-SITE-DESIGN.md](STATIC-SITE-DESIGN.md) and it stays.
- A number that the plan ratchets, a prose count, a lint ceiling, an import edge, a coverage floor, has moved toward its target or stayed.

### Working in a session

One set of rules for every session.

- Read the files a change touches in full before a note, a review or a fix. Do not work from search hits.
- A consistency-sensitive change gets its [design note](#design-notes) first, then one medium review, findings triaged by cause.
- A mechanical change merges on a green quick tier and one review; the tiers are in [Test and lint](#test-and-lint).
- Every commit carries a body; [STYLE.md](STYLE.md) holds the register and the examples.
- A commit whose net new comment lines outnumber its added code lines fails the push, unless it changes only documentation.
- The branch flow is in [Branching and builds](#branching-and-builds), and the closing list is [Definition of done](#definition-of-done).

## Code Rules

### Layers

```
cli/gitsocial
  library/tui, library/rpc
    library/client, library/import, library/proposals
      library/extensions/*
        library/core/*
          stdlib and the modules in go.mod
```

Each layer imports only the layers below it. `core` imports nothing above itself. Two standing exceptions:

- Extensions import each other: `pm`, `review`, `release` and `memo` import `social` for comments, and `review` imports `pm` for the issues a pull request closes.
- Each extension's `nav.go` imports `tui/tuinav` to register its navigation items. Nothing else in `extensions` imports `tui`.

Inside `core` the packages form a stack, and each imports only what is below it: `log`; `protocol`, `text`, `result`; `cache`, `git`; `storage`, `gitmsg`, `identity`; `settings`, `notifications`; `fetch`; `objstore`, `search`; `site`. `scripts/import-graph.sh --check` reads this sentence and fails on an edge against it. A sub-package sits in its parent's tier, and an edge between a sub-package and its parent is exempt. With no argument the script prints the current edges.

### Do

- Read the relevant spec first: `specs/GITMSG.md`, `specs/GITSOCIAL.md`, `specs/GITPM.md`, `specs/GITRELEASE.md`, `specs/GITREVIEW.md`.
- Follow [STYLE.md](STYLE.md) for prose, help text, comments and commits.
- Start each file with a one-line header comment (`// commits.go - Git commit operations`) and each function with a one-line comment.
- Prefer functions to methods. Methods are for interfaces and for Bubbletea models in `tui`.
- Wrap errors with context: `fmt.Errorf("context: %w", err)`.
- Use `cache.ExecLocked` and `cache.QueryLocked` for every database operation.
- Check for an existing type or function before adding one.

### Never

- Run git from an extension; use `core/git`.
- Create a type for a single use.
- Add package-level mutable state outside the seams below. New state takes a new row and a reason.
- Skip error handling.

| Package | Mutable package-level state |
|---|---|
| `core/git` | the executor seam, the command timeout, the s3 helper alias memo |
| `core/gitmsg` | the per-workdir caches |
| `core/cache` | the database singleton, the extension schema registry |
| `core/log` | the process logger |
| `core/notifications` | the provider registry |
| `core/identity` | the in-flight map, the DNS policy flag |
| `core/identity/forge` | the forge registry |
| `core/objstore` | the two credential warn-once flags |
| `extensions/review` | the fetched-refs memo |
| `tui/tuicore` | the theme struct, the registries (contexts, views, cards, nav targets, message handlers), the width-margin flag |
| `tui/tuicore/diff` | the background-suppression flag |

### Patterns

```
Core packages (cache, git, protocol)  return error
Extension public API (social.*)       return Result[T], built with result.Ok and result.Err
Internal helpers                      return error
```

- Convert to `Result[T]` at the API boundary, where the CLI, TUI and RPC read user-facing error codes.
- A batch operation that continues on failure logs the failure instead of returning it.
- An intentionally ignored error carries a comment.
- One `*cobra.Command` per file under `cli/gitsocial/`, registered in `init()`.
- A CLI command returns its exit code as an error through `RunE`, and `main` exits once.
- A TUI view implements the `View` interface (`Update`, `Render`); examples in `library/tui/tuicore/`.

## Directory Structure

```
gitsocial/                     # module github.com/gitsocial-org/gitsocial
├── cli/gitsocial/             # the CLI; builds the binary
├── library/
│   ├── core/
│   │   ├── git/               # git operations
│   │   ├── protocol/          # GitMsg parsing and refs
│   │   ├── gitmsg/            # protocol-level storage (config refs, lists, forks, push)
│   │   ├── cache/             # SQLite
│   │   ├── storage/           # bare repo management
│   │   ├── objstore/          # S3 client and the s3:// remote helper
│   │   ├── site/              # the static site: its assets, artifacts and pages
│   │   ├── fetch/             # fetch orchestration and processing
│   │   ├── identity/          # identity verification; forge/ holds the forge adapters
│   │   ├── notifications/     # notification aggregation
│   │   ├── search/            # cross-extension search
│   │   ├── settings/          # user settings and config paths
│   │   ├── log/, text/, result/
│   ├── extensions/            # social, pm, release, review, memo
│   ├── proposals/             # cross-repo proposals: accept and decline
│   ├── import/                # forge import; github/ and gitlab/ adapters
│   ├── client/                # the fetch and push sequences the thin clients run
│   ├── rpc/                   # JSON-RPC server
│   ├── tui/                   # TUI
│   └── internal/testutil/     # shared test fixtures
├── documentation/
├── scripts/                   # check.sh, prose-check.sh, import-graph.sh, test.sh, coverage.sh, release.sh, install.sh, site-test.sh, site-coverage.sh
└── specs/
```

Outside the tree:

```
~/.config/gitsocial/           # honors XDG_CONFIG_HOME
├── credentials.json           # S3 credentials per endpoint host
└── personal/                  # the personal bare repo: settings and personal memos (GITSOCIAL_PERSONAL_REPO overrides)

~/.cache/gitsocial/            # --cache-dir overrides
├── cache.db                   # SQLite
├── repositories/              # bare clones of followed repositories
├── forks/                     # bare clones of registered forks
├── imports/                   # import mapping files, one per repository URL
└── memo/session/              # session memo repos
```

## Package Reference

| Package | Key Types | Key Exports |
|---------|-------|---------|
| `core/git`<br>Git operations | `Commit`, `FileDiff`, `Hunk`, `DiffLine`, `DiffStats` | `GetCommits`, `CreateCommit`, `ReadRef`, `WriteRef`, `GetDiff`, `GetFileDiff`, `GetFileContent`, `GetDiffStats`, `MergeBranches`, `SquashMerge`, `RebaseMerge`, `ForceMerge`, `RebaseBranch`, `RangeDiff`, `PatchesEqual`, `GetBehindCount`, `GetMergeBase`, `GetUserName`, `GetGitConfig`, `GetCommitSignerKey` |
| `core/protocol`<br>Message parsing | `Header`, `Message`, `Origin`, `Trailer` | `ParseMessage`, `ParseHeader`, `CreateHeader`, `FormatMessage`, `ParseRef`, `CreateRef`, `FormatShortRef`, `QuoteContent`, `ApplyOrigin`, `ExtractTrailers`, `Trailer` |
| `core/cache`<br>SQLite operations | `Repository`, `Commit`, `TrailerRef` | `Open`, `DB`, `ExecLocked`, `QueryLocked`, `InsertCommits`, `FilterUnfetchedCommitsByRepo`, `MarkCommitsStaleByRepo`, `ResetRepositoryData`, `ToNullString`, `ToNullInt64`, `GetTrailerRefsTo`, `TrailerRef` |
| `core/gitmsg`<br>Protocol-level storage | | `ResolveRepoURL`, `Push`, `ReadExtConfig`, `WriteList`, `GetHistory`, `GetExtBranch`, `IsBranchConfigurable`, `IsExtInitialized`, `GetForks`, `AddFork`, `AddForks`, `RemoveFork` |
| `core/storage`<br>Bare repo management | | `EnsureRepository`, `GetStorageDir`, `FetchRepository` |
| `core/objstore`<br>S3 remote | `Client`, `Config`, `HelperEnv`, `Progress`, `LocalCommitSource`, `PushOutcome`, `PostPushHook`, `SiteOverride` | `NewClient`, `ClientForRemote`, `ParseS3URL`, `RunHelper`, `HelperEnvFromOS`, `ListRemoteRefs`, `ReadRemoteRefs`, `RebuildRefManifest`, `LogDumbTransportInfo`, `RefsHeadDigest`, `ReadPackedObject`, `ThinUpstreamURL`, `CompressJSON`, `ReadCompressedJSON`, `PutCompressed`, `UploadConcurrency`, `RunParallel`, `PushArtifactObjects`, `PutObjectToRemote` |
| `core/site`<br>Static site | `SiteCustomization` | `Rebuild`, `PostPushMaintenance`, `SetRemoteHead`, `WriteSiteStats`, `ReadWorkspaceSiteCustomization`, `WriteWorkspaceSiteCustomization`, `NormalizeSiteURL`, `NormalizeSiteImage`, `NormalizeSiteGlobs`, `ValidSiteAccent`, `ValidSiteFavicon` |
| `core/fetch`<br>Fetch orchestration | | `FetchAll`, `FetchRepository`, `FetchForks`, `CommitProcessor`, `PostFetchHook` |
| `core/settings`<br>User settings | | `Get`, `Set`, `ListAll` |
| `core/search`<br>Cross-extension search | | `Search`, `Params`, `Result`, `Item`, `Group`, `GroupedItem`, `FormatResult`, `IsValidGroupBy` |
| `core/result`<br>Result type | `Result[T]`, `Error` | `Ok`, `Err`, `ErrWithDetails` |
| `core/notifications`<br>Notification aggregation | `Notification`, `Provider`, `Filter` | `RegisterProvider`, `GetAll`, `GetUnreadCount`, `MarkAsRead`, `MarkAsUnread`, `MarkAllAsRead`, `MarkAllAsUnread`, `MentionProcessor`, `ExtractMentions`, `TrailerProcessor` |
| `core/identity`<br>Identity verification | `Identity`, `ResolvedIdentity`, `DNSIdentity`, `Binding`, `Source`, `VerifyCandidate` | `VerifyBinding`, `IsVerified`, `IsVerifiedCommit`, `LookupBinding`, `VerifyCandidates`, `NormalizeSignerKey`, `NormalizeEmail`, `ResolveIdentity` |
| `core/identity/forge`<br>Forge adapters | `Forge`, `GPGKey`, `CommitVerification` | `Forge`, `Register`, `Lookup`, `LookupForRepo`, `ParseRepoURL`, `NewGitHub`, `GPGKey`, `CommitVerification` |
| `extensions/social`<br>Posts, lists, timeline | `Post`, `SocialItem` | `GetPosts`, `CreatePost`, `CreateComment`, `GetComments`, `Fetch` |
| `extensions/pm`<br>Issues, milestones, sprints | `Issue`, `Milestone`, `Sprint`, `PMNotification` | `GetIssues`, `CreateIssue`, `GetMilestones`, `GetSprints`, `MessageToPMItem`, `FetchRepository`, `Processors` |
| `extensions/release`<br>Releases | `Release`, `ReleaseItem`, `ReleaseNotification` | `CreateRelease`, `EditRelease`, `GetReleases`, `GetSingleRelease`, `MessageToReleaseItem`, `FetchRepository`, `Processors` |
| `extensions/review`<br>Pull requests, feedback | `PullRequest`, `Feedback`, `ReviewSummary`, `StackEntry`, `ReviewNotification` | `CreatePR`, `GetPR`, `UpdatePR`, `MergePR`, `ClosePR`, `RetractPR`, `MarkReady`, `ConvertToDraft`, `UpdatePRTips`, `SyncPRBranch`, `GetPRVersions`, `ComparePRVersions`, `GetVersionAwareReviews`, `CreateFeedback`, `GetReviewSummary`, `MessageToReviewItem`, `GetPullRequests`, `GetPullRequestsWithForks`, `ResolvePRDiff`, `GetStack`, `RebaseStack`, `Processors` |
| `extensions/memo`<br>Memos across tiers | `Memo`, `MemoItem`, `Tier`, `SessionInfo` | `CreateMemo`, `EditMemo`, `RetractMemo`, `PromoteMemo`, `ListMemos`, `GetSingleMemo`, `InitProject`, `InitPersonal`, `InitSession`, `ListSessions`, `GCSession`, `PushPersonal`, `FetchPersonal`, `PushSession`, `FetchSession`, `SyncAllTierReposToCache`, `AddInherit`, `RemoveInherit`, `ListInherits`, `IsInherited` |
| `client`<br>Fetch and push sequences | `FetchOptions`, `Options`, `Result`, `SiteOutcome` | `Fetch`, `SyncWorkspace`, `ResolveRemotes`, `Preview`, `Push`, `PushAll`, `PublishSite` |
| `proposals`<br>Cross-repo proposals | `Outcome` | `Accept`, `Decline` |
| `import`<br>Forge import | `SourceAdapter`, `Stats`, `MappingFile` | `Run`, `SourceAdapter`, `ReadMapping`, `WriteMapping`, `MappingKey`, `ResolveHost`, `MapLabels` |
| `import/github`<br>GitHub adapter | | `New`, `CheckGH`, `Adapter.FetchPM`, `Adapter.FetchReleases`, `Adapter.FetchReview`, `Adapter.FetchSocial` |

| Term | Context | Meaning |
|------|---------|---------|
| `original` | GITSOCIAL field | the post being commented on, reposted or quoted |
| `canonical` | versioning | the first version of a message |
| `raw` | versioning | the commit's own message, before any edit applies |
| `edits` | GITMSG field | the reference to the canonical version an edit replaces |
| `publish` | the site | the site step of a push, guarded by the `site.publish` config key |
| `rebuild` | the site | the verb of the site step: it uploads the shell and the artifacts, and sends no refs |

A push is the sequence: the data push, then the site step after it.

## Cache

Storage under `repositories/` can be deleted at any time; what to fetch is decided from `cache.db`, not from storage. The cache is append-only. A commit that leaves its source branch (rebase, force-push) is marked `stale_since` by `cache.MarkCommitsStale` or `MarkCommitsStaleByRepo`; stale commits leave timeline and list queries but stay visible, dimmed, in thread and detail views.

SQLite: WAL, 64 MB page cache, temp store in memory, 16 connections, 256 MB mmap.

### Schema

Every extension table is keyed by `(repo_url, hash, branch)` into `core_commits` and carries the extension's prefix. Column lists are in `core/cache/db.go` and each extension's `schema.go`.

- `core_commits(repo_url, hash, branch, author_name, author_email, message, timestamp, edits, is_virtual, origin_author_name, origin_author_email, ...)` plus the generated `effective_*` columns
- `core_commits_version(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)`: edit to canonical, authoritative for versioning
- `core_repositories`, `core_repository_meta`, `core_sync_tips`: followed and workspace repositories, their metadata, and the last synced tips
- `core_lists`, `core_list_repositories`: lists and their members
- `core_fetch_ranges`: fetched time windows per repository
- `core_notification_reads`, `core_mentions`, `core_labels`, `core_trailer_refs`: read markers, `@` mentions, labels, and `Closes:`/`Refs:` trailers per commit
- `core_identity_dns` (24 h TTL), `core_verified_bindings`: identity caches; see [IDENTITY.md](IDENTITY.md)
- `core_edit_acceptances`, `core_edit_declines`: outcomes of cross-repo proposals
- `social_items`, `social_interactions` (recounted from live items on every write), `social_followers`, `social_repo_lists`, `social_repo_list_repositories`
- `pm_items`, `pm_assignees`, `pm_links` (blocks, blocked-by, related)
- `review_items`, `review_reviewers`, `review_branch_observations` (live tips of every branch an open PR points at, refreshed after fetch)
- `release_items`, `release_sbom_cache`
- `memo_items`

`core_commits.edits` stores the raw header value; `core_commits_version` is authoritative. Use `cache.ResolveToCanonical` and `cache.GetLatestVersion`.

Edit resolution is gated to same-repo edits (GITMSG.md §1.5), so a cross-repo edit is an inert proposal until the owner acts. `proposals.Accept` writes the owner's own same-repo mirror edit carrying `accepts=<proposal>`, which wins resolution and derives `core_edit_acceptances` on processing. `proposals.Decline` publishes a marker at `refs/gitmsg/core/declines/*`. Both clear the proposer's marker; accept takes precedence.

### Resolved views

`core_commits` carries generated `effective_message`, `effective_author_name`, `effective_author_email` and `effective_timestamp` columns that take the latest edit's content and the origin fields over the raw ones. Each extension has a `<ext>_items_resolved` view joining its table onto `core_commits` and projecting those columns under the display names:

```sql
CREATE VIEW {ext}_items_resolved AS
SELECT c.effective_message AS resolved_message, c.effective_author_name AS author_name, c.effective_timestamp AS timestamp, ...,
       COALESCE(e.type, 'default') AS type, e.field1, ...
FROM core_commits c
LEFT JOIN {ext}_items e ON c.repo_url = e.repo_url AND c.hash = e.hash AND c.branch = e.branch;
```

The denormalized columns `resolved_message`, `has_edits` and `is_retracted` are written only by `applyEditToCanonical` in `core/cache/versions.go`.

Use the view when the WHERE clause is on `core_commits` columns. Join `core_commits` to the extension table directly when the WHERE clause is selective on extension columns (`pm_items.state = 'open'`) or the query is a recursive CTE over extension relationships; otherwise the planner scans `core_commits`. `social.GetComments`, and the thread and notification readers inside `social`, are the examples.

### Refs and keys

References are `[repo_url]#<type>:<value>`: `https://github.com/user/repo#commit:abc123def456` or, workspace-relative, `#commit:abc123def456`. Types: `commit`, `branch`, `tag`, `file`, `list`.

A repository has two names: the identity, `protocol.NormalizeURL` of any spelling, which keys every cache row, ref path and comparison, and the address, the spelling the user gave, which is stored in the list member or fork ref and handed to git. A repository has an identity and a person has a verified binding; the word is not free for a third use.

A virtual commit is one referenced by a `GitMsg-Ref` trailer but not yet fetched; it is stored with `is_virtual = 1` and full metadata, and flips to `0` when fetched.

State refs under `refs/gitmsg/`:

- `refs/gitmsg/<ext>/config`: per-extension JSON config
- `refs/gitmsg/core/forks/<urlHash>`: one ref per registered fork
- `refs/gitmsg/core/declines/<hash>`: one ref per declined proposal, subject is the proposal ref
- `refs/gitmsg/<ext>/lists/<name>/_meta` and `.../items/<refHash>`: list metadata and one ref per member

Per-element refs have no shared write target, so concurrent adds from several clones do not collide. Metadata lives under `_meta` because git refuses a child ref under a same-named parent ref.

Fork refs sit outside `refs/gitmsg/`, keyed by `fetch.URLHash` of the fork identity:

- `refs/forks/<hash>/gitmsg/*`: the protocol branches a registered fork publishes, written by `core/fetch`
- `refs/fork/<hash>/<branch>`: the code branches a cross-fork diff borrows, written by `extensions/review`

### Fetch rules

| Repository | Cache | Storage |
|---|---|---|
| Workspace | full history, all branches | the workdir |
| Followed with `#branch:*` | full history, all branches | persistent |
| Followed on one branch | full history, incremental | persistent |
| Not followed | a 30-day window | may be deleted at any time |

All-branch following stores each commit under its real refname. The workspace always follows all branches. Deduplication and stale marking work per repository through `FilterUnfetchedCommitsByRepo` and `MarkCommitsStaleByRepo`. Switching a repository between one branch and `*` runs `cache.ResetRepositoryData`; the next fetch rebuilds it.

### Extension rules

- Tables carry the `<ext>_` prefix and key into `core_commits` by `(repo_url, hash, branch)`.
- Content lives on `gitmsg/<ext>`, named by one constant per extension. Only `social` resolves a configured branch, through `gitmsg.GetExtBranch`; the other extensions ignore the `branch` key, which `init` still writes as the initialized marker.
- An extension joins the fetch in `library/client`, which lists every extension's `Processors`, `SyncWorkspaceBatch` and `BackfillSpec`. `pm`, `release` and `review` name their own `Processors` again in their `FetchRepository`, which serves the `--repo` form of a CLI list command.
- An extension costs ten imports: `cache`, `fetch`, `git`, `gitmsg`, `log`, `notifications`, `protocol` and `result`, plus `social` for comments and `tui/tuicore` for its navigation items. It registers a notification provider with `notifications.RegisterProvider`.
- `core/search` names every extension's table itself, in `query.go` and `group.go`, and `core/cache` names them in `analytics.go`, `clear.go`, `commits.go`, `stats.go` and `versions.go`, where `notifications` takes a registration. A sixth extension edits both packages; the asymmetry stays until one exists.
- Core tables are read-only for extensions; use the cache APIs.
- Known limits: `storage.GetStorageDir` hashes the URL only, so one URL on two branches shares storage; check `meta.HasCommits` before reading timestamps.

## TUI

Two panels on Bubbletea: navigation on the left, content on the right. Keys are in [TUI-KEYS.md](TUI-KEYS.md), layouts in [TUI-DIAGRAMS.md](TUI-DIAGRAMS.md), the headless test suite in [TUI-TESTS.md](TUI-TESTS.md).

```
library/tui/
├── app.go, host.go      # the tea.Model, view dispatch, shared state
├── tuicore/             # infrastructure: utils, components, registries, the message bus
├── tuiviews/            # the core routable views
├── tuinav/              # navigation items and the registry extensions register into
├── tuisocial/, tuipm/, tuirelease/, tuireview/, tuimemo/, tuiproposal/
├── tuikeydoc/           # keybinding documentation generator
└── test/                # headless integration tests
```

| Prefix | Purpose | Example |
|--------|---------|---------|
| `view_` | a routable view, in `tuiviews/` or an extension's `tui<ext>/` | `view_timeline.go` |
| `component_` | a reusable stateful component | `component_nav_panel.go` |
| `registry_` | a global registry | `registry_nav_target.go` |
| `form_` | a modal form | `form_issue.go` |
| `version_item_` | a history-picker version item | `version_item_issue.go` |
| `util_` | helpers, and the shared app state they take: `State`, `Router`, `theme` | `util_render.go` |

The table governs `library/tui/*/`, not `test/` and not the `tuicore/diff/` sub-package. A `component_` renders and takes keys; shared app state stays a `util_`.

A new extension gets a `tui/tui<ext>/` directory with its `view_*.go` files and a `util_register.go` exposing `Register(host)`, called from `app.go`.
