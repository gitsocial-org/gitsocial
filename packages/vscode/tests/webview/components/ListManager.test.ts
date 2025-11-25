import { render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ListManager from '../../../src/webview/components/ListManager.svelte';
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

describe('ListManager Component', () => {
  const mockLists: List[] = [
    {
      id: 'list1',
      name: 'First List',
      repositories: ['https://github.com/owner/repo1#branch:main'],
      isUnpushed: false
    },
    {
      id: 'list2',
      name: 'Second List',
      repositories: [],
      isUnpushed: false
    }
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    global.alert = vi.fn();
  });

  describe('Basic Rendering', () => {
    it('renders list create form when not read-only', () => {
      render(ListManager, { props: { lists: [] } });
      const nameInput = screen.getByLabelText('Name');
      expect(nameInput).toBeDefined();
    });

    it('does not render list create form in read-only mode', () => {
      render(ListManager, { props: { lists: [], readOnly: true } });
      const nameInput = screen.queryByLabelText('Name');
      expect(nameInput).toBeNull();
    });

    it('renders all provided lists', () => {
      render(ListManager, { props: { lists: mockLists } });
      expect(screen.getByText('First List')).toBeDefined();
      expect(screen.getByText('Second List')).toBeDefined();
    });

    it('shows empty state when no lists', () => {
      render(ListManager, { props: { lists: [] } });
      expect(screen.getByText(/No lists created yet/)).toBeDefined();
    });

    it('shows read-only empty state', () => {
      render(ListManager, { props: { lists: [], readOnly: true } });
      expect(screen.getByText(/No lists found in this repository/)).toBeDefined();
    });
  });

  describe('List Display', () => {
    it('displays lists using ListCard component', () => {
      const { container } = render(ListManager, { props: { lists: mockLists } });
      const listCards = container.querySelectorAll('.card');
      expect(listCards.length).toBe(2);
    });

    it('shows repository count in list cards', () => {
      const { container } = render(ListManager, { props: { lists: mockLists } });
      const badges = container.querySelectorAll('.badge');
      expect(badges.length).toBeGreaterThan(0);
    });

    it('shows empty list message for empty lists', () => {
      render(ListManager, { props: { lists: mockLists } });
      expect(screen.getByText('Empty list')).toBeDefined();
    });
  });

  describe('Read-Only Mode', () => {
    it('passes readOnly prop to ListCard components', () => {
      const { container } = render(ListManager, { props: { lists: mockLists, readOnly: true } });
      const deleteButtons = container.querySelectorAll('[title="Delete list"]');
      expect(deleteButtons.length).toBe(0);
    });

    it('shows follow buttons when repository is provided in read-only mode', () => {
      const { container } = render(ListManager, {
        props: {
          lists: mockLists,
          readOnly: true,
          repository: 'https://github.com/owner/repo'
        }
      });
      const followButtons = container.querySelectorAll('[title="Follow list"]');
      expect(followButtons.length).toBeGreaterThan(0);
    });

    it('does not show follow buttons without repository in read-only mode', () => {
      const { container } = render(ListManager, {
        props: {
          lists: mockLists,
          readOnly: true
        }
      });
      const followButtons = container.querySelectorAll('[title="Follow list"]');
      expect(followButtons.length).toBe(0);
    });
  });
});
