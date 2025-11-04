/**
 * Cache module for GitSocial posts
 *
 * This module manages post caching, loading coordination, and ID resolution.
 * It orchestrates the transformation pipeline while maintaining correctness:
 * - All posts are loaded together (atomic operation)
 * - All references are resolved with full context
 * - All interaction counts are calculated with complete dataset
 *
 * ARCHITECTURE:
 * - cache.ts: Cache management, loading coordination, ID resolution
 * - cache-transform.ts: Git commit to Post transformation
 * - cache-interactions.ts: Interaction counting and cache updates
 *
 * All post retrieval MUST go through getPosts() → getCachedPosts() to ensure consistency.
 *
 * ABSOLUTE/RELATIVE ID REQUIREMENTS:
 * - Workspace posts: ALWAYS use relative IDs (#commit:xxx) as primary
 * - External posts: ALWAYS use absolute IDs (https://github.com/user/repo#commit:xxx)
 * - Deduplication: Map absolute→relative for workspace posts via postIndex.absolute
 * - Virtual posts to workspace: Merge into existing, don't duplicate
 * - Lookups: Check both direct ID and absolute→relative mapping
 * - References: Keep workspace refs relative, add context to external refs
 * - No origin URL required for workspace operation
 */

import { existsSync } from 'fs';
import { LRUCache } from 'lru-cache';
import type { List, Post } from '../types';
import type { Commit } from '../../git/types';
import { getCommits, getConfiguredBranch } from '../../git/operations';
import { getOriginUrl } from '../../git/remotes';
import { getFetchStartDate } from '../../git/utils';
import { log } from '../../logger';
import { gitMsgHash, gitMsgRef, gitMsgUrl } from '../../gitmsg/protocol';
import { storage } from '../../storage';
import { list } from '../list';
import { processCommits, processPost } from './cache-transform';
import { updateInteractionCounts } from './cache-interactions';

// ========================================
// PUBLIC API / NAMESPACE EXPORT
// ========================================

export const cache = {
  getCachedPosts,
  refresh,
  initializeGlobalCache,
  setCacheEnabled,
  getCacheStats,
  setCacheMaxSize,
  loadRepositoryPosts,
  loadAdditionalPosts,
  isCacheRangeCovered,
  getCachedRanges
};

// ========================================
// CONSTANTS & CONFIGURATION
// ========================================

let CACHE_MAX_SIZE = 100000; // Default: 100k posts (~300-400 MB)
const CACHE_TTL = 30 * 24 * 60 * 60 * 1000; // 30 days

// ========================================
// STATE & STORAGE
// ========================================

export let postsCache = new LRUCache<string, Readonly<Post>>({
  max: CACHE_MAX_SIZE,
  ttl: CACHE_TTL
});

export const postIndex = {
  byHash: new Map<string, Set<string>>(),
  byRepository: new Map<string, Set<string>>(),
  byList: new Map<string, Set<string>>(), // Maps "workdir:listId" to Set of post IDs
  absolute: new Map<string, string>(), // Maps absolute IDs to relative IDs for workspace posts
  merged: new Set<string>() // Tracks which virtual posts have been merged
};

// Track which start dates have been loaded into cache (data goes to HEAD)
const cachedStartDates: Set<string> = new Set();
let cacheEnabled = true;
let globalCacheInitialized = false;

// Track configured GitSocial branch per workdir for filtering
const configuredBranches = new Map<string, string>();

// ========================================
// MAIN API IMPLEMENTATIONS
// ========================================

