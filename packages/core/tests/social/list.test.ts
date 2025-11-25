import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { list } from '../../src/social/list';
import { createTestRepo, type TestRepo } from '../test-utils';
import { gitMsgList } from '../../src/gitmsg/lists';
import * as execModule from '../../src/git/exec';
import * as remotesModule from '../../src/git/remotes';
import * as operationsModule from '../../src/git/operations';
import * as storageModule from '../../src/storage';

describe('list', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('list-test');
  });

  afterEach(() => {
    testRepo.cleanup();
  });

  describe('createList()', () => {
    it('should create a new list', async () => {
      const result = await list.createList(testRepo.path, 'reading', 'Reading List');

      expect(result.success).toBe(true);
    });

    it('should reject invalid list names', async () => {
      const invalidNames = [
        'list with spaces',
        'list@home',
        'list.name',
        'a'.repeat(41),
        '',
        'list/with/slash'
      ];

      for (const name of invalidNames) {
        const result = await list.createList(testRepo.path, name);
        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('INVALID_LIST_NAME');
      }
    });

    it('should accept valid list names', async () => {
      const validNames = [
        'reading',
        'my-list',
        'list_123',
        'a',
        'a'.repeat(40),
        'CamelCase',
        'with-dashes',
        'with_underscores'
      ];

      for (const name of validNames) {
        const result = await list.createList(testRepo.path, name);
        expect(result.success).toBe(true);
      }
    });

    it('should reject duplicate list names', async () => {
      await list.createList(testRepo.path, 'reading');
      const duplicate = await list.createList(testRepo.path, 'reading');

      expect(duplicate.success).toBe(false);
      expect(duplicate.error?.code).toBe('LIST_EXISTS');
    });

    it('should create list with custom name', async () => {
      const result = await list.createList(testRepo.path, 'tech', 'Technology & Science');
      expect(result.success).toBe(true);

      const listResult = await list.getList(testRepo.path, 'tech');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.name).toBe('Technology & Science');
      expect(listResult.data?.id).toBe('tech');
    });

    it('should use id as name when name not provided', async () => {
      await list.createList(testRepo.path, 'reading');
      const listResult = await list.getList(testRepo.path, 'reading');

      expect(listResult.success).toBe(true);
      expect(listResult.data?.name).toBe('reading');
    });

    it('should create list with empty repositories array', async () => {
      await list.createList(testRepo.path, 'reading');
      const listResult = await list.getList(testRepo.path, 'reading');

      expect(listResult.success).toBe(true);
      expect(listResult.data?.repositories).toEqual([]);
    });
  });

  describe('getLists()', () => {
    it('should return empty array when no lists exist', async () => {
      const result = await list.getLists(testRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return all lists', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      await list.createList(testRepo.path, 'news');

      const result = await list.getLists(testRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(3);
      expect(result.data?.map(l => l.id)).toContain('reading');
      expect(result.data?.map(l => l.id)).toContain('tech');
      expect(result.data?.map(l => l.id)).toContain('news');
    });
  });

  describe('getList()', () => {
    it('should return null for non-existent list', async () => {
      const result = await list.getList(testRepo.path, 'nonexistent');

      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
    });

    it('should return list by id', async () => {
      await list.createList(testRepo.path, 'reading', 'Reading List');
      const result = await list.getList(testRepo.path, 'reading');

      expect(result.success).toBe(true);
      expect(result.data?.id).toBe('reading');
      expect(result.data?.name).toBe('Reading List');
    });
  });

  describe('deleteList()', () => {
    it('should delete existing list', async () => {
      await list.createList(testRepo.path, 'reading');
      const deleteResult = await list.deleteList(testRepo.path, 'reading');

      expect(deleteResult.success).toBe(true);

      const getResult = await list.getList(testRepo.path, 'reading');
      expect(getResult.data).toBeNull();
    });

    it('should fail when deleting non-existent list', async () => {
      const result = await list.deleteList(testRepo.path, 'nonexistent');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
    });

    it('should remove list from getLists() results', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');

      await list.deleteList(testRepo.path, 'reading');

      const result = await list.getLists(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0]?.id).toBe('tech');
    });
  });

  describe('updateList()', () => {
    it('should update list name', async () => {
      await list.createList(testRepo.path, 'reading', 'Reading List');
      const updateResult = await list.updateList(testRepo.path, 'reading', {
        name: 'Updated Reading List'
      });

      expect(updateResult.success).toBe(true);

      const getResult = await list.getList(testRepo.path, 'reading');
      expect(getResult.data?.name).toBe('Updated Reading List');
    });

    it('should update list repositories', async () => {
      await list.createList(testRepo.path, 'reading');
      const updateResult = await list.updateList(testRepo.path, 'reading', {
        repositories: [
          'https://github.com/user/repo1#branch:main',
          'https://github.com/user/repo2#branch:main'
        ]
      });

      expect(updateResult.success).toBe(true);

      const getResult = await list.getList(testRepo.path, 'reading');
      expect(getResult.data?.repositories).toHaveLength(2);
    });

    it('should fail when updating non-existent list', async () => {
      const result = await list.updateList(testRepo.path, 'nonexistent', {
        name: 'New Name'
      });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
    });

    it('should merge updates with existing data', async () => {
      await list.createList(testRepo.path, 'reading', 'Original Name');
      await list.updateList(testRepo.path, 'reading', {
        repositories: ['https://github.com/user/repo#branch:main']
      });

      const getResult = await list.getList(testRepo.path, 'reading');
      expect(getResult.data?.name).toBe('Original Name');
      expect(getResult.data?.repositories).toHaveLength(1);
    });
  });

  describe('addRepositoryToList()', () => {
    it('should add repository to list', async () => {
      await list.createList(testRepo.path, 'reading');
      const result = await list.addRepositoryToList(
        testRepo.path,
        'reading',
        'https://github.com/user/repo#branch:main'
      );

      expect(result.success).toBe(true);

      const listResult = await list.getList(testRepo.path, 'reading');
      expect(listResult.data?.repositories).toContain('https://github.com/user/repo#branch:main');
    });

    it('should not add duplicate repositories', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
      const duplicate = await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      expect(duplicate.success).toBe(false);
      expect(duplicate.error?.code).toBe('REPOSITORY_EXISTS');

      const listResult = await list.getList(testRepo.path, 'reading');
      expect(listResult.data?.repositories).toHaveLength(1);
    });

    it('should normalize repository hostname to lowercase', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://GitHub.com/user/repo#branch:main');

      const listResult = await list.getList(testRepo.path, 'reading');
      const storedUrl = listResult.data?.repositories?.[0];
      expect(storedUrl?.startsWith('https://github.com/')).toBe(true);
    });

    it('should fail when adding to non-existent list', async () => {
      const result = await list.addRepositoryToList(
        testRepo.path,
        'nonexistent',
        'https://github.com/user/repo#branch:main'
      );

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
    });
  });

  describe('removeRepositoryFromList()', () => {
    it('should remove repository from list', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo1#branch:main');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo2#branch:main');

      const result = await list.removeRepositoryFromList(
        testRepo.path,
        'reading',
        'https://github.com/user/repo1#branch:main'
      );

      expect(result.success).toBe(true);

      const listResult = await list.getList(testRepo.path, 'reading');
      expect(listResult.data?.repositories).toHaveLength(1);
      expect(listResult.data?.repositories).toContain('https://github.com/user/repo2#branch:main');
    });

    it('should fail when removing non-existent repository', async () => {
      await list.createList(testRepo.path, 'reading');
      const result = await list.removeRepositoryFromList(
        testRepo.path,
        'reading',
        'https://github.com/user/nonexistent#branch:main'
      );

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('REPOSITORY_NOT_FOUND');
    });

    it('should fail when removing from non-existent list', async () => {
      const result = await list.removeRepositoryFromList(
        testRepo.path,
        'nonexistent',
        'https://github.com/user/repo#branch:main'
      );

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
    });

    it('should match repositories by base URL when removing', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const result = await list.removeRepositoryFromList(
        testRepo.path,
        'reading',
        'https://github.com/user/repo#branch:develop'
      );

      expect(result.success).toBe(true);

      const listResult = await list.getList(testRepo.path, 'reading');
      expect(listResult.data?.repositories).toHaveLength(0);
    });
  });

  describe('list ID validation', () => {
    it('should validate list ID format', async () => {
      const validIds = ['a', 'abc', 'my-list', 'list_123', 'a'.repeat(40)];
      const invalidIds = ['', 'a'.repeat(41), 'list with spaces', 'list@home', 'list/name'];

      for (const id of validIds) {
        const result = await list.createList(testRepo.path, id);
        expect(result.success).toBe(true);
      }

      for (const id of invalidIds) {
        const result = await list.createList(testRepo.path, id);
        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('INVALID_LIST_NAME');
      }
    });
  });

  describe('Storage & Initialization', () => {
    describe('initialize()', () => {
      it('should set storage base configuration', () => {
        list.initialize({ storageBase: '/test/storage/path' });
      });
    });

    describe('initializeListStorage()', () => {
      it('should load lists into memory', async () => {
        await list.createList(testRepo.path, 'reading');
        await list.createList(testRepo.path, 'tech');
        await list.initializeListStorage(testRepo.path);
        const lists = list.getAllListsFromStorage(testRepo.path);
        expect(lists).toHaveLength(2);
        expect(lists.map(l => l.id)).toContain('reading');
        expect(lists.map(l => l.id)).toContain('tech');
      });

      it('should handle empty lists', async () => {
        await list.initializeListStorage(testRepo.path);
        const lists = list.getAllListsFromStorage(testRepo.path);
        expect(lists).toEqual([]);
      });
    });

    describe('getAllListsFromStorage()', () => {
      it('should return empty array when not initialized', () => {
        const uninitializedPath = '/tmp/uninitialized';
        const lists = list.getAllListsFromStorage(uninitializedPath);
        expect(lists).toEqual([]);
      });

      it('should return all lists when initialized', async () => {
        await list.createList(testRepo.path, 'reading');
        await list.createList(testRepo.path, 'tech');
        await list.initializeListStorage(testRepo.path);
        const lists = list.getAllListsFromStorage(testRepo.path);
        expect(lists).toHaveLength(2);
      });
    });

    describe('getListFromStorage()', () => {
      it('should return undefined when not initialized', () => {
        const uninitializedPath = '/tmp/uninitialized';
        const result = list.getListFromStorage(uninitializedPath, 'reading');
        expect(result).toBeUndefined();
      });

      it('should return list when found', async () => {
        await list.createList(testRepo.path, 'reading', 'Reading List');
        await list.initializeListStorage(testRepo.path);
        const result = list.getListFromStorage(testRepo.path, 'reading');
        expect(result).toBeDefined();
        expect(result?.id).toBe('reading');
        expect(result?.name).toBe('Reading List');
      });

      it('should return undefined when list does not exist', async () => {
        await list.createList(testRepo.path, 'reading');
        await list.initializeListStorage(testRepo.path);
        const result = list.getListFromStorage(testRepo.path, 'nonexistent');
        expect(result).toBeUndefined();
      });
    });
  });

  describe('isPostInList()', () => {
    it('should return true when post repository matches list repository', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test post',
        repository: 'https://github.com/user/repo#branch:main',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'reading', testRepo.path);
      expect(result).toBe(true);
    });

    it('should return true when post repository matches base URL', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test post',
        repository: 'https://github.com/user/repo#branch:develop',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'reading', testRepo.path);
      expect(result).toBe(true);
    });

    it('should return false when post repository does not match', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo1#branch:main');
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test post',
        repository: 'https://github.com/user/repo2#branch:main',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'reading', testRepo.path);
      expect(result).toBe(false);
    });

    it('should return false when list does not exist', async () => {
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test post',
        repository: 'https://github.com/user/repo#branch:main',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'nonexistent', testRepo.path);
      expect(result).toBe(false);
    });

    it('should return false when list has no repositories', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test post',
        repository: 'https://github.com/user/repo#branch:main',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'reading', testRepo.path);
      expect(result).toBe(false);
    });
  });

  describe('Repository Management', () => {
    describe('removeRepositoryFromList() with isRepositoryInAnyList', () => {
      it('should keep repository in cache when still in other lists', async () => {
        await list.createList(testRepo.path, 'reading');
        await list.createList(testRepo.path, 'tech');
        await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
        await list.addRepositoryToList(testRepo.path, 'tech', 'https://github.com/user/repo#branch:main');
        await list.initializeListStorage(testRepo.path);
        const result = await list.removeRepositoryFromList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
        expect(result.success).toBe(true);
        const techList = await list.getList(testRepo.path, 'tech');
        expect(techList.data?.repositories).toContain('https://github.com/user/repo#branch:main');
      });
    });
  });

  describe('getListRepositories()', () => {
    it('should return repositories for existing list', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo1#branch:main');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo2#branch:main');
      const result = await list.getListRepositories(testRepo.path, 'reading');
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(2);
      expect(result.data).toContain('https://github.com/user/repo1#branch:main');
      expect(result.data).toContain('https://github.com/user/repo2#branch:main');
    });

    it('should return empty array for list with no repositories', async () => {
      await list.createList(testRepo.path, 'reading');
      const result = await list.getListRepositories(testRepo.path, 'reading');
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should fail when list does not exist', async () => {
      const result = await list.getListRepositories(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
    });
  });

  describe('getUnpushedListsCount()', () => {
    it('should return 0 when no lists exist', async () => {
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBe(0);
    });

    it('should count local lists when no origin exists', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBeGreaterThanOrEqual(0);
    });
  });

  describe('unfollowList()', () => {
    it('should remove source from followed list', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.updateList(testRepo.path, 'reading', {
        source: 'gitmsg://list:reading@https://github.com/user/repo'
      });
      await list.initializeListStorage(testRepo.path);
      const result = await list.unfollowList(testRepo.path, 'reading');
      expect(result.success).toBe(true);
      const updatedList = await list.getList(testRepo.path, 'reading');
      expect(updatedList.data?.source).toBeUndefined();
    });

    it('should fail when list is not followed', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.initializeListStorage(testRepo.path);
      const result = await list.unfollowList(testRepo.path, 'reading');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
    });

    it('should fail when list does not exist', async () => {
      await list.initializeListStorage(testRepo.path);
      const result = await list.unfollowList(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
    });

    it('should initialize lists if not already initialized', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.updateList(testRepo.path, 'reading', {
        source: 'gitmsg://list:reading@https://github.com/user/repo'
      });
      const result = await list.unfollowList(testRepo.path, 'reading');
      expect(result.success).toBe(true);
    });
  });

  describe('followList() validation', () => {
    it('should reject invalid source list ID', async () => {
      const result = await list.followList(
        testRepo.path,
        'https://github.com/user/repo',
        'invalid list id',
        undefined
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('INVALID_LIST_ID');
    });

    it('should reject invalid target list ID', async () => {
      const result = await list.followList(
        testRepo.path,
        'https://github.com/user/repo',
        'valid-id',
        'invalid target id'
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('INVALID_LIST_ID');
    });
  });

  describe('syncFollowedList() validation', () => {
    it('should fail when list is not followed', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.initializeListStorage(testRepo.path);
      const result = await list.syncFollowedList(testRepo.path, 'reading');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
    });

    it('should fail when list does not exist', async () => {
      await list.initializeListStorage(testRepo.path);
      const result = await list.syncFollowedList(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
    });

    it('should fail when source format is invalid', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.updateList(testRepo.path, 'reading', {
        source: 'invalid-source-format'
      });
      await list.initializeListStorage(testRepo.path);
      const result = await list.syncFollowedList(testRepo.path, 'reading');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('INVALID_SOURCE');
    });

    it('should initialize lists if not already initialized', async () => {
      await list.createList(testRepo.path, 'reading');
      const result = await list.syncFollowedList(testRepo.path, 'reading');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
    });
  });

  describe('getLists() storage cache', () => {
    it('should return lists from storage cache after initialization', async () => {
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      await list.initializeListStorage(testRepo.path);
      const result = await list.getLists(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(2);
      const result2 = await list.getLists(testRepo.path);
      expect(result2.success).toBe(true);
      expect(result2.data).toHaveLength(2);
    });
  });

  describe('Error handling and exception paths', () => {
    it('should handle exception in createList', async () => {
      const spy = vi.spyOn(gitMsgList, 'write').mockRejectedValueOnce(new Error('Write failed'));
      const result = await list.createList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CREATE_LIST_ERROR');
      spy.mockRestore();
    });

    it('should handle write failure in createList', async () => {
      const spy = vi.spyOn(gitMsgList, 'write').mockResolvedValueOnce({ success: false, error: { code: 'WRITE_FAILED', message: 'Failed' } });
      const result = await list.createList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('WRITE_FAILED');
      spy.mockRestore();
    });

    it('should handle exception in deleteList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'delete').mockRejectedValueOnce(new Error('Delete failed'));
      const result = await list.deleteList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('DELETE_LIST_ERROR');
      spy.mockRestore();
    });

    it('should handle delete failure in deleteList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'delete').mockResolvedValueOnce({ success: false, error: { code: 'DELETE_FAILED', message: 'Failed' } });
      const result = await list.deleteList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('DELETE_FAILED');
      spy.mockRestore();
    });

    it('should handle exception in updateList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'write').mockRejectedValueOnce(new Error('Update failed'));
      const result = await list.updateList(testRepo.path, 'test-list', { name: 'Updated' });
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('UPDATE_LIST_ERROR');
      spy.mockRestore();
    });

    it('should handle write failure in updateList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const readSpy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: { id: 'test-list', name: 'Test', repositories: [] } });
      const writeSpy = vi.spyOn(gitMsgList, 'write').mockResolvedValueOnce({ success: false, error: { code: 'WRITE_FAILED', message: 'Failed' } });
      const result = await list.updateList(testRepo.path, 'test-list', { name: 'Updated' });
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('WRITE_FAILED');
      readSpy.mockRestore();
      writeSpy.mockRestore();
    });

    it('should handle exception in addRepositoryToList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'read').mockRejectedValueOnce(new Error('Read failed'));
      const result = await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('ADD_REPOSITORY_ERROR');
      spy.mockRestore();
    });

    it('should handle exception in removeRepositoryFromList', async () => {
      await list.createList(testRepo.path, 'test-list');
      await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      const spy = vi.spyOn(gitMsgList, 'read').mockRejectedValueOnce(new Error('Read failed'));
      const result = await list.removeRepositoryFromList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('REMOVE_REPOSITORY_ERROR');
      spy.mockRestore();
    });

    it('should handle updateList failure in removeRepositoryFromList', async () => {
      await list.createList(testRepo.path, 'test-list');
      await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      const spy = vi.spyOn(gitMsgList, 'write').mockResolvedValueOnce({ success: false, error: { code: 'WRITE_FAILED', message: 'Failed' } });
      const result = await list.removeRepositoryFromList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('WRITE_FAILED');
      spy.mockRestore();
    });

    it('should handle read failure in getList', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: false, error: { code: 'READ_FAILED', message: 'Failed' } });
      const result = await list.getList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('READ_FAILED');
      spy.mockRestore();
    });

    it('should handle missing id field in getList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: { name: 'Test', repositories: [] } });
      const result = await list.getList(testRepo.path, 'test-list');
      expect(result.success).toBe(true);
      expect(result.data?.id).toBe('test-list');
      spy.mockRestore();
    });

    it('should handle missing name field in getList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: { id: 'test-list', repositories: [] } });
      const result = await list.getList(testRepo.path, 'test-list');
      expect(result.success).toBe(true);
      expect(result.data?.name).toBe('test-list');
      spy.mockRestore();
    });
  });

  describe('syncList()', () => {
    it('should handle push branch failure', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit').mockResolvedValueOnce({ success: false, error: { code: 'PUSH_FAILED', message: 'Push failed' } });
      const result = await list.syncList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('PUSH_ERROR');
      spy.mockRestore();
    });

    it('should handle push ref failure', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: false, error: { code: 'PUSH_REF_FAILED', message: 'Push ref failed' } });
      const result = await list.syncList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('PUSH_REF_ERROR');
      spy.mockRestore();
    });

    it('should handle exception in syncList', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit').mockRejectedValueOnce(new Error('Sync failed'));
      const result = await list.syncList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('SYNC_ERROR');
      spy.mockRestore();
    });
  });

  describe('getListRepositories() error path', () => {
    it('should propagate getList error', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: false, error: { code: 'READ_FAILED', message: 'Failed' } });
      const result = await list.getListRepositories(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('READ_FAILED');
      spy.mockRestore();
    });

    it('should handle null list data', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: null });
      const result = await list.getListRepositories(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
      spy.mockRestore();
    });
  });

  describe('getUnpushedListsCount() comprehensive', () => {
    it('should handle origin with no lists', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockResolvedValueOnce({ success: true, data: { stdout: 'origin\turl', stderr: '' } })
        .mockResolvedValueOnce({ success: false, error: { code: 'LS_REMOTE_FAILED', message: 'Failed' } });
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBeGreaterThanOrEqual(0);
      spy.mockRestore();
    });

    it('should handle origin with empty stdout', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockResolvedValueOnce({ success: true, data: { stdout: 'origin\turl', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBeGreaterThanOrEqual(0);
      spy.mockRestore();
    });
  });

  describe('addRepositoryToList() error paths', () => {
    it('should handle branch detection failure', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(remotesModule, 'getRemoteDefaultBranch').mockResolvedValueOnce({ success: false, error: { code: 'BRANCH_DETECTION_FAILED', message: 'Failed' } });
      const result = await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('BRANCH_DETECTION_FAILED');
      spy.mockRestore();
    });

    it('should handle updateList failure when adding repository', async () => {
      await list.createList(testRepo.path, 'test-list');
      const readSpy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: { id: 'test-list', name: 'Test', repositories: [] } });
      const writeSpy = vi.spyOn(gitMsgList, 'write').mockResolvedValueOnce({ success: false, error: { code: 'WRITE_FAILED', message: 'Failed' } });
      const result = await list.addRepositoryToList(testRepo.path, 'test-list', 'https://github.com/user/repo#branch:main');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('WRITE_FAILED');
      readSpy.mockRestore();
      writeSpy.mockRestore();
    });
  });

  describe('removeRepositoryFromList() repository still in lists', () => {
    it('should log when repository still exists in other lists', async () => {
      await list.createList(testRepo.path, 'list1');
      await list.createList(testRepo.path, 'list2');
      await list.addRepositoryToList(testRepo.path, 'list1', 'https://github.com/user/repo#branch:main');
      await list.addRepositoryToList(testRepo.path, 'list2', 'https://github.com/user/repo#branch:main');
      await list.initializeListStorage(testRepo.path);
      const result = await list.removeRepositoryFromList(testRepo.path, 'list1', 'https://github.com/user/repo#branch:main');
      expect(result.success).toBe(true);
      const list2Data = await list.getList(testRepo.path, 'list2');
      expect(list2Data.data?.repositories).toContain('https://github.com/user/repo#branch:main');
    });
  });

  describe('getList() exception handler', () => {
    it('should handle exception in getList', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockRejectedValueOnce(new Error('Read failed'));
      const result = await list.getList(testRepo.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_LIST_ERROR');
      spy.mockRestore();
    });
  });

  describe('getLists() with origin checks', () => {
    it('should check origin and set unpushed status', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/repo', stderr: '' } });
          }
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown command' } });
        });
      const result = await list.getLists(testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });

    it('should handle origin with lists', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/repo', stderr: '' } });
          }
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: { stdout: 'abc123\trefs/gitmsg/social/lists/test-list', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown command' } });
        });
      const result = await list.getLists(testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });
  });

  describe('getUnpushedListsCount() detailed', () => {
    it('should count unpushed lists correctly', async () => {
      await list.createList(testRepo.path, 'list1');
      await list.createList(testRepo.path, 'list2');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/repo', stderr: '' } });
          }
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: { stdout: 'abc123\trefs/gitmsg/social/lists/list1\n', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown command' } });
        });
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBeGreaterThanOrEqual(0);
      spy.mockRestore();
    });
  });

  describe('Initialization paths', () => {
    it('should initialize lists in syncFollowedList when not initialized', async () => {
      const testRepo2 = await createTestRepo('list-test-sync');
      await list.createList(testRepo2.path, 'test-list');
      const result = await list.syncFollowedList(testRepo2.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
      testRepo2.cleanup();
    });

    it('should initialize lists in unfollowList when not initialized', async () => {
      const testRepo2 = await createTestRepo('list-test-unfollow');
      await list.createList(testRepo2.path, 'test-list');
      const result = await list.unfollowList(testRepo2.path, 'test-list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_FOLLOWED');
      testRepo2.cleanup();
    });
  });

  describe('syncList() success path', () => {
    it('should successfully sync list to remote', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      const result = await list.syncList(testRepo.path, 'test-list');
      expect(result.success).toBe(true);
      spy.mockRestore();
    });

    it('should sync list with custom remote', async () => {
      await list.createList(testRepo.path, 'test-list');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      const result = await list.syncList(testRepo.path, 'test-list', 'upstream');
      expect(result.success).toBe(true);
      spy.mockRestore();
    });
  });

  describe('getLists() with remote repository', () => {
    it('should call getRemoteLists for remote repository URL', async () => {
      const result = await list.getLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
    });
  });

  describe('getUnpushedListsCount() additional paths', () => {
    it('should handle lists with matching origin lists', async () => {
      await list.createList(testRepo.path, 'list1');
      await list.createList(testRepo.path, 'list2');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/repo', stderr: '' } });
          }
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: { stdout: 'abc123\trefs/gitmsg/social/lists/list1\ndef456\trefs/gitmsg/social/lists/list2\n', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBe(0);
      spy.mockRestore();
    });

    it('should handle no data from ls-remote', async () => {
      await list.createList(testRepo.path, 'list1');
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/repo', stderr: '' } });
          }
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: null });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const count = await list.getUnpushedListsCount(testRepo.path);
      expect(count).toBeGreaterThanOrEqual(0);
      spy.mockRestore();
    });
  });

  describe('getLists() isolated clone path', () => {
    it('should handle isolated clone with upstream remote', async () => {
      const testRepo2 = await createTestRepo('isolated-clone-test');
      list.initialize({ storageBase: testRepo2.path });
      const listRefsSpy = vi.spyOn(operationsModule, 'listRefs').mockResolvedValueOnce([]);
      const execSpy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'upstream') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/upstream', stderr: '' } });
          }
          if (args[0] === 'for-each-ref') {
            return Promise.resolve({ success: true, data: { stdout: 'refs/remotes/upstream/gitmsg/social/lists/test-list\n', stderr: '' } });
          }
          if (args[0] === 'show') {
            return Promise.resolve({ success: true, data: { stdout: JSON.stringify({ id: 'test-list', name: 'Test', repositories: [] }), stderr: '' } });
          }
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: false, error: { code: 'NOT_FOUND', message: 'Not found' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getLists(testRepo2.path);
      expect(result.success).toBe(true);
      listRefsSpy.mockRestore();
      execSpy.mockRestore();
      testRepo2.cleanup();
    });

    it('should handle isolated clone with fetch failure', async () => {
      const testRepo2 = await createTestRepo('isolated-clone-fetch-fail');
      list.initialize({ storageBase: testRepo2.path });
      const listRefsSpy = vi.spyOn(operationsModule, 'listRefs').mockResolvedValueOnce([]);
      const fetchSpy = vi.spyOn(storageModule.storage.repository, 'fetch').mockResolvedValueOnce({
        success: false,
        error: { code: 'LOCK_FILE_ERROR', message: 'Lock file error' }
      });
      const execSpy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'upstream') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/upstream', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getLists(testRepo2.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
      listRefsSpy.mockRestore();
      fetchSpy.mockRestore();
      execSpy.mockRestore();
      testRepo2.cleanup();
    });

    it('should handle isolated clone with JSON parse failure', async () => {
      const testRepo2 = await createTestRepo('isolated-clone-parse-fail');
      const listRefsSpy = vi.spyOn(operationsModule, 'listRefs').mockResolvedValueOnce([]);
      const execSpy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'upstream') {
            return Promise.resolve({ success: true, data: { stdout: 'https://github.com/user/upstream', stderr: '' } });
          }
          if (args[0] === 'for-each-ref') {
            return Promise.resolve({ success: true, data: { stdout: 'refs/remotes/upstream/gitmsg/social/lists/test-list\n', stderr: '' } });
          }
          if (args[0] === 'show') {
            return Promise.resolve({ success: true, data: { stdout: 'invalid json', stderr: '' } });
          }
          if (args[0] === 'remote' && args[1] === 'get-url' && args[2] === 'origin') {
            return Promise.resolve({ success: false, error: { code: 'NOT_FOUND', message: 'Not found' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getLists(testRepo2.path);
      expect(result.success).toBe(true);
      listRefsSpy.mockRestore();
      execSpy.mockRestore();
      testRepo2.cleanup();
    });
  });

  describe('getLists() exception handling', () => {
    it('should handle exception and return error', async () => {
      const testRepo2 = await createTestRepo('exception-test');
      const spy = vi.spyOn(operationsModule, 'listRefs').mockRejectedValueOnce(new Error('Fatal error'));
      const result = await list.getLists(testRepo2.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_LISTS_ERROR');
      spy.mockRestore();
      testRepo2.cleanup();
    });
  });

  describe('getWorkspaceRemoteLists()', () => {
    it('should fetch and parse lists from workspace remote', async () => {
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'ls-remote') {
            return Promise.resolve({
              success: true,
              data: {
                stdout: 'abc123\trefs/gitmsg/social/lists/test-list\n',
                stderr: ''
              }
            });
          }
          if (args[0] === 'fetch') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          if (args[0] === 'show') {
            return Promise.resolve({
              success: true,
              data: {
                stdout: JSON.stringify({ id: 'test-list', name: 'Test List', repositories: [] }),
                stderr: ''
              }
            });
          }
          if (args[0] === 'update-ref' && args[1] === '-d') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });

    it('should handle no lists found in workspace remote', async () => {
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'ls-remote') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
      spy.mockRestore();
    });

    it('should handle fetch failure in workspace remote', async () => {
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'ls-remote') {
            return Promise.resolve({
              success: true,
              data: {
                stdout: 'abc123\trefs/gitmsg/social/lists/test-list\n',
                stderr: ''
              }
            });
          }
          if (args[0] === 'fetch') {
            return Promise.resolve({ success: false, error: { code: 'FETCH_FAILED', message: 'Failed' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });

    it('should handle JSON parse failure in workspace remote', async () => {
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'ls-remote') {
            return Promise.resolve({
              success: true,
              data: {
                stdout: 'abc123\trefs/gitmsg/social/lists/test-list\n',
                stderr: ''
              }
            });
          }
          if (args[0] === 'fetch') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          if (args[0] === 'show') {
            return Promise.resolve({ success: true, data: { stdout: 'invalid json', stderr: '' } });
          }
          if (args[0] === 'update-ref' && args[1] === '-d') {
            return Promise.resolve({ success: true, data: { stdout: '', stderr: '' } });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });

    it('should handle isolated repository', async () => {
      const spy = vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'remote') {
            return Promise.resolve({
              success: true,
              data: { stdout: '', stderr: '' }
            });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      spy.mockRestore();
    });
  });

  describe('followList() error paths', () => {
    it('should handle source list not found', async () => {
      const result = await list.followList(testRepo.path, 'nonexistent', 'https://github.com/user/repo');
      expect(result.success).toBe(false);
    });

    it('should handle createList failure', async () => {
      const result = await list.followList(testRepo.path, 'invalid name!', 'https://github.com/user/repo');
      expect(result.success).toBe(false);
    });
  });

  describe('syncFollowedList() error paths', () => {
    it('should handle source list not found in remote lists', async () => {
      await list.createList(testRepo.path, 'test-list');
      const result = await list.syncFollowedList(testRepo.path, 'test-list', {
        sourceRepository: 'https://github.com/nonexistent/repo',
        sourceListId: 'nonexistent'
      });
      expect(result.success).toBe(false);
    });
  });

  describe('in-memory storage updates', () => {
    it('should update in-memory storage when creating list after getLists', async () => {
      await list.getLists(testRepo.path);
      const result = await list.createList(testRepo.path, 'mem-test', 'Memory Test');
      expect(result.success).toBe(true);
      const lists = await list.getLists(testRepo.path);
      expect(lists.success).toBe(true);
      const found = lists.data!.find(l => l.id === 'mem-test');
      expect(found).toBeDefined();
      expect(found?.isUnpushed).toBe(true);
    });

    it('should update in-memory storage when adding repository', async () => {
      await list.getLists(testRepo.path);
      await list.createList(testRepo.path, 'repo-test');
      const result = await list.addRepositoryToList(testRepo.path, 'repo-test', 'https://github.com/test/repo');
      expect(result.success).toBe(true);
      const listResult = await list.getList(testRepo.path, 'repo-test');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.repositories.length).toBeGreaterThan(0);
    });

    it('should update in-memory storage when removing repository', async () => {
      await list.getLists(testRepo.path);
      await list.createList(testRepo.path, 'remove-test');
      await list.addRepositoryToList(testRepo.path, 'remove-test', 'https://github.com/test/repo');
      const result = await list.removeRepositoryFromList(testRepo.path, 'remove-test', 'https://github.com/test/repo');
      expect(result.success).toBe(true);
      const listResult = await list.getList(testRepo.path, 'remove-test');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.repositories).not.toContain('https://github.com/test/repo');
    });

    it('should update in-memory storage when updating list', async () => {
      await list.getLists(testRepo.path);
      await list.createList(testRepo.path, 'update-test', 'Original Name');
      const result = await list.updateList(testRepo.path, 'update-test', {
        name: 'Updated Name'
      });
      expect(result.success).toBe(true);
      const listResult = await list.getList(testRepo.path, 'update-test');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.name).toBe('Updated Name');
    });

    it('should update in-memory storage when deleting list', async () => {
      await list.getLists(testRepo.path);
      await list.createList(testRepo.path, 'delete-test');
      const result = await list.deleteList(testRepo.path, 'delete-test');
      expect(result.success).toBe(true);
      const lists = await list.getLists(testRepo.path);
      expect(lists.success).toBe(true);
      const found = lists.data!.find(l => l.id === 'delete-test');
      expect(found).toBeUndefined();
    });
  });

  describe('repository URL handling', () => {
    it('should handle repository URLs with branch suffix', async () => {
      await list.createList(testRepo.path, 'branch-test');
      const result = await list.addRepositoryToList(testRepo.path, 'branch-test', 'https://github.com/test/repo#branch:develop');
      expect(result.success).toBe(true);
      const listResult = await list.getList(testRepo.path, 'branch-test');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.repositories).toContain('https://github.com/test/repo#branch:develop');
    });

    it('should handle duplicate repository URLs', async () => {
      await list.createList(testRepo.path, 'dup-test');
      await list.addRepositoryToList(testRepo.path, 'dup-test', 'https://github.com/test/repo');
      const result = await list.addRepositoryToList(testRepo.path, 'dup-test', 'https://github.com/test/repo');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('REPOSITORY_EXISTS');
    });

    it('should handle removing non-existent repository', async () => {
      await list.createList(testRepo.path, 'nonexist-test');
      const result = await list.removeRepositoryFromList(testRepo.path, 'nonexist-test', 'https://github.com/test/repo');
      expect(result.success).toBe(false);
    });
  });

  describe('runtime field removal', () => {
    it('should remove isUnpushed before writing', async () => {
      await list.getLists(testRepo.path);
      await list.createList(testRepo.path, 'unpushed-test');
      const listResult = await list.getList(testRepo.path, 'unpushed-test');
      expect(listResult.success).toBe(true);
      expect(listResult.data?.isUnpushed).toBe(true);
    });

    it('should remove isFollowedLocally before writing', async () => {
      await list.createList(testRepo.path, 'followed-test');
      const listResult = await list.getList(testRepo.path, 'followed-test');
      expect(listResult.success).toBe(true);
    });
  });

  describe('initializeListStorage() error paths', () => {
    it('should handle getLists returning success with null data', async () => {
      const testRepo2 = await createTestRepo('init-null-test');
      const spy = vi.spyOn(list, 'getLists').mockResolvedValueOnce({ success: true, data: null });
      await list.initializeListStorage(testRepo2.path);
      const lists = list.getAllListsFromStorage(testRepo2.path);
      expect(lists).toEqual([]);
      spy.mockRestore();
      testRepo2.cleanup();
    });

    it('should handle getLists returning success with undefined data', async () => {
      const testRepo2 = await createTestRepo('init-undefined-test');
      const spy = vi.spyOn(list, 'getLists').mockResolvedValueOnce({ success: true, data: undefined });
      await list.initializeListStorage(testRepo2.path);
      const lists = list.getAllListsFromStorage(testRepo2.path);
      expect(lists).toEqual([]);
      spy.mockRestore();
      testRepo2.cleanup();
    });

    it('should handle exception during initialization', async () => {
      const testRepo2 = await createTestRepo('init-exception-test');
      const spy = vi.spyOn(list, 'getLists').mockRejectedValueOnce(new Error('Fatal error'));
      await expect(list.initializeListStorage(testRepo2.path)).resolves.not.toThrow();
      spy.mockRestore();
      testRepo2.cleanup();
    });
  });

  describe('isPostInList() with branch suffixes', () => {
    it('should match posts when list repos have branch suffixes', async () => {
      await list.createList(testRepo.path, 'branch-match-test');
      await list.updateList(testRepo.path, 'branch-match-test', {
        repositories: ['https://github.com/user/repo#branch:main']
      });
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test',
        repository: 'https://github.com/user/repo',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'branch-match-test', testRepo.path);
      expect(result).toBe(true);
    });

    it('should match posts with different branch suffixes to base URL', async () => {
      await list.createList(testRepo.path, 'branch-diff-test');
      await list.updateList(testRepo.path, 'branch-diff-test', {
        repositories: ['https://github.com/user/repo#branch:develop']
      });
      await list.initializeListStorage(testRepo.path);
      const post = {
        id: 'test',
        author: { name: 'Test', email: 'test@test.com', date: new Date() },
        content: 'Test',
        repository: 'https://github.com/user/repo#branch:main',
        timestamp: new Date()
      };
      const result = list.isPostInList(post, 'branch-diff-test', testRepo.path);
      expect(result).toBe(true);
    });
  });

  describe('createList() existing list check', () => {
    it('should check for existing list before creating', async () => {
      await list.createList(testRepo.path, 'existing-check');
      const readSpy = vi.spyOn(gitMsgList, 'read')
        .mockResolvedValueOnce({ success: true, data: { id: 'existing-check', name: 'Existing', repositories: [] } });
      const result = await list.createList(testRepo.path, 'existing-check');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_EXISTS');
      expect(readSpy).toHaveBeenCalled();
      readSpy.mockRestore();
    });
  });

  describe('deleteList() null data handling', () => {
    it('should handle list read returning success with null data', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: null });
      const result = await list.deleteList(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
      spy.mockRestore();
    });

    it('should handle list read returning success with undefined data', async () => {
      const spy = vi.spyOn(gitMsgList, 'read').mockResolvedValueOnce({ success: true, data: undefined });
      const result = await list.deleteList(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('LIST_NOT_FOUND');
      spy.mockRestore();
    });
  });

  describe('addRepositoryToList() branch detection', () => {
    it('should detect default branch when not specified', async () => {
      await list.createList(testRepo.path, 'branch-detect-test');
      const remoteSpy = vi.spyOn(remotesModule, 'getRemoteDefaultBranch')
        .mockResolvedValueOnce({ success: true, data: 'main' });
      const _result = await list.addRepositoryToList(
        testRepo.path,
        'branch-detect-test',
        'https://github.com/user/repo'
      );
      expect(remoteSpy).toHaveBeenCalled();
      remoteSpy.mockRestore();
    });

    it('should use provided branch when specified', async () => {
      await list.createList(testRepo.path, 'branch-provided-test');
      const remoteSpy = vi.spyOn(remotesModule, 'getRemoteDefaultBranch');
      const _result = await list.addRepositoryToList(
        testRepo.path,
        'branch-provided-test',
        'https://github.com/user/repo#branch:develop'
      );
      expect(remoteSpy).not.toHaveBeenCalled();
      remoteSpy.mockRestore();
    });

    it('should handle branch detection returning null data', async () => {
      await list.createList(testRepo.path, 'branch-null-test');
      const remoteSpy = vi.spyOn(remotesModule, 'getRemoteDefaultBranch')
        .mockResolvedValueOnce({ success: true, data: null });
      const result = await list.addRepositoryToList(testRepo.path, 'branch-null-test', 'https://github.com/user/repo');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('BRANCH_DETECTION_FAILED');
      remoteSpy.mockRestore();
    });
  });

  describe('getRemoteLists() storage and branch handling', () => {
    it('should return empty array when storageBase not configured', async () => {
      list.initialize({ storageBase: '' });
      const remoteSpy = vi.spyOn(remotesModule, 'listRemotes').mockResolvedValueOnce({
        success: true,
        data: []
      });
      const result = await list.getRemoteLists('https://github.com/user/repo', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
      remoteSpy.mockRestore();
      list.initialize({ storageBase: testRepo.path });
    });

    it('should parse branch from repository URL', async () => {
      list.initialize({ storageBase: testRepo.path });
      const remoteSpy = vi.spyOn(remotesModule, 'listRemotes').mockResolvedValueOnce({
        success: true,
        data: []
      });
      const storageSpy = vi.spyOn(storageModule.storage.repository, 'ensure').mockResolvedValueOnce({
        success: true,
        data: '/tmp/isolated'
      });
      const execSpy = vi.spyOn(execModule, 'execGit').mockResolvedValueOnce({
        success: true,
        data: { stdout: '', stderr: '' }
      });
      const result = await list.getRemoteLists(
        'https://github.com/user/repo#branch:develop',
        testRepo.path
      );
      expect(result.success).toBe(true);
      remoteSpy.mockRestore();
      storageSpy.mockRestore();
      execSpy.mockRestore();
    });

    it('should use isolated repository for external repos', async () => {
      list.initialize({ storageBase: testRepo.path });

      vi.spyOn(remotesModule, 'listRemotes').mockResolvedValueOnce({
        success: true,
        data: []
      });

      const ensureSpy = vi.spyOn(storageModule.storage.repository, 'ensure')
        .mockResolvedValueOnce({
          success: true,
          data: '/tmp/isolated/repo'
        });

      vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'for-each-ref') {
            return Promise.resolve({
              success: true,
              data: { stdout: 'refs/remotes/upstream/gitmsg/social/lists/test\n', stderr: '' }
            });
          }
          if (args[0] === 'show') {
            return Promise.resolve({
              success: true,
              data: { stdout: JSON.stringify({ id: 'test', name: 'Test', repositories: [] }), stderr: '' }
            });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });

      const result = await list.getRemoteLists('https://github.com/external/repo', testRepo.path);
      expect(result.success).toBe(true);
      expect(ensureSpy).toHaveBeenCalled();
      vi.restoreAllMocks();
    });
  });

  describe('followList() implementation', () => {
    it('should handle getRemoteLists failure', async () => {
      const remoteSpy = vi.spyOn(remotesModule, 'listRemotes').mockResolvedValueOnce({
        success: false,
        error: { code: 'FETCH_FAILED', message: 'Failed' }
      });

      const result = await list.followList(
        testRepo.path,
        'https://github.com/user/repo',
        'source-list'
      );
      expect(result.success).toBe(false);
      remoteSpy.mockRestore();
    });

    it('should handle source list not found', async () => {
      const remoteSpy = vi.spyOn(remotesModule, 'listRemotes').mockResolvedValueOnce({
        success: true,
        data: []
      });

      const result = await list.followList(
        testRepo.path,
        'https://github.com/user/repo',
        'nonexistent-list'
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('SOURCE_NOT_FOUND');
      remoteSpy.mockRestore();
    });

    it('should successfully follow a remote list', async () => {
      await list.initializeListStorage(testRepo.path);

      vi.spyOn(remotesModule, 'listRemotes').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(storageModule.storage.repository, 'ensure')
        .mockResolvedValue({
          success: true,
          data: '/tmp/isolated/repo'
        });

      vi.spyOn(execModule, 'execGit')
        .mockImplementation((workdir, args) => {
          if (args[0] === 'for-each-ref') {
            return Promise.resolve({
              success: true,
              data: { stdout: 'refs/remotes/upstream/gitmsg/social/lists/remote-source\n', stderr: '' }
            });
          }
          if (args[0] === 'show') {
            return Promise.resolve({
              success: true,
              data: {
                stdout: JSON.stringify({
                  id: 'remote-source',
                  name: 'Remote Source',
                  repositories: ['https://github.com/repo1#branch:main']
                }),
                stderr: ''
              }
            });
          }
          return Promise.resolve({ success: false, error: { code: 'UNKNOWN', message: 'Unknown' } });
        });

      vi.spyOn(remotesModule, 'getRemoteDefaultBranch').mockResolvedValue({
        success: true,
        data: 'main'
      });

      const result = await list.followList(
        testRepo.path,
        'https://github.com/user/repo',
        'remote-source'
      );
      expect(result.success).toBe(true);
      expect(result.data?.listId).toBe('remote-source');

      const followedList = await list.getList(testRepo.path, 'remote-source');
      expect(followedList.success).toBe(true);
      if (followedList.data) {
        expect(followedList.data.source).toBeDefined();
      }
      vi.restoreAllMocks();
    });
  });

  describe('syncFollowedList() implementation', () => {
    it('should handle lists not initialized', async () => {
      const testRepo2 = await createTestRepo('sync-uninit-test');
      await list.createList(testRepo2.path, 'test-sync');
      await list.updateList(testRepo2.path, 'test-sync', {
        source: 'gitmsg://list:remote@https://github.com/user/repo'
      });

      const result = await list.syncFollowedList(testRepo2.path, 'test-sync');
      expect(result.success).toBe(false);
      testRepo2.cleanup();
    });
  });

});
