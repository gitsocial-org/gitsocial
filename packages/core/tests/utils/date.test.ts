import { describe, expect, it } from 'vitest';
import {
  formatDateForLog,
  getCurrentWeekMonday,
  getFetchStartDate,
  getFetchStartDateString,
  getWeekBoundaries,
  getWeekEnd,
  getWeekStart,
  isDateBefore,
  toDateString
} from '../../src/utils/date';

describe('utils/date', () => {
  describe('getCurrentWeekMonday()', () => {
    it('should return Monday at 00:00:00 for a Monday date', () => {
      const monday = new Date('2025-01-06T15:30:00Z');
      const result = getCurrentWeekMonday(monday);
      expect(result.getDay()).toBe(1);
      expect(result.getHours()).toBe(0);
      expect(result.getMinutes()).toBe(0);
      expect(result.getSeconds()).toBe(0);
      expect(result.getMilliseconds()).toBe(0);
    });

    it('should return previous Monday for a Tuesday', () => {
      const tuesday = new Date('2025-01-07T15:30:00Z');
      const result = getCurrentWeekMonday(tuesday);
      expect(result.getDay()).toBe(1);
      expect(toDateString(result)).toBe('2025-01-06');
    });

    it('should return previous Monday for a Wednesday', () => {
      const wednesday = new Date('2025-01-08T15:30:00Z');
      const result = getCurrentWeekMonday(wednesday);
      expect(result.getDay()).toBe(1);
      expect(toDateString(result)).toBe('2025-01-06');
    });

    it('should return previous Monday for a Sunday', () => {
      const sunday = new Date('2025-01-12T15:30:00Z');
      const result = getCurrentWeekMonday(sunday);
      expect(result.getDay()).toBe(1);
      expect(toDateString(result)).toBe('2025-01-06');
    });

    it('should return previous Monday for a Saturday', () => {
      const saturday = new Date('2025-01-11T15:30:00Z');
      const result = getCurrentWeekMonday(saturday);
      expect(result.getDay()).toBe(1);
      expect(toDateString(result)).toBe('2025-01-06');
    });

    it('should handle year boundary correctly', () => {
      const date = new Date('2025-01-01T15:30:00Z');
      const result = getCurrentWeekMonday(date);
      expect(result.getDay()).toBe(1);
      expect(toDateString(result)).toBe('2024-12-30');
    });

    it('should use current date when no argument provided', () => {
      const result = getCurrentWeekMonday();
      expect(result.getDay()).toBe(1);
      expect(result.getHours()).toBe(0);
      expect(result.getMinutes()).toBe(0);
    });
  });

  describe('getWeekStart()', () => {
    it('should return current week Monday when offset is 0', () => {
      const result = getWeekStart(0);
      expect(result.getDay()).toBe(1);
      expect(result.getHours()).toBe(0);
    });

    it('should return previous week Monday when offset is -1', () => {
      const currentMonday = getCurrentWeekMonday(new Date('2025-01-06'));
      const result = getWeekStart(-1);
      const expected = new Date(currentMonday);
      expected.setDate(expected.getDate() - 7);
      expect(result.getDay()).toBe(1);
    });

    it('should return next week Monday when offset is 1', () => {
      const currentMonday = getCurrentWeekMonday(new Date('2025-01-06'));
      const result = getWeekStart(1);
      const expected = new Date(currentMonday);
      expected.setDate(expected.getDate() + 7);
      expect(result.getDay()).toBe(1);
    });

    it('should handle multiple weeks offset', () => {
      const result = getWeekStart(-4);
      expect(result.getDay()).toBe(1);
    });

    it('should use default offset of 0 when not provided', () => {
      const result1 = getWeekStart();
      const result2 = getWeekStart(0);
      expect(result1.getTime()).toBe(result2.getTime());
    });
  });

  describe('getWeekEnd()', () => {
    it('should return Sunday at 23:59:59.999 for current week', () => {
      const result = getWeekEnd(0);
      expect(result.getDay()).toBe(0);
      expect(result.getHours()).toBe(23);
      expect(result.getMinutes()).toBe(59);
      expect(result.getSeconds()).toBe(59);
      expect(result.getMilliseconds()).toBe(999);
    });

    it('should return 6 days after week start', () => {
      const weekStart = getWeekStart(0);
      const weekEnd = getWeekEnd(0);
      const diffMs = weekEnd.getTime() - weekStart.getTime();
      const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
      expect(diffDays).toBe(6);
    });

    it('should handle previous week offset', () => {
      const result = getWeekEnd(-1);
      expect(result.getDay()).toBe(0);
      expect(result.getHours()).toBe(23);
    });

    it('should handle future week offset', () => {
      const result = getWeekEnd(2);
      expect(result.getDay()).toBe(0);
    });

    it('should use default offset of 0 when not provided', () => {
      const result1 = getWeekEnd();
      const result2 = getWeekEnd(0);
      expect(result1.getTime()).toBe(result2.getTime());
    });
  });

  describe('getFetchStartDate()', () => {
    it('should return ISO string format', () => {
      const result = getFetchStartDate();
      expect(result).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    });

    it('should return current week Monday at midnight', () => {
      const result = getFetchStartDate();
      const date = new Date(result);
      expect(date.getDay()).toBe(1);
      expect(date.getHours()).toBe(0);
      expect(date.getMinutes()).toBe(0);
      expect(date.getSeconds()).toBe(0);
    });
  });

  describe('getFetchStartDateString()', () => {
    it('should return YYYY-MM-DD format', () => {
      const result = getFetchStartDateString();
      expect(result).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    });

    it('should return current week Monday date', () => {
      const result = getFetchStartDateString();
      const isoResult = getFetchStartDate();
      expect(result).toBe(isoResult.substring(0, 10));
    });
  });

  describe('toDateString()', () => {
    it('should convert date to YYYY-MM-DD format', () => {
      const date = new Date('2025-01-15T12:30:45.123Z');
      const result = toDateString(date);
      expect(result).toBe('2025-01-15');
    });

    it('should handle dates at year boundary', () => {
      const date = new Date('2024-12-31T23:59:59.999Z');
      const result = toDateString(date);
      expect(result).toBe('2024-12-31');
    });

    it('should handle dates at start of year', () => {
      const date = new Date('2025-01-01T00:00:00.000Z');
      const result = toDateString(date);
      expect(result).toBe('2025-01-01');
    });

    it('should handle leap year dates', () => {
      const date = new Date('2024-02-29T12:00:00.000Z');
      const result = toDateString(date);
      expect(result).toBe('2024-02-29');
    });
  });

  describe('isDateBefore()', () => {
    it('should return true when first date is before second', () => {
      expect(isDateBefore('2025-01-01', '2025-01-02')).toBe(true);
    });

    it('should return false when first date is after second', () => {
      expect(isDateBefore('2025-01-15', '2025-01-10')).toBe(false);
    });

    it('should return true when dates are equal', () => {
      expect(isDateBefore('2025-01-10', '2025-01-10')).toBe(true);
    });

    it('should handle year boundaries', () => {
      expect(isDateBefore('2024-12-31', '2025-01-01')).toBe(true);
      expect(isDateBefore('2025-01-01', '2024-12-31')).toBe(false);
    });

    it('should handle month boundaries', () => {
      expect(isDateBefore('2025-01-31', '2025-02-01')).toBe(true);
    });

    it('should perform string comparison', () => {
      expect(isDateBefore('2025-01-09', '2025-01-10')).toBe(true);
      expect(isDateBefore('2025-02-01', '2025-11-01')).toBe(true);
    });
  });

  describe('getWeekBoundaries()', () => {
    it('should return start and end of week', () => {
      const date = new Date('2025-01-08T15:30:00Z');
      const result = getWeekBoundaries(date);
      expect(result.start.getDay()).toBe(1);
      expect(result.end.getDay()).toBe(0);
    });

    it('should return start at 00:00:00 and end at 23:59:59.999', () => {
      const date = new Date('2025-01-10T12:00:00Z');
      const result = getWeekBoundaries(date);
      expect(result.start.getHours()).toBe(0);
      expect(result.start.getMinutes()).toBe(0);
      expect(result.end.getHours()).toBe(23);
      expect(result.end.getMinutes()).toBe(59);
      expect(result.end.getSeconds()).toBe(59);
      expect(result.end.getMilliseconds()).toBe(999);
    });

    it('should span exactly 6 days and ~24 hours', () => {
      const date = new Date('2025-01-08T15:30:00Z');
      const result = getWeekBoundaries(date);
      const diffMs = result.end.getTime() - result.start.getTime();
      const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
      expect(diffDays).toBe(6);
    });

    it('should handle Monday input', () => {
      const monday = new Date('2025-01-06T10:00:00');
      const result = getWeekBoundaries(monday);
      const startDay = result.start.getDay();
      const endDay = result.end.getDay();
      expect(startDay).toBe(1);
      expect(endDay).toBe(0);
    });

    it('should handle Sunday input', () => {
      const sunday = new Date('2025-01-12T22:00:00');
      const result = getWeekBoundaries(sunday);
      const startDay = result.start.getDay();
      const endDay = result.end.getDay();
      expect(startDay).toBe(1);
      expect(endDay).toBe(0);
    });

    it('should use current date when not provided', () => {
      const result = getWeekBoundaries();
      expect(result.start.getDay()).toBe(1);
      expect(result.end.getDay()).toBe(0);
    });
  });

  describe('formatDateForLog()', () => {
    it('should format date as YYYY-MM-DD', () => {
      const date = new Date('2025-01-15T12:30:45.123Z');
      const result = formatDateForLog(date);
      expect(result).toBe('2025-01-15');
    });

    it('should match toDateString output', () => {
      const date = new Date('2025-03-20T08:15:00.000Z');
      const formatted = formatDateForLog(date);
      const toString = toDateString(date);
      expect(formatted).toBe(toString);
    });

    it('should handle edge case dates', () => {
      const date1 = new Date('2024-02-29T12:00:00.000Z');
      expect(formatDateForLog(date1)).toBe('2024-02-29');
      const date2 = new Date('2025-01-01T00:00:00.000Z');
      expect(formatDateForLog(date2)).toBe('2025-01-01');
    });
  });
});
