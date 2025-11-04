/// <reference lib="dom" />

import type { ExtensionMessage, WebviewMessage } from '../handlers';
import type { Post } from '@gitsocial/core/client';
import { handleExtensionMessage } from './stores';

declare global {
  interface Window {
    vscode: {
      postMessage(message: unknown): void;
      getState(): unknown;
      setState(state: unknown): void;
    };
  }
}

class WebviewAPI {
  constructor() {
    // Listen for messages from extension
    window.addEventListener('message', (event: MessageEvent) => {
      const message = event.data as ExtensionMessage;
      handleExtensionMessage(message);
    });
  }

  postMessage(message: WebviewMessage): void {
    window.vscode.postMessage(message);
  }

  ready(): void {
    this.postMessage({ type: 'ready' });
  }

  getPosts(options?: {
    repository?: string;
    branch?: string;
    limit?: number;
    since?: string;
    until?: string;
    listId?: string;
    types?: Array<'post' | 'quote' | 'comment' | 'repost'> | 'all';
    sources?: Array<'implicit' | 'explicit'>;
    scope?: string; // Support all scope patterns: 'repository:my', 'timeline', 'repository:<url>', 'list:<name>', etc.
    skipCache?: boolean;
  }, requestId?: string): string {
    const id = requestId || `getPosts-${Date.now()}-${Math.random()}`;
    this.postMessage({ type: 'social.getPosts', options, id });
    return id;
  }

  getLists(repository?: string): void {
    this.postMessage({ type: 'list.getAll', repository });
  }

  getListsWithId(repository: string, requestId: string): void {
    this.postMessage({ type: 'list.getAll', repository, id: requestId });
  }

  createPost(content: string): void {
    this.postMessage({ type: 'social.createPost', content });
  }

  createInteraction(type: 'comment' | 'repost' | 'quote', targetPost: Post, content?: string): void {
    this.postMessage({
      type: 'social.createInteraction',
      interactionType: type,
      targetPost,
      content
    });
  }

  refresh(): void {
    this.postMessage({ type: 'refresh' });
  }

  openView(viewType: string, title: string, params?: Record<string, unknown>): void {
    this.postMessage({ type: 'openView', viewType, title, params });
  }

  openExternal(url: string): void {
    this.postMessage({ type: 'openExternal', url });
  }

  fetchListRepositories(listId: string, repository?: string): void {
    this.postMessage({ type: 'fetchListRepositories', listId, repository });
  }

  searchPosts(query: string, limit?: number): void {
    this.postMessage({ type: 'social.searchPosts', query, options: limit ? { limit } : undefined });
  }

  checkRepositoryStatus(repository: string): void {
    this.postMessage({ type: 'checkRepositoryStatus', repository });
  }

  createList(listId: string, name: string): void {
    this.postMessage({ type: 'list.create', listId, name });
  }

  createListWithId(listId: string, name: string, requestId: string): void {
    this.postMessage({ type: 'list.create', listId, name, id: requestId });
  }

  renameList(listId: string, newName: string): void {
    this.postMessage({ type: 'list.rename', listId, newName });
  }

  deleteList(listId: string, listName?: string): void {
    this.postMessage({ type: 'list.delete', listId, listName });
  }

  followList(sourceRepository: string, sourceListId: string, targetListId?: string): void {
    this.postMessage({ type: 'list.follow', sourceRepository, sourceListId, targetListId });
  }

  syncList(listId: string): void {
    this.postMessage({ type: 'list.sync', listId });
  }

  unfollowList(listId: string): void {
    this.postMessage({ type: 'list.unfollow', listId });
  }

  addRepository(listId: string, repository: string, branch?: string): void {
    this.postMessage({ type: 'addRepository', listId, repository, branch });
  }

  addRepositoryWithId(listId: string, repository: string, requestId: string, branch?: string): void {
    this.postMessage({ type: 'addRepository', listId, repository, branch, id: requestId });
  }

  removeRepository(listId: string, repository: string): void {
    this.postMessage({ type: 'removeRepository', listId, repository });
  }

  fetchRepositories(): void {
    this.postMessage({ type: 'fetchRepositories' });
  }

  getRepositories(scope: string): void {
    this.postMessage({ type: 'getRepositories', scope });
  }

  fetchSpecificRepositories(repositoryIds: string[], since?: string): void {
    this.postMessage({ type: 'fetchSpecificRepositories', repositoryIds, since });
  }

  pushToRemote(remoteName = 'origin'): void {
    this.postMessage({ type: 'pushToRemote', remoteName });
  }

  fetchUpdates(repository: string): void {
    this.postMessage({ type: 'fetchUpdates', repository });
  }

  checkGitSocialInit(id?: string): void {
    this.postMessage({ type: 'checkGitSocialInit', id });
  }

  initializeRepository(id: string, workspaceUri: string, branch: string, remote?: string): void {
    this.postMessage({ type: 'initializeRepository', id, params: { config: { workspaceUri, branch, remote } } });
  }

  closePanel(): void {
    this.postMessage({ type: 'closePanel' });
  }

  toggleZenMode(): void {
    this.postMessage({ type: 'toggleZenMode' });
  }

  getUnpushedCounts(): void {
    this.postMessage({ type: 'getUnpushedCounts' });
  }

  getUnpushedListsCount(): void {
    this.postMessage({ type: 'getUnpushedListsCount' });
  }

  getLogs(options?: {
    repository?: string;
    since?: string;
    until?: string;
    limit?: number;
    scope?: 'repository:my' | 'timeline'; // Logs are limited to my operations, not other repositories
  }, requestId?: string): string {
    const id = requestId || `getLogs-${Date.now()}-${Math.random()}`;
    this.postMessage({ type: 'getLogs', options, id });
    return id;
  }

  getFollowers(requestId?: string): string {
    const id = requestId || `getFollowers-${Date.now()}-${Math.random()}`;
    this.postMessage({ type: 'getFollowers', id });
    return id;
  }

  updatePanelIcon(postAuthor: { email: string; repository: string }): void {
    this.postMessage({ type: 'updatePanelIcon', postAuthor });
  }

  updatePanelTitle(title: string): void {
    this.postMessage({ type: 'updatePanelTitle', title });
  }

  clearCache(): void {
    this.postMessage({ type: 'clearCache' });
  }

  getNotifications(options?: { since?: string; until?: string; limit?: number }, requestId?: string): string {
    const id = requestId || `getNotifications-${Date.now()}-${Math.random()}`;
    this.postMessage({ type: 'getNotifications', options, id });
    return id;
  }

  getSettings(key: string): void {
    this.postMessage({ type: 'getSettings', key });
  }

  updateSettings(key: string, value: unknown): void {
    this.postMessage({ type: 'updateSettings', key, value });
  }

}

export const api = new WebviewAPI();
