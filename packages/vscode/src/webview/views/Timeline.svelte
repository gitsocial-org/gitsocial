<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';
  import { skipCacheOnNextRefresh } from '../stores';
  import type { Post } from '@gitsocial/core/client';
  import { formatRelativeTime, useEventDrivenTimeUpdates, getWeekStart, getWeekEnd, getWeekLabel } from '../utils/time';
  import type { Repository } from '@gitsocial/core/client';

  let posts: Post[] = [];
  let error: string | null = null;
  let fetchingRemotes = false;
  let currentRequestId: string | null = null;
  let lastFetchTime: Date | null = null;
  let currentTime = Date.now();
  let cleanupTimeUpdates: (() => void) | null = null;
  let repositories: Repository[] = [];

  // Week navigation state
  let weekOffset = 0; // 0 = this week, -1 = last week, -2 = 2 weeks ago, etc.
  let isLoadingWeek = false;

  // Computed values for current week
  $: weekLabel = getWeekLabel(weekOffset);

  // Reactive fetch time display that updates with currentTime
  $: fetchTimeDisplay = lastFetchTime && currentTime ? formatRelativeTime(lastFetchTime) : '';

  // Message handler function
  function handleMessage(event: MessageEvent) {
    const message = event.data;

    switch (message.type) {
      case 'posts':
        // Only handle posts responses for our requests
        if (message.requestId === currentRequestId) {
          posts = message.data || [];
          error = null;
          isLoadingWeek = false;
        }
        break;

      case 'repositories': {
        // Receive repository data with fetch times
        repositories = message.data || [];
        // Use the most recent fetch time from all repositories
        const reposWithFetchTime = repositories.filter(r => r.lastFetchTime);
        if (reposWithFetchTime.length > 0) {
          const fetchTimes = reposWithFetchTime.map(r => new Date(r.lastFetchTime as Date).getTime());
          const mostRecentTime = Math.max(...fetchTimes);
          lastFetchTime = new Date(mostRecentTime);
        }
        break;
      }

      case 'error':
        // Only handle errors for our requests
        if (message.requestId === currentRequestId) {
          error = message.message || 'Failed to load posts';
          fetchingRemotes = false;
          isLoadingWeek = false;
        }
        break;

      case 'fetchProgress':
        // Handle fetch progress updates
        if (message.data.status === 'fetching') {
          fetchingRemotes = true;
        } else if (message.data.status === 'completed') {
          fetchingRemotes = false;
          // Update fetch time when remote fetch completes successfully
          lastFetchTime = new Date();
        }
        break;

      case 'fetchCompleted':
        fetchingRemotes = false;
        // Update fetch time when remote fetch completes successfully
        lastFetchTime = new Date();
        break;

      case 'refresh': {
        const scopes = message.scope || ['all'];
        const operation = message.operation;
        const skipCache = message.skipCache || $skipCacheOnNextRefresh;

        if (skipCache && $skipCacheOnNextRefresh) {
          skipCacheOnNextRefresh.set(false);
        }

        // List renames don't affect timeline content, only lists metadata
        if (operation === 'listRenamed') {
          // Skip reload - list renames don't affect timeline posts
          break;
        }

        // Standard scope-based refresh
        // Timeline only cares about posts, not lists metadata
        if (scopes.includes('all') || scopes.includes('posts')) {
          isLoadingWeek = true;
          error = null;
          // Calculate dates directly to avoid timing issues with reactive values
          const weekStart = getWeekStart(weekOffset);
          const weekEnd = getWeekEnd(weekOffset);
          const since = weekStart.toISOString();
          const until = weekEnd.toISOString();
          currentRequestId = api.getPosts({
            since,
            until,
            scope: 'timeline',
            types: ['post', 'quote', 'repost'],
            skipCache
          });
        }
        // If scope is only 'lists', do nothing - timeline doesn't show lists
        break;
      }

      case 'refreshAfterFetch': {
        // No longer needed - backend handles fetching before returning posts
        // Kept for compatibility with other views
        break;
      }

      case 'postCreated':
      case 'commitCreated':
        // Refresh timeline when a new post is created
        loadWeekData();
        break;

    }
  }

  onMount(() => {
    // Load posts for current week (weekOffset = 0)
    loadWeekData();

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

  function loadWeekData() {
    isLoadingWeek = true;
    error = null;
    // Calculate dates directly to avoid timing issues with reactive values
    const weekStart = getWeekStart(weekOffset);
    const weekEnd = getWeekEnd(weekOffset);
    const since = weekStart.toISOString();
    const until = weekEnd.toISOString();

    // Simply request posts for the week - backend handles fetching if needed
    currentRequestId = api.getPosts({
      since,
      until,
      scope: 'timeline',
      types: ['post', 'quote', 'repost']
    });
  }

  function handleFetchRepositories() {
    api.fetchRepositories();
  }

  function handleCreatePost() {
    api.openView('createPost', 'Create Post');
  }
</script>

<div>
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar">
    <div class="flex justify-between items-center">
      <h1><svg width="20" height="20" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" style="vertical-align: text-bottom; margin-right: 0.5rem;"><path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" /></svg>Timeline</h1>
      <div class="flex items-center gap-1">
        <!-- Week Navigation -->
        <button
          class="btn sm"
          on:click={goToPreviousWeek}
          title="Previous week"
          disabled={isLoadingWeek}
        >
          <span class="codicon codicon-chevron-left"></span>
        </button>
        <div class="text-center px-1">
          {#if isLoadingWeek}
            <span class="codicon codicon-loading spin"></span>
            Loading...
          {:else}
            <div class="">{weekLabel}</div>
          {/if}
        </div>
        <button
          class="btn sm"
          on:click={weekOffset === 0 ? handleFetchRepositories : goToNextWeek}
          title={weekOffset === 0 ? 'Fetch updates from remote repositories' : 'Next week'}
          disabled={weekOffset === 0 ? fetchingRemotes : (weekOffset >= 0 || isLoadingWeek)}
        >
          {#if weekOffset === 0}
            <span class="codicon codicon-{fetchingRemotes ? 'loading spin' : 'sync'}"></span>
          {:else}
            <span class="codicon codicon-chevron-right"></span>
          {/if}
        </button>
      </div>
    </div>
    <div class="flex justify-end mt-1">
      <div class="text-sm text-muted italic">
        <span>{posts.length} post{posts.length === 1 ? '' : 's'}</span>
        {#if fetchTimeDisplay}
          <span class="mx-1">•</span>
          <span>Fetched {fetchTimeDisplay}</span>
        {/if}
      </div>
    </div>
  </div>

  {#if error}
    <div class="error">
      <span class="codicon codicon-error"></span>
      {error}
    </div>
  {:else if posts.length === 0}
    <div class="empty">
      <span class="codicon codicon-inbox"></span>
      <p>No posts yet</p>
      <button class="btn primary wide" on:click={handleCreatePost}>
        Create your first post
      </button>
    </div>
  {:else}
    <div class="flex flex-col gap-2 -ml-4">
      {#each posts as post (post.id)}
        <PostCard {post} />
      {/each}
    </div>
  {/if}
</div>
