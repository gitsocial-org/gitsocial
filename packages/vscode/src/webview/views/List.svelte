<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { Post, List } from '@gitsocial/core';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';
  import Tabs from '../components/Tabs.svelte';
  import Avatar from '../components/Avatar.svelte';
  import DateNavigation from '../components/DateNavigation.svelte';
  import { skipCacheOnNextRefresh } from '../stores';
  import { gitMsgRef, gitMsgUrl, gitHost } from '@gitsocial/core/client';
  import { formatRelativeTime, useEventDrivenTimeUpdates, getWeekStart, getWeekEnd, getWeekLabel } from '../utils/time';
  import { webLog } from '../utils/weblog';

  let list: List | null = null;
  let postsTabData: Post[] = [];
  let repliesTabData: Post[] = [];
  let lists: List[] = [];
  let error = '';
  let fetchingUpdates = false;
  let activeTab: 'posts' | 'replies' | 'repositories' = 'posts';
  let lastFetchTime: Date | null = null;
  let currentTime = Date.now();
  let cleanupTimeUpdates: (() => void) | null = null;

  // Week navigation state
  let weekOffset = 0; // 0 = this week, -1 = last week, -2 = 2 weeks ago, etc.
  let isLoadingWeek = false;

  // Computed values for current week
  $: weekLabel = getWeekLabel(weekOffset);

  // Reactive fetch time display that updates with currentTime
  $: fetchTimeDisplay = lastFetchTime && currentTime ? formatRelativeTime(lastFetchTime) : '';

  // Repository management state
  let newRepositoryUrl = '';
  let newRepositoryBranch = '';
  let isAddingRepository = false;
  let showCustomBranch = false;

  // Rename state
  let isRenaming = false;
  let editingName = false;
  let newListName = '';

  // Get list ID, repository, and activeTab from view params
  const viewParams = (window as { viewParams?: { listId?: string; repository?: string; activeTab?: 'posts' | 'replies' | 'repositories'; list?: List } }).viewParams;
  const listId = viewParams?.listId;
  const repository = viewParams?.repository;
  const passedList = viewParams?.list;

  // Initialize list from params if available (avoids backend call)
  list = passedList || null;

  // Compute clean base repository URL and list reference
  const baseRepository = repository ? repository.split('#')[0] : null;
  const listReference = baseRepository && listId ? `${baseRepository}#list:${listId}` : null;

  // Check if this remote list is already followed locally
  let isRemoteListFollowed = false;
  $: {
    const followed = (repository && lists.some(l => l.source === listReference)) || (list?.isFollowedLocally ?? false);
    isRemoteListFollowed = followed;
  }

  // Track whether we're waiting for remote repository lists to load
  let waitingForRemoteLists = false;
  let remoteListsTimeout: ReturnType<typeof setTimeout> | null = null;

  // Set initial active tab from params if provided
  if (viewParams?.activeTab) {
    activeTab = viewParams.activeTab;
  }

  $: tabs = [
    { id: 'posts', label: 'Posts', count: postsTabData.length },
    { id: 'replies', label: 'Replies', count: repliesTabData.length },
    { id: 'repositories', label: 'Repositories', count: list?.repositories.length || 0 }
  ];

  onMount(() => {
    webLog('debug', '[ViewList] Component mounted with listId:', listId);

    if (!listId) {
      error = 'No list ID provided';
      return;
    }

    // Don't set initial fetch time - wait for backend to provide it

    // Listen for messages from extension
    window.addEventListener('message', handleMessage);

    // Set up event-driven time updates
    cleanupTimeUpdates = useEventDrivenTimeUpdates(() => {
      currentTime = Date.now();
    });

    // Request data in parallel
    loadData();
  });

  onDestroy(() => {
    // Clean up event listener
    window.removeEventListener('message', handleMessage);
    // Clean up time updates
    if (cleanupTimeUpdates) {
      cleanupTimeUpdates();
    }
  });

  function handleMessage(event: MessageEvent) {
    const message = event.data;
    webLog('debug', '[ViewList] Received message:', message.type, message);

    switch (message.type) {
      case 'lists': {
        lists = message.data || [];

        // Find the list we want
        const previousListName = list?.name;
        list = lists.find(l => l.id === listId) || null;

        // If list name changed (from external rename), update panel title
        if (list && previousListName && list.name !== previousListName) {
          webLog('debug', '[ViewList] List name changed externally, updating panel title');
          api.updatePanelTitle(list.name);
        }

        // If we were waiting for remote lists to load, now load the posts
        if (waitingForRemoteLists) {
          webLog('debug', '[ViewList] Remote lists arrived, now loading posts for week offset:', weekOffset);
          waitingForRemoteLists = false;
          if (remoteListsTimeout) {
            clearTimeout(remoteListsTimeout);
            remoteListsTimeout = null;
          }
          // Use the current week offset when loading posts
          loadPostsData();
        }
        webLog('debug', '[ViewList] Found list:', list);

        if (!list) {
          webLog('warn', '[ViewList] List not found for id:', listId);
          error = 'List not found';
        }
        break;
      }

      case 'posts':
        // Route to appropriate array based on request ID
        if (message.requestId === 'posts-tab') {
          // Filter to ensure we only show posts, quotes, and reposts in the Posts tab
          postsTabData = (message.data || []).filter(p =>
            p.type === 'post' || p.type === 'quote' || p.type === 'repost'
          );
          postsTabData.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
          isLoadingWeek = false;
        } else if (message.requestId === 'replies-tab') {
          // Filter to ensure we only show comments in the Replies tab
          repliesTabData = (message.data || []).filter(p => p.type === 'comment');
          repliesTabData.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
          isLoadingWeek = false;
        }
        break;

      case 'listFollowed':
        webLog('info', '[ViewList] List followed successfully');
        // Reload data to show updated list status
        loadData();
        break;

      case 'listSynced':
        if (message.data) {
          const { added, removed } = message.data;
          webLog('info', `[ViewList] List synced: ${added} added, ${removed} removed`);
          // Reload data to show synced changes
          loadData();
        }
        break;

      case 'listUnfollowed':
        webLog('info', '[ViewList] List unfollowed successfully');
        // Reload data to show updated list status
        loadData();
        break;

      case 'error':
        // console.log('[ViewList] Received error:', message.message);
        error = message.message || 'Failed to load data';
        isLoadingWeek = false;

        // If we were waiting for remote lists and got an error, stop waiting
        if (waitingForRemoteLists) {
          waitingForRemoteLists = false;
          if (remoteListsTimeout) {
            clearTimeout(remoteListsTimeout);
            remoteListsTimeout = null;
          }
          webLog('warn', '[ViewList] Failed to load remote lists, cannot load posts');
        }
        break;

      case 'fetchProgress':
        // Handle fetch progress updates
        if (message.data?.status === 'fetching') {
          fetchingUpdates = true;
        } else if (message.data?.status === 'completed') {
          fetchingUpdates = false;
          // Update fetch time when remote fetch completes successfully
          lastFetchTime = new Date();
        // User can manually refresh if they want to see new posts
        }
        break;

      case 'repositoryAdded':
      case 'repositoryRemoved':
        // Refresh lists after repository operations
        // Clear any existing timeout
        if (remoteListsTimeout) {
          clearTimeout(remoteListsTimeout);
          remoteListsTimeout = null;
        }

        if (repository) {
          // Remote repository list: refresh lists and posts
          waitingForRemoteLists = true;

          // Set timeout in case list fetching never completes
          remoteListsTimeout = setTimeout(() => {
            if (waitingForRemoteLists) {
              waitingForRemoteLists = false;
              remoteListsTimeout = null;
              error = 'Timeout waiting for remote repository lists. Please try again.';
              webLog('error', '[ViewList] Timeout waiting for remote lists after repository operation');
            }
          }, 10000); // 10 second timeout

          api.getLists(repository);
        } else {
          // Local workspace list: refresh lists and posts
          api.getLists();

          // For added repositories, skip cache to load fresh posts from newly initialized repo
          if (message.type === 'repositoryAdded') {
            // Give backend a moment to complete initialization
            setTimeout(() => {
              loadPostsData(true); // Skip cache for fresh data
            }, 500);
          } else {
            loadPostsData();
          }
        }
        break;

      case 'refresh': {
        const scopes = message.scope || ['all'];
        const operation = message.operation;
        const skipCache = message.skipCache || $skipCacheOnNextRefresh;

        if (skipCache && $skipCacheOnNextRefresh) {
          skipCacheOnNextRefresh.set(false);
          webLog('debug', '[ViewList] Skipping cache for this refresh');
        }

        // Handle list rename operation specially
        if (operation === 'listRenamed' && message.metadata) {
          webLog('info', '[ViewList] Handling listRenamed operation');
          if (isRenaming) {
            // Local rename: update UI directly
            isRenaming = false;
            editingName = false;
            if (list && message.metadata.listId === listId) {
              list.name = message.metadata.newName;
              api.updatePanelTitle(list.name);
              webLog('debug', '[ViewList] Updated list name from metadata');
            }
          } else {
            // External rename: reload lists to get updated name
            api.getLists(repository);
            webLog('debug', '[ViewList] Reloading lists after external rename');
          }
          // List renames don't affect posts, so we're done
          break;
        }

        // Standard scope-based refresh
        if (scopes.includes('all')) {
          // Reload everything
          webLog('debug', '[ViewList] Full refresh (scope: all)');
          loadData(skipCache);
        } else {
          // Selective reload based on scopes
          webLog('debug', '[ViewList] Selective refresh, scopes:', scopes);
          if (scopes.includes('posts')) {
            loadPostsData(skipCache);
          }
          if (scopes.includes('lists')) {
            api.getLists(repository);
          }
        }
        break;
      }

      case 'refreshAfterFetch': {
        // Handle refresh after fetch - always skip cache to show new posts
        webLog('debug', '[ViewList] Refreshing after fetch with cache skip');
        loadData(true);
        break;
      }

      case 'postCreated':
      case 'commitCreated':
        // Refresh when a new post/comment/repost/quote is created
        loadData(true); // Skip cache to get fresh data
        break;
      case 'listDeleted':
        // List was deleted, navigate back to repository view
        api.openView('repository', 'My Repository', { activeTab: 'lists' });
        break;

    }
  }

  // Note: Refresh is handled by the message handler 'refresh' case above
  // The reactive block was causing duplicate API calls

  // Week navigation functions
  function goToPreviousWeek() {
    weekOffset--;
    loadWeekData();
  }

  function goToNextWeek() {
    if (weekOffset < 0) { // Can't go to future weeks
      weekOffset++;
      loadWeekData();
    }
  }

  function loadWeekData(skipCache = false) {
    isLoadingWeek = true;
    error = '';

    // For remote repositories, we need to load lists first
    if (repository) {
      // Clear any existing timeout
      if (remoteListsTimeout) {
        clearTimeout(remoteListsTimeout);
        remoteListsTimeout = null;
      }

      // Skip if we're already waiting for lists to avoid duplicate requests
      if (waitingForRemoteLists) {
        webLog('debug', '[ViewList] Already waiting for remote lists, skipping duplicate request');
        return;
      }

      waitingForRemoteLists = true;
      webLog('debug', '[ViewList] Loading remote repository list for week navigation');

      // Set timeout in case list fetching never completes
      remoteListsTimeout = setTimeout(() => {
        if (waitingForRemoteLists) {
          waitingForRemoteLists = false;
          remoteListsTimeout = null;
          error = 'Timeout waiting for remote repository lists. Please try again.';
          webLog('error', '[ViewList] Timeout waiting for remote lists during week navigation');
          isLoadingWeek = false;
        }
      }, 10000); // 10 second timeout

      // Request lists, which will trigger loadPostsData when they arrive
      api.getLists(repository);
    } else {
      // For workspace lists, load posts directly
      loadPostsData(skipCache);
    }
  }

  function loadPostsData(skipCache = false) {
    // Request both tabs data in parallel for current week range
    const weekStart = getWeekStart(weekOffset);
    const weekEnd = getWeekEnd(weekOffset);
    const since = weekStart.toISOString();
    const until = weekEnd.toISOString();

    // Construct proper scope based on whether this is a remote repository list
    // Note: repository and baseRepository are const, so recompute here to ensure we have current values
    const currentBaseRepository = repository ? repository.split('#')[0] : null;

    webLog('debug', '[ViewList] loadPostsData called:', { repository, baseRepository: currentBaseRepository, listId });

    if (repository && currentBaseRepository) {
      // Remote repository list: use combined repository/list scope format
      // This format is parsed by the backend to filter posts from the repository's list
      const scope = `repository:${currentBaseRepository}/list:${listId}`;
      webLog('debug', '[ViewList] Using remote repository list scope:', scope);
      api.getPosts({ since, until, scope, types: ['post', 'quote', 'repost'], skipCache }, 'posts-tab');
      api.getPosts({ since, until, scope, types: ['comment'], skipCache }, 'replies-tab');
    } else {
      // Local workspace list: use list ID directly
      webLog('debug', '[ViewList] Using workspace list scope with listId:', listId);
      api.getPosts({ since, until, listId, types: ['post', 'quote', 'repost'], skipCache }, 'posts-tab');
      api.getPosts({ since, until, listId, types: ['comment'], skipCache }, 'replies-tab');
    }
  }

  function loadData(skipCache = false) {
    webLog('debug', `[ViewList] loadData called for ${repository ? 'other' : 'workspace'} repository, skipCache:`, skipCache);
    // Load posts from cache only - no remote fetching on mount
    // User must click week navigation or sync button to fetch from remotes
    error = '';
    isLoadingWeek = true;

    // Pass skipCache parameter through to loadPostsData
    loadPostsData(skipCache);

    // Always refresh list metadata to ensure name and properties are current
    api.getLists(repository);
  }

  function handleFetchUpdates() {
    fetchingUpdates = true;
    api.fetchListRepositories(listId, repository);
  }

  function handleViewRepository(repository: string) {
    // Use the original repository string directly to preserve exact format
    try {
      // Extract just the base URL for display name (before #branch:)
      const baseUrl = repository.split('#')[0];
      api.openView('repository', gitHost.getDisplayName(baseUrl), {
        repository: repository  // Pass the original repository string with branch
      });
    } catch (e) {
      console.error('Invalid repository identifier:', repository, e);
    // Note: Could add visual error indication here if needed
    }
  }

  function handleTabChange(event: CustomEvent) {
    activeTab = event.detail.tabId;
  }

  function handleAddRepository() {
    if (!newRepositoryUrl.trim() || !listId) {return;}

    isAddingRepository = true;

    // Don't normalize here - let the backend handle it to ensure consistency
    // The backend will use normalizeRepoUrl which removes .git suffix
    const repositoryUrl = newRepositoryUrl.trim();

    api.addRepository(listId, repositoryUrl, newRepositoryBranch.trim() || undefined);

    // Reset form
    newRepositoryUrl = '';
    newRepositoryBranch = '';
    showCustomBranch = false;
    isAddingRepository = false;
  }

  function handleRemoveRepository(repository: string) {
    if (!listId) {return;}
    api.removeRepository(listId, repository);
  }

  function handleSyncList() {
    if (!listId) {return;}
    fetchingUpdates = true;
    api.syncList(listId);
    api.fetchListRepositories(listId, repository);
  }

  function handleUnfollowList() {
    if (!listId) {return;}
    api.unfollowList(listId);
  }

  function handleFollowList() {
    if (!listId || !baseRepository) {return;}
    api.followList(baseRepository, listId);
  }
  function handleDeleteList() {
    if (!listId || !list) {return;}
    api.deleteList(listId, list.name);
  }

  function startRename() {
    if (!list) {return;}
    editingName = true;
    newListName = list.name;
  }

  function cancelRename() {
    editingName = false;
    newListName = '';
  }

  function handleRenameList(event: SubmitEvent) {
    event.preventDefault();
    if (!listId || !list) {return;}
    const trimmedName = newListName.trim();
    if (!trimmedName || trimmedName === list.name) {
      cancelRename();
      return;
    }
    isRenaming = true;
    api.renameList(listId, trimmedName);
  }

  function handleRenameKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      cancelRename();
    }
  }
