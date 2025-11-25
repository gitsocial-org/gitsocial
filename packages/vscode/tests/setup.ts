import '@testing-library/jest-dom';
import { vi } from 'vitest';

global.acquireVsCodeApi = vi.fn(() => ({
  postMessage: vi.fn(),
  setState: vi.fn(),
  getState: vi.fn()
}));

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  }))
});

const _originalPostMessage = window.postMessage.bind(window);
window.postMessage = function(message: unknown, _targetOrigin: string) {
  const event = new MessageEvent('message', {
    data: message,
    origin: window.location.origin
  });
  window.dispatchEvent(event);
};
