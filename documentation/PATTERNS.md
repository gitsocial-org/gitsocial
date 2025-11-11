# Code Patterns Library

## Post Retrieval (Always use social.post.getPosts)

### Available Scopes

```typescript
// Core scopes
const result = await social.post.getPosts(workdir, "all"); // All posts in cache
const result = await social.post.getPosts(workdir, "repository:my"); // Current repository
const result = await social.post.getPosts(workdir, "timeline"); // All followed repositories

// Repository scopes
const result = await social.post.getPosts(workdir, "repository:https://github.com/user/repo");

// List scopes (posts from 'reading' list)
const result = await social.post.getPosts(workdir, "list:reading");

// Combined repository/list scope
const result = await social.post.getPosts(workdir, "repository:https://github.com/user/repo/list:favorites");

// Single post scope
const result = await social.post.getPosts(workdir, "post:https://github.com/user/repo#commit:abc123");

// Multiple posts by ID
const result = await social.post.getPosts(workdir, "byId:id1,id2,id3");

// Thread scope (handled separately by thread module)
const result = await social.post.getPosts(workdir, "thread:https://github.com/user/repo#commit:abc123");
```

### Basic Usage Examples

```typescript
import { social } from "../social";

// Get timeline posts
const result = await social.post.getPosts(workdir, "timeline");
if (!result.success) {
  return { success: false, error: result.error };
}
const posts = result.data; // Post[]
```

### Get Posts by ID

```typescript
// Get multiple posts by ID (comma-separated)
const result = await social.post.getPosts(workdir, "byId:id1,id2,id3");

// Example with full post IDs
const result = await social.post.getPosts(
  workdir,
  "byId:https://github.com/user/repo#commit:abc123,https://github.com/other/repo#commit:def456"
);
```

### Get Single Post

```typescript
// Get a single post by its ID
const result = await social.post.getPosts(workdir, "post:https://github.com/user/repo#commit:abc123456789");
```

### Get Thread (Post with Comments)

```typescript
// Get post and its comment thread
const result = await social.post.getPosts(workdir, "thread:https://github.com/user/repo#commit:abc123456789");
```

### Filter Posts

```typescript
const result = await social.post.getPosts(workdir, "timeline", {
  types: ["post", "quote"], // Filter by type
  limit: 50, // Pagination
  since: new Date("2025-01-01"),
  until: new Date("2025-12-31"),
});
```

### Async Behavior

`getPosts()` is async and returns a Promise. All calls must use `await`:

```typescript
// Correct - properly awaits the Promise
const result = await social.post.getPosts(workdir, "timeline");

// Wrong - returns a Promise instead of Result<Post[]>
const result = social.post.getPosts(workdir, "timeline");
```

**Auto-initialization**: The cache automatically initializes on first query. You don't need to manually call any initialization functions.

**State management**: Cache tracks its state (UNINITIALIZED, INITIALIZING, READY, ERROR, REFRESHING) and ensures it's ready before returning results.

## Post Creation

### Create Post

```typescript
import { social } from "../social";

const result = await social.post.createPost(workdir, "Hello GitSocial!");
if (!result.success) {
  log("error", "Failed to create post:", result.error);
  return result;
}
const newPost = result.data; // Post object
```

### Create Comment

```typescript
const postId = "https://github.com/user/repo#commit:abc123456789";
const result = await social.interaction.createComment(workdir, postId, "Great idea!");
```

### Create Repost

```typescript
const result = await social.interaction.createRepost(workdir, postId);
```

### Create Quote

```typescript
const result = await social.interaction.createQuote(workdir, postId, "Adding my thoughts...");
```

## List Management

### Add Repository to List

```typescript
import { social } from "../social";

const result = await social.list.addRepositoryToList(workdir, "reading", "https://github.com/torvalds/linux");
```

### Remove Repository from List

```typescript
const result = await social.list.removeRepositoryFromList(
  workdir,
  "reading",
  "https://github.com/torvalds/linux"
);
```

### Create List

```typescript
const result = await social.list.createList(workdir, "favorites", "My Favorites");
```

### Get All Lists

```typescript
const result = await social.list.getLists(workdir);
if (result.success && result.data) {
  for (const list of result.data) {
    console.log(list.id, list.name, list.repositories.length);
  }
}
```

### Update List

```typescript
const result = await social.list.updateList(workdir, "favorites", {
  name: "Updated Name",
  repositories: ["https://github.com/user/repo#branch:main"],
});
```

## Repository Management

### Get Repositories

```typescript
const result = await social.repository.getRepositories(workdir);
if (result.success && result.data) {
  const repos = result.data; // Repository[]
}
```

## Error Handling

### Standard Pattern

```typescript
const result = await someOperation(workdir);
if (!result.success) {
  return { success: false, error: result.error };
}
// Use result.data safely
return { success: true, data: processData(result.data) };
```

### Chaining Operations

```typescript
const postResult = await social.post.createPost(workdir, content);
if (!postResult.success) {
  return postResult; // Propagate error
}

const fetchResult = await social.repository.fetchUpdates(workdir, 'following');
if (!fetchResult.success) {
  log("warn", "Post created but fetch failed:", fetchResult.error);
}

return postResult; // Return success even if fetch failed
```

## Search Patterns

### Search Posts

```typescript
const result = await social.search.searchPosts(workdir, {
  query: "GitSocial",
  filters: {
    types: ["post", "quote"],
    authors: ["user@example.com"],
    repositories: ["https://github.com/user/repo"],
  },
  sort: "relevance",
  limit: 20,
});
```

### Search with Regex

```typescript
const result = await social.search.searchPosts(workdir, {
  query: "/git.*social/i", // Regex pattern
  filters: { types: ["post"] },
});
```

## Reference Parsing

### Parse GitMsg References

```typescript
import { gitMsgRef } from "../gitmsg/protocol";

const ref = "https://github.com/user/repo#commit:abc123456789";
const parsed = gitMsgRef.parse(ref);
// { url: 'https://github.com/user/repo', type: 'commit', value: 'abc123456789' }

const formatted = gitMsgRef.format(parsed);
// 'https://github.com/user/repo#commit:abc123456789'
```

### Normalize URLs

```typescript
import { gitMsgUrl } from "../gitmsg/protocol";

const normalized = gitMsgUrl.normalize("HTTPS://GitHub.com/User/Repo.git");
// 'https://github.com/User/Repo'
```

## Logging

### Using the Logger

```typescript
import { log } from "../logger";

log("error", "Operation failed:", error);
log("info", "Processing", posts.length, "posts");
log("debug", "Cache hit for key:", cacheKey);
```