function getCachedPosts(
  workdir: string,
  scope: string,
  filter?: {
    types?: Array<'post' | 'quote' | 'comment' | 'repost'>;
    since?: Date;
    until?: Date;
    limit?: number;
    includeImplicit?: boolean;
    skipCache?: boolean;
    sortBy?: 'top' | 'latest' | 'oldest';
  },
  context?: {
    list?: List;  // Optional list data for remote lists
  }
): Post[] {
  if (!cacheEnabled) {
    log('debug', '[getCachedPosts] Cache disabled, returning empty');
    return [];
  }

  if (!globalCacheInitialized) {
    log('debug', '[getCachedPosts] Global cache not initialized, returning empty results for scope:', scope);
    return [];
  }

  log('debug', '[getCachedPosts] Looking up posts for scope:', scope, 'filter:', filter);

  // OPTIMIZATION: Early return for single post lookups
  if (scope.startsWith('post:')) {
    const postId = scope.slice(5);
    const post = getPostById(postId);
    if (post) {
      return [{ ...post }];
    }
    return [];
  }

  // For all other scopes, we need to iterate
  const allPosts = Array.from(postsCache.values()).map(frozenPost => ({ ...frozenPost } as Post));

  if (scope.startsWith('thread:')) {
    const targetPostId = scope.replace('thread:', '');
    const targetPost = allPosts.find(p => p.id === targetPostId);
    if (targetPost) {
      log('debug', '[getCachedPosts] Thread request - target post interaction counts from cache:', {
        targetPostId,
        comments: targetPost.interactions?.comments,
        reposts: targetPost.interactions?.reposts,
        quotes: targetPost.interactions?.quotes,
        totalReposts: targetPost.display?.totalReposts
      });
    }
  }

  // Special scope to return all posts without filtering
  if (scope === 'all') {
    const sortBy = filter?.sortBy || 'latest';
    let filtered = sortPosts(allPosts, sortBy);

    if (filter?.limit && filter.limit > 0) {
      filtered = filtered.slice(0, filter.limit);
    }

    return filtered;
  }

  const scopeOptions = parseScopeParameter(scope);

  let filtered: Post[];

  // OPTIMIZATION: Use index for list queries
  if (scopeOptions.listName) {
    const listKey = `${workdir}:${scopeOptions.listName}`;
    const postIds = postIndex.byList.get(listKey);

    if (!postIds || postIds.size === 0) {
      // Fallback for remote lists using context
      if (context?.list && context.list.repositories) {
        const listRepoUrls = new Set(
          context.list.repositories.map(r =>
            gitMsgUrl.normalize(r.split('#')[0] || r)
          )
        );

        filtered = allPosts.filter(post => {
          const postRepoUrl = gitMsgUrl.normalize(
            post.repository.split('#')[0] || post.repository
          );
          return listRepoUrls.has(postRepoUrl);
        });
      } else {
        filtered = [];
      }
    } else {
      filtered = Array.from(postIds)
        .map(id => postsCache.get(id))
        .filter((p): p is Post => p !== undefined)
        .map(p => ({ ...p }));

      // Apply repository filter if specified
      if (scopeOptions.repositoryUrl) {
        const normalizedScopeUrl = gitMsgUrl.normalize(scopeOptions.repositoryUrl);
        filtered = filtered.filter(p =>
          gitMsgUrl.normalize(p.repository) === normalizedScopeUrl
        );
      }
    }

    // Apply type/time filters (for both local and remote lists)
    if (filter) {
      filtered = filtered.filter(p => {
        if (filter.types?.length && !filter.types.includes(p.type)) {
          return false;
        }
        if (filter.since && new Date(p.timestamp) < filter.since) {
          return false;
        }
        if (filter.until && new Date(p.timestamp) > filter.until) {
          return false;
        }
        return true;
      });
    }
  } else if (scopeOptions.repositoryUrl) {
    // OPTIMIZATION: Use index for repository queries
    const parsed = gitMsgRef.parseRepositoryId(scopeOptions.repositoryUrl);
    const standardRepoId = `${gitMsgUrl.normalize(parsed.repository)}#branch:${parsed.branch}`;
    const postIds = postIndex.byRepository.get(standardRepoId);

    if (!postIds || postIds.size === 0) {
      filtered = [];
    } else {
      filtered = Array.from(postIds)
        .map(id => postsCache.get(id))
        .filter((p): p is Post => p !== undefined)
        .map(p => ({ ...p }));

      // Apply type/time filters
      if (filter) {
        filtered = filtered.filter(p => {
          if (filter.types?.length && !filter.types.includes(p.type)) {
            return false;
          }
          if (filter.since && new Date(p.timestamp) < filter.since) {
            return false;
          }
          if (filter.until && new Date(p.timestamp) > filter.until) {
            return false;
          }
          return true;
        });
      }
    }
  } else {
    filtered = allPosts.filter(post => {
      if (scopeOptions.postIds) {
        // For byId scope, check both direct ID match and absolute->relative mapping
        const matchesDirectly = scopeOptions.postIds.includes(post.id);

        // Also check if any requested ID maps to this post's ID via absolute mapping
        const matchesViaMapping = scopeOptions.postIds.some(requestedId => {
          const mappedId = postIndex.absolute.get(requestedId);
          return mappedId === post.id;
        });

        if (!matchesDirectly && !matchesViaMapping) {
          return false;
        }
      } else if (scope === 'repository:my') {
        // Check if it's a workspace post
        let isWorkspacePost = false;

        // Workspace posts now have relative IDs (#commit:xxx)
        if (post.id.startsWith('#')) {
          isWorkspacePost = true;
        } else {
          // Fallback: check repository field for older cached posts
          const postRepoUrl = post.repository.includes('#branch:')
            ? (post.repository.split('#branch:')[0] || post.repository)
            : post.repository;
          const normalizedPostRepo = gitMsgUrl.normalize(postRepoUrl);
          const normalizedWorkdir = gitMsgUrl.normalize(workdir);

          if (normalizedPostRepo === normalizedWorkdir || normalizedPostRepo === 'myrepository') {
            isWorkspacePost = true;
          }
        }

        if (!isWorkspacePost) {
          return false;
        }

        // Filter by configured GitSocial branch
        const configuredBranch = configuredBranches.get(workdir);
        if (configuredBranch) {
          // Extract branch from post
          let postBranch: string | undefined = post.branch;

          // If not in branch field, try extracting from repository field
          if (!postBranch && post.repository.includes('#branch:')) {
            const parts = post.repository.split('#branch:');
            postBranch = parts[1];
          }

          // Only include posts from the configured branch
          if (postBranch && postBranch !== configuredBranch) {
            log('debug', `[getCachedPosts] Filtering out post ${post.id} from branch ${postBranch} (configured: ${configuredBranch})`);
            return false;
          }
        }

        // Don't return true here - let it fall through to date filtering
      } else if (scope !== 'timeline') {
        return false;
      }

      if (filter) {
        if (filter.types?.length && !filter.types.includes(post.type)) {
          return false;
        }

        if (filter.since && new Date(post.timestamp) < filter.since) {
          return false;
        }

        if (filter.until && new Date(post.timestamp) > filter.until) {
          return false;
        }
      }

      return true;
    });
  }

  if (scopeOptions.threadPostId && filter) {
    filtered = filtered.filter(post => {
      if (filter.types?.length && !filter.types.includes(post.type)) {
        return false;
      }

      if (filter.since && new Date(post.timestamp) < filter.since) {
        return false;
      }

      if (filter.until && new Date(post.timestamp) > filter.until) {
        return false;
      }

      return true;
    });
  }

  log('debug', '[getCachedPosts] Single-pass filtering returned', filtered.length, 'posts for scope:', scope);

  const sortBy = filter?.sortBy || 'latest';
  filtered = sortPosts(filtered, sortBy);

  if (filter?.limit && filter.limit > 0) {
    filtered = filtered.slice(0, filter.limit);
  }

  return filtered;
}

