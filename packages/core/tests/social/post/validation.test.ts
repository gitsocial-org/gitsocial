import { describe, expect, it } from 'vitest';
import { validatePostBatch, validatePostReferences } from '../../../src/social/post/validation';
import type { Post } from '../../../src/social/types';

describe('social/post/validation', () => {
  describe('validatePostReferences()', () => {
    it('should validate a valid post', () => {
      const validPost: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Hello',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(validPost);

      expect(errors).toEqual([]);
    });

    it('should detect invalid post ID format', () => {
      const invalidPost: Post = {
        id: 'invalid-id-format',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Hello',
        type: 'post',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(invalidPost);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('Invalid post ID format');
    });

    it('should require originalPostId for comment type', () => {
      const commentPost: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Comment',
        type: 'comment',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(commentPost);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('comment post missing required originalPostId');
    });

    it('should require originalPostId for repost type', () => {
      const repostPost: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: '',
        type: 'repost',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(repostPost);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('repost post missing required originalPostId');
    });

    it('should require originalPostId for quote type', () => {
      const quotePost: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Quote',
        type: 'quote',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(quotePost);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('quote post missing required originalPostId');
    });

    it('should validate originalPostId format for comment', () => {
      const commentPost: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Comment',
        type: 'comment',
        originalPostId: 'invalid-original-id',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(commentPost);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('Invalid originalPostId format');
    });

    it('should validate parentCommentId format if present', () => {
      const nestedComment: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Nested comment',
        type: 'comment',
        originalPostId: 'https://github.com/user/repo#commit:abcdef123456',
        parentCommentId: 'invalid-parent-id',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(nestedComment);

      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('Invalid parentCommentId format');
    });

    it('should validate valid comment with all references', () => {
      const validComment: Post = {
        id: 'https://github.com/user/repo#commit:123456789abc',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Nested comment',
        type: 'comment',
        originalPostId: 'https://github.com/user/repo#commit:abcdef123456',
        parentCommentId: 'https://github.com/user/repo#commit:fedcba987654',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(validComment);

      expect(errors).toEqual([]);
    });

    it('should accumulate multiple validation errors', () => {
      const invalidPost: Post = {
        id: 'invalid-id',
        repositoryUrl: 'https://github.com/user/repo',
        author: 'author',
        authorUrl: 'https://github.com/author',
        timestamp: new Date().toISOString(),
        content: 'Comment',
        type: 'comment',
        originalPostId: 'invalid-original',
        parentCommentId: 'invalid-parent',
        likes: 0,
        comments: 0,
        reposts: 0,
        quotes: 0
      };

      const errors = validatePostReferences(invalidPost);

      expect(errors).toHaveLength(3);
      expect(errors[0]).toContain('Invalid post ID format');
      expect(errors[1]).toContain('Invalid originalPostId format');
      expect(errors[2]).toContain('Invalid parentCommentId format');
    });
  });

  describe('validatePostBatch()', () => {
    it('should validate batch of valid posts', () => {
      const validPosts: Post[] = [
        {
          id: 'https://github.com/user/repo#commit:123456789abc',
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
        },
        {
          id: 'https://github.com/user/repo#commit:abcdef123456',
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
        }
      ];

      const result = validatePostBatch(validPosts);

      expect(result.isValid).toBe(true);
      expect(result.errors).toEqual([]);
      expect(result.summary).toContain('All 2 posts have valid references');
    });

    it('should detect invalid posts in batch', () => {
      const mixedPosts: Post[] = [
        {
          id: 'https://github.com/user/repo#commit:123456789abc',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Valid post',
          type: 'post',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        },
        {
          id: 'invalid-id',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Invalid post',
          type: 'post',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const result = validatePostBatch(mixedPosts);

      expect(result.isValid).toBe(false);
      expect(result.errors).toHaveLength(1);
      expect(result.errors[0].postId).toBe('invalid-id');
      expect(result.errors[0].postType).toBe('post');
      expect(result.errors[0].errors).toHaveLength(1);
      expect(result.summary).toContain('1/2 posts have validation errors');
      expect(result.summary).toContain('1 total errors');
    });

    it('should aggregate errors from multiple invalid posts', () => {
      const invalidPosts: Post[] = [
        {
          id: 'invalid-id-1',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Invalid 1',
          type: 'post',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        },
        {
          id: 'invalid-id-2',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Invalid 2',
          type: 'comment',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const result = validatePostBatch(invalidPosts);

      expect(result.isValid).toBe(false);
      expect(result.errors).toHaveLength(2);
      expect(result.summary).toContain('2/2 posts have validation errors');
      expect(result.summary).toContain('3 total errors');
    });

    it('should handle empty batch', () => {
      const result = validatePostBatch([]);

      expect(result.isValid).toBe(true);
      expect(result.errors).toEqual([]);
      expect(result.summary).toContain('All 0 posts have valid references');
    });

    it('should provide detailed error information per post', () => {
      const posts: Post[] = [
        {
          id: 'invalid',
          repositoryUrl: 'https://github.com/user/repo',
          author: 'author',
          authorUrl: 'https://github.com/author',
          timestamp: new Date().toISOString(),
          content: 'Comment',
          type: 'comment',
          originalPostId: 'invalid-original',
          likes: 0,
          comments: 0,
          reposts: 0,
          quotes: 0
        }
      ];

      const result = validatePostBatch(posts);

      expect(result.isValid).toBe(false);
      expect(result.errors).toHaveLength(1);
      expect(result.errors[0].postId).toBe('invalid');
      expect(result.errors[0].postType).toBe('comment');
      expect(result.errors[0].errors).toHaveLength(2);
    });
  });
});
