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

A click on a message goes to the process that sent it.

| Sender | macOS | Linux and Windows |
|---|---|---|
| the TUI | brings the terminal app of the TUI to the front and opens the item in the TUI | no click |
| the CLI (`gitsocial fetch`), RPC | opens the item on the site | opens the item on the site |

In the TUI, the click opens the view that Enter on the row of the feed opens, and marks the notification read. A summary opens the feed. Back goes to the view before the click.

A click of `gitsocial fetch` or RPC opens the served site when `gitsocial serve` runs for the workspace, else the `site.url` of the workspace. With neither, the message has no click. A summary, and an item of another repository, open the start page of the site.

Known cases of a click on a message of the TUI:

- When the TUI exited, the click does nothing.
- When an input or a dialog is open in the TUI, or the TUI does not answer for 2 s (for example, an editor is open), the terminal comes to the front and the view does not change.
- A `follow` opens the feed, not the repository.
- A `GitSocial.app` older than the TUI cannot take the click: the message comes from `osascript` with no click until you update the app.

| OS | Sender | Install |
|---|---|---|
| macOS | `GitSocial.app` | the app in `~/Applications` or `/Applications`; the Homebrew cask and the install script place it, and the darwin release zip has it |
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

### In the browser

The static site, on a bucket or under `gitsocial serve`, shows browser notifications from an open tab. When no tab of the site is open, nothing shows; there is no push server. `serve` fetches on the `fetch.auto.interval` and sends no desktop message itself, so a reader with the TUI open gets each item once.

1. Click the bell in the sidebar header.
2. Click **Turn on notifications** and allow them in the prompt of the browser. The site asks only after this click.
3. To filter, type your email in **Notify me about items for**.

With an email, the tab shows the new items that mention the email, assign an issue to it, request its review, or comment on a post that it wrote. An item by that email does not show. Without an email, the tab shows each new post, comment, issue, pull request and release.

When you open the site, and then each minute while the tab is open, also in the background, and when you go back to the tab, the tab reads the ref list of the site. Each item shows once per browser, and the items that arrived while no tab was open show when you open the site again. The first time a browser turns notifications on, it records the current items and shows nothing. Up to three new items give one notification each, and more give one summary, the same as on the desktop. The tab shows each notification through the service worker of the site, so the notification stays in the notification list of the system after the tab closes. A click opens the item in a tab of the site, or opens the site when no tab is open, and a click on the summary opens the timeline. A browser without service workers shows the notification from the tab, and it can go away when the tab closes.

The browser keeps the email and the seen items in its local storage for the site. Without local storage, on a page that is not https or localhost, or in a browser with no Notification API, the panel tells why and has no button. Safari on iOS gives the Notification API only to a web app on the home screen.

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
