/**
 * List operations for GitSocial
 */

import type { List, Post, Result } from './types';
import { listRefs } from '../git/operations';
import { execGit } from '../git/exec';
import { getRemoteDefaultBranch, listRemotes } from '../git/remotes';
import { log } from '../logger';
import { gitMsgRef, gitMsgUrl } from '../gitmsg/protocol';
import { gitMsgList } from '../gitmsg/lists';
import { storage } from '../storage';
import { cache } from './post/cache';

// Storage configuration for isolated clones
let storageBase: string | undefined;

// In-memory list storage - centralized in lists.ts
const globalLists = new Map<string, List[]>(); // workdir -> List[]
const globalListsByName = new Map<string, List>(); // "workdir:listId" -> List
let listsInitialized = false;

/**
 * List namespace - List management operations
 */
export const list = {
  initialize,
  initializeListStorage,
  getAllListsFromStorage,
  getListFromStorage,
  isPostInList,
  createList,
  deleteList,
  updateList,
  addRepositoryToList,
  removeRepositoryFromList,
  getLists,
  getList,
  getListRepositories,
  syncList,
  getUnpushedListsCount,
  getRemoteLists,
  followList,
  syncFollowedList,
  unfollowList
};

/**
 * Initialize lists module with storage configuration
 */
function initialize(config: { storageBase: string }): void {
  storageBase = config.storageBase;
}

/**
 * Initialize list storage with all lists for a workspace
 */
async function initializeListStorage(workdir: string): Promise<void> {
  try {
    // Load all lists from Git
    const result = await getLists(workdir);

    if (result.success && result.data) {
      // Store lists in memory
      globalLists.set(workdir, result.data);

      // Index lists by ID for quick lookup
      for (const list of result.data) {
        globalListsByName.set(`${workdir}:${list.id}`, list);
      }

      log('debug', '[initializeListStorage] Initialized list storage with', result.data.length, 'lists');
    }

    listsInitialized = true;
  } catch (error) {
    log('error', '[initializeListStorage] Error initializing list storage:', error);
  }
}

/**
 * Get all lists from storage (direct access)
 */
function getAllListsFromStorage(workdir: string): List[] {
  if (!listsInitialized) {return [];}
  return globalLists.get(workdir) || [];
}

/**
 * Get a specific list from storage (direct access)
 */
function getListFromStorage(workdir: string, id: string): List | undefined {
  if (!listsInitialized) {return undefined;}
  return globalListsByName.get(`${workdir}:${id}`);
}

/**
 * Check if a post belongs to a specific list
 */
function isPostInList(post: Post, listId: string, workdir: string): boolean {
  // Get the list from storage
  const list = getListFromStorage(workdir, listId);
  if (!list || !list.repositories) {
    return false;
  }

  // Check if the post's repository is in the list
  const postRepoUrl = gitMsgUrl.normalize(post.repository.split('#')[0] || post.repository);

  return list.repositories.some(listRepoUrl => {
    const normalizedListRepo = gitMsgUrl.normalize(listRepoUrl.split('#')[0] || listRepoUrl);
    return normalizedListRepo === postRepoUrl;
  });
}

/**
 * Extract base repository URL without branch suffix
 * @param repositoryUrl - Full repository URL that may include #branch:name
 * @returns Base repository URL without branch information
 */
function getBaseRepositoryUrl(repositoryUrl: string): string {
  const normalized = gitMsgUrl.normalize(repositoryUrl);
  // Remove #branch:name suffix if present
  const branchIndex = normalized.indexOf('#branch:');
  return branchIndex !== -1 ? normalized.substring(0, branchIndex) : normalized;
}

/**
 * Create a new repository list
 */
async function createList(
  repository: string,
  listId: string,
  name?: string
): Promise<Result<void>> {
  try {
    // Validate list ID
    if (!listId.match(/^[a-zA-Z0-9_-]{1,40}$/)) {
      return {
        success: false,
        error: {
          code: 'INVALID_LIST_NAME',
          message: 'List ID must match pattern [a-zA-Z0-9_-]{1,40}'
        }
      };
    }

    // Check if list already exists
    const existingList = await gitMsgList.read<List>(repository, 'social', listId);
    if (existingList.success && existingList.data) {
      return {
        success: false,
        error: {
          code: 'LIST_EXISTS',
          message: `List '${listId}' already exists`
        }
      };
    }

    // Create initial list data with empty repositories array
    const listData: List = {
      version: '0.1.0',
      id: listId,
      name: name || listId,
      repositories: []
    };

    // Remove runtime-only fields before writing
    const { isUnpushed: _, ...cleanData } = listData as typeof listData & { isUnpushed?: boolean };

    // Write the list JSON using gitMsgList
    const writeResult = await gitMsgList.write(repository, 'social', listId, cleanData);
    if (!writeResult.success) {
      return writeResult;
    }

    // Update in-memory storage to include new list
    if (listsInitialized) {
      const currentLists = globalLists.get(repository) || [];
      // Mark new list as unpushed since it hasn't been pushed to origin yet
      const listWithUnpushedStatus = { ...listData, isUnpushed: true };
      const updatedLists = [...currentLists, listWithUnpushedStatus];
      globalLists.set(repository, updatedLists);
      globalListsByName.set(`${repository}:${listId}`, listWithUnpushedStatus);
    }

    // Clear list-specific caches since we created a new list
    await cache.refresh({ lists: ['*'] });

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'CREATE_LIST_ERROR',
        message: 'Failed to create list',
        details: error
      }
    };
  }
}

