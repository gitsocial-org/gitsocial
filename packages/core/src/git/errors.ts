/**
 * Git layer error codes and types
 */

/**
 * Git-specific error codes
 */
export const GIT_ERROR_CODES = {
  // Core git errors
  GIT_ERROR: 'GIT_ERROR',
  GIT_EXEC_ERROR: 'GIT_EXEC_ERROR',
  GIT_INIT_ERROR: 'GIT_INIT_ERROR',
  GIT_COMMIT_ERROR: 'GIT_COMMIT_ERROR',
  GIT_REF_ERROR: 'GIT_REF_ERROR',
  GIT_REMOTE_ERROR: 'GIT_REMOTE_ERROR',

  // Branch and checkout errors
  BRANCH_ERROR: 'BRANCH_ERROR',
  CHECKOUT_FAILED: 'CHECKOUT_FAILED',
  UNCOMMITTED_CHANGES: 'UNCOMMITTED_CHANGES',
  BRANCH_CREATE_ERROR: 'BRANCH_CREATE_ERROR',
  BRANCH_SWITCH_ERROR: 'BRANCH_SWITCH_ERROR'
} as const;

export type GitErrorCode = typeof GIT_ERROR_CODES[keyof typeof GIT_ERROR_CODES];

/**
 * Git error structure
 */
export interface GitError {
  code: GitErrorCode;
  message: string;
  details?: unknown;
}
