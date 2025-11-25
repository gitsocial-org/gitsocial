import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Post } from '@gitsocial/core/client';

vi.mock('../../../src/webview/stores', () => ({
  handleExtensionMessage: vi.fn()
}));

type WebviewAPIType = {
  ready: () => void;
  getPosts: (options?: Record<string, unknown>, requestId?: string) => string;
  getLists: (repository?: string) => void;
  getListsWithId: (repository: string, requestId: string) => void;
  createPost: (content: string) => void;
  createInteraction: (type: string, targetPost: Post, content?: string) => void;
  refresh: () => void;
  openView: (viewType: string, title: string, params?: Record<string, unknown>) => void;
  openExternal: (url: string) => void;
  fetchListRepositories: (listId: string, repository?: string) => void;
  searchPosts: (query: string, limit?: number) => void;
  checkRepositoryStatus: (repository: string) => void;
  createList: (listId: string, name: string) => void;
  createListWithId: (listId: string, name: string, requestId: string) => void;
  renameList: (listId: string, newName: string) => void;
  deleteList: (listId: string, listName?: string) => void;
  followList: (sourceRepository: string, sourceListId: string, targetListId?: string) => void;
  syncList: (listId: string) => void;
  unfollowList: (listId: string) => void;
  addRepository: (listId: string, repository: string, branch?: string) => void;
  addRepositoryWithId: (listId: string, repository: string, requestId: string, branch?: string) => void;
  removeRepository: (listId: string, repository: string) => void;
  fetchRepositories: () => void;
  getRepositories: (scope: string) => void;
  fetchSpecificRepositories: (repositoryIds: string[], since?: string) => void;
  pushToRemote: (remoteName?: string) => void;
  fetchUpdates: (repository: string) => void;
  checkGitSocialInit: (id?: string) => void;
  initializeRepository: (id: string, workspaceUri: string, branch: string, remote: string) => void;
  closePanel: () => void;
  toggleZenMode: () => void;
  getUnpushedCounts: () => void;
  getUnpushedListsCount: () => void;
  getLogs: (options: Record<string, unknown>, requestId?: string) => string;
  getFollowers: (requestId?: string) => string;
  updatePanelIcon: (postAuthor: Record<string, string>) => void;
  updatePanelTitle: (title: string) => void;
  clearCache: () => void;
  getNotifications: (options: Record<string, unknown>, requestId?: string) => string;
  getSettings: (key: string) => void;
  updateSettings: (key: string, value: unknown) => void;
};

