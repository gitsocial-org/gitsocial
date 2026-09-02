# TUI Test Suite

Headless integration tests that exercise every view and keybinding. Inspired by Ghostty's renderer test approach: seed a real git repo, drive the TUI programmatically, assert on rendered output.

## Table of Contents

- [Running](#running)
- [Architecture](#architecture)
- [Test Inventory](#test-inventory)
- [Harness Implementation Notes](#harness-implementation-notes)
- [What This Catches](#what-this-catches)

---

## Running

```bash
go test ./library/tui/test/...                       # all TUI tests
go test ./library/tui/test/ -run Smoke               # smoke only
go test ./library/tui/test/ -run Display             # display only
go test ./library/tui/test/ -run Golden              # golden file comparison
go test ./library/tui/test/ -run Golden -update      # regenerate golden files
go test ./library/tui/test/ -run Navigation          # navigation only
go test ./library/tui/test/ -run Sequence            # sequence only
go test -v ./library/tui/test/ -run PR               # verbose, PR-related
```

The suite is slow and silent per package until it finishes. For streamed per-test progress (one line per test as it completes, plus a final summary), wrap any of the above with `scripts/test.sh`, which passes its args straight through to `go test -json`:

```bash
scripts/test.sh ./library/tui/test/...               # all TUI tests, streamed
scripts/test.sh -run Smoke ./library/tui/test/       # smoke only, streamed
```

Tests create temp dirs, no external dependencies. Total runtime ~3 minutes (181s measured standalone on a warm build; longer inside the full `go test ./...` gate, where packages compete for cores). The smoke test's all-keys × all-views matrix dominates.

---

## Architecture

```
library/tui/test/
├── harness.go          # Headless model driver
├── fixture.go          # Test repo setup + data seeding
├── generate_test.go    # Fixture tarball generation (gated by -generate flag)
├── assert.go           # Render assertion helpers (ANSI stripping, pattern matching)
├── main_test.go        # Shared fixture via TestMain
├── smoke_test.go       # Key smoke tests (all keys × all views)
├── display_test.go     # Content display tests
├── golden_test.go      # Visual regression tests
├── navigation_test.go  # View-to-view navigation tests
├── sequence_test.go    # Multi-step interaction tests
├── cursor_test.go      # List-cursor stability across fetch and back-nav
├── history_diff_test.go # History-diff footer registration + render
├── stack_test.go       # Stacked-PR display, bindings, and navigation
├── proposal_test.go    # Cross-repo proposal display + accept/decline flow
└── testdata/           # Generated artifacts (committed)
    ├── fixture-repo.tar.gz  # Pre-built workspace repo with seeded data
    ├── fixture-fork.tar.gz  # Pre-built fork repo (the cross-repo proposal)
    ├── fixture.json         # Fixture metadata (entity IDs)
    └── *.golden             # Golden files (generated with -update flag)
```

### Harness

Wraps `tui.NewModel` without a real terminal. Sends `tea.KeyMsg` and `tea.WindowSizeMsg` directly, collects rendered output via `Rendered()`.

```go
type Harness struct {
    model        tui.Model
    t            *testing.T
    workdir      string
    cache        string
    width        int
    height       int
    SkippedExecN int // execMsg commands skipped (editor spawns, etc.)
}

func New(t *testing.T, workdir, cacheDir string) *Harness

// Drive
func (h *Harness) SendKey(key string)
func (h *Harness) SendKeys(keys ...string)
func (h *Harness) Navigate(path string)
func (h *Harness) NavigateTo(loc tuicore.Location)
func (h *Harness) SetSize(w, h int)
func (h *Harness) DrainCmds()

// Inspect
func (h *Harness) Rendered() string
func (h *Harness) CurrentPath() string
func (h *Harness) CurrentContext() tuicore.Context
func (h *Harness) CurrentView() tuicore.View
func (h *Harness) BindingsForContext(ctx tuicore.Context) []tuicore.Binding
```

`New()` initializes the bubblezone global manager, creates the model, sends `WindowSizeMsg{120, 40}` to make it ready, then runs `Init()` and drains all startup commands. Each `SendKey()` and `Navigate()` call automatically drains resulting commands.

### Fixture

Two pre-built git repos stored as tarballs, with entity IDs in `testdata/fixture.json`. `testdata/fixture-repo.tar.gz` is the workspace (origin `https://github.com/user/repo`); `testdata/fixture-fork.tar.gz` is a second repo (origin `https://github.com/bob/repo`) that carries one cross-repo edit of the workspace's issue. Both are extracted once per test run via `TestMain` and shared read-only across tests. Fixture data uses examples from the protocol specs (Alice, dark mode, etc.).

```go
func SetupFixture(t *testing.T) *Fixture     // per-test isolation (extracts fresh copies)
func getFixture(t *testing.T) *Fixture        // shared read-only fixture
```

The cache handle is process-global and `cache.Open` is a no-op while one is already open, so `SetupFixture` closes the shared cache first and reopens it on cleanup. Tests that mutate the repo (accept a proposal, close an issue) must use `SetupFixture`; everything else uses `getFixture`.

To regenerate the fixture after changing seed data:

```bash
go test ./library/tui/test/ -run TestGenerateFixture -generate
go test ./library/tui/test/ -run Golden -update      # regenerate golden files too
```

Generation points `HOME`, `XDG_CONFIG_HOME` and `GITSOCIAL_PERSONAL_REPO` at a throwaway directory, so it can never reach the real personal memo repo or the real config.

Seeds via extension APIs (not raw git):
- **Social**: 2 posts, 1 comment, 1 repost, 1 quote, 1 edit
- **PM**: 3 issues (open/closed/canceled), 1 milestone, 1 sprint
- **Release**: 2 releases (stable + prerelease with artifacts)
- **Review**: 2 PRs (open with feedback + merged), 1 approval
- **Memo**: project tier initialized, 2 project memos (one edited, one labeled), 1 inherited source
- **Forks**: 1 registered fork (`refs/gitmsg/core/forks/<urlHash>`)
- **Proposal**: the fork closes the workspace issue, leaving an inert cross-repo proposal

Only the project memo tier is seeded: the personal and session tiers live outside the repo (`~/.config/gitsocial/personal`, the cache dir) so they cannot travel in a tarball.

Cache populated via `SyncWorkspaceToCache()` for each extension, same as the real app. The workspace is synced first and the fork second, because a cross-repo edit only resolves once the canonical it edits is cached. Commit timestamps are then rewritten one second apart, ending now, so relative time always renders as "just now" while version and list ordering stays deterministic.

### Assert Helpers

```go
func stripANSI(s string) string                          // remove ANSI escape codes
func rendered(h *Harness) string                          // strip ANSI from rendered
func assertContains(t, output, substr)                    // substring in stripped output
func assertRendersItem(t, h, loc, want...)                // navigate, then require seeded content
func assertNotEmpty(t, output)                            // non-empty after stripping
func assertLineCount(t, output, maxLines)                 // output fits height
```

`assertNotEmpty` passes on a view that draws nothing but its own chrome and an empty-state message, so it is only appropriate for views with no seeded data. Prefer `assertRendersItem`: it navigates, then requires every named fragment of seeded content, and fails on an empty expectation so an unset fixture field cannot make the assertion vacuous.

---

## Test Inventory

### 1. Smoke Tests — every key on every view

**File**: `smoke_test.go`

| Test | Description |
|------|-------------|
| `TestSmoke/AllKeysAllViews` | Iterates `AllViewMetas()` × `Registry.ForContext(ctx)`. Sends every registered key on every view — no panic = pass |
| `TestSmoke/UnregisteredKeysIgnored` | Sends unbound keys (z, x, 1, !, #, etc.) — verifies graceful ignore |

The smoke test produces hundreds of subtests (one per key × view combination).

### 2. Display Tests — verify actual content rendering

**File**: `display_test.go`

| Test | Verifies |
|------|----------|
| `TestDisplay/Timeline` | Author name, "Timeline" title |
| `TestDisplay/Search` | Non-empty search view |
| `TestDisplay/MyRepository` | Project memos appear in the workspace feed |
| `TestDisplay/Board` | "Board" title |
| `TestDisplay/IssuesList` | Issue subject from fixture |
| `TestDisplay/Milestones` | Milestone title from fixture |
| `TestDisplay/Sprints` | Sprint title from fixture |
| `TestDisplay/PRList` | PR subject from fixture |
| `TestDisplay/ReleasesList` | Release subject from fixture |
| `TestDisplay/Notifications` | The fork's cross-repo edit notification |
| `TestDisplay/Memos` | Memo subject, body, label and tier badge |
| `TestDisplay/ProjectMemos` | Both project-tier memos |
| `TestDisplay/MemoDetail` | Memo subject, body and label |
| `TestDisplay/MemoHistory` | Edited memo's two versions |
| `TestDisplay/MemoInherits` | Inherited source URL |
| `TestDisplay/Forks` | Fork count and the registered fork URL |
| `TestDisplay/Settings` | "Settings" text |
| `TestDisplay/Cache` | "Cache" text |
| `TestDisplay/Help` | "Help" text |

### 3. Golden File Tests — visual regression

**File**: `golden_test.go`

| Test | View | Size |
|------|------|------|
| `TestGolden/timeline_120x40` | `/social/timeline` | 120×40 |
| `TestGolden/board_120x40` | `/pm/board` | 120×40 |
| `TestGolden/issues_120x40` | `/pm/issues` | 120×40 |
| `TestGolden/pr_list_120x40` | `/review/prs` | 120×40 |
| `TestGolden/releases_120x40` | `/release/list` | 120×40 |
| `TestGolden/settings_120x40` | `/settings` | 120×40 |
| `TestGolden/help_120x40` | `/help` | 120×40 |
| `TestGolden/LayoutProperties` | All views | 120×40, 80×24, 200×60 |

Golden files are ANSI-stripped and compared line-by-line. Update with:

```bash
go test ./library/tui/test/ -run Golden -update
```

Layout property checks verify every view at 3 terminal sizes: the output fits the terminal height and every view renders. They do **not** check width: nothing in the suite fails on a line wider than the terminal.

### 4. Navigation Tests — view transitions

**File**: `navigation_test.go`

| Test | Flow |
|------|------|
| `TestNavigation/GlobalKeys` | T→timeline, B→board, P→PRs, R→releases (from settings) |
| `TestNavigation/Back` | timeline → settings → esc → timeline |
| `TestNavigation/MultiLevelBack` | timeline → settings → cache → esc → esc |
| `TestNavigation/Detail` | issues → enter → esc |
| `TestNavigation/Search` | timeline → / → search |
| `TestNavigation/Help` | timeline → ? → help |
| `TestNavigation/Notifications` | timeline → @ → notifications |

### 5. Sequence Tests — multi-step interactions

**File**: `sequence_test.go`

| Test | Flow |
|------|------|
| `TestSequence/AllExtensions` | T → B → P → R → T cycle |
| `TestSequence/BrowseAndReturn` | timeline → enter → esc → same timeline |
| `TestSequence/IssuesFlow` | B → issues → enter → esc |
| `TestSequence/SettingsAndBack` | settings → cache → esc → settings |
| `TestSequence/QuickJumpOverridesHistory` | deep nav → R → releases |
| `TestSequence/PostEditTriggersEditor` | post detail → e → editor spawned |
| `TestSequence/PostCommentTriggersEditor` | post detail → c → editor spawned |
| `TestSequence/PostRepostTriggersEditor` | post detail → y → editor spawned |
| `TestSequence/PostRetractShowsConfirm` | post detail → X → [y/n] confirm |
| `TestSequence/PostHistoryNavigates` | post detail → h → /social/history |
| `TestSequence/SearchFlow` | /search → type query → enter → results |
| `TestSequence/PRDiffNavigates` | PR detail → d → /review/diff |
| `TestSequence/IssueEditNavigates` | issue detail → e → /pm/edit-issue |
| `TestSequence/IssueCommentTriggersEditor` | issue detail → c → editor spawned |
| `TestSequence/MilestoneEditNavigates` | milestone detail → e → /pm/edit-milestone |
| `TestSequence/MilestoneCommentTriggersEditor` | milestone detail → c → editor spawned |
| `TestSequence/SprintEditNavigates` | sprint detail → e → /pm/edit-sprint |
| `TestSequence/SprintCommentTriggersEditor` | sprint detail → c → editor spawned |
| `TestSequence/ReleaseEditNavigates` | release detail → e → /release/edit |
| `TestSequence/ReleaseCommentTriggersEditor` | release detail → c → editor spawned |
| `TestSequence/PREditNavigates` | PR detail → e → /review/edit-pr |
| `TestSequence/PRCommentTriggersEditor` | PR detail → c → editor spawned |
| `TestSequence/MultipleViewRenders` | Visits 10 views sequentially, verifies each renders |

### 6. Cursor Tests — list selection stability

**File**: `cursor_test.go`

| Test | Verifies |
|------|----------|
| `TestTimelineCursorSurvivesFetch` | A completed fetch does not move the timeline cursor |
| `TestTimelineCursorSurvivesBackNav` | Opening an item and pressing esc returns to the same selection |

### 7. History-Diff Tests — footer registration

**File**: `history_diff_test.go`

| Test | Verifies |
|------|----------|
| `TestHistoryDiffFooter` | Each history-diff context registers the expected footer entries, without duplicates or conflicts with the global key set |
| `TestHistoryDiffFooter/PostHistoryDiffRenders` | The post history-diff view renders |

### 8. Stack Tests — stacked pull requests

**File**: `stack_test.go`

Seeds a child PR on top of the fixture's PR (`DependsOn`), once per package run.

| Test | Verifies |
|------|----------|
| `TestStackDisplay/BadgeOnPRList` | Stack badge appears on the PR list |
| `TestStackBindings` | Stack keys are registered on the PR contexts |
| `TestStackNavigationBackend` | `review.GetStack` / `GetDependents` back the navigation |

### 9. Proposal Tests: cross-repo proposals

**File**: `proposal_test.go`

The fork repo's edit of the workspace issue is inert until the owner acts, so it renders as an open proposal everywhere the owner might act on it.

| Test | Verifies |
|------|----------|
| `TestProposalDisplay/IssueListMarker` | The ✎ proposed-edit marker on the issue card |
| `TestProposalDisplay/IssueDetailBanner` | The "Proposed edits from another repo" banner |
| `TestProposalDisplay/HistoryRow` | The `✎ proposal · bob/repo` tag on the fork's version |
| `TestProposalDisplay/HistoryFooterOffersAcceptAndDecline` | `A`/`X` appear only when an open proposal is present |
| `TestProposalAccept` | `A` applies the proposal: issue closes, marker clears |
| `TestProposalDecline` | `X` declines it: issue stays open, marker clears |

Both action tests mutate the repo, so they take an isolated fixture via `SetupFixture`.

---

## Harness Implementation Notes

### Command Draining

Bubbletea views return `tea.Cmd` (async functions) from `Update` and `Activate`. The harness executes them synchronously with a 50-depth recursion limit:

```go
func (h *Harness) processCmds(cmd tea.Cmd, depth int) {
    if cmd == nil || depth > 50 {
        return
    }
    msg := execCmd(cmd)   // skips known blockers, runs rest synchronously
    if msg == nil {
        return
    }
    if batch, ok := msg.(tea.BatchMsg); ok {
        for _, c := range batch {
            h.processCmds(c, depth+1)
        }
        return
    }
    if shouldSkipMsg(msg) {
        return
    }
    m, next := h.model.Update(msg)
    h.model = toModel(m)
    h.processCmds(next, depth+1)
}
```

### Blocking Command Skip List

Some Bubbletea commands block indefinitely (cursor blink via `time.After`, `tea.SetWindowTitle` writing to nil program channel). The harness identifies these by function name via `runtime.FuncForPC` and skips them before execution. The model runs in headless mode (`SetHeadless(true)`) which also skips terminal-dependent commands in `Init()`.

### Model Type Assertion

`tui.Model.Update()` returns `tea.Model` which may be `tui.Model` (value) or `*tui.Model` (pointer) depending on the code path (`handleKey` uses pointer receiver). The `toModel()` helper handles both.

### Skip Lists

**Commands skipped before execution** (identified by function name):

| Function pattern | Reason |
|-----------------|--------|
| `BlinkCmd` | Cursor blink blocks on `time.After` indefinitely |
| `startFetch` | Network fetch blocks on remote I/O |

**Messages skipped after execution**:

| Message type | Reason |
|--------------|--------|
| `tea.QuitMsg` | Would exit the test |
| `setWindowTitleMsg` | Terminal-only, unexported |
| `execMsg` | Editor/process launch |
| `cursor.BlinkMsg` | Cursor blink result |

### Key Simulation

Maps string key names to `tea.KeyMsg`:

| Key string | Maps to |
|-----------|---------|
| `enter`, `esc`, `tab`, `shift+tab` | Corresponding `tea.Key*` type |
| `up`, `down`, `left`, `right` | Arrow keys |
| `ctrl+c`, `ctrl+d`, `ctrl+u` | Control sequences |
| `space`, `backspace`, `home`, `end`, `pgup`, `pgdown` | Special keys |
| Any other string | `tea.KeyRunes` with the string as runes |

---

## What This Catches

- Nil pointer panics when entering views with empty/missing data
- Render crashes from bad state combinations
- Keys that silently break after refactors (binding registered but handler references stale field)
- Missing `Activate()` calls that leave views empty
- Navigation dead-ends (can't get back from a view)
- Content regressions (post format changes, missing fields in cards)
- Extension registration order bugs
- Context mismatches (key bound to wrong context)
- Misaligned borders, broken box-drawing characters (golden files)
- Content overflowing the terminal height (layout property checks)
- Responsive layout regressions across terminal sizes (multi-size layout checks)

Not caught: horizontal overflow. `assertLineCount` bounds the number of lines only, so a line wider than the terminal passes every check except a golden diff.
