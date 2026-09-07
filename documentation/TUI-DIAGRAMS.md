# TUI View Diagrams

Reference layouts for the TUI's list and detail views, drawn with the data the protocol specs use as examples.

[Layout](#layout) · [List views](#list-views) · [Detail views](#detail-views) · [Routes](#routes)

## Layout

Every structured detail view follows one pattern: a header line, a hero card with the subject, a field table and the body, then sections separated by double rules, and the footer with the view's keys. The keys per view are in [TUI-KEYS.md](TUI-KEYS.md); the diagrams below leave the footer out.

```
╭─ ICON[⇡]  Subject (40ch) · Author · FormatTime · [repo]#hash ───────────╮
│                                                                         │
│ ▏Subject (bold)                                                         │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏State         value             RowStylesWithWidths(14, 0)             │
│ ▏Field         value                                                    │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Body (markdown)                                                        │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Section (count)                                                        │
│  ─────────────────────────────────────────────────────────────          │
│  ... items ...                                                          │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (count)                                                       │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  [comment card]                                                         │
│                                                                         │
│ key:label  key:label  /:search  X:retract                               │
╰─────────────────────────────────────────────────────────────────────────╯
```

## List views

List views are a `CardList`: one card per item, a `▏` bar on the selected card, and a separator between cards. `MaxLines` is the card's body height.

### Timeline

MaxLines 5, with interaction counts.

```
╭─ Timeline ──────────────────────────────────────────────────────────────╮
│                                                                         │
│ ▏•  Alice · 2h ago · #abc123456789                                      │
│ ▏Hello world!                                                           │
│ ▏↩ 2  ↻ 1                                                               │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Bob · 5h ago · #def456789abc                                        │
│  Great idea! I especially like the part about...                        │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↻  Alice · 1d ago · #bcd234567890                                      │
│  ┊ Bob · Great idea!                                                    │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Repository

The title carries the follow state: workspace, followed, mutual or unfollowed.

```
╭─ ⎇  ✓ user/repo · 1/5 ─────────────────────────────────────────────────╮
│                                                                         │
│ ▏•  Alice · 2h ago · #abc123456789                                      │
│ ▏Hello world!                                                           │
│ ▏↩ 2                                                                    │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  •  Alice · 1d ago · #def456789abc                                      │
│  Add dark mode support                                                  │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### List repositories

A text list with an input field for local lists.

```
╭─ ☷  My List ───────────────────────────────────────────────────────────╮
│                                                                         │
│  + |                                                                    │
│    url [branch | *]                                                     │
│                                                                         │
│  ▸ user/repo            ✓ followed  · https://github.com/user/repo      │
│    bob/repo             ✓ mutual    · https://github.com/bob/repo       │
│    alice/repo                       · https://gitlab.com/alice/repo     │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### List posts

Posts from every repository in a list.

```
╭─ ☷  My List ───────────────────────────────────────────────────────────╮
│                                                                         │
│ ▏•  Alice · 2h ago · user/repo#abc123456789                             │
│ ▏Hello world!                                                           │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  •  Bob · 5h ago · bob/repo#def456789abc                                │
│  Great idea!                                                            │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Issues

MaxLines 1. `n` opens the issue form.

```
╭─ ○  Open Issues · (3) ─────────────────────────────────────────────────╮
│                                                                        │
│ ▏○  Add dark mode support · Alice · 2h ago · kind/feature              │
│  ─────────────────────────────────────────────────────────────         │
│  ○  Add dashboard analytics · Bob · 1d ago · kind/feature              │
│  ─────────────────────────────────────────────────────────────         │
│  ●  Add keyboard shortcuts · Alice · 3d ago · kind/task                │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯
```

### Milestones

```
╭─ ◇  Open Milestones · (2) ─────────────────────────────────────────────╮
│                                                                        │
│ ▏◇  Release v2.0 · Alice · due Mar 15 · ████████░░░░  3/5              │
│  ─────────────────────────────────────────────────────────────         │
│  ◇  Design System Epic · Bob · due Feb 15 · ░░░░░░░░░░░░  0/4          │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯
```

### Sprints

```
╭─ ◷  Active Sprints · (2) ──────────────────────────────────────────────╮
│                                                                        │
│ ▏◷  Sprint 23: UX Polish · Alice · Feb 1-14 · ████░░░░  2/5 · 8d       │
│  ─────────────────────────────────────────────────────────────         │
│  ◷  Sprint 24 · Bob · Feb 14-28 · ░░░░░░░░░░░░  0/3 · planned          │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯
```

### Releases

MaxLines 2.

```
╭─ ⏏  Releases (2) ──────────────────────────────────────────────────────╮
│                                                                        │
│ ▏⏏  Release v1.0.0 · Alice · 2d ago                                    │
│ ▏   Pre-built binaries for Linux, macOS, and Windows.                  │
│                                                                        │
│  ─────────────────────────────────────────────────────────────         │
│                                                                        │
│  ⏏  Release v2.0.0-beta.1 · Alice · 2w ago                             │
│     Implements dark mode support.                                      │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯
```

### Pull requests

MaxLines 2.

```
╭─ ⑂  Pull Requests (2) ─────────────────────────────────────────────────╮
│                                                                        │
│ ▏⑂  Add dark mode support · Alice · 3h ago · open                      │
│ ▏   main ← dark-mode · ✓1 ✗0 · +120 -45                                │
│                                                                        │
│  ─────────────────────────────────────────────────────────────         │
│                                                                        │
│  ⑂  Add keyboard shortcuts · Bob · 1d ago · open                       │
│     main ← feature/shortcuts · +42 -8                                  │
│                                                                        │
╰────────────────────────────────────────────────────────────────────────╯
```

### Search

An input at the top, results as cards below, matches highlighted.

```
╭─ Search ────────────────────────────────────────────────────────────────╮
│                                                                         │
│  > dark mode█                                                           │
│                                                                         │
│  •  Alice · 2h ago · #abc123456789                                      │
│  Add [dark] [mode] support                                              │
│  ↩ 2                                                                    │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ○  Add [dark] [mode] support · Alice · 3d ago · kind/feature           │
│  Users can toggle between light and [dark] themes...                    │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Notifications

Read items are dimmed.

```
╭─ Notifications ─────────────────────────────────────────────────────────╮
│                                                                         │
│ ▏•  Bob mentioned you · 1h ago                                          │
│ ▏Hey @alice@example.com, thoughts on this approach?                     │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Alice commented · 3h ago                        (dimmed = read)     │
│  Love this idea, I'll start on it.                                      │
│                                                                         │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ⎇  Bob started following · 1d ago                  (dimmed = read)    │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Detail views

### Post detail

A thread: the parent dimmed above, the post itself, then replies indented by depth.

```
╭─ •  Alice <alice@example.com> · Jan 6, 2025 10:30 UTC · #abc1234 ───────╮
│                                                                         │
│  [parent - dimmed, max 5 lines]                                         │
│  Alice · 2h ago                                                         │
│  Original post content...                                               │
│                                                                         │
│  ────────────────────────────────────────────────────────  (white)      │
│                                                                         │
│ ▏Alice · 2h ago · #abc123456789                                         │
│ ▏Hello world!                                                           │
│ ▏↩ 2  ↻ 1                                                               │
│                                                                         │
│  ────────────────────────────────────────────────────────  (white)      │
│                                                                         │
│  ↩  Bob · 1h ago · #def456789abc                                        │
│  Great idea!                                                            │
│                                                                         │
│  ─────────────────────────────────────────────────────────  (dim)       │
│                                                                         │
│      ↩  Alice · 30m ago                                                 │
│      I agree!                                                           │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Issue detail

```
╭─ ○  Add dark mode support · Alice · 2h ago · #abc123456789 ─────────────╮
│                                                                         │
│ ▏Add dark mode support                                                  │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏State         open                                                     │
│ ▏Assignees     alice@example.com                                        │
│ ▏Due           Feb 15, 2025                                             │
│ ▏Milestone     Release v2.0  due Mar 15                                 │
│ ▏Sprint        Sprint 23: UX Polish  Feb 1 - Feb 14                     │
│ ▏Labels        kind/feature, priority/high                              │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Users can toggle between light and dark themes in settings.            │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (2)                                                           │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Bob · 1h ago                                                        │
│  Love this idea, I'll start on it.                                      │
│                                                                         │
│  ─────────────────────────────────────────────────────────────  (dim)   │
│                                                                         │
│  ↩  Alice · 30m ago                                                     │
│  Adding real-time collaboration to the scope.                           │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Milestone detail

```
╭─ ◇  Release v2.0 · Alice · 5d ago · #def456789012 ──────────────────────╮
│                                                                         │
│ ▏Release v2.0                                                           │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏State         open                                                     │
│ ▏Due           Mar 15, 2025                                             │
│ ▏Progress      ████████░░░░░░░░  3/5 (60%)                              │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Dark mode and dashboard analytics.                                     │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Linked Issues (3)                                                      │
│  ─────────────────────────────────────────────────────────────          │
│ ▏○  Add dark mode support  kind/feature                                 │
│  ○  Add dashboard analytics  kind/feature                               │
│  ●  Add keyboard shortcuts  kind/task                                   │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (1)                                                           │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Bob · 3d ago                                                        │
│  Adding real-time collaboration to the scope, extending due date.       │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Sprint detail

```
╭─ ◷  Sprint 23: UX Polish · Alice · 5d ago · #abc123456789 ──────────────╮
│                                                                         │
│ ▏Sprint 23: UX Polish                                                   │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏State         active                                                   │
│ ▏Progress      ████░░░░░░░░░░░░  2/5 (40%)                              │
│ ▏Days left     8                                                        │
│ ▏Dates         Feb 1 - Feb 14, 2025                                     │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Two-week sprint for user experience improvements.                      │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Sprint Backlog (3)                                                     │
│  ─────────────────────────────────────────────────────────────          │
│  ○  Add dark mode support  kind/feature                                 │
│  ●  Add keyboard shortcuts  kind/task                                   │
│  ○  Add dashboard analytics  kind/feature                               │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (1)                                                           │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Alice · 2d ago                                                      │
│  Retrospective: Good velocity this sprint.                              │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Release detail

`s` opens the SBOM view.

```
╭─ ⏏  Release v1.0.0 · Alice · 2d ago · #abc123456789 ────────────────────╮
│                                                                         │
│ ▏Release v1.0.0                                                         │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Version       1.0.0                                                    │
│ ▏Tag           v1.0.0                                                   │
│ ▏Artifacts     app-linux-x64.tar.gz, app-darwin-arm64.tar.gz            │
│ ▏Artifact URL  refs/gitmsg/release/v1.0.0/artifacts/                    │
│ ▏Checksums     sha256:abc123def456...                                   │
│ ▏Signed by     SHA256:abc123...                                         │
│ ▏SBOM          sbom.spdx.json (SPDX) · 127 packages  [s]                │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Pre-built binaries for Linux, macOS, and Windows.                      │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (1)                                                           │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Bob · 1d ago                                                        │
│  Implements dark mode support.                                          │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Release SBOM

```
╭─ ⏏  SBOM · Release v1.0.0 ────────────────────────────────────────────╮
│                                                                         │
│ ▏Format        SPDX-2.3                                                │
│ ▏Packages      127                                                      │
│ ▏Generator     syft-1.0.0                                               │
│ ▏Licenses      MIT (42)                                                 │
│ ▏               Apache-2.0 (15)                                         │
│ ▏               BSD-3-Clause (8)                                        │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Packages (127)                                                         │
│  ─────────────────────────────────────────────────────────────          │
│ ▏github.com/pkg/errors          v0.9.1        MIT                       │
│  golang.org/x/sys               v0.15.0       BSD-3-Clause              │
│  github.com/mattn/go-sqlite3    v1.14.22      MIT                       │
│  ...                                                                    │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

### Pull request detail

`d` opens the diff, `i` the interdiff between versions.

```
╭─ ⑂  Add dark mode support · Alice · 3h ago · #abc123456789 ─────────────╮
│                                                                         │
│ ▏Add dark mode support                                                  │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏State         open                                                     │
│ ▏Base          main                                                     │
│ ▏Head          dark-mode                                                │
│ ▏Behind        3 commits behind main                                    │
│ ▏Reviewers     bob@example.com                                          │
│ ▏Closes        #commit:def456789012                                     │
│ ▏Files         3 changed  +120 -45  [d]                                 │
│ ▏Reviews       1 approved, 0 changes req, 0 pending                     │
│ ▏Status        Ready to merge                                           │
│ ▏─────────────────────────────────────────────────────────────          │
│                                                                         │
│  Commits (2)                                                            │
│  ─────────────────────────────────────────────────────────────          │
│  abc1234  Dark mode theme engine  Alice · 3h ago                        │
│  def4567  Add theme toggle component  Alice · 2h ago                    │
│                                                                         │
│ ▏─────────────────────────────────────────────────────────────          │
│ ▏Users can toggle between light and dark themes in settings.            │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Reviews (2)                                                            │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ✓  Bob · approved · 1h ago · reviewed v2, no code changes              │
│     LGTM!                                                               │
│                                                                         │
│  ─────────────────────────────────────────────────────────────  (dim)   │
│                                                                         │
│  ✗  Carol · changes requested · 2d ago · reviewed v1 [stale]            │
│     The transition timing needs work                                    │
│                                                                         │
│  ─────────────────────────────────────────────────────────────  (dim)   │
│                                                                         │
│  ↩  Bob · 2h ago · src/theme.js:42                                      │
│     Consider caching this value                                         │
│       40 function getTheme() {                                          │
│       41   const stored = localStorage.getItem('theme');                │
│     > 42   return stored || detectSystemTheme();                        │
│       43 }                                                              │
│                                                                         │
│ ═══════════════════════════════════════════════════════════             │
│                                                                         │
│  Comments (1)                                                           │
│  ─────────────────────────────────────────────────────────────          │
│                                                                         │
│  ↩  Alice · 30m ago                                                     │
│  Clean separation of theme variables.                                   │
│                                                                         │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Routes

Every view has a route; the diagrams above cover the list and detail shapes, and the rest share them.

| Section | Routes |
|---|---|
| Social | `/social/timeline`, `/social/my-repository`, `/social/repository`, `/social/repository/lists`, `/social/list`, `/social/list/repos`, `/social/detail`, `/social/thread`, `/social/post-form`, `/social/history`, `/social/history/diff`, `/social/explore`, `/social/followers` |
| PM | `/pm/board`, `/pm/issues`, `/pm/issue`, `/pm/new-issue`, `/pm/edit-issue`, `/pm/milestones`, `/pm/milestone`, `/pm/new-milestone`, `/pm/edit-milestone`, `/pm/sprints`, `/pm/sprint`, `/pm/new-sprint`, `/pm/edit-sprint`, `/pm/config`, and `/pm/<item>/history` with `/history/diff` for each item type |
| Review | `/review/prs`, `/review/pr`, `/review/new-pr`, `/review/edit-pr`, `/review/feedback`, `/review/diff`, `/review/pr/interdiff`, `/review/pr/history`, `/review/pr/history/diff` |
| Release | `/release/list`, `/release/detail`, `/release/new`, `/release/edit`, `/release/sbom`, `/release/history`, `/release/history/diff`, `/export-artifact` |
| Memo | `/memo/list`, `/memo/project`, `/memo/personal`, `/memo/inherited`, `/memo/inherits`, `/memo/session`, `/memo/session/items`, `/memo/detail`, `/memo/new`, `/memo/edit`, `/memo/history`, `/memo/history/diff` |
| Core | `/search`, `/search/help`, `/notifications`, `/lists`, `/analytics`, `/diff`, `/settings`, `/config`, `/config/forks`, `/config/identity`, `/config/site`, `/cache`, `/errorlog`, `/help` |
