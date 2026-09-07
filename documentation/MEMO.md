# Memo Extension

Memos are knowledge kept as commits on a `gitmsg/memo` branch (core protocol only, [GITMSG.md](../specs/GITMSG.md)), at one of five tiers and organized by labels.

[Tiers](#tiers) · [Initialize](#initialize) · [Write and promote](#write-and-promote) · [Sessions](#sessions) · [Inherit](#inherit) · [Labels](#labels) · [Workflows](#workflows) · [Reference](#reference)

## Tiers

| Order | Tier | Repository | Syncs with | In `memo list` by default |
|---|---|---|---|---|
| 1 | session | `~/.cache/gitsocial/memo/session/<id>/` | its own remote, when one is set | the current session only |
| 2 | personal | `~/.config/gitsocial/personal/`, shared with settings | the personal remote | yes |
| 3 | project | the workspace, `refs/gitmsg/memo` | the project remote | yes |
| 4 | inherited | followed repositories declared with `memo inherit add` | none | yes |
| 5 | external | other followed repositories with a memo branch | none | no; `--include-external` or `--tier external` |

The order is retrieval order, most local first. Which memo wins when two contradict is decided by `priority/` labels, not by tier.

## Initialize

```
gitsocial memo project init
gitsocial memo personal init
gitsocial personal init --remote <url>     # once per machine, so personal memos and settings sync
gitsocial personal sync [--push-only | --fetch-only]
```

Sessions create themselves on first write. Two machines writing between syncs merge automatically, and the merge commit is not a memo.

## Write and promote

```
gitsocial memo create "Cache writes use ExecLocked" --labels kind/policy,priority/high,topic/cache [--body ...] [--scope session|personal|project]
gitsocial memo edit <ref> [--subject ...] [--body ...] [--labels ...]
gitsocial memo retract <ref>
gitsocial memo promote <ref> --to personal|project
gitsocial memo list | show <ref>
```

- The default scope is the session.
- Promotion copies the memo to the higher tier as a new commit with no back-reference; the source stays until it is retracted or its session is collected.
- Project memos travel with `gitsocial push` and `gitsocial fetch`.

## Sessions

```
gitsocial memo session init [<id>]            # create or resume; prints the id
gitsocial memo session list
gitsocial memo session sync <id> [--push-only | --fetch-only]
gitsocial memo session gc <id> | --older-than 30d
gitsocial memo list --include-sessions all|<id>
```

- The id comes from `MEMO_SESSION_ID`, else `<YYYYMMDD>-<8 hex>`. Any string works: `daily`, `task-auth-bug`.
- Sessions persist until `gc`, which deletes the repository and every memo not promoted.

## Inherit

```
gitsocial memo inherit add <url>
gitsocial memo inherit list
gitsocial memo inherit remove <url>
gitsocial memo list --tier inherited | --tier external | --include-external
```

`inherit add` records the source at `refs/gitmsg/memo/inherits/` and adds it to the managed social list `memo-inherits` with all branches, so every fetch picks up its memo branch; `remove` undoes both.

## Labels

Labels are the core `<scope>/<value>` field ([GITMSG.md §1.7](../specs/GITMSG.md#17-labels)).

| Scope | Values |
|---|---|
| `kind/` | `policy`, `guideline`, `fact`, `reference`, `context`, `decision` |
| `priority/` | `low`, `normal`, `high`, `critical` |
| `expires/` | `YYYY-MM-DD`, or full ISO 8601 |
| `topic/`, `area/`, `workflow/`, `compliance/`, `audience/` | free |
| `vocab/<name>` | declares a taxonomy whose terms follow as `<name>/<term>` |

## Workflows

- **Solo.** Write to the session as you work, `memo list` at the end of the day, `promote --to project` what should stay.
- **Team.** `memo create --scope project --labels priority/critical,kind/policy`, then `gitsocial push`; teammates `fetch` and `memo list --tier project`. Comment on a memo after it reaches its final tier, since promotion creates a new commit and comments stay with the source.
- **Organization.** `memo inherit add https://github.com/org/policies`, `fetch`, `memo list --tier inherited`. A `priority/critical` inherited policy outranks a local `priority/low` capture.
- **Several machines.** `gitsocial personal init --remote <url>` and `memo personal init` on each; `memo create --scope personal`, then `gitsocial personal sync` on both.
- **Agents.** Set `MEMO_SESSION_ID=task-auth-bug` so the run resumes; capture with `memo create`; review with `memo list --include-sessions task-auth-bug --json`; promote the keepers; `memo session gc task-auth-bug`, or `gc --older-than 30d` from cron. Sessions with different ids never collide.

## Reference

- Versions follow [GITMSG.md §1.5](../specs/GITMSG.md#15-versioning) and labels [§1.7](../specs/GITMSG.md#17-labels).
- `memo list` excludes retracted memos, expired ones (`--include-expired` shows them, `--expired` shows only them), other sessions, the external tier, and commits no longer on their branch ([ARCHITECTURE.md](ARCHITECTURE.md#cache)). Order: tier, then `priority/` rank, then recency.
- [`gitsocial search`](CLI.md#gitsocial-search) `--type memo` applies the same defaults; `--tier` scopes it.
- `GITSOCIAL_PERSONAL_REPO` overrides the personal repository, `MEMO_SESSION_DIR` the sessions directory and `MEMO_SESSION_ID` the session, all from the environment only ([SETTINGS.md](SETTINGS.md#environment)).
- In the TUI, `M` opens memos, grouped by tier ([TUI-KEYS.md](TUI-KEYS.md#memo-extension)).
