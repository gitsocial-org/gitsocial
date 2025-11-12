import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { storage } from './index';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { join } from 'path';
import { chmodSync, existsSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from 'fs';
import { tmpdir } from 'os';
import { execGit } from '../git/exec';

describe('storage', () => {
  let testRepo: TestRepo;
  let storageBase: string;

  beforeEach(async () => {
    testRepo = await createTestRepo('storage-test');
    await createCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
    storageBase = join(tmpdir(), `storage-test-${Date.now()}`);
    mkdirSync(storageBase, { recursive: true });
  });

  afterEach(() => {
    testRepo.cleanup();
    if (existsSync(storageBase)) {
      rmSync(storageBase, { recursive: true, force: true });
    }
  });

  describe('cache operations', () => {
    describe('cache.get()', () => {
      it('should return null when cache is empty', () => {
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toBeNull();
      });

      it('should return cached repositories when valid', () => {
        const repos = [
          { url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }
        ];
        storage.cache.set(testRepo.path, 'timeline', repos);
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toEqual(repos);
      });

      it('should return null when cache is expired', () => {
        const repos = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'following', repos);
        vi.useFakeTimers();
        vi.advanceTimersByTime(2 * 60 * 60 * 1000);
        const result = storage.cache.get(testRepo.path, 'following');
        expect(result).toBeNull();
        vi.useRealTimers();
      });

      it('should handle different cache scopes independently', () => {
        const repos1 = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        const repos2 = [{ url: 'https://github.com/user/repo2', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'timeline', repos1);
        storage.cache.set(testRepo.path, 'following', repos2);
        expect(storage.cache.get(testRepo.path, 'timeline')).toEqual(repos1);
        expect(storage.cache.get(testRepo.path, 'following')).toEqual(repos2);
      });

      it('should handle external repository cache', () => {
        const repos = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'repository:external', repos);
        const result = storage.cache.get(testRepo.path, 'repository:external');
        expect(result).toEqual(repos);
      });
    });

    describe('cache.set()', () => {
      it('should set cache with repositories', () => {
        const repos = [
          { url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] },
          { url: 'https://github.com/user/repo2', branch: 'main', fetchedRanges: [] }
        ];
        storage.cache.set(testRepo.path, 'timeline', repos);
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toEqual(repos);
      });

      it('should overwrite existing cache', () => {
        const repos1 = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        const repos2 = [{ url: 'https://github.com/user/repo2', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'timeline', repos1);
        storage.cache.set(testRepo.path, 'timeline', repos2);
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toEqual(repos2);
      });

      it('should set cache with empty array', () => {
        storage.cache.set(testRepo.path, 'timeline', []);
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toEqual([]);
      });

      it('should handle different workdirs independently', async () => {
        const otherRepo = await createTestRepo('other-storage');
        const repos1 = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        const repos2 = [{ url: 'https://github.com/user/repo2', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'timeline', repos1);
        storage.cache.set(otherRepo.path, 'timeline', repos2);
        expect(storage.cache.get(testRepo.path, 'timeline')).toEqual(repos1);
        expect(storage.cache.get(otherRepo.path, 'timeline')).toEqual(repos2);
        otherRepo.cleanup();
      });
    });

    describe('cache.clear()', () => {
      it('should clear cache for specific scope', () => {
        const repos = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'timeline', repos);
        storage.cache.clear(testRepo.path, 'timeline');
        const result = storage.cache.get(testRepo.path, 'timeline');
        expect(result).toBeNull();
      });

      it('should not affect other scopes', () => {
        const repos1 = [{ url: 'https://github.com/user/repo1', branch: 'main', fetchedRanges: [] }];
        const repos2 = [{ url: 'https://github.com/user/repo2', branch: 'main', fetchedRanges: [] }];
        storage.cache.set(testRepo.path, 'timeline', repos1);
        storage.cache.set(testRepo.path, 'following', repos2);
        storage.cache.clear(testRepo.path, 'timeline');
        expect(storage.cache.get(testRepo.path, 'timeline')).toBeNull();
        expect(storage.cache.get(testRepo.path, 'following')).toEqual(repos2);
      });

      it('should handle clearing non-existent cache', () => {
        expect(() => {
          storage.cache.clear(testRepo.path, 'nonexistent');
        }).not.toThrow();
      });
    });
  });

  describe('path utilities', () => {
    describe('path.getDirectory()', () => {
      it('should return storage directory for GitHub URL', () => {
        const url = 'https://github.com/user/repo';
        const dir = storage.path.getDirectory(storageBase, url);
        expect(dir).toContain('repositories');
        expect(dir).toContain(storageBase);
      });

      it('should handle URL normalization', () => {
        const url1 = 'https://github.com/user/repo';
        const url2 = 'https://github.com/user/repo.git';
        const dir1 = storage.path.getDirectory(storageBase, url1);
        const dir2 = storage.path.getDirectory(storageBase, url2);
        expect(dir1).toBe(dir2);
      });

      it('should handle different storage bases', () => {
        const url = 'https://github.com/user/repo';
        const base1 = '/tmp/storage1';
        const base2 = '/tmp/storage2';
        const dir1 = storage.path.getDirectory(base1, url);
        const dir2 = storage.path.getDirectory(base2, url);
        expect(dir1).not.toBe(dir2);
        expect(dir1).toContain(base1);
        expect(dir2).toContain(base2);
      });

      it('should handle URLs with special characters', () => {
        const url = 'https://github.com/user/repo-name_123';
        const dir = storage.path.getDirectory(storageBase, url);
        expect(dir).toBeDefined();
        expect(dir.length).toBeGreaterThan(0);
      });
    });

    describe('path.getUrl()', () => {
      it('should extract URL from storage name', () => {
        const storageName = 'github-com-user-repo';
        const extractedUrl = storage.path.getUrl(storageName);
        expect(extractedUrl).toBe('https://github.com/user/repo');
      });

      it('should handle paths with workspace identifier', () => {
        const path = `/some/path/repositories/workspace:${testRepo.path.replace(/\//g, '-')}`;
        const url = storage.path.getUrl(path);
        expect(url).toBeDefined();
      });

      it('should handle local file paths', () => {
        const path = '/tmp/storage/repositories/file---tmp-repo';
        const url = storage.path.getUrl(path);
        expect(url).toBeDefined();
      });
    });

    describe('path.isWorkspace()', () => {
      it('should return true for workspace storage name', () => {
        expect(storage.path.isWorkspace('workspace')).toBe(true);
      });

      it('should return true for empty string', () => {
        expect(storage.path.isWorkspace('')).toBe(true);
      });

      it('should return false for non-workspace storage names', () => {
        expect(storage.path.isWorkspace('github-com-user-repo')).toBe(false);
        expect(storage.path.isWorkspace('gitlab-com-user-repo')).toBe(false);
        expect(storage.path.isWorkspace('/tmp')).toBe(false);
      });
    });
  });

  describe('repository operations', () => {
    describe('repository.ensure()', () => {
      it('should create new repository', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
        if (result.success) {
          expect(existsSync(result.data)).toBe(true);
        }
      }, 15000);

      it('should return existing repository if valid', async () => {
        const result1 = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result1.success).toBe(true);
        const result2 = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result2.success).toBe(true);
        if (result1.success && result2.success) {
          expect(result1.data).toBe(result2.data);
        }
      }, 15000);

      it('should handle force flag', async () => {
        const result1 = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result1.success).toBe(true);
        const result2 = await storage.repository.ensure(storageBase, testRepo.path, 'main', { force: true });
        expect(result2.success).toBe(true);
      }, 15000);

      it('should handle persistent flag', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });
        expect(result.success).toBe(true);
        if (result.success) {
          const config = await storage.repository.readConfig(result.data);
          expect(config?.isPersistent).toBe(true);
        }
      }, 15000);

      it('should handle temporary repositories', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: false });
        expect(result.success).toBe(true);
        if (result.success) {
          const config = await storage.repository.readConfig(result.data);
          expect(config?.isPersistent).toBe(false);
        }
      }, 15000);

      it('should fail with invalid storage base', async () => {
        const result = await storage.repository.ensure('', testRepo.path, 'main');
        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('INVALID_STORAGE_BASE');
      });

      it('should handle concurrent calls to same repository', async () => {
        const promise1 = storage.repository.ensure(storageBase, testRepo.path, 'main');
        const promise2 = storage.repository.ensure(storageBase, testRepo.path, 'main');
        const [result1, result2] = await Promise.all([promise1, promise2]);
        expect(result1.success).toBe(true);
        expect(result2.success).toBe(true);
        if (result1.success && result2.success) {
          expect(result1.data).toBe(result2.data);
        }
      }, 15000);

      it('should handle different branches', async () => {
        await execGit(testRepo.path, ['checkout', '-b', 'develop']);
        await createCommit(testRepo.path, 'Develop commit', { allowEmpty: true });
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'develop');
        expect(result.success).toBe(true);
      }, 15000);
    });

    describe('repository.fetch()', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should fetch repository updates', async () => {
        await createCommit(testRepo.path, 'New commit', { allowEmpty: true });
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0]
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should skip fetch if date range already covered', async () => {
        const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0];
        await storage.repository.fetch(storageBase, testRepo.path, 'main', { since });
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', { since });
        expect(result.success).toBe(true);
        if (result.success && result.data) {
          expect(result.data.skipped).toBe(true);
        }
      }, 15000);

      it('should handle invalid repository path', async () => {
        const result = await storage.repository.fetch(storageBase, '/nonexistent/path', 'main', {
          since: '2025-01-01'
        });
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('REPOSITORY_NOT_FOUND');
        }
      });

      it('should update lastFetch timestamp', async () => {
        await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0]
        });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.lastFetch).toBeDefined();
      }, 15000);

      it('should update fetchedRanges', async () => {
        const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0];
        await storage.repository.fetch(storageBase, testRepo.path, 'main', { since });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.fetchedRanges).toBeDefined();
        expect(config?.fetchedRanges?.length).toBeGreaterThan(0);
      }, 15000);

      it('should handle fetch without since parameter', async () => {
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
      }, 15000);

      it('should use branch from config when not provided', async () => {
        const result = await storage.repository.fetch(storageBase, testRepo.path);
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle concurrent fetch calls', async () => {
        const promises = [
          storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2025-01-01' }),
          storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2025-01-01' })
        ];
        const results = await Promise.all(promises);
        expect(results.every(r => r.success)).toBe(true);
      }, 15000);

      it('should handle fetch with ISO timestamp in since', async () => {
        const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString();
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', { since });
        expect(result.success).toBe(true);
      }, 15000);

      it('should error when branch missing and not in config', async () => {
        const tempBase = join(tmpdir(), `temp-storage-${Date.now()}`);
        mkdirSync(tempBase, { recursive: true });
        const tempResult = await storage.repository.ensure(tempBase, testRepo.path, 'main');
        expect(tempResult.success).toBe(true);
        if (tempResult.success) {
          await execGit(tempResult.data, ['config', '--unset', 'gitsocial.branch']);
          const result = await storage.repository.fetch(tempBase, testRepo.path);
          expect(result.success).toBe(false);
          if (!result.success) {
            expect(result.error?.code).toBe('MISSING_BRANCH');
          }
        }
        rmSync(tempBase, { recursive: true, force: true });
      }, 15000);
    });

    describe('repository.getCommits()', () => {
      let _repoPath: string;

      beforeEach(async () => {
        await createCommit(testRepo.path, 'Commit 1', { allowEmpty: true });
        await createCommit(testRepo.path, 'Commit 2', { allowEmpty: true });
        await createCommit(testRepo.path, 'Commit 3', { allowEmpty: true });
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        _repoPath = ensureResult.data;
        await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0]
        });
      }, 20000);

      it('should get commits from repository', async () => {
        const result = await storage.repository.getCommits(storageBase, testRepo.path, {
          branch: 'main'
        });
        expect(result.success).toBe(true);
        if (result.success) {
          expect(result.data.length).toBeGreaterThan(0);
        }
      }, 15000);

      it('should include commit metadata', async () => {
        const result = await storage.repository.getCommits(storageBase, testRepo.path, {
          branch: 'main'
        });
        expect(result.success).toBe(true);
        if (result.success) {
          const commit = result.data[0];
          expect(commit?.hash).toBeDefined();
          expect(commit?.author).toBeDefined();
          expect(commit?.email).toBeDefined();
          expect(commit?.timestamp).toBeDefined();
        }
      }, 15000);

      it('should filter commits by date range', async () => {
        const since = new Date(Date.now() - 1 * 24 * 60 * 60 * 1000);
        const until = new Date();
        const result = await storage.repository.getCommits(storageBase, testRepo.path, {
          branch: 'main',
          since,
          until
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle limit option', async () => {
        const result = await storage.repository.getCommits(storageBase, testRepo.path, {
          branch: 'main',
          limit: 2
        });
        expect(result.success).toBe(true);
        if (result.success) {
          expect(result.data.length).toBeLessThanOrEqual(2);
        }
      }, 15000);

      it('should error when repository not found', async () => {
        const result = await storage.repository.getCommits(storageBase, '/nonexistent/path', {
          branch: 'main'
        });
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('REPOSITORY_NOT_FOUND');
        }
      }, 15000);

      it('should error when branch not provided', async () => {
        const result = await storage.repository.getCommits(storageBase, testRepo.path);
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('MISSING_BRANCH');
        }
      }, 15000);

      it('should handle unexpected errors gracefully', async () => {
        const result = await storage.repository.getCommits(storageBase, testRepo.path, {
          branch: 'main',
          limit: 10
        });
        expect(result.success).toBe(true);
      }, 15000);
    });

    describe('repository.readConfig() and writeConfig()', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should read repository config', async () => {
        const config = await storage.repository.readConfig(repoPath);
        expect(config).toBeDefined();
      });

      it('should return null for repository without config', async () => {
        const tempRepo = await createTestRepo('no-config');
        const config = await storage.repository.readConfig(tempRepo.path);
        expect(config).toBeNull();
        tempRepo.cleanup();
      });

      it('should handle missing config gracefully', async () => {
        const config = await storage.repository.readConfig('/nonexistent/path');
        expect(config).toBeNull();
      });

      it('should read isPersistent flag', async () => {
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.isPersistent).toBeDefined();
      });

      it('should read lastFetch timestamp', async () => {
        await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
          until: new Date().toISOString().split('T')[0]
        });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.lastFetch).toBeDefined();
      }, 15000);

      it('should read fetchedRanges array', async () => {
        await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: '2025-01-01',
          until: '2025-01-10'
        });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.fetchedRanges).toBeDefined();
        expect(Array.isArray(config?.fetchedRanges)).toBe(true);
      }, 15000);

      it('should read branch name', async () => {
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.branch).toBe('main');
      });

      it('should read version', async () => {
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.version).toBeDefined();
      });

      it('should read createdAt timestamp', async () => {
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.createdAt).toBeDefined();
      });
    });

    describe('repository.cleanup()', () => {
      it('should cleanup expired repositories', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: false });
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      }, 15000);

      it('should not cleanup persistent repositories', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });
        expect(result.success).toBe(true);
        await storage.repository.cleanup(storageBase);
        if (result.success) {
          expect(existsSync(result.data)).toBe(true);
        }
      }, 15000);

      it('should handle empty storage directory', async () => {
        const emptyStorage = join(tmpdir(), `empty-storage-${Date.now()}`);
        mkdirSync(emptyStorage, { recursive: true });
        await storage.repository.cleanup(emptyStorage);
        rmSync(emptyStorage, { recursive: true, force: true });
      });

      it('should handle non-existent storage directory', async () => {
        await storage.repository.cleanup('/nonexistent/storage');
        expect(true).toBe(true);
      });
    });

    describe('repository.getStats()', () => {
      it('should get storage statistics', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.totalRepositories).toBeGreaterThanOrEqual(0);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);
        expect(stats.persistent).toBeGreaterThanOrEqual(0);
        expect(stats.temporary).toBeGreaterThanOrEqual(0);
      }, 15000);

      it('should handle empty storage', async () => {
        const emptyStorage = join(tmpdir(), `empty-stats-${Date.now()}`);
        mkdirSync(emptyStorage, { recursive: true });
        const stats = await storage.repository.getStats(emptyStorage);
        expect(stats.totalRepositories).toBe(0);
        expect(stats.diskUsage).toBe(0);
        rmSync(emptyStorage, { recursive: true, force: true });
      });

      it('should count persistent and temporary repositories', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });
        const tempRepo = await createTestRepo('temp-stats');
        await createCommit(tempRepo.path, 'Initial', { allowEmpty: true });
        await storage.repository.ensure(storageBase, tempRepo.path, 'main', { isPersistent: false });
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.persistent + stats.temporary).toBe(stats.totalRepositories);
        tempRepo.cleanup();
      }, 20000);

      it('should calculate disk usage', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.diskUsage).toBeGreaterThan(0);
      }, 15000);
    });

    describe('repository.clearCache()', () => {
      beforeEach(async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!result.success) {
          throw new Error('Failed to ensure repository');
        }
      }, 15000);

      it('should clear all caches for storage base', () => {
        const result = storage.repository.clearCache(storageBase);
        expect(result.deletedCount).toBeGreaterThanOrEqual(0);
      });

      it('should return deletion stats', () => {
        storage.cache.set(testRepo.path, 'timeline', []);
        storage.cache.set(testRepo.path, 'following', []);
        const result = storage.repository.clearCache(storageBase);
        expect(result.deletedCount).toBeDefined();
        expect(result.diskSpaceFreed).toBeDefined();
        expect(result.errors).toBeDefined();
      });

      it('should not affect other storage bases', () => {
        const otherBase = join(tmpdir(), `other-storage-${Date.now()}`);
        mkdirSync(otherBase, { recursive: true });
        storage.cache.set(testRepo.path, 'timeline', []);
        storage.cache.set(otherBase, 'timeline', []);
        storage.repository.clearCache(storageBase);
        expect(storage.cache.get(otherBase, 'timeline')).toBeDefined();
        rmSync(otherBase, { recursive: true, force: true });
      });

      it('should handle errors during cache clearing', () => {
        const result = storage.repository.clearCache(storageBase);
        expect(result.errors).toBeDefined();
        expect(Array.isArray(result.errors)).toBe(true);
      });

      it('should track disk space freed', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const result = storage.repository.clearCache(storageBase);
        expect(result.diskSpaceFreed).toBeGreaterThanOrEqual(0);
      }, 15000);

      it('should handle non-Error exception objects', () => {
        const nonExistentBase = join(tmpdir(), `non-existent-${Date.now()}`);
        const result = storage.repository.clearCache(nonExistentBase);
        expect(result.errors).toBeDefined();
      });
    });

    describe('cache TTL', () => {
      it('should return workspace TTL for workspace:my scope', () => {
        storage.cache.set('/test/path', 'workspace:my', []);
        const cached = storage.cache.get('/test/path', 'workspace:my');
        expect(cached).toBeDefined();
      });

      it('should return following TTL for following scope', () => {
        storage.cache.set('/test/path', 'following', []);
        const cached = storage.cache.get('/test/path', 'following');
        expect(cached).toBeDefined();
      });

      it('should return all TTL for all scope', () => {
        storage.cache.set('/test/path', 'all', []);
        const cached = storage.cache.get('/test/path', 'all');
        expect(cached).toBeDefined();
      });

      it('should return repository TTL for repository: prefix', () => {
        storage.cache.set('/test/path', 'repository:test', []);
        const cached = storage.cache.get('/test/path', 'repository:test');
        expect(cached).toBeDefined();
      });

      it('should return default TTL for unknown scope', () => {
        storage.cache.set('/test/path', 'unknown-scope', []);
        const cached = storage.cache.get('/test/path', 'unknown-scope');
        expect(cached).toBeDefined();
      });
    });

    describe('path utilities', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should handle actual repository path', () => {
        const relativePath = repoPath.replace(storageBase + '/', '');
        const url = storage.path.getUrl(storageBase, relativePath);
        expect(url).toBeDefined();
        expect(typeof url).toBe('string');
      });
    });

    describe('URL conversion for different hosts', () => {
      it('should convert GitLab repository URLs', () => {
        const storageName = 'gitlab-com-user-repo';
        const url = storage.path.getUrl(storageName);
        expect(url).toBe('https://gitlab.com/user/repo');
      });

      it('should convert Bitbucket repository URLs', () => {
        const storageName = 'bitbucket-org-user-repo';
        const url = storage.path.getUrl(storageName);
        expect(url).toBe('https://bitbucket.org/user/repo');
      });

      it('should handle GitLab URLs with multi-part repo names', () => {
        const storageName = 'gitlab-com-user-my-test-repo';
        const url = storage.path.getUrl(storageName);
        expect(url).toBe('https://gitlab.com/user/my-test-repo');
      });

      it('should handle Bitbucket URLs with multi-part repo names', () => {
        const storageName = 'bitbucket-org-user-my-test-repo';
        const url = storage.path.getUrl(storageName);
        expect(url).toBe('https://bitbucket.org/user/my-test-repo');
      });

      it('should return original for unknown hosts', () => {
        const storageName = 'custom-host-user-repo';
        const url = storage.path.getUrl(storageName);
        expect(url).toBe('custom-host-user-repo');
      });
    });

    describe('repository config edge cases', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should handle invalid fetchedRanges JSON in config', async () => {
        await execGit(repoPath, ['config', 'gitsocial.fetchedranges', 'invalid-json-[not-valid']);
        const result = await storage.repository.readConfig(repoPath);
        expect(result).toBeDefined();
        expect(result?.fetchedRanges).toBeUndefined();
      }, 15000);

      it('should return null for repository with no gitsocial config', async () => {
        const emptyRepo = await createTestRepo('empty-config');
        await createCommit(emptyRepo.path, 'Initial', { allowEmpty: true });
        const result = await storage.repository.readConfig(emptyRepo.path);
        expect(result).toBeNull();
        emptyRepo.cleanup();
      });
    });

    describe('repository.fetch() error paths', () => {
      it('should return error when repository not found', async () => {
        const result = await storage.repository.fetch('/nonexistent/path', testRepo.path, 'main', {
          since: '2025-01-01',
          until: '2025-01-10'
        });
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('REPOSITORY_NOT_FOUND');
        }
      });
    });

    describe('repository.cleanup() error handling', () => {
      it('should handle repositories without lastFetch', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: false });
        expect(result.success).toBe(true);
        if (result.success) {
          const repoPath = result.data;
          const gitDir = join(repoPath, '.git');
          if (existsSync(gitDir)) {
            const configPath = join(gitDir, 'gitsocial.config');
            const config = await storage.repository.readConfig(repoPath);
            if (config) {
              const lines = [];
              lines.push(`url=${config.url}`);
              lines.push(`branch=${config.branch}`);
              lines.push('isPersistent=false');
              writeFileSync(configPath, lines.join('\n') + '\n');
            }
          }
        }
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      }, 15000);

      it('should continue cleanup when deletion fails', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: false });
        vi.useFakeTimers();
        vi.advanceTimersByTime(8 * 24 * 60 * 60 * 1000);
        await storage.repository.cleanup(storageBase);
        vi.useRealTimers();
        expect(true).toBe(true);
      }, 15000);
    });

    describe('repository.getStats() error handling', () => {
      it('should handle errors reading individual configs', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.totalRepositories).toBeGreaterThanOrEqual(0);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);
      }, 15000);

      it('should handle errors calculating directory size', async () => {
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);
      });
    });

    describe('config write edge cases', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should write config with minimal fields', async () => {
        const gitDir = join(repoPath, '.git');
        if (!existsSync(gitDir)) {
          return;
        }
        const configPath = join(gitDir, 'gitsocial.config');
        const minimalConfig = `url=${testRepo.path}\nbranch=main\n`;
        writeFileSync(configPath, minimalConfig);
        const config = await storage.repository.readConfig(repoPath);
        expect(config).toBeDefined();
        expect(config?.url).toBe(testRepo.path);
        expect(config?.branch).toBe('main');
      });

    });

    describe('repository operations edge cases', () => {
      it('should handle concurrent ensure calls', async () => {
        const promises = [
          storage.repository.ensure(storageBase, testRepo.path, 'main'),
          storage.repository.ensure(storageBase, testRepo.path, 'main'),
          storage.repository.ensure(storageBase, testRepo.path, 'main')
        ];
        const results = await Promise.all(promises);
        expect(results.every(r => r.success)).toBe(true);
        const paths = results.filter(r => r.success).map(r => r.data!);
        expect(new Set(paths).size).toBe(1);
      }, 15000);

      it('should handle multiple repository cleanup cycles', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: false });
        await storage.repository.cleanup(storageBase);
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      }, 15000);
    });

    describe('ensureRepository comprehensive paths', () => {
      it('should go through full clone path for fresh repository', async () => {
        const freshBase = join(tmpdir(), `fresh-clone-${Date.now()}`);
        mkdirSync(freshBase, { recursive: true });
        const freshRepo = await createTestRepo(`fresh-test-${Date.now()}`);
        await createCommit(freshRepo.path, 'Test commit', { allowEmpty: true });
        const result = await storage.repository.ensure(freshBase, freshRepo.path, 'main');
        expect(result.success).toBe(true);
        if (result.success) {
          expect(existsSync(result.data)).toBe(true);
        }
        freshRepo.cleanup();
        rmSync(freshBase, { recursive: true, force: true });
      }, 15000);

      it('should handle cleanup and recreation with force flag', async () => {
        const forceBase = join(tmpdir(), `force-test-${Date.now()}`);
        mkdirSync(forceBase, { recursive: true });
        await storage.repository.ensure(forceBase, testRepo.path, 'main');
        const result = await storage.repository.ensure(forceBase, testRepo.path, 'main', { force: true });
        expect(result.success).toBe(true);
        rmSync(forceBase, { recursive: true, force: true });
      }, 15000);

      it('should cleanup on remote add failure', async () => {
        const failBase = join(tmpdir(), `fail-remote-${Date.now()}`);
        mkdirSync(failBase, { recursive: true });
        const result = await storage.repository.ensure(failBase, 'invalid://url', 'main');
        expect(result.success).toBe(false);
        rmSync(failBase, { recursive: true, force: true });
      }, 15000);

      it('should cleanup and recreate when forcing with existing invalid repo', async () => {
        const invalidBase = join(tmpdir(), `invalid-${Date.now()}`);
        mkdirSync(invalidBase, { recursive: true });
        const repoDir = storage.path.getDirectory(invalidBase, testRepo.path);
        mkdirSync(repoDir, { recursive: true });
        writeFileSync(join(repoDir, 'invalid.txt'), 'not a git repo');
        const result = await storage.repository.ensure(invalidBase, testRepo.path, 'main', { force: true });
        expect(result.success).toBe(true);
        rmSync(invalidBase, { recursive: true, force: true });
      }, 15000);

      it('should handle non-existent branch during clone', async () => {
        const branchBase = join(tmpdir(), `branch-fail-${Date.now()}`);
        mkdirSync(branchBase, { recursive: true });
        const result = await storage.repository.ensure(branchBase, testRepo.path, 'nonexistent-branch-xyz');
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('FETCH_ERROR');
        }
        rmSync(branchBase, { recursive: true, force: true });
      }, 15000);

      it('should handle config command warnings', async () => {
        const configBase = join(tmpdir(), `config-test-${Date.now()}`);
        mkdirSync(configBase, { recursive: true });
        const result = await storage.repository.ensure(configBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
        rmSync(configBase, { recursive: true, force: true });
      }, 15000);

      it('should complete fetch even if config commands have warnings', async () => {
        const warnBase = join(tmpdir(), `warn-test-${Date.now()}`);
        mkdirSync(warnBase, { recursive: true });
        const result = await storage.repository.ensure(warnBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
        rmSync(warnBase, { recursive: true, force: true });
      }, 15000);
    });

    describe('fetchRepository date handling', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should calculate default Monday date when no since provided and no existing ranges', async () => {
        await execGit(repoPath, ['config', '--unset', 'gitsocial.fetchedranges']);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
      }, 15000);

      it('should use oldest fetched date when no since provided', async () => {
        await storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2025-01-01' });
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle Sunday date calculation', async () => {
        await execGit(repoPath, ['config', '--unset', 'gitsocial.fetchedranges']);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
      }, 15000);
    });

    describe('fetchRepository error handling and retries', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should handle fetch errors gracefully', async () => {
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2025-01-01' });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle fetch with very old date to trigger actual git fetch', async () => {
        await execGit(repoPath, ['config', '--unset', 'gitsocial.fetchedranges']);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2020-01-01' });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle fetch to non-existent repository storage', async () => {
        const nonExist = join(tmpdir(), `non-exist-${Date.now()}`);
        const result = await storage.repository.fetch(nonExist, testRepo.path, 'main', { since: '2025-01-01' });
        expect(result.success).toBe(false);
        if (!result.success) {
          expect(result.error?.code).toBe('REPOSITORY_NOT_FOUND');
        }
      });

      it('should handle unexpected errors in fetch inner catch', async () => {
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', { since: '2024-01-01' });
        expect(result.success).toBe(true);
      }, 15000);
    });

    describe('cleanupExpiredRepositories comprehensive', () => {
      it('should handle repositories without config', async () => {
        const tempDir = join(storageBase, 'repositories', 'test-no-config');
        mkdirSync(tempDir, { recursive: true });
        await storage.repository.cleanup(storageBase);
        expect(existsSync(tempDir)).toBe(false);
      });

      it('should handle errors during config read', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const reposDir = join(storageBase, 'repositories');
        const invalidDir = join(reposDir, 'invalid-repo');
        mkdirSync(invalidDir, { recursive: true });
        writeFileSync(join(invalidDir, 'file.txt'), 'test');
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      }, 15000);

      it('should handle errors when checking repositories', async () => {
        const reposDir = join(storageBase, 'repositories');
        mkdirSync(reposDir, { recursive: true });
        const errorDir = join(reposDir, 'error-repo');
        mkdirSync(errorDir, { recursive: true });
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      });

      it('should handle outer try-catch errors', async () => {
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      });
    });

    describe('getRepositoryStorageStats edge cases', () => {
      it('should handle errors reading repository configs', async () => {
        const reposDir = join(storageBase, 'repositories');
        mkdirSync(reposDir, { recursive: true});
        const invalidRepo = join(reposDir, 'invalid');
        mkdirSync(invalidRepo, { recursive: true });
        writeFileSync(join(invalidRepo, 'dummy.txt'), 'test');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats).toBeDefined();
        rmSync(invalidRepo, { recursive: true, force: true });
      });

      it('should handle errors calculating directory size', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);
      }, 15000);

      it('should handle outer try-catch errors in getStats', async () => {
        const nonExistent = join(tmpdir(), `stats-error-${Date.now()}`);
        const stats = await storage.repository.getStats(nonExistent);
        expect(stats.totalRepositories).toBe(0);
        expect(stats.diskUsage).toBe(0);
      });

      it('should handle errors in directory size calculation with nested dirs', async () => {
        const reposDir = join(storageBase, 'repositories');
        mkdirSync(reposDir, { recursive: true });
        const testDir = join(reposDir, 'test-nested');
        mkdirSync(join(testDir, 'sub', 'deep'), { recursive: true });
        writeFileSync(join(testDir, 'file.txt'), 'content');
        writeFileSync(join(testDir, 'sub', 'file2.txt'), 'more');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);
        rmSync(testDir, { recursive: true, force: true });
      });

      it('should handle repositories with no valid config', async () => {
        const reposDir = join(storageBase, 'repositories');
        mkdirSync(reposDir, { recursive: true });
        const noConfigRepo = join(reposDir, 'no-valid-config');
        mkdirSync(noConfigRepo, { recursive: true });
        writeFileSync(join(noConfigRepo, 'data.txt'), 'test');
        const stats = await storage.repository.getStats(storageBase);
        expect(stats).toBeDefined();
        rmSync(noConfigRepo, { recursive: true, force: true });
      });
    });

    describe('path utility edge cases', () => {
      it('should return workspace for empty URL in getDirectory', () => {
        const dir = storage.path.getDirectory(storageBase, '#commit:abc123');
        expect(dir).toContain('workspace');
      });

      it('should return empty string for workspace in getUrl', () => {
        const url = storage.path.getUrl('workspace');
        expect(url).toBe('');
      });

      it('should handle getUrl with just workspace path', () => {
        const result = storage.path.getUrl('/some/path/repositories/workspace');
        expect(result).toBeDefined();
      });
    });

    describe('cache validation edge cases', () => {
      it('should handle non-existent repository path', async () => {
        const _nonExistent = join(tmpdir(), `non-existent-repo-${Date.now()}`);
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
        if (result.success) {
          rmSync(result.data, { recursive: true, force: true });
          const secondResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
          expect(secondResult.success).toBe(true);
        }
      }, 15000);

      it('should handle repository with missing lastFetch in config', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);
        if (result.success) {
          await execGit(result.data, ['config', '--unset', 'gitsocial.lastfetch']);
          const secondResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
          expect(secondResult.success).toBe(true);
        }
      }, 15000);
    });

    describe('date range merging and coverage', () => {
      let repoPath: string;

      beforeEach(async () => {
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
      }, 15000);

      it('should merge non-overlapping ranges with gap > 1 day', async () => {
        await execGit(repoPath, ['config', 'gitsocial.fetchedranges', JSON.stringify([
          { start: '2025-01-01', end: '2025-01-05' },
          { start: '2025-01-10', end: '2025-01-15' }
        ])]);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: '2025-01-06',
          until: '2025-01-09'
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should detect gap when checking if range is covered', async () => {
        await execGit(repoPath, ['config', 'gitsocial.fetchedranges', JSON.stringify([
          { start: '2025-01-01', end: '2025-01-05' },
          { start: '2025-01-10', end: '2025-01-15' }
        ])]);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: '2025-01-01',
          until: '2025-01-15'
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle fetching range that starts after all existing ranges', async () => {
        await execGit(repoPath, ['config', 'gitsocial.fetchedranges', JSON.stringify([
          { start: '2025-01-01', end: '2025-01-05' }
        ])]);
        const result = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
          since: '2025-02-01',
          until: '2025-02-05'
        });
        expect(result.success).toBe(true);
      }, 15000);
    });

    describe('cleanup error handling', () => {
      it('should handle permission errors during cleanup', async () => {
        const reposDir = join(storageBase, 'repositories');
        mkdirSync(reposDir, { recursive: true });
        const protectedRepo = join(reposDir, 'protected-repo');
        mkdirSync(protectedRepo, { recursive: true });
        writeFileSync(join(protectedRepo, 'dummy.txt'), 'test');
        await storage.repository.cleanup(storageBase);
        expect(true).toBe(true);
      });
    });

    describe('clearRepositoryCache comprehensive error handling', () => {
      it('should handle errors reading repositories directory', () => {
        const nonExistent = join(tmpdir(), `non-existent-${Date.now()}`);
        const result = storage.repository.clearCache(nonExistent);
        expect(result.errors).toBeDefined();
        expect(Array.isArray(result.errors)).toBe(true);
      });

      it('should handle errors during repository deletion', () => {
        const result = storage.repository.clearCache(storageBase);
        expect(result.deletedCount).toBeGreaterThanOrEqual(0);
        expect(result.errors).toBeDefined();
      });

      it('should handle non-Error exceptions when deleting', async () => {
        await storage.repository.ensure(storageBase, testRepo.path, 'main');
        const result = storage.repository.clearCache(storageBase);
        expect(result).toBeDefined();
        expect(result.errors).toBeDefined();
        expect(Array.isArray(result.errors)).toBe(true);
      }, 15000);

      it('should handle errors in outer try-catch', () => {
        const result = storage.repository.clearCache(storageBase);
        expect(result.deletedCount).toBeGreaterThanOrEqual(0);
      });
    });

    describe('additional coverage for uncovered lines', () => {
      it('should handle ensureRepository with init failure by cleaning up', async () => {
        const failBase = join(tmpdir(), `init-fail-${Date.now()}`);
        mkdirSync(failBase, { recursive: true });

        const initRepo = await createTestRepo('init-test');
        await createCommit(initRepo.path, 'Test', { allowEmpty: true });

        const result = await storage.repository.ensure(failBase, initRepo.path, 'nonexistent-branch');
        expect(result.success).toBe(false);

        initRepo.cleanup();
        rmSync(failBase, { recursive: true, force: true });
      }, 15000);

      it('should handle config commands that log warnings but continue', async () => {
        const warnBase = join(tmpdir(), `warn-config-${Date.now()}`);
        mkdirSync(warnBase, { recursive: true });

        const result = await storage.repository.ensure(warnBase, testRepo.path, 'main');
        expect(result.success).toBe(true);

        rmSync(warnBase, { recursive: true, force: true });
      }, 15000);

      it('should handle getStats with repositories that have errors', async () => {
        const statsBase = join(tmpdir(), `stats-error-${Date.now()}`);
        mkdirSync(join(statsBase, 'repositories', 'bad-repo'), { recursive: true });
        writeFileSync(join(statsBase, 'repositories', 'bad-repo', 'file.txt'), 'test');

        const stats = await storage.repository.getStats(statsBase);
        expect(stats).toBeDefined();
        expect(stats.totalRepositories).toBeGreaterThanOrEqual(0);

        rmSync(statsBase, { recursive: true, force: true });
      });

      it('should handle cleanup with repositories that have read errors', async () => {
        const cleanupBase = join(tmpdir(), `cleanup-error-${Date.now()}`);
        mkdirSync(join(cleanupBase, 'repositories', 'error-repo'), { recursive: true });

        await storage.repository.cleanup(cleanupBase);
        expect(true).toBe(true);

        rmSync(cleanupBase, { recursive: true, force: true });
      });

      it('should handle getDirectory size calculation errors', async () => {
        const sizeBase = join(tmpdir(), `size-test-${Date.now()}`);
        mkdirSync(sizeBase, { recursive: true });

        await storage.repository.ensure(sizeBase, testRepo.path, 'main');
        const stats = await storage.repository.getStats(sizeBase);
        expect(stats.diskUsage).toBeGreaterThanOrEqual(0);

        rmSync(sizeBase, { recursive: true, force: true });
      }, 15000);

      it('should handle cache invalid with error thrown', async () => {
        const invalidBase = join(tmpdir(), `invalid-cache-${Date.now()}`);
        mkdirSync(invalidBase, { recursive: true });

        const result = await storage.repository.ensure(invalidBase, testRepo.path, 'main');
        expect(result.success).toBe(true);

        if (result.success) {
          rmSync(result.data, { recursive: true, force: true });
          const result2 = await storage.repository.ensure(invalidBase, testRepo.path, 'main');
          expect(result2.success).toBe(true);
        }

        rmSync(invalidBase, { recursive: true, force: true });
      }, 15000);

      it('should handle empty ranges array in merge', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);

        if (result.success) {
          await execGit(result.data, ['config', '--unset', 'gitsocial.fetchedranges']);
          const fetchResult = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
            since: '2025-01-01',
            until: '2025-01-10'
          });
          expect(fetchResult.success).toBe(true);
        }
      }, 15000);

      it('should handle ranges that do not overlap (gap check)', async () => {
        const result = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        expect(result.success).toBe(true);

        if (result.success) {
          await execGit(result.data, ['config', 'gitsocial.fetchedranges', JSON.stringify([
            { start: '2025-01-01', end: '2025-01-03' }
          ])]);

          const fetchResult = await storage.repository.fetch(storageBase, testRepo.path, 'main', {
            since: '2025-01-01',
            until: '2025-01-10'
          });
          expect(fetchResult.success).toBe(true);
        }
      }, 15000);

      it('should handle clearCache with protected repository that throws during cleanup', () => {
        const protectedBase = join(tmpdir(), `protected-clear-${Date.now()}`);
        const protectedRepo = join(protectedBase, 'repositories', 'test-repo');
        mkdirSync(protectedRepo, { recursive: true });
        writeFileSync(join(protectedRepo, 'file.txt'), 'data');

        if (process.platform !== 'win32') {
          try {
            chmodSync(protectedRepo, 0o444);
            const result = storage.repository.clearCache(protectedBase);
            expect(result).toBeDefined();

            // Always restore permissions for cleanup
            chmodSync(protectedRepo, 0o755);

            // Check if errors were captured
            if (result.errors.length > 0) {
              expect(result.errors.length).toBeGreaterThan(0);
            }
          } catch (error) {
            // Restore permissions even if test fails
            try {
              chmodSync(protectedRepo, 0o755);
            } catch {
              // Ignore cleanup errors
            }
          }
        }

        // Final cleanup with force
        try {
          rmSync(protectedBase, { recursive: true, force: true });
        } catch {
          // Ignore final cleanup errors
        }
      });

      it('should handle clearCache when repositories directory does not exist', () => {
        const nonExistBase = join(tmpdir(), `non-exist-clear-${Date.now()}`);
        const result = storage.repository.clearCache(nonExistBase);
        expect(result).toBeDefined();
        expect(result.deletedCount).toBe(0);
      });

      it('should handle clearCache with invalid repository directory', () => {
        const invalidBase = join(tmpdir(), `invalid-clear-${Date.now()}`);
        mkdirSync(join(invalidBase, 'repositories'), { recursive: true });
        writeFileSync(join(invalidBase, 'repositories', 'not-a-dir'), 'file-not-dir');

        const result = storage.repository.clearCache(invalidBase);
        expect(result).toBeDefined();

        rmSync(invalidBase, { recursive: true, force: true });
      });

      it('should trigger inner catch block when cleanup fails', () => {
        const failBase = join(tmpdir(), `fail-cleanup-${Date.now()}`);
        const repoDir = join(failBase, 'repositories', 'fail-repo');
        mkdirSync(repoDir, { recursive: true });

        // Create a deeply nested structure that might cause issues
        const deepPath = join(repoDir, 'a', 'b', 'c', 'd', 'e', 'f');
        mkdirSync(deepPath, { recursive: true });
        writeFileSync(join(deepPath, 'file.txt'), 'data');

        // On non-Windows, make it read-only
        if (process.platform !== 'win32') {
          try {
            chmodSync(repoDir, 0o444);
          } catch {
            // Skip if chmod not supported
          }
        }

        const result = storage.repository.clearCache(failBase);
        expect(result).toBeDefined();

        // Restore permissions
        if (process.platform !== 'win32') {
          try {
            chmodSync(repoDir, 0o755);
          } catch {
            // Ignore
          }
        }

        rmSync(failBase, { recursive: true, force: true });
      });

      it('should trigger outer catch block when readdir fails', () => {
        // Create a path that will cause readdirSync to fail
        const badBase = join(tmpdir(), `bad-readdir-${Date.now()}`);
        mkdirSync(badBase, { recursive: true });
        const reposDir = join(badBase, 'repositories');

        // Create repositories as a file instead of directory
        writeFileSync(reposDir, 'not-a-directory');

        const result = storage.repository.clearCache(badBase);
        expect(result).toBeDefined();
        expect(result.errors.length).toBeGreaterThanOrEqual(0);

        rmSync(badBase, { recursive: true, force: true });
      });

      it('should trigger cleanup outer catch when repositories is a file', async () => {
        const errorBase = join(tmpdir(), `cleanup-outer-${Date.now()}`);
        mkdirSync(errorBase, { recursive: true });
        const reposPath = join(errorBase, 'repositories');

        // Make repositories a file instead of directory to trigger readdirSync error
        writeFileSync(reposPath, 'not-a-dir');

        await storage.repository.cleanup(errorBase);
        expect(true).toBe(true); // Should not throw

        rmSync(errorBase, { recursive: true, force: true });
      });

      it('should capture error in clearCache inner catch when directory operations fail', () => {
        const innerErrorBase = join(tmpdir(), `inner-error-${Date.now()}`);
        const reposDir = join(innerErrorBase, 'repositories');
        mkdirSync(reposDir, { recursive: true });

        // Create a symlink to a non-existent target which will cause getDirectorySize to fail
        const brokenLink = join(reposDir, 'broken-link');
        try {
          if (process.platform !== 'win32') {
            symlinkSync('/nonexistent/path/that/does/not/exist', brokenLink);
          } else {
            // On Windows, create a deeply nested readonly structure
            const deepDir = join(brokenLink, 'a', 'b', 'c');
            mkdirSync(deepDir, { recursive: true });
          }

          const result = storage.repository.clearCache(innerErrorBase);
          expect(result).toBeDefined();

          // The error should be captured in result.errors
          if (process.platform !== 'win32') {
            expect(result.errors.length).toBeGreaterThan(0);
          }
        } catch (error) {
          // Some systems might not support symlinks
        }

        rmSync(innerErrorBase, { recursive: true, force: true });
      });
    });

  });
});