/**
 * Delete a repository list
 */
async function deleteList(
  repository: string,
  name: string
): Promise<Result<void>> {
  try {
    // Check if list exists
    const existingList = await gitMsgList.read<List>(repository, 'social', name);
    if (!existingList.success || !existingList.data) {
      return {
        success: false,
        error: {
          code: 'LIST_NOT_FOUND',
          message: `List '${name}' not found`
        }
      };
    }

    // Delete the list using gitMsgList
    const deleteResult = await gitMsgList.delete(repository, 'social', name);
    if (!deleteResult.success) {
      return deleteResult;
    }

    // No need to manage remotes with isolated repository architecture

    // Update in-memory storage to remove deleted list
    const currentLists = globalLists.get(repository) || [];
    const updatedLists = currentLists.filter(list => list.id !== name);
    globalLists.set(repository, updatedLists);
    globalListsByName.delete(`${repository}:${name}`);

    // Clear list-specific caches since we deleted a list
    await cache.refresh({ lists: [name] });

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'DELETE_LIST_ERROR',
        message: 'Failed to delete list',
        details: error
      }
    };
  }
}

/**
 * Update an existing repository list
 */
async function updateList(
  repository: string,
  listId: string,
  updates: Partial<List>
): Promise<Result<void>> {
  try {
    log('info', `[updateList] Updating list '${listId}'`);

    // Get existing list
    const existingResult = await gitMsgList.read<List>(repository, 'social', listId);
    if (!existingResult.success || !existingResult.data) {
      log('error', `[updateList] List '${listId}' not found`);
      return {
        success: false,
        error: {
          code: 'LIST_NOT_FOUND',
          message: `List '${listId}' not found`
        }
      };
    }

    // Merge updates with existing data
    const updatedList: List = {
      ...existingResult.data,
      ...updates,
      version: existingResult.data.version, // Preserve version
      id: existingResult.data.id // Preserve ID
    };

    // Remove runtime-only fields before writing
    const { isUnpushed: _isUnpushed, isFollowedLocally: _isFollowedLocally, ...cleanData } =
      updatedList as typeof updatedList & { isUnpushed?: boolean; isFollowedLocally?: boolean };

    // Write the updated list
    const writeResult = await gitMsgList.write(repository, 'social', listId, cleanData);
    if (!writeResult.success) {
      log('error', '[updateList] Write failed:', writeResult.error);
      return writeResult;
    }

    // Update in-memory storage with updated list
    if (listsInitialized) {
      const currentLists = globalLists.get(repository) || [];
      const updatedLists = currentLists.map(list =>
        list.id === listId ? updatedList : list
      );
      globalLists.set(repository, updatedLists);
      globalListsByName.set(`${repository}:${listId}`, updatedList);
    }

    // Clear list-specific caches since we updated a list
    await cache.refresh({ lists: [listId] });

    log('info', `[updateList] Successfully updated list '${listId}' with ${Object.keys(updates).length} changes`);
    return { success: true };
  } catch (error) {
    log('error', '[updateList] Exception occurred:', error);
    return {
      success: false,
      error: {
        code: 'UPDATE_LIST_ERROR',
        message: 'Failed to update list',
        details: error
      }
    };
  }
}

/**
 * Add a repository to a list
 */
