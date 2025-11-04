/**
 * Git operations wrapper for executing git commands
 */

import type { Commit, CommitOptions, Result } from './types';
import { GIT_ERROR_CODES as ERROR_CODES } from './errors';
import { execGit } from './exec';
import { log } from '../logger';

// ASCII separators for safe parsing
const RECORD_SEP = '\x1E';
const UNIT_SEP = '\x1F';

/**
 * Execute a git command and return the result
 */
async function executeGit(workdir: string, args: string[]): Promise<Result<string>> {
  const result = await execGit(workdir, args);

  if (result.success && result.data) {
    return {
      success: true,
      data: result.data.stdout
    };
  } else {
    return {
      success: false,
      error: result.error || {
        code: ERROR_CODES.GIT_ERROR,
        message: 'Git command failed'
      }
    };
  }
}

/**
 * Check if a directory is a git repository
 */
export async function isGitRepository(workdir: string): Promise<boolean> {
  const result = await execGit(workdir, ['rev-parse', '--git-dir']);
  return result.success;
}

/**
 * Initialize a new git repository
 */
export async function initGitRepository(workdir: string, initialBranch?: string): Promise<Result<void>> {
  const args = ['init'];
  if (initialBranch) {
    args.push('-b', initialBranch);
  }

  const result = await execGit(workdir, args);
  if (result.success) {
    return { success: true };
  } else {
    return {
      success: false,
      error: result.error || {
        code: ERROR_CODES.GIT_INIT_ERROR,
        message: 'Failed to initialize git repository'
      }
    };
  }
}

/**
 * Get commits from a reference
 */
export async function getCommits(
  workdir: string,
  options?: {
    branch?: string;
    limit?: number;
    since?: Date;
    until?: Date;
    all?: boolean;  // Fetch from all branches and remotes
    includeRefs?: string[];  // Additional refs to include (e.g., ['refs/gitmsg/social/lists/*'])
  }
): Promise<Commit[]> {
  log('debug', '[getCommits] Called with options:', {
    branch: options?.branch,
    all: options?.all,
    limit: options?.limit,
    since: options?.since,
    until: options?.until,
    includeRefs: options?.includeRefs
  });

  const limit = options?.limit || 10000;
  // Format uses abbreviated hash (%h) with consistent 12-character length
  // Using ASCII separators to avoid conflicts with message content
  // Use committer date (%cd) instead of author date (%ad) to match --since/--until filtering
  // Use %B (raw body) to preserve exact message formatting including newlines
  const format = `${RECORD_SEP}%h${UNIT_SEP}%cd${UNIT_SEP}%an${UNIT_SEP}%ae${UNIT_SEP}%B${UNIT_SEP}%S`;
  const args = ['log'];

  // Use --all flag if requested, otherwise use specific branch or HEAD
  if (options?.all) {
    // Exclude GitSocial config refs to prevent config commits from appearing in timeline
    args.push('--exclude=refs/gitmsg/social/config');
    args.push('--all');
  } else {
    // Add the main branch/HEAD
    args.push(options?.branch || 'HEAD');

    // Add any additional refs
    if (options?.includeRefs) {
      args.push(...options.includeRefs);
    }
  }

  args.push(
    `--max-count=${limit}`,
    `--format=${format}`,
    '--abbrev=12',           // Ensure consistent 12-character hashes
    '--no-merges',
    '--date=iso-strict'
    // Removed --author-date-order since we're using committer dates now
  );

  if (options?.since) {
    // Format date in local time to avoid timezone issues
    const year = options.since.getFullYear();
    const month = String(options.since.getMonth() + 1).padStart(2, '0');
    const day = String(options.since.getDate()).padStart(2, '0');
    args.push(`--since=${year}-${month}-${day}`);
  }

  if (options?.until) {
    // Add 1 day to include the entire last day (git's --until uses start of day)
    const untilDate = new Date(options.until);
    untilDate.setDate(untilDate.getDate() + 1);
    const year = untilDate.getFullYear();
    const month = String(untilDate.getMonth() + 1).padStart(2, '0');
    const day = String(untilDate.getDate()).padStart(2, '0');
    args.push(`--until=${year}-${month}-${day}`);
  }

  log('debug', '[getCommits] Git command args:', args);

  const result = await executeGit(workdir, args);
  if (!result.success || !result.data) {
    log('debug', '[getCommits] Git command failed or no data');
    return [];
  }

  // Split by record separator and parse each commit
  const commits = result.data
    .split(RECORD_SEP)
    .filter(entry => entry.trim())
    .map(entry => {
      const parts = entry.split(UNIT_SEP);
      if (parts.length < 6) {return null;}

      const [hash, date, author, email, message, refname] = parts;

      return {
        hash: (hash || '').trim(),
        message: message || '',
        author: (author || '').trim(),
        email: (email || '').trim(),
        timestamp: new Date(date || ''),
        refname: (refname || '').trim()
      };
    })
    .filter(Boolean) as Commit[];

  log('debug', '[getCommits] Found commits:', commits.length);
  log('debug', '[getCommits] Sample commits:', commits.slice(0, 3).map(c => ({
    hash: c.hash.substring(0, 8),
    refname: c.refname,
    subject: c.message?.split('\n')[0]?.substring(0, 50) || ''
  })));

  return commits;
}

