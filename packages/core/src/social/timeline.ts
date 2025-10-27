/**
 * Timeline features for GitSocial
 */

import { log } from '../logger';
import { cache } from './post/cache';
import { repository } from './repository';
import { post } from './post';
import { storage } from '../storage';
import { gitMsgRef } from '../gitmsg/protocol';
import type {
  Post,
  Repository,
  Result,
  TimelineEntry
} from './types';

export interface TimelineOptions {
  types?: Array<'post' | 'quote' | 'comment' | 'repost'>;
  limit?: number;
  skipCache?: boolean;
}

export interface TimelineResult {
  posts: Post[];
  repositories?: Repository[];
}

/**
 * Timeline namespace - Timeline operations
 */
export const timeline = {
  getTimelineStats,
  getWeekPosts,
  prefetchAdjacentWeeks
};

/**
 * Get timeline statistics
 */
function getTimelineStats(entries: TimelineEntry[]): {
  totalPosts: number;
  postsByType: Record<string, number>;
  postsByAuthor: Record<string, number>;
  postsByRepository: Record<string, number>;
  dateRange: {
    start: Date | null;
    end: Date | null;
  };
} {
  const stats = {
    totalPosts: entries.length,
    postsByType: {} as Record<string, number>,
    postsByAuthor: {} as Record<string, number>,
    postsByRepository: {} as Record<string, number>,
    dateRange: {
      start: null as Date | null,
      end: null as Date | null
    }
  };

  for (const entry of entries) {
    const { post } = entry;
    const date = new Date(post.timestamp);

    // Count by type
    stats.postsByType[post.type] = (stats.postsByType[post.type] || 0) + 1;

    // Count by author
    stats.postsByAuthor[post.author.email] = (stats.postsByAuthor[post.author.email] || 0) + 1;

    // Count by repository
    stats.postsByRepository[post.repository] = (stats.postsByRepository[post.repository] || 0) + 1;

    // Update date range
    if (!stats.dateRange.start || date < stats.dateRange.start) {
      stats.dateRange.start = date;
    }
    if (!stats.dateRange.end || date > stats.dateRange.end) {
      stats.dateRange.end = date;
    }
  }

  return stats;
}

/**
 * Get posts for a specific week in the timeline
 * Handles all the complexity of checking cache, fetching repositories, and loading posts
 */
async function getWeekPosts(
  workdir: string,
  storageBase: string | undefined,
  weekStart: Date,
  weekEnd: Date,
  options?: TimelineOptions
): Promise<Result<TimelineResult>> {
  try {
    log('debug', '[Timeline] Getting posts for week:', {
      weekStart: weekStart.toISOString(),
      weekEnd: weekEnd.toISOString(),
      options
    });

    // Ensure we have data for this week
    if (storageBase) {
      const ensureResult = await ensureWeekData(workdir, storageBase, weekStart);
      if (!ensureResult.success) {
        return {
          success: false,
          error: ensureResult.error
        };
      }

      // Start background prefetching for adjacent weeks
      prefetchAdjacentWeeks(workdir, storageBase, weekStart).catch(error => {
        log('debug', '[Timeline] Background prefetch error (non-blocking):', error);
      });
    }

    // Get the posts now that all data is loaded
    const postsResult = post.getPosts(workdir, 'timeline', {
      types: options?.types === undefined ? undefined : options.types,
      since: weekStart,
      until: weekEnd,
      limit: options?.limit,
      skipCache: options?.skipCache
    });

    if (!postsResult.success || !postsResult.data) {
      return {
        success: false,
        error: postsResult.error || {
          code: 'FETCH_FAILED',
          message: 'Failed to get timeline posts'
        }
      };
    }

    // Get repository data for Timeline view
    const reposResult = await repository.getRepositories(workdir, 'following');

    return {
      success: true,
      data: {
        posts: postsResult.data,
        repositories: reposResult.success ? reposResult.data : undefined
      }
    };
  } catch (error) {
    log('error', '[Timeline] Error getting week posts:', error);
    return {
      success: false,
      error: {
        code: 'TIMELINE_ERROR',
        message: error instanceof Error ? error.message : 'Failed to get timeline posts'
      }
    };
  }
}

