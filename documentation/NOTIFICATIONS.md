# Notifications

Notifications from all extensions are in one feed, with the newest first.

[Commands](#commands) · [Levels](#levels) · [Desktop notifications](#desktop-notifications) · [Types](#types)

## Commands

```
gitsocial notifications [--all] [--type mention,follow] [--limit <n>]
gitsocial notifications count
gitsocial notifications read <id> | read-all
gitsocial notifications unread <id> | unread-all
gitsocial notifications level [<repository>] [all|default|off]
```

`gitsocial fetch` reports the unread count when it finishes. In the TUI, `@` opens the feed.

`gitsocial notifications` prints the id of each notification that has a read state: `<repository>#commit:<hash>`, or `<repository>#follow` for a follow. `read` and `unread` take that id.

## Levels

A level of a repository changes the notifications that you get from it. The level is in your personal settings ([SETTINGS.md](SETTINGS.md)), not in the repository.

```
gitsocial notifications level                                   # print the levels
gitsocial notifications level all                               # the workspace
gitsocial notifications level https://github.com/org/repo off
gitsocial notifications level https://github.com/org/repo default  # remove the level
```

| Level | Notifications |
|---|---|
| default | the [types](#types); a repository with no level is at default |
| all | the types, and one `activity` notification for each new timeline item of the repository that is not yours, from the time that you set the level |
| off | the notifications stay in the feed and in the count, and go to the webhook; none gives a desktop message |

The level applies to the repository of the commit of a notification, not to the repository of the item that the commit is about. For example, a comment from a followed repository on your post gives a desktop message when your workspace is at `off`; it gives none when the followed repository is at `off`.

A registered fork with no level takes the level of the workspace. The level `all` applies only to the workspace, its registered forks and the repositories of its lists. The `activity` notifications count toward the limit of three desktop messages, so a fetch with many of them gives one summary. A notification of a repository at `off` is recorded as sent, so it gets no desktop message after a later change of the level.

| Rule | Test |
|---|---|
| A repository with no level gives the same notifications as before | `TestLevel_defaultAddsNothing` |
| The level `all` gives one `activity` notification for each timeline item of the repository | `TestLevel_allGivesTimelineRows` |
| An item that is not newer than the set time gives no notification | `TestLevel_allStartsAtSetTime` |
| An item by you gives no notification | `TestLevel_allSkipsOwnItems` |
| A repository that the workspace does not use gives no `activity` notification | `TestLevel_allOnlyInScope` |
| A fork commit that is also in the workspace gives no `activity` notification | `TestLevel_forkCopyGivesNoEvent` |
| A commit with one of the types keeps that type | `TestOnePerKey_activityRanksLast` |
| An assign after a read `activity` notification of the issue gives a new unread one | `TestLevel_assignAfterReadGivesEvent` |
| An `activity` notification is sent one time and is read by its hash | `TestDeliver_activityClaimedOnce` |
| A notification of a repository at `off` is recorded as sent, is in the feed, goes to the webhook, gives no desktop message and is not in the count of a summary | `TestDeliver_offRepoNoDesktop` |
| A registered fork with no level takes the level of the workspace | `TestLevel_forkTakesWorkspaceLevel` |
| The level of a fork has priority over the level of the workspace | `TestLevel_forkOwnEntryWins` |
| A running TUI, `serve` or RPC server reads a level that a different process wrote | `TestLevel_seenByRunningProcess` |
| A set stores the identity of the repository and the set time; `default` removes the level | `TestWriteNotificationLevel_roundTrip` |
| The command sets, prints and removes a level | `TestNotificationsLevel_setAndList` |
| An `activity` row in the TUI opens what the timeline row of the item opens | `TestActivityRow_timelineTarget` |

## Desktop notifications

After a fetch, GitSocial sends a desktop message for each unread notification that no process sent before. Up to three notifications give one message each, and more give one summary message. The cache records each notification that is sent, so the TUI, `gitsocial fetch` and RPC on one cache send a notification one time.

| Fetch | Desktop message | Webhook |
|---|---|---|
| the auto-fetch of the TUI | yes | yes |
| the key `f` in the TUI | no, the footer shows the result; no later fetch sends these notifications | yes |
| the command `gitsocial fetch`, RPC `core.fetch` | yes | yes |
| the command `gitsocial fetch --json`, `gitsocial social fetch`, `gitsocial serve`, the sync when the TUI starts, the fetch of one repository or of an added repository in the TUI, an import | no; the next fetch of the first three rows sends the notifications | no; the same |

Turn on `fetch.auto.enabled` to get desktop messages from the TUI. A notification that you read before a fetch sends it gets no message, also when you mark it unread again. The [branch notifications](#types) send no message. A new cache, or a repository that you add, can bring old unread notifications; they give a maximum of three messages or one summary.

The cache records one sent state per notification, not one per channel. A notification that the webhook got while the desktop was off gets no desktop message later.

| Rule | Test |
|---|---|
| Two deliveries on one cache send an event one time | `TestDeliver_twoProcessesSendOnce` |
| A fetch without delivery leaves its events for the next delivery | `TestDeliver_afterServeFetch` |
| A manual fetch posts to the webhook and shows no desktop message, now or later | `TestDeliver_manualClaimsNoDesktop` |
| An event that was read is never sent | `TestDeliver_readThenUnread` |
| An event that was sent stays unread | `TestClaim_staysUnread` |
| With the desktop off and no webhook, nothing is recorded as sent | `TestDeliver_channelsOffClaimsNothing` |

A click on a message goes to the process that sent it.

| Sender | macOS | Linux | Windows |
|---|---|---|---|
| the TUI | brings the terminal of the TUI to the front and opens the item in the TUI | opens the item in the TUI; in Ghostty 1.4 or later, in GNOME Terminal and in Konsole on KDE Plasma the tab of the TUI comes to the front; in Ptyxis the Ptyxis window that was used last comes to the front, and the tab is not selected; in another terminal the window stays behind | no click |
| the CLI (`gitsocial fetch`), RPC | opens the item on the site | opens the item on the site | opens the item on the site |

In the TUI, the click opens the view that Enter on the row of the feed opens, and marks the notification read. A summary opens the feed. Back goes to the view before the click. In Ghostty, Terminal and iTerm2, the tab of the TUI comes to the front; in another terminal app, only the app comes to the front. Inside tmux, the click selects the tmux window of the TUI and brings the terminal of the tmux client that was active last to the front, with the same result for each terminal as outside tmux.

A click of `gitsocial fetch` or RPC opens the served site when `gitsocial serve` runs for the workspace, else the `site.url` of the workspace. With neither, the message has no click. A summary, and an item of another repository, open the start page of the site.

Known cases of a click on a message of the TUI:

- When the TUI exited, the click does nothing.
- When an input or a dialog is open in the TUI, or the TUI does not answer for 2 s (for example, an editor is open), the view does not change. On macOS, and on Linux in Ghostty, GNOME Terminal, Ptyxis and Konsole on KDE Plasma, the terminal comes to the front.
- A `follow` opens the feed, not the repository.
- A `GitSocial.app` older than the TUI cannot take the click: the message comes from `osascript` with no click until you update the app.
- On Linux, a message has a click only when the session D-Bus is usable. Without it, `notify-send` shows the message with no click.
- On Linux, a click works for one hour after the message, the same as the click that opens the site. After the hour, the click does nothing.

| OS | Sender | Install |
|---|---|---|
| macOS | `GitSocial.app` | the app in `~/Applications` or `/Applications`; the Homebrew cask and the install script place it, and the darwin release zip has it |
| Linux | the notification service on the session D-Bus; `notify-send` for a message with no click | a session D-Bus for the click; the first message writes the icon to `~/.local/share/icons` |
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
| `activity` | workspace, forks, followed | a repository at the [level](#levels) `all` has a new timeline item that is not yours |

The four branch notifications come from `review_branch_observations` ([ARCHITECTURE.md](ARCHITECTURE.md#schema)), which GitSocial updates after each fetch, and go to the author and the reviewers of the pull request. A notification clears when `pr update` records the new tip, when the branch is on its remote again, or when the pull request closes. `branch-diverged` clears when the local and remote branches no longer diverge, for example after you merge and push.

The four branch notifications and `branch-diverged` are conditions: each is in the feed and in the unread count until the cause goes away. A condition has no read state, so `m`, `u`, `read` and `read-all` do not change it, and it sends no desktop message and no webhook post.

Each other notification is an event: one commit, or one follow of one repository. Its state is one row of `core_notification_state`, which exists only after the event is sent or read. The state of a commit holds for its hash, with no branch and in each repository, so it stays with a commit that moves to a different branch, and with the copy of a commit in a registered fork.

One commit gives one notification. A commit that is in the workspace and in a fork gives the notification of the workspace. When a commit matches more than one type, this order decides:

1. a type of `pm`, `review`, `release` or `memo`; of two such types, `issue-assigned` before `fork-issue` and `review-requested` before `fork-pr`
2. a type of `social` (`comment`, `repost`, `quote`)
3. `mention`, then `reference`, then `edit`
4. `activity`

`--type` filters before this rule, so `--type mention` also finds a comment that mentions you.

| Rule | Test |
|---|---|
| One commit gives one notification and one count, also with a limit and a type filter | `TestGather_oneEventPerKey` |
| A commit in the workspace and in a fork gives one notification, one message and one state | `TestOnePerKey_workspaceRowWins`, `TestClaim_copyOfCommit` |
| The count equals the number of rows of the unread feed | `TestUnreadCount_equalsFeed` |
| A condition has no state, hides no event and no other condition, and "read all" leaves it | `TestCondition_noState` |
| A follow is marked read and unread alone | `TestFollow_markByKey`, `TestNotificationKey_roundTrip` |
| A state change needs an edit whose action changes the state | `TestIssueStateChange_actionOnly`, `TestPRStateChange_actionOnly` |