/**
 * Create a new commit
 */
export async function createCommit(
  workdir: string,
  options: CommitOptions
): Promise<Result<string>> {
  // If parent is specified, create a commit directly without checking working directory
  if (options.parent) {
    // Use the empty tree for list operations (no files)
    const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';

    const commitArgs = ['commit-tree', EMPTY_TREE, '-m', options.message, '-p', options.parent];
    const commitResult = await executeGit(workdir, commitArgs);

    if (!commitResult.success || !commitResult.data) {
      return {
        success: false,
        error: commitResult.error || {
          code: ERROR_CODES.GIT_COMMIT_ERROR,
          message: 'Failed to create commit'
        }
      };
    }

    return {
      success: true,
      data: commitResult.data.trim()
    };
  }

  // First, check if there are changes to commit
  const statusResult = await executeGit(workdir, ['status', '--porcelain']);

  if (!options.allowEmpty && (!statusResult.data || statusResult.data.trim() === '')) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_COMMIT_ERROR,
        message: 'No changes to commit'
      }
    };
  }

  // Stage all changes
  await executeGit(workdir, ['add', '-A']);

  // Create commit
  const args = ['commit', '-m', options.message];
  if (options.allowEmpty) {
    args.push('--allow-empty');
  }

  const result = await executeGit(workdir, args);
  if (!result.success) {
    return result;
  }

  // Get the commit hash (abbreviated to 12 characters)
  const hashResult = await executeGit(workdir, ['rev-parse', '--short=12', 'HEAD']);

  return {
    success: hashResult.success,
    data: hashResult.data,
    error: hashResult.error
  };
}

/**
 * Create a commit on a specific branch without checking it out
 * Uses git commit-tree and update-ref to avoid disrupting the working directory
 */
export async function createCommitOnBranch(
  workdir: string,
  branch: string,
  message: string
): Promise<Result<string>> {
  const EMPTY_TREE = '4b825dc642cb6eb9a060e54bf8d69288fbee4904';
  const branchRef = `refs/heads/${branch}`;
  const checkBranchResult = await execGit(workdir, ['rev-parse', '--verify', '--quiet', branchRef]);
  if (checkBranchResult.success && checkBranchResult.data) {
    const parentHash = checkBranchResult.data.stdout.trim();
    const commitArgs = ['commit-tree', EMPTY_TREE, '-m', message, '-p', parentHash];
    const commitResult = await executeGit(workdir, commitArgs);
    if (!commitResult.success || !commitResult.data) {
      return {
        success: false,
        error: commitResult.error || {
          code: ERROR_CODES.GIT_COMMIT_ERROR,
          message: 'Failed to create commit'
        }
      };
    }
    const commitHash = commitResult.data.trim();
    const updateRefResult = await executeGit(workdir, ['update-ref', branchRef, commitHash]);
    if (!updateRefResult.success) {
      return {
        success: false,
        error: updateRefResult.error || {
          code: ERROR_CODES.GIT_COMMIT_ERROR,
          message: 'Failed to update branch reference'
        }
      };
    }
    const shortHashResult = await executeGit(workdir, ['rev-parse', '--short=12', commitHash]);
    return {
      success: true,
      data: shortHashResult.success && shortHashResult.data ? shortHashResult.data.trim() : commitHash.substring(0, 12)
    };
  } else {
    const commitArgs = ['commit-tree', EMPTY_TREE, '-m', message];
    const commitResult = await executeGit(workdir, commitArgs);
    if (!commitResult.success || !commitResult.data) {
      return {
        success: false,
        error: commitResult.error || {
          code: ERROR_CODES.GIT_COMMIT_ERROR,
          message: 'Failed to create initial commit'
        }
      };
    }
    const commitHash = commitResult.data.trim();
    const updateRefResult = await executeGit(workdir, ['update-ref', branchRef, commitHash]);
    if (!updateRefResult.success) {
      return {
        success: false,
        error: updateRefResult.error || {
          code: ERROR_CODES.GIT_COMMIT_ERROR,
          message: 'Failed to create branch reference'
        }
      };
    }
    const shortHashResult = await executeGit(workdir, ['rev-parse', '--short=12', commitHash]);
    return {
      success: true,
      data: shortHashResult.success && shortHashResult.data ? shortHashResult.data.trim() : commitHash.substring(0, 12)
    };
  }
}

