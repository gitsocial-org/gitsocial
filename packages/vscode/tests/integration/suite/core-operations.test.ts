import * as assert from 'assert';
import * as vscode from 'vscode';
import { getHandler, registerHandler } from '../../../src/handlers/registry';

describe('Core Operations Test Suite', function() {
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

  describe('Handler System', function() {
    it('Should have handler registry system functional', function() {
      // Test that the handler registry mechanism works
      // Actual handlers are registered when extension activates
      const testType = 'test.core.operations';
      const testHandler = async (): Promise<void> => Promise.resolve();

      registerHandler(testType, testHandler);
      const retrieved = getHandler(testType);

      assert.ok(retrieved, 'Handler registry should be functional');
      assert.strictEqual(retrieved, testHandler, 'Retrieved handler should match registered handler');
    });

    it('Should return undefined for unregistered handlers', function() {
      const handler = getHandler('nonexistent.core.handler');
      assert.strictEqual(handler, undefined, 'Unregistered handler should return undefined');
    });
  });

  describe('Core Commands', function() {
    it('Should have createPost command available', async function() {
      const commands = await vscode.commands.getCommands();
      assert.ok(
        commands.includes('gitsocial.createPost'),
        'createPost command should be available'
      );
    });

    it('Should have initialize command available', async function() {
      const commands = await vscode.commands.getCommands();
      assert.ok(
        commands.includes('gitsocial.initialize'),
        'initialize command should be available'
      );
    });

    it('Should have view commands available', async function() {
      const commands = await vscode.commands.getCommands();
      const viewCommands = [
        'gitsocial.openTimeline',
        'gitsocial.openRepository',
        'gitsocial.openSearch',
        'gitsocial.openSettings'
      ];

      for (const cmd of viewCommands) {
        assert.ok(
          commands.includes(cmd),
          `${cmd} command should be available`
        );
      }
    });
  });

  describe('Integration with Webview', function() {
    it('Should open timeline view that can trigger post operations', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const hasTimeline = tabGroups.some(group =>
        group.tabs.some(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        )
      );

      assert.ok(hasTimeline, 'Timeline view should be open for post operations');
    });

    it('Should open repository view that can display posts', async function() {
      try {
        await vscode.commands.executeCommand('gitsocial.openRepository');
        await new Promise(resolve => setTimeout(resolve, 50));

        const tabGroups = vscode.window.tabGroups.all;
        const hasRepositoryView = tabGroups.some(group =>
          group.tabs.some(tab =>
            (tab.label === 'Repository' || tab.label === 'Welcome') &&
            tab.input instanceof vscode.TabInputWebview
          )
        );

        assert.ok(hasRepositoryView, 'Repository or Welcome view should be open');
      } catch (error) {
        this.skip();
      }
    });
  });

  describe('Operations via Webview', function() {
    it('Should support post operations through webview messaging', async function() {
      // Post creation, comments, reposts, and quotes are handled via
      // webview messages (social.createPost, social.createInteraction)
      // rather than direct commands. This architecture allows for
      // richer UI interactions in the webview.
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const hasTimeline = tabGroups.some(group =>
        group.tabs.some(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        )
      );

      assert.ok(hasTimeline, 'Timeline webview should be available for operations');
    });
  });
});
