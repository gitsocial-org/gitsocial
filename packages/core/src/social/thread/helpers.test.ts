import { describe, expect, it } from 'vitest';
import {
  buildParentChildMap,
  calculateDepth,
  matchesPostId,
  sortPosts,
  sortThreadTree
} from './helpers';
import type { Post } from '../types';

describe('social/thread/helpers', () => {
  describe('matchesPostId()', () => {
    it('should return false for undefined postId', () => {
      expect(matchesPostId(undefined, 'target-id')).toBe(false);
    });

    it('should match exact equal IDs', () => {
      const id = 'https://github.com/user/repo#commit:1234567890abcdef1234567890abcdef12345678';
      expect(matchesPostId(id, id)).toBe(true);
    });

    it('should match commit hashes across different URL formats', () => {
      const id1 = 'https://github.com/user/repo#commit:1234567890abcdef1234567890abcdef12345678';
      const id2 = 'https://github.com/other/repo#commit:1234567890abcdef1234567890abcdef12345678';
      expect(matchesPostId(id1, id2)).toBe(true);
    });

    it('should not match different commit hashes', () => {
      const id1 = 'https://github.com/user/repo#commit:1234567890abcdef1234567890abcdef12345678';
      const id2 = 'https://github.com/user/repo#commit:abcdef1234567890abcdef1234567890abcdef12';
      expect(matchesPostId(id1, id2)).toBe(false);
    });

    it('should not match different types of references', () => {
      const id1 = 'https://github.com/user/repo#commit:123456789abc';
      const id2 = 'https://github.com/user/repo#branch:main';
      expect(matchesPostId(id1, id2)).toBe(false);
    });
  });

  describe('buildParentChildMap()', () => {
    it('should build map of parent posts', () => {
      const posts: Post[] = [
        {
          id: 'https://github.com/user/repo#commit:post1',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Original',
          type: 'post',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        },
        {
          id: 'https://github.com/user/repo#commit:comment1',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Comment',
          type: 'comment',
          originalPostId: 'https://github.com/user/repo#commit:post1',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const map = buildParentChildMap(posts);

      expect(map.has('https://github.com/user/repo#commit:post1')).toBe(true);
      expect(map.size).toBe(1);
    });

    it('should not include repost originalPostId in parent map', () => {
      const posts: Post[] = [
        {
          id: 'https://github.com/user/repo#commit:repost1',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: '',
          type: 'repost',
          originalPostId: 'https://github.com/user/repo#commit:post1',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const map = buildParentChildMap(posts);

      expect(map.has('https://github.com/user/repo#commit:post1')).toBe(false);
      expect(map.size).toBe(0);
    });

    it('should include parentCommentId in parent map', () => {
      const posts: Post[] = [
        {
          id: 'https://github.com/user/repo#commit:nested',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Nested',
          type: 'comment',
          originalPostId: 'https://github.com/user/repo#commit:post1',
          parentCommentId: 'https://github.com/user/repo#commit:comment1',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const map = buildParentChildMap(posts);

      expect(map.has('https://github.com/user/repo#commit:post1')).toBe(true);
      expect(map.has('https://github.com/user/repo#commit:comment1')).toBe(true);
      expect(map.size).toBe(2);
    });

    it('should handle empty posts array', () => {
      const map = buildParentChildMap([]);

      expect(map.size).toBe(0);
    });
  });

  describe('calculateDepth()', () => {
    it('should return 0 for anchor post itself', () => {
      const post: Post = {
        id: 'https://github.com/user/repo#commit:post1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Post',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const depth = calculateDepth(post, post, [post]);

      expect(depth).toBe(0);
    });

    it('should calculate depth for direct child', () => {
      const parent: Post = {
        id: 'https://github.com/user/repo#commit:post1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Parent',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const child: Post = {
        id: 'https://github.com/user/repo#commit:comment1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Child',
        type: 'comment',
        originalPostId: parent.id,
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const depth = calculateDepth(child, parent, [parent, child]);

      expect(depth).toBe(1);
    });

    it('should calculate depth for nested child', () => {
      const parent: Post = {
        id: 'https://github.com/user/repo#commit:post1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Parent',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const child1: Post = {
        id: 'https://github.com/user/repo#commit:comment1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Child 1',
        type: 'comment',
        originalPostId: parent.id,
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const child2: Post = {
        id: 'https://github.com/user/repo#commit:comment2',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Child 2',
        type: 'comment',
        originalPostId: parent.id,
        parentCommentId: child1.id,
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const depth = calculateDepth(child2, parent, [parent, child1, child2]);

      expect(depth).toBe(2);
    });

    it('should calculate negative depth when post is ancestor of anchor', () => {
      const grandparent: Post = {
        id: 'https://github.com/user/repo#commit:post1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Grandparent',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const parent: Post = {
        id: 'https://github.com/user/repo#commit:comment1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Parent',
        type: 'comment',
        originalPostId: grandparent.id,
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const depth = calculateDepth(grandparent, parent, [grandparent, parent]);

      expect(depth).toBe(-1);
    });

    it('should return 0 when no relationship exists', () => {
      const post1: Post = {
        id: 'https://github.com/user/repo#commit:post1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Post 1',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const post2: Post = {
        id: 'https://github.com/user/repo#commit:post2',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Post 2',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const depth = calculateDepth(post1, post2, [post1, post2]);

      expect(depth).toBe(0);
    });
  });

  describe('sortPosts()', () => {
    const now = Date.now();
    const posts: Post[] = [
      {
        id: 'id1',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date(now - 3000).toISOString(),
        content: 'Old',
        type: 'post',
        likes: 0,
        comments: 5,
        reposts: 0,
        quotes: 0,
        interactions: { comments: 5, likes: 0, reposts: 0, quotes: 0 }
      },
      {
        id: 'id2',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date(now - 1000).toISOString(),
        content: 'New',
        type: 'post',
        likes: 0,
        comments: 2,
        reposts: 0,
        quotes: 0,
        interactions: { comments: 2, likes: 0, reposts: 0, quotes: 0 }
      },
      {
        id: 'id3',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date(now - 2000).toISOString(),
        content: 'Middle',
        type: 'post',
        likes: 0,
        comments: 10,
        reposts: 0,
        quotes: 0,
        interactions: { comments: 10, likes: 0, reposts: 0, quotes: 0 }
      }
    ];

    it('should sort by top (most comments first, then by timestamp)', () => {
      const sorted = sortPosts(posts, 'top');

      expect(sorted[0].id).toBe('id3');
      expect(sorted[1].id).toBe('id1');
      expect(sorted[2].id).toBe('id2');
    });

    it('should sort by latest (newest first)', () => {
      const sorted = sortPosts(posts, 'latest');

      expect(sorted[0].id).toBe('id2');
      expect(sorted[1].id).toBe('id3');
      expect(sorted[2].id).toBe('id1');
    });

    it('should sort by oldest (oldest first)', () => {
      const sorted = sortPosts(posts, 'oldest');

      expect(sorted[0].id).toBe('id1');
      expect(sorted[1].id).toBe('id3');
      expect(sorted[2].id).toBe('id2');
    });

    it('should return original order for unknown sort', () => {
      // @ts-expect-error Testing invalid sort value
      const sorted = sortPosts(posts, 'unknown');

      expect(sorted[0].id).toBe('id1');
      expect(sorted[1].id).toBe('id2');
      expect(sorted[2].id).toBe('id3');
    });

    it('should not mutate original array', () => {
      const original = [...posts];
      sortPosts(posts, 'latest');

      expect(posts).toEqual(original);
    });

    it('should handle posts without interactions', () => {
      const postsWithoutInteractions: Post[] = [
        {
          id: 'id1',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date(now - 1000).toISOString(),
          content: 'Post',
          type: 'post',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const sorted = sortPosts(postsWithoutInteractions, 'top');

      expect(sorted).toHaveLength(1);
    });
  });

  describe('sortThreadTree()', () => {
    const parent: Post = {
      id: 'https://github.com/user/repo#commit:parent',
      repositoryUrl: 'https://github.com/user/repo',
      author: 'author',
      authorUrl: 'https://github.com/author',
      timestamp: new Date('2024-01-01').toISOString(),
      content: 'Parent',
      type: 'post',
      likes: 0,
      comments: 0,
      reposts: 0,
      quotes: 0
    };

    const child1: Post = {
      id: 'https://github.com/user/repo#commit:child1',
      repositoryUrl: 'https://github.com/user/repo',
      author: 'author',
      authorUrl: 'https://github.com/author',
      timestamp: new Date('2024-01-02').toISOString(),
      content: 'Child 1',
      type: 'comment',
      originalPostId: parent.id,
      likes: 0,
      comments: 0,
      reposts: 0,
      quotes: 0,
      interactions: { comments: 5, likes: 0, reposts: 0, quotes: 0 }
    };

    const child2: Post = {
      id: 'https://github.com/user/repo#commit:child2',
      repositoryUrl: 'https://github.com/user/repo',
      author: 'author',
      authorUrl: 'https://github.com/author',
      timestamp: new Date('2024-01-03').toISOString(),
      content: 'Child 2',
      type: 'comment',
      originalPostId: parent.id,
      likes: 0,
      comments: 0,
      reposts: 0,
      quotes: 0,
      interactions: { comments: 2, likes: 0, reposts: 0, quotes: 0 }
    };

    const nested: Post = {
      id: 'https://github.com/user/repo#commit:nested',
      repositoryUrl: 'https://github.com/user/repo',
      author: 'author',
      authorUrl: 'https://github.com/author',
      timestamp: new Date('2024-01-04').toISOString(),
      content: 'Nested',
      type: 'comment',
      originalPostId: parent.id,
      parentCommentId: child1.id,
      likes: 0,
      comments: 0,
      reposts: 0,
      quotes: 0
    };

    it('should sort direct children by specified sort', () => {
      const allPosts = [parent, child1, child2];
      const sorted = sortThreadTree(parent.id, allPosts, 'top');

      expect(sorted[0].id).toBe(child1.id);
      expect(sorted[1].id).toBe(child2.id);
    });

    it('should exclude reposts from thread tree', () => {
      const repost: Post = {
        id: 'https://github.com/user/repo#commit:repost',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date('2024-01-03').toISOString(),
        content: '',
        type: 'repost',
        originalPostId: parent.id,
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const allPosts = [parent, child1, repost];
      const sorted = sortThreadTree(parent.id, allPosts, 'latest');

      expect(sorted).toHaveLength(1);
      expect(sorted[0].id).toBe(child1.id);
    });

    it('should sort nested children by oldest', () => {
      const allPosts = [parent, child1, child2, nested];
      const sorted = sortThreadTree(parent.id, allPosts, 'top');

      expect(sorted).toHaveLength(3);
      expect(sorted[1].id).toBe(nested.id);
    });

    it('should avoid infinite loops with seen set', () => {
      const allPosts = [parent, child1, child2];
      const seen = new Set<string>();

      const sorted = sortThreadTree(parent.id, allPosts, 'latest', 1, seen);

      expect(sorted).toHaveLength(2);
      expect(seen.size).toBe(2);
    });

    it('should exclude posts with parentCommentId from top level', () => {
      const allPosts = [parent, child1, child2, nested];
      const sorted = sortThreadTree(parent.id, allPosts, 'latest');

      expect(sorted[0].id).toBe(child2.id);
      expect(sorted[1].id).toBe(child1.id);
      expect(sorted[2].id).toBe(nested.id);
    });

    it('should handle empty children', () => {
      const sorted = sortThreadTree(parent.id, [parent], 'latest');

      expect(sorted).toEqual([]);
    });
  });
});
