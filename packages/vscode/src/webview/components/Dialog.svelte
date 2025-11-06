<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';

  export let isOpen = false;
  export let title = '';
  export let size = 'w-90vw h-90vh';
  export let closeOnOverlay = true;
  export let closeOnEscape = true;
  export let focusOnMount = true;

  const dispatch = createEventDispatcher();
  let dialogElement: HTMLDivElement;

  onMount(() => {
    if (focusOnMount && dialogElement) {
      // Focus the dialog for keyboard accessibility
      dialogElement.focus();
    }
  });

  function handleOverlayClick(event: MouseEvent) {
    if (closeOnOverlay && event.target === event.currentTarget) {
      dispatch('close');
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (closeOnEscape && event.key === 'Escape') {
      event.preventDefault();
      dispatch('close');
    }
  }
</script>

{#if isOpen}
  <div class="dialog-overlay"
    role="button"
    tabindex="0"
    on:click={handleOverlayClick}
    on:keydown={handleKeydown}>
    <div class="dialog overflow-y-auto {size}"
      bind:this={dialogElement}
      role="dialog"
      aria-modal="true"
      aria-labelledby="dialog-title">

      {#if $$slots.header}
        <slot name="header" />
      {:else if title}
        <div class="flex justify-between items-center mb-4">
          <h3 id="dialog-title" class="m-0">{title}</h3>
          <button class="btn ghost sm" on:click={() => dispatch('close')}>
            <span class="codicon codicon-close"></span>
          </button>
        </div>
      {/if}

      <slot />

      {#if $$slots.footer}
        <slot name="footer" />
      {/if}
    </div>
  </div>
{/if}
