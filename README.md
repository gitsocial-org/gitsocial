<div align="center">

  <h1>GitSocial</h1>

  *Git-native collaboration platform*

</div>

## About

GitSocial is an open source Go binary that stores issues, pull requests, comments, and other data in git. It can push that data to your own S3 bucket, which then serves as both a git remote and a static site. [GitSocial.org](https://gitsocial.org) runs this way.

Each issue, pull request or comment is a commit with GitMsg trailers on a `gitmsg/*` branch. The GitSocial CLI and TUI can create and browse these items ([demo](documentation/demo/demo.mp4)).

## Install

### macOS / Linux with Homebrew

```bash
brew trust gitsocial-org/tap
brew install gitsocial-org/tap/gitsocial
```

### Windows

```bash
scoop bucket add gitsocial https://github.com/gitsocial-org/scoop-bucket.git
scoop install gitsocial
```

### Other

- Install script: `curl -fsSL https://gitsocial.org/install.sh | sh`
- Go: `go install github.com/gitsocial-org/gitsocial/cli/gitsocial@latest`
- Binary: download from the [releases](https://gitsocial.org/releases/index.html) page

## Quick Start

_Public access to a bucket is a one-time step in the provider's dashboard._

### Mirror a project

```bash
gitsocial mirror https://github.com/owner/repo s3://s3.example.com/mybucket/repo    # first run asks for credentials
```

On a large repository, try `-n 100` first to cap the items per type. `--url https://example.org/` turns the HTML pages on at a domain you set up in the same dashboard.

### Explore a repository in the terminal

Clone it from GitHub or GitLab, then from the project directory:

```bash
gitsocial import     # import issues, PRs, etc. from GitHub or GitLab
gitsocial tui        # explore in the terminal
```

### Host your own repository on a bucket

```bash
gitsocial config credentials set s3.example.com    # paste the access + secret key
gitsocial remote add s3://s3.example.com/mybucket/myrepo --default --site
gitsocial push
```

Anyone can then fetch it with `gitsocial clone s3://s3.example.com/mybucket/myrepo`, or with plain `git clone` from the bucket's public URL.

## Documentation

### Core

| Document | Description |
|----------|-------------|
| [GitMsg Protocol](specs/GITMSG.md) | Core message format, headers, refs, versioning |
| [S3 Remote](documentation/S3.md) | Buckets as git remotes, canonical URLs |
| [Static Site](documentation/STATIC-SITE.md) | Repo website served from the bucket: timeline, issues, PRs, releases, code |
| [Identity Verification](documentation/IDENTITY.md) | Attestation sources, commands, caching |
| [Notifications](documentation/NOTIFICATIONS.md) | Notification types, scopes, and triggers |
| [Settings](documentation/SETTINGS.md) | User preferences, keys, sync across machines |

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
| [CLI](documentation/CLI.md) | Commands, flags, output formats |
| [TUI](documentation/TUI-DIAGRAMS.md) | Per-view layouts, [keybindings](documentation/TUI-KEYS.md) |
| [JSON-RPC](documentation/RPC.md) | Client integration over stdio |
| [Agent Skill](https://github.com/gitsocial-org/gitsocial-agent-skill) | AI-assisted workflows for Claude Code, Cursor, and other agents |

### Development

| Document | Description |
|----------|-------------|
| [Contributing](documentation/CONTRIBUTING.md) | Fork, build, submit a pull request, report a bug |
| [Architecture](documentation/ARCHITECTURE.md) | Layers, packages, cache schema, TUI structure |
| [Style](documentation/STYLE.md) | Prose, help text, errors, comments, commits |
| [Static Site Design](documentation/STATIC-SITE-DESIGN.md) | Tokens, components, states, visual tests |
| [Testing](documentation/TESTING.md) | Test tiers, gate stages, coverage, supported platforms |

## License

[MIT](LICENSE)