async function refresh(scope: {
  repositories?: string[];
  hashes?: string[];
  lists?: string[];
  all?: boolean;
}, workdir?: string, storageBase?: string): Promise<void> {
  log('debug', '[Cache] Refreshing cache with scope:', scope);

  if (scope.all) {
    postsCache.clear();
    postIndex.byHash.clear();
    postIndex.byRepository.clear();
    postIndex.byList.clear();
    postIndex.absolute.clear();
    postIndex.merged.clear();
    globalCacheInitialized = false;
  } else {
    if (scope.repositories?.length) {
      globalCacheInitialized = false;
    }

    if (scope.hashes?.length) {
      for (const hash of scope.hashes) {
        const hash12 = gitMsgHash.normalize(hash);
        const postsToRemove = postIndex.byHash.get(hash12);
        if (postsToRemove) {
          for (const postId of postsToRemove) {
            postsCache.delete(postId);
          }
          postIndex.byHash.delete(hash12);
        }
      }
    }

    if (scope.lists?.length) {
      globalCacheInitialized = false;
    }
  }

  // Always reinitialize if workdir provided
  if (workdir) {
    // Determine the oldest date we need to load based on what we're refreshing
    let sinceOverride: Date | undefined;
    if (scope.repositories && scope.repositories.length > 0 && storageBase) {
      // Check the oldest fetched date from the repositories we're refreshing
      let oldestDate: string | null = null;
      for (const repoId of scope.repositories) {
        const parsed = gitMsgRef.parseRepositoryId(repoId);
        if (parsed) {
          const storageDir = storage.path.getDirectory(storageBase, parsed.repository);
          const config = await storage.repository.readConfig(storageDir);
          if (config?.fetchedRanges && config.fetchedRanges.length > 0) {
            const ranges = config.fetchedRanges;
            for (const range of ranges) {
              if (!oldestDate || range.start < oldestDate) {
                oldestDate = range.start;
              }
            }
          }
        }
      }
      if (oldestDate) {
        sinceOverride = new Date(oldestDate);
        log('debug', `[Cache] Using oldest fetched date for cache refresh: ${oldestDate}`);
      }
    }

    await initializeGlobalCache(workdir, storageBase, sinceOverride);
    log('debug', '[Cache] Cache refreshed and reinitialized');
  }
}

