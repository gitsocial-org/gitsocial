import type { NotificationType, Post, Result } from './types';
import { git } from '../git';
import { gitMsgRef, gitMsgUrl } from '../gitmsg/protocol';
import { gitMsgList } from '../gitmsg/lists';
import { log } from '../logger';
import { post } from './post';
import { follower } from './follower';
import { list } from './list';
import { cache } from './post/cache';

/**
 * Notificatio namespace
 */
export const notification = {
  getNotifications
};

export interface Notification {
  type: NotificationType;
  commitId: string;
  commit?: {
    author: string;
    email: string;
    timestamp: Date;
  };
}

export interface NotificationOptions {
  since?: Date;
  until?: Date;
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

async function getFollowNotifications(
  workdir: string,
  myRepoUrl: string,
  since: Date,
  until?: Date
): Promise<Notification[]> {
  const followNotifications: Notification[] = [];
  try {
    const followersResult = await follower.get(workdir);
    if (!followersResult.success || !followersResult.data) {
      return [];
    }
    for (const followerRepo of followersResult.data) {
      if (!followerRepo.path) {
        continue;
      }
      const listsResult = await list.getLists(followerRepo.path);
      if (!listsResult.success || !listsResult.data) {
        continue;
      }
      const relevantList = listsResult.data.find(l => l.name === followerRepo.followsVia);
      if (!relevantList) {
        continue;
      }
      const historyResult = await gitMsgList.getHistory(
        followerRepo.path,
        'social',
        relevantList.id,
        workdir,
        {
          since,
          until
        }
      );
      if (!historyResult.success || !historyResult.data) {
        continue;
      }
      let previousRepositories = new Set<string>();
      for (const commit of historyResult.data) {
        const repositories = (commit.content as Record<string, unknown>)?.['repositories'];
        const currentRepositories = new Set<string>();
        if (Array.isArray(repositories)) {
          for (const repoStr of repositories) {
            if (typeof repoStr === 'string') {
              const repoUrl = repoStr.split('#')[0];
              const normalized = gitMsgUrl.normalize(repoUrl || '');
              currentRepositories.add(normalized);
            }
          }
        }
        if (currentRepositories.has(myRepoUrl) && !previousRepositories.has(myRepoUrl)) {
          followNotifications.push({
            type: 'follow',
            commitId: `${followerRepo.url}#commit:${commit.hash}`,
            commit: {
              author: commit.author,
              email: commit.email,
              timestamp: commit.timestamp
            }
          });
          break;
        }
        previousRepositories = currentRepositories;
      }
    }
  } catch (error) {
    log('error', 'Error getting follow notifications:', error);
  }
  return followNotifications;
}

async function getNotifications(
  workdir: string,
  storageBase?: string,
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

    const since = options?.since || new Date(Date.now() - 7 * 24 * 60 * 60 * 1000);
    const until = options?.until;

    // Ensure cache has data for the requested date range
    if (storageBase && !cache.isCacheRangeCovered(since)) {
      await cache.loadAdditionalPosts(workdir, storageBase, since);
    }

    const allPostsResult = await post.getPosts(workdir, 'timeline', { since, until });
    if (!allPostsResult.success || !allPostsResult.data) {
      return {
        success: false,
        error: allPostsResult.error || {
          code: 'POSTS_FETCH_FAILED',
          message: 'Failed to fetch posts'
        }
      };
    }

    const postNotifications = allPostsResult.data
      .filter((post: Post) => {
        if (isMyRepository(post.repository, myRepoUrl, workdir)) { return false; }

        if (post.type === 'comment') {
          if (post.originalPostId) {
            const originalRef = gitMsgRef.parse(post.originalPostId);
            if (originalRef.repository) {
              const normalizedOrigRepo = gitMsgUrl.normalize(originalRef.repository);
              if (normalizedOrigRepo === myRepoUrl) { return true; }
            }
          }
          if (post.parentCommentId) {
            const parentRef = gitMsgRef.parse(post.parentCommentId);
            if (parentRef.repository) {
              const normalizedParentRepo = gitMsgUrl.normalize(parentRef.repository);
              if (normalizedParentRepo === myRepoUrl) { return true; }
            }
          }
        }

        if ((post.type === 'repost' || post.type === 'quote') && post.originalPostId) {
          const originalRef = gitMsgRef.parse(post.originalPostId);
          if (originalRef.repository) {
            const normalizedOrigRepo = gitMsgUrl.normalize(originalRef.repository);
            if (normalizedOrigRepo === myRepoUrl) { return true; }
          }
        }

        return false;
      })
      .map((post: Post) => ({
        type: post.type as NotificationType,
        commitId: post.id
      }));

    const followNotifications = await getFollowNotifications(workdir, myRepoUrl, since, until);

    const allNotifications = [...postNotifications, ...followNotifications]
      .slice(0, options?.limit || 100);
    return {
      success: true,
      data: allNotifications
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
