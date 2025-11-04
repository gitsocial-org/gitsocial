<script lang="ts">
  import { parseMarkdown } from '../utils/markdown';

  export let value = '';
  export let placeholder = '';
  export let disabled = false;
  export let creating = false;
  export let onSubmit: (() => void) | undefined = undefined;
  export let onCancel: (() => void) | undefined = undefined;

  let showPreview = false;
  let previewHtml = '';

  $: previewHtml = parseMarkdown(value);
</script>

<div class="w-full">
  <div class="flex justify-end items-center gap-3 mb-2 max-w-2xl">
    <span class="text-xs text-muted">
      Markdown (
      <a href="https://github.github.com/gfm/">GFM</a>,
      <a href="https://katex.org/">KaTeX</a>,
      <a href="https://prismjs.com/">Prism</a>
      )
    </span>
    <button
      type="button"
      class="btn secondary sm"
      on:click={() => showPreview = !showPreview}
      title={showPreview ? 'Hide preview' : 'Show preview'}
    >
      <span class="codicon codicon-{showPreview ? 'eye-closed' : 'eye'}"></span>
      {showPreview ? 'Hide' : 'Show'} Preview
    </button>
  </div>

  <div class="grid grid-cols-2 gap-2 w-full items-start">
    <div class="max-w-2xl">
      <div class="w-full">
        <textarea
          bind:value
          {placeholder}
          {disabled}
          class="min-h-md p-4 font-mono"
        ></textarea>
      </div>

      {#if onSubmit && onCancel}
        <div class="flex gap-2 justify-end mt-2 max-w-2xl">
          <button
            type="button"
            class="btn"
            on:click={onCancel}
            disabled={creating}
          >
            Cancel
          </button>
          <button
            type="button"
            class="btn primary wide"
            disabled={!value.trim() || creating}
            on:click={onSubmit}
          >
            <span class="codicon codicon-save"></span>
            {creating ? 'Saving...' : 'Save'}
          </button>
        </div>
      {/if}
    </div>

    {#if showPreview}
      <div class="markdown-content border min-h-md px-4 py-2">
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html previewHtml}
      </div>
    {/if}
  </div>
</div>
