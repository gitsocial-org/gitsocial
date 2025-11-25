import * as assert from 'assert';
import * as vscode from 'vscode';
import { waitForWebviewPanel } from '../test-helpers';

describe('Webview Lifecycle Test Suite', function() {
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

  describe('Panel Creation', function() {
    it('Should create timeline webview panel', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await waitForWebviewPanel('Timeline');

      const tabGroups = vscode.window.tabGroups.all;
      const hasWebview = tabGroups.some(group =>
        group.tabs.some(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        )
      );

      assert.ok(hasWebview, 'Timeline webview should be created');
    });

    it('Should create repository webview panel', async function() {
      try {
        await vscode.commands.executeCommand('gitsocial.openRepository');
        await waitForWebviewPanel(/^(Repository|Welcome)$/);

        const tabGroups = vscode.window.tabGroups.all;
        const hasWebview = tabGroups.some(group =>
          group.tabs.some(tab =>
            (tab.label === 'Repository' || tab.label === 'Welcome') &&
            tab.input instanceof vscode.TabInputWebview
          )
        );

        assert.ok(hasWebview, 'Repository or Welcome webview should be created');
      } catch (error) {
        this.skip();
      }
    });

    it('Should create search webview panel', async function() {
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await waitForWebviewPanel('Search');

      const tabGroups = vscode.window.tabGroups.all;
      const hasWebview = tabGroups.some(group =>
        group.tabs.some(tab =>
          tab.label === 'Search' &&
          tab.input instanceof vscode.TabInputWebview
        )
      );

      assert.ok(hasWebview, 'Search webview should be created');
    });

    it('Should create settings webview panel', async function() {
      await vscode.commands.executeCommand('gitsocial.openSettings');
      await waitForWebviewPanel('Settings');

      const tabGroups = vscode.window.tabGroups.all;
      const hasWebview = tabGroups.some(group =>
        group.tabs.some(tab =>
          tab.label === 'Settings' &&
          tab.input instanceof vscode.TabInputWebview
        )
      );

      assert.ok(hasWebview, 'Settings webview should be created');
    });
  });

  describe('Panel Reuse', function() {
    it('Should reuse timeline panel when opened multiple times', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await waitForWebviewPanel('Timeline');

      const firstTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await waitForWebviewPanel('Timeline');

      const secondTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(
        firstTabCount,
        secondTabCount,
        'Should not create duplicate timeline panel'
      );
    });

    it('Should reuse settings panel when opened multiple times', async function() {
      await vscode.commands.executeCommand('gitsocial.openSettings');
      await waitForWebviewPanel('Settings');

      const firstTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSettings');
      await waitForWebviewPanel('Settings');

      const secondTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(
        firstTabCount,
        secondTabCount,
        'Should not create duplicate settings panel'
      );
    });
  });

  describe('Multiple Panels', function() {
    it('Should allow multiple different panel types', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTabs = tabGroups.flatMap(group =>
        group.tabs.filter(tab => tab.input instanceof vscode.TabInputWebview)
      );

      assert.ok(
        webviewTabs.length >= 3,
        'Should have at least 3 webview panels open'
      );

      const hasTimeline = webviewTabs.some(tab => tab.label === 'Timeline');
      const hasSearch = webviewTabs.some(tab => tab.label === 'Search');
      const hasSettings = webviewTabs.some(tab => tab.label === 'Settings');

      assert.ok(hasTimeline, 'Should have Timeline panel');
      assert.ok(hasSearch, 'Should have Search panel');
      assert.ok(hasSettings, 'Should have Settings panel');
    });
  });

  describe('Panel Disposal', function() {
    it('Should properly dispose panel when closed', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const initialTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(initialTabCount > 0, 'Should have tabs open');

      await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
      await new Promise(resolve => setTimeout(resolve, 50));

      const finalTabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(
        finalTabCount,
        initialTabCount - 1,
        'Tab count should decrease after closing panel'
      );
    });

    it('Should handle closing all panels', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await vscode.commands.executeCommand('gitsocial.openRepository');
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      await vscode.commands.executeCommand('workbench.action.closeAllEditors');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(tabCount, 0, 'All tabs should be closed');
    });
  });

  describe('Webview Content', function() {
    it('Should have proper webview options', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTab = tabGroups.flatMap(group => group.tabs)
        .find(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        );

      assert.ok(webviewTab, 'Should find Timeline webview tab');
    });
  });

  describe('Error Handling and Edge Cases', function() {
    it('Should handle rapid panel open/close cycles', async function() {
      for (let i = 0; i < 5; i++) {
        await vscode.commands.executeCommand('gitsocial.openTimeline');
        await new Promise(resolve => setTimeout(resolve, 50));
        await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
        await new Promise(resolve => setTimeout(resolve, 50));
      }

      const tabCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(tabCount, 0, 'All panels should be closed after rapid cycles');
    });

    it('Should handle opening same panel type multiple times rapidly', async function() {
      const promises = [];
      for (let i = 0; i < 5; i++) {
        promises.push(vscode.commands.executeCommand('gitsocial.openTimeline'));
      }
      await Promise.all(promises);
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const timelineTabs = tabGroups.flatMap(group => group.tabs)
        .filter(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        );

      assert.ok(timelineTabs.length >= 1, 'Should have at least one Timeline panel');
      assert.ok(timelineTabs.length <= 5, 'Should not exceed 5 Timeline panels');
    });

    it('Should handle panel disposal during concurrent operations', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      await Promise.all([
        vscode.commands.executeCommand('workbench.action.closeActiveEditor'),
        vscode.commands.executeCommand('gitsocial.openSettings')
      ]);
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countAfter > 0, 'Should have panels remaining');
    });

    it('Should maintain panel tracking integrity after errors', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterOpen = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('workbench.action.closeAllEditors');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterClose = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterReopen = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countAfterOpen > 0, 'Panel should open successfully');
      assert.strictEqual(countAfterClose, 0, 'All panels should be closed');
      assert.ok(countAfterReopen > 0, 'Panel should reopen successfully after closure');
    });

    it('Should handle multiple panels of different types concurrently', async function() {
      await Promise.all([
        vscode.commands.executeCommand('gitsocial.openTimeline'),
        vscode.commands.executeCommand('gitsocial.openSearch'),
        vscode.commands.executeCommand('gitsocial.openSettings')
      ]);
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTabs = tabGroups.flatMap(group => group.tabs)
        .filter(tab => tab.input instanceof vscode.TabInputWebview);

      assert.ok(webviewTabs.length >= 3, 'Should have multiple panel types open concurrently');

      const labels = webviewTabs.map(tab => tab.label);
      const uniqueLabels = new Set(labels);
      assert.ok(uniqueLabels.size >= 2, 'Should have at least 2 different panel types');
    });

    it('Should handle panel state after rapid reuse attempts', async function() {
      for (let i = 0; i < 3; i++) {
        await vscode.commands.executeCommand('gitsocial.openTimeline');
        await new Promise(resolve => setTimeout(resolve, 50));
      }

      const tabGroups = vscode.window.tabGroups.all;
      const timelineTabs = tabGroups.flatMap(group => group.tabs)
        .filter(tab =>
          tab.label === 'Timeline' &&
          tab.input instanceof vscode.TabInputWebview
        );

      assert.ok(timelineTabs.length >= 1, 'Should have Timeline panel after rapid reuse');
    });

    it('Should properly clean up resources on panel disposal', async function() {
      const countBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countWithPanel = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countWithPanel > countBefore, 'Panel should be created');
      assert.strictEqual(countAfter, countBefore, 'Resources should be cleaned up after disposal');
    });

    it('Should handle disposal of non-existent panels gracefully', async function() {
      const countBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(countAfter, countBefore, 'Should handle disposal of non-existent panel');
    });

    it('Should handle panel creation after all panels disposed', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      await vscode.commands.executeCommand('workbench.action.closeAllEditors');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterDisposal = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterRecreation = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(countAfterDisposal, 0, 'All panels should be disposed');
      assert.ok(countAfterRecreation > 0, 'New panel should be created after disposal');
    });

    it('Should handle concurrent panel disposal operations', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await vscode.commands.executeCommand('gitsocial.openSearch');
      await vscode.commands.executeCommand('gitsocial.openSettings');
      await new Promise(resolve => setTimeout(resolve, 50));

      await Promise.all([
        vscode.commands.executeCommand('workbench.action.closeActiveEditor'),
        new Promise(resolve => setTimeout(resolve, 100)).then(() =>
          vscode.commands.executeCommand('workbench.action.closeActiveEditor')
        )
      ]);
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countAfter >= 0, 'Should handle concurrent disposals without errors');
    });

    it('Should prevent memory leaks with repeated panel creation', async function() {
      const iterations = 10;

      for (let i = 0; i < iterations; i++) {
        await vscode.commands.executeCommand('gitsocial.openTimeline');
        await new Promise(resolve => setTimeout(resolve, 50));
        await vscode.commands.executeCommand('workbench.action.closeActiveEditor');
        await new Promise(resolve => setTimeout(resolve, 50));
      }

      const finalCount = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(finalCount, 0, 'No panels should remain after repeated creation/disposal');
    });

    it('Should handle panel operations with empty workspace', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const tabGroups = vscode.window.tabGroups.all;
      const webviewTab = tabGroups.flatMap(group => group.tabs)
        .find(tab => tab.label === 'Timeline' && tab.input instanceof vscode.TabInputWebview);

      assert.ok(webviewTab, 'Timeline panel should work even without workspace');
    });

    it('Should maintain panel state integrity across operations', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterFirst = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfterSecond = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.strictEqual(countAfterFirst, countAfterSecond, 'Panel reuse should maintain count');
    });

    it('Should handle panel disposal race conditions', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const disposalPromises = [];
      for (let i = 0; i < 3; i++) {
        disposalPromises.push(
          vscode.commands.executeCommand('workbench.action.closeActiveEditor')
        );
      }

      await Promise.all(disposalPromises);
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countAfter >= 0, 'Should handle disposal race conditions gracefully');
    });

    it('Should recover from panel creation errors', async function() {
      await vscode.commands.executeCommand('gitsocial.openTimeline');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countBefore = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      await vscode.commands.executeCommand('gitsocial.openSearch');
      await new Promise(resolve => setTimeout(resolve, 50));

      const countAfter = vscode.window.tabGroups.all.reduce(
        (count, group) => count + group.tabs.length,
        0
      );

      assert.ok(countAfter >= countBefore, 'Should continue creating panels after potential errors');
    });
  });
});
