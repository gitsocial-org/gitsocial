/**
 * Client-safe exports from GitSocial core
 * This module only exports browser/frontend-safe functionality with no Node.js dependencies
 */

// Export client-safe types only (from social/types and gitmsg/types)
export type { Post, List, Result, NotificationType } from '../social/types';
export type { Repository } from '../social/types';
export type { Notification } from '../social/notification';
export type { GitMsgHeader, GitMsgRef as GitMsgRefType, GitMsgMessage } from '../gitmsg/types';

// Export GitMsg protocol parsing functions (client-safe - pure TypeScript, no Node.js)
export { parseGitMsgMessage, parseGitMsgHeader, parseGitMsgRef, validateGitMsgMessage } from '../gitmsg/parser';
export { createGitMsgHeader, createGitMsgRef, formatGitMsgMessage } from '../gitmsg/writer';

// Export namespace objects for webview use (pure functions only)
export { gitMsgRef, gitMsgUrl, gitMsgHash } from '../gitmsg/protocol';
export { gitHost } from '../githost';

// Export client-safe date utilities
export * from '../utils/date';

// Note: GitMsgList, GitRepository, and Social require Node.js - access via message passing
