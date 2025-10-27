/**
 * Git remote management operations
 */

import type { Result } from './types';
import { GIT_ERROR_CODES as ERROR_CODES } from './errors';
import { execGit } from './exec';

/**
 * Add a git remote
 */
export async function addRemote(
  workdir: string,
  name: string,
  url: string
): Promise<Result<void>> {
  const result = await execGit(workdir, ['remote', 'add', name, url]);

  return {
    success: result.success,
    error: result.error
  };
}

/**
 * Configure a remote with specific settings
 */
export async function configureRemote(
  workdir: string,
  remoteName: string,
  config: {
    partialCloneFilter?: string;
    pushUrl?: string;
    fetchRefspec?: string | string[];
    [key: string]: string | string[] | undefined;
  }
): Promise<Result<void>> {
  try {
    // Configure partial clone filter if specified
    if (config.partialCloneFilter !== undefined) {
      const filterResult = await execGit(workdir, [
        'config',
        `remote.${remoteName}.partialclonefilter`,
        config.partialCloneFilter
      ]);

      if (!filterResult.success) {
        return {
          success: false,
          error: {
            code: ERROR_CODES.GIT_REMOTE_ERROR,
            message: 'Failed to set partial clone filter',
            details: filterResult.error
          }
        };
      }
    }

    // Configure push URL if specified
    if (config.pushUrl !== undefined) {
      const pushResult = await execGit(workdir, [
        'config',
        `remote.${remoteName}.pushurl`,
        config.pushUrl
      ]);

      if (!pushResult.success) {
        return {
          success: false,
          error: {
            code: ERROR_CODES.GIT_REMOTE_ERROR,
            message: 'Failed to set push URL',
            details: pushResult.error
          }
        };
      }
    }

    // Configure fetch refspec(s) if specified
    if (config.fetchRefspec !== undefined) {
      const refspecs = Array.isArray(config.fetchRefspec) ? config.fetchRefspec : [config.fetchRefspec];

      // Store existing refspecs in case we need to restore them
      const existingResult = await execGit(workdir, [
        'config',
        '--get-all',
        `remote.${remoteName}.fetch`
      ]);
      const existingRefspecs = existingResult.success && existingResult.data
        ? existingResult.data.stdout.split('\n').filter(Boolean)
        : [];

      // Clear existing fetch refspecs
      await execGit(workdir, [
        'config',
        '--unset-all',
        `remote.${remoteName}.fetch`
      ]).catch(() => { /* Ignore if doesn't exist */ });

      // Add each new refspec
      for (const refspec of refspecs) {
        const fetchResult = await execGit(workdir, [
          'config',
          '--add',
          `remote.${remoteName}.fetch`,
          refspec
        ]);

        if (!fetchResult.success) {
          // Try to restore existing refspecs on failure
          for (const oldRefspec of existingRefspecs) {
            await execGit(workdir, [
              'config',
              '--add',
              `remote.${remoteName}.fetch`,
              oldRefspec
            ]).catch(() => { /* Best effort */ });
          }

          return {
            success: false,
            error: {
              code: ERROR_CODES.GIT_REMOTE_ERROR,
              message: 'Failed to set fetch refspec',
              details: fetchResult.error
            }
          };
        }
      }
    }

    // Configure any additional settings
    for (const [key, value] of Object.entries(config)) {
      if (key === 'partialCloneFilter' || key === 'pushUrl' || key === 'fetchRefspec') {
        continue; // Already handled
      }

      if (value !== undefined) {
        const configResult = await execGit(workdir, [
          'config',
          `remote.${remoteName}.${key}`,
          String(value)
        ]);

        if (!configResult.success) {
          return {
            success: false,
            error: {
              code: ERROR_CODES.GIT_REMOTE_ERROR,
              message: `Failed to set ${key}`,
              details: configResult.error
            }
          };
        }
      }
    }

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_REMOTE_ERROR,
        message: 'Failed to configure remote',
        details: error
      }
    };
  }
}

/**
 * Fetch from a remote repository
 */
export async function fetchRemote(
  workdir: string,
  remoteName: string,
  options?: {
    shallowSince?: string;
    depth?: number;
    branch?: string;
    jobs?: number;
  }
): Promise<Result<void>> {
  const args = ['fetch', remoteName];

  if (options?.shallowSince) {
    args.push(`--shallow-since=${options.shallowSince}`);
  }

  if (options?.depth) {
    args.push(`--depth=${options.depth}`);
  }

  if (options?.branch) {
    args.push(options.branch);
  }

  if (options?.jobs) {
    args.push(`--jobs=${options.jobs}`);
  }

  const result = await execGit(workdir, args);

  return {
    success: result.success,
    error: result.error
  };
}

