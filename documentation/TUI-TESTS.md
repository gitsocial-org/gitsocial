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

The full tier runs in about 206 s standalone; `TestSmoke` (90 s) and `TestGolden/LayoutProperties` (59 s) are most of it. The quick tier runs in about 40 s. Tests create temporary directories and need nothing external.

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

Every `SendKey` and `Navigate` drains the commands it produces. Key names: `enter`, `esc`, `tab`, `shift+tab`, the arrows, `ctrl+c`, `ctrl+d`, `ctrl+u`, `space`, `backspace`, `home`, `end`, `pgup`, `pgdown`; any other string is sent as runes.

Assertions: `stripANSI`, `rendered(h)`, `assertContains(t, output, substr)`, `assertRendersItem(t, h, loc, want...)`, `assertNotEmpty`, `assertLineCount(t, output, maxLines)`. Prefer `assertRendersItem`: it navigates, requires every named fragment of seeded content, and fails on an empty expectation. `assertNotEmpty` passes on chrome alone, so it fits only views with no seeded data.

## Fixture

Two repositories, extracted once per run by `TestMain` and shared read-only: `testdata/fixture-repo.tar.gz`, the workspace with origin `https://github.com/user/repo`, and `testdata/fixture-fork.tar.gz`, a fork with origin `https://github.com/bob/repo` that carries one cross-repo edit of the workspace's issue. Entity ids are in `testdata/fixture.json`.

```go
func getFixture(t *testing.T) *Fixture      // the shared fixture, for read-only tests
func SetupFixture(t *testing.T) *Fixture    // a fresh copy, for tests that mutate the repository
```

`SetupFixture` closes the shared cache first and reopens it on cleanup, since `cache.Open` is a no-op while a cache is open.

Seeded through the extension APIs, with data from the protocol specs:

- Social: 2 posts, 1 comment, 1 repost, 1 quote, 1 edit
- PM: 3 issues (open, closed, canceled), 1 milestone, 1 sprint
- Release: 2 releases, one a prerelease with artifacts
- Review: 2 pull requests (one open with feedback, one merged), 1 approval; `stack_test.go` adds a dependent pull request once per run
- Memo: the project tier, 2 memos (one edited, one labeled), 1 inherited source; the personal and session tiers live outside the repository and are not seeded
- Forks: 1 registered fork; the fork closes the workspace issue, which leaves an inert proposal

The cache is filled with `SyncWorkspaceToCache` per extension, workspace first and fork second, since a cross-repo edit resolves only once its canonical is cached. Commit timestamps are rewritten one second apart, ending now, so relative times render as "just now" and ordering stays deterministic. Generation points `HOME`, `XDG_CONFIG_HOME` and `GITSOCIAL_PERSONAL_REPO` at a throwaway directory.

## Inventory

| File | Tests | Covers |
|---|---|---|
| `smoke_test.go` | `TestSmoke/AllKeysAllViews`, `UnregisteredKeysIgnored` | every registered key on every view (6,364 subtests), and unbound keys, without a panic; full tier only |
| `display_test.go` | `TestDisplay/*`: Timeline, Search, MyRepository, Board, IssuesList, Milestones, Sprints, PRList, ReleasesList, Notifications, Memos, ProjectMemos, MemoDetail, MemoHistory, MemoInherits, Forks, Settings, Site, Cache, Help | seeded content appears on each view |
| `golden_test.go` | `TestGolden/{timeline,board,issues,pr_list,releases,settings,help}_120x40`, `LayoutProperties` | ANSI-stripped renders against `testdata/*.golden`; every view fits the height at 120x40, 80x24 and 200x60 (full tier only) |
| `navigation_test.go` | `TestNavigation/GlobalKeys` (`S`, `P`, `R`, `V`, `M`), `Back`, `SiteEditToggle`, `MultiLevelBack`, `Detail`, `Search`, `Help`, `Notifications` | the global jump keys land on their routes; `esc`, `/`, `?` and `@` do what they say |
| `sequence_test.go` | `TestSequence/*`: AllExtensions, BrowseAndReturn, IssuesFlow, SettingsAndBack, QuickJumpOverridesHistory, the `*OpensForm` and `*Navigates` flows per item type, PostRetractShowsConfirm, SearchFlow, PRDiffNavigates, MultipleViewRenders, PushConfirmNamesRemote | multi-step flows; full tier only |
| `cursor_test.go` | `TestTimelineCursorSurvivesFetch`, `TestTimelineCursorSurvivesBackNav` | the timeline selection survives a fetch and a detail round trip |
| `history_diff_test.go` | `TestHistoryDiffFooter`, `PostHistoryDiffRenders` | every history-diff context registers its footer entries without duplicates, and the view renders |
| `stack_test.go` | `TestStackDisplay/BadgeOnPRList`, `TestStackBindings`, `TestStackNavigationBackend` | the stack badge, the stack keys, and `GetStack` and `GetDependents` behind them |
| `proposal_test.go` | `TestProposalDisplay/{IssueListMarker,IssueDetailBanner,HistoryRow,HistoryFooterOffersAcceptAndDecline}`, `TestProposalAccept`, `TestProposalDecline` | the ✎ marker, the banner, the history row, and `A` and `X` applying or declining a proposal on an isolated fixture |

## Notes

- Commands run synchronously to a depth of 50; `tea.BatchMsg` fans out. Commands are skipped by function name before execution when they would block (`BlinkCmd`, `startFetch`), and these messages are dropped after execution: `tea.QuitMsg`, `setWindowTitleMsg`, `execMsg` (editor and process launches, counted in `SkippedExecN`), `cursor.BlinkMsg`. `SetHeadless(true)` skips the terminal-dependent commands in `Init()`.
- `tui.Model.Update` returns either `tui.Model` or `*tui.Model`; `toModel` handles both.
- The suite catches panics on empty data, render crashes, keys that stop working after a refactor, missing `Activate` calls, navigation dead ends, content regressions, registration-order bugs, context mismatches, broken box drawing and height overflow.
- Not caught: horizontal overflow. `assertLineCount` bounds the line count only, so a line wider than the terminal passes everything but a golden diff.
