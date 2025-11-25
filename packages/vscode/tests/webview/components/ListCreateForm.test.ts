import { fireEvent, render, screen } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ListCreateForm from '../../../src/webview/components/ListCreateForm.svelte';

describe('ListCreateForm Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.alert = vi.fn();
  });

  describe('Basic Rendering', () => {
    it('renders name input', () => {
      render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name');
      expect(nameInput).toBeDefined();
    });

    it('renders create button', () => {
      render(ListCreateForm);
      expect(screen.getByText('Create List')).toBeDefined();
    });

    it('renders toggle custom ID button', () => {
      const { container } = render(ListCreateForm);
      const toggleButton = container.querySelector('[title="Use custom ID"]');
      expect(toggleButton).toBeDefined();
    });

    it('does not show custom ID input by default', () => {
      render(ListCreateForm);
      const customIdInput = screen.queryByLabelText('Custom ID');
      expect(customIdInput).toBeNull();
    });
  });

  describe('Custom ID Toggle', () => {
    it('shows custom ID input when toggle button clicked', async () => {
      const { container } = render(ListCreateForm);
      const toggleButton = container.querySelector('[title="Use custom ID"]');
      if (toggleButton) {
        await fireEvent.click(toggleButton);
        const customIdInput = screen.getByLabelText('Custom ID');
        expect(customIdInput).toBeDefined();
      }
    });

    it('hides custom ID input when toggle button clicked again', async () => {
      const { container } = render(ListCreateForm);
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
        await fireEvent.click(toggleButton);
        const customIdInput = screen.queryByLabelText('Custom ID');
        expect(customIdInput).toBeNull();
      }
    });

    it('shows custom ID hint when custom ID is enabled', async () => {
      const { container } = render(ListCreateForm);
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
        expect(screen.getByText('Letters, numbers, hyphens, and underscores only')).toBeDefined();
      }
    });
  });

  describe('ID Generation', () => {
    it('generates ID from name', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'My Test List';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: my-test-list/)).toBeDefined();
    });

    it('converts spaces to hyphens', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Multiple   Spaces';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: multiple-spaces/)).toBeDefined();
    });

    it('converts to lowercase', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'UPPERCASE';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: uppercase/)).toBeDefined();
    });

    it('removes special characters', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test@#$List!';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: test-list/)).toBeDefined();
    });

    it('truncates to 40 characters', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'a'.repeat(50);
      await fireEvent.input(nameInput);
      const idMatch = screen.getByText(/ID:/).textContent?.match(/ID: (.+)/);
      expect(idMatch?.[1].length).toBeLessThanOrEqual(40);
    });

    it('removes leading and trailing hyphens', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = '---test---';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: test/)).toBeDefined();
    });

    it('shows invalid for empty generated ID', async () => {
      const { container: _container } = render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = '!@#$%';
      await fireEvent.input(nameInput);
      expect(screen.getByText(/ID: invalid/)).toBeDefined();
    });
  });

  describe('Validation', () => {
    it('disables create button when name is empty', () => {
      render(ListCreateForm);
      const createButton = screen.getByText('Create List').closest('button') as HTMLButtonElement;
      expect(createButton?.disabled).toBe(true);
    });

    it('enables create button when name is provided', async () => {
      render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test List';
      await fireEvent.input(nameInput);
      const createButton = screen.getByText('Create List').closest('button') as HTMLButtonElement;
      expect(createButton?.disabled).toBe(false);
    });

    it('alerts when custom ID is invalid', async () => {
      const { container } = render(ListCreateForm);
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
      }
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test';
      await fireEvent.input(nameInput);
      const customIdInput = screen.getByLabelText('Custom ID') ;
      customIdInput.value = 'invalid@id!';
      await fireEvent.input(customIdInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect(global.alert).toHaveBeenCalledWith(
        'List ID must be 1-40 characters and contain only letters, numbers, hyphens, and underscores'
      );
    });
  });

  describe('Event Dispatching', () => {
    it('dispatches createList event with generated ID', async () => {
      const { component } = render(ListCreateForm);
      const handleCreateList = vi.fn();
      component.$on('createList', handleCreateList);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'My Test List';
      await fireEvent.input(nameInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect(handleCreateList).toHaveBeenCalledTimes(1);
      expect((handleCreateList.mock.calls[0][0] as CustomEvent).detail).toEqual({
        id: 'my-test-list',
        name: 'My Test List'
      });
    });

    it('dispatches createList event with custom ID', async () => {
      const { component, container } = render(ListCreateForm);
      const handleCreateList = vi.fn();
      component.$on('createList', handleCreateList);
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
      }
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Custom List';
      await fireEvent.input(nameInput);
      const customIdInput = screen.getByLabelText('Custom ID') ;
      customIdInput.value = 'custom-id';
      await fireEvent.input(customIdInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect(handleCreateList).toHaveBeenCalledTimes(1);
      expect((handleCreateList.mock.calls[0][0] as CustomEvent).detail).toEqual({
        id: 'custom-id',
        name: 'Custom List'
      });
    });

    it('clears form after successful submission', async () => {
      const { component } = render(ListCreateForm);
      component.$on('createList', vi.fn());
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test List';
      await fireEvent.input(nameInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect(nameInput.value).toBe('');
    });

    it('resets custom ID toggle after submission', async () => {
      const { component, container } = render(ListCreateForm);
      component.$on('createList', vi.fn());
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
      }
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test';
      await fireEvent.input(nameInput);
      const customIdInput = screen.getByLabelText('Custom ID') ;
      customIdInput.value = 'test-id';
      await fireEvent.input(customIdInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      const customIdInputAfter = screen.queryByLabelText('Custom ID');
      expect(customIdInputAfter).toBeNull();
    });
  });

  describe('Loading State', () => {
    it('shows "Creating..." when isCreating is true', () => {
      render(ListCreateForm, { props: { isCreating: true } });
      expect(screen.getByText('Creating...')).toBeDefined();
    });

    it('disables name input when isCreating is true', () => {
      render(ListCreateForm, { props: { isCreating: true } });
      const nameInput = screen.getByLabelText('Name') ;
      expect(nameInput.disabled).toBe(true);
    });

    it('disables create button when isCreating is true', () => {
      render(ListCreateForm, { props: { isCreating: true, newListName: 'Test' } });
      const createButton = screen.getByText('Creating...').closest('button') as HTMLButtonElement;
      expect(createButton?.disabled).toBe(true);
    });

    it('disables toggle button when isCreating is true', () => {
      const { container } = render(ListCreateForm, { props: { isCreating: true } });
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement as HTMLButtonElement;
      expect(toggleButton?.disabled).toBe(true);
    });
  });

  describe('Compact Mode', () => {
    it('hides create button in compact mode', () => {
      render(ListCreateForm, { props: { compact: true } });
      const createButton = screen.queryByText('Create List');
      expect(createButton).toBeNull();
    });

    it('still shows name input in compact mode', () => {
      render(ListCreateForm, { props: { compact: true } });
      const nameInput = screen.getByLabelText('Name');
      expect(nameInput).toBeDefined();
    });

    it('still shows toggle button in compact mode', () => {
      const { container } = render(ListCreateForm, { props: { compact: true } });
      const toggleButton = container.querySelector('.codicon-gear');
      expect(toggleButton).toBeDefined();
    });
  });

  describe('Edge Cases', () => {
    it('handles whitespace-only name', async () => {
      render(ListCreateForm);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = '   ';
      await fireEvent.input(nameInput);
      const createButton = screen.getByText('Create List').closest('button') as HTMLButtonElement;
      expect(createButton?.disabled).toBe(true);
    });

    it('trims whitespace from name before submission', async () => {
      const { component } = render(ListCreateForm);
      const handleCreateList = vi.fn();
      component.$on('createList', handleCreateList);
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = '  Test List  ';
      await fireEvent.input(nameInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect((handleCreateList.mock.calls[0][0] as CustomEvent<{ name: string }>).detail.name).toBe('Test List');
    });

    it('trims whitespace from custom ID before submission', async () => {
      const { component, container } = render(ListCreateForm);
      const handleCreateList = vi.fn();
      component.$on('createList', handleCreateList);
      const toggleButton = container.querySelector('.codicon-gear')?.parentElement;
      if (toggleButton) {
        await fireEvent.click(toggleButton);
      }
      const nameInput = screen.getByLabelText('Name') ;
      nameInput.value = 'Test';
      await fireEvent.input(nameInput);
      const customIdInput = screen.getByLabelText('Custom ID') ;
      customIdInput.value = '  test-id  ';
      await fireEvent.input(customIdInput);
      const createButton = screen.getByText('Create List');
      await fireEvent.click(createButton);
      expect((handleCreateList.mock.calls[0][0] as CustomEvent<{ id: string }>).detail.id).toBe('test-id');
    });
  });
});
