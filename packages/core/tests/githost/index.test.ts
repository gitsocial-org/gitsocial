import { beforeEach, describe, expect, it, vi } from 'vitest';
import { extractDomain, gitHost } from '../../src/githost/index';

describe('githost', () => {
  describe('extractDomain()', () => {
    it('should extract domain from HTTPS URL', () => {
      const domain = extractDomain('https://github.com/user/repo.git');
      expect(domain).toBe('github.com');
    });

    it('should extract domain from SSH URL', () => {
      const domain = extractDomain('git@github.com:user/repo.git');
      expect(domain).toBe('github.com');
    });

    it('should extract domain from HTTP URL', () => {
      const domain = extractDomain('http://gitlab.com/user/repo');
      expect(domain).toBe('gitlab.com');
    });

    it('should extract domain from URL without protocol', () => {
      const domain = extractDomain('github.com/user/repo');
      expect(domain).toBe('github.com');
    });

    it('should extract domain from Bitbucket URL', () => {
      const domain = extractDomain('https://bitbucket.org/user/repo.git');
      expect(domain).toBe('bitbucket.org');
    });

    it('should extract domain from self-hosted GitLab', () => {
      const domain = extractDomain('https://gitlab.example.com/user/repo.git');
      expect(domain).toBe('gitlab.example.com');
    });

    it('should extract domain-like string from simple input', () => {
      const domain = extractDomain('not-a-valid-url');
      expect(domain).toBe('not-a-valid-url');
    });

    it('should return null for empty string', () => {
      const domain = extractDomain('');
      expect(domain).toBeNull();
    });
  });

  describe('detect()', () => {
    it('should detect GitHub', () => {
      const service = gitHost.detect('https://github.com/user/repo.git');
      expect(service).toBe('github');
    });

    it('should detect GitLab', () => {
      const service = gitHost.detect('https://gitlab.com/user/repo.git');
      expect(service).toBe('gitlab');
    });

    it('should detect Bitbucket', () => {
      const service = gitHost.detect('https://bitbucket.org/user/repo.git');
      expect(service).toBe('bitbucket');
    });

    it('should detect Codeberg as GitHub', () => {
      const service = gitHost.detect('https://codeberg.org/user/repo.git');
      expect(service).toBe('github');
    });

    it('should return unknown for self-hosted', () => {
      const service = gitHost.detect('https://git.example.com/user/repo.git');
      expect(service).toBe('unknown');
    });

    it('should return unknown for empty string', () => {
      const service = gitHost.detect('');
      expect(service).toBe('unknown');
    });

    it('should return unknown for invalid URL', () => {
      const service = gitHost.detect('invalid-url');
      expect(service).toBe('unknown');
    });

    it('should return unknown when extractDomain returns null', () => {
      const service = gitHost.detect(':::invalid:::');
      expect(service).toBe('unknown');
    });

    it('should detect from SSH URLs', () => {
      const service = gitHost.detect('git@github.com:user/repo.git');
      expect(service).toBe('github');
    });

    it('should detect from HTTP URLs', () => {
      const service = gitHost.detect('http://gitlab.com/user/repo');
      expect(service).toBe('gitlab');
    });

    it('should detect from URLs without .git suffix', () => {
      const service = gitHost.detect('https://github.com/user/repo');
      expect(service).toBe('github');
    });
  });

  describe('getDisplayName()', () => {
    it('should return repo name from GitHub URL', () => {
      const name = gitHost.getDisplayName('https://github.com/user/repo.git');
      expect(name).toBe('repo');
    });

    it('should return repo name from GitLab URL', () => {
      const name = gitHost.getDisplayName('https://gitlab.com/user/repo.git');
      expect(name).toBe('repo');
    });

    it('should return repo name without .git suffix', () => {
      const name = gitHost.getDisplayName('https://github.com/user/my-repo.git');
      expect(name).toBe('my-repo');
    });

    it('should handle URL with branch', () => {
      const name = gitHost.getDisplayName('https://github.com/user/repo#main');
      expect(name).toBe('repo');
    });

    it('should handle URL without protocol', () => {
      const name = gitHost.getDisplayName('github.com/user/repo');
      expect(name).toBe('repo');
    });

    it('should return "My Repository" for myrepository', () => {
      const name = gitHost.getDisplayName('myrepository');
      expect(name).toBe('My Repository');
    });

    it('should return "My Repository" for empty string', () => {
      const name = gitHost.getDisplayName('');
      expect(name).toBe('My Repository');
    });

    it('should handle SSH URLs', () => {
      const name = gitHost.getDisplayName('git@github.com:user/repo.git');
      expect(name).toBe('repo');
    });

    it('should handle nested paths', () => {
      const name = gitHost.getDisplayName('https://gitlab.com/group/subgroup/repo.git');
      expect(name).toBe('subgroup');
    });

    it('should fallback to URL on parse error', () => {
      const invalidUrl = 'https://';
      const name = gitHost.getDisplayName(invalidUrl);
      expect(name).toBe(invalidUrl);
    });
  });

  describe('getWebUrl()', () => {
    it('should return web URL from git URL', () => {
      const webUrl = gitHost.getWebUrl('https://github.com/user/repo.git');
      expect(webUrl).toBe('https://github.com/user/repo');
    });

    it('should convert SSH to HTTPS', () => {
      const webUrl = gitHost.getWebUrl('git@github.com:user/repo.git');
      expect(webUrl).toBe('https://github.com/user/repo');
    });

    it('should remove .git suffix', () => {
      const webUrl = gitHost.getWebUrl('https://gitlab.com/user/repo.git');
      expect(webUrl).toBe('https://gitlab.com/user/repo');
    });

    it('should return myrepository unchanged', () => {
      const webUrl = gitHost.getWebUrl('myrepository');
      expect(webUrl).toBe('myrepository');
    });

    it('should return empty string unchanged', () => {
      const webUrl = gitHost.getWebUrl('');
      expect(webUrl).toBe('');
    });

    it('should handle URL without .git', () => {
      const webUrl = gitHost.getWebUrl('https://github.com/user/repo');
      expect(webUrl).toBe('https://github.com/user/repo');
    });
  });

  describe('getCommitUrl()', () => {
    it('should return GitHub commit URL', () => {
      const commitUrl = gitHost.getCommitUrl('https://github.com/user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://github.com/user/repo/commit/abc123');
    });

    it('should return GitLab commit URL', () => {
      const commitUrl = gitHost.getCommitUrl('https://gitlab.com/user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://gitlab.com/user/repo/-/commit/abc123');
    });

    it('should return Bitbucket commit URL', () => {
      const commitUrl = gitHost.getCommitUrl('https://bitbucket.org/user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://bitbucket.org/user/repo/commits/abc123');
    });

    it('should return GitHub-style URL for unknown service', () => {
      const commitUrl = gitHost.getCommitUrl('https://git.example.com/user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://git.example.com/user/repo/commit/abc123');
    });

    it('should return null for myrepository', () => {
      const commitUrl = gitHost.getCommitUrl('myrepository', 'abc123');
      expect(commitUrl).toBeNull();
    });

    it('should return null for empty URL', () => {
      const commitUrl = gitHost.getCommitUrl('', 'abc123');
      expect(commitUrl).toBeNull();
    });

    it('should return null for empty hash', () => {
      const commitUrl = gitHost.getCommitUrl('https://github.com/user/repo.git', '');
      expect(commitUrl).toBeNull();
    });

    it('should return null for local path', () => {
      const commitUrl = gitHost.getCommitUrl('/local/path/repo', 'abc123');
      expect(commitUrl).toBeNull();
    });

    it('should return null for relative path', () => {
      const commitUrl = gitHost.getCommitUrl('./relative/repo', 'abc123');
      expect(commitUrl).toBeNull();
    });

    it('should handle SSH URLs', () => {
      const commitUrl = gitHost.getCommitUrl('git@github.com:user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://github.com/user/repo/commit/abc123');
    });

    it('should handle full commit hashes', () => {
      const fullHash = 'abc123def456abc123def456abc123def456abc1';
      const commitUrl = gitHost.getCommitUrl('https://github.com/user/repo.git', fullHash);
      expect(commitUrl).toBe(`https://github.com/user/repo/commit/${fullHash}`);
    });

    it('should handle Codeberg as GitHub', () => {
      const commitUrl = gitHost.getCommitUrl('https://codeberg.org/user/repo.git', 'abc123');
      expect(commitUrl).toBe('https://codeberg.org/user/repo/commit/abc123');
    });
  });

  describe('parseGitHub()', () => {
    it('should parse GitHub owner and repo', () => {
      const parsed = gitHost.parseGitHub('https://github.com/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should parse Codeberg owner and repo', () => {
      const parsed = gitHost.parseGitHub('https://codeberg.org/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should return null for GitLab URL', () => {
      const parsed = gitHost.parseGitHub('https://gitlab.com/owner/repo.git');
      expect(parsed).toBeNull();
    });

    it('should return null for Bitbucket URL', () => {
      const parsed = gitHost.parseGitHub('https://bitbucket.org/owner/repo.git');
      expect(parsed).toBeNull();
    });

    it('should return null for unknown service', () => {
      const parsed = gitHost.parseGitHub('https://git.example.com/owner/repo.git');
      expect(parsed).toBeNull();
    });

    it('should parse SSH URLs', () => {
      const parsed = gitHost.parseGitHub('git@github.com:owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should handle repo names with .git', () => {
      const parsed = gitHost.parseGitHub('https://github.com/owner/repo.git.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo.git' });
    });
  });

  describe('parseRepo()', () => {
    it('should parse GitHub repo', () => {
      const parsed = gitHost.parseRepo('https://github.com/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should parse GitLab repo', () => {
      const parsed = gitHost.parseRepo('https://gitlab.com/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should parse Bitbucket repo', () => {
      const parsed = gitHost.parseRepo('https://bitbucket.org/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should parse self-hosted repo', () => {
      const parsed = gitHost.parseRepo('https://git.example.com/owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should parse SSH URLs', () => {
      const parsed = gitHost.parseRepo('git@gitlab.com:owner/repo.git');
      expect(parsed).toEqual({ owner: 'owner', repo: 'repo' });
    });

    it('should return null for invalid URL', () => {
      const parsed = gitHost.parseRepo('invalid-url');
      expect(parsed).toBeNull();
    });

    it('should return null for URL with only one path component', () => {
      const parsed = gitHost.parseRepo('https://github.com/single');
      expect(parsed).toBeNull();
    });

    it('should return null for empty string', () => {
      const parsed = gitHost.parseRepo('');
      expect(parsed).toBeNull();
    });

    it('should handle nested paths', () => {
      const parsed = gitHost.parseRepo('https://gitlab.com/group/subgroup/repo.git');
      expect(parsed).toEqual({ owner: 'group', repo: 'subgroup' });
    });
  });

  describe('isGitHub()', () => {
    it('should return true for GitHub', () => {
      expect(gitHost.isGitHub('https://github.com/user/repo.git')).toBe(true);
    });

    it('should return true for Codeberg', () => {
      expect(gitHost.isGitHub('https://codeberg.org/user/repo.git')).toBe(true);
    });

    it('should return false for GitLab', () => {
      expect(gitHost.isGitHub('https://gitlab.com/user/repo.git')).toBe(false);
    });

    it('should return false for Bitbucket', () => {
      expect(gitHost.isGitHub('https://bitbucket.org/user/repo.git')).toBe(false);
    });

    it('should return false for unknown', () => {
      expect(gitHost.isGitHub('https://git.example.com/user/repo.git')).toBe(false);
    });

    it('should return false for empty string', () => {
      expect(gitHost.isGitHub('')).toBe(false);
    });

    it('should handle SSH URLs', () => {
      expect(gitHost.isGitHub('git@github.com:user/repo.git')).toBe(true);
    });
  });

  describe('extractDomain() method', () => {
    it('should extract domain from URL', () => {
      const domain = gitHost.extractDomain('https://github.com/user/repo.git');
      expect(domain).toBe('github.com');
    });

    it('should extract domain from SSH URL', () => {
      const domain = gitHost.extractDomain('git@gitlab.com:user/repo.git');
      expect(domain).toBe('gitlab.com');
    });

    it('should extract domain-like string from simple input', () => {
      const domain = gitHost.extractDomain('invalid');
      expect(domain).toBe('invalid');
    });
  });

  describe('integration scenarios', () => {
    it('should handle GitHub workflow', () => {
      const url = 'https://github.com/user/repo.git';
      expect(gitHost.detect(url)).toBe('github');
      expect(gitHost.isGitHub(url)).toBe(true);
      expect(gitHost.getDisplayName(url)).toBe('repo');
      expect(gitHost.getWebUrl(url)).toBe('https://github.com/user/repo');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBe('https://github.com/user/repo/commit/abc123');
      expect(gitHost.parseGitHub(url)).toEqual({ owner: 'user', repo: 'repo' });
      expect(gitHost.parseRepo(url)).toEqual({ owner: 'user', repo: 'repo' });
      expect(gitHost.extractDomain(url)).toBe('github.com');
    });

    it('should handle GitLab workflow', () => {
      const url = 'https://gitlab.com/user/repo.git';
      expect(gitHost.detect(url)).toBe('gitlab');
      expect(gitHost.isGitHub(url)).toBe(false);
      expect(gitHost.getDisplayName(url)).toBe('repo');
      expect(gitHost.getWebUrl(url)).toBe('https://gitlab.com/user/repo');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBe('https://gitlab.com/user/repo/-/commit/abc123');
      expect(gitHost.parseGitHub(url)).toBeNull();
      expect(gitHost.parseRepo(url)).toEqual({ owner: 'user', repo: 'repo' });
      expect(gitHost.extractDomain(url)).toBe('gitlab.com');
    });

    it('should handle Bitbucket workflow', () => {
      const url = 'https://bitbucket.org/user/repo.git';
      expect(gitHost.detect(url)).toBe('bitbucket');
      expect(gitHost.isGitHub(url)).toBe(false);
      expect(gitHost.getDisplayName(url)).toBe('repo');
      expect(gitHost.getWebUrl(url)).toBe('https://bitbucket.org/user/repo');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBe('https://bitbucket.org/user/repo/commits/abc123');
      expect(gitHost.parseGitHub(url)).toBeNull();
      expect(gitHost.parseRepo(url)).toEqual({ owner: 'user', repo: 'repo' });
      expect(gitHost.extractDomain(url)).toBe('bitbucket.org');
    });

    it('should handle SSH URL workflow', () => {
      const url = 'git@github.com:user/repo.git';
      expect(gitHost.detect(url)).toBe('github');
      expect(gitHost.isGitHub(url)).toBe(true);
      expect(gitHost.getDisplayName(url)).toBe('repo');
      expect(gitHost.getWebUrl(url)).toBe('https://github.com/user/repo');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBe('https://github.com/user/repo/commit/abc123');
      expect(gitHost.parseRepo(url)).toEqual({ owner: 'user', repo: 'repo' });
    });

    it('should handle myrepository special case', () => {
      const url = 'myrepository';
      expect(gitHost.detect(url)).toBe('unknown');
      expect(gitHost.isGitHub(url)).toBe(false);
      expect(gitHost.getDisplayName(url)).toBe('My Repository');
      expect(gitHost.getWebUrl(url)).toBe('myrepository');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBeNull();
      expect(gitHost.parseGitHub(url)).toBeNull();
      expect(gitHost.parseRepo(url)).toBeNull();
    });

    it('should handle self-hosted instance', () => {
      const url = 'https://git.company.com/team/project.git';
      expect(gitHost.detect(url)).toBe('unknown');
      expect(gitHost.isGitHub(url)).toBe(false);
      expect(gitHost.getDisplayName(url)).toBe('project');
      expect(gitHost.getWebUrl(url)).toBe('https://git.company.com/team/project');
      expect(gitHost.getCommitUrl(url, 'abc123')).toBe('https://git.company.com/team/project/commit/abc123');
      expect(gitHost.parseGitHub(url)).toBeNull();
      expect(gitHost.parseRepo(url)).toEqual({ owner: 'team', repo: 'project' });
      expect(gitHost.extractDomain(url)).toBe('git.company.com');
    });
  });

  describe('detectAsync()', () => {
    it('should detect known providers immediately', async () => {
      const result = await gitHost.detectAsync('https://github.com/user/repo.git');
      expect(result).toBe('github');
    });

    it('should detect GitLab from known domain', async () => {
      const result = await gitHost.detectAsync('https://gitlab.com/user/repo.git');
      expect(result).toBe('gitlab');
    });

    it('should detect Bitbucket from known domain', async () => {
      const result = await gitHost.detectAsync('https://bitbucket.org/user/repo.git');
      expect(result).toBe('bitbucket');
    });

    it('should return unknown for empty string', async () => {
      const result = await gitHost.detectAsync('');
      expect(result).toBe('unknown');
    });

    it('should return unknown for invalid URL', async () => {
      const result = await gitHost.detectAsync('not-a-valid-url');
      expect(result).toBe('unknown');
    });

    it('should return unknown for URL without owner/repo', async () => {
      const result = await gitHost.detectAsync('https://example.com');
      expect(result).toBe('unknown');
    });

    it('should return unknown when extractDomain returns null in async', async () => {
      const result = await gitHost.detectAsync(':::invalid:::');
      expect(result).toBe('unknown');
    });

    it('should fallback to unknown for unreachable self-hosted instance', async () => {
      const result = await gitHost.detectAsync('https://git.invalid-domain-12345.com/user/repo.git');
      expect(result).toBe('unknown');
    });

    it('should use cache for repeated calls', async () => {
      const url = 'https://unknown-service-12345.com/user/repo.git';
      const result1 = await gitHost.detectAsync(url);
      const result2 = await gitHost.detectAsync(url);
      expect(result1).toBe(result2);
    });
  });

  describe('error handling and edge cases', () => {
    it('should handle getDisplayName with malformed URL gracefully', () => {
      const result = gitHost.getDisplayName('https://');
      expect(result).toBe('https://');
    });

    it('should handle getDisplayName with URL that throws during parsing', () => {
      const result = gitHost.getDisplayName('ht!tp://invalid');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with single path component', () => {
      const result = gitHost.getDisplayName('https://github.com/single-component');
      expect(result).toBe('single-component');
    });

    it('should handle getDisplayName with two path components where second is empty', () => {
      const result = gitHost.getDisplayName('https://github.com/owner/');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with empty path components', () => {
      const result = gitHost.getDisplayName('https://github.com//');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with URL starting with hash', () => {
      const result = gitHost.getDisplayName('#something');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with only hash', () => {
      const result = gitHost.getDisplayName('#');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with URL ending in hash', () => {
      const result = gitHost.getDisplayName('https://github.com/owner/#');
      expect(typeof result).toBe('string');
    });

    it('should handle getDisplayName with hash but empty first part', () => {
      const result = gitHost.getDisplayName('#branch:main');
      expect(typeof result).toBe('string');
    });

    it('should handle extractDomain with various edge cases', () => {
      expect(gitHost.extractDomain('')).toBeNull();
      expect(gitHost.extractDomain('github.com/user/repo')).toBe('github.com');
      expect(gitHost.extractDomain('https://github.com/user/repo')).toBe('github.com');
    });

    it('should handle parseRepo with single path component', () => {
      const result = gitHost.parseRepo('https://github.com/single');
      expect(result).toBeNull();
    });

    it('should handle parseRepo with empty path', () => {
      const result = gitHost.parseRepo('https://github.com');
      expect(result).toBeNull();
    });

    it('should handle getCommitUrl with various invalid inputs', () => {
      expect(gitHost.getCommitUrl('', 'abc')).toBeNull();
      expect(gitHost.getCommitUrl('url', '')).toBeNull();
      expect(gitHost.getCommitUrl('', '')).toBeNull();
    });

    it('should handle getCommitUrl with whitespace-only URL', () => {
      const result = gitHost.getCommitUrl('   ', 'abc123');
      expect(result).toBeNull();
    });

    it('should handle isGitHub with various inputs', () => {
      expect(gitHost.isGitHub('')).toBe(false);
      expect(gitHost.isGitHub('invalid')).toBe(false);
      expect(gitHost.isGitHub('https://github.com/user/repo')).toBe(true);
    });
  });

  describe('API probing and cache', () => {
    beforeEach(() => {
      vi.restoreAllMocks();
    });

    it('should detect GitHub via API probe success', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: (key: string) => key === 'content-type' ? 'application/json' : null }
      });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-github-test.example.com/user/repo.git');
      expect(result).toBe('github');
      vi.unstubAllGlobals();
    });

    it('should detect GitHub via alternative API probe', async () => {
      const mockFetch = vi.fn()
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: { get: (key: string) => key === 'content-type' ? 'application/json' : null }
        });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-github-alt-test.example.com/user/repo.git');
      expect(result).toBe('github');
      vi.unstubAllGlobals();
    });

    it('should detect GitLab via API probe success', async () => {
      const mockFetch = vi.fn()
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: { get: (key: string) => key === 'content-type' ? 'application/json' : null }
        });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-gitlab-test.example.com/user/repo.git');
      expect(result).toBe('gitlab');
      vi.unstubAllGlobals();
    });

    it('should detect Bitbucket via API probe success', async () => {
      const mockFetch = vi.fn()
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: false,
          status: 404,
          headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: { get: (key: string) => key === 'content-type' ? 'application/json' : null }
        });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-bitbucket-test.example.com/user/repo.git');
      expect(result).toBe('bitbucket');
      vi.unstubAllGlobals();
    });

    it('should handle null content-type header', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null }
      });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-nullct-test.example.com/user/repo.git');
      expect(result).toBe('unknown');
      vi.unstubAllGlobals();
    });

    it('should handle content-type without application/json', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: (key: string) => key === 'content-type' ? 'text/html' : null }
      });
      vi.stubGlobal('fetch', mockFetch);
      const result = await gitHost.detectAsync('https://git-texthtml-test.example.com/user/repo.git');
      expect(result).toBe('unknown');
      vi.unstubAllGlobals();
    });

    it('should use cache for sync detect() when available and not expired', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: (key: string) => key === 'content-type' ? 'application/json' : null }
      });
      vi.stubGlobal('fetch', mockFetch);
      const url = 'https://git-cached-example.com/user/repo.git';
      await gitHost.detectAsync(url);
      const result = gitHost.detect(url);
      expect(result).toBe('github');
      vi.unstubAllGlobals();
    });
  });
});
