/**
 * Repository management functions for GitSocial
 */

// Built-in modules
import { existsSync, readdirSync } from 'fs';
import { join } from 'path';

// Types
import type { Repository, RepositoryFilter, Result } from './types';

// Cross-layer imports
import { execGit } from '../git/exec';
import { getConfiguredBranch } from '../git/operations';
import { log } from '../logger';
import { gitMsgRef, gitMsgUrl } from '../gitmsg/protocol';
import { gitHost } from '../githost';
import { storage } from '../storage';

// Same-layer imports
import { list } from './list';
import { getGitSocialConfig, initializeGitSocial } from './config';
import { cache } from './post/cache';

// Storage configuration
let storageBase: string | undefined;

/**
 * Repository namespace - handles repository management operations
 */
export const repository = {
  /**
   * Initialize the repository system with storage configuration
   * Must be called before using repository storage features
   */
  initialize(config: { storageBase: string }): void {
    initializeRepositorySystem(config);
  },

  /**
   * Get repositories using the isolated storage system
   */
  async getRepositories(
    workdir: string,
    scope: string = 'workspace:my',
    filter?: RepositoryFilter
  ): Promise<Result<Repository[]>> {
    const storageDir = storageBase || getConfiguredStorageBase();
    return getRepositories(workdir, scope, filter, storageDir);
  },

  /**
   * Fetch updates for repositories in the specified scope
   */
  async fetchUpdates(
    workdir: string,
    scope: string = 'following',
    options?: { branch?: string; since?: string }
  ): Promise<Result<{ fetched: number; failed: number }>> {
    const storageDir = getConfiguredStorageBase();
    if (!storageDir) {
      return {
        success: false,
        error: {
          code: 'NOT_INITIALIZED',
          message: 'Repository system not initialized. Call repository.initialize() first.'
        }
      };
    }

    const reposResult = await getRepositories(workdir, scope, undefined, storageDir);

    if (!reposResult.success || !reposResult.data) {
      return {
        success: false,
        error: reposResult.error || {
          code: 'NO_REPOSITORIES',
          message: 'No repositories found'
        }
      };
    }

    let fetched = 0;
    let failed = 0;
    const failureDetails: Array<{ url: string; branch?: string; error: string }> = [];
    const fetchedRepositories: string[] = [];

    for (const repo of reposResult.data) {
      if (repo.url) {
        // Fetch any repository that has a URL (remote or workspace with remotes)
        if (!repo.branch) {
          failed++;
          failureDetails.push({
            url: repo.url,
            error: 'Repository missing branch information'
          });
          log('error', `[fetchUpdates] Repository missing branch: ${repo.url}`);
          continue;
        }
        const ensureResult = await storage.repository.ensure(storageDir, repo.url, repo.branch, {
          isPersistent: true,  // List repositories should be persistent
          force: false
        });

        if (!ensureResult.success) {
          failed++;
          failureDetails.push({
            url: repo.url,
            branch: repo.branch,
            error: ensureResult.error?.message || 'Failed to clone/ensure repository'
          });
          log('warn', `[fetchUpdates] Failed to ensure repository: ${repo.url}#branch:${repo.branch}`, ensureResult.error);
          continue;
        }

        // Now fetch updates (this will be quick if we just cloned)
        // Use branch from options if provided, otherwise use repo.branch
        const branchToFetch = options?.branch || repo.branch;
        const result = await storage.repository.fetch(storageDir, repo.url, branchToFetch,
          options?.since ? { since: options.since } : undefined);
        if (result.success) {
          // Check if it was actually fetched or skipped
          if (result.data && typeof result.data === 'object' && 'skipped' in result.data && result.data.skipped) {
            log('debug', `[fetchUpdates] Skipped (already covered): ${repo.url}#branch:${repo.branch}`);
            // Don't count as fetched, don't add to fetchedRepositories
          } else {
            fetched++;
            // Track successfully fetched repository for cache invalidation
            const repoIdentifier = `${repo.url}#branch:${repo.branch}`;
            fetchedRepositories.push(repoIdentifier);
            log('debug', `[fetchUpdates] Successfully fetched: ${repoIdentifier}`);
          }
        } else {
          failed++;
          failureDetails.push({
            url: repo.url,
            branch: repo.branch,
            error: result.error?.message || 'Failed to fetch updates'
          });
          log('warn', `[fetchUpdates] Failed to fetch updates: ${repo.url}#branch:${repo.branch}`, result.error);
        }
      }
    }

    // Refresh cache for fetched repositories
    if (fetchedRepositories.length > 0) {
      log('debug', `[fetchUpdates] Refreshing cache for ${fetchedRepositories.length} repositories`);
      await cache.refresh({
        repositories: fetchedRepositories
      }, workdir, storageDir);
    }

    // Log summary
    if (failureDetails.length > 0) {
      log('warn', `[fetchUpdates] Failed to fetch ${failed} repositories:`, failureDetails);
    }

    return {
      success: true,
      data: {
        fetched,
        failed,
        ...(failureDetails.length > 0 && { failures: failureDetails })
      }
    };
  },

  /**
   * Clean up expired repositories from storage
   */
  async cleanupStorage(): Promise<void> {
    const storageDir = getConfiguredStorageBase();
    if (storageDir) {
      await storage.repository.cleanup(storageDir);
    }
  },

  // Add all the missing exported functions
  createRepositoryFromUrl,
  checkGitSocialInit,
  initializeRepository,
  getRepositoryRelationship,
  loadWorkspaceRepository,
  loadFollowingRepositories,
  loadAllRepositories,
  loadExternalRepository,
  applyRepositoryFilters,
  ensureDataForDateRange,
  getConfiguredStorageBase
};

