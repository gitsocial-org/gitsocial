<script lang="ts" context="module">
  import type { Post } from '@gitsocial/core/client';
  import { gitMsgUrl } from '@gitsocial/core/client';

  export function validatePostId(postId: string | undefined): { isValid: boolean; error?: string } {
    if (!postId) {
      return { isValid: false, error: 'No post ID provided' };
    }
    return { isValid: true };
  }

  export function processPostMessage(
    message: { type: string; data?: Post | Post[]; requestId?: string; message?: string },
    currentRequestId: string | null,
    hasReceivedInitialPost: boolean
  ): {
    shouldUpdate: boolean;
    post: Post | null;
    error: string;
    hasReceivedInitialPost: boolean;
  } {
    if (!message) {
      return { shouldUpdate: false, post: null, error: '', hasReceivedInitialPost };
    }

    switch (message.type) {
      case 'initialPost':
        if (message.data && !Array.isArray(message.data)) {
          return {
            shouldUpdate: true,
            post: message.data,
            error: '',
            hasReceivedInitialPost: true
          };
        }
        return { shouldUpdate: false, post: null, error: '', hasReceivedInitialPost };

      case 'posts':
        if (
          message.requestId &&
          message.requestId.startsWith('getMainPost-') &&
          !hasReceivedInitialPost
        ) {
          const posts = Array.isArray(message.data) ? message.data : [];
          if (posts.length > 0) {
            return {
              shouldUpdate: true,
              post: posts[0],
              error: '',
              hasReceivedInitialPost
            };
          }
          return {
            shouldUpdate: true,
            post: null,
            error: 'Post not found',
            hasReceivedInitialPost
          };
        }
        return { shouldUpdate: false, post: null, error: '', hasReceivedInitialPost };

      case 'error':
        return {
          shouldUpdate: true,
          post: null,
          error: message.message || 'Failed to load post',
          hasReceivedInitialPost
        };

      case 'refresh':
      case 'postCreated':
      case 'commitCreated':
        return { shouldUpdate: false, post: null, error: '', hasReceivedInitialPost };

      default:
        return { shouldUpdate: false, post: null, error: '', hasReceivedInitialPost };
    }
  }

  export function determineRepositoryView(post: Post | null): {
    viewType: 'repository';
    title: string;
    params?: { repository: string };
  } {
    if (!post) {
      return { viewType: 'repository', title: 'My Repository' };
    }

    if (post.display.isOrigin) {
      return { viewType: 'repository', title: 'My Repository' };
    }

    if (gitMsgUrl.validate(post.repository)) {
      return {
        viewType: 'repository',
        title: post.display.repositoryName,
        params: { repository: post.repository }
      };
    }

    return { viewType: 'repository', title: 'My Repository' };
  }
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import { gitHost } from '@gitsocial/core/client';
  import { api } from '../api';
  import Avatar from '../components/Avatar.svelte';
  import Thread from '../components/Thread.svelte';

  let post: Post | null = null;
  let loading = true;
  let error = '';
  let sortBy: 'top' | 'latest' | 'oldest' = 'top';
  let timeRangeLabel = '';

  const postId = (window as { viewParams?: { postId?: string } }).viewParams?.postId;

  onMount(() => {
    const validation = validatePostId(postId);
    if (!validation.isValid) {
      error = validation.error || 'Invalid post ID';
      loading = false;
      return;
    }

    let hasReceivedInitialPost = false;

    window.addEventListener('message', (event) => {
      const message = event.data;
      const result = processPostMessage(message, null, hasReceivedInitialPost);

      if (result.shouldUpdate) {
        if (result.post) {
          post = result.post;
        }
        if (result.error) {
          error = result.error;
        }
        hasReceivedInitialPost = result.hasReceivedInitialPost;
        loading = false;
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
    const view = determineRepositoryView(post);
    if (view.params) {
      api.openView(view.viewType, view.title, view.params);
    } else {
      api.openView(view.viewType, view.title);
    }
  }

</script>

<!-- Header -->
<div class="sticky z-20 top-0 -ml-4 -mr-4 p-2 bg-sidebar flex justify-between items-center border-b-r">
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

<div class="-ml-4 -mr-4">
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
