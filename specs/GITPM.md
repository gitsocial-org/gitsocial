# GitPM Extension Specification

Work tracking extension for GitMsg (name: `pm`, version: `0.1.0`).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

### 1.1. Types

- `issue`: Bug, task, feature request, or story
- `milestone`: Release or version milestone
- `sprint`: Time-boxed iteration (agile sprint)

For comments, see GitSocial.

### 1.2. Labels

Scoped labels (core `labels` field). All scopes are OPTIONAL.

| Scope | Example values |
|-------|----------------|
| `status/` | `backlog`, `in-progress`, `review`, `done` |
| `priority/` | `low`, `medium`, `high`, `critical` |
| `kind/` | `bug`, `feature`, `task`, `story` |

Teams MAY define custom scopes: `area/*`, `team/*`, `needs/*`, `release/*`.

### 1.3. Issue Fields

Issue fields (in header order):
- `state`: MUST be `open` or `closed`
- `assignees`: MAY contain comma-delimited email addresses
- `due`: MAY contain an ISO 8601 date
- `milestone`: MAY reference a milestone commit
- `sprint`: MAY reference a sprint commit
- `parent`: MAY reference a parent issue commit (only if parent differs from root)
- `root`: MAY reference the root issue commit (always included for any nested issue)
- `blocks`: MAY contain comma-delimited issue commit refs that this issue blocks
- `blocked-by`: MAY contain comma-delimited issue commit refs that block this issue
- `related`: MAY contain comma-delimited issue commit refs related to this issue
- `labels`: MAY contain scoped labels

Field order: `state`, `assignees`, `due`, `milestone`, `sprint`, `parent`, `root`, `blocks`, `blocked-by`, `related`, `labels`.

### 1.4. Milestone Fields

Milestone fields (in header order):
- `state`: MUST be `open`, `closed`, or `cancelled`
- `due`: MAY contain an ISO 8601 date

Field order: `state`, `due`.

### 1.5. Sprint Fields

Sprint fields (in header order):
- `state`: MUST be `planned`, `active`, `completed`, or `cancelled`
- `start`: MUST contain an ISO 8601 date (sprint start)
- `end`: MUST contain an ISO 8601 date (sprint end)

Field order: `state`, `start`, `end`.

Only one sprint SHOULD be `active` at a time per repository/branch.

### 1.6. Issue Links

Issues MAY declare dependency and informational relationships using `blocks`, `blocked-by`, and `related` fields. Each field accepts comma-delimited issue commit refs.

- `blocks` and `blocked-by` are directional inverses: if A declares `blocks=B`, then B is implicitly blocked by A
- `related` is bidirectional and informational — no workflow constraint
- Closing a blocked issue SHOULD NOT be prevented, but implementations MAY warn the user

### 1.7. Hierarchy References

- Direct child: `root` field MUST reference the parent issue (no `parent` field needed)
- Nested child: MUST include both `parent` (immediate parent) and `root` (top-level issue) fields

Closing or deleting an issue SHOULD NOT automatically cascade to children. Child issues remain unchanged by default.

### 1.8. Comments

Implementations MUST use GitSocial for PM item comments (issues, milestones, sprints). The `original` field references the PM item commit:

```
Love this idea, I'll start on it.

GitMsg: ext="social"; type="comment"; original="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Add dark mode support
```

Milestone comment example:
```
Adding real-time collaboration to the scope, extending due date by one week.

GitMsg: ext="social"; type="comment"; original="#commit:def456789012@gitmsg/pm"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="milestone"; author="Bob"; email="bob@example.com"; time="2025-01-01T00:00:00Z"; ref="#commit:def456789012@gitmsg/pm"; v="0.1.0"
 > Release v2.0
```

Sprint comment example:
```
Retrospective: Good velocity this sprint. Need to improve estimation.

GitMsg: ext="social"; type="comment"; original="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="sprint"; author="Alice"; email="alice@example.com"; time="2025-02-01T00:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Sprint 23: UX Polish
```

## 2. Config

Configuration MUST be stored at `refs/gitmsg/pm/config`.

Configuration MUST include: `version`.

Configuration SHOULD include: `branch`. Default: `gitmsg/pm`.

Configuration MAY include: `framework` (board framework preset: `minimal`, `kanban`, `scrum`; default: `kanban`), `boards` (array of custom board definitions).

```json
{
  "version": "0.1.0",
  "branch": "gitmsg/pm",
  "framework": "kanban"
}
```

Custom boards override framework defaults:

```json
{
  "version": "0.1.0",
  "boards": [
    {
      "id": "default",
      "name": "Board",
      "columns": [
        { "name": "Backlog", "filter": "state:open" },
        { "name": "In Progress", "filter": "status:in-progress", "wip": 3 },
        { "name": "Review", "filter": "status:review" },
        { "name": "Done", "filter": "state:closed" }
      ]
    }
  ]
}
```

Board column fields:
- `name`: Display name (REQUIRED)
- `filter`: Match expression — `state:<value>` or `status:<value>` (REQUIRED)
- `wip`: Work-in-progress limit (OPTIONAL)

Board resolution: (1) Use custom `boards` if defined, (2) otherwise derive from `framework`, (3) otherwise use default kanban board. Issues without a matching column filter SHOULD appear in the first column. Issues with `state=closed` SHOULD appear in the last column.

