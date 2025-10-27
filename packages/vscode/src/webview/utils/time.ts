/**
 * Format a date as relative time (e.g., "2 hours ago") or absolute date for older posts
 * Shows relative time for < 24 hours, then switches to exact date
 */
export function formatRelativeTime(date: Date | string | number): string {
  if (!date) {return 'Never';}

  try {
    const dateObj = typeof date === 'string' ? new Date(date) : typeof date === 'number' ? new Date(date) : date;
    const now = new Date();
    const diff = now.getTime() - dateObj.getTime();

    if (diff < 60_000) {return `${Math.floor(diff / 1000)}s`;}
    if (diff < 3_600_000) {return `${Math.floor(diff / 60_000)}m`;}
    if (diff < 86_400_000) {return `${Math.floor(diff / 3_600_000)}h`;}

    // After 24 hours, show exact date
    return new Intl.DateTimeFormat('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    }).format(dateObj);
  } catch {
    return typeof date === 'string' ? date : 'Invalid date';
  }
}

/**
 * Sets up event-driven time updates for webview components
 * Updates timestamps on visibility changes and user interactions
 */
export function useEventDrivenTimeUpdates(onUpdate: () => void): () => void {
  let isActive = true;

  // Update on visibility changes
  const handleVisibilityChange = (): void => {
    if (!document.hidden && isActive) {
      onUpdate();
    }
  };

  // Update on user interactions (debounced)
  let updateTimeout: ReturnType<typeof setTimeout> | null = null;
  const debouncedUpdate = (): void => {
    if (updateTimeout) {clearTimeout(updateTimeout);}
    updateTimeout = setTimeout(() => {
      if (isActive) {onUpdate();}
    }, 100);
  };

  const handleInteraction = (): void => {
    if (isActive) {debouncedUpdate();}
  };

  // Set up event listeners
  document.addEventListener('visibilitychange', handleVisibilityChange);
  document.addEventListener('focus', handleInteraction);
  document.addEventListener('scroll', handleInteraction, { passive: true });
  document.addEventListener('click', handleInteraction);

  // Initial update
  onUpdate();

  // Cleanup function
  const cleanup = (): void => {
    isActive = false;
    if (updateTimeout) {clearTimeout(updateTimeout);}
    document.removeEventListener('visibilitychange', handleVisibilityChange);
    document.removeEventListener('focus', handleInteraction);
    document.removeEventListener('scroll', handleInteraction);
    document.removeEventListener('click', handleInteraction);
  };

  return cleanup;
}

/**
 * Format a date as a readable string
 */
export function formatDate(date: Date | string): string {
  const dateObj = typeof date === 'string' ? new Date(date) : date;
  return new Intl.DateTimeFormat('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(dateObj);
}

import { getCurrentWeekMonday, getWeekEnd as getWeekEndCore, getWeekStart as getWeekStartCore } from '@gitsocial/core/client';

/**
 * Get the date of last Monday at midnight
 * @deprecated Use getCurrentWeekMonday from core instead
 */
export function getLastMonday(): Date {
  return getCurrentWeekMonday();
}

/**
 * Get a formatted time range label for data since a given date
 */
export function getTimeRangeLabel(since: Date): string {
  return `Data since ${since.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}`;
}

/**
 * Get the start date (Monday) of a week with the given offset
 * @param offset 0 = this week, -1 = last week, -2 = 2 weeks ago, etc.
 */
export function getWeekStart(offset = 0): Date {
  return getWeekStartCore(offset);
}

/**
 * Get the end date (Sunday) of a week with the given offset
 * @param offset 0 = this week, -1 = last week, -2 = 2 weeks ago, etc.
 */
export function getWeekEnd(offset = 0): Date {
  return getWeekEndCore(offset);
}

/**
 * Format a week date range for display
 * @param start Start date (Monday)
 * @param end End date (Sunday)
 */
export function formatWeekRange(start: Date, end: Date): string {
  const sameMonth = start.getMonth() === end.getMonth();

  if (sameMonth) {
    return `${start.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})} - ${end.getDate()}`;
  } else {
    return `${start.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})} - ${end.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})}`;
  }
}

/**
 * Format a 30-day date range in a human-readable format
 * @param start Start date
 * @param end End date
 * @returns Formatted string like "Aug 28 - Sep 27" or "Sep 1 - 30"
 */
export function format30DayRange(start: Date, end: Date): string {
  const sameMonth = start.getMonth() === end.getMonth();

  if (sameMonth) {
    return `${start.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})} - ${end.getDate()}`;
  } else {
    return `${start.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})} - ${end.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})}`;
  }
}

/**
 * Get a human-readable week label
 * @param offset 0 = "This Week", -1 = date range, etc.
 */
export function getWeekLabel(offset = 0): string {
  if (offset === 0) {
    return 'This Week';
  }
  const start = getWeekStart(offset);
  const end = getWeekEnd(offset);
  return formatWeekRange(start, end);
}
