<div align="center">

  <img src="documentation/images/gitsocial-icon.png" width="120" height="120">
  <h1>GitSocial</h1>

  *Git-native cross-forge collaboration platform*

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
  [![GitMsg Protocol](https://img.shields.io/badge/GitMsg-v0.1.0-blue)](specs/GITMSG.md)
  [![CI](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml/badge.svg)](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml)

[Why GitSocial](#why-gitsocial) • [How It Works](#how-it-works) • [Installation](#installation) • [Quick Start](#quick-start) • [Documentation](#documentation)

![GitSocial TUI](documentation/demo/demo.gif)

</div>

## Why GitSocial

Git solved distributed collaboration for code. GitSocial extends it beyond code, making your data portable and independent.

## How It Works

All collaboration data (posts, issues, PRs, etc.) is stored in your git repository as [git commits with structured trailers](specs/GITMSG.md) on `gitmsg/*` branches, syncing via `git fetch` and `git push`. Activity from other repositories appears in your timeline when you add them to your lists.

## Installation

**macOS / Linux**:
```bash
brew install gitsocial-org/tap/gitsocial
# or
curl -fsSL https://raw.githubusercontent.com/gitsocial-org/gitsocial/main/install.sh | sh
```

If macOS blocks the binary ("cannot verify developer"), run:
```bash
xattr -d com.apple.quarantine $(which gitsocial)
```

**Windows**:
```bash
scoop bucket add gitsocial https://github.com/gitsocial-org/scoop-bucket.git
scoop install gitsocial
```

**Go**:
```bash
go install github.com/gitsocial-org/gitsocial/library/gitsocial@latest
```

Or download a binary from [Releases](https://github.com/gitsocial-org/gitsocial/releases/latest).

## Quick Start

Clone your project from GitHub, GitLab, or any host, then from your project directory:

```bash
gitsocial import         # imports issues, PRs, releases, discussions
gitsocial tui            # explore in the terminal
```

## Documentation

### Concepts

| Document | Description |
|----------|-------------|
| [GitMsg Protocol](specs/GITMSG.md) | Core message format, headers, refs, versioning |
| [Identity Verification](documentation/IDENTITY.md) | Decentralized trust model, attestation sources, caching |
| [Notifications](documentation/NOTIFICATIONS.md) | Notification types, scopes, and triggers |

### Extensions

| Document | Description | Spec |
|----------|-------------|------|
| [Social](documentation/SOCIAL.md) | Posts, comments, lists, timeline, followers | [GitSocial](specs/GITSOCIAL.md) |
| [PM](documentation/PM.md) | Issues, milestones, sprints, labels, boards | [GitPM](specs/GITPM.md) |
| [Review](documentation/REVIEW.md) | Pull requests, feedback, forks, version tracking, cross-forge scenarios | [GitReview](specs/GITREVIEW.md) |
| [Release](documentation/RELEASE.md) | Releases, artifacts, checksums, signatures, SBOM | [GitRelease](specs/GITRELEASE.md) |

### Clients

| Document | Description |
|----------|-------------|
| [Agent Skill](https://github.com/gitsocial-org/gitsocial-agent-skill) | AI-assisted workflows for Claude Code, Cursor, and other agents |
| [TUI](documentation/TUI-DIAGRAMS.md) | Per-view layout diagrams (see also [keybindings](documentation/TUI-KEYS.md)) |
| [CLI](documentation/CLI.md) | Commands, flags, output formats |
| [JSON-RPC](documentation/RPC.md) | Client integration over stdio |

## Contributing

Platform issues and PRs are disabled on all mirrors. GitSocial uses its own tools for collaboration.

### Getting Started

1. Install GitSocial (see [Installation](#installation))
2. Fork the repository on any host (GitHub, GitLab, Codeberg, or self-hosted)
3. Clone your fork: `git clone https://your-host.com/you/gitsocial`
4. Read [Architecture](documentation/ARCHITECTURE.md) for system design, packages, and cache layout

### Submitting Pull Requests

```bash
git checkout -b feature/my-change         # make changes, commit

gitsocial review pr create \
  --base main \
  --head feature/my-change \
  "Short description of change"

git push origin feature/my-change         # push your branch
gitsocial push                            # push PR metadata
```

After your first push, request fork registration in the [Matrix room](https://matrix.to/#/!uZYlsFjjQgPmSBYJaY:matrix.org?via=matrix.org) so maintainers can discover your PRs and issues.

See [Review](documentation/REVIEW.md) for the full cross-forge PR workflow.

### Reporting Bugs & Requesting Features

```bash
gitsocial pm issue create "Bug: description"
gitsocial push
```

For quick questions or discussion, use the [Matrix room](https://matrix.to/#/!uZYlsFjjQgPmSBYJaY:matrix.org?via=matrix.org).

## License

MIT
