# Project Management Extension

Issues, milestones, and sprints stored as commits on the `gitmsg/pm` branch. State changes (close, reopen, label edits) are edits to the canonical commit; comments live on the social branch.

> **Spec:** [GITPM.md](../specs/GITPM.md) — wire format for issues, milestones, sprints, and links.

## Initialize

```
gitsocial pm init                     # creates refs/gitmsg/pm/config and the gitmsg/pm branch
gitsocial pm init -b <branch>         # initialize on a custom branch
gitsocial pm config get / set / list
```

`init` is idempotent. Branch resolution follows GITMSG.md Section 3.3.

## Issues

```
gitsocial pm issue create "Login page returns 500" \
    -l kind/bug,priority/high \
    -a alice@example.com,bob@example.com \
    -m <milestone-hash> -s <sprint-hash> \
    -d 2026-06-01 \
    --blocks <hash>,<hash> --blocked-by <hash> --related <hash>

gitsocial pm issue list -s open -l kind/bug
gitsocial pm issue list -f 'state:open priority:high assignee:alice@example.com due:overdue'
gitsocial pm issue show <ref>
gitsocial pm issue close <ref>
gitsocial pm issue reopen <ref>
gitsocial pm issue comment <ref> "Repro steps below..."
gitsocial pm issue comments <ref>
```

Issue links (`blocks`, `blocked-by`, `related`) are stored in `pm_links`. Sub-issues use `parent` (and `root` is denormalized for fast tree queries).

Issues auto-close when a PR with a matching `closes="<issue-ref>"` transitions to `state="merged"`.

## Milestones and sprints

```
gitsocial pm milestone create "v1.0" --due 2026-06-30
gitsocial pm milestone close <ref>
gitsocial pm milestone cancel <ref>     # close without "completed" semantics
gitsocial pm milestone delete <ref>     # retract

gitsocial pm sprint create "Sprint 14" --start 2026-05-01 --end 2026-05-14
gitsocial pm sprint start <ref>         # transition to active
gitsocial pm sprint complete <ref>
gitsocial pm sprint cancel <ref>
gitsocial pm sprint delete <ref>        # retract
```

Issues are linked to milestones/sprints via `--milestone` / `--sprint` flags at create or via `pm issue` edits.

## Labels

PM uses the core `labels` field (comma-separated `<scope>/<value>`). Common conventions:

| Scope | Example values |
|-------|----------------|
| `kind/` | `bug`, `feature`, `task`, `chore`, `docs` |
| `priority/` | `low`, `normal`, `high`, `critical` |
| `area/`, `topic/` | freeform categorical |

See GITMSG.md Section 1.7 for the core label format.

## Forks (cross-fork issue discovery)

```
gitsocial fork add <fork-url>           # registered in core config
gitsocial fetch                         # picks up issues, PRs, etc. from each fork
```

Issues opened on a registered fork appear in the upstream's `pm issue list` and trigger notifications. Fork registration lives at `refs/gitmsg/core/forks/<urlHash>` (per-element refs, no write contention).

## Board view

```
gitsocial pm board                      # CLI-only summary; rich kanban is in the TUI
```

The TUI's PM section (`B` from any screen — Board) groups issues by state, with separate views for issues, milestones, sprints, and detail/history.

## How items surface in queries

Default `issue list` filters:

- Excludes retracted (latest version wins).
- Excludes commits removed from the source branch (force-pushed away).
- Filter flags: `-s/--state`, `-l/--labels`, plus the rich filter query via `-f` (`state:open priority:high assignee:<email> milestone:<hash> sprint:<hash> due:today|overdue|week`, prefix with `-` to exclude, and freeform text for full-text search).

## Notifications

Mentions, assignments, and link updates surface through core notifications. See [NOTIFICATIONS.md](NOTIFICATIONS.md).

## Operational checks

```bash
gitsocial pm config get

# Issue counts by state
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT type, state, COUNT(*) FROM pm_items_resolved GROUP BY type, state"

# Open issues assigned to a user
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT i.hash, i.state FROM pm_items_resolved i
     JOIN pm_assignees a USING(repo_url, hash, branch)
     WHERE a.email = 'alice@example.com' AND i.state = 'open'"
```
