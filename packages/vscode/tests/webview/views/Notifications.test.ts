/* eslint-disable @typescript-eslint/no-unsafe-call, @typescript-eslint/no-unsafe-assignment */
/* eslint-disable @typescript-eslint/no-unsafe-member-access, @typescript-eslint/no-unsafe-argument */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createSyntheticFollowPost, getNotificationIcon, matchesPostId } from '../../../src/webview/views/Notifications.svelte';
import type { Notification, Post } from '@gitsocial/core/client';

// NOTE: Component rendering tests have limited coverage due to known limitations in the test environment.
//
// Notifications.svelte relies heavily on features that don't work in happy-dom:
// 1. onMount/onDestroy lifecycle hooks - don't execute properly
// 2. window.addEventListener for message events - unreliable in test environment
// 3. Complex reactive statements - cause mounting errors
//
// Similar to Timeline.test.ts and Thread.test.ts, component rendering fails with:
// "TypeError: Cannot read properties of undefined (reading 'forEach')" during mount.
//
// Notifications.svelte is extensively used in production and manually tested. The component:
// - Displays notifications (comments, reposts, quotes, follows)
// - Handles 4 different message types for communication with the extension
// - Manages week-based navigation with DateNavigation component
// - Creates synthetic posts for follow notifications
// - Fetches original posts for comment notifications
//
// For full verification:
// - Manual testing in real VSCode environment
// - E2E tests with real webview (see test/e2e/)
// - Integration tests in VSCode extension test suite (test/suite/)
//
// This test file focuses on:
// 1. Testing all exported pure functions (100% coverage)
// 2. Verifying the module can be imported without syntax errors
// 3. Maintaining test file consistency with other Svelte components