</script>

<div class="view-container">
  {#if error}
    <div class="error">
      <p>⚠️ {error}</p>
      <button class="btn" on:click={loadData}>Retry</button>
    </div>
  {:else if list}
    <!-- Header -->
    <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar border-r">
      <!-- Main Row -->
      <div class="grid gap-2 items-center" style="grid-template-columns: auto 1fr auto;">
        <!-- Column 1: Icon -->
        <div>
          <svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg" class="w-6 h-6">
            <circle fill="currentColor" cx="3" cy="4" r="1"/>
            <rect fill="currentColor" x="6" y="3.5" width="8" height="1"/>
            <circle fill="currentColor" cx="3" cy="8" r="1"/>
            <rect fill="currentColor" x="6" y="7.5" width="8" height="1"/>
            <circle fill="currentColor" cx="3" cy="12" r="1"/>
            <rect fill="currentColor" x="6" y="11.5" width="8" height="1"/>
          </svg>
        </div>

        <!-- Column 2: Title -->
        <div class="min-w-0 flex items-center gap-2">
          {#if editingName}
            <form on:submit={handleRenameList} class="flex items-center gap-2 flex-1">
              <input
                type="text"
                bind:value={newListName}
                on:keydown={handleRenameKeydown}
                class="flex-1 text-lg font-bold"
                placeholder="List name"
                disabled={isRenaming}
              />
              <button
                type="submit"
                class="btn sm"
                disabled={!newListName.trim() || isRenaming}
                title="Save"
              >
                <span class="codicon codicon-check"></span>
              </button>
              <button
                type="button"
                class="btn sm"
                on:click={cancelRename}
                disabled={isRenaming}
                title="Cancel"
              >
                <span class="codicon codicon-close"></span>
              </button>
            </form>
          {:else}
            <h1 class="truncate">{list.name}</h1>
            {#if !repository && !list.source}
              <button
                class="btn sm"
                on:click={startRename}
                title="Rename list"
                disabled={isRenaming}
              >
                <span class="codicon codicon-edit"></span>
              </button>
            {/if}
          {/if}
        </div>

        <!-- Column 3: Navigation + Actions -->
        <div class="flex items-center gap-2">
          <!-- Time Navigation (grouped) -->
          <DateNavigation
            offset={weekOffset}
            label={weekLabel}
            loading={isLoadingWeek}
            onPrevious={goToPreviousWeek}
            onNext={goToNextWeek}
            onRefresh={weekOffset === 0 && list?.repositories.length > 0
              ? (list?.source ? handleSyncList : handleFetchUpdates)
              : undefined}
            refreshLoading={fetchingUpdates}
          />

          <!-- Action Buttons -->
          {#if list.source && !repository}
            <button
              class="btn"
              on:click={handleUnfollowList}
              title="Stop following this list"
            >
              <span class="codicon codicon-close"></span>
            </button>
            <button
              class="btn"
              on:click={handleDeleteList}
              title="Delete list"
            >
              <span class="codicon codicon-trash"></span>
            </button>
          {:else if repository && isRemoteListFollowed}
            <button
              class="btn"
              on:click={handleUnfollowList}
              title="Stop following this list"
            >
              <span class="codicon codicon-close"></span>
            </button>
          {:else if repository && !isRemoteListFollowed}
            <button
              class="btn btn-primary"
              on:click={handleFollowList}
              title="Follow this list"
            >
              <span class="codicon codicon-add"></span>
              Follow
            </button>
          {:else if !repository}
            <button
              class="btn"
              on:click={handleDeleteList}
              title="Delete list"
            >
              <span class="codicon codicon-trash"></span>
            </button>
          {/if}
        </div>
      </div>

      <!-- Bottom Row: URL + Meta (concatenated, right-aligned) -->
      <div class="flex justify-left items-center mt-1">
        <div class="text-sm text-muted italic whitespace-nowrap">
          {#if listReference}
            {@const webUrl = gitHost.getWebUrl(baseRepository)}
            {@const fullUrl = webUrl ? `${webUrl}#list:${listId}` : null}
            {#if fullUrl}
              <a href={fullUrl} class="hover-underline text-muted">{listReference}</a>
            {:else}
              <span>{listReference}</span>
            {/if}
            <span>, </span>
          {:else if list.source}
            {@const sourceWebUrl = gitHost.getWebUrl(list.source)}
            {#if sourceWebUrl}
              <a href={sourceWebUrl} class="hover-underline text-muted">{list.source}</a>
            {:else}
              <span>{list.source}</span>
            {/if}
          {/if}
          {#if fetchTimeDisplay}
            {#if list?.source}
              <span> • </span>
            {/if}
            <span class="cursor-help" title={`Fetched ${lastFetchTime ? new Date(lastFetchTime).toLocaleString() : ''}`}>
              Fetched {fetchTimeDisplay}
            </span>
          {/if}
        </div>
      </div>

      <Tabs {tabs} {activeTab} on:change={handleTabChange} />
    </div>

    {#if activeTab === 'posts'}
      <div class="posts-section">
        {#if postsTabData.length > 0}
          <div class="flex flex-col gap-2 -ml-4">
            {#each postsTabData as post (post.id)}
              <PostCard post={post} />
            {/each}
          </div>
        {:else}
          <div class="no-posts">
            <p>No posts yet from repositories in this list.</p>
            {#if list.repositories.length > 0}
              <p class="mt-4 text-sm">
                The remote repositories may need to be fetched first.
                Use the "Fetch Updates" button above to sync remote posts.
              </p>
            {/if}
          </div>
        {/if}
      </div>
    {:else if activeTab === 'replies'}
      <div class="posts-section">
        {#if repliesTabData.length > 0}
          <div class="flex flex-col gap-2 -ml-4">
            {#each repliesTabData as post (post.id)}
              <PostCard post={post} />
            {/each}
          </div>
        {:else}
          <div class="no-posts">
            <p>No replies yet from repositories in this list.</p>
            {#if list.repositories.length > 0}
              <p class="mt-4 text-sm">
                The remote repositories may need to be fetched first.
                Use the "Fetch Updates" button above to sync remote replies.
              </p>
            {/if}
          </div>
        {/if}
      </div>
    {:else if activeTab === 'repositories'}
      <div class="section">
        <!-- Add Repository Form (only show for workspace lists) -->
        {#if !repository}
          <div class="mb-4">
            <div class="flex flex-wrap gap-2 items-end">
              <div class="flex-1">
                <label for="repo-url" class="block text-sm font-medium mb-1">Repository URL</label>
                <input
                  id="repo-url"
                  type="text"
                  placeholder="https://github.com/user/repo"
                  bind:value={newRepositoryUrl}
                  disabled={isAddingRepository}
                  class="w-full"
                />
              </div>

              {#if showCustomBranch}
                <div class="flex-1">
                  <label for="repo-branch" class="block text-sm font-medium mb-1">Branch</label>
                  <input
                    id="repo-branch"
                    type="text"
                    placeholder="main"
                    bind:value={newRepositoryBranch}
                    disabled={isAddingRepository}
                    class="w-full"
                  />
                </div>
              {/if}

              <div class="flex items-end gap-2">
                <button
                  class="btn"
                  on:click={() => showCustomBranch = !showCustomBranch}
                  disabled={isAddingRepository}
                  title={showCustomBranch ? 'Auto-detect branch' : 'Specify custom branch'}
                  class:active={showCustomBranch}
                >
                  <span class="codicon codicon-gear"></span>
                </button>
              </div>

              <button
                class="btn primary"
                on:click={handleAddRepository}
                disabled={!newRepositoryUrl.trim()
                  || !gitMsgUrl.validate(newRepositoryUrl.trim())
                  || isAddingRepository}
              >
                <span class="codicon codicon-add"></span>
                {isAddingRepository ? 'Saving...' : 'Follow Repository'}
              </button>
            </div>

            {#if showCustomBranch}
              <div class="text-xs text-muted mt-1">
                Leave empty to auto-detect the default branch
              </div>
            {:else}
              <div class="text-xs text-muted mt-1">
                Branch will be auto-detected from the repository
              </div>
            {/if}
          </div>
        {/if}

        {#if list.repositories.length > 0}
          <div class="flex flex-col gap-2">
            {#each list.repositories as repoString}
              {@const repo = gitMsgRef.parseRepositoryId(repoString)}
              {#if repo}
                <div class="card p-3 hover cursor-pointer" on:click={() => handleViewRepository(repoString)} role="button" tabindex="0" on:keydown={(e) => e.key === 'Enter' && handleViewRepository(repoString)}>
                  <div class="flex items-center gap-3">
                    <Avatar
                      type="repository"
                      identifier={gitMsgUrl.normalize(repo.repository)}
                      name={gitHost.getDisplayName(repo.repository)}
                      size={40}
                    />
                    <div class="flex-1 min-w-0">
                      <div class="font-bold truncate">
                        {gitHost.getDisplayName(repo.repository)}
                      </div>
                      <div class="flex items-center text-sm text-muted">
                        <span>{repo.repository}#branch:{repo.branch}</span>
                      </div>
                    </div>

                    {#if !repository}
                      <button
                        class="btn"
                        on:click|stopPropagation={() => handleRemoveRepository(repo.repository)}
                        title="Remove repository from list"
                      >
                        <span class="codicon codicon-trash"></span>
                      </button>
                    {/if}
                  </div>
                </div>
              {/if}
            {/each}
          </div>
        {:else}
          <div class="empty text-center py-8">
            <span class="codicon codicon-repo text-4xl mb-3 block opacity-50"></span>
            <p class="text-muted mb-2">No repositories in this list yet</p>
            <p class="text-sm text-muted">Add repositories to this list to see their posts</p>
          </div>
        {/if}
      </div>
    {/if}
  {/if}
</div>
