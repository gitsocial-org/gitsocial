import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from './';
import {
  git,
  gitMsgRef,
  type List,
  log,
  type Post,
  type Repository,
  social,
  storage
} from '@gitsocial/core';
import { fetchTimeManager } from '../utils/fetchTime';
import { getStorageUri } from '../extension';
import { getUnpushedCounts } from '../utils/unpushedCounts';

// Message types for repository operations
export type RepositoryMessages =
  | { type: 'initializeRepository'; id?: string; params: { config: unknown } }
  | { type: 'checkGitSocialInit'; id?: string }
  | { type: 'fetchRepositories'; id?: string }
  | { type: 'getRepositories'; id?: string; scope: string }
  | { type: 'fetchSpecificRepositories'; id?: string; repositoryIds: string[]; since?: string }
  | { type: 'fetchListRepositories'; id?: string; listId: string; repository?: string }
  | { type: 'fetchUpdates'; id?: string; repository: string }
  | { type: 'addRepository'; id?: string; listId: string; repository: string; branch?: string }
  | { type: 'removeRepository'; id?: string; listId: string; repository: string }
  | { type: 'checkRepositoryStatus'; id?: string; repository?: string }
  | { type: 'getUnpushedCounts'; id?: string }
  | { type: 'getUnpushedListsCount'; id?: string }
  | { type: 'pushToRemote'; id?: string; remoteName?: string };

// Response types for repository operations
export type RepositoryResponses =
  | { type: 'repositoryInitialized'; data: { message: string; config: unknown; branchCreated?: boolean; branchName?: string }; requestId?: string }
  | { type: 'initializationError'; data: { message: string }; requestId?: string }
  | { type: 'gitSocialStatus'; data: { isInitialized: boolean; config?: unknown }; requestId?: string }
  | { type: 'fetchProgress'; data: { status: string; repository?: string; message?: string }; requestId?: string }
  | { type: 'fetchCompleted'; data: { repositories: string[]; failed: string[] }; requestId?: string }
  | { type: 'repositoryAdded'; data: { message: string; list: string }; requestId?: string }
  | { type: 'repositoryRemoved'; data: { message: string; list: string }; requestId?: string }
  | { type: 'repositoryStatus'; data: Repository; requestId?: string }
  | { type: 'unpushedCounts'; data: { posts: number; comments: number; total: number }; requestId?: string }
  | { type: 'unpushedListsCount'; data: number; requestId?: string }
  | { type: 'pushProgress'; data: { status: string; message?: string }; requestId?: string }
  | { type: 'pushCompleted'; data: { message: string; pushed: number }; requestId?: string };

// Helper for broadcasting to all panels
let broadcastToAll: ((message: { type: string }) => void) | undefined;

export function setRepositoryCallbacks(
  broadcast: (message: { type: string }) => void
): void {
  broadcastToAll = broadcast;
}

// Register initialize repository handler
registerHandler('initializeRepository', async function handleInitializeRepository(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'initializeRepository') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'initializeRepository' }>;
    if (!msg.params?.config) {
      throw new Error('Configuration is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const config = msg.params.config as {
      id?: string;
      workspaceUri?: string;
      branch?: string;
      remote?: string;
    };

    // Determine configuration based on branch choice
    const wantsGitSocialBranch = config.branch === 'gitsocial';
    const branchName = config.branch || undefined;

    // Check if it's a git repository, initialize if not
    const isGitRepo = await git.isGitRepository(workspaceFolder.uri.fsPath);
    if (!isGitRepo) {
      // Initialize git with the appropriate initial branch
      const initialBranch = branchName;
      log('info', `Initializing git repository${initialBranch ? ` with branch ${initialBranch}` : ''}`);
      const gitInitResult = await git.initGitRepository(workspaceFolder.uri.fsPath, initialBranch);
      if (!gitInitResult.success) {
        throw new Error('Failed to initialize git repository');
      }
    }

    // Check if repository is empty (no commits)
    const headResult = await git.execGit(workspaceFolder.uri.fsPath, ['rev-parse', 'HEAD']);
    const isEmptyRepo = !headResult.success;

    // Handle branch configuration
    if (branchName) {
      // Check if branch exists locally
      const localBranchExists = await git.execGit(workspaceFolder.uri.fsPath, [
        'rev-parse',
        '--verify',
        `refs/heads/${branchName}`
      ]);

      if (!localBranchExists.success) {
        // Branch doesn't exist locally, check if it exists remotely
        const remoteBranchExists = await git.execGit(workspaceFolder.uri.fsPath, [
          'rev-parse',
          '--verify',
          `refs/remotes/origin/${branchName}`
        ]);

        if (remoteBranchExists.success) {
          // Remote branch exists, create local tracking branch
          log('info', `Creating local tracking branch for origin/${branchName}`);
          const createTrackingResult = await git.execGit(workspaceFolder.uri.fsPath, [
            'checkout',
            '-b',
            branchName,
            `origin/${branchName}`
          ]);

          if (!createTrackingResult.success) {
            throw new Error(`Failed to create tracking branch for ${branchName}`);
          }
        } else {
          // Branch doesn't exist locally or remotely - will be created
          if (isEmptyRepo) {
            // For empty repositories, set the default branch
            await git.execGit(workspaceFolder.uri.fsPath, ['symbolic-ref', 'HEAD', `refs/heads/${branchName}`]);
          }
        }
      }

      // Initialize with explicit branch name
      const needsToCreateBranch = wantsGitSocialBranch && isGitRepo;
      const result = await social.repository.initializeRepository(
        workspaceFolder.uri.fsPath,
        {
          createBranch: needsToCreateBranch,
          branchName: branchName
        }
      );

      if (!result.success) {
        throw new Error(result.error?.message || 'Failed to initialize repository');
      }
    } else {
      // No branch specified - use auto-detection
      if (isEmptyRepo) {
        // For empty repositories, set the default branch to main
        await git.execGit(workspaceFolder.uri.fsPath, ['symbolic-ref', 'HEAD', 'refs/heads/main']);
      }

      const result = await social.repository.initializeRepository(workspaceFolder.uri.fsPath);

      if (!result.success) {
        throw new Error(result.error?.message || 'Failed to initialize repository');
      }
    }

    // Send success response
    const finalBranchName = branchName || 'main';
    postMessage(panel, 'repositoryInitialized', {
      message: 'Repository initialized successfully!',
      branchCreated: wantsGitSocialBranch,
      branchName: finalBranchName
    }, requestId);

    // Show success message with branch info
    let successMessage = 'GitSocial initialized successfully!';
    if (branchName) {
      successMessage += ` Using branch: ${branchName}`;
    }
    void vscode.window.showInformationMessage(successMessage);

    // Notify all panels to refresh
    if (broadcastToAll) {
      broadcastToAll({ type: 'refresh' });
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'initializationError', {
      message: error instanceof Error ? error.message : 'Failed to initialize repository'
    }, requestId);
  }
});

