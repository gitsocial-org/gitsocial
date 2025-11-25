import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtempSync, rmSync } from 'fs';
import {
  checkoutBranch,
  createBranch,
  createCommit,
  createTestRepo,
  getCommitCount,
  getCommitMessage
} from './test-utils';
import { execGit } from '../src/git/exec';

vi.mock('fs');
vi.mock('../src/git/exec');

describe('test-utils', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    const mockMkdtempSync = vi.mocked(mkdtempSync);
    mockMkdtempSync.mockReturnValue('/tmp/gitsocial-test-my-test-abc123');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('createTestRepo()', () => {
    it('should create a test repository', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } });

      const repo = await createTestRepo('my-test');
      expect(repo.path).toBeTruthy();
      expect(repo.cleanup).toBeInstanceOf(Function);
    });

    it('should handle cleanup errors gracefully', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } });

      const repo = await createTestRepo();
      const mockRmSync = vi.mocked(rmSync);
      mockRmSync.mockImplementation(() => {
        throw new Error('Permission denied');
      });

      const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      repo.cleanup();
      expect(consoleWarnSpy).toHaveBeenCalledWith(
        expect.stringContaining('Failed to cleanup test repo'),
        expect.any(Error)
      );
      consoleWarnSpy.mockRestore();
    });
  });

  describe('createCommit()', () => {
    it('should create commit with author option', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } })
        .mockResolvedValueOnce({ success: true, data: { stdout: 'abc123def456789', stderr: '', exitCode: 0 } });

      const hash = await createCommit('/test/repo', 'Test commit', {
        author: { name: 'Custom Author', email: 'custom@example.com' }
      });

      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', [
        'commit',
        '-m',
        'Test commit',
        '--author',
        'Custom Author <custom@example.com>'
      ]);
      expect(hash).toBe('abc123def456');
    });

    it('should throw error when commit fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValueOnce({
        success: false,
        error: { code: 'GIT_ERROR', message: 'Nothing to commit' }
      });

      await expect(createCommit('/test/repo', 'Test')).rejects.toThrow('Failed to create commit: Nothing to commit');
    });

    it('should throw error when getting commit hash fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } })
        .mockResolvedValueOnce({ success: false, error: { code: 'GIT_ERROR', message: 'No HEAD' } });

      await expect(createCommit('/test/repo', 'Test')).rejects.toThrow('Failed to get commit hash');
    });

    it('should throw error when hash data is missing', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } })
        .mockResolvedValueOnce({ success: true, data: undefined });

      await expect(createCommit('/test/repo', 'Test')).rejects.toThrow('Failed to get commit hash');
    });
  });

  describe('createBranch()', () => {
    it('should create and checkout branch by default', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } });

      await createBranch('/test/repo', 'feature-branch');
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['checkout', '-b', 'feature-branch']);
    });

    it('should create branch without checkout when specified', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } });

      await createBranch('/test/repo', 'feature-branch', false);
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['branch', 'feature-branch']);
    });

    it('should throw error when branch creation fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({
        success: false,
        error: { code: 'GIT_ERROR', message: 'Branch already exists' }
      });

      await expect(createBranch('/test/repo', 'main')).rejects.toThrow('Failed to create branch: Branch already exists');
    });
  });

  describe('checkoutBranch()', () => {
    it('should checkout existing branch', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '', exitCode: 0 } });

      await checkoutBranch('/test/repo', 'main');
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['checkout', 'main']);
    });

    it('should throw error when checkout fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({
        success: false,
        error: { code: 'GIT_ERROR', message: 'Branch not found' }
      });

      await expect(checkoutBranch('/test/repo', 'nonexistent')).rejects.toThrow('Failed to checkout branch: Branch not found');
    });
  });

  describe('getCommitCount()', () => {
    it('should return commit count', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: '42\n', stderr: '', exitCode: 0 } });

      const count = await getCommitCount('/test/repo');
      expect(count).toBe(42);
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['rev-list', '--count', 'HEAD']);
    });

    it('should return 0 when command fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({
        success: false,
        error: { code: 'GIT_ERROR', message: 'No commits' }
      });

      const count = await getCommitCount('/test/repo');
      expect(count).toBe(0);
    });

    it('should return 0 when data is missing', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: undefined });

      const count = await getCommitCount('/test/repo');
      expect(count).toBe(0);
    });
  });

  describe('getCommitMessage()', () => {
    it('should return commit message for HEAD', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: 'Initial commit\n', stderr: '', exitCode: 0 } });

      const message = await getCommitMessage('/test/repo');
      expect(message).toBe('Initial commit');
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['log', '-1', '--format=%B', 'HEAD']);
    });

    it('should return commit message for specific ref', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: { stdout: 'Feature commit\n', stderr: '', exitCode: 0 } });

      const message = await getCommitMessage('/test/repo', 'abc123');
      expect(message).toBe('Feature commit');
      expect(mockExecGit).toHaveBeenCalledWith('/test/repo', ['log', '-1', '--format=%B', 'abc123']);
    });

    it('should throw error when command fails', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({
        success: false,
        error: { code: 'GIT_ERROR', message: 'Invalid ref' }
      });

      await expect(getCommitMessage('/test/repo')).rejects.toThrow('Failed to get commit message');
    });

    it('should throw error when data is missing', async () => {
      const mockExecGit = vi.mocked(execGit);
      mockExecGit.mockResolvedValue({ success: true, data: undefined });

      await expect(getCommitMessage('/test/repo')).rejects.toThrow('Failed to get commit message');
    });
  });
});
