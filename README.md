<div align="center">

  <img src="documentation/images/gitsocial-icon.png" width="120" height="120">
  <h1>GitSocial</h1>

  *Git-native cross-forge collaboration: posts, issues, PRs, releases, all in your repo*

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
  [![GitMsg Protocol](https://img.shields.io/badge/GitMsg-v0.1.0-blue)](specs/GITMSG.md)
  [![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev) <br />
  [![CI](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml/badge.svg)](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml)
  [![codecov](https://codecov.io/gh/gitsocial-org/gitsocial/branch/main/graph/badge.svg)](https://codecov.io/gh/gitsocial-org/gitsocial)

[How It Works](#how-it-works) • [Extensions](#extensions) • [Interfaces](#interfaces) • [Installation](#installation) • [Quick Start](#quick-start) • [Documentation](#documentation)

![GitSocial Timeline](documentation/images/screenshot.png)

</div>

## How It Works

- Everything is a commit: posts, issues, PRs, releases stored on `gitmsg/*` branches
- Syncing is git: `git fetch` to update, `git push` to publish; works offline and peer-to-peer
- Portable: `git clone --mirror`, no export tools needed

## Extensions

| Extension | Status | Description |
|-----------|--------|-------------|
| **Social** | Stable | Posts, comments, reposts, lists, timeline |
| **PM** | Stable | Issues, milestones, sprints, boards |
| **Review** | Stable | Cross-forge PRs, version-aware reviews ([see more](documentation/GITREVIEW-FLOWS.md)) |
| **Release** | Stable | Releases, artifacts, checksums, signatures |

## Interfaces

- CLI: `gitsocial pm issue create "Dark mode"`
- TUI: `gitsocial tui`
- JSON: `gitsocial rpc`

## Installation

**Homebrew** (macOS / Linux):
```bash
brew install gitsocial-org/tap/gitsocial
```

**Scoop** (Windows):
```bash
scoop bucket add gitsocial https://github.com/gitsocial-org/scoop-bucket.git
scoop install gitsocial
```

**Shell script** (macOS / Linux):
```bash
curl -fsSL https://raw.githubusercontent.com/gitsocial-org/gitsocial/main/install.sh | sh
```

**Go**:
```bash
go install github.com/gitsocial-org/gitsocial/library/cli@latest
```

Or download a binary manually from [Releases](../../releases/latest).

## Quick Start

Import an existing project and explore it in the TUI:

```bash
git clone https://github.com/your/repo
cd repo
gitsocial import         # imports issues, PRs, releases, discussions
gitsocial tui
```

### CLI Walkthrough

Start from scratch with the CLI:

```bash
# Initialize GitSocial in your repo
cd repo
gitsocial social init

# Create a list and follow a repository
gitsocial social list create reading
gitsocial social list add reading https://github.com/someone/interesting-project

# Fetch updates and view timeline
gitsocial fetch
gitsocial social timeline

# Write a post
gitsocial social post "First post from GitSocial"

# Publish
gitsocial push
```

## Documentation

**[GitReview Flows](documentation/GITREVIEW-FLOWS.md) - PR workflows and cross-forge scenarios**

### Developer Docs

| Document | Description |
|----------|-------------|
| [Architecture](documentation/ARCHITECTURE.md) | System design, packages, cache, and dependencies |
| [CLI Reference](documentation/CLI.md) | Commands, flags, output formats, and scripting |
| [TUI Diagrams](documentation/TUI-DIAGRAMS.md) | ASCII layouts for every list and detail view |
| [JSON-RPC](documentation/RPC.md) | Client integration over stdio |

### Protocol Specs

| Spec | Description |
|------|-------------|
| [GITMSG.md](specs/GITMSG.md) | Core message format, headers, refs, and versioning |
| [GITSOCIAL.md](specs/GITSOCIAL.md) | Posts, comments, reposts, quotes, and lists |
| [GITPM.md](specs/GITPM.md) | Issues, milestones, sprints, and hierarchy |
| [GITREVIEW.md](specs/GITREVIEW.md) | Pull requests, inline feedback, and review states |
| [GITRELEASE.md](specs/GITRELEASE.md) | Releases with artifacts, checksums, and signatures |

## License

MIT
