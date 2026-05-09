# Social Extension

Posts, comments, reposts, and quotes stored as commits on the `gitmsg/social` branch. The timeline is driven by **lists** — named groups of repositories the workspace follows.

> **Spec:** [GITSOCIAL.md](../specs/GITSOCIAL.md) — wire format for messages, fields, and lists.

## Implicit vs. explicit

On the configured branch (default `gitmsg/social`), commits without a `GitMsg:` trailer are **implicit posts** — plain `git commit` is a valid post. Commits with a `GitMsg:` trailer are **explicit interactions** (`comment`, `repost`, `quote`) or edits/retracts. All other branches are ignored.

## Initialize

```
gitsocial social init                  # creates refs/gitmsg/social/config and the gitmsg/social branch
gitsocial social init -b <branch>      # initialize on a custom branch
gitsocial social config get / set / list   # read/write config keys
```

`init` is idempotent. Branch resolution follows GITMSG.md Section 3.3.

## Author content

```
gitsocial social post "Hello world"
gitsocial social comment <ref> "Great idea!"
gitsocial social repost <ref>
gitsocial social quote <ref> "Worth reading:"

gitsocial social edit <ref> "Updated text"
gitsocial social retract <ref>
```

Edits and retracts use the core versioning chain (`edits` + `retracted="true"`); the latest version wins in queries.

Comments reply to the **thread root** via `original`. Nested replies add `reply-to` pointing at the parent comment, while `original` still points at the root post.

## Lists and timeline

Lists are named sets of `<url>#branch:<branch>` entries. The timeline queries posts from the union of all lists (or one, with `--list`).

```
gitsocial social list create following
gitsocial social list add following https://github.com/user/repo
gitsocial social list add following https://github.com/user/repo --branch '*'   # follow all branches
gitsocial social list remove following https://github.com/user/repo
gitsocial social list show                       # all lists
gitsocial social list show following             # one list
gitsocial social list repo <repo-url>            # lists defined by a remote repo
```

```
gitsocial social timeline                  # all lists, newest first
gitsocial social timeline -l following     # one list
gitsocial social timeline -r workspace     # workspace only
gitsocial social timeline -n 50            # limit
```

Lists are stored under `refs/gitmsg/social/lists/<name>/` (one ref per member; metadata at `_meta`). Adds and removes from concurrent clones don't collide.

## Followers

A repository is treated as a **follower** of the workspace when its lists include the workspace URL. Followers are detected during fetch (when followed repositories' list refs are scanned) and recorded in `social_followers`.

```
gitsocial social followers              # who follows the workspace
gitsocial social followers --json
```

Following someone is just adding their repo to a list; "follow back" is symmetrical.

## Fetch

```
gitsocial social fetch                  # fetch all repos in all lists
```

This wraps the core fetch with social-only processors. The general `gitsocial fetch` does the same plus all other extensions.

## How posts surface in queries

Default `social timeline` filters:

- Excludes retracted (latest version wins).
- Excludes commits removed from the source branch (force-pushed away → marked stale).
- Includes implicit posts (no trailer) and explicit posts on the configured branch only.

Order: newest first by effective timestamp (origin-time wins for imported content).

## Notifications

Mentions (`@email`), replies, comments on workspace posts, and reposts of workspace posts surface through core notifications. See [NOTIFICATIONS.md](NOTIFICATIONS.md) for the trailer-driven aggregation model.

## Operational checks

```bash
# What's the social config?
gitsocial social config get

# Raw cache state (posts on the configured branch)
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT type, COUNT(*) FROM social_items_resolved
     WHERE branch = 'gitmsg/social' GROUP BY type"

# Who follows this workspace?
sqlite3 ~/.cache/gitsocial/cache.db \
    "SELECT workspace_url, repo_url, detected_at FROM social_followers"
```

The TUI's social section (`T` from any screen — Timeline) provides the post list, lists, repositories, and threaded discussion views. See [TUI-KEYS.md](TUI-KEYS.md) for per-view bindings.
