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
- A branch that changes the cache schema runs with its own `--cache-dir`: the first binary to open the shared `~/.cache/gitsocial/cache.db` upgrades it, and older binaries then refuse it. Delete the cache to rebuild.

### Test and lint

`scripts/check.sh` is the gate, in two tiers. `--quick` runs the prose check, `go vet`, `golangci-lint` and every test except the guarded ones; the pre-push hook runs it on every push, about 90 s warm when every package reruns and less after a change in one package. Without `--quick` it sets `GITSOCIAL_TEST_FULL=1`, so the guarded tests run too, about 4 min warm; run that before merging to `main` and at release. A missing `golangci-lint` fails unless `--skip-lint` is passed.

The guarded tests call `fullTierOnly`: the TUI matrices `TestSmoke`, `TestSequence` and `TestGolden/LayoutProperties`, the CLI `--json` walk `TestCommandTreeJSONOutput`, and the `TestS3Helper_*` child-process tests. `-race` and the browser site battery run at release from `scripts/release.sh`. `-short` skips 66 real-git subtests and is a local smoke run, never a tier.

`scripts/prose-check.sh` is stage 0. It counts [STYLE.md](STYLE.md) violations and fails when a count rises above `scripts/prose-baseline.txt`; `--update` accepts lowered counts, `--list <rule>` prints the offending lines. Commit subjects over 72 characters in the pushed range fail outright.

```bash
scripts/check.sh --quick                    # the push tier
scripts/check.sh                            # the full tier
scripts/check.sh -short ./...               # extra args go to the test stage; a smoke run, not a gate run
git config core.hooksPath scripts/hooks     # install the pre-push hook, once per clone
GITSOCIAL_SKIP_GATE=1 git push              # skip the gate once
scripts/test.sh -run TestSmoke ./library/tui/test/          # go test -json with streamed per-test progress
scripts/coverage.sh                         # statement-weighted coverage with -coverpkg=./..., into .test-artifacts/coverage/
go test -tags sitetest -timeout 30m ./library/core/objstore/   # the browser site battery; needs node
```

Coverage is a floor: the S3 helper tests run the helper as a child process, and the browser suites run under node, so neither is credited.

## Code Rules

### Layers

```
cli/gitsocial
  library/tui, library/rpc
    library/clientfetch, library/clientpush, library/import, library/proposals
      library/extensions/*
        library/core/*
          stdlib and the modules in go.mod
```

Each layer imports only the layers below it. `core` imports nothing above itself. Two standing exceptions:

