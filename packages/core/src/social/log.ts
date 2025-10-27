/**
 * Log operations for GitSocial - Direct git access for complete audit trail
 *
 * Uses direct git operations to retrieve ALL git history including:
 * - Regular commits (implicit posts)
 * - GitMsg posts, comments, reposts, quotes
 * - GitSocial metadata operations (lists, config)
 * - refs/gitmsg/* operations
 */

import type { LogEntry, Result } from './types';
import type { Commit } from '../git/types';
import type { GitMsgMessage } from '../gitmsg/types';
import { git } from '../git';
import { gitMsg } from '../gitmsg';
import { gitMsgRef, gitMsgUrl } from '../gitmsg/protocol';
import { storage } from '../storage';
import { existsSync } from 'fs';
import { log as logger } from '../logger';

/**
 * Log namespace - Log management operations
 */
export const log = {
  getLogs,
  initialize
};

/**
 * Initialize the logs system with storage configuration
 * @deprecated Storage base should be passed via getLogs options
 */
function initialize(_config: { storageBase: string }): void {
  logger('debug', '[social.logs] Initialize called (deprecated - pass storageBase via getLogs options)');
}

async function getLogs(
  workdir: string,
  scope: string = 'repository:my',
  filter?: {
    since?: Date;
    until?: Date;
    limit?: number;
    types?: Array<LogEntry['type']>;
    storageBase?: string;
  }
): Promise<Result<LogEntry[]>> {
  try {
    logger('debug', '[social.log] getLogs called with:', {
      scope,
      since: filter?.since,
      until: filter?.until,
      limit: filter?.limit
    });

    // Parse scope to determine data source
    if (scope.startsWith('repository:') && scope !== 'repository:my') {
      // External repository - use storage layer
      logger('debug', '[social.log] Using external repository logs for scope:', scope);
      return await getExternalRepositoryLogs(scope, filter);
    } else {
      // Workspace repository - use local git operations
      logger('debug', '[social.log] Using workspace repository logs for scope:', scope);
      return await getWorkspaceRepositoryLogs(workdir, scope, filter);
    }
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'GET_LOGS_ERROR',
        message: error instanceof Error ? error.message : 'Failed to get logs'
      }
    };
  }
}

async function getExternalRepositoryLogs(
  scope: string,
  filter?: {
    since?: Date;
    until?: Date;
    limit?: number;
    types?: Array<LogEntry['type']>;
    storageBase?: string;
  }
): Promise<Result<LogEntry[]>> {
  // Extract repository URL from scope (e.g., 'repository:https://github.com/user/repo#branch:main')
  const repositoryUrl = scope.replace('repository:', '');

  // Use storage layer to get commits from external repository

  // Parse repository URL and branch
  const parsed = gitMsgRef.parseRepositoryId(repositoryUrl);
  if (!parsed) {
    throw new Error(`Invalid repository URL in scope: ${repositoryUrl}`);
  }

  const effectiveStorageBase = filter?.storageBase;
  if (!effectiveStorageBase) {
    logger('error', '[social.log] No storage base provided for external repository logs');
    return {
      success: false,
      error: {
        code: 'NO_STORAGE_BASE',
        message: 'Storage base not provided for external repository logs'
      }
    };
  }
  const storageDir = storage.path.getDirectory(effectiveStorageBase, parsed.repository);
  logger('debug', '[social.log] External repository storage dir:', storageDir);

  if (!existsSync(storageDir)) {
    return {
      success: false,
      error: {
        code: 'REPOSITORY_NOT_CLONED',
        message: `Repository not cloned: ${repositoryUrl}`
      }
    };
  }

  // Get commits using existing storage infrastructure
  const result = await storage.repository.getCommits(effectiveStorageBase, parsed.repository, {
    branch: parsed.branch,
    limit: filter?.limit,
    since: filter?.since,
    until: filter?.until
  });

  if (!result.success || !result.data) {
    return {
      success: false,
      error: result.error || {
        code: 'GET_COMMITS_FAILED',
        message: 'Failed to get commits from external repository'
      }
    };
  }

  const logEntries = await transformCommitsToLogEntries(result.data, repositoryUrl);

  logger('debug', '[social.log] Transformed commits to log entries:', {
    commitCount: result.data.length,
    logEntryCount: logEntries.length,
    dateRange: {
      since: filter?.since,
      until: filter?.until,
      firstEntry: logEntries[0]?.timestamp,
      lastEntry: logEntries[logEntries.length - 1]?.timestamp
    }
  });

  // Apply type filter if specified
  const filtered = filter?.types
    ? logEntries.filter(entry => filter.types!.includes(entry.type))
    : logEntries;

  return { success: true, data: filtered };
}

