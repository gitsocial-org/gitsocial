import * as assert from 'assert';
import * as vscode from 'vscode';

describe('Commands Test Suite', function() {
  before(async function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    if (ext && !ext.isActive) {
      await ext.activate();
    }
  });

  it('Should register all commands', async function() {
    const commands = await vscode.commands.getCommands(true);

    const expectedCommands = [
      'gitsocial.openTimeline',
      'gitsocial.openRepository',
      'gitsocial.openSearch',
      'gitsocial.openSettings',
      'gitsocial.createPost',
      'gitsocial.initialize'
    ];

    for (const cmd of expectedCommands) {
      assert.ok(
        commands.includes(cmd),
        `Command ${cmd} should be registered`
      );
    }
  });

  it('Commands should execute without error', async function() {
    const commandsToTest = [
      'gitsocial.openTimeline',
      'gitsocial.openRepository',
      'gitsocial.openSearch',
      'gitsocial.openSettings'
    ];

    for (const cmd of commandsToTest) {
      try {
        await vscode.commands.executeCommand(cmd);
        assert.ok(true, `Command ${cmd} executed successfully`);
      } catch (error) {
        assert.fail(`Command ${cmd} failed: ${String(error)}`);
      }
    }
  });
});
