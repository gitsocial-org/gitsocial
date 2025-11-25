import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  format30DayRange,
  formatDate,
  formatRelativeTime,
  formatWeekRange,
  getLastMonday,
  getTimeRangeLabel,
  getWeekEnd,
  getWeekLabel,
  getWeekStart,
  useEventDrivenTimeUpdates
} from '../../../src/webview/utils/time';

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

    it('should handle invalid string dates gracefully', () => {
      const result = formatRelativeTime('invalid date');
      expect(result).toBe('invalid date');
    });

    it('should handle invalid Date objects gracefully', () => {
      const invalidDate = new Date('invalid');
      const result = formatRelativeTime(invalidDate);
      expect(result).toBe('Invalid date');
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

    it('should handle default parameter', () => {
      expect(getWeekLabel()).toBe('This Week');
    });
  });

  describe('getWeekStart', () => {
    it('should return start of current week for offset 0', () => {
      const result = getWeekStart(0);
      expect(result).toBeInstanceOf(Date);
      expect(result.getDay()).toBe(1);
    });

    it('should return start of previous week for offset -1', () => {
      const result = getWeekStart(-1);
      expect(result).toBeInstanceOf(Date);
      expect(result.getDay()).toBe(1);
    });

    it('should handle default parameter', () => {
      const result = getWeekStart();
      expect(result).toBeInstanceOf(Date);
    });
  });

  describe('getWeekEnd', () => {
    it('should return end of current week for offset 0', () => {
      const result = getWeekEnd(0);
      expect(result).toBeInstanceOf(Date);
      expect(result.getDay()).toBe(0);
    });

    it('should return end of previous week for offset -1', () => {
      const result = getWeekEnd(-1);
      expect(result).toBeInstanceOf(Date);
      expect(result.getDay()).toBe(0);
    });

    it('should handle default parameter', () => {
      const result = getWeekEnd();
      expect(result).toBeInstanceOf(Date);
    });
  });

  describe('getLastMonday', () => {
    it('should return the date of last Monday', () => {
      const result = getLastMonday();
      expect(result).toBeInstanceOf(Date);
      expect(result.getDay()).toBe(1);
    });
  });

  describe('getTimeRangeLabel', () => {
    it('should format time range label', () => {
      const date = new Date('2025-11-03T12:00:00Z');
      const result = getTimeRangeLabel(date);
      expect(result).toMatch(/Data since \w+ \d+/);
    });

    it('should handle different dates', () => {
      const date = new Date('2025-01-15T00:00:00Z');
      const result = getTimeRangeLabel(date);
      expect(result).toContain('Data since');
      expect(result).toContain('Jan');
    });
  });

  describe('useEventDrivenTimeUpdates', () => {
    let onUpdate: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      onUpdate = vi.fn();
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('should call onUpdate immediately on mount', () => {
      useEventDrivenTimeUpdates(onUpdate);
      expect(onUpdate).toHaveBeenCalledTimes(1);
    });

    it('should call onUpdate on visibility change when not hidden', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      Object.defineProperty(document, 'hidden', { value: false, configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));

      expect(onUpdate).toHaveBeenCalled();
    });

    it('should not call onUpdate on visibility change when hidden', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      Object.defineProperty(document, 'hidden', { value: true, configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));

      expect(onUpdate).not.toHaveBeenCalled();
    });

    it('should call onUpdate on focus event with debounce', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      document.dispatchEvent(new Event('focus'));
      vi.advanceTimersByTime(100);

      expect(onUpdate).toHaveBeenCalled();
    });

    it('should call onUpdate on scroll event with debounce', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      document.dispatchEvent(new Event('scroll'));
      vi.advanceTimersByTime(100);

      expect(onUpdate).toHaveBeenCalled();
    });

    it('should call onUpdate on click event with debounce', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      document.dispatchEvent(new Event('click'));
      vi.advanceTimersByTime(100);

      expect(onUpdate).toHaveBeenCalled();
    });

    it('should debounce multiple rapid events', () => {
      useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      document.dispatchEvent(new Event('click'));
      document.dispatchEvent(new Event('click'));
      document.dispatchEvent(new Event('click'));

      vi.advanceTimersByTime(50);
      expect(onUpdate).not.toHaveBeenCalled();

      vi.advanceTimersByTime(50);
      expect(onUpdate).toHaveBeenCalledTimes(1);
    });

    it('should cleanup event listeners when cleanup function is called', () => {
      const cleanup = useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      cleanup();

      document.dispatchEvent(new Event('focus'));
      vi.advanceTimersByTime(100);

      expect(onUpdate).not.toHaveBeenCalled();
    });

    it('should not call onUpdate after cleanup even on visibility change', () => {
      const cleanup = useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      cleanup();

      Object.defineProperty(document, 'hidden', { value: false, configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));

      expect(onUpdate).not.toHaveBeenCalled();
    });

    it('should clear pending timeout on cleanup', () => {
      const cleanup = useEventDrivenTimeUpdates(onUpdate);
      onUpdate.mockClear();

      document.dispatchEvent(new Event('click'));
      cleanup();

      vi.advanceTimersByTime(100);
      expect(onUpdate).not.toHaveBeenCalled();
    });
  });
});
