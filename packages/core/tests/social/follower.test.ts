import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { follower } from '../../src/social/follower';
import { list } from '../../src/social/list';
import { repository } from '../../src/social/repository';
import { git } from '../../src/git';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../../src/git/exec';
import type { Repository } from '../../src/social/types';

describe('follower', () => {
  let myRepo: TestRepo;
  let otherRepo1: TestRepo;
  let otherRepo2: TestRepo;

  beforeEach(async () => {
    myRepo = await createTestRepo('follower-my');
    otherRepo1 = await createTestRepo('follower-other1');
    otherRepo2 = await createTestRepo('follower-other2');
    await createCommit(myRepo.path, 'initial', { allowEmpty: true });
    await createCommit(otherRepo1.path, 'initial', { allowEmpty: true });
    await createCommit(otherRepo2.path, 'initial', { allowEmpty: true });
    await execGit(myRepo.path, ['remote', 'add', 'origin', `file://${myRepo.path}`]);
    await execGit(otherRepo1.path, ['remote', 'add', 'origin', `file://${otherRepo1.path}`]);
    await execGit(otherRepo2.path, ['remote', 'add', 'origin', `file://${otherRepo2.path}`]);
  });

  afterEach(() => {
    myRepo.cleanup();
    otherRepo1.cleanup();
    otherRepo2.cleanup();
    vi.restoreAllMocks();
  });

  describe('getFollowers()', () => {
    it('should return empty array when no repositories are followed', async () => {
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle repository with no origin', async () => {
      const tempRepo = await createTestRepo('temp-no-origin');
      await createCommit(tempRepo.path, 'initial', { allowEmpty: true });
      const result = await follower.get(tempRepo.path);
      tempRepo.cleanup();
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle lists being checked for followers', async () => {
      await list.createList(myRepo.path, 'following');
      await list.addRepositoryToList(myRepo.path, 'following', 'https://github.com/user/test-repo.git#main');
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle error when workdir does not exist', async () => {
      const result = await follower.get('/nonexistent/invalid/directory');
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle limit option of 0', async () => {
      const result = await follower.get(myRepo.path, { limit: 0 });
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle limit option greater than results', async () => {
      const result = await follower.get(myRepo.path, { limit: 100 });
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle checking repos that throw errors', async () => {
      await list.createList(myRepo.path, 'following');
      await list.addRepositoryToList(myRepo.path, 'following', 'https://github.com/test/repo.git');
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should execute getFollowers logic path', async () => {
      await list.createList(myRepo.path, 'test-list');
      await list.addRepositoryToList(myRepo.path, 'test-list', 'https://github.com/user/repo1.git#main');
      await list.addRepositoryToList(myRepo.path, 'test-list', 'https://github.com/user/repo2.git');
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(Array.isArray(result.data)).toBe(true);
    });

    it('should find followers when repositories follow back', async () => {
      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [{
        url: `file://${otherRepo1.path}`,
        path: otherRepo1.path,
        branch: 'main'
      }];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].followsVia).toBe('following');
    });

    it('should handle multiple followers with different lists', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      await list.createList(otherRepo2.path, 'following');
      await list.addRepositoryToList(otherRepo2.path, 'following', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [
        { url: `file://${otherRepo1.path}`, path: otherRepo1.path, branch: 'main' },
        { url: `file://${otherRepo2.path}`, path: otherRepo2.path, branch: 'main' }
      ];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(2);
      expect(result.data?.map(f => f.followsVia)).toContain('friends');
      expect(result.data?.map(f => f.followsVia)).toContain('following');
    });

    it('should stop at limit when checking followers', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      await list.createList(otherRepo2.path, 'following');
      await list.addRepositoryToList(otherRepo2.path, 'following', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [
        { url: `file://${otherRepo1.path}`, path: otherRepo1.path, branch: 'main' },
        { url: `file://${otherRepo2.path}`, path: otherRepo2.path, branch: 'main' }
      ];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path, { limit: 1 });
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should continue checking when one repo throws error', async () => {
      await list.createList(otherRepo2.path, 'following');
      await list.addRepositoryToList(otherRepo2.path, 'following', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [
        { url: 'file:///nonexistent/path', path: '/nonexistent/path', branch: 'main' },
        { url: `file://${otherRepo2.path}`, path: otherRepo2.path, branch: 'main' }
      ];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should handle inner catch block when list operations throw', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [
        { url: `file://${otherRepo1.path}`, path: otherRepo1.path, branch: 'main' }
      ];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      vi.spyOn(list, 'getLists').mockImplementationOnce(() => {
        throw new Error('Simulated list error');
      }).mockResolvedValueOnce({
        success: true,
        data: [{ id: 'friends', name: 'Friends', repositories: [`file://${myRepo.path}`] }]
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
    });

    it('should skip repositories without paths', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [
        { url: 'https://github.com/user/repo.git', branch: 'main' },
        { url: `file://${otherRepo1.path}`, path: otherRepo1.path, branch: 'main' }
      ];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should handle repository in multiple lists, counting only once', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.createList(otherRepo1.path, 'favorites');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      await list.addRepositoryToList(otherRepo1.path, 'favorites', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [{
        url: `file://${otherRepo1.path}`,
        path: otherRepo1.path,
        branch: 'main'
      }];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].followsVia).toBe('friends');
    });

    it('should handle URL normalization in follower detection', async () => {
      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', `file://${myRepo.path}.git#main`);
      const mockRepos: Repository[] = [{
        url: `file://${otherRepo1.path}`,
        path: otherRepo1.path,
        branch: 'main'
      }];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should handle unexpected errors in outer catch block', async () => {
      vi.spyOn(repository, 'getRepositories').mockRejectedValue(new Error('Unexpected error'));
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_FOLLOWERS_ERROR');
    });

    it('should return NO_ORIGIN error when git.getOriginUrl fails', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: false,
        error: { code: 'NO_REMOTE', message: 'No origin remote found' }
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_ORIGIN');
    });

    it('should return NO_ORIGIN error when git.getOriginUrl returns no data', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: true,
        data: ''
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_ORIGIN');
    });

    it('should check lists that do not contain our repository', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', 'https://github.com/other/different-repo.git');
      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', 'https://github.com/another/repo.git');
      const mockRepos: Repository[] = [{
        url: `file://${otherRepo1.path}`,
        path: otherRepo1.path,
        branch: 'main'
      }];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.get(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(0);
    });
  });

  describe('isFollower()', () => {
    it('should return true when repository follows us', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(true);
    });

    it('should return false when repository does not follow us', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', 'https://github.com/user/some-other-repo.git');
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should return false when cannot get repository lists', async () => {
      const result = await follower.check(myRepo.path, 'https://github.com/nonexistent/repo.git');
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should handle URL normalization', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}.git`);
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(true);
    });

    it('should handle repositories with branch specifiers', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}#main`);
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(true);
    });

    it('should handle error when getting origin URL fails', async () => {
      const result = await follower.check('/nonexistent/invalid/path', otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should handle empty lists in repository', async () => {
      await list.createList(otherRepo1.path, 'friends');
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should check multiple lists in a repository', async () => {
      await list.createList(otherRepo1.path, 'list1');
      await list.createList(otherRepo1.path, 'list2');
      await list.createList(otherRepo1.path, 'list3');
      await list.addRepositoryToList(otherRepo1.path, 'list1', 'https://github.com/other/repo.git');
      await list.addRepositoryToList(otherRepo1.path, 'list2', 'https://github.com/another/repo.git');
      await list.addRepositoryToList(otherRepo1.path, 'list3', `file://${myRepo.path}`);
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(true);
    });

    it('should return false when checked in multiple lists but not found', async () => {
      await list.createList(otherRepo1.path, 'list1');
      await list.createList(otherRepo1.path, 'list2');
      await list.addRepositoryToList(otherRepo1.path, 'list1', 'https://github.com/other/repo.git');
      await list.addRepositoryToList(otherRepo1.path, 'list2', 'https://github.com/another/repo.git');
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should handle unexpected errors in catch block', async () => {
      vi.spyOn(list, 'getLists').mockRejectedValue(new Error('Unexpected error'));
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CHECK_FOLLOWER_ERROR');
    });

    it('should return NO_ORIGIN error when git.getOriginUrl fails', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: false,
        error: { code: 'NO_REMOTE', message: 'No origin remote found' }
      });
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_ORIGIN');
    });

    it('should return NO_ORIGIN error when git.getOriginUrl returns no data', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: true,
        data: ''
      });
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_ORIGIN');
    });

    it('should return false when getLists returns error', async () => {
      vi.spyOn(list, 'getLists').mockResolvedValue({
        success: false,
        error: { code: 'GIT_ERROR', message: 'Failed to read lists' }
      });
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });

    it('should return false when getLists returns no data', async () => {
      vi.spyOn(list, 'getLists').mockResolvedValue({
        success: true,
        data: undefined
      });
      const result = await follower.check(myRepo.path, otherRepo1.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(false);
    });
  });

  describe('getFollowerCount()', () => {
    it('should return 0 when no followers', async () => {
      const result = await follower.count(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(0);
    });

    it('should propagate error from getFollowers when error occurs', async () => {
      vi.spyOn(repository, 'getRepositories').mockRejectedValue(new Error('Unexpected error'));
      const result = await follower.count(myRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_FOLLOWERS_ERROR');
    });

    it('should call getFollowers internally', async () => {
      await list.createList(myRepo.path, 'following');
      await list.addRepositoryToList(myRepo.path, 'following', 'https://github.com/user/repo.git');
      const result = await follower.count(myRepo.path);
      expect(result.success).toBe(true);
      expect(typeof result.data).toBe('number');
      expect(result.data).toBeGreaterThanOrEqual(0);
    });

    it('should handle undefined data from getFollowers', async () => {
      const result = await follower.count(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(0);
    });

    it('should return count when followers are found', async () => {
      await list.createList(otherRepo1.path, 'friends');
      await list.addRepositoryToList(otherRepo1.path, 'friends', `file://${myRepo.path}`);
      const mockRepos: Repository[] = [{
        url: `file://${otherRepo1.path}`,
        path: otherRepo1.path,
        branch: 'main'
      }];
      vi.spyOn(repository, 'getRepositories').mockResolvedValue({
        success: true,
        data: mockRepos
      });
      const result = await follower.count(myRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe(1);
    });
  });
});
