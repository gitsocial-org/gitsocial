/**
 * Storage layer for Git repository management and caching
 */

import { join } from 'path';
import { existsSync, mkdirSync, readdirSync, rmSync, statSync } from 'fs';
import type { Repository, Result } from '../social/types';
import type { Commit } from '../git/types';
import { execGit } from '../git/exec';
import { getCommits } from '../git/operations';
import { log } from '../logger';
import { gitMsgUrl } from '../gitmsg/protocol';

/**
 * Track in-progress repository operations to prevent race conditions
 */
const repositoryOperations = new Map<string, Promise<Result<string>>>();
const fetchOperations = new Map<string, Promise<Result<{ skipped?: boolean } | void>>>();

/**
 * Storage namespace - Repository storage and cache management
 */
export const storage = {
  // Cache operations (only what's actually used)
  cache: {
    get: getCachedRepositories,
    set: setCachedRepositories,
    clear: clearRepositoryMetadataCache
  },

  // Repository operations (all are used)
  repository: {
    ensure: ensureRepository,
    fetch: fetchRepository,
    getCommits: getRepositoryCommits,
    cleanup: cleanupExpiredRepositories,
    readConfig: readRepositoryConfig,
    getStats: getRepositoryStorageStats,
    clearCache: clearRepositoryCache
  },

  // Path utilities (only what's actually used)
  path: {
    getUrl: getRepositoryUrl,
    getDirectory: getRepositoryStorageDir,
    isWorkspace: isWorkspaceRepository
  }
};

// Memory cache for repository metadata
const repositoryCache = new Map<string, CacheEntry<Repository[]>>();

const PERSISTENT_REPO_TTL = 30 * 24 * 60 * 60 * 1000;
const TEMP_REPO_TTL = 24 * 60 * 60 * 1000;

const CACHE_TTL = {
  WORKSPACE: 0,
  FOLLOWING: 60 * 60 * 1000,
  ALL: 24 * 60 * 60 * 1000,
  EXTERNAL: 30 * 60 * 1000
};

/**
 * Cache entry for repository metadata
 */
interface CacheEntry<T> {
  data: T;
  timestamp: number;
  ttl: number;
}

/**
 * Date range for fetched data
 */
interface DateRange {
  start: string; // YYYY-MM-DD
  end: string;   // YYYY-MM-DD
}

/**
 * Merge overlapping or adjacent date ranges
 */
function mergeDateRanges(ranges: DateRange[]): DateRange[] {
  if (ranges.length <= 1) {return ranges;}

  // Sort ranges by start date
  const sorted = [...ranges].sort((a, b) => a.start.localeCompare(b.start));

  const merged: DateRange[] = [];
  let current: DateRange | undefined = sorted[0];

  for (let i = 1; i < sorted.length; i++) {
    const next = sorted[i];

    if (!current) {
      current = next;
      continue;
    }

    if (!next) {
      continue;
    }

    // Check if ranges overlap or are adjacent (within 1 day)
    const currentEndDate = new Date(current.end);
    const nextStartDate = new Date(next.start);
    const daysBetween = Math.floor((nextStartDate.getTime() - currentEndDate.getTime()) / (1000 * 60 * 60 * 24));

    if (daysBetween <= 1) {
      // Merge ranges
      current = {
        start: current.start,
        end: current.end > next.end ? current.end : next.end
      };
    } else {
      // No overlap, add current and move to next
      merged.push(current);
      current = next;
    }
  }

  if (current) {
    merged.push(current);
  }
  return merged;
}

/**
 * Check if a date range is fully covered by existing ranges (no gaps)
 */
function isDateRangeCovered(
  requestedStart: string,
  requestedEnd: string,
  existingRanges: DateRange[]
): boolean {
  if (existingRanges.length === 0) {return false;}

  // Sort ranges by start date
  const sorted = [...existingRanges].sort((a, b) => a.start.localeCompare(b.start));

  let currentPos = requestedStart;

  for (const range of sorted) {
    // If this range starts after our current position, we have a gap
    if (range.start > currentPos) {
      return false;
    }

    // This range covers current position, advance to its end
    if (range.end >= currentPos) {
      currentPos = range.end;
    }

    // If we've covered up to the requested end, we're done
    if (currentPos >= requestedEnd) {
      return true;
    }
  }

  // Didn't reach requested end
  return false;
}

/**
 * Add a new date range and merge with existing ranges
 */
function addDateRange(existingRanges: DateRange[], newRange: DateRange): DateRange[] {
  return mergeDateRanges([...existingRanges, newRange]);
}

/**
 * Get cache TTL based on scope
 */
