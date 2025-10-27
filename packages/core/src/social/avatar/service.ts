import { type AvatarCacheStats, type ClearAvatarCacheOptions, type ClearResult, DEFAULT_AVATAR_CONFIG } from './types';
import { avatarUtils } from './utils';
import { gitHost, type HostingService } from '../../githost';
import { log } from '../../logger';
import * as path from 'path';
import * as fs from 'fs';

let lastApiCall = 0;
const GRAVATAR_CACHE_DURATION = 60 * 60 * 1000; // 1 hour
let githubToken: string | null = null;
let firstAuthenticatedRequest = true;
let enableGravatar = false;

export function setGitHubToken(token: string | null): void {
  githubToken = token;
  firstAuthenticatedRequest = true;
}

export function setEnableGravatar(enabled: boolean): void {
  enableGravatar = enabled;
  log('info', `[Avatar] Gravatar ${enabled ? 'enabled' : 'disabled'}`);
}

async function throttleApiCall(): Promise<void> {
  const now = Date.now();
  const timeSinceLastCall = now - lastApiCall;
  if (timeSinceLastCall < DEFAULT_AVATAR_CONFIG.apiRateLimit) {
    await new Promise(resolve =>
      setTimeout(resolve, DEFAULT_AVATAR_CONFIG.apiRateLimit - timeSinceLastCall)
    );
  }
  lastApiCall = Date.now();
}

async function fetchWithTimeout(url: string): Promise<Response> {
  await throttleApiCall();

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), DEFAULT_AVATAR_CONFIG.apiTimeout);

  try {
    const headers: Record<string, string> = {
      'User-Agent': DEFAULT_AVATAR_CONFIG.userAgent
    };

    if (githubToken) {
      headers['Authorization'] = `Bearer ${githubToken}`;
      if (firstAuthenticatedRequest) {
        log('info', '[Avatar] First authenticated GitHub request - rate limit: 5,000 req/hr');
        firstAuthenticatedRequest = false;
      }
    }

    const response = await fetch(url, {
      headers,
      signal: controller.signal
    });
    clearTimeout(timeout);
    return response;
  } catch (error) {
    clearTimeout(timeout);
    throw error;
  }
}

function getGitHubUserAvatarUrl(email: string): string | null {
  const match = email.match(/(\d+)\+(.+)@users\.noreply\.github\.com/);
  if (match) {
    // Return base URL without size - we'll add it when downloading
    return `https://avatars.githubusercontent.com/u/${match[1]}?v=4`;
  }
  return null;
}

async function resolveGitHubUserAvatar(email: string, remoteUrl: string): Promise<string | null> {
  try {
    const repo = gitHost.parseGitHub(remoteUrl);
    if (!repo) {return null;}

    const apiUrl = `https://api.github.com/repos/${repo.owner}/${repo.repo}/commits?author=${email}&per_page=1`;
    log('debug', `[Avatar] GitHub API request: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);

    log('debug', `[Avatar] GitHub API response status: ${response.status}`);
    if (response.ok) {
      const commits = await response.json() as Array<{ author?: { avatar_url?: string } }>;
      log('debug', `[Avatar] GitHub API commits count: ${commits.length}, first commit:`, commits[0]);
      return commits[0]?.author?.avatar_url || null;
    }
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch user avatar from GitHub API for ${email}:`, error);
  }
  return null;
}

async function resolveGitLabUserAvatar(email: string, remoteUrl: string): Promise<string | null> {
  try {
    const domain = gitHost.extractDomain(remoteUrl);
    if (!domain) {return null;}

    const apiUrl = `https://${domain}/api/v4/avatar?email=${encodeURIComponent(email)}`;
    log('debug', `[Avatar] GitLab API request: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);

    log('debug', `[Avatar] GitLab API response status: ${response.status}`);
    if (response.ok) {
      const data = await response.json() as { avatar_url?: string };
      log('debug', `[Avatar] GitLab API avatar URL: ${data.avatar_url || 'null'}`);
      return data.avatar_url || null;
    }
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch user avatar from GitLab API for ${email}:`, error);
  }
  return null;
}

