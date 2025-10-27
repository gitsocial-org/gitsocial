/**
 * Git Hosting Layer - Platform-specific URL operations
 * Handles different Git hosting services (GitHub, GitLab, Bitbucket, etc.)
 */

import { gitMsgUrl } from '../gitmsg/protocol';
import { log } from '../logger';

/**
 * Git hosting service type
 */
export type HostingService = 'github' | 'gitlab' | 'bitbucket' | 'unknown';

/**
 * Repository info (owner/repo)
 */
export interface RepoInfo {
  owner: string;
  repo: string;
}

/**
 * GitHub repository info (deprecated - use RepoInfo)
 * @deprecated Use RepoInfo instead
 */
export interface GitHubRepo {
  owner: string;
  repo: string;
}

/**
 * Domain provider cache entry
 */
interface DomainCacheEntry {
  provider: HostingService;
  timestamp: number;
}

/**
 * Known public hosting providers (instant lookup)
 */
const KNOWN_PROVIDERS: Record<string, HostingService> = {
  'github.com': 'github',
  'gitlab.com': 'gitlab',
  'bitbucket.org': 'bitbucket',
  'codeberg.org': 'github'
};

/**
 * Domain provider cache (7-day TTL)
 */
const domainProviderCache = new Map<string, DomainCacheEntry>();
const DOMAIN_CACHE_TTL = 7 * 24 * 60 * 60 * 1000;

/**
 * Extract domain from repository URL
 */
export function extractDomain(url: string): string | null {
  try {
    const normalized = gitMsgUrl.normalize(url);
    const urlObj = new URL(normalized.startsWith('http') ? normalized : `https://${normalized}`);
    return urlObj.hostname;
  } catch {
    return null;
  }
}

/**
 * Parse owner/repo from URL
 */
function parseOwnerRepo(url: string): RepoInfo | null {
  try {
    const normalized = gitMsgUrl.normalize(url);
    const urlObj = new URL(normalized.startsWith('http') ? normalized : `https://${normalized}`);
    const pathParts = urlObj.pathname.split('/').filter(Boolean);
    if (pathParts.length >= 2 && pathParts[0] && pathParts[1]) {
      return {
        owner: pathParts[0],
        repo: pathParts[1]
      };
    }
  } catch {
    // URL parsing failed
  }
  return null;
}

/**
 * Try GitHub API (GitHub, Gitea, Codeberg)
 */
async function tryGitHubAPI(domain: string, owner: string, repo: string): Promise<boolean> {
  try {
    const apiUrl = `https://${domain}/api/v1/repos/${owner}/${repo}`;
    log('debug', `[GitHost] Probing GitHub/Gitea API: ${apiUrl}`);
    const response = await fetch(apiUrl, {
      method: 'HEAD',
      signal: AbortSignal.timeout(5000)
    });
    const contentType = response.headers.get('content-type');
    log('debug', `[GitHost] GitHub/Gitea API probe: status=${response.status}, content-type=${contentType}`);
    if (response.ok && contentType?.includes('application/json')) {
      log('debug', `[GitHost] Detected GitHub/Gitea API at ${domain}`);
      return true;
    }
    const apiUrlGitHub = `https://${domain}/repos/${owner}/${repo}`;
    log('debug', `[GitHost] Probing GitHub API (alt): ${apiUrlGitHub}`);
    const responseGitHub = await fetch(apiUrlGitHub, {
      method: 'HEAD',
      signal: AbortSignal.timeout(5000)
    });
    const contentTypeGitHub = responseGitHub.headers.get('content-type');
    log('debug', `[GitHost] GitHub API (alt) probe: status=${responseGitHub.status}, content-type=${contentTypeGitHub}`);
    if (responseGitHub.ok && contentTypeGitHub?.includes('application/json')) {
      log('debug', `[GitHost] Detected GitHub API at ${domain}`);
      return true;
    }
  } catch (error) {
    log('debug', '[GitHost] GitHub/Gitea API probe failed:', error);
  }
  return false;
}

/**
 * Try GitLab API
 */
async function tryGitLabAPI(domain: string, owner: string, repo: string): Promise<boolean> {
  try {
    const projectPath = encodeURIComponent(`${owner}/${repo}`);
    const apiUrl = `https://${domain}/api/v4/projects/${projectPath}`;
    log('debug', `[GitHost] Probing GitLab API: ${apiUrl}`);
    const response = await fetch(apiUrl, {
      method: 'HEAD',
      signal: AbortSignal.timeout(5000)
    });
    const contentType = response.headers.get('content-type');
    log('debug', `[GitHost] GitLab API probe: status=${response.status}, content-type=${contentType}`);
    if (response.ok && contentType?.includes('application/json')) {
      log('debug', `[GitHost] Detected GitLab API at ${domain}`);
      return true;
    }
  } catch (error) {
    log('debug', '[GitHost] GitLab API probe failed:', error);
  }
  return false;
}

/**
 * Try Bitbucket API
 */