/**
 * Read a git reference
 */
export async function readGitRef(
  workdir: string,
  ref: string
): Promise<Result<string>> {
  return executeGit(workdir, ['rev-parse', '--short=12', ref]);
}

/**
 * Get the list of commits that are on local branch but not on origin
 */
export async function getUnpushedCommits(
  workdir: string,
  branch: string
): Promise<Set<string>> {
  // Check if origin/branch exists
  const originRef = `origin/${branch}`;
  const checkResult = await executeGit(workdir, ['rev-parse', '--verify', '--quiet', originRef]);
  if (!checkResult.success) {
    // origin/branch doesn't exist, all commits on local branch are unpushed
    // Get all commits from the local branch
    const result = await executeGit(workdir, ['rev-list', '--abbrev-commit', '--abbrev=12', branch]);
    if (result.success && result.data) {
      const hashes = result.data.trim().split('\n').filter(h => h.length > 0);
      return new Set(hashes);
    }
    return new Set();
  }

  // Get commits that are on local branch but not on origin/branch
  // Using --abbrev=12 to get consistent 12-character hashes
  const result = await executeGit(workdir, ['rev-list', '--abbrev-commit', '--abbrev=12', `${originRef}..${branch}`]);
  if (result.success && result.data) {
    const hashes = result.data.trim().split('\n').filter(h => h.length > 0);
    return new Set(hashes);
  }
  return new Set();
}

/**
 * Write a git reference
 */
export async function writeGitRef(
  workdir: string,
  ref: string,
  value: string
): Promise<Result<void>> {
  log('debug', `[writeGitRef] Updating ref '${ref}' to '${value}' in ${workdir}`);

  const result = await executeGit(workdir, ['update-ref', ref, value]);

  if (!result.success) {
    log('error', `[writeGitRef] Failed to update ref '${ref}':`, {
      error: result.error,
      workdir,
      ref,
      value
    });
  } else {
    log('debug', `[writeGitRef] Successfully updated ref '${ref}' to '${value}'`);
  }

  return {
    success: result.success,
    error: result.error
  };
}

/**
 * Get current branch name
 */
export async function getCurrentBranch(
  workdir: string
): Promise<Result<string>> {
  return executeGit(workdir, ['rev-parse', '--abbrev-ref', 'HEAD']);
}

/**
 * Get a single commit by hash
 */
export async function getCommit(
  workdir: string,
  hash: string
): Promise<Commit | null> {
  const commits = await getCommits(workdir, {
    branch: hash,
    limit: 1
  });
  return commits[0] || null;
}

/**
 * List all refs under a GitMsg namespace
 * @param workdir - Working directory path
 * @param namespace - Namespace under refs/gitmsg/ (optional)
 * @returns Array of ref names relative to refs/gitmsg/
 */
export async function listRefs(
  workdir: string,
  namespace: string = ''
): Promise<string[]> {
  // Ensure namespace doesn't start with refs/gitmsg/
  const cleanNamespace = namespace.replace(/^refs\/gitmsg\//, '');
  const pattern = cleanNamespace ? `refs/gitmsg/${cleanNamespace}` : 'refs/gitmsg/';

  const result = await execGit(workdir, [
    'for-each-ref',
    '--format=%(refname)',
    pattern
  ]);

  if (!result.success || !result.data?.stdout.trim()) {
    return [];
  }

  // Return paths relative to refs/gitmsg/
  return result.data.stdout
    .trim()
    .split('\n')
    .map(ref => ref.replace(/^refs\/gitmsg\//, ''));
}

/**
 * Get the configured GitSocial branch
 * Returns the branch name where all social content should be stored
 */
export async function getConfiguredBranch(workdir: string): Promise<string> {
  // 1. Check refs/gitmsg/social/config (source of truth)
  const refResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    'refs/gitmsg/social/config'
  ]);

  if (refResult.success && refResult.data) {
    const commitHash = refResult.data.stdout.trim();
    const messageResult = await execGit(workdir, [
      'log',
      '-1',
      '--format=%B',
      commitHash
    ]);

    if (messageResult.success && messageResult.data) {
      try {
        const config = JSON.parse(messageResult.data.stdout.trim()) as { branch?: string };
        if (config.branch) {
          log('debug', `[getConfiguredBranch] Using refs config: ${config.branch}`);
          return config.branch;
        }
      } catch {
        // Ignore JSON parse errors
      }
    }
  }

  // 2. Check if 'gitsocial' branch exists as convention
  const branchResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    'refs/heads/gitsocial'
  ]);

  if (branchResult.success) {
    log('debug', '[getConfiguredBranch] Using convention: gitsocial branch exists');
    return 'gitsocial';
  }

  // 3. Use default branch
  const defaultBranchResult = await execGit(workdir, [
    'symbolic-ref',
    'refs/remotes/origin/HEAD'
  ]);

  if (defaultBranchResult.success && defaultBranchResult.data) {
    const branch = defaultBranchResult.data.stdout.trim().replace('refs/remotes/origin/', '');
    log('debug', `[getConfiguredBranch] Using default branch: ${branch}`);
    return branch;
  }

  // 4. Fallback to 'main'
  log('debug', '[getConfiguredBranch] Using fallback: main');
  return 'main';
}