async function resolveGiteaUserAvatar(email: string, remoteUrl: string): Promise<string | null> {
  try {
    const repo = gitHost.parseRepo(remoteUrl);
    const domain = gitHost.extractDomain(remoteUrl);
    if (!repo || !domain) {return null;}

    const apiUrl = `https://${domain}/api/v1/repos/${repo.owner}/${repo.repo}/commits?author=${encodeURIComponent(email)}&limit=1`;
    log('debug', `[Avatar] Gitea API request: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);

    log('debug', `[Avatar] Gitea API response status: ${response.status}`);
    if (response.ok) {
      const commits = await response.json() as Array<{ author?: { avatar_url?: string } }>;
      log('debug', `[Avatar] Gitea API commits count: ${commits.length}, first commit:`, commits[0]);
      return commits[0]?.author?.avatar_url || null;
    }
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch user avatar from Gitea API for ${email}:`, error);
  }
  return null;
}

async function safeJsonParse<T = unknown>(response: Response): Promise<T | null> {
  try {
    const contentType = response.headers.get('content-type');
    if (!contentType?.includes('application/json')) {
      return null;
    }
    return await response.json() as T;
  } catch {
    return null;
  }
}

async function resolveGitHubRepositoryAvatar(domain: string, owner: string, repo: string): Promise<string | null> {
  try {
    let apiUrl: string;
    if (domain === 'github.com') {
      apiUrl = `https://api.github.com/repos/${owner}/${repo}`;
    } else {
      apiUrl = `https://${domain}/api/v1/repos/${owner}/${repo}`;
    }
    log('debug', `[Avatar] Trying GitHub/Gitea API: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);
    log('debug', `[Avatar] GitHub/Gitea API response: status=${response.status}, content-type=${response.headers.get('content-type')}`);
    if (!response.ok) {
      log('debug', `[Avatar] GitHub/Gitea API returned non-OK status: ${response.status}`);
      return null;
    }
    const data = await safeJsonParse<{
      avatar_url?: string;
      owner?: { avatar_url?: string };
      organization?: { avatar_url?: string };
    }>(response);
    if (!data) {
      log('debug', '[Avatar] GitHub/Gitea API returned non-JSON response');
      return null;
    }
    const avatarUrl = data.avatar_url || data.organization?.avatar_url || data.owner?.avatar_url || null;
    log('debug', `[Avatar] GitHub/Gitea API avatar URL: ${avatarUrl || 'null'}`);
    return avatarUrl;
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch repository avatar from GitHub/Gitea API at ${domain}:`, error);
  }
  return null;
}

async function resolveGitLabRepositoryAvatar(domain: string, owner: string, repo: string): Promise<string | null> {
  try {
    const projectPath = encodeURIComponent(`${owner}/${repo}`);
    const apiUrl = `https://${domain}/api/v4/projects/${projectPath}`;
    log('debug', `[Avatar] Trying GitLab API: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);
    log('debug', `[Avatar] GitLab API response: status=${response.status}, content-type=${response.headers.get('content-type')}`);
    if (!response.ok) {
      log('debug', `[Avatar] GitLab API returned non-OK status: ${response.status}`);
      return null;
    }
    const data = await safeJsonParse<{ avatar_url?: string; namespace?: { avatar_url?: string } }>(response);
    if (!data) {
      log('debug', '[Avatar] GitLab API returned non-JSON response');
      return null;
    }
    if (data.avatar_url) {
      let avatarUrl = data.avatar_url;
      if (avatarUrl.startsWith('/')) {
        avatarUrl = `https://${domain}${avatarUrl}`;
      }
      log('debug', `[Avatar] GitLab API avatar URL: ${avatarUrl}`);
      return avatarUrl;
    }
    log('debug', '[Avatar] GitLab API returned no avatar_url');
    return null;
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch repository avatar from GitLab API at ${domain}:`, error);
  }
  return null;
}

async function resolveBitbucketRepositoryAvatar(domain: string, owner: string, repo: string): Promise<string | null> {
  try {
    const apiUrl = `https://${domain}/2.0/repositories/${owner}/${repo}`;
    log('debug', `[Avatar] Trying Bitbucket API: ${apiUrl}`);
    const response = await fetchWithTimeout(apiUrl);
    log('debug', `[Avatar] Bitbucket API response: status=${response.status}, content-type=${response.headers.get('content-type')}`);
    if (!response.ok) {
      log('debug', `[Avatar] Bitbucket API returned non-OK status: ${response.status}`);
      return null;
    }
    const data = await safeJsonParse<{ links?: { avatar?: { href?: string } } }>(response);
    if (!data) {
      log('debug', '[Avatar] Bitbucket API returned non-JSON response');
      return null;
    }
    const avatarUrl = data.links?.avatar?.href || null;
    log('debug', `[Avatar] Bitbucket API avatar URL: ${avatarUrl || 'null'}`);
    return avatarUrl;
  } catch (error) {
    log('debug', `[Avatar] Failed to fetch repository avatar from Bitbucket API at ${domain}:`, error);
  }
  return null;
}

