<script lang="ts">
  import type { List } from '@gitsocial/core';
  import { createEventDispatcher } from 'svelte';
  import Avatar from './Avatar.svelte';
  import { gitHost, gitMsgRef } from '@gitsocial/core/client';
  import { api } from '../api';

  export let list: List;
  export let readOnly = false;
  export let showFollowButton = false;

  const dispatch = createEventDispatcher();

  let isDeleting = false;
  let isRenaming = false;
  let editingName = false;
  let newName = list.name;

  $: if (isRenaming && list.name === newName.trim()) {
    isRenaming = false;
    editingName = false;
  }

  function handleDelete() {
    isDeleting = true;
    dispatch('delete', { list });
  }

  function handleViewList() {
    // If list is empty, open directly to the Repositories tab
    const eventDetail = list.repositories.length === 0
      ? { list, activeTab: 'repositories' }
      : { list };
    dispatch('viewList', eventDetail);
  }

  function handleViewRepository(repoString: string) {
    return (event: MouseEvent) => {
      event.stopPropagation();
      event.preventDefault();
      try {
        const parsed = gitMsgRef.parseRepositoryId(repoString);
        const repositoryId = `${parsed.repository}#branch:${parsed.branch}`;
        api.openView('repository', gitHost.getDisplayName(parsed.repository), {
          repository: repositoryId
        });
      } catch (e) {
        console.error('Invalid repository identifier:', repoString, e);
      }
    };
  }

  function handleFollow() {
    dispatch('follow', { list });
  }

  function startRename(event: MouseEvent) {
    event.stopPropagation();
    editingName = true;
    newName = list.name;
  }

  function cancelRename() {
    editingName = false;
    newName = list.name;
  }

  function handleRename(event: SubmitEvent) {
    event.preventDefault();
    const trimmedName = newName.trim();
    if (!trimmedName || trimmedName === list.name) {
      cancelRename();
      return;
    }
    isRenaming = true;
    api.renameList(list.id, trimmedName);
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      cancelRename();
    }
  }
</script>

<div class="card pad hover cursor-pointer {list.isUnpushed ? 'border-warning' : ''} {list.source ? 'border-accent' : ''}" on:click={handleViewList} on:keydown={(e) => e.key === 'Enter' && handleViewList()} role="button" tabindex="0">
  <div class="flex justify-between items-center">
    <div class="flex-1 min-w-0">
      {#if editingName}
        <form on:submit={handleRename} class="flex items-center gap-2">
          <input
            type="text"
            bind:value={newName}
            on:keydown={handleKeydown}
            on:click|stopPropagation
            class="flex-1 text-lg font-semibold"
            placeholder="List name"
            disabled={isRenaming}
          />
          <button
            type="submit"
            class="btn sm"
            on:click|stopPropagation
            disabled={!newName.trim() || isRenaming}
            title="Save"
          >
            <span class="codicon codicon-check"></span>
          </button>
          <button
            type="button"
            class="btn sm"
            on:click|stopPropagation={cancelRename}
            disabled={isRenaming}
            title="Cancel"
          >
            <span class="codicon codicon-close"></span>
          </button>
        </form>
      {:else}
        <div class="flex items-center text-lg font-semibold truncate mb-1">
          {#if list.source}
            <span class="codicon codicon-sync mr-2" title="Following"></span>
          {/if}
          {list.name}
        </div>
      {/if}
      <div class="flex items-center text-sm text-muted">
        {#if list.repositories.length > 0}
          <div class="flex mr-2 gap-2 relative z-10">
            {#each list.repositories.slice(0, 10) as repoString}
              {@const repo = gitMsgRef.parseRepositoryId(repoString)}
              {#if repo}
                <span
                  class="cursor-pointer inline-block rounded-full hover-border-link"
                  on:click={handleViewRepository(repoString)}
                  on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && handleViewRepository(repoString)(e)}
                  title="View {gitHost.getDisplayName(repo.repository)}"
                  role="button"
                  tabindex="0"
                >
                  <Avatar
                    type="repository"
                    identifier={repo.repository}
                    name={gitHost.getDisplayName(repo.repository)}
                    size={16}
                  />
                </span>
              {/if}
            {/each}
          </div>
          {#if list.repositories.length > 0}
            <span class="mx-2">·</span><span>{list.repositories.length}</span><span class="mx-2">·</span>
          {/if}
        {:else}
          <span>Empty list, click to add repositories</span>
          <span class="mx-2">·</span>
        {/if}
        {#if list.source}
          <span class="text-xs">Following: {list.source}</span>
        {/if}
      </div>
    </div>

    {#if showFollowButton}
      <button
        class="btn btn-primary"
        title="Follow list"
        on:click|stopPropagation={handleFollow}
      >
        <span class="codicon codicon-add mr-1"></span>
        Follow
      </button>
    {:else if !readOnly}
      {#if !list.source && !editingName}
        <button
          class="btn"
          title="Rename list"
          on:click|stopPropagation={startRename}
          disabled={isDeleting || isRenaming}
        >
          <span class="codicon codicon-edit"></span>
        </button>
      {/if}
      <button
        class="btn"
        title="Delete list"
        on:click|stopPropagation={handleDelete}
        disabled={isDeleting || editingName}
      >
        <span class="codicon codicon-trash"></span>
      </button>
    {/if}
  </div>
</div>
