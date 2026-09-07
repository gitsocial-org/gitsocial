# Project Management Extension

Issues, milestones and sprints are commits on the `gitmsg/pm` branch ([GITPM.md](../specs/GITPM.md)); a state change is an edit of the original commit, and comments live on the social branch.

[Initialize](#initialize) · [Issues](#issues) · [Milestones and sprints](#milestones-and-sprints) · [Labels](#labels) · [Forks](#forks) · [Board](#board) · [Reference](#reference)

## Initialize

```
gitsocial pm init [-b <branch>]          # refs/gitmsg/pm/config and the gitmsg/pm branch
gitsocial pm config get|set|list
```

`init` is idempotent.

## Issues

```
gitsocial pm issue create "Login page returns 500" -l kind/bug,priority/high -a alice@example.com -d 2026-06-01
gitsocial pm issue create "Add OAuth" -m <milestone> -s <sprint> --parent <issue> --blocks <issue> --blocked-by <issue> --related <issue>
gitsocial pm issue list [-s open] [-l kind/bug] [--sort <field>] [-n 50]
gitsocial pm issue list -f 'state:open priority:high assignee:alice@example.com due:overdue'
gitsocial pm issue show <ref>
gitsocial pm issue edit <ref> [--subject ...] [--body ...] [--state ...] [-l ...] [-a ...]
gitsocial pm issue close <ref>
gitsocial pm issue reopen <ref>
gitsocial pm issue comment <ref> "Repro steps below"
gitsocial pm issue comments <ref>
```

- `-f` takes `state:`, `priority:`, `assignee:`, `milestone:`, `sprint:` and `due:today|overdue|week` terms, a leading `-` to exclude, and free text for full-text search.
- A sub-issue names its `--parent`; `root` is derived. `--blocks`, `--blocked-by` and `--related` link issues.
- An issue closes when a pull request whose `--closes` names it is merged.

## Milestones and sprints

```
gitsocial pm milestone create "v1.0" --due 2026-06-30
gitsocial pm milestone list | show <ref> | edit <ref> | close <ref> | reopen <ref> | cancel <ref> | delete <ref>
gitsocial pm sprint create "Sprint 14" --start 2026-05-01 --end 2026-05-14
gitsocial pm sprint list | show <ref> | edit <ref> | start <ref> | complete <ref> | cancel <ref> | delete <ref>
```

`delete` retracts. An issue joins a milestone or sprint with `-m` or `-s` on create or edit.

## Labels

Labels are the core `<scope>/<value>` field ([GITMSG.md §1.7](../specs/GITMSG.md#17-labels)).

| Scope | Values |
|---|---|
| `kind/` | `bug`, `feature`, `task`, `story` |
| `priority/` | `low`, `medium`, `high`, `critical` |
| `status/` | the board columns: `backlog`, `in-progress`, `review`, `done` |
| `area/`, `team/`, `needs/`, `release/` | free |

## Forks

```
gitsocial fork add <fork-url>
gitsocial fetch
```

Issues opened on a registered fork appear in `pm issue list` and raise notifications. An edit made from another repository is a proposal until the owner accepts it.

## Board

```
gitsocial pm board          # a summary; the kanban board is in the TUI
```

Columns come from the `framework` config (`minimal`, `kanban`, `scrum`) or a custom `boards` list ([GITPM.md §2](../specs/GITPM.md#2-config)).

## Reference

- Links and hierarchy: [GITPM.md §1.6](../specs/GITPM.md#16-issue-links) and [§1.7](../specs/GITPM.md#17-hierarchy-references).
- `issue list` excludes retracted items ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and commits no longer on their branch ([ARCHITECTURE.md](ARCHITECTURE.md#cache)); the latest version wins.
- Links are stored in `pm_links`, assignees in `pm_assignees` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)).
- Mentions, assignments and link changes raise [notifications](NOTIFICATIONS.md#types).
- In the TUI, `P` opens the board ([TUI-KEYS.md](TUI-KEYS.md#pm-extension)).
