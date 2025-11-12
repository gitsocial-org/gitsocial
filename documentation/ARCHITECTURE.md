# GitSocial Architecture

## System Overview

GitSocial is a decentralized open source social network built entirely on Git and the GitMsg protocol.

### Layer Architecture

```
┌─────────────────────────────────────┐
│         Social Layer                │
├─────────────────────────────────────┤
│      GitMsg Layer (Protocol)        │
├─────────────────────────────────────┤
│       Git Layer (Core)              │
└─────────────────────────────────────┘
```

**Dependencies:** Social → GitMsg → Git (no circular references)

## Key Architectural Decisions

### 1. Functional Programming

Namespace objects and functions only - no classes.

### 2. Global Cache Architecture

**Requirements**: Atomic transformation for cross-repository references and interaction counting.

**Async Operations**: All cache operations (`getCachedPosts()`, `getPosts()`) are async and return Promises. Auto-initialization ensures cache is ready before returning results. Callers must use `await`.

**State Management**: Uses `CacheState` enum (UNINITIALIZED → INITIALIZING → READY/ERROR/REFRESHING) to track initialization status. Promise-based initialization prevents duplicate concurrent loads.

**Initialization**: Load ALL posts from ALL sources on first query or Git state change:

- Workspace repository posts
- External repository posts from lists
- Virtual posts from references (placeholders when real post missing)
- Single instance per post ID (deduplication)
- Auto-initializes via `ensureInitialized()` on first query

**Transformation Pipeline** (must stay together in cache.ts):

1. Load commits from Git (single operation)
2. Create Post objects from commits
3. Enrich posts with references (creates virtual posts)
4. Calculate global interaction counts
5. Cache frozen objects with LRU eviction

**Invalidation**: Triggered by new commits, branch switches, or manual refresh.

**Performance**: 50ms cache hit, 500-1000ms miss for 1000 posts.

**Progressive Loading**: Start with current week only for fast startup. Historical data fetched on-demand as users navigate backwards. Repositories track `fetchedRanges` in git config to prevent redundant fetching - only gaps are fetched, not entire history. Cache expands incrementally: check memory → check disk → fetch remote.

### 3. State-Based Storage

Lists/config use JSON snapshots at Git refs - O(1) read, no event reconstruction. Lists maintain commit history chains while storing complete state in each commit.

### 4. Isolated Repository Storage

External repos in app storage with bare, blobless clones (90% size reduction).

## Layer Usage

**GitMsg Layer**: Protocol operations, references, list storage, message formatting
**Git Layer**: Branch/remote operations, repository state, raw commits, config
**Both**: Post creation (Git commit + GitMsg formatting)

## Data Flow

### Post Loading

`Client → await getPosts(scope) → await getCachedPosts() → [Auto-initialize if needed] → Direct Filter → Enriched Posts`

**Scopes**: `timeline` (all), `repository:my` (workspace), `list:{name}`, `post:{id}`

**Rules**: All posts via `getPosts()` API (with `await` since it's async). No post creation/counting outside cache.ts.

### State Storage

`Client → getLists() → JSON at refs/gitmsg/social/lists/<name> → List Objects`

Posts use event stream (commits), Lists/Config use JSON snapshots with commit history (O(1) read, queryable history).

## Performance & Constants

**Performance**: Cache <50ms hit, 500-1000ms miss (1000 posts), ~1.5KB/post, Search <100ms (10K posts)

**Constants**: 12-char hashes, `[a-zA-Z0-9_-]{1,40}` list names, `url#branch:name` repos, `url#commit:hash` posts

## Repository Architecture

**Workspace**: Direct access, contains lists/config
**External**: App storage, bare blobless clones, auto-deduplication
**Identification**: GitMsg refs (`url#commit:hash` posts, `url#branch:name` repos)

## Core Principles

**Design**: Functional (namespaces only), Direct Node.js APIs, Standard parameters (workdir/repository/scope)

**APIs**: `getPosts()` (all post retrieval), `getRepositories()` (repo management)

**Interfaces**: Reuse types.ts, extend don't duplicate, no one-offs, clear naming, JSDoc comments

## Project Structure

```
packages/
├── core/
│   ├── git/              # Git operations
│   ├── gitmsg/           # Protocol (protocol.ts, lists.ts, parser/)
│   ├── githost/          # Git hosting
│   ├── repository/       # Repo management
│   ├── social/           # Social features
│   │   ├── cache.ts      # Global cache & transformation
│   │   ├── posts.ts      # Post operations
│   │   ├── lists.ts      # List management
│   │   └── types.ts      # Core types
│   ├── client/           # Browser exports
│   └── utils/            # Utilities
└── vscode/
    ├── webview/          # UI components
    └── handlers/         # Message handlers
```

**Storage**: Config/Lists at `refs/gitmsg/social/*`, External repos in app storage

## Related Documentation

[CONTRIBUTING.md](CONTRIBUTING.md) | [START.md](START.md) | [PATTERNS.md](PATTERNS.md) | [INTERFACES.md](INTERFACES.md) | [GITMSG.md](GITMSG.md) | [GITSOCIAL.md](GITSOCIAL.md) | [AVATAR.md](AVATAR.md)
