# GitSocial JSON-RPC Protocol

JSON-RPC 2.0 interface for editor and client integration. Launched via `gitsocial rpc`.

## Table of Contents

- [1. Transport](#1-transport)
  - [1.1. Message Format](#11-message-format)
  - [1.2. Batching](#12-batching)
- [2. Lifecycle](#2-lifecycle)
  - [2.1. Startup](#21-startup)
  - [2.2. Initialize](#22-initialize)
  - [2.3. Shutdown](#23-shutdown)
  - [2.4. Ping](#24-ping)
- [3. Error Codes](#3-error-codes)
- [4. Methods](#4-methods)
  - [4.1. Social](#41-social)
  - [4.2. PM](#42-pm)
  - [4.3. Review](#43-review)
  - [4.4. Release](#44-release)
  - [4.5. Core](#45-core)
- [5. Server Notifications](#5-server-notifications)
  - [5.1. Subscribe](#51-subscribe)
  - [5.2. Unsubscribe](#52-unsubscribe)
  - [5.3. Fetch Events](#53-fetch-events)
  - [5.4. Notification Events](#54-notification-events)
  - [5.5. Workspace Events](#55-workspace-events)
- [6. Type Reference](#6-type-reference)
- [7. Implementation Notes](#7-implementation-notes)
  - [7.1. Concurrency](#71-concurrency)
  - [7.2. Workspace Scope](#72-workspace-scope)
  - [7.3. Serialization](#73-serialization)
  - [7.4. Extension Registration](#74-extension-registration)
  - [7.5. Package Structure](#75-package-structure)

---

## 1. Transport

Communication uses JSON-RPC 2.0 over stdio (stdin/stdout). Each message is a single line of JSON terminated by `\n`. Stderr is reserved for logging.

```
Client (editor)                    gitsocial rpc
     │                                    │
     │ ── request (stdin) ──────────────► │
     │                                    │
     │ ◄── response (stdout) ─────────── │
     │                                    │
     │ ◄── notification (stdout) ──────── │  (server-initiated, no id)
```

### 1.1. Message Format

Requests and responses follow JSON-RPC 2.0. All messages MUST be valid JSON on a single line.

**Request:**
```json
{"jsonrpc":"2.0","id":1,"method":"social.getPosts","params":{"scope":"timeline","limit":50}}
```

**Success response:**
```json
{"jsonrpc":"2.0","id":1,"result":[...]}
```

**Error response:**
```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"not found","data":{"appCode":"NOT_FOUND"}}}
```

**Server notification (no id):**
```json
{"jsonrpc":"2.0","method":"notifications.changed","params":{"unreadCount":3}}
```

### 1.2. Batching

Clients MAY send JSON-RPC batch requests (array of request objects). The server MUST respond with a batch response in the same order.

---

## 2. Lifecycle

### 2.1. Startup

The client spawns `gitsocial rpc` as a subprocess. The server reads from stdin and writes to stdout. The server MUST NOT produce output before receiving `initialize`.

### 2.2. Initialize

The first request MUST be `initialize`. The server opens the cache, resolves the workspace, and returns server capabilities.

**Method:** `initialize`

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `workdir` | string | yes | Absolute path to the git repository working directory |
| `cacheDir` | string | no | Cache directory (default: `~/.cache/gitsocial`) |
| `clientName` | string | no | Client identifier (e.g., `"vscode"`, `"neovim"`) |
| `clientVersion` | string | no | Client version |

**Result:**
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

**Params:** none

The server closes the cache, flushes pending writes, and exits with code 0. Clients SHOULD send `shutdown` before killing the process.

### 2.4. Ping

**Method:** `ping`

**Params:** none

**Result:** `"pong"`

For keepalive and health checks.

---

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

---

## 4. Methods

Methods are namespaced as `namespace.method`. The `workdir` set during `initialize` is implicit — individual methods do not accept it.

### 4.1. Social

#### social.getPosts

Returns posts for a given scope.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | yes | `"timeline"`, `"workspace"`, `"mine"`, `"repo:<url>"`, `"list:<id>"`, `"post:<ref>"`, `"thread:<ref>"` |
| `limit` | int | no | Max posts to return (0 = all) |
| `types` | string[] | no | Filter by type: `"post"`, `"comment"`, `"repost"`, `"quote"` |
| `since` | string | no | ISO 8601 timestamp lower bound |
| `until` | string | no | ISO 8601 timestamp upper bound |
| `sort` | string | no | Sort order: `"newest"` (default), `"oldest"` |

**Result:** `Post[]`

```json
[{
  "id": "#commit:abc123456789@gitmsg/social",
  "repository": "https://github.com/user/repo",
  "branch": "gitmsg/social",
  "author": {"name": "Alice", "email": "alice@example.com"},
  "timestamp": "2025-01-06T10:00:00Z",
  "content": "Hello world",
  "type": "post",
  "interactions": {"comments": 2, "reposts": 1, "quotes": 0},
  "isEdited": false,
  "isRetracted": false
}]
```

#### social.createPost

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | yes | Post body |

**Result:** `Post`

#### social.editPost

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Post ref to edit |
| `content` | string | yes | New content |

**Result:** `Post`

#### social.retractPost

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Post ref to retract |

**Result:** `true`

#### social.createComment

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | string | yes | Ref of post to comment on |
| `content` | string | yes | Comment body |

**Result:** `Post`

#### social.createRepost

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | string | yes | Ref of post to repost |

**Result:** `Post`

#### social.createQuote

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | string | yes | Ref of post to quote |
| `content` | string | yes | Quote body |

**Result:** `Post`

#### social.search

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | Search query (supports `author:`, `repo:`, `type:` filters) |
| `limit` | int | no | Max results (default: 20) |
| `scope` | string | no | Restrict search scope |

**Result:** `SearchResult`

```json
{
  "query": "dark mode",
  "results": [{"post": {...}, "score": 0.95}],
  "total": 3,
  "hasMore": false
}
```

#### social.getLists

**Params:** none

**Result:** `List[]`

#### social.getList

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | List ID |

**Result:** `List`

#### social.createList

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | List ID (slug) |
| `name` | string | yes | Display name |

**Result:** `List`

#### social.deleteList

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | List ID |

**Result:** `true`

#### social.addToList

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `listId` | string | yes | List ID |
| `repoURL` | string | yes | Repository URL to add |
| `branch` | string | no | Branch (uses default if omitted) |
| `allBranches` | boolean | no | Follow all branches (stores `branch:*`). Mutually exclusive with `branch`. |

**Result:** `string` (added repo URL)

#### social.removeFromList

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `listId` | string | yes | List ID |
| `repoURL` | string | yes | Repository URL to remove |

**Result:** `true`

#### social.getRepositories

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | no | `"all"` (default), `"list:<id>"` |

**Result:** `Repository[]`

#### social.getLogs

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | no | Scope filter |
| `limit` | int | no | Max entries |
| `types` | string[] | no | Filter by log entry type |
| `author` | string | no | Filter by author |
| `after` | string | no | ISO 8601 lower bound |
| `before` | string | no | ISO 8601 upper bound |

**Result:** `LogEntry[]`

---

### 4.2. PM

#### pm.getIssues

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | no | Repository URL (default: workspace) |
| `branch` | string | no | Branch |
| `states` | string[] | no | Filter: `"open"`, `"closed"`, `"canceled"` |
| `limit` | int | no | Max results |

**Result:** `Issue[]`

#### pm.getIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Issue ref |

**Result:** `Issue`

#### pm.createIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `subject` | string | yes | Issue title |
| `body` | string | no | Issue description |
| `state` | string | no | Initial state (default: `"open"`) |
| `assignees` | string[] | no | Assignee emails |
| `due` | string | no | ISO 8601 due date |
| `milestone` | string | no | Milestone ref |
| `sprint` | string | no | Sprint ref |
| `parent` | string | no | Parent issue ref |
| `labels` | Label[] | no | `[{"scope":"priority","value":"high"}]` |

**Result:** `Issue`

#### pm.updateIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Issue ref |
| `subject` | string | no | New title |
| `body` | string | no | New description |
| `state` | string | no | New state |
| `assignees` | string[] | no | New assignees |
| `due` | string | no | New due date |
| `milestone` | string | no | New milestone ref |
| `sprint` | string | no | New sprint ref |
| `parent` | string | no | New parent ref |
| `labels` | Label[] | no | New labels |

**Result:** `Issue`

#### pm.closeIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Issue ref |

**Result:** `Issue`

#### pm.reopenIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Issue ref |

**Result:** `Issue`

#### pm.retractIssue

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Issue ref |

**Result:** `true`

#### pm.getMilestones

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | no | Repository URL (default: workspace) |
| `branch` | string | no | Branch |
| `states` | string[] | no | Filter by state |
| `limit` | int | no | Max results |

**Result:** `Milestone[]`

#### pm.getMilestone

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Milestone ref |

**Result:** `Milestone`

#### pm.createMilestone

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Milestone title |
| `body` | string | no | Description |
| `state` | string | no | Initial state |
| `due` | string | no | ISO 8601 due date |

**Result:** `Milestone`

#### pm.updateMilestone

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Milestone ref |
| `title` | string | no | New title |
| `body` | string | no | New description |
| `state` | string | no | New state |
| `due` | string | no | New due date |

**Result:** `Milestone`

#### pm.closeMilestone / pm.reopenMilestone / pm.cancelMilestone

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Milestone ref |

**Result:** `Milestone`

#### pm.retractMilestone

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Milestone ref |

**Result:** `true`

#### pm.getMilestoneIssues

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Milestone ref |
| `states` | string[] | no | Filter by state |

**Result:** `Issue[]`

#### pm.getSprints

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | no | Repository URL (default: workspace) |
| `branch` | string | no | Branch |
| `states` | string[] | no | Filter: `"planned"`, `"active"`, `"completed"`, `"canceled"` |
| `limit` | int | no | Max results |

**Result:** `Sprint[]`

#### pm.getSprint

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Sprint ref |

**Result:** `Sprint`

#### pm.createSprint

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Sprint title |
| `body` | string | no | Description |
| `state` | string | no | Initial state (default: `"planned"`) |
| `start` | string | no | ISO 8601 start date |
| `end` | string | no | ISO 8601 end date |

**Result:** `Sprint`

#### pm.updateSprint

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Sprint ref |
| `title` | string | no | New title |
| `body` | string | no | New description |
| `state` | string | no | New state |
| `start` | string | no | New start date |
| `end` | string | no | New end date |

**Result:** `Sprint`

#### pm.activateSprint / pm.completeSprint / pm.cancelSprint

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Sprint ref |

**Result:** `Sprint`

#### pm.retractSprint

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Sprint ref |

**Result:** `true`

#### pm.getSprintIssues

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Sprint ref |
| `states` | string[] | no | Filter by state |

**Result:** `Issue[]`

#### pm.getBoardView

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `boardId` | string | no | Board ID (default: first configured board) |

**Result:** `BoardView`

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

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Item ref (issue, milestone, or sprint) |
| `content` | string | yes | Comment body |

**Result:** `Post` (social comment)

#### pm.getItemComments

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Item ref |

**Result:** `Post[]`

---

### 4.3. Review

#### review.getPullRequests

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | no | Repository URL (default: workspace) |
| `branch` | string | no | Branch |
| `states` | string[] | no | Filter: `"open"`, `"merged"`, `"closed"` |
| `includeForks` | bool | no | Include PRs from registered forks |
| `limit` | int | no | Max results |

**Result:** `PullRequest[]`

#### review.getPR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `PullRequest`

#### review.createPR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `subject` | string | yes | PR title |
| `body` | string | no | PR description |
| `base` | string | yes | Base branch ref |
| `head` | string | yes | Head branch ref |
| `closes` | string[] | no | Issue refs to close on merge |
| `reviewers` | string[] | no | Reviewer emails |

**Result:** `PullRequest`

#### review.updatePR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `subject` | string | no | New title |
| `body` | string | no | New description |
| `state` | string | no | New state |
| `base` | string | no | New base |
| `head` | string | no | New head |
| `closes` | string[] | no | New close refs |
| `reviewers` | string[] | no | New reviewers |

**Result:** `PullRequest`

#### review.mergePR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `strategy` | string | no | Merge strategy: `ff` (default), `squash`, `rebase`, `merge` |

**Result:** `PullRequest`

#### review.updatePRTips

Capture current branch tips as a new version (signals "new code ready for review").

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `PullRequest`

#### review.syncPRBranch

Update the head branch with changes from the base branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `strategy` | string | no | Sync strategy: `rebase` (default), `merge` |

**Result:** `PullRequest`

#### review.getPRVersions

Get all versions of a PR (original + edits chain) with base-tip/head-tip snapshots.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `PRVersion[]`

#### review.comparePRVersions

Range-diff between two PR versions.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `from` | number | yes | Source version number |
| `to` | number | yes | Target version number |

**Result:** `string` (range-diff output)

#### review.getVersionAwareReviews

Compute per-reviewer staleness against PR versions. Uses range-diff to distinguish pure rebases from code changes.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `VersionAwareReview[]`

#### review.closePR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `PullRequest`

#### review.retractPR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `true`

#### review.getFeedbackForPR

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `Feedback[]`

#### review.createFeedback

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | yes | Feedback body |
| `pullRequest` | string | yes | PR ref |
| `commit` | string | no | Commit hash (12 chars) |
| `file` | string | no | File path |
| `oldLine` | int | no | Line in old file |
| `newLine` | int | no | Line in new file |
| `oldLineEnd` | int | no | End line in old file |
| `newLineEnd` | int | no | End line in new file |
| `reviewState` | string | no | `"approved"` or `"changes-requested"` |
| `suggestion` | bool | no | Body contains ` ```suggestion ``` ` block |

**Result:** `Feedback`

#### review.updateFeedback

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Feedback ref |
| `content` | string | no | New content |
| `reviewState` | string | no | New review state |

**Result:** `Feedback`

#### review.retractFeedback

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Feedback ref |

**Result:** `true`

#### review.applySuggestion

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Feedback ref containing suggestion |

**Result:** `string` (applied file path)

#### review.getDiff

Returns the diff between a PR's base and head.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `FileDiff[]`

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

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `DiffStats`

```json
{"filesChanged": 5, "insertions": 120, "deletions": 45}
```

#### review.getFileDiff

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `file` | string | yes | File path |

**Result:** `FileDiff`

#### review.getFileContent

Returns file content at a specific ref.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |
| `file` | string | yes | File path |
| `side` | string | yes | `"base"` or `"head"` |

**Result:** `string` (file contents)

#### review.getPRComments

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | PR ref |

**Result:** `Post[]`

#### review.getForks

**Params:** none

**Result:** `string[]` (fork URLs)

#### review.addFork

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Fork repository URL |

**Result:** `true`

#### review.removeFork

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Fork repository URL |

**Result:** `true`

---

### 4.4. Release

#### release.getReleases

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | no | Repository URL (default: workspace) |
| `branch` | string | no | Branch |
| `limit` | int | no | Max results |

**Result:** `Release[]`

#### release.getRelease

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |

**Result:** `Release`

#### release.createRelease

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `subject` | string | yes | Release title |
| `body` | string | no | Release notes |
| `tag` | string | no | Git tag |
| `version` | string | no | Version string |
| `prerelease` | bool | no | Pre-release flag |
| `artifacts` | string[] | no | Artifact names |
| `artifactURL` | string | no | Download URL |
| `checksums` | string | no | Checksum data |
| `signedBy` | string | no | GPG signer |
| `sbom` | string | no | SBOM filename (e.g., `sbom.spdx.json`) |

**Result:** `Release`

#### release.editRelease

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |
| `subject` | string | no | New title |
| `body` | string | no | New notes |
| `tag` | string | no | New tag |
| `version` | string | no | New version |
| `prerelease` | bool | no | New pre-release flag |
| `artifacts` | string[] | no | New artifacts |
| `artifactURL` | string | no | New download URL |
| `checksums` | string | no | New checksums |
| `signedBy` | string | no | New signer |
| `sbom` | string | no | New SBOM filename |

**Result:** `Release`

#### release.retractRelease

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |

**Result:** `true`

#### release.getReleaseComments

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |

**Result:** `Post[]`

#### release.getSBOM

Returns parsed SBOM summary for a release (format, package count, licenses, generator).

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |

**Result:** `SBOMSummary`

#### release.getSBOMRaw

Returns the raw SBOM file content as a JSON string.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Release ref |

**Result:** `string` (raw SBOM JSON content)

---

### 4.5. Core

#### core.fetch

Fetches updates from all subscribed repositories. Returns immediately with a fetch ID. Progress and completion are reported via server notifications.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `listId` | string | no | Fetch only repositories in this list |

**Result:**
```json
{"fetchId": "f-1"}
```

The server sends `fetch.progress` and `fetch.complete` notifications for this `fetchId` (see Section 5).

#### core.push

Pushes local changes to the remote.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `extensions` | string[] | no | Extensions to push (default: all initialized) |

**Result:**
```json
{"pushed": ["social", "pm"]}
```

#### core.status

Returns workspace and extension status.

**Params:** none

**Result:**
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

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `extension` | string | yes | Extension name |

**Result:** `object` (extension-specific config JSON)

#### core.setConfig

Writes extension configuration.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `extension` | string | yes | Extension name |
| `config` | object | yes | Config object to write |

**Result:** `true`

#### core.initExtension

Initializes an extension in the workspace.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `extension` | string | yes | Extension name |
| `branch` | string | no | Custom branch name |

**Result:** `true`

#### core.getNotifications

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `unreadOnly` | bool | no | Only unread (default: false) |
| `types` | string[] | no | Filter by type |
| `limit` | int | no | Max results |

**Result:** `Notification[]`

```json
[{
  "repoURL": "https://github.com/user/repo",
  "hash": "abc123456789",
  "branch": "gitmsg/social",
  "type": "comment",
  "source": "social",
  "actor": {"name": "Bob", "email": "bob@example.com"},
  "timestamp": "2025-01-06T10:00:00Z",
  "isRead": false
}]
```

#### core.getUnreadCount

**Params:** none

**Result:** `int`

#### core.markAsRead

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repoURL` | string | yes | Notification repo URL |
| `hash` | string | yes | Notification hash |
| `branch` | string | yes | Notification branch |

**Result:** `true`

#### core.markAllAsRead

**Params:** none

**Result:** `true`

#### core.getHistory

Returns edit history for any item (post, issue, PR, release, etc.).

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref` | string | yes | Item ref |

**Result:** `MessageVersion[]`

```json
[
  {"hash": "abc123456789", "timestamp": "2025-01-06T10:00:00Z", "author": {...}, "content": "v1"},
  {"hash": "def234567890", "timestamp": "2025-01-06T11:00:00Z", "author": {...}, "content": "v2 (edited)"}
]
```

#### core.getSettings

**Params:** none

**Result:** `KeyValue[]`

```json
[
  {"key": "fetch.parallel", "value": "4"},
  {"key": "log.level", "value": "info"}
]
```

#### core.setSetting

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | string | yes | Setting key |
| `value` | string | yes | Setting value |

**Result:** `true`

---

## 5. Server Notifications

Server-initiated notifications (no `id` field) pushed to the client. Clients opt in by sending `subscribe` after initialization.

### 5.1. Subscribe

**Method:** `subscribe`

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `events` | string[] | yes | Events to subscribe to: `"fetch"`, `"notifications"`, `"workspace"` |

**Result:** `true`

### 5.2. Unsubscribe

**Method:** `unsubscribe`

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `events` | string[] | yes | Events to unsubscribe from |

**Result:** `true`

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

---

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

### PRVersion

```json
{
  "number": 0,
  "label": "original | v1 | v2 | latest",
  "commit_hash": "string (12-char)",
  "repo_url": "string",
  "branch": "string",
  "author_name": "string",
  "author_email": "string",
  "timestamp": "string (ISO 8601)",
  "base_tip": "string (12-char hash)",
  "head_tip": "string (12-char hash)",
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
  "reviewed_label": "original | v1 | latest",
  "current_version": 2,
  "current_label": "latest",
  "head_changed": true,
  "code_changed": false,
  "stale": false
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

---

## 7. Implementation Notes

### 7.1. Concurrency

The server MUST handle concurrent requests. Long-running operations (`core.fetch`) run asynchronously and report progress via notifications. Read operations (`getPosts`, `getIssues`, etc.) MUST NOT block on writes.

The cache layer already serializes DB access via `ExecLocked`/`QueryLocked`. The RPC server adds no additional locking.

### 7.2. Workspace Scope

All methods operate on the workspace set during `initialize`. To switch workspaces, the client shuts down and spawns a new server. Multi-root editors spawn one server per workspace.

### 7.3. Serialization

- Go `time.Time` serializes as ISO 8601 string
- Go `nil` pointers are omitted from JSON (not `null`)
- `Result[T]` maps to JSON-RPC: `Success` → `result`, `Failure` → `error`
- Refs are strings in `#commit:hash@branch` or `url#commit:hash@branch` format

### 7.4. Extension Registration

Methods are registered per extension. If an extension is not initialized, its methods return `-32003 NOT_INITIALIZED`. Clients check `initialize` response to know which extensions are available.

### 7.5. Package Structure

```
library/rpc/
├── server.go           # Stdio read loop, JSON-RPC dispatch
├── handler.go          # Method registration, param unmarshaling
├── methods_social.go   # social.* method handlers
├── methods_pm.go       # pm.* method handlers
├── methods_review.go   # review.* method handlers
├── methods_release.go  # release.* method handlers
├── methods_core.go     # core.* + lifecycle method handlers
├── notifications.go    # Subscription management, event push
└── types.go            # Request/response param structs
```

Each method handler is a function that unmarshals params, calls the existing extension API, and returns the result. No business logic lives in the RPC layer.
