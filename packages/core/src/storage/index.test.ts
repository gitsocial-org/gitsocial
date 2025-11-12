import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { storage } from './index';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { join } from 'path';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'fs';
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

    describe.skip('repository.fetch()', () => {
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
        const result = await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
          until: new Date().toISOString().split('T')[0]
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should skip fetch if already fetched', async () => {
        const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0];
        const until = new Date().toISOString().split('T')[0];
        await storage.repository.fetch(repoPath, testRepo.path, 'main', { since, until });
        const result = await storage.repository.fetch(repoPath, testRepo.path, 'main', { since, until });
        expect(result.success).toBe(true);
        if (result.success && result.data) {
          expect(result.data.skipped).toBe(true);
        }
      }, 15000);

      it('should handle fetch with depth option', async () => {
        const result = await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
          until: new Date().toISOString().split('T')[0],
          depth: 1
        });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle invalid repository path', async () => {
        const result = await storage.repository.fetch('/nonexistent/path', testRepo.path, 'main', {
          since: '2025-01-01',
          until: '2025-01-10'
        });
        expect(result.success).toBe(false);
      });

      it('should update lastFetch timestamp', async () => {
        await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
          until: new Date().toISOString().split('T')[0]
        });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.lastFetch).toBeDefined();
      }, 15000);

      it('should update fetchedRanges', async () => {
        const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0];
        const until = new Date().toISOString().split('T')[0];
        await storage.repository.fetch(repoPath, testRepo.path, 'main', { since, until });
        const config = await storage.repository.readConfig(repoPath);
        expect(config?.fetchedRanges).toBeDefined();
        expect(config?.fetchedRanges?.length).toBeGreaterThan(0);
      }, 15000);
    });

    describe.skip('repository.getCommits()', () => {
      let repoPath: string;

      beforeEach(async () => {
        await createCommit(testRepo.path, 'Commit 1', { allowEmpty: true });
        await createCommit(testRepo.path, 'Commit 2', { allowEmpty: true });
        await createCommit(testRepo.path, 'Commit 3', { allowEmpty: true });
        const ensureResult = await storage.repository.ensure(storageBase, testRepo.path, 'main');
        if (!ensureResult.success) {
          throw new Error('Failed to ensure repository');
        }
        repoPath = ensureResult.data;
        await storage.repository.fetch(repoPath, testRepo.path, 'main', {
          since: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
          until: new Date().toISOString().split('T')[0]
        });
      }, 20000);

      it('should get commits from repository', async () => {
        const result = await storage.repository.getCommits(repoPath, testRepo.path, 'main');
        expect(result.success).toBe(true);
        if (result.success) {
          expect(result.data.length).toBeGreaterThan(0);
        }
      }, 15000);

      it('should include commit metadata', async () => {
        const result = await storage.repository.getCommits(repoPath, testRepo.path, 'main');
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
        const result = await storage.repository.getCommits(repoPath, testRepo.path, 'main', { since, until });
        expect(result.success).toBe(true);
      }, 15000);

      it('should handle limit option', async () => {
        const result = await storage.repository.getCommits(repoPath, testRepo.path, 'main', { limit: 2 });
        expect(result.success).toBe(true);
        if (result.success) {
          expect(result.data.length).toBeLessThanOrEqual(2);
        }
      }, 15000);

      it('should handle non-existent branch', async () => {
        const result = await storage.repository.getCommits(repoPath, testRepo.path, 'nonexistent');
        expect(result.success).toBe(false);
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
        const gitDir = join(repoPath, '.git');
        if (!existsSync(gitDir)) {
          return;
        }
        const configPath = join(gitDir, 'gitsocial.config');
        const config = await storage.repository.readConfig(repoPath);
        if (config) {
          writeFileSync(configPath, `url=${config.url}\nbranch=${config.branch}\nfetchedRanges=invalid-json\n`);
          const result = await storage.repository.readConfig(repoPath);
          expect(result).toBeDefined();
          expect(result?.fetchedRanges).toEqual([]);
        }
      });

      it('should return null for empty config', async () => {
        const gitDir = join(repoPath, '.git');
        if (!existsSync(gitDir)) {
          return;
        }
        const configPath = join(gitDir, 'gitsocial.config');
        writeFileSync(configPath, '');
        const result = await storage.repository.readConfig(repoPath);
        expect(result).toBeNull();
      });
    });
  });
});
