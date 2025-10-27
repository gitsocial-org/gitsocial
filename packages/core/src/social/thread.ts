/**
 * Thread management and display logic
 */

import type { Post, Result, ThreadContext, ThreadItem, ThreadSort } from './types';
import { cache } from './post/cache';
import { log } from '../logger';
import { gitMsgRef } from '../gitmsg/protocol';

/**
 * Thread namespace
 */
export const thread = {
  getThread: getThreadImpl,
  buildThreadItems: buildThreadItemsImpl,
  sortPosts: sortPostsImpl,
  flattenContext: flattenContextImpl,
  buildContext: buildContextImpl
};

function matchesPostId(postId: string | undefined, targetId: string): boolean {
  if (!postId) { return false; }
  if (postId === targetId) { return true; }

  const parsedPost = gitMsgRef.parse(postId);
  const parsedTarget = gitMsgRef.parse(targetId);

  if (parsedPost.type === 'commit' && parsedTarget.type === 'commit' &&
      parsedPost.value === parsedTarget.value) {
    return true;
  }

  return false;
}

function getThreadImpl(
  workdir: string,
  anchorPostId: string,
  options?: {
    sort?: ThreadSort;
    maxParents?: number;
    maxChildren?: number;
  }
): Result<ThreadContext> {
  try {
    // Get all posts from cache (including virtual posts from external repos)
    const allPosts = cache.getCachedPosts(workdir, 'all');
    if (!allPosts || allPosts.length === 0) {
      return {
        success: false,
        error: {
          code: 'NO_POSTS',
          message: 'No posts found in timeline'
        }
      };
    }

    // Build context
    const contextResult = buildContextImpl(anchorPostId, allPosts, options?.sort || 'top');
    if (!contextResult.success) {
      return contextResult;
    }

    log('debug', '[thread.getThread] Thread context built:', {
      anchorPostId,
      parentCount: contextResult.data!.parentPosts.length,
      childCount: contextResult.data!.childPosts.length,
      sort: options?.sort || 'top'
    });

    return contextResult;
  } catch (error) {
    log('error', '[thread.getThread] Error:', error);
    return {
      success: false,
      error: {
        code: 'THREAD_ERROR',
        message: 'Failed to get thread',
        details: error
      }
    };
  }
}

function buildThreadItemsImpl(
  context: ThreadContext,
  options?: {
    deferParents?: boolean;
    maxParents?: number;
    maxChildren?: number;
  }
): ThreadItem[] {
  const items: ThreadItem[] = [];
  const opts = {
    deferParents: false,
    maxParents: 5,
    maxChildren: 50,
    ...options
  };

  // Add parent posts if not deferred
  if (!opts.deferParents && context.parentPosts.length > 0) {
    const parentsToShow = context.parentPosts.slice(-opts.maxParents);
    parentsToShow.forEach((post, index) => {
      items.push({
        type: 'post',
        key: post.id,
        depth: index - parentsToShow.length,
        data: post
      });
    });

    // Add "load more" if there are more parents
    if (context.parentPosts.length > opts.maxParents) {
      items.unshift({
        type: 'readMore',
        key: 'readMore-parents',
        depth: -opts.maxParents - 1,
        onLoadMore: () => {
          // Will be implemented with pagination
        }
      });
    }
  }

  // Add anchor post
  items.push({
    type: 'anchor',
    key: context.anchorPost.id,
    depth: 0,
    data: context.anchorPost
  });

  // Add child posts
  const childrenToShow = context.childPosts.slice(0, opts.maxChildren);
  childrenToShow.forEach((post, index) => {
    items.push({
      type: 'post',
      key: post.id,
      depth: index + 1,
      data: post
    });
  });

  // Add "load more" if there are more children
  if (context.childPosts.length > opts.maxChildren) {
    items.push({
      type: 'readMore',
      key: 'readMore-children',
      depth: opts.maxChildren + 1,
      onLoadMore: () => {
        // Will be implemented with pagination
      }
    });
  }

  return items;
}

function sortPostsImpl(posts: Post[], sort: ThreadSort): Post[] {
  switch (sort) {
  case 'top':
    // Sort by interaction count (comments + reposts)
    return [...posts].sort((a, b) => {
      const scoreA = (a.interactions?.comments || 0) +
                      (a.interactions?.reposts || 0) +
                      (a.interactions?.quotes || 0);
      const scoreB = (b.interactions?.comments || 0) +
                      (b.interactions?.reposts || 0) +
                      (b.interactions?.quotes || 0);
      return scoreB - scoreA;
    });

  case 'oldest':
    // Sort by creation time ascending
    return [...posts].sort((a, b) =>
      a.timestamp.getTime() - b.timestamp.getTime()
    );

  case 'latest':
    // Sort by creation time descending
    return [...posts].sort((a, b) =>
      b.timestamp.getTime() - a.timestamp.getTime()
    );

  default:
    return posts;
  }
}

