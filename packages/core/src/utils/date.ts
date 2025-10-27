/**
 * Centralized date utilities for GitSocial
 * Ensures consistent date handling across the codebase
 */

/**
 * Get the start of the current week (Monday at 00:00:00)
 */
export function getCurrentWeekMonday(date: Date = new Date()): Date {
  const result = new Date(date);
  const dayOfWeek = result.getDay();
  const daysToMonday = dayOfWeek === 0 ? 6 : dayOfWeek - 1;
  result.setDate(result.getDate() - daysToMonday);
  result.setHours(0, 0, 0, 0);
  return result;
}

/**
 * Get the start of a specific week by offset from current week
 * @param offset Number of weeks to offset (negative for past weeks)
 */
export function getWeekStart(offset: number = 0): Date {
  const currentMonday = getCurrentWeekMonday();
  if (offset !== 0) {
    currentMonday.setDate(currentMonday.getDate() + (offset * 7));
  }
  return currentMonday;
}

/**
 * Get the end of a specific week (Sunday at 23:59:59.999)
 * @param offset Number of weeks to offset (negative for past weeks)
 */
export function getWeekEnd(offset: number = 0): Date {
  const weekStart = getWeekStart(offset);
  const weekEnd = new Date(weekStart);
  weekEnd.setDate(weekStart.getDate() + 6);
  weekEnd.setHours(23, 59, 59, 999);
  return weekEnd;
}

/**
 * Get the fetch start date for loading posts (start of current week)
 * This is used as the default date for initial repository fetching
 */
export function getFetchStartDate(): string {
  return getCurrentWeekMonday().toISOString();
}

/**
 * Get the fetch start date as YYYY-MM-DD format
 */
export function getFetchStartDateString(): string {
  return getCurrentWeekMonday().toISOString().substring(0, 10);
}

/**
 * Convert a date to YYYY-MM-DD string format
 */
export function toDateString(date: Date): string {
  return date.toISOString().substring(0, 10);
}

/**
 * Check if a date string (YYYY-MM-DD) is before or equal to another
 */
export function isDateBefore(date1: string, date2: string): boolean {
  return date1 <= date2;
}

/**
 * Get week boundaries for a given date
 * Returns { start: Date, end: Date } for the week containing the date
 */
export function getWeekBoundaries(date: Date = new Date()): { start: Date; end: Date } {
  const start = getCurrentWeekMonday(date);
  const end = new Date(start);
  end.setDate(start.getDate() + 6);
  end.setHours(23, 59, 59, 999);
  return { start, end };
}

/**
 * Format a date for logging (YYYY-MM-DD)
 */
export function formatDateForLog(date: Date): string {
  return toDateString(date);
}
