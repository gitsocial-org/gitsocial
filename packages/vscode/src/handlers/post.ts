import * as vscode from 'vscode';
import { registerHandler } from './registry';
import { postMessage } from '.';
import {
  git,
  gitMsgRef,
  log,
  type Post,
  social
} from '@gitsocial/core';
import { getStorageUri } from '../extension';

/**
 * Build standardized options for getPosts calls
 */
interface GetPostsOptions {
  types?: Array<'post' | 'comment' | 'repost' | 'quote'>;
  since?: Date;
  until?: Date;
  limit?: number;
  skipCache?: boolean;
  storageBase?: string;
}

function buildGetPostsOptions(msg: Extract<PostsMessages, { type: 'social.getPosts' }>): GetPostsOptions {
  return {
    types: msg.options?.types === 'all' ? undefined : msg.options?.types as Array<'post' | 'comment' | 'repost' | 'quote'> | undefined,
    since: msg.options?.since ? new Date(msg.options.since) : undefined,
    until: msg.options?.until ? new Date(msg.options.until) : undefined,
    limit: msg.options?.limit,
    skipCache: msg.options?.skipCache,
    storageBase: getStorageUri()?.fsPath
  };
}

/**
 * Determine if a repository identifier is a URL
 */
function isRepositoryUrl(repository: string): boolean {
  return repository.includes('#') ||
         repository.startsWith('http://') ||
         repository.startsWith('https://') ||
         repository.includes('git@');
}

/**
 * Prepare repository data for the requested date range
 * Unified function for all repository types
 */
async function prepareRepositoryData(
  workspaceFolder: string,
  storageBase: string | undefined,
  repository: string | undefined,
  scope: string,
  options: { since?: string, until?: string }
): Promise<void> {
  // Skip if no date range requested
  if (!options.since || !options.until || !storageBase) {
    return;
  }

  const since = new Date(options.since);

  // Determine repository URL from either repository param or scope
  let repoUrl: string | null = null;

  if (repository && isRepositoryUrl(repository)) {
    repoUrl = repository;
  } else if (scope.startsWith('repository:') && scope !== 'repository:my') {
    const scopeRepo = scope.replace('repository:', '');
    if (isRepositoryUrl(scopeRepo)) {
      repoUrl = scopeRepo;
    }
  }

  // Case 1: External repository
  if (repoUrl && repoUrl.includes('#')) {
    log('debug', '[prepareRepositoryData] External repository, ensuring data for date range:', {
      repository: repoUrl,
      since: options.since,
      until: options.until
    });

    const parsed = gitMsgRef.parseRepositoryId(repoUrl);
    const ensureResult = await social.repository.ensureDataForDateRange(
      workspaceFolder,
      storageBase,
      parsed.repository,
      parsed.branch,
      since,
      { isPersistent: false }  // External repos are temporary
    );

    if (!ensureResult.success) {
      log('error', '[prepareRepositoryData] Failed to ensure data for external repository:', {
        repository: parsed.repository,
        error: ensureResult.error
      });
      throw new Error(ensureResult.error?.message || 'Failed to ensure repository data');
    }
  }
  // Case 2: Workspace repository
  else if (scope === 'repository:my' && options.since) {
    log('debug', '[prepareRepositoryData] Workspace repository, checking if historical data needed:', {
      since: options.since
    });

    if (!social.cache.isCacheRangeCovered(since)) {
      log('debug', '[prepareRepositoryData] Loading historical data for workspace repository');
      await social.cache.loadAdditionalPosts(
        workspaceFolder,
        storageBase,
        since
      );
    }
  }
}

// Message types for post operations
export type PostsMessages =
  | {
      type: 'social.getPosts';
      id?: string;
      options?: {
        scope?: string;
        listId?: string;
        types?: string | string[];
        limit?: number;
        since?: string;
        until?: string;
        repository?: string;
        skipCache?: boolean;
      };
    }
  | {
      type: 'social.createPost';
      id?: string;
      content: string;
      parentId?: string;
    }
  | {
      type: 'social.searchPosts';
      id?: string;
      query: string;
      options?: {
        types?: string[];
        scope?: string;
        limit?: number;
      };
    };

export type PostsResponses =
  | { type: 'posts'; data: Post[]; id?: string }
  | { type: 'postCreated'; data: { post: Post }; id?: string }
  | { type: 'searchResults'; data: { posts: Post[] }; id?: string };

let broadcastToAll: ((message: PostsResponses) => void) | undefined;

export function setBroadcast(broadcast: (message: PostsResponses) => void): void {
  broadcastToAll = broadcast;
}

