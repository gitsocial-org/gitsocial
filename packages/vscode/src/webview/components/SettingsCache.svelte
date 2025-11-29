<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  interface CacheStats {
    enabled: boolean;
    postsCache: { size: number; maxSize: number };
  }

  interface AvatarCacheStats {
    memoryCacheSize: number;
    fileStats?: {
      totalFiles: number;
      diskUsage: number;
      tempFiles: number;
    };
  }

  interface RepositoryStorageStats {
    totalRepositories: number;
    diskUsage: number;
    persistent: number;
    temporary: number;
  }

  interface StoragePath {
    base: string | null;
    avatars: string | null;
    repositories: string | null;
  }

  let cacheStats: CacheStats | null = null;
  let storagePath: StoragePath | null = null;
  let avatarCacheStats: AvatarCacheStats | null = null;
  let repositoryStorageStats: RepositoryStorageStats | null = null;
  let loading = false;
  let avatarLoading = false;
  let repositoryStorageLoading = false;
  let operationInProgress = false;
  let avatarOperationInProgress = false;
  let repositoryStorageOperationInProgress = false;
  let error: string | null = null;
  let successMessage: string | null = null;
  let cacheMaxSize = 100000; // Default value
  let newCacheMaxSize = 100000;

  // Load cache stats when component mounts or becomes visible
  export function loadCacheStats(): void {
    loading = true;
    avatarLoading = true;
    repositoryStorageLoading = true;
    window.vscode.postMessage({ type: 'getCacheStats' });
    window.vscode.postMessage({ type: 'getAvatarCacheStats' });
    window.vscode.postMessage({ type: 'getRepositoryStorageStats' });
    window.vscode.postMessage({ type: 'getStoragePath' });
  }

  const clearCache = (): void => {
    operationInProgress = true;
    window.vscode.postMessage({ type: 'clearCache' });
  };

  const clearPostsCache = (): void => {
    operationInProgress = true;
    window.vscode.postMessage({ type: 'clearCache' });
  };

  const refreshStats = (): void => {
    loadCacheStats();
  };

  const clearAvatarCache = (options = { clearMemoryCache: true }): void => {
    avatarOperationInProgress = true;
    window.vscode.postMessage({ type: 'clearAvatarCache', options });
  };

  const clearRepositoryCache = (): void => {
    repositoryStorageOperationInProgress = true;
    window.vscode.postMessage({ type: 'clearRepositoryCache' });
  };

  const formatNumber = (num: number): string => {
    return new Intl.NumberFormat().format(num);
  };

  const updateCacheMaxSize = (): void => {
    if (newCacheMaxSize < 1000 || newCacheMaxSize > 1000000 || isNaN(newCacheMaxSize)) {
      error = 'Cache size must be between 1,000 and 1,000,000';
      setTimeout(() => (error = null), 5000);
      return;
    }
    operationInProgress = true;
    window.vscode.postMessage({ type: 'setCacheMaxSize', value: newCacheMaxSize });
  };

  const formatMemoryEstimate = (posts: number): string => {
    const mb = (posts * 3 / 1024).toFixed(1);
    return `~${mb} MB`;
  };

  // Message handler
  function handleMessage(event: MessageEvent): void {
    const message = event.data;

    switch (message.type) {
      case 'cacheStats':
        cacheStats = message.data;
        if (cacheStats?.postsCache?.maxSize) {
          cacheMaxSize = cacheStats.postsCache.maxSize;
          newCacheMaxSize = cacheStats.postsCache.maxSize;
        }
        loading = false;
        break;

      case 'avatarCacheStats':
        avatarCacheStats = message.data;
        avatarLoading = false;
        break;

      case 'repositoryStorageStats':
        repositoryStorageStats = message.data;
        repositoryStorageLoading = false;
        break;

      case 'storagePath':
        storagePath = message.data;
        break;

      case 'cacheCleared':
        successMessage = 'Cache cleared successfully';
        setTimeout(() => (successMessage = null), 3000);
        operationInProgress = false;
        // Reload stats
        loadCacheStats();
        break;

      case 'avatarCacheCleared': {
        const result = message.data;
        const clearedInfo = [];
        if (result.clearedMemoryEntries > 0) {
          clearedInfo.push(`${result.clearedMemoryEntries} memory entries`);
        }
        if (result.filesDeleted > 0) {
          clearedInfo.push(`${result.filesDeleted} files`);
        }
        successMessage = clearedInfo.length > 0
          ? `Avatar cache cleared: ${clearedInfo.join(', ')}`
          : 'Avatar cache cleared successfully';
        setTimeout(() => (successMessage = null), 3000);
        avatarOperationInProgress = false;
        // Reload stats
        loadCacheStats();
        break;
      }

      case 'repositoryCacheCleared': {
        const result = message.data;
        if (result.deletedCount > 0) {
          const deletedMsg = result.deletedCount === 1
            ? '1 repository'
            : `${result.deletedCount} repositories`;
          const freedMsg = (result.diskSpaceFreed / 1024 / 1024).toFixed(1) + ' MB';
          successMessage = `Repository cache cleared: ${deletedMsg} (${freedMsg})`;
        } else {
          successMessage = 'Repository cache cleared (no cached repositories found)';
        }
        setTimeout(() => (successMessage = null), 3000);
        repositoryStorageOperationInProgress = false;
        // Reload stats
        loadCacheStats();
        break;
      }

      case 'cacheMaxSizeUpdated':
        successMessage = 'Cache size updated successfully';
        setTimeout(() => (successMessage = null), 3000);
        operationInProgress = false;
        cacheMaxSize = newCacheMaxSize;
        // Reload stats to reflect new size
        loadCacheStats();
        break;

      case 'error':
        if ((message.context === 'cache' && (loading || operationInProgress)) ||
          (message.context === 'avatarCache' && (avatarLoading || avatarOperationInProgress)) ||
          (message.context === 'repositoryStorage' && (repositoryStorageLoading || repositoryStorageOperationInProgress))) {
          error = message.message || 'An error occurred';
          setTimeout(() => (error = null), 5000);
          loading = false;
          operationInProgress = false;
          avatarLoading = false;
          avatarOperationInProgress = false;
          repositoryStorageLoading = false;
          repositoryStorageOperationInProgress = false;
        }
        break;
    }
  }

  onMount(() => {
    window.addEventListener('message', handleMessage);
    loadCacheStats();
  });

  onDestroy(() => {
    window.removeEventListener('message', handleMessage);
  });
