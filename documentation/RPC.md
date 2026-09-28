# GitSocial JSON-RPC Protocol

`gitsocial rpc` serves the library over JSON-RPC 2.0 on stdio, for editors and other clients.

[Transport](#1-transport) · [Lifecycle](#2-lifecycle) · [Error codes](#3-error-codes) · [Methods](#4-methods) · [Notifications](#5-server-notifications) · [Types](#6-type-reference) · [Notes](#7-implementation-notes)

## 1. Transport

Communication uses JSON-RPC 2.0 over stdio (stdin/stdout). Each message is a single line of JSON terminated by `\n`. Stderr is reserved for logging. A line over 1 MB ends the session with no error response.

### 1.1. Message Format

Requests and responses follow JSON-RPC 2.0. All messages must be valid JSON on a single line.

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

Clients may send JSON-RPC batch requests (an array of request objects). The server must send a batch response in the same order. An empty array returns a single `-32600` error.

## 2. Lifecycle

### 2.1. Startup

The client starts `gitsocial rpc` as a subprocess. The server reads from stdin and writes to stdout, and must not write output before it receives `initialize`.

### 2.2. Initialize

The first request must be `initialize`; only `ping`, `subscribe`, `unsubscribe` and `shutdown` work before it. The server opens the cache, resolves the workspace, and returns server capabilities. A second `initialize` returns `-32007 CONFLICT`, and a missing `workdir` returns `-32602`.

**Method:** `initialize`

Params:
- `workdir` (string, required): Absolute path to the git repository working directory
- `cacheDir` (string): Cache directory (default: `~/.cache/gitsocial`)
- `clientName` (string): Client identifier, for example `"vscode"` or `"neovim"`
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

The server closes the cache and stops the read loop. Clients should send `shutdown` before they stop the process.

### 2.4. Ping

**Method:** `ping`

Params: none

Result: `"pong"`

Clients use `ping` for keepalive and health checks.

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
| `-32007` | `CONFLICT` | Concurrent modification conflict |
| `-32010` | `NOT_READY` | Server not yet initialized |

An error built from a library `Result[T]` carries `data.appCode` and, when the library set one, `data.details`:

```json
{"code":-32001,"message":"post not found","data":{"appCode":"NOT_FOUND"}}
```

`data.details` holds an error as its text, and a struct or a map as its JSON object.

The table above is not exhaustive. `appCode` is the code string from the library, without change, so a `-32000` response can carry `INVALID_SCOPE`, `LIST_NOT_FOUND`, `GIT_ERROR`, `NO_SBOM`, `NO_VERSION`, `SBOM_FAILED` or `READ_FAILED`. Parameter validation (`-32602`) and the search methods return no `data` at all.

## 4. Methods

Methods are namespaced as `namespace.method`. All methods use the `workdir` from `initialize`, and no method accepts a `workdir` param.

### 4.1. Social

#### social.getPosts

Returns posts for a given scope.

Params:
- `scope` (string, required): `"timeline"`, `"repository:my"`, `"repository:workspace"`, `"repository:<url>"` or `"repository:<url>@<branch>"`, `"list:<id>"`, `"post:<ref>"`, `"thread:<ref>"`. Any other value returns `INVALID_SCOPE`
- `limit` (int): Max posts to return (0 = all)
- `types` (string[]): Filter by type: `"post"`, `"comment"`, `"repost"`, `"quote"`
- `since` (string): RFC 3339 timestamp lower bound; the server ignores a value that does not parse
- `until` (string): RFC 3339 timestamp upper bound; the server ignores a value that does not parse
- `includeImplicit` (boolean): Include implicit posts

Posts come back newest first. A param this list does not name is ignored.

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

Result: `string` (the added repository URL)

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
- `after` (string): RFC 3339 timestamp lower bound
- `before` (string): RFC 3339 timestamp upper bound

Result: `LogEntry[]`

### 4.2. PM

#### pm.getIssues

Params:
- `repoURL` (string): Repository URL (default: the workspace and its registered forks)
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
- `due` (string): RFC 3339 due date
- `milestone` (string): Milestone ref
- `sprint` (string): Sprint ref
- `parent` (string): Parent issue ref. The server derives `root` from it (GITPM.md §1.7), so usually a client sends only `parent`.
- `root` (string): Top-level ancestor ref. Send it only to override the derivation.
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
- `parent` (string): New parent ref. Without `root`, the server derives the root again; the value `""` clears both.
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

#### pm.adoptIssue

Params:
- `ref` (string, required): Ref of a registered fork's issue

Result: `Issue`, the workspace copy; an issue adopted before returns its copy

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
- `due` (string): RFC 3339 due date

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
- `start` (string): RFC 3339 start date
- `end` (string): RFC 3339 end date

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
- `includeForks` (bool): Include pull requests from registered forks
- `limit` (int): Max results; 0 means 1000

Result: `PullRequest[]`

#### review.getPR

Params:
- `ref` (string, required): Pull request ref

Result: `PullRequest`

#### review.createPR

Params:
- `subject` (string, required): Pull request title
- `body` (string): Pull request description
- `base` (string): Base branch ref. A pull request created without one cannot be merged
- `head` (string): Head branch ref. A pull request created without one cannot be merged
- `closes` (string[]): Issue refs to close on merge
- `reviewers` (string[]): Reviewer emails

Result: `PullRequest`

#### review.updatePR

Params:
- `ref` (string, required): Pull request ref
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
- `ref` (string, required): Pull request ref

Merges fast-forward. The other strategies the CLI offers are not reachable over RPC.

Result: `PullRequest`

#### review.markReady

Takes a draft pull request out of draft state.

Params:
- `ref` (string, required): Pull request ref

Result: `PullRequest`

#### review.convertToDraft

Puts an open pull request back into draft state.

Params:
- `ref` (string, required): Pull request ref

Result: `PullRequest`

#### review.closePR

Params:
- `ref` (string, required): Pull request ref

Result: `PullRequest`

#### review.adoptPR

Params:
- `ref` (string, required): Ref of a registered fork's pull request to this repository

Result: `PullRequest`, the workspace copy; a pull request adopted before returns its copy

#### review.retractPR

Params:
- `ref` (string, required): Pull request ref

Result: `true`

#### review.getFeedbackForPR

Params:
- `ref` (string, required): Pull request ref

Result: `Feedback[]`

#### review.createFeedback

Params:
- `content` (string, required): Feedback body
- `pullRequest` (string, required): Pull request ref
- `commit` (string): Commit hash (12 characters)
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

Returns the diff between the base and the head of a pull request.

Params:
- `ref` (string, required): Pull request ref

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
- `ref` (string, required): Pull request ref

Result: `DiffStats`

```json
{"Files": 5, "Added": 120, "Removed": 45}
```

#### review.getFileDiff

Params:
- `ref` (string, required): Pull request ref
- `file` (string, required): File path

Result: `FileDiff`

#### review.getFileContent

Returns file content at a specific ref.

Params:
- `ref` (string, required): Pull request ref
- `file` (string, required): File path
- `side` (string): `"base"` reads the base; any other value, including an absent one, reads the head

Result: `string` (file contents)

#### review.getPRComments

Params:
- `ref` (string, required): Pull request ref

Result: `Post[]`

#### review.getForks

Returns the registered fork URLs. Each fork is at a ref under `refs/gitmsg/core/forks/`, and all extensions share the list.

Params: none

Result: `string[]` (fork URLs)

#### review.addFork

Registers a fork URL at a ref under `refs/gitmsg/core/forks/`.

Params:
- `url` (string, required): Fork repository URL

Result: `true`

#### review.removeFork

Removes the ref of a registered fork URL. A URL that is not registered returns `NOT_FOUND`.

Params:
- `url` (string, required): Fork repository URL

Result: `true`

#### review.updatePRTips

Records the base and head tips of the pull request again, from the current branches.

Params:
- `ref` (string, required): Pull request ref

Result: `PullRequest`

#### review.syncPRBranch

Rebases or merges the head branch onto the base, then records the tips again.

Params:
- `ref` (string, required): Pull request ref
- `strategy` (string): `rebase` (default) or `merge`

Result: `PullRequest`

#### review.getPRVersions

Lists all versions of the pull request, oldest first.

Params:
- `ref` (string, required): Pull request ref

Result: `PRVersion[]`

#### review.comparePRVersions

Returns the range-diff of two versions of the pull request.

Params:
- `ref` (string, required): Pull request ref
- `from` (int, required): Version number to compare from
- `to` (int, required): Version number to compare to

Result: string (the range-diff)

#### review.getVersionAwareReviews

Returns the latest review of each reviewer, with the pull request version that it reviewed and the current version.

Params:
- `ref` (string, required): Pull request ref

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
- `sbom` (string): SBOM filename, for example `sbom.spdx.json`

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

Returns the parsed SBOM summary of a release: format, package count, licenses and generator.

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

Fetches updates from the followed repositories and returns a fetch ID immediately. Server notifications report the progress and the completion.

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
- `allRemotes` (boolean): Push to every configured remote in alphabetical order. Mutually exclusive with `remote`
- `noCode` (boolean): Skip code branches
- `noSite` (boolean): Skip the static site
- `siteOnly` (boolean): Rebuild the site, send no refs
- `full` (boolean): Send every object and detach a thin fork bucket
- `dryRun` (boolean): Report the plan, send nothing
- `extensions` (string[]): Ignored; the server pushes all initialized extensions

Result: for one remote, the object below; for more than one remote, an array of these objects in push order.

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
- `branch` (string): Custom branch name, honored for `social` only

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
- `repoURL` (string, required): Notification repository URL
- `hash` (string, required): Notification hash
- `branch` (string, required): Notification branch

Result: `true`

#### core.markAllAsRead

Params: none

Result: `true`

#### core.getHistory

Returns the edit history of an item, for example a post, an issue, a pull request or a release.

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
  {"key": "fetch.parallel", "value": "4", "description": "Concurrent fetch workers."},
  {"key": "log.level", "value": "info", "description": "Logging verbosity."}
]
```

`description` is the description of the key in the registry; the server omits it for a key that is not in the registry.

#### core.setSetting

Params:
- `key` (string, required): Setting key
- `value` (string, required): Setting value

Result: `true`

### 4.6. Search

#### search

Searches all extensions: posts, issues, pull requests, releases and feedback.

Params:
- `query` (string): Free-text query
- `author` (string): Filter by author email
- `repo` (string): Filter by repository URL
- `type` (string): Filter by type: `post`, `comment`, `repost`, `quote`, `issue`, `milestone`, `sprint`, `pr`, `feedback`, `release`
- `hash` (string): Filter by commit-hash prefix
- `after` (string): RFC 3339 timestamp lower bound
- `before` (string): RFC 3339 timestamp upper bound
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

The server sends notifications (with no `id` field) to the client. A client opts in with `subscribe`, which works before `initialize`.

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

Sent at the end of `core.fetch`. Mark-as-read and local commits send nothing; a client that changes the read state updates its own count.

```json
{"jsonrpc":"2.0","method":"notifications.changed","params":{
  "unreadCount": 5
}}
```

### 5.5. Workspace Events

#### workspace.changed

Reserved. `subscribe` accepts `"workspace"`, but the server has no emitter for this notification and sends none. A client should expect the payload shape below when an emitter exists.

```json
{"jsonrpc":"2.0","method":"workspace.changed","params":{
  "branches": ["gitmsg/social", "gitmsg/pm"]
}}
```

## 6. Type Reference

The methods return the types below. Most are Go structs with no JSON tags, so their field names serialize as written in Go, with a capital first letter. Zero values are not omitted: an unset pointer is `null`, an unset time is `"0001-01-01T00:00:00Z"`, an unset slice is `null`. The five tagged types, marked below, serialize under their tag names.

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
| `Adopts` | string | the fork issue this copy adopts, empty when native |
| `OriginalAuthor` | Author | the adopted issue's author, `null` when native |
| `OriginalTime` | string | RFC 3339, the adopted issue's time |

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
| `Adopts` | string | the fork pull request this copy adopts, empty when native |

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
  "timestamp": "string (RFC 3339)",
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
  "reviewed_at": "string (RFC 3339)",
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
  "generated": "string (the SBOM's own creation time)",
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

Tagged type. The [search](#search) method shows the shape.

## 7. Implementation Notes

- The read loop is single-threaded: the server runs one request to completion before it reads the next line, and runs the entries of a batch in order. `core.fetch` is the exception: it starts a goroutine, returns a `fetchId` immediately and reports through notifications.
- A server serves the workspace given at `initialize`. For a different workspace, shut down the server and start a new one; a multi-root editor runs one server for each workspace.
- Serialization rules are in [Section 6](#6-type-reference).
- `-32003 NOT_INITIALIZED` is defined, but no method returns it. Read the `initialize` response to learn which extensions are available.
- Handlers are in `library/rpc/methods_*.go`, one file for each namespace. Each handler unmarshals its params, calls the extension API and returns the result; the RPC layer has no business logic.
