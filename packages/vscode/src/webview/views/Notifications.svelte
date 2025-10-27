<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';
  import type { Notification, Post } from '@gitsocial/core/client';

  let notifications: Notification[] = [];
  let loading = true;
  let error: string | null = null;
  let parentPosts: Map<string, Post> = new Map();

  // Combine all posts for PostCard's resolution
  $: allPosts = (() => {
    const posts: Post[] = [];
    // Add all parent posts
    for (const [_, post] of parentPosts) {
      posts.push(post);
    }
    // Add all notification posts
    for (const notif of notifications) {
      posts.push(notif.post);
    }
    return posts;
  })();

  onMount(() => {
    loadNotifications();

    window.addEventListener('message', event => {
      const message = event.data;
      switch (message.type) {
        case 'notifications':
          notifications = message.data || [];
          loading = false;
          error = null;
          // Fetch parent posts for comment notifications
          fetchParentPosts(notifications);
          break;
        case 'posts':
          // Handle parent posts response
          if (message.requestId?.startsWith('parent-posts-')) {
            const posts = message.data || [];
            for (const post of posts) {
              parentPosts.set(post.id, post);
            }
            // Trigger reactivity
            parentPosts = parentPosts;
          }
          break;
        case 'error':
          error = message.error || 'Failed to load notifications';
          loading = false;
          break;
        case 'refresh':
          loadNotifications();
          break;
      }
    });

    api.ready();
  });

  function loadNotifications() {
    loading = true;
    error = null;
    api.getNotifications();
  }

  function handleRefresh() {
    loadNotifications();
  }

  function getNotificationTitle(notification: Notification): string {
    const actorName = notification.actor.name || 'Someone';
    switch (notification.type) {
      case 'comment':
        return `${actorName} commented on your post`;
      case 'repost':
        return `${actorName} reposted your post`;
      case 'quote':
        return `${actorName} quoted your post`;
      default:
        return `${actorName} interacted with your post`;
    }
  }

  function getNotificationIcon(type: string): string {
    switch (type) {
      case 'comment':
        return 'codicon-comment';
      case 'repost':
        return 'codicon-sync';
      case 'quote':
        return 'codicon-quote';
      default:
        return 'codicon-bell';
    }
  }

  function fetchParentPosts(notifs: Notification[]) {
    // Collect parent post IDs for comment notifications
    const neededIds = new Set<string>();
    for (const notif of notifs) {
      if (notif.type === 'comment' && notif.targetPostId) {
        neededIds.add(notif.targetPostId);
      }
    }

    // Fetch missing parent posts
    if (neededIds.size > 0) {
      const parentRequestId = `parent-posts-${Date.now()}`;
      api.getPosts({
        scope: `byId:${Array.from(neededIds).join(',')}`,
        skipCache: false
      }, parentRequestId);
    }
  }
</script>

<div>
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar flex justify-between items-center mb-6">
    <h1><span class="codicon codicon-lg codicon-bell mr-2"></span>Notifications</h1>
    <button class="btn" on:click={handleRefresh} title="Refresh">
      <span class="codicon codicon-refresh"></span>
      Refresh
    </button>
  </div>

  {#if loading}
    <div class="flex flex-col items-center justify-center gap-2 py-12 text-center">
      <span class="codicon codicon-loading codicon-modifier-spin text-2xl opacity-50"></span>
      <span>Loading notifications...</span>
    </div>
  {:else if error}
    <div class="flex flex-col items-center justify-center gap-2 py-12 text-center text-error">
      <span class="codicon codicon-error text-2xl opacity-50"></span>
      <span>{error}</span>
    </div>
  {:else if notifications.length === 0}
    <div class="flex flex-col items-center justify-center gap-2 py-12 text-center">
      <span class="codicon codicon-bell-slash text-2xl opacity-50"></span>
      <p class="m-1">No notifications yet</p>
      <p class="m-1 text-muted text-sm">When someone interacts with your posts, you'll see it here</p>
    </div>
  {:else}
    <div class="flex flex-col gap-4">
      {#each notifications as notification}
        <div class="">
          <div class="flex items-center mb-3 pb-1 border-b text-muted text-sm">
            <span class="codicon {getNotificationIcon(notification.type)} mr-2"></span>
            <span class="italic">{getNotificationTitle(notification)}</span>
          </div>
          {#if notification.type === 'comment' && notification.targetPostId && parentPosts.has(notification.targetPostId)}
            {@const parentPost = parentPosts.get(notification.targetPostId)}
            {@const parentRepo = parentPost?.repository.split('#')[0]}
            {@const commentRepo = notification.post.repository.split('#')[0]}
            {@const isSameRepo = parentRepo && commentRepo && parentRepo === commentRepo}
            {@const localTargetId = isSameRepo && notification.targetPostId.includes('#')
              ? '#' + notification.targetPostId.split('#')[1]
              : notification.targetPostId}
            {@const commentAsQuote = { ...notification.post, type: 'quote', originalPostId: localTargetId }}
            <PostCard post={commentAsQuote} posts={allPosts} />
          {:else}
            <PostCard post={notification.post} posts={allPosts} />
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
