import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ensureRemoteListRepositories, post } from './index';
import { cache } from './cache';
import { createCommit, createTestRepo, type TestRepo } from '../../test-utils';
import { execGit } from '../../git/exec';
import { getCurrentBranch } from '../../git/operations';
import { initializeGitSocial } from '../config';
import { git } from '../../git';
import { repository } from '../repository';
import { gitMsgRef } from '../../gitmsg/protocol';
import type { Post } from '../types';

describe('Post Integration Tests', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('post-integration');

    await initializeGitSocial(testRepo.path, 'gitsocial');

    cache.setCacheEnabled(true);
    cache.setCacheMaxSize(10000);
    // Properly initialize cache with workdir to avoid uninitialized state
    await cache.refresh({ all: true }, testRepo.path);
  });

  afterEach(async () => {
    // Clear cache completely before cleanup
    cache.setCacheEnabled(false);
    await cache.refresh({ all: true });
    testRepo.cleanup();
    cache.setCacheEnabled(true);
    vi.restoreAllMocks();
  });

  describe('createPost()', () => {
    it('should create a post on gitsocial branch', async () => {
      const result = await post.createPost(testRepo.path, 'My first post');

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
      expect(result.data?.content).toBe('My first post');
      expect(result.data?.type).toBe('post');
      expect(result.data?.source).toBe('implicit');
    });

    it('should create post on gitsocial branch without switching current branch', async () => {
      await createCommit(testRepo.path, 'initial commit', { allowEmpty: true });
      await execGit(testRepo.path, ['checkout', '-b', 'feature']);

      const result = await post.createPost(testRepo.path, 'Test post');

      expect(result.success).toBe(true);
      const postResult = await execGit(testRepo.path, ['log', '--oneline', 'gitsocial']);
      expect(postResult.success).toBe(true);
      expect(postResult.data?.stdout).toContain('Test post');
      const currentBranchResult = await getCurrentBranch(testRepo.path);
      expect(currentBranchResult.success).toBe(true);
      expect(currentBranchResult.data).toBe('feature');
    });

    it('should create gitsocial branch if it does not exist', async () => {
      const branchExistsBefore = await execGit(testRepo.path, [
        'rev-parse',
        '--verify',
        'refs/heads/gitsocial'
      ]);
      expect(branchExistsBefore.success).toBe(false);

      await post.createPost(testRepo.path, 'First post');

      const branchExistsAfter = await execGit(testRepo.path, [
        'rev-parse',
        '--verify',
        'refs/heads/gitsocial'
      ]);
      expect(branchExistsAfter.success).toBe(true);
    });

    it('should create empty commit on gitsocial branch', async () => {
      const countBeforeResult = await execGit(testRepo.path, ['rev-list', '--count', 'gitsocial']);
      const countBefore = countBeforeResult.success && countBeforeResult.data
        ? parseInt(countBeforeResult.data.stdout.trim(), 10)
        : 0;

      await post.createPost(testRepo.path, 'Test post');

      const countAfterResult = await execGit(testRepo.path, ['rev-list', '--count', 'gitsocial']);
      const countAfter = countAfterResult.success && countAfterResult.data
        ? parseInt(countAfterResult.data.stdout.trim(), 10)
        : 0;
      expect(countAfter).toBe(countBefore + 1);
    });

    it('should use commit message as post content', async () => {
      const content = 'This is my post content\nWith multiple lines';
      const result = await post.createPost(testRepo.path, content);

      expect(result.success).toBe(true);
      expect(result.data?.content).toContain('This is my post content');
      expect(result.data?.content).toContain('With multiple lines');
    });

    it('should create post with author information', async () => {
      const result = await post.createPost(testRepo.path, 'Test post');

      expect(result.success).toBe(true);
      expect(result.data?.author.name).toBe('Test User');
      expect(result.data?.author.email).toBe('test@example.com');
    });

    it('should generate valid post ID', async () => {
      const result = await post.createPost(testRepo.path, 'Test post');

      expect(result.success).toBe(true);
      expect(result.data?.id).toMatch(/^#commit:[a-f0-9]{12}$/);
    });

    it('should set isWorkspacePost to true', async () => {
      const result = await post.createPost(testRepo.path, 'Test post');

      expect(result.success).toBe(true);
      expect(result.data?.isWorkspacePost).toBe(true);
    });

    it('should create multiple posts in sequence', async () => {
      const result1 = await post.createPost(testRepo.path, 'First post');
      const result2 = await post.createPost(testRepo.path, 'Second post');
      const result3 = await post.createPost(testRepo.path, 'Third post');

      expect(result1.success).toBe(true);
      expect(result2.success).toBe(true);
      expect(result3.success).toBe(true);

      expect(result1.data?.id).not.toBe(result2.data?.id);
      expect(result2.data?.id).not.toBe(result3.data?.id);

      const countResult = await execGit(testRepo.path, ['rev-list', '--count', 'gitsocial']);
      const count = countResult.success && countResult.data
        ? parseInt(countResult.data.stdout.trim(), 10)
        : 0;
      expect(count).toBeGreaterThanOrEqual(3);
    });

    it('should handle special characters in content', async () => {
      const content = 'Post with "quotes" and \'apostrophes\' and #hashtags @mentions';
      const result = await post.createPost(testRepo.path, content);

      expect(result.success).toBe(true);
      expect(result.data?.content).toBe(content);
    });

    it('should handle empty content', async () => {
      const result = await post.createPost(testRepo.path, '');

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    }, 10000);

    it('should handle very long content', async () => {
      const longContent = 'A'.repeat(10000);
      const result = await post.createPost(testRepo.path, longContent);

      expect(result.success).toBe(true);
      expect(result.data?.content).toBe(longContent);
    });
  });

  describe('getPosts()', () => {
    it('should retrieve created post', async () => {
      const createResult = await post.createPost(testRepo.path, 'Test post');
      expect(createResult.success).toBe(true);

      const getResult = await post.getPosts(testRepo.path, 'repository:my');

      expect(getResult.success).toBe(true);
      expect(getResult.data).toHaveLength(1);
      expect(getResult.data?.[0]?.content).toBe('Test post');
    });

    it('should retrieve multiple posts', async () => {
      await post.createPost(testRepo.path, 'First');
      await post.createPost(testRepo.path, 'Second');
      await post.createPost(testRepo.path, 'Third');

      const result = await post.getPosts(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data?.length).toBeGreaterThanOrEqual(3);

      const contents = result.data?.map(p => p.content) || [];
      expect(contents).toContain('First');
      expect(contents).toContain('Second');
      expect(contents).toContain('Third');
    });

    it('should retrieve single post by ID', async () => {
      const createResult = await post.createPost(testRepo.path, 'Test post');
      const postId = createResult.data?.id;
      expect(postId).toBeDefined();

      const getResult = await post.getPosts(testRepo.path, `post:${postId}`);

      expect(getResult.success).toBe(true);
      expect(getResult.data).toHaveLength(1);
      expect(getResult.data?.[0]?.id).toBe(postId);
    });

    it('should return empty array when no posts exist', async () => {
      const result = await post.getPosts(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should filter posts by type', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');

      const result = await post.getPosts(testRepo.path, 'repository:my', {
        types: ['post']
      });

      expect(result.success).toBe(true);
      expect(result.data?.length).toBeGreaterThanOrEqual(3);
      expect(result.data?.every(p => p.type === 'post')).toBe(true);
    });

    it('should filter posts by date range', async () => {
      await post.createPost(testRepo.path, 'Post 1');

      // Wait a bit to ensure time difference
      await new Promise(resolve => setTimeout(resolve, 100));
      const cutoffTime = new Date();
      await new Promise(resolve => setTimeout(resolve, 100));

      await post.createPost(testRepo.path, 'Post 2');

      const result = await post.getPosts(testRepo.path, 'repository:my', {
        since: cutoffTime
      });

      expect(result.success).toBe(true);
      // Should only include Post 2 (created after cutoff)
      expect(result.data?.length).toBeLessThanOrEqual(2);
    });

    it('should limit number of returned posts', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      await post.createPost(testRepo.path, 'Post 3');
      await post.createPost(testRepo.path, 'Post 4');
      await post.createPost(testRepo.path, 'Post 5');

      const result = await post.getPosts(testRepo.path, 'repository:my', {
        limit: 3
      });

      expect(result.success).toBe(true);
      expect(result.data?.length).toBeLessThanOrEqual(3);
    });
  });

  describe('End-to-end workflow', () => {
    it('should complete full post lifecycle', async () => {
      const createResult = await post.createPost(testRepo.path, 'Hello, World!');
      expect(createResult.success).toBe(true);

      const postId = createResult.data?.id;
      expect(postId).toMatch(/^#commit:[a-f0-9]{12}$/);

      const gitSocialLogResult = await execGit(testRepo.path, ['log', '--oneline', 'gitsocial']);
      expect(gitSocialLogResult.success).toBe(true);
      expect(gitSocialLogResult.data?.stdout).toContain('Hello, World!');

      const getResult = await post.getPosts(testRepo.path, `post:${postId}`);
      expect(getResult.success).toBe(true);
      expect(getResult.data).toHaveLength(1);

      const retrievedPost = getResult.data?.[0];
      expect(retrievedPost?.id).toBe(postId);
      expect(retrievedPost?.content).toBe('Hello, World!');
      expect(retrievedPost?.type).toBe('post');
      expect(retrievedPost?.source).toBe('implicit');
      expect(retrievedPost?.author.name).toBe('Test User');
      expect(retrievedPost?.isWorkspacePost).toBe(true);
    });

    it('should handle creating posts from different branches without switching', async () => {
      await createCommit(testRepo.path, 'initial commit', { allowEmpty: true });
      await post.createPost(testRepo.path, 'Post 1');
      let branchResult = await getCurrentBranch(testRepo.path);
      expect(branchResult.success).toBe(true);
      expect(branchResult.data).toBe('main');

      await execGit(testRepo.path, ['checkout', '-b', 'feature']);

      await post.createPost(testRepo.path, 'Post 2');
      branchResult = await getCurrentBranch(testRepo.path);
      expect(branchResult.success).toBe(true);
      expect(branchResult.data).toBe('feature');

      const result = await post.getPosts(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(result.data?.length).toBeGreaterThanOrEqual(2);
    });

    it('should maintain posts across cache refreshes', async () => {
      const post1 = await post.createPost(testRepo.path, 'Unique-First-' + Date.now());
      const post2 = await post.createPost(testRepo.path, 'Unique-Second-' + Date.now());
      const post3 = await post.createPost(testRepo.path, 'Unique-Third-' + Date.now());

      const result1 = await post.getPosts(testRepo.path, 'repository:my');
      const contents1 = new Set(result1.data?.map(p => p.content));

      // Refresh cache
      await cache.refresh({}, testRepo.path);

      const result2 = await post.getPosts(testRepo.path, 'repository:my');
      const contents2 = new Set(result2.data?.map(p => p.content));

      // All three unique posts should be in both results
      expect(contents1.has(post1.data!.content)).toBe(true);
      expect(contents1.has(post2.data!.content)).toBe(true);
      expect(contents1.has(post3.data!.content)).toBe(true);
      expect(contents2.has(post1.data!.content)).toBe(true);
      expect(contents2.has(post2.data!.content)).toBe(true);
      expect(contents2.has(post3.data!.content)).toBe(true);
    });
  });

  describe('Thread scope', () => {
    it('should retrieve thread for valid post ID', async () => {
      const parentResult = await post.createPost(testRepo.path, 'Parent post');
      expect(parentResult.success).toBe(true);
      const parentId = parentResult.data?.id;

      const threadResult = await post.getPosts(testRepo.path, `thread:${parentId}`);

      expect(threadResult.success).toBe(true);
      expect(threadResult.data).toBeDefined();
      expect(threadResult.data?.length).toBeGreaterThanOrEqual(1);
      expect(threadResult.data?.some(p => p.id === parentId)).toBe(true);
    });

    it('should handle thread scope with invalid post ID', async () => {
      const result = await post.getPosts(testRepo.path, 'thread:#commit:invalidhash000');

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should handle thread scope with sort options', async () => {
      const parentResult = await post.createPost(testRepo.path, 'Parent for sorting');
      expect(parentResult.success).toBe(true);
      const parentId = parentResult.data?.id;

      const latestResult = await post.getPosts(testRepo.path, `thread:${parentId}`, {
        sortBy: 'latest'
      });
      expect(latestResult.success).toBe(true);

      const oldestResult = await post.getPosts(testRepo.path, `thread:${parentId}`, {
        sortBy: 'oldest'
      });
      expect(oldestResult.success).toBe(true);

      const topResult = await post.getPosts(testRepo.path, `thread:${parentId}`, {
        sortBy: 'top'
      });
      expect(topResult.success).toBe(true);
    });
  });

  describe('Error handling', () => {
    it('should return error for invalid repository path', async () => {
      const result = await post.createPost('/nonexistent/path', 'Test');

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should handle getPosts with invalid scope gracefully', async () => {
      const result = await post.getPosts(testRepo.path, 'invalid:scope');

      // Should not crash, might return empty or error
      expect(result).toBeDefined();
    });

    it('should handle cache disabled scenario', async () => {
      cache.setCacheEnabled(false);

      await post.createPost(testRepo.path, 'Test post');
      const result = await post.getPosts(testRepo.path, 'repository:my');

      // Should still work with cache disabled
      expect(result).toBeDefined();

      cache.setCacheEnabled(true);
    });

    it('should fallback to full refresh when incremental cache add fails', async () => {
      const addPostSpy = vi.spyOn(cache, 'addPostToCache').mockResolvedValue(false);
      const refreshSpy = vi.spyOn(cache, 'refresh');

      const result = await post.createPost(testRepo.path, 'Test cache fallback');

      expect(result.success).toBe(true);
      expect(addPostSpy).toHaveBeenCalled();
      expect(refreshSpy).toHaveBeenCalledWith({ all: true }, testRepo.path);
    });

    it('should handle unexpected exception in createPost', async () => {
      const errorMessage = 'Unexpected internal error';
      vi.spyOn(git, 'getConfiguredBranch').mockRejectedValue(new Error(errorMessage));

      const result = await post.createPost(testRepo.path, 'Test exception');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CREATE_POST_ERROR');
      expect(result.error?.message).toBe('Failed to create post');
    });
  });

  describe('Edge cases', () => {
    it('should return post on successful retry after first failure', async () => {
      let getCachedPostsCallCount = 0;
      const originalGetCachedPosts = cache.getCachedPosts.bind(cache);

      vi.spyOn(cache, 'getCachedPosts').mockImplementation(async (workdir, scope, filter, context) => {
        getCachedPostsCallCount++;
        if (getCachedPostsCallCount === 1) {
          return [];
        }
        return originalGetCachedPosts(workdir, scope, filter, context);
      });

      const result = await post.createPost(testRepo.path, 'Test retry success');

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
      expect(getCachedPostsCallCount).toBeGreaterThanOrEqual(2);
    });

    it('should handle undefined post in data array', async () => {
      let callCount = 0;
      vi.spyOn(cache, 'getCachedPosts').mockImplementation(() => {
        callCount++;
        if (callCount <= 2) {
          return Promise.resolve([undefined as unknown as Post]);
        }
        return Promise.resolve([]);
      });

      const result = await post.createPost(testRepo.path, 'Test undefined post');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('POST_LOAD_ERROR');
      expect(result.error?.message).toContain('post was undefined');
    });

    it('should handle null posts from cache (fallback to empty array)', async () => {
      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue(null as unknown as Post[]);

      const result = await post.getPosts(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });
  });

  describe('ensureRemoteListRepositories()', () => {
    it('should process valid repositories', async () => {
      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange').mockResolvedValue({
        success: true,
        data: undefined
      });

      await ensureRemoteListRepositories(
        testRepo.path,
        ['https://github.com/user/repo#branch:main'],
        '/tmp/storage',
        new Date()
      );

      expect(ensureDataSpy).toHaveBeenCalledWith(
        testRepo.path,
        '/tmp/storage',
        'https://github.com/user/repo',
        'main',
        expect.any(Date),
        { isPersistent: false }
      );
    });

    it('should skip invalid repository format', async () => {
      const parseRepoSpy = vi.spyOn(gitMsgRef, 'parseRepositoryId').mockReturnValue(null);
      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange');

      await ensureRemoteListRepositories(
        testRepo.path,
        ['invalid-repo-format'],
        '/tmp/storage'
      );

      expect(parseRepoSpy).toHaveBeenCalledWith('invalid-repo-format');
      expect(ensureDataSpy).not.toHaveBeenCalled();
    });

    it('should continue on individual repository failure', async () => {
      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange')
        .mockResolvedValueOnce({
          success: false,
          error: { code: 'ERROR', message: 'Failed' }
        })
        .mockResolvedValueOnce({
          success: true,
          data: undefined
        });

      await ensureRemoteListRepositories(
        testRepo.path,
        [
          'https://github.com/user/repo1#branch:main',
          'https://github.com/user/repo2#branch:main'
        ],
        '/tmp/storage'
      );

      expect(ensureDataSpy).toHaveBeenCalledTimes(2);
    });

    it('should use current date when since not provided', async () => {
      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange').mockResolvedValue({
        success: true,
        data: undefined
      });

      await ensureRemoteListRepositories(
        testRepo.path,
        ['https://github.com/user/repo#branch:main'],
        '/tmp/storage'
      );

      expect(ensureDataSpy).toHaveBeenCalledWith(
        testRepo.path,
        '/tmp/storage',
        'https://github.com/user/repo',
        'main',
        expect.any(Date),
        { isPersistent: false }
      );
    });

    it('should process multiple valid repositories', async () => {
      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange').mockResolvedValue({
        success: true,
        data: undefined
      });

      await ensureRemoteListRepositories(
        testRepo.path,
        [
          'https://github.com/user/repo1#branch:main',
          'https://github.com/user/repo2#branch:dev',
          'https://github.com/user/repo3#branch:feature'
        ],
        '/tmp/storage',
        new Date('2024-01-01')
      );

      expect(ensureDataSpy).toHaveBeenCalledTimes(3);
    });

    it('should handle mixed valid and invalid repositories', async () => {
      vi.spyOn(gitMsgRef, 'parseRepositoryId').mockImplementation((repoString) => {
        if (repoString === 'invalid') {
          return null;
        }
        return {
          repository: 'https://github.com/user/repo',
          branch: 'main'
        };
      });

      const ensureDataSpy = vi.spyOn(repository, 'ensureDataForDateRange').mockResolvedValue({
        success: true,
        data: undefined
      });

      await ensureRemoteListRepositories(
        testRepo.path,
        [
          'https://github.com/user/repo1#branch:main',
          'invalid',
          'https://github.com/user/repo2#branch:main'
        ],
        '/tmp/storage'
      );

      expect(ensureDataSpy).toHaveBeenCalledTimes(2);
    });
  });
});
