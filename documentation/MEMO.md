# Memo Extension

Memos store knowledge as commits on the `gitmsg/memo` branch, organized by a label vocabulary (`kind/policy`, `priority/critical`, `topic/cache`, `expires/<date>`) and a tier model.

> **Spec:** Memos use the core protocol directly — see [GITMSG.md](../specs/GITMSG.md) (especially §1.5 versioning and §1.7 labels). No memo-specific extension spec exists yet.

## Tiers

A memo is stored at one of five tiers. Tiers describe *who shares the memo* and *how local it is to the current task*. The tier model is a GitSocial layer; underneath, every tier is a `gitmsg/memo` branch. `gitsocial memo list` merges results across tiers in retrieval order, most-locally-relevant first.

| Order | Tier | On disk | Push target | Default lifetime | In default `memo list` |
|---|---|---|---|---|---|
| 1 | Session | Bare repo at `~/.cache/gitsocial/memo/session/<id>/` | Per-session remote (when configured) | Until retracted (or session gc) | Yes (current session only) |
| 2 | Personal | Bare repo at `~/.config/gitsocial/personal/` (shared with core settings) | User's private remote (when configured) | Until retracted | Yes |
| 3 | Project | Workspace `.git` (`refs/gitmsg/memo`) | Project remote | Until retracted | Yes |
| 4 | Inherited | `~/.cache/gitsocial/repositories/` (memo sources declared binding via `memo inherit add`) | None | Until retracted | Yes |
| 5 | External | `~/.cache/gitsocial/repositories/` (incidentally-followed memo repos) | None | Until retracted | No (opt in via `--include-external` or `--tier external`) |

**Tier order is retrieval, not authority.** When `memo list` has to rank or truncate, the order above decides what surfaces first — session captures are most contextually fresh; external memos are noise unless explicitly requested. Authority (which memo "wins" when two contradict) is *not* a function of tier; it rides on the memo's own labels (`priority/critical`, future `enforce`) so a `priority/critical` inherited org policy outweighs a `priority/low` personal preference regardless of tier rank.

**Inherited vs External:** any followed repo with a `gitmsg/memo` branch surfaces memos. Inherited memos come from sources the project explicitly declared binding (`memo inherit add <url>`) — they belong in default queries because they're relevant to this codebase. External memos come from repos followed for unrelated reasons (e.g., a colleague's repo subscribed for code review) and are hidden by default to keep `memo list` focused.

## Initialize tier repos

- Project and personal tiers require explicit init.
- Sessions auto-create on first write; no init needed.
- Personal cross-machine sync requires a remote on the personal bare repo.
- All init commands are idempotent.

```
gitsocial memo project init                          # project tier (workspace's gitmsg/memo)
gitsocial memo personal init                         # personal tier (~/.config/gitsocial/personal/)

# Attach a remote so personal memos sync across machines. The personal repo is
# shared with core settings, so `gitsocial personal init --remote <url>` is the
# preferred path — it syncs settings AND memos in one shot.
gitsocial personal init --remote <url>               # one-time: attach origin
gitsocial personal sync                              # push + fetch refs/heads/* and refs/gitmsg/*

# Memo-only sync (when you don't want to touch settings):
gitsocial memo personal push                         # push gitmsg/memo
gitsocial memo personal fetch                        # fetch gitmsg/memo and re-sync cache
```

**Concurrent writes across machines:** when two machines push to the same personal repo between syncs, the histories diverge. `gitsocial memo personal push` and `fetch` (and the session equivalents) detect the divergence and auto-create an empty-tree merge commit with both tips as parents — the union of both sides' memos remains reachable. Merge commits have no `Ext: memo` header, so they don't appear as memos in `memo list`; only the underlying captures do.

## Author and promote

- By default, memos are written to the session tier.
- Promotion copies a memo to a higher tier as a fresh, standalone commit. There is no back-reference to the source — sessions and personal tiers are scratch space, and project commits should hold the polished form, not embed the rough draft. The source stays put until session GC or explicit retraction, so cross-tier promotions appear in `memo list` once per tier.
- Project-tier memos sync via `gitsocial push` / `fetch` (TUI: `p` / `f`).

```
gitsocial memo create "Cache writes use ExecLocked" \
    --labels kind/policy,priority/high,topic/cache \
    --scope project        # session | personal | project (default: session)

gitsocial memo edit <ref> --body "Updated guidance..."
gitsocial memo retract <ref>

gitsocial memo promote <ref> --to project
gitsocial memo promote <ref> --to personal
```

## Sessions

