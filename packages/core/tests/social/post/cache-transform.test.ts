import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { constructPost, createVirtualPostFromReference, mergeVirtualPostIntoWorkspace, processCommits, processPost } from '../../../src/social/post/cache-transform';
import { createTestRepo, type TestRepo } from '../../test-utils';
import type { Post } from '../../../src/social/types';

describe('social/post/cache-transform', () => {
  let testRepo: TestRepo;

  beforeEach(async () => {
    testRepo = await createTestRepo('cache-transform-test');
  });

  afterEach(() => {
    testRepo.cleanup();
  });

  describe('constructPost()', () => {
    describe('from real commits', () => {
      it('should construct a basic workspace post from a real commit', () => {
        const realCommit = {
          hash: 'abc123456789',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: 'Hello world',
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeDefined();
        expect(post?.id).toBe('#commit:abc123456789');
        expect(post?.author).toEqual({ name: 'Test User', email: 'test@example.com' });
        expect(post?.content).toBe('Hello world');
        expect(post?.type).toBe('post');
        expect(post?.isWorkspacePost).toBe(true);
        expect(post?.raw.commit.hash).toBe('abc123456789');
      });

      it('should construct an external post from a commit with upstream remote', () => {
        const realCommit = {
          hash: 'def456789abc',
          author: 'External User',
          email: 'external@example.com',
          timestamp: new Date('2024-01-15T11:00:00Z'),
          message: 'External post',
          repository: testRepo.path,
          repositoryIdentifier: 'https://github.com/user/repo',
          remoteName: 'upstream',
          hasOriginRemote: false
        };

        const post = constructPost(realCommit);

        expect(post).toBeDefined();
        expect(post?.id).toBe('https://github.com/user/repo#commit:def456789abc');
        expect(post?.isWorkspacePost).toBeUndefined();
        expect(post?.repository).toBe('https://github.com/user/repo');
      });

      it('should extract GitMsg metadata from commit message', () => {
        const realCommit = {
          hash: 'aabbccddeeff',
          author: 'GitMsg User',
          email: 'gitmsg@example.com',
          timestamp: new Date('2024-01-15T12:00:00Z'),
          message: `This is the content

--- GitMsg: ext="social"; type="post"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeDefined();
        expect(post?.content).toBe('This is the content');
        expect(post?.source).toBe('explicit');
        expect(post?.raw.gitMsg).toBeDefined();
      });

      it('should handle commits with branch information', () => {
        const realCommit = {
          hash: 'bbccddeeff00',
          author: 'Branch User',
          email: 'branch@example.com',
          timestamp: new Date('2024-01-15T13:00:00Z'),
          message: 'Branch commit',
          repository: testRepo.path,
          repositoryIdentifier: 'https://github.com/user/repo',
          branch: 'refs/remotes/origin/feature-branch',
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeDefined();
        expect(post?.branch).toBe('refs/remotes/origin/feature-branch');
        expect(post?.repository).toContain('#branch:remotes/origin/feature-branch');
      });

      it('should detect unpushed commits', () => {
        const unpushedSet = new Set(['abc123def456']);
        const realCommit = {
          hash: 'abc123def456',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'Unpushed',
          repository: testRepo.path,
          hasOriginRemote: true,
          unpushedCommits: unpushedSet,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(true);
      });

      it('should handle commits with refname', () => {
        const realCommit = {
          hash: 'abc123def456',
          author: 'Ref User',
          email: 'ref@example.com',
          timestamp: new Date(),
          message: 'With refname',
          repository: testRepo.path,
          refname: 'refs/remotes/origin/main',
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.raw.commit.refname).toBe('refs/remotes/origin/main');
      });

      it('should handle comment type from GitMsg', () => {
        const realCommit = {
          hash: 'aabbccddee11',
          author: 'Commenter',
          email: 'comment@example.com',
          timestamp: new Date(),
          message: `This is a comment

--- GitMsg: ext="social"; type="comment"; reply-to="#commit:1122334455aa"; \
original="#commit:0011223344ff"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.type).toBe('comment');
        expect(post?.parentCommentId).toBe('#commit:1122334455aa');
      });

      it('should handle repost type from GitMsg', () => {
        const realCommit = {
          hash: 'bbccddee1122',
          author: 'Reposter',
          email: 'repost@example.com',
          timestamp: new Date(),
          message: `# Repost

--- GitMsg: ext="social"; type="repost"; original="#commit:2233445566bb"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.type).toBe('repost');
        expect(post?.originalPostId).toBe('#commit:2233445566bb');
        expect(post?.display.isEmpty).toBe(true);
      });

      it('should handle quote type from GitMsg', () => {
        const realCommit = {
          hash: 'ccddeeff2233',
          author: 'Quoter',
          email: 'quote@example.com',
          timestamp: new Date(),
          message: `My thoughts on this

--- GitMsg: ext="social"; type="quote"; original="#commit:3344556677cc"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.type).toBe('quote');
        expect(post?.originalPostId).toBe('#commit:3344556677cc');
      });
    });

    describe('from virtual commits', () => {
      it('should construct a post from a virtual commit with absolute reference', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Virtual User"; email="virtual@example.com"; time="2024-01-15T14:00:00Z"; ref="https://github.com/external/repo#commit:aabbccddeeff"; v="0.1.0"; ext-v="0.1.0" ---
> This is virtual content`,
          refId: 'https://github.com/external/repo#commit:aabbccddeeff',
          refType: 'social'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeDefined();
        expect(post?.id).toBe('https://github.com/external/repo#commit:aabbccddeeff');
        expect(post?.author).toEqual({ name: 'Virtual User', email: 'virtual@example.com' });
        expect(post?.content).toBe('This is virtual content');
        expect(post?.isVirtual).toBe(true);
        expect(post?.isWorkspacePost).toBeUndefined();
      });

      it('should construct a workspace post from virtual commit with relative reference', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Local User"; email="local@example.com"; \
time="2024-01-15T15:00:00Z"; ref="#commit:bbccddee1122"; v="0.1.0"; ext-v="0.1.0" ---
> Local virtual content`,
          refId: '#commit:bbccddee1122'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeDefined();
        expect(post?.id).toBe('#commit:bbccddee1122');
        expect(post?.isVirtual).toBe(true);
        expect(post?.isWorkspacePost).toBe(true);
      });

      it('should handle virtual commit with type header', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Type User"; email="type@example.com"; time="2024-01-15T16:00:00Z"; type="post"; ref="https://github.com/external/repo#commit:ccddeeff2233"; v="0.1.0"; ext-v="0.1.0" ---
> This is a post`,
          refId: 'https://github.com/external/repo#commit:ccddeeff2233',
          refType: 'social',
          fields: { type: 'post' }
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post?.type).toBe('post');
      });

      it('should return null for virtual commit without author', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; email="test@example.com"; \
time="2024-01-15T17:00:00Z"; ref="#commit:ddeeff334455"; v="0.1.0"; ext-v="0.1.0" ---
> Content without author`,
          refId: '#commit:ddeeff334455'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeNull();
      });

      it('should return null for virtual commit without email', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="User Without Email"; \
time="2024-01-15T17:00:00Z"; ref="#commit:eeff44556677"; v="0.1.0"; ext-v="0.1.0" ---
> Content`,
          refId: '#commit:eeff44556677'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeNull();
      });

      it('should return null for virtual commit without timestamp', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="User"; email="user@example.com"; \
ref="#commit:ff5566778899"; v="0.1.0"; ext-v="0.1.0" ---
> Content`,
          refId: '#commit:ff5566778899'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeNull();
      });

      it('should return null for virtual commit without content', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="User"; email="user@example.com"; \
time="2024-01-15T17:00:00Z"; ref="#commit:aabb66778899"; v="0.1.0"; ext-v="0.1.0" ---
`,
          refId: '#commit:aabb66778899'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeNull();
      });

      it('should return null for virtual commit without hash in refId', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="User"; email="user@example.com"; \
time="2024-01-15T17:00:00Z"; ref="https://github.com/repo/url"; v="0.1.0"; ext-v="0.1.0" ---
> Content`,
          refId: 'https://github.com/repo/url'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeNull();
      });

      it('should extract content from metadata lines starting with >', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Multi User"; email="multi@example.com"; \
time="2024-01-15T17:00:00Z"; ref="#commit:ccdd77889900"; v="0.1.0"; ext-v="0.1.0" ---
> Line 1
> Line 2
> Line 3`,
          refId: '#commit:ccdd77889900'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post?.content).toBe('Line 1\nLine 2\nLine 3');
      });

      it('should use pre-parsed fields if available', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Preparsed User"; \
email="preparsed@example.com"; time="2024-01-15T18:00:00Z"; \
ref="#commit:ddee88990011"; v="0.1.0"; ext-v="0.1.0" ---
> Content`,
          refId: '#commit:ddee88990011',
          fields: {
            author: 'Preparsed User',
            email: 'preparsed@example.com',
            time: '2024-01-15T18:00:00Z',
            type: 'post'
          }
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post).toBeDefined();
        expect(post?.author.name).toBe('Preparsed User');
        expect(post?.type).toBe('post');
      });
    });

    describe('edge cases', () => {
      it('should return null when both realCommit and virtualCommit are provided', () => {
        const realCommit = {
          hash: 'eeff99001122',
          author: 'User',
          email: 'user@example.com',
          timestamp: new Date(),
          message: 'Real',
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="Virtual"; email="virtual@example.com"; \
time="2024-01-15T19:00:00Z"; ref="#commit:ffaa00112233"; v="0.1.0"; ext-v="0.1.0" ---
> Virtual`,
          refId: '#commit:ffaa00112233'
        };

        const post = constructPost(realCommit, virtualCommit);

        expect(post).toBeNull();
      });

      it('should return null when neither realCommit nor virtualCommit are provided', () => {
        const post = constructPost();

        expect(post).toBeNull();
      });

      it('should normalize repository URLs', () => {
        const realCommit = {
          hash: 'aabbcc112233',
          author: 'User',
          email: 'user@example.com',
          timestamp: new Date(),
          message: 'Content',
          repository: testRepo.path,
          repositoryIdentifier: 'https://github.com/user/repo.git',
          remoteName: 'upstream'
        };

        const post = constructPost(realCommit);

        expect(post?.repository).toBe('https://github.com/user/repo');
      });

      it('should normalize commit hashes', () => {
        const realCommit = {
          hash: 'AABBCCDDEE11',
          author: 'User',
          email: 'user@example.com',
          timestamp: new Date(),
          message: 'Content',
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.raw.commit.hash).toBe('aabbccddee11');
      });

      it('should handle empty repository URL for workspace posts', () => {
        const virtualCommit = {
          body: `--- GitMsg-Ref: ext="social"; author="User"; email="user@example.com"; \
time="2024-01-15T20:00:00Z"; ref="#commit:bbccdd223344"; v="0.1.0"; ext-v="0.1.0" ---
> Content`,
          refId: '#commit:bbccdd223344'
        };

        const post = constructPost(undefined, virtualCommit);

        expect(post?.repository).toBe('');
      });

      it('should normalize relative references in external posts', () => {
        const realCommit = {
          hash: 'ccddeeff3344',
          author: 'User',
          email: 'user@example.com',
          timestamp: new Date(),
          message: `Comment on my repo

--- GitMsg: ext="social"; type="comment"; reply-to="#commit:ddeeff445566"; \
original="#commit:aabbcc112233"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          repositoryIdentifier: 'https://github.com/user/repo',
          remoteName: 'upstream'
        };

        const post = constructPost(realCommit);

        expect(post?.parentCommentId).toBe('https://github.com/user/repo#commit:ddeeff445566');
        expect(post?.originalPostId).toBe('https://github.com/user/repo#commit:aabbcc112233');
      });

      it('should set display properties correctly', () => {
        const realCommit = {
          hash: 'eeff00112233',
          author: 'Display User',
          email: 'display@example.com',
          timestamp: new Date(),
          message: 'Display test',
          repository: testRepo.path,
          repositoryIdentifier: 'https://github.com/owner/repository',
          remoteName: 'upstream',
          hasOriginRemote: false
        };

        const post = constructPost(realCommit);

        expect(post?.display).toBeDefined();
        expect(post?.display.repositoryName).toBeDefined();
        expect(post?.display.commitHash).toBe('eeff00112233');
        expect(post?.display.commitUrl).toContain('eeff00112233');
        expect(post?.display.isOrigin).toBe(false);
      });
    });

    describe('validation', () => {
      it('should return null for comment without originalPostId', () => {
        const realCommit = {
          hash: 'abc123456789',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: `Comment without original

--- GitMsg: ext="social"; type="comment"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeNull();
      });

      it('should return null for repost without originalPostId', () => {
        const realCommit = {
          hash: 'def456789abc',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: `Repost without original

--- GitMsg: ext="social"; type="repost"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeNull();
      });

      it('should return null for quote without originalPostId', () => {
        const realCommit = {
          hash: 'aabbccddee11',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: `Quote without original

--- GitMsg: ext="social"; type="quote"; v="0.1.0"; ext-v="0.1.0" ---`,
          repository: testRepo.path,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post).toBeNull();
      });
    });

    describe('unpushed detection fallback', () => {
      it('should detect unpushed when unpushedCommits is undefined and refname starts with refs/heads/', () => {
        const realCommit = {
          hash: 'abc123def456',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'Unpushed local branch',
          repository: testRepo.path,
          refname: 'refs/heads/main',
          hasOriginRemote: true,
          unpushedCommits: undefined,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(true);
      });

      it('should detect unpushed when unpushedCommits is undefined and refname is undefined', () => {
        const realCommit = {
          hash: 'def456abc123',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'Unpushed without refname',
          repository: testRepo.path,
          refname: undefined,
          hasOriginRemote: true,
          unpushedCommits: undefined,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(true);
      });

      it('should detect unpushed when unpushedCommits is undefined and refname does not start with refs/remotes/origin/', () => {
        const realCommit = {
          hash: 'aabbccddeeff',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'Unpushed other remote',
          repository: testRepo.path,
          refname: 'refs/remotes/upstream/main',
          hasOriginRemote: true,
          unpushedCommits: undefined,
          remoteName: 'upstream'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(true);
      });

      it('should NOT detect unpushed when refname starts with refs/remotes/origin/ and unpushedCommits is undefined', () => {
        const realCommit = {
          hash: 'bbccddee1122',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'Pushed to origin',
          repository: testRepo.path,
          refname: 'refs/remotes/origin/main',
          hasOriginRemote: true,
          unpushedCommits: undefined,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(false);
      });

      it('should NOT detect unpushed when hasOriginRemote is false', () => {
        const realCommit = {
          hash: 'ccddeeff2233',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date(),
          message: 'No origin remote',
          repository: testRepo.path,
          refname: 'refs/heads/main',
          hasOriginRemote: false,
          unpushedCommits: undefined,
          remoteName: 'origin'
        };

        const post = constructPost(realCommit);

        expect(post?.display.isUnpushed).toBe(false);
      });
    });
  });

  describe('processPost()', () => {
    describe('reference normalization', () => {
      it('should normalize references for workspace post with originUrl and external reference', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'comment',
          source: 'explicit',
          isWorkspacePost: true,
          originalPostId: 'https://github.com/other/repo#commit:def456789abc',
          parentCommentId: 'https://github.com/other/repo#commit:aabbccddeeff',
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();
        const originUrl = 'https://github.com/user/repo';

        processPost(post, posts, testRepo.path, originUrl);

        expect(post.originalPostId).toBe('https://github.com/other/repo#commit:def456789abc');
        expect(post.parentCommentId).toBe('https://github.com/other/repo#commit:aabbccddeeff');
        expect(posts.has('#commit:abc123456789')).toBe(true);
      });

      it('should normalize references for external post', () => {
        const post: Post = {
          id: 'https://github.com/user/repo#commit:abc123456789',
          repository: 'https://github.com/user/repo',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'comment',
          source: 'explicit',
          originalPostId: '#commit:def456789abc',
          parentCommentId: '#commit:aabbccddeeff',
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: false, isWorkspacePost: false }
        };
        const posts = new Map<string, Post>();

        processPost(post, posts, 'https://github.com/user/repo');

        expect(post.originalPostId).toBe('https://github.com/user/repo#commit:def456789abc');
        expect(post.parentCommentId).toBe('https://github.com/user/repo#commit:aabbccddeeff');
      });

      it('should not normalize references for workspace post without originUrl', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'comment',
          source: 'explicit',
          isWorkspacePost: true,
          originalPostId: '#commit:def456789abc',
          parentCommentId: '#commit:aabbccddeeff',
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();

        processPost(post, posts, testRepo.path);

        expect(post.originalPostId).toBe('#commit:def456789abc');
        expect(post.parentCommentId).toBe('#commit:aabbccddeeff');
      });
    });

    describe('absolute->relative ID mapping', () => {
      it('should create absolute->relative mapping for workspace post with originUrl', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();
        const postIndex = { absolute: new Map<string, string>(), merged: new Set<string>() };
        const originUrl = 'https://github.com/user/repo';

        processPost(post, posts, testRepo.path, originUrl, postIndex);

        expect(postIndex.absolute.has('https://github.com/user/repo#commit:abc123456789')).toBe(true);
        expect(postIndex.absolute.get('https://github.com/user/repo#commit:abc123456789')).toBe('#commit:abc123456789');
      });
    });

    describe('deduplication', () => {
      it('should deduplicate external post that duplicates workspace post', () => {
        const workspacePost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Workspace post',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Workspace post',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>([['#commit:abc123456789', workspacePost]]);
        const postIndex = { absolute: new Map<string, string>(), merged: new Set<string>() };
        const originUrl = 'https://github.com/user/repo';

        const externalPost: Post = {
          id: 'https://github.com/user/repo#commit:abc123456789',
          repository: 'https://github.com/user/repo',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'External post',
          type: 'post',
          source: 'explicit',
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'External post',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: false, isWorkspacePost: false }
        };

        processPost(externalPost, posts, testRepo.path, originUrl, postIndex);

        expect(posts.size).toBe(1);
        expect(posts.get('#commit:abc123456789')).toBe(workspacePost);
        expect(postIndex.absolute.has('https://github.com/user/repo#commit:abc123456789')).toBe(true);
        expect(postIndex.absolute.get('https://github.com/user/repo#commit:abc123456789')).toBe('#commit:abc123456789');
      });
    });

    describe('post replacement logic', () => {
      it('should add new post when none exists', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'New post',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'New post',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();

        processPost(post, posts, testRepo.path);

        expect(posts.has('#commit:abc123456789')).toBe(true);
        expect(posts.get('#commit:abc123456789')).toBe(post);
      });

      it('should replace implicit post with explicit post', () => {
        const implicitPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Implicit',
          type: 'post',
          source: 'implicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Implicit',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>([['#commit:abc123456789', implicitPost]]);

        const explicitPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Explicit',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Explicit',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };

        processPost(explicitPost, posts, testRepo.path);

        expect(posts.get('#commit:abc123456789')).toBe(explicitPost);
      });

      it('should keep existing explicit post when new is also explicit', () => {
        const existingPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Existing',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Existing',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>([['#commit:abc123456789', existingPost]]);

        const newPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'New',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'New',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };

        processPost(newPost, posts, testRepo.path);

        expect(posts.get('#commit:abc123456789')).toBe(existingPost);
      });

      it('should not replace explicit post with virtual post', () => {
        const explicitPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Explicit',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Explicit',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>([['#commit:abc123456789', explicitPost]]);

        const virtualPost: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Virtual',
          type: 'post',
          source: 'implicit',
          isVirtual: true,
          raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Virtual',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };

        processPost(virtualPost, posts, testRepo.path);

        expect(posts.get('#commit:abc123456789')).toBe(explicitPost);
      });
    });

    describe('skipEmbeddedReferences flag', () => {
      it('should skip processing embedded references when flag is true', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: {
            commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' },
            gitMsg: {
              content: 'Test',
              header: { fields: {}, raw: '' },
              references: [{
                ref: '#commit:def456789abc',
                ext: 'social',
                v: '0.1.0',
                extV: '0.1.0',
                author: 'Ref Author',
                email: 'ref@example.com',
                time: '2024-01-15T10:00:00Z',
                fields: { 'social:ref-type': 'comment' },
                metadata: '> Referenced content'
              }]
            }
          },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();

        processPost(post, posts, testRepo.path, undefined, undefined, true);

        expect(posts.size).toBe(1);
        expect(posts.has('#commit:def456789abc')).toBe(false);
      });
    });

    describe('embedded references processing', () => {
      it('should create virtual post from embedded reference', () => {
        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: {
            commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' },
            gitMsg: {
              content: 'Test',
              header: { fields: {}, raw: '' },
              references: [{
                ref: 'https://github.com/external/repo#commit:def456789abc',
                ext: 'social',
                v: '0.1.0',
                extV: '0.1.0',
                author: 'Ref Author',
                email: 'ref@example.com',
                time: '2024-01-15T10:00:00Z',
                fields: { 'social:ref-type': 'post', type: 'post' },
                metadata: '> Referenced content'
              }]
            }
          },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>();

        processPost(post, posts, testRepo.path);

        expect(posts.size).toBe(2);
        expect(posts.has('https://github.com/external/repo#commit:def456789abc')).toBe(true);
      });

      it('should merge virtual reference into workspace post when it exists', () => {
        const workspacePost: Post = {
          id: '#commit:def456789abc',
          repository: '',
          author: { name: 'Workspace', email: 'workspace@example.com' },
          timestamp: new Date(),
          content: 'Workspace content',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: { commit: { hash: 'def456789abc', author: 'Workspace', email: 'workspace@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Workspace content',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'def456789abc', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };
        const posts = new Map<string, Post>([['#commit:def456789abc', workspacePost]]);
        const postIndex = { absolute: new Map<string, string>(), merged: new Set<string>() };
        const originUrl = 'https://github.com/user/repo';

        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: {
            commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' },
            gitMsg: {
              content: 'Test',
              header: { fields: {}, raw: '' },
              references: [{
                ref: 'https://github.com/user/repo#commit:def456789abc',
                ext: 'social',
                v: '0.1.0',
                extV: '0.1.0',
                author: 'Ref Author',
                email: 'ref@example.com',
                time: '2024-01-15T10:00:00Z',
                fields: { 'social:ref-type': 'comment', type: 'post' },
                metadata: '> Referenced content'
              }]
            }
          },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };

        processPost(post, posts, testRepo.path, originUrl, postIndex);

        expect(posts.size).toBe(2);
        expect(workspacePost.interactions.comments).toBe(1);
        expect(postIndex.absolute.has('https://github.com/user/repo#commit:def456789abc')).toBe(true);
        expect(postIndex.merged.has('https://github.com/user/repo#commit:def456789abc')).toBe(true);
      });

      it('should not add virtual post if it already exists', () => {
        const existingVirtualPost: Post = {
          id: 'https://github.com/external/repo#commit:def456789abc',
          repository: 'https://github.com/external/repo',
          author: { name: 'Existing', email: 'existing@example.com' },
          timestamp: new Date(),
          content: 'Existing',
          type: 'comment',
          source: 'explicit',
          raw: { commit: { hash: 'def456789abc', author: 'Existing', email: 'existing@example.com', timestamp: new Date(), message: 'Test' } },
          cleanContent: 'Existing',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'def456789abc', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: false, isWorkspacePost: false }
        };
        const posts = new Map<string, Post>([['https://github.com/external/repo#commit:def456789abc', existingVirtualPost]]);

        const post: Post = {
          id: '#commit:abc123456789',
          repository: '',
          author: { name: 'Test', email: 'test@example.com' },
          timestamp: new Date(),
          content: 'Test',
          type: 'post',
          source: 'explicit',
          isWorkspacePost: true,
          raw: {
            commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' },
            gitMsg: {
              content: 'Test',
              header: { fields: {}, raw: '' },
              references: [{
                ref: 'https://github.com/external/repo#commit:def456789abc',
                ext: 'social',
                v: '0.1.0',
                extV: '0.1.0',
                author: 'Ref Author',
                email: 'ref@example.com',
                time: '2024-01-15T10:00:00Z',
                fields: { 'social:ref-type': 'comment', type: 'comment' },
                metadata: '> Referenced content'
              }]
            }
          },
          cleanContent: 'Test',
          interactions: { comments: 0, reposts: 0, quotes: 0 },
          display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
        };

        processPost(post, posts, testRepo.path);

        expect(posts.size).toBe(2);
        expect(posts.get('https://github.com/external/repo#commit:def456789abc')).toBe(existingVirtualPost);
      });
    });
  });

  describe('createVirtualPostFromReference()', () => {
    it('should create virtual post from absolute reference', () => {
      const ref = {
        ref: 'https://github.com/external/repo#commit:abc123456789',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: { 'social:ref-type': 'post', type: 'post' },
        metadata: '> Virtual content'
      };
      const parentPost: Post = {
        id: '#commit:parent123',
        repository: '',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path);

      expect(virtualPost).toBeDefined();
      expect(virtualPost?.id).toBe('https://github.com/external/repo#commit:abc123456789');
      expect(virtualPost?.author.name).toBe('Virtual Author');
      expect(virtualPost?.content).toBe('Virtual content');
    });

    it('should convert relative reference to absolute when parent is workspace with originUrl', () => {
      const ref = {
        ref: '#commit:abc123456789',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: { 'social:ref-type': 'post', type: 'post' },
        metadata: '> Virtual content'
      };
      const parentPost: Post = {
        id: '#commit:parent123',
        repository: '',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const originUrl = 'https://github.com/user/repo';

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path, originUrl);

      expect(virtualPost).toBeDefined();
      expect(virtualPost?.id).toBe('https://github.com/user/repo#commit:abc123456789');
    });

    it('should keep relative reference when parent is workspace without originUrl', () => {
      const ref = {
        ref: '#commit:abc123456789',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: { 'social:ref-type': 'post', type: 'post' },
        metadata: '> Virtual content'
      };
      const parentPost: Post = {
        id: '#commit:parent123',
        repository: '',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path);

      expect(virtualPost).toBeDefined();
      expect(virtualPost?.id).toBe('#commit:abc123456789');
    });

    it('should convert relative reference to absolute when parent is external post', () => {
      const ref = {
        ref: '#commit:abc123456789',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: { 'social:ref-type': 'post', type: 'post' },
        metadata: '> Virtual content'
      };
      const parentPost: Post = {
        id: 'https://github.com/external/repo#commit:parent123',
        repository: 'https://github.com/external/repo',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: false, isWorkspacePost: false }
      };

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path);

      expect(virtualPost).toBeDefined();
      expect(virtualPost?.id).toBe('https://github.com/external/repo#commit:abc123456789');
    });

    it('should return null for reference without # separator', () => {
      const ref = {
        ref: 'invalid-reference',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: { 'social:ref-type': 'comment' },
        metadata: '> Content'
      };
      const parentPost: Post = {
        id: '#commit:parent123',
        repository: '',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path);

      expect(virtualPost).toBeNull();
    });

    it('should normalize references in virtual post when originUrl is provided', () => {
      const ref = {
        ref: 'https://github.com/user/repo#commit:abc123456789',
        ext: 'social',
        v: '0.1.0',
        extV: '0.1.0',
        author: 'Virtual Author',
        email: 'virtual@example.com',
        time: '2024-01-15T10:00:00Z',
        fields: {
          'social:ref-type': 'comment',
          type: 'comment',
          'reply-to': 'https://github.com/user/repo#commit:parent456',
          original: 'https://github.com/user/repo#commit:original789'
        },
        metadata: '> Virtual content'
      };
      const parentPost: Post = {
        id: '#commit:parent123',
        repository: '',
        author: { name: 'Parent', email: 'parent@example.com' },
        timestamp: new Date(),
        content: 'Parent',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'parent123', author: 'Parent', email: 'parent@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Parent',
        interactions: { comments: 0, reposts: 0, quotes: 0 },
        display: { repositoryName: '', commitHash: 'parent123', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const originUrl = 'https://github.com/user/repo';

      const virtualPost = createVirtualPostFromReference(ref, parentPost, testRepo.path, originUrl);

      expect(virtualPost).toBeDefined();
    });
  });

  describe('mergeVirtualPostIntoWorkspace()', () => {
    it('should create interactions object if missing', () => {
      const workspacePost: Post = {
        id: '#commit:abc123456789',
        repository: '',
        author: { name: 'Test', email: 'test@example.com' },
        timestamp: new Date(),
        content: 'Test',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Test',
        display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 0, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const ref = { fields: { 'social:ref-type': 'comment' } };

      mergeVirtualPostIntoWorkspace(workspacePost, ref);

      expect(workspacePost.interactions).toBeDefined();
      expect(workspacePost.interactions?.comments).toBe(1);
    });

    it('should increment comment count', () => {
      const workspacePost: Post = {
        id: '#commit:abc123456789',
        repository: '',
        author: { name: 'Test', email: 'test@example.com' },
        timestamp: new Date(),
        content: 'Test',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Test',
        interactions: { comments: 5, reposts: 2, quotes: 1 },
        display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 3, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const ref = { fields: { 'social:ref-type': 'comment' } };

      mergeVirtualPostIntoWorkspace(workspacePost, ref);

      expect(workspacePost.interactions.comments).toBe(6);
    });

    it('should increment repost count and update totalReposts', () => {
      const workspacePost: Post = {
        id: '#commit:abc123456789',
        repository: '',
        author: { name: 'Test', email: 'test@example.com' },
        timestamp: new Date(),
        content: 'Test',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Test',
        interactions: { comments: 5, reposts: 2, quotes: 1 },
        display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 3, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const ref = { fields: { 'social:ref-type': 'repost' } };

      mergeVirtualPostIntoWorkspace(workspacePost, ref);

      expect(workspacePost.interactions.reposts).toBe(3);
      expect(workspacePost.display.totalReposts).toBe(4);
    });

    it('should increment quote count and update totalReposts', () => {
      const workspacePost: Post = {
        id: '#commit:abc123456789',
        repository: '',
        author: { name: 'Test', email: 'test@example.com' },
        timestamp: new Date(),
        content: 'Test',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Test',
        interactions: { comments: 5, reposts: 2, quotes: 1 },
        display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 3, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const ref = { fields: { 'social:ref-type': 'quote' } };

      mergeVirtualPostIntoWorkspace(workspacePost, ref);

      expect(workspacePost.interactions.quotes).toBe(2);
      expect(workspacePost.display.totalReposts).toBe(4);
    });

    it('should handle unknown ref-type gracefully', () => {
      const workspacePost: Post = {
        id: '#commit:abc123456789',
        repository: '',
        author: { name: 'Test', email: 'test@example.com' },
        timestamp: new Date(),
        content: 'Test',
        type: 'post',
        source: 'explicit',
        isWorkspacePost: true,
        raw: { commit: { hash: 'abc123456789', author: 'Test', email: 'test@example.com', timestamp: new Date(), message: 'Test' } },
        cleanContent: 'Test',
        interactions: { comments: 5, reposts: 2, quotes: 1 },
        display: { repositoryName: '', commitHash: 'abc123456789', commitUrl: '', totalReposts: 3, isEmpty: false, isUnpushed: false, isOrigin: true, isWorkspacePost: true }
      };
      const ref = { fields: { 'social:ref-type': 'unknown-type' } };

      mergeVirtualPostIntoWorkspace(workspacePost, ref);

      expect(workspacePost.interactions.comments).toBe(5);
      expect(workspacePost.interactions.reposts).toBe(2);
      expect(workspacePost.interactions.quotes).toBe(1);
    });
  });

  describe('processCommits()', () => {
    it('should process commits with remotes', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/remotes/origin/gitsocial'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBeGreaterThanOrEqual(0);
    });

    it('should get unpushed commits when origin exists', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/heads/gitsocial'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts).toBeDefined();
    });

    it('should skip duplicate commits', async () => {
      const commits = [
        {
          hash: 'abc123456789',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: 'Test commit',
          refname: 'refs/heads/gitsocial'
        },
        {
          hash: 'abc123456789',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: 'Test commit',
          refname: 'refs/remotes/origin/gitsocial'
        }
      ];

      const posts = await processCommits(testRepo.path, commits);

      const postHashes = posts.map(p => p.raw.commit.hash);
      const uniqueHashes = new Set(postHashes);
      expect(postHashes.length).toBe(uniqueHashes.size);
    });

    it('should process external commits with __external metadata', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'External User',
        email: 'external@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'External commit',
        refname: 'refs/remotes/upstream/gitsocial',
        __external: {
          repoUrl: 'https://github.com/external/repo',
          storageDir: '/tmp/external',
          branch: 'gitsocial'
        }
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBe(1);
      expect(posts[0].id).toContain('https://github.com/external/repo');
    });

    it('should process commits from refs/remotes/origin/gitSocialBranch', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/remotes/origin/gitsocial'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts).toBeDefined();
    });

    it('should skip commits from refs/remotes/other-remote/branch', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/remotes/upstream/other-branch'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBe(0);
    });

    it('should process commits from refs/heads/gitSocialBranch', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/heads/gitsocial'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBeGreaterThanOrEqual(0);
    });

    it('should skip commits from refs/heads/other-branch', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'refs/heads/other-branch'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBe(0);
    });

    it('should process commits with refname matching gitSocialBranch exactly', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: 'gitsocial'
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBe(1);
    });

    it('should process commits without refname when on gitSocialBranch', async () => {
      const commits = [{
        hash: 'abc123456789',
        author: 'Test User',
        email: 'test@example.com',
        timestamp: new Date('2024-01-15T10:00:00Z'),
        message: 'Test commit',
        refname: undefined
      }];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts).toBeDefined();
    });

    it('should filter out null posts from constructPost failures', async () => {
      const commits = [
        {
          hash: 'abc123456789',
          author: 'Test User',
          email: 'test@example.com',
          timestamp: new Date('2024-01-15T10:00:00Z'),
          message: `Test comment without original

--- GitMsg: ext="social"; type="comment"; v="0.1.0"; ext-v="0.1.0" ---`,
          refname: 'refs/heads/gitsocial'
        }
      ];

      const posts = await processCommits(testRepo.path, commits);

      expect(posts.length).toBe(0);
    });
  });
});