async function addRepositoryToList(
  repository: string,
  listId: string,
  repositoryUrl: string,
  explicitStorageBase?: string
): Promise<Result<void>> {
  try {
    // Use explicitly passed storage base or fall back to configured one
    const effectiveStorageBase = explicitStorageBase || storageBase;
    let normalizedUrl = '';
    let branch = '';

    if (repositoryUrl.includes('#branch:')) {
      normalizedUrl = gitMsgUrl.normalize(repositoryUrl);
      const parsed = gitMsgRef.parseRepositoryId(repositoryUrl);
      branch = parsed.branch;
    } else {
      const branchResult = await getRemoteDefaultBranch(repository, repositoryUrl);
      if (!branchResult.success || !branchResult.data) {
        return {
          success: false,
          error: {
            code: 'BRANCH_DETECTION_FAILED',
            message: `Could not detect default branch for repository: ${repositoryUrl}`,
            details: branchResult.error
          }
        };
      }
      branch = branchResult.data;
      normalizedUrl = gitMsgUrl.normalize(`${repositoryUrl}#branch:${branch}`);
    }

    // Get existing list
    const existingResult = await gitMsgList.read<List>(repository, 'social', listId);
    if (!existingResult.success || !existingResult.data) {
      return {
        success: false,
        error: {
          code: 'LIST_NOT_FOUND',
          message: `List '${listId}' not found`
        }
      };
    }

    const list = existingResult.data;

    // Check if repository already exists in list
    const exists = list.repositories.some(repo => {
      const normalized = gitMsgUrl.normalize(repo);
      return normalized === normalizedUrl;
    });

    if (exists) {
      return {
        success: false,
        error: {
          code: 'REPOSITORY_EXISTS',
          message: `Repository '${repositoryUrl}' already exists in list '${listId}'`
        }
      };
    }

    // Extract base URL without branch (normalizedUrl already has branch appended)
    const baseUrl = normalizedUrl.split('#')[0] || normalizedUrl;

    // Add repository to list
    const updatedRepositories = [...list.repositories, normalizedUrl];

    // Update the list
    const updateResult = await updateList(repository, listId, {
      repositories: updatedRepositories
    });

    if (!updateResult.success) {
      return updateResult;
    }

    // Refresh cache for the newly added repository and list to force reload from Git
    const repoIdentifier = `${baseUrl}#branch:${branch}`;
    await cache.refresh({
      repositories: [repoIdentifier],
      lists: [listId]
    }, repository, effectiveStorageBase);
    log('debug', '[addRepositoryToList] Cache refreshed to include new repository posts');

    log('info', `[addRepositoryToList] Added '${normalizedUrl}' to list '${listId}'`);
    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'ADD_REPOSITORY_ERROR',
        message: 'Failed to add repository to list',
        details: error
      }
    };
  }
}

/**
 * Remove a repository from a list
 */
async function removeRepositoryFromList(
  repository: string,
  listId: string,
  repositoryUrl: string
): Promise<Result<void>> {
  try {
    log('info', `[removeRepositoryFromList] Starting removal of '${repositoryUrl}' from list '${listId}'`);

    // Get base repository URL (without branch suffix) for comparison
    const baseUrlToRemove = getBaseRepositoryUrl(repositoryUrl);
    log('debug', `[removeRepositoryFromList] Base URL to remove: '${repositoryUrl}' → '${baseUrlToRemove}'`);

    // Get existing list
    const existingResult = await gitMsgList.read<List>(repository, 'social', listId);
    if (!existingResult.success || !existingResult.data) {
      log('error', `[removeRepositoryFromList] List '${listId}' not found or failed to read`);
      return {
        success: false,
        error: {
          code: 'LIST_NOT_FOUND',
          message: `List '${listId}' not found`
        }
      };
    }

    const list = existingResult.data;

    // Filter out the repository using base URL matching
    const updatedRepositories = list.repositories.filter(repo => {
      const baseUrl = getBaseRepositoryUrl(repo);
      return baseUrl !== baseUrlToRemove;
    });

    // Check if repository was actually removed
    if (updatedRepositories.length === list.repositories.length) {
      log('error', `[removeRepositoryFromList] Repository with base URL '${baseUrlToRemove}' not found in list. Available repositories:`,
        list.repositories.map(repo => `'${repo}' (base: '${getBaseRepositoryUrl(repo)}')`));
      return {
        success: false,
        error: {
          code: 'REPOSITORY_NOT_FOUND',
          message: `Repository '${repositoryUrl}' not found in list '${listId}'`
        }
      };
    }

    log('info', `[removeRepositoryFromList] Removed repository from list '${listId}' (${list.repositories.length} → ${updatedRepositories.length})`);

    // Update the list
    const updateResult = await updateList(repository, listId, {
      repositories: updatedRepositories
    });

    if (!updateResult.success) {
      log('error', '[removeRepositoryFromList] updateList failed:', updateResult.error);
      return updateResult;
    }

    log('info', `[removeRepositoryFromList] Successfully removed repository with base URL '${baseUrlToRemove}' from list '${listId}'`);
    return { success: true };
  } catch (error) {
    log('error', '[removeRepositoryFromList] Exception occurred:', error);
    return {
      success: false,
      error: {
        code: 'REMOVE_REPOSITORY_ERROR',
        message: 'Failed to remove repository from list',
        details: error
      }
    };
  }
}

/**
 * Get all lists in a repository
 */
