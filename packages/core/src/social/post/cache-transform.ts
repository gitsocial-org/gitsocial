/**
 * These functions handle the transformation of Git commits into Posts
 */

import type { Post } from '../types';
import type { Commit } from '../../git/types';
import type { GitMsgRef } from '../../gitmsg/types';
import { extractCleanContent, getPostType, isEmptyRepost, parseGitMsgMessage, parseGitMsgRef } from '../../gitmsg/parser';
import { getConfiguredBranch, getCurrentBranch, getUnpushedCommits } from '../../git/operations';
import { log } from '../../logger';
import { gitMsgHash, gitMsgRef, gitMsgUrl } from '../../gitmsg/protocol';
import { gitHost } from '../../githost';
import { listRemotes } from '../../git/remotes';

/**
 * Unified function to construct posts from real commits or virtual references
 */
export function constructPost(
  // From real Git commit
  realCommit?: {
    hash: string;
    author: string;
    email: string;
    timestamp: Date;
    message: string;
    refname?: string;
    // Context
    repository: string;
    repositoryIdentifier?: string;
    branch?: string;
    isGitSocialBranch?: boolean;
    hasOriginRemote?: boolean;
    unpushedCommits?: Set<string> | null;
    remoteName?: string;
  },

  // From virtual commit (embedded reference)
  virtualCommit?: {
    body: string;           // Full embedded message with headers and quoted content
    refId: string;          // Must be absolute reference (e.g., "https://github.com/user/repo#commit:abc123")
    refType?: string;       // GitMsg-Ref type (e.g., "social")
    fields?: Record<string, string>; // Pre-parsed fields if available
  }
): Post | null {
  // Determine source type
  const isVirtual = !realCommit && virtualCommit;
  const isReal = realCommit && !virtualCommit;

  if (!isVirtual && !isReal) {return null;}

  // Extract data based on source
  let author, timestamp, content, gitMsg, hash, typeHeader;

  if (isReal && realCommit) {
    // Extract from real commit
    author = { name: realCommit.author, email: realCommit.email };
    timestamp = realCommit.timestamp;
    gitMsg = parseGitMsgMessage(realCommit.message);
    content = gitMsg?.content || realCommit.message;
    hash = realCommit.hash;
  } else if (isVirtual && virtualCommit) {
    // Use parseGitMsgRef for embedded messages
    const ref = parseGitMsgRef(virtualCommit.body);
    if (!ref) {return null;}

    // Extract author from core GitMsg-Ref fields
    author = {
      name: ref.author || virtualCommit.fields?.['author'] || '',
      email: ref.email || virtualCommit.fields?.['email'] || ''
    };

    if (!author.name || !author.email) {return null;}

    // Extract timestamp from core field
    const timeStr = ref.time || virtualCommit.fields?.['time'];
    if (!timeStr) {return null;}
    timestamp = new Date(timeStr);

    // Extract type from headers (defaults to 'post')
    typeHeader = ref.fields['type'] || virtualCommit.fields?.['type'];

    // Extract quoted content from metadata (lines starting with ">")
    const quotedLines = ref.metadata?.split('\n')
      .filter(line => line.startsWith('>'))
      .map(line => line.substring(1).trim()) || [];
    content = quotedLines.join('\n');

    if (!content) {return null;}

    // Extract repository and hash from reference ID
    const hashMatch = virtualCommit.refId.match(/#commit:([a-f0-9]+)$/);
    hash = hashMatch?.[1] || '';
    if (!hash) {
      log('warn', '[constructPost] No hash found in virtual commit refId:', virtualCommit.refId);
      return null;
    }

    log('debug', '[constructPost] Creating virtual post:', {
      refId: virtualCommit.refId,
      isRelative: virtualCommit.refId.startsWith('#'),
      hash,
      authorName: author.name,
      type: typeHeader || 'post'
    });
  } else {
    return null;
  }

  // Determine if this is a workspace post (not from external repository)
  // For real commits: check remoteName
  // For virtual commits: check if refId is relative (starts with '#')
  const isWorkspacePost = isVirtual
    ? virtualCommit?.refId.startsWith('#') || false
    : !!(realCommit && realCommit.remoteName !== 'upstream');

  // Get the repository URL
  let repoUrl: string;
  if (isVirtual && virtualCommit) {
    // For virtual posts, extract from refId
    if (virtualCommit.refId.startsWith('#')) {
      // Relative workspace reference - no repository URL
      repoUrl = '';
    } else {
      // Absolute external reference - extract repository URL
      const refParts = virtualCommit.refId.split('#');
      repoUrl = refParts[0] || '';
    }
  } else if (isWorkspacePost && realCommit) {
    // Workspace posts use local path
    repoUrl = realCommit.repository || '';
  } else {
    // External posts use the repository URL
    repoUrl = realCommit?.repositoryIdentifier || realCommit?.repository || '';
  }

  const normalizedUrl = repoUrl ? gitMsgUrl.normalize(repoUrl) : '';

  // Branch handling - undefined for virtual posts
  const branchName = realCommit?.branch
    ? gitMsgRef.extractBranchFromRemote(realCommit.branch)
    : undefined;

  // Repository ID - no branch suffix for virtual posts
  const standardizedRepoId = branchName
    ? `${normalizedUrl}#branch:${branchName}`
    : normalizedUrl;

  // ID generation with clear workspace vs external logic
  let postId: string;

  if (isWorkspacePost) {
    // Workspace posts ALWAYS use relative ID as primary
    postId = gitMsgRef.create('commit', hash);

    log('debug', '[constructPost] Creating workspace post with relative ID:', {
      postId,
      isWorkspacePost: true
    });
  } else {
    // External posts ALWAYS use absolute ID
    postId = gitMsgRef.create('commit', hash, normalizedUrl);

    log('debug', '[constructPost] Creating external post with absolute ID:', {
      postId,
      isWorkspacePost: false
    });
  }

  // Use gitMsgHash.normalize for consistent hash handling
  const normalizedHash = gitMsgHash.normalize(hash);

  // Unified post construction
  const post: Post = {
    id: postId,
    repository: standardizedRepoId,
    branch: realCommit?.branch,
    author,
    timestamp,
    content,
    type: isVirtual
      ? (typeHeader as 'post' | 'comment' | 'repost' | 'quote' || 'post')
      : getPostType(gitMsg || undefined),
    source: gitMsg || isVirtual ? 'explicit' : 'implicit',
    isVirtual: isVirtual ? true : undefined,

    // Workspace post identification
    isWorkspacePost: isWorkspacePost || undefined,

    raw: {
      commit: realCommit ? {
        hash: normalizedHash,
        author: author.name,
        email: author.email,
        timestamp,
        message: realCommit.message,
        refname: realCommit.refname
      } : {
        hash: normalizedHash,
        author: author.name,
        email: author.email,
        timestamp,
        message: content,
        refname: undefined
      },
      gitMsg: gitMsg || undefined
    },
    cleanContent: gitMsg ? extractCleanContent(realCommit?.message || content) : content,
    interactions: { comments: 0, reposts: 0, quotes: 0 },
    remote: realCommit?.remoteName,
    display: {
      repositoryName: gitHost.getDisplayName(normalizedUrl),
      commitHash: normalizedHash,
      commitUrl: gitHost.getCommitUrl(normalizedUrl, normalizedHash),
      totalReposts: 0,
      isEmpty: gitMsg ? isEmptyRepost(gitMsg) : false,
      isUnpushed: realCommit?.hasOriginRemote ?
        (realCommit.unpushedCommits !== undefined
          ? realCommit.unpushedCommits !== null && realCommit.unpushedCommits.has(hash)
          : !realCommit.refname ||
            realCommit.refname.startsWith('refs/heads/') ||
            !realCommit.refname.startsWith('refs/remotes/origin/')
        ) : false,
      isOrigin: isWorkspacePost || false,  // Set to true for all workspace posts
      isWorkspacePost: isWorkspacePost      // Mirror at display level for UI convenience
    }
  };

  // Extract references if present
  if (gitMsg) {
    const replyTo = gitMsg.header.fields['reply-to'];
    const original = gitMsg.header.fields['original'];
    if (replyTo) {
      // Use existing protocol function instead of custom logic
      post.parentCommentId = gitMsgRef.isMyRepository(replyTo) && !isWorkspacePost
        ? gitMsgRef.normalizeHashInRefWithContext(replyTo, normalizedUrl)
        : gitMsgRef.normalize(replyTo);

      log('debug', '[constructPost] Set parentCommentId:', {
        raw: replyTo,
        normalized: post.parentCommentId,
        normalizedUrl
      });
    }
    if (original) {
      // Use existing protocol function instead of custom logic
      post.originalPostId = gitMsgRef.isMyRepository(original) && !isWorkspacePost
        ? gitMsgRef.normalizeHashInRefWithContext(original, normalizedUrl)
        : gitMsgRef.normalize(original);

      log('debug', '[constructPost] Set originalPostId:', {
        raw: original,
        normalized: post.originalPostId,
        normalizedUrl
      });
    }
  }

  // Validate
  if (post.type !== 'post' && !post.originalPostId) {
    log('error', '[constructPost] Post missing required originalPostId:', {
      type: post.type,
      id: post.id
    });
    return null;
  }

  return post;
}

/**
 * Process a post and its references in a single pass
 * This eliminates the duplicate code between processAndAddPost and processReferences
 */
export function processPost(
  post: Post,
  posts: Map<string, Post>,
  repository: string,
  originUrl?: string,
  postIndex?: {
    absolute: Map<string, string>;
    merged: Set<string>;
  }
): void {
  // Normalize post references using existing protocol functions
  // Only re-normalize if we have an originUrl (for workspace posts with remote)
  // or if this is an external post (needs repository context)
  if (originUrl || !post.isWorkspacePost) {
    const repoForNormalization = (post.isWorkspacePost && originUrl) ? originUrl : repository;

    if (post.originalPostId) {
      post.originalPostId = gitMsgRef.isMyRepository(post.originalPostId)
        ? gitMsgRef.normalizeHashInRefWithContext(post.originalPostId, repoForNormalization)
        : gitMsgRef.normalize(post.originalPostId);
    }
    if (post.parentCommentId) {
      post.parentCommentId = gitMsgRef.isMyRepository(post.parentCommentId)
        ? gitMsgRef.normalizeHashInRefWithContext(post.parentCommentId, repoForNormalization)
        : gitMsgRef.normalize(post.parentCommentId);
    }
  }
  // Otherwise, for workspace posts without originUrl, references are already correctly relative

  // For workspace posts, create absolute ID mapping if origin URL is available
  if (post.isWorkspacePost && originUrl && postIndex) {
    const parsed = gitMsgRef.parse(post.id);
    const absoluteId = gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value, originUrl);
    postIndex.absolute.set(absoluteId, post.id);
    log('debug', '[processPostWithReferences] Created absolute->relative mapping for workspace post:', {
      relativeId: post.id,
      absoluteId: absoluteId
    });
  }

  // Check for deduplication: if this is an external post that duplicates a workspace post
  if (!post.isWorkspacePost && originUrl && post.id.startsWith(originUrl) && postIndex) {
    const parsed = gitMsgRef.parse(post.id);
    const relativeId = gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value);
    const workspacePost = posts.get(relativeId);

    if (workspacePost) {
      // This external post duplicates a workspace post - don't add it, just create mapping
      postIndex.absolute.set(post.id, relativeId);
      log('debug', '[processPostWithReferences] Deduplicated external post to workspace post:', {
        externalId: post.id,
        workspaceId: relativeId
      });
      return; // Skip adding duplicate
    }
  }

  const existing = posts.get(post.id);
  // Never overwrite a real post with explicit source
  if (!existing) {
    posts.set(post.id, post);
  } else if (post.source === 'explicit' && existing.source !== 'explicit') {
    // Replace virtual/implicit post with real explicit post
    posts.set(post.id, post);
  } else if (post.source === 'explicit' && existing.source === 'explicit') {
    // Both are explicit - keep the existing one (shouldn't happen in normal flow)
    log('debug', '[processPostWithReferences] Keeping existing explicit post:', post.id);
  }
  // Otherwise keep existing (don't replace real with virtual)

  // Process embedded references to create virtual posts
  if (post.raw?.gitMsg?.references) {
    for (const ref of post.raw.gitMsg.references) {
      if (ref.ext === 'social' && ref.metadata) {
        const virtualPost = createVirtualPostFromReference(ref, post, repository, originUrl);
        if (virtualPost) {
          // Check if this virtual reference points to our workspace
          if (originUrl && virtualPost.id.startsWith(originUrl)) {
            const parsed = gitMsgRef.parse(virtualPost.id);
            const relativeId = gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value);

            // Check if workspace post exists
            const workspacePost = posts.get(relativeId);
            if (workspacePost) {
              // MERGE virtual post data into workspace post
              mergeVirtualPostIntoWorkspace(workspacePost, ref);

              // Map absolute ID to relative for lookups
              if (postIndex) {
                postIndex.absolute.set(virtualPost.id, relativeId);
                postIndex.merged.add(virtualPost.id);
              }
              log('debug', '[processPostWithReferences] Merged virtual ref into workspace post:', {
                virtualRef: virtualPost.id,
                workspaceId: relativeId
              });
              continue;
            }
          }

          // Add virtual post if no existing post
          const existingPost = posts.get(virtualPost.id);
          if (!existingPost) {
            posts.set(virtualPost.id, virtualPost);
            log('debug', '[processPostWithReferences] Added virtual post:', {
              id: virtualPost.id,
              refType: ref.fields['social:ref-type']
            });
          }
        }
      }
    }
  }
}

