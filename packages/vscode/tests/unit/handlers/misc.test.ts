import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockGetAvatar } = vi.hoisted(() => ({
  mockGetAvatar: vi.fn()
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('../../../src/avatar', () => ({
  getAvatar: mockGetAvatar
}));

vi.mock('@gitsocial/core', () => ({
  social: {
    cache: { getCacheStats: vi.fn(), refresh: vi.fn(), setCacheMaxSize: vi.fn() },
    repository: { getStorageStats: vi.fn(), clearCache: vi.fn() }
  }
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import { setMiscCallbacks } from '../../../src/handlers/misc';
import '../../../src/handlers/misc';

describe('misc.ts handlers', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;
  let broadcastSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    broadcastSpy = vi.fn();
    setMiscCallbacks(broadcastSpy);
    mockGetAvatar.mockClear();
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('getAvatar handler', () => {
    it('should get avatar successfully', async () => {
      const handler = getHandler('getAvatar');
      const mockAvatarUrl = 'https://example.com/avatar.jpg';

      mockGetAvatar.mockResolvedValue(mockAvatarUrl);

      await handler(mockPanel, {
        type: 'getAvatar',
        id: 'test-1',
        identifier: 'user@example.com',
        avatarType: 'repository'
      });

      expect(mockGetAvatar).toHaveBeenCalledWith(
        'repo',
        'user@example.com',
        { size: undefined, name: undefined }
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'avatar',
        data: { identifier: 'user@example.com', url: mockAvatarUrl },
        requestId: 'test-1'
      });
    });

    it('should convert repository type to repo', async () => {
      const handler = getHandler('getAvatar');

      mockGetAvatar.mockResolvedValue('https://example.com/avatar.jpg');

      await handler(mockPanel, {
        type: 'getAvatar',
        id: 'test-2',
        identifier: 'user@example.com',
        avatarType: 'repository'
      });

      expect(mockGetAvatar).toHaveBeenCalledWith(
        'repo',
        'user@example.com',
        { size: undefined, name: undefined }
      );
    });

    it('should handle user avatar type', async () => {
      const handler = getHandler('getAvatar');

      mockGetAvatar.mockResolvedValue('https://example.com/avatar.jpg');

      await handler(mockPanel, {
        type: 'getAvatar',
        id: 'test-3',
        identifier: 'user@example.com',
        avatarType: 'user'
      });

      expect(mockGetAvatar).toHaveBeenCalledWith(
        'user',
        'user@example.com',
        { size: undefined, name: undefined }
      );
    });

    it('should handle avatar fetch errors', async () => {
      const handler = getHandler('getAvatar');

      mockGetAvatar.mockRejectedValue(new Error('Failed to fetch avatar'));

      await handler(mockPanel, {
        type: 'getAvatar',
        id: 'test-4',
        identifier: 'user@example.com',
        avatarType: 'user'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to fetch avatar')
          })
        })
      );
    });
  });

  describe('openExternal handler', () => {
    it('should open external URL successfully', async () => {
      const handler = getHandler('openExternal');

      vi.mocked(vscode.env.openExternal).mockResolvedValue(true);

      await handler(mockPanel, {
        type: 'openExternal',
        id: 'test-5',
        url: 'https://github.com'
      });

      expect(vscode.env.openExternal).toHaveBeenCalledWith(
        expect.objectContaining({
          toString: expect.any(Function)
        })
      );
    });

    it('should handle open external errors', async () => {
      const handler = getHandler('openExternal');
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined);

      vi.mocked(vscode.env.openExternal).mockRejectedValue(new Error('Failed to open'));

      await handler(mockPanel, {
        type: 'openExternal',
        id: 'test-6',
        url: 'https://github.com'
      });

      expect(consoleSpy).toHaveBeenCalledWith('Failed to open external URL:', expect.any(Error));
      consoleSpy.mockRestore();
    });
  });

  describe('refresh handler', () => {
    it('should broadcast refresh to all panels', async () => {
      const handler = getHandler('refresh');

      await handler(mockPanel, {
        type: 'refresh',
        id: 'test-7'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('getSettings handler', () => {
    it('should get settings value successfully', async () => {
      const handler = getHandler('getSettings');

      const config = vscode.workspace.getConfiguration('gitsocial');

      vi.mocked(config.get).mockReturnValue('test-value');

      await handler(mockPanel, {
        type: 'getSettings',
        id: 'test-8',
        key: 'cache.maxSize'
      });

      expect(config.get).toHaveBeenCalledWith('cache.maxSize');

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'settings',
        data: { key: 'cache.maxSize', value: 'test-value' },
        requestId: 'test-8'
      });
    });

    it('should handle settings fetch errors', async () => {
      const handler = getHandler('getSettings');

      const config = vscode.workspace.getConfiguration();

      vi.mocked(config.get).mockImplementation(() => {
        throw new Error('Config error');
      });

      await handler(mockPanel, {
        type: 'getSettings',
        id: 'test-9',
        key: 'gitsocial.cache.maxSize'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Config error')
          })
        })
      );
    });
  });

  describe('updateSettings handler', () => {
    it('should update settings successfully', async () => {
      const handler = getHandler('updateSettings');

      const config = vscode.workspace.getConfiguration('gitsocial');

      vi.mocked(config.update).mockResolvedValue(undefined);

      await handler(mockPanel, {
        type: 'updateSettings',
        id: 'test-10',
        key: 'cache.maxSize',
        value: 2000
      });

      expect(config.update).toHaveBeenCalledWith(
        'cache.maxSize',
        2000,
        vscode.ConfigurationTarget.Global
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'settings',
        data: { key: 'cache.maxSize', value: 2000 },
        requestId: 'test-10'
      });
    });

    it('should broadcast settings change to all panels', async () => {
      const handler = getHandler('updateSettings');

      const config = vscode.workspace.getConfiguration('gitsocial');

      vi.mocked(config.update).mockResolvedValue(undefined);

      await handler(mockPanel, {
        type: 'updateSettings',
        id: 'test-11',
        key: 'theme',
        value: 'dark'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({
        type: 'settings',
        data: { key: 'theme', value: 'dark' }
      });
    });

    it('should handle update errors', async () => {
      const handler = getHandler('updateSettings');

      const config = vscode.workspace.getConfiguration();

      vi.mocked(config.update).mockRejectedValue(new Error('Update failed'));

      await handler(mockPanel, {
        type: 'updateSettings',
        id: 'test-12',
        key: 'gitsocial.cache.maxSize',
        value: 2000
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Update failed')
          })
        })
      );
    });
  });
});
