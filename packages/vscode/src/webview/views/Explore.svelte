<script lang="ts" context="module">
  import { api } from '../api';

  export const DEFAULT_REPO = 'https://github.com/gitsocial-org/gitsocial-official-lists#branch:main';

  export function handleSettingsMessage(message: { type?: string; data?: { key?: string; value?: string } }): void {
    if (message.type === 'settings' && message.data?.key === 'exploreListsSource') {
      const repository = message.data.value ?? DEFAULT_REPO;
      api.openView('repository', 'Explore', {
        repository,
        activeTab: 'lists'
      });
    }
  }
</script>

<script lang="ts">
  import { onMount } from 'svelte';

  onMount(() => {
    api.getSettings('exploreListsSource');
    window.addEventListener('message', (event) => handleSettingsMessage(event.data));
  });
</script>

<div class="view-container">
  <div class="empty">
    <span class="codicon codicon-loading spin"></span>
    <p>Loading official lists...</p>
  </div>
</div>
