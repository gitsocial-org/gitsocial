import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { thread } from '../../src/social/thread';
import { cache } from '../../src/social/post/cache';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { initializeGitSocial } from '../../src/social/config';
import type { Post, ThreadContext } from '../../src/social/types';

describe('social/thread', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('thread-test');
    await createCommit(testRepo.path, 'initial', { allowEmpty: true });
    await initializeGitSocial(testRepo.path, 'gitsocial');
  });

  afterEach(() => {
    testRepo.cleanup();
    vi.restoreAllMocks();
  });

  function createMockPost(overrides: Partial<Post> = {}): Post {
    return {
      id: 'https://github.com/user/repo#commit:abc123',
      repositoryUrl: 'https://github.com/user/repo',
      author: 'user',
      authorUrl: 'https://github.com/user',
      timestamp: new Date('2024-01-01T10:00:00Z').toISOString(),
      content: 'Test post',
      type: 'post',
      likes: 0,
      comments: 0,
      reposts: 0,
      quotes: 0,
      ...overrides
    };
  }

  describe('thread.getThread()', () => {
    it('should successfully get thread for existing post', async () => {
      const parentPost = createMockPost({
        id: 'https://github.com/user/repo#commit:parent',
        content: 'Parent post'
      });
      const anchorPost = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        content: 'Anchor post',
        type: 'comment',
        originalPostId: parentPost.id
      });
      const childPost = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        content: 'Child comment',
        type: 'comment',
        originalPostId: anchorPost.id
      });

      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([parentPost, anchorPost, childPost]);

      const result = await thread.getThread(testRepo.path, anchorPost.id);

      expect(result.success).toBe(true);
      expect(result.data?.anchorPost.id).toBe(anchorPost.id);
      expect(result.data?.parentPosts).toContain(parentPost);
      expect(result.data?.childPosts).toContain(childPost);
    });

    it('should use top sort by default', async () => {
      const anchorPost = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor'
      });
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-02').toISOString(),
        comments: 5,
        interactions: { comments: 5, likes: 0, reposts: 0, quotes: 0 }
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-03').toISOString(),
        comments: 2,
        interactions: { comments: 2, likes: 0, reposts: 0, quotes: 0 }
      });

      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([anchorPost, child1, child2]);

      const result = await thread.getThread(testRepo.path, anchorPost.id);

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child1.id);
      expect(result.data?.childPosts[1].id).toBe(child2.id);
    });

    it('should respect sort option (latest)', async () => {
      const anchorPost = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-02').toISOString()
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-03').toISOString()
      });

      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([anchorPost, child1, child2]);

      const result = await thread.getThread(testRepo.path, anchorPost.id, { sort: 'latest' });

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child2.id);
    });

    it('should respect sort option (oldest)', async () => {
      const anchorPost = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-02').toISOString()
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchorPost.id,
        timestamp: new Date('2024-01-03').toISOString()
      });

      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([anchorPost, child1, child2]);

      const result = await thread.getThread(testRepo.path, anchorPost.id, { sort: 'oldest' });

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child1.id);
    });

    it('should return error when no posts in cache', async () => {
      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([]);

      const result = await thread.getThread(testRepo.path, 'some-id');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_POSTS');
      expect(result.error?.message).toBe('No posts found in timeline');
    });

    it('should return error when buildContext fails (post not found)', async () => {
      const somePost = createMockPost();
      vi.spyOn(cache, 'getCachedPosts').mockResolvedValue([somePost]);

      const result = await thread.getThread(testRepo.path, 'nonexistent-post-id');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('POST_NOT_FOUND');
    });

    it('should handle exceptions and return error', async () => {
      vi.spyOn(cache, 'getCachedPosts').mockRejectedValue(new Error('Cache error'));

      const result = await thread.getThread(testRepo.path, 'some-id');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('THREAD_ERROR');
      expect(result.error?.message).toBe('Failed to get thread');
    });
  });

  describe('thread.buildThreadItems()', () => {
    it('should build items with parents, anchor, and children', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent',
        content: 'Parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        content: 'Anchor',
        type: 'comment',
        originalPostId: parent.id
      });
      const child = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        content: 'Child',
        type: 'comment',
        originalPostId: anchor.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [child],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [parent, anchor, child]);

      expect(items).toHaveLength(3);
      expect(items[0].type).toBe('post');
      expect(items[0].data.id).toBe(parent.id);
      expect(items[0].depth).toBeLessThan(0);
      expect(items[1].type).toBe('anchor');
      expect(items[1].data.id).toBe(anchor.id);
      expect(items[1].depth).toBe(0);
      expect(items[2].type).toBe('post');
      expect(items[2].data.id).toBe(child.id);
      expect(items[2].depth).toBeGreaterThan(0);
    });

    it('should skip parents when deferParents is true', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [parent, anchor], { deferParents: true });

      expect(items).toHaveLength(1);
      expect(items[0].type).toBe('anchor');
      expect(items[0].data.id).toBe(anchor.id);
    });

    it('should limit parents with maxParents option', () => {
      const parent1 = createMockPost({ id: 'https://github.com/user/repo#commit:p1' });
      const parent2 = createMockPost({
        id: 'https://github.com/user/repo#commit:p2',
        type: 'comment',
        originalPostId: parent1.id
      });
      const parent3 = createMockPost({
        id: 'https://github.com/user/repo#commit:p3',
        type: 'comment',
        originalPostId: parent2.id
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent3.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent1, parent2, parent3],
        childPosts: [],
        threadRootId: parent1.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [parent1, parent2, parent3, anchor], {
        maxParents: 2
      });

      expect(items.filter(i => i.type === 'post')).toHaveLength(2);
      expect(items[0].data.id).toBe(parent2.id);
      expect(items[1].data.id).toBe(parent3.id);
    });

    it('should limit children with maxChildren option', () => {
      const anchor = createMockPost();
      const children = Array.from({ length: 100 }, (_, i) =>
        createMockPost({
          id: `https://github.com/user/repo#commit:child${i}`,
          type: 'comment',
          originalPostId: anchor.id
        })
      );

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: children,
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor, ...children], { maxChildren: 10 });

      expect(items.filter(i => i.type === 'post')).toHaveLength(10);
    });

    it('should clamp depth with maxDepth option', () => {
      const anchor = createMockPost();
      let currentPost = anchor;
      const allPosts = [anchor];
      for (let i = 0; i < 15; i++) {
        const child = createMockPost({
          id: `https://github.com/user/repo#commit:child${i}`,
          type: 'comment',
          originalPostId: anchor.id,
          parentCommentId: currentPost.id
        });
        allPosts.push(child);
        currentPost = child;
      }

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: allPosts.slice(1),
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, allPosts, { maxDepth: 5 });

      const maxDepth = Math.max(...items.filter(i => i.type === 'post').map(i => i.depth));
      expect(maxDepth).toBeLessThanOrEqual(5);
    });

    it('should calculate negative depth for parent posts', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [parent, anchor]);

      expect(items[0].depth).toBe(-1);
    });

    it('should calculate positive depth for child posts', () => {
      const anchor = createMockPost();
      const child = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        type: 'comment',
        originalPostId: anchor.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [child],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor, child]);

      expect(items[1].depth).toBe(1);
    });

    it('should set hasChildren flag for posts with children', () => {
      const anchor = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchor.id
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchor.id,
        parentCommentId: child1.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [child1, child2],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor, child1, child2]);

      const child1Item = items.find(i => i.data.id === child1.id);
      expect(child1Item?.hasChildren).toBe(true);
    });

    it('should handle empty parents array', () => {
      const anchor = createMockPost();

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor]);

      expect(items).toHaveLength(1);
      expect(items[0].type).toBe('anchor');
    });

    it('should handle empty children array', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [parent, anchor]);

      expect(items).toHaveLength(2);
      expect(items[1].type).toBe('anchor');
    });

    it('should handle context with only anchor post', () => {
      const anchor = createMockPost();

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor]);

      expect(items).toHaveLength(1);
      expect(items[0].type).toBe('anchor');
      expect(items[0].depth).toBe(0);
    });

    it('should use default options when none provided', () => {
      const anchor = createMockPost();
      const children = Array.from({ length: 100 }, (_, i) =>
        createMockPost({
          id: `https://github.com/user/repo#commit:child${i}`,
          type: 'comment',
          originalPostId: anchor.id
        })
      );

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: children,
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const items = thread.buildThreadItems(context, [anchor, ...children]);

      expect(items.filter(i => i.type === 'post')).toHaveLength(50);
    });
  });

  describe('thread.flattenContext()', () => {
    it('should flatten context with all sections', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent.id
      });
      const child = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        type: 'comment',
        originalPostId: anchor.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [child],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const flattened = thread.flattenContext(context);

      expect(flattened).toHaveLength(3);
      expect(flattened[0].id).toBe(parent.id);
      expect(flattened[1].id).toBe(anchor.id);
      expect(flattened[2].id).toBe(child.id);
    });

    it('should handle empty parents', () => {
      const anchor = createMockPost();
      const child = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        type: 'comment',
        originalPostId: anchor.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [child],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const flattened = thread.flattenContext(context);

      expect(flattened).toHaveLength(2);
      expect(flattened[0].id).toBe(anchor.id);
      expect(flattened[1].id).toBe(child.id);
    });

    it('should handle empty children', () => {
      const parent = createMockPost({
        id: 'https://github.com/user/repo#commit:parent'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent],
        childPosts: [],
        threadRootId: parent.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const flattened = thread.flattenContext(context);

      expect(flattened).toHaveLength(2);
      expect(flattened[0].id).toBe(parent.id);
      expect(flattened[1].id).toBe(anchor.id);
    });

    it('should handle context with only anchor', () => {
      const anchor = createMockPost();

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [],
        childPosts: [],
        threadRootId: anchor.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const flattened = thread.flattenContext(context);

      expect(flattened).toHaveLength(1);
      expect(flattened[0].id).toBe(anchor.id);
    });

    it('should maintain order: parents -> anchor -> children', () => {
      const parent1 = createMockPost({ id: 'https://github.com/user/repo#commit:p1' });
      const parent2 = createMockPost({
        id: 'https://github.com/user/repo#commit:p2',
        type: 'comment',
        originalPostId: parent1.id
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: parent2.id
      });
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:c1',
        type: 'comment',
        originalPostId: anchor.id
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:c2',
        type: 'comment',
        originalPostId: anchor.id
      });

      const context: ThreadContext = {
        anchorPost: anchor,
        parentPosts: [parent1, parent2],
        childPosts: [child1, child2],
        threadRootId: parent1.id,
        hasMoreParents: false,
        hasMoreChildren: false
      };

      const flattened = thread.flattenContext(context);

      expect(flattened[0].id).toBe(parent1.id);
      expect(flattened[1].id).toBe(parent2.id);
      expect(flattened[2].id).toBe(anchor.id);
      expect(flattened[3].id).toBe(child1.id);
      expect(flattened[4].id).toBe(child2.id);
    });
  });

  describe('thread.buildContext()', () => {
    it('should return error when anchor post not found', () => {
      const somePost = createMockPost();

      const result = thread.buildContext('nonexistent-id', [somePost], 'top');

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('POST_NOT_FOUND');
      expect(result.error?.message).toContain('Anchor post not found');
    });

    it('should build context for simple post with no parents or children', () => {
      const anchor = createMockPost();

      const result = thread.buildContext(anchor.id, [anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.anchorPost.id).toBe(anchor.id);
      expect(result.data?.parentPosts).toHaveLength(0);
      expect(result.data?.childPosts).toHaveLength(0);
      expect(result.data?.threadRootId).toBe(anchor.id);
    });

    it('should walk originalPostId chain to find thread root', () => {
      const root = createMockPost({
        id: 'https://github.com/user/repo#commit:root'
      });
      const middle = createMockPost({
        id: 'https://github.com/user/repo#commit:middle',
        type: 'comment',
        originalPostId: root.id
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: middle.id
      });

      const result = thread.buildContext(anchor.id, [root, middle, anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.threadRootId).toBe(root.id);
    });

    it('should walk parentCommentId chain', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const comment1 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment1',
        type: 'comment',
        originalPostId: original.id
      });
      const comment2 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment2',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment1.id
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment2.id
      });

      const result = thread.buildContext(anchor.id, [original, comment1, comment2, anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toContain(original);
      expect(result.data?.parentPosts).toContain(comment1);
      expect(result.data?.parentPosts).toContain(comment2);
      expect(result.data?.parentPosts[0].id).toBe(original.id);
      expect(result.data?.parentPosts[1].id).toBe(comment1.id);
      expect(result.data?.parentPosts[2].id).toBe(comment2.id);
    });

    it('should handle missing parent comment (break early)', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: 'https://github.com/user/repo#commit:nonexistent'
      });

      const result = thread.buildContext(anchor.id, [original, anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toHaveLength(1);
      expect(result.data?.parentPosts[0].id).toBe(original.id);
    });

    it('should find original post from topmost parent', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const comment1 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment1',
        type: 'comment',
        originalPostId: original.id
      });
      const comment2 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment2',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment1.id
      });

      const result = thread.buildContext(comment2.id, [original, comment1, comment2], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts[0].id).toBe(original.id);
      expect(result.data?.parentPosts[1].id).toBe(comment1.id);
    });

    it('should find original post from anchor (non-quote)', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: original.id
      });

      const result = thread.buildContext(anchor.id, [original, anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toContain(original);
    });

    it('should skip original for quote-type posts', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const quote = createMockPost({
        id: 'https://github.com/user/repo#commit:quote',
        type: 'quote',
        originalPostId: original.id
      });

      const result = thread.buildContext(quote.id, [original, quote], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).not.toContain(original);
      expect(result.data?.parentPosts).toHaveLength(0);
    });

    it('should handle virtual posts', () => {
      const virtualOriginal = createMockPost({
        id: 'https://github.com/other/repo#commit:original',
        repositoryUrl: 'https://github.com/other/repo',
        isVirtual: true
      });
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: virtualOriginal.id
      });

      const result = thread.buildContext(anchor.id, [virtualOriginal, anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toContain(virtualOriginal);
      expect(result.data?.parentPosts[0].isVirtual).toBe(true);
    });

    it('should collect direct children', () => {
      const anchor = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchor.id
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchor.id
      });

      const result = thread.buildContext(anchor.id, [anchor, child1, child2], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.childPosts).toContain(child1);
      expect(result.data?.childPosts).toContain(child2);
    });

    it('should use sortThreadTree for sorting children (top)', () => {
      const anchor = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-02').toISOString(),
        comments: 10,
        interactions: { comments: 10, likes: 0, reposts: 0, quotes: 0 }
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-03').toISOString(),
        comments: 5,
        interactions: { comments: 5, likes: 0, reposts: 0, quotes: 0 }
      });

      const result = thread.buildContext(anchor.id, [anchor, child1, child2], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child1.id);
      expect(result.data?.childPosts[1].id).toBe(child2.id);
    });

    it('should use sortThreadTree for sorting children (latest)', () => {
      const anchor = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-02').toISOString()
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-03').toISOString()
      });

      const result = thread.buildContext(anchor.id, [anchor, child1, child2], 'latest');

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child2.id);
      expect(result.data?.childPosts[1].id).toBe(child1.id);
    });

    it('should use sortThreadTree for sorting children (oldest)', () => {
      const anchor = createMockPost();
      const child1 = createMockPost({
        id: 'https://github.com/user/repo#commit:child1',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-02').toISOString()
      });
      const child2 = createMockPost({
        id: 'https://github.com/user/repo#commit:child2',
        type: 'comment',
        originalPostId: anchor.id,
        timestamp: new Date('2024-01-03').toISOString()
      });

      const result = thread.buildContext(anchor.id, [anchor, child1, child2], 'oldest');

      expect(result.success).toBe(true);
      expect(result.data?.childPosts[0].id).toBe(child1.id);
      expect(result.data?.childPosts[1].id).toBe(child2.id);
    });

    it('should handle complex thread with both originalPostId and parentCommentId', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const comment1 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment1',
        type: 'comment',
        originalPostId: original.id
      });
      const comment2 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment2',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment1.id
      });
      const child = createMockPost({
        id: 'https://github.com/user/repo#commit:child',
        type: 'comment',
        originalPostId: comment2.id
      });

      const result = thread.buildContext(
        comment2.id,
        [original, comment1, comment2, child],
        'top'
      );

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toContain(original);
      expect(result.data?.parentPosts).toContain(comment1);
      expect(result.data?.childPosts).toContain(child);
    });

    it('should handle multi-level nested comments', () => {
      const original = createMockPost({
        id: 'https://github.com/user/repo#commit:original'
      });
      const comment1 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment1',
        type: 'comment',
        originalPostId: original.id
      });
      const comment2 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment2',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment1.id
      });
      const comment3 = createMockPost({
        id: 'https://github.com/user/repo#commit:comment3',
        type: 'comment',
        originalPostId: original.id,
        parentCommentId: comment2.id
      });

      const result = thread.buildContext(
        comment3.id,
        [original, comment1, comment2, comment3],
        'top'
      );

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toHaveLength(3);
      expect(result.data?.parentPosts[0].id).toBe(original.id);
      expect(result.data?.parentPosts[1].id).toBe(comment1.id);
      expect(result.data?.parentPosts[2].id).toBe(comment2.id);
    });

    it('should handle anchor with originalPostId that is not found (non-quote)', () => {
      const anchor = createMockPost({
        id: 'https://github.com/user/repo#commit:anchor',
        type: 'comment',
        originalPostId: 'https://github.com/user/repo#commit:missing'
      });

      const result = thread.buildContext(anchor.id, [anchor], 'top');

      expect(result.success).toBe(true);
      expect(result.data?.parentPosts).toHaveLength(0);
      expect(result.data?.childPosts).toHaveLength(0);
    });
  });
});
