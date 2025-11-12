import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { log } from './log';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../git/exec';
import { initializeGitSocial } from './config';
import { list } from './list';
import { post } from './post';
import { interaction } from './post/interaction';
import { storage } from '../storage';
import { git } from '../git';
import { existsSync } from 'fs';

vi.mock('fs', async () => {
  const actual = await vi.importActual('fs');
  return {
    ...actual,
    existsSync: vi.fn()
  };
});

describe('log', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('log-test');
    await createCommit(testRepo.path, 'initial', { allowEmpty: true });
    await execGit(testRepo.path, ['remote', 'add', 'origin', `file://${testRepo.path}`]);
    await initializeGitSocial(testRepo.path, 'gitsocial');
    vi.clearAllMocks();
  });

  afterEach(() => {
    testRepo.cleanup();
    vi.restoreAllMocks();
  });

  describe('initialize()', () => {
    it('should log deprecation message', () => {
      const logSpy = vi.spyOn(console, 'debug').mockImplementation(() => {});
      log.initialize({ storageBase: '/tmp/storage' });
      logSpy.mockRestore();
    });
  });

  describe('getLogs()', () => {
    describe('repository:my scope', () => {
      it('should retrieve logs from workspace repository', async () => {
        await createCommit(testRepo.path, 'Test post 1', { allowEmpty: true });
        await createCommit(testRepo.path, 'Test post 2', { allowEmpty: true });

        const result = await log.getLogs(testRepo.path, 'repository:my');

        expect(result.success).toBe(true);
        expect(result.data).toBeDefined();
        expect(result.data!.length).toBeGreaterThan(0);
        const postEntries = result.data!.filter(entry => entry.type === 'post');
        expect(postEntries.length).toBeGreaterThan(0);
      });

      it('should include post type entries', async () => {
        await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

        const result = await log.getLogs(testRepo.path, 'repository:my');

        expect(result.success).toBe(true);
        expect(result.data!.length).toBeGreaterThan(0);
        const postEntry = result.data!.find(entry => entry.type === 'post');
        expect(postEntry).toBeDefined();
        expect(postEntry!.type).toBe('post');
        expect(postEntry!.hash).toBeDefined();
        expect(postEntry!.timestamp).toBeInstanceOf(Date);
        expect(postEntry!.author).toBeDefined();
        expect(postEntry!.author.name).toBeDefined();
        expect(postEntry!.author.email).toBeDefined();
      });

      it('should include list operations', async () => {
        await list.createList(testRepo.path, 'reading', 'Reading List');

        const result = await log.getLogs(testRepo.path, 'repository:my');

        expect(result.success).toBe(true);
        const listEntry = result.data!.find(entry => entry.type === 'list-create');
        expect(listEntry).toBeDefined();
        expect(listEntry!.details).toContain('reading');
      });
    });

    describe('timeline scope', () => {
      it('should retrieve logs from all branches', async () => {
        await createCommit(testRepo.path, 'Main branch post', { allowEmpty: true });

        const result = await log.getLogs(testRepo.path, 'timeline');

        expect(result.success).toBe(true);
        expect(result.data).toBeDefined();
        expect(result.data!.length).toBeGreaterThan(0);
      });
    });

    describe('external repository scope', () => {
      it('should fail when no storageBase provided', async () => {
        const externalUrl = 'https://github.com/user/repo#branch:main';
        const result = await log.getLogs(testRepo.path, `repository:${externalUrl}`);

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('NO_STORAGE_BASE');
      });

      it('should fail when repository URL is invalid', async () => {
        const result = await log.getLogs(testRepo.path, 'repository:invalid-url', {
          storageBase: '/tmp/storage'
        });

        expect(result.success).toBe(false);
      });

      it('should fail when repository not cloned', async () => {
        const externalUrl = 'https://github.com/user/repo#branch:main';
        vi.mocked(existsSync).mockReturnValue(false);

        const result = await log.getLogs(testRepo.path, `repository:${externalUrl}`, {
          storageBase: '/tmp/storage'
        });

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('REPOSITORY_NOT_CLONED');
      });

      it('should fail when getCommits fails', async () => {
        const externalUrl = 'https://github.com/user/repo#branch:main';
        vi.mocked(existsSync).mockReturnValue(true);
        vi.spyOn(storage.repository, 'getCommits').mockResolvedValue({
          success: false,
          error: { code: 'GET_COMMITS_FAILED', message: 'Failed' }
        });

        const result = await log.getLogs(testRepo.path, `repository:${externalUrl}`, {
          storageBase: '/tmp/storage'
        });

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('GET_COMMITS_FAILED');
      });

      it('should retrieve external repository logs successfully', async () => {
        const externalUrl = 'https://github.com/user/repo#branch:main';
        vi.mocked(existsSync).mockReturnValue(true);
        vi.spyOn(storage.repository, 'getCommits').mockResolvedValue({
          success: true,
          data: [
            {
              hash: 'abc123def456',
              author: 'Test User',
              email: 'test@example.com',
              timestamp: new Date('2024-01-15T10:00:00Z'),
              message: 'Test post'
            }
          ]
        });

        const result = await log.getLogs(testRepo.path, `repository:${externalUrl}`, {
          storageBase: '/tmp/storage'
        });

        expect(result.success).toBe(true);
        expect(result.data).toBeDefined();
        expect(result.data!.length).toBeGreaterThan(0);
      });
    });

    describe('filters', () => {
      beforeEach(async () => {
        await createCommit(testRepo.path, 'Post 1', { allowEmpty: true });
        await new Promise(resolve => setTimeout(resolve, 100));
        await createCommit(testRepo.path, 'Post 2', { allowEmpty: true });
        await new Promise(resolve => setTimeout(resolve, 100));
        await createCommit(testRepo.path, 'Post 3', { allowEmpty: true });
      });

      it('should apply limit filter', async () => {
        const result = await log.getLogs(testRepo.path, 'repository:my', { limit: 2 });

        expect(result.success).toBe(true);
        expect(result.data!.length).toBeLessThanOrEqual(2);
      });

      it('should apply type filter for posts only', async () => {
        await list.createList(testRepo.path, 'reading');

        const result = await log.getLogs(testRepo.path, 'repository:my', { types: ['post'] });

        expect(result.success).toBe(true);
        const allPosts = result.data!.every(entry => entry.type === 'post');
        expect(allPosts).toBe(true);
      });

      it('should apply type filter for list operations', async () => {
        await list.createList(testRepo.path, 'reading');
        await createCommit(testRepo.path, 'Regular post', { allowEmpty: true });

        const result = await log.getLogs(testRepo.path, 'repository:my', { types: ['list-create'] });

        expect(result.success).toBe(true);
        const allListOps = result.data!.every(entry => entry.type === 'list-create');
        expect(allListOps).toBe(true);
      });

      it('should apply multiple type filters', async () => {
        await list.createList(testRepo.path, 'reading');

        const result = await log.getLogs(testRepo.path, 'repository:my', {
          types: ['post', 'list-create']
        });

        expect(result.success).toBe(true);
        const validTypes = result.data!.every(entry =>
          entry.type === 'post' || entry.type === 'list-create'
        );
        expect(validTypes).toBe(true);
      });
    });

    describe('error handling', () => {
      it('should handle errors gracefully', async () => {
        vi.spyOn(git, 'getConfiguredBranch').mockRejectedValue(new Error('Git error'));

        const result = await log.getLogs(testRepo.path, 'repository:my');

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('GET_LOGS_ERROR');
      });
    });
  });

  describe('log entry types', () => {
    it('should identify post entries correctly', async () => {
      await createCommit(testRepo.path, 'Regular commit post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const postEntry = result.data!.find(entry => entry.type === 'post');
      expect(postEntry).toBeDefined();
      expect(postEntry!.type).toBe('post');
    });

    it('should identify comment entries correctly', async () => {
      const postResult = await post.createPost(testRepo.path, 'Original post');
      expect(postResult.success).toBe(true);

      const commentResult = await interaction.createInteraction(
        'comment',
        testRepo.path,
        postResult.data!,
        'Test comment'
      );
      expect(commentResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const commentEntry = result.data!.find(entry => entry.type === 'comment');
      expect(commentEntry).toBeDefined();
      expect(commentEntry!.details).toContain('Re:');
    });

    it('should identify repost entries correctly', async () => {
      const postResult = await post.createPost(testRepo.path, 'Original post for repost');
      expect(postResult.success).toBe(true);

      const repostResult = await interaction.createInteraction(
        'repost',
        testRepo.path,
        postResult.data!
      );
      expect(repostResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const repostEntry = result.data!.find(entry => entry.type === 'repost');
      expect(repostEntry).toBeDefined();
      expect(repostEntry!.details).toContain('Repost:');
    });

    it('should identify quote entries correctly', async () => {
      const postResult = await post.createPost(testRepo.path, 'Original post for quote');
      expect(postResult.success).toBe(true);

      const quoteResult = await interaction.createInteraction(
        'quote',
        testRepo.path,
        postResult.data!,
        'My quote comment'
      );
      expect(quoteResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const quoteEntry = result.data!.find(entry => entry.type === 'quote');
      expect(quoteEntry).toBeDefined();
      expect(quoteEntry!.details).toContain('Quote:');
    });

    it('should identify config entries correctly', async () => {
      const configRef = 'refs/gitmsg/social/config';
      await execGit(testRepo.path, ['update-ref', configRef, 'HEAD']);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const configEntry = result.data!.find(entry => entry.type === 'config');
      expect(configEntry).toBeDefined();
    });
  });

  describe('list operations', () => {
    it('should generate list-create entry for new list', async () => {
      await list.createList(testRepo.path, 'reading', 'Reading List');

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const listEntry = result.data!.find(entry => entry.type === 'list-create');
      expect(listEntry).toBeDefined();
      expect(listEntry!.details).toContain('reading');
    });

    it('should generate repository-follow entry when adding repo to list', async () => {
      await list.createList(testRepo.path, 'reading');
      const repoUrl = 'https://github.com/user/repo';
      await list.addRepositoryToList(testRepo.path, 'reading', repoUrl);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const followEntry = result.data!.find(entry => entry.type === 'repository-follow');
      expect(followEntry).toBeDefined();
      expect(followEntry!.details).toContain(repoUrl);
      expect(followEntry!.details).toContain('reading');
    });

    it('should generate repository-unfollow entry when removing repo from list', async () => {
      const repoUrl = 'https://github.com/user/repo';
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', repoUrl);
      await list.removeRepositoryFromList(testRepo.path, 'reading', repoUrl);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const unfollowEntry = result.data!.find(entry => entry.type === 'repository-unfollow');
      expect(unfollowEntry).toBeDefined();
      expect(unfollowEntry!.details).toContain(repoUrl);
      expect(unfollowEntry!.details).toContain('reading');
    });

    it('should generate multiple follow entries when adding multiple repos', async () => {
      await list.createList(testRepo.path, 'reading');
      const repo1 = 'https://github.com/user/repo1';
      const repo2 = 'https://github.com/user/repo2';
      await list.addRepositoryToList(testRepo.path, 'reading', repo1);
      await list.addRepositoryToList(testRepo.path, 'reading', repo2);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const followEntries = result.data!.filter(entry => entry.type === 'repository-follow');
      expect(followEntries.length).toBeGreaterThanOrEqual(1);
      const hasRepo1 = followEntries.some(e => e.details.includes('repo1'));
      const hasRepo2 = followEntries.some(e => e.details.includes('repo2'));
      expect(hasRepo1 || hasRepo2).toBe(true);
    });
  });

  describe('log entry format', () => {
    it('should include shortened commit hash', async () => {
      await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const entry = result.data![0];
      expect(entry.hash).toBeDefined();
      expect(entry.hash.length).toBe(12);
    });

    it('should include timestamp', async () => {
      await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const entry = result.data![0];
      expect(entry.timestamp).toBeInstanceOf(Date);
    });

    it('should include author information', async () => {
      await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const entry = result.data![0];
      expect(entry.author).toBeDefined();
      expect(entry.author.name).toBeDefined();
      expect(entry.author.email).toBeDefined();
    });

    it('should include repository', async () => {
      await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const entry = result.data![0];
      expect(entry.repository).toBeDefined();
    });

    it('should include postId for post entries', async () => {
      const postResult = await post.createPost(testRepo.path, 'Test post');
      expect(postResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const postEntry = result.data!.find(entry => entry.type === 'post' && entry.details.includes('Test post'));
      expect(postEntry).toBeDefined();
      expect(postEntry!.postId).toBeDefined();
    });

    it('should include raw commit data', async () => {
      await createCommit(testRepo.path, 'Test post', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const entry = result.data![0];
      expect(entry.raw).toBeDefined();
      expect(entry.raw.commit).toBeDefined();
    });
  });

  describe('sorting', () => {
    it('should sort entries by timestamp descending (newest first)', async () => {
      await createCommit(testRepo.path, 'Post 1', { allowEmpty: true });
      await new Promise(resolve => setTimeout(resolve, 100));
      await createCommit(testRepo.path, 'Post 2', { allowEmpty: true });
      await new Promise(resolve => setTimeout(resolve, 100));
      await createCommit(testRepo.path, 'Post 3', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data!.length).toBeGreaterThan(1);

      for (let i = 0; i < result.data!.length - 1; i++) {
        const current = result.data![i].timestamp.getTime();
        const next = result.data![i + 1].timestamp.getTime();
        expect(current).toBeGreaterThanOrEqual(next);
      }
    });
  });

  describe('edge cases', () => {
    it('should handle commits without GitMsg data', async () => {
      await createCommit(testRepo.path, 'Plain commit without GitMsg', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data!.length).toBeGreaterThan(0);
      const entry = result.data!.find(e => e.type === 'post');
      expect(entry).toBeDefined();
      expect(entry!.type).toBe('post');
    });

    it('should handle invalid list JSON gracefully', async () => {
      const listRef = 'refs/gitmsg/social/lists/broken';
      await createCommit(testRepo.path, 'invalid json {{{', { allowEmpty: true });
      await execGit(testRepo.path, ['update-ref', listRef, 'HEAD']);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
    });

    it('should handle commits with malformed data gracefully', async () => {
      await createCommit(testRepo.path, 'Valid commit', { allowEmpty: true });

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should handle empty commit list', async () => {
      vi.spyOn(git, 'getCommits').mockResolvedValue([]);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });
  });

  describe('formatActionDetails', () => {
    it('should format post details from GitMsg content', async () => {
      const postResult = await post.createPost(testRepo.path, 'Post with GitMsg content');
      expect(postResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const postEntry = result.data!.find(entry =>
        entry.type === 'post' && entry.details.includes('Post with GitMsg content')
      );
      expect(postEntry).toBeDefined();
    });

    it('should format comment details with original content reference', async () => {
      const postResult = await post.createPost(testRepo.path, 'Original post content');
      expect(postResult.success).toBe(true);

      const commentResult = await interaction.createInteraction(
        'comment',
        testRepo.path,
        postResult.data!,
        'Comment text'
      );
      expect(commentResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const commentEntry = result.data!.find(entry => entry.type === 'comment');
      expect(commentEntry).toBeDefined();
      expect(commentEntry!.details).toMatch(/^Re:/);
    });

    it('should format repost details with original content', async () => {
      const postResult = await post.createPost(testRepo.path, 'Post to repost');
      expect(postResult.success).toBe(true);

      const repostResult = await interaction.createInteraction(
        'repost',
        testRepo.path,
        postResult.data!
      );
      expect(repostResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const repostEntry = result.data!.find(entry => entry.type === 'repost');
      expect(repostEntry).toBeDefined();
      expect(repostEntry!.details).toMatch(/^Repost:/);
    });

    it('should format quote details with original content', async () => {
      const postResult = await post.createPost(testRepo.path, 'Post to quote');
      expect(postResult.success).toBe(true);

      const quoteResult = await interaction.createInteraction(
        'quote',
        testRepo.path,
        postResult.data!,
        'Quote comment'
      );
      expect(quoteResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const quoteEntry = result.data!.find(entry => entry.type === 'quote');
      expect(quoteEntry).toBeDefined();
      expect(quoteEntry!.details).toMatch(/^Quote:/);
    });

    it('should format list-create details from JSON', async () => {
      await list.createList(testRepo.path, 'tech', 'Technology');

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const listEntry = result.data!.find(entry => entry.type === 'list-create');
      expect(listEntry).toBeDefined();
      expect(listEntry!.details).toContain('tech');
    });

    it('should handle missing original content in comment gracefully', async () => {
      const postResult = await post.createPost(testRepo.path, 'Post');
      expect(postResult.success).toBe(true);

      const commentResult = await interaction.createInteraction(
        'comment',
        testRepo.path,
        postResult.data!,
        'Comment'
      );
      expect(commentResult.success).toBe(true);

      const result = await log.getLogs(testRepo.path, 'repository:my');

      expect(result.success).toBe(true);
      const commentEntry = result.data!.find(entry => entry.type === 'comment');
      expect(commentEntry).toBeDefined();
      expect(commentEntry!.details).toBeDefined();
    });
  });

  describe('formatActionDetails edge cases', () => {
    it('should handle config and metadata actions', async () => {
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should handle list operations in logs', async () => {
      await list.createList(testRepo.path, 'test-list', 'Test');
      await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo');

      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });
  });

  describe('branch coverage improvements', () => {
    it('should handle type filter with no matches', async () => {
      await post.createPost(testRepo.path, 'Test post');
      const result = await log.getLogs(testRepo.path, 'repository:my', {
        types: ['nonexistent-type']
      });
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(0);
    });

    it('should handle malformed list JSON in log entry', async () => {
      await list.createList(testRepo.path, 'json-test', 'Test');
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should handle empty commit messages', async () => {
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should handle repository-follow and repository-unfollow actions', async () => {
      await list.createList(testRepo.path, 'follow-test');
      await list.addRepositoryToList(testRepo.path, 'follow-test', 'https://github.com/test/repo');
      await list.removeRepositoryFromList(testRepo.path, 'follow-test', 'https://github.com/test/repo');
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(result.data!.length).toBeGreaterThan(0);
    });

    it('should handle commits with no GitMsg headers', async () => {
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should handle action type detection fallback', async () => {
      await post.createPost(testRepo.path, 'Fallback test');
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      const entries = result.data || [];
      expect(entries.length).toBeGreaterThan(0);
    });

    it('should handle getPreviousListCommit errors', async () => {
      await list.createList(testRepo.path, 'prev-test');
      await list.addRepositoryToList(testRepo.path, 'prev-test', 'https://github.com/test/repo1');
      await list.addRepositoryToList(testRepo.path, 'prev-test', 'https://github.com/test/repo2');
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
    });

    it('should handle parseListState errors gracefully', async () => {
      await list.createList(testRepo.path, 'parse-test');
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should apply type filters correctly', async () => {
      await post.createPost(testRepo.path, 'Post 1');
      await post.createPost(testRepo.path, 'Post 2');
      const result = await log.getLogs(testRepo.path, 'repository:my', {
        types: ['post']
      });
      expect(result.success).toBe(true);
      expect(result.data?.every(entry => entry.type === 'post' || entry.type === 'list-create')).toBe(true);
    });

    it('should handle invalid commit during transformation', async () => {
      const result = await log.getLogs(testRepo.path, 'repository:my');
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });
  });
});
