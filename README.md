# GitSocial

*Git-native collaboration platform*

---

Git made code portable, but nothing around the code is: a clone carries every commit, author, signature and branch, while the issues, pull requests and comments stay in the host's database, and the notes of each AI agent stay in its own files.

GitSocial's philosophy is that the commit was always enough: it has an author and a time, it carries a signature, and its message carries structured trailers, so an issue or a pull request can be stored as a commit with the right trailers. GitSocial writes this collaboration data on `gitmsg/*` branches alongside the code, following the [GitMsg](specs/GITMSG.md) protocol.

GitSocial is a single open source binary: run `gitsocial tui` or `gitsocial serve` in any git repository to explore it, or `gitsocial import` to bring the issues, pull requests and releases of a project from GitHub or GitLab. You can also publish the repository and a static site to an S3 bucket with `gitsocial push`; [GitSocial.org](https://gitsocial.org) is hosted this way on Cloudflare R2.

For data sovereignty, static hosting, portability, AI agent memory, verified identity and the alternatives, see [Why GitSocial](documentation/WHY-GITSOCIAL.md).

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

GitSocial needs git 2.38 or later.

## Quick Start

In any cloned repository, show the code, the commits and the tags in the browser:

```bash
gitsocial serve
```

To bring the issues, pull requests and releases from GitHub or GitLab:

```bash
gitsocial import -n 20
```

`-n` caps the items per type. Drop it for a full import, which can take a while on a large repository.

## Terminal UI

Use the TUI to browse and create the same items and to configure the repository ([demo](documentation/demo/demo.gif)):

```bash
gitsocial tui
```

## Publish to a bucket

Any S3-compatible bucket works, whether AWS S3, Cloudflare R2, DigitalOcean Spaces or Hetzner Object Storage, and it serves as a git remote and a static site at the same time.

To mirror a GitHub or GitLab project into a bucket:

```bash
gitsocial mirror https://github.com/owner/repo s3://s3.example.com/mybucket/repo
```

To publish your own repository:

```bash
gitsocial config credentials set s3.example.com
gitsocial remote add s3://s3.example.com/mybucket/myrepo --default --site
gitsocial push
```

A private bucket needs no further setup, and anyone with the credentials can clone it:

```bash
gitsocial clone s3://s3.example.com/mybucket/myrepo
```

For a public project, enable public access in the dashboard of the hosting provider, and anyone can then `git clone` from the public URL of the bucket. To serve the HTML pages at your own domain, configure the domain in the same dashboard, then set the URL and push again:

```bash
gitsocial config site set url https://example.org/
gitsocial push
```

## Documentation

### Core

| Document | Description |
|----------|-------------|
| [GitMsg Protocol](specs/GITMSG.md) | Core message format, headers, refs, versioning |
| [S3 Remote](documentation/S3.md) | Buckets as git remotes, clone, push, thin fork buckets |
| [Static Site](documentation/STATIC-SITE.md) | Repository website served from the bucket: timeline, issues, pull requests, releases, code |
| [Identity Verification](documentation/IDENTITY.md) | Attestation sources, commands, caching |
| [Notifications](documentation/NOTIFICATIONS.md) | Notification types, scopes, and triggers |
| [Settings](documentation/SETTINGS.md) | User preferences, keys, `personal sync` across machines |

### Extensions

| Document | Description | Spec |
|----------|-------------|------|
| [Social](documentation/SOCIAL.md) | Posts, comments, lists, timeline, followers | [GitSocial](specs/GITSOCIAL.md) |
| [PM](documentation/PM.md) | Issues, milestones, sprints, labels, boards | [GitPM](specs/GITPM.md) |
| [Review](documentation/REVIEW.md) | Pull requests, feedback, forks, version tracking, cross-forge scenarios | [GitReview](specs/GITREVIEW.md) |
| [Release](documentation/RELEASE.md) | Releases, artifacts, checksums, signatures, SBOM | [GitRelease](specs/GITRELEASE.md) |
| [Memo](documentation/MEMO.md) | Tiered memos for knowledge as commits | none |

### Clients

| Document | Description |
|----------|-------------|
| [CLI](documentation/CLI.md) | Commands, flags, output formats |
| [TUI](documentation/TUI-DIAGRAMS.md) | Per-view layouts, [keybindings](documentation/TUI-KEYS.md) |
| [JSON-RPC](documentation/RPC.md) | Client integration over stdio |
| [Agent Skill](https://github.com/gitsocial-org/gitsocial-agent-skill) | AI-assisted workflows for Claude Code, Cursor and other agents |

### Development

| Document | Description |
|----------|-------------|
| [Contributing](documentation/CONTRIBUTING.md) | Fork, build, submit a pull request, report a bug |
| [Architecture](documentation/ARCHITECTURE.md) | Layers, packages, cache schema, TUI structure |
| [Style](documentation/STYLE.md) | Prose, help text, errors, comments, commits |
| [Testing](documentation/TESTING.md) | Test tiers, check stages, coverage, supported platforms |

## License

[MIT](LICENSE)
