<script lang="ts">
  import type { Post } from '@gitsocial/core/client';
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import PostCard from './PostCard.svelte';
  import MarkdownEditor from './MarkdownEditor.svelte';
  export let post: Post;
  export let interactionType: 'comment' | 'quote' | 'repost';
  export let text = '';
  export let isSubmitting = false;
  const dispatch = createEventDispatcher<{
    submit: { text: string; isQuoteRepost: boolean };
    cancel: void;
  }>();
  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      dispatch('cancel');
    }
  }
  function handleSubmit() {
    const isQuoteRepost = interactionType === 'repost' && text.trim().length > 0;
    dispatch('submit', { text, isQuoteRepost });
  }
  function handleCancel() {
    dispatch('cancel');
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
  aria-label="Fullscreen {interactionType} editor">
  <button
    class="btn ghost icon absolute top-0 right-0 mt-4 mr-2 z-50 text-xl"
    on:click={() => dispatch('cancel')}
    aria-label="Close"
    title="Close (Esc)"
    disabled={isSubmitting}>
    <span class="codicon codicon-close"></span>
  </button>
  <div class="relative overflow-y-auto z-10 w-full h-full py-8" style="overscroll-behavior: contain">
    <div class="w-full px-8">
      <div class="card border bg-muted mb-4">
        <PostCard
          {post}
          layout="compact"
          interactive={false}
          clickable={false}
          trimmed={true} />
      </div>
      <MarkdownEditor
        bind:value={text}
        placeholder={interactionType === 'comment' ? 'Write your comment...' : interactionType === 'quote' ? 'Add your thoughts...' : 'Add a comment (optional)'}
        disabled={isSubmitting}
        creating={isSubmitting}
        allowEmpty={interactionType === 'repost'}
        onSubmit={handleSubmit}
        onCancel={handleCancel}
      />
    </div>
  </div>
</div>
