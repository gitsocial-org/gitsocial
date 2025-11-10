<script lang="ts">
  import type { Post } from '@gitsocial/core/client';
  import { gitHost } from '@gitsocial/core/client';
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';
  import { settings } from '../stores';
  import Avatar from './Avatar.svelte';
  import Dialog from './Dialog.svelte';
  import FullscreenPostViewer from './FullscreenPostViewer.svelte';
  import FullscreenTextareaEditor from './FullscreenTextareaEditor.svelte';
  import MarkdownEditor from './MarkdownEditor.svelte';
  import { formatRelativeTime, useEventDrivenTimeUpdates } from '../utils/time';
  import { createInteractionHandler } from '../utils/interactions';
  import { parseMarkdown, extractImages, transformCodeAndMath } from '../utils/markdown';

  export let post: Post;
  export let posts: Map<string, Post> | Post[] = [];

  // Visual layout
  export let layout: 'compact' | 'normal' = 'normal';

  // Behavioral flags
  export let clickable = true;
  export let interactive = true;
  export let expandContent = false;
  export let trimmed = false;
  export let collapsed = false;

  // Context
  export let showParentContext = false;
  export let anchorPostId: string | undefined = undefined;
  export let hideFullscreenButton = false;

  // Raw view toggle state
  let showRawView = false;

  // Fullscreen state
  let showFullscreen = false;
  let showFullscreenEditor = false;

  // Computed display logic
  $: showFullContent = expandContent;
  $: isCompact = layout === 'compact';
  $: showInteractions = interactive;
  $: showParentContextComputed = post && (post.originalPostId || post.parentCommentId) && showParentContext;

  // Markdown and image processing (on-demand)
  $: images = extractImages(post?.content || '');
  let parsedHtml = '';
  let transformedHtml = '';
  $: if (expandContent && post?.content) {
    parsedHtml = parseMarkdown(post.content);
  }
  $: if (!expandContent && post?.content) {
    transformedHtml = transformCodeAndMath(post.content);
  }
  function truncateContent(content: string, maxLines = 3): string {
    const lines = content.split('\n');
    if (lines.length <= maxLines) {
      return content;
    }
    return lines.slice(0, maxLines).join('\n') + '...';
  }

  // Image lightbox state
  let selectedImageIndex: number | null = null;

  // Image loading state
  let imagesManuallyLoaded = false;
  $: autoLoadImages = $settings.autoLoadImages ?? true;
  $: shouldShowImages = autoLoadImages || imagesManuallyLoaded;

  function loadImages() {
    imagesManuallyLoaded = true;
  }

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
    // Request autoLoadImages setting
    api.getSettings('autoLoadImages');

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
    if (clickable) {
      // Simply use post.id directly - it's already correct (relative for workspace, absolute for external)
      api.openView('viewPost', post.content.split('\n')[0].substring(0, 30) + '...', {
        postId: post.id,
        repository: post.repository
      });
    }
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

  function handleFullscreenClick() {
    showFullscreen = true;
    api.toggleZenMode();
  }

  function handleCloseFullscreen() {
    showFullscreen = false;
    api.toggleZenMode();
  }
  function handleFullscreenEditor() {
    showFullscreenEditor = true;
    api.toggleZenMode();
  }
  function handleFullscreenEditorCancel() {
    showFullscreenEditor = false;
    api.toggleZenMode();
  }
  function handleFullscreenEditorSubmit(event: CustomEvent<{ text: string; isQuoteRepost: boolean }>) {
    showFullscreenEditor = false;
    api.toggleZenMode();
    handleInteractionSubmit(event);
  }
  function handleDialogSubmit() {
    const isQuoteRepost = $interactionState.interactionType === 'repost' && interactionText.trim().length > 0;
    handleInteractionSubmit({ detail: { text: interactionText, isQuoteRepost } });
  }
  function handleDialogCancel() {
    handleInteractionCancel();
    interactionText = '';
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
    <div class="card hover {post.display.isUnpushed ? 'border-l-warning' : ''}">
      <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
      <div class="ml-12 mt-2 -mb-2 text-sm text-muted font-bold relative z-10 {clickable ? 'cursor-pointer subtle hover-underline' : ''}"
        role={clickable ? 'button' : undefined}
        tabindex={clickable ? 0 : -1}
        on:click={clickable ? handleViewPost : undefined}
        on:keydown={clickable ? handleKeydown : undefined}>
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
            {posts} />
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
    <div class="card {clickable ? 'hover' : ''} {isCompact ? 'p-2' : 'p-3'} {post.type} {post.display.isUnpushed ? 'border-l-warning' : ''}"
      tabindex={clickable ? 0 : -1}
      role={clickable ? 'button' : 'article'}
      on:click={clickable ? handleViewPost : undefined}
      on:keydown={clickable ? handleKeydown : undefined}>

      {#if expandContent}
        <!-- Main Post Layout - Full Width Content -->
        <div class="w-full">
          <!-- Header with Avatar -->
          <div class="flex items-center justify-between gap-3 mb-3">
            <div class="flex items-center gap-3 min-w-0">
              <Avatar
                type="user"
                identifier={post.author.email}
                name={post.author.name}
                repository={post.repository}
                size={40}
              />
              <div class="flex items-center gap-2 min-w-0">
                <span class="font-bold text-lg truncate">{post.author.name}</span>
                <span>·</span>
                <span class="text-sm text-muted truncate">{post.author.email}</span>
                <span>·</span>
                <span
                  class="text-sm text-muted italic cursor-help whitespace-nowrap"
                  title={new Date(post.timestamp).toLocaleString()}
                >
                  {formattedTime}
                </span>
              </div>
            </div>
            <!-- Action Buttons (top right) -->
            {#if showInteractions}
              <div class="flex gap-2 flex-shrink-0">
                {#if post.content}
                  <button
                    class="btn ghost sm {showRawView ? 'active' : ''}"
                    on:click={() => showRawView = !showRawView}
                    title="{showRawView ? 'Show rendered view' : 'Show raw view'}">
                    <span class="codicon codicon-code"></span>
                    <span>Raw</span>
                  </button>
                {/if}
                {#if !hideFullscreenButton}
                  <button
                    class="btn ghost sm"
                    on:click={handleFullscreenClick}
                    title="View fullscreen (F)">
                    <span class="codicon codicon-screen-full"></span>
                  </button>
                {/if}
              </div>
            {/if}
          </div>

          {#if !collapsed}
            <!-- Full Width Content -->

            <!-- Content -->
            <div class="post-content {expandContent ? 'mb-5' : 'mb-3'}">
              {#if showRawView}
                <!-- Raw text view -->
                <pre class="whitespace-pre-wrap break-words font-mono py-3 m-0">{post.content}</pre>
              {:else if expandContent && parsedHtml}
                <!-- Full markdown for anchor post -->
                <div class="markdown-content break-words {expandContent ? 'text-lg' : ''}">
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  {@html parsedHtml}
                </div>
              {:else if transformedHtml}
                <!-- Code and math transformed -->
                <div class="markdown-content break-words {expandContent ? 'text-lg' : ''}">
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  {@html transformedHtml}
                </div>
              {:else if showFullContent || post.type !== 'comment'}
                <!-- Plain text for other posts -->
                <div class="whitespace-pre-wrap break-words {expandContent ? 'text-lg' : ''}">{post.content}</div>
              {:else}
                <div class="break-words {expandContent ? 'text-lg' : ''}">{post.content.split('\n')[0]}</div>
                {#if post.content.split('\n').length > 1}
                  <span class="text-muted">...</span>
                {/if}
              {/if}

              <!-- Images for ALL posts (lazy loaded) -->
              {#if !showRawView && images.length > 0}
                {#if shouldShowImages}
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
                {:else}
                  <div class="flex items-center gap-2 mt-3 p-3 border rounded">
                    <button class="btn sm" on:click={(e) => { e.stopPropagation(); loadImages(); }}>
                      <span class="codicon codicon-file-media mr-2"></span>
                      Load {images.length} {images.length === 1 ? 'image' : 'images'}
                    </button>
                  </div>
                {/if}
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
                        layout="compact"
                        clickable={false}
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
                      layout="compact"
                      clickable={false} />
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
            <div class="flex gap-4">
              {#if showInteractions}
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
              {/if}
              {#if post.display.commitUrl}
                <a
                  href={post.display.commitUrl}
                  class="btn ghost sm min-w-0"
                  class:-ml-2={!showInteractions}
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
                  <span class="text-sm text-muted subtle hover-underline truncate">{post.display.commitUrl}</span>
                </a>
              {:else}
                <span class="btn ghost sm disabled min-w-0" class:-ml-2={!showInteractions} title="Local commit">
                  <span class="codicon codicon-home sm"></span>
                  <span class="text-sm -ml-2 truncate">{post.display.commitHash}</span>
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
              <div class="flex items-center gap-2 min-w-0">
                <span class="font-bold truncate">{post.author.name}</span>
                <span>·</span>
                <span class="text-sm text-muted truncate">{post.author.email}</span>
                <span>·</span>
                <span
                  class="text-sm text-muted italic cursor-help whitespace-nowrap"
                  title={new Date(post.timestamp).toLocaleString()}
                >
                  {formattedTime}
                </span>
              </div>
              <!-- Action Buttons (top right) -->
              {#if showInteractions}
                <div class="flex gap-2 flex-shrink-0">
                  {#if post.content}
                    <button
                      class="btn ghost sm {showRawView ? 'active' : ''}"
                      on:click={(e) => { e.stopPropagation(); showRawView = !showRawView; }}
                      title="{showRawView ? 'Show rendered view' : 'Show raw view'}">
                      <span class="codicon codicon-{showRawView ? 'markdown' : 'code'}"></span>
                      <span>Raw</span>
                    </button>
                  {/if}
                  {#if !hideFullscreenButton}
                    <button
                      class="btn ghost sm"
                      on:click={(e) => { e.stopPropagation(); handleFullscreenClick(); }}
                      title="View fullscreen (F)">
                      <span class="codicon codicon-screen-full"></span>
                    </button>
                  {/if}
                </div>
              {/if}
            </div>

            {#if !collapsed}
              <!-- Content -->
              <div class="post-content mb-3">
                {#if trimmed}
                  <!-- Trimmed view for dialog preview -->
                  <div class="break-words text-sm text-muted">
                    {truncateContent(post.content)}
                  </div>
                {:else if showRawView}
                  <!-- Raw text view -->
                  <pre class="break-words font-mono text-sm p-3 rounded">{post.content}</pre>
                {:else if expandContent && parsedHtml}
                  <!-- Full markdown for anchor post -->
                  <div class="markdown-content break-words">
                    <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                    {@html parsedHtml}
                  </div>
                {:else if transformedHtml}
                  <!-- Code and math transformed -->
                  <div class="markdown-content break-words">
                    <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                    {@html transformedHtml}
                  </div>
                {:else if showFullContent || post.type !== 'comment'}
                  <!-- Plain text for other posts -->
                  <div class="break-words">{post.content}</div>
                {:else}
                  <div class="break-words">{post.content.split('\n')[0]}</div>
                  {#if post.content.split('\n').length > 1}
                    <span class="text-muted">...</span>
                  {/if}
                {/if}

                <!-- Images for ALL posts (lazy loaded) -->
                {#if !showRawView && !trimmed && images.length > 0}
                  {#if shouldShowImages}
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
                  {:else}
                    <div class="flex items-center gap-2 mt-3 p-3 border rounded">
                      <button class="btn sm" on:click={(e) => { e.stopPropagation(); loadImages(); }}>
                        <span class="codicon codicon-file-media mr-2"></span>
                        Load {images.length} {images.length === 1 ? 'image' : 'images'}
                      </button>
                    </div>
                  {/if}
                {/if}
              </div>

              <!-- Quoted Content -->
              {#if !trimmed && post.type === 'quote' && post.originalPostId !== anchorPostId}
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
              {#if !trimmed && showParentContextComputed && (post.originalPostId || post.parentCommentId)}
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
                        {posts} />
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
              <div class="flex gap-4">
                {#if showInteractions}
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
                {/if}
                {#if post.display.commitUrl}
                  <a
                    href={post.display.commitUrl}
                    class="btn ghost sm min-w-0"
                    class:-ml-2={!showInteractions}
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
                    <span class="text-sm text-muted subtle hover-underline truncate">{post.display.commitUrl}</span>
                  </a>
                {:else}
                  <span class="btn ghost sm disabled min-w-0" class:-ml-2={!showInteractions} title="Local commit">
                    <span class="codicon codicon-home sm"></span>
                    <span class="text-sm -ml-2 truncate">{post.display.commitHash}</span>
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
    on:close={() => {
      handleInteractionCancel();
      interactionText = '';
    }}
  >
    <div slot="header" class="flex justify-between items-center mb-4">
      <h3 id="dialog-title" class="m-0">
        {$interactionState.interactionType ? $interactionState.interactionType.charAt(0).toUpperCase() + $interactionState.interactionType.slice(1) : ''}
      </h3>
      <div class="flex gap-2">
        <button
          class="btn ghost sm"
          on:click={handleFullscreenEditor}
          title="Fullscreen editor (F)">
          <span class="codicon codicon-screen-full"></span>
        </button>
        <button
          class="btn ghost sm"
          on:click={() => {
            handleInteractionCancel();
            interactionText = '';
          }}>
          <span class="codicon codicon-close"></span>
        </button>
      </div>
    </div>
    {#if $interactionState.selectedPost}
      <div class="card border bg-muted mt-1 mb-4 overflow-x-auto">
        <svelte:self post={$interactionState.selectedPost} layout="compact" interactive={false} trimmed={true} />
      </div>

      {#if $interactionState.interactionType === 'comment' || $interactionState.interactionType === 'quote'}
        <MarkdownEditor
          bind:value={interactionText}
          placeholder={$interactionState.interactionType === 'comment' ? 'Write your comment...' : 'Add your thoughts...'}
          disabled={$interactionState.isSubmitting}
          creating={$interactionState.isSubmitting}
          onSubmit={handleDialogSubmit}
          onCancel={handleDialogCancel}
        />
      {:else if $interactionState.interactionType === 'repost'}
        <MarkdownEditor
          bind:value={interactionText}
          placeholder="Add a comment (optional)"
          disabled={$interactionState.isSubmitting}
          creating={$interactionState.isSubmitting}
          allowEmpty={true}
          onSubmit={handleDialogSubmit}
          onCancel={handleDialogCancel}
        />
      {/if}

      {#if $interactionState.submissionMessage}
        <div class="mb-4 p-2 rounded {$interactionState.submissionSuccess ? 'bg-success text-on-dark' : $interactionState.isSubmitting ? 'badge' : 'text-error'}">
          {$interactionState.submissionMessage}
        </div>
      {/if}
    {/if}
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

  {#if showFullscreen}
    <FullscreenPostViewer
      posts={[post]}
      currentIndex={0}
      on:close={handleCloseFullscreen} />
  {/if}
  {#if showFullscreenEditor && $interactionState.selectedPost && $interactionState.interactionType}
    <FullscreenTextareaEditor
      post={$interactionState.selectedPost}
      interactionType={$interactionState.interactionType}
      bind:text={interactionText}
      isSubmitting={$interactionState.isSubmitting}
      on:submit={handleFullscreenEditorSubmit}
      on:cancel={handleFullscreenEditorCancel} />
  {/if}
{/if}