- Each session has its own bare repo at `~/.cache/gitsocial/memo/session/<id>/` (override with `MEMO_SESSION_DIR`). The id is resolved in priority order: `MEMO_SESSION_ID` env var, then auto-generated `<YYYYMMDD>-<8 hex>`. User-provided ids are free-form (e.g., `daily`, `client-acme`, `task-auth-bug`).
- Sessions persist by default; cleanup is always explicit. `session gc` deletes the session and any memos that weren't promoted.
- Writes auto-create a session using the resolved id if `session init` hasn't been called.
- Sessions don't sync by default; to back one up, add a remote to its bare repo first.

By default, queries see only the current session's refs:

```
gitsocial memo list                           # current session only
gitsocial memo list --include-sessions all    # all sessions (review/promotion)
gitsocial memo list --include-sessions <id>   # one specific session
```

Manage them:

```
gitsocial memo session init [<id>]           # create or resume; prints resolved id
gitsocial memo session list                  # list active sessions with ages
gitsocial memo session push <id>             # push to remote (if configured)
gitsocial memo session fetch <id>            # fetch from remote (if configured)
gitsocial memo session gc <id>               # delete a specific session
gitsocial memo session gc --older-than 30d   # delete sessions inactive past N days
```

## Labels

Memos use the core `labels` field: comma-separated `<scope>/<value>` pairs (e.g. `labels="kind/policy,priority/high"`). All labels are optional. Common conventions:

| Scope | Example values |
|-------|----------------|
| `kind/` | `policy`, `guideline`, `fact`, `reference`, `context`, `decision` |
| `priority/` | `low`, `normal`, `high`, `critical` |
| `expires/` | `<YYYY-MM-DD>` (or full ISO 8601 `<YYYY-MM-DDTHH:MM:SSZ>` when time-of-day matters) |
| `topic/`, `area/`, `workflow/`, `compliance/`, `audience/` | freeform categorical |

Teams can define their own scopes. For formal taxonomies, declare a `vocab/<name>` label and use that taxonomy's terms (e.g. `vocab/dewey,dewey/005.8`); see GITMSG.md Section 1.7 for the core label format.

## Inherits

Inheriting a memo source declares it relevant to this project — its memos belong in default queries because the project agreed they apply here. Binding sources are tracked at `refs/gitmsg/memo/inherits/` (one ref per URL); their memos surface as the inherited tier in `memo list`.

Repos that aren't inherited but happen to be followed (e.g., a colleague's repo subscribed for code review) fall in the external tier and are hidden from default queries; opt in with `--include-external` or `--tier external`.

How "binding" enforcement actually happens: a binding policy memo carries `priority/critical` (or, in future, an `enforce` flag). That label, not the inherited tier rank, is what tells agents and tooling the policy must override local preferences. Tier rank only governs retrieval order.

```
gitsocial memo inherit add <url>      # register a binding memo source
gitsocial memo inherit list           # show inherited sources
gitsocial memo inherit remove <url>   # unregister
gitsocial memo list --tier inherited  # memos from inherited sources only
gitsocial memo list --tier external   # memos from incidentally-followed repos
gitsocial memo list --include-external # default merge plus external
```

**`inherit add` auto-follows the source.** Behind the scenes, `inherit add <url>` ensures the URL is in a managed social list named `memo-inherits` with `--all-branches`, so the source's `gitmsg/memo` branch is included in every subsequent `gitsocial fetch`. The list shows up in `gitsocial social list ls` if you want to audit what's there; `inherit remove <url>` removes the URL from both the inherits ref set and the list.

If the URL is already followed via another list, that's fine — `memo-inherits` is additive, fetch dedupes by URL.

## Search

Memos surface through core `gitsocial search` with the same defaults as `memo list` (current session + higher tiers). `--labels` and free-text work generically; `memo` registers as a type and `--tier` scopes by tier.

## Storage paths

| Env var | Default | Effect |
|---|---|---|
| `GITSOCIAL_PERSONAL_REPO` | `~/.config/gitsocial/personal` | Personal bare repo (shared with core settings sync — see [SETTINGS.md](SETTINGS.md)) |
| `MEMO_SESSION_DIR` | `~/.cache/gitsocial/memo/session` | Parent directory holding per-session bare repos |
| `MEMO_SESSION_ID` | (auto-generated per process) | Pin the active session id |

These are read-only at runtime — override via the environment, not via `gitsocial settings set`.

## How memos surface in queries

Default `memo list` filters:

- Excludes retracted (latest version wins).
- Excludes memos with a past `expires/<date>` label; `--include-expired` to override; `--expired` shows only those.
- Excludes session refs unless they belong to the current session id (override with `--include-sessions`).
- Excludes external-tier (incidentally-followed) repos; opt in with `--include-external` or `--tier external`.
- Excludes commits removed from the source branch (force-pushed away).

Order (retrieval, not authority):

1. Tier locality: session > personal > project > inherited > external.
2. Priority label rank within a tier: `priority/critical` > `priority/high` > `priority/normal` > `priority/low`.
3. Recency: among equal-priority memos, more recent first.

