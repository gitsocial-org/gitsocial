/**
 * GitMsg Lists Layer - Protocol-level list operations
 * Handles generic list storage following GitMsg specification
 */

import type { Result } from '../social/types';
import { getCommit, listRefs, readGitRef, writeGitRef } from '../git/operations';
import { execGit } from '../git/exec';
import { log } from '../logger';

/**
 * GitMsg List operations - Generic protocol-level list storage
 */
export const gitMsgList = {
  /**
   * Read list data from GitMsg storage
   * @param workdir - Working directory
   * @param extension - Extension name (e.g., 'social')
   * @param name - List name
   * @returns List data as any type (extension-specific)
   */
  async read<T = unknown>(workdir: string, extension: string, name: string): Promise<Result<T | null>> {
    try {
      const refPath = `refs/gitmsg/${extension}/lists/${name}`;
      log('debug', `[gitMsgList.read] Reading from ref: ${refPath}`);

      // Read the ref to get the commit hash
      const refResult = await readGitRef(workdir, refPath);
      if (!refResult.success || !refResult.data) {
        return { success: true, data: null };
      }

      // Get the commit to read the JSON content
      const commit = await getCommit(workdir, refResult.data.trim());
      if (!commit) {
        return { success: true, data: null };
      }

      // Parse the JSON content
      try {
        const listData = JSON.parse(commit.message) as T;
        log('debug', `[gitMsgList.read] Successfully parsed list data for ${name}`);
        return { success: true, data: listData };
      } catch (parseError) {
        log('warn', `[gitMsgList.read] Failed to parse list JSON for ${name}:`, parseError);
        return { success: true, data: null };
      }
    } catch (error) {
      log('error', `[gitMsgList.read] Error reading list ${name}:`, error);
      return {
        success: false,
        error: {
          code: 'READ_ERROR',
          message: `Failed to read list ${name}`,
          details: error
        }
      };
    }
  },

  /**
   * Write list data to GitMsg storage
   * @param workdir - Working directory
   * @param extension - Extension name (e.g., 'social')
   * @param name - List name
   * @param data - List data to store
   */
  async write<T = unknown>(workdir: string, extension: string, name: string, data: T): Promise<Result<void>> {
    try {
      log('info', `[gitMsgList.write] Starting write of list '${name}' for extension '${extension}'`);
      log('debug', `[gitMsgList.write] Working directory: ${workdir}`);
      log('debug', '[gitMsgList.write] List data:', data);

      // Create JSON content
      const content = JSON.stringify(data, null, 2);
      log('debug', `[gitMsgList.write] JSON content (${content.length} characters):`, content.substring(0, 500) + (content.length > 500 ? '...' : ''));

      // Use the empty tree for commits (we store data in the commit message)
      const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';

      // Create a commit with JSON in the message
      const commitArgs = ['commit-tree', EMPTY_TREE, '-m', content];
      log('debug', `[gitMsgList.write] Creating commit with git command: git ${commitArgs.join(' ')}`);

      const commitResult = await execGit(workdir, commitArgs);
      if (!commitResult.success || !commitResult.data) {
        log('error', '[gitMsgList.write] Failed to create commit:', {
          success: commitResult.success,
          error: commitResult.error,
          stdout: commitResult.data?.stdout,
          stderr: commitResult.data?.stderr
        });
        return {
          success: false,
          error: {
            code: 'COMMIT_ERROR',
            message: `Failed to create list commit: ${commitResult.error?.message || 'unknown error'}`,
            details: commitResult.error
          }
        };
      }

      const commitHash = commitResult.data.stdout.trim();
      log('info', `[gitMsgList.write] Created commit: ${commitHash}`);

      // Update the ref to point to the new commit
      const refPath = `refs/gitmsg/${extension}/lists/${name}`;
      log('info', `[gitMsgList.write] Updating ref '${refPath}' to commit ${commitHash}`);

      const refResult = await writeGitRef(workdir, refPath, commitHash);

      if (!refResult.success) {
        log('error', `[gitMsgList.write] Failed to update ref '${refPath}':`, {
          error: refResult.error,
          commitHash
        });
        return {
          success: false,
          error: {
            code: 'REF_ERROR',
            message: `Failed to update list reference: ${refResult.error?.message || 'unknown error'}`,
            details: refResult.error
          }
        };
      }

      log('info', `[gitMsgList.write] Successfully wrote list '${name}' for extension '${extension}' at ref ${refPath} → ${commitHash}`);
      return { success: true };
    } catch (error) {
      log('error', `[gitMsgList.write] Exception while writing list '${name}':`, error);
      return {
        success: false,
        error: {
          code: 'WRITE_ERROR',
          message: `Failed to write list: ${String(error)}`,
          details: error
        }
      };
    }
  },

  /**
   * Delete a list from GitMsg storage
   * @param workdir - Working directory
   * @param extension - Extension name (e.g., 'social')
   * @param name - List name to delete
   */
  async delete(workdir: string, extension: string, name: string): Promise<Result<void>> {
    try {
      const refPath = `refs/gitmsg/${extension}/lists/${name}`;
      log('debug', `[gitMsgList.delete] Deleting ref: ${refPath}`);

      // Delete the Git ref
      const deleteResult = await execGit(workdir, ['update-ref', '-d', refPath]);

      if (!deleteResult.success) {
        return {
          success: false,
          error: {
            code: 'DELETE_ERROR',
            message: `Failed to delete list ${name}`,
            details: deleteResult.error
          }
        };
      }

      log('info', `[gitMsgList.delete] Successfully deleted list '${name}' for extension '${extension}'`);
      return { success: true };
    } catch (error) {
      log('error', `[gitMsgList.delete] Exception: ${String(error)}`);
      return {
        success: false,
        error: {
          code: 'DELETE_ERROR',
          message: `Failed to delete list: ${String(error)}`,
          details: error
        }
      };
    }
  },

  /**
   * Enumerate all lists for an extension
   * @param workdir - Working directory
   * @param extension - Extension name (e.g., 'social')
   * @returns Array of list names
   */
  async enumerate(workdir: string, extension: string): Promise<Result<string[]>> {
    try {
      const refPrefix = `refs/gitmsg/${extension}/lists/`;
      log('debug', `[gitMsgList.enumerate] Enumerating lists for extension '${extension}'`);

      const refs = await listRefs(workdir, `${extension}/lists/`);

      // Extract list names from ref paths
      const listNames = refs
        .map((ref: string) => ref.replace(refPrefix, ''))
        .filter((name: string) => name.length > 0);

      log('debug', `[gitMsgList.enumerate] Found ${listNames.length} lists for extension '${extension}'`);
      return { success: true, data: listNames };
    } catch (error) {
      log('error', `[gitMsgList.enumerate] Exception: ${String(error)}`);
      return {
        success: false,
        error: {
          code: 'ENUM_ERROR',
          message: `Failed to enumerate lists: ${String(error)}`,
          details: error
        }
      };
    }
  }
};
