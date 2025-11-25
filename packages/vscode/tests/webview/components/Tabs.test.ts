import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import Tabs from '../../../src/webview/components/Tabs.svelte';

describe('Tabs Component', () => {
  const mockTabs = [
    { id: 'tab1', label: 'Tab One' },
    { id: 'tab2', label: 'Tab Two' },
    { id: 'tab3', label: 'Tab Three' }
  ];

  it('renders all tabs', () => {
    render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    expect(screen.getByText('Tab One')).toBeDefined();
    expect(screen.getByText('Tab Two')).toBeDefined();
    expect(screen.getByText('Tab Three')).toBeDefined();
  });

  it('applies active class to active tab', () => {
    const { container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const activeTab = Array.from(container.querySelectorAll('[role="button"]')).find(
      el => el.textContent?.includes('Tab One')
    );
    expect(activeTab?.classList.contains('tab-active')).toBe(true);
  });

  it('applies inactive class to inactive tabs', () => {
    const { container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const inactiveTab = Array.from(container.querySelectorAll('[role="button"]')).find(
      el => el.textContent?.includes('Tab Two')
    );
    expect(inactiveTab?.classList.contains('tab-inactive')).toBe(true);
  });

  it('dispatches change event on tab click', async () => {
    const { component } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const handleChange = vi.fn();
    component.$on('change', handleChange);
    const tab2 = screen.getByText('Tab Two');
    await fireEvent.click(tab2);
    expect(handleChange).toHaveBeenCalledTimes(1);
    expect((handleChange.mock.calls[0][0] as CustomEvent).detail).toEqual({ tabId: 'tab2' });
  });

  it('does not dispatch change event when clicking active tab', async () => {
    const { component } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const handleChange = vi.fn();
    component.$on('change', handleChange);
    const tab1 = screen.getByText('Tab One');
    await fireEvent.click(tab1);
    expect(handleChange).not.toHaveBeenCalled();
  });

  it('dispatches change event on Enter key', async () => {
    const { component, container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const handleChange = vi.fn();
    component.$on('change', handleChange);
    const inactiveTab = Array.from(container.querySelectorAll('[role="button"]')).find(
      el => el.textContent?.includes('Tab Two')
    );
    if (inactiveTab) {
      await fireEvent.keyDown(inactiveTab, { key: 'Enter' });
      expect(handleChange).toHaveBeenCalledTimes(1);
      expect((handleChange.mock.calls[0][0] as CustomEvent).detail).toEqual({ tabId: 'tab2' });
    }
  });

  it('does not dispatch change event on other keys', async () => {
    const { component, container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const handleChange = vi.fn();
    component.$on('change', handleChange);
    const inactiveTab = Array.from(container.querySelectorAll('[role="button"]')).find(
      el => el.textContent?.includes('Tab Two')
    );
    if (inactiveTab) {
      await fireEvent.keyDown(inactiveTab, { key: 'Space' });
      expect(handleChange).not.toHaveBeenCalled();
    }
  });

  it('shows icon when provided', () => {
    const tabsWithIcon = [
      { id: 'tab1', label: 'Tab One', icon: 'codicon codicon-home' }
    ];
    const { container } = render(Tabs, { props: { tabs: tabsWithIcon, activeTab: 'tab1' } });
    const iconSpan = container.querySelector('.codicon.codicon-home');
    expect(iconSpan).toBeDefined();
  });

  it('shows custom icon HTML when provided', () => {
    const tabsWithCustomIcon = [
      { id: 'tab1', label: 'Tab One', customIcon: '<svg><circle r="5"/></svg>' }
    ];
    const { container } = render(Tabs, { props: { tabs: tabsWithCustomIcon, activeTab: 'tab1' } });
    const svg = container.querySelector('svg');
    expect(svg).toBeDefined();
  });

  it('shows count when provided', () => {
    const tabsWithCount = [
      { id: 'tab1', label: 'Tab One', count: 42 }
    ];
    render(Tabs, { props: { tabs: tabsWithCount, activeTab: 'tab1' } });
    expect(screen.getByText('42')).toBeDefined();
  });

  it('does not show count when undefined', () => {
    const { container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const countBadge = container.querySelector('.opacity-70');
    expect(countBadge).toBeNull();
  });

  it('shows unpushed count badge when greater than zero', () => {
    const tabsWithUnpushed = [
      { id: 'tab1', label: 'Tab One', unpushedCount: 3 }
    ];
    const { container } = render(Tabs, { props: { tabs: tabsWithUnpushed, activeTab: 'tab1' } });
    const badge = container.querySelector('.badge');
    expect(badge?.textContent).toBe('3');
  });

  it('does not show unpushed count badge when zero', () => {
    const tabsWithZeroUnpushed = [
      { id: 'tab1', label: 'Tab One', unpushedCount: 0 }
    ];
    const { container } = render(Tabs, { props: { tabs: tabsWithZeroUnpushed, activeTab: 'tab1' } });
    const badge = container.querySelector('.badge');
    expect(badge).toBeNull();
  });

  it('has proper accessibility attributes', () => {
    const { container } = render(Tabs, { props: { tabs: mockTabs, activeTab: 'tab1' } });
    const tabButtons = container.querySelectorAll('[role="button"]');
    expect(tabButtons.length).toBe(3);
    tabButtons.forEach(button => {
      expect(button.getAttribute('tabindex')).toBe('0');
    });
  });

  it('handles empty tabs array', () => {
    const { container } = render(Tabs, { props: { tabs: [], activeTab: '' } });
    const tabButtons = container.querySelectorAll('[role="button"]');
    expect(tabButtons.length).toBe(0);
  });

  it('shows all tab features together', () => {
    const fullFeaturedTabs = [
      {
        id: 'tab1',
        label: 'Featured Tab',
        icon: 'codicon codicon-star',
        count: 10,
        unpushedCount: 2
      }
    ];
    const { container } = render(Tabs, { props: { tabs: fullFeaturedTabs, activeTab: 'tab1' } });
    expect(screen.getByText('Featured Tab')).toBeDefined();
    expect(container.querySelector('.codicon-star')).toBeDefined();
    expect(screen.getByText('10')).toBeDefined();
    expect(container.querySelector('.badge')?.textContent).toBe('2');
  });
});
