<script lang="ts">
  import type { List } from '@gitsocial/core';
  import { createEventDispatcher } from 'svelte';
  import ListCard from './ListCard.svelte';
  import ListCreateForm from './ListCreateForm.svelte';

  export let lists: List[] = [];
  export let readOnly = false;
  export let repository: string | undefined = undefined;

  const dispatch = createEventDispatcher();

  function handleCreateList(event: CustomEvent<{ id: string; name: string }>) {
    dispatch('createList', event.detail);
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
  function handleUnfollowList(event: CustomEvent<{ list: List }>) {
    dispatch('unfollowList', event.detail);
  }
</script>

<div class="list-manager">
  {#if !readOnly}
    <div class="mb-4">
      <ListCreateForm
        on:createList={handleCreateList}
      />
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
    <div class="flex flex-col gap-2">
      {#each lists as list (list.id)}
        <ListCard
          {list}
          {readOnly}
          showFollowButton={readOnly && repository}
          on:delete={handleDeleteList}
          on:viewList={handleViewList}
          on:follow={handleFollowList}
          on:unfollow={handleUnfollowList}
        />
      {/each}
    </div>
  {/if}
</div>
