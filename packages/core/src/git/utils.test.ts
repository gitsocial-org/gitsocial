import { describe, expect, it } from 'vitest';
import { getFetchStartDate, mergeCommitsChronologically } from './utils';
import type { Commit } from './types';

describe('git/utils', () => {
  describe('mergeCommitsChronologically()', () => {
    it('should merge and sort commits by timestamp', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc123',
          message: 'My commit',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];

      const externalCommits: Commit[] = [
        {
          hash: 'def456',
          message: 'External commit',
          author: 'Them',
          email: 'them@example.com',
          timestamp: new Date('2024-01-16T12:00:00Z')
        }
      ];

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(2);
      expect(merged[0]?.hash).toBe('def456');
      expect(merged[1]?.hash).toBe('abc123');
    });

    it('should handle empty my commits array', () => {
      const myCommits: Commit[] = [];
      const externalCommits: Commit[] = [
        {
          hash: 'def456',
          message: 'External commit',
          author: 'Them',
          email: 'them@example.com',
          timestamp: new Date('2024-01-16T12:00:00Z')
        }
      ];

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(1);
      expect(merged[0]?.hash).toBe('def456');
    });

    it('should handle empty external commits array', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc123',
          message: 'My commit',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];
      const externalCommits: Commit[] = [];

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(1);
      expect(merged[0]?.hash).toBe('abc123');
    });

    it('should handle both arrays empty', () => {
      const merged = mergeCommitsChronologically([], []);

      expect(merged.length).toBe(0);
    });

    it('should sort multiple commits correctly', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc1',
          message: 'Oldest',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-10T12:00:00Z')
        },
        {
          hash: 'abc3',
          message: 'Newest',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-16T12:00:00Z')
        }
      ];

      const externalCommits: Commit[] = [
        {
          hash: 'def2',
          message: 'Middle',
          author: 'Them',
          email: 'them@example.com',
          timestamp: new Date('2024-01-13T12:00:00Z')
        }
      ];

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(3);
      expect(merged[0]?.hash).toBe('abc3');
      expect(merged[1]?.hash).toBe('def2');
      expect(merged[2]?.hash).toBe('abc1');
    });

    it('should handle commits with same timestamp', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc1',
          message: 'Commit 1',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];

      const externalCommits: Commit[] = [
        {
          hash: 'def2',
          message: 'Commit 2',
          author: 'Them',
          email: 'them@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(2);
      const hashes = merged.map(c => c.hash);
      expect(hashes).toContain('abc1');
      expect(hashes).toContain('def2');
    });

    it('should preserve all commit properties', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc123',
          message: 'My commit',
          author: 'John Doe',
          email: 'john@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];

      const merged = mergeCommitsChronologically(myCommits, []);

      expect(merged[0]).toEqual(myCommits[0]);
    });

    it('should handle large number of commits', () => {
      const baseTime = new Date('2024-01-01T00:00:00Z').getTime();
      const myCommits: Commit[] = Array.from({ length: 50 }, (_, i) => ({
        hash: `my${i}`,
        message: `My commit ${i}`,
        author: 'Me',
        email: 'me@example.com',
        timestamp: new Date(baseTime + i * 1000)
      }));

      const externalCommits: Commit[] = Array.from({ length: 50 }, (_, i) => ({
        hash: `ext${i}`,
        message: `External commit ${i}`,
        author: 'Them',
        email: 'them@example.com',
        timestamp: new Date(baseTime + (i + 50) * 1000)
      }));

      const merged = mergeCommitsChronologically(myCommits, externalCommits);

      expect(merged.length).toBe(100);
      expect(merged[0]?.hash).toContain('ext');
      expect(merged[99]?.hash).toContain('my');
    });

    it('should not modify original arrays', () => {
      const myCommits: Commit[] = [
        {
          hash: 'abc123',
          message: 'My commit',
          author: 'Me',
          email: 'me@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z')
        }
      ];

      const externalCommits: Commit[] = [
        {
          hash: 'def456',
          message: 'External commit',
          author: 'Them',
          email: 'them@example.com',
          timestamp: new Date('2024-01-16T12:00:00Z')
        }
      ];

      const myOriginal = [...myCommits];
      const extOriginal = [...externalCommits];

      mergeCommitsChronologically(myCommits, externalCommits);

      expect(myCommits).toEqual(myOriginal);
      expect(externalCommits).toEqual(extOriginal);
    });
  });

  describe('getFetchStartDate()', () => {
    it('should return a date string', () => {
      const date = getFetchStartDate();
      expect(typeof date).toBe('string');
      expect(date.length).toBeGreaterThan(0);
    });

    it('should return ISO format date', () => {
      const date = getFetchStartDate();
      expect(date).toMatch(/^\d{4}-\d{2}-\d{2}/);
    });

    it('should return consistent value in same call', () => {
      const date1 = getFetchStartDate();
      const date2 = getFetchStartDate();
      expect(date1).toBe(date2);
    });

    it('should return valid date that can be parsed', () => {
      const dateStr = getFetchStartDate();
      const dateObj = new Date(dateStr);
      expect(dateObj).toBeInstanceOf(Date);
      expect(isNaN(dateObj.getTime())).toBe(false);
    });
  });
});
