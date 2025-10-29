# GitSocial Extension Specification

GitSocial is a social networking extension for the GitMsg protocol.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

### 1.1. Basic Structure

```
[<subject-line>]

[<message-body>]

[--- GitMsg: ext="social"; [field-name="value"]; v="<version>"; ext-v="<version>" ---]

[--- GitMsg-Ref: ext="social"; author="<author>"; email="<email>"; time="<timestamp>"; [field-name="value"]; ref="<reference>"; v="<version>"; ext-v="<ext-version>" ---]
[> Referenced content on each line]
```

Message types:
- `post`: Standard message (implicit - no header required on configured branch)
- `comment`: Response to content
- `repost`: Share without commentary
- `quote`: Share with commentary

Commits without GitMsg headers on the configured branch (see 3.1) are implicit posts. Commits with GitMsg headers are explicit interactions.

### 1.2. Header Requirements

Subject line requirements:
- Reposts: `# <Author Name> @ <owner>/<repo>: <excerpt>` (remote) or `# <Author Name>: <excerpt>` (local)
- Comments/quotes: Subject line MUST contain user content

GitMsg header fields:
- `original`: Reference to original content being commented on, reposted, or quoted
- `reply-to`: Reference to parent comment in nested discussions

When both fields are present, `reply-to` MUST appear before `original`.

### 1.3. Reference Sections

Reference structure requirements:
- Comments: `original` field MUST reference the thread's first post, not intermediate comments
- Nested comments: MUST include both `reply-to` (parent comment) and `original` (first post) fields
- Reposts: MUST reference original posts (not other reposts) using `original` field

## 2. Lists

Lists are stored at `refs/gitmsg/social/lists/<list-id>`:

```json
{
  "version": "0.1.0",
  "id": "reading",
  "name": "Reading",
  "repositories": [
    "git@github.com:owner/repo#branch:main",
    "https://github.com/public/myproject#branch:main"
  ],
  "source": "https://github.com/user/repo#list:reading"
}
```

Lists MUST include: `version`, `id` (matching `[a-zA-Z0-9_-]{1,40}`), `name`, `repositories` (array of repository references in `<url>#branch:<name>` format).

Lists MAY include: `source` (source list reference in `<url>#list:<list-id>` format). When present, indicates a followed list. Synchronization updates `repositories` array to match source. Removing `source` converts list to regular list with current repositories.

Lists use state-based storage where each update creates a new commit with complete state.

## 3. GitSocial Extension

### 3.1. Configuration

Configuration is stored at `refs/gitmsg/social/config`:

```json
{
  "version": "0.1.0",
  "branch": "gitsocial"
}
```

Configuration MUST include: `version`, `branch` (the branch containing all GitSocial content).

Branch resolution: (1) Read `refs/gitmsg/social/config` for configured branch, (2) if not found check if `gitsocial` branch exists (convention), (3) if not found use repository default branch. Only the resolved GitSocial branch is scanned for social content. Commits without headers on this branch are implicit posts. Commits with GitMsg headers on this branch are interactions. All other branches are ignored.

### 3.2. Manifest

```json
{
  "name": "social",
  "version": "0.1.0",
  "display": "GitSocial",
  "description": "Social networking extension for GitMsg",
  "types": ["post", "comment", "repost", "quote"],
  "fields": ["original", "reply-to"]
}
```

Core GitMsg fields (`type`, `author`, `email`, `time`, `ref`) are available to all extensions and do not need to be declared in the manifest.

## Appendix: Examples

### Implicit Post

```
Hello world!
```

### Comment

```
Great idea!

--- GitMsg: ext="social"; type="comment"; original="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---
> Original post content
```

### Nested Comment

```
I agree!

--- GitMsg: ext="social"; type="comment"; reply-to="#commit:def456789abc"; original="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="comment"; author="Bob"; email="bob@example.com"; time="2025-01-06T11:00:00Z"; ref="#commit:def456789abc"; v="0.1.0"; ext-v="0.1.0" ---
> Parent comment

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---
> Original post
```

### Repost

```
# Alice @ user/repo: Original content excerpt

--- GitMsg: ext="social"; type="repost"; original="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---
> Original content
```

### Quote

```
Adding my thoughts on this...

--- GitMsg: ext="social"; type="quote"; original="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Bob"; email="bob@example.com"; time="2025-01-06T09:15:00Z"; ref="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---
> Quoted content
```
