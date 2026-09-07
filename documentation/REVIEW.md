# Review Extension

Pull requests and review feedback are commits on the `gitmsg/review` branch ([GITREVIEW.md](../specs/GITREVIEW.md)) of the author's repository, discovered by reviewers through fetch and follow.

[Initialize](#initialize) · [Pull requests](#pull-requests) · [Feedback](#feedback) · [Forks](#forks) · [Flows](#flows) · [Reference](#reference)

## Initialize

```
gitsocial review init [-b <branch>]         # refs/gitmsg/review/config and the gitmsg/review branch
gitsocial review config get|set|list
```

`init` is idempotent.

## Pull requests

```
gitsocial review pr create "Add dark mode" --base '#branch:main' --head '#branch:dark-mode' \
    [--reviewers bob@example.com,carol@example.com] [--closes <issue-ref>] [--draft] [--stack | --depends-on <pr-ref>]
gitsocial review pr list [-s open]
gitsocial review pr show <ref>
gitsocial review pr edit <ref> [--title ...] [--body ...] [--reviewers ...] [--closes ...]
gitsocial review pr update <ref>                       # record the current branch tips as a new version
gitsocial review pr diff <ref> [--from <n> --to <m>]   # range-diff between two versions
gitsocial review pr sync <ref> [--strategy rebase|merge]
gitsocial review pr merge <ref> [--strategy fast-forward|squash|rebase|merge]
gitsocial review pr close <ref>
gitsocial review pr retract <ref>
gitsocial review pr draft <ref> | ready <ref>
gitsocial review pr stack <ref> | rebase-stack <ref> | sync-stack <ref>
```

- `--base` and `--head` take `#branch:<name>` for this repository or `<url>#branch:<name>` for another one.
- `--stack` derives `depends-on` from the pull request whose head is this one's base; `--depends-on` sets it by hand.

## Feedback

```
gitsocial review feedback approve <pr-ref> [-m "LGTM"]
gitsocial review feedback request-changes <pr-ref> -m "Why"
gitsocial review feedback comment "Consider caching this" --pr <pr-ref> --commit <sha12> --file path/to.go \
    --new-line 42 [--new-line-end 50] [--old-line 40] [--suggest]
```

Feedback is tied to the version the reviewer saw. A later version never dismisses it; it is marked stale when the code changed.

## Forks

```
gitsocial fork add <fork-url>       # also `gitsocial review fork add|list|remove`
gitsocial fetch
```

Pull requests from a registered fork appear in `pr list` and raise a `fork-pr` notification. On merge or close, a fork pull request is copied to this repository with the author's identity preserved, so the record survives the fork's deletion.

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

Alice's repository is on GitLab and Bob's on GitHub; either could be a bucket instead, on any S3 provider, and two buckets need not share one. The pull request lives on Alice's `gitmsg/review` branch with a `base` URL into Bob's repository; Bob's feedback lives on his own branch and references her pull request by URL.

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
      │                                 ●  pr merge: copies the pull request to
      │                                 │  bob's repository, author preserved
```

A bucket upstream is named by its `s3://` URL, the form it uses for itself, or targeted with a local ref such as `#branch:main`; the contributor reads a public bucket over its `https://` domain.

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

A fork's pull request is discovered when its `base` is a local ref or names the workspace URL.

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

`pr update` records `base-tip` and `head-tip` as a new version, and the edits chain is the version history. Feedback stays current when the head tip is unchanged or the patches are identical, and is marked stale when the code changed. Nothing is dismissed automatically.

### Stacks

```
    Alice                                        Bob
      │                                           │
      ●  pr create PR1   main ← middleware        │
      ●  pr create PR2 --stack                    │
      │  middleware ← routes, depends-on=PR1      │
      ●  pr create PR3 --stack                    │
      │  routes ← tests, depends-on=PR2           │
      │                                           ●  pr stack: the whole chain
      │                                           ●  approve PR1
      ●  pr merge PR1: PR2 retargets to main      │
      ●  pr sync PR2: rebases it onto main        │
      ●  pr rebase-stack PR2: rebases PR3         │
      │                                           ●  approve PR2
      ●  pr merge PR2: PR3 retargets              │
```

`rebase-stack` rebases every member above the given one and records versions, stopping at the first conflict; `sync-stack` records tips without rebasing. `pr merge` refuses a member whose dependency is unmerged. Stacks span forges, since `depends-on` is a reference, and `gitsocial import review` detects them among imported pull requests by matching base and head branches.

### Other flows

- **Suggestions.** `feedback comment --suggest` carries a replacement in a `suggestion` fence; the author applies it and pushes.
- **Several reviewers.** With `--reviewers bob,carol`, any `changes-requested` blocks the pull request, and it is ready when every reviewer's latest feedback is `approved`.
- **Linked issues.** `--closes <issue-ref>,<issue-ref>` closes the issues when the pull request merges.
- **Discussion.** General comments are social comments on the pull request, `gitsocial social comment <pr-ref> "..."`; replies nest with `reply-to`.
- **Lifecycle.** `open` becomes `merged` or `closed` by an edit from the base owner. The author withdraws with `retract`. There is no reopen; create a new pull request.
- **Merge strategies.** `pr merge --strategy fast-forward|squash|rebase|merge`, per pull request; fast-forward is the default. `merge-base` and `merge-head` are recorded before the merge, so the merged diff can be reconstructed. After a merge the base branch is pushed; a failed push is a warning, and the merge stands locally.
- **Branch sync.** `pr sync` rebases the head onto the base, or merges the base into it with `--strategy merge`, then records the new tips as a version.

## Reference

- Versions and review aggregation: [GITREVIEW.md §1.5](../specs/GITREVIEW.md#15-editing-and-retracting) and [§1.8](../specs/GITREVIEW.md#18-review-aggregation).
- `pr list` shows this repository's pull requests and those from [registered forks](CLI.md#gitsocial-fork) whose base is this repository.
- Fork registrations live at `refs/gitmsg/core/forks/<urlHash>` ([ARCHITECTURE.md](ARCHITECTURE.md#refs-and-keys)).
- New fork pull requests, feedback, approvals and change requests raise [notifications](NOTIFICATIONS.md#types), as do branch tips that moved or vanished under an open pull request.
- In the TUI, `R` opens pull requests ([TUI-KEYS.md](TUI-KEYS.md#review-extension)); the detail view has files changed (`d`), interdiff, history and feedback, and `[` and `]` move through a stack.
