import type { Post } from '@gitsocial/core';
import { writable } from 'svelte/store';
import { api } from '../api';
import { webLog } from './weblog';

export interface InteractionState {
  selectedPost: Post | null;
  interactionType: 'comment' | 'repost' | 'quote' | null;
  isSubmitting: boolean;
  submissionMessage: string;
  submissionSuccess: boolean;
}

export function createInteractionHandler(): {
  state: typeof state;
  handleInteraction: (event: CustomEvent<{ post: Post; type: 'comment' | 'repost' | 'quote' }>) => void;
  handleInteractionSubmit: (event: CustomEvent<{ text: string; isQuoteRepost?: boolean }>) => void;
  handleInteractionCancel: () => void;
  setupMessageListeners: () => (() => void);
  } {
  // Create reactive state
  const state = writable<InteractionState>({
    selectedPost: null,
    interactionType: null,
    isSubmitting: false,
    submissionMessage: '',
    submissionSuccess: false
  });

  let currentState: InteractionState;
  state.subscribe(value => {
    currentState = value;
  });

  // Event handler
  function handleInteraction(event: CustomEvent<{ post: Post; type: 'comment' | 'repost' | 'quote' }>): void {
    state.update(s => ({
      ...s,
      selectedPost: event.detail.post,
      interactionType: event.detail.type,
      submissionMessage: '',
      submissionSuccess: false
    }));
  }

  function handleInteractionSubmit(event: CustomEvent<{ text: string; isQuoteRepost?: boolean }>): void {
    if (!currentState.selectedPost || !currentState.interactionType) {return;}

    // Determine the actual interaction type being created
    let actualInteractionType: 'comment' | 'repost' | 'quote';
    if (currentState.interactionType === 'comment') {
      actualInteractionType = 'comment';
    } else if (currentState.interactionType === 'quote' || (currentState.interactionType === 'repost' && event.detail.isQuoteRepost)) {
      actualInteractionType = 'quote';
    } else {
      actualInteractionType = 'repost';
    }

    webLog('info', 'Submitting interaction:', {
      dialogType: currentState.interactionType,
      actualType: actualInteractionType,
      postId: currentState.selectedPost.id,
      hasText: !!event.detail.text
    });

    state.update(s => ({
      ...s,
      isSubmitting: true,
      submissionMessage: `Creating ${actualInteractionType}...`,
      submissionSuccess: false
    }));

    try {
      if (actualInteractionType === 'comment') {
        api.createInteraction('comment', currentState.selectedPost, event.detail.text);
      } else if (actualInteractionType === 'quote') {
        api.createInteraction('quote', currentState.selectedPost, event.detail.text);
      } else {
        api.createInteraction('repost', currentState.selectedPost);
      }

      webLog('debug', 'Interaction API call sent, waiting for response');
      // Success state will be handled by message listeners
    } catch (error) {
      webLog('error', 'Failed to submit interaction:', error);
      state.update(s => ({
        ...s,
        submissionMessage: `Failed to create ${currentState.interactionType ?? 'interaction'}: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        submissionSuccess: false,
        isSubmitting: false
      }));
    }
  }

  function handleInteractionCancel(): void {
    state.update(s => ({
      ...s,
      selectedPost: null,
      interactionType: null,
      isSubmitting: false,
      submissionMessage: '',
      submissionSuccess: false
    }));
  }

  // Message listeners for creation success
  function setupMessageListeners(): (() => void) {
    const handleMessage = (event: MessageEvent): void => {
      const message = event.data as {
        type: string;
        data?: {
          interactionType?: string;
          interaction?: Post;
          message?: string;
        };
      };

      switch (message.type) {
      case 'interactionCreated':
        webLog('info', 'Received interactionCreated message:', message.data);
        // Accept the success message regardless of dialog type vs actual type mismatch
        // (e.g., repost dialog that becomes quote due to added text)
        if (message.data?.interactionType && currentState.isSubmitting) {
          const interactionType = message.data.interactionType;
          const typeName = interactionType.charAt(0).toUpperCase() + interactionType.slice(1);
          handleCreationSuccess(typeName, message.data?.interaction as Post);
        }
        break;
      case 'error':
        webLog('error', 'Received error message:', message.data);
        if (currentState.isSubmitting) {
          state.update(s => ({
            ...s,
            submissionMessage: message.data?.message || 'Failed to create interaction',
            submissionSuccess: false,
            isSubmitting: false
          }));
        }
        break;
      }
    };

    window.addEventListener('message', handleMessage);

    // Return cleanup function
    return () => {
      window.removeEventListener('message', handleMessage);
    };
  }

  function handleCreationSuccess(type: string, _createdPost: Post): void {
    const successMessage = `${type} created successfully!`;

    state.update(s => ({
      ...s,
      submissionMessage: successMessage,
      submissionSuccess: true,
      isSubmitting: false
    }));

    // Close dialog after showing success message
    setTimeout(() => {
      handleInteractionCancel();
    }, 1500);
  }

  return {
    state,
    handleInteraction,
    handleInteractionSubmit,
    handleInteractionCancel,
    setupMessageListeners
  };
}
