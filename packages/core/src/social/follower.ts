/**
 * Followers feature for GitSocial
 *
 * Shows which repositories follow our repository (mutual followers only).
 * Due to GitSocial's distributed architecture, we can only detect followers
 * among repositories we already follow.
 */

import type { Repository, Result } from './types';
import { repository } from './repository';
import { list } from './list';
import { git } from '../git';
import { gitMsgUrl } from '../gitmsg/protocol';
import { log } from '../logger';

/**
 * Followers namespace - Follower detection for repositories
 */
export const follower = {
  get: getFollowers,
  check: isFollower,
  count: getFollowerCount
};

/**
 * Follower information extends Repository with follow metadata
 */
export interface Follower extends Repository {
  followsVia: string; // Which of their lists contains our repository
}

/**
 * Get repositories that follow our repository (mutual followers only)
 *
 * This function checks each repository we follow to see if they have us
 * in any of their lists. No caching - computed fresh on demand.
 *
 * @param workdir - The workspace directory
 * @param options - Optional parameters
 * @returns List of follower repositories
 */
async function getFollowers(
  workdir: string,
  options?: {
    limit?: number;
  }
): Promise<Result<Follower[]>> {
  try {
    // 1. Get my repository URL using git namespace
    const myRepoUrlResult = await git.getOriginUrl(workdir);
    if (!myRepoUrlResult.success || !myRepoUrlResult.data) {
      return {
        success: false,
        error: {
          code: 'NO_ORIGIN',
          message: 'Could not determine repository URL'
        }
      };
    }

    const normalizedMyUrl = gitMsgUrl.normalize(myRepoUrlResult.data);
    log('info', '[getFollowers] My repository URL:', normalizedMyUrl);
    log('info', '[getFollowers] Original URL:', myRepoUrlResult.data);

    // 2. Get all repositories I follow
    const followingResult = await repository.getRepositories(workdir, 'following');
    if (!followingResult.success || !followingResult.data) {
      log('warn', '[getFollowers] No repositories found in following lists');
      return {
        success: true,
        data: [] // No followed repositories means no followers
      };
    }

    log('info', '[getFollowers] Checking', followingResult.data.length, 'followed repositories');
    log('debug', '[getFollowers] Following repositories:', followingResult.data.map(r => r.url));

    // 3. For each repository I follow, check their lists
    const followers: Follower[] = [];

    for (const repo of followingResult.data) {
      try {
        log('info', '[getFollowers] Checking repository:', repo.url);
        log('debug', '[getFollowers] Repository path:', repo.path || 'no local path');

        // Get lists from this repository
        const listsResult = await list.getLists(repo.path || repo.url);
        if (!listsResult.success || !listsResult.data) {
          log('warn', '[getFollowers] Could not get lists from', repo.url, '- Error:', listsResult.error);
          continue;
        }

        log('info', '[getFollowers] Found', listsResult.data.length, 'lists in', repo.url);

        // Check if any list contains my repository
        for (const list of listsResult.data) {
          log('debug', '[getFollowers] Checking list:', list.name, 'with', list.repositories.length, 'repositories');
          log('debug', '[getFollowers] List repositories:', list.repositories);

          const hasMyRepo = list.repositories.some((repoStr: string) => {
            const repoUrl = repoStr.split('#')[0]; // Remove branch part
            const normalized = gitMsgUrl.normalize(repoUrl || '');
            const isMatch = normalized === normalizedMyUrl;
            log('debug', '[getFollowers] Comparing URLs:');
            log('debug', '  - Original repo string:', repoStr);
            log('debug', '  - Extracted URL:', repoUrl);
            log('debug', '  - Normalized:', normalized);
            log('debug', '  - My URL:', normalizedMyUrl);
            log('debug', '  - Match:', isMatch);
            return isMatch;
          });

          if (hasMyRepo) {
            followers.push({
              ...repo,
              followsVia: list.name
            });
            log('info', '[getFollowers] ✓ Found follower:', repo.url, 'via list:', list.name);
            break; // Found in at least one list
          } else {
            log('debug', '[getFollowers] List', list.name, 'does not contain my repository');
          }
        }

        // Apply limit if specified
        if (options?.limit && followers.length >= options.limit) {
          break;
        }
      } catch (error) {
        log('warn', '[getFollowers] Error checking repository', repo.url, ':', error);
        // Continue checking other repositories
      }
    }

    log('info', '[getFollowers] Summary:');
    log('info', '  - My repository:', normalizedMyUrl);
    log('info', '  - Repositories I follow:', followingResult.data.length);
    log('info', '  - Mutual followers found:', followers.length);
    if (followers.length > 0) {
      log('info', '  - Followers:', followers.map(f => `${f.url} (via ${f.followsVia})`).join(', '));
    }

    return {
      success: true,
      data: followers
    };
  } catch (error) {
    log('error', '[getFollowers] Error:', error);
    return {
      success: false,
      error: {
        code: 'GET_FOLLOWERS_ERROR',
        message: 'Failed to get followers',
        details: error
      }
    };
  }
}

/**
 * Check if a specific repository follows us
 *
 * @param workdir - The workspace directory
 * @param repositoryUrl - The repository URL to check
 * @returns True if the repository follows us
 */
async function isFollower(
  workdir: string,
  repositoryUrl: string
): Promise<Result<boolean>> {
  try {
    // Get my repository URL using git namespace
    const myRepoUrlResult = await git.getOriginUrl(workdir);
    if (!myRepoUrlResult.success || !myRepoUrlResult.data) {
      return {
        success: false,
        error: {
          code: 'NO_ORIGIN',
          message: 'Could not determine repository URL'
        }
      };
    }

    const normalizedMyUrl = gitMsgUrl.normalize(myRepoUrlResult.data);

    // Get the target repository's lists
    const listsResult = await list.getLists(repositoryUrl);
    if (!listsResult.success || !listsResult.data) {
      return {
        success: true,
        data: false // Can't get lists means not following
      };
    }

    // Check if any list contains my repository
    for (const list of listsResult.data) {
      const hasMyRepo = list.repositories.some((repoStr: string) => {
        const repoUrl = repoStr.split('#')[0];
        const normalized = gitMsgUrl.normalize(repoUrl || '');
        return normalized === normalizedMyUrl;
      });

      if (hasMyRepo) {
        return {
          success: true,
          data: true
        };
      }
    }

    return {
      success: true,
      data: false
    };
  } catch (error) {
    log('error', '[isFollower] Error:', error);
    return {
      success: false,
      error: {
        code: 'CHECK_FOLLOWER_ERROR',
        message: 'Failed to check if repository is a follower',
        details: error
      }
    };
  }
}

/**
 * Get count of mutual followers
 *
 * @param workdir - The workspace directory
 * @returns Number of mutual followers
 */
async function getFollowerCount(
  workdir: string
): Promise<Result<number>> {
  const followersResult = await getFollowers(workdir);

  if (!followersResult.success) {
    return {
      success: false,
      error: followersResult.error
    };
  }

  return {
    success: true,
    data: followersResult.data?.length || 0
  };
}
