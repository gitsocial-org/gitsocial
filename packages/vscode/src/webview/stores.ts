import { writable } from 'svelte/store';
import type { List, Post } from '@gitsocial/core/client';
import type { ExtensionMessage } from '../handlers';

// Core data stores
export const posts = writable<Post[]>([]);
export const lists = writable<List[]>([]);
export const loading = writable(false);
export const error = writable<string | null>(null);

// Refresh trigger store
export const refreshTrigger = writable(0);

// Skip cache flag for refresh after fetch
export const skipCacheOnNextRefresh = writable(false);

// Repository info
export const repositoryInfo = writable<{
  url: string;
  name: string;
  branch: string;
} | null>(null);

// Settings store
export const settings = writable<Record<string, unknown>>({});

// Handle messages from extension
export function handleExtensionMessage(message: ExtensionMessage): void {
  switch (message.type) {
  case 'posts':
    posts.set(message.data);
    error.set(null);
    break;

  case 'lists':
    lists.set(message.data);
    error.set(null);
    break;

  case 'postCreated':
    posts.update(current => [(message.data as { post: Post }).post, ...current]);
    error.set(null);
    break;

  case 'error':
    error.set(message.data?.message || 'Unknown error');
    break;

  case 'loading':
    loading.set(message.data?.value || false);
    if (message.data?.value) {
      error.set(null);
    }
    break;

  case 'repositoryInfo':
    repositoryInfo.set(message.data);
    break;

  case 'refresh':
    // Trigger refresh in views by incrementing the trigger
    refreshTrigger.update(n => n + 1);
    break;

  case 'refreshAfterFetch':
    // Trigger refresh with cache skip after repository fetch
    skipCacheOnNextRefresh.set(true);
    refreshTrigger.update(n => n + 1);
    break;

  case 'settings':
    if (message.data?.key) {
      settings.update(current => ({
        ...current,
        [message.data.key]: message.data.value
      }));
    }
    break;
  }
}