// Register check GitSocial init handler
registerHandler('checkGitSocialInit', async function handleCheckGitSocialInit(panel, message) {
  const requestId = message.id || undefined;

  try {
    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // First check if this is even a git repository
    const isGitRepo = await git.isGitRepository(workspaceFolder.uri.fsPath);

    if (!isGitRepo) {
      // Not a git repository - send status indicating it needs initialization
      postMessage(panel, 'gitSocialStatus', {
        isInitialized: false,
        currentBranch: 'Not a git repository',
        setupType: 'Requires initialization',
        config: null,
        detectedDefaultBranch: 'main',
        gitSocialBranchExists: false,
        branches: [],
        configuredBranch: null
      }, requestId);
      return;
    }

    // Check GitSocial initialization
    const result = await social.repository.checkGitSocialInit(workspaceFolder.uri.fsPath);

    if (result.success) {
      // Get current branch information - handle empty repos gracefully
      const branchResult = await git.getCurrentBranch(workspaceFolder.uri.fsPath);
      let currentBranch: string;
      let setupType: string;
      let detectedDefaultBranch = 'main';

      if (branchResult.success && branchResult.data) {
        currentBranch = branchResult.data;
        detectedDefaultBranch = branchResult.data;
        setupType = currentBranch === 'gitsocial' ? 'GitSocial branch' : 'Main branch';
      } else {
        // Empty repository - check what the default branch will be
        const headResult = await git.execGit(workspaceFolder.uri.fsPath, ['symbolic-ref', 'HEAD']);
        if (headResult.success && headResult.data?.stdout) {
          const defaultBranch = headResult.data.stdout.replace('refs/heads/', '').trim();
          currentBranch = `${defaultBranch} (empty)`;
          detectedDefaultBranch = defaultBranch;
          setupType = defaultBranch === 'gitsocial' ? 'GitSocial branch' : 'Main branch';
        } else {
          // Try to detect from origin/HEAD
          const originHeadResult = await git.execGit(workspaceFolder.uri.fsPath, ['symbolic-ref', 'refs/remotes/origin/HEAD']);
          if (originHeadResult.success && originHeadResult.data?.stdout) {
            detectedDefaultBranch = originHeadResult.data.stdout.trim().replace('refs/remotes/origin/', '');
          }
          currentBranch = 'Empty repository';
          setupType = 'Requires first commit';
        }
      }

      // Check if gitsocial branch exists
      const gitSocialBranchResult = await git.execGit(workspaceFolder.uri.fsPath, [
        'rev-parse',
        '--verify',
        'refs/heads/gitsocial'
      ]);
      const gitSocialBranchExists = gitSocialBranchResult.success;

      // Fetch all local branches
      const localBranchesResult = await git.execGit(workspaceFolder.uri.fsPath, [
        'for-each-ref',
        '--format=%(refname:short)',
        'refs/heads/'
      ]);
      const localBranches = localBranchesResult.success && localBranchesResult.data?.stdout
        ? localBranchesResult.data.stdout.trim().split('\n').filter(b => b.length > 0)
        : [];

      // Fetch all remote branches
      const remoteBranchesResult = await git.execGit(workspaceFolder.uri.fsPath, [
        'for-each-ref',
        '--format=%(refname:short)',
        'refs/remotes/origin/'
      ]);
      const remoteBranches = remoteBranchesResult.success && remoteBranchesResult.data?.stdout
        ? remoteBranchesResult.data.stdout.trim().split('\n').filter(b => b.length > 0).map(b => b.replace('origin/', '')).filter(b => b !== 'HEAD')
        : [];

      // Build branch list with location info
      const branchSet = new Set([...localBranches, ...remoteBranches]);
      const branches = Array.from(branchSet).map(branchName => {
        const hasLocal = localBranches.includes(branchName);
        const hasRemote = remoteBranches.includes(branchName);
        let location: 'local' | 'remote' | 'both';
        if (hasLocal && hasRemote) {
          location = 'both';
        } else if (hasLocal) {
          location = 'local';
        } else {
          location = 'remote';
        }
        return {
          name: branchName,
          location,
          isCurrent: branchName === currentBranch
        };
      });

      // Sort: current first, then local branches, then remote-only
      branches.sort((a, b) => {
        if (a.isCurrent) {return -1;}
        if (b.isCurrent) {return 1;}
        if (a.location === 'local' && b.location === 'remote') {return -1;}
        if (a.location === 'remote' && b.location === 'local') {return 1;}
        if (a.location === 'both' && b.location !== 'both') {return -1;}
        if (b.location === 'both' && a.location !== 'both') {return 1;}
        return a.name.localeCompare(b.name);
      });

      // Get configured branch from result
      const configuredBranch = result.data?.branch || null;

      // Send status response
      postMessage(panel, 'gitSocialStatus', {
        isInitialized: result.data?.isInitialized || false,
        currentBranch,
        setupType,
        branch: result.data?.branch,
        detectedDefaultBranch,
        gitSocialBranchExists,
        branches,
        configuredBranch
      }, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to check GitSocial status');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to check GitSocial status'
    }, requestId);
  }
});

