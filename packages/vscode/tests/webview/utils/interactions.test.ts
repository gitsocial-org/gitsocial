/* eslint-disable @typescript-eslint/unbound-method */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createInteractionHandler } from '../../../src/webview/utils/interactions';
import type { Post } from '@gitsocial/core';
import { get } from 'svelte/store';

// Mock dependencies
vi.mock('../../../src/webview/api', () => ({
  api: {
    createInteraction: vi.fn()
  }
}));

vi.mock('../../../src/webview/weblog', () => ({
  webLog: vi.fn()
}));

import { api } from '../../../src/webview/api';

describe('interactions utilities', () => {
  let handler: ReturnType<typeof createInteractionHandler>;
  let cleanup: (() => void) | undefined;

  const mockPost: Post = {
    id: 'test-post-id',
    type: 'post',
    author: 'testuser',
    authorRepository: 'https://github.com/user/repo',
    content: 'Test post content',
    timestamp: new Date().toISOString()
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    handler = createInteractionHandler();
  });

  afterEach(() => {
    if (cleanup) {
      cleanup();
      cleanup = undefined;
    }
    vi.useRealTimers();
  });

  describe('createInteractionHandler', () => {
    it('should create handler with initial state', () => {
      const state = get(handler.state);
      expect(state).toEqual({
        selectedPost: null,
        interactionType: null,
        isSubmitting: false,
        submissionMessage: '',
        submissionSuccess: false
      });
    });

    it('should return all handler functions', () => {
      expect(handler.handleInteraction).toBeDefined();
      expect(handler.handleInteractionSubmit).toBeDefined();
      expect(handler.handleInteractionCancel).toBeDefined();
      expect(handler.setupMessageListeners).toBeDefined();
      expect(typeof handler.handleInteraction).toBe('function');
      expect(typeof handler.handleInteractionSubmit).toBe('function');
      expect(typeof handler.handleInteractionCancel).toBe('function');
      expect(typeof handler.setupMessageListeners).toBe('function');
    });
  });

  describe('handleInteraction', () => {
    it('should update state with comment interaction', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });

      handler.handleInteraction(event);

      const state = get(handler.state);
      expect(state.selectedPost).toEqual(mockPost);
      expect(state.interactionType).toBe('comment');
      expect(state.submissionMessage).toBe('');
      expect(state.submissionSuccess).toBe(false);
    });

    it('should update state with repost interaction', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'repost' as const }
      });

      handler.handleInteraction(event);

      const state = get(handler.state);
      expect(state.selectedPost).toEqual(mockPost);
      expect(state.interactionType).toBe('repost');
    });

    it('should update state with quote interaction', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'quote' as const }
      });

      handler.handleInteraction(event);

      const state = get(handler.state);
      expect(state.selectedPost).toEqual(mockPost);
      expect(state.interactionType).toBe('quote');
    });

    it('should reset submission messages when new interaction starts', () => {
      handler.state.update(s => ({
        ...s,
        submissionMessage: 'Previous message',
        submissionSuccess: true
      }));

      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });

      handler.handleInteraction(event);

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('');
      expect(state.submissionSuccess).toBe(false);
    });
  });

  describe('handleInteractionSubmit', () => {
    beforeEach(() => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });
      handler.handleInteraction(event);
    });

    it('should not submit if no post selected', () => {
      handler.handleInteractionCancel();

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test comment' }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).not.toHaveBeenCalled();
    });

    it('should not submit if no interaction type selected', () => {
      handler.state.update(s => ({ ...s, interactionType: null }));

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test comment' }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).not.toHaveBeenCalled();
    });

    it('should create comment interaction', () => {
      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test comment' }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).toHaveBeenCalledWith('comment', mockPost, 'Test comment');

      const state = get(handler.state);
      expect(state.isSubmitting).toBe(true);
      expect(state.submissionMessage).toBe('Creating comment...');
      expect(state.submissionSuccess).toBe(false);
    });

    it('should create repost interaction without text', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'repost' as const }
      });
      handler.handleInteraction(event);

      const submitEvent = new CustomEvent('submit', {
        detail: { text: '', isQuoteRepost: false }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).toHaveBeenCalledWith('repost', mockPost);

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Creating repost...');
    });

    it('should create quote when repost has text', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'repost' as const }
      });
      handler.handleInteraction(event);

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'My thoughts', isQuoteRepost: true }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).toHaveBeenCalledWith('quote', mockPost, 'My thoughts');

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Creating quote...');
    });

    it('should create quote interaction', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'quote' as const }
      });
      handler.handleInteraction(event);

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'My quote text' }
      });

      handler.handleInteractionSubmit(submitEvent);

      expect(vi.mocked(api.createInteraction)).toHaveBeenCalledWith('quote', mockPost, 'My quote text');

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Creating quote...');
    });

    it('should handle API errors with Error object', () => {
      vi.mocked(api.createInteraction).mockImplementation(() => {
        throw new Error('Network error');
      });

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test comment' }
      });

      handler.handleInteractionSubmit(submitEvent);

      const state = get(handler.state);
      expect(state.submissionMessage).toContain('Failed to create comment');
      expect(state.submissionMessage).toContain('Network error');
      expect(state.submissionSuccess).toBe(false);
      expect(state.isSubmitting).toBe(false);
    });

    it('should handle API errors with non-Error object', () => {
      vi.mocked(api.createInteraction).mockImplementation(() => {
        // eslint-disable-next-line no-throw-literal
        throw 'String error';
      });

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test comment' }
      });

      handler.handleInteractionSubmit(submitEvent);

      const state = get(handler.state);
      expect(state.submissionMessage).toContain('Failed to create comment');
      expect(state.submissionMessage).toContain('Unknown error');
      expect(state.submissionSuccess).toBe(false);
      expect(state.isSubmitting).toBe(false);
    });

    it('should handle error when interactionType becomes null during submission', () => {
      vi.mocked(api.createInteraction).mockImplementation(() => {
        // Simulate interactionType becoming null during API call
        handler.state.update(s => ({ ...s, interactionType: null }));
        throw new Error('Test error');
      });

      const submitEvent = new CustomEvent('submit', {
        detail: { text: 'Test' }
      });

      handler.handleInteractionSubmit(submitEvent);

      const state = get(handler.state);
      expect(state.submissionMessage).toContain('Failed to create interaction');
      expect(state.submissionMessage).toContain('Test error');
    });
  });

  describe('handleInteractionCancel', () => {
    it('should reset state to initial values', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });
      handler.handleInteraction(event);

      handler.state.update(s => ({
        ...s,
        isSubmitting: true,
        submissionMessage: 'Test message',
        submissionSuccess: true
      }));

      handler.handleInteractionCancel();

      const state = get(handler.state);
      expect(state).toEqual({
        selectedPost: null,
        interactionType: null,
        isSubmitting: false,
        submissionMessage: '',
        submissionSuccess: false
      });
    });
  });

  describe('setupMessageListeners', () => {
    beforeEach(() => {
      cleanup = handler.setupMessageListeners();
    });

    it('should return cleanup function', () => {
      expect(cleanup).toBeDefined();
      expect(typeof cleanup).toBe('function');
    });

    it('should handle interactionCreated message for comment', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });
      handler.handleInteraction(event);

      handler.state.update(s => ({ ...s, isSubmitting: true }));

      const createdPost: Post = {
        ...mockPost,
        id: 'created-comment-id',
        type: 'comment'
      };

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interactionType: 'comment',
            interaction: createdPost
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Comment created successfully!');
      expect(state.submissionSuccess).toBe(true);
      expect(state.isSubmitting).toBe(false);
    });

    it('should handle interactionCreated message for repost', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'repost' as const }
      });
      handler.handleInteraction(event);

      handler.state.update(s => ({ ...s, isSubmitting: true }));

      const createdPost: Post = {
        ...mockPost,
        id: 'created-repost-id',
        type: 'repost'
      };

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interactionType: 'repost',
            interaction: createdPost
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Repost created successfully!');
      expect(state.submissionSuccess).toBe(true);
    });

    it('should handle interactionCreated message for quote', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'quote' as const }
      });
      handler.handleInteraction(event);

      handler.state.update(s => ({ ...s, isSubmitting: true }));

      const createdPost: Post = {
        ...mockPost,
        id: 'created-quote-id',
        type: 'quote'
      };

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interactionType: 'quote',
            interaction: createdPost
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Quote created successfully!');
      expect(state.submissionSuccess).toBe(true);
    });

    it('should close dialog after success with timeout', () => {
      const event = new CustomEvent('interact', {
        detail: { post: mockPost, type: 'comment' as const }
      });
      handler.handleInteraction(event);

      handler.state.update(s => ({ ...s, isSubmitting: true }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interactionType: 'comment',
            interaction: mockPost
          }
        }
      }));

      let state = get(handler.state);
      expect(state.submissionSuccess).toBe(true);

      vi.advanceTimersByTime(1500);

      state = get(handler.state);
      expect(state.selectedPost).toBe(null);
      expect(state.interactionType).toBe(null);
    });

    it('should not handle interactionCreated if not submitting', () => {
      handler.state.update(s => ({ ...s, isSubmitting: false }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interactionType: 'comment',
            interaction: mockPost
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('');
      expect(state.submissionSuccess).toBe(false);
    });

    it('should not handle interactionCreated if no interactionType in data', () => {
      handler.state.update(s => ({ ...s, isSubmitting: true }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'interactionCreated',
          data: {
            interaction: mockPost
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('');
    });

    it('should handle error message when submitting', () => {
      handler.state.update(s => ({ ...s, isSubmitting: true }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'error',
          data: {
            message: 'Failed to create interaction'
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Failed to create interaction');
      expect(state.submissionSuccess).toBe(false);
      expect(state.isSubmitting).toBe(false);
    });

    it('should handle error message without data.message', () => {
      handler.state.update(s => ({ ...s, isSubmitting: true }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'error',
          data: {}
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('Failed to create interaction');
      expect(state.submissionSuccess).toBe(false);
      expect(state.isSubmitting).toBe(false);
    });

    it('should not handle error message if not submitting', () => {
      handler.state.update(s => ({ ...s, isSubmitting: false }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'error',
          data: {
            message: 'Some error'
          }
        }
      }));

      const state = get(handler.state);
      expect(state.submissionMessage).toBe('');
    });

    it('should ignore unknown message types', () => {
      handler.state.update(s => ({ ...s, isSubmitting: true }));

      window.dispatchEvent(new MessageEvent('message', {
        data: {
          type: 'unknownType',
          data: {}
        }
      }));

      const state = get(handler.state);
      expect(state.isSubmitting).toBe(true);
    });

    it('should remove event listener when cleanup is called', () => {
      const addEventListenerSpy = vi.spyOn(window, 'addEventListener');
      const removeEventListenerSpy = vi.spyOn(window, 'removeEventListener');

      const newCleanup = handler.setupMessageListeners();

      expect(addEventListenerSpy).toHaveBeenCalledWith('message', expect.any(Function));

      newCleanup();

      expect(removeEventListenerSpy).toHaveBeenCalledWith('message', expect.any(Function));
    });
  });
});