async function resolveRepositoryAvatar(
  repoUrl: string
): Promise<{ avatarUrl: string | null; provider: HostingService }> {
  log('debug', `[Avatar] Resolving repository avatar for: ${repoUrl}`);
  const provider = await gitHost.detectAsync(repoUrl);
  const repoInfo = gitHost.parseRepo(repoUrl);
  const domain = gitHost.extractDomain(repoUrl);
  log('debug', `[Avatar] Detected provider: ${provider}, domain: ${domain}, owner/repo: ${repoInfo?.owner}/${repoInfo?.repo}`);
  if (!repoInfo || !domain) {
    log('debug', '[Avatar] Failed to parse repository info or domain');
    return { avatarUrl: null, provider };
  }
  let avatarUrl: string | null = null;
  switch (provider) {
  case 'github':
    log('debug', '[Avatar] Using GitHub/Gitea API resolver');
    avatarUrl = await resolveGitHubRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    break;
  case 'gitlab':
    log('debug', '[Avatar] Using GitLab API resolver');
    avatarUrl = await resolveGitLabRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    break;
  case 'bitbucket':
    log('debug', '[Avatar] Using Bitbucket API resolver');
    avatarUrl = await resolveBitbucketRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    break;
  case 'unknown':
  default: {
    log('debug', '[Avatar] Provider unknown, trying all APIs in sequence');
    avatarUrl = await resolveGitHubRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    if (avatarUrl) {
      log('debug', '[Avatar] Found avatar via GitHub API');
      return { avatarUrl, provider: 'github' };
    }
    avatarUrl = await resolveGitLabRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    if (avatarUrl) {
      log('debug', '[Avatar] Found avatar via GitLab API');
      return { avatarUrl, provider: 'gitlab' };
    }
    avatarUrl = await resolveBitbucketRepositoryAvatar(domain, repoInfo.owner, repoInfo.repo);
    if (avatarUrl) {
      log('debug', '[Avatar] Found avatar via Bitbucket API');
      return { avatarUrl, provider: 'bitbucket' };
    }
    log('debug', '[Avatar] No avatar found via any API');
    break;
  }
  }
  return { avatarUrl, provider };
}

function getGravatarUrl(email: string): string {
  const hash = avatarUtils.md5Hash(email);
  return `https://www.gravatar.com/avatar/${hash}?s=80&d=identicon`;
}