async function getWorkspaceRepositoryLogs(
  workdir: string,
  scope: string,
  filter?: {
    since?: Date;
    until?: Date;
    limit?: number;
    types?: Array<LogEntry['type']>;
    storageBase?: string;
  }
): Promise<Result<LogEntry[]>> {
  const gitSocialBranch = await git.getConfiguredBranch(workdir);

  // Get all refs/gitmsg/* refs for metadata operations
  const refs = await git.listRefs(workdir, 'social');
  const refPaths = refs.map((ref: string) => `refs/gitmsg/${ref}`);

  let commits: Commit[] = [];

  if (scope === 'repository:my') {
    // Repository scope: local branches + origin + GitSocial refs
    commits = await git.getCommits(workdir, {
      all: false,
      branch: gitSocialBranch,
      includeRefs: refPaths,
      since: filter?.since,
      until: filter?.until,
      limit: filter?.limit
    });
  } else if (scope === 'timeline') {
    // Timeline scope: all branches including remotes + GitSocial refs
    commits = await git.getCommits(workdir, {
      all: true,
      since: filter?.since,
      until: filter?.until,
      limit: filter?.limit
    });
  }

  // Transform commits to log entries
  const logEntries = await transformCommitsToLogEntries(commits, workdir);

  // Apply type filter if specified
  const filtered = filter?.types
    ? logEntries.filter(entry => filter.types!.includes(entry.type))
    : logEntries;

  return { success: true, data: filtered };
}

async function transformCommitsToLogEntries(commits: Commit[], repository: string): Promise<LogEntry[]> {
  const logEntries: LogEntry[] = [];

  // Build ref map for metadata operations
  const refMap = new Map<string, string>();
  for (const commit of commits) {
    if (commit.refname && commit.refname.startsWith('refs/gitmsg/')) {
      const ref = commit.refname.replace(/^refs\/gitmsg\//, '');
      refMap.set(commit.hash, ref);
    }
  }

  // Transform each commit
  for (const commit of commits) {
    try {
      const actionType = getActionType(commit, refMap);
      const gitMsgParsed = gitMsg.parseMessage(commit.message);

      // Handle list state changes specially - generate virtual follow/unfollow entries
      if (actionType === 'list-state-change') {
        const ref = refMap.get(commit.hash);
        if (ref && ref.startsWith('social/lists/')) {
          const listName = ref.replace('social/lists/', '');
          const virtualEntries = await generateVirtualRepositoryEntries(commit, ref, listName, repository);
          logEntries.push(...virtualEntries);
        }
      } else {
        // Regular log entry
        // Parse repository to get base URL without branch
        const parsedRepo = gitMsgRef.parseRepositoryId(repository);
        const baseRepository = parsedRepo?.repository || repository;

        const logEntry: LogEntry = {
          hash: commit.hash.substring(0, 12),
          timestamp: commit.timestamp,
          author: {
            name: commit.author,
            email: commit.email
          },
          type: actionType as LogEntry['type'],
          details: formatActionDetails(actionType, gitMsgParsed, commit),
          repository,
          postId: ['post', 'comment', 'repost', 'quote'].includes(actionType)
            ? gitMsgRef.create('commit', commit.hash, gitMsgUrl.validate(baseRepository) ? baseRepository : undefined)
            : undefined,
          raw: { commit, gitMsg: gitMsgParsed || undefined }
        };

        logEntries.push(logEntry);
      }
    } catch (error) {
      logger('warn', '[social.getLogs] Error processing commit for logs:', error);
      // Skip invalid commits
    }
  }

  // Sort by timestamp (newest first)
  logEntries.sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime());

  return logEntries;
}

function getActionType(commit: Commit, refMap: Map<string, string>): string {
  // Check if this commit is pointed to by any refs/gitmsg/* refs
  const ref = refMap.get(commit.hash);

  if (ref) {
    // This commit is a special GitSocial metadata operation
    if (ref.startsWith('social/lists/')) {
      // List operation - state-based storage, will generate virtual follow/unfollow events
      return 'list-state-change';
    } else if (ref === 'social/config') {
      return 'config';
    } else {
      return 'metadata';
    }
  }

  // Check GitMsg headers for interaction types (comment, repost, quote)
  const gitMsgParsed = gitMsg.parseMessage(commit.message);
  if (gitMsgParsed && gitMsgParsed.header.ext === 'social') {
    const socialType = gitMsgParsed.header.fields['type'];
    if (socialType && ['comment', 'repost', 'quote'].includes(socialType)) {
      return socialType;
    }
  }

  // Default to 'post' for commits without GitMsg headers (implicit posts)
  return 'post';
}

