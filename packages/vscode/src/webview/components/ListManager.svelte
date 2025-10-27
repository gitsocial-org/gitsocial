<script lang="ts">
  import type { List } from '@gitsocial/core';
  import { createEventDispatcher } from 'svelte';
  import ListCard from './ListCard.svelte';

  export let lists: List[] = [];
  export let readOnly = false;
  export let repository: string | undefined = undefined; // Repository URL if viewing external lists

  const dispatch = createEventDispatcher();

  let newListName = '';
  let newListId = '';
  let isCreating = false;
  let showCustomId = false;

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

    // Validate list name pattern
    const validPattern = /^[a-zA-Z0-9_-]{1,40}$/;
    if (!validPattern.test(listId)) {
      alert('List ID must be 1-40 characters and contain only letters, numbers, hyphens, and underscores');
      return;
    }

    isCreating = true;
    dispatch('createList', {
      id: listId,
      name: name
    });

    // Reset form
    newListName = '';
    newListId = '';
    showCustomId = false;
    isCreating = false;
  }

  function handleDeleteList(event: CustomEvent<{ list: List }>) {
    dispatch('deleteList', event.detail);
  }

  function handleViewList(event: CustomEvent<{ list: List; activeTab?: string }>) {
    dispatch('viewList', event.detail);
  }

  function handleFollowList(event: CustomEvent<{ list: List }>) {
    dispatch('followList', { ...event.detail, repository });
  }
</script>

<div class="list-manager">
  {#if !readOnly}
    <div class="mb-4">
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

        <div class="flex items-end gap-2">
          <button
            class="btn"
            on:click={() => showCustomId = !showCustomId}
            disabled={isCreating}
            title={showCustomId ? 'Use auto-generated ID' : 'Use custom ID'}
            class:active={showCustomId}
          >
            <span class="codicon codicon-gear"></span>
          </button>
        </div>

        <button
          class="btn primary"
          on:click={handleCreateList}
          disabled={!newListName.trim() || isCreating}
        >
          <span class="codicon codicon-add"></span>
          {isCreating ? 'Creating...' : 'Create List'}
        </button>
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
    </div>
  {/if}

  {#if lists.length === 0}
    <div class="empty">
      {#if readOnly}
        <p>No lists found in this repository.</p>
      {:else}
        <p>No lists created yet.</p>
        <p>Create a list to organize repositories you follow.</p>
      {/if}
    </div>
  {:else}
    <div>
      {#each lists as list (list.id)}
        <ListCard
          {list}
          {readOnly}
          showFollowButton={readOnly && repository}
          on:delete={handleDeleteList}
          on:viewList={handleViewList}
          on:follow={handleFollowList}
        />
      {/each}
    </div>
  {/if}
</div>
