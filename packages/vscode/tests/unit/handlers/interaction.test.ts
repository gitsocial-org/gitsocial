import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockSocial } = vi.hoisted(() => ({
  mockSocial: {
    interaction: {
      createInteraction: vi.fn()
    }
  }
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('@gitsocial/core', () => ({
  social: mockSocial,
  log: vi.fn()
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import { setInteractionCallbacks } from '../../../src/handlers/interaction';
import '../../../src/handlers/interaction';

describe('interaction.ts handlers', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;
  let broadcastSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    broadcastSpy = vi.fn();
    setInteractionCallbacks(broadcastSpy);
    resetAllMocks({ social: mockSocial });
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('social.createInteraction handler', () => {
    it('should create comment interaction successfully', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };
      const mockInteraction = {
        id: 'comment1',
        type: 'comment',
        content: 'Great post!',
        postId: 'post1'
      };

      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: true,
        data: mockInteraction
      });

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-1',
        interactionType: 'comment',
        targetPost: mockTargetPost,
        content: 'Great post!'
      });

      expect(mockSocial.interaction.createInteraction).toHaveBeenCalledWith(
        'comment',
        expect.any(String),
        mockTargetPost,
        'Great post!'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'interactionCreated',
        data: expect.objectContaining({
          message: expect.stringContaining('Comment created'),
          interactionType: 'comment',
          interaction: mockInteraction
        }),
        requestId: 'test-1'
      });
    });

    it('should create repost interaction successfully', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };
      const mockInteraction = {
        id: 'repost1',
        type: 'repost',
        postId: 'post1'
      };

      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: true,
        data: mockInteraction
      });

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-2',
        interactionType: 'repost',
        targetPost: mockTargetPost
      });

      expect(mockSocial.interaction.createInteraction).toHaveBeenCalledWith(
        'repost',
        expect.any(String),
        mockTargetPost,
        undefined
      );
    });

    it('should create quote interaction successfully', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };
      const mockInteraction = {
        id: 'quote1',
        type: 'quote',
        postId: 'post1',
        content: 'I agree with this'
      };

      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: true,
        data: mockInteraction
      });

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-3',
        interactionType: 'quote',
        targetPost: mockTargetPost,
        content: 'I agree with this'
      });

      expect(mockSocial.interaction.createInteraction).toHaveBeenCalledWith(
        'quote',
        expect.any(String),
        mockTargetPost,
        'I agree with this'
      );
    });

    it('should validate content is required for comments', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-4',
        interactionType: 'comment',
        targetPost: mockTargetPost
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Content is required')
          })
        })
      );
    });

    it('should validate content is required for quotes', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-5',
        interactionType: 'quote',
        targetPost: mockTargetPost
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Content is required')
          })
        })
      );
    });

    it('should broadcast new interaction to all panels', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };
      const mockInteraction = {
        id: 'comment1',
        type: 'comment',
        content: 'Great!',
        postId: 'post1'
      };

      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: true,
        data: mockInteraction
      });

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-6',
        interactionType: 'comment',
        targetPost: mockTargetPost,
        content: 'Great!'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({
        type: 'postCreated',
        data: { post: mockInteraction }
      });
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-7',
        interactionType: 'comment',
        targetPost: mockTargetPost,
        content: 'Great!'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle creation errors', async () => {
      const handler = getHandler('social.createInteraction');
      const mockTargetPost = { id: 'post1', content: 'Original post' };

      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: false,
        error: { code: 'CREATE_ERROR', message: 'Failed to create interaction' }
      });

      await handler(mockPanel, {
        type: 'social.createInteraction',
        id: 'test-8',
        interactionType: 'comment',
        targetPost: mockTargetPost,
        content: 'Great!'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to create')
          })
        })
      );
    });
  });
});
