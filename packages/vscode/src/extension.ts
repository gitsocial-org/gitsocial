import * as vscode from 'vscode';
import { SidebarProvider } from './SidebarProvider';
import { WebviewManager } from './WebviewManager';
import { getLogLevel, git, log, type LogLevel, social } from '@gitsocial/core';
import { initializeAvatarSystem } from './avatar';

let webviewManager: WebviewManager;
let globalStorageUri: vscode.Uri | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  // Configure logger based on VS Code settings - with detailed debugging
  const config = vscode.workspace.getConfiguration('gitsocial');
  const workspaceConfig = vscode.workspace.getConfiguration('gitsocial', vscode.workspace.workspaceFolders?.[0]?.uri);
  const globalConfig = vscode.workspace.getConfiguration('gitsocial', null);

  const debugLevel = config.get<LogLevel>('debug', 'off');
  const workspaceDebugLevel = workspaceConfig.get<LogLevel>('debug', 'off');
  const globalDebugLevel = globalConfig.get<LogLevel>('debug', 'off');

  // Set environment variable first so logging can work properly
  const configInspect = config.inspect<LogLevel>('debug');
  const finalDebugLevel = configInspect?.workspaceFolderValue ??
                         configInspect?.workspaceValue ??
                         configInspect?.globalValue ??
                         configInspect?.defaultValue ??
                         'off';

  process.env.GITSOCIAL_LOG_LEVEL = finalDebugLevel;

  // Detailed configuration debugging - console.log needed when logging system may not work
  console.log('[GitSocial Debug] Config sources:'); // eslint-disable-line no-console
  console.log('  - Default config debug level:', debugLevel); // eslint-disable-line no-console
  console.log('  - Workspace config debug level:', workspaceDebugLevel); // eslint-disable-line no-console
  console.log('  - Global config debug level:', globalDebugLevel); // eslint-disable-line no-console
  console.log('  - Config inspect:', config.inspect('debug')); // eslint-disable-line no-console

  console.log('[GitSocial Debug] Using precedence - workspaceFolderValue:', configInspect?.workspaceFolderValue); // eslint-disable-line no-console
  console.log('[GitSocial Debug] Using precedence - workspaceValue:', configInspect?.workspaceValue); // eslint-disable-line no-console
  console.log('[GitSocial Debug] Using precedence - globalValue:', configInspect?.globalValue); // eslint-disable-line no-console

  // Debug the environment variable setting
  console.log('[GitSocial Debug] Final debug level used:', finalDebugLevel); // eslint-disable-line no-console
  console.log('[GitSocial Debug] Environment GITSOCIAL_LOG_LEVEL:', process.env.GITSOCIAL_LOG_LEVEL); // eslint-disable-line no-console
  console.log('[GitSocial Debug] Logger getCurrentLogLevel():', getLogLevel()); // eslint-disable-line no-console

  log('info', 'GitSocial extension activated');
  log('info', `Debug level set to: ${debugLevel}`);
  log('debug', 'This is a test debug message - you should see this if debug logging is working');

  // Store global storage URI for access by handlers
  globalStorageUri = context.globalStorageUri;

  // Initialize avatar system with extension context
  await initializeAvatarSystem(context);

  // Initialize repository system with extension storage
  if (globalStorageUri && globalStorageUri.fsPath) {
    social.repository.initialize({
      storageBase: globalStorageUri.fsPath
    });
    social.list.initialize({
      storageBase: globalStorageUri.fsPath
    });
    social.log.initialize({
      storageBase: globalStorageUri.fsPath
    });
    log('debug', 'Repository, social, lists, and logs systems initialized with storage:', globalStorageUri.fsPath);

    // Initialize global cache at startup (only for git repositories)
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (workspaceFolder) {
      try {
        // Check if workspace is a git repository before initializing cache
        const isGitRepo = await git.isGitRepository(workspaceFolder.uri.fsPath);

        if (isGitRepo) {
          // Initialize cache with 30 days of workspace posts for My Repository
          // External repos still load from current week for performance
          const thirtyDaysAgo = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000);
          thirtyDaysAgo.setHours(0, 0, 0, 0);

          log('debug', '[Extension] Initializing global cache with data from:', thirtyDaysAgo.toISOString());
          await social.cache.initializeGlobalCache(workspaceFolder.uri.fsPath, globalStorageUri.fsPath, thirtyDaysAgo);
          log('debug', '[Extension] Global cache initialized successfully');
        } else {
          log('debug', '[Extension] Workspace is not a git repository, skipping cache initialization');
        }
      } catch (error) {
        log('error', 'Failed to initialize global cache:', error);
      }
    }
  }

  // Initialize WebviewManager
  webviewManager = new WebviewManager(context.extensionUri);

  // Register sidebar provider
  const sidebarProvider = new SidebarProvider(context.extensionUri, webviewManager);
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider('gitsocial.sidebar', sidebarProvider)
  );

  // Wire up the sidebar provider reference in WebviewManager
  webviewManager.setSidebarProvider(sidebarProvider);

  // Register commands for opening panels
  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.openTimeline', () => {
      webviewManager.openPanel('timeline', 'Timeline');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.openRepository', () => {
      const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
      if (workspaceFolder) {
        webviewManager.openPanel('repository', 'My Repository', {
          path: workspaceFolder.uri.fsPath
        });
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.openNotifications', () => {
      webviewManager.openPanel('notifications', 'Notifications');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.openSearch', () => {
      webviewManager.openPanel('search', 'Search');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.openSettings', () => {
      webviewManager.openPanel('settings', 'Settings');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.createPost', () => {
      webviewManager.openPanel('createPost', 'Create Post');
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('gitsocial.initialize', () => {
      webviewManager.openPanel('welcome', 'Welcome to GitSocial');
    })
  );

  // Listen for configuration changes
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(e => {
      if (e.affectsConfiguration('gitsocial.debug')) {
        const config = vscode.workspace.getConfiguration('gitsocial');
        const debugLevel = config.get<LogLevel>('debug', 'off');

        // Update environment variable for logging
        process.env.GITSOCIAL_LOG_LEVEL = debugLevel;

        log('info', `Debug level changed to: ${debugLevel}`);
      }
      if (e.affectsConfiguration('gitsocial.enableGravatar')) {
        const config = vscode.workspace.getConfiguration('gitsocial');
        const enableGravatar = config.get<boolean>('enableGravatar', false);
        social.avatar.setEnableGravatar(enableGravatar);
      }
    })
  );

  // Check if we're in a git repository after a delay
  setTimeout(() => {
    void checkGitRepository();
  }, 1000);
}

