import { describe, expect, it } from 'vitest';
import { sortPosts } from '../../../src/webview/utils/sorting';
import type { Post } from '@gitsocial/core/client';

function createMockPost(overrides: Partial<Post> = {}): Post {
  return {
    id: 'test-id',
    content: 'Test content',
    timestamp: new Date(),
    author: {
      name: 'Test Author',
      email: 'test@example.com',
      repository: 'https://github.com/test/repo'
    },
    repository: 'https://github.com/test/repo',
    ...overrides
  } as Post;
}

describe('sorting utilities', () => {
  describe('sortPosts', () => {
    it('should not mutate original array', () => {
      const posts = [
        createMockPost({ id: '1', timestamp: new Date('2025-11-01') }),
        createMockPost({ id: '2', timestamp: new Date('2025-11-02') })
      ];
      const original = [...posts];
      sortPosts(posts, 'latest');
      expect(posts).toEqual(original);
    });

    describe('latest sorting', () => {
      it('should sort by newest first', () => {
        const posts = [
          createMockPost({ id: '1', timestamp: new Date('2025-11-01') }),
          createMockPost({ id: '2', timestamp: new Date('2025-11-03') }),
          createMockPost({ id: '3', timestamp: new Date('2025-11-02') })
        ];
        const sorted = sortPosts(posts, 'latest');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('3');
        expect(sorted[2].id).toBe('1');
      });

      it('should handle string timestamps', () => {
        const posts = [
          createMockPost({ id: '1', timestamp: '2025-11-01' as unknown as Date }),
          createMockPost({ id: '2', timestamp: '2025-11-03' as unknown as Date })
        ];
        const sorted = sortPosts(posts, 'latest');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('1');
      });
    });

    describe('oldest sorting', () => {
      it('should sort by oldest first', () => {
        const posts = [
          createMockPost({ id: '1', timestamp: new Date('2025-11-03') }),
          createMockPost({ id: '2', timestamp: new Date('2025-11-01') }),
          createMockPost({ id: '3', timestamp: new Date('2025-11-02') })
        ];
        const sorted = sortPosts(posts, 'oldest');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('3');
        expect(sorted[2].id).toBe('1');
      });
    });

    describe('top sorting', () => {
      it('should sort by comment count', () => {
        const posts = [
          createMockPost({ id: '1', interactions: { comments: 5, reposts: 0, quotes: 0 } }),
          createMockPost({ id: '2', interactions: { comments: 10, reposts: 0, quotes: 0 } }),
          createMockPost({ id: '3', interactions: { comments: 2, reposts: 0, quotes: 0 } })
        ];
        const sorted = sortPosts(posts, 'top');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('1');
        expect(sorted[2].id).toBe('3');
      });

      it('should handle missing interactions', () => {
        const posts = [
          createMockPost({ id: '1' }),
          createMockPost({ id: '2', interactions: { comments: 5, reposts: 0, quotes: 0 } })
        ];
        const sorted = sortPosts(posts, 'top');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('1');
      });

      it('should use timestamp as tiebreaker', () => {
        const posts = [
          createMockPost({
            id: '1',
            timestamp: new Date('2025-11-01'),
            interactions: { comments: 5, reposts: 0, quotes: 0 }
          }),
          createMockPost({
            id: '2',
            timestamp: new Date('2025-11-03'),
            interactions: { comments: 5, reposts: 0, quotes: 0 }
          })
        ];
        const sorted = sortPosts(posts, 'top');
        expect(sorted[0].id).toBe('2');
        expect(sorted[1].id).toBe('1');
      });
    });

    describe('invalid timestamps', () => {
      it('should handle invalid timestamps gracefully', () => {
        const posts = [
          createMockPost({ id: '1', timestamp: 'invalid' as unknown as Date }),
          createMockPost({ id: '2', timestamp: new Date('2025-11-01') })
        ];
        const sorted = sortPosts(posts, 'latest');
        expect(sorted).toHaveLength(2);
      });
    });

    describe('empty arrays', () => {
      it('should handle empty array', () => {
        const sorted = sortPosts([], 'latest');
        expect(sorted).toEqual([]);
      });
    });
  });
});