async function downloadAndSaveAvatar(
  url: string,
  filePath: string,
  temporary: boolean = false
): Promise<string | null> {
  try {
    const response = await fetchWithTimeout(url);

    if (response.ok) {
      const buffer = await response.arrayBuffer();
      if (buffer.byteLength > 0) {
        const imageBuffer = Buffer.from(buffer);

        // If temporary, create a temp file instead
        if (temporary) {
          const avatarDir = path.dirname(filePath);
          const prefix = path.basename(filePath, '.png');
          const tempFilePath = path.join(avatarDir, `temp_${prefix}_${Date.now()}.png`);
          await fs.promises.mkdir(avatarDir, { recursive: true });
          await fs.promises.writeFile(tempFilePath, imageBuffer);

          // Schedule cleanup after 5 minutes
          setTimeout(() => {
            fs.promises.access(tempFilePath)
              .then(() => fs.promises.unlink(tempFilePath))
              .catch(() => {
                // Temp file cleanup failed, ignore
              });
          }, DEFAULT_AVATAR_CONFIG.tempFileCleanup);

          return tempFilePath;
        }

        // Ensure directory exists for permanent file
        await fs.promises.mkdir(path.dirname(filePath), { recursive: true });
        await fs.promises.writeFile(filePath, imageBuffer);
        return filePath;
      }
    }
  } catch (error) {
    log('debug', `[Avatar] Failed to download/save avatar from ${url}:`, error);
  }
  return null;
}

async function createTempFile(
  avatarDir: string,
  content: string,
  prefix: string
): Promise<string> {
  const tempFilePath = path.join(avatarDir, `temp_${prefix}_${Date.now()}.svg`);

  // Ensure directory exists
  await fs.promises.mkdir(avatarDir, { recursive: true });
  await fs.promises.writeFile(tempFilePath, content);

  // Schedule cleanup after 5 minutes
  setTimeout(() => {
    fs.promises.access(tempFilePath)
      .then(() => fs.promises.unlink(tempFilePath))
      .catch(() => {
        // Temp file cleanup failed, ignore
      });
  }, DEFAULT_AVATAR_CONFIG.tempFileCleanup);

  return tempFilePath;
}

// Global avatar cache and promise queue
const avatarCache = new Map<string, { filePath: string; expires: number }>();
const promiseQueue = new Map<string, Promise<string>>();

function getCacheKey(type: 'user' | 'repo', identifier: string): string {
  const hash = avatarUtils.md5Hash(identifier.toLowerCase().trim());
  return `${type}_${hash}`;
}

// LRU cache management
function evictOldestCacheEntry(): void {
  if (avatarCache.size >= DEFAULT_AVATAR_CONFIG.maxMemoryEntries) {
    let oldestKey: string | null = null;
    let oldestTime = Infinity;

    for (const [key, entry] of avatarCache) {
      if (entry.expires < oldestTime) {
        oldestTime = entry.expires;
        oldestKey = key;
      }
    }

    if (oldestKey) {
      avatarCache.delete(oldestKey);
    }
  }
}

