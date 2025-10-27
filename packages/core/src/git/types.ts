/**
 * Core Git types for the GitMsg protocol implementation
 */

/** Git commit with metadata */
export interface Commit {
  hash: string;              // Full SHA-1 (40 chars)
  message: string;           // Full commit message
  author: string;
  email: string;
  timestamp: Date;
  refname?: string;          // Branch or remote reference (e.g., "refs/heads/main" or "refs/remotes/origin/main")
}

/** Options for creating commits */
export interface CommitOptions {
  message: string;
  allowEmpty?: boolean;      // Allow commits with no changes
  parent?: string;           // Parent commit hash for creating commit chains
}

// Re-export unified Result type
export type { Result } from '../social/types';
