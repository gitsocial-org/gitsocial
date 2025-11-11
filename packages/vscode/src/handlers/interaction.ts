import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from '.';
import { log, type Post, social } from '@gitsocial/core';

// Message types for interaction operations
export type InteractionMessages = {
  type: 'social.createInteraction';
  id?: string;
  interactionType: 'comment' | 'repost' | 'quote';
  targetPost: Post;
  content?: string;
};

// Response types for interaction operations
export type InteractionResponses = {
  type: 'interactionCreated';
  data: {
    message: string;
    interactionType: string;
    interaction: unknown;
  };
  requestId?: string;
};

// Helper for broadcasting to all panels
let broadcastToAll: ((message: { type: string; [key: string]: unknown }) => void) | undefined;

export function setInteractionCallbacks(
  broadcast: (message: { type: string; [key: string]: unknown }) => void
): void {
  broadcastToAll = broadcast;
}

// Register create interaction handler
registerHandler('social.createInteraction', async function handleCreateInteraction(panel, message) {
  const requestId = message.id || undefined;
  log('debug', '[VSCode Handler] Received social.createInteraction message');

  try {
    if (message.type !== 'social.createInteraction') {
      throw new Error('Invalid message type');
    }
    const msg = message as InteractionMessages;
    log('debug', '[VSCode Handler] Message details:', {
      interactionType: msg.interactionType,
      hasTargetPost: !!msg.targetPost,
      hasContent: !!msg.content
    });
    const interactionType = msg.interactionType;

    // Validate required parameters based on interaction type
    if ((interactionType === 'comment' || interactionType === 'quote') && !msg.content) {
      throw new Error('Content is required for comments and quotes');
    }

    if (!msg.targetPost) {
      throw new Error('Target post is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Call the simplified interaction function
    log('debug', '[VSCode Handler] Calling social.interaction.createInteraction with:', {
      type: interactionType,
      repository: workspaceFolder.uri.fsPath,
      targetPostId: msg.targetPost.id
    });

    const result = await social.interaction.createInteraction(
      interactionType,
      workspaceFolder.uri.fsPath,
      msg.targetPost,
      msg.content
    );

    log('debug', '[VSCode Handler] social.interaction.createInteraction result:', {
      success: result?.success,
      error: result?.error
    });

    if (result?.success && result.data) {
      // Use the original post directly - it has the correct branch from createPost
      const enrichedPost = result.data;
      log('debug', '[VSCode Handler] Using created post with correct branch:', {
        id: enrichedPost.id,
        repository: enrichedPost.repository
      });

      // Send success response with the post
      const capitalizedType = interactionType.charAt(0).toUpperCase() + interactionType.slice(1);
      const successMessage = `${capitalizedType} created successfully!`;

      postMessage(panel, 'interactionCreated', {
        message: successMessage,
        interactionType,
        interaction: enrichedPost
      }, requestId);

      // Notify all panels to refresh
      if (broadcastToAll) {
        broadcastToAll({
          type: 'postCreated',
          data: { post: enrichedPost }
        });
      }

    } else {
      throw new Error(result?.error?.message || `Failed to create ${interactionType}`);
    }
  } catch (error) {
    log('error', '[VSCode Handler] Error in handleCreateInteraction:', error);
    // Send error response
    const interactionTypeString = message.type === 'social.createInteraction' && 'interactionType' in message
      ? String(message.interactionType) || 'interaction'
      : 'interaction';
    const errorMessage = error instanceof Error ? error.message : `Failed to create ${interactionTypeString}`;
    log('error', '[VSCode Handler] Sending error to webview:', errorMessage);
    postMessage(panel, 'error', {
      message: errorMessage
    }, requestId);
  }
});
