import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockSocial } = vi.hoisted(() => ({
  mockSocial: {
    follower: { get: vi.fn() },
    log: { getLogs: vi.fn() },
    notification: { getNotifications: vi.fn() }
  }
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('@gitsocial/core', () => ({
  social: mockSocial,
  log: vi.fn()
}));

vi.mock('../../../src/extension', () => ({
  getStorageUri: vi.fn(() => ({ fsPath: '/mock/storage' }))
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import '../../../src/handlers/follower';
import '../../../src/handlers/log';
import '../../../src/handlers/notification';

describe('simple handlers (follower, log, notification)', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    resetAllMocks({ social: mockSocial });
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('follower.ts - getFollowers handler', () => {
    it('should get followers successfully', async () => {
      const handler = getHandler('getFollowers');
      const mockFollowers = [
        { id: 'follower1', name: 'User 1' },
        { id: 'follower2', name: 'User 2' }
      ];

      mockSocial.follower.get.mockResolvedValue({
        success: true,
        data: mockFollowers
      });

      await handler(mockPanel, {
        type: 'getFollowers',
        id: 'test-1'
      });

      expect(mockSocial.follower.get).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({})
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'followers',
        data: mockFollowers,
        requestId: 'test-1'
      });
    });

    it('should get followers with limit option', async () => {
      const handler = getHandler('getFollowers');

      mockSocial.follower.get.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'getFollowers',
        id: 'test-2',
        options: { limit: 10 }
      });

      expect(mockSocial.follower.get).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          limit: 10
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getFollowers');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getFollowers',
        id: 'test-3'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle fetch errors', async () => {
      const handler = getHandler('getFollowers');

      mockSocial.follower.get.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch followers' }
      });

      await handler(mockPanel, {
        type: 'getFollowers',
        id: 'test-4'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to fetch')
          })
        })
      );
    });
  });

  describe('log.ts - getLogs handler', () => {
    it('should get logs successfully', async () => {
      const handler = getHandler('getLogs');
      const mockLogs = [
        { id: 'log1', message: 'Post created', timestamp: new Date() },
        { id: 'log2', message: 'Comment added', timestamp: new Date() }
      ];

      mockSocial.log.getLogs.mockResolvedValue({
        success: true,
        data: mockLogs
      });

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-5'
      });

      expect(mockSocial.log.getLogs).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          storageBase: '/mock/storage'
        })
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'logs',
        data: mockLogs,
        requestId: 'test-5'
      });
    });

    it('should get logs with date range filters', async () => {
      const handler = getHandler('getLogs');

      mockSocial.log.getLogs.mockResolvedValue({
        success: true,
        data: []
      });

      const since = '2025-01-01T00:00:00Z';
      const until = '2025-01-31T23:59:59Z';

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-6',
        options: { since, until }
      });

      expect(mockSocial.log.getLogs).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          since: new Date(since),
          until: new Date(until)
        })
      );
    });

    it('should get logs with limit option', async () => {
      const handler = getHandler('getLogs');

      mockSocial.log.getLogs.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-7',
        options: { limit: 50 }
      });

      expect(mockSocial.log.getLogs).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          limit: 50
        })
      );
    });

    it('should get logs with scope filter', async () => {
      const handler = getHandler('getLogs');

      mockSocial.log.getLogs.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-8',
        options: { scope: 'posts' }
      });

      expect(mockSocial.log.getLogs).toHaveBeenCalledWith(
        expect.any(String),
        'posts',
        expect.anything()
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getLogs');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-9'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle fetch errors', async () => {
      const handler = getHandler('getLogs');

      mockSocial.log.getLogs.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch logs' }
      });

      await handler(mockPanel, {
        type: 'getLogs',
        id: 'test-10'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to fetch')
          })
        })
      );
    });
  });

  describe('notification.ts - getNotifications handler', () => {
    it('should get notifications successfully', async () => {
      const handler = getHandler('getNotifications');
      const mockNotifications = [
        { id: 'notif1', type: 'comment', message: 'New comment on your post' },
        { id: 'notif2', type: 'repost', message: 'Someone reposted your post' }
      ];

      mockSocial.notification.getNotifications.mockResolvedValue({
        success: true,
        data: mockNotifications
      });

      await handler(mockPanel, {
        type: 'getNotifications',
        id: 'test-11'
      });

      expect(mockSocial.notification.getNotifications).toHaveBeenCalledWith(
        expect.any(String),
        '/mock/storage',
        undefined
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'notifications',
        data: mockNotifications,
        id: 'test-11'
      });
    });

    it('should get notifications with date range filters', async () => {
      const handler = getHandler('getNotifications');

      mockSocial.notification.getNotifications.mockResolvedValue({
        success: true,
        data: []
      });

      const since = '2025-01-01T00:00:00Z';
      const until = '2025-01-31T23:59:59Z';

      await handler(mockPanel, {
        type: 'getNotifications',
        id: 'test-12',
        options: { since, until }
      });

      expect(mockSocial.notification.getNotifications).toHaveBeenCalledWith(
        expect.any(String),
        '/mock/storage',
        expect.objectContaining({
          since: new Date(since),
          until: new Date(until)
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getNotifications');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getNotifications',
        id: 'test-13'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle fetch errors', async () => {
      const handler = getHandler('getNotifications');

      mockSocial.notification.getNotifications.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch notifications' }
      });

      await handler(mockPanel, {
        type: 'getNotifications',
        id: 'test-14'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          error: expect.stringContaining('Failed to fetch')
        })
      );
    });
  });
});
