import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockPanel, getMockPostMessage } from './helpers/mock-panel';
import { resetAllMocks } from './helpers/mock-social';
import { mockVscodeModule } from './helpers/mock-vscode';

const { mockSocial, mockGit, mockGitMsgRef } = vi.hoisted(() => ({
  mockSocial: {
    post: { getPosts: vi.fn(), createPost: vi.fn() },
    interaction: { createComment: vi.fn(), createInteraction: vi.fn() },
    timeline: { getTimeline: vi.fn(), getWeekPosts: vi.fn() },
    search: { searchPosts: vi.fn() },
    repository: { checkGitSocialInit: vi.fn(), initializeRepository: vi.fn(), ensureDataForDateRange: vi.fn() },
    cache: { isCacheRangeCovered: vi.fn(), loadAdditionalPosts: vi.fn() },
    list: { getList: vi.fn(), getLists: vi.fn() }
  },
  mockGit: {
    isGitRepository: vi.fn(),
    initGitRepository: vi.fn()
  },
  mockGitMsgRef: {
    parse: vi.fn(),
    create: vi.fn(),
    parseRepositoryId: vi.fn((id: string) => ({ repository: id.split('#')[0], branch: 'gitmsg/social' }))
  }
}));

vi.mock('vscode', () => mockVscodeModule());

vi.mock('@gitsocial/core', () => ({
  social: mockSocial,
  git: mockGit,
  gitMsgRef: mockGitMsgRef,
  log: vi.fn()
}));

vi.mock('../../../src/extension', () => ({
  getStorageUri: vi.fn(() => ({ fsPath: '/mock/storage' }))
}));

import * as vscode from 'vscode';
import { getHandler } from '../../../src/handlers/registry';
import { setBroadcast } from '../../../src/handlers/post';
import '../../../src/handlers/post';

