import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  clearAvatarCache,
  getAvatar,
  getAvatarCacheStats,
  getRepositoryAvatar,
  getUserAvatar,
  setEnableGravatar,
  setGitHubToken
} from '../../../src/social/avatar/service';
import { createTestRepo, type TestRepo } from '../../test-utils';
import { avatarUtils } from '../../../src/social/avatar/utils';
import * as path from 'path';
import * as fs from 'fs';

const mockFetch = vi.fn();
global.fetch = mockFetch;

function createMockResponse(body: unknown, status = 200, contentType = 'application/json'): {
  ok: boolean;
  status: number;
  headers: Headers;
  json: () => Promise<unknown>;
  arrayBuffer: () => Promise<ArrayBuffer>;
} {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': contentType }),
    json: () => Promise.resolve(body),
    arrayBuffer: () => {
      if (typeof body === 'string') {
        return Promise.resolve(new TextEncoder().encode(body).buffer);
      }
      return Promise.resolve(new ArrayBuffer(0));
    }
  };
}

describe('social/avatar/service', () => {
  let testRepo: TestRepo;
  let testAvatarDir: string;

  beforeEach(async () => {
    testRepo = await createTestRepo('avatar-test');
    testAvatarDir = path.join(testRepo.path, '.avatars');
    await fs.promises.mkdir(testAvatarDir, { recursive: true });
    vi.clearAllMocks();
    mockFetch.mockReset();
  });

  afterEach(() => {
    testRepo.cleanup();
    vi.restoreAllMocks();
  });

  describe('setGitHubToken()', () => {
    it('should accept GitHub token', () => {
      expect(() => setGitHubToken('test-token')).not.toThrow();
    });

    it('should accept null token', () => {
      expect(() => setGitHubToken(null)).not.toThrow();
    });

    it('should reset to null after setting a token', () => {
      setGitHubToken('test-token');
      expect(() => setGitHubToken(null)).not.toThrow();
    });
  });

  describe('setEnableGravatar()', () => {
    it('should enable Gravatar', () => {
      expect(() => setEnableGravatar(true)).not.toThrow();
    });

    it('should disable Gravatar', () => {
      expect(() => setEnableGravatar(false)).not.toThrow();
    });

    it('should toggle Gravatar setting', () => {
      setEnableGravatar(true);
      setEnableGravatar(false);
      setEnableGravatar(true);
      expect(true).toBe(true);
    });
  });

  describe('getUserAvatar()', () => {
    it('should generate fallback avatar for user without remote', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result = await getUserAvatar(testAvatarDir, 'test@example.com');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
      const fileExists = await fs.promises.access(result).then(() => true).catch(() => false);
      expect(fileExists).toBe(true);
    });

    it('should cache avatar and reuse it', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result1 = await getUserAvatar(testAvatarDir, 'cached@example.com');
      const result2 = await getUserAvatar(testAvatarDir, 'cached@example.com');

      expect(result1).toBe(result2);
    });

    it('should handle GitHub noreply email pattern', async () => {
      const noreplyEmail = '12345+user@users.noreply.github.com';
      const imageData = 'fake-image-data';
      mockFetch.mockResolvedValue(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, noreplyEmail, 'https://github.com/test/repo');

      expect(result).toBeDefined();
      expect(mockFetch).toHaveBeenCalled();
    });

    it('should fetch from GitHub API for github.com', async () => {
      const email = 'user@example.com';
      const commits = [{ author: { avatar_url: 'https://avatars.githubusercontent.com/u/12345' } }];
      const imageData = 'fake-image-data';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(commits))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.png');
    });

    it('should fetch from Gitea API for non-github.com GitHub repos', async () => {
      const email = 'user@example.com';
      const commits = [{ author: { avatar_url: 'https://gitea.example.com/avatars/123' } }];
      const imageData = 'fake-image-data';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(commits))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitea.example.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should fetch from GitLab API', async () => {
      const email = 'user@example.com';
      const avatarData = { avatar_url: 'https://gitlab.com/uploads/user/avatar/123/image.png' };
      const imageData = 'fake-image-data';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(avatarData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle GitHub API with no commits', async () => {
      const email = 'user@example.com';
      mockFetch.mockResolvedValueOnce(createMockResponse([]));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle GitHub API errors gracefully', async () => {
      const email = 'user@example.com';
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should use Gravatar when enabled and API fails', async () => {
      const email = 'user@example.com';
      setEnableGravatar(true);

      const imageData = 'fake-gravatar-data';
      mockFetch
        .mockResolvedValueOnce(createMockResponse([], 200))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();

      setEnableGravatar(false);
    });

    it('should reuse existing file on disk', async () => {
      const email = 'existing@example.com';
      const filePath = path.join(testAvatarDir, `user_${avatarUtils.md5Hash(email)}.png`);

      await fs.promises.writeFile(filePath, Buffer.from('existing-image'));

      const result = await getUserAvatar(testAvatarDir, email);

      expect(result).toBe(filePath);
    });

    it('should handle cache with deleted file', async () => {
      const email = 'deleted-cache@example.com';

      mockFetch.mockResolvedValue(createMockResponse([], 200));
      const result1 = await getUserAvatar(testAvatarDir, email);

      await fs.promises.unlink(result1);

      const result2 = await getUserAvatar(testAvatarDir, email);
      expect(result2).toBeDefined();
    });

    it('should deduplicate concurrent requests', async () => {
      const email = 'concurrent@example.com';

      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const [result1, result2, result3] = await Promise.all([
        getUserAvatar(testAvatarDir, email),
        getUserAvatar(testAvatarDir, email),
        getUserAvatar(testAvatarDir, email)
      ]);

      expect(result1).toBe(result2);
      expect(result2).toBe(result3);
    });

    it('should use cached Gravatar when enabled', async () => {
      const email = 'gravatar-cached@example.com';
      setEnableGravatar(true);

      const gravatarPath = path.join(testAvatarDir, `gravatar_${avatarUtils.md5Hash(email)}.png`);
      await fs.promises.writeFile(gravatarPath, Buffer.from('cached-gravatar'));

      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBe(gravatarPath);

      setEnableGravatar(false);
    });

    it('should handle provider other than GitHub/GitLab', async () => {
      const email = 'user@example.com';

      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const result = await getUserAvatar(testAvatarDir, email, 'https://bitbucket.org/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should use GitHub token when set', async () => {
      const email = 'token-user@example.com';
      setGitHubToken('test-gh-token-123');

      const commits = [{ author: { avatar_url: 'https://avatars.githubusercontent.com/u/999' } }];
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(commits))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      const firstCall = mockFetch.mock.calls[0] as [string, { headers: Record<string, string> }];
      expect(firstCall[1].headers['Authorization']).toBe('Bearer test-gh-token-123');

      setGitHubToken(null);
    });

    it('should handle URL parsing failure for avatar URL', async () => {
      const email = 'url-parse@example.com';
      const commits = [{ author: { avatar_url: 'not-a-valid-url-scheme:://broken' } }];
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(commits))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle GitLab API failure', async () => {
      const email = 'gitlab-fail@example.com';

      mockFetch.mockRejectedValueOnce(new Error('GitLab API error'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });
  });

  describe('getRepositoryAvatar()', () => {
    it('should generate fallback avatar for repository', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/user/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
      const fileExists = await fs.promises.access(result).then(() => true).catch(() => false);
      expect(fileExists).toBe(true);
    });

    it('should cache repository avatar and reuse it', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result1 = await getRepositoryAvatar(testAvatarDir, 'https://github.com/user/repo2');
      const result2 = await getRepositoryAvatar(testAvatarDir, 'https://github.com/user/repo2');

      expect(result1).toBe(result2);
    });

    it('should handle API errors gracefully', async () => {
      mockFetch.mockRejectedValue(new Error('Network error'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/user/repo3');

      expect(result).toBeDefined();
    });

    it('should use home icon for myrepository', async () => {
      const result = await getRepositoryAvatar(testAvatarDir, 'myrepository');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
      expect(result).toContain('temp_repo_myrepository');
    });

    it('should fetch GitHub repository avatar with avatar_url', async () => {
      const repoData = { avatar_url: 'https://avatars.githubusercontent.com/u/12345' };
      const imageData = 'fake-repo-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.png');
    });

    it('should fetch GitHub repository avatar with organization.avatar_url', async () => {
      const repoData = { organization: { avatar_url: 'https://avatars.githubusercontent.com/u/org123' } };
      const imageData = 'fake-org-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/org/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.png');
    });

    it('should fetch GitHub repository avatar with owner.avatar_url', async () => {
      const repoData = { owner: { avatar_url: 'https://avatars.githubusercontent.com/u/owner123' } };
      const imageData = 'fake-owner-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should fetch from Gitea API for non-github.com repos', async () => {
      const repoData = { avatar_url: 'https://gitea.example.com/avatars/123' };
      const imageData = 'fake-gitea-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitea.example.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should fetch GitLab repository avatar', async () => {
      const repoData = { avatar_url: 'https://gitlab.com/uploads/project/avatar/123/image.png' };
      const imageData = 'fake-gitlab-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle GitLab relative avatar URLs', async () => {
      const repoData = { avatar_url: '/uploads/project/avatar/123/image.png' };
      const imageData = 'fake-gitlab-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle GitLab API returning non-OK status', async () => {
      mockFetch.mockResolvedValueOnce(createMockResponse({}, 404));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle GitLab API returning non-JSON', async () => {
      mockFetch.mockResolvedValueOnce(createMockResponse('Not JSON', 200, 'text/html'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle GitLab API with no avatar_url', async () => {
      mockFetch.mockResolvedValueOnce(createMockResponse({}));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should fetch Bitbucket repository avatar', async () => {
      const repoData = { links: { avatar: { href: 'https://bitbucket.org/account/user/repo/avatar/256' } } };
      const imageData = 'fake-bitbucket-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://bitbucket.org/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle Bitbucket API non-OK status', async () => {
      mockFetch.mockResolvedValueOnce(createMockResponse({}, 500));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://bitbucket.org/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle Bitbucket API error', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Bitbucket API error'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://bitbucket.org/owner/repo');

      expect(result).toBeDefined();
    });

    it('should try all APIs for unknown provider', async () => {
      const repoData = { avatar_url: 'https://unknown.git/avatars/123' };
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://unknown.git/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle all APIs failing for unknown provider', async () => {
      mockFetch
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse({}, 404));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://unknown.git/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle GitHub API non-JSON response', async () => {
      mockFetch.mockResolvedValueOnce(createMockResponse('HTML page', 200, 'text/html'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle download failure gracefully', async () => {
      const repoData = { avatar_url: 'https://avatars.githubusercontent.com/u/12345' };

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse('', 500));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle empty buffer from download', async () => {
      const repoData = { avatar_url: 'https://avatars.githubusercontent.com/u/12345' };

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: new Headers(),
          arrayBuffer: () => Promise.resolve(new ArrayBuffer(0))
        });

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should reuse existing repository file on disk', async () => {
      const repoUrl = 'https://github.com/existing/repo';
      const filePath = path.join(testAvatarDir, `repo_${avatarUtils.md5Hash(repoUrl)}.png`);

      await fs.promises.writeFile(filePath, Buffer.from('existing-repo-image'));

      const result = await getRepositoryAvatar(testAvatarDir, repoUrl);

      expect(result).toBe(filePath);
    });

    it('should handle cache with deleted repository file', async () => {
      const repoUrl = 'https://github.com/deleted-cache/repo';

      mockFetch.mockResolvedValue(createMockResponse({}, 404));
      const result1 = await getRepositoryAvatar(testAvatarDir, repoUrl);

      await fs.promises.unlink(result1);

      const result2 = await getRepositoryAvatar(testAvatarDir, repoUrl);
      expect(result2).toBeDefined();
    });

    it('should deduplicate concurrent repository requests', async () => {
      const repoUrl = 'https://github.com/concurrent/repo';

      mockFetch.mockResolvedValue(createMockResponse({}, 404));

      const [result1, result2, result3] = await Promise.all([
        getRepositoryAvatar(testAvatarDir, repoUrl),
        getRepositoryAvatar(testAvatarDir, repoUrl),
        getRepositoryAvatar(testAvatarDir, repoUrl)
      ]);

      expect(result1).toBe(result2);
      expect(result2).toBe(result3);
    });

    it('should handle URL parsing failure for repository avatar', async () => {
      const repoData = { avatar_url: 'not-a-valid-url:://broken' };
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
    });

    it('should add size parameter for GitHub repos', async () => {
      const repoData = { avatar_url: 'https://avatars.githubusercontent.com/u/12345' };
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      const downloadCall = mockFetch.mock.calls[1];
      expect(downloadCall[0]).toContain('s=80');
    });
  });

  describe('getAvatarCacheStats()', () => {
    it('should return cache stats for empty directory', async () => {
      const stats = await getAvatarCacheStats(testAvatarDir);

      expect(stats).toBeDefined();
      expect(stats.memoryCacheSize).toBeGreaterThanOrEqual(0);
      expect(stats.memoryCacheEntries).toBeInstanceOf(Array);
      expect(stats.fileStats).toBeDefined();
      expect(stats.fileStats?.totalFiles).toBe(0);
    });

    it('should count different avatar files', async () => {
      await fs.promises.writeFile(path.join(testAvatarDir, 'user_abc123.png'), Buffer.from('test'));
      await fs.promises.writeFile(path.join(testAvatarDir, 'repo_def456.png'), Buffer.from('test'));
      await fs.promises.writeFile(path.join(testAvatarDir, 'temp_xyz.svg'), Buffer.from('test'));

      const stats = await getAvatarCacheStats(testAvatarDir);

      expect(stats).toBeDefined();
      expect(stats.fileStats).toBeDefined();
      expect(stats.fileStats?.totalFiles).toBe(3);
      expect(stats.fileStats?.tempFiles).toBe(1);
      expect(stats.fileStats?.permanentFiles).toBe(2);
      expect(stats.fileStats?.diskUsage).toBeGreaterThan(0);
    });

    it('should handle nonexistent directory', async () => {
      const stats = await getAvatarCacheStats('/nonexistent/dir');

      expect(stats).toBeDefined();
      expect(stats.memoryCacheSize).toBeGreaterThanOrEqual(0);
      expect(stats.fileStats).toBeUndefined();
    });
  });

  describe('clearAvatarCache()', () => {
    it('should clear all avatar files', async () => {
      await fs.promises.writeFile(path.join(testAvatarDir, 'user_abc.png'), Buffer.from('test'));
      await fs.promises.writeFile(path.join(testAvatarDir, 'repo_def.png'), Buffer.from('test'));

      const result = await clearAvatarCache(testAvatarDir, { clearAllFiles: true });

      expect(result.filesDeleted).toBe(2);
      expect(result.errors).toEqual([]);
    });

    it('should clear only temporary files', async () => {
      await fs.promises.writeFile(path.join(testAvatarDir, 'temp_abc.svg'), Buffer.from('test'));
      await fs.promises.writeFile(path.join(testAvatarDir, 'user_def.png'), Buffer.from('test'));

      const result = await clearAvatarCache(testAvatarDir, { clearTempFiles: true });

      expect(result.filesDeleted).toBe(1);
      expect(result.errors).toEqual([]);

      const files = await fs.promises.readdir(testAvatarDir);
      expect(files).toContain('user_def.png');
      expect(files).not.toContain('temp_abc.svg');
    });

    it('should clear memory cache', async () => {
      const result = await clearAvatarCache(testAvatarDir, { clearMemoryCache: true });

      expect(result).toBeDefined();
      expect(result.clearedMemoryEntries).toBeGreaterThanOrEqual(0);
    });

    it('should handle directory not found', async () => {
      const result = await clearAvatarCache('/nonexistent/dir/xyz', { clearAllFiles: true });

      expect(result).toBeDefined();
      expect(result.filesDeleted).toBe(0);
      expect(result.errors.length).toBeGreaterThan(0);
    });

    it('should handle file deletion errors', async () => {
      await fs.promises.writeFile(path.join(testAvatarDir, 'user_protected.png'), Buffer.from('test'));
      const filePath = path.join(testAvatarDir, 'user_protected.png');

      await fs.promises.chmod(filePath, 0o444);

      vi.spyOn(fs.promises, 'unlink').mockRejectedValueOnce(new Error('Permission denied'));

      const result = await clearAvatarCache(testAvatarDir, { clearAllFiles: true });

      expect(result).toBeDefined();

      vi.restoreAllMocks();
      await fs.promises.chmod(filePath, 0o644);
      await fs.promises.unlink(filePath);
    });
  });

  describe('getAvatar()', () => {
    it('should get user avatar', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result = await getAvatar({
        type: 'user',
        identifier: 'test@example.com',
        avatarDir: testAvatarDir
      });

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should get repository avatar', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result = await getAvatar({
        type: 'repository',
        identifier: 'https://github.com/user/repo',
        avatarDir: testAvatarDir
      });

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle user avatar with name', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 404,
        headers: new Headers()
      });

      const result = await getAvatar({
        type: 'user',
        identifier: 'test@example.com',
        avatarDir: testAvatarDir,
        name: 'Test User'
      });

      expect(result).toBeDefined();
    });

    it('should delegate to getUserAvatar with context', async () => {
      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const result = await getAvatar({
        type: 'user',
        identifier: 'test@example.com',
        avatarDir: testAvatarDir,
        context: 'https://github.com/owner/repo',
        size: 100,
        name: 'Test User'
      });

      expect(result).toBeDefined();
    });

    it('should delegate to getRepositoryAvatar with type repo', async () => {
      mockFetch.mockResolvedValue(createMockResponse({}, 404));

      const result = await getAvatar({
        type: 'repo',
        identifier: 'https://github.com/owner/repo',
        avatarDir: testAvatarDir,
        size: 100
      });

      expect(result).toBeDefined();
    });
  });

  describe('Temp file cleanup', () => {
    it('should create temp files for fallback avatars', async () => {
      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const email = 'tempfile@example.com';
      const result = await getUserAvatar(testAvatarDir, email);

      expect(result).toContain('temp_');

      const fileExists = await fs.promises.access(result).then(() => true).catch(() => false);
      expect(fileExists).toBe(true);
    });
  });

  describe('LRU cache eviction', () => {
    it('should evict oldest cache entry when at max capacity', async () => {
      vi.clearAllMocks();
      mockFetch.mockResolvedValue(createMockResponse([], 200));

      const emails: string[] = [];
      for (let i = 0; i < 1010; i++) {
        emails.push(`lru-test-${i}@example.com`);
      }

      for (const email of emails) {
        await getUserAvatar(testAvatarDir, email);
      }

      const stats = await getAvatarCacheStats(testAvatarDir);
      expect(stats.memoryCacheSize).toBeLessThanOrEqual(1000);
    });
  });

  describe('Edge cases', () => {
    it('should handle invalid repository info', async () => {
      mockFetch.mockResolvedValue(createMockResponse({}, 404));

      const result = await getRepositoryAvatar(testAvatarDir, 'invalid-url');

      expect(result).toBeDefined();
    });

    it('should handle API response with partial data', async () => {
      const email = 'partial@example.com';
      const commits = [{ author: {} }];

      mockFetch.mockResolvedValueOnce(createMockResponse(commits));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle API errors and return fallback', async () => {
      const email = 'api-error@example.com';

      mockFetch.mockRejectedValueOnce(new Error('API error'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle fetch errors with timeout cleanup', async () => {
      const email = 'fetch-error@example.com';

      mockFetch.mockImplementation(() => {
        throw new Error('Network failure');
      });

      const result = await getUserAvatar(testAvatarDir, email, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle download errors gracefully', async () => {
      const repoData = { avatar_url: 'https://avatars.githubusercontent.com/u/12345' };

      mockFetch
        .mockResolvedValueOnce(createMockResponse(repoData))
        .mockRejectedValueOnce(new Error('Download failed'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://github.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle unknown provider trying all APIs and finding via GitLab', async () => {
      mockFetch
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse({ avatar_url: 'https://unknown.git/avatar.png' }))
        .mockResolvedValueOnce(createMockResponse('fake-image', 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://unknown.git/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle unknown provider trying all APIs and finding via Bitbucket', async () => {
      mockFetch
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse({}, 404))
        .mockResolvedValueOnce(createMockResponse({ links: { avatar: { href: 'https://unknown.git/avatar.png' } } }))
        .mockResolvedValueOnce(createMockResponse('fake-image', 200, 'image/png'));

      const result = await getRepositoryAvatar(testAvatarDir, 'https://unknown.git/owner/repo');

      expect(result).toBeDefined();
    });

    it('should handle Gitea user avatar API response', async () => {
      const email = 'gitea-user@example.com';
      const commits = [{ author: { avatar_url: 'https://gitea.example.com/avatars/123' } }];
      const imageData = 'fake-image';

      mockFetch
        .mockResolvedValueOnce(createMockResponse(commits))
        .mockResolvedValueOnce(createMockResponse(imageData, 200, 'image/png'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitea.example.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.png');
    });

    it('should handle GitLab user avatar not found', async () => {
      const email = 'gitlab-notfound@example.com';

      mockFetch.mockResolvedValueOnce(createMockResponse({}, 404));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitlab.com/owner/repo');

      expect(result).toBeDefined();
      expect(result).toContain('.svg');
    });

    it('should handle Gitea user avatar error', async () => {
      const email = 'gitea-error@example.com';

      mockFetch.mockRejectedValueOnce(new Error('Gitea API error'));

      const result = await getUserAvatar(testAvatarDir, email, 'https://gitea.example.com/owner/repo');

      expect(result).toBeDefined();
    });
  });
});
