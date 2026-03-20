# GitMsg Core Protocol Specification

GitMsg is a decentralized messaging protocol using Git commits as message containers.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

Messages are immutable Git commits.

### 1.1. Basic Structure

```
[<subject-line>]

[<message-body>]

[--- GitMsg: ext="<extension>"; [field-name="value"]; v="<version>" ---]

[--- GitMsg-Ref: ext="<extension>"; author="<author>"; email="<email>"; time="<timestamp>"; [field-name="value"]; ref="<reference>"; v="<version>" ---]
[> Referenced content on each line]
```

### 1.2. Header Requirements

Headers MUST start with `--- GitMsg: ` and end with ` ---`, using semicolon-separated `field="value"` pairs. All field values MUST be UTF-8 encoded.

Headers MUST contain `ext` (extension namespace) and `v` (protocol version) fields. Extensions with message types MUST include a `type` field. Third-party extensions MAY include `ext-v` (extension version) for compatibility tracking. Extension-specific fields are OPTIONAL.

Field order in GitMsg headers MUST be:
1. `ext` (REQUIRED, first); `ext-v` (if present, immediately after `ext`)
2. `type` (if present)
3. `edits` (if present)
4. `retracted` (if present, boolean modifier for `edits`)
5. Origin fields in alphabetical order (if present): `origin-author-email`, `origin-author-name`, `origin-platform`, `origin-time`, `origin-url`
6. Extension-specific fields and `labels` in the order defined by the extension (alphabetical by default). Extensions SHOULD order fields for human readability since headers appear in git commit messages — group related fields and lead with the most important (e.g., state first, categorization last).
7. `v` (REQUIRED, last)

Extensions SHOULD include semantic reference fields in GitMsg headers for performance and searchability. Header reference field values MUST match corresponding GitMsg-Ref section `ref` field values.

### 1.3. Reference Sections

Reference sections MUST start with `--- GitMsg-Ref:` and end with ` ---`. Referenced content MUST be prefixed with "> " on each line.

References use `<type>:<value>@<branch>` format. The branch suffix is REQUIRED and represents the branch context at time of reference (historical, may become stale). Implementations MUST support both remote and local references:
- Remote: `<repository-url>#<type>:<value>@<branch>` (e.g., `https://github.com/user/repo#commit:abc123456789@main`)
- Local: `#<type>:<value>@<branch>` (e.g., `#commit:abc123456789@main`)

Implementations MUST support the following core reference types:
- `commit:<hash>@<branch>` - Git commit in branch context (hash MUST be exactly 12 characters)
- `branch:<name>` - Git branch (no branch suffix)
- `tag:<name>` - Git tag (no branch suffix)
- `file:<path>@<branch>` - File at branch HEAD
- `file:<path>@<branch>:<ref>` - File at specific commit/tag
- `file:<path>@<branch>:L<n>` - File at specific line
- `file:<path>@<branch>:L<n>-<m>` - File at line range

GitMsg-Ref sections MUST include the following fields:
- `ext`: Extension name
- `author`: Commit author name
- `email`: Commit author email
- `time`: Commit timestamp in ISO 8601 format
- `ref`: Reference with proper commit hash
- `v`: Protocol version

Field order in GitMsg-Ref headers MUST be:
1. `ext` (REQUIRED, first)
2. `type` (if present)
3. `author` (REQUIRED)
4. `email` (REQUIRED)
5. `time` (REQUIRED)
6. Extension-specific fields in alphabetical order (extensions MAY define exceptions)
7. `ref` (REQUIRED)
8. `v` (REQUIRED, last)

Third-party extensions MAY include `ext-v` (extension version) for compatibility tracking.

The Git primitives (`author`, `email`, `time`, `ref`) enable traceability, decentralization, and message reconstruction without requiring commit fetches. GitMsg-Ref sections MAY include a `type` field to describe the referenced message type.

Extensions MAY add extension-specific metadata fields to GitMsg-Ref headers. These fields provide contextual information about the referenced message without requiring content parsing. Extension-specific fields:
- SHOULD be small structured values (strings, numbers, booleans)
- SHOULD represent immutable or stable metadata (file paths, line numbers, status at time of reference)
- MUST be declared in the extension manifest's `fields` array
- MAY become stale (represent state at time of reference, not current state)

When multiple GitMsg-Ref sections reference a dependency chain, references SHOULD be ordered with the most immediate dependency first and the root/original last.

### 1.4. Versioning

Messages MAY be edited or retracted using the `edits` field.

- `edits` MUST reference the original message, not intermediate edits
- Edit commits MUST contain complete replacement content
- Latest edit by timestamp is current version; tie-breaker: commit hash lexicographically descending
- Original commit hash remains the canonical ID
- `retracted="true"` modifier marks message as deleted; SHOULD be hidden from normal views
- Retracted messages MAY omit the `type` field