async function getLists(repository: string, workspaceRoot?: string): Promise<Result<List[]>> {
  try {
    // Check if this is a remote repository request
    if (gitMsgUrl.validate(repository) && workspaceRoot) {
      return await getRemoteLists(repository, workspaceRoot);
    }

    // Check local storage first
    const storedLists = getAllListsFromStorage(repository);
    if (storedLists.length > 0) {
      log('debug', `[getLists] Returning ${storedLists.length} lists from storage`);
      return { success: true, data: storedLists };
    }

    // Get all list refs - first try local refs
    let refs = await listRefs(repository, 'social/lists/');

    // Check if this is an isolated clone (has upstream remote but no local refs)
    let isIsolatedClone = false;
    if (refs.length === 0) {
      const checkUpstream = await execGit(repository, ['remote', 'get-url', 'upstream']);
      if (checkUpstream.success && checkUpstream.data) {
        // This is an isolated clone - fetch latest refs first
        isIsolatedClone = true;
        const upstreamUrl = checkUpstream.data.stdout.trim();
        log('debug', '[getLists] No local refs found, fetching latest from upstream:', upstreamUrl);

        // Fetch latest lists from upstream (won't affect post cache)
        if (storageBase) {
          // Don't pass branch - let fetchRepository use the branch stored in git config
          // from when the repository was initially cloned
          const fetchResult = await storage.repository.fetch(storageBase, upstreamUrl);
          if (!fetchResult.success) {
            log('warn', '[getLists] Failed to fetch latest refs:', fetchResult.error);
            // If repository was cleaned up due to lock file, skip it (will re-clone next time)
            if (fetchResult.error?.code === 'LOCK_FILE_ERROR') {
              log('debug', '[getLists] Skipping repository after lock file cleanup');
              return { success: true, data: [] };
            }
          } else {
            log('debug', '[getLists] Successfully fetched latest refs from upstream');
          }
        }

        // Now check remote refs after fetching
        const remoteRefsResult = await execGit(repository, [
          'for-each-ref',
          '--format=%(refname)',
          'refs/remotes/upstream/gitmsg/social/lists/'
        ]);

        if (remoteRefsResult.success && remoteRefsResult.data?.stdout.trim()) {
          // Keep full remote refs for reading later
          refs = remoteRefsResult.data.stdout
            .trim()
            .split('\n')
            .filter(Boolean);
          log('info', '[getLists] Found', refs.length, 'remote refs in isolated clone');
        }
      }
    }

    const lists: List[] = [];

    // Check if origin exists to determine unpushed status
    let originLists: Set<string> | null = null;
    const checkOrigin = await execGit(repository, ['remote', 'get-url', 'origin']);
    if (checkOrigin.success) {
      // Get lists from origin
      const originListsResult = await execGit(repository, [
        'ls-remote',
        'origin',
        'refs/gitmsg/social/lists/*'
      ]);

      if (originListsResult.success && originListsResult.data) {
        originLists = new Set<string>();
        const lines = originListsResult.data.stdout.trim().split('\n').filter(Boolean);
        for (const line of lines) {
          const match = line.match(/refs\/gitmsg\/social\/lists\/(.+)$/);
          if (match && match[1]) {
            originLists.add(match[1]);
          }
        }
      }
    }

    // Read each list's JSON
    for (const ref of refs) {
      let listId: string;
      let listData: List | null = null;

      if (isIsolatedClone) {
        // For isolated clones, read from remote ref
        listId = ref.replace('refs/remotes/upstream/gitmsg/social/lists/', '');

        // Read the content directly from the remote ref's commit message
        const showResult = await execGit(repository, ['show', '-s', '--format=%B', ref]);
        if (showResult.success && showResult.data?.stdout) {
          try {
            listData = JSON.parse(showResult.data.stdout) as List;
          } catch (e) {
            log('warn', '[getLists] Failed to parse list JSON for', listId, ':', e);
            continue;
          }
        }
      } else {
        // For regular repos, use gitMsgList
        listId = ref.replace('social/lists/', '');
        const listResult = await gitMsgList.read<List>(repository, 'social', listId);
        if (listResult.success && listResult.data) {
          listData = listResult.data;
        }
      }

      if (listData) {
        // Ensure id and name fields exist
        if (!listData.id) {
          listData.id = listId;
        }
        if (!listData.name) {
          listData.name = listId;
        }

        // Check if list is unpushed (only relevant for non-isolated clones)
        if (!isIsolatedClone && originLists !== null) {
          listData.isUnpushed = !originLists.has(listId);
        }

        lists.push(listData);
      }
    }

    log('info', `[getLists] Loaded ${lists.length} lists`);

    return { success: true, data: lists };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'GET_LISTS_ERROR',
        message: 'Failed to get lists',
        details: error
      }
    };
  }
}

/**
 * Get a specific list
 */
async function getList(repository: string, id: string): Promise<Result<List | null>> {
  try {
    // Check local storage first
    const storedList = getListFromStorage(repository, id);
    if (storedList) {
      log('debug', `[getList] Returning list '${id}' from storage`);
      return { success: true, data: storedList };
    }

    // Read list JSON directly - O(1) operation!
    const listResult = await gitMsgList.read<List>(repository, 'social', id);
    if (!listResult.success) {
      return listResult;
    }

    if (!listResult.data) {
      return { success: true, data: null };
    }

    const list = listResult.data;

    // Ensure id and name fields exist
    if (!list.id) {
      list.id = id;
    }
    if (!list.name) {
      list.name = id;
    }

    log('debug', `[getList] Loaded list '${id}'`);

    return { success: true, data: list };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'GET_LIST_ERROR',
        message: 'Failed to get list',
        details: error
      }
    };
  }
}

/**
 * Get repositories in a list
 */
async function getListRepositories(
  repository: string,
  listId: string
): Promise<Result<string[]>> {
  const list = await getList(repository, listId);
  if (!list.success) {
    return {
      success: false,
      error: list.error
    };
  }

  if (!list.data) {
    return {
      success: false,
      error: {
        code: 'LIST_NOT_FOUND',
        message: `List '${listId}' not found`
      }
    };
  }

  // Return the repository strings array from the list
  return { success: true, data: list.data.repositories };
}