/**
 * Ensure we have all necessary data for a specific week
 * Handles cache checking, repository fetching, and post loading
 */
async function ensureWeekData(
  workdir: string,
  storageBase: string,
  weekStart: Date
): Promise<Result<void>> {
  try {
    // First, check if data from the requested start date is in cache
    if (!cache.isCacheRangeCovered(weekStart)) {
      log('debug', '[Timeline] Data from date not in cache, loading additional posts:', {
        since: weekStart.toISOString(),
        cachedStartDates: cache.getCachedRanges()
      });

      // Load additional posts from this date onwards
      await cache.loadAdditionalPosts(workdir, storageBase, weekStart);
      log('debug', '[Timeline] Additional posts loaded, cache updated');
    } else {
      log('debug', '[Timeline] Data from date already in cache, using cached data');
    }

    // Check and fetch repositories that need data for this week
    const fetchResult = await fetchRepositoriesForWeek(workdir, storageBase, weekStart);
    if (!fetchResult.success) {
      return fetchResult;
    }

    return { success: true };
  } catch (error) {
    log('error', '[Timeline] Error ensuring week data:', error);
    return {
      success: false,
      error: {
        code: 'ENSURE_DATA_ERROR',
        message: error instanceof Error ? error.message : 'Failed to ensure week data'
      }
    };
  }
}

/**
 * Fetch repositories that don't have data for the specified week
 * Uses fetchedRanges to intelligently fetch only missing data
 */
async function fetchRepositoriesForWeek(
  workdir: string,
  storageBase: string,
  weekStart: Date
): Promise<Result<void>> {
  try {
    // Get all repositories to check their fetchedRanges
    const reposResult = await repository.getRepositories(workdir, 'following', { skipCache: true });

    if (!reposResult.success || !reposResult.data) {
      // No repositories, nothing to fetch
      return { success: true };
    }

    const repositories = reposResult.data;
    const weekStartStr = weekStart.toISOString().substring(0, 10);

    log('debug', '[Timeline] Checking repositories for week:', {
      weekStart: weekStartStr,
      repoCount: repositories.length
    });

    // Check which repositories need fetching for this week
    const repositoriesNeedingFetch = repositories.filter(repo => {
      if (!repo.fetchedRanges || repo.fetchedRanges.length === 0) {
        log('debug', '[Timeline] Repository has no fetchedRanges:', {
          repoId: repo.id,
          url: repo.url
        });
        return true;
      }

      // Check if this week is covered by any fetched range
      const isWeekCovered = repo.fetchedRanges.some((range: { start: string; end: string }) =>
        range.start <= weekStartStr
      );

      if (!isWeekCovered) {
        log('debug', '[Timeline] Repository needs fetch:', {
          repoId: repo.id,
          weekStart: weekStartStr,
          fetchedRanges: repo.fetchedRanges
        });
      }

      return !isWeekCovered;
    });

    if (repositoriesNeedingFetch.length === 0) {
      log('debug', '[Timeline] All repositories have data for this week');
      return { success: true };
    }

    log('debug', `[Timeline] Found ${repositoriesNeedingFetch.length} repositories needing fetch`);

    // Fetch all repositories in parallel
    const fetchPromises = repositoriesNeedingFetch.map(async (repo) => {
      if (!repo.id) {
        log('error', '[Timeline] Repository missing ID:', repo);
        return { fetched: 0, failed: 1 };
      }

      try {
        const parsed = gitMsgRef.parseRepositoryId(repo.id);
        const fetchResult = await repository.fetchUpdates(
          workdir,
          `repository:${repo.id}`,
          {
            branch: parsed.branch,
            since: weekStartStr
          }
        );

        if (fetchResult.success && fetchResult.data) {
          return { fetched: fetchResult.data.fetched, failed: fetchResult.data.failed };
        } else {
          log('error', `[Timeline] Failed to fetch ${repo.id}:`, fetchResult.error);
          return { fetched: 0, failed: 1 };
        }
      } catch (error) {
        log('error', `[Timeline] Error fetching ${repo.id}:`, error);
        return { fetched: 0, failed: 1 };
      }
    });

    // Wait for all fetches to complete
    const fetchResults = await Promise.all(fetchPromises);

    // Calculate totals
    const totalFetched = fetchResults.reduce((sum, r) => sum + r.fetched, 0);
    const totalFailed = fetchResults.reduce((sum, r) => sum + r.failed, 0);

    log('debug', `[Timeline] Fetched ${totalFetched} repositories, ${totalFailed} failed`);

    // Clear repository cache if something was actually fetched
    if (totalFetched > 0) {
      storage.cache.clear(workdir, 'following');
      log('debug', '[Timeline] Cleared repository cache after fetching');
    }

    // Load posts for all repositories that needed fetching (whether fetched or skipped)
    if (repositoriesNeedingFetch.length > 0) {
      log('debug', `[Timeline] Loading posts for ${repositoriesNeedingFetch.length} repositories`);

      for (const repo of repositoriesNeedingFetch) {
        if (!repo.id) {continue;}
        try {
          const parsed = gitMsgRef.parseRepositoryId(repo.id);
          await cache.loadRepositoryPosts(
            workdir,
            parsed.repository,
            parsed.branch,
            storageBase
          );
          log('debug', `[Timeline] Loaded posts for repository: ${repo.id}`);
        } catch (error) {
          log('error', `[Timeline] Failed to load posts for ${repo.id}:`, error);
        }
      }
    }

    return { success: true };
  } catch (error) {
    log('error', '[Timeline] Error fetching repositories:', error);
    return {
      success: false,
      error: {
        code: 'FETCH_REPOS_ERROR',
        message: error instanceof Error ? error.message : 'Failed to fetch repositories'
      }
    };
  }
}

