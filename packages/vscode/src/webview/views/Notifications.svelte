<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import PostCard from '../components/PostCard.svelte';
  import DateNavigation from '../components/DateNavigation.svelte';
  import type { Notification, Post } from '@gitsocial/core/client';
  import { gitHost, gitMsgRef } from '@gitsocial/core/client';
  import { getWeekStart, getWeekEnd, getWeekLabel } from '../utils/time';

  let notifications: Notification[] = [];
  let loading = true;
  let refreshLoading = false;
  let error: string | null = null;
  let posts: Record<string, Post> = {};
  let weekOffset = 0;

  $: weekStart = getWeekStart(weekOffset);
  $: weekEnd = getWeekEnd(weekOffset);
  $: weekLabel = getWeekLabel(weekOffset);

  function matchesPostId(postId: string | undefined, targetId: string): boolean {
    if (!postId) {return false;}
    if (postId === targetId) {return true;}
    const parsedPost = gitMsgRef.parse(postId);
    const parsedTarget = gitMsgRef.parse(targetId);
    if (parsedPost.type === 'commit' && parsedTarget.type === 'commit' &&
      parsedPost.value === parsedTarget.value) {
      return true;
    }
    return false;
  }

  onMount(() => {
    api.getNotifications({
      since: weekStart,
      until: weekEnd
    });

    window.addEventListener('message', event => {
      const message = event.data;
      switch (message.type) {
        case 'notifications':
          notifications = message.data || [];
          loading = false;
          refreshLoading = false;
          error = null;
          // Fetch posts for comment/repost/quote notifications
          fetchNotificationPosts(notifications);
          break;
        case 'posts':
          if (message.requestId?.startsWith('notification-interaction-')) {
            const fetchedPosts = message.data || [];
            for (const post of fetchedPosts) {
              posts[post.id] = post;
            }
            posts = posts;
            fetchOriginalPosts(fetchedPosts);
          } else if (message.requestId?.startsWith('notification-original-')) {
            const fetchedPosts = message.data || [];
            for (const post of fetchedPosts) {
              posts[post.id] = post;
            }
            posts = posts;
          }
          break;
        case 'error':
          error = message.error || 'Failed to load notifications';
          loading = false;
          refreshLoading = false;
          break;
        case 'refresh':
          loadNotifications();
          break;
      }
    });

    api.ready();
  });

  function loadNotifications() {
    refreshLoading = true;
    error = null;
    api.getNotifications({
      since: weekStart,
      until: weekEnd
    });
  }

  function goToPreviousWeek() {
    weekOffset -= 1;
    loading = true;
    error = null;
    api.getNotifications({
      since: getWeekStart(weekOffset),
      until: getWeekEnd(weekOffset)
    });
  }

  function goToNextWeek() {
    weekOffset += 1;
    loading = true;
    error = null;
    api.getNotifications({
      since: getWeekStart(weekOffset),
      until: getWeekEnd(weekOffset)
    });
  }

  function getNotificationIcon(type: string): string {
    switch (type) {
      case 'comment':
        return 'codicon-comment';
      case 'repost':
        return 'codicon-sync';
      case 'quote':
        return 'codicon-quote';
      case 'follow':
        return 'codicon-person-add';
      default:
        return 'codicon-bell';
    }
  }

  function createSyntheticFollowPost(notification: Notification): Post {
    const [repositoryUrl, commitPart] = notification.commitId.split('#');
    const commitHash = commitPart.replace('commit:', '');
    const repositoryName = gitHost.getDisplayName(repositoryUrl);
    const commitUrl = gitHost.getCommitUrl(repositoryUrl, commitHash);
    const author = notification.commit?.author || repositoryName;
    const email = notification.commit?.email || '';
    const timestamp = notification.commit?.timestamp ? new Date(notification.commit.timestamp) : new Date();
    return {
      id: notification.commitId,
      repository: repositoryUrl,
      author: {
        name: author,
        email
      },
      timestamp,
      content: 'Added you to a list',
      type: 'post',
      source: 'explicit',
      raw: {
        commit: {
          hash: commitHash,
          message: 'Added you to a list',
          author,
          email,
          timestamp
        }
      },
      display: {
        repositoryName,
        commitHash: commitHash.substring(0, 12),
        commitUrl,
        totalReposts: 0,
        isEmpty: false,
        isUnpushed: false,
        isOrigin: false,
        isWorkspacePost: false
      }
    };
  }
  function fetchNotificationPosts(notifs: Notification[]) {
    const followNotifs = notifs.filter(n => n.type === 'follow');
    for (const notif of followNotifs) {
      posts[notif.commitId] = createSyntheticFollowPost(notif);
    }
    posts = posts;
    const postIds = notifs.filter(n => n.type !== 'follow').map(n => n.commitId);
    if (postIds.length > 0) {
      const requestId = `notification-interaction-${Date.now()}`;
      api.getPosts({
        scope: `byId:${postIds.join(',')}`,
        skipCache: false
      }, requestId);
    }
  }
  function fetchOriginalPosts(fetchedPosts: Post[]) {
    const parentPostIds = fetchedPosts
      .map(p => {
        if (p.type === 'comment') {
          return p.parentCommentId || p.originalPostId;
        }
        return undefined;
      })
      .filter((id): id is string => !!id && !(id in posts));
    if (parentPostIds.length > 0) {
      const requestId = `notification-original-${Date.now()}`;
      api.getPosts({
        scope: `byId:${parentPostIds.join(',')}`,
        skipCache: false
      }, requestId);
    }
  }
