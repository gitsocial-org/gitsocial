/**
 * GitSocial configuration management via Git refs
 * Configuration is stored as JSON in refs/gitmsg/social/config
 */

import type { Result } from './types';
import { execGit } from '../git/exec';
import { log } from '../logger';

/**
 * GitSocial configuration stored in refs
 */
export interface GitSocialConfig {
  version: string;
  branch?: string;  // The branch for all social content
  // Extensible for future configuration needs
}

/**
 * Default configuration values
 */
const DEFAULT_CONFIG: GitSocialConfig = {
  version: '0.1.0',
  branch: 'gitsocial'
};

/**
 * Get GitSocial configuration from refs
 */
export async function getGitSocialConfig(workdir: string): Promise<GitSocialConfig | null> {
  try {
    const refResult = await execGit(workdir, [
      'rev-parse',
      '--verify',
      'refs/gitmsg/social/config'
    ]);

    if (!refResult.success || !refResult.data) {
      log('debug', '[getGitSocialConfig] No config found in refs');
      return null;
    }

    const commitHash = refResult.data.stdout.trim();

    const messageResult = await execGit(workdir, [
      'log',
      '-1',
      '--format=%B',
      commitHash
    ]);

    if (!messageResult.success || !messageResult.data) {
      log('error', '[getGitSocialConfig] Failed to read config commit');
      return null;
    }

    try {
      const config = JSON.parse(messageResult.data.stdout.trim()) as GitSocialConfig;
      log('debug', '[getGitSocialConfig] Loaded from refs:', config);
      return config;
    } catch (error) {
      log('error', '[getGitSocialConfig] Failed to parse config JSON:', error);
      return null;
    }
  } catch (error) {
    log('error', '[getGitSocialConfig] Error loading config:', error);
    return null;
  }
}

/**
 * Set GitSocial configuration in refs
 */
export async function setGitSocialConfig(
  workdir: string,
  config: GitSocialConfig
): Promise<Result<void>> {
  try {
    if (!config.version) {
      config.version = DEFAULT_CONFIG.version;
    }

    const configJson = JSON.stringify(config, null, 2);

    // Create a commit with the config as the message
    // Using empty tree for config commits
    const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
    const commitResult = await execGit(workdir, [
      'commit-tree',
      EMPTY_TREE,
      '-m',
      configJson
    ]);

    if (!commitResult.success || !commitResult.data) {
      return {
        success: false,
        error: {
          code: 'CONFIG_COMMIT_ERROR',
          message: 'Failed to create config commit',
          details: commitResult.error
        }
      };
    }

    const commitHash = commitResult.data.stdout.trim();

    // Update the ref to point to the new config commit
    const refResult = await execGit(workdir, [
      'update-ref',
      'refs/gitmsg/social/config',
      commitHash
    ]);

    if (!refResult.success) {
      return {
        success: false,
        error: {
          code: 'CONFIG_REF_ERROR',
          message: 'Failed to update config ref',
          details: refResult.error
        }
      };
    }

    log('info', '[setGitSocialConfig] Config saved to refs:', config);
    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'SET_CONFIG_ERROR',
        message: 'Failed to set GitSocial config',
        details: error
      }
    };
  }
}

/**
 * Get the configured GitSocial branch
 * Returns the branch name where all social content should be stored
 */
export async function getConfiguredBranch(workdir: string): Promise<string> {
  // 1. Check refs/gitmsg/social/config (source of truth)
  const refResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    'refs/gitmsg/social/config'
  ]);

  if (refResult.success && refResult.data) {
    const commitHash = refResult.data.stdout.trim();
    const messageResult = await execGit(workdir, [
      'log',
      '-1',
      '--format=%B',
      commitHash
    ]);

    if (messageResult.success && messageResult.data) {
      try {
        const config = JSON.parse(messageResult.data.stdout.trim()) as GitSocialConfig;
        if (config.branch) {
          log('debug', `[getConfiguredBranch] Using refs config: ${config.branch}`);
          return config.branch;
        }
      } catch {
        // Ignore JSON parse errors
      }
    }
  }

  // 2. Check if 'gitsocial' branch exists as convention
  const branchResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    'refs/heads/gitsocial'
  ]);

  if (branchResult.success) {
    log('debug', '[getConfiguredBranch] Using convention: gitsocial branch exists');
    return 'gitsocial';
  }

  // 3. Use default branch
  const defaultBranchResult = await execGit(workdir, [
    'symbolic-ref',
    'refs/remotes/origin/HEAD'
  ]);

  if (defaultBranchResult.success && defaultBranchResult.data) {
    const branch = defaultBranchResult.data.stdout.trim().replace('refs/remotes/origin/', '');
    log('debug', `[getConfiguredBranch] Using default branch: ${branch}`);
    return branch;
  }

  // 4. Fallback to 'main'
  log('debug', '[getConfiguredBranch] Using fallback: main');
  return 'main';
}

