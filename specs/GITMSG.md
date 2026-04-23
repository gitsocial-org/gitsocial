# GitMsg Core Protocol Specification

GitMsg is a decentralized messaging protocol using Git commits as message containers.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

Messages are immutable Git commits.

### 1.1. Basic Structure

```
[<subject-line>]

[<message-body>]

GitMsg: ext="<extension>"; [field-name="value"]; v="<version>"

GitMsg-Ref: ext="<extension>"; author="<author>"; email="<email>"; time="<timestamp>"; [field-name="value"]; ref="<reference>"; v="<version>"
 > Referenced content on each line
```

GitMsg uses standard git trailers (`git-interpret-trailers(1)`) as the message envelope. The `GitMsg:` trailer carries message metadata. `GitMsg-Ref:` trailers carry reference sections with quoted content as continuation lines (lines starting with a space).

### 1.2. Header Requirements

The `GitMsg:` trailer MUST use semicolon-separated `field="value"` pairs as its value. All field values MUST be UTF-8 encoded.

The `GitMsg:` trailer MUST contain `ext` (extension namespace) and `v` (protocol version) fields. Extensions with message types MUST include a `type` field. Third-party extensions MAY include `ext-v` (extension version) for compatibility tracking. Extension-specific fields are OPTIONAL.

Field order in the `GitMsg:` trailer value MUST be:
1. `ext` (REQUIRED, first); `ext-v` (if present, immediately after `ext`)
2. `type` (if present)
3. `edits` (if present)
4. `retracted` (if present, boolean modifier for `edits`)
5. Origin fields in alphabetical order (if present): `origin-author-email`, `origin-author-name`, `origin-platform`, `origin-time`, `origin-url`
6. Extension-specific fields and `labels` in the order defined by the extension (alphabetical by default). Extensions SHOULD order fields for human readability since headers appear in git commit messages — group related fields and lead with the most important (e.g., state first, categorization last).
7. `v` (REQUIRED, last)

Extensions SHOULD include semantic reference fields in the `GitMsg:` trailer for performance and searchability. Header reference field values MUST match corresponding `GitMsg-Ref:` trailer `ref` field values.

### 1.3. Reference Sections

Reference sections use `GitMsg-Ref:` trailers. Referenced content MUST appear as continuation lines (prefixed with ` > ` — a space followed by `> ` on each line).

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

`GitMsg-Ref:` trailers MUST include the following fields:
- `ext`: Extension name
- `author`: Commit author name
- `email`: Commit author email
- `time`: Commit timestamp in ISO 8601 format
- `ref`: Reference with proper commit hash
- `v`: Protocol version

Field order in `GitMsg-Ref:` trailer values MUST be:
1. `ext` (REQUIRED, first)
2. `type` (if present)
3. `author` (REQUIRED)
4. `email` (REQUIRED)
5. `time` (REQUIRED)
6. Extension-specific fields in alphabetical order (extensions MAY define exceptions)
7. `ref` (REQUIRED)
8. `v` (REQUIRED, last)

Third-party extensions MAY include `ext-v` (extension version) for compatibility tracking.

The Git primitives (`author`, `email`, `time`, `ref`) enable traceability, decentralization, and message reconstruction without requiring commit fetches. `GitMsg-Ref:` trailers MAY include a `type` field to describe the referenced message type.

Extensions MAY add extension-specific metadata fields to `GitMsg-Ref:` trailers. These fields provide contextual information about the referenced message without requiring content parsing. Extension-specific fields:
- SHOULD be small structured values (strings, numbers, booleans)
- SHOULD represent immutable or stable metadata (file paths, line numbers, status at time of reference)
- MUST be declared in the extension manifest's `fields` array
- MAY become stale (represent state at time of reference, not current state)

When multiple `GitMsg-Ref:` trailers reference a dependency chain, references SHOULD be ordered with the most immediate dependency first and the root/original last.

### 1.4. Trailer Block Structure

All `GitMsg:` and `GitMsg-Ref:` trailers MUST appear in a single contiguous trailer block at the end of the commit message, separated from the body by a blank line. The `GitMsg:` trailer MUST appear first, followed by `GitMsg-Ref:` trailers.

Continuation lines (quoted content) MUST start with a space followed by `> ` (e.g., ` > quoted text`). Blank lines within quoted content MUST use ` >` (space followed by `>`).

GitMsg trailers MAY coexist with standard git trailers (`Signed-off-by:`, `Co-authored-by:`, etc.) in the same trailer block.

### 1.5. Versioning

Messages MAY be edited or retracted using the `edits` field.