function formatActionDetails(type: string, gitMsg: GitMsgMessage | null, commit: Commit): string {
  // Use commit message as primary content source
  const primaryContent = commit.message?.split('\n')[0] || 'No message';

  switch (type) {
  case 'list-create': {
    // For list operations, the commit message contains JSON data directly
    try {
      const listData = JSON.parse(commit.message) as { name?: string; id?: string };
      return `Created list "${listData.name || listData.id || 'unknown'}"`;
    } catch {
      return 'Created list "unknown"';
    }
  }
  case 'list-delete':
    return 'Deleted list';
  case 'repository-follow': {
    const repo = gitMsg?.header?.fields['social:repository'] || 'unknown';
    const listName = gitMsg?.header?.fields['social:list'] || 'unknown';
    return `Added ${repo} to list "${listName}"`;
  }
  case 'repository-unfollow': {
    const repo = gitMsg?.header?.fields['social:repository'] || 'unknown';
    const listName = gitMsg?.header?.fields['social:list'] || 'unknown';
    return `Removed ${repo} from list "${listName}"`;
  }
  case 'config':
    return gitMsg?.content || primaryContent;
  case 'metadata':
    return gitMsg?.content || primaryContent;
  case 'post':
    // For posts, prefer GitMsg content if available, otherwise use commit message
    return gitMsg?.content?.split('\n')[0] || primaryContent;
  case 'comment': {
    const original = gitMsg?.references?.find((ref) => ref.fields['social:ref-type'] === 'original');
    const originalContent = original?.metadata?.split('\n')?.find((line: string) => line.startsWith('>'))?.substring(1)?.trim();
    return `Re: ${originalContent || 'post'}`;
  }
  case 'repost': {
    const original = gitMsg?.references?.find((ref) => ref.fields['social:ref-type'] === 'original');
    const originalContent = original?.metadata?.split('\n')?.find((line: string) => line.startsWith('>'))?.substring(1)?.trim();
    return `Repost: ${originalContent || 'post'}`;
  }
  case 'quote': {
    const original = gitMsg?.references?.find((ref) => ref.fields['social:ref-type'] === 'original');
    const originalContent = original?.metadata?.split('\n')?.find((line: string) => line.startsWith('>'))?.substring(1)?.trim();
    return `Quote: ${originalContent || 'post'}`;
  }
  default:
    return primaryContent;
  }
}

async function getPreviousListCommit(currentCommit: Commit, ref: string, workdir: string): Promise<Commit | null> {
  try {
    // Get the previous commit for this specific ref
    const refPath = `refs/gitmsg/${ref}`;
    const commits = await git.getCommits(workdir, {
      branch: refPath,
      limit: 2
    });

    // Find the commit before the current one
    return commits.find(c => c.hash !== currentCommit.hash) || null;
  } catch (error) {
    logger('debug', '[getPreviousListCommit] Failed to get previous commit:', error);
    return null;
  }
}

function parseListState(commit: Commit): { name?: string; repositories: string[] } {
  try {
    const listData = JSON.parse(commit.message) as { name?: string; repositories?: string[] };
    return {
      name: listData.name,
      repositories: listData.repositories || []
    };
  } catch (error) {
    logger('debug', '[parseListState] Failed to parse list state:', error);
    return { repositories: [] };
  }
}

function calculateRepositoryDiff(previous: string[], current: string[]): {
  added: string[];
  removed: string[];
} {
  const previousSet = new Set(previous);
  const currentSet = new Set(current);

  const added = current.filter(repo => !previousSet.has(repo));
  const removed = previous.filter(repo => !currentSet.has(repo));

  return { added, removed };
}

async function generateVirtualRepositoryEntries(
  commit: Commit,
  ref: string,
  listName: string,
  repository: string
): Promise<LogEntry[]> {
  const entries: LogEntry[] = [];

  try {
    // Parse current list state
    const currentState = parseListState(commit);

    // Get previous commit and parse its state
    const previousCommit = await getPreviousListCommit(commit, ref, repository);
    const previousState = previousCommit ? parseListState(previousCommit) : { repositories: [] };

    // Calculate what changed
    const { added, removed } = calculateRepositoryDiff(previousState.repositories, currentState.repositories);

    // Generate virtual entries for added repositories (follow events)
    for (const repoUrl of added) {
      entries.push({
        hash: commit.hash.substring(0, 12),
        timestamp: commit.timestamp,
        author: {
          name: commit.author,
          email: commit.email
        },
        type: 'repository-follow',
        details: `Added ${repoUrl} to list "${listName}"`,
        repository,
        raw: { commit, gitMsg: undefined }
      });
    }

    // Generate virtual entries for removed repositories (unfollow events)
    for (const repoUrl of removed) {
      entries.push({
        hash: commit.hash.substring(0, 12),
        timestamp: commit.timestamp,
        author: {
          name: commit.author,
          email: commit.email
        },
        type: 'repository-unfollow',
        details: `Removed ${repoUrl} from list "${listName}"`,
        repository,
        raw: { commit, gitMsg: undefined }
      });
    }

    // If no repositories changed, show it as a list update
    if (added.length === 0 && removed.length === 0) {
      entries.push({
        hash: commit.hash.substring(0, 12),
        timestamp: commit.timestamp,
        author: {
          name: commit.author,
          email: commit.email
        },
        type: 'list-create',
        details: `Updated list "${listName}"`,
        repository,
        raw: { commit, gitMsg: undefined }
      });
    }

  } catch (error) {
    logger('warn', '[generateVirtualRepositoryEntries] Error generating virtual entries:', error);
    // Fallback to basic list entry
    entries.push({
      hash: commit.hash.substring(0, 12),
      timestamp: commit.timestamp,
      author: {
        name: commit.author,
        email: commit.email
      },
      type: 'list-create',
      details: `Updated list "${listName}"`,
      repository,
      raw: { commit, gitMsg: undefined }
    });
  }

  return entries;
}