</script>

<div>
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar mb-6">
    <div class="flex justify-between items-center">
      <h1><span class="codicon codicon-lg codicon-bell mr-2"></span>Notifications</h1>
      <DateNavigation
        offset={weekOffset}
        label={weekLabel}
        loading={loading}
        onPrevious={goToPreviousWeek}
        onNext={goToNextWeek}
        onRefresh={loadNotifications}
        refreshLoading={refreshLoading}
      />
    </div>
  </div>

  {#if loading}
    <div class="flex flex-col items-center justify-center gap-2 py-12 text-center">
      <span class="codicon codicon-loading codicon-modifier-spin text-2xl opacity-50"></span>
      <span>Checking all followed repositories for interactions...</span>
      <span class="text-sm text-muted">{weekLabel}</span>
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
      <p class="m-1 text-muted text-sm">
        Comments, reposts, quotes, and new followers from repositories you follow will appear here
      </p>
    </div>
  {:else}
    <div class="flex flex-col gap-4">
      {#each notifications as notification}
        {@const post = posts[notification.commitId]}
        <div class="">
          <div class="flex items-center mb-3 pb-1 border-b text-muted text-sm">
            <span class="codicon {getNotificationIcon(notification.type)} mr-2"></span>
            <span class="italic">
              {#if notification.type === 'comment'}
                {post?.author.name || 'Someone'} commented on your post
              {:else if notification.type === 'repost'}
                {post?.author.name || 'Someone'} reposted your post
              {:else if notification.type === 'quote'}
                {post?.author.name || 'Someone'} quoted your post
              {:else if notification.type === 'follow'}
                {post?.display.repositoryName || 'Repository'} started following you
              {/if}
            </span>
          </div>
          {#if post}
            {#if notification.type === 'comment'}
              {@const parentPostId = post.parentCommentId || post.originalPostId}
              {@const parentPost = parentPostId
                ? Object.values(posts).find(p => matchesPostId(p.id, parentPostId))
                : undefined}
              {#if parentPost}
                {@const parentRepo = parentPost?.repository.split('#')[0]}
                {@const commentRepo = post.repository.split('#')[0]}
                {@const isSameRepo = parentRepo && commentRepo && parentRepo === commentRepo}
                {@const localTargetId = isSameRepo && parentPostId.includes('#')
                  ? '#' + parentPostId.split('#')[1]
                  : parentPostId}
                {@const commentAsQuote = { ...post, type: 'quote', originalPostId: localTargetId }}
                <PostCard post={commentAsQuote} />
              {:else}
                <PostCard
                  post={post}
                  clickable={true}
                  interactive={true} />
              {/if}
            {:else}
              <PostCard post={post} />
            {/if}
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
