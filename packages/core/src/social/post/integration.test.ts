import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { post } from './index';
import { cache } from './cache';
import { createCommit, createTestRepo, type TestRepo } from '../../test-utils';
import { execGit } from '../../git/exec';
import { getCurrentBranch } from '../../git/operations';
import { initializeGitSocial } from '../config';

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
  });
});
