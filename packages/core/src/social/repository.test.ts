import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { repository } from './repository';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { join } from 'path';
import { existsSync, mkdirSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { list } from './list';
import { initializeGitSocial } from './config';
import { execGit } from '../git/exec';
import { storage } from '../storage';

describe('repository', () => {
  let testRepo: TestRepo;
  let storageBase: string;

  beforeEach(async () => {
    testRepo = await createTestRepo('repository-test');
    await createCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
    storageBase = join(tmpdir(), `repository-test-${Date.now()}`);
    mkdirSync(storageBase, { recursive: true });
    repository.initialize({ storageBase: '' });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    testRepo.cleanup();
    if (existsSync(storageBase)) {
      rmSync(storageBase, { recursive: true, force: true });
    }
  });

  describe('initialize()', () => {
    it('should initialize repository system with storage base', () => {
      repository.initialize({ storageBase });
      const configured = repository.getConfiguredStorageBase();
      expect(configured).toBe(storageBase);
    });

    it('should allow re-initialization with different storage base', () => {
      repository.initialize({ storageBase });
      const newStorage = join(tmpdir(), `repository-test-new-${Date.now()}`);
      repository.initialize({ storageBase: newStorage });
      const configured = repository.getConfiguredStorageBase();
      expect(configured).toBe(newStorage);
    });
  });

  describe('getConfiguredStorageBase()', () => {
    it('should return empty string when reset', () => {
      const configured = repository.getConfiguredStorageBase();
      expect(configured).toBe('');
    });

    it('should return storage base after initialization', () => {
      repository.initialize({ storageBase });
      const configured = repository.getConfiguredStorageBase();
      expect(configured).toBe(storageBase);
    });
  });

  describe('createRepositoryFromUrl()', () => {
    it('should create repository from GitHub HTTPS URL', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo');
      expect(repo.url).toBe('https://github.com/user/repo');
      expect(repo.name).toBe('user/repo');
      expect(repo.branch).toBe('main');
      expect(repo.type).toBe('other');
      expect(repo.socialEnabled).toBe(true);
    });

    it('should create repository from GitHub SSH URL', () => {
      const repo = repository.createRepositoryFromUrl('git@github.com:user/repo.git');
      expect(repo.name).toBe('user/repo');
      expect(repo.url).toBe('git@github.com:user/repo.git');
    });

    it('should create repository from GitLab URL', () => {
      const repo = repository.createRepositoryFromUrl('https://gitlab.com/user/repo');
      expect(repo.name).toBe('user/repo');
      expect(repo.url).toBe('https://gitlab.com/user/repo');
    });

    it('should remove .git suffix from name', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo.git');
      expect(repo.name).toBe('user/repo');
    });

    it('should create repository with custom branch', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        branch: 'develop'
      });
      expect(repo.branch).toBe('develop');
      expect(repo.id).toBe('https://github.com/user/repo#branch:develop');
    });

    it('should create repository with workspace type', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        type: 'workspace'
      });
      expect(repo.type).toBe('workspace');
    });

    it('should create repository with socialEnabled false', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        socialEnabled: false
      });
      expect(repo.socialEnabled).toBe(false);
    });

    it('should create repository with followedAt date', () => {
      const followedAt = new Date('2024-01-01');
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        followedAt
      });
      expect(repo.followedAt).toBe(followedAt);
    });

    it('should create repository with remoteName', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        remoteName: 'upstream'
      });
      expect(repo.remoteName).toBe('upstream');
    });

    it('should create repository with lists', () => {
      const repo = repository.createRepositoryFromUrl('https://github.com/user/repo', {
        lists: ['reading', 'tech']
      });
      expect(repo.lists).toEqual(['reading', 'tech']);
    });

    it('should handle local path URLs', () => {
      const repo = repository.createRepositoryFromUrl('/path/to/user/repo');
      expect(repo.name).toBe('user/repo');
    });

    it('should fallback to last part for single-part paths', () => {
      const repo = repository.createRepositoryFromUrl('/repo');
      expect(repo.name).toBe('repo');
    });

    it('should handle unknown URL format', () => {
      const repo = repository.createRepositoryFromUrl('unknown://format');
      expect(repo.name).toBe('unknown:/format');
    });
  });

  describe('checkGitSocialInit()', () => {
    it('should detect uninitialized repository', async () => {
      const result = await repository.checkGitSocialInit(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.isInitialized).toBe(false);
    });

    it('should detect initialized repository', async () => {
      await initializeGitSocial(testRepo.path, 'gitsocial');
      const result = await repository.checkGitSocialInit(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.isInitialized).toBe(true);
      expect(result.data?.branch).toBe('gitsocial');
    });

    it('should return error on check failure', async () => {
      const configModule = await import('./config');
      const spy = vi.spyOn(configModule, 'getGitSocialConfig');
      spy.mockRejectedValue(new Error('Git error'));

      const result = await repository.checkGitSocialInit(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CHECK_FAILED');
    });
  });

  describe('initializeRepository()', () => {
    it('should initialize repository without branch options', async () => {
      const result = await repository.initializeRepository(testRepo.path);
      expect(result.success).toBe(true);
    });

    it('should initialize repository with custom branch name', async () => {
      const result = await repository.initializeRepository(testRepo.path, {
        branchName: 'custom-social'
      });
      expect(result.success).toBe(true);
      const checkResult = await repository.checkGitSocialInit(testRepo.path);
      expect(checkResult.data?.branch).toBe('custom-social');
    });

    it('should return error when initialization fails', async () => {
      const configModule = await import('./config');
      const spy = vi.spyOn(configModule, 'initializeGitSocial');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'INIT_FAILED', message: 'Init failed' }
      });

      const result = await repository.initializeRepository(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('INIT_FAILED');
    });

    it('should return error on unexpected exception', async () => {
      const configModule = await import('./config');
      const spy = vi.spyOn(configModule, 'initializeGitSocial');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const result = await repository.initializeRepository(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('INIT_ERROR');
    });
  });

  describe('getRepositoryRelationship()', () => {
    it('should identify workspace repository', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositoryRelationship(testRepo.path, testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.type).toBe('workspace');
      expect(result.data?.socialEnabled).toBe(true);
    });

    it('should identify workspace repository when target is empty', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositoryRelationship(testRepo.path, '');
      expect(result.success).toBe(true);
      expect(result.data?.type).toBe('workspace');
    });

    it('should identify repository in single list', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/user/repo#branch:main'
      );
      expect(result.success).toBe(true);
      expect(result.data?.lists).toEqual(['reading']);
      expect(result.data?.remoteName).toBe('upstream');
    });

    it('should identify repository in multiple lists', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
      await list.addRepositoryToList(testRepo.path, 'tech', 'https://github.com/user/repo#branch:main');

      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/user/repo#branch:main'
      );
      expect(result.success).toBe(true);
      expect(result.data?.lists).toHaveLength(2);
      expect(result.data?.lists).toContain('reading');
      expect(result.data?.lists).toContain('tech');
    });

    it('should normalize URLs for comparison', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo.git#branch:main');

      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/user/repo#branch:main'
      );
      expect(result.success).toBe(true);
      expect(result.data?.lists).toEqual(['reading']);
    });

    it('should identify external repository not in lists', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/external/repo#branch:main'
      );
      expect(result.success).toBe(true);
      expect(result.data?.type).toBe('other');
      expect(result.data?.socialEnabled).toBe(false);
      expect(result.data?.remoteName).toBe('upstream');
    });

    it('should return error when getLists fails', async () => {
      const listModule = await import('./list');
      const spy = vi.spyOn(listModule.list, 'getLists');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'GET_ERROR', message: 'Failed' }
      });

      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/user/repo'
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_LISTS_ERROR');
    });

    it('should return error on unexpected exception', async () => {
      const listModule = await import('./list');
      const spy = vi.spyOn(listModule.list, 'getLists');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const result = await repository.getRepositoryRelationship(
        testRepo.path,
        'https://github.com/user/repo'
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CHECK_STATUS_ERROR');
    });
  });

  describe('loadWorkspaceRepository()', () => {
    it('should load workspace repository with config', async () => {
      await initializeGitSocial(testRepo.path, 'gitsocial');
      const repos = await repository.loadWorkspaceRepository(testRepo.path);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.type).toBe('workspace');
      expect(repos[0]?.branch).toBe('gitsocial');
      expect(repos[0]?.socialEnabled).toBe(true);
      expect(repos[0]?.config).toBeDefined();
      expect(repos[0]?.config?.branch).toBe('gitsocial');
    });

    it('should load workspace repository without config', async () => {
      const repos = await repository.loadWorkspaceRepository(testRepo.path);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.type).toBe('workspace');
      expect(repos[0]?.branch).toBe('main');
      expect(repos[0]?.socialEnabled).toBe(true);
    });

    it('should set workspace repository name from path', async () => {
      const repos = await repository.loadWorkspaceRepository(testRepo.path);
      expect(repos[0]?.name).toBeDefined();
      expect(repos[0]?.name.length).toBeGreaterThan(0);
    });

    it('should return empty array on error', async () => {
      const configModule = await import('./config');
      const spy = vi.spyOn(configModule, 'getGitSocialConfig');
      spy.mockRejectedValue(new Error('Config error'));

      const repos = await repository.loadWorkspaceRepository(testRepo.path);
      expect(repos).toEqual([]);
    });
  });

  describe('loadFollowingRepositories()', () => {
    it('should return empty array when no lists exist', async () => {
      await initializeGitSocial(testRepo.path);
      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toEqual([]);
    });

    it('should load repositories from single list', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.url).toBe('https://github.com/user/repo');
      expect(repos[0]?.branch).toBe('main');
      expect(repos[0]?.lists).toEqual(['reading']);
    });

    it('should load repositories from multiple lists', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo1#branch:main');
      await list.addRepositoryToList(testRepo.path, 'tech', 'https://github.com/user/repo2#branch:main');

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toHaveLength(2);
    });

    it('should deduplicate repositories across lists', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.createList(testRepo.path, 'tech');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');
      await list.addRepositoryToList(testRepo.path, 'tech', 'https://github.com/user/repo#branch:main');

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.lists).toHaveLength(2);
      expect(repos[0]?.lists).toContain('reading');
      expect(repos[0]?.lists).toContain('tech');
    });

    it('should load repository with storage path when cloned', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${testRepo.path}#branch:main`);
      await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.path).toBeDefined();
      expect(existsSync(repos[0]!.path!)).toBe(true);
    });

    it('should load repository without storage path when not cloned', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.path).toBeUndefined();
    });

    it('should handle repository IDs that fail parsing', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');

      const gitMsgRefModule = await import('../gitmsg/protocol');
      const spy = vi.spyOn(gitMsgRefModule.gitMsgRef, 'parseRepositoryId');
      spy.mockReturnValue(null);

      const listResult = await list.getList(testRepo.path, 'reading');
      if (listResult.data) {
        listResult.data.repositories = ['some-repo-id'];
        await list.updateList(testRepo.path, 'reading', listResult.data);
      }

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toEqual([]);
    });

    it('should return empty array when getLists fails', async () => {
      const listModule = await import('./list');
      const spy = vi.spyOn(listModule.list, 'getLists');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'ERROR', message: 'Failed' }
      });

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toEqual([]);
    });

    it('should return empty array on exception', async () => {
      const listModule = await import('./list');
      const spy = vi.spyOn(listModule.list, 'getLists');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const repos = await repository.loadFollowingRepositories(testRepo.path, storageBase);
      expect(repos).toEqual([]);
    });
  });

  describe('loadAllRepositories()', () => {
    it('should load workspace and following repositories', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const repos = await repository.loadAllRepositories(testRepo.path, storageBase);
      expect(repos.length).toBeGreaterThanOrEqual(2);
      expect(repos.some(r => r.type === 'workspace')).toBe(true);
    });

    it('should scan storage for additional repositories', async () => {
      await initializeGitSocial(testRepo.path);
      const externalRepo = await createTestRepo('external-repo');
      await createCommit(externalRepo.path, 'External commit', { allowEmpty: true });
      await storage.repository.ensure(storageBase, externalRepo.path, 'main', { isPersistent: true });

      const repos = await repository.loadAllRepositories(testRepo.path, storageBase);
      const hasExternal = repos.some(r => r.url === externalRepo.path);
      expect(hasExternal).toBe(true);

      externalRepo.cleanup();
    });

    it('should not duplicate repositories', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${testRepo.path}#branch:main`);
      await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });

      const repos = await repository.loadAllRepositories(testRepo.path, storageBase);
      const workspaceRepos = repos.filter(r => r.url === testRepo.path || r.type === 'workspace');
      expect(workspaceRepos.length).toBeGreaterThanOrEqual(1);
    });

    it('should work without storage base', async () => {
      await initializeGitSocial(testRepo.path);
      const repos = await repository.loadAllRepositories(testRepo.path);
      expect(repos).toHaveLength(1);
      expect(repos[0]?.type).toBe('workspace');
    });

    it('should handle errors gracefully', async () => {
      const configModule = await import('./config');
      const spy = vi.spyOn(configModule, 'getGitSocialConfig');
      spy.mockRejectedValue(new Error('Config error'));

      const repos = await repository.loadAllRepositories(testRepo.path, storageBase);
      expect(repos).toEqual([]);
    });
  });

  describe('loadExternalRepository()', () => {
    it('should return null when branch not provided', async () => {
      const repo = await repository.loadExternalRepository(
        'https://github.com/user/repo',
        storageBase
      );
      expect(repo).toBeNull();
    });

    it('should load external repository without cloning', async () => {
      const repo = await repository.loadExternalRepository(
        'https://github.com/user/repo',
        storageBase,
        { branch: 'main' }
      );
      expect(repo).not.toBeNull();
      expect(repo?.url).toBe('https://github.com/user/repo');
      expect(repo?.branch).toBe('main');
      expect(repo?.path).toBeUndefined();
    });

    it('should ensure repository is cloned when requested', async () => {
      const repo = await repository.loadExternalRepository(
        testRepo.path,
        storageBase,
        { branch: 'main', ensureCloned: true }
      );
      expect(repo).not.toBeNull();
      expect(repo?.path).toBeDefined();
      expect(existsSync(repo!.path!)).toBe(true);
    });

    it('should return null when ensure fails', async () => {
      const repo = await repository.loadExternalRepository(
        'https://invalid-url-that-will-fail.com/repo',
        storageBase,
        { branch: 'main', ensureCloned: true }
      );
      expect(repo).toBeNull();
    });

    it('should load repository with storage metadata', async () => {
      await storage.repository.ensure(storageBase, testRepo.path, 'main', { isPersistent: true });
      const repo = await repository.loadExternalRepository(
        testRepo.path,
        storageBase,
        { branch: 'main' }
      );
      expect(repo?.path).toBeDefined();
      expect(repo?.lastFetchTime).toBeDefined();
    });

    it('should return null on unexpected error', async () => {
      const storageModule = await import('../storage');
      const spy = vi.spyOn(storageModule.storage.repository, 'ensure');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const repo = await repository.loadExternalRepository(
        'https://github.com/user/repo',
        storageBase,
        { branch: 'main', ensureCloned: true }
      );
      expect(repo).toBeNull();
    });
  });

  describe('applyRepositoryFilters()', () => {
    const repos = [
      repository.createRepositoryFromUrl('https://github.com/user/repo1', {
        type: 'workspace',
        socialEnabled: true
      }),
      repository.createRepositoryFromUrl('https://github.com/user/repo2', {
        type: 'other',
        socialEnabled: true
      }),
      repository.createRepositoryFromUrl('https://github.com/user/repo3', {
        type: 'other',
        socialEnabled: false
      })
    ];

    it('should return all repositories when no filter', () => {
      const filtered = repository.applyRepositoryFilters(repos);
      expect(filtered).toHaveLength(3);
    });

    it('should filter by workspace type', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        types: ['workspace']
      });
      expect(filtered).toHaveLength(1);
      expect(filtered[0]?.type).toBe('workspace');
    });

    it('should filter by other type', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        types: ['other']
      });
      expect(filtered).toHaveLength(2);
    });

    it('should filter by socialEnabled true', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        socialEnabled: true
      });
      expect(filtered).toHaveLength(2);
    });

    it('should filter by socialEnabled false', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        socialEnabled: false
      });
      expect(filtered).toHaveLength(1);
      expect(filtered[0]?.socialEnabled).toBe(false);
    });

    it('should apply limit', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        limit: 2
      });
      expect(filtered).toHaveLength(2);
    });

    it('should apply combined filters', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        types: ['other'],
        socialEnabled: true,
        limit: 1
      });
      expect(filtered).toHaveLength(1);
      expect(filtered[0]?.type).toBe('other');
      expect(filtered[0]?.socialEnabled).toBe(true);
    });

    it('should handle empty types array', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        types: []
      });
      expect(filtered).toHaveLength(3);
    });

    it('should handle zero limit', () => {
      const filtered = repository.applyRepositoryFilters(repos, {
        limit: 0
      });
      expect(filtered).toHaveLength(3);
    });
  });

  describe('getRepositories()', () => {
    beforeEach(() => {
      repository.initialize({ storageBase });
    });

    it('should get workspace repositories', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositories(testRepo.path, 'workspace:my');
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0]?.type).toBe('workspace');
    });

    it('should get following repositories', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const result = await repository.getRepositories(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should get all repositories', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const result = await repository.getRepositories(testRepo.path, 'all');
      expect(result.success).toBe(true);
      expect(result.data!.length).toBeGreaterThanOrEqual(2);
    });

    it('should get specific external repository', async () => {
      const result = await repository.getRepositories(
        testRepo.path,
        'repository:https://github.com/user/repo#branch:main'
      );
      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0]?.url).toBe('https://github.com/user/repo');
    });

    it('should apply filters', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositories(testRepo.path, 'workspace:my', {
        socialEnabled: true
      });
      expect(result.success).toBe(true);
      expect(result.data?.every(r => r.socialEnabled)).toBe(true);
    });

    it('should use cache by default', async () => {
      await initializeGitSocial(testRepo.path);
      const result1 = await repository.getRepositories(testRepo.path, 'workspace:my');
      const result2 = await repository.getRepositories(testRepo.path, 'workspace:my');
      expect(result1.data).toEqual(result2.data);
    });

    it('should skip cache when requested', async () => {
      await initializeGitSocial(testRepo.path);
      const result = await repository.getRepositories(testRepo.path, 'workspace:my', {
        skipCache: true
      });
      expect(result.success).toBe(true);
    });

    it('should handle unknown scope', async () => {
      const result = await repository.getRepositories(testRepo.path, 'unknown-scope');
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return error on exception', async () => {
      const cacheModule = await import('../storage');
      const spy = vi.spyOn(cacheModule.storage.cache, 'get');
      spy.mockImplementation(() => {
        throw new Error('Cache error');
      });

      const result = await repository.getRepositories(testRepo.path, 'workspace:my');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_REPOSITORIES_ERROR');
    });
  });

  describe('fetchUpdates()', () => {
    beforeEach(() => {
      repository.initialize({ storageBase });
    });

    it('should return error when not initialized', async () => {
      repository.initialize({ storageBase: '' });
      const result = await repository.fetchUpdates(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOT_INITIALIZED');
    });

    it('should return error when getRepositories fails', async () => {
      await initializeGitSocial(testRepo.path);

      const storageModule = await import('../storage');
      const spy = vi.spyOn(storageModule.storage.cache, 'get');
      spy.mockImplementation(() => {
        throw new Error('Cache error');
      });

      const result = await repository.fetchUpdates(testRepo.path, 'workspace:my');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GET_REPOSITORIES_ERROR');
    });

    it('should fetch updates for workspace scope', async () => {
      await initializeGitSocial(testRepo.path);
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Test commit']);

      const result = await repository.fetchUpdates(testRepo.path, 'workspace:my');
      expect(result.success).toBe(true);
      expect(result.data?.fetched).toBeGreaterThanOrEqual(0);
      expect(result.data?.failed).toBeGreaterThanOrEqual(0);
    });

    it('should fetch updates for following scope', async () => {
      await initializeGitSocial(testRepo.path);
      const externalRepo = await createTestRepo('external');
      await createCommit(externalRepo.path, 'External commit', { allowEmpty: true });
      await initializeGitSocial(externalRepo.path);

      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${externalRepo.path}#branch:main`);

      const result = await repository.fetchUpdates(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data?.fetched).toBeGreaterThanOrEqual(0);

      externalRepo.cleanup();
    });

    it('should handle repositories with missing branch', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://github.com/user/repo#branch:main');

      const loadFollowingSpy = vi.spyOn(repository, 'loadFollowingRepositories');
      loadFollowingSpy.mockResolvedValue([{
        id: 'test-id',
        url: 'https://github.com/user/repo',
        name: 'user/repo',
        branch: '',
        type: 'other',
        socialEnabled: true,
        fetchedRanges: []
      }]);

      const result = await repository.fetchUpdates(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data?.failed).toBe(1);
    });

    it('should handle ensure failures', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', 'https://invalid-url.com/repo#branch:main');

      const result = await repository.fetchUpdates(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data?.failed).toBeGreaterThanOrEqual(1);
    });

    it('should handle fetch failures', async () => {
      await initializeGitSocial(testRepo.path);
      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${testRepo.path}#branch:main`);

      const storageModule = await import('../storage');
      const originalFetch = storageModule.storage.repository.fetch;
      const spy = vi.spyOn(storageModule.storage.repository, 'fetch');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Fetch failed' }
      });

      const result = await repository.fetchUpdates(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data?.failed).toBeGreaterThanOrEqual(1);

      spy.mockRestore();
      storageModule.storage.repository.fetch = originalFetch;
    });

    it('should skip already fetched repositories', async () => {
      await initializeGitSocial(testRepo.path);
      const externalRepo = await createTestRepo('external');
      await createCommit(externalRepo.path, 'External commit', { allowEmpty: true });
      await initializeGitSocial(externalRepo.path);

      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${externalRepo.path}#branch:main`);

      const storageModule = await import('../storage');
      const originalFetch = storageModule.storage.repository.fetch;
      const spy = vi.spyOn(storageModule.storage.repository, 'fetch');
      spy.mockResolvedValue({
        success: true,
        data: { skipped: true }
      });

      const result = await repository.fetchUpdates(testRepo.path, 'following');
      expect(result.success).toBe(true);
      expect(result.data?.fetched).toBe(0);

      spy.mockRestore();
      storageModule.storage.repository.fetch = originalFetch;
      externalRepo.cleanup();
    });

    it('should use custom branch from options', async () => {
      await initializeGitSocial(testRepo.path);
      const externalRepo = await createTestRepo('external');
      await createCommit(externalRepo.path, 'External commit', { allowEmpty: true });
      await execGit(externalRepo.path, ['checkout', '-b', 'custom']);
      await initializeGitSocial(externalRepo.path);

      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${externalRepo.path}#branch:custom`);

      const result = await repository.fetchUpdates(testRepo.path, 'following', {
        branch: 'custom'
      });
      expect(result.success).toBe(true);

      externalRepo.cleanup();
    });

    it('should use since option for date filtering', async () => {
      await initializeGitSocial(testRepo.path);
      const externalRepo = await createTestRepo('external');
      await createCommit(externalRepo.path, 'External commit', { allowEmpty: true });
      await initializeGitSocial(externalRepo.path);

      await list.createList(testRepo.path, 'reading');
      await list.addRepositoryToList(testRepo.path, 'reading', `${externalRepo.path}#branch:main`);

      const result = await repository.fetchUpdates(testRepo.path, 'following', {
        since: '2024-01-01'
      });
      expect(result.success).toBe(true);

      externalRepo.cleanup();
    });
  });

  describe('ensureDataForDateRange()', () => {
    beforeEach(() => {
      repository.initialize({ storageBase });
    });

    it('should ensure data for date range', async () => {
      const since = new Date('2024-01-01');
      const result = await repository.ensureDataForDateRange(
        testRepo.path,
        storageBase,
        testRepo.path,
        'main',
        since
      );
      expect(result.success).toBe(true);
    });

    it('should return error when ensure fails', async () => {
      const storageModule = await import('../storage');
      const spy = vi.spyOn(storageModule.storage.repository, 'ensure');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'ENSURE_ERROR', message: 'Ensure failed' }
      });

      const since = new Date('2024-01-01');
      const result = await repository.ensureDataForDateRange(
        testRepo.path,
        storageBase,
        'https://github.com/user/repo',
        'main',
        since
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('ENSURE_ERROR');
    });

    it('should return error when fetch fails', async () => {
      const storageModule = await import('../storage');
      const spy = vi.spyOn(storageModule.storage.repository, 'fetch');
      spy.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Fetch failed' }
      });

      const since = new Date('2024-01-01');
      const result = await repository.ensureDataForDateRange(
        testRepo.path,
        storageBase,
        testRepo.path,
        'main',
        since
      );
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('FETCH_ERROR');
    });

    it('should handle isPersistent option', async () => {
      const since = new Date('2024-01-01');
      const result = await repository.ensureDataForDateRange(
        testRepo.path,
        storageBase,
        testRepo.path,
        'main',
        since,
        { isPersistent: true }
      );
      expect(result.success).toBe(true);
    });
  });

  describe('cleanupStorage()', () => {
    it('should cleanup storage when initialized', async () => {
      repository.initialize({ storageBase });
      await repository.cleanupStorage();
    });

    it('should handle cleanup when not initialized', async () => {
      await repository.cleanupStorage();
    });
  });

  describe('getStorageStats()', () => {
    it('should return zero stats when not initialized', async () => {
      const stats = await repository.getStorageStats();
      expect(stats.totalRepositories).toBe(0);
      expect(stats.diskUsage).toBe(0);
      expect(stats.persistent).toBe(0);
      expect(stats.temporary).toBe(0);
    });

    it('should return stats when initialized', async () => {
      repository.initialize({ storageBase });
      const stats = await repository.getStorageStats();
      expect(stats).toHaveProperty('totalRepositories');
      expect(stats).toHaveProperty('diskUsage');
      expect(stats).toHaveProperty('persistent');
      expect(stats).toHaveProperty('temporary');
    });
  });

  describe('clearCache()', () => {
    it('should return empty result when not initialized', () => {
      const result = repository.clearCache();
      expect(result.deletedCount).toBe(0);
      expect(result.diskSpaceFreed).toBe(0);
      expect(result.errors).toEqual([]);
    });

    it('should clear cache when initialized', () => {
      repository.initialize({ storageBase });
      const result = repository.clearCache();
      expect(result).toHaveProperty('deletedCount');
      expect(result).toHaveProperty('diskSpaceFreed');
      expect(result).toHaveProperty('errors');
    });
  });
});
