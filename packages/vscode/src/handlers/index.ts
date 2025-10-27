import type * as vscode from 'vscode';

// Export all message and response types
export * from './cache';
export * from './post';
export * from './list';
export * from './repository';
export * from './interaction';
export * from './misc';
export * from './log';
export * from './follower';
export * from './notification';
export * from './types';

// Import handlers to trigger registration
import './cache';
import './post';
import './list';
import './repository';
import './interaction';
import './misc';
import './log';
import './follower';
import './notification';

/**
 * Helper function to post messages to webview
 */
export function postMessage(panel: vscode.WebviewPanel, type: string, data?: unknown, requestId?: string): void {
  const message: { type: string; data?: unknown; requestId?: string } = {
    type,
    data
  };

  // Only include requestId if it's actually provided
  if (requestId) {
    message.requestId = requestId;
  }

  void panel.webview.postMessage(message);
}