## Appendix: Manifest

```json
{
  "name": "pm",
  "version": "0.1.0",
  "display": "GitPM",
  "description": "Work tracking extension for GitMsg",
  "types": ["issue", "milestone", "sprint"],
  "fields": ["state", "assignees", "due", "milestone", "sprint", "parent", "root", "blocks", "blocked-by", "related", "start", "end"]
}
```

## Appendix: Validation

| Field/Scope | Pattern |
|-------------|---------|
| `state` (issue) | `open\|closed` |
| `state` (milestone) | `open\|closed\|cancelled` |
| `state` (sprint) | `planned\|active\|completed\|cancelled` |
| `assignees` | comma-delimited email addresses |
| `due` | ISO 8601 date (YYYY-MM-DD) |
| `start` | ISO 8601 date (YYYY-MM-DD) |
| `end` | ISO 8601 date (YYYY-MM-DD) |
| `milestone` | `#commit:<hash>@<branch>` |
| `sprint` | `#commit:<hash>@<branch>` |
| `parent` | `#commit:<hash>@<branch>` |
| `root` | `#commit:<hash>@<branch>` |
| `blocks` | comma-delimited `#commit:<hash>@<branch>` |
| `blocked-by` | comma-delimited `#commit:<hash>@<branch>` |
| `related` | comma-delimited `#commit:<hash>@<branch>` |
| `status/` | from board config |
| `priority/` | `low\|medium\|high\|critical` |
| `kind/` | `bug\|feature\|task\|story` |
| label | `^[a-z]+/[a-zA-Z0-9._-]+$` |

## Appendix: Examples

Create issue:
```
Add dark mode support

GitMsg: ext="pm"; type="issue"; state="open"; labels="kind/feature,priority/high"; v="0.1.0"
```

Close issue (full replacement via core versioning):
```
Add dark mode support

GitMsg: ext="pm"; type="issue"; edits="#commit:abc123456789@gitmsg/pm"; state="closed"; labels="kind/feature,priority/high,status/done"; v="0.1.0"
```

Delete issue:
```
GitMsg: ext="pm"; edits="#commit:abc123456789@gitmsg/pm"; retracted="true"; v="0.1.0"
```

Create milestone:
```
Release v2.0

Dark mode and dashboard analytics.

GitMsg: ext="pm"; type="milestone"; state="open"; due="2025-03-15"; v="0.1.0"
```

Create sprint:
```
Sprint 23: UX Polish

Two-week sprint for user experience improvements.

GitMsg: ext="pm"; type="sprint"; state="planned"; start="2025-02-01"; end="2025-02-14"; v="0.1.0"
```

Issue with milestone, sprint, and blocking link:
```
Build real-time collaboration

GitMsg: ext="pm"; type="issue"; state="open"; milestone="#commit:def456789012@gitmsg/pm"; sprint="#commit:abc123456789@gitmsg/pm"; blocks="#commit:aaa111222333@gitmsg/pm"; labels="kind/feature,status/backlog"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="milestone"; author="Bob"; email="bob@example.com"; time="2025-01-01T00:00:00Z"; ref="#commit:def456789012@gitmsg/pm"; v="0.1.0"
 > Release v2.0
GitMsg-Ref: ext="pm"; type="sprint"; author="Alice"; email="alice@example.com"; time="2025-02-01T00:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Sprint 23: UX Polish
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-10T09:00:00Z"; ref="#commit:aaa111222333@gitmsg/pm"; v="0.1.0"
 > Deploy to production
```

Direct subtask with related link (child of epic):
```
Dark mode theme engine

GitMsg: ext="pm"; type="issue"; state="open"; assignees="alice@example.com"; root="#commit:abc123456789@gitmsg/pm"; related="#commit:ddd444555666@gitmsg/pm"; labels="kind/story,status/backlog"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-05T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Design System Epic
GitMsg-Ref: ext="pm"; type="issue"; author="Bob"; email="bob@example.com"; time="2025-01-08T14:00:00Z"; ref="#commit:ddd444555666@gitmsg/pm"; v="0.1.0"
 > Redesign settings page
```

Nested subtask blocked by multiple issues (grandchild of epic):
```
Add theme toggle component

GitMsg: ext="pm"; type="issue"; state="open"; assignees="bob@example.com"; parent="#commit:def456789012@gitmsg/pm"; root="#commit:abc123456789@gitmsg/pm"; blocked-by="#commit:bbb222333444@gitmsg/pm,#commit:ccc333444555@gitmsg/pm"; labels="kind/task,status/backlog"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:def456789012@gitmsg/pm"; v="0.1.0"
 > Dark mode theme engine
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-05T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Design System Epic
GitMsg-Ref: ext="pm"; type="issue"; author="Bob"; email="bob@example.com"; time="2025-01-11T10:00:00Z"; ref="#commit:bbb222333444@gitmsg/pm"; v="0.1.0"
 > Fix authentication bug
GitMsg-Ref: ext="pm"; type="issue"; author="Bob"; email="bob@example.com"; time="2025-01-12T10:00:00Z"; ref="#commit:ccc333444555@gitmsg/pm"; v="0.1.0"
 > Update CI pipeline
```
