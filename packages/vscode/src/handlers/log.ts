import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from '.';
import { log, type LogEntry, social } from '@gitsocial/core';
import { getStorageUri } from '../extension';

// Message types for log operations
export type LogsMessages =
  | {
      type: 'getLogs';
      id?: string;
      options?: {
        since?: string;
        until?: string;
        limit?: number;
        scope?: string;
      };
    };

// Response types for log operations
export type LogsResponses =
  | { type: 'logs'; data: LogEntry[]; requestId?: string };

registerHandler('getLogs', async function handleGetLogs(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'getLogs') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<LogsMessages, { type: 'getLogs' }>;

    log('debug', '[Handlers] handleGetLogs called with options:', msg.options);

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const storageUri = getStorageUri();
    const result = await social.log.getLogs(
      workspaceFolder.uri.fsPath,
      msg.options?.scope || 'repository:my',
      {
        since: msg.options?.since ? new Date(msg.options.since) : undefined,
        until: msg.options?.until ? new Date(msg.options.until) : undefined,
        limit: msg.options?.limit,
        storageBase: storageUri?.fsPath
      }
    );

    if (result.success && result.data) {
      postMessage(panel, 'logs', result.data, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to get logs');
    }
  } catch (error) {
    log('error', '[Handlers] handleGetLogs error:', error);
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get logs'
    }, requestId);
  }
});
