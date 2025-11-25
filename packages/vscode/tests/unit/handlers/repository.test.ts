import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const {
  mockSocial, mockGit, mockStorage, mockFetchTimeManager, mockGetUnpushedCounts, mockGitMsgRef,
  mockStoragePath, mockStorageRepository
} = vi.hoisted(() => ({
  mockSocial: {
    cache: { refresh: vi.fn() },
    list: {
      addRepositoryToList: vi.fn(),
      removeRepositoryFromList: vi.fn(),
      getList: vi.fn(),
      getLists: vi.fn(),
      getUnpushedListsCount: vi.fn()
    },
    post: { getPosts: vi.fn() },
    repository: {
      initializeRepository: vi.fn(),
      checkGitSocialInit: vi.fn(),
      getRepositories: vi.fn(),
      fetchRepositories: vi.fn(),
      fetchUpdates: vi.fn(),
      getConfig: vi.fn(),
      getRepositoryRelationship: vi.fn()
    },
    config: {
      initialize: vi.fn(),
      isInitialized: vi.fn(),
      getConfig: vi.fn()
    }
  },
  mockGit: {
    isGitRepository: vi.fn(),
    initGitRepository: vi.fn(),
    getConfig: vi.fn(),
    getCurrentBranch: vi.fn(),
    hasUnpushedCommits: vi.fn(),
    execGit: vi.fn()
  },
  mockStorage: {},
  mockFetchTimeManager: {
    getLastFetchTime: vi.fn(),
    updateFetchTime: vi.fn(),
    clearFetchTimes: vi.fn(),
    get: vi.fn()
  },
  mockGetUnpushedCounts: vi.fn(),
  mockGitMsgRef: {
    parse: vi.fn(),
    create: vi.fn(),
    normalize: vi.fn(),
    parseRepositoryId: vi.fn((id: string) => ({ repository: id.split('#')[0], branch: 'gitmsg/social' }))
  },
  mockStoragePath: { getDirectory: vi.fn() },
  mockStorageRepository: { readConfig: vi.fn() }
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('@gitsocial/core', () => ({
  social: mockSocial,
  git: mockGit,
  storage: {
    path: mockStoragePath,
    repository: mockStorageRepository
  },
  gitMsgRef: mockGitMsgRef,
  log: vi.fn()
}));

vi.mock('../../../src/utils/fetchTime', () => ({
  fetchTimeManager: mockFetchTimeManager
}));

vi.mock('../../../src/utils/unpushedCounts', () => ({
  getUnpushedCounts: mockGetUnpushedCounts
}));

vi.mock('../../../src/extension', () => ({
  getStorageUri: vi.fn(() => ({ fsPath: '/mock/storage' }))
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import { setRepositoryCallbacks } from '../../../src/handlers/repository';
import '../../../src/handlers/repository';

describe('repository.ts handlers', () => {
  let mockPanel: ReturnType<typeof createMockPanel>;
  let broadcastSpy: ReturnType<typeof vi.fn>;
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;

  beforeEach(() => {
    mockPanel = createMockPanel();
    broadcastSpy = vi.fn();
    setRepositoryCallbacks(broadcastSpy);
    resetAllMocks({ social: mockSocial, git: mockGit, storage: mockStorage });
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
  });

  afterEach(() => {
    vi.clearAllMocks();
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
  });

  describe('initializeRepository handler', () => {
    it('should initialize repository successfully with default configuration', async () => {
      const handler = getHandler('initializeRepository');
      expect(handler).toBeDefined();

      mockGit.isGitRepository.mockResolvedValue(true);
      mockGit.execGit.mockResolvedValue({ success: true, data: '' });
      mockSocial.repository.initializeRepository.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'initializeRepository',
        id: 'test-1',
        params: { config: {} }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalled();
      const lastCall = postMessage.mock.calls[postMessage.mock.calls.length - 1][0];
      expect(lastCall.type).toBe('repositoryInitialized');
      expect(lastCall.requestId).toBe('test-1');
    });

    it('should handle missing workspace folder error', async () => {
      const handler = getHandler('initializeRepository');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'initializeRepository',
        id: 'test-2',
        params: { config: {} }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'initializationError',
          requestId: 'test-2'
        })
      );
    });

    it('should handle missing configuration error', async () => {
      const handler = getHandler('initializeRepository');

      await handler(mockPanel, {
        type: 'initializeRepository',
        id: 'test-3',
        params: {}
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'initializationError',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });

    it('should initialize git repository if not already a git repo', async () => {
      const handler = getHandler('initializeRepository');

      mockGit.isGitRepository.mockResolvedValue(false);
      mockGit.initGitRepository.mockResolvedValue({ success: true });
      mockSocial.config.initialize.mockResolvedValue({ success: true });
      mockSocial.config.isInitialized.mockResolvedValue({ success: true, data: true });
      mockSocial.config.getConfig.mockResolvedValue({ success: true, data: {} });

      await handler(mockPanel, {
        type: 'initializeRepository',
        id: 'test-4',
        params: { config: { branch: 'main' } }
      });

      expect(mockGit.initGitRepository).toHaveBeenCalledWith(
        expect.any(String),
        'main'
      );
    });
  });

  describe('checkGitSocialInit handler', () => {
    it('should return initialization status when initialized', async () => {
      const handler = getHandler('checkGitSocialInit');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true, branch: 'gitsocial' }
      });
      mockGit.getCurrentBranch.mockResolvedValue({ success: true, data: 'gitsocial' });
      mockGit.execGit.mockResolvedValue({ success: true, data: { stdout: '', stderr: '' } });

      await handler(mockPanel, {
        type: 'checkGitSocialInit',
        id: 'test-5'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'gitSocialStatus',
          data: expect.objectContaining({
            isInitialized: true
          }),
          requestId: 'test-5'
        })
      );
    });

    it('should return not initialized status', async () => {
      const handler = getHandler('checkGitSocialInit');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: false }
      });
      mockGit.getCurrentBranch.mockResolvedValue({ success: true, data: 'main' });
      mockGit.execGit.mockResolvedValue({ success: false });

      await handler(mockPanel, {
        type: 'checkGitSocialInit',
        id: 'test-6'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'gitSocialStatus',
          data: expect.objectContaining({
            isInitialized: false
          }),
          requestId: 'test-6'
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('checkGitSocialInit');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'checkGitSocialInit',
        id: 'test-7'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('getRepositories handler', () => {
    it('should get repositories by scope successfully', async () => {
      const handler = getHandler('getRepositories');
      const mockRepos = [
        { id: 'repo1', name: 'Repo 1' },
        { id: 'repo2', name: 'Repo 2' }
      ];

      mockSocial.repository.getRepositories.mockResolvedValue({
        success: true,
        data: mockRepos
      });

      await handler(mockPanel, {
        type: 'getRepositories',
        id: 'test-8',
        scope: 'following'
      });

      expect(mockSocial.repository.getRepositories).toHaveBeenCalledWith(
        expect.any(String),
        'following'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'repositories',
        data: mockRepos,
        requestId: 'test-8'
      });
    });

    it('should handle repository fetch errors', async () => {
      const handler = getHandler('getRepositories');

      mockSocial.repository.getRepositories.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch repositories' }
      });

      await handler(mockPanel, {
        type: 'getRepositories',
        id: 'test-9',
        scope: 'all'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to fetch')
          })
        })
      );
    });
  });

  describe('fetchRepositories handler', () => {
    it('should fetch repositories with progress updates', async () => {
      const handler = getHandler('fetchRepositories');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 2, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchRepositories',
        id: 'test-10'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const calls = postMessage.mock.calls.map(call => call[0]);

      expect(calls.some(call => call.type === 'fetchCompleted')).toBe(true);
    });

    it('should broadcast refresh after successful fetch', async () => {
      const handler = getHandler('fetchRepositories');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchRepositories',
        id: 'test-11'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refreshAfterFetch' });
    });
  });

  describe('fetchUpdates handler', () => {
    it('should fetch updates for workspace repository', async () => {
      const handler = getHandler('fetchUpdates');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.execGit.mockResolvedValue({ success: true, data: { stdout: '' } });
      mockGit.fetchRemote = vi.fn().mockResolvedValue({ success: true });
      mockGit.getUpstreamBranch = vi.fn().mockResolvedValue({ success: true, data: 'origin/gitsocial' });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchUpdates',
        id: 'test-12',
        repository: 'workspace:my'
      });

      expect(mockGit.fetchRemote).toHaveBeenCalled();
    });

    it('should fetch updates for external repository', async () => {
      const handler = getHandler('fetchUpdates');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchUpdates',
        id: 'test-13',
        repository: 'https://github.com/user/repo'
      });

      expect(mockSocial.repository.fetchUpdates).toHaveBeenCalled();
      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refreshAfterFetch' });
    });
  });

  describe('addRepository handler', () => {
    it('should add repository to list successfully', async () => {
      const handler = getHandler('addRepository');

      mockSocial.list.addRepositoryToList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-14',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      expect(mockSocial.list.addRepositoryToList).toHaveBeenCalledWith(
        expect.any(String),
        'my-list',
        'https://github.com/user/repo'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'repositoryAdded',
          requestId: 'test-14'
        })
      );
    });

    it('should add repository with specific branch', async () => {
      const handler = getHandler('addRepository');

      mockSocial.list.addRepositoryToList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-15',
        listId: 'my-list',
        repository: 'https://github.com/user/repo',
        branch: 'main'
      });

      expect(mockSocial.list.addRepositoryToList).toHaveBeenCalledWith(
        expect.any(String),
        'my-list',
        'https://github.com/user/repo'
      );
    });

    it('should broadcast refresh after adding repository', async () => {
      const handler = getHandler('addRepository');

      mockSocial.list.addRepositoryToList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-16',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refreshAfterFetch' });
    });
  });

  describe('removeRepository handler', () => {
    it('should remove repository from list successfully', async () => {
      const handler = getHandler('removeRepository');

      mockSocial.list.removeRepositoryFromList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-17',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      expect(mockSocial.list.removeRepositoryFromList).toHaveBeenCalledWith(
        expect.any(String),
        'my-list',
        'https://github.com/user/repo'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'repositoryRemoved',
          requestId: 'test-17'
        })
      );
    });

    it('should handle removal errors', async () => {
      const handler = getHandler('removeRepository');

      mockSocial.list.removeRepositoryFromList.mockResolvedValue({
        success: false,
        error: { code: 'REMOVE_ERROR', message: 'Failed to remove' }
      });

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-18',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('getUnpushedCounts handler', () => {
    it('should get unpushed counts successfully', async () => {
      const handler = getHandler('getUnpushedCounts');

      mockGetUnpushedCounts.mockResolvedValue({
        posts: 3,
        comments: 2,
        total: 5
      });

      await handler(mockPanel, {
        type: 'getUnpushedCounts',
        id: 'test-19'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedCounts',
        data: { posts: 3, comments: 2, total: 5 },
        requestId: 'test-19'
      });
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getUnpushedCounts');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getUnpushedCounts',
        id: 'test-20'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedCounts',
        data: { posts: 0, comments: 0, total: 0 },
        requestId: 'test-20'
      });
    });
  });

  describe('pushToRemote handler', () => {
    it('should push to remote with progress updates', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false }) // rev-parse (no remote branch for divergence check)
        .mockResolvedValueOnce({ success: false }) // rev-parse (no remote branch for isFirstPush check)
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } }) // push posts
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } }); // push lists
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: [] });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-21'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const calls = postMessage.mock.calls.map(call => call[0]);

      expect(calls.some(call => call.type === 'pushProgress')).toBe(true);
      expect(calls.some(call => call.type === 'pushCompleted')).toBe(true);
    });

    it('should handle push with specific remote name', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false }) // rev-parse (no remote branch)
        .mockResolvedValueOnce({ success: true, data: { stdout: '0', stderr: '' } }) // hasUnpushedCommits
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } }); // push
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-22',
        remoteName: 'upstream'
      });

      expect(mockGit.execGit).toHaveBeenCalled();
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('pushToRemote');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-ws'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle validation failure', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.execGit = vi.fn().mockResolvedValue({ success: false });
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({
        success: false,
        error: { message: 'No commits to push' }
      });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-val'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('No commits to push')
          })
        })
      );
    });

    it('should auto-sync when diverged from remote', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.fetchRemote = vi.fn().mockResolvedValue({ success: true });
      mockGit.mergeBranch = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: true }) // rev-parse (remote branch exists)
        .mockResolvedValueOnce({ success: true, data: { stdout: '2\t3', stderr: '' } }) // divergence check (behind 2, ahead 3)
        .mockResolvedValueOnce({ success: false }) // isFirstPush check
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } }) // push posts
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } }); // push lists
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: [] });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-sync'
      });

      expect(mockGit.fetchRemote).toHaveBeenCalled();
      expect(mockGit.mergeBranch).toHaveBeenCalled();
    });
  });

  describe('checkRepositoryStatus handler', () => {
    it('should check workspace repository status', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: true,
        data: {
          type: 'workspace',
          url: '/workspace/path',
          isFollowing: false
        }
      });
      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('main');
      mockGit.listRemotes = vi.fn().mockResolvedValue({
        success: true,
        data: [{ name: 'origin', url: 'https://github.com/user/repo' }]
      });
      mockFetchTimeManager.get.mockResolvedValue(new Date('2025-01-01'));

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-1',
        repository: '.'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'repositoryStatus',
          requestId: 'test-status-1'
        })
      );
    });

    it('should check external repository status', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: true,
        data: {
          type: 'other',
          url: 'https://github.com/user/repo',
          isFollowing: true,
          remoteName: 'user-repo'
        }
      });
      mockFetchTimeManager.get.mockResolvedValue(new Date('2025-01-01'));

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-2',
        repository: 'https://github.com/user/repo'
      });

      expect(mockSocial.repository.getRepositoryRelationship).toHaveBeenCalledWith(
        expect.any(String),
        'https://github.com/user/repo'
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('checkRepositoryStatus');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-3',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle repository relationship errors', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: false,
        error: { message: 'Repository not found' }
      });

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-4',
        repository: 'https://github.com/nonexistent/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('not found')
          })
        })
      );
    });
  });

  describe('getUnpushedListsCount handler', () => {
    it('should get unpushed lists count successfully', async () => {
      const handler = getHandler('getUnpushedListsCount');

      mockSocial.list.getUnpushedListsCount.mockResolvedValue(3);

      await handler(mockPanel, {
        type: 'getUnpushedListsCount',
        id: 'test-lists-1'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedListsCount',
        data: 3,
        requestId: 'test-lists-1'
      });
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getUnpushedListsCount');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getUnpushedListsCount',
        id: 'test-lists-2'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedListsCount',
        data: 0,
        requestId: 'test-lists-2'
      });
    });

    it('should handle errors gracefully', async () => {
      const handler = getHandler('getUnpushedListsCount');

      mockSocial.list.getUnpushedListsCount.mockRejectedValue(new Error('Database error'));

      await handler(mockPanel, {
        type: 'getUnpushedListsCount',
        id: 'test-lists-3'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedListsCount',
        data: 0,
        requestId: 'test-lists-3'
      });
    });
  });

  describe('fetchSpecificRepositories handler', () => {
    it('should fetch specific repositories successfully', async () => {
      const handler = getHandler('fetchSpecificRepositories');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchSpecificRepositories',
        id: 'test-specific-1',
        repositoryIds: ['https://github.com/user/repo1#gitmsg/social']
      });

      const postMessage = getMockPostMessage(mockPanel);
      const calls = postMessage.mock.calls.map(call => call[0]);
      expect(calls.some(call => call.type === 'fetchCompleted')).toBe(true);
    });

    it('should handle empty repository list', async () => {
      const handler = getHandler('fetchSpecificRepositories');

      await handler(mockPanel, {
        type: 'fetchSpecificRepositories',
        id: 'test-specific-2',
        repositoryIds: []
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('No repositories specified')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('fetchSpecificRepositories');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'fetchSpecificRepositories',
        id: 'test-specific-3',
        repositoryIds: ['https://github.com/user/repo']
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle partial failures', async () => {
      const handler = getHandler('fetchSpecificRepositories');

      mockSocial.repository.fetchUpdates
        .mockResolvedValueOnce({ success: true, data: { fetched: 1, failed: 0 } })
        .mockResolvedValueOnce({ success: false, error: { message: 'Network error' } });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchSpecificRepositories',
        id: 'test-specific-4',
        repositoryIds: [
          'https://github.com/user/repo1#gitmsg/social',
          'https://github.com/user/repo2#gitmsg/social'
        ]
      });

      const postMessage = getMockPostMessage(mockPanel);
      const completedCall = postMessage.mock.calls.find(call => call[0].type === 'fetchCompleted');
      expect(completedCall).toBeDefined();
      expect(completedCall[0].data.failed.length).toBeGreaterThan(0);
    });
  });

  describe('fetchListRepositories handler', () => {
    it('should fetch repositories for a workspace list', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: {
          id: 'reading',
          name: 'Reading',
          repositories: ['https://github.com/user/repo#gitmsg/social']
        }
      });
      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-fetch-1',
        listId: 'reading'
      });

      expect(mockSocial.list.getList).toHaveBeenCalledWith(
        expect.any(String),
        'reading'
      );
    });

    it('should fetch repositories for a remote list', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: [{
          id: 'reading',
          name: 'Reading',
          repositories: ['https://github.com/author/blog#gitmsg/social']
        }]
      });
      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-fetch-2',
        listId: 'reading',
        repository: 'https://github.com/curator/lists'
      });

      expect(mockSocial.list.getLists).toHaveBeenCalledWith(
        'https://github.com/curator/lists',
        expect.any(String)
      );
    });

    it('should handle missing listId', async () => {
      const handler = getHandler('fetchListRepositories');

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-fetch-3',
        listId: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('List ID is required')
          })
        })
      );
    });

    it('should handle empty list', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: {
          id: 'empty-list',
          name: 'Empty',
          repositories: []
        }
      });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-fetch-4',
        listId: 'empty-list'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'fetchProgress',
          data: expect.objectContaining({
            status: 'completed',
            message: 'No repositories in list'
          })
        })
      );
    });

    it('should handle list not found', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getList.mockResolvedValue({
        success: false,
        error: { message: 'List not found' }
      });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-fetch-5',
        listId: 'nonexistent'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('not found')
          })
        })
      );
    });
  });

  describe('addRepository handler - validation', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('addRepository');

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-add-val-1',
        listId: '',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });

    it('should handle missing repository', async () => {
      const handler = getHandler('addRepository');

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-add-val-2',
        listId: 'my-list',
        repository: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('addRepository');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-add-val-3',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle add errors with specific error codes', async () => {
      const handler = getHandler('addRepository');

      mockSocial.list.addRepositoryToList.mockResolvedValue({
        success: false,
        error: { code: 'LIST_NOT_FOUND', message: 'List not found' }
      });

      await handler(mockPanel, {
        type: 'addRepository',
        id: 'test-add-val-4',
        listId: 'nonexistent',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('does not exist')
          })
        })
      );
    });
  });

  describe('removeRepository handler - validation', () => {
    it('should handle missing listId', async () => {
      const handler = getHandler('removeRepository');

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-remove-val-1',
        listId: '',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });

    it('should handle missing repository', async () => {
      const handler = getHandler('removeRepository');

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-remove-val-2',
        listId: 'my-list',
        repository: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('required')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('removeRepository');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-remove-val-3',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should broadcast refresh after successful removal', async () => {
      const handler = getHandler('removeRepository');

      mockSocial.list.removeRepositoryFromList.mockResolvedValue({
        success: true
      });

      await handler(mockPanel, {
        type: 'removeRepository',
        id: 'test-remove-val-4',
        listId: 'my-list',
        repository: 'https://github.com/user/repo'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });
  });

  describe('fetchRepositories handler - error handling', () => {
    it('should handle missing workspace folder', async () => {
      const handler = getHandler('fetchRepositories');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'fetchRepositories',
        id: 'test-fetch-err-1'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle fetch errors', async () => {
      const handler = getHandler('fetchRepositories');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: false,
        error: { message: 'Network error' }
      });

      await handler(mockPanel, {
        type: 'fetchRepositories',
        id: 'test-fetch-err-2'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Network error')
          })
        })
      );
    });

    it('should show warning for partial failures', async () => {
      const handler = getHandler('fetchRepositories');

      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 3, failed: 2 }
      });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchRepositories',
        id: 'test-fetch-err-3'
      });

      expect(vscode.window.showWarningMessage).toHaveBeenCalledWith(
        expect.stringContaining('3 repositories')
      );
    });
  });

  describe('getRepositories handler - error handling', () => {
    it('should handle missing workspace folder', async () => {
      const handler = getHandler('getRepositories');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'getRepositories',
        id: 'test-get-err-1',
        scope: 'all'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should add fetch times to repositories', async () => {
      const handler = getHandler('getRepositories');

      mockSocial.repository.getRepositories.mockResolvedValue({
        success: true,
        data: [
          { id: 'repo1', remoteName: 'origin' },
          { id: 'repo2', remoteName: 'upstream' }
        ]
      });
      mockFetchTimeManager.get
        .mockResolvedValueOnce(new Date('2025-01-01'))
        .mockResolvedValueOnce(new Date('2025-01-02'));

      await handler(mockPanel, {
        type: 'getRepositories',
        id: 'test-get-err-2',
        scope: 'all'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const call = postMessage.mock.calls.find(c => c[0].type === 'repositories');
      expect(call[0].data[0].lastFetchTime).toEqual(new Date('2025-01-01'));
      expect(call[0].data[1].lastFetchTime).toEqual(new Date('2025-01-02'));
    });
  });

  describe('fetchListRepositories handler - additional coverage', () => {
    it('should show warning when some repos fail to fetch', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: {
          id: 'reading',
          name: 'Reading',
          repositories: [
            'https://github.com/user/repo1#gitmsg/social',
            'https://github.com/user/repo2#gitmsg/social'
          ]
        }
      });
      mockSocial.repository.fetchUpdates
        .mockResolvedValueOnce({ success: true, data: { fetched: 1, failed: 0 } })
        .mockResolvedValueOnce({ success: true, data: { fetched: 0, failed: 1 } });
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-warn',
        listId: 'reading'
      });

      expect(vscode.window.showWarningMessage).toHaveBeenCalledWith(
        expect.stringContaining('failed')
      );
    });

    it('should handle cache refresh errors gracefully', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: {
          id: 'reading',
          name: 'Reading',
          repositories: ['https://github.com/user/repo#gitmsg/social']
        }
      });
      mockSocial.repository.fetchUpdates.mockResolvedValue({
        success: true,
        data: { fetched: 1, failed: 0 }
      });
      mockSocial.cache.refresh.mockRejectedValue(new Error('Cache error'));

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-cache-err',
        listId: 'reading'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const completedCall = postMessage.mock.calls.find(
        call => call[0].type === 'fetchProgress' && call[0].data.status === 'completed'
      );
      expect(completedCall).toBeDefined();
    });

    it('should handle remote list not found in repository', async () => {
      const handler = getHandler('fetchListRepositories');

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: [{ id: 'other-list', name: 'Other', repositories: [] }]
      });

      await handler(mockPanel, {
        type: 'fetchListRepositories',
        id: 'test-list-remote-not-found',
        listId: 'reading',
        repository: 'https://github.com/curator/lists'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('not found')
          })
        })
      );
    });
  });

  describe('checkRepositoryStatus handler - fetch time from git config', () => {
    it('should read fetch time from isolated repository git config', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: true,
        data: {
          type: 'other',
          url: 'https://github.com/user/repo',
          isFollowing: true
        }
      });
      mockFetchTimeManager.get.mockResolvedValue(null);
      mockStoragePath.getDirectory.mockReturnValue('/storage/user-repo');
      mockStorageRepository.readConfig.mockResolvedValue({
        lastFetch: '2025-01-15T10:00:00Z'
      });

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-config',
        repository: 'https://github.com/user/repo'
      });

      expect(mockStoragePath.getDirectory).toHaveBeenCalled();
      expect(mockStorageRepository.readConfig).toHaveBeenCalledWith('/storage/user-repo');
    });

    it('should handle git config read errors gracefully', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: true,
        data: {
          type: 'other',
          url: 'https://github.com/user/repo',
          isFollowing: true
        }
      });
      mockFetchTimeManager.get.mockResolvedValue(null);
      mockStoragePath.getDirectory.mockReturnValue('/storage/user-repo');
      mockStorageRepository.readConfig.mockRejectedValue(new Error('Config not found'));

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-config-err',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'repositoryStatus'
        })
      );
    });

    it('should handle missing lastFetch in git config', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: true,
        data: {
          type: 'other',
          url: 'https://github.com/user/repo',
          isFollowing: true
        }
      });
      mockFetchTimeManager.get.mockResolvedValue(null);
      mockStoragePath.getDirectory.mockReturnValue('/storage/user-repo');
      mockStorageRepository.readConfig.mockResolvedValue({});

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-no-lastfetch',
        repository: 'https://github.com/user/repo'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'repositoryStatus'
        })
      );
    });

    it('should handle workspace relationship error for empty repository', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: false,
        error: { message: 'GitSocial not initialized' }
      });

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-workspace-error',
        repository: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('not initialized')
          })
        })
      );
    });

    it('should handle missing workspace folder for empty repository', async () => {
      const handler = getHandler('checkRepositoryStatus');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-empty-no-workspace',
        repository: ''
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('No workspace folder')
          })
        })
      );
    });

    it('should handle workspace relationship error for dot repository', async () => {
      const handler = getHandler('checkRepositoryStatus');

      mockSocial.repository.getRepositoryRelationship.mockResolvedValue({
        success: false,
        error: { message: 'Repository check failed' }
      });

      await handler(mockPanel, {
        type: 'checkRepositoryStatus',
        id: 'test-status-dot-error',
        repository: '.'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('check failed')
          })
        })
      );
    });
  });

  describe('pushToRemote handler - additional coverage', () => {
    it('should handle fetch failure during auto-sync', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.fetchRemote = vi.fn().mockResolvedValue({
        success: false,
        error: { message: 'Network unreachable' }
      });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: true })
        .mockResolvedValueOnce({ success: true, data: { stdout: '2\t3', stderr: '' } });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-fetch-fail'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Network unreachable')
          })
        })
      );
    });

    it('should handle merge failure during auto-sync', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.fetchRemote = vi.fn().mockResolvedValue({ success: true });
      mockGit.mergeBranch = vi.fn().mockResolvedValue({
        success: false,
        error: { message: 'Merge conflict' }
      });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: true })
        .mockResolvedValueOnce({ success: true, data: { stdout: '2\t3', stderr: '' } });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-merge-fail'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Merge conflict')
          })
        })
      );
    });

    it('should handle push posts failure', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({
          success: false,
          error: { message: 'Permission denied' }
        });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-posts-fail'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Permission denied')
          })
        })
      );
    });

    it('should count and display unpushed items correctly', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: [
          { id: 'p1', type: 'post', display: { isUnpushed: true } },
          { id: 'p2', type: 'post', display: { isUnpushed: true } },
          { id: 'c1', type: 'comment', display: { isUnpushed: true } },
          { id: 'p3', type: 'post', display: { isUnpushed: false } }
        ]
      });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(1);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-count'
      });

      expect(vscode.window.showInformationMessage).toHaveBeenCalledWith(
        expect.stringContaining('2 posts')
      );
      expect(vscode.window.showInformationMessage).toHaveBeenCalledWith(
        expect.stringContaining('1 comment')
      );
      expect(vscode.window.showInformationMessage).toHaveBeenCalledWith(
        expect.stringContaining('1 list')
      );
    });

    it('should handle lists push failure gracefully', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: false, error: { message: 'No lists refs' } });
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: [] });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-lists-fail'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const completedCall = postMessage.mock.calls.find(call => call[0].type === 'pushCompleted');
      expect(completedCall).toBeDefined();
    });

    it('should handle cache refresh failure after push', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: [] });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockRejectedValue(new Error('Cache error'));

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-cache-fail'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const completedCall = postMessage.mock.calls.find(call => call[0].type === 'pushCompleted');
      expect(completedCall).toBeDefined();
    });

    it('should broadcast refresh after successful push', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: [] });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-broadcast'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({ type: 'refresh' });
    });

    it('should handle getPosts returning success with undefined data', async () => {
      const handler = getHandler('pushToRemote');

      mockGit.getConfiguredBranch = vi.fn().mockResolvedValue('gitsocial');
      mockGit.validatePushPreconditions = vi.fn().mockResolvedValue({ success: true });
      mockGit.execGit = vi.fn()
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: false })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } })
        .mockResolvedValueOnce({ success: true, data: { stdout: '', stderr: '' } });
      mockSocial.post.getPosts.mockResolvedValue({ success: true, data: undefined });
      mockSocial.list.getUnpushedListsCount.mockResolvedValue(0);
      mockSocial.cache.refresh.mockResolvedValue({ success: true });

      await handler(mockPanel, {
        type: 'pushToRemote',
        id: 'test-push-no-data'
      });

      const postMessage = getMockPostMessage(mockPanel);
      const completedCall = postMessage.mock.calls.find(call => call[0].type === 'pushCompleted');
      expect(completedCall).toBeDefined();
      expect(completedCall[0].data.pushed).toBe(0);
    });
  });

  describe('getUnpushedCounts handler - additional coverage', () => {
    it('should handle utility errors gracefully', async () => {
      const handler = getHandler('getUnpushedCounts');

      mockGetUnpushedCounts.mockRejectedValue(new Error('Utility error'));

      await handler(mockPanel, {
        type: 'getUnpushedCounts',
        id: 'test-unpushed-err'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'unpushedCounts',
        data: { posts: 0, comments: 0, total: 0 },
        requestId: 'test-unpushed-err'
      });
    });
  });
});