export async function getUserAvatar(
  avatarDir: string,
  email: string,
  remoteUrl?: string,
  _size: number = DEFAULT_AVATAR_CONFIG.defaultSizes.posts,
  name?: string
): Promise<string> {
  const cacheKey = getCacheKey('user', email);

  // Check memory cache
  const cached = avatarCache.get(cacheKey);
  if (cached) {
    // Verify file still exists (might be deleted temp file)
    try {
      await fs.promises.access(cached.filePath);
      if (cached.expires > Date.now()) {
        return cached.filePath;
      }
    } catch {
      // File doesn't exist, remove from cache and continue
      avatarCache.delete(cacheKey);
    }
  }

  // Check promise queue to avoid duplicate requests
  if (promiseQueue.has(cacheKey)) {
    return promiseQueue.get(cacheKey)!;
  }

  const promise = (async () => {
    const filePath = path.join(avatarDir, `user_${avatarUtils.md5Hash(email)}.png`);
    const gravatarPath = path.join(avatarDir, `gravatar_${avatarUtils.md5Hash(email)}.png`);

    // Check if GitHub avatar file already exists
    try {
      await fs.promises.access(filePath);
      evictOldestCacheEntry();
      avatarCache.set(cacheKey, {
        filePath,
        expires: Date.now() + DEFAULT_AVATAR_CONFIG.cacheExpiration
      });
      return filePath;
    } catch (error) {
      // GitHub avatar doesn't exist, try fetching it
    }

    let avatarPath: string | null = null;

    // Try provider-specific API for better results
    log('debug', `[Avatar] Fetching user avatar for ${email}, remoteUrl: ${remoteUrl}`);
    if (remoteUrl) {
      const provider = gitHost.detect(remoteUrl);
      log('debug', `[Avatar] Detected provider: ${provider}`);

      if (provider === 'github') {
        const domain = gitHost.extractDomain(remoteUrl);
        const isGitHub = domain === 'github.com';
        const isGitea = !isGitHub && domain !== null;

        log('debug', `[Avatar] Attempting ${isGitea ? 'Gitea' : 'GitHub'} avatar fetch for ${email}`);

        let avatarUrl: string | null = null;

        if (isGitHub) {
          // Try GitHub noreply email pattern first
          avatarUrl = getGitHubUserAvatarUrl(email);
          log('debug', `[Avatar] noreply pattern result: ${avatarUrl || 'null'}`);

          // Fallback to GitHub commit history API
          if (!avatarUrl) {
            avatarUrl = await resolveGitHubUserAvatar(email, remoteUrl);
            log('debug', `[Avatar] GitHub API result: ${avatarUrl || 'null'}`);
          }
        } else if (isGitea) {
          // Use Gitea commit history API
          avatarUrl = await resolveGiteaUserAvatar(email, remoteUrl);
          log('debug', `[Avatar] Gitea API result: ${avatarUrl || 'null'}`);
        }

        if (avatarUrl) {
          // Always download at 80px for optimal quality
          try {
            const url = new URL(avatarUrl);
            url.searchParams.set('s', '80');
            const highResUrl = url.toString();
            avatarPath = await downloadAndSaveAvatar(highResUrl, filePath, false);
            log('debug', `[Avatar] ${isGitea ? 'Gitea' : 'GitHub'} avatar saved: ${avatarPath || 'failed'}`);
          } catch {
            // Fallback to original URL if parsing fails
            avatarPath = await downloadAndSaveAvatar(avatarUrl, filePath, false);
            log('debug', `[Avatar] ${isGitea ? 'Gitea' : 'GitHub'} avatar saved (fallback): ${avatarPath || 'failed'}`);
          }
        }
      } else if (provider === 'gitlab') {
        log('debug', `[Avatar] Attempting GitLab avatar fetch for ${email}`);
        const gitlabAvatarUrl = await resolveGitLabUserAvatar(email, remoteUrl);
        log('debug', `[Avatar] GitLab API result: ${gitlabAvatarUrl || 'null'}`);

        if (gitlabAvatarUrl) {
          avatarPath = await downloadAndSaveAvatar(gitlabAvatarUrl, filePath, false);
          log('debug', `[Avatar] GitLab avatar saved: ${avatarPath || 'failed'}`);
        }
      } else {
        log('debug', `[Avatar] Skipping provider-specific avatar lookup (provider: ${provider})`);
      }
    } else {
      log('debug', '[Avatar] No remoteUrl provided, skipping provider-specific avatar lookup');
    }

    // If GitHub fetch failed and Gravatar is enabled, check for cached Gravatar
    if (!avatarPath && enableGravatar) {
      try {
        await fs.promises.access(gravatarPath);
        evictOldestCacheEntry();
        avatarCache.set(cacheKey, {
          filePath: gravatarPath,
          expires: Date.now() + GRAVATAR_CACHE_DURATION
        });
        return gravatarPath;
      } catch (error) {
        // No cached Gravatar, fetch new one
      }
    }

    // Try Gravatar if enabled
    if (!avatarPath && enableGravatar) {
      const gravatarUrl = getGravatarUrl(email);
      avatarPath = await downloadAndSaveAvatar(gravatarUrl, gravatarPath, false);
    }

    // Create default avatar SVG (keep as SVG since it's generated)
    if (!avatarPath) {
      const defaultSvg = avatarUtils.createLetterAvatarSvg(email, 80, name);
      avatarPath = await createTempFile(avatarDir, defaultSvg, `user_${avatarUtils.md5Hash(email)}`);
    }

    evictOldestCacheEntry();
    avatarCache.set(cacheKey, {
      filePath: avatarPath,
      expires: Date.now() + DEFAULT_AVATAR_CONFIG.cacheExpiration
    });
    return avatarPath;
  })();

  promiseQueue.set(cacheKey, promise);

  try {
    const result = await promise;
    return result;
  } catch (error) {
    // Return expired cache as fallback
    const fallback = avatarCache.get(cacheKey);
    if (fallback) {return fallback.filePath;}
    throw error;
  } finally {
    promiseQueue.delete(cacheKey);
  }
}