async function initializeGlobalCache(workdir: string, storageBase?: string, sinceOverride?: Date): Promise<void> {
  if (!cacheEnabled) {return;}

  log('debug', '[initializeGlobalCache] Starting global cache initialization with sinceOverride:', sinceOverride?.toISOString());

  // Get configured GitSocial branch for filtering repository:my posts
  try {
    const configuredBranch = await getConfiguredBranch(workdir);
    configuredBranches.set(workdir, configuredBranch);
    log('debug', '[initializeGlobalCache] Configured GitSocial branch:', configuredBranch);
  } catch (error) {
    log('debug', '[initializeGlobalCache] Failed to get configured branch:', error);
  }

  // Get origin URL if available for absolute->relative mappings
  let originUrl: string | undefined;
  try {
    const originResult = await getOriginUrl(workdir);
    if (originResult.success && originResult.data && originResult.data !== 'myrepository') {
      originUrl = gitMsgUrl.normalize(originResult.data);
      log('debug', '[initializeGlobalCache] Origin URL:', originUrl);
    }
  } catch (error) {
    log('debug', '[initializeGlobalCache] No origin URL available:', error);
  }

  const posts = new Map<string, Post>();

  try {
    // Load all posts in single phase with unified processing
    const workspaceCommits = await loadPosts(workdir, 'workspace', undefined, sinceOverride);
    const externalCommits = await loadPosts(workdir, 'external', storageBase, sinceOverride);

    // Process all posts with unified function
    for (const post of workspaceCommits) {
      processPost(post, posts, workdir, originUrl, postIndex);
    }

    for (const post of externalCommits) {
      processPost(post, posts, workdir, originUrl, postIndex);
    }

    log('debug', '[initializeGlobalCache] Processed all posts and references');
  } catch (error) {
    log('error', '[initializeGlobalCache] Error processing posts:', error);
    // Continue with initialization even if some posts fail
  }

  try {
    await list.initializeListStorage(workdir);
    log('debug', '[initializeGlobalCache] Initialized list storage');
  } catch (error) {
    log('error', '[initializeGlobalCache] Failed to initialize list storage:', error);
  }

  log('debug', '[initializeGlobalCache] Total posts after processing:', posts.size);

  await updateInteractionCounts(posts, workdir);
  log('debug', '[initializeGlobalCache] Updated interaction counts');

  for (const post of posts.values()) {
    const frozenPost: Readonly<Post> = Object.freeze(post);

    postsCache.set(post.id, frozenPost);
    updateIndexes(post.id, frozenPost, workdir);
  }

  // Track that we've loaded from this date
  // IMPORTANT: Must match what was actually loaded in loadPosts() above
  const actualSince = sinceOverride || new Date(getFetchStartDate());
  const dateStr = actualSince.toISOString().split('T')[0]!;
  cachedStartDates.add(dateStr);
  log('debug', '[initializeGlobalCache] Marked cache as having data from:', dateStr);

  globalCacheInitialized = true;
  log('debug', '[initializeGlobalCache] Completed global cache initialization with', posts.size, 'posts');
}

