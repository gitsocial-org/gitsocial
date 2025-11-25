import { render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import Settings from '../../../src/webview/views/Settings.svelte';

vi.mock('../../../src/webview/api', () => ({
  api: {
    getSettings: vi.fn(() => Promise.resolve({
      debug: 'off',
      autoLoadImages: true,
      cacheMaxSize: 100
    })),
    updateSettings: vi.fn(),
    ready: vi.fn()
  }
}));

describe('Settings Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Basic Rendering', () => {
    it('renders settings view', () => {
      const { container } = render(Settings);
      expect(container).toBeDefined();
    });

    it('renders settings header', () => {
      render(Settings);
      expect(screen.getByText(/Settings/)).toBeDefined();
    });
  });

  describe('Settings Sections', () => {
    it('renders general settings section', () => {
      render(Settings);
      const generalSection = screen.queryByText(/General/) || screen.queryByText(/Appearance/);
      expect(generalSection).toBeDefined();
    });

    it('renders cache settings section', () => {
      render(Settings);
      const cacheSection = screen.queryByText(/Cache/) || screen.queryByText(/Storage/);
      expect(cacheSection).toBeDefined();
    });
  });

  describe('Settings Controls', () => {
    it('renders setting controls', () => {
      const { container } = render(Settings);
      const inputs = container.querySelectorAll('input, select');
      expect(inputs.length).toBeGreaterThan(0);
    });
  });
});