- Extensions import each other: `pm`, `review`, `release` and `memo` import `social` for comments, and `review` imports `pm` for the issues a pull request closes.
- Each extension's `nav.go` imports `tui/tuicore` to register its navigation items. Nothing else in `extensions` imports `tui`.

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
- Add package-level mutable state. The per-workdir caches in `core/gitmsg` and the in-flight map in `core/identity` are the standing exceptions.
- Skip error handling.

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
│   │   ├── objstore/          # S3 client, remote helper, static site
│   │   ├── fetch/             # fetch orchestration and processing
│   │   ├── identity/          # identity verification; forge/ holds the forge adapters
│   │   ├── notifications/     # notification aggregation
│   │   ├── search/            # cross-extension search
│   │   ├── settings/          # user settings and config paths
│   │   ├── log/, text/, result/
│   ├── extensions/            # social, pm, release, review, memo
│   ├── proposals/             # cross-repo proposals: accept and decline
│   ├── import/                # forge import; github/ and gitlab/ adapters
│   ├── clientfetch/           # fetch orchestration for the thin clients
│   ├── clientpush/            # push orchestration for the thin clients
│   ├── rpc/                   # JSON-RPC server
│   ├── tui/                   # TUI
│   └── internal/testutil/     # shared test fixtures
├── documentation/
├── scripts/                   # check.sh, prose-check.sh, test.sh, coverage.sh, release.sh, install.sh, site-test.sh
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
| `core/git`<br>Git operations | `Commit`, `FileDiff`, `Hunk`, `DiffLine`, `DiffStats` | `GetCommits`, `CreateCommit`, `ReadRef`, `WriteRef`, `GetDiff`, `GetFileDiff`, `GetFileContent`, `GetDiffStats`, `MergeBranches`, `SquashMerge`, `RebaseMerge`, `ForceMerge`, `RebaseBranch`, `RangeDiff`, `PatchesEqual`, `GetBehindCount`, `GetMergeBase`, `GetUserName`, `GetGitConfig`, `CreateSignedCommitTree`, `VerifyCommitSignature`, `GetCommitSignerKey` |
| `core/protocol`<br>Message parsing | `Header`, `Message`, `Origin`, `Trailer` | `ParseMessage`, `ParseHeader`, `CreateHeader`, `FormatMessage`, `ParseRef`, `CreateRef`, `FormatShortRef`, `QuoteContent`, `ApplyOrigin`, `ExtractTrailers`, `Trailer`, `IsClosingTrailer` |
| `core/cache`<br>SQLite operations | `Repository`, `Commit`, `TrailerRef` | `Open`, `DB`, `ExecLocked`, `QueryLocked`, `InsertCommits`, `FilterUnfetchedCommitsByRepo`, `MarkCommitsStaleByRepo`, `ResetRepositoryData`, `RegisterMigration`, `ToNullString`, `ToNullInt64`, `GetTrailerRefsTo`, `TrailerRef` |
| `core/gitmsg`<br>Protocol-level storage | | `ResolveRepoURL`, `Push`, `ReadExtConfig`, `WriteList`, `GetHistory`, `GetExtBranch`, `IsExtInitialized`, `GetForks`, `AddFork`, `AddForks`, `RemoveFork` |
| `core/storage`<br>Bare repo management | | `EnsureRepository`, `GetStorageDir`, `FetchRepository` |
| `core/objstore`<br>S3 remote and site | `Client`, `Config`, `Capability`, `HelperEnv` | `NewClient`, `ParseS3URL`, `RunHelper`, `HelperEnvFromOS`, `ListRemoteRefs`, `PushSite`, `PushArtifactObjects`, `PutObjectToRemote` |
| `core/fetch`<br>Fetch orchestration | | `FetchAll`, `FetchRepository`, `FetchForks`, `CommitProcessor`, `PostFetchHook` |
| `core/settings`<br>User settings | | `Get`, `Set`, `ListAll` |
| `core/search`<br>Cross-extension search | | `Search`, `Params`, `Result`, `Item`, `Group`, `GroupedItem`, `FormatResult`, `IsValidGroupBy` |
| `core/result`<br>Result type | `Result[T]`, `Error` | `Ok`, `Err`, `ErrWithDetails` |
| `core/notifications`<br>Notification aggregation | `Notification`, `Provider`, `Filter` | `RegisterProvider`, `GetAll`, `GetUnreadCount`, `MarkAsRead`, `MarkAsUnread`, `MarkAllAsRead`, `MarkAllAsUnread`, `MentionProcessor`, `ExtractMentions`, `TrailerProcessor` |
| `core/identity`<br>Identity verification | `Identity`, `ResolvedIdentity`, `DNSIdentity`, `Binding`, `Source`, `VerifyCandidate` | `VerifyBinding`, `IsVerified`, `IsVerifiedCommit`, `LookupBinding`, `VerifyCandidates`, `NormalizeSignerKey`, `NormalizeEmail`, `ResolveIdentity` |
| `core/identity/forge`<br>Forge adapters | `Forge`, `GPGKey`, `CommitVerification` | `Forge`, `Register`, `Lookup`, `LookupForRepo`, `ParseRepoURL`, `NewGitHub`, `GPGKey`, `CommitVerification` |
| `extensions/social`<br>Posts, lists, timeline | `Post`, `SocialItem` | `GetPosts`, `CreatePost`, `GetTimeline`, `Fetch` |
| `extensions/pm`<br>Issues, milestones, sprints | `Issue`, `Milestone`, `Sprint`, `PMNotification` | `GetIssues`, `CreateIssue`, `GetMilestones`, `GetSprints`, `MessageToPMItem`, `FetchRepository`, `Processors` |
| `extensions/release`<br>Releases | `Release`, `ReleaseItem`, `ReleaseNotification` | `CreateRelease`, `EditRelease`, `GetReleases`, `GetSingleRelease`, `MessageToReleaseItem`, `FetchRepository`, `Processors` |
| `extensions/review`<br>Pull requests, feedback | `PullRequest`, `Feedback`, `ReviewSummary`, `StackEntry`, `ReviewNotification` | `CreatePR`, `GetPR`, `UpdatePR`, `MergePR`, `ClosePR`, `RetractPR`, `MarkReady`, `ConvertToDraft`, `UpdatePRTips`, `SyncPRBranch`, `GetPRVersions`, `ComparePRVersions`, `GetVersionAwareReviews`, `CreateFeedback`, `GetReviewSummary`, `MessageToReviewItem`, `FetchRepository`, `GetPullRequestsWithForks`, `GetStack`, `GetDependents`, `Processors` |
| `extensions/memo`<br>Memos across tiers | `Memo`, `MemoItem`, `Tier`, `SessionInfo` | `CreateMemo`, `EditMemo`, `RetractMemo`, `PromoteMemo`, `ListMemos`, `GetSingleMemo`, `InitProject`, `InitPersonal`, `InitSession`, `ListSessions`, `GCSession`, `PushPersonal`, `FetchPersonal`, `PushSession`, `FetchSession`, `SyncAllTierReposToCache`, `AddInherit`, `RemoveInherit`, `ListInherits`, `IsInherited` |
| `proposals`<br>Cross-repo proposals | `Outcome` | `Accept`, `Decline` |
| `import`<br>Forge import | `SourceAdapter`, `Stats`, `MappingFile` | `Run`, `SourceAdapter`, `ReadMapping`, `WriteMapping`, `MappingKey`, `ResolveHost`, `MapLabels` |
| `import/github`<br>GitHub adapter | | `New`, `CheckGH`, `Adapter.FetchPM`, `Adapter.FetchReleases`, `Adapter.FetchReview`, `Adapter.FetchSocial` |