/**
 * Validate preconditions for pushing to remote
 * Checks for common issues that would cause push to fail
 */
export async function validatePushPreconditions(
  workdir: string,
  remoteName: string = 'origin'
): Promise<Result<void>> {
  // 1. Check if HEAD is detached
  const symbolicRefResult = await execGit(workdir, ['symbolic-ref', '-q', 'HEAD']);
  if (!symbolicRefResult.success) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_ERROR,
        message: 'Cannot push from detached HEAD. Checkout a branch first.'
      }
    };
  }

  // 2. Check if remote exists
  const remotesResult = await execGit(workdir, ['remote']);
  if (!remotesResult.success || !remotesResult.data) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_REMOTE_ERROR,
        message: 'Failed to list remotes'
      }
    };
  }

  const remotes = remotesResult.data.stdout.trim().split('\n').filter(Boolean);
  if (!remotes.includes(remoteName)) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_REMOTE_ERROR,
        message: `No '${remoteName}' remote configured. Add a remote first.`
      }
    };
  }

  // 3. Get configured branch (not current branch, as we'll push the configured one)
  const branch = await getConfiguredBranch(workdir);

  // 4. Check if local branch exists
  const localBranchResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    '--quiet',
    `refs/heads/${branch}`
  ]);

  if (!localBranchResult.success) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_ERROR,
        message: `Branch '${branch}' does not exist locally. Create a post first.`
      }
    };
  }

  // 5. Check if origin/branch exists to detect divergence
  const originBranchRef = `${remoteName}/${branch}`;
  const originBranchResult = await execGit(workdir, [
    'rev-parse',
    '--verify',
    '--quiet',
    originBranchRef
  ]);

  // If origin/branch exists, check for divergence
  if (originBranchResult.success) {
    const divergenceResult = await execGit(workdir, [
      'rev-list',
      '--left-right',
      '--count',
      `${originBranchRef}...${branch}`
    ]);

    if (divergenceResult.success && divergenceResult.data) {
      const output = divergenceResult.data.stdout.trim();
      const parts = output.split('\t').map(Number);
      const behind = parts[0];
      const ahead = parts[1];

      if (typeof behind === 'number' && typeof ahead === 'number' && behind > 0 && ahead > 0) {
        return {
          success: false,
          error: {
            code: ERROR_CODES.GIT_ERROR,
            message:
              `Branch '${branch}' has diverged (${ahead} ahead, ${behind} behind ${remoteName}). ` +
              'Fetch and merge first.'
          }
        };
      }
    }
  }

  return { success: true };
}

/**
 * Merge a branch into the current branch
 * For GitSocial: Always succeeds (empty commits = no conflicts)
 */
export async function mergeBranch(
  workdir: string,
  sourceBranch: string
): Promise<Result<void>> {
  const result = await execGit(workdir, ['merge', sourceBranch]);

  if (!result.success) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_ERROR,
        message: 'Failed to merge',
        details: result.error
      }
    };
  }

  return { success: true };
}
/**
 * Set upstream tracking branch for the current or specified branch
 */
export async function setUpstreamBranch(
  workdir: string,
  upstreamBranch: string,
  localBranch?: string
): Promise<Result<void>> {
  const args = ['branch', '--set-upstream-to', upstreamBranch];
  if (localBranch) {
    args.push(localBranch);
  }
  const result = await execGit(workdir, args);
  if (!result.success) {
    return {
      success: false,
      error: {
        code: ERROR_CODES.GIT_ERROR,
        message: 'Failed to set upstream branch',
        details: result.error
      }
    };
  }
  return { success: true };
}

/**
 * Get the upstream branch for the current or specified branch
 */
export async function getUpstreamBranch(
  workdir: string,
  localBranch?: string
): Promise<Result<string>> {
  const args = ['rev-parse', '--abbrev-ref'];
  if (localBranch) {
    args.push(`${localBranch}@{upstream}`);
  } else {
    args.push('@{upstream}');
  }
  return executeGit(workdir, args);
}
