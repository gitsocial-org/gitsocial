<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import type { WebviewMessage as _ } from '../types';
  import { getWebLogLevel, setWebLogLevel, type LogLevel } from '../utils/weblog';
  import SettingsCache from '../components/SettingsCache.svelte';

  let isInitialized = false;
  let currentBranch = '';
  let setupType = '';
  let loading = true;
  let error = '';
  let requestId: string | null = null;
  let currentLogLevel: LogLevel = 'error';
  let enableGravatar = false;
  let autoLoadImages = true;
  let enableExplore = true;
  let exploreListsSource = '';
  const defaultExploreListsSource = 'https://github.com/gitsocial-org/gitsocial-official-lists';

  function checkRepositoryStatus() {
    loading = true;
    error = '';

    // Generate request ID
    requestId = Date.now().toString(36) + Math.random().toString(36);

    // Check GitSocial initialization status
    api.checkGitSocialInit(requestId);
  }

  onMount(async () => {
    // Load current log level
    currentLogLevel = getWebLogLevel();

    // Load settings
    api.getSettings('enableGravatar');
    api.getSettings('autoLoadImages');
    api.getSettings('enableExplore');
    api.getSettings('exploreListsSource');

    checkRepositoryStatus();

    // Listen for responses
    window.addEventListener('message', (event) => {
      const message = event.data;

      // Handle settings response
      if (message.type === 'settings') {
        if (message.data?.key === 'enableGravatar') {
          enableGravatar = message.data.value ?? false;
        } else if (message.data?.key === 'autoLoadImages') {
          autoLoadImages = message.data.value ?? true;
        } else if (message.data?.key === 'enableExplore') {
          enableExplore = message.data.value ?? true;
        } else if (message.data?.key === 'exploreListsSource') {
          exploreListsSource = message.data.value ?? defaultExploreListsSource;
        }
      }

      // Only handle responses for our request
      if (message.requestId === requestId) {
        if (message.type === 'gitSocialStatus') {
          loading = false;
          error = '';
          isInitialized = message.data?.isInitialized || false;
          currentBranch = message.data?.currentBranch || '';
          setupType = message.data?.setupType || '';
          requestId = null;
        } else if (message.type === 'error') {
          loading = false;
          error = message.data?.message || 'Failed to check repository status';
          isInitialized = false;
          currentBranch = '';
          setupType = '';
          requestId = null;
        }
      }
    });
  });

  // Note: Settings data doesn't need refresh on interactions

  function openInitialization() {
    api.openView('welcome', 'GitSocial Configuration');
  }

  function handleLogLevelChange(event: Event) {
    const target = event.target as HTMLSelectElement;
    const newLevel = target.value as LogLevel;
    currentLogLevel = newLevel;
    setWebLogLevel(newLevel);
  }

  function handleGravatarToggle() {
    api.updateSettings('enableGravatar', enableGravatar);
  }

  function handleAutoLoadImagesToggle() {
    api.updateSettings('autoLoadImages', autoLoadImages);
  }

  function handleExploreToggle() {
    api.updateSettings('enableExplore', enableExplore);
  }

  function handleExploreListsSourceChange() {
    api.updateSettings('exploreListsSource', exploreListsSource);
  }

  function handleExploreListsSourceReset() {
    exploreListsSource = defaultExploreListsSource;
    api.updateSettings('exploreListsSource', exploreListsSource);
  }
</script>

