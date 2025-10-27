/**
 * Pure utility functions for Git operations
 * These are stateless functions that don't interact with Git directly
 */

import type { Commit } from './types';

/**
 * Merge two arrays of commits chronologically (newest first)
 * @param localCommits - Commits from my repository
 * @param externalCommits - Commits from external repositories
 * @returns Merged commits sorted by timestamp (newest first)
 */
export function mergeCommitsChronologically(myCommits: Commit[], externalCommits: Commit[]): Commit[] {
  // Combine all commits
  const allCommits = [...myCommits, ...externalCommits];

  // Sort by timestamp in descending order (newest first)
  return allCommits.sort((a, b) => {
    const dateA = new Date(a.timestamp).getTime();
    const dateB = new Date(b.timestamp).getTime();
    return dateB - dateA; // Descending order
  });
}

import { getFetchStartDate as getStartDate } from '../utils/date';

/**
 * Get fetch start date for loading posts (start of this week - last Monday)
 * Pure date calculation utility
 */
export function getFetchStartDate(): string {
  return getStartDate();
}
