# GitSocial Extension Specification

GitSocial is a social networking extension for the GitMsg protocol.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

### 1.1. Types

Message types:
- `post`: Standard message (implicit - no header required on configured branch)
- `comment`: Response to content
- `repost`: Share without commentary
- `quote`: Share with commentary

Commits without GitMsg headers on the configured branch (see Section 3) are implicit posts. Commits with GitMsg headers are explicit interactions.

### 1.2. Fields

Subject line requirements:
- Reposts: `# <Author Name> @ <owner>/<repo>: <excerpt>` (remote) or `# <Author Name>: <excerpt>` (local)
- Comments/quotes: Subject line MUST contain user content

GitMsg header fields:
- `original`: Reference to original content being commented on, reposted, or quoted
- `reply-to`: Reference to parent comment in nested discussions

Field order: `type`, `edits`, `retracted` (core fields per GITMSG.md), then `reply-to`, then `original`.

### 1.3. Reference Sections

Reference structure requirements:
- Comments: `original` field MUST reference the thread's first post, not intermediate comments
- Nested comments: MUST include both `reply-to` (parent comment) and `original` (first post) fields
- Reposts: MUST reference original posts (not other reposts) using `original` field

### 1.4. Editing and Deleting

Messages MAY be edited or deleted using core versioning (GITMSG.md 1.4). Implementations SHOULD display an edit indicator on modified messages.

## 2. Lists

Lists define repositories to follow. Posts from repositories in lists appear in the timeline.

Repository entries use `<url>#branch:<branch>` format. `#branch:*` follows all branches (see GITMSG.md 2.1).

## 3. Configuration

Configuration is stored at `refs/gitmsg/social/config`:

```json
{
  "version": "0.1.0",
  "branch": "gitmsg/social"
}
```

Configuration MUST include: `version`.

Configuration SHOULD include: `branch` (the branch containing all GitSocial content). Default: `gitmsg/social`.

Branch resolution follows GITMSG.md Section 3.3. Only the resolved branch is scanned for social content. Commits without headers on this branch are implicit posts. Commits with GitMsg headers on this branch are interactions. All other branches are ignored.

## 4. Manifest

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

## Appendix: Examples

### Implicit Post

```
Hello world!
```

### Comment

```
Great idea!

--- GitMsg: ext="social"; type="comment"; original="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---
> Original post content
```

### Nested Comment

```
I agree!

--- GitMsg: ext="social"; type="comment"; reply-to="#commit:def456789abc@main"; original="#commit:abc123456789@main"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="comment"; author="Bob"; email="bob@example.com"; time="2025-01-06T11:00:00Z"; ref="#commit:def456789abc@main"; v="0.1.0" ---
> Parent comment

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="#commit:abc123456789@main"; v="0.1.0" ---
> Original post
```

### Repost

```
# Alice @ user/repo: Original content excerpt

--- GitMsg: ext="social"; type="repost"; original="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---
> Original content
```

### Edit Post

```
Hello world! (updated)

--- GitMsg: ext="social"; type="post"; edits="#commit:abc123456789@main"; v="0.1.0" ---
```

### Delete Post

```
--- GitMsg: ext="social"; edits="#commit:abc123456789@main"; retracted="true"; v="0.1.0" ---
```

### Edit Comment

```
Great idea! I especially like the part about...

--- GitMsg: ext="social"; type="comment"; edits="#commit:def456789abc@main"; original="#commit:abc123456789@main"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; type="post"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="#commit:abc123456789@main"; v="0.1.0" ---
> Original post content
```

