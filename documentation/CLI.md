# GitSocial CLI Reference

## Table of Contents

- [Command Structure](#command-structure)
- [Extensions](#extensions)
- [Import](#import-gitsocial-import)
- [Core Commands](#core-commands)
- [Reference Format](#reference-format)
- [Exit Codes](#exit-codes)
- [Environment Variables](#environment-variables)
- [Scripting](#scripting)

This document covers the cross-cutting CLI: global flags, core commands shared across extensions, import, reference format, exit codes, env vars, and scripting. **Per-extension commands live in each extension's doc** — see [Extensions](#extensions).

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

**Extensions:** `social`, `pm`, `release`, `review`, `memo`

---

## Extensions

Each extension owns its own command surface. Use `--help` for authoritative flag-level detail; the per-extension docs walk through concepts and workflows.

| Extension | Doc | Top-level commands |
|-----------|-----|--------------------|
| Social | [SOCIAL.md](SOCIAL.md) | `social init`, `status`, `config`, `post`, `comment`, `repost`, `quote`, `edit`, `retract`, `timeline`, `fetch`, `list`, `followers` |
| PM | [PM.md](PM.md) | `pm init`, `status`, `config`, `issue`, `milestone`, `sprint`, `board` |
| Review | [REVIEW.md](REVIEW.md) | `review init`, `status`, `config`, `pr`, `feedback`, `fork` |
| Release | [RELEASE.md](RELEASE.md) | `release init`, `status`, `config`, `create`, `edit`, `retract`, `list`, `show`, `artifacts`, `sbom` |
| Memo | [MEMO.md](MEMO.md) | `memo status`, `config`, `project`, `personal`, `session`, `inherit`, `create`, `edit`, `retract`, `promote`, `list`, `show` |

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

Manage user settings. Each key is scoped — local (this host, in `~/.config/gitsocial/settings.json`) or personal (synced across machines via `refs/gitmsg/core/config`). Writes route by scope automatically; see [SETTINGS.md](SETTINGS.md) for the full key list and the scope model.

```
gitsocial settings get <key>
gitsocial settings set <key> <value>
gitsocial settings list
```

### gitsocial personal

Manage the personal bare repo that holds your synced preferences (and, where applicable, the personal-tier data of extensions). See [SETTINGS.md](SETTINGS.md#cross-machine-sync) for the sync workflow.

```
gitsocial personal init [--remote <url>]
gitsocial personal sync [--push | --fetch]
gitsocial personal status
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

### gitsocial fork

Register external repositories as forks of the current workspace. Issues and PRs filed in a registered fork surface in the workspace's PM/Review views; the workspace author can edit them (state, labels, comments), but a cross-repo edit is an inert *proposal* until the owning repo accepts it, rather than auto-applying.

```
gitsocial fork add <url>             # Register a fork
gitsocial fork remove <url>          # Unregister
gitsocial fork list                  # Show registered forks
```

**Forks vs. lists — when to use which:**

- **Use forks** when you want bidirectional collaboration on the same items: issues filed by Bob in his fork show up in your issue list; closing them locally tells Bob.
- **Use lists** (`gitsocial social list ...`) when you want one-way follow-and-aggregate: you want to *see* upstream's activity but keep your own items separate. This is the right model for soft forks, packaging forks (e.g., maintaining a Flatpak package), and "hub" repos that aggregate many sources into a single feed.

The two are independent — you can register the same repo as a fork *and* include it in a list; they serve different surfaces (PM/Review vs. timeline).

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

### gitsocial clone

Clone a repository. Identical to `git clone` but with zero-setup `s3://` remote support: injects the S3 helper alias and writes it to the cloned repo's local config so plain git commands work there too. Pasted provider URLs are normalized to the canonical `s3://` form (see [remote add](#gitsocial-remote-add) for the accepted shapes).

```
gitsocial clone <url> [directory]
gitsocial clone s3://nyc3.digitaloceanspaces.com/mybucket/repo
gitsocial clone https://<account>.r2.cloudflarestorage.com/mybucket
gitsocial clone https://github.com/org/repo
```

### gitsocial remote add

Add a git remote. When the URL points at an S3 bucket it is normalized to the canonical `s3://<endpoint-host>/<bucket>/<prefix>` form and the S3 helper alias is recorded in the repo's local config, so both gitsocial and plain git work with no further setup. Name defaults to `origin`. Accepted URL shapes (region/account are carried verbatim in the endpoint host):

- Canonical `s3://<endpoint-host>/<bucket>/<prefix>` and a known provider's virtual-host `s3://<bucket>.<endpoint-host>/...`
- An `https://` endpoint or virtual-host URL for a recognized provider (AWS, Cloudflare R2, DigitalOcean) — e.g. the `https://<account>.r2.cloudflarestorage.com` endpoint copied from the R2 dashboard
- A pasted AWS S3 web-console URL (`https://<region>.console.aws.amazon.com/s3/buckets/<bucket>`)

A self-hosted S3 endpoint (host no preset recognizes) must use the `s3://` scheme explicitly; any other URL is added as an ordinary git remote unchanged.

```
gitsocial remote add [name] <url>
gitsocial remote add s3://s3.us-east-1.amazonaws.com/mybucket/repo
gitsocial remote add https://<account>.r2.cloudflarestorage.com/mybucket
gitsocial remote add https://us-east-1.console.aws.amazon.com/s3/buckets/mybucket
gitsocial remote add upstream s3://s3.us-east-1.amazonaws.com/mybucket/repo
```

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
| `GITSOCIAL_PPROF` | Capture a profile for the current run: `cpu` → `/tmp/gitsocial-cpu.pprof`, `mem` → `/tmp/gitsocial-mem.pprof`, `trace` → `/tmp/gitsocial.trace`. Output written on clean exit; analyze with `go tool pprof` / `go tool trace`. |
| `GITSOCIAL_S3_ACCESS_KEY` / `GITSOCIAL_S3_SECRET_KEY` | S3 credentials (take precedence over AWS vars) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | S3 credential fallback (S3-ecosystem convention) |
| `GITSOCIAL_S3_ENDPOINT` | Override S3 endpoint scheme (e.g. `http` for local dev/self-hosted) |
| `GITSOCIAL_S3_PATH_STYLE` | Force path-style S3 addressing |
| `GITSOCIAL_S3_REGION` | SigV4 region for endpoint hosts no preset recognizes |
| `GITSOCIAL_S3_DEBUG` | Set to `1` to dump every S3 request/response to stderr |

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
