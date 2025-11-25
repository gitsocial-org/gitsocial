import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { gitMsgList } from '../../src/gitmsg/lists';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { readGitRef } from '../../src/git/operations';
import { execGit } from '../../src/git/exec';
import * as execModule from '../../src/git/exec';
import * as operationsModule from '../../src/git/operations';

describe('gitmsg/lists', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('gitmsg-lists');
    await createCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
  });

  afterEach(() => {
    testRepo.cleanup();
  });

  describe('write()', () => {
    it('should write list data', async () => {
      const listData = { items: ['repo1', 'repo2'], version: 1 };
      const result = await gitMsgList.write(testRepo.path, 'social', 'reading', listData);
      expect(result.success).toBe(true);
      const refResult = await readGitRef(testRepo.path, 'refs/gitmsg/social/lists/reading');
      expect(refResult.success).toBe(true);
      expect(refResult.data).toBeDefined();
    });

    it('should write string list', async () => {
      const listData = { name: 'test', value: 'hello' };
      const result = await gitMsgList.write(testRepo.path, 'social', 'test', listData);
      expect(result.success).toBe(true);
    });

    it('should write array list', async () => {
      const listData = ['item1', 'item2', 'item3'];
      const result = await gitMsgList.write(testRepo.path, 'social', 'array', listData);
      expect(result.success).toBe(true);
    });

    it('should write nested object', async () => {
      const listData = {
        config: { enabled: true, options: { foo: 'bar' } },
        items: [{ id: 1, name: 'test' }]
      };
      const result = await gitMsgList.write(testRepo.path, 'social', 'complex', listData);
      expect(result.success).toBe(true);
    });

    it('should maintain commit history on updates', async () => {
      const data1 = { version: 1 };
      const data2 = { version: 2 };
      await gitMsgList.write(testRepo.path, 'social', 'versioned', data1);
      await gitMsgList.write(testRepo.path, 'social', 'versioned', data2);
      const history = await gitMsgList.getHistory(testRepo.path, 'social', 'versioned', testRepo.path);
      expect(history.success).toBe(true);
      expect(history.data?.length).toBe(2);
    });

    it('should handle empty object', async () => {
      const result = await gitMsgList.write(testRepo.path, 'social', 'empty', {});
      expect(result.success).toBe(true);
    });

    it('should handle null values', async () => {
      const listData = { value: null, other: 'data' };
      const result = await gitMsgList.write(testRepo.path, 'social', 'nulls', listData);
      expect(result.success).toBe(true);
    });

    it('should handle different extension names', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list1', { data: 1 });
      await gitMsgList.write(testRepo.path, 'custom', 'list2', { data: 2 });
      const socialResult = await gitMsgList.read(testRepo.path, 'social', 'list1');
      const customResult = await gitMsgList.read(testRepo.path, 'custom', 'list2');
      expect(socialResult.success).toBe(true);
      expect(customResult.success).toBe(true);
    });

    it('should overwrite existing list', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list', { version: 1 });
      const result = await gitMsgList.write(testRepo.path, 'social', 'list', { version: 2 });
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'list');
      expect(readResult.data).toEqual({ version: 2 });
    });
  });

  describe('read()', () => {
    it('should read existing list', async () => {
      const listData = { items: ['repo1', 'repo2'], version: 1 };
      await gitMsgList.write(testRepo.path, 'social', 'reading', listData);
      const result = await gitMsgList.read(testRepo.path, 'social', 'reading');
      expect(result.success).toBe(true);
      expect(result.data).toEqual(listData);
    });

    it('should return null for nonexistent list', async () => {
      const result = await gitMsgList.read(testRepo.path, 'social', 'nonexistent');
      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
    });

    it('should return null when ref points to invalid commit', async () => {
      await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/lists/corrupted', '0000000000000000000000000000000000000000']);
      const result = await gitMsgList.read(testRepo.path, 'social', 'corrupted');
      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
    });

    it('should read list with typed data', async () => {
      interface ListData {
        name: string;
        count: number;
      }
      const listData: ListData = { name: 'test', count: 42 };
      await gitMsgList.write(testRepo.path, 'social', 'typed', listData);
      const result = await gitMsgList.read<ListData>(testRepo.path, 'social', 'typed');
      expect(result.success).toBe(true);
      expect(result.data?.name).toBe('test');
      expect(result.data?.count).toBe(42);
    });

    it('should read array list', async () => {
      const listData = ['a', 'b', 'c'];
      await gitMsgList.write(testRepo.path, 'social', 'array', listData);
      const result = await gitMsgList.read<string[]>(testRepo.path, 'social', 'array');
      expect(result.success).toBe(true);
      expect(result.data).toEqual(listData);
    });

    it('should read nested object', async () => {
      const listData = {
        nested: { deep: { value: 'test' } },
        array: [1, 2, 3]
      };
      await gitMsgList.write(testRepo.path, 'social', 'nested', listData);
      const result = await gitMsgList.read(testRepo.path, 'social', 'nested');
      expect(result.success).toBe(true);
      expect(result.data).toEqual(listData);
    });

    it('should read most recent version', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'versioned', { version: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'versioned', { version: 2 });
      await gitMsgList.write(testRepo.path, 'social', 'versioned', { version: 3 });
      const result = await gitMsgList.read(testRepo.path, 'social', 'versioned');
      expect(result.success).toBe(true);
      expect(result.data).toEqual({ version: 3 });
    });

    it('should handle empty object', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'empty', {});
      const result = await gitMsgList.read(testRepo.path, 'social', 'empty');
      expect(result.success).toBe(true);
      expect(result.data).toEqual({});
    });

    it('should handle list names with special characters', async () => {
      const listData = { test: true };
      await gitMsgList.write(testRepo.path, 'social', 'list-with-dashes', listData);
      const result = await gitMsgList.read(testRepo.path, 'social', 'list-with-dashes');
      expect(result.success).toBe(true);
      expect(result.data).toEqual(listData);
    });
  });

  describe('delete()', () => {
    beforeEach(async () => {
      await gitMsgList.write(testRepo.path, 'social', 'to-delete', { data: 'test' });
    });

    it('should delete existing list', async () => {
      const result = await gitMsgList.delete(testRepo.path, 'social', 'to-delete');
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'to-delete');
      expect(readResult.data).toBeNull();
    });

    it('should succeed deleting nonexistent list', async () => {
      const result = await gitMsgList.delete(testRepo.path, 'social', 'nonexistent');
      expect(result.success).toBe(true);
    });

    it('should delete without affecting other lists', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'keep1', { data: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'keep2', { data: 2 });
      await gitMsgList.delete(testRepo.path, 'social', 'to-delete');
      const keep1 = await gitMsgList.read(testRepo.path, 'social', 'keep1');
      const keep2 = await gitMsgList.read(testRepo.path, 'social', 'keep2');
      expect(keep1.data).toEqual({ data: 1 });
      expect(keep2.data).toEqual({ data: 2 });
    });

    it('should allow recreating deleted list', async () => {
      await gitMsgList.delete(testRepo.path, 'social', 'to-delete');
      const writeResult = await gitMsgList.write(testRepo.path, 'social', 'to-delete', { new: 'data' });
      expect(writeResult.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'to-delete');
      expect(readResult.data).toEqual({ new: 'data' });
    });

    it('should delete from specific extension only', async () => {
      await gitMsgList.write(testRepo.path, 'custom', 'same-name', { ext: 'custom' });
      await gitMsgList.write(testRepo.path, 'social', 'same-name', { ext: 'social' });
      await gitMsgList.delete(testRepo.path, 'social', 'same-name');
      const customResult = await gitMsgList.read(testRepo.path, 'custom', 'same-name');
      const socialResult = await gitMsgList.read(testRepo.path, 'social', 'same-name');
      expect(customResult.data).toEqual({ ext: 'custom' });
      expect(socialResult.data).toBeNull();
    });
  });

  describe('enumerate()', () => {
    it('should return empty array when no lists', async () => {
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should enumerate single list', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list1', { data: 1 });
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(true);
      expect(result.data).toContain('social/lists/list1');
      expect(result.data?.length).toBe(1);
    });

    it('should enumerate multiple lists', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list1', { data: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'list2', { data: 2 });
      await gitMsgList.write(testRepo.path, 'social', 'list3', { data: 3 });
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(3);
      expect(result.data).toContain('social/lists/list1');
      expect(result.data).toContain('social/lists/list2');
      expect(result.data).toContain('social/lists/list3');
    });

    it('should only enumerate lists for specific extension', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'social-list', { data: 1 });
      await gitMsgList.write(testRepo.path, 'custom', 'custom-list', { data: 2 });
      const socialResult = await gitMsgList.enumerate(testRepo.path, 'social');
      const customResult = await gitMsgList.enumerate(testRepo.path, 'custom');
      expect(socialResult.data).toContain('social/lists/social-list');
      expect(socialResult.data).not.toContain('custom/lists/custom-list');
      expect(customResult.data).toContain('custom/lists/custom-list');
      expect(customResult.data).not.toContain('social/lists/social-list');
    });

    it('should not include deleted lists', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list1', { data: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'list2', { data: 2 });
      await gitMsgList.delete(testRepo.path, 'social', 'list1');
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(true);
      expect(result.data).not.toContain('social/lists/list1');
      expect(result.data).toContain('social/lists/list2');
    });

    it('should handle list names with special characters', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list-with-dashes', { data: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'list_with_underscores', { data: 2 });
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(true);
      expect(result.data).toContain('social/lists/list-with-dashes');
      expect(result.data).toContain('social/lists/list_with_underscores');
    });
  });

  describe('getHistory()', () => {
    beforeEach(async () => {
      await gitMsgList.write(testRepo.path, 'social', 'history', { version: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'history', { version: 2 });
      await gitMsgList.write(testRepo.path, 'social', 'history', { version: 3 });
    });

    it('should get commit history', async () => {
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(3);
    });

    it('should have commits in reverse chronological order', async () => {
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.[0]?.content).toEqual({ version: 3 });
      expect(result.data?.[1]?.content).toEqual({ version: 2 });
      expect(result.data?.[2]?.content).toEqual({ version: 1 });
    });

    it('should include commit metadata', async () => {
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path);
      expect(result.success).toBe(true);
      const commit = result.data?.[0];
      expect(commit?.hash).toBeDefined();
      expect(commit?.hash.length).toBeGreaterThan(0);
      expect(commit?.author).toBe('Test User');
      expect(commit?.email).toBe('test@example.com');
      expect(commit?.timestamp).toBeInstanceOf(Date);
    });

    it('should filter history by since date', async () => {
      const now = new Date();
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path, {
        since: new Date(now.getTime() - 1000)
      });
      expect(result.success).toBe(true);
      expect(result.data?.length).toBeGreaterThanOrEqual(1);
    });

    it('should filter history by until date', async () => {
      const now = new Date();
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path, {
        until: new Date(now.getTime() + 1000)
      });
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(3);
    });

    it('should filter history by date range', async () => {
      const now = new Date();
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'history', testRepo.path, {
        since: new Date(now.getTime() - 2000),
        until: new Date(now.getTime() + 1000)
      });
      expect(result.success).toBe(true);
      expect(result.data?.length).toBeGreaterThanOrEqual(1);
    });

    it('should return empty history for nonexistent list', async () => {
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'nonexistent', testRepo.path);
      expect(result.success).toBe(false);
    });

    it('should handle single commit', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'single', { data: 'test' });
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'single', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      expect(result.data?.[0]?.content).toEqual({ data: 'test' });
    });

    it('should handle malformed JSON in commit message', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const invalidJSON = 'not valid JSON {{{';
      const commitResult = await execGit(testRepo.path, ['commit-tree', EMPTY_TREE, '-m', invalidJSON]);
      const commitHash = commitResult.data?.stdout.trim();
      await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/lists/malformed', commitHash!]);
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'malformed', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      expect(result.data?.[0]?.content).toBe(invalidJSON);
    });

    it('should handle commit with missing fields', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const plainText = 'just plain text, no JSON';
      const commitResult = await execGit(testRepo.path, ['commit-tree', EMPTY_TREE, '-m', plainText]);
      const commitHash = commitResult.data?.stdout.trim();
      await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/lists/plaintext', commitHash!]);
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'plaintext', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      expect(result.data?.[0]?.content).toBe(plainText);
    });
  });

  describe('error handling', () => {
    it('should handle invalid working directory in read()', async () => {
      const result = await gitMsgList.read('/nonexistent/path/that/does/not/exist', 'social', 'list');
      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
    });

    it('should handle invalid working directory in write()', async () => {
      const result = await gitMsgList.write('/nonexistent/path/that/does/not/exist', 'social', 'list', { data: 1 });
      expect(result.success).toBe(false);
      expect(result.error?.code).toMatch(/COMMIT_ERROR|WRITE_ERROR/);
    });

    it('should handle invalid working directory in delete()', async () => {
      const result = await gitMsgList.delete('/nonexistent/path/that/does/not/exist', 'social', 'list');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('DELETE_ERROR');
    });

    it('should handle invalid working directory in enumerate()', async () => {
      const result = await gitMsgList.enumerate('/nonexistent/path/that/does/not/exist', 'social');
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle invalid working directory in getHistory()', async () => {
      const result = await gitMsgList.getHistory('/nonexistent/path/that/does/not/exist', 'social', 'list', testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_ERROR');
    });

    it('should return null when commit message is not valid JSON in read()', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const invalidJSON = 'not { valid JSON';
      const commitResult = await execGit(testRepo.path, ['commit-tree', EMPTY_TREE, '-m', invalidJSON]);
      const commitHash = commitResult.data?.stdout.trim();
      await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/lists/invalid-json', commitHash!]);
      const result = await gitMsgList.read(testRepo.path, 'social', 'invalid-json');
      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
    });

    it('should handle very large list data', async () => {
      const largeData = {
        items: Array.from({ length: 1000 }, (_, i) => `item${i}`),
        metadata: 'x'.repeat(10000)
      };
      const writeResult = await gitMsgList.write(testRepo.path, 'social', 'large', largeData);
      expect(writeResult.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'large');
      expect(readResult.success).toBe(true);
      expect(readResult.data).toEqual(largeData);
    });

    it('should handle special characters in extension name', async () => {
      const result = await gitMsgList.write(testRepo.path, 'ext-with-dash', 'list', { data: 1 });
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'ext-with-dash', 'list');
      expect(readResult.data).toEqual({ data: 1 });
    });

    it('should handle special characters in list name', async () => {
      const result = await gitMsgList.write(testRepo.path, 'social', 'list_123-test', { data: 1 });
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'list_123-test');
      expect(readResult.data).toEqual({ data: 1 });
    });

    it('should handle empty string values', async () => {
      const data = { name: '', value: '', empty: '' };
      const result = await gitMsgList.write(testRepo.path, 'social', 'empty-strings', data);
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'empty-strings');
      expect(readResult.data).toEqual(data);
    });

    it('should handle boolean values', async () => {
      const data = { enabled: true, disabled: false };
      const result = await gitMsgList.write(testRepo.path, 'social', 'booleans', data);
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'booleans');
      expect(readResult.data).toEqual(data);
    });

    it('should handle numeric values including zero', async () => {
      const data = { zero: 0, negative: -42, float: 3.14, large: 999999999 };
      const result = await gitMsgList.write(testRepo.path, 'social', 'numbers', data);
      expect(result.success).toBe(true);
      const readResult = await gitMsgList.read(testRepo.path, 'social', 'numbers');
      expect(readResult.data).toEqual(data);
    });

    it('should handle date range filtering', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'dated', { version: 1 });
      const now = new Date();
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'dated', testRepo.path, {
        since: new Date(now.getTime() - 1000),
        until: new Date(now.getTime() + 1000)
      });
      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should handle reading from non-workspace repository with remote refs', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'remote-test', { data: 'test' });
      const otherRepo = await createTestRepo('other-repo');
      await createCommit(otherRepo.path, 'Initial', { allowEmpty: true });
      await execGit(otherRepo.path, ['remote', 'add', 'upstream', testRepo.path]);
      await execGit(otherRepo.path, ['fetch', 'upstream', 'refs/gitmsg/social/lists/remote-test:refs/remotes/upstream/gitmsg/social/lists/remote-test']);
      const result = await gitMsgList.getHistory(otherRepo.path, 'social', 'remote-test', testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      otherRepo.cleanup();
    });
  });

  describe('integration scenarios', () => {
    it('should handle complete lifecycle', async () => {
      const listData1 = { items: ['repo1'] };
      const listData2 = { items: ['repo1', 'repo2'] };
      const writeResult1 = await gitMsgList.write(testRepo.path, 'social', 'lifecycle', listData1);
      expect(writeResult1.success).toBe(true);
      const readResult1 = await gitMsgList.read(testRepo.path, 'social', 'lifecycle');
      expect(readResult1.data).toEqual(listData1);
      const writeResult2 = await gitMsgList.write(testRepo.path, 'social', 'lifecycle', listData2);
      expect(writeResult2.success).toBe(true);
      const readResult2 = await gitMsgList.read(testRepo.path, 'social', 'lifecycle');
      expect(readResult2.data).toEqual(listData2);
      const history = await gitMsgList.getHistory(testRepo.path, 'social', 'lifecycle', testRepo.path);
      expect(history.data?.length).toBe(2);
      const deleteResult = await gitMsgList.delete(testRepo.path, 'social', 'lifecycle');
      expect(deleteResult.success).toBe(true);
      const readResult3 = await gitMsgList.read(testRepo.path, 'social', 'lifecycle');
      expect(readResult3.data).toBeNull();
    });

    it('should handle multiple lists across extensions', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'list1', { ext: 'social', id: 1 });
      await gitMsgList.write(testRepo.path, 'social', 'list2', { ext: 'social', id: 2 });
      await gitMsgList.write(testRepo.path, 'custom', 'list1', { ext: 'custom', id: 1 });
      const socialLists = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(socialLists.data?.length).toBe(2);
      const customLists = await gitMsgList.enumerate(testRepo.path, 'custom');
      expect(customLists.data?.length).toBe(1);
      const social1 = await gitMsgList.read(testRepo.path, 'social', 'list1');
      const custom1 = await gitMsgList.read(testRepo.path, 'custom', 'list1');
      expect(social1.data).toEqual({ ext: 'social', id: 1 });
      expect(custom1.data).toEqual({ ext: 'custom', id: 1 });
    });

    it('should maintain isolation between extensions', async () => {
      await gitMsgList.write(testRepo.path, 'ext1', 'shared', { owner: 'ext1' });
      await gitMsgList.write(testRepo.path, 'ext2', 'shared', { owner: 'ext2' });
      await gitMsgList.delete(testRepo.path, 'ext1', 'shared');
      const ext1Result = await gitMsgList.read(testRepo.path, 'ext1', 'shared');
      const ext2Result = await gitMsgList.read(testRepo.path, 'ext2', 'shared');
      expect(ext1Result.data).toBeNull();
      expect(ext2Result.data).toEqual({ owner: 'ext2' });
    });
  });

  describe('exception handling coverage', () => {
    it('should handle exception thrown in read()', async () => {
      const spy = vi.spyOn(operationsModule, 'readGitRef');
      spy.mockImplementation(() => {
        throw new Error('Unexpected read error');
      });
      const result = await gitMsgList.read(testRepo.path, 'social', 'test');
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('read list');
      spy.mockRestore();
    });

    it('should handle exception thrown in write()', async () => {
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(() => {
        throw new Error('Unexpected write error');
      });
      const result = await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('write list');
      spy.mockRestore();
    });

    it('should handle exception thrown in delete()', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(() => {
        throw new Error('Unexpected delete error');
      });
      const result = await gitMsgList.delete(testRepo.path, 'social', 'test');
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('delete list');
      spy.mockRestore();
    });

    it('should handle exception thrown in enumerate()', async () => {
      const spy = vi.spyOn(operationsModule, 'listRefs');
      spy.mockImplementation(() => {
        throw new Error('Unexpected enumerate error');
      });
      const result = await gitMsgList.enumerate(testRepo.path, 'social');
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('enumerate lists');
      spy.mockRestore();
    });

    it('should handle exception thrown in getHistory()', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(() => {
        throw new Error('Unexpected history error');
      });
      const result = await gitMsgList.getHistory(testRepo.path, 'social', 'test', testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('get list history');
      spy.mockRestore();
    });
  });

  describe('edge case coverage', () => {
    it('should handle read() when commit exists but getCommit returns null', async () => {
      await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      const spy = vi.spyOn(operationsModule, 'getCommit');
      spy.mockResolvedValueOnce(null);
      const result = await gitMsgList.read(testRepo.path, 'social', 'test');
      expect(result.success).toBe(true);
      expect(result.data).toBeNull();
      spy.mockRestore();
    });

    it('should handle write() when ref update fails after successful commit', async () => {
      const writeGitRefSpy = vi.spyOn(operationsModule, 'writeGitRef');
      writeGitRefSpy.mockResolvedValueOnce({
        success: false,
        error: { code: 'REF_ERROR', message: 'Failed to update ref' }
      });
      const result = await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('update ref');
      writeGitRefSpy.mockRestore();
    });

    it('should handle cascading failures during write', async () => {
      const execGitSpy = vi.spyOn(execModule, 'execGit');
      let callCount = 0;
      execGitSpy.mockImplementation((_workdir, _args) => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve({
            success: true,
            data: { stdout: 'abc123', stderr: '', exitCode: 0 }
          });
        }
        return Promise.resolve({
          success: false,
          error: { code: 'ERROR', message: 'Update ref failed' }
        });
      });
      const result = await gitMsgList.write(testRepo.path, 'social', 'test', { data: 1 });
      expect(result.success).toBe(false);
      execGitSpy.mockRestore();
    });
  });
});
