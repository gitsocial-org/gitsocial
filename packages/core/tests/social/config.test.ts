import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  fetchRemoteConfig,
  getConfiguredBranch,
  getGitSocialConfig,
  initializeGitSocial,
  setGitSocialConfig
} from '../../src/social/config';
import { createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../../src/git/exec';
import * as execGitModule from '../../src/git/exec';

describe('social/config', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('config-test');
  });

  afterEach(() => {
    vi.restoreAllMocks();
    testRepo.cleanup();
  });

  describe('getConfiguredBranch()', () => {
    it('should return default branch when no config', async () => {
      const branch = await getConfiguredBranch(testRepo.path);

      expect(branch).toBe('main');
    });

    it('should return configured branch from refs', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);
      await initializeGitSocial(testRepo.path, 'custom-branch');

      const branch = await getConfiguredBranch(testRepo.path);

      expect(branch).toBe('custom-branch');
    });

    it('should return gitsocial when branch exists', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);
      await execGit(testRepo.path, ['branch', 'gitsocial']);

      const branch = await getConfiguredBranch(testRepo.path);

      expect(branch).toBe('gitsocial');
    });

    it('should fallback when config JSON is invalid', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const commitResult = await execGit(testRepo.path, [
        'commit-tree',
        EMPTY_TREE,
        '-m',
        'invalid json {'
      ]);
      const commitHash = commitResult.data!.stdout.trim();
      await execGit(testRepo.path, [
        'update-ref',
        'refs/gitmsg/social/config',
        commitHash
      ]);

      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('main');
    });

    it('should use origin/HEAD when available', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);
      await execGit(testRepo.path, ['branch', 'develop']);
      await execGit(testRepo.path, ['remote', 'add', 'origin', 'https://github.com/test/repo.git']);
      await execGit(testRepo.path, ['symbolic-ref', 'refs/remotes/origin/HEAD', 'refs/remotes/origin/develop']);

      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('develop');
    });
  });

  describe('setGitSocialConfig()', () => {
    it('should set config in refs', async () => {
      const result = await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'gitsocial'
      });

      expect(result.success).toBe(true);

      const refResult = await execGit(testRepo.path, [
        'rev-parse',
        '--verify',
        'refs/gitmsg/social/config'
      ]);
      expect(refResult.success).toBe(true);
    });

    it('should add version if not provided', async () => {
      const result = await setGitSocialConfig(testRepo.path, {
        version: '',
        branch: 'gitsocial'
      });

      expect(result.success).toBe(true);
    });

    it('should return error when commit creation fails', async () => {
      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit-tree') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'commit-tree failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'test'
      });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CONFIG_COMMIT_ERROR');
    });

    it('should return error when ref update fails', async () => {
      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'update-ref' && args[1] === 'refs/gitmsg/social/config') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'update-ref failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'test'
      });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CONFIG_REF_ERROR');
    });

    it('should return error on unexpected exception', async () => {
      const spy = vi.spyOn(execGitModule, 'execGit');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const result = await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'test'
      });

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('SET_CONFIG_ERROR');
    });
  });

  describe('getGitSocialConfig()', () => {
    it('should return null when no config exists', async () => {
      const config = await getGitSocialConfig(testRepo.path);

      expect(config).toBeNull();
    });

    it('should get config from refs', async () => {
      await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'social-branch'
      });

      const config = await getGitSocialConfig(testRepo.path);

      expect(config).not.toBeNull();
      expect(config?.branch).toBe('social-branch');
    });

    it('should return null when commit read fails', async () => {
      await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'test-branch'
      });

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'log' && args[1] === '-1') {
          return { success: false };
        }
        return originalExecGit(workdir, args);
      });

      const config = await getGitSocialConfig(testRepo.path);
      expect(config).toBeNull();
    });

    it('should return null when config JSON is invalid', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const commitResult = await execGit(testRepo.path, [
        'commit-tree',
        EMPTY_TREE,
        '-m',
        'invalid json {'
      ]);
      const commitHash = commitResult.data!.stdout.trim();
      await execGit(testRepo.path, [
        'update-ref',
        'refs/gitmsg/social/config',
        commitHash
      ]);

      const config = await getGitSocialConfig(testRepo.path);
      expect(config).toBeNull();
    });

    it('should return null on unexpected error', async () => {
      const spy = vi.spyOn(execGitModule, 'execGit');
      spy.mockRejectedValue(new Error('Unexpected git error'));

      const config = await getGitSocialConfig(testRepo.path);
      expect(config).toBeNull();
    });
  });

  describe('fetchRemoteConfig()', () => {
    it('should return null when remote has no config ref', async () => {
      const result = await fetchRemoteConfig(testRepo.path, 'https://github.com/test/no-config.git');
      expect(result).toBeNull();
    });

    it('should return null when fetch succeeds but commit read fails', async () => {
      await setGitSocialConfig(testRepo.path, {
        version: '0.1.0',
        branch: 'test-branch'
      });

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');
      let fetchCalled = false;

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'fetch') {
          fetchCalled = true;
          return { success: true, data: { stdout: '', stderr: '' } };
        }
        if (fetchCalled && args[0] === 'log' && args[1] === '-1') {
          return { success: false };
        }
        return originalExecGit(workdir, args);
      });

      const result = await fetchRemoteConfig(testRepo.path, 'https://github.com/test/repo.git');
      expect(result).toBeNull();
    });

    it('should return null when remote config has invalid JSON', async () => {
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
      const commitResult = await execGit(testRepo.path, [
        'commit-tree',
        EMPTY_TREE,
        '-m',
        'invalid json {'
      ]);
      const commitHash = commitResult.data!.stdout.trim();
      await execGit(testRepo.path, [
        'update-ref',
        'refs/gitmsg/social/config',
        commitHash
      ]);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'fetch') {
          return { success: true, data: { stdout: '', stderr: '' } };
        }
        if (args[0] === 'log' && args[1] === '-1' && args[3] === 'refs/remotes/temp-config') {
          return {
            success: true,
            data: { stdout: 'invalid json {', stderr: '' }
          };
        }
        return originalExecGit(workdir, args);
      });

      const result = await fetchRemoteConfig(testRepo.path, 'https://github.com/test/repo.git');
      expect(result).toBeNull();
    });

    it('should return null on unexpected error', async () => {
      const spy = vi.spyOn(execGitModule, 'execGit');
      spy.mockRejectedValue(new Error('Unexpected error'));

      const result = await fetchRemoteConfig(testRepo.path, 'https://github.com/test/repo.git');
      expect(result).toBeNull();
    });
  });

  describe('initializeGitSocial()', () => {
    it('should initialize with auto-detection (no config written)', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const result = await initializeGitSocial(testRepo.path);

      expect(result.success).toBe(true);

      // Should NOT write config when branchName is undefined
      const configResult = await execGit(testRepo.path, [
        'rev-parse',
        '--verify',
        'refs/gitmsg/social/config'
      ]);
      expect(configResult.success).toBe(false);

      // Should NOT create gitsocial branch (uses auto-detection)
      const branch = await getConfiguredBranch(testRepo.path);
      expect(branch).toBe('main');
    });

    it('should initialize with custom branch', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const result = await initializeGitSocial(testRepo.path, 'my-social');

      expect(result.success).toBe(true);

      const branchExists = await execGit(testRepo.path, [
        'rev-parse',
        '--verify',
        'refs/heads/my-social'
      ]);
      expect(branchExists.success).toBe(true);
    });

    it('should handle empty repository', async () => {
      const result = await initializeGitSocial(testRepo.path);

      expect(result.success).toBe(true);
    });

    it('should set config in refs', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);
      await initializeGitSocial(testRepo.path, 'social');

      const config = await getGitSocialConfig(testRepo.path);
      expect(config).not.toBeNull();
      expect(config?.branch).toBe('social');
    });

    it('should not create duplicate branch', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);
      await execGit(testRepo.path, ['branch', 'gitsocial']);

      const result = await initializeGitSocial(testRepo.path);

      expect(result.success).toBe(true);
    });

    it('should return error when config save fails', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit-tree') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'commit-tree failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await initializeGitSocial(testRepo.path, 'test-branch');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('CONFIG_COMMIT_ERROR');
    });

    it('should use fallback when current branch detection fails', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');
      let checkoutCalled = false;

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'rev-parse' && args[1] === '--abbrev-ref' && args[2] === 'HEAD') {
          return { success: false };
        }
        if (args[0] === 'checkout' && args[1] === '--orphan') {
          checkoutCalled = true;
        }
        return originalExecGit(workdir, args);
      });

      const result = await initializeGitSocial(testRepo.path, 'new-branch');

      expect(result.success).toBe(true);
      expect(checkoutCalled).toBe(true);
    });

    it('should return error when orphan checkout fails', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'checkout' && args[1] === '--orphan') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'checkout failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await initializeGitSocial(testRepo.path, 'test-branch');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('BRANCH_CREATE_ERROR');
    });

    it('should return error when initial commit fails', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');
      let orphanCreated = false;

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'checkout' && args[1] === '--orphan') {
          orphanCreated = true;
        }
        if (orphanCreated && args[0] === 'commit' && args[1] === '--allow-empty') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'commit failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await initializeGitSocial(testRepo.path, 'test-branch');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('COMMIT_ERROR');
    });

    it('should log warning when switch back fails but still succeed', async () => {
      await execGit(testRepo.path, ['commit', '--allow-empty', '-m', 'Initial']);

      const originalExecGit = execGitModule.execGit;
      const spy = vi.spyOn(execGitModule, 'execGit');
      let commitSucceeded = false;

      spy.mockImplementation(async (workdir, args) => {
        if (args[0] === 'commit' && args[1] === '--allow-empty' && args[3]?.startsWith('Initialize')) {
          commitSucceeded = true;
        }
        if (commitSucceeded && args[0] === 'checkout' && args[1] === '-f') {
          return { success: false, error: { code: 'GIT_ERROR', message: 'checkout failed' } };
        }
        return originalExecGit(workdir, args);
      });

      const result = await initializeGitSocial(testRepo.path, 'test-branch');

      expect(result.success).toBe(true);
    });
  });
});
