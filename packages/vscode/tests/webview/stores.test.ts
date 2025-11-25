import { beforeEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import type { Post } from '@gitsocial/core/client';
import type { ExtensionMessage } from '../src/handlers';
import {
  error,
  handleExtensionMessage,
  lists,
  loading,
  posts,
  refreshTrigger,
  repositoryInfo,
  settings,
  skipCacheOnNextRefresh
} from '../../src/webview/stores';

describe('Stores', () => {
  beforeEach(() => {
    posts.set([]);
    lists.set([]);
    loading.set(false);
    error.set(null);
    refreshTrigger.set(0);
    skipCacheOnNextRefresh.set(false);
    repositoryInfo.set(null);
    settings.set({});
  });

  describe('handleExtensionMessage', () => {
    describe('posts message', () => {
      it('sets posts data', () => {
        const postData = [
          { id: 'post-1', content: 'Test post', author: { name: 'User' } }
        ];
        handleExtensionMessage({
          type: 'posts',
          data: postData
        } as ExtensionMessage);
        expect(get(posts)).toEqual(postData);
      });

      it('clears error', () => {
        error.set('Previous error');
        handleExtensionMessage({
          type: 'posts',
          data: []
        } as ExtensionMessage);
        expect(get(error)).toBeNull();
      });
    });

    describe('lists message', () => {
      it('sets lists data', () => {
        const listData = [
          { id: 'list-1', name: 'My List' }
        ];
        handleExtensionMessage({
          type: 'lists',
          data: listData
        } as ExtensionMessage);
        expect(get(lists)).toEqual(listData);
      });

      it('clears error', () => {
        error.set('Previous error');
        handleExtensionMessage({
          type: 'lists',
          data: []
        } as ExtensionMessage);
        expect(get(error)).toBeNull();
      });
    });

    describe('postCreated message', () => {
      it('prepends new post to posts', () => {
        const existingPost = { id: 'post-1', content: 'Old', author: { name: 'User' } };
        const newPost = { id: 'post-2', content: 'New', author: { name: 'User' } };
        posts.set([existingPost] as Post[]);

        handleExtensionMessage({
          type: 'postCreated',
          data: { post: newPost }
        } as ExtensionMessage);

        const currentPosts = get(posts);
        expect(currentPosts).toHaveLength(2);
        expect(currentPosts[0]).toEqual(newPost);
        expect(currentPosts[1]).toEqual(existingPost);
      });

      it('clears error', () => {
        error.set('Previous error');
        handleExtensionMessage({
          type: 'postCreated',
          data: { post: { id: 'post-1', content: 'Test', author: { name: 'User' } } }
        } as ExtensionMessage);
        expect(get(error)).toBeNull();
      });
    });

    describe('error message', () => {
      it('sets error from message.data.message', () => {
        handleExtensionMessage({
          type: 'error',
          data: { message: 'Something went wrong' }
        } as ExtensionMessage);
        expect(get(error)).toBe('Something went wrong');
      });

      it('uses default error when no message', () => {
        handleExtensionMessage({
          type: 'error',
          data: {}
        } as ExtensionMessage);
        expect(get(error)).toBe('Unknown error');
      });

      it('uses default error when data is null', () => {
        handleExtensionMessage({
          type: 'error',
          data: null
        } as ExtensionMessage);
        expect(get(error)).toBe('Unknown error');
      });
    });

    describe('loading message', () => {
      it('sets loading to true', () => {
        handleExtensionMessage({
          type: 'loading',
          data: { value: true }
        } as ExtensionMessage);
        expect(get(loading)).toBe(true);
      });

      it('sets loading to false', () => {
        loading.set(true);
        handleExtensionMessage({
          type: 'loading',
          data: { value: false }
        } as ExtensionMessage);
        expect(get(loading)).toBe(false);
      });

      it('defaults to false when no value', () => {
        loading.set(true);
        handleExtensionMessage({
          type: 'loading',
          data: {}
        } as ExtensionMessage);
        expect(get(loading)).toBe(false);
      });

      it('clears error when loading starts', () => {
        error.set('Previous error');
        handleExtensionMessage({
          type: 'loading',
          data: { value: true }
        } as ExtensionMessage);
        expect(get(error)).toBeNull();
      });

      it('does not clear error when loading stops', () => {
        error.set('Previous error');
        handleExtensionMessage({
          type: 'loading',
          data: { value: false }
        } as ExtensionMessage);
        expect(get(error)).toBe('Previous error');
      });
    });

    describe('repositoryInfo message', () => {
      it('sets repository info', () => {
        const repoInfo = {
          url: 'https://github.com/user/repo',
          name: 'repo',
          branch: 'main'
        };
        handleExtensionMessage({
          type: 'repositoryInfo',
          data: repoInfo
        } as ExtensionMessage);
        expect(get(repositoryInfo)).toEqual(repoInfo);
      });
    });

    describe('refresh message', () => {
      it('increments refresh trigger', () => {
        const initial = get(refreshTrigger);
        handleExtensionMessage({
          type: 'refresh'
        } as ExtensionMessage);
        expect(get(refreshTrigger)).toBe(initial + 1);
      });

      it('increments multiple times', () => {
        const initial = get(refreshTrigger);
        handleExtensionMessage({ type: 'refresh' } as ExtensionMessage);
        handleExtensionMessage({ type: 'refresh' } as ExtensionMessage);
        handleExtensionMessage({ type: 'refresh' } as ExtensionMessage);
        expect(get(refreshTrigger)).toBe(initial + 3);
      });
    });

    describe('refreshAfterFetch message', () => {
      it('sets skipCacheOnNextRefresh and increments refresh trigger', () => {
        const initial = get(refreshTrigger);
        handleExtensionMessage({
          type: 'refreshAfterFetch'
        } as ExtensionMessage);
        expect(get(skipCacheOnNextRefresh)).toBe(true);
        expect(get(refreshTrigger)).toBe(initial + 1);
      });
    });

    describe('settings message', () => {
      it('updates setting by key', () => {
        handleExtensionMessage({
          type: 'settings',
          data: { key: 'autoLoadImages', value: true }
        } as ExtensionMessage);
        expect(get(settings)).toEqual({ autoLoadImages: true });
      });

      it('merges with existing settings', () => {
        settings.set({ existingSetting: 'value' });
        handleExtensionMessage({
          type: 'settings',
          data: { key: 'autoLoadImages', value: true }
        } as ExtensionMessage);
        expect(get(settings)).toEqual({
          existingSetting: 'value',
          autoLoadImages: true
        });
      });

      it('updates existing setting', () => {
        settings.set({ autoLoadImages: false });
        handleExtensionMessage({
          type: 'settings',
          data: { key: 'autoLoadImages', value: true }
        } as ExtensionMessage);
        expect(get(settings)).toEqual({ autoLoadImages: true });
      });

      it('does nothing when key is missing', () => {
        settings.set({ existingSetting: 'value' });
        handleExtensionMessage({
          type: 'settings',
          data: { value: true }
        } as ExtensionMessage);
        expect(get(settings)).toEqual({ existingSetting: 'value' });
      });

      it('does nothing when data is null', () => {
        settings.set({ existingSetting: 'value' });
        handleExtensionMessage({
          type: 'settings',
          data: null
        } as ExtensionMessage);
        expect(get(settings)).toEqual({ existingSetting: 'value' });
      });
    });
  });

  describe('Store initialization', () => {
    it('posts starts as empty array', () => {
      expect(get(posts)).toEqual([]);
    });

    it('lists starts as empty array', () => {
      expect(get(lists)).toEqual([]);
    });

    it('loading starts as false', () => {
      expect(get(loading)).toBe(false);
    });

    it('error starts as null', () => {
      expect(get(error)).toBeNull();
    });

    it('refreshTrigger starts at 0', () => {
      expect(get(refreshTrigger)).toBe(0);
    });

    it('skipCacheOnNextRefresh starts as false', () => {
      expect(get(skipCacheOnNextRefresh)).toBe(false);
    });

    it('repositoryInfo starts as null', () => {
      expect(get(repositoryInfo)).toBeNull();
    });

    it('settings starts as empty object', () => {
      expect(get(settings)).toEqual({});
    });
  });
});
