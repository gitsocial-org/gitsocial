/**
 * Post operations for GitSocial
 */

import type { List, Post, Result } from '../types';
import { git } from '../../git';
import { gitMsgRef } from '../../gitmsg/protocol';
import { log } from '../../logger';
import { cache } from './cache';
import { thread } from '../thread';
import { repository } from '../repository';

/**
 * Post namespace - Post management operations
 */
export const post = {
  getPosts,
  createPost
};

/**
 * Get posts from Git repository with caching
 * This is now a simple wrapper that delegates all work to cache.ts
 * as per the CRITICAL REQUIREMENT in ARCHITECTURE.md
 */
function getPosts(
  workdir: string,
  scope: string = 'repository:my',
  filter?: {
    types?: Array<'post' | 'quote' | 'comment' | 'repost'>;
    since?: Date;
    until?: Date;
    limit?: number;
    includeImplicit?: boolean;
    skipCache?: boolean;
    sortBy?: 'top' | 'latest' | 'oldest';
  },
  context?: {
    list?: List;  // Optional list data for remote lists
  }
): Result<Post[]> {
  try {
    // Handle thread scope
    if (scope.startsWith('thread:')) {
      const postId = scope.substring(7);
      const threadResult = thread.getThread(workdir, postId, {
        sort: filter?.sortBy || 'top'
      });

      if (!threadResult.success) {
        return {
          success: false,
          error: threadResult.error
        };
      }

      // Convert ThreadContext to flat Post array
      const posts = thread.flattenContext(threadResult.data!);
      return { success: true, data: posts };
    }

    // All post operations happen in cache.ts
    const posts = cache.getCachedPosts(workdir, scope, filter, context);
    return { success: true, data: posts || [] };
  } catch (error) {
    log('error', '[getPosts] Error:', error);
    return {
      success: false,
      error: {
        code: 'GET_POSTS_ERROR',
        message: 'Failed to get posts',
        details: error
      }
    };
  }
}

/**
 * Create a new post (always on GitSocial branch)
 */
async function createPost(
  workdir: string,
  content: string,
  _options?: {
    branch?: string;
  }
): Promise<Result<Post>> {
  try {
    log('debug', '[createPost] Creating post:', {
      contentLength: content.length,
      contentPreview: content.substring(0, 100)
    });

    if (!content || content.trim() === '') {
      return {
        success: false,
        error: {
          code: 'EMPTY_CONTENT',
          message: 'Post content cannot be empty'
        }
      };
    }

    const gitSocialBranch = await git.getConfiguredBranch(workdir);
    const message = content;

    log('debug', '[createPost] Creating post on GitSocial branch:', {
      branch: gitSocialBranch,
      messagePreview: message.substring(0, 200)
    });

    const commitResult = await git.createCommitOnBranch(workdir, gitSocialBranch, message);
    if (!commitResult.success || !commitResult.data) {
      log('error', '[createPost] Failed to create commit:', commitResult.error);
      return {
        success: false,
        error: {
          code: 'COMMIT_ERROR',
          message: 'Failed to create commit',
          details: commitResult.error
        }
      };
    }

    log('info', '[createPost] Commit created successfully:', commitResult.data);

    await cache.refresh({ repositories: [workdir] }, workdir);
    log('debug', '[createPost] Cache refreshed to include new post');

    const commitHash = commitResult.data.substring(0, 12);
    const postId = gitMsgRef.create('commit', commitHash);

    const postsResult = getPosts(workdir, `post:${postId}`);

    if (!postsResult.success || !postsResult.data || postsResult.data.length === 0) {
      return {
        success: false,
        error: {
          code: 'POST_LOAD_ERROR',
          message: 'Failed to load created post',
          details: postsResult.error
        }
      };
    }

    const post = postsResult.data[0];

    if (!post) {
      return {
        success: false,
        error: {
          code: 'POST_LOAD_ERROR',
          message: 'Failed to load created post - post was undefined'
        }
      };
    }

    log('info', '[createPost] Post created successfully:', {
      postId: post.id,
      type: post.type,
      source: post.source
    });

    return { success: true, data: post };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'CREATE_POST_ERROR',
        message: 'Failed to create post',
        details: error
      }
    };
  }
}

/**
 * Ensure repositories for a remote list are loaded into cache
 * Call this BEFORE calling getPosts() for remote lists
 *
 * @param workdir - Workspace directory
 * @param repositories - Array of repository strings (format: "url#branch:name")
 * @param storageBase - Storage base directory
 * @param since - Optional date to start fetching from (defaults to current week)
 */
export async function ensureRemoteListRepositories(
  workdir: string,
  repositories: string[],
  storageBase: string,
  since?: Date
): Promise<void> {
  for (const repoString of repositories) {
    const parsed = gitMsgRef.parseRepositoryId(repoString);
    if (!parsed) {
      log('warn', `[ensureRemoteListRepositories] Invalid repository: ${repoString}`);
      continue;
    }

    // Reuse existing function - does ensure + fetch + loadRepositoryPosts
    const result = await repository.ensureDataForDateRange(
      workdir,
      storageBase,
      parsed.repository,
      parsed.branch,
      since || new Date(),
      { isPersistent: false }  // Remote list repos are temporary
    );

    if (!result.success) {
      log('warn', `[ensureRemoteListRepositories] Failed: ${parsed.repository}`, result.error);
      // Continue with other repos - tolerant error handling
    }
  }
}
