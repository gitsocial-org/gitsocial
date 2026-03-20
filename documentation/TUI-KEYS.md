# TUI Keybindings

## Shared Navigation

### CardList (used by: Timeline, Notifications, Repository, My Repository, List Posts, Issues, Milestones, Sprints, Releases, Pull Requests)

| Key | Action |
|-----|--------|
| `j / down` | Move down |
| `k / up` | Move up |
| `g / home` | Jump to top |
| `G / end` | Jump to bottom |
| `ctrl+d / pgdown` | Half-page down |
| `ctrl+u / pgup` | Half-page up |
| `enter` | Open selected |
| `tab` | Next link |
| `shift+tab` | Previous link |

### SectionList (used by: Issue Detail, Milestone Detail, Sprint Detail, Release Detail, PR Detail)

| Key | Action |
|-----|--------|
| `j / down` | Move down |
| `k / up` | Move up |
| `g / home` | Jump to top |
| `G / end` | Jump to bottom |
| `ctrl+d / pgdown` | Half-page down |
| `ctrl+u / pgup` | Half-page up |
| `enter` | Activate selected item or link |
| `tab` | Next link |
| `shift+tab` | Previous link |
| `/` | Start inline search |

### VersionPicker (used by: History, Issue History, Milestone History, Sprint History, PR History)

| Key | Action |
|-----|--------|
| `j / down` | Move down |
| `k / up` | Move up |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `home` | Jump to top |
| `end` | Jump to bottom |
| `ctrl+d / pgdown` | Half-page down |
| `ctrl+u / pgup` | Half-page up |
| `enter` | Open detail |
| `esc` | Back (or exit detail) |
| `left` | Previous version |
| `right` | Next version |

---

## Global Keys

### Core Keys

