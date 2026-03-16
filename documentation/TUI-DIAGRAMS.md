# TUI View Diagrams

ASCII reference layouts for all TUI views. Examples use data from protocol specs.

## Table of Contents

- [TUI View Diagrams](#tui-view-diagrams)
  - [Table of Contents](#table-of-contents)
  - [Canonical Layout](#canonical-layout)
- [List Views](#list-views)
  - [Timeline](#timeline)
  - [Repository](#repository)
  - [List Repositories](#list-repositories)
  - [List Posts](#list-posts)
  - [Issues](#issues)
  - [Milestones](#milestones)
  - [Sprints](#sprints)
  - [Releases](#releases)
  - [Pull Requests](#pull-requests)
  - [Search](#search)
  - [Notifications](#notifications)
- [Detail Views](#detail-views)
  - [Post Detail](#post-detail)
  - [Issue Detail](#issue-detail)
  - [Milestone Detail](#milestone-detail)
  - [Sprint Detail](#sprint-detail)
  - [Release Detail](#release-detail)
  - [PR Detail](#pr-detail)

---

## Canonical Layout

All structured detail views follow this pattern:

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

---

# List Views

## Timeline

CardList. MaxLines: 5, ShowStats, Separator.

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
│ m:my repo  n:new post  p:push  o:notifs  l:lists  /:search              │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Repository

CardList. Title reflects follow status (workspace/followed/mutual/unfollowed).

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
│ l:lists  a:add  /:search  [:older  ]:newer  o:notifs  %:analytics       │
╰─────────────────────────────────────────────────────────────────────────╯
```

```
╭─ ♥  My Repository · 1/3 ────────────────────────────────────────────────╮
│  ...                                                                    │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## List Repositories

Text-based list (not CardList). Input field for local lists.

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
│ a:add  D:remove  enter:open                                             │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## List Posts

CardList. Posts aggregated from all repos in a list.

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
│ r:repositories  /:search                                                │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Issues

CardList. MaxLines: 1, Separator. Quick-create and search modes.

```
╭─ ○  Open Issues · (3) ─────────────────────────────────────────────────╮
│                                                                        │
│ ▏○  Add dark mode support · Alice · 2h ago · kind/feature              │
│  ─────────────────────────────────────────────────────────────         │
│  ○  Add dashboard analytics · Bob · 1d ago · kind/feature              │
│  ─────────────────────────────────────────────────────────────         │
│  ●  Add keyboard shortcuts · Alice · 3d ago · kind/task                │
│                                                                        │
│ n:quick  N:full  F:filter  m:mine  r:refresh  /:search                 │
╰────────────────────────────────────────────────────────────────────────╯
```

Quick-create:

```
╭─ ○  Open Issues · (3) ──────────────────────────────────────────────────╮
│                                                                         │
│  [cardlist]                                                             │
│                                                                         │
│  New issue: Implement real-time notifications█                          │
│                                                                         │
│ enter:create  esc:cancel                                                │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Milestones

CardList. MaxLines: 1, Separator.

```
╭─ ◇  Open Milestones · (2) ─────────────────────────────────────────────╮
│                                                                        │
│ ▏◇  Release v2.0 · Alice · due Mar 15 · ████████░░░░  3/5              │
│  ─────────────────────────────────────────────────────────────         │
│  ◇  Design System Epic · Bob · due Feb 15 · ░░░░░░░░░░░░  0/4          │
│                                                                        │
│ n:new  F:filter  r:refresh  /:search                                   │
╰────────────────────────────────────────────────────────────────────────╯
```

---

## Sprints

CardList. MaxLines: 1, Separator.

```
╭─ ◷  Active Sprints · (2) ──────────────────────────────────────────────╮
│                                                                        │
│ ▏◷  Sprint 23: UX Polish · Alice · Feb 1-14 · ████░░░░  2/5 · 8d       │
│  ─────────────────────────────────────────────────────────────         │
│  ◷  Sprint 24 · Bob · Feb 14-28 · ░░░░░░░░░░░░  0/3 · planned          │
│                                                                        │
│ n:new  F:filter  r:refresh  /:search                                   │
╰────────────────────────────────────────────────────────────────────────╯
```

---

## Releases

CardList. MaxLines: 2, Separator, no ShowStats.

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
│ N:create  c:comment                                                    │
╰────────────────────────────────────────────────────────────────────────╯
```

---

## Pull Requests

CardList. MaxLines: 2, Separator, no ShowStats.

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
│ N:create                                                               │
╰────────────────────────────────────────────────────────────────────────╯
```

---

## Search

Input at top, CardList results below.

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
│ /:edit  enter:open                                                      │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Notifications

CardList. Read items dimmed.

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
│ m:read  M:read-all  u:unread  U:unread-all  F:filter                    │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

# Detail Views

## Post Detail

Thread layout (social-specific, not canonical pattern).

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
│ c:comment  y:repost  e:edit  h:history  v:raw  r:repository  /:search   │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Issue Detail

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
│ c:comment  e:edit  m:milestone  s:sprint  h:history  /:search  X:retr   │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Milestone Detail

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
│ c:comment  e:edit  h:history  /:search  X:retract                       │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Sprint Detail

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
│ c:comment  e:edit  h:history  /:search  X:retract                       │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## Release Detail

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
│ s:sbom  e:edit  c:comment  /:search  X:retract                          │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Release SBOM

Navigated to via `s` from Release Detail. Shows full SBOM package list with search.

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
│ /:search                                                                │
╰─────────────────────────────────────────────────────────────────────────╯
```

---

## PR Detail

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
│ d:diff  i:interdiff  r:review  c:comment  M:merge  S:sync  e:edit  h    │
╰─────────────────────────────────────────────────────────────────────────╯
```
