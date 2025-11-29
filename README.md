<div align="center">

  <img src="documentation/images/gitsocial-icon.png" width="120" height="120">
  <h1>GitSocial</h1>

  *Decentralized open-source Git-native social network*

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
  [![GitMsg Protocol](https://img.shields.io/badge/GitMsg-v0.1.0-blue)](https://github.com/gitsocial-org/gitsocial/blob/main/documentation/GITMSG.md)
  [![GitSocial Extension](https://img.shields.io/badge/GitSocial%20Protocol-v0.1.0-blue)](https://github.com/gitsocial-org/gitsocial/blob/main/documentation/GITSOCIAL.md) <br />
  [![Beta](https://img.shields.io/badge/Status-Beta-orange)](https://github.com/gitsocial-org/gitsocial)
  [![VS Code Marketplace Version](https://img.shields.io/visual-studio-marketplace/v/gitsocial.gitsocial?label=VS%20Code%20Marketplace)](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial)
  [![VS Code Marketplace Installs](https://img.shields.io/visual-studio-marketplace/i/gitsocial.gitsocial)](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial) <br />
  [![CI](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml/badge.svg)](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml)
  [![CodeQL](https://github.com/gitsocial-org/gitsocial/actions/workflows/codeql.yml/badge.svg)](https://github.com/gitsocial-org/gitsocial/actions/workflows/codeql.yml)
  [![codecov](https://codecov.io/gh/gitsocial-org/gitsocial/branch/main/graph/badge.svg)](https://codecov.io/gh/gitsocial-org/gitsocial)

</div>

## About

GitSocial is a decentralized social network built entirely on Git: posts are commits, follows are lists of repositories stored as references, and syncing uses `git fetch`/`push`. You own your data and your social graph, with offline-first support on GitHub, GitLab, self-hosted, or local repositories.

![GitSocial Timeline](documentation/images/screenshot.png)

## How It Works

### Posts are commits

Every post is a commit on your `gitsocial` branch, as are comments, reposts, and quotes, which link to their parent posts via GitMsg headers to form conversation threads.

### Follows are lists

Follows are lists of repositories like "OSS" or "AI", stored as Git references, with posts from your lists appearing in your timeline.

### Syncing via Git

Updates use `git fetch`, publishing uses `git push`, with no special servers or APIs needed.

## Installation

Choose one of these methods:

- VS Code Marketplace: Install directly from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial)
- Extensions Panel: Search for "GitSocial" in VS Code's Extensions panel
- Manual Installation: Download the `.vsix` file from [GitHub Releases](https://github.com/gitsocial-org/gitsocial/releases)

## Quick Start

1. Open GitSocial in VS Code's sidebar, create a list, and add repositories to see them on your timeline
2. Write posts, comment on others, and publish using push

## Documentation

### Specifications

- [GITSOCIAL.md](documentation/GITSOCIAL.md) - GitSocial protocol specification, social extension to GitMsg protocol
- [GITMSG.md](documentation/GITMSG.md) - GitMsg message protocol specification

### Development

- [CONTRIBUTING.md](documentation/CONTRIBUTING.md) - Setup, testing, and development workflow
- [ARCHITECTURE.md](documentation/ARCHITECTURE.md) - System design and decisions
- [PATTERNS.md](documentation/PATTERNS.md) - Code patterns and conventions
- [INTERFACES.md](documentation/INTERFACES.md) - Type reference
- [TESTING.md](documentation/TESTING.md) - Testing guide
- [START.md](documentation/START.md) - LLM guide

## License

MIT
