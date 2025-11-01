<script lang="ts">
  import type { Post } from '@gitsocial/core/client';
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import PostCard from './PostCard.svelte';
  export let posts: Post[] = [];
  export let currentIndex = 0;
  const dispatch = createEventDispatcher<{ close: void }>();
  $: currentPost = posts[currentIndex];
  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      dispatch('close');
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      navigatePrevious();
    } else if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      navigateNext();
    }
  }
  function navigatePrevious() {
    if (currentIndex > 0) {
      currentIndex--;
    }
  }
  function navigateNext() {
    if (currentIndex < posts.length - 1) {
      currentIndex++;
    }
  }
  onMount(() => {
    const scrollY = window.scrollY;
    document.body.style.position = 'fixed';
    document.body.style.top = `-${scrollY}px`;
    document.body.style.width = '100%';
    window.addEventListener('keydown', handleKeydown);
  });
  onDestroy(() => {
    const scrollY = document.body.style.top;
    document.body.style.position = '';
    document.body.style.top = '';
    document.body.style.width = '';
    window.scrollTo(0, parseInt(scrollY || '0') * -1);
    window.removeEventListener('keydown', handleKeydown);
  });
</script>

<div
  class="fixed inset-0 w-full h-full flex justify-center z-50 bg-sidebar overflow-hidden"
  role="dialog"
  aria-modal="true"
  aria-label="Fullscreen post viewer">
  <button
    class="btn ghost icon absolute top-0 right-0 mt-4 mr-2 z-50 text-xl"
    on:click={() => dispatch('close')}
    aria-label="Close"
    title="Close (Esc)">
    <span class="codicon codicon-close"></span>
  </button>
  <div class="relative overflow-y-auto z-10 w-full h-screen py-8" style="overscroll-behavior: contain">
    <div class="mx-auto" style="width: 90%; max-width: 60rem">
      {#if currentPost}
        <PostCard
          post={currentPost}
          displayMode="main"
          isAnchorPost={true}
          hideFullscreenButton={true} />
      {/if}
    </div>
    {#if posts.length > 1}
      <div
        class="fixed bottom-8 left-1/2 -translate-x-1/2 flex justify-center items-center gap-4
          border rounded-full z-50 bg-sidebar p-2">
        <button
          class="bg-transparent border-0 cursor-pointer p-2 rounded-sm flex items-center justify-center transition"
          on:click={navigatePrevious}
          disabled={currentIndex === 0}
          aria-label="Previous post"
          title="Previous post (← or ↑)">
          <span class="codicon codicon-arrow-up"></span>
        </button>
        <div class="text-sm text-muted text-center fullscreen-indicator">
          {currentIndex + 1} / {posts.length}
        </div>
        <button
          class="bg-transparent border-0 cursor-pointer p-2 rounded-sm flex items-center justify-center transition"
          on:click={navigateNext}
          disabled={currentIndex === posts.length - 1}
          aria-label="Next post"
          title="Next post (→ or ↓)">
          <span class="codicon codicon-arrow-down"></span>
        </button>
      </div>
    {/if}
  </div>
</div>
