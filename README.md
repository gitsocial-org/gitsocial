<div align="center">

  <img src="documentation/images/gitsocial-icon.svg" width="120" height="120">
  <h1>GitSocial</h1>

  *Git-native collaboration platform*

[About](#about) · [Installation](#installation) · [Quick Start](#quick-start) · [Documentation](#documentation) · [Contributing](#contributing)

</div>

## About

GitSocial is a CLI/TUI Go binary that stores issues, PRs, and other collaboration in git itself, as commits with [structured trailers](specs/GITMSG.md) on `gitmsg/*` branches.

A single idempotent command mirrors a project from GitHub or other hosts to an S3-compatible bucket you own; the bucket serves a [complete static site](documentation/STATIC-SITE.md) of the project (code, timeline, issues, releases, etc.), and doubles as a plain `git clone` source:

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix>
```

[GitSocial.org](https://gitsocial.org) is served this way. The forge becomes optional: issues, PRs, releases, and posts can be created with the CLI/TUI ([demo](documentation/demo/demo.mp4)) and pushed like any other commits, and other repositories followed through a [timeline](documentation/SOCIAL.md).

## Installation

#### macOS / Linux with Homebrew

```bash
brew trust gitsocial-org/tap
brew install gitsocial-org/tap/gitsocial
```

Or using installation script

```bash
curl -fsSL https://gitsocial.org/install.sh | sh
```

#### Windows
```bash
scoop bucket add gitsocial https://github.com/gitsocial-org/scoop-bucket.git
scoop install gitsocial
```

#### Go
```bash
go install github.com/gitsocial-org/gitsocial/cli/gitsocial@latest
```

Or download a binary from [releases](https://gitsocial.org/releases/index.html).

## Quick Start

#### Mirror a project

```bash
gitsocial mirror https://github.com/owner/repo s3://<endpoint>/<bucket>/<prefix> --url https://your-domain/
```

`--url` is the site's public address. `mirror` asks for the bucket credentials on first run; public access and the domain are one-time provider-dashboard steps. On a large repository try `-n 100` first ([all flags](documentation/CLI.md#gitsocial-mirror)).

#### Explore a repository in the terminal

Clone it from GitHub or any host, then from the project directory:

```bash
gitsocial import     # import issues, PRs, etc
gitsocial tui        # explore in the terminal
```

#### Host your own repository on a bucket

```bash
gitsocial config credentials set s3.example.com    # paste the access + secret key
gitsocial remote add s3://s3.example.com/mybucket/myrepo --default --site
gitsocial push
```

Anyone can then fetch it with `gitsocial clone s3://s3.example.com/mybucket/myrepo`, or with plain `git clone` from the bucket's public URL.

## Documentation

### Concepts

| Document | Description |
|----------|-------------|
| [GitMsg Protocol](specs/GITMSG.md) | Core message format, headers, refs, versioning |
| [S3 Remote](documentation/S3.md) | Buckets as git remotes, canonical URLs |
| [Static Site](documentation/STATIC-SITE.md) | Repo website served from the bucket: timeline, issues, PRs, releases, code |
| [Identity Verification](documentation/IDENTITY.md) | Decentralized trust model, attestation sources, caching |
| [Notifications](documentation/NOTIFICATIONS.md) | Notification types, scopes, and triggers |

### Extensions

| Document | Description | Spec |
|----------|-------------|------|
| [Social](documentation/SOCIAL.md) | Posts, comments, lists, timeline, followers | [GitSocial](specs/GITSOCIAL.md) |
| [PM](documentation/PM.md) | Issues, milestones, sprints, labels, boards | [GitPM](specs/GITPM.md) |
| [Review](documentation/REVIEW.md) | Pull requests, feedback, forks, version tracking, cross-forge scenarios | [GitReview](specs/GITREVIEW.md) |
| [Release](documentation/RELEASE.md) | Releases, artifacts, checksums, signatures, SBOM | [GitRelease](specs/GITRELEASE.md) |
| [Memo](documentation/MEMO.md) | Tiered memos for knowledge as commits | — |

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

[MIT](LICENSE)