When two memos contradict, the winner is decided by `priority/` labels, not by tier — a `priority/critical` inherited memo outweighs a `priority/low` session capture even though the session capture surfaces first.

## Operational checks

```bash
# What's on each tier right now?
gitsocial memo list --tier session --json
gitsocial memo list --tier personal --json
gitsocial memo list --tier project --json
gitsocial memo list --tier inherited --json
gitsocial memo list --tier external --json

# Where do tier repos live?
echo "personal: ${GITSOCIAL_PERSONAL_REPO:-~/.config/gitsocial/personal}"
echo "sessions: ${MEMO_SESSION_DIR:-~/.cache/gitsocial/memo/session}"

# Inspect raw cache state (memos live on the gitmsg/memo branch on each tier repo)
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT repo_url, COUNT(*) FROM core_commits
     WHERE branch = 'gitmsg/memo' GROUP BY repo_url"

# List active sessions
ls ~/.cache/gitsocial/memo/session/
```

The TUI's memo view groups memos by tier (the easiest way to confirm a promotion landed) and shows the full edit chain in detail view for in-place edits. Press `M` from any TUI screen to jump to memos.

## Workflows

End-to-end recipes for common usage patterns. Each one assumes you have the CLI installed and a git repo as your workspace.

### Solo: capture-as-you-work

The lightest path — write to session, promote whatever survives the day.

```bash
gitsocial memo create "Cache writes must use ExecLocked" \
    --labels kind/policy,priority/high,topic/cache \
    --body "Concurrent writers without ExecLocked caused data loss in incident X."

gitsocial memo list                              # see what you've captured so far
gitsocial memo promote <ref> --to project        # for memos worth committing to git
```

Project promotion requires one-time `gitsocial memo project init`. Project-tier memos push and fetch with the rest of the workspace via `gitsocial push` / `gitsocial fetch`.

### Team: shared project policies

Push a memo to the workspace's `gitmsg/memo` branch; teammates fetch and see it.

```bash
# Author
gitsocial memo create "Use ExecLocked for cache writes" \
    --scope project --labels priority/critical,kind/policy
gitsocial push

# Reader
gitsocial fetch
gitsocial memo list --tier project
```

Comments are first-class: in the TUI, hit `c` on a memo to start a thread. Note that promoting a memo to a different tier creates a fresh commit, so comments attached to the source don't follow the promotion — comment *after* the memo reaches its final tier.

### Org: binding policies via inherits

`memo inherit add` registers a repo as a binding source and follows it in one shot — the URL is added to a managed `memo-inherits` list with `--all-branches`, so the source's `gitmsg/memo` branch is picked up by every subsequent fetch.

```bash
gitsocial memo inherit add https://github.com/org/policies
gitsocial fetch
gitsocial memo list --tier inherited
```

Authority is by label, not tier — a `priority/critical` inherited policy outweighs a local `priority/low` capture (see [How memos surface in queries](#how-memos-surface-in-queries)). The managed list shows up in `gitsocial social list ls`; `memo inherit remove <url>` cleans up both the inherits ref and the list entry.

### Cross-machine personal memos

Personal-tier memos share the bare repo at `~/.config/gitsocial/personal` with core settings, so one remote covers both.

```bash
# One-time, on each machine
gitsocial personal init --remote git@example.com:alice/gitsocial-personal.git
gitsocial memo personal init

# Push from machine A
gitsocial memo create "I prefer early returns" --scope personal
gitsocial personal sync     # syncs settings AND memos

# Pull on machine B
gitsocial personal sync
gitsocial memo list --tier personal
```

If both machines write between syncs, the second `push` (or `fetch`) auto-merges — the `gitmsg/memo` branch is empty-tree and append-only, so divergence has no conflict surface. Both sides' memos remain accessible from the merged tip.

### AI agent: ephemeral sessions

Agents capture as they work, then surface session memos for review before GC.

```bash
# Agent process — pinning the session id makes the run resumable.
export MEMO_SESSION_ID=task-auth-bug

# During the task
gitsocial memo create "Auth uses bcrypt cost 12 in prod" --labels kind/fact,topic/auth

# End of task — user reviews, decides what to keep
gitsocial memo list --tier session --include-sessions task-auth-bug --json
gitsocial memo promote <ref> --to project    # for keepers

# Cleanup
gitsocial memo session gc task-auth-bug      # explicit
# or, weekly cron:
gitsocial memo session gc --older-than 30d   # everything inactive past 30 days
```

Each session has its own bare repo, so concurrent agents on different `MEMO_SESSION_ID`s don't collide. Session GC removes both the bare repo and its cache rows; promoted memos live on at the higher tier and survive untouched.
