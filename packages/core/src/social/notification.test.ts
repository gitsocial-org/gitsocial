import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { notification } from './notification';
import { createCommit, createTestRepo, type TestRepo } from '../test-utils';
import { execGit } from '../git/exec';
import { post } from './post';
import { follower } from './follower';
import { list } from './list';
import { git } from '../git';
import { cache } from './post/cache';
import { gitMsgList } from '../gitmsg/lists';
import { gitMsgUrl } from '../gitmsg/protocol';
import { initializeGitSocial } from './config';
import type { Post } from './types';

describe('notification', () => {
  let myRepo: TestRepo;
  let otherRepo1: TestRepo;
  let otherRepo2: TestRepo;

  beforeEach(async () => {
    myRepo = await createTestRepo('notification-my');
    otherRepo1 = await createTestRepo('notification-other1');
    otherRepo2 = await createTestRepo('notification-other2');
    await createCommit(myRepo.path, 'initial', { allowEmpty: true });
    await createCommit(otherRepo1.path, 'initial', { allowEmpty: true });
    await createCommit(otherRepo2.path, 'initial', { allowEmpty: true });
    await execGit(myRepo.path, ['remote', 'add', 'origin', `file://${myRepo.path}`]);
    await execGit(otherRepo1.path, ['remote', 'add', 'origin', `file://${otherRepo1.path}`]);
    await execGit(otherRepo2.path, ['remote', 'add', 'origin', `file://${otherRepo2.path}`]);
    await initializeGitSocial(myRepo.path, 'gitsocial');
    await initializeGitSocial(otherRepo1.path, 'gitsocial');
    await initializeGitSocial(otherRepo2.path, 'gitsocial');
  });

  afterEach(() => {
    myRepo.cleanup();
    otherRepo1.cleanup();
    otherRepo2.cleanup();
    vi.restoreAllMocks();
  });

  function createMockPost(overrides: Partial<Post> = {}): Post {
    return {
      id: '#commit:abc123',
      repository: gitMsgUrl.normalize(otherRepo1.path),
      author: { name: 'Other User', email: 'other@example.com' },
      timestamp: new Date('2024-01-15T10:00:00Z'),
      content: 'Test post content',
      type: 'post',
      source: 'explicit',
      isWorkspacePost: false,
      raw: {
        commit: {
          hash: 'abc123',
          author: 'Other User',
          email: 'other@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: 'Test post content'
        }
      },
      cleanContent: 'Test post content',
      interactions: { comments: 0, reposts: 0, quotes: 0 },
      display: {
        repositoryName: 'test-repo',
        commitHash: 'abc123',
        commitUrl: '',
        totalReposts: 0,
        isEmpty: false,
        isUnpushed: false,
        isOrigin: false,
        isWorkspacePost: false
      },
      ...overrides
    };
  }

  describe('getNotifications()', () => {
    it('should return empty array when no notifications', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return comment notification when someone comments on my post', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const commentPost = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].type).toBe('comment');
      expect(result.data?.[0].commitId).toBe(commentPost.id);
    });

    it('should return repost notification when someone reposts my post', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const repostPost = createMockPost({
        id: `${otherRepo1.path}#commit:repost123`,
        type: 'repost',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [repostPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].type).toBe('repost');
    });

    it('should return quote notification when someone quotes my post', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const quotePost = createMockPost({
        id: `${otherRepo1.path}#commit:quote123`,
        type: 'quote',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [quotePost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].type).toBe('quote');
    });

    it('should filter out notifications from my own repository', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const myOwnComment = createMockPost({
        id: `${myRepoUrl}#commit:comment123`,
        type: 'comment',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: myRepoUrl
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [myOwnComment]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return comment on comment notification (parentCommentId)', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const commentOnMyComment = createMockPost({
        id: `${otherRepo1.path}#commit:nested123`,
        type: 'comment',
        originalPostId: `${otherRepo1.path}#commit:original789`,
        parentCommentId: `${myRepoUrl}#commit:mycomment456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentOnMyComment]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
      expect(result.data?.[0].type).toBe('comment');
    });

    it('should handle since option for date filtering', async () => {
      const since = new Date('2024-01-01T00:00:00Z');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path, undefined, { since });

      expect(result.success).toBe(true);
      expect(post.getPosts).toHaveBeenCalledWith(
        myRepo.path,
        'timeline',
        expect.objectContaining({ since })
      );
    });

    it('should handle until option for date filtering', async () => {
      const since = new Date('2024-01-01T00:00:00Z');
      const until = new Date('2024-01-31T23:59:59Z');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path, undefined, { since, until });

      expect(result.success).toBe(true);
      expect(post.getPosts).toHaveBeenCalledWith(
        myRepo.path,
        'timeline',
        expect.objectContaining({ since, until })
      );
    });

    it('should default to last 7 days when no since option provided', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(post.getPosts).toHaveBeenCalledWith(
        myRepo.path,
        'timeline',
        expect.objectContaining({
          since: expect.any(Date) as Date
        })
      );
    });

    it('should handle limit option (default 100)', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const notifications = Array.from({ length: 150 }, (_, i) =>
        createMockPost({
          id: `${otherRepo1.path}#commit:comment${i}`,
          type: 'comment',
          originalPostId: `${myRepoUrl}#commit:mypost456`,
          repository: gitMsgUrl.normalize(otherRepo1.path)
        })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: notifications
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(100);
    });

    it('should handle custom limit option', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const notifications = Array.from({ length: 50 }, (_, i) =>
        createMockPost({
          id: `${otherRepo1.path}#commit:comment${i}`,
          type: 'comment',
          originalPostId: `${myRepoUrl}#commit:mypost456`,
          repository: gitMsgUrl.normalize(otherRepo1.path)
        })
      );

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: notifications
      });

      const result = await notification.getNotifications(myRepo.path, undefined, { limit: 10 });

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(10);
    });

    it('should load additional posts when storageBase provided and cache not covered', async () => {
      const since = new Date('2024-01-01T00:00:00Z');
      const storageBase = '/tmp/storage';

      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(false);
      const loadSpy = vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path, storageBase, { since });

      expect(result.success).toBe(true);
      expect(cache.isCacheRangeCovered).toHaveBeenCalledWith(since);
      expect(loadSpy).toHaveBeenCalledWith(myRepo.path, storageBase, since);
    });

    it('should not load additional posts when cache is covered', async () => {
      const since = new Date('2024-01-01T00:00:00Z');
      const storageBase = '/tmp/storage';

      vi.spyOn(cache, 'isCacheRangeCovered').mockReturnValue(true);
      const loadSpy = vi.spyOn(cache, 'loadAdditionalPosts').mockResolvedValue();
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path, storageBase, { since });

      expect(result.success).toBe(true);
      expect(loadSpy).not.toHaveBeenCalled();
    });

    it('should return error when cannot get origin URL', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: false,
        error: { code: 'NO_REMOTE', message: 'No origin remote found' }
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_MY_REPOSITORY');
      expect(result.error?.message).toContain('Could not find my repository URL');
    });

    it('should return error when origin URL is empty', async () => {
      vi.spyOn(git, 'getOriginUrl').mockResolvedValue({
        success: true,
        data: ''
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NO_MY_REPOSITORY');
    });

    it('should return error when getPosts fails', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch posts' }
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('FETCH_ERROR');
    });

    it('should return error when getPosts returns no data', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: undefined
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('POSTS_FETCH_FAILED');
    });

    it('should handle unexpected errors', async () => {
      vi.spyOn(git, 'getOriginUrl').mockRejectedValue(new Error('Unexpected error'));

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOTIFICATION_ERROR');
      expect(result.error?.message).toBe('Unexpected error');
    });

    it('should handle non-Error exceptions', async () => {
      vi.spyOn(git, 'getOriginUrl').mockRejectedValue('String error');

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(false);
      expect(result.error?.code).toBe('NOTIFICATION_ERROR');
      expect(result.error?.message).toContain('Failed to get notifications');
    });

    it('should combine post and follow notifications', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const commentPost = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentPost]
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', `file://${myRepo.path}`);

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data!.length).toBeGreaterThanOrEqual(1);
      expect(result.data?.some(n => n.type === 'comment')).toBe(true);
    });

    it('should handle comment without originalPostId', async () => {
      const commentPost = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });
      delete commentPost.originalPostId;

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle repost without originalPostId', async () => {
      const repostPost = createMockPost({
        id: `${otherRepo1.path}#commit:repost123`,
        type: 'repost',
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });
      delete repostPost.originalPostId;

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [repostPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle post with no repository in parsed ref', async () => {
      const commentPost = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        originalPostId: '#commit:mypost456',
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });
  });

  describe('getFollowNotifications()', () => {
    it('should return empty array when no followers', async () => {
      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: []
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return follow notification when added to a list', async () => {
      await list.createList(otherRepo1.path, 'friends');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'friends'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: [
          {
            hash: 'addcommit123',
            author: 'Test User',
            email: 'test@example.com',
            timestamp: new Date('2024-01-15T10:00:00Z'),
            content: { repositories: [`file://${myRepo.path}`] }
          },
          {
            hash: 'initialcommit',
            author: 'Test User',
            email: 'test@example.com',
            timestamp: new Date('2024-01-14T10:00:00Z'),
            content: { repositories: [] }
          }
        ]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      const followNotif = result.data?.find(n => n.type === 'follow');
      expect(followNotif).toBeDefined();
      expect(followNotif?.commitId).toContain(otherRepo1.path);
    });

    it('should handle multiple followers', async () => {
      await list.createList(otherRepo1.path, 'following');
      await list.createList(otherRepo2.path, 'friends');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [
          {
            url: `file://${otherRepo1.path}`,
            path: otherRepo1.path,
            branch: 'main',
            followsVia: 'following'
          },
          {
            url: `file://${otherRepo2.path}`,
            path: otherRepo2.path,
            branch: 'main',
            followsVia: 'friends'
          }
        ]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: [
          {
            hash: 'addcommit123',
            author: 'Test User',
            email: 'test@example.com',
            timestamp: new Date('2024-01-15T10:00:00Z'),
            content: { repositories: [`file://${myRepo.path}`] }
          },
          {
            hash: 'initialcommit',
            author: 'Test User',
            email: 'test@example.com',
            timestamp: new Date('2024-01-14T10:00:00Z'),
            content: { repositories: [] }
          }
        ]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      const followNotifs = result.data?.filter(n => n.type === 'follow');
      expect(followNotifs).toBeDefined();
      expect(followNotifs!.length).toBeGreaterThanOrEqual(1);
    });

    it('should skip followers without paths', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: 'https://github.com/user/repo.git',
          branch: 'main',
          followsVia: 'following'
        }]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle error getting followers', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: false,
        error: { code: 'ERROR', message: 'Failed' }
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle follower with no data', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: undefined
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle error getting lists from follower', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(list, 'getLists').mockResolvedValue({
        success: false,
        error: { code: 'ERROR', message: 'Failed' }
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle lists returning no data', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(list, 'getLists').mockResolvedValue({
        success: true,
        data: undefined
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle list not found in follower lists', async () => {
      await list.createList(otherRepo1.path, 'other-list');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'nonexistent-list'
        }]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle error getting list history', async () => {
      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', `file://${myRepo.path}`);

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: false,
        error: { code: 'ERROR', message: 'Failed' }
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      const followNotifs = result.data?.filter(n => n.type === 'follow');
      expect(followNotifs).toEqual([]);
    });

    it('should handle list history returning no data', async () => {
      await list.createList(otherRepo1.path, 'following');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: undefined
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle list history with non-array repositories', async () => {
      await list.createList(otherRepo1.path, 'following');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: [{
          hash: 'commit123',
          author: 'Test',
          email: 'test@example.com',
          timestamp: new Date(),
          content: { repositories: 'not-an-array' }
        }]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle list history with non-string repository items', async () => {
      await list.createList(otherRepo1.path, 'following');

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: [{
          hash: 'commit123',
          author: 'Test',
          email: 'test@example.com',
          timestamp: new Date(),
          content: { repositories: [123, { url: 'test' }, null] }
        }]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should stop searching history after finding when repo was added', async () => {
      await list.createList(otherRepo1.path, 'following');
      await list.addRepositoryToList(otherRepo1.path, 'following', `file://${myRepo.path}`);

      vi.spyOn(follower, 'get').mockResolvedValue({
        success: true,
        data: [{
          url: `file://${otherRepo1.path}`,
          path: otherRepo1.path,
          branch: 'main',
          followsVia: 'following'
        }]
      });

      vi.spyOn(gitMsgList, 'getHistory').mockResolvedValue({
        success: true,
        data: [
          {
            hash: 'commit2',
            author: 'Test',
            email: 'test@example.com',
            timestamp: new Date('2024-01-20'),
            content: { repositories: [`file://${myRepo.path}`, 'https://github.com/other/repo'] }
          },
          {
            hash: 'commit1',
            author: 'Test',
            email: 'test@example.com',
            timestamp: new Date('2024-01-15'),
            content: { repositories: ['https://github.com/other/repo'] }
          }
        ]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      const followNotif = result.data?.find(n => n.type === 'follow');
      expect(followNotif).toBeDefined();
      expect(followNotif?.commitId).toContain('commit2');
    });

    it('should handle unexpected exception in getFollowNotifications', async () => {
      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: []
      });

      vi.spyOn(follower, 'get').mockRejectedValue(new Error('Unexpected follower error'));

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });
  });

  describe('isMyRepository()', () => {
    it('should return true for exact URL match', async () => {
      const postRepo = `file://${myRepo.path}`;

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [createMockPost({
          repository: postRepo,
          id: `${postRepo}#commit:test123`
        })]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
    });

    it('should return true for normalized URL match', async () => {
      const myOwnPost = createMockPost({
        repository: `file://${myRepo.path}.git`,
        id: `file://${myRepo.path}#commit:mypost123`,
        type: 'comment',
        originalPostId: `file://${myRepo.path}#commit:original456`
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [myOwnPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should handle URLs with branch fragments', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const commentPost = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        originalPostId: `${myRepoUrl}#main#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [commentPost]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });

    it('should handle workdir matching', async () => {
      const myOwnPostViaWorkdir = createMockPost({
        repository: myRepo.path,
        id: `${myRepo.path}#commit:mypost123`,
        type: 'comment',
        originalPostId: `file://${otherRepo1.path}#commit:other456`
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [myOwnPostViaWorkdir]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toEqual([]);
    });

    it('should return false for URL mismatch', async () => {
      const myRepoUrl = gitMsgUrl.normalize(`file://${myRepo.path}`);
      const otherComment = createMockPost({
        id: `${otherRepo1.path}#commit:comment123`,
        type: 'comment',
        originalPostId: `${myRepoUrl}#commit:mypost456`,
        repository: gitMsgUrl.normalize(otherRepo1.path)
      });

      vi.spyOn(post, 'getPosts').mockResolvedValue({
        success: true,
        data: [otherComment]
      });

      const result = await notification.getNotifications(myRepo.path);

      expect(result.success).toBe(true);
      expect(result.data).toHaveLength(1);
    });
  });
});
