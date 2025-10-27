<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  export let tabs: Array<{
    id: string;
    label: string;
    icon?: string;
    customIcon?: string;
    count?: number;
    unpushedCount?: number
  }> = [];
  export let activeTab: string;

  const dispatch = createEventDispatcher();

  function handleTabClick(tabId: string) {
    if (tabId !== activeTab) {
      dispatch('change', { tabId });
    }
  }
</script>

<div class="mb-2">
  <div class="flex justify-between items-center border-b">
    <div class="flex items-center">
      {#each tabs as tab}
        <div
          class="px-4 py-3 transition-colors cursor-pointer {activeTab === tab.id ? 'tab-active' : 'tab-inactive'}"
          on:click={() => handleTabClick(tab.id)}
          role="button"
          tabindex="0"
          on:keydown={(e) => e.key === 'Enter' && handleTabClick(tab.id)}
        >
          <span class="flex items-center gap-2">
            {#if tab.icon}
              <span class={tab.icon}></span>
            {:else if tab.customIcon}
              <!-- eslint-disable-next-line svelte/no-at-html-tags -->
              {@html tab.customIcon}
            {/if}
            <span class="font-medium">{tab.label}</span>
            {#if tab.count !== undefined}
              <span class="ml-1 opacity-70">{tab.count}</span>
            {/if}
            {#if tab.unpushedCount !== undefined && tab.unpushedCount > 0}
              <span class="badge ml-2">{tab.unpushedCount}</span>
            {/if}
          </span>
        </div>
      {/each}
    </div>

    <div class="text-sm text-muted pr-4">
      <slot name="info" />
    </div>
  </div>
</div>
