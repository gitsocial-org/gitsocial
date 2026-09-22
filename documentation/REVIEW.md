# Review Extension

Pull requests and feedback are commits on the `gitmsg/review` branch ([GITREVIEW.md](../specs/GITREVIEW.md)) of the author's repository. Reviewers get them when they follow or fetch that repository.

[Initialize](#initialize) · [Pull requests](#pull-requests) · [Feedback](#feedback) · [Forks](#forks) · [Flows](#flows) · [Reference](#reference)

## Initialize

```
gitsocial review init
gitsocial review config get|set|list
```

`init` is idempotent. It creates `refs/gitmsg/review/config` and the `gitmsg/review` branch.

## Pull requests

```
gitsocial review pr create "Add dark mode" --base '#branch:main' --head '#branch:dark-mode' \
    [--reviewers bob@example.com,carol@example.com] [--closes <issue-ref>] [--draft] [-l area/ui] \
    [--stack | --depends-on <pr-ref>] [--allow-unpublished-head]
gitsocial review pr list [-s open]
gitsocial review pr show <ref>
gitsocial review pr edit <ref> [--title ...] [--body ...] [--reviewers ...] [--closes ...]
gitsocial review pr update <ref>                       # record the current branch tips as a new version
gitsocial review pr diff <ref> [--from <n> --to <m>]   # range-diff between two versions
gitsocial review pr sync <ref> [--strategy rebase|merge]
gitsocial review pr merge <ref> [--strategy ff|squash|rebase|merge]
gitsocial review pr close <ref>
gitsocial review pr adopt <ref>                        # adopt a registered fork's pull request into this repository
gitsocial review pr retract <ref>
gitsocial review pr draft <ref> | ready <ref>
gitsocial review pr stack <ref> | rebase-stack <ref> | sync-stack <ref>
```

- `--base` and `--head` take `#branch:<name>` for this repository or `<url>#branch:<name>` for another one.
- `--stack` sets `depends-on` to the pull request whose head is the base of this one; `--depends-on` sets it explicitly.

## Feedback

```
gitsocial review feedback approve <pr-ref> [-m "LGTM"]
gitsocial review feedback request-changes <pr-ref> -m "Why"
gitsocial review feedback comment "Consider caching this" --pr <pr-ref> --commit <sha12> --file path/to.go \
    --new-line 42 [--new-line-end 50] [--old-line 40] [--old-line-end 48] [--suggest]
```

Feedback applies to the version that the reviewer saw, and a later version does not dismiss it. See [Versions](#versions).

## Forks

```
gitsocial fork add <fork-url>       # also `gitsocial review fork add|list|remove`
gitsocial fetch
```

`fork add` fetches the URL that you type; `fork remove` accepts each form of it, because GitSocial compares URLs by identity.

Pull requests from a registered fork show in `pr list` and create a `fork-pr` notification. When you merge or close a pull request of a fork, GitSocial adopts it into this repository and keeps the identity of its author.

## Flows

### Same repository

```
    Alice                            Bob
      │                               │
      ●  push dark-mode               │
      ●  pr create, push              │
      │  base=main, head=dark-mode    │
      │                               ●  fetch
      │                               ●  post inline feedback, push
      ●  fetch, push a fix            │
      ●  pr update, push              │
      │                               ●  fetch, approve, push
      ●  fetch, pr merge              │
      ●  the linked issues close      │
```

### Cross-forge

Alice's repository is on GitLab and Bob's is on GitHub. Either can also be a bucket, and two buckets can be on different S3 providers.

The pull request is on Alice's `gitmsg/review` branch, and its `base` URL names Bob's repository. Bob's feedback is on his own branch and references her pull request by URL.

```
    GitLab (alice)                    GitHub (bob)
      │                                 │
      ●  push dark-mode                 │
      ●  pr create, push                │
      │  head=gitlab.com/alice/repo     │
      │  base=github.com/bob/repo       │
      │                                 ●  fork add alice's repository, fetch
      │                                 ●  post feedback, push
      ●  follow bob's repository, fetch │
      ●  push a fix, pr update, push    │
      │                                 ●  approve
      │                                 ●  pr merge: adopts the pull request into
      │                                 │  bob's repository, keeps its author
```

To name a bucket as the base, use its `s3://` URL or a local reference such as `#branch:main`. A contributor reads a public bucket through its `https://` domain.

### Fork discovery

```
    Maintainer (upstream)                  Contributor (fork)
      │                                         │
      ●  gitsocial fork add <fork-url>          │
      │                                         ●  push feature
      │                                         ●  pr create, base=#branch:main
      ●  gitsocial fetch                        │
      │  fetches the fork's gitmsg/* branches   │
      ●  pr list shows the fork's pull request  │
      ●  notification: fork-pr                  │
      ●  review, merge                          │
```

GitSocial finds a pull request of a fork when its `base` is a local reference or names the workspace URL.

### Versions

```
    Alice                                Bob
      │                                   │
      ●  pr create, head-tip=bbb          │
      │                                   ●  request changes
      ●  push a fix                       │
      ●  pr update, head-tip=ccc          │
      │                                   ●  fetch; pr show reads
      │                                   │  changes-requested (reviewed original, current is v1, code changed) [stale]
      │                                   ●  pr diff: range-diff of the two versions
      │                                   ●  approve
```

`pr update` records `base-tip` and `head-tip` as a new version, and the edits of the pull request are its version history.

| Feedback | After a new version |
|---|---|
| approval or change request, same head tip or same patches | current |
| approval or change request, changed code | stale |
| inline comment | keeps its commit and lines |

GitSocial does not dismiss feedback automatically.

### Stacks

```
    Alice                                        Bob
      │                                           │
      ●  pr create PR1   main ← middleware        │
      ●  pr create PR2 --stack                    │
      │  middleware ← routes, depends-on=PR1      │
      ●  pr create PR3 --stack                    │
      │  routes ← tests, depends-on=PR2           │
      │                                           ●  pr stack: every member
      │                                           ●  approve PR1
      ●  pr merge PR1: PR2 retargets to main      │
      ●  pr sync PR2: rebases it onto main        │
      ●  pr rebase-stack PR2: rebases PR3         │
      │                                           ●  approve PR2
      ●  pr merge PR2: PR3 retargets              │
```

`rebase-stack` rebases each member above the given one, records the versions, and stops at the first conflict. `sync-stack` records the tips and does not rebase. `pr merge` refuses a member whose dependency is not merged. A stack can include pull requests on different forges. `gitsocial import review` finds the stacks in imported pull requests from their base and head branches.

### Other flows

- **Suggestions.** `feedback comment --suggest` puts a replacement in a `suggestion` fence; the author applies it and pushes.
- **Several reviewers.** With `--reviewers bob,carol`, one `changes-requested` blocks the pull request, and it is ready when the latest feedback of each reviewer is `approved`.
- **Linked issues.** `--closes <issue-ref>,<issue-ref>` closes the issues when the pull request merges.
- **Discussion.** General comments are social comments on the pull request (`gitsocial social comment <pr-ref> "..."`), and replies nest with `reply-to`.
- **Lifecycle.** An edit from the owner of the base changes `open` to `merged` or `closed`, and the author withdraws a pull request with `retract`. You cannot reopen a pull request; create a new one.
- **Merge strategies.** Set the strategy for each pull request with `pr merge --strategy ff|squash|rebase|merge`; the default is `ff`. GitSocial records `merge-base` and `merge-head` before the merge and pushes the base branch after it. If the push fails, GitSocial shows a warning and the merge stays in the local repository.
- **Branch sync.** `pr sync` rebases the head onto the base, or merges the base into it with `--strategy merge`, and then records the new tips as a version. The head must be in this repository; a head in a different repository fails with `INVALID_TARGET`.

## Reference

- Versions and review aggregation: [GITREVIEW.md §1.5](../specs/GITREVIEW.md#15-editing-and-retracting) and [§1.8](../specs/GITREVIEW.md#18-review-aggregation).
- Errors when you apply a suggestion from the TUI or RPC `review.applySuggestion`: `NOT_SUGGESTION`, `INVALID_PATH`, `PARSE_ERROR`, `FILE_ERROR`, `RANGE_ERROR`, `WRITE_ERROR`.
- `review config set require-review true` makes approval a merge condition, so `pr merge` fails with `REVIEW_REQUIRED` until the latest feedback of each reviewer is `approved`.
- `pr list` shows the pull requests of this repository, and the pull requests of [registered forks](CLI.md#gitsocial-fork) whose base is this repository.
- Fork registrations are at `refs/gitmsg/core/forks/<urlHash>` ([ARCHITECTURE.md](ARCHITECTURE.md#refs-and-keys)).
- The diff of an imported pull request uses its stored tips; if no repository has one of these commits, the diff names the commit and the fetch that gets it.
- Branch tips come from the remote: `refs/remotes/origin/<branch>` for this repository, and `git ls-remote` for a fork with no tracking ref. If a branch is not on its remote, GitSocial creates a `head-deleted` or `base-deleted` notification.
- New fork pull requests, feedback, approvals and change requests create [notifications](NOTIFICATIONS.md#types), and so does a branch tip of an open pull request that moves or is deleted.
- In the TUI, `R` opens pull requests ([TUI-KEYS.md](TUI-KEYS.md#review-extension)). The detail view has files changed (`d`), interdiff, history and feedback, and `[` and `]` move through a stack.