/**
 * Create a Repository object from a URL with consistent name extraction
 */
export function createRepositoryFromUrl(
  url: string,
  options: {
    branch?: string;
    type?: 'workspace' | 'other';
    socialEnabled?: boolean;
    followedAt?: Date;
    remoteName?: string;
    lists?: string[];
  } = {}
): Repository {
  const {
    branch = 'main',
    type = 'other',
    socialEnabled = true,
    followedAt,
    remoteName,
    lists
  } = options;

  // Extract repository name with comprehensive logic
  let name: string;

  // Handle GitHub URLs using centralized parser
  const githubRepo = gitHost.parseGitHub(url);
  if (githubRepo) {
    name = `${githubRepo.owner}/${githubRepo.repo}`;
  } else {
    // Handle GitLab URLs
    const gitlabMatch = url.match(/gitlab\.com[:/]([^/]+\/[^/.]+)/);
    if (gitlabMatch && gitlabMatch[1]) {
      name = gitlabMatch[1];
    } else {
      // Handle local paths - get last two parts
      const parts = url.split('/').filter(Boolean);
      if (parts.length >= 2) {
        name = `${parts[parts.length - 2]}/${parts[parts.length - 1]}`;
      } else {
        // Fallback to last part
        name = parts.length > 0 ? (parts[parts.length - 1] || 'unknown') : 'unknown';
      }
    }
  }

  // Remove .git suffix for cleaner display names
  name = name.replace(/\.git$/, '');

  return {
    id: `${url}#branch:${branch}`,
    url,
    name,
    branch,
    type,
    socialEnabled,
    followedAt,
    remoteName,
    lists
  };
}

/**
 * Check if GitSocial is initialized in a repository
 */
export async function checkGitSocialInit(
  repository: string
): Promise<Result<{ isInitialized: boolean; branch?: string }>> {
  try {
    const config = await getGitSocialConfig(repository);
    return {
      success: true,
      data: {
        isInitialized: !!config,
        branch: config?.branch
      }
    };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'CHECK_FAILED',
        message: 'Failed to check GitSocial initialization',
        details: error
      }
    };
  }
}

/**
 * Initialize GitSocial in a repository
 *
 * @param repository - Path to the git repository
 * @param branchOptions - Options for creating/switching to social branch
 * @param branchOptions.createBranch - Whether to create and switch to a social branch (default: false)
 * @param branchOptions.branchName - Name of the branch to create (default: 'gitsocial')
 */
export async function initializeRepository(
  repository: string,
  branchOptions?: {
    createBranch?: boolean;
    branchName?: string;
  }
): Promise<Result<void>> {
  try {
    // Pass branchName only if explicitly provided, otherwise use auto-detection
    const result = await initializeGitSocial(repository, branchOptions?.branchName);

    if (!result.success) {
      return result;
    }

    // Switch to the branch if requested and branchName is explicitly provided
    if (branchOptions?.createBranch && branchOptions?.branchName) {
      const checkoutResult = await execGit(repository, ['checkout', branchOptions.branchName]);

      if (!checkoutResult.success) {
        // Try to create and switch if checkout failed
        const createAndCheckoutResult = await execGit(repository, ['checkout', '-b', branchOptions.branchName]);

        if (!createAndCheckoutResult.success) {
          console.warn(`Could not switch to ${branchOptions.branchName} branch:`, createAndCheckoutResult.error);
        } else {
          log('info', `Created and switched to branch: ${branchOptions.branchName}`);
        }
      } else {
        log('info', `Switched to branch: ${branchOptions.branchName}`);
      }
    }

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'INIT_ERROR',
        message: 'Failed to initialize repository',
        details: error
      }
    };
  }
}

