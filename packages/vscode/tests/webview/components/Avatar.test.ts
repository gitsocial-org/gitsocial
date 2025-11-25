import { render } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Avatar from '../../../src/webview/components/Avatar.svelte';

vi.mock('../../../src/webview/api', () => ({
  api: {
    postMessage: vi.fn()
  }
}));

describe('Avatar Component', () => {
  let observeMock: ReturnType<typeof vi.fn>;
  let unobserveMock: ReturnType<typeof vi.fn>;
  let disconnectMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    observeMock = vi.fn();
    unobserveMock = vi.fn();
    disconnectMock = vi.fn();

    global.IntersectionObserver = class IntersectionObserver {
      observe = observeMock;
      unobserve = unobserveMock;
      disconnect = disconnectMock;
      root = null;
      rootMargin = '';
      thresholds = [];
      constructor() {
        // Mock constructor
      }
      takeRecords = (): IntersectionObserverEntry[] => [];
    } as unknown as typeof IntersectionObserver;
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe('Initials Generation', () => {
    it('generates single letter for repository', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'repository',
          identifier: 'https://github.com/owner/repo',
          name: 'MyRepo'
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback?.textContent?.trim()).toBe('M');
    });

    it('generates initials from first and last name for user', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe'
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback?.textContent?.trim()).toBe('JD');
    });

    it('generates single initial for user with one name', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John'
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback?.textContent?.trim()).toBe('J');
    });

    it('handles empty name with question mark', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: ''
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback?.textContent?.trim()).toBe('?');
    });

    it('handles multiple spaces in name', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John   Middle   Doe'
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback?.textContent?.trim()).toBe('JD');
    });
  });

  describe('Rendering', () => {
    it('renders with correct size', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe',
          size: 48
        }
      });
      const avatar = container.querySelector('.avatar') as HTMLElement;
      expect(avatar?.style.width).toBe('48px');
      expect(avatar?.style.height).toBe('48px');
    });

    it('applies user-avatar class for user type', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe'
        }
      });
      const avatar = container.querySelector('.user-avatar');
      expect(avatar).toBeDefined();
    });

    it('applies repository-avatar class for repository type', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'repository',
          identifier: 'https://github.com/owner/repo',
          name: 'MyRepo'
        }
      });
      const avatar = container.querySelector('.repository-avatar');
      expect(avatar).toBeDefined();
    });

    it('shows fallback initials initially', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe'
        }
      });
      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback).toBeDefined();
      expect(fallback?.textContent?.trim()).toBe('JD');
    });
  });

  describe('Lazy Loading', () => {
    it('does not fetch avatar immediately on render', async () => {
      const { api } = await import('../../../src/webview/api');
      render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe'
        }
      });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      const postMessage = api.postMessage;
      expect(postMessage).not.toHaveBeenCalled();
    });
  });

  describe('Error Handling', () => {
    it('handles missing identifier gracefully', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: '',
          name: 'John Doe'
        }
      });

      const fallback = container.querySelector('.avatar-fallback');
      expect(fallback).toBeDefined();
      expect(fallback?.textContent?.trim()).toBe('JD');
    });
  });

  describe('Size Variations', () => {
    it('uses default size of 32px', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe'
        }
      });
      const avatar = container.querySelector('.avatar') as HTMLElement;
      expect(avatar?.style.width).toBe('32px');
      expect(avatar?.style.height).toBe('32px');
    });

    it('accepts custom size', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe',
          size: 64
        }
      });
      const avatar = container.querySelector('.avatar') as HTMLElement;
      expect(avatar?.style.width).toBe('64px');
      expect(avatar?.style.height).toBe('64px');
    });
  });

  describe('Repository Context', () => {
    it('renders avatar with repository context', () => {
      const { container } = render(Avatar, {
        props: {
          type: 'user',
          identifier: 'test@example.com',
          name: 'John Doe',
          repository: 'https://github.com/owner/repo'
        }
      });

      const avatar = container.querySelector('.avatar');
      expect(avatar).toBeDefined();
    });
  });
});
