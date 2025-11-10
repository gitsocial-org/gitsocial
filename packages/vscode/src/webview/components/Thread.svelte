<script lang="ts">
  import { onMount } from 'svelte';
  import type { Post, ThreadItem as ThreadItemType, ThreadSort } from '@gitsocial/core/client';
  import { matchesPostId, calculateDepth, buildParentChildMap, sortThreadTree } from '@gitsocial/core/client';
  import PostCard from '../components/PostCard.svelte';
  import FullscreenPostViewer from '../components/FullscreenPostViewer.svelte';
  import { api } from '../api';
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

  let collapsedPosts: Set<string> = new Set();

  function toggleCollapse(postKey: string) {
    if (collapsedPosts.has(postKey)) {
      collapsedPosts.delete(postKey);
    } else {
      collapsedPosts.add(postKey);
    }
    collapsedPosts = collapsedPosts;
  }

  function isCollapsed(postKey: string): boolean {
    return collapsedPosts.has(postKey);
  }

  function getHiddenCount(postKey: string, items: ThreadItemType[]): number {
    const idx = items.findIndex(i => i.key === postKey);
    if (idx === -1) {return 0;}
    const parentDepth = items[idx].depth;
    let count = 0;
    for (let i = idx + 1; i < items.length; i++) {
      if (items[i].depth <= parentDepth) {break;}
      if (items[i].data) {count++;}
    }
    return count;
  }

  $: visibleThreadItems = threadItems.filter((item, idx) => {
    if (!item.data || item.type === 'anchor') {return true;}
    for (let i = idx - 1; i >= 0; i--) {
      const prev = threadItems[i];
      if (prev.depth < item.depth && collapsedPosts.has(prev.key)) {return false;}
      if (prev.type === 'anchor') {break;}
    }
    return true;
  });

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

  function buildThreadItems(posts: Post[]): ThreadItemType[] {
    const items: ThreadItemType[] = [];
    const anchorIndex = posts.findIndex(p => matchesPostId(p.id, anchorPostId));
    if (anchorIndex === -1) {
      return items;
    }

    const anchorPost = posts[anchorIndex];
    const maxDepth = 8;

    // Build parent-child map once for O(1) lookups
    const parentChildMap = buildParentChildMap(posts);

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
    parentPosts.forEach((post) => {
      const rawDepth = calculateDepth(post, anchorPost, posts);
      const depth = Math.max(-maxDepth, rawDepth);
      const hasChildren = parentChildMap.has(post.id);
      items.push({
        type: 'post',
        key: post.id,
        depth,
        data: post,
        hasChildren
      });
    });

    // Add anchor post
    items.push({
      type: 'anchor',
      key: anchorPost.id,
      depth: 0,
      data: anchorPost
    });

    // Get all descendant posts with tree-aware sorting (maintains parent-child visual order)
    const sortedReplies = sortThreadTree(anchorPostId, posts, sort, 1);

    // Add sorted reply posts
    sortedReplies.forEach((post) => {
      const rawDepth = calculateDepth(post, anchorPost, posts);
      const depth = Math.min(maxDepth, rawDepth);
      const hasChildren = parentChildMap.has(post.id);
      items.push({
        type: 'post',
        key: post.id,
        depth,
        data: post,
        hasChildren
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
    <div class="flex-1 overflow-y-auto">
      <!-- Simple loading skeletons using utility classes -->
      {#each [1, 2, 3] as _}
        <div class="card p-3 border rounded mb-2">
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
    <div class="flex-1 overflow-y-auto">
      {#each visibleThreadItems as item (item.key)}
        {#if item.type === 'post' || item.type === 'anchor'}
          {#if item.data}
            <div class="thread-item relative mb-2 thread-item-depth-{item.depth}">
              <div class="flex">
                {#if item.hasChildren && item.depth >= 0}
                  <button
                    class="btn ghost icon h-10 mt-3"
                    on:click={(e) => { e.stopPropagation(); toggleCollapse(item.key); }}
                    aria-label="{isCollapsed(item.key) ? 'Expand' : 'Collapse'} replies">
                    <span class="codicon codicon-chevron-{isCollapsed(item.key) ? 'right' : 'down'}"></span>
                  </button>
                {:else if item.type !== 'anchor' && item.depth >= 0}
                  <div class="w-7"></div>
                {/if}

                <div class="flex-1">
                  <div class="relative" class:border-l={item.type === 'anchor'}>
                    <PostCard
                      post={item.data}
                      clickable={item.type !== 'anchor'}
                      expandContent={item.type === 'anchor'}
                      collapsed={isCollapsed(item.key)}
                      anchorPostId={anchorPostId}
                      on:fullscreen={handleFullscreen} />
                  </div>

                  {#if isCollapsed(item.key)}
                    <button
                      class="btn subtle xs wide mt-1 mb-2"
                      on:click={(e) => { e.stopPropagation(); toggleCollapse(item.key); }}>
                      {getHiddenCount(item.key, threadItems)} more
                    </button>
                  {/if}
                </div>
              </div>
            </div>
          {/if}
        {:else if item.type === 'skeleton'}
          <div class="card p-3 border rounded mb-2">
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
          <div class="card p-3 border rounded mb-2 flex items-center gap-2 text-muted">
            <span>🚫</span>
            <span>This post is blocked</span>
          </div>
        {:else if item.type === 'notFound'}
          <div class="card p-3 border rounded mb-2 flex items-center gap-2 text-error">
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
