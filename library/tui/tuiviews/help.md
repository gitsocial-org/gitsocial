# GitSocial

Git-native collaboration — posts, issues, PRs, releases, all in your repo.

## How It Works

Everything is a commit: posts, issues, PRs, reviews, releases — all stored
as git commits on `gitmsg/*` branches. Syncing is git: fetch updates, publish
with push. Works offline, air-gapped, and peer-to-peer. Move to any host
with `git clone --mirror` — no API scraping, no data loss.

## Workflow

- **Fetch** — Pull updates from repositories you follow
- **Push** — Publish your local changes to your remote
- **Lists** — Group repositories into curated feeds

Follow someone by adding their repo to a list. Their posts appear in your
timeline after fetch.

## Extensions

- **Social** — Posts, comments, reposts, quotes, timeline, lists, followers
- **PM** — Issues, milestones, sprints, boards
- **Review** — Pull requests, inline feedback, fork PRs, merges
- **Release** — Releases, artifacts, checksums, signatures

Each extension stores data on its own `gitmsg/*` branch and can be enabled
or disabled in Settings.

## TUI Layout

- Left panel: navigation sidebar (toggle with `` ` ``)
- Right panel: content area for the active view
- Footer: shows available keys for the current view