- `edits` MUST reference the original message, not intermediate edits
- Edit commits MUST contain complete replacement content
- Latest edit by timestamp is current version; tie-breaker: commit hash lexicographically descending
- Original commit hash remains the canonical ID
- `retracted="true"` modifier marks message as deleted; SHOULD be hidden from normal views
- Retracted messages MAY omit the `type` field

```
GitMsg: ext="social"; type="post"; edits="#commit:abc123456789@main"; v="0.1.0"
```

```
GitMsg: ext="social"; edits="#commit:abc123456789@main"; retracted="true"; v="0.1.0"
```

Implementations MUST resolve references by locating the original commit, finding all edits, and returning the latest (or retracted state).

### 1.6. Mentions

Mentions reference a person within message content using the syntax `@<email>` where `<email>` is a valid address per RFC 5322 `addr-spec`. The leading `@` distinguishes mentions from plain email addresses in text.

Mentions MAY appear in subject lines and message bodies of any extension's messages.

- Implementations MUST recognize `@<email>` patterns in message bodies and subject lines
- `@@<email>` is an escaped mention and MUST NOT be treated as a mention; implementations SHOULD render it as literal `@<email>`

### 1.7. Labels

Messages MAY include a `labels` field containing comma-separated scoped values in `<scope>/<value>` format (e.g., `labels="kind/feature,priority/high"`). Labels provide categorization and filtering across extensions.

- Label format: `^[a-z]+/[a-zA-Z0-9._-]+$`
- Labels are a core field: available to all extensions without manifest declaration
- Extensions define the position of `labels` within their field order

Messages MAY use `vocab/<name>` labels to declare a formal taxonomy, with `<name>/*` labels carrying terms from that taxonomy (e.g., `vocab/dewey,dewey/005.8`).

### 1.8. Commit Trailers

Regular commits (without `GitMsg:` trailers) MAY use git trailers to reference GitMsg items. Implementations MUST recognize these trailer keys:

- `Fixes:`, `Closes:`, `Resolves:`, `Implements:` — closing references
- `Refs:` — non-closing reference

Trailer values MUST be a GitMsg reference, a URL, or an opaque external identifier (e.g., `PROJ-123`). Each reference MUST use a separate trailer line. Implementations SHOULD scan for trailers containing GitMsg references during fetch and surface them in the referenced item's activity.

Closing trailers MUST NOT trigger state changes. Only structured GitMsg messages (e.g., pull requests with `closes` field) control item state. Implementations MAY ignore unresolvable external identifiers.

### 1.9. Origin

Messages MAY include origin fields to indicate content imported from external platforms. Origin fields provide machine-readable provenance metadata and are OPTIONAL.

Available origin fields:
- `origin-author-email`: Original author email address (e.g., `alice@example.com`)
- `origin-author-name`: Original author display name (e.g., `Alice Smith`)
- `origin-platform`: Source platform name (e.g., `github`, `gitlab`)
- `origin-time`: Original creation timestamp in ISO 8601 format
- `origin-url`: URL to the original item on the source platform

All origin fields are OPTIONAL and independent. Implementations SHOULD include `origin-url` when available for traceability. Origin fields MUST NOT be modified during edits of imported content.

```
GitMsg: ext="pm"; type="issue"; origin-author-email="alice@example.com"; origin-author-name="Alice Smith"; origin-platform="github"; origin-time="2025-01-06T10:30:00Z"; origin-url="https://github.com/user/repo/issues/42"; state="open"; v="0.1.0"
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

Extensions define message types and operations. Messages with `GitMsg:` trailers MUST declare the extension:

### 3.1. Core Configuration

Core protocol configuration MUST be stored at `refs/gitmsg/core/config` as JSON. Implementations MAY store arbitrary keys.

Core configuration MAY include: `forks` (array of repository URLs for cross-fork collaboration).

### 3.2. Identity Verification

A signed commit is "verified" when its `(signing key, author email)` pair is attested by an external authority. Verification is a property of the binding, not of an individual commit: once a `(key, email)` binding is verified, every signed commit matching that binding is verified.

Implementations MUST NOT reject unsigned commits; unsigned commits are unverified. Implementations MAY consult any number of attestation sources; sources attest the binding independently. A binding is verified when at least one source affirms it, and an implementation MUST NOT treat a non-affirmative response from one source as evidence against another.

This protocol defines one attestation source: the domain-owner well-known endpoint. For commits whose author email is on a domain the user controls, implementations MAY fetch `https://<domain>/.well-known/gitmsg-id.json`. The response MUST be a JSON document of the form:

