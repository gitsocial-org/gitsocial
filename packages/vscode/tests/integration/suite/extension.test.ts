import * as assert from 'assert';
import * as vscode from 'vscode';

describe('Extension Test Suite', function() {
  void vscode.window.showInformationMessage('Running extension tests');

  it('Extension should be present', function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    assert.ok(ext, 'Extension should be installed');
  });

  it('Extension should activate', async function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    assert.ok(ext, 'Extension should exist');

    await ext.activate();
    assert.strictEqual(ext.isActive, true, 'Extension should be active');
  });

  it('Extension should have correct package metadata', function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    assert.ok(ext, 'Extension should exist');

    const packageJson = ext.packageJSON as { name: string; displayName: string; publisher: string };
    assert.strictEqual(packageJson.name, 'gitsocial');
    assert.strictEqual(packageJson.displayName, 'GitSocial');
    assert.strictEqual(packageJson.publisher, 'gitsocial');
  });
});