function setCacheEnabled(enabled: boolean): void {
  cacheEnabled = enabled;
  if (!enabled) {
    postsCache.clear();
    postIndex.byHash.clear();
    postIndex.byRepository.clear();
    postIndex.byList.clear();
    cachedStartDates.clear();
    globalCacheInitialized = false;
  }
}

function setCacheMaxSize(maxSize: number): void {
  // Validate input
  if (maxSize < 1000) {
    log('warn', '[setCacheMaxSize] Cache size too small, using minimum of 1000');
    maxSize = 1000;
  }
  if (maxSize > 1000000) {
    log('warn', '[setCacheMaxSize] Cache size too large, using maximum of 1000000');
    maxSize = 1000000;
  }

  CACHE_MAX_SIZE = maxSize;

  // Create new cache with new size, preserving existing entries
  const oldCache = postsCache;
  postsCache = new LRUCache<string, Readonly<Post>>({
    max: CACHE_MAX_SIZE,
    ttl: CACHE_TTL
  });

  // Copy entries from old cache (LRU will handle overflow if new size is smaller)
  for (const [key, value] of oldCache.entries()) {
    postsCache.set(key, value);
  }

  log('debug', `[setCacheMaxSize] Cache resized to ${CACHE_MAX_SIZE} entries`);
}

function getCacheStats(): {
  postsCache: { size: number; maxSize: number };
  enabled: boolean;
  } {
  return {
    postsCache: { size: postsCache.size, maxSize: CACHE_MAX_SIZE },
    enabled: cacheEnabled
  };
}

// ========================================
// ID RESOLUTION HELPERS (CENTRALIZED)
// ========================================

/**
 * Get a post by ID, checking both direct and mapped IDs
 */
function getPostById(postId: string): Readonly<Post> | undefined {
  // Direct lookup
  let post = postsCache.get(postId);
  if (post) {return post;}

  // Check if there's a mapping
  const mappedId = postIndex.absolute.get(postId);
  if (mappedId) {
    post = postsCache.get(mappedId);
  }

  return post;
}

// ========================================
// DATA LOADING
// ========================================

async function loadPosts(
  workdir: string,
  scope: 'workspace' | 'external',
  storageBase?: string,
  sinceOverride?: Date
): Promise<Post[]> {
  const gitSocialBranch = await getConfiguredBranch(workdir);
  // Use override if provided (for historical data), otherwise use 30-day default
  const since = sinceOverride || new Date(getFetchStartDate());

  if (scope === 'workspace') {
    const myCommits = await getCommits(workdir, {
      all: false,
      branch: gitSocialBranch,
      since,
      limit: 10000
    });
    return myCommits.length > 0 ? processCommits(workdir, myCommits) : [];
  }

  // 'external' scope only loads external repository commits
  if (!storageBase) {
    return [];
  }

  const externalCommits = await loadExternalCommits(storageBase, {
    source: 'all',
    workdir,
    filter: { since }
  });

  if (externalCommits.length === 0) {return [];}
  return processCommits(workdir, externalCommits);
}

