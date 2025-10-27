<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import type { List } from '@gitsocial/core/client';

  type NavItem = {
    id: string;
    label: string;
    icon: string;
    customIcon?: boolean;
    viewType: string;
  };

  const topNavItems: NavItem[] = [
    { id: 'timeline', label: 'Timeline', icon: 'gitsocial', customIcon: true, viewType: 'timeline' },
    { id: 'repository', label: 'My Repository', icon: 'codicon-home', viewType: 'repository' },
    { id: 'notifications', label: 'Notifications', icon: 'codicon-bell', viewType: 'notifications' },
    { id: 'search', label: 'Search', icon: 'codicon-search', viewType: 'search' },
    { id: 'explore', label: 'Explore', icon: 'codicon-compass', viewType: 'explore' }
  ];

  let currentActivePanelId = '';
  let unpushedCount = 0;
  let lists: List[] = [];
  let enableExplore = true;
  let exploreListsSource = 'https://github.com/gitsocial-org/gitsocial-official-lists#branch:main';

  $: myRepositoryActive = currentActivePanelId === 'repository';
  $: timelineActive = currentActivePanelId === 'timeline';
  $: notificationsActive = currentActivePanelId === 'notifications';
  $: searchActive = currentActivePanelId === 'search';
  $: exploreActive = currentActivePanelId === 'explore';
  $: sortedLists = [...lists].sort((a, b) =>
    a.name.localeCompare(b.name)
  ).map(list => ({
    ...list,
    active: currentActivePanelId === `viewList-${list.id}`
  }));
  $: visibleNavItems = topNavItems.filter(item =>
    item.id !== 'explore' || enableExplore
  );

  onMount(() => {
    // Request unpushed counts
    api.getUnpushedCounts();

    // Load settings
    api.getSettings('enableExplore');
    api.getSettings('exploreListsSource');

    // Listen for messages from extension
    window.addEventListener('message', (event) => {
      const message = event.data;
      switch (message.type) {
        case 'setActivePanel': {
          const newPanelId = message.data || '';
          if (currentActivePanelId !== newPanelId) {
            currentActivePanelId = newPanelId;
          }
          break;
        }
        case 'unpushedCounts':
          unpushedCount = message.data?.total || 0;
          break;
        case 'lists':
          lists = message.data || [];
          break;
        case 'postCreated':
          api.getUnpushedCounts();
          break;
        case 'refresh':
          api.getLists();
          api.getUnpushedCounts();
          break;
        case 'settings':
          if (message.data?.key === 'enableExplore') {
            enableExplore = message.data.value ?? true;
          } else if (message.data?.key === 'exploreListsSource') {
            exploreListsSource = message.data.value ?? 'https://github.com/gitsocial-org/gitsocial-official-lists#branch:main';
          }
          break;
      }
    });

    // Notify extension that sidebar is ready
    api.ready();

    // Request lists
    api.getLists();
  });

  function handleNavClick(item: NavItem) {
    if (item.id === 'explore') {
      api.openView('repository', 'Explore', {
        repository: exploreListsSource,
        activeTab: 'lists'
      });
    } else {
      api.openView(item.viewType, item.label);
    }
  }

  function handleCreatePost() {
    api.openView('createPost', 'Create Post');
  }

  function handleListClick(list: List) {
    api.openView('viewList', list.name, { listId: list.id, list });
  }
  function handleManageLists() {
    api.openView('repository', 'My Repository', { activeTab: 'lists' });
  }
</script>

<ul class="flex-1 overflow-y-auto p-2 m-0 list-none">
  {#each visibleNavItems as item}
    <li>
      <button
        class="btn ghost flex w-full gap-2 px-2 py-3 items-center justify-start text-left"
        class:active={item.id === 'timeline' ? timelineActive : item.id === 'repository' ? myRepositoryActive : item.id === 'notifications' ? notificationsActive : item.id === 'search' ? searchActive : item.id === 'explore' ? exploreActive : false}
        on:click={() => handleNavClick(item)}
      >
        {#if item.customIcon && item.icon === 'gitsocial'}
          <svg width="16" height="16" viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" style="flex-shrink: 0;">
            <!-- eslint-disable-next-line max-len -->
            <path d="m 191,100 c 0,3 -0.1,5 -0.3,8 C 187,148 158,181 118,189 75,198 33,175 16,135 -1,95 13,49 49,25 85,0 133,5 164,35 M 109,10 C 92,9 67,17 55,34 37,59 45,98 85,100 h 26 l 79,0" fill="none" stroke="currentColor" stroke-width="18" stroke-linecap="square" stroke-linejoin="round" />
          </svg>
        {:else}
          <span class="codicon {item.icon}"></span>
        {/if}
        <span class="flex-1">{item.label}</span>
        {#if item.id === 'repository' && unpushedCount > 0}
          <span class="badge">{unpushedCount}</span>
        {/if}
      </button>
    </li>
  {/each}

  <li class="border-b mt-3"></li>

  <li>
    <div class="flex items-center justify-between px-2 pt-2">
      <span class="text-sm font-medium text-muted">Lists {#if sortedLists.length > 0}({sortedLists.length}){/if}</span>
      <div class="flex gap-1">
        <button
          class="btn ghost sm"
          on:click={handleManageLists}
          title="Create new list"
        >
          <span class="codicon codicon-add"></span>
        </button>
        <button
          class="btn ghost sm"
          on:click={handleManageLists}
          title="Manage lists"
        >
          <span class="codicon codicon-gear"></span>
        </button>
      </div>
    </div>
  </li>

  {#if sortedLists.length > 0}
    {#each sortedLists as list}
      <li>
        <button
          class="btn ghost flex w-full gap-2 px-2 py-3 items-center justify-start text-left"
          class:active={list.active}
          on:click={() => handleListClick(list)}
        >
          <svg class="flex-shrink-0" width="14" height="14" viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg">
            <circle fill="currentColor" cx="3" cy="4" r="1" />
            <rect fill="currentColor" x="6" y="3.5" width="8" height="1" />
            <circle fill="currentColor" cx="3" cy="8" r="1" />
            <rect fill="currentColor" x="6" y="7.5" width="8" height="1" />
            <circle fill="currentColor" cx="3" cy="12" r="1" />
            <rect fill="currentColor" x="6" y="11.5" width="8" height="1" />
          </svg>
          <span class="flex-1">{list.name}</span>
        </button>
      </li>
    {/each}
  {:else}
    <li>
      <div class="px-2 py-2 text-sm text-muted italic">
        No lists yet. Click + to create one.
      </div>
    </li>
  {/if}

  <li class="border-b my-3"></li>
</ul>

<div class="w-full px-2">
  <button class="btn primary w-full flex items-center justify-center" on:click={handleCreatePost} title="Create Post">
    <span class="codicon codicon-edit"></span>
    <span>New Post</span>
  </button>
</div>
