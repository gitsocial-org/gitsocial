# Testing

The two test tiers, every gate stage, and the flags, environment variables and artifacts the scripts in `scripts/` use.

[Tiers](#tiers) · [Stages](#stages) · [Guarded tests](#guarded-tests) · [Coverage](#coverage) · [Beyond the gate](#beyond-the-gate) · [Environment](#environment) · [Artifacts](#artifacts)

## Tiers

`scripts/check.sh` is the gate, in two tiers. `--quick` runs every test except the guarded ones, and the pre-push hook runs it on every push. Without `--quick` it sets `GITSOCIAL_TEST_FULL=1`, so the guarded tests run too, writes the coverage profile and checks the floors. Run the full tier before merging to `main` and at release.

| Tier | Command | Runs |
|---|---|---|
| quick, the push tier | `scripts/check.sh --quick` | stages 0 to 4, every test except the guarded ones |
| full | `scripts/check.sh` | the same stages with the guarded tests, then the coverage floors |

```bash
scripts/check.sh --quick                    # the push tier
scripts/check.sh                            # the full tier
scripts/check.sh --skip-lint                # allow a missing golangci-lint; not a gate run
scripts/check.sh -short ./...               # extra args go to the test stage; a smoke run, not a gate run
git config core.hooksPath scripts/hooks     # install the pre-push hook, once per clone
GITSOCIAL_SKIP_GATE=1 git push              # skip the gate once
```

A missing `golangci-lint` fails unless `--skip-lint` is passed. `-short` skips the real-git subtests. Every script under `scripts/` prints its usage for `-h`.

## Stages

The full tier runs six stages, the quick tier the first five.

| Stage | Command | Fails on |
|---|---|---|
| 0 | `scripts/prose-check.sh` | a [STYLE.md](STYLE.md) rule count above `scripts/prose-baseline.txt`, or a commit subject over 72 characters |
| 1 | `scripts/import-graph.sh --check` | an upward import edge missing from `scripts/import-baseline.txt`, a package over 15,000 non-test lines, or a `core` package importing one at or above its tier |
| 2 | `go vet -tags bench ./...` | a vet finding |
| 3 | `golangci-lint run --build-tags bench ./...` | a lint finding |
| 4 | `scripts/test.sh ./...` | a failing test; the full tier adds `-coverpkg=./...` and writes the profile |
| 5 | `scripts/coverage.sh --check` | a package more than 2.0 points under `scripts/coverage-baseline.txt`; the full tier only |

Stage 1 reads the core stack sentence from [ARCHITECTURE.md](ARCHITECTURE.md#layers), and fails when that sentence names a `core` package that does not exist or misses one that does.

Each baseline takes `--update`, which accepts the current numbers and is for a count that fell. `funlen` and `gocognit` in `.golangci.yml` hold today's largest function and highest complexity, and a threshold only moves down.

```bash
scripts/prose-check.sh --list <rule>        # the offending lines of one rule
scripts/prose-check.sh --update             # accept the lowered counts
scripts/import-graph.sh                     # the per-package size, fan-in, fan-out and churn report
scripts/import-graph.sh --update            # accept the current upward edges
```

The report marks an edge against the core stack with `!`.

## Guarded tests

A guarded test calls `fullTierOnly` and runs only under `GITSOCIAL_TEST_FULL=1`.

| Test | Package |
|---|---|
| `TestSmoke`, `TestSequence`, `TestGolden/LayoutProperties` | `library/tui/test`, the matrices in [TUI-TESTS.md](TUI-TESTS.md) |
| `TestCommandTreeJSONOutput` | `cli/gitsocial`, the `--json` walk |
| `TestS3Helper_*` | `cli/gitsocial`, the child-process helper battery |
| `TestSiteFixtureBuild` | `library/core/site`, the showcase fixture build |

The fixture build needs neither node nor Chrome. It reruns when the site assets, the site generators, `sitetest/fixture.sh` or `sitetest/fixture-lib.sh` change.

## Coverage

`scripts/coverage.sh` measures statement-weighted coverage under `-coverpkg=./...`, so a package with no test files of its own still counts and another package's integration tests credit the code they reach. The full tier writes the profile the floors read; a bare run writes the whole report.

The CLI tests build their binary with `go build -cover` when `GITSOCIAL_COVERDIR` names a directory, and every child process inherits `GOCOVERDIR`. The push verbs, the `TestS3Helper_*` battery and the thin-bucket tests then credit what their children run, down to the s3 remote helper git spawns. `scripts/coverage.sh --merge` converts that data with `go tool covdata textfmt` and appends it to the profile, before the table and the floors are computed from it. The converted `children.out` is kept: a run whose tests the test cache replays reuses it, and a block the profile does not carry is dropped.

```bash
scripts/coverage.sh                            # run the suite and write the report
scripts/coverage.sh <outdir>                   # the same, written somewhere else
scripts/coverage.sh --check [profile]          # the gate's floors
scripts/coverage.sh --update [profile]         # accept the current per-package coverage as the floor
scripts/coverage.sh --merge [profile]          # fold the child-process data into a profile
GS_COVER_PROFILE=old.out scripts/coverage.sh   # re-report an existing profile, running no tests
```

A bare run sets `GITSOCIAL_TEST_FULL=1` itself, so its figures carry the guarded tests. The browser suites stay uncredited: they are JavaScript, so `site_pages*.go` reads as untested and is not.

## Beyond the gate

| Run | Command |
|---|---|
| the race detector and the browser battery, at release | `scripts/release.sh vX.Y.Z` |
| the browser site battery | `scripts/site-test.sh`, or `go test -tags sitetest -timeout 30m ./library/core/site/` |
| the 100k-row thread benchmark | `go test -tags bench -run '^$' -bench . ./library/extensions/social/` |
| one package with streamed per-test progress | `scripts/test.sh -run TestSmoke ./library/tui/test/` |

`scripts/release.sh` preflights `GITSOCIAL_TEST_FULL=1 go test -race ./...` and the browser battery, each into its own log. The battery needs node, and its style suite needs Chrome; the harness and the fixtures are in [STATIC-SITE.md](STATIC-SITE.md#testing). `scripts/test.sh` pipes `go test -json` through `scripts/testfmt` and defaults to `./...`.

## Environment

| Variable | Set by | Effect |
|---|---|---|
| `GITSOCIAL_TEST_FULL` | the full tier, `scripts/coverage.sh`, `scripts/release.sh` | runs the guarded tests |
| `GITSOCIAL_COVERDIR` | the full tier, `scripts/coverage.sh` | the directory the child processes write coverage data into, and the switch for the `-cover` build |
| `GITSOCIAL_PUSH_RANGES` | `scripts/hooks/pre-push` | the commit range the subject and `comment-heavy` checks read; else `@{upstream}..HEAD`, else neither runs |
| `GITSOCIAL_SKIP_GATE` | the user | skips the pre-push gate once |
| `GS_COVER_PROFILE` | the user | re-reports that profile instead of running the suite |
| `DAYS` | the user | the churn window of the import report, in days; 90 by default |
| `CHROME` | the user | the Chrome binary the style suite drives |

## Artifacts

Every run writes under `.test-artifacts/`, which git ignores.

| Path | Holds |
|---|---|
| `coverage/coverage.out` | the profile the full tier writes and the floors read |
| `coverage/coverage.html` | `go tool cover -html`, for line-level browsing |
| `coverage/summary.txt` | the statement-weighted total, the ranked package table and the blind spots |
| `coverage/functions.txt` | per-function coverage, from `go tool cover -func` |
| `coverage/zero-functions.txt` | every function at 0.0% |
| `coverage/children/` | the child processes' raw coverage data, and the `children.out` converted from it |
| `coverage/packages.txt`, `coverage/agg.txt` | the package list and the per-package totals the report is built from |
| `coverage/test.log` | the suite output of a bare `scripts/coverage.sh` run |
| `release-race.log` | `go test -race ./...` at release |
| `release-site.log` | the browser battery at release |
