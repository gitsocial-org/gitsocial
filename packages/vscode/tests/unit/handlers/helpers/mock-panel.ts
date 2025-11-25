import { vi } from 'vitest';
import type * as vscode from 'vscode';

export function createMockPanel(): vscode.WebviewPanel {
  return {
    webview: {
      postMessage: vi.fn(),
      onDidReceiveMessage: vi.fn(),
      html: '',
      options: {},
      cspSource: ''
    },
    onDidDispose: vi.fn(),
    onDidChangeViewState: vi.fn(),
    dispose: vi.fn(),
    reveal: vi.fn(),
    title: 'Test Panel',
    viewType: 'gitsocial.sidebar',
    visible: true,
    active: true,
    viewColumn: undefined,
    iconPath: undefined
  } as unknown as vscode.WebviewPanel;
}

export function getMockPostMessage(panel: vscode.WebviewPanel) {
  return vi.mocked(panel.webview.postMessage);
}

export function clearMockPostMessage(panel: vscode.WebviewPanel) {
  getMockPostMessage(panel).mockClear();
}

export function getLastPostMessage(panel: vscode.WebviewPanel) {
  const calls = getMockPostMessage(panel).mock.calls;
  return calls.length > 0 ? calls[calls.length - 1][0] : undefined;
}

export function getAllPostMessages(panel: vscode.WebviewPanel) {
  return getMockPostMessage(panel).mock.calls.map(call => call[0]);
}
