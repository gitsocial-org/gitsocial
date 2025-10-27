import { git, gitMsgUrl } from '@gitsocial/core';
import { existsSync } from 'fs';
import { join } from 'path';

/**
 * Unified fetch time manager for repository remotes
 * For isolated clones, reads from the clone's git config
 * For workspace remotes, reads from the workspace's git config
 */
export const fetchTimeManager = {
  /**
   * Get the last fetch time for a remote or repository
   * @param repository - The workspace repository path
   * @param remoteName - Remote name (e.g., 'origin') or repository URL
   */
  get: async (repository: string, remoteName: string): Promise<Date | null> => {
    try {
      // Check if this is a repository URL (for isolated clones)
      if (remoteName.startsWith('repository:') || remoteName.includes('://')) {
        // Get storage base from workspace
        const configResult = await git.execGit(repository, ['config', 'gitsocial.storage']);
        if (!configResult.success || !configResult.data?.stdout) {
          return null;
        }

        const storageBase = configResult.data.stdout.trim();
        if (!storageBase) {
          return null;
        }

        // Extract URL from remoteName if it has repository: prefix
        const url = remoteName.startsWith('repository:')
          ? remoteName.substring('repository:'.length)
          : remoteName;

        // Get storage directory for this repository
        const normalizedUrl = gitMsgUrl.normalize(url);
        const storageName = normalizedUrl
          .replace(/^https?:\/\//, '')
          .replace(/\//g, '-')
          .replace(/[^a-zA-Z0-9-]/g, '');
        const storageDir = join(storageBase, 'repositories', storageName);

        // Check if isolated clone exists
        if (!existsSync(storageDir)) {
          return null;
        }

        // Read fetch time from isolated clone's git config
        const result = await git.execGit(
          storageDir,
          ['config', 'gitsocial.lastfetch']
        );

        if (result.success && result.data?.stdout) {
          const dateStr = result.data.stdout.trim();
          const date = new Date(dateStr);
          if (!isNaN(date.getTime())) {
            return date;
          }
        }
      } else {
        // For regular remotes in workspace, read from workspace config
        const result = await git.execGit(
          repository,
          ['config', `remote.${remoteName}.gitsocial-lastfetch`]
        );

        if (result.success && result.data?.stdout) {
          const dateStr = result.data.stdout.trim();
          const date = new Date(dateStr);
          if (!isNaN(date.getTime())) {
            return date;
          }
        }
      }
    } catch {
      // Config key doesn't exist
    }

    return null;
  },

  /**
   * Set the last fetch time for a remote
   * Note: For isolated clones, this is handled automatically by fetchRepository
   */
  set: async (repository: string, remoteName: string, time: Date = new Date()): Promise<void> => {
    // Only set for regular remotes, not for isolated clones
    if (!remoteName.startsWith('repository:') && !remoteName.includes('://')) {
      await git.execGit(
        repository,
        ['config', `remote.${remoteName}.gitsocial-lastfetch`, time.toISOString()]
      );
    }
  },

  /**
   * Remove the fetch time for a remote
   */
  remove: async (repository: string, remoteName: string): Promise<void> => {
    try {
      // Only remove for regular remotes, not for isolated clones
      if (!remoteName.startsWith('repository:') && !remoteName.includes('://')) {
        await git.execGit(
          repository,
          ['config', '--unset', `remote.${remoteName}.gitsocial-lastfetch`]
        );
      }
    } catch {
      // Ignore error if key doesn't exist
    }
  }
};