/**
 * Create a virtual post from a GitMsg reference
 * Extracted to eliminate duplication between processAndAddPost and processReferences
 */
export function createVirtualPostFromReference(
  ref: GitMsgRef,
  parentPost: Post,
  _repository: string,
  originUrl?: string
): Post | null {
  // Handle relative references (within same repository)
  let fullRefId = ref.ref;

  if (ref.ref && ref.ref.startsWith('#')) {
    // This is a relative reference - need to add repository context
    if (parentPost.id.startsWith('#')) {
      // Workspace post with relative ID
      if (originUrl) {
        // Use origin URL to make absolute
        fullRefId = `${originUrl}${ref.ref}`;
        log('debug', '[createVirtualPostFromReference] Converted relative reference to absolute:', ref.ref, '->', fullRefId);
      } else {
        // No origin URL - keep reference relative for workspace-to-workspace references
        fullRefId = ref.ref;
        log('debug', '[createVirtualPostFromReference] Keeping workspace reference relative:', ref.ref);
      }
    } else {
      // External post with absolute ID - extract repository from ID
      const postIdParts = parentPost.id.split('#');
      const repoUrl = postIdParts[0] || '';
      fullRefId = `${repoUrl}${ref.ref}`;
      log('debug', '[createVirtualPostFromReference] Converted relative reference to absolute:', ref.ref, '->', fullRefId);
    }
  } else if (!ref.ref || !ref.ref.includes('#')) {
    log('warn', '[createVirtualPostFromReference] Skipping reference without valid format:', ref.ref);
    return null;
  }

  // Construct GitMsg-Ref body for virtual commit
  const coreFields = [];
  if (ref.author) {coreFields.push(`author="${ref.author}"`);}
  if (ref.email) {coreFields.push(`email="${ref.email}"`);}
  if (ref.time) {coreFields.push(`time="${ref.time}"`);}
  const extensionFields = Object.entries(ref.fields).map(([k, v]) => `${k}="${v}"`);
  const refBody = `--- GitMsg-Ref: ext="${ref.ext}"; ${coreFields.join('; ')}${coreFields.length > 0 ? '; ' : ''}${extensionFields.join('; ')}${extensionFields.length > 0 ? '; ' : ''}ref="${ref.ref}"; v="${ref.v}"; ext-v="${ref.extV}" ---\n${ref.metadata || ''}`;

  try {
    const virtualPost = constructPost(
      undefined,
      {
        body: refBody,
        refId: fullRefId,
        refType: ref.ext,
        fields: ref.fields
      }
    );

    if (virtualPost && originUrl) {
      // Normalize references in virtual post if they point to workspace
      if (virtualPost.originalPostId) {
        const parsed = gitMsgRef.parse(virtualPost.originalPostId);
        if (parsed.repository === originUrl && parsed.type !== 'unknown') {
          virtualPost.originalPostId = gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value);
        }
      }
      if (virtualPost.parentCommentId) {
        const parsed = gitMsgRef.parse(virtualPost.parentCommentId);
        if (parsed.repository === originUrl && parsed.type !== 'unknown') {
          virtualPost.parentCommentId = gitMsgRef.create(parsed.type as 'commit' | 'branch', parsed.value);
        }
      }
    }

    return virtualPost;
  } catch (error) {
    log('error', '[createVirtualPostFromReference] Failed to construct virtual post:', {
      error,
      refId: fullRefId,
      refType: ref.fields['social:ref-type']
    });
    return null;
  }
}

