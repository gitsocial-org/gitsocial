<script lang="ts">
  import { parseMarkdown } from '../utils/markdown';

  export let value = '';
  export let placeholder = '';
  export let disabled = false;
  export let creating = false;
  export let allowEmpty = false;
  export let onSubmit: (() => void) | undefined = undefined;
  export let onCancel: (() => void) | undefined = undefined;

  let showPreview = false;
  let previewHtml = '';

  $: previewHtml = parseMarkdown(value);
</script>

<div class="w-full">
  <div class="grid grid-cols-2 gap-2 w-full items-start">
    <!-- Row 1, Col 1: Controls -->
    <div class="flex justify-end items-center gap-3">
      <span class="text-xs text-muted">
        Markdown (
        <a href="https://github.github.com/gfm/">GFM</a>,
        <a href="https://katex.org/">KaTeX</a>,
        <a href="https://prismjs.com/">Prism</a>
        )
      </span>
      <button
        type="button"
        class="btn sm secondary"
        on:click={() => showPreview = !showPreview}
        title={showPreview ? 'Hide preview' : 'Show preview'}
      >
        <span class="codicon codicon-{showPreview ? 'eye-closed' : 'eye'}"></span>
        Preview
      </button>
    </div>

    <!-- Row 1, Col 2: Empty -->
    <div></div>

    <!-- Row 2, Col 1: Textarea + Buttons -->
    <div>
      <div class="w-full">
        <textarea
          bind:value
          {placeholder}
          {disabled}
          class="min-h-md p-4 font-mono"
        ></textarea>
      </div>

      {#if onSubmit && onCancel}
        <div class="flex gap-2 justify-end mt-2">
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
            disabled={(!value.trim() && !allowEmpty) || creating}
            on:click={onSubmit}
          >
            <span class="codicon codicon-save"></span>
            {creating ? 'Saving...' : 'Save'}
          </button>
        </div>
      {/if}
    </div>

    <!-- Row 2, Col 2: Preview -->
    {#if showPreview}
      <div class="markdown-content border bg-muted min-h-md px-4 py-2">
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html previewHtml}
      </div>
    {/if}
  </div>
</div>