/**
 * Synchronize a list with a remote
 */
async function syncList(
  repository: string,
  listId: string,
  remote: string = 'origin'
): Promise<Result<void>> {
  try {
    // Push list branch
    const pushResult = await execGit(repository, [
      'push',
      remote,
      `refs/heads/gitmsg-lists-${listId}:refs/heads/gitmsg-lists-${listId}`
    ]);

    if (!pushResult.success) {
      return {
        success: false,
        error: {
          code: 'PUSH_ERROR',
          message: 'Failed to push list branch',
          details: String(pushResult.error)
        }
      };
    }

    // Push list reference
    const pushRefResult = await execGit(repository, [
      'push',
      remote,
      `refs/gitmsg/social/lists/${listId}:refs/gitmsg/social/lists/${listId}`
    ]);

    if (!pushRefResult.success) {
      return {
        success: false,
        error: {
          code: 'PUSH_REF_ERROR',
          message: 'Failed to push list reference',
          details: String(pushRefResult.error)
        }
      };
    }

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'SYNC_ERROR',
        message: 'Failed to sync list',
        details: error
      }
    };
  }
}

/**
 * Mark remote lists with isFollowedLocally flag by checking workspace lists
 */
function setFollowedStatus(
  remoteLists: List[],
  workspaceLists: List[],
  normalizedRepoUrl: string
): void {
  for (const remoteList of remoteLists) {
    remoteList.isFollowedLocally = workspaceLists.some(wl => {
      if (!wl.source) {return false;}
      const parsed = gitMsgRef.parse(wl.source);
      if (parsed.type !== 'list' || !parsed.repository) {return false;}
      const normalizedSourceUrl = gitMsgUrl.normalize(parsed.repository);
      return normalizedSourceUrl === normalizedRepoUrl && parsed.value === remoteList.id;
    });
  }
}

/**
 * Get lists from a workspace remote using normal git operations
 */
async function getWorkspaceRemoteLists(workspaceRoot: string, remoteName: string): Promise<Result<List[]>> {
  try {
    log('debug', `[getWorkspaceRemoteLists] Fetching lists from workspace remote: ${remoteName}`);

    // Use git ls-remote on the workspace remote to discover lists
    const lsRemoteResult = await execGit(workspaceRoot, [
      'ls-remote',
      remoteName,
      'refs/gitmsg/social/lists/*'
    ]);

    if (!lsRemoteResult.success || !lsRemoteResult.data || !lsRemoteResult.data.stdout.trim()) {
      log('debug', `[getWorkspaceRemoteLists] No lists found in remote: ${remoteName}`);
      return { success: true, data: [] };
    }

    // Parse list names from refs
    const listRefs = lsRemoteResult.data.stdout.split('\n')
      .filter(Boolean)
      .map(line => {
        const [hash, ref] = line.split('\t');
        const name = ref?.replace('refs/gitmsg/social/lists/', '') || '';
        return { name, hash };
      });

    log('debug', `[getWorkspaceRemoteLists] Found ${listRefs.length} list refs in remote: ${remoteName}`);

    // Get the repository URL for this remote to use in caching
    const getUrlResult = await execGit(workspaceRoot, ['remote', 'get-url', remoteName]);
    const rawRepositoryUrl = getUrlResult.success && getUrlResult.data ? getUrlResult.data.stdout.trim() : '';
    const repositoryUrl = rawRepositoryUrl ? gitMsgUrl.normalize(rawRepositoryUrl) : '';

    log(
      'debug',
      `[getWorkspaceRemoteLists] Remote '${remoteName}' URL: raw='${rawRepositoryUrl}' normalized='${repositoryUrl}'`
    );

    // Fetch JSON data for each list using workspace operations
    const lists: List[] = [];
    for (const { name } of listRefs) {
      try {
        // Fetch the specific ref from the remote
        const tempRef = `refs/temp/remote-list-${remoteName}-${name}`;
        const remoteRef = `refs/gitmsg/social/lists/${name}`;

        const fetchResult = await execGit(workspaceRoot, [
          'fetch',
          remoteName,
          `${remoteRef}:${tempRef}`
        ]);

        if (!fetchResult.success) {
          log('warn', `[getWorkspaceRemoteLists] Failed to fetch list "${name}" from remote "${remoteName}"`);
          continue;
        }

        try {
          // Get the commit message containing JSON
          const messageResult = await execGit(workspaceRoot, [
            'show',
            '-s',
            '--format=%B',
            tempRef
          ]);

          if (messageResult.success && messageResult.data) {
            const jsonContent = messageResult.data.stdout.trim();
            const jsonData = JSON.parse(jsonContent) as List;

            // Use list data directly (no date conversion needed)
            const list: List = jsonData;

            lists.push(list);

            if (repositoryUrl) {
              log('debug', `[getWorkspaceRemoteLists] Found list '${name}'`);
            }
          }
        } finally {
          // Clean up temp ref
          await execGit(workspaceRoot, ['update-ref', '-d', tempRef]).catch(() => {
            // Ignore cleanup errors
          });
        }
      } catch (error) {
        log('error', `[getWorkspaceRemoteLists] Failed to fetch list "${name}":`, error);
        // Add fallback list
        lists.push({
          version: '0.1.0',
          id: name,
          name: name,
          repositories: []
        } as List);
      }
    }

    log('info', `[getWorkspaceRemoteLists] Successfully loaded ${lists.length} lists from workspace remote`);
    const workspaceListsResult = await getLists(workspaceRoot);
    if (workspaceListsResult.success && workspaceListsResult.data && repositoryUrl) {
      setFollowedStatus(lists, workspaceListsResult.data, gitMsgUrl.normalize(repositoryUrl));
    }
    return { success: true, data: lists };
  } catch (error) {
    log('error', '[getWorkspaceRemoteLists] Error fetching workspace remote lists:', error);
    return {
      success: false,
      error: {
        code: 'GET_WORKSPACE_REMOTE_LISTS_ERROR',
        message: 'Failed to get workspace remote lists',
        details: error
      }
    };
  }
}