// Register fetch repositories handler
registerHandler('fetchRepositories', async function handleFetchRepositories(panel, message) {
  const requestId = message.id || undefined;

  try {
    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Show progress notification
    await vscode.window.withProgress({
      location: vscode.ProgressLocation.Notification,
      title: 'Fetching repositories...',
      cancellable: false
    }, async (_progress) => {
      // Fetch updates for all following repositories
      const fetchResult = await social.repository.fetchUpdates(
        workspaceFolder.uri.fsPath,
        'following'
      );

      if (fetchResult.success && fetchResult.data) {
        const { fetched, failed } = fetchResult.data;

        // Fetch time is automatically updated in isolated clones by fetchRepository

        // Refresh entire cache after fetching all following repositories
        // Using 'all: true' ensures both workspace and external repositories are loaded
        try {
          const storageUri = getStorageUri();
          await social.cache.refresh({ all: true }, workspaceFolder.uri.fsPath, storageUri?.fsPath);
          log('debug', '[fetchAllRepositories] Full cache refreshed after fetch');
        } catch (error) {
          log('error', '[fetchAllRepositories] Failed to refresh cache:', error);
        }

        // No longer need to broadcast repository updates or refresh
        // Views will get updated data when they request posts again
        // Keep refreshAfterFetch for compatibility with other views that may need it
        if (broadcastToAll) {
          broadcastToAll({ type: 'refreshAfterFetch' });
        }

        // Send completion response
        postMessage(panel, 'fetchCompleted', {
          repositories: Array(fetched).fill('repository'),
          failed: Array(failed).fill('repository')
        }, requestId);

        // Show completion message
        if (failed > 0) {
          void vscode.window.showWarningMessage(
            `Fetched ${fetched} repositories, ${failed} failed`
          );
        } else {
          void vscode.window.showInformationMessage(
            `Successfully fetched ${fetched} repositories`
          );
        }
      } else {
        throw new Error(fetchResult.error?.message || 'Failed to fetch repositories');
      }
    });
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to fetch repositories'
    }, requestId);
  }
});

