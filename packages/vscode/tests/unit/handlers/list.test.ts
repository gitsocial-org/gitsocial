import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockSocial } = vi.hoisted(() => ({
  mockSocial: {
    list: {
      getLists: vi.fn(),
      createList: vi.fn(),
      updateList: vi.fn(),
      deleteList: vi.fn(),
      followList: vi.fn(),
      syncFollowedList: vi.fn(),
      unfollowList: vi.fn()
    }
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
import { setListCallbacks } from '../../../src/handlers/list';
import '../../../src/handlers/list';

describe('list.ts handlers', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;
  let broadcastSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    broadcastSpy = vi.fn();
    setListCallbacks(broadcastSpy);
    resetAllMocks({ social: mockSocial });
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('list.getAll handler', () => {
    it('should get all lists for workspace', async () => {
      const handler = getHandler('list.getAll');
      const mockLists = [
        { id: 'list1', name: 'Reading' },
        { id: 'list2', name: 'Favorites' }
      ];

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: mockLists
      });

      await handler(mockPanel, {
        type: 'list.getAll',
        id: 'test-1'
      });

      expect(mockSocial.list.getLists).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'lists',
        data: mockLists,
        requestId: 'test-1'
      });
    });

    it('should get lists from remote repository', async () => {
      const handler = getHandler('list.getAll');
      const mockLists = [{ id: 'list1', name: 'Remote List' }];

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: mockLists
      });

      await handler(mockPanel, {
        type: 'list.getAll',
        id: 'test-2',
        repository: 'https://github.com/user/repo'
      });

      expect(mockSocial.list.getLists).toHaveBeenCalledWith(
        'https://github.com/user/repo',
        expect.any(String)
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.getAll');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.getAll',
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
      const handler = getHandler('list.getAll');

      mockSocial.list.getLists.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch lists' }
      });

      await handler(mockPanel, {
        type: 'list.getAll',
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

  describe('list.create handler', () => {
    it('should create list successfully', async () => {
      const handler = getHandler('list.create');

      mockSocial.list.createList.mockResolvedValue({
        success: true,
        data: { id: 'new-list', name: 'New List' }
      });

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-5',
        listId: 'new-list',
        name: 'New List'
      });

      expect(mockSocial.list.createList).toHaveBeenCalledWith(
        expect.any(String),
        'new-list',
        'New List'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listCreated',
          requestId: 'test-5'
        })
      );
    });

    it('should broadcast refresh after creating list', async () => {
      const handler = getHandler('list.create');

      mockSocial.list.createList.mockResolvedValue({
        success: true,
        data: { id: 'new-list', name: 'New List' }
      });

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-6',
        listId: 'new-list',
        name: 'New List'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });

    it('should handle create errors', async () => {
      const handler = getHandler('list.create');

      mockSocial.list.createList.mockResolvedValue({
        success: false,
        error: { code: 'CREATE_ERROR', message: 'List already exists' }
      });

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-7',
        listId: 'existing-list',
        name: 'Existing List'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List already exists')
          })
        })
      );
    });
  });

  describe('list.rename handler', () => {
    it('should rename list successfully', async () => {
      const handler = getHandler('list.rename');

      mockSocial.list.updateList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-8',
        listId: 'my-list',
        newName: 'Updated Name'
      });

      expect(mockSocial.list.updateList).toHaveBeenCalledWith(
        expect.any(String),
        'my-list',
        { name: 'Updated Name' }
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listRenamed',
          requestId: 'test-8'
        })
      );
    });

    it('should broadcast refresh after renaming', async () => {
      const handler = getHandler('list.rename');

      mockSocial.list.updateList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-9',
        listId: 'my-list',
        newName: 'Updated Name'
      });

      expect(broadcastSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'refresh',
          operation: 'listRenamed'
        })
      );
    });
  });

  describe('list.delete handler', () => {
    it('should delete list after user confirmation', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('Yes' as any);
      mockSocial.list.deleteList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-10',
        listId: 'old-list'
      });

      expect(vscode.window.showWarningMessage).toHaveBeenCalledWith(
        expect.stringContaining('Are you sure'),
        { modal: true },
        'Yes',
        'No'
      );
      expect(mockSocial.list.deleteList).toHaveBeenCalledWith(
        expect.any(String),
        'old-list'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listDeleted',
          requestId: 'test-10'
        })
      );
    });

    it('should not delete list if user cancels', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('No' as any);

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-11',
        listId: 'safe-list'
      });

      expect(mockSocial.list.deleteList).not.toHaveBeenCalled();
      expect(broadcastSpy).not.toHaveBeenCalled();
    });

    it('should broadcast refresh after deleting', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('Yes' as any);
      mockSocial.list.deleteList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-12',
        listId: 'old-list'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('list.follow handler', () => {
    it('should follow list successfully', async () => {
      const handler = getHandler('list.follow');

      mockSocial.list.followList.mockResolvedValue({
        success: true,
        data: { listId: 'reading' }
      });

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-13',
        sourceListId: 'reading',
        sourceRepository: 'https://github.com/user/repo'
      });

      expect(mockSocial.list.followList).toHaveBeenCalledWith(
        expect.any(String),
        'https://github.com/user/repo',
        'reading',
        undefined,
        expect.any(String)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listFollowed',
          requestId: 'test-13'
        })
      );
    });

    it('should broadcast refresh after following', async () => {
      const handler = getHandler('list.follow');

      mockSocial.list.followList.mockResolvedValue({
        success: true,
        data: { listId: 'reading' }
      });

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-14',
        sourceListId: 'reading',
        sourceRepository: 'https://github.com/user/repo'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('list.sync handler', () => {
    it('should sync followed list successfully', async () => {
      const handler = getHandler('list.sync');

      mockSocial.list.syncFollowedList.mockResolvedValue({
        success: true,
        data: { added: 2, removed: 1 }
      });

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-15',
        listId: 'reading'
      });

      expect(mockSocial.list.syncFollowedList).toHaveBeenCalledWith(
        expect.any(String),
        'reading',
        expect.any(String)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listSynced',
          requestId: 'test-15'
        })
      );
    });

    it('should broadcast refresh after syncing', async () => {
      const handler = getHandler('list.sync');

      mockSocial.list.syncFollowedList.mockResolvedValue({
        success: true,
        data: { added: 0, removed: 0 }
      });

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-16',
        listId: 'reading'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('list.unfollow handler', () => {
    it('should unfollow list after confirmation', async () => {
      const handler = getHandler('list.unfollow');

      vi.mocked(vscode.window.showInformationMessage).mockResolvedValue('Unfollow' as any);
      mockSocial.list.unfollowList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-17',
        listId: 'reading'
      });

      expect(vscode.window.showInformationMessage).toHaveBeenCalledWith(
        expect.stringContaining('Unfollow this list'),
        { modal: true },
        'Unfollow',
        'Cancel'
      );
      expect(mockSocial.list.unfollowList).toHaveBeenCalledWith(
        expect.any(String),
        'reading'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listUnfollowed',
          requestId: 'test-17'
        })
      );
    });

    it('should not unfollow if user cancels', async () => {
      const handler = getHandler('list.unfollow');

      vi.mocked(vscode.window.showInformationMessage).mockResolvedValue('Cancel' as any);

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-18',
        listId: 'reading'
      });

      expect(mockSocial.list.unfollowList).not.toHaveBeenCalled();
      expect(broadcastSpy).not.toHaveBeenCalled();
    });

    it('should broadcast refresh after unfollowing', async () => {
      const handler = getHandler('list.unfollow');

      vi.mocked(vscode.window.showInformationMessage).mockResolvedValue('Unfollow' as any);
      mockSocial.list.unfollowList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-19',
        listId: 'reading'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });

    it('should handle unfollow errors', async () => {
      const handler = getHandler('list.unfollow');

      vi.mocked(vscode.window.showInformationMessage).mockResolvedValue('Unfollow' as any);
      mockSocial.list.unfollowList.mockResolvedValue({
        success: false,
        error: { code: 'UNFOLLOW_ERROR', message: 'Failed to unfollow list' }
      });

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-20',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to unfollow')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.unfollow');

      vi.mocked(vscode.window.showInformationMessage).mockResolvedValue('Unfollow' as any);
      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-21',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle missing listId', async () => {
      const handler = getHandler('list.unfollow');

      await handler(mockPanel, {
        type: 'list.unfollow',
        id: 'test-22',
        listId: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });
  });

  describe('list.create handler - validation', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('list.create');

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-val-1',
        listId: '',
        name: 'Test List'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List ID is required')
          })
        })
      );
    });

    it('should handle missing name', async () => {
      const handler = getHandler('list.create');

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-val-2',
        listId: 'test-list',
        name: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List name is required')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.create');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.create',
        id: 'test-val-3',
        listId: 'test-list',
        name: 'Test List'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('list.rename handler - validation', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('list.rename');

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-rename-1',
        listId: '',
        newName: 'New Name'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List ID is required')
          })
        })
      );
    });

    it('should handle missing newName', async () => {
      const handler = getHandler('list.rename');

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-rename-2',
        listId: 'my-list',
        newName: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('New list name is required')
          })
        })
      );
    });

    it('should handle whitespace-only newName', async () => {
      const handler = getHandler('list.rename');

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-rename-3',
        listId: 'my-list',
        newName: '   '
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('cannot be empty')
          })
        })
      );
    });

    it('should handle rename errors', async () => {
      const handler = getHandler('list.rename');

      mockSocial.list.updateList.mockResolvedValue({
        success: false,
        error: { code: 'RENAME_ERROR', message: 'List not found' }
      });

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-rename-4',
        listId: 'nonexistent',
        newName: 'New Name'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List not found')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.rename');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.rename',
        id: 'test-rename-5',
        listId: 'my-list',
        newName: 'New Name'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('list.delete handler - validation and errors', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('list.delete');

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-delete-1',
        listId: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List ID is required')
          })
        })
      );
    });

    it('should handle delete errors', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('Yes' as any);
      mockSocial.list.deleteList.mockResolvedValue({
        success: false,
        error: { code: 'DELETE_ERROR', message: 'Cannot delete system list' }
      });

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-delete-2',
        listId: 'system-list'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Cannot delete system list')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('Yes' as any);
      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-delete-3',
        listId: 'my-list'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should use listName in confirmation dialog when provided', async () => {
      const handler = getHandler('list.delete');

      vi.mocked(vscode.window.showWarningMessage).mockResolvedValue('No' as any);

      await handler(mockPanel, {
        type: 'list.delete',
        id: 'test-delete-4',
        listId: 'my-list',
        listName: 'My Awesome List'
      });

      expect(vscode.window.showWarningMessage).toHaveBeenCalledWith(
        expect.stringContaining('My Awesome List'),
        expect.any(Object),
        'Yes',
        'No'
      );
    });
  });

  describe('list.follow handler - validation and errors', () => {
    it('should handle missing sourceRepository', async () => {
      const handler = getHandler('list.follow');

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-follow-1',
        sourceRepository: '',
        sourceListId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Source repository is required')
          })
        })
      );
    });

    it('should handle missing sourceListId', async () => {
      const handler = getHandler('list.follow');

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-follow-2',
        sourceRepository: 'https://github.com/user/repo',
        sourceListId: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Source list ID is required')
          })
        })
      );
    });

    it('should handle follow errors', async () => {
      const handler = getHandler('list.follow');

      mockSocial.list.followList.mockResolvedValue({
        success: false,
        error: { code: 'FOLLOW_ERROR', message: 'List already followed' }
      });

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-follow-3',
        sourceRepository: 'https://github.com/user/repo',
        sourceListId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List already followed')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.follow');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-follow-4',
        sourceRepository: 'https://github.com/user/repo',
        sourceListId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should pass targetListId when provided', async () => {
      const handler = getHandler('list.follow');

      mockSocial.list.followList.mockResolvedValue({
        success: true,
        data: { listId: 'custom-name' }
      });

      await handler(mockPanel, {
        type: 'list.follow',
        id: 'test-follow-5',
        sourceRepository: 'https://github.com/user/repo',
        sourceListId: 'reading',
        targetListId: 'custom-name'
      });

      expect(mockSocial.list.followList).toHaveBeenCalledWith(
        expect.any(String),
        'https://github.com/user/repo',
        'reading',
        'custom-name',
        expect.any(String)
      );
    });
  });

  describe('list.sync handler - validation and errors', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('list.sync');

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-sync-1',
        listId: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List ID is required')
          })
        })
      );
    });

    it('should handle sync errors', async () => {
      const handler = getHandler('list.sync');

      mockSocial.list.syncFollowedList.mockResolvedValue({
        success: false,
        error: { code: 'SYNC_ERROR', message: 'List is not a followed list' }
      });

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-sync-2',
        listId: 'local-list'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('not a followed list')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('list.sync');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-sync-3',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should show different message when no changes during sync', async () => {
      const handler = getHandler('list.sync');

      mockSocial.list.syncFollowedList.mockResolvedValue({
        success: true,
        data: { added: 0, removed: 0 }
      });

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-sync-4',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listSynced',
          data: expect.objectContaining({
            message: 'List synced successfully',
            added: 0,
            removed: 0
          })
        })
      );
    });

    it('should show count message when changes occurred during sync', async () => {
      const handler = getHandler('list.sync');

      mockSocial.list.syncFollowedList.mockResolvedValue({
        success: true,
        data: { added: 3, removed: 1 }
      });

      await handler(mockPanel, {
        type: 'list.sync',
        id: 'test-sync-5',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'listSynced',
          data: expect.objectContaining({
            message: 'List synced: 3 added, 1 removed',
            added: 3,
            removed: 1
          })
        })
      );
    });
  });
});