// Register get posts handler - SIMPLIFIED VERSION
registerHandler('social.getPosts', async function handleGetPosts(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'social.getPosts') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<PostsMessages, { type: 'social.getPosts' }>;

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const storageUri = getStorageUri();
    const storageBase = storageUri?.fsPath;

    // Special case 1: Timeline scope with date range
    if (msg.options?.scope === 'timeline' && msg.options?.since && msg.options?.until) {
      log('debug', '[handleGetPosts] Timeline scope with date range');

      const timelineResult = await social.timeline.getWeekPosts(
        workspaceFolder.uri.fsPath,
        storageBase,
        new Date(msg.options.since),
        new Date(msg.options.until),
        buildGetPostsOptions(msg)
      );

      if (timelineResult.success && timelineResult.data) {
        postMessage(panel, 'posts', timelineResult.data.posts, requestId);
        if (timelineResult.data.repositories) {
          postMessage(panel, 'repositories', timelineResult.data.repositories);
        }
        return;
      }
      throw new Error(timelineResult.error?.message || 'Failed to get timeline posts');
    }

    // Special case 2: List-specific posts
    if (msg.options?.listId) {
      log('debug', '[handleGetPosts] List-specific posts:', msg.options.listId);

      // Prepare data for all list repositories if date range specified
      if (msg.options?.since && msg.options?.until && storageBase) {
        const listResult = await social.list.getList(
          workspaceFolder.uri.fsPath,
          msg.options.listId
        );

        if (listResult.success && listResult.data?.repositories) {
          for (const repoId of listResult.data.repositories) {
            try {
              const parsed = gitMsgRef.parseRepositoryId(repoId);
              const result = await social.repository.ensureDataForDateRange(
                workspaceFolder.uri.fsPath,
                storageBase,
                parsed.repository,
                parsed.branch,
                new Date(msg.options.since),
                { isPersistent: true }  // List repositories are persisted
              );

              if (!result.success) {
                log('error', '[handleGetPosts] Failed to ensure data for list repository:', {
                  repository: parsed.repository,
                  error: result.error
                });
              }
            } catch (error) {
              log('error', '[handleGetPosts] Error processing list repository:', repoId, error);
            }
          }
        }
      }

      // Get posts for the list
      const postsResult = await social.post.getPosts(
        workspaceFolder.uri.fsPath,
        `list:${msg.options.listId}`,
        buildGetPostsOptions(msg)
      );

      if (postsResult.success && postsResult.data) {
        postMessage(panel, 'posts', postsResult.data, requestId);
        return;
      }
      throw new Error(postsResult.error?.message || 'Failed to get list posts');
    }

    // Special case 3: Remote repository list (repository:URL/list:listId)
    if (msg.options?.scope?.includes('/list:')) {
      const match = msg.options.scope.match(/^repository:(.+)\/list:(.+)$/);
      if (match) {
        const [, repoUrl, listId] = match;
        log('debug', '[handleGetPosts] Remote repository list:', { repoUrl, listId });

        // Get the list from the remote repository
        const listsResult = await social.list.getLists(repoUrl, workspaceFolder.uri.fsPath);
        if (!listsResult.success || !listsResult.data) {
          throw new Error(`Failed to get lists from repository ${repoUrl}`);
        }

        const listData = listsResult.data.find(l => l.id === listId);
        if (!listData) {
          throw new Error(`List ${listId} not found in repository ${repoUrl}`);
        }

        log('debug', '[handleGetPosts] Processing remote list:', {
          listId,
          repositories: listData.repositories.length
        });

        // Prepare data for all list repositories if date range specified
        if (msg.options?.since && msg.options?.until && storageBase) {
          for (const repoId of listData.repositories) {
            try {
              const parsed = gitMsgRef.parseRepositoryId(repoId);
              const result = await social.repository.ensureDataForDateRange(
                workspaceFolder.uri.fsPath,
                storageBase,
                parsed.repository,
                parsed.branch,
                new Date(msg.options.since),
                {
                  isPersistent: true  // List repositories are persisted
                }
              );

              if (!result.success) {
                log('error', '[handleGetPosts] Failed to ensure data for remote list repository:', {
                  repository: parsed.repository,
                  error: result.error
                });
              }
            } catch (error) {
              log('error', '[handleGetPosts] Error processing remote list repository:', repoId, error);
            }
          }
        }

        // Get posts for the list, passing the list data directly as context
        const postsResult = await social.post.getPosts(
          workspaceFolder.uri.fsPath,
          `list:${listId}`,
          buildGetPostsOptions(msg),
          { list: listData }  // Pass remote list data directly
        );

        if (postsResult.success && postsResult.data) {
          postMessage(panel, 'posts', postsResult.data, requestId);
          return;
        }
        throw new Error(postsResult.error?.message || 'Failed to get remote list posts');
      }
    }

    // Main case: Regular repository or scope
    const scope = msg.options?.scope ||
                  (msg.options?.repository ? `repository:${msg.options.repository}` : 'repository:my');

    // Prepare repository data if needed
    await prepareRepositoryData(
      workspaceFolder.uri.fsPath,
      storageBase,
      msg.options?.repository,
      scope,
      msg.options || {}
    );

    // Get posts
    const result = await social.post.getPosts(
      workspaceFolder.uri.fsPath,
      scope,
      buildGetPostsOptions(msg)
    );

    if (result.success && result.data) {
      // Log interaction counts for debugging
      const postsWithCounts = result.data.filter((p: Post) =>
        (p.interactions?.comments || 0) > 0 ||
        (p.interactions?.reposts || 0) > 0 ||
        (p.interactions?.quotes || 0) > 0
      );

      if (postsWithCounts.length > 0) {
        log('debug', '[handleGetPosts] Posts with interaction counts:', postsWithCounts.length);
      } else {
        log('debug', '[handleGetPosts] No posts have interaction counts in result');
      }

      postMessage(panel, 'posts', result.data, requestId);
    } else {
      throw new Error(result.error?.message || 'Failed to get posts');
    }

  } catch (error) {
    log('error', '[handleGetPosts] Error in handler:', error);
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to get posts'
    }, requestId);
  }
});