/**
 * Get repository relationship info including its connection to workspace and lists
 * Returns a full Repository object with relationship data populated
 * Used for cache optimization and UI display, not for access control
 */
export async function getRepositoryRelationship(
  workspaceRepository: string,
  targetRepository: string
): Promise<Result<Repository>> {
  try {
    log('debug', `[getRepositoryRelationship] Checking relationship for target: ${targetRepository}`);

    // Check if it's the workspace repository
    if (!targetRepository || targetRepository === workspaceRepository) {
      const workspaceRepo = createRepositoryFromUrl(workspaceRepository, {
        type: 'workspace',
        socialEnabled: true
      });
      return {
        success: true,
        data: workspaceRepo
      };
    }

    // Get all lists to check if repository is in any
    const listsResult = await list.getLists(workspaceRepository);
    if (!listsResult.success || !listsResult.data) {
      return {
        success: false,
        error: {
          code: 'GET_LISTS_ERROR',
          message: 'Failed to get lists',
          details: listsResult.error
        }
      };
    }

    log('debug', `[getRepositoryRelationship] Found ${listsResult.data.length} lists to check`);

    // Check if repository URL is in any list
    const inLists: string[] = [];

    // Normalize target repository URL for comparison (remove branch part)
    const targetRepoUrl = targetRepository.split('#')[0] || targetRepository;
    const normalizedTarget = gitMsgUrl.normalize(targetRepoUrl);
    log('debug', `[getRepositoryRelationship] Target repo URL (no branch): ${targetRepoUrl}`);
    log('debug', `[getRepositoryRelationship] Normalized target: ${normalizedTarget}`);

    for (const list of listsResult.data) {
      log('debug', `[getRepositoryRelationship] Checking list '${list.name}' with ${list.repositories.length} repositories`);

      const repoString = list.repositories.find(repoStr => {
        // Remove branch part from list repository URL for comparison
        const listRepoUrl = repoStr.split('#')[0] || repoStr;
        const normalizedRepo = gitMsgUrl.normalize(listRepoUrl);
        log('debug', `[getRepositoryRelationship] Comparing '${normalizedRepo}' with '${normalizedTarget}'`);
        return normalizedRepo === normalizedTarget;
      });

      if (repoString) {
        log('debug', `[getRepositoryRelationship] Found repository in list '${list.name}': ${repoString}`);
        const parsed = gitMsgRef.parseRepositoryId(repoString);
        if (parsed) {
          inLists.push(list.name);
        }
      }
    }

    log('debug', `[getRepositoryRelationship] Repository found in ${inLists.length} lists: ${inLists.join(', ')}`);

    if (inLists.length > 0) {
      // Repository is in lists
      const repoInList = createRepositoryFromUrl(targetRepository, {
        type: 'other',
        socialEnabled: true,
        lists: inLists,
        remoteName: 'upstream' // All external repos use 'upstream' as remote name
      });
      log('debug', `[getRepositoryRelationship] Returning repository in lists: ${JSON.stringify({ id: repoInList.id, lists: repoInList.lists })}`);
      return {
        success: true,
        data: repoInList
      };
    }

    // Repository not found in lists
    log('debug', '[getRepositoryRelationship] Repository not found in any lists, returning as unknown');
    const unknownRepo = createRepositoryFromUrl(targetRepository, {
      type: targetRepository.startsWith('http://') || targetRepository.startsWith('https://') ? 'other' : 'workspace',
      socialEnabled: false, // Unknown repositories are not social-enabled by default
      remoteName: targetRepository.startsWith('http://') || targetRepository.startsWith('https://') ? 'upstream' : undefined // External repos use 'upstream'
    });
    return {
      success: true,
      data: unknownRepo
    };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'CHECK_STATUS_ERROR',
        message: 'Failed to check repository status',
        details: error
      }
    };
  }
}

// ============================================================================
// Exported Repository Loaders
// ============================================================================

/**
 * Load the workspace repository
 */
