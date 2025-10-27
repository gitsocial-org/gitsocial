/**
 * Aggregated message types for webview communication
 */

import type {
  CacheMessages,
  CacheResponses,
  FollowersMessages,
  FollowersResponses,
  InteractionMessages,
  InteractionResponses,
  ListsMessages,
  ListsResponses,
  LogsMessages,
  LogsResponses,
  MiscMessages,
  MiscResponses,
  NotificationsMessages,
  NotificationsResponses,
  PostsMessages,
  PostsResponses,
  RepositoryMessages,
  RepositoryResponses
} from './index';

/**
 * Base message interface
 */
export interface BaseWebviewMessage {
  type: string;
  id?: string;
  [key: string]: unknown;
}

/**
 * Special webview messages that are handled directly by WebviewManager
 */
export type SpecialWebviewMessages =
  | { type: 'ready'; id?: string }
  | { type: 'openView'; id?: string; viewType: string; title: string; params?: Record<string, unknown> }
  | { type: 'updatePanelIcon'; postAuthor: { email: string; repository: string }; id?: string }
  | { type: 'updatePanelTitle'; title: string; id?: string }
  | { type: 'closePanel'; id?: string };

/**
 * Special extension messages
 */
export type SpecialExtensionMessages =
  | { type: 'error'; data: { message: string; details?: unknown }; requestId?: string }
  | { type: 'loading'; data: { value: boolean }; requestId?: string }
  | { type: 'repositoryInfo'; data: { url: string; name: string; branch: string }; requestId?: string }
  | { type: 'unpushedCounts'; data: { posts: number; comments: number; total: number }; requestId?: string }
  | { type: 'initialPost'; data: unknown; requestId?: string }
  | { type: 'interactionCreated'; data: { message: string; interactionType: string; interaction: unknown }; requestId?: string }
  | { type: 'avatar'; data: { identifier: string; url: string }; requestId?: string }
  | { type: 'searchResults'; data: { posts: unknown[] }; requestId?: string };

/**
 * Union of all webview messages
 */
export type WebviewMessage =
  | CacheMessages
  | FollowersMessages
  | PostsMessages
  | ListsMessages
  | LogsMessages
  | NotificationsMessages
  | RepositoryMessages
  | InteractionMessages
  | MiscMessages
  | SpecialWebviewMessages;

/**
 * Union of all extension messages
 */
export type ExtensionMessage =
  | CacheResponses
  | FollowersResponses
  | PostsResponses
  | ListsResponses
  | LogsResponses
  | NotificationsResponses
  | RepositoryResponses
  | InteractionResponses
  | MiscResponses
  | SpecialExtensionMessages;

/**
 * Log entry interface for repository action logs
 */
export interface LogEntry {
  hash: string;           // 12-char commit hash
  timestamp: Date;        // Commit timestamp
  author: {               // Author info
    name: string;
    email: string;
  };
  type: 'post' | 'comment' | 'repost' | 'quote' |
        'list-create' | 'list-delete' | 'repository-follow' | 'repository-unfollow' |
        'config' | 'metadata';
  details: string;        // Formatted action description
  repository: string;     // Repository context
  postId?: string;        // GitSocial post ID for navigation (for content entries)
  raw?: {                 // Raw data for navigation
    commit: unknown;
    gitMsg?: unknown;
  };
}
