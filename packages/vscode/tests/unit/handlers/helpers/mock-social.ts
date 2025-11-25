import { vi } from 'vitest';

export function createMockSocial() {
  return {
    cache: {
      getCacheStats: vi.fn(),
      refresh: vi.fn(),
      setMaxSize: vi.fn()
    },
    post: {
      getPosts: vi.fn(),
      createPost: vi.fn()
    },
    interaction: {
      createComment: vi.fn(),
      createRepost: vi.fn(),
      createQuote: vi.fn(),
      createInteraction: vi.fn()
    },
    list: {
      getLists: vi.fn(),
      createList: vi.fn(),
      updateList: vi.fn(),
      deleteList: vi.fn(),
      followList: vi.fn(),
      syncFollowedList: vi.fn(),
      unfollowList: vi.fn(),
      addRepositoryToList: vi.fn(),
      removeRepositoryFromList: vi.fn()
    },
    repository: {
      getRepositories: vi.fn(),
      fetchRepositories: vi.fn(),
      fetchUpdates: vi.fn(),
      addRepository: vi.fn(),
      removeRepository: vi.fn(),
      getStorageStats: vi.fn(),
      cleanupStorage: vi.fn()
    },
    follower: {
      get: vi.fn()
    },
    notification: {
      getNotifications: vi.fn()
    },
    log: {
      get: vi.fn()
    },
    search: {
      search: vi.fn()
    },
    timeline: {
      getTimeline: vi.fn()
    },
    avatar: {
      getAvatar: vi.fn(),
      getCacheStats: vi.fn(),
      clearCache: vi.fn()
    },
    config: {
      initialize: vi.fn(),
      isInitialized: vi.fn(),
      getConfig: vi.fn()
    }
  };
}

export function createMockGit() {
  return {
    execGit: vi.fn(),
    getCurrentBranch: vi.fn(),
    getBranches: vi.fn(),
    createBranch: vi.fn(),
    init: vi.fn(),
    getConfig: vi.fn(),
    setConfig: vi.fn(),
    getRemoteUrl: vi.fn(),
    hasUnpushedCommits: vi.fn()
  };
}

export function createMockGitMsgRef() {
  return {
    parse: vi.fn(),
    create: vi.fn(),
    validate: vi.fn(),
    normalize: vi.fn()
  };
}

export function createMockStorage() {
  return {
    getStoragePath: vi.fn(),
    ensureStorageDir: vi.fn(),
    readStorage: vi.fn(),
    writeStorage: vi.fn(),
    deleteStorage: vi.fn()
  };
}

export function resetAllMocks(mocks: Record<string, any>) {
  Object.values(mocks).forEach(namespace => {
    if (typeof namespace === 'object') {
      Object.values(namespace).forEach(fn => {
        if (typeof fn === 'function' && 'mockReset' in fn) {
          fn.mockReset();
        }
      });
    }
  });
}
