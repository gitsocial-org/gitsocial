import type { NotificationType, Post, Result } from './types';
import { git } from '../git';
import { gitMsgRef, gitMsgUrl } from '../gitmsg/protocol';
import { log } from '../logger';
import { post } from './post';

/**
 * Notificatio namespace
 */
export const notification = {
  getNotifications
};

export interface Notification {
  id: string;
  type: NotificationType;
  timestamp: Date;

  post: Post;
  targetPostId?: string;

  actor: {
    repository: string;
    name: string;
    email: string;
  };
}

export interface NotificationOptions {
  since?: Date;
  limit?: number;
}

function isMyRepository(
  postRepository: string,
  myRepoUrl: string,
  workdir: string
): boolean {
  const postRepoUrl = postRepository.split('#')[0] || postRepository;
  const normalizedPostRepo = gitMsgUrl.normalize(postRepoUrl);
  const normalizedWorkdir = gitMsgUrl.normalize(workdir);

  return normalizedPostRepo === myRepoUrl || normalizedPostRepo === normalizedWorkdir;
}

async function getNotifications(
  workdir: string,
  options?: NotificationOptions
): Promise<Result<Notification[]>> {
  try {
    const myRepoUrlResult = await git.getOriginUrl(workdir);
    if (!myRepoUrlResult.success || !myRepoUrlResult.data) {
      return {
        success: false,
        error: {
          code: 'NO_MY_REPOSITORY',
          message: 'Could not find my repository URL'
        }
      };
    }
    const myRepoUrl = gitMsgUrl.normalize(myRepoUrlResult.data);
    log('debug', `Normalized my repository URL: ${myRepoUrl}`);

    const allPostsResult = post.getPosts(workdir, 'timeline');
    if (!allPostsResult.success || !allPostsResult.data) {
      return {
        success: false,
        error: allPostsResult.error || {
          code: 'POSTS_FETCH_FAILED',
          message: 'Failed to fetch posts'
        }
      };
    }

    const notifications = allPostsResult.data
      .filter((post: Post) => {
        // Skip posts from my own repository
        if (isMyRepository(post.repository, myRepoUrl, workdir)) { return false; }

        // Skip old posts if since is specified
        if (options?.since && post.timestamp < options.since) { return false; }

        // Check if this is a comment on my post
        if (post.type === 'comment') {
          // Check originalPostId
          if (post.originalPostId) {
            const originalRef = gitMsgRef.parse(post.originalPostId);
            if (originalRef.repository) {
              const normalizedOrigRepo = gitMsgUrl.normalize(originalRef.repository);
              if (normalizedOrigRepo === myRepoUrl) { return true; }
            }
          }
          // Check parentCommentId
          if (post.parentCommentId) {
            const parentRef = gitMsgRef.parse(post.parentCommentId);
            if (parentRef.repository) {
              const normalizedParentRepo = gitMsgUrl.normalize(parentRef.repository);
              if (normalizedParentRepo === myRepoUrl) { return true; }
            }
          }
        }

        // Check if this is a repost or quote of my post
        if ((post.type === 'repost' || post.type === 'quote') && post.originalPostId) {
          const originalRef = gitMsgRef.parse(post.originalPostId);
          if (originalRef.repository) {
            const normalizedOrigRepo = gitMsgUrl.normalize(originalRef.repository);
            if (normalizedOrigRepo === myRepoUrl) { return true; }
          }
        }

        return false;
      })
      .sort((a: Post, b: Post) => b.timestamp.getTime() - a.timestamp.getTime())
      .slice(0, options?.limit || 100)
      .map((post: Post) => ({
        id: post.id,
        type: post.type as NotificationType,
        timestamp: post.timestamp,
        post,
        targetPostId: post.originalPostId || post.parentCommentId,
        actor: {
          repository: post.repository,
          name: post.author.name,
          email: post.author.email
        }
      }));

    log('debug', `Found ${notifications.length} notifications`);

    return {
      success: true,
      data: notifications
    };
  } catch (error) {
    log('error', 'Failed to get notifications:', error);
    return {
      success: false,
      error: {
        code: 'NOTIFICATION_ERROR',
        message: error instanceof Error ? error.message : 'Failed to get notifications'
      }
    };
  }
}
