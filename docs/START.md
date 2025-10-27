# GitSocial Quick Start - LLM Guide

## CRITICAL RULES (MUST FOLLOW)
✅ **ALWAYS** use namespace objects: `gitMsgRef`, `gitMsgUrl`, `social`, `gitRepository`
✅ **ALWAYS** use `getPosts()` for ANY post retrieval - NEVER access git/cache directly
✅ **ALWAYS** use `Result<T>` for all error handling
✅ **ALWAYS** check existing interfaces in `types.ts` files before creating new ones
✅ **ALWAYS** use `function` declarations, not `const` arrow functions
❌ **NEVER** create classes - use namespace objects only
❌ **NEVER** import individual functions - use namespace imports
❌ **NEVER** create one-off interfaces - reuse or extend existing
❌ **NEVER** add comments unless absolutely necessary
❌ **NEVER** access `.git` directory or Git internals directly

## Essential Imports
```typescript
// Core namespaces - ALWAYS use these
import { gitMsgRef, gitMsgUrl, gitMsgHash } from '../gitmsg/protocol';
import { gitHost } from '../githost';
import { gitRepository, gitList } from '../repository';
import { social } from '../social';
import { log } from '../logger';
// Types - reuse these
import type { Post, List, Result } from '../social/types';
import type { Repository } from '../repository/types';
```

## Architecture (3 Layers - No Circular Dependencies)
```
Git Layer → GitMsg Layer → Social Layer
```

## Core Operations

### Get Posts (MANDATORY API - Use for ALL post retrieval)
```typescript
// Timeline (all sources)
const result = await social.getPosts(workdir, 'timeline');
// My repository
const result = await social.getPosts(workdir, 'repository:my');
// Specific list
const result = await social.getPosts(workdir, 'list:reading');
// Single post
const result = await social.getPosts(workdir, 'post:https://github.com/user/repo#commit:abc123456789');
```

### Create Post
```typescript
const result = await social.createPost(workdir, 'Hello world!');
if (!result.success) {
  return { success: false, error: result.error };
}
```

### Create Comment
```typescript
const result = await social.createComment(workdir, postId, 'Great idea!');
```

### Repository Management
```typescript
// Add repository to list
const result = await gitRepository.addToList(workdir, 'reading', 'https://github.com/user/repo');
// Get all repositories
const repos = await gitRepository.getAll(workdir);
```

## Error Handling Pattern
```typescript
const result = await someOperation();
if (!result.success) {
  return { 
    success: false, 
    error: { 
      code: 'ERROR_CODE', 
      message: result.error?.message || 'Operation failed' 
    }
  };
}
// Use result.data safely here
```

## Type Locations (ALWAYS REUSE)
- `Post` → `social/types.ts`
- `Repository` → `repository/types.ts`
- `List` → `social/types.ts`
- `Result<T>` → `social/types.ts`
- `GitMsgMessage` → `gitmsg/types.ts`

## Namespace Exports
- `gitMsgRef`: parse(), create(), format()
- `gitMsgUrl`: normalize(), isValid()
- `social`: getPosts(), createPost(), createComment(), searchPosts()
- `gitRepository`: getAll(), addToList(), removeFromList()
- `gitList`: getAll(), create(), update(), delete()
- `log`: log(level, message, ...args)

## Documentation
- [ARCHITECTURE.md](ARCHITECTURE.md) - System design & decisions
- [PATTERNS.md](PATTERNS.md) - Code patterns | [INTERFACES.md](INTERFACES.md) - Type reference
- [GITMSG.md](GITMSG.md) - Protocol | [GITSOCIAL.md](GITSOCIAL.md) - Social extension