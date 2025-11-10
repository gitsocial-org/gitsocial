/**
 * Thread helper functions - client-safe utilities with no Node.js dependencies
 */

import type { Post, ThreadSort } from '../types';
import { gitMsgRef } from '../../gitmsg/protocol';

export function matchesPostId(postId: string | undefined, targetId: string): boolean {
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

export function buildParentChildMap(allPosts: Post[]): Map<string, boolean> {
  const parentMap = new Map<string, boolean>();

  for (const post of allPosts) {
    if (post.originalPostId && post.type !== 'repost') {
      parentMap.set(post.originalPostId, true);
    }
    if (post.parentCommentId) {
      parentMap.set(post.parentCommentId, true);
    }
  }

  return parentMap;
}

export function calculateDepth(post: Post, anchorPost: Post, allPosts: Post[]): number {
  if (matchesPostId(post.id, anchorPost.id)) {
    return 0;
  }
  let depth = 0;
  let currentPost = post;
  while (currentPost.parentCommentId || currentPost.originalPostId) {
    const parentId = currentPost.parentCommentId || currentPost.originalPostId;
    if (!parentId) {break;}
    const parent = allPosts.find(p => matchesPostId(p.id, parentId));
    if (!parent) {break;}
    if (matchesPostId(parent.id, anchorPost.id)) {
      return depth + 1;
    }
    depth++;
    currentPost = parent;
  }
  let anchorDepth = 0;
  currentPost = anchorPost;
  while (currentPost.parentCommentId || currentPost.originalPostId) {
    const parentId = currentPost.parentCommentId || currentPost.originalPostId;
    if (!parentId) {break;}
    const parent = allPosts.find(p => matchesPostId(p.id, parentId));
    if (!parent) {break;}
    anchorDepth++;
    if (matchesPostId(parent.id, post.id)) {
      return -anchorDepth;
    }
    currentPost = parent;
  }
  return 0;
}

export function sortPosts(posts: Post[], sort: ThreadSort): Post[] {
  switch (sort) {
  case 'top':
    return [...posts].sort((a, b) => {
      const scoreA = a.interactions?.comments || 0;
      const scoreB = b.interactions?.comments || 0;
      if (scoreA !== scoreB) {
        return scoreB - scoreA;
      }
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    });

  case 'oldest':
    return [...posts].sort((a, b) =>
      new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
    );

  case 'latest':
    return [...posts].sort((a, b) =>
      new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    );

  default:
    return posts;
  }
}

export function sortThreadTree(
  postId: string,
  allPosts: Post[],
  sort: ThreadSort,
  depth: number = 1,
  seen: Set<string> = new Set()
): Post[] {
  const directChildren = allPosts.filter(p =>
    !seen.has(p.id) &&
    (matchesPostId(p.parentCommentId, postId) ||
      (matchesPostId(p.originalPostId, postId) && p.type !== 'repost' && !p.parentCommentId))
  );
  const sortToUse = depth === 1 ? sort : 'oldest';
  const sortedChildren = sortPosts(directChildren, sortToUse);
  const result: Post[] = [];
  for (const child of sortedChildren) {
    seen.add(child.id);
    result.push(child);
    result.push(...sortThreadTree(child.id, allPosts, sort, depth + 1, seen));
  }
  return result;
}
