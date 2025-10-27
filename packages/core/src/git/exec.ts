/**
 * Execute git commands
 */

import { spawn } from 'child_process';
import type { Result } from './types';
import { GIT_ERROR_CODES as ERROR_CODES } from './errors';

/**
 * Execute a git command in a repository
 */
export async function execGit(
  workdir: string,
  args: string[]
): Promise<Result<{ stdout: string; stderr: string }>> {
  return new Promise((resolve) => {
    const git = spawn('git', args, {
      cwd: workdir
    });

    let stdout = '';
    let stderr = '';

    git.stdout.on('data', (data: Buffer) => {
      stdout += data.toString();
    });

    git.stderr.on('data', (data: Buffer) => {
      stderr += data.toString();
    });

    git.on('close', (code: number | null) => {
      if (code === 0) {
        resolve({
          success: true,
          data: { stdout: stdout.trim(), stderr: stderr.trim() }
        });
      } else {
        resolve({
          success: false,
          error: {
            code: ERROR_CODES.GIT_EXEC_ERROR,
            message: stderr.trim() || `Git command failed with code ${code}`,
            details: { code, stderr: stderr.trim(), args }
          }
        });
      }
    });

    git.on('error', (error: Error) => {
      resolve({
        success: false,
        error: {
          code: ERROR_CODES.GIT_ERROR,
          message: `Git spawn error: ${error.message}`,
          details: { originalError: error, args }
        }
      });
    });
  });
}
