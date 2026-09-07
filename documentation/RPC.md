# GitSocial JSON-RPC Protocol

`gitsocial rpc` serves the library over JSON-RPC 2.0 on stdio, for editors and other clients.

[Transport](#1-transport) · [Lifecycle](#2-lifecycle) · [Error codes](#3-error-codes) · [Methods](#4-methods) · [Notifications](#5-server-notifications) · [Types](#6-type-reference) · [Notes](#7-implementation-notes)

## 1. Transport

Communication uses JSON-RPC 2.0 over stdio (stdin/stdout). Each message is a single line of JSON terminated by `\n`. Stderr is reserved for logging.

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

Clients MAY send JSON-RPC batch requests (array of request objects). The server MUST respond with a batch response in the same order.

## 2. Lifecycle

### 2.1. Startup

The client spawns `gitsocial rpc` as a subprocess. The server reads from stdin and writes to stdout. The server MUST NOT produce output before receiving `initialize`.

### 2.2. Initialize

The first request MUST be `initialize`. The server opens the cache, resolves the workspace, and returns server capabilities.

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

The server closes the cache, flushes pending writes, and exits with code 0. Clients SHOULD send `shutdown` before killing the process.

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

Error responses include the application code in `data.appCode` for programmatic handling:

```json
{"code":-32001,"message":"post not found","data":{"appCode":"NOT_FOUND"}}
```

## 4. Methods

Methods are namespaced as `namespace.method`. The `workdir` set during `initialize` is implicit; individual methods do not accept it.

### 4.1. Social

#### social.getPosts

Returns posts for a given scope.

Params:
- `scope` (string, required): `"timeline"`, `"workspace"`, `"mine"`, `"repo:<url>"`, `"list:<id>"`, `"post:<ref>"`, `"thread:<ref>"`
- `limit` (int): Max posts to return (0 = all)
- `types` (string[]): Filter by type: `"post"`, `"comment"`, `"repost"`, `"quote"`
- `since` (string): ISO 8601 timestamp lower bound
- `until` (string): ISO 8601 timestamp upper bound
- `sort` (string): Sort order: `"newest"` (default), `"oldest"`

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

Result: `true`

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

Result: `true`

#### social.getRepositories

Params:
- `scope` (string): `"all"` (default), `"list:<id>"`

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
- `limit` (int): Max results

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
- `limit` (int): Max results

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
- `limit` (int): Max results

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
- `base` (string, required): Base branch ref
- `head` (string, required): Head branch ref
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
- `strategy` (string): Merge strategy: `ff` (default), `squash`, `rebase`, `merge`

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
  "oldFile": "a/theme.go",
  "newFile": "b/theme.go",
  "hunks": [{
    "oldStart": 10, "oldCount": 5,
    "newStart": 10, "newCount": 8,
    "lines": [
      {"type": "context", "content": "func init() {", "oldLine": 10, "newLine": 10},
      {"type": "delete", "content": "\told := theme()", "oldLine": 11},
      {"type": "add", "content": "\tnewTheme := darkTheme()", "newLine": 11}
    ]
  }]
}]
```

#### review.getDiffStats

Params:
- `ref` (string, required): PR ref

Result: `DiffStats`

```json
{"filesChanged": 5, "insertions": 120, "deletions": 45}
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
- `side` (string, required): `"base"` or `"head"`

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

Pushes local changes to the remote.

Params:
- `extensions` (string[]): Extensions to push (default: all initialized)

Result:
```json
{"pushed": ["social", "pm"]}
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
    "pm": {"initialized": true, "branch": "gitmsg/pm", "unpushed": 0},
    "review": {"initialized": false},
    "release": {"initialized": false}
  }
}
```

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
  {"hash": "abc123456789", "timestamp": "2025-01-06T10:00:00Z", "author": {...}, "content": "v1"},
  {"hash": "def234567890", "timestamp": "2025-01-06T11:00:00Z", "author": {...}, "content": "v2 (edited)"}
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
  "repository": "https://github.com/user/repo",
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

```json
{"jsonrpc":"2.0","method":"fetch.error","params":{
  "fetchId": "f-1",
  "repository": "https://github.com/user/repo",
  "message": "network timeout"
}}
```

### 5.4. Notification Events

#### notifications.changed

Sent when the unread notification count changes (after fetch, after new local commits, or after mark-as-read).

```json
{"jsonrpc":"2.0","method":"notifications.changed","params":{
  "unreadCount": 5
}}
```

### 5.5. Workspace Events

#### workspace.changed

Sent when the server detects changes to gitmsg branches in the workspace (via filesystem watch on `.git/refs/heads/gitmsg/`).

```json
{"jsonrpc":"2.0","method":"workspace.changed","params":{
  "branches": ["gitmsg/social", "gitmsg/pm"]
}}
```

## 6. Type Reference

Types returned by methods. JSON field names use camelCase. Null/absent fields are omitted.

