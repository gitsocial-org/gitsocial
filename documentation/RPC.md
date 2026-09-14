# GitSocial JSON-RPC Protocol

`gitsocial rpc` serves the library over JSON-RPC 2.0 on stdio, for editors and other clients.

[Transport](#1-transport) · [Lifecycle](#2-lifecycle) · [Error codes](#3-error-codes) · [Methods](#4-methods) · [Notifications](#5-server-notifications) · [Types](#6-type-reference) · [Notes](#7-implementation-notes)

## 1. Transport

Communication uses JSON-RPC 2.0 over stdio (stdin/stdout). Each message is a single line of JSON terminated by `\n`. Stderr is reserved for logging. A line over 1 MB ends the session with no error response.

### 1.1. Message Format

Requests and responses follow JSON-RPC 2.0. All messages MUST be valid JSON on a single line.

Request:
```json
{"jsonrpc":"2.0","id":1,"method":"social.getPosts","params":{"scope":"timeline","limit":50}}
```

Success response:
```json
{"jsonrpc":"2.0","id":1,"result":[...]}
```

Error response:
```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"not found","data":{"appCode":"NOT_FOUND"}}}
```

Server notification (no id):
```json
{"jsonrpc":"2.0","method":"notifications.changed","params":{"unreadCount":3}}
```

### 1.2. Batching

Clients MAY send JSON-RPC batch requests (array of request objects). The server MUST respond with a batch response in the same order. An empty array returns a single `-32600` error.

## 2. Lifecycle

### 2.1. Startup

The client spawns `gitsocial rpc` as a subprocess. The server reads from stdin and writes to stdout. The server MUST NOT produce output before receiving `initialize`.

### 2.2. Initialize

The first request MUST be `initialize`, except for `ping`, `subscribe`, `unsubscribe` and `shutdown`, which work before it. The server opens the cache, resolves the workspace, and returns server capabilities. A second `initialize` returns `-32007 CONFLICT`; a missing `workdir` returns `-32602`.

**Method:** `initialize`

Params:
- `workdir` (string, required): Absolute path to the git repository working directory
- `cacheDir` (string): Cache directory (default: `~/.cache/gitsocial`)
- `clientName` (string): Client identifier (e.g., `"vscode"`, `"neovim"`)
- `clientVersion` (string): Client version

Result:
```json
{
  "version": "0.1.0",
  "repoURL": "https://github.com/user/repo",
  "extensions": {
    "social": {"initialized": true, "branch": "gitmsg/social"},
    "pm": {"initialized": true, "branch": "gitmsg/pm"},
    "review": {"initialized": false, "branch": ""},
    "release": {"initialized": false, "branch": ""}
  }
}
```

### 2.3. Shutdown

**Method:** `shutdown`

Params: none

Result: `"ok"`

The server closes the cache and stops the read loop. Clients SHOULD send `shutdown` before killing the process.

### 2.4. Ping

**Method:** `ping`

Params: none

Result: `"pong"`

For keepalive and health checks.

## 3. Error Codes

JSON-RPC 2.0 standard errors:

| Code | Meaning |
|------|---------|
| `-32700` | Parse error |
| `-32600` | Invalid request |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32603` | Internal error |

Application errors use the `-32000` to `-32099` range:

| Code | App Code | Meaning |
|------|----------|---------|
| `-32000` | `INTERNAL` | Unexpected server error |
| `-32001` | `NOT_FOUND` | Item not found |
| `-32002` | `NOT_A_REPOSITORY` | Workdir is not a git repository |
| `-32003` | `NOT_INITIALIZED` | Extension not initialized |
| `-32004` | `INVALID_ARGUMENT` | Invalid parameter value |
| `-32005` | `PERMISSION_DENIED` | Operation not permitted |
| `-32006` | `NETWORK_ERROR` | Network operation failed |
| `-32007` | `CONFLICT` | Concurrent modification conflict |
| `-32010` | `NOT_READY` | Server not yet initialized |

An error built from a library `Result[T]` carries `data.appCode` and, when the library set one, `data.details`:

```json
{"code":-32001,"message":"post not found","data":{"appCode":"NOT_FOUND"}}
```

The table above is not exhaustive. `appCode` is the library's own code string passed through, so a `-32000` response can carry `INVALID_SCOPE`, `LIST_NOT_FOUND`, `GIT_ERROR`, `NO_SBOM`, `NO_VERSION`, `SBOM_FAILED` or `READ_FAILED`. Parameter validation (`-32602`) and the search methods return no `data` at all.

## 4. Methods

Methods are namespaced as `namespace.method`. The `workdir` set during `initialize` is implicit; individual methods do not accept it.

### 4.1. Social

#### social.getPosts

Returns posts for a given scope.

Params:
- `scope` (string, required): `"timeline"`, `"repository:my"`, `"repository:workspace"`, `"repository:<url>"` or `"repository:<url>@<branch>"`, `"list:<id>"`, `"post:<ref>"`, `"thread:<ref>"`. Any other value returns `INVALID_SCOPE`
- `limit` (int): Max posts to return (0 = all)
- `types` (string[]): Filter by type: `"post"`, `"comment"`, `"repost"`, `"quote"`
- `since` (string): ISO 8601 timestamp lower bound; a value that does not parse is dropped
- `until` (string): ISO 8601 timestamp upper bound; a value that does not parse is dropped
- `includeImplicit` (boolean): Include implicit posts
- `sort` (string): Accepted and ignored; results come back newest first

Result: `Post[]`

#### social.createPost

Params:
- `content` (string, required): Post body

Result: `Post`

#### social.editPost

Params:
- `ref` (string, required): Post ref to edit
- `content` (string, required): New content

Result: `Post`

#### social.retractPost

Params:
- `ref` (string, required): Post ref to retract

Result: `true`

#### social.createComment

Params:
- `target` (string, required): Ref of post to comment on
- `content` (string, required): Comment body

Result: `Post`

#### social.createRepost

Params:
- `target` (string, required): Ref of post to repost

Result: `Post`

#### social.createQuote

Params:
- `target` (string, required): Ref of post to quote
- `content` (string, required): Quote body

Result: `Post`

#### social.getLists

Params: none

Result: `List[]`

#### social.getList

Params:
- `id` (string, required): List ID

Result: `List`

#### social.createList

Params:
- `id` (string, required): List ID (slug)
- `name` (string, required): Display name

Result: `List`

#### social.deleteList

Params:
- `id` (string, required): List ID

Result: `{}`

#### social.addToList

Params:
- `listId` (string, required): List ID
- `repoURL` (string, required): Repository URL to add
- `branch` (string): Branch (uses default if omitted)
- `allBranches` (boolean): Follow all branches (stores `branch:*`). Mutually exclusive with `branch`.

Result: `string` (added repo URL)

#### social.removeFromList

Params:
- `listId` (string, required): List ID
- `repoURL` (string, required): Repository URL to remove

Result: `{}`

#### social.getRepositories

Params:
- `scope` (string): `"list:<id>"` for one list; any other value returns every cached repository
- `limit` (int): Max repositories to return

Result: `Repository[]`

#### social.getLogs

Params:
- `scope` (string): Scope filter
- `limit` (int): Max entries
- `types` (string[]): Filter by log entry type
- `author` (string): Filter by author
- `after` (string): ISO 8601 lower bound
- `before` (string): ISO 8601 upper bound

Result: `LogEntry[]`

### 4.2. PM

#### pm.getIssues

Params:
- `repoURL` (string): Repository URL (default: workspace)
- `branch` (string): Branch
- `states` (string[]): Filter: `"open"`, `"closed"`, `"canceled"`
- `limit` (int): Max results; 0 means 1000

Result: `Issue[]`

#### pm.getIssue

Params:
- `ref` (string, required): Issue ref

Result: `Issue`

#### pm.createIssue

Params:
- `subject` (string, required): Issue title
- `body` (string): Issue description
- `state` (string): Initial state (default: `"open"`)
- `assignees` (string[]): Assignee emails
- `due` (string): ISO 8601 due date
- `milestone` (string): Milestone ref
- `sprint` (string): Sprint ref
- `parent` (string): Parent issue ref. `root` is derived from it per GITPM.md §1.7, so a client normally sends this alone.
- `root` (string): Top-level ancestor ref. Only send this to override the derivation; sending `parent` alone is the usual case.
- `labels` (Label[]): `[{"scope":"priority","value":"high"}]`

Result: `Issue`

#### pm.updateIssue

Params:
- `ref` (string, required): Issue ref
- `subject` (string): New title
- `body` (string): New description
- `state` (string): New state
- `assignees` (string[]): New assignees
- `due` (string): New due date
- `milestone` (string): New milestone ref
- `sprint` (string): New sprint ref
- `parent` (string): New parent ref. Sending it without `root` re-derives the root; sending `""` clears both.
- `root` (string): Top-level ancestor ref. Only send this to override the derivation.
- `labels` (Label[]): New labels

Result: `Issue`

#### pm.closeIssue

Params:
- `ref` (string, required): Issue ref

Result: `Issue`

#### pm.reopenIssue

Params:
- `ref` (string, required): Issue ref

Result: `Issue`

#### pm.retractIssue

Params:
- `ref` (string, required): Issue ref

Result: `true`

#### pm.getMilestones

Params:
- `repoURL` (string): Repository URL (default: workspace)
- `branch` (string): Branch
- `states` (string[]): Filter by state
- `limit` (int): Max results; 0 means 1000

Result: `Milestone[]`

#### pm.getMilestone

Params:
- `ref` (string, required): Milestone ref

Result: `Milestone`

#### pm.createMilestone

Params:
- `title` (string, required): Milestone title
- `body` (string): Description
- `state` (string): Initial state
- `due` (string): ISO 8601 due date

Result: `Milestone`

#### pm.updateMilestone

Params:
- `ref` (string, required): Milestone ref
- `title` (string): New title
- `body` (string): New description
- `state` (string): New state
- `due` (string): New due date

Result: `Milestone`

#### pm.closeMilestone / pm.reopenMilestone / pm.cancelMilestone

Params:
- `ref` (string, required): Milestone ref

Result: `Milestone`

#### pm.retractMilestone

Params:
- `ref` (string, required): Milestone ref

Result: `true`

#### pm.getMilestoneIssues

Params:
- `ref` (string, required): Milestone ref
- `states` (string[]): Filter by state

Result: `Issue[]`

#### pm.getSprints

Params:
- `repoURL` (string): Repository URL (default: workspace)
- `branch` (string): Branch
- `states` (string[]): Filter: `"planned"`, `"active"`, `"completed"`, `"canceled"`
- `limit` (int): Max results; 0 means 1000

Result: `Sprint[]`

#### pm.getSprint

Params:
- `ref` (string, required): Sprint ref

Result: `Sprint`

#### pm.createSprint

Params:
- `title` (string, required): Sprint title
- `body` (string): Description
- `state` (string): Initial state (default: `"planned"`)
- `start` (string): ISO 8601 start date
- `end` (string): ISO 8601 end date

Result: `Sprint`

#### pm.updateSprint

Params:
- `ref` (string, required): Sprint ref
- `title` (string): New title
- `body` (string): New description
- `state` (string): New state
- `start` (string): New start date
- `end` (string): New end date

Result: `Sprint`

#### pm.activateSprint / pm.completeSprint / pm.cancelSprint

Params:
- `ref` (string, required): Sprint ref

Result: `Sprint`

#### pm.retractSprint

Params:
- `ref` (string, required): Sprint ref

Result: `true`

#### pm.getSprintIssues

Params:
- `ref` (string, required): Sprint ref
- `states` (string[]): Filter by state

Result: `Issue[]`

#### pm.getBoardView

Params:
- `boardId` (string): Board ID (default: first configured board)

Result: `BoardView`

```json
{
  "id": "default",
  "name": "Default Board",
  "columns": [
    {"name": "open", "label": "Open", "wip": null, "issues": [...]},
    {"name": "closed", "label": "Done", "wip": null, "issues": [...]}
  ]
}
```

#### pm.commentOnItem

Params:
- `ref` (string, required): Item ref (issue, milestone, or sprint)
- `content` (string, required): Comment body

Result: `Post` (social comment)

#### pm.getItemComments

Params:
- `ref` (string, required): Item ref

Result: `Post[]`

#### pm.getLinks

Returns the link graph around an item: what it blocks, what blocks it, and what it relates to.

Params:
- `ref` (string, required): Item ref

Result: `{"blocks": Issue[], "blockedBy": Issue[], "related": Issue[]}`

#### pm.isBlocked

Params:
- `ref` (string, required): Item ref

Result: `bool`

### 4.3. Review

#### review.getPullRequests

Params:
- `repoURL` (string): Repository URL (default: workspace)
- `branch` (string): Branch
- `states` (string[]): Filter: `"open"`, `"merged"`, `"closed"`
- `includeForks` (bool): Include PRs from registered forks
- `limit` (int): Max results

Result: `PullRequest[]`

#### review.getPR

Params:
- `ref` (string, required): PR ref

Result: `PullRequest`

#### review.createPR

Params:
- `subject` (string, required): PR title
- `body` (string): PR description
- `base` (string): Base branch ref. A pull request created without one cannot be merged
- `head` (string): Head branch ref. A pull request created without one cannot be merged
- `closes` (string[]): Issue refs to close on merge
- `reviewers` (string[]): Reviewer emails

Result: `PullRequest`

#### review.updatePR

Params:
- `ref` (string, required): PR ref
- `subject` (string): New title
- `body` (string): New description
- `state` (string): New state
- `base` (string): New base
- `head` (string): New head
- `closes` (string[]): New close refs
- `reviewers` (string[]): New reviewers

Result: `PullRequest`

#### review.mergePR

Params:
- `ref` (string, required): PR ref

Merges fast-forward. The other strategies the CLI offers are not reachable over RPC.

Result: `PullRequest`

#### review.markReady

Takes a draft PR out of draft state.

Params:
- `ref` (string, required): PR ref

Result: `PullRequest`

#### review.convertToDraft

Puts an open PR back into draft state.

Params:
- `ref` (string, required): PR ref

Result: `PullRequest`

#### review.closePR

Params:
- `ref` (string, required): PR ref

Result: `PullRequest`

#### review.retractPR

Params:
- `ref` (string, required): PR ref

Result: `true`

#### review.getFeedbackForPR

Params:
- `ref` (string, required): PR ref

Result: `Feedback[]`

#### review.createFeedback

Params:
- `content` (string, required): Feedback body
- `pullRequest` (string, required): PR ref
- `commit` (string): Commit hash (12 chars)
- `file` (string): File path
- `oldLine` (int): Line in old file
- `newLine` (int): Line in new file
- `oldLineEnd` (int): End line in old file
- `newLineEnd` (int): End line in new file
- `reviewState` (string): `"approved"` or `"changes-requested"`
- `suggestion` (bool): Body contains ` ```suggestion ``` ` block

Result: `Feedback`

#### review.updateFeedback

Params:
- `ref` (string, required): Feedback ref
- `content` (string): New content
- `reviewState` (string): New review state

Result: `Feedback`

#### review.retractFeedback

Params:
- `ref` (string, required): Feedback ref

Result: `true`

#### review.applySuggestion

Params:
- `ref` (string, required): Feedback ref containing suggestion

Result: `string` (applied file path)

#### review.getDiff

Returns the diff between a PR's base and head.

Params:
- `ref` (string, required): PR ref

Result: `FileDiff[]`

```json
[{
  "OldPath": "a/theme.go",
  "NewPath": "b/theme.go",
  "Status": 1,
  "Binary": false,
  "Hunks": [{
    "OldStart": 10, "OldCount": 5,
    "NewStart": 10, "NewCount": 8,
    "Header": "@@ -10,5 +10,8 @@",
    "Lines": [
      {"Type": 0, "Content": "func init() {", "OldNum": 10, "NewNum": 10},
      {"Type": 2, "Content": "\told := theme()", "OldNum": 11, "NewNum": 0},
      {"Type": 1, "Content": "\tnewTheme := darkTheme()", "OldNum": 0, "NewNum": 11}
    ]
  }]
}]
```

`Type` is an integer: 0 context, 1 added, 2 removed.

#### review.getDiffStats

Params:
- `ref` (string, required): PR ref

Result: `DiffStats`

```json
{"Files": 5, "Added": 120, "Removed": 45}
```

#### review.getFileDiff

Params:
- `ref` (string, required): PR ref
- `file` (string, required): File path

Result: `FileDiff`

#### review.getFileContent

Returns file content at a specific ref.

Params:
- `ref` (string, required): PR ref
- `file` (string, required): File path
- `side` (string): `"base"` reads the base; any other value, including an absent one, reads the head

Result: `string` (file contents)

#### review.getPRComments

Params:
- `ref` (string, required): PR ref

Result: `Post[]`

#### review.getForks

Returns registered fork URLs (stored in core config, shared across all extensions).

Params: none

Result: `string[]` (fork URLs)

#### review.addFork

Registers a fork URL in the core config (shared across all extensions).

Params:
- `url` (string, required): Fork repository URL

Result: `true`

#### review.removeFork

Removes a fork URL from the core config.

Params:
- `url` (string, required): Fork repository URL

Result: `true`

#### review.updatePRTips

Re-snapshots the PR's base and head tips from the live branches.

Params:
- `ref` (string, required): PR ref

Result: `PullRequest`

#### review.syncPRBranch

Brings the head branch up to date with the base, then re-snapshots the tips.

Params:
- `ref` (string, required): PR ref
- `strategy` (string): `rebase` (default) or `merge`

Result: `PullRequest`

#### review.getPRVersions

Lists every version of the PR, oldest first.

Params:
- `ref` (string, required): PR ref

Result: `PRVersion[]`

#### review.comparePRVersions

Range-diffs two versions of the PR.

Params:
- `ref` (string, required): PR ref
- `from` (int, required): Version number to compare from
- `to` (int, required): Version number to compare to

Result: string (the range-diff)

#### review.getVersionAwareReviews

Each reviewer's latest review tagged with the version it was left against, so a
client can tell a stale approval from a current one.

Params:
- `ref` (string, required): PR ref

Result: `VersionAwareReview[]`

### 4.4. Release

#### release.getReleases

Params:
- `repoURL` (string): Repository URL (default: workspace)
- `branch` (string): Branch
- `limit` (int): Max results

Result: `Release[]`

#### release.getRelease

Params:
- `ref` (string, required): Release ref

Result: `Release`

#### release.createRelease

Params:
- `subject` (string, required): Release title
- `body` (string): Release notes
- `tag` (string): Git tag
- `version` (string): Version string
- `prerelease` (bool): Pre-release flag
- `artifacts` (string[]): Artifact names
- `artifactURL` (string): Download URL
- `checksums` (string): Checksum data
- `signedBy` (string): GPG signer
- `sbom` (string): SBOM filename (e.g., `sbom.spdx.json`)

Result: `Release`

#### release.editRelease

Params:
- `ref` (string, required): Release ref
- `subject` (string): New title
- `body` (string): New notes
- `tag` (string): New tag
- `version` (string): New version
- `prerelease` (bool): New pre-release flag
- `artifacts` (string[]): New artifacts
- `artifactURL` (string): New download URL
- `checksums` (string): New checksums
- `signedBy` (string): New signer
- `sbom` (string): New SBOM filename

Result: `Release`

#### release.retractRelease

Params:
- `ref` (string, required): Release ref

Result: `true`

#### release.getReleaseComments

Params:
- `ref` (string, required): Release ref

Result: `Post[]`

#### release.getSBOM

Returns parsed SBOM summary for a release (format, package count, licenses, generator).

Params:
- `ref` (string, required): Release ref

Result: `SBOMSummary`

#### release.getSBOMRaw

Returns the raw SBOM file content as a JSON string.

Params:
- `ref` (string, required): Release ref

Result: `string` (raw SBOM JSON content)

### 4.5. Core

#### core.fetch

Fetches updates from all subscribed repositories. Returns immediately with a fetch ID. Progress and completion are reported via server notifications.

Params:
- `listId` (string): Fetch only repositories in this list

Result:
```json
{"fetchId": "f-1"}
```

The server sends `fetch.progress` and `fetch.complete` notifications for this `fetchId` (see Section 5).

#### core.push

Pushes local changes to every default push remote, the list `gitsocial push` resolves.

Params:
- `remote` (string): One remote to push to (default: every resolved push remote)
- `allBranches` (boolean): Publish every local branch
- `noCode` (boolean): Skip code branches
- `noSite` (boolean): Skip the static site
- `siteOnly` (boolean): Rebuild the site, send no refs
- `full` (boolean): Send every object and detach a thin fork bucket
- `dryRun` (boolean): Report the plan, send nothing
- `extensions` (string[]): Accepted and ignored; every initialized extension is pushed

Result for one remote, the object below; for several, an array of them in push order.

```json
{
  "push": {"...": "per-branch push counts"},
  "site": {"published": true, "complete": true},
  "emptyBoot": false
}
```

#### core.status

Returns workspace and extension status.

Params: none

Result:
```json
{
  "workdir": "/path/to/repo",
  "repoURL": "https://github.com/user/repo",
  "extensions": {
    "social": {"initialized": true, "branch": "gitmsg/social", "unpushed": 3},
    "pm": {"initialized": true, "branch": "gitmsg/pm"},
    "review": {"initialized": false},
    "release": {"initialized": false}
  }
}
```

`branch` and `unpushed` are omitted when empty or zero. `unpushed` counts posts and lists on the extension's branch.

#### core.getConfig

Reads extension configuration.

Params:
- `extension` (string, required): Extension name

Result: `object` (extension-specific config JSON)

#### core.setConfig

Writes extension configuration.

Params:
- `extension` (string, required): Extension name
- `config` (object, required): Config object to write

Result: `true`

#### core.initExtension

Initializes an extension in the workspace.

Params:
- `extension` (string, required): Extension name
- `branch` (string): Custom branch name

Result: `true`

#### core.getNotifications

Params:
- `unreadOnly` (bool): Only unread (default: false)
- `types` (string[]): Filter by type
- `limit` (int): Max results

Result: `Notification[]`

#### core.getUnreadCount

Params: none

Result: `int`

#### core.markAsRead

Params:
- `repoURL` (string, required): Notification repo URL
- `hash` (string, required): Notification hash
- `branch` (string, required): Notification branch

Result: `true`

#### core.markAllAsRead

Params: none

Result: `true`

#### core.getHistory

Returns edit history for any item (post, issue, PR, release, etc.).

Params:
- `ref` (string, required): Item ref

Result: `MessageVersion[]`

```json
[
  {"ID": "#commit:abc123456789@gitmsg/social", "CommitHash": "abc123456789",
   "Timestamp": "2025-01-06T10:00:00Z", "AuthorName": "Alice", "AuthorEmail": "alice@example.com",
   "Extension": "social", "Type": "post", "Content": "v1", "EditOf": "", "IsRetracted": false}
]
```

#### core.getSettings

Params: none

Result: `KeyValue[]`

```json
[
  {"key": "fetch.parallel", "value": "4"},
  {"key": "log.level", "value": "info"}
]
```

#### core.setSetting

Params:
- `key` (string, required): Setting key
- `value` (string, required): Setting value

Result: `true`

### 4.6. Search

#### search / social.search

Cross-extension search (posts, issues, PRs, releases, feedback). `search` is the
real name: it spans every extension, so filing it under `social.` would
misdescribe it. `social.search` is registered as an alias for clients written
against the name this document used to give, and dispatches to the same handler.

Params:
- `query` (string): Free-text query
- `author` (string): Filter by author email
- `repo` (string): Filter by repository URL
- `type` (string): Filter by type: `post`, `comment`, `repost`, `quote`, `issue`, `milestone`, `sprint`, `pr`, `feedback`, `release`
- `hash` (string): Filter by commit-hash prefix
- `after` (string): ISO 8601 timestamp lower bound
- `before` (string): ISO 8601 timestamp upper bound
- `limit` (int): Max results (default: 20)
- `scope` (string): `timeline` (default), `list:<id>`, `repository:<url>`, `repos:<csv>`
- `sort` (string): `score` (default) or `date`

Result: `SearchResult`

```json
{
  "query": "dark mode",
  "results": [{"repo_url": "https://github.com/user/repo", "hash": "abc123456789", "branch": "gitmsg/social", "content": "dark mode toggle", "type": "post", "extension": "social", "score": 8.5}],
  "total": 3,
  "total_searched": 1240,
  "has_more": false,
  "execution_time_ms": 12
}
```

## 5. Server Notifications

Server-initiated notifications (no `id` field) pushed to the client. Clients opt in by sending `subscribe` after initialization.

### 5.1. Subscribe

**Method:** `subscribe`

Params:
- `events` (string[], required): Events to subscribe to: `"fetch"`, `"notifications"`, `"workspace"`

Result: `true`

### 5.2. Unsubscribe

**Method:** `unsubscribe`

Params:
- `events` (string[], required): Events to unsubscribe from

Result: `true`

### 5.3. Fetch Events

#### fetch.progress

```json
{"jsonrpc":"2.0","method":"fetch.progress","params":{
  "fetchId": "f-1",
  "repoURL": "https://github.com/user/repo",
  "processed": 3,
  "total": 10
}}
```

#### fetch.complete

```json
{"jsonrpc":"2.0","method":"fetch.complete","params":{
  "fetchId": "f-1",
  "repositories": 10,
  "newCommits": 42,
  "errors": 0
}}
```

#### fetch.error

Sent once when the fetch as a whole fails, not per repository.

```json
{"jsonrpc":"2.0","method":"fetch.error","params":{
  "fetchId": "f-1",
  "message": "network timeout"
}}
```

### 5.4. Notification Events

#### notifications.changed

Sent at the end of `core.fetch`. Mark-as-read and local commits send nothing; a client that changes read state updates its own count.

```json
{"jsonrpc":"2.0","method":"notifications.changed","params":{
  "unreadCount": 5
}}
```

### 5.5. Workspace Events

#### workspace.changed

Reserved. `subscribe` accepts `"workspace"`, but the server has no emitter for this notification and sends none. The payload shape below is what a client should expect once one exists.

```json
{"jsonrpc":"2.0","method":"workspace.changed","params":{
  "branches": ["gitmsg/social", "gitmsg/pm"]
}}
```

## 6. Type Reference

Types returned by methods. Most are Go structs with no JSON tags, so their field names serialize as written in Go, with a capital first letter and no omission of zero values: an unset pointer is `null`, an unset time is `"0001-01-01T00:00:00Z"`, an unset slice is `null`. The five tagged types, marked below, serialize under their tag names instead.

Times are RFC 3339 strings. Refs are strings in `#commit:hash@branch` or `url#commit:hash@branch` form. A library `Result[T]` maps to `result` or `error`.

### Author, Actor

`{"Name": "string", "Email": "string"}`. `pm.Label` is `{"Scope": "string", "Value": "string"}`. `IssueRef` and review's `Ref` are `{"RepoURL": "string", "Hash": "string", "Branch": "string"}`.

### Post

| Field | Type | Note |
|---|---|---|
| `ID` | string | ref |
| `Repository`, `Branch` | string | |
| `Author` | Author | |
| `Timestamp` | string | RFC 3339 |
| `Content`, `CleanContent` | string | raw message, and the message with the GitMsg header removed |
| `Type` | string | `post`, `comment`, `repost`, `quote` |
| `Source` | string | where the post was read from |
| `OriginalPostID`, `ParentCommentID` | string | refs, empty when absent |
| `EditOf`, `EditorName`, `EditorEmail` | string | the edit chain |
| `EditRepoURL`, `EditHash`, `EditBranch` | string | the latest edit commit |
| `IsRetracted`, `IsEdited`, `HasProposedEdits` | bool | |
| `Depth` | int | thread nesting |
| `Interactions` | object | `{"Comments": 0, "Reposts": 0, "Quotes": 0}` |
| `Remote` | string | |
| `IsVirtual`, `IsStale`, `IsWorkspacePost` | bool | |
| `Display` | object | render hints: `RepositoryName`, `CommitURL`, `IsVerified`, `Badge` and siblings |
| `OriginalExtension`, `OriginalType` | string | the referenced item's extension and type |
| `HeaderExt`, `HeaderType`, `HeaderState` | string | raw GitMsg header fields |
| `Labels` | string[] | |
| `Origin` | object | import provenance, `null` when the post is native |
| `Raw` | object | `{"Commit": {...}, "GitMsg": {...}}`, the parsed commit and message |

### Issue

| Field | Type | Note |
|---|---|---|
| `ID`, `Repository`, `Branch` | string | |
| `Author` | Author | |
| `Timestamp` | string | RFC 3339 |
| `Subject`, `Body` | string | |
| `State` | string | `open`, `closed`, `canceled` |
| `Assignees` | string[] | emails |
| `Due` | string | RFC 3339, `null` when unset |
| `Milestone`, `Sprint`, `Parent`, `Root` | IssueRef | `null` when unset |
| `Blocks`, `BlockedBy`, `Related` | IssueRef[] | |
| `Labels` | Label[] | |
| `IsEdited`, `HasProposedEdits`, `IsRetracted`, `IsUnpushed` | bool | |
| `Comments` | int | |
| `Origin` | object | `null` when native |

### Milestone

`ID`, `Repository`, `Branch`, `Author`, `Timestamp`, `Title`, `Body`, `State`, `Due`, `Labels` (string[]), `IsEdited`, `HasProposedEdits`, `IsRetracted`, `IsUnpushed`, `IssueCount`, `ClosedCount`, `Origin`.

### Sprint

The Milestone fields, with `Start` and `End` (RFC 3339) in place of `Due`, and `State` one of `planned`, `active`, `completed`, `canceled`.

### PullRequest

| Field | Type | Note |
|---|---|---|
| `ID`, `Repository`, `Branch` | string | |
| `Author` | Author | |
| `Timestamp` | string | RFC 3339 |
| `Subject`, `Body` | string | |
| `State` | string | `open`, `merged`, `closed` |
| `IsDraft` | bool | |
| `Base`, `BaseTip`, `Head`, `HeadTip` | string | refs and the tips recorded at the latest version |
| `DependsOn`, `Closes`, `Reviewers`, `Labels` | string[] | |
| `IsEdited`, `HasProposedEdits`, `IsRetracted`, `IsUnpushed` | bool | |
| `Comments` | int | |
| `ReviewSummary` | object | `{"Approved": 0, "ChangesRequested": 0, "Pending": 0, "IsBlocked": false, "IsApproved": false}` |
| `MergeBase`, `MergeHead` | string | recorded before the merge |
| `MergedBy`, `ClosedBy`, `OriginalAuthor` | Author | `null` when unset |
| `MergedAt`, `ClosedAt`, `OriginalTime` | string | RFC 3339; the zero time when unset |
| `Origin` | object | `null` when native |

### Feedback

`ID`, `Repository`, `Branch`, `Author`, `Timestamp`, `Content`, `PullRequest` (Ref), `Commit`, `File`, `OldLine`, `NewLine`, `OldLineEnd`, `NewLineEnd`, `ReviewState` (`comment`, `approved`, `changes-requested`), `Suggestion`, `IsEdited`, `IsRetracted`, `Comments`.

### PRVersion

Tagged type.

```json
{
  "number": 0,
  "label": "string",
  "commit_hash": "string",
  "repo_url": "string (URL)",
  "branch": "string",
  "author_name": "string",
  "author_email": "string",
  "timestamp": "string (ISO 8601)",
  "subject": "string (optional)",
  "body": "string (optional)",
  "base_tip": "string (optional)",
  "head_tip": "string (optional)",
  "state": "open | merged | closed",
  "is_retracted": false
}
```

### VersionAwareReview

Tagged type.

```json
{
  "reviewer_name": "string",
  "reviewer_email": "string",
  "state": "approved | changes-requested",
  "reviewed_at": "string (ISO 8601)",
  "reviewed_version": 0,
  "reviewed_label": "string",
  "current_version": 0,
  "current_label": "string",
  "head_changed": false,
  "code_changed": false,
  "stale": false
}
```

### Release

`ID`, `Repository`, `Branch`, `Author`, `Timestamp`, `Subject`, `Body`, `Version`, `Tag`, `Prerelease`, `Artifacts` (string[]), `ArtifactURL`, `Checksums`, `SignedBy`, `SBOM`, `Labels` (string[]), `IsEdited`, `HasProposedEdits`, `IsRetracted`, `IsUnpushed`, `Comments`, `Origin`.

### SBOMSummary

Tagged type. Every field is always present.

```json
{
  "format": "spdx | cyclonedx | syft",
  "packages": 127,
  "generator": "string, e.g. syft-1.0.0",
  "licenses": {"MIT": 42, "Apache-2.0": 15},
  "generated": "string (ISO 8601)",
  "items": [
    {"name": "string", "version": "string", "license": "string"}
  ]
}
```

### Notification

`RepoURL`, `Hash`, `Branch`, `Type` ([NOTIFICATIONS.md](NOTIFICATIONS.md#types)), `Source` (`social`, `pm`, `review`, `release`, `memo`, `core`), `Item` (the extension's own notification object), `Actor`, `ActorRepo`, `Timestamp`, `IsRead`.

### MessageVersion

`ID`, `CommitHash`, `Branch`, `RepoURL`, `AuthorName`, `AuthorEmail`, `Timestamp`, `Extension`, `Type`, `Content`, `EditOf`, `IsRetracted`, `Labels` (string[]), `Fields` (the parsed GitMsg header, key to value).

### FileDiff, Hunk, DiffLine, DiffStats

| Type | Fields |
|---|---|
| `FileDiff` | `OldPath`, `NewPath`, `Status` (int), `Hunks`, `Binary` |
| `Hunk` | `OldStart`, `OldCount`, `NewStart`, `NewCount`, `Header`, `Lines` |
| `DiffLine` | `Type` (int: 0 context, 1 added, 2 removed), `Content`, `OldNum`, `NewNum` |
| `DiffStats` | `Files`, `Added`, `Removed` |

### search.Result

Tagged type; see the [search](#search--socialsearch) method for the shape.

## 7. Implementation Notes

- The read loop is single-threaded: one request is dispatched to completion before the next line is read, and a batch runs its entries in order. `core.fetch` is the exception; it starts a goroutine, returns a `fetchId` at once and reports through notifications.
- A server serves the workspace given at `initialize`. For another workspace, shut down and spawn a new server; multi-root editors run one per workspace.
- Serialization rules are in [Section 6](#6-type-reference).
- `-32003 NOT_INITIALIZED` is defined but no method raises it today. Read the `initialize` response to learn which extensions are available.
- Handlers live in `library/rpc/methods_*.go`, one file per namespace; each unmarshals its params, calls the extension API and returns the result. No business logic lives in the RPC layer.