```
--- GitMsg: ext="social"; type="post"; edits="#commit:abc123456789@main"; v="0.1.0" ---
```

```
--- GitMsg: ext="social"; edits="#commit:abc123456789@main"; retracted="true"; v="0.1.0" ---
```

Implementations MUST resolve references by locating the original commit, finding all edits, and returning the latest (or retracted state).

### 1.5. Mentions

Mentions reference a person within message content using the syntax `@<email>` where `<email>` is a valid address per RFC 5322 `addr-spec`. The leading `@` distinguishes mentions from plain email addresses in text.

Mentions MAY appear in subject lines and message bodies of any extension's messages.

- Implementations MUST recognize `@<email>` patterns in message bodies and subject lines
- `@@<email>` is an escaped mention and MUST NOT be treated as a mention; implementations SHOULD render it as literal `@<email>`

### 1.6. Labels

Messages MAY include a `labels` field containing comma-separated scoped values in `<scope>/<value>` format (e.g., `labels="kind/feature,priority/high"`). Labels provide categorization and filtering across extensions.

- Label format: `^[a-z]+/[a-zA-Z0-9._-]+$`
- Labels are a core field: available to all extensions without manifest declaration
- Extensions define the position of `labels` within their field order

### 1.7. Commit Trailers

Regular commits (without GitMsg headers) MAY use git trailers (`git-interpret-trailers(1)`) to reference GitMsg items. Implementations MUST recognize these trailer keys:

- `Fixes:`, `Closes:`, `Resolves:`, `Implements:` — closing references
- `Refs:` — non-closing reference

Trailer values MUST be a GitMsg reference, a URL, or an opaque external identifier (e.g., `PROJ-123`). Each reference MUST use a separate trailer line. Implementations SHOULD scan for trailers containing GitMsg references during fetch and surface them in the referenced item's activity.

Closing trailers MUST NOT trigger state changes. Only structured GitMsg messages (e.g., pull requests with `closes` field) control item state. Implementations MAY ignore unresolvable external identifiers.

### 1.8. Origin

Messages MAY include origin fields to indicate content imported from external platforms. Origin fields provide machine-readable provenance metadata and are OPTIONAL.

Available origin fields:
- `origin-author-email`: Original author email address (e.g., `alice@example.com`)
- `origin-author-name`: Original author display name (e.g., `Alice Smith`)
- `origin-platform`: Source platform name (e.g., `github`, `gitlab`)
- `origin-time`: Original creation timestamp in ISO 8601 format
- `origin-url`: URL to the original item on the source platform

All origin fields are OPTIONAL and independent. Implementations SHOULD include `origin-url` when available for traceability. Origin fields MUST NOT be modified during edits of imported content.

```
--- GitMsg: ext="pm"; type="issue"; origin-author-email="alice@example.com"; origin-author-name="Alice Smith"; origin-platform="github"; origin-time="2025-01-06T10:30:00Z"; origin-url="https://github.com/user/repo/issues/42"; state="open"; v="0.1.0" ---
```

## 2. Lists

Lists are mutable collections stored at `refs/gitmsg/<extension>/lists/<id>`. Lists use state-based storage where each update creates a new commit with complete state as JSON.

```json
{
  "version": "0.1.0",
  "id": "reading",
  "name": "Reading",
  "repositories": ["https://github.com/user/repo#branch:main", "git@github.com:owner/repo#branch:main"],
  "source": "https://github.com/alice/lists#list:reading"
}
```

Lists MUST include: `version`, `id` (matching `[a-zA-Z0-9_-]{1,40}`), `name`, `repositories` (array of references).

Lists MAY include: `source` (reference to source list, `<url>#list:<id>`). When present, list syncs with source.

### 2.1. All-Branch Following

Repository references in lists MAY use `#branch:*` to follow all branches. When fetching a `*` repository, implementations MUST store each commit with its actual refname rather than a single hardcoded branch.

```json
{
  "version": "0.1.0",
  "id": "reading",
  "name": "Reading",
  "repositories": ["https://github.com/user/repo#branch:*"]
}
```

## 3. Extensions

Extensions define message types and operations. Messages with GitMsg headers MUST declare the extension:

### 3.1. Core Configuration

Core protocol configuration MUST be stored at `refs/gitmsg/core/config` as JSON. Implementations MAY store arbitrary keys.

Core configuration MAY include: `forks` (array of repository URLs for cross-fork collaboration).

### 3.2. Extension Requirements

- MUST store data under `refs/gitmsg/<extension-name>/`
- MUST validate extension compatibility before processing
- SHOULD handle unknown extensions gracefully
- Configuration MUST be stored at `refs/gitmsg/<extension-name>/config` as JSON

### 3.3. Branch Resolution

Extensions store content on a dedicated branch. Implementations MUST resolve the content branch using the following algorithm:

1. Read `refs/gitmsg/<extension-name>/config` for a configured `branch` value
2. If not found, check if `gitmsg/<extension-name>` branch exists (convention)
3. If not found, use the repository default branch

Implementations MUST only scan the resolved branch for extension content.

### 3.4. Manifest

Extension manifests enable cross-extension discovery and compatibility. When an extension encounters a reference to another extension's message, it fetches the referenced commit to read its extension declaration and uses the manifest to understand that extension's message types and fields.

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

Extension manifests MUST include: `name` (matching `[a-z][a-z0-9_-]*`), `version` (semver), `description`.

Extension manifests MAY include: `display` (human-readable name), `types` (array of valid values for the `type` field), `fields` (array of extension-specific field names for both GitMsg headers and GitMsg-Ref sections).

Core fields (`type`, `author`, `email`, `time`, `ref`, `edits`, `retracted`, `labels`, `origin-author-email`, `origin-author-name`, `origin-platform`, `origin-time`, `origin-url`) are available to all extensions and do not need to be declared in the manifest.

## Appendix: Validation

- Header: `^--- GitMsg: (.*) ---$`
- Required Fields: `ext="[a-z][a-z0-9_-]*"`, `v="\d+\.\d+\.\d+"`
- Optional Fields: `ext-v="\d+\.\d+\.\d+"` (for third-party extensions)
- Versioning Fields: `edits="<reference>"`, `retracted="true"` (boolean, requires `edits`)
- Origin Fields: `origin-author-email="<email>"`, `origin-author-name="<name>"`, `origin-platform="<string>"`, `origin-time="<ISO 8601>"`, `origin-url="<URL>"`
- Extension Name: `^[a-z][a-z0-9_-]*$`
- Version: `^\d+\.\d+\.\d+$`
- Repo Prefix (HTTPS): `^https?://[^#]+#`
- Repo Prefix (SSH): `^git@[^:]+:[^#]+#`
- Reference (commit): `#commit:[a-f0-9]{12,}@[a-zA-Z0-9/_.-]+`
- Reference (branch): `#branch:[^\s#]+`
- Reference (tag): `#tag:[^\s#]+`
- Reference (file): `#file:[^@\s]+@[a-zA-Z0-9/_.-]+(:(L\d+(-\d+)?|[a-zA-Z0-9/_.-]+))?`
- Reference (list): `#list:[^\s#]+`
- Label: `^[a-z]+/[a-zA-Z0-9._-]+$`
- List Names: `^[a-zA-Z0-9_-]{1,40}$`
- Namespace: `^refs/gitmsg/[a-z][a-z0-9_-]*/.*$`
- Mention: `@([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`
- Trailer key: `^(Fixes|Closes|Resolves|Implements|Refs):$`
- Trailer line: `^(Fixes|Closes|Resolves|Implements|Refs): .+$`

## Appendix: Examples

### Minimal Message

```
Hello world!
```

### Message with Reference

```
This is a response

--- GitMsg: ext="social"; type="comment"; original="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---

--- GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0" ---
> Original message content
```

### Cross-Extension Example: PM + Review Integration

Extensions can reference each other's messages to create integrated workflows.

Step 1 - Create issue (pm extension):

```
Add dark mode support

Users can toggle between light and dark themes in settings.

--- GitMsg: ext="pm"; type="issue"; state="open"; labels="kind/feature,priority/high"; v="0.1.0" ---
```

Regular commit referencing the issue via trailer:

```
Implement theme toggle component

Refs: #commit:abc123456789@gitmsg/pm
```

Step 2 - Create pull request that closes the issue (review extension):

```
Add dark mode support

Implements theme toggle with system preference detection.

--- GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; closes="#commit:abc123456789@gitmsg/pm"; reviewers="bob@example.com"; v="0.1.0" ---

--- GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0" ---
> Add dark mode support
```

Step 3 - Merge PR (auto-closes the linked issue):

```
Add dark mode support

Implements theme toggle with system preference detection.

--- GitMsg: ext="review"; type="pull-request"; edits="#commit:def456789abc@gitmsg/review"; state="merged"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; closes="#commit:abc123456789@gitmsg/pm"; merge-base="f1e2d3c4b5a6"; merge-head="a1b2c3d4e5f6"; reviewers="bob@example.com"; v="0.1.0" ---
```

### Imported Message with Origin

Content imported from external platforms uses origin fields for provenance:

```
Add dark mode support

Users can toggle between light and dark themes in settings.

--- GitMsg: ext="pm"; type="issue"; origin-author-email="alice@example.com"; origin-author-name="Alice Smith"; origin-platform="github"; origin-time="2025-01-06T10:30:00Z"; origin-url="https://github.com/user/repo/issues/42"; state="open"; labels="kind/feature,priority/high"; v="0.1.0" ---
```
