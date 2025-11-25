import { beforeEach, describe, expect, it, vi } from 'vitest';
import { detectVSCodeTheme, onThemeChange } from '../../../src/webview/utils/theme';

type MockMediaQueryList = {
  matches: boolean;
  addEventListener: ReturnType<typeof vi.fn>;
  removeEventListener: ReturnType<typeof vi.fn>;
};

describe('Theme utilities', () => {
  let mockMediaQuery: MockMediaQueryList;

  beforeEach(() => {
    mockMediaQuery = {
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    };

    window.matchMedia = vi.fn(() => mockMediaQuery as unknown as MediaQueryList);
  });

  describe('detectVSCodeTheme', () => {
    it('returns dark when prefers-color-scheme is dark', () => {
      mockMediaQuery.matches = true;
      expect(detectVSCodeTheme()).toBe('dark');
    });

    it('returns light when prefers-color-scheme is light', () => {
      mockMediaQuery.matches = false;
      expect(detectVSCodeTheme()).toBe('light');
    });

    it('queries correct media query', () => {
      detectVSCodeTheme();
      expect(window.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)');
    });
  });

  describe('onThemeChange', () => {
    it('adds event listener for theme changes', () => {
      const callback = vi.fn();
      onThemeChange(callback);
      expect(mockMediaQuery.addEventListener).toHaveBeenCalledWith('change', expect.any(Function));
    });

    it('calls callback with dark when theme changes to dark', () => {
      const callback = vi.fn();
      onThemeChange(callback);

      const handler = mockMediaQuery.addEventListener.mock.calls[0][1] as (event: MediaQueryListEvent) => void;
      handler({ matches: true } as MediaQueryListEvent);

      expect(callback).toHaveBeenCalledWith('dark');
    });

    it('calls callback with light when theme changes to light', () => {
      const callback = vi.fn();
      onThemeChange(callback);

      const handler = mockMediaQuery.addEventListener.mock.calls[0][1] as (event: MediaQueryListEvent) => void;
      handler({ matches: false } as MediaQueryListEvent);

      expect(callback).toHaveBeenCalledWith('light');
    });

    it('returns cleanup function', () => {
      const callback = vi.fn();
      const cleanup = onThemeChange(callback);

      expect(typeof cleanup).toBe('function');
    });

    it('cleanup function removes event listener', () => {
      const callback = vi.fn();
      const cleanup = onThemeChange(callback);

      cleanup();

      expect(mockMediaQuery.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function));
    });

    it('removes same handler that was added', () => {
      const callback = vi.fn();
      const cleanup = onThemeChange(callback);

      const addedHandler = mockMediaQuery.addEventListener.mock.calls[0][1] as
        (event: MediaQueryListEvent) => void;
      cleanup();
      const removedHandler = mockMediaQuery.removeEventListener.mock.calls[0][1] as
        (event: MediaQueryListEvent) => void;

      expect(addedHandler).toBe(removedHandler);
    });
  });
});
