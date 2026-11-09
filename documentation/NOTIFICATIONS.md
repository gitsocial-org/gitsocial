# Notifications

Notifications from all extensions are in one feed, with the newest first.

[Commands](#commands) · [Desktop notifications](#desktop-notifications) · [Types](#types)

## Commands

```
gitsocial notifications [--all] [--type mention,follow] [--limit <n>]
gitsocial notifications count
gitsocial notifications read <id> | read-all
gitsocial notifications unread <id> | unread-all
```

`gitsocial fetch` reports the unread count when it finishes. In the TUI, `@` opens the feed.

## Desktop notifications

A fetch that brings new unread notifications sends a desktop message. Up to three new notifications give one message each, and more give one summary message. `gitsocial fetch --json` sends no message. The TUI sends messages only after an auto-fetch, so turn on `fetch.auto.enabled` to get messages from the TUI.

A click on a message opens the item on the site. When `gitsocial serve` runs for the workspace, the click opens the served site; else it opens the `site.url` of the workspace. With neither, the message has no click. A summary, and an item of another repository, open the start page of the site.

| OS | Sender | Install |
|---|---|---|
| macOS | `GitSocial.app` | the app in `~/Applications` or `/Applications`; the Homebrew cask installs it, and the darwin release zip has it |
| Linux | `notify-send` | libnotify 0.7.9 or later for the click; the first message writes the icon to `~/.local/share/icons` |
| Windows | a PowerShell toast | nothing; the first message writes a GitSocial shortcut into the Start Menu |

On macOS without the app, or when the app cannot post, the message comes from `osascript`, under the name of Script Editor, with no click. The first message asks for permission. If you see no messages, allow notifications for GitSocial in the system settings.

To turn the messages off, run `gitsocial settings set notifications.desktop false`, or set `GITSOCIAL_DESKTOP_NOTIFICATIONS=false` on one machine.

### Webhook

`notifications.webhook` is a URL that receives each message as JSON, for a relay that shows it on a phone or in a chat. The webhook gets the messages also when `notifications.desktop` is off.

```bash
gitsocial settings set notifications.webhook https://relay.example/hook
```

Each message is one POST with `Content-Type: application/json`. A summary has no `type`, `repo` or `hash`.

```json
{
  "title": "GitSocial",
  "body": "Alice mentioned you · owner/repo",
  "url": "https://site.example/index.html#commit:0123456789ab",
  "type": "mention",
  "repo": "https://example.com/owner/repo",
  "hash": "0123456789abcdef0123456789abcdef01234567"
}
```

| Request | Value |
|---|---|
| timeout | 5 s |
| retry | one, 1 s after a network error or a 5xx status |
| after the retry | a debug log line; the message is not sent again |
| batch | the posts of one fetch run at the same time, and the batch ends after two timeouts and the retry delay |

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