/**
 * Prefetch data for adjacent weeks in the background
 * This improves UX by having data ready when user navigates
 */
async function prefetchAdjacentWeeks(
  workdir: string,
  storageBase: string,
  currentWeekStart: Date
): Promise<void> {
  try {
    // Calculate previous week start (7 days before)
    const previousWeekStart = new Date(currentWeekStart);
    previousWeekStart.setDate(previousWeekStart.getDate() - 7);

    // Calculate next week start (7 days after)
    const nextWeekStart = new Date(currentWeekStart);
    nextWeekStart.setDate(nextWeekStart.getDate() + 7);

    log('debug', '[Timeline] Starting background prefetch for adjacent weeks:', {
      current: currentWeekStart.toISOString(),
      previous: previousWeekStart.toISOString(),
      next: nextWeekStart.toISOString()
    });

    // Prefetch both weeks in parallel
    const prefetchPromises = [];

    // Check if previous week needs prefetching
    if (!cache.isCacheRangeCovered(previousWeekStart)) {
      log('debug', '[Timeline] Prefetching previous week data');
      prefetchPromises.push(
        ensureWeekData(workdir, storageBase, previousWeekStart)
          .then(result => {
            if (result.success) {
              log('debug', '[Timeline] Previous week prefetch completed');
            } else {
              log('debug', '[Timeline] Previous week prefetch failed:', result.error);
            }
          })
      );
    } else {
      log('debug', '[Timeline] Previous week already in cache');
    }

    // Check if next week needs prefetching (less common but useful for returning users)
    if (!cache.isCacheRangeCovered(nextWeekStart)) {
      log('debug', '[Timeline] Prefetching next week data');
      prefetchPromises.push(
        ensureWeekData(workdir, storageBase, nextWeekStart)
          .then(result => {
            if (result.success) {
              log('debug', '[Timeline] Next week prefetch completed');
            } else {
              log('debug', '[Timeline] Next week prefetch failed:', result.error);
            }
          })
      );
    } else {
      log('debug', '[Timeline] Next week already in cache');
    }

    // Wait for all prefetches to complete
    if (prefetchPromises.length > 0) {
      await Promise.all(prefetchPromises);
      log('debug', '[Timeline] All adjacent week prefetches completed');
    } else {
      log('debug', '[Timeline] No adjacent weeks needed prefetching');
    }
  } catch (error) {
    // This is a background operation, so we just log errors
    log('error', '[Timeline] Error prefetching adjacent weeks:', error);
  }
}
