<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';

  export let type: 'user' | 'repository';
  export let identifier: string; // email for users, url for repos
  export let name: string;       // display name
  export let size = 32;
  export let repository: string | undefined = undefined; // for user avatars with repo context

  let avatarUrl: string | null = null;
  let loading = true;
  let error = false;
  let timeout: number | null = null;
  let messageHandler: ((event: MessageEvent) => void) | null = null;
  let requestId: string | null = null;
  let element: HTMLElement;
  let observer: IntersectionObserver | null = null;
  let hasStartedLoading = false;
  let currentIdentifier: string | null = null;

  $: initials = getInitials(name);
  $: avatarIdentifier = type === 'user'
    ? (repository ? `${identifier}|${repository}` : identifier)
    : identifier;

  // Reset and re-fetch when identifier changes
  $: if (avatarIdentifier !== currentIdentifier && currentIdentifier !== null) {
    cleanup();
    avatarUrl = null;
    hasStartedLoading = false;
    error = false;
    loading = true;
    currentIdentifier = avatarIdentifier;
    if (element && observer) {
      observer.observe(element);
    }
  }

  function getInitials(displayName: string): string {
    if (!displayName) {return '?';}

    if (type === 'repository') {
      return displayName.charAt(0).toUpperCase();
    }

    // User avatar - use first and last name initials
    const parts = displayName.trim().split(/\s+/);
    if (parts.length === 0) {return '?';}
    if (parts.length === 1) {return parts[0].charAt(0).toUpperCase();}
    return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
  }

  async function fetchAvatar() {
    if (!identifier || hasStartedLoading) {
      if (!identifier) {
        error = true;
        loading = false;
      }
      return;
    }

    hasStartedLoading = true;
    currentIdentifier = avatarIdentifier;
    error = false;
    loading = true;

    // Generate unique request ID
    requestId = `${type}-avatar-${Date.now()}-${Math.random()}`;

    // Set up message handler
    messageHandler = (event: MessageEvent) => {
      const message = event.data;
      if (message.requestId === requestId) {
        if (message.type === 'avatar' && message.data?.identifier === avatarIdentifier) {
          avatarUrl = message.data.url;
          loading = false;
          cleanup();
        } else if (message.type === 'error') {
          error = true;
          loading = false;
          cleanup();
        }
      }
    };

    window.addEventListener('message', messageHandler);

    // Set timeout for 5 seconds
    timeout = setTimeout(() => {
      error = true;
      loading = false;
      cleanup();
    }, 5000);

    // Request avatar
    api.postMessage({
      type: 'getAvatar',
      identifier: avatarIdentifier,
      avatarType: type,
      size,
      name,
      id: requestId
    });
  }

  function cleanup() {
    if (messageHandler) {
      window.removeEventListener('message', messageHandler);
      messageHandler = null;
    }
    if (timeout) {
      clearTimeout(timeout);
      timeout = null;
    }
    requestId = null;
  }

  onMount(() => {
    // Set up Intersection Observer for lazy loading
    observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting && !hasStartedLoading) {
            fetchAvatar();
            if (observer) {
              observer.unobserve(element);
            }
          }
        });
      },
      {
        rootMargin: '50px' // Start loading 50px before the avatar enters viewport
      }
    );

    if (element) {
      observer.observe(element);
    }
  });

  onDestroy(() => {
    cleanup();
    if (observer) {
      observer.disconnect();
      observer = null;
    }
  });
</script>

<div
  class="avatar {type}-avatar"
  class:loading
  style="width: {size}px; height: {size}px;"
  bind:this={element}
>
  {#if avatarUrl && !error}
    <img
      src={avatarUrl}
      alt={name}
      style="width: {size}px; height: {size}px;"
    />
  {:else}
    <span
      class="avatar-fallback"
      style="font-size: {size * (type === 'repository' ? 0.6 : 0.4)}px;"
    >
      {initials}
    </span>
  {/if}
</div>

<style>
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    position: relative;
    z-index: 10;
  }

  .user-avatar {
    border-radius: 50%;
    overflow: hidden;
  }

  .repo-avatar {
    border-radius: 50%;
    overflow: hidden;
    background-color: var(--vscode-badge-background);
    color: var(--vscode-badge-foreground);
    font-weight: 500;
  }

  .avatar img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 50%;  /* Apply circular shape to PNG images */
  }

  .avatar.loading {
    animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
  }

  .avatar-fallback {
    text-transform: uppercase;
  }

  @keyframes pulse {
    0%, 100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }
</style>
