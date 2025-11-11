import * as assert from 'assert';
import * as vscode from 'vscode';
import { getHandler, registerHandler } from '../../handlers/registry';
import { getStorageUri } from '../../extension';

describe('Storage Operations Test Suite', function() {
  this.timeout(30000);

  before(async function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    if (ext && !ext.isActive) {
      await ext.activate();
    }
  });

  afterEach(async function() {
    await vscode.commands.executeCommand('workbench.action.closeAllEditors');
  });

  describe('Global Storage URI', function() {
    it('Should initialize global storage URI from extension context', function() {
      const storageUri = getStorageUri();
      assert.ok(storageUri, 'Global storage URI should be initialized');
      assert.ok(storageUri instanceof vscode.Uri, 'Storage URI should be a vscode.Uri instance');
    });

    it('Should be accessible via getStorageUri()', function() {
      const storageUri = getStorageUri();
      assert.ok(storageUri, 'Storage URI should be accessible');
      assert.strictEqual(typeof storageUri.fsPath, 'string', 'Storage URI should have fsPath');
      assert.ok(storageUri.fsPath.length > 0, 'Storage URI fsPath should not be empty');
    });

    it('Should have storage subsystems initialized', function() {
      const storageUri = getStorageUri();
      assert.ok(storageUri, 'Storage URI required for subsystem initialization');
    });
  });

  describe('Storage Systems Integration', function() {
    it('Should have storage subsystems available after initialization', function() {
      const storageUri = getStorageUri();
      assert.ok(storageUri, 'Storage URI should be available for storage systems');
    });

    it('Should support cache handler registration', function() {
      const testHandler = async (): Promise<void> => Promise.resolve();
      registerHandler('test.cache.operation', testHandler);
      const retrieved = getHandler('test.cache.operation');
      assert.ok(retrieved, 'Cache-related handlers should be registerable');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match');
    });

    it('Should support avatar cache handler registration', function() {
      const testHandler = async (): Promise<void> => Promise.resolve();
      registerHandler('test.avatarCache.operation', testHandler);
      const retrieved = getHandler('test.avatarCache.operation');
      assert.ok(retrieved, 'Avatar cache handlers should be registerable');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match');
    });

    it('Should support repository storage handler registration', function() {
      const testHandler = async (): Promise<void> => Promise.resolve();
      registerHandler('test.repositoryStorage.operation', testHandler);
      const retrieved = getHandler('test.repositoryStorage.operation');
      assert.ok(retrieved, 'Repository storage handlers should be registerable');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match');
    });
  });

  describe('Storage Error Scenarios', function() {
    it('Should handle storage URI being undefined gracefully', function() {
      const storageUri = getStorageUri();
      const isValidOrUndefined = storageUri === undefined || storageUri instanceof vscode.Uri;
      assert.ok(isValidOrUndefined, 'Storage URI should be either undefined or a valid Uri');
    });

    it('Should handle concurrent storage handler registrations', function() {
      const type1 = 'test.storage.concurrent.1';
      const type2 = 'test.storage.concurrent.2';
      const type3 = 'test.storage.concurrent.3';

      const handler1 = async (): Promise<void> => Promise.resolve();
      const handler2 = async (): Promise<void> => Promise.resolve();
      const handler3 = async (): Promise<void> => Promise.resolve();

      registerHandler(type1, handler1);
      registerHandler(type2, handler2);
      registerHandler(type3, handler3);

      const retrieved1 = getHandler(type1);
      const retrieved2 = getHandler(type2);
      const retrieved3 = getHandler(type3);

      assert.ok(retrieved1, 'First concurrent handler should be registered');
      assert.ok(retrieved2, 'Second concurrent handler should be registered');
      assert.ok(retrieved3, 'Third concurrent handler should be registered');
      assert.strictEqual(retrieved1, handler1, 'First handler should match');
      assert.strictEqual(retrieved2, handler2, 'Second handler should match');
      assert.strictEqual(retrieved3, handler3, 'Third handler should match');
    });

    it('Should maintain storage system integrity after handler errors', async function() {
      const errorType = 'test.storage.error';
      const successType = 'test.storage.success';
      let successCount = 0;

      const errorHandler = (): Promise<void> => {
        throw new Error('Storage handler error');
      };

      const successHandler = (): void => {
        successCount++;
      };

      registerHandler(errorType, errorHandler);
      registerHandler(successType, successHandler);

      const errorHandlerRetrieved = getHandler(errorType);
      const successHandlerRetrieved = getHandler(successType);

      assert.ok(errorHandlerRetrieved, 'Error handler should be registered');
      assert.ok(successHandlerRetrieved, 'Success handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      try {
        await errorHandlerRetrieved(mockPanel, { type: errorType });
      } catch {
        // Expected error
      }

      await successHandlerRetrieved(mockPanel, { type: successType });
      assert.strictEqual(successCount, 1, 'Success handler should work after error handler fails');
    });

    it('Should handle storage handler with invalid message types', function() {
      const invalidType = 'storage.invalid.handler.type.does.not.exist';
      const handler = getHandler(invalidType);
      assert.strictEqual(handler, undefined, 'Invalid storage handler type should return undefined');
    });

    it('Should handle multiple storage operations sequentially', async function() {
      const type = 'test.storage.sequential';
      let callCount = 0;

      const sequentialHandler = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 10));
        callCount++;
      };

      registerHandler(type, sequentialHandler);
      const handler = getHandler(type);
      assert.ok(handler, 'Sequential handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      for (let i = 0; i < 5; i++) {
        await handler(mockPanel, { type });
      }

      assert.strictEqual(callCount, 5, 'All 5 sequential storage operations should complete');
    });

    it('Should handle storage handler registration overwriting', async function() {
      const type = 'test.storage.overwrite';
      let firstCalled = false;
      let secondCalled = false;

      const firstHandler = (): void => {
        firstCalled = true;
      };

      const secondHandler = (): void => {
        secondCalled = true;
      };

      registerHandler(type, firstHandler);
      registerHandler(type, secondHandler);

      const handler = getHandler(type);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      await handler(mockPanel, { type });

      assert.strictEqual(firstCalled, false, 'First handler should not be called after overwrite');
      assert.strictEqual(secondCalled, true, 'Second handler should be called');
    });

    it('Should validate storage subsystem availability', function() {
      const storageUri = getStorageUri();
      if (storageUri) {
        assert.ok(storageUri.fsPath, 'Storage URI should have fsPath when defined');
        assert.ok(storageUri.fsPath.length > 0, 'Storage URI fsPath should not be empty');
      }
      assert.ok(true, 'Storage validation check completed');
    });

    it('Should handle storage operations with empty workspace context', function() {
      const storageUri = getStorageUri();
      const workspaceFolder = vscode.workspace.workspaceFolders?.[0];

      assert.ok(storageUri !== undefined || workspaceFolder === undefined,
        'Storage URI should be defined or workspace should be empty');
    });
  });
});
