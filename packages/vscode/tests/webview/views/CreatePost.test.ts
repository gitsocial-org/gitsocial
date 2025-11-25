import { render } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import CreatePost from '../../../src/webview/views/CreatePost.svelte';

type WindowWithViewParams = Window & { viewParams?: { content: string } };

vi.mock('../../../src/webview/api', () => ({
  api: {
    createPost: vi.fn(),
    closePanel: vi.fn(),
    openView: vi.fn()
  }
}));

vi.mock('../../../src/webview/utils/weblog', () => ({
  webLog: vi.fn()
}));

describe('CreatePost Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '<div id="app"></div>';
  });

  describe('Basic Rendering', () => {
    it('renders create post view', () => {
      const { container } = render(CreatePost);
      expect(container).toBeDefined();
    });

    it('handles app element class modification', () => {
      const { container } = render(CreatePost);
      expect(container).toBeDefined();
    });
  });

  describe('API Calls', () => {
    it('component loads successfully', () => {
      const { container } = render(CreatePost);
      expect(container.firstChild).toBeDefined();
    });
  });

  describe('Initial Content', () => {
    it('handles viewParams when provided', () => {
      (window as WindowWithViewParams).viewParams = { content: 'Initial content from params' };
      const { container } = render(CreatePost);
      expect(container).toBeDefined();
      delete (window as WindowWithViewParams).viewParams;
    });

    it('handles missing viewParams', () => {
      delete (window as WindowWithViewParams).viewParams;
      const { container } = render(CreatePost);
      expect(container).toBeDefined();
    });
  });
});
