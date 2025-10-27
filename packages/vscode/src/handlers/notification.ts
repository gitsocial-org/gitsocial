import * as vscode from 'vscode';
import { registerHandler } from './registry';
import type { BaseWebviewMessage } from './types';
import { log, social } from '@gitsocial/core';

export interface GetNotificationsMessage extends BaseWebviewMessage {
  type: 'getNotifications';
  options?: {
    since?: string;
    limit?: number;
  };
  id?: string;
}

export type NotificationsMessages = GetNotificationsMessage;

export interface NotificationsResponse {
  type: 'notifications';
  data: unknown;
  id?: string;
}

export type NotificationsResponses = NotificationsResponse;

registerHandler<GetNotificationsMessage>('getNotifications', async (panel, message) => {
  const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
  if (!workspaceFolder) {
    await panel.webview.postMessage({
      type: 'error',
      error: 'No workspace folder',
      id: message.id
    });
    return;
  }

  try {
    const options = message.options ? {
      ...message.options,
      since: message.options.since ? new Date(message.options.since) : undefined
    } : undefined;

    const result = await social.notification.getNotifications(
      workspaceFolder.uri.fsPath,
      options
    );

    if (result.success && result.data) {
      await panel.webview.postMessage({
        type: 'notifications',
        data: result.data,
        id: message.id
      });
    } else {
      await panel.webview.postMessage({
        type: 'error',
        error: result.error?.message || 'Failed to get notifications',
        id: message.id
      });
    }
  } catch (error) {
    log('error', 'Failed to get notifications:', error);
    await panel.webview.postMessage({
      type: 'error',
      error: error instanceof Error ? error.message : 'Failed to get notifications',
      id: message.id
    });
  }
});