/**
 * Merge virtual post interaction data into existing workspace post
 * Simplified: removed unused variables
 */
export function mergeVirtualPostIntoWorkspace(
  workspacePost: Post,
  ref: { fields: Record<string, string> }
): void {
  // Ensure interactions object exists
  if (!workspacePost.interactions) {
    workspacePost.interactions = { comments: 0, reposts: 0, quotes: 0 };
  }

  // Extract interaction counts from the reference if available
  const refType = ref.fields['social:ref-type'];

  // Merge interaction counts TO the post (interactions ON this post)
  if (refType === 'comment') {
    workspacePost.interactions.comments++;
    log('debug', '[mergeVirtualPost] Incremented comment count on workspace post');
  } else if (refType === 'repost') {
    workspacePost.interactions.reposts++;
    workspacePost.display.totalReposts = workspacePost.interactions.reposts + workspacePost.interactions.quotes;
    log('debug', '[mergeVirtualPost] Incremented repost count on workspace post');
  } else if (refType === 'quote') {
    workspacePost.interactions.quotes++;
    workspacePost.display.totalReposts = workspacePost.interactions.reposts + workspacePost.interactions.quotes;
    log('debug', '[mergeVirtualPost] Incremented quote count on workspace post');
  }

  // Note: Reference normalization is handled in processPostWithReferences
}

