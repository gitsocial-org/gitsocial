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
| `;` | Next link |
| `,` | Previous link |

### SectionList (used by: Issue Detail, Milestone Detail, Sprint Detail, Release Detail, SBOM, PR Detail)

| Key | Action |
|-----|--------|
| `j / down` | Move down |
| `k / up` | Move up |
| `g / home` | Jump to top |
| `G / end` | Jump to bottom |
| `ctrl+d / pgdown` | Half-page down |
| `ctrl+u / pgup` | Half-page up |
| `enter` | Activate selected item or link |
| `;` | Next link |
| `,` | Previous link |
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
| `esc / b` | Back (or exit detail) |
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
| `!` | Errors | Everywhere except Error Log |
| `f` | Fetch updates | Everywhere except Detail/Thread/History |
| `/` | Search | Everywhere except Search |
| `tab` | Toggle nav/content focus | Global |
| `q` | Quit | Global |
| `?` | Help | Global |

### Extension Keys (uppercase, shown in sidebar)

| Key | Extension | Target View | Status |
|-----|-----------|-------------|--------|
| `S` | Social | Timeline | Active |
| `P` | PM | Board | Active |
| `R` | Review | Pull Requests | Active |
| `V` | Release | Releases | Active |
| `C` | CI/CD | Actions | Planned |
| `I` | Infrastructure | Infrastructure | Planned |
| `O` | Operations | Operations | Planned |
| `Y` | Security | Security | Planned |
| `|` | DM | Dm | Planned |
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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Search

| Key | Action |
|-----|--------|
| `enter` | Search/open |
| `esc` | Exit input |
| `down` | To results |
| `up` | To input |
| `/` | Search |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Search Help

| Key | Action |
|-----|--------|
| `j` | Scroll down |
| `k` | Scroll up |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Post Detail

| Key | Action |
|-----|--------|
| `c` | Comment |
| `y` | Repost |
| `e` | Edit |
| `X` | Retract |
| `h` | History |
| `d` | Diff |
| `v` | Raw |
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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Thread

| Key | Action |
|-----|--------|
| `c` | Comment |
| `y` | Repost |
| `e` | Edit |
| `X` | Retract |
| `h` | History |
| `d` | Diff |
| `v` | Raw |
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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Repository

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `l` | Lists |
| `a` | Add to my lists |
| `[` | Older |
| `]` | Newer |
| `r` | Refresh |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### My Repository

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `n` | New post |
| `p` | Push |
| `l` | Lists |
| `r` | Refresh |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### List Posts

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `m` | More |
| `r` | Repositories |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### List Repos

| Key | Action |
|-----|--------|
| `a` | Add repository |
| `x` | Remove repository |
| `j` | Down |
| `k` | Up |
| `enter` | Open repo |
| `p` | Push |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Repository Lists

| Key | Action |
|-----|--------|
| `enter` | View posts |
| `r` | Repositories |
| `j` | Down |
| `k` | Up |
| `p` | Push |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `d` | Version diff |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Post Diff

| Key | Action |
|-----|--------|
| `[/]` | Shift pair |
| `,/.` | From anchor |
| `</>` | To anchor |
| `e/E` | Expand |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Issue Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `m` | Milestone |
| `s` | Sprint |
| `h` | History |
| `v` | Raw |
| `/` | Search |
| `C` | Close |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Issue History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `d` | Version diff |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Issue Diff

| Key | Action |
|-----|--------|
| `[/]` | Shift pair |
| `,/.` | From anchor |
| `</>` | To anchor |
| `e/E` | Expand |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Milestones

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `F` | Filter |
| `m` | Mine |
| `r` | Refresh |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Milestone Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `h` | History |
| `v` | Raw |
| `/` | Search |
| `C` | Close |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Milestone History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `d` | Version diff |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Milestone Diff

| Key | Action |
|-----|--------|
| `[/]` | Shift pair |
| `,/.` | From anchor |
| `</>` | To anchor |
| `e/E` | Expand |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Sprints

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `F` | Filter |
| `m` | Mine |
| `r` | Refresh |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Sprint Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `c` | Comment |
| `e` | Edit |
| `h` | History |
| `v` | Raw |
| `/` | Search |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Sprint History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `d` | Version diff |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Sprint Diff