function getCacheTTL(scope: string): number {
  switch (scope) {
  case 'workspace:my':
    return CACHE_TTL.WORKSPACE;
  case 'following':
    return CACHE_TTL.FOLLOWING;
  case 'all':
    return CACHE_TTL.ALL;
  default:
    if (scope.startsWith('repository:')) {
      return CACHE_TTL.EXTERNAL;
    }
    return CACHE_TTL.ALL;
  }
}

/**
 * Get cached repositories
 */
function getCachedRepositories(
  workdir: string,
  scope: string
): Repository[] | null {
  const cacheKey = `${workdir}:${scope}`;
  const cached = repositoryCache.get(cacheKey);

  if (!cached) {
    return null;
  }

  const age = Date.now() - cached.timestamp;
  if (age < cached.ttl) {
    return cached.data;
  }

  // Cache expired, remove it
  repositoryCache.delete(cacheKey);
  return null;
}

/**
 * Set cached repositories
 */
function setCachedRepositories(
  workdir: string,
  scope: string,
  repositories: Repository[]
): void {
  const cacheKey = `${workdir}:${scope}`;
  const ttl = getCacheTTL(scope);

  repositoryCache.set(cacheKey, {
    data: repositories,
    timestamp: Date.now(),
    ttl
  });
}

/**
 * Clear cached repositories for a specific scope
 */
function clearRepositoryMetadataCache(
  workdir: string,
  scope: string
): void {
  const cacheKey = `${workdir}:${scope}`;
  repositoryCache.delete(cacheKey);
  log('debug', '[storage] Cleared repository cache for scope:', scope);
}

/**
 * Read repository configuration from git config
 */
async function readRepositoryConfig(repositoryPath: string): Promise<{
  version?: string;
  lastFetch?: string;
  fetchedRanges?: DateRange[];
  isPersistent?: boolean;
  createdAt?: string;
  branch?: string;
} | null> {
  try {
    const config: {
      version?: string;
      lastFetch?: string;
      fetchedRanges?: DateRange[];
      isPersistent?: boolean;
      createdAt?: string;
      branch?: string;
    } = {};

    // Read all gitsocial.* config values
    const result = await execGit(repositoryPath, ['config', '--get-regexp', '^gitsocial']);
    if (result.success && result.data?.stdout) {
      const lines = result.data.stdout.trim().split('\n');
      for (const line of lines) {
        const [key, ...valueParts] = line.split(' ');
        if (!key) {continue;}
        const value = valueParts.join(' ');
        const keyName = key.replace('gitsocial.', '');

        // Parse boolean values
        if (keyName === 'ispersistent') {
          config.isPersistent = value === 'true';
        } else if (keyName === 'lastfetch') {
          config.lastFetch = value;
        } else if (keyName === 'fetchedranges') {
          try {
            config.fetchedRanges = JSON.parse(value) as Array<{ start: string; end: string }>;
          } catch {
            log('debug', '[readRepositoryConfig] Failed to parse fetchedranges:', value);
          }
        } else if (keyName === 'createdat') {
          config.createdAt = value;
        } else if (keyName === 'version') {
          config.version = value;
        } else if (keyName === 'branch') {
          config.branch = value;
        }
      }
      return Object.keys(config).length > 0 ? config : null;
    }
  } catch (error) {
    log('debug', '[isolated-repositories] No config found:', { repositoryPath });
  }
  return null;
}

/**
 * Write repository configuration to git config
 */
async function writeRepositoryConfig(repositoryPath: string, config: {
  version?: string;
  lastFetch?: string;
  fetchedRanges?: DateRange[];
  isPersistent?: boolean;
  createdAt?: string;
  branch?: string;
}): Promise<void> {
  try {
    log('debug', '[writeRepositoryConfig] Writing config:', { repositoryPath, config });

    // Set git config values
    if (config.version) {
      await execGit(repositoryPath, ['config', 'gitsocial.version', config.version]);
    }
    if (config.lastFetch) {
      await execGit(repositoryPath, ['config', 'gitsocial.lastfetch', config.lastFetch]);
    }
    if (config.fetchedRanges) {
      log('debug', '[writeRepositoryConfig] Writing fetchedRanges:', config.fetchedRanges);
      await execGit(repositoryPath, ['config', 'gitsocial.fetchedranges', JSON.stringify(config.fetchedRanges)]);
    }
    if (config.isPersistent !== undefined) {
      await execGit(repositoryPath, ['config', 'gitsocial.ispersistent', config.isPersistent.toString()]);
    }
    if (config.createdAt) {
      await execGit(repositoryPath, ['config', 'gitsocial.createdat', config.createdAt]);
    }
    if (config.branch) {
      await execGit(repositoryPath, ['config', 'gitsocial.branch', config.branch]);
    }
  } catch (error) {
    log('warn', '[isolated-repositories] Failed to write config:', { repositoryPath, error });
  }
}

