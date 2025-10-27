import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  getGitSocialConfig,
  initializeGitSocial,
  setGitSocialConfig
} from './config';
import { getConfiguredBranch } from '../git/operations';
import { createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../git/exec';

describe('social/config', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('config-test');
  });

  afterEach(() => {
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
  });
});
