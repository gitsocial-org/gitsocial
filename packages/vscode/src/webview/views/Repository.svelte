<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';
  import Tabs from '../components/Tabs.svelte';
  import ListManager from '../components/ListManager.svelte';
  import ListCreateForm from '../components/ListCreateForm.svelte';
  import Avatar from '../components/Avatar.svelte';
  import Dialog from '../components/Dialog.svelte';
  import DateNavigation from '../components/DateNavigation.svelte';
  import { skipCacheOnNextRefresh } from '../stores';
  import type { Post, List, Follower } from '@gitsocial/core';
  import { gitHost, gitMsgUrl, gitMsgRef } from '@gitsocial/core/client';
  import { formatRelativeTime, useEventDrivenTimeUpdates, getWeekStart, getWeekEnd, getWeekLabel, format30DayRange } from '../utils/time';
  import type { Repository } from '@gitsocial/core/client';
  import type { LogEntry } from '../../handlers/types';

  let postsTabData: Post[] = [];
  let repliesTabData: Post[] = [];
  let contextPosts: Map<string, Post> = new Map();

  // Week navigation state
  let rangeOffset = 0; // 0 = current period, -1 = previous period, etc.
  // For workspace: 30-day periods. For other repos: weekly periods
  let isLoadingRange = false;

  // Reset rangeOffset when repository changes (important when component is reused)
  let previousRepository = '';
  $: if (repository !== previousRepository) {
    previousRepository = repository;
    rangeOffset = 0;
  }

  // All posts for context lookup - combine arrays with context posts
  $: allPostsForContext = (() => {
    const allPosts = [...postsTabData, ...repliesTabData];
    // Add context posts that aren't already in the arrays
    for (const [id, post] of contextPosts) {
      if (!allPosts.find(p => p.id === id)) {
        allPosts.push(post);
      }
    }
    return allPosts;
  })();
  let error: string | null = null;
  let repository = ''; // Single repository reference (may include branch)
  let repositoryStatus: Repository | null = null;

  // Computed properties from repositoryStatus (single source of truth)
  // For My Repository view, we know it's workspace if no repository param is provided
  $: isWorkspace = !repository || repositoryStatus?.type === 'workspace';
  $: hasOriginRemote = repositoryStatus?.hasOriginRemote || false;
  $: originUrl = repositoryStatus?.originUrl || null;
  $: canPush = isWorkspace && hasOriginRemote;
  $: isFollowedRepo = !isWorkspace && repositoryStatus?.lists && repositoryStatus.lists.length > 0;
  $: showFetchButton = rangeOffset === 0 && (!isWorkspace || hasOriginRemote);

  // Parse repository to extract URL and branch (strict - no fallbacks)
  $: repositoryParsed = (() => {
    if (!repository) {return null;}
    try {
      return gitMsgRef.parseRepositoryId(repository);
    } catch (e) {
      console.error('Failed to parse repository:', e);
      return null;
    }
  })();
  $: repositoryUrl = repositoryParsed?.repository || repository;
  $: repositoryBranch = repositoryParsed?.branch || (isWorkspace ? repositoryStatus?.branch : null) || null;
  let activeTab: 'posts' | 'comments' | 'lists' | 'followers' | 'log' = 'posts';
  let postsRequestId: string | null = null;
  let repliesRequestId: string | null = null;
  let lastFetchTime: Date | null = null;
  let currentTime = Date.now();
  let cleanupTimeUpdates: (() => void) | null = null;

  // Computed values for current range
  $: currentRangeStart = isWorkspace
    ? new Date(Date.now() - (30 * (1 - rangeOffset) * 24 * 60 * 60 * 1000))
    : getWeekStart(rangeOffset);
  $: currentRangeEnd = isWorkspace
    ? new Date(Date.now() - (30 * (-rangeOffset) * 24 * 60 * 60 * 1000))
    : getWeekEnd(rangeOffset);
  $: rangeLabel = isWorkspace
    ? (rangeOffset === 0 ? 'Last 30 days' : format30DayRange(currentRangeStart, currentRangeEnd))
    : getWeekLabel(rangeOffset);
  let pushingToRemote = false;
  let fetchingRepository = false;

  // Follow dialog state
  let showFollowDialog = false;
  let isFollowDialogSubmitting = false;
  let availableLists: List[] = [];
  let selectedListIds: string[] = [];
  let showCreateNew = false;
  let firstListName = 'Default';
  let submitCreateListForm: (() => void) | null = null;

  // Reactive fetch time display that updates with currentTime
  $: fetchTimeDisplay = lastFetchTime && currentTime ? formatRelativeTime(lastFetchTime) : '';

  // Lists tab state
  let lists: List[] = [];

  // Log tab state
  let logTabData: LogEntry[] = [];
  let logRequestId: string | null = null;
  let typeFilter = 'all';
  let logsLoaded = false;

  // Followers tab state
  let followersTabData: Follower[] = [];
  let followersRequestId: string | null = null;
  let followersLoaded = false;

  // Total unpushed counts (accurate from all posts, not just current week)
  let totalUnpushedCounts = { posts: 0, comments: 0, total: 0 };

  // Calculate unpushed counts for tabs
  $: unpushedPostsCount = postsTabData.filter(p => p.display.isUnpushed).length;
  $: unpushedRepliesCount = repliesTabData.filter(p => p.display.isUnpushed).length;
  $: unpushedListsCountFromArray = lists.filter(l => l.isUnpushed).length;

  $: tabs = isWorkspace ? [
    { id: 'posts', label: 'Posts', count: postsTabData.length, unpushedCount: unpushedPostsCount },
    { id: 'comments', label: 'Replies', count: repliesTabData.length, unpushedCount: unpushedRepliesCount },
    { id: 'lists', label: 'Lists', count: lists.length, unpushedCount: unpushedListsCountFromArray },
    { id: 'followers', label: 'Followers', count: followersLoaded ? followersTabData.length : undefined },
    { id: 'log', label: 'Log' }
  ] : [
    { id: 'posts', label: 'Posts', count: postsTabData.length, unpushedCount: unpushedPostsCount },
    { id: 'comments', label: 'Replies', count: repliesTabData.length, unpushedCount: unpushedRepliesCount },
    { id: 'lists', label: 'Lists', count: lists.length, unpushedCount: unpushedListsCountFromArray },
    { id: 'log', label: 'Log' }
  ];

  // Group replies by their original thread
  function groupRepliesByThread(replies: Post[]): Map<string, Post[]> {
    const threadMap = new Map<string, Post[]>();

    for (const reply of replies) {
      // Group by original post ID (the thread root)
      const threadId = reply.originalPostId || reply.id;
      if (!threadMap.has(threadId)) {
        threadMap.set(threadId, []);
      }
      const threadReplies = threadMap.get(threadId);
      if (threadReplies) {
        threadReplies.push(reply);
      }
    }

    // Sort each thread's replies by timestamp (oldest first within thread)
    for (const [, threadReplies] of threadMap) {
      threadReplies.sort((a, b) => {
        const aTime = a.timestamp instanceof Date ? a.timestamp.getTime() : new Date(a.timestamp).getTime();
        const bTime = b.timestamp instanceof Date ? b.timestamp.getTime() : new Date(b.timestamp).getTime();
        return aTime - bTime; // Oldest first within thread
      });
    }

    return threadMap;
  }

  // Message handler function
  function handleMessage(event: MessageEvent) {
    const message = event.data;

    switch (message.type) {
      case 'posts':
        // Route to appropriate array based on request ID
        if (message.requestId === 'posts-tab') {
          // Filter to ensure we only show posts, quotes, and reposts in the Posts tab
          postsTabData = (message.data || []).filter(p =>
            p.type === 'post' || p.type === 'quote' || p.type === 'repost'
          );
          isLoadingRange = false;
        } else if (message.requestId === 'replies-tab') {
          // Filter to ensure we only show comments in the Replies tab, sorted newest first (social media style)
          repliesTabData = (message.data || [])
            .filter(p => p.type === 'comment')
            .sort((a, b) => {
              const aTime = a.timestamp instanceof Date ? a.timestamp.getTime() : new Date(a.timestamp).getTime();
              const bTime = b.timestamp instanceof Date ? b.timestamp.getTime() : new Date(b.timestamp).getTime();
              return bTime - aTime; // Newest first
            });
          isLoadingRange = false;

          // Fetch parent/original posts for context
          fetchContextPosts(repliesTabData);
        } else if (message.requestId?.startsWith('context-posts-')) {
          // Handle context posts response
          const posts = message.data || [];
          for (const post of posts) {
            contextPosts.set(post.id, post);
          }
          // Trigger reactivity
          contextPosts = contextPosts;
        }
        error = null;
        break;

      case 'logs':
        if (message.requestId === logRequestId) {
          logTabData = message.data || [];
          logsLoaded = true;
        }
        break;

      case 'followers':
        if (message.requestId === followersRequestId) {
          followersTabData = message.data || [];
          followersLoaded = true;
        }
        break;

      case 'error':
        // Only handle errors for our requests
        if (message.requestId === postsRequestId ||
          message.requestId === repliesRequestId ||
          message.requestId === logRequestId) {
          // Only show error when we're on the posts, comments, or log tab
          if (activeTab !== 'lists') {
            error = message.message || 'Failed to load data';
          }
          isLoadingRange = false;
        }
        break;

      case 'lists':
        // Check if this is for the workspace lists (follow dialog or follow status display)
        if (message.requestId === 'workspace-lists-for-follow' || message.requestId === 'workspace-lists-for-display') {
          availableLists = message.data || [];
        } else {
          lists = message.data || [];
        }
        break;

      case 'listCreated':
        // If we're creating from the follow dialog, handle it specially
        if (message.requestId === 'create-list-for-follow') {
          // Refresh workspace lists and continue with follow
          api.getListsWithId('', 'workspace-lists-for-follow');
        } else {
          // Normal list refresh
          api.getLists(repository);
        }
        break;

      case 'listDeleted':
      case 'listUnfollowed':
      case 'repositoryRemoved':
        // Refresh lists after any list operation
        api.getLists(repository);
        // Refresh repository status to update follow status in UI
        if (message.type === 'repositoryRemoved') {
          api.checkRepositoryStatus(repository);
        }
        break;

      case 'repositoryAdded':
        // Repository was successfully added
        if (message.requestId === 'follow-repository') {
          isFollowDialogSubmitting = false;
          showFollowDialog = false;
          // Refresh repository status to update follow status in UI
          api.checkRepositoryStatus(repository);
        }
        break;

      case 'unpushedCounts':
        // Update total unpushed counts for accurate Push button display
        totalUnpushedCounts = message.data || { posts: 0, comments: 0, total: 0 };
        break;

      case 'repositoryStatus':
        repositoryStatus = message.data as Repository;
        // Use repository's fetch time if available
        if (repositoryStatus.lastFetchTime) {
          lastFetchTime = new Date(repositoryStatus.lastFetchTime);
        }
        // Status received - user initiated load should have already happened
        break;

      case 'fetchProgress':
        if (message.data?.status === 'fetching') {
          fetchingRepository = true;
        } else if (message.data?.status === 'completed') {
          fetchingRepository = false;
          // Update fetch time when remote fetch completes successfully
          lastFetchTime = new Date();
          // Refresh posts after fetch completes - skip cache to get fresh data
          loadRangeData(true);
        } else if (message.data?.status === 'error') {
          fetchingRepository = false;
        }
        break;

      // Removed 'repositories' case - no longer needed, all info comes from repositoryStatus

      case 'pushProgress':
        if (message.data?.status === 'pushing') {
          pushingToRemote = true;
        }
        break;

      case 'pushCompleted':
        pushingToRemote = false;
        // Refresh to update unpushed counts, skip cache to get fresh data after push
        loadRangeData(true);
        // Also refresh total unpushed counts
        api.getUnpushedCounts();
        break;

      case 'refresh': {
        const scopes = message.scope || ['all'];
        const operation = message.operation;
        const skipCache = message.skipCache || $skipCacheOnNextRefresh;

        if (skipCache && $skipCacheOnNextRefresh) {
          skipCacheOnNextRefresh.set(false);
        }

        // Handle list rename operation specially
        if (operation === 'listRenamed') {
          // List renames only affect the Lists tab - just refresh lists metadata
          api.getLists(repository);
          // Don't reload posts/replies
          break;
        }

        // Standard scope-based refresh
        if (scopes.includes('all')) {
          // Reload everything
          loadRangeData(skipCache);
          if (isWorkspace) {
            api.getUnpushedCounts();
          }
        } else {
          // Selective reload based on scopes
          if (scopes.includes('posts')) {
            loadRangeData(skipCache);
            if (isWorkspace) {
              api.getUnpushedCounts();
            }
          }
          if (scopes.includes('lists')) {
            api.getLists(repository);
          }
        }
        break;
      }

      case 'refreshAfterFetch': {
        // Handle refresh after fetch - always skip cache to show new posts
        loadRangeData(true);
        break;
      }

      case 'updateViewParams':
        // Handle param updates when view is already open
        if (message.data?.activeTab) {
          activeTab = message.data.activeTab;
        }
        break;
    }
  }

  onMount(() => {
    // Clear any stale log data when mounting
    logTabData = [];
    logsLoaded = false;

    // CRITICAL: Reset range offset when mounting a new repository view
    rangeOffset = 0;

    // Get repository info from params
    const params = (window as { viewParams?: { repository?: string; path?: string; activeTab?: 'posts' | 'comments' | 'lists' | 'followers' | 'log' } }).viewParams;
    repository = params?.repository || params?.path || '';

    // Set initial active tab from params if provided
    if (params?.activeTab) {
      activeTab = params.activeTab;
    }

    // Don't set initial fetch time - wait for backend to provide it

    // Determine repository type and load posts directly
    if (repository && params?.repository) {
      // Remote repository - load posts directly without access control
      loadRangeData();
      // Also check repository status to get fetch time
      api.checkRepositoryStatus(repository);
      // Load lists for other repository to show count in tab
      api.getLists(repository);
      // Load workspace lists to enable "Following in" display with unfollow buttons
      api.getListsWithId('', 'workspace-lists-for-display');
    } else {
      // Workspace repository - clear repository identifier
      repository = ''; // Workspace repos don't have a repository identifier
      loadRangeData();
      // Check repository status for workspace too (includes origin info)
      api.checkRepositoryStatus('.');
      // Load lists for workspace to show count in tab
      api.getLists(repository);
      // Get accurate unpushed counts for workspace
      api.getUnpushedCounts();
    }

    // Listen for messages from extension
    window.addEventListener('message', handleMessage);

    // Set up event-driven time updates
    cleanupTimeUpdates = useEventDrivenTimeUpdates(() => {
      currentTime = Date.now();
    });
  });

  onDestroy(() => {
    // Clean up event listener
    window.removeEventListener('message', handleMessage);
    // Clean up time updates
    if (cleanupTimeUpdates) {
      cleanupTimeUpdates();
    }
  });

  // React to refresh trigger
  async function fetchContextPosts(replies: Post[]) {
    // Collect all needed parent and original post IDs
    const neededIds = new Set<string>();
    for (const reply of replies) {
      if (reply.parentCommentId) {
        neededIds.add(reply.parentCommentId);
      }
      if (reply.originalPostId) {
        neededIds.add(reply.originalPostId);
      }
    }

    // Filter out IDs we already have
    const existingIds = new Set([...postsTabData, ...repliesTabData].map(p => p.id));
    const missingIds = Array.from(neededIds).filter(id => !existingIds.has(id));

    if (missingIds.length > 0) {
      // Fetch missing posts using byId scope
      const contextRequestId = `context-posts-${Date.now()}`;
      api.getPosts({
        scope: `byId:${missingIds.join(',')}`,
        skipCache: false
      }, contextRequestId);
    }
  }

  // Note: Refresh is handled by the message handler 'refresh' case above
  // The reactive block was causing duplicate API calls

  // Week navigation functions
  function goToPreviousRange() {
    rangeOffset--;
    loadRangeData();
  }

  function goToNextRange() {
    if (rangeOffset < 0) { // Can't go to future periods
      rangeOffset++;
      loadRangeData();
    }
  }

  function loadRangeData(skipCache = false) {
    isLoadingRange = true;
    error = null;
    // Clear context posts on refresh
    contextPosts.clear();
    contextPosts = contextPosts; // Trigger reactivity

    // Load posts and replies tabs data in parallel and track request IDs
    postsRequestId = 'posts-tab';
    repliesRequestId = 'replies-tab';

    // CRITICAL: Compute isWorkspace locally - reactive property may not have updated yet during initialization
    // Only workspace has 30-day periods, all others (lists and external repos) use weekly periods
    const isWorkspaceForRange = !repository;

    // Calculate dates based on range type and offset
    const rangeStart = isWorkspaceForRange
      ? new Date(Date.now() - (30 * (1 - rangeOffset) * 24 * 60 * 60 * 1000))  // 30-day periods with offset
      : getWeekStart(rangeOffset);
    const rangeEnd = isWorkspaceForRange
      ? new Date(Date.now() - (30 * (-rangeOffset) * 24 * 60 * 60 * 1000))  // End of period with offset
      : getWeekEnd(rangeOffset);
    const since = rangeStart.toISOString();
    const until = rangeEnd.toISOString();

    // Update the time range label - no longer needed as we use computed properties
    // timeRangeLabel is now computed directly from weekLabel and dateRangeLabel

    // Determine scope based on whether repository is a filesystem path or remote URL
    const isRemoteRepository = repository && gitMsgUrl.validate(repository);
    const scope = isRemoteRepository ? `repository:${repository}` : 'repository:my';

    // Only pass repository for my repos, for other repos it's included in the scope
    const postsOptions = isRemoteRepository
      ? { since, until, types: ['post', 'quote', 'repost'], scope, skipCache }
      : { repository, since, until, types: ['post', 'quote', 'repost'], scope, skipCache };
    const repliesOptions = isRemoteRepository
      ? { since, until, types: ['comment'], scope, skipCache }
      : { repository, since, until, types: ['comment'], scope, skipCache };
    api.getPosts(postsOptions, postsRequestId);
    api.getPosts(repliesOptions, repliesRequestId);

    // Reset log state - will be loaded lazily when tab is clicked
    logsLoaded = false;
    logTabData = [];

    // If Log tab is active, reload logs with the new date range
    if (activeTab === 'log') {
      loadLogsData();
      logsLoaded = true; // Mark as loaded so it doesn't reload when switching back
    }

    // Load lists to show count immediately
    // For other repositories, this fetches lists from that repository
    api.getLists(repository);
  }

  function handleCreatePost() {
    api.openView('createPost', 'New Post');
  }

  function handlePush() {
    if (!canPush) {return;}
    api.pushToRemote('origin');
  }

  function handleFetch() {
    fetchingRepository = true;
    if (isWorkspace) {
      // Pass empty string for workspace
      api.fetchUpdates('');
    } else {
      // Check that we have a valid repository identifier
      if (!repository || !repository.includes('#branch:')) {
        fetchingRepository = false;
        return;
      }
      api.fetchUpdates(repository);
    }
  }

  function handleFollow() {
    if (!repository) {return;}

    // Show the follow dialog
    showFollowDialog = true;

    // Load workspace lists if not already loaded
    if (availableLists.length === 0) {
      api.getListsWithId('', 'workspace-lists-for-follow');
    }
  }

  function handleFollowSubmit(event: CustomEvent<{ listIds: string[] }>) {
    isFollowDialogSubmitting = true;
    for (const listId of event.detail.listIds) {
      api.addRepositoryWithId(listId, repository, 'follow-repository', undefined);
    }
  }

  function handleCreateAndFollow(event: CustomEvent<{ id: string; name: string }>) {
    isFollowDialogSubmitting = true;
    api.createListWithId(event.detail.id, event.detail.name, 'create-list-for-follow');
    if (typeof window !== 'undefined' && window.sessionStorage) {
      window.sessionStorage.setItem('pendingFollowList', event.detail.id);
      window.sessionStorage.setItem('pendingFollowRepo', repository);
    }
  }

  // Check for pending follow after list creation
  $: if (availableLists.length > 0 && typeof window !== 'undefined' && window.sessionStorage) {
    const pendingList = window.sessionStorage.getItem('pendingFollowList');
    const pendingRepo = window.sessionStorage.getItem('pendingFollowRepo');
    if (pendingList && pendingRepo) {
      window.sessionStorage.removeItem('pendingFollowList');
      window.sessionStorage.removeItem('pendingFollowRepo');
      // Now add the repository to the newly created list
      api.addRepositoryWithId(pendingList, pendingRepo, 'follow-repository', undefined);
    }
  }

  function handleTabChange(event: CustomEvent) {
    activeTab = event.detail.tabId;

    if (activeTab === 'lists') {
      // Load lists - for other repos, this fetches lists from that repository
      // For workspace repos, this gets local lists
      api.getLists(repository);
    } else if (activeTab === 'followers' && !followersLoaded && isWorkspace) {
      // Lazy load followers when tab is clicked for the first time (only for workspace)
      loadFollowersData();
    } else if (activeTab === 'log' && !logsLoaded) {
      // Lazy load logs when log tab is clicked for the first time
      loadLogsData();
      logsLoaded = true; // Mark as loaded to prevent duplicate calls
    }
  }

  function loadFollowersData() {
    followersRequestId = `followers-${Date.now()}`;
    api.getFollowers(followersRequestId);
  }

  function loadLogsData() {
    logRequestId = `logs-${Date.now()}`;
    // Use the current range for logs (same calculation as loadRangeData)
    const rangeStart = isWorkspace
      ? new Date(Date.now() - (30 * (1 - rangeOffset) * 24 * 60 * 60 * 1000))
      : getWeekStart(rangeOffset);
    const rangeEnd = isWorkspace
      ? new Date(Date.now() - (30 * (-rangeOffset) * 24 * 60 * 60 * 1000))
      : getWeekEnd(rangeOffset);
    const since = rangeStart.toISOString();
    const until = rangeEnd.toISOString();

    // Determine appropriate scope based on repository type
    const isRemoteRepository = repository && gitMsgUrl.validate(repository);
    const scope = isRemoteRepository ? `repository:${repository}` : 'repository:my';

    const logOptions = isRemoteRepository
      ? { since, until, scope }
      : { repository, since, until, scope };

    api.getLogs(logOptions, logRequestId);
  }

  function handleCreateList(event: CustomEvent<{ id: string; name: string }>) {
    api.createList(event.detail.id, event.detail.name);
  }

  function handleDeleteList(event: CustomEvent<{ list: List }>) {
    api.deleteList(event.detail.list.id, event.detail.list.name);
  }

  function handleViewList(event: CustomEvent<{ list: List; activeTab?: string }>) {
    const params: { listId: string; list: List; repository?: string; activeTab?: string } = {
      listId: event.detail.list.id,
      list: event.detail.list
    };

    // Add repository context for other repositories
    if (!isWorkspace && repository) {
      params.repository = repository;
    }

    if (event.detail.activeTab) {
      params.activeTab = event.detail.activeTab;
    }
    api.openView('viewList', event.detail.list.name, params);
  }

  function handleFollowListCard(event: CustomEvent<{ list: List; repository?: string }>) {
    const sourceRepo = event.detail.repository || repository;
    const sourceListId = event.detail.list.id;

    // Send follow request with optional target list ID (use same as source by default)
    api.followList(sourceRepo, sourceListId);
  }
  function handleUnfollowListCard(event: CustomEvent<{ list: List }>) {
    api.unfollowList(event.detail.list.id);
  }

  function handleSettings() {
    api.openView('settings', 'Settings');
  }

  function handleUnfollowFromList(listId: string) {
    if (!repository) {return;}
    api.removeRepository(listId, repository);
  }

  function handleOpenList(list: List) {
    const params: { listId: string; list: List; repository?: string } = {
      listId: list.id,
      list: list
    };
    api.openView('viewList', list.name, params);
  }

  // Log table helpers
  function isClickableEntry(entry: LogEntry): boolean {
    return ['post', 'comment', 'repost', 'quote'].includes(entry.type);
  }

  function handleLogRowClick(entry: LogEntry) {
    if (entry.postId) {
      // For content entries (posts, comments, etc.), use proper postId navigation
      api.openView('viewPost', `${entry.type} ${entry.hash}`, {
        postId: entry.postId,
        repository: repository || 'myrepository'
      });
    } else {
      // For metadata entries (lists, config), show a simple view
      api.openView('viewPost', `${entry.type} ${entry.hash}`, {
        repository: repository || 'myrepository',
        logEntry: entry
      });
    }
  }

  // Filtered log data based on type filter, already sorted by timestamp (newest first)
  $: filteredLogData = typeFilter === 'all'
    ? logTabData
    : logTabData.filter(entry => entry.type === typeFilter);

