# GitMsg Core Protocol Specification

GitMsg is a decentralized messaging protocol using Git commits as message containers.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 1. Messages

Messages are immutable Git commits.

### 1.1. Basic Structure

```
[<subject-line>]

[<message-body>]

[--- GitMsg: ext="<extension>"; [field-name="value"]; v="<version>"; ext-v="<version>" ---]

[--- GitMsg-Ref: ext="<extension>"; author="<author>"; email="<email>"; time="<timestamp>"; [field-name="value"]; ref="<reference>"; v="<version>"; ext-v="<ext-version>" ---]
[> Referenced content on each line]
```

### 1.2. Header Requirements

Headers MUST start with `--- GitMsg: ` and end with ` ---`, using semicolon-separated `field="value"` pairs. All field values MUST be UTF-8 encoded.

Headers MUST contain `ext` (extension namespace), `v` (protocol version), and `ext-v` (extension version) fields. Extensions with message types MUST include a `type` field. Extension-specific fields are OPTIONAL.

Extensions SHOULD include semantic reference fields in GitMsg headers for performance and searchability. Header reference field values MUST match corresponding GitMsg-Ref section `ref` field values.

### 1.3. Reference Sections

Reference sections MUST start with `--- GitMsg-Ref:` and end with ` ---`. Referenced content MUST be prefixed with "> " on each line.

References use `<type>:<value>` format. Implementations MUST support both remote and local references:
- Remote: `<repository-url>#<type>:<value>` (HTTPS: `https://github.com/user/repo#commit:abc123456789`, SSH: `git@github.com:user/repo.git#commit:abc123456789`)
- Local: `#<type>:<value>` (e.g., `#commit:abc123456789`)

Implementations MUST support `commit` and `branch` references. Extensions MAY define additional reference types. Commit hashes MUST be exactly 12 characters (e.g., `abc123456789`).

GitMsg-Ref sections MUST include the following fields:
- `ext`: Extension name
- `author`: Commit author name
- `email`: Commit author email
- `time`: Commit timestamp in ISO 8601 format
- `ref`: Reference with proper commit hash
- `v`: Protocol version
- `ext-v`: Extension version

The Git primitives (`author`, `email`, `time`, `ref`) enable traceability, decentralization, and message reconstruction without requiring commit fetches. GitMsg-Ref sections MAY include a `type` field to describe the referenced message type.

Extensions MAY add extension-specific metadata fields to GitMsg-Ref headers. These fields provide contextual information about the referenced message without requiring content parsing. Extension-specific fields:
- SHOULD be small structured values (strings, numbers, booleans)
- SHOULD represent immutable or stable metadata (file paths, line numbers, status at time of reference)
- MUST be declared in the extension manifest's `fields` array
- MAY become stale (represent state at time of reference, not current state)

When multiple GitMsg-Ref sections reference a dependency chain, references SHOULD be ordered with the most immediate dependency first and the root/original last.

## 2. Lists

Lists are mutable collections stored at `refs/gitmsg/<extension>/lists/<id>`. Lists use state-based storage where each update creates a new commit with complete state as JSON.

```json
{
  "version": "0.1.0",
  "id": "reading",
  "name": "Reading",
  "repositories": ["https://github.com/user/repo#branch:main", "git@github.com:owner/repo#branch:main"]
}
```

Lists MUST include: `version`, `id` (matching `[a-zA-Z0-9_-]{1,40}`), `name`, `repositories` (array of references).

## 3. Extensions

Extensions define message types and operations. Messages with GitMsg headers MUST declare the extension:

### 3.1. Extension Requirements

- MUST store data under `refs/gitmsg/<extension-name>/`
- MUST validate extension compatibility before processing
- SHOULD handle unknown extensions gracefully
- Configuration stored at `refs/gitmsg/<extension-name>/config` as JSON

### 3.2. Manifest

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

Core fields (`type`, `author`, `email`, `time`, `ref`) are available to all extensions and do not need to be declared in the manifest.

## Appendix: Validation

- Header: `^--- GitMsg: (.*) ---$`
- Required Fields: `ext="[a-z][a-z0-9_-]*"`, `v="\d+\.\d+\.\d+"`, `ext-v="\d+\.\d+\.\d+"`
- Extension Name: `^[a-z][a-z0-9_-]*$`
- Version: `^\d+\.\d+\.\d+$`
- Reference (HTTPS): `^https?://[^#]+#[a-z]+:[a-zA-Z0-9_/-]+$`
- Reference (SSH): `^git@[^:]+:[^#]+#[a-z]+:[a-zA-Z0-9_/-]+$`
- Reference (Own): `^#[a-z]+:[a-zA-Z0-9_/-]+$`
- List Names: `^[a-zA-Z0-9_-]{1,40}$`
- Namespace: `^refs/gitmsg/[a-z][a-z0-9_-]*/.*$`

## Appendix: Examples

### Minimal Message

```
Hello world!
```

### Message with Reference

```
This is a response

--- GitMsg: ext="social"; type="comment"; original="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:30:00Z"; ref="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---
> Original message content
```

### Cross-Extension Example: PM + CI/CD Integration

Extensions can reference each other's messages to create integrated workflows.

Step 1 - Create feature request (pm extension):

```
Add dark mode support

Users can toggle between light and dark themes in settings.

--- GitMsg: ext="pm"; type="feature"; status="approved"; priority="high"; v="0.1.0"; ext-v="0.2.0" ---
```

Step 2 - Job references feature (cicd extension):

```
Job #142: Dark mode implementation

--- GitMsg: ext="cicd"; type="job"; status="success"; job-id="142"; implements="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="pm"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; priority="high"; ref="#commit:abc123456789"; v="0.1.0"; ext-v="0.2.0" ---
> Add dark mode support
```

Step 3 - Deployment job references both (cicd extension):

```
Deployed v1.2.0 to production

--- GitMsg: ext="cicd"; type="job"; status="success"; environment="production"; version="1.2.0"; depends-on="#commit:def234567890"; implements="#commit:abc123456789"; v="0.1.0"; ext-v="0.1.0" ---

--- GitMsg-Ref: ext="cicd"; author="Bob"; email="bob@example.com"; time="2025-01-06T11:00:00Z"; ref="#commit:def234567890"; v="0.1.0"; ext-v="0.1.0" ---
> Job #142: Dark mode implementation

--- GitMsg-Ref: ext="pm"; author="Alice"; email="alice@example.com"; time="2025-01-06T10:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"; ext-v="0.2.0" ---
> Add dark mode support
```