function flattenContextImpl(context: ThreadContext): Post[] {
  const posts: Post[] = [];

  // Add parents in order
  posts.push(...context.parentPosts);

  // Add anchor
  posts.push(context.anchorPost);

  // Add children
  posts.push(...context.childPosts);

  return posts;
}

function buildContextImpl(
  anchorPostId: string,
  allPosts: Post[],
  sort: ThreadSort = 'top'
): Result<ThreadContext> {
  // Find anchor post
  const anchorPost = allPosts.find(p => p.id === anchorPostId);
  if (!anchorPost) {
    return {
      success: false,
      error: {
        code: 'POST_NOT_FOUND',
        message: `Anchor post not found: ${anchorPostId}`
      }
    };
  }

  log('debug', '[buildContext] Anchor post found:', {
    id: anchorPost.id,
    type: anchorPost.type,
    originalPostId: anchorPost.originalPostId,
    parentCommentId: anchorPost.parentCommentId,
    hasOriginalPostId: !!anchorPost.originalPostId,
    hasParentCommentId: !!anchorPost.parentCommentId
  });

  // Find thread root
  let threadRootId = anchorPostId;
  let currentPost = anchorPost;
  while (currentPost.originalPostId) {
    const parent = allPosts.find(p => matchesPostId(p.id, currentPost.originalPostId!));
    if (!parent) {break;}
    threadRootId = parent.id;
    currentPost = parent;
  }

  // Build parent chain including virtual posts
  const parentPosts: Post[] = [];
  const parentComments: Post[] = [];
  currentPost = anchorPost;

  // First, collect all parent comments in order (bottom to top)
  while (currentPost.parentCommentId) {
    const parentId = currentPost.parentCommentId;
    log('debug', '[buildContext] Looking for parent comment:', {
      currentPostId: currentPost.id,
      parentId,
      totalPosts: allPosts.length
    });
    const parent = allPosts.find(p => matchesPostId(p.id, parentId));
    if (!parent) {
      log('debug', '[buildContext] Parent comment not found:', {
        searchedId: parentId,
        possibleMatches: allPosts.filter(p => p.id.includes(parentId?.split('#commit:')[1] || 'none')).map(p => p.id)
      });
      break;
    }
    parentComments.unshift(parent); // Add to beginning to maintain top-to-bottom order
    currentPost = parent;
  }

  // Now find the original post (from the topmost parent or the anchor itself)
  let originalPost: Post | undefined;

  // Check if the topmost parent has an original
  if (parentComments.length > 0) {
    const topmostParent = parentComments[0];
    if (topmostParent && topmostParent.originalPostId) {
      originalPost = allPosts.find(p => matchesPostId(p.id, topmostParent.originalPostId!));
      if (originalPost) {
        log('debug', '[buildContext] Found original from topmost parent:', {
          originalId: originalPost.id,
          isVirtual: originalPost.isVirtual
        });
      }
    }
  }

  // If not found yet and anchor has an original, use that
  // BUT: Skip for quotes since they already display the original content
  if (!originalPost && anchorPost.originalPostId && anchorPost.type !== 'quote') {
    originalPost = allPosts.find(p => matchesPostId(p.id, anchorPost.originalPostId!));
    if (originalPost) {
      log('debug', '[buildContext] Found original from anchor post:', {
        originalId: originalPost.id,
        isVirtual: originalPost.isVirtual
      });
    } else {
      log('debug', '[buildContext] Original post not found:', {
        searchedId: anchorPost.originalPostId,
        availableVirtualPosts: allPosts.filter(p => p.isVirtual).map(p => p.id)
      });
    }
  }

  // Build final parent chain: original first, then parent comments
  if (originalPost) {
    parentPosts.push(originalPost);
  }
  parentPosts.push(...parentComments);

  // Find child posts (direct replies to anchor)
  // Exclude reposts when viewing the original post directly
  const childPosts = allPosts.filter(p =>
    (matchesPostId(p.originalPostId, anchorPostId) && p.type !== 'repost') ||
    matchesPostId(p.parentCommentId, anchorPostId)
  );

  log('debug', '[buildContext] Finding children for anchor:', {
    anchorPostId,
    anchorPostType: anchorPost.type,
    totalPosts: allPosts.length,
    childrenFound: childPosts.length
  });

  // Sort children
  const sortedChildren = sortPostsImpl(childPosts, sort);

  return {
    success: true,
    data: {
      anchorPost,
      parentPosts,
      childPosts: sortedChildren,
      threadRootId,
      hasMoreParents: false,  // Will be determined by pagination
      hasMoreChildren: false  // Will be determined by pagination
    }
  };
}
