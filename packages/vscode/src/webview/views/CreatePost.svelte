<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../api';
  import { webLog } from '../utils/weblog';
  import MarkdownEditor from '../components/MarkdownEditor.svelte';

  let content = '';
  let creating = false;
  let error: string | null = null;
  let successMessage: string | null = null;
  let currentRequestId: string | null = null;

  onMount(() => {
    const appEl = document.getElementById('app');
    if (appEl) {
      appEl.classList.add('full-width-view');
    }

    // Get initial content from params if provided
    const params = (window as { viewParams?: { content?: string } }).viewParams;
    if (params?.content) {
      content = params.content;
    }

    // Listen for response from extension
    window.addEventListener('message', (event) => {
      const message = event.data;

      // Only handle responses for our current request
      if (message.requestId && message.requestId !== currentRequestId) {
        return;
      }

      if (message.type === 'postCreated' || message.type === 'commitCreated') {
        creating = false;
        currentRequestId = null;
        successMessage = message.data?.message || 'Post created successfully!';

        // Reset form and close panel after a short delay
        setTimeout(() => {
          if (message.data?.post) {
            const post = message.data.post;
            webLog('debug', '[CreatePost] Opening post view for:', post);
            api.openView('viewPost', post.content.split('\n')[0].substring(0, 30) + '...', { postId: post.id, repository: post.repository });
          }

          // Close this panel
          api.closePanel();
        }, 1500);
      } else if (message.type === 'error' && message.requestId === currentRequestId) {
        creating = false;
        currentRequestId = null;
        error = message.data?.message || message.message || 'Failed to create post';
        successMessage = null;
      }
    });
  });

  function handleSubmit() {
    if (!content.trim() || creating) {return;}

    creating = true;
    error = null;
    successMessage = null;

    // Generate unique request ID
    currentRequestId = Date.now().toString(36) + Math.random().toString(36);

    // Send message to extension
    api.createPost(content);
  }

  function handleCancel() {
    api.closePanel();
  }

  onDestroy(() => {
    const appEl = document.getElementById('app');
    if (appEl) {
      appEl.classList.remove('full-width-view');
    }
  });
</script>

<div class="view-container">
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar border-b-r">
    <h1><span class="codicon codicon-lg codicon-edit mr-2"></span>New Post</h1>
  </div>

  {#if error}
    <div class="error">
      <span class="codicon codicon-error"></span>
      {error}
    </div>
  {/if}

  {#if successMessage}
    <div class="success">
      <span class="codicon codicon-check"></span>
      {successMessage}
    </div>
  {/if}

  <form on:submit|preventDefault={handleSubmit}>
    <MarkdownEditor
      bind:value={content}
      placeholder="What's on your mind?"
      disabled={creating}
      creating={creating}
      onSubmit={handleSubmit}
      onCancel={handleCancel}
    />
  </form>
</div>

<style>
  :global(#app.full-width-view) {
    max-width: none;
  }
</style>
