import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { updateInteractionCounts } from '../../../src/social/post/cache-interactions';
import { postIndex, postsCache } from '../../../src/social/post/cache';
import type { Post } from '../../../src/social/types';
import { createTestRepo, type TestRepo } from '../../test-utils';
import { addRemote } from '../../../src/git/remotes';

describe('social/post/cache-interactions', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('cache-interactions-test');
    postsCache.clear();
    postIndex.absolute.clear();
    postIndex.merged.clear();
  });

  afterEach(() => {
    testRepo.cleanup();
    vi.restoreAllMocks();
  });

  function createPost(overrides: Partial<Post>): Post {
    return {
      id: '#commit:abc123',
      repository: '',
      author: { name: 'Test', email: 'test@example.com' },
      timestamp: new Date('2024-01-15T10:00:00Z'),
      content: 'Test post',
      type: 'post',
      source: 'explicit',
      isWorkspacePost: true,
      raw: { commit: { hash: 'abc123', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
      cleanContent: 'Test post',
      interactions: { comments: 0, reposts: 0, quotes: 0 },
      display: {
        repositoryName: '',
        commitHash: 'abc123',
        commitUrl: '',
        totalReposts: 0,
        isEmpty: false,
        isUnpushed: false,
        isOrigin: true,
        isWorkspacePost: true
      },
      ...overrides
    };
  }

  describe('updateInteractionCounts()', () => {
    describe('basic interaction counting', () => {
      it('should count comments on a post', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: '#commit:target123'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(1);
        expect(targetPost.interactions.reposts).toBe(0);
        expect(targetPost.interactions.quotes).toBe(0);
        expect(targetPost.display.totalReposts).toBe(0);
      });

      it('should count reposts on a post', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const repostPost = createPost({
          id: '#commit:repost456',
          type: 'repost',
          originalPostId: '#commit:target123'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:repost456', repostPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(0);
        expect(targetPost.interactions.reposts).toBe(1);
        expect(targetPost.interactions.quotes).toBe(0);
        expect(targetPost.display.totalReposts).toBe(1);
      });

      it('should count quotes on a post', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const quotePost = createPost({
          id: '#commit:quote456',
          type: 'quote',
          originalPostId: '#commit:target123'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:quote456', quotePost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(0);
        expect(targetPost.interactions.reposts).toBe(0);
        expect(targetPost.interactions.quotes).toBe(1);
        expect(targetPost.display.totalReposts).toBe(1);
      });

      it('should count mixed interactions on same post', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const comment1 = createPost({ id: '#commit:comment1', type: 'comment', originalPostId: '#commit:target123' });
        const comment2 = createPost({ id: '#commit:comment2', type: 'comment', originalPostId: '#commit:target123' });
        const repost1 = createPost({ id: '#commit:repost1', type: 'repost', originalPostId: '#commit:target123' });
        const quote1 = createPost({ id: '#commit:quote1', type: 'quote', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment1', comment1],
          ['#commit:comment2', comment2],
          ['#commit:repost1', repost1],
          ['#commit:quote1', quote1]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(2);
        expect(targetPost.interactions.reposts).toBe(1);
        expect(targetPost.interactions.quotes).toBe(1);
        expect(targetPost.display.totalReposts).toBe(2);
      });

      it('should handle posts with no interactions', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        const newPosts = new Map<string, Post>([['#commit:target123', targetPost]]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(0);
        expect(targetPost.interactions.reposts).toBe(0);
        expect(targetPost.interactions.quotes).toBe(0);
        expect(targetPost.display.totalReposts).toBe(0);
      });
    });

    describe('deduplication logic', () => {
      it('should skip duplicate interactions with same canonical source and target', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/myuser/myrepo.git');

        const targetPost = createPost({ id: '#commit:target123' });
        const comment1 = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: '#commit:target123'
        });
        const comment2 = createPost({
          id: 'https://github.com/myuser/myrepo#commit:comment456',
          repository: 'https://github.com/myuser/myrepo',
          type: 'comment',
          originalPostId: 'https://github.com/myuser/myrepo#commit:target123',
          isWorkspacePost: false
        });

        postIndex.absolute.set('https://github.com/myuser/myrepo#commit:comment456', '#commit:comment456');
        postIndex.absolute.set('https://github.com/myuser/myrepo#commit:target123', '#commit:target123');

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', comment1],
          ['https://github.com/myuser/myrepo#commit:comment456', comment2]
        ]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should allow different sources to interact with same target', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const comment1 = createPost({ id: '#commit:comment1', type: 'comment', originalPostId: '#commit:target123' });
        const comment2 = createPost({ id: '#commit:comment2', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment1', comment1],
          ['#commit:comment2', comment2]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(2);
      });
    });

    describe('ID resolution and mapping', () => {
      it('should find target via direct ID match', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({ id: '#commit:comment456', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should find target via absolute mapping', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({
          id: 'https://github.com/external/repo#commit:comment456',
          repository: 'https://github.com/external/repo',
          type: 'comment',
          originalPostId: 'https://github.com/user/repo#commit:target123',
          isWorkspacePost: false
        });

        postIndex.absolute.set('https://github.com/user/repo#commit:target123', '#commit:target123');

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['https://github.com/external/repo#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should find target via fallback hash lookup when target has workspace origin URL', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/myuser/myrepo.git');

        const targetPost = createPost({ id: '#commit:target789xyz' });
        const commentPost = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: 'https://github.com/myuser/myrepo#commit:target789xyz'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target789xyz', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should handle missing target post gracefully', async () => {
        const commentPost = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: '#commit:missing999'
        });

        const newPosts = new Map<string, Post>([['#commit:comment456', commentPost]]);

        await updateInteractionCounts(newPosts);

        expect(newPosts.size).toBe(1);
      });
    });

    describe('origin URL handling', () => {
      it('should process with valid origin URL', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');

        const targetPost = createPost({ id: '#commit:target123' });
        const newPosts = new Map<string, Post>([['#commit:target123', targetPost]]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions).toBeDefined();
      });

      it('should process without origin URL', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const newPosts = new Map<string, Post>([['#commit:target123', targetPost]]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions).toBeDefined();
      });

      it('should handle comments when origin URL exists', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');

        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: '#commit:target123'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions.comments).toBe(1);
      });
    });

    describe('parent comment tracking', () => {
      it('should count nested comments toward parent comment', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:target123'
        });
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: '#commit:parent456'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:parent456', parentComment],
          ['#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(2);
        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should find parent via direct lookup', async () => {
        const parentComment = createPost({ id: '#commit:parent456', type: 'comment', originalPostId: '#commit:target123' });
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: '#commit:parent456'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:parent456', parentComment],
          ['#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts);

        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should find parent via absolute mapping', async () => {
        const parentComment = createPost({ id: '#commit:parent456', type: 'comment', originalPostId: '#commit:target123' });
        const nestedComment = createPost({
          id: 'https://github.com/external/repo#commit:nested789',
          repository: 'https://github.com/external/repo',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: 'https://github.com/user/repo#commit:parent456',
          isWorkspacePost: false
        });

        postIndex.absolute.set('https://github.com/user/repo#commit:parent456', '#commit:parent456');

        const newPosts = new Map<string, Post>([
          ['#commit:parent456', parentComment],
          ['https://github.com/external/repo#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts);

        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should find parent via fallback hash lookup when parent has workspace origin URL', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/myuser/myrepo.git');

        const parentComment = createPost({
          id: '#commit:abc123def456',
          type: 'comment',
          originalPostId: '#commit:target123'
        });
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: 'https://github.com/myuser/myrepo#commit:abc123def456'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:abc123def456', parentComment],
          ['#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should skip duplicate parent interactions', async () => {
        const parentComment = createPost({ id: '#commit:parent456', type: 'comment', originalPostId: '#commit:target123' });
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: '#commit:parent456'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:parent456', parentComment],
          ['#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts);

        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should handle missing parent comment gracefully', async () => {
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: '#commit:missing999'
        });

        const newPosts = new Map<string, Post>([['#commit:nested789', nestedComment]]);

        await updateInteractionCounts(newPosts);

        expect(newPosts.size).toBe(1);
      });
    });

    describe('incremental cache updates', () => {
      it('should update counts with no existing cache', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({ id: '#commit:comment456', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should incrementally update with existing cached posts', async () => {
        const cachedTargetPost = createPost({ id: '#commit:target123' });
        const cachedComment = createPost({ id: '#commit:comment1', type: 'comment', originalPostId: '#commit:target123' });

        const frozenTarget: Readonly<Post> = Object.freeze({ ...cachedTargetPost });
        const frozenComment: Readonly<Post> = Object.freeze({ ...cachedComment });

        postsCache.set('#commit:target123', frozenTarget);
        postsCache.set('#commit:comment1', frozenComment);

        const newComment = createPost({ id: '#commit:comment2', type: 'comment', originalPostId: '#commit:target123' });
        const newPosts = new Map<string, Post>([['#commit:comment2', newComment]]);

        await updateInteractionCounts(newPosts);

        const updatedTarget = postsCache.get('#commit:target123');
        expect(updatedTarget?.interactions.comments).toBe(2);
      });

      it('should recalculate all counts with complete dataset', async () => {
        const cachedPost = createPost({ id: '#commit:target123', interactions: { comments: 5, reposts: 3, quotes: 2 } });
        postsCache.set('#commit:target123', Object.freeze({ ...cachedPost }));

        const comment1 = createPost({ id: '#commit:comment1', type: 'comment', originalPostId: '#commit:target123' });
        const comment2 = createPost({ id: '#commit:comment2', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:comment1', comment1],
          ['#commit:comment2', comment2]
        ]);

        await updateInteractionCounts(newPosts);

        const updatedPost = postsCache.get('#commit:target123');
        expect(updatedPost?.interactions.comments).toBe(2);
        expect(updatedPost?.interactions.reposts).toBe(0);
        expect(updatedPost?.interactions.quotes).toBe(0);
      });

      it('should freeze existing posts after update', async () => {
        const cachedPost = createPost({ id: '#commit:target123' });
        postsCache.set('#commit:target123', Object.freeze({ ...cachedPost }));

        const newComment = createPost({ id: '#commit:comment1', type: 'comment', originalPostId: '#commit:target123' });
        const newPosts = new Map<string, Post>([['#commit:comment1', newComment]]);

        await updateInteractionCounts(newPosts);

        const updatedPost = postsCache.get('#commit:target123');
        expect(Object.isFrozen(updatedPost)).toBe(true);
      });

      it('should update newPosts Map with calculated counts', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({ id: '#commit:comment456', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        const updatedTarget = newPosts.get('#commit:target123');
        expect(updatedTarget?.interactions.comments).toBe(1);
      });
    });

    describe('edge cases', () => {
      it('should handle posts with both originalPostId and parentCommentId', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const parentComment = createPost({ id: '#commit:parent456', type: 'comment', originalPostId: '#commit:target123' });
        const nestedComment = createPost({
          id: '#commit:nested789',
          type: 'comment',
          originalPostId: '#commit:target123',
          parentCommentId: '#commit:parent456'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:parent456', parentComment],
          ['#commit:nested789', nestedComment]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(2);
        expect(parentComment.interactions.comments).toBe(1);
      });

      it('should handle workspace posts with absolute IDs in references', async () => {
        await addRemote(testRepo.path, 'origin', 'https://github.com/user/repo.git');

        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({
          id: '#commit:comment456',
          type: 'comment',
          originalPostId: 'https://github.com/user/repo#commit:target123'
        });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts, testRepo.path);

        expect(targetPost.interactions.comments).toBe(1);
      });

      it('should handle external posts with relative IDs in references', async () => {
        const targetPost = createPost({
          id: 'https://github.com/user/repo#commit:target123',
          repository: 'https://github.com/user/repo',
          isWorkspacePost: false
        });
        const commentPost = createPost({
          id: 'https://github.com/user/repo#commit:comment456',
          repository: 'https://github.com/user/repo',
          type: 'comment',
          originalPostId: '#commit:target123',
          isWorkspacePost: false
        });

        const newPosts = new Map<string, Post>([
          ['https://github.com/user/repo#commit:target123', targetPost],
          ['https://github.com/user/repo#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(newPosts.size).toBe(2);
      });

      it('should initialize interactions and totalReposts for posts without them', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        delete (targetPost as { interactions?: unknown }).interactions;
        delete targetPost.display.totalReposts;

        const newPosts = new Map<string, Post>([['#commit:target123', targetPost]]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions).toEqual({ comments: 0, reposts: 0, quotes: 0 });
        expect(targetPost.display.totalReposts).toBe(0);
      });

      it('should handle non-comment/repost/quote types gracefully', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const regularPost = createPost({ id: '#commit:regular456', type: 'post' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:regular456', regularPost]
        ]);

        await updateInteractionCounts(newPosts);

        expect(targetPost.interactions.comments).toBe(0);
        expect(targetPost.interactions.reposts).toBe(0);
        expect(targetPost.interactions.quotes).toBe(0);
      });

      it('should handle undefined workdir parameter', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentPost = createPost({ id: '#commit:comment456', type: 'comment', originalPostId: '#commit:target123' });

        const newPosts = new Map<string, Post>([
          ['#commit:target123', targetPost],
          ['#commit:comment456', commentPost]
        ]);

        await updateInteractionCounts(newPosts, undefined);

        expect(targetPost.interactions.comments).toBe(1);
      });
    });
  });
});
