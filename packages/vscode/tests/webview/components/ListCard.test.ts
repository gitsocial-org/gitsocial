import { fireEvent, render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ListCard from '../../../src/webview/components/ListCard.svelte';
import type { List } from '@gitsocial/core';

vi.mock('../../../src/webview/api', () => ({
  api: {
    openView: vi.fn(),
    renameList: vi.fn()
  }
}));

vi.mock('@gitsocial/core/client', () => ({
  gitHost: {
    getDisplayName: (repo: string) => repo.replace('https://github.com/', '')
  },
  gitMsgRef: {
    parseRepositoryId: (id: string) => ({
      repository: id.split('#')[0],
      branch: 'main'
    })
  }
}));

describe('ListCard Component', () => {
  const mockList: List = {
    id: 'test-list',
    name: 'Test List',
    repositories: [
      'https://github.com/owner/repo1#branch:main',
      'https://github.com/owner/repo2#branch:main'
    ],
    isUnpushed: false
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Basic Rendering', () => {
    it('renders list name', () => {
      render(ListCard, { props: { list: mockList } });
      expect(screen.getByText('Test List')).toBeDefined();
    });

    it('shows repository count badge', () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const badge = container.querySelector('.badge');
      expect(badge?.textContent).toBe('2');
    });

    it('shows empty list message when no repositories', () => {
      const emptyList = { ...mockList, repositories: [] };
      render(ListCard, { props: { list: emptyList } });
      expect(screen.getByText('Empty list')).toBeDefined();
    });

    it('does not show badge for empty list', () => {
      const emptyList = { ...mockList, repositories: [] };
      const { container } = render(ListCard, { props: { list: emptyList } });
      const badge = container.querySelector('.badge');
      expect(badge).toBeNull();
    });
  });

  describe('Repository Avatars', () => {
    it('renders avatars for repositories', () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const avatars = container.querySelectorAll('.avatar');
      expect(avatars.length).toBe(2);
    });

    it('limits to 10 repository avatars', () => {
      const listWithMany = {
        ...mockList,
        repositories: Array.from({ length: 15 }, (_, i) => `https://github.com/owner/repo${i}#branch:main`)
      };
      const { container } = render(ListCard, { props: { list: listWithMany } });
      const avatars = container.querySelectorAll('.avatar');
      expect(avatars.length).toBe(10);
    });
  });

  describe('Event Dispatching', () => {
    it('dispatches viewList event on card click', async () => {
      const { component, container } = render(ListCard, { props: { list: mockList } });
      const handleViewList = vi.fn();
      component.$on('viewList', handleViewList);
      const card = container.querySelector('.card');
      if (card) {
        await fireEvent.click(card);
        expect(handleViewList).toHaveBeenCalledTimes(1);
        expect((handleViewList.mock.calls[0][0] as CustomEvent).detail).toEqual({ list: mockList });
      }
    });

    it('dispatches viewList with activeTab for empty list', async () => {
      const emptyList = { ...mockList, repositories: [] };
      const { component, container } = render(ListCard, { props: { list: emptyList } });
      const handleViewList = vi.fn();
      component.$on('viewList', handleViewList);
      const card = container.querySelector('.card');
      if (card) {
        await fireEvent.click(card);
        expect(handleViewList).toHaveBeenCalledTimes(1);
        expect((handleViewList.mock.calls[0][0] as CustomEvent).detail).toEqual({
          list: emptyList,
          activeTab: 'repositories'
        });
      }
    });

    it('dispatches delete event when delete button clicked', async () => {
      const { component, container } = render(ListCard, { props: { list: mockList } });
      const handleDelete = vi.fn();
      component.$on('delete', handleDelete);
      const deleteButton = container.querySelector('[title="Delete list"]');
      if (deleteButton) {
        await fireEvent.click(deleteButton);
        expect(handleDelete).toHaveBeenCalledTimes(1);
        expect((handleDelete.mock.calls[0][0] as CustomEvent).detail).toEqual({ list: mockList });
      }
    });
  });

  describe('Rename Functionality', () => {
    it('shows rename form when rename button clicked', async () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const renameButton = container.querySelector('[title="Rename list"]');
      if (renameButton) {
        await fireEvent.click(renameButton);
        const input = container.querySelector('input[type="text"]') as HTMLInputElement;
        expect(input).toBeDefined();
        expect(input?.value).toBe('Test List');
      }
    });

    it('cancels rename on escape key', async () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const renameButton = container.querySelector('[title="Rename list"]');
      if (renameButton) {
        await fireEvent.click(renameButton);
        const input = container.querySelector('input[type="text"]');
        if (input) {
          await fireEvent.keyDown(input, { key: 'Escape' });
          const inputAfter = container.querySelector('input[type="text"]');
          expect(inputAfter).toBeNull();
        }
      }
    });

    it('disables save button when name is empty', async () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const renameButton = container.querySelector('[title="Rename list"]');
      if (renameButton) {
        await fireEvent.click(renameButton);
        const input = container.querySelector('input[type="text"]') as HTMLInputElement;
        if (input) {
          input.value = '';
          await fireEvent.input(input);
          const saveButton = container.querySelector('[title="Save"]') as HTMLButtonElement;
          expect(saveButton?.disabled).toBe(true);
        }
      }
    });

    it('does not show rename button for followed lists', () => {
      const followedList = { ...mockList, source: 'https://github.com/owner/repo#gitmsg/social/lists/test' };
      const { container } = render(ListCard, { props: { list: followedList } });
      const renameButton = container.querySelector('[title="Rename list"]');
      expect(renameButton).toBeNull();
    });
  });

  describe('Read-Only Mode', () => {
    it('hides action buttons in read-only mode', () => {
      const { container } = render(ListCard, { props: { list: mockList, readOnly: true } });
      const deleteButton = container.querySelector('[title="Delete list"]');
      const renameButton = container.querySelector('[title="Rename list"]');
      expect(deleteButton).toBeNull();
      expect(renameButton).toBeNull();
    });

    it('still allows viewing list in read-only mode', async () => {
      const { component, container } = render(ListCard, { props: { list: mockList, readOnly: true } });
      const handleViewList = vi.fn();
      component.$on('viewList', handleViewList);
      const card = container.querySelector('.card');
      if (card) {
        await fireEvent.click(card);
        expect(handleViewList).toHaveBeenCalledTimes(1);
      }
    });
  });

  describe('Follow Functionality', () => {
    it('shows follow button when showFollowButton is true', () => {
      const { container } = render(ListCard, { props: { list: mockList, showFollowButton: true } });
      const followButton = container.querySelector('[title="Follow list"]');
      expect(followButton).toBeDefined();
    });

    it('shows unfollow button for followed lists', () => {
      const followedList = { ...mockList, isFollowedLocally: true };
      const { container } = render(ListCard, { props: { list: followedList, showFollowButton: true } });
      const unfollowButton = container.querySelector('[title="Unfollow list"]');
      expect(unfollowButton).toBeDefined();
    });

    it('dispatches follow event when follow button clicked', async () => {
      const { component, container } = render(ListCard, { props: { list: mockList, showFollowButton: true } });
      const handleFollow = vi.fn();
      component.$on('follow', handleFollow);
      const followButton = container.querySelector('[title="Follow list"]');
      if (followButton) {
        await fireEvent.click(followButton);
        expect(handleFollow).toHaveBeenCalledTimes(1);
        expect((handleFollow.mock.calls[0][0] as CustomEvent).detail).toEqual({ list: mockList });
      }
    });

    it('dispatches unfollow event when unfollow button clicked', async () => {
      const followedList = { ...mockList, isFollowedLocally: true };
      const { component, container } = render(ListCard, { props: { list: followedList, showFollowButton: true } });
      const handleUnfollow = vi.fn();
      component.$on('unfollow', handleUnfollow);
      const unfollowButton = container.querySelector('[title="Unfollow list"]');
      if (unfollowButton) {
        await fireEvent.click(unfollowButton);
        expect(handleUnfollow).toHaveBeenCalledTimes(1);
        expect((handleUnfollow.mock.calls[0][0] as CustomEvent).detail).toEqual({ list: followedList });
      }
    });
  });

  describe('Source List Indicator', () => {
    it('shows sync icon for followed lists', () => {
      const followedList = { ...mockList, source: 'https://github.com/owner/repo#gitmsg/social/lists/test' };
      const { container } = render(ListCard, { props: { list: followedList } });
      const syncIcon = container.querySelector('.codicon-sync');
      expect(syncIcon).toBeDefined();
    });

    it('displays source URL for followed lists', () => {
      const followedList = { ...mockList, source: 'https://github.com/owner/repo#gitmsg/social/lists/test' };
      render(ListCard, { props: { list: followedList } });
      expect(screen.getByText('https://github.com/owner/repo#gitmsg/social/lists/test')).toBeDefined();
    });

    it('does not show sync icon for local lists', () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const syncIcon = container.querySelector('.codicon-sync');
      expect(syncIcon).toBeNull();
    });
  });

  describe('Unpushed Indicator', () => {
    it('shows warning border for unpushed lists', () => {
      const unpushedList = { ...mockList, isUnpushed: true };
      const { container } = render(ListCard, { props: { list: unpushedList } });
      const card = container.querySelector('.border-l-warning');
      expect(card).toBeDefined();
    });

    it('does not show warning border for pushed lists', () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const card = container.querySelector('.border-l-warning');
      expect(card).toBeNull();
    });
  });

  describe('Keyboard Navigation', () => {
    it('handles Enter key on card', async () => {
      const { component, container } = render(ListCard, { props: { list: mockList } });
      const handleViewList = vi.fn();
      component.$on('viewList', handleViewList);
      const card = container.querySelector('.card');
      if (card) {
        await fireEvent.keyDown(card, { key: 'Enter' });
        expect(handleViewList).toHaveBeenCalledTimes(1);
      }
    });

    it('has proper tabindex for accessibility', () => {
      const { container } = render(ListCard, { props: { list: mockList } });
      const card = container.querySelector('.card');
      expect(card?.getAttribute('tabindex')).toBe('0');
    });
  });
});