/**
 * Check if a cached repository is still valid
 */
async function isCacheValid(repositoryPath: string, isPersistent: boolean): Promise<boolean> {
  try {
    if (!existsSync(repositoryPath)) {
      return false;
    }

    const config = await readRepositoryConfig(repositoryPath);
    if (!config || !config.lastFetch) {
      return false;
    }

    const lastFetch = new Date(config.lastFetch).getTime();
    const age = Date.now() - lastFetch;
    const ttl = isPersistent ? PERSISTENT_REPO_TTL : TEMP_REPO_TTL;

    return age < ttl;
  } catch {
    return false;
  }
}

/**
 * Clean up a repository directory
 */
function cleanupRepository(repositoryPath: string): void {
  try {
    if (existsSync(repositoryPath)) {
      rmSync(repositoryPath, { recursive: true, force: true });
      log('debug', '[isolated-repositories] Cleaned up repository:', repositoryPath);
    }
  } catch (error) {
    log('warn', '[isolated-repositories] Failed to cleanup repository:', { repositoryPath, error });
  }
}

/**
 * Ensure a repository is cloned and up-to-date
 *
 * @param storageBase - Base storage directory
 * @param url - Repository URL
 * @param branch - Branch to clone/fetch
 * @param options - Clone options
 * @returns Path to the repository directory
 */
async function ensureRepository(
  storageBase: string,
  url: string,
  branch: string,
  options?: {
    isPersistent?: boolean;
    force?: boolean;
  }
): Promise<Result<string>> {
  const { isPersistent = true, force = false } = options || {};

  // Validate parameters
  if (!storageBase || typeof storageBase !== 'string') {
    return {
      success: false,
      error: {
        code: 'INVALID_STORAGE_BASE',
        message: 'Storage base directory must be a valid string'
      }
    };
  }

  const normalizedUrl = gitMsgUrl.normalize(url);

  // Check for in-progress operations on this repository
  const operationKey = `${normalizedUrl}:${branch}`;
  const existingOperation = repositoryOperations.get(operationKey);
  if (existingOperation && !force) {
    log('debug', '[isolated-repositories] Waiting for existing operation:', operationKey);
    return existingOperation;
  }

  // Create new operation
  const operation = doEnsureRepository(storageBase, normalizedUrl, branch, { isPersistent, force })
    .finally(() => {
      // Clean up when done
      repositoryOperations.delete(operationKey);
    });

  // Store operation for other concurrent calls
  repositoryOperations.set(operationKey, operation);
  return operation;
}

/**
 * Internal implementation of ensureRepository
 */