async function loadExternalCommits(
  storageBase: string,
  options: {
    source: 'single' | 'multiple' | 'all';
    repository?: { url: string; branch: string };
    repositories?: string[];
    workdir?: string;
    filter?: { since?: Date; until?: Date; limit?: number };
  }
): Promise<Commit[]> {
  if (!storageBase) {return [];}

  const repoMap = new Map<string, string>();

  if (options.source === 'single') {
    if (!options.repository) {
      throw new Error('repository option required for single source');
    }
    const normalizedUrl = gitMsgUrl.normalize(options.repository.url);
    repoMap.set(normalizedUrl, options.repository.branch);
  } else if (options.source === 'multiple') {
    if (!options.repositories || options.repositories.length === 0) {
      return [];
    }
    for (const repoString of options.repositories) {
      const parsed = gitMsgRef.parseRepositoryId(repoString);
      if (parsed) {
        const normalizedUrl = gitMsgUrl.normalize(parsed.repository);
        repoMap.set(normalizedUrl, parsed.branch);
      }
    }
  } else if (options.source === 'all') {
    if (!options.workdir) {
      throw new Error('workdir option required for all source');
    }
    const listsResult = await list.getLists(options.workdir);
    const allLists = listsResult.success && listsResult.data ? listsResult.data : [];

    const allRepoStrings = new Set<string>();
    for (const list of allLists) {
      if (list.repositories) {
        for (const repoString of list.repositories) {
          allRepoStrings.add(repoString);
        }
      }
    }

    for (const repoString of allRepoStrings) {
      const parsed = gitMsgRef.parseRepositoryId(repoString);
      if (parsed) {
        const normalizedUrl = gitMsgUrl.normalize(parsed.repository);
        repoMap.set(normalizedUrl, parsed.branch);
      }
    }
  }

  const externalCommits: Commit[] = [];

  for (const [repoUrl, branch] of repoMap) {
    const storageDir = storage.path.getDirectory(storageBase, repoUrl);
    if (!existsSync(storageDir)) {
      log('warn', `Repository not yet cloned: ${repoUrl}#branch:${branch}`);
      continue;
    }

    const result = await storage.repository.getCommits(storageBase, repoUrl, {
      branch,
      limit: options.filter?.limit || 10000,
      since: options.filter?.since || new Date(getFetchStartDate())
    });

    if (result.success && result.data) {
      for (const commit of result.data) {
        const extendedCommit = commit as Commit & {
          __external?: { repoUrl: string; storageDir: string; branch: string }
        };
        extendedCommit.__external = { repoUrl, storageDir, branch };
      }
      externalCommits.push(...result.data);
    }
  }

  return externalCommits;
}

// Internal function for loading repository posts (used by social layer)
export async function loadRepositoryPosts(
  workdir: string,
  repositoryUrl: string,
  branch: string,
  storageBase: string
): Promise<void> {

  // Check the repository's fetched ranges to determine the appropriate date range
  let since: Date = new Date(getFetchStartDate());
  const storageDir = storage.path.getDirectory(storageBase, repositoryUrl);
  const config = await storage.repository.readConfig(storageDir);
  if (config?.fetchedRanges && config.fetchedRanges.length > 0) {
    // Use the oldest fetched date from the repository's ranges
    let oldestDate = config.fetchedRanges[0]!.start;
    for (const range of config.fetchedRanges) {
      if (range.start < oldestDate) {
        oldestDate = range.start;
      }
    }
    since = new Date(oldestDate);
    log('debug', `[loadRepositoryPosts] Using fetched range date for ${repositoryUrl}: ${oldestDate}`);
  }

  // Load commits for this specific repository
  const commits = await loadExternalCommits(storageBase, {
    source: 'single',
    repository: { url: repositoryUrl, branch },
    filter: { since }
  });

  if (commits.length === 0) {return;}

  const posts = await processCommits(workdir, commits);

  // Use temporary Map to process posts with unified pipeline
  const postsMap = new Map<string, Post>();

  // Get origin URL for proper reference resolution
  let originUrl: string | undefined;
  try {
    const originResult = await getOriginUrl(workdir);
    if (originResult.success && originResult.data && originResult.data !== 'myrepository') {
      originUrl = gitMsgUrl.normalize(originResult.data);
    }
  } catch (error) {
    log('debug', '[loadRepositoryPosts] No origin URL available:', error);
  }

  // Process all posts with unified function
  for (const post of posts) {
    processPost(post, postsMap, repositoryUrl, originUrl, postIndex);
  }

  // Update interaction counts incrementally
  await updateInteractionCounts(postsMap, workdir);

  // Add all posts to global cache (real and virtual)
  for (const post of postsMap.values()) {
    const frozenPost: Readonly<Post> = Object.freeze(post);
    postsCache.set(post.id, frozenPost);
    updateIndexes(post.id, frozenPost, workdir);
  }

  log('debug', `[loadRepositoryPosts] Loaded ${postsMap.size} posts for ${repositoryUrl}`);
}

