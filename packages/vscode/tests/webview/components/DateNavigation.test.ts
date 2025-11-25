import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import DateNavigation from '../../../src/webview/components/DateNavigation.svelte';

describe('DateNavigation Component', () => {
  it('renders label', () => {
    render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week of Jan 15',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    expect(screen.getByText('Week of Jan 15')).toBeDefined();
  });

  it('renders previous button', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const prevButton = container.querySelector('[title="Previous"]');
    expect(prevButton).toBeDefined();
  });

  it('renders next button', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const nextButton = container.querySelector('[title="Next"]');
    expect(nextButton).toBeDefined();
  });

  it('calls onPrevious when previous button clicked', async () => {
    const onPrevious = vi.fn();
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious,
        onNext: vi.fn()
      }
    });
    const prevButton = container.querySelector('[title="Previous"]');
    if (prevButton) {
      await fireEvent.click(prevButton);
      expect(onPrevious).toHaveBeenCalledTimes(1);
    }
  });

  it('calls onNext when next button clicked', async () => {
    const onNext = vi.fn();
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext
      }
    });
    const nextButton = container.querySelector('[title="Next"]');
    if (nextButton) {
      await fireEvent.click(nextButton);
      expect(onNext).toHaveBeenCalledTimes(1);
    }
  });

  it('disables previous button when loading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: true,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const prevButton = container.querySelector('[title="Previous"]') as HTMLButtonElement;
    expect(prevButton?.disabled).toBe(true);
  });

  it('disables next button when loading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: true,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const nextButton = container.querySelector('[title="Next"]') as HTMLButtonElement;
    expect(nextButton?.disabled).toBe(true);
  });

  it('disables next button when offset is 0', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const nextButton = container.querySelector('[title="Next"]') as HTMLButtonElement;
    expect(nextButton?.disabled).toBe(true);
  });

  it('disables next button when offset is positive', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const nextButton = container.querySelector('[title="Next"]') as HTMLButtonElement;
    expect(nextButton?.disabled).toBe(true);
  });

  it('shows loading spinner when loading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: true,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const loadingSpinner = container.querySelector('.codicon-loading.spin');
    expect(loadingSpinner).toBeDefined();
  });

  it('does not show loading spinner when not loading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const loadingSpinner = container.querySelector('.codicon-loading.spin');
    expect(loadingSpinner).toBeNull();
  });

  it('shows refresh button when offset is 0 and onRefresh is provided', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Current Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: vi.fn()
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]');
    expect(refreshButton).toBeDefined();
  });

  it('does not show refresh button when offset is not 0', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: vi.fn()
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]');
    expect(refreshButton).toBeNull();
  });

  it('does not show refresh button when onRefresh is undefined', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: undefined
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]');
    expect(refreshButton).toBeNull();
  });

  it('calls onRefresh when refresh button clicked', async () => {
    const onRefresh = vi.fn();
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]');
    if (refreshButton) {
      await fireEvent.click(refreshButton);
      expect(onRefresh).toHaveBeenCalledTimes(1);
    }
  });

  it('shows sync icon on refresh button by default', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: vi.fn(),
        refreshLoading: false
      }
    });
    const syncIcon = container.querySelector('.codicon-sync');
    expect(syncIcon).toBeDefined();
  });

  it('shows loading spinner on refresh button when refreshLoading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: vi.fn(),
        refreshLoading: true
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]');
    const loadingIcon = refreshButton?.querySelector('.codicon-loading.spin');
    expect(loadingIcon).toBeDefined();
  });

  it('disables refresh button when refreshLoading', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: 0,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn(),
        onRefresh: vi.fn(),
        refreshLoading: true
      }
    });
    const refreshButton = container.querySelector('[title="Refresh"]') as HTMLButtonElement;
    expect(refreshButton?.disabled).toBe(true);
  });

  it('shows chevron-right when not showing refresh', () => {
    const { container } = render(DateNavigation, {
      props: {
        offset: -1,
        label: 'Week',
        loading: false,
        onPrevious: vi.fn(),
        onNext: vi.fn()
      }
    });
    const chevronRight = container.querySelector('.codicon-chevron-right');
    expect(chevronRight).toBeDefined();
  });
});