// Register fetch specific repositories handler
registerHandler('fetchSpecificRepositories', async function handleFetchSpecificRepositories(panel, message) {
  const requestId = message.id || undefined;
  const msg = message as Extract<RepositoryMessages, { type: 'fetchSpecificRepositories' }>;
  const repositoryIds = msg.repositoryIds || [];
  const since = msg.since;

  try {
    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    if (repositoryIds.length === 0) {
      throw new Error('No repositories specified for fetching');
    }

    log('debug', '[fetchSpecificRepositories] Starting fetch for repositories:', { repositoryIds });

    // Show progress notification
    await vscode.window.withProgress({
      location: vscode.ProgressLocation.Notification,
      title: `Fetching ${repositoryIds.length} repositories...`,
      cancellable: false
    }, async (_progress) => {
      let totalFetched = 0;
      let totalFailed = 0;

      // Fetch each repository individually
      for (const repositoryId of repositoryIds) {
        try {
          // Validate repository ID
          if (!repositoryId || typeof repositoryId !== 'string') {
            log('error', '[fetchSpecificRepositories] Invalid repository ID:', { repositoryId });
            totalFailed++;
            continue;
          }

          // Parse repository ID to extract URL and branch
          const parsed = gitMsgRef.parseRepositoryId(repositoryId);
          // Use the full repository ID (with branch) in the scope to preserve branch info
          const fetchResult = await social.repository.fetchUpdates(
            workspaceFolder.uri.fsPath,
            `repository:${repositoryId}`,  // Full ID includes #branch:branchname
            {
              branch: parsed.branch,
              since: since
            }
          );

          if (fetchResult.success && fetchResult.data) {
            totalFetched += fetchResult.data.fetched;
            totalFailed += fetchResult.data.failed;
          } else {
            totalFailed++;
            log('error', `[fetchSpecificRepositories] Failed to fetch ${repositoryId}:`, fetchResult.error);
          }
        } catch (error) {
          totalFailed++;
          log('error', `[fetchSpecificRepositories] Error fetching ${repositoryId}:`, error);
        }
      }

      // Refresh cache after fetching specific repositories
      try {
        const storageUri = getStorageUri();
        await social.cache.refresh({ all: true }, workspaceFolder.uri.fsPath, storageUri?.fsPath);
        log('debug', '[fetchSpecificRepositories] Cache refreshed after specific fetch');
      } catch (error) {
        log('error', '[fetchSpecificRepositories] Failed to refresh cache:', error);
      }

      // No longer need to broadcast repository updates or refresh
      // The Timeline will get updated data when it requests posts again

      // Send completion response
      postMessage(panel, 'fetchCompleted', {
        repositories: Array(totalFetched).fill('repository'),
        failed: Array(totalFailed).fill('repository')
      }, requestId);

      // Show completion message
      if (totalFailed > 0) {
        void vscode.window.showWarningMessage(
          `Fetched ${totalFetched} repositories, ${totalFailed} failed`
        );
      } else {
        void vscode.window.showInformationMessage(
          `Successfully fetched ${totalFetched} repositories`
        );
      }
    });
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to fetch specific repositories'
    }, requestId);
  }
});

// Register fetch updates handler for specific repository
registerHandler('fetchUpdates', async function handleFetchUpdates(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'fetchUpdates') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'fetchUpdates' }>;

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const workdir = workspaceFolder.uri.fsPath;
    const isWorkspace = !msg.repository || msg.repository === 'workspace:my';

    // Send progress notification
    postMessage(panel, 'fetchProgress', {
      status: 'fetching',
      repository: msg.repository,
      message: isWorkspace ? 'Fetching workspace updates...' : 'Fetching repository updates...'
    }, requestId);

    // Show system progress notification
    await vscode.window.withProgress({
      location: vscode.ProgressLocation.Notification,
      title: isWorkspace ? 'Fetching workspace updates...' : 'Fetching repository updates...',
      cancellable: false
    }, async (_progress) => {
      if (isWorkspace) {
        // Workspace fetch: fetch from origin directly
        const gitSocialBranch = await git.getConfiguredBranch(workdir);

        const fetchResult = await git.fetchRemote(workdir, 'origin', { branch: gitSocialBranch });

        if (!fetchResult.success) {
          throw new Error(fetchResult.error?.message || 'Failed to fetch from origin');
        }

        // Refresh cache for workspace repository
        try {
          await social.cache.refresh({ repositories: [workdir] }, workdir);
          log('debug', '[fetchUpdates] Workspace cache refreshed after fetch');
        } catch (error) {
          log('error', '[fetchUpdates] Failed to refresh workspace cache:', error);
        }

        // Send completion response
        postMessage(panel, 'fetchProgress', {
          status: 'completed',
          message: 'Workspace updated successfully'
        }, requestId);

        // Show completion message
        void vscode.window.showInformationMessage('Successfully fetched workspace updates');

        // Refresh all open views to show updated content
        if (broadcastToAll) {
          broadcastToAll({ type: 'refreshAfterFetch' });
        }
      } else {
        // External repository fetch: use isolated storage
        const fetchResult = await social.repository.fetchUpdates(
          workdir,
          `repository:${msg.repository}`
        );

        if (fetchResult.success && fetchResult.data) {
          const { fetched, failed } = fetchResult.data;

          // Refresh entire cache after fetching the repository
          // Using 'all: true' ensures both workspace and external repositories are loaded
          try {
            const storageUri = getStorageUri();
            await social.cache.refresh({ all: true }, workdir, storageUri?.fsPath);
            log('debug', '[fetchUpdates] Full cache refreshed after fetch');
          } catch (error) {
            log('error', '[fetchUpdates] Failed to refresh cache:', error);
          }

          // Send completion response
          postMessage(panel, 'fetchProgress', {
            status: 'completed',
            repository: msg.repository,
            message: 'Repository updated successfully'
          }, requestId);

          // Show completion message
          if (failed > 0) {
            void vscode.window.showWarningMessage(
              `Fetched ${fetched} repositories, ${failed} failed`
            );
          } else {
            void vscode.window.showInformationMessage(
              `Successfully fetched ${fetched} repositories`
            );
          }

          // Refresh all open views to show updated content - force cache skip after fetch
          if (broadcastToAll) {
            broadcastToAll({ type: 'refreshAfterFetch' });
          }
        } else {
          throw new Error(fetchResult.error?.message || 'Failed to fetch repository updates');
        }
      }
    });
  } catch (error) {
    // Send error response
    postMessage(panel, 'fetchProgress', {
      status: 'error',
      message: error instanceof Error ? error.message : 'Failed to fetch updates'
    }, requestId);

    void vscode.window.showErrorMessage(
      error instanceof Error ? error.message : 'Failed to fetch repository updates'
    );
  }
});

