<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { Post } from '@gitsocial/core';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';

  let searchQuery = '';
  let searchResults: Post[] = [];
  let loading = false;
  let error: string | null = null;
  let debounceTimer: number | undefined;
  let currentRequestId: string | null = null;

  // Message handler
  function handleMessage(event: MessageEvent) {
    const message = event.data;

    // Handle search results
    if (message.type === 'searchResults' && message.requestId === currentRequestId) {
      loading = false;
      currentRequestId = null;

      if (message.data?.posts) {
        searchResults = message.data.posts;
        error = null;
      } else {
        searchResults = [];
      }
    }

    // Handle errors
    if (message.type === 'error' && message.requestId === currentRequestId) {
      loading = false;
      currentRequestId = null;
      error = message.data?.message || 'Search failed';
      searchResults = [];
    }
  }

  onMount(() => {
    // Focus on search input when component mounts
    const input = document.querySelector('.search-input') as HTMLInputElement;
    if (input) {
      input.focus();
    }

    // Listen for messages from extension
    window.addEventListener('message', handleMessage);
  });

  onDestroy(() => {
    // Clean up event listener
    window.removeEventListener('message', handleMessage);

    // Clear any pending timers
    if (debounceTimer) {
      clearTimeout(debounceTimer);
    }
  });

  function handleSearch() {
    if (debounceTimer) {
      clearTimeout(debounceTimer);
    }

    debounceTimer = setTimeout(() => {
      performSearch();
    }, 300) as unknown as number;
  }

  function performSearch() {
    if (!searchQuery.trim()) {
      searchResults = [];
      error = null;
      return;
    }

    loading = true;
    error = null;

    // Generate request ID
    currentRequestId = Date.now().toString(36) + Math.random().toString(36);

    // Send search request with request ID
    api.postMessage({
      type: 'social.searchPosts',
      query: searchQuery.trim(),
      limit: 100,
      id: currentRequestId
    });
  }

  function clearSearch() {
    searchQuery = '';
    searchResults = [];
    error = null;
    currentRequestId = null;
  }
</script>

<div>
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar mb-6 border-b-r">
    <h1><span class="codicon codicon-lg codicon-search mr-2"></span>Search Posts</h1>
  </div>

  <div class="mb-6">
    <div class="flex gap-2 items-end">
      <div class="flex-1">
        <label for="search-input" class="block text-sm font-medium mb-1">Search Query</label>
        <div class="relative">
          <input
            id="search-input"
            type="text"
            class="w-full pr-10"
            placeholder="Search posts, authors, or content..."
            bind:value={searchQuery}
            on:input={handleSearch}
            on:keydown={(e) => e.key === 'Enter' && performSearch()}
          />
          {#if searchQuery}
            <div class="absolute right-0 top-0 mr-1 mt-1">
              <span
                class="codicon codicon-close codicon-lg"
                role="button"
                tabindex="0"
                on:click={clearSearch}
                on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && clearSearch()}
                aria-label="Clear search"
              ></span>
            </div>
          {/if}
        </div>
      </div>
      <button
        class="btn primary"
        on:click={performSearch}
        disabled={loading || !searchQuery.trim()}
      >
        <span class="codicon codicon-search"></span>
        Search
      </button>
    </div>
    <div class="text-xs text-muted mt-1">
      Use filters like <code class="code-inline">author:email</code>, <code class="code-inline">type:comment</code>
    </div>
  </div>

  {#if searchResults.length > 0 && !loading}
    <div class="text-sm text-muted mb-4">
      {searchResults.length} {searchResults.length === 1 ? 'post' : 'posts'} found
    </div>
  {/if}

  <div class="min-h-[200px]">
    {#if loading}
      <div class="flex flex-col items-center justify-center py-8">
        <span class="codicon codicon-loading spin text-2xl mb-2"></span>
        <p class="text-muted">Searching...</p>
      </div>
    {:else if error}
      <div class="text-center py-8">
        <p class="text-sm text-error">Error: {error}</p>
      </div>
    {:else if searchResults.length > 0}
      <div class="flex flex-col gap-2 -ml-4">
        {#each searchResults as post (post.id)}
          <PostCard post={post} />
        {/each}
      </div>
    {:else if searchQuery.trim()}
      <div class="text-center py-12 text-muted">
        <p>No posts found matching your search.</p>
        <p class="text-sm opacity-80 mt-2">Try different keywords or check your filters.</p>
      </div>
    {:else}
      <div class="text-center py-12 text-muted">
        <p>Enter a search term to find posts.</p>
      </div>
    {/if}
  </div>
</div>
