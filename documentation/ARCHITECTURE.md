# GitSocial Architecture

Single Go library with thin clients connecting via RPC/HTTP.

## Table of Contents

- [Overview](#overview)
- [Directory Structure](#directory-structure)
- [Code Rules](#code-rules)
- [Package Reference](#package-reference)
- [Code Patterns](#code-patterns)
- [Cache Architecture](#cache-architecture)
- [CLI Commands](#cli-commands)
- [TUI](#tui)
- [Development](#development)

---

## Overview

```
┌─────────────────────────────────────────────────────────────┐
│                   Go Library (single source)                │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  core/              # Shared infrastructure            │ │
│  │  ├── git/           # Git operations                   │ │
│  │  ├── protocol/      # GitMsg protocol parsing          │ │
│  │  ├── gitmsg/        # Protocol-level storage           │ │
│  │  ├── cache/         # SQLite (posts, counts, lists)    │ │
│  │  ├── storage/       # Bare repo management             │ │
│  │  └── fetch/         # Fetch orchestration + processing │ │
│  ├────────────────────────────────────────────────────────┤ │
│  │  extensions/        # Extension-specific               │ │
│  │  └── social/        # Posts, lists, timeline           │ │
│  └────────────────────────────────────────────────────────┘ │
│          ┌─────────────────┼─────────────────┐              │
│          ▼                 ▼                 ▼              │
│    ┌──────────┐     ┌──────────┐     ┌──────────┐           │
│    │   CLI    │     │ JSON-RPC │     │   HTTP   │           │
│    │ (direct) │     │ (stdio)  │     │ (server) │           │
│    └──────────┘     └──────────┘     └──────────┘           │
└─────────────────────────────────────────────────────────────┘
         │                  │                  │
         ▼                  ▼                  ▼
    Terminal/TUI      VSCode/Neovim       Web/Mobile
```

---

## Directory Structure

```
gitmsg/
├── library/                        # Go library
│   ├── core/
│   │   ├── git/                    # Git operations
│   │   ├── protocol/               # GitMsg protocol parsing
│   │   ├── gitmsg/                 # Protocol-level storage
│   │   ├── cache/                  # SQLite operations
│   │   ├── storage/                # Bare repo management
│   │   ├── fetch/                  # Fetch orchestration + processing
│   │   ├── search/                 # Cross-extension search
│   │   └── result/                 # Result[T] type
│   ├── extensions/
│   │   ├── social/                 # Posts, lists, timeline
│   │   ├── pm/                     # Issues, milestones, sprints
│   │   ├── release/                # Releases, versions, artifacts
│   │   └── review/                 # Pull requests, code reviews
│   ├── import/                     # Platform import pipeline
│   │   └── github/                # GitHub adapter (gh CLI)
│   ├── cli/                        # CLI commands
│   └── tui/                        # TUI views
├── clients/
│   └── vscode/                     # VSCode extension (planned)
├── documentation/                  # Protocol docs
└── specs/                          # Protocol specifications
```

### Layer Dependencies

```
extensions/* → core/* → stdlib only
     ↓            ↓
    cli/        (no circular refs)
    tui/
    import/  → extensions/* + core/protocol
```

---

## Code Rules

### Do

- Add a brief comment at the top of each file (e.g., `// commits.go - Git commit operations`)
- Add a one-liner comment above each function
- Use functional patterns (no methods on structs unless implementing interfaces)
- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use `cache.ExecLocked`/`QueryLocked` for all DB operations
- Check existing types before creating new ones

### Never

- Access git directly from extensions (use `core/git`)
- Create one-off types for single use
- Use global mutable state
- Skip error handling

---

## Package Reference

| Package | Purpose | Key Exports |
|---------|---------|-------------|
| `core/git` | Git operations | `GetCommits`, `CreateCommit`, `ReadRef`, `WriteRef`, `GetDiff`, `GetFileDiff`, `GetFileContent`, `GetDiffStats`, `MergeBranches`, `SquashMerge`, `RebaseMerge`, `ForceMerge`, `RebaseBranch`, `RangeDiff`, `PatchesEqual`, `GetBehindCount`, `GetMergeBase` |
| `core/protocol` | Message parsing | `ParseMessage`, `ParseHeader`, `CreateHeader`, `FormatMessage`, `ParseRef`, `CreateRef`, `FormatShortRef`, `QuoteContent`, `ApplyOrigin`, `ExtractTrailers`, `Trailer`, `IsClosingTrailer` |
| `core/cache` | SQLite operations | `Open`, `DB`, `ExecLocked`, `QueryLocked`, `InsertCommits`, `FilterUnfetchedCommitsByRepo`, `MarkCommitsStaleByRepo`, `ResetRepositoryData`, `RegisterMigration`, `ToNullString`, `ToNullInt64`, `GetTrailerRefsTo`, `TrailerRef` |
| `core/gitmsg` | Protocol-level storage | `ResolveRepoURL`, `Push`, `ReadExtConfig`, `WriteList`, `GetHistory`, `GetExtBranch`, `IsExtInitialized`, `GetForks`, `AddFork`, `AddForks`, `RemoveFork` |
| `core/storage` | Bare repo management | `EnsureRepository`, `GetStorageDir`, `FetchRepository` |
| `core/fetch` | Fetch orchestration | `FetchAll`, `FetchRepository`, `FetchForks`, `CommitProcessor`, `PostFetchHook` |
| `core/settings` | User settings | `Get`, `Set`, `ListAll` |
| `core/search` | Cross-extension search | `Search`, `Params`, `Result`, `Item`, `Group`, `GroupedItem`, `FormatResult`, `IsValidGroupBy` |
| `core/result` | Result type | `Result[T]`, `Success`, `Failure` |
| `core/notifications` | Notification aggregation | `RegisterProvider`, `GetAll`, `GetUnreadCount`, `MarkAsRead`, `MarkAsUnread`, `MarkAllAsRead`, `MarkAllAsUnread`, `MentionProcessor`, `ExtractMentions`, `TrailerProcessor` |
| `extensions/social` | Social layer | `GetPosts`, `CreatePost`, `GetTimeline`, `Fetch` |
| `extensions/pm` | Project management | `GetIssues`, `CreateIssue`, `GetMilestones`, `GetSprints`, `FetchRepository`, `Processors` |
| `extensions/release` | Release management | `CreateRelease`, `EditRelease`, `GetReleases`, `GetSingleRelease`, `FetchRepository`, `Processors` |
| `extensions/review` | Code review | `CreatePR`, `GetPR`, `UpdatePR`, `MergePR`, `ClosePR`, `RetractPR`, `MarkReady`, `ConvertToDraft`, `UpdatePRTips`, `SyncPRBranch`, `GetPRVersions`, `ComparePRVersions`, `GetVersionAwareReviews`, `CreateFeedback`, `GetReviewSummary`, `FetchRepository`, `GetPullRequestsWithForks`, `Processors` |
| `import` | Platform import pipeline | `Run`, `SourceAdapter`, `ReadMapping`, `WriteMapping`, `MappingKey`, `ResolveHost`, `MapLabels` |
| `import/github` | GitHub adapter | `New`, `CheckGH`, `Adapter.FetchPM`, `Adapter.FetchReleases`, `Adapter.FetchReview`, `Adapter.FetchSocial` |

### Type Locations

| Type | Package |
|------|---------|
| `git.Commit`, `FileDiff`, `Hunk`, `DiffLine`, `DiffStats` | `library/core/git/` |
| `protocol.Header`, `Message`, `Origin`, `Trailer` | `library/core/protocol/` |
| `cache.Repository`, `Commit`, `TrailerRef` | `library/core/cache/` |
| `result.Result[T]` | `library/core/result/` |
| `notifications.Notification`, `Provider`, `Filter` | `library/core/notifications/` |
| `social.Post`, `SocialItem` | `library/extensions/social/` |
| `pm.Issue`, `Milestone`, `Sprint`, `PMNotification` | `library/extensions/pm/` |
| `release.Release`, `ReleaseItem`, `ReleaseNotification` | `library/extensions/release/` |
| `review.PullRequest`, `Feedback`, `ReviewSummary`, `ReviewNotification` | `library/extensions/review/` |
| `importpkg.SourceAdapter`, `ImportPlan`, `Stats`, `MappingFile` | `library/import/` |

### Terminology

| Term | Context | Meaning |
|------|---------|---------|
| `original` | GITSOCIAL field | Post being commented/reposted/quoted |
| `canonical` | Versioning | First version of a message (before edits) |
| `edits` | GITMSG field | Reference to canonical version being edited |

---

## Code Patterns

### Error Handling by Layer

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

### Cache Query

```go
items, err := cache.QueryLocked(func(db *sql.DB) ([]Item, error) {
    rows, err := db.Query(`SELECT id, content FROM social_items WHERE type = ?`, typePost)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    // scan rows...
    return items, nil
})
```

### Cache Write

```go
err := cache.ExecLocked(func(db *sql.DB) error {
    _, err := db.Exec(`INSERT INTO social_items (id, type) VALUES (?, ?)`, id, itemType)
    return err
})
```

### Extension Result Pattern

```go
result := social.GetPosts(workdir, scope, opts)
if !result.Ok {
    return social.Failure[T](result.Error.Code, result.Error.Message)
}
posts := result.Data
```

### CLI Command

```go
var exampleCmd = &cobra.Command{
    Use:   "example",
    Short: "Example command",
    RunE: func(cmd *cobra.Command, args []string) error {
        return nil
    },
}

func init() { rootCmd.AddCommand(exampleCmd) }
```

### TUI View

```go
type View interface {
    Update(msg tea.Msg, state *State) tea.Cmd
    Render(state *State) string
}
```

---

## Cache Architecture

```
~/.cache/gitsocial/
├── cache.db          # SQLite (commits + extension tables)
├── repositories/     # Bare git clones (cleaned up periodically)
├── forks/            # Fork bare clones
└── imports/          # Import mapping files (per repo URL slug)
```

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

**Social extension:**
- `social_items(repo_url, hash, branch, type, original_*, reply_to_*)` - PK: `(repo_url, hash, branch)`
- `social_interactions(repo_url, hash, branch, comments, refs)` - PK: `(repo_url, hash, branch)`
- `social_followers(repo_url, workspace_url, detected_at, list_id, commit_hash)` - PK: `(repo_url, workspace_url)`

**Release extension:**
- `release_items(repo_url, hash, branch, tag, version, prerelease, artifacts, artifact_url, checksums, signed_by, sbom)` - PK: `(repo_url, hash, branch)`

**Review extension:**
- `review_items(repo_url, hash, branch, type, state, base, base_tip, head, head_tip, closes, reviewers, pull_request_*, commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion)` - PK: `(repo_url, hash, branch)`

**Versioning:** `core_commits.edits` stores raw header value; `core_commits_version` is authoritative. Use `cache.ResolveToCanonical()` / `cache.GetLatestVersion()`.

### Resolved Views

`core_commits_resolved` resolves versioning (edits, retractions) via LEFT JOINs on `core_commits_version`. It COALESCEs `origin_author_name`/`origin_author_email` over git author fields so imported content shows the original author. Each extension then has a resolved view that LEFT JOINs its tables onto `core_commits_resolved`:

```sql
CREATE VIEW {ext}_items_resolved AS
SELECT r.*, COALESCE(e.type, 'default') as type, e.field1, ...
FROM core_commits_resolved r
LEFT JOIN {ext}_items e ON r.repo_url = e.repo_url AND r.hash = e.hash AND r.branch = e.branch;
```

This ensures items are found regardless of whether they have extension-specific records.

### Refs and Keys

**Ref format**: `[repo_url]#type:value`
- `https://github.com/user/repo#commit:abc123def456` - full ref
- `#commit:abc123def456` - workspace-relative ref
- Types: `commit`, `branch`, `tag`, `file`, `list`

**Virtual commits**: Referenced in `GitMsg-Ref` but not yet fetched. Stored with `is_virtual = 1` and full metadata. When fetched, `is_virtual` flips to `0`.

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

```
gitsocial
├── status              # Show GitSocial status
├── fetch               # Fetch updates from subscribed repos
├── config              # Manage core protocol config
├── settings            # Manage user settings
├── log                 # Show activity log
├── search              # Search posts
├── explore             # Browse repositories
├── history             # View edit history
├── notifications       # View/manage notifications
├── fork                # Add, remove, list registered forks
├── tui                 # Launch TUI
│
├── import              # Import from external platforms
│   ├── all             # Import everything (pm → release → review → social)
│   ├── pm              # Import milestones + issues
│   ├── release         # Import releases
│   ├── review          # Import forks + pull requests
│   └── social          # Import discussions/posts (GitHub only)
│
├── social              # Social extension
│   ├── status/init/config
│   ├── timeline        # View timeline
│   ├── post/edit/retract
│   ├── comment/repost/quote
│   ├── fetch/followers
│   └── list            # Manage lists (show, create, delete, add, remove)
│
├── pm                  # Project management extension
│   ├── status/init/config
│   ├── issue           # Create, list, show, close, reopen issues
│   ├── milestone       # Create, list, show milestones
│   └── sprint          # Create, list, show sprints
│
├── release             # Release management extension
│   ├── status/init/config
│   ├── create/edit/retract
│   ├── list/show
│   ├── sbom            # Show SBOM details for a release
│   └── artifacts, versions, checksums
│
└── review              # Code review extension
    ├── status/init/config
    ├── pr              # Create, list, show, merge, close, retract PRs
    └── feedback        # Approve, request-changes, inline comments
```

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
│   PM (planned)          ││                                          │
│ ─────────────────────── ││                                          │
│   Settings              ││                                          │
├─────────────────────────┤├──────────────────────────────────────────┤
│ Current dir             ││ Context-sensitive keybindings            │
└─────────────────────────┘└──────────────────────────────────────────┘
```

### Structure

```
library/tui/
├── app.go                    # Main tea.Model
├── host.go                   # View dispatch + shared state
│
├── tuicore/                  # Infrastructure + core views
│   ├── view_*.go             # Routable views (cache, config, notifications, search, settings)
│   ├── component_*.go        # Reusable components (nav_panel, version_picker, confirm, choice)
│   ├── registry_*.go         # Global registries (action, card, nav, view)
│   ├── util_*.go             # Utilities (render, keys, router, types, syntax, diff, etc.)
│   └── bus.go                # Event bus
│
├── tuisocial/                # Social extension views
│   ├── view_*.go             # Views (timeline, post, repository, list_*)
│   └── util_*.go             # Utilities (adapters, register, helpers)
│
├── tuipm/                    # PM extension views
│   ├── view_*.go             # Views (board, issues, milestones, sprints, *_detail, *_history)
│   ├── form_*.go             # Modal forms (issue, milestone, sprint)
│   └── util_*.go             # Utilities (adapters, register)
│
├── tuirelease/               # Release extension views
│   ├── view_*.go             # Views (releases list, release detail)
│   └── util_*.go             # Utilities (register, card renderer)
│
├── tuireview/                # Review extension views
│   ├── view_*.go             # Views (PR list, PR detail, files changed diff, interdiff, PR history)
│   ├── form_*.go             # Modal forms (PR create/edit, feedback with inline support)
│   └── util_*.go             # Utilities (register, card renderer, syntax highlighting)
│
└── test/                     # Headless TUI integration tests
    ├── harness.go            # Headless model driver (sends keys, drains commands)
    ├── fixture.go            # Test repo setup + data seeding
    ├── assert.go             # Render assertion helpers (ANSI stripping)
    ├── main_test.go          # Shared fixture via TestMain
    ├── smoke_test.go         # All keys × all views (no-panic verification)
    ├── display_test.go       # Content rendering verification
    ├── golden_test.go        # Visual regression via golden files
    ├── navigation_test.go    # View-to-view transitions
    ├── sequence_test.go      # Multi-step user flows
    └── testdata/             # Golden files (generated with -update flag)
```

### File Naming Convention

| Prefix | Purpose | Examples |
|--------|---------|----------|
| `view_` | Routable views | `view_timeline.go`, `view_issues.go` |
| `component_` | Reusable stateful components | `component_nav_panel.go` |
| `registry_` | Global registries | `registry_nav.go` |
| `form_` | Modal form overlays | `form_issue.go` |
| `util_` | Stateless utilities | `util_render.go`, `util_keys.go` |

### Adding a New Extension

1. Create `tui/tuiXX/` directory
2. Add views as `view_*.go` files
3. Add `util_register.go` with `Register(host)` function
4. Call from `app.go`

If more ceremony needed, we over-engineered.

---

## Development

### Build & Run

```bash
go build -o /tmp/gitsocial ./library    # Build CLI
/tmp/gitsocial social timeline          # Run command
/tmp/gitsocial tui                      # Launch TUI
```

### Test & Lint

```bash
go test ./...                    # All tests
go test ./library/core/cache    # Specific package
go test -v ./...                 # Verbose output
go test -race ./...              # Race detector
go test -cover ./...             # Coverage summary
cd library && golangci-lint run --fix ./...  # Lint & fix code

# TUI tests (headless integration)
# run from library/
go test ./tui/test/...                       # All TUI tests
go test ./tui/test/ -run Smoke               # Smoke: all keys × all views
go test ./tui/test/ -run Display             # Content rendering
go test ./tui/test/ -run Golden              # Golden file comparison
go test ./tui/test/ -run Golden -update      # Regenerate golden files
go test ./tui/test/ -run Navigation          # View transitions
go test ./tui/test/ -run Sequence            # Multi-step flows
```

See `documentation/TUI-TESTS.md` for the full TUI test suite documentation.

### Specs

| Spec | Status |
|------|--------|
| `specs/GITMSG.md` | Stable |
| `specs/GITSOCIAL.md` | Stable |
| `specs/GITPM.md` | Stable |
| `specs/GITRELEASE.md` | Stable |
| `specs/GITREVIEW.md` | Stable |
