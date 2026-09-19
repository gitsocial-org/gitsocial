# Testing

The two test tiers, every gate stage, the supported platforms, and the flags, environment variables and artifacts the scripts in `scripts/` use.

[Tiers](#tiers) · [Stages](#stages) · [Guarded tests](#guarded-tests) · [Coverage](#coverage) · [Beyond the gate](#beyond-the-gate) · [Platforms](#platforms) · [Environment](#environment) · [Artifacts](#artifacts)

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

A missing `golangci-lint` fails unless `--skip-lint` is passed. `.golangci.yml` is `version: "2"`, which a v1 binary refuses, so install v2 or newer:

```bash
brew install golangci-lint
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

`-short` skips the real-git subtests. Every script under `scripts/` prints its usage for `-h`.

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

`scripts/coverage.sh` measures statement-weighted coverage under `-coverpkg=./...`, so a package with no test files of its own still counts and another package's integration tests credit the code they reach. The full tier writes the profile the floors read; a bare run writes the report in full.

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
| the race detector, the browser battery and the advisory scan, at release | `scripts/release.sh vX.Y.Z` |
| the advisory scan alone; needs the network and downloads the module | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| the browser site battery | `scripts/site-test.sh`, or `go test -tags sitetest -timeout 30m ./library/core/site/` |
| the reader assets' per-file function coverage | `scripts/site-coverage.sh` |
| the 100k-row thread benchmark | `go test -tags bench -run '^$' -bench . ./library/extensions/social/` |
| one package with streamed per-test progress | `scripts/test.sh -run TestSmoke ./library/tui/test/` |

`scripts/site-coverage.sh` runs the battery under `NODE_V8_COVERAGE` and prints one row per asset in `library/core/site/assets/`: covered functions, total functions and the percentage. `--list <file>` prints the line and name of each uncovered function. `GS_JSCOV=<dir>` reports an existing profile directory and runs nothing; a run writes `.test-artifacts/jscov/`.

```bash
scripts/site-coverage.sh                                   # the per-file table
scripts/site-coverage.sh --list gs-render.js               # the uncovered functions of one asset
GS_JSCOV=.test-artifacts/jscov scripts/site-coverage.sh    # re-report the last run
```

`scripts/release.sh` preflights `GITSOCIAL_TEST_FULL=1 go test -race ./...`, the browser battery and `govulncheck ./...`, each into its own log. The scan is the one preflight step that reaches the network, so both tiers stay offline. The battery needs node, and its style suite needs Chrome; the harness and the fixtures are in [STATIC-SITE.md](STATIC-SITE.md#testing). `scripts/test.sh` pipes `go test -json` through `scripts/testfmt` and defaults to `./...`.

## Platforms

`.goreleaser.yaml` builds five targets, `scripts/install.sh` covers the four it can, and both tiers run on the maintainer's machine.

| Target | Released | `scripts/install.sh` | Tests run on it |
|---|---|---|---|
| darwin/arm64 | yes | yes | yes |
| darwin/amd64 | yes | yes | no |
| linux/amd64 | yes | yes | no |
| linux/arm64 | yes | yes | no |
| windows/amd64 | yes | no | no |

Windows installs through the Scoop bucket the [README](../README.md#installation) names. No source file carries a platform build tag, and `runtime.GOOS` is read in one place.

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
| `GS_JSCOV` | the user | the V8 coverage directory `scripts/site-coverage.sh` reports instead of running the battery |

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
| `jscov/` | the V8 coverage of one `scripts/site-coverage.sh` run, and the battery log beside it |
| `release-race.log` | `go test -race ./...` at release |
| `release-site.log` | the browser battery at release |
| `release-vuln.log` | the advisory scan at release |