export async function getRepositoryAvatar(
  avatarDir: string,
  repoUrl: string,
  _size: number = DEFAULT_AVATAR_CONFIG.defaultSizes.repository
): Promise<string> {
  const cacheKey = getCacheKey('repo', repoUrl);

  // Check memory cache
  const cached = avatarCache.get(cacheKey);
  if (cached) {
    // Verify file still exists (might be deleted temp file)
    try {
      await fs.promises.access(cached.filePath);
      if (cached.expires > Date.now()) {
        return cached.filePath;
      }
    } catch {
      // File doesn't exist, remove from cache and continue
      avatarCache.delete(cacheKey);
    }
  }

  // Check promise queue
  if (promiseQueue.has(cacheKey)) {
    return promiseQueue.get(cacheKey)!;
  }

  const promise = (async () => {
    const filePath = path.join(avatarDir, `repo_${avatarUtils.md5Hash(repoUrl)}.png`);

    // Check if file already exists
    try {
      await fs.promises.access(filePath);
      evictOldestCacheEntry();
      avatarCache.set(cacheKey, {
        filePath,
        expires: Date.now() + DEFAULT_AVATAR_CONFIG.cacheExpiration
      });
      return filePath;
    } catch (error) {
      // Operation failed, continue with fallback
    }

    let avatarPath: string | null = null;

    // Special case for my repository
    if (repoUrl === 'myrepository') {
      log('debug', '[Avatar] Using home icon for myrepository');
      const homeSvg = avatarUtils.createHomeIconAvatarSvg(80);
      avatarPath = await createTempFile(avatarDir, homeSvg, 'repo_myrepository');
    } else {
      // Try provider API (GitHub, GitLab, Gitea, Bitbucket)
      const { avatarUrl: repositoryAvatarUrl, provider } = await resolveRepositoryAvatar(repoUrl);
      if (repositoryAvatarUrl) {
        log('debug', `[Avatar] Downloading avatar from: ${repositoryAvatarUrl} (provider: ${provider})`);
        // Try to add size parameter for providers that support it (GitHub, Gitea)
        try {
          const url = new URL(repositoryAvatarUrl);
          if (provider === 'github') {
            url.searchParams.set('s', '80');
          }
          avatarPath = await downloadAndSaveAvatar(url.toString(), filePath, false);
          if (avatarPath) {
            log('debug', `[Avatar] Successfully downloaded and saved avatar to: ${avatarPath}`);
          } else {
            log('debug', '[Avatar] Failed to download avatar');
          }
        } catch {
          // Fallback to original URL if parsing fails
          avatarPath = await downloadAndSaveAvatar(repositoryAvatarUrl, filePath, false);
          if (avatarPath) {
            log('debug', `[Avatar] Successfully downloaded and saved avatar (fallback) to: ${avatarPath}`);
          } else {
            log('debug', '[Avatar] Failed to download avatar (fallback)');
          }
        }
      } else {
        log('debug', '[Avatar] No avatar URL resolved from APIs');
      }
    }

    // Create default avatar SVG (keep as SVG since it's generated)
    if (!avatarPath) {
      log('debug', '[Avatar] Falling back to generated letter avatar');
      const defaultSvg = avatarUtils.createLetterAvatarSvg(repoUrl, 80);
      avatarPath = await createTempFile(avatarDir, defaultSvg, `repo_${avatarUtils.md5Hash(repoUrl)}`);
    }

    evictOldestCacheEntry();
    avatarCache.set(cacheKey, {
      filePath: avatarPath,
      expires: Date.now() + DEFAULT_AVATAR_CONFIG.cacheExpiration
    });
    return avatarPath;
  })();

  promiseQueue.set(cacheKey, promise);

  try {
    const result = await promise;
    return result;
  } catch (error) {
    // Return expired cache as fallback
    const fallback = avatarCache.get(cacheKey);
    if (fallback) {return fallback.filePath;}
    throw error;
  } finally {
    promiseQueue.delete(cacheKey);
  }
}