### Post

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "content": "string",
  "type": "post | comment | repost | quote",
  "interactions": {"comments": 0, "reposts": 0, "quotes": 0},
  "originalPostId": "string (ref, optional)",
  "parentCommentId": "string (ref, optional)",
  "isEdited": false,
  "isRetracted": false,
  "isVirtual": false
}
```

### Issue

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "subject": "string",
  "body": "string",
  "state": "open | closed | canceled",
  "assignees": ["string (email)"],
  "due": "string (ISO 8601, optional)",
  "milestone": {"repoURL": "string", "hash": "string", "branch": "string"},
  "sprint": {"repoURL": "string", "hash": "string", "branch": "string"},
  "labels": [{"scope": "string", "value": "string"}],
  "isEdited": false,
  "isRetracted": false,
  "comments": 0
}
```

### Milestone

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "title": "string",
  "body": "string",
  "state": "open | closed | canceled",
  "due": "string (ISO 8601, optional)",
  "isEdited": false,
  "isRetracted": false,
  "issueCount": 0,
  "closedCount": 0
}
```

### Sprint

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "title": "string",
  "body": "string",
  "state": "planned | active | completed | canceled",
  "start": "string (ISO 8601)",
  "end": "string (ISO 8601)",
  "isEdited": false,
  "isRetracted": false,
  "issueCount": 0,
  "closedCount": 0
}
```

### PullRequest

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "subject": "string",
  "body": "string",
  "state": "open | merged | closed",
  "base": "string (branch ref)",
  "baseTip": "string (12-char hash, base branch tip at creation/update)",
  "head": "string (branch ref)",
  "headTip": "string (12-char hash, head branch tip at creation/update)",
  "closes": ["string (issue ref)"],
  "reviewers": ["string (email)"],
  "labels": ["string"],
  "isEdited": false,
  "isRetracted": false,
  "comments": 0,
  "reviewSummary": {
    "approved": 0,
    "changesRequested": 0,
    "pending": 0,
    "isBlocked": false,
    "isApproved": false
  },
  "mergeBase": "string (12-char hash, merge-base at merge time, optional)",
  "mergeHead": "string (12-char hash, head at merge time, optional)",
  "mergedBy": {"name": "string", "email": "string"} | null,
  "mergedAt": "string (ISO 8601)" | null,
  "closedBy": {"name": "string", "email": "string"} | null,
  "closedAt": "string (ISO 8601)" | null,
  "originalAuthor": {"name": "string", "email": "string"} | null
}
```

### Feedback

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "content": "string",
  "pullRequest": {"repoURL": "string", "hash": "string", "branch": "string"},
  "commit": "string (optional)",
  "file": "string (optional)",
  "oldLine": 0,
  "newLine": 0,
  "reviewState": "approved | changes-requested (optional)",
  "suggestion": false,
  "isEdited": false,
  "isRetracted": false,
  "comments": 0
}
```

### PRVersion

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

```json
{
  "id": "string (ref)",
  "repository": "string (URL)",
  "branch": "string",
  "author": {"name": "string", "email": "string"},
  "timestamp": "string (ISO 8601)",
  "subject": "string",
  "body": "string",
  "version": "string",
  "tag": "string",
  "prerelease": false,
  "artifacts": ["string"],
  "artifactURL": "string (optional)",
  "checksums": "string (optional)",
  "signedBy": "string (optional)",
  "sbom": "string (optional, e.g. sbom.spdx.json)",
  "isEdited": false,
  "isRetracted": false,
  "comments": 0
}
```

### SBOMSummary

```json
{
  "format": "spdx | cyclonedx | syft",
  "packages": 127,
  "generator": "string (optional, e.g. syft-1.0.0)",
  "licenses": {"MIT": 42, "Apache-2.0": 15},
  "generated": "string (ISO 8601, optional)",
  "items": [
    {"name": "string", "version": "string", "license": "string"}
  ]
}
```

### Notification

```json
{
  "repoURL": "string",
  "hash": "string",
  "branch": "string",
  "type": "string",
  "source": "social | pm | review | core",
  "actor": {"name": "string", "email": "string"},
  "actorRepo": "string (optional)",
  "timestamp": "string (ISO 8601)",
  "isRead": false
}
```

## 7. Implementation Notes

- Requests run concurrently. `core.fetch` returns at once and reports through notifications; reads never wait on writes. The cache serializes database access, and the server adds no locking of its own.
- A server serves the workspace given at `initialize`. For another workspace, shut down and spawn a new server; multi-root editors run one per workspace.
- Times serialize as ISO 8601 strings, nil pointers are omitted, refs are strings in `#commit:hash@branch` or `url#commit:hash@branch` form, and a `Result[T]` maps to `result` or `error`.
- Methods of an extension that is not initialized return `-32003 NOT_INITIALIZED`; the `initialize` response says which extensions are available.
- Handlers live in `library/rpc/methods_*.go`, one file per namespace; each unmarshals its params, calls the extension API and returns the result. No business logic lives in the RPC layer.
