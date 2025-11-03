import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from '.';
import {
  type List,
  social
} from '@gitsocial/core';
import { getStorageUri } from '../extension';

// Message types for list operations
export type ListsMessages =
  | { type: 'list.getAll'; id?: string; repository?: string }
  | { type: 'list.create'; id?: string; listId: string; name: string }
  | { type: 'list.rename'; id?: string; listId: string; newName: string }
  | { type: 'list.delete'; id?: string; listId: string; listName?: string }
  | { type: 'list.follow'; id?: string; sourceRepository: string; sourceListId: string; targetListId?: string }
  | { type: 'list.sync'; id?: string; listId: string }
  | { type: 'list.unfollow'; id?: string; listId: string };

// Response types for list operations
export type ListsResponses =
  | { type: 'lists'; data: List[]; requestId?: string }
  | { type: 'listCreated'; data: { message: string }; requestId?: string }
  | { type: 'listRenamed'; data: { message: string }; requestId?: string }
  | { type: 'listDeleted'; data: { message: string }; requestId?: string }
  | { type: 'listFollowed'; data: { message: string; listId: string }; requestId?: string }
  | { type: 'listSynced'; data: { message: string; added: number; removed: number }; requestId?: string }
  | { type: 'listUnfollowed'; data: { message: string }; requestId?: string };

// Helper for broadcasting to all panels
let broadcastToAll: ((message: { type: string; [key: string]: unknown }) => void) | undefined;

export function setListCallbacks(
  broadcast: (message: { type: string; [key: string]: unknown }) => void
): void {
  broadcastToAll = broadcast;
}

// Register get lists handler
registerHandler('list.getAll', async function handleGetLists(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.getAll') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.getAll' }>;

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Determine if this is a request for other repository lists
    const repository = msg.repository;
    // Always pass workspace root so other repositories can be properly detected
    const workspaceRoot = workspaceFolder.uri.fsPath;
    const result = await social.list.getLists(repository || workspaceFolder.uri.fsPath, workspaceRoot);

    if (result.success && result.data) {
      // Send lists response
      postMessage(panel, 'lists', result.data, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to get lists');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get lists'
    }, requestId);
  }
});

// Register create list handler
registerHandler('list.create', async function handleCreateList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.create') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.create' }>;
    if (!msg.listId) {
      throw new Error('List ID is required');
    }
    if (!msg.name) {
      throw new Error('List name is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Create list using core library
    const result = await social.list.createList(
      workspaceFolder.uri.fsPath,
      msg.listId,
      msg.name
    );

    if (result.success) {
      // Send success response
      postMessage(panel, 'listCreated', {
        message: 'List created successfully!'
      }, requestId);

      // Refresh all views
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to create list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to create list'
    }, requestId);
  }
});

// Register rename list handler
registerHandler('list.rename', async function handleRenameList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.rename') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.rename' }>;
    if (!msg.listId) {
      throw new Error('List ID is required');
    }
    if (!msg.newName) {
      throw new Error('New list name is required');
    }

    const trimmedName = msg.newName.trim();
    if (!trimmedName) {
      throw new Error('List name cannot be empty');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Rename list using core library updateList function
    const result = await social.list.updateList(
      workspaceFolder.uri.fsPath,
      msg.listId,
      { name: trimmedName }
    );

    if (result.success) {
      // Send success response to initiating panel
      postMessage(panel, 'listRenamed', {
        message: 'List renamed successfully!'
      }, requestId);

      // Broadcast single refresh with scope metadata
      // This eliminates race conditions by sending operation context in one message
      if (broadcastToAll) {
        broadcastToAll({
          type: 'refresh',
          operation: 'listRenamed',
          scope: ['lists'],
          metadata: { listId: msg.listId, newName: trimmedName }
        });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to rename list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to rename list'
    }, requestId);
  }
});

// Register delete list handler
registerHandler('list.delete', async function handleDeleteList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.delete') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.delete' }>;
    if (!msg.listId) {
      throw new Error('List ID is required');
    }

    // Show confirmation dialog using list name if available, otherwise use ID
    const confirmed = await vscode.window.showWarningMessage(
      `Are you sure you want to delete the list "${msg.listName || msg.listId}"?`,
      { modal: true },
      'Yes',
      'No'
    );

    if (confirmed !== 'Yes') {
      // User cancelled, send success response but don't delete
      postMessage(panel, 'listDeleted', {
        message: 'Delete cancelled.'
      }, requestId);
      return;
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Delete list using core library
    const result = await social.list.deleteList(
      workspaceFolder.uri.fsPath,
      msg.listId
    );

    if (result.success) {
      // Send success response
      postMessage(panel, 'listDeleted', {
        message: 'List deleted successfully!'
      }, requestId);

      // Refresh all views
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to delete list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to delete list'
    }, requestId);
  }
});

// Register follow list handler
registerHandler('list.follow', async function handleFollowList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.follow') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.follow' }>;
    if (!msg.sourceRepository) {
      throw new Error('Source repository is required');
    }
    if (!msg.sourceListId) {
      throw new Error('Source list ID is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const storageUri = getStorageUri();
    const storageBase = storageUri?.fsPath;

    // Follow the list using core library
    const result = await social.list.followList(
      workspaceFolder.uri.fsPath,
      msg.sourceRepository,
      msg.sourceListId,
      msg.targetListId,
      storageBase
    );

    if (result.success && result.data) {
      // Send success response
      postMessage(panel, 'listFollowed', {
        message: `Successfully followed list "${msg.sourceListId}"`,
        listId: result.data.listId
      }, requestId);

      // Refresh all views
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to follow list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to follow list'
    }, requestId);
  }
});

// Register sync followed list handler
registerHandler('list.sync', async function handleSyncFollowedList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.sync') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.sync' }>;
    if (!msg.listId) {
      throw new Error('List ID is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const storageUri = getStorageUri();
    const storageBase = storageUri?.fsPath;

    // Sync the followed list using core library
    const result = await social.list.syncFollowedList(
      workspaceFolder.uri.fsPath,
      msg.listId,
      storageBase
    );

    if (result.success && result.data) {
      // Send success response
      const { added, removed } = result.data;
      let message = 'List synced successfully';
      if (added > 0 || removed > 0) {
        message = `List synced: ${added} added, ${removed} removed`;
      }
      postMessage(panel, 'listSynced', {
        message,
        added,
        removed
      }, requestId);

      // Refresh all views
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to sync list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to sync list'
    }, requestId);
  }
});

// Register unfollow list handler
registerHandler('list.unfollow', async function handleUnfollowList(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'list.unfollow') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<ListsMessages, { type: 'list.unfollow' }>;
    if (!msg.listId) {
      throw new Error('List ID is required');
    }

    // Show confirmation dialog
    const confirmed = await vscode.window.showInformationMessage(
      'Unfollow this list? It will become a regular list that you can manage independently.',
      { modal: true },
      'Unfollow',
      'Cancel'
    );

    if (confirmed !== 'Unfollow') {
      // User cancelled, send success response but don't unfollow
      postMessage(panel, 'listUnfollowed', {
        message: 'Unfollow cancelled.'
      }, requestId);
      return;
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Unfollow the list using core library
    const result = await social.list.unfollowList(
      workspaceFolder.uri.fsPath,
      msg.listId
    );

    if (result.success) {
      // Send success response
      postMessage(panel, 'listUnfollowed', {
        message: 'List unfollowed successfully!'
      }, requestId);

      // Refresh all views
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      throw new Error(result.error?.message || 'Failed to unfollow list');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to unfollow list'
    }, requestId);
  }
});
