# GitSocial

GitSocial keeps posts, issues, pull requests and releases as commits in your
repository.

## How It Works

Each item is a commit: posts, issues, pull requests, feedback and releases
are git commits on `gitmsg/*` branches. Fetch and push are git operations:
fetch gets updates and push sends your changes. GitSocial works
offline, air-gapped and peer-to-peer. `git clone --mirror` moves the data to a
different host, with no API scraping and no data loss.

## Workflow

- **Fetch**: get updates from the repositories that you follow
- **Push**: send your local changes to your remote
- **Lists**: put repositories into named sets for your timeline

To follow a person, add their repository to a list. Their posts show in
your timeline after a fetch.

## Extensions

- **Social**: posts, comments, reposts, quotes, timeline, lists, followers
- **PM**: issues, milestones, sprints, boards
- **Review**: pull requests, inline feedback, fork pull requests, merges
- **Release**: releases, artifacts, checksums, signatures
- **Memo**: notes across session, personal, project and inherited tiers

Each extension stores data on its own `gitmsg/*` branch, and you can
enable or disable it in Settings.

## TUI Layout

- Left panel: navigation sidebar
- Right panel: content area for the active view
- Footer: the keys for the current view

`tab` moves focus between the two panels.
