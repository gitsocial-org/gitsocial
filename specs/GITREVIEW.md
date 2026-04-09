# GitReview Extension Specification

Code contribution and review extension for the GitMsg protocol (name: `review`, version: `0.1.0`).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

### 1.1. Types

- `pull-request`: Propose code changes
- `feedback`: Code review activity (inline comment, verdict, suggestion, or combination)

For general comments on a pull request, see Section 1.6.

### 1.2. Pull Request Fields

Fields (in header order):
- `state`: MUST be `open`, `merged`, or `closed`
- `draft`: MAY be `true` to indicate the pull request is not ready for review (OPTIONAL)
- `base`: Target branch reference (`<repo-url>#branch:<name>` or `#branch:<name>`)
- `base-tip`: Base branch commit hash at time of creation or update, 12 characters (OPTIONAL)
- `head`: Source branch reference (`<repo-url>#branch:<name>` or `#branch:<name>`)
- `head-tip`: Head branch commit hash at time of creation or update, 12 characters (OPTIONAL)
- `closes`: MAY contain comma-separated issue references to close on merge
- `merge-base`: Common ancestor commit hash, 12 characters (OPTIONAL, only on `state="merged"` edits)
- `merge-head`: Head branch commit hash at merge time, 12 characters (OPTIONAL, only on `state="merged"` edits)
- `reviewers`: MAY contain comma-separated reviewer email addresses
- `labels`: MAY contain comma-separated scoped values (e.g. `labels="kind/bug,priority/high"`) (OPTIONAL, core field)

Field order: `state`, `draft`, `base`, `base-tip`, `head`, `head-tip`, `closes`, `merge-base`, `merge-head`, `reviewers`, `labels`.

The `head` and `base` fields support full repository URLs, enabling cross-forge contributions (e.g., GitLab to GitHub).

### 1.3. Feedback Fields

Fields (in header order):
- `pull-request`: MUST reference the pull request commit
- `commit`: Specific commit hash (12 characters, OPTIONAL)
- `file`: File path relative to repo root (OPTIONAL)
- `new-line`: Line number in the new file version, 1-indexed (OPTIONAL)
- `new-line-end`: End line in the new file version (OPTIONAL)
- `old-line`: Line number in the old file version, 1-indexed (OPTIONAL)
- `old-line-end`: End line in the old file version (OPTIONAL)
- `review-state`: `approved` or `changes-requested` (OPTIONAL)
- `suggestion`: `true` if the message body contains a suggested replacement (OPTIONAL)

Field order: `pull-request`, `commit`, `file`, `new-line`, `new-line-end`, `old-line`, `old-line-end`, `review-state`, `suggestion`.

A feedback message MUST include at least one of: code-location fields (`file`, `commit`, and at least one of `old-line` or `new-line`) OR `review-state`.

Inline feedback (with code-location fields) MUST include `file`, `commit`, and at least one of `old-line` or `new-line`.

### 1.4. Message Rules

- Pull requests with `closes` SHOULD auto-close referenced issues when merged via core versioning
- Suggestions MUST include the replacement code in the message body as a fenced code block (`` ```suggestion ... ``` ``)

### 1.5. Editing and Retracting

Pull requests MAY be edited or retracted using core versioning (GITMSG.md Section 1.5). State transitions (open -> merged, open -> closed) are edits to the original `pull-request` commit. Draft transitions (adding or removing `draft="true"`) are also edits. Implementations MUST NOT merge a pull request while `draft="true"`. Implementations SHOULD suppress review request notifications for draft pull requests.

Implementations SHOULD include `base-tip` and `head-tip` when creating or editing a pull request. These fields capture the commit hashes (12 characters) that the `base` and `head` branches point to at the time of the commit. When a pull request's code is updated (e.g., after rebase or new commits), implementations SHOULD create an edit with updated `base-tip` and `head-tip` values. The edits chain serves as a version history of the pull request's code state, enabling implementations to compare any two versions via range-diff.

When transitioning to `state="merged"`, implementations SHOULD include `merge-base` with the common ancestor commit hash (12 characters) and `merge-head` with the head branch tip commit hash (12 characters), both computed before the merge. Implementations MAY use these fields to reconstruct the original diff and commit range after the head branch has been merged into base.

When merging a cross-repository pull request (fork PR), implementations SHOULD first copy the pull request to the upstream review branch with a `GitMsg-Ref:` trailer preserving the original author's identity. The merge edit then references the local copy as canonical, ensuring the upstream has a self-contained record that survives fork deletion.

Feedback messages MAY be edited or retracted using core versioning. Implementations SHOULD display an edit indicator on modified messages.

### 1.6. Comments

Implementations MUST use GitSocial for pull request comments. The `original` field references the pull request commit:

```
Looks great, just one nit on the theme toggle.

GitMsg: ext="social"; type="comment"; original="#commit:abc123456789@gitmsg/review"; v="0.1.0"
GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789@gitmsg/review"; v="0.1.0"
 > Add dark mode support
```

Nested comment threads use GitSocial's `reply-to` field.

### 1.7. Comment Anchoring

Inline reviews are anchored to specific file locations at a specific commit. When the PR branch is rebased or updated, implementations SHOULD attempt to map comment locations to the new commit:

1. Match the file and surrounding context lines
2. If no match, mark the comment as outdated

