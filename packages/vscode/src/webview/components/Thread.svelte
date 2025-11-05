<script lang="ts">
  import { onMount } from 'svelte';
  import type { Post, ThreadItem as ThreadItemType, ThreadSort } from '@gitsocial/core/client';
  import PostCard from '../components/PostCard.svelte';
  import FullscreenPostViewer from '../components/FullscreenPostViewer.svelte';
  import { api } from '../api';
  import { sortPosts } from '../utils/sorting';
  import { getLastMonday, getTimeRangeLabel } from '../utils/time';

  export let anchorPostId: string;
  export let sort: ThreadSort = 'top';

  // Bind timeRangeLabel so parent can access it
  export let timeRangeLabel = '';
  let loading = true;
  let rawPosts: Post[] = [];
  let threadItems: ThreadItemType[] = [];
  let error: string | null = null;
  let currentRequestId: string | null = null;
  let fullscreenPost: Post | null = null;
  let fullscreenPostIndex = 0;

  // Reactive: rebuild thread items when posts or sort changes
  $: threadItems = rawPosts.length > 0 ? buildThreadItems(rawPosts) : [];
  // Explicitly track sort changes to trigger rebuild
  $: if (sort && rawPosts.length > 0) {
    threadItems = buildThreadItems(rawPosts);
  }

  function fetchThread() {
    loading = true;
    error = null;

    // Set time range for data
    const lastMonday = getLastMonday();
    const since = lastMonday.toISOString();
    timeRangeLabel = getTimeRangeLabel(lastMonday);

    // Store the request ID so we can filter responses
    currentRequestId = `thread-${anchorPostId}-${Date.now()}`;

    api.getPosts({
      scope: `thread:${anchorPostId}`,
      since,
      sortBy: sort
    }, currentRequestId);
  }

  onMount(() => {
    const handler = (event: MessageEvent) => {
      const message = event.data;

      // Handle broadcast messages (no requestId)
      if (message.type === 'postCreated' || message.type === 'commitCreated' || message.type === 'refresh') {
        // Refresh thread when a new post/comment/repost/quote is created
        fetchThread();
        return;
      }

      // Only process messages for our current request
      if (message.requestId !== currentRequestId) {
        return;
      }

      if (message.type === 'posts') {
        rawPosts = message.data || [];
        loading = false;
      } else if (message.type === 'error') {
        error = message.data.message || 'Failed to load thread';
        loading = false;
      }
    };

    window.addEventListener('message', handler);
    fetchThread();

    return () => {
      window.removeEventListener('message', handler);
    };
  });

  function matchesPostId(postId: string | undefined, targetId: string): boolean {
    if (!postId) { return false; }
    if (postId === targetId) { return true; }

    const getHash = (id: string) => id.split('#commit:')[1]?.substring(0, 12);
    const hash1 = getHash(postId);
    const hash2 = getHash(targetId);

    return !!(hash1 && hash2 && hash1 === hash2);
  }

  function buildThreadItems(posts: Post[]): ThreadItemType[] {
    const items: ThreadItemType[] = [];
    const anchorIndex = posts.findIndex(p => p.id === anchorPostId);
    if (anchorIndex === -1) {
      return items;
    }

    const anchorPost = posts[anchorIndex];

    // Find parent posts by following the chain
    const parentPosts: Post[] = [];
    let currentPost = anchorPost;

    // First follow parentCommentId chain for nested comments
    while (currentPost.parentCommentId) {
      const parentId = currentPost.parentCommentId;
      const parent = posts.find(p => matchesPostId(p.id, parentId));
      if (!parent) {break;}
      parentPosts.unshift(parent);
      currentPost = parent;
    }

    // Then check if there's an original post (for comments on external posts)
    // Get the topmost parent or the anchor itself
    const topmostPost = parentPosts.length > 0 ? parentPosts[0] : anchorPost;
    if (topmostPost.originalPostId && anchorPost.type !== 'quote') {
      const originalPost = posts.find(p => matchesPostId(p.id, topmostPost.originalPostId));
      if (originalPost) {
        // Add original post at the beginning of the parent chain
        parentPosts.unshift(originalPost);
      }
    }

    // Add parent posts (keep original order for thread context)
    parentPosts.forEach((post, index) => {
      items.push({
        type: 'post',
        key: post.id,
        depth: index - parentPosts.length,
        data: post
      });
    });

    // Add anchor post
    items.push({
      type: 'anchor',
      key: anchorPost.id,
      depth: 0,
      data: anchorPost
    });

    // Get reply posts and sort them
    const replyPosts = posts.filter(p =>
      matchesPostId(p.originalPostId, anchorPostId) ||
        matchesPostId(p.parentCommentId, anchorPostId)
    );
    const sortedReplies = sortPosts(replyPosts, sort);

    // Add sorted reply posts
    sortedReplies.forEach((post, index) => {
      items.push({
        type: 'post',
        key: post.id,
        depth: index + 1,
        data: post
      });
    });

    return items;
  }

  function handleFullscreen(event: CustomEvent<Post>) {
    const post = event.detail;
    const allPosts = threadItems.filter(item => item.data).map(item => item.data as Post);
    fullscreenPostIndex = allPosts.findIndex(p => p.id === post.id);
    if (fullscreenPostIndex === -1) {
      fullscreenPostIndex = 0;
    }
    fullscreenPost = post;
    api.toggleZenMode();
  }

  function handleCloseFullscreen() {
    fullscreenPost = null;
    api.toggleZenMode();
  }