</script>

<div class="view-container">
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar border-r">
    <!-- Main Row -->
    <div class="grid gap-2 items-center" style="grid-template-columns: auto 1fr auto;">
      <!-- Column 1: Avatar/Icon -->
      <div>
        {#if !isWorkspace}
          <Avatar
            type="repository"
            identifier={gitHost.getWebUrl(repositoryUrl) || repositoryUrl}
            name={gitHost.getDisplayName(repositoryUrl)}
            size={48}
          />
        {:else}
          <span class="codicon codicon-xl codicon-home"></span>
        {/if}
      </div>

      <!-- Column 2: Title -->
      <div class="min-w-0">
        <h1 class="truncate">
          {#if isWorkspace}
            My Repository
          {:else}
            {gitHost.getDisplayName(repositoryUrl)}
          {/if}
        </h1>
      </div>

      <!-- Column 3: Navigation + Actions -->
      <div class="flex items-center gap-2">
        <!-- Time Navigation (grouped) -->
        <DateNavigation
          offset={rangeOffset}
          label={rangeLabel}
          loading={isLoadingRange}
          onPrevious={goToPreviousRange}
          onNext={goToNextRange}
          onRefresh={showFetchButton ? handleFetch : undefined}
          refreshLoading={fetchingRepository}
        />

        <!-- Action Buttons -->
        {#if !isWorkspace && !isFollowedRepo}
          <button
            class="btn btn-primary"
            on:click={handleFollow}
            title="Follow this repository"
          >
            <span class="codicon codicon-add"></span>
            Follow
          </button>
        {/if}

        {#if canPush}
          {@const totalUnpushed = totalUnpushedCounts.total + lists.filter(l => l.isUnpushed).length}
          <button
            class="btn"
            disabled={totalUnpushed === 0 || pushingToRemote}
            on:click={handlePush}
            title={totalUnpushed > 0
              ? `Push ${totalUnpushedCounts.posts} post${totalUnpushedCounts.posts !== 1 ? 's' : ''}, ${totalUnpushedCounts.comments} comment${totalUnpushedCounts.comments !== 1 ? 's' : ''}, and ${lists.filter(l => l.isUnpushed).length} list${lists.filter(l => l.isUnpushed).length !== 1 ? 's' : ''} to origin`
              : 'Nothing to push'}
          >
            <span class="codicon codicon-{pushingToRemote ? 'loading spin' : 'cloud-upload'}"></span>
            {pushingToRemote ? 'Pushing...' : totalUnpushed > 0 ? 'Push All' : 'Push'}
          </button>
        {/if}

        {#if isWorkspace && activeTab === 'posts'}
          <button
            class="btn primary"
            on:click={handleCreatePost}
            title="Create a new post"
          >
            <span class="codicon codicon-edit"></span>
          </button>
        {/if}

        {#if isWorkspace}
          <button
            class="btn"
            on:click={handleSettings}
            title="Repository Settings"
          >
            <span class="codicon codicon-gear"></span>
          </button>
        {/if}
      </div>
    </div>

    <!-- Bottom Row: URL + Meta (concatenated, right-aligned) -->
    <div class="flex justify-left items-center {isWorkspace ? 'mt-1' : '-mb-1'} ">
      <div class="flex items-center gap-4 text-sm text-muted italic whitespace-nowrap">
        {#if (isWorkspace && originUrl) || (!isWorkspace && repository)}
          {@const displayUrl = isWorkspace ? gitMsgUrl.normalize(originUrl) : repositoryUrl}
          {@const baseUrl = gitHost.getWebUrl(displayUrl) || '#'}
          {@const fullUrl = repositoryBranch ? `${baseUrl}#branch:${repositoryBranch}` : baseUrl}
          {#if fullUrl !== '#'}
            <a href={fullUrl} class="hover-underline text-muted">
              {displayUrl}{repositoryBranch ? `#branch:${repositoryBranch}` : ''}
            </a>
          {:else}
            <span class="text-muted">
              {displayUrl}{repositoryBranch ? `#branch:${repositoryBranch}` : ''}
            </span>
          {/if}
        {/if}
        {#if isFollowedRepo && repositoryStatus?.lists}
          {#each repositoryStatus.lists as listName}
            {@const listObj = availableLists.find(l => l.name === listName)}
            {#if listObj}
              <div class="inline-flex items-center gap-1">
                <span class="codicon codicon-list-unordered" title="Following in list"></span>
                <span
                  class="flex items-center gap-1 hover-underline text-muted cursor-pointer"
                  on:click={() => handleOpenList(listObj)}
                  on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && handleOpenList(listObj)}
                  role="button"
                  tabindex="0"
                  title="Open {listName} list"
                >
                  {listName}
                </span>
                <button
                  class="btn xxs ghost"
                  on:click={() => handleUnfollowFromList(listObj.id)}
                  title="Unfollow from {listName}"
                >
                  <span class="codicon codicon-close text-xs"></span>
                </button>
              </div>
            {:else}
              <span>{listName}</span>
            {/if}
          {/each}
        {/if}
        {#if fetchTimeDisplay}
          <span class="flex items-center gap-1">
            <span class="codicon codicon-sync" title="Fetched"></span>
            {fetchTimeDisplay}</span>
        {/if}
      </div>
    </div>

    <Tabs {tabs} {activeTab} on:change={handleTabChange} />
  </div>

  {#if error}
    <div class="error">
      <span class="codicon codicon-error"></span>
      {error}
    </div>
  {:else}
    {#if activeTab === 'posts'}
      <div class="section">
        <div class="flex flex-col gap-2 -ml-4">
          {#each postsTabData as post (post.id)}
            <PostCard post={post} posts={allPostsForContext} />
          {/each}
        </div>
      </div>
    {:else if activeTab === 'comments'}
      <div class="section">
        {#if repliesTabData.length === 0}
          <div class="empty">
            <span class="codicon codicon-comment"></span>
            <p>No replies yet</p>
          </div>
        {:else}
          {@const threadGroups = groupRepliesByThread(repliesTabData)}
          {@const sortedThreads = Array.from(threadGroups.entries()).sort((a, b) => {
            // Sort threads by the most recent reply in each thread
            const aLatest = Math.max(...a[1].map(r =>
              r.timestamp instanceof Date ? r.timestamp.getTime() : new Date(r.timestamp).getTime()
            ));
            const bLatest = Math.max(...b[1].map(r =>
              r.timestamp instanceof Date ? r.timestamp.getTime() : new Date(r.timestamp).getTime()
            ));
            return bLatest - aLatest; // Newest threads first
          })}
          <div class="flex flex-col gap-2 -ml-4">
            {#each sortedThreads as [threadId, threadReplies]}
              {@const originalPost = contextPosts.get(threadId) || allPostsForContext.find(p => p.id === threadId)}
              <div class="thread-group mb-4">
                {#if originalPost}
                  <div class="relative mb-2">
                    {#if threadReplies.length > 0}
                      <div class="thread-connector"></div>
                    {/if}
                    <PostCard post={originalPost} posts={allPostsForContext} />
                  </div>
                {/if}
                {#each threadReplies as reply, index}
                  <div class="relative mb-2">
                    {#if index < threadReplies.length - 1}
                      <div class="thread-connector"></div>
                    {/if}
                    <PostCard post={reply} posts={allPostsForContext} showParentContext={false} />
                  </div>
                {/each}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {:else if activeTab === 'lists'}
      <div class="section">
        <div class="flex flex-col gap-2">
          <ListManager
            {lists}
            readOnly={!isWorkspace}
            repository={!isWorkspace ? repository : undefined}
            on:createList={handleCreateList}
            on:deleteList={handleDeleteList}
            on:viewList={handleViewList}
            on:followList={handleFollowListCard}
            on:unfollowList={handleUnfollowListCard}
          />
        </div>
      </div>
    {:else if activeTab === 'followers'}
      <div class="section">
        {#if !followersLoaded}
          <div class="empty">
            <span class="codicon codicon-loading spin"></span>
            <p>Loading followers...</p>
          </div>
        {:else if followersTabData.length === 0}
          <div class="empty">
            <span class="codicon codicon-person"></span>
            <p>No mutual followers yet</p>
            <p class="text-muted text-sm">Repositories you follow that also follow you will appear here</p>
          </div>
        {:else}
          <div class="flex flex-col gap-2">
            {#each followersTabData as follower}
              <div class="card p-3">
                <div class="flex items-start justify-between">
                  <div>
                    <h3 class="font-medium">{follower.name || follower.url}</h3>
                    <p class="text-muted text-sm mt-1">{follower.url}</p>
                  </div>
                  <span class="px-2 py-1 bg-muted rounded text-xs">
                    via "{follower.followsVia}" list
                  </span>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {:else if activeTab === 'log'}
      <div class="section">
        {#if !logsLoaded}
          <div class="empty">
            <span class="codicon codicon-loading spin"></span>
            <p>Loading logs...</p>
          </div>
        {:else if logTabData.length === 0}
          <div class="empty">
            <span class="codicon codicon-history"></span>
            <p>No actions yet</p>
          </div>
        {:else}
          <div class="mb-4">
            <label for="type-filter" class="block text-sm font-medium mb-2">Filter by type:</label>
            <select id="type-filter" bind:value={typeFilter} class="w-48">
              <option value="all">All actions</option>
              <option value="post">Posts</option>
              <option value="comment">Comments</option>
              <option value="repost">Reposts</option>
              <option value="quote">Quotes</option>
              <option value="list-create">List create</option>
              <option value="list-delete">List delete</option>
              <option value="repository-follow">Repository follow</option>
              <option value="repository-unfollow">Repository unfollow</option>
              <option value="config">Configuration</option>
              <option value="metadata">Metadata</option>
            </select>
          </div>

          <div class="rounded border overflow-hidden">
            <table class="w-full text-xs">
              <thead class="bg-muted text-left">
                <tr>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">Hash</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">Time</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">Author</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">Type</th>
                  <th class="px-3 py-2 font-medium w-full">Details</th>
                </tr>
              </thead>
              <tbody>
                {#each filteredLogData as entry}
                  <tr
                    class="border-t"
                    class:clickable-row={isClickableEntry(entry)}
                    on:click={isClickableEntry(entry) ? () => handleLogRowClick(entry) : undefined}
                  >
                    <td class="px-3 py-2 whitespace-nowrap">
                      <div>{entry.hash}</div>
                    </td>
                    <td class="px-3 py-2 text-muted whitespace-nowrap">
                      {formatRelativeTime(entry.timestamp)}
                    </td>
                    <td class="px-3 py-2 whitespace-nowrap">
                      <div class="flex items-center gap-2 min-w-0">
                        <Avatar
                          type="user"
                          identifier={entry.author.email}
                          name={entry.author.name}
                          size={20}
                        />
                        <span class="truncate max-w-24" title={entry.author.name}>
                          {entry.author.name}
                        </span>
                      </div>
                    </td>
                    <td class="px-3 py-2 whitespace-nowrap">
                      <span class="px-2 py-1">
                        {entry.type}
                      </span>
                    </td>
                    <td class="px-3 py-2 w-full">
                      {entry.details}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<Dialog
  isOpen={showFollowDialog}
  title="Follow Repository"
  on:close={() => {
    showFollowDialog = false;
    selectedListIds = [];
    showCreateNew = false;
    firstListName = 'Default';
  }}
>
  <div class="mb-4">
    <div class="card bg-muted p-3">
      <div class="font-semibold">{gitHost.getDisplayName(repository)}</div>
      <div class="text-sm text-muted">{repository}</div>
    </div>
  </div>

  <div class="mb-4">
    {#if availableLists.length === 0 && !showCreateNew}
      <div class="space-y-2">
        <div class="font-medium mb-2">Create your first list:</div>
        <form on:submit|preventDefault={() => {
          const trimmedName = firstListName.trim();
          if (!trimmedName) {return;}
          const listId = trimmedName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').substring(0, 40);
          handleCreateAndFollow({ detail: { id: listId, name: trimmedName } });
        }}>
          <div class="flex gap-2">
            <input
              type="text"
              bind:value={firstListName}
              placeholder="List name"
              disabled={isFollowDialogSubmitting}
              class="flex-1"
            />
            <button
              type="submit"
              class="btn primary"
              disabled={isFollowDialogSubmitting || !firstListName.trim()}>
              {#if isFollowDialogSubmitting}
                <span class="codicon codicon-loading spin"></span>
              {:else}
                Create & Follow
              {/if}
            </button>
          </div>
        </form>
      </div>
    {:else if !showCreateNew}
      <div class="space-y-2">
        <div class="font-medium mb-2">Add to lists:</div>
        {#each availableLists as list}
          <label class="flex items-center gap-2 cursor-pointer hover:bg-muted rounded px-2 py-1">
            <input
              type="checkbox"
              bind:group={selectedListIds}
              value={list.id}
              disabled={isFollowDialogSubmitting}
            />
            <div class="flex-1">
              <div>{list.name}</div>
              {#if list.repositories && list.repositories.length > 0}
                <div class="text-xs text-muted">
                  {list.repositories.length} repositor{list.repositories.length === 1 ? 'y' : 'ies'}
                </div>
              {/if}
            </div>
          </label>
        {/each}

        <div class="border-t pt-2 mt-2">
          <button
            class="btn sm"
            on:click={() => { showCreateNew = true; selectedListIds = []; }}>
            <span class="codicon codicon-add"></span>
            Create new list
          </button>
        </div>
      </div>
    {:else}
      <div>
        <button
          class="btn sm"
          on:click={() => { showCreateNew = false; }}>
          <span class="codicon codicon-arrow-left"></span>
          Back to list selection
        </button>

        <div class="mt-3">
          <ListCreateForm
            bind:submitHandler={submitCreateListForm}
            isCreating={isFollowDialogSubmitting}
            compact={true}
            on:createList={handleCreateAndFollow}
          />
        </div>
      </div>
    {/if}
  </div>

  <div slot="footer" class="flex justify-left gap-2">
    <button class="btn" on:click={() => {
      showFollowDialog = false;
      selectedListIds = [];
      showCreateNew = false;
      firstListName = 'Default';
    }} disabled={isFollowDialogSubmitting}>
      Cancel
    </button>
    {#if showCreateNew}
      <button
        class="btn primary"
        on:click={() => { if (submitCreateListForm) {submitCreateListForm();} }}
        disabled={isFollowDialogSubmitting}>
        {#if isFollowDialogSubmitting}
          <span class="codicon codicon-loading spin"></span>
          Adding...
        {:else}
          Create List & Follow
        {/if}
      </button>
    {:else}
      <button
        class="btn primary"
        on:click={() => {
          if (selectedListIds.length > 0) {
            handleFollowSubmit({ detail: { listIds: selectedListIds } });
          }
        }}
        disabled={isFollowDialogSubmitting || selectedListIds.length === 0}>
        {#if isFollowDialogSubmitting}
          <span class="codicon codicon-loading spin"></span>
          Adding...
        {:else}
          Follow Repository{#if selectedListIds.length > 1} ({selectedListIds.length} lists){/if}
        {/if}
      </button>
    {/if}
  </div>
</Dialog>