async function doEnsureRepository(
  storageBase: string,
  normalizedUrl: string,
  branch: string,
  options: {
    isPersistent: boolean;
    force: boolean;
  }
): Promise<Result<string>> {
  try {
    const { isPersistent, force } = options;
    const storageDir = getRepositoryStorageDir(storageBase, normalizedUrl);

    // Ensure base directory exists
    mkdirSync(join(storageBase, 'repositories'), { recursive: true });

    // Check if we already have this repository and it's still valid
    if (!force && existsSync(storageDir) && await isCacheValid(storageDir, isPersistent)) {
      log('debug', '[isolated-repositories] Using existing repository:', { url: normalizedUrl, path: storageDir });

      // Don't update lastFetch here - it should only be updated when actual git fetch occurs
      return { success: true, data: storageDir };
    }

    // Clean up any existing directory if forcing or invalid
    if (existsSync(storageDir)) {
      cleanupRepository(storageDir);
    }

    log('info', '[isolated-repositories] Creating minimal repository:', {
      url: normalizedUrl,
      branch,
      storageDir,
      isPersistent
    });

    // Create directory
    mkdirSync(storageDir, { recursive: true });

    // Initialize bare repository (no working directory - we only read commits)
    const initResult = await execGit(storageDir, ['init', '--bare']);
    if (!initResult.success) {
      cleanupRepository(storageDir);
      return {
        success: false,
        error: {
          code: 'INIT_ERROR',
          message: 'Failed to initialize repository',
          details: initResult.error
        }
      };
    }

    // Add remote as 'upstream' (not origin, to make it clear this is read-only)
    const remoteResult = await execGit(storageDir, [
      'remote',
      'add',
      'upstream',
      normalizedUrl
    ]);
    if (!remoteResult.success) {
      cleanupRepository(storageDir);
      return {
        success: false,
        error: {
          code: 'REMOTE_ERROR',
          message: 'Failed to add remote',
          details: remoteResult.error
        }
      };
    }

    // Configure repository for partial clone
    const configCommands = [
      ['config', 'remote.upstream.partialclonefilter', 'blob:none'],
      ['config', 'remote.upstream.pushurl', '']
    ];

    for (const args of configCommands) {
      const result = await execGit(storageDir, args);
      if (!result.success) {
        log('warn', '[isolated-repositories] Failed to configure repository:', { args, error: result.error });
      }
    }

    // Calculate since date for initial fetch (start of this week - last Monday)
    const { getCurrentWeekMonday, toDateString } = await import('../utils/date');
    const now = new Date();
    const lastMonday = getCurrentWeekMonday();
    const sinceDate = toDateString(lastMonday);
    const todayDate = toDateString(now);

    // Fetch the specific branch and social refs using date-based shallow fetch
    let fetchResult = await execGit(storageDir, [
      'fetch',
      'upstream',
      `+refs/heads/${branch}:refs/remotes/upstream/${branch}`,
      '+refs/gitmsg/social/*:refs/remotes/upstream/gitmsg/social/*',
      '--shallow-since', sinceDate,
      '--no-tags'
    ]);

    let usedDepthFetch = false;
    if (!fetchResult.success) {
      const errorMessage = fetchResult.error?.message || '';

      // Check for lock file issues during clone
      if (errorMessage.includes('Unable to create') && errorMessage.includes('.lock')) {
        log('warn', '[isolated-repositories] Detected lock file issue during clone, cleaning up:', {
          url: normalizedUrl,
          storageDir
        });
        cleanupRepository(storageDir);
        return {
          success: false,
          error: {
            code: 'LOCK_FILE_ERROR',
            message: 'Repository lock file error during clone',
            details: fetchResult.error
          }
        };
      }

      log('debug', '[isolated-repositories] Shallow fetch not supported by server, using depth-based fetch:', {
        url: normalizedUrl,
        branch,
        sinceDate
      });
      fetchResult = await execGit(storageDir, [
        'fetch',
        'upstream',
        `+refs/heads/${branch}:refs/remotes/upstream/${branch}`,
        '+refs/gitmsg/social/*:refs/remotes/upstream/gitmsg/social/*',
        '--depth', '100',
        '--update-shallow',  // Required: first fetch created shallow repo
        '--no-tags'
      ]);
      usedDepthFetch = true;

      if (!fetchResult.success) {
        log('error', '[isolated-repositories] Failed to fetch branch after retry:', {
          url: normalizedUrl,
          branch,
          error: fetchResult.error
        });
        cleanupRepository(storageDir);
        return {
          success: false,
          error: {
            code: 'FETCH_ERROR',
            message: `Failed to fetch branch '${branch}' from ${normalizedUrl}`,
            details: fetchResult.error
          }
        };
      }
    }

    // Determine actual fetched range based on commits
    let actualStartDate = sinceDate;
    if (usedDepthFetch) {
      const oldestCommitResult = await execGit(storageDir, [
        'log',
        `upstream/${branch}`,
        '--reverse',
        '--max-count=1',
        '--format=%cd',
        '--date=short'
      ]);
      if (oldestCommitResult.success && oldestCommitResult.data?.stdout.trim()) {
        actualStartDate = oldestCommitResult.data.stdout.trim();
        log('info', '[isolated-repositories] Using actual oldest commit date for fetched range:', {
          actualStartDate,
          originalSinceDate: sinceDate
        });
      }
    }

    // Write repository configuration to git config with initial fetched range
    await writeRepositoryConfig(storageDir, {
      version: '1.0.0',
      lastFetch: now.toISOString(),
      fetchedRanges: [{ start: actualStartDate, end: todayDate }],
      isPersistent,
      createdAt: now.toISOString(),
      branch
    });

    log('info', '[isolated-repositories] Successfully cloned repository:', {
      url: normalizedUrl,
      storageDir
    });

    return { success: true, data: storageDir };
  } catch (error) {
    log('error', '[isolated-repositories] Unexpected error in ensureRepository:', error);
    return {
      success: false,
      error: {
        code: 'REPOSITORY_ERROR',
        message: 'Failed to ensure repository',
        details: error
      }
    };
  }
}

/**
 * Fetch latest changes for a repository
 */