describe('WebviewAPI', () => {
  let mockPostMessage: ReturnType<typeof vi.fn>;
  let api: WebviewAPIType;

  beforeEach(async () => {
    mockPostMessage = vi.fn();
    global.window = {
      vscode: {
        postMessage: mockPostMessage,
        getState: vi.fn(),
        setState: vi.fn()
      },
      addEventListener: vi.fn()
    } as unknown as Window & typeof globalThis;

    vi.resetModules();
    const apiModule = await import('../../src/webview/api');
    api = apiModule.api;
  });

  describe('ready', () => {
    it('posts ready message', () => {
      api.ready();
      expect(mockPostMessage).toHaveBeenCalledWith({ type: 'ready' });
    });
  });

  describe('getPosts', () => {
    it('posts getPosts message with options', () => {
      const options = { repository: 'test-repo', limit: 10 };
      const id = api.getPosts(options);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.getPosts',
        options,
        id
      });
      expect(id).toMatch(/^getPosts-/);
    });

    it('accepts custom requestId', () => {
      const id = api.getPosts({}, 'custom-id');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.getPosts',
        options: {},
        id: 'custom-id'
      });
      expect(id).toBe('custom-id');
    });

    it('works without options', () => {
      const id = api.getPosts();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.getPosts',
        options: undefined,
        id
      });
    });
  });

  describe('getLists', () => {
    it('posts getAll message', () => {
      api.getLists();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.getAll',
        repository: undefined
      });
    });

    it('accepts repository parameter', () => {
      api.getLists('test-repo');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.getAll',
        repository: 'test-repo'
      });
    });
  });

  describe('getListsWithId', () => {
    it('posts getAll message with requestId', () => {
      api.getListsWithId('test-repo', 'req-123');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.getAll',
        repository: 'test-repo',
        id: 'req-123'
      });
    });
  });

  describe('createPost', () => {
    it('posts createPost message', () => {
      api.createPost('Hello world');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.createPost',
        content: 'Hello world'
      });
    });
  });

  describe('createInteraction', () => {
    it('posts createInteraction message', () => {
      const targetPost = { id: 'post-1', content: 'Test' } as Post;
      api.createInteraction('comment', targetPost, 'Nice post');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.createInteraction',
        interactionType: 'comment',
        targetPost,
        content: 'Nice post'
      });
    });

    it('works without content', () => {
      const targetPost = { id: 'post-1', content: 'Test' } as Post;
      api.createInteraction('repost', targetPost);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.createInteraction',
        interactionType: 'repost',
        targetPost,
        content: undefined
      });
    });
  });

  describe('refresh', () => {
    it('posts refresh message', () => {
      api.refresh();
      expect(mockPostMessage).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('openView', () => {
    it('posts openView message', () => {
      api.openView('createPost', 'Create Post', { foo: 'bar' });
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'openView',
        viewType: 'createPost',
        title: 'Create Post',
        params: { foo: 'bar' }
      });
    });

    it('works without params', () => {
      api.openView('timeline', 'Timeline');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'openView',
        viewType: 'timeline',
        title: 'Timeline',
        params: undefined
      });
    });
  });

  describe('openExternal', () => {
    it('posts openExternal message', () => {
      api.openExternal('https://example.com');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'openExternal',
        url: 'https://example.com'
      });
    });
  });

  describe('fetchListRepositories', () => {
    it('posts fetchListRepositories message', () => {
      api.fetchListRepositories('list-1', 'repo-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchListRepositories',
        listId: 'list-1',
        repository: 'repo-1'
      });
    });

    it('works without repository', () => {
      api.fetchListRepositories('list-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchListRepositories',
        listId: 'list-1',
        repository: undefined
      });
    });
  });

  describe('searchPosts', () => {
    it('posts searchPosts message with limit', () => {
      api.searchPosts('test query', 50);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.searchPosts',
        query: 'test query',
        options: { limit: 50 }
      });
    });

    it('works without limit', () => {
      api.searchPosts('test query');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'social.searchPosts',
        query: 'test query',
        options: undefined
      });
    });
  });

  describe('checkRepositoryStatus', () => {
    it('posts checkRepositoryStatus message', () => {
      api.checkRepositoryStatus('repo-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'checkRepositoryStatus',
        repository: 'repo-1'
      });
    });
  });

  describe('createList', () => {
    it('posts create list message', () => {
      api.createList('list-1', 'My List');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.create',
        listId: 'list-1',
        name: 'My List'
      });
    });
  });

  describe('createListWithId', () => {
    it('posts create list message with requestId', () => {
      api.createListWithId('list-1', 'My List', 'req-123');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.create',
        listId: 'list-1',
        name: 'My List',
        id: 'req-123'
      });
    });
  });

  describe('renameList', () => {
    it('posts rename list message', () => {
      api.renameList('list-1', 'New Name');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.rename',
        listId: 'list-1',
        newName: 'New Name'
      });
    });
  });

  describe('deleteList', () => {
    it('posts delete list message', () => {
      api.deleteList('list-1', 'My List');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.delete',
        listId: 'list-1',
        listName: 'My List'
      });
    });

    it('works without listName', () => {
      api.deleteList('list-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.delete',
        listId: 'list-1',
        listName: undefined
      });
    });
  });

  describe('followList', () => {
    it('posts follow list message', () => {
      api.followList('source-repo', 'source-list', 'target-list');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.follow',
        sourceRepository: 'source-repo',
        sourceListId: 'source-list',
        targetListId: 'target-list'
      });
    });

    it('works without targetListId', () => {
      api.followList('source-repo', 'source-list');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.follow',
        sourceRepository: 'source-repo',
        sourceListId: 'source-list',
        targetListId: undefined
      });
    });
  });

  describe('syncList', () => {
    it('posts sync list message', () => {
      api.syncList('list-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.sync',
        listId: 'list-1'
      });
    });
  });

  describe('unfollowList', () => {
    it('posts unfollow list message', () => {
      api.unfollowList('list-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'list.unfollow',
        listId: 'list-1'
      });
    });
  });

  describe('addRepository', () => {
    it('posts addRepository message', () => {
      api.addRepository('list-1', 'repo-1', 'main');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'addRepository',
        listId: 'list-1',
        repository: 'repo-1',
        branch: 'main'
      });
    });

    it('works without branch', () => {
      api.addRepository('list-1', 'repo-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'addRepository',
        listId: 'list-1',
        repository: 'repo-1',
        branch: undefined
      });
    });
  });

  describe('addRepositoryWithId', () => {
    it('posts addRepository message with requestId', () => {
      api.addRepositoryWithId('list-1', 'repo-1', 'req-123', 'main');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'addRepository',
        listId: 'list-1',
        repository: 'repo-1',
        branch: 'main',
        id: 'req-123'
      });
    });
  });

  describe('removeRepository', () => {
    it('posts removeRepository message', () => {
      api.removeRepository('list-1', 'repo-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'removeRepository',
        listId: 'list-1',
        repository: 'repo-1'
      });
    });
  });

  describe('fetchRepositories', () => {
    it('posts fetchRepositories message', () => {
      api.fetchRepositories();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchRepositories'
      });
    });
  });

  describe('getRepositories', () => {
    it('posts getRepositories message', () => {
      api.getRepositories('all');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getRepositories',
        scope: 'all'
      });
    });
  });

  describe('fetchSpecificRepositories', () => {
    it('posts fetchSpecificRepositories message', () => {
      api.fetchSpecificRepositories(['repo-1', 'repo-2'], '2025-01-01');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchSpecificRepositories',
        repositoryIds: ['repo-1', 'repo-2'],
        since: '2025-01-01'
      });
    });

    it('works without since', () => {
      api.fetchSpecificRepositories(['repo-1']);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchSpecificRepositories',
        repositoryIds: ['repo-1'],
        since: undefined
      });
    });
  });

  describe('pushToRemote', () => {
    it('posts pushToRemote message with default origin', () => {
      api.pushToRemote();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'pushToRemote',
        remoteName: 'origin'
      });
    });

    it('accepts custom remote name', () => {
      api.pushToRemote('upstream');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'pushToRemote',
        remoteName: 'upstream'
      });
    });
  });

  describe('fetchUpdates', () => {
    it('posts fetchUpdates message', () => {
      api.fetchUpdates('repo-1');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'fetchUpdates',
        repository: 'repo-1'
      });
    });
  });

  describe('checkGitSocialInit', () => {
    it('posts checkGitSocialInit message', () => {
      api.checkGitSocialInit('id-123');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'checkGitSocialInit',
        id: 'id-123'
      });
    });

    it('works without id', () => {
      api.checkGitSocialInit();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'checkGitSocialInit',
        id: undefined
      });
    });
  });

  describe('initializeRepository', () => {
    it('posts initializeRepository message', () => {
      api.initializeRepository('id-123', '/workspace', 'main', 'origin');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'initializeRepository',
        id: 'id-123',
        params: {
          config: {
            workspaceUri: '/workspace',
            branch: 'main',
            remote: 'origin'
          }
        }
      });
    });
  });

  describe('closePanel', () => {
    it('posts closePanel message', () => {
      api.closePanel();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'closePanel'
      });
    });
  });

  describe('toggleZenMode', () => {
    it('posts toggleZenMode message', () => {
      api.toggleZenMode();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'toggleZenMode'
      });
    });
  });

  describe('getUnpushedCounts', () => {
    it('posts getUnpushedCounts message', () => {
      api.getUnpushedCounts();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getUnpushedCounts'
      });
    });
  });

  describe('getUnpushedListsCount', () => {
    it('posts getUnpushedListsCount message', () => {
      api.getUnpushedListsCount();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getUnpushedListsCount'
      });
    });
  });

  describe('getLogs', () => {
    it('posts getLogs message with options', () => {
      const options = { repository: 'repo-1', limit: 10 };
      const id = api.getLogs(options);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getLogs',
        options,
        id
      });
      expect(id).toMatch(/^getLogs-/);
    });

    it('accepts custom requestId', () => {
      const id = api.getLogs({}, 'custom-id');
      expect(id).toBe('custom-id');
    });
  });

  describe('getFollowers', () => {
    it('posts getFollowers message', () => {
      const id = api.getFollowers();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getFollowers',
        id
      });
      expect(id).toMatch(/^getFollowers-/);
    });

    it('accepts custom requestId', () => {
      const id = api.getFollowers('custom-id');
      expect(id).toBe('custom-id');
    });
  });

  describe('updatePanelIcon', () => {
    it('posts updatePanelIcon message', () => {
      api.updatePanelIcon({ email: 'test@example.com', repository: 'repo-1' });
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'updatePanelIcon',
        postAuthor: { email: 'test@example.com', repository: 'repo-1' }
      });
    });
  });

  describe('updatePanelTitle', () => {
    it('posts updatePanelTitle message', () => {
      api.updatePanelTitle('New Title');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'updatePanelTitle',
        title: 'New Title'
      });
    });
  });

  describe('clearCache', () => {
    it('posts clearCache message', () => {
      api.clearCache();
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'clearCache'
      });
    });
  });

  describe('getNotifications', () => {
    it('posts getNotifications message with options', () => {
      const options = { since: '2025-01-01', limit: 10 };
      const id = api.getNotifications(options);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getNotifications',
        options,
        id
      });
      expect(id).toMatch(/^getNotifications-/);
    });

    it('accepts custom requestId', () => {
      const id = api.getNotifications({}, 'custom-id');
      expect(id).toBe('custom-id');
    });
  });

  describe('getSettings', () => {
    it('posts getSettings message', () => {
      api.getSettings('autoLoadImages');
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'getSettings',
        key: 'autoLoadImages'
      });
    });
  });

  describe('updateSettings', () => {
    it('posts updateSettings message', () => {
      api.updateSettings('autoLoadImages', true);
      expect(mockPostMessage).toHaveBeenCalledWith({
        type: 'updateSettings',
        key: 'autoLoadImages',
        value: true
      });
    });
  });
});