| Key | Action | Scope |
|-----|--------|-------|
| `esc` | Go back | Everywhere except Timeline |
| `@` | Notifications | Everywhere except Notifications |
| `%` | Analytics | Everywhere except Analytics |
| `f` | Fetch updates | Everywhere except Detail/Thread/History |
| `/` | Search | Everywhere except Search |
| `` ` `` | Toggle nav/content focus | Global |
| `q` | Quit | Global |
| `?` | Help | Global |

### Extension Keys (uppercase, shown in sidebar)

| Key | Extension | Target View | Status |
|-----|-----------|-------------|--------|
| `T` | Social | Timeline | Active |
| `B` | PM | Board | Active |
| `P` | Review | Pull Requests | Active |
| `R` | Release | Releases | Active |
| `C` | CI/CD | Actions | Planned |
| `I` | Infrastructure | Infrastructure | Planned |
| `O` | Operations | Operations | Planned |
| `S` | Security | Security | Planned |
| `>` | DM | Dm | Planned |
| `F` | Portfolio | Overview | Planned |

---

## Social Extension

### Timeline

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `m` | My repo |
| `n` | New post |
| `p` | Push |
| `l` | Lists |
| `r` | Refresh |

### Search

| Key | Action |
|-----|--------|
| `enter` | Search/open |
| `esc` | Exit input |
| `down` | To results |
| `up` | To input |
| `/` | Search |

### Notifications

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `m` | Read |
| `M` | Read all |
| `u` | Unread |
| `U` | Unread all |
| `r` | Refresh |
| `F` | Filter |

### Post Detail

| Key | Action |
|-----|--------|
| `c` | Comment |
| `y` | Repost |
| `e` | Edit |
| `X` | Retract |
| `h` | History |
| `v` | Raw (full commit message) |
| `r` | Repository |
| `/` | Search |
| `n` | Next match |
| `N` | Prev match |
| `left` | Prev |
| `right` | Next |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | Top |
| `end` | Bottom |
| `enter` | Activate |
| `p` | Push |

### Thread

| Key | Action |
|-----|--------|
| `c` | Comment |
| `y` | Repost |
| `e` | Edit |
| `X` | Retract |
| `h` | History |
| `v` | Raw (full commit message) |
| `r` | Repository |
| `/` | Search |
| `n` | Next match |
| `N` | Prev match |
| `left` | Prev |
| `right` | Next |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | Top |
| `end` | Bottom |
| `enter` | Activate |
| `p` | Push |

### Repository

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `l` | Lists |
| `a` | Add to my lists |
| `[` | Older |
| `]` | Newer |
| `r` | Refresh |

### My Repository

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `n` | New post |
| `p` | Push |
| `l` | Lists |
| `r` | Refresh |

### List Picker

| Key | Action |
|-----|--------|
| `n` | New list |
| `D` | Delete list |
| `m` | My repo |
| `a` | Create |
| `enter` | Open/add |
| `j` | Down |
| `k` | Up |
| `p` | Push |
| `/` | Search |

### List Posts

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `m` | More |
| `r` | Repositories |

### List Repos

| Key | Action |
|-----|--------|
| `a` | Add repository (`url [branch \| *]`) |
| `x` | Remove repository |
| `j` | Down |
| `k` | Up |
| `enter` | Open repo |
| `p` | Push |
| `/` | Search |

### Repository Lists

| Key | Action |
|-----|--------|
| `enter` | View posts |
| `r` | Repositories |
| `j` | Down |
| `k` | Up |
| `p` | Push |
| `/` | Search |

### History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `/` | Search |

---

## PM Extension

### Board

| Key | Action |
|-----|--------|
| `n` | Quick create |
| `N` | Full create |
| `F` | Forks |
| `x` | Collapse col |
| `s` | Swimlanes |
| `r` | Refresh |
| `up` | Up |
| `down` | Down |
| `left` | Prev col |
| `right` | Next col |
| `home` | First |
| `end` | Last |
| `enter` | Open issue |
| `p` | Push |
| `/` | Search |

### Issues

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `n` | Quick create |
| `N` | New |
| `f` | Filter |
| `F` | Forks |
| `m` | My issues |
| `r` | Refresh |
| `p` | Push |

### Issue Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `m` | Milestone |
| `s` | Sprint |
| `h` | History |
| `v` | Raw (full commit message) |
| `/` | Search |
| `C` | Close |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |

### Issue History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `/` | Search |

### Milestones

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `F` | Filter |
| `m` | Mine |
| `r` | Refresh |
| `p` | Push |

### Milestone Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `h` | History |
| `v` | Raw (full commit message) |
| `/` | Search |
| `C` | Close |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |

### Milestone History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `/` | Search |

### Sprints

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `F` | Filter |
| `m` | Mine |
| `r` | Refresh |
| `p` | Push |

### Sprint Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `h` | History |
| `v` | Raw (full commit message) |
| `/` | Search |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |

### Sprint History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `/` | Search |

### PM Config

| Key | Action |
|-----|--------|
| `p` | Push |
| `/` | Search |

---

## Review Extension

### Pull Requests

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `m` | Mine |
| `r` | Refresh |
| `F` | Forks |
| `p` | Push |

### PR Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `d` | Diff |
| `i` | Interdiff (range-diff between versions) |
| `r` | Review |
| `c` | Comment |
| `M` | Merge (strategy picker) |
| `C` | Close |
| `S` | Sync branch (rebase/merge) |
| `e` | Edit |
| `h` | History |
| `v` | Raw (full commit message) |
| `X` | Retract |
| `A` | Apply suggestion |
| `/` | Search |
| `left` | Prev |
| `right` | Next |
| `p` | Push |

### PR History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |

### Interdiff

| Key | Action |
|-----|--------|
| `j / down` | Scroll down |
| `k / up` | Scroll up |
| `g / home` | Jump to top |
| `G / end` | Jump to bottom |
| `[` | Previous version pair |
| `]` | Next version pair |

### Files Changed

| Key | Action |
|-----|--------|
| `c` | Comment |
| `enter` | Expand/collapse |
| `v` | View mode (unified → split → fullscreen → unified) |
| `tab` | Next file |
| `[/]` | Prev/next hunk |
| `n/N` | Next/prev comment |
| `f` | Fold hunk |
| `/` | Search |
| `e` | Expand context |
| `E` | Expand/collapse all files |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `esc` | Exit fullscreen / back |
| `p` | Push |

---

## Release Extension

### Releases

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `r` | Refresh |
| `p` | Push |
| `L` | Push LFS |

### Release Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `s` | SBOM |
| `e` | Edit |
| `c` | Comment |
| `v` | Raw (full commit message) |
| `/` | Search |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |
| `L` | Push LFS |

### Release SBOM

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `/` | Search |

---

## Core Views

### Commit Diff

| Key | Action |
|-----|--------|
| `enter` | Expand/collapse |
| `v` | View mode (unified → split → fullscreen → unified) |
| `tab` | Next file |
| `[/]` | Prev/next hunk |
| `f` | Fold hunk |
| `E` | Expand/collapse all files |
| `/` | Search |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `esc` | Exit fullscreen / back |

### Analytics

| Key | Action |
|-----|--------|
| `r` | Refresh |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | Top |
| `end` | Bottom |
| `/` | Search |

### Settings

| Key | Action |
|-----|--------|
| `e` | Edit |
| `enter` | Edit/cycle |
| `j` | Down |
| `k` | Up |
| `home` | First |
| `end` | Last |
| `/` | Search |

### Config

| Key | Action |
|-----|--------|
| `e` | Edit |
| `a` | Add |
| `D` | Delete key |
| `j` | Down |
| `k` | Up |
| `home` | First |
| `end` | Last |
| `p` | Push |
| `/` | Search |

### Forks

| Key | Action |
|-----|--------|
| `a` | Add fork |
| `x` | Remove fork |
| `v` | Sort (name / fetched / commits) |
| `/` | Search (live filter) |
| `enter` | Open repo |
| `j/k` | Up / down |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | First |
| `end` | Last |

### Cache

| Key | Action |
|-----|--------|
| `x` | Delete selected |
| `C` | Clear all |
| `D` | Clear db |
| `X` | Clear repos |
| `F` | Clear forks |
| `r` | Refresh |
| `/` | Search |

### Help

| Key | Action |
|-----|--------|
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | Top |
| `end` | Bottom |
| `/` | Search |

---

## Mouse Support

All views support mouse wheel scrolling and click-to-select/activate. CardList and SectionList views provide full mouse support including link zone clicking via the AnchorCollector system. Simple list views (List Picker, List Repos, Repository Lists, PM Config, Settings, Config) support wheel scroll and click-to-select/activate via zone marking. Board view supports column header clicks and issue selection. Cache view supports cursor selection for per-item deletion.

## Confirmation Dialogs

Retract, delete, close, and remove actions show a `[y/n]` confirmation prompt:
- `y` / `Y` - Confirm action
- `n` / `N` / `esc` - Cancel

All confirmations use the shared `ConfirmDialog` component.

## Choice Dialogs

Merge and sync actions show a multi-choice prompt with labeled keys:
- Merge: `[f]ast-forward  [s]quash  [r]ebase  [m]erge  esc`
- Sync: `[r]ebase  [m]erge  esc`

Choice dialogs use the shared `ChoiceDialog` component.