async function fetchRepository(
  storageBase: string,
  url: string,
  branch?: string,
  options?: { since?: string }
): Promise<Result<{ skipped?: boolean } | void>> {
  try {
    const normalizedUrl = gitMsgUrl.normalize(url);
    const storageDir = getRepositoryStorageDir(storageBase, normalizedUrl);

    if (!existsSync(storageDir)) {
      return {
        success: false,
        error: {
          code: 'REPOSITORY_NOT_FOUND',
          message: 'Repository not found in storage'
        }
      };
    }

    // Try to get branch from config if not provided
    let targetBranch = branch;
    if (!targetBranch) {
      const existingConfig = await readRepositoryConfig(storageDir);
      if (existingConfig?.branch) {
        targetBranch = existingConfig.branch;
        log('debug', '[fetchRepository] Using branch from stored config:', targetBranch);
      } else {
        return {
          success: false,
          error: {
            code: 'MISSING_BRANCH',
            message: `Branch is required for repository: ${url}`
          }
        };
      }
    }

    // Check for in-progress fetch operations on this repository
    const fetchKey = `${normalizedUrl}:${targetBranch}:${options?.since || 'latest'}`;
    const existingFetch = fetchOperations.get(fetchKey);
    if (existingFetch) {
      log('debug', '[fetchRepository] Waiting for existing fetch operation:', fetchKey);
      return existingFetch;
    }

    // Create the fetch operation promise
    const fetchOperation = (async () => {
      try {
        log('debug', '[isolated-repositories] Fetching repository:', {
          url: normalizedUrl,
          branch: targetBranch,
          storageDir
        });

        // Read existing config to check current state
        const existingConfig = await readRepositoryConfig(storageDir);
        const existingRanges = existingConfig?.fetchedRanges || [];

        // Determine the date to fetch from
        let sinceDate: string;
        const now = new Date();
        const todayDate = now.toISOString().substring(0, 10);

        if (options?.since) {
        // Use the requested date, normalized to YYYY-MM-DD
          sinceDate = options.since.includes('T') ?
            options.since.substring(0, 10) : options.since;
          log('info', `[fetchRepository] Fetching from requested date: ${sinceDate}`);

          // Check if the requested date range is already covered (gap-aware check)
          const isAlreadyCovered = isDateRangeCovered(sinceDate, todayDate, existingRanges);

          if (isAlreadyCovered) {
            log('info', '[fetchRepository] Date range already covered by existing fetchedRanges, skipping fetch:', {
              requestedDate: sinceDate,
              todayDate,
              existingRanges
            });
            // Don't update lastFetch since we didn't actually fetch
            return { success: true, data: { skipped: true } };
          }
          // If not covered, keep using the requested sinceDate to fetch earlier data
          log('info', `[fetchRepository] Fetching earlier data from requested date: ${sinceDate}`);
        } else if (!options?.since && existingRanges.length > 0) {
        // Only use existing ranges when NO specific date was requested
          const firstRange = existingRanges[0]!;
          const oldestDate = existingRanges.reduce((oldest, range) =>
            range.start < oldest ? range.start : oldest, firstRange.start);
          sinceDate = oldestDate;
          log('info', `[fetchRepository] No specific date requested, using oldest fetched date from ranges: ${sinceDate}`);
        } else {
        // Default to this week's Monday
          const dayOfWeek = now.getDay();
          const daysFromMonday = (dayOfWeek === 0) ? 6 : dayOfWeek - 1;
          const thisWeekMonday = new Date(now);
          thisWeekMonday.setDate(now.getDate() - daysFromMonday);
          thisWeekMonday.setHours(0, 0, 0, 0);
          sinceDate = thisWeekMonday.toISOString().substring(0, 10);
          log('info', `[fetchRepository] Using default (this week's Monday): ${sinceDate}`);
        }

        // Build fetch command with --shallow-since and --update-shallow
        const fetchArgs = [
          'fetch',
          'upstream',
          `+refs/heads/${targetBranch}:refs/remotes/upstream/${targetBranch}`,
          '+refs/gitmsg/social/*:refs/remotes/upstream/gitmsg/social/*',
          '--shallow-since', sinceDate,
          '--update-shallow',  // Required to update shallow boundaries
          '--no-tags'
        ];

        log('info', '[fetchRepository] Executing git fetch with args:', fetchArgs);
        let fetchResult = await execGit(storageDir, fetchArgs);

        let usedDepthFetch = false;
        if (!fetchResult.success) {
          const errorMessage = fetchResult.error?.message || '';

          // Check for lock file issues - clean up and let it re-clone next time
          if (errorMessage.includes('Unable to create') && errorMessage.includes('.lock')) {
            log('warn', '[fetchRepository] Detected lock file issue, cleaning up repository:', {
              url: normalizedUrl,
              storageDir
            });
            cleanupRepository(storageDir);
            return {
              success: false,
              error: {
                code: 'LOCK_FILE_ERROR',
                message: 'Repository has stale lock file - cleaned up, will re-clone on next fetch',
                details: fetchResult.error
              }
            };
          }

          log('debug', '[fetchRepository] Shallow fetch not supported by server, using depth-based fetch:', {
            url: normalizedUrl,
            branch: targetBranch,
            sinceDate
          });
          fetchResult = await execGit(storageDir, [
            'fetch',
            'upstream',
            `+refs/heads/${targetBranch}:refs/remotes/upstream/${targetBranch}`,
            '+refs/gitmsg/social/*:refs/remotes/upstream/gitmsg/social/*',
            '--depth', '100',
            '--update-shallow',
            '--no-tags'
          ]);
          usedDepthFetch = true;

          if (!fetchResult.success) {
          // Try --unshallow as last resort before giving up
            log('debug', '[fetchRepository] Depth-based fetch not supported, using --unshallow:', {
              url: normalizedUrl,
              branch: targetBranch
            });
            fetchResult = await execGit(storageDir, [
              'fetch',
              'upstream',
              `+refs/heads/${targetBranch}:refs/remotes/upstream/${targetBranch}`,
              '+refs/gitmsg/social/*:refs/remotes/upstream/gitmsg/social/*',
              '--unshallow',
              '--no-tags'
            ]);

            if (!fetchResult.success) {
              log('error', '[fetchRepository] Git fetch failed after all retries:', {
                command: `git ${fetchArgs.join(' ')}`,
                error: fetchResult.error
              });
              return {
                success: false,
                error: {
                  code: 'FETCH_ERROR',
                  message: 'Failed to fetch repository after all fallback attempts',
                  details: fetchResult.error
                }
              };
            }
          }
        }

        log('info', '[fetchRepository] Git fetch succeeded');

        // Determine actual fetched range
        let actualStartDate = sinceDate;
        if (usedDepthFetch) {
          const oldestCommitResult = await execGit(storageDir, [
            'log',
            `upstream/${targetBranch}`,
            '--reverse',
            '--max-count=1',
            '--format=%cd',
            '--date=short'
          ]);
          if (oldestCommitResult.success && oldestCommitResult.data?.stdout.trim()) {
            actualStartDate = oldestCommitResult.data.stdout.trim();
            log('info', '[fetchRepository] Using actual oldest commit date for fetched range:', {
              actualStartDate,
              originalSinceDate: sinceDate
            });
          }
        }

        // Update fetched ranges with the new range
        const newRange: DateRange = { start: actualStartDate, end: todayDate };
        const updatedRanges = addDateRange(existingRanges, newRange);

        log('info', '[fetchRepository] Updating fetched ranges:', {
          existingRanges,
          newRange,
          updatedRanges
        });

        // Update config with new ranges and last fetch time
        const configUpdate: {
        lastFetch: string;
        fetchedRanges: DateRange[];
      } = {
        lastFetch: new Date().toISOString(),
        fetchedRanges: updatedRanges
      };

        log('debug', '[fetchRepository] Writing config update:', configUpdate);
        await writeRepositoryConfig(storageDir, configUpdate);

        return { success: true };
      } catch (error) {
        log('error', '[isolated-repositories] Unexpected error in fetchRepository:', error);
        return {
          success: false,
          error: {
            code: 'FETCH_ERROR',
            message: 'Failed to fetch repository',
            details: error
          }
        };
      }
    })()
      .finally(() => {
        fetchOperations.delete(fetchKey);
      });

    // Store operation for other concurrent calls
    fetchOperations.set(fetchKey, fetchOperation);
    return fetchOperation;
  } catch (error) {
    log('error', '[isolated-repositories] Unexpected error in fetchRepository outer:', error);
    return {
      success: false,
      error: {
        code: 'FETCH_ERROR',
        message: 'Failed to fetch repository',
        details: error
      }
    };
  }
}

