import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from '.';
import { type Follower, log, social } from '@gitsocial/core';

// Message types for follower operations
export type FollowersMessages =
  | {
      type: 'getFollowers';
      id?: string;
      options?: {
        limit?: number;
      };
    };

// Response types for follower operations
export type FollowersResponses =
  | { type: 'followers'; data: Follower[]; requestId?: string };

registerHandler('getFollowers', async function handleGetFollowers(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'getFollowers') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<FollowersMessages, { type: 'getFollowers' }>;

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const result = await social.follower.get(
      workspaceFolder.uri.fsPath,
      {
        limit: msg.options?.limit
      }
    );

    if (result.success && result.data) {
      postMessage(panel, 'followers', result.data, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to get followers');
    }
  } catch (error) {
    log('error', '[Handlers] handleGetFollowers error:', error);
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get followers'
    }, requestId);
  }
});