</script>

<div class="flex flex-col h-full">

  {#if loading}
    <div class="flex-1 pl-3 overflow-y-auto">
      <!-- Simple loading skeletons using utility classes -->
      {#each [1, 2, 3] as _}
        <div class="card pad border rounded mb-2">
          <div class="flex gap-2 mb-2">
            <div class="w-10 h-10 rounded-full bg-muted opacity-50"></div>
            <div class="flex-1">
              <div class="h-3 bg-muted opacity-50 rounded mb-1 w-32"></div>
              <div class="h-2 bg-muted opacity-50 rounded w-20"></div>
            </div>
          </div>
          <div class="h-3 bg-muted opacity-50 rounded mb-1"></div>
          <div class="h-3 bg-muted opacity-50 rounded w-3/4"></div>
        </div>
      {/each}
    </div>
  {:else if error}
    <div class="p-3 text-error text-center">
      <p class="mb-3">{error}</p>
      <button class="btn" on:click={fetchThread}>Retry</button>
    </div>
  {:else}
    <div class="flex-1 pl-3 overflow-y-auto">
      {#each threadItems as item (item.key)}
        {#if item.type === 'post' || item.type === 'anchor'}
          {#if item.data}
            <div class="relative mb-2" class:border-l-4={item.type === 'anchor'} class:border-primary={item.type === 'anchor'}>
              {#if item.type !== 'anchor' && item.depth !== threadItems[threadItems.length - 1].depth}
                <div class="thread-connector"></div>
              {/if}
              <PostCard
                post={item.data}
                clickable={item.type !== 'anchor'}
                expandContent={item.type === 'anchor'}
                anchorPostId={anchorPostId}
                on:fullscreen={handleFullscreen} />
            </div>
          {/if}
        {:else if item.type === 'readMore'}
          <button class="btn ghost w-full mb-2" on:click={item.onLoadMore}>
            {#if item.depth < 0}
              Load more parents...
            {:else}
              Load more replies...
            {/if}
          </button>
        {:else if item.type === 'skeleton'}
          <div class="card pad border rounded mb-2">
            <div class="flex gap-2 mb-2">
              <div class="w-10 h-10 rounded-full bg-muted opacity-50"></div>
              <div class="flex-1">
                <div class="h-3 bg-muted opacity-50 rounded mb-1 w-32"></div>
                <div class="h-2 bg-muted opacity-50 rounded w-20"></div>
              </div>
            </div>
            <div class="h-3 bg-muted opacity-50 rounded mb-1"></div>
            <div class="h-3 bg-muted opacity-50 rounded w-3/4"></div>
          </div>
        {:else if item.type === 'blocked'}
          <div class="card pad border rounded mb-2 flex items-center gap-2 text-muted">
            <span>🚫</span>
            <span>This post is blocked</span>
          </div>
        {:else if item.type === 'notFound'}
          <div class="card pad border rounded mb-2 flex items-center gap-2 text-error">
            <span>❌</span>
            <span>Post not found</span>
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

{#if fullscreenPost}
  <FullscreenPostViewer
    posts={threadItems.filter(item => item.data).map(item => item.data)}
    currentIndex={fullscreenPostIndex}
    on:close={handleCloseFullscreen} />
{/if}
