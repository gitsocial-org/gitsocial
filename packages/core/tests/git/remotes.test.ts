import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  addRemote,
  configureRemote,
  fetchRemote,
  getOriginUrl,
  getRemoteConfig,
  getRemoteDefaultBranch,
  listRemotes,
  removeRemote
} from '../../src/git/remotes';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../../src/git/exec';
import * as execModule from '../../src/git/exec';
import * as remotesModule from '../../src/git/remotes';

describe('git/remotes', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('git-remotes');
    await createCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
  });

  afterEach(() => {
    testRepo.cleanup();
  });

  describe('addRemote()', () => {
    it('should add a remote', async () => {
      const result = await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.success).toBe(true);
      expect(remotes.data).toEqual([
        { name: 'origin', url: 'https://github.com/user/repo.git' }
      ]);
    });

    it('should add multiple remotes', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.success).toBe(true);
      expect(remotes.data?.length).toBe(2);
      expect(remotes.data).toContainEqual({ name: 'origin', url: 'https://github.com/user/repo.git' });
      expect(remotes.data).toContainEqual({ name: 'upstream', url: 'https://github.com/upstream/repo.git' });
    });

    it('should fail to add duplicate remote', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo1.git');
      const result = await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo2.git');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should add remote with SSH URL', async () => {
      const result = await addRemote(testRepo.path, 'origin', 'git@github.com:user/repo.git');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data?.[0]?.url).toBe('git@github.com:user/repo.git');
    });

    it('should add remote with local path', async () => {
      const result = await addRemote(testRepo.path, 'local', '/tmp/local-repo');
      expect(result.success).toBe(true);
    });
  });

  describe('removeRemote()', () => {
    beforeEach(async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
    });

    it('should remove existing remote', async () => {
      const result = await removeRemote(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data).toEqual([]);
    });

    it('should fail to remove nonexistent remote', async () => {
      const result = await removeRemote(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should remove one of multiple remotes', async () => {
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      const result = await removeRemote(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data?.length).toBe(1);
      expect(remotes.data?.[0]?.name).toBe('upstream');
    });
  });

  describe('listRemotes()', () => {
    it('should return empty array when no remotes', async () => {
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should list single remote', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([
        { name: 'origin', url: 'https://github.com/user/repo.git' }
      ]);
    });

    it('should list multiple remotes', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      await addRemote(testRepo.path, 'fork', 'https://github.com/fork/repo.git');
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(3);
    });

    it('should only return fetch URLs', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['remote', 'set-url', '--push', 'origin', 'git@github.com:user/repo.git']);
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      expect(result.data?.[0]?.url).toBe('https://github.com/user/repo.git');
    });
  });

  describe('configureRemote()', () => {
    beforeEach(async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
    });

    it('should configure partial clone filter', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: 'blob:none'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.partialclonefilter).toBe('blob:none');
    });

    it('should configure push URL', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        pushUrl: 'git@github.com:user/repo.git'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.pushurl).toBe('git@github.com:user/repo.git');
    });

    it('should configure single fetch refspec', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: '+refs/heads/main:refs/remotes/origin/main'
      });
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, [
        'config',
        '--get-all',
        'remote.origin.fetch'
      ]);
      expect(verifyResult.data?.stdout).toContain('refs/heads/main');
    });

    it('should configure multiple fetch refspecs', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: [
          '+refs/heads/main:refs/remotes/origin/main',
          '+refs/heads/develop:refs/remotes/origin/develop'
        ]
      });
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, [
        'config',
        '--get-all',
        'remote.origin.fetch'
      ]);
      expect(verifyResult.data?.stdout).toContain('refs/heads/main');
      expect(verifyResult.data?.stdout).toContain('refs/heads/develop');
    });

    it('should configure multiple settings at once', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: 'blob:none',
        pushUrl: 'git@github.com:user/repo.git',
        fetchRefspec: '+refs/heads/main:refs/remotes/origin/main'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.partialclonefilter).toBe('blob:none');
      expect(config.data?.pushurl).toBe('git@github.com:user/repo.git');
    });

    it('should configure custom setting', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {
        tagopt: '--no-tags'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.tagopt).toBe('--no-tags');
    });

    it('should handle empty config object', async () => {
      const result = await configureRemote(testRepo.path, 'origin', {});
      expect(result.success).toBe(true);
    });

    it('should succeed even for nonexistent remote', async () => {
      const result = await configureRemote(testRepo.path, 'nonexistent', {
        partialCloneFilter: 'blob:none'
      });
      expect(result.success).toBe(true);
    });

    it('should handle setting new fetch refspec', async () => {
      await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: '+refs/heads/*:refs/remotes/origin/*'
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: '+refs/heads/main:refs/remotes/origin/main'
      });
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, [
        'config',
        '--get-all',
        'remote.origin.fetch'
      ]);
      expect(verifyResult.data?.stdout).toContain('refs/heads/main');
    });
  });

  describe('getRemoteConfig()', () => {
    beforeEach(async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
    });

    it('should get basic remote config', async () => {
      const result = await getRemoteConfig(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      expect(result.data?.url).toBe('https://github.com/user/repo.git');
      expect(result.data?.fetch).toBeDefined();
    });

    it('should get config with custom settings', async () => {
      await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: 'blob:none',
        pushUrl: 'git@github.com:user/repo.git'
      });
      const result = await getRemoteConfig(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      expect(result.data?.partialclonefilter).toBe('blob:none');
      expect(result.data?.pushurl).toBe('git@github.com:user/repo.git');
    });

    it('should fail for nonexistent remote', async () => {
      const result = await getRemoteConfig(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should get config for multiple remotes independently', async () => {
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      await configureRemote(testRepo.path, 'origin', { partialCloneFilter: 'blob:none' });
      const originConfig = await getRemoteConfig(testRepo.path, 'origin');
      const upstreamConfig = await getRemoteConfig(testRepo.path, 'upstream');
      expect(originConfig.data?.partialclonefilter).toBe('blob:none');
      expect(upstreamConfig.data?.partialclonefilter).toBeUndefined();
    });
  });

  describe('fetchRemote()', () => {
    let sourceRepo: TestRepo;

    beforeEach(async () => {
      sourceRepo = await createTestRepo('git-remotes-source');
      await createCommit(sourceRepo.path, 'Source commit', { allowEmpty: true });
      await addRemote(testRepo.path, 'origin', sourceRepo.path);
    });

    afterEach(() => {
      sourceRepo.cleanup();
    });

    it('should fetch from remote', async () => {
      const result = await fetchRemote(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, ['show-ref', '--verify', 'refs/remotes/origin/main']);
      expect(verifyResult.success).toBe(true);
    });

    it('should fetch with depth option', async () => {
      const result = await fetchRemote(testRepo.path, 'origin', { depth: 1 });
      expect(result.success).toBe(true);
    });

    it('should fetch specific branch', async () => {
      await execGit(sourceRepo.path, ['checkout', '-b', 'feature']);
      await createCommit(sourceRepo.path, 'Feature commit', { allowEmpty: true });
      const result = await fetchRemote(testRepo.path, 'origin', { branch: 'feature' });
      expect(result.success).toBe(true);
    });

    it('should fetch with shallow-since', async () => {
      const weekAgo = new Date();
      weekAgo.setDate(weekAgo.getDate() - 7);
      const dateStr = weekAgo.toISOString().split('T')[0];
      const result = await fetchRemote(testRepo.path, 'origin', { shallowSince: dateStr });
      expect(result.success).toBe(true);
    });

    it('should fetch with jobs option', async () => {
      const result = await fetchRemote(testRepo.path, 'origin', { jobs: 4 });
      expect(result.success).toBe(true);
    });

    it('should fetch with multiple options', async () => {
      const result = await fetchRemote(testRepo.path, 'origin', {
        depth: 1,
        branch: 'main',
        jobs: 2
      });
      expect(result.success).toBe(true);
    });

    it('should fail for nonexistent remote', async () => {
      const result = await fetchRemote(testRepo.path, 'nonexistent');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should fail for nonexistent branch', async () => {
      const result = await fetchRemote(testRepo.path, 'origin', { branch: 'nonexistent' });
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  describe('getRemoteDefaultBranch()', () => {
    let sourceRepo: TestRepo;

    beforeEach(async () => {
      sourceRepo = await createTestRepo('git-remotes-default-branch');
      await createCommit(sourceRepo.path, 'Initial commit', { allowEmpty: true });
    });

    afterEach(() => {
      sourceRepo.cleanup();
    });

    it('should get default branch from remote', async () => {
      const result = await getRemoteDefaultBranch(testRepo.path, sourceRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should get custom default branch', async () => {
      await execGit(sourceRepo.path, ['checkout', '-b', 'develop']);
      await execGit(sourceRepo.path, ['symbolic-ref', 'HEAD', 'refs/heads/develop']);
      const result = await getRemoteDefaultBranch(testRepo.path, sourceRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('develop');
    });

    it('should fall back to main for invalid URL', async () => {
      const result = await getRemoteDefaultBranch(testRepo.path, 'https://invalid-url.git');
      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should fall back to main when ls-remote fails', async () => {
      const result = await getRemoteDefaultBranch(testRepo.path, '/nonexistent/path');
      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should handle master as default branch', async () => {
      const masterRepo = await createTestRepo('git-remotes-master');
      await execGit(masterRepo.path, ['checkout', '-b', 'master']);
      await createCommit(masterRepo.path, 'Initial', { allowEmpty: true });
      await execGit(masterRepo.path, ['symbolic-ref', 'HEAD', 'refs/heads/master']);
      const result = await getRemoteDefaultBranch(testRepo.path, masterRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('master');
      masterRepo.cleanup();
    });
  });

  describe('getOriginUrl()', () => {
    it('should return myrepository when no remotes', async () => {
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('myrepository');
    });

    it('should return origin URL when origin exists', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('https://github.com/user/repo.git');
    });

    it('should prefer origin over other remotes', async () => {
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await addRemote(testRepo.path, 'fork', 'https://github.com/fork/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('https://github.com/user/repo.git');
    });

    it('should return first remote when no origin', async () => {
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      await addRemote(testRepo.path, 'fork', 'https://github.com/fork/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(['https://github.com/upstream/repo.git', 'https://github.com/fork/repo.git']).toContain(result.data);
    });

    it('should handle SSH URLs', async () => {
      await addRemote(testRepo.path, 'origin', 'git@github.com:user/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('git@github.com:user/repo.git');
    });

    it('should handle local paths', async () => {
      await addRemote(testRepo.path, 'origin', '/tmp/local-repo');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('/tmp/local-repo');
    });

    it('should return myrepository after removing all remotes', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await removeRemote(testRepo.path, 'origin');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('myrepository');
    });
  });

  describe('advanced error handling', () => {
    it('should handle listRemotes with empty output', async () => {
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle invalid workdir in addRemote', async () => {
      const result = await addRemote('/nonexistent/path', 'origin', 'https://github.com/user/repo.git');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should handle invalid workdir in listRemotes', async () => {
      const result = await listRemotes('/nonexistent/path');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should handle invalid workdir in getRemoteConfig', async () => {
      const result = await getRemoteConfig('/nonexistent/path', 'origin');
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    it('should handle configureRemote with custom config keys', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await configureRemote(testRepo.path, 'origin', {
        prune: 'true',
        mirror: 'false'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.prune).toBe('true');
      expect(config.data?.mirror).toBe('false');
    });

    it('should handle configureRemote when setting fetchRefspec with existing refspecs', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['config', '--add', 'remote.origin.fetch', '+refs/heads/*:refs/remotes/origin/*']);
      await execGit(testRepo.path, ['config', '--add', 'remote.origin.fetch', '+refs/tags/*:refs/tags/*']);
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: '+refs/heads/main:refs/remotes/origin/main'
      });
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, ['config', '--get-all', 'remote.origin.fetch']);
      expect(verifyResult.data?.stdout).toContain('refs/heads/main');
      expect(verifyResult.data?.stdout).not.toContain('refs/heads/*');
    });

    it('should handle listRemotes output with special characters in URL', async () => {
      await addRemote(testRepo.path, 'special', 'https://user:password@github.com/user/repo.git');
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.find(r => r.name === 'special')).toBeDefined();
    });

    it('should handle getRemoteConfig with empty result', async () => {
      await addRemote(testRepo.path, 'minimal', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['config', '--unset-all', 'remote.minimal.fetch']);
      const result = await getRemoteConfig(testRepo.path, 'minimal');
      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should handle addRemote with relative path URL', async () => {
      const result = await addRemote(testRepo.path, 'relative', '../relative-repo');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data?.find(r => r.name === 'relative')).toBeDefined();
    });

    it('should handle addRemote with file:// URL', async () => {
      const result = await addRemote(testRepo.path, 'file', 'file:///tmp/repo.git');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data?.find(r => r.name === 'file')?.url).toBe('file:///tmp/repo.git');
    });

    it('should handle configureRemote with tagopt', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await configureRemote(testRepo.path, 'origin', {
        tagopt: '--no-tags'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.tagopt).toBe('--no-tags');
    });

    it('should handle getOriginUrl with only non-origin remotes', async () => {
      await addRemote(testRepo.path, 'upstream', 'https://github.com/upstream/repo.git');
      await addRemote(testRepo.path, 'fork', 'https://github.com/fork/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBeTruthy();
      expect(result.data).not.toBe('myrepository');
    });

    it('should handle getRemoteDefaultBranch with unusual branch names', async () => {
      const branchRepo = await createTestRepo('unusual-branch');
      await createCommit(branchRepo.path, 'Initial', { allowEmpty: true });
      await execGit(branchRepo.path, ['checkout', '-b', 'feature/test-123']);
      await execGit(branchRepo.path, ['symbolic-ref', 'HEAD', 'refs/heads/feature/test-123']);
      const result = await getRemoteDefaultBranch(testRepo.path, branchRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('feature/test-123');
      branchRepo.cleanup();
    });

    it('should handle fetchRemote with depth and branch options', async () => {
      const sourceRepo = await createTestRepo('all-options');
      await createCommit(sourceRepo.path, 'Source', { allowEmpty: true });
      await addRemote(testRepo.path, 'all', sourceRepo.path);
      const result = await fetchRemote(testRepo.path, 'all', {
        depth: 1,
        branch: 'main'
      });
      expect(result.success).toBe(true);
      sourceRepo.cleanup();
    });

    it('should handle configureRemote with multiple settings', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await configureRemote(testRepo.path, 'origin', {
        tagopt: '--no-tags',
        prune: 'true',
        mirror: 'false'
      });
      expect(result.success).toBe(true);
      const config = await getRemoteConfig(testRepo.path, 'origin');
      expect(config.data?.tagopt).toBe('--no-tags');
      expect(config.data?.prune).toBe('true');
      expect(config.data?.mirror).toBe('false');
    });
  });

  describe('edge cases and error handling', () => {
    it('should handle listRemotes with push and fetch URLs', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['remote', 'set-url', '--add', '--push', 'origin', 'git@github.com:user/repo.git']);
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data?.length).toBe(1);
      expect(result.data?.[0]?.url).toBe('https://github.com/user/repo.git');
    });

    it('should handle getRemoteConfig with multiple fetch refspecs', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      await execGit(testRepo.path, ['config', '--add', 'remote.origin.fetch', '+refs/heads/develop:refs/remotes/origin/develop']);
      const result = await getRemoteConfig(testRepo.path, 'origin');
      expect(result.success).toBe(true);
      expect(result.data).toBeDefined();
    });

    it('should handle getRemoteDefaultBranch with no symbolic ref', async () => {
      const result = await getRemoteDefaultBranch(testRepo.path, 'file:///nonexistent/repo');
      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should handle configureRemote with undefined values in config', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: undefined,
        pushUrl: undefined,
        fetchRefspec: undefined
      });
      expect(result.success).toBe(true);
    });

    it('should handle fetchRemote with invalid shallow-since date', async () => {
      const sourceRepo = await createTestRepo('git-remotes-invalid-date');
      await createCommit(sourceRepo.path, 'Source commit', { allowEmpty: true });
      await addRemote(testRepo.path, 'test', sourceRepo.path);
      const result = await fetchRemote(testRepo.path, 'test', { shallowSince: '1970-01-01' });
      expect(result.success).toBe(true);
      sourceRepo.cleanup();
    });

    it('should handle removeRemote with special characters in name', async () => {
      await addRemote(testRepo.path, 'test-remote', 'https://example.com/repo.git');
      const result = await removeRemote(testRepo.path, 'test-remote');
      expect(result.success).toBe(true);
      const remotes = await listRemotes(testRepo.path);
      expect(remotes.data?.find(r => r.name === 'test-remote')).toBeUndefined();
    });

    it('should handle configureRemote with array of single fetchRefspec', async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: ['+refs/heads/main:refs/remotes/origin/main']
      });
      expect(result.success).toBe(true);
      const verifyResult = await execGit(testRepo.path, ['config', '--get-all', 'remote.origin.fetch']);
      expect(verifyResult.data?.stdout).toContain('refs/heads/main');
    });

    it('should handle getOriginUrl when listRemotes returns success but empty data', async () => {
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('myrepository');
    });
  });

  describe('error path coverage', () => {
    beforeEach(async () => {
      await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it('should handle failure when setting partial clone filter', async () => {
      const originalExecGit = execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args.includes('remote.origin.partialclonefilter')) {
          return { success: false, error: { code: 'ERROR', message: 'Config failed' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: 'blob:none'
      });
      expect(result.success).toBe(false);
      expect(result.error?.message).toBe('Failed to set partial clone filter');
    });

    it('should handle failure when setting push URL', async () => {
      const originalExecGit = execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args.includes('remote.origin.pushurl')) {
          return { success: false, error: { code: 'ERROR', message: 'Config failed' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        pushUrl: 'git@github.com:user/repo.git'
      });
      expect(result.success).toBe(false);
      expect(result.error?.message).toBe('Failed to set push URL');
    });

    it('should handle failure when setting custom config properties', async () => {
      const originalExecGit = execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(async (workdir, args) => {
        if (args.includes('remote.origin.customKey')) {
          return { success: false, error: { code: 'ERROR', message: 'Config failed' } };
        }
        return originalExecGit(workdir, args);
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        customKey: 'customValue'
      });
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('Failed to set');
    });

    it('should handle failure when adding fetch refspec and restore old refspecs', async () => {
      await execGit(testRepo.path, ['config', '--add', 'remote.origin.fetch', '+refs/heads/*:refs/remotes/origin/*']);
      const originalExecGit = execGit;
      const spy = vi.spyOn(execModule, 'execGit');
      let addCallCount = 0;
      spy.mockImplementation(async (workdir, args) => {
        if (args.includes('--add') && args.includes('remote.origin.fetch')) {
          addCallCount++;
          if (addCallCount === 1) {
            return { success: false, error: { code: 'ERROR', message: 'Config failed' } };
          }
        }
        return originalExecGit(workdir, args);
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        fetchRefspec: '+refs/heads/main:refs/remotes/origin/main'
      });
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('fetch refspec');
    });

    it('should handle exception thrown in configureRemote', async () => {
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(() => {
        throw new Error('Unexpected error');
      });
      const result = await configureRemote(testRepo.path, 'origin', {
        partialCloneFilter: 'blob:none'
      });
      expect(result.success).toBe(false);
      expect(result.error?.message).toBe('Failed to configure remote');
    });

    it('should handle listRemotes when execGit returns no data', async () => {
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockResolvedValueOnce({ success: false, data: undefined });
      const result = await listRemotes(testRepo.path);
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('list remotes');
    });

    it('should handle getRemoteConfig when execGit returns no data', async () => {
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockResolvedValueOnce({ success: false, data: undefined });
      const result = await getRemoteConfig(testRepo.path, 'origin');
      expect(result.success).toBe(false);
      expect(result.error?.message).toContain('get remote config');
    });

    it('should handle getRemoteDefaultBranch when exception is thrown', async () => {
      const spy = vi.spyOn(execModule, 'execGit');
      spy.mockImplementation(() => {
        throw new Error('Network error');
      });
      const result = await getRemoteDefaultBranch(testRepo.path, 'https://github.com/user/repo.git');
      expect(result.success).toBe(true);
      expect(result.data).toBe('main');
    });

    it('should handle getOriginUrl when listRemotes fails', async () => {
      await execGit(testRepo.path, ['remote', 'remove', 'origin']);
      const spy = vi.spyOn(remotesModule, 'listRemotes');
      spy.mockResolvedValueOnce({
        success: false,
        error: { code: 'ERROR', message: 'Failed to list' }
      });
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(result.data).toBe('myrepository');
    });

    it('should handle getOriginUrl with gitsocial remotes', async () => {
      await execGit(testRepo.path, ['remote', 'remove', 'origin']);
      await addRemote(testRepo.path, 'gitsocial-alice', 'https://github.com/alice/repo.git');
      await addRemote(testRepo.path, 'gitsocial-bob', 'https://github.com/bob/repo.git');
      const result = await getOriginUrl(testRepo.path);
      expect(result.success).toBe(true);
      expect(['https://github.com/alice/repo.git', 'https://github.com/bob/repo.git']).toContain(result.data);
    });
  });
});
