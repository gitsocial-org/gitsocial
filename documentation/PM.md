# Project Management Extension

Issues, milestones and sprints are commits on the `gitmsg/pm` branch ([GITPM.md](../specs/GITPM.md)). A state change is an edit of the original commit, and comments are on the social branch.

[Initialize](#initialize) · [Issues](#issues) · [Milestones and sprints](#milestones-and-sprints) · [Labels](#labels) · [Forks](#forks) · [Board](#board) · [Reference](#reference)

## Initialize

```
gitsocial pm init
gitsocial pm config get|set|list
```

`init` is idempotent. It creates `refs/gitmsg/pm/config` and the `gitmsg/pm` branch.

## Issues

```
gitsocial pm issue create "Login page returns 500" -l kind/bug,priority/high -a alice@example.com -d 2026-06-01
gitsocial pm issue create "Add OAuth" -m <milestone> -s <sprint> --parent <issue> --blocks <issue> --blocked-by <issue> --related <issue>
gitsocial pm issue list [-s open] [-l kind/bug] [--sort <field>] [-n 50]
gitsocial pm issue list -f 'state:open priority:high assignees:alice@example.com due:overdue'
gitsocial pm issue show <ref>
gitsocial pm issue edit <ref> [--subject ...] [--body ...] [--state ...] [-l ...] [-a ...]
gitsocial pm issue close <ref>
gitsocial pm issue reopen <ref>
gitsocial pm issue adopt <ref>                 # adopt a registered fork's issue into this repository
gitsocial pm issue comment <ref> "Repro steps below"
gitsocial pm issue comments <ref>
```

- `-f` takes the field terms `state:`, `assignees:`, `milestone:`, `parent:`, `root:` and `due:`, a leading `-` to exclude, and quoted free text for full-text search. Each other `<scope>:<value>` term matches the label `<scope>/<value>`, so `priority:high` finds `priority/high`.
- `due:` takes `today`, `overdue`, `week` or `<n>d`.
- `--sort` takes `created`, `due` or `priority`, each with `:asc` or `:desc`. `updated` is also accepted and sorts the same as `created`.
- A sub-issue names its `--parent`, and GitSocial derives `root` from it. `--blocks`, `--blocked-by` and `--related` link issues.
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

The `framework` config declares the values for each scope. `minimal` declares none.

| Scope | `kanban` | `scrum` |
|---|---|---|
| `kind/` | `bug`, `feature`, `task`, `chore` | `story`, `bug`, `task`, `spike` |
| `priority/` | `critical`, `high`, `medium`, `low` | `critical`, `high`, `medium`, `low` |
| `status/` | `in-progress`, `review`, `done` | `sprint-backlog`, `in-progress`, `review` |
| `points/` | | `1`, `2`, `3`, `5`, `8`, `13` |

`area/`, `team/`, `needs/` and `release/` are free.

## Forks

```
gitsocial fork add <fork-url>
gitsocial fetch
```

Issues opened on a registered fork appear in `pm issue list` and create notifications. `pm issue adopt` adopts one into this repository with no change; an edit, a close or an assignment adopts it the same way. The adopted issue has an `adopts` field, and later changes edit it. An edit from a different repository is a proposal until the owner accepts it.

## Board

```
gitsocial pm board          # a summary; the kanban board is in the TUI
```

Columns come from a custom `boards` list or, if there is none, from the `framework` config (`minimal`, `kanban`, `scrum`) ([GITPM.md §2](../specs/GITPM.md#2-configuration)).

## Reference

- Links and hierarchy: [GITPM.md §1.6](../specs/GITPM.md#16-issue-links) and [§1.7](../specs/GITPM.md#17-hierarchy-references).
- `issue list` excludes retracted items ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and stale commits ([ARCHITECTURE.md](ARCHITECTURE.md#cache)), and shows the latest version of each issue.
- Links are stored in `pm_links`, assignees in `pm_assignees` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)).
- Mentions, assignments and link changes create [notifications](NOTIFICATIONS.md#types).
- In the TUI, `P` opens the board ([TUI-KEYS.md](TUI-KEYS.md#pm-extension)).
