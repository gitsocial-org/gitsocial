<script lang="ts">
  import type { Post } from '@gitsocial/core/client';
  import { gitHost } from '@gitsocial/core/client';
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';
  import Avatar from './Avatar.svelte';
  import Dialog from './Dialog.svelte';
  import { formatRelativeTime, useEventDrivenTimeUpdates } from '../utils/time';
  import { createInteractionHandler } from '../utils/interactions';
  import { parseMarkdown, extractImages, configureMarked } from '../utils/markdown';

  export let post: Post;  // Post object
  export let posts: Map<string, Post> | Post[] = [];  // Optional posts collection for optimization
  export let displayMode: 'main' | 'context' | 'reply' | 'nested' | 'preview' | 'full' = 'preview';
  export let depth = 0;
  export let isNested = false;
  export let showParentContext = false;
  export let isEmbedded = false;
  export let anchorPostId: string | undefined = undefined;  // Optional: the ID of the main post being viewed
  export let isAnchorPost = false;  // True only for main post in Post view

  // Note: post is now always a Post object, no string ID handling needed

  // Raw view toggle state
  let showRawView = false;

  // Context-aware display logic
  $: showFullContent = post && (displayMode === 'main' || displayMode === 'reply' || displayMode === 'full');
  $: _compactLayout = displayMode === 'nested';
  $: showInteractions = !_compactLayout;
  $: isMainPost = displayMode === 'main';
  $: showParentContextComputed = post && (post.originalPostId || post.parentCommentId) && showParentContext;

  // Markdown and image processing (on-demand)
  $: images = extractImages(post?.content || '');
  let parsedHtml = '';
  $: if (isAnchorPost && post?.content) {
    const result = parseMarkdown(post.content);
    parsedHtml = result.html;
  }

  // Image lightbox state
  let selectedImageIndex: number | null = null;

  // Handle keyboard navigation for lightbox
  function handleLightboxKeys(event: KeyboardEvent) {
    if (selectedImageIndex === null) {return;}

    if (event.key === 'Escape') {
      selectedImageIndex = null;
    } else if (event.key === 'ArrowLeft') {
      navigatePrevious();
    } else if (event.key === 'ArrowRight') {
      navigateNext();
    }
  }

  function navigatePrevious() {
    if (selectedImageIndex !== null && selectedImageIndex > 0) {
      selectedImageIndex--;
    }
  }

  function navigateNext() {
    if (selectedImageIndex !== null && selectedImageIndex < images.length - 1) {
      selectedImageIndex++;
    }
  }

  $: if (selectedImageIndex !== null) {
    window.addEventListener('keydown', handleLightboxKeys);
  } else {
    window.removeEventListener('keydown', handleLightboxKeys);
  }

  // Self-contained interaction handling
  const {
    state: interactionState,
    handleInteraction: handleInteractionClick,
    handleInteractionSubmit,
    handleInteractionCancel,
    setupMessageListeners
  } = createInteractionHandler();

  let interactionText = '';
  let textareaElement: HTMLTextAreaElement;

  let cleanup: (() => void) | null = null;

  // Missing post resolution state
  let resolvedPosts: Map<string, Post> = new Map();
  let loadingPosts: Set<string> = new Set();
  let failedPosts: Set<string> = new Set();

  // Unique component identifier to prevent cross-contamination
  const componentId = `postcard-${Math.random().toString(36).substr(2, 9)}`;

  // Reactive trigger for time updates
  let timeUpdateTrigger = 0;

  // Force re-render of time displays when trigger changes
  $: formattedTime = timeUpdateTrigger >= 0 && post ? formatRelativeTime(post.timestamp) : '';

  onMount(() => {
    // Initialize marked configuration once
    configureMarked();
    cleanup = setupMessageListeners();

    // Set up event-driven time updates
    const timeUpdateCleanup = useEventDrivenTimeUpdates(() => {
      timeUpdateTrigger++;
    });

    // Set up message handling for resolved posts
    const handleMessage = (event: MessageEvent) => {
      const message = event.data;

      if (message.type === 'posts' && message.requestId && message.requestId.startsWith('resolve-post::')) {
        const parts = message.requestId.split('::');
        const postId = parts[1];
        const msgComponentId = parts[2];

        // Only process messages for this component instance
        if (msgComponentId !== componentId) {
          return;
        }

        const posts = message.data || [];

        loadingPosts.delete(postId);
        loadingPosts = loadingPosts; // Trigger reactivity

        if (posts.length > 0) {
          resolvedPosts.set(postId, posts[0]);
          resolvedPosts = resolvedPosts; // Trigger reactivity
        } else {
          failedPosts.add(postId);
          failedPosts = failedPosts; // Trigger reactivity
        }
      }

      if (message.type === 'error' && message.requestId && message.requestId.startsWith('resolve-post::')) {
        const parts = message.requestId.split('::');
        const postId = parts[1];
        const msgComponentId = parts[2];

        // Only process messages for this component instance
        if (msgComponentId !== componentId) {
          return;
        }

        loadingPosts.delete(postId);
        loadingPosts = loadingPosts;
        failedPosts.add(postId);
        failedPosts = failedPosts;
      }
    };

    window.addEventListener('message', handleMessage);

    // Enhanced cleanup
    const originalCleanup = cleanup;
    cleanup = () => {
      window.removeEventListener('message', handleMessage);
      timeUpdateCleanup();
      if (originalCleanup) {
        originalCleanup();
      }
    };
  });

  onDestroy(() => {
    if (cleanup) {
      cleanup();
    }
    window.removeEventListener('keydown', handleLightboxKeys);
  });

  function handleViewPost(): void {
    if (!isMainPost && !isNested && !isEmbedded) {
      // Simply use post.id directly - it's already correct (relative for workspace, absolute for external)
      api.openView('viewPost', post.content.split('\n')[0].substring(0, 30) + '...', {
        postId: post.id,
        repository: post.repository
      });
    }
  }

  function _handleViewRepository(): void {
    api.openView('repository', post.display.repositoryName, {
      repository: post.repository
    });
  }

  // Disable buttons when dialog is open or submitting
  $: buttonsDisabled = $interactionState.selectedPost !== null || $interactionState.isSubmitting;

  // Create event handler for interaction buttons
  function handleInteraction(type: 'comment' | 'repost'): (event: Event) => void {
    return (event: Event) => {
      if (buttonsDisabled) {
        return;
      }
      event.stopPropagation();
      handleInteractionClick(new CustomEvent('interaction', { detail: { post, type } }));
    };
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleViewPost();
    }
  }

  // Resolve missing post from backend
  function resolveMissingPost(postId: string): void {
    if (loadingPosts.has(postId) || resolvedPosts.has(postId) || failedPosts.has(postId)) {
      return;
    }

    loadingPosts.add(postId);
    loadingPosts = loadingPosts; // Trigger reactivity

    const requestId = `resolve-post::${postId}::${componentId}::${Date.now()}`;
    api.getPosts({ scope: `byId:${postId}` }, requestId);
  }

  // Get resolved post (try frontend first, then resolved cache)
  function getResolvedPost(postId: string): Post | null {
    // Try frontend posts collection first
    const frontendPost = Array.isArray(posts)
      ? posts.find(p => p.id === postId)
      : posts.get ? posts.get(postId) : null;

    if (frontendPost) {
      return frontendPost;
    }

    // Try resolved posts cache
    return resolvedPosts.get(postId) || null;
  }

  // Reactive: auto-resolve missing original posts (only for quote/repost types)
  $: if (post && post.originalPostId && (post.type === 'quote' || post.type === 'repost')) {
    const resolved = getResolvedPost(post.originalPostId);
    if (!resolved && !loadingPosts.has(post.originalPostId) && !failedPosts.has(post.originalPostId)) {
      resolveMissingPost(post.originalPostId);
    }
  }

  // Reactive: auto-resolve missing parent posts (only when showParentContext is true)
  $: if (post && post.parentCommentId && showParentContext) {
    const resolved = getResolvedPost(post.parentCommentId);
    if (!resolved && !loadingPosts.has(post.parentCommentId) && !failedPosts.has(post.parentCommentId)) {
      resolveMissingPost(post.parentCommentId);
    }
  }

