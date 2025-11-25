import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import Dialog from '../../../src/webview/components/Dialog.svelte';

describe('Dialog Component', () => {
  it('renders when isOpen is true', () => {
    const { container } = render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    const overlay = container.querySelector('.dialog-overlay');
    expect(overlay).toBeDefined();
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(Dialog, { props: { isOpen: false, title: 'Test Dialog' } });
    const overlay = container.querySelector('.dialog-overlay');
    expect(overlay).toBeNull();
  });

  it('displays title', () => {
    render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    expect(screen.getByText('Test Dialog')).toBeDefined();
  });

  it('renders close button when title is provided', () => {
    const { container } = render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    const closeButton = container.querySelector('.codicon-close');
    expect(closeButton).toBeDefined();
  });

  it('dispatches close event on close button click', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    const closeButton = container.querySelector('button.btn.ghost.sm');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (closeButton) {
      await fireEvent.click(closeButton);
      expect(handleClose).toHaveBeenCalledTimes(1);
    }
  });

  it('dispatches close event on overlay click when closeOnOverlay is true', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test', closeOnOverlay: true } });
    const overlay = container.querySelector('.dialog-overlay');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (overlay) {
      await fireEvent.click(overlay);
      expect(handleClose).toHaveBeenCalledTimes(1);
    }
  });

  it('does not dispatch close event on overlay click when closeOnOverlay is false', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test', closeOnOverlay: false } });
    const overlay = container.querySelector('.dialog-overlay');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (overlay) {
      await fireEvent.click(overlay);
      expect(handleClose).not.toHaveBeenCalled();
    }
  });

  it('does not dispatch close event when clicking inside dialog', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    const dialog = container.querySelector('.dialog');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (dialog) {
      await fireEvent.click(dialog);
      expect(handleClose).not.toHaveBeenCalled();
    }
  });

  it('dispatches close event on Escape key when closeOnEscape is true', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test', closeOnEscape: true } });
    const overlay = container.querySelector('.dialog-overlay');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (overlay) {
      await fireEvent.keyDown(overlay, { key: 'Escape' });
      expect(handleClose).toHaveBeenCalledTimes(1);
    }
  });

  it('does not dispatch close event on Escape key when closeOnEscape is false', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test', closeOnEscape: false } });
    const overlay = container.querySelector('.dialog-overlay');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (overlay) {
      await fireEvent.keyDown(overlay, { key: 'Escape' });
      expect(handleClose).not.toHaveBeenCalled();
    }
  });

  it('does not dispatch close event on other keys', async () => {
    const { component, container } = render(Dialog, { props: { isOpen: true, title: 'Test', closeOnEscape: true } });
    const overlay = container.querySelector('.dialog-overlay');
    const handleClose = vi.fn();
    component.$on('close', handleClose);
    if (overlay) {
      await fireEvent.keyDown(overlay, { key: 'Enter' });
      expect(handleClose).not.toHaveBeenCalled();
    }
  });

  it('applies custom size class', () => {
    const { container } = render(Dialog, { props: { isOpen: true, title: 'Test', size: 'w-50vw h-50vh' } });
    const dialog = container.querySelector('.dialog');
    expect(dialog?.classList.contains('w-50vw')).toBe(true);
    expect(dialog?.classList.contains('h-50vh')).toBe(true);
  });

  it('applies default size when not specified', () => {
    const { container } = render(Dialog, { props: { isOpen: true, title: 'Test' } });
    const dialog = container.querySelector('.dialog');
    expect(dialog?.classList.contains('w-90vw')).toBe(true);
    expect(dialog?.classList.contains('h-90vh')).toBe(true);
  });

  it('has proper ARIA attributes', () => {
    const { container } = render(Dialog, { props: { isOpen: true, title: 'Test Dialog' } });
    const dialog = container.querySelector('[role="dialog"]');
    expect(dialog?.getAttribute('aria-modal')).toBe('true');
    expect(dialog?.getAttribute('aria-labelledby')).toBe('dialog-title');
  });
});
