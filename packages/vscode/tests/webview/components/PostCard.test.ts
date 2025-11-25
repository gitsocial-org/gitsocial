import { fireEvent, render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import PostCard from '../../../src/webview/components/PostCard.svelte';
import type { Post } from '@gitsocial/core/client';

vi.mock('../../../src/webview/api', () => ({
  api: {
    getSettings: vi.fn(),
    openView: vi.fn(),
    getPosts: vi.fn(),
    toggleZenMode: vi.fn()
  }
}));

vi.mock('../../../src/webview/stores', () => ({
  settings: {
    subscribe: vi.fn((fn: (value: { autoLoadImages: boolean }) => void) => {
      fn({ autoLoadImages: true });
      return vi.fn();
    })
  }
}));

describe('PostCard Component', () => {
  const mockPost: Post = {
    id: 'https://github.com/user/repo#commit:abc123456789',
    type: 'post',
    author: {
      name: 'Test User',
      email: 'test@example.com'
    },
    content: 'Hello world! This is a test post.',
    timestamp: new Date('2025-01-15T10:00:00Z'),
    repository: 'https://github.com/user/repo',
    display: {
      commitHash: 'abc1234',
      commitUrl: 'https://github.com/user/repo/commit/abc123456789',
      repositoryName: 'user/repo',
      isOrigin: true,
      isUnpushed: false,
      totalReposts: 2
    },
    interactions: {
      comments: 5,
      reposts: 2
    }
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders post content', () => {
    render(PostCard, { props: { post: mockPost } });
    expect(screen.getByText('Hello world! This is a test post.')).toBeDefined();
  });

  it('displays author information', () => {
    render(PostCard, { props: { post: mockPost } });
    expect(screen.getByText('Test User')).toBeDefined();
    expect(screen.getByText('test@example.com')).toBeDefined();
  });

  it('shows interaction counts', () => {
    render(PostCard, { props: { post: mockPost } });
    expect(screen.getByText('5')).toBeDefined();
    expect(screen.getByText('2')).toBeDefined();
  });

  it('renders compact layout when specified', () => {
    render(PostCard, { props: { post: mockPost, layout: 'compact' } });
    const card = screen.getByTestId('post-card');
    expect(card).toBeDefined();
    expect(card.getAttribute('data-layout')).toBe('compact');
  });

  it('renders normal layout by default', () => {
    render(PostCard, { props: { post: mockPost } });
    const card = screen.getByTestId('post-card');
    expect(card).toBeDefined();
    expect(card.getAttribute('data-layout')).toBe('normal');
  });

  it('handles missing interactions gracefully', () => {
    const postWithoutInteractions = { ...mockPost, interactions: undefined };
    render(PostCard, { props: { post: postWithoutInteractions } });
    expect(screen.getByText('Hello world! This is a test post.')).toBeDefined();
  });

  it('renders repost layout for repost type', () => {
    const repost: Post = {
      ...mockPost,
      type: 'repost',
      originalPostId: 'https://github.com/other/repo#commit:def456'
    };
    render(PostCard, { props: { post: repost } });
    expect(screen.getByText(/reposted/)).toBeDefined();
  });

  it('hides interactions when interactive is false', () => {
    render(PostCard, { props: { post: mockPost, interactive: false } });
    const commentButton = screen.queryByTitle('Comment');
    expect(commentButton).toBeNull();
  });

  it('shows interactions when interactive is true', () => {
    render(PostCard, { props: { post: mockPost, interactive: true } });
    const commentButton = screen.queryByTitle('Comment');
    expect(commentButton).toBeDefined();
  });

  it('displays commit URL when available', () => {
    render(PostCard, { props: { post: mockPost } });
    const commitLink = screen.getByText(/github\.com\/user\/repo\/commit/);
    expect(commitLink).toBeDefined();
  });

  it('shows unpushed indicator for unpushed posts', () => {
    const unpushedPost = {
      ...mockPost,
      display: { ...mockPost.display, isUnpushed: true }
    };
    const { container } = render(PostCard, { props: { post: unpushedPost } });
    const unpushedElement = container.querySelector('.border-l-warning');
    expect(unpushedElement).toBeDefined();
  });

  describe('Error Handling', () => {
    it('handles malformed post ID gracefully', () => {
      const postWithBadId = {
        ...mockPost,
        id: 'invalid-post-id-format'
      };
      render(PostCard, { props: { post: postWithBadId } });
      // Should still render the post content
      expect(screen.getByText('Hello world! This is a test post.')).toBeDefined();
    });

    it('handles empty content gracefully', () => {
      const postWithEmptyContent = {
        ...mockPost,
        content: ''
      };
      render(PostCard, { props: { post: postWithEmptyContent } });
      // Should render without crashing even with empty content
      const card = screen.getByTestId('post-card');
      expect(card).toBeDefined();
    });

    it('handles invalid timestamp gracefully', () => {
      const postWithBadTimestamp = {
        ...mockPost,
        timestamp: 'invalid-date' as unknown as Date
      };
      render(PostCard, { props: { post: postWithBadTimestamp } });
      expect(screen.getByText('Hello world! This is a test post.')).toBeDefined();
    });
  });

  describe('User Interactions - Click Events', () => {
    it('opens post view when clickable card is clicked', async () => {
      const { api } = await import('../../../src/webview/api');
      render(PostCard, { props: { post: mockPost, clickable: true } });
      const card = screen.getByTestId('post-card');
      await fireEvent.click(card);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('viewPost', expect.any(String), {
        postId: mockPost.id,
        repository: mockPost.repository
      });
    });

    it('opens post view when Enter key pressed on clickable card', async () => {
      const { api } = await import('../../../src/webview/api');
      render(PostCard, { props: { post: mockPost, clickable: true } });
      const card = screen.getByTestId('post-card');
      await fireEvent.keyDown(card, { key: 'Enter' });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(1);
    });

    it('opens post view when Space key pressed on clickable card', async () => {
      const { api } = await import('../../../src/webview/api');
      render(PostCard, { props: { post: mockPost, clickable: true } });
      const card = screen.getByTestId('post-card');
      await fireEvent.keyDown(card, { key: ' ' });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(1);
    });

    it('does not open post view when card is not clickable', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      render(PostCard, { props: { post: mockPost, clickable: false } });
      const card = screen.getByTestId('post-card');
      await fireEvent.click(card);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(0);
    });

    it('toggles raw view when raw button is clicked', async () => {
      render(PostCard, { props: { post: mockPost, interactive: true } });
      const rawButton = screen.getByText('Raw');
      await fireEvent.click(rawButton);
      const preElement = screen.getByText(mockPost.content);
      expect(preElement.tagName).toBe('PRE');
    });

    it('opens fullscreen when fullscreen button clicked', async () => {
      const { api } = await import('../../../src/webview/api');
      render(PostCard, { props: { post: mockPost, interactive: true } });
      const fullscreenButton = screen.getByTitle('View fullscreen (F)');
      await fireEvent.click(fullscreenButton);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.toggleZenMode).toHaveBeenCalledTimes(1);
    });
  });

  describe('Props - Expanded Content', () => {
    it('renders expanded layout when expandContent is true', () => {
      render(PostCard, { props: { post: mockPost, expandContent: true } });
      const card = screen.getByTestId('post-card');
      expect(card).toBeDefined();
    });

    it('parses markdown in expanded view', () => {
      const postWithMarkdown = { ...mockPost, content: '# Header\n\nText' };
      const { container } = render(PostCard, { props: { post: postWithMarkdown, expandContent: true } });
      const markdownDiv = container.querySelector('.markdown-content');
      expect(markdownDiv).toBeDefined();
      expect(markdownDiv?.classList.contains('text-lg')).toBe(true);
      expect(markdownDiv?.innerHTML).toContain('Text');
    });

    it('renders parsed HTML with code blocks in expanded view', () => {
      const postWithCode = { ...mockPost, content: '```js\nconst x = 1;\n```\n\nText content' };
      const { container } = render(PostCard, { props: { post: postWithCode, expandContent: true } });
      const markdownDiv = container.querySelector('.markdown-content');
      expect(markdownDiv).toBeDefined();
      expect(markdownDiv?.classList.contains('text-lg')).toBe(true);
      expect(screen.getByText('Text content')).toBeDefined();
    });

    it('renders markdown with math in expanded view', () => {
      const postWithMath = { ...mockPost, content: 'Formula: $x = y$\n\nMore text' };
      const { container } = render(PostCard, { props: { post: postWithMath, expandContent: true } });
      const markdownContent = container.querySelector('.markdown-content');
      expect(markdownContent).toBeDefined();
    });

    it('uses parsedHtml branch in expanded layout when expandContent is true', async () => {
      const post = { ...mockPost, content: '# Heading\n\n**Bold** and [link](url)' };
      const { container } = render(PostCard, { props: { post, expandContent: true } });
      await tick();
      const mainLayout = container.querySelector('.w-full');
      expect(mainLayout).toBeDefined();
      const postContent = mainLayout?.querySelector('.post-content.mb-5');
      expect(postContent).toBeDefined();
      const markdownDiv = postContent?.querySelector('.markdown-content.text-lg.break-words');
      expect(markdownDiv).toBeDefined();
      const html = markdownDiv?.innerHTML || '';
      expect(html).toContain('Heading');
    });
  });

  describe('Props - Trimmed View', () => {
    it('shows trimmed content when trimmed is true', () => {
      const longPost = { ...mockPost, content: 'Line 1\nLine 2\nLine 3\nLine 4' };
      render(PostCard, { props: { post: longPost, trimmed: true } });
      expect(screen.getByText(/Line 1.*\.\.\./s)).toBeDefined();
    });
  });

  describe('Props - Collapsed', () => {
    it('hides content when collapsed is true', () => {
      render(PostCard, { props: { post: mockPost, collapsed: true } });
      expect(screen.queryByText(mockPost.content)).toBeNull();
    });
  });

  describe('Props - Hide Fullscreen Button', () => {
    it('hides fullscreen button when hideFullscreenButton is true', () => {
      render(PostCard, { props: { post: mockPost, interactive: true, hideFullscreenButton: true } });
      expect(screen.queryByTitle('View fullscreen (F)')).toBeNull();
    });

    it('shows fullscreen button when hideFullscreenButton is false', () => {
      render(PostCard, { props: { post: mockPost, interactive: true, hideFullscreenButton: false } });
      expect(screen.getByTitle('View fullscreen (F)')).toBeDefined();
    });
  });

  describe('Image Handling', () => {
    const postWithImages = {
      ...mockPost,
      content: 'Text\n![alt](https://example.com/image.jpg)'
    };

    it('renders image gallery when post contains images', () => {
      const { container } = render(PostCard, { props: { post: postWithImages } });
      const img = container.querySelector('img');
      expect(img).toBeDefined();
      expect(img?.getAttribute('src')).toBe('https://example.com/image.jpg');
    });

    it('auto-loads images when autoLoadImages is true', () => {
      const { container } = render(PostCard, { props: { post: postWithImages } });
      expect(container.querySelector('img')).toBeDefined();
    });

    it('shows load button when autoLoadImages is false', async () => {
      const stores = await import('../../../src/webview/stores');
      const mockedStores = vi.mocked(stores);
      mockedStores.settings.subscribe = vi.fn((fn) => {
        // eslint-disable-next-line @typescript-eslint/no-unsafe-call
        fn({ autoLoadImages: false });
        return vi.fn();
      });
      render(PostCard, { props: { post: postWithImages } });
      expect(screen.getByText(/Load.*image/)).toBeDefined();
    });

    it('loads images when load button clicked', async () => {
      const stores = await import('../../../src/webview/stores');
      const mockedStores = vi.mocked(stores);
      mockedStores.settings.subscribe = vi.fn((fn) => {
        // eslint-disable-next-line @typescript-eslint/no-unsafe-call
        fn({ autoLoadImages: false });
        return vi.fn();
      });
      const { container } = render(PostCard, { props: { post: postWithImages } });
      const loadBtn = screen.getByText(/Load.*image/);
      await fireEvent.click(loadBtn);
      const img = container.querySelector('img');
      expect(img).toBeDefined();
    });

    it('opens lightbox when image clicked', async () => {
      const { container } = render(PostCard, { props: { post: postWithImages } });
      let imgButton = container.querySelector('.image-button') as HTMLButtonElement;
      if (!imgButton) {
        const loadBtn = screen.getByText(/Load.*image/);
        await fireEvent.click(loadBtn);
        imgButton = container.querySelector('.image-button') as HTMLButtonElement;
      }
      await fireEvent.click(imgButton);
      const lightbox = screen.getByRole('dialog', { name: 'Image viewer' });
      expect(lightbox).toBeDefined();
    });

    it('closes lightbox when close button clicked', async () => {
      const { container } = render(PostCard, { props: { post: postWithImages } });
      let imgButton = container.querySelector('.image-button') as HTMLButtonElement;
      if (!imgButton) {
        const loadBtn = screen.getByText(/Load.*image/);
        await fireEvent.click(loadBtn);
        imgButton = container.querySelector('.image-button') as HTMLButtonElement;
      }
      await fireEvent.click(imgButton);
      const closeButtons = screen.getAllByLabelText('Close image viewer');
      await fireEvent.click(closeButtons[1]);
      expect(screen.queryByRole('dialog', { name: 'Image viewer' })).toBeNull();
    });

    it('navigates to next image when next button clicked', async () => {
      const multiImagePost = {
        ...mockPost,
        content: '![](https://example.com/img1.jpg)\n![](https://example.com/img2.jpg)'
      };
      const { container } = render(PostCard, { props: { post: multiImagePost } });
      if (!container.querySelector('.image-button')) {
        const loadBtn = screen.getByText(/Load.*images/);
        await fireEvent.click(loadBtn);
      }
      const images = container.querySelectorAll('.image-button');
      await fireEvent.click(images[0]);
      const nextButton = screen.getByLabelText('Next image');
      await fireEvent.click(nextButton);
      const lightboxImage = container.querySelector('.lightbox-image') as HTMLImageElement;
      expect(lightboxImage.src).toContain('img2.jpg');
    });

    it('navigates to previous image when prev button clicked', async () => {
      const multiImagePost = {
        ...mockPost,
        content: '![](https://example.com/img1.jpg)\n![](https://example.com/img2.jpg)'
      };
      const { container } = render(PostCard, { props: { post: multiImagePost } });
      if (!container.querySelector('.image-button')) {
        const loadBtn = screen.getByText(/Load.*images/);
        await fireEvent.click(loadBtn);
      }
      const images = container.querySelectorAll('.image-button');
      await fireEvent.click(images[1]);
      const prevButton = screen.getByLabelText('Previous image');
      await fireEvent.click(prevButton);
      const lightboxImage = container.querySelector('.lightbox-image') as HTMLImageElement;
      expect(lightboxImage.src).toContain('img1.jpg');
    });

    it('disables prev button on first image', async () => {
      const multiImagePost = {
        ...mockPost,
        content: '![](https://example.com/img1.jpg)\n![](https://example.com/img2.jpg)'
      };
      const { container } = render(PostCard, { props: { post: multiImagePost } });
      if (!container.querySelector('.image-button')) {
        const loadBtn = screen.getByText(/Load.*images/);
        await fireEvent.click(loadBtn);
      }
      const images = container.querySelectorAll('.image-button');
      await fireEvent.click(images[0]);
      const prevButton = screen.getByLabelText('Previous image') ;
      expect(prevButton.disabled).toBe(true);
    });

    it('disables next button on last image', async () => {
      const multiImagePost = {
        ...mockPost,
        content: '![](https://example.com/img1.jpg)\n![](https://example.com/img2.jpg)'
      };
      const { container } = render(PostCard, { props: { post: multiImagePost } });
      if (!container.querySelector('.image-button')) {
        const loadBtn = screen.getByText(/Load.*images/);
        await fireEvent.click(loadBtn);
      }
      const images = container.querySelectorAll('.image-button');
      await fireEvent.click(images[1]);
      const nextButton = screen.getByLabelText('Next image') ;
      expect(nextButton.disabled).toBe(true);
    });
  });

  describe('Quote Posts', () => {
    const quotePost: Post = {
      ...mockPost,
      type: 'quote',
      originalPostId: 'https://github.com/other/repo#commit:def456'
    };

    it('renders quote with resolved original post', () => {
      const originalPost = { ...mockPost, id: 'https://github.com/other/repo#commit:def456', content: 'Original content' };
      render(PostCard, {
        props: {
          post: quotePost,
          posts: [originalPost]
        }
      });
      expect(screen.getByText('Original content')).toBeDefined();
    });

    it('shows loading state while resolving quoted post', () => {
      render(PostCard, { props: { post: quotePost, posts: [] } });
      expect(screen.getByText(/Loading quoted post/)).toBeDefined();
    });

    it('attempts to resolve unavailable quoted post', async () => {
      const { api } = await import('../../../src/webview/api');
      const quotePostWithUnknown = {
        ...mockPost,
        type: 'quote' as const,
        originalPostId: 'https://github.com/unknown/repo#commit:unknown'
      };

      render(PostCard, { props: { post: quotePostWithUnknown, posts: [] } });

      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.getPosts).toHaveBeenCalledWith(
        { scope: 'byId:https://github.com/unknown/repo#commit:unknown' },
        expect.stringContaining('resolve-post')
      );
    });

    it('navigates to quoted post when quote card clicked', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      const originalPost = { ...mockPost, id: 'https://github.com/other/repo#commit:def456' };
      const { container } = render(PostCard, {
        props: {
          post: quotePost,
          posts: [originalPost]
        }
      });
      const quoteCard = container.querySelector('.card.ghost.border') as HTMLElement;
      await fireEvent.click(quoteCard);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledWith('viewPost', 'Post', expect.objectContaining({
        postId: originalPost.id
      }));
    });

    it('navigates to quoted post when Enter key pressed', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      const originalPost = { ...mockPost, id: 'https://github.com/other/repo#commit:def456' };
      const { container } = render(PostCard, {
        props: {
          post: quotePost,
          posts: [originalPost]
        }
      });
      const quoteCard = container.querySelector('.card.ghost.border') as HTMLElement;
      await fireEvent.keyDown(quoteCard, { key: 'Enter' });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(1);
    });

    it('hides quote when originalPostId matches anchorPostId', () => {
      render(PostCard, {
        props: {
          post: quotePost,
          anchorPostId: quotePost.originalPostId
        }
      });
      expect(screen.queryByText(/Original post/)).toBeNull();
    });
  });

  describe('Repost Type', () => {
    it('auto-resolves original post for repost', async () => {
      const { api } = await import('../../../src/webview/api');
      const repostPost: Post = {
        ...mockPost,
        type: 'repost',
        originalPostId: 'https://github.com/other/repo#commit:original123'
      };
      render(PostCard, { props: { post: repostPost, posts: [] } });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.getPosts).toHaveBeenCalledWith(
        { scope: 'byId:https://github.com/other/repo#commit:original123' },
        expect.stringContaining('resolve-post')
      );
    });

    it('shows loading state for repost original post', () => {
      const repostPost: Post = {
        ...mockPost,
        type: 'repost',
        originalPostId: 'https://github.com/other/repo#commit:original123'
      };
      render(PostCard, { props: { post: repostPost, posts: [] } });
      expect(screen.getByText(/Loading original post/)).toBeDefined();
    });

    it('navigates to post when repost header clicked', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      const repostPost: Post = {
        ...mockPost,
        type: 'repost',
        originalPostId: 'https://github.com/other/repo#commit:original123'
      };
      const { container } = render(PostCard, { props: { post: repostPost, posts: [], clickable: true } });
      const repostHeader = container.querySelector('[role="button"]') as HTMLElement;
      await fireEvent.click(repostHeader);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalledTimes(1);
    });
  });

  describe('Parent Context for Comments', () => {
    it('shows parent context when showParentContext is true', () => {
      const commentPost: Post = {
        ...mockPost,
        type: 'comment',
        parentCommentId: 'https://github.com/user/repo#commit:parent123'
      };
      const parentPost = {
        ...mockPost,
        id: 'https://github.com/user/repo#commit:parent123',
        content: 'Parent comment content'
      };
      render(PostCard, {
        props: {
          post: commentPost,
          posts: [parentPost],
          showParentContext: true
        }
      });
      expect(screen.getByText('Parent comment content')).toBeDefined();
    });

    it('shows loading state while resolving parent', () => {
      const commentPost: Post = {
        ...mockPost,
        type: 'comment',
        parentCommentId: 'https://github.com/user/repo#commit:parent123'
      };
      render(PostCard, {
        props: {
          post: commentPost,
          posts: [],
          showParentContext: true
        }
      });
      expect(screen.getByText(/Loading parent post/)).toBeDefined();
    });

    it('navigates to parent when parent card clicked', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      const commentPost: Post = {
        ...mockPost,
        type: 'comment',
        parentCommentId: 'https://github.com/user/repo#commit:parent123'
      };
      const parentPost = {
        ...mockPost,
        id: 'https://github.com/user/repo#commit:parent123',
        content: 'Parent comment content'
      };
      const { container } = render(PostCard, {
        props: {
          post: commentPost,
          posts: [parentPost],
          showParentContext: true
        }
      });
      const parentCards = container.querySelectorAll('.parent-context .card');
      await fireEvent.click(parentCards[0]);
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.openView).toHaveBeenCalled();
    });
  });

  describe('Markdown and Content Rendering', () => {
    it('renders parsed markdown in expanded view', () => {
      const mdPost = { ...mockPost, content: '# Header\n\nParagraph text' };
      render(PostCard, { props: { post: mdPost, expandContent: true } });
      expect(screen.getByText('Paragraph text')).toBeDefined();
    });

    it('shows raw text when raw view toggled', async () => {
      render(PostCard, { props: { post: mockPost, interactive: true } });
      const rawBtn = screen.getByText('Raw');
      await fireEvent.click(rawBtn);
      const pre = screen.getByText(mockPost.content);
      expect(pre.tagName).toBe('PRE');
    });

    it('shows content for multiline comments', () => {
      const multilineComment = {
        ...mockPost,
        type: 'comment' as const,
        content: 'Line 1\nLine 2\nLine 3'
      };
      const { container } = render(PostCard, { props: { post: multilineComment } });
      const content = container.querySelector('.post-content');
      expect(content?.textContent).toContain('Line 1');
    });

    it('shows full content for non-comment posts', () => {
      const multilinePost = {
        ...mockPost,
        type: 'post' as const,
        content: 'Line 1\nLine 2\nLine 3'
      };
      render(PostCard, { props: { post: multilinePost } });
      expect(screen.getByText(/Line 1.*Line 2.*Line 3/s)).toBeDefined();
    });
  });

  describe('Post Resolution System', () => {
    it('resolves missing original post via API', async () => {
      const { api } = await import('../../../src/webview/api');
      vi.clearAllMocks();
      const quotePost = {
        ...mockPost,
        type: 'quote' as const,
        originalPostId: 'https://github.com/missing/repo#commit:missing123'
      };
      render(PostCard, { props: { post: quotePost, posts: [] } });
      // eslint-disable-next-line @typescript-eslint/unbound-method
      expect(api.getPosts).toHaveBeenCalledWith(
        { scope: 'byId:https://github.com/missing/repo#commit:missing123' },
        expect.stringContaining('resolve-post')
      );
    });

    it('finds post in posts array', () => {
      const searchPost = { ...mockPost, id: 'https://github.com/search/repo#commit:search123', content: 'Searched content' };
      const quotePost = {
        ...mockPost,
        type: 'quote' as const,
        originalPostId: 'https://github.com/search/repo#commit:search123'
      };
      render(PostCard, {
        props: {
          post: quotePost,
          posts: [searchPost]
        }
      });
      expect(screen.getByText('Searched content')).toBeDefined();
    });

    it('finds post in posts Map', () => {
      const searchPost = { ...mockPost, id: 'https://github.com/search/repo#commit:search123', content: 'Map content' };
      const quotePost = {
        ...mockPost,
        type: 'quote' as const,
        originalPostId: 'https://github.com/search/repo#commit:search123'
      };
      const postsMap = new Map([['https://github.com/search/repo#commit:search123', searchPost]]);
      render(PostCard, {
        props: {
          post: quotePost,
          posts: postsMap
        }
      });
      expect(screen.getByText('Map content')).toBeDefined();
    });
  });

  describe('Lifecycle and Settings', () => {
    it('requests settings on mount', () => {
      const mockPost2 = {
        ...mockPost,
        id: 'https://github.com/test/test#commit:test123'
      };
      render(PostCard, { props: { post: mockPost2 } });
      const card = screen.getByTestId('post-card');
      expect(card).toBeDefined();
    });

    it('uses autoLoadImages from settings store', () => {
      const postWithImages = {
        ...mockPost,
        content: 'Text\n![alt](https://example.com/image.jpg)'
      };
      const { container } = render(PostCard, { props: { post: postWithImages } });
      expect(container.querySelector('img')).toBeDefined();
    });

    it('defaults to true when autoLoadImages undefined', async () => {
      const stores = await import('../../../src/webview/stores');
      const mockedStores = vi.mocked(stores);
      mockedStores.settings.subscribe = vi.fn((fn) => {
        // eslint-disable-next-line @typescript-eslint/no-unsafe-call
        fn({});
        return vi.fn();
      });
      const postWithImages = {
        ...mockPost,
        content: 'Text\n![alt](https://example.com/image.jpg)'
      };
      const { container } = render(PostCard, { props: { post: postWithImages } });
      expect(container.querySelector('img')).toBeDefined();
    });
  });
});