describe('post.ts handlers', () => {
  const originalWorkspaceFolders = vscode.workspace.workspaceFolders;
  let mockPanel: ReturnType<typeof createMockPanel>;
  let broadcastSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    mockPanel = createMockPanel();
    broadcastSpy = vi.fn();
    setBroadcast(broadcastSpy);
    resetAllMocks({ social: mockSocial, git: mockGit });
  });

  afterEach(() => {
    vscode.workspace.workspaceFolders = originalWorkspaceFolders;
    vi.clearAllMocks();
  });

  describe('social.getPosts handler', () => {
    it('should get posts from timeline scope', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [
        { id: 'post1', content: 'Test post 1' },
        { id: 'post2', content: 'Test post 2' }
      ];

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-1',
        options: { scope: 'timeline' }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'timeline',
        expect.any(Object)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'posts',
        data: mockPosts,
        requestId: 'test-1'
      });
    });

    it('should get posts from repository scope', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'Repo post' }];

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-2',
        options: { scope: 'repository:my' }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          storageBase: '/mock/storage'
        })
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'posts',
        data: mockPosts,
        requestId: 'test-2'
      });
    });

    it('should get posts from list scope', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'List post' }];

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-3',
        options: { listId: 'my-list' }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'list:my-list',
        expect.any(Object)
      );
    });

    it('should handle date range options', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.cache.isCacheRangeCovered.mockReturnValue(true);
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      const since = '2025-01-01T00:00:00Z';
      const until = '2025-01-31T23:59:59Z';

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-4',
        options: { scope: 'repository:my', since, until }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          since: new Date(since),
          until: new Date(until)
        })
      );
    });

    it('should handle type filters', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-5',
        options: { scope: 'timeline', types: ['post', 'comment'] }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'timeline',
        expect.objectContaining({
          types: ['post', 'comment']
        })
      );
    });

    it('should handle limit option', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-6',
        options: { scope: 'repository:my', limit: 10 }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          limit: 10
        })
      );
    });

    it('should handle skipCache option', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-7',
        options: { scope: 'repository:my', skipCache: true }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.objectContaining({
          skipCache: true
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('social.getPosts');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-8',
        options: { scope: 'timeline' }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle post fetch errors', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: false,
        error: { code: 'FETCH_ERROR', message: 'Failed to fetch posts' }
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-9',
        options: { scope: 'repository:my' }
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

  describe('social.createPost handler', () => {
    it('should create post successfully', async () => {
      const handler = getHandler('social.createPost');
      const mockPost = {
        id: 'new-post',
        content: 'Hello world!',
        author: 'user'
      };

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.createPost.mockResolvedValue({
        success: true,
        data: mockPost
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-10',
        content: 'Hello world!'
      });

      expect(mockSocial.post.createPost).toHaveBeenCalledWith(
        expect.any(String),
        'Hello world!'
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'postCreated',
        data: { post: mockPost },
        requestId: 'test-10'
      });
    });

    it('should broadcast new post to all panels', async () => {
      const handler = getHandler('social.createPost');
      const mockPost = { id: 'new-post', content: 'Test' };

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.createPost.mockResolvedValue({
        success: true,
        data: mockPost
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-11',
        content: 'Test'
      });

      expect(broadcastSpy).toHaveBeenCalledWith({
        type: 'postCreated',
        data: { post: mockPost }
      });
    });

    it('should auto-initialize git repository if not a git repo', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(false);
      mockGit.initGitRepository.mockResolvedValue({ success: true });
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.createPost.mockResolvedValue({
        success: true,
        data: { id: 'post1', content: 'Test' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-12',
        content: 'Test'
      });

      expect(mockGit.initGitRepository).toHaveBeenCalled();
    });

    it('should auto-initialize GitSocial if not initialized', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: false }
      });
      mockSocial.repository.initializeRepository.mockResolvedValue({ success: true });
      mockSocial.post.createPost.mockResolvedValue({
        success: true,
        data: { id: 'post1', content: 'Test' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-13',
        content: 'Test'
      });

      expect(mockSocial.repository.initializeRepository).toHaveBeenCalled();
    });

    it('should create comment when replyTo is provided', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: [{ id: 'original-post-id', content: 'Original' }]
      });
      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: true,
        data: { id: 'comment1', content: 'Reply' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-14',
        content: 'Reply',
        parentId: 'original-post-id'
      });

      expect(mockSocial.interaction.createInteraction).toHaveBeenCalledWith(
        'comment',
        expect.any(String),
        expect.objectContaining({ id: 'original-post-id' }),
        'Reply'
      );
    });

    it('should handle create post errors', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.createPost.mockResolvedValue({
        success: false,
        error: { code: 'CREATE_ERROR', message: 'Failed to create post' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-15',
        content: 'Test'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to create')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('social.createPost');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-16',
        content: 'Test'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('social.searchPosts handler', () => {
    it('should search posts successfully', async () => {
      const handler = getHandler('social.searchPosts');
      const mockResults = [
        { id: 'post1', content: 'Match this query' },
        { id: 'post2', content: 'Another match for query' }
      ];

      mockSocial.search.searchPosts.mockResolvedValue({
        success: true,
        data: { results: mockResults }
      });

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-17',
        query: 'query'
      });

      expect(mockSocial.search.searchPosts).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          query: 'query'
        })
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'searchResults',
        data: { posts: mockResults },
        requestId: 'test-17'
      });
    });

    it('should search with filters', async () => {
      const handler = getHandler('social.searchPosts');

      mockSocial.search.searchPosts.mockResolvedValue({
        success: true,
        data: { results: [] }
      });

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-18',
        query: 'test',
        options: {
          types: ['post']
        }
      });

      expect(mockSocial.search.searchPosts).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          query: 'test',
          filters: expect.objectContaining({
            interactionType: ['post']
          })
        })
      );
    });

    it('should handle search errors', async () => {
      const handler = getHandler('social.searchPosts');

      mockSocial.search.searchPosts.mockResolvedValue({
        success: false,
        error: { code: 'SEARCH_ERROR', message: 'Search failed' }
      });

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-19',
        query: 'test'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Search failed')
          })
        })
      );
    });

    it('should handle missing workspace folder', async () => {
      const handler = getHandler('social.searchPosts');

      vi.mocked(vscode.workspace).workspaceFolders = undefined;

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-20',
        query: 'test'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });

    it('should handle missing query', async () => {
      const handler = getHandler('social.searchPosts');

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-21',
        query: ''
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

    it('should handle search with limit option', async () => {
      const handler = getHandler('social.searchPosts');

      mockSocial.search.searchPosts.mockResolvedValue({
        success: true,
        data: { results: [] }
      });

      await handler(mockPanel, {
        type: 'social.searchPosts',
        id: 'test-22',
        query: 'test',
        options: { limit: 50 }
      });

      expect(mockSocial.search.searchPosts).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          query: 'test',
          limit: 50
        })
      );
    });
  });

  describe('social.getPosts handler - timeline with date range', () => {
    it('should use getWeekPosts for timeline scope with date range', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'Timeline post' }];

      mockSocial.timeline.getWeekPosts.mockResolvedValue({
        success: true,
        data: { posts: mockPosts, repositories: [] }
      });

      const since = '2025-01-01T00:00:00Z';
      const until = '2025-01-07T23:59:59Z';

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-timeline-1',
        options: { scope: 'timeline', since, until }
      });

      expect(mockSocial.timeline.getWeekPosts).toHaveBeenCalledWith(
        expect.any(String),
        '/mock/storage',
        new Date(since),
        new Date(until),
        expect.any(Object)
      );

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'posts',
        data: mockPosts,
        requestId: 'test-timeline-1'
      });
    });

    it('should send repositories from getWeekPosts response', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'Post' }];
      const mockRepos = [{ id: 'repo1', name: 'Test Repo' }];

      mockSocial.timeline.getWeekPosts.mockResolvedValue({
        success: true,
        data: { posts: mockPosts, repositories: mockRepos }
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-timeline-2',
        options: { scope: 'timeline', since: '2025-01-01', until: '2025-01-07' }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'repositories',
        data: mockRepos,
        requestId: undefined
      });
    });

    it('should handle getWeekPosts errors', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.timeline.getWeekPosts.mockResolvedValue({
        success: false,
        error: { code: 'TIMELINE_ERROR', message: 'Failed to get timeline' }
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-timeline-3',
        options: { scope: 'timeline', since: '2025-01-01', until: '2025-01-07' }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to get timeline')
          })
        })
      );
    });
  });

  describe('social.getPosts handler - list with date range', () => {
    it('should prepare repository data for list posts with date range', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'List post' }];

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: { id: 'my-list', repositories: ['https://github.com/user/repo#gitmsg/social'] }
      });
      mockSocial.repository.ensureDataForDateRange.mockResolvedValue({ success: true });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-list-1',
        options: {
          listId: 'my-list',
          since: '2025-01-01T00:00:00Z',
          until: '2025-01-07T23:59:59Z'
        }
      });

      expect(mockSocial.list.getList).toHaveBeenCalledWith(
        expect.any(String),
        'my-list'
      );
      expect(mockSocial.repository.ensureDataForDateRange).toHaveBeenCalled();
      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'list:my-list',
        expect.any(Object)
      );
    });

    it('should handle list repository data preparation errors gracefully', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'List post' }];

      mockSocial.list.getList.mockResolvedValue({
        success: true,
        data: { id: 'my-list', repositories: ['https://github.com/user/repo#gitmsg/social'] }
      });
      mockSocial.repository.ensureDataForDateRange.mockResolvedValue({
        success: false,
        error: { message: 'Network error' }
      });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-list-2',
        options: {
          listId: 'my-list',
          since: '2025-01-01',
          until: '2025-01-07'
        }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith({
        type: 'posts',
        data: mockPosts,
        requestId: 'test-list-2'
      });
    });
  });

  describe('social.getPosts handler - remote repository list', () => {
    it('should handle remote repository list scope', async () => {
      const handler = getHandler('social.getPosts');
      const mockPosts = [{ id: 'post1', content: 'Remote list post' }];
      const mockListData = {
        id: 'reading',
        name: 'Reading List',
        repositories: ['https://github.com/author/blog#gitmsg/social']
      };

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: [mockListData]
      });
      mockSocial.repository.ensureDataForDateRange.mockResolvedValue({ success: true });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: mockPosts
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-remote-1',
        options: {
          scope: 'repository:https://github.com/user/lists/list:reading',
          since: '2025-01-01',
          until: '2025-01-07'
        }
      });

      expect(mockSocial.list.getLists).toHaveBeenCalledWith(
        'https://github.com/user/lists',
        expect.any(String)
      );
    });

    it('should handle remote list not found', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.list.getLists.mockResolvedValue({
        success: true,
        data: [{ id: 'other-list', name: 'Other' }]
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-remote-2',
        options: { scope: 'repository:https://github.com/user/lists/list:nonexistent' }
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

    it('should handle remote lists fetch failure', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.list.getLists.mockResolvedValue({
        success: false,
        error: { message: 'Failed to fetch remote lists' }
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-remote-3',
        options: { scope: 'repository:https://github.com/user/lists/list:reading' }
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error'
        })
      );
    });
  });

  describe('social.createPost handler - error cases', () => {
    it('should handle missing content', async () => {
      const handler = getHandler('social.createPost');

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-create-1',
        content: ''
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

    it('should handle git init failure', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(false);
      mockGit.initGitRepository.mockResolvedValue({
        success: false,
        error: { message: 'Permission denied' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-create-2',
        content: 'Test post'
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

    it('should handle GitSocial init failure', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: false }
      });
      mockSocial.repository.initializeRepository.mockResolvedValue({
        success: false,
        error: { message: 'Branch creation failed' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-create-3',
        content: 'Test post'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Branch creation failed')
          })
        })
      );
    });

    it('should handle parent post not found for comment', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-create-4',
        content: 'Reply',
        parentId: 'nonexistent-post'
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

    it('should handle interaction creation failure', async () => {
      const handler = getHandler('social.createPost');

      mockGit.isGitRepository.mockResolvedValue(true);
      mockSocial.repository.checkGitSocialInit.mockResolvedValue({
        success: true,
        data: { isInitialized: true }
      });
      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: [{ id: 'parent-post', content: 'Original' }]
      });
      mockSocial.interaction.createInteraction.mockResolvedValue({
        success: false,
        error: { message: 'Failed to create comment' }
      });

      await handler(mockPanel, {
        type: 'social.createPost',
        id: 'test-create-5',
        content: 'Reply',
        parentId: 'parent-post'
      });

      const postMessage = getMockPostMessage(mockPanel);
      expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          data: expect.objectContaining({
            message: expect.stringContaining('Failed to create')
          })
        })
      );
    });
  });

  describe('social.getPosts handler - default scope', () => {
    it('should use repository:my as default scope when no scope provided', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-default-1',
        options: {}
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:my',
        expect.any(Object)
      );
    });

    it('should construct scope from repository option', async () => {
      const handler = getHandler('social.getPosts');

      mockSocial.post.getPosts.mockResolvedValue({
        success: true,
        data: []
      });

      await handler(mockPanel, {
        type: 'social.getPosts',
        id: 'test-default-2',
        options: { repository: 'https://github.com/user/repo' }
      });

      expect(mockSocial.post.getPosts).toHaveBeenCalledWith(
        expect.any(String),
        'repository:https://github.com/user/repo',
        expect.any(Object)
      );
    });
  });
});
