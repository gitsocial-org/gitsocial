<script lang="ts">
  import { onMount } from 'svelte';
  import type { Post as _Post } from '@gitsocial/core/client';
  import { posts, loading, error, repositoryInfo } from './stores';
  import { api } from './api';
  import PostCard from './components/PostCard.svelte';
  import Repository from './views/Repository.svelte';
  import { webLog } from './utils/weblog';

  const viewMode: 'simple' | 'repository' = 'repository';

  onMount(() => {
    // Notify extension that webview is ready
    api.ready();
  });

  // Subscribe to error messages
  $: if ($error) {
    webLog('error', 'GitSocial error:', $error);
  }

</script>

<main>
  {#if viewMode === 'simple'}
    {#if $repositoryInfo}
      <div class="header">
        <h2>GitSocial</h2>
        <div class="repo-info">
          <span class="repo-name">{$repositoryInfo.name}</span>
          <span class="branch-name">{$repositoryInfo.branch}</span>
        </div>
      </div>
    {/if}

    {#if $loading}
      <div class="loading">
        <div class="spinner"></div>
        <p>Loading posts...</p>
      </div>
    {:else if $error}
      <div class="error">
        <p>⚠️ {$error}</p>
        <button class="btn" on:click={() => api.refresh()}>Retry</button>
      </div>
    {:else if $posts.length === 0}
      <div class="empty">
        <p>No posts yet.</p>
        <p>Create your first post above!</p>
      </div>
    {:else}
      <div class="posts-list">
        {#each $posts as post (post.id)}
          <PostCard post={post} />
        {/each}
      </div>
    {/if}
  {:else if viewMode === 'repository' && $repositoryInfo}
    <Repository />
  {/if}
</main>