async function tryBitbucketAPI(domain: string, owner: string, repo: string): Promise<boolean> {
  try {
    const apiUrl = `https://${domain}/2.0/repositories/${owner}/${repo}`;
    log('debug', `[GitHost] Probing Bitbucket API: ${apiUrl}`);
    const response = await fetch(apiUrl, {
      method: 'HEAD',
      signal: AbortSignal.timeout(5000)
    });
    const contentType = response.headers.get('content-type');
    log('debug', `[GitHost] Bitbucket API probe: status=${response.status}, content-type=${contentType}`);
    if (response.ok && contentType?.includes('application/json')) {
      log('debug', `[GitHost] Detected Bitbucket API at ${domain}`);
      return true;
    }
  } catch (error) {
    log('debug', '[GitHost] Bitbucket API probe failed:', error);
  }
  return false;
}

/**
 * Probe domain to detect hosting provider
 */
async function probeProvider(domain: string, owner: string, repo: string): Promise<HostingService> {
  if (await tryGitHubAPI(domain, owner, repo)) {
    return 'github';
  }
  if (await tryGitLabAPI(domain, owner, repo)) {
    return 'gitlab';
  }
  if (await tryBitbucketAPI(domain, owner, repo)) {
    return 'bitbucket';
  }
  log('debug', `[GitHost] Could not detect provider for ${domain}, using unknown`);
  return 'unknown';
}

/**
 * GitHost namespace - handles Git hosting service-specific operations
 */
export const gitHost = {
  /**
   * Detect hosting service from URL (sync - known domains + cache only)
   * For unknown domains, use detectAsync() to probe APIs
   */
  detect(url: string): HostingService {
    if (!url) {return 'unknown';}

    const domain = extractDomain(url);
    if (!domain) {return 'unknown';}

    const known = KNOWN_PROVIDERS[domain];
    if (known) {
      return known;
    }

    const cached = domainProviderCache.get(domain);
    if (cached && Date.now() - cached.timestamp < DOMAIN_CACHE_TTL) {
      return cached.provider;
    }

    return 'unknown';
  },

  /**
   * Detect hosting service from URL (async - includes API probing)
   * Uses known domains → cache → API probing for detection
   */
  async detectAsync(url: string): Promise<HostingService> {
    if (!url) {return 'unknown';}

    const domain = extractDomain(url);
    if (!domain) {return 'unknown';}

    const known = KNOWN_PROVIDERS[domain];
    if (known) {
      return known;
    }

    const cached = domainProviderCache.get(domain);
    if (cached && Date.now() - cached.timestamp < DOMAIN_CACHE_TTL) {
      return cached.provider;
    }

    const repoInfo = parseOwnerRepo(url);
    if (!repoInfo) {return 'unknown';}

    const provider = await probeProvider(domain, repoInfo.owner, repoInfo.repo);
    domainProviderCache.set(domain, {
      provider,
      timestamp: Date.now()
    });

    return provider;
  },

  /**
   * Get display name for a repository (owner/repo format)
   */
  getDisplayName(url: string): string {
    if (!url || url === 'myrepository') {
      return 'My Repository';
    }

    try {
      // Handle repository#branch format
      let cleanUrl = url;
      if (url.includes('#')) {
        const parts = url.split('#');
        cleanUrl = parts[0] || url;
      }

      const normalized = gitMsgUrl.normalize(cleanUrl);

      // Extract the repository name from the URL path
      const urlObj = new URL(normalized.startsWith('http') ? normalized : `https://${normalized}`);
      const pathParts = urlObj.pathname.split('/').filter(Boolean);

      if (pathParts.length >= 2) {
        // Return just repo name (not owner/repo format)
        return pathParts[1] || pathParts[0] || cleanUrl;
      }

      // Fallback to just the repo name
      const repoName = pathParts[pathParts.length - 1] || cleanUrl;
      return repoName.replace(/\.git$/, '');
    } catch {
      // Fallback to the original URL if parsing fails
      return url;
    }
  },

  /**
   * Get web-viewable URL (no .git suffix)
   */
  getWebUrl(url: string): string {
    if (!url || url === 'myrepository') {return url;}

    const normalized = gitMsgUrl.normalize(url);
    return normalized; // gitMsgUrl.normalize already removes .git
  },

  /**
   * Get commit URL for hosting service
   */
  getCommitUrl(url: string, hash: string): string | null {
    if (!url || !hash) {return null;}

    if (url === 'myrepository' || url.startsWith('/') || url.startsWith('.')) {
      return null;
    }

    const baseUrl = this.getWebUrl(url);
    if (!baseUrl) {return null;}

    const service = this.detect(url);

    switch (service) {
    case 'gitlab':
      return `${baseUrl}/-/commit/${hash}`;
    case 'bitbucket':
      return `${baseUrl}/commits/${hash}`;
    case 'github':
    case 'unknown':
    default:
      return `${baseUrl}/commit/${hash}`;
    }
  },

  /**
   * Parse GitHub repository owner and name from URL
   */
  parseGitHub(url: string): GitHubRepo | null {
    const service = this.detect(url);
    if (service !== 'github') {
      return null;
    }

    return parseOwnerRepo(url);
  },

  /**
   * Parse repository owner and name from URL (works for all providers)
   */
  parseRepo(url: string): RepoInfo | null {
    return parseOwnerRepo(url);
  },

  /**
   * Check if URL is a GitHub URL
   */
  isGitHub(url: string): boolean {
    return this.detect(url) === 'github';
  },

  /**
   * Extract domain from repository URL
   */
  extractDomain(url: string): string | null {
    return extractDomain(url);
  }
};
