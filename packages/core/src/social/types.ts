/**
 * Core social feature types
 */

import type { Commit } from '../git/types';
import type { GitMsgMessage } from '../gitmsg/types';

/**
 * Post represents a social post (implicit or explicit)
 */
export interface Post {
  id: string; // GitMsg ref: "https://github.com/user/repo#commit:hash" or "#commit:hash"
  repository: string; // Repository identifier (url#branch format) - may be redundant with ID
  branch?: string; // Branch name for implicit posts
  author: {
    name: string;
    email: string;
  };
  timestamp: Date;
  content: string; // First line is subject, rest is body
  type: 'post' | 'comment' | 'repost' | 'quote';
  source: 'explicit' | 'implicit'; // explicit = GitSocial format, implicit = regular commit
  raw: {
    commit: Commit;
    gitMsg?: GitMsgMessage;
  };

  // Additional fields
  cleanContent?: string; // Content without GitMsg metadata (used in search)

  // All references use full GitMsg refs (IDs only, no nested objects)
  originalPostId?: string; // Full ref to original post (for comments/reposts/quotes)
  parentCommentId?: string; // Full ref to parent comment (for nested comments)

  // Interaction counts
  interactions?: {
    comments: number;
    reposts: number;
    quotes: number;
  };

  // Remote name if post is from a remote repository (e.g., "origin", "upstream")
  remote?: string;

  // Virtual post indicator
  isVirtual?: boolean;           // True for posts created from references

  // Workspace post identification (set at creation time)
  isWorkspacePost?: boolean;     // True if this post is from the workspace repository

  // Pre-computed display values and UI state
  display: {
    // Computed values
    repositoryName: string;      // Display-friendly repository name (e.g., "myproject" or "My Repository")
    commitHash: string;          // 12-character commit hash
    commitUrl: string | null;    // URL to view commit on hosting platform
    totalReposts: number;        // Combined reposts + quotes count

    // UI state
    isEmpty: boolean;            // True for simple reposts without additional content
    isUnpushed: boolean;         // True if post exists in my repository but not on origin remote
    isOrigin: boolean;           // True if from origin remote (shows home icon)
    isWorkspacePost: boolean;    // Same as root level, for UI convenience
  };
}

/**
 * Thread context for hierarchical thread display
 */
export interface ThreadContext {
  anchorPost: Post;           // The main post being viewed (depth = 0)
  parentPosts: Post[];        // Posts above anchor (ancestors, depth < 0)
  childPosts: Post[];         // Posts below anchor (replies, depth > 0)
  threadRootId: string;       // Original post ID
  hasMoreParents: boolean;    // For pagination
  hasMoreChildren: boolean;   // For pagination
}

/**
 * Thread item for rendering
 */
export interface ThreadItem {
  type: 'post' | 'anchor' | 'skeleton' | 'blocked' | 'notFound';
  key: string;
  depth: number;              // <0 for parents, 0 for anchor, >0 for children
  data?: Post;
  hasChildren?: boolean;       // True if this post has replies
}

/**
 * Thread sort options
 */
export type ThreadSort = 'top' | 'oldest' | 'latest';

export type NotificationType = 'comment' | 'repost' | 'quote' | 'follow';

/**
 * List represents a collection of repositories using state-based JSON storage
 */
export interface List {
  version: string; // List format version (e.g., "0.1.0")
  id: string; // List identifier (e.g., "reading") - matches [a-zA-Z0-9_-]{1,40}
  name: string; // Human-readable name (e.g., "Reading")
  repositories: string[]; // Array of "url#branch:name" strings (e.g., "https://github.com/user/repo#branch:main")
  isUnpushed?: boolean; // Runtime flag (not stored in JSON)
  isFollowedLocally?: boolean; // Runtime flag for remote lists (not stored in JSON)

  // Source list reference (present = followed list, stored in JSON)
  source?: string; // Format: "url#list:<list-id>"
}

/**
 * GitSocial repository configuration
 */
export interface RepositoryConfig {
  version?: string;
  branch?: string;
  remote?: string;
  upstream?: string;
  social?: {
    enabled: boolean;
    branch?: string;
  };
}

/**
 * Unified Repository interface - single source of truth for repository data
 */
export interface Repository {
  // Identity
  id: string;           // Unique identifier (url#branch or filesystem path)
  url: string;          // Repository URL or filesystem path
  name: string;         // Display name (e.g., "owner/repo")

  // Location
  path?: string;        // Local filesystem path (if cloned)
  branch: string;       // Current/tracked branch
  defaultBranch?: string; // Repository's default branch

  // Status
  type: 'workspace' | 'other';
  socialEnabled: boolean;
  config?: RepositoryConfig;  // GitSocial configuration

  // Timestamps
  followedAt?: Date;    // When added to a list (if applicable)
  lastFetchTime?: Date; // When last fetched from remote
  fetchedRanges?: Array<{ start: string; end: string }>; // Date ranges (YYYY-MM-DD) that have been fetched
  lastSyncTime?: Date;  // When last synced

  // Relationships
  remoteName?: string;  // Git remote name (e.g., "gitsocial.list.repo")
  lists?: string[];     // Lists this repo belongs to
  // Origin remote info (for workspace repos)
  hasOriginRemote?: boolean;  // Whether workspace has origin remote configured
  originUrl?: string;          // URL of origin remote (if exists)

  // Statistics (optional, for future use)
  stats?: {
    postCount?: number;
    commentCount?: number;
    contributorCount?: number;
    lastActivityTime?: Date;
  };

  // Extensible metadata for future needs
  metadata?: Record<string, unknown>;
}

/**
 * Repository filter options
 */
export interface RepositoryFilter {
  types?: Array<'workspace' | 'other'>;
  socialEnabled?: boolean;
  limit?: number;
  skipCache?: boolean;
}

/**
 * Timeline entry combines posts and interactions
 */
export interface TimelineEntry {
  post: Post;
  interactions?: {
    comments: Post[];
    reposts: Post[];
    quotes: Post[];
  };
}

/**
 * Pagination options
 */
export interface PaginationOptions {
  limit?: number; // Number of items per page
  offset?: number; // Number of items to skip
  cursor?: string; // Cursor for cursor-based pagination
}

// ListOperation type removed - lists now use state-based JSON storage instead of event sourcing

/**
 * Unified result type for operations that can succeed or fail
 * Replaces both GitResult and SocialResult with consistent error handling
 */
export interface Result<T> {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
    details?: unknown;
  };
}

/**
 * Helper function to create a success result
 */
export function success<T>(data: T): Result<T> {
  return { success: true, data };
}

/**
 * Helper function to create an error result
 */
export function error<T>(code: string, message: string, details?: unknown): Result<T> {
  return { success: false, error: { code, message, details } };
}

/**
 * Log entry represents a git operation in the audit trail
 */
export interface LogEntry {
  hash: string;
  timestamp: Date;
  author: {
    name: string;
    email: string;
  };
  type: 'post' | 'comment' | 'repost' | 'quote' | 'list-create' | 'list-delete' |
        'repository-follow' | 'repository-unfollow' | 'config' | 'metadata';
  details: string;
  repository: string;
  postId?: string;
  raw?: {
    commit?: Commit;
    gitMsg?: GitMsgMessage;
  };
}