/**
 * Get commits from an isolated repository
 */
async function getRepositoryCommits(
  storageBase: string,
  url: string,
  options?: {
    branch?: string;
    limit?: number;
    since?: Date;
    until?: Date;
  }
): Promise<Result<Commit[]>> {
  try {
    const normalizedUrl = gitMsgUrl.normalize(url);
    const storageDir = getRepositoryStorageDir(storageBase, normalizedUrl);

    if (!existsSync(storageDir)) {
      return {
        success: false,
        error: {
          code: 'REPOSITORY_NOT_FOUND',
          message: 'Repository not found in storage'
        }
      };
    }

    const branch = options?.branch;
    if (!branch) {
      return {
        success: false,
        error: {
          code: 'MISSING_BRANCH',
          message: `Branch is required for repository: ${url}`
        }
      };
    }

    log('debug', '[isolated-repositories] Getting commits from repository:', {
      url: normalizedUrl,
      branch,
      limit: options?.limit
    });

    // Get commits from the repository (using upstream remote)
    const commits = await getCommits(storageDir, {
      branch: `upstream/${branch}`,
      limit: options?.limit || 10000,
      since: options?.since,
      until: options?.until
    });

    log('info', '[isolated-repositories] Retrieved commits from repository:', {
      url: normalizedUrl,
      count: commits.length,
      branch: `upstream/${branch}`,
      since: options?.since,
      firstCommit: commits.length > 0 && commits[0] ? {
        hash: commits[0].hash,
        message: commits[0].message?.substring(0, 50)
      } : null
    });

    return { success: true, data: commits };
  } catch (error) {
    log('error', '[isolated-repositories] Unexpected error in getRepositoryCommits:', error);
    return {
      success: false,
      error: {
        code: 'REPOSITORY_ERROR',
        message: 'Failed to get commits from repository',
        details: error
      }
    };
  }
}

