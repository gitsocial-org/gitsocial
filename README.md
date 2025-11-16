<div align="center">

  <img src="documentation/images/gitsocial-icon.png" width="120" height="120">
  <h1>GitSocial</h1>

  *Decentralized, open-source, Git-native social network*

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
  [![GitMsg Protocol](https://img.shields.io/badge/GitMsg-v0.1.0-blue)](https://github.com/gitsocial-org/gitsocial/blob/main/documentation/GITMSG.md)
  [![GitSocial Extension](https://img.shields.io/badge/GitSocial%20Protocol-v0.1.0-blue)](https://github.com/gitsocial-org/gitsocial/blob/main/documentation/GITSOCIAL.md) <br />
  [![Beta](https://img.shields.io/badge/Status-Beta-orange)](https://github.com/gitsocial-org/gitsocial)
  [![VS Code Marketplace Version](https://img.shields.io/visual-studio-marketplace/v/gitsocial.gitsocial?label=VS%20Code%20Marketplace)](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial)
  [![VS Code Marketplace Installs](https://img.shields.io/visual-studio-marketplace/i/gitsocial.gitsocial)](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial)
  [![codecov](https://codecov.io/gh/gitsocial-org/gitsocial/branch/main/graph/badge.svg)](https://codecov.io/gh/gitsocial-org/gitsocial)
  [![CI](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml/badge.svg)](https://github.com/gitsocial-org/gitsocial/actions/workflows/ci.yml)

</div>

## About

GitSocial is a decentralized social network built entirely on Git. Your posts, interactions, and social graph are stored as commits in standard Git repositories. You have complete ownership of your data, everything works offline-first, and syncing happens through normal Git operations.

GitSocial uses only standard Git features: commits store your posts, branches organize content, and references track your lists. This means it works anywhere Git works (GitHub, GitLab, self-hosted, or local repositories) and integrates naturally with existing Git workflows.

![GitSocial Timeline](documentation/images/screenshot.png)

## How It Works

### Posts are commits

When you create a post, GitSocial writes a commit to your `gitsocial` branch. Comments, reposts, and quotes also become commits. GitMsg headers link these interactions to their parent posts, creating conversation threads.

### Following is list-based

Instead of following individual accounts, you organize repositories into custom lists like "OSS" or "AI". Your timeline shows posts from all repositories across your lists. Since lists are stored as Git references in your repository, your social graph stays portable and version-controlled.

### Syncing is through Git

Updates from repositories you follow come through standard `git fetch` commands. Sharing your posts uses `git push` and `git pull`. No special servers or APIs needed: if you can use Git, you can use GitSocial.

## Installation

Choose one of these methods:

- VS Code Marketplace: Install directly from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=gitsocial.gitsocial)
- Extensions Panel: Search for "GitSocial" in VS Code's Extensions panel
- Manual Installation: Download the `.vsix` file from [GitHub Releases](https://github.com/gitsocial-org/gitsocial/releases)

## Quick Start

1. Install the GitSocial VS Code extension
2. Open the GitSocial panel in VS Code's sidebar and write your first post
3. Create a list and add repository URLs to start following them
4. View your timeline to see posts from repositories you follow
5. Interact with posts through comments, reposts, or quotes

## Documentation

### Specifications

- [GITSOCIAL.md](documentation/GITSOCIAL.md) - GitSocial protocol specification, social extension to GitMsg protocol
- [GITMSG.md](documentation/GITMSG.md) - GitMsg message protocol specification

### Development

- [CONTRIBUTING.md](documentation/CONTRIBUTING.md) - Setup, testing, and development workflow
- [ARCHITECTURE.md](documentation/ARCHITECTURE.md) - System design and decisions
- [PATTERNS.md](documentation/PATTERNS.md) - Code patterns and conventions
- [INTERFACES.md](documentation/INTERFACES.md) - Type reference
- [START.md](documentation/START.md) - LLM guide

## License

MIT