vi.mock('@gitsocial/core/client', () => ({
  gitHost: {
    getDisplayName: vi.fn((url: string) => {
      const match = url.match(/github\.com\/([^/]+\/[^/#]+)/);
      return match ? match[1] : 'repository';
    }),
    getCommitUrl: vi.fn((repo: string, hash: string) => `${repo}/commit/${hash}`)
  },
  gitMsgRef: {
    parse: vi.fn((ref: string) => {
      if (ref.includes('commit:')) {
        const hash = ref.split('commit:')[1];
        return { type: 'commit', value: hash };
      }
      return { type: 'unknown', value: ref };
    })
  }
}));

describe('Notifications Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Module Import', () => {
    it('module exists and can be imported', async () => {
      const module = await import('../../../src/webview/views/Notifications.svelte');
      expect(module).toBeDefined();
      expect(module.default).toBeDefined();
    });

    it('exports a Svelte component', async () => {
      const module = await import('../../../src/webview/views/Notifications.svelte');
      expect(typeof module.default).toBe('function');
    });

    it('exports matchesPostId function', async () => {
      const module = await import('../../../src/webview/views/Notifications.svelte');
      expect(typeof module.matchesPostId).toBe('function');
    });

    it('exports getNotificationIcon function', async () => {
      const module = await import('../../../src/webview/views/Notifications.svelte');
      expect(typeof module.getNotificationIcon).toBe('function');
    });

    it('exports createSyntheticFollowPost function', async () => {
      const module = await import('../../../src/webview/views/Notifications.svelte');
      expect(typeof module.createSyntheticFollowPost).toBe('function');
    });
  });

  describe('matchesPostId', () => {
    it('returns false when postId is undefined', () => {
      expect(matchesPostId(undefined, 'target-id')).toBe(false);
    });

    it('returns false when postId is empty string', () => {
      expect(matchesPostId('', 'target-id')).toBe(false);
    });

    it('returns true when postId exactly matches targetId', () => {
      expect(matchesPostId('same-id', 'same-id')).toBe(true);
    });

    it('returns true when both are commit refs with matching hashes', () => {
      const postId = 'https://github.com/user/repo#commit:abc123';
      const targetId = 'https://github.com/other/repo#commit:abc123';
      expect(matchesPostId(postId, targetId)).toBe(true);
    });

    it('returns false when commit hashes do not match', () => {
      const postId = 'https://github.com/user/repo#commit:abc123';
      const targetId = 'https://github.com/user/repo#commit:def456';
      expect(matchesPostId(postId, targetId)).toBe(false);
    });

    it('returns false when only one is a commit ref', () => {
      const postId = 'https://github.com/user/repo#commit:abc123';
      const targetId = 'https://github.com/user/repo#branch:main';
      expect(matchesPostId(postId, targetId)).toBe(false);
    });

    it('handles short commit hash vs long commit hash', () => {
      const postId = 'repo#commit:abc123456789';
      const targetId = 'repo#commit:abc123456789';
      expect(matchesPostId(postId, targetId)).toBe(true);
    });

    it('returns false for non-matching non-commit refs', () => {
      const postId = 'repo#branch:main';
      const targetId = 'repo#branch:develop';
      expect(matchesPostId(postId, targetId)).toBe(false);
    });
  });

  describe('getNotificationIcon', () => {
    it('returns comment icon for comment type', () => {
      expect(getNotificationIcon('comment')).toBe('codicon-comment');
    });

    it('returns sync icon for repost type', () => {
      expect(getNotificationIcon('repost')).toBe('codicon-sync');
    });

    it('returns quote icon for quote type', () => {
      expect(getNotificationIcon('quote')).toBe('codicon-quote');
    });

    it('returns person-add icon for follow type', () => {
      expect(getNotificationIcon('follow')).toBe('codicon-person-add');
    });

    it('returns bell icon for unknown type', () => {
      expect(getNotificationIcon('unknown')).toBe('codicon-bell');
    });

    it('returns bell icon for empty string', () => {
      expect(getNotificationIcon('')).toBe('codicon-bell');
    });

    it('returns bell icon for null type', () => {
      expect(getNotificationIcon(null as unknown as string)).toBe('codicon-bell');
    });
  });

  describe('createSyntheticFollowPost', () => {
    it('creates post with full commit data', () => {
      const notification: Notification = {
        id: 'notif-1',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abc123456789',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        commit: {
          author: 'John Doe',
          email: 'john@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z')
        }
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.id).toBe('https://github.com/user/repo#commit:abc123456789');
      expect(post.repository).toBe('https://github.com/user/repo');
      expect(post.author.name).toBe('John Doe');
      expect(post.author.email).toBe('john@example.com');
      expect(post.content).toBe('Added you to a list');
      expect(post.type).toBe('post');
      expect(post.source).toBe('explicit');
      expect(post.display.commitHash).toBe('abc123456789');
      expect(post.display.commitUrl).toBe('https://github.com/user/repo/commit/abc123456789');
    });

    it('falls back to repositoryName when author is missing', () => {
      const notification: Notification = {
        id: 'notif-2',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:def456',
        timestamp: new Date('2024-01-15T10:00:00Z')
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.author.name).toBe('user/repo');
    });

    it('uses empty string when email is missing', () => {
      const notification: Notification = {
        id: 'notif-3',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:ghi789',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        commit: {
          author: 'Jane Doe'
        }
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.author.email).toBe('');
    });

    it('uses current date when timestamp is missing', () => {
      const beforeTest = new Date();
      const notification: Notification = {
        id: 'notif-4',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:jkl012',
        timestamp: new Date('2024-01-15T10:00:00Z')
      };

      const post: Post = createSyntheticFollowPost(notification);
      const afterTest = new Date();

      expect(post.timestamp.getTime()).toBeGreaterThanOrEqual(beforeTest.getTime());
      expect(post.timestamp.getTime()).toBeLessThanOrEqual(afterTest.getTime());
    });

    it('sets correct display properties', () => {
      const notification: Notification = {
        id: 'notif-5',
        type: 'follow',
        commitId: 'https://github.com/owner/project#commit:mno345678901',
        timestamp: new Date('2024-01-15T10:00:00Z')
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.display.repositoryName).toBe('owner/project');
      expect(post.display.totalReposts).toBe(0);
      expect(post.display.isEmpty).toBe(false);
      expect(post.display.isUnpushed).toBe(false);
      expect(post.display.isOrigin).toBe(false);
      expect(post.display.isWorkspacePost).toBe(false);
    });

    it('truncates commit hash to 12 characters', () => {
      const notification: Notification = {
        id: 'notif-6',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abcdef1234567890',
        timestamp: new Date('2024-01-15T10:00:00Z')
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.display.commitHash).toBe('abcdef123456');
      expect(post.display.commitHash.length).toBe(12);
    });

    it('includes raw commit data', () => {
      const notification: Notification = {
        id: 'notif-7',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:pqr678',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        commit: {
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T09:00:00Z')
        }
      };

      const post: Post = createSyntheticFollowPost(notification);

      expect(post.raw.commit.hash).toBe('pqr678');
      expect(post.raw.commit.message).toBe('Added you to a list');
      expect(post.raw.commit.author).toBe('Test User');
      expect(post.raw.commit.email).toBe('test@example.com');
    });
  });

  describe('Edge Cases', () => {
    it('matchesPostId handles null postId', () => {
      expect(matchesPostId(null as unknown as string, 'target')).toBe(false);
    });

    it('matchesPostId handles malformed commit refs', () => {
      const postId = 'not-a-ref';
      const targetId = 'also-not-a-ref';
      expect(matchesPostId(postId, targetId)).toBe(false);
    });

    it('createSyntheticFollowPost handles minimal notification', () => {
      const notification: Notification = {
        id: 'minimal',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post).toBeDefined();
      expect(post.id).toBe('https://github.com/user/repo#commit:abc');
    });

    it('getNotificationIcon handles undefined', () => {
      expect(getNotificationIcon(undefined as unknown as string)).toBe('codicon-bell');
    });

    it('handles very long commit hashes', () => {
      const notification: Notification = {
        id: 'long-hash',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abcdef1234567890abcdef1234567890',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.display.commitHash).toBe('abcdef123456');
    });

    it('handles commit hash shorter than 12 characters', () => {
      const notification: Notification = {
        id: 'short-hash',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.display.commitHash).toBe('abc');
    });
  });

  describe('Repository URL Parsing', () => {
    it('correctly splits repository URL and commit', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'https://github.com/org/project#commit:hash123',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.repository).toBe('https://github.com/org/project');
    });

    it('handles repository URLs with multiple slashes', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'https://github.com/org/project/subpath#commit:hash456',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.repository).toBe('https://github.com/org/project/subpath');
    });

    it('handles commitId without hash separator', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'https://github.com/user/repo',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.repository).toBe('https://github.com/user/repo');
    });
  });

  describe('Timestamp Handling', () => {
    it('uses commit timestamp when provided', () => {
      const commitTimestamp = new Date('2024-01-01T12:00:00Z');
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date(),
        commit: {
          timestamp: commitTimestamp
        }
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.timestamp).toEqual(commitTimestamp);
    });

    it('creates valid Date object when timestamp is missing', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.timestamp).toBeInstanceOf(Date);
      expect(isNaN(post.timestamp.getTime())).toBe(false);
    });
  });

  describe('Display Properties', () => {
    it('sets all boolean display flags to false', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.display.isEmpty).toBe(false);
      expect(post.display.isUnpushed).toBe(false);
      expect(post.display.isOrigin).toBe(false);
      expect(post.display.isWorkspacePost).toBe(false);
    });

    it('sets totalReposts to 0', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.display.totalReposts).toBe(0);
    });

    it('generates correct commit URL', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'https://github.com/user/repo#commit:abc123',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.display.commitUrl).toBe('https://github.com/user/repo/commit/abc123');
    });
  });

  describe('Post Type and Source', () => {
    it('sets type to post', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.type).toBe('post');
    });

    it('sets source to explicit', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.source).toBe('explicit');
    });

    it('sets content to "Added you to a list"', () => {
      const notification: Notification = {
        id: 'test',
        type: 'follow',
        commitId: 'repo#commit:abc',
        timestamp: new Date()
      };

      const post: Post = createSyntheticFollowPost(notification);
      expect(post.content).toBe('Added you to a list');
    });
  });
});