export async function getAvatarCacheStats(avatarDir: string): Promise<AvatarCacheStats> {
  const now = Date.now();
  const memoryCacheEntries = Array.from(avatarCache.entries()).map(([key, entry]) => ({
    key,
    expires: entry.expires,
    isExpired: entry.expires <= now
  }));

  let fileStats;
  try {
    const files = await fs.promises.readdir(avatarDir);
    const imageFiles = files.filter(f => f.endsWith('.svg') || f.endsWith('.png'));

    let totalSize = 0;
    let tempFileCount = 0;
    let permanentFileCount = 0;
    const fileDetails: Array<{ name: string; size: number; isTemp: boolean }> = [];

    for (const file of imageFiles) {
      const filePath = path.join(avatarDir, file);
      const stats = await fs.promises.stat(filePath);
      const isTemp = file.startsWith('temp_');

      totalSize += stats.size;
      if (isTemp) {
        tempFileCount++;
      } else {
        permanentFileCount++;
      }

      fileDetails.push({
        name: file,
        size: stats.size,
        isTemp
      });
    }

    fileStats = {
      totalFiles: imageFiles.length,
      diskUsage: totalSize,
      tempFiles: tempFileCount,
      permanentFiles: permanentFileCount,
      directoryPath: avatarDir,
      fileDetails
    };
  } catch (error) {
    console.error('Failed to get avatar file stats:', error);
  }

  return {
    memoryCacheSize: avatarCache.size,
    memoryCacheEntries,
    fileStats
  };
}

export async function clearAvatarCache(
  avatarDir: string,
  options: ClearAvatarCacheOptions = { clearMemoryCache: true }
): Promise<ClearResult> {
  const errors: string[] = [];
  let clearedMemoryEntries = 0;
  let filesDeleted = 0;

  try {
    // Clear memory cache if requested (default behavior)
    if (options.clearMemoryCache !== false) {
      const initialSize = avatarCache.size;
      avatarCache.clear();
      promiseQueue.clear();
      clearedMemoryEntries = initialSize;
    }

    // Clear files if requested
    if (options.clearAllFiles || options.clearTempFiles) {
      try {
        const files = await fs.promises.readdir(avatarDir);
        const imageFiles = files.filter(f => f.endsWith('.svg') || f.endsWith('.png'));

        for (const file of imageFiles) {
          // Skip non-temp files if only clearing temp files
          if (options.clearTempFiles && !options.clearAllFiles && !file.startsWith('temp_')) {
            continue;
          }

          const filePath = path.join(avatarDir, file);
          try {
            await fs.promises.unlink(filePath);
            filesDeleted++;
          } catch (deleteError) {
            console.error(`Failed to delete avatar file ${file}:`, deleteError);
          }
        }
      } catch (fileError) {
        const errorMsg = fileError instanceof Error ? fileError.message : String(fileError);
        errors.push(`Failed to clear avatar files: ${errorMsg}`);
      }
    }

    return { clearedMemoryEntries, filesDeleted, errors };
  } catch (error) {
    const errorMsg = error instanceof Error ? error.message : String(error);
    errors.push(`Failed to clear avatar cache: ${errorMsg}`);
    return { clearedMemoryEntries: 0, filesDeleted: 0, errors };
  }
}

/**
 * Unified avatar function - handles both user and repository avatars
 */
export async function getAvatar(request: {
  avatarDir: string;
  type: 'user' | 'repo';
  identifier: string;  // email for users, repoUrl for repos
  size?: number;
  context?: string;    // remoteUrl for users
  name?: string;       // display name for users
}): Promise<string> {
  const { avatarDir, type, identifier, size, context, name } = request;

  if (type === 'user') {
    return getUserAvatar(avatarDir, identifier, context, size, name);
  } else {
    return getRepositoryAvatar(avatarDir, identifier, size);
  }
}