/**
 * Get lists from a remote repository
 * @internal
 */
async function getRemoteLists(repositoryUrl: string, workspaceRoot: string): Promise<Result<List[]>> {
  try {
    log('debug', `[getRemoteLists] Fetching lists from remote repository: ${repositoryUrl}`);

    // First check if this repository exists as a workspace remote
    const remotesResult = await listRemotes(workspaceRoot);
    if (remotesResult.success && remotesResult.data) {
      const normalizedUrl = gitMsgUrl.normalize(repositoryUrl);
      const matchingRemote = remotesResult.data.find(remote =>
        gitMsgUrl.normalize(remote.url) === normalizedUrl
      );

      if (matchingRemote) {
        log(
          'debug',
          `[getRemoteLists] Repository exists as workspace remote '${matchingRemote.name}', using workspace operations`
        );
        // Use workspace git operations instead of temp repository
        return await getWorkspaceRemoteLists(workspaceRoot, matchingRemote.name);
      }
    }

    log('debug', '[getRemoteLists] Repository not found in workspace remotes, using isolated repository');

    // Use isolated repository to fetch lists
    if (!storageBase) {
      log('warn', '[getRemoteLists] No storage base configured for isolated repositories');
      return { success: true, data: [] };
    }

    // Parse URL to get base URL and branch
    const normalizedUrl = gitMsgUrl.normalize(repositoryUrl);
    let baseUrl = normalizedUrl;
    let branch = 'main';

    const branchMatch = normalizedUrl.match(/#branch:(.+)$/);
    if (branchMatch && branchMatch[1]) {
      baseUrl = normalizedUrl.substring(0, normalizedUrl.indexOf('#branch:'));
      branch = branchMatch[1];
    }

    // Ensure repository is cloned/fetched
    const ensureResult = await storage.repository.ensure(storageBase, baseUrl, branch, {
      isPersistent: false // External repositories are temporary by default
    });

    if (!ensureResult.success || !ensureResult.data) {
      log('error', `[getRemoteLists] Failed to ensure repository: ${ensureResult.error?.message}`);
      return { success: true, data: [] };
    }

    const isolatedRepoPath = ensureResult.data;
    log('debug', `[getRemoteLists] Using isolated repository at: ${isolatedRepoPath}`);

    // Get list refs from remote tracking branches
    const forEachRefResult = await execGit(isolatedRepoPath, [
      'for-each-ref',
      '--format=%(refname)',
      'refs/remotes/upstream/gitmsg/social/lists/'
    ]);

    if (!forEachRefResult.success || !forEachRefResult.data?.stdout.trim()) {
      log('debug', '[getRemoteLists] No list refs found in isolated repository');
      return { success: true, data: [] };
    }

    const refs = forEachRefResult.data.stdout
      .trim()
      .split('\n')
      .filter(Boolean)
      .map(ref => ref.replace('refs/remotes/upstream/gitmsg/social/lists/', ''));

    log('debug', `[getRemoteLists] Found ${refs.length} list refs in isolated repository`);

    const lists: List[] = [];

    // Read each list's JSON from the isolated repository
    for (const listId of refs) {
      try {
        // Get the commit message from the remote ref
        const messageResult = await execGit(isolatedRepoPath, [
          'show',
          '-s',
          '--format=%B',
          `refs/remotes/upstream/gitmsg/social/lists/${listId}`
        ]);

        if (messageResult.success && messageResult.data) {
          const jsonContent = messageResult.data.stdout.trim();
          const list = JSON.parse(jsonContent) as List;

          // Ensure id and name fields exist
          if (!list.id) {
            list.id = listId;
          }
          if (!list.name) {
            list.name = listId;
          }

          lists.push(list);
          log('debug', `[getRemoteLists] Successfully loaded list '${listId}' with ${list.repositories?.length || 0} repositories`);
        }
      } catch (error) {
        log('error', `[getRemoteLists] Failed to read list "${listId}":`, error);
      }
    }

    log('info', `[getRemoteLists] Successfully loaded ${lists.length} remote lists`);
    const workspaceListsResult = await getLists(workspaceRoot);
    if (workspaceListsResult.success && workspaceListsResult.data) {
      setFollowedStatus(lists, workspaceListsResult.data, gitMsgUrl.normalize(baseUrl));
    }
    return { success: true, data: lists };
  } catch (error) {
    log('error', '[getRemoteLists] Error fetching remote lists:', error);
    return {
      success: false,
      error: {
        code: 'GET_REMOTE_LISTS_ERROR',
        message: 'Failed to get remote lists',
        details: error
      }
    };
  }
}

/**
 * Fetch a specific list from a workspace remote using normal git operations
 * @internal
 * Currently not used but kept for potential future use
 */
/*
async function fetchWorkspaceRemoteList(workspaceRoot: string, remoteName: string, listId: string): Promise<List> {
  try {
    log('debug', `[fetchWorkspaceRemoteList] Fetching list '${listId}' from workspace remote '${remoteName}'`);

    // Fetch the specific ref from the remote to a temporary local ref
    const tempRef = `refs/temp/remote-list-${remoteName}-${listId}`;
    const remoteRef = `refs/gitmsg/social/lists/${listId}`;

    const fetchResult = await execGit(workspaceRoot, [
      'fetch',
      remoteName,
      `${remoteRef}:${tempRef}`
    ]);

    if (!fetchResult.success) {
      throw new Error(
        `Failed to fetch list ref from workspace remote: ${fetchResult.error?.message || 'Unknown error'}`
      );
    }

    try {
      // Get the commit message containing JSON
      const messageResult = await execGit(workspaceRoot, [
        'show',
        '-s',
        '--format=%B',
        tempRef
      ]);

      if (!messageResult.success || !messageResult.data) {
        throw new Error('Failed to get list data from workspace remote');
      }

      const jsonContent = messageResult.data.stdout.trim();
      const jsonData = JSON.parse(jsonContent) as List;

      // Convert ISO strings to Dates
      const listState: List = {
        ...jsonData,
        created: new Date(jsonData.created),
        updated: new Date(jsonData.updated),
        repositories: jsonData.repositories.map(repo => ({
          ...repo,
          added: new Date(repo.added)
        }))
      };

      log(
        'debug',
        `[fetchWorkspaceRemoteList] Successfully fetched list '${listId}' from workspace remote '${remoteName}'`
      );
      return listState;
    } finally {
      // Clean up temporary ref
      await execGit(workspaceRoot, ['update-ref', '-d', tempRef]).catch(() => {
        // Ignore cleanup errors
      });
    }
  } catch (error) {
    log(
      'error',
      `[fetchWorkspaceRemoteList] Error fetching list '${listId}' from workspace remote '${remoteName}':`,
      error
    );
    throw error;
  }
}
*/

/**
 * Fetch a specific list from a truly external repository (not a workspace remote)
 * Returns a minimal list structure as full fetching requires local access
 * @internal
 * Currently not used but kept for potential future use
 */
/*
function fetchRemoteList(repositoryUrl: string, listId: string, _workspaceRoot: string): Promise<List> {
  // For external repositories, we can only return basic info
  // Full list content requires fetching through workspace remotes
  log(
    'debug',
    `[fetchRemoteList] Creating minimal list for external repository: ${repositoryUrl}/${listId}`
  );

  return Promise.resolve({
    id: listId,
    name: listId,
    repositories: [],
    created: new Date(),
    updated: new Date()
  } as List);
}
*/

/**
 * Get unpushed lists count - lists that exist locally but not on origin
 */
async function getUnpushedListsCount(repository: string): Promise<number> {
  try {
    // Get all lists from my repository
    const myLists = await listRefs(repository, 'social/lists/');
    if (!myLists || myLists.length === 0) {
      return 0;
    }

    // Check if origin remote exists
    const checkOrigin = await execGit(repository, ['remote', 'get-url', 'origin']);
    if (!checkOrigin.success) {
      // No origin remote, all lists are unpushed
      return myLists.length;
    }

    // Get lists from origin
    const originListsResult = await execGit(repository, [
      'ls-remote',
      'origin',
      'refs/gitmsg/social/lists/*'
    ]);

    if (!originListsResult.success || !originListsResult.data) {
      // Origin exists but no lists on origin, all local lists are unpushed
      return myLists.length;
    }

    // Parse origin lists
    const originLists = new Set<string>();
    const lines = originListsResult.data.stdout.trim().split('\n').filter(Boolean);
    for (const line of lines) {
      const match = line.match(/refs\/gitmsg\/social\/lists\/(.+)$/);
      if (match && match[1]) {
        originLists.add(match[1]);
      }
    }

    // Count unpushed lists (my lists not in origin)
    let unpushedCount = 0;
    for (const ref of myLists) {
      const listId = ref.replace('social/lists/', '');
      if (!originLists.has(listId)) {
        unpushedCount++;
      }
    }

    return unpushedCount;
  } catch (error) {
    log('error', '[getUnpushedListsCount] Error:', error);
    return 0;
  }
}

/**
 * Follow a list from another repository
 */
async function followList(
  repository: string,
  sourceRepo: string,
  sourceListId: string,
  targetListId?: string,
  explicitStorageBase?: string
): Promise<Result<{ listId: string }>> {
  try {
    // Validate list IDs
    const listIdPattern = /^[a-zA-Z0-9_-]{1,40}$/;
    if (!listIdPattern.test(sourceListId)) {
      return {
        success: false,
        error: {
          code: 'INVALID_LIST_ID',
          message: 'Source list ID format invalid'
        }
      };
    }
    if (targetListId && !listIdPattern.test(targetListId)) {
      return {
        success: false,
        error: {
          code: 'INVALID_LIST_ID',
          message: 'Target list ID format invalid'
        }
      };
    }

    // Use the exported getRemoteLists function
    const sourceLists = await getRemoteLists(sourceRepo, repository);
    if (!sourceLists.success) {
      return { success: false, error: sourceLists.error };
    }

    const sourceList = sourceLists.data?.find(l => l.id === sourceListId);
    if (!sourceList) {
      return {
        success: false,
        error: {
          code: 'SOURCE_NOT_FOUND',
          message: 'Source list not found'
        }
      };
    }

    const newListId = targetListId || sourceListId;

    // Check for existing list
    const existing = getListFromStorage(repository, newListId);
    if (existing) {
      return {
        success: false,
        error: {
          code: 'LIST_EXISTS',
          message: 'List with this ID already exists'
        }
      };
    }

    await createList(repository, newListId, sourceList.name);

    for (const repo of sourceList.repositories) {
      await addRepositoryToList(repository, newListId, repo, explicitStorageBase);
    }

    // Remove any existing fragment (like #branch:main) from sourceRepo
    const baseSourceRepo = sourceRepo.split('#')[0];

    await updateList(repository, newListId, {
      source: gitMsgRef.create('list', sourceListId, baseSourceRepo)
    });

    // Refresh cache for the new list and its repositories
    const effectiveStorageBase = explicitStorageBase || storageBase;
    await cache.refresh({
      lists: [newListId],
      repositories: sourceList.repositories
    }, repository, effectiveStorageBase);

    return { success: true, data: { listId: newListId } };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'FOLLOW_LIST_ERROR',
        message: 'Failed to follow list',
        details: error
      }
    };
  }
}

