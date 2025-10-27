<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import type { WebviewMessage as _ } from '../types';

  type BranchInfo = {
    name: string;
    location: 'local' | 'remote' | 'both';
    isCurrent: boolean;
  };

  let isInitializing = false;
  let initError = '';
  let currentRequestId: string | null = null;
  let checkRequestId: string | null = null;

  let branches: BranchInfo[] = [];
  let configuredBranch: string | null = null;
  let selectionMode: 'existing' | 'new' = 'new';
  let selectedExistingBranch = '';
  let newBranchName = '';
  let validationError = '';

  function getBranchLabel(branch: BranchInfo): string {
    const parts = [];
    if (branch.isCurrent) {parts.push('current');}
    if (branch.location === 'both') {parts.push('local + remote');}
    else if (branch.location === 'local') {parts.push('local only');}
    else {parts.push('remote only');}
    return parts.join(', ');
  }

  function validateNewBranch(): boolean {
    validationError = '';
    if (!newBranchName.trim()) {
      validationError = 'Branch name cannot be empty';
      return false;
    }
    if (branches.some(b => b.name === newBranchName.trim())) {
      validationError = 'Branch already exists';
      return false;
    }
    return true;
  }

  function handleInitialize() {
    validationError = '';
    let branchToUse = '';

    if (selectionMode === 'existing') {
      if (!selectedExistingBranch) {
        validationError = 'Please select a branch';
        return;
      }
      branchToUse = selectedExistingBranch;
    } else {
      if (!validateNewBranch()) {return;}
      branchToUse = newBranchName.trim();
    }

    isInitializing = true;
    initError = '';
    currentRequestId = Date.now().toString(36) + Math.random().toString(36);
    api.initializeRepository(currentRequestId, '', branchToUse, '');
  }

  onMount(() => {
    checkRequestId = Date.now().toString(36) + Math.random().toString(36);
    api.checkGitSocialInit(checkRequestId);

    window.addEventListener('message', (event) => {
      const message = event.data;

      if (message.type === 'gitSocialStatus' && message.requestId === checkRequestId) {
        if (message.data?.branches) {
          branches = message.data.branches;
        }
        if (message.data?.configuredBranch !== undefined) {
          configuredBranch = message.data.configuredBranch;
        }

        // Set smart defaults
        if (configuredBranch && branches.some(b => b.name === configuredBranch)) {
          selectionMode = 'existing';
          selectedExistingBranch = configuredBranch;
        } else {
          selectionMode = 'new';
          const gitSocialExists = branches.some(b => b.name === 'gitsocial');
          newBranchName = gitSocialExists ? '' : 'gitsocial';
        }

        checkRequestId = null;
      }

      if (message.requestId && message.requestId !== currentRequestId) {
        return;
      }

      if (message.type === 'repositoryInitialized' && message.requestId === currentRequestId) {
        isInitializing = false;
        currentRequestId = null;
        api.openView('timeline', 'Timeline');
        setTimeout(() => api.closePanel(), 100);
      } else if (message.type === 'initializationError' && message.requestId === currentRequestId) {
        isInitializing = false;
        currentRequestId = null;
        initError = message.data?.message || 'Failed to initialize repository';
      }
    });
  });
</script>

<div class="p-6 max-w-2xl mx-auto">
  <!-- Header -->
  <div class="sticky z-20 top-0 -ml-4 -mr-4 p-4 pb-2 bg-sidebar mb-8 text-center">
    <h1 class="justify-center"><span class="codicon codicon-lg codicon-heart mr-2"></span>Welcome to GitSocial</h1>
    <p class="text-muted">Choose branch for GitSocial content</p>
  </div>

  {#if initError}
    <div class="p-3 mb-6 border border-l-4 rounded text-sm border-error bg-danger">
      {initError}
    </div>
  {/if}

  {#if validationError}
    <div class="p-3 mb-6 border border-l-4 rounded text-sm border-error bg-danger">
      {validationError}
    </div>
  {/if}

  <div class="p-4 rounded border space-y-4">
    <label class="flex items-start gap-3 cursor-pointer">
      <input
        type="radio"
        bind:group={selectionMode}
        value="existing"
        disabled={isInitializing || branches.length === 0}
        class="mt-1"
      />
      <div class="flex-1">
        <div class="text-sm font-medium mb-2">Use existing branch</div>
        <select
          bind:value={selectedExistingBranch}
          disabled={isInitializing || selectionMode !== 'existing' || branches.length === 0}
          class="w-full"
        >
          <option value="">Select branch...</option>
          {#each branches as branch}
            <option value={branch.name}>
              {branch.name} ({getBranchLabel(branch)})
            </option>
          {/each}
        </select>
        <p class="text-xs text-muted mt-1">
          Use an existing branch for social content
        </p>
      </div>
    </label>

    <label class="flex items-start gap-3 cursor-pointer">
      <input
        type="radio"
        bind:group={selectionMode}
        value="new"
        disabled={isInitializing}
        class="mt-1"
      />
      <div class="flex-1">
        <div class="text-sm font-medium mb-2">Create new branch</div>
        <input
          type="text"
          bind:value={newBranchName}
          disabled={isInitializing || selectionMode !== 'new'}
          placeholder="gitsocial"
          class="w-full"
        />
        <p class="text-xs text-muted mt-1">
          Branch will be created on first post
        </p>
      </div>
    </label>
  </div>

  <div class="mt-6 flex justify-center">
    <button
      class="btn primary"
      on:click={handleInitialize}
      disabled={isInitializing}
    >
      {#if isInitializing}
        <span class="codicon codicon-loading spin mr-2"></span>
        Initializing...
      {:else}
        Initialize GitSocial
      {/if}
    </button>
  </div>

  <div class="mt-8 text-center">
    <p class="text-xs text-muted">
      You can reconfigure this later in Settings
    </p>
  </div>
</div>
