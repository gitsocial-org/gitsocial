# GitSocial Architecture

GitSocial is one Go library that implements [GITMSG.md](../specs/GITMSG.md), with thin clients on top. The [CLI](CLI.md) and the TUI call it directly, JSON-RPC serves it over stdio, and the [static site](STATIC-SITE.md) reads the bucket that it publishes to over the [S3 remote](S3.md).

[Development](#development) · [Code Rules](#code-rules) · [Directory Structure](#directory-structure) · [Package Reference](#package-reference) · [Cache](#cache) · [TUI](#tui)

## Development

### Branching and builds

Development is trunk-based. `main` is the integration branch and stays linear; each feature is on a `feature/<name>` branch, rebased on `main` and merged fast-forward.

```bash
git switch main && git pull --ff-only                    # refresh before branching and after any merge
git switch -c feature/<name>
git fetch origin && git rebase main                      # keep current with main
git switch main && git merge --ff-only feature/<name>    # integrate
git branch -d feature/<name>
```

```bash
git worktree add .local/worktrees/<name> -b feature/<name> main   # a parallel branch in its own directory
git worktree remove .local/worktrees/<name>                       # after the merge
```

```bash
go build -o bin/gitsocial ./cli/gitsocial     # the CLI
go build -o bin/ ./...                        # compile-check everything; mains land in bin/, never the repo root
bin/gitsocial tui
```

- `gitmsg/*` are content branches, not feature branches.
- A worktree for a parallel branch is at `.local/worktrees/<name>`, the same as a design note is at `.local/design/`.
- Give each parallel build a different output name, so one build does not overwrite the output of another.
- Put a review fix into the branch commit that it corrects; a fix for a change that is already on `main` is a separate commit.
- If a branch changes the cache schema, run it with its own `--cache-dir`: the first binary that opens the shared `~/.cache/gitsocial/cache.db` reseeds it at the new version and keeps only the read markers, and older binaries then refuse it. To rebuild, delete the cache.

### Test and lint

`scripts/check.sh` is the gate, in two tiers: the quick tier (`--quick`) and the full tier. The pre-push hook runs the quick tier on each push that carries code; run the full tier before you merge to `main` and at release. [TESTING.md](TESTING.md#tiers) tells what each tier runs.

```bash
scripts/check.sh --quick                    # the push tier
scripts/check.sh                            # the full tier
git config core.hooksPath scripts/hooks     # install the pre-push hook, once per clone
```

[TESTING.md](TESTING.md) has the six stages and the failures of each, the guarded tests, the coverage ratchet with child processes, the environment variables and the artifact paths. Each baseline is a ratchet: a count can go down but not up, and `--update` accepts a lower count.

### Design notes

A branch that changes `core/objstore`, `core/site`, `core/gitmsg` or `core/cache`, or that changes what readers see during concurrent writes, where data is stored, or what the protocol writes, adds three steps to the [branch flow](#branching-and-builds).

- Before code: an approved design note, half a page in `.local/design/<feature>.md` while the branch is open, with these parts:
  - the invariants, each with the name of its test
  - who writes and reads each artifact, and under what guard
  - the accepted failure modes and their repair
  - what is out of scope
- After the quick tier passes: one medium review of the branch against the note. Triage each finding by cause before you fix any of them: fix it, accept it and record it in the commit body, or defer it to an issue. No second full review.
- On merge, delete the note; each invariant remains as its test, a row in the reference tables of the owning doc, and a one-line comment where the code enforces it.

A change to the appearance of the site needs no note; it follows the rules in [STATIC-SITE.md](STATIC-SITE.md#design).

Go back to the note, not to another fix, when one of these is true:

- a function is about to be rewritten a second time
- a finding is a consequence of a decision
- three findings share a cause
- a fix needs a concept that the guide does not describe

### Definition of done

A change is done when each of these lines is true; a review checks them in order.

- The prose, help text and comments follow [STYLE.md](STYLE.md); each comment is one line, and the reason for the change is in the commit body.
- The quick tier is green on the branch and the full tier before the fast-forward to `main`.
- Each client that the feature affects is updated in the same branch, so the CLI, the TUI, RPC and the site get a feature at the same time.
- The guide says what the reader types or sees, the reference table carries the values, and a spec change goes into `specs/` first.
- A change that needs a [design note](#design-notes) has its note, its one review and its triage recorded, and the note is deleted at merge.
- Each ratcheted number (a prose count, a lint ceiling, an import edge, a coverage floor) has moved toward its target or has not changed.

### Working in a session

One set of rules for every session.

- Read the files a change touches in full before a note, a review or a fix. Do not work from search hits.
- A change that needs a [design note](#design-notes) gets the note first, then one medium review, findings triaged by cause.
- A mechanical change merges on a green quick tier and one review; the tiers are in [Test and lint](#test-and-lint).
- Each commit has a body; [STYLE.md](STYLE.md) has the register and the examples.
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

- Run git from an extension; extensions use `core/git`.
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
- A TUI view implements the `View` interface (`Update`, `Render`), with examples in `library/tui/tuicore/`.

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
| `core/git`<br>Git operations | `Commit`, `FileDiff`, `Hunk`, `DiffLine`, `DiffStats` | `GetCommits`, `CreateCommit`, `ReadRef`, `WriteRef`, `GetDiff`, `WorkingStatus`, `GetWorkingFileDiff`, `StageFiles`, `UnstageFiles`, `CommitIndex`, `GetFileDiff`, `GetFileContent`, `ListTree`, `ObjectType`, `ListTags`, `TagOrder`, `CompareTagsDesc`, `CountCommits`, `GetDiffStats`, `MergeBranches`, `SquashMerge`, `RebaseMerge`, `ForceMerge`, `RebaseBranch`, `RangeDiff`, `PatchesEqual`, `GetBehindCount`, `GetMergeBase`, `GetUserName`, `GetGitConfig`, `GetCommitSignerKey` |
| `core/protocol`<br>Message parsing | `Header`, `Message`, `Origin`, `Trailer` | `ParseMessage`, `ParseHeader`, `CreateHeader`, `FormatMessage`, `ParseRef`, `CreateRef`, `FormatShortRef`, `QuoteContent`, `ApplyOrigin`, `ExtractTrailers`, `Trailer` |
| `core/cache`<br>SQLite operations | `Repository`, `Commit`, `TrailerRef` | `Open`, `DB`, `ExecLocked`, `QueryLocked`, `InsertCommits`, `FilterUnfetchedCommitsByRepo`, `MarkCommitsStaleByRepo`, `MarkCommitsStaleByHome`, `GetCommitOnAnyBranch`, `ResetRepositoryData`, `ToNullString`, `ToNullInt64`, `GetTrailerRefsTo`, `TrailerRef`, `GetRepositoryBranches` |
| `core/gitmsg`<br>Protocol-level storage | | `ResolveRepoURL`, `Push`, `ReadExtConfig`, `WriteList`, `GetHistory`, `GetExtBranch`, `IsBranchConfigurable`, `IsExtInitialized`, `GetForks`, `AddFork`, `AddForks`, `RemoveFork` |
| `core/storage`<br>Bare repo management | | `EnsureRepository`, `GetStorageDir`, `FetchRepository` |
| `core/objstore`<br>S3 remote | `Client`, `Config`, `HelperEnv`, `Progress`, `LocalCommitSource`, `PushOutcome`, `PostPushHook`, `SiteOverride` | `NewClient`, `ClientForRemote`, `ParseS3URL`, `RunHelper`, `HelperEnvFromOS`, `ListRemoteRefs`, `ReadRemoteRefs`, `RebuildRefManifest`, `LogDumbTransportInfo`, `RefsHeadDigest`, `ReadPackedObject`, `ThinUpstreamURL`, `CompressJSON`, `ReadCompressedJSON`, `PutCompressed`, `UploadConcurrency`, `RunParallel`, `PushArtifactObjects`, `PutObjectToRemote` |
| `core/site`<br>Static site | `SiteCustomization` | `Rebuild`, `PostPushMaintenance`, `SetRemoteHead`, `WriteSiteStats`, `ReadWorkspaceSiteCustomization`, `WriteWorkspaceSiteCustomization`, `NormalizeSiteURL`, `NormalizeSiteImage`, `NormalizeSiteGlobs`, `ValidSiteAccent` |
| `core/fetch`<br>Fetch orchestration | | `FetchAll`, `FetchRepository`, `FetchForks`, `CommitProcessor`, `PostFetchHook`, `DefaultBranch` |
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
| `client`<br>Fetch and push sequences | `FetchOptions`, `Options`, `Result`, `SiteOutcome` | `Fetch`, `SyncWorkspace`, `ResolveRemotes`, `Preview`, `Push`, `PushAll`, `PublishSite`, `DeleteTrackingRefs` |
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

You can delete the storage under `repositories/` at any time; GitSocial uses `cache.db`, not the storage, to decide what to fetch. The cache is append-only. A commit that leaves its source branch (rebase, force-push) is stale: `cache.MarkCommitsStale` or `MarkCommitsStaleByRepo` sets its `stale_since`. In the workspace, a row is stale when its branch is not the [home](#workspace-home-branch) of its commit, and `MarkCommitsStaleByHome` sets the value. Timeline and list queries exclude stale commits, and thread and detail views show them dimmed.

SQLite: WAL, 64 MB page cache, temp store in memory, 16 connections, 256 MB mmap.

### Schema

Every extension table is keyed by `(repo_url, hash, branch)` into `core_commits` and carries the extension's prefix. Column lists are in `core/cache/db.go` and each extension's `schema.go`.

- `core_commits(repo_url, hash, branch, author_name, author_email, message, timestamp, edits, is_virtual, origin_author_name, origin_author_email, action, ...)` plus the generated `effective_*` columns
- `core_commits_version(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)`: edit to canonical, authoritative for versioning
- `core_repositories`, `core_repository_meta`, `core_sync_tips`: followed and workspace repositories, their metadata, and the tips of the last workspace fetch
- `core_lists`, `core_list_repositories`: lists and their members
- `core_fetch_ranges`: fetched time windows per repository
- `core_notification_reads`, `core_mentions`, `core_labels`, `core_trailer_refs`: read markers, `@` mentions, labels, and `Closes:`/`Refs:` trailers per commit
- `core_identity_dns` (24 h TTL), `core_verified_bindings`: identity caches ([IDENTITY.md](IDENTITY.md))
- `core_edit_acceptances`, `core_edit_declines`: outcomes of cross-repo proposals
- `social_items`, `social_interactions` (recounted on every write from the items that are not retracted), `social_followers`, `social_repo_lists`, `social_repo_list_repositories`
- `pm_items`, `pm_assignees`, `pm_links` (blocks, blocked-by, related)
- `review_items`, `review_reviewers`, `review_branch_observations` (current tips of every branch an open pull request points at, refreshed after fetch)
- `release_items`, `release_sbom_cache`
- `memo_items`

`core_commits.edits` stores the raw header value; `core_commits_version` is authoritative by repository and hash. Use `cache.ResolveToCanonical` and `cache.GetLatestVersion`.

Edit resolution applies only to same-repository edits (GITMSG.md §1.5), so a cross-repository edit is a proposal that has no effect until the owner accepts it. `proposals.Accept` writes an accepting edit: a same-repository edit of the owner that carries `accepts=<proposal>`. The accepting edit has priority in resolution, and its processing writes the `core_edit_acceptances` row. `proposals.Decline` publishes a marker at `refs/gitmsg/core/declines/*`. Both clear the marker of the proposer, and accept has priority over decline.

A change to an item of a registered fork adopts the item and does not make a proposal (GITMSG.md §1.5). The first change writes a copy on the branch of this repository, and the copy carries `adopts=<original>` and a `GitMsg-Ref:` reference section with the author of the original. Each later change edits the copy.

### Resolved views

`core_commits` has the generated columns `effective_message`, `effective_author_name`, `effective_author_email` and `effective_timestamp`, which use the content of the latest edit and the origin fields in place of the raw values. Each extension has a `<ext>_items_resolved` view that joins its table onto `core_commits` and projects those columns under the display names:

```sql
CREATE VIEW {ext}_items_resolved AS
SELECT c.effective_message AS resolved_message, c.effective_author_name AS author_name, c.effective_timestamp AS timestamp, ...,
       COALESCE(e.type, 'default') AS type, e.field1, ...
FROM core_commits c
LEFT JOIN {ext}_items e ON c.repo_url = e.repo_url AND c.hash = e.hash AND c.branch = e.branch;
```

The denormalized columns `resolved_message`, `has_edits` and `is_retracted` are written only by `applyEditToCanonical` in `core/cache/versions.go`.

`core_commits.action` is the [timeline action](SOCIAL.md#timeline-actions) of a commit, NULL for none. The commit insert writes it for a first version, and `applyEditToCanonical` writes it for each edit of a canonical on each pass, so the value does not depend on the order of arrival. Only the timeline admits an edit row, through `cache.TimelineItemFilter`.

| Rule | Test |
|---|---|
| The action of an edit is the same for each order of arrival | `TestAction_orderOfArrival` |
| A text, label or assignee edit has no action | `TestAction_textEditHasNone` |
| A proposal and a retraction have no action | `TestAction_proposalAndRetractionHaveNone` |
| A merge, a ready mark and a review have their action | `TestAction_pullRequestAndReview` |
| The timeline has the item at its creation time and each action at its own time | `TestTimeline_actionEntries` |
| A stale row of an action is in no timeline | `TestTimeline_excludesStaleAction` |
| A merge that closes an issue gives one entry | `TestTimeline_mergeHidesIssueClose` |
| The actions of a retracted item are in no timeline | `TestTimeline_excludesActionOfRetractedItem` |
| No list other than the timeline shows an edit row | `TestGetPMItems_excludesActionRows` |
| The site app derives the same action as the cache | `TestParityActions`, `unit_timeline_actions.js` |

Use the view when the WHERE clause is on `core_commits` columns. Join `core_commits` to the extension table directly when the WHERE clause is selective on extension columns (`pm_items.state = 'open'`) or the query is a recursive CTE over extension relationships; otherwise, the planner scans `core_commits`. `social.GetComments`, and the thread and notification readers inside `social`, are the examples.

### Refs and keys

References are `[repo_url]#<type>:<value>`: `https://github.com/user/repo#commit:abc123def456` or, workspace-relative, `#commit:abc123def456`. Types: `commit`, `branch`, `tag`, `file`, `list`.

A repository has two names:

- The identity is `protocol.NormalizeURL` of an address: the key of each cache row, ref path and comparison.
- The address is the URL as the user typed it, stored in the list member or fork ref and given to git.

A repository has an identity, and a person has a verified binding; do not use the word "identity" for a third concept.

A virtual commit is a commit that a `GitMsg-Ref` trailer references and that is not fetched yet. It is stored with `is_virtual = 1` and full metadata, and a fetch of the commit changes the value to `0`.

State refs under `refs/gitmsg/`:

- `refs/gitmsg/<ext>/config`: per-extension JSON config
- `refs/gitmsg/core/forks/<urlHash>`: one ref per registered fork
- `refs/gitmsg/core/declines/<hash>`: one ref per declined proposal, subject is the proposal ref
- `refs/gitmsg/<ext>/lists/<name>/_meta` and `.../items/<refHash>`: list metadata and one ref per member

Each element has its own ref, so concurrent adds from several clones do not collide. Metadata is at `_meta`. Git refuses a child ref under a parent ref that has the same name.

Fork refs sit outside `refs/gitmsg/`, keyed by `fetch.URLHash` of the fork identity:

- `refs/forks/<hash>/gitmsg/*`: the protocol branches a registered fork publishes, written by `core/fetch`
- `refs/fork/<hash>/<branch>`: the code branches that a cross-fork diff uses, written by `extensions/review`

### Fetch rules

| Repository | Cache | Storage |
|---|---|---|
| Workspace | full history, local branches and `origin` | the workspace directory |
| Followed with `#branch:*` | full history, all branches | persistent |
| Followed on one branch | full history, incremental | persistent |
| Not followed | a 30-day window | may be deleted at any time |

All-branch following stores each commit under its real refname. Deduplication and stale marking of a followed repository work per repository through `FilterUnfetchedCommitsByRepo` and `MarkCommitsStaleByRepo`. Switching a repository between one branch and `*` runs `cache.ResetRepositoryData`, and the next fetch rebuilds the data of the repository.

### Workspace home branch

The workspace sync stores each commit under one branch, its home, and the home is a function of the current refs. A cache built from empty has the same live rows as a cache that followed each change. `core/fetch/home.go` computes the home, and `SyncWorkspaceOrigin` and each `SyncWorkspace*` function ingest through it, with the workspace sync function of each extension and the mention and trailer processors.

| Rule | Value | Test |
|---|---|---|
| Walked refs | `refs/heads/*` and `refs/remotes/origin/*`; no other remote, no tag, no state ref | `TestWorkspaceSync_otherRemoteAddsNothing`, `TestWorkspaceSync_skipsStateRefs` |
| Logical branch | the ref name without `refs/heads/` or `refs/remotes/origin/` | `TestWorkspaceSync_contentBranchHome` |
| Default branch | the `HEAD` of `origin`, then `main`, then `master`, then the first code branch by name | `TestDefaultBranch_ignoresCheckout` |
| Home | the first branch that reaches the commit: the default branch, then each `gitmsg/*` branch by name, then each other branch | `TestHomeBranch_precedence` |
| Order of the other branches | a branch whose tips another branch reaches comes first; unrelated branches sort by name | `TestHomeBranch_stackAncestorFirst` |
| Live row | the row whose branch is the home; each other row of the hash is stale | `TestWorkspaceSync_mergeMovesHome`, `TestWorkspaceSync_selfHeals` |
| Read marker | copied to the new home when a row goes stale | `TestMarkCommitsStaleByHome_readMarkerFollowsTheCommit` |
| Gate | the names, tips and symrefs of the walked refs, in `core_sync_tips` | `TestWorkspaceSync_gateEqualsWalkSet` |
| Stable branches | the default branch and the `gitmsg/*` branches; the sync reads only the commits after their last tips | `TestWorkspaceSync_rewindTakesTheFullPath` |
| Full comparison | on a first build, a tip from older code, a stable branch that lost a commit, or a new set of stable branches; it reads each row | `TestWorkspaceSync_rewindTakesTheFullPath`, `TestSyncTip_oldFormatTriggersSync` |
| Tip | advances only when each commit has its home row | `TestWorkspaceSync_tipNeedsFullState` |
| Finalize | the stale marks and the tip are written only if no walked ref moved during the sync | `TestWorkspaceSync_refChangeDuringSyncSkipsFinalize` |
| Origin sync | ingests with the sync functions in `Options.WorkspaceSyncs`; with none, the gate stays open for the caller | `TestSyncWorkspaceOrigin_runsTheWorkspaceSyncs` |
| Rebuild | the live rows equal those of a cache built from empty | `TestWorkspaceSync_rebuildEqualsIncremental` |
| Lookup of a commit | by repository and hash, the live row first | `TestGetCommitOnAnyBranch_prefersLive` |
| Reference target | matched by repository and hash, the live row first; the stored reference keeps its branch | `TestGetComments_movedCommit`, `TestRecount_movedTarget`, `TestGetTrailerRefsTo_branchlessTrailer`, `TestResolveRefLocation` |
| Edit resolution | the canonical is matched by repository and hash, and its resolved state is on each row of the hash | `TestApplyEdit_everyRowOfTheHash`, `TestInsertCommits_editAppliesAcrossBranch`, `TestSyncWorkspace_editSurvivesMerge` |
| Stale source | no mention notification, no trailer notification and no trailer reference | `TestMentionProvider_excludesStaleCommit`, `TestGetTrailerRefsTo_excludesStaleSource` |
| Stale row | in no list, count or notification; a lookup by hash takes the live row first | `TestGetPMItems_excludesStaleCommit`, `TestGetPMItemByHashPrefix_liveFirst`, `TestNotifications_excludesStaleComment` |

A different repository, a mirror or the upstream of a fork, is not workspace content when it is only a git remote. It gets into the cache through a list or a fork registration, under its own URL.

### Extension rules

- Tables carry the `<ext>_` prefix and key into `core_commits` by `(repo_url, hash, branch)`.
- Content is on `gitmsg/<ext>`, named by one constant per extension. Only `social` writes a configured branch, which its `init` records under the `branch` key and `gitmsg.GetExtBranch` reads back. Every config carries `version`, the key `IsExtInitialized` reads. A pm, review, release or memo processor skips a commit from any other branch.
- An extension joins the fetch in `library/client`, which lists every extension's `Processors`, `SyncWorkspaceBatch` and `BackfillSpec`. `pm`, `release` and `review` name their own `Processors` again in their `FetchRepository`, which serves the `--repo` form of a CLI list command.
- An extension has ten imports: `cache`, `fetch`, `git`, `gitmsg`, `log`, `notifications`, `protocol` and `result`, plus `social` for comments and `tui/tuicore` for its navigation items. It registers a notification provider with `notifications.RegisterProvider`.
- `core/search` names the table of each extension itself, in `query.go` and `group.go`, and `core/cache` names them in `analytics.go`, `clear.go`, `commits.go`, `stats.go` and `versions.go`. `notifications` uses a registration in place of names. A sixth extension must edit both packages; do not change this asymmetry before a sixth extension exists.
- Core tables are read-only for extensions; use the cache APIs.
- Known limits: `storage.GetStorageDir` hashes only the URL, so one URL on two branches has one storage directory. Check `meta.HasCommits` before you read timestamps.

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

The table governs `library/tui/*/`, not `test/` and not the `tuicore/diff/` sub-package. A `component_` renders and takes keys; shared app state stays in a `util_` file.

A new extension gets a `tui/tui<ext>/` directory with its `view_*.go` files and a `util_register.go` exposing `Register(host)`, called from `app.go`.
