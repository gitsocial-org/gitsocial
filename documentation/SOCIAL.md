# Social Extension

Posts, comments, reposts and quotes are commits ([GITSOCIAL.md](../specs/GITSOCIAL.md)), written to the `gitmsg/social` branch unless `init` names another. The timeline is the union of the lists a workspace follows.

[Initialize](#initialize) · [Post](#post) · [Blog](#blog) · [Lists and timeline](#lists-and-timeline) · [Followers](#followers) · [Reference](#reference)

## Initialize

```
gitsocial social init [-b <branch>]
gitsocial social config get|set|list
```

`init` is idempotent. It creates `refs/gitmsg/social/config` and the `gitmsg/social` branch. `-b` names the branch that `post`, `comment`, `repost` and `quote` write to.

On each branch that the timeline reads, a commit without a `GitMsg:` trailer is a post, so a plain `git commit` is also a post. Commits with a trailer are comments, reposts, quotes, edits or retractions.

## Post

```
gitsocial social post "Hello world" [-l kind/note]
gitsocial social comment <ref> "Great idea!"
gitsocial social repost <ref>
gitsocial social quote <ref> "Worth reading:"
gitsocial social edit <ref> "Updated text"
gitsocial social retract <ref>
```

The `original` of a comment is the root post of the thread, and a nested reply adds `reply-to` for its parent. Edits and retractions use core versioning, and the latest version has priority.

## Blog

A blog uses one branch and has no `gitmsg/social` branch.

```
gitsocial social init -b main
git commit -m "Hello world"                 # a plain commit on main is a post
gitsocial social post "A shorter note"
gitsocial social edit <ref> "Updated text"
gitsocial social retract <ref>
```

`edit` and `retract` write to the branch that the post is on; for a blog, this is `main`. A follower names that branch with `list add -b main`, or lets `list add` record the repository's default branch.

## Lists and timeline

A list is a named set of repositories. The timeline shows posts from every list, or from one list with `-l`.

```
gitsocial social list create following
gitsocial social list add following https://github.com/user/repo [-b <branch> | --all-branches]
gitsocial social list remove following https://github.com/user/repo
gitsocial social list show [following]
gitsocial social list ls
gitsocial social list delete following
gitsocial social list repo <repo-url>       # the lists a remote repository publishes
gitsocial social timeline [-l following] [-r workspace] [-n 50]
gitsocial social fetch                      # every repository in every list; `gitsocial fetch` does this and more
```

`list add` fetches the URL that you type; `list remove` accepts each form of it, because GitSocial compares URLs by identity. A repository is in a list one time, on one branch or on all branches. The timeline reads the branch that the member names, the `gitmsg/social` branch of the repository, and all branches of the workspace.

## Followers

A repository follows the workspace when one of its lists contains the workspace URL. GitSocial finds followers during a fetch.

```
gitsocial social followers [--json]
```

## Reference

- Where social messages are stored is specified in [GITMSG.md §3.4](../specs/GITMSG.md#34-content-branch).
- Lists are at `refs/gitmsg/social/lists/<name>/`, one ref per member and metadata at `_meta` ([ARCHITECTURE.md](ARCHITECTURE.md#refs-and-keys)).
- The timeline excludes retracted posts ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and stale commits ([ARCHITECTURE.md](ARCHITECTURE.md#cache)). It sorts by effective timestamp, newest first, and imported content sorts by its origin time.
- The comment, repost and quote counts of a card include only the items that are not retracted, so retracting a comment decreases the comment count.
- `gitsocial social log` lists an item by its own type: a comment on a post is a comment, not a post.
- Mentions, replies, comments and reposts of workspace posts create [notifications](NOTIFICATIONS.md#types).
- In the TUI, `S` opens the timeline ([TUI-KEYS.md](TUI-KEYS.md#social-extension)).
