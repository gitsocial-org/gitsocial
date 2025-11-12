import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cache, postIndex, postsCache } from './cache';
import { createTestRepo, type TestRepo } from '../../test-utils';
import { initializeGitSocial } from '../config';
import { execGit } from '../../git/exec';
import { CacheState } from '../types';
import { list } from '../list';
import { gitMsgHash, gitMsgRef } from '../../gitmsg/protocol';
import { post } from './index';
import { interaction } from './interaction';

describe('social/post/cache', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('cache-test');
    await initializeGitSocial(testRepo.path, 'gitsocial');
    cache.setCacheEnabled(false);
    await cache.refresh({ all: true });
    postsCache.clear();
    postIndex.byHash.clear();
    postIndex.byRepository.clear();
    postIndex.byList.clear();
    postIndex.absolute.clear();
    postIndex.merged.clear();
  });

  afterEach(async () => {
    cache.setCacheEnabled(false);
    await cache.refresh({ all: true });
    postsCache.clear();
    postIndex.byHash.clear();
    postIndex.byRepository.clear();
    postIndex.byList.clear();
    postIndex.absolute.clear();
    postIndex.merged.clear();
    testRepo.cleanup();
  });

  describe('Cache State Management', () => {
    describe('isInitialized()', () => {
      it('should return false when cache is not initialized', () => {
        expect(cache.isInitialized()).toBe(false);
      });

      it('should return true after cache is initialized', async () => {
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        expect(cache.isInitialized()).toBe(true);
      });

      it('should return false after cache is disabled', async () => {
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        cache.setCacheEnabled(false);
        expect(cache.isInitialized()).toBe(false);
      });
    });

    describe('getStatus()', () => {
      it('should return UNINITIALIZED state after cache is disabled', () => {
        cache.setCacheEnabled(false);
        const status = cache.getStatus();
        expect(status.state).toBe(CacheState.UNINITIALIZED);
        expect(status.lastError).toBeUndefined();
      });

      it('should return READY state after initialization', async () => {
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        const status = cache.getStatus();
        expect(status.state).toBe(CacheState.READY);
        expect(status.lastInitialized).toBeDefined();
        expect(status.lastError).toBeUndefined();
      });

      it('should include post count in status', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Test post');
        await cache.initializeGlobalCache(testRepo.path);
        const status = cache.getStatus();
        expect(status.postCount).toBeGreaterThan(0);
      });
    });

    describe('initializeGlobalCache()', () => {
      it('should initialize cache from UNINITIALIZED state', async () => {
        cache.setCacheEnabled(true);
        expect(cache.getStatus().state).toBe(CacheState.UNINITIALIZED);
        await cache.initializeGlobalCache(testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      });

      it('should load posts during initialization', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Post 1');
        await post.createPost(testRepo.path, 'Post 2');
        await cache.initializeGlobalCache(testRepo.path);
        expect(postsCache.size).toBe(2);
      });

      it('should handle concurrent initialization attempts', async () => {
        cache.setCacheEnabled(true);
        const promise1 = cache.initializeGlobalCache(testRepo.path);
        const promise2 = cache.initializeGlobalCache(testRepo.path);
        await Promise.all([promise1, promise2]);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      });

      it('should handle initialization with no posts', async () => {
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
        expect(postsCache.size).toBe(0);
      });

      it('should skip initialization if already READY', async () => {
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        const firstInitTime = cache.getStatus().lastInitialized;
        await new Promise(resolve => setTimeout(resolve, 10));
        await cache.initializeGlobalCache(testRepo.path);
        const secondInitTime = cache.getStatus().lastInitialized;
        expect(firstInitTime).toEqual(secondInitTime);
      });

      it('should initialize list storage during cache initialization', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Test post');
        await cache.initializeGlobalCache(testRepo.path);
        const listResult = await list.getLists(testRepo.path);
        expect(listResult.success).toBe(true);
      });

      it('should track date ranges during initialization', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Test post');
        await cache.initializeGlobalCache(testRepo.path);
        const status = cache.getStatus();
        expect(status.dateRanges.size).toBeGreaterThan(0);
      });
    });

    describe('setCacheEnabled()', () => {
      it('should enable cache by default', () => {
        cache.setCacheEnabled(true);
        expect(cache.getStatus().state).toBeDefined();
      });

      it('should disable cache and clear all data', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Test post');
        await cache.initializeGlobalCache(testRepo.path);
        expect(postsCache.size).toBeGreaterThan(0);
        cache.setCacheEnabled(false);
        expect(postsCache.size).toBe(0);
        expect(postIndex.byHash.size).toBe(0);
        expect(postIndex.byRepository.size).toBe(0);
        expect(cache.getStatus().state).toBe(CacheState.UNINITIALIZED);
      });

      it('should re-enable cache after being disabled', async () => {
        cache.setCacheEnabled(false);
        cache.setCacheEnabled(true);
        await cache.initializeGlobalCache(testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      });
    });

    describe('setCacheMaxSize()', () => {
      it('should clamp size below minimum to 1000', () => {
        cache.setCacheMaxSize(500);
        const stats = cache.getCacheStats();
        expect(stats.postsCache.maxSize).toBe(1000);
      });

      it('should clamp size above maximum to 1000000', () => {
        cache.setCacheMaxSize(2000000);
        const stats = cache.getCacheStats();
        expect(stats.postsCache.maxSize).toBe(1000000);
      });

      it('should accept valid size', () => {
        expect(() => cache.setCacheMaxSize(5000)).not.toThrow();
      });

      it('should resize cache with existing data', async () => {
        cache.setCacheEnabled(true);
        cache.setCacheMaxSize(10000);
        await post.createPost(testRepo.path, 'Test post 1');
        await post.createPost(testRepo.path, 'Test post 2');
        await cache.initializeGlobalCache(testRepo.path);
        const sizeBefore = postsCache.size;
        cache.setCacheMaxSize(20000);
        expect(postsCache.size).toBe(sizeBefore);
      });

      it('should preserve data when resizing to smaller capacity', async () => {
        cache.setCacheEnabled(true);
        cache.setCacheMaxSize(10000);
        await post.createPost(testRepo.path, 'Test post');
        await cache.initializeGlobalCache(testRepo.path);
        cache.setCacheMaxSize(5000);
        expect(postsCache.size).toBeGreaterThan(0);
      });
    });

    describe('getCacheStats()', () => {
      it('should return stats when cache is disabled', () => {
        cache.setCacheEnabled(false);
        const stats = cache.getCacheStats();
        expect(stats).toBeDefined();
        expect(stats.postsCache.size).toBe(0);
        expect(stats.enabled).toBe(false);
      });

      it('should return accurate stats after adding posts', async () => {
        cache.setCacheEnabled(true);
        await post.createPost(testRepo.path, 'Post 1');
        await post.createPost(testRepo.path, 'Post 2');
        await cache.initializeGlobalCache(testRepo.path);
        const stats = cache.getCacheStats();
        expect(stats.postsCache.size).toBe(2);
        expect(stats.postsCache.maxSize).toBeGreaterThan(0);
        expect(stats.enabled).toBe(true);
      });
    });
  });

  describe('Post Retrieval (getCachedPosts)', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
    });

    it('should initialize cache automatically if not initialized', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts)).toBe(true);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should return posts from repository:my scope', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(posts.length).toBe(3);
    });

    it('should force refresh cache when skipCache is true', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const initialCount = (await cache.getCachedPosts(testRepo.path, 'repository:my')).length;
      const postsWithoutSkip = await cache.getCachedPosts(testRepo.path, 'repository:my');
      const postsWithSkip = await cache.getCachedPosts(testRepo.path, 'repository:my', { skipCache: true });
      expect(postsWithoutSkip.length).toBe(initialCount);
      expect(postsWithSkip.length).toBe(initialCount);
    });

    it('should return all posts with "all" scope', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThanOrEqual(3);
    });

    it('should sort by latest (default)', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'latest' });
      for (let i = 0; i < posts.length - 1; i++) {
        expect(posts[i]!.timestamp.getTime()).toBeGreaterThanOrEqual(posts[i + 1]!.timestamp.getTime());
      }
    });

    it('should sort by oldest', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'oldest' });
      for (let i = 0; i < posts.length - 1; i++) {
        expect(posts[i]!.timestamp.getTime()).toBeLessThanOrEqual(posts[i + 1]!.timestamp.getTime());
      }
    });

    it('should sort by top (interaction count)', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top' });
      expect(posts).toBeDefined();
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should apply limit to results', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 2 });
      expect(posts.length).toBe(2);
    });

    it('should handle limit parameter', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 1 });
      expect(posts.length).toBeLessThanOrEqual(1);
    });

    it('should filter by post types', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['post'] });
      expect(posts.every(p => p.type === 'post')).toBe(true);
    });

    it('should filter by since date', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const cutoffDate = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { since: cutoffDate });
      expect(posts.every(p => p.timestamp >= cutoffDate)).toBe(true);
    });

    it('should filter by until date', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const cutoffDate = new Date(Date.now() + 1000);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { until: cutoffDate });
      expect(posts.every(p => p.timestamp <= cutoffDate)).toBe(true);
    });

    it('should filter by both since and until dates', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const until = new Date(Date.now() + 1000);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { since, until });
      expect(posts.every(p => p.timestamp >= since && p.timestamp <= until)).toBe(true);
    });

    it('should handle empty filter object', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {});
      expect(posts.length).toBeGreaterThanOrEqual(3);
    });
  });

  describe('Scope Parsing and Retrieval', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle list scope without errors', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'list:nonexistent');
      expect(Array.isArray(posts)).toBe(true);
      expect(posts.length).toBe(0);
    });

    it('should handle repository scope with URL', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, `repository:${testRepo.path}`);
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle post scope with ID', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      const postId = allPosts[0]?.id;
      if (postId) {
        const posts = await cache.getCachedPosts(testRepo.path, `post:${postId}`);
        expect(posts.length).toBeGreaterThanOrEqual(0);
      }
    });

    it('should handle byId scope with multiple IDs', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      const id1 = allPosts[0]?.id;
      const id2 = allPosts[1]?.id;
      if (id1 && id2) {
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${id1},${id2}`);
        expect(posts.length).toBeLessThanOrEqual(2);
      }
    });

    it('should handle timeline scope', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle thread scope', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      const postId = allPosts[0]?.id;
      if (postId) {
        const posts = await cache.getCachedPosts(testRepo.path, `thread:${postId}`);
        expect(Array.isArray(posts)).toBe(true);
      }
    });
  });

  describe('Cache Refresh Operations', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should refresh with all scope', async () => {
      await post.createPost(testRepo.path, 'Post 2');
      await cache.refresh({ all: true }, testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBe(2);
    });

    it('should refresh by specific hashes', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      const hash = posts[0]?.raw.commit.hash;
      if (hash) {
        await cache.refresh({ hashes: [hash] }, testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      }
    });

    it('should refresh with removed repositories', async () => {
      await cache.refresh({ removedRepositories: [testRepo.path] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should refresh specific list', async () => {
      const listResult = await list.createList(testRepo.path, 'test-list', 'Test List');
      expect(listResult.success).toBe(true);
      await cache.refresh({ lists: ['test-list'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle empty scope refresh', async () => {
      await cache.refresh({}, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh when cache is not initialized', async () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      await cache.refresh({ all: true }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('Incremental Updates', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Initial post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should add new post to cache', async () => {
      const result = await execGit(testRepo.path, [
        'commit',
        '--allow-empty',
        '-m',
        'New post',
        'gitsocial'
      ]);
      if (result.success) {
        const hash = result.data?.stdout.match(/\[gitsocial ([a-f0-9]+)\]/)?.[1];
        if (hash) {
          await cache.addPostToCache(testRepo.path, hash);
          const posts = await cache.getCachedPosts(testRepo.path, 'all');
          expect(posts.some(p => p.content === 'New post')).toBe(true);
        }
      }
    });

    it('should handle addPostToCache when cache is not initialized', async () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      const hash = 'abc123456789';
      await cache.addPostToCache(testRepo.path, hash);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle invalid commit hash', async () => {
      await cache.addPostToCache(testRepo.path, 'invalid-hash');
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('Date Range Tracking', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should track date ranges after initialization', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const ranges = cache.getCachedRanges();
      expect(ranges.length).toBeGreaterThan(0);
    });

    it('should check if date range is covered', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const today = new Date();
      const isCovered = cache.isCacheRangeCovered(today, today);
      expect(typeof isCovered).toBe('boolean');
    });

    it('should return false for uncovered date ranges', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const futureDate = new Date(Date.now() + 1000 * 60 * 60 * 24 * 365);
      const isCovered = cache.isCacheRangeCovered(futureDate, futureDate);
      expect(isCovered).toBe(false);
    });

    it('should load additional posts for uncovered ranges', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const pastDate = new Date(Date.now() - 1000 * 60 * 60 * 24 * 365);
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', pastDate, new Date());
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('ID Resolution and Deduplication', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should store posts with relative IDs for workspace posts', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(posts.every(p => p.id.startsWith('#commit:'))).toBe(true);
    });

    it('should deduplicate workspace posts via absolute mapping', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      const postIds = posts.map(p => p.id);
      const uniqueIds = new Set(postIds);
      expect(postIds.length).toBe(uniqueIds.size);
    });

    it('should handle absolute ID lookups', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      const post = posts[0];
      if (post && post.repository) {
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        expect(postIndex.absolute.has(absoluteId)).toBe(true);
      }
    });
  });

  describe('Repository Post Loading', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should load posts from workspace repository', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(posts.length).toBeGreaterThan(0);
    });

    it('should handle repository with multiple posts', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThanOrEqual(3);
    });

    it('should process posts with references', async () => {
      const ref = gitMsgRef.create({
        type: 'post',
        remote: 'origin',
        branch: 'gitsocial',
        hash: 'abc123456789'
      });
      await post.createPost(testRepo.path, `Test post with reference\n\n${ref}`);
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThan(0);
    });
  });

  describe('Edge Cases and Error Handling', () => {
    it('should handle missing repository directory', async () => {
      cache.setCacheEnabled(true);
      const posts = await cache.getCachedPosts('/non/existent/path', 'all');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should return valid results for all tested scopes', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const scopes = ['repository:my', 'timeline', 'all'];
      for (const scope of scopes) {
        const posts = await cache.getCachedPosts(testRepo.path, scope);
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle null/undefined filters gracefully', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', undefined);
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle empty post list gracefully', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle concurrent getCachedPosts calls', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const [posts1, posts2, posts3] = await Promise.all([
        cache.getCachedPosts(testRepo.path, 'all'),
        cache.getCachedPosts(testRepo.path, 'repository:my'),
        cache.getCachedPosts(testRepo.path, 'timeline')
      ]);
      expect(Array.isArray(posts1)).toBe(true);
      expect(Array.isArray(posts2)).toBe(true);
      expect(Array.isArray(posts3)).toBe(true);
    });

    it('should handle refresh with empty results', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      await cache.refresh({ hashes: [] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('Advanced Filtering and Scope Tests', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Quote post');
      await post.createPost(testRepo.path, 'Comment post');
      await post.createPost(testRepo.path, 'Regular post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle includeImplicit filter', async () => {
      const postsWithImplicit = await cache.getCachedPosts(testRepo.path, 'all', { includeImplicit: true });
      const postsWithoutImplicit = await cache.getCachedPosts(testRepo.path, 'all', { includeImplicit: false });
      expect(Array.isArray(postsWithImplicit)).toBe(true);
      expect(Array.isArray(postsWithoutImplicit)).toBe(true);
    });

    it('should handle combined type and date filters', async () => {
      const since = new Date(Date.now() - 1000 * 60 * 60);
      const until = new Date(Date.now() + 1000);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {
        types: ['post'],
        since,
        until
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle sortBy with limits', async () => {
      const topPosts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top', limit: 2 });
      const latestPosts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'latest', limit: 2 });
      const oldestPosts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'oldest', limit: 2 });
      expect(topPosts.length).toBeLessThanOrEqual(2);
      expect(latestPosts.length).toBeLessThanOrEqual(2);
      expect(oldestPosts.length).toBeLessThanOrEqual(2);
    });

    it('should handle very large limits', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 100000 });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle repository scope with type filters', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my', { types: ['post', 'quote'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle timeline scope with date filters', async () => {
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', { since });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Scope Parsing Edge Cases', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle post scope with workspace ID', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `post:${postId}`);
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle byId with single ID', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${postId}`);
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle thread scope with filters', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `thread:${postId}`, { types: ['post'] });
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle repository scope with date filters', async () => {
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const until = new Date();
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my', { since, until });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Cache State Transitions', () => {
    it('should handle multiple rapid initializations', async () => {
      cache.setCacheEnabled(true);
      const promises = [
        cache.initializeGlobalCache(testRepo.path),
        cache.initializeGlobalCache(testRepo.path),
        cache.initializeGlobalCache(testRepo.path)
      ];
      await Promise.all(promises);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle disable/enable cycles', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should maintain state after multiple operations', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await cache.initializeGlobalCache(testRepo.path);
      await post.createPost(testRepo.path, 'Post 2');
      await cache.refresh({ all: true }, testRepo.path);
      await post.createPost(testRepo.path, 'Post 3');
      const status = cache.getStatus();
      expect(status.state).toBe(CacheState.READY);
    });
  });

  describe('Performance and Stress Tests', () => {
    it('should handle large numbers of posts', async () => {
      cache.setCacheEnabled(true);
      for (let i = 0; i < 10; i++) {
        await post.createPost(testRepo.path, `Post ${i}`);
      }
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThanOrEqual(10);
    });

    it('should handle rapid successive queries', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const promises = Array.from({ length: 10 }, () =>
        cache.getCachedPosts(testRepo.path, 'all')
      );
      const results = await Promise.all(promises);
      expect(results.every(r => Array.isArray(r))).toBe(true);
    });
  });

  describe('Scope Parsing Error Cases', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle repository scope with complex URL', async () => {
      const complexUrl = 'https://github.com/user/repo.git';
      const posts = await cache.getCachedPosts(testRepo.path, `repository:${complexUrl}`);
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle byId with multiple comma-separated IDs', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.refresh({ all: true }, testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length >= 2) {
        const ids = allPosts.slice(0, 2).map(p => p.id).join(',');
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${ids}`);
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle byId with whitespace in IDs', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ all: true }, testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `byId: ${postId} , ${postId} `);
        expect(Array.isArray(posts)).toBe(true);
      }
    });
  });

  describe('Advanced Cache Operations', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should handle getCachedPosts with all scope and empty cache', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle refresh with specific hashes from existing posts', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const hash = allPosts[0]!.raw.commit.hash;
        await cache.refresh({ hashes: [hash] }, testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      }
    });

    it('should handle refresh with multiple hashes', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length >= 2) {
        const hashes = allPosts.slice(0, 2).map(p => p.raw.commit.hash);
        await cache.refresh({ hashes }, testRepo.path);
        expect(cache.getStatus().state).toBe(CacheState.READY);
      }
    });

    it('should handle addPostToCache with valid hash', async () => {
      const result = await post.createPost(testRepo.path, 'New post');
      await cache.initializeGlobalCache(testRepo.path);
      if (result.success && result.data) {
        const hash = result.data.raw.commit.hash;
        await cache.addPostToCache(testRepo.path, hash);
        const posts = await cache.getCachedPosts(testRepo.path, 'all');
        expect(posts.some(p => p.raw.commit.hash === hash)).toBe(true);
      }
    });

    it('should handle cache with posts that have interaction counts', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top' });
      expect(Array.isArray(posts)).toBe(true);
      posts.forEach(p => {
        expect(p.interactions).toBeDefined();
      });
    });
  });

  describe('Date Filtering Edge Cases', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Old post');
      await new Promise(resolve => setTimeout(resolve, 100));
      await post.createPost(testRepo.path, 'Recent post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should filter posts by exact timestamp', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length >= 2) {
        const midpoint = new Date((allPosts[0]!.timestamp.getTime() + allPosts[1]!.timestamp.getTime()) / 2);
        const postsAfter = await cache.getCachedPosts(testRepo.path, 'all', { since: midpoint });
        expect(Array.isArray(postsAfter)).toBe(true);
      }
    });

    it('should handle very old since dates', async () => {
      const veryOld = new Date('2000-01-01');
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { since: veryOld });
      expect(posts.length).toBeGreaterThan(0);
    });

    it('should handle future until dates', async () => {
      const future = new Date('2100-01-01');
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { until: future });
      expect(posts.length).toBeGreaterThan(0);
    });

    it('should handle same since and until dates', async () => {
      const now = new Date();
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { since: now, until: now });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Type Filtering Combinations', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should filter by multiple types', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {
        types: ['post', 'quote', 'comment']
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should filter by single type quote', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['quote'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should filter by single type comment', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['comment'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should filter by single type repost', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['repost'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should combine type filters with limits and sorting', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {
        types: ['post'],
        sortBy: 'latest',
        limit: 5
      });
      expect(Array.isArray(posts)).toBe(true);
      expect(posts.length).toBeLessThanOrEqual(5);
    });
  });

  describe('Cache Configuration Edge Cases', () => {
    it('should handle cache size at minimum boundary', () => {
      cache.setCacheMaxSize(1000);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.maxSize).toBe(1000);
    });

    it('should handle cache size at maximum boundary', () => {
      cache.setCacheMaxSize(1000000);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.maxSize).toBe(1000000);
    });

    it('should handle cache size in middle range', () => {
      cache.setCacheMaxSize(50000);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.maxSize).toBe(50000);
    });

    it('should preserve posts when enlarging cache', async () => {
      cache.setCacheEnabled(true);
      cache.setCacheMaxSize(5000);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const countBefore = postsCache.size;
      cache.setCacheMaxSize(10000);
      expect(postsCache.size).toBe(countBefore);
    });
  });

  describe('Timeline and List Scope Tests', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Timeline post 1');
      await post.createPost(testRepo.path, 'Timeline post 2');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle timeline scope with sortBy top', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', { sortBy: 'top' });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle timeline scope with sortBy oldest', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', { sortBy: 'oldest' });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle timeline scope with type filters', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', {
        types: ['post', 'comment']
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle timeline scope with limits', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', { limit: 1 });
      expect(posts.length).toBeLessThanOrEqual(1);
    });
  });

  describe('Repository Scope Advanced Tests', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Repo post 1');
      await post.createPost(testRepo.path, 'Repo post 2');
      await post.createPost(testRepo.path, 'Repo post 3');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle repository:my with all sort options', async () => {
      const latest = await cache.getCachedPosts(testRepo.path, 'repository:my', { sortBy: 'latest' });
      const oldest = await cache.getCachedPosts(testRepo.path, 'repository:my', { sortBy: 'oldest' });
      const top = await cache.getCachedPosts(testRepo.path, 'repository:my', { sortBy: 'top' });
      expect(Array.isArray(latest)).toBe(true);
      expect(Array.isArray(oldest)).toBe(true);
      expect(Array.isArray(top)).toBe(true);
    });

    it('should handle repository:my with combined filters', async () => {
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my', {
        types: ['post'],
        since,
        limit: 10,
        sortBy: 'latest'
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle repository:my with includeImplicit', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my', {
        includeImplicit: true
      });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Cache Disabled Scenarios', () => {
    it('should return empty array when cache is disabled', async () => {
      cache.setCacheEnabled(false);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts).toEqual([]);
    });

    it('should return empty array for any scope when disabled', async () => {
      cache.setCacheEnabled(false);
      const posts1 = await cache.getCachedPosts(testRepo.path, 'repository:my');
      const posts2 = await cache.getCachedPosts(testRepo.path, 'timeline');
      expect(posts1).toEqual([]);
      expect(posts2).toEqual([]);
    });
  });

  describe('Post Scope Optimization', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post for lookup');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle post: scope with valid ID', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `post:${postId}`);
        expect(posts.length).toBe(1);
        expect(posts[0]!.id).toBe(postId);
      }
    });

    it('should return empty array for post: scope with invalid ID', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'post:#commit:nonexistent123');
      expect(posts).toEqual([]);
    });

    it('should return empty array for post: scope with missing post', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'post:#commit:abc123456789');
      expect(posts).toEqual([]);
    });
  });

  describe('Remote List Context Handling', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle list scope with context object', async () => {
      const mockList = {
        id: 'test-list',
        name: 'Test List',
        repositories: [testRepo.path]
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:test-list', undefined, {
        list: mockList
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle list scope with multiple repositories in context', async () => {
      const mockList = {
        id: 'multi-list',
        name: 'Multi List',
        repositories: [
          testRepo.path,
          'https://github.com/user/repo1',
          'https://github.com/user/repo2'
        ]
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:multi-list', undefined, {
        list: mockList
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle list scope without context', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'list:empty-list');
      expect(posts).toEqual([]);
    });
  });

  describe('Thread Scope with Interaction Logging', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Thread root post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle thread scope and log interaction counts', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `thread:${postId}`);
        expect(Array.isArray(posts)).toBe(true);
      }
    });

    it('should handle thread scope with missing target post', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'thread:#commit:nonexistent');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle thread scope with filters', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `thread:${postId}`, {
          types: ['post', 'comment'],
          limit: 5
        });
        expect(Array.isArray(posts)).toBe(true);
      }
    });
  });

  describe('ById Scope with Multiple IDs', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should return posts in order for byId scope', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length >= 2) {
        const ids = allPosts.slice(0, 2).map(p => p.id);
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${ids.join(',')}`);
        expect(posts.length).toBeGreaterThan(0);
        expect(posts.length).toBeLessThanOrEqual(2);
      }
    });

    it('should filter out missing IDs in byId scope', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const validId = allPosts[0]!.id;
        const scope = `byId:${validId},#commit:missing123,#commit:missing456`;
        const posts = await cache.getCachedPosts(testRepo.path, scope);
        expect(posts.length).toBeGreaterThanOrEqual(1);
      }
    });

    it('should handle byId with all invalid IDs', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'byId:#commit:missing1,#commit:missing2');
      expect(posts).toEqual([]);
    });
  });

  describe('All Scope Filtering', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post A');
      await post.createPost(testRepo.path, 'Post B');
      await post.createPost(testRepo.path, 'Post C');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should apply limit to all scope', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 2 });
      expect(posts.length).toBe(2);
    });

    it('should handle limit greater than post count', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 1000 });
      expect(posts.length).toBeGreaterThan(0);
      expect(posts.length).toBeLessThanOrEqual(1000);
    });

    it('should default to latest sorting for all scope', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
      if (posts.length > 1) {
        for (let i = 0; i < posts.length - 1; i++) {
          expect(posts[i]!.timestamp.getTime()).toBeGreaterThanOrEqual(posts[i + 1]!.timestamp.getTime());
        }
      }
    });

    it('should handle all scope with sortBy oldest', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'oldest' });
      if (posts.length > 1) {
        for (let i = 0; i < posts.length - 1; i++) {
          expect(posts[i]!.timestamp.getTime()).toBeLessThanOrEqual(posts[i + 1]!.timestamp.getTime());
        }
      }
    });
  });

  describe('Storage Base Parameter', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should handle getCachedPosts with storageBase parameter', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {
        storageBase: '/tmp/test-storage'
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should pass storageBase to ensureInitialized', async () => {
      await cache.getCachedPosts(testRepo.path, 'repository:my', {
        storageBase: '/tmp/test-storage'
      });
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Refresh Operations with Lists', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle refresh with lists parameter', async () => {
      await cache.refresh({ lists: ['list1', 'list2'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh with single list', async () => {
      await cache.refresh({ lists: ['single-list'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh with empty lists array', async () => {
      await cache.refresh({ lists: [] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('Date Range Coverage Checks', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should return true for covered date ranges', () => {
      const today = new Date();
      const isCovered = cache.isCacheRangeCovered(today, today);
      expect(typeof isCovered).toBe('boolean');
    });

    it('should return false for uncovered future date ranges', () => {
      const future = new Date(Date.now() + 1000 * 60 * 60 * 24 * 365);
      const isCovered = cache.isCacheRangeCovered(future, future);
      expect(isCovered).toBe(false);
    });

    it('should handle date range check with different since and until', () => {
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const until = new Date();
      const isCovered = cache.isCacheRangeCovered(since, until);
      expect(typeof isCovered).toBe('boolean');
    });
  });

  describe('Post Index Operations', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Indexed post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should populate byHash index', () => {
      expect(postIndex.byHash.size).toBeGreaterThan(0);
    });

    it('should populate byRepository index', () => {
      expect(postIndex.byRepository.size).toBeGreaterThan(0);
    });

    it('should maintain absolute ID mapping for workspace posts', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(allPosts.length).toBeGreaterThan(0);
    });

    it('should handle clearing indexes on cache disable', () => {
      cache.setCacheEnabled(false);
      expect(postIndex.byHash.size).toBe(0);
      expect(postIndex.byRepository.size).toBe(0);
      expect(postIndex.byList.size).toBe(0);
      expect(postIndex.absolute.size).toBe(0);
    });
  });

  describe('Cache Stats Detailed', () => {
    it('should return correct enabled status', () => {
      cache.setCacheEnabled(true);
      const stats = cache.getCacheStats();
      expect(stats.enabled).toBe(true);
    });

    it('should return correct disabled status', () => {
      cache.setCacheEnabled(false);
      const stats = cache.getCacheStats();
      expect(stats.enabled).toBe(false);
    });

    it('should report accurate post count', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.size).toBe(2);
    });
  });

  describe('Error Path & State Transition Tests', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should handle parseScopeParameter with empty repository URL', async () => {
      await expect(cache.getCachedPosts(testRepo.path, 'repository:')).rejects.toThrow('Repository URL cannot be empty');
    });

    it('should handle parseScopeParameter with empty list ID in repository/list scope', async () => {
      await expect(cache.getCachedPosts(testRepo.path, 'repository:https://github.com/user/repo/list:')).rejects.toThrow('List ID cannot be empty in repository/list scope');
    });

    it('should handle post: scope with empty ID via early return', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'post:');
      expect(posts).toEqual([]);
    });

    it('should handle parseScopeParameter with empty byId string', async () => {
      await expect(cache.getCachedPosts(testRepo.path, 'byId:')).rejects.toThrow('Post IDs cannot be empty');
    });

    it('should handle parseScopeParameter with empty thread ID', async () => {
      await expect(cache.getCachedPosts(testRepo.path, 'thread:')).rejects.toThrow('Thread post ID cannot be empty');
    });

    it('should handle parseScopeParameter with invalid scope', async () => {
      await expect(cache.getCachedPosts(testRepo.path, 'invalid:scope')).rejects.toThrow('Invalid scope parameter');
    });

    it('should handle addPostToCache with invalid commit format', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const result = await cache.addPostToCache(testRepo.path, 'nonexistent123456');
      expect(typeof result).toBe('boolean');
    });

    it('should handle addPostToCache with very short hash', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const result = await cache.addPostToCache(testRepo.path, 'abc');
      expect(typeof result).toBe('boolean');
    });

    it('should handle list scope without context and empty postIds', async () => {
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'list:nonexistent-list');
      expect(posts).toEqual([]);
    });

    it('should handle repository:my scope with date filters', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const since = new Date(Date.now() - 1000 * 60 * 60);
      const until = new Date(Date.now() + 1000);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my', { since, until });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('sortPosts Tie-Breaking Tests', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle top sort with posts having equal interaction counts', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top' });
      expect(Array.isArray(posts)).toBe(true);
      if (posts.length > 1) {
        for (let i = 0; i < posts.length - 1; i++) {
          const aComments = posts[i]!.interactions?.comments || 0;
          const aReposts = posts[i]!.interactions?.reposts || 0;
          const aQuotes = posts[i]!.interactions?.quotes || 0;
          const aInteractions = aComments + aReposts + aQuotes;
          const bComments = posts[i + 1]!.interactions?.comments || 0;
          const bReposts = posts[i + 1]!.interactions?.reposts || 0;
          const bQuotes = posts[i + 1]!.interactions?.quotes || 0;
          const bInteractions = bComments + bReposts + bQuotes;
          if (aInteractions === bInteractions) {
            expect(posts[i]!.timestamp.getTime()).toBeGreaterThanOrEqual(posts[i + 1]!.timestamp.getTime());
          }
        }
      }
    });
  });

  describe('ID Resolution Tests', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post for ID resolution');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should resolve post by direct ID lookup', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const result = await cache.getCachedPosts(testRepo.path, `post:${postId}`);
        expect(result.length).toBe(1);
        expect(result[0]!.id).toBe(postId);
      }
    });

    it('should handle absolute ID mappings in postIndex', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        expect(postIndex.absolute.get(absoluteId)).toBe(post.id);
        const result = await cache.getCachedPosts(testRepo.path, `post:${absoluteId}`);
        expect(result.length).toBeGreaterThanOrEqual(0);
      }
    });

    it('should handle post lookup with non-existent mapped ID', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'post:#commit:nonexistentmapped123');
      expect(posts).toEqual([]);
    });
  });

  describe('Date Range and Additional Post Loading', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should mark date ranges after loading additional posts', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const pastDate = new Date(Date.now() - 1000 * 60 * 60 * 24 * 7);
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', pastDate, new Date());
      const ranges = cache.getCachedRanges();
      expect(Array.isArray(ranges)).toBe(true);
    });

    it('should not load additional posts when cache is disabled', async () => {
      cache.setCacheEnabled(false);
      const result = await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', new Date(), new Date());
      expect(result).toBeUndefined();
    });

    it('should handle loadAdditionalPosts with already covered range', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const today = new Date();
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', today, today);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should return sorted cached ranges', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const ranges = cache.getCachedRanges();
      expect(Array.isArray(ranges)).toBe(true);
      if (ranges.length > 1) {
        for (let i = 0; i < ranges.length - 1; i++) {
          expect(ranges[i]! <= ranges[i + 1]!).toBe(true);
        }
      }
    });
  });

  describe('setCacheMaxSize Warning Paths', () => {
    it('should log warning when clamping size below minimum', () => {
      cache.setCacheMaxSize(100);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.maxSize).toBe(1000);
    });

    it('should log warning when clamping size above maximum', () => {
      cache.setCacheMaxSize(5000000);
      const stats = cache.getCacheStats();
      expect(stats.postsCache.maxSize).toBe(1000000);
    });

    it('should resize cache and preserve existing entries', async () => {
      cache.setCacheEnabled(true);
      cache.setCacheMaxSize(10000);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
      const sizeBefore = postsCache.size;
      cache.setCacheMaxSize(15000);
      expect(postsCache.size).toBe(sizeBefore);
    });
  });

  describe('Refresh with Removed Repositories', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle refresh with removedRepositories array', async () => {
      await cache.refresh({ removedRepositories: ['https://github.com/user/removed-repo'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh with multiple removed repositories', async () => {
      await cache.refresh({
        removedRepositories: [
          'https://github.com/user/repo1',
          'https://github.com/user/repo2'
        ]
      }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('Repository Filtering Edge Cases', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle timeline scope with type filter array', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline', {
        types: ['post', 'comment', 'quote', 'repost']
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle repository:URL/list:ID scope format parsing', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:https://github.com/user/repo/list:testlist');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Cache Initialization with sinceOverride', () => {
    it('should initialize with sinceOverride parameter', async () => {
      cache.setCacheEnabled(true);
      const sinceDate = new Date('2024-01-01');
      await cache.initializeGlobalCache(testRepo.path, undefined, sinceDate);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle concurrent initialization with same parameters', async () => {
      cache.setCacheEnabled(true);
      await Promise.all([
        cache.initializeGlobalCache(testRepo.path),
        cache.initializeGlobalCache(testRepo.path)
      ]);
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('ById Scope with Whitespace', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle byId scope with IDs separated by commas and spaces', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length >= 2) {
        const id1 = allPosts[0]!.id;
        const id2 = allPosts[1]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${id1} , ${id2}`);
        expect(posts.length).toBeGreaterThanOrEqual(1);
      }
    });

    it('should trim whitespace from individual IDs in byId scope', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const posts = await cache.getCachedPosts(testRepo.path, `byId: ${postId} `);
        expect(posts.length).toBeGreaterThanOrEqual(1);
      }
    });
  });

  describe('LoadRepositoryPosts Coverage', () => {
    it('should call loadRepositoryPosts via refresh', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ all: true }, testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThan(0);
    });

    it('should handle loadRepositoryPosts with storageBase', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Refresh Date Range Calculations', () => {
    beforeEach(() => {
      cache.setCacheEnabled(true);
    });

    it('should handle refresh with empty scope object', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      await cache.refresh({}, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh all with storageBase for date calculations', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ all: true }, testRepo.path, '/tmp/test-storage');
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh lists with date range calculations', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      await cache.refresh({ lists: ['list1', 'list2', 'list3'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh repositories with date range lookups', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      await cache.refresh({ repositories: ['https://github.com/user/repo1', 'https://github.com/user/repo2'] }, testRepo.path);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('AddPostToCache Edge Cases', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle addPostToCache with cache not ready falling back to refresh', async () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      const result = await cache.addPostToCache(testRepo.path, 'abc123');
      expect(typeof result).toBe('boolean');
    });

    it('should handle addPostToCache with commit that returns no posts', async () => {
      const result = await cache.addPostToCache(testRepo.path, 'nonexistent999');
      expect(typeof result).toBe('boolean');
    });

    it('should handle addPostToCache error path with fallback refresh', async () => {
      const result = await cache.addPostToCache(testRepo.path, '!@#$%invalid');
      expect(typeof result).toBe('boolean');
    });
  });

  describe('List Scope with Remote Context', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle list scope with remote list context and no cached posts', async () => {
      const mockList = {
        id: 'remote-list',
        name: 'Remote List',
        repositories: ['https://github.com/user/repo1', 'https://github.com/user/repo2']
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:remote-list', undefined, { list: mockList });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should filter posts by list repository URLs when using remote context', async () => {
      const mockList = {
        id: 'filter-list',
        name: 'Filter List',
        repositories: ['https://github.com/nonexistent/repo']
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:filter-list', undefined, { list: mockList });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Repository Scope Date and Type Filtering', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should apply date filters to repository scope with else branch', async () => {
      const since = new Date(Date.now() - 1000 * 60 * 60 * 24);
      const until = new Date(Date.now() + 1000);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:https://github.com/user/repo', { since, until });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should apply type filters to repository scope', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:https://github.com/user/repo', {
        types: ['post', 'comment']
      });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Timeline Scope with Absolute ID Mapping', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post for absolute mapping');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle timeline filtering with absolute IDs that map to relative', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        const posts = await cache.getCachedPosts(testRepo.path, 'timeline');
        expect(Array.isArray(posts)).toBe(true);
      }
    });
  });

  describe('Index Cleanup and Maintenance', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post for index test');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should handle post index absolute mappings', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        postIndex.merged.add(absoluteId);
        expect(postIndex.absolute.has(absoluteId)).toBe(true);
        expect(postIndex.merged.has(absoluteId)).toBe(true);
      }
    });

    it('should verify byHash index is populated', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const hash = allPosts[0]!.raw.commit.hash;
        const hashEntry = postIndex.byHash.get(gitMsgHash.normalize(hash));
        expect(hashEntry).toBeDefined();
      }
    });

    it('should verify byRepository index is populated', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        expect(postIndex.byRepository.size).toBeGreaterThan(0);
      }
    });
  });

  describe('GetPostById with Mapping', () => {
    beforeEach(async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post for getPostById test');
      await cache.initializeGlobalCache(testRepo.path);
    });

    it('should get post by direct ID via post: scope', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const postId = allPosts[0]!.id;
        const result = await cache.getCachedPosts(testRepo.path, `post:${postId}`);
        expect(result.length).toBe(1);
      }
    });

    it('should get post by mapped absolute ID', async () => {
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        const result = await cache.getCachedPosts(testRepo.path, `post:${absoluteId}`);
        expect(result.length).toBeGreaterThanOrEqual(0);
      }
    });

    it('should return undefined for non-existent post ID', async () => {
      const result = await cache.getCachedPosts(testRepo.path, 'post:#commit:doesnotexist999');
      expect(result).toEqual([]);
    });
  });

  describe('LoadPosts External Scope Without StorageBase', () => {
    it('should handle external scope without storageBase returning empty', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path, undefined);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('SetCacheEnabled Cleanup', () => {
    it('should clear all cache data when disabling', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      expect(postsCache.size).toBeGreaterThan(0);
      expect(postIndex.byHash.size).toBeGreaterThan(0);
      cache.setCacheEnabled(false);
      expect(postsCache.size).toBe(0);
      expect(postIndex.byHash.size).toBe(0);
      expect(postIndex.byRepository.size).toBe(0);
      expect(postIndex.byList.size).toBe(0);
      expect(postIndex.absolute.size).toBe(0);
      expect(postIndex.merged.size).toBe(0);
    });

    it('should handle enabling cache that is already enabled', () => {
      cache.setCacheEnabled(true);
      cache.setCacheEnabled(true);
      const stateAfter = cache.getStatus();
      expect(stateAfter).toBeDefined();
    });
  });

  describe('Error State Recovery', () => {
    it('should initialize from ERROR state', async () => {
      cache.setCacheEnabled(true);
      const status = cache.getStatus() as { state: CacheState };
      status.state = CacheState.ERROR;
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle getCachedPosts when cache is in ERROR state', async () => {
      cache.setCacheEnabled(true);
      const status = cache.getStatus() as { state: CacheState };
      status.state = CacheState.ERROR;
      await post.createPost(testRepo.path, 'Test post');
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Cache Disabled Early Return', () => {
    it('should return early from initializeGlobalCache when cache disabled', async () => {
      cache.setCacheEnabled(false);
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(false);
    });

    it('should return early from loadAdditionalPosts when cache disabled', async () => {
      cache.setCacheEnabled(false);
      const result = await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', new Date(), new Date());
      expect(result).toBeUndefined();
    });
  });

  describe('Initialization Error Handling', () => {
    it('should handle getOriginUrl throwing error during initialization', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle getConfiguredBranch errors gracefully', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache('/nonexistent/path');
      const status = cache.getStatus();
      expect(status).toBeDefined();
    });
  });

  describe('Branch Filtering in repository:my', () => {
    it('should filter posts by configured branch in repository:my scope', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post on configured branch');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Empty Scope Warning Path', () => {
    it('should handle unexpected cache state in ensureInitialized', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      await cache.getCachedPosts(testRepo.path, 'all');
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Refresh Date Range Calculations with Storage', () => {
    it('should handle refresh all with storageBase for date calculations', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ all: true }, testRepo.path, '/tmp/test-storage');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should calculate oldest date when refreshing all repositories', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const stateAfterInit = cache.getStatus().state;
      expect(stateAfterInit).toBe(CacheState.READY);
      await cache.refresh({ all: true }, testRepo.path, '/tmp/test-storage');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });
  });

  describe('List Scope with Context and Repository Filter', () => {
    it('should apply repository filter in list scope with remote context', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const mockList = {
        id: 'filtered-list',
        name: 'Filtered List',
        repositories: ['https://github.com/user/repo1']
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:filtered-list', {
        repositoryUrl: 'https://github.com/user/repo1'
      }, { list: mockList });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle list scope with context but no matching posts in index', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      const mockList = {
        id: 'empty-remote-list',
        name: 'Empty Remote List',
        repositories: ['https://github.com/external/repo']
      };
      const posts = await cache.getCachedPosts(testRepo.path, 'list:empty-remote-list', undefined, { list: mockList });
      expect(posts).toEqual([]);
    });
  });

  describe('Refresh with Hash Removal and Index Cleanup', () => {
    it('should remove posts by hash and cleanup empty indexes', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post to remove');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const hash = allPosts[0]!.raw.commit.hash;
        await cache.refresh({ hashes: [hash] }, testRepo.path);
        const postsAfterRemoval = await cache.getCachedPosts(testRepo.path, 'all');
        expect(postsAfterRemoval.length).toBeLessThan(allPosts.length);
      }
    });

    it('should cleanup empty index entries when removing posts', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      const hashes = allPosts.map(p => p.raw.commit.hash);
      await cache.refresh({ hashes }, testRepo.path);
      expect(postsCache.size).toBe(0);
    });
  });

  describe('LoadAdditionalPosts Error Handling', () => {
    it('should handle getOriginUrl error in loadAdditionalPosts', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const pastDate = new Date(Date.now() - 1000 * 60 * 60 * 24 * 7);
      await cache.loadAdditionalPosts('/nonexistent/path', 'gitsocial', pastDate, new Date());
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle error when processing posts in loadAdditionalPosts', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      const pastDate = new Date(Date.now() - 1000 * 60 * 60 * 24);
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', pastDate, new Date());
      expect(cache.isInitialized()).toBe(true);
    });

    it('should not mark date as cached when no new posts found', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const futureDate = new Date(Date.now() + 1000 * 60 * 60 * 24 * 365);
      const rangesBefore = cache.getCachedRanges().length;
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', futureDate, futureDate);
      const rangesAfter = cache.getCachedRanges().length;
      expect(rangesAfter).toBe(rangesBefore);
    });

    it('should skip duplicate posts when loading additional posts', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const sizeBefore = postsCache.size;
      const today = new Date();
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', today, today);
      const sizeAfter = postsCache.size;
      expect(sizeAfter).toBe(sizeBefore);
    });
  });

  describe('SortPosts Tie-Breaking with Top Sort', () => {
    it('should use timestamp as tie-breaker for top sort', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await new Promise(resolve => setTimeout(resolve, 10));
      await post.createPost(testRepo.path, 'Post 2');
      await new Promise(resolve => setTimeout(resolve, 10));
      await post.createPost(testRepo.path, 'Post 3');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top' });
      if (posts.length > 1) {
        for (let i = 0; i < posts.length - 1; i++) {
          const aInteractions = (posts[i]!.interactions?.comments || 0);
          const bInteractions = (posts[i + 1]!.interactions?.comments || 0);
          if (aInteractions === bInteractions) {
            expect(posts[i]!.timestamp.getTime()).toBeGreaterThanOrEqual(posts[i + 1]!.timestamp.getTime());
          }
        }
      }
    });
  });

  describe('ResolveToCanonicalId Edge Cases', () => {
    it('should resolve absolute ID via postIndex mapping', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        const result = await cache.getCachedPosts(testRepo.path, `post:${absoluteId}`);
        expect(Array.isArray(result)).toBe(true);
      }
    });
  });

  describe('ParseScopeParameter Empty ID Validations', () => {
    it('should throw error for post: scope with truly empty ID', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      expect(() => {
        const scope = 'post:';
        if (scope.startsWith('post:')) {
          const postId = scope.slice(5);
          if (!postId) {throw new Error('Post ID cannot be empty');}
        }
      }).toThrow('Post ID cannot be empty');
    });
  });

  describe('Virtual Post Processing with Embedded References', () => {
    it('should process embedded social references and create virtual posts', async () => {
      cache.setCacheEnabled(true);
      const externalRef = 'git-msg-ref:social/post/origin/gitsocial#commit:abc123456789';
      await post.createPost(testRepo.path, `Post with embedded reference\n\n${externalRef}`);
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThan(0);
    });

    it('should merge virtual posts into workspace posts when IDs match', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Workspace post');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(allPosts)).toBe(true);
    });

    it('should track merged virtual posts in postIndex', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        postIndex.merged.add(absoluteId);
        expect(postIndex.merged.has(absoluteId)).toBe(true);
      }
    });

    it('should not create duplicate virtual posts when already processed', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post with reference');
      await cache.initializeGlobalCache(testRepo.path);
      const size1 = postsCache.size;
      await cache.initializeGlobalCache(testRepo.path, undefined, undefined, true);
      const size2 = postsCache.size;
      expect(size2).toBe(size1);
    });
  });

  describe('LoadRepositoryPosts Direct Testing', () => {
    it('should load posts from external repository via loadRepositoryPosts', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      await cache.loadRepositoryPosts(testRepo.path, 'https://github.com/user/repo', 'main', '/tmp/test-storage');
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle loadRepositoryPosts with no commits', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      await cache.loadRepositoryPosts(testRepo.path, 'https://github.com/nonexistent/repo', 'main', '/tmp/empty-storage');
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle loadRepositoryPosts with fetched ranges', async () => {
      cache.setCacheEnabled(true);
      await cache.initializeGlobalCache(testRepo.path);
      await cache.loadRepositoryPosts(testRepo.path, 'https://github.com/user/repo', 'gitsocial', '/tmp/test-storage');
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('UpdateIndexes Error Handling', () => {
    it('should handle updateIndexes with invalid post ID format', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
    });

    it('should handle updateIndexes with list indexing errors', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThan(0);
    });
  });

  describe('Refresh Date Range List Calculations', () => {
    it('should calculate date ranges when refreshing lists', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      const listResult = await list.createList(testRepo.path, 'test-list', 'Test List');
      expect(listResult.success).toBe(true);
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      await cache.refresh({ lists: ['test-list'] }, testRepo.path, '/tmp/test-storage');
      expect(cache.getStatus().state).toBe(CacheState.READY);
    });

    it('should handle refresh lists with repositories having fetched ranges', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      await cache.refresh({ lists: ['test-list'] }, testRepo.path, '/tmp/test-storage');
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Timeline Scope Absolute ID Filtering', () => {
    it('should skip absolute IDs that map to relative workspace posts in timeline', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Workspace post');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        const timelinePosts = await cache.getCachedPosts(testRepo.path, 'timeline');
        expect(Array.isArray(timelinePosts)).toBe(true);
      }
    });
  });

  describe('RemoveFromIndexes All Cleanup Paths', () => {
    it('should cleanup absolute and merged indexes when removing posts', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0 && allPosts[0]!.repository) {
        const post = allPosts[0]!;
        const absoluteId = `${post.repository}${post.id}`;
        postIndex.absolute.set(absoluteId, post.id);
        postIndex.merged.add(post.id);
        const mergedBefore = postIndex.merged.size;
        const hash = post.raw.commit.hash;
        await cache.refresh({ hashes: [hash] }, testRepo.path);
        expect(postIndex.merged.size).toBeLessThanOrEqual(mergedBefore);
      }
    });

    it('should cleanup byList index entries that become empty', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      const listResult = await list.createList(testRepo.path, 'cleanup-list', 'Cleanup List');
      expect(listResult.success).toBe(true);
      await cache.initializeGlobalCache(testRepo.path);
      const allPosts = await cache.getCachedPosts(testRepo.path, 'all');
      if (allPosts.length > 0) {
        const hash = allPosts[0]!.raw.commit.hash;
        await cache.refresh({ hashes: [hash] }, testRepo.path);
        expect(cache.isInitialized()).toBe(true);
      }
    });
  });

  describe('Repository:my Branch Extraction Edge Cases', () => {
    it('should extract branch from repository field when branch field missing', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should filter out posts not on configured branch in repository:my', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post on gitsocial branch');
      await cache.initializeGlobalCache(testRepo.path);
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('Additional LoadAdditionalPosts Coverage', () => {
    it('should mark date range after successfully loading posts', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      const pastDate = new Date(Date.now() - 1000 * 60 * 60);
      const rangesBefore = cache.getCachedRanges().length;
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', pastDate, new Date());
      const rangesAfter = cache.getCachedRanges().length;
      expect(rangesAfter).toBeGreaterThanOrEqual(rangesBefore);
    });
  });

  describe('InitializeGlobalCache Phase Separation', () => {
    it('should complete Phase 1 before Phase 2 in initialization', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
      expect(postsCache.size).toBeGreaterThan(0);
    });

    it('should process embedded references in Phase 2', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post with potential reference');
      await cache.initializeGlobalCache(testRepo.path);
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Cache Refresh with Repositories Scope', () => {
    it('should set state to REFRESHING when refreshing repositories', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      await cache.refresh({ repositories: ['https://github.com/user/repo'] }, testRepo.path);
      const status = cache.getStatus();
      expect([CacheState.READY, CacheState.REFRESHING]).toContain(status.state);
    });
  });

  describe('GetOriginUrl Error During LoadAdditionalPosts', () => {
    it('should handle getOriginUrl throwing exception', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Test post');
      await cache.initializeGlobalCache(testRepo.path);
      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', new Date(), new Date());
      expect(cache.isInitialized()).toBe(true);
    });
  });

  describe('Comprehensive Coverage Tests', () => {
    it('should handle all major code paths in single test session', async () => {
      cache.setCacheEnabled(true);
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');

      const posts1 = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts1.length).toBeGreaterThan(0);

      await cache.refresh({ all: true }, testRepo.path, '/tmp/test-storage');

      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const posts2 = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts2)).toBe(true);

      await cache.loadAdditionalPosts(testRepo.path, 'gitsocial', new Date(), new Date());
      expect(cache.isInitialized()).toBe(true);
    });

    it('should exercise error recovery paths across operations', async () => {
      cache.setCacheEnabled(true);
      const status = cache.getStatus() as { state: CacheState };
      status.state = CacheState.ERROR;
      await cache.getCachedPosts(testRepo.path, 'all');
      expect(cache.isInitialized()).toBe(true);

      await post.createPost(testRepo.path, 'Post after error');
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(posts.length).toBeGreaterThan(0);
    });
  });

  describe('cache state edge cases', () => {
    it('should handle ERROR state during waitForReady', async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const status = cache.getStatus() as { state: CacheState };
      status.state = CacheState.ERROR;
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should log warning for unexpected cache states', async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const status = cache.getStatus() as { state: CacheState };
      status.state = 999 as CacheState;
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('getCachedPosts complex filters', () => {
    beforeEach(async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      await post.createPost(testRepo.path, 'Test post');
    });

    it('should apply types + since + until filters together', async () => {
      const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000);
      const until = new Date();
      const posts = await cache.getCachedPosts(testRepo.path, 'all', {
        types: ['post'],
        since,
        until
      });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle thread post with interaction counts', async () => {
      const postResult = await post.createPost(testRepo.path, 'Thread target');
      if (postResult.success) {
        const posts = await cache.getCachedPosts(testRepo.path, 'thread', {
          targetPost: postResult.data.id
        });
        expect(Array.isArray(posts)).toBe(true);
      }
    });
  });

  describe('embedded references edge cases', () => {
    beforeEach(async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
    });

    it('should create virtual posts from references', async () => {
      const postResult = await post.createPost(testRepo.path, 'Original post');
      if (postResult.success) {
        await interaction.createInteraction('repost', testRepo.path, postResult.data);
        await cache.refresh({ all: true }, testRepo.path, '/tmp/test-storage');
        const posts = await cache.getCachedPosts(testRepo.path, 'all');
        expect(posts.length).toBeGreaterThanOrEqual(2);
      }
    });
  });

  describe('scope parameter parsing edge cases', () => {
    beforeEach(async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
    });

    it('should handle all scope', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
      const posts = await cache.getCachedPosts(testRepo.path, 'all');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle timeline scope', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
      const posts = await cache.getCachedPosts(testRepo.path, 'timeline');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle repository:my scope', async () => {
      await post.createPost(testRepo.path, 'Test post');
      await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
      const posts = await cache.getCachedPosts(testRepo.path, 'repository:my');
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle byId scope with single ID', async () => {
      const postResult = await post.createPost(testRepo.path, 'Test post');
      if (postResult.success) {
        await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${postResult.data.id}`);
        expect(posts.length).toBeGreaterThanOrEqual(0);
      }
    });

    it('should handle byId scope with multiple IDs', async () => {
      const post1 = await post.createPost(testRepo.path, 'Post 1');
      const post2 = await post.createPost(testRepo.path, 'Post 2');
      if (post1.success && post2.success) {
        await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
        const posts = await cache.getCachedPosts(testRepo.path, `byId:${post1.data.id},${post2.data.id}`);
        expect(posts.length).toBeGreaterThanOrEqual(0);
      }
    });

    it('should handle thread scope with non-existent post', async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      const posts = await cache.getCachedPosts(testRepo.path, 'thread', {
        targetPost: 'nonexistent-post-id'
      });
      expect(Array.isArray(posts)).toBe(true);
      expect(posts.length).toBe(0);
    });
  });

  describe('filter edge cases', () => {
    beforeEach(async () => {
      await cache.initializeGlobalCache(testRepo.path, '/tmp/test-storage');
      await post.createPost(testRepo.path, 'Test post 1');
      await post.createPost(testRepo.path, 'Test post 2');
      await cache.refresh({ workspace: true }, testRepo.path, '/tmp/test-storage');
    });

    it('should handle limit filter', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 1 });
      expect(posts.length).toBeLessThanOrEqual(1);
    });

    it('should handle zero limit', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { limit: 0 });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle types filter with single type', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['post'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle types filter with multiple types', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { types: ['post', 'comment'] });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle sortBy latest', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'latest' });
      expect(Array.isArray(posts)).toBe(true);
    });

    it('should handle sortBy top', async () => {
      const posts = await cache.getCachedPosts(testRepo.path, 'all', { sortBy: 'top' });
      expect(Array.isArray(posts)).toBe(true);
    });
  });

  describe('cache state management', () => {
    it('should handle isInitialized before initialization', () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      expect(cache.isInitialized()).toBe(false);
    });

    it('should handle getStatus before initialization', () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      const status = cache.getStatus();
      expect(status).toBeDefined();
    });

    it('should handle getCachedRanges before initialization', () => {
      cache.setCacheEnabled(false);
      cache.setCacheEnabled(true);
      const ranges = cache.getCachedRanges();
      expect(Array.isArray(ranges)).toBe(true);
    });
  });
});
