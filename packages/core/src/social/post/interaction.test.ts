import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { interaction } from './interaction';
import { post as postNamespace } from '.';
import type { Post } from '../types';
import { createTestRepo, type TestRepo } from '../../test-utils';
import { initializeGitSocial } from '../config';
import { gitMsgRef, gitMsgUrl } from '../../gitmsg/protocol';
import { gitHost } from '../../githost';

describe('social/post/interaction', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('interaction-test');
    await initializeGitSocial(testRepo.path, 'gitsocial');
  });

  afterEach(() => {
    testRepo.cleanup();
    vi.restoreAllMocks();
  });

  function createPost(overrides: Partial<Post>): Post {
    return {
      id: '#commit:abc123',
      repository: gitMsgUrl.normalize(testRepo.path),
      author: { name: 'Test Author', email: 'test@example.com' },
      timestamp: new Date('2024-01-15T10:00:00Z'),
      content: 'Test post content',
      type: 'post',
      source: 'explicit',
      isWorkspacePost: true,
      raw: {
        commit: {
          hash: 'abc123',
          author: 'Test Author',
          email: 'test@example.com',
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
        isOrigin: true,
        isWorkspacePost: true
      },
      ...overrides
    };
  }

  describe('createInteraction()', () => {
    describe('comment creation', () => {
      it('should create a direct comment on a post', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const commentContent = 'This is a comment';

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          content: commentContent,
          type: 'post'
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          commentContent
        );

        expect(result.success).toBe(true);
        expect(result.data).toBeDefined();
        expect(result.data?.type).toBe('comment');
        expect(result.data?.originalPostId).toBe('#commit:target123');
        expect(postNamespace.createPost).toHaveBeenCalled();
      });

      it('should require content for comment', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          undefined
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('MISSING_CONTENT');
        expect(result.error?.message).toContain('Content is required for comment');
      });

      it('should create nested comment on a comment with originalPostId field', async () => {
        const originalPost = createPost({ id: '#commit:original123' });
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:original123'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: [originalPost]
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(result.data?.type).toBe('comment');
        expect(result.data?.originalPostId).toBe('#commit:original123');
      });

      it('should create nested comment finding original from GitMsg fields', async () => {
        const originalPost = createPost({ id: '#commit:original123' });
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          raw: {
            commit: {
              hash: 'parent456',
              author: 'Test',
              email: 'test@example.com',
              timestamp: new Date(),
              message: 'Comment'
            },
            gitMsg: {
              content: 'Comment',
              header: {
                ext: 'social',
                v: '0.1.0',
                extV: '0.1.0',
                fields: {
                  type: 'comment',
                  original: '#commit:original123'
                }
              },
              references: []
            }
          }
        });
        delete parentComment.originalPostId;

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: [originalPost]
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(result.data?.originalPostId).toBe('#commit:original123');
      });

      it('should use target post as original when no original reference exists', async () => {
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment'
        });
        delete parentComment.originalPostId;

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(result.data?.originalPostId).toBe('#commit:parent456');
      });

      it('should fetch actual original post when different from target', async () => {
        const actualOriginalPost = createPost({
          id: '#commit:original123',
          content: 'Original post'
        });
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:original123'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: [actualOriginalPost]
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(postNamespace.getPosts).toHaveBeenCalledWith(
          testRepo.path,
          'post:#commit:original123'
        );
      });

      it('should fallback to target post when original post fetch fails', async () => {
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:missing999'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: []
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(result.data?.originalPostId).toBe('#commit:parent456');
      });

      it('should fallback to target post when original post fetch throws error', async () => {
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:error999'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockRejectedValue(new Error('Fetch error'));

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
        expect(result.data?.originalPostId).toBe('#commit:parent456');
      });
    });

    describe('repost creation', () => {
      it('should create repost without content requirement', async () => {
        const targetPost = createPost({
          id: '#commit:target123',
          content: 'Original post to repost',
          author: { name: 'Original Author', email: 'original@example.com' }
        });

        const mockCreatedPost = createPost({
          id: '#commit:repost456',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'repost',
          testRepo.path,
          targetPost
        );

        expect(result.success).toBe(true);
        expect(result.data?.type).toBe('repost');
        expect(result.data?.display.isEmpty).toBe(true);
        expect(result.data?.originalPostId).toBe('#commit:target123');
      });

      it('should generate subject line for workspace repost', async () => {
        const targetPost = createPost({
          id: '#commit:target123',
          content: 'First line of post\nSecond line',
          author: { name: 'Original Author', email: 'original@example.com' }
        });

        const mockCreatedPost = createPost({
          id: '#commit:repost456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'repost',
          testRepo.path,
          targetPost
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const messageContent = callArgs[1];
        expect(messageContent).toContain('Original Author: First line of post');
      });

      it('should generate subject line for external repository repost', async () => {
        const targetPost = createPost({
          id: 'https://github.com/user/repo#commit:target123',
          repository: 'https://github.com/user/repo',
          content: 'External post',
          author: { name: 'External Author', email: 'external@example.com' },
          isWorkspacePost: false
        });

        const mockCreatedPost = createPost({
          id: '#commit:repost456',
          type: 'post'
        });

        vi.spyOn(gitHost, 'getDisplayName').mockReturnValue('user/repo');

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'repost',
          testRepo.path,
          targetPost
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const messageContent = callArgs[1];
        expect(messageContent).toContain('External Author @ user/repo: External post');
      });
    });

    describe('quote creation', () => {
      it('should create quote with content', async () => {
        const targetPost = createPost({ id: '#commit:target123' });
        const quoteContent = 'My thoughts on this';

        const mockCreatedPost = createPost({
          id: '#commit:quote456',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'quote',
          testRepo.path,
          targetPost,
          quoteContent
        );

        expect(result.success).toBe(true);
        expect(result.data?.type).toBe('quote');
        expect(result.data?.display.isEmpty).toBe(false);
        expect(result.data?.originalPostId).toBe('#commit:target123');
      });

      it('should require content for quote', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        const result = await interaction.createInteraction(
          'quote',
          testRepo.path,
          targetPost,
          undefined
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('MISSING_CONTENT');
        expect(result.error?.message).toContain('Content is required for quote');
      });
    });

    describe('reference handling', () => {
      it('should create relative reference for workspace post', async () => {
        const workspacePost = createPost({
          id: '#commit:target123',
          repository: gitMsgUrl.normalize(testRepo.path),
          isWorkspacePost: true
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          workspacePost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('ref="#commit:target123"');
        expect(message).not.toContain('https://');
      });

      it('should create absolute reference for external post', async () => {
        const externalPost = createPost({
          id: 'https://github.com/external/repo#commit:external123',
          repository: 'https://github.com/external/repo',
          isWorkspacePost: false
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          externalPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('https://github.com/external/repo#commit:external123');
      });

      it('should extract hash from commit reference', async () => {
        const targetPost = createPost({
          id: '#commit:abc123def456',
          repository: gitMsgUrl.normalize(testRepo.path)
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment789',
          type: 'post'
        });

        vi.spyOn(gitMsgRef, 'parse').mockReturnValue({
          type: 'commit',
          value: 'abc123def456',
          repository: null,
          raw: '#commit:abc123def456'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('abc123def456');
      });

      it('should order fields correctly for nested comment (reply-to then original)', async () => {
        const originalPost = createPost({ id: '#commit:original123' });
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          originalPostId: '#commit:original123'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: [originalPost]
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];

        const replyToIndex = message.indexOf('reply-to=');
        const originalIndex = message.indexOf('original=');
        expect(replyToIndex).toBeGreaterThan(-1);
        expect(originalIndex).toBeGreaterThan(-1);
        expect(replyToIndex).toBeLessThan(originalIndex);
      });

      it('should handle nested comment on external parent comment', async () => {
        const originalPost = createPost({
          id: 'https://github.com/user/repo#commit:original123',
          repository: 'https://github.com/user/repo',
          isWorkspacePost: false
        });
        const externalParentComment = createPost({
          id: 'https://github.com/user/repo#commit:parent456',
          repository: 'https://github.com/user/repo',
          type: 'comment',
          originalPostId: 'https://github.com/user/repo#commit:original123',
          isWorkspacePost: false
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(postNamespace, 'getPosts').mockResolvedValue({
          success: true,
          data: [originalPost]
        });

        vi.spyOn(gitMsgRef, 'parse').mockReturnValue({
          type: 'commit',
          value: 'parent456',
          repository: 'https://github.com/user/repo',
          raw: 'https://github.com/user/repo#commit:parent456'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          externalParentComment,
          'Nested comment on external'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('https://github.com/user/repo');
      });

      it('should only include original field for direct comment', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];

        expect(message).toContain('original=');
        expect(message).not.toContain('reply-to=');
      });
    });

    describe('error handling', () => {
      it('should handle createPost failure', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: false,
          error: {
            code: 'POST_ERROR',
            message: 'Failed to create post'
          }
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('CREATE_POST_ERROR');
        expect(result.error?.message).toContain('Failed to create comment');
      });

      it('should handle createPost returning no data', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: undefined
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('CREATE_POST_ERROR');
      });

      it('should catch unexpected errors', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        vi.spyOn(postNamespace, 'createPost').mockRejectedValue(
          new Error('Unexpected error')
        );

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('INTERACTION_ERROR');
        expect(result.error?.message).toBe('Unexpected error');
      });

      it('should handle non-Error exceptions', async () => {
        const targetPost = createPost({ id: '#commit:target123' });

        vi.spyOn(postNamespace, 'createPost').mockRejectedValue('String error');

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(result.success).toBe(false);
        expect(result.error?.code).toBe('INTERACTION_ERROR');
        expect(result.error?.message).toBe('Unknown error');
      });
    });

    describe('content formatting', () => {
      it('should use cleanContent when available for references', async () => {
        const targetPost = createPost({
          id: '#commit:target123',
          content: 'Raw content with GitMsg headers',
          cleanContent: 'Clean content'
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('> Clean content');
      });

      it('should extract clean content when cleanContent is not available', async () => {
        const targetPost = createPost({
          id: '#commit:target123',
          content: 'Content to extract'
        });
        delete targetPost.cleanContent;

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('> Content to extract');
      });

      it('should extract clean content for nested comment parent', async () => {
        const parentComment = createPost({
          id: '#commit:parent456',
          type: 'comment',
          content: 'Parent content',
          originalPostId: '#commit:original123'
        });
        delete parentComment.cleanContent;

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('> Parent content');
      });

      it('should prefix multi-line content correctly', async () => {
        const targetPost = createPost({
          id: '#commit:target123',
          cleanContent: 'Line 1\nLine 2\nLine 3'
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        const createPostSpy = vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(createPostSpy).toHaveBeenCalled();
        const callArgs = createPostSpy.mock.calls[0];
        const message = callArgs[1];
        expect(message).toContain('> Line 1\n> Line 2\n> Line 3');
      });
    });

    describe('edge cases for branch coverage', () => {
      it('should handle non-commit type parsed result for target post', async () => {
        const targetPost = createPost({
          id: '#branch:main',
          repository: gitMsgUrl.normalize(testRepo.path)
        });

        const mockCreatedPost = createPost({
          id: '#commit:comment456',
          type: 'post'
        });

        vi.spyOn(gitMsgRef, 'parse').mockReturnValue({
          type: 'branch',
          value: 'main',
          repository: null,
          raw: '#branch:main'
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          targetPost,
          'Comment'
        );

        expect(result.success).toBe(true);
      });

      it('should handle non-commit type for nested comment parent', async () => {
        const parentComment = createPost({
          id: '#branch:feature',
          type: 'comment',
          originalPostId: '#commit:original123'
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        vi.spyOn(gitMsgRef, 'parse').mockImplementation((id: string) => {
          if (id === '#branch:feature') {
            return {
              type: 'branch',
              value: 'feature',
              repository: null,
              raw: '#branch:feature'
            };
          }
          return {
            type: 'commit',
            value: 'original123',
            repository: null,
            raw: id
          };
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          parentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
      });

      it('should handle parsed repository with null repository field', async () => {
        const externalParentComment = createPost({
          id: 'https://github.com/user/repo#commit:parent456',
          repository: 'https://github.com/user/repo',
          type: 'comment',
          originalPostId: '#commit:original123',
          isWorkspacePost: false
        });

        const mockCreatedPost = createPost({
          id: '#commit:nested789',
          type: 'post'
        });

        let parseCallCount = 0;
        vi.spyOn(gitMsgRef, 'parse').mockImplementation((id: string) => {
          parseCallCount++;
          if (id === externalParentComment.repository && parseCallCount > 5) {
            return {
              type: 'commit',
              value: 'parent456',
              repository: null,
              raw: id
            };
          }
          return {
            type: 'commit',
            value: 'parent456',
            repository: 'https://github.com/user/repo',
            raw: id
          };
        });

        vi.spyOn(postNamespace, 'createPost').mockResolvedValue({
          success: true,
          data: mockCreatedPost
        });

        const result = await interaction.createInteraction(
          'comment',
          testRepo.path,
          externalParentComment,
          'Nested comment'
        );

        expect(result.success).toBe(true);
      });
    });
  });
});
