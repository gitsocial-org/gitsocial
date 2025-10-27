import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from './';
import { getAvatar } from '../avatar';

// Message types for miscellaneous operations
export type MiscMessages =
  | { type: 'getAvatar'; id?: string; identifier: string; avatarType: 'user' | 'repository'; size?: number; name?: string }
  | { type: 'openExternal'; id?: string; url: string }
  | { type: 'refresh'; id?: string }
  | { type: 'getSettings'; id?: string; key: string }
  | { type: 'updateSettings'; id?: string; key: string; value: unknown };

// Response types for miscellaneous operations
export type MiscResponses =
  | { type: 'avatar'; data: { identifier: string; url: string }; requestId?: string }
  | { type: 'refresh'; requestId?: string }
  | { type: 'refreshAfterFetch'; requestId?: string }
  | { type: 'settings'; data: { key: string; value: unknown }; requestId?: string };

// Helper for broadcasting to all panels
let broadcastToAll: ((message: { type: string; [key: string]: unknown }) => void) | undefined;

export function setMiscCallbacks(
  broadcast: (message: { type: string; [key: string]: unknown }) => void
): void {
  broadcastToAll = broadcast;
}

// Register get avatar handler
registerHandler('getAvatar', async function handleAvatarRequest(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'getAvatar') {
      throw new Error('Invalid message type');
    }
    const avatarMessage = message as Extract<MiscMessages, { type: 'getAvatar' }>;

    const dataUri = await getAvatar(
      avatarMessage.avatarType === 'repository' ? 'repo' : avatarMessage.avatarType,
      avatarMessage.identifier,
      { size: avatarMessage.size, name: avatarMessage.name }
    );

    postMessage(panel, 'avatar', {
      identifier: avatarMessage.identifier,
      url: dataUri
    }, requestId);
  } catch (error) {
    postMessage(panel, 'error', {
      message: String(error),
      identifier: message.type === 'getAvatar' && 'identifier' in message ? message.identifier : undefined
    }, requestId);
  }
});

// Register open external handler
registerHandler('openExternal', async function handleOpenExternal(_panel, message) {
  try {
    if (message.type !== 'openExternal') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<MiscMessages, { type: 'openExternal' }>;

    if (typeof msg.url === 'string') {
      await vscode.env.openExternal(vscode.Uri.parse(msg.url));
    }
  } catch (error) {
    console.error('Failed to open external URL:', error);
  }
});

// Register refresh handler
registerHandler('refresh', function handleRefresh(_panel, _message) {
  // Trigger refresh in all panels
  if (broadcastToAll) {
    broadcastToAll({ type: 'refresh' });
  }
});

// Register get settings handler
registerHandler('getSettings', function handleGetSettings(panel, message) {
  try {
    if (message.type !== 'getSettings') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<MiscMessages, { type: 'getSettings' }>;
    const config = vscode.workspace.getConfiguration('gitsocial');
    const value = config.get(msg.key);

    postMessage(panel, 'settings', { key: msg.key, value }, msg.id);
  } catch (error) {
    postMessage(panel, 'error', { message: String(error) }, message.id);
  }
});

// Register update settings handler
registerHandler('updateSettings', async function handleUpdateSettings(panel, message) {
  try {
    if (message.type !== 'updateSettings') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<MiscMessages, { type: 'updateSettings' }>;
    const config = vscode.workspace.getConfiguration('gitsocial');
    await config.update(msg.key, msg.value, vscode.ConfigurationTarget.Global);

    postMessage(panel, 'settings', { key: msg.key, value: msg.value }, msg.id);

    // Broadcast to all panels so sidebar and other views can react
    if (broadcastToAll) {
      broadcastToAll({ type: 'settings', data: { key: msg.key, value: msg.value } });
    }
  } catch (error) {
    postMessage(panel, 'error', { message: String(error) }, message.id);
  }
});