// ========================================
// HELPER FUNCTIONS
// ========================================

function updateIndexes(postId: string, post: Readonly<Post> | Post, workdir: string): void {
  try {
    const parsed = gitMsgRef.parse(postId);
    const hash = parsed.type === 'commit' ? parsed.value : null;
    if (hash) {
      if (!postIndex.byHash.has(hash)) {
        postIndex.byHash.set(hash, new Set());
      }
      postIndex.byHash.get(hash)!.add(postId);
    }
  } catch {
    // Skip if parsing fails
  }

  if (post.repository) {
    const parsed = gitMsgRef.parseRepositoryId(post.repository);
    const standardRepoId = `${gitMsgUrl.normalize(parsed.repository)}#branch:${parsed.branch}`;

    if (!postIndex.byRepository.has(standardRepoId)) {
      postIndex.byRepository.set(standardRepoId, new Set());
    }
    postIndex.byRepository.get(standardRepoId)!.add(postId);
  }

  // Index by lists that contain this post's repository
  const allLists = list.getAllListsFromStorage(workdir);
  for (const listObj of allLists) {
    const postRepoUrl = gitMsgUrl.normalize(post.repository.split('#')[0] || post.repository);
    const inList = listObj.repositories.some(listRepoUrl => {
      const normalizedListRepo = gitMsgUrl.normalize(listRepoUrl.split('#')[0] || listRepoUrl);
      return normalizedListRepo === postRepoUrl;
    });
    if (inList) {
      const listKey = `${workdir}:${listObj.id}`;
      if (!postIndex.byList.has(listKey)) {
        postIndex.byList.set(listKey, new Set());
      }
      postIndex.byList.get(listKey)!.add(postId);
    }
  }
}

function parseScopeParameter(scope: string): {
  postId?: string;
  postIds?: string[];
  repositoryUrl?: string;
  scope?: 'repository:my' | 'timeline';
  listName?: string;
  threadPostId?: string;
} {
  if (scope === 'repository:my') {return { scope: 'repository:my' };}
  if (scope === 'timeline') {return { scope: 'timeline' };}

  if (scope.startsWith('list:')) {
    const listId = scope.slice(5);
    if (!listId) {throw new Error('List ID cannot be empty');}
    return { listName: listId, scope: 'timeline' };
  }

  if (scope.startsWith('repository:')) {
    const repositoryPart = scope.slice(11);
    if (!repositoryPart) {throw new Error('Repository URL cannot be empty');}

    if (repositoryPart.includes('/list:')) {
      const [repositoryUrl, listId] = repositoryPart.split('/list:');
      if (!repositoryUrl) {throw new Error('Repository URL cannot be empty in repository/list scope');}
      if (!listId) {throw new Error('List ID cannot be empty in repository/list scope');}
      return { repositoryUrl, listName: listId, scope: 'timeline' };
    }

    return { repositoryUrl: repositoryPart, scope: 'timeline' };
  }

  if (scope.startsWith('post:')) {
    const postId = scope.slice(5);
    if (!postId) {throw new Error('Post ID cannot be empty');}
    return { postId };
  }

  if (scope.startsWith('byId:')) {
    const idsString = scope.slice(5);
    if (!idsString) {throw new Error('Post IDs cannot be empty');}
    const postIds = idsString.split(',').map(id => id.trim()).filter(id => id);
    if (postIds.length === 0) {throw new Error('At least one post ID is required');}
    return { postIds };
  }

  if (scope.startsWith('thread:')) {
    const postId = scope.slice(7);
    if (!postId) {throw new Error('Thread post ID cannot be empty');}
    return { threadPostId: postId };
  }

  throw new Error(`Invalid scope parameter: '${scope}'`);
}

// ========================================
// INCREMENTAL CACHE LOADING
// ========================================

/**
 * Check if data from a start date is already in cache
 */
