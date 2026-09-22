# Notifications

Notifications from all extensions are in one feed, with the newest first.

[Commands](#commands) · [Types](#types)

## Commands

```
gitsocial notifications [--all] [--type mention,follow] [--limit <n>]
gitsocial notifications count
gitsocial notifications read <id> | read-all
gitsocial notifications unread <id> | unread-all
```

`gitsocial fetch` reports the unread count when it finishes. In the TUI, `@` opens the feed.

## Types

The scope of a type tells which repositories can create it. GitSocial does not notify you about your own actions.

| Scope | Repositories |
|---|---|
| workspace | your repository |
| forks | the registered forks |
| followed | the repositories in your lists |
| inherited | the memo repositories that you add with `memo inherit add` |
| any | all repositories in the cache |

| Type | Scope | Trigger |
|---|---|---|
| `mention` | any | your email is mentioned in a commit message |
| `reference` | any | a `Closes:` or `Refs:` trailer names an item you authored |
| `edit` | any | someone else edits or retracts an item you authored, in any extension |
| `comment`, `repost`, `quote` | workspace, followed | on your post, or on a thread you took part in |
| `follow` | workspace | a repository adds yours to a list |
| `issue-assigned` | any | an issue is assigned to you |
| `issue-closed`, `issue-reopened` | any | someone else closes or reopens an issue assigned to you |
| `fork-issue` | forks | someone else opens an issue on a registered fork |
| `fork-pr` | forks | a non-draft pull request on a registered fork targets your repository |
| `review-requested` | any | you are added as a reviewer on an open, non-draft pull request |
| `feedback`, `approved`, `changes-requested` | workspace, any | on a pull request in your workspace or one you authored |
| `pr-merged`, `pr-closed` | any | someone else merges or closes a pull request you authored |
| `pr-ready` | forks | a draft pull request on a registered fork is marked ready |
| `head-advanced`, `base-advanced` | workspace, forks | a branch of an open pull request is ahead of its recorded tip, until `pr update` records the new tip |
| `head-deleted`, `base-deleted` | workspace, forks | a branch of an open pull request is not on its remote |
| `new-release` | followed | a repository in your lists publishes a release |
| `memo-comment` | any | someone else comments on a memo you authored |
| `inherited-policy` | inherited | a `priority/critical` memo is added to an inherited repository |
| `branch-diverged` | workspace | a local `gitmsg/<ext>` branch has unpushed commits and diverges from origin |

The four branch notifications come from `review_branch_observations` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)), which GitSocial updates after each fetch, and go to the author and the reviewers of the pull request. A notification clears when `pr update` records the new tip, when the branch is on its remote again, or when the pull request closes. `branch-diverged` clears when the local and remote branches no longer diverge, for example after you merge and push.

The read state of each notification is in `core_notification_reads`.