// Register fetch list repositories handler
registerHandler('fetchListRepositories', async function handleFetchListRepositories(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'fetchListRepositories') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'fetchListRepositories' }>;

    if (!msg.listId) {
      throw new Error('List ID is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    log('debug', '[Handlers] Fetching repositories for list:', msg.listId, 'from repository:', msg.repository || 'workspace');

    let listData: List | null = null;

    if (msg.repository) {
      // Remote repository list: get the list from the remote repository
      const listsResult = await social.list.getLists(msg.repository, workspaceFolder.uri.fsPath);
      if (!listsResult.success || !listsResult.data) {
        throw new Error(`Failed to get lists from repository ${msg.repository}`);
      }

      listData = listsResult.data.find(l => l.id === msg.listId) || null;
      if (!listData) {
        throw new Error(`List ${msg.listId} not found in repository ${msg.repository}`);
      }
    } else {
      // Workspace list: get the list from the workspace
      const listResult = await social.list.getList(workspaceFolder.uri.fsPath, msg.listId);
      if (!listResult.success || !listResult.data) {
        throw new Error(`List ${msg.listId} not found`);
      }
      listData = listResult.data;
    }

    if (listData.repositories.length === 0) {
      // No repositories to fetch
      postMessage(panel, 'fetchProgress', {
        status: 'completed',
        message: 'No repositories in list'
      }, requestId);
      return;
    }

    // Send progress update
    postMessage(panel, 'fetchProgress', {
      status: 'fetching',
      message: `Fetching updates for ${listData.repositories.length} repositories`
    }, requestId);

    // Show system progress notification
    await vscode.window.withProgress({
      location: vscode.ProgressLocation.Notification,
      title: 'Fetching list updates...',
      cancellable: false
    }, async (_progress) => {
      // Fetch updates for all repositories in the list using the new architecture
      let fetched = 0;
      let failed = 0;
      const fetchedRepositories: string[] = [];

      for (const repoId of listData.repositories) {
        const parsed = gitMsgRef.parseRepositoryId(repoId);
        if (!parsed) {
          failed++;
          continue;
        }

        // Fetch using the isolated repository system
        const fetchResult = await social.repository.fetchUpdates(
          workspaceFolder.uri.fsPath,
          `repository:${parsed.repository}`
        );

        if (fetchResult.success && fetchResult.data) {
          fetched += fetchResult.data.fetched;
          failed += fetchResult.data.failed;
          fetchedRepositories.push(parsed.repository);
        } else {
          failed++;
        }
      }

      // Fetch time is automatically updated in each isolated clone by fetchRepository

      // Refresh entire cache if any repositories were fetched
      if (fetchedRepositories.length > 0) {
        try {
          const storageUri = getStorageUri();
          await social.cache.refresh({ all: true }, workspaceFolder.uri.fsPath, storageUri?.fsPath);
          log('debug', '[fetchListRepositories] Full cache refreshed after fetch');
        } catch (error) {
          log('error', '[fetchListRepositories] Failed to refresh cache:', error);
        }
      }

      // Refresh all open views to show updated data - force cache skip after fetch
      if (broadcastToAll) {
        broadcastToAll({ type: 'refreshAfterFetch' });
      }

      // Send completion message
      postMessage(panel, 'fetchProgress', {
        status: 'completed',
        message: `Fetched ${fetched} repositories${failed > 0 ? `, ${failed} failed` : ''}`
      }, requestId);

      // Show system completion message
      if (failed > 0) {
        void vscode.window.showWarningMessage(
          `Fetched ${fetched} repositories, ${failed} failed`
        );
      } else {
        void vscode.window.showInformationMessage(
          `Successfully fetched ${fetched} repositories`
        );
      }
    });

  } catch (error) {
    console.error('[Handlers] Error in handleFetchListRepositories:', error);
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to fetch repositories'
    }, requestId);
  }
});

// Register add repository handler
registerHandler('addRepository', async function handleAddRepository(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'addRepository') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'addRepository' }>;
    if (!msg.listId || !msg.repository) {
      throw new Error('List ID and repository URL are required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    log('info', `[handleAddRepository] Adding repository '${msg.repository}' to list '${msg.listId}'`);

    // Use the proper addRepositoryToList function from social namespace
    const result = await social.list.addRepositoryToList(
      workspaceFolder.uri.fsPath,
      msg.listId,
      msg.repository
    );

    if (result.success) {
      postMessage(panel, 'repositoryAdded', {
        message: 'Repository added successfully! Fetching posts...',
        list: msg.listId
      }, requestId);

      // addRepositoryToList already refreshed cache with the repository posts
      // Just trigger UI refresh to show the new posts immediately
      broadcastToAll?.({ type: 'refreshAfterFetch' });
      void vscode.window.showInformationMessage('Repository added successfully! Posts should now appear in timeline.');

    } else {
      const errorMessage = result.error?.message || 'Failed to add repository';
      const errorCode = result.error?.code || 'UNKNOWN_ERROR';
      log('error', `[handleAddRepository] Failed to add repository: ${errorCode} - ${errorMessage}`);

      // Provide more specific error messages
      let userMessage = errorMessage;
      if (errorCode === 'LIST_NOT_FOUND') {
        userMessage = 'The specified list does not exist. Please create it first.';
      }

      throw new Error(userMessage);
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : 'Failed to add repository';
    log('error', `[handleAddRepository] Exception: ${errorMessage}`);

    // Send error response
    postMessage(panel, 'error', {
      message: errorMessage
    }, requestId);

    // Show error notification
    void vscode.window.showErrorMessage(`Failed to add repository: ${errorMessage}`);
  }
});

