# GitSocial CLI Reference

## Table of Contents

- [Command Structure](#command-structure)
- [Core Commands](#core-commands)
- [Social Extension](#social-extension)
- [Project Management](#project-management-gitsocial-pm)
- [Code Review](#code-review-gitsocial-review)
- [Release Extension](#release-extension-gitsocial-release)
- [Import](#import-gitsocial-import)
- [Reference Format](#reference-format)
- [Exit Codes](#exit-codes)
- [Environment Variables](#environment-variables)
- [Scripting](#scripting)

---

## Command Structure

Git-native CLI for GitSocial. Follows git's conventions: simple, composable, unix-philosophy.

```
gitsocial [--json] <command> [subcommand] [args...] [flags]
```

**Global flags:**
- `--json` - Machine-readable JSON output
- `-C <path>` - Run in directory (like git -C)
- `--cache-dir` - Cache directory (default: `~/.cache/gitsocial`)
- `--help` - Show help

**Extensions:** `social`, `pm`, `release`, `review`

---

## Core Commands

### gitsocial status

Show GitSocial status for current repository.

```
gitsocial status
```

### gitsocial fetch

Fetch updates from all extensions.

```
gitsocial fetch                              # Fetch all
gitsocial fetch <url>                        # Specific repo
gitsocial fetch --list reading               # Specific list
gitsocial fetch --since 2024-01-01           # Since date
```

**Flags:**
- `--list, -l` - Fetch only repos from this list
- `--since` - Fetch posts since date (YYYY-MM-DD, default: 30 days ago)
- `--before` - Fetch posts before date (YYYY-MM-DD, default: today)
- `--parallel, -p` - Number of concurrent fetches (default: 4)

Reports unread notification count after completion (e.g., "You have 3 new notifications").

### gitsocial push

Push local changes to remote.

```
gitsocial push
gitsocial push --dry-run
```

### gitsocial config

Manage core protocol configuration (stored in `refs/gitmsg/core/config`).

```
gitsocial config get <key>
gitsocial config set <key> <value>
gitsocial config list
```

### gitsocial settings

Manage local user settings (stored in `~/.config/gitmsg/settings.json`).

```
gitsocial settings get <key>
gitsocial settings set <key> <value>
gitsocial settings list
```

### gitsocial explore

Browse subscribed repositories.

```
gitsocial explore                    # All repos
gitsocial explore --list reading     # Repos from a specific list
```

### gitsocial related

Find repositories related to a given repository through shared lists and authors.

```
gitsocial related <repository>       # Full URL
gitsocial related owner/repo         # Short form (assumes GitHub)
gitsocial related owner/repo -l 10   # Limit results
```

### gitsocial log

Show activity log for the current repository or timeline.

```
gitsocial log                                  # Current repository
gitsocial log --scope timeline                 # All timeline activity
gitsocial log --type post,comment              # Filter by types
gitsocial log --after 2024-01-01               # After date
gitsocial log --author alice@example.com       # Filter by author
gitsocial log --limit 50                       # Limit results
```

**Flags:**
- `--scope, -s` - Scope: `timeline`, `repository:my` (default)
- `--type, -t` - Filter by types (comma-separated): post, comment, repost, quote, list-create, list-delete, repository-follow, repository-unfollow, config, metadata
- `--after` - Show entries after date (YYYY-MM-DD)
- `--before` - Show entries before date (YYYY-MM-DD)
- `--author, -a` - Filter by author email
- `--limit, -n` - Maximum entries (default: 20)

### gitsocial search

Search across all extensions (posts, issues, PRs, releases, and more).

```
gitsocial search "query"
gitsocial search "feature" --author alice@example.com --type post
gitsocial search "bug fix" --scope list:favorites --sort date
gitsocial search --type pr --state open --json
gitsocial search --type issue --labels bug --assignee dev@example.com --json
gitsocial search --draft --json
```

**Flags:**
- `--author, -a` - Filter by author email
- `--type, -t` - Filter by type (post|comment|repost|quote|pr|issue|milestone|sprint|release|feedback)
- `--hash` - Filter by commit hash prefix
- `--after` - Posts after date (YYYY-MM-DD)
- `--before` - Posts before date (YYYY-MM-DD)
- `--repo, -r` - Filter by repository URL
- `--scope, -s` - Search scope: `timeline` (default), `list:<name>`, `repository:<url>`
- `--sort` - Sort by: `score` (default) or `date`
- `--limit, -n` - Maximum results (default: 20)
- `--state` - Filter by state (open, closed, merged, canceled)
- `--labels` - Filter by labels (comma-separated, any match)
- `--assignee` - Filter by assignee email (implies `--type issue`)
- `--reviewer` - Filter by reviewer email (implies `--type pr`)
- `--milestone` - Filter by milestone name (implies `--type issue`)
- `--sprint` - Filter by sprint name (implies `--type issue`)
- `--draft` - Filter draft PRs only (implies `--type pr`)
- `--prerelease` - Filter pre-releases only (implies `--type release`)
- `--tag` - Filter by release tag (implies `--type release`)
- `--base` - Filter by PR base branch (implies `--type pr`)
- `--group-by` - Group results by field (state, author, type, extension, repo, label, assignee, reviewer, milestone, base)
- `--top` - Max items per group (default: unlimited)
- `--count-only` - Show only group counts, no items

### gitsocial show

Show full details for any item. Auto-detects extension (issue, PR, release, or post).

```
gitsocial show <ref>
gitsocial show "#commit:abc123"
gitsocial show "#commit:abc123" --json
```

### gitsocial history

View edit history of any GitMsg message.

```
gitsocial history <ref>
gitsocial history "#commit:abc123"
```

Works for any extension (social posts, PM issues, etc.) since versioning is a core protocol feature.

### gitsocial notifications

View and manage notifications.

```
gitsocial notifications                        # Show unread
gitsocial notifications --all                  # Show all
gitsocial notifications --limit 50             # Limit results
gitsocial notifications --type mention,follow  # Filter by type
gitsocial notifications count                  # Unread count
gitsocial notifications read <id>              # Mark as read (commit ref or repo#follow)
gitsocial notifications read-all               # Mark all as read
gitsocial notifications unread <id>            # Mark as unread
gitsocial notifications unread-all             # Mark all as unread
```

### gitsocial id

Verify commit signatures and resolve identities. See [Identity Verification](IDENTITY.md) for the trust model, sources, and caching behavior.

```
gitsocial id verify <commit>                   # Verify a commit's binding
gitsocial id resolve <email>                   # Resolve an identity via DNS well-known
```

Requires git signing configured (`user.signingkey` and `gpg.format`). Supports SSH and GPG keys.

DNS-based verification (`/.well-known/gitmsg-id.json`) is **off by default** — see [IDENTITY.md](IDENTITY.md#why-dns-is-opt-in) for the rationale. Enable with `gitsocial settings set identity.dns_verification true` or via the TUI Settings view.

### gitsocial tui

Launch interactive terminal UI.

```
gitsocial tui
gitsocial tui --list reading                   # Filter by list
gitsocial tui --limit 100                      # Limit initial posts
```

### gitsocial rpc

Start JSON-RPC server on stdio for editor integration.

```
gitsocial rpc
```

### gitsocial docs

Generate documentation.

```
gitsocial docs keybindings                     # Generate keybinding docs
```

---

## Social Extension

```
gitsocial social init                              # Initialize social extension
gitsocial social init --branch <name>              # Custom branch
gitsocial social status                            # Show social status
gitsocial social config get|set|list               # Manage config

gitsocial social post "Hello world"                # Create post
gitsocial social post - < message.txt              # Read from stdin
gitsocial social comment <post-id> "Great idea!"   # Comment on a post
gitsocial social repost <post-id>                  # Repost
gitsocial social quote <post-id> "Adding thoughts" # Quote with commentary
gitsocial social edit <post-id> <new-text>         # Edit post
gitsocial social retract <post-id>                 # Retract post

gitsocial social timeline                          # View timeline
gitsocial social timeline --list reading           # From specific list
gitsocial social timeline --repo workspace         # Current repository only
gitsocial social timeline --repo <url>             # Specific repository
gitsocial social timeline --limit 50               # Limit results

gitsocial social fetch                             # Fetch social updates
gitsocial social fetch --list reading              # Fetch specific list
gitsocial social fetch <url>                       # Fetch specific repo

gitsocial social list create <id>                  # Create list
gitsocial social list create <id> --name "Display" # With display name
gitsocial social list delete <id>                  # Delete list
gitsocial social list add <id> <url>               # Add repo to list
gitsocial social list add <id> <url> -b <branch>   # With specific branch
gitsocial social list add <id> <url> --all-branches # Follow all branches
gitsocial social list remove <id> <url>            # Remove repo from list
gitsocial social list show [name]                  # Show list(s)
gitsocial social list ls                           # List all lists
gitsocial social list repo <url>                   # Show lists from a repo

gitsocial social followers                         # List followers
```

---

## Project Management (gitsocial pm)

### Setup

```
gitsocial pm init                                  # Initialize (default: kanban)
gitsocial pm init --framework scrum                # Choose framework
gitsocial pm init --branch <name>                  # Custom branch
gitsocial pm status                                # Show PM status
gitsocial pm config get|set|list                   # Manage config
```

Frameworks: `minimal`, `kanban` (default), `scrum`

### Issues

```
gitsocial pm issue create "Title"                  # Create issue
gitsocial pm issue create "Title" -l kind/bug,priority/high  # With labels
gitsocial pm issue create "Title" -a alice@x.com   # With assignee
gitsocial pm issue create "Title" -d 2024-06-01    # With due date
gitsocial pm issue create - < issue.txt            # Read from stdin
gitsocial pm issue list                            # List open issues
gitsocial pm issue list --state closed             # Filter by state
gitsocial pm issue list --labels kind/bug          # Filter by labels
gitsocial pm issue list --filter "priority:high"   # Filter query
gitsocial pm issue list --sort due:asc             # Sort by field
gitsocial pm issue list --repo <url>               # From remote repo
gitsocial pm issue show <ref>                      # Show issue details
gitsocial pm issue close <ref>                     # Close issue
gitsocial pm issue reopen <ref>                    # Reopen issue
gitsocial pm issue comment <ref> "Comment text"    # Add comment
gitsocial pm issue comments <ref>                  # List comments
```

**Issue list filter syntax:**
- `state:open` — Filter by state
- `assignees:alice@x.com` — Filter by assignee
- `priority:high` — Filter by label
- `-kind:chore` — Exclude label
- `due:today`, `due:overdue`, `due:week` — Due date filters
- `"search text"` — Text search

### Milestones

```
gitsocial pm milestone create "v1.0"               # Create milestone
gitsocial pm milestone create "v1.0" --due 2024-06-01
gitsocial pm milestone list                        # List open milestones
gitsocial pm milestone list --state all            # All states
gitsocial pm milestone show <ref>                  # Show details + linked issues
gitsocial pm milestone close <ref>
gitsocial pm milestone reopen <ref>
gitsocial pm milestone cancel <ref>
gitsocial pm milestone delete <ref>
```

### Sprints

```
gitsocial pm sprint create "Sprint 1" --start 2024-01-01 --end 2024-01-14
gitsocial pm sprint list                           # List active/planned
gitsocial pm sprint list --state all               # All states
gitsocial pm sprint show <ref>                     # Show details + linked issues
gitsocial pm sprint start <ref>
gitsocial pm sprint complete <ref>
gitsocial pm sprint cancel <ref>
gitsocial pm sprint delete <ref>
```

### Board

```
gitsocial pm board                                 # Kanban board view
```

---

## Code Review (gitsocial review)

### Setup

```
gitsocial review init                              # Initialize (branch: gitmsg/review)
gitsocial review init --branch reviews             # Custom branch
gitsocial review status                            # Show review status
gitsocial review config get|set|list               # Manage config
```

### Forks

Register fork repositories so their PRs and issues are discovered during fetch.
Forks are managed at the core level via `gitsocial fork` (also available as `gitsocial review fork`).

```
gitsocial fork add <url>                           # Register a fork
gitsocial fork remove <url>                        # Remove a fork
gitsocial fork list                                # List registered forks
```

### Pull Requests

```
gitsocial review pr create "Add feature"                       # Create PR
gitsocial review pr create "Add feature" --base main --head feature/branch
gitsocial review pr create "Fix bug" --closes "#commit:abc123@gitmsg/pm"
gitsocial review pr create "Fix bug" --reviewers alice@x.com,bob@x.com
gitsocial review pr create "Add routes" --depends-on "#commit:abc123@gitmsg/review"  # Stacked PR
gitsocial review pr create "Add routes" --base feature/auth --stack               # Auto-detect stack parent
gitsocial review pr create - < pr-description.md               # Read from stdin
gitsocial review pr list                                       # List open PRs
gitsocial review pr list --state merged                        # Filter by state
gitsocial review pr list --repo <url>                          # From remote repo
gitsocial review pr show <ref>                                 # Show details + feedback
gitsocial review pr show <ref> --versions                      # Include version history
gitsocial review pr update <ref>                               # Capture branch tips as new version
gitsocial review pr diff <ref>                                 # Range-diff between last two versions
gitsocial review pr diff <ref> --from 0 --to 2                 # Between specific versions
gitsocial review pr merge <ref>                                # Merge (default: fast-forward)
gitsocial review pr merge <ref> --strategy squash              # Squash merge
gitsocial review pr merge <ref> --strategy rebase              # Rebase merge
gitsocial review pr merge <ref> --strategy merge               # Force merge commit
gitsocial review pr sync <ref>                                 # Sync head with base (default: rebase)
gitsocial review pr sync <ref> --strategy merge                # Merge base into head
gitsocial review pr close <ref>                                # Close without merge
gitsocial review pr retract <ref>                              # Retract
gitsocial review pr stack <ref>                                # Show full stack from any member
gitsocial review pr rebase-stack <ref>                         # Cascade rebase all PRs above this one
gitsocial review pr sync-stack <ref>                           # Update branch tips for all open PRs in the stack
```

**Version management:**
- `pr update` signals "new code ready for review" — captures current base-tip and head-tip as a new version in the edits chain
- `pr diff` uses `git range-diff` to compare patch series between versions, showing what the author actually changed vs. what was just rebased
- `pr show --versions` displays a table of all versions with base-tip, head-tip, author, and date

**Merge strategies:**
- `ff` (default) — fast-forward if possible, otherwise merge commit
- `squash` — squash all head commits into one on base
- `rebase` — replay head commits individually onto base
- `merge` — always create a merge commit, even if fast-forward is possible

**Review staleness:** `pr show` displays version-aware review status. Uses `git range-diff` to distinguish pure rebases from actual code changes:
```
Reviews:
  ✓ bob@example.com      approved (reviewed v2, current is latest, no code changes)
  ✗ carol@example.com    changes requested (reviewed original, current is latest, code changed) [stale]
```

**Stacked PRs:** Decompose a large change into an ordered chain of small PRs where each builds on the one below it.
- `--depends-on <ref>` — explicit dependency on a parent PR
- `--stack` — auto-detect parent by matching the new PR's base branch to an open PR's head branch
- `pr merge` refuses to merge if any `depends-on` target is unmerged (enforces bottom-up order)
- After merge, dependent PRs whose base matched the merged PR's head are auto-retargeted to the merged PR's base
- `pr rebase-stack <ref>` — walks the stack upward from the given PR and rebases each dependent's head onto its base
- `pr sync-stack <ref>` — snapshots current branch tips for all open PRs in the stack (no rebase)
- `pr stack <ref>` — shows the full stack from any member with state icons and position

### Feedback

```
gitsocial review feedback approve <pr-ref>                     # Approve
gitsocial review feedback approve <pr-ref> -m "LGTM"          # With message
gitsocial review feedback request-changes <pr-ref> -m "Fix X" # Request changes

# Inline comments
gitsocial review feedback comment "Fix this" --pr <ref> --file src/auth.go --commit abc123456789 --new-line 42
gitsocial review feedback comment "Fix range" --pr <ref> --file src/auth.go --commit abc123 --new-line 42 --new-line-end 50
gitsocial review feedback comment "Old code" --pr <ref> --file src/auth.go --commit abc123 --old-line 42
gitsocial review feedback comment "Suggestion" --pr <ref> --file src/auth.go --commit abc123 --new-line 42 --suggest
```

**Inline comment flags:**
- `--pr` - Pull request ref (required)
- `--file` - File path (required for inline)
- `--commit` - Commit hash, 12+ chars (required for inline)
- `--old-line` - Line in old file version (1-indexed)
- `--new-line` - Line in new file version (1-indexed)
- `--old-line-end` - End line in old file version
- `--new-line-end` - End line in new file version
- `--suggest` - Mark as code suggestion

---

## Release Extension (gitsocial release)

### Setup

```
gitsocial release init                             # Initialize (branch: gitmsg/release)
gitsocial release init --branch releases           # Custom branch
gitsocial release status                           # Show release status
gitsocial release config get|set|list              # Manage config
```

### Create & Manage

```
gitsocial release create "v1.0 Release"                        # Create release
gitsocial release create "v1.0" --version 1.0.0 --tag v1.0.0  # With version + tag
gitsocial release create "v1.0" --artifacts "app.tar.gz,app.zip" --checksums SHA256SUMS
gitsocial release create "v1.0" --artifact-url https://cdn.example.com/releases/v1.0/
gitsocial release create "v1.0" --prerelease                   # Pre-release
gitsocial release create "v1.0" --signed-by ABCDEF123          # Signed
gitsocial release create "v1.0" --sbom sbom.spdx.json          # With SBOM
gitsocial release create - < release-notes.md                  # Read from stdin
gitsocial release edit <ref> --version 1.0.1                   # Edit version
gitsocial release edit <ref> --tag v1.0.1 --body "Updated"     # Edit tag + body
gitsocial release edit <ref> --sbom sbom.cdx.json              # Update SBOM
gitsocial release retract <ref>                                # Retract
```

### Query

```
gitsocial release list                             # List releases (default: 20)
gitsocial release list -n 50                       # List more
gitsocial release list --repo <url>                # From remote repo
gitsocial release show <ref>                       # Show release details
gitsocial release sbom <ref>                       # Show SBOM details (packages, licenses)
gitsocial release sbom <ref> --raw                 # Raw SBOM JSON output
```

---

## Import (gitsocial import)

Import data from external platforms (GitHub, GitLab, Gitea, etc.) into GitSocial extensions. Auto-detects host type from URL using `protocol.DetectHost()`.

When no URL is provided, the origin remote of the current repository is used. When no subcommand is given, imports everything (same as `import all`).

### Usage

```
gitsocial import [url]                  # Import all from URL or origin remote
gitsocial import all [url]              # Everything in dependency order (pm → release → review → social)
gitsocial import pm [url]               # Milestones + issues
gitsocial import release [url]          # Releases + artifact metadata
gitsocial import review [url]           # Fork registrations + pull/merge requests
gitsocial import social [url]           # Discussions/posts + comments (GitHub only)
```

### URL Formats

Any format is accepted — normalized automatically. When omitted, origin remote is used.

```
gitsocial import pm                              # uses origin remote
gitsocial import pm https://github.com/org/repo
gitsocial import pm git@github.com:org/repo.git
gitsocial import pm github.com/org/repo
```

### Flags

```
gitsocial import [url]
  -n, --limit int          Max items per type (default: 50)
      --since string       Only import items after date (YYYY-MM-DD)
      --dry-run            Print what would be imported without creating commits
      --map-file string    Path to ID mapping file (default: ~/.cache/gitsocial/imports/<repo>.json)
      --labels string      Label mapping: auto, raw, skip (default: auto)
      --skip-bots          Skip bot-authored items (default: true)
      --host string        Force host type: github, gitlab, gitea, bitbucket
      --api-url string     Custom API base URL for self-hosted instances
      --token string       API token (default: from platform CLI or env)
      --state string       Filter by state: open, closed, merged, all (default: all)
      --email-map string   Path to username=email mapping file
  -v, --verbose            Print each item as it's imported
```

### Examples

```bash
# Import everything from origin remote
gitsocial import

# Import everything from a specific URL
gitsocial import all https://github.com/example-org/example

# Import only issues and milestones
gitsocial import pm https://github.com/example-org/example

# Import only open issues from origin
gitsocial import pm --state open

# Import releases from GitLab
gitsocial import release https://gitlab.com/example-org/example

# Self-hosted GitLab with explicit host type
gitsocial import all https://git.company.com/team/project --host gitlab

# Dry run — see what would be imported
gitsocial import --dry-run

# Re-run is idempotent (skips already-imported items via map file)
gitsocial import all https://github.com/example-org/example
```

### Host Detection

| Domain | Detected As |
|--------|-------------|
| `github.com` | GitHub (`gh` CLI) |
| `gitlab.com`, contains `gitlab` | GitLab (REST API) |
| `codeberg.org`, contains `gitea` | Gitea (REST API) |
| `bitbucket.org`, contains `bitbucket` | Bitbucket (REST API) |
| Unknown | Probes API endpoints, or use `--host` |

### Mapping File

Import writes `~/.cache/gitsocial/imports/<url-slug>.json` to track `{platform}:{type}:{id}` → GitSocial commit hash. Re-running skips already-imported items. Override with `--map-file`.

---

## Reference Format

References follow GitMsg format:

- `#commit:abc123456789` — Git commit
- `#branch:main` — Git branch
- `#tag:v1.0.0` — Git tag
- `#file:src/auth.go` — File at HEAD
- `#file:src/auth.go:L42` — File at line
- `#file:src/auth.go:L42-50` — File at line range
- `#file:src/auth.go@v1.0.0` — File at version
- `https://github.com/user/repo#commit:abc123@main` — Remote ref

Short form: when unambiguous, use just the hash prefix (e.g., `abc123`).

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Permission denied |
| 4 | Network error |
| 5 | Not a git repository |

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `GITSOCIAL_HOME` | Config directory (default: `~/.config/gitsocial`) |
| `GITSOCIAL_EDITOR` | Editor for messages (falls back to `$EDITOR`) |
| `GITSOCIAL_PAGER` | Pager for output (falls back to `$PAGER`) |
| `GITSOCIAL_NO_COLOR` | Disable colors |

---

## Scripting

```bash
# Batch close issues
gitsocial pm issue list --labels kind/wontfix --json | \
  jq -r '.[].id' | \
  xargs -I {} gitsocial pm issue close {}

# Fetch and summarize timeline
gitsocial fetch && gitsocial social timeline --json | \
  jq -r '.[] | "\(.author_name): \(.content)"'

# Check for open PRs
if gitsocial review pr list --json | jq -e 'length > 0' > /dev/null; then
  echo "PRs pending review"
fi

# Read from stdin
echo "Hello world" | gitsocial social post -
cat CHANGELOG.md | gitsocial release create -
```