function isCacheRangeCovered(since: Date): boolean {
  const sinceStr: string = since.toISOString().split('T')[0]!;
  const covered = cachedStartDates.has(sinceStr);
  log('debug', '[isCacheRangeCovered] Checking if', sinceStr, 'is covered. Result:', covered, 'Cached dates:', Array.from(cachedStartDates));
  return covered;
}

/**
 * Get the start dates that have been loaded into cache
 */
function getCachedRanges(): string[] {
  return Array.from(cachedStartDates).sort();
}

/**
 * Load additional posts into the cache without clearing existing data
 */
async function loadAdditionalPosts(
  workdir: string,
  storageBase: string,
  since: Date
): Promise<void> {
  if (!cacheEnabled) {return;}

  // Check if data from this start date is already cached
  if (isCacheRangeCovered(since)) {
    log('debug', '[loadAdditionalPosts] Data from this date already cached:', {
      since: since.toISOString()
    });
    return;
  }

  log('debug', '[loadAdditionalPosts] Loading additional posts from:', {
    since: since.toISOString()
  });

  // Get origin URL for reference resolution
  let originUrl: string | undefined;
  try {
    const originResult = await getOriginUrl(workdir);
    if (originResult.success && originResult.data && originResult.data !== 'myrepository') {
      originUrl = gitMsgUrl.normalize(originResult.data);
    }
  } catch (error) {
    log('debug', '[loadAdditionalPosts] No origin URL available:', error);
  }

  const posts = new Map<string, Post>();

  try {
    // Load posts for the specific date range
    const workspaceCommits = await loadPosts(workdir, 'workspace', undefined, since);
    const externalCommits = await loadPosts(workdir, 'external', storageBase, since);

    // Process all posts
    for (const post of workspaceCommits) {
      processPost(post, posts, workdir, originUrl, postIndex);
    }
    for (const post of externalCommits) {
      processPost(post, posts, workdir, originUrl, postIndex);
    }

    log('debug', '[loadAdditionalPosts] Processed', posts.size, 'additional posts');
  } catch (error) {
    log('error', '[loadAdditionalPosts] Error processing posts:', error);
  }

  // Update interaction counts for new posts
  await updateInteractionCounts(posts, workdir);

  // Add new posts to cache (don't clear existing)
  let addedCount = 0;
  for (const post of posts.values()) {
    if (!postsCache.has(post.id)) {
      const frozenPost: Readonly<Post> = Object.freeze(post);
      postsCache.set(post.id, frozenPost);
      updateIndexes(post.id, frozenPost, workdir);
      addedCount++;
    }
  }

  // Only mark this date as cached if we actually found and loaded posts
  if (addedCount > 0) {
    cachedStartDates.add(since.toISOString().split('T')[0]!);
    log('debug', '[loadAdditionalPosts] Added', addedCount, 'new posts to cache, marked date as cached');
  } else {
    log('debug', '[loadAdditionalPosts] No new posts found for date, not marking as cached:', since.toISOString());
  }
}

function sortPosts(posts: Post[], sortBy: 'top' | 'latest' | 'oldest'): Post[] {
  return posts.sort((a, b) => {
    switch (sortBy) {
    case 'oldest':
      return new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime();
    case 'top': {
      const aCount = a.interactions?.comments || 0;
      const bCount = b.interactions?.comments || 0;
      if (aCount !== bCount) {
        return bCount - aCount;
      }
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    }
    case 'latest':
    default:
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    }
  });
}

/**
 * Resolve any ID to its canonical form for deduplication
 * Simplified using existing protocol functions
 */

export function resolveToCanonicalId(
  id: string,
  myOriginUrl?: string,
  postIndex?: { absolute: Map<string, string>; }
): string {
  // Check if this absolute ID maps to a relative workspace post
  const mappedId = postIndex?.absolute.get(id);
  if (mappedId) {
    return mappedId;
  }

  // If it's already relative, return as-is
  if (id.startsWith('#')) {
    return id;
  }

  // If it's absolute pointing to workspace, convert to relative
  if (myOriginUrl && id.includes(myOriginUrl)) {
    const parsed = gitMsgRef.parse(id);
    if (parsed.type !== 'unknown') {
      return gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value);
    }
  }

  return id;
}
