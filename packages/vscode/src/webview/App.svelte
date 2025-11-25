<script lang="ts">
  import { onMount } from 'svelte';
  import { error } from './stores';
  import { api } from './api';
  import Repository from './views/Repository.svelte';
  import { webLog } from './utils/weblog';

  onMount(() => {
    // Notify extension that webview is ready
    api.ready();
  });

  // Subscribe to error messages
  $: if ($error) {
    webLog('error', 'GitSocial error:', $error);
  }

</script>

<main>
  <Repository />
</main>
