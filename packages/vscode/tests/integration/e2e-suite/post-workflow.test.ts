import * as assert from 'assert';
import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { execSync } from 'child_process';

function logTabsForDebug(): void {
  console.error('Current tabs:', vscode.window.tabGroups.all.flatMap(g =>
    g.tabs.map(t => ({ label: t.label, type: t.input?.constructor.name }))
  ));
}

async function waitForTab(label: string | RegExp, timeout = 5000): Promise<boolean> {
  const startTime = Date.now();

  return new Promise((resolve) => {
    const checkTab = (): boolean | null => {
      const tabGroups = vscode.window.tabGroups.all;
      const hasTab = tabGroups.some(group =>
        group.tabs.some(tab => {
          const matches = typeof label === 'string'
            ? tab.label === label
            : label.test(tab.label);
          return matches && tab.input instanceof vscode.TabInputWebview;
        })
      );

      if (hasTab) {
        return true;
      }

      if (Date.now() - startTime > timeout) {
        return false;
      }

      return null;
    };

    const result = checkTab();
    if (result !== null) {
      resolve(result);
      return;
    }

    const disposable = vscode.window.tabGroups.onDidChangeTabs(() => {
      const result = checkTab();
      if (result !== null) {
        disposable.dispose();
        resolve(result);
      }
    });

    setTimeout(() => {
      disposable.dispose();
      resolve(false);
    }, timeout);
  });
}

describe('E2E: Post Creation Workflow', function() {
  this.timeout(60000);

  let workspaceDir: string;

  before(async function() {
    const ext = vscode.extensions.getExtension('gitsocial.gitsocial');
    if (ext && !ext.isActive) {
      await ext.activate();
    }

    workspaceDir = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || '';
    assert.ok(workspaceDir, 'Workspace directory should be available');

    try {
      execSync('git init', { cwd: workspaceDir, stdio: 'pipe' });
    } catch (error) {
      if (!fs.existsSync(path.join(workspaceDir, '.git'))) {
        const message = error instanceof Error ? error.message : String(error);
        console.error('Failed to initialize git repository:', message);
        throw error;
      }
    }

    try {
      execSync('git config user.name "E2E Test User"', { cwd: workspaceDir, stdio: 'pipe' });
      execSync('git config user.email "e2e@test.com"', { cwd: workspaceDir, stdio: 'pipe' });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error('Failed to set git config:', message);
      try {
        const currentConfig = execSync('git config --list', {
          cwd: workspaceDir,
          encoding: 'utf8'
        });
        // eslint-disable-next-line no-console
        console.log('Current git config:', currentConfig);
      } catch {
        // Ignore error if git config --list fails
      }
      throw error;
    }
  });

  afterEach(async function() {
    await vscode.commands.executeCommand('workbench.action.closeAllEditors');
  });

  describe('Post Creation', function() {
    it('should open welcome view when initializing', async function() {
      try {
        await vscode.commands.executeCommand('gitsocial.initialize');
        const hasWelcomeView = await waitForTab('Welcome to GitSocial');

        assert.ok(hasWelcomeView, 'Welcome view should open when initializing');
      } catch (error) {
        console.error('Initialize error:', error);
        logTabsForDebug();
        throw error;
      }
    });

    it('should open timeline view', async function() {
      try {
        await vscode.commands.executeCommand('gitsocial.openTimeline');
        const hasTimelineView = await waitForTab('Timeline');

        assert.ok(hasTimelineView, 'Timeline view should be open');
      } catch (error) {
        console.error('Open timeline error:', error);
        logTabsForDebug();
        throw error;
      }
    });

    it('should have create post command available', async function() {
      const commands = await vscode.commands.getCommands();
      assert.ok(
        commands.includes('gitsocial.createPost'),
        'createPost command should be available'
      );
    });

    it('should be able to execute create post command', async function() {
      try {
        await vscode.commands.executeCommand('gitsocial.createPost');
        const hasCreatePostView = await waitForTab(/^(Create Post|New Post)$/);

        assert.ok(hasCreatePostView, 'Create Post view should open');
      } catch (error) {
        console.error('Create post error:', error);
        logTabsForDebug();
        const errorMessage = error instanceof Error ? error.message : String(error);
        assert.fail(`createPost command should not throw error: ${errorMessage}`);
      }
    });
  });

  describe('Repository State', function() {
    it('should have git repository initialized', function() {
      const gitDir = path.join(workspaceDir, '.git');
      assert.ok(fs.existsSync(gitDir), 'Git repository should exist');
    });

    it('should have git config set', function() {
      try {
        const userName = execSync('git config user.name', {
          cwd: workspaceDir,
          encoding: 'utf8'
        }).trim();

        const userEmail = execSync('git config user.email', {
          cwd: workspaceDir,
          encoding: 'utf8'
        }).trim();

        assert.ok(userName, 'Git user.name should be set');
        assert.ok(userEmail, 'Git user.email should be set');
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : String(error);
        assert.fail(`Git config should be readable: ${errorMessage}`);
      }
    });
  });
});