<div>
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar">
    <h1><span class="codicon codicon-lg codicon-settings-gear mr-2"></span>Settings</h1>
  </div>

  <section class="mb-6">
    <h2>Repository</h2>

    {#if loading}
      <div class="flex items-center gap-2 text-sm text-muted">
        <span class="codicon codicon-loading spin"></span>
        Checking status...
      </div>
    {:else if error}
      <div class="p-4 rounded border">
        <div class="flex items-center gap-2 text-sm text-error mb-3">
          <span class="codicon codicon-error"></span>
          {error}
        </div>
        <button
          class="btn primary sm"
          on:click={openInitialization}
        >
          Initialize GitSocial
        </button>
      </div>
    {:else}
      <div class="p-4 rounded border">
        <div class="flex justify-between items-center mb-3">
          <span class="text-sm font-medium">Status</span>
          <span class="px-2 py-1 text-xs rounded-full {isInitialized ? 'bg-success text-white' : 'border text-muted'}">
            {isInitialized ? '✓ Active' : 'Inactive'}
          </span>
        </div>

        {#if isInitialized}
          <div class="space-y-2 text-sm">
            <div>
              <span class="text-muted">Current branch:</span>
              <span class="ml-2"><code class="text-xs">{currentBranch}</code></span>
            </div>
            <div>
              <span class="text-muted">Setup type:</span>
              <span class="ml-2">{setupType}</span>
            </div>
          </div>
        {/if}

        <button
          class="btn {isInitialized ? 'secondary' : 'primary'} subtle sm mt-3"
          on:click={openInitialization}
        >
          {isInitialized ? 'Reconfigure' : 'Initialize'}
        </button>
      </div>
    {/if}
  </section>

  <section class="mb-6">
    <h2>Logging</h2>

    <div class="p-4 rounded border">
      <label for="log-level" class="block text-sm font-medium mb-2">Console output</label>
      <select
        id="log-level"
        bind:value={currentLogLevel}
        on:change={handleLogLevelChange}
        class="w-full"
      >
        <option value="off">Off</option>
        <option value="error">Error</option>
        <option value="warn">Warning</option>
        <option value="info">Info</option>
        <option value="debug">Debug</option>
        <option value="verbose">Verbose</option>
      </select>
      <p class="text-xs text-muted mt-2">
        Verbose for detailed debugging, Debug for development, Error for normal use
      </p>
    </div>
  </section>

  <section class="mb-6">
    <h2>Interface</h2>

    <div class="p-4 rounded border space-y-4">
      <label class="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          bind:checked={enableExplore}
          on:change={handleExploreToggle}
        />
        <div class="flex-1">
          <div class="text-sm font-medium">Show Explore tab</div>
          <p class="text-xs text-muted mt-1">
            Display the Explore tab in the sidebar for discovering curated lists.
          </p>
        </div>
      </label>

      <div class="pt-2">
        <label for="explore-source" class="block text-sm font-medium mb-2">Explore lists source</label>
        <div class="flex gap-2">
          <input
            id="explore-source"
            type="text"
            bind:value={exploreListsSource}
            on:blur={handleExploreListsSourceChange}
            placeholder={defaultExploreListsSource}
            class="flex-1"
          />
          <button
            class="btn secondary sm"
            on:click={handleExploreListsSourceReset}
            disabled={exploreListsSource === defaultExploreListsSource}
          >
            Reset
          </button>
        </div>
        <p class="text-xs text-muted mt-2">
          Repository URL containing curated lists for the Explore tab.
        </p>
      </div>
    </div>
  </section>

  <section class="mb-6">
    <h2>Privacy</h2>

    <div class="p-4 rounded border space-y-4">
      <label class="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          bind:checked={enableGravatar}
          on:change={handleGravatarToggle}
        />
        <div class="flex-1">
          <div class="text-sm font-medium">Enable Gravatar</div>
          <p class="text-xs text-muted mt-1">
            Use Gravatar as fallback for user avatars. When disabled, only avatars from the git
            repository host and generated avatars are used.
          </p>
        </div>
      </label>

      <label class="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          bind:checked={autoLoadImages}
          on:change={handleAutoLoadImagesToggle}
        />
        <div class="flex-1">
          <div class="text-sm font-medium">Auto-load images</div>
          <p class="text-xs text-muted mt-1">
            Automatically load images in post content. When disabled, images show as
            click-to-load placeholders for privacy and bandwidth control.
          </p>
        </div>
      </label>
    </div>
  </section>

  <section class="mb-6">
    <h2>Cache</h2>
    <SettingsCache />
  </section>
</div>
