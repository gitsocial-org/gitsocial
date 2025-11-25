import * as assert from 'assert';
import * as vscode from 'vscode';

describe('Configuration Test Suite', function() {
  let config: vscode.WorkspaceConfiguration;

  beforeEach(function() {
    config = vscode.workspace.getConfiguration('gitsocial');
  });

  it('Should read default debug level', function() {
    const debugLevel = config.get('debug');
    assert.ok(debugLevel !== undefined, 'Debug level should be defined');
    assert.strictEqual(debugLevel, 'off', 'Default debug level should be off');
  });

  it('Should read default cache max size', () => {
    const cacheMaxSize = config.get<number>('cacheMaxSize');
    assert.ok(cacheMaxSize !== undefined, 'Cache max size should be defined');
    assert.strictEqual(cacheMaxSize, 100000, 'Default cache max size should be 100000');
  });

  it('Should read default Gravatar setting', () => {
    const enableGravatar = config.get<boolean>('enableGravatar');
    assert.ok(enableGravatar !== undefined, 'Gravatar setting should be defined');
    assert.strictEqual(enableGravatar, false, 'Gravatar should be disabled by default');
  });

  it('Should read default auto load images setting', () => {
    const autoLoadImages = config.get<boolean>('autoLoadImages');
    assert.ok(autoLoadImages !== undefined, 'Auto load images setting should be defined');
    assert.strictEqual(autoLoadImages, true, 'Auto load images should be enabled by default');
  });

  it('Should read default explore setting', () => {
    const enableExplore = config.get<boolean>('enableExplore');
    assert.ok(enableExplore !== undefined, 'Explore setting should be defined');
    assert.strictEqual(enableExplore, true, 'Explore should be enabled by default');
  });

  it('Should have correct configuration section', () => {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    assert.ok(ext, 'Extension should exist');

    const packageJSON = ext.packageJSON as { contributes: { configuration: { title: string } } };
    const contributes = packageJSON.contributes;
    assert.ok(contributes.configuration, 'Should have configuration section');
    assert.strictEqual(contributes.configuration.title, 'GitSocial');
  });

  it('Configuration properties should have correct types', () => {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    type PackageConfig = {
      contributes: {
        configuration: {
          properties: Record<string, { type: string }>
        }
      }
    };
    const packageJSON = ext?.packageJSON as PackageConfig;
    const properties = packageJSON.contributes.configuration.properties;

    assert.strictEqual(properties['gitsocial.debug'].type, 'string');
    assert.strictEqual(properties['gitsocial.cacheMaxSize'].type, 'number');
    assert.strictEqual(properties['gitsocial.enableGravatar'].type, 'boolean');
    assert.strictEqual(properties['gitsocial.autoLoadImages'].type, 'boolean');
    assert.strictEqual(properties['gitsocial.enableExplore'].type, 'boolean');
  });
});
