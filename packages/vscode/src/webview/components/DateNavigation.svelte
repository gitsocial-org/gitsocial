<script lang="ts">
  export let offset: number;
  export let label: string;
  export let loading: boolean;
  export let onPrevious: () => void;
  export let onNext: () => void;
  export let onRefresh: (() => void) | undefined = undefined;
  export let refreshLoading = false;

  $: showRefresh = offset === 0 && onRefresh !== undefined;
  $: nextDisabled = showRefresh ? refreshLoading : (offset >= 0 || loading);
</script>

<div class="flex items-center gap-1">
  <button
    class="btn sm"
    on:click={onPrevious}
    title="Previous"
    disabled={loading}
  >
    <span class="codicon codicon-chevron-left"></span>
  </button>
  <div class="text-center px-1 flex items-center gap-1">
    {label}
    {#if loading}
      <span class="codicon codicon-loading spin"></span>
    {/if}
  </div>
  <button
    class="btn sm"
    on:click={showRefresh ? onRefresh : onNext}
    title={showRefresh ? 'Refresh' : 'Next'}
    disabled={nextDisabled}
  >
    {#if showRefresh}
      <span class="codicon codicon-{refreshLoading ? 'loading spin' : 'sync'}"></span>
    {:else}
      <span class="codicon codicon-chevron-right"></span>
    {/if}
  </button>
</div>
