import { afterEach, beforeEach, describe, expect, it } from 'vitest';
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
import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';

describe('git/operations', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('git-operations');
    await createTestCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
  });

  afterEach(() => {
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
  });
});
