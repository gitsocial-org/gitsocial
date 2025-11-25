import * as assert from 'assert';
import * as vscode from 'vscode';
import { getHandler, registerHandler } from '../../../src/handlers/registry';

describe('Handler Registry Test Suite', function() {
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

  describe('Handler Registration', function() {
    it('Should register and retrieve handler successfully', function() {
      const testType = 'test.registry.basic';
      const testHandler = async (): Promise<void> => Promise.resolve();

      registerHandler(testType, testHandler);
      const retrieved = getHandler(testType);

      assert.ok(retrieved, 'Handler should be registered');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match registered handler');
    });

    it('Should handle duplicate handler registration by overwriting', function() {
      const testType = 'test.registry.duplicate';
      const firstHandler = async (): Promise<void> => Promise.resolve();
      const secondHandler = async (): Promise<void> => Promise.resolve();

      registerHandler(testType, firstHandler);
      const firstRetrieved = getHandler(testType);
      assert.strictEqual(firstRetrieved, firstHandler, 'First handler should be retrieved');

      registerHandler(testType, secondHandler);
      const secondRetrieved = getHandler(testType);
      assert.strictEqual(secondRetrieved, secondHandler, 'Second handler should overwrite first');
      assert.notStrictEqual(secondRetrieved, firstHandler, 'Second handler should not be first handler');
    });

    it('Should support multiple handlers with different types', function() {
      const type1 = 'test.registry.multi.1';
      const type2 = 'test.registry.multi.2';
      const type3 = 'test.registry.multi.3';

      const handler1 = async (): Promise<void> => Promise.resolve();
      const handler2 = async (): Promise<void> => Promise.resolve();
      const handler3 = async (): Promise<void> => Promise.resolve();

      registerHandler(type1, handler1);
      registerHandler(type2, handler2);
      registerHandler(type3, handler3);

      assert.strictEqual(getHandler(type1), handler1, 'Handler 1 should be retrievable');
      assert.strictEqual(getHandler(type2), handler2, 'Handler 2 should be retrievable');
      assert.strictEqual(getHandler(type3), handler3, 'Handler 3 should be retrievable');
    });

    it('Should return undefined for non-existent handler type', function() {
      const result = getHandler('test.registry.nonexistent.handler.type');
      assert.strictEqual(result, undefined, 'Non-existent handler should return undefined');
    });
  });

  describe('Handler Execution', function() {
    it('Should support async handler execution', async function() {
      const testType = 'test.registry.async';
      let executed = false;

      const asyncHandler = async (): Promise<void> => {
        await new Promise(resolve => setTimeout(resolve, 10));
        executed = true;
      };

      registerHandler(testType, asyncHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      await handler(mockPanel, { type: testType });
      assert.ok(executed, 'Async handler should have executed');
    });

    it('Should support sync handler execution', async function() {
      const testType = 'test.registry.sync';
      let executed = false;

      const syncHandler = (): void => {
        executed = true;
      };

      registerHandler(testType, syncHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      await handler(mockPanel, { type: testType });
      assert.ok(executed, 'Sync handler should have executed');
    });

    it('Should handle errors thrown by handlers gracefully', async function() {
      const testType = 'test.registry.error';
      const errorMessage = 'Test handler error';

      const errorHandler = (): Promise<void> => {
        throw new Error(errorMessage);
      };

      registerHandler(testType, errorHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;

      try {
        await handler(mockPanel, { type: testType });
        assert.fail('Handler should have thrown an error');
      } catch (error) {
        assert.ok(error instanceof Error, 'Error should be an Error instance');
        if (error instanceof Error) {
          assert.strictEqual(error.message, errorMessage, 'Error message should match');
        }
      }
    });

    it('Should support handler registration with complex message types', async function() {
      const testType = 'test.registry.complex';

      type ComplexMessage = {
        type: typeof testType;
        id?: string;
        data: {
          field1: string;
          field2: number;
        };
      };

      let receivedMessage: ComplexMessage | null = null;

      const complexHandler = (_panel: vscode.WebviewPanel, message: ComplexMessage): void => {
        receivedMessage = message;
      };

      registerHandler(testType, complexHandler);
      const handler = getHandler(testType);
      assert.ok(handler, 'Handler should be registered');

      const mockPanel = {} as vscode.WebviewPanel;
      const testMessage: ComplexMessage = {
        type: testType,
        id: 'test-id',
        data: {
          field1: 'test',
          field2: 42
        }
      };

      await handler(mockPanel, testMessage);
      assert.ok(receivedMessage, 'Handler should have received message');
      const message = receivedMessage as ComplexMessage;
      assert.strictEqual(message.data.field1, 'test', 'Message field1 should match');
      assert.strictEqual(message.data.field2, 42, 'Message field2 should match');
    });
  });
});
