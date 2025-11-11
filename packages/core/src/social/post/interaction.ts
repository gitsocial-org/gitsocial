/**
 * Interaction features for GitSocial (comments, reposts, quotes)
 */

import type { Post, Result } from '../types';
import type { GitMsgRef as GitMsgRefType } from '../../gitmsg/types';
import { formatGitMsgMessage } from '../../gitmsg/writer';
import { post as postNamespace } from '.';
import { log } from '../../logger';
import { gitMsgRef, gitMsgUrl } from '../../gitmsg/protocol';
import { gitHost } from '../../githost';
import { extractCleanContent } from '../../gitmsg/parser';

/**
 * Interaction namespace - Interaction management operations
 */
export const interaction = {
  createInteraction
};

/**
 * Create an interaction (comment, repost, or quote)
 */
async function createInteraction(
  type: 'comment' | 'repost' | 'quote',
  workdir: string,
  targetPost: Post,
  content?: string
): Promise<Result<Post>> {
  try {
    log('info', '[createInteraction] Creating interaction:', {
      type,
      targetPostId: targetPost.id,
      targetPostType: targetPost.type,
      hasContent: !!content
    });
    // Validate required parameters based on type
    if ((type === 'comment' || type === 'quote') && !content) {
      log('error', '[createInteraction] Missing content for type:', type);
      return {
        success: false,
        error: {
          code: 'MISSING_CONTENT',
          message: `Content is required for ${type}`
        }
      };
    }

    log('debug', '[createInteraction] Starting to build references');

    // Build references array
    const references: GitMsgRefType[] = [];

    // Determine if this is a nested comment
    const isNestedComment = type === 'comment' && targetPost.type === 'comment';
    log('debug', '[createInteraction] Is nested comment?', isNestedComment);

    let actualOriginalPostId: string;
    let actualOriginalPost: Post;

    if (isNestedComment) {
      log('debug', '[createInteraction] Processing nested comment');
      // For nested comments, we need to find the thread's original post
      // Check if targetPost has the original reference
      if (targetPost.originalPostId) {
        actualOriginalPostId = targetPost.originalPostId;
        log('debug', '[createInteraction] Found originalPostId field:', actualOriginalPostId);
      } else if (targetPost.raw?.gitMsg?.header.fields['original']) {
        // Parse from GitMsg fields
        const originalRef = targetPost.raw.gitMsg.header.fields['original'];
        actualOriginalPostId = originalRef;
        log('debug', '[createInteraction] Parsed original from GitMsg:', actualOriginalPostId);
      } else {
        actualOriginalPostId = targetPost.id;
        log('debug', '[createInteraction] Using targetPost as original:', actualOriginalPostId);
      }

      // Fetch the actual original post if different from target
      if (actualOriginalPostId !== targetPost.id) {
        log('debug', '[createInteraction] Fetching original post:', actualOriginalPostId);
        try {
          const originalResult = await postNamespace.getPosts(workdir, `post:${actualOriginalPostId}`);

          if (originalResult.success && originalResult.data && originalResult.data.length > 0) {
            actualOriginalPost = originalResult.data[0]!;
            log('debug', '[createInteraction] Successfully fetched original post');
          } else {
            // Fallback to target post if we can't find the original
            log('warn', '[createInteraction] Could not find original post, using target as fallback');
            actualOriginalPost = targetPost;
            actualOriginalPostId = targetPost.id;
          }
        } catch (err) {
          log('error', '[createInteraction] Error fetching original post:', err);
          actualOriginalPost = targetPost;
          actualOriginalPostId = targetPost.id;
        }
      } else {
        actualOriginalPost = targetPost;
      }

      // Add parent reference first (most immediate relationship)
      const targetRepoId = targetPost.repository;
      // Extract just the hash from the post ID to avoid duplicating repository info
      const targetParsed = gitMsgRef.parse(targetPost.id);
      const targetHash = (targetParsed.type === 'commit' ? targetParsed.value : null) || targetPost.id;

      log('debug', '[createInteraction] Creating parent reference:', {
        targetPostId: targetPost.id,
        extractedHash: targetHash,
        targetRepoId: targetRepoId,
        isRemote: gitMsgUrl.validate(targetRepoId),
        repoUrl: gitMsgRef.parse(targetRepoId).repository || null
      });

      // Parse the repository from the post's repository field (may contain #branch:xxx)
      const targetRepoParsed = gitMsgRef.parseRepositoryId(targetRepoId);
      const targetRepoUrl = targetRepoParsed.repository;

      // Only include repository URL for external repositories
      // My/workspace repository commits should use relative references
      const isMyRepository = targetRepoUrl === gitMsgUrl.normalize(workdir);

      const parentRefValue = gitMsgRef.create(
        'commit',
        targetHash,
        isMyRepository ? undefined : targetRepoUrl
      );

      log('debug', '[createInteraction] Parent reference created:', parentRefValue);

      const parentRef: GitMsgRefType = {
        ext: 'social',
        ref: parentRefValue,
        v: '0.1.0',
        extV: '0.1.0',
        author: targetPost.author.name,
        email: targetPost.author.email,
        time: new Date(targetPost.timestamp).toISOString(),
        fields: {},
        metadata: prefixContent(targetPost.cleanContent || extractCleanContent(targetPost.content))
      };
      references.push(parentRef);
      log('debug', '[createInteraction] Added parent reference');
    } else {
      // For direct interactions, the target is the original
      actualOriginalPost = targetPost;
      actualOriginalPostId = targetPost.id;
      log('debug', '[createInteraction] Direct interaction, using target as original');
    }

    // Add original post reference (most distant relationship)
    // Extract just the hash from the post ID to avoid duplicating repository info
    const originalParsed = gitMsgRef.parse(actualOriginalPostId);
    const originalHash = (originalParsed.type === 'commit' ? originalParsed.value : null) || actualOriginalPostId;

    log('debug', '[createInteraction] Creating original reference:', {
      actualOriginalPostId: actualOriginalPostId,
      extractedHash: originalHash,
      originalRepo: actualOriginalPost.repository,
      isRemote: gitMsgUrl.validate(actualOriginalPost.repository),
      repoUrl: gitMsgRef.parse(actualOriginalPost.repository).repository || null
    });

    // Parse the repository from the post's repository field (may contain #branch:xxx)
    const originalRepoParsed = gitMsgRef.parseRepositoryId(actualOriginalPost.repository);
    const originalRepoUrl = originalRepoParsed.repository;

    // Only include repository URL for external repositories
    // My/workspace repository commits should use relative references
    const isMyRepository = originalRepoUrl === gitMsgUrl.normalize(workdir);

    // Create the reference with the full repository URL for external repos
    const originalRefValue = gitMsgRef.create(
      'commit',
      originalHash,
      isMyRepository ? undefined : originalRepoUrl
    );

    log('debug', '[createInteraction] Original reference created:', originalRefValue);

    references.push({
      ext: 'social',
      ref: originalRefValue,
      v: '0.1.0',
      extV: '0.1.0',
      author: actualOriginalPost.author.name,
      email: actualOriginalPost.author.email,
      time: new Date(actualOriginalPost.timestamp).toISOString(),
      fields: {},
      metadata: prefixContent(actualOriginalPost.cleanContent || extractCleanContent(actualOriginalPost.content))
    });
    log('debug', '[createInteraction] Added original reference, total references:', references.length);

    // Generate content based on interaction type
    let finalContent = content || '';

    if (type === 'repost') {
      // Generate subject line for reposts
      const excerpt = actualOriginalPost.content.split('\n')[0];
      const repositoryUrl = gitMsgRef.parse(actualOriginalPost.repository).repository || null;
      const repoDisplayName = gitHost.getDisplayName(repositoryUrl || actualOriginalPost.repository);
      finalContent = gitMsgUrl.validate(actualOriginalPost.repository)
        ? `# ${actualOriginalPost.author.name} @ ${repoDisplayName}: ${excerpt}`
        : `# ${actualOriginalPost.author.name}: ${excerpt}`;
    }

    // Build GitMsg message with properly ordered fields
    const fields: Record<string, string> = {};

    // Add fields in the correct order
    if (isNestedComment) {
      // For nested comments, add fields in required order: reply-to, then original
      // Extract just the hash from the post ID to avoid duplicating repository info
      const targetParsed = gitMsgRef.parse(targetPost.id);
      const targetHash = (targetParsed.type === 'commit' ? targetParsed.value : null) || targetPost.id;
      const parentRefValue = gitMsgRef.create(
        'commit',
        targetHash,
        gitMsgUrl.validate(targetPost.repository)
          ? gitMsgRef.parse(targetPost.repository).repository || undefined
          : undefined
      );
      fields['reply-to'] = parentRefValue;
      fields['original'] = originalRefValue;
      log('debug', '[createInteraction] Set nested comment header fields:', {
        'reply-to': parentRefValue,
        'original': originalRefValue
      });
    } else {
      // For simple comments, quotes, and reposts, just add original reference
      fields['original'] = originalRefValue;
      log('debug', '[createInteraction] Set direct interaction header field:', {
        'original': originalRefValue
      });
    }

    const gitMsgMessage = {
      content: finalContent,
      header: {
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        fields: {
          'type': type,
          ...fields
        }
      },
      references
    };

    // Format the complete message for createPost
    const formattedMessage = formatGitMsgMessage(
      gitMsgMessage.content,
      gitMsgMessage.header,
      gitMsgMessage.references
    );

    log('debug', '[createInteraction] Formatted message:', {
      isNestedComment,
      fields: Object.keys(fields),
      referencesCount: references.length,
      messagePreview: formattedMessage.substring(0, 200)
    });

    const postResult = await postNamespace.createPost(workdir, formattedMessage);

    if (!postResult.success || !postResult.data) {
      log('error', '[createInteraction] Failed to create post:', postResult.error);
      return {
        success: false,
        error: {
          code: 'CREATE_POST_ERROR',
          message: `Failed to create ${type}`,
          details: postResult.error
        }
      };
    }

    log('info', '[createInteraction] Post created successfully');

    const post = postResult.data;

    // Add interaction-specific fields to the post
    const interaction: Post = {
      ...post,
      type,
      originalPostId: actualOriginalPostId
    };

    // Add additional fields based on type
    if (type === 'repost') {
      interaction.display.isEmpty = true;
    } else if (type === 'quote') {
      interaction.display.isEmpty = false;
    }

    log('info', `[createInteraction] Returning ${type}:`, { id: interaction.id });
    return { success: true, data: interaction };
  } catch (err) {
    log('error', '[createInteraction] Caught error:', err);
    return {
      success: false,
      error: {
        code: 'INTERACTION_ERROR',
        message: err instanceof Error ? err.message : 'Unknown error',
        details: err
      }
    };
  }
}

/**
 * Helper function to prefix content with > on each line
 */
function prefixContent(content: string): string {
  return content.split('\n').map(line => '> ' + line).join('\n');
}