// Register create post handler
registerHandler('social.createPost', async function handleCreatePost(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'social.createPost') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<PostsMessages, { type: 'social.createPost' }>;
    if (!msg.content) {
      throw new Error('Post content is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    // Check if it's a git repository, initialize if not
    const isGitRepo = await git.isGitRepository(workspaceFolder.uri.fsPath);
    if (!isGitRepo) {
      log('info', '[createPost] Directory is not a git repository, initializing git with gitsocial branch');
      const gitInitResult = await git.initGitRepository(workspaceFolder.uri.fsPath, 'gitsocial');
      if (!gitInitResult.success) {
        throw new Error('Failed to initialize git repository: ' + (gitInitResult.error?.message || 'Unknown error'));
      }
      log('info', '[createPost] Git repository initialized successfully');
    }
    // Check if GitSocial is initialized, if not, initialize automatically
    const initResult = await social.repository.checkGitSocialInit(workspaceFolder.uri.fsPath);
    if (initResult.success && initResult.data && !initResult.data.isInitialized) {
      log('info', '[createPost] Repository not initialized, initializing automatically');
      const autoInitResult = await social.repository.initializeRepository(workspaceFolder.uri.fsPath);
      if (!autoInitResult.success) {
        throw new Error('Failed to initialize GitSocial: ' + (autoInitResult.error?.message || 'Unknown error'));
      }
      log('info', '[createPost] Repository initialized successfully');
    }

    // Create post based on whether it's a parent or child
    let result;
    if (msg.parentId) {
      // For comments, we need to get the parent post first
      const parentPostResult = await social.post.getPosts(
        workspaceFolder.uri.fsPath,
        `post:${msg.parentId}`,
        { limit: 1 }
      );

      if (!parentPostResult.success || !parentPostResult.data || parentPostResult.data.length === 0) {
        throw new Error(`Parent post ${msg.parentId} not found`);
      }

      const parentPost = parentPostResult.data[0];
      result = await social.interaction.createInteraction(
        'comment',
        workspaceFolder.uri.fsPath,
        parentPost,
        msg.content
      );
    } else {
      result = await social.post.createPost(
        workspaceFolder.uri.fsPath,
        msg.content
      );
    }

    if (result.success && result.data) {
      // Send response to requesting panel
      postMessage(panel, 'postCreated', { post: result.data }, requestId);

      // Broadcast to all panels
      if (broadcastToAll) {
        broadcastToAll({
          type: 'postCreated',
          data: { post: result.data }
        });
      }
    } else {
      const errorMessage = result.error?.message || 'Failed to create post';
      const errorDetails = result.error?.details ? JSON.stringify(result.error.details) : 'No details';
      console.error('[GitSocial] Create post failed:', errorMessage, 'Details:', errorDetails);
      throw new Error(errorMessage);
    }
  } catch (error) {
    console.error('[GitSocial] handleCreatePost error:', error);
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Failed to create post'
    }, requestId);
  }
});

// Register search posts handler
registerHandler('social.searchPosts', async function handleSearchPosts(panel, message) {
  const requestId = message.id || undefined;

  try {
    if (message.type !== 'social.searchPosts') {
      throw new Error('Invalid message type');
    }
    const msg = message as Extract<PostsMessages, { type: 'social.searchPosts' }>;
    if (!msg.query) {
      throw new Error('Search query is required');
    }

    // Get workspace folder
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
      throw new Error('No workspace folder found');
    }

    const result = await social.search.searchPosts(
      workspaceFolder.uri.fsPath,
      {
        query: msg.query,
        filters: msg.options?.types ? {
          interactionType: msg.options.types as Array<'post' | 'comment' | 'repost' | 'quote'>
        } : undefined,
        limit: msg.options?.limit
      }
    );

    if (result.success && result.data) {
      postMessage(panel, 'searchResults', {
        posts: result.data.results
      }, requestId);
    } else {
      throw new Error(result.error?.message || 'Search failed');
    }
  } catch (error) {
    postMessage(panel, 'error', {
      message: error instanceof Error ? error.message : 'Search failed'
    }, requestId);
  }
});