export async function loadWorkspaceRepository(workdir: string): Promise<Repository[]> {
  try {
    // Get GitSocial configuration
    const config = await getGitSocialConfig(workdir);

    // Create workspace repository object
    const workspaceBranch = config?.branch || 'main';
    const workspaceRepo: Repository = {
      id: `${workdir}#branch:${workspaceBranch}`,
      url: workdir,
      name: workdir.split('/').pop() || 'workspace',
      path: workdir,
      branch: workspaceBranch,
      type: 'workspace',
      socialEnabled: true,
      config: config ? {
        version: config.version,
        branch: config.branch,
        social: {
          enabled: true,
          branch: config.branch
        }
      } : undefined
    };

    return [workspaceRepo];
  } catch (error) {
    log('error', '[loaders] Failed to load workspace repository:', error);
    return [];
  }
}

/**
 * Load repositories from user's lists (following)
 */
export async function loadFollowingRepositories(
  workdir: string,
  storageBase?: string
): Promise<Repository[]> {
  try {
    // Get all lists
    const listsResult = await list.getLists(workdir);
    if (!listsResult.success || !listsResult.data) {
      return [];
    }

    // Collect unique repositories with their list memberships
    const repoMap = new Map<string, Repository>();

    for (const list of listsResult.data) {
      for (const repoString of list.repositories) {
        const parsed = gitMsgRef.parseRepositoryId(repoString);
        if (!parsed) {continue;}

        const key = gitMsgUrl.normalize(parsed.repository);

        if (!repoMap.has(key)) {
          // Check if we have storage directory for this repository
          let path: string | undefined;
          let fetchedRanges: Array<{ start: string; end: string }> | undefined;
          let lastFetchTime: Date | undefined;

          if (storageBase) {
            const storageDir = storage.path.getDirectory(storageBase, parsed.repository);
            if (existsSync(storageDir)) {
              path = storageDir;

              // Read repository config from git config
              try {
                const lastFetchResult = await execGit(storageDir, ['config', '--get', 'gitsocial.lastfetch']);
                const fetchedRangesResult = await execGit(storageDir, ['config', '--get', 'gitsocial.fetchedranges']);

                if (lastFetchResult.success && lastFetchResult.data) {
                  lastFetchTime = new Date(lastFetchResult.data.stdout.trim());
                }

                if (fetchedRangesResult.success && fetchedRangesResult.data) {
                  try {
                    fetchedRanges = JSON.parse(
                      fetchedRangesResult.data.stdout.trim()
                    ) as Array<{ start: string; end: string }>;
                  } catch {
                    // Ignore parse errors
                  }
                }
              } catch (error) {
                log('debug', '[loadFollowingRepositories] Failed to read repository config:', { storageDir, error });
              }
            }
          }

          // Create repository object
          const repo: Repository = {
            id: `${parsed.repository}#branch:${parsed.branch}`,
            url: parsed.repository,
            name: gitHost.getDisplayName(parsed.repository),
            path,
            branch: parsed.branch,
            type: 'other',
            socialEnabled: true,
            lists: [list.name],
            lastFetchTime,
            fetchedRanges
          };

          // No remote management needed with isolated repository architecture

          repoMap.set(key, repo);
        } else {
          // Add list to existing repository
          const existing = repoMap.get(key)!;
          if (!existing.lists?.includes(list.name)) {
            existing.lists = [...(existing.lists || []), list.name];
          }
        }
      }
    }

    return Array.from(repoMap.values());
  } catch (error) {
    log('error', '[loaders] Failed to load following repositories:', error);
    return [];
  }
}

/**
 * Load all cached repositories from storage
 */