export function deactivate(): void {
  log( 'info', 'GitSocial extension deactivated');
}

async function getUserPreference(workdir: string): Promise<string | null> {
  try {
    const result = await git.execGit(workdir, ['config', '--local', '--get', 'gitsocial.userPreference']);
    if (result.success && result.data) {
      return result.data.stdout.trim();
    }
  } catch (error) {
    log('debug', '[getUserPreference] No preference found:', error);
  }
  return null;
}

async function setUserPreference(workdir: string, value: string): Promise<void> {
  try {
    await git.execGit(workdir, ['config', '--local', 'gitsocial.userPreference', value]);
    log('info', `[setUserPreference] Set preference to: ${value}`);
  } catch (error) {
    log('error', '[setUserPreference] Failed to set preference:', error);
  }
}

async function showGetStartedNotification(workdir: string): Promise<void> {
  const action = await vscode.window.showInformationMessage(
    'Get started with GitSocial',
    'Initialize',
    'Not Now',
    'Never'
  );

  switch (action) {
  case 'Initialize':
    await setUserPreference(workdir, 'initialized');
    webviewManager.openPanel('welcome', 'Welcome to GitSocial');
    break;
  case 'Not Now':
    await setUserPreference(workdir, 'later');
    break;
  case 'Never':
    await setUserPreference(workdir, 'dismissed');
    break;
  }
}

async function checkGitRepository(): Promise<void> {
  const workspaceFolders = vscode.workspace.workspaceFolders;

  // If no workspace, do nothing
  if (!workspaceFolders || workspaceFolders.length === 0) {
    log('info', 'No workspace folders found');
    return;
  }

  try {
    const workspacePath = workspaceFolders[0].uri.fsPath;

    // First check if it's a git repository
    const isGitRepo = await git.isGitRepository(workspacePath);

    if (!isGitRepo) {
      log('info', 'Workspace is not a git repository');
      return;
    }

    // It's a git repository, now check GitSocial initialization
    const initResult = await social.repository.checkGitSocialInit(workspacePath);

    if (initResult.success && initResult.data) {
      if (initResult.data.isInitialized) {
        // Repository is initialized, open Timeline
        log('info', 'GitSocial repository detected, opening Timeline');
        webviewManager.openPanel('timeline', 'Timeline');
        return;
      } else {
        // Repository exists but not initialized - check user preference
        const preference = await getUserPreference(workspacePath);

        if (preference === 'dismissed' || preference === 'later') {
          log('info', `User preference is '${preference}', not showing notification`);
          return;
        }

        // No preference or preference is 'initialized' but not actually initialized
        // Show notification
        log('info', 'Git repository not initialized for GitSocial, showing notification');
        await showGetStartedNotification(workspacePath);
        return;
      }
    } else {
      log('info', 'Could not determine GitSocial status');
    }
  } catch (error) {
    console.error('Error checking GitSocial repository:', error);
  }
}

/**
 * Get the global storage URI
 */
export function getStorageUri(): vscode.Uri | undefined {
  return globalStorageUri;
}
