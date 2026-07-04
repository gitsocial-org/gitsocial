# GitSocial Architecture

Single Go library with thin clients: CLI/TUI (direct) and JSON-RPC (stdio).

[Development](#development) • [Directory Structure](#directory-structure) • [Package Reference](#package-reference) • [Cache Architecture](#cache-architecture) • [CLI Commands](#cli-commands) • [TUI](#tui)

---

## Development

### Branching & Worktrees

Trunk-based: `main` is the integration branch and stays clean in the primary checkout. Develop each feature on a `feature/<name>` branch in its own git worktree, so several run in parallel. Worktrees live in a sibling directory — standard location `../gitsocial-worktrees`, called `$WT` below.

```bash
WT=../gitsocial-worktrees

# 0. refresh main — primary checkout; before branching and after any merge
git switch main && git pull --ff-only

# 1. start a feature
git worktree add -b feature/<name> "$WT/<name>" main

# 2. build, test & run in "$WT/<name>" (distinct binary — won't clobber main's or a parallel build's)
go build -o /tmp/gitsocial-<name> ./cli/gitsocial && go test ./...
# schema-changing work: add --cache-dir /tmp/gs-<name>
/tmp/gitsocial-<name> tui

# 3. keep current while others are in flight (in "$WT/<name>")
git fetch origin && git rebase main

# 4. integrate — rebase then fast-forward = linear history
git -C "$WT/<name>" rebase main
# from primary; OK while the branch is in its worktree
git merge --ff-only feature/<name>

# 5. clean up — from primary, not inside the worktree
git worktree remove "$WT/<name>" && git branch -d feature/<name>
```

- `gitmsg/*` and `gitsocial` are protocol/data branches — not for feature work.
- Integrate and cleanup run from the primary checkout: a branch checked out in a worktree can't be deleted or checked out twice.
- Run schema-changing branches with a separate `--cache-dir` (as above): the first binary to open the shared `~/.cache/gitsocial/cache.db` upgrades it in place, after which older binaries (other worktrees, `main`) refuse it. Delete the cache to rebuild.

### Build & Run

```bash
go build -o bin/gitsocial ./cli/gitsocial     # Build CLI
bin/gitsocial social timeline                 # Run command
bin/gitsocial tui                             # Launch TUI
```

### Test & Lint

```bash
go test ./...                    # All tests
go test ./library/core/cache     # Specific package
go test -v ./...                 # Verbose output
go test -race ./...              # Race detector
go test -cover ./...             # Coverage summary
golangci-lint run --fix ./...    # Lint & fix code

go test ./library/tui/test/...   # Headless TUI suite (smoke/display/golden/nav/sequence; see TUI-TESTS.md)
```

### Code Rules

#### Layer Dependencies

```
library/extensions/* → library/core/* → stdlib only
          ↓                 ↓
  cli/gitsocial/        (no circular refs)
  library/tui/
  library/rpc/
  library/import/  → library/extensions/* + library/core/protocol
```

#### Do

- Read relevant specs first: `specs/GITMSG.md`, `specs/GITSOCIAL.md`, `specs/GITPM.md`, `specs/GITRELEASE.md`, `specs/GITREVIEW.md`
- Optionally read relevant `documentation/` files
- Add a brief comment at the top of each file (e.g., `// commits.go - Git commit operations`)
- Add a one-liner comment above each function
- Use functional patterns (no methods on structs unless implementing interfaces)
- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use `cache.ExecLocked`/`QueryLocked` for all DB operations
- Check existing types before creating new ones

#### Never

- Access git directly from extensions (use `core/git`)
- Create one-off types for single use
- Use global mutable state
- Skip error handling

### Code Patterns

#### Error Handling by Layer

```
Core packages (cache, git, protocol)  → return error (idiomatic Go)
Extension public API (social.*)       → return Result[T] (user-facing codes)
Internal helpers                      → return error
```

**Rules:**
1. Always wrap errors: `fmt.Errorf("operation: %w", err)`
2. At API boundaries, convert to `Result[T]` for user-friendly error codes
3. For batch operations that continue on failure, log instead of returning
4. Intentional suppressions must be commented

#### Common patterns (see code for examples)

- **Cache access** — wrap every DB op in `cache.QueryLocked` / `ExecLocked`; example: `library/extensions/social/`.
- **Extension API** — public extension funcs return `Result[T]` (`Success`/`Failure`); example: `library/extensions/social/`.
- **CLI command** — one `*cobra.Command` per file under `cli/gitsocial/`, registered in `init()`.
- **TUI view** — implement the `View` interface (`Update`/`Render`); example: `library/tui/tuicore/`.

---

## Directory Structure

```
gitsocial/                     # module github.com/gitsocial-org/gitsocial
├── cli/gitsocial/             # CLI thin client; builds the binary
├── library/                   # Go library — single source of truth
│   ├── core/                  # Shared infrastructure
│   │   ├── git/               # Git operations
│   │   ├── protocol/          # GitMsg protocol parsing
│   │   ├── gitmsg/            # Protocol-level storage
│   │   ├── cache/             # SQLite operations
│   │   ├── storage/           # Bare repo management
│   │   ├── fetch/             # Fetch orchestration + processing
│   │   ├── identity/          # Identity declarations + verification
│   │   │   └── forge/         # Forge adapters (GitHub, …)
│   │   ├── notifications/     # Notification aggregation
│   │   ├── search/            # Cross-extension search
│   │   ├── settings/          # User settings + config paths
│   │   ├── log/               # Structured logging
│   │   ├── text/              # String helpers
│   │   └── result/            # Result[T] type
│   ├── extensions/
│   │   ├── social/            # Posts, lists, timeline
│   │   ├── pm/                # Issues, milestones, sprints
│   │   ├── release/           # Releases, versions, artifacts
│   │   ├── review/            # Pull requests, code reviews
│   │   └── memo/              # Tiered memos (knowledge as commits)
│   ├── proposals/             # Cross-repo proposals (gate; accept + decline engine)
│   ├── import/                # Platform import pipeline
│   │   ├── github/            # GitHub adapter (gh CLI)
│   │   └── gitlab/            # GitLab adapter
│   ├── clientfetch/           # Thin-client fetch orchestration (CLI/TUI)
│   ├── rpc/                   # JSON-RPC server (stdio) — thin-client surface
│   └── tui/                   # TUI views — thin client
├── documentation/             # Protocol + architecture docs
├── docs/                      # Project website (GitHub Pages)
├── scripts/                   # Build/release scripts (mirror.sh, release.sh)
└── specs/                     # Protocol specifications
```

**Outside the repo tree:**

```
../gitsocial-worktrees/<name>/ # Sibling git worktrees for parallel feature work

~/.config/gitsocial/           # User config; honors `XDG_CONFIG_HOME`
├── settings.json              # Machine-specific settings
└── personal/                  # Personal-tier bare repo (override: `GITSOCIAL_PERSONAL_REPO`)

~/.cache/gitsocial/            # Cache dir, `--cache-dir` overrides
├── cache.db                   # SQLite (commits + extension tables)
├── repositories/              # Bare git clones (cleaned up periodically)
├── forks/                     # Fork bare clones
├── imports/                   # Import mapping files (per repo URL slug)
└── memo/session/              # Per-session memo bare repos
```

---

## Package Reference

| Package | Key Types | Key Exports |
|---------|-------|---------|
| `core/git`<br>Git operations | `Commit`, `FileDiff`, `Hunk`, `DiffLine`, `DiffStats` | `GetCommits`, `CreateCommit`, `ReadRef`, `WriteRef`, `GetDiff`, `GetFileDiff`, `GetFileContent`, `GetDiffStats`, `MergeBranches`, `SquashMerge`, `RebaseMerge`, `ForceMerge`, `RebaseBranch`, `RangeDiff`, `PatchesEqual`, `GetBehindCount`, `GetMergeBase`, `GetUserName`, `GetGitConfig`, `CreateSignedCommitTree`, `VerifyCommitSignature`, `GetCommitSignerKey` |
| `core/protocol`<br>Message parsing | `Header`, `Message`, `Origin`, `Trailer` | `ParseMessage`, `ParseHeader`, `CreateHeader`, `FormatMessage`, `ParseRef`, `CreateRef`, `FormatShortRef`, `QuoteContent`, `ApplyOrigin`, `ExtractTrailers`, `Trailer`, `IsClosingTrailer` |
| `core/cache`<br>SQLite operations | `Repository`, `Commit`, `TrailerRef` | `Open`, `DB`, `ExecLocked`, `QueryLocked`, `InsertCommits`, `FilterUnfetchedCommitsByRepo`, `MarkCommitsStaleByRepo`, `ResetRepositoryData`, `RegisterMigration`, `ToNullString`, `ToNullInt64`, `GetTrailerRefsTo`, `TrailerRef` |
| `core/gitmsg`<br>Protocol-level storage | — | `ResolveRepoURL`, `Push`, `ReadExtConfig`, `WriteList`, `GetHistory`, `GetExtBranch`, `IsExtInitialized`, `GetForks`, `AddFork`, `AddForks`, `RemoveFork` |
| `core/storage`<br>Bare repo management | — | `EnsureRepository`, `GetStorageDir`, `FetchRepository` |
| `core/fetch`<br>Fetch orchestration | — | `FetchAll`, `FetchRepository`, `FetchForks`, `CommitProcessor`, `PostFetchHook` |
| `core/settings`<br>User settings | — | `Get`, `Set`, `ListAll` |
| `core/search`<br>Cross-extension search | — | `Search`, `Params`, `Result`, `Item`, `Group`, `GroupedItem`, `FormatResult`, `IsValidGroupBy` |
| `core/result`<br>Result type | `Result[T]` | `Result[T]`, `Success`, `Failure` |
| `core/notifications`<br>Notification aggregation | `Notification`, `Provider`, `Filter` | `RegisterProvider`, `GetAll`, `GetUnreadCount`, `MarkAsRead`, `MarkAsUnread`, `MarkAllAsRead`, `MarkAllAsUnread`, `MentionProcessor`, `ExtractMentions`, `TrailerProcessor` |
| `core/identity`<br>Identity verification | `Identity`, `ResolvedIdentity`, `DNSIdentity`, `Binding`, `Source`, `VerifyCandidate` | `VerifyBinding`, `IsVerified`, `IsVerifiedCommit`, `LookupBinding`, `VerifyCandidates`, `NormalizeSignerKey`, `NormalizeEmail`, `ResolveIdentity` |
| `core/identity/forge`<br>Forge adapters for identity verification | `Forge`, `GPGKey`, `CommitVerification` | `Forge`, `Register`, `Lookup`, `LookupForRepo`, `ParseRepoURL`, `NewGitHub`, `GPGKey`, `CommitVerification` |
| `extensions/social`<br>Social layer | `Post`, `SocialItem` | `GetPosts`, `CreatePost`, `GetTimeline`, `Fetch` |
| `extensions/pm`<br>Project management | `Issue`, `Milestone`, `Sprint`, `PMNotification` | `GetIssues`, `CreateIssue`, `GetMilestones`, `GetSprints`, `MessageToPMItem`, `FetchRepository`, `Processors` |
| `extensions/release`<br>Release management | `Release`, `ReleaseItem`, `ReleaseNotification` | `CreateRelease`, `EditRelease`, `GetReleases`, `GetSingleRelease`, `MessageToReleaseItem`, `FetchRepository`, `Processors` |
| `extensions/review`<br>Code review | `PullRequest`, `Feedback`, `ReviewSummary`, `StackEntry`, `ReviewNotification` | `CreatePR`, `GetPR`, `UpdatePR`, `MergePR`, `ClosePR`, `RetractPR`, `MarkReady`, `ConvertToDraft`, `UpdatePRTips`, `SyncPRBranch`, `GetPRVersions`, `ComparePRVersions`, `GetVersionAwareReviews`, `CreateFeedback`, `GetReviewSummary`, `MessageToReviewItem`, `FetchRepository`, `GetPullRequestsWithForks`, `GetStack`, `GetDependents`, `Processors` |
| `extensions/memo`<br>Tiered memos (knowledge as commits) | `Memo`, `MemoItem`, `Tier`, `SessionInfo` | `CreateMemo`, `EditMemo`, `RetractMemo`, `PromoteMemo`, `ListMemos`, `GetSingleMemo`, `InitProject`, `InitPersonal`, `InitSession`, `ListSessions`, `GCSession`, `PushPersonal`, `FetchPersonal`, `PushSession`, `FetchSession`, `SyncAllTierReposToCache`, `AddInherit`, `RemoveInherit`, `ListInherits`, `IsInherited` |
| `proposals`<br>Cross-repo proposals | `Outcome` | `Accept`, `Decline` |
| `import`<br>Platform import pipeline | `SourceAdapter`, `ImportPlan`, `Stats`, `MappingFile` | `Run`, `SourceAdapter`, `ReadMapping`, `WriteMapping`, `MappingKey`, `ResolveHost`, `MapLabels` |
| `import/github`<br>GitHub adapter | — | `New`, `CheckGH`, `Adapter.FetchPM`, `Adapter.FetchReleases`, `Adapter.FetchReview`, `Adapter.FetchSocial` |

### Terminology

| Term | Context | Meaning |
|------|---------|---------|
| `original` | GITSOCIAL field | Post being commented/reposted/quoted |
| `canonical` | Versioning | First version of a message (before edits) |
| `edits` | GITMSG field | Reference to canonical version being edited |

---

## Cache Architecture

**Key principle**: Storage (repositories/) can be deleted anytime. Fetch strategy is determined by cache.db metadata, not storage state.

**Staleness**: The cache is append-only, but commits that no longer exist in their source branch (e.g., after rebase or force-push) are marked with a `stale_since` timestamp via `cache.MarkCommitsStale()` (single-branch) or `cache.MarkCommitsStaleByRepo()` (all-branch). Stale commits are excluded from timeline and list queries but remain visible (dimmed) in thread and detail views to preserve discussion context.

**SQLite tuning**: WAL mode, 64MB cache (`_cache_size=-65536`), memory temp store, 16 max connections, 256MB mmap.

### Schema

**Core tables:**
- `core_commits(repo_url, hash, branch, author_name, author_email, message, timestamp, edits, is_virtual, origin_author_name, origin_author_email)` - PK: `(repo_url, hash, branch)`
- `core_commits_version(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)` - PK: `(edit_repo_url, edit_hash, edit_branch)`
- `core_repositories(url, branch, storage_path, is_followed, last_fetch)` - PK: `url`
- `core_lists(id, name, source, version, workdir)` - PK: `id`
- `core_list_repositories(list_id, repo_url, branch)` - PK: `(list_id, repo_url)`
- `core_fetch_ranges(id, repo_url, range_start, range_end, status, fetched_at, commit_count, error_message)`
- `core_notification_reads(repo_url, hash, branch, read_at)` - PK: `(repo_url, hash, branch)`
- `core_mentions(repo_url, hash, branch, email)` - PK: `(repo_url, hash, branch, email)`
- `core_trailer_refs(repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key, trailer_value)` - PK: `(repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key)`
- `core_identity_dns(email, key, repo, resolved_at)` - PK: `email` — caches DNS well-known lookups (24h TTL).
- `core_verified_bindings(key_fingerprint, email, source, forge_host, forge_account, verified, resolved_at)` - PK: `(key_fingerprint, email, source, forge_host)` — caches per-source attestations. See [Identity Verification](IDENTITY.md) for the trust model and source list.
- `core_edit_acceptances(edit_repo_url, edit_hash, edit_branch)` - PK: `(edit_repo_url, edit_hash, edit_branch)` — derived index that a cross-repo proposal was accepted, populated from the mirror edit's `accepts=` header on every fetch path. Read only as a NOT EXISTS marker (clears the proposed-edit ✎) and for accept idempotency.
- `core_edit_declines(edit_repo_url, edit_hash, edit_branch)` - PK: `(edit_repo_url, edit_hash, edit_branch)` — the owner declined a cross-repo proposal. Durable and published at `refs/gitmsg/core/declines/*` so the proposer learns and the choice survives a re-clone; clears the proposed-edit marker (accept takes precedence).

**Social extension:**
- `social_items(repo_url, hash, branch, type, original_*, reply_to_*)` - PK: `(repo_url, hash, branch)`
- `social_interactions(repo_url, hash, branch, comments, refs)` - PK: `(repo_url, hash, branch)`
- `social_followers(repo_url, workspace_url, detected_at, list_id, commit_hash)` - PK: `(repo_url, workspace_url)`

**Release extension:**
- `release_items(repo_url, hash, branch, tag, version, prerelease, artifacts, artifact_url, checksums, signed_by, sbom)` - PK: `(repo_url, hash, branch)`

**Review extension:**
- `review_items(repo_url, hash, branch, type, state, base, base_tip, head, head_tip, depends_on, closes, reviewers, pull_request_*, commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion)` - PK: `(repo_url, hash, branch)`
- `review_branch_observations(repo_url, branch, tip, branch_exists, observed_at)` - PK: `(repo_url, branch)` — transient cache of the live remote tip for every branch any open PR's head or base points at, across the workspace and registered forks. Refreshed by `RefreshOpenPRBranches` after fetch; consumed by the `head-advanced` / `head-deleted` / `base-advanced` / `base-deleted` notifications.

**Versioning:** `core_commits.edits` stores raw header value; `core_commits_version` is authoritative. Use `cache.ResolveToCanonical()` / `cache.GetLatestVersion()`.

**Cross-repo proposals:** edit resolution is gated to same-repo edits (GITMSG.md §1.5), so a cross-repo edit (e.g. a fork editing your issue) is an inert *proposal* until the owner acts. `proposals.Accept` applies it as the owner's own same-repo mirror edit carrying `accepts=<proposal>`, which wins resolution and, on processing, derives `core_edit_acceptances`; the proposer learns via that mirror on the gitmsg data branch, so acceptance needs no published marker. `proposals.Decline` publishes a durable marker at `refs/gitmsg/core/declines/*` so the proposer learns and the owner's choice survives a re-clone. Both clear the proposer's ✎ marker; accept takes precedence over decline.

### Resolved Views

`core_commits` carries `effective_*` generated columns (`effective_message`, `effective_author_name`, `effective_author_email`, `effective_timestamp`) that COALESCE the latest edit's content (`resolved_message`) and origin-author/origin-time (set on imported content) over the raw fields. Each extension has a `*_items_resolved` view that joins its tables onto `core_commits` and projects the generated columns under the legacy display names:

```sql
CREATE VIEW {ext}_items_resolved AS
SELECT
    c.effective_message AS resolved_message,
    c.effective_author_name AS author_name,
    c.effective_timestamp AS timestamp,
    ...,
    COALESCE(e.type, 'default') as type, e.field1, ...
FROM core_commits c
LEFT JOIN {ext}_items e ON c.repo_url = e.repo_url AND c.hash = e.hash AND c.branch = e.branch;
```

This ensures items are found regardless of whether they have extension-specific records. The denormalized resolved-state columns (`resolved_message`, `has_edits`, `is_retracted`) are written exclusively by `applyEditToCanonical` (`core/cache/versions.go`).

**When to bypass the view:** the `*_items_resolved` views are right for typical list/show queries where the WHERE clause is on `core_commits` columns (timestamp, repo_url, etc.) and the result needs every commit-as-an-item. Bypass them — JOIN `core_commits` directly to the extension table — when:

- The WHERE clause is highly selective on extension columns (e.g., `pm_items.state = 'open'`, `social_items.original_*`). Driving from the small extension table avoids a planner mishap where `core_commits` (millions of rows) becomes the outer table.
- The query uses a recursive CTE over extension relationships (e.g., walking `social_items.reply_to_*`).

Examples already in the codebase: `social.GetThread` (recursive CTE on `social_items`), `social.GetNotifications` (drives from `social_items` joined to `core_commits`). Both bypass the resolved view because the view forced a full scan on a 1M-commit cache.

### Refs and Keys

**Ref format**: `[repo_url]#type:value`
- `https://github.com/user/repo#commit:abc123def456` - full ref
- `#commit:abc123def456` - workspace-relative ref
- Types: `commit`, `branch`, `tag`, `file`, `list`

**Virtual commits**: Referenced in `GitMsg-Ref` but not yet fetched. Stored with `is_virtual = 1` and full metadata. When fetched, `is_virtual` flips to `0`.

**Workspace refs (`refs/gitmsg/*`)**: extension data branches (`gitmsg/<ext>`) and these classes of state refs:
- `refs/gitmsg/<ext>/config` — per-extension JSON config (single ref)
- `refs/gitmsg/core/forks/<urlHash>` — one ref per registered fork (per-element layout, no shared write target — concurrent fork adds across clones don't collide)
- `refs/gitmsg/core/declines/<hash>` — one ref per declined cross-repo proposal (subject = the proposal ref); published so the proposer's ✎ marker clears on their next fetch and the owner's decline survives a re-clone (acceptance needs no marker: it rides the owner's mirror edit)
- `refs/gitmsg/<ext>/lists/<name>/_meta` + `.../items/<refHash>` — list metadata at `_meta`, members as per-element refs (same rationale; metadata lives under `_meta` because git refuses to create child refs while a same-named parent ref exists)

### Fetch Rules

| Repo Type | Cache (core_commits) | Storage (repositories/) |
|-----------|---------------------|------------------------|
| Workspace | Full history, all branches (`*`) | N/A (uses workdir) |
| Followed (`*`) | Full history, all branches | Persistent |
| Followed (specific branch) | Full history, incremental | Persistent |
| Non-followed | 30-day window | Can be deleted anytime |

**All-branch following (`branch = "*"`)**: Commits are stored with their actual git refname (e.g., `main`, `gitmsg/social`, `feature/x`). The workspace always uses all-branch semantics. Deduplication and stale marking operate at the repo level via `FilterUnfetchedCommitsByRepo` / `MarkCommitsStaleByRepo`.

**Switching modes**: `cache.ResetRepositoryData()` clears old commits and extension items when switching between specific branch and `*`. Next fetch rebuilds with correct branches.

### Extension Guidelines

- Tables MUST use `{extension_name}_` prefix
- Core tables are read-only (use cache APIs)
- Link to git via `(repo_url, hash)` composite FK to `core_commits`
- Use `cache.ExecLocked`/`QueryLocked` for DB access

### Known Limitations

1. `storage.GetStorageDir()` hashes URL only; same URL with different branches shares storage
2. Check `meta.HasCommits` before using timestamps (zero-value edge case)

---

## CLI Commands

Cobra-generated — run `gitsocial --help` or `gitsocial <group> --help` for the authoritative, current list.

- **Top-level:** `status`, `fetch`, `config`, `settings`, `log`, `search`, `show`, `explore`, `history`, `notifications`, `fork`, `id`, `tui`
- **Import:** `import {all,pm,release,review,social}`
- **Extensions:** `social`, `pm`, `release`, `review`, `memo` — each adds `status`/`config` + its own verbs (and `init`, except `memo`, which inits per-tier)

**Planned extensions**: cicd, ops, security, dm, portfolio

---

## TUI

Two-panel layout using Bubbletea: Nav (left) + Content (right). See `documentation/TUI-KEYS.md` for key bindings.

```
┌─ Navigation ────────────┐┌─ Content ────────────────────────────────┐
│   Search                ││                                          │
│   Notifications (3)     ││  Timeline / Post / Repository / Search   │
│ ─────────────────────── ││                                          │
│ ▸ Social                ││  View content based on selection         │
│   PM                    ││                                          │
│ ─────────────────────── ││                                          │
│   Settings              ││                                          │
├─────────────────────────┤├──────────────────────────────────────────┤
│ Current dir             ││ Context-sensitive keybindings            │
└─────────────────────────┘└──────────────────────────────────────────┘
```

### Structure

```
library/tui/
├── app.go / host.go     # main tea.Model + view dispatch / shared state
├── tuicore/             # infrastructure + core views (view_/component_/registry_/util_/bus)
├── tuisocial/           # social views
├── tuipm/               # PM views
├── tuirelease/          # release views
├── tuireview/           # review views
├── tuimemo/             # memo views
└── test/                # headless integration tests (see TUI-TESTS.md)
```

### File Naming Convention

| Prefix | Purpose | Examples |
|--------|---------|----------|
| `view_` | Routable views | `view_timeline.go`, `view_issues.go` |
| `component_` | Reusable stateful components | `component_nav_panel.go` |
| `registry_` | Global registries | `registry_nav.go` |
| `form_` | Modal form overlays | `form_issue.go` |
| `version_item_` | History-picker version items (hero-card detail render) | `version_item_issue.go` |
| `util_` | Stateless utilities | `util_render.go`, `util_keys.go` |

### Adding a New Extension

1. Create `tui/tuiXX/` directory
2. Add views as `view_*.go` files
3. Add `util_register.go` with `Register(host)` function
4. Call from `app.go`

If more ceremony needed, we over-engineered.