export async function loadAllRepositories(
  workdir: string,
  storageBase?: string
): Promise<Repository[]> {
  const repositories: Repository[] = [];

  try {
    // Start with workspace repository
    const workspace = await loadWorkspaceRepository(workdir);
    repositories.push(...workspace);

    // Add following repositories
    const following = await loadFollowingRepositories(workdir, storageBase);
    repositories.push(...following);

    // If we have storage base, also scan for any additional cached repositories
    if (storageBase) {
      const reposDir = join(storageBase, 'repositories');
      if (existsSync(reposDir)) {
        const entries = readdirSync(reposDir);

        for (const entry of entries) {
          // Skip if it's the workspace
          if (storage.path.isWorkspace(entry)) {continue;}

          // Check if we already have this repository
          const url = storage.path.getUrl(entry);
          const normalizedUrl = gitMsgUrl.normalize(url);
          const exists = repositories.some(r =>
            gitMsgUrl.normalize(r.url) === normalizedUrl
          );

          if (!exists && url) {
            const storageDir = join(reposDir, entry);

            // Read repository config from git config
            try {
              // Get URL from git config
              const urlResult = await execGit(storageDir, ['config', '--get', 'remote.upstream.url']);
              if (urlResult.success && urlResult.data) {
                const url = urlResult.data.stdout.trim();

                // Get repository config from git config
                const lastFetchResult = await execGit(storageDir, ['config', '--get', 'gitsocial.lastfetch']);
                const fetchedRangesResult = await execGit(storageDir, ['config', '--get', 'gitsocial.fetchedranges']);

                const lastFetch = lastFetchResult.success && lastFetchResult.data ?
                  lastFetchResult.data.stdout.trim() : new Date().toISOString();
                let fetchedRanges: Array<{ start: string; end: string }> | undefined;
                if (fetchedRangesResult.success && fetchedRangesResult.data) {
                  try {
                    fetchedRanges = JSON.parse(
                      fetchedRangesResult.data.stdout.trim()
                    ) as Array<{ start: string; end: string }>;
                  } catch {
                    // Ignore parse errors
                  }
                }
                const branch = await getConfiguredBranch(storageDir);

                repositories.push({
                  id: `${url}#branch:${branch}`,
                  url,
                  name: gitHost.getDisplayName(url),
                  path: storageDir,
                  branch,
                  type: 'other',
                  socialEnabled: true,
                  lastFetchTime: new Date(lastFetch),
                  fetchedRanges
                });
              }
            } catch (error) {
              log('debug', '[loaders] Failed to read cached repository config:', { entry, error });
            }
          }
        }
      }
    }
  } catch (error) {
    log('error', '[loaders] Failed to load all repositories:', error);
  }

  return repositories;
}

/**
 * Load a specific external repository
 */
export async function loadExternalRepository(
  url: string,
  storageBase?: string,
  options?: {
    branch?: string;
    ensureCloned?: boolean;
  }
): Promise<Repository | null> {
  try {
    const normalizedUrl = gitMsgUrl.normalize(url);
    const branch = options?.branch || 'main';

    // Check if we should ensure it's cloned
    if (storageBase && options?.ensureCloned) {
      const result = await storage.repository.ensure(storageBase, normalizedUrl, branch, {
        isPersistent: false // External repositories are temporary by default
      });

      if (!result.success) {
        log('error', '[loaders] Failed to ensure external repository:', result.error);
        return null;
      }
    }

    // Get storage path if available
    let path: string | undefined;
    if (storageBase) {
      const storageDir = storage.path.getDirectory(storageBase, normalizedUrl);
      if (existsSync(storageDir)) {
        path = storageDir;

        // Read repository config from git config
        try {
          const lastFetchResult = await execGit(storageDir, ['config', '--get', 'gitsocial.lastfetch']);
          const fetchedRangesResult = await execGit(storageDir, ['config', '--get', 'gitsocial.fetchedranges']);

          const lastFetch = lastFetchResult.success && lastFetchResult.data ?
            lastFetchResult.data.stdout.trim() : new Date().toISOString();
          let fetchedRanges: Array<{ start: string; end: string }> | undefined;
          if (fetchedRangesResult.success && fetchedRangesResult.data) {
            try {
              fetchedRanges = JSON.parse(
                fetchedRangesResult.data.stdout.trim()
              ) as Array<{ start: string; end: string }>;
            } catch {
              // Ignore parse errors
            }
          }

          return {
            id: `${normalizedUrl}#branch:${branch}`,
            url: normalizedUrl,
            name: gitHost.getDisplayName(normalizedUrl),
            path: storageDir,
            branch,
            type: 'other',
            socialEnabled: true,
            lastFetchTime: new Date(lastFetch),
            fetchedRanges
          };
        } catch (error) {
          log('debug', '[loaders] Failed to read external repository config:', error);
        }
      }
    }

    // Return basic repository info even if not cloned
    return {
      id: `${normalizedUrl}#branch:${branch}`,
      url: normalizedUrl,
      name: gitHost.getDisplayName(normalizedUrl),
      path,
      branch,
      type: 'other',
      socialEnabled: true
    };
  } catch (error) {
    log('error', '[loaders] Failed to load external repository:', error);
    return null;
  }
}

/**
 * Filter repositories based on criteria
 */
