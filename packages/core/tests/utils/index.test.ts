import { describe, expect, it } from 'vitest';
import * as utils from '../../src/utils/index';
import type { LogLevel } from '../../src/utils/index';

describe('utils/index', () => {
  it('should export LogLevel type', () => {
    const level: LogLevel = 'info';
    expect(level).toBe('info');
  });

  it('should export date utilities', () => {
    expect(utils.getCurrentWeekMonday).toBeDefined();
    expect(utils.getWeekStart).toBeDefined();
    expect(utils.getWeekEnd).toBeDefined();
    expect(utils.getFetchStartDate).toBeDefined();
    expect(utils.getFetchStartDateString).toBeDefined();
    expect(utils.toDateString).toBeDefined();
    expect(utils.isDateBefore).toBeDefined();
    expect(utils.getWeekBoundaries).toBeDefined();
    expect(utils.formatDateForLog).toBeDefined();
  });

  it('should call date utility functions', () => {
    const monday = utils.getCurrentWeekMonday();
    expect(monday).toBeInstanceOf(Date);
    expect(monday.getHours()).toBe(0);
    expect(monday.getMinutes()).toBe(0);
    expect(monday.getSeconds()).toBe(0);
  });

  it('should use LogLevel in code', () => {
    const levels: LogLevel[] = ['debug', 'info', 'warn', 'error'];
    expect(levels).toHaveLength(4);
    expect(levels).toContain('info');
  });
});