// Register remove repository handler
registerHandler('removeRepository', async function handleRemoveRepository(panel, message) {
  const requestId = message.id || undefined;

  try {
    log('info', '[removeRepository] Handler called', { requestId });

    if (message.type !== 'removeRepository') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'removeRepository' }>;

    log('info', '[removeRepository] Request details:', {
      listId: msg.listId,
      repository: msg.repository,
      requestId
    });

    if (!msg.listId || !msg.repository) {
      throw new Error('List ID and repository URL are required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    log('debug', `[removeRepository] Using workspace: ${workspaceFolder.uri.fsPath}`);

    // Use the proper removeRepositoryFromList function from social namespace
    log('info', `[removeRepository] Calling social.list.removeRepositoryFromList('${workspaceFolder.uri.fsPath}', '${msg.listId}', '${msg.repository}')`);
    const result = await social.list.removeRepositoryFromList(
      workspaceFolder.uri.fsPath,
      msg.listId,
      msg.repository
    );

    log('info', '[removeRepository] social.list.removeRepositoryFromList result:', {
      success: result.success,
      error: result.error
    });

    if (result.success) {
      log('info', '[removeRepository] Operation succeeded, clearing cache and broadcasting');

      // Refresh cache to ensure fresh data after list change
      try {
        await social.cache.refresh({ lists: ['*'] }, workspaceFolder.uri.fsPath);
        log('debug', '[removeRepository] Cache refreshed after list change');
      } catch (error) {
        log('error', '[removeRepository] Failed to refresh cache:', error);
      }

      // Send success response
      postMessage(panel, 'repositoryRemoved', {
        message: 'Repository removed successfully!',
        list: msg.listId
      }, requestId);

      // Refresh all open views to reflect the removal
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }
    } else {
      log('error', '[removeRepository] Operation failed:', result.error);
      throw new Error(result.error?.message || 'Failed to remove repository');
    }
  } catch (error) {
    log('error', '[removeRepository] Handler exception:', error);
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to remove repository'
    }, requestId);
  }
});

// Register check repository status handler
registerHandler('checkRepositoryStatus', async function handleGetRepositoryRelationship(panel, message) {
  const requestId = message.id || undefined;
  const msg = message as Extract<RepositoryMessages, { type: 'checkRepositoryStatus' }>;
  log('info', '[checkRepositoryStatus] Handler called with repository:', msg.repository || 'undefined');

  try {
    if (message.type !== 'checkRepositoryStatus') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'checkRepositoryStatus' }>;
    const targetRepository = msg.repository;

    // Allow empty/dot for workspace repository
    if (!targetRepository || targetRepository === '.') {
      // For workspace, use workspace path as target
      const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
      if (!workspaceFolder) {
        throw new Error('No workspace folder found');
      }
      const targetRepo = targetRepository === '.' ? '' : (targetRepository || '');

      // Get repository relationship for workspace
      const result = await social.repository.getRepositoryRelationship(
        workspaceFolder.uri.fsPath,
        targetRepo  // Empty string will be treated as workspace
      );

      if (result.success && result.data) {
        const repository = result.data;

        // For workspace repos, load config and check for origin remote
        if (repository.type === 'workspace') {
          // Get GitSocial branch configuration
          const gitSocialBranch = await git.getConfiguredBranch(workspaceFolder.uri.fsPath);
          repository.branch = gitSocialBranch;
          repository.config = {
            branch: gitSocialBranch,
            social: {
              enabled: true,
              branch: gitSocialBranch
            }
          };

          const remotesResult = await git.listRemotes(workspaceFolder.uri.fsPath);
          if (remotesResult.success && remotesResult.data) {
            const originRemote = remotesResult.data.find(r => r.name === 'origin');
            repository.hasOriginRemote = !!originRemote;
            repository.originUrl = originRemote?.url || undefined;

            // Get fetch time from origin if it exists
            if (originRemote) {
              const fetchTime = await fetchTimeManager.get(workspaceFolder.uri.fsPath, 'origin');
              if (fetchTime) {
                repository.lastFetchTime = fetchTime;
              }
            }
          }
        }

        postMessage(panel, 'repositoryStatus', repository, requestId);
      } else {
        throw new Error(result.error?.message || 'Failed to check repository status');
      }
      return;
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Get repository relationship for UI
    const result = await social.repository.getRepositoryRelationship(
      workspaceFolder.uri.fsPath,
      targetRepository
    );

    if (result.success && result.data) {
      // Use the Repository object directly from getRepositoryRelationship
      const repository: Repository = result.data;

      // If it's a remote repository, get its fetch time
      if (repository.type === 'other') {
        // First try to get from fetchTimeManager if remoteName exists
        if (repository.remoteName) {
          log('debug', `[checkRepositoryStatus] Getting fetch time for remote: ${repository.remoteName}`);
          const fetchTime = await fetchTimeManager.get(workspaceFolder.uri.fsPath, repository.remoteName);
          if (fetchTime) {
            repository.lastFetchTime = fetchTime;
            log('debug', `[checkRepositoryStatus] Got fetch time from manager: ${fetchTime.toISOString()}`);
          }
        }

        // If no fetch time yet and we have a URL, try reading from isolated repository's git config
        if (!repository.lastFetchTime && repository.url) {
          const storageUri = getStorageUri();
          if (storageUri) {
            try {
              // Get the isolated repository path
              const repoPath = storage.path.getDirectory(storageUri.fsPath, repository.url);
              log('debug', `[checkRepositoryStatus] Checking git config at: ${repoPath}`);

              // Read the git config to get lastFetch
              const config = await storage.repository.readConfig(repoPath);
              if (config && config.lastFetch) {
                repository.lastFetchTime = new Date(config.lastFetch);
                log('debug', `[checkRepositoryStatus] Got fetch time from git config: ${config.lastFetch}`);
              } else {
                log('debug', `[checkRepositoryStatus] No fetch time in git config for: ${repository.url}`);
              }
            } catch (error) {
              log('debug', `[checkRepositoryStatus] Error reading git config: ${String(error)}`);
            }
          }
        }
      }

      // Send repository status
      postMessage(panel, 'repositoryStatus', repository, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to check repository status');
    }
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to check repository status'
    }, requestId);
  }
});