export function applyRepositoryFilters(
  repositories: Repository[],
  filter?: {
    types?: Array<'workspace' | 'other'>;
    socialEnabled?: boolean;
    limit?: number;
  }
): Repository[] {
  let filtered = repositories;

  if (filter?.types && filter.types.length > 0) {
    filtered = filtered.filter(r => filter.types!.includes(r.type));
  }

  if (filter?.socialEnabled !== undefined) {
    filtered = filtered.filter(r => r.socialEnabled === filter.socialEnabled);
  }

  if (filter?.limit && filter.limit > 0) {
    filtered = filtered.slice(0, filter.limit);
  }

  return filtered;
}

/**
 * Ensure repository data is available for the requested date range
 * Universal function for all repository types (workspace, list, external)
 */
export async function ensureDataForDateRange(
  workdir: string,
  storageBase: string,
  repositoryUrl: string,
  branch: string,
  since: Date,
  options?: { isPersistent?: boolean }
): Promise<Result<void>> {
  const weekStartStr = since.toISOString().substring(0, 10);

  log('debug', '[ensureDataForDateRange] Ensuring data for:', {
    repository: repositoryUrl,
    branch,
    weekStart: weekStartStr,
    isPersistent: options?.isPersistent
  });

  // Step 1: Ensure repository is cloned (has cost gate)
  const ensureResult = await storage.repository.ensure(
    storageBase,
    repositoryUrl,
    branch,
    { isPersistent: options?.isPersistent ?? false }
  );

  if (!ensureResult.success) {
    return {
      success: false,
      error: {
        code: 'ENSURE_ERROR',
        message: `Failed to ensure repository: ${ensureResult.error?.message}`,
        details: ensureResult.error
      }
    };
  }

  // Step 2: Fetch updates for the requested date range (has cost gate)
  const fetchResult = await storage.repository.fetch(
    storageBase,
    repositoryUrl,
    branch,
    { since: weekStartStr }
  );

  if (!fetchResult.success) {
    return {
      success: false,
      error: {
        code: 'FETCH_ERROR',
        message: `Failed to fetch repository: ${fetchResult.error?.message}`,
        details: fetchResult.error
      }
    };
  }

  // Step 3: Load posts into cache
  // Always load even if fetch was skipped to ensure cache consistency
  log('debug', '[ensureDataForDateRange] Loading posts into cache');
  await cache.loadRepositoryPosts(
    workdir,
    repositoryUrl,
    branch,
    storageBase
  );

  log('debug', '[ensureDataForDateRange] Successfully ensured data for:', repositoryUrl);
  return { success: true };
}

/**
 * Initialize the repository system with storage configuration
 */
function initializeRepositorySystem(config: { storageBase: string }): void {
  storageBase = config.storageBase;
}

/**
 * Get the configured storage base directory
 */
export function getConfiguredStorageBase(): string | undefined {
  return storageBase;
}

/**
 * Get repositories based on scope
 */
async function getRepositories(
  workdir: string,
  scope: string = 'workspace:my',
  filter?: RepositoryFilter,
  storageBase?: string
): Promise<Result<Repository[]>> {
  try {
    // Check memory cache
    if (!filter?.skipCache) {
      const cached = storage.cache.get(workdir, scope);
      if (cached) {
        return {
          success: true,
          data: applyRepositoryFilters(cached, filter)
        };
      }
    }

    // Load based on scope
    let repositories: Repository[] = [];

    switch (scope) {
    case 'workspace:my':
      repositories = await loadWorkspaceRepository(workdir);
      break;

    case 'following':
      repositories = await loadFollowingRepositories(workdir, storageBase);
      break;

    case 'all':
      repositories = await loadAllRepositories(workdir, storageBase);
      break;

    default:
      // Handle repository:<url> scope for external repos
      if (scope.startsWith('repository:')) {
        const url = scope.slice(11);
        if (url) {
          // Parse the URL to extract branch (defaults to 'main' if not specified)
          const parsed = gitMsgRef.parseRepositoryId(url);

          log('debug', `[Repositories] Parsed repository: url='${parsed.repository}', branch='${parsed.branch}'`);

          const repo = await loadExternalRepository(parsed.repository, storageBase, {
            branch: parsed.branch
          });
          if (repo) {
            repositories = [repo];
          }
        }
      }
      break;
    }

    // Cache the results
    if (!filter?.skipCache && repositories.length > 0) {
      storage.cache.set(workdir, scope, repositories);
    }

    // Apply filters and return
    return {
      success: true,
      data: applyRepositoryFilters(repositories, filter)
    };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'GET_REPOSITORIES_ERROR',
        message: 'Failed to get repositories',
        details: error
      }
    };
  }
}