/**
 * Remove a git remote
 */
export async function removeRemote(
  workdir: string,
  name: string
): Promise<Result<void>> {
  const result = await execGit(workdir, ['remote', 'remove', name]);

  return {
    success: result.success,
    error: result.error
  };
}

/**
 * List all configured remotes
 */
export async function listRemotes(
  workdir: string
): Promise<Result<Array<{ name: string; url: string }>>> {
  const result = await execGit(workdir, ['remote', '-v']);

  if (!result.success || !result.data) {
    return {
      success: false,
      error: result.error || {
        code: ERROR_CODES.GIT_REMOTE_ERROR,
        message: 'Failed to list remotes'
      }
    };
  }

  // Parse remote output (name\turl (fetch/push))
  const remotes = new Map<string, string>();
  const lines = result.data.stdout.trim().split('\n').filter(Boolean);

  for (const line of lines) {
    const match = line.match(/^([^\t\s]+)\s+([^\t\s]+)\s+\(fetch\)/);
    if (match && match[1] && match[2]) {
      remotes.set(match[1], match[2]);
    }
  }

  return {
    success: true,
    data: Array.from(remotes.entries()).map(([name, url]) => ({ name, url }))
  };
}

/**
 * Get configuration for a specific remote
 */
export async function getRemoteConfig(
  workdir: string,
  remoteName: string
): Promise<Result<Record<string, string>>> {
  const result = await execGit(workdir, [
    'config',
    '--get-regexp',
    `^remote\\.${remoteName}\\.`
  ]);

  if (!result.success || !result.data) {
    return {
      success: false,
      error: result.error || {
        code: ERROR_CODES.GIT_REMOTE_ERROR,
        message: 'Failed to get remote config'
      }
    };
  }

  const config: Record<string, string> = {};
  const lines = result.data.stdout.trim().split('\n').filter(Boolean);

  for (const line of lines) {
    const match = line.match(/^remote\.\S+\.(\S+)\s+(.+)$/);
    if (match && match[1] && match[2]) {
      config[match[1]] = match[2];
    }
  }

  return {
    success: true,
    data: config
  };
}

/**
 * Get the default branch name for a remote repository URL
 */
export async function getRemoteDefaultBranch(
  workdir: string,
  remoteUrl: string
): Promise<Result<string>> {
  try {
    const result = await execGit(workdir, [
      'ls-remote',
      '--symref',
      remoteUrl,
      'HEAD'
    ]);

    if (!result.success || !result.data) {
      return { success: true, data: 'main' };
    }

    // Parse symbolic reference: "ref: refs/heads/master\tHEAD"
    const lines = result.data.stdout.trim().split('\n');
    for (const line of lines) {
      if (line.startsWith('ref: refs/heads/')) {
        const parts = line.split('\t');
        if (parts[0]) {
          const branch = parts[0].replace('ref: refs/heads/', '');
          if (branch) {
            return { success: true, data: branch };
          }
        }
      }
    }

    return { success: true, data: 'main' };
  } catch (error) {
    return { success: true, data: 'main' };
  }
}

/**
 * Get the origin remote URL for a repository
 * Returns "myrepository" for repositories without remotes
 */
export async function getOriginUrl(
  workdir: string
): Promise<Result<string>> {
  const remotesResult = await listRemotes(workdir);

  if (!remotesResult.success || !remotesResult.data) {
    return {
      success: true,
      data: 'myrepository' // Repository with no remotes
    };
  }

  const remotes = remotesResult.data;

  if (remotes.length === 0) {
    return {
      success: true,
      data: 'myrepository' // Repository with no remotes
    };
  }

  // Prefer origin remote
  const origin = remotes.find(r => r.name === 'origin');
  if (origin) {
    return {
      success: true,
      data: origin.url
    };
  }

  // If no origin, try to find any other remote
  const firstRemote = remotes.find(r => r.name !== 'origin');
  if (firstRemote) {
    return {
      success: true,
      data: firstRemote.url
    };
  }

  // If only gitsocial remotes exist, it's effectively without remotes
  return {
    success: true,
    data: 'myrepository'
  };
}
