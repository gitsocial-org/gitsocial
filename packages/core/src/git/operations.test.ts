import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createCommit,
  getCommit,
  getCommits,
  getCurrentBranch,
  getUnpushedCommits,
  initGitRepository,
  isGitRepository,
  listRefs,
  readGitRef,
  writeGitRef
} from './operations';
import { createCommit as createTestCommit, createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from './exec';
import { mkdtempSync, rmSync, writeFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';

describe('git/operations', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('git-operations');
    await createTestCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    testRepo.cleanup();
  });

  describe('isGitRepository()', () => {
    it('should return true for git repository', async () => {
      const result = await isGitRepository(testRepo.path);
      expect(result).toBe(true);
    });

    it('should return false for non-git directory', async () => {
      const result = await isGitRepository('/tmp/nonexistent-repo-xyz');
      expect(result).toBe(false);
    });
  });

  describe('initGitRepository()', () => {
    it('should initialize repository with default branch', async () => {
      const tempPath = mkdtempSync(join(tmpdir(), 'test-git-init-'));
      const result = await initGitRepository(tempPath);

      expect(result.success).toBe(true);
      const isRepo = await isGitRepository(tempPath);
      expect(isRepo).toBe(true);

      rmSync(tempPath, { recursive: true, force: true });
    });

    it('should initialize repository with custom initial branch', async () => {
      const tempPath = mkdtempSync(join(tmpdir(), 'test-git-init-custom-'));
      const result = await initGitRepository(tempPath, 'develop');

      expect(result.success).toBe(true);
      const isRepo = await isGitRepository(tempPath);
      expect(isRepo).toBe(true);

      rmSync(tempPath, { recursive: true, force: true });
    });

    it('should return error for invalid path', async () => {
      const result = await initGitRepository('/invalid/path/that/does/not/exist');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should return fallback error when execGit returns no error', async () => {
      const spy = vi.spyOn(await import('./exec'), 'execGit');
      spy.mockResolvedValueOnce({ success: false });
      const tempPath = mkdtempSync(join(tmpdir(), 'test-git-fallback-'));
      const result = await initGitRepository(tempPath);
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_INIT_ERROR');
      expect(result.error?.message).toBe('Failed to initialize git repository');
      spy.mockRestore();
      rmSync(tempPath, { recursive: true, force: true });
    });
  });

  describe('getCommits()', () => {
    beforeEach(async () => {
      await createTestCommit(testRepo.path, 'First commit', { allowEmpty: true });
      await createTestCommit(testRepo.path, 'Second commit', { allowEmpty: true });
      await createTestCommit(testRepo.path, 'Third commit', { allowEmpty: true });
    });

    it('should get commits from HEAD', async () => {
      const commits = await getCommits(testRepo.path);

      expect(commits.length).toBeGreaterThanOrEqual(3);
      expect(commits[0]?.message).toContain('Third commit');
      expect(commits[1]?.message).toContain('Second commit');
      expect(commits[2]?.message).toContain('First commit');
    });

    it('should limit number of commits', async () => {
      const commits = await getCommits(testRepo.path, { limit: 2 });

      expect(commits.length).toBeLessThanOrEqual(2);
    });

    it('should get commits from specific branch', async () => {
      await execGit(testRepo.path, ['checkout', '-b', 'feature']);
      await createTestCommit(testRepo.path, 'Feature commit', { allowEmpty: true });

      const commits = await getCommits(testRepo.path, { branch: 'feature' });

      expect(commits.length).toBeGreaterThanOrEqual(4);
      expect(commits[0]?.message).toContain('Feature commit');
    });

    it('should get commits since date', async () => {
      const yesterday = new Date();
      yesterday.setDate(yesterday.getDate() - 1);

      const commits = await getCommits(testRepo.path, { since: yesterday });

      expect(commits.length).toBeGreaterThanOrEqual(3);
    });

    it('should get commits until date', async () => {
      const tomorrow = new Date();
      tomorrow.setDate(tomorrow.getDate() + 1);

      const commits = await getCommits(testRepo.path, { until: tomorrow });

      expect(commits.length).toBeGreaterThanOrEqual(3);
    });

    it('should get commits from all branches', async () => {
      await execGit(testRepo.path, ['checkout', '-b', 'feature']);
      await createTestCommit(testRepo.path, 'Feature commit', { allowEmpty: true });
      await execGit(testRepo.path, ['checkout', 'main']);

      const commits = await getCommits(testRepo.path, { all: true });

      expect(commits.length).toBeGreaterThanOrEqual(4);
    });

    it('should include additional refs', async () => {
      await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/lists/test', 'HEAD']);

      const commits = await getCommits(testRepo.path, {
        includeRefs: ['refs/gitmsg/social/lists/test']
      });

      expect(commits.length).toBeGreaterThanOrEqual(3);
    });

    it('should return empty array for invalid branch', async () => {
      const commits = await getCommits(testRepo.path, { branch: 'nonexistent' });

      expect(commits).toEqual([]);
    });
  });

  describe('createCommit()', () => {
    it('should create commit with message', async () => {
      await execGit(testRepo.path, ['add', '-A']);
      const result = await createCommit(testRepo.path, {
        message: 'Test commit',
        allowEmpty: true
      });

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
      expect(result.data?.length).toBe(12);
    });

    it('should create commit with parent', async () => {
      const parentResult = await execGit(testRepo.path, ['rev-parse', 'HEAD']);
      const parentHash = parentResult.data?.stdout.trim();

      const result = await createCommit(testRepo.path, {
        message: 'Child commit',
        parent: parentHash
      });

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should fail when no changes and not allowing empty', async () => {
      const result = await createCommit(testRepo.path, {
        message: 'No changes',
        allowEmpty: false
      });

      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('No changes to commit');
    });

    it('should create empty commit when allowed', async () => {
      const result = await createCommit(testRepo.path, {
        message: 'Empty commit',
        allowEmpty: true
      });

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should return error for invalid parent', async () => {
      const result = await createCommit(testRepo.path, {
        message: 'Test',
        parent: 'invalidhash123'
      });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should return fallback error when commit-tree succeeds but returns empty data', async () => {
      const parentResult = await execGit(testRepo.path, ['rev-parse', 'HEAD']);
      const parentHash = parentResult.data?.stdout.trim();
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit-tree') {
          return { success: true, data: { stdout: '', stderr: '' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await createCommit(testRepo.path, {
        message: 'Test',
        parent: parentHash
      });
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_COMMIT_ERROR');
      expect(result.error?.message).toBe('Failed to create commit');
      spy.mockRestore();
    });

    it('should return error when git commit command fails', async () => {
      writeFileSync(join(testRepo.path, 'test.txt'), 'content');
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'Commit failed' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await createCommit(testRepo.path, {
        message: 'Test',
        allowEmpty: false
      });
      expect(result.success).toBe(false);
      spy.mockRestore();
    });
  });

  describe('readGitRef()', () => {
    it('should read HEAD ref', async () => {
      const result = await readGitRef(testRepo.path, 'HEAD');

      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
      expect(result.data?.length).toBe(12);
    });

    it('should return error for invalid ref', async () => {
      const result = await readGitRef(testRepo.path, 'refs/heads/nonexistent');

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  describe('writeGitRef()', () => {
    it('should write git ref', async () => {
      const headResult = await readGitRef(testRepo.path, 'HEAD');
      const headHash = headResult.data!;

      const result = await writeGitRef(testRepo.path, 'refs/test/myref', headHash);

      expect(result.success).toBe(true);

      const verifyResult = await readGitRef(testRepo.path, 'refs/test/myref');
      expect(verifyResult.data).toBe(headHash);
    });

    it('should return error for invalid hash', async () => {
      const result = await writeGitRef(testRepo.path, 'refs/test/invalid', 'invalidhash');

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  describe('getCurrentBranch()', () => {
    it('should get current branch name', async () => {
      const result = await getCurrentBranch(testRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should get feature branch name', async () => {
      await execGit(testRepo.path, ['checkout', '-b', 'feature']);

      const result = await getCurrentBranch(testRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toBe('feature');
    });
  });

  describe('getCommit()', () => {
    it('should get single commit by hash', async () => {
      await createTestCommit(testRepo.path, 'Test commit', { allowEmpty: true });
      const commits = await getCommits(testRepo.path, { limit: 1 });
      const hash = commits[0]?.hash;

      const commit = await getCommit(testRepo.path, hash!);

      expect(commit).not.toBeNull();
      expect(commit?.hash).toBe(hash);
      expect(commit?.message).toContain('Test commit');
    });

    it('should return null for invalid hash', async () => {
      const commit = await getCommit(testRepo.path, 'invalidhash123');

      expect(commit).toBeNull();
    });
  });

  describe('listRefs()', () => {
    beforeEach(async () => {
      const headResult = await readGitRef(testRepo.path, 'HEAD');
      const headHash = headResult.data!;

      await writeGitRef(testRepo.path, 'refs/gitmsg/social/lists/reading', headHash);
      await writeGitRef(testRepo.path, 'refs/gitmsg/social/lists/tech', headHash);
      await writeGitRef(testRepo.path, 'refs/gitmsg/social/config', headHash);
    });

    it('should list all refs under refs/gitmsg/', async () => {
      const refs = await listRefs(testRepo.path);

      expect(refs.length).toBeGreaterThanOrEqual(3);
      expect(refs).toContain('social/lists/reading');
      expect(refs).toContain('social/lists/tech');
      expect(refs).toContain('social/config');
    });

    it('should list refs under specific namespace', async () => {
      const refs = await listRefs(testRepo.path, 'social/lists');

      expect(refs.length).toBe(2);
      expect(refs).toContain('social/lists/reading');
      expect(refs).toContain('social/lists/tech');
    });

    it('should handle namespace with refs/gitmsg/ prefix', async () => {
      const refs = await listRefs(testRepo.path, 'refs/gitmsg/social/lists');

      expect(refs.length).toBe(2);
    });

    it('should return empty array for nonexistent namespace', async () => {
      const refs = await listRefs(testRepo.path, 'nonexistent');

      expect(refs).toEqual([]);
    });
  });

  describe('getUnpushedCommits()', () => {
    it('should return all commits when no remote', async () => {
      await createTestCommit(testRepo.path, 'Commit 1', { allowEmpty: true });
      await createTestCommit(testRepo.path, 'Commit 2', { allowEmpty: true });

      const unpushed = await getUnpushedCommits(testRepo.path, 'main');

      expect(unpushed.size).toBeGreaterThanOrEqual(2);
    });

    it('should return commits not in origin', async () => {
      await createTestCommit(testRepo.path, 'Initial', { allowEmpty: true });

      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/main', 'HEAD']);

      await createTestCommit(testRepo.path, 'Unpushed 1', { allowEmpty: true });
      await createTestCommit(testRepo.path, 'Unpushed 2', { allowEmpty: true });

      const unpushed = await getUnpushedCommits(testRepo.path, 'main');

      expect(unpushed.size).toBe(2);
    });

    it('should return empty set when all commits are pushed', async () => {
      const commits = await getCommits(testRepo.path, { limit: 1 });
      const headHash = commits[0]?.hash;

      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/main', headHash!]);

      const unpushed = await getUnpushedCommits(testRepo.path, 'main');

      expect(unpushed.size).toBe(0);
    });

    it('should return empty set when rev-list fails for local branch', async () => {
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'rev-list' && !args.includes('..')) {
          return { success: false };
        }
        return originalExecGit(workdir, args);
      });
      const unpushed = await getUnpushedCommits(testRepo.path, 'main');
      expect(unpushed.size).toBe(0);
      spy.mockRestore();
    });
  });

  describe('createCommitOnBranch()', () => {
    it('should create commit on new branch', async () => {
      const { createCommitOnBranch } = await import('./operations');
      const result = await createCommitOnBranch(testRepo.path, 'new-branch', 'Test commit');
      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
      expect(result.data?.length).toBe(12);
    });

    it('should create commit on existing branch with parent', async () => {
      const { createCommitOnBranch } = await import('./operations');
      await createTestCommit(testRepo.path, 'First', { allowEmpty: true });
      await execGit(testRepo.path, ['branch', 'existing']);
      const result = await createCommitOnBranch(testRepo.path, 'existing', 'Second commit');
      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should return short hash', async () => {
      const { createCommitOnBranch } = await import('./operations');
      const result = await createCommitOnBranch(testRepo.path, 'test', 'Message');
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(12);
    });

    it('should return fallback error when commit-tree returns empty data on existing branch', async () => {
      const { createCommitOnBranch } = await import('./operations');
      await execGit(testRepo.path, ['branch', 'existing']);
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit-tree') {
          return { success: true, data: { stdout: '', stderr: '' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await createCommitOnBranch(testRepo.path, 'existing', 'Test');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_COMMIT_ERROR');
      expect(result.error?.message).toBe('Failed to create commit');
      spy.mockRestore();
    });

    it('should return fallback error when commit-tree returns empty data on new branch', async () => {
      const { createCommitOnBranch } = await import('./operations');
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit-tree') {
          return { success: true, data: { stdout: '', stderr: '' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await createCommitOnBranch(testRepo.path, 'new-branch', 'Test');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_COMMIT_ERROR');
      expect(result.error?.message).toBe('Failed to create initial commit');
      spy.mockRestore();
    });
  });

  describe('getConfiguredBranch()', () => {
    it('should return branch from refs/gitmsg/social/config', async () => {
      const { getConfiguredBranch } = await import('./operations');
      const config = JSON.stringify({ branch: 'custom' });
      await execGit(testRepo.path, ['commit-tree', '4b825dc642cb6eb9a060e54bf8d69288fbee4904', '-m', config])
        .then(async (result) => {
          const hash = result.data?.stdout.trim();
          await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/config', hash!]);
        });
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('custom');
    });

    it('should return gitsocial if branch exists', async () => {
      const { getConfiguredBranch } = await import('./operations');
      await execGit(testRepo.path, ['branch', 'gitsocial']);
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('gitsocial');
    });

    it('should return default branch from origin/HEAD', async () => {
      const { getConfiguredBranch } = await import('./operations');
      await execGit(testRepo.path, ['symbolic-ref', 'refs/remotes/origin/HEAD', 'refs/remotes/origin/develop']);
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('develop');
    });

    it('should fallback to main', async () => {
      const { getConfiguredBranch } = await import('./operations');
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('main');
    });

    it('should ignore invalid JSON in config ref', async () => {
      const { getConfiguredBranch } = await import('./operations');
      const invalidJson = 'not valid json {';
      await execGit(testRepo.path, ['commit-tree', '4b825dc642cb6eb9a060e54bf8d69288fbee4904', '-m', invalidJson])
        .then(async (result) => {
          const hash = result.data?.stdout.trim();
          await execGit(testRepo.path, ['update-ref', 'refs/gitmsg/social/config', hash!]);
        });
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('main');
    });
  });

  describe('validatePushPreconditions()', () => {
    it('should fail on detached HEAD', async () => {
      const { validatePushPreconditions } = await import('./operations');
      await execGit(testRepo.path, ['checkout', '--detach']);
      const result = await validatePushPreconditions(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('detached HEAD');
    });

    it('should fail when no remotes exist', async () => {
      const { validatePushPreconditions } = await import('./operations');
      const result = await validatePushPreconditions(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('No \'origin\' remote');
    });

    it('should fail when git remote command fails', async () => {
      const { validatePushPreconditions } = await import('./operations');
      const execModule = await import('./exec');
      const originalExecGit = execModule.execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'remote' && args.length === 1) {
          return { success: false };
        }
        return originalExecGit(workdir, args);
      });
      const result = await validatePushPreconditions(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toBe('Failed to list remotes');
      spy.mockRestore();
    });

    it('should fail when branch does not exist', async () => {
      const { validatePushPreconditions } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      const newRepo = await createTestRepo('validate-no-branch');
      await addRemoteFn(newRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await validatePushPreconditions(newRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('does not exist locally');
      newRepo.cleanup();
    });

    it('should fail when branches have diverged', async () => {
      const { validatePushPreconditions } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      await addRemoteFn(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await createTestCommit(testRepo.path, 'Base', { allowEmpty: true });
      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/main', 'HEAD']);
      await createTestCommit(testRepo.path, 'Local ahead', { allowEmpty: true });
      await execGit(testRepo.path, ['commit-tree', '4b825dc642cb6eb9a060e54bf8d69288fbee4904', '-m', 'Remote ahead', '-p', 'refs/remotes/origin/main'])
        .then(async (result) => {
          const hash = result.data?.stdout.trim();
          await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/main', hash!]);
        });
      const result = await validatePushPreconditions(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('diverged');
    });

    it('should succeed with valid state', async () => {
      const { validatePushPreconditions } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      await addRemoteFn(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['branch', 'main']);
      const result = await validatePushPreconditions(testRepo.path);
      expect(result.success).toBe(true);
    });
  });

  describe('mergeBranch()', () => {
    it('should merge branch successfully', async () => {
      const { mergeBranch } = await import('./operations');
      await execGit(testRepo.path, ['branch', 'feature']);
      await execGit(testRepo.path, ['checkout', 'feature']);
      await createTestCommit(testRepo.path, 'Feature commit', { allowEmpty: true });
      await execGit(testRepo.path, ['checkout', 'main']);
      const result = await mergeBranch(testRepo.path, 'feature');
      expect(result.success).toBe(true);
    });

    it('should return error on merge failure', async () => {
      const { mergeBranch } = await import('./operations');
      const result = await mergeBranch(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('merge');
    });
  });

  describe('setUpstreamBranch() and getUpstreamBranch()', () => {
    it('should set and get upstream branch', async () => {
      const { setUpstreamBranch, getUpstreamBranch } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      await addRemoteFn(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/main', 'HEAD']);
      const setResult = await setUpstreamBranch(testRepo.path, 'origin/main');
      expect(setResult.success).toBe(true);
      const getResult = await getUpstreamBranch(testRepo.path);
      expect(getResult.success).toBe(true);
      expect(getResult.data).toContain('origin/main');
    });

    it('should set upstream for specific branch', async () => {
      const { setUpstreamBranch } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      await addRemoteFn(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['branch', 'feature']);
      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/feature', 'HEAD']);
      const result = await setUpstreamBranch(testRepo.path, 'origin/feature', 'feature');
      expect(result.success).toBe(true);
    });

    it('should fail to get upstream when not set', async () => {
      const { getUpstreamBranch } = await import('./operations');
      const result = await getUpstreamBranch(testRepo.path);
      expect(result.success).toBe(false);
    });

    it('should fail to set upstream with invalid ref', async () => {
      const { setUpstreamBranch } = await import('./operations');
      const result = await setUpstreamBranch(testRepo.path, 'origin/nonexistent');
      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('GIT_ERROR');
      expect(result.error?.message).toBe('Failed to set upstream branch');
    });

    it('should get upstream for specific branch', async () => {
      const { setUpstreamBranch, getUpstreamBranch } = await import('./operations');
      const { addRemote: addRemoteFn } = await import('./remotes');
      await addRemoteFn(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['branch', 'feature']);
      await execGit(testRepo.path, ['update-ref', 'refs/remotes/origin/feature', 'HEAD']);
      await setUpstreamBranch(testRepo.path, 'origin/feature', 'feature');
      const result = await getUpstreamBranch(testRepo.path, 'feature');
      expect(result.success).toBe(true);
      expect(result.data).toContain('origin/feature');
    });
  });
});
