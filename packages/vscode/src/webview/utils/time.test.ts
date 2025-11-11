import { beforeEach, describe, expect, it, vi } from 'vitest';
import { format30DayRange, formatDate, formatRelativeTime, formatWeekRange, getWeekLabel } from './time';

describe('time utilities', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2025-11-11T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe('formatRelativeTime', () => {
    it('should return "Never" for null/undefined', () => {
      expect(formatRelativeTime(null as unknown as Date)).toBe('Never');
      expect(formatRelativeTime(undefined as unknown as Date)).toBe('Never');
    });

    it('should format seconds', () => {
      const date = new Date('2025-11-11T11:59:30Z');
      expect(formatRelativeTime(date)).toBe('30s');
    });

    it('should format minutes', () => {
      const date = new Date('2025-11-11T11:45:00Z');
      expect(formatRelativeTime(date)).toBe('15m');
    });

    it('should format hours', () => {
      const date = new Date('2025-11-11T09:00:00Z');
      expect(formatRelativeTime(date)).toBe('3h');
    });

    it('should format exact date for >= 24 hours', () => {
      const date = new Date('2025-11-10T11:00:00Z');
      const result = formatRelativeTime(date);
      expect(result).toMatch(/Nov 10, 2025/);
    });

    it('should handle string dates', () => {
      const dateString = '2025-11-11T11:45:00Z';
      expect(formatRelativeTime(dateString)).toBe('15m');
    });

    it('should handle timestamp numbers', () => {
      const timestamp = new Date('2025-11-11T11:45:00Z').getTime();
      expect(formatRelativeTime(timestamp)).toBe('15m');
    });

    it('should handle invalid dates gracefully', () => {
      const result = formatRelativeTime('invalid date');
      expect(result).toBe('invalid date');
    });
  });

  describe('formatDate', () => {
    it('should format Date object', () => {
      const date = new Date('2025-11-11T15:30:00Z');
      const result = formatDate(date);
      expect(result).toContain('Nov 11, 2025');
      expect(result).toContain(':30');
    });

    it('should format date string', () => {
      const dateString = '2025-11-11T15:30:00Z';
      const result = formatDate(dateString);
      expect(result).toContain('Nov 11, 2025');
      expect(result).toContain(':30');
    });
  });

  describe('formatWeekRange', () => {
    it('should format same month range', () => {
      const start = new Date('2025-11-03T12:00:00Z');
      const end = new Date('2025-11-09T12:00:00Z');
      const result = formatWeekRange(start, end);
      expect(result).toMatch(/Nov \d+ - \d+/);
    });

    it('should format cross-month range', () => {
      const start = new Date('2025-10-27T12:00:00Z');
      const end = new Date('2025-11-02T12:00:00Z');
      const result = formatWeekRange(start, end);
      expect(result).toMatch(/Oct \d+ - Nov \d+/);
    });
  });

  describe('format30DayRange', () => {
    it('should format same month range', () => {
      const start = new Date('2025-11-01T12:00:00Z');
      const end = new Date('2025-11-30T12:00:00Z');
      const result = format30DayRange(start, end);
      expect(result).toMatch(/Nov \d+ - \d+/);
    });

    it('should format cross-month range', () => {
      const start = new Date('2025-10-15T12:00:00Z');
      const end = new Date('2025-11-14T12:00:00Z');
      const result = format30DayRange(start, end);
      expect(result).toMatch(/Oct \d+ - Nov \d+/);
    });
  });

  describe('getWeekLabel', () => {
    it('should return "This Week" for offset 0', () => {
      expect(getWeekLabel(0)).toBe('This Week');
    });

    it('should return formatted date range for other offsets', () => {
      const result = getWeekLabel(-1);
      expect(result).toMatch(/\w+ \d+ - (\d+|\w+ \d+)/);
    });
  });
});
