<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';

  const DEFAULT_REPO = 'https://github.com/gitsocial-org/gitsocial-official-lists#branch:main';
  let exploreListsSource = DEFAULT_REPO;

  onMount(() => {
    api.getSettings('exploreListsSource');

    window.addEventListener('message', (event) => {
      const message = event.data;
      if (message.type === 'settings' && message.data?.key === 'exploreListsSource') {
        exploreListsSource = message.data.value ?? DEFAULT_REPO;
        api.openView('repository', 'Explore', {
          repository: exploreListsSource,
          activeTab: 'lists'
        });
      }
    });
  });
</script>

<div class="view-container">
  <div class="empty">
    <span class="codicon codicon-loading spin"></span>
    <p>Loading official lists...</p>
  </div>
</div>