/**
 * Sync a followed list with its source
 */
async function syncFollowedList(
  repository: string,
  listId: string,
  explicitStorageBase?: string
): Promise<Result<{ added: number; removed: number }>> {
  try {
    // Ensure lists are initialized
    if (!listsInitialized) {
      await initializeListStorage(repository);
    }

    const local = getListFromStorage(repository, listId);
    if (!local?.source) {
      return {
        success: false,
        error: {
          code: 'NOT_FOLLOWED',
          message: 'List is not followed'
        }
      };
    }

    const parsed = gitMsgRef.parse(local.source);
    if (parsed.type !== 'list' || !parsed.repository || !parsed.value) {
      return {
        success: false,
        error: {
          code: 'INVALID_SOURCE',
          message: 'Invalid source format'
        }
      };
    }

    const sourceRepo = parsed.repository;
    const sourceListId = parsed.value;

    // Use the exported getRemoteLists function
    const sourceLists = await getRemoteLists(sourceRepo, repository);
    if (!sourceLists.success) {
      return { success: false, error: sourceLists.error };
    }

    const source = sourceLists.data?.find(l => l.id === sourceListId);
    if (!source) {
      return {
        success: false,
        error: {
          code: 'SOURCE_NOT_FOUND',
          message: 'Source list not found'
        }
      };
    }

    const added = source.repositories.filter(r => !local.repositories.includes(r)).length;
    const removed = local.repositories.filter(r => !source.repositories.includes(r)).length;

    await updateList(repository, listId, {
      repositories: source.repositories
    });

    // Refresh cache for the updated list and repositories
    const effectiveStorageBase = explicitStorageBase || storageBase;
    await cache.refresh({
      lists: [listId],
      repositories: source.repositories
    }, repository, effectiveStorageBase);

    return { success: true, data: { added, removed } };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'SYNC_LIST_ERROR',
        message: 'Failed to sync followed list',
        details: error
      }
    };
  }
}

/**
 * Unfollow a list (remove import source metadata)
 */
async function unfollowList(
  repository: string,
  listId: string
): Promise<Result<void>> {
  try {
    // Ensure lists are initialized
    if (!listsInitialized) {
      await initializeListStorage(repository);
    }

    const local = getListFromStorage(repository, listId);
    if (!local?.source) {
      return {
        success: false,
        error: {
          code: 'NOT_FOLLOWED',
          message: 'List is not followed'
        }
      };
    }

    await updateList(repository, listId, { source: undefined });

    // Refresh cache for the unfollowed list
    await cache.refresh({ lists: [listId] });

    return { success: true };
  } catch (error) {
    return {
      success: false,
      error: {
        code: 'UNFOLLOW_LIST_ERROR',
        message: 'Failed to unfollow list',
        details: error
      }
    };
  }
}