| Key | Action |
|-----|--------|
| `[/]` | Shift pair |
| `,/.` | From anchor |
| `</>` | To anchor |
| `e/E` | Expand |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### PM Config

| Key | Action |
|-----|--------|
| `p` | Push |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### PR Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `d` | Diff |
| `r` | Review |
| `c` | Comment |
| `M` | Merge |
| `C` | Close |
| `D` | Draft |
| `e` | Edit |
| `u` | Update tips |
| `h` | History |
| `i` | Interdiff |
| `v` | Raw |
| `X` | Retract |
| `A` | Apply suggestion |
| `/` | Search |
| `left` | Prev |
| `right` | Next |
| `[` | Stack prev |
| `]` | Stack next |
| `p` | Push |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### PR History

| Key | Action |
|-----|--------|
| VersionPicker navigation | (see Shared Navigation) |
| `d` | Version diff |
| `i` | Interdiff |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### PR Diff

| Key | Action |
|-----|--------|
| `[/]` | Shift pair |
| `,/.` | From anchor |
| `</>` | To anchor |
| `e/E` | Expand |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Files Changed

| Key | Action |
|-----|--------|
| `c` | Comment |
| `enter` | Expand/collapse |
| `v` | View mode |
| `tab` | Next file |
| `[/]` | Prev/next hunk |
| `n/N` | Next/prev comment |
| `f` | Fold hunk |
| `/` | Search |
| `e` | Expand context |
| `E` | Expand/collapse all |
| `w` | Wrap |
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `esc` | Exit mode |
| `p` | Push |
| `!` | Errors |
| `shift+tab` | Focus |

### Interdiff

| Key | Action |
|-----|--------|
| `[` | Prev version |
| `]` | Next version |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

---

## Release Extension

### Releases

| Key | Action |
|-----|--------|
| CardList navigation | (see Shared Navigation) |
| `N` | New |
| `r` | Refresh |
| `p` | Push |
| `L` | Push lfs |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### Release Detail

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `s` | Sbom |
| `e` | Edit |
| `c` | Comment |
| `v` | Raw |
| `/` | Search |
| `X` | Retract |
| `left` | Prev |
| `right` | Next |
| `p` | Push |
| `L` | Push lfs |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

### SBOM

| Key | Action |
|-----|--------|
| SectionList navigation | (see Shared Navigation) |
| `/` | Search |
| `!` | Errors |
| `tab` | Focus |
| `shift+tab` | Focus |

---

## Core Views

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
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Settings

| Key | Action |
|-----|--------|
| `e` | Edit |
| `enter` | Edit/cycle |
| `j` | Down |
| `k` | Up |
| `home` | First |
| `end` | Last |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

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
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Cache

| Key | Action |
|-----|--------|
| `x` | Delete selected |
| `C` | Clear all |
| `D` | Clear db |
| `X` | Clear repos |
| `F` | Clear forks |
| `r` | Refresh |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Help

| Key | Action |
|-----|--------|
| `j` | Scroll down |
| `k` | Scroll up |
| `ctrl+d` | Half-page down |
| `ctrl+u` | Half-page up |
| `home` | Top |
| `end` | Bottom |
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Error Log

| Key | Action |
|-----|--------|
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Commit Diff

| Key | Action |
|-----|--------|
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Forks

| Key | Action |
|-----|--------|
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

### Identity

| Key | Action |
|-----|--------|
| `!` | Errors |
| `/` | Search |
| `tab` | Focus |
| `shift+tab` | Focus |

---

## Mouse Support

All views support mouse wheel scrolling and click-to-select/activate. CardList and SectionList views provide full mouse support including link zone clicking via the AnchorCollector system. Simple list views (List Picker, List Repos, Repository Lists, PM Config, Settings, Config) support wheel scroll and click-to-select/activate via zone marking. Board view supports column header clicks and issue selection. Cache view is action-based with no cursor, so mouse is not applicable.

## Confirmation Dialogs

Retract, delete, merge, close, and remove actions show a `[y/n]` confirmation prompt:
- `y` / `Y` - Confirm action
- `n` / `N` / `esc` - Cancel

All confirmations use the shared `ConfirmDialog` component.
