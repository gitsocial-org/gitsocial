import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockSocial, mockGetAvatarCacheStats, mockClearAvatarCache } = vi.hoisted(() => ({
  mockSocial: {
    cache: {
      getCacheStats: vi.fn(),
      refresh: vi.fn(),
      setCacheMaxSize: vi.fn()
    },
    repository: {
      getStorageStats: vi.fn(),
      clearCache: vi.fn()
    }
  },
  mockGetAvatarCacheStats: vi.fn(),
  mockClearAvatarCache: vi.fn()
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('@gitsocial/core', () => ({
  social: mockSocial,
  log: vi.fn()
}));

vi.mock('../../../src/avatar', () => ({
  getAvatarCacheStats: mockGetAvatarCacheStats,
  clearAvatarCache: mockClearAvatarCache
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import '../../../src/handlers/cache';

describe('cache.ts handlers', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    resetAllMocks({ social: mockSocial });
    mockGetAvatarCacheStats.mockClear();
    mockClearAvatarCache.mockClear();
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('getCacheStats handler', () => {
    it('should get cache stats successfully', async () => {
      const handler = getHandler('getCacheStats');
      const mockStats = {
        size: 100,
        maxSize: 1000,
        hits: 50,
        misses: 10
      };

      mockSocial.cache.getCacheStats.mockReturnValue(mockStats);

      await handler(mockPanel, {
        type: 'getCacheStats',
        id: 'test-1'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'cacheStats',
        data: mockStats,
        requestId: 'test-1'
      });
    });

    it('should handle errors gracefully', async () => {
      const handler = getHandler('getCacheStats');

      mockSocial.cache.getCacheStats.mockImplementation(() => {
        throw new Error('Cache error');
      });

      await handler(mockPanel, {
        type: 'getCacheStats',
        id: 'test-2'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Cache error')
          })
        })
      );
    });
  });

  describe('clearCache handler', () => {
    it('should clear cache successfully', async () => {
      const handler = getHandler('clearCache');

      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'clearCache',
        id: 'test-3'
      });

      expect(mockSocial.cache.refresh).toHaveBeenCalledWith(
        { all: true },
        expect.any(String)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'cacheCleared',
        data: {},
        requestId: 'test-3'
      });
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('clearCache');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'clearCache',
        id: 'test-4'
      });

      expect(mockSocial.cache.refresh).toHaveBeenCalledWith({ all: true });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'cacheCleared',
        data: {},
        requestId: 'test-4'
      });
    });

    it('should handle cache refresh errors', async () => {
      const handler = getHandler('clearCache');

      mockSocial.cache.refresh.mockRejectedValue(new Error('Failed to refresh cache'));

      await handler(mockPanel, {
        type: 'clearCache',
        id: 'test-5'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to refresh cache')
          })
        })
      );
    });
  });

  describe('getAvatarCacheStats handler', () => {
    it('should get avatar cache stats successfully', async () => {
      const handler = getHandler('getAvatarCacheStats');
      const mockStats = {
        diskSize: 1024000,
        fileCount: 50
      };

      mockGetAvatarCacheStats.mockResolvedValue(mockStats);

      await handler(mockPanel, {
        type: 'getAvatarCacheStats',
        id: 'test-6'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'avatarCacheStats',
        data: mockStats,
        requestId: 'test-6'
      });
    });
  });

  describe('clearAvatarCache handler', () => {
    it('should clear avatar cache without clearing memory', async () => {
      const handler = getHandler('clearAvatarCache');

      mockClearAvatarCache.mockResolvedValue(undefined);

      await handler(mockPanel, {
        type: 'clearAvatarCache',
        id: 'test-7',
        options: { clearMemoryCache: false }
      });

      expect(mockClearAvatarCache).toHaveBeenCalledWith({
        clearMemoryCache: false
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'avatarCacheCleared',
        data: undefined,
        requestId: 'test-7'
      });
    });

    it('should clear avatar cache including memory', async () => {
      const handler = getHandler('clearAvatarCache');

      mockClearAvatarCache.mockResolvedValue(undefined);

      await handler(mockPanel, {
        type: 'clearAvatarCache',
        id: 'test-8',
        options: { clearMemoryCache: true }
      });

      expect(mockClearAvatarCache).toHaveBeenCalledWith({
        clearMemoryCache: true
      });
    });
  });

  describe('setCacheMaxSize handler', () => {
    it('should update cache max size successfully', async () => {
      const handler = getHandler('setCacheMaxSize');

      const config = vscode.workspace.getConfiguration();

      await handler(mockPanel, {
        type: 'setCacheMaxSize',
        id: 'test-9',
        value: 2000
      });

      expect(config.update).toHaveBeenCalledWith(
        'cacheMaxSize',
        2000,
        1
      );
      expect(mockSocial.cache.setCacheMaxSize).toHaveBeenCalledWith(2000);

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'cacheMaxSizeUpdated',
        data: { newSize: 2000 },
        requestId: 'test-9'
      });
    });

    it('should handle configuration update errors', async () => {
      const handler = getHandler('setCacheMaxSize');

      const config = vscode.workspace.getConfiguration();

      vi.mocked(config.update).mockRejectedValue(new Error('Config update failed'));

      await handler(mockPanel, {
        type: 'setCacheMaxSize',
        id: 'test-10',
        value: 2000
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Config update failed')
          })
        })
      );
    });
  });

  describe('getRepositoryStorageStats handler', () => {
    it('should get repository storage stats successfully', async () => {
      const handler = getHandler('getRepositoryStorageStats');
      const mockStats = {
        totalSize: 50000000,
        repositoryCount: 10
      };

      mockSocial.repository.getStorageStats.mockResolvedValue(mockStats);

      await handler(mockPanel, {
        type: 'getRepositoryStorageStats',
        id: 'test-11'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'repositoryStorageStats',
        data: mockStats,
        requestId: 'test-11'
      });
    });
  });

  describe('clearRepositoryCache handler', () => {
    it('should clear repository cache successfully', async () => {
      const handler = getHandler('clearRepositoryCache');

      mockSocial.repository.clearCache.mockReturnValue({
        deletedCount: 5,
        diskSpaceFreed: 5000000
      });

      await handler(mockPanel, {
        type: 'clearRepositoryCache',
        id: 'test-12'
      });

      expect(mockSocial.repository.clearCache).toHaveBeenCalled();

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'repositoryCacheCleared',
        data: {
          deletedCount: 5,
          diskSpaceFreed: 5000000
        },
        requestId: 'test-12'
      });
    });

    it('should handle cleanup errors', async () => {
      const handler = getHandler('clearRepositoryCache');

      mockSocial.repository.clearCache.mockImplementation(() => {
        throw new Error('Cleanup failed');
      });

      await handler(mockPanel, {
        type: 'clearRepositoryCache',
        id: 'test-13'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Cleanup failed')
          })
        })
      );
    });
  });
});
