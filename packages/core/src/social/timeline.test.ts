import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { timeline } from './timeline';
import { cache } from './post/cache';
import { repository } from './repository';
import { post } from './post';
import { storage } from '../storage';
import { gitMsgRef } from '../gitmsg/protocol';
import type { Post, Repository, TimelineEntry } from './types';

describe('timeline', () => {
  const originalLogLevel = process.env['GITSOCIAL_LOG_LEVEL'];
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    if (originalLogLevel !== undefined) {
      process.env['GITSOCIAL_LOG_LEVEL'] = originalLogLevel;
    } else {
      delete process.env['GITSOCIAL_LOG_LEVEL'];
    }
  });
  function createMockPost(overrides: Partial<Post> = {}): Post {
    return {
      id: '#commit:abc123',
      repository: 'https://github.com/user/repo',
      author: { name: 'Test User', email: 'test@example.com' },
      timestamp: new Date('2024-01-15T10:00:00Z'),
      content: 'Test post content',
      type: 'post',
      source: 'explicit',
      isWorkspacePost: false,
      raw: {
        commit: {
          hash: 'abc123',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: 'Test post content'
        }
      },
      cleanContent: 'Test post content',
      interactions: { comments: 0, reposts: 0, quotes: 0 },
      display: {
        repositoryName: 'test-repo',
        commitHash: 'abc123',
        commitUrl: '',
        totalReposts: 0,
        isEmpty: false,
        isUnpushed: false,
        isOrigin: false,
        isWorkspacePost: false
      },
      ...overrides
    };
  }
  function createMockTimelineEntry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
    return {
      post: createMockPost(),
      source: 'following' as const,
      ...overrides
    };
  }
  function createMockRepository(overrides: Partial<Repository> = {}): Repository {
    return {
      id: 'https://github.com/user/repo#main',
      url: 'https://github.com/user/repo',
      name: 'test-repo',
      author: { name: 'Test User', email: 'test@example.com' },
      description: 'Test repository',
      stats: {
        posts: 0,
        followers: 0,
        following: 0
      },
      ...overrides
    };
  }
  describe('getTimelineStats()', () => {
    it('should return empty stats for empty entries array', () => {
      const result = timeline.getTimelineStats([]);
      expect(result.totalPosts).toBe(0);
      expect(result.postsByType).toEqual({});
      expect(result.postsByAuthor).toEqual({});
      expect(result.postsByRepository).toEqual({});
      expect(result.dateRange.start).toBe(null);
      expect(result.dateRange.end).toBe(null);
    });
    it('should calculate stats for single entry', () => {
      const entry = createMockTimelineEntry({
        post: createMockPost({
          type: 'post',
          author: { name: 'User 1', email: 'user1@example.com' },
          repository: 'https://github.com/user1/repo',
          timestamp: new Date('2024-01-15T10:00:00Z')
        })
      });
      const result = timeline.getTimelineStats([entry]);
      expect(result.totalPosts).toBe(1);
      expect(result.postsByType).toEqual({ post: 1 });
      expect(result.postsByAuthor).toEqual({ 'user1@example.com': 1 });
      expect(result.postsByRepository).toEqual({ 'https://github.com/user1/repo': 1 });
      expect(result.dateRange.start).toEqual(new Date('2024-01-15T10:00:00Z'));
      expect(result.dateRange.end).toEqual(new Date('2024-01-15T10:00:00Z'));
    });
    it('should aggregate posts by type', () => {
      const entries = [
        createMockTimelineEntry({ post: createMockPost({ type: 'post' }) }),
        createMockTimelineEntry({ post: createMockPost({ type: 'post' }) }),
        createMockTimelineEntry({ post: createMockPost({ type: 'comment' }) }),
        createMockTimelineEntry({ post: createMockPost({ type: 'repost' }) }),
        createMockTimelineEntry({ post: createMockPost({ type: 'quote' }) })
      ];
      const result = timeline.getTimelineStats(entries);
      expect(result.totalPosts).toBe(5);
      expect(result.postsByType).toEqual({
        post: 2,
        comment: 1,
        repost: 1,
        quote: 1
      });
    });
    it('should aggregate posts by author', () => {
      const entries = [
        createMockTimelineEntry({
          post: createMockPost({ author: { name: 'User 1', email: 'user1@example.com' } })
        }),
        createMockTimelineEntry({
          post: createMockPost({ author: { name: 'User 1', email: 'user1@example.com' } })
        }),
        createMockTimelineEntry({
          post: createMockPost({ author: { name: 'User 2', email: 'user2@example.com' } })
        })
      ];
      const result = timeline.getTimelineStats(entries);
      expect(result.postsByAuthor).toEqual({
        'user1@example.com': 2,
        'user2@example.com': 1
      });
    });
    it('should aggregate posts by repository', () => {
      const entries = [
        createMockTimelineEntry({ post: createMockPost({ repository: 'https://github.com/user1/repo' }) }),
        createMockTimelineEntry({ post: createMockPost({ repository: 'https://github.com/user1/repo' }) }),
        createMockTimelineEntry({ post: createMockPost({ repository: 'https://github.com/user2/repo' }) })
      ];
      const result = timeline.getTimelineStats(entries);
      expect(result.postsByRepository).toEqual({
        'https://github.com/user1/repo': 2,
        'https://github.com/user2/repo': 1
      });
    });
    it('should calculate correct date range', () => {
      const entries = [
        createMockTimelineEntry({ post: createMockPost({ timestamp: new Date('2024-01-15T10:00:00Z') }) }),
        createMockTimelineEntry({ post: createMockPost({ timestamp: new Date('2024-01-10T08:00:00Z') }) }),
        createMockTimelineEntry({ post: createMockPost({ timestamp: new Date('2024-01-20T12:00:00Z') }) }),
        createMockTimelineEntry({ post: createMockPost({ timestamp: new Date('2024-01-12T14:00:00Z') }) })
      ];
      const result = timeline.getTimelineStats(entries);
      expect(result.dateRange.start).toEqual(new Date('2024-01-10T08:00:00Z'));
      expect(result.dateRange.end).toEqual(new Date('2024-01-20T12:00:00Z'));
    });
  });
  describe('getWeekPosts()', () => {
    const weekStart = new Date('2024-01-15T00:00:00Z');
    const weekEnd = new Date('2024-01-21T23:59:59Z');
    const workdir = '/test/workdir';
    const storageBase = '/test/storage';
    it('should get posts without storageBase (skip ensureWeekData)', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [createMockRepository()];
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(result.data?.posts).toEqual(mockPosts);
      expect(result.data?.repositories).toEqual(mockRepos);
      expect(post.getPosts).toHaveBeenCalledWith(workdir, 'timeline', {
        types: undefined,
        since: weekStart,
        until: weekEnd,
        limit: undefined,
        skipCache: undefined,
        storageBase: undefined
      });
    });
    it('should get posts with storageBase and trigger ensureWeekData', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(true);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(result.data?.posts).toEqual(mockPosts);
      expect(cache.isCacheRangeCovered).toHaveBeenCalledWith(weekStart);
    });
    it('should handle options (types, limit, skipCache)', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      const options = {
        types: ['post', 'comment'] as Array<'post' | 'comment'>,
        limit: 50,
        skipCache: true
      };
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd, options);
      expect(result.success).toBe(true);
      expect(post.getPosts).toHaveBeenCalledWith(workdir, 'timeline', {
        types: options.types,
        since: weekStart,
        until: weekEnd,
        limit: 50,
        skipCache: true,
        storageBase: undefined
      });
    });
    it('should return error when ensureWeekData fails', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockRejectedValue(new Error('Load failed'));
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('ENSURE_DATA_ERROR');
    });
    it('should return error when post.getPosts fails', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch' }
      });
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('FETCH_ERROR');
    });
    it('should return error when post.getPosts returns no data', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: undefined
      });
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('FETCH_FAILED');
      expect(result.error?.message).toBe('Failed to get timeline posts');
    });
    it('should handle repository.getRepositories failure (non-blocking)', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: false,
        error: { code: 'REPO_ERROR', message: 'Failed' }
      });
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(result.data?.posts).toEqual(mockPosts);
      expect(result.data?.repositories).toBeUndefined();
    });
    it('should handle caught exception', async () => {
      vi.spyOn(post, 'getPosts').mockRejectedValue(new Error('Unexpected error'));
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('TIMELINE_ERROR');
      expect(result.error?.message).toBe('Unexpected error');
    });
    it('should handle non-Error exception', async () => {
      vi.spyOn(post, 'getPosts').mockRejectedValue('String error');
      const result = await timeline.getWeekPosts(workdir, undefined, weekStart, weekEnd);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('TIMELINE_ERROR');
      expect(result.error?.message).toBe('Failed to get timeline posts');
    });
    it('should trigger background prefetch for adjacent weeks', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(true);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      await new Promise(resolve => setTimeout(resolve, 10));
      expect(cache.isCacheRangeCovered).toHaveBeenCalled();
    });
  });
  describe('ensureWeekData (via getWeekPosts)', () => {
    const weekStart = new Date('2024-01-15T00:00:00Z');
    const weekEnd = new Date('2024-01-21T23:59:59Z');
    const workdir = '/test/workdir';
    const storageBase = '/test/storage';
    it('should skip loading when data already in cache', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(true);
      const loadSpy = vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(loadSpy).not.toHaveBeenCalled();
    });
    it('should load additional posts when data not in cache', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      const loadSpy = vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(loadSpy).toHaveBeenCalledWith(workdir, storageBase, weekStart);
    });
  });
  describe('fetchRepositoriesForWeek (via getWeekPosts)', () => {
    const weekStart = new Date('2024-01-15T00:00:00Z');
    const weekEnd = new Date('2024-01-21T23:59:59Z');
    const workdir = '/test/workdir';
    const storageBase = '/test/storage';
    it('should handle no repositories to fetch', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: [] })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should handle repository.getRepositories failure', async () => {
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: false, error: { code: 'ERROR', message: 'Failed' } })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should handle repositories with no fetchedRanges', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({ id: 'https://github.com/user/repo1#main', fetchedRanges: undefined })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      const fetchUpdatesSpy = vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(fetchUpdatesSpy).toHaveBeenCalled();
    });
    it('should handle repositories with empty fetchedRanges', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({ id: 'https://github.com/user/repo1#main', fetchedRanges: [] })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      const fetchUpdatesSpy = vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(fetchUpdatesSpy).toHaveBeenCalled();
    });
    it('should fetch repositories when week not covered by fetchedRanges', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: [{ start: '2024-01-20', end: '2024-01-25' }]
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      const fetchUpdatesSpy = vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(fetchUpdatesSpy).toHaveBeenCalledWith(
        workdir,
        'repository:https://github.com/user/repo1#main',
        {
          branch: 'main',
          since: '2024-01-15'
        }
      );
    });
    it('should skip fetch when week is covered by fetchedRanges', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: [{ start: '2024-01-10', end: '2024-01-25' }]
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      const fetchSpy = vi.spyOn(repository, 'fetchUpdates');
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(fetchSpy).not.toHaveBeenCalled();
    });
    it('should handle repository missing ID', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [createMockRepository({ id: undefined })];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should handle fetchUpdates failure', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed' }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should handle fetchUpdates throwing error', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockRejectedValue(new Error('Fetch error'));
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should clear cache after successful fetches', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      const clearSpy = vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(clearSpy).toHaveBeenCalledWith(workdir, 'following');
    });
    it('should not clear cache when no successful fetches', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 0, failed: 1 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      const clearSpy = vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(clearSpy).not.toHaveBeenCalled();
    });
    it('should load posts for repositories needing fetch', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      const loadRepoSpy = vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(loadRepoSpy).toHaveBeenCalledWith(
        workdir,
        'https://github.com/user/repo1',
        'main',
        storageBase
      );
    });
    it('should handle loadRepositoryPosts failure (non-blocking)', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockRejectedValue(new Error('Load failed'));
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
    });
    it('should handle mixed successful and failed fetches', async () => {
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: []
        }),
        createMockRepository({
          id: 'https://github.com/user/repo2#main',
          fetchedRanges: []
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates')
        .mockResolvedValueOnce({ success: true, data: { fetched: 1, failed: 0 } })
        .mockResolvedValueOnce({ success: false, error: { code: 'ERROR', message: 'Failed' } });
      vi.spyOn(gitMsgRef, 'parseRepositoryId')
        .mockReturnValueOnce({ repository: 'https://github.com/user/repo1', branch: 'main' })
        .mockReturnValueOnce({ repository: 'https://github.com/user/repo2', branch: 'main' })
        .mockReturnValueOnce({ repository: 'https://github.com/user/repo1', branch: 'main' })
        .mockReturnValueOnce({ repository: 'https://github.com/user/repo2', branch: 'main' });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      const result = await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      expect(result.success).toBe(true);
      expect(storage.cache.clear).toHaveBeenCalled();
    });
  });
  describe('prefetchAdjacentWeeks()', () => {
    const currentWeekStart = new Date('2024-01-15T00:00:00Z');
    const workdir = '/test/workdir';
    const storageBase = '/test/storage';
    it('should prefetch previous week when not in cache', async () => {
      const isCoveredSpy = vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(false)
        .mockReturnValueOnce(true);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
      expect(isCoveredSpy).toHaveBeenCalledWith(
        expect.objectContaining({ getTime: expect.any(Function) as () => number })
      );
      const firstCall = isCoveredSpy.mock.calls[0][0];
      expect(firstCall.toISOString()).toBe('2024-01-08T00:00:00.000Z');
    });
    it('should prefetch next week when not in cache', async () => {
      const isCoveredSpy = vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(true)
        .mockReturnValueOnce(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
      expect(isCoveredSpy).toHaveBeenCalledWith(
        expect.objectContaining({ getTime: expect.any(Function) as () => number })
      );
      const secondCall = isCoveredSpy.mock.calls[1][0];
      expect(secondCall.toISOString()).toBe('2024-01-22T00:00:00.000Z');
    });
    it('should skip prefetch when both weeks already in cache', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(true);
      const loadSpy = vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
      expect(loadSpy).not.toHaveBeenCalled();
    });
    it('should prefetch both weeks when neither in cache', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should handle prefetch failure for previous week (non-blocking)', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(false)
        .mockReturnValueOnce(true);
      vi.spyOn(cache, 'loadAdditionalPosts').mockRejectedValue(new Error('Load failed'));
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should handle prefetch failure for next week (non-blocking)', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(true)
        .mockReturnValueOnce(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockRejectedValue(new Error('Load failed'));
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should handle prefetch success', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should handle caught exception (non-blocking)', async () => {
      vi.spyOn(cache, 'isCacheRangeCovered').mockImplementation(() => {
        throw new Error('Unexpected error');
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
  });
  describe('Code coverage for log statements', () => {
    const weekStart = new Date('2024-01-15T00:00:00Z');
    const weekEnd = new Date('2024-01-21T23:59:59Z');
    const workdir = '/test/workdir';
    const storageBase = '/test/storage';
    it('should execute log statements in fetchRepositoriesForWeek with logging enabled', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: undefined
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(cache, 'loadRepositoryPosts').mockResolvedValue();
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(repository, 'fetchUpdates').mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue({
        repository: 'https://github.com/user/repo1',
        branch: 'main'
      });
      vi.spyOn(storage.cache, 'clear').mockReturnValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
    });
    it('should execute log statements for all repositories covered case with logging enabled', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const mockPosts = [createMockPost()];
      const mockRepos = [
        createMockRepository({
          id: 'https://github.com/user/repo1#main',
          fetchedRanges: [{ start: '2024-01-10', end: '2024-01-25' }]
        })
      ];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories')
        .mockResolvedValueOnce({ success: true, data: mockRepos })
        .mockResolvedValue({ success: true, data: [] });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
    });
    it('should execute log statements in prefetchAdjacentWeeks with logging enabled', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const currentWeekStart = new Date('2024-01-15T00:00:00Z');
      vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(false)
        .mockReturnValueOnce(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should execute log statements for fetch failure with logging enabled', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const currentWeekStart = new Date('2024-01-15T00:00:00Z');
      vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(false)
        .mockReturnValueOnce(true);
      vi.spyOn(cache, 'loadAdditionalPosts').mockRejectedValue(new Error('Load failed'));
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
    it('should execute error log in fetchRepositoriesForWeek catch block', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const mockPosts = [createMockPost()];
      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockRejectedValue(new Error('Repo error'));
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
    });
    it('should execute log statement in catch block when prefetch throws with logging enabled', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const mockPosts = [createMockPost()];
      let callCount = 0;
      vi.spyOn(cache, 'isCacheRangeCovered').mockImplementation(() => {
        callCount++;
        if (callCount > 1) {
          throw new Error('Cache error in background prefetch');
        }
        return false;
      });
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: []
      });
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });
      await timeline.getWeekPosts(workdir, storageBase, weekStart, weekEnd);
      await new Promise(resolve => setTimeout(resolve, 100));
    });
    it('should execute log statement when prefetch fails in else branch', async () => {
      process.env['GITSOCIAL_LOG_LEVEL'] = 'debug';
      const currentWeekStart = new Date('2024-01-15T00:00:00Z');
      vi.spyOn(cache, 'isCacheRangeCovered')
        .mockReturnValueOnce(false)
        .mockReturnValueOnce(true);
      vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(cache, 'getCachedRanges').mockReturnValue([]);
      vi.spyOn(repository, 'getRepositories').mockRejectedValue(new Error('Fetch failed'));
      await timeline.prefetchAdjacentWeeks(workdir, storageBase, currentWeekStart);
    });
  });
});
