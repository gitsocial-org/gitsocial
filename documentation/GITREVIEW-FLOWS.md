# GitReview Flows

Usage scenarios for the review extension. Pull requests live on the author's repository and are discovered by reviewers via fetch/follow.

## Table of Contents

- [Why GitSocial Reviews](#why-gitsocial-reviews)
- [Same-Repo Pull Request](#1-same-repo-pull-request)
- [Cross-Forge Contribution](#2-cross-forge-contribution)
- [Fork PR Discovery](#3-fork-pr-discovery)
- [Review with Suggestions](#4-review-with-suggestions)
- [Multi-Reviewer Approval](#5-multi-reviewer-approval)
- [Pull Request Linked to Issues](#6-pull-request-linked-to-issues)
- [Discussion Threads](#7-discussion-threads-on-a-pull-request)
- [Pull Request Lifecycle](#8-pull-request-lifecycle)
- [Viewing Diffs in TUI](#9-viewing-diffs-in-tui)
- [Version Tracking and Review Staleness](#10-version-tracking-and-review-staleness)
- [Merge Strategies](#11-merge-strategies)
- [Branch Sync](#12-branch-sync)

---

## Why GitSocial Reviews

GitSocial stores PRs as commits on a `gitmsg/review` branch inside the repo itself. The repo is the source of truth. Everything else follows from that.

**Cross-forge PRs.** `base` and `head` fields are URLs, not internal IDs. A GitLab contributor can submit a PR targeting a GitHub upstream. On merge, fork PRs are copied to upstream with the original author preserved — surviving fork deletion.

**Rebase-resilient identity.** The PR references `head="#branch:feature"` — the branch name doesn't change when its commits are rebased. State changes (edits, merge, close) are new commits linked via `edits`. Force-push can't destroy review context because review data lives on a separate branch.

**Version tracking.** Every `pr update` records the hash of the base and head branch tips. The edits chain is the version history — walking it gives every recorded code snapshot. `pr diff` runs `git range-diff` between any two versions. Feedback timestamps are compared against version timestamps to determine which version a reviewer saw — pure rebases (patches identical) are distinguished from actual code changes. Reviews are never auto-dismissed.

**Per-PR merge strategies.** Fast-forward, squash, rebase, and merge commit — chosen per-PR via `--strategy`, not a repo-wide setting.

### Comparison

| Pain Point | GitHub/GitLab | GitSocial |
|------------|--------------|-----------|
| Cross-forge PR | Impossible | Protocol-native via URLs |
| PR identity survives rebase | Tied to commit SHAs | PR commit on review branch = permanent anchor |
| History of code changes | Force-push = gone | `base-tip`/`head-tip` snapshots in edits chain |
| Review lost on rebase | Comments hidden/outdated | Edits chain preserves all versions; range-diff shows what changed |
| What changed between reviews? | GitLab has version diffs; GitHub has no built-in comparison | `pr diff` shows range-diff between any two versions |
| Review dismissal on rebase | Unpredictable auto-dismiss | Range-diff detects actual code changes vs. pure rebase; never auto-dismiss |
| Merge strategy | Repo-level config; choose from enabled options | All 4 strategies per-PR via `--strategy` |
| Update branch | Merge vs rebase dilemma | `pr sync` with rebase or merge, auto-captures tips |
| Fork PR, fork deleted | Review orphaned | PR copied to upstream on merge |

---

## Flows

## 1. Same-Repo Pull Request

Alice and Bob work on the same repo. Alice proposes a change, Bob reviews it.

```
    Alice                           Bob
      │                              │
      ●  push dark-mode branch       │
      ●  create PR                   │
      │  open, base=main             │
      │  head=dark-mode              │
      │                              │
      │                              ●  fetch gitmsg/review
      │                              ●  see open PR
      │                              ●  post inline review
      │                              │
      ●  read review, push fix       │
      │                              │
      │                              ●  approve
      │                              │
      ●  merge (state=merged)        │
      ●  closes linked issues        │
```

### Messages

**Alice creates pull request:**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; closes="#commit:abc123456789@gitmsg/pm"; head="#branch:dark-mode"; reviewers="bob@example.com"; v="0.1.0" ---
```

**Bob posts inline review:**
```
Consider caching this value to avoid recomputation on every render.

--- GitMsg: ext="review"; type="feedback"; pull-request="#commit:aaa111222333@gitmsg/review"; commit="def456789abc"; file="src/theme.js"; line-start="42"; side="right"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:aaa111222333@gitmsg/review"; v="0.1.0" ---
> Add dark mode support
```

**Bob approves:**
```
LGTM!

--- GitMsg: ext="review"; type="feedback"; pull-request="#commit:aaa111222333@gitmsg/review"; review-state="approved"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:aaa111222333@gitmsg/review"; v="0.1.0" ---
> Add dark mode support
```

**Alice merges (edits original pull request commit):**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; edits="#commit:aaa111222333@gitmsg/review"; state="merged"; base="#branch:main"; head="#branch:dark-mode"; v="0.1.0" ---
```

---

## 2. Cross-Forge Contribution

Alice works on GitLab, Bob maintains the upstream repo on GitHub. Alice forks, proposes changes, Bob reviews on GitHub.

```
    GitLab (alice)                 GitHub (bob)
      │                              │
      ●  push dark-mode branch       │
      ●  create PR                   │
      │  head=gitlab/alice/repo      │
      │  base=github/bob/repo        │
      │                              │
      │                              ●  follow alice's repo
      │                              ●  fetch gitmsg/review
      │                              ●  see cross-forge PR
      │                              ●  post review
      │                              │
      ●  read review (fetch bob)     │
      ●  push fix                    │
      │                              │
      │                              ●  approve
      │                              ●  merge
```

### Messages

**Alice creates cross-forge pull request (on her GitLab repo's gitmsg/review branch):**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="https://github.com/bob/repo#branch:main"; head="https://gitlab.com/alice/repo#branch:dark-mode"; v="0.1.0" ---
```

**Bob reviews (on his GitHub repo's gitmsg/review branch):**
```
Solid approach. One suggestion on the CSS transitions.

--- GitMsg: ext="review"; type="feedback"; pull-request="https://gitlab.com/alice/repo#commit:bbb222333444@gitmsg/review"; review-state="approved"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@gitlab.com"; time="2025-01-20T10:00:00Z"; ref="https://gitlab.com/alice/repo#commit:bbb222333444@gitmsg/review"; v="0.1.0" ---
> Add dark mode support
```

**Bob merges (on his GitHub repo):**

Step 1 — Copy fork PR to upstream (preserves original author via GitMsg-Ref):
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; head="https://gitlab.com/alice/repo#branch:dark-mode"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@gitlab.com"; time="2025-01-20T10:00:00Z"; ref="https://gitlab.com/alice/repo#commit:bbb222333444@gitmsg/review"; v="0.1.0" ---
> Add dark mode support
```

Step 2 — Merge edit references the local copy:
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; edits="#commit:ccc333444555@gitmsg/review"; state="merged"; base="#branch:main"; head="https://gitlab.com/alice/repo#branch:dark-mode"; merge-base="f1e2d3c4b5a6"; merge-head="a1b2c3d4e5f6"; v="0.1.0" ---
```

---

## 3. Fork PR Discovery

A maintainer registers forks so PRs (and issues) from contributors are automatically discovered during fetch, appear in `pr list`, and trigger notifications.

```
    Maintainer (upstream)               Contributor (fork)
      │                                      │
      ●  gitsocial fork add                  │
      │  <fork-url>                          │
      │                                      │
      │                                      ●  push feature branch
      │                                      ●  create PR
      │                                      │  base=#branch:main
      │                                      │  head=#branch:feature
      │                                      │
      ●  gitsocial fetch                     │
      │  → fetches fork's gitmsg/* branches  │
      │  → all extension processors          │
      │                                      │
      ●  pr list shows fork PR               │
      ●  notification: "fork-pr"             │
      │                                      │
      ●  review / merge                      │
```

### How It Works

1. **Register fork**: `gitsocial fork add https://github.com/contributor/repo`
2. **Fetch**: During `gitsocial fetch`, all `gitmsg/*` branches from each registered fork are fetched and processed through all extension processors (review, PM, social, release)
3. **Discovery**: Fork PRs with a `base` that is a local ref (`#branch:main`) or explicitly targets the workspace URL are included in `pr list`
4. **Notifications**: New fork PRs appear as `fork-pr` notifications; feedback on workspace PRs appears as `feedback`/`approved`/`changes-requested` notifications

### Fork Config

Forks are stored in the core config (`refs/gitmsg/core/config`):

```json
{
  "version": "0.1.0",
  "forks": [
    "https://github.com/contributor/repo"
  ]
}
```

---

## 4. Review with Suggestions

Reviewer proposes concrete code changes that the author can apply directly.

```
     Bob                           Alice
      │                              │
      ●  create PR                   │
      │                              │
      │                              ●  post suggestion
      │                              │
      ●  apply suggestion            │
      ●  push updated branch         │
      │                              │
      │                              ●  approve
```

### Messages

**Alice suggests a change:**
~~~
Use a CSS custom property for the transition

```suggestion
transition: background-color var(--theme-transition, 200ms) ease;
```

--- GitMsg: ext="review"; type="feedback"; pull-request="#commit:aaa111222333@gitmsg/review"; commit="def456789abc"; file="src/theme.css"; line-start="18"; side="right"; suggestion="true"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Bob"; email="bob@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:aaa111222333@gitmsg/review"; v="0.1.0" ---
> Refactor theme transitions
~~~

---

## 5. Multi-Reviewer Approval

A pull request requires multiple reviewers. Aggregation determines merge readiness.

```
    Alice                   Bob                       Carol
      │                      │                        │
      ●  create PR           │                        │
      │  reviewers=          │                        │
      │  bob,carol           │                        │
      │                      │                        │
      │                      ●  changes-requested     │
      │                      │                        │
      ●  push fix            │                        │
      │                      │                        │
      │                      ●  approved              │
      │                      │                        ●  approved
      │                      │                        │
      ●  merge               │                        │
```

### Aggregation Rules

- Any `changes-requested` → pull request is blocked
- All reviewers `approved` → pull request is ready to merge
- Only the latest review per reviewer counts

---

## 6. Pull Request Linked to Issues

A pull request closes issues when merged. The `closes` field contains commit references to PM issues.

### Messages

**Pull request that closes two issues:**
```
Add dark mode support

Implements theme switching with system preference detection.

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; closes="#commit:abc123456789@gitmsg/pm,#commit:def456789012@gitmsg/pm"; head="#branch:dark-mode"; v="0.1.0" ---

--- GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0" ---
> Add dark mode support

--- GitMsg-Ref: ext="pm"; type="issue"; author="Bob"; email="bob@example.com"; time="2025-01-07T09:00:00Z"; ref="#commit:def456789012@gitmsg/pm"; v="0.1.0" ---
> Support system theme preference
```

When the pull request transitions to `state="merged"`, implementations auto-close the referenced issues.

---

## 7. Discussion Threads on a Pull Request

General discussion (not anchored to code) uses GitSocial comments. Nested replies use `reply-to`.

```
    Alice               Bob                   Carol
      │                  │                      │
      ●  create PR       │                      │
      │                  │                      │
      │                  ●  "Should we also     │
      │                  │   handle prefers-    │
      │                  │   contrast?"         │
      │                  │                      │
      │                  │                      ●  "Yes, good idea.
      │                  │                      │   I can follow up
      │                  │                      │   in a separate PR."
      │                  │                      │
      ●  "Filed          │                      │
      │   issue #42"     │                      │
```

### Messages

**Bob comments on the pull request:**
```
Should we also handle prefers-contrast?

--- GitMsg: ext="social"; type="comment"; original="#commit:aaa111222333@gitmsg/review"; v="0.1.0" ---

--- GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:aaa111222333@gitmsg/review"; v="0.1.0" ---
> Add dark mode support
```

**Carol replies to Bob's comment:**
```
Yes, good idea. I can follow up in a separate PR.

--- GitMsg: ext="social"; type="comment"; original="#commit:aaa111222333@gitmsg/review"; reply-to="#commit:ccc333444555@gitmsg/social"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="comment"; author="Bob"; email="bob@example.com"; time="2025-01-20T11:00:00Z"; ref="#commit:ccc333444555@gitmsg/social"; v="0.1.0" ---
> Should we also handle prefers-contrast?
```

---

## 8. Pull Request Lifecycle

Full state machine for a pull request:

```
         create
           │
           v
        ┌──────┐           ┌────────┐
        │ open │─────────> │ closed │
        └──┬───┘           └────────┘
           │               reopen via
           v               new PR
       ┌────────┐
       │ merged │
       └────────┘
```

- `open` → `merged`: Author edits the original pull request commit with `state="merged"`
- `open` → `closed`: Author or maintainer edits with `state="closed"`
- Retract: Separate from closing. Uses `retracted="true"` to hide from all views
- Reopen: Not a state transition. Create a new pull request referencing the same branches

---

## 9. Viewing Diffs in TUI

The TUI provides a files changed view accessible from the PR detail via `d`. It shows syntax-highlighted unified diffs with colored added/removed lines and line number gutters.

Inline feedback from the diff view pre-fills the file, line number, and side parameters when creating feedback commits, producing the same `file`, `line-start`, and `side` headers as CLI-created inline reviews.

---

## 10. Version Tracking and Review Staleness

When an author rebases or pushes new commits, `pr update` captures new branch tips as a version. Reviewers see version-aware staleness — using `git range-diff` to distinguish pure rebases from actual code changes.

```
    Alice                               Bob
      │                                  │
      ●  create PR (head=dark-mode)      │
      │  base-tip="aaa", head-tip="bbb"  │
      │                                  │
      │                                  ●  review, request changes
      │                                  │
      ●  push fix                        │
      ●  pr update                       │
      │  base-tip="aaa", head-tip="ccc"  │
      │                                  │
      │                                  ●  fetch → sees new version
      │                                  ●  pr show:
      │                                  │  "changes requested
      │                                  │   (reviewed original,
      │                                  │   current is latest,
      │                                  │   code changed) [stale]"
      │                                  │
      │                                  ●  pr diff → range-diff
      │                                  │  shows what changed
      │                                  ●  approve
```

### Messages

**Alice creates PR (version 0 = original):**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; base-tip="aaa111bbb222"; head="#branch:dark-mode"; head-tip="bbb222ccc333"; reviewers="bob@example.com"; v="0.1.0" ---
```

**Alice runs `pr update` after pushing fixes (version 1 = latest):**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; edits="#commit:aaa111222333@gitmsg/review"; state="open"; base="#branch:main"; base-tip="aaa111bbb222"; head="#branch:dark-mode"; head-tip="ccc333ddd444"; v="0.1.0" ---
```

Updates are explicit — the author signals "new code is ready" by running `pr update`. Not every WIP push triggers a version.

### Version-Aware Staleness Detection

The reviewer's feedback has a timestamp. The PR's edits chain has timestamps and `head-tip` values. By comparing them, we know which version the reviewer saw. When the head-tip changed, `git range-diff` determines if the patches actually changed:

- `head-tip` unchanged → approval is **current** ("no code changes")
- `head-tip` changed, patches identical (pure rebase) → "no code changes"
- Patch content changed → "code changed" with `[stale]` marker
- Never auto-dismiss. Never hide. Let humans decide.

---

## 11. Merge Strategies

Four merge strategies available per-PR via `--strategy` flag. Teams pick per-PR instead of fighting over a repo-wide default.

```
    Alice
      │
      ●  pr merge <ref>                    # fast-forward (default)
      ●  pr merge <ref> --strategy squash  # squash all commits
      ●  pr merge <ref> --strategy rebase  # replay commits onto base
      ●  pr merge <ref> --strategy merge   # force merge commit
```

### Messages

**Merge edit (same for all strategies — the git operation differs, but the review record is the same):**
```
Add dark mode support

--- GitMsg: ext="review"; type="pull-request"; edits="#commit:aaa111222333@gitmsg/review"; state="merged"; base="#branch:main"; base-tip="ddd444eee555"; head="#branch:dark-mode"; head-tip="eee555fff666"; merge-base="ddd444eee555"; merge-head="eee555fff666"; v="0.1.0" ---
```

`merge-base` and `merge-head` are captured before the merge (lost after fast-forward). They enable reconstructing the original diff of a merged PR.

---

## 12. Branch Sync

Keep the head branch up-to-date with the base branch. `pr sync` rebases or merges, then auto-captures new tips.

```
    Alice
      │
      ●  pr show → "3 commits behind main"
      │
      ●  pr sync <ref>          # rebase head onto base (default)
      ●  pr sync <ref> --strategy merge    # merge base into head
      │
      │  → auto-runs pr update
      │  → new version with updated tips
```

After sync, the version history shows the rebase via range-diff. Reviewers can see what changed (if anything) between the pre-sync and post-sync versions.
