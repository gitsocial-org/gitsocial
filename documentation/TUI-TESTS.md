# TUI Test Suite

Headless integration tests drive the real TUI model over a seeded git repository and assert on the rendered output, with no terminal.

[Running](#running) · [Layout](#layout) · [Harness](#harness) · [Fixture](#fixture) · [Inventory](#inventory) · [Notes](#notes)

## Running

```bash
go test ./library/tui/test/                              # the quick tier
GITSOCIAL_TEST_FULL=1 go test ./library/tui/test/        # the full tier: adds TestSmoke, TestSequence, TestGolden/LayoutProperties
go test ./library/tui/test/ -run Golden -update          # regenerate the golden files
go test ./library/tui/test/ -run TestGenerateFixture -generate   # regenerate the fixture tarballs
scripts/test.sh -run Smoke ./library/tui/test/           # any of the above with streamed per-test progress
```

The tests create temporary directories and need no external services. Run times with a warm build cache:

| Run | Time |
|---|---|
| quick tier | 13 s |
| full tier | 78 s |
| `TestSmoke` | 40 s |
| `TestGolden/LayoutProperties` | 20 s |

## Layout

```
library/tui/test/
├── harness.go            # the headless model driver
├── fixture.go            # repository setup and data seeding
├── assert.go             # render assertions: ANSI stripping, pattern matching
├── main_test.go          # the shared fixture, via TestMain
├── generate_test.go      # fixture tarball generation, behind -generate
├── smoke_test.go         # every key on every view
├── display_test.go       # seeded content renders
├── golden_test.go        # golden files and layout properties
├── navigation_test.go    # view transitions
├── sequence_test.go      # multi-step interactions
├── form_test.go          # form submits
├── diff_test.go          # diff view keys
├── cursor_test.go        # list cursor stability
├── history_diff_test.go  # history-diff footers
├── stack_test.go         # stacked pull requests
├── proposal_test.go      # cross-repo proposals
└── testdata/             # fixture-repo.tar.gz, fixture-fork.tar.gz, fixture.json, *.golden
```

## Harness

`Harness` wraps `tui.NewModel`, sends `tea.KeyMsg` and `tea.WindowSizeMsg` directly, and reads the rendered output.

```go
func New(t *testing.T, workdir, cacheDir string) *Harness    // 120x40, Init() run, startup commands drained

func (h *Harness) SendKey(key string)
func (h *Harness) SendKeys(keys ...string)
func (h *Harness) Navigate(path string)
func (h *Harness) NavigateTo(loc tuicore.Location)
func (h *Harness) SetSize(w, h int)
func (h *Harness) DrainCmds()

func (h *Harness) Rendered() string
func (h *Harness) CurrentPath() string
func (h *Harness) CurrentContext() tuicore.Context
func (h *Harness) CurrentView() tuicore.View
func (h *Harness) BindingsForContext(ctx tuicore.Context) []tuicore.Binding
```

Every `SendKey` and `Navigate` drains the commands it produces. Key names: `enter`, `esc`, `tab`, `shift+tab`, the arrows, `ctrl+c`, `ctrl+d`, `ctrl+u`, `space`, `backspace`, `home`, `end`, `pgup`, `pgdown`; every other string is sent as runes.

Assertions: `stripANSI`, `rendered(h)`, `assertContains(t, output, substr)`, `assertRendersItem(t, h, loc, want...)`, `assertNotEmpty`, `assertMaxWidth(t, output, maxCols)`, `assertFitsTerminal(t, h)`, `assertFrameFits(t, frame, cols, lines)`.

Prefer `assertRendersItem`. It navigates, requires every named fragment of seeded content, and fails on an empty expectation. `assertNotEmpty` passes when a view shows only its frame (borders, title and footer), so use it only for a view with no seeded data.

## Fixture

`TestMain` extracts two repositories once per run, and the tests share them read-only:

- `testdata/fixture-repo.tar.gz`: the workspace, with origin `https://github.com/user/repo`
- `testdata/fixture-fork.tar.gz`: a fork, with origin `https://github.com/bob/repo`, that has one cross-repo edit of the workspace issue

Entity ids are in `testdata/fixture.json`.

```go
func getFixture(t *testing.T) *Fixture      // the shared fixture, for read-only tests
func SetupFixture(t *testing.T) *Fixture    // a fresh copy, for tests that mutate the repository
```

`SetupFixture` closes the shared cache first and opens it again on cleanup; `cache.Open` is a no-op while a cache is open.

The extension APIs seed this data, taken from the protocol specs:

- Social: 2 posts, 1 comment, 1 repost, 1 quote, 1 edit
- PM: 3 issues (open, closed, canceled), 1 milestone, 1 sprint
- Release: 2 releases, one a prerelease with artifacts
- Review: 2 pull requests (one open with feedback, one merged), 1 approval; `stack_test.go` adds a dependent pull request to its own copy
- Memo: the project tier, 2 memos (one edited, one labeled), 1 inherited source
- Forks: 1 registered fork; its cross-repo edit closes the workspace issue and stays a proposal

The cache is filled with `client.SyncWorkspace`, workspace first and fork second. A cross-repo edit resolves only after its canonical version is in the cache. Commit timestamps are rewritten one second apart, with the last one `fixtureCommitAge` before the run, so each row shows the same relative time for the full run and the order is deterministic. Fixture generation sets `HOME`, `XDG_CONFIG_HOME` and `GITSOCIAL_PERSONAL_REPO` to a temporary directory.

`seedMemoTiersPanic` builds the memo tiers that are outside the workspace repository: the personal bare repository, the session `tui-fixture`, and the repository behind the inherits ref. Each has one memo, written through the memo API. `TestMain` sets `MEMO_SESSION_ID` to that session, and the repositories are under the isolated `HOME`. They stay after a fixture ends, so setup keeps an existing repository and only re-indexes it. Their commit dates are `fixtureCommitAge` before the run. The session picker reads the session age from the repository, not from the cache.

## Inventory

| File | Tests | Covers |
|---|---|---|
| `smoke_test.go` | `TestSmoke/AllKeysAllViews`, `GlobalShortcuts`, `UnregisteredKeysIgnored` | each view with the keys of its context and one key that nothing binds, then each global shortcut once, on the first view that binds it (86 subtests), with no panic; full tier only |
| `display_test.go` | `TestDisplay/*`: Timeline, Search, MyRepository, Board, IssuesList, Milestones, Sprints, PRList, ReleasesList, Notifications, Memos, ProjectMemos, MemoDetail, MemoHistory, MemoInherits, Forks, Settings, Site, Cache, Help | seeded content appears on each view |
| `golden_test.go` | `TestGolden/{timeline,board,issues,pr_list,releases,settings,help,memo_personal,memo_session,memo_session_items,memo_inherited,memo_inherits}_120x40` | ANSI-stripped renders against `testdata/*.golden`, each frame also 120 columns wide and 40 lines; the memo goldens have one memo per tier and the inherited source |
| `golden_test.go` | `TestGolden/LayoutProperties` | each route fits the width and the height at 120x40, 80x24 and 200x60; full tier only |
| `golden_test.go` | `TestGoldenWidth_rejectsNavOneColumnTooWide` | a navigation row one column too wide fails the check |
| `golden_test.go` | `TestGoldenTime_labelHoldsForALongRun` | the fixture keeps one relative-time label for the full run |
| `navigation_test.go` | `TestNavigation/GlobalKeys` (`S`, `P`, `R`, `V`, `M`), `Back`, `SiteEditToggle`, `MultiLevelBack`, `Detail`, `Search`, `Help`, `Notifications` | the global jump keys open their routes; `esc` goes back, and `/`, `?` and `@` open search, help and notifications |
| `sequence_test.go` | `TestSequence/*`: AllExtensions, BrowseAndReturn, IssuesFlow, SettingsAndBack, QuickJumpOverridesHistory, the `*OpensForm` and `*Navigates` flows per item type, PostRetractShowsConfirm, SearchFlow, PRDiffNavigates, MultipleViewRenders, PushConfirmNamesRemote | multi-step flows; full tier only |
| `form_test.go` | `TestMilestoneFormCreates`, `TestSprintFormCreates` | a submitted create form opens the detail view of the new item, on an isolated fixture |
| `diff_test.go` | `TestDiffTabMovesToNextFile` | `tab` in a diff moves the cursor to the next file and leaves the panel focus alone, on an isolated fixture with a two-file commit |
| `cursor_test.go` | `TestTimelineCursorSurvivesFetch`, `TestTimelineCursorSurvivesBackNav` | the timeline selection stays after a fetch and after a return from a detail view |
| `history_diff_test.go` | `TestHistoryDiffFooter`, `PostHistoryDiffRenders` | every history-diff context registers its footer entries without duplicates, and the view renders |
| `stack_test.go` | `TestStackDisplay/BadgeOnPRList`, `TestStackBindings`, `TestStackNavigationBackend` | the stack badge, the stack keys, and `GetStack` and `getDependents` behind them |
| `proposal_test.go` | `TestProposalDisplay/{IssueListMarker,IssueDetailBanner,HistoryRow,HistoryFooterOffersAcceptAndDecline}`, `TestProposalAccept`, `TestProposalDecline` | the ✎ marker, the banner, the history row, and `A` and `X` that accept or decline a proposal, on an isolated fixture |

## Notes

- Commands run synchronously to a depth of 50, and the harness runs each command in a `tea.BatchMsg`. A command that would block is skipped by function name before execution: `BlinkCmd`, `startFetch`, `tea.Tick` (message timeouts, the auto-fetch heartbeat). These messages are dropped after execution: `tea.QuitMsg`, `setWindowTitleMsg`, `execMsg` (editor and process launches, counted in `SkippedExecN`), `cursor.BlinkMsg`. `SetHeadless(true)` skips the terminal-dependent commands in `Init()`.
- `tui.Model.Update` returns either `tui.Model` or `*tui.Model`; `toModel` handles both.
- The suite finds these problems:
  - panics on empty data and render crashes
  - keys that stop working after a refactor, and context mismatches
  - missing `Activate` calls, navigation dead ends and registration-order bugs
  - content regressions, broken box drawing and height overflow
- `assertFrameFits` finds horizontal overflow on each route in `TestGolden/LayoutProperties`, at 120x40, 80x24 and 200x60, and `assertFitsTerminal` finds it on each `TestDisplay` view. Both measure printable cells with `tuicore.AnsiWidth`, so a border past the terminal edge fails on its own line, not as a golden diff.
- The suite does not find these problems: a terminal under 80 columns, a view that a key opens and no route opens (a form, a diff, a confirmation), and content that the frame cut to fit. A clipped line fits the frame, so no test reports the content that it lost.