export async function processCommits(workdir: string, commits: Commit[]): Promise<Post[]> {
  const posts: Post[] = [];
  const gitSocialBranch = await getConfiguredBranch(workdir);
  const currentBranchResult = await getCurrentBranch(workdir);
  const currentBranch = currentBranchResult.success ? currentBranchResult.data : null;

  const remotesResult = await listRemotes(workdir);
  const remoteUrlMap = new Map<string, string>();
  if (remotesResult.success && remotesResult.data) {
    for (const remote of remotesResult.data) {
      if (remote.name && remote.url) {
        remoteUrlMap.set(remote.name, remote.url);
      }
    }
  }

  const hasOriginRemote = remoteUrlMap.has('origin');
  let unpushedCommits: Set<string> | null = null;
  if (hasOriginRemote) {
    unpushedCommits = await getUnpushedCommits(workdir, gitSocialBranch);
  }

  // Track processed commits to avoid duplicates
  const processedHashes = new Set<string>();

  for (const commit of commits) {
    // Skip if we've already processed this commit
    if (processedHashes.has(commit.hash)) {
      log('debug', '[processCommits] Skipping duplicate commit:', gitMsgHash.truncate(commit.hash, 8), 'from', commit.refname);
      continue;
    }

    const extendedCommit = commit as Commit & {
      __external?: { repoUrl: string; storageDir: string; branch: string }
    };
    const external = extendedCommit.__external;
    if (external) {
      processedHashes.add(commit.hash);
      const post = constructPost(
        {
          hash: commit.hash,
          author: commit.author,
          email: commit.email,
          timestamp: commit.timestamp,
          message: commit.message,
          refname: commit.refname,
          repository: external.storageDir,
          repositoryIdentifier: external.repoUrl,
          branch: `remotes/upstream/${external.branch}`,
          isGitSocialBranch: true,
          hasOriginRemote: false,
          unpushedCommits: null,
          remoteName: 'upstream'
        },
        undefined
      );
      if (post) {posts.push(post);}
      continue;
    }

    let branchRepoUrl = workdir;
    let branch = gitSocialBranch;
    let isGitSocialBranch = false;
    let remoteName: string | undefined;

    if (commit.refname) {
      if (commit.refname.startsWith('refs/remotes/')) {
        const parts = commit.refname.substring('refs/remotes/'.length).split('/');
        if (parts.length >= 2) {
          remoteName = parts[0];
          const branchName = parts.slice(1).join('/');
          branch = `remotes/${remoteName}/${branchName}`;

          if (remoteName && remoteUrlMap.has(remoteName)) {
            branchRepoUrl = gitMsgUrl.normalize(remoteUrlMap.get(remoteName)!);
          }

          if (remoteName === 'origin' && branchName === gitSocialBranch) {
            isGitSocialBranch = true;
          } else {
            continue;
          }
        }
      } else if (commit.refname.startsWith('refs/heads/')) {
        branch = commit.refname.substring('refs/heads/'.length);
        if (branch !== gitSocialBranch) {continue;}
        isGitSocialBranch = true;
      } else if (commit.refname === gitSocialBranch) {
        isGitSocialBranch = true;
        branch = commit.refname;
      }
    } else {
      isGitSocialBranch = currentBranch === gitSocialBranch;
    }

    // Add hash to processed set to prevent duplicates
    processedHashes.add(commit.hash);

    const post = constructPost(
      {
        hash: commit.hash,
        author: commit.author,
        email: commit.email,
        timestamp: commit.timestamp,
        message: commit.message,
        refname: commit.refname,
        repository: workdir,
        repositoryIdentifier: branchRepoUrl,
        branch,
        isGitSocialBranch,
        hasOriginRemote,
        unpushedCommits,
        remoteName
      },
      undefined
    );

    if (post) {posts.push(post);}
  }

  return posts;
}