</script>

{#if post}
  {#if post.type === 'repost'}
    <div class="card hover {post.display.isUnpushed ? 'border-warning' : ''}">
      <div class="ml-12 mt-2 -mb-2 text-sm text-muted font-bold relative z-10 cursor-pointer subtle hover-underline"
        role="button" tabindex="0"
        on:click={handleViewPost} on:keydown={isMainPost ? undefined : handleKeydown}>
        <span class="codicon codicon-2xs codicon-arrow-swap mr-1"></span>
        <span>{post.author.name} reposted</span>
        <span>·</span>
        <span class="italic cursor-help" title={new Date(post.timestamp).toLocaleString()}>
          {formattedTime}
        </span>
      </div>
      {#if post.originalPostId}
        {@const originalPost = getResolvedPost(post.originalPostId)}
        {#if originalPost}
          <svelte:self
            post={originalPost}
            {posts}
            displayMode="preview"
            depth={depth + 1}
            isNested={false} />
        {:else if loadingPosts.has(post.originalPostId)}
          <div class="p-3 text-center text-muted">
            <span class="codicon codicon-loading spin"></span>
            <span class="ml-2">Loading original post...</span>
          </div>
        {:else}
          <div>
            <span class="text-muted">Original post not available</span>
          </div>
        {/if}
      {:else}
        <div>
          <span class="text-muted">Original post not available</span>
        </div>
      {/if}
    </div>
  {:else}
    <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
    <div class="card {isMainPost || isNested ? '' : 'hover'} {isNested ? 'p-2' : 'pad'} {post.type} {displayMode} {post.display.isUnpushed ? 'border-warning' : ''}"
      tabindex={isMainPost || isNested ? -1 : 0}
      role={isMainPost || isNested ? 'article' : 'button'}
      on:click={isMainPost || isNested ? undefined : handleViewPost}
      on:keydown={isMainPost || isNested ? undefined : handleKeydown}>

      {#if isMainPost}
        <!-- Main Post Layout - Full Width Content -->
        <div class="w-full">
          <!-- Header with Avatar -->
          <div class="flex items-center justify-between gap-3 mb-3">
            <div class="flex items-center gap-3">
              <Avatar
                type="user"
                identifier={post.author.email}
                name={post.author.name}
                repository={post.repository}
                size={40}
              />
              <div class="flex items-center gap-2">
                <span class="font-bold text-lg">{post.author.name}</span>
                <span>·</span>
                <span class="text-sm text-muted">{post.author.email}</span>
                <span>·</span>
                <span class="text-sm text-muted italic cursor-help" title={new Date(post.timestamp).toLocaleString()}>
                  {formattedTime}
                </span>
              </div>
            </div>
            <!-- Raw View Toggle Button (top right) -->
            {#if isAnchorPost && (parsedHtml || images.length > 0)}
              <button
                class="btn ghost sm {showRawView ? 'active' : ''}"
                on:click={() => showRawView = !showRawView}
                title="{showRawView ? 'Show rendered view' : 'Show raw view'}">
                <span class="codicon codicon-code"></span>
                <span>Raw</span>
              </button>
            {/if}
          </div>
          <!-- Full Width Content -->

          <!-- Content -->
          <div class="post-content {isMainPost ? 'mb-5' : 'mb-3'}">
            {#if showRawView}
              <!-- Raw text view -->
              <div class="whitespace-pre-wrap py-3">{post.content}</div>
            {:else if isAnchorPost && parsedHtml}
              <!-- Full markdown for anchor post -->
              <div class="markdown-content {isMainPost ? 'text-lg' : ''}">
                <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                {@html parsedHtml}
              </div>
            {:else if showFullContent || post.type !== 'comment'}
              <!-- Plain text for other posts -->
              {#if post.content.split('\n').length > 0}
                <div class="font-bold break-words {isMainPost ? 'text-xl' : ''}">{post.content.split('\n')[0]}</div>
                {#if post.content.split('\n').slice(1).join('\n').trim()}
                  <div class="whitespace-pre-wrap break-words {isMainPost ? 'text-lg mt-2' : 'mt-1'}">{post.content.split('\n').slice(1).join('\n').trim()}</div>
                {/if}
              {/if}
            {:else}
              <div class="font-bold break-words {isMainPost ? 'text-xl' : ''}">{post.content.split('\n')[0]}</div>
              {#if post.content.split('\n').length > 1}
                <span class="text-muted">...</span>
              {/if}
            {/if}

            <!-- Images for ALL posts (lazy loaded) -->
            {#if !showRawView && images.length > 0}
              <div class="image-gallery mt-3">
                {#each images as url, index}
                  <button
                    class="image-button"
                    on:click={(e) => { e.stopPropagation(); selectedImageIndex = index; }}
                    aria-label="View larger image">
                    <img
                      src={url}
                      alt=""
                      loading="lazy"
                      class="gallery-image"
                      on:error={(e) => e.currentTarget.style.display = 'none'} />
                  </button>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Quoted Content -->
          {#if post.type === 'quote' && post.originalPostId !== anchorPostId}
            <div class="mb-3">
              <div class="card ghost border border-link rounded p-2 cursor-pointer"
                role="button"
                tabindex="0"
                on:click={(e) => {
                  e.stopPropagation();
                  if (post.originalPostId) {
                    const originalPost = getResolvedPost(post.originalPostId);
                    const postIdToUse = originalPost ? originalPost.id : post.originalPostId;
                    api.openView('viewPost', 'Post', { postId: postIdToUse });
                  }
                }}
                on:keydown={(e) => {
                  if (e.key === 'Enter') {
                    e.stopPropagation();
                    if (post.originalPostId) {
                      api.openView('viewPost', 'Post', { postId: post.originalPostId });
                    }
                  }
                }}
                title="View quoted post">
                {#if post.originalPostId}
                  {@const originalPost = getResolvedPost(post.originalPostId)}
                  {#if originalPost}
                    <svelte:self
                      post={originalPost}
                      {posts}
                      displayMode="preview"
                      isNested={true}
                      isEmbedded={true}
                      {anchorPostId} />
                  {:else if loadingPosts.has(post.originalPostId)}
                    <div class="text-center text-muted p-2">
                      <span class="codicon codicon-loading spin"></span>
                      <span class="ml-2 text-sm">Loading quoted post...</span>
                    </div>
                  {:else}
                    <span class="text-muted text-sm">Original post not available</span>
                  {/if}
                {:else}
                  <span class="text-muted text-sm">Original post not available</span>
                {/if}
              </div>
            </div>
          {/if}

          <!-- Parent Context for Comments -->
          {#if showParentContextComputed && (post.originalPostId || post.parentCommentId)}
            {@const parentId = post.originalPostId || post.parentCommentId}
            {@const parentPost = parentId ? getResolvedPost(parentId) : null}
            <div class="parent-context mb-3">
              <div class="card ghost border border-link rounded p-2 cursor-pointer"
                role="button"
                tabindex="0"
                on:click={(e) => {
                  e.stopPropagation();
                  if (parentId) {
                    const parentPost = getResolvedPost(parentId);
                    const postIdToUse = parentPost ? parentPost.id : parentId;
                    api.openView('viewPost', 'Post', { postId: postIdToUse });
                  }
                }}
                on:keydown={(e) => {
                  if (e.key === 'Enter') {
                    e.stopPropagation();
                    if (parentId) {
                      api.openView('viewPost', 'Post', { postId: parentId });
                    }
                  }
                }}
                title="View parent post">
                {#if parentPost}
                  <svelte:self
                    post={parentPost}
                    {posts}
                    displayMode="preview"
                    isNested={true}
                    isEmbedded={true} />
                {:else if parentId && loadingPosts.has(parentId)}
                  <div class="text-center text-muted p-2">
                    <span class="codicon codicon-loading spin"></span>
                    <span class="ml-2 text-sm">Loading parent post...</span>
                  </div>
                {:else}
                  <span class="text-muted text-sm">Replying to a post that is not available</span>
                {/if}
              </div>
            </div>
          {/if}

          <!-- Actions -->
          {#if showInteractions}
            <div class="flex gap-4">
              <button
                class="btn ghost subtle sm "
                class:disabled={buttonsDisabled}
                disabled={buttonsDisabled}
                on:click={handleInteraction('comment')}
                title="Comment"
              >
                <span class="codicon codicon-comment"></span>
                {#if post.interactions?.comments}
                  <span class="text-sm text-muted">{post.interactions.comments}</span>
                {/if}
              </button>
              <button
                class="btn ghost subtle sm"
                class:disabled={buttonsDisabled}
                disabled={buttonsDisabled}
                on:click={handleInteraction('repost')}
                title="Repost"
              >
                <span class="codicon codicon-arrow-swap"></span>
                {#if post.display.totalReposts > 0}
                  <span class="text-sm text-muted">{post.display.totalReposts}</span>
                {/if}
              </button>
              {#if post.display.commitUrl}
                <a
                  href={post.display.commitUrl}
                  class="btn ghost sm"
                  title="View commit on {post.display.repositoryName}"
                >
                  {#if post.display.isOrigin}
                    <span class="codicon codicon-home sm"></span>
                  {:else}
                    <Avatar
                      type="repository"
                      identifier={gitHost.getWebUrl(post.repository) || post.repository}
                      name={post.display.repositoryName}
                      size={16}
                    />
                  {/if}
                  <span class="text-sm text-muted subtle hover-underline">{post.display.commitUrl}</span>
                </a>
              {:else}
                <span class="btn ghost sm disabled" title="Local commit">
                  <span class="codicon codicon-home sm"></span>
                  <span class="text-sm -ml-2">{post.display.commitHash}</span>
                </span>
              {/if}
            </div>
          {/if}
        </div>
      {:else}
        <!-- Regular Post Layout -->
        <div class="flex gap-3">
          <!-- Avatar Column -->
          <div class="flex-shrink-0">
            <Avatar
              type="user"
              identifier={post.author.email}
              name={post.author.name}
              repository={post.repository}
              size={40}
            />
          </div>

          <!-- Content Column -->
          <div class="flex-1 min-w-0">
            <!-- Header -->
            <div class="flex items-center justify-between gap-2 mb-1">
              <div class="flex items-center gap-2">
                <span class="font-bold">{post.author.name}</span>
                <span>·</span>
                <span class="text-sm text-muted">{post.author.email}</span>
                <span>·</span>
                <span class="text-sm text-muted italic cursor-help" title={new Date(post.timestamp).toLocaleString()}>
                  {formattedTime}
                </span>
              </div>
              <!-- Raw View Toggle Button (top right) -->
              {#if isAnchorPost && (parsedHtml || images.length > 0)}
                <button
                  class="btn ghost sm {showRawView ? 'active' : ''}"
                  on:click={(e) => { e.stopPropagation(); showRawView = !showRawView; }}
                  title="{showRawView ? 'Show rendered view' : 'Show raw view'}">
                  <span class="codicon codicon-{showRawView ? 'markdown' : 'code'}"></span>
                  <span>Raw</span>
                </button>
              {/if}
            </div>

            <!-- Content -->
            <div class="post-content mb-3">
              {#if showRawView}
                <!-- Raw text view -->
                <div class="whitespace-pre-wrap font-mono text-sm p-3 bg-opacity-50 rounded">{post.content}</div>
              {:else if isAnchorPost && parsedHtml}
                <!-- Full markdown for anchor post -->
                <div class="markdown-content">
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  {@html parsedHtml}
                </div>
              {:else if showFullContent || post.type !== 'comment'}
                <!-- Plain text for other posts -->
                {#if post.content.split('\n').length > 0}
                  <div class="font-bold break-words">{post.content.split('\n')[0]}</div>
                  {#if post.content.split('\n').slice(1).join('\n').trim()}
                    <div class="whitespace-pre-wrap break-words mt-1">{post.content.split('\n').slice(1).join('\n').trim()}</div>
                  {/if}
                {/if}
              {:else}
                <div class="font-bold break-words">{post.content.split('\n')[0]}</div>
                {#if post.content.split('\n').length > 1}
                  <span class="text-muted">...</span>
                {/if}
              {/if}

              <!-- Images for ALL posts (lazy loaded) -->
              {#if !showRawView && images.length > 0}
                <div class="image-gallery mt-3">
                  {#each images as url, index}
                    <button
                      class="image-button"
                      on:click={(e) => { e.stopPropagation(); selectedImageIndex = index; }}
                      aria-label="View larger image">
                      <img
                        src={url}
                        alt=""
                        loading="lazy"
                        class="gallery-image"
                        on:error={(e) => e.currentTarget.style.display = 'none'} />
                    </button>
                  {/each}
                </div>
              {/if}
            </div>

            <!-- Quoted Content -->
            {#if post.type === 'quote' && post.originalPostId !== anchorPostId}
              <div class="mb-3">
                <div class="card ghost border border-link rounded p-2 cursor-pointer"
                  role="button"
                  tabindex="0"
                  on:click={(e) => {
                    e.stopPropagation();
                    if (post.originalPostId) {
                      const originalPost = getResolvedPost(post.originalPostId);
                      const postIdToUse = originalPost ? originalPost.id : post.originalPostId;
                      api.openView('viewPost', 'Post', { postId: postIdToUse });
                    }
                  }}
                  on:keydown={(e) => {
                    if (e.key === 'Enter') {
                      e.stopPropagation();
                      if (post.originalPostId) {
                        api.openView('viewPost', 'Post', { postId: post.originalPostId });
                      }
                    }
                  }}
                  title="View quoted post">
                  {#if post.originalPostId}
                    {@const originalPost = getResolvedPost(post.originalPostId)}
                    {#if originalPost}
                      <svelte:self
                        post={originalPost}
                        {posts}
                        displayMode="preview"
                        isNested={true}
                        isEmbedded={true}
                        {anchorPostId} />
                    {:else if loadingPosts.has(post.originalPostId)}
                      <div class="text-center text-muted p-2">
                        <span class="codicon codicon-loading spin"></span>
                        <span class="ml-2 text-sm">Loading quoted post...</span>
                      </div>
                    {:else}
                      <span class="text-muted text-sm">Original post not available</span>
                    {/if}
                  {:else}
                    <span class="text-muted text-sm">Original post not available</span>
                  {/if}
                </div>
              </div>
            {/if}

            <!-- Parent Context for Comments -->
            {#if showParentContextComputed && (post.originalPostId || post.parentCommentId)}
              {@const parentId = post.originalPostId || post.parentCommentId}
              {@const parentPost = parentId ? getResolvedPost(parentId) : null}
              <div class="parent-context mb-3">
                <div class="card ghost border border-link rounded p-2 cursor-pointer"
                  role="button"
                  tabindex="0"
                  on:click={() => {
                    if (parentId) {
                      const parentPost = getResolvedPost(parentId);
                      const postIdToUse = parentPost ? parentPost.id : parentId;
                      api.openView('viewPost', 'Post', { postId: postIdToUse });
                    }
                  }}
                  on:keydown={(e) => {
                    if (e.key === 'Enter' && parentId) {
                      api.openView('viewPost', 'Post', { postId: parentId });
                    }
                  }}
                  title="View parent post">
                  {#if parentPost}
                    <svelte:self
                      post={parentPost}
                      {posts}
                      displayMode="preview"
                      isNested={true}
                      isEmbedded={true} />
                  {:else if parentId && loadingPosts.has(parentId)}
                    <div class="text-center text-muted p-2">
                      <span class="codicon codicon-loading spin"></span>
                      <span class="ml-2 text-sm">Loading parent post...</span>
                    </div>
                  {:else}
                    <span class="text-muted text-sm">Replying to a post that is not available</span>
                  {/if}
                </div>
              </div>
            {/if}

            <!-- Actions -->
            {#if showInteractions}
              <div class="flex gap-4">
                <button
                  class="btn ghost subtle sm "
                  class:disabled={buttonsDisabled}
                  disabled={buttonsDisabled}
                  on:click={handleInteraction('comment')}
                  title="Comment"
                >
                  <span class="codicon codicon-comment"></span>
                  {#if post.interactions?.comments}
                    <span class="text-sm text-muted">{post.interactions.comments}</span>
                  {/if}
                </button>
                <button
                  class="btn ghost subtle sm"
                  class:disabled={buttonsDisabled}
                  disabled={buttonsDisabled}
                  on:click={handleInteraction('repost')}
                  title="Repost"
                >
                  <span class="codicon codicon-arrow-swap"></span>
                  {#if post.display.totalReposts > 0}
                    <span class="text-sm text-muted">{post.display.totalReposts}</span>
                  {/if}
                </button>
                {#if post.display.commitUrl}
                  <a
                    href={post.display.commitUrl}
                    class="btn ghost sm"
                    title="View commit on {post.display.repositoryName}"
                  >
                    {#if post.display.isOrigin}
                      <span class="codicon codicon-home sm"></span>
                    {:else}
                      <Avatar
                        type="repository"
                        identifier={gitHost.getWebUrl(post.repository) || post.repository}
                        name={post.display.repositoryName}
                        size={16}
                      />
                    {/if}
                    <span class="text-sm text-muted subtle hover-underline">{post.display.commitUrl}</span>
                  </a>
                {:else}
                  <span class="btn ghost sm disabled" title="Local commit">
                    <span class="codicon codicon-home sm"></span>
                    <span class="text-sm -ml-2">{post.display.commitHash}</span>
                  </span>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <Dialog
    isOpen={!!$interactionState.selectedPost && !!$interactionState.interactionType}
    title={$interactionState.interactionType ? $interactionState.interactionType.charAt(0).toUpperCase() + $interactionState.interactionType.slice(1) : ''}
    on:close={() => {
      handleInteractionCancel();
      interactionText = '';
    }}
  >
    {#if $interactionState.selectedPost}
      <div class="card border bg-muted mt-1 mb-4 overflow-x-auto">
        <svelte:self post={$interactionState.selectedPost} displayMode="preview" />
      </div>

      {#if $interactionState.interactionType === 'comment' || $interactionState.interactionType === 'quote'}
        <div>
          <label for="interaction-text" class="block text-sm font-medium mb-1">
            {$interactionState.interactionType === 'comment' ? 'Comment' : 'Your thoughts'}
          </label>
          <textarea
            id="interaction-text"
            bind:this={textareaElement}
            bind:value={interactionText}
            on:keydown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                handleInteractionSubmit({ detail: { text: interactionText, isQuoteRepost: false } });
              }
            }}
            on:click|stopPropagation
            placeholder={$interactionState.interactionType === 'comment' ? 'Write your comment...' : 'Add your thoughts...'}
            disabled={$interactionState.isSubmitting}
            class="w-full"
            rows="10"
          />
        </div>
      {:else if $interactionState.interactionType === 'repost'}
        <div>
          <label for="repost-text" class="block text-sm font-medium mb-1">Comment (optional)</label>
          <textarea
            id="repost-text"
            bind:this={textareaElement}
            bind:value={interactionText}
            on:keydown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                handleInteractionSubmit({
                  detail: { text: interactionText, isQuoteRepost: interactionText.trim().length > 0 }
                });
              }
            }}
            on:click|stopPropagation
            placeholder="Add a comment (optional)"
            disabled={$interactionState.isSubmitting}
            class="w-full"
            rows="10"
          />
        </div>
      {/if}

      {#if $interactionState.submissionMessage}
        <div class="mb-4 p-2 rounded {$interactionState.submissionSuccess ? 'bg-success text-on-dark' : $interactionState.isSubmitting ? 'badge' : 'text-error'}">
          {$interactionState.submissionMessage}
        </div>
      {/if}
    {/if}

    <div slot="footer" class="flex gap-2 mt-1 justify-end">
      <button
        class="btn"
        on:click={() => {
          handleInteractionCancel();
          interactionText = '';
        }}
        disabled={$interactionState.isSubmitting}
      >
        Cancel
      </button>
      <button
        class="btn primary wide"
        on:click={() => handleInteractionSubmit({ detail: { text: interactionText, isQuoteRepost: $interactionState.interactionType === 'repost' && interactionText.trim().length > 0 } })}
        disabled={$interactionState.isSubmitting || ($interactionState.interactionType === 'comment' && !interactionText.trim()) || ($interactionState.interactionType === 'quote' && !interactionText.trim())}
      >
        <span class="codicon codicon-{$interactionState.interactionType === 'comment' ? 'comment' : 'arrow-swap'}"></span>
        {$interactionState.interactionType === 'comment' ? 'Comment' : $interactionState.interactionType === 'quote' ? 'Quote' : 'Repost'}
      </button>
    </div>
  </Dialog>

  <!-- Image Lightbox -->
  {#if selectedImageIndex !== null}
    <div
      class="lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="Image viewer"
      tabindex="-1">
      <button
        class="lightbox-backdrop"
        on:click={() => selectedImageIndex = null}
        aria-label="Close image viewer">
        <span class="sr-only">Close</span>
      </button>
      <button
        class="lightbox-close"
        on:click={() => selectedImageIndex = null}
        aria-label="Close image viewer">
        <span class="codicon codicon-close"></span>
      </button>
      <img src={images[selectedImageIndex]} alt="Enlarged view" class="lightbox-image" />

      {#if images.length > 1}
        <button
          class="lightbox-nav lightbox-nav-prev"
          on:click={navigatePrevious}
          disabled={selectedImageIndex === 0}
          aria-label="Previous image">
          <span class="codicon codicon-arrow-left"></span>
        </button>
        <button
          class="lightbox-nav lightbox-nav-next"
          on:click={navigateNext}
          disabled={selectedImageIndex === images.length - 1}
          aria-label="Next image">
          <span class="codicon codicon-arrow-right"></span>
        </button>
        <div class="lightbox-indicator">
          {selectedImageIndex + 1} / {images.length}
        </div>
      {/if}
    </div>
  {/if}
{/if}
