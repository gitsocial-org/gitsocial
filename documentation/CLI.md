# CLI

The gitsocial command line: global flags, the core commands, import, scripting, exit codes and environment variables.

[Command Structure](#command-structure) · [Core Commands](#core-commands) · [Import](#import) · [Scripting](#scripting) · [Reference Format](#reference-format) · [Exit Codes](#exit-codes) · [Environment Variables](#environment-variables)

## Command Structure

```
gitsocial [--json] <command> [subcommand] [args] [flags]
```

`--help` on any command is the authority for its flags. Global flags:

- `--json` - JSON output
- `--workdir, -C <path>` - run in another directory
- `--cache-dir <path>` - cache directory (default `~/.cache/gitsocial`)
- `--help`, `--version`

| Extension | Doc | Covers |
|---|---|---|
| `social` | [SOCIAL.md](SOCIAL.md) | posts, comments, lists, timeline, followers |
| `pm` | [PM.md](PM.md) | issues, milestones, sprints, boards |
| `review` | [REVIEW.md](REVIEW.md) | pull requests, feedback, forks |
| `release` | [RELEASE.md](RELEASE.md) | releases, artifacts, SBOM |
| `memo` | [MEMO.md](MEMO.md) | memos across tiers |

Each extension has `init`, `status` and `config`; `memo` inits per tier.

## Core Commands

### gitsocial status

Shows the repository, the cache, and each extension's branch, last fetch and item counts.

```
gitsocial status
```

### gitsocial fetch

Fetches followed repositories, registered forks and identity bindings, then reports unread notifications.

```
gitsocial fetch                      # everything
gitsocial fetch <url>                # one repository
gitsocial fetch --list reading       # one list
gitsocial fetch --since 2026-01-01
```

The first fetch in a workspace asks whether to track the default branch only or every upstream branch. `--all-branches`, `--json` or a non-interactive stdin answer without asking. The answer is saved per workspace.

### gitsocial push

Publishes the `gitmsg/*` branches, state refs, tags, the default branch when it is ahead, open PR heads, and on an s3 remote with `site.publish` the site. Flags and remote resolution are in [S3.md](S3.md#push).

```
gitsocial push [remote...]
gitsocial push --dry-run
gitsocial push --site-only
```

### gitsocial mirror

Mirrors a forge-hosted project into a bucket: fetch from the forge, import issues, pull requests, releases and discussions, then push data, code and the site. Re-running refreshes. It is safe from cron, and a crashed run resumes.

```
gitsocial mirror <forge-url> <s3-url> --url <public-url>   # cold start: clone, import, push
gitsocial mirror <s3-url>                                  # in a workspace: attach the bucket, import, push
gitsocial mirror                                           # refresh
gitsocial mirror --dry-run <forge-url> <s3-url>            # the provider checklist and the plan
```

- The forge URL stays `origin`; the bucket is a second remote. Every upstream branch is mirrored unless `--default-branch-only`.
- `--url` sets `site.url` and turns the HTML pages on. `-n` caps items per type on import. `-y` never prompts; missing credentials then fail naming the `gitsocial config credentials set` command.
- Creating the bucket, allowing public reads and attaching a domain are provider dashboard steps.

### gitsocial clone

`git clone` with `s3://` support: pasted provider URLs are normalized, and the helper alias is written to the clone's local config so plain git works there.

```
gitsocial clone <url> [directory]
```

### gitsocial remote

```
gitsocial remote add [name] <url> [--default] [--site]    # --default adds it to gitsocial.pushRemote; --site sets site.publish
gitsocial remote default [name...]                        # set the default push remotes, or print the current resolution
gitsocial remote put <key> <file> [--remote <name>]       # upload one file to the bucket root, e.g. an installer
```

An s3 URL is normalized on `add`; the accepted shapes are in [S3.md](S3.md#push). Any other URL is added as an ordinary git remote.

### gitsocial config

Core configuration in `refs/gitmsg/core/config`, the site keys, and the credential store.

```
gitsocial config get <key> | set <key> <value> | list
gitsocial config site set <key> <value>          # see STATIC-SITE.md
gitsocial config credentials set <host>          # see S3.md
```

### gitsocial settings

User settings, stored in the personal bare repo and synced across machines. Keys are in [SETTINGS.md](SETTINGS.md).

```
gitsocial settings get <key> | set <key> <value> | list
```

### gitsocial personal

The personal bare repo that holds settings and personal-tier memos. See [SETTINGS.md](SETTINGS.md#cross-machine-sync).

```
gitsocial personal init [--remote <url>]
gitsocial personal sync [--push-only | --fetch-only]
gitsocial personal status
```

### gitsocial fork

Registers other repositories as forks of this one. Issues and pull requests filed in a fork appear here; an edit to another repository's item stays a proposal until its owner accepts it.

```
gitsocial fork add <url>
gitsocial fork remove <url>
gitsocial fork list
gitsocial fork create <destination>     # fork on a forge or a bucket, clone it, wire origin and upstream
```

Forks are for two-way collaboration on the same items. Lists (`gitsocial social list`) are for one-way following. A repository can be both.

### gitsocial explore

```
gitsocial explore [--list <name>]      # cached repositories
```

### gitsocial related

```
gitsocial related <repository>         # repositories sharing lists or authors with it
```

### gitsocial log

Activity log of this repository, or of the timeline with `--scope timeline`.

```
gitsocial log [--type post,comment] [--after <date>] [--author <email>] [--limit <n>]
```

### gitsocial search

Search across every extension, with per-type filters and grouping.

```
gitsocial search "query"
gitsocial search --type pr --state open --json
gitsocial search --type issue --labels bug --assignee dev@example.com
gitsocial search --type pr --group-by state --count-only
```

### gitsocial show

```
gitsocial show <ref>          # any item; the extension is detected
```

### gitsocial history

```
gitsocial history <ref>       # the edit history of any message
```

### gitsocial notifications

```
gitsocial notifications [--all] [--type mention,follow] [--limit <n>]
gitsocial notifications count
gitsocial notifications read <id> | read-all
gitsocial notifications unread <id> | unread-all
```

### gitsocial id

Signature verification and identity resolution. The trust model is in [IDENTITY.md](IDENTITY.md).

```
gitsocial id verify <commit>
gitsocial id resolve <email>
```

DNS verification is off by default: `gitsocial settings set identity.dns_verification true`.

### gitsocial tui

```
gitsocial tui
```

Keys are in [TUI-KEYS.md](TUI-KEYS.md).

### gitsocial rpc

JSON-RPC over stdio for editor integration. See [RPC.md](RPC.md).

```
gitsocial rpc
```

### gitsocial docs

```
gitsocial docs keybindings [--json]
```

## Import

`gitsocial import` copies issues, milestones, releases, pull requests and discussions from a forge into the extensions. The origin remote is the default source. Re-running skips what is already imported.

```
gitsocial import [url]              # everything: pm, release, review, social, in that order
gitsocial import pm [url]           # milestones, issues and their comments
gitsocial import release [url]      # releases and artifact metadata
gitsocial import review [url]       # fork registrations, pull requests and their comments
gitsocial import social [url]       # discussions and their comments
gitsocial import pm --state open --limit 100 --dry-run
```

- URLs: `https://github.com/org/repo`, `git@github.com:org/repo.git` or `github.com/org/repo`.
- GitHub imports through the `gh` CLI, GitLab through its REST API. Comments and discussions come from GitHub only. Other hosts are detected but not imported.
- `--host` forces the host type, `--api-url` points at a self-hosted instance, `--token` overrides the platform CLI's token.
- `--update` syncs changes to items already imported. `--labels auto|raw|skip` controls label mapping. `--email-map <file>` maps usernames to emails. `--skip-bots` is on by default.
- The mapping file `~/.cache/gitsocial/imports/<url-slug>.json` records platform ids against commit hashes; `--map-file` overrides it.

## Scripting

```bash
gitsocial pm issue list --labels kind/wontfix --json | jq -r '.[].id' | xargs -I {} gitsocial pm issue close {}
gitsocial review pr list --json | jq -e 'length > 0' >/dev/null && echo "PRs pending review"
echo "Hello world" | gitsocial social post -          # stdin as the body
cat CHANGELOG.md | gitsocial release create -
```

## Reference Format

References follow [GITMSG.md §1.3](../specs/GITMSG.md#13-reference-sections):

- `#commit:abc123456789@main` - a commit; a bare hash prefix (`abc123`) works when unambiguous
- `#branch:main`, `#tag:v1.0.0`
- `#file:src/auth.go@main`, `#file:src/auth.go@main:L42`, `#file:src/auth.go@main:L42-50`, `#file:src/auth.go@main:v1.0.0`
- `https://github.com/user/repo#commit:abc123456789@main` - a reference in another repository

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Permission denied |
| 4 | Network error |
| 5 | Not a git repository |

## Environment Variables

| Variable | Purpose |
|---|---|
| `XDG_CONFIG_HOME` | config root, default `~/.config`; gitsocial uses `<root>/gitsocial` |
| `GITSOCIAL_PERSONAL_REPO` | path of the personal bare repo, default `<config>/personal` |
| `GITSOCIAL_EDITOR` | editor for messages; falls back to `$EDITOR` |
| `GM_PAGER` | pager for output; falls back to `$PAGER` |
| `GITSOCIAL_PPROF` | `cpu`, `mem` or `trace`: write a profile to `/tmp/gitsocial-cpu.pprof`, `/tmp/gitsocial-mem.pprof` or `/tmp/gitsocial.trace` on exit |
| S3 credentials, endpoints and tuning | see [S3.md](S3.md#environment-variables) |
