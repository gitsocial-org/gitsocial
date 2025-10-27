import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from './';
import { social } from '@gitsocial/core';
import { clearAvatarCache, getAvatarCacheStats } from '../avatar';

// Message types for cache operations
export type CacheMessages =
  | { type: 'getCacheStats'; id?: string }
  | { type: 'clearCache'; id?: string }
  | { type: 'getAvatarCacheStats'; id?: string }
  | { type: 'clearAvatarCache'; id?: string; options?: { clearMemoryCache?: boolean } }
  | { type: 'setCacheMaxSize'; value: number; id?: string };

// Response types for cache operations
export type CacheResponses =
  | { type: 'cacheStats'; data: unknown; requestId?: string }
  | { type: 'cacheCleared'; data?: unknown; requestId?: string }
  | { type: 'avatarCacheStats'; data: unknown; requestId?: string }
  | { type: 'avatarCacheCleared'; data: unknown; requestId?: string }
  | { type: 'cacheMaxSizeUpdated'; data?: unknown; requestId?: string };

// Register cache statistics handler
registerHandler('getCacheStats', function handleGetCacheStats(panel, message) {
  const requestId = message.id || undefined;

  try {
    const stats = social.cache.getCacheStats();
    postMessage(panel, 'cacheStats', stats, requestId);
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get cache stats',
      context: 'cache'
    }, requestId);
  }
});

// Register clear cache handler
registerHandler('clearCache', async function handleClearCache(panel, message) {
  const requestId = message.id || undefined;

  try {
    // Get workspace folder for refresh
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (workspaceFolder) {
      await social.cache.refresh({ all: true }, workspaceFolder.uri.fsPath);
    } else {
      // If no workspace folder, just clear without reinitializing
      await social.cache.refresh({ all: true });
    }
    postMessage(panel, 'cacheCleared', {}, requestId);
    void vscode.window.showInformationMessage('Cache cleared successfully');
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to clear cache',
      context: 'cache'
    }, requestId);
  }
});

// Register avatar cache statistics handler
registerHandler('getAvatarCacheStats', async function handleGetAvatarCacheStats(panel, message) {
  const requestId = message.id || undefined;

  try {
    const stats = await getAvatarCacheStats();
    postMessage(panel, 'avatarCacheStats', stats, requestId);
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get avatar cache stats',
      context: 'avatarCache'
    }, requestId);
  }
});

// Register clear avatar cache handler
registerHandler('clearAvatarCache', async function handleClearAvatarCache(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'clearAvatarCache') {
      throw new Error('Invalid message type');
    }
    const msg = message;
    const options = msg.options || { clearMemoryCache: true };

    const result = await clearAvatarCache(options);
    postMessage(panel, 'avatarCacheCleared', result, requestId);

    const clearedInfo = [];
    if (result.clearedMemoryEntries > 0) {
      clearedInfo.push(`${result.clearedMemoryEntries} memory entries`);
    }
    if (result.filesDeleted > 0) {
      clearedInfo.push(`${result.filesDeleted} files`);
    }

    const successMessage = clearedInfo.length > 0
      ? `Avatar cache cleared: ${clearedInfo.join(', ')}`
      : 'Avatar cache cleared successfully';

    void vscode.window.showInformationMessage(successMessage);
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to clear avatar cache',
      context: 'avatarCache'
    }, requestId);
  }
});

// Register set cache max size handler
registerHandler('setCacheMaxSize', async function handleSetCacheMaxSize(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'setCacheMaxSize') {
      throw new Error('Invalid message type');
    }
    const msg = message as { type: 'setCacheMaxSize'; value: number; id?: string };
    const newSize = msg.value;

    if (!newSize || newSize < 1000 || newSize > 1000000) {
      throw new Error('Cache size must be between 1,000 and 1,000,000');
    }

    // Update VSCode configuration
    const config = vscode.workspace.getConfiguration('gitsocial');
    await config.update('cacheMaxSize', newSize, vscode.ConfigurationTarget.Global);

    // Update the cache size
    social.cache.setCacheMaxSize(newSize);

    postMessage(panel, 'cacheMaxSizeUpdated', { newSize }, requestId);
    void vscode.window.showInformationMessage(`Cache size updated to ${new Intl.NumberFormat().format(newSize)} posts`);
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to update cache size',
      context: 'cache'
    }, requestId);
  }
});
