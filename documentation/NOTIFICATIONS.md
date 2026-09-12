# Notifications

Notifications gather events from every extension into one feed, newest first, with read state kept per notification in `core_notification_reads`.

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

Scopes: the workspace is your repository, forks are registered forks, followed are the repositories in your lists, inherited are the memo sources added with `memo inherit add`, and any is every repository in the cache. Your own actions do not notify you.

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
| `head-advanced`, `base-advanced` | workspace, forks | an open pull request's branch moved past its recorded tip; `pr update` records the new one |
| `head-deleted`, `base-deleted` | workspace, forks | an open pull request's branch is gone from its remote |
| `new-release` | followed | a repository in your lists publishes a release |
| `memo-comment` | any | someone else comments on a memo you authored |
| `inherited-policy` | inherited | a `priority/critical` memo appears on an inherited source |
| `branch-diverged` | workspace | a local `gitmsg/<ext>` branch has unpushed commits and diverges from origin |

The four branch notifications come from `review_branch_observations` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)), refreshed after each fetch. They go to the pull request's author and reviewers, and clear once the pull request catches up. `branch-diverged` clears once the branch is reconciled and pushed.
