<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  const dispatch = createEventDispatcher();

  export let newListName = '';
  export let newListId = '';
  export let showCustomId = false;
  export let isCreating = false;
  export let compact = false;
  export let submitHandler: (() => void) | null = null;

  function generateListId(name: string): string {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .substring(0, 40);
  }

  function handleCreateList() {
    const listId = showCustomId ? newListId.trim() : generateListId(newListName.trim());
    const name = newListName.trim();

    if (!name) {
      alert('Name is required');
      return;
    }

    if (!listId) {
      alert('Generated ID is empty. Please use custom ID or a different name.');
      return;
    }

    const validPattern = /^[a-zA-Z0-9_-]{1,40}$/;
    if (!validPattern.test(listId)) {
      alert('List ID must be 1-40 characters and contain only letters, numbers, hyphens, and underscores');
      return;
    }

    dispatch('createList', {
      id: listId,
      name: name
    });

    newListName = '';
    newListId = '';
    showCustomId = false;
  }

  function toggleCustomId() {
    showCustomId = !showCustomId;
  }

  $: submitHandler = handleCreateList;
</script>

<div class="list-create-form">
  <div class="flex flex-wrap gap-2 items-end">
    <div class="flex-1">
      <label for="list-display-name" class="block text-sm font-medium mb-1">Name</label>
      <input
        id="list-display-name"
        type="text"
        bind:value={newListName}
        placeholder="My Favorite Repositories"
        disabled={isCreating}
        class="w-full"
      />
    </div>

    {#if showCustomId}
      <div class="flex-1">
        <label for="list-custom-id" class="block text-sm font-medium mb-1">Custom ID</label>
        <input
          id="list-custom-id"
          type="text"
          bind:value={newListId}
          placeholder="my-favorite-repos"
          disabled={isCreating}
          class="w-full"
        />
      </div>
    {/if}

    <button
      class="btn"
      on:click={toggleCustomId}
      disabled={isCreating}
      title={showCustomId ? 'Use auto-generated ID' : 'Use custom ID'}
      class:active={showCustomId}
    >
      <span class="codicon codicon-gear"></span>
    </button>

    {#if !compact}
      <button
        class="btn primary"
        on:click={handleCreateList}
        disabled={!newListName.trim() || isCreating}
      >
        <span class="codicon codicon-add"></span>
        {isCreating ? 'Creating...' : 'Create List'}
      </button>
    {/if}
  </div>

  {#if showCustomId}
    <div class="text-xs text-muted mt-1">
      Letters, numbers, hyphens, and underscores only
    </div>
  {:else if newListName.trim()}
    <div class="text-xs text-muted mt-1">
      ID: {generateListId(newListName.trim()) || 'invalid'}
    </div>
  {/if}

  {#if compact}
    <slot name="actions" {handleCreateList} />
  {/if}
</div>
