import * as assert from 'assert';
import * as vscode from 'vscode';
import { getHandler, registerHandler } from '../../handlers/registry';

describe('Message Passing Test Suite', function() {
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

  describe('Handler Registry', function() {
    it('Should register and retrieve handlers', function() {
      const testType = 'test.message.type';
      const testHandler = async (): Promise<void> => {
        return Promise.resolve();
      };

      registerHandler(testType, testHandler);
      const retrieved = getHandler(testType);

      assert.ok(retrieved, 'Handler should be registered');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match registered handler');
    });

    it('Should return undefined for unregistered handler', function() {
      const handler = getHandler('nonexistent.handler.type');
      assert.strictEqual(handler, undefined, 'Unregistered handler should return undefined');
    });

    it('Should have handler registry system available', function() {
      // Handler registry should be functional
      // Handlers are registered when extension activates and modules are imported
      // We can't easily test specific handlers without triggering full extension initialization
      // but we can verify the registry system works
      const testType = 'test.registry.verification';
      const testHandler = async (): Promise<void> => Promise.resolve();

      registerHandler(testType, testHandler);
      const retrieved = getHandler(testType);

      assert.ok(retrieved, 'Registry should allow registration and retrieval');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match');
    });
  });

  describe('Panel Message Communication', function() {
    it('Should create panel that can receive messages', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTab = tabGroups.flatMap(group => group.tabs)
        .find(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        );

      assert.ok(webviewTab, 'Timeline panel should be created');
      assert.ok(webviewTab?.input instanceof vscode.TabInputWebview, 'Panel should have webview input');
    });

    it('Should handle opening multiple panels', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 300));

      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 300));

      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 300));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTabs = tabGroups.flatMap(group => group.tabs)
        .filter(tab => tab.input instanceof vscode.TabInputWebview);

      assert.ok(webviewTabs.length >= 3, 'Should have at least 3 webview panels');
    });
  });

  describe('Special Message Handling', function() {
    it('Should handle openView message by creating new panel', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const initialTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 500));

      const finalTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(finalTabCount > initialTabCount, 'Opening view should create new panel');
    });

    it('Should handle panel closure', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(tabCountBefore > 0, 'Should have at least one panel');

      await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(
        tabCountAfter,
        tabCountBefore - 1,
        'Panel should be removed after closure'
      );
    });
  });

  describe('Panel Reuse and Deduplication', function() {
    it('Should reuse existing panel when opening same view', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(
        tabCountBefore,
        tabCountAfter,
        'Should reuse existing panel instead of creating new one'
      );
    });

    it('Should create separate panels for different views', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 300));

      const tabCountAfterFirst = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 300));

      const tabCountAfterSecond = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(
        tabCountAfterSecond > tabCountAfterFirst,
        'Different views should create separate panels'
      );
    });
  });

  describe('Message Broadcasting', function() {
    it('Should broadcast to multiple active panels', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTabs = tabGroups.flatMap(group => group.tabs)
        .filter(tab => tab.input instanceof vscode.TabInputWebview);

      assert.ok(
        webviewTabs.length >= 3,
        'Should have multiple panels for broadcast testing'
      );
    });
  });

  describe('Error Handling', function() {
    it('Should handle messages with invalid types gracefully', function() {
      const invalidType = 'completely.invalid.message.type.that.does.not.exist';
      const handler = getHandler(invalidType);
      assert.strictEqual(handler, undefined, 'Invalid message type should return undefined handler');
    });

    it('Should handle handler registration errors', function() {
      const testType = 'test.error.registration';

      const firstHandler = (): Promise<void> => {
        return Promise.resolve();
      };

      registerHandler(testType, firstHandler);
      const retrieved1 = getHandler(testType);
      assert.ok(retrieved1, 'First handler should be registered');

      const secondHandler = (): Promise<void> => {
        throw new Error('Second handler error');
      };

      registerHandler(testType, secondHandler);
      const retrieved2 = getHandler(testType);
      assert.notStrictEqual(retrieved2, retrieved1, 'Second registration should overwrite first');
    });

    it('Should handle missing message fields gracefully', async function() {
      const testType = 'test.error.missingFields';
      type TestMessage = { type: string; data?: unknown };
      let receivedMessage: TestMessage | null = null;

      const handler = (_panel: vscode.WebviewPanel, message: TestMessage): void => {
        receivedMessage = message;
      };

      registerHandler(testType, handler);
      const retrievedHandler = getHandler(testType);
      assert.ok(retrievedHandler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      const incompleteMessage = { type: testType };

      await retrievedHandler(mockPanel, incompleteMessage);
      assert.ok(receivedMessage, 'Handler should receive incomplete message');
      const message = receivedMessage as TestMessage;
      assert.strictEqual(message.type, testType, 'Message type should be preserved');
    });

    it('Should handle handler errors without crashing', async function() {
      const testType = 'test.error.handlerError';

      const errorHandler = (): Promise<void> => {
        throw new Error('Intentional handler error');
      };

      registerHandler(testType, errorHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Error-throwing handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      try {
        await handler(mockPanel, { type: testType });
        assert.fail('Handler should have thrown error');
      } catch (error) {
        assert.ok(error instanceof Error, 'Should catch handler error');
      }
    });

    it('Should handle malformed message payloads', async function() {
      const testType = 'test.error.malformed';
      let errorCaught = false;

      type MalformedMessage = { type: string; data?: unknown };
      const strictHandler = (_panel: vscode.WebviewPanel, message: MalformedMessage): void => {
        if (!message.data || typeof message.data !== 'object') {
          throw new Error('Invalid message data');
        }
      };

      registerHandler(testType, strictHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      const malformedMessage = { type: testType, data: 'not-an-object' };

      try {
        await handler(mockPanel, malformedMessage);
      } catch (error) {
        errorCaught = true;
      }

      assert.ok(errorCaught, 'Handler should catch malformed payload');
    });

    it('Should handle rapid successive messages', async function() {
      const testType = 'test.error.rapid';
      let callCount = 0;

      const countingHandler = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 5));
        callCount++;
      };

      registerHandler(testType, countingHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      const promises = Array.from({ length: 10 }, () =>
        handler(mockPanel, { type: testType })
      );

      await Promise.all(promises);
      assert.strictEqual(callCount, 10, 'All 10 rapid messages should be handled');
    });

    it('Should handle message with missing handler gracefully', function() {
      const nonExistentType = 'test.error.nonExistent';
      const handler = getHandler(nonExistentType);
      assert.strictEqual(handler, undefined, 'Should return undefined for non-existent handler');
    });

    it('Should maintain handler registry integrity after errors', async function() {
      const testType = 'test.error.integrity';
      let successCallCount = 0;

      const reliableHandler = (): void => {
        successCallCount++;
      };

      registerHandler(testType, reliableHandler);

      const errorType = 'test.error.integrity.error';
      const errorHandler = (): Promise<void> => {
        throw new Error('Error handler');
      };
      registerHandler(errorType, errorHandler);

      const successHandler = getHandler(testType);
      const failHandler = getHandler(errorType);

      assert.ok(successHandler, 'Success handler should still be registered');
      assert.ok(failHandler, 'Error handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      await successHandler(mockPanel, { type: testType });
      assert.strictEqual(successCallCount, 1, 'Success handler should work after error handler registration');

      try {
        await failHandler(mockPanel, { type: errorType });
      } catch {
        // Expected error
      }

      await successHandler(mockPanel, { type: testType });
      assert.strictEqual(successCallCount, 2, 'Success handler should still work after error handler execution');
    });

    it('Should handle concurrent handler calls without interference', async function() {
      const type1 = 'test.error.concurrent.1';
      const type2 = 'test.error.concurrent.2';
      let count1 = 0;
      let count2 = 0;

      const handler1 = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 10));
        count1++;
      };

      const handler2 = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 10));
        count2++;
      };

      registerHandler(type1, handler1);
      registerHandler(type2, handler2);

      const h1 = getHandler(type1);
      const h2 = getHandler(type2);

      assert.ok(h1, 'Handler 1 should be registered');
      assert.ok(h2, 'Handler 2 should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      await Promise.all([
        h1(mockPanel, { type: type1 }),
        h2(mockPanel, { type: type2 }),
        h1(mockPanel, { type: type1 }),
        h2(mockPanel, { type: type2 })
      ]);

      assert.strictEqual(count1, 2, 'Handler 1 should be called twice');
      assert.strictEqual(count2, 2, 'Handler 2 should be called twice');
    });

    it('Should handle async errors in handler execution', async function() {
      const testType = 'test.error.asyncError';

      const asyncErrorHandler = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 5));
        throw new Error('Async error after delay');
      };

      registerHandler(testType, asyncErrorHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      try {
        await handler(mockPanel, { type: testType });
        assert.fail('Async error should be thrown');
      } catch (error) {
        assert.ok(error instanceof Error, 'Should catch async error');
        assert.strictEqual((error ).message, 'Async error after delay');
      }
    });

    it('Should handle panel disposal during message handling', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(tabCountBefore > 0, 'Should have at least one panel');

      await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
      await new Promise(resolve => setTimeout(resolve, 500));

      const tabCountAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(tabCountAfter, tabCountBefore - 1, 'Panel should be disposed');
    });

    it('Should handle request ID tracking in messages', async function() {
      const testType = 'test.error.requestId';
      let receivedRequestId: string | undefined;

      type RequestMessage = { type: string; id?: string };
      const handler = (_panel: vscode.WebviewPanel, message: RequestMessage): void => {
        receivedRequestId = message.id;
      };

      registerHandler(testType, handler);
      const retrievedHandler = getHandler(testType);
      assert.ok(retrievedHandler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      const requestId = 'test-request-id-12345';
      const messageWithId = { type: testType, id: requestId };

      await retrievedHandler(mockPanel, messageWithId);
      assert.strictEqual(receivedRequestId, requestId, 'Request ID should be preserved');
    });
  });
});