```json
{
  "identities": {
    "<local-part>": {
      "key": "ssh-ed25519 AAAAC3...",
      "repo": "https://example.com/user/repo"
    }
  }
}
```

Each entry MUST contain `key`; `repo` is OPTIONAL.

A document fetched from host `<host>` attests bindings at `<host>` only. Implementations MUST NOT treat such a document as attesting a binding at any other domain. The binding is verified when the document contains an entry whose `<local-part>` matches the local part of the author email and whose `key` matches the commit's signing key.

If the email's exact host has no document, implementations MAY apply a single mail-subdomain fallback: for an email at `<prefix>.<parent>` where `<prefix>` is one of `mail`, `email`, `smtp`, `imap`, `pop`, or `mx`, the document at `<parent>` MAY also attest the binding at `<prefix>.<parent>`. Implementations MUST NOT walk more than one level up, and MUST NOT apply the fallback when `<parent>` contains no dot (which would attempt fetches against TLDs).

Implementations MAY consult additional attestation sources, such as forge-published key/UID mappings or forge commit-verification APIs. The conventions of those services are defined by their providers and are out of scope for this specification.

### 3.3. Extension Requirements

- MUST store data under `refs/gitmsg/<extension-name>/`
- MUST validate extension compatibility before processing
- SHOULD handle unknown extensions gracefully
- Configuration MUST be stored at `refs/gitmsg/<extension-name>/config` as JSON

### 3.4. Branch Resolution

Extensions store content on a dedicated branch. Implementations MUST resolve the content branch using the following algorithm:

1. Read `refs/gitmsg/<extension-name>/config` for a configured `branch` value
2. If not found, check if `gitmsg/<extension-name>` branch exists (convention)
3. If not found, use the repository default branch

Implementations MUST only scan the resolved branch for extension content.

### 3.5. Manifest

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

Extension manifests MAY include: `display` (human-readable name), `types` (array of valid values for the `type` field), `fields` (array of extension-specific field names for both `GitMsg:` and `GitMsg-Ref:` trailers).

Core fields (`type`, `author`, `email`, `time`, `ref`, `edits`, `retracted`, `labels`, `origin-author-email`, `origin-author-name`, `origin-platform`, `origin-time`, `origin-url`) are available to all extensions and do not need to be declared in the manifest.

## Appendix: Validation

- Header trailer: `^GitMsg: (.*)$`
- Reference trailer: `^GitMsg-Ref: (.*)$`
- Continuation line: `^ > .*$` or `^ >$`
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
- Identity key (SSH): `^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/=]+$`
- Identity key (GPG): `^gpg:[A-Fa-f0-9]{16,40}$`
- DNS well-known: `https://<domain>/.well-known/gitmsg-id.json`

## Appendix: Examples

### Minimal Message

```
Hello world!
```

### Message with Reference

```
This is a response

GitMsg: ext="social"; type="comment"; original="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0"
GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789@main"; v="0.1.0"
 > Original message content
```

### Cross-Extension Example: PM + Review Integration

Extensions can reference each other's messages to create integrated workflows.

Step 1 - Create issue (pm extension):

```
Add dark mode support

Users can toggle between light and dark themes in settings.

GitMsg: ext="pm"; type="issue"; state="open"; labels="kind/feature,priority/high"; v="0.1.0"
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

GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; closes="#commit:abc123456789@gitmsg/pm"; reviewers="bob@example.com"; v="0.1.0"
GitMsg-Ref: ext="pm"; type="issue"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789@gitmsg/pm"; v="0.1.0"
 > Add dark mode support
```

Step 3 - Merge PR (auto-closes the linked issue):

```
Add dark mode support

Implements theme toggle with system preference detection.

GitMsg: ext="review"; type="pull-request"; edits="#commit:def456789abc@gitmsg/review"; state="merged"; base="#branch:main"; base-tip="f1e2d3c4b5a6"; head="#branch:dark-mode"; head-tip="a1b2c3d4e5f6"; closes="#commit:abc123456789@gitmsg/pm"; merge-base="f1e2d3c4b5a6"; merge-head="a1b2c3d4e5f6"; reviewers="bob@example.com"; v="0.1.0"
```

### Imported Message with Origin

Content imported from external platforms uses origin fields for provenance:

```
Add dark mode support

Users can toggle between light and dark themes in settings.

GitMsg: ext="pm"; type="issue"; origin-author-email="alice@example.com"; origin-author-name="Alice Smith"; origin-platform="github"; origin-time="2025-01-06T10:30:00Z"; origin-url="https://github.com/user/repo/issues/42"; state="open"; labels="kind/feature,priority/high"; v="0.1.0"
```
