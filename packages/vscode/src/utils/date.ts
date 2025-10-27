/**
 * Date utilities for GitSocial
 */

/**
 * Get this week's Monday (start of current week) for displaying a week's worth of data.
 */
export function getLastMonday(): Date {
  const now = new Date();
  const dayOfWeek = now.getDay();
  const daysToMonday = dayOfWeek === 0 ? 6 : dayOfWeek - 1;
  const thisMonday = new Date(now);
  thisMonday.setDate(now.getDate() - daysToMonday);
  thisMonday.setHours(0, 0, 0, 0);
  return thisMonday;
}

/**
 * Get the fetch start date as an ISO string (date only)
 */
export function getFetchStartDate(): string {
  return getLastMonday().toISOString().split('T')[0];
}

/**
 * Format a date for display in debug/log messages
 */
export function formatDateForLog(date: Date): string {
  return date.toISOString().split('T')[0]; // YYYY-MM-DD
}

/**
 * Get a human-readable description of the time range
 */
export function getTimeRangeDescription(since: Date): string {
  const now = new Date();
  const days = Math.floor((now.getTime() - since.getTime()) / (1000 * 60 * 60 * 24));

  if (days === 7) {
    return 'past week';
  } else if (days === 14) {
    return 'past two weeks';
  } else if (days < 7) {
    return `past ${days} days`;
  } else {
    return `since ${since.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric' })}`;
  }
}
