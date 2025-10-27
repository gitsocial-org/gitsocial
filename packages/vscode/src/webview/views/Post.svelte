<script lang="ts">
  import { onMount } from 'svelte';
  import type { Post } from '@gitsocial/core/client';
  import { gitHost, gitMsgUrl } from '@gitsocial/core/client';
  import { api } from '../api';
  import Avatar from '../components/Avatar.svelte';
  import Thread from '../components/Thread.svelte';

  let post: Post | null = null;
  let loading = true;
  let error = '';
  let sortBy: 'top' | 'latest' | 'oldest' = 'top';
  let timeRangeLabel = '';

  // Get post ID from view params
  const postId = (window as { viewParams?: { postId?: string } }).viewParams
    ?.postId;

  onMount(() => {
    if (!postId) {
      error = 'No post ID provided';
      loading = false;
      return;
    }

    let hasReceivedInitialPost = false;

    // Listen for messages from extension
    window.addEventListener('message', (event) => {
      const message = event.data;

      switch (message.type) {
        case 'initialPost':
          // Received the post data directly
          post = message.data;
          hasReceivedInitialPost = true;
          loading = false;
          break;

        case 'posts':
          // Handle main post response only
          if (message.requestId && message.requestId.startsWith('getMainPost-') && !hasReceivedInitialPost) {
            // This is the main post response
            const posts = message.data || [];
            if (posts.length > 0) {
              post = posts[0];
              loading = false;
            } else {
              error = 'Post not found';
              loading = false;
            }
          }
          break;

        case 'error':
          error = message.message || 'Failed to load post';
          loading = false;
          break;

        case 'refresh':
          // Thread component will handle its own refresh
          break;

        case 'postCreated':
        case 'commitCreated':
          // Refresh when a new post/comment/repost/quote is created
          // Thread component will handle its own refresh
          break;
      }
    });

    // Tell extension we're ready
    api.ready();

    // If no initial post is provided after a short delay, fetch it
    setTimeout(() => {
      if (!hasReceivedInitialPost && !post && !error) {
        // Fetch the post by ID
        api.getPosts({ scope: `post:${postId}`, limit: 1 }, `getMainPost-${Date.now()}`);
      }
    }, 100);
  });

  // Note: Refresh is handled by Thread component, no action needed here

  // Update panel icon when post loads
  $: if (post && post.author?.email) {
    api.updatePanelIcon({
      email: post.author.email,
      repository: post.repository || 'myrepository'
    });
  }

  function goBack() {
    api.closePanel();
  }

  function handleViewRepository() {
    if (post) {
      if (post.display.isOrigin) {
        api.openView('repository', 'My Repository');
      } else if (gitMsgUrl.validate(post.repository)) {
        api.openView(
          'repository',
          post.display.repositoryName,
          { repository: post.repository }
        );
      } else {
        api.openView('repository', 'My Repository');
      }
    }
  }

</script>

<!-- Header -->
<div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar flex justify-between items-center border-b">
  <div class="flex items-center">
    <button class="btn ghost" on:click={goBack}><span class="codicon codicon-arrow-left"></span>Post</button>
    <select
      bind:value={sortBy}
      class="btn ghost"
    >
      <option value="top">Top replies</option>
      <option value="latest">Latest first</option>
      <option value="oldest">Oldest first</option>
    </select>
    {#if timeRangeLabel}
      <span class="text-sm text-muted italic ml-4 pl-2">{timeRangeLabel}</span>
    {/if}
  </div>
  <div class="flex items-center">
    {#if post}
      <button class="btn ghost" on:click={handleViewRepository}>
        {#if post.display.isOrigin || !gitMsgUrl.validate(post.repository)}
          <span class="codicon codicon-home"></span>
          <span>My Repository</span>
        {:else}
          <Avatar
            type="repository"
            identifier={gitHost.getWebUrl(post.repository) || post.repository}
            name={post.display.repositoryName}
            size={32}
          />
          <span>{post.display.repositoryName}</span>
        {/if}
      </button>
    {/if}
  </div>
</div>

<div class="-ml-4">
  {#if loading}
    <div class="flex flex-col items-center justify-center p-3">
      <p class="mt-2">Loading post...</p>
    </div>
  {:else if error}
    <div class="p-3 text-center">
      <p class="text-error mb-4">
        ⚠️ {error}
      </p>
      <button class="btn" on:click={() => window.location.reload()}>Retry</button>
    </div>
  {:else if postId}
    <!-- Thread View -->
    <Thread anchorPostId={postId} sort={sortBy} bind:timeRangeLabel />
  {/if}
</div>
