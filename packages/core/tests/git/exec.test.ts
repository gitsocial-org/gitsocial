import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execGit } from '../../src/git/exec';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { GIT_ERROR_CODES as ERROR_CODES } from '../../src/git/errors';

describe('git/exec', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('git-exec');
    await createCommit(testRepo.path, 'Initial commit', { allowEmpty: true });
  });

  afterEach(() => {
    testRepo.cleanup();
  });

  describe('execGit()', () => {
    it('should execute git command successfully with stdout', async () => {
      const result = await execGit(testRepo.path, ['rev-parse', '--show-toplevel']);
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.data.stdout).toContain('gitsocial-test-git-exec');
        expect(result.data.stderr).toBe('');
      }
    });

    it('should trim stdout and stderr whitespace', async () => {
      const result = await execGit(testRepo.path, ['status', '--short']);
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.data.stdout).not.toMatch(/^\s/);
        expect(result.data.stdout).not.toMatch(/\s$/);
      }
    });

    it('should return success even when stderr contains data with exit code 0', async () => {
      const result = await execGit(testRepo.path, ['checkout', '-b', 'test-branch']);
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.data.stderr).toBeTruthy();
      }
    });

    it('should return error for non-zero exit code', async () => {
      const result = await execGit(testRepo.path, ['rev-parse', 'nonexistent-ref']);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.code).toBe(ERROR_CODES.GIT_EXEC_ERROR);
        expect(result.error.message).toBeTruthy();
        expect(result.error.details?.code).not.toBe(0);
        expect(result.error.details?.args).toEqual(['rev-parse', 'nonexistent-ref']);
      }
    });

    it('should include stderr in error message when available', async () => {
      const result = await execGit(testRepo.path, ['checkout', 'nonexistent-branch']);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.code).toBe(ERROR_CODES.GIT_EXEC_ERROR);
        expect(result.error.message).toContain('nonexistent-branch');
        expect(result.error.details?.stderr).toBeTruthy();
      }
    });

    it('should provide message when command fails', async () => {
      const result = await execGit(testRepo.path, ['--invalid-option']);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.code).toBe(ERROR_CODES.GIT_EXEC_ERROR);
        expect(result.error.message).toBeTruthy();
      }
    });

    it('should return error for invalid working directory', async () => {
      const result = await execGit('/this/path/definitely/does/not/exist/12345', ['status']);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect([ERROR_CODES.GIT_ERROR, ERROR_CODES.GIT_EXEC_ERROR]).toContain(result.error.code);
      }
    });

    it('should include command args in error details', async () => {
      const result = await execGit(testRepo.path, ['invalid', 'command', 'args']);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.details?.args).toEqual(['invalid', 'command', 'args']);
      }
    });

    it('should handle commands with no output', async () => {
      await execGit(testRepo.path, ['config', 'user.name', 'Test User']);
      const result = await execGit(testRepo.path, ['config', 'user.name']);
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.data.stdout).toBe('Test User');
      }
    });

    it('should handle multiple sequential commands', async () => {
      const result1 = await execGit(testRepo.path, ['status']);
      expect(result1.success).toBe(true);
      const result2 = await execGit(testRepo.path, ['branch']);
      expect(result2.success).toBe(true);
      const result3 = await execGit(testRepo.path, ['rev-parse', 'HEAD']);
      expect(result3.success).toBe(true);
    });
  });
});