| Term | Context | Meaning |
|------|---------|---------|
| `original` | GITSOCIAL field | the post being commented on, reposted or quoted |
| `canonical` | versioning | the first version of a message |
| `edits` | GITMSG field | the reference to the canonical version an edit replaces |

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
- `social_items`, `social_interactions`, `social_followers`, `social_repo_lists`, `social_repo_list_repositories`, `social_counted_sources`, `social_notification_reads`
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

Use the view when the WHERE clause is on `core_commits` columns. Join `core_commits` to the extension table directly when the WHERE clause is selective on extension columns (`pm_items.state = 'open'`) or the query is a recursive CTE over extension relationships; otherwise the planner scans `core_commits`. `social.GetThread` and `social.GetNotifications` are the examples.

### Refs and keys

References are `[repo_url]#<type>:<value>`: `https://github.com/user/repo#commit:abc123def456` or, workspace-relative, `#commit:abc123def456`. Types: `commit`, `branch`, `tag`, `file`, `list`.

A virtual commit is one referenced by a `GitMsg-Ref` trailer but not yet fetched; it is stored with `is_virtual = 1` and full metadata, and flips to `0` when fetched.

State refs under `refs/gitmsg/`:

- `refs/gitmsg/<ext>/config`: per-extension JSON config
- `refs/gitmsg/core/forks/<urlHash>`: one ref per registered fork
- `refs/gitmsg/core/declines/<hash>`: one ref per declined proposal, subject is the proposal ref
- `refs/gitmsg/<ext>/lists/<name>/_meta` and `.../items/<refHash>`: list metadata and one ref per member

Per-element refs have no shared write target, so concurrent adds from several clones do not collide. Metadata lives under `_meta` because git refuses a child ref under a same-named parent ref.

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
- Core tables are read-only for extensions; use the cache APIs.
- Known limits: `storage.GetStorageDir` hashes the URL only, so one URL on two branches shares storage; check `meta.HasCommits` before reading timestamps.

## TUI

Two panels on Bubbletea: navigation on the left, content on the right. Keys are in [TUI-KEYS.md](TUI-KEYS.md), layouts in [TUI-DIAGRAMS.md](TUI-DIAGRAMS.md), the headless test suite in [TUI-TESTS.md](TUI-TESTS.md).

```
library/tui/
├── app.go, host.go      # the tea.Model, view dispatch, shared state
├── tuicore/             # infrastructure and core views
├── tuisocial/, tuipm/, tuirelease/, tuireview/, tuimemo/, tuiproposal/
├── tuikeydoc/           # keybinding documentation generator
└── test/                # headless integration tests
```

| Prefix | Purpose | Example |
|--------|---------|---------|
| `view_` | a routable view | `view_timeline.go` |
| `component_` | a reusable stateful component | `component_nav_panel.go` |
| `registry_` | a global registry | `registry_nav.go` |
| `form_` | a modal form | `form_issue.go` |
| `version_item_` | a history-picker version item | `version_item_issue.go` |
| `util_` | stateless helpers | `util_render.go` |

A new extension gets a `tui/tui<ext>/` directory with its `view_*.go` files and a `util_register.go` exposing `Register(host)`, called from `app.go`.
