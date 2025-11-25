import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import MarkdownEditor from '../../../src/webview/components/MarkdownEditor.svelte';

describe('MarkdownEditor Component', () => {
  it('renders textarea with placeholder', () => {
    render(MarkdownEditor, { props: { placeholder: 'Write something...' } });
    const textarea = screen.getByPlaceholderText('Write something...');
    expect(textarea).toBeDefined();
  });

  it('displays preview button', () => {
    render(MarkdownEditor, { props: {} });
    const previewButton = screen.getByTitle('Show preview');
    expect(previewButton).toBeDefined();
  });

  it('toggles preview visibility when button is clicked', async () => {
    const { container } = render(MarkdownEditor, { props: { value: '# Test' } });
    const previewButton = screen.getByTitle('Show preview');

    expect(container.querySelector('.markdown-content')).toBeNull();

    await fireEvent.click(previewButton);

    expect(container.querySelector('.markdown-content')).toBeDefined();

    await fireEvent.click(screen.getByTitle('Hide preview'));

    expect(container.querySelector('.markdown-content')).toBeNull();
  });

  it('shows submit and cancel buttons when handlers provided', () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { onSubmit, onCancel } });

    expect(screen.getByText('Save')).toBeDefined();
    expect(screen.getByText('Cancel')).toBeDefined();
  });

  it('disables submit button when value is empty and allowEmpty is false', () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: '', onSubmit, onCancel, allowEmpty: false } });

    const saveButton = screen.getByText('Save');
    expect(saveButton).toHaveProperty('disabled', true);
  });

  it('enables submit button when value is not empty', async () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    const { component } = render(MarkdownEditor, { props: { value: '', onSubmit, onCancel } });

    component.$set({ value: 'Test content' });
    await tick(); // Wait for Svelte reactivity to update

    const saveButton = screen.getByText('Save');
    expect(saveButton).toHaveProperty('disabled', false);
  });

  it('enables submit button when allowEmpty is true', () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: '', onSubmit, onCancel, allowEmpty: true } });

    const saveButton = screen.getByText('Save');
    expect(saveButton).toHaveProperty('disabled', false);
  });

  it('calls onSubmit when Save button is clicked', async () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: 'Test content', onSubmit, onCancel } });

    const saveButton = screen.getByText('Save');
    await fireEvent.click(saveButton);

    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it('calls onCancel when Cancel button is clicked', async () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: 'Test content', onSubmit, onCancel } });

    const cancelButton = screen.getByText('Cancel');
    await fireEvent.click(cancelButton);

    expect(onCancel).toHaveBeenCalledOnce();
  });

  it('shows "Saving..." text when creating is true', () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: 'Test', onSubmit, onCancel, creating: true } });

    expect(screen.getByText('Saving...')).toBeDefined();
  });

  it('disables buttons when creating is true', () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();

    render(MarkdownEditor, { props: { value: 'Test', onSubmit, onCancel, creating: true } });

    expect(screen.getByText('Cancel')).toHaveProperty('disabled', true);
    expect(screen.getByText('Saving...')).toHaveProperty('disabled', true);
  });

  it('disables textarea when disabled prop is true', () => {
    render(MarkdownEditor, { props: { placeholder: 'Test', disabled: true } });

    const textarea = screen.getByPlaceholderText('Test');
    expect(textarea).toHaveProperty('disabled', true);
  });
});
