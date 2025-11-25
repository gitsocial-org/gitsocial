import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { search } from '../../src/social/search';
import { post } from '../../src/social/post';
import type { Post } from '../../src/social/types';

describe('search', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
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
      branch: 'gitsocial',
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

  describe('searchPosts()', () => {
    it('should return search results successfully', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello world' }),
        createMockPost({ id: '2', content: 'test message' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
      expect(result.data?.total).toBe(1);
      expect(result.data?.query).toBe('hello');
    });

    it('should return empty results when no posts match', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'nonexistent' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(0);
      expect(result.data?.total).toBe(0);
      expect(result.data?.hasMore).toBe(false);
    });

    it('should handle empty query string', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'post1' }),
        createMockPost({ id: '2', content: 'post2' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });

    it('should return error when getPosts fails', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: false,
        error: { code: 'GET_POSTS_ERROR', message: 'Failed to get posts' }
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_POSTS_ERROR');
    });

    it('should return error when getPosts returns no data', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        // @ts-expect-error - Testing edge case where data is unexpectedly undefined
        data: undefined
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('POSTS_ERROR');
    });

    it('should handle exceptions gracefully', async () => {
      vi.spyOn(post, 'getPosts').mockRejectedValue(new Error('Unexpected error'));

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('SEARCH_ERROR');
      expect(result.error?.message).toBe('Unexpected error');
    });

    it('should handle non-Error exceptions', async () => {
      vi.spyOn(post, 'getPosts').mockRejectedValue('string error');

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('SEARCH_ERROR');
      expect(result.error?.message).toBe('Search failed');
    });

    it('should apply limit parameter correctly', async () => {
      const mockPosts = Array.from({ length: 20 }, (_, i) =>
        createMockPost({ id: `post-${i}`, content: `test ${i}` })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test', limit: 5 });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(5);
      expect(result.data?.total).toBe(20);
      expect(result.data?.hasMore).toBe(true);
    });

    it('should apply offset parameter correctly', async () => {
      const mockPosts = Array.from({ length: 20 }, (_, i) =>
        createMockPost({ id: `post-${i}`, content: `test ${i}` })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test', offset: 10, limit: 5 });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(5);
      expect(result.data?.total).toBe(20);
    });

    it('should set hasMore to false when no more results', async () => {
      const mockPosts = Array.from({ length: 5 }, (_, i) =>
        createMockPost({ id: `post-${i}`, content: `test ${i}` })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test', limit: 10 });

      expect(result.success).toBe(true);
      expect(result.data?.hasMore).toBe(false);
    });

    it('should use default limit of 10000', async () => {
      const mockPosts = Array.from({ length: 5 }, (_, i) =>
        createMockPost({ id: `post-${i}`, content: 'test' })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(5);
    });

    it('should calculate execution time', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(true);
      expect(result.data?.executionTime).toBeGreaterThanOrEqual(0);
    });

    it('should merge explicit filters with parsed filters', async () => {
      const mockPosts = [
        createMockPost({ author: { name: 'Alice', email: 'alice@example.com' }, content: 'hello' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: 'author:bob@example.com',
        filters: { author: 'alice@example.com' }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should use parsed filters when no explicit filters provided', async () => {
      const mockPosts = [
        createMockPost({ author: { name: 'Alice', email: 'alice@example.com' }, content: 'hello' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'author:alice@example.com' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should filter by interactionType from parsed query', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'comment', content: 'test' }),
        createMockPost({ id: '2', type: 'post', content: 'test' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:comment' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('comment');
    });

    it('should handle offset beyond results length', async () => {
      const mockPosts = [createMockPost({ content: 'test' })];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test', offset: 100 });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(0);
      expect(result.data?.hasMore).toBe(false);
    });
  });

  describe('filterAndSearchPosts()', () => {
    it('should skip GitSocial metadata posts', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'GitSocial: metadata post' }),
        createMockPost({ id: '2', content: 'regular post' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should filter by author email', async () => {
      const mockPosts = [
        createMockPost({ id: '1', author: { name: 'Alice', email: 'alice@example.com' } }),
        createMockPost({ id: '2', author: { name: 'Bob', email: 'bob@example.com' } })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { author: 'alice@example.com' }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should filter by repository case-insensitive partial match', async () => {
      const mockPosts = [
        createMockPost({ id: '1', repository: 'https://github.com/user/repo1' }),
        createMockPost({ id: '2', repository: 'https://github.com/user/repo2' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { repository: 'REPO1' }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should filter by startDate', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { startDate: new Date('2024-01-15') }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should filter by endDate', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { endDate: new Date('2024-01-15') }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should filter by single interactionType', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { interactionType: ['comment'] }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('comment');
    });

    it('should filter by multiple interactionTypes', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' }),
        createMockPost({ id: '3', type: 'repost' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { interactionType: ['comment', 'repost'] }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });

    it('should not filter when interactionType is empty array', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { interactionType: [] }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });

    it('should filter by branch', async () => {
      const mockPosts = [
        createMockPost({ id: '1', branch: 'gitsocial' }),
        createMockPost({ id: '2', branch: 'main' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: '',
        filters: { branch: 'main' }
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should match search terms in content', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello world' }),
        createMockPost({ id: '2', content: 'goodbye moon' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should match search terms in cleanContent', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'raw', cleanContent: 'hello world' }),
        createMockPost({ id: '2', content: 'raw', cleanContent: 'goodbye' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should match search terms in author name', async () => {
      const mockPosts = [
        createMockPost({ id: '1', author: { name: 'Alice Smith', email: 'a@b.com' }, content: 'post' }),
        createMockPost({ id: '2', author: { name: 'Bob Jones', email: 'c@d.com' }, content: 'post' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'alice' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should match search terms in author email', async () => {
      const mockPosts = [
        createMockPost({ id: '1', author: { name: 'User', email: 'alice@example.com' }, content: 'post' }),
        createMockPost({ id: '2', author: { name: 'User', email: 'bob@example.com' }, content: 'post' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'alice@example' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should require ALL search terms to match (AND logic)', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello world' }),
        createMockPost({ id: '2', content: 'hello moon' }),
        createMockPost({ id: '3', content: 'goodbye world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello world' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should handle posts with missing cleanContent', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello', cleanContent: undefined })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should return all posts when no search terms provided', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello' }),
        createMockPost({ id: '2', content: 'world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });

    it('should handle empty posts array', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(0);
      expect(result.data?.total).toBe(0);
    });
  });

  describe('calculateRelevanceScore()', () => {
    it('should score content matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'this contains keyword' }),
        createMockPost({ id: '2', content: 'no match' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(0);
    });

    it('should give higher score to first line matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword in first line\nrest of content' }),
        createMockPost({ id: '2', content: 'first line\nkeyword in second line' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.id).toBe('1');
      expect(result.data?.results[0]?.score).toBeGreaterThan(result.data?.results[1]?.score || 0);
    });

    it('should score cleanContent matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'raw', cleanContent: 'keyword here' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(0);
    });

    it('should score author name matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', author: { name: 'alice', email: 'a@b.com' }, content: 'post' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'alice' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(0);
    });

    it('should score author email matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', author: { name: 'User', email: 'test@example.com' }, content: 'post' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test@example' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(0);
    });

    it('should boost recent posts (less than 7 days)', async () => {
      const now = new Date();
      const fiveDaysAgo = new Date(now.getTime() - 5 * 24 * 60 * 60 * 1000);

      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword', timestamp: fiveDaysAgo })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(5);
    });

    it('should boost posts less than 30 days old', async () => {
      const now = new Date();
      const twentyDaysAgo = new Date(now.getTime() - 20 * 24 * 60 * 60 * 1000);

      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword', timestamp: twentyDaysAgo })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(5);
    });

    it('should not boost old posts (more than 30 days)', async () => {
      const now = new Date();
      const sixtyDaysAgo = new Date(now.getTime() - 60 * 24 * 60 * 60 * 1000);

      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword\nmore content', cleanContent: 'keyword more stuff', timestamp: sixtyDaysAgo })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'content' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBe(5);
    });

    it('should accumulate scores for multiple term matches', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'hello world', cleanContent: 'hello world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello world' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.score).toBeGreaterThan(10);
    });

    it('should handle case-insensitive matching', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'HELLO WORLD' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello world' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });
  });

  describe('sortByRelevance()', () => {
    it('should sort by score descending', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword in post', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', content: 'keyword keyword', cleanContent: 'keyword keyword', timestamp: new Date('2024-01-10') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should sort by date when scores are equal', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'keyword', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', content: 'keyword', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'keyword' });

      expect(result.success).toBe(true);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should handle missing scores', async () => {
      const mockPosts = [
        createMockPost({ id: '1', content: 'post1' }),
        createMockPost({ id: '2', content: 'post2' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });
  });

  describe('parseSearchQuery()', () => {
    it('should parse plain text search terms', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello world' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should parse quoted strings', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello world everyone' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '"hello world"' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should parse author filter', async () => {
      const mockPosts = [
        createMockPost({ author: { name: 'Alice', email: 'alice@example.com' } })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'author:alice@example.com' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should parse repo filter', async () => {
      const mockPosts = [
        createMockPost({ repository: 'https://github.com/user/myrepo' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'repo:myrepo' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should parse repository filter', async () => {
      const mockPosts = [
        createMockPost({ repository: 'https://github.com/user/myrepo' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'repository:myrepo' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should parse type filter for posts', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:post' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('post');
    });

    it('should parse type filter for comments', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:comment' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('comment');
    });

    it('should parse type filter for reposts', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'repost' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:repost' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('repost');
    });

    it('should parse type filter for quotes', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'quote' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:quote' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.type).toBe('quote');
    });

    it('should ignore invalid type filter values', async () => {
      const mockPosts = [
        createMockPost({ id: '1', type: 'post' }),
        createMockPost({ id: '2', type: 'comment' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'type:invalid' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(2);
    });

    it('should parse after date filter', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'after:2024-01-15' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('2');
    });

    it('should parse before date filter', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-10') }),
        createMockPost({ id: '2', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'before:2024-01-15' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
      expect(result.data?.results[0]?.id).toBe('1');
    });

    it('should handle invalid date formats', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-10') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'after:invalid-date' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should treat unknown filters as search terms', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello unknown:value world' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'unknown:value' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should handle mixed filters and search terms', async () => {
      const mockPosts = [
        createMockPost({
          id: '1',
          author: { name: 'Alice', email: 'alice@example.com' },
          content: 'hello world',
          type: 'post'
        })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: 'hello author:alice@example.com type:post'
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should handle multiple quoted strings', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello world foo bar' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '"hello world" "foo bar"' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should handle mixed quoted and unquoted terms', async () => {
      const mockPosts = [
        createMockPost({ content: 'hello world test' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '"hello world" test' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });
  });

  describe('tokenize()', () => {
    it('should tokenize plain text', async () => {
      const mockPosts = [
        createMockPost({ content: 'one two three' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'one two three' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should preserve quoted strings', async () => {
      const mockPosts = [
        createMockPost({ content: 'this is a test phrase' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '"test phrase"' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should handle empty strings', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await search.searchPosts('/test', { query: '' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(0);
    });

    it('should handle whitespace', async () => {
      const mockPosts = [
        createMockPost({ content: 'test' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '   test   ' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });
  });

  describe('parseDate()', () => {
    it('should parse valid ISO date strings', async () => {
      const mockPosts = [
        createMockPost({ id: '1', timestamp: new Date('2024-01-20') })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'after:2024-01-15T00:00:00Z' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should handle invalid date strings', async () => {
      const mockPosts = [createMockPost()];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'after:not-a-date' });

      expect(result.success).toBe(true);
    });
  });

  describe('normalizeSearchTerm()', () => {
    it('should convert to lowercase', async () => {
      const mockPosts = [
        createMockPost({ content: 'HELLO' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'hello' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });

    it('should trim whitespace', async () => {
      const mockPosts = [
        createMockPost({ content: 'test' })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: '  test  ' });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });
  });

  describe('edge cases', () => {
    it('should handle very large result sets', async () => {
      const mockPosts = Array.from({ length: 1000 }, (_, i) =>
        createMockPost({ id: `post-${i}`, content: 'test' })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', { query: 'test' });

      expect(result.success).toBe(true);
      expect(result.data?.total).toBe(1000);
    });

    it('should handle complex query with all filter types', async () => {
      const mockPosts = [
        createMockPost({
          id: '1',
          author: { name: 'Alice', email: 'alice@example.com' },
          repository: 'https://github.com/user/myrepo',
          type: 'post',
          timestamp: new Date('2024-01-15'),
          content: 'hello world',
          branch: 'gitsocial'
        })
      ];

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: mockPosts
      });

      const result = await search.searchPosts('/test', {
        query: 'hello author:alice@example.com repo:myrepo type:post after:2024-01-10 before:2024-01-20'
      });

      expect(result.success).toBe(true);
      expect(result.data?.results).toHaveLength(1);
    });
  });
});
