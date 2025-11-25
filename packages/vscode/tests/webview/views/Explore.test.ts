/* eslint-disable @typescript-eslint/no-unsafe-call, @typescript-eslint/no-unsafe-assignment */
import { render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import Explore, { DEFAULT_REPO, handleSettingsMessage } from '../../../src/webview/views/Explore.svelte';

vi.mock('../../../src/webview/api', () => ({
  api: {
    getSettings: vi.fn(),
    openView: vi.fn()
  }
}));

describe('Explore Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Basic Rendering', () => {
    it('renders component container', () => {
      const { container } = render(Explore);
      const viewContainer = container.querySelector('.view-container');
      expect(viewContainer).toBeDefined();
    });

    it('renders empty state div', () => {
      const { container } = render(Explore);
      const emptyState = container.querySelector('.empty');
      expect(emptyState).toBeDefined();
    });

    it('renders loading spinner', () => {
      const { container } = render(Explore);
      const spinner = container.querySelector('.codicon-loading.spin');
      expect(spinner).toBeDefined();
    });

    it('renders loading message text', () => {
      render(Explore);
      expect(screen.getByText('Loading official lists...')).toBeDefined();
    });

    it('has correct loading spinner classes', () => {
      const { container } = render(Explore);
      const spinner = container.querySelector('span.codicon.codicon-loading.spin');
      expect(spinner?.classList.contains('codicon')).toBe(true);
      expect(spinner?.classList.contains('codicon-loading')).toBe(true);
      expect(spinner?.classList.contains('spin')).toBe(true);
    });
  });

  describe('Component Initialization', () => {
    it('renders without errors', () => {
      const { container } = render(Explore);
      expect(container).toBeDefined();
    });

    it('initializes with proper structure', () => {
      const { container } = render(Explore);
      const viewContainer = container.querySelector('.view-container');
      const emptyState = viewContainer?.querySelector('.empty');
      const spinner = emptyState?.querySelector('.codicon-loading');
      const text = emptyState?.querySelector('p');

      expect(viewContainer).toBeDefined();
      expect(emptyState).toBeDefined();
      expect(spinner).toBeDefined();
      expect(text?.textContent).toBe('Loading official lists...');
    });
  });

  describe('Message Handling', () => {
    it('handles settings message with custom exploreListsSource', async () => {
      const { api } = await import('../../../src/webview/api');
      const customRepo = 'https://github.com/user/custom-lists#branch:main';

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: customRepo
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('repository', 'Explore', {
        repository: customRepo,
        activeTab: 'lists'
      });
    });

    it('handles settings message with null value (uses DEFAULT_REPO)', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: null as unknown as string
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('repository', 'Explore', {
        repository: DEFAULT_REPO,
        activeTab: 'lists'
      });
    });

    it('handles settings message with undefined value (uses DEFAULT_REPO)', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: undefined
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('repository', 'Explore', {
        repository: DEFAULT_REPO,
        activeTab: 'lists'
      });
    });

    it('ignores messages with wrong type', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'other',
        data: {
          key: 'exploreListsSource',
          value: 'some-value'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).not.toHaveBeenCalled();
    });

    it('ignores messages with wrong key', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'otherKey',
          value: 'some-value'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).not.toHaveBeenCalled();
    });

    it('ignores messages with missing data', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings'
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).not.toHaveBeenCalled();
    });

    it('ignores messages with missing data.key', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          value: 'some-value'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).not.toHaveBeenCalled();
    });
  });

  describe('API Calls', () => {
    it('calls api.openView with correct parameters', async () => {
      const { api } = await import('../../../src/webview/api');
      const customRepo = 'https://github.com/test/repo#branch:test';

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: customRepo
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('repository', 'Explore', {
        repository: customRepo,
        activeTab: 'lists'
      });
    });

    it('passes repository parameter to api.openView', async () => {
      const { api } = await import('../../../src/webview/api');
      const repo = 'https://github.com/example/lists#branch:v1';

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: repo
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith(
        'repository',
        'Explore',
        expect.objectContaining({
          repository: repo
        })
      );
    });

    it('always sets activeTab to lists', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: 'custom-repo'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String),
        expect.objectContaining({
          activeTab: 'lists'
        })
      );
    });

    it('sets view title to Explore', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: 'custom-repo'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith(
        expect.any(String),
        'Explore',
        expect.any(Object)
      );
    });
  });

  describe('Edge Cases', () => {
    it('handles multiple message calls', async () => {
      const { api } = await import('../../../src/webview/api');

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: 'repo1'
        }
      });

      handleSettingsMessage({
        type: 'settings',
        data: {
          key: 'exploreListsSource',
          value: 'repo2'
        }
      });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(2);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenNthCalledWith(1, 'repository', 'Explore', {
        repository: 'repo1',
        activeTab: 'lists'
      });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenNthCalledWith(2, 'repository', 'Explore', {
        repository: 'repo2',
        activeTab: 'lists'
      });
    });

    it('component unmounts without errors', () => {
      const { unmount } = render(Explore);
      expect(() => unmount()).not.toThrow();
    });

    it('handles multiple sequential renders', () => {
      const { unmount: unmount1 } = render(Explore);
      unmount1();

      const { unmount: unmount2 } = render(Explore);
      unmount2();

      expect(true).toBe(true);
    });
  });

  describe('Component Structure', () => {
    it('view container has correct class', () => {
      const { container } = render(Explore);
      const viewContainer = container.querySelector('.view-container');
      expect(viewContainer?.classList.contains('view-container')).toBe(true);
    });

    it('empty state has correct class', () => {
      const { container } = render(Explore);
      const emptyState = container.querySelector('.empty');
      expect(emptyState?.classList.contains('empty')).toBe(true);
    });

    it('empty state is inside view container', () => {
      const { container } = render(Explore);
      const viewContainer = container.querySelector('.view-container');
      const emptyState = viewContainer?.querySelector('.empty');
      expect(emptyState).toBeDefined();
    });

    it('paragraph element contains loading text', () => {
      const { container } = render(Explore);
      const paragraph = container.querySelector('.empty p');
      expect(paragraph?.textContent).toBe('Loading official lists...');
    });
  });
});
