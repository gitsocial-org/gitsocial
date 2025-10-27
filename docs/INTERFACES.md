# Type Reference Guide

## Core Types (ALWAYS REUSE THESE)

### Post (`social/types.ts`)
```typescript
interface Post {
  id: string;                    // GitMsg ref format
  repository: string;            // url#branch format
  branch?: string;
  author: { name: string; email: string; };
  timestamp: Date;
  content: string;
  type: 'post' | 'comment' | 'repost' | 'quote';
  source: 'explicit' | 'implicit';
  originalPostId?: string;       // For interactions
  parentCommentId?: string;      // For nested comments
  originalPost?: Post;           // Enriched
  interactions?: { comments: number; reposts: number; quotes: number; };
  display: {                    // UI values
    repositoryName: string;
    commitHash: string;
    commitUrl: string | null;
    totalReposts: number;
    isEmpty: boolean;
    isUnpushed: boolean;
    isOrigin: boolean;
  };
}
```

### Repository (`repository/types.ts`)
```typescript
interface Repository {
  id: string;                    // url#branch:name format
  url: string;                   // Normalized (no .git)
  branch: string;
  name: string;
  path?: string;                 // Local path
  stats?: RepositoryStats;
  lastFetch?: Date;
  isActive: boolean;
  source: 'local' | 'list' | 'remote';
}
```

### List (`social/types.ts`)
```typescript
interface List {
  version: string;               // e.g., "0.1.0"
  id: string;                    // [a-zA-Z0-9_-]{1,40}
  name: string;
  repositories: string[];        // url#branch:name format
  isUnpushed?: boolean;
}
```

### Result (`social/types.ts`)
```typescript
interface Result<T> {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
    details?: unknown;
  };
}
```

## GitMsg Types (`gitmsg/types.ts`)

### GitMsgMessage
```typescript
interface GitMsgMessage {
  subject: string;
  body: string;
  headers: GitMsgHeader[];
  references: GitMsgReference[];
}
```

### GitMsgHeader
```typescript
interface GitMsgHeader {
  extension: string;
  version: string;
  extensionVersion: string;
  fields: Record<string, string>;
}
```

### GitMsgReference
```typescript
interface GitMsgReference {
  ref: string;                   // url#type:value
  parsedRef: ParsedRef;
  metadata?: Record<string, string>;
  content?: string;
}
```

## Namespace Exports

### gitMsgRef (`gitmsg/protocol.ts`)
```typescript
create(type: 'commit' | 'branch', value: string, repository?: string): string
parse(ref: string): { type: string; repository?: string; value: string }
validate(ref: string, type?: 'commit' | 'branch'): boolean
validateListName(name: string): boolean
normalize(ref: string): string
isMyRepository(ref: string): boolean
parseRepositoryId(identifier: string): { repository: string; branch: string }
extractBranchFromRemote(remoteBranch: string): string
normalizeHashInRefWithContext(ref: string, currentRepository?: string): string
```

### gitMsgUrl (`gitmsg/protocol.ts`)
```typescript
normalize(url: string): string
validate(url: string): boolean
toGit(url: string): string
fromRef(ref: string): string | null
parseFragment(url: string): { base: string; fragment?: string; branch?: string }
```

### gitMsgHash (`gitmsg/protocol.ts`)
```typescript
normalize(hash: string): string
truncate(hash: string, length: number): string
validate(hash: string): boolean
```

### social (`social/index.ts`)
```typescript
initialize(config: { storageBase: string }): void
getPosts(workdir: string, scope: string, filter?: PostFilter): Promise<Result<Post[]>>
createPost(workdir: string, content: string, options?: CreateOptions): Promise<Result<Post>>
createInteraction(type: 'comment' | 'repost' | 'quote', workdir: string, targetPost: Post, content?: string): Promise<Result<Post>>
getInteractions(workdir: string, postId: string, type?: string): Promise<Result<Post[]>>
searchPosts(workdir: string, params: SearchParams): Promise<Result<SearchResult>>

// List operations
getLists(repository: string, workspaceRoot?: string): Promise<Result<List[]>>
getList(repository: string, name: string): Promise<Result<List | null>>
createList(repository: string, name: string, data?: Partial<List>): Promise<Result<List>>
updateList(repository: string, name: string, data: Partial<List>): Promise<Result<List>>
deleteList(repository: string, name: string): Promise<Result<void>>
addRepositoryToList(repository: string, listName: string, repoUrl: string): Promise<Result<void>>
removeRepositoryFromList(repository: string, listName: string, repoUrl: string): Promise<Result<void>>
getListRepositories(repository: string, listName: string): Promise<Result<string[]>>
syncList(repository: string, name: string): Promise<Result<void>>
```

### gitRepository (`social/repositories.ts`)
```typescript
initialize(config: { storageBase: string }): void
getRepositories(workdir: string, scope: string = 'workspace:my', filter?: RepositoryFilter): Promise<Result<Repository[]>>
fetchUpdates(workdir: string, scope: string = 'following'): Promise<Result<{ fetched: number; failed: number }>>
cleanupStorage(): Promise<void>
getStorageStats(): Promise<{ totalRepositories: number; persistentRepositories: number; temporaryRepositories: number; totalSize: number; repositories: any[] }>
```

### list (alias for lists from `social/index.ts`)
```typescript
// Same as social list operations above - exported as separate namespace for convenience
getLists(repository: string, workspaceRoot?: string): Promise<Result<List[]>>
getList(repository: string, name: string): Promise<Result<List | null>>
createList(repository: string, name: string, data?: Partial<List>): Promise<Result<List>>
updateList(repository: string, name: string, data: Partial<List>): Promise<Result<List>>
deleteList(repository: string, name: string): Promise<Result<void>>
addRepositoryToList(repository: string, listName: string, repoUrl: string): Promise<Result<void>>
removeRepositoryFromList(repository: string, listName: string, repoUrl: string): Promise<Result<void>>
getListRepositories(repository: string, listName: string): Promise<Result<string[]>>
syncList(repository: string, name: string): Promise<Result<void>>
```

## Interface Reuse Rules

1. **Check existing types first** - Search `types.ts` files before creating new
2. **Extend don't duplicate** - Use `extends` for variations
3. **Use inline types for simple cases** - Don't create interfaces for `{ id: string }`
4. **Prefer type aliases for unions** - `type PostType = 'post' | 'comment' | 'repost' | 'quote'`
5. **Document new interfaces** - Add JSDoc comments explaining purpose

## Common Type Patterns

### Type Utilities
```typescript
// Partial for options: options?: Partial<CreateOptions>
// Pick for subsets: Pick<Post, 'id' | 'content'>
// Omit to exclude: Omit<Post, 'originalPost'>
// Constants: const ERROR_CODES = { ... } as const
```