# Social Extension

Posts, comments, reposts and quotes are commits on the `gitmsg/social` branch ([GITSOCIAL.md](../specs/GITSOCIAL.md)), and the timeline is the union of the lists a workspace follows.

[Initialize](#initialize) · [Post](#post) · [Lists and timeline](#lists-and-timeline) · [Followers](#followers) · [Reference](#reference)

## Initialize

```
gitsocial social init [-b <branch>]        # refs/gitmsg/social/config and the gitmsg/social branch
gitsocial social config get|set|list
```

`init` is idempotent. On the configured branch a commit without a `GitMsg:` trailer is a post, so a plain `git commit` there posts. Commits with a trailer are comments, reposts, quotes, edits or retractions. Other branches are ignored.

## Post

```
gitsocial social post "Hello world" [-l kind/note]
gitsocial social comment <ref> "Great idea!"
gitsocial social repost <ref>
gitsocial social quote <ref> "Worth reading:"
gitsocial social edit <ref> "Updated text"
gitsocial social retract <ref>
```

A comment's `original` is the thread's root post; a nested reply adds `reply-to` for its parent. Edits and retractions use core versioning, and the latest version wins.

## Lists and timeline

A list is a named set of repositories. The timeline shows posts from every list, or from one list with `-l`.

```
gitsocial social list create following
gitsocial social list add following https://github.com/user/repo [--all-branches]
gitsocial social list remove following https://github.com/user/repo
gitsocial social list show [following]
gitsocial social list ls
gitsocial social list repo <repo-url>       # the lists a remote repository publishes
gitsocial social timeline [-l following] [-r workspace] [-n 50]
gitsocial social fetch                      # every repository in every list; `gitsocial fetch` does this and more
```

## Followers

A repository follows the workspace when one of its lists contains the workspace URL. Followers are detected during fetch.

```
gitsocial social followers [--json]
```

## Reference

- Branch resolution follows [GITMSG.md §3.4](../specs/GITMSG.md#34-branch-resolution).
- Lists live at `refs/gitmsg/social/lists/<name>/`, one ref per member and metadata at `_meta` ([ARCHITECTURE.md](ARCHITECTURE.md#refs-and-keys)), so adds from concurrent clones do not collide.
- The timeline excludes retracted posts ([GITMSG.md §1.5](../specs/GITMSG.md#15-versioning)) and commits no longer on their branch ([ARCHITECTURE.md](ARCHITECTURE.md#cache)), and orders by effective timestamp, newest first; imported content sorts by its origin time.
- Mentions, replies, comments and reposts of workspace posts raise [notifications](NOTIFICATIONS.md#types).
- In the TUI, `S` opens the timeline ([TUI-KEYS.md](TUI-KEYS.md#social-extension)).