</script>

<div class="p-4 rounded border">
  {#if error}
    <div class="text-xs text-error mb-2">{error}</div>
  {/if}
  {#if successMessage}
    <div class="text-xs text-muted mb-2">✓ {successMessage}</div>
  {/if}

  {#if loading || avatarLoading || repositoryStorageLoading}
    <div class="flex items-center gap-2 text-sm text-muted">
      <span class="codicon codicon-loading spin"></span>
      Loading statistics...
    </div>
  {:else if cacheStats}
    <div class="space-y-4">
      {#if storagePath?.base}
        <div class="pb-3 border-b">
          <div class="text-sm mb-1">Storage Location</div>
          <div class="text-xs text-muted break-all font-mono">{storagePath.base}</div>
        </div>
      {/if}

      <div class="flex justify-between items-center pb-3 border-b">
        <div class="text-sm">Status</div>
        <div class="text-sm font-medium {cacheStats.enabled ? 'text-muted' : 'text-error'}">
          {cacheStats.enabled ? 'Enabled' : 'Disabled'}
        </div>
      </div>

      <div class="pb-3 border-b">
        <div class="flex justify-between items-center mb-2">
          <div class="text-sm">Cache Size Limit</div>
          <div class="text-xs text-muted">{formatMemoryEstimate(newCacheMaxSize)}</div>
        </div>
        <div class="flex gap-2 items-center">
          <input
            type="number"
            bind:value={newCacheMaxSize}
            min="1000"
            max="1000000"
            step="10000"
            class="flex-1 text-sm"
            disabled={operationInProgress || !cacheStats.enabled}
          />
          <button
            class="btn subtle sm"
            on:click={updateCacheMaxSize}
            disabled={operationInProgress || !cacheStats.enabled || newCacheMaxSize === cacheMaxSize}
            title="Update cache size"
          >
            <span class="codicon codicon-check"></span>
          </button>
        </div>
        <div class="text-xs text-muted mt-1">
          Maximum posts to keep in memory (1,000 - 1,000,000)
        </div>
      </div>

      <div class="flex justify-between items-center">
        <div>
          <div class="text-sm">Post Cache</div>
          <div class="text-xs text-muted">Individual posts</div>
        </div>
        <div class="flex items-center gap-2">
          <div class="text-sm font-medium">
            {cacheStats.postsCache ? `${formatNumber(cacheStats.postsCache.size)} / ${formatNumber(cacheStats.postsCache.maxSize)}` : 'N/A'}
          </div>
          <button
            class="btn subtle sm"
            on:click={clearPostsCache}
            disabled={operationInProgress || avatarOperationInProgress || !cacheStats.enabled}
            title="Clear post cache"
          >
            <span class="codicon codicon-trash text-xs"></span>
          </button>
        </div>
      </div>

      {#if avatarCacheStats}
        <div class="flex justify-between items-center">
          <div>
            <div class="text-sm">Avatar Cache</div>
            <div class="text-xs text-muted">
              Memory: {formatNumber(avatarCacheStats.memoryCacheSize)}
              ({(avatarCacheStats.memoryCacheSize * 8 / 1024).toFixed(1)} MB)
              {#if avatarCacheStats.fileStats}
                · Disk: {formatNumber(avatarCacheStats.fileStats.totalFiles)} files
                ({(avatarCacheStats.fileStats.diskUsage / 1024 / 1024).toFixed(1)} MB)
              {/if}
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button
              class="btn subtle sm"
              on:click={() => clearAvatarCache({ clearAllFiles: true, clearMemoryCache: true })}
              disabled={operationInProgress || avatarOperationInProgress}
              title="Clear avatar cache"
            >
              <span class="codicon codicon-trash text-xs"></span>
            </button>
          </div>
        </div>
      {/if}

      {#if repositoryStorageStats}
        <div class="flex justify-between items-center">
          <div>
            <div class="text-sm">Repository Storage</div>
            <div class="text-xs text-muted">
              {formatNumber(repositoryStorageStats.totalRepositories)} repositories
              ({repositoryStorageStats.persistent} persistent, {repositoryStorageStats.temporary} temporary)
              · {(repositoryStorageStats.diskUsage / 1024 / 1024).toFixed(1)} MB
            </div>
          </div>
          <div class="flex items-center gap-2">
            <button
              class="btn subtle sm"
              on:click={clearRepositoryCache}
              disabled={operationInProgress || avatarOperationInProgress || repositoryStorageOperationInProgress}
              title="Clear all cached repositories"
            >
              <span class="codicon codicon-trash text-xs"></span>
            </button>
          </div>
        </div>
      {/if}

      <div class="flex gap-2 pt-3 border-t">
        <button
          class="btn subtle sm"
          on:click={refreshStats}
          disabled={loading || avatarLoading || repositoryStorageLoading ||
            operationInProgress || avatarOperationInProgress || repositoryStorageOperationInProgress}
          title="Refresh statistics"
        >
          <span class="codicon codicon-refresh"></span>
          <span class="text-xs">Refresh</span>
        </button>
        <button
          class="btn subtle sm"
          on:click={clearCache}
          disabled={operationInProgress || avatarOperationInProgress ||
            repositoryStorageOperationInProgress || !cacheStats.enabled}
          title="Clear all caches"
        >
          <span class="codicon codicon-trash"></span>
          <span class="text-xs">Clear all</span>
        </button>
      </div>
    </div>
  {:else}
    <div class="text-sm text-muted">Failed to load statistics</div>
  {/if}
</div>
