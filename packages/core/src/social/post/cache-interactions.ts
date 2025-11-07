/**
 * Interaction counting and cache update functions
 * Handles calculating interaction counts and updating cache state
 */

import { gitMsgRef, gitMsgUrl } from '../../client';
import { log } from '../../logger';
import type { Post } from '../types';
import { postIndex, postsCache, resolveToCanonicalId } from './cache';
import { getOriginUrl } from '../../git/remotes';

async function calculateInteractionCounts(posts: Map<string, Post>, workdir?: string): Promise<void> {
  for (const post of posts.values()) {
    post.interactions = { comments: 0, reposts: 0, quotes: 0 };
    post.display.totalReposts = 0;
  }

  // Get my origin URL for fallback lookup
  let myOriginUrl: string | undefined;
  if (workdir) {
    const originResult = await getOriginUrl(workdir);
    if (originResult.success && originResult.data && originResult.data !== 'myrepository') {
      myOriginUrl = gitMsgUrl.normalize(originResult.data);
    }
  }

  let matchCount = 0;
  let missCount = 0;
  let fallbackCount = 0;

  // Track which interactions we've already counted to prevent duplicates
  const countedInteractions = new Set<string>();

  for (const post of posts.values()) {
    // Count all interactions toward the original post
    // This ensures nested comments (replies to comments) are counted
    const targetId = post.originalPostId;

    if (targetId && post.type !== 'post') {
      // Normalize both sides of the interaction for dedup checking
      const sourceId = resolveToCanonicalId(post.id, myOriginUrl);
      const canonicalTargetId = resolveToCanonicalId(targetId, myOriginUrl);

      // Create unique key for this interaction using canonical IDs
      const interactionKey = `${sourceId}->${canonicalTargetId}`;
      if (countedInteractions.has(interactionKey)) {
        log('debug', '[calculateInteractionCounts] Skipping duplicate interaction:', interactionKey);
        continue; // Already counted this interaction
      }
      countedInteractions.add(interactionKey);

      let targetPost = posts.get(targetId);

      // If not found directly, check if there's a mapping
      if (!targetPost) {
        const mappedId = postIndex.absolute.get(targetId);
        if (mappedId) {
          targetPost = posts.get(mappedId);
          if (targetPost) {
            fallbackCount++;
            log('debug', '[calculateInteractionCounts] Found via absolute mapping:', {
              absoluteRef: targetId,
              relativeId: mappedId,
              postType: post.type
            });
          }
        }
      }

      // If still not found and targetId contains my origin URL, try hash-only lookup
      if (!targetPost && myOriginUrl && targetId.includes(myOriginUrl)) {
        const parsed = gitMsgRef.parse(targetId);
        if (parsed.type === 'commit' && parsed.value) {
          // Try to find the local post with just the hash
          const localId = `#commit:${parsed.value}`;
          targetPost = posts.get(localId);
          if (targetPost) {
            fallbackCount++;
            log('debug', '[calculateInteractionCounts] Found via fallback lookup:', {
              originalRef: targetId,
              localId,
              postType: post.type
            });
          }
        }
      }

      if (targetPost?.interactions) {
        matchCount++;
        if (post.type === 'comment') { targetPost.interactions.comments++; }
        else if (post.type === 'repost') { targetPost.interactions.reposts++; }
        else if (post.type === 'quote') { targetPost.interactions.quotes++; }

        targetPost.display.totalReposts = targetPost.interactions.reposts + targetPost.interactions.quotes;
      } else {
        missCount++;
        log('debug', '[calculateInteractionCounts] Missing target post:', {
          postId: post.id,
          postType: post.type,
          targetId,
          originalPostId: post.originalPostId,
          parentCommentId: post.parentCommentId,
          hasTargetInMap: posts.has(targetId),
          availableKeys: Array.from(posts.keys()).filter(k => k.includes(targetId?.split('#commit:')?.[1]?.substring(0, 8) || 'none')).slice(0, 3)
        });
      }
    }
  }

  log('debug', '[calculateInteractionCounts] Summary:', {
    totalPosts: posts.size,
    interactionPosts: matchCount + missCount,
    matchCount,
    missCount,
    fallbackCount
  });
}

// Helper function for incremental interaction updates
export async function updateInteractionCounts(newPosts: Map<string, Post>, workdir?: string): Promise<void> {
  // First, get all existing posts from cache
  const allPosts = new Map<string, Post>();

  // Copy existing posts from cache (as mutable copies)
  for (const [id, frozenPost] of postsCache.entries()) {
    allPosts.set(id, { ...frozenPost });
  }

  // Add new posts
  for (const [id, post] of newPosts.entries()) {
    allPosts.set(id, post);
  }

  // Recalculate ALL interaction counts with the complete set
  await calculateInteractionCounts(allPosts, workdir);

  // Update the cache with recalculated existing posts
  for (const [id, post] of allPosts.entries()) {
    if (!newPosts.has(id)) {
      // This is an existing post that may have updated counts
      const frozenPost: Readonly<Post> = Object.freeze(post);
      postsCache.set(id, frozenPost);
    }
  }

  // Update newPosts Map with their calculated counts
  for (const [id, post] of newPosts.entries()) {
    const updatedPost = allPosts.get(id);
    if (updatedPost) {
      Object.assign(post, updatedPost);
    }
  }
}
