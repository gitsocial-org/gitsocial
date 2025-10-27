/**
 * Pure validation functions for posts
 * These are stateless functions that validate post data structures
 */

import type { Post } from '../types';
import { gitMsgRef } from '../../gitmsg/protocol';

/**
 * Validate a post's reference integrity
 * Pure validation function that checks all references in a post
 */
export function validatePostReferences(post: Post): string[] {
  const errors: string[] = [];

  // Validate post ID
  if (!gitMsgRef.validate(post.id, 'commit')) {
    errors.push(`Invalid post ID format: ${post.id}`);
  }

  // Validate originalPostId for interactions
  if (post.type !== 'post') {
    if (!post.originalPostId) {
      errors.push(`${post.type} post missing required originalPostId`);
    } else if (!gitMsgRef.validate(post.originalPostId, 'commit')) {
      errors.push(`Invalid originalPostId format: ${post.originalPostId}`);
    }
  }

  // Validate parentCommentId for nested comments
  if (post.parentCommentId && !gitMsgRef.validate(post.parentCommentId, 'commit')) {
    errors.push(`Invalid parentCommentId format: ${post.parentCommentId}`);
  }

  return errors;
}

/**
 * Validate a batch of posts and return detailed validation report
 * Pure function that validates multiple posts and aggregates results
 */
export function validatePostBatch(posts: Post[]): {
  isValid: boolean;
  errors: Array<{postId: string; postType: string; errors: string[]}>;
  summary: string;
} {
  const postErrors: Array<{postId: string; postType: string; errors: string[]}> = [];
  let totalErrors = 0;

  for (const post of posts) {
    const errors = validatePostReferences(post);
    if (errors.length > 0) {
      postErrors.push({
        postId: post.id,
        postType: post.type,
        errors
      });
      totalErrors += errors.length;
    }
  }

  const summary = totalErrors === 0
    ? `All ${posts.length} posts have valid references`
    : `${postErrors.length}/${posts.length} posts have validation errors (${totalErrors} total errors)`;

  return {
    isValid: totalErrors === 0,
    errors: postErrors,
    summary
  };
}