// Register get unpushed counts handler
registerHandler('getUnpushedCounts', async function handleGetUnpushedCounts(panel, message) {
  const requestId = message.id || undefined;

  try {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Use the shared utility to get unpushed counts
    const counts = await getUnpushedCounts(workspaceFolder.uri.fsPath);

    postMessage(panel, 'unpushedCounts', counts, requestId);
  } catch (error) {
    console.error('[GitSocial] Error getting unpushed counts:', error);
    postMessage(panel, 'unpushedCounts', { posts: 0, comments: 0, total: 0 }, requestId);
  }
});

// Register get unpushed lists count handler
registerHandler('getUnpushedListsCount', async function handleGetUnpushedListsCount(panel, message) {
  const requestId = message.id || undefined;

  try {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Get unpushed lists count
    const count = await social.list.getUnpushedListsCount(workspaceFolder.uri.fsPath);

    postMessage(panel, 'unpushedListsCount', count, requestId);
  } catch (error) {
    console.error('[GitSocial] Error getting unpushed lists count:', error);
    postMessage(panel, 'unpushedListsCount', 0, requestId);
  }
});

// Register push to remote handler
registerHandler('pushToRemote', async function handlePushToRemote(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'pushToRemote') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<RepositoryMessages, { type: 'pushToRemote' }>;
    const remoteName = msg.remoteName || 'origin';

    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Send initial progress
    postMessage(panel, 'pushProgress', {
      status: 'pushing',
      message: `Pushing to ${remoteName}...`
    }, requestId);

    // Check for divergence and auto-merge BEFORE validation
    const workdir = workspaceFolder.uri.fsPath;
    const gitSocialBranch = await git.getConfiguredBranch(workdir);
    const originBranchRef = `${remoteName}/${gitSocialBranch}`;

    // Check if origin/branch exists
    const originBranchResult = await git.execGit(workdir, [
      'rev-parse', '--verify', '--quiet', originBranchRef
    ]);

    if (originBranchResult.success) {
      // Check divergence
      const divergenceResult = await git.execGit(workdir, [
        'rev-list', '--left-right', '--count', `${originBranchRef}...${gitSocialBranch}`
      ]);

      if (divergenceResult.success && divergenceResult.data) {
        const output = divergenceResult.data.stdout.trim();
        const parts = output.split('\t').map(Number);
        const behind = parts[0];
        const ahead = parts[1];

        if (typeof behind === 'number' && typeof ahead === 'number' && behind > 0 && ahead > 0) {
          // Auto-sync: fetch + merge
          postMessage(panel, 'pushProgress', {
            status: 'syncing',
            message: `Syncing with remote (${behind} new posts)...`
          }, requestId);

          log('info', `[pushToRemote] Auto-syncing: ${ahead} local, ${behind} remote`);

          // Fetch from origin
          const fetchResult = await git.fetchRemote(workdir, remoteName, { branch: gitSocialBranch });
          if (!fetchResult.success) {
            throw new Error(`Failed to fetch: ${fetchResult.error?.message || 'Unknown error'}`);
          }

          // Merge origin/branch
          const mergeResult = await git.mergeBranch(workdir, originBranchRef);
          if (!mergeResult.success) {
            throw new Error(`Failed to merge: ${mergeResult.error?.message || 'Unexpected merge failure'}`);
          }

          log('info', '[pushToRemote] Auto-sync successful');

          // Refresh cache after merge
          await social.cache.refresh({ repositories: [workdir] }, workdir);

          // Update progress message - now pushing
          postMessage(panel, 'pushProgress', {
            status: 'pushing',
            message: `Pushing to ${remoteName}...`
          }, requestId);
        }
      }
    }

    // Validate push preconditions (will now pass after auto-merge)
    const validationResult = await git.validatePushPreconditions(workdir, remoteName);
    if (!validationResult.success) {
      throw new Error(validationResult.error?.message || 'Push validation failed');
    }

    // Show progress notification
    await vscode.window.withProgress({
      location: vscode.ProgressLocation.Notification,
      title: `Pushing to ${remoteName}...`,
      cancellable: false
    }, async (_progress) => {
      // Reuse gitSocialBranch and originBranchRef from above (already computed)
      // Check if origin/branch exists to determine if this is first push
      const originBranchCheckResult = await git.execGit(workdir, [
        'rev-parse',
        '--verify',
        '--quiet',
        originBranchRef
      ]);
      const isFirstPush = !originBranchCheckResult.success;

      // Build push arguments
      const pushArgs = ['push'];
      if (isFirstPush) {
        pushArgs.push('--set-upstream');
      }
      pushArgs.push(remoteName, `refs/heads/${gitSocialBranch}:refs/heads/${gitSocialBranch}`);

      // Push posts (main refs)
      const pushPostsResult = await git.execGit(workdir, pushArgs);

      if (!pushPostsResult.success) {
        throw new Error(pushPostsResult.error?.message || 'Failed to push posts');
      }

      // Push lists (refs/gitmsg/social/lists/*)
      const pushListsResult = await git.execGit(workdir, [
        'push',
        remoteName,
        'refs/gitmsg/social/lists/*:refs/gitmsg/social/lists/*'
      ]);

      // Count what we're pushing before clearing cache
      const postsResult = social.post.getPosts(workspaceFolder.uri.fsPath, 'repository:my', { limit: 1000 }) ;
      const unpushedPosts = postsResult.success && postsResult.data
        ? postsResult.data.filter((p: Post) => p.display.isUnpushed)
        : [];
      const postsCount = unpushedPosts.filter((p: Post) => p.type === 'post' || p.type === 'quote' || p.type === 'repost').length;
      const commentsCount = unpushedPosts.filter((p: Post) => p.type === 'comment').length;
      const listsCount = await social.list.getUnpushedListsCount(workspaceFolder.uri.fsPath);

      // Lists push can fail if there are no lists, which is okay
      if (!pushListsResult.success) {
        log('debug', 'No lists to push or lists push failed:', pushListsResult.error);
      }

      // Refresh cache for my repository after push
      try {
        await social.cache.refresh({ repositories: [workspaceFolder.uri.fsPath], lists: ['*'] }, workspaceFolder.uri.fsPath);
        log('debug', '[pushRepository] Cache refreshed after push');
      } catch (error) {
        log('error', '[pushRepository] Failed to refresh cache:', error);
      }

      // Send completion message (push successful)
      postMessage(panel, 'pushCompleted', {
        message: 'Successfully pushed to remote',
        pushed: postsCount + commentsCount + listsCount
      }, requestId);

      // Broadcast refresh to update UI
      if (broadcastToAll) {
        broadcastToAll({ type: 'refresh' });
      }

      // Build detailed success message
      const parts: string[] = [];
      if (postsCount > 0) {parts.push(`${postsCount} post${postsCount !== 1 ? 's' : ''}`);}
      if (commentsCount > 0) {parts.push(`${commentsCount} comment${commentsCount !== 1 ? 's' : ''}`);}
      if (listsCount > 0) {parts.push(`${listsCount} list${listsCount !== 1 ? 's' : ''}`);}

      const message = parts.length > 0
        ? `Successfully pushed ${parts.join(', ')} to ${remoteName}`
        : `Successfully pushed to ${remoteName}`;

      void vscode.window.showInformationMessage(message);
    });
  } catch (error) {
    // Send error response
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to push to remote'
    }, requestId);

    void vscode.window.showErrorMessage(
      error instanceof Error ? error.message : 'Failed to push to remote'
    );
  }
});

// Register get repositories handler
registerHandler('getRepositories', async function handleGetRepositories(panel, message) {
  const requestId = message.id || undefined;
  const msg = message as Extract<RepositoryMessages, { type: 'getRepositories' }>;

  try {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Get repositories based on scope
    const result = await social.repository.getRepositories(
      workspaceFolder.uri.fsPath,
      msg.scope
    );

    if (result.success && result.data) {
      // Add fetch times for each repository
      for (const repo of result.data) {
        if (repo.remoteName) {
          const fetchTime = await fetchTimeManager.get(workspaceFolder.uri.fsPath, repo.remoteName);
          if (fetchTime) {
            repo.lastFetchTime = fetchTime;
          }
        }
      }

      // Send repositories data
      postMessage(panel, 'repositories', result.data, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to get repositories');
    }
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get repositories'
    }, requestId);
  }
});