/**
 * Fetch GitSocial config from a remote repository
 * Used when following/adding remote repositories
 */
export async function fetchRemoteConfig(
  workdir: string,
  remoteUrl: string
): Promise<GitSocialConfig | null> {
  try {
    // Try to fetch the config ref
    const fetchResult = await execGit(workdir, [
      'fetch',
      remoteUrl,
      'refs/gitmsg/social/config:refs/remotes/temp-config'
    ]);

    if (!fetchResult.success) {
      log('debug', `[fetchRemoteConfig] No config ref in remote: ${remoteUrl}`);
      return null;
    }

    // Read the fetched config
    const messageResult = await execGit(workdir, [
      'log',
      '-1',
      '--format=%B',
      'refs/remotes/temp-config'
    ]);

    // Clean up temp ref
    await execGit(workdir, [
      'update-ref',
      '-d',
      'refs/remotes/temp-config'
    ]);

    if (!messageResult.success || !messageResult.data) {
      return null;
    }

    try {
      const config = JSON.parse(messageResult.data.stdout.trim()) as GitSocialConfig;
      log('debug', `[fetchRemoteConfig] Fetched config from ${remoteUrl}:`, config);
      return config;
    } catch {
      return null;
    }
  } catch (error) {
    log('error', '[fetchRemoteConfig] Error fetching remote config:', error);
    return null;
  }
}

/**
 * Initialize GitSocial with optional branch specification
 * If branchName is not provided, uses auto-detection via getConfiguredBranch()
 */
export async function initializeGitSocial(
  workdir: string,
  branchName?: string
): Promise<Result<void>> {
  // Only set config if branchName is explicitly provided
  if (branchName) {
    const config: GitSocialConfig = {
      version: DEFAULT_CONFIG.version,
      branch: branchName
    };

    const configResult = await setGitSocialConfig(workdir, config);
    if (!configResult.success) {
      return configResult;
    }

    // Create the branch if it doesn't exist
    const branchExistsResult = await execGit(workdir, [
      'rev-parse',
      '--verify',
      `refs/heads/${branchName}`
    ]);

    if (!branchExistsResult.success) {
      // Check if repository is empty (no HEAD)
      const headResult = await execGit(workdir, ['rev-parse', 'HEAD']);
      if (!headResult.success) {
        // Repository is empty (no commits), skip branch creation
        // The branch will be created when the first post is made
        log('info', '[initializeGitSocial] Repository is empty, skipping branch creation');
        log('info', `[initializeGitSocial] Initialized with branch: ${branchName} (will be created on first use)`);
        return { success: true };
      }

      if (!headResult.data) {
        return {
          success: false,
          error: {
            code: 'HEAD_ERROR',
            message: 'Failed to get current HEAD'
          }
        };
      }

      // Create the branch from current HEAD
      const createBranchResult = await execGit(workdir, [
        'branch',
        branchName,
        headResult.data.stdout.trim()
      ]);

      if (!createBranchResult.success) {
        return {
          success: false,
          error: {
            code: 'BRANCH_CREATE_ERROR',
            message: `Failed to create branch ${branchName}`,
            details: createBranchResult.error
          }
        };
      }

      log('info', `[initializeGitSocial] Created branch: ${branchName}`);
    }

    log('info', `[initializeGitSocial] Initialized with explicit branch: ${branchName}`);
  } else {
    // No explicit branch - use auto-detection
    log('info', '[initializeGitSocial] Initialized with auto-detection (no config written)');
  }

  return { success: true };
}