/**
 * Get storage statistics for cached repositories
 */
async function getRepositoryStorageStats(storageBase: string): Promise<{
  totalRepositories: number;
  diskUsage: number;
  persistent: number;
  temporary: number;
}> {
  const stats = {
    totalRepositories: 0,
    diskUsage: 0,
    persistent: 0,
    temporary: 0
  };
  try {
    const repositoriesDir = join(storageBase, 'repositories');
    if (!existsSync(repositoriesDir)) {
      return stats;
    }
    const entries = readdirSync(repositoriesDir);
    for (const entry of entries) {
      const fullPath = join(repositoriesDir, entry);
      try {
        const config = await readRepositoryConfig(fullPath);
        if (config) {
          stats.totalRepositories++;
          if (config.isPersistent) {
            stats.persistent++;
          } else {
            stats.temporary++;
          }
          const diskSize = getDirectorySize(fullPath);
          stats.diskUsage += diskSize;
        }
      } catch (error) {
        log('debug', '[getRepositoryStorageStats] Error reading repository:', { path: fullPath, error });
      }
    }
  } catch (error) {
    log('warn', '[getRepositoryStorageStats] Failed to get storage stats:', error);
  }
  return stats;
}

/**
 * Calculate directory size recursively
 */
function getDirectorySize(dirPath: string): number {
  let totalSize = 0;
  try {
    const entries = readdirSync(dirPath, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = join(dirPath, entry.name);
      if (entry.isDirectory()) {
        totalSize += getDirectorySize(fullPath);
      } else if (entry.isFile()) {
        const stats = statSync(fullPath);
        totalSize += stats.size;
      }
    }
  } catch (error) {
    log('debug', '[getDirectorySize] Error calculating size:', { dirPath, error });
  }
  return totalSize;
}

/**
 * Clean up expired repositories
 */
async function cleanupExpiredRepositories(storageBase: string): Promise<void> {
  log('info', '[isolated-repositories] Cleaning up expired repositories');

  try {
    const repositoriesDir = join(storageBase, 'repositories');
    if (!existsSync(repositoriesDir)) {
      return;
    }

    const entries = readdirSync(repositoriesDir);

    for (const entry of entries) {
      const fullPath = join(repositoriesDir, entry);

      try {
        const config = await readRepositoryConfig(fullPath);

        if (!config || !config.lastFetch) {
          // No config means it's invalid, clean it up
          cleanupRepository(fullPath);
          continue;
        }

        const lastFetch = new Date(config.lastFetch).getTime();
        const age = Date.now() - lastFetch;
        const ttl = config.isPersistent ? PERSISTENT_REPO_TTL : TEMP_REPO_TTL;

        if (age > ttl) {
          log('debug', '[isolated-repositories] Removing expired repository:', {
            path: fullPath,
            age: Math.floor(age / (1000 * 60 * 60)) + ' hours'
          });
          cleanupRepository(fullPath);
        }
      } catch (error) {
        log('warn', '[isolated-repositories] Error checking repository:', { path: fullPath, error });
      }
    }
  } catch (error) {
    log('warn', '[isolated-repositories] Failed to cleanup expired repositories:', error);
  }
}

/**
 * Clear all cached repositories regardless of TTL
 */
