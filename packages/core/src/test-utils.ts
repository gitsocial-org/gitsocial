import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { execGit } from './git/exec';

export interface TestRepo {
  path: string;
  cleanup: () => void;
}

export async function createTestRepo(name = 'test-repo'): Promise<TestRepo> {
  const path = mkdtempSync(join(tmpdir(), `gitsocial-test-${name}-`));

  await execGit(path, ['init']);
  await execGit(path, ['config', 'user.name', 'Test User']);
  await execGit(path, ['config', 'user.email', 'test@example.com']);
  await execGit(path, ['config', 'commit.gpgsign', 'false']);

  const cleanup = (): void => {
    try {
      rmSync(path, { recursive: true, force: true });
    } catch (error) {
      console.warn(`Failed to cleanup test repo ${path}:`, error);
    }
  };

  return { path, cleanup };
}

export async function createCommit(
  repoPath: string,
  message: string,
  options?: {
    allowEmpty?: boolean;
    author?: { name: string; email: string };
  }
): Promise<string> {
  const args = ['commit', '-m', message];

  if (options?.allowEmpty) {
    args.push('--allow-empty');
  }

  if (options?.author) {
    args.push('--author', `${options.author.name} <${options.author.email}>`);
  }

  const result = await execGit(repoPath, args);
  if (!result.success) {
    throw new Error(`Failed to create commit: ${result.error?.message}`);
  }

  const hashResult = await execGit(repoPath, ['rev-parse', 'HEAD']);
  if (!hashResult.success || !hashResult.data) {
    throw new Error('Failed to get commit hash');
  }

  return hashResult.data.stdout.trim().substring(0, 12);
}

export async function createBranch(
  repoPath: string,
  branchName: string,
  checkout = true
): Promise<void> {
  const args = checkout ? ['checkout', '-b', branchName] : ['branch', branchName];
  const result = await execGit(repoPath, args);
  if (!result.success) {
    throw new Error(`Failed to create branch: ${result.error?.message}`);
  }
}

export async function checkoutBranch(
  repoPath: string,
  branchName: string
): Promise<void> {
  const result = await execGit(repoPath, ['checkout', branchName]);
  if (!result.success) {
    throw new Error(`Failed to checkout branch: ${result.error?.message}`);
  }
}

export async function getCommitCount(repoPath: string): Promise<number> {
  const result = await execGit(repoPath, ['rev-list', '--count', 'HEAD']);
  if (!result.success || !result.data) {
    return 0;
  }
  return parseInt(result.data.stdout.trim(), 10);
}

export async function getCommitMessage(repoPath: string, ref = 'HEAD'): Promise<string> {
  const result = await execGit(repoPath, ['log', '-1', '--format=%B', ref]);
  if (!result.success || !result.data) {
    throw new Error('Failed to get commit message');
  }
  return result.data.stdout.trim();
}
