import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { list } from './list';
import { createTestRepo, type TestRepo } from '../test-utils';

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
});