function clearRepositoryCache(storageBase: string): {
  deletedCount: number;
  diskSpaceFreed: number;
  errors: string[];
} {
  const result = {
    deletedCount: 0,
    diskSpaceFreed: 0,
    errors: [] as string[]
  };
  log('info', '[isolated-repositories] Clearing all repository cache');
  try {
    const repositoriesDir = join(storageBase, 'repositories');
    if (!existsSync(repositoriesDir)) {
      return result;
    }
    const entries = readdirSync(repositoriesDir);
    for (const entry of entries) {
      const fullPath = join(repositoriesDir, entry);
      try {
        const diskSize = getDirectorySize(fullPath);
        cleanupRepository(fullPath);
        result.deletedCount++;
        result.diskSpaceFreed += diskSize;
        log('debug', '[isolated-repositories] Deleted cached repository:', {
          path: fullPath,
          size: (diskSize / 1024 / 1024).toFixed(1) + ' MB'
        });
      } catch (error) {
        const errorMsg = error instanceof Error ? error.message : String(error);
        result.errors.push(`Failed to delete ${entry}: ${errorMsg}`);
        log('warn', '[isolated-repositories] Error deleting repository:', { path: fullPath, error });
      }
    }
    log('info', '[isolated-repositories] Cleared repository cache:', {
      deleted: result.deletedCount,
      freed: (result.diskSpaceFreed / 1024 / 1024).toFixed(1) + ' MB',
      errors: result.errors.length
    });
  } catch (error) {
    log('error', '[isolated-repositories] Failed to clear repository cache:', error);
    const errorMsg = error instanceof Error ? error.message : String(error);
    result.errors.push(`Failed to read repositories directory: ${errorMsg}`);
  }
  return result;
}

/**
 * Convert a GitMsg reference or URL to a filesystem-safe storage path
 *
 * @param ref - GitMsg reference (e.g., "https://github.com/user/repo#commit:abc123456789")
 * @returns Filesystem-safe storage path (e.g., "github-com-user-repo")
 *
 * @example
 * getStoragePath("https://github.com/torvalds/linux")
 * // => "github-com-torvalds-linux"
 *
 * getStoragePath("https://github.com/facebook/react#branch:main")
 * // => "github-com-facebook-react"
 */
function getStoragePath(ref: string): string {
  // Extract URL from GitMsg reference
  const url = ref.split('#')[0];

  if (!url) {
    return 'workspace';
  }

  // Normalize URL (remove protocol, .git suffix, make lowercase)
  const normalized = gitMsgUrl.normalize(url);

  // Convert to filesystem-safe format
  // Remove protocol prefix
  const withoutProtocol = normalized
    .replace(/^(https?|git):\/\//, '')
    .replace(/^git@/, '');

  // Replace special characters with hyphens
  const safePath = withoutProtocol
    .replace(/[:\\/\\.@]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');

  return safePath;
}

/**
 * Convert a storage path back to a repository URL
 *
 * @param storagePath - Filesystem storage path
 * @returns Repository URL
 *
 * @example
 * getRepositoryUrl("github-com-torvalds-linux")
 * // => "https://github.com/torvalds/linux"
 */
function getRepositoryUrl(storagePath: string): string {
  if (storagePath === 'workspace') {
    return '';
  }

  if (storagePath.startsWith('github-com-')) {
    const parts = storagePath.substring('github-com-'.length).split('-');
    if (parts.length >= 2) {
      const owner = parts[0];
      const repo = parts.slice(1).join('-');
      return `https://github.com/${owner}/${repo}`;
    }
  }

  if (storagePath.startsWith('gitlab-com-')) {
    const parts = storagePath.substring('gitlab-com-'.length).split('-');
    if (parts.length >= 2) {
      const owner = parts[0];
      const repo = parts.slice(1).join('-');
      return `https://gitlab.com/${owner}/${repo}`;
    }
  }

  if (storagePath.startsWith('bitbucket-org-')) {
    const parts = storagePath.substring('bitbucket-org-'.length).split('-');
    if (parts.length >= 2) {
      const owner = parts[0];
      const repo = parts.slice(1).join('-');
      return `https://bitbucket.org/${owner}/${repo}`;
    }
  }

  return storagePath;
}

/**
 * Get the storage directory path for a repository
 *
 * @param storageBase - Base storage directory (e.g., extension's globalStorageUri.fsPath)
 * @param url - Repository URL
 * @returns Full filesystem path to repository storage directory
 *
 * @example
 * getRepositoryStorageDir("/path/to/storage", "https://github.com/user/repo")
 * // => "/path/to/storage/repositories/github-com-user-repo"
 */
function getRepositoryStorageDir(storageBase: string, url: string): string {
  const storageName = getStoragePath(url);
  return join(storageBase, 'repositories', storageName);
}

/**
 * Check if a storage path represents the workspace repository
 */
function isWorkspaceRepository(storagePath: string): boolean {
  return storagePath === 'workspace' || storagePath === '';
}
