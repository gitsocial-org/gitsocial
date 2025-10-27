/**
 * Git operations module exports
 */

import * as exec from './exec';
import * as operations from './operations';
import * as remotes from './remotes';

// Export namespace - MAIN EXPORT
export const git = {
  // From exec
  execGit: exec.execGit,

  // From operations
  isGitRepository: operations.isGitRepository,
  initGitRepository: operations.initGitRepository,
  getCommits: operations.getCommits,
  getCommit: operations.getCommit,
  createCommit: operations.createCommit,
  createCommitOnBranch: operations.createCommitOnBranch,
  getUnpushedCommits: operations.getUnpushedCommits,
  readGitRef: operations.readGitRef,
  writeGitRef: operations.writeGitRef,
  listRefs: operations.listRefs,
  getCurrentBranch: operations.getCurrentBranch,
  getConfiguredBranch: operations.getConfiguredBranch,
  validatePushPreconditions: operations.validatePushPreconditions,
  mergeBranch: operations.mergeBranch,

  // From remotes
  addRemote: remotes.addRemote,
  configureRemote: remotes.configureRemote,
  fetchRemote: remotes.fetchRemote,
  removeRemote: remotes.removeRemote,
  listRemotes: remotes.listRemotes,
  getRemoteConfig: remotes.getRemoteConfig,
  getOriginUrl: remotes.getOriginUrl
};

// Export types explicitly
export type { Commit, CommitOptions, Result } from './types';

// Export errors explicitly
export { GIT_ERROR_CODES, type GitErrorCode, type GitError } from './errors';