Anchoring is an implementation concern. Implementations MAY use different strategies for mapping comments across revisions.

### 1.8. Review Aggregation

Implementations SHOULD derive pull request review state from `feedback` commits that include `review-state`:

- If any reviewer's latest review has `review-state="changes-requested"`, the pull request SHOULD be considered blocked
- If all reviewers' latest reviews have `review-state="approved"`, the pull request SHOULD be considered approved

Implementations MAY enforce additional merge requirements via configuration.

## 2. Config

Configuration MUST be stored at `refs/gitmsg/review/config`:

```json
{
  "version": "0.1.0",
  "branch": "gitmsg/review",
  "require-review": true
}
```

Configuration MUST include: `version`.

Configuration SHOULD include: `branch`. Default: `gitmsg/review`.

Configuration MAY include: `require-review` (boolean, default `false`).

## Appendix: Manifest

```json
{
  "name": "review",
  "version": "0.1.0",
  "display": "GitReview",
  "description": "Code contribution and review extension for GitMsg",
  "types": ["pull-request", "feedback"],
  "fields": ["base", "base-tip", "closes", "commit", "draft", "file", "head", "head-tip", "merge-base", "merge-head", "new-line", "new-line-end", "old-line", "old-line-end", "pull-request", "review-state", "reviewers", "state", "suggestion"]
}
```

## Appendix: Validation

| Field | Pattern |
|-------|---------|
| `state` (pull-request) | `open\|merged\|closed` |
| `draft` | `true` |
| `review-state` | `approved\|changes-requested` |
| `file` | relative path, no leading slash |
| `old-line` | positive integer, 1-indexed |
| `old-line-end` | positive integer, 1-indexed |
| `new-line` | positive integer, 1-indexed |
| `new-line-end` | positive integer, 1-indexed |
| `commit` | 12-character hash |
| `reviewers` | comma-delimited email addresses |
| `closes` | comma-separated issue references |
| `base-tip` | 12-character hash |
| `head-tip` | 12-character hash |
| `merge-base` | 12-character hash |
| `merge-head` | 12-character hash |
| `suggestion` | `true` |

## Appendix: Examples

### Create Pull Request

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; closes="#commit:abc123456789@gitmsg/pm"; reviewers="bob@example.com"; v="0.1.0"
```

### Create Draft Pull Request

```
WIP: Add dark mode support

GitMsg: ext="review"; type="pull-request"; state="open"; draft="true"; base="#branch:main"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; v="0.1.0"
```

### Mark Draft as Ready

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; edits="#commit:abc123456789@gitmsg/review"; state="open"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; reviewers="bob@example.com"; v="0.1.0"
```

### Approve Pull Request

```
LGTM!

GitMsg: ext="review"; type="feedback"; pull-request="#commit:abc123456789@gitmsg/review"; review-state="approved"; v="0.1.0"
GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:abc123456789@gitmsg/review"; v="0.1.0"
 > Add dark mode support
```

### Inline Review Comment

```
Consider caching this value

GitMsg: ext="review"; type="feedback"; pull-request="#commit:abc123456789@gitmsg/review"; commit="def456789abc"; file="src/theme.js"; new-line="42"; v="0.1.0"
GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:abc123456789@gitmsg/review"; v="0.1.0"
 > Add dark mode support
```

### Suggestion

~~~
Use a CSS custom property for the transition

```suggestion
transition: background-color var(--theme-transition, 200ms) ease;
```

GitMsg: ext="review"; type="feedback"; pull-request="#commit:abc123456789@gitmsg/review"; commit="def456789abc"; file="src/theme.css"; new-line="18"; suggestion="true"; v="0.1.0"
GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="#commit:abc123456789@gitmsg/review"; v="0.1.0"
 > Add dark mode support
~~~

### Merge Pull Request

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; edits="#commit:abc123456789@gitmsg/review"; state="merged"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="d4e5f6a1b2c3"; closes="#commit:abc123456789@gitmsg/pm"; merge-base="f1e2d3c4b5a6"; merge-head="d4e5f6a1b2c3"; reviewers="bob@example.com"; v="0.1.0"
```

### Retract Pull Request

```
GitMsg: ext="review"; edits="#commit:abc123456789@gitmsg/review"; retracted="true"; v="0.1.0"
```

### Cross-Forge Pull Request

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; state="open"; base="https://github.com/bob/repo#branch:main"; base-tip="f1e2d3c4b5a6"; head="https://gitlab.com/alice/repo#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; v="0.1.0"
```

### Merge Cross-Repo Pull Request

Step 1 - Copy fork PR to upstream (preserves original author via GitMsg-Ref):

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="https://gitlab.com/alice/repo#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; v="0.1.0"
GitMsg-Ref: ext="review"; type="pull-request"; author="Alice"; email="alice@example.com"; time="2025-01-20T10:00:00Z"; ref="https://gitlab.com/alice/repo#commit:abc123456789@gitmsg/review"; v="0.1.0"
 > Add dark mode support
```

Step 2 - Merge edit references the local copy:

```
Add dark mode support

GitMsg: ext="review"; type="pull-request"; edits="#commit:def456789abc@gitmsg/review"; state="merged"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="https://gitlab.com/alice/repo#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; merge-base="f1e2d3c4b5a6"; merge-head="a1b2c3d4e5f6"; v="0.1.0"
```
